package accounts

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"messeances/api/internal/accountavatar"
)

var ErrAvatarConversionRequired = errors.New("account avatar conversion required")

// AvatarConversionReady checks the constraint on the actual search-path accounts
// relation, not a same-named constraint in another tenant/test schema.
func (s *PostgresStore) AvatarConversionReady(ctx context.Context) error {
	return s.withTransaction(ctx, func(tx pgx.Tx) error {
		var ready bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_constraint
			WHERE conrelid='accounts'::regclass AND conname='accounts_avatar_webp_check'
			AND contype='c' AND convalidated AND conenforced)`).Scan(&ready); err != nil {
			return ErrUnavailable
		}
		if !ready {
			return ErrAvatarConversionRequired
		}
		return nil
	})
}

type AvatarConversionResult struct {
	Examined, Converted, AlreadyWebP, CleanupFailures int
}

type avatarConversionRow struct {
	id, authRevision, avatarRevision int64
	source, path                     string
}

// ConvertAvatars is an offline operation. All account writers must be quiesced;
// media must own the matching database's exclusive root lock. It never resets a
// missing/corrupt reference, and never infers rollback from a commit error.
func (s *PostgresStore) ConvertAvatars(ctx context.Context, media *accountavatar.Store) (AvatarConversionResult, error) {
	var result AvatarConversionResult
	if media == nil {
		return result, ErrUnavailable
	}
	// A predecessor process may have lost its commit reply before releasing flock.
	// Commit this barrier before discovering references or making cleanup decisions.
	if err := s.avatarConversionBarrier(ctx, media, false); err != nil {
		return result, err
	}
	var after int64
	for {
		rows, err := s.avatarConversionPage(ctx, after)
		if err != nil {
			return result, err
		}
		if len(rows) == 0 {
			break
		}
		for _, row := range rows {
			result.Examined++
			converted, cleanupFailed, err := s.convertAvatar(ctx, media, row)
			if err != nil {
				return result, ErrUnavailable
			}
			if converted {
				result.Converted++
			} else {
				result.AlreadyWebP++
			}
			if cleanupFailed {
				result.CleanupFailures++
			}
			after = row.id
		}
	}
	return result, s.avatarConversionBarrier(ctx, media, true)
}

func (s *PostgresStore) avatarConversionPage(ctx context.Context, after int64) ([]avatarConversionRow, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var page []avatarConversionRow
	err := s.withTransaction(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id,auth_revision,avatar_revision,avatar_source,avatar_path
			FROM accounts WHERE id>$1 AND avatar_path IS NOT NULL ORDER BY id LIMIT 100`, after)
		if err != nil {
			return ErrUnavailable
		}
		defer rows.Close()
		for rows.Next() {
			var row avatarConversionRow
			if rows.Scan(&row.id, &row.authRevision, &row.avatarRevision, &row.source, &row.path) != nil {
				return ErrUnavailable
			}
			page = append(page, row)
		}
		if rows.Err() != nil {
			return ErrUnavailable
		}
		return nil
	})
	return page, err
}

func (s *PostgresStore) convertAvatar(ctx context.Context, media *accountavatar.Store, row avatarConversionRow) (converted, cleanupFailed bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	release, err := media.Admit(ctx)
	if err != nil {
		return false, false, err
	}
	defer release()
	if !accountavatar.LegacyPNGName(row.path) {
		_, err = media.Read(ctx, row.path)
		return false, false, err
	}
	if row.avatarRevision == math.MaxInt64 {
		return false, false, ErrAvatarChanged
	}
	b, err := media.ConvertPNG(ctx, row.path)
	if err != nil {
		return false, false, err
	}
	stage, err := media.Stage(ctx, b)
	if err != nil {
		return false, false, err
	}
	defer stage.Done()
	unlock, err := media.Guard(ctx)
	if err != nil {
		return false, false, err
	}
	defer unlock()
	name, err := stage.Publish(ctx)
	if err != nil {
		return false, false, err
	}
	err = s.withTransaction(ctx, func(tx pgx.Tx) error {
		a, err := loadAccount(ctx, tx, row.id, true)
		if err != nil {
			return err
		}
		if a.id != row.id || a.revision != row.authRevision || a.avatarRevision != row.avatarRevision || a.avatarSource != row.source || a.avatarPath == nil || *a.avatarPath != row.path {
			return ErrAvatarChanged
		}
		if err = avatarBarrier(ctx, tx); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE accounts SET avatar_path=$2,avatar_revision=avatar_revision+1 WHERE id=$1`, row.id, name)
		if err != nil || tag.RowsAffected() != 1 {
			return ErrUnavailable
		}
		return nil
	})
	if err != nil {
		// Candidate and predecessor both survive even when Commit actually succeeded.
		return false, false, err
	}
	return true, media.Remove(row.path) != nil, nil
}

func (s *PostgresStore) avatarConversionBarrier(ctx context.Context, media *accountavatar.Store, finish bool) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	unlock, err := media.Guard(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	return s.withTransaction(ctx, func(tx pgx.Tx) error {
		if err := avatarBarrier(ctx, tx); err != nil {
			return err
		}
		if !finish {
			return nil
		}
		var pending bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM accounts WHERE avatar_path IS NOT NULL
			AND avatar_path !~ '^[a-f0-9]{32}\.webp$')`).Scan(&pending); err != nil {
			return ErrUnavailable
		}
		if pending {
			return ErrAvatarConversionRequired
		}
		if _, err := tx.Exec(ctx, `ALTER TABLE accounts VALIDATE CONSTRAINT accounts_avatar_webp_check`); err != nil {
			return ErrUnavailable
		}
		return nil
	})
}
