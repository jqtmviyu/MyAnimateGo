package filesapi_test

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/filesapi"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/server"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/storage"
)

type doerFunc func(*http.Request) (*http.Response, error)

func (f doerFunc) Do(request *http.Request) (*http.Response, error) { return f(request) }

func newAPI(t *testing.T, options filesapi.Options) (*server.Server, *storage.Store, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := storage.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	api, err := server.New(context.Background(), store)
	if err != nil {
		t.Fatal(err)
	}
	options.Store = store
	if options.SnapshotBarrier == nil {
		options.SnapshotBarrier = func(context.Context) (func(), error) { return func() {}, nil }
	}
	service, err := filesapi.New(options)
	if err != nil {
		t.Fatal(err)
	}
	if err := api.Register(service.Routes()...); err != nil {
		t.Fatal(err)
	}
	config, err := store.GetConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return api, store, config["apiKey"].(string)
}

type writeBarrier struct {
	mu      sync.RWMutex
	entered chan struct{}
	resume  chan struct{}
}

func (barrier *writeBarrier) freeze(ctx context.Context) (func(), error) {
	barrier.mu.Lock()
	if barrier.entered != nil {
		close(barrier.entered)
		select {
		case <-barrier.resume:
		case <-ctx.Done():
			barrier.mu.Unlock()
			return nil, ctx.Err()
		}
	}
	return barrier.mu.Unlock, nil
}

func (barrier *writeBarrier) write(update func()) {
	barrier.mu.RLock()
	defer barrier.mu.RUnlock()
	update()
}

func authorizedRequest(api http.Handler, key, method, path string, body io.Reader, contentType string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, body)
	request.Header.Set("x-api-key", key)
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	api.ServeHTTP(recorder, request)
	return recorder
}

func TestUploadAndImageFileRoutesPreserveBytes(t *testing.T) {
	api, _, key := newAPI(t, filesapi.Options{})
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "cover.png")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("0123456789abcdef"))
	_ = writer.Close()
	response := authorizedRequest(api, key, http.MethodPost, "/api/upload", &body, writer.FormDataContentType())
	var result struct {
		Code int    `json:"code"`
		Data string `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.Code != 200 || result.Data == "" {
		t.Fatalf("upload response=%s err=%v", response.Body.String(), err)
	}
	filename := base64.StdEncoding.EncodeToString([]byte(result.Data))
	fileResponse := authorizedRequest(api, key, http.MethodGet, "/api/file?filename="+url.QueryEscape(filename), nil, "")
	if fileResponse.Code != http.StatusOK || fileResponse.Body.String() != "0123456789abcdef" {
		t.Fatalf("uploaded file response=%d %q", fileResponse.Code, fileResponse.Body.String())
	}
	rangeRequest := httptest.NewRequest(http.MethodGet, "/api/file?filename="+url.QueryEscape(filename)+"&s="+url.QueryEscape(key), nil)
	rangeRequest.Header.Set("Range", "bytes=4-8")
	rangeResponse := httptest.NewRecorder()
	api.ServeHTTP(rangeResponse, rangeRequest)
	if rangeResponse.Code != http.StatusOK || rangeResponse.Body.String() != "0123456789abcdef" {
		t.Fatalf("range response=%d headers=%v body=%q", rangeResponse.Code, rangeResponse.Header(), rangeResponse.Body.String())
	}
}

func TestClearCachePreservesReferencedCoverAndRemovesImageCache(t *testing.T) {
	api, store, key := newAPI(t, filesapi.Options{})
	if err := store.PutSubscription(context.Background(), map[string]any{"id": "cover-sub", "title": "Cover", "cover": "used/cover.png"}); err != nil {
		t.Fatal(err)
	}
	used := filepath.Join(store.DataDir(), "files", "used", "cover.png")
	orphan := filepath.Join(store.DataDir(), "files", "orphan.png")
	imageCache := filepath.Join(store.DataDir(), "img", "a", "cached.jpg")
	for _, path := range []string{used, orphan, imageCache} {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("cached"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	response := authorizedRequest(api, key, http.MethodPost, "/api/clearCache", nil, "")
	if !strings.Contains(response.Body.String(), "清理完成") {
		t.Fatalf("clear cache response=%s", response.Body.String())
	}
	if _, err := os.Stat(used); err != nil {
		t.Fatalf("referenced cover was removed: %v", err)
	}
	for _, path := range []string{orphan, imageCache} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("cache was not removed %s: %v", path, err)
		}
	}
}

func TestDeleteTorrentRouteOnlyRemovesSelectedSubscriptionCache(t *testing.T) {
	api, store, key := newAPI(t, filesapi.Options{})
	hash := "0123456789abcdef0123456789abcdef01234567"
	if err := store.PutSubscription(context.Background(), map[string]any{"id": "sub-1", "title": "Show", "season": 1, "ova": false}); err != nil {
		t.Fatal(err)
	}
	selected := filepath.Join(store.DataDir(), "torrents", "s", "sub-1", hash+".torrent")
	other := filepath.Join(store.DataDir(), "torrents", "s", "sub-2", hash+".torrent")
	for _, path := range []string{selected, other} {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("torrent"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	response := authorizedRequest(api, key, http.MethodPost, "/api/deleteTorrent?id=sub-1&hash="+hash, nil, "")
	if !strings.Contains(response.Body.String(), "删除完成") {
		t.Fatalf("delete cache response=%s", response.Body.String())
	}
	if _, err := os.Stat(selected); !os.IsNotExist(err) {
		t.Fatalf("selected torrent cache remains: %v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatalf("another subscription cache was removed: %v", err)
	}
}

func TestBackupRouteRestoresSQLiteAndAttachmentsAsOneSnapshot(t *testing.T) {
	api, store, key := newAPI(t, filesapi.Options{})
	ctx := context.Background()
	config, err := store.GetConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	config["siteTitle"] = "备份时标题"
	config["migrationReport"] = map[string]any{"sourceVersion": "v1.1.50", "subscriptionCount": 1}
	config["automaticTasksPaused"] = false
	if err := store.SaveConfig(ctx, config); err != nil {
		t.Fatal(err)
	}
	if err := store.PutSubscription(ctx, map[string]any{"id": "backup-anime", "title": "快照作品"}); err != nil {
		t.Fatal(err)
	}
	attachment := filepath.Join(store.DataDir(), "files", "cover.png")
	if err := os.MkdirAll(filepath.Dir(attachment), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(attachment, []byte("cover-at-backup"), 0600); err != nil {
		t.Fatal(err)
	}
	backup := authorizedRequest(api, key, http.MethodGet, "/api/exportBackup", nil, "")
	if backup.Code != http.StatusOK || !strings.HasPrefix(backup.Header().Get("Content-Type"), "application/zip") {
		t.Fatalf("backup export response=%d %v body=%s", backup.Code, backup.Header(), backup.Body.String())
	}
	config, err = store.GetConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	config["siteTitle"] = "后续修改"
	if err := store.SaveConfig(ctx, config); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteSubscriptions(ctx, []string{"backup-anime"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(attachment, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "ani-rss.backup.go.zip")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(backup.Body.Bytes())
	_ = writer.Close()
	restored := authorizedRequest(api, key, http.MethodPost, "/api/importBackup", &body, writer.FormDataContentType())
	if !strings.Contains(restored.Body.String(), `"code":200`) {
		t.Fatalf("backup restore response=%s", restored.Body.String())
	}
	config, err = store.GetConfig(ctx)
	if err != nil || config["siteTitle"] != "备份时标题" {
		t.Fatalf("restored config=%#v err=%v", config, err)
	}
	if config["migrationReport"].(map[string]any)["sourceVersion"] != "v1.1.50" {
		t.Fatal("恢复应保留同一数据的迁移报告")
	}
	if config["automaticTasksPaused"] != true {
		t.Fatal("恢复备份后应等待人工检查再启用自动任务")
	}
	subscriptions, err := store.ListSubscriptions(ctx)
	if err != nil || len(subscriptions) != 1 || subscriptions[0]["title"] != "快照作品" {
		t.Fatalf("restored subscriptions=%#v err=%v", subscriptions, err)
	}
	data, err := os.ReadFile(attachment)
	if err != nil || string(data) != "cover-at-backup" {
		t.Fatalf("restored attachment=%q err=%v", data, err)
	}
}

func TestImportBackupConvertsLegacyJavaZipIntoSQLite(t *testing.T) {
	api, store, key := newAPI(t, filesapi.Options{})
	var legacy bytes.Buffer
	archive := zip.NewWriter(&legacy)
	config, _ := archive.Create("config.v2.json")
	_, _ = config.Write([]byte(`{"siteTitle":"legacy backup"}`))
	subscriptions, _ := archive.Create("ani.v2.json")
	_, _ = subscriptions.Write([]byte(`[{"id":"legacy-1","title":"Legacy Show","enable":true}]`))
	_ = archive.Close()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("file", "ani-rss.backup.3.x.zip")
	_, _ = part.Write(legacy.Bytes())
	_ = writer.Close()
	response := authorizedRequest(api, key, http.MethodPost, "/api/importBackup", &body, writer.FormDataContentType())
	if !strings.Contains(response.Body.String(), `"code":200`) {
		t.Fatalf("legacy backup import response=%s", response.Body.String())
	}
	configValue, err := store.GetConfig(context.Background())
	if err != nil || configValue["siteTitle"] != "legacy backup" || configValue["automaticTasksPaused"] != true {
		t.Fatalf("legacy config=%#v err=%v", configValue, err)
	}
	if configValue["migrationReport"].(map[string]any)["sourceVersion"] != "v3.2.52" {
		t.Fatal("页面导入后应能读取对应迁移报告")
	}
	items, err := store.ListSubscriptions(context.Background())
	if err != nil || len(items) != 1 || items[0]["title"] != "Legacy Show" {
		t.Fatalf("legacy subscriptions=%#v err=%v", items, err)
	}
}

func TestAutomaticBackupReconcilesCompletedArchiveAfterRetry(t *testing.T) {
	api, store, _ := newAPI(t, filesapi.Options{})
	service, err := filesapi.New(filesapi.Options{Store: store, SnapshotBarrier: func(context.Context) (func(), error) { return func() {}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	if done, err := service.ReconcileBackup(context.Background(), "job-1"); err != nil || done {
		t.Fatalf("missing backup reconciliation: done=%v err=%v", done, err)
	}
	if err := service.Backup(context.Background(), "job-1"); err != nil {
		t.Fatal(err)
	}
	if done, err := service.ReconcileBackup(context.Background(), "job-1"); err != nil || !done {
		t.Fatalf("completed archive reconciliation: done=%v err=%v", done, err)
	}
	if err := service.Backup(context.Background(), "job-1"); err != nil {
		t.Fatalf("repeating a recovered backup must keep its completed archive: %v", err)
	}
	_ = api
}

func TestBackupBarrierKeepsSQLiteAndAttachmentsOnOneSideOfConcurrentWrite(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveConfig(context.Background(), map[string]any{"snapshotValue": "before"}); err != nil {
		t.Fatal(err)
	}
	attachment := filepath.Join(store.DataDir(), "files", "asset.txt")
	if err := os.MkdirAll(filepath.Dir(attachment), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(attachment, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	barrier := &writeBarrier{entered: make(chan struct{}), resume: make(chan struct{})}
	service, err := filesapi.New(filesapi.Options{Store: store, SnapshotBarrier: barrier.freeze})
	if err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(store.DataDir(), "backup", "auto-controlled.zip")
	backupDone := make(chan error, 1)
	go func() { backupDone <- service.Backup(context.Background(), "controlled") }()
	select {
	case <-barrier.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("backup did not acquire the application write barrier")
	}
	writeDone := make(chan struct{})
	writeStarted := make(chan struct{})
	go func() {
		close(writeStarted)
		barrier.write(func() {
			_ = store.SaveConfig(context.Background(), map[string]any{"snapshotValue": "after"})
			_ = os.WriteFile(attachment, []byte("after"), 0600)
		})
		close(writeDone)
	}()
	<-writeStarted
	select {
	case <-writeDone:
		t.Fatal("business write crossed the held snapshot barrier")
	default:
	}
	close(barrier.resume)
	if err := <-backupDone; err != nil {
		t.Fatal(err)
	}
	select {
	case <-writeDone:
	case <-time.After(5 * time.Second):
		t.Fatal("business write did not resume after backup released its barrier")
	}
	archive, err := zip.OpenReader(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	var snapshotDB string
	var snapshotAsset string
	for _, entry := range archive.File {
		switch entry.Name {
		case "state.db":
			snapshotDB = filepath.Join(t.TempDir(), "snapshot.db")
			reader, _ := entry.Open()
			data, readErr := io.ReadAll(reader)
			_ = reader.Close()
			if readErr != nil {
				t.Fatal(readErr)
			}
			if err := os.WriteFile(snapshotDB, data, 0600); err != nil {
				t.Fatal(err)
			}
		case "files/asset.txt":
			reader, _ := entry.Open()
			data, readErr := io.ReadAll(reader)
			_ = reader.Close()
			if readErr != nil {
				t.Fatal(readErr)
			}
			snapshotAsset = string(data)
		}
	}
	_ = archive.Close()
	if snapshotDB == "" || snapshotAsset != "before" {
		t.Fatalf("snapshot asset=%q database=%q", snapshotAsset, snapshotDB)
	}
	snapshotStore, err := storage.Open(snapshotDB)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshotStore.Close()
	config, err := snapshotStore.GetConfig(context.Background())
	if err != nil || config["snapshotValue"] != "before" {
		t.Fatalf("snapshot database=%#v err=%v", config, err)
	}
}

func TestBackupRefusesToClaimConsistencyWithoutApplicationWriteBarrier(t *testing.T) {
	_, store, _ := newAPI(t, filesapi.Options{})
	service, err := filesapi.New(filesapi.Options{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Backup(context.Background(), "unfenced"); err == nil || !strings.Contains(err.Error(), "业务写屏障") {
		t.Fatalf("backup without an app write barrier must fail clearly: %v", err)
	}
}

func TestProxyImageUsesValidatedExternalHostAndCachesResponse(t *testing.T) {
	requests := 0
	api, _, key := newAPI(t, filesapi.Options{
		ResolveHost: func(context.Context, string) ([]net.IP, error) { return []net.IP{net.ParseIP("93.184.216.34")}, nil },
		HTTPDoer: doerFunc(func(request *http.Request) (*http.Response, error) {
			requests++
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"image/png"}}, Body: io.NopCloser(strings.NewReader("png-data")), Request: request}, nil
		}),
	})
	encoded := base64.StdEncoding.EncodeToString([]byte("https://images.example/cover.png"))
	for n := 0; n < 2; n++ {
		response := authorizedRequest(api, key, http.MethodGet, "/api/proxyImage?imgUrl="+url.QueryEscape(encoded), nil, "")
		if response.Code != http.StatusOK || response.Body.String() != "png-data" {
			t.Fatalf("proxy response=%d %q", response.Code, response.Body.String())
		}
	}
	if requests != 1 {
		t.Fatalf("cached image should be fetched once, got %d requests", requests)
	}
}

func TestDownloadLogsCreatesReadableZip(t *testing.T) {
	api, store, key := newAPI(t, filesapi.Options{})
	logDir := filepath.Join(store.DataDir(), "logs")
	if err := os.MkdirAll(logDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logDir, "app.log"), []byte("line one\n"), 0600); err != nil {
		t.Fatal(err)
	}
	response := authorizedRequest(api, key, http.MethodGet, "/api/downloadLogs", nil, "")
	archive, err := zip.NewReader(bytes.NewReader(response.Body.Bytes()), int64(response.Body.Len()))
	if err != nil || len(archive.File) != 1 {
		t.Fatalf("log archive entries=%v err=%v", archive, err)
	}
	file, err := archive.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(file)
	_ = file.Close()
	if err != nil || string(data) != "line one\n" {
		t.Fatalf("log archive content=%q err=%v", data, err)
	}
}

func TestImportBackupRejectsTraversalZipWithoutTouchingExistingState(t *testing.T) {
	api, store, key := newAPI(t, filesapi.Options{})
	config, err := store.GetConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	config["siteTitle"] = "unchanged"
	if err := store.SaveConfig(context.Background(), config); err != nil {
		t.Fatal(err)
	}
	var archiveBytes bytes.Buffer
	archive := zip.NewWriter(&archiveBytes)
	manifest, _ := archive.Create("backend-backup.json")
	_, _ = manifest.Write([]byte(`{"format":"backend-backup","version":1,"database":"state.db","assetRoots":["files","torrents","webui"]}`))
	entry, _ := archive.Create("../state.db")
	_, _ = entry.Write([]byte("bad"))
	_ = archive.Close()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("file", "evil.zip")
	_, _ = part.Write(archiveBytes.Bytes())
	_ = writer.Close()
	response := authorizedRequest(api, key, http.MethodPost, "/api/importBackup", &body, writer.FormDataContentType())
	if !strings.Contains(response.Body.String(), `"code":400`) {
		t.Fatalf("invalid restore response=%s", response.Body.String())
	}
	config, err = store.GetConfig(context.Background())
	if err != nil || config["siteTitle"] != "unchanged" {
		t.Fatalf("failed restore changed existing state: %#v %v", config, err)
	}
}

func TestRestoreRollsBackAssetsWhenSnapshotSchemaDoesNotMatch(t *testing.T) {
	api, store, key := newAPI(t, filesapi.Options{})
	config, err := store.GetConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	config["siteTitle"] = "keep current"
	if err := store.SaveConfig(context.Background(), config); err != nil {
		t.Fatal(err)
	}
	asset := filepath.Join(store.DataDir(), "files", "cover.png")
	if err := os.MkdirAll(filepath.Dir(asset), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(asset, []byte("current cover"), 0600); err != nil {
		t.Fatal(err)
	}
	incompatiblePath := filepath.Join(t.TempDir(), "incompatible.db")
	incompatibleStore, err := storage.Open(incompatiblePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := incompatibleStore.Close(); err != nil {
		t.Fatal(err)
	}
	incompatibleDB, err := sql.Open("sqlite", incompatiblePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := incompatibleDB.Exec(`CREATE TABLE unexpected_snapshot_table(id TEXT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if err := incompatibleDB.Close(); err != nil {
		t.Fatal(err)
	}
	databaseData, err := os.ReadFile(incompatiblePath)
	if err != nil {
		t.Fatal(err)
	}
	var archiveBytes bytes.Buffer
	archive := zip.NewWriter(&archiveBytes)
	manifest, _ := archive.Create("backend-backup.json")
	_, _ = manifest.Write([]byte(`{"format":"backend-backup","version":1,"database":"state.db","assetRoots":["files","torrents","webui"]}`))
	database, _ := archive.Create("state.db")
	_, _ = database.Write(databaseData)
	cover, _ := archive.Create("files/cover.png")
	_, _ = cover.Write([]byte("replacement cover"))
	_ = archive.Close()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("file", "invalid-state.zip")
	_, _ = part.Write(archiveBytes.Bytes())
	_ = writer.Close()
	response := authorizedRequest(api, key, http.MethodPost, "/api/importBackup", &body, writer.FormDataContentType())
	if !strings.Contains(response.Body.String(), `"code":500`) {
		t.Fatalf("invalid DB restore response=%s", response.Body.String())
	}
	config, err = store.GetConfig(context.Background())
	if err != nil || config["siteTitle"] != "keep current" {
		t.Fatalf("failed restore changed SQLite state: %#v err=%v", config, err)
	}
	data, err := os.ReadFile(asset)
	if err != nil || string(data) != "current cover" {
		t.Fatalf("failed restore did not roll back attachments: %q err=%v", data, err)
	}
}
