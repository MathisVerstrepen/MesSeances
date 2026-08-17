package syncproxy

import (
	"context"
	cryptotls "crypto/tls"
	"net/http"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/http2"
)

func TestNewFingerprintHTTP2ClientsConfiguresEachProxy(t *testing.T) {
	secret := "synthetic-password"
	proxies, err := Parse(strings.NewReader("http://user:" + secret + "@127.0.0.1:8080\n127.0.0.1:8081"))
	if err != nil {
		t.Fatal(err)
	}
	redirect := func(*http.Request, []*http.Request) error { return nil }
	clients, err := NewFingerprintHTTP2Clients(proxies, 7*time.Second, redirect)
	if err != nil {
		t.Fatal(err)
	}
	if len(clients) != 2 {
		t.Fatalf("clients=%d", len(clients))
	}
	for index, client := range clients {
		transport, ok := client.Transport.(*http2.Transport)
		if !ok || transport.DialTLSContext == nil {
			t.Fatalf("client %d fingerprint transport missing", index)
		}
		if client.Timeout != 7*time.Second || client.CheckRedirect == nil || transport.ReadIdleTimeout != 7*time.Second || transport.PingTimeout != 7*time.Second || transport.WriteByteTimeout != 7*time.Second {
			t.Fatalf("client %d timeout wiring incomplete", index)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	transport := clients[0].Transport.(*http2.Transport)
	_, err = transport.DialTLSContext(ctx, "tcp", "kinepolis.fr:443", &cryptotls.Config{})
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("canceled dial error=%v", err)
	}
}

func TestNewFingerprintHTTP2ClientsRejectsMissingOrInvalidProxy(t *testing.T) {
	for _, proxies := range [][]Proxy{nil, make([]Proxy, 1)} {
		if _, err := NewFingerprintHTTP2Clients(proxies, 5*time.Second, nil); err == nil {
			t.Fatal("invalid proxies accepted")
		}
	}
}
