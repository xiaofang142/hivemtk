package tooluse

// egolite_tools.go ego-lite 腿 MCP 工具集（specs/016 S11，设计文档 S11「SetBrowserTaskRunner 同型」）
// 工具: egolite_run
// 执行注入: SetEgoLiteRunner(execSvc.Start 的闭包) 由 router 装配层注入，
// 避免 tooluse → egolite 反向依赖（与 browser_tools.go 同款纪律）。

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime/debug"
	"time"

	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/logger"

	"gorm.io/gorm"
)

// EgoLiteRunRequest egolite_run 的执行请求（注入函数的入参形状）
type EgoLiteRunRequest struct {
	FlowID        uint            `json:"flow_id"`        // draft 通道
	AssetID       string          `json:"asset_id"`       // market 通道（资产编码）
	Params        json.RawMessage `json:"params"`         // 可选调用方参数
	SpaceName     string          `json:"space_name"`     // 可选；同名 space 续跑
	TimeoutSec    int             `json:"timeout_sec"`    // 可选；<=0 用服务端默认
	ConfirmPolicy string          `json:"confirm_policy"` // none/require（按流程 writes 派生，工具层裁决）
}

// egoLiteRunner 执行注入点（进程级，router 装配时注入）
var egoLiteRunner func(ctx context.Context, userID uint, req EgoLiteRunRequest) (map[string]any, error)

// SetEgoLiteRunner 注入 ego-lite 执行函数（EnsureOnline + execution Start）
func SetEgoLiteRunner(fn func(ctx context.Context, userID uint, req EgoLiteRunRequest) (map[string]any, error)) {
	egoLiteRunner = fn
}

// egoLiteFlowRow 查询行结构（直接查表，避免 import egolite 模型——browserTaskRow 同款）
type egoLiteFlowRow struct {
	ID         uint   `gorm:"column:id"`
	UserID     uint   `gorm:"column:user_id"`
	Name       string `gorm:"column:name"`
	Status     string `gorm:"column:status"`
	ScanPassed bool   `gorm:"column:scan_passed"`
	WritesJSON []byte `gorm:"column:writes"`
}

// EgoLiteRunTool 发起一个 ego-lite 执行（用户本机 ego lite 浏览器内运行流程/资产）
type EgoLiteRunTool struct {
	BaseTool
	deps BrowserToolDeps
}

// NewEgoLiteRunTool 创建工具
func NewEgoLiteRunTool(deps BrowserToolDeps) *EgoLiteRunTool {
	return &EgoLiteRunTool{
		BaseTool: BaseTool{
			NameVal:     "egolite_run",
			RiskVal:     RiskHighWrite,
			CategoryVal: CategoryBusiness,
			DescriptionVal: "在本机 ego lite 浏览器（用户已登录会话）中发起一个流程/资产执行。" +
				"flow_id 走用户自建流程（draft，归属人即发起人），asset_id 走资产中心（market，需显式给 user_id），二选一必填。" +
				"返回执行信息并异步派发；执行进度与事件时间线用 egolite 执行查询接口。",
			ParamsVal: ToolParameters{
				Type: "object",
				Properties: map[string]ToolParam{
					"flow_id":     {Type: "integer", Description: "ego-lite 流程 ID（egolite_flows.id，draft 通道）"},
					"asset_id":    {Type: "string", Description: "资产编码（egolite_assets.asset_code，market 通道）"},
					"user_id":     {Type: "integer", Description: "可选，覆盖归属用户（flow 通道默认用流程创建者；asset 通道必填）"},
					"params":      {Type: "object", Description: "可选，调用方参数（透传给脚本 __PARAMS__）"},
					"space_name":  {Type: "string", Description: "可选，TaskSpace 名（同名 space 续跑）"},
					"timeout_sec": {Type: "integer", Description: "可选，执行超时秒数（默认 1800，上限 3600）"},
				},
			},
		},
		deps: deps,
	}
}

// Execute 校验 + 异步派发
func (t *EgoLiteRunTool) Execute(ctx context.Context, args map[string]any) (ToolResult, error) {
	req := EgoLiteRunRequest{}
	var userID uint
	hasFlow := false
	if v, ok := args["flow_id"]; ok && v != nil {
		id, err := GetIntArg(args, "flow_id")
		if err != nil || id <= 0 {
			return ErrorResult(t.Name(), fmt.Errorf("flow_id 参数无效: %v", v)), nil
		}
		hasFlow = true
		req.FlowID = uint(id)
	}
	hasAsset := false
	if v, ok := args["asset_id"]; ok && v != nil {
		s, err := GetStringArg(args, "asset_id")
		if err != nil || s == "" {
			return ErrorResult(t.Name(), fmt.Errorf("asset_id 参数无效: %v", v)), nil
		}
		hasAsset = true
		req.AssetID = s
	}
	if !hasFlow && !hasAsset {
		return ErrorResult(t.Name(), fmt.Errorf("flow_id 与 asset_id 必须二选一")), nil
	}

	if hasFlow {
		// draft 通道：流程行定归属 + 状态前置校验（browser_open_task 同款：
		// 非法状态即时反馈而非异步静默失败）
		var flow egoLiteFlowRow
		if err := t.deps.DB.WithContext(ctx).
			Select("id, user_id, name, status, scan_passed, writes").
			Table("egolite_flows").
			Where("id = ?", req.FlowID).
			Take(&flow).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return ErrorResult(t.Name(), fmt.Errorf("ego-lite 流程 %d 不存在", req.FlowID)), nil
			}
			return ErrorResult(t.Name(), fmt.Errorf("查询流程失败: %w", err)), nil
		}
		if !flow.ScanPassed {
			return ErrorResult(t.Name(), fmt.Errorf("ego-lite 流程 %d 扫描门未通过（存在 block 级命中），禁止执行", req.FlowID)), nil
		}
		userID = flow.UserID
		var writes []string
		if json.Unmarshal(flow.WritesJSON, &writes) == nil && len(writes) > 0 {
			// 决策 4：声明写动作 ⇒ 逐步确认（客户端不可关闭）
			req.ConfirmPolicy = "require"
		} else {
			req.ConfirmPolicy = "none"
		}
	} else {
		// market 通道：资产是全局目录，发起人必须显式给定
		if v, err := GetIntArg(args, "user_id"); err == nil && v > 0 {
			userID = uint(v)
		}
		if userID == 0 {
			return ErrorResult(t.Name(), fmt.Errorf("asset_id（market）通道必须显式提供 user_id（执行绑定到该用户的本机 runner）")), nil
		}
	}
	if v, err := GetIntArg(args, "user_id"); err == nil && v > 0 {
		userID = uint(v) // 可选覆盖（browser_open_task 同款）
	}
	if v, ok := args["params"]; ok && v != nil {
		if b, err := json.Marshal(v); err == nil {
			req.Params = b
		}
	}
	if v, err := GetStringArg(args, "space_name"); err == nil {
		req.SpaceName = v
	}
	if v, err := GetIntArg(args, "timeout_sec"); err == nil && v > 0 {
		req.TimeoutSec = v
	}

	if egoLiteRunner == nil {
		return ErrorResult(t.Name(), fmt.Errorf("ego-lite runner 未配置（进程未装配 ego-lite 执行器）")), nil
	}

	// 异步派发：执行可能持续数分钟，不阻塞工具调用（browser_open_task 同款 + recover 兜底）；
	// runCtx 继承请求租户面，超时上限 30min（执行自身还有服务端硬超时看门狗）
	go func(userID uint, req EgoLiteRunRequest) {
		defer func() {
			if r := recover(); r != nil {
				logger.Errorf("[EgoLiteTools] runner panic: req=%+v panic=%v\n%s", req, r, debug.Stack())
			}
		}()
		runCtx, cancel := context.WithTimeout(tenant.BackgroundWithTenant(ctx), 30*time.Minute)
		defer cancel()
		if _, err := egoLiteRunner(runCtx, userID, req); err != nil {
			logger.Warnf("[EgoLiteTools] 派发失败 user=%d req=%+v: %v", userID, req, err)
		}
	}(userID, req)

	return SuccessResult(t.Name(), map[string]any{
		"dispatched": true,
		"user_id":    userID,
		"flow_id":    req.FlowID,
		"asset_id":   req.AssetID,
		"hint":       "已异步派发；用 egolite 执行列表（origin=最新一条）查询执行 ID、状态与事件时间线",
	}), nil
}

// ---------- 注册 ----------

// BuildEgoLiteTools 构建 ego-lite 工具集
func BuildEgoLiteTools(deps BrowserToolDeps) []Tool {
	return []Tool{NewEgoLiteRunTool(deps)}
}

// RegisterEgoLiteTools 注册 ego-lite 工具到 registry
func RegisterEgoLiteTools(registry *ToolRegistry, deps BrowserToolDeps) error {
	for _, t := range BuildEgoLiteTools(deps) {
		if err := registry.Register(t); err != nil {
			return fmt.Errorf("register egolite tool %s: %w", t.Name(), err)
		}
	}
	return nil
}
