package channelgw

// CH-37b 刀2：B4 pipeline 回执扩展测试（真实 PG 8217 受控变量）。
//
// 覆盖契约：
//   ① AckOutboundDetailed 经 pipeline 委托既有 v2 详细 ack（items 逐项分类）
//   ② ClaimOutbound 透出 claimed_at/push_attempts（既有列，认领返回行必带）
//   ③ 可选 claim 栅栏：带正确 claimed_at→翻转；带过期 claimed_at（已回收重领）→不翻转
//     且归 duplicate 分类；不带→legacy 行为完全不变
//   ⑤ legacy AckOutbound（AckOutboundDeliveredBatch 语义）回归零变化
//   ⑥ 并发：同一行两个 acker 竞争仅一个成功
//
// 受控变量手法：认领租约超时（30s）不等待——直接用 raw DB 把 claimed_at 拨到过去，
// 再走一次 ClaimOutbound 触发惰性回收+重领（与 inbox_ingress_ack_test.go 的 StaleReset 同款）。

import (
	"context"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/service"
)

func newAckFencePipeline(t *testing.T) (IngressPipeline, *service.InboxIngressService) {
	t.Helper()
	database := testutil.NewTestDB(t, &model.MessageHub{})
	db.SetTestDB(database)
	t.Cleanup(func() { db.SetTestDB(nil) })
	ingress := service.NewInboxIngressService()
	return NewPipeline(ingress), ingress
}

// seedOutbound 经 service.DeliverOutbound 落一条 pending 出站行（与既有 ack 测试同款夹具）。
func seedOutbound(t *testing.T, ingress *service.InboxIngressService, channel, accountID, conv, content string) string {
	t.Helper()
	h := &model.MessageHub{
		MsgID:          service.ContentHashMsgID(channel, conv, content),
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
	if err := ingress.DeliverOutbound(context.Background(), h); err != nil {
		t.Fatalf("DeliverOutbound 失败: %v", err)
	}
	return h.MsgID
}

// hubRow 直读库内行（受控变量断言用；GetByMsgID 在 repo 层，测试直接走测试库句柄）。
func hubRow(t *testing.T, msgID string) *model.MessageHub {
	t.Helper()
	gdb := db.GetDB()
	if gdb == nil {
		t.Fatal("测试 DB 未就绪")
	}
	var hub model.MessageHub
	if err := gdb.Where("msg_id = ?", msgID).First(&hub).Error; err != nil {
		t.Fatalf("读行失败 msg_id=%s: %v", msgID, err)
	}
	return &hub
}

func mustClaimedAt(t *testing.T, h *model.MessageHub) time.Time {
	t.Helper()
	if h.ClaimedAt == nil {
		t.Fatalf("claimed_at 不应为空（认领返回行必带租约）")
	}
	return *h.ClaimedAt
}

// TestPipeline_AckOutboundDetailed_DelegationClassification 契约①：
// pipeline 详细 ack 委托既有 v2 实现——items 逐项 acked/duplicate/not_found/not_in_scope 分类。
func TestPipeline_AckOutboundDetailed_DelegationClassification(t *testing.T) {
	p, ingress := newAckFencePipeline(t)
	ctx := context.Background()

	const (
		channel   = "douyin_web"
		accountID = "acc_detailed_1"
		conv      = "conv_detailed_1"
	)
	idA := seedOutbound(t, ingress, channel, accountID, conv, "detailed A")
	idB := seedOutbound(t, ingress, channel, accountID, conv, "detailed B")
	// not_in_scope：同渠道不同账号的行（channel 匹配、account 不匹配）。
	idOther := seedOutbound(t, ingress, channel, "acc_detailed_other", conv, "detailed other")
	// not_found：任何库行都没有的 msg_id。
	const idGhost = "detail-msg-not-exist"

	hubs, err := p.ClaimOutbound(ctx, channel, accountID, 10)
	if err != nil {
		t.Fatalf("ClaimOutbound 不应失败: %v", err)
	}
	if len(hubs) != 2 {
		t.Fatalf("应认领到本账号 2 条，实际 %d", len(hubs))
	}

	res, err := p.AckOutboundDetailed(ctx, channel, accountID, []string{idA, idB, idGhost, idOther}, "", model.BridgeAckStatusDelivered, nil, nil)
	if err != nil {
		t.Fatalf("AckOutboundDetailed 不应失败: %v", err)
	}
	if res.AckedItemsCount != 2 {
		t.Errorf("AckedItemsCount = %d, want 2", res.AckedItemsCount)
	}
	if res.NotFoundCount != 1 || res.NotInScopeCount != 1 {
		t.Errorf("NotFound/NotInScope = %d/%d, want 1/1", res.NotFoundCount, res.NotInScopeCount)
	}
	byID := map[string]service.AckOutboundItem{}
	for _, it := range res.Items {
		byID[it.MsgID] = it
	}
	if it := byID[idA]; it.Status != model.BridgeAckStatusAcked {
		t.Errorf("idA 分类 = %q, want acked", it.Status)
	}
	if it := byID[idB]; it.Status != model.BridgeAckStatusAcked {
		t.Errorf("idB 分类 = %q, want acked", it.Status)
	}
	if it := byID[idGhost]; it.Status != model.BridgeAckStatusNotFound {
		t.Errorf("idGhost 分类 = %q, want not_found", it.Status)
	}
	if it := byID[idOther]; it.Status != model.BridgeAckStatusNotInScope {
		t.Errorf("idOther 分类 = %q, want not_in_scope", it.Status)
	}

	// 幂等回执：重 ack 同一批 → duplicate 分类。
	res2, err := p.AckOutboundDetailed(ctx, channel, accountID, []string{idA, idB}, "", model.BridgeAckStatusDelivered, nil, nil)
	if err != nil {
		t.Fatalf("重 ack 不应失败: %v", err)
	}
	if res2.DuplicateCount != 2 {
		t.Errorf("重 ack DuplicateCount = %d, want 2", res2.DuplicateCount)
	}
	// 行落终态。
	if got := hubRow(t, idA).Status; got != model.BridgeAckStatusDelivered {
		t.Errorf("idA 状态 = %q, want delivered", got)
	}
}

// TestPipeline_AckOutboundDetailed_FailedPath 契约①/④的服务端半边：
// terminalStatus=failed 走 failed 终态翻转，逐项分类 returned failed。
func TestPipeline_AckOutboundDetailed_FailedPath(t *testing.T) {
	p, ingress := newAckFencePipeline(t)
	ctx := context.Background()

	const (
		channel   = "douyin_web"
		accountID = "acc_detailed_failed"
		conv      = "conv_detailed_failed"
	)
	idF := seedOutbound(t, ingress, channel, accountID, conv, "detailed failed")

	if _, err := p.ClaimOutbound(ctx, channel, accountID, 10); err != nil {
		t.Fatalf("ClaimOutbound 不应失败: %v", err)
	}

	res, err := p.AckOutboundDetailed(ctx, channel, accountID, []string{idF}, "", model.BridgeAckStatusFailed, nil, nil)
	if err != nil {
		t.Fatalf("AckOutboundDetailed(failed) 不应失败: %v", err)
	}
	if res.FailedItemsCount != 1 || len(res.Items) != 1 || res.Items[0].Status != model.BridgeAckStatusFailed {
		t.Errorf("failed 分类异常: count=%d items=%+v", res.FailedItemsCount, res.Items)
	}
	if got := hubRow(t, idF).Status; got != model.BridgeAckStatusFailed {
		t.Errorf("行状态 = %q, want failed（停止重推）", got)
	}
}

// TestPipeline_ClaimOutbound_ExposesClaimLeaseFields 契约②：
// 认领返回行透出 claimed_at/push_attempts（既有列；ack 栅栏的锚点来源）。
func TestPipeline_ClaimOutbound_ExposesClaimLeaseFields(t *testing.T) {
	p, ingress := newAckFencePipeline(t)
	ctx := context.Background()

	const (
		channel   = "douyin_web"
		accountID = "acc_lease_1"
		conv      = "conv_lease_1"
	)
	idL := seedOutbound(t, ingress, channel, accountID, conv, "lease row")

	hubs, err := p.ClaimOutbound(ctx, channel, accountID, 10)
	if err != nil {
		t.Fatalf("ClaimOutbound 不应失败: %v", err)
	}
	if len(hubs) != 1 || hubs[0].MsgID != idL {
		t.Fatalf("应认领到 1 条 %s，实际 %+v", idL, hubs)
	}
	h := hubs[0]
	if h.ClaimedAt == nil {
		t.Error("认领返回行 ClaimedAt 不应为空（栅栏锚点必须随认领透出）")
	}
	if h.PushAttempts != 1 {
		t.Errorf("首次认领 PushAttempts = %d, want 1", h.PushAttempts)
	}

	// 二次认领（超时回收+重领）后 push_attempts 递增、claimed_at 刷新。
	if err := ingressRawShiftClaimedAt(t, channel, idL); err != nil {
		t.Fatalf("拨动 claimed_at 失败: %v", err)
	}
	again, err := p.ClaimOutbound(ctx, channel, accountID, 10)
	if err != nil || len(again) != 1 {
		t.Fatalf("回收重领应取回 1 条, err=%v len=%d", err, len(again))
	}
	if again[0].PushAttempts != 2 {
		t.Errorf("重领后 PushAttempts = %d, want 2", again[0].PushAttempts)
	}
	if h.ClaimedAt != nil && again[0].ClaimedAt != nil && !again[0].ClaimedAt.After(*h.ClaimedAt) {
		t.Error("重领后 claimed_at 应晚于首次认领")
	}
}

// ingressRawShiftClaimedAt 把某行 claimed_at 拨到认领租约之外（受控变量：不真等 30s）。
func ingressRawShiftClaimedAt(t *testing.T, channel, msgID string) error {
	t.Helper()
	database := db.GetDB()
	if database == nil {
		t.Fatal("测试 DB 未就绪")
	}
	stale := time.Now().Add(-2 * service.InboxOutboundClaimTimeout)
	return database.Model(&model.MessageHub{}).
		Where("platform = ? AND msg_id = ?", channel, msgID).
		Update("claimed_at", stale).Error
}

// TestPipeline_AckOutboundDetailed_Fence_MatchingClaimFlips 契约③a：
// 带认领时所见的 claimed_at（正确租约）→ 翻转成功。
func TestPipeline_AckOutboundDetailed_Fence_MatchingClaimFlips(t *testing.T) {
	p, ingress := newAckFencePipeline(t)
	ctx := context.Background()

	const (
		channel   = "douyin_web"
		accountID = "acc_fence_ok"
		conv      = "conv_fence_ok"
	)
	idM := seedOutbound(t, ingress, channel, accountID, conv, "fence match")

	hubs, err := p.ClaimOutbound(ctx, channel, accountID, 10)
	if err != nil || len(hubs) != 1 {
		t.Fatalf("认领失败: err=%v len=%d", err, len(hubs))
	}
	lease := mustClaimedAt(t, hubs[0])

	res, err := p.AckOutboundDetailed(ctx, channel, accountID, []string{idM}, "", model.BridgeAckStatusDelivered, nil,
		service.AckOutboundClaimFence{idM: lease})
	if err != nil {
		t.Fatalf("栅栏 ack 不应失败: %v", err)
	}
	if res.AckedItemsCount != 1 {
		t.Errorf("栅栏匹配应翻转 1 条, got %d", res.AckedItemsCount)
	}
	if got := hubRow(t, idM).Status; got != model.BridgeAckStatusDelivered {
		t.Errorf("行状态 = %q, want delivered", got)
	}
}

// TestPipeline_AckOutboundDetailed_Fence_StaleClaimedAtNotFlipped 契约③b：
// 带过期 claimed_at（行已被回收重领）→ 不翻转 + duplicate 分类；换新租约后正常翻转。
func TestPipeline_AckOutboundDetailed_Fence_StaleClaimedAtNotFlipped(t *testing.T) {
	p, ingress := newAckFencePipeline(t)
	ctx := context.Background()

	const (
		channel   = "douyin_web"
		accountID = "acc_fence_stale"
		conv      = "conv_fence_stale"
	)
	idS := seedOutbound(t, ingress, channel, accountID, conv, "fence stale")

	first, err := p.ClaimOutbound(ctx, channel, accountID, 10)
	if err != nil || len(first) != 1 {
		t.Fatalf("首次认领失败: err=%v len=%d", err, len(first))
	}
	staleLease := mustClaimedAt(t, first[0])

	// 受控变量：租约过期 → 回收重领，行归属新一轮认领。
	if err := ingressRawShiftClaimedAt(t, channel, idS); err != nil {
		t.Fatalf("拨动 claimed_at 失败: %v", err)
	}
	reclaimed, err := p.ClaimOutbound(ctx, channel, accountID, 10)
	if err != nil || len(reclaimed) != 1 {
		t.Fatalf("回收重领失败: err=%v len=%d", err, len(reclaimed))
	}
	freshLease := mustClaimedAt(t, reclaimed[0])
	if !freshLease.After(staleLease) {
		t.Fatalf("重领应产生新租约: stale=%v fresh=%v", staleLease, freshLease)
	}

	// 旧 acker 迟到的 ack（带旧租约）→ 不翻转，duplicate 分类。
	res, err := p.AckOutboundDetailed(ctx, channel, accountID, []string{idS}, "", model.BridgeAckStatusDelivered, nil,
		service.AckOutboundClaimFence{idS: staleLease})
	if err != nil {
		t.Fatalf("过期租约 ack 不应报错: %v", err)
	}
	if res.AckedItemsCount != 0 || res.AffectedCount != 0 {
		t.Errorf("过期租约绝不能翻转: acked=%d affected=%d", res.AckedItemsCount, res.AffectedCount)
	}
	if res.DuplicateCount != 1 {
		t.Errorf("过期租约分类 = DuplicateCount %d, want 1", res.DuplicateCount)
	}
	row := hubRow(t, idS)
	if row.Status != "inflight" {
		t.Errorf("行状态 = %q, want 仍 inflight（未被旧 acker 覆盖）", row.Status)
	}
	if row.ClaimedAt == nil || !row.ClaimedAt.Equal(freshLease) {
		t.Errorf("行租约应仍是新认领的 claimed_at，实际 %v", row.ClaimedAt)
	}

	// 新 acker（带新租约）→ 翻转成功。
	res2, err := p.AckOutboundDetailed(ctx, channel, accountID, []string{idS}, "", model.BridgeAckStatusDelivered, nil,
		service.AckOutboundClaimFence{idS: freshLease})
	if err != nil {
		t.Fatalf("新租约 ack 不应失败: %v", err)
	}
	if res2.AckedItemsCount != 1 {
		t.Errorf("新租约应翻转 1 条, got %d", res2.AckedItemsCount)
	}
	if got := hubRow(t, idS).Status; got != model.BridgeAckStatusDelivered {
		t.Errorf("行状态 = %q, want delivered", got)
	}
}

// TestPipeline_AckOutboundDetailed_Fence_AbsentLegacyBehavior 契约③c：
// 不带栅栏 = legacy 行为完全不变——未认领的 pending 行照样翻转（legacy 详细 ack 谓词
// status IN ('pending','inflight')，不看认领者）。
func TestPipeline_AckOutboundDetailed_Fence_AbsentLegacyBehavior(t *testing.T) {
	p, ingress := newAckFencePipeline(t)
	ctx := context.Background()

	const (
		channel   = "douyin_web"
		accountID = "acc_fence_none"
		conv      = "conv_fence_none"
	)
	idN := seedOutbound(t, ingress, channel, accountID, conv, "fence absent legacy")

	// 不认领、不带 fence：legacy 详细 ack 直接翻转 pending。
	res, err := p.AckOutboundDetailed(ctx, channel, accountID, []string{idN}, "", model.BridgeAckStatusDelivered, nil, nil)
	if err != nil {
		t.Fatalf("legacy ack 不应失败: %v", err)
	}
	if res.AckedItemsCount != 1 {
		t.Errorf("legacy（无栅栏）应翻转 pending 行 1 条, got %d", res.AckedItemsCount)
	}
	if got := hubRow(t, idN).Status; got != model.BridgeAckStatusDelivered {
		t.Errorf("行状态 = %q, want delivered", got)
	}
}

// TestPipeline_AckOutbound_LegacyZeroBehaviorChange 契约⑤：
// legacy AckOutbound（repo AckOutboundDeliveredBatch 语义）零变化——不看认领者，
// pending/inflight 一律翻转，返回翻转行数（幂等不计）。
func TestPipeline_AckOutbound_LegacyZeroBehaviorChange(t *testing.T) {
	p, ingress := newAckFencePipeline(t)
	ctx := context.Background()

	const (
		channel   = "douyin_web"
		accountID = "acc_legacy_ack"
		conv      = "conv_legacy_ack"
	)
	idPending := seedOutbound(t, ingress, channel, accountID, conv, "legacy pending")
	idInflight := seedOutbound(t, ingress, channel, accountID, conv, "legacy inflight")

	hubs, err := p.ClaimOutbound(ctx, channel, accountID, 10)
	if err != nil || len(hubs) != 2 {
		t.Fatalf("认领失败: err=%v len=%d", err, len(hubs))
	}

	// legacy ack：pending + inflight（带 claimed_at 的行）都翻转，不看租约。
	n, err := p.AckOutbound(ctx, channel, accountID, []string{idPending, idInflight})
	if err != nil {
		t.Fatalf("legacy ack 不应失败: %v", err)
	}
	if n != 2 {
		t.Errorf("legacy ack 翻转行数 = %d, want 2（pending/inflight 均可翻转，语义零变化）", n)
	}
	if got := hubRow(t, idPending).Status; got != model.BridgeAckStatusDelivered {
		t.Errorf("pending 行状态 = %q, want delivered", got)
	}
	if got := hubRow(t, idInflight).Status; got != model.BridgeAckStatusDelivered {
		t.Errorf("inflight 行状态 = %q, want delivered", got)
	}

	// 幂等：重 ack 返回 0。
	n2, err := p.AckOutbound(ctx, channel, accountID, []string{idPending, idInflight})
	if err != nil || n2 != 0 {
		t.Errorf("重 ack 应幂等返回 0, got n=%d err=%v", n2, err)
	}
}

// TestPipeline_AckOutboundDetailed_Fence_ConcurrentAckersSingleWinner 契约⑥：
// 同一行两个 acker 并发竞争（同持同一份有效租约），栅栏+条件更新保证仅一个翻转成功，
// 另一个得 duplicate 分类；终态唯一且 delivered。
func TestPipeline_AckOutboundDetailed_Fence_ConcurrentAckersSingleWinner(t *testing.T) {
	p, ingress := newAckFencePipeline(t)
	ctx := context.Background()

	const (
		channel   = "douyin_web"
		accountID = "acc_fence_race"
		conv      = "conv_fence_race"
	)
	idR := seedOutbound(t, ingress, channel, accountID, conv, "fence race")

	hubs, err := p.ClaimOutbound(ctx, channel, accountID, 10)
	if err != nil || len(hubs) != 1 {
		t.Fatalf("认领失败: err=%v len=%d", err, len(hubs))
	}
	lease := mustClaimedAt(t, hubs[0])

	const racers = 2
	type outcome struct {
		acked     int
		duplicate int
		affected  int
	}
	results := make([]outcome, racers)
	var wg sync.WaitGroup
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			res, err := p.AckOutboundDetailed(ctx, channel, accountID, []string{idR}, "", model.BridgeAckStatusDelivered, nil,
				service.AckOutboundClaimFence{idR: lease})
			if err != nil {
				t.Errorf("竞速 ack 不应报错: %v", err)
				return
			}
			results[idx] = outcome{acked: res.AckedItemsCount, duplicate: res.DuplicateCount, affected: res.AffectedCount}
		}(i)
	}
	wg.Wait()

	totalAcked, totalDup, totalAffected := 0, 0, 0
	for _, r := range results {
		totalAcked += r.acked
		totalDup += r.duplicate
		totalAffected += r.affected
	}
	if totalAcked != 1 || totalAffected != 1 {
		t.Errorf("仅应一个 acker 成功: acked=%d affected=%d (want 1/1)", totalAcked, totalAffected)
	}
	if totalDup != racers-1 {
		t.Errorf("败者应归 duplicate: %d (want %d)", totalDup, racers-1)
	}
	if got := hubRow(t, idR).Status; got != model.BridgeAckStatusDelivered {
		t.Errorf("行状态 = %q, want delivered", got)
	}
}
