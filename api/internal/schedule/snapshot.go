package schedule

import (
	"sort"
	"strings"
	"unicode"
)

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
}

// SnapshotView is a detached, immutable schedule snapshot. Callers can retain
// a view while a source publishes a newer revision.
type SnapshotView struct {
	data             Dataset
	theaterByID      map[string]int
	theaterBySlug    map[string]int
	cityBuckets      []cityBucket
	cityBucketByFold map[string]int
	cityBucketBySlug map[string]int
	theaterCity      []int
	theaterDate      map[theaterDateKey][]int
	theaterShowtimes [][]int
	movieBySlug      map[string]movieIndex
	movieOrder       []string
	movieDate        map[movieDateKey][]int
	theaterPositions []int
	theaterCatalog   []int
	theaterRank      []int
}

// NewSnapshotView detaches data from its caller and builds request indexes.
// Dataset policy validation remains the caller's responsibility.
func NewSnapshotView(data Dataset) *SnapshotView {
	view := &SnapshotView{
		data:             cloneDataset(data),
		theaterByID:      make(map[string]int, len(data.Theaters)),
		theaterBySlug:    make(map[string]int, len(data.Theaters)),
		cityBucketByFold: make(map[string]int),
		cityBucketBySlug: make(map[string]int),
		theaterDate:      make(map[theaterDateKey][]int),
		theaterShowtimes: make([][]int, len(data.Theaters)),
		movieBySlug:      make(map[string]movieIndex),
		movieDate:        make(map[movieDateKey][]int),
		theaterPositions: make([]int, len(data.Theaters)),
		theaterCatalog:   make([]int, len(data.Theaters)),
		theaterRank:      make([]int, len(data.Theaters)),
		theaterCity:      make([]int, len(data.Theaters)),
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
	cityMovies := make([]map[string]bool, len(view.cityBuckets))
	for index := range cityMovies {
		cityMovies[index] = make(map[string]bool)
	}
	for position, showing := range view.data.Showtimes {
		view.theaterDate[theaterDateKey{theaterID: showing.TheaterID, date: showing.ServiceDate}] = append(view.theaterDate[theaterDateKey{theaterID: showing.TheaterID, date: showing.ServiceDate}], position)
		slug := publicMovieSlug(showing.Movie)
		if theaterPosition, exists := view.theaterByID[showing.TheaterID]; exists {
			view.theaterShowtimes[theaterPosition] = append(view.theaterShowtimes[theaterPosition], position)
			cityMovies[view.theaterCity[theaterPosition]][slug] = true
		}
		view.movieDate[movieDateKey{slug: slug, date: showing.ServiceDate}] = append(view.movieDate[movieDateKey{slug: slug, date: showing.ServiceDate}], position)
		movie, exists := view.movieBySlug[slug]
		if !exists {
			movie.firstShowtime = position
			view.movieOrder = append(view.movieOrder, slug)
			variantPositions[slug] = make(map[string]int)
		}
		title := normalized(showing.Movie.Title)
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
	return view
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
