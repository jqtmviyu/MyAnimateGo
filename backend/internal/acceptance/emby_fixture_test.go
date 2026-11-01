package acceptance_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

type embyLiveFixture struct {
	Root, Base, Data, Token, UserID     string
	SelectedLibraryID, ControlLibraryID string
	SelectedMediaPath, ControlMediaPath string
}

func prepareEmbyLive(t *testing.T, binary string) *embyLiveFixture {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("this isolated Emby launcher requires macOS sandbox-exec")
	}
	ffmpeg := os.Getenv("ANI_RSS_TEST_EMBY_FFMPEG_BIN")
	if ffmpeg == "" {
		var err error
		ffmpeg, err = exec.LookPath("ffmpeg")
		if err != nil {
			t.Fatal("set ANI_RSS_TEST_EMBY_FFMPEG_BIN to generate the synthetic video")
		}
	}
	root := t.TempDir()
	fixture := &embyLiveFixture{Root: root, Data: filepath.Join(root, "programdata")}
	port := freePort(t)
	fixture.Base = "http://127.0.0.1:" + strconv.Itoa(port)
	if err := os.MkdirAll(filepath.Join(fixture.Data, "config"), 0700); err != nil {
		t.Fatal(err)
	}
	configuration := `<?xml version="1.0" encoding="utf-8"?><ServerConfiguration><HttpServerPortNumber>` + strconv.Itoa(port) + `</HttpServerPortNumber><PublicPort>` + strconv.Itoa(port) + `</PublicPort><EnableHttps>false</EnableHttps><EnableRemoteAccess>false</EnableRemoteAccess><EnableUPnP>false</EnableUPnP><EnableAutoUpdate>false</EnableAutoUpdate><EnableAnonymousUsageReporting>false</EnableAnonymousUsageReporting><AutoRunWebApp>false</AutoRunWebApp><RunAtStartup>false</RunAtStartup><IsStartupWizardCompleted>false</IsStartupWizardCompleted><ServerName>MyAnimeGo isolated acceptance</ServerName><LocalNetworkSubnets><string>127.0.0.0/8</string><string>::1/128</string></LocalNetworkSubnets></ServerConfiguration>`
	writeFile(t, filepath.Join(fixture.Data, "config", "system.xml"), []byte(configuration))
	profile := `(version 1)
(allow default)
(deny network*)
(allow network-bind)
(allow network-inbound (local ip "*:` + strconv.Itoa(port) + `"))
(allow network-outbound (remote ip "localhost:*"))
`
	profilePath := filepath.Join(root, "local-client.sb")
	writeFile(t, profilePath, []byte(profile))
	log, err := os.OpenFile(filepath.Join(root, "emby.log"), os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	// Emby 固定监听各网卡；限定测试端口和本机出站，关闭托盘及自动打开网页。
	command := exec.Command("/usr/bin/sandbox-exec", "-f", profilePath, binary, "-programdata", fixture.Data, "-service", "-noautorunwebapp", "-noupdate")
	command.Dir, command.Stdout, command.Stderr = filepath.Dir(binary), log, log
	if err := command.Start(); err != nil {
		_ = log.Close()
		t.Fatal(err)
	}
	done := make(chan struct{})
	var exitErr error
	go func() { exitErr = command.Wait(); close(done) }()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	t.Cleanup(func() {
		if fixture.Token != "" {
			request, _ := http.NewRequest(http.MethodPost, fixture.Base+"/System/Shutdown", nil)
			request.Header.Set("X-Emby-Token", fixture.Token)
			if response, err := client.Do(request); err == nil {
				_ = response.Body.Close()
			}
		} else {
			_ = command.Process.Signal(os.Interrupt)
		}
		select {
		case <-done:
			if exitErr != nil {
				t.Errorf("Emby shutdown: %v", exitErr)
			}
		case <-time.After(10 * time.Second):
			_ = command.Process.Kill()
			<-done
			t.Error("Emby shutdown timed out")
		}
		transport.CloseIdleConnections()
		_ = log.Close()
	})
	await(t, func() bool {
		response, err := client.Get(fixture.Base + "/System/Info/Public")
		if err != nil {
			return false
		}
		defer response.Body.Close()
		var info struct{ Version string }
		return json.NewDecoder(response.Body).Decode(&info) == nil && info.Version == "4.10.2.0"
	})
	api := func(method, path string, body, target any) {
		t.Helper()
		var data []byte
		contentType := "application/json"
		if form, ok := body.(url.Values); ok {
			data, contentType = []byte(form.Encode()), "application/x-www-form-urlencoded"
		} else if body != nil {
			var err error
			data, err = json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
		}
		request, err := http.NewRequest(method, fixture.Base+path, bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", contentType)
		request.Header.Set("X-Emby-Token", fixture.Token)
		request.Header.Set("X-Emby-Authorization", `MediaBrowser Client="MyAnimeGo acceptance", Device="Local", DeviceId="ani-rss-isolated", Version="1"`)
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			t.Fatalf("isolated Emby setup %s HTTP %d", path, response.StatusCode)
		}
		if target != nil {
			if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(target); err != nil {
				t.Fatal(err)
			}
		}
	}
	password := rand.Text()
	api(http.MethodPost, "/Startup/User", url.Values{"Name": {"local-acceptance"}, "Password": {password}}, nil)
	api(http.MethodPost, "/Startup/Complete", nil, nil)
	var login struct {
		AccessToken string
		User        struct{ ID string }
	}
	api(http.MethodPost, "/Users/AuthenticateByName", map[string]string{"Username": "local-acceptance", "Pw": password}, &login)
	fixture.Token, fixture.UserID = login.AccessToken, login.User.ID
	if fixture.Token == "" || fixture.UserID == "" {
		t.Fatal("isolated Emby administrator did not authenticate")
	}
	var tasks []struct{ ID, Key string }
	api(http.MethodGet, "/ScheduledTasks", nil, &tasks)
	for _, task := range tasks {
		if task.Key == "RefreshLibrary" || task.Key == "ScanInternalMetadataFolderTask" {
			api(http.MethodPost, "/ScheduledTasks/"+task.ID+"/Triggers", []any{}, nil)
		}
	}
	source := filepath.Join(root, "synthetic.mp4")
	encodeCtx, cancelEncode := context.WithTimeout(context.Background(), 20*time.Second)
	output, err := exec.CommandContext(encodeCtx, ffmpeg, "-hide_banner", "-loglevel", "error", "-y", "-f", "lavfi", "-i", "color=c=blue:s=160x90:r=10", "-t", "2", "-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", "-movflags", "+faststart", source).CombinedOutput()
	cancelEncode()
	if err != nil {
		t.Fatalf("generate synthetic media: %v %s", err, output)
	}
	for _, kind := range []string{"selected", "control"} {
		mediaRoot := filepath.Join(root, "media-"+kind)
		if err := os.MkdirAll(mediaRoot, 0700); err != nil {
			t.Fatal(err)
		}
		typeOptions := []map[string]any{}
		for _, itemType := range []string{"Series", "Season", "Episode"} {
			typeOptions = append(typeOptions, map[string]any{"Type": itemType, "MetadataFetchers": []string{}, "MetadataFetcherOrder": []string{}, "ImageFetchers": []string{}, "ImageFetcherOrder": []string{}})
		}
		options := map[string]any{"PathInfos": []map[string]string{{"Path": mediaRoot}}, "EnableRealtimeMonitor": false, "SaveLocalMetadata": false, "EnableInternetProviders": false, "MetadataSavers": []string{}, "TypeOptions": typeOptions}
		query := url.Values{"name": {"MyAnimeGo " + kind + " fixture"}, "collectionType": {"tvshows"}, "refreshLibrary": {"false"}}
		api(http.MethodPost, "/Library/VirtualFolders?"+query.Encode(), map[string]any{"LibraryOptions": options}, nil)
	}
	var folders []struct {
		Name, ItemID   string
		LibraryOptions struct{ EnableRealtimeMonitor bool }
	}
	api(http.MethodGet, "/Library/VirtualFolders", nil, &folders)
	for _, kind := range []string{"selected", "control"} {
		libraryID := ""
		for _, folder := range folders {
			if folder.Name == "MyAnimeGo "+kind+" fixture" {
				if folder.LibraryOptions.EnableRealtimeMonitor {
					t.Fatal("fixture realtime monitor remained enabled")
				}
				libraryID = folder.ItemID
			}
		}
		series := filepath.Join(root, "media-"+kind, "合成番剧")
		season := filepath.Join(series, "Season 01")
		if err := os.MkdirAll(season, 0700); err != nil {
			t.Fatal(err)
		}
		video := filepath.Join(season, "合成番剧 S01E01.mp4")
		media, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, video, media)
		writeFile(t, filepath.Join(series, "tvshow.nfo"), []byte("<tvshow><title>MyAnimeGo 合成番剧</title><year>2026</year></tvshow>"))
		writeFile(t, strings.TrimSuffix(video, ".mp4")+".nfo", []byte("<episodedetails><title>合成第1集</title><season>1</season><episode>1</episode></episodedetails>"))
		if kind == "selected" {
			fixture.SelectedLibraryID, fixture.SelectedMediaPath = libraryID, video
		} else {
			fixture.ControlLibraryID, fixture.ControlMediaPath = libraryID, video
		}
	}
	return fixture
}
