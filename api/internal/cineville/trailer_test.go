package cineville

import (
	"encoding/json"
	"testing"
)

func TestTrailerCaptureBindsMetadataVisaAndSkipsMalformedOptionalFields(t *testing.T) {
	for _, tc := range []struct {
		visa, video, show string
		want              bool
	}{
		{`"73"`, `"AbC-12_xYz9"`, `1`, true}, {`"74"`, `"AbC-12_xYz9"`, `1`, false}, {`"73"`, `12`, `1`, false}, {`"73"`, `"AbC-12_xYz9"`, `{}`, false},
	} {
		f := film{Visa: "73", Title: "Synthetic", Metadata: json.RawMessage(`{"visa":` + tc.visa + `,"titre":"Synthetic","duree":"1h30","video_id":` + tc.video + `,"show_video":` + tc.show + `}`)}
		m, err := parseMovie(f)
		if err != nil {
			t.Fatal("optional trailer broke movie", err)
		}
		if (m.SourceTrailer != nil) != tc.want {
			t.Fatalf("trailer=%v want %v", m.SourceTrailer, tc.want)
		}
	}
}
