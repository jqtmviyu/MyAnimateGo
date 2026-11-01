package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/jobs"
)

const JobKind = "notification-delivery"

type JobQueue interface {
	EnqueueJob(context.Context, string, string, json.RawMessage) (jobs.Job, error)
}

type notificationEvent struct {
	Subscription map[string]any `json:"subscription"`
	Text         string         `json:"text"`
	Status       string         `json:"status"`
}

// Enqueue persists a notification snapshot for execution and restart recovery.
func (s *Service) Enqueue(ctx context.Context, key string, subscription map[string]any, text, status string) (jobs.Job, error) {
	queue, ok := s.store.(JobQueue)
	if !ok {
		return jobs.Job{}, errors.New("通知存储未实现持久任务队列")
	}
	if key == "" || subscription == nil || status == "" {
		return jobs.Job{}, errors.New("通知幂等键、订阅和状态不能为空")
	}
	payload, err := json.Marshal(notificationEvent{Subscription: subscription, Text: text, Status: status})
	if err != nil {
		return jobs.Job{}, fmt.Errorf("编码通知任务：%w", err)
	}
	return queue.EnqueueJob(ctx, JobKind, key, payload)
}

// Handler provides a jobs.Handler that reconciles persisted channel results before execution.
func (s *Service) Handler() jobs.Handler { return notificationJobHandler{service: s} }

type notificationJobHandler struct{ service *Service }

func (h notificationJobHandler) event(job jobs.Job) (notificationEvent, error) {
	var event notificationEvent
	if err := json.Unmarshal(job.Payload, &event); err != nil {
		return event, err
	}
	if event.Subscription == nil || event.Status == "" {
		return event, errors.New("通知任务数据不完整")
	}
	return event, nil
}

func (h notificationJobHandler) Reconcile(ctx context.Context, job jobs.Job) (bool, error) {
	event, err := h.event(job)
	if err != nil {
		return false, err
	}
	return h.service.Reconcile(ctx, job.ID, event.Subscription, event.Text, event.Status)
}

func (h notificationJobHandler) Execute(ctx context.Context, job jobs.Job) error {
	event, err := h.event(job)
	if err != nil {
		return err
	}
	return h.service.Send(ctx, job.ID, event.Subscription, event.Text, event.Status)
}
