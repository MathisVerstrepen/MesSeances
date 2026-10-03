package config

import (
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

func loadCinemaImageDir(path string, accounts AccountsConfig) (string, error) {
	if path == "" {
		return "", nil
	}
	if !utf8.ValidString(path) || !filepath.IsAbs(path) || filepath.Clean(path) == "/" {
		return "", configurationError()
	}
	for _, c := range path {
		if unicode.IsControl(c) {
			return "", configurationError()
		}
	}
	path = filepath.Clean(path)
	if accounts.Enabled && overlappingMediaDirs(path, accounts.AvatarDir) {
		return "", configurationError()
	}
	return path, nil
}
func overlappingMediaDirs(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	return a == b || strings.HasPrefix(a, b+string(filepath.Separator)) || strings.HasPrefix(b, a+string(filepath.Separator))
}
