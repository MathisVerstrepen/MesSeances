package kinepolis

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"golang.org/x/net/html"
	"messeances/api/internal/schedule"
)

type cinemaDefinition struct {
	providerID   string
	scheduleName string
	path         string
	detailNames  []string
}

var cinemaDefinitions = []cinemaDefinition{
	{providerID: "KFEN", scheduleName: "Kinepolis Fenouillet", path: "/cinémas/kinepolis-fenouillet/info/", detailNames: []string{"Kinepolis Fenouillet"}},
	{providerID: "KROU", scheduleName: "Kinepolis Rouen", path: "/cinemas/kinepolis-rouen/infos/", detailNames: []string{"Kinepolis Rouen"}},
	{providerID: "KMUL", scheduleName: "Kinepolis Mulhouse", path: "/cinemas/kinepolis-mulhouse/infos/", detailNames: []string{"Kinepolis Mulhouse"}},
	{providerID: "KNCY", scheduleName: "Kinepolis Nancy", path: "/cinemas/kinepolis-nancy/info/", detailNames: []string{"Kinepolis Nancy"}},
	{providerID: "KBOUR", scheduleName: "Kinepolis Bourgoin-Jallieu", path: "/cinémas/kinepolis-bourgoin-jallieu/info/", detailNames: []string{"Kinepolis Bourgoin-Jallieu"}},
	{providerID: "BRETI", scheduleName: "Kinepolis Brétigny-sur-Orge", path: "/cinemas/kinepolis-bretigny-sur-orge/infos/", detailNames: []string{"Kinepolis Brétigny-sur-Orge"}},
	{providerID: "KLOM", scheduleName: "Kinepolis Lomme", path: "/cinemas/kinepolis-lomme/infos/", detailNames: []string{"Kinepolis Lomme"}},
	{providerID: "ULONG", scheduleName: "Kinepolis Longwy", path: "/cinemas/kinepolis-longwy/infos/", detailNames: []string{"Kinepolis Longwy"}},
	{providerID: "KMETZ", scheduleName: "Kinepolis St-Julien-lès-Metz", path: "/cinemas/kinepolis-st-julien-les-metz/infos/", detailNames: []string{"Kinepolis St-Julien-lès-Metz"}},
	{providerID: "KNIM", scheduleName: "Kinepolis Nîmes", path: "/cinemas/kinepolis-nimes/infos/", detailNames: []string{"Kinepolis Nîmes"}},
	{providerID: "KSERV", scheduleName: "Kinepolis Servon", path: "/cinémas/kinepolis-servon/info/", detailNames: []string{"Kinepolis Servon"}},
	{providerID: "KTHIO", scheduleName: "Kinepolis Thionville", path: "/cinemas/kinepolis-thionville/infos/", detailNames: []string{"Kinepolis Thionville"}},
	{providerID: "WAVES", scheduleName: "Kinepolis Waves", path: "/cinémas/kinepolis-waves/info/", detailNames: []string{"Kinepolis Waves"}},
	{providerID: "MTZAM", scheduleName: "Kinepolis Amphi Quartier Muse", path: "/cinémas/kinepolis-amphi-quartier-muse/info/", detailNames: []string{"Kinepolis Amphi Quartier Muse", "Kinepolis Amphi"}},
	{providerID: "FRAMN", scheduleName: "Kinepolis Amnéville", path: "/cinémas/kinepolis-amneville/info/", detailNames: []string{"Kinepolis Amnéville"}},
	{providerID: "FRBLF", scheduleName: "Kinepolis Belfort", path: "/cinémas/kinepolis-belfort/info/", detailNames: []string{"Kinepolis Belfort"}},
	{providerID: "FRBEZ", scheduleName: "Kinepolis Béziers", path: "/cinemas/kinepolis-beziers/info/", detailNames: []string{"Kinepolis Béziers"}},
}

type cinemaAddress struct {
	address    string
	city       string
	postalCode string
}

type cinemaDetailCandidate struct {
	name    string
	address cinemaAddress
}

func resolveCinemaDefinitions(inventory []complexRecord, theaters []schedule.TheaterRecord) (map[string]cinemaDefinition, error) {
	if len(cinemaDefinitions) != 17 || len(inventory) != len(cinemaDefinitions) {
		return nil, fmt.Errorf("cinema catalog or inventory has invalid size")
	}
	definitions := make(map[string]cinemaDefinition, len(cinemaDefinitions))
	paths := make(map[string]string, len(cinemaDefinitions))
	for _, definition := range cinemaDefinitions {
		if definition.providerID == "" || collapseCinemaText(definition.scheduleName) == "" || len(definition.detailNames) == 0 {
			return nil, fmt.Errorf("cinema catalog is invalid")
		}
		if _, exists := definitions[definition.providerID]; exists {
			return nil, fmt.Errorf("cinema catalog contains duplicate ID")
		}
		target, err := parseCinemaURLSource(definition.path)
		if err != nil || target.source != definition.path || target.path != definition.path {
			return nil, fmt.Errorf("cinema catalog contains invalid path")
		}
		if owner := paths[target.path]; owner != "" {
			return nil, fmt.Errorf("cinema catalog contains duplicate path")
		}
		paths[target.path] = definition.providerID
		seenNames := make(map[string]bool, len(definition.detailNames))
		scheduleNameAllowed := false
		for _, name := range definition.detailNames {
			normalized := normalizedCinemaName(name)
			if normalized == "" || seenNames[normalized] {
				return nil, fmt.Errorf("cinema catalog contains invalid detail name")
			}
			seenNames[normalized] = true
			if sameCinemaName(name, definition.scheduleName) {
				scheduleNameAllowed = true
			}
		}
		if !scheduleNameAllowed {
			return nil, fmt.Errorf("cinema catalog excludes schedule name")
		}
		definitions[definition.providerID] = definition
	}

	inventoryIDs := make(map[string]bool, len(inventory))
	for _, complex := range inventory {
		definition, exists := definitions[complex.id]
		if !exists || inventoryIDs[complex.id] || !sameCinemaName(complex.name, definition.scheduleName) {
			return nil, fmt.Errorf("cinema schedule inventory does not match catalog")
		}
		inventoryIDs[complex.id] = true
	}
	for id := range definitions {
		if !inventoryIDs[id] {
			return nil, fmt.Errorf("cinema schedule inventory is incomplete")
		}
	}

	resolved := make(map[string]cinemaDefinition, len(theaters))
	for _, theater := range theaters {
		definition, exists := definitions[theater.ProviderID]
		if !exists || !sameCinemaName(theater.Name, definition.scheduleName) {
			return nil, fmt.Errorf("used cinema does not match catalog")
		}
		if _, duplicate := resolved[theater.ProviderID]; duplicate {
			return nil, fmt.Errorf("used cinema is duplicated")
		}
		resolved[theater.ProviderID] = definition
	}
	return resolved, nil
}

func parseCinemaDetail(body []byte, allowedNames []string) (cinemaAddress, error) {
	if len(allowedNames) == 0 {
		return cinemaAddress{}, fmt.Errorf("cinema detail identity is not configured")
	}
	root, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return cinemaAddress{}, fmt.Errorf("parse cinema detail page")
	}
	var candidates []cinemaDetailCandidate
	invalidCandidate := false
	walkCinemaHTML(root, func(node *html.Node) {
		if err != nil || node.Type != html.ElementNode || node.Data != "script" || !strings.EqualFold(strings.TrimSpace(cinemaHTMLAttr(node, "type")), "application/ld+json") {
			return
		}
		decoder := json.NewDecoder(strings.NewReader(cinemaHTMLRawText(node)))
		decoder.UseNumber()
		var structured any
		if decodeErr := decoder.Decode(&structured); decodeErr != nil {
			err = fmt.Errorf("cinema LD-JSON is malformed")
			return
		}
		var trailing any
		if decodeErr := decoder.Decode(&trailing); decodeErr != io.EOF {
			err = fmt.Errorf("cinema LD-JSON is malformed")
			return
		}
		walkCinemaJSON(structured, func(object map[string]any) {
			if !cinemaEntityType(object["@type"]) {
				return
			}
			name, ok := object["name"].(string)
			name = collapseCinemaText(name)
			if !ok || name == "" || !allowedCinemaName(name, allowedNames) {
				invalidCandidate = true
				return
			}
			addressObject, ok := object["address"].(map[string]any)
			addressType, typeOK := addressObject["@type"].(string)
			if !ok || !typeOK || addressType != "PostalAddress" {
				invalidCandidate = true
				return
			}
			address, complete := completeCinemaAddress(addressObject)
			if !complete {
				invalidCandidate = true
				return
			}
			candidates = append(candidates, cinemaDetailCandidate{name: normalizedCinemaName(name), address: address})
		})
	})
	if err != nil {
		return cinemaAddress{}, err
	}
	if invalidCandidate || len(candidates) == 0 {
		return cinemaAddress{}, fmt.Errorf("cinema detail identity or address is missing or malformed")
	}
	result := candidates[0]
	for _, candidate := range candidates[1:] {
		if candidate != result {
			return cinemaAddress{}, fmt.Errorf("cinema detail candidates conflict")
		}
	}
	return result.address, nil
}

func cinemaEntityType(value any) bool {
	switch value := value.(type) {
	case string:
		return value == "MovieTheater" || value == "Cinema"
	case []any:
		for _, item := range value {
			if item == "MovieTheater" || item == "Cinema" {
				return true
			}
		}
	}
	return false
}

func allowedCinemaName(name string, allowed []string) bool {
	for _, candidate := range allowed {
		if sameCinemaName(name, candidate) {
			return true
		}
	}
	return false
}

func sameCinemaName(left, right string) bool {
	return strings.EqualFold(collapseCinemaText(left), collapseCinemaText(right))
}

func normalizedCinemaName(value string) string {
	return strings.ToLower(collapseCinemaText(value))
}

func completeCinemaAddress(object map[string]any) (cinemaAddress, bool) {
	read := func(key string) (string, bool) {
		value, ok := object[key].(string)
		value = strings.TrimSpace(value)
		return value, ok && value != ""
	}
	address, addressOK := read("streetAddress")
	city, cityOK := read("addressLocality")
	postalCode, postalCodeOK := read("postalCode")
	return cinemaAddress{address: address, city: city, postalCode: postalCode}, addressOK && cityOK && postalCodeOK
}

func walkCinemaJSON(value any, visit func(map[string]any)) {
	switch value := value.(type) {
	case map[string]any:
		visit(value)
		for _, child := range value {
			walkCinemaJSON(child, visit)
		}
	case []any:
		for _, child := range value {
			walkCinemaJSON(child, visit)
		}
	}
}

func walkCinemaHTML(node *html.Node, visit func(*html.Node)) {
	visit(node)
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		walkCinemaHTML(child, visit)
	}
}

func cinemaHTMLAttr(node *html.Node, name string) string {
	for _, attribute := range node.Attr {
		if strings.EqualFold(attribute.Key, name) {
			return attribute.Val
		}
	}
	return ""
}

func cinemaHTMLRawText(node *html.Node) string {
	var result strings.Builder
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.TextNode {
			result.WriteString(child.Data)
		}
	}
	return result.String()
}

func collapseCinemaText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
