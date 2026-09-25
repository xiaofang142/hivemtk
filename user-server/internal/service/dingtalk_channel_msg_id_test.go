package service

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// 承诺 10（钉钉半场）：钉钉生产者必须把官方 msgId 原样写进 event.Extra["channel_msg_id"]。
//
// 为什么单独钉这一格：channel_msg_id 有两个下游消费者，且都不是「读 payload」而是
// 「读 Extra」——
//  1. 入站内容窗口去重的守卫（inbox_ingress_ingest.go:145：带官方 id 的事件不再按 (会话,内容,时间窗)
//     判重），少了它，客户五分钟内的第二句原话会被静默丢掉；
//  2. 出站回环识别与出站关联（inbox_ingress.go:637 → WithChannelMsgID，出站按它回官方 id）。
//
// 现有三条钉钉腿都摸不到这一格：TestN14_DingTalkRepeatedTextIsNotDropped 只数 message_hub 行数，
// TestIngress_PlatformIDEventsSkipContentDedup 手工塞 evt.Extra（不经任何生产者），
// M-01 那两条只断媒体键。同类四个生产者（webhook_channel_qq.go:114、channelbot/core/core.go:203、
// channelbot/qq/qq.go:664、controller/wechat.go:285）各有直断 Extra 的腿，只有钉钉这格空着
// ⇒ 把 dingtalk_app.go:177 改成空串，全仓照样绿。
//
// 断法照抄最窄的同类腿（webhook_batchf4_m01_qq_test.go:312）：真入站 payload → 真管线 →
// 读回落库行的 Extra，断官方值逐字节相等。官方 msgId 自带 `+` `/` `==`，
// 因此这一条同时钉住「非空」「原样透传」「不被任何清洗/归一改写」三件事。

func TestDingTalkInboundChannelMsgIDIsOfficialMsgID(t *testing.T) {
	svc, id, db := f4DtSetup(t)
	headers := dtRobotHeaders(f4DtSecret, time.Now().UnixMilli())

	nonce := fmt.Sprintf("dtcmdid%d", time.Now().UnixNano())
	// 形态取自官方文档样例（msgJ9UJTR5usEh+DlLtRjPrBw==）：保留 base64 面的 `+` `/` `==`，
	// 内部前缀（dt-/dingtalk_）或任何清洗都会立刻体现在这条断言上。
	officialMsgID := "msgAb3d/Efh+DlLtRjPr" + nonce + "Bw=="
	body := f4DtEnvelope(officialMsgID, "text",
		`,"text":{"content":"官方 id 要原样带走 `+nonce+`"}`, nonce)

	if err := svc.ReceiveMessage(context.Background(), id, []byte(body), nil, headers); err != nil {
		t.Fatalf("ReceiveMessage: %v", err)
	}

	row := f4DtLoadHubByMsgID(t, db, "dt-"+fmt.Sprint(id)+"-"+officialMsgID, false)
	got, ok := f4DtExtraString(row.Extra, "channel_msg_id")
	if !ok {
		t.Fatalf("承诺 10 未达成：钉钉入站落库行的 Extra 里没有 channel_msg_id（Extra=%v）"+
			"⇒ 带官方 id 的事件会被内容窗口去重误吞，出站也拿不到官方关联 id", row.Extra)
	}
	if got == "" {
		t.Errorf("承诺 10 未达成：channel_msg_id 是空串（官方 msgId=%q 没被带走）"+
			"⇒ dingtalk_app.go:177 那一格丢了，N-14 的守卫与出站回环识别同时失据", officialMsgID)
	}
	if got != officialMsgID {
		t.Errorf("承诺 10 未达成：hub.Extra[channel_msg_id] = %q, want %q"+
			"（必须是官方 msgId 原值：任何前缀/裁剪都会让出站关联与回环判定错位）", got, officialMsgID)
	}
	if row.Content != "官方 id 要原样带走 "+nonce {
		t.Errorf("夹具漂移：正文被改写 content=%q", row.Content)
	}
}
