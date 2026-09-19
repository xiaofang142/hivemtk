package service

import (
	"errors"
	"strings"
	"testing"
)

// 批2 A1 自愈回路契约测试（手法对齐 post_comment_finalize_test.go：纯函数 + 源码静态约束，
// HostRegistry 无网络桩不做真命令帧回路）。

func TestIsSelectorMiss(t *testing.T) {
	if !isSelectorMiss(errors.New("comment_input_not_found"), "comment_input_not_found") {
		t.Error("精确 token 应命中")
	}
	if isSelectorMiss(errors.New("comment_prep_inject_timeout_15000ms"), "comment_input_not_found") {
		t.Error("inject_timeout 不是定位失效，不得命中")
	}
	if isSelectorMiss(nil, "comment_input_not_found") {
		t.Error("nil 错误不得命中")
	}
	if isSelectorMiss(errors.New("send_button_not_found_x"), "not_found_x ") {
		t.Error("空/异常输入防御")
	}
}

// TestSelfHealWiredSingleShot 静态契约：prep/send 各自只挂一次自愈+一次重下发；
// 自愈触发前置条件是精确 token；finalize 循环体内禁止出现自愈/重提交。
func TestSelfHealWiredSingleShot(t *testing.T) {
	src := readSrc(t, "executor.go")
	// prep 分支：healCommentInput 只在 commentPrep 错误处理内出现一次
	if n := strings.Count(src, "e.healCommentInput("); n != 1 {
		t.Errorf("healCommentInput 应恰好调用 1 处，got %d", n)
	}
	if n := strings.Count(src, "e.healCommentSendButton("); n != 1 {
		t.Errorf("healCommentSendButton 应恰好调用 1 处，got %d", n)
	}
	// send 自愈必须在 isInjectTimeout 早返之后（灰态判定优先于自愈）
	iInj := strings.Index(src, "isInjectTimeout(sendErr)")
	iHeal := strings.Index(src, "e.healCommentSendButton(")
	if !(0 <= iInj && iInj < iHeal) {
		t.Error("inject_timeout 早返必须先于 send 自愈（防对未判明灰态重发）")
	}
	// finalize 体内不得出现自愈/提交类调用（验证态绝不重提交）
	start := strings.Index(src, "func (e *Executor) finalizeComment")
	if start < 0 {
		t.Fatal("finalizeComment 不存在")
	}
	body := src[start:]
	if end := strings.Index(body[1:], "\nfunc "); end > 0 {
		body = body[:end+1]
	}
	for _, banned := range []string{"healComment", "commentSend(", "commentPrep("} {
		if strings.Contains(body, banned) {
			t.Errorf("finalize 体内出现 %q（验证态禁止重提交/自愈）", banned)
		}
	}
}

// TestHealTokenGate 自愈 token 门：heal 函数必须以精确 token 起手判定（其余错误零动作）。
func TestHealTokenGate(t *testing.T) {
	src := readSrc(t, "executor_selfheal.go")
	for _, pair := range [][2]string{
		{"healCommentInput", "comment_input_not_found"},
		{"healCommentSendButton", "send_button_not_found"},
	} {
		start := strings.Index(src, "func (e *Executor) "+pair[0])
		if start < 0 {
			t.Fatalf("%s 不存在", pair[0])
		}
		body := src[start:]
		if end := strings.Index(body[1:], "\nfunc "); end > 0 {
			body = body[:end+1]
		}
		firstIf := strings.Index(body, "isSelectorMiss(cause, \""+pair[1]+"\")")
		if firstIf < 0 {
			t.Errorf("%s 缺少精确 token 门 %s", pair[0], pair[1])
			continue
		}
		gate := body[firstIf:min(firstIf+80, len(body))]
		if !strings.Contains(gate, "return false") {
			t.Errorf("%s token 未命中必须立即 return false（不消耗自愈预算）", pair[0])
		}
	}
}

// TestRelocatePromptContract 自愈 prompt：按用途区分槽位、声明单次语义与快照数据属性。
func TestRelocatePromptContract(t *testing.T) {
	input := BuildRelocateSystemPrompt("comment_input")
	if !strings.Contains(input, "input_ref") || strings.Contains(input, "send_button_text") {
		t.Error("输入框 prompt 槽位错误")
	}
	btn := BuildRelocateSystemPrompt("send_button")
	if !strings.Contains(btn, "send_button_text") || !strings.Contains(btn, "button") {
		t.Error("按钮 prompt 槽位错误")
	}
	for _, p := range []string{input, btn} {
		if !strings.Contains(p, "<page_snapshot>") || !strings.Contains(p, "found") {
			t.Error("prompt 必须声明快照分隔符与 found 字段（T1 同口径 + fail-closed）")
		}
	}
}

// TestRelocateLLMNilGuard 接缝未装配时必须快败（生产 NewExecutor 已默认装配；
// 手工构造 Executor 的测试面不得静默走 nil 函数）。
func TestRelocateLLMNilGuard(t *testing.T) {
	e := &Executor{} // relocateLLM nil
	_, err := e.relocateWithSnapshot(nil, nil, nil, 0, "comment_input")
	if err == nil || !strings.Contains(err.Error(), "未装配") {
		t.Errorf("nil 接缝应快败报未装配，got %v", err)
	}
}
