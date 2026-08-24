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
	if strings.HasPrefix(identity, string(ProviderPathe)+"-") {
		return ProviderPathe
	}
	return ProviderUGC
}
func invalid(message string) error { return &ValidationError{Message: message} }

func materializeRecord(view *SnapshotView, record ShowtimeRecord) Showtime {
	booking := record.BookingURL
	provider := recordProvider(record.Provider, record.ID)
	movie := materializeCatalogMovie(view, record.Movie)
	return Showtime{Provider: provider, ID: record.ID, Movie: Movie{Slug: movie.Slug, Title: movie.Title, RuntimeMinutes: movie.RuntimeMinutes, UpdatedAt: movie.UpdatedAt}, StartTime: record.StartTime.UTC(), EndTime: record.EndTime.UTC(), Language: record.Language, Format: record.Format, Room: record.Room, BookingURL: &booking}
}

func materializeCatalogMovie(view *SnapshotView, record MovieRecord) MovieCatalogItem {
	if position, ok := view.publicMovieByID[record.PublicMovieID]; ok {
		return materializePublicMovie(view.data.PublicMovies[position])
	}
	poster, _ := materializeMovieMedia(view, record)
	item := MovieCatalogItem{Slug: legacyPublicMovieSlug(record), Title: record.Title, RuntimeMinutes: record.RuntimeMinutes, PosterURL: poster, Genres: append([]string{}, record.Genres...)}
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

func materializePublicMovie(record PublicMovieRecord) MovieCatalogItem {
	item := MovieCatalogItem{Slug: publicMovieIDSlug(record.ID), Title: record.Title, RuntimeMinutes: record.RuntimeMinutes, UpdatedAt: record.UpdatedAt, Genres: append([]string{}, record.Genres...)}
	if record.PosterURL != "" {
		value := record.PosterURL
		item.PosterURL = &value
	}
	if record.Overview != "" {
		value := record.Overview
		item.Overview = &value
	}
	if record.ReleaseDate != "" {
		value := record.ReleaseDate
		item.ReleaseDate = &value
	}
	if record.TMDBID > 0 {
		value := record.TMDBID
		item.TMDBID = &value
	}
	return item
}

func materializeMovieMedia(view *SnapshotView, record MovieRecord) (*string, *string) {
	if position, ok := view.publicMovieByID[record.PublicMovieID]; ok {
		movie := view.data.PublicMovies[position]
		var poster, backdrop *string
		if movie.PosterURL != "" {
			value := movie.PosterURL
			poster = &value
		}
		if movie.BackdropURL != "" {
			value := movie.BackdropURL
			backdrop = &value
		}
		return poster, backdrop
	}
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

func legacyPublicMovieSlug(record MovieRecord) string {
	if record.LocalMovieID > 0 {
		return "local-film-" + strconv.FormatInt(record.LocalMovieID, 10)
	}
	if record.Enrichment != nil && record.Enrichment.TMDBID > 0 {
		return "tmdb-film-" + strconv.FormatInt(record.Enrichment.TMDBID, 10)
	}
	return record.Slug
}
