package egolite

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"hivemtk-user/internal/model"

	"gorm.io/datatypes"
)

// EgoExecutionService 执行域服务（设计文档 DF-4/DF-5 状态机；服务端是唯一事实源）。
//
// 状态机（research doc §6.1）：created → dispatching → running ⇄ waiting_confirm → 终态
// 纪律：
//   - 状态迁移全部走 repo.TransitionState 单语句原子迁移（乐观并发，禁读-判-写）；
//   - 并发闸在 dispatch 前查（CountActiveByUser），中间态同样占闸；
//   - 终态写走 context.WithoutCancel（bridge/account_repo.go 先例：WS 生命周期
//     与审计链解绑——执行挂掉不能带走终态落库）；
//   - runner 断连收口在 hooks（FailActiveByUser），不共享 host-ws 的 browser_sessions 语义。

// EgoExecutionService 执行域
type EgoExecutionService struct {
	execRepo  EgoExecutionRepository
	evRepo    EgoEventRepository
	flowRepo  EgoFlowRepository
	assetRepo EgoAssetRepository // market 通道的资产校验源（nil ⇒ market 全拒，fail-closed）
	registry  *EgoRunnerRegistry
}

func NewEgoExecutionService(execRepo EgoExecutionRepository, evRepo EgoEventRepository, flowRepo EgoFlowRepository, assetRepo EgoAssetRepository, registry *EgoRunnerRegistry) *EgoExecutionService {
	return &EgoExecutionService{execRepo: execRepo, evRepo: evRepo, flowRepo: flowRepo, assetRepo: assetRepo, registry: registry}
}

// RunnerEvent runner 上行事件帧（A3 协议服务端侧形状；cmd/ego-runner 以别名共用，
// 双端同源——事件帧字段漂移在编译期暴露）
type RunnerEvent struct {
	Seq         int             `json:"seq"`
	Type        string          `json:"type"`
	ExecutionID string          `json:"execution_id,omitempty"`
	Payload     json.RawMessage `json:"payload,omitempty"`
	Stderr      string          `json:"stderr,omitempty"`
	ExitCode    int             `json:"exit_code,omitempty"`
}

// EgoExecKey 执行的 runner 侧键（ego-<rowID>；服务端是唯一拼装点，
// runner 透传不重组——findByExecKey 前缀解析 + 归属校验是唯一判定路径）
func EgoExecKey(rowID uint) string { return fmt.Sprintf("ego-%d", rowID) }

// StartRequest 下发请求体（routes 层 JSON 绑定）
type StartRequest struct {
	Origin        string          `json:"origin"`          // draft / market
	FlowID        uint            `json:"flow_id"`         // draft 时必填
	AssetID       string          `json:"asset_id"`        // market 时必填
	AssetVersion  string          `json:"asset_version"`
	Entry         string          `json:"entry"` // market 时必填（资产内入口）
	Params        json.RawMessage `json:"params"`
	SpaceName     string          `json:"space_name"`
	ConfirmPolicy string          `json:"confirm_policy"` // none / require
	ConfirmTTLSec int             `json:"confirm_ttl_sec"`
	TimeoutSec    int             `json:"timeout_sec"`
}

// 限额（task.go 同纪律：服务端限额不给客户端协商空间）
const (
	egoExecMaxTimeoutSec = 3600
	egoExecDefTimeoutSec = 1800
	egoExecDefConfirmTTL = 300
	egoExecMaxActive     = 2 // 每用户同时在途上限（M1 从严）
)

// Start 创建 + 下发（状态机起点）。
// 返回 (execution, dispatched)。dispatched=false 表示创建了但 runner 不在线
// （执行留在 created 态，等 runner 上线后由用户重发——M1 不做自动补投）。
func (s *EgoExecutionService) Start(ctx context.Context, userID uint, req StartRequest) (*model.EgoliteExecution, error) {
	if req.Origin == "" {
		req.Origin = "draft"
	}
	if req.Origin != "draft" && req.Origin != "market" {
		return nil, fmt.Errorf("origin 必须为 draft 或 market")
	}
	if req.Origin == "draft" && req.FlowID == 0 {
		return nil, fmt.Errorf("draft 执行必须提供 flow_id")
	}

	// market 通道：资产必须来自本实例已上架的目录（published + scan_passed）。
	// 这是「平台端发布 → 用户端使用 → 下发执行」闭环的唯一入口，也是防越权的唯一判定点：
	// asset_id 由客户端给，但**能不能跑由服务端说了算**。
	var asset *model.EgoliteAsset
	if req.Origin == "market" {
		if req.AssetID == "" {
			return nil, fmt.Errorf("market 执行必须提供 asset_id")
		}
		if s.assetRepo == nil {
			return nil, ErrAssetUnavailable
		}
		a, err := s.assetRepo.GetByCode(ctx, req.AssetID)
		if err != nil {
			return nil, ErrAssetNotFound
		}
		if a.Status != model.EgoAssetPublished || !a.ScanPassed || a.Script == "" {
			return nil, ErrAssetNotFound
		}
		asset = a
		if req.Entry == "" {
			req.Entry = a.Entry // 缺省用资产自身入口（客户端不必知道路径）
		}
		if req.AssetVersion == "" {
			req.AssetVersion = fmt.Sprintf("%d", a.Version)
		}
		// 资产声明了写动作 ⇒ 强制步级确认（客户端不可自行关闭，决策 4）
		if len(assetWriteNames(a)) > 0 {
			req.ConfirmPolicy = "require"
		}
	}
	if req.Origin == "market" && req.Entry == "" {
		return nil, fmt.Errorf("market 执行必须提供 entry（资产未声明入口）")
	}
	if req.TimeoutSec <= 0 {
		req.TimeoutSec = egoExecDefTimeoutSec
	}
	if req.TimeoutSec > egoExecMaxTimeoutSec {
		req.TimeoutSec = egoExecMaxTimeoutSec
	}
	if req.ConfirmPolicy == "" {
		req.ConfirmPolicy = "none"
	}
	if req.ConfirmPolicy != "none" && req.ConfirmPolicy != "require" {
		return nil, fmt.Errorf("confirm_policy 必须为 none 或 require")
	}
	if req.ConfirmTTLSec <= 0 {
		req.ConfirmTTLSec = egoExecDefConfirmTTL
	}

	// 并发闸：非终态全计（dispatching/waiting_confirm 同样占闸）
	active, err := s.execRepo.CountActiveByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if active >= egoExecMaxActive {
		return nil, ErrConcurrencyGate
	}

	e := &model.EgoliteExecution{
		UserID:        userID,
		FlowID:        req.FlowID,
		AssetID:       req.AssetID,
		AssetVer:      req.AssetVersion,
		Origin:        req.Origin,
		Entry:         req.Entry,
		ParamsJSON:    datatypes.JSON(req.Params),
		SpaceName:     req.SpaceName,
		Round:         1,
		ConfirmPolicy: req.ConfirmPolicy,
		ConfirmTTLSec: req.ConfirmTTLSec,
		TimeoutSec:    req.TimeoutSec,
		State:         model.EgoExecCreated,
	}
	if err := s.execRepo.Create(ctx, e); err != nil {
		return nil, err
	}
	if asset != nil {
		// 引用计数（弱计数：失败不影响执行主流程——统计口径不该成为执行前置）
		_ = s.assetRepo.IncInstall(ctx, asset.AssetCode)
	}

	dispatched := s.dispatch(ctx, e)
	_ = dispatched
	return e, nil
}

// dispatch 真下发（created → dispatching → running 由事件行推进；失败回 created）。
func (s *EgoExecutionService) dispatch(ctx context.Context, e *model.EgoliteExecution) bool {
	conn, online := s.registry.GetConn(e.UserID)
	if !online {
		return false
	}
	if err := s.execRepo.TransitionState(ctx, e.ID, []string{model.EgoExecCreated}, model.EgoExecDispatching,
		map[string]any{"device_id": conn.DeviceID, "dispatched_at": time.Now()}); err != nil {
		return false
	}
	spec := map[string]any{
		"execution_id":    fmt.Sprintf("ego-%d-%d", e.ID, time.Now().UnixMilli()),
		"asset_id":        e.AssetID,
		"asset_version":   e.AssetVer,
		"entry":           e.Entry,
		"params":          e.ParamsJSON,
		"space_name":      e.SpaceName,
		"confirm_policy":  e.ConfirmPolicy,
		"confirm_ttl_sec": e.ConfirmTTLSec,
		"timeout_sec":     e.TimeoutSec,
		"round":           e.Round,
		"origin":          e.Origin,
	}
	if e.Origin == "draft" {
		// draft 脚本在 dispatch 时刻从 flow 读取并内嵌（单真相源：flow.Script）。
		// 不内嵌的话 runner 端要再查库，违反「runner 无业务决策」纪律。
		flow, err := s.flowRepo.GetForUser(ctx, e.FlowID, e.UserID)
		if err != nil || flow.Script == "" {
			_ = s.execRepo.TransitionState(ctx, e.ID, []string{model.EgoExecDispatching}, model.EgoExecFailed,
				map[string]any{"fail_reason": "flow 不存在或脚本为空", "ended_at": time.Now()})
			return false
		}
		spec["draft_script"] = flow.Script
	}
	if err := conn.SendSpec(map[string]any{"type": "task_spec", "spec": spec}); err != nil {
		// 下发失败回退 created（用户可重发）；fail_reason 记录
		_ = s.execRepo.TransitionState(ctx, e.ID, []string{model.EgoExecDispatching}, model.EgoExecCreated,
			map[string]any{"fail_reason": "下发失败: " + err.Error()})
		return false
	}
	return true
}

// Redispatch created 态执行重发（runner 掉线时 Start 落 created，上线后用户重试）
func (s *EgoExecutionService) Redispatch(ctx context.Context, userID, execID uint) (*model.EgoliteExecution, error) {
	e, err := s.execRepo.GetForUser(ctx, execID, userID)
	if err != nil {
		return nil, err
	}
	if e.State != model.EgoExecCreated {
		return nil, ErrExecStateConflict
	}
	if !s.dispatch(ctx, e) {
		return e, nil
	}
	return s.execRepo.GetByID(ctx, execID)
}

// IngestEvent runner 上行事件行落库 + 推进状态机。
// 这是 A3 协议的服务端收口：runner 只透传，语义判定全部在这里。
func (s *EgoExecutionService) IngestEvent(ctx context.Context, userID uint, ev RunnerEvent) error {
	e, err := s.findByExecKey(ctx, userID, ev.ExecutionID)
	if err != nil {
		return err
	}

	// 事件行落库（Ok 三态继承：payload 里没带结论就存 nil）
	okVal := parseEventOk(ev.Payload)
	rec := &model.EgoEventLog{
		ExecutionID: e.ID,
		Seq:         ev.Seq,
		Type:        normalizeEventType(ev.Type),
		Payload:     datatypes.JSON(ev.Payload),
		Ok:          okVal,
	}
	if err := s.evRepo.Append(ctx, rec); err != nil {
		return err
	}

	// 状态机推进（幂等：终态后事件行只落库不迁移）
	switch ev.Type {
	case "action", "extract", "snapshot", "artifact":
		// 首个动作行把 dispatching 推到 running（幂等：非法迁移静默忽略）
		_ = s.execRepo.TransitionState(ctx, e.ID, []string{model.EgoExecDispatching}, model.EgoExecRunning, nil)
		// waiting_confirm 中收到动作行 = confirm 通过后继续跑（回 running）
		_ = s.execRepo.TransitionState(ctx, e.ID, []string{model.EgoExecWaitingConfirm}, model.EgoExecRunning, nil)
	case "confirm":
		_ = s.execRepo.TransitionState(ctx, e.ID,
			[]string{model.EgoExecRunning, model.EgoExecDispatching}, model.EgoExecWaitingConfirm, nil)
	case "handoff":
		// DF-5：handoff 事件携带下一 round 的 space 信息，执行保持 running（round 推进在 done 帧）
		_ = s.execRepo.TransitionState(ctx, e.ID, []string{model.EgoExecDispatching}, model.EgoExecRunning, nil)
	case "error":
		// 资产级错误不直接终态——等 done 帧（exit_code）判 failed/completed；
		// 但 dispatching 态的 error 说明没跑起来，直接判 failed
		_ = s.execRepo.TransitionState(ctx, e.ID, []string{model.EgoExecDispatching}, model.EgoExecFailed,
			map[string]any{"fail_reason": "执行启动失败", "ended_at": time.Now()})
	case "done":
		s.finishFromDone(ctx, e, ev)
	case "runner":
		// runner 级事件（busy/abort 确认等）只落库
	}
	return nil
}

// finishFromDone done 帧终态判定：exit_code==0 且未超时 → completed，否则 failed
func (s *EgoExecutionService) finishFromDone(ctx context.Context, e *model.EgoliteExecution, ev RunnerEvent) {
	// done 帧需要拿到 exit_code；eventLine.ExitCode 由 runner 侧填。
	// WithoutCancel：终态写必须活过请求/WS 生命周期（bridge 先例）。
	dbCtx := context.WithoutCancel(ctx)
	now := time.Now()
	state := model.EgoExecCompleted
	sets := map[string]any{"ended_at": now, "updated_at": now}
	if ev.ExitCode != 0 {
		state = model.EgoExecFailed
		sets["fail_reason"] = truncateStr(ev.Stderr, 500)
	}
	sets["state"] = state
	// 从任何非终态迁移到终态（created 意外收到 done 也收敛，宁可终态不悬挂）
	_ = s.execRepo.TransitionState(dbCtx, e.ID, []string{
		model.EgoExecCreated, model.EgoExecDispatching, model.EgoExecRunning, model.EgoExecWaitingConfirm,
	}, state, sets)
}

// Abort 用户停止（created/dispatching/running 可停；waiting_confirm 必须先 confirm/deny——
// 拒绝也走 Abort 但语义是「本轮拒绝」）
func (s *EgoExecutionService) Abort(ctx context.Context, userID, execID uint, reason string) (*model.EgoliteExecution, error) {
	e, err := s.execRepo.GetForUser(ctx, execID, userID)
	if err != nil {
		return nil, err
	}
	if model.EgoExecTerminalStates[e.State] {
		return nil, ErrExecStateConflict
	}
	// 先本地判终态（防对已结束执行发 abort 帧），再单语句迁移，成功才发 runner
	if err := s.execRepo.TransitionState(ctx, e.ID, []string{
		model.EgoExecCreated, model.EgoExecDispatching, model.EgoExecRunning, model.EgoExecWaitingConfirm,
	}, model.EgoExecStopped, map[string]any{"ended_at": time.Now(), "fail_reason": reason}); err != nil {
		return nil, ErrExecStateConflict
	}
	if conn, online := s.registry.GetConn(userID); online {
		_ = conn.SendSpec(map[string]any{"type": "abort", "spec": map[string]any{"execution_id": EgoExecKey(e.ID)}})
	}
	return s.execRepo.GetByID(ctx, execID)
}

// Confirm 用户确认写操作（waiting_confirm → running）
func (s *EgoExecutionService) Confirm(ctx context.Context, userID, execID uint, allow bool) (*model.EgoliteExecution, error) {
	e, err := s.execRepo.GetForUser(ctx, execID, userID)
	if err != nil {
		return nil, err
	}
	if e.State != model.EgoExecWaitingConfirm {
		return nil, ErrExecStateConflict
	}
	decision := "allow"
	if !allow {
		decision = "deny"
		// deny → 终态 stopped
		if err := s.execRepo.TransitionState(ctx, e.ID, []string{model.EgoExecWaitingConfirm},
			model.EgoExecStopped, map[string]any{"ended_at": time.Now(), "fail_reason": "用户拒绝确认"}); err != nil {
			return nil, ErrExecStateConflict
		}
	} else {
		// allow → 发 confirm 帧（runner 把它转给资产）+ 回 running
		if conn, online := s.registry.GetConn(userID); online {
			_ = conn.SendSpec(map[string]any{"type": "confirm", "spec": map[string]any{
				"execution_id": EgoExecKey(e.ID), "decision": "allow"}})
		}
		if err := s.execRepo.TransitionState(ctx, e.ID, []string{model.EgoExecWaitingConfirm},
			model.EgoExecRunning, nil); err != nil {
			return nil, ErrExecStateConflict
		}
	}
	_ = decision
	return s.execRepo.GetByID(ctx, execID)
}

// WatchdogSweep 看门狗扫描（后台定时任务调用）：confirm TTL 超时 + 硬超时
func (s *EgoExecutionService) WatchdogSweep(ctx context.Context) (int, error) {
	now := time.Now()
	swept := 0
	overdue, err := s.execRepo.ListConfirmOverdue(ctx, now)
	if err != nil {
		return swept, err
	}
	for _, e := range overdue {
		// confirm TTL 超时 = 拒绝处理（fail-close：不确认不执行）
		if err := s.execRepo.TransitionState(ctx, e.ID, []string{model.EgoExecWaitingConfirm},
			model.EgoExecStopped, map[string]any{"ended_at": now, "fail_reason": "确认超时（confirm TTL 到期，按拒绝处理）"}); err == nil {
			swept++
		}
	}
	timeoutOverdue, err := s.execRepo.ListTimeoutOverdue(ctx, now)
	if err != nil {
		return swept, err
	}
	for _, e := range timeoutOverdue {
		if err := s.execRepo.TransitionState(ctx, e.ID, []string{model.EgoExecRunning, model.EgoExecDispatching},
			model.EgoExecTimeout, map[string]any{"ended_at": now, "fail_reason": "执行硬超时（wall-clock 看门狗）"}); err == nil {
			swept++
		}
	}
	return swept, nil
}

// OnRunnerDisconnect runner 断连收口（registry hook 调用；browser session.go 同纪律：
// 挂死执行会永久占住并发闸，必须判终态）
func (s *EgoExecutionService) OnRunnerDisconnect(ctx context.Context, userID uint) {
	dbCtx := context.WithoutCancel(ctx)
	_, _ = s.execRepo.FailActiveByUser(dbCtx, userID, "runner 断连，在途执行判失败（可重试）")
}

// findByExecKey runner 的 execution_id 是 string（ego-<id>-<ms>），映射回行
func (s *EgoExecutionService) findByExecKey(ctx context.Context, userID uint, execKey string) (*model.EgoliteExecution, error) {
	// execKey 形如 ego-<rowID>（EgoExecKey 唯一拼装点）；解析 rowID 后做归属校验
	// （不信任 runner 拼串，前缀必须精确匹配）
	var rowID uint
	if _, err := fmt.Sscanf(execKey, "ego-%d", &rowID); err != nil || rowID == 0 {
		return nil, ErrExecutionNotFound
	}
	return s.execRepo.GetForUser(ctx, rowID, userID)
}

func parseEventOk(payload json.RawMessage) *bool {
	if len(payload) == 0 {
		return nil
	}
	var m map[string]any
	if json.Unmarshal(payload, &m) != nil {
		return nil
	}
	if v, ok := m["ok"]; ok {
		if b, ok := v.(bool); ok {
			return &b
		}
	}
	return nil
}

func normalizeEventType(t string) string {
	switch t {
	case "action", "snapshot", "artifact", "handoff", "extract", "confirm", "done", "error", "runner":
		return t
	default:
		return "runner" // 未知类型归 runner（自述级审计不丢信息）
	}
}
