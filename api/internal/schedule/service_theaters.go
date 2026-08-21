package schedule

import "strings"

func (s *Service) Theaters(query TheaterCatalogQuery) []Theater {
	chain := strings.TrimSpace(query.Chain)
	if chain != "" && !strings.EqualFold(chain, ProviderUGC) && !strings.EqualFold(chain, ProviderKinepolis) {
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
		if chain != "" && !strings.EqualFold(chain, provider) {
			continue
		}
		result = append(result, Theater{Provider: provider, ID: theater.ID, Slug: theater.Slug, Name: theater.Name, Address: theater.Address, City: theater.City, PostalCode: theater.PostalCode, AvailableDates: append([]string(nil), theater.AvailableDates...), AcceptedPasses: append([]string(nil), theater.AcceptedPasses...)})
	}
	return result
}
