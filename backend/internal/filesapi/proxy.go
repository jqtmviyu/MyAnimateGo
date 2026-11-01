package filesapi

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const maxProxiedImage = 25 << 20

func (s *Service) proxyImage(w http.ResponseWriter, r *http.Request) {
	encoded := strings.ReplaceAll(r.URL.Query().Get("imgUrl"), " ", "+")
	imageURL, err := decodeBase64(encoded)
	if err != nil || strings.TrimSpace(imageURL) == "" {
		http.Error(w, "图片 URL 格式错误", http.StatusBadRequest)
		return
	}
	current, err := url.Parse(imageURL)
	if err != nil || (current.Scheme != "http" && current.Scheme != "https") || current.Hostname() == "" {
		http.Error(w, "图片 URL 必须使用 HTTP 或 HTTPS", http.StatusBadRequest)
		return
	}
	ext := strings.ToLower(filepath.Ext(current.Path))
	if !supportedImage(ext) {
		http.Error(w, "图片 URL 缺少有效扩展名", http.StatusBadRequest)
		return
	}
	digest := md5.Sum([]byte(imageURL))
	hash := hex.EncodeToString(digest[:])
	cachePath := filepath.Join(s.store.DataDir(), "img", hash[:1], hash+ext)
	if _, err := os.Stat(cachePath); errors.Is(err, os.ErrNotExist) {
		data, contentType, err := s.fetchImage(r.Context(), current)
		if err != nil {
			http.Error(w, "图片下载失败", http.StatusBadGateway)
			return
		}
		if err := writeScoped(filepath.Join(s.store.DataDir(), "img"), cachePath, data); err != nil {
			http.Error(w, "缓存图片失败", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", contentType)
	} else if err != nil {
		http.Error(w, "读取图片缓存失败", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=2592000")
	if w.Header().Get("Content-Type") == "" {
		if contentType := mimeType(ext); contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
	}
	http.ServeFile(w, r, cachePath)
}

func (s *Service) fetchImage(ctx context.Context, target *url.URL) ([]byte, string, error) {
	client, err := s.doerFor(ctx, target.Hostname())
	if err != nil {
		return nil, "", err
	}
	for redirects := 0; redirects <= 5; redirects++ {
		if err := s.validateRemote(ctx, target); err != nil {
			return nil, "", err
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
		if err != nil {
			return nil, "", err
		}
		request.Header.Set("User-Agent", "backend")
		response, err := client.Do(request)
		if err != nil {
			return nil, "", err
		}
		if response.StatusCode >= 300 && response.StatusCode < 400 {
			location := response.Header.Get("Location")
			_ = response.Body.Close()
			if location == "" || redirects == 5 {
				return nil, "", errors.New("图片重定向次数过多或缺少目标")
			}
			next, err := target.Parse(location)
			if err != nil {
				return nil, "", err
			}
			target = next
			continue
		}
		defer response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
			return nil, "", fmt.Errorf("图片服务器返回 HTTP %d", response.StatusCode)
		}
		data, err := io.ReadAll(io.LimitReader(response.Body, maxProxiedImage+1))
		if err != nil {
			return nil, "", err
		}
		if len(data) > maxProxiedImage {
			return nil, "", errors.New("图片超过大小限制")
		}
		contentType := response.Header.Get("Content-Type")
		if contentType == "" {
			contentType = mimeType(target.Path)
		}
		if !strings.HasPrefix(strings.ToLower(contentType), "image/") {
			return nil, "", errors.New("图片服务器返回的内容类型无效")
		}
		return data, contentType, nil
	}
	return nil, "", errors.New("图片重定向次数过多")
}

func supportedImage(extension string) bool {
	switch extension {
	case ".png", ".jpg", ".jpeg", ".webp", ".svg", ".gif":
		return true
	default:
		return false
	}
}

func (s *Service) validateRemote(ctx context.Context, target *url.URL) error {
	if (target.Scheme != "http" && target.Scheme != "https") || target.Hostname() == "" || target.User != nil {
		return errors.New("图片 URL 不安全")
	}
	host := strings.TrimSuffix(strings.ToLower(target.Hostname()), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || host == "metadata.google.internal" {
		return errors.New("图片 URL 指向本机网络")
	}
	var ips []net.IP
	var err error
	if ip := net.ParseIP(host); ip != nil {
		ips = []net.IP{ip}
	} else {
		ips, err = s.options.ResolveHost(ctx, host)
	}
	if err != nil || len(ips) == 0 {
		return errors.New("无法安全解析图片服务器地址")
	}
	for _, ip := range ips {
		if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
			return errors.New("图片 URL 指向内部网络")
		}
	}
	return nil
}

func mimeType(path string) string {
	return mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))
}
