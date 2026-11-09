package agent_runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// ---------- TB-01 数据驱动意图→工具链（绑定层 runtime 本地契约） ----------

// IntentChainStep 链上一步计划工具调用（绑定层数据映射）。
// Args 为绑定层存的模板值，由 planner 用 Payload 白名单变量渲染成终值。
type IntentChainStep struct {
	ToolName string
	Args     map[string]any
	Priority int
	Enabled  bool
}

// IntentChainSpec 绑定层查到的意图工具链。
type IntentChainSpec struct {
	IntentCode string
	Steps      []IntentChainStep
	ReplyHint  string
	// Slots 必填槽位（TB-08）：agentloop 严格模式强制缺失追问（ToolChoice=none）；
	// planner 路径 v1 仅透传不作强制（无实体抽取面）。
	Slots []IntentChainSlot
}

// IntentChainSlot 必填槽位定义。
type IntentChainSlot struct {
	Slot      string
	Question  string
	SourceVar string
	Required  bool
}

// IntentChainLookup 消费方侧接口：意图→工具链绑定数据源。
// service/repository 侧隐式实现；租户作用域由实现方从 ctx 解析，runtime 不感知租户。
// 契约：返回 (nil, nil) = 该意图无启用绑定 → planner 回落既有硬编码规划（零行为变化）。
type IntentChainLookup interface {
	LookupIntentChain(ctx context.Context, intent string) (*IntentChainSpec, error)
}

// chainTemplateVars 模板变量白名单（与绑定层 UI 约定一致；白名单外变量整链拒用）。
var chainTemplateVars = map[string]func(ic *InferenceContext) string{
	"customer_id":  func(ic *InferenceContext) string { return ic.Payload.CustomerID },
	"session_id":   func(ic *InferenceContext) string { return ic.Payload.SessionID },
	"content":      func(ic *InferenceContext) string { return ic.Payload.Content },
	"channel_type": func(ic *InferenceContext) string { return ic.Payload.ChannelType },
	"account_id":   func(ic *InferenceContext) string { return ic.Payload.AccountID },
}

// ChainTemplateVarNames 模板变量白名单名清单（升序；绑定层写侧预检与 UI 提示共用同一事实源）。
func ChainTemplateVarNames() []string {
	names := make([]string, 0, len(chainTemplateVars))
	for name := range chainTemplateVars {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ChainMaxSteps 单链步数上限（防御 UI 误配超长链）。
const ChainMaxSteps = 8

// ValidateChainArgs 绑定层写侧预检（TB-01）：args 值仅允许 string/数值/布尔；
// 字符串值里的 {{var}} 变量必须在白名单内或完全不含模板占位。写侧预检与运行期
// 渲染共用同一白名单（fail-closed 双防线：写侧拒配 + 运行期整链拒用）。
func ValidateChainArgs(args map[string]any) error {
	for k, v := range args {
		switch v.(type) {
		case nil, string, bool, int, int64, float64, json.Number:
			// 数值/布尔原样放行（JSON 反序列化常见形态）
		default:
			return fmt.Errorf("参数 %q 值类型不支持（仅允许字符串/数值/布尔）", k)
		}
		if s, ok := v.(string); ok {
			if err := validateChainTemplateVars(s); err != nil {
				return fmt.Errorf("参数 %q: %w", k, err)
			}
		}
	}
	return nil
}

func validateChainTemplateVars(tpl string) error {
	rest := tpl
	for {
		open := strings.Index(rest, "{{")
		if open < 0 {
			return nil
		}
		close := strings.Index(rest[open:], "}}")
		if close < 0 {
			return fmt.Errorf("模板占位符未闭合: %q", tpl)
		}
		name := strings.TrimSpace(rest[open+2 : open+close])
		if !isChainTemplateVarAllowed(name) {
			return fmt.Errorf("模板变量 %q 不在白名单内（可用: %s 或 entity.<key>）", name, strings.Join(ChainTemplateVarNames(), "/"))
		}
		rest = rest[open+close+2:]
	}
}

// isChainTemplateVarAllowed 白名单判定：固定上下文变量或 entity.<key> 形态（TB-08 实体槽位）。
// entity 键形态与绑定层写侧校验一致（字母/数字/下划线）。
func isChainTemplateVarAllowed(name string) bool {
	if _, ok := chainTemplateVars[name]; ok {
		return true
	}
	if rest := strings.TrimPrefix(name, "entity."); rest != name {
		return len(rest) > 0 && isEntityKeyForm(rest)
	}
	return false
}

// isEntityKeyForm 实体键形态（字母开头的字母/数字/下划线，≤50）。
func isEntityKeyForm(key string) bool {
	if len(key) == 0 || len(key) > 50 {
		return false
	}
	for i, r := range key {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9', r == '_':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	first := rune(key[0])
	return (first >= 'a' && first <= 'z') || (first >= 'A' && first <= 'Z')
}

// renderChainArgs 渲染步骤 Args 模板。失败（未知变量/非字符串模板形态）返回 nil。
// 规则：字符串值里的 {{var}} 按白名单替换；白名单外变量 = 整链拒用（fail-closed 回落硬编码）。
func renderChainArgs(args map[string]any, ic *InferenceContext) (map[string]any, bool) {
	if len(args) == 0 {
		return map[string]any{}, true
	}
	out := make(map[string]any, len(args))
	for k, v := range args {
		s, ok := v.(string)
		if !ok {
			out[k] = v
			continue
		}
		rendered, ok := renderChainTemplate(s, ic)
		if !ok {
			return nil, false
		}
		out[k] = rendered
	}
	return out, true
}

func renderChainTemplate(tpl string, ic *InferenceContext) (string, bool) {
	var b strings.Builder
	rest := tpl
	for {
		open := strings.Index(rest, "{{")
		if open < 0 {
			b.WriteString(rest)
			return b.String(), true
		}
		close := strings.Index(rest[open:], "}}")
		if close < 0 {
			return "", false
		}
		name := strings.TrimSpace(rest[open+2 : open+close])
		if fn, ok := chainTemplateVars[name]; ok {
			b.WriteString(rest[:open])
			b.WriteString(fn(ic))
			rest = rest[open+close+2:]
			continue
		}
		// TB-08：entity.<key> 在 planner 路径渲染为空串（advisory 提示面，无实体抽取源）；
		// 真实执行面 agentloop 由 LLM 供参+槽位门保障。
		if isChainTemplateVarAllowed(name) {
			b.WriteString(rest[:open])
			rest = rest[open+close+2:]
			continue
		}
		return "", false
	}
}

// buildChainPlan 绑定命中时按数据生成计划；任一步骤不可渲染/空工具名/超步数 → (nil,false) 整链拒用。
func buildChainPlan(spec *IntentChainSpec, ic *InferenceContext) (*ActionPlan, bool) {
	if spec == nil || len(spec.Steps) == 0 || len(spec.Steps) > ChainMaxSteps {
		return nil, false
	}
	calls := make([]PlannedToolCall, 0, len(spec.Steps))
	for _, st := range spec.Steps {
		if !st.Enabled {
			continue
		}
		if strings.TrimSpace(st.ToolName) == "" {
			return nil, false
		}
		args, ok := renderChainArgs(st.Args, ic)
		if !ok {
			return nil, false
		}
		calls = append(calls, PlannedToolCall{ToolName: st.ToolName, Args: args, Priority: st.Priority})
	}
	hint := spec.ReplyHint
	if strings.TrimSpace(hint) == "" {
		hint = buildDefaultHint(ic)
	}
	return &ActionPlan{
		PlanType:   "chain",
		ToolCalls:  calls,
		ReplyHint:  hint,
		Confidence: 0.90,
		PlanSource: "chain",
	}, true
}
