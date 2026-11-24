package service

import (
	"strings"
	"testing"

	"hivemtk-user/internal/model"
)

// P0-b 回归：群聊真实多成员历史必须进入判定消息，不被只含当前一条的共享缓存覆盖。
func TestMergeJudgeMessages_GroupMultiMemberHistoryWins(t *testing.T) {
	history := []DetectionMessage{
		{MessageID: "history_0", Content: "[张三] 谁有现货？", Role: "user", SenderID: "u1"},
		{MessageID: "history_1", Content: "[李四] 我有工厂现货", Role: "user", SenderID: "u2"},
		{MessageID: "history_2", Content: "[王五] 的介绍费怎么算", Role: "user", SenderID: "u3"},
	}
	shared := model.ConversationMessages{{MessageID: "m_current", Role: "user", SenderID: "u1", Content: "这个能不能做"}}
	current := DetectionMessage{MessageID: "m_current", Content: "这个能不能做", Role: "user", SenderID: "u1"}

	got := mergeJudgeMessages(history, shared, current)
	if len(got) != 4 {
		t.Fatalf("期望 3 条历史 + 1 条当前，实际 %d 条：%+v", len(got), got)
	}
	senders := map[string]bool{}
	for _, m := range got {
		senders[m.SenderID] = true
	}
	for _, want := range []string{"u1", "u2", "u3"} {
		if !senders[want] {
			t.Fatalf("群成员 %s 的历史发言丢失：%+v", want, got)
		}
	}
	if !strings.Contains(got[1].Content, "李四") {
		t.Fatalf("群内第二条发言内容丢失：%+v", got[1])
	}
}

func TestMergeJudgeMessages_RealMsgIDDedup(t *testing.T) {
	history := []DetectionMessage{{MessageID: "history_0", Content: "历史消息", Role: "user"}}
	shared := model.ConversationMessages{{MessageID: "real_msg_1", Role: "user", SenderID: "u1", Content: "共享补充"}}
	current := DetectionMessage{MessageID: "real_msg_1", Content: "共享补充", Role: "user", SenderID: "u1"}
	if got := mergeJudgeMessages(history, shared, current); len(got) != 2 {
		t.Fatalf("真实 MsgID 应去重：期望 2 条，实际 %d 条 %+v", len(got), got)
	}
}

func TestRoleFromEnrichedContentAndStrip(t *testing.T) {
	if got := roleFromEnrichedContent("[demand:u1] 求购现货"); got != "demand" {
		t.Fatalf("角色提取期望 demand，实际 %s", got)
	}
	if got := stripRolePrefix("[demand:u1] 求购现货"); got != "求购现货" {
		t.Fatalf("前缀剥离期望原文，实际 %s", got)
	}
	if got := roleFromEnrichedContent("无前缀原文"); got != "" {
		t.Fatalf("无前缀不应提取角色，实际 %s", got)
	}
}

// P0-d / D8：历史摘要必须进入判定提示，且声明冲突时以逐条消息为准。
func TestBuildLeadJudgeSystemPrompt_HistorySummaryIncluded(t *testing.T) {
	jctx := &LeadJudgeContext{Conversation: &model.ConversationContext{Summary: "客户先问价格，后表示要样品"}}
	prompt := buildLeadJudgeSystemPrompt(jctx)
	if !strings.Contains(prompt, "客户先问价格，后表示要样品") || !strings.Contains(prompt, "以逐条消息为准") {
		t.Fatalf("历史摘要或冲突裁决说明未进入提示：%s", prompt)
	}
}

func TestBuildLeadJudgeSystemPrompt_GroupRichContext(t *testing.T) {
	jctx := &LeadJudgeContext{
		IsGroup: true, GroupID: "g_001",
		GroupRoleCounts: map[string]int{"demand": 2, "supply": 1},
	}
	prompt := buildLeadJudgeSystemPrompt(jctx)
	if !strings.Contains(prompt, "【群成员发言统计】") || !strings.Contains(prompt, "demand=2") {
		t.Fatalf("群聊提示缺少成员统计：%s", prompt)
	}
}

func TestBuildLeadJudgeSystemPrompt_SingleChatNoRoleStats(t *testing.T) {
	prompt := buildLeadJudgeSystemPrompt(&LeadJudgeContext{IsGroup: false})
	if strings.Contains(prompt, "群成员发言统计") {
		t.Fatalf("单聊提示不应出现群成员统计：%s", prompt)
	}
}

func TestBuildLeadJudgeSystemPrompt_GroupAggregateFailedMarksIncomplete(t *testing.T) {
	prompt := buildLeadJudgeSystemPrompt(&LeadJudgeContext{IsGroup: true, GroupAggregateFailed: true})
	if !strings.Contains(prompt, "证据不完整") {
		t.Fatalf("聚合失败提示必须标注证据不完整：%s", prompt)
	}
}
