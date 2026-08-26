package synccontrol

import (
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxProviderLogLines = 20
	maxProviderLogLine  = 512
	maxProviderLogBytes = 8192
)

type logEvent string
type logOperation string
type logCategory string
type logParseReason string

const (
	eventProviderStarted     logEvent = "provider_started"
	eventClientReady         logEvent = "client_ready"
	eventFetchStarted        logEvent = "fetch_started"
	eventFetchSucceeded      logEvent = "fetch_succeeded"
	eventValidationSucceeded logEvent = "validation_succeeded"
	eventPublicationStarted  logEvent = "publication_started"
	eventProviderFailed      logEvent = "provider_failed"
	eventLogTruncated        logEvent = "log_truncated"

	operationClient            logOperation = "client"
	operationSitemap           logOperation = "sitemap"
	operationCinemas           logOperation = "cinemas"
	operationCinema            logOperation = "cinema"
	operationProgram           logOperation = "program"
	operationShowings          logOperation = "showings"
	operationMovies            logOperation = "movies"
	operationDatasetValidation logOperation = "dataset_validation"
	operationPublication       logOperation = "publication"
	operationOrchestration     logOperation = "orchestration"
	operationLog               logOperation = "log"
	operationUnknown           logOperation = "unknown"

	categoryStarted              logCategory = "started"
	categorySucceeded            logCategory = "succeeded"
	categoryCanceled             logCategory = "canceled"
	categoryInvalidURL           logCategory = "invalid_url"
	categoryTransportUnavailable logCategory = "transport_unavailable"
	categoryTransport            logCategory = "transport"
	categoryRedirect             logCategory = "redirect"
	categoryResponseRead         logCategory = "response_read"
	categoryResponseTooLarge     logCategory = "response_too_large"
	categoryChallenge            logCategory = "challenge"
	categoryHTTPStatus           logCategory = "http_status"
	categoryContentType          logCategory = "content_type"
	categoryInvalidPayload       logCategory = "invalid_payload"
	categoryEmptyResponse        logCategory = "empty_response"
	categoryValidation           logCategory = "validation"
	categoryPublication          logCategory = "publication"
	categoryInternal             logCategory = "internal"
	categoryUnknown              logCategory = "unknown"
	categoryTruncated            logCategory = "truncated"

	parseReasonDocumentParse                         logParseReason = "document_parse"
	parseReasonTimezoneUnavailable                   logParseReason = "timezone_unavailable"
	parseReasonInvalidServiceDate                    logParseReason = "invalid_service_date"
	parseReasonShowingAttributesMissingOrConflicting logParseReason = "showing_attributes_missing_or_conflicting"
	parseReasonConflictingDuplicateShowing           logParseReason = "conflicting_duplicate_showing"
	parseReasonUnrecognizedShowingsDocument          logParseReason = "unrecognized_showings_document"
	parseReasonFilmIdentityConflict                  logParseReason = "film_identity_conflict"
	parseReasonFilmTitleMissing                      logParseReason = "film_title_missing"
	parseReasonFilmTitleConflicting                  logParseReason = "film_title_conflicting"
	parseReasonFilmRuntimeMissing                    logParseReason = "film_runtime_missing"
	parseReasonInvalidFilmRuntime                    logParseReason = "invalid_film_runtime"
	parseReasonUnrecognizedShowingOwnership          logParseReason = "unrecognized_showing_ownership"
	parseReasonInvalidFilmDetailLink                 logParseReason = "invalid_film_detail_link"
	parseReasonUnknownShowingVersion                 logParseReason = "unknown_showing_version"
	parseReasonInvalidShowingHour                    logParseReason = "invalid_showing_hour"
	parseReasonShowingOutsideCinemaDay               logParseReason = "showing_outside_cinema_day"
	parseReasonInvalidShowingDate                    logParseReason = "invalid_showing_date"
	parseReasonUnknownShowingFormat                  logParseReason = "unknown_showing_format"
	parseReasonShowingEndMissingOrConflicting        logParseReason = "showing_end_missing_or_conflicting"
	parseReasonInvalidShowingEnd                     logParseReason = "invalid_showing_end"
	parseReasonUnknown                               logParseReason = "unknown"
)

type logProgress struct {
	Requests  *int
	Cinemas   *int
	Movies    *int
	Dates     *int
	Showtimes *int
	Skipped   *int
}

type logRecord struct {
	Timestamp    time.Time
	Provider     Target
	Event        logEvent
	Stage        FailureStage
	Operation    logOperation
	Category     logCategory
	ParseReason  logParseReason
	HTTPStatus   int
	Attempt      int
	AttemptLimit int
	Progress     logProgress
}

type logFailure struct {
	Operation    logOperation
	Category     logCategory
	ParseReason  logParseReason
	HTTPStatus   int
	Attempt      int
	AttemptLimit int
	Progress     logProgress
}

func lifecycleLog(timestamp time.Time, provider Target, event logEvent) string {
	record := logRecord{Timestamp: timestamp, Provider: provider, Event: event}
	switch event {
	case eventProviderStarted:
		record.Stage, record.Operation, record.Category = StageOrchestration, operationOrchestration, categoryStarted
	case eventClientReady:
		record.Stage, record.Operation, record.Category = StageClientCreation, operationClient, categorySucceeded
	case eventFetchStarted:
		record.Stage, record.Operation, record.Category = StageProviderFetch, operationUnknown, categoryStarted
	case eventFetchSucceeded:
		record.Stage, record.Operation, record.Category = StageProviderFetch, operationUnknown, categorySucceeded
	case eventValidationSucceeded:
		record.Stage, record.Operation, record.Category = StageDatasetValidation, operationDatasetValidation, categorySucceeded
	case eventPublicationStarted:
		record.Stage, record.Operation, record.Category = StagePublication, operationPublication, categoryStarted
	default:
		return ""
	}
	line, _ := serializeLogRecord(record)
	return line
}

func failureLog(timestamp time.Time, provider Target, stage FailureStage, failure logFailure) string {
	record := logRecord{
		Timestamp: timestamp, Provider: provider, Event: eventProviderFailed, Stage: stage,
		Operation: failure.Operation, Category: failure.Category, ParseReason: failure.ParseReason, HTTPStatus: failure.HTTPStatus,
		Attempt: failure.Attempt, AttemptLimit: failure.AttemptLimit, Progress: failure.Progress,
	}
	if record.Operation == "" {
		record.Operation = operationUnknown
	}
	if record.Category == "" {
		record.Category = categoryUnknown
	}
	if parseReasonContext(record) && !validLogParseReason(record.ParseReason) {
		record.ParseReason = parseReasonUnknown
	}
	line, _ := serializeLogRecord(record)
	return line
}

func truncationLog(timestamp time.Time, provider Target) string {
	line, _ := serializeLogRecord(logRecord{Timestamp: timestamp, Provider: provider, Event: eventLogTruncated, Stage: StageOrchestration, Operation: operationLog, Category: categoryTruncated})
	return line
}

func serializeLogRecord(record logRecord) (string, bool) {
	if !validLogProvider(record.Provider) || record.Timestamp.IsZero() || record.Timestamp.Location() != time.UTC {
		return "", false
	}
	level, message, ok := logMessage(record.Event, record.Stage, record.Category)
	if !ok || !validLogOperation(record.Operation) {
		return "", false
	}
	if record.ParseReason != "" && (!parseReasonContext(record) || !validLogParseReason(record.ParseReason)) {
		return "", false
	}
	if record.HTTPStatus != 0 && (record.Category != categoryHTTPStatus || record.HTTPStatus < 100 || record.HTTPStatus > 599) {
		return "", false
	}
	if record.Attempt != 0 || record.AttemptLimit != 0 {
		if record.Attempt < 1 || record.AttemptLimit < record.Attempt || record.AttemptLimit > 10 {
			return "", false
		}
	}
	var line strings.Builder
	line.WriteString("ts=")
	line.WriteString(record.Timestamp.Format(time.RFC3339Nano))
	line.WriteString(" level=")
	line.WriteString(level)
	line.WriteString(" provider=")
	line.WriteString(string(record.Provider))
	line.WriteString(" event=")
	line.WriteString(string(record.Event))
	line.WriteString(" stage=")
	line.WriteString(string(record.Stage))
	line.WriteString(" operation=")
	line.WriteString(string(record.Operation))
	line.WriteString(" category=")
	line.WriteString(string(record.Category))
	if record.ParseReason != "" {
		line.WriteString(" parse_reason=")
		line.WriteString(string(record.ParseReason))
	}
	if record.HTTPStatus != 0 {
		line.WriteString(" http_status=")
		line.WriteString(strconv.Itoa(record.HTTPStatus))
	}
	if record.Attempt != 0 {
		line.WriteString(" attempt=")
		line.WriteString(strconv.Itoa(record.Attempt))
		line.WriteByte('/')
		line.WriteString(strconv.Itoa(record.AttemptLimit))
	}
	for _, counter := range []struct {
		name  string
		value *int
	}{
		{"requests", record.Progress.Requests}, {"cinemas", record.Progress.Cinemas}, {"movies", record.Progress.Movies},
		{"dates", record.Progress.Dates}, {"showtimes", record.Progress.Showtimes}, {"skipped", record.Progress.Skipped},
	} {
		if counter.value == nil || *counter.value < 0 {
			if counter.value != nil {
				return "", false
			}
			continue
		}
		line.WriteByte(' ')
		line.WriteString(counter.name)
		line.WriteByte('=')
		line.WriteString(strconv.Itoa(*counter.value))
	}
	line.WriteString(" message=")
	line.WriteString(strconv.Quote(message))
	result := line.String()
	if len(result) > maxProviderLogLine || !utf8.ValidString(result) || strings.IndexFunc(result, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
		return "", false
	}
	return result, true
}

func logMessage(event logEvent, stage FailureStage, category logCategory) (string, string, bool) {
	switch event {
	case eventProviderStarted:
		return "info", "Synchronisation fournisseur démarrée.", stage == StageOrchestration && category == categoryStarted
	case eventClientReady:
		return "info", "Client fournisseur initialisé.", stage == StageClientCreation && category == categorySucceeded
	case eventFetchStarted:
		return "info", "Récupération fournisseur démarrée.", stage == StageProviderFetch && category == categoryStarted
	case eventFetchSucceeded:
		return "info", "Récupération fournisseur terminée.", stage == StageProviderFetch && category == categorySucceeded
	case eventValidationSucceeded:
		return "info", "Jeu de données fournisseur validé.", stage == StageDatasetValidation && category == categorySucceeded
	case eventPublicationStarted:
		return "info", "Publication du jeu de données démarrée.", stage == StagePublication && category == categoryStarted
	case eventLogTruncated:
		return "warn", "Des lignes antérieures ou non conformes ont été supprimées.", stage == StageOrchestration && category == categoryTruncated
	case eventProviderFailed:
		return failureMessage(stage, category)
	default:
		return "", "", false
	}
}

func failureMessage(stage FailureStage, category logCategory) (string, string, bool) {
	if category == categoryCanceled {
		return "warn", "La synchronisation a été interrompue.", true
	}
	if stage == StageClientCreation {
		return "error", "Initialisation du client fournisseur impossible.", category == categoryInternal || category == categoryUnknown
	}
	switch category {
	case categoryInvalidURL:
		return "error", "La requête fournisseur a été rejetée avant envoi.", stage == StageProviderFetch
	case categoryTransportUnavailable:
		return "error", "Aucun transport fournisseur n’était disponible.", stage == StageProviderFetch
	case categoryTransport:
		return "error", "La connexion au fournisseur a échoué.", stage == StageProviderFetch
	case categoryRedirect:
		return "error", "La redirection du fournisseur a été rejetée.", stage == StageProviderFetch
	case categoryResponseRead:
		return "error", "La réponse du fournisseur n’a pas pu être lue.", stage == StageProviderFetch
	case categoryResponseTooLarge:
		return "error", "La réponse du fournisseur était trop volumineuse.", stage == StageProviderFetch
	case categoryChallenge:
		return "error", "Le fournisseur a renvoyé une page de protection anti-robot.", stage == StageProviderFetch
	case categoryHTTPStatus:
		return "error", "Le fournisseur a renvoyé un statut HTTP inattendu.", stage == StageProviderFetch
	case categoryContentType:
		return "error", "Le fournisseur a renvoyé un type de contenu inattendu.", stage == StageProviderFetch
	case categoryInvalidPayload:
		return "error", "La réponse du fournisseur n’a pas pu être interprétée.", stage == StageProviderFetch
	case categoryEmptyResponse:
		return "error", "Le fournisseur a renvoyé une réponse vide.", stage == StageProviderFetch
	case categoryValidation:
		return "error", "Le jeu de données fournisseur a été rejeté.", stage == StageDatasetValidation
	case categoryPublication:
		return "error", "La publication du jeu de données a échoué.", stage == StagePublication
	case categoryInternal:
		return "error", "La synchronisation a échoué en interne.", stage == StageOrchestration
	case categoryUnknown:
		if stage == StageProviderFetch {
			return "error", "La récupération des données fournisseur a échoué.", true
		}
		if stage == StageOrchestration {
			return "error", "La synchronisation a échoué en interne.", true
		}
	}
	return "", "", false
}

func normalizeProviderLog(provider Target, lines []string, startedAt time.Time, finishedAt *time.Time) []string {
	if len(lines) == 0 {
		return nil
	}
	valid := make([]parsedLogLine, 0, len(lines))
	removed := false
	for index, line := range lines {
		parsed, ok := parseLogLine(line, provider, startedAt, finishedAt)
		if !ok {
			removed = true
			continue
		}
		parsed.index = index
		valid = append(valid, parsed)
	}
	sort.SliceStable(valid, func(i, j int) bool { return valid[i].timestamp.Before(valid[j].timestamp) })
	if len(valid) > maxProviderLogLines || joinedLogBytes(valid) > maxProviderLogBytes {
		removed = true
	}
	if !removed {
		result := make([]string, len(valid))
		for i := range valid {
			result[i] = valid[i].line
		}
		return result
	}
	if finishedAt == nil || finishedAt.IsZero() || finishedAt.Before(startedAt.UTC()) {
		return nil
	}
	notice := truncationLog(finishedAt.UTC(), provider)
	kept := make([]parsedLogLine, 0, maxProviderLogLines-1)
	bytes := len(notice)
	for i := len(valid) - 1; i >= 0 && len(kept) < maxProviderLogLines-1; i-- {
		if valid[i].event == eventLogTruncated || bytes+1+len(valid[i].line) > maxProviderLogBytes {
			continue
		}
		bytes += 1 + len(valid[i].line)
		kept = append(kept, valid[i])
	}
	result := make([]string, 0, len(kept)+1)
	for i := len(kept) - 1; i >= 0; i-- {
		result = append(result, kept[i].line)
	}
	return append(result, notice)
}

type parsedLogLine struct {
	line      string
	timestamp time.Time
	event     logEvent
	index     int
}

func parseLogLine(line string, provider Target, startedAt time.Time, finishedAt *time.Time) (parsedLogLine, bool) {
	if len(line) > maxProviderLogLine || !utf8.ValidString(line) || finishedAt == nil {
		return parsedLogLine{}, false
	}
	left, quotedMessage, found := strings.Cut(line, " message=")
	if !found {
		return parsedLogLine{}, false
	}
	message, err := strconv.Unquote(quotedMessage)
	if err != nil {
		return parsedLogLine{}, false
	}
	fields := strings.Fields(left)
	if len(fields) < 7 {
		return parsedLogLine{}, false
	}
	values := make(map[string]string, len(fields))
	for _, field := range fields {
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			return parsedLogLine{}, false
		}
		values[key] = value
	}
	timestamp, err := time.Parse(time.RFC3339Nano, values["ts"])
	if err != nil || timestamp.Location() != time.UTC || timestamp.Before(startedAt.UTC()) || timestamp.After(finishedAt.UTC()) || values["provider"] != string(provider) {
		return parsedLogLine{}, false
	}
	record := logRecord{Timestamp: timestamp, Provider: provider, Event: logEvent(values["event"]), Stage: FailureStage(values["stage"]), Operation: logOperation(values["operation"]), Category: logCategory(values["category"]), ParseReason: logParseReason(values["parse_reason"])}
	if value := values["http_status"]; value != "" {
		record.HTTPStatus, err = strconv.Atoi(value)
		if err != nil {
			return parsedLogLine{}, false
		}
	}
	if value := values["attempt"]; value != "" {
		parts := strings.Split(value, "/")
		if len(parts) != 2 {
			return parsedLogLine{}, false
		}
		record.Attempt, err = strconv.Atoi(parts[0])
		if err == nil {
			record.AttemptLimit, err = strconv.Atoi(parts[1])
		}
		if err != nil {
			return parsedLogLine{}, false
		}
	}
	for name, destination := range map[string]**int{"requests": &record.Progress.Requests, "cinemas": &record.Progress.Cinemas, "movies": &record.Progress.Movies, "dates": &record.Progress.Dates, "showtimes": &record.Progress.Showtimes, "skipped": &record.Progress.Skipped} {
		if value := values[name]; value != "" {
			n, parseErr := strconv.Atoi(value)
			if parseErr != nil {
				return parsedLogLine{}, false
			}
			*destination = &n
		}
	}
	canonical, ok := serializeLogRecord(record)
	if !ok || canonical != line {
		return parsedLogLine{}, false
	}
	_, expectedMessage, _ := logMessage(record.Event, record.Stage, record.Category)
	if message != expectedMessage {
		return parsedLogLine{}, false
	}
	return parsedLogLine{line: line, timestamp: timestamp, event: record.Event}, true
}

func joinedLogBytes(lines []parsedLogLine) int {
	total := 0
	for i := range lines {
		total += len(lines[i].line)
		if i > 0 {
			total++
		}
	}
	return total
}

func validLogProvider(provider Target) bool {
	return provider == TargetUGC || provider == TargetKinepolis || provider == TargetPathe || provider == TargetCGR
}

func validLogOperation(operation logOperation) bool {
	switch operation {
	case operationClient, operationSitemap, operationCinemas, operationCinema, operationProgram, operationShowings, operationMovies, operationDatasetValidation, operationPublication, operationOrchestration, operationLog, operationUnknown:
		return true
	default:
		return false
	}
}

func parseReasonContext(record logRecord) bool {
	return record.Provider == TargetUGC && record.Event == eventProviderFailed && record.Stage == StageProviderFetch && record.Operation == operationShowings && record.Category == categoryInvalidPayload
}

func validLogParseReason(reason logParseReason) bool {
	switch reason {
	case parseReasonDocumentParse, parseReasonTimezoneUnavailable, parseReasonInvalidServiceDate,
		parseReasonShowingAttributesMissingOrConflicting, parseReasonConflictingDuplicateShowing,
		parseReasonUnrecognizedShowingsDocument, parseReasonFilmIdentityConflict, parseReasonFilmTitleMissing,
		parseReasonFilmTitleConflicting, parseReasonFilmRuntimeMissing, parseReasonInvalidFilmRuntime,
		parseReasonUnrecognizedShowingOwnership, parseReasonInvalidFilmDetailLink, parseReasonUnknownShowingVersion,
		parseReasonInvalidShowingHour, parseReasonShowingOutsideCinemaDay, parseReasonInvalidShowingDate,
		parseReasonUnknownShowingFormat, parseReasonShowingEndMissingOrConflicting, parseReasonInvalidShowingEnd,
		parseReasonUnknown:
		return true
	default:
		return false
	}
}

func sanitizeStatusLogs(status Status) Status {
	copy := status
	if status.Providers == nil {
		return copy
	}
	copy.Providers = make(map[string]ProviderStatus, len(status.Providers))
	for key, providerStatus := range status.Providers {
		provider := Target(key)
		if providerStatus.State == ProviderFailed && validLogProvider(provider) {
			providerStatus.Log = normalizeProviderLog(provider, providerStatus.Log, status.StartedAt, status.FinishedAt)
		} else {
			providerStatus.Log = nil
		}
		copy.Providers[key] = providerStatus
	}
	return copy
}

// SanitizeStatus returns a detached status safe for persistence or API output.
func SanitizeStatus(status Status) Status { return cloneStatus(status) }

func intPointer(value int) *int { return &value }
