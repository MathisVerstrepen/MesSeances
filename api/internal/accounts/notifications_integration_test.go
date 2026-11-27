package accounts

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"testing"
	"time"
)

func notificationRule(kind, scope string, ids []string, returns, enabled bool) NotificationRuleInput {
	return NotificationRuleInput{Kind: kind, TheaterScope: scope, TheaterIDs: ids, IncludeReturns: returns, Enabled: enabled}
}

func notificationEpisode(t *testing.T, f *lifecycleFixture, id int64, theater, movie, kind string) {
	t.Helper()
	if kind == "return_to_program" {
		followedActivityExec(t, f, `INSERT INTO cinema_activity_episodes(id,theater_id,anchor_provider,anchor_source_movie_id,kind,detected_at,first_screening_date,previous_program_end_date,observed_from,observed_through,break_from,break_through,detecting_generation)
 OVERRIDING SYSTEM VALUE VALUES($1,$2,'ugc',$3,'return_to_program',CURRENT_TIMESTAMP,'2026-10-03','2026-09-01','2026-10-03','2026-10-03','2026-09-02','2026-10-02',1)`, id, theater, movie)
		return
	}
	followedActivityExec(t, f, `INSERT INTO cinema_activity_episodes(id,theater_id,anchor_provider,anchor_source_movie_id,kind,detected_at,first_screening_date,observed_from,observed_through,detecting_generation)
 OVERRIDING SYSTEM VALUE VALUES($1,$2,'ugc',$3,$4,CURRENT_TIMESTAMP,CURRENT_DATE,CURRENT_DATE,CURRENT_DATE,1)`, id, theater, movie, kind)
}

func sweepNotifications(t *testing.T, f *lifecycleFixture) NotificationSweepResult {
	t.Helper()
	result, err := f.service.SweepNotifications(t.Context())
	if err != nil || result.Busy {
		t.Fatalf("sweep %+v: %v", result, err)
	}
	return result
}

func notificationCursor(t *testing.T, f *lifecycleFixture) int64 {
	t.Helper()
	var id int64
	if err := f.pool.QueryRow(t.Context(), `SELECT last_event_id FROM account_notification_cursor`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestNotificationRulesIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	ctx := t.Context()
	one := f.complete(t, "rules@example.com", "rules_one")
	two := f.complete(t, "rules-two@example.com", "rules_two")
	var initialAuthRevision int64
	if err := f.pool.QueryRow(ctx, `SELECT auth_revision FROM accounts WHERE email='rules@example.com'`).Scan(&initialAuthRevision); err != nil {
		t.Fatal(err)
	}
	view, err := f.service.NotificationRules(ctx, one.Cookie.Token)
	if err != nil || view.Username != "rules_one" || view.Rules == nil || len(view.Rules) != 0 || view.MaxRules != 20 || view.MaxCustomTheaters != 64 {
		t.Fatalf("initial %+v %v", view, err)
	}
	for name, input := range map[string]NotificationRuleInput{
		"kind":              notificationRule("other", NotificationScopeFollowed, nil, false, true),
		"scope":             notificationRule(NotificationKindActivity, "everywhere", nil, false, true),
		"watchlist returns": notificationRule(NotificationKindWatchlist, NotificationScopeFollowed, nil, true, true),
		"followed ids":      notificationRule(NotificationKindActivity, NotificationScopeFollowed, []string{"ugc-1"}, false, true),
		"custom empty":      notificationRule(NotificationKindActivity, NotificationScopeCustom, nil, false, true),
		"custom invalid":    notificationRule(NotificationKindActivity, NotificationScopeCustom, []string{"ugc/1"}, false, true),
		"custom duplicate":  notificationRule(NotificationKindActivity, NotificationScopeCustom, []string{"ugc-1", "ugc-1"}, false, true),
	} {
		if _, err := f.service.CreateNotificationRule(ctx, one.Cookie.Token, "rules_one", input); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("%s accepted: %v", name, err)
		}
	}
	tooMany := make([]string, 65)
	for i := range tooMany {
		tooMany[i] = "ugc-" + strconv.Itoa(i)
	}
	if _, err := f.service.CreateNotificationRule(ctx, one.Cookie.Token, "rules_one", notificationRule(NotificationKindActivity, NotificationScopeCustom, tooMany, false, true)); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("custom scope above limit accepted", err)
	}
	if _, err := f.service.CreateNotificationRule(ctx, two.Cookie.Token, "rules_one", notificationRule(NotificationKindActivity, NotificationScopeFollowed, nil, true, true)); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("client chose owner", err)
	}
	view, err = f.service.CreateNotificationRule(ctx, one.Cookie.Token, "rules_one", notificationRule(NotificationKindWatchlist, NotificationScopeCustom, []string{"ugc-2", "ugc-1"}, false, true))
	if err != nil || len(view.Rules) != 1 || !slices.Equal(view.Rules[0].TheaterIDs, []string{"ugc-1", "ugc-2"}) || view.Rules[0].Kind != NotificationKindWatchlist || !view.Rules[0].Enabled {
		t.Fatalf("create %+v %v", view, err)
	}
	id := view.Rules[0].ID
	if _, err := f.service.UpdateNotificationRule(ctx, one.Cookie.Token, "rules_one", id, notificationRule(NotificationKindActivity, NotificationScopeFollowed, nil, false, true)); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("kind changed", err)
	}
	if _, err := f.service.UpdateNotificationRule(ctx, two.Cookie.Token, "rules_two", id, notificationRule(NotificationKindWatchlist, NotificationScopeFollowed, nil, false, true)); !errors.Is(err, ErrNotificationRuleNotFound) {
		t.Fatal("foreign rule updated", err)
	}
	if _, err := f.service.DeleteNotificationRule(ctx, two.Cookie.Token, "rules_two", id); !errors.Is(err, ErrNotificationRuleNotFound) {
		t.Fatal("foreign rule deleted", err)
	}
	for _, raw := range []string{"", "0", "01", "-1", "x"} {
		if _, err := f.service.DeleteNotificationRule(ctx, one.Cookie.Token, "rules_one", raw); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("id %q accepted: %v", raw, err)
		}
	}
	view, err = f.service.UpdateNotificationRule(ctx, one.Cookie.Token, "rules_one", id, notificationRule(NotificationKindWatchlist, NotificationScopePreferred, nil, false, false))
	if err != nil || view.Rules[0].TheaterScope != NotificationScopePreferred || len(view.Rules[0].TheaterIDs) != 0 || view.Rules[0].Enabled {
		t.Fatalf("update %+v %v", view, err)
	}
	for range 19 {
		if _, err = f.service.CreateNotificationRule(ctx, one.Cookie.Token, "rules_one", notificationRule(NotificationKindActivity, NotificationScopeFollowed, nil, true, true)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = f.service.CreateNotificationRule(ctx, one.Cookie.Token, "rules_one", notificationRule(NotificationKindActivity, NotificationScopeFollowed, nil, true, true)); !errors.Is(err, ErrNotificationRuleLimit) {
		t.Fatal("rule limit", err)
	}
	if view, err = f.service.DeleteNotificationRule(ctx, one.Cookie.Token, "rules_one", id); err != nil || len(view.Rules) != 19 {
		t.Fatalf("delete %d %v", len(view.Rules), err)
	}
	if view, err = f.service.NotificationRules(ctx, two.Cookie.Token); err != nil || len(view.Rules) != 0 {
		t.Fatal("rules leaked across accounts", err)
	}
	var authRevision int64
	if err = f.pool.QueryRow(ctx, `SELECT auth_revision FROM accounts WHERE email='rules@example.com'`).Scan(&authRevision); err != nil || authRevision != initialAuthRevision {
		t.Fatal("rule writes changed authority", authRevision, err)
	}
}

func TestNotificationSweepIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	seedFollowedActivity(t, f)
	ctx := t.Context()
	a := f.complete(t, "inbox@example.com", "inbox_owner")
	b := f.complete(t, "inbox-other@example.com", "inbox_other")
	token := a.Cookie.Token
	if _, err := f.service.SaveTheaterFollow(ctx, token, "inbox_owner", "0", "ugc-1", true); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.SaveTheaterPreferences(ctx, token, "inbox_owner", "0", []string{"ugc-2"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.SaveWatchlist(ctx, token, "inbox_owner", "0", "film-2", true); err != nil {
		t.Fatal(err)
	}
	// B follows the same cinema without any rule: following alone never notifies.
	if _, err := f.service.SaveTheaterFollow(ctx, b.Cookie.Token, "inbox_other", "0", "ugc-1", true); err != nil {
		t.Fatal(err)
	}
	rules, err := f.service.CreateNotificationRule(ctx, token, "inbox_owner", notificationRule(NotificationKindActivity, NotificationScopeFollowed, nil, true, true))
	if err != nil {
		t.Fatal(err)
	}
	followedRule := rules.Rules[0].ID
	if rules, err = f.service.CreateNotificationRule(ctx, token, "inbox_owner", notificationRule(NotificationKindActivity, NotificationScopePreferred, nil, false, true)); err != nil {
		t.Fatal(err)
	}
	preferredRule := rules.Rules[1].ID
	if rules, err = f.service.CreateNotificationRule(ctx, token, "inbox_owner", notificationRule(NotificationKindWatchlist, NotificationScopeCustom, []string{"ugc-3"}, false, true)); err != nil {
		t.Fatal(err)
	}
	watchRule := rules.Rules[2].ID

	// Seeded events 1-7 predate every rule's arming position: silent catch-up.
	if r := sweepNotifications(t, f); r.Events != 7 || r.Notifications != 0 || notificationCursor(t, f) != 7 {
		t.Fatalf("catch-up %+v cursor %d", r, notificationCursor(t, f))
	}
	notificationEpisode(t, f, 8, "ugc-1", "101", "added_to_program")
	notificationEpisode(t, f, 9, "ugc-1", "102", "return_to_program")
	notificationEpisode(t, f, 10, "ugc-2", "102", "added_to_program")
	notificationEpisode(t, f, 11, "ugc-2", "102", "return_to_program") // preferred rule excludes returns
	notificationEpisode(t, f, 12, "ugc-3", "101", "added_to_program")
	notificationEpisode(t, f, 13, "ugc-3", "102", "added_to_program")
	followedActivityExec(t, f, `UPDATE cinema_activity_episodes SET superseded_by_id=12 WHERE id=13`)
	notificationEpisode(t, f, 14, "ugc-1", "101", "baseline")
	if r := sweepNotifications(t, f); r.Events != 7 || r.Notifications != 3 || notificationCursor(t, f) != 14 {
		t.Fatalf("delivery %+v", r)
	}
	inbox, err := f.service.Notifications(ctx, token, 0, "")
	if err != nil || inbox.UnreadCount != 3 || len(inbox.Items) != 3 || inbox.NextCursor != nil || inbox.Limit != 20 || inbox.Username != "inbox_owner" {
		t.Fatalf("inbox %+v %v", inbox, err)
	}
	byRule := map[string]NotificationView{}
	for _, item := range inbox.Items {
		if item.RuleID == nil || item.Read {
			t.Fatalf("item %+v", item)
		}
		byRule[*item.RuleID] = item
	}
	followed := byRule[followedRule]
	// The redirected source and canonical source collapse to one current film.
	if followed.Kind != NotificationKindActivity || followed.EventCount != 2 || followed.MovieCount != 1 || followed.TheaterCount != 1 ||
		len(followed.Movies) != 1 || followed.Movies[0].Slug != "film-2" || followed.Movies[0].Title != "Current override" ||
		len(followed.Theaters) != 1 || followed.Theaters[0].ID != "ugc-1" || followed.Theaters[0].Name != "Active 1" {
		t.Fatalf("followed %+v", followed)
	}
	if p := byRule[preferredRule]; p.EventCount != 1 || p.Theaters[0].ID != "ugc-2" || p.Theaters[0].Name != "Retained 2" {
		t.Fatalf("preferred %+v", p)
	}
	if w := byRule[watchRule]; w.Kind != NotificationKindWatchlist || w.EventCount != 1 || w.Movies[0].Slug != "film-2" || w.Theaters[0].ID != "ugc-3" {
		t.Fatalf("watchlist %+v", w)
	}
	if other, err := f.service.Notifications(ctx, b.Cookie.Token, 0, ""); err != nil || other.UnreadCount != 0 || len(other.Items) != 0 {
		t.Fatal("notifications leaked", other, err)
	}

	// A later unread batch merges into a new entry; a watchlist film notifies once.
	notificationEpisode(t, f, 15, "ugc-1", "102", "added_to_program")
	notificationEpisode(t, f, 16, "ugc-3", "102", "added_to_program")
	if r := sweepNotifications(t, f); r.Notifications != 1 {
		t.Fatalf("merge %+v", r)
	}
	inbox, err = f.service.Notifications(ctx, token, 0, "")
	if err != nil || len(inbox.Items) != 3 || inbox.UnreadCount != 3 || *inbox.Items[0].RuleID != followedRule || inbox.Items[0].EventCount != 3 || inbox.Items[0].ID == followed.ID {
		t.Fatalf("merged %+v %v", inbox, err)
	}
	merged := inbox.Items[0].ID
	if status, err := f.service.MarkNotificationsRead(ctx, b.Cookie.Token, "inbox_other", []string{merged}); err != nil || status.UnreadCount != 0 {
		t.Fatal("foreign read", status, err)
	}
	if _, err := f.service.MarkNotificationsRead(ctx, token, "inbox_other", []string{merged}); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("client chose owner", err)
	}
	status, err := f.service.MarkNotificationsRead(ctx, token, "inbox_owner", []string{merged, "999999"})
	if err != nil || status.UnreadCount != 2 || status.LatestID != merged {
		t.Fatalf("read %+v %v", status, err)
	}
	// Read entries are never merged into.
	notificationEpisode(t, f, 17, "ugc-1", "101", "added_to_program")
	sweepNotifications(t, f)
	inbox, err = f.service.Notifications(ctx, token, 2, "")
	if err != nil || inbox.UnreadCount != 3 || len(inbox.Items) != 2 || inbox.NextCursor == nil || inbox.Items[0].EventCount != 1 || inbox.Items[0].Read || inbox.Items[1].ID != merged || !inbox.Items[1].Read {
		t.Fatalf("after read %+v %v", inbox, err)
	}
	page, err := f.service.Notifications(ctx, token, 2, *inbox.NextCursor)
	if err != nil || len(page.Items) != 2 || page.NextCursor != nil || page.Items[0].Read || page.Items[1].Read || len(page.Items[0].ID) > len(merged) || page.Items[0].ID >= merged {
		t.Fatalf("page %+v %v", page, err)
	}
	if status, err = f.service.MarkAllNotificationsRead(ctx, token, "inbox_owner", inbox.Items[1].ID); err != nil || status.UnreadCount != 1 {
		t.Fatalf("read-all bounded %+v %v", status, err)
	}
	if status, err = f.service.MarkAllNotificationsRead(ctx, token, "inbox_owner", inbox.LatestID); err != nil || status.UnreadCount != 0 {
		t.Fatalf("read-all %+v %v", status, err)
	}

	// Disabled rules skip events; re-enabling never replays them.
	if _, err = f.service.UpdateNotificationRule(ctx, token, "inbox_owner", followedRule, notificationRule(NotificationKindActivity, NotificationScopeFollowed, nil, true, false)); err != nil {
		t.Fatal(err)
	}
	notificationEpisode(t, f, 18, "ugc-1", "102", "added_to_program")
	sweepNotifications(t, f)
	notificationEpisode(t, f, 19, "ugc-1", "102", "added_to_program")
	if _, err = f.service.UpdateNotificationRule(ctx, token, "inbox_owner", followedRule, notificationRule(NotificationKindActivity, NotificationScopeFollowed, nil, true, true)); err != nil {
		t.Fatal(err)
	}
	if r := sweepNotifications(t, f); r.Notifications != 0 {
		t.Fatalf("re-enabled rule replayed %+v", r)
	}

	// Publication holds the writer lock: the sweep yields without progress.
	notificationEpisode(t, f, 20, "ugc-1", "101", "added_to_program")
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, notificationJournalWriterLock); err != nil {
		t.Fatal(err)
	}
	if r, err := f.service.SweepNotifications(ctx); err != nil || !r.Busy || notificationCursor(t, f) != 19 {
		t.Fatalf("busy %+v %v", r, err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if r := sweepNotifications(t, f); r.Notifications != 1 || notificationCursor(t, f) != 20 {
		t.Fatalf("after publication %+v", r)
	}

	// Rule deletion keeps delivered entries; account deletion cascades them.
	if _, err = f.service.DeleteNotificationRule(ctx, token, "inbox_owner", watchRule); err != nil {
		t.Fatal(err)
	}
	inbox, err = f.service.Notifications(ctx, token, 50, "")
	if err != nil || len(inbox.Items) != 5 {
		t.Fatalf("after delete %+v %v", inbox, err)
	}
	orphans := 0
	for _, item := range inbox.Items {
		if item.RuleID == nil {
			orphans++
		}
	}
	if orphans != 1 {
		t.Fatal("deleted rule reference", orphans)
	}
	f.advance(notificationRetention + time.Hour)
	if r := sweepNotifications(t, f); r.Expired != 5 {
		t.Fatalf("retention %+v", r)
	}
	var remaining int
	if err = f.pool.QueryRow(ctx, `SELECT count(*) FROM account_notifications`).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatal("retention left rows", remaining, err)
	}
}

func TestNotificationInboxCapIntegration(t *testing.T) {
	f := newLifecycleFixture(t)
	seedFollowedActivity(t, f)
	ctx := context.Background()
	a := f.complete(t, "cap@example.com", "cap_owner")
	if _, err := f.service.SaveTheaterFollow(ctx, a.Cookie.Token, "cap_owner", "0", "ugc-1", true); err != nil {
		t.Fatal(err)
	}
	rules, err := f.service.CreateNotificationRule(ctx, a.Cookie.Token, "cap_owner", notificationRule(NotificationKindActivity, NotificationScopeFollowed, nil, false, true))
	if err != nil {
		t.Fatal(err)
	}
	sweepNotifications(t, f)
	ruleID, _ := strconv.ParseInt(rules.Rules[0].ID, 10, 64)
	accountID := int64(0)
	if err = f.pool.QueryRow(ctx, `SELECT id FROM accounts WHERE email='cap@example.com'`).Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	followedActivityExec(t, f, `INSERT INTO account_notifications(account_id,rule_id,kind,movie_ids,theater_ids,event_count,last_event_id,created_at,read_at)
 SELECT $1,$2,'cinema_activity',ARRAY[2::bigint],ARRAY['ugc-1'],1,1,$3,$3 FROM generate_series(1,$4)`, accountID, ruleID, f.now(), maxStoredNotifications+5)
	notificationEpisode(t, f, 8, "ugc-1", "101", "added_to_program")
	sweepNotifications(t, f)
	var count int
	if err = f.pool.QueryRow(ctx, `SELECT count(*) FROM account_notifications WHERE account_id=$1`, accountID).Scan(&count); err != nil || count != maxStoredNotifications {
		t.Fatal("cap", count, err)
	}
	status, err := f.service.NotificationStatus(ctx, a.Cookie.Token)
	if err != nil || status.UnreadCount != 1 {
		t.Fatal("newest entry evicted", status, err)
	}
}
