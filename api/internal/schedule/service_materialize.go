package schedule

import (
	"strconv"
	"strings"
	"time"
)

func normalized(value string) string    { return strings.ToLower(strings.TrimSpace(value)) }
func compareNormalized(a, b string) int { return strings.Compare(normalized(a), normalized(b)) }
func localTime(date time.Time, hour, minute int) time.Time {
	return time.Date(date.Year(), date.Month(), date.Day(), hour, minute, 0, 0, date.Location())
}
func recordProvider(explicit Provider, identity string) Provider {
	if explicit != "" {
		return explicit
	}
	if strings.HasPrefix(identity, string(ProviderKinepolis)+"-") {
		return ProviderKinepolis
	}
	return ProviderUGC
}
func invalid(message string) error { return &ValidationError{Message: message} }

func materializeRecord(record ShowtimeRecord) Showtime {
	booking := record.BookingURL
	provider := recordProvider(record.Provider, record.ID)
	return Showtime{Provider: provider, ID: record.ID, Movie: Movie{Provider: provider, Slug: publicMovieSlug(record.Movie), Title: record.Movie.Title, RuntimeMinutes: record.Movie.RuntimeMinutes}, StartTime: record.StartTime.UTC(), EndTime: record.EndTime.UTC(), Language: record.Language, Format: record.Format, Room: record.Room, BookingURL: &booking}
}

func materializeCatalogMovie(record MovieRecord) MovieCatalogItem {
	poster, _ := materializeMovieMedia(record)
	item := MovieCatalogItem{Provider: recordProvider(record.Provider, record.Slug), Slug: publicMovieSlug(record), Title: record.Title, RuntimeMinutes: record.RuntimeMinutes, PosterURL: poster, Genres: append([]string{}, record.Genres...)}
	if record.Overview != "" {
		value := record.Overview
		item.Overview = &value
	}
	if record.ReleaseDate != "" {
		value := record.ReleaseDate
		item.ReleaseDate = &value
	}
	if record.Enrichment != nil && record.Enrichment.TMDBID > 0 {
		id := record.Enrichment.TMDBID
		item.TMDBID = &id
		if record.Enrichment.Overview != "" {
			value := record.Enrichment.Overview
			item.Overview = &value
		}
		if record.Enrichment.ReleaseDate != "" {
			value := record.Enrichment.ReleaseDate
			item.ReleaseDate = &value
		}
		if len(record.Enrichment.Genres) > 0 {
			item.Genres = append([]string{}, record.Enrichment.Genres...)
		}
	}
	return item
}

func materializeMovieMedia(record MovieRecord) (*string, *string) {
	var poster *string
	if record.PosterURL != "" {
		value := record.PosterURL
		poster = &value
	}
	var backdrop *string
	if record.Enrichment != nil {
		if record.Enrichment.TMDBID > 0 && record.Enrichment.PosterURL != "" {
			value := record.Enrichment.PosterURL
			poster = &value
		}
		if validTMDBBackdropURL(record.Enrichment.BackdropURL) {
			value := record.Enrichment.BackdropURL
			backdrop = &value
		}
	}
	return poster, backdrop
}

func publicMovieSlug(record MovieRecord) string {
	if record.LocalMovieID > 0 {
		return "local-film-" + strconv.FormatInt(record.LocalMovieID, 10)
	}
	if record.Enrichment != nil && record.Enrichment.TMDBID > 0 {
		return "tmdb-film-" + strconv.FormatInt(record.Enrichment.TMDBID, 10)
	}
	return record.Slug
}
