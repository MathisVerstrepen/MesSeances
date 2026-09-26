package accounts

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"math"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *Service) sessionQuota(ctx context.Context, raw, purpose string, send, complete bool) error {
	var id int64
	err := s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		a, _, err := s.authorize(ctx, tx, raw, complete)
		if err != nil {
			return err
		}
		id = a.id
		return nil
	})
	if err != nil {
		return err
	}
	return s.quota(ctx, purpose, "account:"+strconv.FormatInt(id, 10), send)
}

// Quotas are committed independently of rejected credentials/transactions. Keys
// contain no raw address, account ID or IP. Address quotas survive process restarts.
func (s *Service) quota(ctx context.Context, purpose, key string, send bool) error {
	h := hmac.New(sha256.New, s.hmacKey)
	h.Write([]byte("messeances-quota-v1\x00" + purpose + "\x00" + key))
	digest := h.Sum(nil)
	retry := 0
	err := s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, int64(binary.BigEndian.Uint64(digest[:8]))); err != nil {
			return ErrUnavailable
		}
		now := s.now().UTC()
		rules := []struct{ seconds, limit int }{{900, 10}}
		if send {
			rules = []struct{ seconds, limit int }{{3600, 3}, {86400, 10}}
		}
		if send {
			var last *time.Time
			if err := tx.QueryRow(ctx, `SELECT max(window_start) FROM account_rate_limits WHERE purpose=$1 AND key_digest=$2 AND window_seconds=60`, purpose, digest).Scan(&last); err != nil {
				return ErrUnavailable
			}
			if last != nil && now.Before(last.Add(time.Minute)) {
				retry = max(retry, int(math.Ceil(last.Add(time.Minute).Sub(now).Seconds())))
			}
		}
		for _, rule := range rules {
			start := now.Truncate(time.Duration(rule.seconds) * time.Second)
			var count int
			if err := tx.QueryRow(ctx, `INSERT INTO account_rate_limits(purpose,key_digest,window_start,window_seconds,count,expires_at) VALUES($1,$2,$3,$4,1,$5) ON CONFLICT(purpose,key_digest,window_start,window_seconds) DO UPDATE SET count=LEAST(account_rate_limits.count+1,1000000) RETURNING count`, purpose, digest, start, rule.seconds, start.Add(48*time.Hour)).Scan(&count); err != nil {
				return ErrUnavailable
			}
			if count > rule.limit {
				retry = max(retry, int(math.Ceil(start.Add(time.Duration(rule.seconds)*time.Second).Sub(now).Seconds())))
			}
		}
		if send && retry == 0 {
			if _, err := tx.Exec(ctx, `INSERT INTO account_rate_limits(purpose,key_digest,window_start,window_seconds,count,expires_at) VALUES($1,$2,$3,60,1,$4) ON CONFLICT DO NOTHING`, purpose, digest, now, now.Add(48*time.Hour)); err != nil {
				return ErrUnavailable
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if retry > 0 {
		return &RateLimitError{RetryAfter: retry}
	}
	return nil
}
