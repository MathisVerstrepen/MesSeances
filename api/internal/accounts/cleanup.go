package accounts

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// purgeAccount preserves only the username claim through ON DELETE SET NULL.
// An anonymous flow cannot yet be mapped to a subject. Fence all such outstanding
// flows at deletion, including claimed exchanges, without retaining an identity
// tombstone. Fresh starts wait for this transaction and remain permitted.
func purgeAccount(ctx context.Context, tx pgx.Tx, id int64) error {
	return purgeAccountExceptFlow(ctx, tx, id, nil)
}

func purgeAccountExceptFlow(ctx context.Context, tx pgx.Tx, id int64, keep []byte) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(719423044)`); err != nil {
		return ErrUnavailable
	}
	if _, err := tx.Exec(ctx, `DELETE FROM account_oauth_flows WHERE mode='login' AND ($1::bytea IS NULL OR state_digest<>$1)`, keep); err != nil {
		return ErrUnavailable
	}
	if _, err := tx.Exec(ctx, `DELETE FROM accounts WHERE id=$1`, id); err != nil {
		return ErrUnavailable
	}
	return nil
}

func (s *Service) Delete(ctx context.Context, raw, grant, confirmation string) error {
	if confirmation != "SUPPRIMER" {
		return ErrInvalidInput
	}
	var path *string
	err := s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		a, ss, err := s.authorize(ctx, tx, raw, true)
		if err != nil {
			return err
		}
		if err = s.consumeGrant(ctx, tx, a, ss, grant, ActionDelete, ""); err != nil {
			return err
		}
		path = a.avatarPath
		return purgeAccount(ctx, tx, a.id)
	})
	if err == nil {
		s.removeAvatar(path)
	}
	return err
}

type CleanupResult struct {
	PendingAccounts, ExpiredRows int64
	Overdue                      bool
}

// Cleanup is one bounded sweep. Account operations enforce exact deadlines even
// during worker downtime. Account-first locks serialize cleanup with onboarding,
// callbacks and credential changes; child cleanup never locks a parent afterward.
func (s *Service) Cleanup(ctx context.Context) (CleanupResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var result CleanupResult
	var paths []*string
	err := s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		now := s.now().UTC()
		rows, err := tx.Query(ctx, `SELECT id FROM accounts WHERE pending_kind IS NOT NULL AND created_at<=$1 ORDER BY id LIMIT 100 FOR UPDATE SKIP LOCKED`, now.Add(-PendingLifetime))
		if err != nil {
			return ErrUnavailable
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
		if err != nil {
			return ErrUnavailable
		}
		for _, id := range ids {
			a, err := loadAccount(ctx, tx, id, true)
			if err != nil {
				return err
			}
			if a.expired(now) {
				if err = purgeAccount(ctx, tx, id); err != nil {
					return err
				}
				result.PendingAccounts++
				paths = append(paths, a.avatarPath)
			}
		}
		return nil
	})
	if err != nil {
		return result, err
	}
	for _, path := range paths {
		s.removeAvatar(path)
	}
	// Separate transaction releases deleted-account locks before selecting another
	// ordered account batch, avoiding inversions with cross-account login.
	err = s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		now := s.now().UTC()
		rows, err := tx.Query(ctx, `SELECT id FROM accounts WHERE id IN (
		 SELECT account_id FROM account_sessions WHERE expires_at<=$1 OR last_seen_at<=$2
		 UNION SELECT account_id FROM account_tokens WHERE expires_at<=$1
		 UNION SELECT account_id FROM account_oauth_flows WHERE expires_at<=$1 AND account_id IS NOT NULL
		 UNION SELECT account_id FROM account_mail_outbox WHERE account_id IS NOT NULL AND (expires_at<=$1 AND state='pending' OR finished_at<=$3)
		) ORDER BY id LIMIT 100 FOR UPDATE SKIP LOCKED`, now, now.Add(-SessionIdleLifetime), now.Add(-7*24*time.Hour))
		if err != nil {
			return ErrUnavailable
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
		if err != nil {
			return ErrUnavailable
		}
		for _, id := range ids {
			for _, q := range []struct {
				sql  string
				args []any
			}{
				{`DELETE FROM account_sessions WHERE token_digest IN (SELECT token_digest FROM account_sessions WHERE account_id=$1 AND (expires_at<=$2 OR last_seen_at<=$3) LIMIT 100)`, []any{id, now, now.Add(-SessionIdleLifetime)}},
				{`DELETE FROM account_tokens WHERE token_digest IN (SELECT token_digest FROM account_tokens WHERE account_id=$1 AND expires_at<=$2 LIMIT 100)`, []any{id, now}},
				{`DELETE FROM account_oauth_flows WHERE state_digest IN (SELECT state_digest FROM account_oauth_flows WHERE account_id=$1 AND expires_at<=$2 LIMIT 100)`, []any{id, now}},
				{`DELETE FROM account_mail_outbox WHERE id IN (SELECT id FROM account_mail_outbox WHERE account_id=$1 AND ((state='pending' AND expires_at<=$2) OR finished_at<=$3) LIMIT 100)`, []any{id, now, now.Add(-7 * 24 * time.Hour)}},
			} {
				tag, err := tx.Exec(ctx, q.sql, q.args...)
				if err != nil {
					return ErrUnavailable
				}
				result.ExpiredRows += tag.RowsAffected()
			}
		}
		return nil
	})
	if err != nil {
		return result, err
	}
	err = s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		now := s.now().UTC()
		for _, q := range []string{
			`DELETE FROM account_oauth_flows WHERE state_digest IN (SELECT state_digest FROM account_oauth_flows WHERE account_id IS NULL AND expires_at<=$1 LIMIT 1000)`,
			`DELETE FROM account_rate_limits WHERE (purpose,key_digest,window_start,window_seconds) IN (SELECT purpose,key_digest,window_start,window_seconds FROM account_rate_limits WHERE expires_at<=$1 LIMIT 1000)`,
			`DELETE FROM account_mail_suppressions WHERE address_digest IN (SELECT address_digest FROM account_mail_suppressions WHERE expires_at<=$1 LIMIT 1000)`,
			`DELETE FROM account_mail_outbox WHERE id IN (SELECT id FROM account_mail_outbox WHERE account_id IS NULL AND ((state='pending' AND expires_at<=$1) OR finished_at<=$1-interval '168 hours') LIMIT 1000)`,
		} {
			tag, err := tx.Exec(ctx, q, now)
			if err != nil {
				return ErrUnavailable
			}
			result.ExpiredRows += tag.RowsAffected()
		}
		err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM accounts WHERE pending_kind IS NOT NULL AND created_at<=$1::timestamptz-interval '168 hours') OR EXISTS(SELECT 1 FROM account_sessions WHERE expires_at<=$1 OR last_seen_at<=$1::timestamptz-interval '168 hours') OR EXISTS(SELECT 1 FROM account_tokens WHERE expires_at<=$1) OR EXISTS(SELECT 1 FROM account_oauth_flows WHERE expires_at<=$1) OR EXISTS(SELECT 1 FROM account_rate_limits WHERE expires_at<=$1) OR EXISTS(SELECT 1 FROM account_mail_suppressions WHERE expires_at<=$1) OR EXISTS(SELECT 1 FROM account_mail_outbox WHERE (state='pending' AND expires_at<=$1) OR finished_at<=$1::timestamptz-interval '168 hours')`, now.Add(-24*time.Hour)).Scan(&result.Overdue)
		if err != nil {
			return ErrUnavailable
		}
		return nil
	})
	return result, err
}
