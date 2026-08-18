package kinepolis

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"messeances/api/internal/schedule"
)

type film struct {
	id, title, poster, overview, releaseDate string
	runtime                                  int
	genres                                   []string
}

func Parse(body []byte, from, through string, generatedAt time.Time) (schedule.Dataset, error) {
	if len(body) == 0 || len(body) > MaxBodySize {
		return schedule.Dataset{}, fmt.Errorf("invalid page size")
	}
	root, err := settingsObject(body)
	if err != nil {
		return schedule.Dataset{}, err
	}
	variables, ok := root.(map[string]any)
	if !ok {
		return schedule.Dataset{}, fmt.Errorf("embedded schedule is incomplete")
	}
	if nested, ok := value(variables, "variables").(map[string]any); ok {
		variables = nested
	}
	complexes, films := map[string]string{}, map[string]film{}
	for _, object := range objectSlice(value(variables, "complexes")) {
		if id, name := stringValue(object, "id"), stringValue(object, "name"); id != "" && name != "" {
			complexes[id] = name
		}
	}
	currentMovies, _ := value(variables, "current_movies").(map[string]any)
	for _, object := range objectSlice(value(currentMovies, "films")) {
		candidate := parseFilm(object)
		if candidate.id != "" && candidate.title != "" && candidate.runtime > 0 {
			films[candidate.id] = candidate
		}
	}
	sessions := []map[string]any{}
	for _, object := range objectSlice(value(currentMovies, "sessions")) {
		if stringValue(object, "vistaSessionId") != "" {
			sessions = append(sessions, object)
		}
	}
	if len(complexes) == 0 || len(films) == 0 || len(sessions) == 0 {
		return schedule.Dataset{}, fmt.Errorf("embedded schedule is incomplete")
	}
	location, _ := time.LoadLocation(schedule.Timezone)
	fromDate, err1 := time.ParseInLocation("2006-01-02", from, location)
	throughDate, err2 := time.ParseInLocation("2006-01-02", through, location)
	if err1 != nil || err2 != nil || !schedule.ValidInclusiveDateWindow(fromDate, throughDate) {
		return schedule.Dataset{}, fmt.Errorf("invalid date window")
	}
	data := schedule.Dataset{SchemaVersion: schedule.SchemaVersion, Provider: schedule.ProviderKinepolis, Scope: schedule.ScopeAll, GeneratedAt: generatedAt.UTC(), Timezone: schedule.Timezone, Window: schedule.Window{From: from, Through: through}, Theaters: []schedule.TheaterRecord{}, Showtimes: []schedule.ShowtimeRecord{}}
	dates := map[string]map[string]bool{}
	usedComplexes := map[string]bool{}
	seenSessions := map[string]bool{}
	for _, session := range sessions {
		if falseValue(session, "public", "isPublic") || trueValue(session, "sold", "soldOut", "isSoldOut") {
			continue
		}
		complexID := stringValue(session, "complexOperator")
		name, ok := complexes[complexID]
		if !ok {
			return schedule.Dataset{}, fmt.Errorf("session references unknown complex")
		}
		filmObject, _ := value(session, "film").(map[string]any)
		filmID := stringValue(filmObject, "id")
		movie, ok := films[filmID]
		if !ok {
			return schedule.Dataset{}, fmt.Errorf("session references unknown film")
		}
		showingID := stringValue(session, "vistaSessionId")
		start, err := time.Parse(time.RFC3339, stringValue(session, "showtime"))
		if err != nil {
			return schedule.Dataset{}, fmt.Errorf("invalid session showtime")
		}
		local := start.In(location)
		service := local
		if local.Hour() <= 2 {
			service = local.AddDate(0, 0, -1)
		} else if local.Hour() < 8 {
			continue
		}
		serviceDate := service.Format("2006-01-02")
		parsedService, _ := time.ParseInLocation("2006-01-02", serviceDate, location)
		if parsedService.Before(fromDate) || parsedService.After(throughDate) {
			continue
		}
		if seenSessions[showingID] {
			continue
		}
		seenSessions[showingID] = true
		usedComplexes[complexID] = true
		if dates[complexID] == nil {
			dates[complexID] = map[string]bool{}
		}
		dates[complexID][serviceDate] = true
		providerVersion := sessionAttributes(session)
		data.Showtimes = append(data.Showtimes, schedule.ShowtimeRecord{Provider: schedule.ProviderKinepolis, ID: "kinepolis-showing-" + showingID, ProviderShowingID: showingID, ServiceDate: serviceDate, TheaterID: "kinepolis-" + complexID, Movie: schedule.MovieRecord{Provider: schedule.ProviderKinepolis, ProviderID: movie.id, Slug: "kinepolis-film-" + movie.id, Title: movie.title, RuntimeMinutes: movie.runtime, PosterURL: movie.poster, Overview: movie.overview, ReleaseDate: movie.releaseDate, Genres: append([]string(nil), movie.genres...)}, StartTime: local, EndTime: local.Add(time.Duration(movie.runtime) * time.Minute), Language: language(providerVersion), ProviderVersion: nonempty(providerVersion, "standard"), Format: sessionFormat(session, filmObject, providerVersion), Room: stringValue(session, "hall"), BookingURL: "https://kinepolis.fr/direct-vista-redirect/" + showingID + "/0/" + complexID + "/0"})
		_ = name
	}
	if len(data.Showtimes) == 0 {
		return schedule.Dataset{}, fmt.Errorf("schedule contains no showtimes in window")
	}
	ids := make([]string, 0, len(usedComplexes))
	for id := range usedComplexes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		available := make([]string, 0, len(dates[id]))
		for date := range dates[id] {
			available = append(available, date)
		}
		sort.Strings(available)
		data.Theaters = append(data.Theaters, schedule.TheaterRecord{Provider: schedule.ProviderKinepolis, ID: "kinepolis-" + id, ProviderID: id, Slug: "kinepolis-" + id, Name: complexes[id], City: complexCity(complexes[id]), AvailableDates: available, AcceptedPasses: []string{}})
	}
	if err := schedule.ValidateDataset(data, true); err != nil {
		return schedule.Dataset{}, fmt.Errorf("validate Kinepolis schedule: %w", err)
	}
	return data, nil
}

const settingsMarkerBudget = 128

func settingsObject(body []byte) (any, error) {
	if value, ok := extendedSettingsJSON(body); ok {
		return value, nil
	}
	complexes, complexesOK := assignedJSON(body, []byte("Drupal.settings.variables.complexes"))
	sessions, sessionsOK := assignedJSON(body, []byte("Drupal.settings.variables.current_movies.sessions"))
	films, filmsOK := assignedJSON(body, []byte("Drupal.settings.variables.current_movies.films"))
	if complexesOK && sessionsOK && filmsOK {
		return map[string]any{"complexes": complexes, "current_movies": map[string]any{"sessions": sessions, "films": films}}, nil
	}
	markers := [][]byte{[]byte("Drupal.settings.variables"), []byte("Drupal.settings")}
	for _, marker := range markers {
		if value, ok := assignedJSON(body, marker); ok {
			return value, nil
		}
	}
	return nil, fmt.Errorf("Drupal settings JSON not found or malformed")
}

func extendedSettingsJSON(body []byte) (any, bool) {
	marker := []byte("jQuery.extend(Drupal.settings")
	for offset := 0; offset < len(body); {
		relative := bytes.Index(body[offset:], marker)
		if relative < 0 {
			return nil, false
		}
		start := offset + relative + len(marker)
		search := body[start:]
		if len(search) > settingsMarkerBudget+1 {
			search = search[:settingsMarkerBudget+1]
		}
		comma := bytes.IndexByte(search, ',')
		if comma >= 0 && len(bytes.TrimSpace(body[start:start+comma])) == 0 {
			decoder := json.NewDecoder(bytes.NewReader(body[start+comma+1:]))
			decoder.UseNumber()
			var root any
			if decoder.Decode(&root) == nil {
				if rootObject, ok := root.(map[string]any); ok {
					if variables, ok := value(rootObject, "variables").(map[string]any); ok && validSettingsVariables(variables) {
						return variables, true
					}
				}
			}
		}
		offset = start
	}
	return nil, false
}

func validSettingsVariables(variables map[string]any) bool {
	if _, ok := value(variables, "complexes").([]any); !ok {
		return false
	}
	currentMovies, ok := value(variables, "current_movies").(map[string]any)
	if !ok {
		return false
	}
	_, sessionsOK := value(currentMovies, "sessions").([]any)
	_, filmsOK := value(currentMovies, "films").([]any)
	return sessionsOK && filmsOK
}

func assignedJSON(body, marker []byte) (any, bool) {
	start := bytes.Index(body, marker)
	if start < 0 {
		return nil, false
	}
	start += len(marker)
	search := body[start:]
	if len(search) > settingsMarkerBudget+1 {
		search = search[:settingsMarkerBudget+1]
	}
	equals := bytes.IndexByte(search, '=')
	if equals < 0 {
		return nil, false
	}
	start += equals + 1
	decoder := json.NewDecoder(bytes.NewReader(body[start:]))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil {
		return nil, false
	}
	return value, true
}

func walk(value any, visit func(map[string]any)) {
	switch v := value.(type) {
	case map[string]any:
		visit(v)
		for _, child := range v {
			walk(child, visit)
		}
	case []any:
		for _, child := range v {
			walk(child, visit)
		}
	}
}
func objectSlice(value any) []map[string]any {
	raw, _ := value.([]any)
	objects := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if object, ok := item.(map[string]any); ok {
			objects = append(objects, object)
		}
	}
	return objects
}
func parseFilm(v map[string]any) film {
	id := stringValue(v, "id")
	title := nonempty(stringValue(v, "title"), stringValue(v, "name"))
	runtime := intValue(value(v, "duration"))
	poster := ""
	if images, ok := value(v, "images").([]any); ok {
		for _, raw := range images {
			image, _ := raw.(map[string]any)
			media := strings.ToLower(stringValue(image, "mediaType"))
			path := nonempty(stringValue(image, "source"), stringValue(image, "path"), stringValue(image, "url"))
			if path != "" && (poster == "" || strings.Contains(media, "poster")) {
				poster = imageURL(path)
				if strings.Contains(media, "poster") {
					break
				}
			}
		}
	}
	genres := []string{}
	if raw, ok := value(v, "genres").([]any); ok {
		for _, item := range raw {
			switch item := item.(type) {
			case string:
				if strings.TrimSpace(item) != "" {
					genres = append(genres, item)
				}
			case map[string]any:
				if name := stringValue(item, "name"); name != "" {
					genres = append(genres, name)
				}
			}
		}
	}
	return film{id: id, title: title, runtime: runtime, poster: poster, overview: stringValue(v, "synopsis"), releaseDate: normalizeDate(stringValue(v, "releaseDate")), genres: genres}
}
func value(v map[string]any, path ...string) any {
	var current any = v
	for _, key := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = nil
		for k, item := range object {
			if strings.EqualFold(k, key) {
				current = item
				break
			}
		}
		if current == nil {
			return nil
		}
	}
	return current
}
func stringValue(v map[string]any, path ...string) string {
	raw := value(v, path...)
	switch x := raw.(type) {
	case string:
		return strings.TrimSpace(x)
	case json.Number:
		return x.String()
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return ""
}
func intValue(v any) int {
	switch x := v.(type) {
	case json.Number:
		n, _ := strconv.Atoi(x.String())
		return n
	case float64:
		return int(x)
	case string:
		match := regexp.MustCompile(`\d+`).FindString(x)
		n, _ := strconv.Atoi(match)
		return n
	}
	return 0
}
func boolValue(v map[string]any, key string) (bool, bool) {
	raw := value(v, key)
	switch x := raw.(type) {
	case bool:
		return x, true
	case json.Number:
		return x.String() == "1", x.String() == "0" || x.String() == "1"
	case float64:
		return x == 1, x == 0 || x == 1
	case string:
		b, e := strconv.ParseBool(x)
		return b, e == nil
	}
	return false, false
}
func falseValue(v map[string]any, keys ...string) bool {
	for _, k := range keys {
		if b, ok := boolValue(v, k); ok && !b {
			return true
		}
	}
	return false
}
func trueValue(v map[string]any, keys ...string) bool {
	for _, k := range keys {
		if b, ok := boolValue(v, k); ok && b {
			return true
		}
	}
	return false
}
func sessionAttributes(v map[string]any) string {
	parts := []string{}
	for _, key := range []string{"language", "version", "attributes"} {
		collectStrings(value(v, key), &parts)
		walk(value(v, key), func(item map[string]any) {
			for _, field := range []string{"name", "value", "label"} {
				if s := stringValue(item, field); s != "" {
					parts = append(parts, s)
				}
			}
		})
		if s, ok := value(v, key).(string); ok {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " ")
}

func collectStrings(value any, destination *[]string) {
	switch item := value.(type) {
	case string:
		if strings.TrimSpace(item) != "" {
			*destination = append(*destination, item)
		}
	case []any:
		for _, child := range item {
			collectStrings(child, destination)
		}
	case map[string]any:
		for _, child := range item {
			collectStrings(child, destination)
		}
	}
}
func language(v string) string {
	lower := strings.ToLower(v)
	if strings.Contains(lower, "sme") {
		return schedule.LanguageVFSME
	}
	if strings.Contains(lower, "vost") || strings.Contains(lower, "sous-titr") {
		return schedule.LanguageVOSTFR
	}
	if strings.Contains(lower, "vo") || strings.Contains(lower, "original") {
		return schedule.LanguageVO
	}
	return schedule.LanguageVF
}
func sessionFormat(session, film map[string]any, attributes string) string {
	parts := []string{stringValue(film, "format", "name"), attributes}
	for _, key := range []string{"hall", "room", "screen", "technology"} {
		collectStrings(value(session, key), &parts)
	}
	return format(strings.Join(parts, " "))
}
func format(v string) string {
	lower := strings.ToLower(v)
	if strings.Contains(lower, "laser ultra") {
		return schedule.FormatLaserUltra
	}
	if strings.Contains(lower, "screenx") || strings.Contains(lower, "screen x") || strings.Contains(lower, "screen-x") {
		return schedule.FormatScreenX
	}
	for _, item := range []struct{ needle, value string }{
		{"4dx", schedule.Format4DX},
		{"imax", schedule.FormatIMAX},
		{"dolby", schedule.FormatDolby},
		{"3d", schedule.Format3D},
	} {
		if strings.Contains(lower, item.needle) {
			return item.value
		}
	}
	return schedule.Format2D
}
func imageURL(path string) string {
	if strings.HasPrefix(path, "https://cdn.kinepolis.fr/images/") {
		return path
	}
	if strings.Contains(path, "://") {
		return ""
	}
	if strings.HasPrefix(path, "/") {
		return "https://cdn.kinepolis.fr/images" + path
	}
	return "https://cdn.kinepolis.fr/images/" + path
}
func normalizeDate(v string) string {
	if len(v) >= 10 {
		if parsed, err := time.Parse("2006-01-02", v[:10]); err == nil {
			return parsed.Format("2006-01-02")
		}
	}
	return ""
}
func complexCity(name string) string {
	exceptions := map[string]string{"kinepolis waves": "Metz", "kinepolis saint-julien-lès-metz": "Metz", "kinepolis st-julien-lès-metz": "Metz"}
	key := strings.ToLower(strings.TrimSpace(name))
	if city := exceptions[key]; city != "" {
		return city
	}
	city := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(name, "Kinepolis"), "KINÉPOLIS"))
	if city == "" {
		return strings.TrimSpace(name)
	}
	return city
}
func nonempty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
