package service

import (
	"context"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// 2026-11-10 审计 F6：DeliverBridgeOutboundMedia 补上下行媒体的服务端生产者出口。
// 下行媒体协议三端（OutboxMessage.MediaURL / SSE media_url / 扩展 sendOutbound 分派）
// 早已就绪，此前所有出站入口都 text-only 入队。本文件钉三个口径：
//  1. 带媒体出站：pending 行带 media_url + msg_type，msg_id 用 typed 口径（同占位符
//     两张图的行 msg_id 不碰撞）；
//  2. 文本出站（旧签名）：msg_id 与 ContentHashMsgID 逐字节一致（存量不变）；
//  3. media_url 列宽收口（F2）与 text 类型强制清空 media_url。
func TestDeliverBridgeOutboundMedia_TwoImagesDistinctMsgIDs(t *testing.T) {
	setupBridgeWhitelistForTest(t, "douyin")
	testutil.NewTestDB(t, &model.MessageHub{})
	svc := NewInboxIngressService()
	SetGlobalInboxIngressService(svc)
	defer SetGlobalInboxIngressService(nil)

	ctx := context.Background()
	const (
		channel = "douyin"
		account = "acc_media_out"
		conv    = "conv_media_out"
	)

	id1, msg1, err := DeliverBridgeOutboundMedia(ctx, channel, account, conv, "image", "[图片]", "https://f/x1.png", "")
	if err != nil {
		t.Fatalf("DeliverBridgeOutboundMedia(图1) 失败: %v", err)
	}
	id2, msg2, err := DeliverBridgeOutboundMedia(ctx, channel, account, conv, "image", "[图片]", "https://f/x2.png", "")
	if err != nil {
		t.Fatalf("DeliverBridgeOutboundMedia(图2) 失败: %v", err)
	}
	if msg1 == msg2 {
		t.Fatalf("同占位符两张图的 msg_id 必须不同（typed 口径）: %s", msg1)
	}
	if id1 == id2 {
		t.Fatalf("两行 hub id 相同: %d", id1)
	}

	pending, err := svc.ListPendingOutbound(ctx, channel, account)
	if err != nil {
		t.Fatalf("ListPendingOutbound 失败: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("期望 2 条 pending，实际 %d", len(pending))
	}
	byMsg := map[string]*model.MessageHub{}
	for i := range pending {
		byMsg[pending[i].MsgID] = pending[i]
	}
	row1 := byMsg[msg1]
	if row1 == nil {
		t.Fatalf("回执 msg_id %s 未命中 pending 行", msg1)
	}
	if row1.MediaURL != "https://f/x1.png" {
		t.Errorf("MediaURL = %q, want https://f/x1.png", row1.MediaURL)
	}
	if row1.MsgType != "image" {
		t.Errorf("MsgType = %q, want image", row1.MsgType)
	}
	if row1.Direction != "outbound" {
		t.Errorf("Direction = %q, want outbound", row1.Direction)
	}
}

func TestDeliverBridgeOutboundMedia_TextLegacyByteIdentical(t *testing.T) {
	setupBridgeWhitelistForTest(t, "douyin")
	testutil.NewTestDB(t, &model.MessageHub{})
	svc := NewInboxIngressService()
	SetGlobalInboxIngressService(svc)
	defer SetGlobalInboxIngressService(nil)

	const (
		channel = "douyin"
		account = "acc_media_out_txt"
		conv    = "conv_media_out_txt"
		content = "纯文本回复"
	)
	_, msgID, err := DeliverBridgeOutboundMedia(ctx2(), channel, account, conv, "text", content, "", "")
	if err != nil {
		t.Fatalf("DeliverBridgeOutboundMedia(text) 失败: %v", err)
	}
	if msgID != ContentHashMsgID(channel, conv, content) {
		t.Fatalf("文本出站 msg_id 必须与旧口径逐字节一致: got %s want %s", msgID, ContentHashMsgID(channel, conv, content))
	}
	pending, err := svc.ListPendingOutbound(ctx2(), channel, account)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending 查询失败或条数不符: %v %d", err, len(pending))
	}
	if pending[0].MediaURL != "" {
		t.Errorf("文本出站 MediaURL 必须为空: %q", pending[0].MediaURL)
	}
}

func TestDeliverBridgeOutboundMedia_TextTypeForcesEmptyMediaURLAndClamps(t *testing.T) {
	setupBridgeWhitelistForTest(t, "douyin")
	testutil.NewTestDB(t, &model.MessageHub{})
	svc := NewInboxIngressService()
	SetGlobalInboxIngressService(svc)
	defer SetGlobalInboxIngressService(nil)

	// text 类型强制清空 media_url
	_, msgID, err := DeliverBridgeOutboundMedia(ctx2(), "douyin", "acc_a", "conv_a", "text", "hi", "https://f/should-be-dropped.png", "")
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	hub, err := svc.hubRepo.GetByMsgID(ctx2(), msgID)
	if err != nil || hub == nil {
		t.Fatalf("行未落库: %v", err)
	}
	if hub.MediaURL != "" {
		t.Errorf("text 类型 MediaURL 必须清空: %q", hub.MediaURL)
	}

	// 媒体类型超长 URL 截到列宽（F2）
	long := "https://f/" + strings.Repeat("z", 600)
	_, msgID2, err := DeliverBridgeOutboundMedia(ctx2(), "douyin", "acc_a", "conv_a", "image", "[图片]", long, "")
	if err != nil {
		t.Fatalf("超长媒体出站失败: %v", err)
	}
	hub2, err := svc.hubRepo.GetByMsgID(ctx2(), msgID2)
	if err != nil || hub2 == nil {
		t.Fatalf("行未落库: %v", err)
	}
	if len([]rune(hub2.MediaURL)) > model.MaxMediaURLLen {
		t.Errorf("MediaURL 未截断: %d 字符", len([]rune(hub2.MediaURL)))
	}
}

func ctx2() context.Context { return context.Background() }
