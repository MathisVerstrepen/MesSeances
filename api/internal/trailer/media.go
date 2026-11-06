// Package trailer stores provider observations independently of TMDB metadata.
package trailer

import (
	"encoding/json"
	"html"
	"net/url"
	"regexp"
	"strings"
)

type Media struct {
	Type     string  `json:"type"`
	URL      string  `json:"url"`
	Source   string  `json:"source"`
	Language *string `json:"language"`
}

type Observation struct {
	Provider string
	MovieID  string
	Observed bool
	Media    *Media
}

var trackingValue = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
var youtubeKey = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)
var acstaPath = regexp.MustCompile(`^/(?:nmedia|medias)/[A-Za-z0-9_./-]+\.mp4$`)

func YouTube(key, source string) *Media {
	if !youtubeKey.MatchString(key) {
		return nil
	}
	return &Media{Type: "youtube", URL: "https://www.youtube.com/watch?v=" + key, Source: source}
}

// MP4 accepts only the observed public AlloCiné media shape. Tracking parameters
// are discarded, while all unknown parameters and URL aliases are rejected.
func MP4(raw, source string) *Media {
	raw = html.UnescapeString(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "fr.vid.web.acsta.net" || u.User != nil || u.Fragment != "" || u.RawPath != "" || u.Opaque != "" || u.ForceQuery && u.RawQuery == "" || strings.ContainsAny(raw, "\\#% \t\r\n") || len(raw) > 2048 || !acstaPath.MatchString(u.Path) || strings.Contains(u.Path, "//") {
		return nil
	}
	for _, part := range strings.Split(u.Path, "/") {
		if part == "." || part == ".." {
			return nil
		}
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return nil
	}
	for k, v := range q {
		if len(v) != 1 || k != "source" && k != "platform" || !trackingValue.MatchString(v[0]) {
			return nil
		}
	}
	u.RawQuery = ""
	u.ForceQuery = false
	return &Media{Type: "mp4", URL: u.String(), Source: source}
}

func Valid(m *Media) bool {
	if m == nil || m.Language != nil {
		return false
	}
	switch m.Source {
	case "cgr", "ugc":
		v := MP4(m.URL, m.Source)
		return v != nil && *v == *m
	case "cineville":
		const prefix = "https://www.youtube.com/watch?v="
		return m.Type == "youtube" && strings.HasPrefix(m.URL, prefix) && youtubeKey.MatchString(strings.TrimPrefix(m.URL, prefix))
	}
	return false
}

// CGR preserves absence separately from malformed optional metadata.
func CGR(raw json.RawMessage) (*Media, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	if string(raw) == "null" {
		return nil, true
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return nil, false
	}
	_, hasHD := fields["HD"]
	_, hasSD := fields["SD"]
	if !hasHD && !hasSD {
		return nil, false
	}
	for _, key := range []string{"HD", "SD"} {
		value := fields[key]
		if len(value) == 0 || string(value) == "null" {
			continue
		}
		var s string
		if json.Unmarshal(value, &s) != nil {
			return nil, false
		}
		if s == "" {
			continue
		}
		if m := MP4(s, "cgr"); m != nil {
			return m, true
		}
		return nil, false
	}
	return nil, true
}

func Cineville(id, visible json.RawMessage) (*Media, bool) {
	if len(id) == 0 || len(visible) == 0 {
		return nil, false
	}
	var show int
	if string(visible) == "null" {
		return nil, false
	}
	if json.Unmarshal(visible, &show) != nil {
		var s string
		if json.Unmarshal(visible, &s) != nil || s != "0" && s != "1" {
			return nil, false
		}
		if s == "1" {
			show = 1
		}
	}
	if show != 0 && show != 1 {
		return nil, false
	}
	if show == 0 {
		return nil, true
	}
	var key string
	if string(id) == "null" {
		return nil, false
	}
	if json.Unmarshal(id, &key) != nil {
		return nil, false
	}
	if key == "" {
		return nil, true
	}
	m := YouTube(key, "cineville")
	return m, m != nil
}
