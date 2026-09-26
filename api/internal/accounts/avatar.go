package accounts

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"messeances/api/internal/accountavatar"
)

var ErrAvatarChanged = errors.New("avatar changed")
var ErrAvatarNotFound = errors.New("avatar not found")

type AvatarResult struct {
	AvatarURL *string `json:"avatar_url"`
}

func avatarURL(a account) *string {
	if a.avatarPath == nil {
		return nil
	}
	u := "/api/v1/account/avatar/" + strconv.FormatInt(a.avatarRevision, 10)
	return &u
}

// UploadAvatar authenticates, charges durable quota and admits work before read is called.
// The callback is HTTP parsing only; no database transaction spans image or filesystem I/O.
func (s *Service) UploadAvatar(ctx context.Context, raw string, read func() ([]byte, string, error)) (AvatarResult, error) {
	var result AvatarResult
	if err := s.sessionQuota(ctx, raw, "avatar_write", false, true); err != nil {
		return result, err
	}
	var captured account
	err := s.store.withTransaction(ctx, func(tx pgx.Tx) error { var e error; captured, _, e = s.authorize(ctx, tx, raw, true); return e })
	if err != nil {
		return result, err
	}
	if s.avatars == nil {
		return result, ErrUnavailable
	}
	release, err := s.avatars.Admit(ctx)
	if err != nil {
		return result, err
	}
	defer release()
	b, media, err := read()
	if err != nil {
		return result, err
	}
	b, err = accountavatar.Normalize(ctx, b, media)
	if err != nil {
		return result, err
	}
	stage, err := s.avatars.Stage(ctx, b)
	if err != nil {
		return result, err
	}
	defer stage.Done()
	unlock, err := s.avatars.Guard(ctx)
	if err != nil {
		return result, err
	}
	defer unlock()
	path, err := stage.Publish(ctx)
	if err != nil {
		return result, err
	}
	err = s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		a, _, err := s.authorize(ctx, tx, raw, true)
		if err != nil {
			return err
		}
		if a.id != captured.id || a.revision != captured.revision || a.avatarRevision != captured.avatarRevision {
			return ErrAvatarChanged
		}
		if err = avatarBarrier(ctx, tx); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE accounts SET avatar_path=$2,avatar_source='upload',avatar_revision=avatar_revision+1 WHERE id=$1`, a.id, path); err != nil {
			return ErrUnavailable
		}
		a.avatarPath = &path
		a.avatarRevision++
		result.AvatarURL = avatarURL(a)
		return nil
	})
	// Never unlink either candidate or predecessor after an ambiguous commit.
	if err == nil {
		s.removeAvatar(captured.avatarPath)
	}
	return result, err
}
func avatarBarrier(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(719423046)`); err != nil {
		return ErrUnavailable
	}
	return nil
}
func (s *Service) RemoveAvatar(ctx context.Context, raw string) (AvatarResult, error) {
	if err := s.sessionQuota(ctx, raw, "avatar_write", false, true); err != nil {
		return AvatarResult{}, err
	}
	if s.avatars == nil {
		return AvatarResult{}, ErrUnavailable
	}
	var old *string
	err := s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		a, _, err := s.authorize(ctx, tx, raw, true)
		if err != nil {
			return err
		}
		old = a.avatarPath
		_, err = tx.Exec(ctx, `UPDATE accounts SET avatar_path=NULL,avatar_source='removed',avatar_revision=avatar_revision+1 WHERE id=$1`, a.id)
		if err != nil {
			return ErrUnavailable
		}
		return nil
	})
	if err == nil {
		s.removeAvatar(old)
	}
	return AvatarResult{}, err
}
func (s *Service) Avatar(ctx context.Context, raw string, revision int64) ([]byte, error) {
	var captured account
	check := func(tx pgx.Tx) error {
		a, _, err := s.authorize(ctx, tx, raw, true)
		if err != nil {
			return err
		}
		if a.avatarPath == nil || a.avatarRevision != revision {
			return ErrAvatarNotFound
		}
		if captured.id != 0 && (a.id != captured.id || a.revision != captured.revision || *a.avatarPath != *captured.avatarPath) {
			return ErrAvatarNotFound
		}
		captured = a
		return nil
	}
	if err := s.store.withTransaction(ctx, check); err != nil {
		return nil, err
	}
	if s.avatars == nil {
		return nil, ErrUnavailable
	}
	b, err := s.avatars.Read(ctx, *captured.avatarPath)
	if errors.Is(err, os.ErrNotExist) {
		slog.Warn("account avatar missing", "count", 1)
		return nil, ErrAvatarNotFound
	}
	if err != nil {
		slog.Warn("account avatar read failed", "count", 1)
		return nil, ErrUnavailable
	}
	if err = s.store.withTransaction(ctx, check); err != nil {
		return nil, err
	}
	return b, nil
}
func (s *Service) removeAvatar(path *string) {
	if path != nil && s.avatars != nil {
		if err := s.avatars.Remove(*path); err != nil {
			slog.Warn("account avatar unlink failed", "count", 1)
		}
	}
}

type avatarImport struct {
	id, revision, avatarRevision int64
	subject, picture             string
}

// GooglePictureFetcher returns normalized WebP bytes. Production uses the private
// media store; local issuer fixtures inject synthetic media without real network.
type GooglePictureFetcher interface {
	Fetch(context.Context, string) ([]byte, error)
}

func (s *Service) importAvatar(ctx context.Context, c *avatarImport) {
	if c == nil || s.avatars == nil || !accountavatar.ValidGoogleURL(c.picture) {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	release, err := s.avatars.Admit(ctx)
	if err != nil {
		return
	}
	defer release()
	if s.quota(ctx, "avatar_import", "account:"+strconv.FormatInt(c.id, 10), false) != nil {
		return
	}
	b, err := s.pictures.Fetch(ctx, c.picture)
	if err != nil {
		return
	}
	stage, err := s.avatars.Stage(ctx, b)
	if err != nil {
		return
	}
	defer stage.Done()
	unlock, err := s.avatars.Guard(ctx)
	if err != nil {
		return
	}
	defer unlock()
	path, err := stage.Publish(ctx)
	if err != nil {
		return
	}
	// Authentication already committed. All failures, including unknown commit outcomes,
	// are best effort and leave candidates for reference-barrier GC, never login errors.
	_ = s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		a, err := loadAccount(ctx, tx, c.id, true)
		if err != nil {
			return err
		}
		if a.revision != c.revision || a.avatarRevision != c.avatarRevision || a.avatarSource != "none" || a.avatarPath != nil {
			return ErrAvatarChanged
		}
		var linked bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM account_google_identities WHERE account_id=$1 AND issuer=$2 AND subject=$3)`, c.id, googleIssuer, c.subject).Scan(&linked); err != nil {
			return ErrUnavailable
		}
		if !linked {
			return ErrAvatarChanged
		}
		if err = avatarBarrier(ctx, tx); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE accounts SET avatar_path=$2,avatar_source='google',avatar_revision=avatar_revision+1 WHERE id=$1`, c.id, path); err != nil {
			return ErrUnavailable
		}
		return nil
	})
}

func (s *Service) CleanupAvatars(ctx context.Context) (accountavatar.SweepResult, error) {
	if s.avatars == nil {
		return accountavatar.SweepResult{}, nil
	}
	return s.avatars.Sweep(ctx, s.now(), func(ctx context.Context, names []string) (map[string]bool, error) {
		refs := make(map[string]bool)
		err := s.store.withTransaction(ctx, func(tx pgx.Tx) error {
			if err := avatarBarrier(ctx, tx); err != nil {
				return err
			}
			rows, err := tx.Query(ctx, `SELECT avatar_path FROM accounts WHERE avatar_path=ANY($1::text[])`, names)
			if err != nil {
				return ErrUnavailable
			}
			defer rows.Close()
			for rows.Next() {
				var name string
				if rows.Scan(&name) != nil {
					return ErrUnavailable
				}
				refs[name] = true
			}
			if rows.Err() != nil {
				return ErrUnavailable
			}
			return nil
		})
		return refs, err
	})
}

// CloseAvatars belongs to the API runtime, after all requests and workers drain.
func (s *Service) CloseAvatars() error {
	if s.avatars != nil {
		return s.avatars.Close()
	}
	return nil
}
