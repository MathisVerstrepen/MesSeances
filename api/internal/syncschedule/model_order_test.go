package syncschedule

import (
	"slices"
	"testing"
)

func TestOrderedTargetsKeepDisplayOrder(t *testing.T) {
	want := []Target{TargetUGC, TargetKinepolis, TargetPathe, TargetCGR, TargetMegarama, TargetCineville, TargetMK2, TargetCinewest, TargetGrandEcran, TargetNoeCinemas, TargetVeo, TargetMajestic, TargetCineode, TargetCinemovida, TargetOcine, TargetMetadataRefresh, TargetUpcomingMovies}
	if got := OrderedTargets(); !slices.Equal(got, want) {
		t.Fatalf("targets=%v", got)
	}
	for i, target := range want {
		if TargetOrder(target) != i || !ValidTarget(target) {
			t.Fatalf("%s order=%d", target, TargetOrder(target))
		}
	}
	if TargetOrder("unknown") != len(want) || ValidTarget("unknown") || ValidTarget("all") {
		t.Fatal("unknown target")
	}
	if names := orderedTargetNames(); len(names) != len(want) || names[0] != "ugc" || names[len(names)-1] != "tmdb_upcoming_movies" {
		t.Fatalf("names=%v", names)
	}
}
