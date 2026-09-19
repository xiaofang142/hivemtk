package service

import (
	"context"
	"time"

	"hivemtk-user/internal/browser_automation/model"
	"hivemtk-user/internal/browser_automation/repository"
)

// SessionService 会话查询 + 手动中断
type SessionService struct {
	sessionRepo repository.BrowserSessionRepository
	stepRepo    repository.BrowserStepRepository
	cmdLogRepo  repository.BrowserCommandLogRepository
	planRepo    repository.BrowserLLMPlanRepository
	executor    *Executor
}

func NewSessionService(sessionRepo repository.BrowserSessionRepository, stepRepo repository.BrowserStepRepository, executor *Executor) *SessionService {
	return &SessionService{sessionRepo: sessionRepo, stepRepo: stepRepo, executor: executor}
}

// SetCommandLogRepository D1：命令流查询仓储注入（装配期一次性，路由未注入时查询返回空不报错）
func (s *SessionService) SetCommandLogRepository(r repository.BrowserCommandLogRepository) {
	s.cmdLogRepo = r
}

// SetLLMPlanRepository I5：LLM 记录仓储注入（导出取 plan/judge/summary 成本账）
func (s *SessionService) SetLLMPlanRepository(r repository.BrowserLLMPlanRepository) {
	s.planRepo = r
}

func (s *SessionService) Get(ctx context.Context, id, userID uint) (*model.BrowserSession, error) {
	return s.sessionRepo.GetByID(ctx, id, userID)
}

func (s *SessionService) ListByUser(ctx context.Context, userID uint, status string, page, limit int) ([]*model.BrowserSession, int64, error) {
	return s.sessionRepo.ListByUser(ctx, userID, status, page, limit)
}

func (s *SessionService) ListByTask(ctx context.Context, taskID, userID uint, page, limit int) ([]*model.BrowserSession, int64, error) {
	return s.sessionRepo.ListByTaskID(ctx, taskID, userID, page, limit)
}

func (s *SessionService) ListSteps(ctx context.Context, sessionID, userID uint) ([]*model.BrowserStep, error) {
	// 归属校验：session 必须属于该用户
	if _, err := s.sessionRepo.GetByID(ctx, sessionID, userID); err != nil {
		return nil, err
	}
	return s.stepRepo.ListBySessionID(ctx, sessionID)
}

// ListCommandLogs D1（G1 补口）：session 归属校验 + append-only 命令流查询（审计链读侧）。
// direction 空=全部；command/event/judge 三类帧按 seq 升序（铁律 4「日志可还原每一步」的兑现）。
func (s *SessionService) ListCommandLogs(ctx context.Context, sessionID, userID uint, direction string) ([]*model.BrowserCommandLog, error) {
	if s.cmdLogRepo == nil {
		return []*model.BrowserCommandLog{}, nil
	}
	if _, err := s.sessionRepo.GetByID(ctx, sessionID, userID); err != nil {
		return nil, err
	}
	all, err := s.cmdLogRepo.ListBySessionID(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if direction == "" {
		return all, nil
	}
	out := make([]*model.BrowserCommandLog, 0, len(all))
	for _, l := range all {
		if l.Direction == direction {
			out = append(out, l)
		}
	}
	return out, nil
}

// ConfirmPending D7 读侧：session 是否正等人工放行（不落库，实时取 Executor 挂起态）
func (s *SessionService) ConfirmPending(sessionID uint) bool {
	return s.executor.ConfirmPending(sessionID)
}

// Stop 手动中断（session 置 stopped 由 Executor 收口；此处仅发信号 + 返回是否命中运行中）
func (s *SessionService) Stop(ctx context.Context, sessionID, userID uint, reason string) (bool, error) {
	sess, err := s.sessionRepo.GetByID(ctx, sessionID, userID)
	if err != nil {
		return false, err
	}
	if sess.Status != "created" && sess.Status != "active" {
		return false, nil
	}
	ok := s.executor.SignalStop(sessionID)
	return ok, nil
}

// Confirm D7：人工放行停在 require_confirm 闸门上的 post_comment 提交点。
// 归属校验与 Stop 同构；返回 false=该 session 当前没有挂起确认点（未开关/已过提交点/已结束）。
func (s *SessionService) Confirm(ctx context.Context, sessionID, userID uint) (bool, error) {
	sess, err := s.sessionRepo.GetByID(ctx, sessionID, userID)
	if err != nil {
		return false, err
	}
	if sess.Status != "created" && sess.Status != "active" {
		return false, nil
	}
	return s.executor.SignalConfirm(sessionID), nil
}

// ---- I5 审计导出（一次请求归并 session 全量审计事实，可离线归档）----

// SessionExportRow LLM 记录导出行（snapshot 大文本不带：G19 后本就可能为空且导出体积敏感；
// 成本账/归因全字段保留，要快照原文走 llm_plans 查询端点或 command_log event 帧）
type SessionExportRow struct {
	ID        uint      `json:"id"`
	Kind      string    `json:"kind"` // plan / judge / summary
	Goal      string    `json:"goal"`
	Steps     []byte    `json:"steps,omitempty"`
	Reasoning string    `json:"reasoning,omitempty"`
	Model     string    `json:"model"`
	TokenIn   int       `json:"token_in"`
	TokenOut  int       `json:"token_out"`
	CreatedAt time.Time `json:"created_at"`
}

// SessionExport I5：session 全量审计包=会话元数据+步流水+命令流+LLM 成本账。
// 归属校验与 ListCommandLogs 同构（GetByID 带 userID）；仓储未装配的分量置空不报错
// （审计导出是增强，不因装配缺位而失败——与 D1 读侧同纪律）。
func (s *SessionService) SessionExport(ctx context.Context, sessionID, userID uint) (*model.BrowserSession, []*model.BrowserStep, []*model.BrowserCommandLog, []*SessionExportRow, error) {
	sess, err := s.sessionRepo.GetByID(ctx, sessionID, userID)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	steps, err := s.stepRepo.ListBySessionID(ctx, sessionID)
	if err != nil {
		steps = nil // 步查询失败不阻断导出（会话元数据仍有价值）
	}
	var logs []*model.BrowserCommandLog
	if s.cmdLogRepo != nil {
		if l, err := s.cmdLogRepo.ListBySessionID(ctx, sessionID); err == nil {
			logs = l
		}
	}
	var plans []*SessionExportRow
	if s.planRepo != nil {
		if ps, err := s.planRepo.ListBySessionID(ctx, sessionID, 200); err == nil {
			for _, p := range ps {
				plans = append(plans, &SessionExportRow{
					ID: p.ID, Kind: p.Kind, Goal: p.Goal, Steps: []byte(p.Steps),
					Reasoning: p.Reasoning, Model: p.Model,
					TokenIn: p.TokenIn, TokenOut: p.TokenOut, CreatedAt: p.CreatedAt,
				})
			}
		}
	}
	return sess, steps, logs, plans, nil
}
