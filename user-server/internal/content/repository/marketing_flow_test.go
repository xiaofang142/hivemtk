package repository

import (
	"context"
	"errors"
	"hivemtk-user/internal/content/model"
	"hivemtk-user/internal/pkg/db"
	"testing"
	"time"

	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func setupMarketingFlowTestDB(t *testing.T) *gorm.DB {
	database := testutil.NewTestDB(t,
		&model.MarketingFlow{},
		&model.FlowExecution{}, &model.FlowNodeAttempt{},
	)
	db.SetTestDB(database)
	return database
}

func setupMarketingFlowRepositories(t *testing.T) (*MarketingFlowRepository, *FlowExecutionRepository) {
	database := setupMarketingFlowTestDB(t)
	return NewMarketingFlowRepository(database), NewFlowExecutionRepository(database)
}

// TestMarketingFlowRepository_Create 测试创建流程
func TestMarketingFlowRepository_Create(t *testing.T) {
	flowRepo, _ := setupMarketingFlowRepositories(t)

	tests := []struct {
		name    string
		flow    *model.MarketingFlow
		wantErr bool
	}{
		{
			name: "create flow success",
			flow: &model.MarketingFlow{
				Name:          "Test Flow",
				Description:   "Test Description",
				Status:        model.FlowStatusDraft,
				TriggerType:   model.TriggerTypeUserFollow,
				TriggerConfig: `{"key": "value"}`,
				FlowData:      `{"nodes": []}`,
				CreatedBy:     1,
			},
			wantErr: false,
		},
		{
			name: "create flow with minimal fields",
			flow: &model.MarketingFlow{
				Name:   "Minimal Flow",
				Status: model.FlowStatusDraft,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := flowRepo.Create(context.Background(), tt.flow)

			if (err != nil) != tt.wantErr {
				t.Errorf("Create() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr && tt.flow.ID == 0 {
				t.Error("Expected flow ID to be set after creation")
			}
		})
	}
}

// TestMarketingFlowRepository_GetByID 测试根据 ID 获取流程
func TestMarketingFlowRepository_GetByID(t *testing.T) {
	flowRepo, _ := setupMarketingFlowRepositories(t)

	flow := &model.MarketingFlow{
		Name:        "Get By ID Test",
		Status:      model.FlowStatusActive,
		Description: "Test description",
	}
	flowRepo.Create(context.Background(), flow)

	tests := []struct {
		name    string
		id      uint
		wantErr bool
	}{
		{
			name:    "get existing flow",
			id:      flow.ID,
			wantErr: false,
		},
		{
			name:    "get non-existing flow",
			id:      99999,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := flowRepo.GetByID(context.Background(), tt.id)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetByID() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantErr && result != nil {
				t.Errorf("GetByID() 报错时必须交出 nil，实得 %+v", result)
			}

			if !tt.wantErr {
				if result.ID != tt.id {
					t.Errorf("Expected ID %d, got %d", tt.id, result.ID)
				}
				if result.Name != "Get By ID Test" {
					t.Errorf("Expected name 'Get By ID Test', got '%s'", result.Name)
				}
			}
		})
	}
}

// TestMarketingFlowRepository_Update 测试更新流程
func TestMarketingFlowRepository_Update(t *testing.T) {
	flowRepo, _ := setupMarketingFlowRepositories(t)

	flow := &model.MarketingFlow{
		Name:        "Original Name",
		Status:      model.FlowStatusDraft,
		Description: "Original description",
	}
	flowRepo.Create(context.Background(), flow)

	tests := []struct {
		name       string
		updateFunc func(*model.MarketingFlow)
		wantErr    bool
	}{
		{
			name: "update name and status",
			updateFunc: func(f *model.MarketingFlow) {
				f.Name = "Updated Name"
				f.Status = model.FlowStatusActive
			},
			wantErr: false,
		},
		{
			name: "update description",
			updateFunc: func(f *model.MarketingFlow) {
				f.Description = "Updated description"
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.updateFunc(flow)
			err := flowRepo.Update(context.Background(), flow)

			if (err != nil) != tt.wantErr {
				t.Errorf("Update() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				updated, _ := flowRepo.GetByID(context.Background(), flow.ID)
				if updated.Name != flow.Name {
					t.Errorf("Expected name '%s', got '%s'", flow.Name, updated.Name)
				}
				if updated.Status != flow.Status {
					t.Errorf("Expected status '%v', got '%v'", flow.Status, updated.Status)
				}
			}
		})
	}

	// 读改写之间流程被别人删掉：Save 命中 0 行不能报成功，否则上层回「更新成功」而库里什么都没变。
	stale := &model.MarketingFlow{Name: "已被删除的", Status: model.FlowStatusDraft}
	if err := flowRepo.Create(context.Background(), stale); err != nil {
		t.Fatalf("预置流程失败: %v", err)
	}
	if err := flowRepo.Delete(context.Background(), stale.ID); err != nil {
		t.Fatalf("删除流程失败: %v", err)
	}
	stale.Name = "改不回来了"
	if err := flowRepo.Update(context.Background(), stale); !errors.Is(err, ErrMarketingFlowMissing) {
		t.Errorf("对已删除流程 Update 应交出 ErrMarketingFlowMissing，实得 %v", err)
	}

	// 更新失败后那行必须仍然是「没有」：Save 的 upsert 会把它按结构体里的 tenant_id 重新插回来。
	var count int64
	if err := db.GetDB().Model(&model.MarketingFlow{}).Where("id = ?", stale.ID).Count(&count).Error; err != nil {
		t.Fatalf("统计流程失败: %v", err)
	}
	if count != 0 {
		t.Errorf("Update 失败后不应把已删除的流程写回库，实得 count=%d", count)
	}
}

// TestMarketingFlowRepository_Delete 测试删除流程
func TestMarketingFlowRepository_Delete(t *testing.T) {
	flowRepo, _ := setupMarketingFlowRepositories(t)

	flow := &model.MarketingFlow{
		Name:   "To Be Deleted",
		Status: model.FlowStatusDraft,
	}
	flowRepo.Create(context.Background(), flow)

	err := flowRepo.Delete(context.Background(), flow.ID)
	if err != nil {
		t.Errorf("Delete() error = %v", err)
	}

	_, err = flowRepo.GetByID(context.Background(), flow.ID)
	if err == nil {
		t.Error("Expected flow to be deleted")
	}

	// 再删一次命中 0 行：不能把「没删到东西」说成删除成功。
	if err := flowRepo.Delete(context.Background(), flow.ID); !errors.Is(err, ErrMarketingFlowMissing) {
		t.Errorf("重复删除应交出 ErrMarketingFlowMissing，实得 %v", err)
	}
}

// TestMarketingFlowRepository_GetAllClampsPaging 分页夹紧：page=0 会算出负 OFFSET
// 让 Postgres 直接报错，越界的 page_size 会把整表拖进一次响应。
func TestMarketingFlowRepository_GetAllClampsPaging(t *testing.T) {
	flowRepo, _ := setupMarketingFlowRepositories(t)

	for i := 0; i < 3; i++ {
		if err := flowRepo.Create(context.Background(), &model.MarketingFlow{
			Name:   "分页流程",
			Status: model.FlowStatusDraft,
		}); err != nil {
			t.Fatalf("预置流程失败: %v", err)
		}
	}

	flows, total, err := flowRepo.GetAll(context.Background(), 0, 0)
	if err != nil {
		t.Fatalf("page=0 应被夹成第 1 页而不是报错，实得 %v", err)
	}
	if total != 3 || len(flows) != 3 {
		t.Errorf("page=0 应回第 1 页 3 条，实得 total=%d len=%d", total, len(flows))
	}

	if _, _, err := flowRepo.GetAll(context.Background(), 1, 100000); err != nil {
		t.Fatalf("越界 page_size 应被夹到默认值而不是报错，实得 %v", err)
	}
}

// TestMarketingFlowRepository_UpdateStatus 测试更新流程状态
func TestMarketingFlowRepository_UpdateStatus(t *testing.T) {
	flowRepo, _ := setupMarketingFlowRepositories(t)

	flow := &model.MarketingFlow{
		Name:   "Status Update Test",
		Status: model.FlowStatusDraft,
	}
	flowRepo.Create(context.Background(), flow)

	tests := []struct {
		name      string
		newStatus model.FlowStatus
		wantErr   bool
	}{
		{
			name:      "update to active",
			newStatus: model.FlowStatusActive,
			wantErr:   false,
		},
		{
			name:      "update to paused",
			newStatus: model.FlowStatusPaused,
			wantErr:   false,
		},
		{
			name:      "update to inactive",
			newStatus: model.FlowStatusInactive,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := flowRepo.UpdateStatus(context.Background(), flow.ID, tt.newStatus)

			if (err != nil) != tt.wantErr {
				t.Errorf("UpdateStatus() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				updated, _ := flowRepo.GetByID(context.Background(), flow.ID)
				if updated.Status != tt.newStatus {
					t.Errorf("Expected status '%v', got '%v'", tt.newStatus, updated.Status)
				}
			}
		})
	}

	// 命中 0 行（流程不存在）不能报成功：否则 handler 会回「激活成功」而库里什么都没变。
	if err := flowRepo.UpdateStatus(context.Background(), 999999, model.FlowStatusActive); !errors.Is(err, ErrMarketingFlowMissing) {
		t.Errorf("对不存在的流程改状态应交出 ErrMarketingFlowMissing，实得 %v", err)
	}
}

// TestFlowExecutionRepository_Create 测试创建执行记录
func TestFlowExecutionRepository_Create(t *testing.T) {
	_, execRepo := setupMarketingFlowRepositories(t)

	now := time.Now()
	tests := []struct {
		name      string
		execution *model.FlowExecution
		wantErr   bool
	}{
		{
			name: "create execution success",
			execution: &model.FlowExecution{
				FlowID:      1,
				TriggerID:   "trigger-123",
				UserID:      "user-456",
				Status:      "running",
				CurrentNode: "node-1",
				StartedAt:   now,
			},
			wantErr: false,
		},
		{
			name: "create execution with error message",
			execution: &model.FlowExecution{
				FlowID:       1,
				UserID:       "user-789",
				Status:       "failed",
				ErrorMessage: "Test error message",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := execRepo.Create(context.Background(), tt.execution)

			if (err != nil) != tt.wantErr {
				t.Errorf("Create() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr && tt.execution.ID == 0 {
				t.Error("Expected execution ID to be set after creation")
			}
		})
	}
}

// TestFlowExecutionRepository_GetByFlowID 测试根据流程 ID 获取执行记录
func TestFlowExecutionRepository_GetByFlowID(t *testing.T) {
	_, execRepo := setupMarketingFlowRepositories(t)

	for i := 1; i <= 5; i++ {
		execRepo.Create(context.Background(), &model.FlowExecution{
			FlowID: 1,
			UserID: string(rune('0' + i)),
			Status: "running",
		})
	}

	execRepo.Create(context.Background(), &model.FlowExecution{
		FlowID: 999,
		UserID: "user-other",
		Status: "completed",
	})

	tests := []struct {
		name      string
		flowID    uint
		page      int
		pageSize  int
		wantCount int
		wantErr   bool
	}{
		{
			name:      "get first page",
			flowID:    1,
			page:      1,
			pageSize:  3,
			wantCount: 3,
			wantErr:   false,
		},
		{
			name:      "get second page",
			flowID:    1,
			page:      2,
			pageSize:  3,
			wantCount: 2,
			wantErr:   false,
		},
		{
			name:      "get all results",
			flowID:    1,
			page:      1,
			pageSize:  10,
			wantCount: 5,
			wantErr:   false,
		},
		{
			name:      "different flow isolation",
			flowID:    999,
			page:      1,
			pageSize:  10,
			wantCount: 1,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, total, err := execRepo.GetByFlowID(context.Background(), tt.flowID, tt.page, tt.pageSize)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetByFlowID() error = %v, wantErr %v", err, tt.wantErr)
			}

			if len(results) != tt.wantCount {
				t.Errorf("Expected %d results, got %d", tt.wantCount, len(results))
			}

			if tt.page == 1 && tt.flowID == 1 && int(total) != 5 {
				t.Errorf("Expected total 5 for flowID 1, got %d", total)
			}
		})
	}
}

// TestFlowExecutionRepository_Update 测试更新执行记录
func TestFlowExecutionRepository_Update(t *testing.T) {
	_, execRepo := setupMarketingFlowRepositories(t)
	ctx := context.Background()

	execution := &model.FlowExecution{
		FlowID:      1,
		UserID:      "user-123",
		Status:      "running",
		CurrentNode: "node-1",
	}
	if err := execRepo.Create(ctx, execution); err != nil {
		t.Fatalf("预置执行记录失败: %v", err)
	}

	execution.Status = "completed"
	execution.CurrentNode = ""

	err := execRepo.Update(ctx, execution)
	if err != nil {
		t.Errorf("Update() error = %v", err)
	}

	var updated model.FlowExecution
	if err := db.GetDB().First(&updated, execution.ID).Error; err != nil {
		t.Fatalf("读回执行记录失败: %v", err)
	}
	if updated.Status != "completed" {
		t.Errorf("Expected status 'completed', got '%s'", updated.Status)
	}
}

// TestFlowExecutionRepository_GetStats 测试获取执行统计
func TestFlowExecutionRepository_GetStats(t *testing.T) {
	_, execRepo := setupMarketingFlowRepositories(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if err := execRepo.Create(ctx, &model.FlowExecution{
			FlowID: 1,
			UserID: string(rune('0' + i)),
			Status: "running",
		}); err != nil {
			t.Fatalf("预置执行记录失败: %v", err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := execRepo.Create(ctx, &model.FlowExecution{
			FlowID: 1,
			UserID: string(rune('a' + i)),
			Status: "completed",
		}); err != nil {
			t.Fatalf("预置执行记录失败: %v", err)
		}
	}
	if err := execRepo.Create(ctx, &model.FlowExecution{
		FlowID: 1,
		UserID: "user-failed",
		Status: "failed",
	}); err != nil {
		t.Fatalf("预置执行记录失败: %v", err)
	}

	stats, err := execRepo.GetStats(ctx, 1)
	if err != nil {
		t.Fatalf("GetStats() error = %v", err)
	}

	if stats["running"] != 3 {
		t.Errorf("Expected 3 running, got %d", stats["running"])
	}
	if stats["completed"] != 2 {
		t.Errorf("Expected 2 completed, got %d", stats["completed"])
	}
	if stats["failed"] != 1 {
		t.Errorf("Expected 1 failed, got %d", stats["failed"])
	}
}

// TestFlowExecutionRepository_GetByFlowIDClampsPaging 执行记录列表同样要夹紧分页：
// flow_id 命中的记录数不可控，page=0 会让整口直接 500。
func TestFlowExecutionRepository_GetByFlowIDClampsPaging(t *testing.T) {
	_, execRepo := setupMarketingFlowRepositories(t)
	ctx := context.Background()

	if err := execRepo.Create(ctx, &model.FlowExecution{FlowID: 42, UserID: "u", Status: "running"}); err != nil {
		t.Fatalf("预置执行记录失败: %v", err)
	}

	results, total, err := execRepo.GetByFlowID(ctx, 42, 0, 0)
	if err != nil {
		t.Fatalf("page=0 应被夹成第 1 页而不是报错，实得 %v", err)
	}
	if total != 1 || len(results) != 1 {
		t.Errorf("应回第 1 页 1 条，实得 total=%d len=%d", total, len(results))
	}
}
