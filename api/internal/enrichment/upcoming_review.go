package enrichment

import (
	"regexp"

	"messeances/api/internal/tmdb"
)

const (
	ReasonLimitedOnly     = "limited_only"
	ReasonNonTheatrical   = "non_theatrical_before_or_same_day"
	ReasonBroadcaster     = "broadcaster_theatrical_note"
	ReasonSingleScreening = "single_screening_note"
)

// Unicode letter/number boundaries avoid substring matches in names or other words.
// Notes have normalized whitespace. Bare France, TV, festival and venue names are not signals.
var broadcasterNote = regexp.MustCompile(`(?i)(^|[^\pL\pN_])(france\s+(2|3|4|5|24)|france\s*\.\s*tv|france\s+t[ée]l[ée]visions?|franceinfo|arte|tf1|tmc|m6|w9|canal\+|c8|cstar|bfm|rtl|europe\s+1|rfi|radio\s+france|public\s+s[ée]nat|lcp|tv5)($|[^\pL\pN_])`)
var singleScreeningNote = regexp.MustCompile(`(?i)(^|[^\pL\pN_])(séance unique|projection unique|une seule séance|séance exceptionnelle|séance spéciale)($|[^\pL\pN_])`)

// AssessUpcoming returns review hints only. No reason ever changes public eligibility.
func AssessUpcoming(rows []tmdb.FrenchReleaseRow, selected string) []string {
	reasons := []string{}
	if selected == "" {
		return reasons
	}
	limited, national, nonTheatrical, broadcaster, single := false, false, false, false, false
	for _, row := range rows {
		limited = limited || row.Type == 2
		national = national || row.Type == 3
		nonTheatrical = nonTheatrical || row.Type >= 4 && row.Type <= 6 && row.Date <= selected
		if row.Type == 2 || row.Type == 3 {
			broadcaster = broadcaster || broadcasterNote.MatchString(row.Note)
			single = single || singleScreeningNote.MatchString(row.Note)
		}
	}
	for i, match := range []bool{limited && !national, nonTheatrical, broadcaster, single} {
		if match {
			reasons = append(reasons, []string{ReasonLimitedOnly, ReasonNonTheatrical, ReasonBroadcaster, ReasonSingleScreening}[i])
		}
	}
	return reasons
}
