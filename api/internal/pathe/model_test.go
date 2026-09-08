package pathe

import (
	"bytes"
	"reflect"
	"testing"
)

func TestObjectOrEmptyArraySentinelWhitespace(t *testing.T) {
	for _, body := range []string{"[]", "[ ]", "[\t]", "[\n]", "[\r]", " \t\r\n[ \t\r\n ] \t\r\n"} {
		t.Run(body, func(t *testing.T) {
			value := objectOrEmptyArray[int]{"previous": 1}
			if err := value.UnmarshalJSON([]byte(body)); err != nil || value == nil || len(value) != 0 {
				t.Fatalf("empty sentinel not normalized: value=%v err=%v", value, err)
			}
		})
	}
	for _, body := range []string{"", "[", "]", "[0]", "[null]", "[{}]", "[[]]", "[,]", "null", "[\v]", "[\f]", "[\x00]", "[\u00a0]", "[\u2003]", "[] []", "[] {}", "[] junk", "[ ] ]"} {
		t.Run(body, func(t *testing.T) {
			value := objectOrEmptyArray[int]{"previous": 1}
			if err := value.UnmarshalJSON([]byte(body)); err == nil {
				t.Fatal("invalid sentinel accepted")
			}
			if !reflect.DeepEqual(value, objectOrEmptyArray[int]{"previous": 1}) {
				t.Fatalf("rejected sentinel mutated destination: %v", value)
			}
		})
	}
}

func TestObjectOrEmptyArrayPreservesTypedObjects(t *testing.T) {
	var numbers objectOrEmptyArray[int]
	if err := numbers.UnmarshalJSON([]byte(`{"number":42}`)); err != nil || numbers["number"] != 42 {
		t.Fatalf("typed number map: %v err=%v", numbers, err)
	}
	var lists objectOrEmptyArray[[]string]
	if err := lists.UnmarshalJSON([]byte(`{"list":["one","two"]}`)); err != nil || !reflect.DeepEqual(lists["list"], []string{"one", "two"}) {
		t.Fatalf("typed list map: %v err=%v", lists, err)
	}
	if err := numbers.UnmarshalJSON([]byte(`{"number":"wrong type"}`)); err == nil {
		t.Fatal("invalid object value accepted")
	}
}

func TestObjectOrEmptyArrayRejectsLargeArrayWithBoundedAllocations(t *testing.T) {
	// A near-limit payload previously materialized millions of RawMessages
	// before rejecting the array. Build input outside allocation measurements.
	body := make([]byte, MaxResponseBytes-1)
	body[0], body[len(body)-1] = '[', ']'
	for i := 1; i < len(body)-1; i++ {
		if i%2 == 1 {
			body[i] = '0'
		} else {
			body[i] = ','
		}
	}
	for _, input := range [][]byte{body, append(append([]byte{'['}, bytes.Repeat([]byte{' '}, MaxResponseBytes-4)...), '0', ']')} {
		allocations := testing.AllocsPerRun(3, func() {
			var value objectOrEmptyArray[[]sessionResponse]
			if err := value.UnmarshalJSON(input); err == nil || value != nil {
				t.Fatal("large nonempty array accepted")
			}
		})
		if allocations > 4 {
			t.Fatalf("nonempty array rejection allocated per payload element: %.0f allocations", allocations)
		}
	}
}

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
