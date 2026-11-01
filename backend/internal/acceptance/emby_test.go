package acceptance_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/notifications"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/server"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/storage"
)

func TestEmby4102Live(t *testing.T) {
	binary := os.Getenv("ANI_RSS_TEST_EMBY_BIN")
	if binary == "" {
		t.Skip("set ANI_RSS_TEST_EMBY_BIN to an isolated official Emby 4.10.2.0 executable")
	}
	fixture := prepareEmbyLive(t, binary)
	endpoint, err := url.Parse(fixture.Base)
	if err != nil || endpoint.Scheme != "http" || net.ParseIP(endpoint.Hostname()) == nil || !net.ParseIP(endpoint.Hostname()).IsLoopback() {
		t.Fatal("Emby acceptance requires a loopback HTTP fixture")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	t.Cleanup(transport.CloseIdleConnections)
	var refreshes atomic.Int32
	client := &http.Client{Transport: embyLiveTransport{transport: transport, refreshes: &refreshes}, Timeout: 10 * time.Second}
	get := func(path string, target any) {
		t.Helper()
		request, err := http.NewRequest(http.MethodGet, fixture.Base+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("X-Emby-Token", fixture.Token)
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("Emby fixture query HTTP %d", response.StatusCode)
		}
		if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(target); err != nil {
			t.Fatal(err)
		}
	}
	var info struct{ Version, ServerName, ProgramDataPath string }
	get("/System/Info", &info)
	actualData, err := filepath.EvalSymlinks(info.ProgramDataPath)
	if err != nil {
		t.Fatal(err)
	}
	expectedData, err := filepath.EvalSymlinks(filepath.Join(fixture.Root, "programdata"))
	if err != nil || actualData != expectedData || info.ServerName != "MyAnimeGo isolated acceptance" || info.Version != "4.10.2.0" {
		t.Fatal("server is not the isolated Emby 4.10.2.0 fixture")
	}
	if fixture.SelectedLibraryID == "" || fixture.ControlLibraryID == "" || fixture.SelectedLibraryID == fixture.ControlLibraryID {
		t.Fatal("requires distinct selected and control fixture libraries")
	}
	type mediaItem struct {
		ID, Path, Type string
		RunTimeTicks   int64
		MediaStreams   []struct{ Type, Codec string }
	}
	items := func(libraryID string) []mediaItem {
		t.Helper()
		query := url.Values{"ParentId": {libraryID}, "UserId": {fixture.UserID}, "Recursive": {"true"}, "IncludeItemTypes": {"Episode,Video,Movie"}, "Fields": {"Path,MediaStreams"}}
		var result struct{ Items []mediaItem }
		get("/Items?"+query.Encode(), &result)
		return result.Items
	}
	if len(items(fixture.SelectedLibraryID)) != 0 || len(items(fixture.ControlLibraryID)) != 0 {
		t.Fatal("requires two initially unindexed fixture libraries")
	}
	storePath := filepath.Join(t.TempDir(), "state.db")
	store, err := storage.Open(storePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	channel := map[string]any{
		"enable": true, "retry": 1, "notificationType": "EMBY_REFRESH", "statusList": []string{"DOWNLOAD_END"},
		"embyHost": fixture.Base, "embyApiKey": fixture.Token, "embyRefresh": true, "embyRefreshViewIds": []string{fixture.SelectedLibraryID},
	}
	if err := store.SaveConfig(context.Background(), map[string]any{"apiKey": "emby-acceptance-only", "notificationConfigList": []any{channel}}); err != nil {
		t.Fatal(err)
	}
	api, err := server.New(context.Background(), store)
	if err != nil {
		t.Fatal(err)
	}
	service, err := notifications.New(store, notifications.Options{HTTPClient: client})
	if err != nil {
		t.Fatal(err)
	}
	if err := api.Register(service.Routes()...); err != nil {
		t.Fatal(err)
	}
	host := httptest.NewServer(api)
	t.Cleanup(host.Close)
	encoded, _ := json.Marshal(channel)
	request, _ := http.NewRequest(http.MethodPost, host.URL+"/api/getEmbyViews", bytes.NewReader(encoded))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("api-key", "emby-acceptance-only")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Code int
		Data []notifications.EmbyView
	}
	err = json.NewDecoder(response.Body).Decode(&envelope)
	_ = response.Body.Close()
	if err != nil || envelope.Code != 200 {
		t.Fatalf("authenticated getEmbyViews did not resolve fixture libraries: code %d, %v", envelope.Code, err)
	}
	for _, expected := range []string{fixture.SelectedLibraryID, fixture.ControlLibraryID} {
		found := false
		for _, view := range envelope.Data {
			found = found || view.ID == expected
		}
		if !found {
			t.Fatal("getEmbyViews omitted a fixture library")
		}
	}
	subscription := map[string]any{"id": "emby-live", "title": "合成番剧", "season": 1}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := service.Send(ctx, "emby-live-refresh", subscription, "合成番剧 S01E01", "DOWNLOAD_END"); err != nil {
		t.Fatal(err)
	}
	if refreshes.Load() != 1 {
		t.Fatalf("selected refresh POST count = %d", refreshes.Load())
	}
	found := false
	var runtimeTicks int64
	for ctx.Err() == nil {
		for _, item := range items(fixture.SelectedLibraryID) {
			if filepath.Clean(item.Path) == filepath.Clean(fixture.SelectedMediaPath) && item.Type == "Episode" && item.RunTimeTicks == 20000000 {
				for _, stream := range item.MediaStreams {
					if stream.Type == "Video" && stream.Codec == "h264" {
						found, runtimeTicks = true, item.RunTimeTicks
					}
				}
			}
		}
		if found {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if !found {
		t.Fatal("refresh was accepted but selected episode did not enter the Emby index")
	}
	if len(items(fixture.ControlLibraryID)) != 0 {
		t.Fatal("unselected fixture library was scanned")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := storage.Open(storePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	recovered, err := notifications.New(reopened, notifications.Options{HTTPClient: client})
	if err != nil {
		t.Fatal(err)
	}
	complete, err := recovered.Reconcile(ctx, "emby-live-refresh", subscription, "合成番剧 S01E01", "DOWNLOAD_END")
	if err != nil || !complete {
		t.Fatalf("reopened refresh delivery = %v, %v", complete, err)
	}
	if err := recovered.Send(ctx, "emby-live-refresh", subscription, "合成番剧 S01E01", "DOWNLOAD_END"); err != nil {
		t.Fatal(err)
	}
	if refreshes.Load() != 1 {
		t.Fatal("reopened delivery resent the accepted refresh")
	}
	t.Logf("authenticated library query and selected scan completed; episode runtime ticks=%d; control unchanged; reopen added 0 refresh POSTs", runtimeTicks)
}

type embyLiveTransport struct {
	transport http.RoundTripper
	refreshes *atomic.Int32
}

func (d embyLiveTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, "/Refresh") {
		d.refreshes.Add(1)
	}
	return d.transport.RoundTrip(request)
}
