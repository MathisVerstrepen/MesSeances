package accounts

import (
	"errors"
	"strings"
	"testing"
)

func TestTheaterFollowValidation(t *testing.T) {
	s := &Service{}
	for _, revision := range []string{"", "00", "01", "-1", "+1", "1.0", " 1", "9007199254740992", "9223372036854775808"} {
		if _, err := s.SaveTheaterFollow(t.Context(), "", "owner", revision, "ugc-1", true); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("revision %q: %v", revision, err)
		}
	}
	for _, id := range []string{"", "-ugc", "ugc/1", "ugc.1", "ugc 1", "é", strings.Repeat("a", 129)} {
		if _, err := s.SaveTheaterFollow(t.Context(), "", "owner", "0", id, true); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("id %q: %v", id, err)
		}
	}
	for _, revision := range []string{"0", "1", "9007199254740991"} {
		for _, id := range []string{"ugc-1", "Unknown_Z", strings.Repeat("a", 128)} {
			if _, err := s.SaveTheaterFollow(t.Context(), "", "owner", revision, id, true); !errors.Is(err, ErrUnavailable) {
				t.Fatalf("valid input: %v", err)
			}
		}
	}
}
