package schedule

import (
	"strings"
	"testing"
)

func TestTicketingcineIdentities(t *testing.T) {
	for _, test := range []struct {
		kind, value string
		valid       bool
	}{
		{"theater", "EMS1234", true},
		{"theater", "EMS12", false},
		{"theater", "ems1234", false},
		{"movie", "ABCDE", true},
		{"movie", "EMS1234-emsx1234HC42", true},
		{"movie", "EMS1234-emsx4321HC42", false},
		{"movie", "EMS1234-emsx1234HC" + strings.Repeat("9", 91), true},
		{"movie", "EMS1234-emsx1234HC" + strings.Repeat("9", 92), false},
		{"showing", "emsx123400000001", true},
		{"showing", "emsx1234000001", false},
		{"other", "EMS1234", false},
	} {
		if got := ValidTicketingcineIdentity(test.kind, test.value); got != test.valid {
			t.Fatalf("%s %q valid=%t", test.kind, test.value, got)
		}
	}
	if recordProvider("", "ticketingcine-EMS1234") != ProviderTicketingcine || recordProvider("", "cinewest-ticketingcine-EMS1378") != ProviderCinewest {
		t.Fatal("identity prefix resolution")
	}
}

func TestTicketingcineBookingURLs(t *testing.T) {
	const site, showing = "EMS1234", "emsx123400000001"
	fallback := TicketingcineBookingURL(site, showing)
	for _, test := range []struct {
		raw   string
		valid bool
	}{
		{fallback, true},
		{fallback + "&ps=ticketingcine", true},
		{"https://www.ticketingcine.com/EMS1234/FRABCDE#showsession?id=emsx123400000001", true},
		{"https://www.ticketingcine.com/EMS1234/FRABCDE#showsession?id=emsx123400000002", false},
		{"https://www.ticketingcine.com?EMS4321#showsession?id=emsx123400000001", false},
		{"http://www.ticketingcine.com?EMS1234#showsession?id=emsx123400000001", false},
		{"https://cinema.example/reserver#showsession?id=emsx123400000001", false},
		{"https://www.ticketingcine.com/EMS1234/FRABCDE?x=1#showsession?id=emsx123400000001", false},
	} {
		if got := ValidTicketingcineBookingURL(test.raw, site, showing); got != test.valid {
			t.Fatalf("%q valid=%t", test.raw, got)
		}
	}
	if ValidTicketingcineBookingURL(fallback, site, "emsx432100000001") {
		t.Fatal("showing from another site accepted")
	}
	film := "https://www.ticketingcine.com/EMS1234/FRABCDE#showsession?id=emsx123400000001"
	if !TicketingcineBookingMatchesMovie(film, site, "ABCDE") || TicketingcineBookingMatchesMovie(film, site, "ZZZZZ") || TicketingcineBookingMatchesMovie(film, site, "EMS1234-emsx1234HC42") {
		t.Fatal("film path binding")
	}
	if !TicketingcineBookingMatchesMovie(fallback, site, "EMS1234-emsx1234HC42") || !TicketingcineBookingMatchesMovie(fallback, site, "ABCDE") {
		t.Fatal("site route binding")
	}
}
