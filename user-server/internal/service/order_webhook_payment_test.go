// order_webhook_payment_test.go T-P7-02：order-webhook 载荷扩展（payment 对象）与 G15 的四处修复。
//
// 分工：入账本身的判据住在 payment_test.go（那一份用真库真仓储直接调 service，
// 不经过回调这条路）。本文件只测**回调这一侧**的四件事：
//  1. AC① 向后兼容：没有 payment 格子的旧载荷，写入面与本卡之前**逐字一致**
//     （镜像一行为止，payments 零行，且一次回款判据都没跑）；
//  2. AC② 幂等：同一份载荷重投 ⇒ payments 一行、webhook_events 一行（G15-③ 的去重）；
//  3. 不许静默丢钱：载荷带了 payment 而回款腿没装配 ⇒ 明确报错（不是"忽略那一格"）；
//  4. G15 那四处（T-P2-02 实测登记、本卡结清）：跨平台同号不撞、状态不回退、
//     EventID 由内容派生、读故障不被读成"没有这一行"、金额按四舍五入而不是截断。
//
// 夹具用**真 service + 真 repo + 真库**（沿用 setupIntegrationService 那条全局句柄路）：
// 回调这条路的全部价值就在"从 map 到库里那一行"，换内存底座就什么都测不到。
// 只有回款腿用一个假 sink（它的真身在 payment_test.go 已经用真库测过了），
// 本文件要判的是"回调把载荷翻成了什么入参"。
package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
)

// paySink 记录"回调递给回款腿的入参"，并按开关决定报不报错。
type paySink struct {
	calls   []RecordPaymentInput
	receipt *PaymentReceipt
	err     error
	avail   bool
}

func (s *paySink) Available() bool { return s.avail }

func (s *paySink) RecordPayment(_ context.Context, in RecordPaymentInput) (*PaymentReceipt, error) {
	s.calls = append(s.calls, in)
	if s.err != nil {
		return nil, s.err
	}
	if s.receipt != nil {
		return s.receipt, nil
	}
	return &PaymentReceipt{Payment: PaymentView{
		BillID: in.BillID, Amount: in.Amount, Currency: in.Currency,
		ChannelRef: in.ChannelRef, Status: model.PaymentStatusConfirmed,
		Platform: in.Platform, OrderID: in.OrderID,
	}}, nil
}

// webhookCount 数某张表有几行（真库断言，不看返回值）。
func webhookCount[T any](t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var n int64
	var zero T
	if err := db.Model(&zero).Count(&n).Error; err != nil {
		t.Fatalf("清点 %T 失败: %v", zero, err)
	}
	return n
}

func webhookOrder(t *testing.T, db *gorm.DB, platform, orderID string) *model.ExternalOrder {
	t.Helper()
	var row model.ExternalOrder
	if err := db.First(&row, "platform = ? AND order_id = ?", platform, orderID).Error; err != nil {
		t.Fatalf("读回订单 %s/%s 失败: %v", platform, orderID, err)
	}
	return &row
}

// —— ① AC① 向后兼容 ——————————————————————————————————————————————

// TestOrderWebhook_LegacyPayloadIsUntouched 旧载荷（无 payment 格子）：镜像照写，钱一分不记。
//
// 这一条是 DRIFT-06 风险的直接验收：现网三个平台的回调格式不会因为本卡而改变，
// 所以"扩展载荷"必须做成**只加不改**。判据打在两处：payments 零行 + sink 一次都没被叫。
func TestOrderWebhook_LegacyPayloadIsUntouched(t *testing.T) {
	db := setupIntegrationServiceTestDB(t)
	svc := NewIntegrationService()
	sink := &paySink{avail: true}
	svc.SetOrderPaymentSink(sink)

	res, err := svc.UpsertOrderFromWebhook(context.Background(), "taobao", "LEG-001", "paid", map[string]any{
		"order_no": "NO-LEG", "user_phone": "13800001111", "pay_amount": float64(19900),
	})
	if err != nil {
		t.Fatalf("旧载荷被本卡改出了错: %v", err)
	}
	if res.PaymentPresent {
		t.Error("旧载荷没带 payment 格子却被报成带了")
	}
	if res.Payment != nil {
		t.Errorf("旧载荷回出了回款结论: %+v", res.Payment)
	}
	if n := len(sink.calls); n != 0 {
		t.Errorf("旧载荷下回款腿被叫了 %d 次，期望 0（一条没有钱的回调不该跑钱的判据）", n)
	}
	if n := webhookCount[model.Payment](t, db); n != 0 {
		t.Errorf("旧载荷写进了 payments：%d 行", n)
	}
	if got := webhookOrder(t, db, "taobao", "LEG-001"); got.PayAmount != 19900 {
		t.Errorf("镜像金额不对: %d", got.PayAmount)
	}
}

// —— ② 载荷扩展 ——————————————————————————————————————————————

func TestOrderWebhook_PaymentObjectBecomesAnInput(t *testing.T) {
	db := setupIntegrationServiceTestDB(t)
	_ = db
	svc := NewIntegrationService()
	sink := &paySink{avail: true}
	svc.SetOrderPaymentSink(sink)

	body := map[string]any{
		"order_no": "NO-PAY", "pay_amount": float64(36999),
		"payment": map[string]any{
			"bill_id": "b_1", "channel_ref": "ali-2026-0001", "amount": 369.99,
			"currency": "CNY", "paid_at": "2026-05-06 07:08:09",
		},
	}
	res, err := svc.UpsertOrderFromWebhook(context.Background(), "taobao", "PAY-001", "paid", body)
	if err != nil {
		t.Fatalf("带 payment 的载荷处理失败: %v", err)
	}
	if !res.PaymentPresent || res.Payment == nil {
		t.Fatalf("结论里没带回款那一格: %+v", res)
	}
	if len(sink.calls) != 1 {
		t.Fatalf("回款腿被叫了 %d 次，期望 1", len(sink.calls))
	}
	in := sink.calls[0]
	if in.BillID != "b_1" || in.ChannelRef != "ali-2026-0001" || in.Amount != 369.99 {
		t.Errorf("入参三格不对: %+v", in)
	}
	// 平台与订单号取自**回调路径与参数**，不采信载荷自述（同 T-P2-02 签名串的口径）。
	if in.Platform != "taobao" || in.OrderID != "PAY-001" {
		t.Errorf("来路两格取自载荷而非路径: %+v", in)
	}
	if in.Status != "" {
		t.Errorf("载荷没给 status 却填了 %q（默认随入账腿，不在这里填）", in.Status)
	}
	if in.PaidAt == nil || in.PaidAt.Year() != 2026 {
		t.Errorf("paid_at 没解析出来: %v", in.PaidAt)
	}
}

// TestOrderWebhook_PaymentLegMissingIsLoud 载荷带了钱而腿没装配 ⇒ 报错，绝不静默丢。
//
// 反面的形状是"sink 为 nil 就当这一格不存在"：那正是"渠道说收到了钱而我方账上
// 一行都没有"的成因，而且它在两边看起来都是成功的。
func TestOrderWebhook_PaymentLegMissingIsLoud(t *testing.T) {
	db := setupIntegrationServiceTestDB(t)
	svc := NewIntegrationService() // 刻意不 SetOrderPaymentSink

	body := map[string]any{
		"pay_amount": float64(100),
		"payment":    map[string]any{"bill_id": "b_x", "channel_ref": "r_x", "amount": 1.0},
	}
	_, err := svc.UpsertOrderFromWebhook(context.Background(), "taobao", "NOSINK-001", "paid", body)
	if !errors.Is(err, ErrPaymentServiceUnavailable) {
		t.Errorf("回款腿缺失时报的不是 ErrPaymentServiceUnavailable：%v", err)
	}
	if !strings.Contains(err.Error(), "镜像") {
		t.Errorf("错误文案没交代镜像已经写过了（调用方据此决定要不要重发）：%v", err)
	}
	// 镜像那一侧必须仍然写了（订单镜像比"这条回调整体失败"贵，判据见 T-P2-02 那三条口径）。
	if got := webhookOrder(t, db, "taobao", "NOSINK-001"); got.Status != "paid" {
		t.Errorf("回款腿缺失连累了订单镜像: %+v", got)
	}
	// 半装配：sink 在而 Available() 为假 ⇒ 同样报错（不是"它能接"）。
	svc.SetOrderPaymentSink(&paySink{avail: false})
	if _, err := svc.UpsertOrderFromWebhook(context.Background(), "taobao", "NOSINK-002", "paid", body); !errors.Is(err, ErrPaymentServiceUnavailable) {
		t.Errorf("sink 不可用时报的不是 ErrPaymentServiceUnavailable：%v", err)
	}
}

// TestOrderWebhook_MirrorSurvivesPaymentRejection 回款被判拒时，镜像仍写了、错误原样上抛。
func TestOrderWebhook_MirrorSurvivesPaymentRejection(t *testing.T) {
	db := setupIntegrationServiceTestDB(t)
	svc := NewIntegrationService()
	svc.SetOrderPaymentSink(&paySink{avail: true, err: ErrPaymentBillVoided})

	body := map[string]any{
		"pay_amount": float64(100),
		"payment":    map[string]any{"bill_id": "b_void", "channel_ref": "r_v", "amount": 1.0},
	}
	_, err := svc.UpsertOrderFromWebhook(context.Background(), "taobao", "VOID-001", "paid", body)
	if !errors.Is(err, ErrPaymentBillVoided) {
		t.Errorf("哨兵被换掉了：%v", err)
	}
	if got := webhookOrder(t, db, "taobao", "VOID-001"); got.Status != "paid" {
		t.Errorf("回款被拒把订单镜像也挡了: %+v", got)
	}
}

// —— ③ 载荷形状 ——————————————————————————————————————————————

// TestParseOrderWebhookPayment 载荷 → 入参的那次翻译，逐格判（纯函数，不碰库）。
//
// 判据集中在两处：present 的**边界**（什么时候算"这是一笔钱"）与
// 越界形状不许被当成"没有钱"（那是静默丢钱的另一条路）。
func TestParseOrderWebhookPayment(t *testing.T) {
	for _, tc := range []struct {
		name        string
		body        map[string]any
		wantPresent bool
		wantErr     bool
		wantAmount  float64
		wantCur     string
	}{
		{"没有 payment 格子", map[string]any{"order_no": "x"}, false, false, 0, ""},
		{"payment 是 null", map[string]any{"payment": nil}, false, false, 0, ""},
		{"payment 是空对象", map[string]any{"payment": map[string]any{}}, true, true, 0, ""},
		{"payment 是字符串", map[string]any{"payment": "paid"}, true, true, 0, ""},
		{"payment 是数组", map[string]any{"payment": []any{1}}, true, true, 0, ""},
		{"最小三格齐", map[string]any{"payment": map[string]any{
			"bill_id": "b", "channel_ref": "r", "amount": 12.5}}, true, false, 12.5, ""},
		{"金额是字符串小数", map[string]any{"payment": map[string]any{
			"bill_id": "b", "channel_ref": "r", "amount": "369.99"}}, true, false, 369.99, ""},
		{"金额是整数", map[string]any{"payment": map[string]any{
			"bill_id": "b", "channel_ref": "r", "amount": 300}}, true, false, 300, ""},
		{"金额是字符串垃圾", map[string]any{"payment": map[string]any{
			"bill_id": "b", "channel_ref": "r", "amount": "12元"}}, true, true, 0, ""},
		{"金额是 bool", map[string]any{"payment": map[string]any{
			"bill_id": "b", "channel_ref": "r", "amount": true}}, true, true, 0, ""},
		{"缺账单号", map[string]any{"payment": map[string]any{"channel_ref": "r", "amount": 1.0}}, true, true, 0, ""},
		{"缺流水号", map[string]any{"payment": map[string]any{"bill_id": "b", "amount": 1.0}}, true, true, 0, ""},
		{"缺金额", map[string]any{"payment": map[string]any{"bill_id": "b", "channel_ref": "r"}}, true, true, 0, ""},
		{"币种小写要抬起来", map[string]any{"payment": map[string]any{
			"bill_id": "b", "channel_ref": "r", "amount": 1.0, "currency": "cny"}}, true, false, 1, "CNY"},
		{"未知格子被拒", map[string]any{"payment": map[string]any{
			"bill_id": "b", "channel_ref": "r", "amount": 1.0, "bill_amount": 999}}, true, true, 0, ""},
		{"状态 reversed 认", map[string]any{"payment": map[string]any{
			"bill_id": "b", "channel_ref": "r", "amount": 1.0, "status": "reversed"}}, true, false, 1, ""},
		{"状态越域不认", map[string]any{"payment": map[string]any{
			"bill_id": "b", "channel_ref": "r", "amount": 1.0, "status": "refunded"}}, true, true, 0, ""},
	} {
		in, present, err := ParseOrderWebhookPayment(tc.body, "taobao", "ORD-1")
		if present != tc.wantPresent {
			t.Errorf("%s: present=%v，期望 %v", tc.name, present, tc.wantPresent)
			continue
		}
		if tc.wantPresent && (err != nil) != tc.wantErr {
			t.Errorf("%s: err=%v，期望有错=%v", tc.name, err, tc.wantErr)
			if tc.wantErr {
				continue
			}
		}
		if !tc.wantPresent || tc.wantErr {
			continue
		}
		if in.Amount != tc.wantAmount {
			t.Errorf("%s: 金额 %v，期望 %v", tc.name, in.Amount, tc.wantAmount)
		}
		if in.Currency != tc.wantCur {
			t.Errorf("%s: 币种 %q，期望 %q", tc.name, in.Currency, tc.wantCur)
		}
		if in.Platform != "taobao" || in.OrderID != "ORD-1" {
			t.Errorf("%s: 来路两格不对: %+v", tc.name, in)
		}
	}
}

// —— ④ G15 的四处修复 ——————————————————————————————————————————————

// TestG15_TwoPlatformsSameOrderIDDoNotCollide B 平台沿用 A 平台的订单号 ⇒ 两行都在。
//
// T-P2-02 实测的那条硬事实：一次**签名完全合法**的回调撞在 uni_external_orders_order_id 上，
// 表里只 1 行、接口回 500 —— 静默丢单。修法是把唯一键改成 (platform, order_id)。
func TestG15_TwoPlatformsSameOrderIDDoNotCollide(t *testing.T) {
	db := setupIntegrationServiceTestDB(t)
	svc := NewIntegrationService()
	ctx := context.Background()

	for _, platform := range []string{"taobao", "jd"} {
		if _, err := svc.UpsertOrderFromWebhook(ctx, platform, "SHARED-001", "paid", map[string]any{
			"pay_amount": float64(100),
		}); err != nil {
			t.Fatalf("%s 平台的回调被另一家的同号挡住了: %v", platform, err)
		}
	}
	if n := webhookCount[model.ExternalOrder](t, db); n != 2 {
		t.Errorf("external_orders 里 %d 行，期望 2（两家的单都得在）", n)
	}
	// 同号同平台再来一次：仍是更新，不是第三行。
	if _, err := svc.UpsertOrderFromWebhook(ctx, "taobao", "SHARED-001", "shipped", map[string]any{
		"pay_amount": float64(100),
	}); err != nil {
		t.Fatalf("同号同平台的更新失败: %v", err)
	}
	if n := webhookCount[model.ExternalOrder](t, db); n != 2 {
		t.Errorf("三次回调之后 %d 行，期望 2", n)
	}
	if got := webhookOrder(t, db, "taobao", "SHARED-001"); got.Status != "shipped" {
		t.Errorf("同平台的更新没落到镜像: %+v", got)
	}
	if got := webhookOrder(t, db, "jd", "SHARED-001"); got.Status != "paid" {
		t.Errorf("另一家的行被顺手改了: %+v", got)
	}
}

// TestG15_StatusRegressionIsRefused paid 之后一条迟到的 created 不许把状态拽回去。
//
// 判据只挡"往回"，不挡"别的列继续更新"：镜像的价值在于它尽量新，
// 而状态回退会让客服看到"已付款的单又变成待付款"（T-P2-02 实测的第二条硬事实）。
func TestG15_StatusRegressionIsRefused(t *testing.T) {
	db := setupIntegrationServiceTestDB(t)
	svc := NewIntegrationService()
	ctx := context.Background()

	if _, err := svc.UpsertOrderFromWebhook(ctx, "taobao", "REG-001", "paid", map[string]any{
		"pay_amount": float64(500), "user_name": "张三",
	}); err != nil {
		t.Fatalf("首条回调失败: %v", err)
	}
	if _, err := svc.UpsertOrderFromWebhook(ctx, "taobao", "REG-001", "created", map[string]any{
		"pay_amount": float64(500), "user_name": "张三改",
	}); err != nil {
		t.Fatalf("迟到的回调被拒了（回退应当只挡状态那一格）: %v", err)
	}
	got := webhookOrder(t, db, "taobao", "REG-001")
	if got.Status != "paid" {
		t.Errorf("状态被回退成 %q：paid→created 在订单语义里是假事实", got.Status)
	}
	if got.UserName != "张三改" {
		t.Errorf("别的列也跟着不更新了（判据应当只锁状态那一格）: %+v", got)
	}
}

// TestG15_UnknownStatusWordsStillFlow 词表外的状态照旧流转（不冻结镜像）。
//
// 现网载荷用的是中文词（见 order_ft_test.go 的"已付款"/"已完成"），
// 而词表里一个都没有。若把"不在表里"当成回退，那三家平台的每一次推送都会被挡掉，
// 那是比偶发回退贵得多的坏法 —— 所以判据是"两侧都得认识"。
func TestG15_UnknownStatusWordsStillFlow(t *testing.T) {
	db := setupIntegrationServiceTestDB(t)
	svc := NewIntegrationService()
	ctx := context.Background()

	if _, err := svc.UpsertOrderFromWebhook(ctx, "taobao", "ZH-001", "已付款", nil); err != nil {
		t.Fatalf("中文状态首条失败: %v", err)
	}
	if _, err := svc.UpsertOrderFromWebhook(ctx, "taobao", "ZH-001", "退款中", nil); err != nil {
		t.Fatalf("中文状态第二条被拒: %v", err)
	}
	if got := webhookOrder(t, db, "taobao", "ZH-001"); got.Status != "退款中" {
		t.Errorf("词表外的状态没流转成载荷给的那个: %q", got.Status)
	}
}

// TestG15_ThreeIdenticalDeliveriesLeaveOneEventRow 三次投递在 webhook_events 只留一行。
//
// 修法是把 EventID 从"platform:order:<UnixNano>"换成**内容派生**：
// 含纳秒的那个值永远撞不上唯一索引，于是这张表记的其实是"我们被叫了几次"，
// 而它该记的是"渠道报了哪几件事"（nonce 走 fail-open 的那一轮本来就没有别的留痕）。
func TestG15_ThreeIdenticalDeliveriesLeaveOneEventRow(t *testing.T) {
	db := setupIntegrationServiceTestDB(t)
	svc := NewIntegrationService()
	ctx := context.Background()

	body := map[string]any{"pay_amount": float64(700), "user_name": "重复投递"}
	for i := 0; i < 3; i++ {
		if _, err := svc.UpsertOrderFromWebhook(ctx, "taobao", "DUP-001", "paid", body); err != nil {
			t.Fatalf("第 %d 次投递失败: %v", i+1, err)
		}
	}
	if n := webhookCount[model.WebhookEvent](t, db); n != 1 {
		t.Errorf("webhook_events 里 %d 行，期望 1（同一件事的三次通知是一件事）", n)
	}
	// 内容变了就是另一件事：不能被去重挡掉。
	if _, err := svc.UpsertOrderFromWebhook(ctx, "taobao", "DUP-001", "shipped", body); err != nil {
		t.Fatalf("内容变化的投递失败: %v", err)
	}
	if n := webhookCount[model.WebhookEvent](t, db); n != 2 {
		t.Errorf("状态变了之后 %d 行，期望 2", n)
	}
	if n := webhookCount[model.ExternalOrder](t, db); n != 1 {
		t.Errorf("镜像被写成了 %d 行，期望 1", n)
	}
}

// TestG15_ReadFailureIsNotMissingRow 读镜像的故障不许被读成"没有这一行"。
//
// 老代码是 `existing, _ := GetByOrderID(...)`，而那个仓储在"没有"时也返回非 nil 空结构体：
// 于是"库查不动"与"第一次见这单"在判据上长成同一个样子，
// 后果是走 Create 撞唯一键（或者更糟：插出第二行）。这里把表摘掉来造一次真实读故障。
func TestG15_ReadFailureIsNotMissingRow(t *testing.T) {
	db := setupIntegrationServiceTestDB(t)
	svc := NewIntegrationService()
	ctx := context.Background()

	if _, err := svc.UpsertOrderFromWebhook(ctx, "taobao", "READ-001", "paid", nil); err != nil {
		t.Fatalf("建镜像行失败: %v", err)
	}
	if err := db.Migrator().DropTable(&model.ExternalOrder{}); err != nil {
		t.Fatalf("摘表失败: %v", err)
	}
	_, err := svc.UpsertOrderFromWebhook(ctx, "taobao", "READ-002", "paid", nil)
	if err == nil {
		t.Fatal("表都不在了却回成功：读故障被吞成了一次普通处理")
	}
	if !strings.Contains(err.Error(), "读") {
		t.Errorf("错误文案没交代是读这一腿坏的：%v", err)
	}
}

// TestG15_MoneyIsRoundedNotTruncated 镜像的钱按四舍五入进 bigint。
//
// `int64(v)` 是**向零截断**：199.99 会变成 199。这一格是外部订单的元/分快照，
// 截断的方向恰好是让"收了多少钱"永远不少于一分真钱 —— 对账读它的人会少看一分。
func TestG15_MoneyIsRoundedNotTruncated(t *testing.T) {
	db := setupIntegrationServiceTestDB(t)
	svc := NewIntegrationService()
	ctx := context.Background()

	for _, tc := range []struct {
		orderID string
		in      float64
		want    int64
	}{
		{"MONEY-1", 199.99, 200},
		{"MONEY-2", 199.49, 199},
		{"MONEY-3", 199.50, 200},
		{"MONEY-4", 19900, 19900},
	} {
		if _, err := svc.UpsertOrderFromWebhook(ctx, "taobao", tc.orderID, "paid", map[string]any{
			"pay_amount": tc.in, "total_amount": tc.in,
		}); err != nil {
			t.Fatalf("%s 回调失败: %v", tc.orderID, err)
		}
		got := webhookOrder(t, db, "taobao", tc.orderID)
		if got.PayAmount != tc.want || got.TotalAmount != tc.want {
			t.Errorf("%v 落成了 pay=%d total=%d，期望两边都是 %d", tc.in, got.PayAmount, got.TotalAmount, tc.want)
		}
	}
}

// TestG15_BillLinkIsMirroredOntoTheOrder external_orders.bill_id 是**线索副本**，不是判据。
//
// 判据住在 payments.bill_id（求和读的那一列）；这一格只是让运营从订单镜像一眼看到
// "这单挂在哪张应收上"，所以它跟着 payment 格子写，且**不**参与任何求和。
func TestG15_BillLinkIsMirroredOntoTheOrder(t *testing.T) {
	db := setupIntegrationServiceTestDB(t)
	svc := NewIntegrationService()
	svc.SetOrderPaymentSink(&paySink{avail: true})
	ctx := context.Background()

	if _, err := svc.UpsertOrderFromWebhook(ctx, "taobao", "MIRROR-001", "paid", map[string]any{
		"pay_amount": float64(100),
		"payment":    map[string]any{"bill_id": "b_mirror", "channel_ref": "r_mirror", "amount": 1.0},
	}); err != nil {
		t.Fatalf("回调失败: %v", err)
	}
	got := webhookOrder(t, db, "taobao", "MIRROR-001")
	if got.BillID != "b_mirror" {
		t.Errorf("镜像上的账单线索 = %q，期望 b_mirror", got.BillID)
	}
	// 没有 payment 格子的后续推送不许把这一格抹成空串（抹掉等于把来路线索洗掉）。
	if _, err := svc.UpsertOrderFromWebhook(ctx, "taobao", "MIRROR-001", "shipped", map[string]any{
		"pay_amount": float64(100),
	}); err != nil {
		t.Fatalf("第二条回调失败: %v", err)
	}
	if again := webhookOrder(t, db, "taobao", "MIRROR-001"); again.BillID != "b_mirror" {
		t.Errorf("无 payment 的推送把账单线索抹成了 %q", again.BillID)
	}
}
