package ugc

import (
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"

	"golang.org/x/net/html"
)

type Cinema struct {
	ProviderID      string
	Name            string
	Address         string
	City            string
	PostalCode      string
	AdvertisedDates []string
}

var localityPattern = regexp.MustCompile(`(?i)cin[ée]ma\s+[àa]\s+(.+?)\s*\(([0-9]{5})\)`)
var addressLocalityPattern = regexp.MustCompile(`(?:^| )([0-9]{5}) (\p{L}[\p{L}\p{M}\p{N} .,'’()/-]*)$`)
var postalCodePattern = regexp.MustCompile(`^[0-9]{5}$`)
var dateIDPattern = regexp.MustCompile(`^nav_date_(\d{4}-\d{2}-\d{2})$`)

type locality struct{ city, postalCode string }

func titleLocality(title string) (locality, bool) {
	match := localityPattern.FindStringSubmatch(title)
	if len(match) != 3 {
		return locality{}, false
	}
	return locality{city: collapse(match[1]), postalCode: match[2]}, true
}

func addressLocality(address string) (locality, bool, error) {
	address = collapse(address)
	postalCodes := 0
	for _, field := range strings.Fields(address) {
		if postalCodePattern.MatchString(field) {
			postalCodes++
		}
	}
	if postalCodes == 0 {
		return locality{}, false, nil
	}
	if postalCodes != 1 {
		return locality{}, false, fmt.Errorf("cinema locality malformed")
	}
	match := addressLocalityPattern.FindStringSubmatch(address)
	if len(match) != 3 {
		return locality{}, false, fmt.Errorf("cinema locality malformed")
	}
	return locality{city: collapse(match[2]), postalCode: match[1]}, true, nil
}

func ParseCinema(r io.Reader, expectedID string) (Cinema, error) {
	root, err := html.Parse(io.LimitReader(r, 8<<20))
	if err != nil {
		return Cinema{}, fmt.Errorf("parse cinema page: %w", err)
	}
	var ids, names, addresses []string
	dates := map[string]bool{}
	title := ""
	walk(root, func(node *html.Node) {
		if node.Type != html.ElementNode {
			return
		}
		if node.Data == "title" {
			title = collapse(text(node))
		}
		if node.Data == "input" && strings.EqualFold(attr(node, "name"), "cinemaId") {
			ids = append(ids, strings.TrimSpace(attr(node, "value")))
		}
		if match := dateIDPattern.FindStringSubmatch(attr(node, "id")); len(match) == 2 {
			dates[match[1]] = true
		}
		if node.Data == "h1" && hasAncestorID(node, "cinema-heading") {
			names = append(names, collapse(text(node)))
		}
		if node.Data == "p" && hasClass(node, "address") && hasAncestorID(node, "cinema-heading") {
			addresses = append(addresses, collapse(text(node)))
		}
	})
	if !singleEqual(ids, expectedID) {
		return Cinema{}, fmt.Errorf("cinema identity missing or conflicting")
	}
	name, ok := singleNonempty(names)
	if !ok {
		return Cinema{}, fmt.Errorf("cinema name missing or conflicting")
	}
	address, ok := singleNonempty(addresses)
	if !ok {
		return Cinema{}, fmt.Errorf("cinema address missing or conflicting")
	}
	titleLocality, hasTitleLocality := titleLocality(title)
	addressLocality, hasAddressLocality, err := addressLocality(address)
	if err != nil {
		return Cinema{}, err
	}
	if !hasTitleLocality && !hasAddressLocality {
		return Cinema{}, fmt.Errorf("cinema locality missing")
	}
	parsedLocality := addressLocality
	if !hasAddressLocality {
		parsedLocality = titleLocality
	} else if hasTitleLocality && titleLocality.postalCode != addressLocality.postalCode {
		return Cinema{}, fmt.Errorf("cinema locality conflicting")
	}
	result := Cinema{ProviderID: expectedID, Name: name, Address: address, City: parsedLocality.city, PostalCode: parsedLocality.postalCode}
	for date := range dates {
		if _, err := time.Parse("2006-01-02", date); err != nil {
			return Cinema{}, fmt.Errorf("invalid advertised date")
		}
		result.AdvertisedDates = append(result.AdvertisedDates, date)
	}
	sort.Strings(result.AdvertisedDates)
	if len(result.AdvertisedDates) == 0 {
		return Cinema{}, fmt.Errorf("cinema advertised dates missing")
	}
	return result, nil
}
