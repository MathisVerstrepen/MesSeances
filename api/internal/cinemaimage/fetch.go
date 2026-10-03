package cinemaimage

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
	"unicode"

	"messeances/api/internal/syncproxy"
)

type Importer interface {
	Fetch(context.Context, string) ([]byte, string, error)
}
type Fetcher struct {
	proxies []syncproxy.Proxy
	resolve func(context.Context, string, string) ([]netip.Addr, error)
	dial    func(context.Context, string, string) (net.Conn, error)
	roots   *x509.CertPool // private synthetic TLS fixtures only; nil in production
}

func NewFetcher(proxies []syncproxy.Proxy) *Fetcher {
	return &Fetcher{proxies: append([]syncproxy.Proxy(nil), proxies...), resolve: net.DefaultResolver.LookupNetIP, dial: (&net.Dialer{Timeout: 2 * time.Second}).DialContext}
}
func imageURL(raw string) (*url.URL, error) {
	if len(raw) == 0 || len(raw) > 2048 || strings.ContainsAny(raw, "\\#") {
		return nil, ErrURL
	}
	for _, c := range raw {
		if unicode.IsControl(c) || unicode.IsSpace(c) {
			return nil, ErrURL
		}
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Opaque != "" || u.Fragment != "" || u.Host == "" || u.Port() != "" && u.Port() != "443" {
		return nil, ErrURL
	}
	if _, err := url.QueryUnescape(u.RawQuery); err != nil {
		return nil, ErrURL
	}
	host := strings.ToLower(u.Hostname())
	if len(host) > 253 || !strings.Contains(host, ".") || strings.HasSuffix(host, ".") {
		return nil, ErrURL
	}
	if _, e := netip.ParseAddr(host); e == nil {
		return nil, ErrURL
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) < 1 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return nil, ErrURL
		}
		for _, c := range label {
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
				return nil, ErrURL
			}
		}
	}
	// Reject malformed empty-port authorities as well as IP/zone/bracket forms.
	if strings.ToLower(u.Host) != host && strings.ToLower(u.Host) != host+":443" {
		return nil, ErrURL
	}
	for _, c := range u.Path {
		if unicode.IsControl(c) || c == '\\' {
			return nil, ErrURL
		}
	}
	u.Host = host
	return u, nil
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
func (f *Fetcher) Fetch(ctx context.Context, raw string) ([]byte, string, error) {
	if f == nil || len(f.proxies) == 0 {
		return nil, "", ErrImportUnavailable
	}
	u, err := imageURL(raw)
	if err != nil {
		return nil, "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	dnsCtx, dnsCancel := context.WithTimeout(ctx, 2*time.Second)
	ips, err := f.resolve(dnsCtx, "ip", u.Hostname())
	dnsCancel()
	if err != nil || len(ips) == 0 {
		return nil, "", ErrDownload
	}
	if len(ips) > 16 {
		return nil, "", ErrURL
	}
	for _, ip := range ips {
		if !publicIP(ip) {
			return nil, "", ErrURL
		}
	}
	host := u.Hostname()
	numeric := net.JoinHostPort(ips[0].Unmap().String(), "443")
	clients, err := syncproxy.NewHTTPClients(f.proxies[:1], 8*time.Second, func(*http.Request, []*http.Request) error { return ErrDownload })
	if err != nil {
		return nil, "", ErrImportUnavailable
	}
	base, ok := clients[0].Transport.(*http.Transport)
	if !ok {
		return nil, "", ErrImportUnavailable
	}
	probe := &http.Request{URL: u}
	proxy, err := base.Proxy(probe)
	if err != nil || proxy == nil {
		return nil, "", ErrImportUnavailable
	}
	t := base.Clone()
	// Own CONNECT explicitly: standard Transport shares TLSClientConfig between
	// HTTPS proxy and target. Each TLS layer here has its own verified identity.
	t.Proxy = nil
	t.DialContext = nil
	t.ForceAttemptHTTP2 = false
	t.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
	t.DisableCompression = true
	t.DisableKeepAlives = true
	t.MaxResponseHeaderBytes = 16 << 10
	t.TLSHandshakeTimeout = 2 * time.Second
	t.ResponseHeaderTimeout = 2 * time.Second
	t.DialTLSContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != numeric {
			return nil, ErrDownload
		}
		return f.tunnel(ctx, proxy, network, numeric, host)
	}
	defer t.CloseIdleConnections()
	client := *clients[0]
	client.Transport = t
	u.Host = numeric
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, "", ErrURL
	}
	r.Host = host
	r.Header.Set("Accept", "image/jpeg, image/png, image/webp")
	r.Header.Set("Accept-Encoding", "identity")
	resp, err := client.Do(r)
	if err != nil {
		return nil, "", ErrDownload
	}
	defer func() { _ = resp.Body.Close() }()
	encoding := resp.Header.Values("Content-Encoding")
	if resp.StatusCode != 200 || len(resp.Header.Values("Content-Type")) != 1 || len(encoding) > 1 || len(encoding) == 1 && strings.ToLower(strings.TrimSpace(encoding[0])) != "identity" {
		return nil, "", ErrDownload
	}
	media, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || media != "image/jpeg" && media != "image/png" && media != "image/webp" {
		return nil, "", ErrUnsupported
	}
	if resp.ContentLength > MaxInput {
		return nil, "", ErrTooLarge
	}
	b, err := ReadInput(resp.Body)
	if err != nil {
		if errors.Is(err, ErrTooLarge) {
			return nil, "", err
		}
		return nil, "", ErrDownload
	}
	return b, resp.Header.Get("Content-Type"), nil
}

type connectReader struct {
	io.Reader
	remaining int
	bounded   bool
}

func (r *connectReader) Read(p []byte) (int, error) {
	if r.bounded {
		if r.remaining <= 0 {
			return 0, ErrDownload
		}
		if len(p) > r.remaining {
			p = p[:r.remaining]
		}
	}
	n, e := r.Reader.Read(p)
	if r.bounded {
		r.remaining -= n
	}
	return n, e
}

type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) { return c.reader.Read(p) }
func phaseDeadline(ctx context.Context, c net.Conn) error {
	d := time.Now().Add(2 * time.Second)
	if cd, ok := ctx.Deadline(); ok && cd.Before(d) {
		d = cd
	}
	return c.SetDeadline(d)
}
func (f *Fetcher) tunnel(ctx context.Context, proxy *url.URL, network, numeric, host string) (net.Conn, error) {
	if proxy.Scheme != "http" && proxy.Scheme != "https" {
		return nil, ErrDownload
	}
	c, err := f.dial(ctx, network, proxy.Host)
	if err != nil {
		return nil, ErrDownload
	}
	keep := false
	defer func() {
		if !keep {
			_ = c.Close()
		}
	}()
	rawConn := c
	stop := context.AfterFunc(ctx, func() { _ = rawConn.Close() })
	defer stop()
	if err = phaseDeadline(ctx, c); err != nil {
		return nil, ErrDownload
	}
	if proxy.Scheme == "https" {
		outer := tls.Client(c, &tls.Config{MinVersion: tls.VersionTLS12, ServerName: proxy.Hostname(), RootCAs: f.roots})
		if outer.HandshakeContext(ctx) != nil {
			return nil, ErrDownload
		}
		c = outer
	}
	if phaseDeadline(ctx, c) != nil {
		return nil, ErrDownload
	}
	req := &http.Request{Method: http.MethodConnect, URL: &url.URL{Opaque: numeric}, Host: numeric, Header: make(http.Header)}
	if proxy.User != nil {
		password, _ := proxy.User.Password()
		req.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(proxy.User.Username()+":"+password)))
	}
	if req.Write(c) != nil {
		return nil, ErrDownload
	}
	budget := &connectReader{Reader: c, remaining: 16 << 10, bounded: true}
	reader := bufio.NewReader(budget)
	// CONNECT has no response body: parse headers as HEAD so closing the
	// response cannot consume tunnel bytes or an untrusted proxy error body.
	resp, err := http.ReadResponse(reader, &http.Request{Method: http.MethodHead})
	if err != nil {
		return nil, ErrDownload
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		return nil, ErrDownload
	}
	budget.bounded = false
	if phaseDeadline(ctx, c) != nil {
		return nil, ErrDownload
	}
	inner := tls.Client(&bufferedConn{Conn: c, reader: reader}, &tls.Config{MinVersion: tls.VersionTLS12, ServerName: host, RootCAs: f.roots, NextProtos: []string{"http/1.1"}})
	if inner.HandshakeContext(ctx) != nil || inner.SetDeadline(time.Time{}) != nil {
		return nil, ErrDownload
	}
	keep = true
	return inner, nil
}
