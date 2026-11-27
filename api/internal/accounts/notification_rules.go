package accounts

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	NotificationKindActivity  = "cinema_activity"
	NotificationKindWatchlist = "watchlist_screening"

	NotificationScopeFollowed  = "followed"
	NotificationScopePreferred = "preferred"
	NotificationScopeCustom    = "custom"

	maxNotificationRules          = 20
	maxNotificationCustomTheaters = 64
)

var (
	ErrNotificationRuleNotFound = errors.New("notification rule not found")
	ErrNotificationRuleLimit    = errors.New("notification rule limit reached")
)

// NotificationRuleInput is a complete rule definition. Kind is immutable after
// creation; changing the cinema scope or re-enabling re-arms the rule so that
// previously published activity never notifies retroactively.
type NotificationRuleInput struct {
	Kind           string
	TheaterScope   string
	TheaterIDs     []string
	IncludeReturns bool
	Enabled        bool
	Push           bool
}

type NotificationRuleView struct {
	ID             string    `json:"id"`
	Kind           string    `json:"kind"`
	TheaterScope   string    `json:"theater_scope"`
	TheaterIDs     []string  `json:"theater_ids"`
	IncludeReturns bool      `json:"include_returns"`
	Enabled        bool      `json:"enabled"`
	Push           bool      `json:"push"`
	CreatedAt      time.Time `json:"created_at"`
}

type NotificationRulesView struct {
	Username          string                 `json:"username"`
	Rules             []NotificationRuleView `json:"rules"`
	MaxRules          int                    `json:"max_rules"`
	MaxCustomTheaters int                    `json:"max_custom_theaters"`
}

func normalizeNotificationRule(in NotificationRuleInput) (NotificationRuleInput, error) {
	switch in.Kind {
	case NotificationKindActivity:
	case NotificationKindWatchlist:
		if in.IncludeReturns {
			return in, ErrInvalidInput
		}
	default:
		return in, ErrInvalidInput
	}
	switch in.TheaterScope {
	case NotificationScopeFollowed, NotificationScopePreferred:
		if len(in.TheaterIDs) != 0 {
			return in, ErrInvalidInput
		}
		in.TheaterIDs = []string{}
	case NotificationScopeCustom:
		if len(in.TheaterIDs) == 0 || len(in.TheaterIDs) > maxNotificationCustomTheaters {
			return in, ErrInvalidInput
		}
		ids, err := canonicalTheaterIDs(in.TheaterIDs)
		if err != nil {
			return in, err
		}
		in.TheaterIDs = ids
	default:
		return in, ErrInvalidInput
	}
	return in, nil
}

func parseNotificationID(raw string) (int64, error) {
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 || strconv.FormatInt(n, 10) != raw {
		return 0, ErrInvalidInput
	}
	return n, nil
}

// readNotificationRules requires the authorized account lock.
func readNotificationRules(ctx context.Context, tx pgx.Tx, a account) (NotificationRulesView, error) {
	view := NotificationRulesView{Username: *a.username, Rules: []NotificationRuleView{}, MaxRules: maxNotificationRules, MaxCustomTheaters: maxNotificationCustomTheaters}
	rows, err := tx.Query(ctx, `SELECT id,kind,theater_scope,theater_ids,include_returns,enabled,push,created_at FROM account_notification_rules WHERE account_id=$1 ORDER BY id`, a.id)
	if err != nil {
		return view, ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		var rule NotificationRuleView
		var id int64
		if err := rows.Scan(&id, &rule.Kind, &rule.TheaterScope, &rule.TheaterIDs, &rule.IncludeReturns, &rule.Enabled, &rule.Push, &rule.CreatedAt); err != nil {
			return view, ErrUnavailable
		}
		rule.ID, rule.CreatedAt = strconv.FormatInt(id, 10), rule.CreatedAt.UTC()
		slices.Sort(rule.TheaterIDs)
		view.Rules = append(view.Rules, rule)
	}
	if rows.Err() != nil {
		return view, ErrUnavailable
	}
	return view, nil
}

// notificationArmPosition is the newest committed journal event. Rules only
// match strictly newer events, so creating or widening a rule is silent.
func notificationArmPosition(ctx context.Context, tx pgx.Tx) (int64, error) {
	var id int64
	if err := tx.QueryRow(ctx, `SELECT coalesce(max(id),0) FROM cinema_activity_episodes`).Scan(&id); err != nil {
		return 0, ErrUnavailable
	}
	return id, nil
}

func (s *Service) NotificationRules(ctx context.Context, rawSession string) (NotificationRulesView, error) {
	var view NotificationRulesView
	err := s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		a, _, err := s.authorize(ctx, tx, rawSession, true)
		if err != nil {
			return err
		}
		view, err = readNotificationRules(ctx, tx, a)
		return err
	})
	if err != nil {
		return NotificationRulesView{}, err
	}
	return view, nil
}

func (s *Service) notificationRuleMutation(ctx context.Context, rawSession, expectedUsername string, mutate func(pgx.Tx, account) error) (NotificationRulesView, error) {
	var view NotificationRulesView
	err := s.store.withTransaction(ctx, func(tx pgx.Tx) error {
		a, _, err := s.authorize(ctx, tx, rawSession, true)
		if err != nil {
			return err
		}
		if *a.username != expectedUsername {
			return ErrUnauthorized
		}
		if err := mutate(tx, a); err != nil {
			return err
		}
		view, err = readNotificationRules(ctx, tx, a)
		return err
	})
	if err != nil {
		return NotificationRulesView{}, err
	}
	return view, nil
}

func (s *Service) CreateNotificationRule(ctx context.Context, rawSession, expectedUsername string, input NotificationRuleInput) (NotificationRulesView, error) {
	input, err := normalizeNotificationRule(input)
	if err != nil {
		return NotificationRulesView{}, err
	}
	return s.notificationRuleMutation(ctx, rawSession, expectedUsername, func(tx pgx.Tx, a account) error {
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM account_notification_rules WHERE account_id=$1`, a.id).Scan(&count); err != nil {
			return ErrUnavailable
		}
		if count >= maxNotificationRules {
			return ErrNotificationRuleLimit
		}
		armed, err := notificationArmPosition(ctx, tx)
		if err != nil {
			return err
		}
		now := s.now().UTC()
		_, err = tx.Exec(ctx, `INSERT INTO account_notification_rules(account_id,kind,theater_scope,theater_ids,include_returns,enabled,push,armed_after_event_id,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$9)`,
			a.id, input.Kind, input.TheaterScope, input.TheaterIDs, input.IncludeReturns, input.Enabled, input.Push, armed, now)
		if err != nil {
			return ErrUnavailable
		}
		return nil
	})
}

func (s *Service) UpdateNotificationRule(ctx context.Context, rawSession, expectedUsername, rawID string, input NotificationRuleInput) (NotificationRulesView, error) {
	id, err := parseNotificationID(rawID)
	if err != nil {
		return NotificationRulesView{}, err
	}
	input, err = normalizeNotificationRule(input)
	if err != nil {
		return NotificationRulesView{}, err
	}
	return s.notificationRuleMutation(ctx, rawSession, expectedUsername, func(tx pgx.Tx, a account) error {
		var kind, scope string
		var ids []string
		var includeReturns, enabled, push bool
		err := tx.QueryRow(ctx, `SELECT kind,theater_scope,theater_ids,include_returns,enabled,push FROM account_notification_rules WHERE account_id=$1 AND id=$2 FOR UPDATE`, a.id, id).Scan(&kind, &scope, &ids, &includeReturns, &enabled, &push)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotificationRuleNotFound
		}
		if err != nil {
			return ErrUnavailable
		}
		if kind != input.Kind {
			return ErrInvalidInput
		}
		slices.Sort(ids)
		if scope == input.TheaterScope && slices.Equal(ids, input.TheaterIDs) && includeReturns == input.IncludeReturns && enabled == input.Enabled && push == input.Push {
			return nil
		}
		// Re-arm only when the matched population can grow; disabling, narrowing
		// return inclusion or an unchanged scope keeps the existing position.
		rearm := scope != input.TheaterScope || !slices.Equal(ids, input.TheaterIDs) || (!enabled && input.Enabled) || (!includeReturns && input.IncludeReturns)
		armed := int64(-1)
		if rearm {
			if armed, err = notificationArmPosition(ctx, tx); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `UPDATE account_notification_rules SET theater_scope=$3,theater_ids=$4,include_returns=$5,enabled=$6,push=$9,
 armed_after_event_id=CASE WHEN $7::bigint<0 THEN armed_after_event_id ELSE greatest(armed_after_event_id,$7) END,
 updated_at=greatest(created_at,$8) WHERE account_id=$1 AND id=$2`, a.id, id, input.TheaterScope, input.TheaterIDs, input.IncludeReturns, input.Enabled, armed, s.now().UTC(), input.Push)
		if err != nil {
			return ErrUnavailable
		}
		return nil
	})
}

// DeleteNotificationRule keeps delivered inbox entries; their rule reference is
// cleared by the foreign key.
func (s *Service) DeleteNotificationRule(ctx context.Context, rawSession, expectedUsername, rawID string) (NotificationRulesView, error) {
	id, err := parseNotificationID(rawID)
	if err != nil {
		return NotificationRulesView{}, err
	}
	return s.notificationRuleMutation(ctx, rawSession, expectedUsername, func(tx pgx.Tx, a account) error {
		tag, err := tx.Exec(ctx, `DELETE FROM account_notification_rules WHERE account_id=$1 AND id=$2`, a.id, id)
		if err != nil {
			return ErrUnavailable
		}
		if tag.RowsAffected() == 0 {
			return ErrNotificationRuleNotFound
		}
		return nil
	})
}
