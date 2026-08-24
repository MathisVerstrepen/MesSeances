package schedule

import "strings"

func (s *Service) Theaters(query TheaterCatalogQuery) []Theater {
	chain := strings.TrimSpace(string(query.Chain))
	if chain != "" && !strings.EqualFold(chain, string(ProviderUGC)) && !strings.EqualFold(chain, string(ProviderKinepolis)) && !strings.EqualFold(chain, string(ProviderPathe)) {
		return []Theater{}
	}
	city := strings.TrimSpace(query.City)
	view := s.source.Snapshot()
	positions := view.theaterCatalog
	if city != "" {
		positions = view.catalogPositionsForCities(s.cityLookupValues(city))
	}
	result := make([]Theater, 0, len(positions))
	for _, position := range positions {
		theater := view.data.Theaters[position]
		provider := recordProvider(theater.Provider, theater.ID)
		if chain != "" && !strings.EqualFold(chain, string(provider)) {
			continue
		}
		result = append(result, materializeTheater(view, position))
	}
	return result
}

func materializeTheater(view *SnapshotView, position int) Theater {
	theater := view.data.Theaters[position]
	return Theater{Provider: recordProvider(theater.Provider, theater.ID), ID: theater.ID, Slug: theater.Slug, Name: theater.Name, Address: theater.Address, City: theater.City, CitySlug: view.cityBuckets[view.theaterCity[position]].slug, PostalCode: theater.PostalCode, AvailableDates: append([]string{}, theater.AvailableDates...), AcceptedPasses: append([]string{}, theater.AcceptedPasses...)}
}
