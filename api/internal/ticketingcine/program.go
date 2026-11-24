// Package ticketingcine reads Monnaie Services TicketingCiné programmes
// (ws.ticketingcine.com get_prog). It holds the wire model and helpers shared
// by the Majestic, Megarama and generic TicketingCiné connectors, and the
// generic connector itself, whose cinemas come from sync sources.
package ticketingcine

import (
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"messeances/api/internal/schedule"
)

// ProgramURL is the JSON-RPC endpoint serving every site's programme.
const ProgramURL = "https://ws.ticketingcine.com/site"

// MaxResponseBytes bounds one programme response.
const MaxResponseBytes = 16 << 20

var (
	// ErrShape reports a programme that does not match the expected model.
	ErrShape = errors.New("invalid TicketingCiné response structure")
	// ErrFieldTypes reports a programme whose JSON field types are invalid.
	ErrFieldTypes = errors.New("invalid TicketingCiné programme field types")
	// ErrEnvelope reports an invalid JSON-RPC envelope or missing schedule.
	ErrEnvelope = errors.New("invalid TicketingCiné programme envelope")
)

var (
	decimal = regexp.MustCompile(`^[0-9]+$`)
	// SiteID matches a TicketingCiné site (cinema) identifier.
	SiteID = regexp.MustCompile(`^EMS[0-9]{4}$`)
	// GlobalMovieID matches a national film identifier shared by all sites.
	GlobalMovieID = regexp.MustCompile(`^[A-Z0-9]{5}$`)
	// LocalMovieID matches a site-scoped event identifier.
	LocalMovieID = regexp.MustCompile(`^emsx[0-9]{4}HC[0-9]+$`)
	// SessionID matches a session identifier; digits 5–8 are the site number.
	SessionID = regexp.MustCompile(`^emsx[0-9]{12}$`)
	// DateID matches a session start in local YYYYMMDDhhmm form.
	DateID = regexp.MustCompile(`^[0-9]{12}$`)
)

// Minutes accepts the provider's integer and decimal-string representations,
// with null interpreted as zero (unknown runtime or no first part).
type Minutes int

func (m *Minutes) UnmarshalJSON(raw []byte) error {
	if string(raw) == "null" {
		*m = 0
		return nil
	}
	value := string(raw)
	if len(raw) > 0 && raw[0] == '"' {
		if json.Unmarshal(raw, &value) != nil {
			return ErrShape
		}
	}
	if !decimal.MatchString(value) {
		return ErrShape
	}
	n, err := strconv.ParseUint(value, 10, 31)
	if err != nil {
		return ErrShape
	}
	*m = Minutes(n)
	return nil
}

// Program is one site's programme.
type Program struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Address string `json:"address"`
	City    string `json:"city"`
	ZipCode string `json:"zip_code"`
	EMS     struct {
		ID string `json:"id"`
	} `json:"ems"`
	Events []Event `json:"events"`
}

// Event is one film or local event in a programme.
type Event struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Duration Minutes   `json:"duration"`
	BillURL  string    `json:"bill_url"`
	Sessions []Session `json:"sessions"`
}

// Session is one screening of an event.
type Session struct {
	ID                string   `json:"id"`
	Date              string   `json:"date"`
	HallName          string   `json:"hall_name"`
	Version           string   `json:"version"`
	Features          []string `json:"features"`
	Formats           string   `json:"formats"`
	FirstPartDuration Minutes  `json:"first_part_duration"`
	BookingURL        string   `json:"booking_url"`
}

// ProgramRequestBody is the JSON-RPC get_prog request for site.
func ProgramRequestBody(site string) []byte {
	body, _ := json.Marshal(struct {
		JSONRPC string            `json:"jsonrpc"`
		Method  string            `json:"method"`
		Params  map[string]string `json:"params"`
		ID      int               `json:"id"`
	}{"2.0", "get_prog", map[string]string{"site_id": site}, 1})
	return body
}

// DecodeProgram unwraps a get_prog response. It reports ErrFieldTypes or
// ErrEnvelope; callers validate the programme content.
func DecodeProgram(body []byte) (Program, error) {
	var response struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      int             `json:"id"`
		Error   json.RawMessage `json:"error"`
		Result  *struct {
			Schedule *Program `json:"schedule"`
		} `json:"result"`
	}
	if json.Unmarshal(body, &response) != nil {
		return Program{}, ErrFieldTypes
	}
	if response.JSONRPC != "2.0" || response.ID != 1 || len(response.Error) != 0 || response.Result == nil || response.Result.Schedule == nil {
		return Program{}, ErrEnvelope
	}
	return *response.Result.Schedule, nil
}

// ParseStart parses a local YYYYMMDDhhmm start. Paris folds differ by one
// hour, so ambiguous or skipped local times are rejected rather than guessed.
func ParseStart(value string, location *time.Location) (time.Time, bool) {
	if !DateID.MatchString(value) {
		return time.Time{}, false
	}
	start, err := time.ParseInLocation("200601021504", value, location)
	if err != nil || start.Format("200601021504") != value || start.Year() < 1 {
		return time.Time{}, false
	}
	for _, shift := range []time.Duration{-time.Hour, time.Hour} {
		if start.Add(shift).In(location).Format("200601021504") == value {
			return time.Time{}, false
		}
	}
	return start, true
}

// PosterURL returns the poster bound to event id, upgraded to the 600px
// national poster for global films, or "" when raw is not that film's poster.
func PosterURL(raw, id string) string {
	if !schedule.ValidMegaramaPosterURL(raw) {
		return ""
	}
	u, _ := url.Parse(raw)
	if GlobalMovieID.MatchString(id) {
		if !strings.Contains(u.Path, "/FR"+id+"/") {
			return ""
		}
		return strings.Replace(raw, "/movie_poster/120/", "/movie_poster/600/", 1)
	}
	if LocalMovieID.MatchString(id) && u.Path == "/ems_spectacle/120/"+id[4:8]+"/"+id[8:]+".jpg" {
		return raw
	}
	return ""
}

// Clean collapses whitespace.
func Clean(value string) string { return strings.Join(strings.Fields(value), " ") }

// SiteNumber returns the four digits of an EMSdddd site.
func SiteNumber(site string) string { return strings.TrimPrefix(site, "EMS") }
