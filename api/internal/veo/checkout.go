package veo

import (
	"bytes"
	"context"
	"errors"
	"html"
	"io"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/charset"

	"messeances/api/internal/parallel"
	"messeances/api/internal/schedule"
	"messeances/api/internal/syncproxy"
)

var checkoutEnd = regexp.MustCompile(`(?i)fin\s+prévue\s+à\s+([0-9]{1,2})h([0-5][0-9])\b`)
var checkoutRoom = regexp.MustCompile(`"auditorium_showtime"\s*:\s*(null|"salle-([1-9][0-9]{0,19})"|"cine-fronton")(?:\s*[,}]|\s*$)`)
var checkoutRoomKey = regexp.MustCompile(`(?i)\bauditorium_showtime\b`)

type checkoutFacts struct {
	hour, minute int
	hasEnd       bool
	room         string
	// Parser-private attestation, never inferred from diagnostic shape or errors.
	genericEntry bool
}

func parseCheckout(body []byte, canonicalURL string) (checkoutFacts, error) {
	return parseCheckoutForTheater(body, canonicalURL, "")
}

func parseCheckoutForTheater(body []byte, canonicalURL, theaterID string) (checkoutFacts, error) {
	return parseCheckoutPage(body, canonicalURL, theaterID, false)
}

func parseCheckoutPage(body []byte, canonicalURL, theaterID string, requireForm bool) (checkoutFacts, error) {
	if theaterID == "veo-G06DB" {
		requireForm = true
		u, err := url.Parse(canonicalURL)
		if err != nil || !canonicalCheckoutURL(u) || u.Host != "achat.cinema-senechal.com" {
			return checkoutFacts{}, formRejection(formCanonicalInvalid)
		}
	}
	if strings.HasPrefix(canonicalURL, "https://achat.cinema-senechal.com/") {
		if theaterID != "veo-G06DB" {
			return checkoutFacts{}, formRejection(formCanonicalInvalid)
		}
		requireForm = true
	}
	if len(body) == 0 || len(body) > MaxHTMLBytes {
		return checkoutFacts{}, formRejection(formBody)
	}
	if !utf8.Valid(body) {
		r, err := charset.NewReader(bytes.NewReader(body), "")
		if err != nil {
			return checkoutFacts{}, formRejection(formBody)
		}
		body, err = io.ReadAll(io.LimitReader(r, MaxHTMLBytes+1))
		if err != nil || len(body) > MaxHTMLBytes {
			return checkoutFacts{}, formRejection(formBody)
		}
	}
	root, err := xhtml.Parse(bytes.NewReader(body))
	if err != nil {
		return checkoutFacts{}, formRejection(formHTML)
	}
	var blocks []string
	var text func(*xhtml.Node) string
	text = func(n *xhtml.Node) string {
		if n.Type == xhtml.TextNode {
			return n.Data
		}
		var b strings.Builder
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			b.WriteString(text(c))
			b.WriteByte(' ')
		}
		return b.String()
	}
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode {
			for _, a := range n.Attr {
				if a.Key == "id" && a.Val == "s_fin" || a.Key == "class" && contains(strings.Fields(a.Val), "s_fin") {
					blocks = append(blocks, strings.Join(strings.Fields(text(n)), " "))
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	facts := checkoutFacts{}
	for _, block := range blocks {
		matches := checkoutEnd.FindAllStringSubmatch(block, -1)
		if len(matches) != 1 {
			return checkoutFacts{}, checkoutRejection(RejectClock, errShape)
		}
		h, _ := strconv.Atoi(matches[0][1])
		m, _ := strconv.Atoi(matches[0][2])
		if h > 23 || facts.hasEnd && (h != facts.hour || m != facts.minute) {
			return checkoutFacts{}, checkoutRejection(RejectClock, errShape)
		}
		facts.hour = h
		facts.minute = m
		facts.hasEnd = true
	}
	// A recognizable booking-page identity is required even when the source does
	// not publish an end. Generic error/login/maintenance pages are not absence.
	decoded := strings.ReplaceAll(html.UnescapeString(string(body)), `\"`, `"`)
	matches := checkoutRoom.FindAllStringSubmatch(decoded, -1)
	keys := len(checkoutRoomKey.FindAllStringIndex(decoded, -1))
	checkForm := func() (checkoutFacts, error) {
		reason, difference, actionOnly, path := bookingFormAssessmentDetail(root, body, canonicalURL)
		if reason == "" {
			return facts, nil
		}
		rejected := formDiagnosticRejection(reason, difference)
		rejected.actionOnly = actionOnly
		rejected.path = path
		if actionOnly {
			facts.genericEntry = exactEntryAction(root, entryCheckoutAction(theaterID))
			// Only the orchestrator may inspect these discarded first-page clocks
			// to rule out end-resolution failures before spending a retry.
			return facts, rejected
		}
		return checkoutFacts{}, rejected
	}
	if keys == 0 {
		return checkForm()
	}
	if len(matches) == 0 || len(matches) != keys {
		return checkoutFacts{}, checkoutRejection(RejectRoom, errShape)
	}
	first := matches[0][1]
	for _, m := range matches {
		if m[1] != first {
			return checkoutFacts{}, checkoutRejection(RejectRoom, errShape)
		}
	}
	if first == `"cine-fronton"` {
		// This one observed literal is not a general string-room grammar, nor
		// an escaped/entity-decoded lookalike compatibility rule.
		if len(checkoutRoom.FindAllStringSubmatch(string(body), -1)) != keys || theaterID != "veo-P3863" || !strings.HasPrefix(canonicalURL, "https://achat.veocinemas.fr/fronton/reserver/") {
			return checkoutFacts{}, formRejection(formFronton)
		}
		if firstFacts, err := checkForm(); err != nil {
			return firstFacts, err
		}
		facts.room = "cine-fronton"
	}
	if matches[0][2] != "" {
		facts.room = "Salle " + matches[0][2]
	}
	if requireForm {
		return checkForm()
	}
	return facts, nil
}

// Called only after the parser attests every raw/DOM form check except action
// equality. Compare the decoded literal, not URL normalization or diagnostics.
func exactEntryAction(root *xhtml.Node, action string) bool {
	if action == "" {
		return false
	}
	if root.Type == xhtml.ElementNode && root.Data == "form" {
		for _, a := range root.Attr {
			if a.Key == "action" {
				return a.Val == action
			}
		}
	}
	for c := root.FirstChild; c != nil; c = c.NextSibling {
		if exactEntryAction(c, action) {
			return true
		}
	}
	return false
}

// A missing optional room marker is not itself booking-page identity. Require
// the observed POST form to target the exact trusted final response, including
// film and tail IDs, not merely the cinema/start shared by coincident sessions.
func bookingFormSubreason(root *xhtml.Node, body []byte, canonical string) formSubreason {
	reason, _ := bookingFormDiagnostic(root, body, canonical)
	return reason
}

func bookingFormDiagnostic(root *xhtml.Node, body []byte, canonical string) (formSubreason, actionDifference) {
	reason, difference, _ := bookingFormAssessment(root, body, canonical)
	return reason, difference
}

func bookingFormAssessment(root *xhtml.Node, body []byte, canonical string) (formSubreason, actionDifference, bool) {
	reason, difference, actionOnly, _ := bookingFormAssessmentDetail(root, body, canonical)
	return reason, difference, actionOnly
}

func bookingFormAssessmentDetail(root *xhtml.Node, body []byte, canonical string) (formSubreason, actionDifference, bool, *CheckoutActionPathFacts) {
	if canonical == "" {
		return formCanonicalMissing, 0, false, nil
	}
	u, err := url.Parse(canonical)
	if err != nil || !canonicalCheckoutURL(u) || u.String() != canonical {
		return formCanonicalInvalid, 0, false, nil
	}
	// The HTML tree builder discards nested form start tags. They must not hide
	// conflicting identities; tokenize too (script/text lookalikes are inert).
	tokens := xhtml.NewTokenizer(bytes.NewReader(body))
	rawForms := 0
	rawAttributesValid := true
	for {
		kind := tokens.Next()
		if kind == xhtml.ErrorToken {
			if !errors.Is(tokens.Err(), io.EOF) {
				return formHTML, 0, false, nil
			}
			break
		}
		if kind == xhtml.StartTagToken || kind == xhtml.SelfClosingTagToken {
			singleAttributes := singleRawFormAttributes(tokens.Raw())
			token := tokens.Token()
			if token.Data == "form" {
				rawForms++
				actions, methods := 0, 0
				for _, a := range token.Attr {
					if a.Key == "action" {
						actions++
					}
					if a.Key == "method" {
						methods++
						rawAttributesValid = rawAttributesValid && strings.EqualFold(a.Val, "post")
					}
				}
				rawAttributesValid = rawAttributesValid && singleAttributes && actions == 1 && methods == 1
			}
		}
	}
	if rawForms != 1 {
		return formRawCount, 0, false, nil
	}
	forms := 0
	var reason formSubreason
	var difference actionDifference
	var path *CheckoutActionPathFacts
	otherFailure := false
	reject := func(r formSubreason) {
		if r != formActionMismatch {
			otherFailure = true
		}
		if reason == "" {
			reason = r
		}
	}
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode {
			if n.Data == "form" {
				forms++
				for parent := n.Parent; parent != nil; parent = parent.Parent {
					if parent.Type == xhtml.ElementNode && parent.Data == "template" {
						reject(formInert)
					}
				}
				actions, methods := 0, 0
				actionMatches, methodMatches := true, true
				var actionDiff actionDifference
				var actionPath *CheckoutActionPathFacts
				for _, a := range n.Attr {
					switch a.Key {
					case "action":
						actions++
						actionMatches = actionMatches && a.Val == canonical
						if a.Val != canonical {
							actionDiff |= compareAction(a.Val, canonical)
							facts := classifyDiagnosticActionPath(a.Val, canonical)
							actionPath = &facts
						}
					case "method":
						methods++
						methodMatches = methodMatches && strings.EqualFold(a.Val, "post")
					}
				}
				if actions != 1 {
					reject(formActionCount)
				} else if !actionMatches {
					reject(formActionMismatch)
					if reason == formActionMismatch {
						difference = actionDiff
						path = actionPath
					}
				}
				if methods != 1 {
					reject(formMethodCount)
				} else if !methodMatches {
					reject(formMethodNotPOST)
				}
			}
			if n.Data == "input" || n.Data == "textarea" || n.Data == "select" || n.Data == "button" {
				for _, a := range n.Attr {
					value := strings.ToLower(a.Val)
					if a.Key == "type" && value == "password" ||
						(a.Key == "name" || a.Key == "id") && (strings.Contains(value, "password") || strings.Contains(value, "passwd") || value == "login" || value == "username") ||
						a.Key == "autocomplete" && (contains(strings.Fields(value), "current-password") || contains(strings.Fields(value), "new-password") || contains(strings.Fields(value), "username")) {
						reject(formLogin)
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	if forms != 1 {
		return formDOMCount, 0, false, nil
	}
	if reason != formActionMismatch {
		difference = 0
		path = nil
	}
	return reason, difference, reason == formActionMismatch && !otherFailure && rawAttributesValid, path
}

// The HTML tokenizer drops duplicate attributes before exposing Token.Attr.
// Count raw attribute names for retry attestation only, without changing the
// existing first-page acceptance/diagnostic precedence or retaining values.
func singleRawFormAttributes(raw []byte) bool {
	space := func(c byte) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '\f' }
	i := 1 // '<'
	for i < len(raw) && !space(raw[i]) && raw[i] != '>' && raw[i] != '/' {
		i++
	}
	actions, methods := 0, 0
	for i < len(raw) {
		for i < len(raw) && (space(raw[i]) || raw[i] == '/') {
			i++
		}
		if i == len(raw) || raw[i] == '>' {
			break
		}
		start := i
		for i < len(raw) && !space(raw[i]) && raw[i] != '=' && raw[i] != '>' {
			i++
		}
		switch strings.ToLower(string(raw[start:i])) {
		case "action":
			actions++
		case "method":
			methods++
		}
		for i < len(raw) && space(raw[i]) {
			i++
		}
		if i < len(raw) && raw[i] == '=' {
			i++
			for i < len(raw) && space(raw[i]) {
				i++
			}
			if i < len(raw) && (raw[i] == '\'' || raw[i] == '"') {
				quote := raw[i]
				i++
				for i < len(raw) && raw[i] != quote {
					i++
				}
				if i < len(raw) {
					i++
				}
			} else {
				for i < len(raw) && !space(raw[i]) && raw[i] != '>' {
					i++
				}
			}
		}
	}
	return actions == 1 && methods == 1
}

type actionDifference uint32

const (
	actionScheme actionDifference = 1 << iota
	actionAuthority
	actionPathShape
	actionSlug
	actionFilm
	actionEpoch
	actionVersion
	actionTail
	actionTrailingSlash
	actionQuery
	actionFragment
	actionRelative
	actionWhitespace
	actionEscaping
	actionUserinfo
	actionUnparseable
	actionRepresentation
)

// Compare only for diagnostics AFTER the unchanged exact-byte rejection. Never
// retain url.Parse errors: their text may include the untrusted input value.
func compareAction(actual, canonical string) actionDifference {
	var diff actionDifference
	if strings.ContainsFunc(actual, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
		diff |= actionWhitespace
	}
	if strings.ContainsAny(actual, "%\\") {
		diff |= actionEscaping
	}
	a, err := url.Parse(actual)
	c, canonicalErr := url.Parse(canonical)
	if err != nil || canonicalErr != nil {
		return diff | actionUnparseable
	}
	if !a.IsAbs() {
		diff |= actionRelative
	}
	if a.Scheme != c.Scheme {
		diff |= actionScheme
	}
	if a.Host != c.Host {
		diff |= actionAuthority
	}
	if a.User != nil {
		diff |= actionUserinfo
	}
	if a.RawQuery != c.RawQuery || a.ForceQuery != c.ForceQuery {
		diff |= actionQuery
	}
	if a.Fragment != c.Fragment || strings.Contains(actual, "#") != strings.Contains(canonical, "#") {
		diff |= actionFragment
	}
	if strings.HasSuffix(a.Path, "/") != strings.HasSuffix(c.Path, "/") {
		diff |= actionTrailingSlash
	}
	// Separate positional decomposition: never change the transport regex or its
	// slug/D/version capture indexes to obtain F and tail diagnostics.
	ap, cp := strings.Split(strings.TrimSuffix(a.Path, "/"), "/"), strings.Split(strings.TrimSuffix(c.Path, "/"), "/")
	if c.Host == "achat.cinema-senechal.com" && gueretDestination.MatchString(c.Path) {
		if !gueretDestination.MatchString(strings.TrimSuffix(a.Path, "/") + "/") {
			diff |= actionPathShape
		}
		if len(ap) == 6 && len(cp) == 6 && ap[1] == "reserver" {
			for i, flag := range map[int]actionDifference{2: actionFilm, 3: actionEpoch, 4: actionVersion, 5: actionTail} {
				if ap[i] != cp[i] {
					diff |= flag
				}
			}
		} else {
			diff |= actionPathShape
		}
		if diff == 0 && actual != canonical {
			diff = actionRepresentation
		}
		return diff
	}
	if !checkoutDestination.MatchString(strings.TrimSuffix(a.Path, "/") + "/") {
		diff |= actionPathShape
	}
	if len(ap) == 7 && len(cp) == 7 && ap[2] == "reserver" {
		for i, flag := range map[int]actionDifference{1: actionSlug, 3: actionFilm, 4: actionEpoch, 5: actionVersion, 6: actionTail} {
			if ap[i] != cp[i] {
				diff |= flag
			}
		}
	} else {
		diff |= actionPathShape
	}
	if diff == 0 && actual != canonical {
		diff = actionRepresentation
	}
	return diff
}

// Resolve a published local end clock against calendar dates, not 24-hour
// arithmetic. Ambiguous repeated clocks fail closed rather than guess an offset.
func exactEnd(start time.Time, f checkoutFacts) (time.Time, error) {
	if !f.hasEnd {
		return start, nil
	}
	loc := start.Location()
	for day := 0; day < 2; day++ {
		date := start.AddDate(0, 0, day)
		wall := time.Date(date.Year(), date.Month(), date.Day(), f.hour, f.minute, 0, 0, time.UTC).Format("2006-01-02T15:04")
		candidate, err := localTime("2006-01-02T15:04", wall, loc)
		if err != nil {
			return time.Time{}, checkoutRejection(RejectEndResolution, err)
		}
		if candidate.After(start) {
			return candidate, nil
		}
	}
	return time.Time{}, checkoutRejection(RejectEndResolution, errShape)
}

const maxCheckoutRetries = 8

type checkoutRetryBudget struct {
	mu         sync.Mutex
	seen       map[string]bool
	attempts   int
	recoveries int
	first      *CheckoutParseError
	firstOK    bool
}

func (b *checkoutRetryBudget) reserve(ctx context.Context, rejected *CheckoutParseError, row schedule.ShowtimeRecord) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if ctx.Err() != nil || !rejected.actionOnly || rejected.Reason != RejectBookingForm || rejected.formReason != formActionMismatch ||
		!schedule.ValidVeoBookingURL(row.BookingURL, rejected.CinemaID, rejected.ShowingID) || row.StartTime.IsZero() {
		return false
	}
	key := rejected.CinemaID + ":" + rejected.ShowingID
	if b.attempts >= maxCheckoutRetries || b.seen[key] {
		return false
	}
	if b.seen == nil {
		b.seen = make(map[string]bool)
	}
	b.seen[key] = true
	b.attempts++
	if b.first == nil {
		copy := *rejected
		b.first = &copy
	}
	return true
}

func (b *checkoutRetryBudget) recovered(rejected *CheckoutParseError) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.recoveries++
	if b.first.CinemaID == rejected.CinemaID && b.first.ShowingID == rejected.ShowingID {
		b.firstOK = true
	}
}

type resolvedCheckout struct {
	checkoutFacts
	end              time.Time
	entryUnavailable bool
}

func entryCheckoutAction(theaterID string) string {
	switch theaterID {
	case "veo-G0699":
		return "https://achat.veocinemas.fr/toulouse/reserver/"
	case "veo-P0833":
		return "https://achat.veocinemas.fr/saint-chamond/reserver/"
	case "veo-W0065":
		return "https://achat.veocinemas.fr/caussade/reserver/"
	case "veo-P6864":
		return "https://achat.veocinemas.fr/colomiers/reserver/"
	case "veo-G06DB":
		return "https://achat.cinema-senechal.com/reserver/"
	default:
		return ""
	}
}

func boundEntryCheckout(row schedule.ShowtimeRecord, canonical string) bool {
	action := entryCheckoutAction(row.TheaterID)
	cinemaID := strings.TrimPrefix(row.TheaterID, "veo-")
	if action == "" || !schedule.ValidVeoIdentity("showing", row.ProviderShowingID) ||
		!strings.HasPrefix(row.ProviderShowingID, cinemaID+"-") || row.ID != "veo-showing-"+row.ProviderShowingID ||
		!schedule.ValidVeoBookingURL(row.BookingURL, cinemaID, row.ProviderShowingID) {
		return false
	}
	if row.TheaterID == "veo-G06DB" && !schedule.ValidVeoGueretRelayURL(row.BookingURL) {
		return false
	}
	u, err := url.Parse(canonical)
	return err == nil && canonicalCheckoutURL(u) && u.String() == canonical &&
		strings.HasPrefix(canonical, action) &&
		boundCheckoutURL(u, checkoutBinding{source: row.BookingURL, start: row.StartTime})
}

func resolveCheckout(row schedule.ShowtimeRecord, response CheckoutResult, requireForm bool) (resolvedCheckout, error) {
	f := checkoutFacts{}
	var err error
	if !response.unavailableEnrichment {
		// Observed cinema-bound entry forms are unavailable enrichment, not
		// session identity. Inspect their form even when a room marker is present.
		f, err = parseCheckoutPage(response.Body, response.canonicalURL, row.TheaterID, requireForm || entryCheckoutAction(row.TheaterID) != "")
		if err != nil {
			var rejected *CheckoutParseError
			if errors.As(err, &rejected) && rejected.actionOnly {
				// Eligibility includes the existing end-resolution rules, not just
				// the first form subreason. Never use these first-page facts.
				if _, endErr := exactEnd(row.StartTime, f); endErr != nil {
					rejected.actionOnly = false
				} else if !requireForm && f.genericEntry && boundEntryCheckout(row, response.canonicalURL) {
					// First normal response only. Discard ALL page facts, even
					// plausible clocks/rooms. Never follow the general-entry action.
					return resolvedCheckout{end: row.StartTime, entryUnavailable: true}, nil
				}
			}
			return resolvedCheckout{}, identifyCheckoutError(err, row, response)
		}
	}
	end, err := exactEnd(row.StartTime, f)
	if err != nil {
		return resolvedCheckout{}, identifyCheckoutError(err, row, response)
	}
	return resolvedCheckout{checkoutFacts: f, end: end}, nil
}

func enrichCheckouts(parent context.Context, g Getter, rows []schedule.ShowtimeRecord, summary *SyncSummary, now time.Time) error {
	return enrichCheckoutsWithinBudget(parent, g, rows, summary, now, 5*time.Minute)
}

func enrichCheckoutsWithinBudget(parent context.Context, g Getter, rows []schedule.ShowtimeRecord, summary *SyncSummary, now time.Time, limit time.Duration) error {
	if now.IsZero() {
		return errShape
	}
	indices := []int{}
	for i, r := range rows {
		if r.TheaterID != "veo-EMS1361" {
			// Started sessions remain part of their advertised date. Their portal
			// may no longer be bookable: retain API room and an unknown canonical
			// end, without confusing an error page with missing source metadata.
			if !r.StartTime.After(now) {
				rows[i].EndTime = r.StartTime
				continue
			}
			indices = append(indices, i)
		}
	}
	if len(indices) == 0 {
		return parent.Err()
	}
	ctx, cancel := context.WithTimeout(parent, limit)
	defer cancel()
	budget := checkoutRetryBudget{}
	var unavailable atomic.Int64
	var gueretUnavailable atomic.Int64
	var regularFinal404Unavailable atomic.Int64
	var startMismatches atomic.Int64
	defer func() {
		summary.CheckoutRetryAttempts, summary.CheckoutRecoveries = budget.attempts, budget.recoveries
		summary.FirstCheckoutRetry, summary.FirstCheckoutRetryRecovered = budget.first, budget.firstOK
		summary.ToulouseUnavailableCheckouts = int(unavailable.Load())
		summary.GueretUnavailableCheckouts = int(gueretUnavailable.Load())
		summary.RegularFinal404UnavailableCheckouts = int(regularFinal404Unavailable.Load())
		summary.CheckoutStartMismatches = int(startMismatches.Load())
	}()
	type outcome struct {
		facts                          resolvedCheckout
		attempted, resolved, transient bool
	}
	// Each worker owns one index. Preserve completed facts on budget expiry;
	// MapOrdered joins all workers before the results are read or applied.
	results := make([]outcome, len(rows))
	fetch := func(ctx context.Context, index int) (resolvedCheckout, error) {
		if err := ctx.Err(); err != nil {
			return resolvedCheckout{}, err
		}
		results[index].attempted = true
		b, err := g.Checkout(ctx, rows[index].BookingURL, rows[index].StartTime)
		if err != nil {
			if checkoutStartMismatch(err, rows[index]) {
				startMismatches.Add(1)
				return resolvedCheckout{end: rows[index].StartTime}, nil
			}
			if transientCheckoutFailure(err) {
				results[index].transient = true
				return resolvedCheckout{end: rows[index].StartTime}, nil
			}
			if ctx.Err() != nil {
				var canceled *RequestError
				if err == ctx.Err() || errors.As(err, &canceled) && canceled.Operation == OperationCheckout && canceled.Kind == syncproxy.FailureCanceled && errors.Is(err, ctx.Err()) { //nolint:errorlint // Exact context sentinels are optional; arbitrary wrappers can carry unsafe provider failures.
					return resolvedCheckout{}, ctx.Err()
				}
			}
			return resolvedCheckout{}, identifyCheckoutRequestError(err, rows[index])
		}
		if err := ctx.Err(); err != nil {
			return resolvedCheckout{}, err
		}
		resolved, err := resolveCheckout(rows[index], b, false)
		if err == nil {
			if b.unavailableEnrichment && b.regularFinal404Unavailable {
				regularFinal404Unavailable.Add(1)
			}
			if b.unavailableEnrichment && rows[index].TheaterID == "veo-G06DB" && schedule.ValidVeoGueretRelayURL(rows[index].BookingURL) {
				gueretUnavailable.Add(1)
			}
			if resolved.entryUnavailable && rows[index].TheaterID == "veo-G0699" {
				unavailable.Add(1)
			}
			return resolved, nil
		}
		var rejected *CheckoutParseError
		if !errors.As(err, &rejected) || !budget.reserve(ctx, rejected, rows[index]) {
			return resolvedCheckout{}, err
		}
		// Only an actual parser-result attestation reaches here, never a typed
		// error supplied by a getter. Re-fetch the ORIGINAL source/start once.
		b, retryErr := g.Checkout(ctx, rows[index].BookingURL, rows[index].StartTime)
		if retryErr != nil {
			// A failed retry cannot validate an already rejected page, even when
			// its failure is transient or the enrichment budget has expired.
			if transientCheckoutFailure(retryErr) || ctx.Err() != nil {
				return resolvedCheckout{}, err
			}
			return resolvedCheckout{}, identifyCheckoutRequestError(retryErr, rows[index])
		}
		if ctx.Err() != nil {
			return resolvedCheckout{}, err
		}
		if b.unavailableEnrichment {
			// This outcome has no validated page and cannot repair a mismatch.
			return resolvedCheckout{}, err
		}
		resolved, retryErr = resolveCheckout(rows[index], b, true)
		if retryErr != nil {
			return resolvedCheckout{}, retryErr
		}
		budget.recovered(rejected)
		return resolved, nil
	}
	_, err := parallel.MapOrdered(ctx, indices, parallel.Options{Workers: min(WorkerCount, len(indices))}, func(ctx context.Context, index int) (struct{}, error) {
		facts, err := fetch(ctx, index)
		if err == nil {
			results[index].facts = facts
			results[index].resolved = !results[index].transient
		}
		return struct{}{}, err
	})
	if parent.Err() != nil {
		return parent.Err()
	}
	if err != nil && (err != context.DeadlineExceeded || !errors.Is(ctx.Err(), context.DeadlineExceeded)) { //nolint:errorlint // Only the worker's exact deadline is optional; wrapped parse/request errors remain fatal.
		return err
	}
	for _, index := range indices {
		result := results[index]
		r := &rows[index]
		r.EndTime = r.StartTime
		if result.attempted {
			summary.Checkouts++
		}
		if result.transient {
			summary.CheckoutTransientFailures++
			continue
		}
		if !result.resolved {
			summary.CheckoutBudgetSkipped++
			continue
		}
		f := result.facts
		r.EndTime = f.end
		if f.hasEnd {
			summary.ExactEnds++
		}
		if r.Room == "" {
			r.Room = f.room
		}
	}
	return nil
}

func transientCheckoutFailure(err error) bool {
	var requestErr *RequestError
	if !errors.As(err, &requestErr) || requestErr.Operation != OperationCheckout {
		return false
	}
	switch requestErr.Kind {
	case syncproxy.FailureTransport, syncproxy.FailureResponseRead:
		return true
	case syncproxy.FailureServer:
		return requestErr.StatusCode >= 500 && requestErr.StatusCode <= 599
	default:
		return false
	}
}
