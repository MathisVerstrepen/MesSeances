package schedule

import "testing"

func TestNormalizePostalCode(t *testing.T) {
	for _, test := range []struct {
		name, input, want string
	}{
		{name: "normal", input: "29600", want: "29600"},
		{name: "internal space", input: "29 600", want: "29600"},
		{name: "surrounding spaces", input: " 29600 ", want: "29600"},
		{name: "ASCII whitespace", input: "\t29\n6\r0\v0\f", want: "29600"},
		{name: "nonbreaking space", input: "29\u00a0600", want: "29600"},
		{name: "narrow nonbreaking space", input: "29\u202f600", want: "29600"},
		{name: "leading zero", input: "01 000", want: "01000"},
		{name: "empty", input: "", want: ""},
		{name: "whitespace only", input: " \t\r\n\u00a0\u202f", want: ""},
		{name: "punctuation retained", input: "29-600", want: "29-600"},
		{name: "letters retained", input: "AB 123", want: "AB123"},
		{name: "zero width space retained", input: "29\u200b600", want: "29\u200b600"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := NormalizePostalCode(test.input)
			if got != test.want {
				t.Fatalf("NormalizePostalCode(%q)=%q want=%q", test.input, got, test.want)
			}
			if NormalizePostalCode(got) != got {
				t.Fatal("normalization is not idempotent")
			}
		})
	}
}
