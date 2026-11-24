package majestic

import (
	"errors"

	"messeances/api/internal/ticketingcine"
)

var errShape = errors.New("invalid Majestic response structure")

var (
	globalID = ticketingcine.GlobalMovieID
	localID  = ticketingcine.LocalMovieID
	dateID   = ticketingcine.DateID
)

var sites = [...]string{"EMS0700", "EMS0654", "EMS1333", "EMS1212", "EMS1299", "EMS1211", "EMS0957"}

// The wire model is shared with every TicketingCiné connector.
type (
	program = ticketingcine.Program
	event   = ticketingcine.Event
	session = ticketingcine.Session
)
