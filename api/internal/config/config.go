package config

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Profile int

const (
	APIBase Profile = iota
	APISync
)

const (
	DefaultRequestTimeout           = 20 * time.Second
	DefaultKinepolisRequestInterval = 2 * time.Second
	DefaultOperationTimeout         = 2 * time.Minute
)

type Config struct {
	Database struct{ URL string }
	Server   struct {
		Port              int
		Origin            string
		TrustedProxyCIDRs []netip.Prefix
	}
	Admin struct {
		Password      string
		SessionSecret string
	}
	TMDB  struct{ Token string }
	Proxy struct{ Path string }
	Sync  struct {
		RequestTimeout           time.Duration
		KinepolisRequestInterval time.Duration
		OperationTimeout         time.Duration
	}
}

func Load(profile Profile, getenv func(string) string) (Config, error) {
	if getenv == nil {
		return Config{}, configurationError()
	}
	var result Config
	switch profile {
	case APIBase:
		if err := loadDatabase(&result, getenv); err != nil {
			return Config{}, err
		}
		port := getenv("PORT")
		if port == "" {
			port = "8080"
		}
		parsedPort, err := strconv.Atoi(port)
		if err != nil || parsedPort < 1 || parsedPort > 65535 || !decimal(port) {
			return Config{}, configurationError()
		}
		origin := getenv("WEB_ORIGIN")
		if origin == "" {
			origin = "http://localhost:3000"
		}
		if !validOrigin(origin) {
			return Config{}, configurationError()
		}
		trustedProxyCIDRs, err := parseTrustedProxyCIDRs(getenv("TRUSTED_PROXY_CIDRS"))
		if err != nil {
			return Config{}, err
		}
		password, secret := getenv("ADMIN_PASSWORD"), getenv("ADMIN_SESSION_SECRET")
		if (strings.TrimSpace(password) == "") != (strings.TrimSpace(secret) == "") {
			return Config{}, configurationError()
		}
		result.Server.Port, result.Server.Origin, result.Server.TrustedProxyCIDRs = parsedPort, origin, trustedProxyCIDRs
		result.Admin.Password, result.Admin.SessionSecret = password, secret
		result.TMDB.Token = strings.TrimSpace(getenv("TMDB_API_READ_ACCESS_TOKEN"))
		result.Proxy.Path = strings.TrimSpace(getenv("PROXY_FILE"))
	case APISync:
		if err := loadRequestTimeout(&result, getenv); err != nil {
			return Config{}, err
		}
		if err := loadKinepolisInterval(&result, getenv); err != nil {
			return Config{}, err
		}
		if err := loadOperationTimeout(&result, getenv); err != nil {
			return Config{}, err
		}
	default:
		return Config{}, configurationError()
	}
	return result, nil
}

func loadDatabase(result *Config, getenv func(string) string) error {
	result.Database.URL = getenv("DATABASE_URL")
	if strings.TrimSpace(result.Database.URL) == "" {
		return configurationError()
	}
	return nil
}

func parseTrustedProxyCIDRs(raw string) ([]netip.Prefix, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	members := strings.Split(raw, ",")
	prefixes := make([]netip.Prefix, 0, len(members))
	for _, member := range members {
		member = strings.TrimSpace(member)
		if member == "" {
			return nil, configurationError()
		}
		prefix, err := netip.ParsePrefix(member)
		if err != nil {
			return nil, configurationError()
		}
		if prefix.Addr().Is4In6() {
			if prefix.Bits() < 96 {
				return nil, configurationError()
			}
			prefix = netip.PrefixFrom(prefix.Addr().Unmap(), prefix.Bits()-96)
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	return prefixes, nil
}

func loadRequestTimeout(result *Config, getenv func(string) string) error {
	if err := parseDuration(getenv("SYNC_REQUEST_TIMEOUT"), DefaultRequestTimeout, &result.Sync.RequestTimeout); err != nil {
		return err
	}
	if result.Sync.RequestTimeout < 5*time.Second || result.Sync.RequestTimeout > 60*time.Second {
		return configurationError()
	}
	return nil
}

func loadKinepolisInterval(result *Config, getenv func(string) string) error {
	if err := parseDuration(getenv("SYNC_KINEPOLIS_REQUEST_INTERVAL"), DefaultKinepolisRequestInterval, &result.Sync.KinepolisRequestInterval); err != nil {
		return err
	}
	if result.Sync.KinepolisRequestInterval < time.Second {
		return configurationError()
	}
	return nil
}

func loadOperationTimeout(result *Config, getenv func(string) string) error {
	if err := parseDuration(getenv("SYNC_OPERATION_TIMEOUT"), DefaultOperationTimeout, &result.Sync.OperationTimeout); err != nil || result.Sync.OperationTimeout <= 0 {
		return configurationError()
	}
	return nil
}

func parseDuration(raw string, fallback time.Duration, destination *time.Duration) error {
	if raw == "" {
		*destination = fallback
		return nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return configurationError()
	}
	*destination = value
	return nil
}

func validOrigin(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || !literalHostname(parsed.Hostname()) || strings.Contains(raw, "#") || parsed.ForceQuery || parsed.Host == "" || parsed.User != nil || parsed.Path != "" || parsed.RawPath != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	canonical, ok := canonicalOrigin(parsed)
	return ok && raw == canonical
}

func canonicalOrigin(parsed *url.URL) (string, bool) {
	scheme := strings.ToLower(parsed.Scheme)
	hostname := parsed.Hostname()
	canonicalHost := strings.ToLower(hostname)
	if ip := net.ParseIP(hostname); ip != nil {
		canonicalHost = ip.String()
	} else if numericHostname(hostname) {
		return "", false
	}
	port := parsed.Port()
	if port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 0 || number > 65535 {
			return "", false
		}
		port = strconv.Itoa(number)
		if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
			port = ""
		}
	}
	authority := canonicalHost
	if port != "" {
		authority = net.JoinHostPort(canonicalHost, port)
	} else if strings.Contains(canonicalHost, ":") {
		authority = "[" + canonicalHost + "]"
	}
	return scheme + "://" + authority, true
}

func numericHostname(host string) bool {
	for _, character := range host {
		if (character < '0' || character > '9') && character != '.' {
			return false
		}
	}
	return true
}

func literalHostname(host string) bool {
	if host == "" || strings.Contains(host, "*") {
		return false
	}
	if net.ParseIP(host) != nil {
		return true
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') && (character < '0' || character > '9') && character != '-' {
				return false
			}
		}
	}
	return true
}

func decimal(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func configurationError() error { return fmt.Errorf("configuration error") }
