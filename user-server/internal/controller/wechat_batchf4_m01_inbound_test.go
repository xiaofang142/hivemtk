package controller

import (
	"fmt"
	"testing"
	"time"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/service"

	"gorm.io/gorm"
)

// 批F-4 / M-01（公众号半场·controller 承载腿）：
// service/wechat.go 的解析结构体**早就**把 MediaId/PicUrl/Format/Location_*/Title/Url 读出来了，
// 而 controller/wechat.go 构造 MessageEvent 时只带 msg.MsgType 与 msg.Content ⇒ 图片/语音/视频
// 落库成 msg_type=image、content=""（image/voice/video 的 XML 根本没有 Content 节点），
// AI 收到的是一条空消息；event 推送（关注/取关/菜单点击）更是直接落一行空 content 的 hub
// 并驱动一次没有输入的 AI 回复（抖音 N-07 同源）。
//
// 官方口径（developers.weixin.qq.com/doc/offiaccount/Message_Management/
// Receiving_standard_messages.html）：image 带 PicUrl+MediaId、voice 带 MediaId+Format、
// video/shortvideo 带 MediaId，「MediaId 可以调用获取临时素材接口拉取数据」。

// f4WxSetup 只建账号位（不建 WechatAccount 行）：入站媒体转存取不到 accessToken 会自行跳过，
// 用例因此不会向 api.weixin.qq.com 发出任何真实请求。
func f4WxSetup(t *testing.T) (*WechatController, uint, *gorm.DB) {
	t.Helper()
	database := testutil.NewTestDB(t, &model.WechatAccount{}, &model.MessageHub{},
		&model.InboxConversation{}, &model.UnifiedMessage{})
	db.SetTestDB(database)
	accID := uint(time.Now().UnixNano() % 1_000_000_000)
	mc := cache.NewMemoryCache()
	t.Cleanup(mc.Close)
	ctrl := NewWechatController(service.NewWechatService(database))
	ctrl.SetIngressSvc(service.NewInboxIngressServiceWithDB(database, mc))
	return ctrl, accID, database
}

func f4WxLoadHub(t *testing.T, database *gorm.DB, msgID string) (model.MessageHub, bool) {
	t.Helper()
	var hub model.MessageHub
	err := database.Where("platform = ? AND msg_id = ?", "wechat", msgID).First(&hub).Error
	if err == gorm.ErrRecordNotFound {
		return model.MessageHub{}, false
	}
	if err != nil {
		t.Fatalf("读 hub 行 msg_id=%s: %v", msgID, err)
	}
	return hub, true
}

func TestM01_WechatInboundMediaKeepsTypeAndReference(t *testing.T) {
	ctrl, accID, database := f4WxSetup(t)
	nonce := fmt.Sprintf("f4wx%d", time.Now().UnixNano())
	from := "o-f4-" + nonce

	cases := []struct {
		name        string
		msgID       string
		msg         service.WechatIncomingMessage
		wantType    string
		wantContent string
		wantExtra   map[string]string
	}{
		{
			name: "image", msgID: "wximg" + nonce,
			msg:         service.WechatIncomingMessage{MsgID: "wximg" + nonce, MsgType: "image", MediaID: "MID-" + nonce + "-img", PicURL: "https://mmbiz.qpic.cn/pic/" + nonce},
			wantType:    model.MsgTypeImage,
			wantContent: "[图片]",
			wantExtra:   map[string]string{"media_id": "MID-" + nonce + "-img", "pic_url": "https://mmbiz.qpic.cn/pic/" + nonce},
		},
		{
			name: "voice", msgID: "wxvox" + nonce,
			msg:         service.WechatIncomingMessage{MsgID: "wxvox" + nonce, MsgType: "voice", MediaID: "MID-" + nonce + "-vox", Format: "amr"},
			wantType:    model.MsgTypeAudio,
			wantContent: "[语音]",
			wantExtra:   map[string]string{"media_id": "MID-" + nonce + "-vox", "format": "amr"},
		},
		{
			name: "video", msgID: "wxvid" + nonce,
			msg:         service.WechatIncomingMessage{MsgID: "wxvid" + nonce, MsgType: "video", MediaID: "MID-" + nonce + "-vid"},
			wantType:    model.MsgTypeVideo,
			wantContent: "[视频]",
			wantExtra:   map[string]string{"media_id": "MID-" + nonce + "-vid"},
		},
		{
			name: "shortvideo", msgID: "wxsvd" + nonce,
			msg: service.WechatIncomingMessage{MsgID: "wxsvd" + nonce, MsgType: "shortvideo", MediaID: "MID-" + nonce + "-svd"},
			// 类型落中台词表里的 video，不是官方的 shortvideo：词表没有「小视频」这一档，
			// 落词表外名字会让工作台筛选与 by_msg_type 统计都筛不到这一行
			// （见 service/message_hub.go 的 inboundHubMsgTypeAliases 与
			// webhook_batchf4_msgtype_test.go 的同名断言）。小视频的区分靠正文占位符。
			wantType:    model.MsgTypeVideo,
			wantContent: "[小视频]",
			wantExtra:   map[string]string{"media_id": "MID-" + nonce + "-svd"},
		},
		{
			name: "location", msgID: "wxloc" + nonce,
			msg:         service.WechatIncomingMessage{MsgID: "wxloc" + nonce, MsgType: "location", LocationX: 23.1, LocationY: 113.3, Label: "腾讯大厦"},
			wantType:    model.MsgTypeLocation,
			wantContent: "[位置] 腾讯大厦",
			wantExtra:   map[string]string{"latitude": "23.1", "longitude": "113.3"},
		},
		{
			name: "link", msgID: "wxlnk" + nonce,
			msg:         service.WechatIncomingMessage{MsgID: "wxlnk" + nonce, MsgType: "link", Title: "报价单", URL: "https://example.com/q/" + nonce},
			wantType:    model.MsgTypeLink,
			wantContent: "报价单 https://example.com/q/" + nonce,
			wantExtra:   map[string]string{"url": "https://example.com/q/" + nonce},
		},
		{
			name: "text", msgID: "wxTxt" + nonce,
			msg:         service.WechatIncomingMessage{MsgID: "wxTxt" + nonce, MsgType: "text", Content: "有货吗 " + nonce},
			wantType:    model.MsgTypeText,
			wantContent: "有货吗 " + nonce,
			wantExtra:   map[string]string{},
		},
	}

	for _, tc := range cases {
		tc.msg.FromUserName = from
		tc.msg.ToUserName = "gh_f4official"
		tc.msg.CreateTime = time.Now().Add(-time.Hour).Unix()
		ctrl.handleIncomingMessage(accID, &tc.msg)
	}

	hubMsgID := func(msgID string) string { return fmt.Sprintf("wx-%d-%s", accID, msgID) }

	for _, tc := range cases {
		hub, found := f4WxLoadHub(t, database, hubMsgID(tc.msgID))
		if !found {
			t.Errorf("M-01(%s)：hub 行根本没落（msg_id=%s）", tc.name, hubMsgID(tc.msgID))
			continue
		}
		if hub.MsgType != tc.wantType {
			t.Errorf("M-01(%s)：hub.msg_type = %q，want %q（controller 只带原始 XML 字段，媒体类型没归一）",
				tc.name, hub.MsgType, tc.wantType)
		}
		if hub.Content != tc.wantContent {
			t.Errorf("M-01(%s)：hub.content = %q，want %q", tc.name, hub.Content, tc.wantContent)
		}
		for k, want := range tc.wantExtra {
			got := fmt.Sprint(hub.Extra[k])
			if got != want {
				t.Errorf("M-01(%s)：hub.Extra[%q] = %q，want %q（media_id 是会过期的临时素材引用，入站不记就等于永久丢失）",
					tc.name, k, got, want)
			}
		}
		if got := fmt.Sprint(hub.Extra["channel_msg_id"]); got != tc.msgID {
			t.Errorf("M-01(%s)：hub.Extra[channel_msg_id] = %q，want %q", tc.name, got, tc.msgID)
		}
	}
}

// TestM01_WechatEventPushDoesNotEnterInbox 事件推送不是客户消息：既不该落 hub，
// 也不该驱动一次空正文的 AI 回复。
func TestM01_WechatEventPushDoesNotEnterInbox(t *testing.T) {
	ctrl, accID, database := f4WxSetup(t)
	nonce := fmt.Sprintf("f4wxe%d", time.Now().UnixNano())
	msg := &service.WechatIncomingMessage{
		FromUserName: "o-f4-" + nonce, ToUserName: "gh_f4official",
		MsgType: "event", Event: "subscribe", CreateTime: time.Now().Unix(),
	}
	ctrl.handleIncomingMessage(accID, msg)

	var count int64
	if err := database.Model(&model.MessageHub{}).
		Where("platform = ? AND conversation_id = ?", "wechat", fmt.Sprintf("wechat:%d:%s", accID, msg.FromUserName)).
		Count(&count).Error; err != nil {
		t.Fatalf("count hub: %v", err)
	}
	if count != 0 {
		t.Errorf("M-01：事件推送落了 %d 行 hub，want 0（空正文还会驱动一次 AI 回复）", count)
	}
}

// TestM01_WechatEmptyTextDoesNotEnterInbox 空正文的 text 同样不该进收件箱。
func TestM01_WechatEmptyTextDoesNotEnterInbox(t *testing.T) {
	ctrl, accID, database := f4WxSetup(t)
	nonce := fmt.Sprintf("f4wxem%d", time.Now().UnixNano())
	msg := &service.WechatIncomingMessage{
		MsgID: "wxem" + nonce, FromUserName: "o-f4-" + nonce, ToUserName: "gh_f4official",
		MsgType: "text", Content: "   ", CreateTime: time.Now().Unix(),
	}
	ctrl.handleIncomingMessage(accID, msg)
	if _, found := f4WxLoadHub(t, database, fmt.Sprintf("wx-%d-%s", accID, msg.MsgID)); found {
		t.Error("M-01：空白 text 落了 hub 行，want 不落")
	}
}
