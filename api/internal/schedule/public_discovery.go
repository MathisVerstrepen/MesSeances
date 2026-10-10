package schedule

import (
	"sort"
	"time"
)

// DiscoveryWindow describes inclusive scheduled service dates, not future-only sessions.
type DiscoveryWindow struct {
	From     string `json:"from"`
	Through  string `json:"through"`
	Timezone string `json:"timezone"`
}

type DiscoveryCity struct {
	Name          string `json:"name"`
	Slug          string `json:"slug"`
	TheaterCount  int    `json:"theater_count"`
	ShowtimeCount int    `json:"showtime_count"`
}

type MovieDiscovery struct {
	Window *DiscoveryWindow `json:"window"`
	Cities []DiscoveryCity  `json:"cities"`
}

type DiscoveryTheater struct {
	Provider      Provider `json:"provider"`
	ID            string   `json:"id"`
	Slug          string   `json:"slug"`
	Name          string   `json:"name"`
	City          string   `json:"city"`
	CitySlug      string   `json:"city_slug"`
	MovieCount    int      `json:"movie_count"`
	ShowtimeCount int      `json:"showtime_count"`
}

type TheaterDiscovery struct {
	Window        *DiscoveryWindow   `json:"window"`
	Movies        []MovieCatalogItem `json:"movies"`
	OtherTheaters []DiscoveryTheater `json:"other_theaters"`
}

type CityTheaterProgramme struct {
	ID            string `json:"id"`
	MovieCount    int    `json:"movie_count"`
	ShowtimeCount int    `json:"showtime_count"`
}

type CityDiscovery struct {
	Window   *DiscoveryWindow       `json:"window"`
	Theaters []CityTheaterProgramme `json:"theaters"`
}

type programmeAggregate struct {
	window        *DiscoveryWindow
	movieCities   map[string]map[int]*cityProgramme
	theaterMovies []map[string]int
	theaterCounts []int
}

type cityProgramme struct {
	theaters map[string]bool
	count    int
}

// aggregateProgramme makes one pass over sessions, independent of catalogue size.
func aggregateProgramme(view *SnapshotView, now time.Time) programmeAggregate {
	a := programmeAggregate{movieCities: make(map[string]map[int]*cityProgramme), theaterMovies: make([]map[string]int, len(view.data.Theaters)), theaterCounts: make([]int, len(view.data.Theaters))}
	if view.catalogOnly {
		return a
	}
	location := view.readiness.windowFrom.Location()
	if !view.readiness.complete {
		return a
	}
	local := now.In(location)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
	from, through := today, today.AddDate(0, 0, 6)
	if from.Before(view.readiness.windowFrom) {
		from = view.readiness.windowFrom
	}
	end := view.readiness.windowEnd.AddDate(0, 0, -1)
	if through.After(end) {
		through = end
	}
	if from.After(through) {
		return a
	}
	a.window = &DiscoveryWindow{From: from.Format(dateLayout), Through: through.Format(dateLayout), Timezone: Timezone}
	seen := make(map[string]bool, len(view.data.Showtimes))
	for _, record := range view.data.Showtimes {
		if record.ServiceDate < a.window.From || record.ServiceDate > a.window.Through {
			continue
		}
		key := string(recordProvider(record.Provider, record.ID)) + "\x00" + record.ID
		if seen[key] {
			continue
		}
		seen[key] = true
		position, ok := view.theaterByID[record.TheaterID]
		if !ok {
			continue
		}
		slug := view.publicMovieSlug(record.Movie)
		if a.theaterMovies[position] == nil {
			a.theaterMovies[position] = make(map[string]int)
		}
		a.theaterMovies[position][slug]++
		a.theaterCounts[position]++
		city := view.theaterCity[position]
		if a.movieCities[slug] == nil {
			a.movieCities[slug] = make(map[int]*cityProgramme)
		}
		if a.movieCities[slug][city] == nil {
			a.movieCities[slug][city] = &cityProgramme{theaters: make(map[string]bool)}
		}
		entry := a.movieCities[slug][city]
		entry.theaters[record.TheaterID] = true
		entry.count++
	}
	return a
}

func (a programmeAggregate) movie(view *SnapshotView, slug string) MovieDiscovery {
	result := MovieDiscovery{Window: a.window, Cities: []DiscoveryCity{}}
	for position, counts := range a.movieCities[slug] {
		city := view.cityBuckets[position]
		result.Cities = append(result.Cities, DiscoveryCity{Name: city.city, Slug: city.slug, TheaterCount: len(counts.theaters), ShowtimeCount: counts.count})
	}
	sort.Slice(result.Cities, func(i, j int) bool {
		left, right := result.Cities[i], result.Cities[j]
		if left.ShowtimeCount != right.ShowtimeCount {
			return left.ShowtimeCount > right.ShowtimeCount
		}
		if comparison := compareFolded(left.Name, right.Name); comparison != 0 {
			return comparison < 0
		}
		return left.Slug < right.Slug
	})
	result.Cities = result.Cities[:min(6, len(result.Cities))]
	return result
}

func catalogMovieBySlug(view *SnapshotView, slug string) MovieCatalogItem {
	index := view.movieBySlug[slug]
	if len(view.data.PublicMovies) > 0 {
		return materializePublicMovie(view.data.PublicMovies[index.publicMovie])
	}
	return materializeCatalogMovie(view, view.data.Showtimes[index.firstShowtime].Movie)
}

func (a programmeAggregate) theater(view *SnapshotView, position int) TheaterDiscovery {
	result := TheaterDiscovery{Window: a.window, Movies: []MovieCatalogItem{}, OtherTheaters: []DiscoveryTheater{}}
	for slug, count := range a.theaterMovies[position] {
		item := catalogMovieBySlug(view, slug)
		item.ShowtimeCount = count
		result.Movies = append(result.Movies, item)
	}
	sort.Slice(result.Movies, func(i, j int) bool {
		if result.Movies[i].ShowtimeCount != result.Movies[j].ShowtimeCount {
			return result.Movies[i].ShowtimeCount > result.Movies[j].ShowtimeCount
		}
		return compareMovieCatalogTitle(result.Movies[i], result.Movies[j], false)
	})
	result.Movies = result.Movies[:min(6, len(result.Movies))]
	current := view.data.Theaters[position]
	for _, other := range view.cityBuckets[view.theaterCity[position]].positions {
		theater := view.data.Theaters[other]
		if theater.ID == current.ID || theater.Slug == current.Slug || a.theaterCounts[other] == 0 {
			continue
		}
		result.OtherTheaters = append(result.OtherTheaters, DiscoveryTheater{Provider: recordProvider(theater.Provider, theater.ID), ID: theater.ID, Slug: theater.Slug, Name: theater.Name, City: theater.City, CitySlug: view.cityBuckets[view.theaterCity[other]].slug, MovieCount: len(a.theaterMovies[other]), ShowtimeCount: a.theaterCounts[other]})
	}
	sort.Slice(result.OtherTheaters, func(i, j int) bool {
		left, right := result.OtherTheaters[i], result.OtherTheaters[j]
		if left.ShowtimeCount != right.ShowtimeCount {
			return left.ShowtimeCount > right.ShowtimeCount
		}
		if comparison := compareFolded(left.Name, right.Name); comparison != 0 {
			return comparison < 0
		}
		return left.Slug < right.Slug
	})
	result.OtherTheaters = result.OtherTheaters[:min(6, len(result.OtherTheaters))]
	return result
}

func (a programmeAggregate) city(view *SnapshotView, theaters []Theater) CityDiscovery {
	result := CityDiscovery{Window: a.window, Theaters: make([]CityTheaterProgramme, 0, len(theaters))}
	for _, theater := range theaters {
		position := view.theaterByID[theater.ID]
		result.Theaters = append(result.Theaters, CityTheaterProgramme{ID: theater.ID, MovieCount: len(a.theaterMovies[position]), ShowtimeCount: a.theaterCounts[position]})
	}
	return result
}
