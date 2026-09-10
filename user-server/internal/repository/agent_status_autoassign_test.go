package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// setupAutoAssignTestDB 建立自动分配测试库（坐席状态 + 会话两表）
func setupAutoAssignTestDB(t *testing.T) (*gorm.DB, *AgentStatusRepository, *CustomerSessionRepository) {
	t.Helper()
	database := testutil.NewTestDB(t,
		&model.AgentStatus{},
		&model.CustomerSession{},
	)
	return database,
		NewAgentStatusRepositoryWithDB(database),
		NewCustomerSessionRepositoryWithDB(database)
}

// TestAutoAssignAgentTx_PicksLeastBusyWithLock 验证事务分配选中 last_active 最新的最闲坐席，
// 并原子完成 会话分配 + 坐席负载自增
func TestAutoAssignAgentTx_PicksLeastBusyWithLock(t *testing.T) {
	database, agentRepo, sessionRepo := setupAutoAssignTestDB(t)
	ctx := context.Background()

	fresh := time.Now().Add(-1 * time.Minute)
	stale := time.Now().Add(-10 * time.Minute)
	// 坐席1：active_sessions 更少但心跳过期（cutoff 5min 之外，不参选）
	if err := database.Create(&model.AgentStatus{
		AgentID: 1, AgentName: "过期坐席", Status: "online",
		ActiveSessions: 0, MaxSessions: 5, LastActiveAt: &stale,
	}).Error; err != nil {
		t.Fatalf("seed agent1: %v", err)
	}
	// 坐席2：1 个活跃会话，心跳新鲜 → 应被选中
	if err := database.Create(&model.AgentStatus{
		AgentID: 2, AgentName: "在班坐席", Status: "online",
		ActiveSessions: 1, MaxSessions: 5, LastActiveAt: &fresh,
	}).Error; err != nil {
		t.Fatalf("seed agent2: %v", err)
	}
	// 坐席3：满载，不参选
	if err := database.Create(&model.AgentStatus{
		AgentID: 3, AgentName: "满载坐席", Status: "online",
		ActiveSessions: 5, MaxSessions: 5, LastActiveAt: &fresh,
	}).Error; err != nil {
		t.Fatalf("seed agent3: %v", err)
	}

	sess := &model.CustomerSession{SessionID: "as-tx-1", Platform: "wechat", Status: model.SessionStatusPending}
	if err := database.Create(sess).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}

	best, err := agentRepo.AutoAssignAgentTx(ctx, sessionRepo, sess.ID)
	if err != nil {
		t.Fatalf("AutoAssignAgentTx: %v", err)
	}
	if best.AgentID != 2 {
		t.Fatalf("expected agent 2 (在班坐席), got %d (%s)", best.AgentID, best.AgentName)
	}

	var got model.CustomerSession
	if err := database.First(&got, sess.ID).Error; err != nil {
		t.Fatalf("reload session: %v", err)
	}
	if got.AgentID != 2 || got.Status != model.SessionStatusHumanHandling {
		t.Fatalf("session not assigned in tx: agent=%d status=%s", got.AgentID, got.Status)
	}

	var ag model.AgentStatus
	if err := database.Where("agent_id = ?", 2).First(&ag).Error; err != nil {
		t.Fatalf("reload agent: %v", err)
	}
	if ag.ActiveSessions != 2 {
		t.Fatalf("agent load not incremented: active_sessions=%d", ag.ActiveSessions)
	}
}

// TestAutoAssignAgentTx_NoAgentAvailable 无可用坐席时回滚并返回 ErrRecordNotFound
func TestAutoAssignAgentTx_NoAgentAvailable(t *testing.T) {
	database, agentRepo, sessionRepo := setupAutoAssignTestDB(t)
	ctx := context.Background()

	sess := &model.CustomerSession{SessionID: "as-tx-2", Platform: "wechat", Status: model.SessionStatusPending}
	if err := database.Create(sess).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}

	_, err := agentRepo.AutoAssignAgentTx(ctx, sessionRepo, sess.ID)
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected ErrRecordNotFound, got %v", err)
	}

	// 会话状态不应被改动
	var got model.CustomerSession
	if err := database.First(&got, sess.ID).Error; err != nil {
		t.Fatalf("reload session: %v", err)
	}
	if got.AgentID != 0 {
		t.Fatalf("session should remain unassigned on rollback, got agent=%d", got.AgentID)
	}
}
