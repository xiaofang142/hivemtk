package browsercore

import (
	"fmt"
	"strings"
)

// ops DSL v1：闭合动作集。**不接受任意 JS / evaluate**——通用能力默认路径
// 必须能被策略层裁决；需要复杂页面逻辑时走 script 模式（审核 + 扫描门）。
const (
	OpGoto       = "goto"
	OpClick      = "click"
	OpFill       = "fill"
	OpPress      = "press"
	OpSelect     = "select"
	OpScroll     = "scroll"
	OpWaitFor    = "wait_for"
	OpExtract    = "extract"
	OpUpload     = "upload"
	OpScreenshot = "screenshot"
	OpHandoff    = "handoff"
	OpFinish     = "finish"
)

// Op 单个动作。字段按动作取用，未用字段忽略。
type Op struct {
	Op     string `json:"op"`
	URL    string `json:"url,omitempty"`
	Target string `json:"target,omitempty"`
	Value  string `json:"value,omitempty"`
	Key    string `json:"key,omitempty"`   // press / wait_for(function)
	Label  string `json:"label,omitempty"` // 光标动作文案 / confirm 动作名
	// selector 与 target 同义（wait_for 用）；保留两个名字是为了让 DSL 读起来自然
	Selector  string   `json:"selector,omitempty"`
	DY        int      `json:"dy,omitempty"`
	Fields    []string `json:"fields,omitempty"`
	Limit     int      `json:"limit,omitempty"`
	Scope     string   `json:"scope,omitempty"`
	FullPage  bool     `json:"full_page,omitempty"`
	Path      string   `json:"path,omitempty"`   // upload：受控产物路径
	Reason    string   `json:"reason,omitempty"` // handoff
	Keep      []string `json:"keep,omitempty"`   // finish
	Secret    bool     `json:"secret,omitempty"` // fill：敏感值不回显
	TimeoutMS int      `json:"timeout_ms,omitempty"`
}

// IsWriteOp 会产生外部副作用的动作（写确认闸的判定口径）。
func IsWriteOp(name string) bool {
	switch name {
	case OpClick, OpFill, OpPress, OpSelect, OpUpload:
		return true
	default:
		return false
	}
}

// ValidateOps 校验动作集合：白名单 + 必填字段 + 跨轮 ref 禁令 + 数量上限。
func ValidateOps(ops []Op, maxActions int) error {
	if maxActions <= 0 {
		maxActions = 20
	}
	if len(ops) == 0 {
		return fmt.Errorf("%w: ops 不能为空", ErrPolicyViolation)
	}
	if len(ops) > maxActions {
		return fmt.Errorf("%w: 单轮动作数 %d 超过上限 %d", ErrPolicyViolation, len(ops), maxActions)
	}
	for i, op := range ops {
		if err := validateOne(i, op); err != nil {
			return err
		}
	}
	return nil
}

func validateOne(i int, op Op) error {
	at := func(format string, args ...any) error {
		return fmt.Errorf("%w: ops[%d] %s", ErrPolicyViolation, i, fmt.Sprintf(format, args...))
	}
	switch op.Op {
	case OpGoto:
		if strings.TrimSpace(op.URL) == "" {
			return at("goto 缺少 url")
		}
		if !strings.HasPrefix(strings.ToLower(op.URL), "http://") && !strings.HasPrefix(strings.ToLower(op.URL), "https://") {
			return at("goto 只接受 http(s) URL")
		}
	case OpClick, OpFill, OpPress, OpSelect, OpUpload, OpScreenshot:
		if err := validateTarget(op.Target); err != nil {
			return at("%v", err)
		}
		if op.Op == OpFill && op.Value == "" && !op.Secret {
			return at("fill 缺少 value")
		}
		if op.Op == OpSelect && op.Value == "" && op.Key == "" {
			return at("select 缺少 value 或 key")
		}
		if op.Op == OpPress && op.Key == "" {
			return at("press 缺少 key")
		}
		if op.Op == OpUpload && op.Path == "" {
			return at("upload 缺少 path")
		}
		if op.Op == OpScreenshot && op.Scope != "" && !validScope(op.Scope) {
			return at("screenshot scope 非法（viewport/full_page）")
		}
	case OpScroll:
		if op.DY == 0 {
			return at("scroll 需要非零 dy")
		}
	case OpWaitFor:
		if op.Selector == "" && op.URL == "" && op.TimeoutMS <= 0 {
			return at("wait_for 需要 selector/url/timeout_ms 之一")
		}
		if op.Selector != "" {
			if err := validateTarget(op.Selector); err != nil {
				return at("%v", err)
			}
		}
	case OpExtract:
		if op.Target == "" && op.Selector == "" {
			return at("extract 需要 target/selector")
		}
		t := op.Target
		if t == "" {
			t = op.Selector
		}
		if err := validateTarget(t); err != nil {
			return at("%v", err)
		}
		if op.Limit < 0 || op.Limit > 200 {
			return at("extract limit 必须在 0..200")
		}
	case OpHandoff:
		// reason 可选
	case OpFinish:
		if len(op.Keep) > 10 {
			return at("finish keep 最多 10 个 Page label")
		}
	default:
		return at("未知动作 %q（闭合动作集，不允许任意脚本）", op.Op)
	}
	return nil
}

// validateTarget 拒绝跨轮 ref：ego 官方语义 ref 只对生成它的快照有效，
// 允许 `@12`/`ref=12` 会让下一轮点到随机节点（E16 实测）。
func validateTarget(t string) error {
	t = strings.TrimSpace(t)
	if t == "" {
		return fmt.Errorf("缺少 target")
	}
	lower := strings.ToLower(t)
	if strings.HasPrefix(t, "@") || strings.HasPrefix(lower, "ref=") {
		return fmt.Errorf("target 不允许使用快照 ref（%q）：ref 只对当前快照有效，跨轮请用 loc=/text=/css=/xpath=", t)
	}
	return nil
}

func validScope(s string) bool {
	switch s {
	case "viewport", "full_page", "subtree":
		return true
	default:
		return false
	}
}
