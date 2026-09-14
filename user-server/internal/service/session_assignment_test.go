package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

func TestIsUrgentOrComplaint_Complaint(t *testing.T) {
	s := &SessionAssignmentService{}
	cases := []string{
		"我要投诉",
		"我要举报你们",
		"我要退钱",
		"骗子",
		"假货",
		"315曝光",
	}
	for _, c := range cases {
		if !s.isUrgentOrComplaint(context.Background(), c) {
			t.Errorf("expected urgent for %q", c)
		}
	}
}

func TestIsUrgentOrComplaint_Normal(t *testing.T) {
	s := &SessionAssignmentService{}
	cases := []string{
		"你好",
		"请问价格",
		"产品很好",
		"hello world",
	}
	for _, c := range cases {
		if s.isUrgentOrComplaint(context.Background(), c) {
			t.Errorf("expected not urgent for %q", c)
		}
	}
}

func TestIsUrgentOrComplaint_CaseInsensitive(t *testing.T) {
	s := &SessionAssignmentService{}
	if !s.isUrgentOrComplaint(context.Background(), "URGENT") {
	}
	if !s.isUrgentOrComplaint(context.Background(), "投诉") {
		t.Error("expected urgent for 投诉")
	}
}

// setupSessionAssignmentTestDB 构造仅含 customer_sessions 的测试库
func setupSessionAssignmentTestDB(t *testing.T) *gorm.DB {
	return testutil.NewTestDB(t, &model.CustomerSession{})
}

// TestFindActiveSession_EleventhSessionHit R55-T3 验收：
// 用户已有 11 个会话（前 10 个已解决、按 created_at 升序排在前面，第 11 个活跃），
// findActiveSession 必须跳过前 10 条命中第 11 个活跃会话
// （旧实现 GetByMerchant 只取前 10 条内存匹配，必然 ErrSessionNotFound → 重复建会话）。
func TestFindActiveSession_EleventhSessionHit(t *testing.T) {
	db := setupSessionAssignmentTestDB(t)
	if db == nil {
		return
	}
	ctx := context.Background()
	userID := "u-t3-eleventh"
	base := time.Now().Add(-time.Hour)

	for i := 0; i < 10; i++ {
		session := &model.CustomerSession{
			SessionID: fmt.Sprintf("sess_t3_resolved_%02d", i),
			Platform:  model.PlatformWebEmbed,
			UserID:    userID,
			Status:    model.SessionStatusResolved,
			CreatedAt: base.Add(time.Duration(i) * time.Minute),
		}
		if err := db.Create(session).Error; err != nil {
			t.Fatalf("seed resolved session %d: %v", i, err)
		}
	}
	active := &model.CustomerSession{
		SessionID:   "sess_t3_active_11",
		Platform:    model.PlatformWebEmbed,
		UserID:      userID,
		Status:      model.SessionStatusHumanHandling,
		CreatedAt:   base.Add(11 * time.Minute),
		LastMessage: "第11个会话",
	}
	if err := db.Create(active).Error; err != nil {
		t.Fatalf("seed active session: %v", err)
	}

	svc := &SessionAssignmentService{sessionRepo: repository.NewCustomerSessionRepositoryWithDB(db)}
	got, err := svc.findActiveSession(ctx, userID, "")
	if err != nil {
		t.Fatalf("findActiveSession 应命中第11个活跃会话，实际错误: %v", err)
	}
	if got == nil {
		t.Fatal("findActiveSession 返回 nil，前10条限制回归")
	}
	if got.SessionID != "sess_t3_active_11" {
		t.Errorf("期望命中 sess_t3_active_11，实际 %s", got.SessionID)
	}
}

// TestFindActiveSession_ByChatID 验证 chatID 直查路径：GetBySessionID 命中活跃会话即返回
func TestFindActiveSession_ByChatID(t *testing.T) {
	db := setupSessionAssignmentTestDB(t)
	if db == nil {
		return
	}
	ctx := context.Background()
	session := &model.CustomerSession{
		SessionID: "sess_t3_chatid",
		Platform:  model.PlatformWebEmbed,
		UserID:    "u-t3-chatid",
		Status:    model.SessionStatusAIHandling,
	}
	if err := db.Create(session).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}

	svc := &SessionAssignmentService{sessionRepo: repository.NewCustomerSessionRepositoryWithDB(db)}
	got, err := svc.findActiveSession(ctx, "u-t3-chatid", "sess_t3_chatid")
	if err != nil {
		t.Fatalf("chatID 直查应命中，实际错误: %v", err)
	}
	if got == nil || got.SessionID != "sess_t3_chatid" {
		t.Fatalf("期望命中 sess_t3_chatid，实际 %+v", got)
	}
}
