package service

import (
	"fmt"
	"strings"

	"hivemtk-user/internal/browser_automation/platform"
)

// brain_prompts.go — Brain 提示词模板工厂（对标 browser-use system_prompts/ 与 stagehand prompt.ts）。
// 原则（AGENT_BASE_PROOF.md §1.5）：
//   1. prompt 是代码资产：模板进 git、工厂函数按场景拼装，禁止散落字符串拼接；
//   2. 平台/能力知识是「prompt 片段」，由适配器注册表（L3）注入，基座零平台知识；
//   3. 输出 schema 在模板里给 LLM 讲清楚（结构化 reflect 协议，对标 browser-use AgentOutput）。

// planOutputSchema 结构化 reflect 输出协议说明（对标 browser-use AgentOutput 字段语义）
const planOutputSchema = `输出严格 JSON（一个对象，无多余文本、无 markdown 围栏）：
{
  "thinking": "当前页面状态分析（看到了什么、离目标还差什么，<=200字）",
  "evaluation_previous_goal": "上一步动作的达成评估：成功/失败/未知 + 页面证据（<=100字；首轮写\"首轮，无上一步\"）",
  "memory": "跨轮要记住的关键事实：已提取到的数据要点、页面结构发现、失败路径（<=150字；无则空串）",
  "next_goal": "本轮要达成的最小目标（一句话）",
  "done": false,
  "steps": [ {"action":"click","target":"@e3"}, {"action":"extract","selectors":{}} ]
}`

// planActionTable 缺省动作表（platformID 注入的片段可能增删此表）
const planActionTable = `可用 action（字段语义）：
- open_tab：url 放 target 字段
- click：target=CSS 或快照 @eN 引用
- type：target/value/clear_first/submit_on_enter
- post_comment：value=评论文本（一站式发评论+自动验证，优先于 click+type 组合）
- snapshot / markdown / screenshot：观察类
- wait：ms 字段；wait_for_selector：selector/timeout_ms
- scroll：direction(up|down|left|right)/amount
- extract：selectors={"键":"CSS"} 多键提取；目标数据采集首选
- assert：assert_kind(contains_text|selector_exists)/value=断言文本或 selector——目标验证首选
- query：query_kind(text|exists|count|attr)/target=selector——点击前确认元素存在
- close_tab`

// planGuardrails 行为护栏（每轮快照自动截取的固定规则）
const planGuardrails = `行为规则：
1. 每轮系统已自动打开目标页并对当前 tab 截快照——禁止输出 open_tab（会丢弃当前页面进度）；页面没加载好用 wait+snapshot。
2. click/type 的 target 优先用快照中的 @eN 引用（比 CSS 稳）。
3. 若评估发现上一步无效（页面无变化），换元素/换路径，勿原样重试。
4. done=true 时 steps 必须为空数组；done 判定要有页面证据（extract/assert 结果），不要凭猜测。
5. 页面出现拦截判据字样（见平台知识）→ done=true，memory 里写明 blocked。`

// BuildPlanSystemPrompt 编排 system prompt 工厂：schema + 动作表 + 护栏 + 平台知识片段
func BuildPlanSystemPrompt(platformID string) string {
	var b strings.Builder
	b.WriteString("你是浏览器自动化编排 Agent。你观察页面快照、评估历史动作，输出本轮 JSON 执行计划。\n\n")
	b.WriteString(planOutputSchema)
	b.WriteString("\n\n")
	b.WriteString(planActionTable)
	b.WriteString("\n\n")
	b.WriteString(planGuardrails)
	if k := PlanPlatformKnowledge(platformID); k != "" {
		b.WriteString(k)
	}
	return b.String()
}

// BuildRecoveryPrompt 第三次重试的恢复提示（JSON 抖动救回，deepseek 系思维链泄漏对策）
func BuildRecoveryPrompt() string {
	return "\n\n【重申】你的上一条回复是纯文本解释，没有 JSON。本次回复必须只包含一个 JSON 对象，" +
		"以 { 开头、以 } 结尾，不允许任何解释文字、markdown 围栏或思维链内容。"
}

// BuildLoopNudge 循环检测 nudge（对标 browser-use 循环指纹注入）：检测到原地打转时追加
func BuildLoopNudge(recentActions []string) string {
	return fmt.Sprintf(
		"\n\n【警告：检测到原地打转】最近几轮你重复输出了相同动作序列：%s。\n"+
			"这不会产生新信息。请改变策略：(a) 换用平台知识里的其它选择器；"+
			"(b) 用 markdown 原语直取页面全文；"+
			"(c) 若数据其实已在历史提取里，直接 done=true 并在 memory 里汇总。",
		strings.Join(recentActions, " → "))
}

// BuildJudgePrompt done 判定的独立校验（对标 browser-use judge）：agent 自称完成 ≠ 真完成
func BuildJudgePrompt(goal, finalState string) string {
	return fmt.Sprintf(`你是浏览器自动化任务的验收员。执行 Agent 声称已完成目标，请独立判断。
目标：%s
最终页面状态与提取证据：
%s
只输出 JSON：{"approve": true/false, "reason": "<=80字"}`,
		goal, truncate(finalState, 4096))
}

// PlanPlatformKnowledge 平台知识 prompt 片段（铁律 3：由 L3 适配器注册表注入，基座零平台知识）
func PlanPlatformKnowledge(platformID string) string {
	p, err := platform.Get(platformID)
	if err != nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n本任务平台知识（来自适配器，编排放心引用）：\n")
	b.WriteString("平台标识：" + p.Identifier() + "\n")
	caps := make([]string, 0, len(p.Capabilities()))
	for _, c := range p.Capabilities() {
		caps = append(caps, string(c))
	}
	b.WriteString("平台能力：" + strings.Join(caps, ", ") + "\n")
	if !platform.HasCapability(p, platform.CapPostComment) {
		b.WriteString("注意：本平台不支持 post_comment（能力未声明），禁止输出该 action；用 extract/query 读数据完成目标。\n")
	}
	locs := p.Locators()
	if len(locs) > 0 {
		b.WriteString("平台元素定位表（extract/query/selectors 优先用这些，都是该平台实测可用的选择器）：\n")
		for k, v := range locs {
			b.WriteString(fmt.Sprintf("  %s: %s\n", k, v))
		}
	}
	b.WriteString("平台拦截判据（页面出现这些字样即被风控，立即 done=true 并在 memory 里写明 blocked）：\n")
	b.WriteString("  " + locs["blocked_marker"] + "\n")
	return b.String()
}
