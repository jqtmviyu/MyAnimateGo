// Package mcpapi 通过标准 Streamable HTTP transport 提供订阅工具。
package mcpapi

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/buildinfo"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Store 提供订阅查询和 API Key 配置。
type Store interface {
	GetConfig(context.Context) (map[string]any, error)
	ListSubscriptions(context.Context) ([]map[string]any, error)
}

// MikanSeason 描述可选的季度筛选条件。
type MikanSeason struct {
	Year        *int    `json:"year,omitempty" jsonschema:"番剧年份"`
	Season      *string `json:"season,omitempty" jsonschema:"春夏秋冬季度"`
	SeasonLabel *string `json:"seasonLabel,omitempty" jsonschema:"季度显示名称"`
	Select      *bool   `json:"select,omitempty" jsonschema:"是否为当前季度"`
}

type mikanSeasonDTO struct {
	Year        int     `json:"year" jsonschema:"番剧年份"`
	Season      string  `json:"season" jsonschema:"春夏秋冬季度"`
	SeasonLabel *string `json:"seasonLabel,omitempty"`
	Select      *bool   `json:"select,omitempty"`
}

type listSubscriptionsDTO struct {
	Enabled *bool `json:"enabled,omitempty" jsonschema:"按启用状态过滤；省略时返回全部订阅"`
}

type searchMikanDTO struct {
	Text   *string         `json:"text,omitempty" jsonschema:"搜索关键词，省略时使用空字符串"`
	Season *mikanSeasonDTO `json:"season,omitempty" jsonschema:"可选的季度过滤条件"`
}

// SearchMikanInput 是传给 Mikan 查询业务的规范化参数。
type SearchMikanInput struct {
	Text   string
	Season *MikanSeason
}

// StandbyRSS 描述新增订阅时的备用 RSS。
type StandbyRSS struct {
	Label  string `json:"label" jsonschema:"字幕组名称"`
	URL    string `json:"url" jsonschema:"RSS 地址"`
	Offset int    `json:"offset" jsonschema:"剧集偏移"`
}

// RssToAniInput 是预览和新增订阅共用的旧版 DTO。
type RssToAniInput struct {
	URL            string       `json:"url" jsonschema:"RSS URL"`
	Type           string       `json:"type" jsonschema:"资源来源类型：mikan、ani-bt、anime-garden 或 other"`
	BGMURL         *string      `json:"bgmUrl,omitempty" jsonschema:"Bangumi 番剧地址"`
	Subgroup       *string      `json:"subgroup,omitempty" jsonschema:"字幕组名称"`
	Enable         *bool        `json:"enable,omitempty" jsonschema:"是否启用订阅"`
	StandbyRssList []StandbyRSS `json:"standbyRssList" jsonschema:"备用 RSS 列表"`
}

// Operations 接收实际业务实现；未接入的回调会作为 MCP 工具错误返回。
type Operations struct {
	SearchMikan              func(context.Context, SearchMikanInput) (any, error)
	SearchAniBT              func(context.Context) (any, error)
	SearchAnimeGarden        func(context.Context) (any, error)
	GetMikanGroups           func(context.Context, string) (any, error)
	GetAniBTGroups           func(context.Context, string) (any, error)
	GetAnimeGardenGroups     func(context.Context, string) (any, error)
	PreviewSubscriptionItems func(context.Context, RssToAniInput) (any, error)
	AddSubscription          func(context.Context, RssToAniInput) (any, error)
}

// Options 控制 MCP 开关、配置仓储和实际业务调用。
type Options struct {
	Guard      func(context.Context) (func(), error)
	Enabled    bool
	Store      Store
	APIKey     func(context.Context) (string, error)
	Operations Operations
}

// Service 保持已注册的工具和 Streamable HTTP 会话状态。
type Service struct {
	options Options
	server  *mcp.Server
	handler http.Handler
}

// New 创建 MCP 服务；Enabled 的零值关闭整个入口。
func New(options Options) *Service {
	service := &Service{options: options}
	service.server = mcp.NewServer(&mcp.Implementation{Name: "myanimego", Version: buildinfo.Version}, &mcp.ServerOptions{
		Instructions: "使用这些工具查询订阅、检索番剧资源、预览 RSS 命中条目或添加订阅。",
	})
	service.registerTools()
	service.handler = mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return service.server }, nil)
	return service
}

// Handler 返回 MCP transport 的 HTTP handler，供运行入口挂载到 /api/mcp。
func (s *Service) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.options.Enabled {
			http.NotFound(w, r)
			return
		}
		key, err := s.apiKey(r.Context())
		if err != nil {
			http.Error(w, "读取 MCP API Key 失败", http.StatusInternalServerError)
			return
		}
		if strings.TrimSpace(key) == "" || !authorizedAPIKey(r, key) {
			http.Error(w, "MCP API Key 无效", http.StatusUnauthorized)
			return
		}
		s.handler.ServeHTTP(w, r)
	})
}

func (s *Service) apiKey(ctx context.Context) (string, error) {
	if s.options.APIKey != nil {
		return s.options.APIKey(ctx)
	}
	if s.options.Store == nil {
		return "", nil
	}
	config, err := s.options.Store.GetConfig(ctx)
	if err != nil {
		return "", err
	}
	key, _ := config["apiKey"].(string)
	return key, nil
}

func authorizedAPIKey(r *http.Request, expected string) bool {
	for _, name := range []string{"api-key", "x-api-key", "s"} {
		value := r.Header.Get(name)
		if value == "" {
			value = r.URL.Query().Get(name)
		}
		if strings.TrimSpace(value) == "" {
			continue
		}
		return subtle.ConstantTimeCompare([]byte(value), []byte(expected)) == 1
	}
	return false
}

type listSubscriptionsInput struct {
	DTO listSubscriptionsDTO `json:"dto"`
}

type searchMikanArgs struct {
	DTO searchMikanDTO `json:"dto"`
}

type rssToAniArgs struct {
	DTO RssToAniInput `json:"dto"`
}

type mikanGroupArgs struct {
	URL string `json:"url" jsonschema:"Mikan 番剧页面 URL"`
}

type bgmIDArgs struct {
	BGMID string `json:"bgmId" jsonschema:"Bangumi 番剧 ID"`
}

func (s *Service) registerTools() {
	addTool(s.server, s.options.Guard, tool("list_subscriptions", "列出现有 MyAnimeGo 订阅，可按启用状态过滤", "订阅列表", true, false, true, boolPointer(false)), func(ctx context.Context, input listSubscriptionsInput) (any, error) {
		if s.options.Store == nil {
			return nil, unavailable("读取订阅列表")
		}
		items, err := s.options.Store.ListSubscriptions(ctx)
		if err != nil {
			return nil, err
		}
		filtered := make([]map[string]any, 0, len(items))
		for _, item := range items {
			if input.DTO.Enabled != nil {
				enabled, ok := item["enable"].(bool)
				if !ok || enabled != *input.DTO.Enabled {
					continue
				}
			}
			filtered = append(filtered, item)
		}
		return filtered, nil
	})
	addTool(s.server, s.options.Guard, tool("search_mikan", "按关键词搜索 Mikan 番剧，可传入季度条件", "搜索 Mikan", true, false, true, boolPointer(true)), func(ctx context.Context, input searchMikanArgs) (any, error) {
		text := ""
		if input.DTO.Text != nil {
			text = *input.DTO.Text
		}
		var season *MikanSeason
		if input.DTO.Season != nil {
			seasonDTO := input.DTO.Season
			season = &MikanSeason{Year: &seasonDTO.Year, Season: &seasonDTO.Season, SeasonLabel: seasonDTO.SeasonLabel, Select: seasonDTO.Select}
		}
		if season == nil {
			season = &MikanSeason{}
		}
		return invokeOne(ctx, "搜索 Mikan", s.options.Operations.SearchMikan, SearchMikanInput{Text: text, Season: season})
	})
	addTool(s.server, s.options.Guard, tool("search_anibt", "搜索 AniBT 番剧", "搜索 AniBT", true, false, true, boolPointer(true)), func(ctx context.Context, _ struct{}) (any, error) {
		return invokeNoInput(ctx, "搜索 AniBT", s.options.Operations.SearchAniBT)
	})
	addTool(s.server, s.options.Guard, tool("search_anime_garden", "搜索 AnimeGarden 番剧列表", "搜索 AnimeGarden", true, false, true, boolPointer(true)), func(ctx context.Context, _ struct{}) (any, error) {
		return invokeNoInput(ctx, "搜索 AnimeGarden", s.options.Operations.SearchAnimeGarden)
	})
	addTool(s.server, s.options.Guard, tool("get_mikan_groups", "根据 Mikan 番剧页面 URL 获取字幕组 RSS", "获取 Mikan 字幕组", true, false, true, boolPointer(true)), func(ctx context.Context, input mikanGroupArgs) (any, error) {
		return invokeOne(ctx, "获取 Mikan 字幕组", s.options.Operations.GetMikanGroups, input.URL)
	})
	addTool(s.server, s.options.Guard, tool("get_anibt_groups", "根据 AniBT BGM ID 获取字幕组 RSS", "获取 AniBT 字幕组", true, false, true, boolPointer(true)), func(ctx context.Context, input bgmIDArgs) (any, error) {
		return invokeOne(ctx, "获取 AniBT 字幕组", s.options.Operations.GetAniBTGroups, input.BGMID)
	})
	addTool(s.server, s.options.Guard, tool("get_anime_garden_groups", "根据 AnimeGarden BGM ID 获取字幕组 RSS", "获取 AnimeGarden 字幕组", true, false, true, boolPointer(true)), func(ctx context.Context, input bgmIDArgs) (any, error) {
		return invokeOne(ctx, "获取 AnimeGarden 字幕组", s.options.Operations.GetAnimeGardenGroups, input.BGMID)
	})
	addTool(s.server, s.options.Guard, tool("preview_subscription_items", "预览某个 RSS 订阅最终会命中的原始剧集条目", "预览订阅条目", true, false, true, boolPointer(true)), func(ctx context.Context, input rssToAniArgs) (any, error) {
		return invokeOne(ctx, "预览 RSS 条目", s.options.Operations.PreviewSubscriptionItems, input.DTO)
	})
	addTool(s.server, s.options.Guard, tool("add_subscription", "添加一个 MyAnimeGo 订阅。需要预览命中条目时请先调用 preview_subscription_items", "添加订阅", false, false, false, boolPointer(false)), func(ctx context.Context, input rssToAniArgs) (any, error) {
		return invokeOne(ctx, "添加订阅", s.options.Operations.AddSubscription, input.DTO)
	})
}

func tool(name, description, title string, readOnly, destructive, idempotent bool, openWorld *bool) *mcp.Tool {
	return &mcp.Tool{
		Name:        name,
		Description: description,
		Title:       title,
		Annotations: &mcp.ToolAnnotations{
			Title:           title,
			ReadOnlyHint:    readOnly,
			DestructiveHint: boolPointer(destructive),
			IdempotentHint:  idempotent,
			OpenWorldHint:   openWorld,
		},
	}
}

func boolPointer(value bool) *bool { return &value }

func invokeOne[Input any](ctx context.Context, action string, operation func(context.Context, Input) (any, error), input Input) (any, error) {
	if operation == nil {
		return nil, unavailable(action)
	}
	return operation(ctx, input)
}

func invokeNoInput(ctx context.Context, action string, operation func(context.Context) (any, error)) (any, error) {
	if operation == nil {
		return nil, unavailable(action)
	}
	return operation(ctx)
}

func unavailable(action string) error { return errors.New(action + "业务尚未接入") }

func addTool[Input any](server *mcp.Server, guard func(context.Context) (func(), error), definition *mcp.Tool, handler func(context.Context, Input) (any, error)) {
	mcp.AddTool[Input, any](server, definition, func(ctx context.Context, _ *mcp.CallToolRequest, input Input) (*mcp.CallToolResult, any, error) {
		if guard != nil {
			release, err := guard(ctx)
			if err != nil {
				return nil, nil, err
			}
			defer release()
		}
		output, err := handler(ctx, input)
		return nil, output, err
	})
}
