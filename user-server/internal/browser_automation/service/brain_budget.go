package service

import (
	"strings"
	"unicode/utf8"
)

// brain_budget.go — 输入预算与 UTF-8 安全截断（BRAIN_PRODUCT_SPEC P0-1）。
// 业界对照：browser-use 用字符级控制（60k 截断+逐条限长），真实 token 计数是 TODO 桩——
// 我们同样采用 rune 级预算（1 rune ≈ 1 token 的保守近似，中文场景偏保守但安全）。

const (
	// snapshotBudgetRunes 快照预算：a11y 快照 MAX_NODES=400 行，通常 <20k rune；
	// 48k 为硬上限（约 48k token 级别的输入，远低于主流模型 128k context）
	snapshotBudgetRunes = 48000
	// memoryBudgetRunes LLM 自维护记忆上限
	memoryBudgetRunes = 600
	// evaluationBudgetRunes 上轮评估上限
	evaluationBudgetRunes = 400
	// historyItemBudgetRunes 历史单条上限
	historyItemBudgetRunes = 200
)

// truncateRunes 按 rune 截断（UTF-8 安全，中文不产生乱码）。超出部分以 marker 标注。
func truncateRunes(s string, n int, marker string) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	runes := []rune(s)
	return string(runes[:n]) + marker
}

// budgetHistoryItems 历史逐条限长（组装时裁剪，不改动原 slice）
func budgetHistoryItems(history []string) []string {
	out := make([]string, len(history))
	for i, h := range history {
		out[i] = truncateRunes(h, historyItemBudgetRunes, "…")
	}
	return out
}

// snapshotTruncatedMarker 截断标注（LLM 感知到信息不完整，避免臆断）
const snapshotTruncatedMarker = "\n…[快照过长已截断，缺失部分可另用 markdown 原语获取]"

// budgetInput 组装前的统一输入预算（P0-1）：
// snapshot 48k rune / memory 600 / evaluation 400 / history 逐条 200。
// 返回裁剪后的副本；全部 UTF-8 安全。
func budgetInput(snapshot string, st *reflectState) (string, *reflectState) {
	snap := truncateRunes(snapshot, snapshotBudgetRunes, snapshotTruncatedMarker)
	if st == nil {
		return snap, nil
	}
	budgeted := &reflectState{
		History:        budgetHistoryItems(st.History),
		PrevEvaluation: truncateRunes(st.PrevEvaluation, evaluationBudgetRunes, "…"),
		Memory:         truncateRunes(st.Memory, memoryBudgetRunes, "…"),
	}
	// 防御：空串不占位（LLM 提示词里 "累积记忆：" 后跟空串会诱导空洞输出）
	if strings.TrimSpace(budgeted.PrevEvaluation) == "" {
		budgeted.PrevEvaluation = ""
	}
	if strings.TrimSpace(budgeted.Memory) == "" {
		budgeted.Memory = ""
	}
	return snap, budgeted
}
