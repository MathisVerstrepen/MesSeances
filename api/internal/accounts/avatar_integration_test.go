package accounts

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"messeances/api/internal/accountavatar"
)

func avatarFixture(t *testing.T) (*lifecycleFixture, string) {
	t.Helper()
	f := newLifecycleFixture(t)
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	s, err := accountavatar.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	f.service.avatars = s
	f.service.pictures = s
	return f, root
}
func avatarBytes(t *testing.T) []byte {
	t.Helper()
	im := image.NewNRGBA(image.Rect(0, 0, 7, 5))
	im.Set(3, 2, color.NRGBA{255, 0, 0, 255})
	var b bytes.Buffer
	if err := png.Encode(&b, im); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func upload(t *testing.T, f *lifecycleFixture, raw string) AvatarResult {
	t.Helper()
	b := avatarBytes(t)
	result, err := f.service.UploadAvatar(t.Context(), raw, func() ([]byte, string, error) { return b, "image/png", nil })
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func storedAvatar(t *testing.T, f *lifecycleFixture, email string) (*string, string, int64) {
	t.Helper()
	var path *string
	var source string
	var rev int64
	if err := f.pool.QueryRow(t.Context(), `SELECT avatar_path,avatar_source,avatar_revision FROM accounts WHERE email=$1`, email).Scan(&path, &source, &rev); err != nil {
		t.Fatal(err)
	}
	return path, source, rev
}
func TestAvatarAuthorityLifecycleIntegration(t *testing.T) {
	f, root := avatarFixture(t)
	ctx := t.Context()
	a := f.complete(t, "avatar@example.com", "avatar_owner")
	b := f.complete(t, "other@example.com", "avatar_other")
	result := upload(t, f, a.Cookie.Token)
	if result.AvatarURL == nil || *result.AvatarURL != "/api/v1/account/avatar/1" {
		t.Fatal("private URL")
	}
	path, source, rev := storedAvatar(t, f, "avatar@example.com")
	if path == nil || source != "upload" || rev != 1 {
		t.Fatal("state")
	}
	if _, err := f.service.Avatar(ctx, b.Cookie.Token, 1); !errors.Is(err, ErrAvatarNotFound) {
		t.Fatal("cross-owner read")
	}
	if _, err := f.service.Avatar(ctx, "", 1); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("anonymous read")
	}
	if _, err := f.service.Avatar(ctx, a.Cookie.Token, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.UploadAvatar(ctx, a.Cookie.Token, func() ([]byte, string, error) { return []byte("bad"), "image/png", nil }); err == nil {
		t.Fatal("invalid accepted")
	}
	if _, err := os.Stat(filepath.Join(root, *path)); err != nil {
		t.Fatal("failed upload removed original")
	}
	upload(t, f, a.Cookie.Token)
	if _, err := os.Stat(filepath.Join(root, *path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("replacement retained old image")
	}
	if _, err := f.service.Avatar(ctx, a.Cookie.Token, 1); !errors.Is(err, ErrAvatarNotFound) {
		t.Fatal("stale revision")
	}
	if result, err := f.service.RemoveAvatar(ctx, a.Cookie.Token); err != nil || result.AvatarURL != nil {
		t.Fatal("remove", err)
	}
	if _, err := f.service.RemoveAvatar(ctx, a.Cookie.Token); err != nil {
		t.Fatal(err)
	}
	path, source, rev = storedAvatar(t, f, "avatar@example.com")
	if path != nil || source != "removed" || rev != 4 {
		t.Fatal("sticky removal")
	}
	upload(t, f, a.Cookie.Token)
	path, _, _ = storedAvatar(t, f, "avatar@example.com")
	grant, err := f.service.ReauthPassword(ctx, a.Cookie.Token, testPassword, ActionDelete, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = f.service.Delete(ctx, grant.Cookie.Token, grant.Grant, "SUPPRIMER"); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(root, *path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("delete retained image")
	}
}
func TestAvatarUploadCASAndAdmissionIntegration(t *testing.T) {
	for _, race := range []string{"remove", "upload", "logout", "delete"} {
		t.Run(race, func(t *testing.T) {
			f, _ := avatarFixture(t)
			a := f.complete(t, "race@example.com", "avatar_race")
			entered, resume := make(chan struct{}), make(chan struct{})
			done := make(chan error, 1)
			b := avatarBytes(t)
			go func() {
				_, err := f.service.UploadAvatar(t.Context(), a.Cookie.Token, func() ([]byte, string, error) { close(entered); <-resume; return b, "image/png", nil })
				done <- err
			}()
			<-entered
			switch race {
			case "remove":
				if _, err := f.service.RemoveAvatar(t.Context(), a.Cookie.Token); err != nil {
					t.Fatal(err)
				}
			case "upload":
				upload(t, f, a.Cookie.Token)
			case "logout":
				if err := f.service.Logout(t.Context(), a.Cookie.Token, true); err != nil {
					t.Fatal(err)
				}
			case "delete":
				if _, err := f.pool.Exec(t.Context(), `DELETE FROM accounts WHERE email='race@example.com'`); err != nil {
					t.Fatal(err)
				}
			}
			close(resume)
			err := <-done
			if err == nil {
				t.Fatal("stale upload published")
			}
			if race == "remove" || race == "upload" {
				if !errors.Is(err, ErrAvatarChanged) {
					t.Fatal(err)
				}
			}
		})
	}
	f, _ := avatarFixture(t)
	a := f.complete(t, "slots@example.com", "avatar_slots")
	one, _ := f.service.avatars.Admit(t.Context())
	two, _ := f.service.avatars.Admit(t.Context())
	defer one()
	defer two()
	if _, err := f.service.UploadAvatar(t.Context(), a.Cookie.Token, func() ([]byte, string, error) { t.Fatal("busy upload read body"); return nil, "", nil }); !errors.Is(err, accountavatar.ErrBusy) {
		t.Fatal("busy", err)
	}
	if _, err := f.service.UploadAvatar(t.Context(), "", func() ([]byte, string, error) { t.Fatal("anonymous read body"); return nil, "", nil }); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("anonymous", err)
	}
}

type pictureFixture struct {
	fetch func(context.Context, string) ([]byte, error)
}

func (p pictureFixture) Fetch(ctx context.Context, url string) ([]byte, error) {
	return p.fetch(ctx, url)
}
func TestGoogleAvatarCommitAndIntentIntegration(t *testing.T) {
	for _, race := range []string{"none", "remove", "upload", "unlink", "revoke", "recreate", "timeout"} {
		t.Run(race, func(t *testing.T) {
			f, _ := avatarFixture(t)
			a := completeGoogle(t, f, "picture@example.com", "picture_owner")
			identity := GoogleIdentity{Subject: "subject-picture_owner", Email: "picture@example.com", EmailVerified: true, EmailAuthoritative: true, Picture: "https://lh3.googleusercontent.com/photo"}
			f.useGoogle(identity)
			entered, resume := make(chan struct{}), make(chan struct{})
			png, err := accountavatar.Normalize(t.Context(), avatarBytes(t), "image/png")
			if err != nil {
				t.Fatal(err)
			}
			f.service.pictures = pictureFixture{func(ctx context.Context, _ string) ([]byte, error) {
				var flows int
				if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM account_oauth_flows`).Scan(&flows); err != nil || flows != 0 {
					t.Error("fetch preceded committed callback")
				}
				close(entered)
				if race == "timeout" {
					<-ctx.Done()
					return nil, ctx.Err()
				}
				select {
				case <-resume:
					return png, nil
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}}
			start, state := startGoogle(t, f, "", GoogleStart{Mode: FlowLogin})
			done := make(chan error, 1)
			go func() {
				_, err := f.service.GoogleCallback(t.Context(), state, start.Browser.Token, "valid", "")
				done <- err
			}()
			<-entered
			switch race {
			case "remove":
				if _, err := f.service.RemoveAvatar(t.Context(), a.Cookie.Token); err != nil {
					t.Fatal(err)
				}
			case "upload":
				upload(t, f, a.Cookie.Token)
			case "unlink":
				if _, err := f.pool.Exec(t.Context(), `DELETE FROM account_google_identities`); err != nil {
					t.Fatal(err)
				}
			case "revoke":
				if err := f.service.Logout(t.Context(), a.Cookie.Token, true); err != nil {
					t.Fatal(err)
				}
			case "recreate":
				if _, err := f.pool.Exec(t.Context(), `DELETE FROM accounts WHERE email='picture@example.com'`); err != nil {
					t.Fatal(err)
				}
				f.useGoogle(GoogleIdentity{Subject: identity.Subject, Email: identity.Email, EmailVerified: true, EmailAuthoritative: true})
				loginGoogle(t, f)
			}
			close(resume)
			if err := <-done; err != nil {
				t.Fatal("picture broke committed login", err)
			}
			path, source, rev := storedAvatar(t, f, "picture@example.com")
			if race == "none" {
				if path == nil || source != "google" || rev != 1 {
					t.Fatal("not imported")
				}
			} else if race == "upload" {
				if path == nil || source != "upload" {
					t.Fatal("upload overwritten")
				}
			} else if path != nil {
				t.Fatal("stale import published")
			}
			// A repeated Google login never overwrites imported/uploaded/removed state.
			if race == "none" || race == "upload" || race == "remove" {
				f.service.pictures = pictureFixture{func(context.Context, string) ([]byte, error) {
					t.Fatal("nonempty/refused picture refetched")
					return nil, nil
				}}
				loginGoogle(t, f)
			}
		})
	}
}

type ambiguousAvatarDB struct {
	TransactionBeginner
	armed atomic.Bool
}

func (d *ambiguousAvatarDB) BeginTx(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
	tx, err := d.TransactionBeginner.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	return &ambiguousAvatarTx{Tx: tx, db: d}, nil
}

type ambiguousAvatarTx struct {
	pgx.Tx
	db *ambiguousAvatarDB
}

func (tx *ambiguousAvatarTx) Commit(ctx context.Context) error {
	err := tx.Tx.Commit(ctx)
	if err == nil && tx.db.armed.Swap(false) {
		return errors.New("commit reply lost")
	}
	return err
}
func TestAvatarAmbiguousCommitAndGCIntegration(t *testing.T) {
	f, root := avatarFixture(t)
	a := f.complete(t, "ambiguous@example.com", "avatar_ambiguous")
	upload(t, f, a.Cookie.Token)
	old, _, _ := storedAvatar(t, f, "ambiguous@example.com")
	db := &ambiguousAvatarDB{TransactionBeginner: f.pool}
	f.service.store = NewPostgresStore(db)
	b := avatarBytes(t)
	if _, err := f.service.UploadAvatar(t.Context(), a.Cookie.Token, func() ([]byte, string, error) { db.armed.Store(true); return b, "image/png", nil }); !errors.Is(err, ErrUnavailable) {
		t.Fatal("ambiguous outcome", err)
	}
	current, _, _ := storedAvatar(t, f, "ambiguous@example.com")
	if current == nil || *current == *old {
		t.Fatal("commit fixture did not commit")
	}
	for _, name := range []string{*old, *current} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatal("ambiguous file deleted")
		}
		past := f.now().Add(-2 * time.Hour)
		if err := os.Chtimes(filepath.Join(root, name), past, past); err != nil {
			t.Fatal(err)
		}
	}
	result, err := f.service.CleanupAvatars(t.Context())
	if err != nil || result.Deleted != 1 {
		t.Fatalf("GC result %v %v", result, err)
	}
	if _, err := os.Stat(filepath.Join(root, *current)); err != nil {
		t.Fatal("GC deleted referenced ambiguous commit")
	}
	if _, err := os.Stat(filepath.Join(root, *old)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("GC kept stale predecessor")
	}
}
func TestAvatarQuotaAndPendingIntegration(t *testing.T) {
	f, _ := avatarFixture(t)
	pending := f.pending(t, "pending-avatar@example.com")
	if _, err := f.service.UploadAvatar(t.Context(), pending.Cookie.Token, func() ([]byte, string, error) { t.Fatal("pending body read"); return nil, "", nil }); !errors.Is(err, ErrPending) {
		t.Fatal(err)
	}
	a := f.complete(t, "quota-avatar@example.com", "quota_avatar")
	for range 10 {
		_, err := f.service.UploadAvatar(t.Context(), a.Cookie.Token, func() ([]byte, string, error) { return nil, "", ErrInvalidInput })
		if !errors.Is(err, ErrInvalidInput) {
			t.Fatal(err)
		}
	}
	_, err := f.service.UploadAvatar(t.Context(), a.Cookie.Token, func() ([]byte, string, error) { t.Fatal("quota body read"); return nil, "", nil })
	var quota *RateLimitError
	if !errors.As(err, &quota) || quota.RetryAfter < 1 {
		t.Fatal(err)
	}
	var count int
	if err := f.pool.QueryRow(t.Context(), `SELECT count(*) FROM account_rate_limits WHERE purpose='avatar_write' AND window_seconds=900`).Scan(&count); err != nil || count != 1 {
		t.Fatal("quota persistence")
	}
	if strings.Contains(quota.Error(), "quota-avatar") {
		t.Fatal("quota identity leak")
	}
}
