package tooluse

// browser_session_tools.go 通用浏览器执行能力（M2 编排入口）。
//
// 设计纪律（internal-docs/ego-browser-通用执行能力-技术方案与开发清单-2026-10-28.md §7.5）：
//   - 工具面统一命名 + 引擎参数，不再按腿分裂（engine=auto/ego/nm）；
//   - 默认 ops 闭合动作集；预置脚本仍走 egolite_run / browser_task；
//   - 端口注入式接线（SetBrowserSessionPort），tooluse 不反向 import egolite/browser_automation。

import (
	"context"
	"encoding/json"
	"fmt"

	"hivemtk-user/internal/browsercore"
)

// BrowserSessionPort 通用会话执行端口（由 router 装配层注入 browsercore.Executor）。
type BrowserSessionPort interface {
	Open(ctx context.Context, req browsercore.OpenRequest) (*browsercore.SessionInfo, error)
	Act(ctx context.Context, req browsercore.RoundRequest) (*browsercore.ActResult, error)
	Observe(ctx context.Context, req browsercore.ObserveRequest) (*browsercore.RoundInfo, error)
	Finish(ctx context.Context, userID, sessionID uint, keep []string) (*browsercore.SessionInfo, error)
}

var browserSessionPort BrowserSessionPort

// SetBrowserSessionPort 注入通用会话端口（进程级；router 装配时调用）。
func SetBrowserSessionPort(p BrowserSessionPort) { browserSessionPort = p }

// ---------- browser_session_open ----------

// BrowserSessionOpenTool 开一个浏览器会话（ego lite / NM 桥腿，engine=auto 由路由器选）
type BrowserSessionOpenTool struct {
	BaseTool
	deps BrowserToolDeps
}

// NewBrowserSessionOpenTool 创建工具
func NewBrowserSessionOpenTool(deps BrowserToolDeps) *BrowserSessionOpenTool {
	return &BrowserSessionOpenTool{
		BaseTool: BaseTool{
			NameVal:     "browser_session_open",
			RiskVal:     RiskHighWrite,
			CategoryVal: CategoryBusiness,
			DescriptionVal: "在用户本机浏览器中开一个通用任务会话（复用已登录态）。" +
				"engine=auto 优先 ego lite（TaskSpace 隔离），不可用时回落 NM 桥腿。" +
				"domains 必填（空=拒绝一切导航）；写动作会自动强制逐步确认。" +
				"返回 session_id，随后用 browser_act 逐轮执行、browser_observe 取观测。",
			ParamsVal: ToolParameters{
				Type: "object",
				Properties: map[string]ToolParam{
					"user_id":       {Type: "integer", Description: "会话归属用户（执行绑定到该用户本机 runner）"},
					"goal":          {Type: "string", Description: "任务目标（审计/展示用，不直接下发浏览器）"},
					"engine":        {Type: "string", Description: "auto|ego|nm（默认 auto）"},
					"space_name":    {Type: "string", Description: "可选，TaskSpace 名（同名=续跑）"},
					"domains":       {Type: "array", Description: "域名白名单（必填；\"*\" 表示任意站点）"},
					"write_confirm": {Type: "string", Description: "none|require（默认 require）"},
					"timeout_sec":   {Type: "integer", Description: "单轮硬超时秒数（默认 300，上限 3600）"},
					"max_rounds":    {Type: "integer", Description: "总轮次上限（默认 30，上限 200）"},
					"max_actions":   {Type: "integer", Description: "单轮动作上限（默认 20，上限 100）"},
				},
				Required: []string{"user_id", "domains"},
			},
		},
		deps: deps,
	}
}

// Execute 实现 Tool
func (t *BrowserSessionOpenTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	if browserSessionPort == nil {
		return ErrorResult(t.Name(), fmt.Errorf("浏览器会话端口未装配")), nil
	}
	userID, err := GetIntArg(args, "user_id")
	if err != nil || userID <= 0 {
		return ErrorResult(t.Name(), fmt.Errorf("user_id 必填且必须为正整数")), nil
	}
	domains, err := getStringSliceArg(args, "domains")
	if err != nil {
		return ErrorResult(t.Name(), err), nil
	}
	if len(domains) == 0 {
		return ErrorResult(t.Name(), fmt.Errorf("domains 必填（空表示拒绝一切导航；确需任意站点请显式写 \"*\"）")), nil
	}
	engine, _ := GetStringArg(args, "engine")
	space, _ := GetStringArg(args, "space_name")
	goal, _ := GetStringArg(args, "goal")
	confirm, _ := GetStringArg(args, "write_confirm")
	pol := browsercore.DefaultPolicy()
	pol.Domains = domains
	if confirm != "" {
		pol.WriteConfirm = confirm
	}
	if v, e := GetIntArg(args, "timeout_sec"); e == nil && v > 0 {
		pol.TimeoutSec = v
	}
	if v, e := GetIntArg(args, "max_rounds"); e == nil && v > 0 {
		pol.MaxRounds = v
	}
	if v, e := GetIntArg(args, "max_actions"); e == nil && v > 0 {
		pol.MaxActions = v
	}
	sess, err := browserSessionPort.Open(ctx, browsercore.OpenRequest{
		UserID: uint(userID), Engine: engine, Mode: browsercore.ModeOps,
		Goal: goal, SpaceName: space, CreatedFrom: "agent", Policy: pol,
	})
	if err != nil {
		return ErrorResult(t.Name(), err), nil
	}
	return SuccessResult(t.Name(), map[string]any{
		"session_id": sess.ID, "state": sess.State, "engine": sess.Engine,
		"space_name": sess.SpaceName, "round": sess.Round,
		"hint": "用 browser_act 提交一轮 ops；每轮返回后可用 browser_observe 取结构化观测",
	}), nil
}

// ---------- browser_act ----------

// BrowserActTool 提交一轮 ops
type BrowserActTool struct {
	BaseTool
	deps BrowserToolDeps
}

// NewBrowserActTool 创建工具
func NewBrowserActTool(deps BrowserToolDeps) *BrowserActTool {
	return &BrowserActTool{
		BaseTool: BaseTool{
			NameVal:     "browser_act",
			RiskVal:     RiskHighWrite,
			CategoryVal: CategoryBusiness,
			DescriptionVal: "向一个通用浏览器会话提交一轮受限动作（ops）。" +
				"支持 goto/click/fill/press/select/scroll/wait_for/extract/upload/screenshot/handoff/finish。" +
				"不接受任意 JS；跨轮禁止使用快照 ref，请用观测返回的 loc= 语义定位。" +
				"异步派发，返回 round；随后用 browser_observe 取本轮结果。",
			ParamsVal: ToolParameters{
				Type: "object",
				Properties: map[string]ToolParam{
					"user_id":    {Type: "integer", Description: "会话归属用户"},
					"session_id": {Type: "integer", Description: "browser_session_open 返回的会话 ID"},
					"ops":        {Type: "array", Description: "动作数组（闭合集合，见工具描述）"},
					"observe":    {Type: "object", Description: "可选观测选项 {scope,max_targets,max_text,screenshot}"},
				},
				Required: []string{"user_id", "session_id", "ops"},
			},
		},
		deps: deps,
	}
}

// Execute 实现 Tool
func (t *BrowserActTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	if browserSessionPort == nil {
		return ErrorResult(t.Name(), fmt.Errorf("浏览器会话端口未装配")), nil
	}
	userID, err := GetIntArg(args, "user_id")
	if err != nil || userID <= 0 {
		return ErrorResult(t.Name(), fmt.Errorf("user_id 必填")), nil
	}
	sessionID, err := GetIntArg(args, "session_id")
	if err != nil || sessionID <= 0 {
		return ErrorResult(t.Name(), fmt.Errorf("session_id 必填")), nil
	}
	ops, err := getOpsArg(args)
	if err != nil {
		return ErrorResult(t.Name(), err), nil
	}
	var obs browsercore.ObserveSpec
	if raw, ok := args["observe"]; ok && raw != nil {
		b, _ := json.Marshal(raw)
		_ = json.Unmarshal(b, &obs)
	}
	res, err := browserSessionPort.Act(ctx, browsercore.RoundRequest{
		UserID: uint(userID), SessionID: uint(sessionID), Ops: ops, Observe: obs,
	})
	if err != nil {
		return ErrorResult(t.Name(), err), nil
	}
	return SuccessResult(t.Name(), map[string]any{
		"session_id": res.Session.ID, "round": res.Round.Round, "state": res.Session.State,
		"execution_id": res.Round.ExecutionID,
		"hint":         "已异步派发；用 browser_observe（round=0 取最新）读取观测，或轮询会话详情",
	}), nil
}

// ---------- browser_observe ----------

// BrowserObserveTool 读取轮次观测（不产生页面动作）
type BrowserObserveTool struct {
	BaseTool
	deps BrowserToolDeps
}

// NewBrowserObserveTool 创建工具
func NewBrowserObserveTool(deps BrowserToolDeps) *BrowserObserveTool {
	return &BrowserObserveTool{
		BaseTool: BaseTool{
			NameVal:     "browser_observe",
			RiskVal:     RiskReadonly,
			CategoryVal: CategoryBusiness,
			DescriptionVal: "读取通用浏览器会话的轮次观测（结构化 targets/text/extract）。" +
				"只读，不触发任何页面动作；round=0 表示最新一轮。",
			ParamsVal: ToolParameters{
				Type: "object",
				Properties: map[string]ToolParam{
					"user_id":    {Type: "integer", Description: "会话归属用户"},
					"session_id": {Type: "integer", Description: "会话 ID"},
					"round":      {Type: "integer", Description: "轮次号（0=最新）"},
				},
				Required: []string{"user_id", "session_id"},
			},
		},
		deps: deps,
	}
}

// Execute 实现 Tool
func (t *BrowserObserveTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	if browserSessionPort == nil {
		return ErrorResult(t.Name(), fmt.Errorf("浏览器会话端口未装配")), nil
	}
	userID, _ := GetIntArg(args, "user_id")
	sessionID, err := GetIntArg(args, "session_id")
	if userID <= 0 || err != nil || sessionID <= 0 {
		return ErrorResult(t.Name(), fmt.Errorf("user_id / session_id 必填")), nil
	}
	round, _ := GetIntArg(args, "round")
	info, err := browserSessionPort.Observe(ctx, browsercore.ObserveRequest{
		UserID: uint(userID), SessionID: uint(sessionID), Round: round,
	})
	if err != nil {
		return ErrorResult(t.Name(), err), nil
	}
	return SuccessResult(t.Name(), info), nil
}

// ---------- browser_session_finish ----------

// BrowserSessionFinishTool 收尾会话
type BrowserSessionFinishTool struct {
	BaseTool
	deps BrowserToolDeps
}

// NewBrowserSessionFinishTool 创建工具
func NewBrowserSessionFinishTool(deps BrowserToolDeps) *BrowserSessionFinishTool {
	return &BrowserSessionFinishTool{
		BaseTool: BaseTool{
			NameVal:        "browser_session_finish",
			RiskVal:        RiskHighWrite,
			CategoryVal:    CategoryBusiness,
			DescriptionVal: "收尾一个通用浏览器会话：关闭 agent 页面（keep 指定的 Page 保留给用户）。",
			ParamsVal: ToolParameters{
				Type: "object",
				Properties: map[string]ToolParam{
					"user_id":    {Type: "integer", Description: "会话归属用户"},
					"session_id": {Type: "integer", Description: "会话 ID"},
					"keep":       {Type: "array", Description: "要保留给用户的 Page label（如 [\"p1\"]）"},
				},
				Required: []string{"user_id", "session_id"},
			},
		},
		deps: deps,
	}
}

// Execute 实现 Tool
func (t *BrowserSessionFinishTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	if browserSessionPort == nil {
		return ErrorResult(t.Name(), fmt.Errorf("浏览器会话端口未装配")), nil
	}
	userID, _ := GetIntArg(args, "user_id")
	sessionID, err := GetIntArg(args, "session_id")
	if userID <= 0 || err != nil || sessionID <= 0 {
		return ErrorResult(t.Name(), fmt.Errorf("user_id / session_id 必填")), nil
	}
	keep, _ := getStringSliceArg(args, "keep")
	sess, err := browserSessionPort.Finish(ctx, uint(userID), uint(sessionID), keep)
	if err != nil {
		return ErrorResult(t.Name(), err), nil
	}
	return SuccessResult(t.Name(), map[string]any{"session_id": sess.ID, "state": sess.State}), nil
}

// ---------- 注册 ----------

// BuildBrowserSessionTools 构建通用会话工具集
func BuildBrowserSessionTools(deps BrowserToolDeps) []Tool {
	return []Tool{
		NewBrowserSessionOpenTool(deps),
		NewBrowserActTool(deps),
		NewBrowserObserveTool(deps),
		NewBrowserSessionFinishTool(deps),
	}
}

// RegisterBrowserSessionTools 注册到 registry
func RegisterBrowserSessionTools(registry *ToolRegistry, deps BrowserToolDeps) error {
	for _, t := range BuildBrowserSessionTools(deps) {
		if err := registry.Register(t); err != nil {
			return fmt.Errorf("register browser session tool %s: %w", t.Name(), err)
		}
	}
	return nil
}

func getStringSliceArg(args map[string]any, key string) ([]string, error) {
	v, ok := args[key]
	if !ok || v == nil {
		return nil, nil
	}
	switch raw := v.(type) {
	case []string:
		return raw, nil
	case []any:
		out := make([]string, 0, len(raw))
		for _, it := range raw {
			s, ok := it.(string)
			if !ok {
				return nil, fmt.Errorf("%s 必须是字符串数组", key)
			}
			out = append(out, s)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("%s 必须是字符串数组", key)
	}
}

func getOpsArg(args map[string]any) ([]browsercore.Op, error) {
	raw, ok := args["ops"]
	if !ok || raw == nil {
		return nil, fmt.Errorf("ops 不能为空")
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("ops 序列化失败: %w", err)
	}
	var ops []browsercore.Op
	if err := json.Unmarshal(b, &ops); err != nil {
		return nil, fmt.Errorf("ops 形状非法: %w", err)
	}
	return ops, nil
}
