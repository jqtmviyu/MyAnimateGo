package discovery

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/jobs"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/server"
)

const JobEpisodeCount = "subscription-episode-count"

type episodeCountPayload struct {
	IDs   []string `json:"ids"`
	Force bool     `json:"force"`
}

func (c *Client) episodeCountHTTPRoute() server.Route {
	return server.Route{Method: http.MethodPost, Path: "/api/updateTotalEpisodeNumber", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		force, err := strconv.ParseBool(r.URL.Query().Get("force"))
		if err != nil {
			server.Respond(w, 500, "force 必须是布尔值", nil)
			return
		}
		var ids []string
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&ids); err != nil || len(ids) == 0 {
			server.Respond(w, 500, "未选择订阅", nil)
			return
		}
		queue, ok := c.store.(interface {
			EnqueueJob(context.Context, string, string, json.RawMessage) (jobs.Job, error)
		})
		if !ok {
			server.Respond(w, 500, "更新总集数需要持久任务队列", nil)
			return
		}
		payload, err := json.Marshal(episodeCountPayload{IDs: ids, Force: force})
		if err == nil {
			_, err = queue.EnqueueJob(r.Context(), JobEpisodeCount, "manual-episode-count:"+rand.Text(), payload)
		}
		if err != nil {
			server.Respond(w, 500, err.Error(), nil)
			return
		}
		server.Respond(w, 200, "已开始更新总集数", nil)
	})}
}

// MetadataJobHandlers 让手动总集数更新沿用应用的持久工作者和恢复流程。
func (c *Client) MetadataJobHandlers() map[string]jobs.Handler {
	return map[string]jobs.Handler{JobEpisodeCount: episodeCountHandler{client: c}}
}

type episodeCountHandler struct{ client *Client }

func (h episodeCountHandler) Reconcile(_ context.Context, job jobs.Job) (bool, error) {
	var payload episodeCountPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return false, err
	}
	if len(payload.IDs) == 0 {
		return false, errors.New("总集数任务缺少订阅 ID")
	}
	return false, nil
}

func (h episodeCountHandler) Execute(ctx context.Context, job jobs.Job) error {
	var payload episodeCountPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return err
	}
	return h.client.refreshSubscriptionMetadata(ctx, payload.IDs, payload.Force, false)
}

// RefreshAutomaticMetadata 更新启用订阅的评分，并按全局配置决定是否更新总集数。
func (c *Client) RefreshAutomaticMetadata(ctx context.Context) error {
	return c.refreshSubscriptionMetadata(ctx, nil, false, true)
}

func (c *Client) refreshSubscriptionMetadata(ctx context.Context, ids []string, force, automatic bool) error {
	config, err := c.config(ctx)
	if err != nil {
		return err
	}
	subscriptions, err := c.subscriptions(ctx)
	if err != nil {
		return err
	}
	selected := map[string]bool{}
	for _, id := range ids {
		selected[id] = true
	}
	var failures []error
	for _, ani := range subscriptions {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(failures, err)...)
		}
		if automatic && !boolean(ani["enable"]) || !automatic && !selected[text(ani["id"])] {
			continue
		}
		id, err := c.subjectIDForAni(ctx, ani)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		info, err := c.bgmSubject(ctx, config, id, true)
		if err != nil {
			failures = append(failures, fmt.Errorf("刷新订阅 %s 元数据失败：%w", text(ani["id"]), err))
			continue
		}
		fields := map[string]any{}
		if automatic {
			score := 0.0
			if info.Rating != nil {
				score = info.Rating.Score
			}
			fields["score"] = score
		}
		updateTotal := !automatic || boolean(config["updateTotalEpisodeNumber"])
		forced := force || automatic && boolean(config["forceUpdateTotalEpisodeNumber"])
		if updateTotal && (forced || integer(ani["totalEpisodeNumber"]) < 1) {
			eps := info.Eps
			if eps > 0 {
				if list, err := c.bgmEpisodes(ctx, id, intPtr(0)); err == nil && len(list) > 0 {
					eps = len(list)
				}
			}
			fields["totalEpisodeNumber"] = eps
		}
		if len(fields) == 0 {
			continue
		}
		if store, ok := c.store.(interface {
			UpdateSubscriptionFields(context.Context, string, map[string]any) error
		}); ok {
			err = store.UpdateSubscriptionFields(ctx, text(ani["id"]), fields)
		} else {
			for key, value := range fields {
				ani[key] = value
			}
			err = c.store.PutSubscription(ctx, ani)
		}
		if err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
