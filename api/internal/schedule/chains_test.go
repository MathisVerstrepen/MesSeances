package schedule

import "testing"

func TestPublicChainGroupsIndependentProviders(t *testing.T) {
	for _, provider := range SourceProviders() {
		want := provider
		if provider == ProviderTicketingcine || provider == ProviderScare || provider == ProviderWebedia {
			want = ChainIndependent
		}
		if got := PublicChain(provider); got != want {
			t.Fatalf("%s chain=%s", provider, got)
		}
		if !validChain(string(provider)) || !chainMatches("", provider) || !chainMatches(string(provider), provider) || !chainMatches(string(PublicChain(provider)), provider) {
			t.Fatalf("%s chain matching", provider)
		}
	}
	if !validChain("independants") || validChain("independent") || validChain("combined") {
		t.Fatal("chain validation")
	}
	if !chainMatches("INDEPENDANTS", ProviderScare) || chainMatches("independants", ProviderUGC) || chainMatches("scare", ProviderWebedia) {
		t.Fatal("chain matching")
	}
	if len(IndependentProviders()) != 3 {
		t.Fatal("independent providers")
	}
}
