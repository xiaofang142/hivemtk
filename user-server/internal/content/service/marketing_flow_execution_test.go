package service

import (
	"encoding/json"
	"testing"
	"time"

	"hivemtk-user/internal/content/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"github.com/stretchr/testify/require"
)

// 恢复保留原图和绝对dueAt，重复触发不创建第二份运行。
func TestDurableMarketingFlow_RestartDeadlineAndDuplicate(t *testing.T) {
	database := testutil.NewTestDB(t, &model.MarketingFlow{}, &model.FlowExecution{}, &model.FlowNodeAttempt{})
	ctx := tenant.WithTenantID(t.Context(), 771)
	svc := NewMarketingFlowService(newMarketingFlowDeps(database))
	encoded, _ := json.Marshal(model.FlowDefinition{Nodes: []model.FlowNode{{ID: "start", Type: "trigger", NextNodes: []string{"wait"}}, {ID: "wait", Type: "delay", Config: map[string]any{"duration": 60}}}})
	graph := string(encoded)
	flow := &model.MarketingFlow{TenantID: 771, Name: "durable", Status: model.FlowStatusActive, Version: 1, FlowData: graph, CreatedBy: 17}
	require.NoError(t, database.WithContext(ctx).Create(flow).Error)
	key, err := flowTriggerKey(ctx, flow, "event-a", "buyer-a")
	require.NoError(t, err)
	input, _ := json.Marshal(map[string]any{"order": "fixture"})
	row := &model.FlowExecution{FlowID: flow.ID, FlowVersion: flow.Version, GraphSnapshot: flow.FlowData, InputSnapshot: string(input), TriggerKey: &key, TriggerID: "event-a", UserID: "buyer-a", Status: "running"}
	created, err := svc.executionRepo.CreateDurable(ctx, row)
	require.NoError(t, err)
	require.True(t, created)
	require.NoError(t, svc.executeDurableFlow(ctx, row))
	var stored model.FlowExecution
	require.NoError(t, database.WithContext(ctx).First(&stored, row.ID).Error)
	require.Equal(t, "waiting", stored.Status)
	require.NotNil(t, stored.DueAt)
	originalDue := *stored.DueAt
	duplicate := &model.FlowExecution{FlowID: flow.ID, FlowVersion: 1, GraphSnapshot: flow.FlowData, InputSnapshot: string(input), TriggerKey: &key, TriggerID: "event-a", UserID: "buyer-a", Status: "running"}
	created, err = svc.executionRepo.CreateDurable(ctx, duplicate)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, row.ID, duplicate.ID)
	restarted := NewMarketingFlowService(newMarketingFlowDeps(database))
	count, err := restarted.ResumeDue(ctx, flow.ID, 17, false)
	require.NoError(t, err)
	require.Zero(t, count)
	require.NoError(t, database.WithContext(ctx).First(&stored, row.ID).Error)
	require.True(t, stored.DueAt.Equal(originalDue))
	before := time.Now().Add(-time.Second)
	require.NoError(t, database.WithContext(ctx).Model(&model.FlowExecution{}).Where("id = ?", row.ID).Update("due_at", before).Error)
	require.NoError(t, database.WithContext(ctx).Model(&model.FlowNodeAttempt{}).Where("execution_id = ? AND node_id = ?", row.ID, "wait").Update("due_at", before).Error)
	count, err = restarted.ResumeDue(ctx, flow.ID, 17, false)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	require.NoError(t, database.WithContext(ctx).First(&stored, row.ID).Error)
	require.Equal(t, "completed", stored.Status)
	var nodes int64
	require.NoError(t, database.WithContext(ctx).Model(&model.FlowNodeAttempt{}).Where("execution_id = ?", row.ID).Count(&nodes).Error)
	require.EqualValues(t, 2, nodes)
	count, err = restarted.ResumeDue(ctx, flow.ID, 17, false)
	require.NoError(t, err)
	require.Zero(t, count)
}

// 已提交派发而结果未保存的节点恢复后不再执行。
func TestDurableMarketingFlow_UnknownDispatchCannotReplay(t *testing.T) {
	database := testutil.NewTestDB(t, &model.MarketingFlow{}, &model.FlowExecution{}, &model.FlowNodeAttempt{})
	ctx := tenant.WithTenantID(t.Context(), 772)
	svc := NewMarketingFlowService(newMarketingFlowDeps(database))
	encoded, _ := json.Marshal(model.FlowDefinition{Nodes: []model.FlowNode{{ID: "start", Type: "trigger"}}})
	graph := string(encoded)
	flow := &model.MarketingFlow{TenantID: 772, Name: "unknown", Status: model.FlowStatusActive, Version: 1, FlowData: graph, CreatedBy: 17}
	require.NoError(t, database.WithContext(ctx).Create(flow).Error)
	key, err := flowTriggerKey(ctx, flow, "event-b", "buyer-b")
	require.NoError(t, err)
	row := &model.FlowExecution{FlowID: flow.ID, FlowVersion: 1, GraphSnapshot: graph, InputSnapshot: "{}", TriggerKey: &key, TriggerID: "event-b", UserID: "buyer-b", Status: "running"}
	_, err = svc.executionRepo.CreateDurable(ctx, row)
	require.NoError(t, err)
	require.NoError(t, database.WithContext(ctx).Create(&model.FlowNodeAttempt{TenantID: 772, ExecutionID: row.ID, NodeID: "start", State: "dispatch_committed"}).Error)
	require.NoError(t, svc.executeDurableFlow(ctx, row))
	var stored model.FlowExecution
	require.NoError(t, database.WithContext(ctx).First(&stored, row.ID).Error)
	require.Equal(t, "outcome_unknown", stored.Status)
	_, err = svc.ResumeDue(tenant.WithTenantID(t.Context(), 773), flow.ID, 17, true)
	require.Error(t, err)
}
