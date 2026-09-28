package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// 本批（批23）收 §6 移交的三项与 §8.3-18：入口内容去重键的会话维度、hub 层内容去重的
// 时间界与会话界、以及「拦截即从库里消失」。每条用例的断言文案写明它红的时候是哪一层失守。

func r23Event(nonce, eventID, conv, content string) *model.MessageEvent {
	return &model.MessageEvent{
		Channel:        model.ChannelXHS,
		SenderID:       "customer-" + nonce,
		SenderType:     "customer",
		Content:        content,
		EventID:        eventID,
		ConversationID: conv,
		MsgType:        model.MsgTypeText,
		Timestamp:      time.Now(),
		Extra:          map[string]interface{}{"account_id": "acct-" + nonce},
	}
}

// TestIngress_ContentDedupKeyCarriesConversation §6-1：入口那把 Redis 键必须带会话维度。
// 同一发送者在两个会话里发同一句文本是两条真实消息，5 分钟内第二条不得被拦在入库前。
func TestIngress_ContentDedupKeyCarriesConversation(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	svc := NewInboxIngressServiceWithDB(db, newIsolatedCacheForTest(t))
	ctx := context.Background()

	nonce := fmt.Sprintf("convscope-%d", time.Now().UnixNano())
	content := "在吗 " + nonce

	decide := func(conv, eventID string) *IngressDecision {
		d, err := svc.interceptInbound(ctx, r23Event(nonce, eventID, conv, content))
		if err != nil {
			t.Fatalf("interceptInbound(%s): %v", conv, err)
		}
		return d
	}

	if first := decide("conv-r23-A", "evt-r23-A1"); first.Blocked {
		t.Fatalf("首条不该被拦：%s", first.Reason)
	}
	second := decide("conv-r23-A", "evt-r23-A2")
	if !second.Blocked || !second.IsDup {
		t.Errorf("同会话同发送者同内容的第二条应仍走内容窗口去重（窗口是上游重投的兜底），got blocked=%v dup=%v reason=%q",
			second.Blocked, second.IsDup, second.Reason)
	}
	crossConv := decide("conv-r23-B", "evt-r23-B1")
	if crossConv.Blocked {
		t.Errorf("§6-1 未达成：换会话的同一句话被共用了同一把去重键 ⇒ blocked=%v dup=%v reason=%q"+
			"（键里没有 conversation_id，客户侧表现就是「说了没回」）",
			crossConv.Blocked, crossConv.IsDup, crossConv.Reason)
	}
}

// TestIngress_ContentDedupWindowExpires §6-1 的第三个子句：窗口必须**有界**（有上界也有下界）。
//
// 为什么要把 TTL 做成可注入字段：窗口默认 5 分钟，"到期后不再拦"这条断言在常量上要睡满 5 分钟，
// 于是今天没有任何腿跨过这个界（电池 docstring 里自己登记过这条缺口）。注入一个秒级窗口后，
// 同一把键的两头都有腿：窗口内仍拦得住（下界，由 TestHandleIngress_PersistSuccessKeepsContentWindow
// 代测）与到期后必须放行（上界，本腿）。
func TestIngress_ContentDedupWindowExpires(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	svc := NewInboxIngressServiceWithDB(db, newIsolatedCacheForTest(t))
	// 注入值必须是**字面量**：本腿的等时长一度写成 `svc.contentDedupWindow()`，于是把
	// 「注入值不生效」注进那个函数后，腿只是睡得比默认窗口短、断言照样绿（电池 T1 格实测存活=洞）。
	// 计时器不能取自被检的那把 seam，否则 seam 自己就是自己的判据。
	const injectedTTL = 2 * time.Second
	svc.contentDedupTTL = injectedTTL
	ctx := context.Background()

	nonce := fmt.Sprintf("ttl-%d", time.Now().UnixNano())
	content := "这句稍后再说一遍 " + nonce
	conv := "conv-r23-ttl"

	decide := func(eventID string) *IngressDecision {
		d, err := svc.interceptInbound(ctx, r23Event(nonce, eventID, conv, content))
		if err != nil {
			t.Fatalf("interceptInbound(%s): %v", eventID, err)
		}
		return d
	}

	if first := decide("evt-r23-ttl-1"); first.Blocked {
		t.Fatalf("首条不该被拦：%s", first.Reason)
	}
	inWindow := decide("evt-r23-ttl-2")
	if !inWindow.Blocked || !inWindow.IsDup {
		t.Fatalf("夹具失效：窗口内（TTL=%s）第二条竟没被内容窗口拦下 ⇒ 到期那条断言什么也没测", injectedTTL)
	}
	time.Sleep(injectedTTL + 400*time.Millisecond)

	after := decide("evt-r23-ttl-3")
	if after.Blocked {
		t.Errorf("§6-1 有界性未达成：TTL 到期后同一句话仍被拦（blocked=%v dup=%v reason=%q）"+
			"⇒ 这把键实际上没有界，客户隔一段时间重说原话永远没回", after.Blocked, after.IsDup, after.Reason)
	}
}

// TestIngress_PlatformIDEventsSkipContentDedup 是 §6-3「官方 id > (会话, 内容, 时间窗)」这条
// 优先级的锁：平台给了 channel_msg_id 的事件不得再走内容窗口去重。
//
// 为什么需要单独一把锁：N-14 原有的三条腿只数「库里有没有行」。本轮把「拦截即销毁」改成
// 「重复留痕、只压 AI」之后，那条判据在守卫被摘掉时依然成立（行照样落，只是不回），
// 于是那三条腿对这个守卫失去了牙。本用例直接断言判定本身，摘掉守卫即红。
func TestIngress_PlatformIDEventsSkipContentDedup(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	svc := NewInboxIngressServiceWithDB(db, newIsolatedCacheForTest(t))
	ctx := context.Background()

	nonce := fmt.Sprintf("platid-%d", time.Now().UnixNano())
	content := "同一句话带平台 id " + nonce
	conv := "conv-r23-platid"

	decide := func(eventID, chanMsgID string) *IngressDecision {
		evt := r23Event(nonce, eventID, conv, content)
		evt.Extra["channel_msg_id"] = chanMsgID
		d, err := svc.interceptInbound(ctx, evt)
		if err != nil {
			t.Fatalf("interceptInbound(%s): %v", eventID, err)
		}
		return d
	}

	if first := decide("evt-r23-platid-1", "wamid.PLAT"+nonce+"-1"); first.Blocked {
		t.Fatalf("带平台 id 的首条不该被拦：%s", first.Reason)
	}
	second := decide("evt-r23-platid-2", "wamid.PLAT"+nonce+"-2")
	if second.Blocked {
		t.Errorf("§6-3 优先级失守：平台给了不同 channel_msg_id 的同内容第二条被判 %q"+
			"（内容窗口去重只对「平台没给 id」的渠道有独立价值）", second.Reason)
	}
}

// r23SeedOutbound 预置一条出站行：hub 层内容去重（钩子2.5）只对 direction='outbound' 的行生效，
// 所以「过期/跨会话」两类误吞都要拿出站行来造。
func r23SeedOutbound(t *testing.T, db *gorm.DB, platform, account, conv, content, msgID string, sentAt time.Time) {
	t.Helper()
	if err := db.Create(&model.MessageHub{
		MsgID: msgID, Platform: platform, AccountID: account, Direction: "outbound",
		Status: "delivered", MsgType: "text", ConversationID: conv, SenderID: account,
		Content: content, SentAt: sentAt,
	}).Error; err != nil {
		t.Fatalf("预置 outbound 失败: %v", err)
	}
}

// TestPersistBridgeHistory_ContentDedupExpiresAfterWindow §6-2：hub 层内容命中必须有时间界。
// 历史里发过的原话再说一遍，不能被一句「内容一样」静默吞掉。
func TestPersistBridgeHistory_ContentDedupExpiresAfterWindow(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	svc := NewInboxIngressServiceWithDB(db, newIsolatedCacheForTest(t))
	ctx := context.Background()

	const platform = "xiaohongshu"
	nonce := fmt.Sprintf("aged-%d", time.Now().UnixNano())
	content := "老话重提 " + nonce
	account := "acct-" + nonce
	conv := "conv-r23-aged"

	r23SeedOutbound(t, db, platform, account, conv, content, "mh-r23-old",
		time.Now().Add(-InboxOutboundEchoWindow-time.Hour))

	if err := svc.PersistBridgeHistory(ctx, r23Event(nonce, "evt-r23-aged-in", conv, content), "inbound"); err != nil {
		t.Fatalf("PersistBridgeHistory: %v", err)
	}
	var count int64
	if err := db.Model(&model.MessageHub{}).
		Where("platform = ? AND conversation_id = ? AND direction = 'inbound' AND md5(content) = md5(?)",
			platform, conv, content).Count(&count).Error; err != nil {
		t.Fatalf("统计入库行数: %v", err)
	}
	if count != 1 {
		t.Errorf("§6-2 未达成：超出回声窗口的历史同内容又被说一遍应入库，实际 inbound=%d"+
			"（hub 层内容命中不看行多老）", count)
	}
}

// TestPersistBridgeHistory_ContentDedupStillHooksFreshEcho 同一枚刀的反向半边：窗口内的
// DOM 抖动回显仍必须被吞掉，否则把时间界做成「永不命中」。
func TestPersistBridgeHistory_ContentDedupStillHooksFreshEcho(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	svc := NewInboxIngressServiceWithDB(db, newIsolatedCacheForTest(t))
	ctx := context.Background()

	const platform = "xiaohongshu"
	nonce := fmt.Sprintf("fresh-%d", time.Now().UnixNano())
	content := "您好！😊 很高兴为您服务！\n\n- 🛍️ **产品推荐**：" + nonce
	account := "acct-" + nonce
	conv := "conv-r23-fresh"

	r23SeedOutbound(t, db, platform, account, conv, content, "mh-r23-fresh", time.Now().Add(-time.Minute))
	domVariance := "您好！😊 很高兴为您服务！ - 🛍️ **产品推荐**：" + nonce

	if err := svc.PersistBridgeHistory(ctx, r23Event(nonce, "evt-r23-fresh-in", conv, domVariance), "inbound"); err != nil {
		t.Fatalf("PersistBridgeHistory: %v", err)
	}
	var count int64
	db.Model(&model.MessageHub{}).
		Where("platform = ? AND direction = 'inbound'", platform).Count(&count)
	if count != 0 {
		t.Errorf("窗口内的归一化回显必须继续幂等跳过，实际新增 inbound=%d", count)
	}
}

// TestPersistBridgeHistory_ContentDedupScopedToConversation §6-1 的 hub 层半边：
// B 会话里客户说的话，不该被 A 会话的一条同文本出站行吞掉。
func TestPersistBridgeHistory_ContentDedupScopedToConversation(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	svc := NewInboxIngressServiceWithDB(db, newIsolatedCacheForTest(t))
	ctx := context.Background()

	const platform = "xiaohongshu"
	nonce := fmt.Sprintf("crossconv-%d", time.Now().UnixNano())
	content := "这个多少钱 " + nonce
	account := "acct-" + nonce

	r23SeedOutbound(t, db, platform, account, "conv-r23-A", content, "mh-r23-peer", time.Now().Add(-time.Minute))

	if err := svc.PersistBridgeHistory(ctx, r23Event(nonce, "evt-r23-B-in", "conv-r23-B", content), "inbound"); err != nil {
		t.Fatalf("PersistBridgeHistory: %v", err)
	}
	var row model.MessageHub
	if err := db.Where("msg_id = ?", "evt-r23-B-in").First(&row).Error; err != nil {
		t.Errorf("§6-1 未达成：另一会话的同文本出站行把本会话的客户消息吞了，B 会话没入库：%v", err)
		return
	}
	if row.ConversationID != "conv-r23-B" {
		t.Errorf("入库会话错: got=%s", row.ConversationID)
	}
}

// TestHandleIngress_DupDecisionPersistsWithAISuppressed §8.3-18：「接受但不再执行」要有痕迹。
// IsDup 拦截的消息必须入库（排障时查得到客户说过什么），但 AI 仍要抑制（同一句不得回两遍）。
func TestHandleIngress_DupDecisionPersistsWithAISuppressed(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	svc := NewInboxIngressServiceWithDB(db, newIsolatedCacheForTest(t))
	ctx := context.Background()

	nonce := fmt.Sprintf("duptrace-%d", time.Now().UnixNano())
	content := "麻烦看下订单 " + nonce
	conv := "conv-r23-duptrace"

	first, err := svc.HandleIngressMessage(ctx, r23Event(nonce, "evt-r23-dup-1", conv, content))
	if err != nil {
		t.Fatalf("首条 HandleIngressMessage: %v", err)
	}
	if !first.Accepted || !first.QueuedForAI {
		t.Fatalf("首条应入库并排队 AI: %+v", first)
	}

	second, err := svc.HandleIngressMessage(ctx, r23Event(nonce, "evt-r23-dup-2", conv, content))
	if err != nil {
		t.Fatalf("第二条 HandleIngressMessage: %v", err)
	}
	if !second.QueuedForAI {
		t.Logf("第二条按重复抑制 AI：Reason=%s", second.Reason)
	} else {
		t.Errorf("重复结论必须抑制 AI，否则同一句话得到两次回复")
	}
	var row model.MessageHub
	if err := db.Where("msg_id = ?", "evt-r23-dup-2").First(&row).Error; err != nil {
		t.Errorf("§8.3-18 未达成：被判重复的消息在库里查不到任何痕迹（客户说了没回，排障无据）：%v", err)
		return
	}
	if row.Direction != "inbound" {
		t.Errorf("重复留痕行的方向应为 inbound: got=%s", row.Direction)
	}
}

// TestHandleIngressBatch_DupDecisionPersistsWithAISuppressed §8.3-18 的批次半边：
// 批次路径有自己的一份 Blocked 分支，同一条规则必须两边都成立（漏一边＝批里的重复无痕）。
func TestHandleIngressBatch_DupDecisionPersistsWithAISuppressed(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	svc := NewInboxIngressServiceWithDB(db, newIsolatedCacheForTest(t))
	ctx := context.Background()

	nonce := fmt.Sprintf("batchdup-%d", time.Now().UnixNano())
	content := "批次里的重复句话 " + nonce
	conv := "conv-r23-batchdup"

	if _, err := svc.HandleIngressMessage(ctx, r23Event(nonce, "evt-r23-batch-1", conv, content)); err != nil {
		t.Fatalf("首条 HandleIngressMessage: %v", err)
	}

	batch, err := svc.HandleIngressBatch(ctx, []*model.MessageEvent{
		r23Event(nonce, "evt-r23-batch-2", conv, content),
	})
	if err != nil {
		t.Fatalf("HandleIngressBatch: %v", err)
	}
	if batch.TriggeredAI {
		t.Errorf("重复句不得进 AI 合并触发: %+v", batch)
	}
	per := batch.PerEvent[0]
	if !per.Accepted || per.QueuedForAI {
		t.Errorf("批内重复应 Accepted 且压 AI: %+v", per)
	}
	var row model.MessageHub
	if err := db.Where("msg_id = ?", "evt-r23-batch-2").First(&row).Error; err != nil {
		t.Errorf("§8.3-18 批次半边未达成：批里被判重复的消息无痕迹：%v", err)
	}
}

// TestHandleIngress_SelfEchoDecisionPersistsNothing §8.3-18 的另一半：回声维持不落库，
// 否则 AI 自己的话会被当成客户新消息再次入库并触发。
func TestHandleIngress_SelfEchoDecisionPersistsNothing(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	svc := NewInboxIngressServiceWithDB(db, newIsolatedCacheForTest(t))
	ctx := context.Background()

	const platform = "xiaohongshu"
	nonce := fmt.Sprintf("echo-%d", time.Now().UnixNano())
	account := "acct-" + nonce
	conv := "conv-r23-echo"
	wamid := "wamid.ECHO" + nonce
	content := "这是 AI 回复 " + nonce

	r23SeedOutbound(t, db, platform, account, conv, content, wamid, time.Now().Add(-10*time.Second))

	evt := r23Event(nonce, "evt-r23-echo-in", conv, "回声内容无关紧要")
	evt.Extra["channel_msg_id"] = wamid
	res, err := svc.HandleIngressMessage(ctx, evt)
	if err != nil {
		t.Fatalf("HandleIngressMessage: %v", err)
	}
	if res.QueuedForAI {
		t.Errorf("精确回显不应触发 AI: %+v", res)
	}
	var count int64
	db.Model(&model.MessageHub{}).
		Where("platform = ? AND direction = 'inbound'", platform).Count(&count)
	if count != 0 {
		t.Errorf("回声落库会污染会话（§8.3-18 明确维持不落库），实际 inbound=%d", count)
	}
}
