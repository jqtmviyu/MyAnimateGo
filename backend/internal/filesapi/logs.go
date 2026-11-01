package filesapi

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/server"
)

func (s *Service) listLogs(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	entries := append([]LogEntry(nil), s.logs...)
	s.mu.Unlock()
	server.Respond(w, http.StatusOK, "success", entries)
}

func (s *Service) clearLogs(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	s.logs = s.logs[:0]
	s.mu.Unlock()
	server.Respond(w, http.StatusOK, "success", nil)
}

func (s *Service) downloadLogs(w http.ResponseWriter, r *http.Request) {
	temp, err := os.CreateTemp("", "ani-rss-logs-*.zip")
	if err != nil {
		http.Error(w, "创建日志归档失败", http.StatusInternalServerError)
		return
	}
	defer os.Remove(temp.Name())
	archive := zip.NewWriter(temp)
	root := filepath.Join(s.store.DataDir(), "logs")
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) && path == root {
				return nil
			}
			return walkErr
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !strings.EqualFold(filepath.Ext(entry.Name()), ".log") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		writer, err := archive.Create(filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(writer, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	if err == nil {
		s.mu.Lock()
		entries := append([]LogEntry(nil), s.logs...)
		s.mu.Unlock()
		if len(entries) > 0 {
			writer, createErr := archive.Create("ani-rss.log")
			if createErr != nil {
				err = createErr
			} else {
				for _, entry := range entries {
					line, _ := json.Marshal(entry)
					if _, writeErr := fmt.Fprintln(writer, string(line)); writeErr != nil {
						err = writeErr
						break
					}
				}
			}
		}
	}
	if closeErr := archive.Close(); err == nil {
		err = closeErr
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		http.Error(w, "打包日志失败", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `inline; filename="logs.zip"`)
	http.ServeFile(w, r, temp.Name())
}
