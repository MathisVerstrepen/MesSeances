package megarama

import (
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
)

var errShape = errors.New("invalid Megarama response structure")

// minutes accepts the provider's integer and decimal-string representations,
// with null/missing interpreted as unknown (or zero for first-part duration).
type minutes int

func (m *minutes) UnmarshalJSON(raw []byte) error {
	if string(raw) == "null" {
		*m = 0
		return nil
	}
	value := string(raw)
	if len(raw) > 0 && raw[0] == '"' {
		if json.Unmarshal(raw, &value) != nil {
			return errShape
		}
	}
	if !decimal.MatchString(value) {
		return errShape
	}
	n, err := strconv.ParseUint(value, 10, 31)
	if err != nil {
		return errShape
	}
	*m = minutes(n)
	return nil
}

var (
	decimal          = regexp.MustCompile(`^[0-9]+$`)
	globalID         = regexp.MustCompile(`^[A-Z0-9]{5}$`)
	localID          = regexp.MustCompile(`^emsx[0-9]{4}HC[0-9]+$`)
	sessionID        = regexp.MustCompile(`^emsx[0-9]{12}$`)
	dateID           = regexp.MustCompile(`^[0-9]{12}$`)
	configAssignment = regexp.MustCompile(`\bgl_config\s*=\s*`)
)

type cinema struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Address struct {
		Street  string `json:"street"`
		City    string `json:"city"`
		ZipCode string `json:"zip_code"`
	} `json:"address"`
	ProgramID string `json:"prog_id"`
	Timezone  string `json:"time_zone"`
	Website   string `json:"website_url"`
}

type program struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Address string `json:"address"`
	City    string `json:"city"`
	ZipCode string `json:"zip_code"`
	EMS     struct {
		ID string `json:"id"`
	} `json:"ems"`
	Events []event `json:"events"`
}

type event struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Duration minutes   `json:"duration"`
	BillURL  string    `json:"bill_url"`
	Sessions []session `json:"sessions"`
}

type session struct {
	ID                string   `json:"id"`
	Date              string   `json:"date"`
	HallName          string   `json:"hall_name"`
	Version           string   `json:"version"`
	Features          []string `json:"features"`
	Formats           string   `json:"formats"`
	FirstPartDuration minutes  `json:"first_part_duration"`
	BookingURL        string   `json:"booking_url"`
}
