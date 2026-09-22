package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// 批F-4 / M-01（公众号半场·转存腿）：把用户来信的临时素材换成长期可访问的 URL。
//
// 官方口径：
//   - 拉取：GET https://api.weixin.qq.com/cgi-bin/media/get?access_token=&media_id=
//     （developers.weixin.qq.com/doc/offiaccount/Asset_Management/Get_temporary_materials.html）
//     图片/语音返回字节流，**视频类 media_id 返回的是 JSON，里面有 video_url**；
//   - 出错时同一个地址返回 JSON（errcode/errmsg），所以只看 Content-Type 分流不够，
//     必须连首字节一起判；
//   - 官方只写明「上传的临时素材保存 3 天」，用户来信 MediaId 与 PicUrl 的有效期都没有公布
//     ⇒ 与钉钉同理：收到当场转存，不留到工作台点开时再取。
//
// 生产代码里的两个 IO 边界（wechatAPIBase / wxMediaStoreFn）在本文件里换成进程内替身，
// 用例不向 api.weixin.qq.com 发任何真实请求。

// f4WxMediaAPI 起一个官方接口替身：token 端点固定发 tok-f4，media/get 端点按 media_id 前缀分流。
func f4WxMediaAPI(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/cgi-bin/token"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"tok-f4","expires_in":7200}`))
		case strings.Contains(r.URL.Path, "/cgi-bin/media/get"):
			_ = r.ParseForm()
			med := r.Form.Get("media_id")
			switch {
			case strings.HasPrefix(med, "vid-"):
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				_ = json.NewEncoder(w).Encode(map[string]any{"video_url": "https://weixin.example/vid/" + med + ".mp4"})
			case strings.HasPrefix(med, "err-"):
				// 故意用 image/png 头回一段 JSON：真实世界里 header 与体不一致过，
				// 所以判定必须同时看首字节，不能只信 Content-Type。
				w.Header().Set("Content-Type", "image/png")
				_ = json.NewEncoder(w).Encode(map[string]any{"errcode": 40001, "errmsg": "invalid credential"})
			default:
				if strings.Contains(med, "-cx-") {
					// 取消链用例要确定性：让响应晚于 cancel 落地，否则绿可能只是时序运气。
					time.Sleep(400 * time.Millisecond)
				}
				w.Header().Set("Content-Type", "image/png")
				_, _ = w.Write([]byte("png-bytes-of-" + med))
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	orig := loadWechatAPIBase()
	storeWechatAPIBase(srv.URL)
	t.Cleanup(func() { storeWechatAPIBase(orig); srv.Close() })
	return srv
}

// f4WxMediaSetup 建账号 + 一行已落库的入站 hub（异步回填按 platform+account_id+msg_id 定位它）。
func f4WxMediaSetup(t *testing.T, hubMsgID string) (*WechatService, uint, *gorm.DB) {
	t.Helper()
	database := testutil.NewTestDB(t, &model.WechatAccount{}, &model.MessageHub{})
	nonce := fmt.Sprintf("%d", time.Now().UnixNano())
	acc := &model.WechatAccount{
		AppID: "wxappid" + nonce, AppSecret: "wxsecret" + nonce,
		Token: "wxtoken" + nonce, Status: "active",
	}
	if err := database.Create(acc).Error; err != nil {
		t.Fatalf("create wechat account: %v", err)
	}
	hub := &model.MessageHub{
		MsgID: hubMsgID, Platform: "wechat", AccountID: fmt.Sprint(acc.ID),
		Direction: "inbound", MsgType: model.MsgTypeImage, Content: "[图片]",
		ConversationID: "wechat:" + fmt.Sprint(acc.ID) + ":o-f4-" + nonce,
		Extra:          model.JSONMap{"media_id": "m-" + nonce, "channel_msg_id": hubMsgID},
		SentAt:         time.Now(),
	}
	hub.ID = uint(time.Now().UnixNano())
	if err := database.Create(hub).Error; err != nil {
		t.Fatalf("create hub row: %v", err)
	}
	return NewWechatService(database), acc.ID, database
}

// f4WxMediaURL 轮询读回 media_url（回填是异步的，直读会把竞态误读成缺陷）。
func f4WxMediaURL(t *testing.T, database *gorm.DB, hubMsgID string, want bool) string {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		var hub model.MessageHub
		if err := database.Where("platform = ? AND msg_id = ?", "wechat", hubMsgID).First(&hub).Error; err == nil {
			if hub.MediaURL != "" || !want || time.Now().After(deadline) {
				return hub.MediaURL
			}
		}
		if time.Now().After(deadline) {
			return ""
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestM01_WechatInboundImageIsStoredAndBackfilled(t *testing.T) {
	prevStore := wxMediaStoreFn
	t.Cleanup(func() { wxMediaStoreFn = prevStore })
	f4WxMediaAPI(t)

	stored := make(chan string, 4)
	wxMediaStoreFn = func(ctx context.Context, channel, mediaID string, data []byte, contentType, filenameHint string) (string, error) {
		if !strings.Contains(string(data), mediaID) {
			return "", fmt.Errorf("stub: 字节不属于 %s（拿错素材了）", mediaID)
		}
		if contentType != "image/png" {
			return "", fmt.Errorf("stub: contentType = %q, want image/png", contentType)
		}
		stored <- mediaID
		return "/files/" + channel + "/" + mediaID, nil
	}

	hubMsgID := "wx-f4-img-" + fmt.Sprint(time.Now().UnixNano())
	srv, accID, database := f4WxMediaSetup(t, hubMsgID)
	mediaID := "img-" + hubMsgID
	srv.PersistInboundMediaAsync(context.Background(), accID, hubMsgID, mediaID)

	select {
	case got := <-stored:
		if got != mediaID {
			t.Errorf("转存的 media_id = %q, want %q", got, mediaID)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("M-01 未达成：15s 内没有起过一次公众号媒体转存")
	}
	if want := "/files/wechat/" + mediaID; f4WxMediaURL(t, database, hubMsgID, true) != want {
		t.Errorf("M-01 未达成：hub.media_url = %q, want %q（临时素材没换成长期 URL）",
			f4WxMediaURL(t, database, hubMsgID, false), want)
	}
}

// TestM01_WechatVideoMediaUsesVideoURL 官方：视频类 media_id 的响应是 JSON（含 video_url），
// 不能把这段 JSON 当字节流上传成"视频"。
func TestM01_WechatVideoMediaUsesVideoURL(t *testing.T) {
	prevStore := wxMediaStoreFn
	t.Cleanup(func() { wxMediaStoreFn = prevStore })
	f4WxMediaAPI(t)

	var storeCalled atomic.Int32
	wxMediaStoreFn = func(ctx context.Context, channel, mediaID string, data []byte, contentType, filenameHint string) (string, error) {
		storeCalled.Add(1)
		return "/files/" + channel + "/" + mediaID, nil
	}

	hubMsgID := "wx-f4-vid-" + fmt.Sprint(time.Now().UnixNano())
	srv, accID, database := f4WxMediaSetup(t, hubMsgID)
	mediaID := "vid-" + hubMsgID
	srv.PersistInboundMediaAsync(context.Background(), accID, hubMsgID, mediaID)

	want := "https://weixin.example/vid/" + mediaID + ".mp4"
	if got := f4WxMediaURL(t, database, hubMsgID, true); got != want {
		t.Errorf("M-01：hub.media_url = %q, want 官方 video_url %q", got, want)
	}
	time.Sleep(300 * time.Millisecond)
	if got := storeCalled.Load(); got != 0 {
		t.Errorf("视频 media_id 的 JSON 响应被当字节流上传了 %d 次，want 0", got)
	}
}

// TestM01_WechatMediaErrorKeepsPlaceholder 媒体接口回错误 JSON（invalid credential 等）时：
// 不上传、不回填，但占位符正文与 media_id 必须还在——人工/后续重试还要用。
func TestM01_WechatMediaErrorKeepsPlaceholder(t *testing.T) {
	prevStore := wxMediaStoreFn
	t.Cleanup(func() { wxMediaStoreFn = prevStore })
	f4WxMediaAPI(t)

	var storeCalled atomic.Int32
	wxMediaStoreFn = func(ctx context.Context, channel, mediaID string, data []byte, contentType, filenameHint string) (string, error) {
		storeCalled.Add(1)
		return "/files/" + channel + "/" + mediaID, nil
	}

	hubMsgID := "wx-f4-err-" + fmt.Sprint(time.Now().UnixNano())
	srv, accID, database := f4WxMediaSetup(t, hubMsgID)
	srv.PersistInboundMediaAsync(context.Background(), accID, hubMsgID, "err-"+hubMsgID)

	time.Sleep(1500 * time.Millisecond)
	if got := storeCalled.Load(); got != 0 {
		t.Errorf("接口返回错误 JSON 却上传了 %d 次，want 0（会把 errcode 体存成图片）", got)
	}
	var hub model.MessageHub
	if err := database.Where("platform = ? AND msg_id = ?", "wechat", hubMsgID).First(&hub).Error; err != nil {
		t.Fatalf("读 hub: %v", err)
	}
	if hub.MediaURL != "" {
		t.Errorf("转存失败却写了 media_url=%q", hub.MediaURL)
	}
	if hub.Content != "[图片]" || fmt.Sprint(hub.Extra["media_id"]) == "" {
		t.Errorf("占位符/引用应保留，got content=%q extra=%v", hub.Content, hub.Extra)
	}
}

// TestM01_WechatMediaSurvivesRequestCancel 公众号入站跑在 handleIncomingMessage 的
// 「30s 超时 ctx + defer cancel()」里：转存若沿用该 ctx，方法一返回就被杀，media_url 永久为空。
// 用例传 Background() 时看不出来，必须显式取消才有证据。
func TestM01_WechatMediaSurvivesRequestCancel(t *testing.T) {
	prevStore := wxMediaStoreFn
	t.Cleanup(func() { wxMediaStoreFn = prevStore })
	f4WxMediaAPI(t)

	wxMediaStoreFn = func(ctx context.Context, channel, mediaID string, data []byte, contentType, filenameHint string) (string, error) {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "/files/" + channel + "/" + mediaID, nil
	}

	hubMsgID := "wx-f4-cx-" + fmt.Sprint(time.Now().UnixNano())
	srv, accID, database := f4WxMediaSetup(t, hubMsgID)
	ctx, cancel := context.WithCancel(context.Background())
	srv.PersistInboundMediaAsync(ctx, accID, hubMsgID, "img-"+hubMsgID)
	cancel() // 等价于 handler 返回

	if want := "/files/wechat/img-" + hubMsgID; f4WxMediaURL(t, database, hubMsgID, true) != want {
		t.Errorf("M-01 未达成：请求 ctx 取消后 hub.media_url = %q, want %q（异步转存不该随请求一起死）",
			f4WxMediaURL(t, database, hubMsgID, false), want)
	}
}
