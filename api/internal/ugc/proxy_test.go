package ugc

import (
	"strings"
	"testing"
)

func TestParseProxiesAndRedaction(t *testing.T) {
	secret := "synthetic-user:synthetic-password"
	proxies, err := ParseProxies(strings.NewReader("# synthetic only\nhttp://" + secret + "@127.0.0.1:8080\nproxy.example:8081:user:password\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(proxies) != 2 {
		t.Fatalf("count=%d", len(proxies))
	}
	_, err = ParseProxies(strings.NewReader("http://" + secret + "@missing-port"))
	if err == nil {
		t.Fatal("invalid proxy accepted")
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "synthetic-password") {
		t.Fatal("credential leaked")
	}
}
