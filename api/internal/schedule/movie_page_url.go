package schedule

import (
	"net/url"
	"regexp"
	"strings"
)

var sourcePageSlug = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
var kinepolisPageEvent = regexp.MustCompile(`^[0-9]{1,128}$`)

// ValidSourceMoviePageURL admits only film routes captured from the provider's
// own metadata or sitemap. Boxoffice routes must contain the exact source ID.
func ValidSourceMoviePageURL(provider Provider, movieID, raw string) bool {
	if !validProviderIdentity(provider, "movie", movieID) || len(raw) > maxURLLength {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.ContainsAny(raw, "%\\?#") {
		return false
	}
	if provider == ProviderMK2 {
		return u.Host == "www.mk2.com" && strings.HasPrefix(u.Path, "/film/") && sourcePageSlug.MatchString(strings.TrimPrefix(u.Path, "/film/"))
	}
	if provider == ProviderKinepolis {
		// The published getMovieDetailUrl helper uses corporate ID, exact HO
		// identity and event code. The final title segment is optional on the
		// verified detail route, so source/canonical title changes cannot break it.
		parts := strings.Split(u.Path, "/")
		return u.Host == "kinepolis.fr" && len(parts) == 6 && parts[1] == "movies" && parts[2] == "detail" && len(parts[3]) <= 128 && validPositiveDecimal(parts[3]) && parts[4] == movieID && kinepolisPageEvent.MatchString(parts[5])
	}
	var host, prefix string
	switch provider {
	case ProviderCGR:
		host, prefix = "www.cgrcinemas.fr", "/films-a-l-affiche/"
	case ProviderGrandEcran:
		host, prefix = "www.grandecran.fr", "/movies/"
	case ProviderNoeCinemas:
		host, prefix = "www.noecinemas.com", "/films/"
	case ProviderCinemovida:
		host, prefix = "www.cine-movida.com", "/movies/"
	case ProviderVeo:
		host, prefix = "veocinemas.fr", "/movies/"
	default:
		return false
	}
	stem := prefix + movieID + "-"
	return u.Host == host && strings.HasPrefix(u.Path, stem) && strings.HasSuffix(u.Path, "/") && sourcePageSlug.MatchString(strings.TrimSuffix(strings.TrimPrefix(u.Path, stem), "/"))
}

// moviePageURL uses the screening's source identity, never the canonical film
// slug/title (which can combine several chains or be manually edited). Only
// routes supported by the source identity are emitted; other logos stay inert.
func moviePageURL(theater TheaterRecord, movie MovieRecord) *string {
	provider := recordProvider(theater.Provider, theater.ID)
	if recordProvider(movie.Provider, movie.Slug) != provider || !validProviderIdentity(provider, "movie", movie.ProviderID) {
		return nil
	}
	id := movie.ProviderID
	if ValidSourceMoviePageURL(provider, id, movie.SourcePageURL) {
		return &movie.SourcePageURL
	}
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
