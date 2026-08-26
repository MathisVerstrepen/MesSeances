package kinepolis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/schedule"
)

func TestCinemaDefinitionCatalog(t *testing.T) {
	expected := []cinemaDefinition{
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
	if len(cinemaDefinitions) != 17 || len(cinemaDefinitions) != len(expected) {
		t.Fatalf("catalog count=%d", len(cinemaDefinitions))
	}
	for index := range expected {
		actual, want := cinemaDefinitions[index], expected[index]
		if actual.providerID != want.providerID || actual.scheduleName != want.scheduleName || actual.path != want.path || strings.Join(actual.detailNames, "|") != strings.Join(want.detailNames, "|") {
			t.Fatalf("row %d=%+v want=%+v", index, actual, want)
		}
		if _, err := parseCinemaURLSource(actual.path); err != nil {
			t.Fatalf("row %d path: %v", index, err)
		}
	}
}

func TestResolveCinemaDefinitionsValidatesCompleteInventoryBeforeIO(t *testing.T) {
	inventory := catalogInventory()
	for left, right := 0, len(inventory)-1; left < right; left, right = left+1, right-1 {
		inventory[left], inventory[right] = inventory[right], inventory[left]
	}
	theaters := []schedule.TheaterRecord{{ProviderID: "WAVES", Name: " KINEPOLIS   WAVES "}, {ProviderID: "KLOM", Name: "Kinepolis Lomme"}}
	resolved, err := resolveCinemaDefinitions(inventory, theaters)
	if err != nil || len(resolved) != 2 || resolved["WAVES"].path != "/cinémas/kinepolis-waves/info/" || resolved["KLOM"].path != "/cinemas/kinepolis-lomme/infos/" {
		t.Fatalf("resolved=%+v err=%v", resolved, err)
	}

	tests := []struct {
		name      string
		inventory []complexRecord
		theaters  []schedule.TheaterRecord
	}{
		{name: "missing", inventory: append([]complexRecord(nil), catalogInventory()[:16]...), theaters: theaters},
		{name: "unknown", inventory: replaceInventory(catalogInventory(), 2, complexRecord{id: "UNKNOWN", name: "Kinepolis Unknown"}), theaters: theaters},
		{name: "duplicate", inventory: replaceInventory(catalogInventory(), 2, catalogInventory()[1]), theaters: theaters},
		{name: "renamed", inventory: replaceInventory(catalogInventory(), 2, complexRecord{id: "KMUL", name: "Kinepolis Mulhouse Renamed"}), theaters: theaters},
		{name: "used unknown", inventory: catalogInventory(), theaters: []schedule.TheaterRecord{{ProviderID: "UNKNOWN", Name: "Kinepolis Unknown"}}},
		{name: "used duplicate", inventory: catalogInventory(), theaters: []schedule.TheaterRecord{{ProviderID: "KLOM", Name: "Kinepolis Lomme"}, {ProviderID: "KLOM", Name: "Kinepolis Lomme"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if resolved, err := resolveCinemaDefinitions(test.inventory, test.theaters); err == nil {
				t.Fatalf("resolved=%+v", resolved)
			}
		})
	}
}

func TestResolveCinemaDefinitionsRejectsInvalidCatalog(t *testing.T) {
	original := cinemaDefinitions
	t.Cleanup(func() { cinemaDefinitions = original })
	tests := []struct {
		name   string
		mutate func([]cinemaDefinition) []cinemaDefinition
	}{
		{name: "wrong count", mutate: func(value []cinemaDefinition) []cinemaDefinition { return value[:16] }},
		{name: "duplicate ID", mutate: func(value []cinemaDefinition) []cinemaDefinition {
			value[1].providerID = value[0].providerID
			return value
		}},
		{name: "duplicate path", mutate: func(value []cinemaDefinition) []cinemaDefinition { value[1].path = value[0].path; return value }},
		{name: "invalid path", mutate: func(value []cinemaDefinition) []cinemaDefinition { value[1].path = "/cinemas/guess/"; return value }},
		{name: "empty name", mutate: func(value []cinemaDefinition) []cinemaDefinition { value[1].scheduleName = " "; return value }},
		{name: "duplicate detail name", mutate: func(value []cinemaDefinition) []cinemaDefinition {
			value[1].detailNames = []string{"Kinepolis Rouen", " KINEPOLIS  ROUEN "}
			return value
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			copyCatalog := append([]cinemaDefinition(nil), original...)
			for index := range copyCatalog {
				copyCatalog[index].detailNames = append([]string(nil), copyCatalog[index].detailNames...)
			}
			cinemaDefinitions = test.mutate(copyCatalog)
			if _, err := resolveCinemaDefinitions(catalogInventory(), nil); err == nil {
				t.Fatal("invalid catalog accepted")
			}
			cinemaDefinitions = original
		})
	}
}

func TestParseCinemaDetailIdentityAndPostalAddress(t *testing.T) {
	want := cinemaAddress{address: "1 rue du Cinéma", city: "Lille", postalCode: "59000"}
	fixtureAddress, err := parseCinemaDetail(namedFixture(t, "cinema-detail.html"), []string{"Kinepolis Lomme"})
	if err != nil || fixtureAddress != want {
		t.Fatalf("fixture address=%+v err=%v", fixtureAddress, err)
	}
	tests := []struct {
		name, body string
		allowed    []string
	}{
		{name: "direct movie theater", allowed: []string{"Kinepolis Lomme"}, body: ldScript(theaterJSON("MovieTheater", " KINEPOLIS   LOMME ", want))},
		{name: "cinema in graph", allowed: []string{"Kinepolis Lomme"}, body: ldScript(`{"@graph":[` + theaterJSON("Cinema", "Kinepolis Lomme", want) + `]}`)},
		{name: "web page main entity", allowed: []string{"Kinepolis Lomme"}, body: ldScript(`{"@type":"WebPage","mainEntity":` + theaterJSON("MovieTheater", "Kinepolis Lomme", want) + `}`)},
		{name: "multiple blocks", allowed: []string{"Kinepolis Lomme"}, body: ldScript(`{"@type":"WebPage"}`) + ldScript(theaterJSON("MovieTheater", "Kinepolis Lomme", want))},
		{name: "type array", allowed: []string{"Kinepolis Lomme"}, body: ldScript(strings.Replace(theaterJSON("MovieTheater", "Kinepolis Lomme", want), `"MovieTheater"`, `["Thing","MovieTheater"]`, 1))},
		{name: "MTZAM alias", allowed: []string{"Kinepolis Amphi Quartier Muse", "Kinepolis Amphi"}, body: ldScript(theaterJSON("MovieTheater", "Kinepolis Amphi", want))},
		{name: "identical duplicate", allowed: []string{"Kinepolis Lomme"}, body: ldScript(`[` + theaterJSON("MovieTheater", "Kinepolis Lomme", want) + `,` + theaterJSON("MovieTheater", " kinepolis lomme ", want) + `]`)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			address, err := parseCinemaDetail([]byte(test.body), test.allowed)
			if err != nil || address != want {
				t.Fatalf("address=%+v err=%v", address, err)
			}
		})
	}
}

func TestParseCinemaDetailFailsClosed(t *testing.T) {
	validAddress := cinemaAddress{address: "1 rue", city: "Lille", postalCode: "59000"}
	valid := theaterJSON("MovieTheater", "Kinepolis Lomme", validAddress)
	tests := map[string]string{
		"no structured theater":          `<p>1 rue, Lille</p>`,
		"malformed JSON":                 ldScript(`{`),
		"valid plus malformed":           ldScript(valid) + ldScript(`{`),
		"wrong identity":                 ldScript(theaterJSON("MovieTheater", "Kinepolis Other", validAddress)),
		"missing name":                   ldScript(strings.Replace(valid, `"name":"Kinepolis Lomme"`, `"other":"Kinepolis Lomme"`, 1)),
		"numeric name":                   ldScript(strings.Replace(valid, `"Kinepolis Lomme"`, `42`, 1)),
		"unrelated address":              ldScript(`{"@type":"WebPage","address":{"@type":"PostalAddress","streetAddress":"1 rue","addressLocality":"Lille","postalCode":"59000"}}`),
		"missing address type":           ldScript(strings.Replace(valid, `"@type":"PostalAddress",`, ``, 1)),
		"case variant address type":      ldScript(strings.Replace(valid, `PostalAddress`, `postaladdress`, 1)),
		"array address type":             ldScript(strings.Replace(valid, `"PostalAddress"`, `["PostalAddress"]`, 1)),
		"incomplete address":             ldScript(strings.Replace(valid, `,"postalCode":"59000"`, ``, 1)),
		"wrong theater type":             ldScript(strings.Replace(valid, `MovieTheater`, `Place`, 1)),
		"conflicting address":            ldScript(`[` + valid + `,` + theaterJSON("MovieTheater", "Kinepolis Lomme", cinemaAddress{address: "2 rue", city: "Lille", postalCode: "59000"}) + `]`),
		"conflicting allowed identities": ldScript(`[` + theaterJSON("MovieTheater", "Kinepolis Amphi", validAddress) + `,` + theaterJSON("MovieTheater", "Kinepolis Amphi Quartier Muse", validAddress) + `]`),
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			allowed := []string{"Kinepolis Lomme"}
			if name == "conflicting allowed identities" {
				allowed = []string{"Kinepolis Amphi", "Kinepolis Amphi Quartier Muse"}
			}
			if address, err := parseCinemaDetail([]byte(body), allowed); err == nil {
				t.Fatalf("address=%+v", address)
			}
		})
	}
}

func TestSyncFetchesUsedCinemaDetailsSequentiallyAndOverwritesCity(t *testing.T) {
	const lommePath = "/cinemas/kinepolis-lomme/infos/"
	const wavesPath = "/cinémas/kinepolis-waves/info/"
	fetcher := &staticFetcher{body: fixture(t), details: map[string][]byte{
		lommePath: cinemaDetailBody(t, "Kinepolis Lomme", "10 avenue synthétique", "Lille", "59000"),
		wavesPath: cinemaDetailBody(t, "Kinepolis Waves", "1 allée synthétique", "Moulins-lès-Metz", "57160"),
	}}
	data, summary, err := Sync(context.Background(), fetcher, SyncOptions{From: "2026-08-15", Now: time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)})
	if err != nil || summary.Cinemas != 2 || summary.Showtimes != 2 || strings.Join(fetcher.detailCalls, "|") != lommePath+"|"+wavesPath {
		t.Fatalf("summary=%+v calls=%v err=%v", summary, fetcher.detailCalls, err)
	}
	if data.Theaters[0].Address != "10 avenue synthétique" || data.Theaters[0].City != "Lille" || data.Theaters[0].PostalCode != "59000" || data.Theaters[1].City != "Moulins-lès-Metz" {
		t.Fatalf("theaters=%+v", data.Theaters)
	}
}

func TestSyncFailuresAreTypedAndAllOrNothing(t *testing.T) {
	validDetails := map[string][]byte{
		"/cinemas/kinepolis-lomme/infos/": cinemaDetailBody(t, "Kinepolis Lomme", "1 rue", "Lille", "59000"),
		"/cinémas/kinepolis-waves/info/":  cinemaDetailBody(t, "Kinepolis Waves", "2 rue", "Metz", "57000"),
	}
	tests := []struct {
		name         string
		body         []byte
		details      map[string][]byte
		detailErr    error
		wantOp       Operation
		wantCategory ErrorCategory
		wantCalls    int
		wantDataset  bool
	}{
		{name: "raw inventory", body: []byte(strings.Replace(string(fixture(t)), `,{"id":"FRBEZ","name":"Kinepolis Béziers"}`, ``, 1)), details: validDetails, wantOp: OperationSchedule, wantCategory: CategoryInvalidPayload},
		{name: "catalog drift", body: []byte(strings.Replace(string(fixture(t)), "Kinepolis Mulhouse", "Kinepolis Mulhouse Renamed", 1)), details: validDetails, wantOp: OperationSchedule, wantCategory: CategoryInvalidPayload},
		{name: "detail fetch", body: fixture(t), details: validDetails, detailErr: errors.New("synthetic fetch cause"), wantOp: OperationCinema, wantCategory: CategoryTransport, wantCalls: 1},
		{name: "wrong detail identity", body: fixture(t), details: map[string][]byte{"/cinemas/kinepolis-lomme/infos/": cinemaDetailBody(t, "Kinepolis Other", "1 rue", "Lille", "59000")}, wantOp: OperationCinema, wantCategory: CategoryInvalidPayload, wantCalls: 1},
		{name: "final validation", body: fixture(t), details: map[string][]byte{"/cinemas/kinepolis-lomme/infos/": cinemaDetailBody(t, "Kinepolis Lomme", strings.Repeat("x", 2049), "Lille", "59000"), "/cinémas/kinepolis-waves/info/": cinemaDetailBody(t, "Kinepolis Waves", "2 rue", "Metz", "57000")}, wantCalls: 2, wantDataset: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fetcher := &staticFetcher{body: test.body, details: test.details, detailErr: test.detailErr}
			data, summary, err := Sync(context.Background(), fetcher, SyncOptions{From: "2026-08-15", Now: time.Now()})
			if err == nil || data.Provider != "" || summary != (SyncSummary{}) || len(fetcher.detailCalls) != test.wantCalls {
				t.Fatalf("data=%+v summary=%+v calls=%v err=%v", data, summary, fetcher.detailCalls, err)
			}
			var requestErr *RequestError
			if test.wantDataset {
				if !errors.Is(err, schedule.ErrDatasetValidation) || errors.As(err, &requestErr) {
					t.Fatalf("final validation classification=%v", err)
				}
			} else if !errors.As(err, &requestErr) || requestErr.Operation != test.wantOp || requestErr.Category != test.wantCategory {
				t.Fatalf("request error=%+v err=%v", requestErr, err)
			}
		})
	}
}

func TestSyncBaseDatasetValidationRemainsTypedScheduleFailure(t *testing.T) {
	fetcher := &staticFetcher{body: fixture(t)}
	data, summary, err := Sync(context.Background(), fetcher, SyncOptions{From: "2026-08-15"})
	var requestErr *RequestError
	if data.Provider != "" || summary != (SyncSummary{}) || len(fetcher.detailCalls) != 0 || !errors.As(err, &requestErr) || requestErr.Operation != OperationSchedule || requestErr.Category != CategoryInvalidPayload || !errors.Is(err, schedule.ErrDatasetValidation) {
		t.Fatalf("data=%+v summary=%+v calls=%v requestErr=%+v err=%v", data, summary, fetcher.detailCalls, requestErr, err)
	}
}

func catalogInventory() []complexRecord {
	result := make([]complexRecord, len(cinemaDefinitions))
	for index, definition := range cinemaDefinitions {
		result[index] = complexRecord{id: definition.providerID, name: definition.scheduleName}
	}
	return result
}

func replaceInventory(source []complexRecord, index int, replacement complexRecord) []complexRecord {
	result := append([]complexRecord(nil), source...)
	result[index] = replacement
	return result
}

func ldScript(payload string) string {
	return `<script type="application/ld+json">` + payload + `</script>`
}

func theaterJSON(theaterType, name string, address cinemaAddress) string {
	payload := map[string]any{
		"@type": theaterType,
		"name":  name,
		"address": map[string]any{
			"@type": "PostalAddress", "streetAddress": address.address, "addressLocality": address.city, "postalCode": address.postalCode,
		},
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		panic(fmt.Sprintf("marshal synthetic cinema detail: %v", err))
	}
	return string(encoded)
}

func cinemaDetailBody(t *testing.T, name, address, city, postalCode string) []byte {
	t.Helper()
	return []byte(ldScript(theaterJSON("MovieTheater", name, cinemaAddress{address: address, city: city, postalCode: postalCode})))
}
