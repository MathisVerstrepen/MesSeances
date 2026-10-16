package veo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"html"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	xhtml "golang.org/x/net/html"

	"messeances/api/internal/schedule"
)

func TestCheckoutActualPathDiagnosticsSurviveRetryIsolation(t *testing.T) {
	for i := range 16 {
		t.Run(string(rune('a'+i)), func(t *testing.T) {
			t.Parallel()
			row := retryTestRow(i)
			g := &retryCheckoutGetter{fetch: func(_ context.Context, _ string, start time.Time, n int) (CheckoutResult, error) {
				page := retryTestPage(start, false)
				// The exact G0699 entry is now a source gap; a bare query is not.
				action := "https://achat.veocinemas.fr/toulouse/reserver/?"
				if n == 2 {
					action = page.canonicalURL + "opaque-secret/"
				}
				page.Body = []byte(bookingFormFixture(action))
				page.pool = clientPoolPosition{n + 1, 5}
				return page, nil
			}}
			rows, summary := []schedule.ShowtimeRecord{row}, SyncSummary{}
			err := enrichCheckouts(t.Context(), g, rows, &summary, row.StartTime.Add(-time.Hour))
			var final *CheckoutParseError
			if !errors.As(err, &final) || !errors.Is(err, errShape) || g.RequestCount() != 2 || summary.CheckoutRetryAttempts != 1 || summary.CheckoutRecoveries != 0 || final.ShowingID != row.ProviderShowingID {
				t.Fatal("actual final rejection or retry semantics changed")
			}
			first, firstOK := summary.FirstCheckoutRetry.ActionPathFacts()
			last, lastOK := final.ActionPathFacts()
			if !firstOK || !lastOK || first.Shape != "generic_entry" || first.SegmentCount != 2 || first.ExpectedPresentMask != 3 || last.Shape != "canonical_prefix_extra" || last.SegmentCount != 7 || last.ExpectedPresentMask != 63 || last.ExpectedPositionMask != 63 || summary.FirstCheckoutRetry.pool.index != 2 || final.pool.index != 3 {
				t.Fatal("first/final body and canonical metadata crossed requests")
			}
			last.Classes[0] = "forged-secret"
			copy, _ := final.ActionPathFacts()
			if copy.Classes[0] != "expected_slug" || !rows[0].EndTime.Equal(row.EndTime) {
				t.Fatal("mutable accessor or partial row publication")
			}
			encoded, marshalErr := json.Marshal(copy)
			if marshalErr != nil || strings.Contains(string(encoded), "opaque-secret") || strings.Contains(final.Error(), "opaque-secret") {
				t.Fatal("raw rejected path retained")
			}
		})
	}
}

func TestCheckoutPathFactsBoundsAndUnknownEquality(t *testing.T) {
	const canonical = "https://achat.veocinemas.fr/toulouse/reserver/F123/D1792781400/VF/456/"
	d := classifyDiagnosticActionPath(canonical+strings.Repeat("opaque-secret/", 2000), canonical)
	if !d.CountsTruncated || !d.ClassesTruncated || d.SegmentCount != 1024 || d.ExtraCount != 1024 || len(d.Classes) != 32 || d.Shape != "canonical_prefix_extra" || d.ExpectedPresentMask != 63 {
		t.Fatal("unbounded path instrumentation")
	}
	for _, action := range []string{"https://secret@invalid/%XX", "https://achat.veocinemas.fr/"} {
		unknown := classifyDiagnosticActionPath(action, canonical)
		if unknown.CanonicalPrefix || unknown.ExpectedTokensInOrder || unknown.ExpectedPresentMask != 0 || unknown.ExpectedPositionMask != 0 {
			t.Fatal("undecodable or empty path asserted identity equality")
		}
	}
	_, err := parseCheckout([]byte(`<template>`+bookingFormFixture(canonical+"opaque-secret/")+`</template>`), canonical)
	var rejected *CheckoutParseError
	if !errors.As(err, &rejected) || rejected.FormSubreason() != "template_inert" {
		t.Fatal("first subreason changed")
	}
	if _, ok := rejected.ActionPathFacts(); ok {
		t.Fatal("path facts attached outside action mismatch")
	}
}

type diagnosticCheckoutPage struct {
	HTMLParsed                                                         bool
	RawForms, DOMForms, InertForms, ActionAttributes, MethodAttributes int
	POSTMethods, LoginControls, EndBlocks, EndClockMatches             int
	RoomKeys, ValidRoomMarkers, OmittedActionClassifications           int
	Actions                                                            []diagnosticPathStructure
}

func classifyDiagnosticCheckoutPage(body []byte, canonical string) diagnosticCheckoutPage {
	d := diagnosticCheckoutPage{}
	tokens := xhtml.NewTokenizer(bytes.NewReader(body))
	for {
		kind := tokens.Next()
		if kind == xhtml.ErrorToken {
			break
		}
		if kind == xhtml.StartTagToken || kind == xhtml.SelfClosingTagToken {
			if tokens.Token().Data == "form" {
				d.RawForms++
			}
		}
	}
	root, err := xhtml.Parse(bytes.NewReader(body))
	if err != nil {
		return d
	}
	d.HTMLParsed = true
	decoded := strings.ReplaceAll(html.UnescapeString(string(body)), `\"`, `"`)
	d.RoomKeys = len(checkoutRoomKey.FindAllStringIndex(decoded, -1))
	d.ValidRoomMarkers = len(checkoutRoom.FindAllStringIndex(decoded, -1))
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
			login := false
			if n.Data == "form" {
				d.DOMForms++
				for p := n.Parent; p != nil; p = p.Parent {
					if p.Type == xhtml.ElementNode && p.Data == "template" {
						d.InertForms++
						break
					}
				}
			}
			for _, a := range n.Attr {
				if n.Data == "form" {
					switch a.Key {
					case "action":
						d.ActionAttributes++
						if len(d.Actions) < 4 {
							d.Actions = append(d.Actions, classifyDiagnosticActionPath(a.Val, canonical))
						} else {
							d.OmittedActionClassifications++
						}
					case "method":
						d.MethodAttributes++
						if strings.EqualFold(a.Val, "post") {
							d.POSTMethods++
						}
					}
				}
				if a.Key == "id" && a.Val == "s_fin" || a.Key == "class" && contains(strings.Fields(a.Val), "s_fin") {
					d.EndBlocks++
					d.EndClockMatches += len(checkoutEnd.FindAllStringIndex(strings.Join(strings.Fields(text(n)), " "), -1))
				}
				if n.Data == "input" || n.Data == "textarea" || n.Data == "select" || n.Data == "button" {
					v := strings.ToLower(a.Val)
					login = login || a.Key == "type" && v == "password" || (a.Key == "name" || a.Key == "id") && (strings.Contains(v, "password") || strings.Contains(v, "passwd") || v == "login" || v == "username") || a.Key == "autocomplete" && (contains(strings.Fields(v), "current-password") || contains(strings.Fields(v), "new-password") || contains(strings.Fields(v), "username"))
				}
			}
			if login {
				d.LoginControls++
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return d
}

func TestDiagnosticActionPathStructures(t *testing.T) {
	const canonical = "https://achat.veocinemas.fr/toulouse/reserver/F123/D1792781400/VF/456/"
	for _, tc := range []struct {
		name, action, shape             string
		segments, extra, missing, empty int
	}{
		{"canonical", canonical, "canonical_tokens", 6, 0, 0, 0},
		{"extra", canonical + "opaque-secret/", "canonical_prefix_extra", 7, 1, 0, 0},
		{"missing", strings.Replace(canonical, "/F123", "", 1), "missing_segments", 5, 0, 1, 0},
		{"reordered", strings.Replace(canonical, "F123/D1792781400", "D1792781400/F123", 1), "reordered_tokens", 6, 0, 0, 0},
		{"empty", strings.Replace(canonical, "/reserver/", "/reserver//", 1), "empty_segments", 7, 1, 0, 1},
		{"entry", "https://achat.veocinemas.fr/toulouse/reserver/", "generic_entry", 2, 0, 4, 0},
		{"source", "https://achat.veocinemas.fr/toulouse/r/8177", "source_alias", 3, 0, 3, 0},
		{"source_alternate", "https://achat.veocinemas.fr/toulouse/reserver/r/8177", "source_alias", 4, 0, 2, 0},
		{"malformed", "https://achat.veocinemas.fr/opaque-secret/", "missing_segments", 1, 0, 5, 0},
		{"different", strings.Replace(canonical, "F123", "F999", 1), "different_shape", 6, 0, 0, 0},
		{"unparseable", "https://credential-secret@achat.veocinemas.fr/%XX", "unparseable", 0, 0, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := classifyDiagnosticActionPath(tc.action, canonical)
			if d.Shape != tc.shape || d.SegmentCount != tc.segments || d.ExtraCount != tc.extra || d.MissingCount != tc.missing || d.EmptySegments != tc.empty {
				t.Fatalf("wrong closed path classification: %+v", d)
			}
			encoded, err := json.Marshal(d)
			if err != nil || strings.Contains(string(encoded), "opaque-secret") || strings.Contains(string(encoded), "credential-secret") || strings.Contains(string(encoded), "F123") || strings.Contains(string(encoded), "1792781400") || strings.Contains(string(encoded), "8177") {
				t.Fatal("raw action material escaped classifier")
			}
		})
	}
	good := classifyDiagnosticActionPath(canonical, canonical)
	if !good.Exact || !good.CanonicalPrefix || !good.ExpectedTokensInOrder || good.ExpectedAtPosition != [6]bool{true, true, true, true, true, true} || good.ExpectedPositions != [6]int{0, 1, 2, 3, 4, 5} {
		t.Fatal("trusted token equality incorrect")
	}
	badCanonical := classifyDiagnosticActionPath(canonical, "https://secret@invalid/%XX")
	if badCanonical.CanonicalValid || badCanonical.Shape != "canonical_unavailable" {
		t.Fatal("invalid canonical identity trusted")
	}
}

func TestDiagnosticCheckoutPageStructure(t *testing.T) {
	const canonical = "https://achat.veocinemas.fr/toulouse/reserver/F123/D1792781400/VF/456/"
	form := `<form method="POST" action="` + canonical + `"></form>`
	for _, tc := range []struct {
		name, body                       string
		forms, inert, login, ends, rooms int
	}{
		{"valid", form + `<div id="s_fin">fin prévue à 22h15</div><script>var x={"auditorium_showtime":null};</script>`, 1, 0, 0, 1, 1},
		{"script_inert", `<script>` + form + `</script>`, 0, 0, 0, 0, 0},
		{"template_inert", `<template>` + form + `</template>`, 1, 1, 0, 0, 0},
		{"login", form + `<input type="password" name="secret-password">`, 1, 0, 1, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := classifyDiagnosticCheckoutPage([]byte(tc.body), canonical)
			if !d.HTMLParsed || d.RawForms != tc.forms || d.DOMForms != tc.forms || d.InertForms != tc.inert || d.LoginControls != tc.login || d.EndBlocks != tc.ends || d.EndClockMatches != tc.ends || d.RoomKeys != tc.rooms || d.ValidRoomMarkers != tc.rooms || d.ActionAttributes != tc.forms || d.MethodAttributes != tc.forms || d.POSTMethods != tc.forms {
				t.Fatalf("wrong closed page counts: %+v", d)
			}
		})
	}
}

func TestDiagnosticTwoGETBeforeSendCap(t *testing.T) {
	ledger := &diagnosticLedger{maxTotal: 2, maxCheckout: 2}
	var sent atomic.Int64
	d := diagnosticTransport{base: roundTripFunc(func(*http.Request) (*http.Response, error) {
		sent.Add(1)
		return nil, errors.New("synthetic failure")
	}), ledger: ledger}
	var group sync.WaitGroup
	for range 8 {
		group.Go(func() {
			r, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://achat.veocinemas.fr/toulouse/r/8177", nil)
			if err != nil {
				t.Error("synthetic request invalid")
				return
			}
			response, _ := d.RoundTrip(r)
			if response != nil {
				_, _ = io.Copy(io.Discard, response.Body)
				_ = response.Body.Close()
			}
		})
	}
	group.Wait()
	if sent.Load() != 2 || ledger.total != 2 || ledger.checkout != 2 || ledger.acquisition != 0 || !ledger.blocked {
		t.Fatal("two GET cap failed before send")
	}
}
