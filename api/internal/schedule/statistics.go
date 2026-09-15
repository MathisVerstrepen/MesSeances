package schedule

import "time"

// StatisticsQuery matches any selected city and any selected theater, intersected
// with the scalar filters. Empty lists and absent scalar fields mean all.
type StatisticsQuery struct {
	Date, DateTo                         string
	City, Theater                        []string
	Chain, Language, Format, Genre, Pass string
}

type Statistics struct {
	GeneratedAt   time.Time               `json:"generated_at"`
	Timezone      string                  `json:"timezone"`
	Range         Window                  `json:"range"`
	Coverage      StatisticsCoverage      `json:"coverage"`
	Options       StatisticsOptions       `json:"options"`
	Totals        StatisticsTotals        `json:"totals"`
	TopMovies     StatisticsTopMovies     `json:"top_movies"`
	Heatmap       []StatisticsHeatmapCell `json:"heatmap"`
	Versions      []StatisticsCountBucket `json:"versions"`
	Formats       []StatisticsCountBucket `json:"formats"`
	Genres        []StatisticsCountBucket `json:"genres"`
	Runtimes      []StatisticsCountBucket `json:"runtimes"`
	Local         StatisticsLocal         `json:"local"`
	Concentration StatisticsConcentration `json:"concentration"`
}

type StatisticsCoverage struct {
	SnapshotWindow Window  `json:"snapshot_window"`
	Intersection   *Window `json:"intersection"`
	Completeness   string  `json:"completeness"`
	Stale          bool    `json:"stale"`
}

type StatisticsOptions struct {
	Cities    []City                    `json:"cities"`
	Theaters  []StatisticsTheaterOption `json:"theaters"`
	Chains    []Provider                `json:"chains"`
	Languages []string                  `json:"languages"`
	Formats   []string                  `json:"formats"`
	Genres    []StatisticsGenreOption   `json:"genres"`
	Passes    []string                  `json:"passes"`
}

type StatisticsTheaterOption struct {
	ID       string   `json:"id"`
	Slug     string   `json:"slug"`
	Name     string   `json:"name"`
	City     string   `json:"city"`
	CitySlug string   `json:"city_slug"`
	Chain    Provider `json:"chain"`
	Passes   []string `json:"passes"`
}

type StatisticsGenreOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type StatisticsTotals struct {
	Showtimes int `json:"showtimes"`
	Movies    int `json:"movies"`
	Theaters  int `json:"theaters"`
	Cities    int `json:"cities"`
}

type StatisticsTopMovies struct {
	ByShowtimes []StatisticsMovieRank `json:"by_showtimes"`
	ByTheaters  []StatisticsMovieRank `json:"by_theaters"`
}

type StatisticsMovieRank struct {
	Slug          string `json:"slug"`
	Title         string `json:"title"`
	ShowtimeCount int    `json:"showtime_count"`
	TheaterCount  int    `json:"theater_count"`
}

type StatisticsHeatmapCell struct {
	Weekday       int `json:"weekday"`
	Hour          int `json:"hour"`
	ShowtimeCount int `json:"showtime_count"`
}

type StatisticsCountBucket struct {
	Value string `json:"value"`
	Label string `json:"label"`
	Count int    `json:"count"`
}

type StatisticsLocal struct {
	Cities   []StatisticsCityRank    `json:"cities"`
	Theaters []StatisticsTheaterRank `json:"theaters"`
}

type StatisticsCityRank struct {
	Slug          string `json:"slug"`
	Name          string `json:"name"`
	ShowtimeCount int    `json:"showtime_count"`
	MovieCount    int    `json:"movie_count"`
	TheaterCount  int    `json:"theater_count"`
}

type StatisticsTheaterRank struct {
	ID            string   `json:"id"`
	Slug          string   `json:"slug"`
	Name          string   `json:"name"`
	City          string   `json:"city"`
	CitySlug      string   `json:"city_slug"`
	Chain         Provider `json:"chain"`
	ShowtimeCount int      `json:"showtime_count"`
	MovieCount    int      `json:"movie_count"`
}

type StatisticsConcentration struct {
	TopMovieCount      int `json:"top_movie_count"`
	TopShowtimeCount   int `json:"top_showtime_count"`
	OtherShowtimeCount int `json:"other_showtime_count"`
}
