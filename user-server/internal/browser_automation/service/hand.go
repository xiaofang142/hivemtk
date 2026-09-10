package service

import (
	"context"
	"time"
)

// Hand 命令出口：往 HostRegistry 归属连接发命令帧，等 req_id 回包。
// 约束 1: 不启动任何子进程（Chrome 才是 NM Host 的父进程）
// 约束 2: 单 Host 连接内命令串行（Host 单循环），跨用户天然隔离，无全局锁
// 约束 3: 每命令带超时（默认 30s；wait_for_selector 类按步参数放宽）
type Hand struct {
	registry *HostRegistry
}

func NewHand(registry *HostRegistry) *Hand {
	return &Hand{registry: registry}
}

// ensureReady Host 在线检查
func (h *Hand) ensureReady(ctx context.Context, userID uint) error {
	return h.registry.EnsureOnline(userID)
}

// openTab 原语
func (h *Hand) openTab(ctx context.Context, userID uint, url string, active bool) (int, error) {
	res, err := h.registry.Request(ctx, userID, defaultCmdTimeout, map[string]any{
		"action": "open_tab", "url": url, "active": active,
	})
	if err != nil {
		return 0, err
	}
	tabID, _ := toInt(res["chrome_tab_id"])
	return tabID, nil
}

// click 原语
func (h *Hand) click(ctx context.Context, userID uint, tabID int, target string) error {
	_, err := h.registry.Request(ctx, userID, defaultCmdTimeout, map[string]any{
		"action": "click", "tab_id": tabID, "target": target,
	})
	return err
}

// typeText 原语
func (h *Hand) typeText(ctx context.Context, userID uint, tabID int, target, value string, clearFirst, submitOnEnter bool) error {
	_, err := h.registry.Request(ctx, userID, defaultCmdTimeout, map[string]any{
		"action": "type", "tab_id": tabID, "target": target, "value": value,
		"clear_first": clearFirst, "submit_on_enter": submitOnEnter,
	})
	return err
}

// snapshot 原语（accessibility @e{N} refs）
func (h *Hand) snapshot(ctx context.Context, userID uint, tabID int) (string, error) {
	res, err := h.registry.Request(ctx, userID, defaultCmdTimeout, map[string]any{
		"action": "snapshot", "tab_id": tabID,
	})
	if err != nil {
		return "", err
	}
	s, _ := res["snapshot"].(string)
	return s, nil
}

// markdown 原语（页面 Markdown，供 LLM 吃）
func (h *Hand) markdown(ctx context.Context, userID uint, tabID int) (string, error) {
	res, err := h.registry.Request(ctx, userID, 60*time.Second, map[string]any{
		"action": "markdown", "tab_id": tabID,
	})
	if err != nil {
		return "", err
	}
	s, _ := res["markdown"].(string)
	return s, nil
}

// screenshot 原语（M3：仅对激活 tab；扩展侧先激活再截，见设计文档 §6）
func (h *Hand) screenshot(ctx context.Context, userID uint, tabID int, activateFirst bool) (string, error) {
	res, err := h.registry.Request(ctx, userID, 30*time.Second, map[string]any{
		"action": "screenshot", "tab_id": tabID, "activate_first": activateFirst,
	})
	if err != nil {
		return "", err
	}
	b64, _ := res["base64"].(string)
	return b64, nil
}

// waitFor 固定等待
func (h *Hand) waitFor(ctx context.Context, userID uint, tabID int, ms int) error {
	_, err := h.registry.Request(ctx, userID, time.Duration(ms+5000)*time.Millisecond, map[string]any{
		"action": "wait", "tab_id": tabID, "ms": ms,
	})
	return err
}

// waitForSelector 条件等待
func (h *Hand) waitForSelector(ctx context.Context, userID uint, tabID int, selector string, timeoutMs int) error {
	_, err := h.registry.Request(ctx, userID, time.Duration(timeoutMs+10000)*time.Millisecond, map[string]any{
		"action": "wait_for_selector", "tab_id": tabID, "selector": selector, "timeout_ms": timeoutMs,
	})
	return err
}

// scroll 原语
func (h *Hand) scroll(ctx context.Context, userID uint, tabID int, direction string, amount int) error {
	_, err := h.registry.Request(ctx, userID, defaultCmdTimeout, map[string]any{
		"action": "scroll", "tab_id": tabID, "direction": direction, "amount": amount,
	})
	return err
}

// clickNear 原语：以锚元素为基准点击容器内文本含 buttonText 的 button
func (h *Hand) clickNear(ctx context.Context, userID uint, tabID int, anchor, buttonText string) error {
	_, err := h.registry.Request(ctx, userID, defaultCmdTimeout, map[string]any{
		"action": "click_near", "tab_id": tabID, "anchor": anchor, "button_text": buttonText,
	})
	return err
}

// assert 原语：断言类（contains_text / selector_exists），失败即抛错
func (h *Hand) assert(ctx context.Context, userID uint, tabID int, kind, value string, timeoutMs int) error {
	_, err := h.registry.Request(ctx, userID, time.Duration(timeoutMs)*time.Millisecond+10*time.Second, map[string]any{
		"action": "assert", "tab_id": tabID, "assert": kind, "value": value, "timeout_ms": timeoutMs,
	})
	return err
}

// query 原语：只读洞察（text/exists/count/attr），返回数据不抛错
func (h *Hand) query(ctx context.Context, userID uint, tabID int, kind, selector string) (map[string]any, error) {
	return h.registry.Request(ctx, userID, defaultCmdTimeout, map[string]any{
		"action": "query", "tab_id": tabID, "query": kind, "selector": selector,
	})
}

// postComment 原语：一站式发评论（输入+发送+验证），返回扩展回包。
// locators 来自平台适配器（L3 四件套下发，扩展零平台知识——设计稿 P5）。
func (h *Hand) postComment(ctx context.Context, userID uint, tabID int, text string, locators map[string]any) (map[string]any, error) {
	req := map[string]any{
		"action": "post_comment", "tab_id": tabID, "value": text,
	}
	for k, v := range locators {
		req[k] = v
	}
	return h.registry.Request(ctx, userID, 45*time.Second, req)
}

// extract 按 CSS selector 列表提取文本（MVP 降级形态）
func (h *Hand) extract(ctx context.Context, userID uint, tabID int, selectors map[string]string) (map[string]any, error) {
	res, err := h.registry.Request(ctx, userID, defaultCmdTimeout, map[string]any{
		"action": "extract", "tab_id": tabID, "selectors": selectors,
	})
	return res, err
}

// closeTab 原语
func (h *Hand) closeTab(ctx context.Context, userID uint, tabID int) error {
	if tabID <= 0 {
		return nil
	}
	_, err := h.registry.Request(ctx, userID, defaultCmdTimeout, map[string]any{
		"action": "close_tab", "tab_id": tabID,
	})
	return err
}

// tabExists 探测 tab 是否存活（Chrome 断开自动清理用）
func (h *Hand) tabExists(ctx context.Context, userID uint, tabID int) (bool, error) {
	res, err := h.registry.Request(ctx, userID, defaultCmdTimeout, map[string]any{
		"action": "tab_exists", "tab_id": tabID,
	})
	if err != nil {
		return false, err
	}
	exists, _ := res["exists"].(bool)
	return exists, nil
}

const defaultCmdTimeout = 30 * time.Second

// toInt 宽松数字转换（JSON 解出的 float64）
func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	default:
		return 0, false
	}
}
