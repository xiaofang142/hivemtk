package service

import (
	"context"
	"strings"
	"testing"

	"hivemtk-user/internal/aiagent/llm"
)

// D18：指代型消息触发 ReAct 取证；工具失败/白名单外都 fail-visible 降级。
func TestRunLeadReactLoop_ToolEvidenceAndFallback(t *testing.T) {
	resolver := LeadReactResolver{
		HistoricalLeads: func(_ context.Context, _ string) (string, error) {
			return "上次判定：供货线索，意向 75", nil
		},
	}
	tools := defaultLeadReactTools(resolver)
	round := 0
	dispatch := func(_ context.Context, _ []llm.ChatMessage, defs []llm.ToolDefinition) (string, error) {
		round++
		if round == 1 {
			if len(defs) != 3 {
				t.Fatalf("白名单工具数期望 3，实际 %d", len(defs))
			}
			return "Thought: 指代不明\nAction: query_historical_leads\nAction Input: {\"query\":\"u1\"}", nil
		}
		return "Thought: 已取证\nFinal Answer: 构成供货线索", nil
	}
	collected, degraded, err := runLeadReactLoop(context.Background(), tools, dispatch)
	if err != nil || degraded {
		t.Fatalf("正常取证期望不降级：err=%v degraded=%v", err, degraded)
	}
	if len(collected) != 1 || !strings.Contains(collected[0], "75") {
		t.Fatalf("工具证据未收集：%+v", collected)
	}
}

func TestRunLeadReactLoop_ToolFailureDegradesVisible(t *testing.T) {
	tools := defaultLeadReactTools(LeadReactResolver{}) // 未装配 → 工具不可用
	dispatch := func(_ context.Context, _ []llm.ChatMessage, _ []llm.ToolDefinition) (string, error) {
		return "Action: query_customer_360\nAction Input: {\"query\":\"u1\"}", nil
	}
	collected, degraded, err := runLeadReactLoop(context.Background(), tools, dispatch)
	if err != nil {
		t.Fatalf("工具失败不应返回硬错误：%v", err)
	}
	if !degraded || len(collected) == 0 || !strings.Contains(collected[0], "工具失败") {
		t.Fatalf("工具失败必须可见并降级：degraded=%v collected=%+v", degraded, collected)
	}
}

func TestRunLeadReactLoop_ToolNotInWhitelist(t *testing.T) {
	tools := defaultLeadReactTools(LeadReactResolver{})
	dispatch := func(_ context.Context, _ []llm.ChatMessage, _ []llm.ToolDefinition) (string, error) {
		return "Action: delete_all\nAction Input: {}", nil
	}
	collected, degraded, _ := runLeadReactLoop(context.Background(), tools, dispatch)
	if !degraded || len(collected) == 0 || !strings.Contains(collected[0], "不在白名单") {
		t.Fatalf("白名单外工具必须拒绝且可见：%v %+v", degraded, collected)
	}
}
