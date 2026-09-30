package service

// 补偿循环与入群请求内路径抢同一个人 ⇒ 重复禁言 + 重复播报。
//
// 线上形态（群「三人行」，同一次入群三条提示）：台账 10:06:35.497 写入，请求内路径要
// 在这行上跑两次 TG 往返（禁言 + 提示）才收尾，实测这一串花了 8–10 秒；清扫器每分钟
// 一跳，跳进这 8–10 秒里就给同一个人再 restrict 一次（10:06:37 与 10:06:45 两次）再
// 播报一次，并把 welcome_sent_at 标上。
//
// 修法是一条归属/延迟判据（ListStalledRestricted）：这一行自请求内安静窗口以来没被写过、
// 且没留下任何"尝试已收尾"的痕迹时，补偿循环不碰它。于是：
//   - 正被请求内路径握着（刚写台账、resends=0、updated_at 就在刚刚）⇒ 本轮零动作；
//   - 请求内路径真的发失败了（resends>0，那是它在这一行上的最后一笔写入）⇒ 当轮就补，
//     补偿能力没有被这条判据削掉；
//   - 请求内路径根本没走到收尾就断了（进程被 kill、webhook 超时）⇒ 跨过安静窗口就补，
//     是"延后"而不是"年轻就永久跳过"。

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// gateMakeRowIdle 把一行的"最后写入时刻"推到请求内安静窗口之前。
//
// 种出来的行默认顶着 updated_at=now，语义是"刚刚有人在这行上动过笔"。要用它代表
// "请求内路径已经放弃的这一行"，就得把写入时刻真的挪老——挪的是判据读的那一列，
// 不是用例的期望值。UpdateColumn 而非 Update：后者会被 autoUpdateTime 顶回 now。
func gateMakeRowIdle(t *testing.T, db *gorm.DB, userID string, olderThan time.Duration) {
	t.Helper()
	res := db.Model(&model.TelegramGroupMember{}).
		Where("chat_id = ? AND user_id = ?", r65ChatID, userID).
		UpdateColumn("updated_at", time.Now().Add(-olderThan))
	if res.Error != nil {
		t.Fatalf("把 %s 的写入时刻挪老失败: %v", userID, res.Error)
	}
	if res.RowsAffected != 1 {
		t.Fatalf("挪写入时刻应命中 1 行, 实际=%d", res.RowsAffected)
	}
}

// 反向测试①：行刚建、从未送达、窗口开着 ⇒ 一轮补偿不许动它。
//
// 这一格就是线上第 2 条提示的来源：改前补偿循环会把它捞走（补禁言 + 补发 + 登记送达），
// 于是请求内路径随后又发一条 ⇒ 同一个人两条。
func TestRecoverStalledSkipsRowStillInsideInRequestWindow(t *testing.T) {
	stub := &r65GateStub{}
	svc, db, accID := r65SetupGate(t, stub)
	ctx := context.Background()

	exp := time.Now().Add(10 * time.Minute)
	r65SeedMember(t, db, accID, "8001", nil, 0, exp) // 刚 Upsert：sent=NULL、resends=0、updated_at=now

	svc.RecoverStalled(ctx, 50)

	if stub.total() != 0 {
		t.Fatalf("请求内路径还握着这一行时，补偿循环一次 TG 调用都不许发, 实际=%v", stub.calls)
	}
	got := r65ReadMember(t, db, accID, "8001")
	if got.WelcomeSentAt != nil {
		t.Fatalf("补偿没发过却登记了送达（等于把请求内路径那一轮当成已播报）, 实际=%v", got.WelcomeSentAt)
	}
	if got.WelcomeResends != 0 {
		t.Fatalf("补偿没接手就不该计补发次数, 实际=%d", got.WelcomeResends)
	}
	r65AssertNotRetimed(t, got, exp)
}

// 反向测试②：同一行跨过安静窗口（请求内路径已经放弃这一行）⇒ 补偿必须发出去并登记送达。
//
// 这条判据守的是"补偿能力不许被修没"：窗口只是延后，不是永久跳过。
// 先跑一轮"窗口内"再跑一轮"窗口外"，两腿叠在一起改前才是红的——
// 只跑后一腿的话改前也绿（改前连年轻行都补），那条约束就成了空话。
func TestRecoverStalledResendsAfterInRequestWindowPasses(t *testing.T) {
	stub := &r65GateStub{}
	svc, db, accID := r65SetupGate(t, stub)
	ctx := context.Background()

	exp := time.Now().Add(10 * time.Minute)
	r65SeedMember(t, db, accID, "8002", nil, 0, exp)

	svc.RecoverStalled(ctx, 50)
	if stub.total() != 0 {
		t.Fatalf("安静窗口内不该有 TG 调用, 实际=%v", stub.calls)
	}
	if got := r65ReadMember(t, db, accID, "8002"); got.WelcomeSentAt != nil {
		t.Fatalf("安静窗口内不该登记送达, 实际=%v", got.WelcomeSentAt)
	}

	gateMakeRowIdle(t, db, "8002", tgGateInFlightQuiet+time.Minute)
	svc.RecoverStalled(ctx, 50)

	if n := stub.count("sendMessage"); n != 1 {
		t.Fatalf("跨过安静窗口后应补发 1 条, 实际=%d, 全部调用=%v", n, stub.calls)
	}
	if n := stub.count("restrictChatMember"); n != 1 {
		t.Fatalf("补发前应幂等补一次禁言, 实际=%d, 全部调用=%v", n, stub.calls)
	}
	got := r65ReadMember(t, db, accID, "8002")
	if got.WelcomeSentAt == nil {
		t.Fatal("补发成功后应登记送达时间, 实际仍为 nil")
	}
	if got.WelcomeResends != 0 {
		t.Fatalf("送达后补发计数应清零, 实际=%d", got.WelcomeResends)
	}
	if !got.ExpiresAt.After(time.Now().Add(9 * time.Minute)) {
		t.Fatalf("补发成功应把窗口改为送达时刻+TTL, 实际 expires_at=%v", got.ExpiresAt)
	}

	// 第三条腿：登记送达之后再扫一轮，不该再播（否则"延后"变成了"每轮都补"）。
	stub.reset()
	svc.RecoverStalled(ctx, 50)
	if stub.total() != 0 {
		t.Fatalf("已登记送达的行下一轮不该再有 TG 调用, 实际=%v", stub.calls)
	}
}

// 请求内路径自己承认"这次发失败了"（在最后一笔写入里把补发计数 +1）⇒ 补偿当轮就接手，
// 不用等安静窗口跨过。安静窗口只拦"还没人在这行上收过尾"的情形，拦不住交棒。
//
// 这一格同时是"补偿能力没被修没"的最强证据：verify_ttl_min 比安静窗口还短的群里，
// 全靠这一条把没送达的人在本轮就补上。
func TestRecoverStalledResendsImmediatelyAfterRequestPathHandedOff(t *testing.T) {
	stub := &r65GateStub{}
	svc, db, accID := r65SetupGate(t, stub)
	ctx := context.Background()

	exp := time.Now().Add(90 * time.Second) // 窗口比安静窗口还短
	r65SeedMember(t, db, accID, "8003", nil, 1, exp)

	svc.RecoverStalled(ctx, 50)

	if n := stub.count("sendMessage"); n != 1 {
		t.Fatalf("请求内路径已交棒（resends=1）的行应当轮补发, 实际=%d, 全部调用=%v", n, stub.calls)
	}
	if got := r65ReadMember(t, db, accID, "8003"); got.WelcomeSentAt == nil {
		t.Fatal("补发成功后应登记送达")
	}
}

// 手工/迁移写入的行（updated_at 为空）不许被这条判据静默漏掉：判据只在"刚刚有人动过笔"
// 时延后，没有写入时刻就当它早就安静了。
func TestRecoverStalledStillPicksRowsWithoutUpdatedAt(t *testing.T) {
	stub := &r65GateStub{}
	svc, db, accID := r65SetupGate(t, stub)
	ctx := context.Background()

	exp := time.Now().Add(10 * time.Minute)
	r65SeedMember(t, db, accID, "8004", nil, 0, exp)
	res := db.Model(&model.TelegramGroupMember{}).
		Where("chat_id = ? AND user_id = ?", r65ChatID, "8004").
		UpdateColumn("updated_at", gorm.Expr("NULL"))
	if res.Error != nil || res.RowsAffected != 1 {
		t.Fatalf("清空 updated_at 失败: rows=%d err=%v", res.RowsAffected, res.Error)
	}

	svc.RecoverStalled(ctx, 50)

	if n := stub.count("sendMessage"); n != 1 {
		t.Fatalf("没有写入时刻的行应按安静处理并补发, 实际=%d, 全部调用=%v", n, stub.calls)
	}
}
