package synccontrol

import (
	"errors"
	"strings"
	"time"

	"messeances/api/internal/cgr"
	"messeances/api/internal/cinemovida"
	"messeances/api/internal/cineode"
	"messeances/api/internal/cineville"
	"messeances/api/internal/cinewest"
	"messeances/api/internal/grandecran"
	"messeances/api/internal/kinepolis"
	"messeances/api/internal/majestic"
	"messeances/api/internal/megarama"
	"messeances/api/internal/mk2"
	"messeances/api/internal/noecinemas"
	"messeances/api/internal/ocine"
	"messeances/api/internal/pathe"
	"messeances/api/internal/scare"
	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
	"messeances/api/internal/syncsource"
	"messeances/api/internal/ticketingcine"
	"messeances/api/internal/ugc"
	"messeances/api/internal/veo"
	"messeances/api/internal/webedia"
)

// Shared classifiers for providers built on syncproxy.RequestError.
var (
	classifyProxy        = classifyProxyRequest(syncproxy.PhaseUnknown, false)
	classifyProgramProxy = classifyProxyRequest(syncproxy.PhaseProgram, false)
	classifyProgramPages = classifyProxyRequest(syncproxy.PhaseProgram, true)
)

// providerRegistry holds the static traits of every synchronized provider.
// Run order comes from schedule.SourceProviders, not from this table.
var providerRegistry = [...]providerTraits{
	{target: TargetUGC, classify: classifyUGC},
	{target: TargetKinepolis, classify: classifyKinepolis, typedFetchError: isKinepolisRequestError},
	{target: TargetPathe, classify: classifyPathe},
	{target: TargetCGR, classify: classifyCGR},
	{target: TargetMegarama, enrichUnknownRuntime: true, classify: classifyProxy},
	{target: TargetCineville, enrichUnknownRuntime: true, classify: classifyProxy},
	{target: TargetMK2, enrichUnknownRuntime: true, classify: classifyProxy},
	{target: TargetCinewest, enrichUnknownRuntime: true, classify: classifyProxy},
	{target: TargetGrandEcran, enrichUnknownRuntime: true, classify: classifyProxy},
	{target: TargetNoeCinemas, enrichUnknownRuntime: true, classify: classifyProxy},
	{target: TargetVeo, enrichUnknownRuntime: true, retainLogs: true, classify: classifyVeo, logArgs: veoFetchLogArgs},
	{target: TargetMajestic, enrichUnknownRuntime: true, classify: classifyProgramProxy},
	{target: TargetCineode, budget: fetchBudgetOperation, enrichUnknownRuntime: true, classify: classifyProgramPages},
	{target: TargetCinemovida, budget: fetchBudgetCrawl, enrichUnknownRuntime: true, classify: classifyProxy},
	{target: TargetOcine, budget: fetchBudgetCrawl, enrichUnknownRuntime: true, classify: classifyProgramPages},
	{target: TargetTicketingcine, enrichUnknownRuntime: true, classify: classifyProxy},
	{target: TargetScare, enrichUnknownRuntime: true, classify: classifyProxy},
	{target: TargetWebedia, budget: fetchBudgetCrawl, enrichUnknownRuntime: true, classify: classifyProxy},
}

// sourceOutcome maps the summary counters most providers share.
func sourceOutcome(cinemas, movies, requests, showtimes int, generatedAt time.Time) SyncOutcome {
	return SyncOutcome{Cinemas: cinemas, Movies: movies, Requests: requests, Showtimes: showtimes, GeneratedAt: generatedAt}
}

func UGCConnector(newClient func() (ugc.Getter, error)) *SourceConnector[ugc.Getter, ugc.SyncOptions, ugc.SyncSummary] {
	return &SourceConnector[ugc.Getter, ugc.SyncOptions, ugc.SyncSummary]{
		target: TargetUGC, newClient: newClient, sync: ugc.Sync,
		options: func(from string, now time.Time) ugc.SyncOptions { return ugc.SyncOptions{From: from, Now: now} },
		outcome: func(s ugc.SyncSummary) SyncOutcome {
			return SyncOutcome{Cinemas: s.Cinemas, Dates: s.Dates, Requests: s.Requests, Showtimes: s.Showtimes, Skipped: s.Skipped, GeneratedAt: s.GeneratedAt}
		},
	}
}

func KinepolisConnector(newClient func() (kinepolis.Fetcher, error)) *SourceConnector[kinepolis.Fetcher, kinepolis.SyncOptions, kinepolis.SyncSummary] {
	return &SourceConnector[kinepolis.Fetcher, kinepolis.SyncOptions, kinepolis.SyncSummary]{
		target: TargetKinepolis, newClient: newClient, sync: kinepolis.Sync,
		options: func(from string, now time.Time) kinepolis.SyncOptions {
			return kinepolis.SyncOptions{From: from, Now: now}
		},
		outcome: func(s kinepolis.SyncSummary) SyncOutcome {
			return SyncOutcome{Cinemas: s.Cinemas, Showtimes: s.Showtimes, GeneratedAt: s.GeneratedAt}
		},
	}
}

func PatheConnector(newClient func() (pathe.Getter, error)) *SourceConnector[pathe.Getter, pathe.SyncOptions, pathe.SyncSummary] {
	return &SourceConnector[pathe.Getter, pathe.SyncOptions, pathe.SyncSummary]{
		target: TargetPathe, newClient: newClient, sync: pathe.Sync,
		options: func(from string, now time.Time) pathe.SyncOptions { return pathe.SyncOptions{From: from, Now: now} },
		outcome: func(s pathe.SyncSummary) SyncOutcome {
			return SyncOutcome{Cinemas: s.Cinemas, Requests: s.Requests, Showtimes: s.Showtimes, GeneratedAt: s.GeneratedAt}
		},
	}
}

func CGRConnector(newClient func() (cgr.Getter, error)) *SourceConnector[cgr.Getter, cgr.SyncOptions, cgr.SyncSummary] {
	return &SourceConnector[cgr.Getter, cgr.SyncOptions, cgr.SyncSummary]{
		target: TargetCGR, newClient: newClient, sync: cgr.Sync,
		options: func(from string, now time.Time) cgr.SyncOptions { return cgr.SyncOptions{From: from, Now: now} },
		outcome: func(s cgr.SyncSummary) SyncOutcome {
			return sourceOutcome(s.Cinemas, s.Movies, s.Requests, s.Showtimes, s.GeneratedAt)
		},
	}
}

func MegaramaConnector(newClient func() (megarama.Getter, error)) *SourceConnector[megarama.Getter, megarama.SyncOptions, megarama.SyncSummary] {
	return &SourceConnector[megarama.Getter, megarama.SyncOptions, megarama.SyncSummary]{
		target: TargetMegarama, newClient: newClient, sync: megarama.Sync,
		options: func(from string, now time.Time) megarama.SyncOptions {
			return megarama.SyncOptions{From: from, Now: now}
		},
		outcome: func(s megarama.SyncSummary) SyncOutcome {
			return sourceOutcome(s.Cinemas, s.Movies, s.Requests, s.Showtimes, s.GeneratedAt)
		},
	}
}

func CinevilleConnector(newClient func() (cineville.Fetcher, error)) *SourceConnector[cineville.Fetcher, cineville.SyncOptions, cineville.SyncSummary] {
	return &SourceConnector[cineville.Fetcher, cineville.SyncOptions, cineville.SyncSummary]{
		target: TargetCineville, newClient: newClient, sync: cineville.Sync,
		options: func(from string, now time.Time) cineville.SyncOptions {
			return cineville.SyncOptions{From: from, Now: now}
		},
		outcome: func(s cineville.SyncSummary) SyncOutcome {
			return sourceOutcome(s.Cinemas, s.Movies, s.Requests, s.Showtimes, s.GeneratedAt)
		},
	}
}

func MK2Connector(newClient func() (mk2.Fetcher, error)) *SourceConnector[mk2.Fetcher, mk2.SyncOptions, mk2.SyncSummary] {
	return &SourceConnector[mk2.Fetcher, mk2.SyncOptions, mk2.SyncSummary]{
		target: TargetMK2, newClient: newClient, sync: mk2.Sync,
		options: func(from string, now time.Time) mk2.SyncOptions { return mk2.SyncOptions{From: from, Now: now} },
		outcome: func(s mk2.SyncSummary) SyncOutcome {
			return sourceOutcome(s.Cinemas, s.Movies, s.Requests, s.Showtimes, s.GeneratedAt)
		},
	}
}

func CinewestConnector(newClient func() (cinewest.Fetcher, error)) *SourceConnector[cinewest.Fetcher, cinewest.SyncOptions, cinewest.SyncSummary] {
	return &SourceConnector[cinewest.Fetcher, cinewest.SyncOptions, cinewest.SyncSummary]{
		target: TargetCinewest, newClient: newClient, sync: cinewest.Sync,
		options: func(from string, now time.Time) cinewest.SyncOptions {
			return cinewest.SyncOptions{From: from, Now: now}
		},
		outcome: func(s cinewest.SyncSummary) SyncOutcome {
			outcome := sourceOutcome(s.Cinemas, s.Movies, s.Requests, s.Showtimes, s.GeneratedAt)
			outcome.Skipped = s.Skipped
			return outcome
		},
	}
}

func GrandEcranConnector(newClient func() (grandecran.Getter, error)) *SourceConnector[grandecran.Getter, grandecran.SyncOptions, grandecran.SyncSummary] {
	return &SourceConnector[grandecran.Getter, grandecran.SyncOptions, grandecran.SyncSummary]{
		target: TargetGrandEcran, newClient: newClient, sync: grandecran.Sync,
		options: func(from string, now time.Time) grandecran.SyncOptions {
			return grandecran.SyncOptions{From: from, Now: now}
		},
		outcome: func(s grandecran.SyncSummary) SyncOutcome {
			return sourceOutcome(s.Cinemas, s.Movies, s.Requests, s.Showtimes, s.GeneratedAt)
		},
	}
}

func NoeCinemasConnector(newClient func() (noecinemas.Getter, error)) *SourceConnector[noecinemas.Getter, noecinemas.SyncOptions, noecinemas.SyncSummary] {
	return &SourceConnector[noecinemas.Getter, noecinemas.SyncOptions, noecinemas.SyncSummary]{
		target: TargetNoeCinemas, newClient: newClient, sync: noecinemas.Sync,
		options: func(from string, now time.Time) noecinemas.SyncOptions {
			return noecinemas.SyncOptions{From: from, Now: now}
		},
		outcome: func(s noecinemas.SyncSummary) SyncOutcome {
			return sourceOutcome(s.Cinemas, s.Movies, s.Requests, s.Showtimes, s.GeneratedAt)
		},
	}
}

func VeoConnector(newClient func() (veo.Getter, error)) *SourceConnector[veo.Getter, veo.SyncOptions, veo.SyncSummary] {
	return &SourceConnector[veo.Getter, veo.SyncOptions, veo.SyncSummary]{
		target: TargetVeo, newClient: newClient, sync: veo.Sync,
		options: func(from string, now time.Time) veo.SyncOptions { return veo.SyncOptions{From: from, Now: now} },
		outcome: func(s veo.SyncSummary) SyncOutcome {
			return sourceOutcome(s.Cinemas, s.Movies, s.Requests, s.Showtimes, s.GeneratedAt)
		},
		report: reportVeo,
	}
}

func MajesticConnector(newClient func() (majestic.Getter, error)) *SourceConnector[majestic.Getter, majestic.SyncOptions, majestic.SyncSummary] {
	return &SourceConnector[majestic.Getter, majestic.SyncOptions, majestic.SyncSummary]{
		target: TargetMajestic, newClient: newClient, sync: majestic.Sync,
		options: func(from string, now time.Time) majestic.SyncOptions {
			return majestic.SyncOptions{From: from, Now: now}
		},
		outcome: func(s majestic.SyncSummary) SyncOutcome {
			return sourceOutcome(s.Cinemas, s.Movies, s.Requests, s.Showtimes, s.GeneratedAt)
		},
	}
}

func CineodeConnector(newClient func() (cineode.Getter, error)) *SourceConnector[cineode.Getter, cineode.SyncOptions, cineode.SyncSummary] {
	return &SourceConnector[cineode.Getter, cineode.SyncOptions, cineode.SyncSummary]{
		target: TargetCineode, newClient: newClient, sync: cineode.Sync,
		options: func(from string, now time.Time) cineode.SyncOptions {
			return cineode.SyncOptions{From: from, Now: now}
		},
		outcome: func(s cineode.SyncSummary) SyncOutcome {
			return sourceOutcome(s.Cinemas, s.Movies, s.Requests, s.Showtimes, s.GeneratedAt)
		},
	}
}

func CinemovidaConnector(newClient func() (cinemovida.Getter, error)) *SourceConnector[cinemovida.Getter, cinemovida.SyncOptions, cinemovida.SyncSummary] {
	return &SourceConnector[cinemovida.Getter, cinemovida.SyncOptions, cinemovida.SyncSummary]{
		target: TargetCinemovida, newClient: newClient, sync: cinemovida.Sync,
		options: func(from string, now time.Time) cinemovida.SyncOptions {
			return cinemovida.SyncOptions{From: from, Now: now}
		},
		outcome: func(s cinemovida.SyncSummary) SyncOutcome {
			return sourceOutcome(s.Cinemas, s.Movies, s.Requests, s.Showtimes, s.GeneratedAt)
		},
		report: func(e *ProductionExecutor, s cinemovida.SyncSummary, _ error, _ *[]string) {
			e.logger.Info("cinemovida_source_end_summary", "attempted", s.EndsAttempted, "recovered", s.EndsRecovered, "unresolved", s.EndsUnresolved, "budget_skipped", s.EndsBudgetSkipped)
		},
	}
}

func OcineConnector(newClient func() (ocine.Getter, error)) *SourceConnector[ocine.Getter, ocine.SyncOptions, ocine.SyncSummary] {
	return &SourceConnector[ocine.Getter, ocine.SyncOptions, ocine.SyncSummary]{
		target: TargetOcine, newClient: newClient, sync: ocine.Sync,
		options: func(from string, now time.Time) ocine.SyncOptions { return ocine.SyncOptions{From: from, Now: now} },
		outcome: func(s ocine.SyncSummary) SyncOutcome {
			return sourceOutcome(s.Cinemas, s.Movies, s.Requests, s.Showtimes, s.GeneratedAt)
		},
		report: func(e *ProductionExecutor, s ocine.SyncSummary, _ error, _ *[]string) {
			e.logger.Info("ocine_source_summary", "checkouts", s.EndsAttempted, "ends", s.EndsRecovered, "missing_rooms", s.RoomsMissing, "checkout_transient_failures", s.CheckoutTransientFailures, "checkout_budget_skipped", s.CheckoutBudgetSkipped)
		},
	}
}

// TicketingcineConnector serves the generic TicketingCiné cinemas listed in
// sync sources; source keys are EMSdddd sites.
func TicketingcineConnector(newClient func() (ticketingcine.Getter, error), sources SourceLister) *SourceConnector[ticketingcine.Getter, ticketingcine.SyncOptions, ticketingcine.SyncSummary] {
	return &SourceConnector[ticketingcine.Getter, ticketingcine.SyncOptions, ticketingcine.SyncSummary]{
		target: TargetTicketingcine, newClient: newClient, sync: ticketingcine.Sync, sources: sources,
		options: func(from string, now time.Time) ticketingcine.SyncOptions {
			return ticketingcine.SyncOptions{From: from, Now: now}
		},
		withSources: func(o ticketingcine.SyncOptions, sources []syncsource.Source) ticketingcine.SyncOptions {
			o.Sources = make([]ticketingcine.Source, len(sources))
			for i, source := range sources {
				o.Sources[i] = ticketingcine.Source{SiteID: source.Key}
			}
			return o
		},
		outcome: func(s ticketingcine.SyncSummary) SyncOutcome {
			return sourceOutcome(s.Cinemas, s.Movies, s.Requests, s.Showtimes, s.GeneratedAt)
		},
	}
}

// ScareConnector serves the SCARE open programme cinemas listed in
// sync_sources.
func ScareConnector(newClient func() (scare.Getter, error), sources SourceLister) *SourceConnector[scare.Getter, scare.SyncOptions, scare.SyncSummary] {
	return &SourceConnector[scare.Getter, scare.SyncOptions, scare.SyncSummary]{
		target: TargetScare, newClient: newClient, sync: scare.Sync, sources: sources,
		options: func(from string, now time.Time) scare.SyncOptions {
			return scare.SyncOptions{From: from, Now: now}
		},
		withSources: func(o scare.SyncOptions, sources []syncsource.Source) scare.SyncOptions {
			o.Sources = make([]scare.Source, len(sources))
			for i, source := range sources {
				o.Sources[i] = scare.Source{CinemaID: source.Key}
			}
			return o
		},
		outcome: func(s scare.SyncSummary) SyncOutcome {
			outcome := sourceOutcome(s.Cinemas, s.Movies, s.Requests, s.Showtimes, s.GeneratedAt)
			outcome.Skipped = s.Skipped
			return outcome
		},
	}
}

// WebediaConnector serves the Webedia Movies Pro cinemas listed in
// sync_sources: the source key is the Boxoffice theater id and the website
// the site origin.
func WebediaConnector(newClient func() (webedia.Getter, error), sources SourceLister) *SourceConnector[webedia.Getter, webedia.SyncOptions, webedia.SyncSummary] {
	return &SourceConnector[webedia.Getter, webedia.SyncOptions, webedia.SyncSummary]{
		target: TargetWebedia, newClient: newClient, sync: webedia.Sync, sources: sources,
		options: func(from string, now time.Time) webedia.SyncOptions {
			return webedia.SyncOptions{From: from, Now: now}
		},
		withSources: func(o webedia.SyncOptions, sources []syncsource.Source) webedia.SyncOptions {
			o.Sources = make([]webedia.Source, len(sources))
			for i, source := range sources {
				o.Sources[i] = webedia.Source{TheaterID: source.Key, Origin: source.Website}
			}
			return o
		},
		outcome: func(s webedia.SyncSummary) SyncOutcome {
			outcome := sourceOutcome(s.Cinemas, s.Movies, s.Requests, s.Showtimes, s.GeneratedAt)
			outcome.Skipped = s.Skipped
			return outcome
		},
	}
}

func reportVeo(e *ProductionExecutor, summary veo.SyncSummary, err error, lines *[]string) {
	if err == nil {
		for _, entry := range []struct {
			count    int
			category logCategory
		}{
			{summary.CheckoutTransientFailures, categoryTransport},
			{summary.CheckoutBudgetSkipped, categoryCanceled},
			{summary.CheckoutStartMismatches, categoryRedirect},
		} {
			if entry.count > 0 {
				line, ok := serializeLogRecord(logRecord{Timestamp: e.now().UTC(), Provider: TargetVeo, Event: eventCheckoutEnrichmentIncomplete, Stage: StageProviderFetch, Operation: operationShowings, Category: entry.category, Progress: logProgress{Skipped: &entry.count}})
				if ok {
					*lines = append(*lines, line)
				}
			}
		}
	}
	if args := veoCheckoutRetryLogArgs(summary); len(args) != 0 {
		e.logger.Info("veo_checkout_retry_summary", args...)
	}
}

func isKinepolisRequestError(err error) bool {
	var requestErr *kinepolis.RequestError
	return errors.As(err, &requestErr)
}

func classifyUGC(err error, details *logFailure) {
	var requestErr *ugc.RequestError
	if errors.As(err, &requestErr) {
		details.Operation = safeUGCOperation(requestErr.Operation)
		details.Category = safeUGCCategory(requestErr.Category)
		if details.Operation == operationShowings && details.Category == categoryInvalidPayload {
			details.ParseReason = safeUGCParseReason(requestErr.ParseReason)
		}
		details.HTTPStatus = requestErr.StatusCode
		details.Attempt = requestErr.Attempt
		details.AttemptLimit = requestErr.AttemptLimit
	}
}

func classifyKinepolis(err error, details *logFailure) {
	var requestErr *kinepolis.RequestError
	if errors.As(err, &requestErr) {
		details.Operation = safeKinepolisOperation(requestErr.Operation)
		details.Category = safeKinepolisCategory(requestErr.Category)
		details.HTTPStatus = requestErr.StatusCode
	}
}

func classifyPathe(err error, details *logFailure) {
	var requestErr *pathe.RequestError
	if errors.As(err, &requestErr) {
		details.Operation = safePatheRequestOperation(requestErr.Operation)
		details.Category = safePatheFetchCategory(requestErr.Category)
		details.HTTPStatus = requestErr.StatusCode
	}
}

func classifyCGR(err error, details *logFailure) {
	var requestErr *cgr.RequestError
	if errors.As(err, &requestErr) {
		details.Operation = safeCGROperation(requestErr.Operation)
		details.Category = safeCGRCategory(requestErr.Category)
		details.HTTPStatus = requestErr.StatusCode
	}
}

// classifyProxyRequest classifies the shared syncproxy.RequestError by its
// operation phase. defaultPhase applies when the error names no phase;
// payloadOnParse reports every other failure (parser rejections) as an
// invalid payload of that phase.
func classifyProxyRequest(defaultPhase syncproxy.Phase, payloadOnParse bool) func(error, *logFailure) {
	return func(err error, details *logFailure) {
		if payloadOnParse {
			details.Operation = phaseOperation(defaultPhase)
		}
		var requestErr *syncproxy.RequestError
		if errors.As(err, &requestErr) {
			phase := requestErr.Operation.Phase
			if phase == syncproxy.PhaseUnknown {
				phase = defaultPhase
			}
			if operation := phaseOperation(phase); operation != operationUnknown {
				details.Operation = operation
			}
			details.Category = safeMegaramaCategory(requestErr.Kind)
			details.HTTPStatus = requestErr.StatusCode
		} else if payloadOnParse {
			details.Category = categoryInvalidPayload
		}
	}
}

func phaseOperation(phase syncproxy.Phase) logOperation {
	switch phase {
	case syncproxy.PhaseCinemas:
		return operationCinemas
	case syncproxy.PhaseProgram:
		return operationProgram
	case syncproxy.PhaseMovies:
		return operationMovies
	case syncproxy.PhaseShowings:
		return operationShowings
	default:
		return operationUnknown
	}
}

func classifyVeo(err error, details *logFailure) {
	var requestErr *veo.RequestError
	var payloadErr *veo.PayloadError
	var op veo.Operation
	if errors.As(err, &requestErr) {
		op = requestErr.Operation
		details.Category = safeMegaramaCategory(requestErr.Kind)
		details.HTTPStatus = requestErr.StatusCode
	} else if errors.As(err, &payloadErr) {
		op, details.Category = payloadErr.Operation(), categoryInvalidPayload
	} else {
		var rejected *veo.CheckoutParseError
		if errors.As(err, &rejected) {
			details.Operation, details.Category = operationShowings, categoryInvalidPayload
		}
	}
	switch op {
	case veo.OperationCinemas:
		details.Operation = operationCinemas
	case veo.OperationProgram, veo.OperationCastelia:
		details.Operation = operationProgram
	case veo.OperationMovies:
		details.Operation = operationMovies
	case veo.OperationSchedule, veo.OperationCheckout:
		details.Operation = operationShowings
	}
}

// veoFetchLogArgs adds the whitelisted Veo checkout facts to a fetch failure.
func veoFetchLogArgs(err error, details logFailure) []any {
	var args []any
	if details.Category == categoryRedirect {
		var rejected *veo.RequestError
		if errors.As(err, &rejected) && rejected.Operation == veo.OperationCheckout && rejected.Kind == syncproxy.FailureRedirect {
			if facts, ok := rejected.RedirectFacts(); ok {
				args = append(args, safeVeoRedirectLogArgs(facts)...)
			}
		}
	}
	if details.Category == categoryInvalidPayload {
		var rejected *veo.CheckoutParseError
		if errors.As(err, &rejected) {
			// Revalidate at the logging boundary, including errors constructed by
			// callers. No provider strings or error/cause rendering is permitted.
			switch rejected.Reason {
			case veo.RejectRoom, veo.RejectBookingForm, veo.RejectClock, veo.RejectEndResolution:
				args = append(args, "veo_checkout_reason", string(rejected.Reason))
				if rejected.Reason == veo.RejectBookingForm {
					if subreason := safeVeoFormSubreason(rejected.FormSubreason()); subreason != "" {
						args = append(args, "veo_form_subreason", subreason)
					}
				}
				if schedule.ValidVeoIdentity("theater", rejected.CinemaID) && schedule.ValidVeoIdentity("showing", rejected.ShowingID) && strings.HasPrefix(rejected.ShowingID, rejected.CinemaID+"-") {
					args = append(args, "veo_cinema_id", rejected.CinemaID, "veo_showing_id", rejected.ShowingID)
					if rejected.Reason == veo.RejectBookingForm && rejected.FormSubreason() == "action_mismatch" {
						if names := safeVeoActionDifferences(rejected.ActionDifferences()); names != "" {
							args = append(args, "veo_action_difference_bits", rejected.ActionDifferences(), "veo_action_differences", names)
						}
						if facts, ok := rejected.ActionPathFacts(); ok {
							args = append(args, safeVeoActionPathLogArgs(facts)...)
						}
					}
					index, count := rejected.ClientPoolPosition()
					if validVeoClientPoolPosition(index, count) {
						args = append(args, "veo_client_pool_position", index, "veo_client_pool_size", count)
					}
				}
			}
		}
	}
	return args
}
