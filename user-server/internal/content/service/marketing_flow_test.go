package service

import (
	"context"
	"encoding/json"
	"errors"
	"hivemtk-user/internal/content/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	cdpmodel "hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func setupMarketingFlowServiceTestDB(t *testing.T) *gorm.DB {
	database := testutil.NewTestDB(t,
		&model.MarketingFlow{},
		&model.FlowExecution{},
		&cdpmodel.UserTag{},
	)
	db.SetTestDB(database)
	return database
}

func setupMarketingFlowService(t *testing.T) *MarketingFlowService {
	return NewMarketingFlowService(newMarketingFlowDeps(setupMarketingFlowServiceTestDB(t)))
}

// TestNewMarketingFlowService 测试创建服务实例
func TestNewMarketingFlowService(t *testing.T) {
	service := setupMarketingFlowService(t)
	if service == nil {
		t.Fatal("Expected service instance, got nil")
	}
	if service.flowRepo == nil {
		t.Error("Expected flowRepo to be initialized")
	}
	if service.executionRepo == nil {
		t.Error("Expected executionRepo to be initialized")
	}
}

// TestMarketingFlowService_CreateFlow 测试创建营销流程
func TestMarketingFlowService_CreateFlow(t *testing.T) {
	service := setupMarketingFlowService(t)

	req := &CreateFlowRequest{
		Name:          "Test Flow",
		Description:   "Test Description",
		TriggerType:   "user_follow",
		TriggerConfig: map[string]any{"event": "user_follow"},
		FlowData:      &model.FlowDefinition{Nodes: []model.FlowNode{{ID: "node1", Type: "trigger"}, {ID: "node2", Type: "action"}}},
	}

	flow, err := service.CreateFlow(context.Background(), 1, req)
	if err != nil {
		t.Fatalf("CreateFlow failed: %v", err)
	}

	if flow == nil {
		t.Fatal("Expected flow to be returned")
	}
	if flow.Name != "Test Flow" {
		t.Errorf("Expected name 'Test Flow', got '%s'", flow.Name)
	}

	if flow.Status != model.FlowStatusDraft {
		t.Errorf("Expected status 'draft', got '%s'", flow.Status)
	}
	if flow.Version != 1 {
		t.Errorf("Expected version 1, got %d", flow.Version)
	}
	if flow.CreatedBy != 1 {
		t.Errorf("Expected created_by 1, got %d", flow.CreatedBy)
	}
}

// TestMarketingFlowService_CreateFlow_InvalidFlowData 测试创建营销流程（无效的 FlowData）
func TestMarketingFlowService_CreateFlow_InvalidFlowData(t *testing.T) {
	service := setupMarketingFlowService(t)

	req := &CreateFlowRequest{
		Name:          "Test Flow",
		Description:   "Test Description",
		TriggerType:   "user_follow",
		FlowData:      &model.FlowDefinition{Nodes: []model.FlowNode{{ID: "node1", Type: "action"}}},
		TriggerConfig: map[string]any{"event": "user_follow"},
	}

	_, err := service.CreateFlow(context.Background(), 1, req)
	if err == nil {
		t.Error("Expected error for invalid flow data (missing trigger node)")
	}
}

// TestMarketingFlowService_CreateFlow_EmptyNodes 测试创建营销流程（空节点）
func TestMarketingFlowService_CreateFlow_EmptyNodes(t *testing.T) {
	service := setupMarketingFlowService(t)

	req := &CreateFlowRequest{
		Name:          "Test Flow",
		Description:   "Test Description",
		TriggerType:   "user_follow",
		TriggerConfig: map[string]any{"event": "user_follow"},
		FlowData:      &model.FlowDefinition{Nodes: []model.FlowNode{}},
	}

	_, err := service.CreateFlow(context.Background(), 1, req)
	if err == nil {
		t.Error("Expected error for empty nodes")
	}
}

// TestMarketingFlowService_GetFlowList 测试获取流程列表
func TestMarketingFlowService_GetFlowList(t *testing.T) {
	service := setupMarketingFlowService(t)

	for i := 0; i < 3; i++ {
		db.GetDB().Create(&model.MarketingFlow{
			Name:   "Flow",
			Status: model.FlowStatusDraft,
		})
	}

	flows, total, err := service.GetFlowList(context.Background(), 1, 10)
	if err != nil {
		t.Fatalf("GetFlowList failed: %v", err)
	}

	if total != 3 {
		t.Errorf("Expected 3 total flows, got %d", total)
	}
	if len(flows) != 3 {
		t.Errorf("Expected 3 flows, got %d", len(flows))
	}
}

// TestMarketingFlowService_GetFlowByID 测试获取流程详情
func TestMarketingFlowService_GetFlowByID(t *testing.T) {
	service := setupMarketingFlowService(t)

	flow := &model.MarketingFlow{
		Name:   "Test Flow",
		Status: model.FlowStatusDraft,
	}
	db.GetDB().Create(flow)

	result, err := service.GetFlowByID(context.Background(), flow.ID)
	if err != nil {
		t.Fatalf("GetFlowByID failed: %v", err)
	}

	if result.Name != "Test Flow" {
		t.Errorf("Expected name 'Test Flow', got '%s'", result.Name)
	}
}

// TestMarketingFlowService_GetFlowByID_SingleTenant 单租户访问验证
// 单租户私有部署：所有数据归当前部署实例所有，GetFlowByID 不做跨租户校验。
func TestMarketingFlowService_GetFlowByID_SingleTenant(t *testing.T) {
	service := setupMarketingFlowService(t)

	flow := &model.MarketingFlow{
		Name:   "Test Flow",
		Status: model.FlowStatusDraft,
	}
	db.GetDB().Create(flow)

	got, err := service.GetFlowByID(context.Background(), flow.ID)
	if err != nil {
		t.Fatalf("GetFlowByID should succeed in single-tenant mode, got: %v", err)
	}
	if got == nil || got.ID != flow.ID {
		t.Errorf("Expected flow ID %d, got %v", flow.ID, got)
	}
}

// TestMarketingFlowService_GetFlowByID_UnboundTenant 服务层不吞租户故障：上下文未绑定时
// 必须把错误交出去，而不是回一个零值流程让上层以为「查到了」。
func TestMarketingFlowService_GetFlowByID_UnboundTenant(t *testing.T) {
	database := setupMarketingFlowServiceTestDB(t)
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })

	row := &model.MarketingFlow{Name: "裸库流程", Status: model.FlowStatusDraft}
	if err := database.WithContext(tenant.WithSystemScope(context.Background())).
		Create(row).Error; err != nil {
		t.Fatalf("预置流程失败: %v", err)
	}

	service := NewMarketingFlowService(newMarketingFlowDeps(database))
	got, err := service.GetFlowByID(context.Background(), row.ID)
	if err == nil {
		t.Fatalf("无租户上下文时不应查到流程，实得 %+v", got)
	}
	if got != nil {
		t.Errorf("出错时必须交出 nil，实得 %+v", got)
	}
}

// TestMarketingFlowService_UpdateFlow 测试更新流程
func TestMarketingFlowService_UpdateFlow(t *testing.T) {
	service := setupMarketingFlowService(t)

	flow := &model.MarketingFlow{
		Name:   "Old Name",
		Status: model.FlowStatusDraft,
	}
	db.GetDB().Create(flow)

	req := &UpdateFlowRequest{
		Name:        "New Name",
		Description: "New Description",
	}

	updated, err := service.UpdateFlow(context.Background(), flow.ID, 123, true, req)
	if err != nil {
		t.Fatalf("UpdateFlow failed: %v", err)
	}

	if updated.Name != "New Name" {
		t.Errorf("Expected name 'New Name', got '%s'", updated.Name)
	}
	if updated.Description != "New Description" {
		t.Errorf("Expected description 'New Description', got '%s'", updated.Description)
	}
}

// TestMarketingFlowService_UpdateFlow_SingleTenant 单租户更新验证
// 单租户私有部署：所有数据归当前部署实例所有，UpdateFlow 不做跨租户校验。
func TestMarketingFlowService_UpdateFlow_SingleTenant(t *testing.T) {
	service := setupMarketingFlowService(t)

	flow := &model.MarketingFlow{
		Name:   "Test Flow",
		Status: model.FlowStatusDraft,
	}
	db.GetDB().Create(flow)

	req := &UpdateFlowRequest{
		Name: "Updated",
	}
	updated, err := service.UpdateFlow(context.Background(), flow.ID, 123, true, req)
	if err != nil {
		t.Fatalf("UpdateFlow should succeed in single-tenant mode, got: %v", err)
	}
	if updated.Name != "Updated" {
		t.Errorf("Expected name 'Updated', got '%s'", updated.Name)
	}
}

// TestMarketingFlowService_DeleteFlow 测试删除流程
func TestMarketingFlowService_DeleteFlow(t *testing.T) {
	service := setupMarketingFlowService(t)

	flow := &model.MarketingFlow{
		Name:   "Test Flow",
		Status: model.FlowStatusDraft,
	}
	db.GetDB().Create(flow)

	err := service.DeleteFlow(context.Background(), flow.ID, 123, true)
	if err != nil {
		t.Fatalf("DeleteFlow failed: %v", err)
	}

	_, err = service.GetFlowByID(context.Background(), flow.ID)
	if err == nil {
		t.Error("Expected flow to be deleted")
	}
}

// TestMarketingFlowService_DeleteFlow_SingleTenant 单租户删除验证
// 单租户私有部署：所有数据归当前部署实例所有，DeleteFlow 不做跨租户校验。
func TestMarketingFlowService_DeleteFlow_SingleTenant(t *testing.T) {
	service := setupMarketingFlowService(t)

	flow := &model.MarketingFlow{
		Name:   "Test Flow",
		Status: model.FlowStatusDraft,
	}
	db.GetDB().Create(flow)

	if err := service.DeleteFlow(context.Background(), flow.ID, 123, true); err != nil {
		t.Fatalf("DeleteFlow should succeed in single-tenant mode, got: %v", err)
	}
	var count int64
	db.GetDB().Model(&model.MarketingFlow{}).Where("id = ?", flow.ID).Count(&count)
	if count != 0 {
		t.Errorf("Expected flow to be deleted, got count %d", count)
	}
}

// TestMarketingFlowService_ActivateFlow 测试激活流程
func TestMarketingFlowService_ActivateFlow(t *testing.T) {
	service := setupMarketingFlowService(t)

	flowData, _ := json.Marshal(map[string]any{
		"nodes": []map[string]any{
			{"id": "trigger_1", "type": "trigger"},
			{"id": "node1", "type": "action", "action": "send_message"},
		},
	})
	flow := &model.MarketingFlow{
		Name:     "Test Flow",
		Status:   model.FlowStatusDraft,
		FlowData: string(flowData),
	}
	db.GetDB().Create(flow)

	err := service.ActivateFlow(context.Background(), flow.ID, 123, true)
	if err != nil {
		t.Fatalf("ActivateFlow failed: %v", err)
	}

	var refreshed model.MarketingFlow
	db.GetDB().First(&refreshed, flow.ID)
	if refreshed.Status != model.FlowStatusActive {
		t.Errorf("Expected status 'active', got '%s'", refreshed.Status)
	}
}

// TestMarketingFlowService_ActivateFlow_InvalidDefinition 定义不合法时拒绝激活
//
// ActivateFlow 唯一的闸门是流程定义校验（状态本身没有迁移门槛，从任意状态置为
// active 是预期行为）。这里锁两件事：错误必须归入参数类，且失败后库里状态不动。
func TestMarketingFlowService_ActivateFlow_InvalidDefinition(t *testing.T) {
	service := setupMarketingFlowService(t)

	flow := &model.MarketingFlow{
		Name:   "Test Flow",
		Status: model.FlowStatusInactive,
	}
	db.GetDB().Create(flow)

	err := service.ActivateFlow(context.Background(), flow.ID, 123, true)
	if err == nil {
		t.Error("空定义不应激活成功")
	}
	if !errors.Is(err, utils.ErrInvalidInput) {
		t.Errorf("定义缺失应归入参数错误，实得 %v", err)
	}

	var refreshed model.MarketingFlow
	db.GetDB().First(&refreshed, flow.ID)
	if refreshed.Status != model.FlowStatusInactive {
		t.Errorf("激活失败后状态不应被改动，实得 %s", refreshed.Status)
	}
}

// TestMarketingFlowService_PauseFlow 测试暂停流程
func TestMarketingFlowService_PauseFlow(t *testing.T) {
	service := setupMarketingFlowService(t)

	flow := &model.MarketingFlow{
		Name:   "Test Flow",
		Status: model.FlowStatusActive,
	}
	db.GetDB().Create(flow)

	err := service.PauseFlow(context.Background(), flow.ID, 123, true)
	if err != nil {
		t.Fatalf("PauseFlow failed: %v", err)
	}

	var refreshed model.MarketingFlow
	db.GetDB().First(&refreshed, flow.ID)
	if refreshed.Status != model.FlowStatusPaused {
		t.Errorf("Expected status 'paused', got '%s'", refreshed.Status)
	}
}

// TestMarketingFlowService_StopFlow 测试停止流程
func TestMarketingFlowService_StopFlow(t *testing.T) {
	service := setupMarketingFlowService(t)

	flow := &model.MarketingFlow{
		Name:   "Test Flow",
		Status: model.FlowStatusActive,
	}
	db.GetDB().Create(flow)

	err := service.StopFlow(context.Background(), flow.ID, 123, true)
	if err != nil {
		t.Fatalf("StopFlow failed: %v", err)
	}

	var refreshed model.MarketingFlow
	db.GetDB().First(&refreshed, flow.ID)
	if refreshed.Status != model.FlowStatusInactive {
		t.Errorf("Expected status 'inactive', got '%s'", refreshed.Status)
	}
}

// TestMarketingFlowService_TriggerFlow 测试触发流程
func TestMarketingFlowService_TriggerFlow(t *testing.T) {
	service := setupMarketingFlowService(t)

	flowData, _ := json.Marshal(map[string]any{
		"nodes": []map[string]any{
			{
				"id":     "node1",
				"type":   "action",
				"action": "send_message",
				"config": map[string]any{
					"content": "Welcome!",
				},
			},
		},
	})

	flow := &model.MarketingFlow{
		Name:        "Test Flow",
		Status:      model.FlowStatusActive,
		TriggerType: model.TriggerTypeUserFollow,
		FlowData:    string(flowData),
	}
	db.GetDB().Create(flow)

	execution, err := service.TriggerFlow(context.Background(), flow, "trigger_123", "user_123", nil)
	if err != nil {
		t.Fatalf("TriggerFlow failed: %v", err)
	}
	if execution == nil || execution.ID == 0 {
		t.Fatal("TriggerFlow 必须直接交回新建的执行记录，不能回 nil")
	}
	if execution.Status != "running" {
		t.Errorf("新建执行记录初始状态应为 running，实得 %s", execution.Status)
	}

	var got model.FlowExecution
	db.GetDB().Where("flow_id = ? AND user_id = ?", flow.ID, "user_123").First(&got)
	if got.ID == 0 {
		t.Fatal("Expected execution to be created")
	}
	if got.FlowID != flow.ID {
		t.Errorf("Expected flow_id %d, got %d", flow.ID, got.FlowID)
	}
	if got.UserID != "user_123" {
		t.Errorf("Expected user_id 'user_123', got '%s'", got.UserID)
	}
}

// TestMarketingFlowService_TriggerFlow_InactiveFlow 测试触发非激活流程
func TestMarketingFlowService_TriggerFlow_InactiveFlow(t *testing.T) {
	service := setupMarketingFlowService(t)

	flow := &model.MarketingFlow{
		Name:   "Test Flow",
		Status: model.FlowStatusDraft,
	}
	db.GetDB().Create(flow)

	_, err := service.TriggerFlow(context.Background(), flow, "trigger_123", "user_123", nil)
	if err == nil {
		t.Error("Expected error for inactive flow")
	}
}

// TestMarketingFlowService_GetExecutionList 测试获取执行列表
func TestMarketingFlowService_GetExecutionList(t *testing.T) {
	service := setupMarketingFlowService(t)
	ctx := context.Background()

	// 子记录读口先按父流程解租户，故必须先有父流程。
	flow := &model.MarketingFlow{Name: "父流程", Status: model.FlowStatusActive}
	if err := service.flowRepo.Create(ctx, flow); err != nil {
		t.Fatalf("预置父流程失败: %v", err)
	}

	for i := 0; i < 3; i++ {
		db.GetDB().Create(&model.FlowExecution{
			FlowID: flow.ID,
			UserID: "user_123",
			Status: "completed",
		})
	}

	executions, total, err := service.GetExecutionList(ctx, flow.ID, 1, 10)
	if err != nil {
		t.Fatalf("GetExecutionList failed: %v", err)
	}

	if total != 3 {
		t.Errorf("Expected 3 total executions, got %d", total)
	}
	if len(executions) != 3 {
		t.Errorf("Expected 3 executions, got %d", len(executions))
	}
}

// TestMarketingFlowService_GetExecutionList_ParentGate 父流程不可见时子记录读口必须一起失败：
// 否则对别租户/已删除流程回 200 空列表，越权探测拿不到任何差异。
func TestMarketingFlowService_GetExecutionList_ParentGate(t *testing.T) {
	service := setupMarketingFlowService(t)

	_, _, err := service.GetExecutionList(context.Background(), 99999999, 1, 10)
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("读不存在父流程的执行列表应交出记录不存在，实得 %v", err)
	}
	if _, err := service.GetExecutionStats(context.Background(), 99999999); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("读不存在父流程的统计应交出记录不存在，实得 %v", err)
	}
}

// TestMarketingFlowService_GetExecutionStats 测试获取执行统计
func TestMarketingFlowService_GetExecutionStats(t *testing.T) {
	service := setupMarketingFlowService(t)
	ctx := context.Background()

	flow := &model.MarketingFlow{Name: "统计父流程", Status: model.FlowStatusActive}
	if err := service.flowRepo.Create(ctx, flow); err != nil {
		t.Fatalf("预置父流程失败: %v", err)
	}

	for i := 0; i < 5; i++ {
		status := "completed"
		if i%2 == 0 {
			status = "failed"
		}
		db.GetDB().Create(&model.FlowExecution{
			FlowID: flow.ID,
			UserID: "user_123",
			Status: status,
		})
	}

	stats, err := service.GetExecutionStats(ctx, flow.ID)
	if err != nil {
		t.Fatalf("GetExecutionStats failed: %v", err)
	}

	if stats == nil {
		t.Fatal("Expected stats to be returned")
	}
	if stats["failed"] != 3 || stats["completed"] != 2 {
		t.Errorf("按状态聚合应得 failed=3 completed=2，实得 %+v", stats)
	}
}

// TestMarketingFlowService_validateFlowDefinition 测试验证流程定义
func TestMarketingFlowService_validateFlowDefinition(t *testing.T) {
	service := setupMarketingFlowService(t)

	tests := []struct {
		name     string
		flowData string
		wantErr  bool
	}{
		{
			name: "valid flow",
			flowData: func() string {
				data, _ := json.Marshal(map[string]any{
					"nodes": []map[string]any{
						{"id": "trigger_1", "type": "trigger"},
						{"id": "node1", "type": "action"},
					},
				})
				return string(data)
			}(),
			wantErr: false,
		},
		{
			name:     "invalid json",
			flowData: "invalid",
			wantErr:  true,
		},
		{
			name: "empty nodes",
			flowData: func() string {
				data, _ := json.Marshal(map[string]any{
					"nodes": []map[string]any{},
				})
				return string(data)
			}(),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := service.validateFlowDefinition(tt.flowData)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateFlowDefinition() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestMarketingFlowService_evaluateCondition 测试条件评估
func TestMarketingFlowService_evaluateCondition(t *testing.T) {
	service := setupMarketingFlowService(t)

	tests := []struct {
		name      string
		config    map[string]any
		context   map[string]any
		wantError bool
	}{
		{
			name:      "empty condition",
			config:    map[string]any{"condition": ""},
			context:   map[string]any{"field": "value"},
			wantError: false,
		},
		{
			name:      "with condition",
			config:    map[string]any{"condition": "field eq value"},
			context:   map[string]any{"field": "value"},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := model.FlowNode{
				ID:     "test_node",
				Type:   "condition",
				Config: tt.config,
			}
			result, err := service.evaluateCondition(node, tt.context)
			if (err != nil) != tt.wantError {
				t.Fatalf("evaluateCondition() error = %v, wantError %v", err, tt.wantError)
			}
			if result == nil {
				t.Error("Expected result map to be returned")
			}
		})
	}
}

// TestMarketingFlowService_handleDelay 测试处理延迟
func TestMarketingFlowService_handleDelay(t *testing.T) {
	service := setupMarketingFlowService(t)

	tests := []struct {
		name      string
		duration  float64
		wantError bool
	}{
		{
			name:     "no delay",
			duration: 0,
		},
		{
			name:     "with delay",
			duration: 0.1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()

			node := model.FlowNode{
				ID:     "delay_node",
				Type:   "delay",
				Config: map[string]any{"duration": tt.duration},
			}

			result, err := service.handleDelay(ctx, node)
			if (err != nil) != tt.wantError {
				t.Fatalf("handleDelay() error = %v, wantError %v", err, tt.wantError)
			}
			_ = result
		})
	}
}

// TestMarketingFlowService_sendActionAddTag 测试添加标签动作
func TestMarketingFlowService_sendActionAddTag(t *testing.T) {
	service := setupMarketingFlowService(t)

	tests := []struct {
		name        string
		config      map[string]any
		merchantID  string
		userID      string
		wantErr     bool
		wantSuccess bool
		wantTags    []string
	}{
		{
			name: "add single tag",
			config: map[string]any{
				"tags": []any{"vip"},
			},

			userID:      "user-1",
			wantErr:     false,
			wantSuccess: true,
			wantTags:    []string{"vip"},
		},
		{
			name: "add multiple tags",
			config: map[string]any{
				"tags": []any{"vip", "active", "high-value"},
			},

			userID:      "user-2",
			wantErr:     false,
			wantSuccess: true,
			wantTags:    []string{"vip", "active", "high-value"},
		},
		{
			name: "add tags with duplicates",
			config: map[string]any{
				"tags": []any{"vip", "vip", "active"},
			},

			userID:      "user-3",
			wantErr:     false,
			wantSuccess: true,
			wantTags:    []string{"vip", "active"},
		},
		{
			name: "empty tags",
			config: map[string]any{
				"tags": []any{},
			},

			userID:      "user-4",
			wantErr:     true,
			wantSuccess: false,
		},
		{
			name: "tags with empty strings",
			config: map[string]any{
				"tags": []any{"", "valid-tag", ""},
			},

			userID:      "user-5",
			wantErr:     false,
			wantSuccess: true,
			wantTags:    []string{"valid-tag"},
		},
		{
			name: "invalid tags format",
			config: map[string]any{
				"tags": "not-a-slice",
			},

			userID:      "user-6",
			wantErr:     true,
			wantSuccess: false,
		},
		{
			name: "merchant isolation",
			config: map[string]any{
				"tags": []any{"merchant-tag"},
			},

			userID:      "user-7",
			wantErr:     false,
			wantSuccess: true,
			wantTags:    []string{"merchant-tag"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := service.sendActionAddTag(context.Background(), tt.config, tt.userID, nil)

			if (err != nil) != tt.wantErr {
				t.Errorf("sendActionAddTag() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && tt.wantSuccess {
				if result == nil {
					t.Fatal("Expected non-nil result on success")
				}
				if success, ok := result["success"].(bool); !ok || !success {
					t.Errorf("Expected success=true, got %v", result["success"])
				}

				if len(tt.wantTags) > 0 {
					tags, terr := service.userTagRepo.GetTagsByUser(context.Background(), tt.userID)
					if terr != nil {
						t.Fatalf("GetTagsByUser failed: %v", terr)
					}
					tagMap := make(map[string]bool)
					for _, tag := range tags {
						tagMap[tag] = true
					}
					for _, want := range tt.wantTags {
						if !tagMap[want] {
							t.Errorf("Expected tag %q in user tags, got %v", want, tags)
						}
					}
				}
			}
		})
	}
}

// TestMarketingFlowService_sendActionAddTag_Idempotency 测试添加标签的幂等性
func TestMarketingFlowService_sendActionAddTag_Idempotency(t *testing.T) {
	service := setupMarketingFlowService(t)

	config := map[string]any{
		"tags": []any{"vip", "active"},
	}

	_, err := service.sendActionAddTag(context.Background(), config, "user-idempotent", nil)
	if err != nil {
		t.Fatalf("First add failed: %v", err)
	}

	_, err = service.sendActionAddTag(context.Background(), config, "user-idempotent", nil)
	if err != nil {
		t.Fatalf("Second add failed: %v", err)
	}

	tags, err := service.userTagRepo.GetTagsByUser(context.Background(), "user-idempotent")
	if err != nil {
		t.Fatalf("GetTagsByUser failed: %v", err)
	}

	if len(tags) != 2 {
		t.Errorf("Expected 2 tags (no duplicates), got %d", len(tags))
	}
}

// TestMarketingFlowService_sendActionWebhook 测试 Webhook 动作
func TestMarketingFlowService_sendActionWebhook(t *testing.T) {
	// SSRF 豁免开关只在**开发姿态**下生效（见 insecureWebhookBypassAllowed），
	// 本用例的 httptest 服务是 127.0.0.1，故必须显式声明开发环境。
	t.Setenv("APP_ENV", "development")
	t.Setenv("MARKETING_WEBHOOK_ALLOW_INSECURE", "true")
	service := setupMarketingFlowService(t)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer ts.Close()

	tests := []struct {
		name    string
		config  map[string]any
		userID  string
		wantErr bool
	}{
		{
			name: "missing url",
			config: map[string]any{
				"method": "POST",
			},
			userID:  "user-1",
			wantErr: true,
		},
		{
			name: "valid url - local test server",
			config: map[string]any{
				"url":    ts.URL,
				"method": "POST",
			},
			userID:  "user-2",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := service.sendActionWebhook(context.Background(), tt.config, tt.userID, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("sendActionWebhook() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestMarketingFlowService_sendActionSendEmail 测试发送邮件动作
func TestMarketingFlowService_sendActionSendEmail(t *testing.T) {
	service := setupMarketingFlowService(t)

	tests := []struct {
		name    string
		config  map[string]any
		userID  string
		wantErr bool
	}{
		{
			name: "missing recipient email",
			config: map[string]any{
				"subject":   "Test Subject",
				"body":      "Test Body",
				"smtp_host": "smtp.example.com",
				"smtp_user": "user",
				"smtp_pass": "pass",
			},
			userID:  "user-1",
			wantErr: true,
		},
		{
			name: "missing email body",
			config: map[string]any{
				"to":        "test@example.com",
				"subject":   "Test Subject",
				"smtp_host": "smtp.example.com",
				"smtp_user": "user",
				"smtp_pass": "pass",
			},
			userID:  "user-3",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := service.sendActionSendEmail(context.Background(), tt.config, tt.userID, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("sendActionSendEmail() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
