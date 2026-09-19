package repository

// 批11 §3.7-2 契约锁：出站行的两条投递路径（SSE 即时下推 / 长轮询取件）必须共用同一把
// 服务端权威认领门，且「欠交付」集合按状态而非 id 游标判定。
//
// 断的都是本批修的实际后果：
//   - 不认领 → 同一条 pending 行被两条路径各取一次 → 同一句话打给真实客户两遍（不可逆写）
//   - 按游标取 → 推送后发送失败的行落在游标之下 → 永不再投 → 客户静默收不到回复

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

const b11ClaimTimeout = 30 * time.Second

// seedB11Hub 建一条指定状态/认领时间的出站行，返回其 ID。
// 复用同包 newHub 造形状，再按用例需要覆写 claimed_at（newHub 不带该列）。
func seedB11Hub(t *testing.T, db *gorm.DB, platform, accountID, msgID, status string, claimedAt *time.Time, direction string) uint64 {
	t.Helper()
	h := newHub(platform, accountID, msgID, direction, "text", "批11内容_"+msgID, status)
	h.ConversationID = "conv_" + msgID
	h.ClaimedAt = claimedAt
	if err := db.Create(h).Error; err != nil {
		t.Fatalf("seed %s: %v", msgID, err)
	}
	return uint64(h.ID)
}

func TestClaimOutboundForPush_ClaimsPending(t *testing.T) {
	db := setupMessageHubTestDB(t)
	repo := &MessageHubRepository{db: db}
	ctx := context.Background()

	id := seedB11Hub(t, db, "douyin", "b11_push", "mh_pending", "pending", nil, "outbound")
	ok, err := repo.ClaimOutboundForPush(ctx, id, b11ClaimTimeout)
	if err != nil || !ok {
		t.Fatalf("pending 行应被认领: ok=%v err=%v", ok, err)
	}

	var got model.MessageHub
	if err := db.First(&got, id).Error; err != nil {
		t.Fatal(err)
	}
	if got.Status != "inflight" {
		t.Errorf("认领后 status=%s，应为 inflight（轮询侧从此拿不到它）", got.Status)
	}
	if got.ClaimedAt == nil {
		t.Error("认领后 claimed_at 为空 → 可见性超时回收无从判起，这条行会永不再投")
	}
}

func TestClaimOutboundForPush_FreshInflightIsExclusive(t *testing.T) {
	db := setupMessageHubTestDB(t)
	repo := &MessageHubRepository{db: db}
	ctx := context.Background()

	now := time.Now()
	id := seedB11Hub(t, db, "douyin", "b11_excl", "mh_fresh_inflight", "inflight", &now, "outbound")
	ok, err := repo.ClaimOutboundForPush(ctx, id, b11ClaimTimeout)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("未超时的 inflight 行被再次认领 → 两条路径可同时持有同一行 → 双投")
	}

	var got model.MessageHub
	if err := db.First(&got, id).Error; err != nil {
		t.Fatal(err)
	}
	if got.ClaimedAt != nil && got.ClaimedAt.Sub(now) > time.Second {
		t.Errorf("抢锁失败还改写了 claimed_at（把别人的超时窗往后推了）: %v vs %v", got.ClaimedAt, now)
	}
}

func TestClaimOutboundForPush_StaleInflightIsRedelivered(t *testing.T) {
	db := setupMessageHubTestDB(t)
	repo := &MessageHubRepository{db: db}
	ctx := context.Background()

	stale := time.Now().Add(-3 * b11ClaimTimeout)
	id := seedB11Hub(t, db, "douyin", "b11_re", "mh_stale_inflight", "inflight", &stale, "outbound")
	ok, err := repo.ClaimOutboundForPush(ctx, id, b11ClaimTimeout)
	if err != nil || !ok {
		t.Fatalf("超时的 inflight（上次推送后没 ack）必须可重投: ok=%v err=%v", ok, err)
	}
	var got model.MessageHub
	if err := db.First(&got, id).Error; err != nil {
		t.Fatal(err)
	}
	if got.ClaimedAt == nil || !got.ClaimedAt.After(stale) {
		t.Errorf("重投应刷新 claimed_at 重新计时，实际 %v（原 %v）", got.ClaimedAt, stale)
	}
}

func TestClaimOutboundForPush_RefusesTerminalInboundAndZero(t *testing.T) {
	db := setupMessageHubTestDB(t)
	repo := &MessageHubRepository{db: db}
	ctx := context.Background()

	for _, tc := range []struct{ name, status, direction string }{
		{"delivered 不再推", "delivered", "outbound"},
		{"failed 不再推", "failed", "outbound"},
		{"入站行不归出站认领门管", "pending", "inbound"},
	} {
		id := seedB11Hub(t, db, "douyin", "b11_refuse_"+tc.status, "mh_refuse_"+tc.status, tc.status, nil, tc.direction)
		ok, err := repo.ClaimOutboundForPush(ctx, id, b11ClaimTimeout)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if ok {
			t.Errorf("%s：认领门放行了不该放行的行", tc.name)
		}
	}
	if ok, err := repo.ClaimOutboundForPush(ctx, 0, b11ClaimTimeout); ok || err != nil {
		t.Errorf("id=0 应直接不认领且不报错: ok=%v err=%v", ok, err)
	}
	var nilRepo *MessageHubRepository
	if ok, err := nilRepo.ClaimOutboundForPush(ctx, 1, b11ClaimTimeout); ok || err != nil {
		t.Errorf("nil repo 应回 (false,nil): ok=%v err=%v", ok, err)
	}
}

func TestFetchOutboundUndelivered_StatusNotCursor(t *testing.T) {
	db := setupMessageHubTestDB(t)
	repo := &MessageHubRepository{db: db}
	ctx := context.Background()
	now := time.Now()
	stale := now.Add(-3 * b11ClaimTimeout)

	const acct = "b11_owed"
	owed := seedB11Hub(t, db, "douyin", acct, "mh_owed_pending", "pending", nil, "outbound")
	retry := seedB11Hub(t, db, "douyin", acct, "mh_owed_stale", "inflight", &stale, "outbound")
	holding := seedB11Hub(t, db, "douyin", acct, "mh_owed_fresh", "inflight", &now, "outbound")
	done := seedB11Hub(t, db, "douyin", acct, "mh_owed_delivered", "delivered", nil, "outbound")
	bad := seedB11Hub(t, db, "douyin", acct, "mh_owed_failed", "failed", nil, "outbound")
	inb := seedB11Hub(t, db, "douyin", acct, "mh_owed_inbound", "pending", nil, "inbound")
	// 同账号另一渠道：不得串台
	other := seedB11Hub(t, db, "xiaohongshu", acct, "mh_owed_other", "pending", nil, "outbound")

	rows, err := repo.FetchOutboundUndelivered(ctx, "douyin", acct, b11ClaimTimeout, 50)
	if err != nil {
		t.Fatal(err)
	}
	got := map[uint64]bool{}
	for _, r := range rows {
		got[uint64(r.ID)] = true
	}
	for id, want := range map[uint64]struct {
		on  bool
		why string
	}{
		owed:    {true, "pending 必须欠交付"},
		retry:   {true, "超时 inflight 必须重投（本批修的就是这条）"},
		holding: {false, "未超时 inflight 正被别人持有，重推即双投"},
		done:    {false, "已交付再推 = 客户收到两遍"},
		bad:     {false, "已判失败由人工路径处置，不自动重投"},
		inb:     {false, "入站行不是出站待办"},
		other:   {false, "跨渠道不得串台"},
	} {
		if got[id] != want.on {
			t.Errorf("行 %d：期望在集合=%v 实际=%v（%s）", id, want.on, got[id], want.why)
		}
	}
}

func TestFetchOutboundUndelivered_OrderLimitAndGuards(t *testing.T) {
	db := setupMessageHubTestDB(t)
	repo := &MessageHubRepository{db: db}
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		seedB11Hub(t, db, "douyin", "b11_lim", time.Now().Format("150405.000000")+string(rune('a'+i)), "pending", nil, "outbound")
	}
	rows, err := repo.FetchOutboundUndelivered(ctx, "douyin", "b11_lim", b11ClaimTimeout, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("limit=2 应截到 2 条，实际 %d", len(rows))
	}
	for i := 1; i < len(rows); i++ {
		if rows[i].ID <= rows[i-1].ID {
			t.Errorf("必须 id ASC（先发的先补，乱序会打乱会话上下文）: %d 后于 %d", rows[i].ID, rows[i-1].ID)
		}
	}
	// limit<=0 与超限都要落回 200 而不是「无限制」或「零条」
	anyRows, err := repo.FetchOutboundUndelivered(ctx, "douyin", "b11_lim", b11ClaimTimeout, 0)
	if err != nil || len(anyRows) != 5 {
		t.Errorf("limit=0 应回退默认上限而非查空: n=%d err=%v", len(anyRows), err)
	}
	if rows, err := repo.FetchOutboundUndelivered(ctx, "", "b11_lim", b11ClaimTimeout, 10); err != nil || rows != nil {
		t.Errorf("空渠道应 (nil,nil) 不查全库: n=%d err=%v", len(rows), err)
	}
	var nilRepo *MessageHubRepository
	if rows, err := nilRepo.FetchOutboundUndelivered(ctx, "douyin", "a", b11ClaimTimeout, 10); err != nil || rows != nil {
		t.Errorf("nil repo 应 (nil,nil): %v %v", rows, err)
	}
}

// TestClaimOutboundForPush_ConcurrentSingleWinner 是本批的立论本身：
// 同一行并发两个认领（模拟 SSE 下推与轮询取件同时发生），只能有一个赢。
// 赢的那个才有权把这句话打给客户。
func TestClaimOutboundForPush_ConcurrentSingleWinner(t *testing.T) {
	db := setupMessageHubTestDB(t)
	repo := &MessageHubRepository{db: db}
	ctx := context.Background()

	const round = 8
	for i := 0; i < round; i++ {
		id := seedB11Hub(t, db, "douyin", "b11_race", time.Now().Format("150405.000000000")+string(rune('a'+i)), "pending", nil, "outbound")
		winner := make(chan bool, 2)
		for k := 0; k < 2; k++ {
			go func() {
				ok, err := repo.ClaimOutboundForPush(ctx, id, b11ClaimTimeout)
				if err != nil {
					t.Errorf("认领报错: %v", err)
				}
				winner <- ok
			}()
		}
		n := 0
		for k := 0; k < 2; k++ {
			if <-winner {
				n++
			}
		}
		if n != 1 {
			t.Fatalf("第 %d 轮并发认领赢家数=%d，必须是 1（0=消息发不出去，2=客户收到两遍）", i+1, n)
		}
	}
}
