package server

import (
	"fmt"
	"net/http"
	"strings"
)

// Route 让业务模块提供协议入口，统一由服务端处理访问控制。
type Route struct {
	Method  string
	Path    string
	Public  bool
	Handler http.Handler
}

func (s *Server) Register(routes ...Route) error {
	s.routesMu.Lock()
	defer s.routesMu.Unlock()
	for _, route := range routes {
		key := strings.ToLower(route.Path)
		if _, exists := s.routes[key]; exists {
			return fmt.Errorf("接口重复注册：%s", route.Path)
		}
		s.routes[key] = route
	}
	return nil
}

// RouteCatalog 枚举实际业务入口，内置接口与模块注册接口一起供文档核对。
func (s *Server) RouteCatalog() []Route {
	routes := []Route{
		{Path: "/api/ping", Public: true},
		{Method: http.MethodPost, Path: "/api/login", Public: true},
	}
	for _, path := range []string{"config", "setConfig", "listAni", "addAni", "setAni", "importAni", "deleteAni", "batchEnable"} {
		routes = append(routes, Route{Method: http.MethodPost, Path: "/api/" + path})
	}
	if s.development {
		routes = append(routes, Route{Method: http.MethodPost, Path: "/api/setAutomaticTasksPaused"})
	}
	s.routesMu.RLock()
	defer s.routesMu.RUnlock()
	for _, route := range s.routes {
		routes = append(routes, route)
	}
	return routes
}

// Respond 保持业务模块响应与原版 Result 的字段和时间戳一致。
func Respond(w http.ResponseWriter, code int, message string, data any) {
	writeResult(w, result{Code: code, Message: message, Data: data})
}
