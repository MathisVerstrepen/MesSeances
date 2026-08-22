package schedule

import (
	"crypto/sha256"
	"encoding/hex"
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
		sort.Slice(names, func(i, j int) bool {
			if comparison := compareNormalized(names[i], names[j]); comparison != 0 {
				return comparison < 0
			}
			return names[i] < names[j]
		})
		name := ""
		if len(names) > 0 {
			name = names[0]
		}
		canonicalFold := cases.Fold().String(norm.NFC.String(name))
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
