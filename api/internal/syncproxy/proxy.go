package syncproxy

import (
	"bufio"
	"context"
	cryptotls "crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/http2"
)

type Proxy struct{ endpoint *url.URL }

func Parse(r io.Reader) ([]Proxy, error) {
	scanner := bufio.NewScanner(io.LimitReader(r, 1<<20))
	scanner.Buffer(make([]byte, 1024), 64<<10)
	proxies := []Proxy{}
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		proxy, err := parseLine(line)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy entry at line %d", lineNumber)
		}
		proxies = append(proxies, proxy)
	}
	if scanner.Err() != nil {
		return nil, fmt.Errorf("read proxy file")
	}
	if len(proxies) == 0 {
		return nil, fmt.Errorf("proxy file contains no entries")
	}
	return proxies, nil
}

func parseLine(line string) (Proxy, error) {
	raw := line
	if !strings.Contains(raw, "://") {
		parts := strings.Split(raw, ":")
		if len(parts) == 4 && !strings.Contains(parts[0], "]") {
			host, port, user, password := parts[0], parts[1], parts[2], parts[3]
			if host == "" || port == "" || user == "" || password == "" {
				return Proxy{}, fmt.Errorf("invalid")
			}
			raw = "http://" + url.UserPassword(user, password).String() + "@" + net.JoinHostPort(host, port)
		} else {
			raw = "http://" + raw
		}
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "http" && parsed.Scheme != "https" || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return Proxy{}, fmt.Errorf("invalid")
	}
	host, port := parsed.Hostname(), parsed.Port()
	if host == "" || port == "" {
		return Proxy{}, fmt.Errorf("invalid")
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return Proxy{}, fmt.Errorf("invalid")
	}
	if _, _, err = net.SplitHostPort(parsed.Host); err != nil {
		return Proxy{}, fmt.Errorf("invalid")
	}
	if parsed.User != nil {
		user := parsed.User.Username()
		password, hasPassword := parsed.User.Password()
		if user == "" || !hasPassword || password == "" {
			return Proxy{}, fmt.Errorf("invalid")
		}
	}
	return Proxy{endpoint: parsed}, nil
}

func NewHTTPClients(proxies []Proxy, timeout time.Duration, redirect func(*http.Request, []*http.Request) error) ([]*http.Client, error) {
	if len(proxies) == 0 {
		return nil, fmt.Errorf("at least one proxy is required")
	}
	clients := make([]*http.Client, 0, len(proxies))
	for _, proxy := range proxies {
		if proxy.endpoint == nil || proxy.endpoint.Host == "" {
			return nil, fmt.Errorf("invalid proxy")
		}
		transport := &http.Transport{Proxy: http.ProxyURL(proxy.endpoint), ForceAttemptHTTP2: true, MaxIdleConns: 1, MaxIdleConnsPerHost: 1, IdleConnTimeout: 30 * time.Second, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: timeout}
		clients = append(clients, &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: redirect})
	}
	return clients, nil
}

func NewFingerprintHTTP2Clients(proxies []Proxy, timeout time.Duration, redirect func(*http.Request, []*http.Request) error) ([]*http.Client, error) {
	if len(proxies) == 0 {
		return nil, fmt.Errorf("at least one proxy is required")
	}
	clients := make([]*http.Client, 0, len(proxies))
	for _, proxy := range proxies {
		if proxy.endpoint == nil || proxy.endpoint.Scheme != "http" || proxy.endpoint.Host == "" {
			return nil, fmt.Errorf("invalid proxy")
		}
		transport := &http2.Transport{
			DialTLSContext:   fingerprintDialTLSContext(proxy, timeout),
			IdleConnTimeout:  30 * time.Second,
			ReadIdleTimeout:  timeout,
			PingTimeout:      timeout,
			WriteByteTimeout: timeout,
		}
		clients = append(clients, &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: redirect})
	}
	return clients, nil
}

type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(buffer []byte) (int, error) { return c.reader.Read(buffer) }

func fingerprintDialTLSContext(proxy Proxy, timeout time.Duration) func(context.Context, string, string, *cryptotls.Config) (net.Conn, error) {
	return func(ctx context.Context, network, addr string, _ *cryptotls.Config) (net.Conn, error) {
		connection, err := (&net.Dialer{Timeout: timeout}).DialContext(ctx, network, proxy.endpoint.Host)
		if err != nil {
			return nil, fmt.Errorf("proxy connection failed")
		}
		deadline := time.Now().Add(timeout)
		if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
			deadline = contextDeadline
		}
		if err := connection.SetDeadline(deadline); err != nil {
			_ = connection.Close()
			return nil, fmt.Errorf("proxy connection deadline failed")
		}
		keepConnection := false
		defer func() {
			if !keepConnection {
				_ = connection.Close()
			}
		}()
		request := &http.Request{Method: http.MethodConnect, URL: &url.URL{Opaque: addr}, Host: addr, Header: make(http.Header)}
		if proxy.endpoint.User != nil {
			password, _ := proxy.endpoint.User.Password()
			credentials := base64.StdEncoding.EncodeToString([]byte(proxy.endpoint.User.Username() + ":" + password))
			request.Header.Set("Proxy-Authorization", "Basic "+credentials)
		}
		if err := request.Write(connection); err != nil {
			return nil, fmt.Errorf("proxy CONNECT failed")
		}
		reader := bufio.NewReader(connection)
		response, err := http.ReadResponse(reader, request)
		if err != nil {
			return nil, fmt.Errorf("proxy CONNECT failed")
		}
		if response.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("proxy CONNECT rejected")
		}
		host, _, err := net.SplitHostPort(addr)
		if err != nil || host == "" {
			return nil, fmt.Errorf("TLS target is invalid")
		}
		fingerprintConnection := utls.UClient(&bufferedConn{Conn: connection, reader: reader}, &utls.Config{ServerName: host, MinVersion: utls.VersionTLS12}, utls.HelloChrome_120)
		if err := fingerprintConnection.HandshakeContext(ctx); err != nil {
			return nil, fmt.Errorf("fingerprint TLS handshake failed")
		}
		if err := connection.SetDeadline(time.Time{}); err != nil {
			return nil, fmt.Errorf("proxy connection deadline failed")
		}
		keepConnection = true
		return fingerprintConnection, nil
	}
}

func IsChallenge(body []byte) bool {
	value := strings.ToLower(string(body))
	markers := []string{"<title>datadome", "<title>captcha", "<title>attention required! | cloudflare", "captcha-delivery.com", "geo.captcha-delivery.com", "class=\"g-recaptcha", "id=\"captcha", "action=\"/captcha", "cf-chl-", "cloudflare ray id", "challenge-platform", "/cdn-cgi/challenge", "id=\"challenge-form\""}
	for _, marker := range markers {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}
