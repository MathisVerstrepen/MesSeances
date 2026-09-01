package config

import (
	"net/netip"
	"strings"
	"testing"
	"time"
)

func environment(values map[string]string) func(string) string {
	return func(name string) string {
		return values[name]
	}
}

func TestLoadAPIBaseDefaultsAndStrictValidation(t *testing.T) {
	base := map[string]string{"DATABASE_URL": "postgres://secret", "ADMIN_PASSWORD": "password", "ADMIN_SESSION_SECRET": "secret"}
	config, err := Load(APIBase, environment(base))
	if err != nil || config.Server.Port != 8080 || config.Server.Origin != "http://localhost:3000" || config.Server.TrustedProxyCIDRs != nil || config.Database.URL != base["DATABASE_URL"] {
		t.Fatalf("config=%+v err=%v", config, err)
	}
	for _, test := range []struct{ name, key, value string }{
		{"missing database", "DATABASE_URL", " "},
		{"nonnumeric port", "PORT", "8080x"},
		{"port zero", "PORT", "0"},
		{"origin path", "WEB_ORIGIN", "https://example.com/path"},
		{"origin userinfo", "WEB_ORIGIN", "https://user@example.com"},
		{"origin empty query", "WEB_ORIGIN", "https://example.com?"},
		{"origin empty fragment", "WEB_ORIGIN", "https://example.com#"},
		{"origin wildcard", "WEB_ORIGIN", "https://*.example.com"},
		{"origin nonliteral host", "WEB_ORIGIN", "https://{tenant}.example.com"},
		{"origin uppercase host", "WEB_ORIGIN", "https://EXAMPLE.com"},
		{"origin uppercase scheme", "WEB_ORIGIN", "HTTPS://example.com"},
		{"origin explicit https default port", "WEB_ORIGIN", "https://example.com:443"},
		{"origin explicit http default port", "WEB_ORIGIN", "http://example.com:80"},
		{"origin padded port", "WEB_ORIGIN", "https://example.com:08443"},
		{"origin noncanonical IPv4", "WEB_ORIGIN", "http://127.000.0.1:8080"},
		{"password only", "ADMIN_SESSION_SECRET", ""},
		{"proxy empty member", "TRUSTED_PROXY_CIDRS", "10.0.0.0/8,,192.0.2.0/24"},
		{"proxy trailing empty member", "TRUSTED_PROXY_CIDRS", "10.0.0.0/8,"},
		{"proxy address without prefix", "TRUSTED_PROXY_CIDRS", "10.0.0.1"},
		{"proxy invalid prefix", "TRUSTED_PROXY_CIDRS", "synthetic-secret"},
		{"proxy broad mapped prefix", "TRUSTED_PROXY_CIDRS", "::ffff:0:0/95"},
		{"internal secret short", "INTERNAL_API_SHARED_SECRET", strings.Repeat("a", 63)},
		{"internal secret long", "INTERNAL_API_SHARED_SECRET", strings.Repeat("a", 65)},
		{"internal secret uppercase", "INTERNAL_API_SHARED_SECRET", strings.Repeat("A", 64)},
		{"internal secret nonhex", "INTERNAL_API_SHARED_SECRET", strings.Repeat("g", 64)},
		{"internal secret whitespace", "INTERNAL_API_SHARED_SECRET", " " + strings.Repeat("a", 64)},
	} {
		t.Run(test.name, func(t *testing.T) {
			values := map[string]string{}
			for key, value := range base {
				values[key] = value
			}
			values[test.key] = test.value
			_, err := Load(APIBase, environment(values))
			if err == nil || strings.Contains(err.Error(), values["DATABASE_URL"]+values["ADMIN_PASSWORD"]+values["ADMIN_SESSION_SECRET"]) {
				t.Fatalf("err=%v", err)
			}
		})
	}
	for _, origin := range []string{"http://localhost:3000", "https://example.com", "https://example.com:8443", "http://127.0.0.1:8080", "http://[::1]:8080", "https://[2001:db8::1]"} {
		values := map[string]string{"DATABASE_URL": "postgres://configured", "WEB_ORIGIN": origin}
		loaded, err := Load(APIBase, environment(values))
		if err != nil || loaded.Server.Origin != origin {
			t.Fatalf("valid origin %q config=%+v err=%v", origin, loaded, err)
		}
	}
}

func TestLoadAPIBaseOptionalInternalSharedSecret(t *testing.T) {
	valid := strings.Repeat("0123456789abcdef", 4)
	for _, secret := range []string{"", valid} {
		loaded, err := Load(APIBase, environment(map[string]string{
			"DATABASE_URL":               "postgres://configured",
			"INTERNAL_API_SHARED_SECRET": secret,
		}))
		if err != nil || loaded.Internal.SharedSecret != secret {
			t.Fatalf("secret configured=%t stored=%q err=%v", secret != "", loaded.Internal.SharedSecret, err)
		}
	}
}

func TestLoadAPIBaseParsesAndNormalizesTrustedProxyCIDRs(t *testing.T) {
	values := map[string]string{
		"DATABASE_URL":        "postgres://configured",
		"TRUSTED_PROXY_CIDRS": " 10.1.2.3/8, 2001:db8:1::12/48 , ::ffff:192.0.2.1/120 ",
	}
	loaded, err := Load(APIBase, environment(values))
	want := []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("2001:db8:1::/48"),
		netip.MustParsePrefix("192.0.2.0/24"),
	}
	if err != nil || len(loaded.Server.TrustedProxyCIDRs) != len(want) {
		t.Fatalf("prefixes=%v err=%v", loaded.Server.TrustedProxyCIDRs, err)
	}
	for index := range want {
		if loaded.Server.TrustedProxyCIDRs[index] != want[index] {
			t.Fatalf("prefix[%d]=%s want=%s", index, loaded.Server.TrustedProxyCIDRs[index], want[index])
		}
	}
}

func TestLoadAPISyncTimingDefaultsAndBounds(t *testing.T) {
	config, err := Load(APISync, environment(nil))
	if err != nil || config.Sync.RequestTimeout != 20*time.Second || config.Sync.KinepolisRequestInterval != 2*time.Second || config.Sync.OperationTimeout != 2*time.Minute {
		t.Fatalf("config=%+v err=%v", config, err)
	}
	for _, values := range []map[string]string{
		{"SYNC_REQUEST_TIMEOUT": "4999ms"},
		{"SYNC_REQUEST_TIMEOUT": "61s"},
		{"SYNC_KINEPOLIS_REQUEST_INTERVAL": "999ms"},
		{"SYNC_OPERATION_TIMEOUT": "0s"},
		{"SYNC_OPERATION_TIMEOUT": "secret-duration"},
	} {
		if _, err := Load(APISync, environment(values)); err == nil {
			t.Fatalf("values=%v accepted", values)
		}
	}
}
