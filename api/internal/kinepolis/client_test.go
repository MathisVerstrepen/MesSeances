package kinepolis

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/http2"
	"messeances/api/internal/syncproxy"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

type trackingReadCloser struct {
	io.Reader
	closes int
}

func (r *trackingReadCloser) Close() error {
	r.closes++
	return nil
}

type failingReader struct {
	remaining int
	err       error
}

func (r *failingReader) Read(buffer []byte) (int, error) {
	if r.remaining == 0 {
		return 0, r.err
	}
	if len(buffer) > r.remaining {
		buffer = buffer[:r.remaining]
	}
	for index := range buffer {
		buffer[index] = 'x'
	}
	r.remaining -= len(buffer)
	if r.remaining == 0 {
		return len(buffer), r.err
	}
	return len(buffer), nil
}

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := f(request)
	if response != nil && response.Request == nil {
		response.Request = request
	}
	return response, err
}
func testResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(body))}
}
func testResponseReader(status int, body io.ReadCloser) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"text/html"}}, Body: body}
}
func testProxies(t *testing.T, raw string) []syncproxy.Proxy {
	t.Helper()
	proxies, err := syncproxy.Parse(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	return proxies
}
func testClient(t *testing.T) *Client {
	t.Helper()
	client, err := NewClient(ClientConfig{Proxies: testProxies(t, "127.0.0.1:8080"), RequestInterval: time.Second, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestClientRequiresAndConfiguresProxyTransport(t *testing.T) {
	if _, err := NewClient(ClientConfig{RequestInterval: time.Second, Timeout: 5 * time.Second}); err == nil {
		t.Fatal("proxy-free client accepted")
	}
	if _, err := NewClient(ClientConfig{Proxies: make([]syncproxy.Proxy, 1), RequestInterval: time.Second, Timeout: 5 * time.Second}); err == nil {
		t.Fatal("invalid proxy allowed direct fallback")
	}
	secret := "synthetic-password"
	client, err := NewClient(ClientConfig{Proxies: testProxies(t, "http://synthetic-user:"+secret+"@127.0.0.1:8080"), RequestInterval: time.Second, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	transport, ok := client.clients[0].Transport.(*http2.Transport)
	if !ok || transport.DialTLSContext == nil {
		t.Fatal("fingerprint HTTP/2 proxy transport missing")
	}
}

func TestClientStrictRequestAndBodyLimit(t *testing.T) {
	client := testClient(t)
	client.clients[0].Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != ScheduleURL || request.Header.Get("User-Agent") != userAgent || request.Header.Get("Accept") != accept || request.Header.Get("Accept-Language") != acceptLang {
			t.Fatalf("request=%+v headers=%v", request.URL, request.Header)
		}
		return testResponse(200, "<html>ok</html>"), nil
	})
	body, err := client.Fetch(context.Background())
	if err != nil || string(body) != "<html>ok</html>" {
		t.Fatalf("body=%q err=%v", body, err)
	}
	client = testClient(t)
	client.clients[0].Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return testResponse(200, strings.Repeat("x", MaxBodySize+1)), nil
	})
	if _, err := client.Fetch(context.Background()); err == nil {
		t.Fatal("oversized body accepted")
	}
}

func TestReadBoundedLimitAndReadErrors(t *testing.T) {
	body, err := readBounded(io.LimitReader(strings.NewReader(strings.Repeat("x", MaxBodySize)), MaxBodySize))
	if err != nil || len(body) != MaxBodySize {
		t.Fatalf("exact limit: bytes=%d err=%v", len(body), err)
	}

	_, err = readBounded(strings.NewReader(strings.Repeat("x", MaxBodySize+1)))
	if !errors.Is(err, errResponseTooLarge) {
		t.Fatalf("limit plus one: err=%v", err)
	}

	readFailure := errors.New("synthetic reader cause")
	_, err = readBounded(&failingReader{remaining: 4, err: readFailure})
	if !errors.Is(err, readFailure) {
		t.Fatalf("reader error identity lost: err=%v", err)
	}

	_, err = readBounded(&failingReader{remaining: MaxBodySize + 1, err: readFailure})
	if !errors.Is(err, errResponseTooLarge) {
		t.Fatalf("oversize did not take precedence: err=%v", err)
	}
}

func TestClientReadFailureRotatesAndCanSucceed(t *testing.T) {
	newClient := func(t *testing.T) *Client {
		t.Helper()
		client, err := NewClient(ClientConfig{Proxies: testProxies(t, "127.0.0.1:8080\n127.0.0.1:8081"), RequestInterval: time.Second, Timeout: 5 * time.Second})
		if err != nil {
			t.Fatal(err)
		}
		return client
	}

	t.Run("rotates and succeeds", func(t *testing.T) {
		client := newClient(t)
		readFailure := errors.New("synthetic reader cause")
		firstBody := &trackingReadCloser{Reader: &failingReader{remaining: 4, err: readFailure}}
		first, second := 0, 0
		client.clients[0].Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
			first++
			return testResponseReader(http.StatusOK, firstBody), nil
		})
		client.clients[1].Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
			second++
			return testResponse(http.StatusOK, "ok"), nil
		})

		body, err := client.Fetch(context.Background())
		if err != nil || string(body) != "ok" || first != 1 || second != 1 || firstBody.closes != 1 {
			t.Fatalf("body=%q requests=%d,%d closes=%d err=%v", body, first, second, firstBody.closes, err)
		}
	})

	t.Run("exhaustion is redacted", func(t *testing.T) {
		client := newClient(t)
		readFailure := errors.New("synthetic reader cause")
		bodies := []*trackingReadCloser{
			{Reader: &failingReader{err: readFailure}},
			{Reader: &failingReader{err: readFailure}},
		}
		for index := range client.clients {
			body := bodies[index]
			client.clients[index].Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				return testResponseReader(http.StatusOK, body), nil
			})
		}

		_, err := client.Fetch(context.Background())
		if err == nil || err.Error() != "fetch Kinepolis schedule failed on proxy 2: response read" || client.RequestCount() != 2 {
			t.Fatalf("requests=%d err=%v", client.RequestCount(), err)
		}
		for index, body := range bodies {
			if body.closes != 1 {
				t.Fatalf("body %d closes=%d", index, body.closes)
			}
		}
	})
}

func TestClientOversizeDoesNotRotate(t *testing.T) {
	client, err := NewClient(ClientConfig{Proxies: testProxies(t, "127.0.0.1:8080\n127.0.0.1:8081"), RequestInterval: time.Second, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	oversizedBody := &trackingReadCloser{Reader: strings.NewReader(strings.Repeat("x", MaxBodySize+1))}
	first, second := 0, 0
	client.clients[0].Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		first++
		return testResponseReader(http.StatusOK, oversizedBody), nil
	})
	client.clients[1].Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		second++
		return testResponse(http.StatusOK, "ok"), nil
	})

	_, err = client.Fetch(context.Background())
	if err == nil || err.Error() != "fetch Kinepolis schedule failed on proxy 1: response too large" || first != 1 || second != 0 || oversizedBody.closes != 1 {
		t.Fatalf("requests=%d,%d closes=%d err=%v", first, second, oversizedBody.closes, err)
	}
}

func TestClientRotatesBlockedProxiesAndStopsWhenExhausted(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusTooManyRequests} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			client, err := NewClient(ClientConfig{Proxies: testProxies(t, "127.0.0.1:8080\n127.0.0.1:8081"), RequestInterval: time.Second, Timeout: 5 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			first, second := 0, 0
			client.clients[0].Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				first++
				return testResponse(status, "blocked"), nil
			})
			client.clients[1].Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { second++; return testResponse(http.StatusOK, "ok"), nil })
			body, err := client.Fetch(context.Background())
			if err != nil || string(body) != "ok" || first != 1 || second != 1 {
				t.Fatalf("body=%q requests=%d,%d err=%v", body, first, second, err)
			}

			client, err = NewClient(ClientConfig{Proxies: testProxies(t, "127.0.0.1:8080\n127.0.0.1:8081"), RequestInterval: time.Second, Timeout: 5 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			for index := range client.clients {
				client.clients[index].Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return testResponse(status, "blocked"), nil })
			}
			_, err = client.Fetch(context.Background())
			var terminal *TerminalError
			if !errors.As(err, &terminal) || client.RequestCount() != 2 {
				t.Fatalf("requests=%d err=%v", client.RequestCount(), err)
			}
		})
	}
}

func TestClientRetriesServerFailureAndRedactsCredentials(t *testing.T) {
	secret := "synthetic-password"
	client, err := NewClient(ClientConfig{Proxies: testProxies(t, "http://user:"+secret+"@127.0.0.1:8080\n127.0.0.1:8081"), RequestInterval: time.Second, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	client.clients[0].Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return testResponse(500, secret), nil })
	client.clients[1].Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return testResponse(200, "ok"), nil })
	body, err := client.Fetch(context.Background())
	if err != nil || string(body) != "ok" || calls != 2 {
		t.Fatalf("body=%q calls=%d err=%v", body, calls, err)
	}
	client = testClient(t)
	client.clients[0].Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("proxy " + secret + " failed") })
	_, err = client.Fetch(context.Background())
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("credential leaked: %v", err)
	}
}

func TestClientTerminalChallengeDoesNotRotateOrLeakBody(t *testing.T) {
	secret := "synthetic-body-secret"
	client, err := NewClient(ClientConfig{Proxies: testProxies(t, "127.0.0.1:8080\n127.0.0.1:8081"), RequestInterval: time.Second, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	first, second := 0, 0
	client.clients[0].Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		first++
		return testResponse(200, "<title>DataDome CAPTCHA</title>"+secret), nil
	})
	client.clients[1].Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { second++; return testResponse(200, "ok"), nil })
	_, err = client.Fetch(context.Background())
	var terminal *TerminalError
	if !errors.As(err, &terminal) || strings.Contains(err.Error(), secret) || first != 1 || second != 0 {
		t.Fatalf("requests=%d,%d err=%v", first, second, err)
	}
}

func TestClientRejectsStatusContentTypeTimeoutAndRedirect(t *testing.T) {
	if _, err := NewClient(ClientConfig{Proxies: testProxies(t, "127.0.0.1:8080"), RequestInterval: time.Second, Timeout: time.Second}); err == nil {
		t.Fatal("short timeout accepted")
	}
	for _, test := range []struct {
		status  int
		content string
	}{{500, "text/html"}, {200, ""}, {200, "application/json"}, {200, "text/html-invalid"}} {
		client := testClient(t)
		client.clients[0].Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
			response := testResponse(test.status, "x")
			response.Header.Set("Content-Type", test.content)
			return response, nil
		})
		if _, err := client.Fetch(context.Background()); err == nil {
			t.Fatalf("response accepted: %+v", test)
		}
	}
	request, _ := http.NewRequest(http.MethodGet, "https://evil.example/", nil)
	if !errors.Is(checkRedirect(request, nil), errRedirectHost) {
		t.Fatal("cross-host redirect accepted")
	}
}
