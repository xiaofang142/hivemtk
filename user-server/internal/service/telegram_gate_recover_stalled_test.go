package service

// 入群验证提示「重复播报」回归用例（TG 群管控补偿循环）。
//
// 线上实测：6 名未验证成员在十几个小时里被同一段入群提示刷屏 219 条。根因是补偿
// 循环只有 expires_at 一个信号，"临近到期"被当成"提示没送达"，于是每个 TTL 就给
// 同一个人重播一次，重播还顺手把 expires_at 顶回去。
//
// 修复后送达状态记在 welcome_sent_at / welcome_resends 上，于是：
//   - 已送达的成员根本不进补偿集合（ListStalledRestricted 只取未送达的行）：一次
//     TG 调用都不发，计时也不再被顶回去——顶回去＝TTL 永不成熟，那是"重播 + 永不
//     处置"这一对症状的另一半；
//   - 从未送达的成员最多补发 tgGateWelcomeResendMax 次，到上限后停手让 TTL 走到底，
//     到期由 SweepExpired 按"没被告知不处置"放过；
//   - 重新入群是一段新的成员关系，送达状态由 Upsert 覆盖回未送达（否则再进群的人
//     永远收不到提示）。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/channelbot/telegram"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

const r65ChatID = "-10099887766"

// r65GateStub 按方法名选择性失败的 Bot API 假服务端。
//
// 既有的 tgAPIStub 只有一个全局状态码，"禁言失败"与"发送失败"两条支路分不开；
// 合成线上那 219 条重复播报的正是这两条各自计数的路径，所以这里按方法名单独放行/拦停。
type r65GateStub struct {
	mu          sync.Mutex
	calls       []string
	failMethods map[string]bool
}

func (s *r65GateStub) handle(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	method := parts[len(parts)-1]
	s.mu.Lock()
	fail := s.failMethods[method]
	s.calls = append(s.calls, method)
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	if fail {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"ok":false,"error_code":400,"description":"Bad Request: chat not found"}`))
		return
	}
	_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
}

func (s *r65GateStub) count(method string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, c := range s.calls {
		if c == method {
			n++
		}
	}
	return n
}

func (s *r65GateStub) total() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

func (s *r65GateStub) reset() {
	s.mu.Lock()
	s.calls = nil
	s.mu.Unlock()
}

// r65PurgeGateRows 清掉本族用例专用群（r65ChatID）留下的台账。
//
// 同一个测试进程里的库是共用的：上一格用例留下的"已到期"成员会被这一格的清扫器一起
// 扫到，于是"本格零处置"量的是别人的数据，最坏还会拿别人账号的假 token 去打真 Bot API。
// 只清本族专用的那个 chat，别人用例的夹具一律不碰。
func r65PurgeGateRows(db *gorm.DB) error {
	return db.Where("chat_id = ?", r65ChatID).Delete(&model.TelegramGroupMember{}).Error
}

// r65SetupGate 起一个启用的 mute_unlock 门控群 + Bot 账号，API 基址指向 stub。
func r65SetupGate(t *testing.T, stub *r65GateStub) (*TelegramGateService, *gorm.DB, uint) {
	t.Helper()
	db := testutil.NewTestDB(t,
		&model.TelegramGroupGate{},
		&model.TelegramGroupMember{},
		&model.TelegramAccount{},
	)
	if err := r65PurgeGateRows(db); err != nil {
		t.Fatalf("清理门控台账: %v", err)
	}
	t.Cleanup(func() {
		if err := r65PurgeGateRows(db); err != nil {
			t.Errorf("用例结束后未清掉门控台账（下一格的清扫器会把它当自己的数据）: %v", err)
		}
	})
	acc := &model.TelegramAccount{AccountName: "门控号", BotToken: "1:fake", BotUsername: "gate_test_bot", Status: 1}
	if err := db.Create(acc).Error; err != nil {
		t.Fatalf("种 TG 账号: %v", err)
	}
	gate := &model.TelegramGroupGate{
		AccountID:    acc.ID,
		ChatID:       r65ChatID,
		Mode:         TGGateModeMuteUnlock,
		Enabled:      true,
		VerifyTTLMin: 10,
	}
	if err := db.Create(gate).Error; err != nil {
		t.Fatalf("种门控配置: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(stub.handle))
	t.Cleanup(srv.Close)
	svc := NewTelegramGateService(db)
	svc.SetAPIBase(srv.URL)
	return svc, db, acc.ID
}

func r65SeedMember(t *testing.T, db *gorm.DB, accID uint, userID string, sentAt *time.Time, resends int, expiresAt time.Time) {
	t.Helper()
	exp := expiresAt
	m := &model.TelegramGroupMember{
		AccountID:      accID,
		ChatID:         r65ChatID,
		UserID:         userID,
		FullName:       "成员" + userID,
		JoinStatus:     model.TGMemberRestricted,
		JoinMode:       TGGateModeMuteUnlock,
		VerifyToken:    "tok_" + userID,
		WelcomeSentAt:  sentAt,
		WelcomeResends: resends,
		ExpiresAt:      &exp,
	}
	if err := db.Create(m).Error; err != nil {
		t.Fatalf("种台账 %s: %v", userID, err)
	}
}

// r65ReadMember 读回台账。每次用独立零值 struct：复用已填充的 dest 会把旧字段并进 WHERE。
func r65ReadMember(t *testing.T, db *gorm.DB, accID uint, userID string) model.TelegramGroupMember {
	t.Helper()
	var m model.TelegramGroupMember
	if err := db.Where("account_id = ? AND chat_id = ? AND user_id = ?", accID, r65ChatID, userID).
		First(&m).Error; err != nil {
		t.Fatalf("读回台账 %s: %v", userID, err)
	}
	return m
}

// r65AssertNotRetimed 断言 expires_at 仍是当初种进去的那一值。
//
// 用等值而不是"After(now+9min)"：新口径下已送达的人不进补偿集合，计时必须**一动不动**；
// 只断言"没超过 10 分钟"会放过"续到 3 分钟"这类半吊子实现。PG 的时间列只到微秒，比前先对齐。
func r65AssertNotRetimed(t *testing.T, m model.TelegramGroupMember, want time.Time) {
	t.Helper()
	if m.ExpiresAt == nil || !m.ExpiresAt.Equal(want.Truncate(time.Microsecond)) {
		t.Fatalf("expires_at 不该被动过, 期望仍为 %v, 实际=%v", want, m.ExpiresAt)
	}
}

// 已送达的人不进补偿集合：一轮清扫连一次 TG 调用都不发（这就是 219 条的来源），
// 计时也不被他续——续期 once 是"重播"的另一半：TTL 永远不成熟，清扫器见不到他。
func TestRecoverStalledNeverRebroadcastsDeliveredWelcome(t *testing.T) {
	stub := &r65GateStub{}
	svc, db, accID := r65SetupGate(t, stub)
	ctx := context.Background()

	deliveredAt := time.Now().Add(-9 * time.Minute).Truncate(time.Microsecond) // PG 只存微秒精度，等值比对前要对齐
	exp := time.Now().Add(1 * time.Minute)
	r65SeedMember(t, db, accID, "1001", &deliveredAt, 0, exp)

	svc.RecoverStalled(ctx, 50)

	if stub.total() != 0 {
		t.Fatalf("已送达成员的一轮清扫不该发任何 TG 请求, 实际=%v", stub.calls)
	}
	got := r65ReadMember(t, db, accID, "1001")
	r65AssertNotRetimed(t, got, exp)
	if got.WelcomeSentAt == nil || !got.WelcomeSentAt.Equal(deliveredAt) {
		t.Fatalf("送达时间不该被改写, 实际=%v", got.WelcomeSentAt)
	}

	// 再扫两轮（模拟 TTL 反复走完）：播报次数仍是 0。
	stub.reset()
	svc.RecoverStalled(ctx, 50)
	svc.RecoverStalled(ctx, 50)
	if stub.total() != 0 {
		t.Fatalf("重复清扫仍不该产生 TG 请求, 实际=%v", stub.calls)
	}
}

// 只有"从未送达"的人才补发，且只发一次；到上限的人和已送达的人都不发。
func TestRecoverStalledResendsOnlyWhenNeverDelivered(t *testing.T) {
	stub := &r65GateStub{}
	svc, db, accID := r65SetupGate(t, stub)
	ctx := context.Background()

	soon := time.Now().Add(1 * time.Minute)
	delivered := time.Now().Add(-9 * time.Minute)
	r65SeedMember(t, db, accID, "2001", nil, 0, soon)                      // 从未送达 → 补发 1 条
	r65SeedMember(t, db, accID, "2002", &delivered, 0, soon)               // 已送达 → 不发
	r65SeedMember(t, db, accID, "2003", nil, tgGateWelcomeResendMax, soon) // 到上限 → 不发
	// 2001 代表"请求内路径已经放弃的那一行"：种出来顶着 updated_at=now，语义是"刚刚有人
	// 在这行上动笔"，正是补偿循环该让路的状态（见 telegram_gate_in_flight_test.go）。
	// 这里要测的是"从未送达的人该被补上"，所以把写入时刻挪过安静窗口，让它落回本格的轴。
	gateMakeRowIdle(t, db, "2001", tgGateInFlightQuiet+time.Minute)

	svc.RecoverStalled(ctx, 50)

	if n := stub.count("sendMessage"); n != 1 {
		t.Fatalf("一轮清扫应只补发 1 条提示（给 2001）, 实际=%d, 全部调用=%v", n, stub.calls)
	}
	fresh := r65ReadMember(t, db, accID, "2001")
	if fresh.WelcomeSentAt == nil {
		t.Fatal("补发成功后应登记送达时间, 实际仍为 nil")
	}
	if fresh.WelcomeResends != 0 {
		t.Fatalf("送达后补发计数应清零, 实际=%d", fresh.WelcomeResends)
	}
	if !fresh.ExpiresAt.After(time.Now().Add(9 * time.Minute)) {
		t.Fatalf("补发成功应重新计时, 实际 expires_at=%v", fresh.ExpiresAt)
	}
	for _, id := range []string{"2002", "2003"} {
		r65AssertNotRetimed(t, r65ReadMember(t, db, accID, id), soon)
	}

	// 下一个周期：2001 已送达，不该再被播报。
	stub.reset()
	svc.RecoverStalled(ctx, 50)
	if n := stub.count("sendMessage"); n != 0 {
		t.Fatalf("已登记送达的成员下一轮不该再播, 实际=%d", n)
	}
}

// 发不出去时要计数（否则每分钟都会重试、且永远到不了上限），且计数满即停。
func TestRecoverStalledCountsFailedAttemptsAndStopsAtCap(t *testing.T) {
	cases := []struct {
		name       string
		failMethod string
		startCount int
	}{
		{name: "禁言失败", failMethod: "restrictChatMember", startCount: 0},
		{name: "提示发送失败", failMethod: "sendMessage", startCount: 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stub := &r65GateStub{failMethods: map[string]bool{c.failMethod: true}}
			svc, db, accID := r65SetupGate(t, stub)
			ctx := context.Background()
			userID := "3001"
			exp := time.Now().Add(1 * time.Minute)
			r65SeedMember(t, db, accID, userID, nil, c.startCount, exp)
			// 本格测的是"补发出去了但没送达 ⇒ 该计数、到上限就该停手"，前提是这一行已经
			// 归补偿循环管：刚种出来的行顶着 updated_at=now，属"请求内路径还握着"的状态，
			// 补偿循环该让路（那一格由 in_flight 用例专门守）。
			gateMakeRowIdle(t, db, userID, tgGateInFlightQuiet+time.Minute)

			svc.RecoverStalled(ctx, 50)

			got := r65ReadMember(t, db, accID, userID)
			if got.WelcomeResends != c.startCount+1 {
				t.Fatalf("失败一次应计数 +1（起点 %d）, 实际=%d", c.startCount, got.WelcomeResends)
			}
			if got.WelcomeSentAt != nil {
				t.Fatal("发送未成功却登记了送达时间")
			}

			// 计数打到上限后彻底停手：不再有该方法的调用。
			if err := db.Model(&model.TelegramGroupMember{}).
				Where("user_id = ?", userID).
				Update("welcome_resends", tgGateWelcomeResendMax).Error; err != nil {
				t.Fatalf("抬到上限失败: %v", err)
			}
			stub.reset()
			svc.RecoverStalled(ctx, 50)
			if stub.total() != 0 {
				t.Fatalf("补发到上限后不该再打 TG, 实际=%v", stub.calls)
			}
			// 到上限后连计时也不再顶：让他到期，由清扫器按"从未送达不处置"放过。
			// 上一版的"维持计时"＝这个人永远不进过期集合，台账上分不清"被放过"和"被漏掉"。
			r65AssertNotRetimed(t, r65ReadMember(t, db, accID, userID), exp)
		})
	}
}

// 入群时发成功 → 登记送达；发失败 → 计数一次，交给清扫器补发。
func TestHandleNewMembersRecordsDeliveryOutcome(t *testing.T) {
	stub := &r65GateStub{}
	svc, db, accID := r65SetupGate(t, stub)
	ctx := context.Background()
	chatID, _ := strconv.ParseInt(r65ChatID, 10, 64)
	newcomer := telegram.TGUser{ID: 4001, FirstName: "新人"}

	if !svc.HandleNewMembers(ctx, accID, chatID, []telegram.TGUser{newcomer}) {
		t.Fatal("门控群应命中 HandleNewMembers")
	}
	if n := stub.count("sendMessage"); n != 1 {
		t.Fatalf("入群应播报一次提示, 实际=%d, 全部调用=%v", n, stub.calls)
	}
	if r65ReadMember(t, db, accID, "4001").WelcomeSentAt == nil {
		t.Fatal("入群提示已送达却没登记送达时间")
	}

	// 把 TTL 推到"再有 1 分钟就到期"（正是历史上触发重播的那个时刻），清扫器接手后
	// 仍不该有任何追加播报。不推这一步的话新人根本不在清扫窗口里，这条判据是空的。
	if err := db.Model(&model.TelegramGroupMember{}).
		Where("account_id = ? AND chat_id = ? AND user_id = ?", accID, r65ChatID, "4001").
		Update("expires_at", time.Now().Add(time.Minute)).Error; err != nil {
		t.Fatalf("把 TTL 推到临近到期失败: %v", err)
	}
	stub.reset()
	svc.RecoverStalled(ctx, 50)
	if stub.total() != 0 {
		t.Fatalf("已送达的新人不该被补偿循环再播报, 实际=%v", stub.calls)
	}
}

// 重新入群是一段新的成员关系：上一段的送达状态必须被覆盖回未送达，否则他再也收不到提示。
func TestHandleNewMembersReJoinResetsWelcomeState(t *testing.T) {
	stub := &r65GateStub{}
	svc, db, accID := r65SetupGate(t, stub)
	ctx := context.Background()
	chatID, _ := strconv.ParseInt(r65ChatID, 10, 64)
	returnee := telegram.TGUser{ID: 5001, FirstName: "二进宫"}

	if !svc.HandleNewMembers(ctx, accID, chatID, []telegram.TGUser{returnee}) {
		t.Fatal("首次入群应命中门控")
	}
	first := r65ReadMember(t, db, accID, "5001")
	if first.WelcomeSentAt == nil {
		t.Fatal("首次入群应登记送达")
	}

	stub.reset()
	if !svc.HandleNewMembers(ctx, accID, chatID, []telegram.TGUser{returnee}) {
		t.Fatal("再次入群应命中门控")
	}
	if n := stub.count("sendMessage"); n != 1 {
		t.Fatalf("重新入群应重新播报一次（新成员关系）, 实际=%d", n)
	}
	second := r65ReadMember(t, db, accID, "5001")
	if second.ID != first.ID {
		t.Fatalf("同 (account,chat,user) 应复用同一行台账, 实际 %d → %d", first.ID, second.ID)
	}
	if second.WelcomeResends != 0 || second.WelcomeSentAt == nil {
		t.Fatalf("重新入群后送达状态应重置并重记, 实际 sent=%v resends=%d", second.WelcomeSentAt, second.WelcomeResends)
	}

	// 上一段的送达必须真的被抹掉，而不是"这一轮发送成功所以又写回了一个时间"——
	// 只判上面那一格分不出这两种。第三次入群让发送失败：若 Upsert 不覆盖
	// welcome_sent_at，他会顶着上一段的旧送达时间，既进不了补偿队列（只补未送达的行），
	// 又满足清扫器的"送达过"判据——新的一段成员关系里他从没被告知，却可以被移出。
	stub.failMethods = map[string]bool{"sendMessage": true}
	stub.reset()
	if !svc.HandleNewMembers(ctx, accID, chatID, []telegram.TGUser{returnee}) {
		t.Fatal("第三次入群应命中门控")
	}
	third := r65ReadMember(t, db, accID, "5001")
	if third.WelcomeSentAt != nil {
		t.Fatalf("重新入群且本次发送失败时，送达状态应回到未送达，实际还挂着上一段的 %v", third.WelcomeSentAt)
	}
	if third.WelcomeResends != 1 {
		t.Fatalf("本次失败应计一次补发（计数同样不该继承）, 实际=%d", third.WelcomeResends)
	}
}

// 入群时提示发不出去＝没被告知。这一条必须留在"未送达"状态：把失败也登记成送达，
// 等于同时关掉补偿循环的补发（查询只取未送达的行）和清扫器的免罚判据——人被禁了言，
// 却从没收到过那句"请点链接验证"。
func TestHandleNewMembersFailedSendStaysUndelivered(t *testing.T) {
	stub := &r65GateStub{failMethods: map[string]bool{"sendMessage": true}}
	svc, db, accID := r65SetupGate(t, stub)
	ctx := context.Background()
	chatID, _ := strconv.ParseInt(r65ChatID, 10, 64)

	if !svc.HandleNewMembers(ctx, accID, chatID, []telegram.TGUser{{ID: 6001, FirstName: "发不出的"}}) {
		t.Fatal("门控群应命中 HandleNewMembers")
	}
	got := r65ReadMember(t, db, accID, "6001")
	if got.WelcomeSentAt != nil {
		t.Fatalf("发送失败却登记了送达时间（补偿循环就此再不会补发）, 实际=%v", got.WelcomeSentAt)
	}
	if got.WelcomeResends != 1 {
		t.Fatalf("失败一次应计一次补发数, 实际=%d", got.WelcomeResends)
	}

	// 换回可发的通道：补偿循环应把他补上（未送达＋未到上限）。
	stub.failMethods = map[string]bool{}
	stub.reset()
	svc.RecoverStalled(ctx, 50)
	if n := stub.count("sendMessage"); n != 1 {
		t.Fatalf("未送达的人应由补偿循环补发 1 条, 实际=%d, 全部调用=%v", n, stub.calls)
	}
	if r65ReadMember(t, db, accID, "6001").WelcomeSentAt == nil {
		t.Fatal("补发成功后应登记送达")
	}
}
