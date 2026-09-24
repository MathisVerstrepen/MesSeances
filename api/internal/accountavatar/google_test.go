package accountavatar

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestGoogleURLAndIPPolicy(t *testing.T) {
	for _, raw := range []string{"", "http://lh3.googleusercontent.com/a", "https://lh3.googleusercontent.com:443/a", "https://user@lh3.googleusercontent.com/a", "https://lh3.googleusercontent.com/a?", "https://lh3.googleusercontent.com/a#", "https://lh3.googleusercontent.com/a?q=x", "https://lh3.googleusercontent.com./a", "https://LH3.googleusercontent.com/a", "https://lh4.googleusercontent.com/a", "https://lh3.googleusercontent.com//a", "https://lh3.googleusercontent.com/a%0a", "https://lh3.googleusercontent.com/a%5c", "https://lh3.googleusercontent.com/" + strings.Repeat("a", 2048)} {
		if ValidGoogleURL(raw) {
			t.Fatalf("unsafe URL %q", raw)
		}
	}
	if !ValidGoogleURL("https://lh3.googleusercontent.com/a=s96-c") {
		t.Fatal("valid URL rejected")
	}
	for _, ip := range []string{"127.0.0.1", "::1", "10.0.0.1", "169.254.169.254", "100.64.0.1", "192.0.2.1", "198.18.0.1", "224.0.0.1", "240.0.0.1", "::ffff:127.0.0.1", "::ffff:10.0.0.1", "64:ff9b::7f00:1", "2001:db8::1", "2002:7f00:1::", "fc00::1", "fe80::1", "ff02::1", "3fff::1"} {
		if publicIP(netip.MustParseAddr(ip)) {
			t.Fatal("nonpublic IP", ip)
		}
	}
	for _, ip := range []string{"142.250.1.1", "2607:f8b0:4000::1"} {
		if !publicIP(netip.MustParseAddr(ip)) {
			t.Fatal("public IP denied")
		}
	}
}
func TestCheckedDialRejectsEveryMixedAnswer(t *testing.T) {
	for _, ips := range [][]netip.Addr{{}, {netip.MustParseAddr("127.0.0.1")}, {netip.MustParseAddr("142.250.1.1"), netip.MustParseAddr("::ffff:127.0.0.1")}} {
		p := pictureClientWithDial(func(context.Context, string, string) ([]netip.Addr, error) { return ips, nil }, func(context.Context, string, string) (net.Conn, error) { t.Fatal("unsafe dial"); return nil, nil })
		if _, err := p.transport.DialContext(t.Context(), "tcp", pictureHost+":443"); err == nil {
			t.Fatal("unsafe resolution accepted")
		}
		p.close()
	}
	calls := 0
	p := pictureClientWithDial(func(context.Context, string, string) ([]netip.Addr, error) {
		calls++
		return []netip.Addr{netip.MustParseAddr("142.250.1.1")}, nil
	}, func(_ context.Context, _ string, address string) (net.Conn, error) {
		if address != "142.250.1.1:443" {
			t.Fatal("hostname re-resolved")
		}
		return nil, errors.New("local injected dial")
	})
	defer p.close()
	_, _ = p.transport.DialContext(t.Context(), "tcp", pictureHost+":443")
	if calls != 1 || p.transport.Proxy != nil || !p.transport.DisableCompression || p.transport.MaxResponseHeaderBytes != 16<<10 {
		t.Fatal("transport policy")
	}
}
func TestLocalGoogleFetchLimitsAndTLSHostname(t *testing.T) {
	b := fixture(t, "png")
	mode := "ok"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS.ServerName != pictureHost || r.Host != pictureHost || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("Accept-Encoding") != "identity" {
			t.Error("request authority leaked")
		}
		w.Header().Set("Content-Type", "image/png")
		switch mode {
		case "redirect":
			http.Redirect(w, r, "https://127.0.0.1/secret", http.StatusFound)
		case "encoding":
			w.Header().Set("Content-Encoding", "gzip")
			_, _ = w.Write(b)
		case "status":
			w.WriteHeader(500)
		case "headers":
			w.Header().Set("X-Large", strings.Repeat("x", 20<<10))
			_, _ = w.Write(b)
		case "oversize":
			_, _ = io.Copy(w, io.LimitReader(bytes.NewReader(make([]byte, MaxInput+1)), MaxInput+1))
		case "slow":
			<-r.Context().Done()
		default:
			_, _ = w.Write(b)
		}
	}))
	defer server.Close()
	p := pictureClientWithDial(func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("142.250.1.1")}, nil
	}, func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "142.250.1.1:443" {
			t.Fatal("unpinned address")
		}
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	})
	defer p.close()
	// Fixture certificate belongs to httptest, not Google. Only this private test
	// transport bypasses certificate validation; production verification stays enabled.
	if p.transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("production TLS disabled")
	}
	p.transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // local synthetic TLS fixture
	s := &Store{client: p}
	for _, m := range []string{"ok", "redirect", "encoding", "status", "headers", "oversize", "slow"} {
		mode = m
		// Successful synchronous WebP encoding needs race-instrumentation headroom.
		// Keep the slow-server cancellation check short; production budget is unchanged.
		budget := 5 * time.Second
		if m == "slow" {
			budget = 200 * time.Millisecond
		}
		ctx, cancel := context.WithTimeout(t.Context(), budget)
		got, err := s.Fetch(ctx, "https://lh3.googleusercontent.com/photo")
		cancel()
		if m == "ok" {
			if err != nil || len(got) == 0 {
				t.Fatal(err)
			}
		} else if err == nil {
			t.Fatal("unsafe fetch accepted", m)
		}
	}
}
