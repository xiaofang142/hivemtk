package service

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func seedPendingOutbound(t *testing.T, db *gorm.DB, channel, accountID, conv, content string) *model.MessageHub {
	t.Helper()
	h := &model.MessageHub{
		MsgID:          ContentHashMsgID(channel, conv, content),
		Platform:       channel,
		AccountID:      accountID,
		Direction:      "outbound",
		Status:         "pending",
		MsgType:        "text",
		SenderID:       "agent_1",
		ReceiverID:     conv,
		Content:        content,
		ConversationID: conv,
		IsRead:         true,
	}
	if err := db.Create(h).Error; err != nil {
		t.Fatalf("seed pending outbound 失败: %v", err)
	}
	return h
}

// TestInboxIngress_ClaimPendingOutbound_ConcurrentNoDuplicate 并发边界：
// 多 goroutine 并发认领同一批 pending（模拟多标签页/多扩展并发轮询）。
// 修复前（无 FOR UPDATE SKIP LOCKED）同一行会被多个轮询同时认领 → 重复转发；
// 修复后同一条只能被一个轮询认领 → 并集恰好覆盖全部 pending 且零重复。
func TestInboxIngress_ClaimPendingOutbound_ConcurrentNoDuplicate(t *testing.T) {
	db := testutil.NewTestDB(t, &model.MessageHub{})
	if err := db.Exec("DELETE FROM message_hub").Error; err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	svc := NewInboxIngressServiceWithDB(db, newIsolatedCacheForTest(t))
	ctx := context.Background()

	const (
		channel   = "douyin_web"
		accountID = "acc_conc_1"
		total     = 100
		workers   = 16
	)
	for i := 0; i < total; i++ {
		seedPendingOutbound(t, db, channel, accountID, "conv_c", "concurrent outbound "+string(rune('A'+i%26))+strconv.Itoa(i))
	}

	results := make([][]uint, workers)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for iter := 0; iter < 50; iter++ {
				claimed, err := svc.ClaimPendingOutbound(ctx, channel, accountID, 10)
				if err != nil {
					t.Errorf("worker %d 认领失败: %v", w, err)
					return
				}
				if len(claimed) == 0 {
					return
				}
				for _, m := range claimed {
					results[w] = append(results[w], m.ID)
				}
			}
		}(w)
	}
	wg.Wait()

	seen := make(map[uint]int)
	var order []uint
	for w := 0; w < workers; w++ {
		for _, id := range results[w] {
			if seen[id] == 0 {
				order = append(order, id)
			}
			seen[id]++
		}
	}
	if len(order) != total {
		t.Fatalf("并发认领后覆盖的 distinct id 应为 %d，实际 %d（存在未被认领或重复认领）", total, len(order))
	}
	for _, id := range order {
		if seen[id] > 1 {
			t.Fatalf("id=%d 被 %d 个并发轮询重复认领（重复转发！）", id, seen[id])
		}
	}

	var msgIDs []string
	for _, id := range order {
		var h model.MessageHub
		if err := db.First(&h, id).Error; err != nil {
			t.Fatalf("取回已认领行失败: %v", err)
		}
		msgIDs = append(msgIDs, h.MsgID)
	}
	n, err := svc.AckOutboundDelivered(ctx, channel, accountID, msgIDs)
	if err != nil {
		t.Fatalf("AckOutboundDelivered 失败: %v", err)
	}
	if int(n) != total {
		t.Fatalf("期望确认 %d 条，实际 %d", total, n)
	}
	if again, err := svc.ClaimPendingOutbound(ctx, channel, accountID, 10); err != nil || len(again) != 0 {
		t.Fatalf("已 delivered 不应再认领，again=%d err=%v", len(again), err)
	}
}

// TestInboxIngress_ClaimPendingOutbound_RespectsLimit 验证 limit 精确截断且按 id ASC 顺序、
// 跨多轮耗尽全部 pending、空转返回 0。
func TestInboxIngress_ClaimPendingOutbound_RespectsLimit(t *testing.T) {
	db := testutil.NewTestDB(t, &model.MessageHub{})
	if err := db.Exec("DELETE FROM message_hub").Error; err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	svc := NewInboxIngressServiceWithDB(db, newIsolatedCacheForTest(t))
	ctx := context.Background()

	const (
		channel   = "douyin_web"
		accountID = "acc_limit_1"
		total     = 10
		batch     = 3
	)
	for i := 0; i < total; i++ {
		seedPendingOutbound(t, db, channel, accountID, "conv_l", "limit outbound "+strconv.Itoa(i))
	}

	claimedIDs := make([]uint, 0, total)
	for round := 0; round < total/batch+2; round++ {
		claimed, err := svc.ClaimPendingOutbound(ctx, channel, accountID, batch)
		if err != nil {
			t.Fatalf("第 %d 轮认领失败: %v", round, err)
		}
		if len(claimed) > batch {
			t.Fatalf("第 %d 轮返回 %d 条，超过 limit=%d", round, len(claimed), batch)
		}
		for _, m := range claimed {
			if m.Status != "inflight" {
				t.Errorf("认领状态应为 inflight，实际 %q", m.Status)
			}
			claimedIDs = append(claimedIDs, m.ID)
		}
		if len(claimed) == 0 {
			break
		}
	}
	if len(claimedIDs) != total {
		t.Fatalf("分轮认领应恰好取回 %d 条，实际 %d", total, len(claimedIDs))
	}
	if extra, err := svc.ClaimPendingOutbound(ctx, channel, accountID, batch); err != nil || len(extra) != 0 {
		t.Fatalf("耗尽后不应再认领，extra=%d err=%v", len(extra), err)
	}
}

// TestInboxIngress_AckOutboundDelivered_CrossSession 跨会话同 msg_id：
// 同一内容出现在多个会话（复合唯一索引允许），ack 一次应翻转该 (channel,account) 下所有匹配行。
func TestInboxIngress_AckOutboundDelivered_CrossSession(t *testing.T) {
	db := testutil.NewTestDB(t, &model.MessageHub{})
	if err := db.Exec("DELETE FROM message_hub").Error; err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	svc := NewInboxIngressServiceWithDB(db, newIsolatedCacheForTest(t))
	ctx := context.Background()

	const (
		channel   = "douyin_web"
		accountID = "acc_xsess_1"
		content   = "跨会话相同内容"
	)
	msgID := ContentHashMsgID(channel, "conv_x1", content)
	for _, conv := range []string{"conv_x1", "conv_x2"} {
		h := &model.MessageHub{
			MsgID:          msgID,
			Platform:       channel,
			AccountID:      accountID,
			Direction:      "outbound",
			Status:         "pending",
			MsgType:        "text",
			SenderID:       "agent_1",
			ReceiverID:     conv,
			Content:        content,
			ConversationID: conv,
			IsRead:         true,
		}
		if err := db.Create(h).Error; err != nil {
			t.Fatalf("插入跨会话行失败: %v", err)
		}
	}
	seedPendingOutbound(t, db, channel, accountID, "conv_other", "另一条内容")
	oth := &model.MessageHub{
		MsgID:          msgID,
		Platform:       channel,
		AccountID:      "acc_xsess_OTHER",
		Direction:      "outbound",
		Status:         "pending",
		MsgType:        "text",
		SenderID:       "agent_1",
		ReceiverID:     "conv_x1b",
		Content:        content,
		ConversationID: "conv_x1b",
		IsRead:         true,
	}
	if err := db.Create(oth).Error; err != nil {
		t.Fatalf("插入越权行失败: %v", err)
	}

	n, err := svc.AckOutboundDelivered(ctx, channel, accountID, []string{msgID})
	if err != nil {
		t.Fatalf("AckOutboundDelivered 失败: %v", err)
	}
	if int(n) != 2 {
		t.Fatalf("跨会话同 msg_id 应翻转 2 条，实际 %d", n)
	}
	assertStatus(t, db, channel, accountID, "conv_x1", msgID, "delivered")
	assertStatus(t, db, channel, accountID, "conv_x2", msgID, "delivered")
	assertStatus(t, db, channel, accountID, "conv_other", ContentHashMsgID(channel, "conv_other", "另一条内容"), "pending")

	var o model.MessageHub
	if err := db.First(&o, oth.ID).Error; err != nil {
		t.Fatalf("取回越权行失败: %v", err)
	}
	if o.Status != "pending" {
		t.Fatalf("越权账号的行不应被翻转，实际 %q", o.Status)
	}
}

// TestInboxIngress_AckOutboundDelivered_OwnershipIsolation 归属隔离：
// 账号 A 的 pending 行，账号 B 发起的 ack 不应触碰。
func TestInboxIngress_AckOutboundDelivered_OwnershipIsolation(t *testing.T) {
	db := testutil.NewTestDB(t, &model.MessageHub{})
	if err := db.Exec("DELETE FROM message_hub").Error; err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	svc := NewInboxIngressServiceWithDB(db, newIsolatedCacheForTest(t))
	ctx := context.Background()

	const (
		channel  = "douyin_web"
		accountA = "acc_owner_A"
		accountB = "acc_owner_B"
		content  = "归属隔离内容"
		conv     = "conv_owner"
	)
	hA := seedPendingOutbound(t, db, channel, accountA, conv, content)
	_ = seedPendingOutbound(t, db, channel, accountB, conv, "归属隔离内容_B")

	n, err := svc.AckOutboundDelivered(ctx, channel, accountB, []string{hA.MsgID})
	if err != nil {
		t.Fatalf("AckOutboundDelivered(B) 失败: %v", err)
	}
	if int(n) != 0 {
		t.Fatalf("越权 ack 应影响 0 条，实际 %d", n)
	}
	assertStatus(t, db, channel, accountA, conv, hA.MsgID, "pending")

	n2, err := svc.AckOutboundDelivered(ctx, channel, accountA, []string{hA.MsgID})
	if err != nil {
		t.Fatalf("AckOutboundDelivered(A) 失败: %v", err)
	}
	if int(n2) != 1 {
		t.Fatalf("A 自 ack 应翻转 1 条，实际 %d", n2)
	}
}

// TestInboxIngress_AckOutboundDelivered_Idempotent 幂等与异常入参：
// 已 delivered 再 ack 返回 0；空 msgIDs 返回 (0,nil)；不存在的 msg_id 返回 0。
func TestInboxIngress_AckOutboundDelivered_Idempotent(t *testing.T) {
	db := testutil.NewTestDB(t, &model.MessageHub{})
	if err := db.Exec("DELETE FROM message_hub").Error; err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	svc := NewInboxIngressServiceWithDB(db, newIsolatedCacheForTest(t))
	ctx := context.Background()

	const (
		channel   = "douyin_web"
		accountID = "acc_idem_1"
		conv      = "conv_idem"
		content   = "幂等内容"
	)
	h := seedPendingOutbound(t, db, channel, accountID, conv, content)

	if n, err := svc.AckOutboundDelivered(ctx, channel, accountID, nil); err != nil || n != 0 {
		t.Fatalf("空 msgIDs 应返回 (0,nil)，实际 (%d,%v)", n, err)
	}
	if n, err := svc.AckOutboundDelivered(ctx, channel, accountID, []string{"nope"}); err != nil || n != 0 {
		t.Fatalf("不存在 msg_id 应返回 (0,nil)，实际 (%d,%v)", n, err)
	}
	n, err := svc.AckOutboundDelivered(ctx, channel, accountID, []string{h.MsgID})
	if err != nil || int(n) != 1 {
		t.Fatalf("首次 ack 应返回 1，实际 (%d,%v)", n, err)
	}
	n2, err := svc.AckOutboundDelivered(ctx, channel, accountID, []string{h.MsgID})
	if err != nil || int(n2) != 0 {
		t.Fatalf("二次 ack 应幂等返回 0，实际 (%d,%v)", n2, err)
	}
}

// TestInboxIngress_ClaimPendingOutbound_InflightThenAck 认领(inflight)后 ack 仍能翻转 inflight→delivered。
func TestInboxIngress_ClaimPendingOutbound_InflightThenAck(t *testing.T) {
	db := testutil.NewTestDB(t, &model.MessageHub{})
	if err := db.Exec("DELETE FROM message_hub").Error; err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	svc := NewInboxIngressServiceWithDB(db, newIsolatedCacheForTest(t))
	ctx := context.Background()

	const (
		channel   = "douyin_web"
		accountID = "acc_inf_1"
		conv      = "conv_inf"
		content   = "inflight 内容"
	)
	h := seedPendingOutbound(t, db, channel, accountID, conv, content)

	claimed, err := svc.ClaimPendingOutbound(ctx, channel, accountID, 10)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("认领失败 len=%d err=%v", len(claimed), err)
	}
	assertStatus(t, db, channel, accountID, conv, h.MsgID, "inflight")

	n, err := svc.AckOutboundDelivered(ctx, channel, accountID, []string{h.MsgID})
	if err != nil || int(n) != 1 {
		t.Fatalf("ack inflight 应翻转 1 条，实际 (%d,%v)", n, err)
	}
	assertStatus(t, db, channel, accountID, conv, h.MsgID, "delivered")
}

// TestInboxIngress_ClaimPendingOutbound_AckReclaimRace 模拟「认领→ack 丢失→超时回收→重认领→ack」：
// 验证 at-least-once 重下发不丢消息，且迟到 ack 在回收重认领后仍能命中翻转。
func TestInboxIngress_ClaimPendingOutbound_AckReclaimRace(t *testing.T) {
	db := testutil.NewTestDB(t, &model.MessageHub{})
	if err := db.Exec("DELETE FROM message_hub").Error; err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	svc := NewInboxIngressServiceWithDB(db, newIsolatedCacheForTest(t))
	ctx := context.Background()

	const (
		channel   = "douyin_web"
		accountID = "acc_race_1"
		conv      = "conv_race"
		content   = "reclaim 竞态内容"
	)
	h := seedPendingOutbound(t, db, channel, accountID, conv, content)

	first, err := svc.ClaimPendingOutbound(ctx, channel, accountID, 10)
	if err != nil || len(first) != 1 {
		t.Fatalf("首次认领失败 len=%d err=%v", len(first), err)
	}
	stale := time.Now().Add(-60 * time.Second)
	if err := db.Model(&model.MessageHub{}).Where("msg_id = ?", h.MsgID).Update("claimed_at", stale).Error; err != nil {
		t.Fatalf("拨动 claimed_at 失败: %v", err)
	}
	again, err := svc.ClaimPendingOutbound(ctx, channel, accountID, 10)
	if err != nil || len(again) != 1 {
		t.Fatalf("回收重认领失败 len=%d err=%v", len(again), err)
	}
	if again[0].ID != first[0].ID {
		t.Fatalf("回收重认领应返回同一行 id=%d，实际 %d", first[0].ID, again[0].ID)
	}
	assertStatus(t, db, channel, accountID, conv, h.MsgID, "inflight")
	n, err := svc.AckOutboundDelivered(ctx, channel, accountID, []string{h.MsgID})
	if err != nil || int(n) != 1 {
		t.Fatalf("迟到 ack 应翻转 1 条，实际 (%d,%v)", n, err)
	}
	assertStatus(t, db, channel, accountID, conv, h.MsgID, "delivered")
	if extra, err := svc.ClaimPendingOutbound(ctx, channel, accountID, 10); err != nil || len(extra) != 0 {
		t.Fatalf("已 delivered 不应再认领，extra=%d err=%v", len(extra), err)
	}
}

// TestInboxOutboundClaimTimeoutCoversBridgeSendBudget 认领超时下界必须 ≥ 桥端一次发送的最大预算。
//
// 杀掉的变异：把 InboxOutboundClaimTimeout 从 30s 砍到 5s（inbox_ingress_outbound.go:164）。
// 今天这一列没有任何一条腿真钉着它：
//   - sse_outbound_claim_b11_test.go:81/:280 两处写的是 `!= service.InboxOutboundClaimTimeout`，
//     比的是常量自己 ⇒ 恒真，改任何数值都照样绿（它们锁的是「两条路径同源」，不是「这个值合理」）；
//   - 唯一的数字腿 TestInboxIngress_ClaimPendingOutbound_AckReclaimRace 把 claimed_at 往回拨 60s
//     再要求回收成功 ⇒ 只能抓到「变大」（>60s），往下改多少都不红。
//
// 为什么下界取 20s：桥端一次出站发送的预算是 sendOutboundTimeoutMs = 20000
// （user-web/bridge/src/core/constants.js §9），扩展是**发完才 ack**；超时窗比它短 ⇒
// 一条还在真实发送的行被回收成 pending 并被另一条路径重新认领 ⇒ 同一句话打给真实客户两遍
// （§3 定性：双发是这条链路上唯一不可撤销的那一侧）。
// 本腿只钉这一条地板：批3/B4 之后按文案长度上浮的动态预算（humanSendTimeoutMs，封顶 120s）
// 是 §8.3 行 7「靠心跳续期」那条被拒绝的取舍的范围，不在这里假装已经覆盖。
func TestInboxOutboundClaimTimeoutCoversBridgeSendBudget(t *testing.T) {
	const bridgeSendBudgetMs = 20000 // 单一来源：constants.js BRIDGE_THREE_CHANNEL.sendOutboundTimeoutMs
	bridgeSendBudget := time.Duration(bridgeSendBudgetMs) * time.Millisecond
	if InboxOutboundClaimTimeout < bridgeSendBudget {
		t.Fatalf("InboxOutboundClaimTimeout=%v < 桥端单次发送预算 %v：发送还没结束、ack 还没回来，"+
			"这一行就被回收重投 ⇒ 双发", InboxOutboundClaimTimeout, bridgeSendBudget)
	}
}

// TestInboxIngress_ClaimPendingOutbound_GuardZeroNegative 入参守卫：limit<=0 返回 (nil,nil) 不panic。
func TestInboxIngress_ClaimPendingOutbound_GuardZeroNegative(t *testing.T) {
	db := testutil.NewTestDB(t, &model.MessageHub{})
	if err := db.Exec("DELETE FROM message_hub").Error; err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	svc := NewInboxIngressServiceWithDB(db, newIsolatedCacheForTest(t))
	ctx := context.Background()

	for _, lim := range []int{0, -1, -100} {
		got, err := svc.ClaimPendingOutbound(ctx, "douyin_web", "acc_guard", lim)
		if err != nil {
			t.Fatalf("limit=%d 不应报错，err=%v", lim, err)
		}
		if len(got) != 0 {
			t.Fatalf("limit=%d 不应认领任何行（无数据），实际 %d 条", lim, len(got))
		}
	}
}

func assertStatus(t *testing.T, db *gorm.DB, channel, accountID, conv, msgID, want string) {
	t.Helper()
	var h model.MessageHub
	if err := db.Where("platform = ? AND account_id = ? AND conversation_id = ? AND msg_id = ?",
		channel, accountID, conv, msgID).First(&h).Error; err != nil {
		t.Fatalf("查行失败(channel=%s conv=%s msg=%s): %v", channel, conv, msgID, err)
	}
	if h.Status != want {
		t.Fatalf("状态应为 %q，实际 %q", want, h.Status)
	}
}
