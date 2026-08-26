package config

import (
	"strings"
	"testing"
	"time"
)

func environment(values map[string]string, reads *[]string) func(string) string {
	return func(name string) string {
		if reads != nil {
			*reads = append(*reads, name)
		}
		return values[name]
	}
}

func TestLoadAPIBaseDefaultsAndStrictValidation(t *testing.T) {
	base := map[string]string{"DATABASE_URL": "postgres://secret", "ADMIN_PASSWORD": "password", "ADMIN_SESSION_SECRET": "secret"}
	config, err := Load(APIBase, environment(base, nil), nil)
	if err != nil || config.Server.Port != 8080 || config.Server.Origin != "http://localhost:3000" || config.Database.URL != base["DATABASE_URL"] {
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
	} {
		t.Run(test.name, func(t *testing.T) {
			values := map[string]string{}
			for key, value := range base {
				values[key] = value
			}
			values[test.key] = test.value
			_, err := Load(APIBase, environment(values, nil), nil)
			if err == nil || strings.Contains(err.Error(), values["DATABASE_URL"]+values["ADMIN_PASSWORD"]+values["ADMIN_SESSION_SECRET"]) {
				t.Fatalf("err=%v", err)
			}
		})
	}
	for _, origin := range []string{"http://localhost:3000", "https://example.com", "https://example.com:8443", "http://127.0.0.1:8080", "http://[::1]:8080", "https://[2001:db8::1]"} {
		values := map[string]string{"DATABASE_URL": "postgres://configured", "WEB_ORIGIN": origin}
		loaded, err := Load(APIBase, environment(values, nil), nil)
		if err != nil || loaded.Server.Origin != origin {
			t.Fatalf("valid origin %q config=%+v err=%v", origin, loaded, err)
		}
	}
}

func TestLoadTimingDefaultsBoundsAndOverrides(t *testing.T) {
	config, err := Load(APISync, environment(nil, nil), nil)
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
		if _, err := Load(APISync, environment(values, nil), nil); err == nil {
			t.Fatalf("values=%v accepted", values)
		}
	}
	request, interval := 5*time.Second, time.Second
	values := map[string]string{"SYNC_REQUEST_TIMEOUT": "invalid-secret", "SYNC_KINEPOLIS_REQUEST_INTERVAL": "invalid-secret"}
	config, err = Load(KinepolisTiming, environment(values, nil), &Overrides{RequestTimeout: &request, KinepolisRequestInterval: &interval})
	if err != nil || config.Sync.RequestTimeout != request || config.Sync.KinepolisRequestInterval != interval {
		t.Fatalf("config=%+v err=%v", config, err)
	}
	config, err = Load(PatheTiming, environment(map[string]string{"SYNC_REQUEST_TIMEOUT": "invalid-secret"}, nil), &Overrides{RequestTimeout: &request})
	if err != nil || config.Sync.RequestTimeout != request || config.Sync.KinepolisRequestInterval != 0 {
		t.Fatalf("Pathé config=%+v err=%v", config, err)
	}
	config, err = Load(CGRTiming, environment(map[string]string{"SYNC_REQUEST_TIMEOUT": "invalid-secret"}, nil), &Overrides{RequestTimeout: &request})
	if err != nil || config.Sync.RequestTimeout != request || config.Sync.KinepolisRequestInterval != 0 {
		t.Fatalf("CGR config=%+v err=%v", config, err)
	}
}

func TestLoadProfilesReadOnlyOwnedVariables(t *testing.T) {
	var reads []string
	_, err := Load(UGCTiming, environment(map[string]string{"SYNC_KINEPOLIS_REQUEST_INTERVAL": "invalid", "SYNC_OPERATION_TIMEOUT": "invalid"}, &reads), nil)
	if err != nil || len(reads) != 1 || reads[0] != "SYNC_REQUEST_TIMEOUT" {
		t.Fatalf("reads=%v err=%v", reads, err)
	}
	reads = nil
	_, err = Load(PatheTiming, environment(map[string]string{"SYNC_KINEPOLIS_REQUEST_INTERVAL": "invalid", "SYNC_OPERATION_TIMEOUT": "invalid"}, &reads), nil)
	if err != nil || len(reads) != 1 || reads[0] != "SYNC_REQUEST_TIMEOUT" {
		t.Fatalf("Pathé reads=%v err=%v", reads, err)
	}
	reads = nil
	_, err = Load(CGRTiming, environment(map[string]string{"SYNC_KINEPOLIS_REQUEST_INTERVAL": "invalid", "SYNC_OPERATION_TIMEOUT": "invalid"}, &reads), nil)
	if err != nil || len(reads) != 1 || reads[0] != "SYNC_REQUEST_TIMEOUT" {
		t.Fatalf("CGR reads=%v err=%v", reads, err)
	}
	reads = nil
	_, err = Load(SyncFull, environment(map[string]string{"DATABASE_URL": "postgres://configured", "SYNC_REQUEST_TIMEOUT": "invalid", "SYNC_KINEPOLIS_REQUEST_INTERVAL": "invalid"}, &reads), nil)
	if err != nil || strings.Join(reads, ",") != "DATABASE_URL,TMDB_API_READ_ACCESS_TOKEN,SYNC_OPERATION_TIMEOUT" {
		t.Fatalf("reads=%v err=%v", reads, err)
	}
}
