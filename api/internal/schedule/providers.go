package schedule

import (
	"slices"
	"strings"
)

// sourceProviders is the canonical, ordered list of synchronized providers.
// Sync targets, schedule ordering, publication batches and admin filters are
// derived from it, so a new provider is registered here once.
var sourceProviders = [...]Provider{
	ProviderUGC,
	ProviderKinepolis,
	ProviderPathe,
	ProviderCGR,
	ProviderMegarama,
	ProviderCineville,
	ProviderMK2,
	ProviderCinewest,
	ProviderGrandEcran,
	ProviderNoeCinemas,
	ProviderVeo,
	ProviderMajestic,
	ProviderCineode,
	ProviderCinemovida,
	ProviderOcine,
	ProviderTicketingcine,
}

// SourceProviders returns a fresh copy of the synchronized providers in
// canonical order.
func SourceProviders() []Provider {
	return slices.Clone(sourceProviders[:])
}

// IsSourceProvider reports whether provider is an exact synchronized provider
// key. The combined pseudo-provider is not a source provider.
func IsSourceProvider(provider Provider) bool {
	return slices.Contains(sourceProviders[:], provider)
}

// SourceProviderFold resolves a case-insensitive provider spelling.
func SourceProviderFold(value string) (Provider, bool) {
	for _, provider := range sourceProviders {
		if strings.EqualFold(value, string(provider)) {
			return provider, true
		}
	}
	return "", false
}
