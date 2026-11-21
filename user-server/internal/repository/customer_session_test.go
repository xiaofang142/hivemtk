package repository

import (
	"context"
	"fmt"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"
	"reflect"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// TestCustomerListQueryContract 无需连接 PG，锁定真实 PG 方言的分页与内外层权限谓词。
func TestCustomerListQueryContract(t *testing.T) {
	database, err := gorm.Open(postgres.New(postgres.Config{DSN: "host=127.0.0.1 port=8217 user=contract dbname=contract sslmode=disable"}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	repo := NewCustomerSessionRepositoryWithDB(database)
	ctx := tenant.WithTenantID(context.Background(), 71)
	agentID := uint(9)
	keyword := `  A%_\' OR 1=1  `
	pattern := `%A\%\_\\' OR 1=1%`

	var total int64
	count := repo.customerListCandidates(ctx, keyword, &agentID).Distinct("user_id").Count(&total)
	if count.Error != nil {
		t.Fatal(count.Error)
	}
	if !strings.Contains(count.Statement.SQL.String(), `COUNT(DISTINCT("user_id"))`) {
		t.Fatalf("必须计数唯一客户，实际 SQL=%s", count.Statement.SQL.String())
	}
	wantCountVars := []any{uint(71), "", uint(9), pattern, pattern, pattern}
	if !reflect.DeepEqual(count.Statement.Vars, wantCountVars) {
		t.Fatalf("字面搜索参数错误：实际 %#v，期望 %#v", count.Statement.Vars, wantCountVars)
	}

	customers := repo.customerListCandidates(ctx, keyword, &agentID).Select("user_id").Distinct().Order("user_id ASC").Offset(20).Limit(20)
	var rows []*model.CustomerSession
	query := repo.customerListScope(ctx, &agentID).Where("user_id IN (?)", customers).Order("user_id ASC, created_at ASC, id ASC").Find(&rows)
	sql := query.Statement.SQL.String()
	for _, fragment := range []string{`SELECT DISTINCT "user_id"`, `ORDER BY user_id ASC LIMIT`, `OFFSET`, `ORDER BY user_id ASC, created_at ASC, id ASC`} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("分页 SQL 缺少 %q：%s", fragment, sql)
		}
	}
	for _, fragment := range []string{"tenant_id =", "agent_id = 0 OR agent_id =", "deleted_at"} {
		if strings.Count(sql, fragment) != 2 {
			t.Fatalf("内外层必须都含 %q：%s", fragment, sql)
		}
	}
	if strings.Contains(sql, "OR 1=1") || strings.Count(sql, "ILIKE") != 3 {
		t.Fatalf("搜索必须参数化且匹配三个字段：%s", sql)
	}
	wantVars := append([]any{uint(71), "", uint(9)}, wantCountVars...)
	wantVars = append(wantVars, 20, 20)
	if !reflect.DeepEqual(query.Statement.Vars, wantVars) {
		t.Fatalf("内外层租户、坐席或分页参数错误：实际 %#v，期望 %#v", query.Statement.Vars, wantVars)
	}
	admin := repo.customerListScope(ctx, nil).Find(&rows)
	if strings.Contains(admin.Statement.SQL.String(), "agent_id") || !reflect.DeepEqual(admin.Statement.Vars, []any{uint(71), ""}) {
		t.Fatalf("管理员仍需限租户但不限坐席：%s %#v", admin.Statement.SQL.String(), admin.Statement.Vars)
	}
}

// TestCustomerListMissingTenant 无租户必须在执行数据库操作前拒绝，不能退为全局查询。
func TestCustomerListMissingTenant(t *testing.T) {
	previous := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previous) })
	// 真实 DryRun 句柄避免连接，缺上下文分支不能到 SQL 执行。
	database, err := gorm.Open(postgres.New(postgres.Config{DSN: "host=127.0.0.1 port=8217 user=contract dbname=contract sslmode=disable"}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	repo := NewCustomerSessionRepositoryWithDB(database)
	for _, ctx := range []context.Context{nil, context.Background(), tenant.WithSystemScope(context.Background())} {
		if _, _, err := repo.ListCustomerPage(ctx, 1, 20, "", nil); err == nil || !strings.Contains(err.Error(), "requires tenant identity") {
			t.Fatalf("缺租户/系统作用域必须拒绝，实际 %v", err)
		}
	}
}

// TestCustomerListPageIntegration 覆盖重复会话、完整客户页、搜索及租户/坐席联合隔离。
func TestCustomerListPageIntegration(t *testing.T) {
	database := testutil.NewTestDB(t, &model.CustomerSession{})
	previous := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previous) })
	ctx := tenant.WithTenantID(context.Background(), 71)
	ctxB := tenant.WithTenantID(context.Background(), 72)
	base := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	seed := func(ctx context.Context, user string, n int, agentID uint, name string) {
		t.Helper()
		for i := 0; i < n; i++ {
			row := &model.CustomerSession{SessionID: fmt.Sprintf("%s-%d-%d", user, agentID, i), UserID: user, UserName: name, UserPhone: "13800138000", UserEmail: "Target@Example.test", AgentID: agentID, CreatedAt: base.Add(time.Duration(i) * time.Minute)}
			if err := database.WithContext(ctx).Create(row).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	seed(ctx, "a-repeat", 25, 9, "重复客户")
	for i := 0; i < 20; i++ {
		seed(ctx, fmt.Sprintf("b-%02d", i), 1, 0, fmt.Sprintf("客户%02d", i))
	}
	seed(ctx, "z-target", 2, 9, `独有%_\客户`)
	seed(ctx, "z-target", 1, 88, "隐藏历史")
	seed(ctx, "hidden", 1, 88, "隐藏客户")
	seed(ctxB, "z-target", 1, 9, "其他租户")
	repo := NewCustomerSessionRepositoryWithDB(database)
	agentID := uint(9)
	first, total, err := repo.ListCustomerPage(ctx, 1, 20, "", &agentID)
	if err != nil || total != 22 || len(first) != 44 {
		t.Fatalf("第一页必须20客户/44会话且total=22，实际 rows=%d total=%d err=%v", len(first), total, err)
	}
	seen := map[string]bool{}
	for _, row := range first {
		seen[row.UserID] = true
	}
	if len(seen) != 20 {
		t.Fatalf("第一页客户数实际 %d，期望20", len(seen))
	}
	second, total, err := repo.ListCustomerPage(ctx, 2, 20, "", &agentID)
	if err != nil || total != 22 || len(second) != 3 {
		t.Fatalf("第二页必须2客户/3会话且total=22，实际 rows=%d total=%d err=%v", len(second), total, err)
	}
	for _, row := range second {
		if seen[row.UserID] || row.TenantID != 71 || (row.AgentID != 0 && row.AgentID != 9) {
			t.Fatalf("跨页重复或越权会话：%+v", row)
		}
	}
	for _, keyword := range []string{`%_\`, " TARGET@example.TEST ", "13800138", "隐藏历史", "其他租户", "不存在"} {
		rows, total, err := repo.ListCustomerPage(ctx, 1, 20, keyword, &agentID)
		wantTotal, wantRows := int64(1), 2
		if keyword == "13800138" {
			wantTotal, wantRows = 22, 44
		} else if keyword == " TARGET@example.TEST " {
			wantTotal, wantRows = 22, 44
		} else if keyword == "隐藏历史" || keyword == "其他租户" || keyword == "不存在" {
			wantTotal, wantRows = 0, 0
		}
		if err != nil || total != wantTotal || len(rows) != wantRows {
			t.Fatalf("搜索 %q：rows=%d total=%d err=%v，期望%d/%d", keyword, len(rows), total, err, wantRows, wantTotal)
		}
	}
	empty, total, err := repo.ListCustomerPage(ctx, 3, 20, "", &agentID)
	if err != nil || len(empty) != 0 || total != 22 {
		t.Fatalf("越界页必须空且保留total=22，实际%d/%d/%v", len(empty), total, err)
	}
	admin, total, err := repo.ListCustomerPage(ctx, 1, 100, "", nil)
	if err != nil || total != 23 || len(admin) != 49 {
		t.Fatalf("管理员只能看到租户71全部23客户/49会话，实际%d/%d/%v", len(admin), total, err)
	}
}

func setupCustomerSessionTestDB(t *testing.T) *gorm.DB {
	database := testutil.NewTestDB(t,
		&model.CustomerSession{},
		&model.SessionMessage{},
		&model.AgentStatus{},
		&model.AISuggestion{},
		&model.QuickReply{},
		&model.SessionTag{},
	)
	db.SetTestDB(database)
	return database
}

func setupCustomerSessionRepositories(t *testing.T) (
	*CustomerSessionRepository,
	*SessionMessageRepository,
	*AgentStatusRepository,
	*AISuggestionRepository,
	*QuickReplyRepository,
	*SessionTagRepository) {

	setupCustomerSessionTestDB(t)
	database := db.GetDB()

	return &CustomerSessionRepository{db: database},
		&SessionMessageRepository{db: database},
		&AgentStatusRepository{db: database},
		&AISuggestionRepository{db: database},
		&QuickReplyRepository{db: database},
		&SessionTagRepository{db: database}
}

// TestCustomerSessionRepository_Create 测试创建会话
func TestCustomerSessionRepository_Create(t *testing.T) {
	sessionRepo, _, _, _, _, _ := setupCustomerSessionRepositories(t)
	ctx := context.Background()

	tests := []struct {
		name    string
		session *model.CustomerSession
		wantErr bool
	}{
		{
			name: "create session success",
			session: &model.CustomerSession{
				SessionID:   "session-001",
				Platform:    model.PlatformWeChat,
				AccountID:   "account-123",
				UserID:      "user-123",
				UserName:    "Test User",
				Status:      model.SessionStatusPending,
				HandlerType: model.HandlerTypeAI,
				Priority:    0,
			},
			wantErr: false,
		},
		{
			name: "create session with agent assignment",
			session: &model.CustomerSession{
				SessionID:   "session-002",
				Platform:    model.PlatformWeb,
				AccountID:   "account-456",
				UserID:      "user-456",
				AgentID:     1,
				AgentName:   "Agent Smith",
				Status:      model.SessionStatusHumanHandling,
				HandlerType: model.HandlerTypeHuman,
				Priority:    2,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := sessionRepo.Create(ctx, tt.session)

			if (err != nil) != tt.wantErr {
				t.Errorf("Create() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr && tt.session.ID == 0 {
				t.Error("Expected session ID to be set after creation")
			}
		})
	}
}

// TestCustomerSessionRepository_Update 测试更新会话
func TestCustomerSessionRepository_Update(t *testing.T) {
	sessionRepo, _, _, _, _, _ := setupCustomerSessionRepositories(t)
	ctx := context.Background()

	session := &model.CustomerSession{
		SessionID: "session-update",
		Platform:  model.PlatformWeChat,
		UserID:    "user-123",
		Status:    model.SessionStatusPending,
	}
	sessionRepo.Create(ctx, session)

	tests := []struct {
		name       string
		updateFunc func(*model.CustomerSession)
		wantErr    bool
	}{
		{
			name: "update status and handler",
			updateFunc: func(s *model.CustomerSession) {
				s.Status = model.SessionStatusAIHandling
				s.HandlerType = model.HandlerTypeAI
			},
			wantErr: false,
		},
		{
			name: "update agent assignment",
			updateFunc: func(s *model.CustomerSession) {
				s.AgentID = 1
				s.AgentName = "Agent Smith"
				s.Status = model.SessionStatusHumanHandling
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.updateFunc(session)
			err := sessionRepo.Update(ctx, session)

			if (err != nil) != tt.wantErr {
				t.Errorf("Update() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				updated, _ := sessionRepo.GetByID(ctx, session.ID)
				if updated.Status != session.Status {
					t.Errorf("Expected status '%s', got '%s'", session.Status, updated.Status)
				}
			}
		})
	}
}

// TestCustomerSessionRepository_GetByID 测试根据 ID 获取会话
func TestCustomerSessionRepository_GetByID(t *testing.T) {
	sessionRepo, _, _, _, _, _ := setupCustomerSessionRepositories(t)
	ctx := context.Background()

	session := &model.CustomerSession{
		SessionID: "session-getbyid",
		Platform:  model.PlatformWeChat,
		UserID:    "user-123",
		UserName:  "Test User",
		Status:    model.SessionStatusPending,
	}
	sessionRepo.Create(ctx, session)

	tests := []struct {
		name    string
		id      uint
		wantErr bool
	}{
		{
			name:    "get existing session",
			id:      session.ID,
			wantErr: false,
		},
		{
			name:    "get non-existing session",
			id:      99999,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := sessionRepo.GetByID(ctx, tt.id)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetByID() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				if result.ID != tt.id {
					t.Errorf("Expected ID %d, got %d", tt.id, result.ID)
				}
				if result.SessionID != "session-getbyid" {
					t.Errorf("Expected SessionID 'session-getbyid', got '%s'", result.SessionID)
				}
			}
		})
	}
}

// TestCustomerSessionRepository_GetBySessionID 测试根据 SessionID 获取会话
func TestCustomerSessionRepository_GetBySessionID(t *testing.T) {
	sessionRepo, _, _, _, _, _ := setupCustomerSessionRepositories(t)
	ctx := context.Background()

	session := &model.CustomerSession{
		SessionID: "unique-session-id",
		Platform:  model.PlatformWeChat,
		UserID:    "user-123",
		Status:    model.SessionStatusPending,
	}
	sessionRepo.Create(ctx, session)

	tests := []struct {
		name      string
		sessionID string
		wantErr   bool
	}{
		{
			name:      "get existing session",
			sessionID: "unique-session-id",
			wantErr:   false,
		},
		{
			name:      "get non-existing session",
			sessionID: "non-existing",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := sessionRepo.GetBySessionID(context.Background(), tt.sessionID)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetBySessionID() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				if result.SessionID != tt.sessionID {
					t.Errorf("Expected SessionID '%s', got '%s'", tt.sessionID, result.SessionID)
				}
			}
		})
	}
}

// TestCustomerSessionRepository_GetPendingSessions 测试获取待处理会话
func TestCustomerSessionRepository_GetPendingSessions(t *testing.T) {
	sessionRepo, _, _, _, _, _ := setupCustomerSessionRepositories(t)
	ctx := context.Background()

	sessionRepo.Create(ctx, &model.CustomerSession{
		SessionID: "pending-1",
		Platform:  model.PlatformWeChat,
		UserID:    "user-1",
		Status:    model.SessionStatusPending,
		Priority:  1,
	})
	sessionRepo.Create(ctx, &model.CustomerSession{
		SessionID: "ai-handling-1",
		Platform:  model.PlatformWeChat,
		UserID:    "user-2",
		Status:    model.SessionStatusAIHandling,
		Priority:  2,
	})
	sessionRepo.Create(ctx, &model.CustomerSession{
		SessionID: "human-handling-1",
		Platform:  model.PlatformWeChat,
		UserID:    "user-3",
		Status:    model.SessionStatusHumanHandling,
		Priority:  0,
	})

	pending, err := sessionRepo.GetPendingSessions(context.Background())
	if err != nil {
		t.Errorf("GetPendingSessions() error = %v", err)
	}

	if len(pending) != 2 {
		t.Errorf("Expected 2 pending sessions, got %d", len(pending))
	}

	if len(pending) == 2 && pending[0].Priority < pending[1].Priority {
		t.Error("Expected pending sessions to be ordered by priority DESC")
	}
}

// TestCustomerSessionRepository_GetAgentSessions 测试获取客服的活跃会话
func TestCustomerSessionRepository_GetAgentSessions(t *testing.T) {
	sessionRepo, _, _, _, _, _ := setupCustomerSessionRepositories(t)
	ctx := context.Background()

	agentID := uint(1)

	sessionRepo.Create(ctx, &model.CustomerSession{
		SessionID: "agent-active-1",
		Platform:  model.PlatformWeChat,
		UserID:    "user-1",
		AgentID:   agentID,
		Status:    model.SessionStatusHumanHandling,
	})
	sessionRepo.Create(ctx, &model.CustomerSession{
		SessionID: "agent-waiting-1",
		Platform:  model.PlatformWeChat,
		UserID:    "user-2",
		AgentID:   agentID,
		Status:    model.SessionStatusWaiting,
	})

	sessionRepo.Create(ctx, &model.CustomerSession{
		SessionID: "agent-resolved-1",
		Platform:  model.PlatformWeChat,
		UserID:    "user-3",
		AgentID:   agentID,
		Status:    model.SessionStatusResolved,
	})

	sessions, err := sessionRepo.GetAgentSessions(context.Background(), agentID)
	if err != nil {
		t.Errorf("GetAgentSessions() error = %v", err)
	}

	if len(sessions) != 2 {
		t.Errorf("Expected 2 active sessions, got %d", len(sessions))
	}
}

// TestCustomerSessionRepository_UpdateStatus 测试更新会话状态
func TestCustomerSessionRepository_UpdateStatus(t *testing.T) {
	sessionRepo, _, _, _, _, _ := setupCustomerSessionRepositories(t)
	ctx := context.Background()

	session := &model.CustomerSession{
		SessionID: "status-update-test",
		Platform:  model.PlatformWeChat,
		UserID:    "user-123",
		Status:    model.SessionStatusPending,
	}
	sessionRepo.Create(ctx, session)

	tests := []struct {
		name      string
		newStatus model.SessionStatus
		wantErr   bool
	}{
		{
			name:      "update to ai_handling",
			newStatus: model.SessionStatusAIHandling,
			wantErr:   false,
		},
		{
			name:      "update to human_handling",
			newStatus: model.SessionStatusHumanHandling,
			wantErr:   false,
		},
		{
			name:      "update to resolved",
			newStatus: model.SessionStatusResolved,
			wantErr:   false,
		},
		{
			name:      "update to closed",
			newStatus: model.SessionStatusClosed,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := sessionRepo.UpdateStatus(context.Background(), session.ID, tt.newStatus)

			if (err != nil) != tt.wantErr {
				t.Errorf("UpdateStatus() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				updated, _ := sessionRepo.GetByID(ctx, session.ID)
				if updated.Status != tt.newStatus {
					t.Errorf("Expected status '%s', got '%s'", tt.newStatus, updated.Status)
				}
			}
		})
	}
}

// TestCustomerSessionRepository_AssignAgent 测试分配客服
func TestCustomerSessionRepository_AssignAgent(t *testing.T) {
	sessionRepo, _, _, _, _, _ := setupCustomerSessionRepositories(t)
	ctx := context.Background()

	session := &model.CustomerSession{
		SessionID:   "agent-assign-test",
		Platform:    model.PlatformWeChat,
		UserID:      "user-123",
		Status:      model.SessionStatusPending,
		HandlerType: model.HandlerTypeAI,
	}
	sessionRepo.Create(ctx, session)

	err := sessionRepo.AssignAgent(context.Background(), session.ID, 1, "Agent Smith")
	if err != nil {
		t.Errorf("AssignAgent() error = %v", err)
	}

	updated, _ := sessionRepo.GetByID(ctx, session.ID)
	if updated.AgentID != 1 {
		t.Errorf("Expected AgentID 1, got %d", updated.AgentID)
	}
	if updated.AgentName != "Agent Smith" {
		t.Errorf("Expected AgentName 'Agent Smith', got '%s'", updated.AgentName)
	}
	if updated.HandlerType != model.HandlerTypeHuman {
		t.Errorf("Expected HandlerType human, got '%s'", updated.HandlerType)
	}
	if updated.Status != model.SessionStatusHumanHandling {
		t.Errorf("Expected status human_handling, got '%s'", updated.Status)
	}
}

// TestCustomerSessionRepository_UpdateLastMessage 测试更新最后消息
func TestCustomerSessionRepository_UpdateLastMessage(t *testing.T) {
	sessionRepo, _, _, _, _, _ := setupCustomerSessionRepositories(t)
	ctx := context.Background()

	session := &model.CustomerSession{
		SessionID:    "message-update-test",
		Platform:     model.PlatformWeChat,
		UserID:       "user-123",
		Status:       model.SessionStatusPending,
		MessageCount: 0,
	}
	sessionRepo.Create(ctx, session)

	err := sessionRepo.UpdateLastMessage(context.Background(), session.ID, "Hello, this is a test message", "user")
	if err != nil {
		t.Errorf("UpdateLastMessage() error = %v", err)
	}

	updated, _ := sessionRepo.GetByID(ctx, session.ID)
	if updated.LastMessage != "Hello, this is a test message" {
		t.Errorf("Expected LastMessage 'Hello, this is a test message', got '%s'", updated.LastMessage)
	}
	if updated.LastMessageBy != "user" {
		t.Errorf("Expected LastMessageBy 'user', got '%s'", updated.LastMessageBy)
	}
	if updated.MessageCount != 1 {
		t.Errorf("Expected MessageCount 1, got %d", updated.MessageCount)
	}
	if updated.LastMessageAt == nil {
		t.Error("Expected LastMessageAt to be set")
	}
}

// TestCustomerSessionRepository_IncrementAIReplyCount 测试增加 AI 回复计数
func TestCustomerSessionRepository_IncrementAIReplyCount(t *testing.T) {
	sessionRepo, _, _, _, _, _ := setupCustomerSessionRepositories(t)
	ctx := context.Background()

	session := &model.CustomerSession{
		SessionID:    "ai-reply-test",
		Platform:     model.PlatformWeChat,
		UserID:       "user-123",
		Status:       model.SessionStatusAIHandling,
		AIReplyCount: 0,
	}
	sessionRepo.Create(ctx, session)

	for i := 0; i < 3; i++ {
		err := sessionRepo.IncrementAIReplyCount(context.Background(), session.ID)
		if err != nil {
			t.Errorf("IncrementAIReplyCount() error = %v", err)
		}
	}

	updated, _ := sessionRepo.GetByID(ctx, session.ID)
	if updated.AIReplyCount != 3 {
		t.Errorf("Expected AIReplyCount 3, got %d", updated.AIReplyCount)
	}
}

// TestCustomerSessionRepository_IncrementHumanReplyCount 测试增加人工回复计数
func TestCustomerSessionRepository_IncrementHumanReplyCount(t *testing.T) {
	sessionRepo, _, _, _, _, _ := setupCustomerSessionRepositories(t)
	ctx := context.Background()

	session := &model.CustomerSession{
		SessionID:       "human-reply-test",
		Platform:        model.PlatformWeChat,
		UserID:          "user-123",
		Status:          model.SessionStatusHumanHandling,
		HumanReplyCount: 0,
	}
	sessionRepo.Create(ctx, session)

	for i := 0; i < 2; i++ {
		err := sessionRepo.IncrementHumanReplyCount(context.Background(), session.ID)
		if err != nil {
			t.Errorf("IncrementHumanReplyCount() error = %v", err)
		}
	}

	updated, _ := sessionRepo.GetByID(ctx, session.ID)
	if updated.HumanReplyCount != 2 {
		t.Errorf("Expected HumanReplyCount 2, got %d", updated.HumanReplyCount)
	}
}

// TestSessionMessageRepository_Create 测试创建消息
func TestSessionMessageRepository_Create(t *testing.T) {
	_, messageRepo, _, _, _, _ := setupCustomerSessionRepositories(t)
	ctx := context.Background()

	tests := []struct {
		name    string
		message *model.SessionMessage
		wantErr bool
	}{
		{
			name: "create user message",
			message: &model.SessionMessage{
				SessionID:   "session-001",
				Content:     "Hello, I need help",
				ContentType: model.MessageTypeText,
				SenderType:  "user",
				SenderID:    "user-123",
				SenderName:  "Test User",
			},
			wantErr: false,
		},
		{
			name: "create AI message with confidence",
			message: &model.SessionMessage{
				SessionID:    "session-001",
				Content:      "How can I help you?",
				ContentType:  model.MessageTypeText,
				SenderType:   "ai",
				SenderID:     "ai-assistant",
				SenderName:   "AI Assistant",
				AIConfidence: 0.95,
				AISource:     "llm",
			},
			wantErr: false,
		},
		{
			name: "create agent message",
			message: &model.SessionMessage{
				SessionID:   "session-001",
				Content:     "Let me assist you with that",
				ContentType: model.MessageTypeText,
				SenderType:  "agent",
				SenderID:    "agent-1",
				SenderName:  "Agent Smith",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := messageRepo.Create(ctx, tt.message)

			if (err != nil) != tt.wantErr {
				t.Errorf("Create() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr && tt.message.ID == 0 {
				t.Error("Expected message ID to be set after creation")
			}
		})
	}
}

// TestSessionMessageRepository_GetBySessionID 测试获取会话消息列表
func TestSessionMessageRepository_GetBySessionID(t *testing.T) {
	_, messageRepo, _, _, _, _ := setupCustomerSessionRepositories(t)
	ctx := context.Background()

	for i := 1; i <= 5; i++ {
		messageRepo.Create(ctx, &model.SessionMessage{
			SessionID:  "session-messages",
			Content:    string(rune('A' + i - 1)),
			SenderType: "user",
		})
	}

	tests := []struct {
		name      string
		sessionID string
		page      int
		pageSize  int
		wantCount int
	}{
		{
			name:      "get first page",
			sessionID: "session-messages",
			page:      1,
			pageSize:  3,
			wantCount: 3,
		},
		{
			name:      "get second page",
			sessionID: "session-messages",
			page:      2,
			pageSize:  3,
			wantCount: 2,
		},
		{
			name:      "get non-existing session",
			sessionID: "non-existing",
			page:      1,
			pageSize:  10,
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, total, err := messageRepo.GetBySessionID(context.Background(), tt.sessionID, tt.page, tt.pageSize)

			if err != nil {
				t.Errorf("GetBySessionID() error = %v", err)
			}

			if len(results) != tt.wantCount {
				t.Errorf("Expected %d messages, got %d", tt.wantCount, len(results))
			}

			if tt.sessionID == "session-messages" && int(total) != 5 {
				t.Errorf("Expected total 5, got %d", total)
			}
		})
	}
}

// TestSessionMessageRepository_MarkAsRead 测试标记消息已读
func TestSessionMessageRepository_MarkAsRead(t *testing.T) {
	_, messageRepo, _, _, _, _ := setupCustomerSessionRepositories(t)
	ctx := context.Background()

	now := time.Now()

	messageRepo.Create(ctx, &model.SessionMessage{
		SessionID:  "session-read-test",
		Content:    "Message 1",
		SenderType: "user",
		IsRead:     false,
		CreatedAt:  now.Add(-2 * time.Minute),
	})
	messageRepo.Create(ctx, &model.SessionMessage{
		SessionID:  "session-read-test",
		Content:    "Message 2",
		SenderType: "user",
		IsRead:     false,
		CreatedAt:  now.Add(-1 * time.Minute),
	})

	err := messageRepo.MarkAsRead(context.Background(), "session-read-test", now)
	if err != nil {
		t.Errorf("MarkAsRead() error = %v", err)
	}

	var messages []*model.SessionMessage
	db.GetDB().Where("session_id = ?", "session-read-test").Find(&messages)

	for _, msg := range messages {
		if !msg.IsRead {
			t.Error("Expected messages to be marked as read")
		}
		if msg.ReadAt == nil {
			t.Error("Expected ReadAt to be set")
		}
	}
}

// TestAgentStatusRepository_Create 测试创建客服状态
func TestAgentStatusRepository_Create(t *testing.T) {
	_, _, agentRepo, _, _, _ := setupCustomerSessionRepositories(t)
	ctx := context.Background()

	tests := []struct {
		name    string
		status  *model.AgentStatus
		wantErr bool
	}{
		{
			name: "create online agent",
			status: &model.AgentStatus{
				AgentID:        1,
				AgentName:      "Agent Smith",
				Status:         "online",
				MaxSessions:    5,
				ActiveSessions: 0,
			},
			wantErr: false,
		},
		{
			name: "create busy agent",
			status: &model.AgentStatus{
				AgentID:        2,
				AgentName:      "Agent Johnson",
				Status:         "busy",
				MaxSessions:    3,
				ActiveSessions: 3,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := agentRepo.Create(ctx, tt.status)

			if (err != nil) != tt.wantErr {
				t.Errorf("Create() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr && tt.status.ID == 0 {
				t.Error("Expected status ID to be set after creation")
			}
		})
	}
}

// TestAgentStatusRepository_GetByAgentID 测试根据客服 ID 获取状态
func TestAgentStatusRepository_GetByAgentID(t *testing.T) {
	_, _, agentRepo, _, _, _ := setupCustomerSessionRepositories(t)
	ctx := context.Background()

	status := &model.AgentStatus{
		AgentID:     42,
		AgentName:   "Agent 42",
		Status:      "online",
		MaxSessions: 5,
	}
	agentRepo.Create(ctx, status)

	tests := []struct {
		name    string
		agentID uint
		wantErr bool
	}{
		{
			name:    "get existing agent",
			agentID: 42,
			wantErr: false,
		},
		{
			name:    "get non-existing agent",
			agentID: 99999,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := agentRepo.GetByAgentID(context.Background(), tt.agentID)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetByAgentID() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				if result.AgentID != tt.agentID {
					t.Errorf("Expected AgentID %d, got %d", tt.agentID, result.AgentID)
				}
			}
		})
	}
}

// TestAgentStatusRepository_GetOnlineAgents 测试获取在线客服
func TestAgentStatusRepository_GetOnlineAgents(t *testing.T) {
	_, _, agentRepo, _, _, _ := setupCustomerSessionRepositories(t)
	ctx := context.Background()
	now := time.Now()

	agentRepo.Create(ctx, &model.AgentStatus{
		AgentID:        1,
		AgentName:      "Agent Online",
		Status:         "online",
		MaxSessions:    5,
		ActiveSessions: 2,
		LastActiveAt:   &now,
	})
	agentRepo.Create(ctx, &model.AgentStatus{
		AgentID:        2,
		AgentName:      "Agent Busy",
		Status:         "busy",
		MaxSessions:    5,
		ActiveSessions: 3,
		LastActiveAt:   &now,
	})
	agentRepo.Create(ctx, &model.AgentStatus{
		AgentID:        3,
		AgentName:      "Agent Away",
		Status:         "away",
		MaxSessions:    5,
		ActiveSessions: 0,
		LastActiveAt:   &now,
	})
	agentRepo.Create(ctx, &model.AgentStatus{
		AgentID:        4,
		AgentName:      "Agent Offline",
		Status:         "offline",
		MaxSessions:    5,
		ActiveSessions: 0,
		LastActiveAt:   &now,
	})
	agentRepo.Create(ctx, &model.AgentStatus{
		AgentID:        5,
		AgentName:      "Agent Full",
		Status:         "online",
		MaxSessions:    3,
		ActiveSessions: 3,
		LastActiveAt:   &now,
	})

	onlineAgents, err := agentRepo.GetOnlineAgents(context.Background())
	if err != nil {
		t.Errorf("GetOnlineAgents() error = %v", err)
	}

	if len(onlineAgents) != 2 {
		t.Errorf("Expected 2 available agents, got %d", len(onlineAgents))
	}
}

// TestAgentStatusRepository_UpdateStatus 测试更新客服状态
func TestAgentStatusRepository_UpdateStatus(t *testing.T) {
	_, _, agentRepo, _, _, _ := setupCustomerSessionRepositories(t)
	ctx := context.Background()

	status := &model.AgentStatus{
		AgentID:   1,
		AgentName: "Agent Smith",
		Status:    "online",
	}
	agentRepo.Create(ctx, status)

	tests := []struct {
		name      string
		newStatus string
		wantErr   bool
	}{
		{
			name:      "update to busy",
			newStatus: "busy",
			wantErr:   false,
		},
		{
			name:      "update to offline",
			newStatus: "offline",
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := agentRepo.UpdateStatus(context.Background(), 1, tt.newStatus)

			if (err != nil) != tt.wantErr {
				t.Errorf("UpdateStatus() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				updated, _ := agentRepo.GetByAgentID(context.Background(), 1)
				if updated.Status != tt.newStatus {
					t.Errorf("Expected status '%s', got '%s'", tt.newStatus, updated.Status)
				}
			}
		})
	}
}

// TestAgentStatusRepository_IncrementActiveSessions 测试增加活跃会话数
func TestAgentStatusRepository_IncrementActiveSessions(t *testing.T) {
	_, _, agentRepo, _, _, _ := setupCustomerSessionRepositories(t)
	ctx := context.Background()

	status := &model.AgentStatus{
		AgentID:        1,
		AgentName:      "Agent Smith",
		Status:         "online",
		MaxSessions:    5,
		ActiveSessions: 0,
		TodaySessions:  0,
	}
	agentRepo.Create(ctx, status)

	for i := 0; i < 3; i++ {
		err := agentRepo.IncrementActiveSessions(context.Background(), 1)
		if err != nil {
			t.Errorf("IncrementActiveSessions() error = %v", err)
		}
	}

	updated, _ := agentRepo.GetByAgentID(context.Background(), 1)
	if updated.ActiveSessions != 3 {
		t.Errorf("Expected ActiveSessions 3, got %d", updated.ActiveSessions)
	}
	if updated.TodaySessions != 3 {
		t.Errorf("Expected TodaySessions 3, got %d", updated.TodaySessions)
	}
}

// TestAgentStatusRepository_DecrementActiveSessions 测试减少活跃会话数
func TestAgentStatusRepository_DecrementActiveSessions(t *testing.T) {
	_, _, agentRepo, _, _, _ := setupCustomerSessionRepositories(t)
	ctx := context.Background()

	status := &model.AgentStatus{
		AgentID:        1,
		AgentName:      "Agent Smith",
		Status:         "online",
		MaxSessions:    5,
		ActiveSessions: 3,
	}
	agentRepo.Create(ctx, status)

	for i := 0; i < 2; i++ {
		err := agentRepo.DecrementActiveSessions(context.Background(), 1)
		if err != nil {
			t.Errorf("DecrementActiveSessions() error = %v", err)
		}
	}

	updated, _ := agentRepo.GetByAgentID(context.Background(), 1)
	if updated.ActiveSessions != 1 {
		t.Errorf("Expected ActiveSessions 1, got %d", updated.ActiveSessions)
	}
}

// TestAgentStatusRepository_IncrementTodayMessages 测试增加今日消息数
func TestAgentStatusRepository_IncrementTodayMessages(t *testing.T) {
	_, _, agentRepo, _, _, _ := setupCustomerSessionRepositories(t)
	ctx := context.Background()

	status := &model.AgentStatus{
		AgentID:       1,
		AgentName:     "Agent Smith",
		Status:        "online",
		TodayMessages: 0,
	}
	agentRepo.Create(ctx, status)

	for i := 0; i < 5; i++ {
		err := agentRepo.IncrementTodayMessages(context.Background(), 1)
		if err != nil {
			t.Errorf("IncrementTodayMessages() error = %v", err)
		}
	}

	updated, _ := agentRepo.GetByAgentID(context.Background(), 1)
	if updated.TodayMessages != 5 {
		t.Errorf("Expected TodayMessages 5, got %d", updated.TodayMessages)
	}
}

// TestAISuggestionRepository_Create 测试创建 AI 建议
func TestAISuggestionRepository_Create(t *testing.T) {
	_, _, _, aiSuggestionRepo, _, _ := setupCustomerSessionRepositories(t)
	ctx := context.Background()

	tests := []struct {
		name       string
		suggestion *model.AISuggestion
		wantErr    bool
	}{
		{
			name: "create suggestion from LLM",
			suggestion: &model.AISuggestion{
				SessionID:  "session-001",
				MessageID:  1,
				Suggestion: "You can track your order in the 'My Orders' section.",
				Confidence: 0.92,
				Source:     "llm",
				IsUsed:     false,
			},
			wantErr: false,
		},
		{
			name: "create suggestion from RAG",
			suggestion: &model.AISuggestion{
				SessionID:  "session-001",
				MessageID:  2,
				Suggestion: "Our refund policy allows returns within 30 days.",
				Confidence: 0.85,
				Source:     "rag",
				IsUsed:     false,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := aiSuggestionRepo.Create(ctx, tt.suggestion)

			if (err != nil) != tt.wantErr {
				t.Errorf("Create() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr && tt.suggestion.ID == 0 {
				t.Error("Expected suggestion ID to be set after creation")
			}
		})
	}
}

// TestAISuggestionRepository_GetBySessionID 测试获取会话的 AI 建议列表
func TestAISuggestionRepository_GetBySessionID(t *testing.T) {
	_, _, _, aiSuggestionRepo, _, _ := setupCustomerSessionRepositories(t)
	ctx := context.Background()

	for i := 1; i <= 5; i++ {
		aiSuggestionRepo.Create(ctx, &model.AISuggestion{
			SessionID:  "session-suggestions",
			MessageID:  uint(i),
			Suggestion: string(rune('A' + i - 1)),
			Confidence: 0.9,
			Source:     "llm",
		})
	}

	aiSuggestionRepo.Create(ctx, &model.AISuggestion{
		SessionID:  "other-session",
		MessageID:  99,
		Suggestion: "Other",
		Confidence: 0.8,
		Source:     "rag",
	})

	suggestions, err := aiSuggestionRepo.GetBySessionID(context.Background(), "session-suggestions")
	if err != nil {
		t.Errorf("GetBySessionID() error = %v", err)
	}

	if len(suggestions) != 5 {
		t.Errorf("Expected 5 suggestions, got %d", len(suggestions))
	}
}

// TestAISuggestionRepository_MarkAsUsed 测试标记建议已使用
func TestAISuggestionRepository_MarkAsUsed(t *testing.T) {
	_, _, _, aiSuggestionRepo, _, _ := setupCustomerSessionRepositories(t)
	ctx := context.Background()

	suggestion := &model.AISuggestion{
		SessionID:  "session-001",
		MessageID:  1,
		Suggestion: "Test suggestion",
		Confidence: 0.9,
		Source:     "llm",
		IsUsed:     false,
	}
	aiSuggestionRepo.Create(ctx, suggestion)

	err := aiSuggestionRepo.MarkAsUsed(context.Background(), suggestion.ID, 1)
	if err != nil {
		t.Errorf("MarkAsUsed() error = %v", err)
	}

	var updated model.AISuggestion
	db.GetDB().First(&updated, suggestion.ID)

	if !updated.IsUsed {
		t.Error("Expected IsUsed to be true")
	}
	if updated.UsedBy != 1 {
		t.Errorf("Expected UsedBy 1, got %d", updated.UsedBy)
	}
	if updated.UsedAt == nil {
		t.Error("Expected UsedAt to be set")
	}
}

// TestQuickReplyRepository_Create 测试创建快捷回复
func TestQuickReplyRepository_Create(t *testing.T) {
	_, _, _, _, quickReplyRepo, _ := setupCustomerSessionRepositories(t)
	ctx := context.Background()

	tests := []struct {
		name    string
		reply   *model.QuickReply
		wantErr bool
	}{
		{
			name: "create public reply",
			reply: &model.QuickReply{
				Category:  "greeting",
				Title:     "Welcome",
				Content:   "Hello! How can I help you today?",
				SortOrder: 1,
				IsPublic:  true,
				CreatedBy: 1,
			},
			wantErr: false,
		},
		{
			name: "create private reply",
			reply: &model.QuickReply{
				Category:  "internal",
				Title:     "Internal Note",
				Content:   "Internal usage only",
				SortOrder: 1,
				IsPublic:  false,
				CreatedBy: 1,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := quickReplyRepo.Create(ctx, tt.reply)

			if (err != nil) != tt.wantErr {
				t.Errorf("Create() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr && tt.reply.ID == 0 {
				t.Error("Expected reply ID to be set after creation")
			}
		})
	}
}

// TestQuickReplyRepository_Update 测试更新快捷回复
func TestQuickReplyRepository_Update(t *testing.T) {
	_, _, _, _, quickReplyRepo, _ := setupCustomerSessionRepositories(t)
	ctx := context.Background()

	reply := &model.QuickReply{
		Category:  "greeting",
		Title:     "Welcome",
		Content:   "Original content",
		SortOrder: 1,
		IsPublic:  true,
	}
	quickReplyRepo.Create(ctx, reply)

	reply.Title = "Updated Welcome"
	reply.Content = "Updated content"

	err := quickReplyRepo.Update(ctx, reply)
	if err != nil {
		t.Errorf("Update() error = %v", err)
	}

	updated, _ := quickReplyRepo.GetByID(ctx, reply.ID)
	if updated.Title != "Updated Welcome" {
		t.Errorf("Expected Title 'Updated Welcome', got '%s'", updated.Title)
	}
	if updated.Content != "Updated content" {
		t.Errorf("Expected Content 'Updated content', got '%s'", updated.Content)
	}
}

// TestQuickReplyRepository_Delete 测试删除快捷回复
func TestQuickReplyRepository_Delete(t *testing.T) {
	_, _, _, _, quickReplyRepo, _ := setupCustomerSessionRepositories(t)
	ctx := context.Background()

	reply := &model.QuickReply{
		Category: "greeting",
		Title:    "To Delete",
		Content:  "Delete me",
		IsPublic: true,
	}
	quickReplyRepo.Create(ctx, reply)

	err := quickReplyRepo.Delete(ctx, reply.ID)
	if err != nil {
		t.Errorf("Delete() error = %v", err)
	}

	_, err = quickReplyRepo.GetByID(ctx, reply.ID)
	if err == nil {
		t.Error("Expected reply to be deleted")
	}
}

// TestQuickReplyRepository_GetByID 测试根据 ID 获取快捷回复
func TestQuickReplyRepository_GetByID(t *testing.T) {
	_, _, _, _, quickReplyRepo, _ := setupCustomerSessionRepositories(t)
	ctx := context.Background()

	reply := &model.QuickReply{
		Category: "greeting",
		Title:    "GetByID Test",
		Content:  "Test content",
		IsPublic: true,
	}
	quickReplyRepo.Create(ctx, reply)

	tests := []struct {
		name    string
		id      uint
		wantErr bool
	}{
		{
			name:    "get existing reply",
			id:      reply.ID,
			wantErr: false,
		},
		{
			name:    "get non-existing reply",
			id:      99999,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := quickReplyRepo.GetByID(ctx, tt.id)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetByID() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				if result.Title != "GetByID Test" {
					t.Errorf("Expected Title 'GetByID Test', got '%s'", result.Title)
				}
			}
		})
	}
}

// TestQuickReplyRepository_GetCategories 测试获取快捷回复分类列表
func TestQuickReplyRepository_GetCategories(t *testing.T) {
	_, _, _, _, quickReplyRepo, _ := setupCustomerSessionRepositories(t)
	ctx := context.Background()

	quickReplyRepo.Create(ctx, &model.QuickReply{
		Category: "greeting",
		Title:    "Hello",
		IsPublic: true,
	})
	quickReplyRepo.Create(ctx, &model.QuickReply{
		Category: "greeting",
		Title:    "Welcome",
		IsPublic: true,
	})
	quickReplyRepo.Create(ctx, &model.QuickReply{
		Category: "faq",
		Title:    "Refund",
		IsPublic: true,
	})
	quickReplyRepo.Create(ctx, &model.QuickReply{
		Category: "shipping",
		Title:    "Delivery",
		IsPublic: true,
	})

	categories, err := quickReplyRepo.GetCategories(context.Background())
	if err != nil {
		t.Errorf("GetCategories() error = %v", err)
	}

	if len(categories) != 3 {
		t.Errorf("Expected 3 categories, got %d", len(categories))
	}
}

// TestSessionTagRepository_Create 测试创建会话标签
func TestSessionTagRepository_Create(t *testing.T) {
	_, _, _, _, _, sessionTagRepo := setupCustomerSessionRepositories(t)
	ctx := context.Background()

	tests := []struct {
		name    string
		tag     *model.SessionTag
		wantErr bool
	}{
		{
			name: "create tag success",
			tag: &model.SessionTag{
				Name:      "Urgent",
				Code:      "urgent",
				Color:     "#FF0000",
				SortOrder: 1,
			},
			wantErr: false,
		},
		{
			name: "create tag without color",
			tag: &model.SessionTag{
				Name:      "Normal",
				Code:      "normal",
				SortOrder: 2,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := sessionTagRepo.Create(ctx, tt.tag)

			if (err != nil) != tt.wantErr {
				t.Errorf("Create() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr && tt.tag.ID == 0 {
				t.Error("Expected tag ID to be set after creation")
			}
		})
	}
}

// TestSessionTagRepository_Update 测试更新会话标签
func TestSessionTagRepository_Update(t *testing.T) {
	_, _, _, _, _, sessionTagRepo := setupCustomerSessionRepositories(t)
	ctx := context.Background()

	tag := &model.SessionTag{
		Name:      "Original",
		Color:     "#000000",
		SortOrder: 1,
	}
	sessionTagRepo.Create(ctx, tag)

	tag.Name = "Updated"
	tag.Color = "#FF0000"

	err := sessionTagRepo.Update(ctx, tag)
	if err != nil {
		t.Errorf("Update() error = %v", err)
	}

	updated, _ := sessionTagRepo.GetByID(ctx, tag.ID)
	if updated.Name != "Updated" {
		t.Errorf("Expected Name 'Updated', got '%s'", updated.Name)
	}
	if updated.Color != "#FF0000" {
		t.Errorf("Expected Color '#FF0000', got '%s'", updated.Color)
	}
}

// TestSessionTagRepository_Delete 测试删除会话标签
func TestSessionTagRepository_Delete(t *testing.T) {
	_, _, _, _, _, sessionTagRepo := setupCustomerSessionRepositories(t)
	ctx := context.Background()

	tag := &model.SessionTag{
		Name:  "To Delete",
		Color: "#000000",
	}
	sessionTagRepo.Create(ctx, tag)

	err := sessionTagRepo.Delete(ctx, tag.ID)
	if err != nil {
		t.Errorf("Delete() error = %v", err)
	}

	_, err = sessionTagRepo.GetByID(ctx, tag.ID)
	if err == nil {
		t.Error("Expected tag to be deleted")
	}
}

// TestSessionTagRepository_GetByID 测试根据 ID 获取标签
func TestSessionTagRepository_GetByID(t *testing.T) {
	_, _, _, _, _, sessionTagRepo := setupCustomerSessionRepositories(t)
	ctx := context.Background()

	tag := &model.SessionTag{
		Name:      "GetByID Tag",
		Color:     "#123456",
		SortOrder: 1,
	}
	sessionTagRepo.Create(ctx, tag)

	tests := []struct {
		name    string
		id      uint
		wantErr bool
	}{
		{
			name:    "get existing tag",
			id:      tag.ID,
			wantErr: false,
		},
		{
			name:    "get non-existing tag",
			id:      99999,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := sessionTagRepo.GetByID(ctx, tt.id)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetByID() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				if result.Name != "GetByID Tag" {
					t.Errorf("Expected Name 'GetByID Tag', got '%s'", result.Name)
				}
			}
		})
	}
}
