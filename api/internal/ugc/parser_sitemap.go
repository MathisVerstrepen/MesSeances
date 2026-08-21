package ugc

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

func ParseSitemap(r io.Reader) ([]string, error) {
	decoder := xml.NewDecoder(io.LimitReader(r, 8<<20))
	seen := map[string]bool{}
	ids := []string{}
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse sitemap: %w", err)
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "loc" {
			continue
		}
		var raw string
		if err := decoder.DecodeElement(&raw, &start); err != nil {
			return nil, fmt.Errorf("parse sitemap location: %w", err)
		}
		parsed, err := url.Parse(strings.TrimSpace(raw))
		if err != nil || parsed.Scheme != "https" || strings.ToLower(parsed.Host) != "www.ugc.fr" || parsed.User != nil || parsed.Fragment != "" || parsed.Path != "/cinema.html" {
			continue
		}
		values := parsed.Query()
		id := values.Get("id")
		if len(values) != 1 || len(values["id"]) != 1 || id == "" {
			continue
		}
		number, err := strconv.ParseUint(id, 10, 64)
		if err != nil || number == 0 {
			continue
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { a, _ := strconv.Atoi(ids[i]); b, _ := strconv.Atoi(ids[j]); return a < b })
	if len(ids) == 0 {
		return nil, fmt.Errorf("sitemap contains no cinemas")
	}
	return ids, nil
}
