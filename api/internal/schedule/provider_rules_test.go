package schedule

import (
	"slices"
	"testing"
	"time"
)

func TestProviderRulesCoverEverySourceProvider(t *testing.T) {
	for _, provider := range SourceProviders() {
		rules, ok := rulesFor(provider)
		if !ok || rules.identity == nil || rules.booking == nil || rules.poster == nil {
			t.Fatalf("%s rules incomplete", provider)
		}
	}
	if len(providerRuleSet) != len(SourceProviders()) {
		t.Fatalf("rules=%d providers=%d", len(providerRuleSet), len(SourceProviders()))
	}
	if _, ok := rulesFor(ProviderCombined); ok || ValidProviderIdentity(ProviderCombined, "theater", "1") || ValidProviderIdentity("unknown", "theater", "1") {
		t.Fatal("non-source provider has rules")
	}
	if validBookingURL("unknown", "https://www.ugc.fr/reservationSeances.html?id=1", "1", "1") || validProviderImageURL("unknown", "https://www.ugc.fr/a.jpg") {
		t.Fatal("unknown provider accepted a URL")
	}
}

// The policy table is intentionally explicit: changing a provider's policy
// must be a visible, reviewed edit.
func TestProviderRulesPolicies(t *testing.T) {
	unknownRuntime := []Provider{ProviderCGR, ProviderMegarama, ProviderMajestic, ProviderCineode, ProviderCinemovida, ProviderOcine, ProviderCineville, ProviderMK2, ProviderCinewest, ProviderGrandEcran, ProviderNoeCinemas, ProviderVeo, ProviderTicketingcine, ProviderScare, ProviderWebedia}
	strictAlias := []Provider{ProviderCineville, ProviderMK2, ProviderCinewest, ProviderGrandEcran, ProviderNoeCinemas, ProviderVeo, ProviderMajestic, ProviderCineode, ProviderCinemovida, ProviderOcine, ProviderTicketingcine, ProviderScare, ProviderWebedia}
	address := map[Provider]addressRule{ProviderKinepolis: addressOptional, ProviderCineode: addressOptional, ProviderOcine: addressOptional, ProviderCineville: addressPostalOnly, ProviderScare: addressPostalOnly}
	completeCoverage := []Provider{ProviderUGC, ProviderPathe, ProviderCGR, ProviderGrandEcran, ProviderNoeCinemas, ProviderCinemovida, ProviderVeo}
	runtimeEnd := []Provider{ProviderMegarama, ProviderCinewest, ProviderVeo, ProviderMajestic, ProviderTicketingcine}
	census := map[Provider]int{ProviderMajestic: 7, ProviderCineode: 37, ProviderOcine: 3}
	serviceDay := map[Provider]serviceDayRule{
		ProviderUGC: serviceDayStrictOvernight, ProviderKinepolis: serviceDayStrictOvernight, ProviderCGR: serviceDayStrictOvernight,
		ProviderPathe:    serviceDayPathe,
		ProviderMegarama: serviceDayOvernight, ProviderCinewest: serviceDayOvernight, ProviderGrandEcran: serviceDayOvernight, ProviderNoeCinemas: serviceDayOvernight, ProviderVeo: serviceDayOvernight, ProviderTicketingcine: serviceDayOvernight, ProviderScare: serviceDayOvernight, ProviderWebedia: serviceDayOvernight,
		ProviderCineville: serviceDayCalendar, ProviderMK2: serviceDayCalendar, ProviderMajestic: serviceDayCalendar, ProviderCineode: serviceDayCalendar, ProviderCinemovida: serviceDayCalendar, ProviderOcine: serviceDayCalendar,
	}
	for _, provider := range SourceProviders() {
		rules, _ := rulesFor(provider)
		if rules.unknownRuntime != slices.Contains(unknownRuntime, provider) || providerAllowsUnknownRuntime(provider) != rules.unknownRuntime {
			t.Fatalf("%s unknown runtime=%t", provider, rules.unknownRuntime)
		}
		if rules.strictAliasSlug != slices.Contains(strictAlias, provider) {
			t.Fatalf("%s strict alias=%t", provider, rules.strictAliasSlug)
		}
		if rules.address != address[provider] || rules.census != census[provider] || rules.serviceDay != serviceDay[provider] {
			t.Fatalf("%s address=%d census=%d day=%d", provider, rules.address, rules.census, rules.serviceDay)
		}
		if wantCoverage := slices.Contains(completeCoverage, provider); (rules.completeCoverage != nil) != wantCoverage {
			t.Fatalf("%s complete coverage=%t", provider, rules.completeCoverage != nil)
		}
		if wantRuntimeEnd := slices.Contains(runtimeEnd, provider); (rules.runtimeEnd != nil) != wantRuntimeEnd {
			t.Fatalf("%s runtime end=%t", provider, rules.runtimeEnd != nil)
		}
		if wantPasses := provider == ProviderUGC; (rules.passes != nil) != wantPasses {
			t.Fatalf("%s passes=%v", provider, rules.passes)
		}
	}
}

func TestExpectedServiceDate(t *testing.T) {
	loc, _ := time.LoadLocation(Timezone)
	at := func(hour int) time.Time { return time.Date(2026, 8, 15, hour, 30, 0, 0, loc) }
	for _, test := range []struct {
		rule     serviceDayRule
		hour     int
		filed    string
		want     string
		inCinema bool
	}{
		{serviceDayStrictOvernight, 1, "2026-08-14", "2026-08-14", true},
		{serviceDayStrictOvernight, 5, "2026-08-15", "", false},
		{serviceDayStrictOvernight, 9, "2026-08-15", "2026-08-15", true},
		{serviceDayOvernight, 2, "2026-08-14", "2026-08-14", true},
		{serviceDayOvernight, 5, "2026-08-15", "2026-08-15", true},
		{serviceDayCalendar, 1, "2026-08-15", "2026-08-15", true},
		{serviceDayPathe, 7, "2026-08-14", "2026-08-14", true},
		{serviceDayPathe, 7, "2026-08-15", "2026-08-15", true},
		{serviceDayPathe, 9, "2026-08-14", "2026-08-15", true},
	} {
		got, ok := providerRules{serviceDay: test.rule}.expectedServiceDate(at(test.hour), test.filed)
		if got != test.want || ok != test.inCinema {
			t.Fatalf("rule=%d hour=%d filed=%s got=%q ok=%t", test.rule, test.hour, test.filed, got, ok)
		}
	}
}

func TestExhaustiveCensusAllowsOnlyCompleteProviderCensus(t *testing.T) {
	theaters := func(provider Provider, ids []string) []TheaterRecord {
		records := make([]TheaterRecord, len(ids))
		for i, id := range ids {
			records[i] = TheaterRecord{Provider: provider, ID: string(provider) + "-" + id, ProviderID: id}
		}
		return records
	}
	ocine := theaters(ProviderOcine, []string{"309", "310", "461"})
	if !exhaustiveCensus(Dataset{Provider: ProviderOcine, Theaters: ocine}) || !exhaustiveCensus(Dataset{Provider: ProviderCombined, Theaters: ocine}) {
		t.Fatal("complete Ocine census rejected")
	}
	if exhaustiveCensus(Dataset{Provider: ProviderOcine, Theaters: ocine[:2]}) || exhaustiveCensus(Dataset{Provider: ProviderMajestic, Theaters: ocine}) {
		t.Fatal("incomplete or mismatched census accepted")
	}
	invalid := theaters(ProviderOcine, []string{"309", "310", "999"})
	if exhaustiveCensus(Dataset{Provider: ProviderOcine, Theaters: invalid}) {
		t.Fatal("census with unknown theater accepted")
	}
	if exhaustiveCensus(Dataset{Provider: ProviderUGC, Theaters: theaters(ProviderUGC, []string{"1", "2", "3"})}) {
		t.Fatal("provider without census accepted")
	}
}

func TestCompleteCoverageExcludesVeoTicketingTheater(t *testing.T) {
	rules, _ := rulesFor(ProviderVeo)
	if !rules.completeCoverage(TheaterRecord{ProviderID: "G06DB"}) || rules.completeCoverage(TheaterRecord{ProviderID: "EMS1361"}) {
		t.Fatal("Véo coverage eligibility")
	}
}
