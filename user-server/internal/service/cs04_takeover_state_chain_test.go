// cs04_takeover_state_chain_test.go CS-04 刀2：接管态全链路状态机。
//
// 缺口②：锁语义 4 用例、接管 12 用例、释放幂等各有单格，但「无锁→接管→访客再发→
// 坐席回→释放→访客再发」整链没人一次走通过——单格各自正确不代表链接缝正确
// （例：接管把 handler=human 写库 + 人工锁写缓存是两条不同介质的写，释放必须同时
// 归还两条；只测单格看不到释放后访客再发会不会仍被判锁、AI 触发会不会恢复）。
// 本文件钉住整链六拍的每拍状态增量，全部走生产入口：
//
//	拍1 无锁：入站 accepted+queued_for_ai，AI 触发恰 1 次（TriggerInboundAI 被调）
//	拍2 接管：handler=human / status=human_handling / 人工锁持有 / 坐席 ActiveSessions+1；
//	    重复接管幂等（计数不翻倍）
//	拍3 访客再发（人工持有期）：入站按 event.SessionID 判锁 bypass AI——落库、不触发
//	    （QueuedForAI=false，HumanLocked=true）。锁键口径：本格让入站 SessionID 与
//	    会话行 session_id 同形（webchat 等入口 session_id 即入站键），渠道漏斗的双键
//	    形态（channel:sender vs sess_平台_账号_oneID）归刀4 显式声明，不在此重复
//	拍4 坐席回：session_messages agent 行落库 / human_reply_count+1 / FirstHumanReplyAt 首次落
//	拍5 释放：handler=ai / status=waiting / AgentID 归零 / 锁解除 / ActiveSessions-1（与拍2
//	    对称）；重复释放可读报错、计数不再扣
//	拍6 访客再发（AI 托管期）：queued_for_ai 恢复，AI 触发第 2 次（恰 +1 增量）
//
// 复用 CS-01 冻结字段与 fakeAITrigger（入站侧），锁/会话/坐席/消息走真实链与真库。
package service

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// releaseAfterAITrigger 在 fake 腿返回后释放 AI 处理中排他标记——生产里该标记由
// runAIGeneration→sendOutbound 尾部 ReleaseAIProcessingFlag 释放（webhook_outbound.go
// defer 收尾），fake 腿不走那条链，这里等价模拟收尾。
type releaseAfterAITrigger struct {
	inner   *fakeAITrigger
	ingress *InboxIngressService
	conv    string
}

func (r *releaseAfterAITrigger) TriggerInboundAI(ctx context.Context, channel, accountID, conversationID, customerID, content, eventID string, opts ...TriggerInboundOption) {
	r.inner.TriggerInboundAI(ctx, channel, accountID, conversationID, customerID, content, eventID, opts...)
	r.ingress.ReleaseAIProcessingFlag(ctx, r.conv)
}

func TestCS04_TakeoverStateChain_FullJourney(t *testing.T) {
	// 库先行：仓储构造时即捕获 _db.GetDB()（dbstate），必须 NewTestDB 之后建服务。
	// 黑名单表在列：CreateSession 入口有 preCreateBlacklistGuard。
	testutil.NewTestDB(t,
		&model.MessageHub{}, &model.InboxConversation{},
		&model.CustomerSession{}, &model.SessionMessage{},
		&model.AgentStatus{}, &model.Customer{}, &model.UserBlacklist{},
		&model.AISuggestion{}, &model.QuickReply{}, &model.SessionTag{},
	)
	// 入站侧接线同生产：SetAITrigger(fakeAITrigger)——拍1/3/6 的 AI 触发计数即生产
	// 里 TriggerInboundAI→runAIGeneration 的启动点；本链测的是「触发不触发」，
	// AI 腿内部（发送前核对）已由 cs04_takeover_race_presend_test 钉住，不进本链。
	// fake 腿不走真实 runAIGeneration→sendOutbound 收尾，AI 处理中排他标记
	// （InboxAIProcessingKey，TTL 2min）须由 fake 释放——生产里该标记正是在
	// sendOutbound 尾部 ReleaseAIProcessingFlag，这里用 releaseAfterAITrigger 等价
	// 模拟，否则拍6 会被拍1 残留的标记误判「AI 已在进行中」。
	ingress := NewInboxIngressService()
	inner := &fakeAITrigger{}
	svc := NewCustomerSessionService()
	ctx := context.Background()

	conv := "cs04-conv-chain-1"
	// 键口径（拍3 见注释）：session_id 取入站派生形（channel:sender），全链同一把键。
	sender := "cs04-openid-chain-1"
	sessionKey := c01Channel + ":" + sender

	tr := &releaseAfterAITrigger{inner: inner, ingress: ingress, conv: conv}
	ingress.SetAITrigger(tr)
	t.Cleanup(func() { ingress.ReleaseAIProcessingFlag(context.Background(), conv) })

	// —— 拍1 无锁入站：AI 触发恰 1 次 ——
	ev1 := c01DouyinSampleEvent("cs04-evt-chain-1", conv, sender, c01AccountA1, c01Question)
	res1, err := ingress.HandleIngressMessage(ctx, ev1)
	if err != nil {
		t.Fatalf("拍1 入站失败: %v", err)
	}
	if !res1.Accepted || !res1.QueuedForAI {
		t.Fatalf("拍1 应 accepted+queued_for_ai，实际 %+v", res1)
	}
	if inner.called != 1 {
		t.Fatalf("拍1 AI 触发应恰 1 次，实际 %d", inner.called)
	}
	// 会话由编排器建立——生产里 TriggerInboundAI 是异步的，本链直接建同形会话
	// （拍1 契约=入站放行+触发计数；会话行存在性归 CS-01/刀1 已钉），保证接管/
	// 回/释放三拍操作的就是入站派生的那条会话。
	sess, err := svc.CreateSession(ctx, &CreateSessionRequest{
		Platform:  model.Platform(c01Channel),
		AccountID: c01AccountA1,
		UserID:    sender,
		OneID:     sender,
	})
	if err != nil {
		t.Fatalf("建会话失败: %v", err)
	}
	// 对齐键口径 + AI 托管初态：入站派生 session_id 写回会话行（webchat 入口本就
	// 同形；渠道漏斗的 sess_ 稳定形由编排器路径建立，双形口径声明归刀4）。
	// CreateSession 自身不落 handler_type（编排器 upsert 才写 "ai"），这里补齐
	// 生产编排器建立会话后的初态。
	if err := svc.sessionRepo.Update(ctx, &model.CustomerSession{
		ID: sess.ID, SessionID: sessionKey, Platform: sess.Platform, AccountID: sess.AccountID,
		UserID: sess.UserID, OneID: sess.OneID, Status: sess.Status, Priority: sess.Priority,
		HandlerType: model.HandlerTypeAI, CreatedAt: sess.CreatedAt, UpdatedAt: sess.UpdatedAt,
	}); err != nil {
		t.Fatalf("对齐 session_id 失败: %v", err)
	}
	if got, _ := svc.GetSessionByID(ctx, sess.ID); got.HandlerType != model.HandlerTypeAI {
		t.Fatalf("拍1 会话应 AI 托管（编排器建立后的初态），实际 %v", got.HandlerType)
	}

	// —— 拍2 接管：库态 + 锁 + 坐席计数 ——
	agent := &model.AgentStatus{AgentID: 7201, AgentName: "cs04-链坐席", Status: "online", MaxSessions: 5, ActiveSessions: 0}
	if err := svc.agentRepo.Create(ctx, agent); err != nil {
		t.Fatalf("登记坐席失败: %v", err)
	}
	if err := svc.TakeoverByAgent(ctx, &TakeoverRequest{SessionID: sess.ID, AgentID: 7201, Reason: "cs04 链-接管"}); err != nil {
		t.Fatalf("拍2 接管失败: %v", err)
	}
	got2, _ := svc.GetSessionByID(ctx, sess.ID)
	if got2.HandlerType != model.HandlerTypeHuman || got2.Status != model.SessionStatusHumanHandling || got2.AgentID != 7201 {
		t.Fatalf("拍2 接管态不符: handler=%v status=%v agent=%d", got2.HandlerType, got2.Status, got2.AgentID)
	}
	locked2, _ := ingress.IsSessionHumanLocked(ctx, sessionKey)
	if !locked2 {
		t.Fatal("拍2 人工锁应持有")
	}
	ag2, _ := svc.agentRepo.GetByAgentID(ctx, 7201)
	if ag2.ActiveSessions != 1 {
		t.Fatalf("拍2 坐席 ActiveSessions 应 1，实际 %d", ag2.ActiveSessions)
	}
	// 幂等附注：重复接管不重复计数（handler/status 仍 human 态，ActiveSessions 仍 1）。
	if err := svc.TakeoverByAgent(ctx, &TakeoverRequest{SessionID: sess.ID, AgentID: 7201, Reason: "cs04 链-重复接管"}); err != nil {
		t.Fatalf("拍2 重复接管应幂等成功，实际报错: %v", err)
	}
	ag2b, _ := svc.agentRepo.GetByAgentID(ctx, 7201)
	if ag2b.ActiveSessions != 1 {
		t.Errorf("拍2 重复接管后 ActiveSessions 应仍 1，实际 %d", ag2b.ActiveSessions)
	}

	// —— 拍3 人工持有期访客再发：bypass AI（落库不触发）——
	ev3 := c01DouyinSampleEvent("cs04-evt-chain-3", conv, sender, c01AccountA1, "转人工了还有人吗")
	res3, err := ingress.HandleIngressMessage(ctx, ev3)
	if err != nil {
		t.Fatalf("拍3 入站失败: %v", err)
	}
	if !res3.Accepted || res3.QueuedForAI || !res3.HumanLocked {
		t.Fatalf("拍3 应 accepted+human_locked+不触发 AI，实际 %+v", res3)
	}
	if inner.called != 1 {
		t.Errorf("拍3 AI 触发应仍 1（锁拦截），实际 %d", inner.called)
	}

	// —— 拍4 坐席回：消息行 + 计数 + 首回时间 ——
	if _, err := svc.SendMessage(ctx, &SendMessageRequest{
		SessionID:  sessionKey,
		Content:    "您好，人工客服已上线，请问有什么可以帮您？",
		SenderType: "agent",
		SenderID:   "7201",
		SenderName: "cs04-链坐席",
	}); err != nil {
		t.Fatalf("拍4 坐席回复失败: %v", err)
	}
	got4, _ := svc.GetSessionByID(ctx, sess.ID)
	if got4.HumanReplyCount != 1 {
		t.Fatalf("拍4 human_reply_count 应 1，实际 %d", got4.HumanReplyCount)
	}
	if got4.FirstHumanReplyAt == nil {
		t.Error("拍4 FirstHumanReplyAt 应首次落值")
	}

	// —— 拍5 释放：库态还原 + 锁解除 + 计数对称 ——
	if err := svc.ReleaseToAI(ctx, &ReleaseToAIRequest{SessionID: sess.ID, AgentID: 7201}); err != nil {
		t.Fatalf("拍5 释放失败: %v", err)
	}
	got5, _ := svc.GetSessionByID(ctx, sess.ID)
	if got5.HandlerType != model.HandlerTypeAI || got5.Status != model.SessionStatusWaiting || got5.AgentID != 0 {
		t.Fatalf("拍5 释放态不符: handler=%v status=%v agent=%d", got5.HandlerType, got5.Status, got5.AgentID)
	}
	locked5, _ := ingress.IsSessionHumanLocked(ctx, sessionKey)
	if locked5 {
		t.Fatal("拍5 人工锁应解除")
	}
	ag5, _ := svc.agentRepo.GetByAgentID(ctx, 7201)
	if ag5.ActiveSessions != 0 {
		t.Fatalf("拍5 ActiveSessions 应归还为 0，实际 %d", ag5.ActiveSessions)
	}
	// 重复释放：会话已无持有坐席，可读报错（不是误吞），也不会再扣计数。
	if err := svc.ReleaseToAI(ctx, &ReleaseToAIRequest{SessionID: sess.ID, AgentID: 7201}); err == nil {
		t.Error("拍5 重复释放应报错（会话已切回 AI）")
	}
	ag5b, _ := svc.agentRepo.GetByAgentID(ctx, 7201)
	if ag5b.ActiveSessions != 0 {
		t.Errorf("拍5 重复释放后 ActiveSessions 应仍 0，实际 %d", ag5b.ActiveSessions)
	}

	// —— 拍6 AI 托管期访客再发：恢复触发（恰 +1）——
	ev6 := c01DouyinSampleEvent("cs04-evt-chain-6", conv, sender, c01AccountA1, "还有问题要咨询")
	res6, err := ingress.HandleIngressMessage(ctx, ev6)
	if err != nil {
		t.Fatalf("拍6 入站失败: %v", err)
	}
	if !res6.Accepted || !res6.QueuedForAI {
		t.Fatalf("拍6 应恢复 queued_for_ai，实际 %+v", res6)
	}
	if inner.called != 2 {
		t.Errorf("拍6 AI 触发应累计 2（1→2 增量），实际 %d", inner.called)
	}
	// 链尾副作用总账：整链只接管过一次、只回过一次，计数不得重复。
	agEnd, _ := svc.agentRepo.GetByAgentID(ctx, 7201)
	if agEnd.ActiveSessions != 0 {
		t.Errorf("链尾坐席计数应 0（接管+1/释放-1 恰一对），实际 %d", agEnd.ActiveSessions)
	}
	if gotEnd, _ := svc.GetSessionByID(ctx, sess.ID); gotEnd.HumanReplyCount != 1 {
		t.Errorf("链尾 human_reply_count 应仍 1，实际 %d", gotEnd.HumanReplyCount)
	}
}
