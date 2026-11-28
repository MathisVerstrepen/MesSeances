package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"

	"messeances/api/internal/cinemaimage"
	"messeances/api/internal/schedule"
	"messeances/api/internal/trailer"
)

// Public routes deliberately left out of public_openapi.yaml, with the reason.
var undocumentedPublicRoutes = map[string]string{
	"get /api/v1/statistics":                                               "feeds the statistics page; contract not stabilised",
	"get /api/v1/statistics/history":                                       "feeds the statistics page; contract not stabilised",
	"get /api/v1/statistics/history/options":                               "feeds the statistics page; contract not stabilised",
	"get /api/v1/theaters/{slug}/activity":                                 "feeds the cinema activity tab; contract not stabilised",
	"post /api/v1/theaters/activity-preview":                               "feeds the account activity preview",
	"get /api/v1/theaters/{provider}/{providerTheaterID}/image/{revision}": "binary image addressed by URLs inside payloads",
	"get /api/v1/sitemap-data":                                             "feeds the sitemaps; very large snapshot",
	"post /api/v1/shortlinks":                                              "restricted to the site origin",
	"get /api/v1/shortlinks/{code}":                                        "resolves share links for the site",
	"post /api/v1/notifications/unsubscribe":                               "one-click unsubscribe from account notification emails",
}

// Private families are covered by their own contracts or never public.
var privateRoutePrefixes = []string{"/api/v1/admin", "/api/v1/account", "/api/v1/auth", "/api/v1/internal/"}

// Component schemas and the Go types whose JSON encoding they describe.
var publicOpenAPISchemaTypes = map[string]reflect.Type{
	"Error":                    reflect.TypeFor[errorResponse](),
	"ErrorDetail":              reflect.TypeFor[apiError](),
	"City":                     reflect.TypeFor[schedule.City](),
	"CityTheater":              reflect.TypeFor[schedule.CityTheater](),
	"CityInventoryItem":        reflect.TypeFor[schedule.CityInventoryItem](),
	"CityInventory":            reflect.TypeFor[schedule.CityInventory](),
	"DiscoveryWindow":          reflect.TypeFor[schedule.DiscoveryWindow](),
	"CityTheaterProgramme":     reflect.TypeFor[schedule.CityTheaterProgramme](),
	"CityDiscovery":            reflect.TypeFor[schedule.CityDiscovery](),
	"CityDetail":               reflect.TypeFor[schedule.CityDetail](),
	"PublicImage":              reflect.TypeFor[cinemaimage.PublicImage](),
	"Theater":                  reflect.TypeFor[schedule.Theater](),
	"TheaterWithImage":         reflect.TypeFor[publicTheater](),
	"TheaterCatalogItem":       reflect.TypeFor[publicTheaterCatalogItem](),
	"Movie":                    reflect.TypeFor[schedule.Movie](),
	"Trailer":                  reflect.TypeFor[trailer.Media](),
	"MovieCatalogItem":         reflect.TypeFor[schedule.MovieCatalogItem](),
	"MovieScreeningWindow":     reflect.TypeFor[schedule.MovieScreeningWindow](),
	"MovieCatalog":             reflect.TypeFor[schedule.MovieCatalog](),
	"UpcomingWindow":           reflect.TypeFor[schedule.Window](),
	"UpcomingMovies":           reflect.TypeFor[schedule.UpcomingMoviesResponse](),
	"Showtime":                 reflect.TypeFor[schedule.Showtime](),
	"TimelineShowtime":         reflect.TypeFor[schedule.TimelineShowtime](),
	"DiscoveryTheater":         reflect.TypeFor[schedule.DiscoveryTheater](),
	"TheaterDiscovery":         reflect.TypeFor[schedule.TheaterDiscovery](),
	"TheaterShowtimes":         reflect.TypeFor[publicTheaterShowtimes](),
	"TimelineTheater":          reflect.TypeFor[schedule.TimelineTheater](),
	"Timeline":                 reflect.TypeFor[schedule.Timeline](),
	"DiscoveryCity":            reflect.TypeFor[schedule.DiscoveryCity](),
	"MovieDiscovery":           reflect.TypeFor[schedule.MovieDiscovery](),
	"MovieTheaterShowtimes":    reflect.TypeFor[schedule.MovieTheaterShowtimes](),
	"MovieShowtimesPagination": reflect.TypeFor[schedule.MovieShowtimesPagination](),
	"MovieSchedule":            reflect.TypeFor[schedule.MovieSchedule](),
	"TheaterSummary":           reflect.TypeFor[schedule.TheaterSummary](),
	"SlotResult":               reflect.TypeFor[schedule.SlotResult](),
}

// Successful response body of each operation, as written by its handler.
var publicOpenAPIResponseTypes = map[string]reflect.Type{
	"listCities":          reflect.TypeFor[schedule.CityInventory](),
	"getCity":             reflect.TypeFor[schedule.CityDetail](),
	"listTheaters":        reflect.TypeFor[[]publicTheaterCatalogItem](),
	"getTheaterShowtimes": reflect.TypeFor[publicTheaterShowtimes](),
	"getTimeline":         reflect.TypeFor[schedule.Timeline](),
	"listMovies":          reflect.TypeFor[schedule.MovieCatalog](),
	"listUpcomingMovies":  reflect.TypeFor[schedule.UpcomingMoviesResponse](),
	"getMovieShowtimes":   reflect.TypeFor[schedule.MovieSchedule](),
	"searchSlot":          reflect.TypeFor[[]schedule.SlotResult](),
}

func loadPublicOpenAPI(t *testing.T) map[string]any {
	t.Helper()
	var document map[string]any
	if err := yaml.Unmarshal(publicOpenAPISpec, &document); err != nil {
		t.Fatalf("public OpenAPI spec is not valid YAML: %v", err)
	}
	if document["openapi"] != "3.1.0" {
		t.Fatalf("openapi=%v", document["openapi"])
	}
	return document
}

func registeredPublicRoutes(t *testing.T) []string {
	t.Helper()
	router := newRouter(nil, "http://localhost:3000", HandlerOptions{})
	var routes []string
	err := chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if !strings.HasPrefix(route, "/api/v1/") {
			return nil
		}
		for _, prefix := range privateRoutePrefixes {
			if route == strings.TrimSuffix(prefix, "/") || strings.HasPrefix(route, prefix) {
				return nil
			}
		}
		routes = append(routes, strings.ToLower(method)+" "+route)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(routes)
	return slices.Compact(routes)
}

func TestPublicOpenAPIDocumentsEveryPublicRoute(t *testing.T) {
	registered := registeredPublicRoutes(t)
	documented, _ := documentedAdminOperations(t, loadPublicOpenAPI(t))
	for _, route := range registered {
		_, excluded := undocumentedPublicRoutes[route]
		inSpec := slices.Contains(documented, route)
		switch {
		case excluded && inSpec:
			t.Errorf("route %q is both documented and listed as undocumented", route)
		case !excluded && !inSpec:
			t.Errorf("public route %q is missing from public_openapi.yaml (or list it in undocumentedPublicRoutes with a reason)", route)
		}
	}
	for _, operation := range documented {
		if !slices.Contains(registered, operation) {
			t.Errorf("operation %q is documented but not a registered public route", operation)
		}
	}
	for route := range undocumentedPublicRoutes {
		if !slices.Contains(registered, route) {
			t.Errorf("undocumented route %q is no longer registered", route)
		}
	}
	if len(documented) < 10 {
		t.Fatalf("suspiciously few public operations: %d", len(documented))
	}
}

func TestPublicOpenAPIOperationContract(t *testing.T) {
	document := loadPublicOpenAPI(t)
	_, operations := documentedAdminOperations(t, document)
	seen := map[string]string{}
	for key, operation := range operations {
		method, path, _ := strings.Cut(key, " ")
		if method != "get" {
			t.Errorf("%s: the public API is read-only", key)
		}
		id, _ := operation["operationId"].(string)
		if id == "" {
			t.Errorf("%s: missing operationId", key)
		} else if previous, exists := seen[id]; exists {
			t.Errorf("%s: operationId %q already used by %s", key, id, previous)
		}
		seen[id] = key
		if summary, _ := operation["summary"].(string); strings.TrimSpace(summary) == "" {
			t.Errorf("%s: missing summary", key)
		}
		responses, _ := operation["responses"].(map[string]any)
		if _, ok := responses["200"]; !ok {
			t.Errorf("%s: missing 200 response", key)
		}
		declared := map[string]bool{}
		parameters, _ := operation["parameters"].([]any)
		for _, rawParameter := range parameters {
			parameter, _ := rawParameter.(map[string]any)
			if ref, ok := parameter["$ref"].(string); ok {
				resolved, found := resolveOpenAPIRef(document, ref)
				if !found {
					t.Errorf("%s: unresolved parameter %s", key, ref)
					continue
				}
				parameter, _ = resolved.(map[string]any)
			}
			if _, ok := parameter["schema"].(map[string]any); !ok {
				t.Errorf("%s: parameter %v has no schema", key, parameter["name"])
			}
			if parameter["in"] == "path" {
				name, _ := parameter["name"].(string)
				if parameter["required"] != true {
					t.Errorf("%s: path parameter %q must be required", key, name)
				}
				declared[name] = true
			}
		}
		for _, match := range openAPIPathParameter.FindAllStringSubmatch(path, -1) {
			if !declared[match[1]] {
				t.Errorf("%s: path parameter %q is not declared", key, match[1])
			}
			delete(declared, match[1])
		}
		for name := range declared {
			t.Errorf("%s: declared path parameter %q is not in the path", key, name)
		}
	}
	for id := range publicOpenAPIResponseTypes {
		if _, ok := seen[id]; !ok {
			t.Errorf("response type mapped for unknown operation %q", id)
		}
	}
}

func TestPublicOpenAPIReferencesResolve(t *testing.T) {
	document := loadPublicOpenAPI(t)
	var refs []string
	collectOpenAPIRefs(document, &refs)
	if len(refs) == 0 {
		t.Fatal("spec has no references")
	}
	for _, ref := range refs {
		if _, ok := resolveOpenAPIRef(document, ref); !ok {
			t.Errorf("unresolved reference %s", ref)
		}
	}
}

type goJSONField struct {
	name      string
	typ       reflect.Type
	omitempty bool
	depth     int
}

// goJSONFields follows encoding/json: embedded struct fields are promoted
// unless a shallower field has the same name.
func goJSONFields(typ reflect.Type, depth int, fields map[string]goJSONField) {
	for index := range typ.NumField() {
		field := typ.Field(index)
		tag := field.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, options, _ := strings.Cut(tag, ",")
		if field.Anonymous && name == "" && field.Type.Kind() == reflect.Struct {
			goJSONFields(field.Type, depth+1, fields)
			continue
		}
		if !field.IsExported() {
			continue
		}
		if name == "" {
			name = field.Name
		}
		if existing, ok := fields[name]; ok && existing.depth <= depth {
			continue
		}
		fields[name] = goJSONField{name: name, typ: field.Type, omitempty: slices.Contains(strings.Split(options, ","), "omitempty"), depth: depth}
	}
}

// splitNullable returns the non-null schema and whether null is accepted.
func splitNullable(t *testing.T, location string, schema map[string]any) (map[string]any, bool) {
	t.Helper()
	if variants, ok := schema["oneOf"].([]any); ok {
		var base map[string]any
		nullable := false
		for _, rawVariant := range variants {
			variant, _ := rawVariant.(map[string]any)
			if variant["type"] == "null" {
				nullable = true
			} else {
				base = variant
			}
		}
		if len(variants) != 2 || !nullable || base == nil {
			t.Errorf("%s: oneOf must pair one schema with null", location)
		}
		return base, nullable
	}
	if types, ok := schema["type"].([]any); ok {
		var remaining []any
		for _, value := range types {
			if value != "null" {
				remaining = append(remaining, value)
			}
		}
		if len(remaining) != 1 {
			t.Errorf("%s: type list must be one type plus null", location)
			return schema, false
		}
		base := map[string]any{}
		for key, value := range schema {
			base[key] = value
		}
		base["type"] = remaining[0]
		return base, len(remaining) != len(types)
	}
	return schema, false
}

func checkSchemaMatchesGoType(t *testing.T, location string, schema map[string]any, typ reflect.Type, nullable bool, components map[reflect.Type]string) {
	t.Helper()
	base, acceptsNull := splitNullable(t, location, schema)
	if nullable && !acceptsNull {
		t.Errorf("%s: Go encodes null here but the schema does not accept it", location)
	}
	if !nullable && acceptsNull {
		t.Errorf("%s: schema accepts null but Go never encodes it", location)
	}
	if typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ == reflect.TypeFor[time.Time]() {
		if base["type"] != "string" || base["format"] != "date-time" {
			t.Errorf("%s: time.Time must be a date-time string", location)
		}
		return
	}
	switch typ.Kind() {
	case reflect.Struct:
		ref, _ := base["$ref"].(string)
		want, known := components[typ]
		if !known {
			t.Errorf("%s: Go type %s has no component schema", location, typ)
		} else if ref != "#/components/schemas/"+want {
			t.Errorf("%s: want $ref to %s, got %q", location, want, ref)
		}
	case reflect.Slice:
		if base["type"] != "array" {
			t.Errorf("%s: want array, got %v", location, base["type"])
			return
		}
		items, _ := base["items"].(map[string]any)
		checkSchemaMatchesGoType(t, location+"[]", items, typ.Elem(), typ.Elem().Kind() == reflect.Pointer, components)
	case reflect.String:
		expectType(t, location, base, "string")
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		expectType(t, location, base, "integer")
	case reflect.Float32, reflect.Float64:
		expectType(t, location, base, "number")
	case reflect.Bool:
		expectType(t, location, base, "boolean")
	default:
		t.Errorf("%s: unsupported Go kind %s", location, typ.Kind())
	}
}

func expectType(t *testing.T, location string, schema map[string]any, want string) {
	t.Helper()
	if schema["type"] != want {
		t.Errorf("%s: want type %s, got %v", location, want, schema["type"])
	}
}

func TestPublicOpenAPISchemasMatchGoEncoding(t *testing.T) {
	document := loadPublicOpenAPI(t)
	componentsSection, _ := document["components"].(map[string]any)
	schemas, _ := componentsSection["schemas"].(map[string]any)
	components := map[reflect.Type]string{}
	for name, typ := range publicOpenAPISchemaTypes {
		if previous, exists := components[typ]; exists {
			t.Errorf("Go type %s mapped to both %s and %s", typ, previous, name)
		}
		components[typ] = name
	}
	for name := range schemas {
		if _, ok := publicOpenAPISchemaTypes[name]; !ok {
			t.Errorf("schema %s has no Go type in publicOpenAPISchemaTypes", name)
		}
	}
	for name, typ := range publicOpenAPISchemaTypes {
		schema, ok := schemas[name].(map[string]any)
		if !ok {
			t.Errorf("schema %s is mapped but missing from the spec", name)
			continue
		}
		if schema["type"] != "object" {
			t.Errorf("%s: want type object", name)
		}
		properties, _ := schema["properties"].(map[string]any)
		fields := map[string]goJSONField{}
		goJSONFields(typ, 0, fields)
		var wantRequired []string
		for fieldName, field := range fields {
			if !field.omitempty {
				wantRequired = append(wantRequired, fieldName)
			}
			property, ok := properties[fieldName].(map[string]any)
			if !ok {
				t.Errorf("%s.%s: encoded by Go but not documented", name, fieldName)
				continue
			}
			checkSchemaMatchesGoType(t, name+"."+fieldName, property, field.typ, field.typ.Kind() == reflect.Pointer && !field.omitempty, components)
		}
		for propertyName := range properties {
			if _, ok := fields[propertyName]; !ok {
				t.Errorf("%s.%s: documented but never encoded by Go", name, propertyName)
			}
		}
		var gotRequired []string
		rawRequired, _ := schema["required"].([]any)
		for _, value := range rawRequired {
			gotRequired = append(gotRequired, value.(string))
		}
		slices.Sort(wantRequired)
		slices.Sort(gotRequired)
		if !slices.Equal(wantRequired, gotRequired) {
			t.Errorf("%s: required %v, want %v (fields without omitempty)", name, gotRequired, wantRequired)
		}
	}
	_, operations := documentedAdminOperations(t, document)
	for key, operation := range operations {
		id, _ := operation["operationId"].(string)
		typ, mapped := publicOpenAPIResponseTypes[id]
		if id == "getOpenAPI" {
			continue
		}
		if !mapped {
			t.Errorf("%s: operation %q has no response Go type", key, id)
			continue
		}
		responses, _ := operation["responses"].(map[string]any)
		ok, _ := responses["200"].(map[string]any)
		content, _ := ok["content"].(map[string]any)
		media, _ := content["application/json"].(map[string]any)
		schema, _ := media["schema"].(map[string]any)
		checkSchemaMatchesGoType(t, id+" 200", schema, typ, false, components)
	}
}

func TestPublicOpenAPIIsServedAsJSONForTheSiteOrigin(t *testing.T) {
	handler := NewHandlerWithOptions(nil, "https://site.test", HandlerOptions{})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/openapi.json", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type=%q", got)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "public, max-age=3600" {
		t.Errorf("Cache-Control=%q", got)
	}
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Access-Control-Allow-Origin=%q", got)
	}
	var document struct {
		OpenAPI string `json:"openapi"`
		Servers []struct {
			URL string `json:"url"`
		} `json:"servers"`
		Paths map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &document); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if document.OpenAPI != "3.1.0" || len(document.Servers) != 1 || document.Servers[0].URL != "https://site.test" {
		t.Errorf("openapi=%q servers=%+v", document.OpenAPI, document.Servers)
	}
	if _, ok := document.Paths["/api/v1/search/slot"]; !ok {
		t.Error("served document lacks /api/v1/search/slot")
	}
}
