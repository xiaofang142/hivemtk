// cs06b_outbound_unknown_list_stats_test.go CH-37b 刀5：unknown 对账管理面的
// service 读数层（清单/计数）+ 驾驶舱 stats 待办读数。
//
// 刀4 钉了 unknown 的写侧规格（真翻转入 unknown、永不自动重发、对账唯一出路）；
// 刀5 补读侧的三个消费口并钉住：
//
//	① service.ListOutboundUnknownOutbound——清单与迁移同谓词（同 (channel,accountID,
//	   unknown) 命中集合一致），total 与行集一致，行 id 是对账定位键；非 unknown 行不混入。
//	② service.CountOutboundUnknownOutbound——直读计数与清单 total 同源。
//	③ repo.GetHubStats().OutboundUnknown——驾驶舱 stats 暴露同一待办数，且跟请求
//	   窗口走（与其他字段同口径：hubWindow 套每一条统计）；零 unknown 时回 0 不缺席。
//
// 夹具沿用刀4 套件惯例：真 PG + system scope 预置/回读（off 模式直通）。
package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

func TestUnknownAdminList_ReadsSameSetAsReconcile(t *testing.T) {
	database := testutil.NewTestDB(t, &model.MessageHub{})
	svc := NewInboxIngressService()
	ctx := context.Background()

	// 两行 unknown（acc1）+ 一行 unknown（acc2，账号维过滤的对照）+ 一行 delivered（不混入）。
	idA := seedUnkRow(t, database, "unk5-list-a", "conv-5", model.BridgeAckStatusUnknown, 1)
	idB := seedUnkRow(t, database, "unk5-list-b", "conv-5", model.BridgeAckStatusUnknown, 1)
	rowC := &model.MessageHub{
		TenantID: 1, Platform: unkPlatform, AccountID: "acc2", ConversationID: "conv-6",
		MsgID: "unk5-list-c", MsgType: "text", Content: "c-unk5-list-c",
		Direction: "outbound", Status: model.BridgeAckStatusUnknown,
	}
	if err := database.WithContext(unkScopeCtx()).Create(rowC).Error; err != nil {
		t.Fatalf("seed acc2 失败: %v", err)
	}
	seedUnkRow(t, database, "unk5-list-ok", "conv-5", model.BridgeAckStatusDelivered, 1)

	// ① 清单：account 过滤下恰 2 行、恰是 a/b 的 id、total=2。
	res, err := svc.ListOutboundUnknownOutbound(ctx, unkPlatform, "acc1", 0, 0)
	if err != nil {
		t.Fatalf("清单失败: %v", err)
	}
	if res.Total != 2 || len(res.Rows) != 2 {
		t.Fatalf("清单应 2 行 total=2，实得 total=%d rows=%d", res.Total, len(res.Rows))
	}
	got := map[uint]bool{}
	for _, r := range res.Rows {
		got[r.ID] = true
		if r.Status != model.BridgeAckStatusUnknown || r.Platform != unkPlatform || r.AccountID != "acc1" {
			t.Errorf("清单行越谓词: %+v", r)
		}
		if r.MsgID == "" {
			t.Errorf("清单行缺 msg_id（定位辅助字段）: %+v", r)
		}
	}
	if !got[idA] || !got[idB] {
		t.Fatalf("清单应含 id %d/%d，实得 %v", idA, idB, got)
	}

	// ② 计数直读同源。
	n, err := svc.CountOutboundUnknownOutbound(ctx, unkPlatform, "acc1")
	if err != nil || n != 2 {
		t.Fatalf("CountOutboundUnknownOutbound = %d err=%v, want 2", n, err)
	}

	// ③ 账号维放开 → 恰 3 行（acc2 的 unknown 也进来）。
	resAll, err := svc.ListOutboundUnknownOutbound(ctx, unkPlatform, "", 0, 0)
	if err != nil || resAll.Total != 3 {
		t.Fatalf("全局清单应 total=3，实得 %d err=%v", resAll.Total, err)
	}

	// ④ 分页：limit=1 取首页（id 升序=a），offset=1 取次页（=b），翻页不重。
	p1, err := svc.ListOutboundUnknownOutbound(ctx, unkPlatform, "acc1", 1, 0)
	if err != nil || len(p1.Rows) != 1 || p1.Rows[0].ID != idA {
		t.Fatalf("第1页应恰 idA，实得 %+v err=%v", p1.Rows, err)
	}
	p2, err := svc.ListOutboundUnknownOutbound(ctx, unkPlatform, "acc1", 1, 1)
	if err != nil || len(p2.Rows) != 1 || p2.Rows[0].ID != idB {
		t.Fatalf("第2页应恰 idB，实得 %+v err=%v", p2.Rows, err)
	}

	// ⑤ 对账联动（读-写同集合的端到端）：迁移 a 之后，清单/计数同步收敛到 1。
	if _, err := svc.ReconcileUnknownOutbound(ctx, unkPlatform, "acc1", []uint{idA}, model.BridgeAckStatusDelivered, "ops_list"); err != nil {
		t.Fatalf("对账失败: %v", err)
	}
	resAfter, err := svc.ListOutboundUnknownOutbound(ctx, unkPlatform, "acc1", 0, 0)
	if err != nil || resAfter.Total != 1 || len(resAfter.Rows) != 1 || resAfter.Rows[0].ID != idB {
		t.Fatalf("迁移后清单应只剩 idB，实得 total=%d err=%v", resAfter.Total, err)
	}
}

// TestHubStats_OutboundUnknownTodo 驾驶舱 stats 的 unknown 待办读数：同窗口口径，
// 零时不缺席（0 不是缺列），入站行不计（unknown 是出站终态词汇，无入站治理面）。
func TestHubStats_OutboundUnknownTodo(t *testing.T) {
	database := testutil.NewTestDB(t, &model.MessageHub{})
	repo := repository.NewMessageHubRepositoryWithDB(database)
	ctx := context.Background()

	now := time.Now()
	rows := []*model.MessageHub{
		{TenantID: 1, Platform: unkPlatform, AccountID: "acc1", ConversationID: "c1", MsgID: "unk5-st-a", MsgType: "text", Direction: "outbound", Status: model.BridgeAckStatusUnknown, SentAt: now.Add(-time.Hour)},
		{TenantID: 1, Platform: unkPlatform, AccountID: "acc1", ConversationID: "c1", MsgID: "unk5-st-b", MsgType: "text", Direction: "outbound", Status: model.BridgeAckStatusUnknown, SentAt: now.Add(-90 * time.Minute)},
		// 窗口外对照（窗口起始=2h 前）：更老的 unknown 不计入（hubWindow 套每条统计的口径）。
		{TenantID: 1, Platform: unkPlatform, AccountID: "acc1", ConversationID: "c1", MsgID: "unk5-st-old", MsgType: "text", Direction: "outbound", Status: model.BridgeAckStatusUnknown, SentAt: now.Add(-5 * time.Hour)},
		// 入站行即便落了 unknown 形态值也不计。
		{TenantID: 1, Platform: unkPlatform, AccountID: "acc1", ConversationID: "c1", MsgID: "unk5-st-in", MsgType: "text", Direction: "inbound", Status: model.BridgeAckStatusUnknown, SentAt: now.Add(-time.Hour)},
		// delivered 出站不混入。
		{TenantID: 1, Platform: unkPlatform, AccountID: "acc1", ConversationID: "c1", MsgID: "unk5-st-ok", MsgType: "text", Direction: "outbound", Status: model.BridgeAckStatusDelivered, SentAt: now.Add(-time.Hour)},
	}
	for _, r := range rows {
		if err := database.WithContext(unkScopeCtx()).Create(r).Error; err != nil {
			t.Fatalf("seed %s 失败: %v", r.MsgID, err)
		}
	}

	since := now.Add(-2 * time.Hour)
	res, err := repo.GetHubStats(ctx, &since, nil)
	if err != nil {
		t.Fatalf("GetHubStats: %v", err)
	}
	if res.OutboundUnknown != 2 {
		t.Fatalf("OutboundUnknown 应恰 2（窗口内 outbound unknown），实得 %d", res.OutboundUnknown)
	}
	if res.Outbound != 3 {
		t.Fatalf("Outbound 总数应 3（窗口内 2 unknown+1 delivered），实得 %d", res.Outbound)
	}

	// 零时不缺席：空表直查回 0（不是缺列报错）。
	emptyDB := testutil.NewTestDB(t, &model.MessageHub{})
	var emptyRes *repository.HubStatsResult
	emptyRes, err = repository.NewMessageHubRepositoryWithDB(emptyDB).GetHubStats(ctx, nil, nil)
	if err != nil {
		t.Fatalf("空库 GetHubStats: %v", err)
	}
	if emptyRes.OutboundUnknown != 0 {
		t.Fatalf("空库 OutboundUnknown 应 0，实得 %d", emptyRes.OutboundUnknown)
	}
}
