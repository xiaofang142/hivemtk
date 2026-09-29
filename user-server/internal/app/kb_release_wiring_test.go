// kb_release_wiring_test.go T-P9-02：变更/发布底座装配层的实跑。
//
// 服务层与出口层的用例都不碰这一层，而这一层最容易留下的正是"实现了但没人装"那一类
// 缺陷（T-P2-06 / T-P3-03 的同一条教训）。这里只断装配层独占的三件事：
//  1. 拿不到 DB 句柄时全局是不是**被清空**（不是"什么都没做"）；
//  2. 装配出的实例是不是真能用，且与路由取的是同一个对象；
//  3. 提交变更时创建的审批行，是不是**裁决入口那一份服务**读得到的那一行 ——
//     这条是本卡最实的一处耦合：入队用一份实例、裁决读另一份，变更就永远停在"没人批"。
//
// 全局句柄一律先记后还原（本包用例共享进程）。
package app

import (
	"context"
	"testing"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"
)

func kbReleaseTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return testutil.NewTestDB(t,
		&model.KBRelease{}, &model.KBChangeRequest{}, &model.KBChangeAuditLog{},
		&model.KnowledgeChunk{}, &model.ApprovalRequest{})
}

// cleanupKBReleaseGlobals 还原变更底座与审批服务两个全局（本文件会动到这两个）。
func cleanupKBReleaseGlobals(t *testing.T) {
	t.Helper()
	prevKB := service.GlobalKBReleaseService()
	prevApproval := service.GlobalApprovalRequestService()
	t.Cleanup(func() {
		service.SetGlobalKBReleaseService(prevKB)
		service.SetGlobalApprovalRequestService(prevApproval)
	})
}

func TestInitKBReleaseRuntime_NilDBClearsGlobal(t *testing.T) {
	cleanupKBReleaseGlobals(t)
	database := kbReleaseTestDB(t)
	if InitKBReleaseRuntime(database) == nil {
		t.Fatal("有 DB 句柄时应装配")
	}
	// 关键的一半：第二次拿到 nil 句柄时必须把上一份撤掉。只 return nil 不清全局的话，
	// 路由会对着"未装配"的启动日志继续回 200，而运维读到的是"这套东西在跑"。
	if got := InitKBReleaseRuntime(nil); got != nil {
		t.Errorf("db=nil 时返回 %v，期望 nil", got)
	}
	if service.GlobalKBReleaseService() != nil {
		t.Error("db=nil 后全局仍留有实例 ⇒ 六个写入口都会以为底座可用")
	}
}

func TestInitKBReleaseRuntime_RegistersUsableGlobal(t *testing.T) {
	cleanupKBReleaseGlobals(t)
	database := kbReleaseTestDB(t)
	svc := InitKBReleaseRuntime(database)
	if svc == nil {
		t.Fatal("有 DB 句柄时不该回 nil")
	}
	if !svc.Available() {
		t.Fatal("装配出的实例 Available() 为假 ⇒ 端点会全 503，而日志说的是已装配")
	}
	if service.GlobalKBReleaseService() != svc {
		t.Error("路由取的全局与 Init 返回的不是同一个实例（两处各一份就会漂移）")
	}
	// 真用一次：走全局那一份提交一条变更，行必须落库。
	// 读回用裸 SQL 而不是本竖的读出口，否则"装配好的服务读不到自己写的行"会被自证。
	row, err := service.GlobalKBReleaseService().SubmitChange(context.Background(), service.KBChangeSubmitInput{
		ProductID: "pprod_wire", Op: model.KBChangeOpAdd, Content: "装配层正文",
		Reason: "装配实跑", RequestedBy: "op-1", DocumentID: 11,
	})
	if err != nil {
		t.Fatalf("全局实例提交变更失败: %v", err)
	}
	var stored model.KBChangeRequest
	if err := database.Where("id = ?", row.ID).First(&stored).Error; err != nil {
		t.Fatalf("回读变更行失败: %v", err)
	}
	if stored.Status != model.KBChangeStatusPending || stored.RequestedBy != "op-1" {
		t.Errorf("变更行形状不对：%s / %q", stored.Status, stored.RequestedBy)
	}
	if stored.ApprovalID == "" {
		t.Fatal("提交没同时入队审批：这条变更将没有任何人能给结论")
	}

	StopKBReleaseRuntime()
	if service.GlobalKBReleaseService() != nil {
		t.Error("StopKBReleaseRuntime 没清全局")
	}
}

// TestInitKBReleaseRuntime_UsesGlobalApprovalService 审批腿是不是那一份。
//
// 判据不是"两个指针相等"（读不到服务实例里的私有字段），而是**行能不能被全局那一份读到**：
// 全局在位时它复用全局，全局不在位时它自建一份 —— 两种情况下写的都是同一张表，
// 所以这一格真正钉住的是"复用发生且结论可查"。反向的那一格（全局缺席）在下面。
func TestInitKBReleaseRuntime_UsesGlobalApprovalService(t *testing.T) {
	cleanupKBReleaseGlobals(t)
	database := kbReleaseTestDB(t)

	approvals := service.NewApprovalRequestService(repository.NewApprovalRequestRepositoryWithDB(database), nil)
	service.SetGlobalApprovalRequestService(approvals)
	if svc := InitKBReleaseRuntime(database); svc == nil {
		t.Fatal("装配失败")
	}

	row, err := service.GlobalKBReleaseService().SubmitChange(context.Background(), service.KBChangeSubmitInput{
		ProductID: "pprod_appr", Op: model.KBChangeOpAdd, Content: "带审批腿的正文",
		Reason: "同实例判据", RequestedBy: "op-2", DocumentID: 12,
	})
	if err != nil {
		t.Fatalf("提交失败: %v", err)
	}
	got, err := approvals.Get(context.Background(), row.ApprovalID)
	if err != nil || got == nil {
		t.Fatalf("全局审批服务读不到这条变更的审批（%v / %v）⇒ 裁决入口与入队口分家了", got, err)
	}
	// 三列逐一对上，少一列就是给别的对象开门（发布侧按这三列认结论）。
	if got.SubjectType != service.KBChangeApprovalSubjectType || got.SubjectID != row.ID ||
		got.PolicyKey != service.KBChangeApprovalPolicyKey {
		t.Errorf("审批行的三列不对：(%q,%q,%q)", got.SubjectType, got.SubjectID, got.PolicyKey)
	}
	if got.Status != model.ApprovalStatusPending {
		t.Errorf("新建的审批该是 pending，实得 %q", got.Status)
	}
}

// TestInitKBReleaseRuntime_WithoutGlobalApprovalStillEnqueues 审批运行时未装配（旗子 off）
// 时底座照样装得上、照样入队 —— 只是没人能在 HTTP 上批（装配点为此单独出声一句）。
//
// 这一格防的是"反过来"的改法：把装配写成"没有审批运行时就不装底座"。那会导致影子期
// 连"这库有几条待发布"都读不到（全 503），而那正是决定要不要转 on 所需的证据。
func TestInitKBReleaseRuntime_WithoutGlobalApprovalStillEnqueues(t *testing.T) {
	cleanupKBReleaseGlobals(t)
	database := kbReleaseTestDB(t)
	service.SetGlobalApprovalRequestService(nil)
	if InitKBReleaseRuntime(database) == nil {
		t.Fatal("审批运行时缺席时不该拒绝装配变更底座")
	}
	row, err := service.GlobalKBReleaseService().SubmitChange(context.Background(), service.KBChangeSubmitInput{
		ProductID: "pprod_noapr", Op: model.KBChangeOpAdd, Content: "无审批运行时的正文",
		Reason: "装配独立于审批腿", RequestedBy: "op-3", DocumentID: 13,
	})
	if err != nil {
		t.Fatalf("提交失败: %v", err)
	}
	// 行确实进了审批表：换一个实例也读得到（同一张表），说明"没装配运行时" ≠ "没入队"。
	var stored model.ApprovalRequest
	if err := database.Where("id = ?", row.ApprovalID).First(&stored).Error; err != nil {
		t.Fatalf("审批行没落库: %v", err)
	}
	if stored.SubjectID != row.ID {
		t.Errorf("审批行指向了别的对象：%q vs %q", stored.SubjectID, row.ID)
	}
}
