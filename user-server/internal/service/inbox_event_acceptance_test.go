package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/channelbot/onebot"
	"hivemtk-user/internal/channelbot/qq"
	"hivemtk-user/internal/channelbot/telegram"
	"hivemtk-user/internal/channelcaps"
	"hivemtk-user/internal/event"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// specs/024 T201–T210 验收点名测试（B2 事件类型体系 + 交互回调上下文的单元级钉子）。
// 与 B2 既有实现测试互补：这里按 tasks.md 验收原文逐条命名落账，防实现漂移。
// 仓储在 NewInboxIngressService 构造时捕获全局句柄——**先建库后建 svc**。

// myFakeAITrigger 记录 AI 触发次数（独立夹具，不依赖并行会话的测试文件）。
type myFakeAITrigger struct {
	mu    sync.Mutex
	calls int
}

func (f *myFakeAITrigger) TriggerInboundAI(context.Context, string, string, string, string, string, string, ...TriggerInboundOption) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
}

func (f *myFakeAITrigger) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func eventAcceptDB(t *testing.T) *gorm.DB {
	t.Helper()
	return testutil.NewTestDB(t,
		&model.MessageHub{}, &model.InboxConversation{}, &model.DelayedOutboundReply{},
		&model.WebhookEvent{}, &model.IntegrationAccount{}, &model.UnifiedMessage{},
		&model.CustomerSession{}, &model.SessionMessage{}, &model.AISuggestion{},
		&model.AgentStatus{}, &model.Customer{}, &model.MessageTrace{},
		&model.ChannelEventAudit{}, &model.ChannelInteraction{}, &model.ChannelRouteRule{},
	)
}

func evidenceEventMine(kind, account string) *model.MessageEvent {
	return &model.MessageEvent{
		EventID:   "evt-acc-" + kind + "-" + account,
		Channel:   "douyin",
		EventType: kind, // 必须显式携带：空类型会被归一成 message.received 走消息管线
		MsgType:   "text",
		SenderID:  "sender-acc",
		Content:   "hello",
		Timestamp: time.Now(),
		Extra:     model.JSONMap{"account_id": account},
	}
}

func ingressEventSeed(t *testing.T) (*InboxIngressService, *myFakeAITrigger, *gorm.DB) {
	t.Helper()
	database := eventAcceptDB(t)
	seed := &model.MessageHub{
		Platform: "douyin", AccountID: "acc-e", MsgID: "mh-e-in", Direction: "inbound",
		Content: "原内容", ConversationID: "c-e", Status: "delivered",
		SenderID: "cust-e", SentAt: time.Now(),
	}
	if err := database.Create(seed).Error; err != nil {
		t.Fatalf("seed hub: %v", err)
	}
	svc := NewInboxIngressService()
	trig := &myFakeAITrigger{}
	svc.SetAITrigger(trig)
	return svc, trig, database
}

// ── T201 NormalizeEvent ─────────────────────────────────────────────────

func TestNormalizeEvent_DefaultsToReceived(t *testing.T) {
	svc := NewInboxIngressService()
	ev := &model.MessageEvent{Channel: "douyin", MsgType: "text"}
	if err := svc.NormalizeEvent(context.Background(), ev); err != nil {
		t.Fatalf("NormalizeEvent 失败: %v", err)
	}
	if ev.EventType != model.EventTypeMessageReceived {
		t.Fatalf("空 EventType 应默认 message.received，实际 %q", ev.EventType)
	}
}

func TestNormalizeEvent_PreservesExplicitType(t *testing.T) {
	svc := NewInboxIngressService()
	ev := &model.MessageEvent{Channel: "douyin", MsgType: "text", EventType: model.EventTypeGroupMemberJoined}
	if err := svc.NormalizeEvent(context.Background(), ev); err != nil {
		t.Fatalf("NormalizeEvent 失败: %v", err)
	}
	if ev.EventType != model.EventTypeGroupMemberJoined {
		t.Fatalf("显式 EventType 必须保留，实际 %q", ev.EventType)
	}
}

// ── T202 仓储能力 ───────────────────────────────────────────────────────

func hubRecallSeed(t *testing.T) (*repository.MessageHubRepository, *gorm.DB) {
	t.Helper()
	database := testutil.NewTestDB(t, &model.MessageHub{})
	rows := []*model.MessageHub{
		{Platform: "douyin", AccountID: "acc-r", MsgID: "mh-r-in", Direction: "inbound",
			Content: "旧内容", ConversationID: "c-r", Status: "delivered", SentAt: time.Now()},
		{Platform: "douyin", AccountID: "acc-r", MsgID: "mh-r-out", Direction: "outbound",
			Content: "外发", ConversationID: "c-r", Status: "delivered", SentAt: time.Now()},
	}
	for _, r := range rows {
		if err := database.Create(r).Error; err != nil {
			t.Fatalf("seed hub: %v", err)
		}
	}
	return repository.NewMessageHubRepository(), database
}

func TestMessageHub_UpdateContentByChannelMsgID(t *testing.T) {
	repo, database := hubRecallSeed(t)
	ok, err := repo.UpdateContentByChannelMsgID(context.Background(), "douyin", "acc-r", "mh-r-in", "编辑后的新内容")
	if err != nil || !ok {
		t.Fatalf("编辑覆盖应成功: ok=%v err=%v", ok, err)
	}
	var row model.MessageHub
	if err := database.Where("msg_id = ?", "mh-r-in").First(&row).Error; err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	if row.Content != "编辑后的新内容" {
		t.Fatalf("内容应已覆盖，实际 %q", row.Content)
	}
}

func TestMessageHub_MarkInboundRecalled_RejectsOutboundRow(t *testing.T) {
	repo, _ := hubRecallSeed(t)
	ok, err := repo.MarkInboundRecalledByChannelMsgID(context.Background(), "douyin", "acc-r", "mh-r-out", time.Now(), "test")
	if err != nil {
		t.Fatalf("撤回调用失败: %v", err)
	}
	if ok {
		t.Fatal("outbound 行不得被 MarkInboundRecalled 命中（只撤客户消息）")
	}
}

func TestMessageHub_MarkInboundRecalled_NotFound(t *testing.T) {
	repo, _ := hubRecallSeed(t)
	ok, err := repo.MarkInboundRecalledByChannelMsgID(context.Background(), "douyin", "acc-r", "mh-nope", time.Now(), "test")
	if err != nil {
		t.Fatalf("撤回调用失败: %v", err)
	}
	if ok {
		t.Fatal("不存在的 msg_id 应返回 false（未命中）")
	}
}

// ── T203 入站按事件类型分流 ─────────────────────────────────────────────

func TestIngress_EditedUpdatesContent(t *testing.T) {
	svc, trig, database := ingressEventSeed(t)
	ev := evidenceEventMine(model.EventTypeMessageEdited, "acc-e")
	ev.EventID = "mh-e-in" // 与存量入站行同 channel_msg_id
	ev.Content = "编辑后的内容"
	res, err := svc.HandleIngressMessage(context.Background(), ev)
	if err != nil || res == nil || !res.Accepted {
		t.Fatalf("编辑事件应被接受: %+v err=%v", res, err)
	}
	if trig.count() != 0 {
		t.Fatalf("编辑事件不得触发 AI（非 message.received），实际 %d 次", trig.count())
	}
	var row model.MessageHub
	if err := database.Where("msg_id = ?", "mh-e-in").Order("id desc").First(&row).Error; err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	if row.Content != "编辑后的内容" {
		t.Fatalf("存量行内容应被覆盖，实际 %q", row.Content)
	}
}

func TestIngress_DeletedMarksRecall(t *testing.T) {
	svc, trig, database := ingressEventSeed(t)
	ev := evidenceEventMine(model.EventTypeMessageDeleted, "acc-e")
	ev.EventID = "mh-e-in"
	res, err := svc.HandleIngressMessage(context.Background(), ev)
	if err != nil || res == nil || !res.Accepted {
		t.Fatalf("撤回事件应被接受: %+v err=%v (EventType=%q)", res, err, ev.EventType)
	}
	if trig.count() != 0 {
		t.Fatalf("撤回事件不得触发 AI，实际 %d 次", trig.count())
	}
	var row model.MessageHub
	if err := database.Where("msg_id = ? AND direction = ?", "mh-e-in", "inbound").
		Order("id asc").First(&row).Error; err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	if row.RecalledAt == nil {
		t.Fatalf("存量行应被标记撤回（recalled_at 落值），got status=%s content=%q", row.Status, row.Content)
	}
}

func TestIngress_MemberEventDoesNotCreateMessage(t *testing.T) {
	svc, trig, database := ingressEventSeed(t)
	ev := evidenceEventMine(model.EventTypeGroupMemberJoined, "acc-e")
	ev.Extra["group_id"] = "g-e"
	res, err := svc.HandleIngressMessage(context.Background(), ev)
	if err != nil || res == nil || !res.Accepted {
		t.Fatalf("成员事件应留证接受: %+v err=%v", res, err)
	}
	if trig.count() != 0 {
		t.Fatalf("成员事件不得触发 AI，实际 %d 次", trig.count())
	}
	// 成员事件全程不产消息行：表内只有 seed 那条入站行
	//（message_hub 无 event_type 列——事件证据落 channel_event_audits，见 T209）。
	var total int64
	if err := database.Model(&model.MessageHub{}).Count(&total).Error; err != nil {
		t.Fatalf("计数失败: %v", err)
	}
	if total != 1 {
		t.Fatalf("成员事件不得新增消息行，实际总量 %d（seed 1 条）", total)
	}
}

func TestIngress_NonReceivedDoesNotTouchUnread(t *testing.T) {
	svc, _, database := ingressEventSeed(t)
	for _, kind := range []string{model.EventTypeGroupMemberJoined, model.EventTypeInteractionCallback} {
		ev := evidenceEventMine(kind, "acc-e")
		if _, err := svc.HandleIngressMessage(context.Background(), ev); err != nil {
			t.Fatalf("%s 处理失败: %v", kind, err)
		}
	}
	// 未读数来自 inbox_conversations（由 message.received 的消息行驱动）：
	// 非 received 事件全程未写消息行 ⇒ 收件箱零新增（未读不受影响）。
	var n int64
	if err := database.Model(&model.InboxConversation{}).Count(&n).Error; err != nil {
		t.Fatalf("读 inbox_conversations 失败: %v", err)
	}
	if n != 0 {
		t.Fatalf("非 received 事件不得新增收件箱行（未读驱动源），实际 %d", n)
	}
}

// ── T204 事件总线 ───────────────────────────────────────────────────────

func TestEvent_AuditTopicDelivery(t *testing.T) {
	eventAcceptDB(t)
	prev := event.GetGlobalBus()
	bus := event.New(2, 64)
	event.SetGlobalBus(bus)
	t.Cleanup(func() { event.SetGlobalBus(prev) })

	type delivery struct {
		mu       sync.Mutex
		tenantID uint
		evType   string
		got      bool
	}
	d := &delivery{}
	bus.Subscribe(event.TopicCustomerInteractionCallback, func(evt event.Event) error {
		p, _ := evt.Payload.(event.MessageLifecycleEvent)
		d.mu.Lock()
		d.tenantID, d.evType, d.got = p.TenantID, p.EventType, true
		d.mu.Unlock()
		return nil
	})

	svc, trig, _ := ingressEventSeed(t)
	ctx := tenant.WithTenantID(context.Background(), 18)
	ev := evidenceEventMine(model.EventTypeInteractionCallback, "acc-e")
	ev.Extra["interaction_id"] = "int-bus-1"
	ev.Extra["tenant_id"] = "19" // 伪租户必须被 ctx 权威覆盖
	if _, err := svc.HandleIngressMessage(ctx, ev); err != nil {
		t.Fatalf("交互回调处理失败: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		d.mu.Lock()
		got := d.got
		d.mu.Unlock()
		if got {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.got {
		t.Fatal("订阅方未收到 customer.interaction.callback 事件")
	}
	if d.tenantID != 18 {
		t.Errorf("事件租户应来自 ctx（18），实际 %d（Extra 伪租户 19 不得生效）", d.tenantID)
	}
	if d.evType != model.EventTypeInteractionCallback {
		t.Errorf("事件类型应透传，实际 %q", d.evType)
	}
	if trig.count() != 0 {
		t.Errorf("交互回调不得触发 AI，实际 %d 次", trig.count())
	}
}

func TestEvent_NoSubscriberDoesNotBlock(t *testing.T) {
	prev := event.GetGlobalBus()
	bus := event.New(2, 64)
	event.SetGlobalBus(bus)
	t.Cleanup(func() { event.SetGlobalBus(prev) })

	start := time.Now()
	event.PublishCtx(context.Background(), "customer.nobody.listens", map[string]any{"x": 1})
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("无订阅者发布不得阻塞，耗时 %v", elapsed)
	}
}

// ── T205 解析器矩阵 ─────────────────────────────────────────────────────

func TestChannelParsers_EventTypeMatrix(t *testing.T) {
	// qq：机器人入群/退群 → 事件类型（解析器只产 MessageEvent，不产消息行）
	qqCases := []struct{ t, want string }{
		{"GROUP_ADD_ROBOT", model.EventTypeBotInvited},
		{"GROUP_DEL_ROBOT", model.EventTypeBotRemoved},
	}
	for _, c := range qqCases {
		e := &qq.Event{ID: "evt-mx", T: c.t, D: []byte(`{"group_openid":"g-mx","op_member_openid":"u-mx"}`)}
		in := e.ToInbound("42")
		if in == nil {
			t.Fatalf("qq %s 不应被丢弃", c.t)
		}
		if in.EventType != c.want {
			t.Errorf("qq %s → %q，期望 %q", c.t, in.EventType, c.want)
		}
		if in.MessageID == "" {
			t.Errorf("qq %s 缺幂等键（事件重投会重复入账）", c.t)
		}
	}
	// feishu：平台事件头映射（事实源=feishuPlatformEventType）
	feishuCases := map[string]string{
		"im.message.recalled_v1":         model.EventTypeMessageDeleted,
		"im.chat.member.user.added_v1":   model.EventTypeGroupMemberJoined,
		"im.chat.member.user.deleted_v1": model.EventTypeGroupMemberLeft,
	}
	for in, want := range feishuCases {
		if got := feishuPlatformEventType(mkFeishuHeader(in)); got != want {
			t.Errorf("feishu %s → %q，期望 %q", in, got, want)
		}
	}
	// onebot：notice 四类事件 → 事件类型（specs/025 T801/T803 具名腿）
	// group_recall=撤回、group_increase/decrease=成员增减、message=普通消息；
	// request/meta_event 只进总线，不产生消息行。
	onebotCases := []struct{ notice, want string }{
		{"group_recall", model.EventTypeMessageDeleted},
		{"group_increase", model.EventTypeGroupMemberJoined},
		{"group_decrease", model.EventTypeGroupMemberLeft},
	}
	for _, c := range onebotCases {
		raw := []byte(`{"time":1700000000,"self_id":1,"post_type":"notice","notice_type":"` + c.notice + `","group_id":7,"user_id":8,"message_id":9}`)
		e, err := onebot.ParseEvent(raw)
		if err != nil {
			t.Fatalf("onebot %s 解析失败: %v", c.notice, err)
		}
		in := e.ToInbound("42")
		if in == nil {
			t.Fatalf("onebot %s 不应被丢弃", c.notice)
		}
		if in.EventType != c.want {
			t.Errorf("onebot %s → %q，期望 %q", c.notice, in.EventType, c.want)
		}
		if in.MessageID == "" {
			t.Errorf("onebot %s 缺幂等键（事件重投会重复入账）", c.notice)
		}
	}
	// slack：subtype 决定事件类型（message_deleted / message_changed / 普通 message）
	slackCases := []struct {
		body string
		want string
	}{
		{`{"type":"event_callback","event_id":"Ev-m1","event":{"type":"message","subtype":"message_deleted","channel":"C1","ts":"1.1","deleted_ts":"1.0"}}`, model.EventTypeMessageDeleted},
		{`{"type":"event_callback","event_id":"Ev-m2","event":{"type":"message","subtype":"message_changed","channel":"C1","ts":"1.2","message":{"text":"edit"}}}`, model.EventTypeMessageEdited},
		{`{"type":"event_callback","event_id":"Ev-m3","event":{"type":"message","user":"U1","text":"hi","channel":"C1","ts":"1.3","channel_type":"im"}}`, model.EventTypeMessageReceived},
	}
	for _, c := range slackCases {
		p, err := parseSlackPayload([]byte(c.body))
		if err != nil {
			t.Fatalf("slack 解析失败: %v", err)
		}
		if p.EventType != c.want {
			t.Errorf("slack %s → %q，期望 %q", c.body, p.EventType, c.want)
		}
		if p.EventID == "" {
			t.Errorf("slack %s 缺幂等键", c.body)
		}
	}

	// 全表兜底（received-only 渠道）：wecom/wechat/dingtalk/whatsapp/web/email/sms/custom
	// 的解析器只产 message.received（或纯触达无入站），manifest.Events 与解析器一致性
	// 由 channelcaps_events_test 钉死；这里逐渠道断言「非消息事件词表为空 ⇒ 不可能产
	// 平台事件行」，与上方三腿共同构成全表覆盖。
	for _, m := range channelcaps.All() {
		switch m.Channel {
		case "telegram", "feishu", "qq", "onebot", "slack":
			continue // 具名腿已各自断言
		}
		if len(m.Events) > 1 {
			t.Errorf("渠道 %s 声明了多事件 %v 但没有对应解析器腿，矩阵须补具名 fixture", m.Channel, m.Events)
		}
	}
	// telegram：按钮回调 → interaction.callback
	u := &telegram.Update{
		UpdateID: 99,
		CallbackQuery: &telegram.TGCallbackQuery{
			ID:   "cb-mx",
			Data: "approve:42",
			From: &telegram.TGUser{ID: 7, FirstName: "矩阵"},
			Message: &telegram.TGCallbackMessage{
				MessageID: 555,
				Chat:      telegram.TGChat{ID: 8, Type: "private"},
			},
		},
	}
	in := u.ToInbound("1")
	if in == nil {
		t.Fatal("telegram 回调不应被丢弃")
	}
	if in.EventType != model.EventTypeInteractionCallback {
		t.Errorf("telegram callback → %q，期望 interaction.callback", in.EventType)
	}
}

// mkFeishuHeader 构造飞书平台事件头（与 TestFeishuPlatformEventType 同型匿名结构）。
func mkFeishuHeader(et string) *struct {
	EventType    string          `json:"event_type"`
	AppID        string          `json:"app_id"`
	TenantKey    string          `json:"tenant_key"`
	EventID      string          `json:"event_id"`
	Token        string          `json:"token"`
	CreateTime   feishuTimestamp `json:"create_time"`
	AppSecretVer int             `json:"app_secret_ver"`
} {
	return &struct {
		EventType    string          `json:"event_type"`
		AppID        string          `json:"app_id"`
		TenantKey    string          `json:"tenant_key"`
		EventID      string          `json:"event_id"`
		Token        string          `json:"token"`
		CreateTime   feishuTimestamp `json:"create_time"`
		AppSecretVer int             `json:"app_secret_ver"`
	}{EventType: et}
}

// ── T207/T209 交互回调入站 ──────────────────────────────────────────────

func TestInteractionCallback_AcksAndDoesNotReply(t *testing.T) {
	svc, trig, database := ingressEventSeed(t)
	ev := evidenceEventMine(model.EventTypeInteractionCallback, "acc-e")
	ev.Extra["interaction_id"] = "int-ack-1"
	ev.Extra["action"] = "cta.click"
	res, err := svc.HandleIngressMessage(context.Background(), ev)
	if err != nil || res == nil || !res.Accepted {
		t.Fatalf("交互回调应被接受: %+v err=%v", res, err)
	}
	if trig.count() != 0 {
		t.Fatalf("交互回调不得触发 AI/回复，实际 %d 次", trig.count())
	}
	// 无平台 ack 通道的渠道（douyin 无 answerer）走 fallback ack：登记行转 answered
	if !svc.MarkInteractionAnswered(context.Background(), "douyin", res.InteractionID) {
		t.Fatal("fallback ack 必须把交互行置为 answered")
	}
	var n int64
	if err := database.Model(&model.MessageHub{}).Count(&n).Error; err != nil {
		t.Fatalf("读消息表失败: %v", err)
	}
	if n != 1 { // 只有 seed 那条入站行；交互回调零新增
		t.Fatalf("交互回调不得在消息表新增行，实际 %d 行", n)
	}
}

func TestChannelInteraction_CreateIdempotent(t *testing.T) {
	eventAcceptDB(t)
	repo := repository.NewChannelInteractionRepositoryDefault()
	row := &model.ChannelInteraction{TenantID: "1", Channel: "douyin", AccountID: "acc-i",
		InteractionID: "int-idem", ConversationID: "c-i", Action: "click", Payload: model.JSONMap{}}
	created1, err := repo.CreateIdempotent(context.Background(), row)
	if err != nil || !created1 {
		t.Fatalf("首次登记应成功: created=%v err=%v", created1, err)
	}
	created2, err := repo.CreateIdempotent(context.Background(), row)
	if err != nil {
		t.Fatalf("重复登记报错: %v", err)
	}
	if created2 {
		t.Fatal("同键重复登记必须幂等（created=false）")
	}
	var n int64
	if err := db.GetDB().Model(&model.ChannelInteraction{}).Count(&n).Error; err != nil {
		t.Fatalf("读交互表失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("同键两次登记应只有 1 行，实际 %d", n)
	}
}

func TestChannelInteraction_MarkAnsweredOnlyOnce(t *testing.T) {
	eventAcceptDB(t)
	repo := repository.NewChannelInteractionRepositoryDefault()
	row := &model.ChannelInteraction{TenantID: "1", Channel: "telegram", AccountID: "acc-i",
		InteractionID: "int-once", ConversationID: "c-i", Payload: model.JSONMap{}}
	if _, err := repo.CreateIdempotent(context.Background(), row); err != nil {
		t.Fatalf("登记失败: %v", err)
	}
	if ok, _ := repo.MarkAnswered(context.Background(), "telegram", "int-once", time.Now()); !ok {
		t.Fatal("首次 MarkAnswered 应命中 pending 行")
	}
	if ok, _ := repo.MarkAnswered(context.Background(), "telegram", "int-once", time.Now()); ok {
		t.Fatal("重复 MarkAnswered 必须命中 0 行（只答一次）")
	}
}

func TestChannelInteraction_ExpiredNotAnswerable(t *testing.T) {
	eventAcceptDB(t)
	repo := repository.NewChannelInteractionRepositoryDefault()
	past := time.Now().Add(-time.Minute)
	row := &model.ChannelInteraction{TenantID: "1", Channel: "feishu", AccountID: "acc-i",
		InteractionID: "int-exp", ConversationID: "c-i", Payload: model.JSONMap{}, ExpiresAt: &past}
	if _, err := repo.CreateIdempotent(context.Background(), row); err != nil {
		t.Fatalf("登记失败: %v", err)
	}
	if n, err := repo.MarkExpired(context.Background(), time.Now().Add(time.Minute)); err != nil || n != 1 {
		t.Fatalf("到期清理应标记 1 行: n=%d err=%v", n, err)
	}
	if ok, _ := repo.MarkAnswered(context.Background(), "feishu", "int-exp", time.Now()); ok {
		t.Fatal("expired 行不得再被答复（WHERE status='pending' 不命中）")
	}
}

func TestIngress_InteractionCallback_AuditWritten(t *testing.T) {
	svc, _, database := ingressEventSeed(t)
	ev := evidenceEventMine(model.EventTypeInteractionCallback, "acc-e")
	ev.Extra["interaction_id"] = "int-audit-1"
	if _, err := svc.HandleIngressMessage(context.Background(), ev); err != nil {
		t.Fatalf("处理失败: %v", err)
	}
	var n int64
	if err := database.Model(&model.ChannelEventAudit{}).Where("event_type = ?", model.EventTypeInteractionCallback).Count(&n).Error; err != nil {
		t.Fatalf("读审计表失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("交互回调必须写一条平台事件审计，实际 %d", n)
	}
}

func TestIngress_InteractionFallbackAckWhenUnsupported(t *testing.T) {
	svc, _, _ := ingressEventSeed(t)
	// douyin 无交互答复通道（answerer 属 025 批）：ingress 登记 + fallback ack 必须成立，
	// 不让点击行永远挂在 pending。
	ev := evidenceEventMine(model.EventTypeInteractionCallback, "acc-e")
	ev.Extra["interaction_id"] = "int-fallback-1"
	res, err := svc.HandleIngressMessage(context.Background(), ev)
	if err != nil || res == nil || res.InteractionID == "" {
		t.Fatalf("交互回调应登记并回传 InteractionID: %+v err=%v", res, err)
	}
	if !svc.MarkInteractionAnswered(context.Background(), "douyin", res.InteractionID) {
		t.Fatal("无 answerer 渠道的 fallback ack 必须成立")
	}
}
