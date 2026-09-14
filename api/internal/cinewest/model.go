package cinewest

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

var errShape = errors.New("invalid Cinewest response structure")

type sourceID string

func (id *sourceID) UnmarshalJSON(raw []byte) error {
	value := string(raw)
	if len(raw) > 0 && raw[0] == '"' {
		if json.Unmarshal(raw, &value) != nil {
			return errShape
		}
	} else if _, err := strconv.ParseUint(value, 10, 64); err != nil {
		return errShape
	}
	if value == "" || len(value) > 2048 || strings.ContainsRune(value, 0) {
		return errShape
	}
	*id = sourceID(value)
	return nil
}

func numericID(id sourceID) bool {
	value := string(id)
	if value == "" || len(value) > 114 || value[0] == '0' {
		return false
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

type minutes int

func (m *minutes) UnmarshalJSON(raw []byte) error {
	value := string(raw)
	if value == "null" {
		*m = 0
		return nil
	}
	if len(raw) > 0 && raw[0] == '"' {
		if json.Unmarshal(raw, &value) != nil {
			return errShape
		}
	}
	n, err := strconv.ParseUint(value, 10, 31)
	if err != nil {
		return errShape
	}
	*m = minutes(n)
	return nil
}
func clean(s string) string { return strings.Join(strings.Fields(s), " ") }

type officeCinema struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Address  string `json:"address"`
	City     string `json:"city"`
	Postcode string `json:"postcode"`
	Token    string `json:"apiToken"`
}
type officeMovie struct {
	ID       sourceID `json:"id"`
	CinemaID string   `json:"cinemaid"`
	Title    string   `json:"title"`
	Duration minutes  `json:"duration"`
	Poster   string   `json:"posterpath"`
}
type officeScreen struct {
	ID       sourceID `json:"id"`
	CinemaID string   `json:"cinemaid"`
	Label    string   `json:"screenlabel"`
	Number   int      `json:"screennumber"`
}
type officeOption struct {
	ID       sourceID `json:"id"`
	CinemaID string   `json:"cinemaid"`
	Label    string   `json:"label"`
}
type officeRef struct {
	ID sourceID `json:"id"`
}
type officeShow struct {
	ID       sourceID  `json:"id"`
	CinemaID string    `json:"cinemaid"`
	Movie    officeRef `json:"mediaid"`
	Screen   officeRef `json:"screenid"`
	Start    string    `json:"showtime"`
	End      string    `json:"showend"`
	ThreeD   bool      `json:"threed"`
	Language sourceID  `json:"languagemediaoptionsid"`
	Format   sourceID  `json:"formatmediaoptionsid"`
	Options  []struct {
		Option officeRef `json:"mediaoptionsid"`
	} `json:"showsMediaoptionsCollection"`
}
type ticketProgram struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Address string `json:"address"`
	City    string `json:"city"`
	Zip     string `json:"zip_code"`
	EMS     struct {
		ID string `json:"id"`
	} `json:"ems"`
	Events []ticketEvent `json:"events"`
}
type ticketEvent struct {
	ID       string          `json:"id"`
	Title    string          `json:"title"`
	Duration minutes         `json:"duration"`
	Poster   string          `json:"bill_url"`
	Sessions []ticketSession `json:"sessions"`
}
type ticketSession struct {
	ID        string   `json:"id"`
	Date      string   `json:"date"`
	Room      string   `json:"hall_name"`
	Version   string   `json:"version"`
	Features  []string `json:"features"`
	Formats   string   `json:"formats"`
	FirstPart minutes  `json:"first_part_duration"`
	Booking   string   `json:"booking_url"`
}
type webTheater struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Timezone      string `json:"timeZone"`
	PracticalInfo struct {
		Location struct {
			Address string `json:"address"`
			City    string `json:"city"`
			Zip     string `json:"zip"`
		} `json:"location"`
	} `json:"practicalInfo"`
}
type webShow struct {
	ID     string   `json:"id"`
	Start  string   `json:"startsAt"`
	Tags   []string `json:"tags"`
	Screen *struct {
		Name string `json:"name"`
	} `json:"screen"`
	Data struct {
		Ticketing []struct {
			URLs []string `json:"urls"`
		} `json:"ticketing"`
	} `json:"data"`
}
type webMovie struct {
	ID      sourceID `json:"id"`
	Title   string   `json:"title"`
	Runtime minutes  `json:"runtime"`
	Poster  string   `json:"poster"`
}
