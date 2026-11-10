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

// 2026-11-10 审计 F1 端到端：同一客户在同一会话连发两张**不同**图（content 同为
// 「[图片]」占位符、msg_type=image、裸 typed event_id 无 #N 后缀）——两张都必须
// accepted + queued（各自触发/排队 AI），第二张不得被内容窗口键判重吞掉。
// 与 TestCS02_DupWindow_ReasonRowAndTriggerCount（同内容重复仍判重）互补。
// 真实 PG 8217 门控：与 cs02 矩阵同一测试库姿态（无库时 Skip，CI/本地起库后跑）。
func TestIngress_TwoDistinctImagesWithinWindowBothQueued(t *testing.T) {
	_ = testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	svc := NewInboxIngressService()
	tr := &fakeAITrigger{}
	svc.SetAITrigger(tr)
	ctx := context.Background()

	nonce := fmt.Sprintf("f1img-%d", time.Now().UnixNano())
	conv := "conv-f1-img"
	makeImg := func(eventID, url string) *model.MessageEvent {
		return &model.MessageEvent{
			Channel:        model.ChannelXHS,
			SenderID:       "customer-" + nonce,
			SenderType:     "customer",
			Content:        "[图片]",
			EventID:        eventID,
			ConversationID: conv,
			MsgType:        model.MsgTypeImage,
			MediaURL:       url,
			Timestamp:      time.Now(),
			Extra:          map[string]interface{}{"account_id": "acct-" + nonce},
		}
	}

	first, err := svc.HandleIngressMessage(ctx, makeImg("mh:aaaa0001", "https://f/img-a.png"))
	if err != nil {
		t.Fatalf("首张图 HandleIngressMessage: %v", err)
	}
	if !first.Accepted || !first.QueuedForAI {
		t.Fatalf("首张图应 accepted+queued: %+v", first)
	}

	second, err := svc.HandleIngressMessage(ctx, makeImg("mh:bbbb0002", "https://f/img-b.png"))
	if err != nil {
		t.Fatalf("次张图 HandleIngressMessage: %v", err)
	}
	if strings.Contains(second.Reason, "duplicate(channel+sender+content)") {
		t.Fatalf("两张不同图不得共用内容窗口键（F1 修复点），reason=%q", second.Reason)
	}
	if !second.Accepted || !second.QueuedForAI {
		t.Fatalf("次张图应 accepted+queued: %+v", second)
	}

	// 同一张图（同 media_url）重报仍判重：媒体维度的幂等不回退
	dup, err := svc.HandleIngressMessage(ctx, makeImg("mh:cccc0003", "https://f/img-a.png"))
	if err != nil {
		t.Fatalf("同图重报 HandleIngressMessage: %v", err)
	}
	if dup.QueuedForAI {
		t.Fatalf("同图（同 media_url）窗口内重报必须判重不触发 AI: %+v", dup)
	}
}
