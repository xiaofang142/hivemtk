// customer_session_pending_pool_test.go 待处理池服务读口的契约冻结。
//
// AV-04c 批次5 刀1：GetPendingSessions / CountPendingSessions / 两个 Scoped
// 变体原先经 sessionRepo.GetDB 直拼查询，下沉后在此冻结服务边界行为：
// 状态口径（pending/ai_handling/waiting）、坐席隔离（自己名下 + 未分配池）。
// 排序/分页细节在仓储层测试钉死（repository/customer_session_pending_pool_test.go）。
package service

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
)

func poolCreateSession(t *testing.T, svc *CustomerSessionService, tag string) *model.CustomerSession {
	t.Helper()
	sess, err := svc.CreateSession(context.Background(), &CreateSessionRequest{
		Platform:  model.PlatformDouyin,
		AccountID: "acct_pool_" + tag,
		UserID:    "u_pool_" + tag,
	})
	if err != nil {
		t.Fatalf("CreateSession(%s): %v", tag, err)
	}
	return sess
}

func TestCustomerSessionService_GetPendingSessions_StatusContract(t *testing.T) {
	svc := setupCustomerSessionService(t)
	ctx := context.Background()

	p1 := poolCreateSession(t, svc, "p1")
	poolCreateSession(t, svc, "p2")
	poolCreateSession(t, svc, "w1")
	// 关单/完结态不入池。
	done := poolCreateSession(t, svc, "done")
	if err := svc.UpdateSessionStatus(ctx, done.ID, model.SessionStatusResolved); err != nil {
		t.Fatalf("close done: %v", err)
	}
	_ = p1

	sessions, err := svc.GetPendingSessions(ctx, 1, 100)
	if err != nil {
		t.Fatalf("GetPendingSessions: %v", err)
	}
	if len(sessions) != 3 {
		t.Fatalf("待处理池应 3 条（2 pending + 1 waiting），实际 %d", len(sessions))
	}
	for _, s := range sessions {
		switch s.Status {
		case model.SessionStatusPending, model.SessionStatusAIHandling, model.SessionStatusWaiting:
		default:
			t.Errorf("非待处理状态混入池: %v", s.Status)
		}
	}

	count, err := svc.CountPendingSessions(ctx)
	if err != nil {
		t.Fatalf("CountPendingSessions: %v", err)
	}
	if count != 3 {
		t.Errorf("计数应与列表同口径（3），实际 %d", count)
	}

	// 分页：offset 生效。
	page2, err := svc.GetPendingSessions(ctx, 2, 2)
	if err != nil {
		t.Fatalf("GetPendingSessions(page2): %v", err)
	}
	if len(page2) != 1 {
		t.Errorf("pageSize=2 取第 2 页应余 1 条，实际 %d", len(page2))
	}
}

func TestCustomerSessionService_PendingPoolScoped_AgentIsolation(t *testing.T) {
	svc := setupCustomerSessionService(t)
	ctx := context.Background()

	mine := poolCreateSession(t, svc, "mine")
	others := poolCreateSession(t, svc, "other")
	pool := poolCreateSession(t, svc, "pool")
	if err := svc.sessionRepo.AssignAgent(ctx, mine.ID, 5, "me"); err != nil {
		t.Fatalf("assign mine: %v", err)
	}
	if err := svc.sessionRepo.AssignAgent(ctx, others.ID, 9, "other"); err != nil {
		t.Fatalf("assign other: %v", err)
	}
	_ = pool // 保持未分配

	// AssignAgent 会把 status 切到 human_handling（脱离待处理池）；这里把它拨回
	// pending，构造「坐席名下 + 仍在池状态」的行（如重新打开的会话）。
	if err := svc.sessionRepo.UpdateFields(ctx, mine.ID, map[string]any{"status": model.SessionStatusPending}); err != nil {
		t.Fatalf("revert mine status: %v", err)
	}

	// 坐席视角：自己名下 + 未分配池，共 2 条。
	agentSessions, err := svc.GetPendingSessionsScoped(ctx, 1, 100, SessionScope{Role: "customer_service", UserID: 5})
	if err != nil {
		t.Fatalf("GetPendingSessionsScoped(agent): %v", err)
	}
	if len(agentSessions) != 2 {
		t.Fatalf("坐席应见 2 条（自己 + 池），实际 %d", len(agentSessions))
	}
	ids := map[uint]bool{}
	for _, s := range agentSessions {
		ids[s.ID] = true
	}
	if !ids[mine.ID] || !ids[pool.ID] || ids[others.ID] {
		t.Errorf("隔离范围失真: ids=%v", ids)
	}

	agentCount, err := svc.CountPendingSessionsScoped(ctx, SessionScope{Role: "customer_service", UserID: 5})
	if err != nil {
		t.Fatalf("CountPendingSessionsScoped(agent): %v", err)
	}
	if agentCount != 2 {
		t.Errorf("坐席隔离计数应为 2，实际 %d", agentCount)
	}

	// admin 视角：全量 2 条（others 已被 AssignAgent 切到 human_handling，不在池）。
	adminSessions, err := svc.GetPendingSessionsScoped(ctx, 1, 100, SessionScope{Role: "admin", UserID: 1})
	if err != nil {
		t.Fatalf("GetPendingSessionsScoped(admin): %v", err)
	}
	if len(adminSessions) != 2 {
		t.Errorf("admin 应见池内全部 2 条，实际 %d", len(adminSessions))
	}
	adminCount, err := svc.CountPendingSessionsScoped(ctx, SessionScope{Role: "admin", UserID: 1})
	if err != nil {
		t.Fatalf("CountPendingSessionsScoped(admin): %v", err)
	}
	if adminCount != 2 {
		t.Errorf("admin 计数应为 2，实际 %d", adminCount)
	}
}
