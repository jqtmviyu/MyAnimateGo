package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/server"
)

type endpoint func(*http.Request) (any, string, error)

func (c *Client) Routes() []server.Route {
	return []server.Route{
		c.route("/api/mikan", c.mikanRoute),
		c.route("/api/mikanGroup", c.mikanGroupRoute),
		c.route("/api/aniBT", c.aniBTRoute),
		c.route("/api/aniBTGroup", c.aniBTGroupRoute),
		c.route("/api/animeGardenList", c.animeGardenListRoute),
		c.route("/api/animeGardenGroup", c.animeGardenGroupRoute),
		c.route("/api/searchBgm", c.searchBgmRoute),
		c.route("/api/getAniBySubjectId", c.getAniBySubjectIDRoute),
		c.route("/api/getBgmTitle", c.getBgmTitleRoute),
		c.tmdbNameHTTPRoute(),
		c.route("/api/getThemoviedbGroup", c.tmdbGroupRoute),
		c.route("/api/rssToAni", c.rssToAniRoute),
		c.episodeCountHTTPRoute(),
	}
}

func (c *Client) route(path string, handler endpoint) server.Route {
	return server.Route{Method: http.MethodPost, Path: path, Public: false, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, message, err := handler(r)
		if err != nil {
			server.Respond(w, 500, err.Error(), nil)
			return
		}
		if message == "" {
			message = "success"
		}
		server.Respond(w, 200, message, data)
	})}
}

func decodeBody(r *http.Request, target any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 4<<20))
	if err := decoder.Decode(target); err != nil {
		return errors.New("JSON 请求格式错误")
	}
	return nil
}

func bodyMap(r *http.Request) (map[string]any, error) {
	var body map[string]any
	if err := decodeBody(r, &body); err != nil {
		return nil, err
	}
	if body == nil {
		return nil, errors.New("请求体不能为空")
	}
	return body, nil
}

func queryRequired(r *http.Request, name string) (string, error) {
	value := r.URL.Query().Get(name)
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("缺少 query 参数 %s", name)
	}
	return value, nil
}

func queryURL(base string, query url.Values) string {
	if len(query) == 0 {
		return base
	}
	return base + "?" + query.Encode()
}

func decodeJSON(data []byte, target any) error {
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("解析 JSON 失败：%w", err)
	}
	return nil
}

func readAll(r io.Reader) ([]byte, error) { return io.ReadAll(io.LimitReader(r, 8<<20)) }

func (c *Client) mikanRoute(r *http.Request) (any, string, error) {
	text := r.URL.Query().Get("text")
	var season *mikanSeason
	if r.Body != nil && r.ContentLength != 0 {
		var input mikanSeason
		if err := decodeBody(r, &input); err != nil {
			return nil, "", err
		}
		season = &input
	}
	ctx := r.Context()
	if r.URL.Query().Get("refresh") == "true" {
		ctx = context.WithValue(ctx, sourceRefreshKey{}, true)
	}
	data, err := c.searchMikan(ctx, text, season, r.URL.Query().Get("metadata") != "false")
	return data, "success", err
}

func (c *Client) mikanGroupRoute(r *http.Request) (any, string, error) {
	value, err := queryRequired(r, "url")
	if err != nil {
		return nil, "", err
	}
	data, err := c.mikanGroups(r.Context(), value)
	return data, "success", err
}

func (c *Client) aniBTRoute(r *http.Request) (any, string, error) {
	var input struct {
		Season string `json:"season"`
		BgmURL string `json:"bgmUrl"`
		Title  string `json:"title"`
	}
	if err := decodeBody(r, &input); err != nil {
		return nil, "", err
	}
	ctx := r.Context()
	if r.URL.Query().Get("refresh") == "true" {
		ctx = context.WithValue(ctx, sourceRefreshKey{}, true)
	}
	data, err := c.searchAniBT(ctx, input.Season, input.BgmURL, input.Title)
	return data, "success", err
}

func (c *Client) aniBTGroupRoute(r *http.Request) (any, string, error) {
	id, err := queryRequired(r, "bgmId")
	if err != nil {
		return nil, "", err
	}
	data, err := c.aniBTGroups(r.Context(), id)
	return data, "success", err
}

func (c *Client) animeGardenListRoute(r *http.Request) (any, string, error) {
	data, err := c.searchAnimeGarden(r.Context(), r.URL.Query().Get("bgmUrl"))
	return data, "success", err
}

func (c *Client) animeGardenGroupRoute(r *http.Request) (any, string, error) {
	id, err := queryRequired(r, "bgmId")
	if err != nil {
		return nil, "", err
	}
	data, err := c.animeGardenGroups(r.Context(), id)
	return data, "success", err
}

func (c *Client) searchBgmRoute(r *http.Request) (any, string, error) {
	data, err := c.searchBgm(r.Context(), r.URL.Query().Get("name"))
	return data, "success", err
}

func (c *Client) getAniBySubjectIDRoute(r *http.Request) (any, string, error) {
	id, err := queryRequired(r, "id")
	if err != nil {
		return nil, "", err
	}
	data, err := c.aniBySubjectID(r.Context(), id)
	return data, "success", err
}

func (c *Client) getBgmTitleRoute(r *http.Request) (any, string, error) {
	ani, err := bodyMap(r)
	if err != nil {
		return nil, "", err
	}
	data, err := c.bgmTitle(r.Context(), ani)
	return data, "success", err
}

func (c *Client) tmdbNameHTTPRoute() server.Route {
	return server.Route{Method: http.MethodPost, Path: "/api/getThemoviedbName", Public: false, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input tmdbDTO
		if err := decodeBody(r, &input); err != nil {
			server.Respond(w, 500, err.Error(), nil)
			return
		}
		if strings.TrimSpace(input.TMDBID) == "" && strings.TrimSpace(input.Title) == "" {
			server.Respond(w, 500, "TmdbId 或 标题 不能为空", nil)
			return
		}
		result, err := c.tmdbName(r.Context(), input.TMDBID, input.Title, input.OVA)
		if err != nil {
			server.Respond(w, 500, err.Error(), nil)
			return
		}
		if result.ThemoviedbName == "" {
			server.Respond(w, 500, "获取 TMDB 失败", result)
			return
		}
		server.Respond(w, 200, "获取 TMDB 成功", result)
	})}
}

func (c *Client) tmdbGroupRoute(r *http.Request) (any, string, error) {
	ani, err := bodyMap(r)
	if err != nil {
		return nil, "", err
	}
	tmdb, _ := ani["tmdb"].(map[string]any)
	if tmdb == nil || text(tmdb["id"]) == "" {
		return nil, "", errors.New("tmdb 或 tmdb.id 不能为空")
	}
	data, err := c.tmdbGroups(r.Context(), tmdb)
	return data, "success", err
}
