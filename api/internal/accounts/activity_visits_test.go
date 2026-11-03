package accounts

import (
	"errors"
	"strings"
	"testing"
)

func TestActivityVisitInput(t *testing.T) {
	for _, scope := range []string{"followed", "theater:ugc-1", "theater:" + strings.Repeat("x", 128)} {
		for _, id := range []string{"0", "1", "9007199254740993", "9223372036854775807"} {
			if _, err := activityVisitInput(scope, id); err != nil {
				t.Fatalf("valid input %s %s: %v", scope, id, err)
			}
		}
	}
	for _, input := range [][2]string{
		{"", "1"}, {"theater:", "1"}, {"theater:_x", "1"}, {"theater:a/b", "1"}, {"theater:" + strings.Repeat("x", 129), "1"}, {"account:other", "1"},
		{"followed", ""}, {"followed", "01"}, {"followed", "-1"}, {"followed", "+1"}, {"followed", "1.0"}, {"followed", "9223372036854775808"},
	} {
		if _, err := activityVisitInput(input[0], input[1]); !errors.Is(err, ErrInvalidInput) {
			t.Fatal("invalid input accepted", input)
		}
	}
}
