package repository

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupAgentStatusRepo(t *testing.T) (*AgentStatusRepository, context.Context) {
	t.Helper()
	db := testutil.NewTestDB(t, &model.AgentStatus{})
	repo := NewAgentStatusRepositoryWithDB(db)
	return repo, context.Background()
}

func newTestAgentStatus(agentID uint, name, status string) *model.AgentStatus {
	return &model.AgentStatus{
		AgentID:        agentID,
		AgentName:      name,
		Status:         status,
		MaxSessions:    5,
		ActiveSessions: 0,
		TodaySessions:  0,
		TodayMessages:  0,
	}
}

func TestAgentStatus_NewAndWithDB(t *testing.T) {
	db := testutil.NewTestDB(t, &model.AgentStatus{})

	repo1 := NewAgentStatusRepositoryWithDB(db)
	assert.NotNil(t, repo1)

	repo2 := NewAgentStatusRepository()
	assert.NotNil(t, repo2)
}

func TestAgentStatus_Create(t *testing.T) {
	repo, ctx := setupAgentStatusRepo(t)

	s := newTestAgentStatus(1001, "客服小明", "online")
	err := repo.Create(ctx, s)
	require.NoError(t, err)
	assert.NotZero(t, s.ID)

	got, err := repo.GetByAgentID(ctx, 1001)
	require.NoError(t, err)
	assert.Equal(t, "客服小明", got.AgentName)
	assert.Equal(t, "online", got.Status)
}

func TestAgentStatus_Update(t *testing.T) {
	repo, ctx := setupAgentStatusRepo(t)

	s := newTestAgentStatus(1002, "小红", "offline")
	require.NoError(t, repo.Create(ctx, s))

	s.Status = "online"
	s.ActiveSessions = 2
	require.NoError(t, repo.Update(ctx, s))

	got, err := repo.GetByAgentID(ctx, 1002)
	require.NoError(t, err)
	assert.Equal(t, "online", got.Status)
	assert.Equal(t, 2, got.ActiveSessions)
}

func TestAgentStatus_GetByAgentID_Found(t *testing.T) {
	repo, ctx := setupAgentStatusRepo(t)

	s := newTestAgentStatus(2001, "小李", "busy")
	require.NoError(t, repo.Create(ctx, s))

	got, err := repo.GetByAgentID(ctx, 2001)
	require.NoError(t, err)
	assert.Equal(t, uint(2001), got.AgentID)
	assert.Equal(t, "busy", got.Status)
}

func TestAgentStatus_GetByAgentID_NotFound(t *testing.T) {
	repo, ctx := setupAgentStatusRepo(t)

	got, err := repo.GetByAgentID(ctx, 99999)
	assert.Error(t, err)
	assert.Nil(t, got)
}

func TestAgentStatus_ListAllAgents(t *testing.T) {
	repo, ctx := setupAgentStatusRepo(t)

	list, err := repo.ListAllAgents(ctx)
	require.NoError(t, err)
	assert.Len(t, list, 0)

	require.NoError(t, repo.Create(ctx, newTestAgentStatus(3001, "A", "online")))
	require.NoError(t, repo.Create(ctx, newTestAgentStatus(3002, "B", "offline")))
	require.NoError(t, repo.Create(ctx, newTestAgentStatus(3003, "C", "busy")))

	list, err = repo.ListAllAgents(ctx)
	require.NoError(t, err)
	assert.Len(t, list, 3)

	assert.Equal(t, uint(3001), list[0].AgentID)
	assert.Equal(t, uint(3003), list[2].AgentID)
}

func TestAgentStatus_GetOnlineAgents(t *testing.T) {
	repo, ctx := setupAgentStatusRepo(t)

	now := time.Now()
	tooOld := time.Now().Add(-10 * time.Minute)

	agents := []*model.AgentStatus{
		{AgentID: 4001, AgentName: "Online1", Status: "online", MaxSessions: 5, ActiveSessions: 2, LastActiveAt: &now},
		{AgentID: 4002, AgentName: "Busy1", Status: "busy", MaxSessions: 5, ActiveSessions: 3, LastActiveAt: &now},
		{AgentID: 4003, AgentName: "Offline1", Status: "offline", MaxSessions: 5, ActiveSessions: 0, LastActiveAt: &now},
		{AgentID: 4004, AgentName: "Full", Status: "online", MaxSessions: 5, ActiveSessions: 5, LastActiveAt: &now},
		{AgentID: 4005, AgentName: "Stale", Status: "online", MaxSessions: 5, ActiveSessions: 1, LastActiveAt: &tooOld},
	}
	for _, a := range agents {
		require.NoError(t, repo.Create(ctx, a))
	}

	list, err := repo.GetOnlineAgents(ctx)
	require.NoError(t, err)

	require.Len(t, list, 2)
	ids := map[uint]bool{}
	for _, a := range list {
		ids[a.AgentID] = true
	}
	assert.True(t, ids[4001])
	assert.True(t, ids[4002])

	assert.LessOrEqual(t, list[0].ActiveSessions, list[1].ActiveSessions)
}

func TestAgentStatus_UpdateStatus_Online(t *testing.T) {
	repo, ctx := setupAgentStatusRepo(t)

	s := newTestAgentStatus(5001, "小王", "offline")
	require.NoError(t, repo.Create(ctx, s))

	err := repo.UpdateStatus(ctx, 5001, "online")
	require.NoError(t, err)

	got, err := repo.GetByAgentID(ctx, 5001)
	require.NoError(t, err)
	assert.Equal(t, "online", got.Status)
	assert.NotNil(t, got.OnlineAt, "online 应写入 online_at")
	assert.NotNil(t, got.LastActiveAt, "应刷新 last_active_at")
}

func TestAgentStatus_UpdateStatus_Offline(t *testing.T) {
	repo, ctx := setupAgentStatusRepo(t)

	s := newTestAgentStatus(5002, "小李", "online")
	require.NoError(t, repo.Create(ctx, s))

	err := repo.UpdateStatus(ctx, 5002, "offline")
	require.NoError(t, err)

	got, err := repo.GetByAgentID(ctx, 5002)
	require.NoError(t, err)
	assert.Equal(t, "offline", got.Status)
	assert.NotNil(t, got.OfflineAt, "offline 应写入 offline_at")
}

func TestAgentStatus_UpdateStatus_Busy_NoExtraTimestamps(t *testing.T) {
	repo, ctx := setupAgentStatusRepo(t)

	s := newTestAgentStatus(5003, "小张", "offline")
	require.NoError(t, repo.Create(ctx, s))

	err := repo.UpdateStatus(ctx, 5003, "busy")
	require.NoError(t, err)

	got, err := repo.GetByAgentID(ctx, 5003)
	require.NoError(t, err)
	assert.Equal(t, "busy", got.Status)
	assert.Nil(t, got.OnlineAt, "非 online 不应写入 online_at")
	assert.Nil(t, got.OfflineAt, "非 offline 不应写入 offline_at")
	assert.NotNil(t, got.LastActiveAt)
}

func TestAgentStatus_CountOnlineAgents(t *testing.T) {
	repo, ctx := setupAgentStatusRepo(t)

	cnt, err := repo.CountOnlineAgents(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, cnt)

	require.NoError(t, repo.Create(ctx, newTestAgentStatus(6001, "a", "online")))
	require.NoError(t, repo.Create(ctx, newTestAgentStatus(6002, "b", "busy")))
	require.NoError(t, repo.Create(ctx, newTestAgentStatus(6003, "c", "offline")))
	require.NoError(t, repo.Create(ctx, newTestAgentStatus(6004, "d", "idle")))

	cnt, err = repo.CountOnlineAgents(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, cnt)
}

func TestAgentStatus_TouchHeartbeat(t *testing.T) {
	repo, ctx := setupAgentStatusRepo(t)

	oldTime := time.Now().Add(-30 * time.Minute)
	s := newTestAgentStatus(7001, "小赵", "online")
	s.LastActiveAt = &oldTime
	require.NoError(t, repo.Create(ctx, s))

	time.Sleep(10 * time.Millisecond)
	err := repo.TouchHeartbeat(ctx, 7001)
	require.NoError(t, err)

	got, err := repo.GetByAgentID(ctx, 7001)
	require.NoError(t, err)
	assert.NotNil(t, got.LastActiveAt)
	assert.True(t, got.LastActiveAt.After(oldTime), "last_active_at 应被刷新")

	assert.Equal(t, "online", got.Status)
}

func TestAgentStatus_IncrementActiveSessions(t *testing.T) {
	repo, ctx := setupAgentStatusRepo(t)

	s := newTestAgentStatus(8001, "A", "online")
	require.NoError(t, repo.Create(ctx, s))

	require.NoError(t, repo.IncrementActiveSessions(ctx, 8001))
	require.NoError(t, repo.IncrementActiveSessions(ctx, 8001))

	got, err := repo.GetByAgentID(ctx, 8001)
	require.NoError(t, err)
	assert.Equal(t, 2, got.ActiveSessions)
	assert.Equal(t, 2, got.TodaySessions)
}

func TestAgentStatus_DecrementActiveSessions(t *testing.T) {
	repo, ctx := setupAgentStatusRepo(t)

	s := newTestAgentStatus(8002, "B", "busy")
	s.ActiveSessions = 3
	require.NoError(t, repo.Create(ctx, s))

	require.NoError(t, repo.DecrementActiveSessions(ctx, 8002))
	require.NoError(t, repo.DecrementActiveSessions(ctx, 8002))

	got, err := repo.GetByAgentID(ctx, 8002)
	require.NoError(t, err)
	assert.Equal(t, 1, got.ActiveSessions)

	require.NoError(t, repo.DecrementActiveSessions(ctx, 8002))
	require.NoError(t, repo.DecrementActiveSessions(ctx, 8002))
	got, err = repo.GetByAgentID(ctx, 8002)
	require.NoError(t, err)
	assert.Equal(t, 0, got.ActiveSessions)
}

func TestAgentStatus_DecrementActiveSessions_Zero(t *testing.T) {
	repo, ctx := setupAgentStatusRepo(t)

	s := newTestAgentStatus(8003, "C", "offline")
	s.ActiveSessions = 0
	require.NoError(t, repo.Create(ctx, s))

	err := repo.DecrementActiveSessions(ctx, 8003)
	require.NoError(t, err)

	got, _ := repo.GetByAgentID(ctx, 8003)
	assert.Equal(t, 0, got.ActiveSessions)
}

func TestAgentStatus_IncrementTodayMessages(t *testing.T) {
	repo, ctx := setupAgentStatusRepo(t)

	s := newTestAgentStatus(9001, "Z", "online")
	require.NoError(t, repo.Create(ctx, s))

	for i := 0; i < 5; i++ {
		require.NoError(t, repo.IncrementTodayMessages(ctx, 9001))
	}

	got, err := repo.GetByAgentID(ctx, 9001)
	require.NoError(t, err)
	assert.Equal(t, 5, got.TodayMessages)
}

func TestAgentStatus_IncrementDecrement_NonExistent(t *testing.T) {
	repo, ctx := setupAgentStatusRepo(t)

	err := repo.IncrementActiveSessions(ctx, 99999)
	require.NoError(t, err)

	err = repo.DecrementActiveSessions(ctx, 99999)
	require.NoError(t, err)

	err = repo.IncrementTodayMessages(ctx, 99999)
	require.NoError(t, err)

	err = repo.TouchHeartbeat(ctx, 99999)
	require.NoError(t, err)

	err = repo.UpdateStatus(ctx, 99999, "online")
	require.NoError(t, err)
}
