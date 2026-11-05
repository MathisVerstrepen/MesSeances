package veo

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"messeances/api/internal/schedule"
)

func saintAffriqueSourceBinding(raw string, start time.Time, tags []string) bool {
	if !schedule.ValidVeoSaintAffriqueRelayURL(raw) {
		return false
	}
	if !relaySourceTimeBinding(raw, start, tags) {
		return false
	}
	if tags != nil {
		_, version, err := normalizeVersion(tags)
		code := "VF"
		if version == "VOSTF" {
			code = "VO"
		}
		original, _ := url.Parse(raw)
		return err == nil && original.Query().Get("code") == code
	}
	return true
}

func saintAffriqueRoot(u *url.URL) bool {
	if !safeURL(u) || len(u.String()) > 4096 {
		return false
	}
	// The observed root relay adds exactly one literal VF/VO code to the
	// existing four-key root grammar. No new host or destination is authorized.
	copy := *u
	pairs := strings.Split(u.RawQuery, "&")
	kept := make([]string, 0, len(pairs))
	codes := 0
	for _, pair := range pairs {
		if pair == "code=VF" || pair == "code=VO" {
			codes++
		} else {
			kept = append(kept, pair)
		}
	}
	copy.RawQuery = strings.Join(kept, "&")
	return codes == 1 && gueretRoot(&copy)
}

func boundSaintAffriqueURL(u *url.URL, b checkoutBinding) bool {
	if u == nil || !saintAffriqueSourceBinding(b.source, b.start, nil) {
		return false
	}
	if u.String() == b.source {
		return true
	}
	if !saintAffriqueRoot(u) {
		return false
	}
	source, _ := url.Parse(b.source)
	for _, key := range []string{"perfd", "perft", "code"} {
		if u.Query().Get(key) != source.Query().Get(key) {
			return false
		}
	}
	return true
}

func saintAffriqueRedirect(req *http.Request, via []*http.Request, b checkoutBinding) bool {
	return req != nil && req.Method == http.MethodGet && len(via) == 1 &&
		via[0] != nil && via[0].Method == http.MethodGet && via[0].URL != nil &&
		via[0].URL.String() == b.source && saintAffriqueRoot(req.URL) && boundSaintAffriqueURL(req.URL, b)
}

func saintAffriqueUnavailable(u *url.URL, r *http.Response, b checkoutBinding) bool {
	if b.response == nil || !saintAffriqueRoot(u) || !boundSaintAffriqueURL(u, b) ||
		r.StatusCode != http.StatusFound || len(r.Header.Values("Location")) != 1 ||
		r.Request == nil || r.Request.Method != http.MethodGet || r.Request.URL == nil || r.Request.URL.String() != u.String() {
		return false
	}
	raw := r.Header.Get("Location")
	if len(raw) > 4096 || !strings.HasPrefix(raw, "/noweb?") || strings.Contains(raw, "#") {
		return false
	}
	target, err := url.Parse(raw)
	if err != nil || target.String() != raw || target.Scheme != "" || target.Host != "" || target.Path != "/noweb" {
		return false
	}
	target.Scheme, target.Host, target.Path = "https", "relay.mvtx.us", "/"
	if !saintAffriqueRoot(target) {
		return false
	}
	for _, key := range []string{"house_id", "movie_id", "perfd", "perft", "code"} {
		if target.Query().Get(key) != u.Query().Get(key) {
			return false
		}
	}
	return true
}

func saintAffriqueResponseAllowed(u *url.URL, r *http.Response, b checkoutBinding) bool {
	// Only source -> root -> exact unavailable is currently observed. Never
	// treat a relay page, arbitrary redirect, or terminal error as session facts.
	if !boundSaintAffriqueURL(u, b) || r.StatusCode == http.StatusOK {
		return false
	}
	if r.StatusCode < 300 || r.StatusCode >= 400 {
		return true // Shared status/challenge/retry classification remains intact.
	}
	if u.String() != b.source || r.StatusCode != http.StatusFound || len(r.Header.Values("Location")) != 1 {
		return false
	}
	raw := r.Header.Get("Location")
	if !strings.HasPrefix(raw, "/?") || strings.Contains(raw, "#") {
		return false
	}
	target, err := url.Parse("https://relay.mvtx.us" + raw)
	return err == nil && target.String() == "https://relay.mvtx.us"+raw &&
		saintAffriqueRoot(target) && boundSaintAffriqueURL(target, b)
}
