package grandecran

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

type sourceID string

func (id *sourceID) UnmarshalJSON(body []byte) error {
	if len(body) > 0 && body[0] == '"' {
		var s string
		if json.Unmarshal(body, &s) != nil {
			return fmt.Errorf("invalid identifier")
		}
		*id = sourceID(s)
		return nil
	}
	var n json.Number
	d := json.NewDecoder(bytes.NewReader(body))
	d.UseNumber()
	if d.Decode(&n) != nil || n == "" {
		return fmt.Errorf("invalid identifier")
	}
	*id = sourceID(n.String())
	return nil
}

type genreList []string

func (g *genreList) UnmarshalJSON(body []byte) error {
	if bytes.Equal(bytes.TrimSpace(body), []byte("null")) {
		*g = nil
		return nil
	}
	var s string
	if json.Unmarshal(body, &s) == nil {
		*g = strings.Split(s, ",")
		return nil
	}
	var list []string
	if json.Unmarshal(body, &list) != nil {
		return fmt.Errorf("invalid genres")
	}
	*g = list
	return nil
}

type cinema struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	TimeZone      string `json:"timeZone"`
	PracticalInfo struct {
		Location struct{ Address, City, Zip string } `json:"location"`
	} `json:"practicalInfo"`
}
type programResponse struct {
	MovieIDs      struct{ TitleAsc, ReleaseAsc []sourceID } `json:"movieIds"`
	ScheduledDays map[string][]string                       `json:"scheduledDays"`
}
type movieResponse struct {
	ID          sourceID  `json:"id"`
	Title       string    `json:"title"`
	Runtime     *int      `json:"runtime"`
	Poster      string    `json:"poster"`
	Genres      genreList `json:"genres"`
	Synopsis    string    `json:"synopsis"`
	ReleaseDate string    `json:"releaseDate"`
}
type showtimeResponse struct {
	ID       string   `json:"id"`
	StartsAt string   `json:"startsAt"`
	Tags     []string `json:"tags"`
	Screen   *struct {
		Name string `json:"name"`
	} `json:"screen"`
	Data struct {
		Ticketing []struct {
			Provider, Type string
			URLs           []string `json:"urls"`
		} `json:"ticketing"`
	} `json:"data"`
}
type scheduleResponse map[string]struct {
	Schedule map[string]map[string][]showtimeResponse `json:"schedule"`
}
