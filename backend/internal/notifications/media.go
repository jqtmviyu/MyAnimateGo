package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (s *Service) refreshEmby(ctx context.Context, channel map[string]any) error {
	if delay := int64Value(channel["embyDelayed"]); delay > 0 {
		if err := s.options.Sleep(ctx, delay); err != nil {
			return err
		}
	}
	views, err := s.embyViews(ctx, channel)
	if err != nil {
		return err
	}
	wanted := stringSet(channel["embyRefreshViewIds"])
	for _, view := range views {
		if !wanted[view.ID] {
			continue
		}
		endpoint := strings.TrimRight(stringValue(channel["embyHost"]), "/") + "/emby/Items/" + url.PathEscape(view.ID) + "/Refresh?Recursive=true&ImageRefreshMode=Default&MetadataRefreshMode=Default&ReplaceAllImages=false&ReplaceAllMetadata=false"
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
		if err != nil {
			return err
		}
		request.Header.Set("X-Emby-Token", stringValue(channel["embyApiKey"]))
		if err := s.doNotificationRequest(request); err != nil {
			return err
		}
	}
	return nil
}

type EmbyView struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (s *Service) embyViews(ctx context.Context, channel map[string]any) ([]EmbyView, error) {
	host, token := strings.TrimRight(stringValue(channel["embyHost"]), "/"), stringValue(channel["embyApiKey"])
	if host == "" || token == "" {
		return nil, errors.New("Emby Host 或 API Key 为空")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, host+"/Library/MediaFolders", nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("X-Emby-Token", token)
	response, err := s.options.HTTPClient.Do(request)
	if err != nil {
		return nil, ErrDeliveryUncertain
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, notificationHTTPError(response.StatusCode)
	}
	var envelope struct {
		Items json.RawMessage `json:"Items"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&envelope); err != nil {
		return nil, err
	}
	if len(envelope.Items) == 0 || bytes.Equal(bytes.TrimSpace(envelope.Items), []byte("null")) {
		return nil, errors.New("Emby 媒体库响应缺少 Items 列表")
	}
	var items []EmbyView
	if err := json.Unmarshal(envelope.Items, &items); err != nil {
		return nil, fmt.Errorf("解析 Emby 媒体库列表失败：%w", err)
	}
	if items == nil {
		return nil, errors.New("Emby 媒体库 Items 不是列表")
	}
	return items, nil
}

func sleepContext(ctx context.Context, seconds int64) error {
	timer := time.NewTimer(time.Duration(seconds) * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func int64Value(value any) int64 {
	switch number := value.(type) {
	case float64:
		return int64(number)
	case int64:
		return number
	case int:
		return int64(number)
	}
	return 0
}

func stringList(value any) []string {
	result := []string{}
	switch list := value.(type) {
	case []any:
		for _, item := range list {
			if text, ok := item.(string); ok {
				result = append(result, text)
			}
		}
	case []string:
		result = append(result, list...)
	}
	return result
}

func stringSet(value any) map[string]bool {
	result := map[string]bool{}
	for _, item := range stringList(value) {
		result[item] = true
	}
	return result
}
