package schedule

import "strings"

// moviePageURL uses the screening's source identity, never the canonical film
// slug/title (which can combine several chains or be manually edited). Only
// routes supported by the source identity are emitted; other logos stay inert.
func moviePageURL(theater TheaterRecord, movie MovieRecord) *string {
	provider := recordProvider(theater.Provider, theater.ID)
	if recordProvider(movie.Provider, movie.Slug) != provider || !validProviderIdentity(provider, "movie", movie.ProviderID) {
		return nil
	}
	id := movie.ProviderID
	var raw string
	switch provider {
	case ProviderUGC:
		// UGC resolves its stable numeric route to the current titled film page.
		raw = "https://www.ugc.fr/film.html?id=" + id
	case ProviderPathe:
		raw = "https://www.pathe.fr/films/" + id
	case ProviderCineode:
		if slug := CineodeSlug(theater.ProviderID); slug != "" {
			raw = "https://www.cineode.fr/" + slug + "/film/" + id + "/"
		}
	case ProviderOcine:
		if slug := OcineSlug(theater.ProviderID); slug != "" {
			raw = "https://www.ocine.fr/" + slug + "/film/" + id + "/"
		}
	case ProviderMegarama, ProviderMajestic:
		if megaramaGlobalIdentity.MatchString(id) {
			raw = "https://www.ticketingcine.com/film/" + id + ".html"
		}
	case ProviderCinewest, ProviderVeo:
		if code, ok := strings.CutPrefix(id, "ticketingcine-"); ok && megaramaGlobalIdentity.MatchString(code) {
			raw = "https://www.ticketingcine.com/film/" + code + ".html"
		}
	}
	if raw == "" {
		return nil
	}
	return &raw
}
