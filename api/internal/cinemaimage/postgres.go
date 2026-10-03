package cinemaimage

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct{ pool *pgxpool.Pool }

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

const currentJoin = ` FROM theaters t JOIN schedule_snapshot s ON s.version=t.generation_id LEFT JOIN theater_images i ON i.provider=t.provider AND i.provider_theater_id=t.provider_id `
const recordColumns = `COALESCE(i.image_revision,0), COALESCE(i.file_key,''),COALESCE(i.width,0),COALESCE(i.height,0),COALESCE(i.size_bytes,0)`
const inventoryWhere = `WHERE ($1::text = '' OR t.provider = $1) AND ($2::text = '' OR t.name ILIKE $2 ESCAPE '\' OR t.city ILIKE $2 ESCAPE '\') `

func inventorySearchPattern(search string) string {
	if search == "" {
		return ""
	}
	return "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(search) + "%"
}

func rollbackImageTx(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}

func (p *PostgresRepository) Current(ctx context.Context, id Identity) (Record, error) {
	var r Record
	err := p.pool.QueryRow(ctx, `SELECT `+recordColumns+currentJoin+`WHERE t.provider=$1 AND t.provider_id=$2`, id.Provider, id.ProviderTheaterID).Scan(&r.Revision, &r.Key, &r.Width, &r.Height, &r.Size)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, ErrStorage
	}
	return r, nil
}
func (p *PostgresRepository) List(ctx context.Context, q ListQuery) ([]Theater, int, error) {
	if !q.Valid() {
		return nil, 0, ErrRequest
	}
	pattern := inventorySearchPattern(q.Search)
	items := make([]Theater, 0)
	total := 0
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, 0, ErrStorage
	}
	defer rollbackImageTx(tx)
	if tx.QueryRow(ctx, `SELECT count(*)`+currentJoin+inventoryWhere, q.Provider, pattern).Scan(&total) != nil {
		return nil, 0, ErrStorage
	}
	rows, err := tx.Query(ctx, `SELECT t.provider,t.provider_id,t.id,t.slug,t.name,t.address,t.postal_code,t.city,`+recordColumns+currentJoin+inventoryWhere+`ORDER BY lower(t.name),t.provider,t.provider_id LIMIT $3 OFFSET $4`, q.Provider, pattern, q.Limit, q.Offset)
	if err != nil {
		return nil, 0, ErrStorage
	}
	defer rows.Close()
	for rows.Next() {
		var t Theater
		var r Record
		if rows.Scan(&t.Provider, &t.ProviderTheaterID, &t.TheaterID, &t.Slug, &t.Name, &t.Address, &t.PostalCode, &t.City, &r.Revision, &r.Key, &r.Width, &r.Height, &r.Size) != nil {
			return nil, 0, ErrStorage
		}
		t.Result = result(Identity{t.Provider, t.ProviderTheaterID}, r)
		items = append(items, t)
	}
	if rows.Err() != nil || tx.Commit(ctx) != nil {
		return nil, 0, ErrStorage
	}
	return items, total, nil
}
func barrier(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(719423047)`); err != nil {
		return ErrStorage
	}
	return nil
}
func currentLocked(ctx context.Context, tx pgx.Tx, id Identity) (Record, error) {
	// Hold the active snapshot against publication until this short mutation commits.
	var version int64
	err := tx.QueryRow(ctx, `SELECT version FROM schedule_snapshot FOR SHARE`).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return Record{}, ErrNotFound
	}
	if err != nil {
		return Record{}, ErrStorage
	}
	var member bool
	if tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM theaters WHERE generation_id=$1 AND provider=$2 AND provider_id=$3)`, version, id.Provider, id.ProviderTheaterID).Scan(&member) != nil {
		return Record{}, ErrStorage
	}
	if !member {
		return Record{}, ErrNotFound
	}
	var r Record
	err = tx.QueryRow(ctx, `SELECT image_revision,COALESCE(file_key,''),COALESCE(width,0),COALESCE(height,0),COALESCE(size_bytes,0) FROM theater_images WHERE provider=$1 AND provider_theater_id=$2 FOR UPDATE`, id.Provider, id.ProviderTheaterID).Scan(&r.Revision, &r.Key, &r.Width, &r.Height, &r.Size)
	if errors.Is(err, pgx.ErrNoRows) {
		return Record{}, nil
	}
	if err != nil {
		return Record{}, ErrStorage
	}
	return r, nil
}
func (p *PostgresRepository) Save(ctx context.Context, id Identity, expected int64, candidate Record) (Record, string, error) {
	if !validIdentity(id) || expected < 0 || expected >= MaxRevision || !validRecord(candidate) {
		return Record{}, "", ErrStorage
	}
	return p.mutate(ctx, id, expected, &candidate)
}
func validRecord(r Record) bool {
	return finalName.MatchString(r.Key) && r.Width >= 1 && r.Width <= MaxEdge && r.Height >= 1 && r.Height <= MaxEdge && r.Size >= 1 && r.Size <= MaxOutput
}
func (p *PostgresRepository) Remove(ctx context.Context, id Identity, expected int64) (Record, string, error) {
	return p.mutate(ctx, id, expected, nil)
}
func (p *PostgresRepository) mutate(ctx context.Context, id Identity, expected int64, candidate *Record) (Record, string, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return Record{}, "", ErrStorage
	}
	defer rollbackImageTx(tx)
	if err = barrier(ctx, tx); err != nil {
		return Record{}, "", err
	}
	r, err := currentLocked(ctx, tx, id)
	if err != nil {
		return Record{}, "", err
	}
	if r.Revision != expected {
		return Record{}, "", ErrConflict
	}
	old := r.Key
	if candidate == nil && old == "" {
		if tx.Commit(ctx) != nil {
			return Record{}, "", ErrStorage
		}
		return r, "", nil
	}
	if r.Revision >= MaxRevision {
		return Record{}, "", ErrStorage
	}
	if _, err = tx.Exec(ctx, `INSERT INTO theater_images(provider,provider_theater_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, id.Provider, id.ProviderTheaterID); err != nil {
		return Record{}, "", ErrStorage
	}
	// Recheck after initialization, including non-runtime writers racing the INSERT.
	r, err = currentLocked(ctx, tx, id)
	if err != nil {
		return Record{}, "", err
	}
	if r.Revision != expected {
		return Record{}, "", ErrConflict
	}
	old = r.Key
	next := Record{Revision: r.Revision + 1}
	if candidate != nil {
		next = *candidate
		next.Revision = r.Revision + 1
	}
	var key *string
	var w, h, size *int
	if next.Key != "" {
		key = &next.Key
		w = &next.Width
		h = &next.Height
		size = &next.Size
	}
	if _, err = tx.Exec(ctx, `UPDATE theater_images SET image_revision=$3,file_key=$4,width=$5,height=$6,size_bytes=$7 WHERE provider=$1 AND provider_theater_id=$2`, id.Provider, id.ProviderTheaterID, next.Revision, key, w, h, size); err != nil {
		return Record{}, "", ErrStorage
	}
	if tx.Commit(ctx) != nil {
		return Record{}, "", ErrStorage
	}
	return next, old, nil
}
func (p *PostgresRepository) References(ctx context.Context, names []string) (map[string]bool, error) {
	refs := make(map[string]bool)
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, ErrStorage
	}
	defer rollbackImageTx(tx)
	if barrier(ctx, tx) != nil {
		return nil, ErrStorage
	}
	rows, err := tx.Query(ctx, `SELECT file_key FROM theater_images WHERE file_key=ANY($1::text[])`, names)
	if err != nil {
		return nil, ErrStorage
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		if rows.Scan(&key) != nil {
			return nil, ErrStorage
		}
		refs[key] = true
	}
	if rows.Err() != nil || tx.Commit(ctx) != nil {
		return nil, ErrStorage
	}
	return refs, nil
}
