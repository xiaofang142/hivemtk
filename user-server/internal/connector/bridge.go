package connector

// bridge.go §3.12 刀1：连接器端点→tooluse 工具自动桥接。
//
// 职责：遍历 preset 注册表，把每个（preset, endpoint）以**纯数据**桥接为
// tooluse.Tool（tool_name = connector.<preset>.<endpoint>），供意图绑定层
// （intent_tool_chains 的 tool_name 步骤）与 LLM function-calling 消费。
// 新平台/新端点=注册 preset 数据即成为工具，零平台专属代码。
//
// 契约冻结（§3.12 刀1，与源码既有契约分开标注）：
//   C1-1 tool_name：`connector.<preset>.<endpoint>`；两段名经 sanitize（小写字母/
//       数字/下划线，其余折叠 `_`，沿 tooluse openapi 导入同款口径）；端点 Name
//       sanitize 后为空 → 该端点跳过。
//   C1-2 输入 schema：从 EndpointConfig 推导——ParamMap 键（缺省同名）即工具入参
//       （type=string，description 标注映射到的平台参数名）；必填性不猜（平台侧
//       校验，fail 由平台错误面透出）。generic 方言 GET 走 query（ParamMap 生效）、
//       POST/PUT/PATCH 走 JSON body（args 原样）。
//   C1-3 能力标签→装饰器映射（RiskHighWrite→WithApproval 强制，沿改单先例
//       business_tools.go WithApproval(NewOrderModifyRequestTool)）：
//         - Capabilities 为空         → 不桥接（负例判据：能力标签缺失不外发，fail-closed）；
//         - 全部 capability 前缀 query_ → RiskReadonly，走默认装饰器链；
//         - 其余任何 capability        → RiskHighWrite（出域写，按 tool_risk 分级判据
//           「副作用落在谁系统」：非只读=出域）→ 强制 WithApproval 包装。
//   C1-4 方言未注册的 preset → 该 preset 全部端点不桥接（负例判据，fail-closed）。
//   C1-5 治理链：桥接工具只做纯执行，治理全部走**既有**链——工具注册进 tooluse
//       注册中心后由 ToolExecutor.buildHandler 统一挂 权限/限流/熔断/重试/超时/
//       审计/计费（+可选审批门/风控门）；RiskHighWrite 端点额外强制 WithApproval。
//       SSRF 复用 connector.Executor 既有严格口径（ValidateOpenAPIBaseURL），不重写。
//   C1-6 输出：Executor.Call 的统一信封 map 原样进 ToolResult.Data；错误原样透传
//       （平台错误进 error 面，不包装成成功）；回灌 LLM 的截断沿 executor 既有
//       MaxContentLen 面，不新造第二套截断。

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"hivemtk-user/internal/aiagent/agent/tooluse"
	"hivemtk-user/internal/pkg/utils/logger"
)

// BridgeSkipReason 单个端点未桥接的原因（负例可观测：为什么没成为工具）。
type BridgeSkipReason struct {
	Preset   string `json:"preset"`
	Endpoint string `json:"endpoint"`
	Reason   string `json:"reason"`
}

// bridgeReport 桥接结果（注册数量 + 跳过原因清单）。
type bridgeReport struct {
	Tools     []tooluse.Tool
	Skipped   []BridgeSkipReason
	PresetErr []string // preset 级错误（方言未注册等）
}

// BridgeTools 遍历 preset 注册表桥接全部端点（生产口径：Executor 用 NewExecutor
// 严格 SSRF）。返回工具清单与跳过原因；调用方把工具注册进 tooluse 注册中心
// （注册进目录后由 executor 建链统一挂治理装饰器）。
func BridgeTools() ([]tooluse.Tool, []BridgeSkipReason, []string) {
	return bridgePresetsWithExecutor(ListPresets(), NewExecutor())
}

// BridgePresetsWithExecutor 测试/特殊部署面（沿 NewExecutorWithValidator 接缝口径）：
// 注入 Executor（测试经 NewExecutorWithValidator 放行环回上游），生产路径用 BridgeTools。
func BridgePresetsWithExecutor(exec *Executor, presets ...ConnectorPreset) ([]tooluse.Tool, []BridgeSkipReason, []string) {
	if len(presets) == 0 {
		presets = ListPresets()
	}
	return bridgePresetsWithExecutor(presets, exec)
}

func bridgePresetsWithExecutor(presets []ConnectorPreset, exec *Executor) ([]tooluse.Tool, []BridgeSkipReason, []string) {
	rep := bridgeReport{}
	for _, p := range presets {
		presetName := sanitizeConnectorName(p.Name)
		if presetName == "" {
			rep.PresetErr = append(rep.PresetErr, fmt.Sprintf("preset 名 sanitize 后为空: %q", p.Name))
			continue
		}
		if Get(p.Config.Provider) == nil {
			// C1-4：方言未注册 fail-closed——整台平台的端点都不成为工具。
			rep.PresetErr = append(rep.PresetErr, fmt.Sprintf(
				"preset %q 方言 %q 未注册（可用: %v）", p.Name, p.Config.Provider, List()))
			continue
		}
		for _, ep := range p.Endpoints {
			endpointName := sanitizeConnectorName(ep.Name)
			if endpointName == "" {
				rep.Skipped = append(rep.Skipped, BridgeSkipReason{Preset: p.Name, Endpoint: ep.Name, Reason: "端点名 sanitize 后为空"})
				continue
			}
			risk, reason := endpointRiskLevel(ep)
			if reason != "" {
				// C1-3：能力标签缺失不桥接（不外发）。
				rep.Skipped = append(rep.Skipped, BridgeSkipReason{Preset: p.Name, Endpoint: ep.Name, Reason: reason})
				continue
			}
			t := &dialectTool{
				base: tooluse.BaseTool{
					NameVal:        bridgeToolName(presetName, endpointName),
					CategoryVal:    tooluse.CategoryBusiness,
					RiskVal:        risk,
					DescriptionVal: bridgeToolDescription(p.Name, ep),
					ParamsVal:      bridgeToolParams(ep),
				},
				preset:   p,
				endpoint: ep,
				exec:     exec,
			}
			if risk == tooluse.RiskHighWrite {
				// C1-3：高危写强制审批包装（沿改单先例）；包装层治理语义见
				// tooluse.decorator_approval.go，覆盖率可经 tooluse.IsApprovalWrapped 断言。
				rep.Tools = append(rep.Tools, tooluse.WithApproval(t))
			} else {
				rep.Tools = append(rep.Tools, t)
			}
		}
	}
	sort.Slice(rep.Skipped, func(i, j int) bool {
		if rep.Skipped[i].Preset != rep.Skipped[j].Preset {
			return rep.Skipped[i].Preset < rep.Skipped[j].Preset
		}
		return rep.Skipped[i].Endpoint < rep.Skipped[j].Endpoint
	})
	return rep.Tools, rep.Skipped, rep.PresetErr
}

// bridgeToolName C1-1：connector.<preset>.<endpoint>（点分惯例，沿 order.lookup/
// logistics.track 同款）。
func bridgeToolName(preset, endpoint string) string {
	return "connector." + preset + "." + endpoint
}

// endpointRiskLevel C1-3：能力标签→风险分级。
// 返回 (级别, 跳过原因)；原因非空=该端点不桥接。
func endpointRiskLevel(ep EndpointConfig) (tooluse.ToolRiskLevel, string) {
	if len(ep.Capabilities) == 0 {
		return "", "能力标签缺失（Capabilities 为空），fail-closed 不桥接"
	}
	for _, c := range ep.Capabilities {
		if !strings.HasPrefix(c, "query_") {
			return tooluse.RiskHighWrite, ""
		}
	}
	return tooluse.RiskReadonly, ""
}

func bridgeToolDescription(presetName string, ep EndpointConfig) string {
	caps := strings.Join(ep.Capabilities, "/")
	return fmt.Sprintf("[Connector:%s] %s %s（能力: %s）。入参经 preset 参数映射转交三方平台，"+
		"返回统一信封（ok/data/items）。", presetName, strings.ToUpper(ep.Method), ep.Path, caps)
}

// bridgeToolParams C1-2：ParamMap 键（缺省同名）→ properties（string）。
func bridgeToolParams(ep EndpointConfig) tooluse.ToolParameters {
	props := map[string]tooluse.ToolParam{}
	// ParamMap 键序不定，排序保证 schema 冻结断言稳定。
	keys := make([]string, 0, len(ep.ParamMap))
	for k := range ep.ParamMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		props[k] = tooluse.ToolParam{Type: "string", Description: "映射到平台参数 " + ep.ParamMap[k]}
	}
	return tooluse.ToolParameters{Type: "object", Properties: props}
}

// sanitizeConnectorName C1-1：小写字母/数字/下划线，其余折叠 `_`，两侧 trim
// （沿 tooluse openapi sanitizeToolName 同款口径）。
func sanitizeConnectorName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return strings.Trim(b.String(), "_")
}

// dialectTool 桥接产物：单个（preset, endpoint）的 tooluse.Tool 实现。
// 只做纯执行（Executor.Call）；治理由外层装饰器链统一负责（与 openAPITool 同款分工）。
type dialectTool struct {
	base     tooluse.BaseTool
	preset   ConnectorPreset
	endpoint EndpointConfig
	exec     *Executor
}

func (t *dialectTool) Name() string                       { return t.base.Name() }
func (t *dialectTool) Category() tooluse.ToolCategory     { return t.base.Category() }
func (t *dialectTool) Description() string                { return t.base.Description() }
func (t *dialectTool) Parameters() tooluse.ToolParameters { return t.base.Parameters() }

// RiskLevel 透出能力标签映射出的分级（风控门判据消费）。
func (t *dialectTool) RiskLevel() tooluse.ToolRiskLevel { return t.base.RiskLevel() }

// Execute 经既有 Executor.Call 执行（SSRF/未注册方言/平台错误语义全部复用
// connector 既有实现，不重写）。
func (t *dialectTool) Execute(ctx context.Context, args map[string]any) (tooluse.ToolResult, error) {
	out, err := t.exec.Call(ctx, t.preset.Config, t.endpoint, args)
	if err != nil {
		return tooluse.ErrorResult(t.Name(), err), err
	}
	return tooluse.SuccessResult(t.Name(), out), nil
}

// BridgeProvider ToolProvider 形态的桥接器：注册进 ProviderRegistry 后由既有
// RegisterAll 装配进工具目录（preset 表为空 = 零工具零行为变化）。
type BridgeProvider struct {
	// Executor nil 时用生产构造（NewExecutor 严格 SSRF）；测试接缝可注入。
	Executor *Executor
}

func (p *BridgeProvider) Name() string                   { return "connector_bridge" }
func (p *BridgeProvider) Category() tooluse.ToolCategory { return tooluse.CategoryBusiness }
func (p *BridgeProvider) Description() string {
	return "连接器 preset 桥接工具（connector.<preset>.<endpoint>；三方平台注册即成为工具）"
}

// Provide 桥接当前 preset 注册表（无 DB 依赖：preset 数据进程内）。
//
// 单个 preset 配置错误（方言未注册/端点名非法/能力标签缺失）**不返回 error**：
// RegisterAll 对 Provider error 的语义是丢弃该 Provider 全部工具——那会把无辜
// preset 的工具一并拖下水。fail-closed 的正确落点=坏 preset 的端点缺席 + WARN
// 留痕（「为什么我的 preset 没成工具」按此日志排查）。
func (p *BridgeProvider) Provide(ctx tooluse.ProviderContext) ([]tooluse.Tool, error) {
	exec := p.Executor
	if exec == nil {
		exec = NewExecutor()
	}
	tools, skipped, presetErrs := bridgePresetsWithExecutor(ListPresets(), exec)
	for _, s := range skipped {
		logger.Warnf("[connector_bridge] 端点未桥接 preset=%s endpoint=%s reason=%s", s.Preset, s.Endpoint, s.Reason)
	}
	for _, e := range presetErrs {
		logger.Warnf("[connector_bridge] preset 未桥接: %s", e)
	}
	return tools, nil
}
