package mcpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/mcpapi"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestStreamableHTTPInitializesListsAllToolsAndExecutesBusinessOperations(t *testing.T) {
	var mu sync.Mutex
	var calls []string
	subscriptions := []map[string]any{
		{"id": "enabled", "title": "启用订阅", "enable": true},
		{"id": "disabled", "title": "停用订阅", "enable": false},
	}
	ops := mcpapi.Operations{
		SearchMikan: func(_ context.Context, input mcpapi.SearchMikanInput) (any, error) {
			mu.Lock()
			calls = append(calls, "search_mikan:"+input.Text)
			mu.Unlock()
			return map[string]any{"query": input.Text, "year": input.Season.Year, "season": input.Season.Season}, nil
		},
		SearchAniBT: func(context.Context) (any, error) {
			return record(&mu, &calls, "search_anibt", map[string]any{"provider": "anibt"})
		},
		SearchAnimeGarden: func(context.Context) (any, error) {
			return record(&mu, &calls, "search_anime_garden", []any{map[string]any{"provider": "anime-garden"}})
		},
		GetMikanGroups: func(_ context.Context, page string) (any, error) {
			return record(&mu, &calls, "get_mikan_groups:"+page, []any{map[string]any{"rss": "https://example.invalid/rss"}})
		},
		GetAniBTGroups: func(_ context.Context, id string) (any, error) {
			return record(&mu, &calls, "get_anibt_groups:"+id, []any{map[string]any{"bgmId": id}})
		},
		GetAnimeGardenGroups: func(_ context.Context, id string) (any, error) {
			return record(&mu, &calls, "get_anime_garden_groups:"+id, []any{map[string]any{"bgmId": id}})
		},
		PreviewSubscriptionItems: func(_ context.Context, input mcpapi.RssToAniInput) (any, error) {
			return record(&mu, &calls, "preview_subscription_items:"+input.URL, map[string]any{"subscription": map[string]any{"url": input.URL}, "items": []any{map[string]any{"title": "第1集"}}})
		},
		AddSubscription: func(_ context.Context, input mcpapi.RssToAniInput) (any, error) {
			mu.Lock()
			calls = append(calls, "add_subscription:"+input.URL)
			subscriptions = append(subscriptions, map[string]any{"id": "created", "url": input.URL, "enable": input.Enable})
			mu.Unlock()
			return map[string]any{"id": "created", "url": input.URL}, nil
		},
	}
	store := &memoryStore{config: map[string]any{"apiKey": "fixture-api-key"}, subscriptions: subscriptions}
	service := mcpapi.New(mcpapi.Options{Enabled: true, Store: store, Operations: ops})
	httpServer := httptest.NewServer(service.Handler())
	defer httpServer.Close()
	_, session := connectClient(t, httpServer.URL, "fixture-api-key")
	defer session.Close()
	if session.InitializeResult().ServerInfo.Name != "ani-rss" {
		t.Fatalf("server identity = %#v", session.InitializeResult().ServerInfo)
	}

	listed, err := session.ListTools(context.Background(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	wantNames := []string{"add_subscription", "get_anibt_groups", "get_anime_garden_groups", "get_mikan_groups", "list_subscriptions", "preview_subscription_items", "search_anibt", "search_anime_garden", "search_mikan"}
	gotNames := make([]string, 0, len(listed.Tools))
	byName := make(map[string]*mcp.Tool, len(listed.Tools))
	for _, tool := range listed.Tools {
		gotNames = append(gotNames, tool.Name)
		byName[tool.Name] = tool
	}
	sort.Strings(gotNames)
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("tool names = %#v", gotNames)
	}
	assertSchema(t, byName["list_subscriptions"], []string{"dto"}, []string{"dto"})
	assertSchema(t, byName["search_mikan"], []string{"dto"}, []string{"dto"})
	assertSchema(t, byName["search_anibt"], nil, nil)
	assertSchema(t, byName["search_anime_garden"], nil, nil)
	assertSchema(t, byName["get_mikan_groups"], []string{"url"}, []string{"url"})
	assertSchema(t, byName["get_anibt_groups"], []string{"bgmId"}, []string{"bgmId"})
	assertSchema(t, byName["get_anime_garden_groups"], []string{"bgmId"}, []string{"bgmId"})
	assertSchema(t, byName["preview_subscription_items"], []string{"dto"}, []string{"dto"})
	assertSchema(t, byName["add_subscription"], []string{"dto"}, []string{"dto"})
	assertNestedSchema(t, byName["list_subscriptions"], []string{"dto"}, []string{"enabled"}, nil)
	assertNestedSchema(t, byName["search_mikan"], []string{"dto"}, []string{"text", "season"}, nil)
	assertNestedSchema(t, byName["search_mikan"], []string{"dto", "season"}, []string{"year", "season", "seasonLabel", "select"}, []string{"year", "season"})
	assertNestedSchema(t, byName["preview_subscription_items"], []string{"dto"}, []string{"url", "type", "bgmUrl", "subgroup", "enable", "standbyRssList"}, []string{"standbyRssList", "type", "url"})
	assertNestedSchema(t, byName["add_subscription"], []string{"dto", "standbyRssList", "$items"}, []string{"label", "url", "offset"}, []string{"label", "offset", "url"})
	assertToolAnnotations(t, byName, "list_subscriptions", true, false, true, false)
	assertToolAnnotations(t, byName, "search_mikan", true, false, true, true)
	assertToolAnnotations(t, byName, "search_anibt", true, false, true, true)
	assertToolAnnotations(t, byName, "search_anime_garden", true, false, true, true)
	assertToolAnnotations(t, byName, "get_mikan_groups", true, false, true, true)
	assertToolAnnotations(t, byName, "get_anibt_groups", true, false, true, true)
	assertToolAnnotations(t, byName, "get_anime_garden_groups", true, false, true, true)
	assertToolAnnotations(t, byName, "preview_subscription_items", true, false, true, true)
	assertToolAnnotations(t, byName, "add_subscription", false, false, false, false)
	for _, tool := range listed.Tools {
		if tool.Description == "" || tool.Annotations == nil {
			t.Errorf("tool %q is missing description or behavioral annotations", tool.Name)
		}
	}

	filtered := call(t, session, "list_subscriptions", map[string]any{"dto": map[string]any{"enabled": true}})
	if filtered.IsError || !strings.Contains(resultText(t, filtered), "启用订阅") || strings.Contains(resultText(t, filtered), "停用订阅") {
		t.Fatalf("filtered subscriptions result = %#v", filtered)
	}
	search := call(t, session, "search_mikan", map[string]any{"dto": map[string]any{"text": "作品", "season": map[string]any{"year": 2026, "season": "冬"}}})
	if search.IsError || !strings.Contains(resultText(t, search), `"year":2026`) {
		t.Fatalf("Mikan search result = %#v", search)
	}
	for _, tc := range []struct {
		name       string
		args       map[string]any
		wantResult string
	}{
		{name: "search_anibt", args: map[string]any{}, wantResult: "anibt"},
		{name: "search_anime_garden", args: map[string]any{}, wantResult: "anime-garden"},
		{name: "get_mikan_groups", args: map[string]any{"url": "https://mikan.example/anime/1"}, wantResult: "https://example.invalid/rss"},
		{name: "get_anibt_groups", args: map[string]any{"bgmId": "123"}, wantResult: `"bgmId":"123"`},
		{name: "get_anime_garden_groups", args: map[string]any{"bgmId": "456"}, wantResult: `"bgmId":"456"`},
		{name: "preview_subscription_items", args: map[string]any{"dto": map[string]any{"url": "https://mikan.example/rss.xml", "type": "mikan", "enable": true, "standbyRssList": []any{}}}, wantResult: "第1集"},
	} {
		if result := call(t, session, tc.name, tc.args); result.IsError || !strings.Contains(resultText(t, result), tc.wantResult) {
			t.Fatalf("%s result = %#v", tc.name, result)
		}
	}
	added := call(t, session, "add_subscription", map[string]any{"dto": map[string]any{"url": "https://mikan.example/new.xml", "type": "mikan", "enable": true, "standbyRssList": []any{}}})
	if added.IsError || !strings.Contains(resultText(t, added), `"id":"created"`) {
		t.Fatalf("add subscription result = %#v", added)
	}
	if len(subscriptions) != 3 || subscriptions[2]["id"] != "created" {
		t.Fatalf("add operation did not mutate the business store: %#v", subscriptions)
	}
	mu.Lock()
	defer mu.Unlock()
	wantCalls := []string{
		"search_mikan:作品", "search_anibt", "search_anime_garden", "get_mikan_groups:https://mikan.example/anime/1",
		"get_anibt_groups:123", "get_anime_garden_groups:456", "preview_subscription_items:https://mikan.example/rss.xml", "add_subscription:https://mikan.example/new.xml",
	}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("business callback arguments = %v, want %v", calls, wantCalls)
	}
}

func TestMCPRequiresExplicitEnablementAndOnlyAcceptsAPIKey(t *testing.T) {
	store := &memoryStore{config: map[string]any{"apiKey": "fixture-api-key"}}
	disabled := mcpapi.New(mcpapi.Options{Store: store}).Handler()
	request := httptest.NewRequest(http.MethodPost, "/api/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	response := httptest.NewRecorder()
	disabled.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("disabled MCP status = %d, body=%q", response.Code, response.Body.String())
	}

	enabled := mcpapi.New(mcpapi.Options{Enabled: true, Store: store}).Handler()
	for _, tc := range []struct {
		name   string
		header string
		value  string
		query  string
		want   int
	}{
		{name: "missing key", want: http.StatusUnauthorized},
		{name: "JWT cannot bypass API key", header: "Authorization", value: "Bearer fixture-api-key", want: http.StatusUnauthorized},
		{name: "API key header", header: "X-API-Key", value: "fixture-api-key", want: http.StatusBadRequest},
		{name: "API key query", query: "?s=fixture-api-key", want: http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/mcp"+tc.query, strings.NewReader(`{}`))
			req.Header.Set("Content-Type", "application/json")
			if tc.header != "" {
				req.Header.Set(tc.header, tc.value)
			}
			res := httptest.NewRecorder()
			enabled.ServeHTTP(res, req)
			if res.Code != tc.want {
				t.Fatalf("status = %d, body=%q, want %d", res.Code, res.Body.String(), tc.want)
			}
		})
	}
}

func TestUnavailableOperationReturnsMCPIsError(t *testing.T) {
	store := &memoryStore{config: map[string]any{"apiKey": "fixture-api-key"}}
	service := mcpapi.New(mcpapi.Options{Enabled: true, Store: store})
	httpServer := httptest.NewServer(service.Handler())
	defer httpServer.Close()
	_, session := connectClient(t, httpServer.URL, "fixture-api-key")
	defer session.Close()
	result := call(t, session, "search_anibt", map[string]any{})
	if !result.IsError || !strings.Contains(resultText(t, result), "尚未接入") {
		t.Fatalf("missing business operation must return MCP tool error: %#v", result)
	}
}

func TestBusinessFailureReturnsMCPIsError(t *testing.T) {
	store := &memoryStore{config: map[string]any{"apiKey": "fixture-api-key"}}
	service := mcpapi.New(mcpapi.Options{Enabled: true, Store: store, Operations: mcpapi.Operations{
		SearchAniBT: func(context.Context) (any, error) { return nil, errors.New("fixture 上游失败") },
	}})
	httpServer := httptest.NewServer(service.Handler())
	defer httpServer.Close()
	_, session := connectClient(t, httpServer.URL, "fixture-api-key")
	defer session.Close()
	result := call(t, session, "search_anibt", map[string]any{})
	if !result.IsError || !strings.Contains(resultText(t, result), "fixture 上游失败") {
		t.Fatalf("business failure must remain a tool error: %#v", result)
	}
}

func record(mu *sync.Mutex, calls *[]string, call string, result any) (any, error) {
	mu.Lock()
	*calls = append(*calls, call)
	mu.Unlock()
	return result, nil
}

func connectClient(t *testing.T, endpoint, key string) (*mcp.Client, *mcp.ClientSession) {
	t.Helper()
	httpClient := &http.Client{Transport: apiKeyTransport{base: http.DefaultTransport, key: key}}
	client := mcp.NewClient(&mcp.Implementation{Name: "fixture-client", Version: "1.0.0"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: endpoint, HTTPClient: httpClient, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return client, session
}

type apiKeyTransport struct {
	base http.RoundTripper
	key  string
}

func (transport apiKeyTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.Header = request.Header.Clone()
	clone.Header.Set("X-API-Key", transport.key)
	return transport.base.RoundTrip(clone)
}

func call(t *testing.T, session *mcp.ClientSession, name string, arguments map[string]any) *mcp.CallToolResult {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	return result
}

func resultText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if len(result.Content) == 0 {
		t.Fatalf("tool result has no content: %#v", result)
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("first tool content is %T", result.Content[0])
	}
	return text.Text
}

func assertSchema(t *testing.T, tool *mcp.Tool, properties, required []string) {
	t.Helper()
	if tool == nil {
		t.Fatal("tool schema is missing")
	}
	encoded, err := json.Marshal(tool.InputSchema)
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Properties map[string]any `json:"properties"`
		Required   []string       `json:"required"`
	}
	if err := json.Unmarshal(encoded, &schema); err != nil {
		t.Fatal(err)
	}
	var got []string
	for name := range schema.Properties {
		got = append(got, name)
	}
	sort.Strings(got)
	want := append([]string(nil), properties...)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s properties = %v, want %v", tool.Name, got, want)
	}
	gotRequired := append([]string(nil), schema.Required...)
	sort.Strings(gotRequired)
	wantRequired := append([]string(nil), required...)
	sort.Strings(wantRequired)
	if !reflect.DeepEqual(gotRequired, wantRequired) {
		t.Errorf("%s required = %v, want %v", tool.Name, gotRequired, wantRequired)
	}
}

func assertNestedSchema(t *testing.T, tool *mcp.Tool, path, want, required []string) {
	t.Helper()
	encoded, err := json.Marshal(tool.InputSchema)
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(encoded, &schema); err != nil {
		t.Fatal(err)
	}
	current := schema
	for _, property := range path {
		if property == "$items" {
			items, ok := current["items"].(map[string]any)
			if !ok {
				t.Fatalf("%s is missing array items at %v", tool.Name, path)
			}
			current = items
			continue
		}
		properties, _ := current["properties"].(map[string]any)
		propertySchema, ok := properties[property].(map[string]any)
		if !ok {
			t.Fatalf("%s is missing property path %v", tool.Name, path)
		}
		current = propertySchema
	}
	properties, _ := current["properties"].(map[string]any)
	if properties == nil {
		if items, ok := current["items"].(map[string]any); ok {
			properties, _ = items["properties"].(map[string]any)
		}
	}
	var got []string
	for name := range properties {
		got = append(got, name)
	}
	sort.Strings(got)
	want = append([]string(nil), want...)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s.%v properties = %v, want %v", tool.Name, path, got, want)
	}
	gotRequired, _ := current["required"].([]any)
	var requiredNames []string
	for _, name := range gotRequired {
		if value, ok := name.(string); ok {
			requiredNames = append(requiredNames, value)
		}
	}
	sort.Strings(requiredNames)
	wantRequired := append([]string(nil), required...)
	sort.Strings(wantRequired)
	if !reflect.DeepEqual(requiredNames, wantRequired) {
		t.Errorf("%s.%v required = %v, want %v", tool.Name, path, requiredNames, wantRequired)
	}
}

func assertToolAnnotations(t *testing.T, tools map[string]*mcp.Tool, name string, readOnly, destructive, idempotent, openWorld bool) {
	t.Helper()
	tool := tools[name]
	if tool == nil || tool.Annotations == nil {
		t.Fatalf("%s annotations are missing", name)
	}
	encoded, err := json.Marshal(tool.Annotations)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"readOnlyHint": readOnly, "destructiveHint": destructive,
		"idempotentHint": idempotent, "openWorldHint": openWorld,
	}
	for key, expected := range want {
		if got[key] != expected {
			t.Errorf("%s annotation %s = %v, want %v", name, key, got[key], expected)
		}
	}
}

type memoryStore struct {
	config        map[string]any
	subscriptions []map[string]any
}

func (store *memoryStore) GetConfig(context.Context) (map[string]any, error) {
	return store.config, nil
}

func (store *memoryStore) ListSubscriptions(context.Context) ([]map[string]any, error) {
	return store.subscriptions, nil
}
