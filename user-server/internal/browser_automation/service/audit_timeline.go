package service

import (
	"hivemtk-user/internal/browser_automation/model"
)

// audit_timeline.go — 触达 P0 Chunk 4：任务时间线可观测（每步：模型 / token / 延迟）。
//
// 修的事实：command_log 的 dur 列只记了「这一步跑了多久」，成本账只记了
// 「这个 session 跑了多少 token」，但**「这一步是哪个模型判的、烧了多少 token」无处可查**。
// 于是成本异常只能按 session 聚合地看，看不出是哪一步在烧；模型换档（比如从便宜模型
// 悄悄切到贵的）也不会在审计面上留痕。
//
// 三条取值纪律（决定了这一层能给出什么、不能给出什么）：
//  ① 延迟来自 dispatch 前后的真实时钟差，与 dur 列同源——不另起一套计时，
//     否则同一个「延迟」在审计包里有两种数，读的人无从判断信哪个。
//  ② 模型名与 token 只在 **Brain 模式**下有值（走 LLM 规划的步）。显式编排每步没有
//     模型，模型名为空就**不写字段**，不写空串——空串会被读成「模型名叫空」。
//  ③ 值仅作时间线信号，不是精确成本账：精确值看 browser_llm_plans（brain_llm_plans 表）。
//     BrainService 的 lastPlan* 是跨 session 共享的计量槽（见其字段注释），并发 session 下
//     归到具体一步本就不精确——写进审计帧是为了让时间线可读，不是为了当账本用。

// stepAuditMeta 组装每步事件帧的可观测字段：latency_ms 恒写，llm_* 仅 Brain 模式有值时写。
// 显式编排的步仍会带 latency_ms（那部分是真事实），只是没有模型与 token。
func (e *Executor) stepAuditMeta(durMs int64) map[string]any {
	meta := map[string]any{"latency_ms": durMs}
	if e.brain == nil {
		return meta
	}
	if model := e.brain.LastPlanModel(); model != "" {
		meta["llm_model"] = model
		meta["llm_tokens"] = e.brain.LastPlanTokens()
	}
	return meta
}

// mergeAuditMeta 把 stepAuditMeta 并进既有 payload（不覆盖调用方已写的同名字段）。
// 存在的意义是让每处事件帧不必各自记得「还得补可观测字段」——漏一处就是一个盲区，
// 而这类漏改在 code review 里几乎看不出来（帧照发，只是少两个字段）。
func mergeAuditMeta(payload map[string]any, meta map[string]any) map[string]any {
	if payload == nil {
		payload = map[string]any{}
	}
	for k, v := range meta {
		if _, dup := payload[k]; !dup {
			payload[k] = v
		}
	}
	return payload
}

// campaignBudgetSnapshot 活动预算的当前水位快照（供时间线/详情面板显示）。
// 未配置预算时返回 nil——调用方据此不显示这一段，而不是显示「0/0」。
func campaignBudgetSnapshot(task *model.BrowserTask) map[string]any {
	if !campaignActBudgetLimited(task) {
		return nil
	}
	return map[string]any{
		"campaign_key":        task.CampaignKey,
		"campaign_act_budget": task.CampaignActBudget,
		"campaign_act_used":   task.CampaignActUsed,
	}
}
