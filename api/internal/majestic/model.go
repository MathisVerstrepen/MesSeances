package majestic

import (
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
)

var errShape = errors.New("invalid Majestic response structure")
var decimal = regexp.MustCompile(`^[0-9]+$`)
var globalID = regexp.MustCompile(`^[A-Z0-9]{5}$`)
var localID = regexp.MustCompile(`^emsx[0-9]{4}HC[0-9]+$`)
var dateID = regexp.MustCompile(`^[0-9]{12}$`)

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

var sites = [...]string{"EMS0700", "EMS0654", "EMS1333", "EMS1212", "EMS1299", "EMS1211", "EMS0957"}

type program struct {
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
