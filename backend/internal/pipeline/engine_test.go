package pipeline_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/pipeline"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/server"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/storage"
)

const sampleHash = "0123456789abcdef0123456789abcdef01234567"

func TestManualRefreshAddsMatchingRSSItemOnlyOnce(t *testing.T) {
	ctx := context.Background()
	torrentBytes := []byte("d4:infod4:name4:testee")
	var torrentURL string
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(`<rss version="2.0"><channel><item><title>[Demo] Example Show - 01 [1080p]</title><enclosure url="` + torrentURL + `/episode.torrent" length="25"/><guid>` + sampleHash + `</guid><pubDate>Mon, 01 Jan 2024 12:00:00 GMT</pubDate></item></channel></rss>`))
	}))
	defer feed.Close()

	var mu sync.Mutex
	var calls []string
	torrents := []map[string]any{}
	rpc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Transmission-Session-Id") == "" {
			w.Header().Set("X-Transmission-Session-Id", "fixture-session")
			w.WriteHeader(http.StatusConflict)
			return
		}
		var request struct {
			Method    string         `json:"method"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode RPC request: %v", err)
			return
		}
		mu.Lock()
		calls = append(calls, request.Method)
		switch request.Method {
		case "torrent-get":
			writeRPC(w, map[string]any{"torrents": torrents})
		case "torrent-add":
			if request.Arguments["download-dir"] == "" || request.Arguments["metainfo"] == "" {
				t.Errorf("torrent-add missing directory/metainfo: %#v", request.Arguments)
			}
			torrents = []map[string]any{{
				"id": 7, "hashString": sampleHash, "name": "Example Show", "status": 4,
				"downloadDir": request.Arguments["download-dir"], "labels": request.Arguments["labels"],
				"isFinished": false, "totalSize": int64(100), "haveValid": int64(10), "files": []any{},
			}}
			writeRPC(w, map[string]any{"torrent-added": map[string]any{"hashString": sampleHash}})
		case "session-get":
			writeRPC(w, map[string]any{"version": "4.0.6", "rpc-version": 17})
		default:
			t.Errorf("unexpected RPC method %q", request.Method)
			writeRPC(w, map[string]any{})
		}
		mu.Unlock()
	}))
	defer rpc.Close()
	torrentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-bittorrent")
		_, _ = w.Write(torrentBytes)
	}))
	defer torrentServer.Close()
	torrentURL = torrentServer.URL

	dataDir := t.TempDir()
	store, err := storage.Open(filepath.Join(dataDir, "ani-rss.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveConfig(ctx, map[string]any{
		"apiKey": "fixture-api-key", "automaticTasksPaused": false,
		"downloadToolHost": rpc.URL, "downloadToolUsername": "fixture-user", "downloadToolPassword": "fixture-password",
		"downloadPathTemplate": filepath.Join(dataDir, "downloads", "${title}", "Season ${season}"),
		"rssTimeout":           float64(5), "downloadRetry": float64(1), "downloadCount": float64(0),
		"standbyRss": false, "coexist": false, "fileExist": false,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.PutSubscription(ctx, map[string]any{
		"id": "sub-1", "url": feed.URL + "/rss.xml", "title": "Example Show", "season": float64(1),
		"subgroup": "Demo", "offset": float64(0), "releaseDate": "2024-01-01", "enable": true,
		"ova": false, "globalExclude": false, "match": []any{}, "exclude": []any{}, "standbyRssList": []any{},
		"notDownload": []any{}, "downloadNew": false,
	}); err != nil {
		t.Fatal(err)
	}
	engine, err := pipeline.New(store, pipeline.Options{})
	if err != nil {
		t.Fatal(err)
	}
	app, err := server.New(ctx, store)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Register(engine.Routes()...); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		request := httptest.NewRequest(http.MethodPost, "/api/refreshAni", strings.NewReader(`{"id":"sub-1"}`))
		request.Header.Set("x-api-key", "fixture-api-key")
		response := httptest.NewRecorder()
		app.ServeHTTP(response, request)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "已开始刷新RSS") {
			t.Fatalf("refresh response = %d %s", response.Code, response.Body.String())
		}
	}
	refreshAll := httptest.NewRequest(http.MethodPost, "/api/refreshAll", nil)
	refreshAll.Header.Set("x-api-key", "fixture-api-key")
	refreshAllResponse := httptest.NewRecorder()
	app.ServeHTTP(refreshAllResponse, refreshAll)
	if refreshAllResponse.Code != http.StatusOK || !strings.Contains(refreshAllResponse.Body.String(), "已开始刷新RSS") {
		t.Fatalf("refresh all response = %d %s", refreshAllResponse.Code, refreshAllResponse.Body.String())
	}
	previewBody := `{"id":"sub-1","url":"` + feed.URL + `/rss.xml","title":"Example Show","season":1,"releaseDate":"2024-01-01","enable":true,"subgroup":"Demo","offset":0,"ova":false,"globalExclude":false,"match":[],"exclude":[],"standbyRssList":[],"notDownload":[],"downloadNew":false}`
	previewResponse := postAPI(t, app, "/api/previewAni", previewBody)
	previewData := previewResponse["data"].(map[string]any)
	previewItems := previewData["items"].([]any)
	if len(previewItems) != 1 {
		t.Fatalf("preview items = %#v", previewItems)
	}
	previewItem := previewItems[0].(map[string]any)
	if previewItem["hasDownloaded"] != true || !strings.Contains(previewItem["torrentPath"].(string), sampleHash) {
		t.Fatalf("preview did not return the cached torrent: %#v", previewItem)
	}
	if _, ok := previewItem["pubDate"].(string); !ok {
		t.Fatalf("pubDate must remain a JSON string: %#v", previewItem["pubDate"])
	}
	pathResponse := postAPI(t, app, "/api/downloadPath", previewBody)
	pathData := pathResponse["data"].(map[string]any)
	if pathData["downloadPath"] != filepath.Join(dataDir, "downloads", "Example Show", "Season 1") {
		t.Fatalf("downloadPath = %#v", pathData)
	}
	listResponse := postAPI(t, app, "/api/torrentsInfos", "")
	list := listResponse["data"].([]any)
	if len(list) != 1 {
		t.Fatalf("torrent list = %#v", list)
	}
	listed := list[0].(map[string]any)
	if listed["hash"] != sampleHash || listed["state"] != "downloading" || listed["progress"] != float64(10) {
		t.Fatalf("torrent list item = %#v", listed)
	}
	loginResponse := postAPI(t, app, "/api/downloadLoginTest", `{"downloadToolType":"Transmission","downloadToolHost":"`+rpc.URL+`","downloadToolUsername":"fixture-user","downloadToolPassword":"fixture-password"}`)
	if loginResponse["message"] != "登录成功" {
		t.Fatalf("download login test response = %#v", loginResponse)
	}
	mu.Lock()
	defer mu.Unlock()
	addCount := 0
	for _, method := range calls {
		if method == "torrent-add" {
			addCount++
		}
	}
	if addCount != 1 {
		t.Fatalf("torrent-add count = %d, RPC calls %v", addCount, calls)
	}
}

func TestPreviewMergesPrimaryAndStandbyWithScopedRules(t *testing.T) {
	ctx := context.Background()
	feeds := map[string]string{
		"primary": `<rss><channel><item><title>[主组] Demo Show - 01</title><enclosure url="https://example.invalid/primary-01.torrent"/><guid>` + sampleHash + `</guid></item><item><title>[主组] Demo Show - 02</title><enclosure url="https://example.invalid/primary-02.torrent"/><guid>1111111111111111111111111111111111111111</guid></item></channel></rss>`,
		"standby": `<rss><channel><item><title>[备用组] Demo Show - 00</title><enclosure url="https://example.invalid/standby-00.torrent"/><guid>2222222222222222222222222222222222222222</guid></item><item><title>[备用组] Demo Show - 02</title><enclosure url="https://example.invalid/standby-02.torrent"/><guid>3333333333333333333333333333333333333333</guid></item></channel></rss>`,
	}
	store, err := storage.Open(filepath.Join(t.TempDir(), "ani-rss.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveConfig(ctx, map[string]any{
		"standbyRss": true, "coexist": false, "downloadPathTemplate": filepath.Join(t.TempDir(), "${title}"),
		"renameTemplate": "${subgroup} ${episodeFormat}", "omit": true,
	}); err != nil {
		t.Fatal(err)
	}
	subscription := map[string]any{
		"id": "sub-merge", "url": "primary", "title": "Demo Show", "season": float64(1), "offset": float64(0),
		"subgroup": "主组", "releaseDate": "2024-01-01", "ova": false, "enable": true,
		"globalExclude": false, "match": []any{"{{主组}}:Demo", "{{无关组}}:不会命中"},
		"exclude": []any{"{{备用组}}:blocked"}, "omit": true,
		"standbyRssList": []any{map[string]any{"label": "备用组", "url": "standby", "offset": float64(1)}},
	}
	if err := store.PutSubscription(ctx, subscription); err != nil {
		t.Fatal(err)
	}
	engine, err := pipeline.New(store, pipeline.Options{RSSFetcher: func(_ context.Context, source string) ([]byte, error) {
		body, exists := feeds[source]
		if !exists {
			return nil, fmt.Errorf("unexpected feed %q", source)
		}
		return []byte(body), nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := engine.Preview(ctx, subscription)
	if err != nil {
		t.Fatal(err)
	}
	items := preview["items"].([]pipeline.Item)
	if len(items) != 3 {
		t.Fatalf("merged items = %#v", items)
	}
	if items[0].Episode != 1 || !items[0].Master || items[0].Subgroup != "主组" {
		t.Fatalf("primary feed did not win duplicate episode: %#v", items[0])
	}
	if items[1].Episode != 2 || !items[1].Master {
		t.Fatalf("primary source order changed: %#v", items[1])
	}
	if items[2].Episode != 3 || items[2].Master || items[2].Subgroup != "备用组" {
		t.Fatalf("standby offset or order was wrong: %#v", items[2])
	}
}

func TestPausedRefreshDoesNotWriteRPCAndPreviewRemainsAvailable(t *testing.T) {
	ctx := context.Background()
	var rpcCalls atomic.Int64
	rpc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rpcCalls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer rpc.Close()
	store, err := storage.Open(filepath.Join(t.TempDir(), "ani-rss.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveConfig(ctx, map[string]any{
		"automaticTasksPaused": true, "downloadToolHost": rpc.URL,
		"downloadPathTemplate": filepath.Join(t.TempDir(), "${title}"),
	}); err != nil {
		t.Fatal(err)
	}
	subscription := map[string]any{
		"id": "paused-sub", "url": "fixture", "title": "Example Show", "season": float64(1), "offset": float64(0),
		"subgroup": "Demo", "enable": true, "ova": false, "globalExclude": false,
		"match": []any{}, "exclude": []any{}, "standbyRssList": []any{},
	}
	if err := store.PutSubscription(ctx, subscription); err != nil {
		t.Fatal(err)
	}
	engine, err := pipeline.New(store, pipeline.Options{RSSFetcher: func(_ context.Context, _ string) ([]byte, error) {
		return []byte(`<rss><channel><item><title>Demo Show - 01</title><enclosure url="https://example.invalid/one.torrent"/><guid>` + sampleHash + `</guid></item></channel></rss>`), nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.RefreshSubscription(ctx, "paused-sub"); !errors.Is(err, pipeline.ErrAutomaticTasksPaused) {
		t.Fatalf("paused refresh error = %v", err)
	}
	preview, err := engine.Preview(ctx, subscription)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview["items"].([]pipeline.Item)) != 1 {
		t.Fatalf("paused mode prevented preview: %#v", preview)
	}
	if rpcCalls.Load() != 0 {
		t.Fatalf("paused refresh called Transmission %d times", rpcCalls.Load())
	}
}

func TestRefreshHTTPReportsRSSFailureInsteadOfSuccess(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(filepath.Join(t.TempDir(), "ani-rss.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveConfig(ctx, map[string]any{"apiKey": "fixture-api-key", "automaticTasksPaused": false}); err != nil {
		t.Fatal(err)
	}
	if err := store.PutSubscription(ctx, map[string]any{"id": "rss-error", "url": "fixture", "title": "Example", "enable": true}); err != nil {
		t.Fatal(err)
	}
	engine, err := pipeline.New(store, pipeline.Options{RSSFetcher: func(context.Context, string) ([]byte, error) {
		return nil, errors.New("fixture RSS outage")
	}})
	if err != nil {
		t.Fatal(err)
	}
	app, err := server.New(ctx, store)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Register(engine.Routes()...); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/refreshAni", strings.NewReader(`{"id":"rss-error"}`))
	request.Header.Set("x-api-key", "fixture-api-key")
	response := httptest.NewRecorder()
	app.ServeHTTP(response, request)
	var result map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	message, _ := result["message"].(string)
	if response.Code != http.StatusOK || result["code"] != float64(500) || !strings.Contains(message, "fixture RSS outage") {
		t.Fatalf("RSS 失败应通过 Result 报出：HTTP %d %s", response.Code, response.Body.String())
	}
}

func TestDownloadDecisionHonorsForbiddenDelayAndActiveLimit(t *testing.T) {
	for _, scenario := range []string{"forbidden", "delayed", "active-limit", "external-active-limit"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			active := []map[string]any{}
			if scenario == "active-limit" || scenario == "external-active-limit" {
				labels := []string{"ani-rss"}
				if scenario == "external-active-limit" {
					labels = []string{"user-task"}
				}
				active = []map[string]any{{"id": 5, "hashString": "ffffffffffffffffffffffffffffffffffffffff", "name": "existing", "status": 4, "downloadDir": "/downloads", "labels": labels, "isFinished": false, "totalSize": 100, "haveValid": 50}}
			}
			addCount := 0
			rpc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-Transmission-Session-Id") == "" {
					w.Header().Set("X-Transmission-Session-Id", "fixture-session")
					w.WriteHeader(http.StatusConflict)
					return
				}
				var request struct {
					Method string `json:"method"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Errorf("decode RPC: %v", err)
					return
				}
				switch request.Method {
				case "torrent-get":
					writeRPC(w, map[string]any{"torrents": active})
				case "torrent-add":
					addCount++
					writeRPC(w, map[string]any{"torrent-added": map[string]any{"hashString": sampleHash}})
				default:
					t.Errorf("unexpected method %q", request.Method)
					writeRPC(w, map[string]any{})
				}
			}))
			defer rpc.Close()
			store, err := storage.Open(filepath.Join(t.TempDir(), "ani-rss.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			config := map[string]any{
				"automaticTasksPaused": false, "downloadToolHost": rpc.URL,
				"downloadPathTemplate": filepath.Join(t.TempDir(), "${title}"), "rename": false,
				"downloadCount": float64(0),
			}
			subscription := map[string]any{
				"id": "decision-sub", "url": "fixture", "title": "Example Show", "season": float64(1),
				"offset": float64(0), "subgroup": "Demo", "enable": true, "ova": false,
				"globalExclude": false, "match": []any{}, "exclude": []any{}, "standbyRssList": []any{},
				"notDownload": []any{}, "downloadNew": false,
			}
			if scenario == "forbidden" {
				subscription["notDownload"] = []any{float64(1)}
			}
			if scenario == "delayed" {
				config["delayedDownload"] = float64(30)
			}
			if scenario == "active-limit" || scenario == "external-active-limit" {
				config["downloadCount"] = float64(1)
			}
			if err := store.SaveConfig(ctx, config); err != nil {
				t.Fatal(err)
			}
			if err := store.PutSubscription(ctx, subscription); err != nil {
				t.Fatal(err)
			}
			engine, err := pipeline.New(store, pipeline.Options{
				Now: func() time.Time { return time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC) },
				RSSFetcher: func(context.Context, string) ([]byte, error) {
					return []byte(`<rss><channel><item><title>Demo Show - 01</title><enclosure url="https://example.invalid/one.torrent"/><guid>` + sampleHash + `</guid><pubDate>Mon, 01 Jan 2024 13:00:00 GMT</pubDate></item></channel></rss>`), nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := engine.RefreshSubscription(ctx, "decision-sub"); err != nil {
				t.Fatal(err)
			}
			if addCount != 0 {
				t.Fatalf("%s decision added %d tasks", scenario, addCount)
			}
		})
	}
}

func TestManualRefreshAddsMagnetAndCachesItAsText(t *testing.T) {
	ctx := context.Background()
	addCount := 0
	torrents := []map[string]any{}
	rpc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Transmission-Session-Id") == "" {
			w.Header().Set("X-Transmission-Session-Id", "fixture-session")
			w.WriteHeader(http.StatusConflict)
			return
		}
		var request struct {
			Method    string         `json:"method"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode RPC: %v", err)
			return
		}
		switch request.Method {
		case "torrent-get":
			writeRPC(w, map[string]any{"torrents": torrents})
		case "torrent-add":
			addCount++
			if request.Arguments["filename"] != "magnet:?xt=urn:btih:"+sampleHash {
				t.Errorf("magnet RPC filename = %#v", request.Arguments["filename"])
			}
			torrents = append(torrents, map[string]any{"hashString": sampleHash, "labels": []string{"ani-rss"}, "status": 4, "totalSize": 1, "haveValid": 0})
			writeRPC(w, map[string]any{"torrent-added": map[string]any{"hashString": sampleHash}})
		default:
			t.Errorf("unexpected method %q", request.Method)
			writeRPC(w, map[string]any{})
		}
	}))
	defer rpc.Close()
	store, err := storage.Open(filepath.Join(t.TempDir(), "ani-rss.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveConfig(ctx, map[string]any{
		"automaticTasksPaused": false, "downloadToolHost": rpc.URL,
		"downloadPathTemplate": filepath.Join(t.TempDir(), "${title}"),
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.PutSubscription(ctx, map[string]any{
		"id": "magnet-sub", "url": "fixture", "title": "Demo Show", "season": float64(1),
		"offset": float64(0), "subgroup": "Demo", "enable": true, "ova": false, "globalExclude": false,
		"match": []any{}, "exclude": []any{}, "standbyRssList": []any{}, "notDownload": []any{},
	}); err != nil {
		t.Fatal(err)
	}
	engine, err := pipeline.New(store, pipeline.Options{RSSFetcher: func(context.Context, string) ([]byte, error) {
		return []byte(`<rss><channel><item><title>Demo Show - 01</title><enclosure url="magnet:?xt=urn:btih:` + sampleHash + `"/><guid>` + sampleHash + `</guid></item></channel></rss>`), nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.RefreshSubscription(ctx, "magnet-sub"); err != nil {
		t.Fatal(err)
	}
	if err := engine.RefreshSubscription(ctx, "magnet-sub"); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(store.DataDir(), "torrents", "*", "magnet-sub", sampleHash+".txt"))
	if err != nil || len(files) != 1 {
		t.Fatalf("magnet cache files = %v, %v", files, err)
	}
	data, err := os.ReadFile(files[0])
	if err != nil || !strings.HasPrefix(string(data), "magnet:") {
		t.Fatalf("magnet cache content = %q, %v", data, err)
	}
	if addCount != 1 {
		t.Fatalf("magnet refresh added %d tasks", addCount)
	}
}

func TestRefreshReusesSeedCacheAfterCrashBeforeRPCSubmission(t *testing.T) {
	ctx := context.Background()
	addCount := 0
	rpc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Transmission-Session-Id") == "" {
			w.Header().Set("X-Transmission-Session-Id", "fixture-session")
			w.WriteHeader(http.StatusConflict)
			return
		}
		var request struct {
			Method    string         `json:"method"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode RPC: %v", err)
			return
		}
		switch request.Method {
		case "torrent-get":
			writeRPC(w, map[string]any{"torrents": []any{}})
		case "torrent-add":
			addCount++
			if request.Arguments["metainfo"] == "" {
				t.Errorf("cached metainfo was not reused: %#v", request.Arguments)
			}
			writeRPC(w, map[string]any{"torrent-added": map[string]any{"hashString": sampleHash}})
		default:
			t.Errorf("unexpected RPC method %q", request.Method)
			writeRPC(w, map[string]any{})
		}
	}))
	defer rpc.Close()
	dataDir := t.TempDir()
	store, err := storage.Open(filepath.Join(dataDir, "ani-rss.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveConfig(ctx, map[string]any{
		"automaticTasksPaused": false, "downloadToolHost": rpc.URL,
		"downloadPathTemplate": filepath.Join(dataDir, "downloads", "${title}"), "downloadRetry": float64(1),
	}); err != nil {
		t.Fatal(err)
	}
	subscription := map[string]any{
		"id": "cache-sub", "url": "fixture", "title": "Demo Show", "season": float64(1),
		"offset": float64(0), "subgroup": "Demo", "enable": true, "ova": false, "globalExclude": false,
		"match": []any{}, "exclude": []any{}, "standbyRssList": []any{}, "notDownload": []any{},
	}
	if err := store.PutSubscription(ctx, subscription); err != nil {
		t.Fatal(err)
	}
	cachePath := filepath.Join(dataDir, "torrents", "c", "cache-sub", sampleHash+".torrent.pending")
	if err := os.MkdirAll(filepath.Dir(cachePath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, []byte("d4:infod4:name4:testee"), 0600); err != nil {
		t.Fatal(err)
	}
	engine, err := pipeline.New(store, pipeline.Options{RSSFetcher: func(context.Context, string) ([]byte, error) {
		return []byte(`<rss><channel><item><title>Demo Show - 01</title><enclosure url="https://example.invalid/one.torrent"/><guid>` + sampleHash + `</guid></item></channel></rss>`), nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.RefreshSubscription(ctx, "cache-sub"); err != nil {
		t.Fatal(err)
	}
	if addCount != 1 {
		t.Fatalf("existing seed cache must be retried against Transmission after a pre-submit crash; add count = %d", addCount)
	}
	committed := strings.TrimSuffix(cachePath, ".pending")
	if _, err := os.Stat(committed); err != nil {
		t.Fatalf("accepted task must atomically promote prepared cache to committed history: %v", err)
	}
	if _, err := os.Stat(cachePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("prepared cache must be removed after acceptance: %v", err)
	}
}

func TestRefreshDoesNotReaddCommittedHistoryAfterTorrentTaskIsRemoved(t *testing.T) {
	ctx := context.Background()
	torrents := []map[string]any{}
	addCount := 0
	rpc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Transmission-Session-Id") == "" {
			w.Header().Set("X-Transmission-Session-Id", "fixture-session")
			w.WriteHeader(http.StatusConflict)
			return
		}
		var request struct {
			Method    string         `json:"method"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode RPC: %v", err)
			return
		}
		switch request.Method {
		case "torrent-get":
			writeRPC(w, map[string]any{"torrents": torrents})
		case "torrent-add":
			addCount++
			torrents = append(torrents, map[string]any{"hashString": sampleHash, "labels": []string{"ani-rss"}, "status": 4, "totalSize": 1, "haveValid": 0})
			_, _ = io.WriteString(w, `{"arguments":{},"result":"response lost"}`)
		default:
			t.Errorf("unexpected RPC method %q", request.Method)
			writeRPC(w, map[string]any{})
		}
	}))
	defer rpc.Close()
	dataDir := t.TempDir()
	store, err := storage.Open(filepath.Join(dataDir, "ani-rss.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveConfig(ctx, map[string]any{
		"automaticTasksPaused": false, "downloadToolHost": rpc.URL,
		"downloadPathTemplate": filepath.Join(dataDir, "downloads", "${title}"), "downloadRetry": float64(1),
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.PutSubscription(ctx, map[string]any{
		"id": "history-sub", "url": "fixture", "title": "Demo Show", "season": float64(1),
		"offset": float64(0), "subgroup": "Demo", "enable": true, "ova": false, "globalExclude": false,
		"match": []any{}, "exclude": []any{}, "standbyRssList": []any{}, "notDownload": []any{},
	}); err != nil {
		t.Fatal(err)
	}
	engine, err := pipeline.New(store, pipeline.Options{RSSFetcher: func(context.Context, string) ([]byte, error) {
		return []byte(`<rss><channel><item><title>Demo Show - 01</title><enclosure url="magnet:?xt=urn:btih:` + sampleHash + `"/><guid>` + sampleHash + `</guid></item></channel></rss>`), nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.RefreshSubscription(ctx, "history-sub"); err != nil {
		t.Fatal(err)
	}
	committed := filepath.Join(dataDir, "torrents", "h", "history-sub", sampleHash+".txt")
	if _, err := os.Stat(committed); err != nil {
		t.Fatalf("response-loss reconciliation should commit history: %v", err)
	}
	torrents = []map[string]any{} // The user removes the Transmission task after its successful download.
	if err := engine.RefreshSubscription(ctx, "history-sub"); err != nil {
		t.Fatal(err)
	}
	if addCount != 1 {
		t.Fatalf("committed history must prevent an old episode from being re-added; add count = %d", addCount)
	}
}

func TestRSSProxyUsesCurrentConfigAndDoesNotProxyTransmission(t *testing.T) {
	ctx := context.Background()
	t.Setenv("HTTP_PROXY", "http://192.0.2.1:3128")
	t.Setenv("http_proxy", "http://192.0.2.1:3128")
	feedXML := []byte(`<rss><channel><item><title>Demo Show - 01</title><enclosure url="https://example.invalid/one.torrent"/><guid>` + sampleHash + `</guid></item></channel></rss>`)
	directCalls, proxyCalls := atomic.Int64{}, atomic.Int64{}
	direct := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		directCalls.Add(1)
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write(feedXML)
	}))
	defer direct.Close()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyCalls.Add(1)
		if !strings.HasPrefix(r.RequestURI, "http://") {
			t.Errorf("proxy request must use absolute URL, got %q", r.RequestURI)
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write(feedXML)
	}))
	defer proxy.Close()
	proxyURL, err := url.Parse(proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxyPort, _ := strconv.Atoi(proxyURL.Port())

	transmissionCalls := atomic.Int64{}
	rpc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		transmissionCalls.Add(1)
		if r.Header.Get("X-Transmission-Session-Id") == "" {
			w.Header().Set("X-Transmission-Session-Id", "fixture-session")
			w.WriteHeader(http.StatusConflict)
			return
		}
		writeRPC(w, map[string]any{"torrents": []any{}})
	}))
	defer rpc.Close()

	store, err := storage.Open(filepath.Join(t.TempDir(), "ani-rss.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	config := map[string]any{
		"proxy": false, "proxyHost": proxyURL.Hostname(), "proxyPort": float64(proxyPort),
		"proxyList": "127.0.0.1", "downloadToolHost": rpc.URL,
		"downloadPathTemplate": filepath.Join(t.TempDir(), "${title}"),
	}
	if err := store.SaveConfig(ctx, config); err != nil {
		t.Fatal(err)
	}
	subscription := map[string]any{
		"id": "proxy-sub", "url": direct.URL + "/rss", "title": "Demo Show", "season": float64(1),
		"subgroup": "Demo", "offset": float64(0), "enable": true, "ova": false,
		"globalExclude": false, "match": []any{}, "exclude": []any{}, "standbyRssList": []any{},
	}
	engine, err := pipeline.New(store, pipeline.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Preview(ctx, subscription); err != nil {
		t.Fatal(err)
	}
	if directCalls.Load() != 1 || proxyCalls.Load() != 0 {
		t.Fatalf("proxy=false used direct=%d proxy=%d", directCalls.Load(), proxyCalls.Load())
	}
	config["proxy"] = true
	if err := store.SaveConfig(ctx, config); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Preview(ctx, subscription); err != nil {
		t.Fatal(err)
	}
	if directCalls.Load() != 1 || proxyCalls.Load() != 1 {
		t.Fatalf("proxy settings did not update: direct=%d proxy=%d", directCalls.Load(), proxyCalls.Load())
	}
	for _, route := range engine.Routes() {
		if route.Path == "/api/torrentsInfos" {
			request := httptest.NewRequest(http.MethodPost, route.Path, nil)
			response := httptest.NewRecorder()
			route.Handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("torrent list response = %d %s", response.Code, response.Body.String())
			}
		}
	}
	if transmissionCalls.Load() == 0 || proxyCalls.Load() != 1 {
		t.Fatalf("Transmission RPC unexpectedly used RSS proxy: rpc=%d proxy=%d", transmissionCalls.Load(), proxyCalls.Load())
	}
}

func TestDownloadPathExpandsDateSeasonMetadataAndCustomOverride(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := storage.Open(filepath.Join(root, "ani-rss.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveConfig(ctx, map[string]any{
		"downloadPathTemplate":    filepath.Join(root, "${letter}", "${year}", "${tmdbYear}", "${monthFormat}", "${quarterFormat}-${quarterName}", "S${seasonFormat}", "${title}", "${subgroup}", "${tmdbid}"),
		"ovaDownloadPathTemplate": filepath.Join(root, "OVA", "${title}"),
	}); err != nil {
		t.Fatal(err)
	}
	engine, err := pipeline.New(store, pipeline.Options{})
	if err != nil {
		t.Fatal(err)
	}
	subscription := map[string]any{
		"id": "path-sub", "title": "Example Show", "subgroup": "Demo", "season": float64(2),
		"releaseDate": "2024-12-15", "tmdb": map[string]any{"id": "123", "date": "2023-02-01"},
	}
	got, err := engine.DownloadPath(ctx, subscription)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "E", "2025", "2023", "12", "01-冬", "S02", "Example Show", "Demo", "123")
	if got["downloadPath"] != want || got["change"] != false {
		t.Fatalf("expanded path = %#v, want %q", got, want)
	}
	subscription["customDownloadPath"] = true
	subscription["customDownloadPathTemplate"] = "\n" + filepath.Join(root, "custom", "${title}") + "\n" + filepath.Join(root, "ignored", "${title}")
	got, err = engine.DownloadPath(ctx, subscription)
	if err != nil {
		t.Fatal(err)
	}
	if got["downloadPath"] != filepath.Join(root, "custom", "Example Show") {
		t.Fatalf("custom path was not selected: %#v", got)
	}
	subscription["customDownloadPath"] = false
	subscription["ova"] = true
	got, err = engine.DownloadPath(ctx, subscription)
	if err != nil {
		t.Fatal(err)
	}
	if got["downloadPath"] != filepath.Join(root, "OVA", "Example Show") {
		t.Fatalf("OVA path was not selected: %#v", got)
	}
}

func TestDownloadPathLetterMatchesJavaInitialRules(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := storage.Open(filepath.Join(root, "ani-rss.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveConfig(ctx, map[string]any{"downloadPathTemplate": filepath.Join(root, "${letter}")}); err != nil {
		t.Fatal(err)
	}
	engine, err := pipeline.New(store, pipeline.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ title, letter string }{
		{"Example Show", "E"},
		{"重生之门", "C"},
		{"2024 Show", "0"},
		{"🎬 Show", "#"},
	} {
		t.Run(tc.title, func(t *testing.T) {
			got, err := engine.DownloadPath(ctx, map[string]any{"title": tc.title, "season": float64(1)})
			if err != nil {
				t.Fatal(err)
			}
			want := filepath.Join(root, tc.letter)
			if got["downloadPath"] != want {
				t.Fatalf("downloadPath = %#v, want %q", got["downloadPath"], want)
			}
		})
	}
}

func TestRefreshHTTPPropagatesRSSFailure(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(filepath.Join(t.TempDir(), "ani-rss.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveConfig(ctx, map[string]any{"apiKey": "fixture-api-key", "automaticTasksPaused": false}); err != nil {
		t.Fatal(err)
	}
	if err := store.PutSubscription(ctx, map[string]any{"id": "rss-error", "url": "fixture", "title": "Example", "enable": true}); err != nil {
		t.Fatal(err)
	}
	engine, err := pipeline.New(store, pipeline.Options{RSSFetcher: func(context.Context, string) ([]byte, error) {
		return nil, errors.New("fixture RSS outage")
	}})
	if err != nil {
		t.Fatal(err)
	}
	app, err := server.New(ctx, store)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Register(engine.Routes()...); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/refreshAni", strings.NewReader(`{"id":"rss-error"}`))
	request.Header.Set("x-api-key", "fixture-api-key")
	response := httptest.NewRecorder()
	app.ServeHTTP(response, request)
	var result map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	message, _ := result["message"].(string)
	if response.Code != http.StatusOK || result["code"] != float64(500) || !strings.Contains(message, "fixture RSS outage") {
		t.Fatalf("RSS 失败应通过 Result 报出：HTTP %d %s", response.Code, response.Body.String())
	}
}

func TestDownloadNewAllowsMultipleItemsPublishedSameDay(t *testing.T) {
	ctx := context.Background()
	firstHash, secondHash := strings.Repeat("a", 40), strings.Repeat("b", 40)
	addCount := atomic.Int64{}
	currentTorrents := []map[string]any{}
	rpc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Transmission-Session-Id") == "" {
			w.Header().Set("X-Transmission-Session-Id", "fixture-session")
			w.WriteHeader(http.StatusConflict)
			return
		}
		var request struct {
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode RPC: %v", err)
			return
		}
		switch request.Method {
		case "torrent-get":
			writeRPC(w, map[string]any{"torrents": currentTorrents})
		case "torrent-add":
			index := addCount.Add(1)
			hash := firstHash
			if index == 2 {
				hash = secondHash
			}
			currentTorrents = append(currentTorrents, map[string]any{
				"id": index, "hashString": hash, "name": "Example", "status": 4, "downloadDir": "/downloads",
				"labels": []string{"ani-rss"}, "isFinished": false, "totalSize": 100, "haveValid": 10,
			})
			writeRPC(w, map[string]any{"torrent-added": map[string]any{"hashString": hash}})
		default:
			t.Errorf("unexpected method %q", request.Method)
			writeRPC(w, map[string]any{})
		}
	}))
	defer rpc.Close()
	torrentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("d4:infod4:name4:testee"))
	}))
	defer torrentServer.Close()
	feedXML := `<rss><channel><item><title>Demo Show - 01</title><enclosure url="` + torrentServer.URL + `/one.torrent"/><guid>` + firstHash + `</guid><pubDate>Mon, 01 Jan 2024 10:00:00 GMT</pubDate></item><item><title>Demo Show - 02</title><enclosure url="` + torrentServer.URL + `/two.torrent"/><guid>` + secondHash + `</guid><pubDate>Mon, 01 Jan 2024 11:00:00 GMT</pubDate></item></channel></rss>`
	store, err := storage.Open(filepath.Join(t.TempDir(), "ani-rss.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveConfig(ctx, map[string]any{
		"automaticTasksPaused": false, "downloadToolHost": rpc.URL,
		"downloadPathTemplate": filepath.Join(t.TempDir(), "${title}"),
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.PutSubscription(ctx, map[string]any{
		"id": "newest-sub", "url": "fixture", "title": "Demo Show", "season": float64(1), "offset": float64(0),
		"subgroup": "Demo", "enable": true, "ova": false, "globalExclude": false, "downloadNew": true,
		"match": []any{}, "exclude": []any{}, "standbyRssList": []any{}, "notDownload": []any{},
	}); err != nil {
		t.Fatal(err)
	}
	engine, err := pipeline.New(store, pipeline.Options{RSSFetcher: func(context.Context, string) ([]byte, error) { return []byte(feedXML), nil }})
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.RefreshSubscription(ctx, "newest-sub"); err != nil {
		t.Fatal(err)
	}
	if addCount.Load() != 2 {
		t.Fatalf("same-day newest batch added %d items", addCount.Load())
	}
	subscriptions, err := store.ListSubscriptions(ctx)
	if err != nil || subscriptions[0]["currentEpisodeNumber"] != float64(2) {
		t.Fatalf("progress did not use newest episode: %#v, %v", subscriptions, err)
	}
}

func TestPreviewKeepsOVAResourceWithoutEpisodeNumber(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(filepath.Join(t.TempDir(), "ani-rss.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveConfig(ctx, map[string]any{
		"downloadPathTemplate": filepath.Join(t.TempDir(), "${title}"),
	}); err != nil {
		t.Fatal(err)
	}
	engine, err := pipeline.New(store, pipeline.Options{RSSFetcher: func(context.Context, string) ([]byte, error) {
		return []byte(`<rss><channel><item><title>Demo Show Movie</title><enclosure url="https://example.invalid/movie.torrent"/><guid>` + sampleHash + `</guid></item></channel></rss>`), nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := engine.Preview(ctx, map[string]any{
		"id": "ova", "url": "fixture", "title": "Demo Show Movie", "subgroup": "Demo",
		"season": float64(1), "offset": float64(0), "ova": true, "globalExclude": false,
		"match": []any{}, "exclude": []any{}, "standbyRssList": []any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	items := preview["items"].([]pipeline.Item)
	if len(items) != 1 || items[0].Episode != 1 || items[0].ReName != "Demo Show Movie" {
		t.Fatalf("OVA item was not preserved: %#v", items)
	}
}

func TestDownloadPublicAPIAddsOneDecidedItemAndReconcilesDuplicate(t *testing.T) {
	ctx := context.Background()
	hash := strings.Repeat("c", 40)
	addCount := 0
	torrents := []map[string]any{}
	rpc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Transmission-Session-Id") == "" {
			w.Header().Set("X-Transmission-Session-Id", "fixture-session")
			w.WriteHeader(http.StatusConflict)
			return
		}
		var request struct {
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode RPC: %v", err)
			return
		}
		switch request.Method {
		case "torrent-get":
			writeRPC(w, map[string]any{"torrents": torrents})
		case "torrent-add":
			addCount++
			torrents = append(torrents, map[string]any{"hashString": hash, "labels": []string{"ani-rss"}, "totalSize": 1, "haveValid": 0, "status": 4})
			writeRPC(w, map[string]any{"torrent-added": map[string]any{"hashString": hash}})
		default:
			t.Errorf("unexpected RPC method %q", request.Method)
			writeRPC(w, map[string]any{})
		}
	}))
	defer rpc.Close()
	store, err := storage.Open(filepath.Join(t.TempDir(), "ani-rss.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveConfig(ctx, map[string]any{
		"automaticTasksPaused": false, "downloadToolHost": rpc.URL,
		"downloadPathTemplate": filepath.Join(t.TempDir(), "${title}"),
	}); err != nil {
		t.Fatal(err)
	}
	engine, err := pipeline.New(store, pipeline.Options{})
	if err != nil {
		t.Fatal(err)
	}
	subscription := map[string]any{"id": "one-item", "title": "Example", "season": float64(1), "subgroup": "Demo"}
	item := pipeline.Item{Title: "Example - 01", ReName: "Example S01E01", Torrent: "magnet:?xt=urn:btih:" + hash, InfoHash: hash, Episode: 1, Master: true, Subgroup: "Demo"}
	result, err := engine.Download(ctx, subscription, item)
	if err != nil || result.Hash != hash {
		t.Fatalf("Download() = %#v, %v", result, err)
	}
	result, err = engine.Download(ctx, subscription, item)
	if err != nil || !result.Duplicate || result.Hash != hash {
		t.Fatalf("duplicate Download() = %#v, %v", result, err)
	}
	if addCount != 1 {
		t.Fatalf("single-item Download added %d tasks", addCount)
	}
}

func postAPI(t *testing.T, app http.Handler, path, body string) map[string]any {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("x-api-key", "fixture-api-key")
	response := httptest.NewRecorder()
	app.ServeHTTP(response, request)
	var result map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response %s: %v (%s)", path, err, response.Body.String())
	}
	if result["code"] != float64(200) {
		t.Fatalf("request %s failed: %s", path, response.Body.String())
	}
	return result
}

func writeRPC(w http.ResponseWriter, arguments map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"arguments": arguments, "result": "success"})
}
