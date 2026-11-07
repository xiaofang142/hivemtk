// inbox_scope_agents_test.go 收件箱坐席名单服务级回归（AV-04c 批次6 刀1）。
//
// ListAssignableAgents 的 SQL 已下沉 InboxConversationRepository（同批
// repository/inbox_assignable_agents_test.go 锁谓词/投影/排序），这里锁：
//  1. 下沉后服务层映射搬运无损（controller 响应四列 json 契约不变）；
//  2. NewFAQService/NewInboxService 装配改 DBReady 探针 + Default 构造后，
//     「有库即接线、无库即留 nil」的双向语义不变；
//  3. NewDataExportService 的 Default 构造在无库时错误路径不变（DSAR_003）。
package service

import (
	"context"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// 下沉后的服务级口径：仓储返回的四列无损搬运成脱敏视图，顺序保持 username 升序。
func TestInboxService_ListAssignableAgents_MapsRepoRows(t *testing.T) {
	db := testutil.NewTestDB(t,
		&model.SystemUser{},
		&model.AgentStatus{},
	)
	seed := func(id uint, username, realName, role string) {
		t.Helper()
		if err := db.Create(&model.SystemUser{
			ID: id, Username: username, Password: "pw-" + username,
			RealName: realName, Role: role, Status: 1, Enabled: true,
		}).Error; err != nil {
			t.Fatalf("seed user %s: %v", username, err)
		}
		if err := db.Create(&model.AgentStatus{AgentID: id, AgentName: username + "档案", Status: "offline"}).Error; err != nil {
			t.Fatalf("seed profile %s: %v", username, err)
		}
	}
	seed(201, "zoe_agent", "佐伊", "staff")
	seed(202, "adam_agent", "亚当", "customer_service")

	svc := NewInboxService()
	agents, err := svc.ListAssignableAgents(context.Background())
	if err != nil {
		t.Fatalf("ListAssignableAgents: %v", err)
	}
	if len(agents) != 2 {
		t.Fatalf("应 2 名坐席, got %d: %+v", len(agents), agents)
	}
	if agents[0].Username != "adam_agent" || agents[1].Username != "zoe_agent" {
		t.Fatalf("顺序应保持 username 升序: %+v", agents)
	}
	if agents[1].ID != 201 || agents[1].RealName != "佐伊" || agents[1].Role != "staff" {
		t.Fatalf("zoe 四列映射不符: %+v", agents[1])
	}
}

// 装配语义（无库侧）：全局句柄未注入时 repo 留 nil，读路径回空（NilDB 家族契约）。
func TestNewFAQService_NilDB_LeavesReposNil(t *testing.T) {
	if repository.DBReady() {
		t.Skip("全局句柄被在途测试占用，无库语义由其余 NilDB 用例覆盖")
	}
	svc := NewFAQService()
	if svc.repo != nil || svc.bindingRepo != nil {
		t.Fatalf("无库时两仓储应留 nil, got repo=%v binding=%v", svc.repo != nil, svc.bindingRepo != nil)
	}
	inbox := NewInboxService()
	if inbox.inboxRepo != nil || inbox.hubRepo != nil || inbox.assignmentRepo != nil || inbox.sessionMsgRepo != nil {
		t.Fatalf("无库时四仓储应留 nil")
	}
	if agents, err := inbox.ListAssignableAgents(context.Background()); err != nil || agents != nil {
		t.Fatalf("无库回空语义破坏: agents=%v err=%v", agents, err)
	}
}

// 装配语义（有库侧）：DBReady 探针 + Default 构造接线后，仓储确实绑到测试库。
func TestNewFAQService_WithDB_WiresDefaultRepos(t *testing.T) {
	_ = testutil.NewTestDB(t, &model.FAQEntry{}, &model.AgentKBBinding{})
	svc := NewFAQService()
	if svc.repo == nil || svc.bindingRepo == nil {
		t.Fatalf("有库时两仓储都应装配, got repo=%v binding=%v", svc.repo == nil, svc.bindingRepo == nil)
	}
	inbox := NewInboxService()
	if inbox.inboxRepo == nil || inbox.hubRepo == nil || inbox.assignmentRepo == nil || inbox.sessionMsgRepo == nil {
		t.Fatalf("有库时四仓储都应装配")
	}
}

// Default 构造在无库时的错误路径：Export 仍走 DSAR_003（仓储 r.db==nil 守卫），
// 与原「service 取句柄传构造」行为一致。
func TestNewDataExportService_NilDB_ErrorPathUnchanged(t *testing.T) {
	if repository.DBReady() {
		t.Skip("全局句柄被在途测试占用，无库语义由其余 NilDB 用例覆盖")
	}
	svc := NewDataExportService()
	_, err := svc.Export(context.Background(), "cust_nil_db")
	if err == nil {
		t.Fatal("无库导出应报错")
	}
	if !strings.Contains(err.Error(), "DSAR_003") {
		t.Fatalf("错误路径应保持 DSAR_003, got: %v", err)
	}
}
