package service

import (
	"context"
	"errors"
	"testing"

	"hivemtk-user/internal/model"
)

// FA-IM-01：统一收件箱人工回复按平台路由并返回真实投递状态。
// 通过注入 seam 做纯单测，不依赖真实 HTTP/DB，覆盖无路由/离线/拒收/ACK 丢失/本地写失败。

type faIM01Dispatch struct {
	calls    int
	channel  WebhookChannel
	account  string
	eventID  string
	content  string
	sent     bool
	platMsg  string
	err      error
}

func (f *faIM01Dispatch) fn(ctx context.Context, ch WebhookChannel, accountID, eventID, content string) (bool, string, error) {
	f.calls++
	f.channel, f.account, f.eventID, f.content = ch, accountID, eventID, content
	return f.sent, f.platMsg, f.err
}

type faIM01Local struct {
	calls   int
	session string
	content string
	err     error
}

func (f *faIM01Local) fn(ctx context.Context, sessionID, content, senderID, senderName string) (*model.SessionMessage, error) {
	f.calls++
	f.session, f.content = sessionID, content
	if f.err != nil {
		return nil, f.err
	}
	return &model.SessionMessage{SessionID: sessionID, Content: content, SenderType: "agent"}, nil
}

func newFAIM01Svc(d *faIM01Dispatch, l *faIM01Local) *InboxReplyOutboundService {
	return &InboxReplyOutboundService{dispatch: d.fn, localSend: l.fn}
}

func faIM01Req(platform model.Platform) *InboxReplyRequest {
	return &InboxReplyRequest{
		Platform:   platform,
		AccountID:  "acc-1",
		SessionID:  "sess-1",
		Content:    "您好",
		AgentID:    42,
		SenderName: "坐席甲",
	}
}

// web 本地路径：只走 localSend，delivered，不触发外部 dispatch。
func TestFAIM01_WebLocalPath(t *testing.T) {
	d := &faIM01Dispatch{}
	l := &faIM01Local{}
	svc := newFAIM01Svc(d, l)
	del, msg, err := svc.Reply(context.Background(), faIM01Req(model.PlatformWeb))
	if err != nil || del.Status != InboxReplyDelivered {
		t.Fatalf("web 应本地送达: %+v %v", del, err)
	}
	if l.calls != 1 || d.calls != 0 || msg == nil {
		t.Fatalf("web 应只本地落库一次, dispatch=0: local=%d dispatch=%d msg=%v", l.calls, d.calls, msg)
	}
}

// 外部平台成功且有回执 → delivered；外部发送器计数=1，事件ID唯一含 human_reply 前缀。
func TestFAIM01_ExternalDeliveredWithReceipt(t *testing.T) {
	d := &faIM01Dispatch{sent: true, platMsg: "pm-123"}
	l := &faIM01Local{}
	svc := newFAIM01Svc(d, l)
	del, _, err := svc.Reply(context.Background(), faIM01Req(model.PlatformDouyin))
	if err != nil || del.Status != InboxReplyDelivered || del.PlatformMsgID != "pm-123" {
		t.Fatalf("外部应送达: %+v %v", del, err)
	}
	if d.calls != 1 || l.calls != 1 || d.channel != ChannelDouyin || d.account != "acc-1" {
		t.Fatalf("发送器应收到一次: %+v local=%d", d, l.calls)
	}
	if len(d.eventID) < len("human_reply:") || d.eventID[:len("human_reply:")] != "human_reply:" {
		t.Fatalf("事件ID 应为人工回复唯一键: %s", d.eventID)
	}
}

// ACK 丢失：sent=true 但 platformMsgID 空 → platform_confirmed（不冒充送达）。
func TestFAIM01_AckLostIsPlatformConfirmed(t *testing.T) {
	d := &faIM01Dispatch{sent: true, platMsg: ""}
	svc := newFAIM01Svc(d, &faIM01Local{})
	del, _, err := svc.Reply(context.Background(), faIM01Req(model.PlatformDouyin))
	if err != nil || del.Status != InboxReplyPlatformConfirmed {
		t.Fatalf("ACK 丢失应为平台确认: %+v %v", del, err)
	}
}

// 无路由：未映射平台 → no_route，不触发外部 dispatch，但本地受理仍发生。
func TestFAIM01_NoRoute(t *testing.T) {
	d := &faIM01Dispatch{}
	l := &faIM01Local{}
	svc := newFAIM01Svc(d, l)
	del, _, err := svc.Reply(context.Background(), faIM01Req(model.Platform("unknown_platform")))
	if err != nil || del.Status != InboxReplyNoRoute {
		t.Fatalf("无路由应明确返回: %+v %v", del, err)
	}
	if d.calls != 0 || l.calls != 1 {
		t.Fatalf("无路由不应派发外部: dispatch=%d local=%d", d.calls, l.calls)
	}
}

// 离线：dispatch 返回 error → failed，错误带平台投递事实。
func TestFAIM01_OfflineDispatchFailure(t *testing.T) {
	d := &faIM01Dispatch{err: errors.New("platform offline")}
	svc := newFAIM01Svc(d, &faIM01Local{})
	del, _, err := svc.Reply(context.Background(), faIM01Req(model.PlatformDouyin))
	if err != nil || del.Status != InboxReplyFailed || del.Error == "" {
		t.Fatalf("离线应失败且有原因: %+v %v", del, err)
	}
}

// 拒收：sent=false → failed。
func TestFAIM01_RejectedIsFailed(t *testing.T) {
	d := &faIM01Dispatch{sent: false}
	svc := newFAIM01Svc(d, &faIM01Local{})
	del, _, err := svc.Reply(context.Background(), faIM01Req(model.PlatformDouyin))
	if err != nil || del.Status != InboxReplyFailed {
		t.Fatalf("拒收应失败: %+v %v", del, err)
	}
}

// 本地写失败：localSend 报错 → failed，且绝不触发外部派发。
func TestFAIM01_LocalWriteFailure(t *testing.T) {
	d := &faIM01Dispatch{sent: true, platMsg: "x"}
	l := &faIM01Local{err: errors.New("db error")}
	svc := newFAIM01Svc(d, l)
	del, _, err := svc.Reply(context.Background(), faIM01Req(model.PlatformDouyin))
	if err != nil || del.Status != InboxReplyFailed || d.calls != 0 {
		t.Fatalf("本地失败应终止且不外发: %+v dispatch=%d err=%v", del, d.calls, err)
	}
}

// 平台→渠道映射覆盖。
func TestFAIM01_PlatformChannelMapping(t *testing.T) {
	cases := []struct {
		p    model.Platform
		want WebhookChannel
		ok   bool
	}{
		{model.PlatformDouyin, ChannelDouyin, true},
		{model.PlatformKuaishou, ChannelKuaishou, true},
		{model.PlatformXiaohongshu, ChannelXiaohongshu, true},
		{model.PlatformXianyu, ChannelXianyu, true},
		{model.PlatformTiktok, ChannelTiktok, true},
		{model.PlatformWeChat, ChannelWechat, true},
		{model.PlatformWeb, "", false},
		{model.PlatformWebEmbed, "", false},
	}
	for _, c := range cases {
		got, ok := mapInboxPlatformToChannel(c.p)
		if got != c.want || ok != c.ok {
			t.Fatalf("映射 %s: got %s/%v want %s/%v", c.p, got, ok, c.want, c.ok)
		}
	}
}
