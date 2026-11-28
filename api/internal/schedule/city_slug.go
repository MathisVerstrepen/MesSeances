package schedule

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

type cityIdentity struct {
	name    string
	slug    string
	foldKey string
	hash    string
}

// TheaterCityIdentities derives inventory-dependent cities without constructing a
// schedule snapshot. The returned entries correspond to the supplied theaters.
func TheaterCityIdentities(theaters []TheaterRecord) []City {
	labels := make(map[string][]string)
	for _, theater := range theaters {
		key := cityKey(theater.City)
		labels[key] = append(labels[key], theater.City)
	}
	byKey := make(map[string]City)
	for _, identity := range buildCityIdentities(labels) {
		byKey[identity.foldKey] = City{Name: identity.name, Slug: identity.slug}
	}
	result := make([]City, len(theaters))
	for i, theater := range theaters {
		result[i] = byKey[cityKey(theater.City)]
	}
	return result
}

// legacyCitySlugSuffix matches the hash suffix that used to separate spellings
// of one city ("creteil--3e9b9ea8"); such links resolve to the merged city.
var legacyCitySlugSuffix = regexp.MustCompile(`--[0-9a-f]{8,64}$`)

// cityKey groups labels naming the same city: providers disagree on case,
// accents, spacing and punctuation ("CRETEIL", "Créteil"). Labels without
// any Latin letter keep a case-folded key so they never merge by accident.
func cityKey(label string) string {
	name := strings.Join(strings.Fields(label), " ")
	if base := cityBaseSlug(name); base != "ville" || foldKey(name) == "ville" {
		return base
	}
	return "~" + foldKey(name)
}

// cityLookupKey resolves a city slug or name given by a client, including
// legacy hash-suffixed slugs.
func cityLookupKey(value string) string {
	return cityKey(legacyCitySlugSuffix.ReplaceAllString(strings.TrimSpace(value), ""))
}

var cityParticles = map[string]bool{
	"au": true, "aux": true, "d": true, "de": true, "des": true, "du": true,
	"en": true, "et": true, "l": true, "la": true, "le": true, "les": true,
	"lès": true, "lez": true, "sous": true, "sur": true,
}

// cityDisplayName picks the best-written label for one city: mixed case over
// capitals, then the most accents, then lowercase particles, then hyphens.
// Capital-only cities get French title case.
func cityDisplayName(labels []string) string {
	names := make([]string, 0, len(labels))
	for _, label := range labels {
		if name := strings.Join(strings.Fields(label), " "); name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return ""
	}
	sort.SliceStable(names, func(i, j int) bool {
		left, right := names[i], names[j]
		if hasLower(left) != hasLower(right) {
			return hasLower(left)
		}
		if a, b := diacriticCount(left), diacriticCount(right); a != b {
			return a > b
		}
		if a, b := capitalizedParticles(left), capitalizedParticles(right); a != b {
			return a < b
		}
		if a, b := strings.Count(left, "-"), strings.Count(right, "-"); a != b {
			return a > b
		}
		if comparison := compareNormalized(left, right); comparison != 0 {
			return comparison < 0
		}
		return left < right
	})
	if !hasLower(names[0]) {
		return frenchTitleCase(names[0])
	}
	return names[0]
}

func hasLower(value string) bool {
	for _, current := range value {
		if unicode.IsLower(current) {
			return true
		}
	}
	return false
}

func diacriticCount(value string) int {
	count := 0
	for _, current := range norm.NFD.String(value) {
		if unicode.Is(unicode.Mn, current) {
			count++
		}
	}
	return count
}

func cityWords(value string) []string {
	return strings.FieldsFunc(value, func(current rune) bool {
		return current == ' ' || current == '-' || current == '\'' || current == '’'
	})
}

func capitalizedParticles(value string) int {
	count := 0
	for index, word := range cityWords(value) {
		if index > 0 && cityParticles[strings.ToLower(word)] && word != strings.ToLower(word) {
			count++
		}
	}
	return count
}

// frenchTitleCase rewrites a capital-only commune name the way French
// communes are written: words joined by hyphens, particles in lowercase,
// a leading article kept apart ("LE CHESNAY" → "Le Chesnay",
// "VILLENEUVE D'ASCQ" → "Villeneuve-d'Ascq").
func frenchTitleCase(value string) string {
	hasDigit := strings.IndexFunc(value, unicode.IsDigit) >= 0
	var result strings.Builder
	word := 0
	for _, token := range strings.FieldsFunc(value, func(current rune) bool { return current == ' ' || current == '-' }) {
		lower := strings.ToLower(token)
		if word > 0 {
			separator := "-"
			if hasDigit || word == 1 && (result.String() == "Le" || result.String() == "La" || result.String() == "Les") {
				separator = " "
			}
			result.WriteString(separator)
		}
		apostrophe := strings.IndexAny(lower, "'’")
		switch {
		case apostrophe >= 0:
			prefix, rest := lower[:apostrophe+1], lower[apostrophe+1:]
			if word == 0 {
				prefix = capitalizeWord(prefix)
			}
			result.WriteString(prefix + capitalizeWord(rest))
		case word > 0 && cityParticles[lower]:
			result.WriteString(lower)
		default:
			result.WriteString(capitalizeWord(lower))
		}
		word++
	}
	return result.String()
}

func capitalizeWord(value string) string {
	for index, current := range value {
		return string(unicode.ToTitle(current)) + value[index+len(string(current)):]
	}
	return value
}

func buildCityIdentities(labelsByFold map[string][]string) []cityIdentity {
	identities := make([]cityIdentity, 0, len(labelsByFold))
	for key, labels := range labelsByFold {
		names := make([]string, 0, len(labels))
		for _, label := range labels {
			name := strings.TrimSpace(label)
			if name != "" {
				names = append(names, name)
			}
		}
		name := cityDisplayName(names)
		// Hash the group key, not the chosen label, so suffixes stay stable
		// when a better spelling appears.
		canonicalFold := cases.Fold().String(norm.NFC.String(key))
		digest := sha256.Sum256([]byte(canonicalFold))
		identities = append(identities, cityIdentity{name: name, slug: cityBaseSlug(name), foldKey: key, hash: hex.EncodeToString(digest[:])})
	}

	byBase := make(map[string][]int)
	for index := range identities {
		byBase[identities[index].slug] = append(byBase[identities[index].slug], index)
	}
	for base, indexes := range byBase {
		if len(indexes) < 2 {
			continue
		}
		prefixLength := 8
		for prefixLength < sha256.Size*2 && !uniqueHashPrefixes(identities, indexes, prefixLength) {
			prefixLength++
		}
		for _, index := range indexes {
			identities[index].slug = base + "--" + identities[index].hash[:prefixLength]
		}
	}
	sort.Slice(identities, func(i, j int) bool { return identities[i].foldKey < identities[j].foldKey })
	return identities
}

func uniqueHashPrefixes(identities []cityIdentity, indexes []int, length int) bool {
	seen := make(map[string]bool, len(indexes))
	for _, index := range indexes {
		prefix := identities[index].hash[:length]
		if seen[prefix] {
			return false
		}
		seen[prefix] = true
	}
	return true
}

func cityBaseSlug(value string) string {
	value = strings.NewReplacer("œ", "oe", "Œ", "oe", "æ", "ae", "Æ", "ae").Replace(value)
	value = strings.ToLower(norm.NFKD.String(value))
	var result strings.Builder
	separator := false
	for _, current := range value {
		if unicode.Is(unicode.Mn, current) {
			continue
		}
		if current >= 'a' && current <= 'z' || current >= '0' && current <= '9' {
			if separator && result.Len() > 0 {
				result.WriteByte('-')
			}
			result.WriteRune(current)
			separator = false
			continue
		}
		separator = true
	}
	if result.Len() == 0 {
		return "ville"
	}
	return result.String()
}
