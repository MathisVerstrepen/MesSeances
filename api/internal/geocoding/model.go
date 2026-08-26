package geocoding

import (
	"context"
	"time"
)

type Status string

const (
	StatusMatched   Status = "matched"
	StatusAmbiguous Status = "ambiguous"
	StatusNotFound  Status = "not_found"
	StatusManual    Status = "manual"
)

const (
	SourceIGN    = "ign"
	SourceManual = "manual"
)

type Location struct {
	Provider          string
	ProviderTheaterID string
	Latitude          *float64
	Longitude         *float64
	Source            string
	MatchedLabel      string
	MatchScore        *float64
	AddressHash       string
	Status            Status
	UpdatedAt         time.Time
	Suggestion        *CandidateSuggestion
}

type CandidateSuggestion struct {
	Latitude   *float64
	Longitude  *float64
	PostalCode string
	City       string
	Type       string
}

type Theater struct {
	Provider   string
	ProviderID string
	ID         string
	Address    string
	PostalCode string
	City       string
	Location   *Location
}

type Filters struct {
	Provider  string
	TheaterID string
}

type Query struct {
	Address    string
	PostalCode string
	City       string
}

type Candidate struct {
	Longitude      float64
	Latitude       float64
	HasCoordinates bool
	Label          string
	Score          float64
	HasScore       bool
	PostalCode     string
	City           string
	Type           string
}

type Provider interface {
	Search(context.Context, Query) ([]Candidate, error)
}

type Store interface {
	Select(context.Context, Filters) ([]Theater, error)
	Save(context.Context, *Location, Location) (bool, error)
}

func ValidProvider(value string) bool {
	return value == "ugc" || value == "kinepolis" || value == "pathe" || value == "cgr"
}
