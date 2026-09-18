package service

// reach_pipeline_alert.go 任务终态告警回调：hook 注入、panic 安全触发与 HTTP webhook 实现。

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
)

type ReachAlertHook func(ctx context.Context, job *model.ReachJob, finalState string, reason string)

func (s *ReachPipelineService) SetAlertHook(h ReachAlertHook) {
	s.alertHook = h
}

func (s *ReachPipelineService) fireAlert(ctx context.Context, job *model.ReachJob, finalState, reason string) {
	if s.alertHook == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("[reach_alert] 告警回调 panic（已忽略）: %v", r)
		}
	}()
	s.alertHook(ctx, job, finalState, reason)
}

func NewHTTPAlertHook(webhookURL string) ReachAlertHook {
	if webhookURL == "" {
		return nil
	}
	return func(ctx context.Context, job *model.ReachJob, finalState string, reason string) {
		payload, err := json.Marshal(map[string]interface{}{
			"job_id":      job.ID,
			"channel":     job.Channel,
			"account_id":  job.AccountID,
			"customer_id": job.CustomerID,
			"final_state": finalState,
			"reason":      reason,
			"ts":          time.Now().Unix(),
		})
		if err != nil {
			logger.Errorf("[reach_alert] 序列化告警负载失败: %v", err)
			return
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(payload))
		if err != nil {
			logger.Errorf("[reach_alert] 构造告警请求失败: %v", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if resp != nil {
			defer func() { _ = resp.Body.Close() }()
		}
		if err != nil {
			logger.Errorf("[reach_alert] 发送告警失败: %v", err)
			return
		}
	}
}
