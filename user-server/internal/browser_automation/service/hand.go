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

// openTab 原语。回包一并返回：批9a 起扩展侧会等页面加载完成，
// loaded 是「这一步到底读到了没有」的审计事实，不能只留个 tab id 就当成功。
func (h *Hand) openTab(ctx context.Context, userID uint, url string, active bool) (int, map[string]any, error) {
	res, err := h.registry.Request(ctx, userID, defaultCmdTimeout, map[string]any{
		"action": "open_tab", "url": url, "active": active,
	})
	if err != nil {
		return 0, nil, err
	}
	tabID, _ := toInt(res["chrome_tab_id"])
	return tabID, res, nil
}

// click 原语。回包含扩展侧 injClick 的 navigated 标志（是否发生页面跳转）——
// F6a 轮内截断的证据来源（browser-use「页面变即截断剩余动作」语义，零额外往返）。
// verifyIdentity（批17(b)）：是否请求「trusted 真点之后的身份复核」。这个开关只有写步该付——
// 复核是一次额外的页内注入，且它的产物 element_moved 对读步毫无意义（读步 retries 还在，
// 一次布局抖动就会被记成失败）。请求侧在 Go、执行侧在扩展，所以它必须出现在帧上。
func (h *Hand) click(ctx context.Context, userID uint, tabID int, target string, verifyIdentity bool) (map[string]any, error) {
	res, err := h.registry.Request(ctx, userID, defaultCmdTimeout, map[string]any{
		"action": "click", "tab_id": tabID, "target": target,
		"verify_identity": verifyIdentity,
	})
	if err != nil {
		return nil, err
	}
	if res == nil {
		res = map[string]any{}
	}
	return res, nil
}

// typeText 原语。回包必须交回上层：批14 起 type 也带 channel（trusted 键入 vs DOM 兜底），
// 早前的 `) error` 签名把整个回包丢在 hand 层，降级在审计面上完全不可见。
func (h *Hand) typeText(ctx context.Context, userID uint, tabID int, target, value string, clearFirst, submitOnEnter bool) (map[string]any, error) {
	res, err := h.registry.Request(ctx, userID, defaultCmdTimeout, map[string]any{
		"action": "type", "tab_id": tabID, "target": target, "value": value,
		"clear_first": clearFirst, "submit_on_enter": submitOnEnter,
	})
	if err != nil {
		return nil, err
	}
	if res == nil {
		res = map[string]any{}
	}
	return res, nil
}

// snapshot 原语（accessibility @e{N} refs）。
// A2（批2）：回包同时携带 page_url——拦截判据的 URL 层此前无数据可用（快照不含 URL，
// website-login/error 等判据永不命中）。url 为空表示旧版扩展未回传（fail-soft 降级）。
func (h *Hand) snapshot(ctx context.Context, userID uint, tabID int) (text, pageURL string, err error) {
	res, err := h.registry.Request(ctx, userID, defaultCmdTimeout, map[string]any{
		"action": "snapshot", "tab_id": tabID,
	})
	if err != nil {
		return "", "", err
	}
	s, _ := res["snapshot"].(string)
	u, _ := res["url"].(string)
	return s, u, nil
}

// resolveRef 把快照 @eN 引用解析回 CSS selector（A1 selector 自愈回路用：
// LLM 依据快照行选 ref，Go 拿 ref 换真实定位）。ref 已失效（页面导航/新快照重置）返回空串。
func (h *Hand) resolveRef(ctx context.Context, userID uint, tabID int, ref string) (string, error) {
	res, err := h.registry.Request(ctx, userID, defaultCmdTimeout, map[string]any{
		"action": "resolve_ref", "tab_id": tabID, "ref": ref,
	})
	if err != nil {
		return "", err
	}
	sel, _ := res["selector"].(string)
	return sel, nil
}

// markdown 原语（页面 Markdown，供 LLM 吃）
func (h *Hand) markdown(ctx context.Context, userID uint, tabID int) (map[string]any, error) {
	res, err := h.registry.Request(ctx, userID, handMarkdownTimeout, map[string]any{
		"action": "markdown", "tab_id": tabID,
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// screenshot 原语（M3：仅对激活 tab；扩展侧先激活再截，见设计文档 §6）
func (h *Hand) screenshot(ctx context.Context, userID uint, tabID int, activateFirst bool) (string, error) {
	res, err := h.registry.Request(ctx, userID, defaultCmdTimeout, map[string]any{
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
	_, err := h.registry.Request(ctx, userID, time.Duration(ms)*time.Millisecond+handWaitGrace, map[string]any{
		"action": "wait", "tab_id": tabID, "ms": ms,
	})
	return err
}

// waitForSelector 条件等待
func (h *Hand) waitForSelector(ctx context.Context, userID uint, tabID int, selector string, timeoutMs int) error {
	_, err := h.registry.Request(ctx, userID, time.Duration(timeoutMs)*time.Millisecond+handConditionGrace, map[string]any{
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

// clickNear 原语：以锚元素为基准点击容器内文本含 buttonText 的 button。
// 回包同 typeText 交回上层（channel 审计面，批14）。verifyIdentity 同 click（批17(b)）：
// 这一步的定位是「锚点+文本」现场算出来的，复核要拿 probe 回传的 selector 再解析一次。
func (h *Hand) clickNear(ctx context.Context, userID uint, tabID int, anchor, buttonText string, verifyIdentity bool) (map[string]any, error) {
	res, err := h.registry.Request(ctx, userID, defaultCmdTimeout, map[string]any{
		"action": "click_near", "tab_id": tabID, "anchor": anchor, "button_text": buttonText,
		"verify_identity": verifyIdentity,
	})
	if err != nil {
		return nil, err
	}
	if res == nil {
		res = map[string]any{}
	}
	return res, nil
}

// assert 原语：断言类（contains_text / selector_exists），失败即抛错
func (h *Hand) assert(ctx context.Context, userID uint, tabID int, kind, value string, timeoutMs int) error {
	_, err := h.registry.Request(ctx, userID, time.Duration(timeoutMs)*time.Millisecond+handConditionGrace, map[string]any{
		"action": "assert", "tab_id": tabID, "assert": kind, "value": value, "timeout_ms": timeoutMs,
	})
	return err
}

// query 原语：只读洞察（text/exists/count/attr），返回数据不抛错。
// attr 子类型必须带 attribute（属性名）——D2 三层贯通：dto→StepParams→本帧→扩展 injQuery。
func (h *Hand) query(ctx context.Context, userID uint, tabID int, kind, selector, attribute string) (map[string]any, error) {
	cmd := map[string]any{
		"action": "query", "tab_id": tabID, "query": kind, "selector": selector,
	}
	if attribute != "" {
		cmd["attribute"] = attribute
	}
	return h.registry.Request(ctx, userID, defaultCmdTimeout, cmd)
}

// F2②（G11 正确版）三段式子命令：扩展侧零状态、零编排知识，提交与验证分离。
// 旧一站式 Hand.postComment 已随 Executor 切三段式删除（扩展侧 v1.4.0 起 post_comment
// 协议动作也已移除，comment_prep/send/verify 是唯一路径，防双路径分叉）。
// 契约：comment_prep / comment_verify 可安全重复（只读定位+可重入键入/只读检查）；
// comment_send = 不可逆提交点，全链路只允许发生一次（服务端步级禁重试 F2① 继续兜底）。

// commentPrep 阶段一：定位评论输入框+聚焦+（contenteditable 走 CDP trusted 键入）注入文字
func (h *Hand) commentPrep(ctx context.Context, userID uint, tabID int, text string, locators map[string]any) (map[string]any, error) {
	req := map[string]any{"action": "comment_prep", "tab_id": tabID, "value": text}
	for k, v := range locators {
		req[k] = v
	}
	return h.registry.Request(ctx, userID, defaultCmdTimeout, req)
}

// commentSend 阶段二：定位发送按钮坐标 + CDP trusted 点击（唯一不可逆点，禁重试）。
// 超时预算对齐旧一站式 post_comment=45s：重页（小红书评论区渲染）上 CDP 事件逐条
// round-trip 可达秒级，30s 实测触发假超时（session179：发送实际成功但回包迟于超时）。
// 真机实证教训：发送结果未知时归因交 finalize 回查，不重发。
func (h *Hand) commentSend(ctx context.Context, userID uint, tabID int, locators map[string]any) (map[string]any, error) {
	req := map[string]any{"action": "comment_send", "tab_id": tabID}
	for k, v := range locators {
		req[k] = v
	}
	return h.registry.Request(ctx, userID, handCommentSendTimeout, req)
}

// commentVerify 阶段三：只读验证评论渲染（finalize 轮询单元，短超时多次调用）
func (h *Hand) commentVerify(ctx context.Context, userID uint, tabID int, text, containerSel, itemSel string, timeoutMs int) (map[string]any, error) {
	return h.registry.Request(ctx, userID, time.Duration(timeoutMs)*time.Millisecond+handConditionGrace, map[string]any{
		"action": "comment_verify", "tab_id": tabID, "value": text,
		"comment_container": containerSel, "comment_item_text": itemSel, "timeout_ms": timeoutMs,
	})
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

// G9 死代码清扫（R25）：Hand.tabExists（远程 tab_exists 命令）全仓零调用方已删除；
// 扩展侧 tab_exists case 保留——dispatch 内部用它做 tab 存活闸门（那是本地函数，不走命令帧）。

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
