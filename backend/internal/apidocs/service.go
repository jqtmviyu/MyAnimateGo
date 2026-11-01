// Package apidocs 根据当前 Go 接口定义提供 OpenAPI 与本地 Swagger UI。
package apidocs

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/buildinfo"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/server"
)

//go:embed openapi.json assets/*
var files embed.FS

type object = map[string]any

type Service struct {
	document []byte
	routes   []server.Route
}

// New 只发布实际挂载的路由，新增接口必须同时补充静态定义。
func New(routes []server.Route) (*Service, error) {
	raw, err := files.ReadFile("openapi.json")
	if err != nil {
		return nil, err
	}
	var document object
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, err
	}
	definitions := document["paths"].(object)
	paths := object{}
	for _, route := range routes {
		operations, exists := definitions[route.Path].(map[string]any)
		if !exists {
			return nil, fmt.Errorf("接口 %s 缺少 OpenAPI 定义", route.Path)
		}
		selected := object{}
		for method, raw := range operations {
			if route.Method != "" && method != strings.ToLower(route.Method) {
				continue
			}
			operation := raw.(map[string]any)
			if route.Public {
				operation["security"] = []any{}
			}
			selected[method] = operation
		}
		if len(selected) == 0 {
			return nil, fmt.Errorf("接口 %s 的 OpenAPI 方法不匹配", route.Path)
		}
		paths[route.Path] = selected
	}
	document["paths"] = paths
	document["info"].(object)["version"] = buildinfo.Version
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, err
	}
	s := &Service{document: encoded}
	s.add("/api/v3/api-docs", "application/json;charset=UTF-8", encoded)
	s.add("/v3/api-docs", "application/json;charset=UTF-8", encoded)
	configuration := []byte(`{"url":"/api/v3/api-docs","validatorUrl":null}`)
	s.add("/api/v3/api-docs/swagger-config", "application/json;charset=UTF-8", configuration)
	s.add("/v3/api-docs/swagger-config", "application/json;charset=UTF-8", configuration)
	s.add("/swagger-ui/index.html", "text/html;charset=UTF-8", []byte(indexHTML))
	for _, asset := range []struct{ name, kind string }{{"swagger-ui-bundle.js", "application/javascript;charset=UTF-8"}, {"swagger-ui.css", "text/css;charset=UTF-8"}, {"swagger-ui-bundle.js.LICENSE.txt", "text/plain;charset=UTF-8"}, {"LICENSE", "text/plain;charset=UTF-8"}} {
		content, err := files.ReadFile("assets/" + asset.name)
		if err != nil {
			return nil, err
		}
		s.add("/swagger-ui/"+asset.name, asset.kind, content)
	}
	for _, path := range []string{"/swagger-ui.html", "/swagger-ui", "/swagger-ui/"} {
		s.routes = append(s.routes, server.Route{Method: http.MethodGet, Path: path, Public: true, Handler: http.RedirectHandler("/swagger-ui/index.html", http.StatusFound)})
	}
	return s, nil
}

func (s *Service) Routes() []server.Route { return append([]server.Route(nil), s.routes...) }
func (s *Service) IsPath(path string) bool {
	for _, route := range s.routes {
		if strings.EqualFold(route.Path, path) {
			return true
		}
	}
	return false
}
func (s *Service) add(path, kind string, content []byte) {
	s.routes = append(s.routes, server.Route{Method: http.MethodGet, Path: path, Public: true, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", kind)
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeContent(w, r, path, time.Time{}, bytes.NewReader(content))
	})})
}

const indexHTML = `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>MyAnimeGo API</title><link rel="icon" href="data:,"><link rel="stylesheet" href="/swagger-ui/swagger-ui.css"></head><body><div id="swagger-ui"></div><script src="/swagger-ui/swagger-ui-bundle.js"></script><script>window.ui=SwaggerUIBundle({url:"/api/v3/api-docs",dom_id:"#swagger-ui",deepLinking:true,validatorUrl:null,persistAuthorization:false});</script></body></html>`
