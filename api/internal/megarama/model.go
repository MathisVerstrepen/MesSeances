package megarama

import (
	"errors"
	"regexp"

	"messeances/api/internal/ticketingcine"
)

var errShape = errors.New("invalid Megarama response structure")

var (
	decimal          = regexp.MustCompile(`^[0-9]+$`)
	cinemaID         = ticketingcine.SiteID
	globalID         = ticketingcine.GlobalMovieID
	localID          = ticketingcine.LocalMovieID
	sessionID        = ticketingcine.SessionID
	dateID           = ticketingcine.DateID
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
	ProgramID   string `json:"prog_id"`
	Timezone    string `json:"time_zone"`
	Website     string `json:"website_url"`
	FullWebsite string `json:"website_full_url"`
}

// The wire model is shared with every TicketingCiné connector.
type (
	minutes = ticketingcine.Minutes
	program = ticketingcine.Program
	session = ticketingcine.Session
)
