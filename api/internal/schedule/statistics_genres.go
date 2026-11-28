package schedule

import (
	"slices"
	"strings"
)

// statisticsGenreAliases maps a normalized source genre (see normalized) to its
// canonical statistics labels. Compound genres expand to several parents, an
// empty list marks a placeholder that carries no genre, and canonical labels map
// to themselves so their display spelling is fixed. Statistics only: stored
// metadata keeps provider spellings. schedulepg builds its SQL mapping from
// StatisticsGenreAliases, so this table is the single source of truth.
var statisticsGenreAliases = map[string][]string{
	// Compound genres.
	"comédie dramatique":            {"Comédie", "Drame"},
	"comédie romantique":            {"Comédie", "Romance"},
	"comédie d'action":              {"Comédie", "Action"},
	"comédie policière":             {"Comédie", "Crime"},
	"drame historique":              {"Drame", "Histoire"},
	"drame costume":                 {"Drame", "Histoire"},
	"drame fantastique":             {"Drame", "Fantastique"},
	"horror/thriller":               {"Horreur", "Thriller"},
	"horreur/thriller":              {"Horreur", "Thriller"},
	"horreur / thriller":            {"Horreur", "Thriller"},
	"romancen musical":              {"Romance", "Comédie musicale"},
	"animation à partir de 3 ans":   {"Animation", "Famille"},
	"animation à partir de 2/3 ans": {"Animation", "Famille"},

	// Spelling variants and synonyms.
	"comédie":             {"Comédie"},
	"drame":               {"Drame"},
	"action":              {"Action"},
	"familial":            {"Famille"},
	"famille":             {"Famille"},
	"famille/enfants":     {"Famille"},
	"enfants":             {"Famille"},
	"jeune public":        {"Famille"},
	"à partir de 6 ans":   {"Famille"},
	"opéra":               {"Opéra"},
	"opera":               {"Opéra"},
	"science-fiction":     {"Science-fiction"},
	"science fiction":     {"Science-fiction"},
	"histoire":            {"Histoire"},
	"historique":          {"Histoire"},
	"fiction historique":  {"Histoire"},
	"horreur":             {"Horreur"},
	"horreur / épouvante": {"Horreur"},
	"horreur / epouvante": {"Horreur"},
	"épouvante-horreur":   {"Horreur"},
	"epouvante-horreur":   {"Horreur"},
	"épouvante - horreur": {"Horreur"},
	"epouvante - horreur": {"Horreur"},
	"thriller":            {"Thriller"},
	"thriler":             {"Thriller"},
	"suspense":            {"Thriller"},
	"romance":             {"Romance"},
	"amour":               {"Romance"},
	"animation":           {"Animation"},
	"dessin animé":        {"Animation"},
	"aventure":            {"Aventure"},
	"film d'aventures":    {"Aventure"},
	"crime":               {"Crime"},
	"policier":            {"Crime"},
	"biopic":              {"Biopic"},
	"biographie":          {"Biopic"},
	"court-métrage":       {"Court-métrage"},
	"court-metrage":       {"Court-métrage"},
	"court métrage":       {"Court-métrage"},
	"courts-métrages":     {"Court-métrage"},
	"courts-metrages":     {"Court-métrage"},
	"courts métrages":     {"Court-métrage"},
	"ballet":              {"Ballet"},
	"ballets":             {"Ballet"},
	"comédie musicale":    {"Comédie musicale"},
	"musical":             {"Comédie musicale"},
	"événement sportif":   {"Événement sportif"},
	"évènement sportif":   {"Événement sportif"},
	"evenement sportif":   {"Événement sportif"},
	"sport event":         {"Événement sportif"},
	"spectacle":           {"Spectacle"},
	"show":                {"Spectacle"},
	"théâtre":             {"Théâtre"},
	"theatre":             {"Théâtre"},
	"espionnage":          {"Espionnage"},
	"tous les films d’espionnage du moment": {"Espionnage"},

	// MK2 talk themes.
	"conférence / rencontre": {"Conférence / Rencontre"},
	"philosophie":            {"Conférence / Rencontre"},
	"société":                {"Conférence / Rencontre"},
	"discussion":             {"Conférence / Rencontre"},
	"littérature":            {"Conférence / Rencontre"},
	"neurosciences":          {"Conférence / Rencontre"},
	"économie":               {"Conférence / Rencontre"},
	"podcast live":           {"Conférence / Rencontre"},
	"féminisme":              {"Conférence / Rencontre"},
	"astrophysique":          {"Conférence / Rencontre"},
	"politique":              {"Conférence / Rencontre"},
	"cinéma":                 {"Conférence / Rencontre"},
	"santé":                  {"Conférence / Rencontre"},
	"food":                   {"Conférence / Rencontre"},
	"divertissement":         {"Conférence / Rencontre"},

	// Placeholders: the movie counts as "Non renseigné" unless it has a real genre.
	"null":          {},
	"non classifié": {},
	"soirée film":   {},
	"divers":        {},
}

// StatisticsGenreAlias is one normalized source genre and its canonical labels.
type StatisticsGenreAlias struct {
	Alias  string
	Labels []string
}

// StatisticsGenreAliases returns a sorted copy of the statistics genre aliases
// so SQL projections can mirror statisticsGenreParents and statisticsGenre.
func StatisticsGenreAliases() []StatisticsGenreAlias {
	aliases := make([]StatisticsGenreAlias, 0, len(statisticsGenreAliases))
	for alias, labels := range statisticsGenreAliases {
		aliases = append(aliases, StatisticsGenreAlias{Alias: alias, Labels: slices.Clone(labels)})
	}
	slices.SortFunc(aliases, func(a, b StatisticsGenreAlias) int { return strings.Compare(a.Alias, b.Alias) })
	return aliases
}

// statisticsGenreParents replaces a source genre with its canonical labels.
func statisticsGenreParents(genre string) []string {
	if labels, ok := statisticsGenreAliases[normalized(genre)]; ok {
		return labels
	}
	return []string{genre}
}

// statisticsGenre canonicalizes one genre or filter value, never stored metadata.
// Placeholders select the unknown bucket; compound genres keep their own value,
// which matches no movie because movies only carry their parents.
func statisticsGenre(genre string) (value, label string) {
	label = strings.TrimSpace(genre)
	if labels, ok := statisticsGenreAliases[normalized(label)]; ok {
		switch len(labels) {
		case 0:
			return statisticsUnknown, ""
		case 1:
			label = labels[0]
		}
	}
	return normalized(label), label
}

func statisticsGenreLabel(labels map[string]string, value, label string) {
	if current, ok := labels[value]; !ok || label < current {
		labels[value] = label
	}
}
