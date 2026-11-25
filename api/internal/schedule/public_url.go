package schedule

import (
	"net"
	"net/url"
	"regexp"
	"strings"
)

var publicHost = regexp.MustCompile(`^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)

// ValidPublicHTTPSURL accepts canonical public HTTPS URLs: a lowercase DNS
// host (no IP address, port or credentials), no whitespace or backslash, and
// spelled exactly as Go re-encodes it. Generic platform connectors use it for
// links to each cinema's own ticketing pages, which no fixed manifest lists.
func ValidPublicHTTPSURL(raw string) bool {
	if len(raw) > maxURLLength || strings.ContainsAny(raw, "\\ \t\r\n") {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Opaque != "" || u.Port() != "" || u.Host != u.Hostname() || net.ParseIP(u.Hostname()) != nil || !publicHost.MatchString(u.Host) {
		return false
	}
	return u.String() == raw
}
