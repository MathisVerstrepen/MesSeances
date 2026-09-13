package schedule

import (
	"net/url"
	"regexp"
	"strings"
	"time"
)

// megaramaWebsiteHosts is deliberately not a suffix allowlist. These are the
// cinema identities and official roots verified in chain CHN0042.
var megaramaWebsiteHosts = map[string]string{
	"EMS1315": "boulogne.megarama.fr", "EMS1310": "roubaix.megarama.fr",
	"EMS1169": "nice.megarama.fr", "EMS0971": "villeneuve.megarama.fr",
	"EMS1102": "montigny.megarama.fr", "EMS0565": "bordeaux.megarama.fr",
	"EMS1152": "chalon.megarama.fr", "EMS1101": "montpellier.megarama.fr",
	"EMS0660": "besancon.megarama.fr", "EMS1105": "beaux-arts.megarama.fr",
	"EMS0748": "audincourt.megarama.fr", "EMS0759": "arcueil.megarama.fr",
	"EMS1167": "arras.megarama.fr", "EMS1022": "pian.megarama.fr",
	"EMS0809": "royalpalace-nogent.ticketingcine.com", "EMS0720": "lons.megarama.fr",
	"EMS0015": "lons-le-palace.megarama.fr", "EMS0611": "studio66.megarama.fr",
	"EMS0348": "chambly.megarama.fr", "EMS0349": "garat.megarama.fr",
	"EMS1205": "camion-rouge.megarama.fr", "EMS1204": "alhambra.megarama.fr",
	"EMS1168": "denain.megarama.fr", "EMS1265": "orange.megarama.fr",
	"EMS1269": "annecy.megarama.fr", "EMS1288": "pince-vent.megarama.fr",
	"EMS1292": "givors.megarama.fr", "EMS0649": "dieppe.megarama.fr",
	"EMS1187": "louviers.megarama.fr", "EMS1188": "gaillon.megarama.fr",
	"EMS1053": "cine-armentieres.fr", "EMS0592": "lepalacecambrai.com",
	"EMS1348": "les-ulis.megarama.fr", "EMS1366": "cormeilles.megarama.fr",
}

// These cinema-specific aliases were verified in full-chain session URLs.
// They are booking-only: never accept them as config referers or fallback roots.
var megaramaBookingHosts = map[string]string{
	"EMS0592": "www.lepalacecambrai.com",
	"EMS0809": "www.royalpalacenogent.fr",
	"EMS1053": "www.cine-armentieres.fr",
	"EMS1204": "jean-jaures.megarama.fr",
	"EMS1205": "chavanelle.megarama.fr",
}

var (
	megaramaTheaterIdentity = regexp.MustCompile(`^EMS[0-9]{4}$`)
	megaramaGlobalIdentity  = regexp.MustCompile(`^[A-Z0-9]{5}$`)
	megaramaLocalIdentity   = regexp.MustCompile(`^EMS[0-9]{4}-emsx[0-9]{4}HC[0-9]+$`)
	megaramaShowingIdentity = regexp.MustCompile(`^emsx[0-9]{12}$`)
	megaramaGlobalPoster    = regexp.MustCompile(`^/movie_poster/(120|600)/FR[A-Z0-9]{5}/[A-Z0-9]{8}\.webp$`)
	megaramaLocalPoster     = regexp.MustCompile(`^/ems_spectacle/120/[0-9]{4}/HC[0-9]+\.jpg$`)
	megaramaPosterQuery     = regexp.MustCompile(`^ts=[0-9]+$`)
)

func ValidMegaramaBookingURL(raw, theaterID, showingID string) bool {
	u, err := url.Parse(raw)
	host, ok := megaramaWebsiteHosts[theaterID]
	if err != nil || !ok || len(raw) > maxURLLength || u.Scheme != "https" || u.User != nil || u.Opaque != "" || u.RawPath != "" || u.RawFragment != "" || u.RawQuery != "" || u.ForceQuery || (u.Path != "" && u.Path != "/") {
		return false
	}
	if u.Fragment == "" {
		return u.Host == host
	}
	return (u.Host == host || u.Host == megaramaBookingHosts[theaterID]) && megaramaShowingMatchesTheater(showingID, theaterID) && u.Fragment == "showsession?id="+showingID
}

func ValidMegaramaPosterURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > maxURLLength || u.Scheme != "https" || u.Host != "images.monnaie-services.com" || u.User != nil || u.Opaque != "" || u.RawPath != "" || u.Fragment != "" || u.ForceQuery {
		return false
	}
	return megaramaGlobalPoster.MatchString(u.Path) && u.RawQuery == "" || megaramaLocalPoster.MatchString(u.Path) && (u.RawQuery == "" || megaramaPosterQuery.MatchString(u.RawQuery))
}

func validMegaramaIdentity(kind, value string) bool {
	switch kind {
	case "theater":
		return megaramaTheaterIdentity.MatchString(value)
	case "movie":
		return len(value) <= 114 && (megaramaGlobalIdentity.MatchString(value) || megaramaLocalIdentity.MatchString(value) && value[3:7] == value[12:16])
	case "showing":
		return len(value) <= 111 && megaramaShowingIdentity.MatchString(value)
	default:
		return false
	}
}

// MegaramaEnd preserves unknown runtime as end=start, even with a first part.
// Bounds are checked before conversion to time.Duration or addition.
func MegaramaEnd(start time.Time, runtime, firstPart int) (time.Time, bool) {
	if runtime < 0 || firstPart < 0 {
		return time.Time{}, false
	}
	if firstPart > 0 {
		if _, ok := RuntimeDuration(firstPart); !ok {
			return time.Time{}, false
		}
	}
	if runtime == 0 {
		return start, true
	}
	r, ok := RuntimeDuration(runtime)
	if !ok {
		return time.Time{}, false
	}
	f := time.Duration(firstPart) * time.Minute
	if r > time.Duration(1<<63-1)-f {
		return time.Time{}, false
	}
	end := start.Add(r + f)
	return end, end.Year() >= 1 && end.Year() <= 9999
}

func effectiveRecordEnd(view *SnapshotView, record ShowtimeRecord) time.Time {
	if recordProvider(record.Provider, record.ID) != ProviderMegarama {
		return record.EndTime
	}
	runtime := record.Movie.RuntimeMinutes
	if runtime == 0 {
		if position, ok := view.publicMovieByID[record.Movie.PublicMovieID]; ok {
			movie := view.data.PublicMovies[position]
			if movie.TMDBID > 0 {
				runtime = movie.TMDBRuntimeMinutes
			}
		}
	}
	end, ok := MegaramaEnd(record.StartTime, runtime, record.FirstPartDurationMinutes)
	if !ok {
		return record.StartTime
	}
	return end
}

func megaramaShowingMatchesTheater(showingID, theaterID string) bool {
	return megaramaShowingIdentity.MatchString(showingID) && strings.HasPrefix(showingID, "emsx"+strings.TrimPrefix(theaterID, "EMS"))
}
