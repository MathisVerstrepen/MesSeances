package schedule

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrHistoryUnavailable  = errors.New("history unavailable")
	ErrHistoryBusy         = errors.New("history busy")
	ErrHistoryQueryTimeout = errors.New("history query timeout")
)

type HistoryStatistics struct {
	Mode          string                  `json:"mode"`
	GeneratedAt   time.Time               `json:"generated_at"`
	Timezone      string                  `json:"timezone"`
	Range         *Window                 `json:"range"`
	Coverage      HistoryCoverage         `json:"coverage"`
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
	Limits        HistoryLimits           `json:"limits"`
}

type HistoryCoverage struct {
	CollectionStartedAt *time.Time                `json:"collection_started_at"`
	LastPublicationAt   *time.Time                `json:"last_publication_at"`
	RecordedWindow      *Window                   `json:"recorded_window"`
	Completeness        string                    `json:"completeness"`
	Bootstrap           string                    `json:"bootstrap"`
	Providers           []HistoryProviderCoverage `json:"providers"`
}

type HistoryProviderCoverage struct {
	Provider            Provider  `json:"provider"`
	CollectionStartedAt time.Time `json:"collection_started_at"`
	LastPublicationAt   time.Time `json:"last_publication_at"`
	SourceGeneratedAt   time.Time `json:"source_generated_at"`
}

type HistoryOptionLimits struct {
	Cities   bool `json:"cities"`
	Theaters bool `json:"theaters"`
	Genres   bool `json:"genres"`
	Passes   bool `json:"passes"`
}

type HistoryLocalLimits struct {
	Cities   bool `json:"cities"`
	Theaters bool `json:"theaters"`
}

type HistoryLimits struct {
	Options HistoryOptionLimits `json:"options"`
	Genres  bool                `json:"genres"`
	Local   HistoryLocalLimits  `json:"local"`
}

type HistoryOptionsQuery struct {
	Kind     string
	Q        string
	Selected []string
}

type HistoryOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type HistoryOptions struct {
	Items    []HistoryOption `json:"items"`
	Selected []HistoryOption `json:"selected"`
	HasMore  bool            `json:"has_more"`
}

// NormalizeHistoryQuery validates direct callers as strictly as HTTP callers;
// unlike upcoming queries it imposes no today or duration boundary.
func NormalizeHistoryQuery(query StatisticsQuery) (StatisticsQuery, error) {
	var err error
	query.Film, err = statisticsNormalizeFilm(query.Film)
	if err != nil {
		return query, err
	}
	for _, values := range []*[]string{&query.City, &query.Theater} {
		for _, value := range *values {
			if !utf8.ValidString(value) {
				return query, invalid("Les filtres statistiques sont invalides.")
			}
		}
		*values, err = statisticsNormalizeSelection(*values)
		if err != nil {
			return query, err
		}
	}
	for _, value := range []*string{&query.Chain, &query.Language, &query.Format, &query.Genre, &query.Pass} {
		if len(*value) > 200 || !utf8.ValidString(*value) || *value != "" && strings.TrimSpace(*value) == "" {
			return query, invalid("Les filtres statistiques sont invalides.")
		}
		*value = strings.TrimSpace(*value)
	}
	query.Genre, _ = statisticsGenre(query.Genre)
	if query.Chain != "" && !validProvider(Provider(query.Chain), false) ||
		query.Language != "" && query.Language != statisticsUnknown && statisticsLanguage(Language(query.Language)) == statisticsUnknown ||
		query.Format != "" && query.Format != statisticsUnknown && statisticsFormat(Format(query.Format)) == statisticsUnknown {
		return query, invalid("Les filtres statistiques sont invalides.")
	}
	if query.Date == "" {
		if query.DateTo != "" {
			return query, invalid("La date de début est requise.")
		}
		return query, nil
	}
	if query.DateTo == "" {
		query.DateTo = query.Date
	}
	for _, value := range []string{query.Date, query.DateTo} {
		date, err := time.Parse("2006-01-02", value)
		if err != nil || date.Year() < 1 || date.Year() > 9999 || date.Format("2006-01-02") != value {
			return query, invalid("La période est invalide.")
		}
	}
	if query.DateTo < query.Date {
		return query, invalid("La période est invalide.")
	}
	return query, nil
}

func NormalizeHistoryOptionsQuery(query HistoryOptionsQuery) (HistoryOptionsQuery, error) {
	switch query.Kind {
	case "city", "theater", "genre", "pass":
	default:
		return query, invalid("Les options statistiques sont invalides.")
	}
	if len(query.Q) > 200 || !utf8.ValidString(query.Q) || query.Q != "" && strings.TrimSpace(query.Q) == "" {
		return query, invalid("Les options statistiques sont invalides.")
	}
	query.Q = normalized(query.Q)
	for _, value := range query.Selected {
		if !utf8.ValidString(value) {
			return query, invalid("Les options statistiques sont invalides.")
		}
	}
	values, err := statisticsNormalizeSelection(query.Selected)
	query.Selected = values
	return query, err
}
