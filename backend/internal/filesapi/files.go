package filesapi

import (
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

func (s *Service) file(w http.ResponseWriter, r *http.Request) {
	encoded := strings.ReplaceAll(r.URL.Query().Get("filename"), " ", "+")
	decoded, err := decodeBase64(encoded)
	if err != nil || decoded == "" {
		http.Error(w, "不允许访问", http.StatusBadRequest)
		return
	}
	if !supportedImage(strings.ToLower(filepath.Ext(decoded))) {
		http.Error(w, "不允许访问", http.StatusForbidden)
		return
	}
	filename := filepath.Clean(decoded)
	if !filepath.IsAbs(filename) {
		filename = filepath.Join(s.store.DataDir(), "files", filename)
	}
	info, err := os.Lstat(filename)
	if err != nil || !info.Mode().IsRegular() {
		http.Error(w, "404 Not Found !", http.StatusNotFound)
		return
	}
	file, err := os.Open(filename)
	if err != nil {
		http.Error(w, "404 Not Found !", http.StatusNotFound)
		return
	}
	defer file.Close()
	ext := strings.ToLower(filepath.Ext(filename))
	contentType := mime.TypeByExtension(ext)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", `inline; filename="`+url.PathEscape(filepath.Base(filename))+`"`)
	// 图片附件不提供视频流和分段读取。
	r.Header.Del("Range")
	w.Header().Set("Cache-Control", "private, max-age=2592000")
	http.ServeContent(w, r, filepath.Base(filename), info.ModTime(), file)
}

func formatSize(size int64) string {
	value := float64(size)
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	unit := 0
	for value >= 1024 && unit < len(units)-1 {
		value /= 1024
		unit++
	}
	return fmt.Sprintf("%.2f %s", value, units[unit])
}
