package schedule

import (
	"sort"
	"strings"

	"golang.org/x/text/cases"
)

func (s *Service) Cities() CityInventory {
	view := s.source.Snapshot()
	result := CityInventory{GeneratedAt: view.data.GeneratedAt, Items: make([]CityInventoryItem, 0, len(view.cityBuckets))}
	for _, bucket := range view.cityBuckets {
		item := CityInventoryItem{Name: bucket.city, Slug: bucket.slug, Theaters: make([]CityTheater, 0, len(bucket.catalogPositions))}
		for _, position := range bucket.catalogPositions {
			theater := view.data.Theaters[position]
			item.Theaters = append(item.Theaters, CityTheater{Provider: recordProvider(theater.Provider, theater.ID), ID: theater.ID, Slug: theater.Slug, Name: theater.Name})
		}
		sort.Slice(item.Theaters, func(i, j int) bool {
			if comparison := compareFolded(item.Theaters[i].Name, item.Theaters[j].Name); comparison != 0 {
				return comparison < 0
			}
			return item.Theaters[i].Slug < item.Theaters[j].Slug
		})
		result.Items = append(result.Items, item)
	}
	sort.Slice(result.Items, func(i, j int) bool {
		if comparison := compareFolded(result.Items[i].Name, result.Items[j].Name); comparison != 0 {
			return comparison < 0
		}
		return result.Items[i].Slug < result.Items[j].Slug
	})
	return result
}

func (s *Service) City(slug string) (CityDetail, error) {
	view := s.source.Snapshot()
	bucketPosition, found := view.cityBucketBySlug[slug]
	if !found {
		return CityDetail{}, &NotFoundError{Message: "Ville introuvable."}
	}
	bucket := view.cityBuckets[bucketPosition]
	result := CityDetail{GeneratedAt: view.data.GeneratedAt, City: City{Name: bucket.city, Slug: bucket.slug}, Theaters: make([]Theater, 0, len(bucket.catalogPositions)), Movies: make([]MovieCatalogItem, 0, len(bucket.movieSlugs))}
	for _, position := range bucket.catalogPositions {
		result.Theaters = append(result.Theaters, materializeTheater(view, position))
	}
	sort.Slice(result.Theaters, func(i, j int) bool {
		if comparison := compareFolded(result.Theaters[i].Name, result.Theaters[j].Name); comparison != 0 {
			return comparison < 0
		}
		return result.Theaters[i].Slug < result.Theaters[j].Slug
	})
	for _, slug := range bucket.movieSlugs {
		movie := view.movieBySlug[slug]
		result.Movies = append(result.Movies, materializeCatalogMovie(view, view.data.Showtimes[movie.firstShowtime].Movie))
	}
	sort.Slice(result.Movies, func(i, j int) bool { return compareMovieCatalogTitle(result.Movies[i], result.Movies[j], false) })
	return result, nil
}

func compareFolded(left, right string) int {
	return strings.Compare(cases.Fold().String(strings.TrimSpace(left)), cases.Fold().String(strings.TrimSpace(right)))
}
