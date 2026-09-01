package httpapi

import (
	"net/http"
	"net/netip"
	"strings"

	"messeances/api/internal/observability"
)

const (
	unknownClientKey     = "unknown"
	maxForwardedForAddrs = 32
	// 4 KiB leaves ample room for 32 textual IPv6 addresses and delimiters.
	maxForwardedForBytes = 4096
)

type clientIdentifier struct {
	trustedProxyCIDRs []netip.Prefix
}

type resolvedClientIdentity struct {
	key      string
	keyClass observability.RateLimitKeyClass
}

func newClientIdentifier(trustedProxyCIDRs []netip.Prefix) clientIdentifier {
	return clientIdentifier{trustedProxyCIDRs: append([]netip.Prefix(nil), trustedProxyCIDRs...)}
}

func (i clientIdentifier) key(r *http.Request) string {
	return i.resolve(r).key
}

func (i clientIdentifier) resolve(r *http.Request) resolvedClientIdentity {
	peer, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return resolvedClientIdentity{key: unknownClientKey, keyClass: observability.RateLimitKeyUnknownPeer}
	}
	peerAddr := peer.Addr().Unmap()
	if !i.trusted(peerAddr) {
		return resolvedClientIdentity{key: peerAddr.String(), keyClass: observability.RateLimitKeyDirectClient}
	}

	forwarded, ok := forwardedForAddresses(r.Header.Values("X-Forwarded-For"))
	if !ok || len(forwarded) == 0 {
		return resolvedClientIdentity{key: peerAddr.String(), keyClass: observability.RateLimitKeyTrustedPeerFallback}
	}
	current := peerAddr
	for index := len(forwarded) - 1; index >= 0 && i.trusted(current); index-- {
		current = forwarded[index]
	}
	return resolvedClientIdentity{key: current.String(), keyClass: observability.RateLimitKeyForwardedClient}
}

func (i clientIdentifier) trusted(addr netip.Addr) bool {
	for _, prefix := range i.trustedProxyCIDRs {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func forwardedForAddresses(values []string) ([]netip.Addr, bool) {
	if !forwardedForWithinByteLimit(values) {
		return nil, false
	}
	capacity := min(len(values), maxForwardedForAddrs)
	addresses := make([]netip.Addr, 0, capacity)
	for _, value := range values {
		for start := 0; ; {
			if len(addresses) == maxForwardedForAddrs {
				return nil, false
			}
			end := len(value)
			if offset := strings.IndexByte(value[start:], ','); offset >= 0 {
				end = start + offset
			}
			member := value[start:end]
			member = strings.TrimSpace(member)
			address, err := netip.ParseAddr(member)
			if member == "" || err != nil || address.Zone() != "" {
				return nil, false
			}
			addresses = append(addresses, address.Unmap())
			if end == len(value) {
				break
			}
			start = end + 1
		}
	}
	return addresses, true
}

func forwardedForWithinByteLimit(values []string) bool {
	total := 0
	for index, value := range values {
		if index > 0 {
			if total == maxForwardedForBytes {
				return false
			}
			total++
		}
		if len(value) > maxForwardedForBytes-total {
			return false
		}
		total += len(value)
	}
	return true
}
