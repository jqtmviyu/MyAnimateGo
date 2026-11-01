package filesapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/server"
)

func decodeJSON(r *http.Request, target any, limit int64) error {
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, limit))
	if err := decoder.Decode(target); err != nil {
		return errors.New("JSON 请求格式错误")
	}
	return nil
}

func respondError(w http.ResponseWriter, err error) {
	if err == nil {
		err = errors.New("请求失败")
	}
	server.Respond(w, http.StatusInternalServerError, err.Error(), nil)
}

func writeResult(w http.ResponseWriter, code int, message string, data any) {
	server.Respond(w, code, message, data)
}

func stringValue(value any) string { text, _ := value.(string); return text }

func integer(value any) int {
	switch number := value.(type) {
	case int:
		return number
	case int64:
		return int(number)
	case float64:
		return int(number)
	case json.Number:
		result, _ := number.Int64()
		return int(result)
	case string:
		result, _ := strconv.Atoi(number)
		return result
	}
	return 0
}

func encodeBase64(text string) string { return base64.StdEncoding.EncodeToString([]byte(text)) }

func decodeBase64(text string) (string, error) {
	if decoded, err := base64.StdEncoding.DecodeString(text); err == nil {
		return string(decoded), nil
	}
	decoded, err := base64.RawStdEncoding.DecodeString(strings.TrimRight(text, "="))
	return string(decoded), err
}

func noCache(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
}

func writeScoped(root, target string, data []byte) error {
	if err := os.MkdirAll(root, 0700); err != nil {
		return err
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	rootAbs, err = filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return err
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("文件目标超出数据目录")
	}
	current := rootAbs
	for _, part := range strings.Split(filepath.Dir(rel), string(filepath.Separator)) {
		if part == "." || part == "" {
			continue
		}
		current = filepath.Join(current, part)
		if info, err := os.Lstat(current); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return errors.New("文件目标路径不能穿过符号链接")
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if info, err := os.Lstat(targetAbs); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("文件目标不能是符号链接")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(targetAbs), 0700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(targetAbs), ".ani-rss-write-*")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, 0600); err != nil {
		return err
	}
	if err := os.Rename(name, targetAbs); err != nil {
		return fmt.Errorf("原子保存失败：%w", err)
	}
	return nil
}

func removeScoped(root, target string) error {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("删除目标超出数据目录")
	}
	info, err := os.Lstat(targetAbs)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("不能删除符号链接目标")
	}
	return os.RemoveAll(targetAbs)
}

func expiredName(date time.Time) string { return date.Format("2006-01-02") }
