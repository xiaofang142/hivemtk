// executor_selfheal.go — A1 定位自愈回路（批2，Stagehand selfHeal 同型）。
//
// 触发：扩展 comment_prep/comment_send 定位原语失败，回包携带结构化 token
// （primitives.js：comment_input_not_found / send_button_not_found）。
// 回路：拍 a11y 快照 → LLM 按快照行选新定位（@eN ref / 按钮文本）→ resolve_ref
// 换回 CSS → 调用方以修正后的 prepReq 重下发。
//
// 预算与副作用纪律：
//   - 每段只自愈一次、不回写适配器选择器表（防污染，同 Stagehand「只回写缓存」）；
//   - 仅认精确 token：*_not_found 意味着元素从未命中=注入点击从未发生=无副作用，
//     重下发安全（R26-2 归因矩阵）；inject_timeout/其余错误一律不自愈不重发。
package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/browser_automation/model"
	"hivemtk-user/internal/pkg/utils/logger"
)

// relocateOutcome LLM 重定位结构化输出（prompt 见 BuildRelocateSystemPrompt）
type relocateOutcome struct {
	Found          bool   `json:"found"`
	InputRef       string `json:"input_ref"`
	SendButtonText string `json:"send_button_text"`
}

// isSelectorMiss 定位失败结构化 token 判定（扩展侧 primitives.js 抛出的机器可读错误名）
func isSelectorMiss(err error, token string) bool {
	return err != nil && strings.Contains(err.Error(), token)
}

// defaultRelocateLLM 生产实现：走全局 LLM 分发器。
// 调用级看门狗同 brain.planOnce（R22 实测：execCtx 取消在部分调用栈不生效）——
// 120s 真实时钟强返，防自愈回路挂死整个 session。
func defaultRelocateLLM(ctx context.Context, systemPrompt, prompt string) (relocateOutcome, error) {
	var out relocateOutcome
	req := llm.DispatchRequest{
		Scenario:     llm.ScenarioHighQuality,
		SystemPrompt: systemPrompt,
		Prompt:       prompt,
		JSONMode:     true,
		MaxTokens:    1024,
	}
	ch := make(chan error, 1)
	go func() {
		_, err := llm.GetGlobalDispatcher().DispatchStructured(ctx, req, &out)
		ch <- err
	}()
	timer := time.NewTimer(relocateLLMWatchdog)
	defer timer.Stop()
	select {
	case err := <-ch:
		return out, err
	case <-timer.C:
		return out, fmt.Errorf("relocate LLM 看门狗超时 %ds", int(relocateLLMWatchdog.Seconds()))
	case <-ctx.Done():
		return out, ctx.Err()
	}
}

// relocateWithSnapshot 公共段：拍快照 + LLM 选位。失败（快照拿不到/LLM 报错/未找到）
// 一律返回零值 outcome——自愈是增强不是闸门，原错误由调用方继续上抛。
func (e *Executor) relocateWithSnapshot(ctx context.Context, task *model.BrowserTask, session *model.BrowserSession, tabID int, purpose string) (relocateOutcome, error) {
	if e.relocateLLM == nil {
		return relocateOutcome{}, fmt.Errorf("自愈 LLM 接缝未装配")
	}
	snap, _, err := e.hand.snapshot(ctx, task.UserID, tabID)
	if err != nil || snap == "" {
		return relocateOutcome{}, fmt.Errorf("自愈前置快照失败: %w", err)
	}
	var b strings.Builder
	b.WriteString("失效的自动化选择器正在把页面 a11y 快照交给你，请按 system 指令选出新定位。\n")
	b.WriteString(snapshotOpen + truncate(snap, 12000) + snapshotClose)
	out, err := e.relocateLLM(ctx, BuildRelocateSystemPrompt(purpose), b.String())
	if err != nil {
		return out, err
	}
	if !out.Found {
		return out, fmt.Errorf("LLM 判定无匹配元素")
	}
	return out, nil
}

// healCommentInput comment_prep 输入框定位失败 → LLM 选 input ref → resolve_ref 换 CSS
// 写回 prepReq["input_selector"]。返回 true 才允许重下发 prep。
func (e *Executor) healCommentInput(ctx context.Context, task *model.BrowserTask, session *model.BrowserSession, tabID int, prepReq map[string]any, cause error) bool {
	if !isSelectorMiss(cause, "comment_input_not_found") {
		return false
	}
	out, err := e.relocateWithSnapshot(ctx, task, session, tabID, "comment_input")
	if err != nil {
		logger.Warnf("[BrowserExec] A1 输入框自愈失败 session=%d: %v", session.ID, err)
		return false
	}
	css, err := e.hand.resolveRef(ctx, task.UserID, tabID, strings.TrimSpace(out.InputRef))
	if err != nil || css == "" {
		logger.Warnf("[BrowserExec] A1 输入框自愈 ref 解析失败 session=%d ref=%q: %v", session.ID, out.InputRef, err)
		return false
	}
	logger.Warnf("[BrowserExec] A1 输入框自愈生效 session=%d：失效选择器 → %q（单次，不回写）", session.ID, css)
	prepReq["input_selector"] = css
	return true
}

// healCommentSendButton comment_send 按钮定位失败 → LLM 选按钮文本写回 prepReq。
// send_button_not_found=按钮从未被找到=点击从未发生，重发不违「单次提交禁重试」红线。
func (e *Executor) healCommentSendButton(ctx context.Context, task *model.BrowserTask, session *model.BrowserSession, tabID int, prepReq map[string]any, cause error) bool {
	if !isSelectorMiss(cause, "send_button_not_found") {
		return false
	}
	out, err := e.relocateWithSnapshot(ctx, task, session, tabID, "send_button")
	if err != nil {
		logger.Warnf("[BrowserExec] A1 发送按钮自愈失败 session=%d: %v", session.ID, err)
		return false
	}
	txt := strings.TrimSpace(out.SendButtonText)
	if txt == "" {
		logger.Warnf("[BrowserExec] A1 发送按钮自愈返回空文本 session=%d", session.ID)
		return false
	}
	logger.Warnf("[BrowserExec] A1 发送按钮自愈生效 session=%d：发送按钮文本 → %q（单次，不回写）", session.ID, txt)
	prepReq["send_button_text"] = txt
	return true
}
