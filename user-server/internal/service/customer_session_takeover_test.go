package service

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
)

// TestTakeoverByAgent_Success 正常接管：AI 会话切到人工
func TestTakeoverByAgent_Success(t *testing.T) {
	svc := setupCustomerSessionService(t)

	sess, err := svc.CreateSession(context.Background(), &CreateSessionRequest{
		Platform:  model.PlatformWeb,
		AccountID: "acc_1",
		UserID:    "u_1",
		UserName:  "访客A",
	})
	if err != nil {
		t.Fatal(err)
	}

	agent := &model.AgentStatus{AgentID: 101, AgentName: "客服甲", Status: "online", MaxSessions: 5, ActiveSessions: 0}
	if err := svc.agentRepo.Create(context.Background(), agent); err != nil {
		t.Fatal(err)
	}

	err = svc.TakeoverByAgent(context.Background(), &TakeoverRequest{
		SessionID: sess.ID,
		AgentID:   101,
		Reason:    "AI 答非所问",
	})
	if err != nil {
		t.Fatalf("TakeoverByAgent: %v", err)
	}

	got, _ := svc.GetSessionByID(context.Background(), sess.ID)
	if got.HandlerType != model.HandlerTypeHuman {
		t.Errorf("handler_type = %s, want human", got.HandlerType)
	}
	if got.Status != model.SessionStatusHumanHandling {
		t.Errorf("status = %s, want human_handling", got.Status)
	}
	if got.AgentID != 101 {
		t.Errorf("agent_id = %d, want 101", got.AgentID)
	}
	a, _ := svc.agentRepo.GetByAgentID(context.Background(), 101)
	if a.ActiveSessions != 1 {
		t.Errorf("agent active = %d, want 1", a.ActiveSessions)
	}
}

// TestTakeoverByAgent_OfflineAgent 离线坐席不允许接管
func TestTakeoverByAgent_OfflineAgent(t *testing.T) {
	svc := setupCustomerSessionService(t)

	sess, _ := svc.CreateSession(context.Background(), &CreateSessionRequest{
		Platform:  model.PlatformWeb,
		AccountID: "acc_1",
		UserID:    "u_1",
	})
	offline := &model.AgentStatus{AgentID: 200, AgentName: "客服乙", Status: "offline", MaxSessions: 5}
	_ = svc.agentRepo.Create(context.Background(), offline)

	err := svc.TakeoverByAgent(context.Background(), &TakeoverRequest{SessionID: sess.ID, AgentID: 200})
	if err == nil {
		t.Error("expected error for offline agent")
	}
}

// TestTakeoverByAgent_AgentFull 坐席已满不允许接管
func TestTakeoverByAgent_AgentFull(t *testing.T) {
	svc := setupCustomerSessionService(t)

	sess, _ := svc.CreateSession(context.Background(), &CreateSessionRequest{
		Platform: model.PlatformWeb, AccountID: "acc_1", UserID: "u_1",
	})
	agent := &model.AgentStatus{AgentID: 300, AgentName: "客服丙", Status: "online", MaxSessions: 1, ActiveSessions: 1}
	_ = svc.agentRepo.Create(context.Background(), agent)

	err := svc.TakeoverByAgent(context.Background(), &TakeoverRequest{SessionID: sess.ID, AgentID: 300})
	if err == nil {
		t.Error("expected error for full agent")
	}
}

// TestTakeoverByAgent_Idempotent 同坐席重复接管：幂等
func TestTakeoverByAgent_Idempotent(t *testing.T) {
	svc := setupCustomerSessionService(t)

	sess, _ := svc.CreateSession(context.Background(), &CreateSessionRequest{
		Platform: model.PlatformWeb, AccountID: "acc_1", UserID: "u_1",
	})
	agent := &model.AgentStatus{AgentID: 400, AgentName: "客服丁", Status: "online", MaxSessions: 5, ActiveSessions: 0}
	_ = svc.agentRepo.Create(context.Background(), agent)

	_ = svc.TakeoverByAgent(context.Background(), &TakeoverRequest{SessionID: sess.ID, AgentID: 400})
	a, _ := svc.agentRepo.GetByAgentID(context.Background(), 400)
	if a.ActiveSessions != 1 {
		t.Fatalf("after 1st takeover active = %d, want 1", a.ActiveSessions)
	}

	_ = svc.TakeoverByAgent(context.Background(), &TakeoverRequest{SessionID: sess.ID, AgentID: 400})
	a, _ = svc.agentRepo.GetByAgentID(context.Background(), 400)
	if a.ActiveSessions != 1 {
		t.Errorf("after 2nd takeover active = %d, want still 1 (idempotent)", a.ActiveSessions)
	}
}

// TestTakeoverByAgent_TakeoverFromAnother 从别的坐席接管：原坐席活跃数 -1
func TestTakeoverByAgent_TakeoverFromAnother(t *testing.T) {
	svc := setupCustomerSessionService(t)

	sess, _ := svc.CreateSession(context.Background(), &CreateSessionRequest{
		Platform: model.PlatformWeb, AccountID: "acc_1", UserID: "u_1",
	})
	a1 := &model.AgentStatus{AgentID: 500, AgentName: "甲", Status: "online", MaxSessions: 5, ActiveSessions: 1}
	a2 := &model.AgentStatus{AgentID: 501, AgentName: "乙", Status: "online", MaxSessions: 5, ActiveSessions: 0}
	_ = svc.agentRepo.Create(context.Background(), a1)
	_ = svc.agentRepo.Create(context.Background(), a2)
	_ = svc.sessionRepo.AssignAgent(context.Background(), sess.ID, 500, "甲")

	if err := svc.TakeoverByAgent(context.Background(), &TakeoverRequest{SessionID: sess.ID, AgentID: 501}); err != nil {
		t.Fatalf("takeover: %v", err)
	}
	old, _ := svc.agentRepo.GetByAgentID(context.Background(), 500)
	if old.ActiveSessions != 0 {
		t.Errorf("a1 active = %d, want 0", old.ActiveSessions)
	}
	newA, _ := svc.agentRepo.GetByAgentID(context.Background(), 501)
	if newA.ActiveSessions != 1 {
		t.Errorf("a2 active = %d, want 1", newA.ActiveSessions)
	}
}

// TestReleaseToAI 释放回 AI
func TestReleaseToAI(t *testing.T) {
	svc := setupCustomerSessionService(t)

	sess, _ := svc.CreateSession(context.Background(), &CreateSessionRequest{
		Platform: model.PlatformWeb, AccountID: "acc_1", UserID: "u_1",
	})
	agent := &model.AgentStatus{AgentID: 600, AgentName: "客服", Status: "online", MaxSessions: 5, ActiveSessions: 0}
	_ = svc.agentRepo.Create(context.Background(), agent)
	_ = svc.TakeoverByAgent(context.Background(), &TakeoverRequest{SessionID: sess.ID, AgentID: 600})

	if err := svc.ReleaseToAI(context.Background(), &ReleaseToAIRequest{SessionID: sess.ID, AgentID: 600}); err != nil {
		t.Fatalf("ReleaseToAI: %v", err)
	}
	got, _ := svc.GetSessionByID(context.Background(), sess.ID)
	if got.HandlerType != model.HandlerTypeAI {
		t.Errorf("handler = %s, want ai", got.HandlerType)
	}
	if got.Status != model.SessionStatusWaiting {
		t.Errorf("status = %s, want waiting", got.Status)
	}
	if got.AgentID != 0 {
		t.Errorf("agent_id = %d, want 0", got.AgentID)
	}
	a, _ := svc.agentRepo.GetByAgentID(context.Background(), 600)
	if a.ActiveSessions != 0 {
		t.Errorf("active = %d, want 0", a.ActiveSessions)
	}
}

// TestReleaseToAI_NotOwner 无权释放别人的会话
func TestReleaseToAI_NotOwner(t *testing.T) {
	svc := setupCustomerSessionService(t)

	sess, _ := svc.CreateSession(context.Background(), &CreateSessionRequest{
		Platform: model.PlatformWeb, AccountID: "acc_1", UserID: "u_1",
	})
	a1 := &model.AgentStatus{AgentID: 700, AgentName: "甲", Status: "online", MaxSessions: 5, ActiveSessions: 0}
	a2 := &model.AgentStatus{AgentID: 701, AgentName: "乙", Status: "online", MaxSessions: 5, ActiveSessions: 0}
	_ = svc.agentRepo.Create(context.Background(), a1)
	_ = svc.agentRepo.Create(context.Background(), a2)
	_ = svc.TakeoverByAgent(context.Background(), &TakeoverRequest{SessionID: sess.ID, AgentID: 700})

	err := svc.ReleaseToAI(context.Background(), &ReleaseToAIRequest{SessionID: sess.ID, AgentID: 701})
	if err == nil {
		t.Error("expected permission denied")
	}
}

// TestSwitchHandler_Human 切到人工
func TestSwitchHandler_Human(t *testing.T) {
	svc := setupCustomerSessionService(t)

	sess, _ := svc.CreateSession(context.Background(), &CreateSessionRequest{
		Platform: model.PlatformWeb, AccountID: "acc_1", UserID: "u_1",
	})
	agent := &model.AgentStatus{AgentID: 800, AgentName: "客服", Status: "online", MaxSessions: 5, ActiveSessions: 0}
	_ = svc.agentRepo.Create(context.Background(), agent)

	err := svc.SwitchHandler(context.Background(), &SwitchHandlerRequest{
		SessionID:   sess.ID,
		AgentID:     800,
		HandlerType: model.HandlerTypeHuman,
		Reason:      "客户要求转人工",
	})
	if err != nil {
		t.Fatalf("SwitchHandler: %v", err)
	}
	got, _ := svc.GetSessionByID(context.Background(), sess.ID)
	if got.HandlerType != model.HandlerTypeHuman {
		t.Errorf("handler = %s, want human", got.HandlerType)
	}
}

// TestSwitchHandler_AI 切回 AI（agent_id 自动从会话读取）
func TestSwitchHandler_AI(t *testing.T) {
	svc := setupCustomerSessionService(t)

	sess, _ := svc.CreateSession(context.Background(), &CreateSessionRequest{
		Platform: model.PlatformWeb, AccountID: "acc_1", UserID: "u_1",
	})
	agent := &model.AgentStatus{AgentID: 900, AgentName: "客服", Status: "online", MaxSessions: 5, ActiveSessions: 0}
	_ = svc.agentRepo.Create(context.Background(), agent)
	_ = svc.TakeoverByAgent(context.Background(), &TakeoverRequest{SessionID: sess.ID, AgentID: 900})

	err := svc.SwitchHandler(context.Background(), &SwitchHandlerRequest{
		SessionID:   sess.ID,
		AgentID:     0,
		HandlerType: model.HandlerTypeAI,
	})
	if err != nil {
		t.Fatalf("SwitchHandler: %v", err)
	}
	got, _ := svc.GetSessionByID(context.Background(), sess.ID)
	if got.HandlerType != model.HandlerTypeAI {
		t.Errorf("handler = %s, want ai", got.HandlerType)
	}
}

// TestSwitchHandler_AI_NoAgent 会话尚未分配坐席就切 AI → 报错
func TestSwitchHandler_AI_NoAgent(t *testing.T) {
	svc := setupCustomerSessionService(t)

	sess, _ := svc.CreateSession(context.Background(), &CreateSessionRequest{
		Platform: model.PlatformWeb, AccountID: "acc_1", UserID: "u_1",
	})
	err := svc.SwitchHandler(context.Background(), &SwitchHandlerRequest{
		SessionID:   sess.ID,
		HandlerType: model.HandlerTypeAI,
	})
	if err == nil {
		t.Error("expected error: no agent assigned")
	}
}

// TestSwitchHandler_InvalidType 非法 handler_type
func TestSwitchHandler_InvalidType(t *testing.T) {
	svc := setupCustomerSessionService(t)

	sess, _ := svc.CreateSession(context.Background(), &CreateSessionRequest{
		Platform: model.PlatformWeb, AccountID: "acc_1", UserID: "u_1",
	})
	err := svc.SwitchHandler(context.Background(), &SwitchHandlerRequest{
		SessionID:   sess.ID,
		HandlerType: "robot",
	})
	if err == nil {
		t.Error("expected error for invalid handler type")
	}
}

// TestTakeoverByAgent_SelfServiceAutoOnline 坐席自己回复触发的接管：离线也放行并自动上线
//
// 全旅程验收实测（2026-11-04）：新租户管理员首次点「回复访客」被
// 「坐席已离线，无法接管」挡死——单老板租户没有「先点上线再回复」的心智。
// 口径：只有在 Reply 双口显式置 SelfService=true 的接管才自动上线；
// 其它入口（抢会话等）仍按离线拒绝（见 TestTakeoverByAgent_OfflineAgent）。
func TestTakeoverByAgent_SelfServiceAutoOnline(t *testing.T) {
	svc := setupCustomerSessionService(t)

	sess, err := svc.CreateSession(context.Background(), &CreateSessionRequest{
		Platform:  model.PlatformWeb,
		AccountID: "acc_1",
		UserID:    "u_self",
		UserName:  "访客B",
	})
	if err != nil {
		t.Fatal(err)
	}
	agent := &model.AgentStatus{AgentID: 202, AgentName: "老板兼客服", Status: "offline", MaxSessions: 5}
	if err := svc.agentRepo.Create(context.Background(), agent); err != nil {
		t.Fatal(err)
	}

	err = svc.TakeoverByAgent(context.Background(), &TakeoverRequest{
		SessionID:   sess.ID,
		AgentID:     202,
		Reason:      "inbox_reply",
		SelfService: true,
	})
	if err != nil {
		t.Fatalf("SelfService 接管应放行离线坐席: %v", err)
	}

	got, _ := svc.GetSessionByID(context.Background(), sess.ID)
	if got.HandlerType != model.HandlerTypeHuman || got.AgentID != 202 {
		t.Fatalf("接管后 handler=%s agent=%d, want human/202", got.HandlerType, got.AgentID)
	}
	a, _ := svc.agentRepo.GetByAgentID(context.Background(), 202)
	if a.Status != "online" {
		t.Fatalf("SelfService 接管后坐席状态 = %s, want online", a.Status)
	}
}
