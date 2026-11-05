package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"hivemtk-user/internal/content/model"
	reachmodel "hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func setupMarketingFlowServiceTestDBWithCDP(t *testing.T) *gorm.DB {
	database := testutil.NewTestDB(t,
		&model.MarketingFlow{},
		&model.FlowExecution{},
		&reachmodel.UserTag{},
		&reachmodel.CustomerSession{},
		&reachmodel.AgentStatus{},
		&reachmodel.OperationLog{},
		&reachmodel.Clue{},
		&reachmodel.Order{},
	)
	return database
}

// TestMarketingFlowService_sendActionRemoveTag 测试移除标签动作
func TestMarketingFlowService_sendActionRemoveTag(t *testing.T) {
	database := setupMarketingFlowServiceTestDBWithCDP(t)
	service := NewMarketingFlowService(NewMarketingFlowDeps(database))

	if err := service.userTagRepo.AddTags(context.Background(), "user-remove-1", []string{"vip", "active", "trial"}); err != nil {
		t.Fatalf("预设标签失败：%v", err)
	}

	tests := []struct {
		name        string
		config      map[string]any
		userID      string
		wantErr     bool
		wantRemoved []string
	}{
		{
			name: "remove single tag",
			config: map[string]any{
				"tags": []any{"vip"},
			},
			userID:      "user-remove-1",
			wantErr:     false,
			wantRemoved: []string{"vip"},
		},
		{
			name: "remove multiple tags",
			config: map[string]any{
				"tags": []any{"active", "trial"},
			},
			userID:      "user-remove-1",
			wantErr:     false,
			wantRemoved: []string{"active", "trial"},
		},
		{
			name: "remove with duplicates",
			config: map[string]any{
				"tags": []any{"vip", "vip"},
			},
			userID:      "user-remove-dup",
			wantErr:     false,
			wantRemoved: []string{"vip"},
		},
		{
			name: "empty tags list",
			config: map[string]any{
				"tags": []any{},
			},
			userID:  "user-remove-2",
			wantErr: true,
		},
		{
			name: "invalid tags format",
			config: map[string]any{
				"tags": "not-a-slice",
			},
			userID:  "user-remove-3",
			wantErr: true,
		},
		{
			name: "tags with empty strings only",
			config: map[string]any{
				"tags": []any{"", ""},
			},
			userID:  "user-remove-4",
			wantErr: true,
		},
		{
			name: "empty user_id",
			config: map[string]any{
				"tags": []any{"vip"},
			},
			userID:  "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := service.sendActionRemoveTag(context.Background(), tt.config, tt.userID, nil)

			if (err != nil) != tt.wantErr {
				t.Errorf("sendActionRemoveTag() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if result == nil {
					t.Fatal("Expected non-nil result on success")
				}
				if success, ok := result["success"].(bool); !ok || !success {
					t.Errorf("Expected success=true, got %v", result["success"])
				}

				if len(tt.wantRemoved) > 0 {
					tags, terr := service.userTagRepo.GetTagsByUser(context.Background(), tt.userID)
					if terr != nil {
						t.Fatalf("GetTagsByUser failed: %v", terr)
					}
					tagMap := make(map[string]bool)
					for _, tag := range tags {
						tagMap[tag] = true
					}
					for _, removed := range tt.wantRemoved {
						if tagMap[removed] {
							t.Errorf("Tag %q should have been removed but still exists in %v", removed, tags)
						}
					}
				}
			}
		})
	}
}

// TestMarketingFlowService_sendActionAssignAgent 测试分配客服动作
func TestMarketingFlowService_sendActionAssignAgent(t *testing.T) {
	database := setupMarketingFlowServiceTestDBWithCDP(t)
	service := NewMarketingFlowService(NewMarketingFlowDeps(database))

	now := time.Now()
	agent1 := &reachmodel.AgentStatus{
		AgentID:        1001,
		AgentName:      "客服A",
		Status:         "online",
		MaxSessions:    5,
		ActiveSessions: 0,
		LastActiveAt:   &now,
	}
	agent2 := &reachmodel.AgentStatus{
		AgentID:        1002,
		AgentName:      "客服B",
		Status:         "online",
		MaxSessions:    5,
		ActiveSessions: 2,
		LastActiveAt:   &now,
	}
	if err := database.Create(agent1).Error; err != nil {
		t.Fatalf("创建客服A 失败：%v", err)
	}
	if err := database.Create(agent2).Error; err != nil {
		t.Fatalf("创建客服B 失败：%v", err)
	}

	session := &reachmodel.CustomerSession{
		SessionID: "sess-assign-1",
		UserID:    "user-assign-1",
		UserName:  "测试用户",
		Platform:  "douyin",
		Status:    reachmodel.SessionStatusAIHandling,
	}
	if err := database.Create(session).Error; err != nil {
		t.Fatalf("创建会话失败：%v", err)
	}

	tests := []struct {
		name        string
		config      map[string]any
		data        map[string]any
		userID      string
		wantErr     bool
		wantAgentID uint
	}{
		{
			name:        "auto select agent (lowest load)",
			config:      map[string]any{},
			data:        map[string]any{},
			userID:      "user-assign-1",
			wantErr:     false,
			wantAgentID: 1001,
		},
		{
			name: "explicit agent_id",
			config: map[string]any{
				"agent_id": float64(1002),
			},
			data:        map[string]any{},
			userID:      "user-assign-1",
			wantErr:     false,
			wantAgentID: 1002,
		},
		{
			name: "via session_id in data",
			config: map[string]any{
				"agent_id": float64(1001),
			},
			data: map[string]any{
				"session_id": fmt.Sprintf("%d", session.ID),
			},
			userID:      "any-user",
			wantErr:     false,
			wantAgentID: 1001,
		},
		{
			name: "no active session for user",
			config: map[string]any{
				"agent_id": float64(1001),
			},
			data:    map[string]any{},
			userID:  "user-no-session",
			wantErr: true,
		},
		{
			name:    "no available agents",
			config:  map[string]any{},
			data:    map[string]any{},
			userID:  "user-no-agents",
			wantErr: true,
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_ = i
			result, err := service.sendActionAssignAgent(context.Background(), tt.config, tt.userID, tt.data)

			if (err != nil) != tt.wantErr {
				t.Errorf("sendActionAssignAgent() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if result == nil {
					t.Fatal("Expected non-nil result on success")
				}
				if success, ok := result["success"].(bool); !ok || !success {
					t.Errorf("Expected success=true, got %v", result["success"])
				}
				if tt.wantAgentID > 0 {
					agentID, _ := result["agent_id"].(uint)
					if agentID != tt.wantAgentID {
						t.Errorf("Expected agent_id=%d, got %d", tt.wantAgentID, agentID)
					}
				}
			}
		})
	}
}

// TestMarketingFlowService_sendActionAssignAgent_NoAgents 测试无可用客服场景
func TestMarketingFlowService_sendActionAssignAgent_NoAgents(t *testing.T) {
	database := setupMarketingFlowServiceTestDBWithCDP(t)
	service := NewMarketingFlowService(NewMarketingFlowDeps(database))

	session := &reachmodel.CustomerSession{
		SessionID: "sess-no-agent",
		UserID:    "user-no-agent",
		Platform:  "douyin",
		Status:    reachmodel.SessionStatusPending,
	}
	if err := database.Create(session).Error; err != nil {
		t.Fatalf("创建会话失败：%v", err)
	}

	_, err := service.sendActionAssignAgent(context.Background(), map[string]any{}, "user-no-agent", nil)
	if err == nil {
		t.Error("Expected error when no agents available, got nil")
	}
}

// TestMarketingFlowService_sendActionCreateTask 测试创建任务动作
func TestMarketingFlowService_sendActionCreateTask(t *testing.T) {
	database := setupMarketingFlowServiceTestDBWithCDP(t)
	service := NewMarketingFlowService(NewMarketingFlowDeps(database))

	tests := []struct {
		name           string
		config         map[string]any
		userID         string
		data           map[string]any
		wantErr        bool
		wantTitle      string
		wantAssigneeID uint
		wantModule     string
	}{
		{
			name: "create task with explicit assignee",
			config: map[string]any{
				"title":       "跟进高意向客户",
				"description": "客户多次咨询价格，请尽快跟进",
				"assignee_id": float64(100),
				"module":      "sales",
				"resource_id": "clue-123",
			},
			userID:         "user-200",
			data:           map[string]any{},
			wantErr:        false,
			wantTitle:      "跟进高意向客户",
			wantAssigneeID: 100,
			wantModule:     "sales",
		},
		{
			name: "create task fallback to user_id as assignee",
			config: map[string]any{
				"title":       "默认责任人任务",
				"description": "未指定 assignee_id 时使用 user_id",
			},
			userID:         "300",
			data:           map[string]any{},
			wantErr:        false,
			wantTitle:      "默认责任人任务",
			wantAssigneeID: 300,
			wantModule:     "marketing_flow",
		},
		{
			name: "missing title",
			config: map[string]any{
				"description": "no title",
			},
			userID:  "user-201",
			data:    map[string]any{},
			wantErr: true,
		},
		{
			name: "invalid assignee and user_id",
			config: map[string]any{
				"title": "任务",
			},
			userID:  "not-a-number",
			data:    map[string]any{},
			wantErr: true,
		},
		{
			name: "with extra data fields",
			config: map[string]any{
				"title":       "带上下文任务",
				"assignee_id": float64(400),
			},
			userID: "user-202",
			data: map[string]any{
				"clue_id":   "clue-456",
				"_internal": "should-be-excluded",
				"customer":  "Acme Inc",
			},
			wantErr:        false,
			wantTitle:      "带上下文任务",
			wantAssigneeID: 400,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			result, err := service.sendActionCreateTask(ctx, tt.config, tt.userID, tt.data)

			if (err != nil) != tt.wantErr {
				t.Errorf("sendActionCreateTask() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if result == nil {
					t.Fatal("Expected non-nil result on success")
				}
				if success, ok := result["success"].(bool); !ok || !success {
					t.Errorf("Expected success=true, got %v", result["success"])
				}

				taskID, ok := result["task_id"].(uint)
				if !ok || taskID == 0 {
					t.Errorf("Expected valid task_id, got %v", result["task_id"])
					return
				}

				logEntry, lerr := service.operationLogRepo.GetByID(ctx, taskID)
				if lerr != nil {
					t.Fatalf("GetByID(%d) failed: %v", taskID, lerr)
				}
				if logEntry.Action != "create" {
					t.Errorf("Expected Action=create, got %q", logEntry.Action)
				}
				if logEntry.Resource != "task" {
					t.Errorf("Expected Resource=task, got %q", logEntry.Resource)
				}
				if tt.wantTitle != "" && logEntry.NewValue != tt.wantTitle {
					t.Errorf("Expected NewValue=%q, got %q", tt.wantTitle, logEntry.NewValue)
				}
				if tt.wantAssigneeID > 0 && logEntry.UserID != tt.wantAssigneeID {
					t.Errorf("Expected UserID=%d, got %d", tt.wantAssigneeID, logEntry.UserID)
				}
				if tt.wantModule != "" && logEntry.Module != tt.wantModule {
					t.Errorf("Expected Module=%q, got %q", tt.wantModule, logEntry.Module)
				}
			}
		})
	}
}

// TestMarketingFlowService_sendActionSendSms 测试发送短信动作
func TestMarketingFlowService_sendActionSendSms(t *testing.T) {
	database := setupMarketingFlowServiceTestDBWithCDP(t)
	service := NewMarketingFlowService(NewMarketingFlowDeps(database))

	t.Run("sms sender not registered", func(t *testing.T) {
		origSender := smsSenderFunc
		smsSenderFunc = nil
		defer func() { smsSenderFunc = origSender }()

		_, err := service.sendActionSendSms(context.Background(),
			map[string]any{"phone": "13800138000", "content": "test"},
			"user-1", nil)
		if err == nil {
			t.Error("Expected error when smsSenderFunc is nil, got nil")
		}
	})

	t.Run("validation", func(t *testing.T) {

		var callCount int32
		mockSender := func(_ context.Context, phone, content string) error {
			atomic.AddInt32(&callCount, 1)
			return nil
		}
		origSender := smsSenderFunc
		smsSenderFunc = mockSender
		defer func() { smsSenderFunc = origSender }()

		tests := []struct {
			name    string
			config  map[string]any
			data    map[string]any
			wantErr bool
		}{
			{
				name:    "missing phone",
				config:  map[string]any{"content": "hello"},
				data:    map[string]any{},
				wantErr: true,
			},
			{
				name:    "missing content",
				config:  map[string]any{"phone": "13800138000"},
				data:    map[string]any{},
				wantErr: true,
			},
			{
				name:    "phone from data fallback",
				config:  map[string]any{"content": "hello"},
				data:    map[string]any{"phone": "13900139000"},
				wantErr: false,
			},
			{
				name:    "user_phone from data fallback",
				config:  map[string]any{"content": "hello"},
				data:    map[string]any{"user_phone": "13700137000"},
				wantErr: false,
			},
			{
				name:    "valid send",
				config:  map[string]any{"phone": "13800138000", "content": "您的验证码是 1234"},
				data:    map[string]any{},
				wantErr: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				atomic.StoreInt32(&callCount, 0)
				_, err := service.sendActionSendSms(context.Background(), tt.config, "user-1", tt.data)
				if (err != nil) != tt.wantErr {
					t.Errorf("sendActionSendSms() error = %v, wantErr %v", err, tt.wantErr)
				}
				if !tt.wantErr && atomic.LoadInt32(&callCount) != 1 {
					t.Errorf("Expected sender to be called once, got %d", atomic.LoadInt32(&callCount))
				}
			})
		}
	})

	t.Run("sender error propagation", func(t *testing.T) {
		origSender := smsSenderFunc
		smsSenderFunc = func(_ context.Context, phone, content string) error {
			return errors.New("provider timeout")
		}
		defer func() { smsSenderFunc = origSender }()

		_, err := service.sendActionSendSms(context.Background(),
			map[string]any{"phone": "13800138000", "content": "test"},
			"user-1", nil)
		if err == nil {
			t.Fatal("Expected error from sender, got nil")
		}
	})
}

// TestMarketingFlowService_sendActionUpdateLead 测试更新线索动作
func TestMarketingFlowService_sendActionUpdateLead(t *testing.T) {
	database := setupMarketingFlowServiceTestDBWithCDP(t)
	service := NewMarketingFlowService(NewMarketingFlowDeps(database))

	clue := &reachmodel.Clue{
		SourceID: "src-1",
		Account:  "test-account",
		Type:     1,
		IsVerify: 0,
		Name:     "原始名称",
		City:     "北京",
		Address:  "原始地址",
		Desc:     "原始描述",
	}
	if err := database.Create(clue).Error; err != nil {
		t.Fatalf("创建测试线索失败：%v", err)
	}

	tests := []struct {
		name        string
		config      map[string]any
		data        map[string]any
		userID      string
		wantErr     bool
		wantUpdates map[string]any
	}{
		{
			name: "update name and city",
			config: map[string]any{
				"clue_id": clue.ID,
				"fields": map[string]any{
					"name": "新名称",
					"city": "上海",
				},
			},
			data:    map[string]any{},
			userID:  "user-300",
			wantErr: false,
			wantUpdates: map[string]any{
				"name": "新名称",
				"city": "上海",
			},
		},
		{
			name: "update is_verify to int64",
			config: map[string]any{
				"clue_id": clue.ID,
				"fields": map[string]any{
					"is_verify": float64(1),
				},
			},
			data:    map[string]any{},
			userID:  "user-301",
			wantErr: false,
			wantUpdates: map[string]any{
				"is_verify": int64(1),
			},
		},
		{
			name: "update via data clue_id fallback",
			config: map[string]any{
				"fields": map[string]any{
					"desc": "从data获取clue_id",
				},
			},
			data: map[string]any{
				"clue_id": clue.ID,
			},
			userID:  "user-302",
			wantErr: false,
			wantUpdates: map[string]any{
				"desc": "从data获取clue_id",
			},
		},
		{
			name: "update via lead_id fallback",
			config: map[string]any{
				"fields": map[string]any{
					"address": "新地址",
				},
			},
			data: map[string]any{
				"lead_id": clue.ID,
			},
			userID:  "user-303",
			wantErr: false,
		},
		{
			name: "missing clue_id",
			config: map[string]any{
				"fields": map[string]any{"name": "新"},
			},
			data:    map[string]any{},
			userID:  "user-304",
			wantErr: true,
		},
		{
			name: "empty fields",
			config: map[string]any{
				"clue_id": clue.ID,
				"fields":  map[string]any{},
			},
			data:    map[string]any{},
			userID:  "user-305",
			wantErr: true,
		},
		{
			name: "fields with disallowed keys filtered out",
			config: map[string]any{
				"clue_id": clue.ID,
				"fields": map[string]any{
					"id":          "should-be-ignored",
					"create_time": 12345,
					"name":        "valid-update",
				},
			},
			data:    map[string]any{},
			userID:  "user-306",
			wantErr: false,
			wantUpdates: map[string]any{
				"name": "valid-update",
			},
		},
		{
			name: "nonexistent clue_id",
			config: map[string]any{
				"clue_id": "nonexistent-uuid",
				"fields": map[string]any{
					"name": "test",
				},
			},
			data:    map[string]any{},
			userID:  "user-307",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := service.sendActionUpdateLead(context.Background(), tt.config, tt.userID, tt.data)

			if (err != nil) != tt.wantErr {
				t.Errorf("sendActionUpdateLead() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if result == nil {
					t.Fatal("Expected non-nil result on success")
				}
				if success, ok := result["success"].(bool); !ok || !success {
					t.Errorf("Expected success=true, got %v", result["success"])
				}

				if len(tt.wantUpdates) > 0 {
					var updated reachmodel.Clue
					if err := database.First(&updated, "id = ?", clue.ID).Error; err != nil {
						t.Fatalf("查询更新后的线索失败：%v", err)
					}
					for k, v := range tt.wantUpdates {
						switch k {
						case "name":
							if updated.Name != v.(string) {
								t.Errorf("Field %s = %v, want %v", k, updated.Name, v)
							}
						case "city":
							if updated.City != v.(string) {
								t.Errorf("Field %s = %v, want %v", k, updated.City, v)
							}
						case "address":
							if updated.Address != v.(string) {
								t.Errorf("Field %s = %v, want %v", k, updated.Address, v)
							}
						case "desc":
							if updated.Desc != v.(string) {
								t.Errorf("Field %s = %v, want %v", k, updated.Desc, v)
							}
						case "is_verify":
							if expected, ok := v.(int64); ok && updated.IsVerify != expected {
								t.Errorf("Field %s = %d, want %d", k, updated.IsVerify, expected)
							}
						}
					}
				}
			}
		})
	}
}

// TestMarketingFlowService_sendActionCreateOrder 测试创建订单动作
func TestMarketingFlowService_sendActionCreateOrder(t *testing.T) {
	database := setupMarketingFlowServiceTestDBWithCDP(t)
	service := NewMarketingFlowService(NewMarketingFlowDeps(database))

	tests := []struct {
		name        string
		config      map[string]any
		data        map[string]any
		userID      string
		wantErr     bool
		wantPrice   string
		wantTgID    int64
		wantAccount string
		wantStatus  int
	}{
		{
			name: "create order with string price",
			config: map[string]any{
				"price":      "99.50",
				"tg_id":      float64(1001),
				"account_id": "acc-1",
			},
			data:        map[string]any{},
			userID:      "user-400",
			wantErr:     false,
			wantPrice:   "99.50",
			wantTgID:    1001,
			wantAccount: "acc-1",
			wantStatus:  0,
		},
		{
			name: "create order with float64 price",
			config: map[string]any{
				"price":      float64(199.99),
				"tg_id":      "1002",
				"account_id": "acc-2",
				"status":     float64(1),
			},
			data:        map[string]any{},
			userID:      "user-401",
			wantErr:     false,
			wantPrice:   "199.99",
			wantTgID:    1002,
			wantAccount: "acc-2",
			wantStatus:  1,
		},
		{
			name: "tg_id from data fallback",
			config: map[string]any{
				"price":      "50",
				"account_id": "acc-3",
			},
			data: map[string]any{
				"tg_id": float64(1003),
			},
			userID:      "user-402",
			wantErr:     false,
			wantPrice:   "50",
			wantTgID:    1003,
			wantAccount: "acc-3",
		},
		{
			name: "account_id from data fallback",
			config: map[string]any{
				"price": "30",
				"tg_id": float64(1004),
			},
			data: map[string]any{
				"account_id": "acc-from-data",
			},
			userID:      "user-403",
			wantErr:     false,
			wantPrice:   "30",
			wantTgID:    1004,
			wantAccount: "acc-from-data",
		},
		{
			name: "missing price",
			config: map[string]any{
				"tg_id":      float64(1005),
				"account_id": "acc-5",
			},
			data:    map[string]any{},
			userID:  "user-404",
			wantErr: true,
		},
		{
			name: "missing tg_id",
			config: map[string]any{
				"price":      "10",
				"account_id": "acc-6",
			},
			data:    map[string]any{},
			userID:  "user-405",
			wantErr: true,
		},
		{
			name: "missing account_id",
			config: map[string]any{
				"price": "10",
				"tg_id": float64(1007),
			},
			data:    map[string]any{},
			userID:  "user-406",
			wantErr: true,
		},
		{
			name: "invalid tg_id format",
			config: map[string]any{
				"price":      "10",
				"tg_id":      "not-a-number",
				"account_id": "acc-8",
			},
			data:    map[string]any{},
			userID:  "user-407",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			result, err := service.sendActionCreateOrder(ctx, tt.config, tt.userID, tt.data)

			if (err != nil) != tt.wantErr {
				t.Errorf("sendActionCreateOrder() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if result == nil {
					t.Fatal("Expected non-nil result on success")
				}
				if success, ok := result["success"].(bool); !ok || !success {
					t.Errorf("Expected success=true, got %v", result["success"])
				}

				orderID, ok := result["order_id"].(string)
				if !ok || orderID == "" {
					t.Errorf("Expected valid order_id, got %v", result["order_id"])
					return
				}

				order, oerr := service.orderRepo.GetByStringID(ctx, orderID)
				if oerr != nil {
					t.Fatalf("GetByStringID(%q) failed: %v", orderID, oerr)
				}
				if tt.wantPrice != "" && order.Price != tt.wantPrice {
					t.Errorf("Expected Price=%q, got %q", tt.wantPrice, order.Price)
				}
				if tt.wantTgID > 0 && order.TgID != tt.wantTgID {
					t.Errorf("Expected TgID=%d, got %d", tt.wantTgID, order.TgID)
				}
				if tt.wantAccount != "" && order.AccountID != tt.wantAccount {
					t.Errorf("Expected AccountID=%q, got %q", tt.wantAccount, order.AccountID)
				}
				if tt.wantStatus != 0 && int(order.Status) != tt.wantStatus {
					t.Errorf("Expected Status=%d, got %d", tt.wantStatus, int(order.Status))
				}
			}
		})
	}
}

// TestSetSmsSender 测试 SetSmsSender 函数注入机制
func TestSetSmsSender(t *testing.T) {
	origSender := smsSenderFunc
	defer func() { smsSenderFunc = origSender }()

	var callCount int32
	var mu sync.Mutex
	calls := make([][2]string, 0)

	testSender := func(ctx context.Context, phone, content string) error {
		atomic.AddInt32(&callCount, 1)
		mu.Lock()
		calls = append(calls, [2]string{phone, content})
		mu.Unlock()
		return nil
	}

	SetSmsSender(testSender)

	if smsSenderFunc == nil {
		t.Fatal("Expected smsSenderFunc to be set after SetSmsSender")
	}

	if err := smsSenderFunc(context.Background(), "13800138000", "hello"); err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("Expected 1 call, got %d", atomic.LoadInt32(&callCount))
	}

	mu.Lock()
	if len(calls) != 1 || calls[0][0] != "13800138000" || calls[0][1] != "hello" {
		t.Errorf("Unexpected call args: %v", calls)
	}
	mu.Unlock()

	SetSmsSender(nil)
	if smsSenderFunc != nil {
		t.Error("Expected smsSenderFunc to be nil after SetSmsSender(nil)")
	}
}

// TestFirstNonEmpty 测试 firstNonEmpty 辅助函数
func TestFirstNonEmpty(t *testing.T) {
	tests := []struct {
		name   string
		values []string
		want   string
	}{
		{"all empty", []string{"", "", ""}, ""},
		{"first non-empty", []string{"a", "b", "c"}, "a"},
		{"middle non-empty", []string{"", "b", "c"}, "b"},
		{"last non-empty", []string{"", "", "c"}, "c"},
		{"no values", []string{}, ""},
		{"single empty", []string{""}, ""},
		{"single non-empty", []string{"only"}, "only"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := firstNonEmpty(tt.values...)
			if got != tt.want {
				t.Errorf("firstNonEmpty(%v) = %q, want %q", tt.values, got, tt.want)
			}
		})
	}
}

var _ = time.Now
