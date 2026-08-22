package schedule

import (
	"fmt"
	"sort"
	"strings"
)

type Publication struct {
	Dataset Dataset
	Movies  []MovieRecord
}

func PreparePublication(data Dataset) (Publication, error) {
	if err := ValidateDataset(data, true); err != nil {
		return Publication{}, err
	}
	data = cloneDataset(data)
	for index := range data.Theaters {
		data.Theaters[index].Provider = recordProvider(data.Theaters[index].Provider, data.Theaters[index].ID)
	}
	for index := range data.Showtimes {
		data.Showtimes[index].Provider = recordProvider(data.Showtimes[index].Provider, data.Showtimes[index].ID)
		data.Showtimes[index].Movie.Provider = recordProvider(data.Showtimes[index].Movie.Provider, data.Showtimes[index].Movie.Slug)
	}
	normalizeDataset(&data)
	byID := make(map[string]MovieRecord)
	for _, showing := range data.Showtimes {
		movie := showing.Movie
		movie.Provider = recordProvider(movie.Provider, movie.Slug)
		key := string(movie.Provider) + "\x00" + movie.ProviderID
		if prior, exists := byID[key]; exists && !samePublishedMovie(prior, movie) {
			return Publication{}, fmt.Errorf("conflicting movie metadata")
		}
		byID[key] = movie
	}
	movies := make([]MovieRecord, 0, len(byID))
	for _, movie := range byID {
		movies = append(movies, movie)
	}
	sort.Slice(movies, func(i, j int) bool {
		return string(movies[i].Provider)+"\x00"+movies[i].ProviderID < string(movies[j].Provider)+"\x00"+movies[j].ProviderID
	})
	return Publication{Dataset: data, Movies: movies}, nil
}

func samePublishedMovie(a, b MovieRecord) bool {
	return a.Provider == b.Provider && a.ProviderID == b.ProviderID && a.Slug == b.Slug && a.Title == b.Title && a.RuntimeMinutes == b.RuntimeMinutes && a.PosterURL == b.PosterURL && a.Overview == b.Overview && a.ReleaseDate == b.ReleaseDate && strings.Join(a.Genres, "\x00") == strings.Join(b.Genres, "\x00")
}
