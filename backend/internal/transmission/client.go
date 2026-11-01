// Package transmission implements the Transmission 4.x legacy RPC protocol.
package transmission

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const endpoint = "/transmission/rpc"

// Config contains the connection details for a Transmission RPC endpoint.
type Config struct {
	URL        string
	Username   string
	Password   string
	HTTPClient *http.Client
}

// Client is safe for concurrent use. Session challenges update shared state atomically.
type Client struct {
	config    Config
	http      *http.Client
	url       string
	mu        sync.RWMutex
	sessionID string
}

// RPCError is a legacy Transmission error encoded in the response's result string.
type RPCError struct{ Result string }

func (e *RPCError) Error() string { return "transmission RPC error: " + e.Result }

// New creates a Transmission RPC client. URL may be the server base URL or RPC URL.
func New(config Config) *Client {
	httpClient := config.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	rpcURL := strings.TrimRight(config.URL, "/")
	if !strings.HasSuffix(rpcURL, endpoint) {
		rpcURL += endpoint
	}
	return &Client{config: config, http: httpClient, url: rpcURL}
}

// Call invokes a legacy RPC method and returns its arguments object as raw JSON.
func (c *Client) Call(ctx context.Context, method string, args map[string]any) (json.RawMessage, error) {
	if strings.TrimSpace(c.config.URL) == "" {
		return nil, errors.New("请先配置 Transmission 服务地址")
	}
	if args == nil {
		args = map[string]any{}
	}
	body, err := json.Marshal(struct {
		Method string         `json:"method"`
		Args   map[string]any `json:"arguments"`
	}{Method: method, Args: args})
	if err != nil {
		return nil, fmt.Errorf("encode Transmission RPC request: %w", err)
	}
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, strings.NewReader(string(body)))
		if err != nil {
			return nil, fmt.Errorf("create Transmission RPC request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.SetBasicAuth(c.config.Username, c.config.Password)
		c.mu.RLock()
		session := c.sessionID
		c.mu.RUnlock()
		if session != "" {
			req.Header.Set("X-Transmission-Session-Id", session)
		}
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, fmt.Errorf("Transmission RPC request failed: %w", err)
		}
		if resp.StatusCode == http.StatusConflict {
			challenge := resp.Header.Get("X-Transmission-Session-Id")
			_ = resp.Body.Close()
			if challenge == "" {
				return nil, errors.New("Transmission RPC 会话挑战缺少会话 ID")
			}
			c.mu.Lock()
			c.sessionID = challenge
			c.mu.Unlock()
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			_ = resp.Body.Close()
			return nil, fmt.Errorf("Transmission RPC HTTP 状态码：%d", resp.StatusCode)
		}
		responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("读取 Transmission RPC 响应失败：%w", readErr)
		}
		var envelope struct {
			Arguments json.RawMessage `json:"arguments"`
			Result    json.RawMessage `json:"result"`
		}
		if err := json.Unmarshal(responseBody, &envelope); err != nil {
			return nil, fmt.Errorf("解析 Transmission RPC 响应失败：%w", err)
		}
		var result string
		if len(envelope.Result) == 0 {
			return nil, errors.New("Transmission RPC 响应缺少 result 字段")
		}
		if err := json.Unmarshal(envelope.Result, &result); err != nil {
			return nil, fmt.Errorf("Transmission RPC result 必须是字符串：%w", err)
		}
		if result != "success" {
			return nil, &RPCError{Result: result}
		}
		if len(envelope.Arguments) == 0 || string(envelope.Arguments) == "null" {
			return json.RawMessage(`{}`), nil
		}
		return envelope.Arguments, nil
	}
	return nil, errors.New("Transmission RPC 会话挑战重试次数超限")
}

// Torrent contains commonly requested fields; unmodeled fields remain available in Raw.
type Torrent struct {
	ID          int               `json:"id"`
	Hash        string            `json:"hashString"`
	Name        string            `json:"name"`
	Status      int               `json:"status"`
	DownloadDir string            `json:"downloadDir"`
	Labels      []string          `json:"labels"`
	Files       []TorrentFile     `json:"files"`
	FileStats   []TorrentFileStat `json:"fileStats"`
	IsFinished  bool              `json:"isFinished"`
	TotalSize   int64             `json:"totalSize"`
	HaveValid   int64             `json:"haveValid"`
	Raw         json.RawMessage   `json:"-"`
}

// TorrentFile describes one file reported by torrent-get.
type TorrentFile struct {
	Name           string `json:"name"`
	Length         int64  `json:"length"`
	BytesCompleted int64  `json:"bytesCompleted"`
}

// TorrentFileStat contains mutable per-file state returned by torrent-get.
type TorrentFileStat struct {
	BytesCompleted int64 `json:"bytesCompleted"`
	Wanted         bool  `json:"wanted"`
	Priority       int   `json:"priority"`
}

// List retrieves torrents using the requested legacy field names (for example hashString).
func (c *Client) List(ctx context.Context, fields []string) ([]Torrent, error) {
	result, err := c.Call(ctx, "torrent-get", map[string]any{"fields": fields})
	if err != nil {
		return nil, err
	}
	var response struct {
		Torrents []json.RawMessage `json:"torrents"`
	}
	var responseFields map[string]json.RawMessage
	if err := json.Unmarshal(result, &responseFields); err != nil {
		return nil, fmt.Errorf("解析 Transmission 种子列表失败：%w", err)
	}
	torrentsJSON, exists := responseFields["torrents"]
	if !exists || strings.TrimSpace(string(torrentsJSON)) == "null" {
		return nil, errors.New("Transmission RPC 响应缺少 torrents 数组")
	}
	if err := json.Unmarshal(result, &response); err != nil {
		return nil, fmt.Errorf("解析 Transmission 种子列表失败：%w", err)
	}
	torrents := make([]Torrent, 0, len(response.Torrents))
	for _, raw := range response.Torrents {
		var torrent Torrent
		if err := json.Unmarshal(raw, &torrent); err != nil {
			return nil, fmt.Errorf("解析 Transmission 种子失败：%w", err)
		}
		torrent.Raw = append(json.RawMessage(nil), raw...)
		torrents = append(torrents, torrent)
	}
	return torrents, nil
}

// AddRequest describes a magnet or metainfo torrent to add.
type AddRequest struct {
	Magnet      string
	Metainfo    []byte
	DownloadDir string
	Labels      []string
	Paused      bool
}

// AddResult identifies an added torrent or an already-existing duplicate.
type AddResult struct {
	Hash      string
	Duplicate bool
}

// Add adds a torrent and reports duplicate responses as a successful domain result.
func (c *Client) Add(ctx context.Context, request AddRequest) (AddResult, error) {
	args := map[string]any{"download-dir": request.DownloadDir, "labels": request.Labels, "paused": request.Paused}
	if len(request.Metainfo) != 0 {
		args["metainfo"] = base64.StdEncoding.EncodeToString(request.Metainfo)
	} else if request.Magnet != "" {
		args["filename"] = request.Magnet
	} else {
		return AddResult{}, errors.New("Transmission add requires magnet or metainfo")
	}
	result, err := c.Call(ctx, "torrent-add", args)
	if err != nil {
		return AddResult{}, err
	}
	var response struct {
		Added *struct {
			Hash string `json:"hashString"`
		} `json:"torrent-added"`
		Duplicate *struct {
			Hash string `json:"hashString"`
		} `json:"torrent-duplicate"`
	}
	if err := json.Unmarshal(result, &response); err != nil {
		return AddResult{}, fmt.Errorf("decode Transmission add result: %w", err)
	}
	if response.Added != nil {
		return AddResult{Hash: response.Added.Hash}, nil
	}
	if response.Duplicate != nil {
		return AddResult{Hash: response.Duplicate.Hash, Duplicate: true}, nil
	}
	return AddResult{}, errors.New("Transmission add response omitted torrent-added/torrent-duplicate")
}

// Remove removes a torrent, with deleteFiles controlling local data deletion explicitly.
func (c *Client) Remove(ctx context.Context, hash string, deleteFiles bool) error {
	_, err := c.Call(ctx, "torrent-remove", map[string]any{"ids": []string{hash}, "delete-local-data": deleteFiles})
	return err
}

// Rename changes a torrent's file or directory path through torrent-rename-path.
func (c *Client) Rename(ctx context.Context, hash, path, name string) error {
	_, err := c.Call(ctx, "torrent-rename-path", map[string]any{"ids": []string{hash}, "path": path, "name": name})
	return err
}

// SetLocation changes a torrent's download location and moves its data.
func (c *Client) SetLocation(ctx context.Context, hash, location string) error {
	_, err := c.Call(ctx, "torrent-set-location", map[string]any{"ids": []string{hash}, "location": location, "move": true})
	return err
}

// SetLabels replaces the labels attached to a torrent.
func (c *Client) SetLabels(ctx context.Context, hash string, labels []string) error {
	_, err := c.Call(ctx, "torrent-set", map[string]any{"ids": []string{hash}, "labels": labels})
	return err
}

// SetFilesWanted marks file IDs for wanted or unwanted download.
func (c *Client) SetFilesWanted(ctx context.Context, hash string, fileIDs []int, wanted bool) error {
	key := "files-unwanted"
	if wanted {
		key = "files-wanted"
	}
	_, err := c.Call(ctx, "torrent-set", map[string]any{"ids": []string{hash}, key: fileIDs})
	return err
}

// Start resumes a torrent after its selected files and names have been applied.
func (c *Client) Start(ctx context.Context, hash string) error {
	_, err := c.Call(ctx, "torrent-start", map[string]any{"ids": []string{hash}})
	return err
}
