package app_test

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/app"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/storage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestRuntimeImageAndBackupRestoreUseAuthenticatedRoutes(t *testing.T) {
	dir := t.TempDir()
	events, stop := start(t, "serve", "-data-dir", dir, "-listen", "localhost:0")
	defer stop()
	address := events["address"].(string)
	token := login(t, address)
	upload := func(path, name string, data []byte) map[string]any {
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		part, err := form.CreateFormFile("file", name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write(data)
		_ = form.Close()
		req, err := http.NewRequest(http.MethodPost, "http://"+address+path, &body)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", form.FormDataContentType())
		req.Header.Set("Authorization", token)
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var result map[string]any
		if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
			t.Fatal(err)
		}
		if result["code"] != float64(200) {
			t.Fatalf("上传/恢复入口未接通：%v", result)
		}
		return result
	}
	uploaded := upload("/api/upload", "fixture.png", []byte("0123456789"))
	filename := uploaded["data"].(string)
	fileURL := "http://" + address + "/api/file?s=" + url.QueryEscape(token) + "&filename=" + url.QueryEscape(base64.StdEncoding.EncodeToString([]byte(filename)))
	request, _ := http.NewRequest(http.MethodGet, fileURL, nil)
	request.Header.Set("Range", "bytes=2-5")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != 200 || string(data) != "0123456789" {
		t.Fatalf("图片附件未接通：%d %q", response.StatusCode, data)
	}
	post(t, address, "/api/setConfig", token, map[string]any{"siteTitle": "归档标题"})
	backup, err := http.Get("http://" + address + "/api/exportBackup?s=" + url.QueryEscape(token))
	if err != nil {
		t.Fatal(err)
	}
	archive, err := io.ReadAll(backup.Body)
	_ = backup.Body.Close()
	if err != nil || !strings.Contains(backup.Header.Get("Content-Type"), "zip") {
		t.Fatalf("导出入口未接通：%v %s", err, archive)
	}
	post(t, address, "/api/setConfig", token, map[string]any{"siteTitle": "后续标题"})
	if err := os.Remove(filepath.Join(dir, "files", filepath.FromSlash(filename))); err != nil {
		t.Fatal(err)
	}
	upload("/api/importBackup", "backup.zip", archive)
	config := post(t, address, "/api/config", token, nil)["data"].(map[string]any)
	if config["siteTitle"] != "归档标题" || config["automaticTasksPaused"] != false {
		t.Fatalf("恢复状态未正确接通：%v", config)
	}
	restored, err := http.Get(fileURL)
	if err != nil {
		t.Fatal(err)
	}
	data, _ = io.ReadAll(restored.Body)
	_ = restored.Body.Close()
	if string(data) != "0123456789" {
		t.Fatalf("附件未随数据库恢复：%q", data)
	}
}

func TestRuntimeNotificationTestSendsConfiguredWebhook(t *testing.T) {
	delivered := make(chan string, 1)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		delivered <- string(body)
		w.WriteHeader(204)
	}))
	defer origin.Close()
	events, stop := start(t, "serve", "-data-dir", t.TempDir(), "-listen", "localhost:0")
	defer stop()
	address := events["address"].(string)
	token := login(t, address)
	defaults := post(t, address, "/api/newNotification", token, nil)
	if defaults["code"] != float64(200) {
		t.Fatalf("通知配置入口未接通：%v", defaults)
	}
	result := post(t, address, "/api/testNotification", token, map[string]any{"notificationType": "WEB_HOOK", "webHookUrl": origin.URL, "webHookMethod": "POST", "notificationTemplate": "${title} ${text}", "webHookBody": `{"text":"${text}"}`})
	if result["code"] != float64(200) {
		t.Fatalf("通知发送入口未接通：%v", result)
	}
	select {
	case body := <-delivered:
		if !strings.Contains(body, "test") {
			t.Fatal(body)
		}
	case <-time.After(time.Second):
		t.Fatal("没有调用配置的Webhook")
	}
}

func TestCompletedDownloadNotifiesWhenRenameAndCleanupAreDisabled(t *testing.T) {
	dir := t.TempDir()
	mediaPath := filepath.Join(dir, "media", "Example")
	delivered := make(chan string, 1)
	var mu sync.Mutex
	labels := []string{"ani-rss"}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/notify" {
			body, _ := io.ReadAll(r.Body)
			delivered <- string(body)
			w.WriteHeader(204)
			return
		}
		var request struct {
			Method    string `json:"method"`
			Arguments struct {
				Labels []string `json:"labels"`
			} `json:"arguments"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		mu.Lock()
		defer mu.Unlock()
		arguments := map[string]any{}
		switch request.Method {
		case "torrent-get":
			arguments["torrents"] = []any{map[string]any{"hashString": "1111111111111111111111111111111111111111", "name": "Example S01E01", "downloadDir": mediaPath, "labels": labels, "status": 6, "isFinished": true, "totalSize": 100, "haveValid": 100, "files": []any{}}}
		case "torrent-set":
			labels = request.Arguments.Labels
		default:
			t.Errorf("unexpected RPC %s", request.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"result": "success", "arguments": arguments})
	}))
	defer origin.Close()
	store, err := storage.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	config := map[string]any{"automaticTasksPaused": false, "rss": false, "rename": false, "delete": false, "completed": false, "autoDisabled": false, "scrape": false, "downloadToolHost": origin.URL, "downloadPathTemplate": mediaPath, "renameSleepSeconds": 1, "notificationConfigList": []any{map[string]any{"notificationType": "WEB_HOOK", "enable": true, "statusList": []string{"DOWNLOAD_END"}, "retry": 1, "webHookUrl": origin.URL + "/notify", "webHookMethod": "POST", "webHookBody": "${text}", "notificationTemplate": "${text}"}}}
	if err := store.SaveConfig(context.Background(), config); err != nil {
		t.Fatal(err)
	}
	if err := store.PutSubscription(context.Background(), map[string]any{"id": "finished", "title": "Example", "season": 1, "enable": false, "message": true}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	_, stop := start(t, "serve", "-data-dir", dir, "-listen", "localhost:0")
	defer stop()
	select {
	case text := <-delivered:
		if text != "Example S01E01 下载完成" {
			t.Fatal(text)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("关闭重命名和清理不应阻止下载完成通知")
	}
}

func TestRuntimeConnectsCollectionPreviewAndCoverRoutes(t *testing.T) {
	metainfo, err := os.ReadFile("../torrent/testdata/v1-multifile-utf8.torrent")
	if err != nil {
		t.Fatal(err)
	}
	events, stop := start(t, "serve", "-data-dir", t.TempDir(), "-listen", "localhost:0")
	defer stop()
	address := events["address"].(string)
	token := login(t, address)
	preview := post(t, address, "/api/previewCollection", token, map[string]any{"torrent": base64.StdEncoding.EncodeToString(metainfo), "ani": map[string]any{"title": "作品", "season": 1, "subgroup": "字幕组"}})
	if preview["code"] != float64(200) {
		t.Fatalf("合集入口未接通：%v", preview)
	}
	files := preview["data"].([]any)
	if len(files) != 1 || files[0].(map[string]any)["reName"] != "[字幕组] 作品 S01E01.mkv" {
		t.Fatal(preview)
	}
	cover := post(t, address, "/api/refreshCover", token, map[string]any{})
	if cover["code"] != float64(200) || cover["data"] != "cover.png" {
		t.Fatalf("媒体封面入口未接通：%v", cover)
	}
}

func TestRuntimeMountsMCPWithAPIKeyAndStoredSubscriptions(t *testing.T) {
	t.Setenv("MCP_ENABLED", "true")
	events, stop := start(t, "serve", "-data-dir", t.TempDir(), "-listen", "localhost:0")
	defer stop()
	address := events["address"].(string)
	token := login(t, address)
	post(t, address, "/api/setConfig", token, map[string]any{"apiKey": "runtime-fixture-key"})
	post(t, address, "/api/addAni", token, map[string]any{"title": "MCP 实际订阅", "season": 1})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "runtime-fixture", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: "http://" + address + "/api/mcp?s=runtime-fixture-key"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	tools, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil || len(tools.Tools) != 9 {
		t.Fatalf("MCP工具未接通：%v %v", tools, err)
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_subscriptions", Arguments: map[string]any{"dto": map[string]any{}}})
	if err != nil || result.IsError || len(result.Content) == 0 {
		t.Fatalf("MCP订阅查询失败：%v %v", result, err)
	}
	data, _ := json.Marshal(result.Content)
	if !bytes.Contains(data, []byte("MCP 实际订阅")) {
		t.Fatalf("MCP未使用共享存储：%s", data)
	}
}

func TestRuntimeMCPResolvesPreviewsAndCreatesSubscription(t *testing.T) {
	t.Setenv("MCP_ENABLED", "true")
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/subjects/123", "/bgm/subjects/123":
			_, _ = io.WriteString(w, `{"id":123,"name":"Example JP","name_cn":"作品中文","date":"2025-04-06","eps":12,"total_episodes":12,"type":2,"tags":[{"name":"动画","count":12}],"rating":{"score":8},"images":{}}`)
		case "/v0/episodes":
			_, _ = io.WriteString(w, `{"data":[]}`)
		case "/3/search/tv":
			_, _ = io.WriteString(w, `{"results":[]}`)
		case "/rss":
			_, _ = io.WriteString(w, `<rss><channel><item><title>[Fansub] 作品中文 - 01</title><enclosure url="magnet:?xt=urn:btih:1111111111111111111111111111111111111111"/></item></channel></rss>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer origin.Close()
	events, stop := start(t, "serve", "-data-dir", t.TempDir(), "-listen", "localhost:0")
	defer stop()
	address := events["address"].(string)
	token := login(t, address)
	post(t, address, "/api/setConfig", token, map[string]any{"apiKey": "runtime-fixture-key", "bgmApi": origin.URL, "cacheBaseURL": origin.URL, "tmdbApi": origin.URL, "offset": false, "downloadPathTemplate": filepath.Join(t.TempDir(), "${title}")})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "runtime-fixture", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: "http://" + address + "/api/mcp?s=runtime-fixture-key"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	input := map[string]any{"dto": map[string]any{"type": "other", "url": origin.URL + "/rss", "bgmUrl": "https://bgm.tv/subject/123", "subgroup": "Fansub", "enable": false, "standbyRssList": []any{}}}
	preview, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "preview_subscription_items", Arguments: input})
	if err != nil || preview.IsError {
		t.Fatalf("MCP预览共享流程未接通：%v %v", preview, err)
	}
	data, _ := json.Marshal(preview.Content)
	if !bytes.Contains(data, []byte("S01E01")) {
		t.Fatalf("没有实际解析RSS：%s", data)
	}
	added, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "add_subscription", Arguments: input})
	if err != nil || added.IsError {
		t.Fatalf("MCP创建共享流程未接通：%v %v", added, err)
	}
	assertTitleListed(t, address, token, "作品中文 (2025)")
}

func TestRuntimeResumesPersistedRSSJobAndStopsWorker(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rss" {
			_, _ = io.WriteString(w, `<rss><channel></channel></rss>`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"result":"success","arguments":{"torrents":[]}}`)
	}))
	defer upstream.Close()
	store, err := storage.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveConfig(ctx, map[string]any{"automaticTasksPaused": false, "rss": false, "rename": false, "downloadToolHost": upstream.URL, "downloadPathTemplate": filepath.Join(dir, "media", "${title}")}); err != nil {
		t.Fatal(err)
	}
	if err := store.PutSubscription(ctx, map[string]any{"id": "queued", "title": "Example", "season": 1, "enable": true, "url": upstream.URL + "/rss"}); err != nil {
		t.Fatal(err)
	}
	job, err := store.EnqueueJob(ctx, "rss-refresh", "resume-fixture", json.RawMessage(`{"subscriptionId":"queued","automatic":false}`))
	if err != nil {
		t.Fatal(err)
	}
	_, stop := start(t, "serve", "-data-dir", dir, "-listen", "localhost:0")
	defer stop()
	deadline := time.Now().Add(3 * time.Second)
	for {
		state, err := store.GetJob(ctx, job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if state.State == "resolved" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("运行入口没有恢复持久任务：%+v", state)
		}
		time.Sleep(10 * time.Millisecond)
	}
	stop()
}

func TestCreatingEnabledSubscriptionSchedulesDownload(t *testing.T) {
	dir := t.TempDir()
	added := make(chan struct{}, 1)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rss" {
			_, _ = io.WriteString(w, `<rss><channel><item><title>Example - 01</title><enclosure url="magnet:?xt=urn:btih:1111111111111111111111111111111111111111"/></item></channel></rss>`)
			return
		}
		var request struct {
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		if request.Method == "torrent-add" {
			select {
			case added <- struct{}{}:
			default:
			}
			_, _ = io.WriteString(w, `{"result":"success","arguments":{"torrent-added":{"hashString":"1111111111111111111111111111111111111111"}}}`)
			return
		}
		_, _ = io.WriteString(w, `{"result":"success","arguments":{"torrents":[]}}`)
	}))
	defer origin.Close()
	store, err := storage.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveConfig(context.Background(), map[string]any{"automaticTasksPaused": false, "rss": false, "rename": false, "downloadToolHost": origin.URL, "downloadPathTemplate": filepath.Join(dir, "media", "${title}")}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	events, stop := start(t, "serve", "-data-dir", dir, "-listen", "localhost:0")
	defer stop()
	address := events["address"].(string)
	created := post(t, address, "/api/addAni", login(t, address), map[string]any{"title": "Example", "season": 1, "enable": true, "url": origin.URL + "/rss"})
	if created["code"] != float64(200) {
		t.Fatal(created)
	}
	select {
	case <-added:
	case <-time.After(3 * time.Second):
		t.Fatal("启用订阅保存后没有调度下载")
	}
	token := login(t, address)
	post(t, address, "/api/addAni", token, map[string]any{"title": "Disabled", "season": 1, "enable": false, "url": origin.URL + "/rss"})
	deadline := time.Now().Add(3 * time.Second)
	for {
		listed := post(t, address, "/api/listAni", token, nil)
		weeks := listed["data"].(map[string]any)["weekList"].([]any)
		for _, week := range weeks {
			for _, raw := range week.(map[string]any)["items"].([]any) {
				item := raw.(map[string]any)
				if item["title"] == "Disabled" && item["currentEpisodeNumber"] == float64(1) {
					if item["lastDownloadTime"] != float64(0) {
						t.Fatalf("仅预览进度应保留下载时间：%v", item)
					}
					return
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("禁用订阅创建后没有读取RSS进度")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestSubscriptionPathChangeAndDeletionExecutePersistedMediaWork(t *testing.T) {
	root := t.TempDir()
	dir, oldPath, newPath := filepath.Join(root, "data"), filepath.Join(root, "old"), filepath.Join(root, "new")
	if err := os.MkdirAll(oldPath, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldPath, "Example S01E01.mkv"), []byte("fixture media"), 0600); err != nil {
		t.Fatal(err)
	}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rss" {
			_, _ = io.WriteString(w, `<rss><channel></channel></rss>`)
			return
		}
		_, _ = io.WriteString(w, `{"result":"success","arguments":{"torrents":[]}}`)
	}))
	defer origin.Close()
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	store, err := storage.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveConfig(context.Background(), map[string]any{"automaticTasksPaused": false, "rss": false, "rename": false, "downloadToolHost": origin.URL, "downloadPathTemplate": oldPath}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	events, stop := start(t, "serve", "-data-dir", dir, "-listen", "localhost:0")
	defer stop()
	address := events["address"].(string)
	token := login(t, address)
	post(t, address, "/api/addAni", token, map[string]any{"id": "moving", "title": "Example", "season": 1, "enable": false, "url": origin.URL + "/rss"})
	changed := post(t, address, "/api/setAni?move=true", token, map[string]any{"id": "moving", "title": "Example", "season": 1, "customDownloadPath": true, "customDownloadPathTemplate": newPath})
	if changed["code"] != float64(200) {
		t.Fatalf("移动意图未保存：%v", changed)
	}
	deadline := time.Now().Add(4 * time.Second)
	for {
		if data, err := os.ReadFile(filepath.Join(newPath, "Example S01E01.mkv")); err == nil && string(data) == "fixture media" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("路径修改后媒体没有迁移")
		}
		time.Sleep(10 * time.Millisecond)
	}
	deleted := post(t, address, "/api/deleteAni?deleteFiles=true", token, []string{"moving"})
	if deleted["code"] != float64(200) {
		t.Fatalf("删除意图未保存：%v", deleted)
	}
	deadline = time.Now().Add(4 * time.Second)
	for {
		if _, err := os.Stat(newPath); errors.Is(err, os.ErrNotExist) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("订阅删除后持久任务没有清理媒体")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestSubscriptionMoveIntentSurvivesRestartAndPathConfigChange(t *testing.T) {
	root := t.TempDir()
	dir, oldPath, newPath := filepath.Join(root, "data"), filepath.Join(root, "old"), filepath.Join(root, "new")
	if err := os.MkdirAll(oldPath, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldPath, "Example S01E01.mkv"), []byte("restart media"), 0600); err != nil {
		t.Fatal(err)
	}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rss" {
			_, _ = io.WriteString(w, `<rss><channel></channel></rss>`)
			return
		}
		_, _ = io.WriteString(w, `{"result":"success","arguments":{"torrents":[]}}`)
	}))
	defer origin.Close()
	events, stop := start(t, "serve", "-data-dir", dir, "-listen", "localhost:0")
	address := events["address"].(string)
	token := login(t, address)
	post(t, address, "/api/setConfig", token, map[string]any{"downloadToolHost": origin.URL, "downloadPathTemplate": oldPath, "rss": false, "rename": false})
	post(t, address, "/api/addAni", token, map[string]any{"id": "restart-move", "title": "Example", "season": 1, "enable": false, "url": origin.URL + "/rss"})
	result := post(t, address, "/api/setAni?move=true", token, map[string]any{"id": "restart-move", "title": "Example", "season": 1, "customDownloadPath": true, "customDownloadPathTemplate": newPath})
	if result["code"] != float64(200) {
		t.Fatal(result)
	}
	stop()
	store, err := storage.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	config, err := store.GetConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	config["automaticTasksPaused"], config["downloadPathTemplate"] = false, filepath.Join(root, "unrelated")
	if err := store.SaveConfig(context.Background(), config); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	_, stop = start(t, "serve", "-data-dir", dir, "-listen", "localhost:0")
	defer stop()
	deadline := time.Now().Add(4 * time.Second)
	for {
		if data, err := os.ReadFile(filepath.Join(newPath, "Example S01E01.mkv")); err == nil && string(data) == "restart media" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("重启后没有按保存的源/目标路径恢复迁移")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestServeProvidesHTTPAndPreservesSubscriptionsAcrossRestart(t *testing.T) {
	dataDir := t.TempDir()
	events, stop := start(t, "serve", "-data-dir", dataDir, "-listen", "127.0.0.1:0")
	address := events["address"].(string)
	if !strings.HasPrefix(address, "127.0.0.1:") {
		t.Fatalf("unexpected listening address: %q", address)
	}

	token := login(t, address)
	post(t, address, "/api/addAni", token, map[string]any{"id": "persisted", "title": "持续保存", "season": 1})
	assertTitleListed(t, address, token, "持续保存")

	// 首个实例持锁期间，同一数据目录应拒绝第二个服务。
	var duplicateOutput bytes.Buffer
	if err := app.Run(context.Background(), []string{"serve", "-data-dir", dataDir, "-listen", "127.0.0.1:0"}, &duplicateOutput, &duplicateOutput); err == nil {
		t.Fatal("second process using the same data directory should fail")
	}

	stop()
	events, stop = start(t, "serve", "-data-dir", dataDir, "-listen", "127.0.0.1:0")
	defer stop()
	assertTitleListed(t, events["address"].(string), login(t, events["address"].(string)), "持续保存")
}

func TestRuntimeConnectsRSSPreviewAndDiscoveryRoutes(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rss" {
			w.Header().Set("Content-Type", "application/rss+xml")
			_, _ = w.Write([]byte(`<rss><channel><item><title>[Fansub] Example - 01 [1080p]</title><enclosure url="magnet:?xt=urn:btih:1111111111111111111111111111111111111111" length="1024"/></item></channel></rss>`))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/search/subject/") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"list":[{"id":"123","name":"示例作品","name_cn":"示例作品","date":"2025-04-06"}]}`))
			return
		}
		if r.URL.Path == "/v0/episodes" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":1,"ep":1,"sort":1,"type":0,"name":"Episode One","name_cn":"第一集标题"}],"total":1,"limit":1000,"offset":0}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer origin.Close()
	events, stop := start(t, "serve", "-data-dir", t.TempDir(), "-listen", "localhost:0")
	defer stop()
	address := events["address"].(string)
	token := login(t, address)
	post(t, address, "/api/setConfig", token, map[string]any{"bgmApi": origin.URL, "downloadPathTemplate": filepath.Join(t.TempDir(), "${title}", "Season ${season}")})
	preview := post(t, address, "/api/previewAni", token, map[string]any{"id": "preview", "title": "Example", "season": 1, "offset": 0, "url": origin.URL + "/rss", "subgroup": "Fansub", "exclude": []string{}, "match": []string{}, "ova": false})
	if preview["code"] != float64(200) {
		t.Fatalf("预览入口未接通：%v", preview)
	}
	items := preview["data"].(map[string]any)["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["reName"] != "[Fansub] Example S01E01" {
		t.Fatal(preview)
	}
	search := post(t, address, "/api/searchBgm?name=sample", token, nil)
	if search["code"] != float64(200) || len(search["data"].([]any)) != 1 {
		t.Fatalf("检索入口未接通：%v", search)
	}
	post(t, address, "/api/setConfig", token, map[string]any{"renameTemplate": "${title} E${episodeFormat} ${bgmEpisodeTitle}"})
	advanced := post(t, address, "/api/previewAni", token, map[string]any{"id": "preview", "title": "Example", "season": 1, "url": origin.URL + "/rss", "subgroup": "Fansub", "bgmUrl": "https://bgm.tv/subject/123", "jpTitle": "Example JP"})
	if advanced["code"] != float64(200) {
		t.Fatal(advanced)
	}
	if got := advanced["data"].(map[string]any)["items"].([]any)[0].(map[string]any)["reName"]; got != "Example E01 第一集标题" {
		t.Fatalf("高级命名元数据未接通：%v", got)
	}
}

func TestRuntimeConnectsSettingsAndGracefulStop(t *testing.T) {
	events, stop, done := startWithExit(t, "serve", "-data-dir", t.TempDir(), "-listen", "localhost:0")
	defer stop()
	address := events["address"].(string)
	token := login(t, address)
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatal(err)
	}
	post(t, address, "/api/setConfig", token, map[string]any{"ipWhitelist": true, "ipWhitelistStr": host})
	probe := post(t, address, "/api/testIpWhitelist", "", nil)
	if probe["code"] != float64(200) {
		t.Fatalf("白名单探测未接通：%v", probe)
	}
	result := post(t, address, "/api/stop?status=1", token, nil)
	if result["code"] != float64(200) {
		t.Fatalf("停止回调未接通：%v", result)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("停止接口应让运行入口正常退出")
	}
}

func TestRuntimeRestartReopensSameDataDirectory(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	dataDir := t.TempDir()
	go func() {
		done <- app.Run(ctx, []string{"serve", "-data-dir", dataDir, "-listen", "localhost:0"}, writer, io.Discard)
		close(done)
		_ = writer.Close()
	}()
	t.Cleanup(func() {
		cancel()
		_ = reader.Close()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("重启测试服务未退出")
		}
	})
	decoder := json.NewDecoder(reader)
	next := func() string {
		result := make(chan map[string]any, 1)
		failed := make(chan error, 1)
		go func() {
			var event map[string]any
			if err := decoder.Decode(&event); err != nil {
				failed <- err
				return
			}
			result <- event
		}()
		select {
		case event := <-result:
			return event["address"].(string)
		case err := <-failed:
			t.Fatal(err)
		case <-time.After(3 * time.Second):
			t.Fatal("重启后未重新监听")
		}
		return ""
	}
	first := next()
	token := login(t, first)
	post(t, first, "/api/addAni", token, map[string]any{"id": "restart-item", "title": "重启后保留", "season": 1})
	if result := post(t, first, "/api/stop?status=0", token, nil); result["code"] != float64(200) {
		t.Fatal(result)
	}
	second := next()
	assertTitleListed(t, second, login(t, second), "重启后保留")
	if result := post(t, second, "/api/stop?status=1", login(t, second), nil); result["code"] != float64(200) {
		t.Fatal(result)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("重启后的服务未正常停止")
	}
}

func TestPreviewDoesNotCreateDestinationAndImportReportsRedactedSummary(t *testing.T) {
	source := legacyDirectory(t)
	destination := filepath.Join(t.TempDir(), "not-created")
	var out, stderr bytes.Buffer
	if err := app.Run(context.Background(), []string{"preview", "-source", source, "-data-dir", destination}, &out, &stderr); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("preview created target data directory: %v", err)
	}
	report := decodeReport(t, out.String())
	if report["sourceVersion"] != "v3.2.52" || report["subscriptionCount"] != float64(1) || report["automaticTasksPaused"] != true {
		t.Fatalf("unexpected preview report: %#v", report)
	}

	out.Reset()
	if err := app.Run(context.Background(), []string{"import", "-source", source, "-data-dir", destination}, &out, &stderr); err != nil {
		t.Fatal(err)
	}
	report = decodeReport(t, out.String())
	if report["sourceVersion"] != "v3.2.52" || report["automaticTasksPaused"] != true {
		t.Fatalf("unexpected import report: %#v", report)
	}
	if strings.Contains(out.String(), "source-only-secret") || strings.Contains(out.String(), "username-secret") {
		t.Fatalf("report leaked configuration values: %s", out.String())
	}
}

func TestCLIHelpAndArgumentErrorsUseChinese(t *testing.T) {
	var output, stderr bytes.Buffer
	if err := app.Run(context.Background(), []string{"preview", "-h"}, &output, &stderr); err != nil {
		t.Fatalf("help should exit successfully: %v", err)
	}
	if !strings.Contains(stderr.String(), "用法：ani-rss preview") || !strings.Contains(stderr.String(), "旧版数据目录") {
		t.Fatalf("help should be in Simplified Chinese: %s", stderr.String())
	}
	if err := app.Run(context.Background(), []string{"preview", "-unknown"}, &output, &stderr); err == nil || !strings.Contains(err.Error(), "命令参数错误") {
		t.Fatalf("argument errors should be localized: %v", err)
	}
}

func TestImportRejectsDataDirectoryOverlappingLegacySource(t *testing.T) {
	for _, target := range []string{"same-directory", "nested-directory"} {
		t.Run(target, func(t *testing.T) {
			source := legacyDirectory(t)
			dataDir := source
			if target == "nested-directory" {
				dataDir = filepath.Join(source, "go-state")
			}
			var output bytes.Buffer
			err := app.Run(context.Background(), []string{"import", "-source", source, "-data-dir", dataDir}, &output, &output)
			if err == nil {
				t.Fatal("import must reject a target that overlaps the read-only source")
			}
			if _, err := os.Stat(filepath.Join(dataDir, "state.db")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("overlapping import created a target database: %v", err)
			}
			if _, err := os.Stat(filepath.Join(dataDir, "instance.lock")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("overlapping import created a lock in source: %v", err)
			}
		})
	}

	t.Run("archive-inside-target", func(t *testing.T) {
		dataDir := t.TempDir()
		source := filepath.Join(dataDir, "legacy.zip")
		file, err := os.Create(source)
		if err != nil {
			t.Fatal(err)
		}
		archive := zip.NewWriter(file)
		for name, content := range map[string]string{
			"config.v2.json": `{"downloadToolType":"Transmission"}`,
			"ani.v2.json":    `[]`,
		} {
			entry, err := archive.Create(name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := entry.Write([]byte(content)); err != nil {
				t.Fatal(err)
			}
		}
		if err := archive.Close(); err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		var output bytes.Buffer
		if err := app.Run(context.Background(), []string{"import", "-source", source, "-data-dir", dataDir}, &output, &output); err == nil {
			t.Fatal("import must reject an archive contained by its target directory")
		}
		for _, name := range []string{"state.db", "instance.lock"} {
			if _, err := os.Stat(filepath.Join(dataDir, name)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("overlapping import created %s: %v", name, err)
			}
		}
	})
}

func TestServeWithoutSubcommandAndWebAssets(t *testing.T) {
	dataDir := t.TempDir()
	webDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(webDir, "index.html"), []byte("ui shell"), 0600); err != nil {
		t.Fatal(err)
	}
	event, stop := start(t, "-data-dir", dataDir, "-listen", "127.0.0.1:0", "-web-dir", webDir)
	defer stop()
	address := event["address"].(string)
	response, err := http.Get("http://" + address + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if string(body) != "ui shell" {
		t.Fatalf("static response = %q", body)
	}
	stop()
}

func TestWebDirectoryDoesNotShadowCaseInsensitiveAPIRoutes(t *testing.T) {
	dataDir := t.TempDir()
	webDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(webDir, "apix"), []byte("static prefix"), 0600); err != nil {
		t.Fatal(err)
	}
	event, stop := start(t, "serve", "-data-dir", dataDir, "-listen", "127.0.0.1:0", "-web-dir", webDir)
	defer stop()
	address := event["address"].(string)

	apiResponse, err := http.Get("http://" + address + "/API/PING")
	if err != nil {
		t.Fatal(err)
	}
	defer apiResponse.Body.Close()
	var result map[string]any
	if err := json.NewDecoder(apiResponse.Body).Decode(&result); err != nil {
		t.Fatalf("case-insensitive API path was handled as a static file: %v", err)
	}
	if result["code"] != float64(200) {
		t.Fatalf("API response = %#v", result)
	}

	staticResponse, err := http.Get("http://" + address + "/apix")
	if err != nil {
		t.Fatal(err)
	}
	defer staticResponse.Body.Close()
	body, err := io.ReadAll(staticResponse.Body)
	if err != nil || string(body) != "static prefix" {
		t.Fatalf("/apix should remain a static path, got %q (%v)", body, err)
	}
}

func start(t *testing.T, args ...string) (map[string]any, func()) {
	events, stop, _ := startWithExit(t, args...)
	return events, stop
}

func startWithExit(t *testing.T, args ...string) (map[string]any, func(), <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx, args, writer, io.Discard); close(done); _ = writer.Close() }()
	line, err := bufio.NewReader(reader).ReadBytes('\n')
	if err != nil {
		t.Fatalf("runtime did not emit its listening event: %v", err)
	}
	var event map[string]any
	if err := json.Unmarshal(line, &event); err != nil {
		t.Fatalf("decode runtime event %q: %v", line, err)
	}
	if event["type"] != "listening" || event["address"] == "" {
		t.Fatalf("unexpected runtime event: %#v", event)
	}
	var stopped bool
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		cancel()
		select {
		case err := <-done:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Errorf("runtime failed: %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Error("runtime did not stop")
		}
		_ = reader.Close()
	}
	t.Cleanup(stop)
	return event, stop, done
}

func login(t *testing.T, address string) string {
	t.Helper()
	hash := md5.Sum([]byte("admin"))
	result := post(t, address, "/api/login", "", map[string]any{"username": "admin", "password": hex.EncodeToString(hash[:])})
	token, _ := result["data"].(string)
	if result["code"] != float64(200) || token == "" {
		t.Fatalf("login failed: %#v", result)
	}
	return token
}

func post(t *testing.T, address, path, token string, body any) map[string]any {
	t.Helper()
	payload, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, "http://"+address+path, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", token)
	}
	response, err := http.DefaultClient.Do(req)
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

func assertTitleListed(t *testing.T, address, token, title string) {
	t.Helper()
	result := post(t, address, "/api/listAni", token, nil)
	data := result["data"].(map[string]any)
	for _, group := range data["weekList"].([]any) {
		for _, item := range group.(map[string]any)["items"].([]any) {
			if item.(map[string]any)["title"] == title {
				return
			}
		}
	}
	t.Fatalf("%q not found in subscription response: %s", title, fmt.Sprint(result))
}

func decodeReport(t *testing.T, raw string) map[string]any {
	t.Helper()
	var report map[string]any
	if err := json.Unmarshal([]byte(raw), &report); err != nil {
		t.Fatalf("decode report %q: %v", raw, err)
	}
	return report
}

func legacyDirectory(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.v2.json"), []byte(`{"downloadToolType":"Transmission","downloadToolPassword":"source-only-secret","downloadToolUsername":"username-secret"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ani.v2.json"), []byte(`[{"id":"fixture","title":"fixture show","season":1}]`), 0600); err != nil {
		t.Fatal(err)
	}
	return dir
}
