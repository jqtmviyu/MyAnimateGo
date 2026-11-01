package filesapi

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/library"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/server"
)

var cacheHash = regexp.MustCompile(`(?i)^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

func (s *Service) clearCache(w http.ResponseWriter, r *http.Request) {
	media, err := library.New(s.store, library.Options{})
	if err != nil {
		respondError(w, err)
		return
	}
	removed, err := media.ClearCover(r.Context())
	if err != nil {
		respondError(w, err)
		return
	}
	imgDir := filepath.Join(s.store.DataDir(), "img")
	imgSize, err := directorySize(imgDir)
	if err != nil {
		respondError(w, err)
		return
	}
	if err := removeScoped(s.store.DataDir(), imgDir); err != nil {
		respondError(w, err)
		return
	}
	server.Respond(w, http.StatusOK, "清理完成, 共清理 "+formatSize(removed+imgSize), nil)
}

func (s *Service) deleteTorrentCache(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	hashes := parseCacheHashes(r.URL.Query().Get("hash"))
	if id == "" || len(hashes) == 0 {
		server.Respond(w, http.StatusInternalServerError, "订阅 ID 或种子 hash 无效", nil)
		return
	}
	subscriptions, err := s.store.ListSubscriptions(r.Context())
	if err != nil {
		respondError(w, err)
		return
	}
	var selected map[string]any
	for _, subscription := range subscriptions {
		if stringValue(subscription["id"]) == id {
			selected = subscription
			break
		}
	}
	if selected == nil {
		server.Respond(w, http.StatusInternalServerError, "此订阅不存在", nil)
		return
	}
	root := filepath.Join(s.store.DataDir(), "torrents")
	if _, err := os.Lstat(root); errors.Is(err, os.ErrNotExist) {
		server.Respond(w, http.StatusOK, "删除完成", nil)
		return
	} else if err != nil {
		respondError(w, err)
		return
	}
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !cacheFileMatches(entry.Name(), hashes) {
			return nil
		}
		allowed, err := cacheBelongsToSubscription(root, path, id, selected)
		if err != nil || !allowed {
			return err
		}
		return os.Remove(path)
	})
	if err != nil {
		respondError(w, err)
		return
	}
	server.Respond(w, http.StatusOK, "删除完成", nil)
}

func parseCacheHashes(value string) map[string]bool {
	result := map[string]bool{}
	for _, hash := range strings.Split(value, ",") {
		hash = strings.TrimSpace(hash)
		if cacheHash.MatchString(hash) {
			result[strings.ToLower(hash)] = true
		}
	}
	return result
}

func cacheFileMatches(name string, hashes map[string]bool) bool {
	extension := strings.ToLower(filepath.Ext(name))
	if extension != ".torrent" && extension != ".txt" {
		return false
	}
	return hashes[strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name)))]
}

func cacheBelongsToSubscription(root, path, id string, subscription map[string]any) (bool, error) {
	idRoot := filepath.Join(root, id[:1], id)
	if withinDirectory(idRoot, path) {
		return true, nil
	}
	title := strings.TrimSpace(stringValue(subscription["title"]))
	if title == "" {
		return false, nil
	}
	parent := filepath.Base(filepath.Dir(path))
	ova, _ := subscription["ova"].(bool)
	if ova {
		return parent == title, nil
	}
	season := integer(subscription["season"])
	return parent == fmt.Sprintf("Season %d", season) && filepath.Base(filepath.Dir(filepath.Dir(path))) == title, nil
}

func withinDirectory(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func directorySize(root string) (int64, error) {
	var size int64
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if errors.Is(walkErr, os.ErrNotExist) && path == root {
			return nil
		}
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		size += info.Size()
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("读取缓存大小失败：%w", err)
	}
	return size, nil
}
