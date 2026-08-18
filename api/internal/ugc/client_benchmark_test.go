package ugc

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

var benchmarkFetchResult FetchResult

func BenchmarkClientGetKnownLength(b *testing.B) {
	benchmarkClientGet(b, true)
}

func BenchmarkClientGetUnknownLength(b *testing.B) {
	benchmarkClientGet(b, false)
}

func benchmarkClientGet(b *testing.B, knownLength bool) {
	b.Helper()
	body := strings.Repeat("public synthetic response data\n", 32*1024)
	client := &Client{
		clients: []*http.Client{{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			response := &http.Response{
				StatusCode:    http.StatusOK,
				Body:          io.NopCloser(strings.NewReader(body)),
				Header:        http.Header{},
				Request:       request,
				ContentLength: -1,
			}
			if knownLength {
				response.ContentLength = int64(len(body))
			}
			return response, nil
		})}},
		unavailable:  make([]bool, 1),
		leased:       make([]bool, 1),
		leaseChanged: make(chan struct{}),
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(body)))
	b.ResetTimer()
	for range b.N {
		result, err := client.Get(context.Background(), "showings", "https://www.ugc.fr/public-benchmark")
		if err != nil || len(result.Body) != len(body) {
			b.Fatalf("body=%d want=%d error=%v", len(result.Body), len(body), err)
		}
		benchmarkFetchResult = result
	}
}
