package cinemaimage

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"messeances/api/internal/syncproxy"
)

func fixtureCertificate(t *testing.T, host string) (tls.Certificate, []byte) {
	t.Helper()
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	c := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: host}, DNSNames: []string{host}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true, IsCA: true}
	der, e := x509.CreateCertificate(rand.Reader, c, c, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, der
}

type fetchObservation struct {
	connect, host, accept, encoding, auth, cookie, origin string
	connects                                              atomic.Int32
	resolves                                              atomic.Int32
}

func fixtureFetcher(t *testing.T, httpsProxy bool, response string, body []byte) (*Fetcher, *fetchObservation) {
	t.Helper()
	targetCert, targetDER := fixtureCertificate(t, "image.example")
	proxyCert, proxyDER := fixtureCertificate(t, "proxy.example")
	scheme := "http"
	if httpsProxy {
		scheme = "https"
	}
	proxies, e := syncproxy.Parse(strings.NewReader(scheme + "://proxy.example:8443"))
	if e != nil {
		t.Fatal(e)
	}
	f := NewFetcher(proxies)
	f.roots = x509.NewCertPool()
	for _, der := range [][]byte{targetDER, proxyDER} {
		cert, e := x509.ParseCertificate(der)
		if e != nil {
			t.Fatal(e)
		}
		f.roots.AddCert(cert)
	}
	o := &fetchObservation{}
	done := make(chan struct{})
	f.resolve = func(context.Context, string, string) ([]netip.Addr, error) {
		o.resolves.Add(1)
		return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
	}
	f.dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr != "proxy.example:8443" {
			return nil, errors.New("direct connection attempted")
		}
		o.connects.Add(1)
		client, server := net.Pipe()
		go func() {
			defer close(done)
			defer func() { _ = server.Close() }()
			conn := server
			if httpsProxy {
				outer := tls.Server(conn, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{proxyCert}})
				if outer.HandshakeContext(ctx) != nil {
					return
				}
				conn = outer
			}
			reader := bufio.NewReader(conn)
			req, e := http.ReadRequest(reader)
			if e != nil {
				return
			}
			o.connect = req.Host
			if req.Method != http.MethodConnect {
				return
			}
			if _, e = io.WriteString(conn, "HTTP/1.1 200 Connection established\r\n\r\n"); e != nil {
				return
			}
			inner := tls.Server(&bufferedConn{Conn: conn, reader: reader}, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{targetCert}})
			if inner.HandshakeContext(ctx) != nil {
				return
			}
			req, e = http.ReadRequest(bufio.NewReader(inner))
			if e != nil {
				return
			}
			o.host = req.Host
			o.accept = req.Header.Get("Accept")
			o.encoding = req.Header.Get("Accept-Encoding")
			o.auth = req.Header.Get("Authorization")
			o.cookie = req.Header.Get("Cookie")
			o.origin = req.Header.Get("Origin")
			if _, e = io.WriteString(inner, response); e != nil {
				return
			}
			_, _ = inner.Write(body)
		}()
		return client, nil
	}
	t.Cleanup(func() {
		if o.connects.Load() == 0 {
			return
		}
		select {
		case <-done:
		case <-time.After(4 * time.Second):
			t.Error("proxy fixture did not drain")
		}
	})
	return f, o
}
func TestFetchPinnedNumericConnectAndIndependentTLS(t *testing.T) {
	for _, httpsProxy := range []bool{false, true} {
		t.Run(fmt.Sprintf("https_proxy_%t", httpsProxy), func(t *testing.T) {
			b := pngPhoto(t, 16, 8)
			f, o := fixtureFetcher(t, httpsProxy, fmt.Sprintf("HTTP/1.1 200 OK\r\nContent-Type: image/png\r\nContent-Length: %d\r\nConnection: close\r\n\r\n", len(b)), b)
			t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
			t.Setenv("NO_PROXY", "*")
			got, media, e := f.Fetch(context.Background(), "https://IMAGE.example/test.png?q=1")
			if e != nil || string(got) != string(b) || media != "image/png" {
				t.Fatal(e)
			}
			if o.connect != "93.184.216.34:443" || o.host != "image.example" || o.encoding != "identity" || o.accept != "image/jpeg, image/png, image/webp" || o.auth != "" || o.cookie != "" || o.origin != "" || o.connects.Load() != 1 || o.resolves.Load() != 1 {
				t.Fatalf("observation %+v", o)
			}
		})
	}
}
func TestFetchURLPolicy(t *testing.T) {
	for _, raw := range []string{"http://image.example/x", "//image.example/x", "https://user:pass@image.example/x", "https://127.0.0.1/x", "https://[::1]/x", "https://image.example:444/x", "https://image.example:/x", "https://image.example./x", "https://localhost/x", "https://-bad.example/x", "https://image.example/x#", "https://image.example/x#fragment", "https://image.example\\x", "https://image.example/%0a", "https://image.example/a b", "https://éxample.test/x", "https://image.example/a?q=%zz", "https://image.example/" + strings.Repeat("a", 2048)} {
		if _, e := imageURL(raw); !errors.Is(e, ErrURL) {
			t.Fatalf("accepted %q", raw)
		}
	}
	for _, raw := range []string{"https://image.example:443/a%20b?x=1", "https://IMAGE.example/a", "https://sub.image.example/"} {
		if _, e := imageURL(raw); e != nil {
			t.Fatal(e)
		}
	}
}
func TestFetchDNSPolicyAndNoDirectFallback(t *testing.T) {
	proxies, e := syncproxy.Parse(strings.NewReader("http://proxy.example:8443"))
	if e != nil {
		t.Fatal(e)
	}
	for _, ips := range [][]string{{"127.0.0.1"}, {"10.0.0.1"}, {"::ffff:127.0.0.1"}, {"93.184.216.34", "192.168.1.1"}, {"100.64.0.1"}, {"192.0.2.1"}, {"198.18.0.1"}, {"203.0.113.1"}, {"240.0.0.1"}, {"2001:db8::1"}, {"2002::1"}, {"3fff::1"}, {"fc00::1"}, {"fe80::1"}, {"::"}, {"ff02::1"}} {
		f := NewFetcher(proxies)
		f.resolve = func(context.Context, string, string) ([]netip.Addr, error) {
			out := []netip.Addr{}
			for _, ip := range ips {
				out = append(out, netip.MustParseAddr(ip))
			}
			return out, nil
		}
		f.dial = func(context.Context, string, string) (net.Conn, error) {
			t.Fatal("dial on denied DNS")
			return nil, ErrDownload
		}
		if _, _, e = f.Fetch(context.Background(), "https://image.example/x"); !errors.Is(e, ErrURL) {
			t.Fatal(ips, e)
		}
	}
	f := NewFetcher(nil)
	if _, _, e = f.Fetch(context.Background(), "https://image.example/x"); !errors.Is(e, ErrImportUnavailable) {
		t.Fatal(e)
	}
	f = NewFetcher(proxies)
	f.resolve = func(context.Context, string, string) ([]netip.Addr, error) {
		return nil, errors.New("DNS fixture failure")
	}
	if _, _, e = f.Fetch(context.Background(), "https://image.example/x"); !errors.Is(e, ErrDownload) {
		t.Fatal(e)
	}
	if !publicIP(netip.MustParseAddr("::ffff:93.184.216.34")) || !publicIP(netip.MustParseAddr("2606:4700::1111")) {
		t.Fatal("public address denied")
	}
}
func TestFetchResponsePolicy(t *testing.T) {
	for _, tc := range []struct {
		name, headers string
		body          []byte
		want          error
	}{
		{"redirect", "HTTP/1.1 302 Found\r\nLocation: https://other.example/x\r\nContent-Length: 0\r\n\r\n", nil, ErrDownload},
		{"failure", "HTTP/1.1 500 Failed\r\nContent-Length: 0\r\n\r\n", nil, ErrDownload},
		{"encoded", "HTTP/1.1 200 OK\r\nContent-Type: image/png\r\nContent-Encoding: gzip\r\nContent-Length: 0\r\n\r\n", nil, ErrDownload},
		{"duplicate_type", "HTTP/1.1 200 OK\r\nContent-Type: image/png\r\nContent-Type: image/jpeg\r\nContent-Length: 0\r\n\r\n", nil, ErrDownload},
		{"unsupported", "HTTP/1.1 200 OK\r\nContent-Type: image/svg+xml\r\nContent-Length: 0\r\n\r\n", nil, ErrUnsupported},
		{"large_length", fmt.Sprintf("HTTP/1.1 200 OK\r\nContent-Type: image/png\r\nContent-Length: %d\r\n\r\n", MaxInput+1), nil, ErrTooLarge},
		{"bounded_without_length", "HTTP/1.1 200 OK\r\nContent-Type: image/png\r\nConnection: close\r\n\r\n", make([]byte, MaxInput+1), ErrTooLarge},
		{"truncated", "HTTP/1.1 200 OK\r\nContent-Type: image/png\r\nContent-Length: 50\r\n\r\n", []byte{1}, ErrDownload},
		{"header_cap", "HTTP/1.1 200 OK\r\nContent-Type: image/png\r\nX-Padding: " + strings.Repeat("x", 17<<10) + "\r\nContent-Length: 0\r\n\r\n", nil, ErrDownload},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, o := fixtureFetcher(t, false, tc.headers, tc.body)
			if _, _, e := f.Fetch(context.Background(), "https://image.example/x"); !errors.Is(e, tc.want) {
				t.Fatalf("got %v want %v", e, tc.want)
			}
			if o.connects.Load() != 1 {
				t.Fatal("additional request")
			}
		})
	}
}
func TestFetchCertificateVerificationAndCancellation(t *testing.T) {
	for _, httpsProxy := range []bool{false, true} {
		f, _ := fixtureFetcher(t, httpsProxy, "", nil)
		f.roots = x509.NewCertPool()
		if _, _, e := f.Fetch(context.Background(), "https://image.example/x"); !errors.Is(e, ErrDownload) {
			t.Fatal("untrusted TLS accepted", e)
		}
	}
	f, _ := fixtureFetcher(t, false, "", nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, e := f.Fetch(ctx, "https://image.example/x"); !errors.Is(e, ErrDownload) {
		t.Fatal(e)
	}
}
