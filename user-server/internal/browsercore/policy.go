package browsercore

import (
	"fmt"
	"strings"
)

// DefaultPolicy 通用能力的保守缺省（所有上限都由服务端定，客户端不可协商）。
func DefaultPolicy() Policy {
	return Policy{
		WriteConfirm:  "require",
		ConfirmTTLSec: 300,
		TimeoutSec:    300,
		MaxRounds:     30,
		MaxActions:    20,
	}
}

// Normalize 填缺省 + 归一（engine/mode 在 Open 处归一，这里只处理策略）。
func (p *Policy) Normalize() {
	d := DefaultPolicy()
	if p.WriteConfirm == "" {
		p.WriteConfirm = d.WriteConfirm
	}
	if p.ConfirmTTLSec <= 0 {
		p.ConfirmTTLSec = d.ConfirmTTLSec
	}
	if p.TimeoutSec <= 0 {
		p.TimeoutSec = d.TimeoutSec
	}
	if p.MaxRounds <= 0 {
		p.MaxRounds = d.MaxRounds
	}
	if p.MaxActions <= 0 {
		p.MaxActions = d.MaxActions
	}
	// 上限不给客户端协商空间（既有 task.go / egolite 执行域同纪律）
	if p.ConfirmTTLSec > 3600 {
		p.ConfirmTTLSec = 3600
	}
	if p.TimeoutSec > 3600 {
		p.TimeoutSec = 3600
	}
	if p.MaxRounds > 200 {
		p.MaxRounds = 200
	}
	if p.MaxActions > 100 {
		p.MaxActions = 100
	}
	out := make([]string, 0, len(p.Domains))
	for _, d := range p.Domains {
		d = strings.ToLower(strings.TrimSpace(d))
		if d == "" {
			continue
		}
		out = append(out, d)
	}
	p.Domains = out
}

// Validate 策略合法性。Domains 为空 = 拒绝一切导航（fail-close）——与 runner 侧
// __DOMAINS__ 空数组全拒同口径，不是"不限制"。
func (p Policy) Validate() error {
	if len(p.Domains) == 0 {
		return fmt.Errorf("%w: domains 必填（空表示拒绝一切导航）；确需任意站点请显式写 \"*\"", ErrPolicyViolation)
	}
	if p.WriteConfirm != "none" && p.WriteConfirm != "require" {
		return fmt.Errorf("%w: write_confirm 必须为 none 或 require", ErrPolicyViolation)
	}
	if p.ConfirmTTLSec <= 0 || p.ConfirmTTLSec > 3600 {
		return fmt.Errorf("%w: confirm_ttl_sec 必须在 1..3600", ErrPolicyViolation)
	}
	if p.TimeoutSec <= 0 || p.TimeoutSec > 3600 {
		return fmt.Errorf("%w: timeout_sec 必须在 1..3600", ErrPolicyViolation)
	}
	if p.MaxRounds <= 0 || p.MaxRounds > 200 {
		return fmt.Errorf("%w: max_rounds 必须在 1..200", ErrPolicyViolation)
	}
	if p.MaxActions <= 0 || p.MaxActions > 100 {
		return fmt.Errorf("%w: max_actions 必须在 1..100", ErrPolicyViolation)
	}
	return nil
}

// RequiresConfirm 写动作是否必须逐步确认。
// 策略写 require ⇒ 强制；策略写 none ⇒ 不强制（但调用方声明写动作时仍会被上层改成 require）。
func (p Policy) RequiresConfirm() bool { return p.WriteConfirm == "require" }

// DomainAllowed 与 runner 侧 __domainAllowed__ 同源语义：整站 * 放行；
// 精确域或子域命中；www. 归一。
func DomainAllowed(domains []string, rawURL string) bool {
	host := hostOf(rawURL)
	if host == "" {
		return false
	}
	for _, d := range domains {
		d = strings.ToLower(strings.TrimSpace(d))
		if d == "*" {
			return true
		}
		d = strings.TrimPrefix(d, "www.")
		if d != "" && (host == d || strings.HasSuffix(host, "."+d)) {
			return true
		}
	}
	return false
}

func hostOf(rawURL string) string {
	u := strings.ToLower(strings.TrimSpace(rawURL))
	for _, p := range []string{"https://", "http://"} {
		if strings.HasPrefix(u, p) {
			u = u[len(p):]
			break
		}
	}
	if i := strings.IndexAny(u, "/?#"); i >= 0 {
		u = u[:i]
	}
	if i := strings.LastIndex(u, "@"); i >= 0 {
		u = u[i+1:]
	}
	if i := strings.Index(u, ":"); i >= 0 {
		u = u[:i]
	}
	return strings.TrimSuffix(u, ".")
}
