package config

import (
	"strings"
	"testing"
)

func TestCinemaImageAPIBaseOwnership(t *testing.T) {
	for _, path := range []string{"", "/srv/messeances/cinema-images"} {
		cfg, e := Load(APIBase, environment(map[string]string{"DATABASE_URL": "postgres://configured", "CINEMA_IMAGE_DIR": path, "ACCOUNTS_ENABLED": "false"}))
		if e != nil || cfg.CinemaImageDir != path || cfg.Accounts.Enabled {
			t.Fatalf("config=%+v err=%v", cfg, e)
		}
	}
	seen := false
	cfg, e := Load(APISync, func(k string) string {
		if k == "CINEMA_IMAGE_DIR" {
			seen = true
			return "invalid"
		}
		return ""
	})
	if e != nil || seen || cfg.CinemaImageDir != "" {
		t.Fatalf("API sync consulted media %t %v", seen, e)
	}
}
func TestCinemaImagePathAndOverlap(t *testing.T) {
	for _, path := range []string{"relative", "/", "/srv/x\nsecret", "/srv/x\tsecret", "/srv/x\x00", "/srv/x\x7f", "/srv/\xff"} {
		_, e := loadCinemaImageDir(path, AccountsConfig{})
		if e == nil || e.Error() != "configuration error" || strings.Contains(e.Error(), path) {
			t.Fatalf("invalid root %q err=%v", path, e)
		}
	}
	for _, path := range []string{"/srv/avatar", "/srv/avatar/cinema", "/srv"} {
		if _, e := loadCinemaImageDir(path, AccountsConfig{Enabled: true, AvatarDir: "/srv/avatar"}); e == nil {
			t.Fatalf("overlap accepted %s", path)
		}
	}
	if _, e := loadCinemaImageDir("/srv/avatar-other", AccountsConfig{Enabled: true, AvatarDir: "/srv/avatar"}); e != nil {
		t.Fatal(e)
	}
	if _, e := loadCinemaImageDir("/srv/avatar", AccountsConfig{Enabled: false, AvatarDir: "/srv/avatar"}); e != nil {
		t.Fatal("disabled avatar imposes overlap", e)
	}
}
