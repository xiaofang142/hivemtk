package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"hivemtk-user/internal/model"
)

// recordingNotifier 通知口的记录仪：把每条通知原样抄一份留下，并可模拟写失败。
// 本文件不连库 —— 断言的是"该响的时候响了没有、响的是什么"，不是存储本身。
type recordingNotifier struct {
	mu  sync.Mutex
	got []*model.Notification
	err error
}

func (r *recordingNotifier) Create(_ context.Context, n *model.Notification) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	cp := *n
	r.got = append(r.got, &cp)
	return nil
}

func (r *recordingNotifier) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.got)
}

func (r *recordingNotifier) last() *model.Notification {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.got) == 0 {
		return nil
	}
	return r.got[len(r.got)-1]
}

// installTestNotifier 装一个通知口并在测试结束时还原（全局槽是进程级的，
// 不还原会把本用例的桩漏给同包后续用例 —— 与 SetGlobalBadCaseService 的用例同理）。
func installTestNotifier(t *testing.T, n BusinessNotifier) {
	t.Helper()
	before := GlobalNotifier()
	SetGlobalNotifier(n)
	t.Cleanup(func() { SetGlobalNotifier(before) })
}

// ---------------------------------------------------------------------------
// 槽与旁路语义
// ---------------------------------------------------------------------------

// TestNotifyBusiness_SlotRoundTrip 槽的装—写—撤三态：nil 槽只出声不写、
// 写失败回 false 不上抛、装上的实例收到的就是构造函数给的那份内容。
func TestNotifyBusiness_SlotRoundTrip(t *testing.T) {
	ctx := context.Background()
	installTestNotifier(t, nil)

	if NotifyBusiness(ctx, &model.Notification{Title: "无槽"}) {
		t.Error("槽为 nil 时应回 false（只出声不写）")
	}
	if NotifyBusiness(ctx, nil) {
		t.Error("通知体为 nil 应回 false")
	}

	rec := &recordingNotifier{}
	SetGlobalNotifier(rec)
	n := &model.Notification{
		UserID:  7,
		Type:    OrderDraftCreatedNotifyType,
		Title:   "标题",
		Content: "正文",
		Link:    "/somewhere",
	}
	if !NotifyBusiness(ctx, n) {
		t.Fatal("装配后写入应成功")
	}
	if rec.count() != 1 {
		t.Fatalf("应恰写 1 条，实得 %d", rec.count())
	}
	got := rec.last()
	if got.UserID != 7 || got.Type != OrderDraftCreatedNotifyType ||
		got.Title != "标题" || got.Content != "正文" || got.Link != "/somewhere" {
		t.Errorf("记录仪收到的通知与构造不一致：%+v", got)
	}

	rec.err = errors.New("库挂了")
	if NotifyBusiness(ctx, &model.Notification{Title: "写失败也要响"}) {
		t.Error("写失败应回 false（fail-quiet，不把失败上抛给业务动作）")
	}
}

// TestNotifyBusiness_NilSlot_ProducerNoop 钉住存量测试的默认态：
// 全局槽为 nil 时，两个草稿生产入口照常返回草稿 —— 通知缺席不改变任何业务结果，
// 也不能 panic（全包既有用例都没装槽，这一条不成立它们会集体变红）。
func TestNotifyBusiness_NilSlot_ProducerNoop(t *testing.T) {
	ctx := context.Background()
	installTestNotifier(t, nil)
	svc := NewOrderDraftService(nil)

	d := svc.CreateFromIntent(ctx, &OrderIntent{
		CustomerID: "c_nil", ProductName: "光子嫩肤", Quantity: 3,
		UnitPrice: 880, Confidence: 0.9, RawText: "光子嫩肤 3 次 2280 元",
	}, "10086")
	if d == nil {
		t.Fatal("无槽时 CreateFromIntent 应照常建草稿")
	}
	d2, err := svc.CreateManual(ctx, &CreateDraftRequest{
		CustomerID: "c_nil2", OwnerID: "10086", ProductName: "补水", Quantity: 1, UnitPrice: 100,
	})
	if err != nil || d2 == nil {
		t.Fatalf("无槽时 CreateManual 应照常成功：(%v, %v)", d2, err)
	}
}

// ---------------------------------------------------------------------------
// 三个纯构造函数（无 DB、无槽）
// ---------------------------------------------------------------------------

func TestOrderDraftCreatedNotification_Builder(t *testing.T) {
	if OrderDraftCreatedNotification(nil) != nil {
		t.Error("nil 草稿应给 nil 通知")
	}
	n := OrderDraftCreatedNotification(&OrderDraft{
		ID: "od_1", OwnerID: "10086", ProductName: "光子嫩肤", Quantity: 3, TotalAmount: 2640,
	})
	if n.UserID != 10086 {
		t.Errorf("数字 owner 应解析成本人：%d", n.UserID)
	}
	if n.Type != OrderDraftCreatedNotifyType || n.Link != "/dashboard/drafts/od_1" {
		t.Errorf("type/link 错：type=%q link=%q（应直指该草稿详情页 /dashboard/drafts/<id>）", n.Type, n.Link)
	}
	for _, want := range []string{"od_1", "光子嫩肤"} {
		if !strings.Contains(n.Content, want) {
			t.Errorf("正文缺 %q：%s", want, n.Content)
		}
	}
	if n.Title == "" {
		t.Error("标题不能为空")
	}

	// 非数字 owner（"system"/"sales_001" 这类真实值）回 0 广播 —— 发错人比没人看到更坏。
	if got := OrderDraftCreatedNotification(&OrderDraft{ID: "od_2", OwnerID: "sales_001"}); got.UserID != 0 {
		t.Errorf("解析不出的 owner 应回 0 广播，实得 %d", got.UserID)
	}
}

func TestBadCasePendingNotification_Builder(t *testing.T) {
	if BadCasePendingNotification(nil) != nil {
		t.Error("nil 坏例应给 nil 通知")
	}
	n := BadCasePendingNotification(&model.BadCase{
		ID: "bc_1", Source: model.BadCaseSourceLowConfidence, SessionID: "sess_9",
	})
	if n.UserID != 0 {
		t.Errorf("归因通知应广播（0），实得 %d", n.UserID)
	}
	if n.Type != BadCasePendingNotifyType || n.Link != "/badCase/list" {
		t.Errorf("type/link 错：type=%q link=%q", n.Type, n.Link)
	}
	for _, want := range []string{"bc_1", "sess_9"} {
		if !strings.Contains(n.Content, want) {
			t.Errorf("正文缺 %q：%s", want, n.Content)
		}
	}
}

func TestKBChangePendingNotification_Builder(t *testing.T) {
	if KBChangePendingNotification(nil) != nil {
		t.Error("nil 变更应给 nil 通知")
	}
	n := KBChangePendingNotification(&model.KBChangeRequest{
		ID: "kbc_1", Op: model.KBChangeOpAdd, ProductID: "p1", RequestedBy: "op-1",
	})
	if n.UserID != 0 {
		t.Errorf("审核通知应广播（0），实得 %d", n.UserID)
	}
	if n.Type != KBChangePendingNotifyType || n.Link != "/approvalTask/list" {
		t.Errorf("type/link 错：type=%q link=%q", n.Type, n.Link)
	}
	for _, want := range []string{"kbc_1", "p1", "op-1"} {
		if !strings.Contains(n.Content, want) {
			t.Errorf("正文缺 %q：%s", want, n.Content)
		}
	}
}

// ---------------------------------------------------------------------------
// 生产者链路（槽 + 真实业务动作）
// ---------------------------------------------------------------------------

// TestCreateFromIntent_NotifiesOnlyOnNew 新建响一次、合并不再响：
// 合并每次都会命中（客户多聊几句同一产品），每次都响的话真正的那条新草稿会被刷没。
func TestCreateFromIntent_NotifiesOnlyOnNew(t *testing.T) {
	ctx := context.Background()
	rec := &recordingNotifier{}
	installTestNotifier(t, rec)

	svc := NewOrderDraftService(nil)
	intent := &OrderIntent{
		CustomerID: "c_notify", ProductName: "光子嫩肤", Quantity: 3,
		UnitPrice: 880, Confidence: 0.9, RawText: "光子嫩肤 3 次 2280 元",
	}
	d1 := svc.CreateFromIntent(ctx, intent, "10086")
	if d1 == nil {
		t.Fatal("首建应产出草稿")
	}
	if rec.count() != 1 {
		t.Fatalf("首建应恰响 1 条，实得 %d", rec.count())
	}
	n := rec.last()
	if n.UserID != 10086 || n.Type != OrderDraftCreatedNotifyType || n.Link != "/dashboard/drafts/"+d1.ID {
		t.Errorf("通知形状错：%+v", n)
	}
	if !strings.Contains(n.Content, d1.ID) {
		t.Errorf("正文应带草稿 ID：%s", n.Content)
	}

	d2 := svc.CreateFromIntent(ctx, intent, "10086")
	if d2 == nil || d2.ID != d1.ID {
		t.Fatalf("第二次同意向应合并进 %s，实得 %+v", d1.ID, d2)
	}
	if rec.count() != 1 {
		t.Errorf("合并不该再响（会把新草稿的提醒刷没），实得 %d 条", rec.count())
	}
}

// TestCreateManual_Notifies 手创也响：草稿"待确认"本身是挂在 owner 头上的待办，
// 创建者即 owner 时是自提提醒，owner 是别人时这正是他要的。
func TestCreateManual_Notifies(t *testing.T) {
	ctx := context.Background()
	rec := &recordingNotifier{}
	installTestNotifier(t, rec)

	svc := NewOrderDraftService(nil)
	d, err := svc.CreateManual(ctx, &CreateDraftRequest{
		CustomerID: "c_manual", OwnerID: "10086", ProductName: "补水", Quantity: 2, UnitPrice: 199,
	})
	if err != nil || d == nil {
		t.Fatalf("手创建应成功：(%v, %v)", d, err)
	}
	if rec.count() != 1 {
		t.Fatalf("手创建应恰响 1 条，实得 %d", rec.count())
	}
	n := rec.last()
	if n.UserID != 10086 || n.Type != OrderDraftCreatedNotifyType {
		t.Errorf("通知形状错：%+v", n)
	}
}

// TestBadCaseMarkNotifiesOnlyOnCreate 自动首标响、幂等重复不再响、人工补录再响。
// 走 setupBadCaseSvc ⇒ PostgreSQL 测试库，本机不可达时按 testutil 语义 Skip、
// CI 连不上直接 Fatal（阻断式，不允许假绿）。
func TestBadCaseMarkNotifiesOnlyOnCreate(t *testing.T) {
	ctx := context.Background()
	rec := &recordingNotifier{}
	installTestNotifier(t, rec)
	svc, _ := setupBadCaseSvc(t)

	in := BadCaseMarkInput{
		SessionID: "sess_notify", MessageID: "msg_notify", IntentType: "price_inquiry",
		QueryText: "多少钱", AnswerText: "大概那些",
		Confidence: 0.42, Threshold: 0.7, RetrievedCount: 4,
	}
	created, err := svc.Mark(ctx, in)
	if err != nil || !created {
		t.Fatalf("首标应落一行：(%v, %v)", created, err)
	}
	if rec.count() != 1 {
		t.Fatalf("首标应响 1 条，实得 %d", rec.count())
	}
	n := rec.last()
	if n.UserID != 0 || n.Type != BadCasePendingNotifyType || n.Link != "/badCase/list" {
		t.Errorf("通知形状错：%+v", n)
	}

	if created, err = svc.Mark(ctx, in); err != nil || created {
		t.Fatalf("重复标记应幂等：(%v, %v)", created, err)
	}
	if rec.count() != 1 {
		t.Errorf("幂等重复不该再响，实得 %d 条", rec.count())
	}

	if _, err := svc.MarkManual(ctx, BadCaseMarkInput{
		SessionID: "sess_notify_m", MessageID: "msg_notify_m",
		QueryText: "问句", AnswerText: "答句", Confidence: 0.9, Threshold: 0.7,
	}, "7"); err != nil {
		t.Fatalf("人工补录失败：%v", err)
	}
	if rec.count() != 2 {
		t.Errorf("人工补录应再响 1 条（要催的是归因人），实得 %d 条", rec.count())
	}
}

// TestKBSubmitChangeNotifies 审批桩走通 → 响 1 条且带变更三要素；
// 审批入队失败 → 一条都不响（变更行都没落，没有待审核的东西可催）。
func TestKBSubmitChangeNotifies(t *testing.T) {
	ctx := context.Background()
	rec := &recordingNotifier{}
	installTestNotifier(t, rec)
	svc, _, ap := newKBService(t)

	ch, err := svc.SubmitChange(ctx, KBChangeSubmitInput{
		ProductID: "p1", Op: model.KBChangeOpAdd, Content: "正文", DocumentID: 7,
		Reason: "补一条价目", RequestedBy: "op-1",
	})
	if err != nil {
		t.Fatalf("提交应成功：%v", err)
	}
	if rec.count() != 1 {
		t.Fatalf("提交成功应响 1 条，实得 %d", rec.count())
	}
	n := rec.last()
	if n.UserID != 0 || n.Type != KBChangePendingNotifyType || n.Link != "/approvalTask/list" {
		t.Errorf("通知形状错：%+v", n)
	}
	for _, want := range []string{ch.ID, "p1", "op-1"} {
		if !strings.Contains(n.Content, want) {
			t.Errorf("正文缺 %q：%s", want, n.Content)
		}
	}

	ap.submitErr = errors.New("审批底座挂了")
	if _, err := svc.SubmitChange(ctx, KBChangeSubmitInput{
		ProductID: "p1", Op: model.KBChangeOpAdd, Content: "正文", DocumentID: 7,
		Reason: "再补一条", RequestedBy: "op-1",
	}); err == nil {
		t.Fatal("审批入队失败应上抛")
	}
	if rec.count() != 1 {
		t.Errorf("失败的提交不该响，实得 %d 条", rec.count())
	}
}
