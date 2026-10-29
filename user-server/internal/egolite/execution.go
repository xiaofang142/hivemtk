package egolite

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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
	assetRepo EgoAssetRepository      // market 通道的资产校验源（nil ⇒ market 全拒，fail-closed）
	wcRepo    EgoWriteClaimRepository // 写台账（红线 2：声明写的执行必须留台账）
	registry  *EgoRunnerRegistry
}

func NewEgoExecutionService(execRepo EgoExecutionRepository, evRepo EgoEventRepository, flowRepo EgoFlowRepository, assetRepo EgoAssetRepository, wcRepo EgoWriteClaimRepository, registry *EgoRunnerRegistry) *EgoExecutionService {
	return &EgoExecutionService{execRepo: execRepo, evRepo: evRepo, flowRepo: flowRepo, assetRepo: assetRepo, wcRepo: wcRepo, registry: registry}
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
	Origin        string          `json:"origin"`   // draft / market
	FlowID        uint            `json:"flow_id"`  // draft 时必填
	AssetID       string          `json:"asset_id"` // market 时必填
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
		// 版本钉扎（specs/017①）：客户端报的版本必须等于服务端当前批准版本。
		// 橱窗快照/拉取滞后带来的旧版本号不能换来"按旧脚本组装"的执行——
		// 批准 hash 快照只在当前版本上成立。
		if req.AssetVersion != fmt.Sprintf("%d", a.Version) {
			return nil, ErrAssetVersionConflict
		}
		// 资产声明了写动作 ⇒ 强制步级确认（客户端不可自行关闭，决策 4）
		if len(assetWriteNames(a)) > 0 {
			req.ConfirmPolicy = "require"
		}
		// 参数契约（S-11）：market 通道以资产上架时登记的 params_schema 为准。
		// 放在 Start 而不是前端表单里——表单可绕，执行域不可绕。
		if err := ValidateEgoParams(a.ParamsSchema, req.Params); err != nil {
			return nil, fmt.Errorf("资产 %s 参数校验失败: %w", a.AssetCode, err)
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

	// draft 通道与 DryRun 同门（specs/017④）：直接 POST executions 不允许绕过扫描门。
	// 扫描门只在 DryRun/Publish/cron 挂过的日子已经结束——Start 是第四个入口，同规则。
	//（校验顺序放在并发闸之后：闸满先报闸，属验证排序而非豁免。）
	if req.Origin == "draft" {
		flow, err := s.flowRepo.GetForUser(ctx, req.FlowID, userID)
		if err != nil {
			return nil, fmt.Errorf("flow 不存在")
		}
		if !flow.ScanPassed {
			return nil, ErrFlowScanGate
		}
		if flow.Script == "" {
			return nil, fmt.Errorf("flow 脚本为空")
		}
		// 参数契约（S-11）：draft 通道（含 DryRun）以流程自身登记的 params_schema 为准。
		if err := ValidateEgoParams(flow.ParamsSchema, req.Params); err != nil {
			return nil, fmt.Errorf("流程参数校验失败: %w", err)
		}
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
		// 防御性扫描门（specs/017④）：Redispatch 不再过 Start 入口，扫描结论在这里
		// 兜底——保存后扫描翻红的流程不能经重发通道漏出去。
		if !flow.ScanPassed {
			_ = s.execRepo.TransitionState(ctx, e.ID, []string{model.EgoExecDispatching}, model.EgoExecFailed,
				map[string]any{"fail_reason": "扫描门未通过（重发通道兜底拦截）", "ended_at": time.Now()})
			return false
		}
		spec["draft_script"] = flow.Script
		spec["domains"] = parseDomains(flow.DomainsJSON)
		s.claimWrites(ctx, e, writeNames(flow.WritesJSON))
	}
	if e.Origin == "market" && s.assetRepo != nil {
		// 域名白名单随 spec 下发（specs/015：runner 运行时域审计的数据源；
		// 下发时刻快照，执行期目录变更不影响在途执行——版本钉扎同语义）
		if a, err := s.assetRepo.GetByCode(ctx, e.AssetID); err == nil {
			// 批准快照一致性（specs/017①）：Start 与 dispatch 之间资产更新 ⇒
			// 本执行行的版本号已不对应任何现行批准状态，判 failed 而非照跑。
			if fmt.Sprintf("%d", a.Version) != e.AssetVer {
				_ = s.execRepo.TransitionState(ctx, e.ID, []string{model.EgoExecDispatching}, model.EgoExecFailed,
					map[string]any{"fail_reason": fmt.Sprintf("资产版本已更新（当前 v%d，执行创建于 v%s），批准快照失效", a.Version, e.AssetVer), "ended_at": time.Now()})
				return false
			}
			// 服务端批准 hash 快照随 spec 下发：runner 侧与本地 manifest 交叉核对，
			// 拦"本地工作区滞留旧版本资产照旧组装"（拉取滞后/同步失败的窗口）。
			spec["script_hash"] = a.ScriptHash
			spec["domains"] = parseDomains(a.DomainsJSON)
			s.claimWrites(ctx, e, assetWriteNames(a))
		}
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

// finishFromDone done 帧终态判定：exit_code==0 且未超时 → completed，否则 failed。
// completed 的证据口径（specs/017⑤）：退出码是 runner 进程级信号，不是业务完成证明；
// 事件流里没有任何结构化行（动作/提取/产物/确认）时，完成判定事实上仅凭退出码，
// 此时补一条 EGO-R033 审计告警行——终态不变（exit 0 仍是唯一可用信号），但监控面
// 能看到这次"完成"是自述级弱证据，与 G-E5「不虚记 verified」同一诚实口径。
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
	if state == model.EgoExecCompleted && !s.hasStructuredEvidence(dbCtx, e.ID) {
		warnPayload, _ := json.Marshal(map[string]any{
			"event": "completion_evidence_warning",
			"code":  "EGO-R033",
			"note":  "exit 0 但事件流无结构化行（动作/提取/产物/确认），完成判定仅凭 runner 退出码（自述级弱证据）",
		})
		_ = s.evRepo.Append(dbCtx, &model.EgoEventLog{
			ExecutionID: e.ID,
			Seq:         ev.Seq + 1,
			Type:        "runner",
			Payload:     datatypes.JSON(warnPayload),
		})
	}
}

// hasStructuredEvidence 事件流里是否存在结构化证据行（done/runner/error 之外的
// 业务侧事件）。200 条窗口：结构化行只会出现在前 200 条里（每轮动作至少一条），
// 超过 200 条必然已经有过结构化行。
func (s *EgoExecutionService) hasStructuredEvidence(ctx context.Context, execID uint) bool {
	events, err := s.evRepo.ListByExecution(ctx, execID, 200)
	if err != nil {
		return true // 查询失败不误报：宁可少一条告警，不给执行扣上无证据帽子
	}
	for _, ev := range events {
		switch ev.Type {
		case "action", "extract", "snapshot", "artifact", "confirm", "handoff", "round_result":
			return true
		}
	}
	return false
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
		s.markSentOnAllow(ctx, e)
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
			// fail-close 要落到进程：TTL 判停必须让 runner 停掉在途 ego 执行（specs/014 D8），
			// 否则脚本还在轮询空转占住 space。conn 不在线时静默——脚本侧 TTL 自停兜底。
			if conn, online := s.registry.GetConn(e.UserID); online {
				_ = conn.SendSpec(map[string]any{"type": "abort", "spec": map[string]any{"execution_id": EgoExecKey(e.ID)}})
			}
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

// parseDomains domains JSON 列 → 字符串数组（解析失败/空视为零域：runner 侧零域
// = 全拒，fail-close 与服务端「保存时 domains 必填」口径互为纵深）
func parseDomains(raw []byte) []string {
	var domains []string
	if json.Unmarshal(raw, &domains) != nil {
		return nil
	}
	return domains
}

// writeNames 流程 writes JSON 列 → 动作名数组（解析失败视为未声明）
func writeNames(raw []byte) []string {
	var writes []string
	if json.Unmarshal(raw, &writes) != nil {
		return nil
	}
	return writes
}

// claimWrites 写台账占坑（落地指南红线 2：声明写的执行必须留台账）。
// ON CONFLICT 幂等——redispatch 重复 dispatch 不重复占坑；占坑时机在真下发
// （dispatch）而非 Start：没下发的执行没有授权任何写。
func (s *EgoExecutionService) claimWrites(ctx context.Context, e *model.EgoliteExecution, writes []string) {
	if s.wcRepo == nil {
		return
	}
	for _, w := range writes {
		w = strings.TrimSpace(w)
		if w == "" {
			continue
		}
		_, _ = s.wcRepo.Claim(ctx, &model.EgoliteWriteClaim{
			TenantID:    e.TenantID,
			ExecutionID: e.ID,
			Action:      w,
			UserID:      e.UserID,
			AssetID:     e.AssetID,
			ClaimState:  model.EgoWriteClaimed,
		})
	}
}

// markSentOnAllow 台账推进：放行 = 这次写已授权执行 → claimed → sent。
// 动作名取自最近一条 confirm 事件（脚本 __awaitConfirm__(label) 的 label）；
// 对不上声明清单就如实不动——不认识的写不标 sent（与 confirm 闸 fail-close 同向）。
// 刻意不标 verified：ego 腿审计是自述级（G-E5），服务端没有独立验证证据，虚记
// verified 就是腿 1 假绿事故的同款形状。
func (s *EgoExecutionService) markSentOnAllow(ctx context.Context, e *model.EgoliteExecution) {
	if s.wcRepo == nil {
		return
	}
	events, err := s.evRepo.ListByExecution(ctx, e.ID, 200)
	if err != nil || len(events) == 0 {
		return
	}
	action := ""
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type == "confirm" {
			var p struct {
				Action string `json:"action"`
			}
			if json.Unmarshal(events[i].Payload, &p) == nil {
				action = strings.TrimSpace(p.Action)
			}
			break
		}
	}
	if action == "" {
		return
	}
	claims, err := s.wcRepo.ListByExecution(ctx, e.ID)
	if err != nil {
		return
	}
	for _, c := range claims {
		if c.Action == action && c.ClaimState == model.EgoWriteClaimed {
			_ = s.wcRepo.MarkSent(ctx, c.ID)
		}
	}
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
	case "action", "snapshot", "artifact", "handoff", "extract", "confirm", "round_result", "done", "error", "runner":
		return t
	default:
		return "runner" // 未知类型归 runner（自述级审计不丢信息）
	}
}
