package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// 批F-4 / M-01（钉钉半场·承载腿）：入站媒体整条被抹平成一行 text。
//
// 修复前的形状：ReceiveMessage 的匿名结构体只声明了 content.content 与 text.content，
// 官方媒体容器（图片/语音/视频/文件的 downloadCode、file 的 fileName、audio 的 recognition、
// richText 的逐项内容）**根本没有承载位**；MsgType 一律写死 text，空正文兜成
// "[" + msgtype + "]"（图片那行因此叫 "[picture]"）。
//
// 官方口径（open.dingtalk.com/document/orgapp/receive-message，并用官方 python stream SDK
// dingtalk_stream/chatbot.py 的 ChatbotMessage.from_dict 交叉核对）：媒体字段挂在**顶层 content
// 对象**里（SDK 原文 `ImageContent.from_dict(d['content'])`、`RichTextContent.from_dict(d['content'])`），
// 不是与 msgtype 同名的子对象；richText 逐项形如 {"text":…} / {"downloadCode":…,"type":"picture"}；
// robotCode 由回调顶层携带（仅自定义机器人没有）；
// downloadCode 的有效期官方没有公布数字，只在错误码里出现「下载码有误或者已经过期」⇒ 入站当场
// 换取下载链接是唯一安全设计。
// 下载转存那条腿见 webhook_batchf4_m01_dingtalk_download_test.go。

const f4DtSecret = "SECf4media"

type f4DtHubRow struct {
	MsgType  string
	Content  string
	MediaURL string
	Extra    model.JSONMap
	SentAt   time.Time
}

// f4DtLoadHubByMsgID 轮询读回：media_url 由异步转存回填，直接读会把竞态误读成缺陷。
func f4DtLoadHubByMsgID(t *testing.T, db *gorm.DB, msgID string, waitMediaURL bool) f4DtHubRow {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var hub model.MessageHub
	var err error
	for {
		hub = model.MessageHub{}
		err = db.Where("platform = ? AND msg_id = ?", "dingtalk", msgID).First(&hub).Error
		if err == nil && (!waitMediaURL || hub.MediaURL != "") {
			break
		}
		if time.Now().After(deadline) {
			if err != nil {
				t.Fatalf("读不到 hub 行 msg_id=%s: %v", msgID, err)
			}
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	return f4DtHubRow{MsgType: hub.MsgType, Content: hub.Content, MediaURL: hub.MediaURL,
		Extra: hub.Extra, SentAt: hub.SentAt}
}

// f4DtSetup 自建一条钉钉入站管线：账号随机化（共享测试库）+ 进程内 cache（去重状态不外溢）。
//
// 顺带掐掉媒体转存的对外两条腿：本文件测的是**承载**（msg_type / content / Extra 落库形状），
// 不测转存。不掐的话每条带 downloadCode 的用例都会拿假凭据真打一次
// api.dingtalk.com/v1.0/oauth2/accessToken（实测一次跑批 5 次真外网请求、5 条
// 「媒体下载失败」WRN），断网环境下慢且吵，转存那条腿另有 webhook_batchf4_m01_dingtalk_download_test.go
// 用替身逐字段断言。
func f4DtSetup(t *testing.T) (*DingTalkAppService, uint, *gorm.DB) {
	t.Helper()
	prevFetch, prevStore := dtMediaFetchFn, dtMediaStoreFn
	t.Cleanup(func() { dtMediaFetchFn, dtMediaStoreFn = prevFetch, prevStore })
	dtMediaFetchFn = func(context.Context, string, string, string, string) ([]byte, string, error) {
		return nil, "", errors.New("f4dt: 承载用例不发起真实下载")
	}
	dtMediaStoreFn = func(context.Context, string, string, []byte, string, string) (string, error) {
		return "", errors.New("f4dt: 承载用例不写长期存储")
	}
	t.Setenv("ALLOW_INSECURE_WEBHOOK", "")
	db := testutil.NewTestDB(t, &model.DingTalkAppAccount{}, &model.MessageHub{},
		&model.InboxConversation{}, &model.UnifiedMessage{})
	acc := &model.DingTalkAppAccount{
		AppKey: fmt.Sprintf("ak-f4-%d", time.Now().UnixNano()), AppSecret: f4DtSecret,
		InboundEnabled: true, Status: 1,
	}
	acc.UserID = 1
	if err := db.Create(acc).Error; err != nil {
		t.Fatalf("create dingtalk account: %v", err)
	}
	mc := cache.NewMemoryCache()
	t.Cleanup(mc.Close)
	return NewDingTalkAppService(db, &WebhookService{ingressSvc: NewInboxIngressServiceWithDB(db, mc)}), acc.ID, db
}

// f4DtEnvelope 拼官方回调外壳：公共字段 + msgtype + 该类型的消息体片段。
func f4DtEnvelope(msgID, msgtype, body, nonce string) string {
	return `{"conversationType":"1","senderStaffId":"staff-f4` + nonce +
		`","conversationId":"cid-` + nonce + `","msgId":"` + msgID + `","robotCode":"robot-f4",` +
		`"msgtype":"` + msgtype + `"` + body + `}`
}

func TestM01_DingTalkInboundMediaKeepsTypeAndDownloadCode(t *testing.T) {
	svc, id, db := f4DtSetup(t)
	headers := dtRobotHeaders(f4DtSecret, time.Now().UnixMilli())
	nonce := fmt.Sprintf("f4dt%d", time.Now().UnixNano())

	cases := []struct {
		name        string
		msgID       string
		body        string
		wantType    string
		wantContent string
		wantExtra   map[string]string
	}{
		{
			name:        "picture",
			msgID:       "pic-" + nonce,
			body:        f4DtEnvelope("pic-"+nonce, "picture", `,"content":{"downloadCode":"dc-`+nonce+`-pic","width":"720","height":"1280"}`, nonce),
			wantType:    model.MsgTypeImage,
			wantContent: "[图片]",
			wantExtra:   map[string]string{"media_download_code": "dc-" + nonce + "-pic", "robot_code": "robot-f4"},
		},
		{
			name:        "audio_with_recognition",
			msgID:       "aud-" + nonce,
			body:        f4DtEnvelope("aud-"+nonce, "audio", `,"content":{"downloadCode":"dc-`+nonce+`-aud","duration":6000,"recognition":"帮我查下订单发货了吗"}`, nonce),
			wantType:    model.MsgTypeAudio,
			wantContent: "帮我查下订单发货了吗",
			wantExtra:   map[string]string{"media_download_code": "dc-" + nonce + "-aud", "media_duration": "6000"},
		},
		{
			name:  "file",
			msgID: "fil-" + nonce,
			body: f4DtEnvelope("fil-"+nonce, "file",
				`,"content":{"spaceId":"99","fileId":"f1","fileName":"报价单.pdf","downloadCode":"dc-`+nonce+`-file","fileType":"pdf"}`, nonce),
			wantType:    model.MsgTypeFile,
			wantContent: "[文件] 报价单.pdf",
			wantExtra:   map[string]string{"media_download_code": "dc-" + nonce + "-file", "file_name": "报价单.pdf"},
		},
		{
			name:        "video",
			msgID:       "vid-" + nonce,
			body:        f4DtEnvelope("vid-"+nonce, "video", `,"content":{"downloadCode":"dc-`+nonce+`-vid","videoType":"mp4","duration":15000}`, nonce),
			wantType:    model.MsgTypeVideo,
			wantContent: "[视频]",
			wantExtra:   map[string]string{"media_download_code": "dc-" + nonce + "-vid"},
		},
		{
			name:  "richText",
			msgID: "rt-" + nonce,
			body: f4DtEnvelope("rt-"+nonce, "richText",
				`,"content":{"richText":[{"text":"这款有货"},{"downloadCode":"dc-`+nonce+`-rt1","type":"picture"},{"downloadCode":"dc-`+nonce+`-rt2","type":"picture"}]}`, nonce),
			// 富文本与飞书 post 同口径：正文已逐项解出来（文字 + 每图一个占位符），
			// 类型就按文本走 —— 官方名 "richText" 不在中台 msg_type 词表里，
			// 直接入库等于写一行工作台永远筛不到的记录（见 N-17）。
			wantType:    model.MsgTypeText,
			wantContent: "这款有货[图片][图片]",
			wantExtra:   map[string]string{"media_download_code": "dc-" + nonce + "-rt1"},
		},
	}

	for _, tc := range cases {
		if err := svc.ReceiveMessage(context.Background(), id, []byte(tc.body), nil, headers); err != nil {
			t.Fatalf("%s: ReceiveMessage: %v", tc.name, err)
		}
	}

	for _, tc := range cases {
		row := f4DtLoadHubByMsgID(t, db, "dt-"+fmt.Sprint(id)+"-"+tc.msgID, false)
		if row.MsgType != tc.wantType {
			t.Errorf("M-01(%s)：hub.msg_type = %q，want %q（官方 msgtype 被写死成 text）", tc.name, row.MsgType, tc.wantType)
		}
		if row.Content != tc.wantContent {
			t.Errorf("M-01(%s)：hub.content = %q，want %q", tc.name, row.Content, tc.wantContent)
		}
		for k, want := range tc.wantExtra {
			got, _ := f4DtExtraString(row.Extra, k)
			if got != want {
				t.Errorf("M-01(%s)：hub.Extra[%q] = %q，want %q（downloadCode 会过期，入站不取走就永久丢失）",
					tc.name, k, got, want)
			}
		}
	}
}

// f4DtExtraString 取 Extra 里的标量：数字经 JSON 往返后是 float64，编码不该由用例猜。
func f4DtExtraString(extra model.JSONMap, key string) (string, bool) {
	if extra == nil {
		return "", false
	}
	switch v := extra[key].(type) {
	case string:
		return v, true
	case float64:
		return fmt.Sprintf("%d", int64(v)), true
	case int64:
		return fmt.Sprintf("%d", v), true
	case json.Number:
		return v.String(), true
	}
	return "", false
}

// TestM01_DingTalkKeepsOfficialCreateAt 官方 createAt 是毫秒时间戳，
// 修复前入站直接写 time.Now()：客服侧看到的是「处理时刻」而不是「客户发送时刻」，
// 时序、响应时长统计、以及跨渠道对齐全都被这 3 小时的差值污染。
// 两种官方写法都要收：文档里 createAt/duration 一会儿带引号一会儿不带，
// 而整包 Unmarshal 是原子的——形态不符就返回 400，等于把这条客户消息永久丢掉。
func TestM01_DingTalkKeepsOfficialCreateAt(t *testing.T) {
	svc, id, db := f4DtSetup(t)
	headers := dtRobotHeaders(f4DtSecret, time.Now().UnixMilli())
	nonce := fmt.Sprintf("f4dtc%d", time.Now().UnixNano())
	sent := time.Now().Add(-3 * time.Hour)

	cases := []struct{ name, msgID, createAt, extra string }{
		{"quoted_millis", "tsq-" + nonce, `"` + fmt.Sprint(sent.UnixMilli()) + `"`, ``},
		{"bare_millis", "tsb-" + nonce, fmt.Sprint(sent.UnixMilli()), `,"duration":"6000"`},
		{"bare_seconds", "tss-" + nonce, fmt.Sprint(sent.Unix()), ``},
		{"missing", "tsm-" + nonce, ``, ``},
		{"null", "tsn-" + nonce, `null`, ``},
	}
	for _, tc := range cases {
		body := `{"conversationType":"1","senderStaffId":"staff-f4` + nonce +
			`","conversationId":"cid-` + nonce + `","msgId":"` + tc.msgID +
			`","robotCode":"robot-f4","msgtype":"text"`
		if tc.createAt != "" {
			body += `,"createAt":` + tc.createAt
		}
		body += tc.extra + `,"text":{"content":"三点钟发的 ` + nonce + `"}}`
		if err := svc.ReceiveMessage(context.Background(), id, []byte(body), nil, headers); err != nil {
			t.Fatalf("%s: ReceiveMessage: %v", tc.name, err)
		}
	}

	for _, tc := range cases {
		row := f4DtLoadHubByMsgID(t, db, "dt-"+fmt.Sprint(id)+"-"+tc.msgID, false)
		switch tc.name {
		case "missing", "null":
			if d := row.SentAt.Sub(time.Now()); d > 2*time.Second || d < -2*time.Second {
				t.Errorf("M-01(%s)：createAt 缺失时 sent_at = %v，want ≈now", tc.name, row.SentAt)
			}
		default:
			if d := row.SentAt.Sub(sent); d > 2*time.Second || d < -2*time.Second {
				t.Errorf("M-01(%s)：hub.sent_at = %v，want ≈%v（官方 createAt 被 time.Now() 覆盖，差 %v）",
					tc.name, row.SentAt, sent, d)
			}
		}
	}
}

// TestM01_DingTalkTextUnchanged 反向对照：文本消息不受媒体改造影响（正文原样、无媒体键）。
func TestM01_DingTalkTextUnchanged(t *testing.T) {
	svc, id, db := f4DtSetup(t)
	headers := dtRobotHeaders(f4DtSecret, time.Now().UnixMilli())
	nonce := fmt.Sprintf("f4dtt%d", time.Now().UnixNano())
	body := f4DtEnvelope("t-"+nonce, "text", `,"text":{"content":"有货吗 `+nonce+`"}`, nonce)
	if err := svc.ReceiveMessage(context.Background(), id, []byte(body), nil, headers); err != nil {
		t.Fatalf("ReceiveMessage: %v", err)
	}
	row := f4DtLoadHubByMsgID(t, db, "dt-"+fmt.Sprint(id)+"-t-"+nonce, false)
	if row.MsgType != model.MsgTypeText || row.Content != "有货吗 "+nonce {
		t.Errorf("文本入站被改坏：type=%q content=%q", row.MsgType, row.Content)
	}
	if _, ok := row.Extra["media_download_code"]; ok {
		t.Errorf("文本消息不该带 media_download_code，Extra=%v", row.Extra)
	}
	if row.MediaURL != "" {
		t.Errorf("文本消息不该有 media_url，got %q", row.MediaURL)
	}
}
