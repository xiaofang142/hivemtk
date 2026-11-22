package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"hivemtk-user/internal/content/model"
	"hivemtk-user/internal/content/repository"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils"
)

var errFlowWaiting = errors.New("flow waiting at durable deadline")
var errFlowOutcomeUnknown = errors.New("flow node outcome unknown: observe before retry")

func flowTriggerKey(ctx context.Context, flow *model.MarketingFlow, triggerID, userID string) (string, error) {
	id, ok := tenant.FromContext(ctx)
	if !ok || id == 0 || tenant.IsSystemScope(ctx) {
		return "", repository.ErrFlowExecutionFence
	}
	if triggerID == "" || len(triggerID) > 160 || userID == "" {
		return "", fmt.Errorf("%w: 缺少稳定事件和收件身份", utils.ErrInvalidInput)
	}
	raw, _ := json.Marshal([]any{id, flow.ID, flow.Version, triggerID, userID})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func (s *MarketingFlowService) executeDurableFlow(ctx context.Context, execution *model.FlowExecution) error {
	var tokenBytes [24]byte
	if _, err := rand.Read(tokenBytes[:]); err != nil {
		return err
	}
	token := hex.EncodeToString(tokenBytes[:])
	row, err := s.executionRepo.ClaimDurable(ctx, execution.ID, token, time.Now().UTC())
	if err != nil {
		return err
	}
	graph, err := parseMarketingFlowGraph(row.GraphSnapshot)
	if err != nil {
		row.Status = "failed"
		row.ErrorMessage = "invalid bound graph"
		return s.executionRepo.FinishDurable(ctx, row, token)
	}
	var input map[string]any
	if err := json.Unmarshal([]byte(row.InputSnapshot), &input); err != nil {
		row.Status = "failed"
		row.ErrorMessage = "invalid bound input"
		return s.executionRepo.FinishDurable(ctx, row, token)
	}
	flow := &model.MarketingFlow{ID: row.FlowID, Version: row.FlowVersion, FlowData: row.GraphSnapshot, Status: model.FlowStatusActive}
	ctx = WithMarketingFlowRef(ctx, flow)
	var nextDue *time.Time
	var dueMu sync.Mutex
	result, runErr := graph.run(ctx, input, func(nodeCtx context.Context, node model.FlowNode, data map[string]any) (map[string]any, error) {
		now := time.Now().UTC()
		var due *time.Time
		if node.Type == "delay" {
			seconds, _ := node.Config["duration"].(float64)
			if seconds < 0 || seconds > float64(maxFlowDelaySeconds()) {
				return nil, fmt.Errorf("%w: 等待时间超出允许范围", utils.ErrInvalidInput)
			}
			at := now.Add(time.Duration(seconds * float64(time.Second)))
			due = &at
		}
		attempt, fresh, err := s.executionRepo.BeginDurableNode(nodeCtx, row.ID, token, node.ID, due, now)
		if err != nil {
			return nil, err
		}
		if attempt.State == "completed" {
			var saved map[string]any
			if err := json.Unmarshal([]byte(attempt.Output), &saved); err != nil {
				return nil, err
			}
			return saved, nil
		}
		if attempt.State == "waiting" {
			if attempt.DueAt == nil {
				return nil, repository.ErrFlowExecutionFence
			}
			if attempt.DueAt.After(now) {
				dueMu.Lock()
				if nextDue == nil || attempt.DueAt.Before(*nextDue) {
					nextDue = attempt.DueAt
				}
				dueMu.Unlock()
				return nil, errFlowWaiting
			}
			if err := s.executionRepo.CompleteDurableNode(nodeCtx, row.ID, token, node.ID, "null", now); err != nil {
				return nil, err
			}
			return nil, nil
		}
		if !fresh {
			return nil, errFlowOutcomeUnknown
		}
		// 每次真实动作携带稳定节点键；进程中断后不会再次调用原节点。
		data["action_intent_id"] = fmt.Sprintf("flow-%d-node-%s", row.ID, node.ID)
		output, err := s.executeNode(nodeCtx, node, row.UserID, data)
		if err != nil {
			return nil, err
		}
		delete(output, "action_intent_id")
		encoded, err := json.Marshal(output)
		if err != nil {
			return nil, err
		}
		if err := s.executionRepo.CompleteDurableNode(nodeCtx, row.ID, token, node.ID, string(encoded), time.Now().UTC()); err != nil {
			return nil, errFlowOutcomeUnknown
		}
		return output, nil
	}, nil)
	row.DueAt = nextDue
	switch {
	case errors.Is(runErr, errFlowWaiting):
		row.Status = "waiting"
		row.ErrorMessage = ""
	case errors.Is(runErr, repository.ErrFlowExecutionFence):
		row.Status = "paused"
		row.ErrorMessage = "current flow permission changed"
	case runErr != nil:
		row.Status = "outcome_unknown"
		row.ErrorMessage = "node requires reconciliation; no automatic replay"
	default:
		row.Status = "completed"
		now := time.Now().UTC()
		row.CompletedAt = &now
		encoded, _ := json.Marshal(result)
		row.ExecutionData = string(encoded)
	}
	return s.executionRepo.FinishDurable(context.WithoutCancel(ctx), row, token)
}

// ResumeDue 只领取本租户当前激活流程的到期运行，不改变绝对等待时刻。
func (s *MarketingFlowService) ResumeDue(ctx context.Context, flowID uint, userID uint, isAdmin bool) (int, error) {
	id, ok := tenant.FromContext(ctx)
	if !ok || id == 0 || tenant.IsSystemScope(ctx) {
		return 0, repository.ErrFlowExecutionFence
	}
	flow, err := s.flowRepo.GetByID(ctx, flowID)
	if err != nil {
		return 0, err
	}
	if flow.TenantID != id {
		return 0, repository.ErrFlowExecutionFence
	}
	if !isAdmin && flow.CreatedBy != userID {
		return 0, utils.ErrForbidden
	}
	rows, err := s.executionRepo.DueDurable(ctx, flowID, time.Now().UTC())
	if err != nil {
		return 0, err
	}
	count := 0
	for i := range rows {
		if err := s.executeDurableFlow(ctx, &rows[i]); err != nil {
			if errors.Is(err, repository.ErrFlowExecutionFence) {
				continue
			}
			return count, err
		}
		count++
	}
	return count, nil
}

// SweepDue 由租户轮转消费者调用；不使用system scope读取租户业务。
func (s *MarketingFlowService) SweepDue(ctx context.Context) (int64, error) {
	ids, err := s.executionRepo.ActiveDueFlows(ctx, time.Now().UTC())
	if err != nil {
		return 0, err
	}
	var count int64
	for _, id := range ids {
		rows, err := s.executionRepo.DueDurable(ctx, id, time.Now().UTC())
		if err != nil {
			return count, err
		}
		for i := range rows {
			if err := s.executeDurableFlow(ctx, &rows[i]); err != nil {
				if errors.Is(err, repository.ErrFlowExecutionFence) {
					continue
				}
				return count, err
			}
			count++
		}
	}
	return count, nil
}
