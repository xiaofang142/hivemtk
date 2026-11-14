package service

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// TestBridgeAITriggerBindingGate_AccountLevelBinding 绑定门回归钉（2026-11-14 生产回归，修复 8b4d46b6）。
//
// 生产回归机理（18:05 实证：douyin 有绑定仍被 skip，入站有 trace 无 ai_dispatch，AI 触发全断）：
// TriggerInboundAI 的绑定门曾把 conversationID 传进 loadAgentForChannel 的 chatID 变参
// → 走 ResolveBinding 的 chat 精确匹配 → 漏掉 account 级绑定行（桥接 DM 的绑定
// chat_id 为空）→ 有绑定的渠道也被拦死。
//
// 本钉锁住两件事：
//  1. 绑定门同参调用（不传 chatID）必须命中 account 级绑定（修复后语义）；
//  2. 传 chatID 的旧错参形态必须保持"命中不到"的负例——它就是回归发生时的症状形状，
//     防止未来有人把变参改回去而不自知。
//
// 全链路真库：channel_agent_bindings + ai_agents + config_params（绑定门开关）。
func TestBridgeAITriggerBindingGate_AccountLevelBinding(t *testing.T) {
	db := testutil.NewTestDB(t,
		&model.Tenant{}, &model.AIAgent{}, &model.ChannelAgentBinding{},
		&model.ConfigParam{}, &model.SystemConfigKV{},
	)
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })

	if err := db.Create(&model.Tenant{ID: 1, Code: "gate_a", Name: "绑定门租户", Status: model.TenantStatusActive}).Error; err != nil {
		t.Fatalf("建租户: %v", err)
	}
	ctxA := tenant.WithTenantID(context.Background(), 1)

	// 智能体 + account 级绑定（桥接 DM 形态：chat_id 空）
	agentSvc := NewAIAgentServiceWithRepo(repository.NewAIAgentRepositoryWithDB(db))
	agent := &model.AIAgent{AgentCode: "gate_agent_001", Name: "绑定门智能体", Persona: "绑定门人设", Status: 1}
	if err := agentSvc.Create(ctxA, agent); err != nil {
		t.Fatalf("建智能体: %v", err)
	}
	enabledTrue := true
	primaryTrue := true
	binding := &model.ChannelAgentBinding{
		TenantID: 1, ChannelType: "douyin", AccountID: "acc_gate_001", ChatID: "",
		IsPrimary: &primaryTrue, Enabled: &enabledTrue, AgentID: agent.ID,
	}
	if err := db.Create(binding).Error; err != nil {
		t.Fatalf("建 account 级绑定: %v", err)
	}

	webhook := NewWebhookService()
	webhook.SetAgentBindingService(context.Background(), NewChannelAgentBindingService())

	// ---- 断言1（修复后语义）：绑定门同参调用（不传 chatID）命中 account 级绑定 ----
	bound, err := webhook.loadAgentForChannel(ctxA, "douyin", "acc_gate_001")
	if err != nil || bound == nil {
		t.Fatalf("account 级绑定必须命中（绑定门放行）: bound=%v err=%v", bound, err)
	}
	if bound.AgentID != agent.ID {
		t.Fatalf("应解析到绑定的智能体: got=%d want=%d", bound.AgentID, agent.ID)
	}

	// ---- 断言2（旧错参负例）：把会话 id 传进 chatID → chat 精确匹配漏掉 account 级行 ----
	// 这就是生产回归的症状形状；未来若有人把变参改回去，这条会红。
	boundWrong, _ := webhook.loadAgentForChannel(ctxA, "douyin", "acc_gate_001", "conv_duijiang")
	if boundWrong != nil {
		t.Fatal("传 chatID 会走 chat 精确匹配漏掉 account 级绑定——旧错参形态必须保持负例（否则绑定门回归将无声复发）")
	}

	// ---- 断言3：绑定门开关经 config_param 真库行生效 ----
	if BridgeAITriggerBindingRequired(ctxA) {
		t.Fatal("未播种开关行时应为默认 false")
	}
	if err := db.Create(&model.ConfigParam{
		Group: "bridge", Key: BridgeAITriggerBindingRequiredParam,
		Name: "桥接AI触发绑定门", ValueType: "bool",
		Value: "true", DefaultValue: "false",
	}).Error; err != nil {
		t.Fatalf("播种开关行: %v", err)
	}
	SetGlobalForTest(NewConfigParamService())
	t.Cleanup(func() { SetGlobal(nil) })
	if !BridgeAITriggerBindingRequired(ctxA) {
		t.Fatal("播种 true 后开关必须生效")
	}

	// ---- 断言4：开关开启后，无绑定的桥接渠道必须被门拦（resolve 到 nil） ----
	// 绑定门据此 return（静默不触发 AI）——这就是"绑定面板即 AI 开关面板"的语义。
	boundNone, errNone := webhook.loadAgentForChannel(ctxA, "weibo", "acc_nobind")
	if boundNone != nil {
		t.Fatalf("无绑定渠道必须解析不到智能体（门拦）: got=%v err=%v", boundNone, errNone)
	}
}
