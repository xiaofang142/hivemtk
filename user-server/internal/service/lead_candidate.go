package service

import (
	"strings"

	"hivemtk-user/internal/model"
)

// classifyLeadStatus 按意向分与证据完整性决定线索入库状态（D11/D12）。
//
// 高分且证据完整 → new（自动入库并通知）；
// 低分或证据不完整 → candidate（待审，不触发外部通知）；
// 非线索（jd.IsLead=false）→ 返回空，不建线索。
//
// 判据用 IntentScore 与 MinIntentScore（不是 Confidence，builder review F5 纠错）。
func classifyLeadStatus(jd *LeadJudgement, cfg *model.LeadMiningConfig) string {
	if jd == nil || !jd.IsLead {
		return ""
	}
	if cfg == nil {
		return "candidate" // 无阈值配置时保守待审，不自动放行
	}
	scoreOK := jd.IntentScore >= cfg.MinIntentScore
	evidenceOK := evidenceComplete(jd)
	if scoreOK && evidenceOK {
		return "new"
	}
	return "candidate"
}

// leadStatusRank 线索状态优先级：数字越大越靠后（已推进），只升不降。
// 覆盖前端/后端全部枚举值，避免白名单漏项导致已转化线索被打回。
func leadStatusRank(status string) int {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "candidate":
		return 0
	case "new":
		return 1
	case "notified":
		return 2
	case "following":
		return 3
	case "qualified":
		return 4
	case "converted", "won":
		return 5
	case "ignored", "invalid", "lost":
		return 6 // 终态：不再被自动判定改写
	default:
		return 1 // 未知状态按 new 处理，避免误降级
	}
}

// promoteStatus 计算更新后的线索状态：只升级不降级（D14）。
// 新判定只会把 candidate 提升为 new；已通知/跟进/转化/终态保持原状态。
func promoteStatus(current string, jd *LeadJudgement, cfg *model.LeadMiningConfig) string {
	next := classifyLeadStatus(jd, cfg)
	if next == "" {
		return current
	}
	if leadStatusRank(current) >= leadStatusRank(next) {
		return current
	}
	return next
}

// evidenceComplete 证据是否完整可自动放行。
// degraded（LLM 降级判定）或 Reason 明确标注证据不完整，均视为不完整。
func evidenceComplete(jd *LeadJudgement) bool {
	if jd == nil {
		return false
	}
	if jd.Degraded {
		return false
	}
	if strings.Contains(jd.Reason, "证据不完整") {
		return false
	}
	return true
}
