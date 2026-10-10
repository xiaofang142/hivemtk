package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

func TestInboxIngress_SelfEcho_RecentOutboundByContent_Blocked(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	svc := NewInboxIngressServiceWithDB(db, newIsolatedCacheForTest(t))
	ctx := context.Background()

	const (
		platform = "xiaohongshu"
		account  = "69c730300000000034018cb2"
		conv     = "69c62722000000003203b0fe"
	)
	outContent := "可以的！😊 关于代码二次开发,给您简单说明：\n\n- **开源协议**：HiveMTK 采用 **GNU AGPL-3.0**\n- **仓库地址**：Gitee 主仓库"
	if err := db.Create(&model.MessageHub{
		MsgID:          "mh:outbound-test-1",
		Platform:       platform,
		AccountID:      account,
		Direction:      "outbound",
		Status:         "delivered",
		MsgType:        "text",
		ConversationID: conv,
		SenderID:       account,
		SenderName:     "",
		Content:        outContent,
		SentAt:         time.Now().Add(-30 * time.Second),
	}).Error; err != nil {
		t.Fatalf("插入 outbound 失败: %v", err)
	}

	loopContent := strings.ReplaceAll(outContent, "\n", " ")
	event := &model.MessageEvent{
		Channel:        platform,
		SenderID:       conv,
		SenderName:     "小红薯69C69EDE",
		SenderType:     "customer",
		Content:        loopContent,
		EventID:        "mh:loopback-test-1",
		ConversationID: conv,
		Extra:          map[string]interface{}{"account_id": account},
	}
	decision, err := svc.interceptInbound(ctx, event)
	if err != nil {
		t.Fatalf("interceptInbound 失败: %v", err)
	}
	if decision == nil || !decision.Blocked {
		t.Fatalf("回环应被拦截, got: %+v", decision)
	}
	if !decision.IsSelfEcho {
		t.Fatalf("回环应标记 IsSelfEcho=true, got: %+v", decision)
	}
	if !strings.Contains(decision.Reason, "recent outbound") {
		t.Fatalf("拦截原因应包含 recent outbound, got: %q", decision.Reason)
	}
}

// 真实客户消息:内容与本账号近期 outbound 不同,必须放行(不误伤)
func TestInboxIngress_RealCustomer_NotBlockedByContentEcho(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	svc := NewInboxIngressServiceWithDB(db, newIsolatedCacheForTest(t))
	ctx := context.Background()

	const (
		platform = "xiaohongshu"
		account  = "acct-real-1"
		conv     = "conv-real-1"
	)
	if err := db.Create(&model.MessageHub{
		MsgID:          "mh:ob-A",
		Platform:       platform,
		AccountID:      account,
		Direction:      "outbound",
		Status:         "delivered",
		MsgType:        "text",
		ConversationID: conv,
		SenderID:       account,
		Content:        "HiveMTK 支持私有化部署,云服务器也可以",
		SentAt:         time.Now().Add(-30 * time.Second),
	}).Error; err != nil {
		t.Fatalf("插入 outbound 失败: %v", err)
	}

	event := &model.MessageEvent{
		Channel:        platform,
		SenderID:       "customer-physical-id",
		SenderName:     "真实客户",
		SenderType:     "customer",
		Content:        "你们这个项目多少人用",
		EventID:        "mh:real-cust-1",
		ConversationID: conv,
		Extra:          map[string]interface{}{"account_id": account},
	}
	decision, err := svc.interceptInbound(ctx, event)
	if err != nil {
		t.Fatalf("interceptInbound 失败: %v", err)
	}
	if decision != nil && decision.Blocked {
		t.Fatalf("真实客户消息不应被拦截, got: %+v", decision)
	}
}

// 跨账号:同内容但不同 account_id,不应被本账号的 outbound 误判为回环
func TestInboxIngress_DifferentAccount_NotBlockedByContentEcho(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	svc := NewInboxIngressServiceWithDB(db, newIsolatedCacheForTest(t))
	ctx := context.Background()

	const (
		platform   = "xiaohongshu"
		accountA   = "acct-A"
		accountB   = "acct-B"
		conv       = "conv-cross-account"
		sharedText = "HiveMTK 支持私有化部署,云服务器也可以"
	)
	if err := db.Create(&model.MessageHub{
		MsgID:          "mh:ob-A",
		Platform:       platform,
		AccountID:      accountA,
		Direction:      "outbound",
		Status:         "delivered",
		MsgType:        "text",
		ConversationID: conv,
		SenderID:       accountA,
		Content:        sharedText,
		SentAt:         time.Now().Add(-10 * time.Second),
	}).Error; err != nil {
		t.Fatalf("插入 accountA outbound 失败: %v", err)
	}

	event := &model.MessageEvent{
		Channel:        platform,
		SenderID:       conv,
		SenderName:     "客户X",
		SenderType:     "customer",
		Content:        sharedText,
		EventID:        "mh:cross-1",
		ConversationID: conv,
		Extra:          map[string]interface{}{"account_id": accountB},
	}
	decision, err := svc.interceptInbound(ctx, event)
	if err != nil {
		t.Fatalf("interceptInbound 失败: %v", err)
	}
	if decision != nil && decision.Blocked {
		t.Fatalf("跨账号同内容不应被拦截, got: %+v", decision)
	}
}

// 跨会话:同账号同内容不同 conv,真实场景=AI 模板话术被多个不同客户触发,新会话的 patrol 抓回
// 不应被旧会话的 outbound 误判为回环(否则新客户首条消息永远进不来)
func TestInboxIngress_DifferentConversation_NotBlockedByContentEcho(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	svc := NewInboxIngressServiceWithDB(db, newIsolatedCacheForTest(t))
	ctx := context.Background()

	const (
		platform  = "xiaohongshu"
		account   = "acct-shared-template"
		convOld   = "conv-old"
		convNew   = "conv-new"
		templText = "您好！很高兴为您服务,请问有什么可以帮您？"
	)
	if err := db.Create(&model.MessageHub{
		MsgID:          "mh:ob-templ",
		Platform:       platform,
		AccountID:      account,
		Direction:      "outbound",
		Status:         "delivered",
		MsgType:        "text",
		ConversationID: convOld,
		SenderID:       account,
		Content:        templText,
		SentAt:         time.Now().Add(-10 * time.Second),
	}).Error; err != nil {
		t.Fatalf("插入 templ outbound 失败: %v", err)
	}

	event := &model.MessageEvent{
		Channel:        platform,
		SenderID:       convNew,
		SenderName:     "新客户Y",
		SenderType:     "customer",
		Content:        templText,
		EventID:        "mh:new-1",
		ConversationID: convNew,
		Extra:          map[string]interface{}{"account_id": account},
	}
	decision, err := svc.interceptInbound(ctx, event)
	if err != nil {
		t.Fatalf("interceptInbound 失败: %v", err)
	}
	if decision != nil && decision.Blocked {
		t.Fatalf("跨会话同模板不应被拦截(新客户首条消息), got: %+v", decision)
	}
}

// 时间窗:2h 之前的 outbound 不应触发(避免无限期拦所有历史内容)
func TestInboxIngress_OldOutbound_NotBlockedByContentEcho(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	svc := NewInboxIngressServiceWithDB(db, newIsolatedCacheForTest(t))
	ctx := context.Background()

	const (
		platform  = "xiaohongshu"
		account   = "acct-old"
		conv      = "conv-old"
		templText = "AI 早上的回复"
	)
	if err := db.Create(&model.MessageHub{
		MsgID:          "mh:ob-old",
		Platform:       platform,
		AccountID:      account,
		Direction:      "outbound",
		Status:         "delivered",
		MsgType:        "text",
		ConversationID: conv,
		SenderID:       account,
		Content:        templText,
		SentAt:         time.Now().Add(-3 * time.Hour),
	}).Error; err != nil {
		t.Fatalf("插入 old outbound 失败: %v", err)
	}

	event := &model.MessageEvent{
		Channel:        platform,
		SenderID:       conv,
		SenderName:     "客户Z",
		SenderType:     "customer",
		Content:        templText,
		EventID:        "mh:re-cust",
		ConversationID: conv,
		Extra:          map[string]interface{}{"account_id": account},
	}
	decision, err := svc.interceptInbound(ctx, event)
	if err != nil {
		t.Fatalf("interceptInbound 失败: %v", err)
	}
	if decision != nil && decision.Blocked {
		t.Fatalf("2h 外的 outbound 不应触发回环拦截, got: %+v", decision)
	}
}

// 端到端:HandleIngressMessage 对回环消息必须 Accepted=true 但 QueuedForAI=false
func TestInboxIngress_HandleIngress_SelfEcho_NotQueuedForAI(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	svc := NewInboxIngressServiceWithDB(db, newIsolatedCacheForTest(t))
	ctx := context.Background()

	const (
		platform = "xiaohongshu"
		account  = "acct-e2e"
		conv     = "conv-e2e"
	)
	outContent := "AI 回复:欢迎咨询 HiveMTK"
	if err := db.Create(&model.MessageHub{
		MsgID:          "mh:ob-e2e",
		Platform:       platform,
		AccountID:      account,
		Direction:      "outbound",
		Status:         "delivered",
		MsgType:        "text",
		ConversationID: conv,
		SenderID:       account,
		SenderName:     "",
		Content:        outContent,
		SentAt:         time.Now().Add(-5 * time.Second),
	}).Error; err != nil {
		t.Fatalf("插入 outbound 失败: %v", err)
	}

	event := &model.MessageEvent{
		Channel:        platform,
		SenderID:       conv,
		SenderName:     "客户E2E",
		SenderType:     "customer",
		Content:        outContent,
		EventID:        "mh:loop-e2e",
		ConversationID: conv,
		Extra:          map[string]interface{}{"account_id": account},
	}
	result, err := svc.HandleIngressMessage(ctx, event)
	if err != nil {
		t.Fatalf("HandleIngressMessage 失败: %v", err)
	}
	if !result.Accepted {
		t.Fatalf("回环消息应被 accepted(GetByMsgID/getByDedup 幂等命中), got: %+v", result)
	}
	if result.QueuedForAI {
		t.Fatalf("回环消息绝对不能 QueuedForAI=true(否则 AI 再次被自己触发), got: %+v", result)
	}

	var count int64
	if err := db.Model(&model.MessageHub{}).
		Where("account_id = ? AND conversation_id = ? AND direction = 'inbound' AND msg_id = ?",
			account, conv, "mh:loop-e2e").
		Count(&count).Error; err != nil {
		t.Fatalf("查询 inbound 失败: %v", err)
	}
	if count > 0 {
		t.Fatalf("回环消息不应被持久化为 inbound, 实际 %d 条", count)
	}
}

// 多气泡拼接回声：AI 回复被拆成多条气泡发送，扩展把相邻气泡拼成一整条抓回。
// 入站归一化后剥掉全部近期出站几乎不剩内容 → 判回声拦截。
func TestInboxIngress_EchoBubbleConcatenation_Blocked(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	svc := NewInboxIngressServiceWithDB(db, newIsolatedCacheForTest(t))
	ctx := context.Background()

	const (
		platform = "douyin"
		account  = "acct-bubble-1"
		conv     = "conv-bubble-1"
	)
	outA := "收到！武汉的业主您好，方便告诉我您的建筑面积和全包半包想法吗？"
	outB := "还有一个关键问题：您是想全包还是半包？全包省心点，半包自己掌控主材，价格差挺多的~"
	for i, c := range []string{outA, outB} {
		if err := db.Create(&model.MessageHub{
			MsgID:          "mh:ob-bubble-" + string(rune('0'+i)),
			Platform:       platform,
			AccountID:      account,
			Direction:      "outbound",
			Status:         "delivered",
			MsgType:        "text",
			ConversationID: conv,
			SenderID:       account,
			Content:        c,
			SentAt:         time.Now().Add(-30 * time.Second),
		}).Error; err != nil {
			t.Fatalf("插入 outbound 失败: %v", err)
		}
	}

	echoContent := outA + "，" + outB
	event := &model.MessageEvent{
		Channel:        platform,
		SenderID:       conv,
		SenderName:     "抖音客户",
		SenderType:     "customer",
		Content:        echoContent,
		EventID:        "mh:echo-bubble-1",
		ConversationID: conv,
		Extra:          map[string]interface{}{"account_id": account},
	}
	decision, err := svc.interceptInbound(ctx, event)
	if err != nil {
		t.Fatalf("interceptInbound 失败: %v", err)
	}
	if decision == nil || !decision.Blocked || !decision.IsSelfEcho {
		t.Fatalf("拼接回声应被拦截, got: %+v", decision)
	}
	if !strings.Contains(decision.Reason, "recent outbound") {
		t.Fatalf("拦截原因应包含 recent outbound, got: %q", decision.Reason)
	}
}

// 客户引用我们刚说的话再追问：剥离出站后剩余的是客户自己的问题 → 必须放行。
func TestInboxIngress_CustomerQuotePlusQuestion_NotBlocked(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	svc := NewInboxIngressServiceWithDB(db, newIsolatedCacheForTest(t))
	ctx := context.Background()

	const (
		platform = "douyin"
		account  = "acct-quote-1"
		conv     = "conv-quote-1"
	)
	outContent := "全包的话大概每平一千二到一千五，具体要量房之后才能给您出准确报价单"
	if err := db.Create(&model.MessageHub{
		MsgID:          "mh:ob-quote-1",
		Platform:       platform,
		AccountID:      account,
		Direction:      "outbound",
		Status:         "delivered",
		MsgType:        "text",
		ConversationID: conv,
		SenderID:       account,
		Content:        outContent,
		SentAt:         time.Now().Add(-20 * time.Second),
	}).Error; err != nil {
		t.Fatalf("插入 outbound 失败: %v", err)
	}

	customerMsg := outContent + " 那半包呢？能便宜多少？"
	event := &model.MessageEvent{
		Channel:        platform,
		SenderID:       conv,
		SenderName:     "抖音客户",
		SenderType:     "customer",
		Content:        customerMsg,
		EventID:        "mh:quote-question-1",
		ConversationID: conv,
		Extra:          map[string]interface{}{"account_id": account},
	}
	decision, err := svc.interceptInbound(ctx, event)
	if err != nil {
		t.Fatalf("interceptInbound 失败: %v", err)
	}
	if decision != nil && decision.Blocked && decision.IsSelfEcho {
		t.Fatalf("引用追问不应被当回声拦截, got: %+v", decision)
	}
}
