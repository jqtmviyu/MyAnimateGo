package server_test

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/server"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/storage"
)

type listInterleavingStore struct {
	*storage.Store
	afterList func()
}

func (s *listInterleavingStore) ListSubscriptions(ctx context.Context) ([]map[string]any, error) {
	items, err := s.Store.ListSubscriptions(ctx)
	if err == nil && s.afterList != nil {
		after := s.afterList
		s.afterList = nil
		after()
	}
	return items, err
}

func TestAddSubscriptionServiceAndHTTPShareDuplicateAndOverrideSemantics(t *testing.T) {
	app, store, closeStore := newPersistentServer(t)
	defer closeStore()
	ctx := context.Background()

	first, err := app.AddSubscription(ctx, map[string]any{"id": "stable-id", "title": "示例作品", "season": 2, "enable": false}, false)
	if err != nil || first["id"] != "stable-id" || first["enable"] != false || first["currentEpisodeNumber"] != float64(0) {
		t.Fatalf("AddSubscription() = %#v, %v", first, err)
	}
	if _, err := app.AddSubscription(ctx, map[string]any{"id": "stable-id", "title": "另一标题", "season": 1}, false); err == nil {
		t.Fatal("duplicate ID should be rejected without override")
	}
	if _, err := app.AddSubscription(ctx, map[string]any{"title": "示例作品", "season": 2}, false); err == nil {
		t.Fatal("duplicate title and season should be rejected without override")
	}

	client := httptest.NewServer(app)
	defer client.Close()
	token := loginSubscriptions(t, client.URL)
	result := postSubscriptions(t, client.URL, "/api/addAni?override=true", token, map[string]any{"title": "示例作品", "season": 2, "enable": true})
	if result["code"] != float64(200) {
		t.Fatalf("HTTP override failed: %#v", result)
	}
	items, err := store.ListSubscriptions(ctx)
	if err != nil || len(items) != 1 {
		t.Fatalf("stored subscriptions = %#v, %v", items, err)
	}
	if items[0]["id"] == "stable-id" || items[0]["title"] != "示例作品" || items[0]["enable"] != true {
		t.Fatalf("override should replace duplicate title/season with a new subscription: %#v", items[0])
	}
	if result := postSubscriptions(t, client.URL, "/api/addAni", token, map[string]any{"title": "示例作品", "season": 2}); result["code"] != float64(500) {
		t.Fatalf("HTTP duplicate must use the same conflict check: %#v", result)
	}
}

func TestImportAniHTTPSupportsSkipAndReplaceWhilePreservingProgressAndID(t *testing.T) {
	app, store, closeStore := newPersistentServer(t)
	defer closeStore()
	ctx := context.Background()
	if err := store.PutSubscription(ctx, map[string]any{
		"id": "keep-id", "title": "已订阅作品", "season": 1, "enable": false,
		"currentEpisodeNumber": 8, "lastDownloadTime": 123456, "customField": "旧值",
	}); err != nil {
		t.Fatal(err)
	}
	client := httptest.NewServer(app)
	defer client.Close()
	token := loginSubscriptions(t, client.URL)

	imported := map[string]any{"id": "incoming-id", "title": "已订阅作品", "season": 1, "enable": true, "currentEpisodeNumber": 99, "lastDownloadTime": 999999, "customField": "新值"}
	skip := map[string]any{"aniList": []any{imported}, "conflict": "SKIP"}
	if result := postSubscriptions(t, client.URL, "/api/importAni", token, skip); result["code"] != float64(200) {
		t.Fatalf("SKIP import failed: %#v", result)
	}
	items, err := store.ListSubscriptions(ctx)
	if err != nil || len(items) != 1 || items[0]["customField"] != "旧值" {
		t.Fatalf("SKIP changed the existing subscription: %#v (%v)", items, err)
	}

	replace := map[string]any{"aniList": []any{imported}, "conflict": "REPLACE"}
	if result := postSubscriptions(t, client.URL, "/api/importAni", token, replace); result["code"] != float64(200) {
		t.Fatalf("REPLACE import failed: %#v", result)
	}
	items, err = store.ListSubscriptions(ctx)
	if err != nil || len(items) != 1 {
		t.Fatalf("replaced subscriptions = %#v, %v", items, err)
	}
	got := items[0]
	if got["id"] != "keep-id" || got["currentEpisodeNumber"] != float64(8) || got["lastDownloadTime"] != float64(123456) || got["customField"] != "新值" || got["enable"] != true {
		t.Fatalf("REPLACE must retain ID/progress while updating imported fields: %#v", got)
	}

	newEntry := map[string]any{"id": "discard-this-id", "title": "新作品", "season": 2, "enable": true}
	if result := postSubscriptions(t, client.URL, "/api/importAni", token, map[string]any{"aniList": []any{newEntry}, "conflict": "SKIP"}); result["code"] != float64(200) {
		t.Fatalf("new subscription import failed: %#v", result)
	}
	items, err = store.ListSubscriptions(ctx)
	if err != nil || len(items) != 2 || items[1]["id"] == "discard-this-id" || textValue(items[1]["id"]) == "" {
		t.Fatalf("new imported subscription should receive a fresh ID: %#v (%v)", items, err)
	}
}

func TestImportAniRejectsEmptyOrUnknownConflictWithoutChangingStore(t *testing.T) {
	app, store, closeStore := newPersistentServer(t)
	defer closeStore()
	if err := store.PutSubscription(context.Background(), map[string]any{"id": "original", "title": "现有作品", "season": 1}); err != nil {
		t.Fatal(err)
	}
	client := httptest.NewServer(app)
	defer client.Close()
	token := loginSubscriptions(t, client.URL)
	for _, body := range []any{
		map[string]any{"aniList": []any{}, "conflict": "SKIP"},
		map[string]any{"aniList": []any{map[string]any{"title": "新作品", "season": 1}}, "conflict": "MERGE"},
	} {
		result := postSubscriptions(t, client.URL, "/api/importAni", token, body)
		if result["code"] != float64(500) {
			t.Fatalf("invalid import should return an application error: %#v", result)
		}
	}
	items, err := store.ListSubscriptions(context.Background())
	if err != nil || len(items) != 1 || items[0]["id"] != "original" {
		t.Fatalf("failed imports changed persisted subscriptions: %#v (%v)", items, err)
	}
}

func TestImportAniStoresCoversOnlyForAddedOrReplacedSubscriptions(t *testing.T) {
	app, store, closeStore := newPersistentServer(t)
	defer closeStore()
	ctx := context.Background()
	if err := store.PutSubscription(ctx, map[string]any{
		"id": "existing", "title": "作品", "season": 1, "cover": "old/cover.jpg", "currentEpisodeNumber": 4,
	}); err != nil {
		t.Fatal(err)
	}
	imageServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "fixture cover bytes")
	}))
	defer imageServer.Close()
	coverPath := filepath.Join(t.TempDir(), "files", "cover.jpg")
	calls := 0
	app.SetSubscriptionCoverImporter(func(ctx context.Context, image string) (string, error) {
		calls++
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, image, nil)
		if err != nil {
			return "", err
		}
		response, err := imageServer.Client().Do(request)
		if err != nil {
			return "", err
		}
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		if err != nil {
			return "", err
		}
		if err := os.MkdirAll(filepath.Dir(coverPath), 0700); err != nil {
			return "", err
		}
		if err := os.WriteFile(coverPath, data, 0600); err != nil {
			return "", err
		}
		return "files/cover.jpg", nil
	})
	image := imageServer.URL + "/cover.jpg"
	if err := app.ImportAni(ctx, []map[string]any{{"title": "作品", "season": 1, "image": image}}, "SKIP"); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("SKIP downloaded a cover %d times", calls)
	}
	if err := app.ImportAni(ctx, []map[string]any{{"title": "作品", "season": 1, "image": image, "extra": "updated"}}, "REPLACE"); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("REPLACE cover importer calls = %d, want 1", calls)
	}
	data, err := os.ReadFile(coverPath)
	if err != nil || string(data) != "fixture cover bytes" {
		t.Fatalf("stored cover = %q, %v", data, err)
	}
	items, err := store.ListSubscriptions(ctx)
	if err != nil || len(items) != 1 || items[0]["id"] != "existing" || items[0]["cover"] != "files/cover.jpg" || items[0]["image"] != image || items[0]["currentEpisodeNumber"] != float64(4) {
		t.Fatalf("imported cover and preserved progress = %#v, %v", items, err)
	}
}

func TestImportAniRequiresCoverImporterBeforePersistingImage(t *testing.T) {
	app, store, closeStore := newPersistentServer(t)
	defer closeStore()
	err := app.ImportAni(context.Background(), []map[string]any{{"title": "新作品", "season": 1, "image": "https://example.invalid/cover.jpg"}}, "REPLACE")
	if err == nil {
		t.Fatal("an image import without a cover importer should fail")
	}
	items, listErr := store.ListSubscriptions(context.Background())
	if listErr != nil || len(items) != 0 {
		t.Fatalf("failed cover import persisted subscriptions: %#v, %v", items, listErr)
	}
}

func TestSetSubscriptionPatchesProvidedFieldsAgainstConcurrentProgress(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.PutSubscription(ctx, map[string]any{
		"id": "target", "title": "旧标题", "season": 1, "enable": true,
		"score": 4.5, "totalEpisodeNumber": 12, "currentEpisodeNumber": 2, "lastDownloadTime": 100,
		"customField": "preserved",
	}); err != nil {
		t.Fatal(err)
	}
	wrapped := &listInterleavingStore{Store: store}
	api, err := server.New(ctx, wrapped)
	if err != nil {
		t.Fatal(err)
	}
	wrapped.afterList = func() {
		if err := store.UpdateSubscriptionFields(ctx, "target", map[string]any{
			"score": 8.8, "totalEpisodeNumber": 24, "currentEpisodeNumber": 9, "lastDownloadTime": 900,
		}); err != nil {
			t.Error(err)
		}
	}
	if err := api.UpdateSubscription(ctx, map[string]any{"id": "target", "title": "新标题", "season": 1, "enable": false}, false); err != nil {
		t.Fatal(err)
	}
	items, err := store.ListSubscriptions(ctx)
	if err != nil || len(items) != 1 {
		t.Fatalf("subscriptions = %#v, %v", items, err)
	}
	got := items[0]
	if got["title"] != "新标题" || got["enable"] != false || got["pinyin"] != "xinbiaoti" || got["pinyinInitials"] != "xbt" {
		t.Fatalf("provided and derived fields were not patched: %#v", got)
	}
	if got["score"] != 8.8 || got["totalEpisodeNumber"] != float64(24) || got["currentEpisodeNumber"] != float64(9) || got["lastDownloadTime"] != float64(900) || got["customField"] != "preserved" {
		t.Fatalf("concurrent and unknown fields were overwritten: %#v", got)
	}
}

func TestSetSubscriptionRSSURLEnqueuesRefreshWithoutLosingProgress(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.PutSubscription(ctx, map[string]any{
		"id": "target", "title": "作品", "season": 1, "url": "https://old.example/rss", "enable": true,
		"currentEpisodeNumber": 2, "lastDownloadTime": 100,
	}); err != nil {
		t.Fatal(err)
	}
	wrapped := &listInterleavingStore{Store: store}
	api, err := server.New(ctx, wrapped)
	if err != nil {
		t.Fatal(err)
	}
	wrapped.afterList = func() {
		if err := store.UpdateSubscriptionFields(ctx, "target", map[string]any{
			"currentEpisodeNumber": 8, "lastDownloadTime": 800,
		}); err != nil {
			t.Error(err)
		}
	}
	if err := api.UpdateSubscription(ctx, map[string]any{
		"id": "target", "title": "作品", "season": 1, "url": "https://new.example/rss",
	}, false); err != nil {
		t.Fatal(err)
	}
	items, err := store.ListSubscriptions(ctx)
	if err != nil || len(items) != 1 {
		t.Fatalf("subscriptions = %#v, %v", items, err)
	}
	if items[0]["url"] != "https://new.example/rss" || items[0]["currentEpisodeNumber"] != float64(8) || items[0]["lastDownloadTime"] != float64(800) {
		t.Fatalf("RSS edit lost its URL or concurrent progress: %#v", items[0])
	}
	job, err := store.ClaimJob(ctx, "test", time.Now().Add(time.Second), time.Minute)
	if err != nil || job == nil || job.Kind != "rss-refresh" {
		t.Fatalf("RSS edit job = %#v, %v", job, err)
	}
	var payload map[string]any
	if err := json.Unmarshal(job.Payload, &payload); err != nil || payload["subscriptionId"] != "target" || payload["automatic"] != false {
		t.Fatalf("RSS edit payload = %#v, %v", payload, err)
	}
}

func TestMoveSubscriptionAndRSSRefreshAreQueuedInOrder(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.PutSubscription(ctx, map[string]any{
		"id": "moving", "title": "作品", "season": 1, "url": "https://old.example/rss", "enable": true,
		"customDownloadPath": true, "customDownloadPathTemplate": "/old",
		"currentEpisodeNumber": 2, "lastDownloadTime": 100,
	}); err != nil {
		t.Fatal(err)
	}
	wrapped := &listInterleavingStore{Store: store}
	api, err := server.New(ctx, wrapped)
	if err != nil {
		t.Fatal(err)
	}
	api.SetSubscriptionPathResolver(func(_ context.Context, item map[string]any) (string, error) {
		return item["customDownloadPathTemplate"].(string), nil
	})
	wrapped.afterList = func() {
		if err := store.UpdateSubscriptionFields(ctx, "moving", map[string]any{
			"currentEpisodeNumber": 8, "lastDownloadTime": 800,
		}); err != nil {
			t.Error(err)
		}
	}
	if err := api.UpdateSubscription(ctx, map[string]any{
		"id": "moving", "title": "作品", "season": 1, "url": "https://new.example/rss",
		"customDownloadPath": true, "customDownloadPathTemplate": "/new",
	}, true); err != nil {
		t.Fatal(err)
	}
	items, err := store.ListSubscriptions(ctx)
	if err != nil || len(items) != 1 || items[0]["currentEpisodeNumber"] != float64(8) || items[0]["lastDownloadTime"] != float64(800) {
		t.Fatalf("move edit lost current progress: %#v, %v", items, err)
	}
	first, err := store.ClaimJob(ctx, "test", time.Now().Add(time.Second), time.Minute)
	if err != nil || first == nil || first.Kind != server.JobSubscriptionEffects {
		t.Fatalf("first move job = %#v, %v", first, err)
	}
	second, err := store.ClaimJob(ctx, "test", time.Now().Add(time.Second), time.Minute)
	if err != nil || second == nil || second.Kind != "rss-refresh" {
		t.Fatalf("second refresh job = %#v, %v", second, err)
	}
}

func TestBatchEnablePatchesOnlySelectedEnableFields(t *testing.T) {
	app, store, closeStore := newPersistentServer(t)
	defer closeStore()
	ctx := context.Background()
	for _, item := range []map[string]any{
		{"id": "first", "title": "第一部", "season": 1, "enable": true, "score": 8, "currentEpisodeNumber": 4, "lastDownloadTime": 12, "custom": "preserved"},
		{"id": "second", "title": "第二部", "season": 1, "enable": true, "score": 7},
		{"id": "untouched", "title": "未选择", "season": 1, "enable": true},
	} {
		if err := store.PutSubscription(ctx, item); err != nil {
			t.Fatal(err)
		}
	}
	client := httptest.NewServer(app)
	defer client.Close()
	token := loginSubscriptions(t, client.URL)
	result := postSubscriptions(t, client.URL, "/api/batchEnable?value=false", token, []string{"first", "second", "missing"})
	if result["code"] != float64(200) {
		t.Fatalf("batch enable failed: %#v", result)
	}
	items, err := store.ListSubscriptions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	byID := make(map[string]map[string]any, len(items))
	for _, item := range items {
		byID[item["id"].(string)] = item
	}
	if byID["first"]["enable"] != false || byID["second"]["enable"] != false || byID["untouched"]["enable"] != true {
		t.Fatalf("batch enable changed the wrong rows: %#v", byID)
	}
	if byID["first"]["score"] != float64(8) || byID["first"]["currentEpisodeNumber"] != float64(4) || byID["first"]["lastDownloadTime"] != float64(12) || byID["first"]["custom"] != "preserved" {
		t.Fatalf("batch enable lost unrelated fields: %#v", byID["first"])
	}
}

func TestImportAniMergesCurrentProgressAndUntouchedRowsAtCommit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, item := range []map[string]any{
		{"id": "replace", "title": "作品", "season": 1, "currentEpisodeNumber": 2, "lastDownloadTime": 10, "cover": "old-cover"},
		{"id": "untouched", "title": "其他", "season": 1, "currentEpisodeNumber": 3, "lastDownloadTime": 20, "customField": "old"},
	} {
		if err := store.PutSubscription(ctx, item); err != nil {
			t.Fatal(err)
		}
	}
	api, err := server.New(ctx, store)
	if err != nil {
		t.Fatal(err)
	}
	requested, release := make(chan struct{}), make(chan struct{})
	api.SetSubscriptionCoverImporter(func(ctx context.Context, _ string) (string, error) {
		close(requested)
		select {
		case <-release:
			return "covers/new.jpg", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	})
	done := make(chan error, 1)
	go func() {
		done <- api.ImportAni(ctx, []map[string]any{{"title": "作品", "season": 1, "image": "fixture://cover", "enable": true}}, "REPLACE")
	}()
	select {
	case <-requested:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := store.UpdateSubscriptionFields(ctx, "replace", map[string]any{"currentEpisodeNumber": 8, "lastDownloadTime": 80}); err != nil {
		close(release)
		t.Fatal(err)
	}
	if err := store.UpdateSubscriptionFields(ctx, "untouched", map[string]any{"currentEpisodeNumber": 9, "lastDownloadTime": 90, "customField": "current"}); err != nil {
		close(release)
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	items, err := store.ListSubscriptions(ctx)
	if err != nil || len(items) != 2 {
		t.Fatalf("imported subscriptions = %#v, %v", items, err)
	}
	byID := map[string]map[string]any{}
	for _, item := range items {
		byID[item["id"].(string)] = item
	}
	if byID["replace"]["currentEpisodeNumber"] != float64(8) || byID["replace"]["lastDownloadTime"] != float64(80) || byID["replace"]["cover"] != "covers/new.jpg" {
		t.Fatalf("replaced row did not merge its latest progress: %#v", byID["replace"])
	}
	if byID["untouched"]["currentEpisodeNumber"] != float64(9) || byID["untouched"]["lastDownloadTime"] != float64(90) || byID["untouched"]["customField"] != "current" {
		t.Fatalf("untouched row used a stale import snapshot: %#v", byID["untouched"])
	}
}

func newPersistentServer(t *testing.T) (*server.Server, *storage.Store, func()) {
	t.Helper()
	store, err := storage.Open(t.TempDir() + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	app, err := server.New(context.Background(), store)
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	return app, store, func() { _ = store.Close() }
}

func loginSubscriptions(t *testing.T, baseURL string) string {
	t.Helper()
	hash := md5.Sum([]byte("admin"))
	result := postSubscriptions(t, baseURL, "/api/login", "", map[string]any{"username": "admin", "password": hex.EncodeToString(hash[:])})
	token, _ := result["data"].(string)
	if result["code"] != float64(200) || token == "" {
		t.Fatalf("login result = %#v", result)
	}
	return token
}

func postSubscriptions(t *testing.T, baseURL, path, token string, body any) map[string]any {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, baseURL+path, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", token)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var result map[string]any
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	return result
}

func textValue(value any) string { text, _ := value.(string); return text }
