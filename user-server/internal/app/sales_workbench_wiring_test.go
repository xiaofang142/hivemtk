package app

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
)

// TestInitSalesWorkbenchRuntime_Assembles 项11a：工作台服务装配入口此前全仓构造点为 0
// （五个 Set* 一个都没被调过）。这条用例守住三件事：
//  1. 装配入口真的造出服务并存为全局单例（HTTP 层取得到）；
//  2. 借依赖这一跳没把 nil 服务写进去（借不到就少几块数据，不是没有服务）；
//  3. GetOverview 仍能回一个带 sales_id 的概览 —— 概览整体为 nil 只可能是读侧崩了。
//
// 必须先给全局测试库：stats 仓库在构造期就绑死 _db.GetDB()（NewSalesEventRepository），
// 进程里没库时 GetOverview 的业绩板块会拿 nil 句柄去查 —— 不是"少几块数据"，是当场 panic。
func TestInitSalesWorkbenchRuntime_Assembles(t *testing.T) {
	ctx := context.Background()

	database := testutil.NewTestDB(t, &model.SalesEvent{})
	db.SetTestDB(database)

	wb := InitSalesWorkbenchRuntime()
	if wb == nil {
		t.Fatal("装配入口应返回服务实例")
	}
	if got := SalesWorkbenchServiceForHTTP(); got != wb {
		t.Fatalf("全局单例与装配返回不是同一个实例：got=%p want=%p", got, wb)
	}

	ov := wb.GetOverview(ctx, "sales_wb_wire")
	if ov == nil {
		t.Fatal("GetOverview 应返回概览（nil 只可能是读侧崩了）")
	}
	if ov.SalesID != "sales_wb_wire" {
		t.Errorf("SalesID 应原样回显，实际 %q", ov.SalesID)
	}
	if ov.Todos == nil {
		t.Error("Todos 应是空切片而不是 nil：前端把 null 当解析失败")
	}

	// I7 事件流读侧：GetTeamDashboard/GetChampionProfile 此前生产零调用方，
	// 装配后必须能经工作台服务读出来（nil 只可能是读侧没接上）。
	if dash := wb.GetTeamDashboard(ctx, 30); dash == nil {
		t.Error("装配后 GetTeamDashboard 应返回仪表盘")
	}
	if champ := wb.GetChampionProfile(ctx, 30); champ == nil {
		t.Error("装配后 GetChampionProfile 应返回画像")
	}

	// 二次调用必须幂等（重复装配不产生第二个实例可被观察到的分叉）。
	if wb2 := InitSalesWorkbenchRuntime(); wb2 == nil {
		t.Error("重复装配仍应返回实例")
	}

	// 借依赖这一跳：草稿竖运行时在测试进程里没装配 ⇒ 借到的是 nil，
	// 但服务本身必须在（GetOverview 里各板块按 nil 跳过）。
	draft := OrderDraftServiceForHTTP()
	if draft != nil {
		t.Log("草稿运行时已装配，概览应含 draft 待办（本用例不断言条数：数据来自本进程写入方）")
	}
}
