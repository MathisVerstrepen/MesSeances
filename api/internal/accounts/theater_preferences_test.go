package accounts

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestTheaterPreferenceValidation(t *testing.T) {
	for _, raw := range []string{"", "00", "01", "+1", "-1", " 1", "1 ", "1.0", "1e1", "9007199254740992", "9223372036854775808"} {
		if _, err := theaterRevision(raw); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("accepted revision %q", raw)
		}
	}
	for _, raw := range []string{"0", "1", "9007199254740991"} {
		if _, err := theaterRevision(raw); err != nil {
			t.Fatalf("rejected revision %q", raw)
		}
	}
	for _, ids := range [][]string{nil, {""}, {"ugc-1", "ugc-1"}, {" ugc-1"}, {"ugc-1 "}, {"ugc-1\n"}, {"-1"}, {"é"}, {"ugc/1"}, {strings.Repeat("a", 129)}, make([]string, 4097)} {
		if _, err := canonicalTheaterIDs(ids); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("accepted invalid IDs (count %d)", len(ids))
		}
	}
	input := []string{"unknown-z", "ugc-1", "Unknown_A"}
	got, err := canonicalTheaterIDs(input)
	if err != nil || !slices.Equal(got, []string{"Unknown_A", "ugc-1", "unknown-z"}) || input[0] != "unknown-z" {
		t.Fatal("canonicalization changed input or lost unknown IDs")
	}
	if got, err := canonicalTheaterIDs([]string{}); err != nil || got == nil || len(got) != 0 {
		t.Fatal("explicit empty must remain nonnull")
	}
	maxIDs := make([]string, 4096)
	for i := range maxIDs {
		maxIDs[i] = fmt.Sprintf("%0128d", i)
	}
	if got, err := canonicalTheaterIDs(maxIDs); err != nil || len(got) != 4096 {
		t.Fatal("maximum bounded selection rejected")
	}
}
