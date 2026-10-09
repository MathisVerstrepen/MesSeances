package schedule

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"time"
)

// PublicPageImage contains stable public metadata only. No media I/O participates in observation.
type PublicPageImage struct {
	URL    string
	Width  int
	Height int
}

type SitemapData struct {
	AsOf              time.Time             `json:"as_of"`
	Revision          string                `json:"revision"`
	Movies            []MovieCatalogItem    `json:"movies"`
	MovieTotal        int                   `json:"movie_total"`
	Cities            *CityInventory        `json:"cities"`
	UpcomingAvailable bool                  `json:"upcoming_available"`
	LastmodByPath     map[string]*time.Time `json:"lastmod_by_path"`
}

type SitemapProjection struct {
	Data         SitemapData
	Fingerprints map[string][sha256.Size]byte
}

// These explicit content types deliberately cannot carry publication clocks or revisions.
// Extend them when the canonical public page starts rendering another significant fact.
type pageMovie struct {
	Slug, Title                                                      string
	OriginalTitle, OriginalLanguage                                  *string
	RuntimeMinutes                                                   int
	PosterURL, BackdropURL, Overview, ReleaseDate, FrenchReleaseDate *string
	TMDBID                                                           *int64
	IMDBID, MetacriticID, TrailerVF, TrailerVO                       *string
	Genres                                                           []string
}

type pageMovieCard struct {
	Slug, Title                   string
	RuntimeMinutes, ShowtimeCount int
	PosterURL                     *string
}

type pageVenue struct {
	Provider                                            Provider
	ID, Slug, Name, Address, City, CitySlug, PostalCode string
	AcceptedPasses                                      []string
}

type pageSession struct {
	Provider                                     Provider
	ID, Date                                     string
	Venue                                        pageSessionVenue
	Movie                                        pageMovieCard
	OriginalTitle, OriginalLanguage, BackdropURL *string
	Start, End                                   time.Time
	EstimatedEnd                                 *time.Time
	EstimatedAds                                 *int
	Language                                     Language
	Format                                       Format
	Room                                         string
	BookingURL                                   *string
}

type pageSessionVenue struct {
	Provider                       Provider
	ID, Slug, Name, City, CitySlug string
}

type filmPageContent struct {
	Movie             pageMovie
	ReleaseStatus     string
	CurrentlyScreened bool
	Dates             []string
	Programme         []pageSession
	Discovery         MovieDiscovery
}

type theaterPageDiscovery struct {
	Window        *DiscoveryWindow
	Movies        []pageMovieCard
	OtherTheaters []DiscoveryTheater
}

type cinemaPageContent struct {
	Venue              pageVenue
	Image              *PublicPageImage
	AvailableDateCount int
	Dates              []string
	DefaultDate        string
	Programme          []pageSession
	Discovery          theaterPageDiscovery
}

type cityPageContent struct {
	City            City
	Venues          []pageVenue
	MovieCount      int
	Catalogue       []pageMovieCard
	Total, PageSize int
	Discovery       CityDiscovery
}

func projectMovie(item MovieCatalogItem, backdrop *string) pageMovie {
	genres := append([]string{}, item.Genres...)
	sort.Strings(genres)
	return pageMovie{Slug: item.Slug, Title: item.Title, OriginalTitle: item.OriginalTitle, OriginalLanguage: item.OriginalLanguage, RuntimeMinutes: item.RuntimeMinutes, PosterURL: item.PosterURL, BackdropURL: backdrop, Overview: item.Overview, ReleaseDate: item.ReleaseDate, FrenchReleaseDate: item.FrenchReleaseDate, TMDBID: item.TMDBID, IMDBID: item.IMDBID, MetacriticID: item.MetacriticID, TrailerVF: item.TrailerVFYouTubeKey, TrailerVO: item.TrailerVOYouTubeKey, Genres: genres}
}

func projectCard(item MovieCatalogItem) pageMovieCard {
	return pageMovieCard{Slug: item.Slug, Title: item.Title, RuntimeMinutes: item.RuntimeMinutes, ShowtimeCount: item.ShowtimeCount, PosterURL: item.PosterURL}
}

func projectVenue(theater Theater) pageVenue {
	passes := append([]string{}, theater.AcceptedPasses...)
	sort.Strings(passes)
	return pageVenue{Provider: theater.Provider, ID: theater.ID, Slug: theater.Slug, Name: theater.Name, Address: theater.Address, City: theater.City, CitySlug: theater.CitySlug, PostalCode: theater.PostalCode, AcceptedPasses: passes}
}

func nonpastDates(dates []string, today string) []string {
	result := []string{}
	for _, date := range dates {
		if date >= today {
			result = append(result, date)
		}
	}
	sort.Strings(result)
	return result
}

func releaseStatus(view *SnapshotView, slug string, item MovieCatalogItem, today string, current bool) string {
	if item.FrenchReleaseDate != nil && *item.FrenchReleaseDate > today {
		return "upcoming"
	}
	if current {
		return "showing"
	}
	index := view.movieBySlug[slug]
	if len(view.data.PublicMovies) > 0 && view.data.PublicMovies[index.publicMovie].HasUpcomingRelease && item.FrenchReleaseDate == nil {
		return "unavailable"
	}
	return "ended"
}

// BuildSitemapProjection materializes a coherent canonical inventory and public page content.
// It does not infer dates, persist state, perform I/O, or apply frontend indexability policy.
func BuildSitemapProjection(view *SnapshotView, now time.Time, images map[string]PublicPageImage) (SitemapProjection, error) {
	if view == nil || now.IsZero() || !view.catalogOnly && !view.readiness.complete {
		return SitemapProjection{}, ErrNoCompleteSnapshot
	}
	location, err := time.LoadLocation(Timezone)
	if err != nil {
		return SitemapProjection{}, err
	}
	today := now.In(location).Format(dateLayout)
	projection := SitemapProjection{Data: SitemapData{AsOf: now.UTC(), Movies: []MovieCatalogItem{}, UpcomingAvailable: !view.data.UpcomingCompletedAt.IsZero(), LastmodByPath: make(map[string]*time.Time)}, Fingerprints: make(map[string][sha256.Size]byte)}
	aggregate := aggregateProgramme(view, now)
	movieProgramme := make(map[string][]pageSession)
	theaterProgramme := make([][]pageSession, len(view.data.Theaters))
	currentCounts := make(map[string]int)
	cityCounts := make([]map[string]int, len(view.cityBuckets))
	seen := make(map[string]bool, len(view.data.Showtimes))
	for _, record := range view.data.Showtimes {
		key := string(recordProvider(record.Provider, record.ID)) + "\x00" + record.ID
		if seen[key] {
			continue
		}
		seen[key] = true
		position, ok := view.theaterByID[record.TheaterID]
		if !ok {
			return SitemapProjection{}, fmt.Errorf("missing public theater")
		}
		slug := view.publicMovieSlug(record.Movie)
		if isCurrentMovieShowtime(record, now) {
			currentCounts[slug]++
			city := view.theaterCity[position]
			if cityCounts[city] == nil {
				cityCounts[city] = make(map[string]int)
			}
			cityCounts[city][slug]++
		}
		if record.ServiceDate < today {
			continue
		}
		showing := materializeRecord(view, record)
		poster, backdrop := materializeMovieMedia(view, record.Movie)
		theater := materializeTheater(view, position)
		session := pageSession{Provider: showing.Provider, ID: showing.ID, Date: record.ServiceDate, Venue: pageSessionVenue{Provider: theater.Provider, ID: theater.ID, Slug: theater.Slug, Name: theater.Name, City: theater.City, CitySlug: theater.CitySlug}, Movie: pageMovieCard{Slug: showing.Movie.Slug, Title: showing.Movie.Title, RuntimeMinutes: showing.Movie.RuntimeMinutes, PosterURL: poster}, OriginalTitle: showing.Movie.OriginalTitle, OriginalLanguage: showing.Movie.OriginalLanguage, BackdropURL: backdrop, Start: showing.StartTime, End: showing.EndTime, EstimatedEnd: showing.EstimatedEndTime, EstimatedAds: showing.EstimatedEndAdsMinutes, Language: showing.Language, Format: showing.Format, Room: showing.Room, BookingURL: showing.BookingURL}
		movieProgramme[slug] = append(movieProgramme[slug], session)
		theaterProgramme[position] = append(theaterProgramme[position], session)
	}
	orderSessions := func(sessions []pageSession) {
		sort.Slice(sessions, func(i, j int) bool {
			left, right := sessions[i], sessions[j]
			if left.Date != right.Date {
				return left.Date < right.Date
			}
			if !left.Start.Equal(right.Start) {
				return left.Start.Before(right.Start)
			}
			if left.Venue.Slug != right.Venue.Slug {
				return left.Venue.Slug < right.Venue.Slug
			}
			if left.Provider != right.Provider {
				return left.Provider < right.Provider
			}
			return left.ID < right.ID
		})
	}
	add := func(path string, content any) error {
		if _, exists := projection.Fingerprints[path]; exists {
			return fmt.Errorf("duplicate public detail path")
		}
		encoded, err := json.Marshal(content)
		if err != nil {
			return fmt.Errorf("encode public content: %w", err)
		}
		projection.Fingerprints[path] = sha256.Sum256(encoded)
		projection.Data.LastmodByPath[path] = nil
		return nil
	}
	order := view.allMovieOrder
	if len(view.data.PublicMovies) == 0 {
		order = view.movieOrder
	}
	for _, slug := range order {
		item := catalogMovieBySlug(view, slug)
		item.ShowtimeCount = currentCounts[slug]
		projection.Data.Movies = append(projection.Data.Movies, item)
		var backdrop *string
		index := view.movieBySlug[slug]
		if len(view.data.PublicMovies) > 0 {
			value := view.data.PublicMovies[index.publicMovie].BackdropURL
			if value != "" {
				backdrop = &value
			}
		} else {
			_, backdrop = materializeMovieMedia(view, view.data.Showtimes[index.firstShowtime].Movie)
		}
		discovery := aggregate.movie(view, slug)
		// A period absent from the rendered ended-film section is not clock evidence.
		if len(discovery.Cities) == 0 {
			discovery.Window = nil
		}
		programme := movieProgramme[slug]
		if programme == nil {
			programme = []pageSession{}
		}
		orderSessions(programme)
		content := filmPageContent{Movie: projectMovie(item, backdrop), ReleaseStatus: releaseStatus(view, slug, item, today, currentCounts[slug] > 0), CurrentlyScreened: currentCounts[slug] > 0, Dates: nonpastDates(view.movieDates[slug], today), Programme: programme, Discovery: discovery}
		if err := add("/film/"+url.PathEscape(slug), content); err != nil {
			return SitemapProjection{}, err
		}
	}
	sort.Slice(projection.Data.Movies, func(i, j int) bool {
		return compareMovieCatalogTitle(projection.Data.Movies[i], projection.Data.Movies[j], false)
	})
	projection.Data.MovieTotal = len(projection.Data.Movies)
	if view.catalogOnly {
		return projection, nil
	}
	inventory := cities(view)
	projection.Data.Cities = &inventory
	for position, theater := range view.data.Theaters {
		discovery := aggregate.theater(view, position)
		cards := make([]pageMovieCard, 0, len(discovery.Movies))
		for _, item := range discovery.Movies {
			cards = append(cards, projectCard(item))
		}
		if len(cards) == 0 && len(discovery.OtherTheaters) == 0 {
			discovery.Window = nil
		}
		programme := theaterProgramme[position]
		if programme == nil {
			programme = []pageSession{}
		}
		orderSessions(programme)
		var image *PublicPageImage
		if value, ok := images[theater.ID]; ok {
			image = &value
		}
		content := cinemaPageContent{Venue: projectVenue(materializeTheater(view, position)), Image: image, AvailableDateCount: len(theater.AvailableDates), Dates: nonpastDates(theater.AvailableDates, today), DefaultDate: today, Programme: programme, Discovery: theaterPageDiscovery{Window: discovery.Window, Movies: cards, OtherTheaters: discovery.OtherTheaters}}
		if err := add("/cinema/"+url.PathEscape(theater.Slug), content); err != nil {
			return SitemapProjection{}, err
		}
	}
	for cityPosition, bucket := range view.cityBuckets {
		theaters := make([]Theater, 0, len(bucket.positions))
		for _, position := range bucket.positions {
			theaters = append(theaters, materializeTheater(view, position))
		}
		sort.Slice(theaters, func(i, j int) bool {
			if comparison := compareFolded(theaters[i].Name, theaters[j].Name); comparison != 0 {
				return comparison < 0
			}
			return theaters[i].Slug < theaters[j].Slug
		})
		venues := make([]pageVenue, 0, len(theaters))
		for _, theater := range theaters {
			venues = append(venues, projectVenue(theater))
		}
		items := make([]MovieCatalogItem, 0, len(cityCounts[cityPosition]))
		for slug, count := range cityCounts[cityPosition] {
			item := catalogMovieBySlug(view, slug)
			item.ShowtimeCount = count
			items = append(items, item)
		}
		sort.Slice(items, func(i, j int) bool { return compareMovieCatalogTitle(items[i], items[j], false) })
		cards := make([]pageMovieCard, 0, min(24, len(items)))
		for _, item := range items[:min(24, len(items))] {
			cards = append(cards, projectCard(item))
		}
		content := cityPageContent{City: City{Name: bucket.city, Slug: bucket.slug}, Venues: venues, MovieCount: len(bucket.movieSlugs), Catalogue: cards, Total: len(items), PageSize: 24, Discovery: aggregate.city(view, theaters)}
		if err := add("/ville/"+url.PathEscape(bucket.slug)+"/cinemas", content); err != nil {
			return SitemapProjection{}, err
		}
	}
	return projection, nil
}
