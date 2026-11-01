package acceptance_test

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/collections"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/jobs"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/pipeline"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/storage"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/torrent"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/transmission"
)

func TestTransmission406Live(t *testing.T) {
	binary := os.Getenv("ANI_RSS_TEST_TRANSMISSION_DAEMON")
	if binary == "" {
		t.Skip("set ANI_RSS_TEST_TRANSMISSION_DAEMON to a local 4.0.6 daemon")
	}
	version, err := exec.Command(binary, "--version").CombinedOutput()
	if err != nil || !strings.Contains(string(version), "4.0.6") {
		t.Fatalf("requires Transmission 4.0.6: %s, %v", version, err)
	}
	t.Log(strings.TrimSpace(string(version)))
	d := newDaemon(t, binary)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	client := d.client()
	info, err := client.Call(ctx, "session-get", nil)
	if err != nil {
		t.Fatal(err)
	}
	var session struct {
		Version string `json:"version"`
		RPC     int    `json:"rpc-version"`
	}
	if err := json.Unmarshal(info, &session); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(session.Version, "4.0.6") {
		t.Fatalf("unexpected server version %q", session.Version)
	}
	t.Logf("session handshake: version=%s rpc=%d", session.Version, session.RPC)

	t.Run("ordinary-torrent", func(t *testing.T) {
		download := filepath.Join(t.TempDir(), "download")
		data := []byte("ani-rss live acceptance content\n")
		name := "Show - 01.mkv"
		writeFile(t, filepath.Join(download, name), data)
		meta := metainfo(t, name, []asset{{name, data}}, false)
		added, err := client.Add(ctx, transmission.AddRequest{Metainfo: meta, DownloadDir: download, Labels: []string{"ani-rss-live"}, Paused: true})
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := torrent.Parse(meta)
		if err != nil {
			t.Fatal(err)
		}
		if added.Hash != parsed.TransmissionHash || added.Duplicate {
			t.Fatal("added torrent identity differs")
		}
		duplicate, err := client.Add(ctx, transmission.AddRequest{Metainfo: meta, DownloadDir: download, Paused: true})
		if err != nil || !duplicate.Duplicate || duplicate.Hash != added.Hash {
			t.Fatalf("duplicate add = %+v, %v", duplicate, err)
		}
		if err := client.SetLabels(ctx, added.Hash, []string{"ani-rss-live-updated"}); err != nil {
			t.Fatal(err)
		}
		if err := client.SetFilesWanted(ctx, added.Hash, []int{0}, false); err != nil {
			t.Fatal(err)
		}
		current := getTorrent(t, ctx, client, added.Hash)
		if current.FileStats[0].Wanted || len(current.Labels) != 1 || current.Labels[0] != "ani-rss-live-updated" {
			t.Fatal("selection or labels did not persist")
		}
		if err := client.SetFilesWanted(ctx, added.Hash, []int{0}, true); err != nil {
			t.Fatal(err)
		}
		if !getTorrent(t, ctx, client, added.Hash).FileStats[0].Wanted {
			t.Fatal("file was not selected")
		}
		renamed := "Show S01E01.mkv"
		if err := client.Rename(ctx, added.Hash, name, renamed); err != nil {
			t.Fatal(err)
		}
		await(t, func() bool {
			value, err := os.ReadFile(filepath.Join(download, renamed))
			return err == nil && bytes.Equal(value, data)
		})
		moved := filepath.Join(t.TempDir(), "moved")
		if err := client.SetLocation(ctx, added.Hash, moved); err != nil {
			t.Fatal(err)
		}
		await(t, func() bool {
			value, err := os.ReadFile(filepath.Join(moved, renamed))
			return err == nil && bytes.Equal(value, data)
		})
		if _, err := client.Call(ctx, "torrent-verify", map[string]any{"ids": []string{added.Hash}}); err != nil {
			t.Fatal(err)
		}
		await(t, func() bool {
			current := getTorrent(t, ctx, client, added.Hash)
			return current.HaveValid == int64(len(data))
		})
		if err := client.Start(ctx, added.Hash); err != nil {
			t.Fatal(err)
		}
		await(t, func() bool { return getTorrent(t, ctx, client, added.Hash).Status == 6 })
		if err := client.Remove(ctx, added.Hash, false); err != nil {
			t.Fatal(err)
		}
		if value, err := os.ReadFile(filepath.Join(moved, renamed)); err != nil || !bytes.Equal(value, data) {
			t.Fatal("remove without deletion changed local content")
		}
		deleteName := "delete-me.mkv"
		writeFile(t, filepath.Join(download, deleteName), data)
		second, err := client.Add(ctx, transmission.AddRequest{Metainfo: metainfo(t, deleteName, []asset{{deleteName, data}}, false), DownloadDir: download, Paused: true})
		if err != nil {
			t.Fatal(err)
		}
		if err := client.Remove(ctx, second.Hash, true); err != nil {
			t.Fatal(err)
		}
		await(t, func() bool { _, err := os.Stat(filepath.Join(download, deleteName)); return os.IsNotExist(err) })
		t.Log("add/duplicate/query/labels/selection/rename/move/verify/start/remove: real RPC and file contents verified")
	})

	t.Run("rss-to-downloaded-file", func(t *testing.T) {
		data := bytes.Repeat([]byte("ani-rss local webseed\n"), 200)
		name := "[Fixture] Webseed Show - 01.mkv"
		var meta []byte
		origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/episode.torrent" {
				_, _ = w.Write(meta)
				return
			}
			http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
		}))
		defer origin.Close()
		meta = metainfo(t, name, []asset{{name, data}}, false, origin.URL+"/content.mkv")
		parsed, err := torrent.Parse(meta)
		if err != nil {
			t.Fatal(err)
		}
		root := t.TempDir()
		store, err := storage.Open(filepath.Join(root, "state.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		config := map[string]any{"automaticTasksPaused": false, "downloadToolHost": d.url, "downloadToolUsername": "audit", "downloadToolPassword": "local-acceptance-only", "downloadPathTemplate": filepath.Join(root, "downloads", "${title}", "Season ${season}"), "renameTemplate": "${title} S${seasonFormat}E${episodeFormat}", "downloadRetry": 1, "downloadNew": false, "fileExist": false}
		if err := store.SaveConfig(ctx, config); err != nil {
			t.Fatal(err)
		}
		sub := map[string]any{"id": "live-webseed", "title": "Webseed Show", "season": 1, "enable": true, "subgroup": "Fixture", "url": origin.URL + "/rss", "downloadNew": false, "notDownload": []any{}, "match": []any{}, "exclude": []any{}, "standbyRssList": []any{}}
		if err := store.PutSubscription(ctx, sub); err != nil {
			t.Fatal(err)
		}
		feed := fmt.Sprintf(`<rss><channel><item><title>%s</title><enclosure url="%s/episode.torrent"/></item></channel></rss>`, name, origin.URL)
		engine, err := pipeline.New(store, pipeline.Options{RSSFetcher: func(context.Context, string) ([]byte, error) { return []byte(feed), nil }})
		if err != nil {
			t.Fatal(err)
		}
		if err := engine.RefreshSubscription(ctx, "live-webseed"); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, "downloads", "Webseed Show", "Season 1", name)
		await(t, func() bool {
			current := getTorrent(t, ctx, client, parsed.TransmissionHash)
			value, err := os.ReadFile(path)
			return current.HaveValid == int64(len(data)) && err == nil && bytes.Equal(value, data)
		})
		if err := engine.RefreshSubscription(ctx, "live-webseed"); err != nil {
			t.Fatal(err)
		}
		all, err := client.List(ctx, []string{"hashString"})
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, item := range all {
			if item.Hash == parsed.TransmissionHash {
				count++
			}
		}
		if count != 1 {
			t.Fatal("repeated RSS created duplicate torrents")
		}
		subs, err := store.ListSubscriptions(ctx)
		if err != nil || len(subs) != 1 || subs[0]["currentEpisodeNumber"] != float64(1) {
			t.Fatalf("subscription progress differs: %v", err)
		}
		if err := client.Remove(ctx, parsed.TransmissionHash, true); err != nil {
			t.Fatal(err)
		}
		t.Log("RSS parse/metainfo fetch/real download/byte hash/progress and repeat refresh verified against local webseed")
	})

	t.Run("collection-and-recovery", func(t *testing.T) {
		download := t.TempDir()
		assets := []asset{{"[Fixture] Show - 01.mkv", []byte("episode one\n")}, {"[Fixture] Show - 02.mkv", []byte("episode two\n")}, {"notes.txt", []byte("not selected\n")}}
		for _, file := range assets {
			writeFile(t, filepath.Join(download, "Batch", file.name), file.data)
		}
		meta := metainfo(t, "Batch", assets, true)
		encoded := base64.StdEncoding.EncodeToString(meta)
		ani := map[string]any{"title": "Show", "season": 1, "subgroup": "Fixture", "exclude": []any{`\.txt$`}}
		config := map[string]any{"downloadToolHost": d.url, "downloadToolUsername": "audit", "downloadToolPassword": "local-acceptance-only", "renameTemplate": "${title} S${seasonFormat}E${episodeFormat}"}
		db := filepath.Join(t.TempDir(), "state.db")
		store, err := storage.Open(db)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if store != nil {
				_ = store.Close()
			}
		}()
		payload, err := json.Marshal(map[string]any{"torrent": encoded, "ani": ani})
		if err != nil {
			t.Fatal(err)
		}
		job, err := store.EnqueueJob(ctx, collections.JobKind, "live-collection", payload)
		if err != nil {
			t.Fatal(err)
		}
		claimed, err := store.ClaimJob(ctx, "before-restart", time.Now(), time.Second)
		if err != nil || claimed == nil {
			t.Fatalf("claim = %v, %v", claimed, err)
		}
		makeService := func(client *http.Client) *collections.Service {
			service, err := collections.New(collections.Options{Enqueuer: store, Config: func(context.Context) (map[string]any, error) { return config, nil }, DownloadPath: func(context.Context, map[string]any) (string, error) { return download, nil }, HTTPClient: client, PollTimeout: 10 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			return service
		}
		service := makeService(nil)
		if err := service.Handler().Execute(ctx, *claimed); err != nil {
			t.Fatal(err)
		}
		parsed, err := torrent.Parse(meta)
		if err != nil {
			t.Fatal(err)
		}
		current := getTorrent(t, ctx, client, parsed.TransmissionHash)
		if len(current.FileStats) != 3 || !current.FileStats[0].Wanted || !current.FileStats[1].Wanted || current.FileStats[2].Wanted {
			t.Fatal("collection file selection differs")
		}
		for i := 0; i < 2; i++ {
			value, err := os.ReadFile(filepath.Join(download, "Batch", fmt.Sprintf("Show S01E%02d.mkv", i+1)))
			if err != nil || !bytes.Equal(value, assets[i].data) {
				t.Fatalf("renamed episode %d differs: %v", i+1, err)
			}
		}
		// 模拟远端已完成、本地尚未确认时退出，重开队列后应通过对账直接完成。
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		store = nil
		d.restart(t)
		store, err = storage.Open(db)
		if err != nil {
			t.Fatal(err)
		}
		var mutations atomic.Int64
		service = makeService(&http.Client{Transport: countWrites{count: &mutations}})
		manager := jobs.New(store, map[string]jobs.Handler{collections.JobKind: service.Handler()}, jobs.Options{Now: func() time.Time { return time.Now().Add(time.Minute) }})
		if ran, err := manager.RunOnce(ctx); err != nil || !ran {
			t.Fatalf("recovery run = %v, %v", ran, err)
		}
		finished, err := store.GetJob(ctx, job.ID)
		if err != nil || finished.State != "resolved" {
			t.Fatalf("recovered job = %+v, %v", finished, err)
		}
		if mutations.Load() != 0 {
			t.Fatalf("recovery repeated %d write RPCs", mutations.Load())
		}
		if err := d.client().Remove(ctx, parsed.TransmissionHash, true); err != nil {
			t.Fatal(err)
		}
		t.Log("collection selection/physical rename/start, daemon restart and persisted job reconciliation verified; recovery sent zero write RPCs")
	})
}

type countWrites struct{ count *atomic.Int64 }

func (c countWrites) RoundTrip(r *http.Request) (*http.Response, error) {
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	_ = r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(data))
	var rpc struct {
		Method string `json:"method"`
	}
	if err := json.Unmarshal(data, &rpc); err != nil {
		return nil, err
	}
	if rpc.Method != "torrent-get" {
		c.count.Add(1)
	}
	return http.DefaultTransport.RoundTrip(r)
}

type daemon struct {
	binary, root, url string
	cmd               *exec.Cmd
	log               *os.File
	done              chan error
}

func newDaemon(t *testing.T, binary string) *daemon {
	t.Helper()
	root := t.TempDir()
	port := freePort(t)
	settings := map[string]any{"rpc-bind-address": "127.0.0.1", "rpc-port": port, "rpc-whitelist-enabled": true, "rpc-whitelist": "127.0.0.1", "rpc-host-whitelist-enabled": false, "rpc-authentication-required": true, "rpc-username": "audit", "rpc-password": "local-acceptance-only", "peer-port": freePort(t), "bind-address-ipv4": "127.0.0.1", "bind-address-ipv6": "::1", "port-forwarding-enabled": false, "dht-enabled": false, "lpd-enabled": false, "pex-enabled": false, "utp-enabled": false, "download-dir": filepath.Join(root, "downloads"), "incomplete-dir-enabled": false, "rename-partial-files": false, "start-added-torrents": false}
	data, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "settings.json"), data)
	d := &daemon{binary: binary, root: root, url: "http://127.0.0.1:" + strconv.Itoa(port)}
	t.Cleanup(func() { d.stop(t) })
	d.start(t)
	return d
}
func (d *daemon) client() *transmission.Client {
	return transmission.New(transmission.Config{URL: d.url, Username: "audit", Password: "local-acceptance-only", HTTPClient: &http.Client{Timeout: 2 * time.Second}})
}
func (d *daemon) start(t *testing.T) {
	t.Helper()
	var err error
	d.log, err = os.OpenFile(filepath.Join(d.root, "daemon.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	d.cmd = exec.Command(d.binary, "-f", "-g", d.root)
	d.cmd.Stdout = d.log
	d.cmd.Stderr = d.log
	if err := d.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	d.done = make(chan error, 1)
	go func() { d.done <- d.cmd.Wait() }()
	await(t, func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, err := d.client().Call(ctx, "session-get", nil)
		return err == nil
	})
}
func (d *daemon) stop(t *testing.T) {
	t.Helper()
	if d.cmd == nil {
		return
	}
	_ = d.cmd.Process.Signal(os.Interrupt)
	select {
	case err := <-d.done:
		if err != nil {
			t.Errorf("daemon exit: %v", err)
		}
	case <-time.After(10 * time.Second):
		_ = d.cmd.Process.Kill()
		<-d.done
		t.Error("daemon shutdown timed out")
	}
	_ = d.log.Close()
	d.cmd = nil
}
func (d *daemon) restart(t *testing.T) { d.stop(t); d.start(t) }
func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	return port
}
func await(t *testing.T, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("state did not converge before deadline")
}
func getTorrent(t *testing.T, ctx context.Context, client *transmission.Client, hash string) transmission.Torrent {
	t.Helper()
	all, err := client.List(ctx, []string{"hashString", "name", "status", "downloadDir", "files", "fileStats", "labels", "totalSize", "haveValid"})
	if err != nil {
		t.Fatal(err)
	}
	for _, current := range all {
		if current.Hash == hash {
			return current
		}
	}
	t.Fatal("torrent missing")
	return transmission.Torrent{}
}
func writeFile(t *testing.T, name string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, data, 0600); err != nil {
		t.Fatal(err)
	}
}

type asset struct {
	name string
	data []byte
}

func metainfo(t *testing.T, name string, assets []asset, multi bool, webseeds ...string) []byte {
	t.Helper()
	var content []byte
	var files []any
	for _, file := range assets {
		content = append(content, file.data...)
		segments := []any{}
		for _, part := range strings.Split(file.name, "/") {
			segments = append(segments, part)
		}
		files = append(files, map[string]any{"length": len(file.data), "path": segments})
	}
	digest := sha1.Sum(content)
	info := map[string]any{"name": name, "piece length": 16384, "pieces": digest[:], "private": 1}
	if multi {
		info["files"] = files
	} else {
		info["length"] = len(content)
	}
	root := map[string]any{"info": info}
	if len(webseeds) > 0 {
		root["url-list"] = webseeds[0]
	}
	return bencode(root)
}
func bencode(value any) []byte {
	switch value := value.(type) {
	case string:
		return append([]byte(strconv.Itoa(len(value))+":"), []byte(value)...)
	case []byte:
		return append([]byte(strconv.Itoa(len(value))+":"), value...)
	case int:
		return []byte("i" + strconv.Itoa(value) + "e")
	case []any:
		out := []byte("l")
		for _, item := range value {
			out = append(out, bencode(item)...)
		}
		return append(out, 'e')
	case map[string]any:
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out := []byte("d")
		for _, key := range keys {
			out = append(out, bencode(key)...)
			out = append(out, bencode(value[key])...)
		}
		return append(out, 'e')
	default:
		panic("unsupported metainfo value")
	}
}
