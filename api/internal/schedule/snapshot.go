package schedule

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
)

const maxScheduleAge = 24 * time.Hour

type theaterDateKey struct {
	theaterID string
	date      string
}

type movieDateKey struct {
	slug string
	date string
}

type cityBucket struct {
	city             string
	slug             string
	positions        []int
	catalogPositions []int
	movieSlugs       []string
}

type movieTitleVariant struct {
	title         string
	firstShowtime int
	count         int
}

type movieIndex struct {
	firstShowtime int
	variants      []movieTitleVariant
	publicMovie   int
}

// SnapshotView is a detached, immutable schedule snapshot. Callers can retain
// a view while a source publishes a newer revision.
type SnapshotView struct {
	data             Dataset
	readiness        snapshotReadiness
	theaterByID      map[string]int
	theaterBySlug    map[string]int
	cityBuckets      []cityBucket
	cityBucketByFold map[string]int
	cityBucketBySlug map[string]int
	theaterCity      []int
	theaterDate      map[theaterDateKey][]int
	theaterShowtimes [][]int
	movieBySlug      map[string]movieIndex
	movieAlias       map[string]string
	publicMovieByID  map[int64]int
	movieOrder       []string
	allMovieOrder    []string
	movieDate        map[movieDateKey][]int
	movieDates       map[string][]string
	theaterPositions []int
	theaterCatalog   []int
	theaterRank      []int
	catalogRevision  string
}

type snapshotReadiness struct {
	complete    bool
	generatedAt time.Time
	windowFrom  time.Time
	windowEnd   time.Time
}

// NewSnapshotView detaches data from its caller and builds request indexes.
// Dataset policy validation remains the caller's responsibility.
func NewSnapshotView(data Dataset, revisions ...SnapshotRevision) *SnapshotView {
	view := &SnapshotView{
		data:             cloneDataset(data),
		theaterByID:      make(map[string]int, len(data.Theaters)),
		theaterBySlug:    make(map[string]int, len(data.Theaters)),
		cityBucketByFold: make(map[string]int),
		cityBucketBySlug: make(map[string]int),
		theaterDate:      make(map[theaterDateKey][]int),
		theaterShowtimes: make([][]int, len(data.Theaters)),
		movieBySlug:      make(map[string]movieIndex),
		movieAlias:       make(map[string]string),
		publicMovieByID:  make(map[int64]int),
		movieDate:        make(map[movieDateKey][]int),
		movieDates:       make(map[string][]string),
		theaterPositions: make([]int, len(data.Theaters)),
		theaterCatalog:   make([]int, len(data.Theaters)),
		theaterRank:      make([]int, len(data.Theaters)),
		theaterCity:      make([]int, len(data.Theaters)),
	}
	view.readiness = newSnapshotReadiness(view.data)
	if len(revisions) > 0 {
		view.catalogRevision = fmt.Sprintf("schedule:%d;enrichment:%d", revisions[0].ScheduleVersion, revisions[0].EnrichmentVersion)
	}
	for position, movie := range view.data.PublicMovies {
		view.publicMovieByID[movie.ID] = position
		if movie.RedirectToID != 0 {
			continue
		}
		slug := publicMovieIDSlug(movie.ID)
		view.movieBySlug[slug] = movieIndex{firstShowtime: -1, publicMovie: position}
		view.allMovieOrder = append(view.allMovieOrder, slug)
	}
	for _, movie := range view.data.PublicMovies {
		if movie.RedirectToID == 0 {
			continue
		}
		if target, exists := view.publicMovieByID[movie.RedirectToID]; exists && view.data.PublicMovies[target].RedirectToID == 0 {
			view.movieAlias[publicMovieIDSlug(movie.ID)] = publicMovieIDSlug(movie.RedirectToID)
		}
	}
	for _, alias := range view.data.MovieAliases {
		if target, exists := view.publicMovieByID[alias.PublicMovieID]; exists && view.data.PublicMovies[target].RedirectToID == 0 {
			view.movieAlias[alias.Slug] = publicMovieIDSlug(alias.PublicMovieID)
		}
	}
	labelsByFold := make(map[string][]string)
	for position, theater := range view.data.Theaters {
		view.theaterByID[theater.ID] = position
		view.theaterBySlug[theater.Slug] = position
		view.theaterPositions[position] = position
		view.theaterCatalog[position] = position
		cityKey := foldKey(theater.City)
		bucket, exists := view.cityBucketByFold[cityKey]
		if !exists {
			view.cityBuckets = append(view.cityBuckets, cityBucket{city: theater.City})
			bucket = len(view.cityBuckets) - 1
			view.cityBucketByFold[cityKey] = bucket
		}
		labelsByFold[cityKey] = append(labelsByFold[cityKey], theater.City)
		view.theaterCity[position] = bucket
		view.cityBuckets[bucket].positions = append(view.cityBuckets[bucket].positions, position)
	}
	for _, identity := range buildCityIdentities(labelsByFold) {
		bucket := view.cityBucketByFold[identity.foldKey]
		view.cityBuckets[bucket].city = identity.name
		view.cityBuckets[bucket].slug = identity.slug
		view.cityBucketBySlug[identity.slug] = bucket
	}
	sort.Slice(view.theaterCatalog, func(i, j int) bool {
		left, right := view.data.Theaters[view.theaterCatalog[i]], view.data.Theaters[view.theaterCatalog[j]]
		if comparison := compareNormalized(left.City, right.City); comparison != 0 {
			return comparison < 0
		}
		if comparison := compareNormalized(left.Name, right.Name); comparison != 0 {
			return comparison < 0
		}
		return left.ID < right.ID
	})
	for rank, position := range view.theaterCatalog {
		view.theaterRank[position] = rank
		cityKey := foldKey(view.data.Theaters[position].City)
		bucket := view.cityBucketByFold[cityKey]
		view.cityBuckets[bucket].catalogPositions = append(view.cityBuckets[bucket].catalogPositions, position)
	}
	variantPositions := make(map[string]map[string]int)
	movieDateSeen := make(map[movieDateKey]bool)
	cityMovies := make([]map[string]bool, len(view.cityBuckets))
	for index := range cityMovies {
		cityMovies[index] = make(map[string]bool)
	}
	for position, showing := range view.data.Showtimes {
		view.theaterDate[theaterDateKey{theaterID: showing.TheaterID, date: showing.ServiceDate}] = append(view.theaterDate[theaterDateKey{theaterID: showing.TheaterID, date: showing.ServiceDate}], position)
		slug := view.publicMovieSlug(showing.Movie)
		if theaterPosition, exists := view.theaterByID[showing.TheaterID]; exists {
			view.theaterShowtimes[theaterPosition] = append(view.theaterShowtimes[theaterPosition], position)
			cityMovies[view.theaterCity[theaterPosition]][slug] = true
		}
		view.movieDate[movieDateKey{slug: slug, date: showing.ServiceDate}] = append(view.movieDate[movieDateKey{slug: slug, date: showing.ServiceDate}], position)
		dateKey := movieDateKey{slug: slug, date: showing.ServiceDate}
		if !movieDateSeen[dateKey] {
			movieDateSeen[dateKey] = true
			view.movieDates[slug] = append(view.movieDates[slug], showing.ServiceDate)
		}
		movie, exists := view.movieBySlug[slug]
		if !exists || movie.firstShowtime < 0 {
			movie.firstShowtime = position
			view.movieOrder = append(view.movieOrder, slug)
			variantPositions[slug] = make(map[string]int)
		}
		title := normalized(view.movieTitle(showing.Movie))
		variantPosition, exists := variantPositions[slug][title]
		if !exists {
			variantPosition = len(movie.variants)
			variantPositions[slug][title] = variantPosition
			movie.variants = append(movie.variants, movieTitleVariant{title: title, firstShowtime: position})
		}
		movie.variants[variantPosition].count++
		view.movieBySlug[slug] = movie
	}
	for bucket := range view.cityBuckets {
		for slug := range cityMovies[bucket] {
			view.cityBuckets[bucket].movieSlugs = append(view.cityBuckets[bucket].movieSlugs, slug)
		}
		sort.Strings(view.cityBuckets[bucket].movieSlugs)
	}
	for slug := range view.movieDates {
		sort.Strings(view.movieDates[slug])
	}
	return view
}

// ReadyAt reports whether the view contains a complete schedule that is fresh
// and covers the current calendar date in Europe/Paris.
func (v *SnapshotView) ReadyAt(now time.Time) bool {
	if v == nil || !v.readiness.complete || now.IsZero() {
		return false
	}
	if v.readiness.generatedAt.After(now) || now.Sub(v.readiness.generatedAt) > maxScheduleAge {
		return false
	}
	localNow := now.In(v.readiness.windowFrom.Location())
	today := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, localNow.Location())
	return !today.Before(v.readiness.windowFrom) && today.Before(v.readiness.windowEnd)
}

func newSnapshotReadiness(data Dataset) snapshotReadiness {
	if ValidateDataset(data, true) != nil {
		return snapshotReadiness{}
	}
	location, err := time.LoadLocation(Timezone)
	if err != nil {
		return snapshotReadiness{}
	}
	from, err := time.ParseInLocation(dateLayout, data.Window.From, location)
	if err != nil {
		return snapshotReadiness{}
	}
	through, err := time.ParseInLocation(dateLayout, data.Window.Through, location)
	if err != nil {
		return snapshotReadiness{}
	}
	return snapshotReadiness{
		complete:    true,
		generatedAt: data.GeneratedAt,
		windowFrom:  from,
		windowEnd:   through.AddDate(0, 0, 1),
	}
}

func publicMovieIDSlug(id int64) string { return fmt.Sprintf("film-%d", id) }

func (v *SnapshotView) publicMovieSlug(record MovieRecord) string {
	if record.PublicMovieID > 0 {
		return publicMovieIDSlug(record.PublicMovieID)
	}
	return legacyPublicMovieSlug(record)
}

func (v *SnapshotView) movieTitle(record MovieRecord) string {
	if position, ok := v.publicMovieByID[record.PublicMovieID]; ok {
		return v.data.PublicMovies[position].Title
	}
	return record.Title
}

func (v *SnapshotView) resolveMovieSlug(slug string) (string, bool) {
	if _, ok := v.movieBySlug[slug]; ok {
		return slug, true
	}
	canonical, ok := v.movieAlias[slug]
	return canonical, ok
}

func (v *SnapshotView) positionsForCities(cities []string) []int {
	if len(cities) == 0 {
		return append([]int(nil), v.theaterPositions...)
	}
	positions := make([]int, 0)
	seen := make(map[int]bool)
	for _, city := range cities {
		bucketPosition, matched := v.cityBucketByFold[foldKey(city)]
		if !matched {
			continue
		}
		bucket := v.cityBuckets[bucketPosition]
		for _, position := range bucket.positions {
			if !seen[position] {
				seen[position] = true
				positions = append(positions, position)
			}
		}
	}
	sort.Ints(positions)
	return positions
}

func (v *SnapshotView) catalogPositionsForCities(cities []string) []int {
	if len(cities) == 0 {
		return nil
	}
	positions := make([]int, 0)
	matchedBuckets := 0
	for _, city := range cities {
		bucketPosition, matched := v.cityBucketByFold[foldKey(city)]
		if !matched {
			continue
		}
		matchedBuckets++
		positions = append(positions, v.cityBuckets[bucketPosition].catalogPositions...)
	}
	if matchedBuckets < 2 {
		return positions
	}
	sort.Slice(positions, func(i, j int) bool {
		return v.theaterRank[positions[i]] < v.theaterRank[positions[j]]
	})
	return positions
}

func foldKey(value string) string {
	var result strings.Builder
	result.Grow(len(value))
	for _, current := range value {
		minimum := current
		for candidate := unicode.SimpleFold(current); candidate != current; candidate = unicode.SimpleFold(candidate) {
			if candidate < minimum {
				minimum = candidate
			}
		}
		result.WriteRune(minimum)
	}
	return result.String()
}
