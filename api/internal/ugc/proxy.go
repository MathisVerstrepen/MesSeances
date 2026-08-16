package ugc

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
)

type Proxy struct{ endpoint *url.URL }

func ParseProxies(r io.Reader) ([]Proxy, error) {
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
		proxy, err := parseProxyLine(line)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy entry at line %d", lineNumber)
		}
		proxies = append(proxies, proxy)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read proxy file")
	}
	if len(proxies) == 0 {
		return nil, fmt.Errorf("proxy file contains no entries")
	}
	return proxies, nil
}

func parseProxyLine(line string) (Proxy, error) {
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
	host := parsed.Hostname()
	port := parsed.Port()
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
