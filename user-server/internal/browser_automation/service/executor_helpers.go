package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"time"

	"hivemtk-user/internal/browser_automation/dto"
	"hivemtk-user/internal/browser_automation/model"
	"hivemtk-user/internal/browser_automation/platform"
)

// executor_helpers.go —— 纯函数与小型工具：错误分类、步骤参数、历史压缩、可中断睡眠等。
// sendErrText 提交命令错误的落库文本（nil→空串）
func sendErrText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// isInjectTimeout 识别扩展侧竞速错误（*_inject_timeout_*ms，见 primitives.js raceTimeout）。
// 语义=注入从未在页面执行→该命令零副作用，与「执行了但失败」「WS 超时结果未知」三分归一。
func isInjectTimeout(err error) bool {
	return err != nil && strings.Contains(err.Error(), "_inject_timeout_")
}

// isSendGateReject 扩展侧 comment_send 的可点性闸门在把坐标交给 CDP **之前**就把按钮判死
// （send_button_not_interactable: covered/zero_box/disabled，见 primitives.js injPostCommentSend）。
// 与注入超时同属「零副作用」一类：页面从未收到那次点击，所以台账停在 prepared、
// 不进 finalize 白轮、不自愈重发（换文本重选对一个「存在但不可点」的按钮没有依据）。
func isSendGateReject(err error) bool {
	return err != nil && strings.Contains(err.Error(), "send_button_not_interactable")
}

// isNeverDispatched 扩展侧 CDP **attach 阶段**就没成功 ⇒ 一条输入事件都没下发过 ⇒ 零副作用。
//
// 为什么必须单独一类：cdp/input.js 的 withDebugger 是「先 attach、再跑 fn(target)」。
// attach 撞自己的 deadline（cdp/input.js 的 CDP_ATTACH_DEADLINE_MS）时 fn 压根没被调用，
// 页面上不可能发生过点击/键入——它与 cdp_send_deadline / click_unacked / cdp_command_deadline
// 那一族有本质区别：后三者都意味着**事件已入队进渲染进程**（结局未知，不可重发）。
//
// 漏判的代价是实打实的双发闸误伤：若把它当「结果未知」，台账落 unattributed，
// 该文本从此进拦阻集合（write_ledger.go guardResubmit 连 unattributed 一起拦），
// 用户看清页面重跑时会被告知「已有提交尝试」——而实际一次都没发出去。
func isNeverDispatched(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	// 只认 attach 两处；send/command 那一族是「已入队」，绝不能并进来。
	return strings.Contains(msg, "cdp_attach_deadline")
}

func buildStepParams(step parsedStep) StepParams {
	p := StepParams{
		Ms:            step.Ms,
		ClearFirst:    step.ClearFirst,
		SubmitOnEnter: step.SubmitOnEnter,
		Direction:     step.Direction,
		Amount:        step.Amount,
		Selector:      step.Selector,
		TimeoutMs:     step.TimeoutMs,
		Selectors:     step.Selectors,
		Anchor:        step.Anchor,
		ButtonText:    step.ButtonText,
		AssertKind:    step.AssertKind,
		QueryKind:     step.QueryKind,
		Attribute:     step.Attribute,
	}
	return p
}

// taskPlatformID 任务归属平台：task.platform 列是权威（R20 起为显式字段）。
// B21：**不兜底**。早先空值回落 xiaohongshu 是 fail-open——漏填平台的抖音任务会
// 静默拿小红书的定位表去点抖音页面，与 P4「未声明能力即报错」方向相反。
// 现在空值原样返回 ""，交由下游 platform.Get("") 报错（执行面 fails-loudly）；
// 入口侧另有门把它拦住：Create/Update 走 validateTaskPlatform（必填+已注册），
// Publish/RunTask 走 requireTaskPlatform（必填）。
func taskPlatformID(task *model.BrowserTask) string {
	return strings.TrimSpace(task.Platform)
}

// ParseSteps 编排 JSON → parsedStep 数组（task.steps 为空返回空切片）
func ParseSteps(raw []byte) ([]parsedStep, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var items []dto.StepItem
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("steps JSON 解析失败: %w", err)
	}
	out := make([]parsedStep, 0, len(items))
	for _, it := range items {
		out = append(out, parsedStep{
			StepItem:        it,
			ContinueOnError: it.ContinueOnError,
			RetryCount:      it.RetryCount,
			RetryBackoffMs:  it.RetryBackoffMs,
		})
	}
	return out, nil
}

// sleepInterruptible 可被 ctx 取消 / stopCh 打断的睡眠；返回 false 表示被中断
func sleepInterruptible(ctx context.Context, stopCh chan struct{}, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-stopCh:
		return false
	case <-t.C:
		return true
	}
}

// humanizedDelay 步间节奏抖动（铁律 2 模拟人工）：base ±30% 均匀随机。
// 机器执行步间间隔恒定是可检测的自动化特征；真人在页面停留时间是长尾分布。
// base<=0 返回 0（不引入计划外等待）。
func humanizedDelay(base int) time.Duration {
	if base <= 0 {
		return 0
	}
	span := int(float64(base) * 0.3)
	if span <= 0 {
		return time.Duration(base) * time.Millisecond
	}
	jitter := rand.Intn(2*span+1) - span // [-span, span]
	return time.Duration(base+jitter) * time.Millisecond
}

// lastN 取 history 末 n 条（judge 摘要用）
func lastN(s []string, n int) []string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// ---- 历史压缩：滑动窗口 + 确定性折叠台账 ----
// browser-use 用额外 LLM 调用压缩历史（25 步/40k→6k）；我们取等价语义的零成本形态：
// 溢出窗口的最旧条目不静默丢弃，而是计入折叠台账（按动作计数），台账首行喂 LLM——
// 保留"总共做过什么"的构成信息（防重复无效动作），逐条细节永久可查 command_log。

// historyWindow Brain 历史滑动窗口容量
const historyWindow = 24

// appendHistoryBounded 容量约束下追加历史：溢出时最旧一条计入 folded 台账（键=动作名）。
// folded 允许为 nil（judge/摘要等只读路径复用本函数记账时不折叠）。
func appendHistoryBounded(history []string, entry string, folded map[string]int) []string {
	if len(history) >= historyWindow {
		oldest := history[0]
		history = history[1:]
		if folded != nil {
			folded[historyActionOf(oldest)]++
		}
	}
	return append(history, entry)
}

// historyActionOf 提取存量行动作词（行格式 "<action> <target>[ → 状态]"；折叠行以 "[" 前缀开头不计动作）
func historyActionOf(line string) string {
	if strings.HasPrefix(line, "[") {
		return "compacted"
	}
	if i := strings.IndexByte(line, ' '); i > 0 {
		return line[:i]
	}
	return line
}

// buildFoldHeader 折叠台账首行，如 "[已折叠 37 步: click×21 type×9 extract×7]"；台账为空返回 ""。
func buildFoldHeader(folded map[string]int) string {
	if len(folded) == 0 {
		return ""
	}
	total := 0
	keys := make([]string, 0, len(folded))
	for k, v := range folded {
		total += v
		if v > 0 {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s×%d", k, folded[k]))
	}
	return fmt.Sprintf("[已折叠 %d 步: %s]", total, strings.Join(parts, " "))
}

// stepChangedPage F6a 证据判定：open_tab 必然换页；click 以扩展回包 navigated 为准；
// click_near/type 无换页证据不截断（保守——误截只多一轮快照，不错截）。
func stepChangedPage(action string, result json.RawMessage) bool {
	if action == "open_tab" {
		return true
	}
	if action == "click" && len(result) > 0 {
		var r struct {
			Navigated bool `json:"navigated"`
		}
		if json.Unmarshal(result, &r) == nil {
			return r.Navigated
		}
	}
	return false
}

// stepErrRetryable 步失败是否值得再试（fail-closed）：
// 仅显式归因 retry（瞬态：超时/元素未就绪）继续退避重试；bad_body/refresh_token/
// disconnect 终止（语义同前）；ErrUnknown（判据未命中）同样终止——未知错误盲目重试
// 会把归因缺口放大成风控信号，正确路径是打点归因后显式扩表（AWS SDK「全 Unknown 不重试」语义）。
// 平台未注册时维持兼容放行（无判据可用，不归 fail-closed 范畴）。
func stepErrRetryable(platformID, errText string) bool {
	p, err := platform.Get(platformID)
	if err != nil {
		return true
	}
	return p.ClassifyError(errText) == platform.ErrRetry
}
