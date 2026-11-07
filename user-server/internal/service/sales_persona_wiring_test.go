// sales_persona_wiring_test.go 销冠画像装配与 fail-open 契约（AV-04c 批次6 刀2）。
//
// NewSalesPersonaService / ensureReposFromDB 收口为 NewSalesPersonaRepositoryDefault
// （仓储包内解析全局句柄）后，双向装配语义与 fail-open 输出不变：
//   - 无库：repo 留 nil，懒构造下轮再试；BuildReport/ListStaffs 不 panic、记零分；
//   - 有库：repo 绑到测试库，空表上各聚合 fail-open（错误被调用方吞掉记零分）。
package service

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// 无库侧：Default 构造返回 nil ⇒ repo 留 nil（懒重试语义保留），读路径 fail-open。
func TestSalesPersonaService_NilDB_FailOpen(t *testing.T) {
	if repository.DBReady() {
		t.Skip("全局句柄被在途测试占用，无库语义由其余 NilDB 用例覆盖")
	}
	svc := NewSalesPersonaService()
	if svc.repo != nil {
		t.Fatal("无库时 repo 应为 nil")
	}
	report, err := svc.BuildReport(context.Background(), 1)
	if err != nil {
		t.Fatalf("无库 BuildReport 应 fail-open 不报错: %v", err)
	}
	if report == nil || len(report.Items) == 0 {
		t.Fatalf("无库 BuildReport 应回零分画像, got %+v", report)
	}
	staffs, err := svc.ListStaffs(context.Background())
	if err != nil || len(staffs) != 0 {
		t.Fatalf("无库 ListStaffs 应回空不报错: %v %v", staffs, err)
	}
	// 懒构造语义：无库时 ensureReposFromDB 不得把 repo 钉成非 nil（否则丢失下轮再试）。
	svc.ensureReposFromDB(context.Background())
	if svc.repo != nil {
		t.Fatal("无库懒构造后 repo 仍应为 nil（下轮再试语义）")
	}
}

// 有库侧：Default 构造把仓储绑到测试库；聚合触及的六张表都建出来，
// 空表上各查询走真实 SQL（零行零分），ListStaffs 的错误路径不再被缺表干扰。
func TestSalesPersonaService_WithDB_WiresDefaultRepo(t *testing.T) {
	_ = testutil.NewTestDB(t,
		&model.SessionMessage{},
		&model.CustomerSession{},
		&model.Order{},
		&model.SOPExecution{},
		&model.SOPExecEvent{},
		&model.RagSession{},
	)
	svc := NewSalesPersonaService()
	if svc.repo == nil {
		t.Fatal("有库时 repo 应装配")
	}
	report, err := svc.BuildReport(context.Background(), 42)
	if err != nil {
		t.Fatalf("BuildReport: %v", err)
	}
	if report == nil || report.StaffID != 42 || len(report.Items) < 5 {
		t.Fatalf("画像项应齐全: %+v", report)
	}
	staffs, err := svc.ListStaffs(context.Background())
	if err != nil || len(staffs) != 0 {
		t.Fatalf("空表 ListStaffs 应回空: %v %v", staffs, err)
	}
}
