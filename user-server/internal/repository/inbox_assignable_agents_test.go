// inbox_assignable_agents_test.go 收件箱可分配坐席名单的仓储契约。
//
// AV-04c 批次6 刀1：ListAssignableAgents 的 agent_statuses⋈system_users 四列投影
// 原写在 service/inbox_scope.go（经 dbUtil.GetDB() 直查），下沉时把谓词、投影、
// 排序钉死在这里——停用账号/无档案账号/软删档案不得混进脱敏名单，顺序按
// u.username 升序（controller 分配下拉的稳定输出）。
package repository

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func setupAssignableAgentsTestDB(t *testing.T) (*gorm.DB, *InboxConversationRepository) {
	t.Helper()
	database := testutil.NewTestDB(t,
		&model.SystemUser{},
		&model.AgentStatus{},
	)
	return database, NewInboxConversationRepositoryWithDB(database)
}

// seedAgentUser 建一个系统账号（口令列 NOT NULL，测试口令经模型 BeforeCreate 哈希）。
// Enabled/Status 带 gorm 列默认值，零值会被 Create 省略——停用态必须回写强制落列，
// 否则被测的过滤谓词根本吃不到「停用」输入。
func seedAgentUser(t *testing.T, db *gorm.DB, id uint, username, realName, role string, status int, enabled bool) {
	t.Helper()
	u := &model.SystemUser{
		ID:       id,
		Username: username,
		Password: "test-password-" + username,
		RealName: realName,
		Role:     role,
		Status:   status,
		Enabled:  enabled,
	}
	if err := db.Create(u).Error; err != nil {
		t.Fatalf("seed user %s: %v", username, err)
	}
	if err := db.Model(&model.SystemUser{}).Where("id = ?", id).
		Updates(map[string]any{"enabled": enabled, "status": status}).Error; err != nil {
		t.Fatalf("force enabled/status for %s: %v", username, err)
	}
}

// 名单口径：有坐席档案 + 账号 enabled + status=1；停用/无档案/软删档案不进名单；
// 输出按 u.username 升序，只投影四列。
func TestInboxConversationRepository_ListAssignableAgents_FilterOrderProjection(t *testing.T) {
	db, repo := setupAssignableAgentsTestDB(t)
	ctx := context.Background()

	// bob：在编坐席，enabled+status=1+有档案 → 应入选
	seedAgentUser(t, db, 101, "bob_agent", "鲍勃", "customer_service", 1, true)
	// alice：在编坐席（用户名序应排在 bob 前）
	seedAgentUser(t, db, 102, "alice_agent", "爱丽丝", "staff", 1, true)
	// carol：账号被停用（enabled=false）→ 不得入选（把会话分给没人接的坐席）
	seedAgentUser(t, db, 103, "carol_disabled", "卡罗尔", "staff", 1, false)
	// dave：status=0（停用态）→ 不得入选
	seedAgentUser(t, db, 104, "dave_status0", "戴夫", "staff", 0, true)
	// erin：enabled+status=1 但没有坐席档案（纯账号域 admin）→ 不得入选
	seedAgentUser(t, db, 105, "erin_no_profile", "艾琳", "admin", 1, true)
	if err := db.Create(&model.AgentStatus{AgentID: 101, AgentName: "bob档案", Status: "offline"}).Error; err != nil {
		t.Fatalf("seed profile bob: %v", err)
	}
	if err := db.Create(&model.AgentStatus{AgentID: 102, AgentName: "alice档案", Status: "offline"}).Error; err != nil {
		t.Fatalf("seed profile alice: %v", err)
	}
	if err := db.Create(&model.AgentStatus{AgentID: 103, AgentName: "carol档案", Status: "offline"}).Error; err != nil {
		t.Fatalf("seed profile carol: %v", err)
	}
	if err := db.Create(&model.AgentStatus{AgentID: 104, AgentName: "dave档案", Status: "offline"}).Error; err != nil {
		t.Fatalf("seed profile dave: %v", err)
	}
	// frank：有档案但档案被软删 → 不得入选
	seedAgentUser(t, db, 106, "frank_deleted_profile", "弗兰克", "staff", 1, true)
	if err := db.Delete(&model.AgentStatus{}, "agent_id = ?", 106).Error; err != nil {
		t.Fatalf("soft-delete profile frank: %v", err)
	}

	rows, err := repo.ListAssignableAgents(ctx)
	if err != nil {
		t.Fatalf("ListAssignableAgents: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("应只有 alice/bob 两人入选，got %d: %+v", len(rows), rows)
	}
	if rows[0].Username != "alice_agent" || rows[1].Username != "bob_agent" {
		t.Fatalf("应按 u.username 升序: %+v", rows)
	}
	if rows[0].ID != 102 || rows[0].RealName != "爱丽丝" || rows[0].Role != "staff" {
		t.Fatalf("alice 投影四列不符: %+v", rows[0])
	}
	if rows[1].ID != 101 || rows[1].RealName != "鲍勃" || rows[1].Role != "customer_service" {
		t.Fatalf("bob 投影四列不符: %+v", rows[1])
	}
}

// 无库（db=nil）读路径回空不报错——与其余读路径同一套"无库回空"语义。
func TestInboxConversationRepository_ListAssignableAgents_NilDB(t *testing.T) {
	var repo *InboxConversationRepository
	rows, err := repo.ListAssignableAgents(context.Background())
	if err != nil {
		t.Fatalf("nil db 不应报错: %v", err)
	}
	if rows != nil {
		t.Fatalf("nil db 应回 nil, got %+v", rows)
	}
}
