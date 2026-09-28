package service

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// 承诺 18 的批次半边：批次路径 handleIngressSingleForBatch 里那份 Blocked 分支
// （inbox_ingress.go:980-997）与单条路径同规则——dup 判定要落库留痕、只压 AI，
// 什么都不落的只有自己的出站回声。
//
// 为什么单独立一条：单条那半边有腿（inbox_ingress_dedup_scope_test.go:317
// TestHandleIngress_SelfEchoDecisionPersistsNothing 走 HandleIngressMessage），
// 批里那份此前从未被回声事件穿过：批里唯一的正向腿 TestHandleIngressBatch_DupDecisionPersistsWithAISuppressed
// 断的是「dup 要落库」，把 :991 的守卫摘掉（改成 `if true`，回声也落库）它照样成立
// ⇒ 单条半边绿、批里把自己的话存两遍没人拦。本用例补的就是这一格，并且两半都钉：
// 回声不落库（守卫摘掉即红）+ 批里非回声的 Blocked 判定照样落库（守卫反方向写死即红），
// 免得新腿只在靶子上红。
//
// 回环的「因」走真识别路径：预置一条同会话近期 outbound（r23SeedOutbound），入站正文与它
// 精确同文 ⇒ interceptInbound 的 platform+sender_name+content 命中（inbox_ingress_ingest.go:127），
// 判 IsSelfEcho。这里刻意**不**手工塞 Extra["channel_msg_id"]：塞了就是在验夹具而不是验判定。

func TestHandleIngressBatch_SelfEchoPersistsNothingAndDupPersists(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	svc := NewInboxIngressServiceWithDB(db, newIsolatedCacheForTest(t))
	ctx := context.Background()

	const platform = "xiaohongshu"
	nonce := fmt.Sprintf("batchecho-%d", time.Now().UnixNano())
	account := "acct-" + nonce

	// ---- ① 回声那一半：批入口收到自己出站的原话，必须什么都不落 ----
	echoConv := "conv-r23-batch-echo"
	echoContent := "这是 AI 已经发出去的话 " + nonce
	r23SeedOutbound(t, db, platform, account, echoConv, echoContent, "mh-r23-batch-out",
		time.Now().Add(-10*time.Second))

	echoEvt := r23Event(nonce, "evt-r23-batch-echo-in", echoConv, echoContent)
	if _, ok := echoEvt.Extra["channel_msg_id"]; ok {
		t.Fatalf("夹具失效：本腿必须走真回环识别，不能借 Extra[channel_msg_id] 命中")
	}
	batch, err := svc.HandleIngressBatch(ctx, []*model.MessageEvent{echoEvt})
	if err != nil {
		t.Fatalf("HandleIngressBatch(回声): %v", err)
	}
	per := batch.PerEvent[0]
	if per == nil {
		t.Fatalf("批里没交回这一条的判定: %+v", batch)
	}
	if !strings.Contains(per.Reason, "self-echo") {
		t.Fatalf("前置没满足：这一条根本没被判成回声（Reason=%q）⇒ 后面「不落库」的断言什么也没测",
			per.Reason)
	}
	if !per.Accepted || per.QueuedForAI {
		t.Errorf("回声应 Accepted 且不进 AI: %+v", per)
	}
	if batch.TriggeredAI {
		t.Errorf("回声不得让批合并触发 AI（自己触发自己）: %+v", batch)
	}
	var echoInbound int64
	if err := db.Model(&model.MessageHub{}).
		Where("platform = ? AND conversation_id = ? AND direction = 'inbound'", platform, echoConv).
		Count(&echoInbound).Error; err != nil {
		t.Fatalf("统计回声会话 inbound 行数: %v", err)
	}
	if echoInbound != 0 {
		t.Errorf("§8.3-18 批次半边未达成：出站回声在批里又被当成客户消息存了一遍（inbound=%d，Reason=%q）"+
			"⇒ 单条不落、批里存两遍，同一句 AI 话术在会话里出现两次", echoInbound, per.Reason)
	}

	// ---- ② 反向半边：批里「被判重复」的那一条不是回声，必须照样落库 ----
	dupConv := "conv-r23-batch-dup"
	dupContent := "客户在批里重复说的那句 " + nonce
	if _, err := svc.HandleIngressMessage(ctx, r23Event(nonce, "evt-r23-batch-dup-1", dupConv, dupContent)); err != nil {
		t.Fatalf("首条 HandleIngressMessage: %v", err)
	}
	dupBatch, err := svc.HandleIngressBatch(ctx, []*model.MessageEvent{
		r23Event(nonce, "evt-r23-batch-dup-2", dupConv, dupContent),
	})
	if err != nil {
		t.Fatalf("HandleIngressBatch(重复): %v", err)
	}
	dupPer := dupBatch.PerEvent[0]
	if !strings.Contains(dupPer.Reason, "dup=true") {
		t.Fatalf("前置没满足：第二条没被判成内容窗口重复（Reason=%q）⇒ 落库断言落空", dupPer.Reason)
	}
	var dupRows int64
	if err := db.Model(&model.MessageHub{}).
		Where("platform = ? AND conversation_id = ? AND direction = 'inbound' AND msg_id = ?",
			platform, dupConv, "evt-r23-batch-dup-2").
		Count(&dupRows).Error; err != nil {
		t.Fatalf("统计重复留痕行数: %v", err)
	}
	if dupRows != 1 {
		t.Errorf("批里的重复判定必须留痕（客户真说过这句话），实际 inbound=%d, Reason=%q"+
			"⇒ 把守卫写成「Blocked 一律不落」＝工作台查不到的「说了没回」", dupRows, dupPer.Reason)
	}
	if dupPer.QueuedForAI || dupBatch.TriggeredAI {
		t.Errorf("重复句不得进 AI（同一句回两遍）: per=%+v batch=%+v", dupPer, dupBatch)
	}
}
