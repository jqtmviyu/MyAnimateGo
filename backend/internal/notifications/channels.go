package notifications

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type rejectedDelivery struct{ status int }

func (e rejectedDelivery) Error() string { return fmt.Sprintf("通知服务返回 HTTP %d", e.status) }

func notificationHTTPError(status int) error {
	if status == http.StatusRequestTimeout || status >= 500 {
		return ErrDeliveryUncertain
	}
	return rejectedDelivery{status: status}
}

func (s *Service) deliverChannel(ctx context.Context, global, channel, sub map[string]any, text, status string) error {
	switch strings.ToUpper(stringValue(channel["notificationType"])) {
	case "WEB_HOOK":
		message, err := s.render(ctx, global, channel, sub, text, status)
		if err != nil {
			return err
		}
		return s.sendWebhook(ctx, channel, sub, text, status, message)
	case "EMBY_REFRESH":
		return s.refreshEmby(ctx, channel)
	default:
		return errors.New("仅支持 Webhook 和 Emby 媒体库刷新")
	}
}

func (s *Service) sendWebhook(ctx context.Context, channel, sub map[string]any, text, status, message string) error {
	method := strings.ToUpper(stringValue(channel["webHookMethod"]))
	if method == "" {
		method = http.MethodPost
	}
	urlText, err := s.renderFields(ctx, map[string]any{}, sub, stringValue(channel["webHookUrl"]), text, status)
	if err != nil {
		return err
	}
	body, err := s.renderFields(ctx, map[string]any{}, sub, stringValue(channel["webHookBody"]), text, status)
	if err != nil {
		return err
	}
	escaped := jsonEscaped(message)
	image := stringValue(sub["image"])
	urlText = strings.ReplaceAll(strings.ReplaceAll(urlText, "${notification}", escaped), "${message}", escaped)
	body = strings.ReplaceAll(strings.ReplaceAll(body, "${notification}", escaped), "${message}", escaped)
	urlText = strings.ReplaceAll(urlText, "${image}", image)
	body = strings.ReplaceAll(body, "${image}", image)
	if strings.Contains(urlText, "${imageBase64}") || strings.Contains(body, "${imageBase64}") {
		encoded, err := s.coverBase64(sub)
		if err != nil {
			return err
		}
		urlText = strings.ReplaceAll(urlText, "${imageBase64}", encoded)
		body = strings.ReplaceAll(body, "${imageBase64}", encoded)
	}
	if strings.TrimSpace(urlText) == "" {
		return errors.New("webhook URL 不能为空")
	}
	var requestBody io.Reader
	if body != "" {
		requestBody = strings.NewReader(body)
	}
	request, err := http.NewRequestWithContext(ctx, method, urlText, requestBody)
	if err != nil {
		return err
	}
	for _, line := range strings.Split(stringValue(channel["webHookHeader"]), "\n") {
		name, value, ok := strings.Cut(line, ":")
		if ok && strings.TrimSpace(name) != "" {
			request.Header.Set(strings.TrimSpace(name), strings.TrimSpace(value))
		}
	}
	if body != "" && request.Header.Get("Content-Type") == "" {
		request.Header.Set("Content-Type", "application/json")
	}
	return s.doNotificationRequest(request)
}

func jsonEscaped(value string) string {
	raw, _ := json.Marshal(value)
	return strings.Trim(string(raw), `"`)
}

func (s *Service) doNotificationRequest(request *http.Request) error {
	response, err := s.options.HTTPClient.Do(request)
	if err != nil {
		return ErrDeliveryUncertain
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return notificationHTTPError(response.StatusCode)
	}
	return nil
}

func (s *Service) coverBase64(sub map[string]any) (string, error) {
	path, err := s.localCoverPath(sub)
	if err != nil {
		return base64.StdEncoding.EncodeToString([]byte("GIF89a\x01\x00\x01\x00\x80\x00\x00\x00\x00\x00\xff\xff\xff!\xf9\x04\x01\x00\x00\x00\x00,\x00\x00\x00\x00\x01\x00\x01\x00\x00\x02\x02D\x01\x00;")), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

func (s *Service) localCoverPath(sub map[string]any) (string, error) {
	store, ok := s.store.(interface{ DataDir() string })
	if !ok {
		return "", errors.New("封面目录未配置")
	}
	cover := strings.TrimSpace(stringValue(sub["cover"]))
	if cover == "" {
		return "", errors.New("订阅封面为空")
	}
	root, err := filepath.Abs(filepath.Join(store.DataDir(), "files"))
	if err != nil {
		return "", err
	}
	path := cover
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, cover)
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("订阅封面超出附件目录")
	}
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("订阅封面不能是符号链接")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	relative, err = filepath.Rel(root, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("订阅封面超出附件目录")
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("订阅封面不是普通文件")
	}
	return resolved, nil
}
