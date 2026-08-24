package pathe

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type cinemaResponse struct {
	Slug     string          `json:"slug"`
	Name     string          `json:"name"`
	Theaters []cinemaTheater `json:"theaters"`
}

type cinemaTheater struct {
	AddressLine1 string `json:"addressLine1"`
	AddressZip   string `json:"addressZip"`
	AddressCity  string `json:"addressCity"`
}

type showsResponse struct {
	Shows []showResponse `json:"shows"`
}

type showResponse struct {
	Slug       string         `json:"slug"`
	Title      string         `json:"title"`
	Duration   int            `json:"duration"`
	PosterPath showPosterPath `json:"posterPath"`
	Genres     []string       `json:"genres"`
	IsMovie    *bool          `json:"isMovie"`
	Type       string         `json:"type"`
}

type showPosterPath struct {
	Large string `json:"lg"`
}

type objectOrEmptyArray[T any] map[string]T

func (value *objectOrEmptyArray[T]) UnmarshalJSON(body []byte) error {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return fmt.Errorf("expected object or empty array")
	}
	switch trimmed[0] {
	case '{':
		var object map[string]T
		if err := json.Unmarshal(trimmed, &object); err != nil {
			return err
		}
		if object == nil {
			return fmt.Errorf("expected object or empty array")
		}
		*value = object
		return nil
	case '[':
		var array []json.RawMessage
		if err := json.Unmarshal(trimmed, &array); err != nil {
			return err
		}
		if len(array) != 0 {
			return fmt.Errorf("expected object or empty array")
		}
		*value = make(map[string]T)
		return nil
	default:
		return fmt.Errorf("expected object or empty array")
	}
}

type cinemaProgram struct {
	Days  objectOrEmptyArray[json.RawMessage]
	Shows objectOrEmptyArray[programShow]
}

func (program *cinemaProgram) UnmarshalJSON(body []byte) error {
	var raw struct {
		Days  json.RawMessage `json:"days"`
		Shows json.RawMessage `json:"shows"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return err
	}
	if len(raw.Days) == 0 || len(raw.Shows) == 0 {
		return fmt.Errorf("cinema program fields are required")
	}
	var days objectOrEmptyArray[json.RawMessage]
	if err := json.Unmarshal(raw.Days, &days); err != nil {
		return err
	}
	var shows objectOrEmptyArray[programShow]
	if err := json.Unmarshal(raw.Shows, &shows); err != nil {
		return err
	}
	program.Days, program.Shows = days, shows
	return nil
}

type programShow struct {
	Days map[string]programDay `json:"days"`
}

type programDay struct {
	Tags     []string `json:"tags"`
	Versions []string `json:"versions"`
}

type sessionResponse struct {
	Time               string          `json:"time"`
	Version            string          `json:"version"`
	Tags               []string        `json:"tags"`
	Status             string          `json:"status"`
	RefCmd             string          `json:"refCmd"`
	AuditoriumName     json.RawMessage `json:"auditoriumName"`
	AuditoriumCapacity json.RawMessage `json:"auditoriumCapacity"`
	EndTime            string          `json:"endTime"`
}
