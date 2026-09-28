package repository

// message_hub_outbound_push_cap_b20d_test.go — 批20d-A3：出站重推必须有上界并落终态。
//
// 立项事实（§8.3 行 3）： owed 集合的语义本身是对的（pending 或 inflight 且认领超时；
// delivered/failed 自然离开），但**没有任何尝试计数**，于是「扩展每次都发不出去」的行
// （目标会话已被删、页面结构变了却仍在线）以 claimTimeout(30s) 为周期**永久重推**：
// 没有哪一轮会把它升级成终态，也没人知道它已经推过 200 次。同行都有这一界
// （Sidekiq 25 → dead、SQS maxReceiveCount → DLQ、River discarded）。
//
// 本批要锁的四件事，逐格一钉：
//  1. 每次把行交给桥端 = 一次尝试，计数必须留在行上（否则上界无从谈起）；
//  2. 到界即离开 owed 集合，且**三条取行路径同口径**（轮询认领 / SSE 即时推送认领 / SSE 补拉列表）——
//     只给一条加界等于没给（换条路径照样永推）；
//  3. 到界必须落**已有**终态 failed 并写明原因（不是悄悄消失：行还在、能查、能解释）；
//  4. 超时回收（inflight→pending）不许把计数清零，否则预算每 30s 重置一次，上界形同虚设。
//
// 手法沿用本包：真 PG 影子库 + 手动把 claimed_at 推旧来模拟「认领超时」，不造网络桩。

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

const b20dPlatform = "b20d_cap"

func seedOutbound(t *testing.T, db *gorm.DB, msgID, convID, status string, claimedAgo time.Duration) uint {
	t.Helper()
	var claimedAt *time.Time
	if claimedAgo > 0 {
		c := time.Now().Add(-claimedAgo)
		claimedAt = &c
	}
	row := &model.MessageHub{
		Platform: b20dPlatform, AccountID: "acc1", ConversationID: convID, MsgID: msgID,
		MsgType: "text", Content: "c-" + msgID, Direction: "outbound", Status: status, ClaimedAt: claimedAt,
	}
	if err := db.Create(row).Error; err != nil {
		t.Fatalf("seed %s 失败: %v", msgID, err)
	}
	return row.ID
}

// readHub 用独立零值 struct 回读（复用已填充的 dest 会把旧字段并进 WHERE）。
//
// 字段名必须与列名同驼峰（push_attempts ↔ PushAttempts）：gorm 的 Scan 按列名映射，
// 起个更短的名字（Attempts）不会报错、只会静默留 0 —— 于是「实现是对的」也会被读成
// 「计数没落到行上」，一条永久假红。
type pushRow struct {
	Status       string
	PushAttempts int
	PushError    string
}

func readPushRow(t *testing.T, db *gorm.DB, msgID string) pushRow {
	t.Helper()
	var got pushRow
	err := db.Model(&model.MessageHub{}).
		Select("status, push_attempts, push_error").
		Where("platform = ? AND msg_id = ?", b20dPlatform, msgID).
		Scan(&got).Error
	if err != nil {
		t.Fatalf("回读 %s 失败: %v", msgID, err)
	}
	return got
}

func newB20dRepo(t *testing.T, db *gorm.DB) *MessageHubRepository {
	t.Helper()
	if err := db.Exec("DELETE FROM message_hub WHERE platform = ?", b20dPlatform).Error; err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	return &MessageHubRepository{db: db}
}

// 1+2+3 轮询认领路径：每轮计一次，到界后不再给出，并落 failed+原因。
func TestClaimPendingOutboundCapsPushAttemptsAndTerminalizes(t *testing.T) {
	db := testutil.NewTestDB(t, &model.MessageHub{})
	repo := newB20dRepo(t, db)
	ctx := context.Background()
	seedOutbound(t, db, "m-cap", "c1", "pending", 0)

	claimTimeout := time.Second
	for round := 1; round <= MaxOutboundPushAttempts; round++ {
		// 上一轮的认领「超时」：桥端拿走了却没 ack（发不出去/进程被杀/SW 被回收）
		if round > 1 {
			if err := db.Exec(`UPDATE message_hub SET claimed_at = now() - interval '1 hour'
				WHERE platform = ? AND msg_id = 'm-cap'`, b20dPlatform).Error; err != nil {
				t.Fatalf("推旧 claimed_at 失败: %v", err)
			}
		}
		rows, err := repo.ClaimPendingOutbound(ctx, b20dPlatform, "acc1", 10, claimTimeout)
		if err != nil {
			t.Fatalf("第 %d 轮认领失败: %v", round, err)
		}
		if len(rows) != 1 {
			t.Fatalf("第 %d 轮认领到 %d 行 want 1（界内必须照常重投，否则这就是丢消息不是收口）", round, len(rows))
		}
		if got := readPushRow(t, db, "m-cap").PushAttempts; got != round {
			t.Fatalf("第 %d 轮后 push_attempts=%d want %d：计数没留在行上，上界就没有依据", round, got, round)
		}
	}

	// 到界之后：这一行必须既不再被给出，也不是「悄悄消失」——它得在库里带着结论。
	if err := db.Exec(`UPDATE message_hub SET claimed_at = now() - interval '1 hour'
		WHERE platform = ? AND msg_id = 'm-cap'`, b20dPlatform).Error; err != nil {
		t.Fatal(err)
	}
	rows, err := repo.ClaimPendingOutbound(ctx, b20dPlatform, "acc1", 10, claimTimeout)
	if err != nil {
		t.Fatalf("到界后认领不应报错: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("到界后仍给出 %d 行（第 %d 次重推）：上界没关住", len(rows), MaxOutboundPushAttempts+1)
	}
	got := readPushRow(t, db, "m-cap")
	if got.Status != model.BridgeAckStatusFailed {
		t.Fatalf("到界后 status=%q want %q——不是新造一个状态，也不是把它留在 owed 集合里等人手工处理",
			got.Status, model.BridgeAckStatusFailed)
	}
	if got.PushAttempts != MaxOutboundPushAttempts {
		t.Fatalf("push_attempts=%d want %d（终态后不得继续计数）", got.PushAttempts, MaxOutboundPushAttempts)
	}
	if got.PushError == "" {
		t.Fatal("push_error 为空：行落到了终态却没说为什么，读库的人仍要自己猜（per-row 可观测＝这一列）")
	}

	// 再跑若干轮也不能复活它（终态不是「暂停一会儿」）
	for i := 0; i < 3; i++ {
		if r, err := repo.ClaimPendingOutbound(ctx, b20dPlatform, "acc1", 10, claimTimeout); err != nil || len(r) != 0 {
			t.Fatalf("终态后被复活：rows=%d err=%v", len(r), err)
		}
	}
}

// 4 超时回收不许重置预算：inflight→pending 只是「这次没送成」，不是「没送过」。
func TestInflightRecycleKeepsPushAttemptBudget(t *testing.T) {
	db := testutil.NewTestDB(t, &model.MessageHub{})
	repo := newB20dRepo(t, db)
	ctx := context.Background()
	seedOutbound(t, db, "m-budget", "c1", "inflight", time.Hour)
	if err := db.Exec(`UPDATE message_hub SET push_attempts = 5 WHERE platform = ? AND msg_id = 'm-budget'`,
		b20dPlatform).Error; err != nil {
		t.Fatalf("预置计数失败（列不存在则本批未落地）: %v", err)
	}
	rows, err := repo.ClaimPendingOutbound(ctx, b20dPlatform, "acc1", 10, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("回收后应能重新认领，got %d 行", len(rows))
	}
	if got := readPushRow(t, db, "m-budget").PushAttempts; got != 6 {
		t.Fatalf("push_attempts=%d want 6：回收把已花掉的 %d 次预算抹了回去（等于永远到不了上界）", got, 5)
	}
}

// 2 SSE 即时推送认领同口径：到界的行不给推（否则只有轮询被收口），界内推一次计一次。
func TestClaimOutboundForPushHonorsCap(t *testing.T) {
	db := testutil.NewTestDB(t, &model.MessageHub{})
	repo := newB20dRepo(t, db)
	ctx := context.Background()
	okID := seedOutbound(t, db, "m-sse", "c1", "pending", 0)
	cappedID := seedOutbound(t, db, "m-sse-capped", "c2", "pending", 0)
	if err := db.Exec(`UPDATE message_hub SET push_attempts = ? WHERE id = ?`,
		MaxOutboundPushAttempts, cappedID).Error; err != nil {
		t.Fatal(err)
	}

	got, err := repo.ClaimOutboundForPush(ctx, uint64(okID), time.Second)
	if err != nil || !got {
		t.Fatalf("界内行应认领成功，got=%v err=%v", got, err)
	}
	if a := readPushRow(t, db, "m-sse").PushAttempts; a != 1 {
		t.Errorf("SSE 认领未计数：push_attempts=%d want 1", a)
	}
	got, err = repo.ClaimOutboundForPush(ctx, uint64(cappedID), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Fatalf("到界行仍被 SSE 即时推送认领：三条取行路径只给一条加界＝换条路径照样永推")
	}
}

// 2+3 SSE 补拉列表同口径：到界的行离开 owed 集合，并被就地升级成终态（只走 SSE 的账号
// 不会每 1.5s 轮询一次，若只有轮询做升级，这些行会永远停在 inflight/pending）。
func TestFetchOutboundUndeliveredExcludesAndEscalatesExhausted(t *testing.T) {
	db := testutil.NewTestDB(t, &model.MessageHub{})
	repo := newB20dRepo(t, db)
	ctx := context.Background()
	seedOutbound(t, db, "m-owed", "c1", "pending", 0)
	seedOutbound(t, db, "m-exhausted", "c2", "inflight", time.Hour)
	if err := db.Exec(`UPDATE message_hub SET push_attempts = ? WHERE platform = ? AND msg_id = 'm-exhausted'`,
		MaxOutboundPushAttempts, b20dPlatform).Error; err != nil {
		t.Fatal(err)
	}
	rows, err := repo.FetchOutboundUndelivered(ctx, b20dPlatform, "acc1", time.Second, 50)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, r := range rows {
		seen[r.MsgID] = true
	}
	if seen["m-exhausted"] {
		t.Fatal("补拉列表仍列出到界行（SSE 每 30s 就再推它一次，永不停）")
	}
	if !seen["m-owed"] {
		t.Fatalf("补拉列表把界内行也丢了：rows=%v", seen)
	}
	if s := readPushRow(t, db, "m-exhausted"); s.Status != model.BridgeAckStatusFailed {
		t.Fatalf("只走 SSE 的账号里到界行 status=%q want failed——升级动作只挂在轮询路径上，纯 SSE 部署永远等不到那一步", s.Status)
	}
}

// 6 收口只碰出站欠交付行：入站行、已交付/已失败的出站行一律不动。
func TestPushCapSweepTouchesOnlyOwedOutbound(t *testing.T) {
	db := testutil.NewTestDB(t, &model.MessageHub{})
	repo := newB20dRepo(t, db)
	ctx := context.Background()

	in := &model.MessageHub{
		Platform: b20dPlatform, AccountID: "acc1", ConversationID: "c1", MsgID: "m-in",
		MsgType: "text", Content: "in", Direction: "inbound", Status: "pending", PushAttempts: MaxOutboundPushAttempts,
	}
	if err := db.Create(in).Error; err != nil {
		t.Fatal(err)
	}
	seedOutbound(t, db, "m-done", "c2", model.BridgeAckStatusDelivered, 0)
	if err := db.Exec(`UPDATE message_hub SET push_attempts = ? WHERE platform = ? AND msg_id = 'm-done'`,
		MaxOutboundPushAttempts, b20dPlatform).Error; err != nil {
		t.Fatal(err)
	}

	if _, err := repo.ClaimPendingOutbound(ctx, b20dPlatform, "acc1", 10, time.Second); err != nil {
		t.Fatal(err)
	}
	if s := readPushRow(t, db, "m-in"); s.Status != "pending" {
		t.Fatalf("入站行被出站收口改动：status=%q（收口的 WHERE 漏了 direction='outbound'）", s.Status)
	}
	if s := readPushRow(t, db, "m-done"); s.Status != model.BridgeAckStatusDelivered {
		t.Fatalf("已交付行被改判成 %q：终态行不得被收口重写（审计事实被覆盖）", s.Status)
	}
	// 到界行落终态时**不许**盖 sent_at：它从未交付出去，而回显检测（ListRecentOutboundInConv /
	// GetOutboundByPlatformSenderContent*）正是按 sent_at 筛候选的。给一条页面上从未出现过的
	// 内容盖上时间戳，等于让后续回复被误判成「自己发过的回显」而整段吞掉。
	seedOutbound(t, db, "m-never", "c9", "pending", 0)
	if err := db.Exec(`UPDATE message_hub SET push_attempts = ? WHERE platform = ? AND msg_id = 'm-never'`,
		MaxOutboundPushAttempts, b20dPlatform).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ClaimPendingOutbound(ctx, b20dPlatform, "acc1", 10, time.Second); err != nil {
		t.Fatal(err)
	}
	var stamped model.MessageHub
	if err := db.Select("status, sent_at").First(&stamped,
		"platform = ? AND msg_id = ?", b20dPlatform, "m-never").Error; err != nil {
		t.Fatalf("回读 m-never 失败: %v", err)
	}
	if stamped.Status != model.BridgeAckStatusFailed {
		t.Fatalf("到界 pending 行 status=%q want failed", stamped.Status)
	}
	if !stamped.SentAt.IsZero() {
		t.Errorf("收口给从未交付的行盖了 sent_at=%v（回显检测会把它当自己发过的内容）", stamped.SentAt)
	}
}

// 7 收口的时间界必须是**真条件**：到界、但此刻桥端仍持有（claimed_at 未超时）的行不得翻终态。
//
// 杀掉的变异：`exhaustOutbound` 的最后一参由 `cutoff` 换成 `time.Now()`
// （message_hub_inbox_outbound.go:71）。那一刀把谓词从「inflight 且认领已超时」退化成
// 「任何 inflight 行」——`claimed_at < now()` 对任何已认领的行恒真，于是正在发的那一条被就地判死。
// 前面的腿为什么拦不住它：b20d 那几条推旧 claimed_at 的行 push_attempts 都还没到界（被
// `push_attempts >= ?` 先排除在收口之外），而 b11 里唯一两条「未超时 inflight」腿
// （FreshInflightIsExclusive / StatusNotCursor 的 holding 格）同样是 push_attempts=0。
// 这一格把两个条件第一次凑齐：到界 + 未超时 + inflight。
//
// 断两件事，因为这一刀的后果不止「状态难看」：
//   - 行必须还是 inflight（收口不许抢走桥端正在持有的那一行）；
//   - 它随后的 ack 必须仍然命中——翻转条件写的是 status IN ('pending','inflight')，
//     被提前判成 failed 之后那句「客户已经收到的话」在库里永远结不了账（不可撤回的错账）。
func TestPushCapSweepSparesUnexpiredInflightRow(t *testing.T) {
	db := testutil.NewTestDB(t, &model.MessageHub{})
	repo := newB20dRepo(t, db)
	ctx := context.Background()

	liveID := seedOutbound(t, db, "m-live-capped", "c1", "inflight", 0)
	// 到界 + 刚刚认领（远未超时）：三条取行路径都会先跑一次收口，这里走「只读不认领」那条，
	// 免得回收/认领两步替收口打掩护。claimTimeout 取 1h ⇒ cutoff=now-1h ⇒ 这一行明确在窗内。
	if err := db.Exec(`UPDATE message_hub SET push_attempts = ?, claimed_at = now() WHERE id = ?`,
		MaxOutboundPushAttempts, liveID).Error; err != nil {
		t.Fatal(err)
	}

	if _, err := repo.FetchOutboundUndelivered(ctx, b20dPlatform, "acc1", time.Hour, 50); err != nil {
		t.Fatal(err)
	}
	got := readPushRow(t, db, "m-live-capped")
	if got.Status != "inflight" {
		t.Fatalf("正被桥端持有的到界行 status=%q want inflight：push_attempts 到界只说明「没几次可推了」，"+
			"不说明「这一次没在发」——收口少了「认领已超时」那一半条件", got.Status)
	}
	if got.PushError != "" {
		t.Fatalf("未超时的在途行被写了收口原因 push_error=%q", got.PushError)
	}

	ids, n, err := repo.AckOutboundDeliveredBatchReturning(ctx, b20dPlatform, "acc1", []string{"m-live-capped"})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(ids) != 1 {
		t.Fatalf("在途行随后的 ack 命中 %d 条 want 1（行被提前判成 failed 后，ack 的 "+
			"status IN ('pending','inflight') 再也对不上它：消息真发出去了，库里却是 failed）", n)
	}
}

// 7b 与 7 同一判据，但走**轮询认领**那条收口调用点（ClaimPendingOutbound:84）。
//
// 为什么要单独立一条：O1 的刀（收口实参 cutoff→time.Now()）有两处调用点，各自单独一刀。
// C 相 lane 5 电池 run1 实测（logs/R22-lanes/mut_outbound_claim_r22lane-run1.log）：
// 补拉点注刀 ⇒ 上一条腿红（杀掉），轮询点注刀 ⇒ 全电池 6 格无一红（存活=洞）——
// 轮询那处此刻没有任何腿压着。缺口成立：正在被桥端发送的到界行会在下一轮轮询进场时
// 被收口就地判死，随后的 ack 打空（客户收到了、库里说失败），与 7 说的是同一件事故，
// 只是换了一条路径发生。
//
// 夹具与回收/认领两步划清界限：claimed_at 取 now() ⇒ 回收（claimed_at < cutoff=now-1h）
// 不碰它；另备一行 pending 给认领那一步消耗，证明收口确实抢跑在「行仍是 inflight」之前。
// 断两件事（同 7）：行必须仍是 inflight；它随后的 ack 必须仍然命中。
func TestPushCapPollSweepSparesUnexpiredInflightRow(t *testing.T) {
	db := testutil.NewTestDB(t, &model.MessageHub{})
	repo := newB20dRepo(t, db)
	ctx := context.Background()

	liveID := seedOutbound(t, db, "m-poll-live", "c1", "inflight", 0)
	// 到界 + 刚刚认领（claimTimeout 取 1h ⇒ 远未超时）：轮询收口的 cutoff 应为 now-1h，
	// 这一行的 claimed_at 在它之后 ⇒ 不该被收口碰。
	if err := db.Exec(`UPDATE message_hub SET push_attempts = ?, claimed_at = now() WHERE id = ?`,
		MaxOutboundPushAttempts, liveID).Error; err != nil {
		t.Fatal(err)
	}
	seedOutbound(t, db, "m-poll-pending", "c2", "pending", 0) // 让认领那一步有事可做，别把刀藏进空转

	if rows, err := repo.ClaimPendingOutbound(ctx, b20dPlatform, "acc1", 10, time.Hour); err != nil {
		t.Fatal(err)
	} else if len(rows) != 1 || rows[0].MsgID != "m-poll-pending" {
		t.Fatalf("轮询认领取到 %v want 仅 m-poll-pending（在途行不该被本轮取走，回收窗也还没到）",
			msgIDsOf(rows))
	}
	got := readPushRow(t, db, "m-poll-live")
	if got.Status != "inflight" {
		t.Fatalf("正被桥端持有的到界行 status=%q want inflight：轮询路径的收口把「inflight 且认领已超时」"+
			"放宽成了「任何 inflight」（实参 cutoff 被换成 time.Now() 时红在这一行）", got.Status)
	}
	if got.PushError != "" {
		t.Fatalf("未超时的在途行被写了收口原因 push_error=%q", got.PushError)
	}

	ids, n, err := repo.AckOutboundDeliveredBatchReturning(ctx, b20dPlatform, "acc1", []string{"m-poll-live"})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(ids) != 1 {
		t.Fatalf("轮询收口若在途处抢跑，ack 的 status IN ('pending','inflight') 就打空：%d 条 want 1", n)
	}
}

func msgIDsOf(rows []model.MessageHub) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.MsgID)
	}
	return out
}
