package cgr

import (
	"testing"
)

func TestOptionalTrailerCaptureDoesNotRejectMovieMetadata(t *testing.T) {
	for _, raw := range []string{`{"HD":"https://fr.vid.web.acsta.net/nmedia/33/trailer.mp4"}`, `123`, `{"HD":"https://evil.example/a.mp4"}`} {
		movies, err := parseMovies([]byte(`[{"id":"1001","title":"Synthetic","runtime":5400,"trailer":` + raw + `}]`))
		if err != nil {
			t.Fatal(err)
		}
		m := movies["1001"]
		if m.runtime != 90 {
			t.Fatal("schedule metadata changed")
		}
		if raw == `{"HD":"https://fr.vid.web.acsta.net/nmedia/33/trailer.mp4"}` {
			if m.trailer == nil || !m.trailerObserved {
				t.Fatal("trailer lost")
			}
		} else if m.trailer != nil || m.trailerObserved {
			t.Fatal("invalid trailer observed")
		}
	}
}
