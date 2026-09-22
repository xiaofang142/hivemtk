package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// N-10 反证：一条 WA 推送里的每条媒体消息都要各自转存，且回填键必须是 hub 行的
// msg_id（wamid）而不是渠道的 media_id。
//
// 修复前的形状：dispatch 只取 payload 的第一条媒体（MediaRef()），并把 media_id 当作
// msgID 传给 EnrichHubMediaURLByMsgID ⇒ 下载/转存都成功，落库却按 platform+msg_id
// 找不到任何行（hub 行的 msg_id 是 wamid，media_id 存在 Extra 里），
// 结果「媒体已转存」日志与 0 行更新同时发生。

const (
	f2AccountID = "90112"
	f2WAID      = "+8613900000002"
)

type f2StoreCall struct {
	mediaID  string
	filename string
	dataHead string
}

// f2InstallMediaStubs 替换 WA 媒体的下载与转存两个外部 IO 边界：
// 下载返回的字节里带上 media_id，转存按 media_id 推出 URL —— 这样「哪条媒体
// 落到哪一行」是可断言的，串号或漏条都会在断言里露出来。
func f2InstallMediaStubs(t *testing.T) (<-chan f2StoreCall, *sync.Map) {
	t.Helper()
	fetchPrev, storePrev := waMediaFetchFn, waMediaStoreFn
	downloaded := &sync.Map{}
	calls := make(chan f2StoreCall, 16)

	waMediaFetchFn = func(ctx context.Context, accessToken, phoneID, mediaID string) (io.ReadCloser, string, error) {
		if accessToken == "" || phoneID == "" {
			return nil, "", fmt.Errorf("stub: credentials empty")
		}
		downloaded.Store(mediaID, true)
		return io.NopCloser(bytes.NewReader([]byte("bytes-of-" + mediaID))), "image/png", nil
	}
	waMediaStoreFn = func(ctx context.Context, channel, mediaID string, data []byte, contentType, filenameHint string) (string, error) {
		if !bytes.Contains(data, []byte(mediaID)) {
			return "", fmt.Errorf("stub: downloaded bytes do not belong to %s", mediaID)
		}
		calls <- f2StoreCall{mediaID: mediaID, filename: filenameHint, dataHead: string(data)}
		return "/files/" + channel + "/" + mediaID, nil
	}
	t.Cleanup(func() {
		waMediaFetchFn, waMediaStoreFn = fetchPrev, storePrev
	})
	return calls, downloaded
}

func f2MediaBody() []byte {
	return []byte(`{"object":"whatsapp_business_account","entry":[{"id":"W","changes":[{"value":{"messages":[` +
		`{"from":"` + f2WAID + `","id":"wamid-f2-1","timestamp":"1700000001","type":"image","image":{"id":"wa-media-f2-image","mime_type":"image/png","sha256":"a"}},` +
		`{"from":"` + f2WAID + `","id":"wamid-f2-2","timestamp":"1700000002","type":"text","text":{"body":"这是报价单"}},` +
		`{"from":"` + f2WAID + `","id":"wamid-f2-3","timestamp":"1700000003","type":"document","document":{"id":"wa-media-f2-doc","mime_type":"application/pdf","filename":"报价单.pdf"}},` +
		`{"from":"` + f2WAID + `","id":"wamid-f2-4","timestamp":"1700000004","type":"image"}` +
		`],"contacts":[{"profile":{"name":"Bob"},"wa_id":"` + f2WAID + `"}]},"field":"messages"}]}]}`)
}

func TestN10_WhatsAppInboundMediaBackfillsEveryHubRow(t *testing.T) {
	db := testutil.NewTestDB(t,
		&model.WhatsAppCloudAccount{}, &model.MessageHub{}, &model.InboxConversation{},
		&model.UnifiedMessage{}, &model.WebhookEvent{}, &model.IntegrationAccount{},
	)
	if err := db.Create(&model.WhatsAppCloudAccount{
		ID: 90112, AccountName: "f2-acc", PhoneNumberID: "pn-f2",
		WhatsAppBusinessID: "wb-f2", AccessToken: "tok-f2", Status: 1,
	}).Error; err != nil {
		t.Fatalf("create wa account: %v", err)
	}
	svc := NewWebhookService(db)
	defer svc.Stop(context.Background())

	calls, downloaded := f2InstallMediaStubs(t)

	if _, err := svc.dispatchWhatsApp(context.Background(), f2AccountID,
		&ParsedPayload{EventID: "evt-n10-f2"}, f2MediaBody()); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	// 两条媒体都要被排队转存（图片 + 文档），各等一次 IO 完成
	seen := map[string]f2StoreCall{}
	deadline := time.After(15 * time.Second)
	for len(seen) < 2 {
		select {
		case c := <-calls:
			seen[c.mediaID] = c
		case <-deadline:
			t.Fatalf("N-10 未达成：15s 内只完成 %d 条媒体转存（期望 2 条：wa-media-f2-image / wa-media-f2-doc），已见 %v",
				len(seen), f2Keys(seen))
		}
	}

	wantRowMedia := map[string]struct{ mediaID, mime, filename string }{
		"wamid-f2-1": {"wa-media-f2-image", "image/png", ""},
		"wamid-f2-3": {"wa-media-f2-doc", "application/pdf", "报价单.pdf"},
	}
	for msgID, want := range wantRowMedia {
		mediaID := want.mediaID
		hub := f2WaitHubMediaURL(t, db, msgID)
		wantURL := "/files/whatsapp/" + mediaID
		if hub.MediaURL != wantURL {
			t.Errorf("N-10 未达成：hub(msg_id=%s).media_url = %q，want %q（Extra=%s）",
				msgID, hub.MediaURL, wantURL, f2JSON(t, hub.Extra))
		}
		if got := f2MediaIDOf(t, hub.Extra); got != mediaID {
			t.Errorf("N-10b：hub(msg_id=%s).Extra.media_id = %q，want %q（渠道原生媒体引用没落库）",
				msgID, got, mediaID)
		}
		if got, _ := hub.Extra["mime_type"].(string); got != want.mime {
			t.Errorf("N-10b：hub(msg_id=%s).Extra.mime_type = %q，want %q", msgID, got, want.mime)
		}
		// 原始文件名只落在 Extra：media_url 是转存后自己拼的名字，客户上传时的
		// 「报价单.pdf」只有这一处留痕，工作台侧展示与出站都要靠它。
		gotFile, hasFile := hub.Extra["filename"].(string)
		if want.filename == "" {
			if hasFile && gotFile != "" {
				t.Errorf("N-10b：hub(msg_id=%s) 不该带 filename，got %q", msgID, gotFile)
			}
		} else if gotFile != want.filename {
			t.Errorf("N-10b：hub(msg_id=%s).Extra.filename = %q，want %q", msgID, gotFile, want.filename)
		}
		if _, ok := downloaded.Load(mediaID); !ok {
			t.Errorf("media %s 没有被下载（应为该行的媒体各起一次转存）", mediaID)
		}
	}

	// 非媒体行、以及"type 说是图片但没带 image 对象"的畸形行，都不该被回填，
	// 也不该起一次 media_id 为空的下载
	// 先收拢多余任务：只等到"前两条到齐"就判覆盖，漏得掉多起来的第三条
settle:
	for {
		select {
		case c := <-calls:
			seen[c.mediaID] = c
		case <-time.After(400 * time.Millisecond):
			break settle
		}
	}
	if len(seen) != 2 {
		t.Errorf("转存任务数 = %d，want 2（已见 %v）", len(seen), f2Keys(seen))
	}
	for _, msgID := range []string{"wamid-f2-2", "wamid-f2-4"} {
		var other model.MessageHub
		if err := db.Where("platform = ? AND account_id = ? AND msg_id = ?", "whatsapp", f2AccountID, msgID).
			First(&other).Error; err != nil {
			t.Fatalf("load hub %s: %v", msgID, err)
		}
		if other.MediaURL != "" {
			t.Errorf("hub(msg_id=%s) 不该有 media_url，got %q", msgID, other.MediaURL)
		}
		if got := f2MediaIDOf(t, other.Extra); got != "" {
			t.Errorf("hub(msg_id=%s).Extra.media_id = %q，want 空", msgID, got)
		}
	}

	// 文件名要跟着各自的媒体走（文档带 filename，图片不带）
	if c := seen["wa-media-f2-doc"]; c.filename != "报价单.pdf" {
		t.Errorf("文档转存 filename = %q，want 报价单.pdf", c.filename)
	}
	if c := seen["wa-media-f2-image"]; c.filename != "" {
		t.Errorf("图片转存 filename = %q，want 空（图片消息没有 filename 字段）", c.filename)
	}
}

// f2WaitHubMediaURL 轮询等回填：转存协程是「下载 → 转存 → 回填」三步，
// 测试里替身返回的那一刻还没写库，直接读会把竞态误读成缺陷。
func f2WaitHubMediaURL(t *testing.T, db *gorm.DB, msgID string) model.MessageHub {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var hub model.MessageHub
	for {
		hub = model.MessageHub{}
		if err := db.Where("platform = ? AND account_id = ? AND msg_id = ?", "whatsapp", f2AccountID, msgID).
			First(&hub).Error; err == nil && hub.MediaURL != "" {
			return hub
		}
		if time.Now().After(deadline) {
			return hub
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func f2Keys(m map[string]f2StoreCall) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func f2JSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%+v", v)
	}
	return string(b)
}

func f2MediaIDOf(t *testing.T, extra model.JSONMap) string {
	t.Helper()
	if extra == nil {
		return ""
	}
	s, _ := extra["media_id"].(string)
	return s
}
