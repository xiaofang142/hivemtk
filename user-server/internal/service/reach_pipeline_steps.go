package service

// reach_pipeline_steps.go 流水线步骤实现：runStep 步骤分发、内容准备、
// 消息生成、发送结果跟踪上报（aggregateReport）与模板变量渲染。

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
)

func (s *ReachPipelineService) runStep(ctx context.Context, step string, job *model.ReachJob, rl *RateLimitConfig) StepResult {
	start := time.Now()
	res := StepResult{Step: step, StartedAt: start}
	switch step {
	case StepAudience:
		if job.CustomerID == "" {
			res.Success = false
			res.Error = "empty customer_id"
		} else {
			res.Success = true
			res.Output = map[string]any{"customer_id": job.CustomerID}
		}
	case StepContentPrepare:
		content, err := s.prepareContent(ctx, job)
		if err != nil {
			res.Success = false
			res.Error = "content prepare failed: " + err.Error()
		} else {
			res.Success = true
			res.Output = map[string]any{
				"prepared":      true,
				"content":       content,
				"content_bytes": len(content),
			}
		}
	case StepAccountSelect:
		if job.AccountID == "" {
			res.Success = true
			res.Output = map[string]any{"account_id": "auto"}
		} else {
			res.Success = true
			res.Output = map[string]any{"account_id": job.AccountID}
		}
	case StepRateLimit:
		if !s.checkRateLimit(ctx, job.Channel, job.AccountID, job.CustomerID, rl, isTransactionalPayload(job)) {
			res.Success = false
			res.Error = ErrReachRateLimited.Error()
		} else {
			res.Success = true
			res.Output = map[string]any{"pass": true}
		}
	case StepMessageGen:
		message, err := s.generateMessage(ctx, job)
		if err != nil {
			res.Success = false
			res.Error = "message gen failed: " + err.Error()
		} else {
			res.Success = true
			res.Output = map[string]any{
				"generated":     true,
				"message":       message,
				"message_bytes": len(message),
			}
		}
	case StepSend:
		messageID, err := s.dispatchOutbound(ctx, job)
		if err != nil {
			res.Success = false
			res.Error = "send failed: " + err.Error()
		} else {
			res.Success = true
			res.Output = map[string]any{
				"sent":       true,
				"message_id": messageID,
				"channel":    job.Channel,
			}
		}
	case StepTrackResult:
		if err := s.trackSendResult(ctx, job, res); err != nil {
			res.Success = false
			res.Error = "track failed: " + err.Error()
		} else {
			res.Success = true
			res.Output = map[string]any{
				"tracked":     true,
				"job_id":      job.ID,
				"customer_id": job.CustomerID,
				"channel":     job.Channel,
			}
		}
	case StepRetry:
		res.Success = true
		res.Output = map[string]any{"checked": true}
	case StepReport:
		report, err := s.aggregateReport(ctx, job)
		if err != nil {
			res.Success = false
			res.Error = "report failed: " + err.Error()
		} else {
			res.Success = true
			res.Output = report
		}
	default:
		res.Success = false
		res.Error = "unknown step"
	}
	res.EndedAt = time.Now()
	res.DurationMs = int(res.EndedAt.Sub(res.StartedAt).Milliseconds())
	return res
}

func (s *ReachPipelineService) prepareContent(ctx context.Context, job *model.ReachJob) (string, error) {
	if job == nil {
		return "", fmt.Errorf("job is nil")
	}
	raw := ""
	if v, ok := job.Payload["content"]; ok {
		if s, ok := v.(string); ok {
			raw = s
		}
	}
	if raw == "" {
		if v, ok := job.Payload["template_id"]; ok {
			if tmplID, ok := v.(string); ok && tmplID != "" && s.repo != nil && s.repo.Available() {
				tmplContent, err := s.loadTemplateContent(ctx, tmplID)
				if err != nil {
					return "", fmt.Errorf("load template %s: %w", tmplID, err)
				}
				raw = tmplContent
			}
		}
	}
	if raw == "" {
		return "", fmt.Errorf("payload.content and payload.template_id are both empty")
	}
	return renderReachTemplate(raw, job), nil
}

func (s *ReachPipelineService) loadTemplateContent(ctx context.Context, templateID string) (string, error) {
	if s.repo == nil || !s.repo.Available() {
		return "", fmt.Errorf("db is nil")
	}
	return s.repo.GetScriptContent(ctx, templateID)
}

func (s *ReachPipelineService) generateMessage(ctx context.Context, job *model.ReachJob) (string, error) {
	if job == nil {
		return "", fmt.Errorf("job is nil")
	}
	base, err := s.prepareContent(ctx, job)
	if err != nil {
		return "", err
	}
	cleaned := strings.TrimSpace(base)
	cleaned = strings.Join(strings.Fields(cleaned), " ")
	if v, ok := job.Payload["include_channel_footer"]; ok {
		if b, _ := v.(bool); b && job.Channel != "" {
			footer := fmt.Sprintf("\n\n[via %s @ %s]", job.Channel, time.Now().Format("2006-01-02 15:04:05"))
			cleaned += footer
		}
	}
	return cleaned, nil
}

func (s *ReachPipelineService) aggregateReport(ctx context.Context, job *model.ReachJob) (map[string]any, error) {
	if job == nil {
		return nil, fmt.Errorf("job is nil")
	}
	results := []StepResult{}
	if job.StepResults != nil {
		if err := json.Unmarshal(mustJSON(job.StepResults), &results); err != nil {
			return nil, fmt.Errorf("parse step results: %w", err)
		}
	}
	report := map[string]any{
		"job_id":            job.ID,
		"pipeline_id":       job.PipelineID,
		"channel":           job.Channel,
		"customer_id":       job.CustomerID,
		"total_steps":       len(results),
		"success_steps":     0,
		"failed_steps":      0,
		"total_duration_ms": 0,
	}
	success, failed, totalDur, maxStep, maxDur := 0, 0, 0, "", 0
	for _, r := range results {
		if r.Success {
			success++
		} else {
			failed++
		}
		totalDur += r.DurationMs
		if r.DurationMs > maxDur {
			maxDur = r.DurationMs
			maxStep = r.Step
		}
	}
	report["success_steps"] = success
	report["failed_steps"] = failed
	report["total_duration_ms"] = totalDur
	if maxStep != "" {
		report["slowest_step"] = maxStep
		report["slowest_step_ms"] = maxDur
	}
	if v, ok := job.Payload["_tracking"]; ok {
		if m, ok := v.(map[string]any); ok {
			for _, k := range []string{"message_id", "channel", "tracked_at"} {
				if vv, exists := m[k]; exists {
					report["tracking_"+k] = vv
				}
			}
		}
	}
	if s.repo != nil && s.repo.Available() && job.PipelineID > 0 {
		if success > 0 && failed == 0 {
			if err := s.repo.IncrementPipelineField(ctx, job.PipelineID, "total_success", 1); err != nil {
				logger.Warnf("[ReachPipeline] 累计成功数失败 pipelineID=%d: %v", job.PipelineID, err)
			}
		} else if failed > 0 {
			if err := s.repo.IncrementPipelineField(ctx, job.PipelineID, "total_failure", 1); err != nil {
				logger.Warnf("[ReachPipeline] 累计失败数失败 pipelineID=%d: %v", job.PipelineID, err)
			}
		}
	}
	return report, nil
}

func renderReachTemplate(template string, job *model.ReachJob) string {
	if template == "" || job == nil {
		return template
	}
	autoVars := map[string]string{
		"customer_id": job.CustomerID,
		"account_id":  job.AccountID,
		"channel":     job.Channel,
		"date":        time.Now().Format("2006-01-02"),
		"time":        time.Now().Format("15:04:05"),
		"datetime":    time.Now().Format("2006-01-02 15:04:05"),
	}
	var b strings.Builder
	b.Grow(len(template))
	i := 0
	for i < len(template) {
		if i+1 < len(template) && template[i] == '{' && template[i+1] == '{' {
			j := i + 2
			for j+1 < len(template) && !(template[j] == '}' && template[j+1] == '}') {
				j++
			}
			if j+1 < len(template) {
				key := strings.TrimSpace(template[i+2 : j])
				if v, ok := job.Payload[key]; ok {
					b.WriteString(fmt.Sprintf("%v", v))
				} else if v, ok := autoVars[key]; ok {
					b.WriteString(v)
				} else {
					b.WriteString(template[i : j+2])
				}
				i = j + 2
				continue
			}
			b.WriteString(template[i:])
			break
		}
		b.WriteByte(template[i])
		i++
	}
	return b.String()
}
