package accounts

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"messeances/api/internal/schedule"
)

// Database timestamp locations are driver representation, not journal data.
// Normalize only locations, retaining exact instants and every other field.
func sameFollowedActivityProjection(public, followed schedule.ActivityEvent) bool {
	public.DetectedAt = public.DetectedAt.UTC()
	public.Movie.UpdatedAt = public.Movie.UpdatedAt.UTC()
	followed.DetectedAt = followed.DetectedAt.UTC()
	followed.Movie.UpdatedAt = followed.Movie.UpdatedAt.UTC()
	return reflect.DeepEqual(public, followed)
}

func TestFollowedActivityProjectionComparison(t *testing.T) {
	want := schedule.ActivityEvent{
		EventID: "5", Type: "added_to_program", FirstScreeningDate: "2026-10-03",
		DetectedAt: time.Date(2026, 10, 2, 12, 0, 0, 123456000, time.UTC),
		Movie:      schedule.ActivityMovie{Slug: "film-2", Title: "Current override", UpdatedAt: time.Date(2026, 10, 2, 12, 1, 0, 654321000, time.UTC)},
	}
	public := want
	m := pgtype.NewMap()
	for _, timestamp := range []struct {
		value       time.Time
		destination *time.Time
	}{
		{want.DetectedAt, &public.DetectedAt},
		{want.Movie.UpdatedAt, &public.Movie.UpdatedAt},
	} {
		encoded, err := m.Encode(pgtype.TimestamptzOID, pgx.BinaryFormatCode, timestamp.value, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := m.Scan(pgtype.TimestamptzOID, pgx.BinaryFormatCode, encoded, timestamp.destination); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("public-style pgx decoded detected_at=%s location=%s; followed detected_at=%s location=%s", public.DetectedAt.Format(time.RFC3339Nano), public.DetectedAt.Location(), want.DetectedAt.Format(time.RFC3339Nano), want.DetectedAt.Location())
	t.Logf("public-style pgx decoded movie.updated_at=%s location=%s; followed movie.updated_at=%s location=%s", public.Movie.UpdatedAt.Format(time.RFC3339Nano), public.Movie.UpdatedAt.Location(), want.Movie.UpdatedAt.Format(time.RFC3339Nano), want.Movie.UpdatedAt.Location())
	if !public.DetectedAt.Equal(want.DetectedAt) || !public.Movie.UpdatedAt.Equal(want.Movie.UpdatedAt) || !sameFollowedActivityProjection(public, want) {
		t.Fatal("driver locations changed the journal projection")
	}
	// This deterministic non-UTC representation also exercises UTC-host runners.
	local := want
	local.DetectedAt = local.DetectedAt.In(time.FixedZone("Europe/Paris", 2*60*60))
	local.Movie.UpdatedAt = local.Movie.UpdatedAt.In(time.FixedZone("Europe/Paris", 2*60*60))
	if reflect.DeepEqual(local, want) || !sameFollowedActivityProjection(local, want) {
		t.Fatal("comparison still depends on time location")
	}
	for _, change := range []func(*schedule.ActivityEvent){
		func(e *schedule.ActivityEvent) { e.DetectedAt = e.DetectedAt.Add(time.Microsecond) },
		func(e *schedule.ActivityEvent) { e.Movie.UpdatedAt = e.Movie.UpdatedAt.Add(time.Microsecond) },
		func(e *schedule.ActivityEvent) { e.Movie.Title = "Different" },
		func(e *schedule.ActivityEvent) { e.EventID = "6" },
		func(e *schedule.ActivityEvent) { e.HasUpcomingShowtimes = true },
	} {
		changed := local
		change(&changed)
		if sameFollowedActivityProjection(changed, want) {
			t.Fatal("comparison accepted actual projection drift")
		}
	}
}

func TestFollowedActivityCursor(t *testing.T) {
	s := &Service{hmacKey: []byte(strings.Repeat("h", 32))}
	e := schedule.ActivityEvent{EventID: "9", DetectedAt: time.Date(2026, 10, 3, 12, 0, 0, 123456000, time.UTC)}
	raw := s.encodeFollowedActivityCursor(12, "4", 10, e)
	p, err := s.decodeFollowedActivityCursor(raw, 12)
	if err != nil || p.FollowsRevision != "4" || p.LastID != "9" || p.UpperID != "10" || p.LastDetectedAt != "2026-10-03T12:00:00.123456Z" {
		t.Fatalf("round trip: %+v %v", p, err)
	}
	if _, err = s.decodeFollowedActivityCursor(raw, 13); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("foreign owner accepted")
	}
	if strings.Contains(raw, "=") || len(raw) > 1024 {
		t.Fatal("cursor framing")
	}
	parts := strings.Split(raw, ".")
	for _, bad := range []string{"", raw + "=", raw + "\n", raw + ".x", parts[0] + "." + parts[1][1:], "!" + raw, strings.Repeat("a", 1025), parts[0] + ".AA"} {
		if _, err := s.decodeFollowedActivityCursor(bad, 12); !errors.Is(err, ErrInvalidInput) {
			t.Fatal("malformed cursor accepted")
		}
	}
	base, _ := json.Marshal(p)
	sign := func(payload string) string {
		return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + base64.RawURLEncoding.EncodeToString(s.activityMAC(12, []byte(payload)))
	}
	for _, payload := range []string{
		`{}`, `[]`, string(base) + ` {}`, strings.TrimSuffix(string(base), "}") + `,"extra":"x"}`,
		strings.TrimSuffix(string(base), "}") + `,"last_id":"9"}`,
		strings.TrimSuffix(string(base), "}") + `,"\u006cast_id":"9"}`,
		strings.Replace(string(base), `"v":1`, `"v":null`, 1), strings.Replace(string(base), `"v":1`, `"v":1.0`, 1),
		strings.Replace(string(base), `"v":1`, `"v":2`, 1), strings.Replace(string(base), `"v":1`, `"V":1`, 1),
		strings.Replace(string(base), `"last_id":"9"`, `"last_id":9`, 1),
		strings.Replace(string(base), `"upper_id":"10"`, `"upper_id":null`, 1),
		strings.Replace(string(base), `"follows_revision":"4"`, `"follows_revision":"04"`, 1),
		strings.Replace(string(base), `"follows_revision":"4"`, `"follows_revision":"9007199254740992"`, 1),
		strings.Replace(string(base), `"upper_id":"10"`, `"upper_id":"0"`, 1),
		strings.Replace(string(base), `"upper_id":"10"`, `"upper_id":"9223372036854775808"`, 1),
		strings.Replace(string(base), `"last_id":"9"`, `"last_id":"11"`, 1),
		strings.Replace(string(base), `"last_id":"9"`, `"last_id":"09"`, 1),
		strings.Replace(string(base), p.LastDetectedAt, "0001-01-01T00:00:00Z", 1),
		strings.Replace(string(base), p.LastDetectedAt, "2026-10-03T12:00:00.1234567Z", 1),
		strings.Replace(string(base), p.LastDetectedAt, "2026-10-03T12:00:00.1234560Z", 1),
		strings.Replace(string(base), p.LastDetectedAt, "2026-10-03T12:00:00.123456+00:00", 1),
		strings.Replace(string(base), p.LastDetectedAt, "not-time", 1),
	} {
		if _, err := s.decodeFollowedActivityCursor(sign(payload), 12); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid signed payload accepted: %s", payload)
		}
	}
	// A changed payload is never accepted using the old MAC, even if still valid JSON.
	changed := strings.Replace(string(base), `"last_id":"9"`, `"last_id":"8"`, 1)
	if _, err := s.decodeFollowedActivityCursor(base64.RawURLEncoding.EncodeToString([]byte(changed))+"."+parts[1], 12); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("tampered payload accepted")
	}
}

func TestFollowedActivityQuery(t *testing.T) {
	for _, q := range []FollowedActivityQuery{{Limit: -1}, {Limit: 101}, {Cursor: strings.Repeat("x", 1025)}, {Cursor: "\xff"}} {
		if _, err := NormalizeFollowedActivityQuery(q); !errors.Is(err, ErrInvalidInput) {
			t.Fatal("invalid query accepted")
		}
	}
	for _, limit := range []int{0, 1, 20, 100} {
		q, err := NormalizeFollowedActivityQuery(FollowedActivityQuery{Limit: limit})
		if err != nil || (limit == 0 && q.Limit != 20) {
			t.Fatal("valid query rejected")
		}
	}
}

type activityBlockingBeginner struct {
	entered chan struct{}
	release chan struct{}
	active  atomic.Int32
	maximum atomic.Int32
}

func (b *activityBlockingBeginner) BeginTx(ctx context.Context, _ pgx.TxOptions) (pgx.Tx, error) {
	n := b.active.Add(1)
	defer b.active.Add(-1)
	for previous := b.maximum.Load(); n > previous && !b.maximum.CompareAndSwap(previous, n); previous = b.maximum.Load() {
	}
	b.entered <- struct{}{}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-b.release:
		return nil, ErrUnavailable
	}
}

func TestFollowedActivityGateConcurrencyAndCancellation(t *testing.T) {
	b := &activityBlockingBeginner{entered: make(chan struct{}, 3), release: make(chan struct{})}
	s, err := NewService(NewPostgresStore(b), ServiceOptions{Hasher: &lifecycleHasher{}, Origin: "https://messeances.fr", AddressHMACKey: []byte(strings.Repeat("h", 32))})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 3)
	for range 2 {
		go func() {
			v, err := s.FollowedActivity(ctx, "", FollowedActivityQuery{})
			if v.Items != nil {
				done <- errors.New("tentative data escaped")
				return
			}
			done <- err
		}()
	}
	for range 2 {
		select {
		case <-b.entered:
		case <-time.After(time.Second):
			t.Fatal("gate did not admit")
		}
	}
	if _, err := s.FollowedActivity(t.Context(), "", FollowedActivityQuery{}); !errors.Is(err, ErrUnavailable) {
		t.Fatal("third request queued")
	}
	if b.active.Load() != 2 {
		t.Fatal("gate crossed pool admission")
	}
	cancel()
	for range 2 {
		if err := <-done; !errors.Is(err, ErrUnavailable) {
			t.Fatal(err)
		}
	}
	if len(s.followedActivityGate) != 0 || b.active.Load() != 0 {
		t.Fatal("cancellation leaked gate")
	}
	close(b.release)
	if _, err := s.FollowedActivity(t.Context(), "", FollowedActivityQuery{}); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if b.maximum.Load() != 2 || len(s.followedActivityGate) != 0 {
		t.Fatal("gate not reusable after failure")
	}
	if _, err := s.FollowedActivity(ctx, "", FollowedActivityQuery{}); !errors.Is(err, ErrUnavailable) {
		t.Fatal("cancelled request admitted")
	}
}
