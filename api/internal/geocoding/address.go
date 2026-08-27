package geocoding

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

var fold = cases.Fold()

var cityAliases = map[string]string{
	"evry":                  "evrycourcouronnes",
	"evrycourcouronnes":     "evrycourcouronnes",
	"cherbourgocteville":    "cherbourgencotentin",
	"cherbourgencotentin":   "cherbourgencotentin",
	"grandquevilly":         "legrandquevilly",
	"legrandquevilly":       "legrandquevilly",
	"levallois":             "levalloisperret",
	"levalloisperret":       "levalloisperret",
	"lechesnay":             "lechesnayrocquencourt",
	"lechesnayrocquencourt": "lechesnayrocquencourt",
}

func AddressHash(address, postalCode, city string) string {
	hash := sha256.New()
	for index, value := range []string{address, postalCode, city} {
		if index > 0 {
			hash.Write([]byte{0})
		}
		normalized := norm.NFC.String(fold.String(strings.Join(strings.Fields(value), " ")))
		hash.Write([]byte(normalized))
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func cityKey(value string) string {
	decomposed := norm.NFKD.String(fold.String(expandLeadingSaint(value)))
	var result strings.Builder
	for _, character := range decomposed {
		if unicode.Is(unicode.Mn, character) {
			continue
		}
		if unicode.IsLetter(character) || unicode.IsNumber(character) {
			result.WriteRune(character)
		}
	}
	key := result.String()
	if alias, ok := cityAliases[key]; ok {
		return alias
	}
	return key
}

func expandLeadingSaint(value string) string {
	value = strings.TrimSpace(value)
	if len(value) < 2 || !strings.EqualFold(value[:2], "st") {
		return value
	}
	if len(value) == 2 {
		return "Saint"
	}
	next, _ := utf8.DecodeRuneInString(value[2:])
	if unicode.IsLetter(next) || unicode.IsNumber(next) {
		return value
	}
	return "Saint" + value[2:]
}
