package schedule

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const ActivityReturnMinimumBreakDays = 28

type TheaterActivityQuery struct {
	Slug   string
	Limit  int
	Cursor string
}

type TheaterActivity struct {
	GeneratedAt time.Time        `json:"generated_at"`
	Timezone    string           `json:"timezone"`
	Theater     Theater          `json:"theater"`
	Coverage    ActivityCoverage `json:"coverage"`
	Items       []ActivityEvent  `json:"items"`
	Limit       int              `json:"limit"`
	NextCursor  *string          `json:"next_cursor"`
}

type ActivityCoverage struct {
	HistoryStartedAt       *time.Time `json:"history_started_at"`
	LastPublicationAt      *time.Time `json:"last_publication_at"`
	SourceGeneratedAt      *time.Time `json:"source_generated_at"`
	Completeness           string     `json:"completeness"`
	Bootstrap              string     `json:"bootstrap"`
	ReturnMinimumBreakDays int        `json:"return_minimum_break_days"`
}

type ActivityEvent struct {
	EventID                string        `json:"event_id"`
	Type                   string        `json:"type"`
	DetectedAt             time.Time     `json:"detected_at"`
	FirstScreeningDate     string        `json:"first_screening_date"`
	PreviousProgramEndDate *string       `json:"previous_program_end_date"`
	Movie                  ActivityMovie `json:"movie"`
	HasUpcomingShowtimes   bool          `json:"has_upcoming_showtimes"`
	NextShowtimeDate       *string       `json:"next_showtime_date"`
}

type ActivityMovie struct {
	Slug      string    `json:"slug"`
	Title     string    `json:"title"`
	PosterURL *string   `json:"poster_url"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ActivityPosition struct {
	Version        int    `json:"v"`
	Slug           string `json:"theater"`
	UpperID        string `json:"upper_id"`
	LastDetectedAt string `json:"last_detected_at"`
	LastID         string `json:"last_id"`
}

var activitySlug = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

func InvalidActivityQuery() error {
	return &ValidationError{Message: "Les paramètres d’activité sont invalides."}
}

func NormalizeTheaterActivityQuery(q TheaterActivityQuery) (TheaterActivityQuery, error) {
	if !activitySlug.MatchString(q.Slug) || q.Limit < 0 || q.Limit > 100 || len(q.Cursor) > 1024 || !utf8.ValidString(q.Cursor) {
		return q, InvalidActivityQuery()
	}
	if q.Limit == 0 {
		q.Limit = 20
	}
	if q.Cursor != "" {
		if _, err := DecodeActivityCursor(q.Cursor, q.Slug); err != nil {
			return q, err
		}
	}
	return q, nil
}

func ActivityID(raw string) (int64, error) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != raw {
		return 0, InvalidActivityQuery()
	}
	return id, nil
}

func DecodeActivityCursor(raw, slug string) (ActivityPosition, error) {
	var p ActivityPosition
	if len(raw) == 0 || len(raw) > 1024 || strings.ContainsAny(raw, "=\r\n") {
		return p, InvalidActivityQuery()
	}
	data, err := base64.RawURLEncoding.Strict().DecodeString(raw)
	if err != nil || !utf8.Valid(data) {
		return p, InvalidActivityQuery()
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	// JSON duplicate fields are ambiguous cursor positions, even when values agree.
	if err := uniqueActivityCursorFields(data); err != nil {
		return p, err
	}
	if err = d.Decode(&p); err != nil {
		return p, InvalidActivityQuery()
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return p, InvalidActivityQuery()
	}
	upper, e1 := ActivityID(p.UpperID)
	last, e2 := ActivityID(p.LastID)
	at, e3 := time.Parse(time.RFC3339Nano, p.LastDetectedAt)
	if p.Version != 1 || p.Slug != slug || !activitySlug.MatchString(p.Slug) || e1 != nil || e2 != nil || last > upper || e3 != nil || at.IsZero() || !strings.HasSuffix(p.LastDetectedAt, "Z") || at.Nanosecond()%1000 != 0 || at.UTC().Format(time.RFC3339Nano) != p.LastDetectedAt {
		return p, InvalidActivityQuery()
	}
	return p, nil
}

func uniqueActivityCursorFields(data []byte) error {
	d := json.NewDecoder(bytes.NewReader(data))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return InvalidActivityQuery()
	}
	seen := map[string]bool{}
	for d.More() {
		token, err = d.Token()
		key, ok := token.(string)
		if err != nil || !ok || seen[key] {
			return InvalidActivityQuery()
		}
		switch key {
		case "v", "theater", "upper_id", "last_detected_at", "last_id":
		default:
			return InvalidActivityQuery()
		}
		seen[key] = true
		var value json.RawMessage
		if err = d.Decode(&value); err != nil {
			return InvalidActivityQuery()
		}
	}
	if len(seen) != 5 {
		return InvalidActivityQuery()
	}
	return nil
}

func EncodeActivityCursor(slug string, upperID int64, event ActivityEvent) string {
	p := ActivityPosition{Version: 1, Slug: slug, UpperID: strconv.FormatInt(upperID, 10), LastDetectedAt: event.DetectedAt.UTC().Format(time.RFC3339Nano), LastID: event.EventID}
	data, _ := json.Marshal(p)
	return base64.RawURLEncoding.EncodeToString(data)
}
