package app_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestDevelopmentOpenAPIDescribesMountedRoutesAndAuthentication(t *testing.T) {
	t.Setenv("SWAGGER_ENABLED", "true")
	t.Setenv("MCP_ENABLED", "false")
	events, stop := start(t, "serve", "-dev", "-data-dir", t.TempDir(), "-listen", "127.0.0.1:0")
	defer stop()
	address := events["address"].(string)
	response, err := http.Get("http://" + address + "/api/v3/api-docs")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var document map[string]any
	if err := json.NewDecoder(response.Body).Decode(&document); err != nil {
		t.Fatal(err)
	}
	if document["openapi"] != "3.1.0" {
		t.Fatalf("OpenAPI document unavailable: %v", document["code"])
	}
	paths := document["paths"].(map[string]any)
	if len(paths) != 59 {
		t.Fatalf("documented paths = %d, want 59 mounted business paths", len(paths))
	}
	activation := paths["/api/setAutomaticTasksPaused"].(map[string]any)["post"].(map[string]any)
	activationSchema := activation["requestBody"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
	if activationSchema["required"].([]any)[0] != "paused" || activationSchema["properties"].(map[string]any)["reviewed"] == nil {
		t.Fatal("manual activation review contract is undocumented")
	}
	loginOperation := paths["/api/login"].(map[string]any)["post"].(map[string]any)
	if len(loginOperation["security"].([]any)) != 0 {
		t.Fatal("login incorrectly requires an existing session")
	}
	components := document["components"].(map[string]any)
	schemes := components["securitySchemes"].(map[string]any)
	if schemes["session"].(map[string]any)["name"] != "Authorization" || schemes["api-key"].(map[string]any)["name"] != "api-key" || schemes["resource-token"].(map[string]any)["in"] != "query" {
		t.Fatal("authentication documentation differs from runtime")
	}
	file := paths["/api/file"].(map[string]any)["get"].(map[string]any)
	if _, ok := file["responses"].(map[string]any)["200"]; !ok {
		t.Fatal("image attachment response is undocumented")
	}
	for _, path := range []string{"/api/peerFilter", "/api/peerFilterBans", "/api/peerFilterAction", "/api/peerFilterFeed"} {
		if paths[path] == nil {
			t.Fatalf("peer filter route missing: %s", path)
		}
	}
	upload := paths["/api/upload"].(map[string]any)["post"].(map[string]any)
	if _, ok := upload["requestBody"].(map[string]any)["content"].(map[string]any)["multipart/form-data"]; !ok {
		t.Fatal("multipart upload is undocumented")
	}
	schemas := components["schemas"].(map[string]any)
	config := schemas["Config"].(map[string]any)["properties"].(map[string]any)
	if config["jwtKey"].(map[string]any)["readOnly"] != true {
		t.Fatal("internal signing key is documented as writable")
	}
	for _, path := range []string{"/v3/api-docs", "/swagger-ui/index.html", "/swagger-ui/swagger-ui-bundle.js", "/swagger-ui/swagger-ui.css"} {
		res, err := http.Get("http://" + address + path)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if err != nil || res.StatusCode != 200 || len(body) == 0 {
			t.Fatalf("documentation asset %s = %d, %v", path, res.StatusCode, err)
		}
		if strings.HasSuffix(path, "index.html") && (!strings.Contains(string(body), "SwaggerUIBundle") || strings.Contains(string(body), "https://unpkg")) {
			t.Fatal("Swagger UI must load bundled assets")
		}
	}
	token := login(t, address)
	post(t, address, "/api/setConfig", token, map[string]any{"apiKey": "runtime-only-secret"})
	res, err := http.Get("http://" + address + "/api/v3/api-docs")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if strings.Contains(string(body), "runtime-only-secret") {
		t.Fatal("runtime config values leaked into the public schema")
	}
}

func TestRuntimeKeepsSwaggerDisabledByDefault(t *testing.T) {
	t.Setenv("SWAGGER_ENABLED", "false")
	events, stop := start(t, "serve", "-data-dir", t.TempDir(), "-listen", "127.0.0.1:0")
	defer stop()
	response, err := http.Get("http://" + events["address"].(string) + "/api/v3/api-docs")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var result map[string]any
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result["code"] != float64(404) {
		t.Fatal("Swagger was enabled without the configured switch")
	}
}
