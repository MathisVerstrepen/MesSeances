package schedule

import (
	"slices"
	"strings"
)

// ChainIndependent is the public chain grouping the generic independent
// cinema providers. Filters, statistics and the cinema directory show them as
// one "Cinémas indépendants" chain; each cinema keeps its own provider.
const ChainIndependent Provider = "independants"

var independentProviders = [...]Provider{ProviderTicketingcine, ProviderScare, ProviderWebedia}

// IndependentProviders returns the providers grouped under ChainIndependent.
func IndependentProviders() []Provider { return slices.Clone(independentProviders[:]) }

// PublicChain maps a provider to its public chain.
func PublicChain(provider Provider) Provider {
	if slices.Contains(independentProviders[:], provider) {
		return ChainIndependent
	}
	return provider
}

// validChain accepts a source provider or a public chain.
func validChain(value string) bool {
	return value == string(ChainIndependent) || validProvider(Provider(value), false)
}

// chainMatches reports whether provider belongs to the requested chain, given
// as a provider or a public chain (case-insensitive); empty matches all.
func chainMatches(chain string, provider Provider) bool {
	return chain == "" || strings.EqualFold(chain, string(provider)) || strings.EqualFold(chain, string(PublicChain(provider)))
}
