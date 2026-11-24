package schedule

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// addressRule states which theater address fields a provider must publish.
type addressRule uint8

const (
	// addressRequired requires both street address and postal code.
	addressRequired addressRule = iota
	// addressPostalOnly requires a postal code; the street may be missing.
	addressPostalOnly
	// addressOptional lets the provider omit both.
	addressOptional
)

// serviceDayRule maps a local start time to the cinema day it is sold under.
type serviceDayRule uint8

const (
	// serviceDayStrictOvernight files 00:00–02:59 under the previous day and
	// rejects 03:00–07:59 starts.
	serviceDayStrictOvernight serviceDayRule = iota
	// serviceDayOvernight files 00:00–02:59 under the previous day.
	serviceDayOvernight
	// serviceDayCalendar uses the local calendar date of the start.
	serviceDayCalendar
	// serviceDayPathe accepts the previous day for starts before 08:00 when
	// the source files them there (overnight sessions and early premieres).
	serviceDayPathe
)

// providerRules gathers everything ValidateDataset needs to know about one
// provider. Invariants shared by every provider stay in validation.go.
type providerRules struct {
	// identity validates theater, movie and showing source identities.
	identity func(kind, value string) bool
	// booking validates a stored booking URL against its showing and theater.
	booking func(raw, showingID, theaterProviderID string) bool
	// poster validates a movie poster URL.
	poster func(raw string) bool

	// unknownRuntime lets movies omit their runtime.
	unknownRuntime bool
	address        addressRule
	// passes lists the exact accepted passes; nil means none.
	passes []string
	// census, when positive, is the provider's exhaustive theater count: a
	// snapshot with exactly these theaters may publish no showings.
	census int
	// strictAliasSlug requires source slug aliases to be the canonical
	// "<provider>-film-<id>" spelling.
	strictAliasSlug bool
	serviceDay      serviceDayRule

	// firstPart reports whether a showing may carry a non-zero first part
	// (advertising) duration; nil means never.
	firstPart func(ShowtimeRecord, TheaterRecord) bool
	// showing applies provider bindings and may replace the default end-time
	// rule; it receives and returns whether the end time is valid.
	showing func(s ShowtimeRecord, t TheaterRecord, validEnd bool) (bool, error)
	// version reports silent films and unknown versions, which may omit the
	// language or provider version.
	version func(ShowtimeRecord) (silent, unknown bool, err error)
	// completeCoverage reports whether a theater's acquisition can attest
	// complete per-date activity coverage; nil means never.
	completeCoverage func(TheaterRecord) bool
}

func anyTheater(TheaterRecord) bool { return true }

func rulesFor(provider Provider) (providerRules, bool) {
	rules, ok := providerRuleSet[provider]
	return rules, ok
}

// ValidProviderIdentity validates a source identity of kind theater, movie or
// showing for provider.
func ValidProviderIdentity(provider Provider, kind, value string) bool {
	return validProviderIdentity(provider, kind, value)
}

func validProviderIdentity(provider Provider, kind, value string) bool {
	rules, ok := rulesFor(provider)
	return ok && rules.identity(kind, value)
}

func validBookingURL(provider Provider, raw, showingID, theaterProviderID string) bool {
	rules, ok := rulesFor(provider)
	return ok && rules.booking(raw, showingID, theaterProviderID)
}

func validProviderImageURL(provider Provider, raw string) bool {
	rules, ok := rulesFor(provider)
	return ok && rules.poster(raw)
}

func providerAllowsUnknownRuntime(provider Provider) bool {
	rules, _ := rulesFor(provider)
	return rules.unknownRuntime
}

var providerRuleSet = map[Provider]providerRules{
	ProviderUGC: {
		identity: ugcIdentity, booking: ugcBookingURL, poster: func(raw string) bool { return validUGCURL(raw, true) },
		passes: []string{"UGC_ILLIMITE"}, serviceDay: serviceDayStrictOvernight,
		completeCoverage: anyTheater,
	},
	ProviderKinepolis: {
		identity: kinepolisIdentity, booking: kinepolisBookingURL, poster: kinepolisPosterURL,
		address: addressOptional, serviceDay: serviceDayStrictOvernight, showing: kinepolisShowingRule,
	},
	ProviderPathe: {
		identity: patheIdentity, booking: patheBookingURL, poster: pathePosterURL,
		serviceDay:       serviceDayPathe,
		completeCoverage: anyTheater,
	},
	ProviderCGR: {
		identity: cgrIdentity, booking: cgrBookingURL, poster: cgrPosterURL,
		unknownRuntime: true, serviceDay: serviceDayStrictOvernight,
		completeCoverage: anyTheater,
	},
	ProviderMegarama: {
		identity: validMegaramaIdentity, booking: reorderedBooking(ValidMegaramaBookingURL), poster: ValidMegaramaPosterURL,
		unknownRuntime: true, serviceDay: serviceDayOvernight, firstPart: alwaysFirstPart, showing: megaramaShowingRule,
	},
	ProviderCineville: {
		identity: ValidCinevilleIdentity, booking: reorderedBooking(ValidCinevilleBookingURL), poster: ValidCinevillePosterURL,
		unknownRuntime: true, address: addressPostalOnly, strictAliasSlug: true, serviceDay: serviceDayCalendar, showing: cinevilleShowingRule,
	},
	ProviderMK2: {
		identity: ValidMK2Identity, booking: reorderedBooking(ValidMK2BookingURL), poster: ValidMK2PosterURL,
		unknownRuntime: true, strictAliasSlug: true, serviceDay: serviceDayCalendar, showing: mk2ShowingRule, version: mk2VersionRule,
	},
	ProviderCinewest: {
		identity: ValidCinewestIdentity, booking: reorderedBooking(ValidCinewestBookingURL), poster: ValidCinewestPosterURL,
		unknownRuntime: true, strictAliasSlug: true, serviceDay: serviceDayOvernight,
		firstPart: cinewestFirstPartRule, showing: cinewestShowingRule, version: cinewestVersionRule,
	},
	ProviderGrandEcran: {
		identity: ValidGrandEcranIdentity, poster: ValidGrandEcranPosterURL,
		booking: func(raw, showingID, theaterID string) bool {
			return ValidGrandEcranBookingURL(raw) && strings.HasPrefix(showingID, theaterID+"-")
		},
		unknownRuntime: true, strictAliasSlug: true, serviceDay: serviceDayOvernight,
		showing:          theaterScopedShowing("invalid Grand Ecran cinema", endEqualsStart),
		completeCoverage: anyTheater,
	},
	ProviderNoeCinemas: {
		identity: ValidNoeCinemasIdentity, poster: ValidNoeCinemasPosterURL,
		booking: func(raw, showingID, theaterID string) bool {
			return ValidNoeCinemasBookingURL(raw, theaterID) && strings.HasPrefix(showingID, theaterID+"-")
		},
		unknownRuntime: true, strictAliasSlug: true, serviceDay: serviceDayOvernight,
		showing:          theaterScopedShowing("invalid Noé Cinémas cinema", endEqualsStart),
		completeCoverage: anyTheater,
	},
	ProviderVeo: {
		identity: ValidVeoIdentity, booking: reorderedBooking(ValidVeoBookingURL), poster: ValidVeoPosterURL,
		unknownRuntime: true, strictAliasSlug: true, serviceDay: serviceDayOvernight,
		firstPart: veoFirstPartRule, showing: veoShowingRule,
		completeCoverage: func(t TheaterRecord) bool { return !veoTicketingTheater(t) },
	},
	ProviderMajestic: {
		identity: ValidMajesticIdentity, booking: reorderedBooking(ValidMajesticBookingURL), poster: ValidMajesticPosterURL,
		unknownRuntime: true, census: 7, strictAliasSlug: true, serviceDay: serviceDayCalendar,
		firstPart: alwaysFirstPart, showing: majesticShowingRule,
	},
	ProviderCineode: {
		identity: ValidCineodeIdentity, booking: reorderedBooking(ValidCineodeBookingURL), poster: ValidCineodePosterURL,
		unknownRuntime: true, address: addressOptional, census: 37, strictAliasSlug: true, serviceDay: serviceDayCalendar,
		showing: cineodeShowingRule,
	},
	ProviderCinemovida: {
		identity: ValidCinemovidaIdentity, poster: ValidCinemovidaPosterURL,
		booking: func(raw, showingID, theaterID string) bool {
			return ValidCinemovidaBookingURL(raw, theaterID) && ValidCinemovidaIdentity("showing", showingID) && strings.HasPrefix(showingID, theaterID+"-")
		},
		unknownRuntime: true, strictAliasSlug: true, serviceDay: serviceDayCalendar,
		showing:          theaterScopedShowing("invalid Ciné-Movida cinema", endNotBeforeStart),
		completeCoverage: anyTheater,
	},
	ProviderOcine: {
		identity: ValidOcineIdentity, booking: reorderedBooking(ValidOcineBookingURL), poster: ValidOcinePosterURL,
		unknownRuntime: true, address: addressOptional, census: 3, strictAliasSlug: true, serviceDay: serviceDayCalendar,
		showing: ocineShowingRule, version: ocineVersionRule,
	},
}

// reorderedBooking adapts provider validators that take the theater before
// the showing.
func reorderedBooking(valid func(raw, theaterID, showingID string) bool) func(raw, showingID, theaterID string) bool {
	return func(raw, showingID, theaterID string) bool { return valid(raw, theaterID, showingID) }
}

func alwaysFirstPart(ShowtimeRecord, TheaterRecord) bool { return true }

func endEqualsStart(s ShowtimeRecord) bool    { return s.EndTime.Equal(s.StartTime) }
func endNotBeforeStart(s ShowtimeRecord) bool { return !s.EndTime.Before(s.StartTime) }

// theaterScopedShowing requires showing IDs to be prefixed by their theater ID
// and replaces the end-time rule.
func theaterScopedShowing(message string, validEnd func(ShowtimeRecord) bool) func(ShowtimeRecord, TheaterRecord, bool) (bool, error) {
	return func(s ShowtimeRecord, t TheaterRecord, _ bool) (bool, error) {
		if !strings.HasPrefix(s.ProviderShowingID, t.ProviderID+"-") {
			return false, errors.New(message)
		}
		return validEnd(s), nil
	}
}

func kinepolisShowingRule(s ShowtimeRecord, _ TheaterRecord, validEnd bool) (bool, error) {
	runtime, _ := RuntimeDuration(s.Movie.RuntimeMinutes)
	return validEnd && s.EndTime.Equal(s.StartTime.Add(runtime)), nil
}

func megaramaShowingRule(s ShowtimeRecord, t TheaterRecord, validEnd bool) (bool, error) {
	end, valid := MegaramaEnd(s.StartTime, s.Movie.RuntimeMinutes, s.FirstPartDurationMinutes)
	if !valid || !end.Equal(s.EndTime) || !megaramaShowingMatchesTheater(s.ProviderShowingID, t.ProviderID) {
		return false, errors.New("invalid Megarama showing times or cinema")
	}
	return validEnd, nil
}

func majesticShowingRule(s ShowtimeRecord, t TheaterRecord, validEnd bool) (bool, error) {
	if !MajesticBookingMatchesMovie(s.BookingURL, t.ProviderID, s.ProviderShowingID, s.Movie.ProviderID) {
		return false, errors.New("invalid Majestic booking or movie cinema")
	}
	return megaramaShowingRule(s, t, validEnd)
}

func ocineShowingRule(s ShowtimeRecord, t TheaterRecord, _ bool) (bool, error) {
	if !validOcineShowing(s, t.ProviderID) {
		return false, errors.New("invalid Ocine showing binding or attributes")
	}
	return endNotBeforeStart(s), nil
}

func ocineVersionRule(s ShowtimeRecord) (bool, bool, error) {
	unknown := s.Language == "" && s.ProviderVersion == "" && s.BookingURL == "" && strings.HasSuffix(s.ProviderShowingID, "-past")
	return false, unknown, nil
}

func cineodeShowingRule(s ShowtimeRecord, t TheaterRecord, _ bool) (bool, error) {
	validVersion := s.Language == LanguageVF && s.ProviderVersion == "VF" || s.Language == LanguageVOSTFR && s.ProviderVersion == "VOSTF"
	if !CineodeShowingMatches(s.ProviderShowingID, t.ProviderID, s.Movie.ProviderID, s.StartTime) || s.Room != "" || !validVersion {
		return false, errors.New("invalid Cineode showing binding or attributes")
	}
	return endEqualsStart(s), nil
}

func veoTicketingTheater(t TheaterRecord) bool { return t.ProviderID == "EMS1361" }

func veoFirstPartRule(_ ShowtimeRecord, t TheaterRecord) bool { return veoTicketingTheater(t) }

func veoShowingRule(s ShowtimeRecord, t TheaterRecord, _ bool) (bool, error) {
	ticket := veoTicketingTheater(t)
	if !strings.HasPrefix(s.ProviderShowingID, t.ProviderID+"-") || ticket != strings.HasPrefix(s.Movie.ProviderID, "ticketingcine-") {
		return false, errors.New("invalid Véo platform or cinema")
	}
	if ticket {
		end, ok := MegaramaEnd(s.StartTime, s.Movie.RuntimeMinutes, s.FirstPartDurationMinutes)
		return ok && end.Equal(s.EndTime), nil
	}
	return endNotBeforeStart(s), nil
}

func cinewestFirstPartRule(s ShowtimeRecord, _ TheaterRecord) bool {
	return strings.HasPrefix(s.ProviderShowingID, "ticketingcine-")
}

func cinewestShowingRule(s ShowtimeRecord, t TheaterRecord, _ bool) (bool, error) {
	platform, _, _ := strings.Cut(s.ProviderShowingID, "-")
	legacy := t.ProviderID == "ticketingcine-EMS1378" && platform == "webediamovies"
	if (!strings.HasPrefix(t.ProviderID, platform+"-") && !legacy) || !strings.HasPrefix(s.Movie.ProviderID, platform+"-") || strings.TrimSpace(s.Room) == "" {
		return false, errors.New("invalid Cinewest cinema or room")
	}
	switch platform {
	case "cineoffice":
		return s.EndTime.After(s.StartTime), nil
	case "ticketingcine":
		end, ok := MegaramaEnd(s.StartTime, s.Movie.RuntimeMinutes, s.FirstPartDurationMinutes)
		return ok && end.Equal(s.EndTime), nil
	case "webediamovies":
		return endEqualsStart(s), nil
	default:
		return false, nil
	}
}

func cinewestVersionRule(s ShowtimeRecord) (bool, bool, error) {
	silent := s.Language == "" && strings.HasPrefix(s.ProviderShowingID, "cineoffice-") && s.ProviderVersion == "VERSION_MUET"
	if s.ProviderVersion == "VERSION_MUET" && !silent {
		return false, false, errors.New("invalid Cinewest silent version")
	}
	return silent, false, nil
}

func mk2ShowingRule(s ShowtimeRecord, t TheaterRecord, _ bool) (bool, error) {
	if s.Room != "" || !strings.HasPrefix(s.ProviderShowingID, t.ProviderID+"-") {
		return false, errors.New("invalid MK2 cinema or room")
	}
	return endEqualsStart(s), nil
}

func mk2VersionRule(s ShowtimeRecord) (bool, bool, error) {
	silent := s.Language == "" && s.ProviderVersion == "Muet"
	if s.ProviderVersion == "Muet" && !silent {
		return false, false, errors.New("invalid showing attributes")
	}
	return silent, s.Language == "" && s.ProviderVersion == "", nil
}

func cinevilleShowingRule(s ShowtimeRecord, t TheaterRecord, _ bool) (bool, error) {
	if !strings.HasPrefix(s.ProviderShowingID, t.ProviderID+"-") || !ValidCinevilleIdentity("theater", s.Room) {
		return false, errors.New("invalid Cineville cinema or room")
	}
	return endEqualsStart(s), nil
}

// expectedServiceDate returns the cinema day a showing starting at localStart
// must be filed under, or false when the start falls outside any cinema day.
func (r providerRules) expectedServiceDate(localStart time.Time, filed string) (string, bool) {
	expected := localStart.Format(dateLayout)
	switch r.serviceDay {
	case serviceDayPathe:
		if expected != filed && localStart.Hour() < 8 {
			expected = localStart.AddDate(0, 0, -1).Format(dateLayout)
		}
	case serviceDayStrictOvernight, serviceDayOvernight:
		if localStart.Hour() <= 2 {
			expected = localStart.AddDate(0, 0, -1).Format(dateLayout)
		} else if localStart.Hour() < 8 && r.serviceDay == serviceDayStrictOvernight {
			return "", false
		}
	}
	return expected, true
}

// Identity, booking and poster rules for the national chains whose formats
// predate dedicated provider files.

func ugcIdentity(_, value string) bool {
	if !providerIdentity.MatchString(value) {
		return false
	}
	number, err := strconv.ParseUint(value, 10, 64)
	return err == nil && number > 0
}

func kinepolisIdentity(_, value string) bool { return providerIdentity.MatchString(value) }

func patheIdentity(kind, value string) bool {
	if !providerIdentity.MatchString(value) {
		return false
	}
	maxLength := maxIdentityLength
	switch kind {
	case "theater":
		maxLength -= len("pathe-")
	case "movie":
		maxLength -= len("pathe-film-")
	case "showing":
		maxLength -= len("pathe-showing-")
	default:
		return false
	}
	if len(value) > maxLength {
		return false
	}
	if kind == "showing" {
		return patheShowingIdentity.MatchString(value)
	}
	return true
}

func cgrIdentity(kind, value string) bool {
	if !providerIdentity.MatchString(value) {
		return false
	}
	switch kind {
	case "theater":
		return cgrTheaterIdentity.MatchString(value)
	case "movie":
		return validPositiveDecimal(value)
	case "showing":
		return cgrShowingIdentity.MatchString(value)
	default:
		return false
	}
}

// strictBookingURL parses an https booking URL without credentials, fragment,
// encoded path, opaque form or empty query marker.
func strictBookingURL(raw string) (*url.URL, bool) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Fragment != "" || parsed.RawPath != "" || parsed.Opaque != "" || parsed.ForceQuery {
		return nil, false
	}
	return parsed, true
}

func ugcBookingURL(raw, showingID, _ string) bool {
	parsed, ok := strictBookingURL(raw)
	if !ok || parsed.Host != "www.ugc.fr" || parsed.Path != "/reservationSeances.html" {
		return false
	}
	query := parsed.Query()
	return len(query) == 1 && len(query["id"]) == 1 && query.Get("id") == showingID
}

func kinepolisBookingURL(raw, showingID, theaterID string) bool {
	parsed, ok := strictBookingURL(raw)
	return ok && parsed.Host == "kinepolis.fr" && parsed.RawQuery == "" && parsed.Path == "/direct-vista-redirect/"+showingID+"/0/"+theaterID+"/0"
}

func patheBookingURL(raw, showingID, _ string) bool {
	parsed, ok := strictBookingURL(raw)
	if !ok {
		return false
	}
	parts := strings.Split(parsed.Path, "/")
	return parsed.Host == "s.pathe.fr" && parsed.RawQuery == "" && len(parts) == 4 && parts[0] == "" && parts[1] == "fr" && providerIdentity.MatchString(parts[2]) && parts[2] == showingID && parts[3] == "booking" && !strings.Contains(parsed.Path, `\`) && !hasPatheTraversalSegment(parsed.Path)
}

func cgrBookingURL(raw, _, _ string) bool {
	parsed, ok := strictBookingURL(raw)
	if !ok {
		return false
	}
	parts := strings.Split(parsed.Path, "/")
	return parsed.Host == "achat.cgrcinemas.fr" && parsed.RawQuery == "" && len(parts) == 4 && parts[0] == "" && validCGRBookingSlug(parts[1]) && parts[2] == "r" && validPositiveDecimal(parts[3]) && !strings.Contains(parsed.Path, `\`) && !hasPatheTraversalSegment(parsed.Path)
}

func kinepolisPosterURL(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && len(raw) <= maxURLLength && parsed.Scheme == "https" && parsed.Host == "cdn.kinepolis.fr" && parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == "" && strings.HasPrefix(parsed.Path, "/images/") && !hasPathTraversalSegment(parsed.Path) && !strings.Contains(parsed.Path, `\`)
}

// strictImageURL parses an https image URL with a non-root path and no query,
// fragment, credentials, encoded path or traversal.
func strictImageURL(raw string) (*url.URL, bool) {
	parsed, err := url.Parse(raw)
	if err != nil || len(raw) > maxURLLength || parsed.Scheme != "https" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.RawPath != "" || parsed.Opaque != "" || parsed.ForceQuery || parsed.Path == "" || parsed.Path == "/" || hasPatheTraversalSegment(parsed.Path) || strings.Contains(parsed.Path, `\`) {
		return nil, false
	}
	return parsed, true
}

func cgrPosterURL(raw string) bool {
	parsed, ok := strictImageURL(raw)
	if !ok {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return parsed.Host == host && (host == "acsta.net" || strings.HasSuffix(host, ".acsta.net"))
}

func pathePosterURL(raw string) bool {
	parsed, ok := strictImageURL(raw)
	if !ok {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return parsed.Host == host && (host == "pathe.fr" || strings.HasSuffix(host, ".pathe.fr"))
}
