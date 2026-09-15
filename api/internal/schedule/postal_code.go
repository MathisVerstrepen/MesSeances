package schedule

import "strings"

// NormalizePostalCode removes Unicode whitespace without changing other characters.
// It preserves leading zeros and does not validate postal code syntax.
func NormalizePostalCode(value string) string {
	return strings.Join(strings.Fields(value), "")
}
