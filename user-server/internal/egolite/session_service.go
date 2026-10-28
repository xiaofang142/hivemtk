package egolite

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"hivemtk-user/internal/browsercore"
	"hivemtk-user/internal/model"

	"gorm.io/datatypes"
)

// EgoSessionService 通用浏览器任务的编排面（browsercore.Executor 的 ego 实现）。
//
// 职责边界（设计文档 §6.1）：
//   - 这里只做编排：策略裁决、轮次记账、状态聚合、下发、审计；
//   - 动作执行全部在 runner + ego-browser（TaskSpace）里，服务端不解释页面；
//   - 每轮创建一个 egolite_executions 行复用既有状态机/看门狗/确认闸，
//     session 只做「多轮目标」的聚合视图——不另造第二套事实源。
type EgoSessionService struct {
	sessions EgoSessionRepository
	rounds   EgoRoundRepository
	execRepo EgoExecutionRepository
	evRepo   EgoEventRepository
	registry *EgoRunnerRegistry
	base     EventSink // 既有执行域（非 round 事件原样委托，保证 execution 状态机不断链）
}

// NewEgoSessionService 装配
func NewEgoSessionService(
	sessions EgoSessionRepository,
	rounds EgoRoundRepository,
	execRepo EgoExecutionRepository,
	evRepo EgoEventRepository,
	registry *EgoRunnerRegistry,
	base EventSink,
) *EgoSessionService {
	return &EgoSessionService{
		sessions: sessions, rounds: rounds, execRepo: execRepo,
		evRepo: evRepo, registry: registry, base: base,
	}
}

// Engine 实现 browsercore.Executor
func (s *EgoSessionService) Engine() string { return browsercore.EngineEgo }

// Open 创建（或同名续跑）一个通用会话。
func (s *EgoSessionService) Open(ctx context.Context, req browsercore.OpenRequest) (*browsercore.SessionInfo, error) {
	browsercore.NormalizeOpen(&req)
	if req.UserID == 0 {
		return nil, fmt.Errorf("user_id 必填")
	}
	if req.Mode != browsercore.ModeOps {
		return nil, fmt.Errorf("session 模式暂只支持 ops；预置脚本请走 /api/egolite/executions（flow/asset）")
	}
	if req.Engine == browsercore.EngineNM {
		return nil, fmt.Errorf("%w: nm 腿通用会话尚未接入（旧入口 browser_task 仍可用）", browsercore.ErrEngineUnavailable)
	}
	if err := req.Policy.Validate(); err != nil {
		return nil, err
	}
	if err := s.registry.EnsureOnline(req.UserID); err != nil {
		return nil, err
	}

	// 同名 space = 续跑（ego TaskSpace 语义）；终态会话允许重新开一条同名（空间已收口）
	if old, err := s.sessions.GetBySpaceName(ctx, req.UserID, req.SpaceName); err == nil {
		if !browsercore.TerminalStates[old.State] {
			return s.toInfo(old), nil
		}
	}
	// 并发闸：与执行域同口径（非终态 execution 全计），避免"开了 20 个空闲会话"
	active, err := s.execRepo.CountActiveByUser(ctx, req.UserID)
	if err != nil {
		return nil, err
	}
	if active >= 2 {
		return nil, browsercore.ErrGate
	}

	polJSON, _ := json.Marshal(req.Policy)
	sess := &model.EgoliteSession{
		UserID: req.UserID, Goal: req.Goal, Engine: req.Engine, Mode: req.Mode,
		SpaceName: req.SpaceName, PolicyJSON: datatypes.JSON(polJSON),
		Round: 0, State: browsercore.SessionCreated, CreatedFrom: req.CreatedFrom,
	}
	if err := s.sessions.Create(ctx, sess); err != nil {
		// 唯一键冲突（并发同名）→ 回读，实现幂等
		if old, e2 := s.sessions.GetBySpaceName(ctx, req.UserID, req.SpaceName); e2 == nil {
			return s.toInfo(old), nil
		}
		return nil, err
	}
	return s.toInfo(sess), nil
}

// Act 提交一轮 ops（异步：返回已派发轮次；观测用轮次账本/Observe）。
func (s *EgoSessionService) Act(ctx context.Context, req browsercore.RoundRequest) (*browsercore.ActResult, error) {
	sess, err := s.sessions.GetForUser(ctx, req.SessionID, req.UserID)
	if err != nil {
		return nil, err
	}
	switch sess.State {
	case browsercore.SessionCreated, browsercore.SessionActive, browsercore.SessionHandedOff:
	case browsercore.SessionWaitingConfirm:
		return nil, fmt.Errorf("%w: 会话正在等待写确认，请先 allow/deny", browsercore.ErrStateConflict)
	default:
		return nil, fmt.Errorf("%w: 会话已结束（%s）", browsercore.ErrStateConflict, sess.State)
	}

	pol := policyFromJSON(sess.PolicyJSON)
	if sess.Round >= pol.MaxRounds {
		return nil, fmt.Errorf("%w: 已达轮次上限 %d", browsercore.ErrPolicyViolation, pol.MaxRounds)
	}
	if err := browsercore.ValidateOps(req.Ops, pol.MaxActions); err != nil {
		return nil, err
	}
	for _, op := range req.Ops {
		if op.Op == browsercore.OpGoto && !browsercore.DomainAllowed(pol.Domains, op.URL) {
			return nil, fmt.Errorf("%w: goto 越域 %s（声明 domains=%v）", browsercore.ErrPolicyViolation, op.URL, pol.Domains)
		}
	}
	// 写动作 ⇒ 强制逐步确认（客户端不可关闭，与资产 writes 同口径）
	hasWrite := false
	for _, op := range req.Ops {
		if browsercore.IsWriteOp(op.Op) {
			hasWrite = true
			break
		}
	}
	if hasWrite && pol.WriteConfirm != "require" {
		pol.WriteConfirm = "require"
		polJSON, _ := json.Marshal(pol)
		_ = s.sessions.Update(ctx, sess.ID, map[string]any{"policy": datatypes.JSON(polJSON)})
	}
	req.Observe.Normalize()
	roundNo := sess.Round + 1

	exec := &model.EgoliteExecution{
		UserID: req.UserID, Origin: "session", SpaceName: sess.SpaceName,
		Round: roundNo, ConfirmPolicy: pol.WriteConfirm,
		ConfirmTTLSec: pol.ConfirmTTLSec, TimeoutSec: pol.TimeoutSec,
		State: model.EgoExecCreated,
	}
	if err := s.execRepo.Create(ctx, exec); err != nil {
		return nil, err
	}

	reqJSON, _ := json.Marshal(map[string]any{"ops": req.Ops, "observe": req.Observe})
	rec := &model.EgoliteRound{
		SessionID: sess.ID, ExecutionID: exec.ID, UserID: req.UserID, RoundNo: roundNo,
		RequestJSON: datatypes.JSON(reqJSON),
	}
	if _, err := s.rounds.CreateOrIgnore(ctx, rec); err != nil {
		return nil, err
	}

	conn, online := s.registry.GetConn(req.UserID)
	if !online {
		_ = s.rounds.UpdateResult(ctx, sess.ID, roundNo, map[string]any{
			"ok": false, "error_code": "RUNNER_OFFLINE", "error_msg": "runner 离线",
		})
		return nil, ErrEgoRunnerOffline
	}
	if err := s.execRepo.TransitionState(ctx, exec.ID, []string{model.EgoExecCreated}, model.EgoExecDispatching,
		map[string]any{"device_id": conn.DeviceID, "dispatched_at": time.Now()}); err != nil {
		return nil, err
	}

	spec := map[string]any{
		"execution_id":    EgoExecKey(exec.ID),
		"session_id":      fmt.Sprintf("%d", sess.ID),
		"round":           roundNo,
		"space_name":      sess.SpaceName,
		"space_id":        sess.SpaceID,
		"ops":             req.Ops,
		"observe":         req.Observe,
		"confirm_policy":  pol.WriteConfirm,
		"confirm_ttl_sec": pol.ConfirmTTLSec,
		"timeout_sec":     pol.TimeoutSec,
		"domains":         pol.Domains,
		"origin":          "session",
		"mode":            "ops",
	}
	// 会话先推进再下发：runner 可能在本函数返回前就回 round_result/confirm，
	// 若先发后写，终帧会因 GetByExecutionID 查不到而丢失（真机高概率触发）。
	_ = s.sessions.TransitionState(ctx, sess.ID,
		[]string{browsercore.SessionCreated, browsercore.SessionActive, browsercore.SessionHandedOff},
		browsercore.SessionActive,
		map[string]any{"round": roundNo, "execution_id": exec.ID})

	if err := conn.SendSpec(map[string]any{"type": "round_spec", "spec": spec}); err != nil {
		_ = s.execRepo.TransitionState(ctx, exec.ID, []string{model.EgoExecDispatching}, model.EgoExecCreated,
			map[string]any{"fail_reason": "下发失败: " + err.Error()})
		return nil, fmt.Errorf("下发失败: %w", err)
	}

	updated, err := s.sessions.GetForUser(ctx, sess.ID, req.UserID)
	if err != nil {
		return nil, err
	}
	return &browsercore.ActResult{Session: *s.toInfo(updated), Round: browsercore.RoundInfo{
		Round: roundNo, ExecutionID: exec.ID, Usage: browsercore.Usage{},
	}}, nil
}

// Observe 取轮次观测（round=0 取最新）
func (s *EgoSessionService) Observe(ctx context.Context, req browsercore.ObserveRequest) (*browsercore.RoundInfo, error) {
	sess, err := s.sessions.GetForUser(ctx, req.SessionID, req.UserID)
	if err != nil {
		return nil, err
	}
	roundNo := req.Round
	if roundNo <= 0 {
		roundNo = sess.Round
	}
	if roundNo <= 0 {
		return nil, fmt.Errorf("%w: 还没有任何轮次", browsercore.ErrNotFound)
	}
	rec, err := s.rounds.GetByRound(ctx, sess.ID, roundNo)
	if err != nil {
		return nil, err
	}
	return roundToInfo(rec), nil
}

// Confirm 写确认（复用执行行状态机；allow/deny 都落到 runner 裁决文件）
func (s *EgoSessionService) Confirm(ctx context.Context, userID, sessionID uint, allow bool) (*browsercore.SessionInfo, error) {
	sess, err := s.sessions.GetForUser(ctx, sessionID, userID)
	if err != nil {
		return nil, err
	}
	if sess.State != browsercore.SessionWaitingConfirm || sess.ExecutionID == 0 {
		return nil, fmt.Errorf("%w: 当前不在等待确认状态", browsercore.ErrStateConflict)
	}
	if allow {
		if err := s.sendFrame(userID, map[string]any{"type": "confirm", "spec": map[string]any{
			"execution_id": EgoExecKey(sess.ExecutionID), "decision": "allow"}}); err != nil {
			return nil, err
		}
		_ = s.execRepo.TransitionState(ctx, sess.ExecutionID,
			[]string{model.EgoExecWaitingConfirm}, model.EgoExecRunning, nil)
		_ = s.sessions.TransitionState(ctx, sess.ID, []string{browsercore.SessionWaitingConfirm}, browsercore.SessionActive, nil)
	} else {
		_ = s.sendFrame(userID, map[string]any{"type": "confirm", "spec": map[string]any{
			"execution_id": EgoExecKey(sess.ExecutionID), "decision": "deny"}})
		_ = s.execRepo.TransitionState(ctx, sess.ExecutionID,
			[]string{model.EgoExecWaitingConfirm}, model.EgoExecStopped,
			map[string]any{"ended_at": time.Now(), "fail_reason": "用户拒绝确认"})
		_ = s.sessions.TransitionState(ctx, sess.ID, []string{browsercore.SessionWaitingConfirm},
			browsercore.SessionStopped, map[string]any{"ended_at": time.Now(), "fail_reason": "用户拒绝确认"})
	}
	updated, err := s.sessions.GetForUser(ctx, sessionID, userID)
	if err != nil {
		return nil, err
	}
	return s.toInfo(updated), nil
}

// Abort 停止会话（在途轮次进程一并 kill）
func (s *EgoSessionService) Abort(ctx context.Context, userID, sessionID uint, reason string) (*browsercore.SessionInfo, error) {
	sess, err := s.sessions.GetForUser(ctx, sessionID, userID)
	if err != nil {
		return nil, err
	}
	if browsercore.TerminalStates[sess.State] {
		return nil, fmt.Errorf("%w: 会话已结束", browsercore.ErrStateConflict)
	}
	if sess.ExecutionID > 0 {
		_ = s.sendFrame(userID, map[string]any{"type": "abort", "spec": map[string]any{
			"execution_id": EgoExecKey(sess.ExecutionID)}})
		_ = s.execRepo.TransitionState(ctx, sess.ExecutionID, []string{
			model.EgoExecCreated, model.EgoExecDispatching, model.EgoExecRunning, model.EgoExecWaitingConfirm,
		}, model.EgoExecStopped, map[string]any{"ended_at": time.Now(), "fail_reason": reason})
	}
	if reason == "" {
		reason = "用户停止"
	}
	_ = s.sessions.TransitionState(ctx, sess.ID, []string{
		browsercore.SessionCreated, browsercore.SessionActive, browsercore.SessionWaitingConfirm, browsercore.SessionHandedOff,
	}, browsercore.SessionStopped, map[string]any{"ended_at": time.Now(), "fail_reason": reason})
	updated, err := s.sessions.GetForUser(ctx, sessionID, userID)
	if err != nil {
		return nil, err
	}
	return s.toInfo(updated), nil
}

// Finish 收尾（幂等）：runner 侧 task.finish({keep}) 保留用户需要的页面。
func (s *EgoSessionService) Finish(ctx context.Context, userID, sessionID uint, keep []string) (*browsercore.SessionInfo, error) {
	sess, err := s.sessions.GetForUser(ctx, sessionID, userID)
	if err != nil {
		return nil, err
	}
	if browsercore.TerminalStates[sess.State] {
		return s.toInfo(sess), nil
	}
	_ = s.sendFrame(userID, map[string]any{"type": "finish_spec", "spec": map[string]any{
		"execution_id": EgoExecKey(sess.ExecutionID), "space_name": sess.SpaceName, "keep": keep}})
	_ = s.sessions.TransitionState(ctx, sess.ID, []string{
		browsercore.SessionCreated, browsercore.SessionActive, browsercore.SessionWaitingConfirm, browsercore.SessionHandedOff,
	}, browsercore.SessionFinished, map[string]any{"ended_at": time.Now()})
	updated, err := s.sessions.GetForUser(ctx, sessionID, userID)
	if err != nil {
		return nil, err
	}
	return s.toInfo(updated), nil
}

// Get 会话详情
func (s *EgoSessionService) Get(ctx context.Context, userID, sessionID uint) (*browsercore.SessionInfo, error) {
	sess, err := s.sessions.GetForUser(ctx, sessionID, userID)
	if err != nil {
		return nil, err
	}
	return s.toInfo(sess), nil
}

// List 会话列表
func (s *EgoSessionService) List(ctx context.Context, userID uint, page, pageSize int) ([]*browsercore.SessionInfo, int64, error) {
	list, total, err := s.sessions.ListByUser(ctx, userID, page, pageSize)
	if err != nil {
		return nil, 0, err
	}
	out := make([]*browsercore.SessionInfo, 0, len(list))
	for _, it := range list {
		out = append(out, s.toInfo(it))
	}
	return out, total, nil
}

// Rounds 轮次账本（时间线）
func (s *EgoSessionService) Rounds(ctx context.Context, userID, sessionID uint, limit int) ([]*browsercore.RoundInfo, error) {
	if _, err := s.sessions.GetForUser(ctx, sessionID, userID); err != nil {
		return nil, err
	}
	list, err := s.rounds.ListBySession(ctx, sessionID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]*browsercore.RoundInfo, 0, len(list))
	for _, rec := range list {
		out = append(out, roundToInfo(rec))
	}
	return out, nil
}

// IngestEvent 事件收口（AttachEventSink 指向本服务）：先委托执行域，再做会话聚合。
func (s *EgoSessionService) IngestEvent(ctx context.Context, userID uint, ev RunnerEvent) error {
	if s.base != nil {
		if err := s.base.IngestEvent(ctx, userID, ev); err != nil {
			return err
		}
	}
	if ev.ExecutionID == "" {
		return nil
	}
	execID, ok := parseExecKey(ev.ExecutionID)
	if !ok {
		return nil
	}
	sess, err := s.sessions.GetByExecutionID(ctx, userID, execID)
	if err != nil {
		return nil // 不是会话执行（flow/asset 的普通事件）
	}
	switch ev.Type {
	case "confirm":
		_ = s.sessions.TransitionState(ctx, sess.ID,
			[]string{browsercore.SessionActive, browsercore.SessionCreated},
			browsercore.SessionWaitingConfirm, nil)
	case "handoff":
		_ = s.sessions.TransitionState(ctx, sess.ID,
			[]string{browsercore.SessionActive, browsercore.SessionWaitingConfirm},
			browsercore.SessionHandedOff, nil)
	case "round_result":
		s.applyRoundResult(ctx, sess, ev)
	case "done":
		if ev.ExitCode != 0 {
			reason := ev.Stderr
			if reason == "" {
				reason = "轮次退出码非 0"
			}
			_ = s.sessions.TransitionState(context.WithoutCancel(ctx), sess.ID,
				[]string{browsercore.SessionCreated, browsercore.SessionActive, browsercore.SessionWaitingConfirm, browsercore.SessionHandedOff},
				browsercore.SessionFailed, map[string]any{"ended_at": time.Now(), "fail_reason": truncateStr(reason, 500)})
		}
	}
	return nil
}

// applyRoundResult 终帧落轮次账本 + 推进会话状态。
func (s *EgoSessionService) applyRoundResult(ctx context.Context, sess *model.EgoliteSession, ev RunnerEvent) {
	var out roundOutcome
	if err := json.Unmarshal(ev.Payload, &out); err != nil {
		return
	}
	roundNo := sess.Round
	if out.Round > 0 {
		roundNo = out.Round
	}
	if roundNo <= 0 {
		return
	}
	obs := &browsercore.Observation{
		URL: out.URL, Title: out.Title, Viewport: out.Viewport,
		Targets: out.Targets, Text: out.Text, TextTruncated: out.TextTruncated,
		SnapshotHandle: out.SnapshotHandle, Extract: out.Extract, Artifacts: out.Artifacts,
	}
	pol := policyFromJSON(sess.PolicyJSON)
	obs.Sanitize(browsercore.DefaultMaxTargets, 1200, pol.RedactPatterns)
	obsJSON, _ := json.Marshal(obs)
	resJSON, _ := json.Marshal(out)

	sets := map[string]any{
		"result":      datatypes.JSON(resJSON),
		"observation": datatypes.JSON(obsJSON),
		"ok":          out.OK,
		"duration_ms": out.Usage.MS,
		"actions":     out.Usage.Actions,
	}
	if out.Error != nil {
		sets["error_code"] = truncateStr(out.Error.Code, 64)
		sets["error_msg"] = truncateStr(out.Error.Msg, 500)
	}
	_ = s.rounds.UpdateResult(ctx, sess.ID, roundNo, sets)

	now := time.Now()
	next := browsercore.SessionActive
	reason := ""
	if out.Handoff {
		next = browsercore.SessionHandedOff
	} else if out.Fatal {
		next = browsercore.SessionFailed
		if out.Error != nil {
			reason = out.Error.Msg
		}
	}
	sets2 := map[string]any{"last_observed_at": &now}
	if out.SpaceID != "" {
		sets2["space_id"] = out.SpaceID
	}
	if reason != "" {
		sets2["fail_reason"] = truncateStr(reason, 500)
	}
	if next != browsercore.SessionActive {
		sets2["ended_at"] = &now
	}
	_ = s.sessions.TransitionState(ctx, sess.ID,
		[]string{browsercore.SessionCreated, browsercore.SessionActive, browsercore.SessionWaitingConfirm, browsercore.SessionHandedOff},
		next, sets2)
}

// WatchdogSweep 会话侧收口：execution 已终态但 session 还挂着 → 同步状态。
// （confirm TTL / 硬超时本体仍由执行域看门狗负责，这里只做视图收敛。）
func (s *EgoSessionService) WatchdogSweep(ctx context.Context) (int, error) {
	list, err := s.sessions.ListNonTerminal(ctx)
	if err != nil {
		return 0, err
	}
	swept := 0
	for _, sess := range list {
		if sess.ExecutionID == 0 {
			continue
		}
		e, err := s.execRepo.GetByID(ctx, sess.ExecutionID)
		if err != nil || !model.EgoExecTerminalStates[e.State] {
			continue
		}
		next := ""
		reason := e.FailReason
		switch e.State {
		case model.EgoExecStopped:
			next = browsercore.SessionStopped
		case model.EgoExecFailed:
			next = browsercore.SessionFailed
		case model.EgoExecTimeout:
			next = browsercore.SessionTimeout
		case model.EgoExecCompleted:
			// 本轮正常结束 ≠ 会话结束（多轮任务）；保持 active
			continue
		}
		if next == "" {
			continue
		}
		if err := s.sessions.TransitionState(ctx, sess.ID,
			[]string{browsercore.SessionCreated, browsercore.SessionActive, browsercore.SessionWaitingConfirm, browsercore.SessionHandedOff},
			next, map[string]any{"ended_at": time.Now(), "fail_reason": truncateStr(reason, 500)}); err == nil {
			swept++
		}
	}
	return swept, nil
}

// OnRunnerDisconnect 断连收口：把该用户非终态会话判 failed（执行域已先判 execution failed）。
func (s *EgoSessionService) OnRunnerDisconnect(ctx context.Context, userID uint) {
	dbCtx := context.WithoutCancel(ctx)
	list, _, err := s.sessions.ListByUser(dbCtx, userID, 1, 100)
	if err != nil {
		return
	}
	for _, sess := range list {
		if browsercore.TerminalStates[sess.State] {
			continue
		}
		_ = s.sessions.TransitionState(dbCtx, sess.ID,
			[]string{browsercore.SessionCreated, browsercore.SessionActive, browsercore.SessionWaitingConfirm, browsercore.SessionHandedOff},
			browsercore.SessionFailed, map[string]any{"ended_at": time.Now(), "fail_reason": "runner 断连"})
	}
}

// ---------- 内部 ----------

func (s *EgoSessionService) sendFrame(userID uint, frame map[string]any) error {
	conn, ok := s.registry.GetConn(userID)
	if !ok {
		return ErrEgoRunnerOffline
	}
	return conn.SendSpec(frame)
}

func (s *EgoSessionService) toInfo(sess *model.EgoliteSession) *browsercore.SessionInfo {
	return &browsercore.SessionInfo{
		ID: sess.ID, UserID: sess.UserID, Goal: sess.Goal, Engine: sess.Engine, Mode: sess.Mode,
		SpaceName: sess.SpaceName, SpaceID: sess.SpaceID, ExecutionID: sess.ExecutionID,
		State: sess.State, Round: sess.Round, Policy: policyFromJSON(sess.PolicyJSON),
		CreatedFrom: sess.CreatedFrom, FailReason: sess.FailReason,
		CreatedAt: sess.CreatedAt, UpdatedAt: sess.UpdatedAt,
	}
}

func policyFromJSON(raw []byte) browsercore.Policy {
	var p browsercore.Policy
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &p)
	}
	p.Normalize()
	return p
}

func parseExecKey(key string) (uint, bool) {
	var id uint
	if _, err := fmt.Sscanf(key, "ego-%d", &id); err != nil || id == 0 {
		return 0, false
	}
	return id, true
}

type roundOutcome struct {
	OK             *bool                  `json:"ok"`
	Round          int                    `json:"round"`
	URL            string                 `json:"url"`
	Title          string                 `json:"title"`
	SpaceID        string                 `json:"space_id"`
	Viewport       browsercore.Viewport   `json:"viewport"`
	Targets        []browsercore.Target   `json:"targets"`
	Text           string                 `json:"text"`
	TextTruncated  bool                   `json:"text_truncated"`
	SnapshotHandle string                 `json:"snapshot_handle"`
	Extract        any                    `json:"extract"`
	Artifacts      []browsercore.Artifact `json:"artifacts"`
	Usage          browsercore.Usage      `json:"usage"`
	Error          *browsercore.ExecError `json:"error"`
	Fatal          bool                   `json:"fatal"`
	Handoff        bool                   `json:"handoff"`
}

func roundToInfo(rec *model.EgoliteRound) *browsercore.RoundInfo {
	info := &browsercore.RoundInfo{
		Round: rec.RoundNo, ExecutionID: rec.ExecutionID, OK: rec.Ok,
		Error:     &browsercore.ExecError{Code: rec.ErrorCode, Msg: rec.ErrorMsg},
		Usage:     browsercore.Usage{MS: rec.DurationMs, Actions: rec.Actions},
		CreatedAt: rec.CreatedAt,
	}
	if rec.ErrorCode == "" && rec.ErrorMsg == "" {
		info.Error = nil
	}
	var out roundOutcome
	if len(rec.ResultJSON) > 0 {
		_ = json.Unmarshal(rec.ResultJSON, &out)
		info.URL = out.URL
		info.Title = out.Title
		info.Extract = out.Extract
	}
	var obs browsercore.Observation
	if len(rec.ObservationJSON) > 0 {
		_ = json.Unmarshal(rec.ObservationJSON, &obs)
		info.Targets = obs.Targets
		info.Text = obs.Text
		info.Artifacts = obs.Artifacts
		if info.URL == "" {
			info.URL = obs.URL
		}
		if info.Title == "" {
			info.Title = obs.Title
		}
	}
	return info
}

var _ browsercore.Executor = (*EgoSessionService)(nil)
