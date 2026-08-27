package httpapi

import (
	"crypto/sha256"
	"strings"
	"time"

	"messeances/api/internal/enrichment"
)

type adminAPI struct {
	origin     string
	password   string
	key        [32]byte
	hasKey     bool
	reviews    *enrichment.ReviewService
	tmdbReruns TMDBRerunner
	locals     *enrichment.LocalMovieService
	syncs      SyncController
	schedules  SyncScheduleController
	locations  TheaterLocationController
	geocoding  TheaterGeocodingController
	now        func() time.Time
}

type sessionResponse struct {
	Authenticated bool `json:"authenticated"`
}

func newAdminAPI(origin string, options AdminOptions) *adminAPI {
	if options.Now == nil {
		options.Now = time.Now
	}
	password := options.Password
	if strings.TrimSpace(password) == "" {
		password = ""
	}
	var key [32]byte
	hasKey := strings.TrimSpace(options.SessionSecret) != ""
	if hasKey {
		key = sha256.Sum256([]byte("messeances-admin-session-secret-v1\x00" + options.SessionSecret))
	}
	return &adminAPI{
		origin: origin, password: password, key: key, hasKey: hasKey,
		reviews: options.Reviews, tmdbReruns: options.TMDBReruns, locals: options.LocalMovies, syncs: options.Syncs, schedules: options.SyncSchedules, locations: options.TheaterLocations, geocoding: options.TheaterGeocoding, now: options.Now,
	}
}

func (a *adminAPI) configured() bool { return a.password != "" && a.hasKey && a.reviews != nil }
