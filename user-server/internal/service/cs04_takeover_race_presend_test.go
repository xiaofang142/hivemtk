// cs04_takeover_race_presend_test.go CS-04 刀1：接管与已启动 AI 并发的**发送前核对**。
//
// 生产窗口（webhook_ai.go runAIGeneration）：AI 生成是异步 goroutine——入站判锁
// （inbox_ingress.go :416 humanLocked）通过后 AI 已在生成；这期间坐席接管
// （CustomerSessionService.TakeoverByAgent → handler=human + LockSessionForHuman
// 人工锁）。生成完成后 runAIGeneration 直接 sendOutbound——修复前发送前无人复核
// 人工锁，AI 抢答（客户在人工持有期收到 AI 文案）。本文件钉住：实际发送前必须
// 复核人工锁，持有期出站 0 条、锁与坐席状态不被出站链改写。
//
// 并发窗口的确定性构造：AI 腿就是生产实体 runAIGeneration（webhook_ai.go:290 同一
// goroutine 体，测试里 go + done channel 包裹只为可等待），在其内部 SalesEngine.Handle
// 入口用 salesEngineHandleFault 接缝（sales_engine.go:267，生产恒 nil）阻塞——此时
// findOrCreateSession 已落库、出站点未到，坐席接管落进「AI 已在生成」的中段；放行后
// 发送点的复核就是被测行为。全程无 sleep 校时，时序零抖动。
//
// 会话 ID 口径：webhook 路径 in.OneID = hubMsg.SenderID（webhook_ai.go:261，无渠道前缀），
// 稳定 ID = sess_<platform>_<account>_<oneID>（UpsertByOneID）；与入站侧派生的
// channel:sender 是两把不同的键——接管锁持有的是前者，这是 CS-04 缺口④的显式口径。
//
// 复用 CS-01 三样本冻结字段（cs01- 前缀），只 fake 模型/检索器，锁/会话/出站走真实链。
package service

import (
	"hivemtk-user/internal/pkg/tenant"
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
)

// cs04RacingIntent 正常置信意图（AI 会走生成→回复路径，制造并发窗口）。
type cs04RacingIntent struct{}

func (cs04RacingIntent) Recognize(context.Context, string, string, string) (*dto.RecognizeResult, error) {
	return &dto.RecognizeResult{IntentType: IntentPriceInquiry, IntentName: "价格咨询", Confidence: 0.92}, nil
}

// cs04WaitFor 等信号，超时 fatal（测试自身纪律：宁可红不可挂）。
func cs04WaitFor(t *testing.T, what string, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
		return
	case <-time.After(5 * time.Second):
		t.Fatalf("等待 %s 超时（AI 腿未按预期推进）", what)
	}
}

func TestCS04_TakeoverDuringAIGeneration_PreSendLockCheck(t *testing.T) {
	rag := &fakeRAGSearcher{} // 无知识命中：走高置信直答，最快到发送点
	ws, _, db := c01SetupAI(t, rag, cs04RacingIntent{})
	ctx := tenant.WithTenantID(context.Background(), c01TenantA)

	// —— 格①：AI 腿启动（无锁放行）→ 生成中段坐席接管 → 发送点复核拦下。
	// 接缝只在本格持有；放行后立即摘除，格②不受影响。
	gate := make(chan struct{})
	entered := make(chan struct{})
	SetSalesEngineHandleFault(func(*dto.SalesRequest) error {
		close(entered)
		<-gate
		return nil
	})
	defer SetSalesEngineHandleFault(nil)

	conv1, sender1, evt1 := "cs04-conv-race-1", "cs04-openid-race-1", "cs04-evt-race-1"
	hubMsg := c01InboundHub(evt1, conv1, sender1, c01AccountA1, c01Question)
	p := &ParsedPayload{EventID: evt1, Sender: sender1, Content: c01Question}
	releaseReplyClaimForTest(t, evt1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ws.runAIGeneration(ctx, ChannelDouyin, c01AccountA1, p, hubMsg, &IncomingContext{
			Platform: model.Platform(c01Channel), AccountID: c01AccountA1,
			SenderID: sender1, OneID: sender1, Content: c01Question, MessageID: evt1,
		}, nil)
	}()
	cs04WaitFor(t, "AI 腿进入引擎（生成窗口开启）", entered)

	// 生成中段接管：走生产路径 CustomerSessionService.TakeoverByAgent
	// （handler=human + LockSessionForHuman 人工锁）。
	// 会话 ID 口径：OneID=sender（webhook_ai.go:261），稳定 ID 无渠道前缀。
	svc := NewCustomerSessionService()
	var sess *model.CustomerSession
	var sessErr error
	deadline := time.Now().Add(5 * time.Second)
	for {
		sess, sessErr = svc.sessionRepo.GetBySessionID(ctx, "sess_"+c01Channel+"_"+c01AccountA1+"_"+sender1)
		if sessErr == nil && sess != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("AI 腿应已建会话: %v", sessErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
	agent := &model.AgentStatus{AgentID: 7101, AgentName: "cs04-坐席", Status: "online", MaxSessions: 5, ActiveSessions: 0}
	if err := svc.agentRepo.Create(ctx, agent); err != nil {
		t.Fatalf("登记坐席失败: %v", err)
	}
	if err := svc.TakeoverByAgent(ctx, &TakeoverRequest{SessionID: sess.ID, AgentID: 7101, Reason: "cs04 并发接管"}); err != nil {
		t.Fatalf("接管失败: %v", err)
	}

	// 锁状态自证：接管链路确实把人工锁上了（TakeoverByAgent→lockHumanSession）。
	locked, lerr := NewInboxIngressService().IsSessionHumanLocked(ctx, sess.SessionID)
	if lerr != nil {
		t.Fatalf("读锁失败: %v", lerr)
	}
	if !locked {
		t.Fatal("前置：接管后人工锁应持有（否则本格测的不是并发窗口）")
	}

	// 放行 → 编排器收尾 → 发送点（被测核对点）→ AI 腿整体结束。
	// 放行前先摘接缝：格②同进程还要走一遍 Handle，seam 残留会对 entered 二次 close panic。
	SetSalesEngineHandleFault(nil)
	close(gate)
	<-done

	var out []*model.MessageHub
	if err := db.Where("conversation_id = ? AND direction = ?", conv1, "outbound").Find(&out).Error; err != nil {
		t.Fatalf("读出站失败: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("人工持有期 AI 应 0 外发（发送前核对），实际 %d 条: %+v", len(out), out)
	}

	// 锁与会话状态不被发送链改写。
	stillLocked, _ := NewInboxIngressService().IsSessionHumanLocked(ctx, sess.SessionID)
	if !stillLocked {
		t.Error("发送链不得释放人工锁")
	}
	got, _ := svc.sessionRepo.GetByID(ctx, sess.ID)
	if got != nil && got.HandlerType != model.HandlerTypeHuman {
		t.Errorf("接管态不得被发送链改写: handler=%v", got.HandlerType)
	}

	// —— 格②（对照）：同链路、未接管 → 照常出站恰 1 条（抑制来源=锁复核非链路断）。
	conv2, sender2, evt2 := "cs04-conv-race-2", "cs04-openid-race-2", "cs04-evt-race-2"
	hubMsg2 := c01InboundHub(evt2, conv2, sender2, c01AccountA1, c01Question)
	p2 := &ParsedPayload{EventID: evt2, Sender: sender2, Content: c01Question}
	releaseReplyClaimForTest(t, evt2)
	ws.runAIGeneration(ctx, ChannelDouyin, c01AccountA1, p2, hubMsg2, &IncomingContext{
		Platform: model.Platform(c01Channel), AccountID: c01AccountA1,
		SenderID: sender2, Content: c01Question, MessageID: evt2,
	}, nil)

	var out2 []*model.MessageHub
	if err := db.Where("conversation_id = ? AND direction = ?", conv2, "outbound").Find(&out2).Error; err != nil {
		t.Fatalf("对照轮读出站失败: %v", err)
	}
	if len(out2) != 1 {
		t.Errorf("未接管对照轮应照常出站恰 1 条，实际 %d", len(out2))
	}
	locked2, _ := NewInboxIngressService().IsSessionHumanLocked(ctx, "sess_"+c01Channel+"_"+c01AccountA1+"_"+sender2)
	if locked2 {
		t.Error("对照轮不应有人工锁（锁只来自接管链）")
	}
}
