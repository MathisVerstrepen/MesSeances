package enrichment

import "strings"

var titleLigatures = strings.NewReplacer("œ", "oe", "Œ", "OE", "æ", "ae", "Æ", "AE")

func expandTitleLigatures(title string) string {
	return titleLigatures.Replace(title)
}

// comparisonTitle keeps every title word and number, treating written connectors
// as equivalents only between words. NormalizeTitle remains the stored fingerprint.
func comparisonTitle(title string) string {
	title = strings.ReplaceAll(expandTitleLigatures(title), "&", " et ")
	parts := strings.Fields(NormalizeTitle(title))
	for index := 1; index < len(parts)-1; index++ {
		if parts[index] == "and" {
			parts[index] = "et"
		}
	}
	return strings.Join(parts, " ")
}

// Require an explicit delimiter: Halloween Ends is a title, Halloween : Coco
// is a Kinepolis event label. Unknown labels and empty remainders stay intact.
func trimSourceEventPrefix(provider, title string) string {
	prefix, remainder, found := strings.Cut(title, ":")
	if found && strings.TrimSpace(remainder) != "" {
		recognized := false
		switch provider {
		case SourceKinepolis:
			switch NormalizeTitle(prefix) {
			case "ev", "ev ap", "silent movie ev", "ap", "halloween", "horror night", "avt 1ere direct":
				recognized = true
			}
		case SourcePathe:
			switch NormalizeTitle(prefix) {
			case "la seance cine hits", "horror cinema club", "avant premiere thriller", "avant premiere nationale", "elle s au cinema", "elle au cinema":
				recognized = true
			}
		}
		if recognized {
			return strings.TrimSpace(remainder)
		}
	}
	if provider == SourcePathe {
		for _, delimiter := range []string{"...", "…"} {
			prefix, remainder, found := strings.Cut(title, delimiter)
			if found && NormalizeTitle(prefix) == "il etait une fois" && strings.TrimSpace(remainder) != "" {
				return strings.TrimSpace(remainder)
			}
		}
	}
	return title
}
