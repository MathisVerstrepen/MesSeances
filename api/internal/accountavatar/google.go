package accountavatar

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
	"unicode"
)

const pictureHost = "lh3.googleusercontent.com"

// ValidGoogleURL is deliberately narrower than Google's entire media estate.
func ValidGoogleURL(raw string) bool {
	if len(raw) == 0 || len(raw) > 2048 || strings.ContainsAny(raw, "\\\x00\r\n\t ") {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != pictureHost || u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(raw, "#") || !strings.HasPrefix(u.Path, "/") || strings.HasPrefix(u.Path, "//") || u.String() != raw {
		return false
	}
	for _, c := range u.Path {
		if unicode.IsControl(c) || c == '\\' {
			return false
		}
	}
	return true
}

var deniedNetworks = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("192.88.99.0/24"), netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001::/23"), netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("2002::/16"), netip.MustParsePrefix("3fff::/20"),
}

func publicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	// Accept only current IPv6 global-unicast allocation; excludes translation and reserved space.
	if ip.Is6() && !netip.MustParsePrefix("2000::/3").Contains(ip) {
		return false
	}
	for _, p := range deniedNetworks {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}

type pictureClient struct {
	http      *http.Client
	transport *http.Transport
}

func newPictureClient() *pictureClient {
	return pictureClientWithDial(net.DefaultResolver.LookupNetIP, (&net.Dialer{Timeout: 2 * time.Second}).DialContext)
}

// Private injection seam: tests exercise pinned IP dialing, never a production bypass.
func pictureClientWithDial(resolve func(context.Context, string, string) ([]netip.Addr, error), dial func(context.Context, string, string) (net.Conn, error)) *pictureClient {
	t := &http.Transport{Proxy: nil, DisableCompression: true, DisableKeepAlives: true, MaxResponseHeaderBytes: 16 << 10, TLSHandshakeTimeout: 2 * time.Second, ResponseHeaderTimeout: 2 * time.Second, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil || host != pictureHost || port != "443" {
				return nil, ErrInvalid
			}
			ips, err := resolve(ctx, "ip", host)
			if err != nil || len(ips) == 0 {
				return nil, ErrInvalid
			}
			for _, ip := range ips {
				if !publicIP(ip) {
					return nil, ErrInvalid
				}
			}
			return dial(ctx, network, net.JoinHostPort(ips[0].Unmap().String(), port))
		},
	}
	return &pictureClient{http: &http.Client{Transport: t, CheckRedirect: func(*http.Request, []*http.Request) error { return ErrInvalid }}, transport: t}
}
func (p *pictureClient) close() { p.transport.CloseIdleConnections() }

// Fetch uses its caller's shared import deadline and admission slot.
func (s *Store) Fetch(ctx context.Context, raw string) ([]byte, error) {
	if !ValidGoogleURL(raw) {
		return nil, ErrInvalid
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, ErrInvalid
	}
	r.Header.Set("Accept-Encoding", "identity")
	r.Header.Set("Accept", "image/jpeg, image/png, image/webp")
	resp, err := s.client.http.Do(r)
	if err != nil {
		return nil, ErrInvalid
	}
	defer func() { _ = resp.Body.Close() }() // The bounded read reports transport failures.
	if resp.StatusCode != 200 || resp.Header.Get("Content-Encoding") != "" || len(resp.Header.Values("Content-Type")) != 1 {
		return nil, ErrInvalid
	}
	b, err := ReadInput(resp.Body)
	if err != nil {
		return nil, err
	}
	return Normalize(ctx, b, resp.Header.Get("Content-Type"))
}
