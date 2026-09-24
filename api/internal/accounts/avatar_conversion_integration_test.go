package accounts

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"messeances/api/internal/accountavatar"
	"messeances/api/internal/database"
)

// Bootstrap the genuine historical schema and ledger, never weaken 047 to seed
// impossible current-schema PNG rows. This is isolated test-schema code only.
func historicalAvatarSchema(ctx context.Context, pool *pgxpool.Pool) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err = tx.Exec(ctx, `CREATE TABLE movieflow_schema_migrations(version bigint PRIMARY KEY,name text NOT NULL,applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	source := os.DirFS("../database/migrations")
	names, err := fs.Glob(source, "*.sql")
	if err != nil {
		return err
	}
	for _, name := range names {
		version, err := strconv.Atoi(name[:3])
		if err != nil {
			return err
		}
		if version > 46 {
			break
		}
		raw, err := fs.ReadFile(source, name)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, string(raw), pgx.QueryExecModeSimpleProtocol); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO movieflow_schema_migrations(version,name) VALUES($1,$2)`, version, name); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func conversionFixture(t *testing.T) (*lifecycleFixture, string) {
	t.Helper()
	f := newLifecycleFixtureWithSchema(t, historicalAvatarSchema)
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	media, err := accountavatar.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = media.Close() })
	f.service.avatars = media
	return f, root
}

func conversionPNG(t *testing.T) []byte {
	t.Helper()
	im := image.NewNRGBA(image.Rect(0, 0, 256, 256))
	for y := 0; y < 256; y++ {
		for x := 0; x < 256; x++ {
			im.SetNRGBA(x, y, color.NRGBA{uint8(x), uint8(y), 100, uint8(x)})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, im); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func seedConversionAvatar(t *testing.T, f *lifecycleFixture, root, email, source string, revision int64) string {
	t.Helper()
	var id int64
	if err := f.pool.QueryRow(t.Context(), `SELECT id FROM accounts WHERE email=$1`, email).Scan(&id); err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("%032x.png", id)
	if err := os.WriteFile(filepath.Join(root, name), conversionPNG(t), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(t.Context(), `UPDATE accounts SET avatar_path=$2,avatar_source=$3,avatar_revision=$4 WHERE email=$1`, email, name, source, revision); err != nil {
		t.Fatal(err)
	}
	return name
}

func upgradeConversionSchema(t *testing.T, f *lifecycleFixture) *PostgresStore {
	t.Helper()
	if err := database.RunMigrations(t.Context(), f.pool); err != nil {
		t.Fatal(err)
	}
	return NewPostgresStore(f.pool)
}

func TestAvatarConversionFreshAndHistoricalSchemaIntegration(t *testing.T) {
	f, _ := avatarFixture(t)
	if err := NewPostgresStore(f.pool).AvatarConversionReady(t.Context()); err != nil {
		t.Fatal("fresh schema not ready", err)
	}
	old, root := conversionFixture(t)
	old.complete(t, "legacy@example.com", "legacy_schema")
	name := seedConversionAvatar(t, old, root, "legacy@example.com", "upload", 2)
	store := upgradeConversionSchema(t, old)
	if err := store.AvatarConversionReady(t.Context()); !errors.Is(err, ErrAvatarConversionRequired) {
		t.Fatal("unconverted schema ready", err)
	}
	for _, sql := range []string{
		`UPDATE accounts SET auth_revision=auth_revision+1 WHERE email='legacy@example.com'`,
		`INSERT INTO accounts(email,pending_kind,created_at,avatar_source,avatar_path,avatar_revision) VALUES('bad@example.com','email',now(),'upload','000000000000000000000000000000ff.png',1)`,
	} {
		if _, err := old.pool.Exec(t.Context(), sql); err == nil {
			t.Fatal("NOT VALID constraint did not enforce writes")
		}
	}
	// A same-named validated constraint on another relation is not readiness.
	if _, err := old.pool.Exec(t.Context(), `CREATE TABLE avatar_decoy(x int CONSTRAINT accounts_avatar_webp_check CHECK(x>0))`); err != nil {
		t.Fatal(err)
	}
	if err := store.AvatarConversionReady(t.Context()); !errors.Is(err, ErrAvatarConversionRequired) {
		t.Fatal("decoy marked ready")
	}
	result, err := store.ConvertAvatars(t.Context(), old.service.avatars)
	if err != nil || result.Converted != 1 {
		t.Fatal(result, err)
	}
	if err := store.AvatarConversionReady(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, name)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("predecessor retained")
	}
	if err := old.service.avatars.Remove(name); err != nil {
		t.Fatal("cleanup not idempotent", err)
	}
}

func TestAvatarConversionPartialResumeAndIntentIntegration(t *testing.T) {
	f, root := conversionFixture(t)
	a := f.complete(t, "first@example.com", "conversion_first")
	f.pending(t, "pending@example.com")
	f.complete(t, "removed@example.com", "conversion_removed")
	if _, err := f.pool.Exec(t.Context(), `UPDATE accounts SET avatar_source='removed',avatar_revision=7 WHERE email='removed@example.com'`); err != nil {
		t.Fatal(err)
	}
	one := seedConversionAvatar(t, f, root, "first@example.com", "upload", 4)
	two := seedConversionAvatar(t, f, root, "pending@example.com", "google", 3)
	if err := os.Remove(filepath.Join(root, two)); err != nil {
		t.Fatal(err)
	}
	store := upgradeConversionSchema(t, f)
	result, err := store.ConvertAvatars(t.Context(), f.service.avatars)
	if !errors.Is(err, ErrUnavailable) || result.Converted != 1 {
		t.Fatal(result, err)
	}
	path, source, rev := storedAvatar(t, f, "first@example.com")
	if path == nil || !strings.HasSuffix(*path, ".webp") || source != "upload" || rev != 5 {
		t.Fatal("first row not committed")
	}
	if _, err := os.Stat(filepath.Join(root, one)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("confirmed commit did not clean")
	}
	if err := store.AvatarConversionReady(t.Context()); !errors.Is(err, ErrAvatarConversionRequired) {
		t.Fatal("partial marked complete")
	}
	if err := os.WriteFile(filepath.Join(root, two), conversionPNG(t), 0600); err != nil {
		t.Fatal(err)
	}
	result, err = store.ConvertAvatars(t.Context(), f.service.avatars)
	if err != nil || result.Converted != 1 || result.AlreadyWebP != 1 {
		t.Fatal(result, err)
	}
	_, source, rev = storedAvatar(t, f, "pending@example.com")
	if source != "google" || rev != 4 {
		t.Fatal("pending source lost")
	}
	var pending *string
	if err := f.pool.QueryRow(t.Context(), `SELECT pending_kind FROM accounts WHERE email='pending@example.com'`).Scan(&pending); err != nil || pending == nil {
		t.Fatal("pending account changed")
	}
	_, source, rev = storedAvatar(t, f, "removed@example.com")
	if source != "removed" || rev != 7 {
		t.Fatal("removal intent changed")
	}
	if _, err := f.service.Avatar(t.Context(), a.Cookie.Token, 5); err != nil {
		t.Fatal("session/auth state changed", err)
	}
	result, err = store.ConvertAvatars(t.Context(), f.service.avatars)
	if err != nil || result.Converted != 0 || result.AlreadyWebP != 2 {
		t.Fatal("rerun not idempotent", result, err)
	}
}

type conversionCommitProbe struct {
	pool        *pgxpool.Pool
	mode        string
	pending     pgx.Tx
	afterCommit func()
}
type conversionProbeTx struct {
	pgx.Tx
	owner   *conversionCommitProbe
	changed bool
}

func (p *conversionCommitProbe) BeginTx(ctx context.Context, o pgx.TxOptions) (pgx.Tx, error) {
	tx, err := p.pool.BeginTx(ctx, o)
	if err != nil {
		return nil, err
	}
	return &conversionProbeTx{Tx: tx, owner: p}, nil
}
func (p *conversionProbeTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	tag, err := p.Tx.Exec(ctx, sql, args...)
	if err == nil && strings.Contains(sql, "SET avatar_path=$2,avatar_revision=avatar_revision+1") {
		p.changed = true
	}
	if p.owner.mode == "final" {
		p.changed = err == nil && strings.Contains(sql, "VALIDATE CONSTRAINT")
	}
	return tag, err
}
func (p *conversionProbeTx) Commit(ctx context.Context) error {
	if !p.changed {
		return p.Tx.Commit(ctx)
	}
	switch p.owner.mode {
	case "final":
		if err := p.Tx.Commit(ctx); err != nil {
			return err
		}
	case "unlink":
		if err := p.Tx.Commit(ctx); err != nil {
			return err
		}
		p.owner.afterCommit()
		return nil
	case "commit":
		if err := p.Tx.Commit(ctx); err != nil {
			return err
		}
	case "rollback":
		if err := p.Tx.Rollback(ctx); err != nil {
			return err
		}
	case "pending":
		p.owner.pending = p.Tx
	}
	return errors.New("synthetic lost commit reply")
}
func (p *conversionProbeTx) Rollback(ctx context.Context) error {
	if p.owner.pending == p.Tx {
		return nil
	}
	return p.Tx.Rollback(ctx)
}

func TestAvatarConversionAmbiguousCommitAndBarrierIntegration(t *testing.T) {
	for _, mode := range []string{"commit", "rollback", "pending"} {
		t.Run(mode, func(t *testing.T) {
			f, root := conversionFixture(t)
			f.complete(t, "ambiguous@example.com", "conversion_ambiguous")
			old := seedConversionAvatar(t, f, root, "ambiguous@example.com", "google", 8)
			store := upgradeConversionSchema(t, f)
			probe := &conversionCommitProbe{pool: f.pool, mode: mode}
			result, err := NewPostgresStore(probe).ConvertAvatars(t.Context(), f.service.avatars)
			if !errors.Is(err, ErrUnavailable) || result.Converted != 0 {
				t.Fatal(result, err)
			}
			if probe.pending != nil {
				t.Cleanup(func() { _ = probe.pending.Rollback(context.Background()) })
			}
			entries, err := os.ReadDir(root)
			if err != nil {
				t.Fatal(err)
			}
			var candidates int
			for _, entry := range entries {
				if strings.HasSuffix(entry.Name(), ".webp") {
					candidates++
				}
			}
			if candidates != 1 {
				t.Fatal("ambiguous candidate removed")
			}
			if _, err := os.Stat(filepath.Join(root, old)); err != nil {
				t.Fatal("ambiguous predecessor removed")
			}
			if mode == "pending" {
				ctx, cancel := context.WithTimeout(t.Context(), 60*time.Millisecond)
				result, err = store.ConvertAvatars(ctx, f.service.avatars)
				cancel()
				if err == nil || result.Examined != 0 {
					t.Fatal("restart bypassed predecessor barrier", result, err)
				}
				if err := probe.pending.Commit(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			result, err = store.ConvertAvatars(t.Context(), f.service.avatars)
			if err != nil {
				t.Fatal(result, err)
			}
			_, source, rev := storedAvatar(t, f, "ambiguous@example.com")
			if source != "google" || rev != 9 {
				t.Fatal("revision incremented twice")
			}
			// Legacy orphans from a committed lost reply remain eligible for normal GC.
			if mode != "rollback" {
				age := f.now().Add(-2 * time.Hour)
				if err := os.Chtimes(filepath.Join(root, old), age, age); err != nil {
					t.Fatal(err)
				}
				if result, err := f.service.CleanupAvatars(t.Context()); err != nil || result.Deleted != 1 {
					t.Fatal("legacy orphan GC", result, err)
				}
			}
		})
	}
}

func TestAvatarConversionCASAndInvalidFileIntegration(t *testing.T) {
	for _, kind := range []string{"cas", "delete", "auth", "source", "revision", "corrupt", "wrong-size", "overflow", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			f, root := conversionFixture(t)
			f.complete(t, "invalid@example.com", "conversion_invalid")
			name := seedConversionAvatar(t, f, root, "invalid@example.com", "upload", 2)
			if kind == "overflow" {
				if _, err := f.pool.Exec(t.Context(), `UPDATE accounts SET avatar_revision=9223372036854775807`); err != nil {
					t.Fatal(err)
				}
			}
			store := upgradeConversionSchema(t, f)
			rows, err := store.avatarConversionPage(t.Context(), 0)
			if err != nil || len(rows) != 1 {
				t.Fatal(err)
			}
			switch kind {
			case "cas":
				if _, err := f.pool.Exec(t.Context(), `UPDATE accounts SET avatar_path=NULL,avatar_source='removed',avatar_revision=3`); err != nil {
					t.Fatal(err)
				}
			case "delete":
				if _, err := f.pool.Exec(t.Context(), `DELETE FROM accounts`); err != nil {
					t.Fatal(err)
				}
			case "auth":
				rows[0].authRevision++
			case "source":
				rows[0].source = "google"
			case "revision":
				rows[0].avatarRevision++
			case "corrupt":
				if err := os.WriteFile(filepath.Join(root, name), []byte("bad"), 0600); err != nil {
					t.Fatal(err)
				}
			case "wrong-size":
				if err := os.WriteFile(filepath.Join(root, name), avatarBytes(t), 0600); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if kind == "cancel" {
				cancel()
			}
			if _, _, err := store.convertAvatar(ctx, f.service.avatars, rows[0]); err == nil {
				t.Fatal("unsafe conversion succeeded")
			}
			if _, err := os.Stat(filepath.Join(root, name)); err != nil {
				t.Fatal("failed conversion removed source")
			}
			if err := store.AvatarConversionReady(t.Context()); !errors.Is(err, ErrAvatarConversionRequired) {
				t.Fatal("failure marked complete")
			}
		})
	}
}

func TestAvatarConversionFinalCommitAndCleanupFailureIntegration(t *testing.T) {
	for _, mode := range []string{"final", "unlink", "stage"} {
		t.Run(mode, func(t *testing.T) {
			if os.Geteuid() == 0 && mode != "final" {
				t.Skip("permission fixture requires non-root UID")
			}
			f, root := conversionFixture(t)
			f.complete(t, "finish@example.com", "conversion_finish")
			name := seedConversionAvatar(t, f, root, "finish@example.com", "upload", 1)
			store := upgradeConversionSchema(t, f)
			probe := &conversionCommitProbe{pool: f.pool, mode: mode, afterCommit: func() {
				if err := os.Chmod(root, 0500); err != nil {
					t.Error(err)
				}
			}}
			t.Cleanup(func() { _ = os.Chmod(root, 0700) })
			if mode == "stage" {
				if err := os.Chmod(root, 0500); err != nil {
					t.Fatal(err)
				}
			}
			result, err := NewPostgresStore(probe).ConvertAvatars(t.Context(), f.service.avatars)
			switch mode {
			case "final":
				if err == nil || result.Converted != 1 {
					t.Fatal("lost final commit reply treated as success", result, err)
				}
			case "unlink":
				if err != nil || result.Converted != 1 || result.CleanupFailures != 1 {
					t.Fatal("unlink failure invalidated commit", result, err)
				}
			case "stage":
				if err == nil || result.Converted != 0 {
					t.Fatal("unwritable root accepted", result, err)
				}
			}
			if mode != "final" {
				if _, err := os.Stat(filepath.Join(root, name)); err != nil {
					t.Fatal("source lost", err)
				}
			}
			if err := os.Chmod(root, 0700); err != nil {
				t.Fatal(err)
			}
			result, err = store.ConvertAvatars(t.Context(), f.service.avatars)
			if err != nil {
				t.Fatal(result, err)
			}
			_, source, rev := storedAvatar(t, f, "finish@example.com")
			if source != "upload" || rev != 2 {
				t.Fatal("resume changed committed revision")
			}
		})
	}
}

func TestAvatarConversionKeysetAndInvalidWebPIntegration(t *testing.T) {
	f, root := conversionFixture(t)
	data := conversionPNG(t)
	for i := 1; i <= 101; i++ {
		name := fmt.Sprintf("%032x.png", i)
		if _, err := f.pool.Exec(t.Context(), `INSERT INTO accounts(email,pending_kind,created_at,avatar_path,avatar_source,avatar_revision) VALUES($1,'google',now(),$2,'google',1)`, fmt.Sprintf("page%d@example.com", i), name); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	store := upgradeConversionSchema(t, f)
	page, err := store.avatarConversionPage(t.Context(), 0)
	if err != nil || len(page) != 100 {
		t.Fatal("unbounded keyset page", err)
	}
	if err := store.avatarConversionBarrier(t.Context(), f.service.avatars, true); !errors.Is(err, ErrAvatarConversionRequired) {
		t.Fatal("remaining PNG allowed finalization")
	}
	result, err := store.ConvertAvatars(t.Context(), f.service.avatars)
	if err != nil || result.Converted != 101 {
		t.Fatal("keyset traversal lost rows", result, err)
	}
	page, err = store.avatarConversionPage(t.Context(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, page[0].path), []byte("bad"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err = store.ConvertAvatars(t.Context(), f.service.avatars)
	if err == nil || result.Examined != 1 || result.Converted != 0 {
		t.Fatal("invalid existing WebP silently accepted", result, err)
	}
}
