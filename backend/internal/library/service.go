package library

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

//go:embed testdata/cover.png
var defaultCover []byte

func (s *Service) RefreshCover(ctx context.Context, imageURL string) (string, error) {
	if strings.TrimSpace(imageURL) == "" {
		target := filepath.Join(s.store.DataDir(), "files", "cover.png")
		if !fileExists(target) {
			if err := writeScoped(filepath.Join(s.store.DataDir(), "files"), target, defaultCover); err != nil {
				return "", err
			}
		}
		return "cover.png", nil
	}
	if _, err := url.ParseRequestURI(imageURL); err != nil {
		return "", fmt.Errorf("封面 URL 格式错误：%w", err)
	}
	parsed, _ := url.Parse(imageURL)
	if parsed.Scheme != "http" && parsed.Scheme != "https" || parsed.Host == "" {
		return "", errors.New("封面 URL 必须使用 HTTP 或 HTTPS")
	}
	data, err := s.options.CoverImage(ctx, imageURL)
	if err != nil {
		return "", fmt.Errorf("下载封面失败：%w", err)
	}
	if int64(len(data)) > s.options.MaxImageSize {
		return "", errors.New("封面图片超过大小限制")
	}
	ext := strings.ToLower(filepath.Ext(strings.SplitN(imageURL, "?", 2)[0]))
	if ext == "" {
		return "", errors.New("封面 URL 缺少扩展名")
	}
	filename := md5String(imageURL) + ext
	relative := filepath.ToSlash(filepath.Join(filename[:1], filename))
	target := filepath.Join(s.store.DataDir(), "files", filepath.FromSlash(relative))
	if err := writeScoped(filepath.Join(s.store.DataDir(), "files"), target, data); err != nil {
		return "", err
	}
	return relative, nil
}

func md5String(value string) string {
	digest := md5.Sum([]byte(value))
	return hex.EncodeToString(digest[:])
}
func valueString(value any) string { text, _ := value.(string); return text }
func valueBool(value any) bool     { flag, _ := value.(bool); return flag }
func valueInt(value any) int {
	switch number := value.(type) {
	case int:
		return number
	case int64:
		return int(number)
	case float64:
		return int(number)
	case string:
		n, _ := strconv.Atoi(number)
		return n
	}
	return 0
}
func cloneMap(value map[string]any) map[string]any {
	result := make(map[string]any, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
}

func writeScoped(root, target string, data []byte) error {
	rootAbs, err := canonicalPath(root)
	if err != nil {
		return err
	}
	if err := ensureDirectory(rootAbs); err != nil {
		return err
	}
	rootAbs, err = canonicalPath(rootAbs)
	if err != nil {
		return err
	}
	targetAbs, err := canonicalPath(target)
	if err != nil {
		return err
	}
	if info, err := os.Lstat(targetAbs); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("媒体写入目标不能是符号链接")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	rel, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("文件目标超出已确认目录")
	}
	current := rootAbs
	for _, part := range strings.Split(filepath.Dir(rel), string(filepath.Separator)) {
		if part == "." || part == "" {
			continue
		}
		current = filepath.Join(current, part)
		if info, err := os.Lstat(current); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return errors.New("媒体写入路径不能穿过符号链接")
		}
	}
	if err := os.MkdirAll(filepath.Dir(targetAbs), 0755); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(targetAbs), ".ani-rss-*")
	if err != nil {
		return err
	}
	temp := file.Name()
	defer os.Remove(temp)
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Chmod(temp, 0644); err != nil {
		return err
	}
	return os.Rename(temp, targetAbs)
}

func copyFile(source, target string) error {
	if info, err := os.Lstat(source); err != nil {
		return err
	} else if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("媒体源文件不能是符号链接")
	}
	if info, err := os.Lstat(target); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("媒体目标文件不能是符号链接")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	parent, err := canonicalPath(filepath.Dir(target))
	if err != nil {
		return err
	}
	if parent != filepath.Clean(filepath.Dir(target)) {
		return errors.New("媒体目标路径不能穿过符号链接")
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(target), ".ani-rss-move-*")
	if err != nil {
		return err
	}
	temp := file.Name()
	defer os.Remove(temp)
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(temp, target)
}

func sameFileContents(left, right string) (bool, error) {
	leftFile, err := os.Open(left)
	if err != nil {
		return false, err
	}
	defer leftFile.Close()
	rightFile, err := os.Open(right)
	if err != nil {
		return false, err
	}
	defer rightFile.Close()
	leftHash, rightHash := sha256.New(), sha256.New()
	if _, err := io.Copy(leftHash, leftFile); err != nil {
		return false, err
	}
	if _, err := io.Copy(rightHash, rightFile); err != nil {
		return false, err
	}
	return bytes.Equal(leftHash.Sum(nil), rightHash.Sum(nil)), nil
}
