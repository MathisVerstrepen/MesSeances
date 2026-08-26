package cgr

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

type sourceID string

func (id *sourceID) UnmarshalJSON(body []byte) error {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return fmt.Errorf("identifier is required")
	}
	if trimmed[0] == '"' {
		var value string
		if err := json.Unmarshal(trimmed, &value); err != nil {
			return err
		}
		*id = sourceID(value)
		return nil
	}
	var number json.Number
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	if err := decoder.Decode(&number); err != nil {
		return err
	}
	*id = sourceID(number.String())
	return nil
}

type genreList []string

func (genres *genreList) UnmarshalJSON(body []byte) error {
	trimmed := bytes.TrimSpace(body)
	if bytes.Equal(trimmed, []byte("null")) {
		*genres = nil
		return nil
	}
	if len(trimmed) > 0 && trimmed[0] == '"' {
		var value string
		if err := json.Unmarshal(trimmed, &value); err != nil {
			return err
		}
		if value == "" {
			*genres = genreList{}
			return nil
		}
		*genres = genreList(strings.Split(value, ","))
		return nil
	}
	var values []string
	if err := json.Unmarshal(trimmed, &values); err != nil {
		return err
	}
	*genres = genreList(values)
	return nil
}

type cinemaResponse struct {
	ID            string        `json:"id"`
	Name          string        `json:"name"`
	Path          string        `json:"path"`
	TimeZone      string        `json:"timeZone"`
	PracticalInfo practicalInfo `json:"practicalInfo"`
}

type practicalInfo struct {
	Location cinemaLocation `json:"location"`
}

type cinemaLocation struct {
	Address string `json:"address"`
	City    string `json:"city"`
	Zip     string `json:"zip"`
}

type scheduledMoviesResponse struct {
	MovieIDs      scheduledMovieIDs   `json:"movieIds"`
	ScheduledDays map[string][]string `json:"scheduledDays"`
}

type scheduledMovieIDs struct {
	ReleaseAsc  []sourceID `json:"releaseAsc"`
	ReleaseDesc []sourceID `json:"releaseDesc"`
	TitleAsc    []sourceID `json:"titleAsc"`
}

type scheduleResponse map[string]struct {
	Schedule map[string]map[string][]showtimeResponse `json:"schedule"`
}

type showtimeResponse struct {
	ID       string          `json:"id"`
	StartsAt string          `json:"startsAt"`
	Tags     []string        `json:"tags"`
	Screen   *showtimeScreen `json:"screen"`
	Data     showtimeData    `json:"data"`
}

type showtimeScreen struct {
	Name string `json:"name"`
}

type showtimeData struct {
	Ticketing json.RawMessage `json:"ticketing"`
}

type ticketingResponse struct {
	Provider string   `json:"provider"`
	Type     string   `json:"type"`
	URLs     []string `json:"urls"`
}

type movieResponse struct {
	ID      sourceID  `json:"id"`
	Title   string    `json:"title"`
	Runtime *int      `json:"runtime"`
	Poster  string    `json:"poster"`
	Genres  genreList `json:"genres"`
}
