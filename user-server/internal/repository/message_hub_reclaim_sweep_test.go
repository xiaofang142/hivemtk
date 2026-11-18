package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// TestReclaimOutboundSweep 全局出站回收巡检三步的验收（2026-11-19 SSE 租约缺口修复）：
//
//	① 超时 inflight → pending（claimed_at 过界；duplicate_of 占位行不回收）；
//	② 到界行（push_attempts 耗尽）升级 failed+outbound_push_exhausted；
//	③ 陈旧 pending/inflight（created_at 过 staleAfter）→ expired+outbound_stale_expired，
//	   但到界行优先按 exhausted 收口、正常新鲜行不动、audit 行（delivery_kind=audit）不动；
//	④ 幂等重入零命中。
//
// 真实 PG 8217 受控变量（shop- 前缀隔离，测试库持久实例下用独一前缀+清理）。
func TestReclaimOutboundSweep(t *testing.T) {
	if testing.Short() {
		t.Skip("需要测试库")
	}
	database := testutil.NewTestDB(t, &model.MessageHub{})
	if database == nil {
		t.Fatal("测试库不可达：回收语义是否收口无法判定")
	}
	ctx := context.Background()
	repo := NewMessageHubRepositoryWithDB(database)
	t.Cleanup(func() {
		database.Where("conversation_id LIKE ?", "reclaim-sweep-%").Delete(&model.MessageHub{})
	})

	seq := 0
	mk := func(status string, age time.Duration, claimedAge time.Duration, attempts int, extra model.JSONMap) model.MessageHub {
		seq++
		row := model.MessageHub{
			TenantID: 1, Platform: "taobao", AccountID: "reclaim-sweep-acc", Direction: "outbound",
			Status: status, MsgType: "text", SenderID: "reclaim-sweep-acc", ReceiverID: "reclaim-sweep-peer",
			Content: "回收巡检样本", ConversationID: "reclaim-sweep-conv",
			MsgID: fmt.Sprintf("reclaim-sweep-%s-%d-%d", status, seq, time.Now().UnixNano()),
			PushAttempts: attempts, Extra: extra,
		}
		if claimedAge >= 0 {
			c := time.Now().Add(-claimedAge)
			row.ClaimedAt = &c
		}
		row.CreatedAt = time.Now().Add(-age)
		row.UpdatedAt = row.CreatedAt
		return row
	}

	seed := []model.MessageHub{
		mk("inflight", time.Hour, 2*time.Minute, 1, nil),                                        // ① 超时 inflight → pending
		mk("pending", time.Hour, -1, MaxOutboundPushAttempts, nil),                              // ② 到界 pending → failed
		mk("inflight", time.Hour, 2*time.Minute, MaxOutboundPushAttempts, nil),                  // ② 到界 inflight → failed
		mk("pending", 30*24*time.Hour, -1, 1, nil),                                              // ③ 陈旧 pending → expired
		mk("inflight", 30*24*time.Hour, 2*time.Minute, 1, nil),                                  // ③ 陈旧 inflight → expired
		mk("pending", time.Hour, -1, 1, nil),                                                    // 新鲜 pending → 不动
		mk("pending", 30*24*time.Hour, -1, 1, model.JSONMap{"duplicate_of": "mh:orig"}),         // duplicate 占位行 → 不动
		mk("pending", 30*24*time.Hour, -1, 1, model.JSONMap{"delivery_kind": "audit"}),          // 审计行 → 不动
		mk("send_failed", 30*24*time.Hour, -1, 1, nil),                                          // send_failed 终态 → 不动
	}
	for i := range seed {
		if err := database.Create(&seed[i]).Error; err != nil {
			t.Fatalf("造样失败[%d]: %v", i, err)
		}
	}

	reclaimed, exhausted, aged, err := repo.ReclaimOutboundSweep(ctx, 30*time.Second, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("回收巡检失败: %v", err)
	}
	// ①+③ 的两行 inflight（claimed_at 均过界）都先被 b 步回收；③ 的行随后被 c 步老化。
	if reclaimed != 2 {
		t.Errorf("① 超时 inflight 回收数=%d，期望 2", reclaimed)
	}
	if exhausted != 2 {
		t.Errorf("② 到界升级数=%d，期望 2", exhausted)
	}
	if aged != 2 {
		t.Errorf("③ 陈旧老化数=%d，期望 2", aged)
	}

	var statuses map[string]int
	var rows []model.MessageHub
	if err := database.Where("conversation_id = ?", "reclaim-sweep-conv").Find(&rows).Error; err != nil {
		t.Fatalf("回读失败: %v", err)
	}
	statuses = map[string]int{}
	for _, r := range rows {
		statuses[r.Status]++
		if r.Status == "expired" && r.PushError != pushStaleExpiredError {
			t.Errorf("③ 老化行 push_error 应为 outbound_stale_expired，got %q", r.PushError)
		}
	}
	// pending=① 回收 1 + 新鲜 1 + duplicate 占位 1（谓词保护不动）+ audit 1（同）
	if statuses["pending"] != 4 {
		t.Errorf("pending 应 4（①回收+新鲜+两行谓词保护），got %d", statuses["pending"])
	}
	for _, r := range rows {
		if r.Extra != nil {
			if _, isDup := r.Extra["duplicate_of"]; isDup && r.Status != "pending" {
				t.Errorf("duplicate 占位行被回收翻态（谓词失效）: id=%d status=%s", r.ID, r.Status)
			}
			if r.Extra["delivery_kind"] == "audit" && r.Status != "pending" {
				t.Errorf("audit 行被回收翻态（谓词失效）: id=%d status=%s", r.ID, r.Status)
			}
		}
	}
	if statuses["failed"] != 2 { // ② 两行
		t.Errorf("② failed 应 2，got %d", statuses["failed"])
	}
	if statuses["expired"] != 2 { // ③ 两行
		t.Errorf("③ expired 应 2，got %d", statuses["expired"])
	}
	if statuses["send_failed"] != 1 {
		t.Errorf("send_failed 终态不得被老化，got %d", statuses["send_failed"])
	}

	// ④ 幂等重入
	r2, e2, a2, err := repo.ReclaimOutboundSweep(ctx, 30*time.Second, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("④ 幂等重入失败: %v", err)
	}
	if r2+e2+a2 != 0 {
		t.Errorf("④ 重入应零命中: reclaimed=%d exhausted=%d aged=%d", r2, e2, a2)
	}
}
