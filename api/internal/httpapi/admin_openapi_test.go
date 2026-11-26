package httpapi

import (
	"bytes"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"
)

var openAPIMethods = []string{"get", "post", "put", "patch", "delete"}

var openAPIPathParameter = regexp.MustCompile(`\{([A-Za-z]+)\}`)

func loadAdminOpenAPI(t *testing.T) map[string]any {
	t.Helper()
	var document map[string]any
	if err := yaml.Unmarshal(adminOpenAPISpec, &document); err != nil {
		t.Fatalf("admin OpenAPI spec is not valid YAML: %v", err)
	}
	if document["openapi"] != "3.1.0" {
		t.Fatalf("openapi=%v", document["openapi"])
	}
	return document
}

func registeredAdminRoutes(t *testing.T) []string {
	t.Helper()
	router := chi.NewRouter()
	newAdminAPI("http://localhost:3000", AdminOptions{}).routes(router)
	var routes []string
	err := chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		routes = append(routes, strings.ToLower(method)+" "+route)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(routes)
	return routes
}

func documentedAdminOperations(t *testing.T, document map[string]any) ([]string, map[string]map[string]any) {
	t.Helper()
	paths, ok := document["paths"].(map[string]any)
	if !ok || len(paths) == 0 {
		t.Fatal("spec has no paths")
	}
	var operations []string
	byKey := map[string]map[string]any{}
	for path, rawItem := range paths {
		item, ok := rawItem.(map[string]any)
		if !ok {
			t.Fatalf("path %s is not an object", path)
		}
		for key, rawOperation := range item {
			if !slices.Contains(openAPIMethods, key) {
				t.Fatalf("path %s has unsupported key %q", path, key)
			}
			operation, ok := rawOperation.(map[string]any)
			if !ok {
				t.Fatalf("%s %s is not an object", key, path)
			}
			operations = append(operations, key+" "+path)
			byKey[key+" "+path] = operation
		}
	}
	slices.Sort(operations)
	return operations, byKey
}

func resolveOpenAPIRef(document map[string]any, ref string) (any, bool) {
	if !strings.HasPrefix(ref, "#/") {
		return nil, false
	}
	var current any = document
	for _, segment := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[strings.ReplaceAll(strings.ReplaceAll(segment, "~1", "/"), "~0", "~")]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func collectOpenAPIRefs(value any, refs *[]string) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if ref, ok := child.(string); key == "$ref" && ok {
				*refs = append(*refs, ref)
				continue
			}
			collectOpenAPIRefs(child, refs)
		}
	case []any:
		for _, child := range typed {
			collectOpenAPIRefs(child, refs)
		}
	}
}

func TestAdminOpenAPIDocumentsEveryRegisteredRoute(t *testing.T) {
	document := loadAdminOpenAPI(t)
	registered := registeredAdminRoutes(t)
	documented, _ := documentedAdminOperations(t, document)
	for _, route := range registered {
		if !slices.Contains(documented, route) {
			t.Errorf("route %q is registered but missing from admin_openapi.yaml", route)
		}
	}
	for _, operation := range documented {
		if !slices.Contains(registered, operation) {
			t.Errorf("operation %q is documented but not registered", operation)
		}
	}
	if len(registered) < 40 {
		t.Fatalf("suspiciously few admin routes: %d", len(registered))
	}
}

func TestAdminOpenAPIOperationContract(t *testing.T) {
	document := loadAdminOpenAPI(t)
	_, operations := documentedAdminOperations(t, document)
	operationIDs := map[string]string{}
	for key, operation := range operations {
		method, path, _ := strings.Cut(key, " ")
		id, _ := operation["operationId"].(string)
		if id == "" {
			t.Errorf("%s: missing operationId", key)
		} else if previous, exists := operationIDs[id]; exists {
			t.Errorf("%s: operationId %q already used by %s", key, id, previous)
		}
		operationIDs[id] = key
		if summary, _ := operation["summary"].(string); strings.TrimSpace(summary) == "" {
			t.Errorf("%s: missing summary", key)
		}
		risk, _ := operation["x-risk"].(string)
		switch {
		case !slices.Contains([]string{"read", "auth", "review", "operational"}, risk):
			t.Errorf("%s: x-risk %q is not read|auth|review|operational", key, risk)
		case method == "get" && risk != "read":
			t.Errorf("%s: GET operations must be x-risk read", key)
		case method != "get" && risk == "read":
			t.Errorf("%s: mutations cannot be x-risk read", key)
		}
		responses, _ := operation["responses"].(map[string]any)
		if len(responses) == 0 {
			t.Errorf("%s: missing responses", key)
		}
		security, hasSecurity := operation["security"].([]any)
		public := hasSecurity && len(security) == 0
		if _, documented := responses["401"]; !public && !documented {
			t.Errorf("%s: authenticated operation must document 401", key)
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
}

func TestAdminOpenAPIReferencesResolve(t *testing.T) {
	document := loadAdminOpenAPI(t)
	var refs []string
	collectOpenAPIRefs(document, &refs)
	if len(refs) == 0 {
		t.Fatal("spec has no references")
	}
	for _, ref := range refs {
		if _, ok := resolveOpenAPIRef(document, ref); !ok {
			t.Errorf("unresolved $ref %q", ref)
		}
	}
}

func TestAdminOpenAPIEmbeddedMatchesSource(t *testing.T) {
	source, err := os.ReadFile("admin_openapi.yaml")
	if err != nil || !bytes.Equal(source, adminOpenAPISpec) {
		t.Fatalf("embedded spec differs from admin_openapi.yaml: err=%v", err)
	}
}

func TestAdminOpenAPIServedToAuthenticatedClients(t *testing.T) {
	handler := tokenAdminHandler(t, syntheticAdminToken, nil, nil)
	if anonymous := tokenRequest(handler, http.MethodGet, "/api/v1/admin/openapi.yaml", "", nil, nil); anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status=%d", anonymous.Code)
	}
	response := tokenRequest(handler, http.MethodGet, "/api/v1/admin/openapi.yaml", "", bearer(syntheticAdminToken), nil)
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/yaml; charset=utf-8" || response.Header().Get("Cache-Control") != "no-store" || !bytes.Equal(response.Body.Bytes(), adminOpenAPISpec) {
		t.Fatalf("status=%d headers=%v", response.Code, response.Header())
	}
	cookie := loginAdmin(t, handler, "synthetic-admin-password")
	if browser := tokenRequest(handler, http.MethodGet, "/api/v1/admin/openapi.yaml", "", nil, cookie); browser.Code != http.StatusOK {
		t.Fatalf("cookie status=%d", browser.Code)
	}
}
