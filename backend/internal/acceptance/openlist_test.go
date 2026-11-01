package acceptance_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/notifications"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/storage"
)

func TestOpenList427Live(t *testing.T) {
	binary := os.Getenv("ANI_RSS_TEST_OPENLIST_BIN")
	if binary == "" {
		t.Skip("set ANI_RSS_TEST_OPENLIST_BIN to an isolated OpenList 4.2.7 binary")
	}
	version, err := exec.Command(binary, "version").CombinedOutput()
	if err != nil || !strings.Contains(string(version), "Version: v4.2.7") {
		t.Fatalf("requires OpenList v4.2.7: %v", err)
	}
	root := t.TempDir()
	dataDir := filepath.Join(root, "service")
	remote := filepath.Join(root, "remote")
	if err := os.MkdirAll(remote, 0700); err != nil {
		t.Fatal(err)
	}
	initCtx, cancelInit := context.WithTimeout(context.Background(), 30*time.Second)
	initialize := exec.CommandContext(initCtx, binary, "admin", "set", "local-acceptance-only", "--data", dataDir)
	// 新版默认等待安装向导，独立实例通过环境变量创建管理员。
	initialize.Env = append(os.Environ(), "OPENLIST_ADMIN_PASSWORD=local-acceptance-only")
	output, err := initialize.CombinedOutput()
	cancelInit()
	writeFile(t, filepath.Join(root, "initialize.log"), output)
	if err != nil {
		t.Fatalf("initialize isolated OpenList: %v", err)
	}
	configPath := filepath.Join(dataDir, "config.json")
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	port := freePort(t)
	scheme := config["scheme"].(map[string]any)
	scheme["address"] = "127.0.0.1"
	scheme["http_port"] = port
	scheme["https_port"] = -1
	raw, err = json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, configPath, raw)
	log, err := os.OpenFile(filepath.Join(root, "server.log"), os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, "server", "--data", dataDir)
	command.Dir = root
	command.Stdout = log
	command.Stderr = log
	command.Env = append(os.Environ(), "OPENLIST_ADDR=127.0.0.1", "OPENLIST_HTTP_PORT="+strconv.Itoa(port))
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	t.Cleanup(func() {
		_ = command.Process.Signal(os.Interrupt)
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("OpenList exit: %v", err)
			}
		case <-time.After(10 * time.Second):
			_ = command.Process.Kill()
			<-done
			t.Error("OpenList shutdown timed out")
		}
		_ = log.Close()
	})
	base := "http://127.0.0.1:" + strconv.Itoa(port)
	client := &http.Client{Timeout: 10 * time.Second}
	await(t, func() bool {
		res, err := client.Get(base + "/ping")
		if err != nil {
			return false
		}
		_ = res.Body.Close()
		return res.StatusCode == 200
	})
	post := func(path string, body any, token string) map[string]any {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequest(http.MethodPost, base+path, bytes.NewReader(encoded))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", token)
		res, err := client.Do(req)
		if err != nil {
			t.Fatalf("OpenList %s request failed", path)
		}
		defer res.Body.Close()
		var result map[string]any
		if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != 200 || result["code"] != float64(200) {
			message, _ := result["message"].(string)
			if token != "" {
				message = strings.ReplaceAll(message, token, "<REDACTED>")
			}
			message = strings.ReplaceAll(strings.ReplaceAll(message, base, "<instance>"), root, "<temp>")
			t.Fatalf("OpenList %s HTTP=%d service-code=%v message=%s", path, res.StatusCode, result["code"], message)
		}
		value, _ := result["data"].(map[string]any)
		return value
	}
	tokenOutput, err := exec.Command(binary, "admin", "token", "--data", dataDir).CombinedOutput()
	writeFile(t, filepath.Join(root, "admin-token.log"), tokenOutput)
	if err != nil {
		t.Fatalf("read isolated OpenList API token: %v", err)
	}
	parts := strings.SplitN(string(tokenOutput), "Admin token:", 2)
	if len(parts) != 2 || len(strings.Fields(parts[1])) == 0 {
		t.Fatal("OpenList CLI returned no API token")
	}
	token := strings.Fields(parts[1])[0]

	addition, err := json.Marshal(map[string]any{"root_folder_path": remote})
	if err != nil {
		t.Fatal(err)
	}
	post("/api/admin/storage/create", map[string]any{"mount_path": "/library", "driver": "Local", "addition": string(addition), "cache_expiration": 1, "order": 0}, token)
	await(t, func() bool {
		req, err := http.NewRequest(http.MethodPost, base+"/api/fs/list", strings.NewReader(`{"path":"/library","refresh":true}`))
		if err != nil {
			return false
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", token)
		res, err := client.Do(req)
		if err != nil {
			return false
		}
		defer res.Body.Close()
		var result struct {
			Code int `json:"code"`
		}
		return json.NewDecoder(res.Body).Decode(&result) == nil && result.Code == 200
	})

	local := filepath.Join(root, "downloads")
	name := "Show S01E01.mkv"
	first := []byte("first media version\n")
	writeFile(t, filepath.Join(local, name), first)
	store, err := storage.Open(filepath.Join(root, "ani-rss.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if store != nil {
			_ = store.Close()
		}
	}()
	channel := map[string]any{"enable": true, "retry": 1, "sort": 0, "notificationType": "OPEN_LIST_UPLOAD", "statusList": []string{"DOWNLOAD_END"}, "openListUploadHost": base, "openListUploadApiKey": token, "openListUploadPath": "/library/新番/Season ${seasonFormat}"}
	save := func() {
		if err := store.SaveConfig(context.Background(), map[string]any{"notificationConfigList": []any{channel}}); err != nil {
			t.Fatal(err)
		}
	}
	save()
	newService := func() *notifications.Service {
		s, err := notifications.New(store, notifications.Options{HTTPClient: client, DownloadPath: func(context.Context, map[string]any) (string, error) { return local, nil }})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	service := newService()
	sub := map[string]any{"id": "openlist-live", "title": "Show", "season": 1}
	if err := service.Send(context.Background(), "initial-upload", sub, "download complete", "DOWNLOAD_END"); err != nil {
		t.Fatalf("upload to fresh destination: %v", err)
	}
	target := filepath.Join(remote, "新番", "Season 01")
	actual, err := os.ReadFile(filepath.Join(target, name))
	if err != nil || !bytes.Equal(actual, first) {
		t.Fatalf("initial uploaded content differs: %v", err)
	}
	// 只替换同季同集，另一集保留；成功上传之后才清理本地合成文件。
	writeFile(t, filepath.Join(target, "Old Show S01E01.mkv"), []byte("old release"))
	untouched := []byte("another episode")
	writeFile(t, filepath.Join(target, "Show S01E02.mkv"), untouched)
	replacement := []byte("replacement media version with a different size\n")
	writeFile(t, filepath.Join(local, name), replacement)
	channel["openListUploadDeleteOldEpisode"] = true
	channel["openListUploadDeleteLocalFile"] = true
	save()
	if err := service.Send(context.Background(), "replacement-upload", sub, "download complete", "DOWNLOAD_END"); err != nil {
		t.Fatal(err)
	}
	actual, err = os.ReadFile(filepath.Join(target, name))
	if err != nil || !bytes.Equal(actual, replacement) {
		t.Fatalf("replacement content differs: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "Old Show S01E01.mkv")); !os.IsNotExist(err) {
		t.Fatal("old release remains")
	}
	actual, err = os.ReadFile(filepath.Join(target, "Show S01E02.mkv"))
	if err != nil || !bytes.Equal(actual, untouched) {
		t.Fatal("another episode changed")
	}
	if _, err := os.Stat(filepath.Join(local, name)); !os.IsNotExist(err) {
		t.Fatal("accepted local file was not cleaned up")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store = nil
	store, err = storage.Open(filepath.Join(root, "ani-rss.db"))
	if err != nil {
		t.Fatal(err)
	}
	if done, err := newService().Reconcile(context.Background(), "replacement-upload", sub, "download complete", "DOWNLOAD_END"); err != nil || !done {
		t.Fatalf("persisted delivery reconcile = %v, %v", done, err)
	}
	t.Log("OpenList v4.2.7: fresh Unicode destination, byte content, same-episode replacement, other-episode preservation, local cleanup and persisted delivery verified")
}
