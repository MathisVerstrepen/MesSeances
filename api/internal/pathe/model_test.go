package pathe

import "testing"

func TestCinemaProgramDecodesObjectsAndEmptyArraySentinels(t *testing.T) {
	for name, body := range map[string][]byte{
		"objects":           []byte(`{"days":{},"shows":{}}`),
		"empty arrays":      []byte(`{"days":[],"shows":[]}`),
		"array and object":  []byte(`{"days":[],"shows":{}}`),
		"object and array":  []byte(`{"days":{},"shows":[]}`),
		"populated objects": []byte(`{"days":{"2026-08-15":{}},"shows":{"film":{"days":{"2026-08-15":{"tags":[],"versions":[]}}}}}`),
	} {
		t.Run(name, func(t *testing.T) {
			var program cinemaProgram
			if err := decodeJSON(body, &program); err != nil {
				t.Fatalf("valid program rejected: %v", err)
			}
			if program.Days == nil || program.Shows == nil {
				t.Fatalf("program fields not normalized: %+v", program)
			}
		})
	}
}

func TestCinemaProgramRejectsInvalidSentinelShapes(t *testing.T) {
	for name, body := range map[string][]byte{
		"missing days":         []byte(`{"shows":{}}`),
		"missing shows":        []byte(`{"days":{}}`),
		"null days":            []byte(`{"days":null,"shows":{}}`),
		"null shows":           []byte(`{"days":{},"shows":null}`),
		"nonempty days array":  []byte(`{"days":[{}],"shows":{}}`),
		"nonempty shows array": []byte(`{"days":{},"shows":[{}]}`),
		"scalar days":          []byte(`{"days":true,"shows":{}}`),
		"scalar shows":         []byte(`{"days":{},"shows":"invalid"}`),
		"malformed show":       []byte(`{"days":{},"shows":{"film":[]}}`),
		"malformed item days":  []byte(`{"days":{},"shows":{"film":{"days":[]}}}`),
		"top-level array":      []byte(`[]`),
		"malformed JSON":       []byte(`{"days":{},"shows":`),
	} {
		t.Run(name, func(t *testing.T) {
			var program cinemaProgram
			if err := decodeJSON(body, &program); err == nil {
				t.Fatal("invalid program accepted")
			}
		})
	}
}
