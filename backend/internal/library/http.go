package library

import (
	"encoding/json"
	"errors"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/server"
	"net/http"
)

// Routes 提供订阅卡片封面的更新接口。
func (s *Service) Routes() []server.Route {
	return []server.Route{{Method: http.MethodPost, Path: "/api/refreshCover", Handler: http.HandlerFunc(s.handleRefreshCover)}}
}

func (s *Service) handleRefreshCover(w http.ResponseWriter, r *http.Request) {
	var ani map[string]any
	if err := decodeLibraryBody(r, &ani); err != nil {
		server.Respond(w, 500, err.Error(), nil)
		return
	}
	cover, err := s.RefreshCover(r.Context(), valueString(ani["image"]))
	if err != nil {
		server.Respond(w, 500, err.Error(), nil)
		return
	}
	server.Respond(w, 200, "success", cover)
}

func decodeLibraryBody(r *http.Request, target any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 4<<20))
	if err := decoder.Decode(target); err != nil {
		return errors.New("JSON 请求格式错误")
	}
	return nil
}
