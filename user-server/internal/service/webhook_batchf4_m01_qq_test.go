package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// 批F-4 / M-01（QQ 半场）：官方消息事件的富媒体在 d.attachments[]，修复前该字段
// 在 GroupMessageData/C2CMessageData 里根本没有承载位，MsgType 一律写死 text、
// 空正文兜成 "[qq]" ⇒ 客户发的图片/视频/语音/文件全部蒸发，AI 也拿不到任何线索。
//
// 官方口径（bot.q.qq.com/wiki/develop/api-v2/autogen/event/group_message_create.html）：
// attachments[] 每项含 url / filename / width / height / size / content_type /
// voice_wav_url / asr_refer_text，content_type 取值 image/jpeg|image/png|image/gif|
// video/mp4|voice|file；url 直接 GET 即得字节（没有「先换下载链接」这一步），
// 但它是 CDN 临时链接且官方未公布有效期 ⇒ 与钉钉 downloadCode 同口径当场转存。
//
// 出站腿：官方发送接口的 msg_id 说明为「从 GROUP_AT_MESSAGE_CREATE 等事件的 d.id 获取，
// 5 分钟内有效」，而 hub.Extra.channel_msg_id 原先带内部命名空间前缀 qq_，
// 每条 AI 回复都会因 msg_id 不被识别而失败。

// f4QQHubRow 读回的一行 hub。
type f4QQHubRow struct {
	MsgType  string
	Content  string
	MediaURL string
	Extra    model.JSONMap
}

// f4QQLoadHub 轮询读回：media_url 由异步转存回填，直读会把竞态误读成缺陷。
func f4QQLoadHub(t *testing.T, db *gorm.DB, msgID string, waitMediaURL bool) f4QQHubRow {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var hub model.MessageHub
	var err error
	for {
		hub = model.MessageHub{}
		err = db.Where("platform = ? AND msg_id = ?", "qq", msgID).First(&hub).Error
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
	return f4QQHubRow{MsgType: hub.MsgType, Content: hub.Content, MediaURL: hub.MediaURL, Extra: hub.Extra}
}

// f4QQSetup 起一条 QQ 入站管线（账号 id 固定 1：NewTestDB 每次给全新库，自增从 1 起）。
func f4QQSetup(t *testing.T) (*WebhookService, *gorm.DB) {
	t.Helper()
	db := setupQQDB(t)
	if err := db.Create(&model.QQAccount{
		AccountName: "媒体号", AppID: "app-f4", AppSecret: "sec-f4",
		WebhookSecret: "wh-f4", Status: 1, AIAgentEnabled: true,
	}).Error; err != nil {
		t.Fatalf("seed qq account: %v", err)
	}
	ws := NewWebhookService(db)
	t.Cleanup(func() { ws.Stop(context.Background()) })
	return ws, db
}

// f4QQEvent 拼一条群 @ 事件；atts 为 attachments[] 的 JSON 片段（空=无附件）。
func f4QQEvent(evtID, msgID, content, atts string) []byte {
	d := `{"id":"` + msgID + `","group_openid":"G-f4-` + evtID + `","content":"` + content +
		`","author":{"member_openid":"M-f4-` + evtID + `"}`
	if atts != "" {
		d += `,"attachments":` + atts
	}
	return []byte(`{"id":"` + evtID + `","op":0,"t":"GROUP_AT_MESSAGE_CREATE","d":` + d + `}}`)
}

// f4QQStubMedia 换掉下载/转存两条腿，返回「下载到的 url」「转存的 mediaID|文件名」通道。
//
// 桩里的字节 = data + url，而用例的 url 一律形如 …/pic-0.png（下标写在文件名里），
// 于是「mediaID 尾号 ↔ 字节里的下标」就是每次转存拿到的确实是对应那条附件的证据；
// 少了这一步，多附件用例可以全靠串号蒙绿。
func f4QQStubMedia(t *testing.T, data string) (urls chan string, stored chan string) {
	t.Helper()
	prevFetch, prevStore := qqMediaFetchFn, qqMediaStoreFn
	t.Cleanup(func() { qqMediaFetchFn, qqMediaStoreFn = prevFetch, prevStore })
	urls = make(chan string, 8)
	stored = make(chan string, 8)
	qqMediaFetchFn = func(ctx context.Context, rawURL string) ([]byte, string, error) {
		urls <- rawURL
		return []byte(data + rawURL), "image/png", nil
	}
	qqMediaStoreFn = func(ctx context.Context, channel, mediaID string, b []byte, contentType, filenameHint string) (string, error) {
		if channel != "qq" {
			return "", fmt.Errorf("stub: channel = %q, want qq", channel)
		}
		i := strings.LastIndex(mediaID, "_")
		if i < 0 {
			return "", fmt.Errorf("stub: mediaID %q 没带附件下标", mediaID)
		}
		idx := mediaID[i+1:]
		if !strings.Contains(string(b), "-"+idx+".") {
			return "", fmt.Errorf("stub: 字节与 mediaID 对不上（拿错素材了）mediaID=%s data=%s", mediaID, b)
		}
		stored <- mediaID + "|" + filenameHint
		return "/files/" + channel + "/" + mediaID, nil
	}
	return urls, stored
}

func TestM01_QQImageIsStoredAndBackfilled(t *testing.T) {
	ws, db := f4QQSetup(t)
	evtID := fmt.Sprintf("f4qqimg%d", time.Now().UnixNano())
	urls, stored := f4QQStubMedia(t, "bytes-")

	raw := f4QQEvent(evtID, "ROBOT1.0_"+evtID, "看下这张",
		`[{"url":"http://gchat.qpic.cn/pic-0.png","filename":"pic.png","content_type":"image/png","width":10,"height":8,"size":100}]`)
	hub, err := ws.dispatchQQ(context.Background(), "1", &ParsedPayload{}, raw)
	if err != nil {
		t.Fatalf("dispatchQQ: %v", err)
	}
	if hub == nil {
		t.Fatalf("dispatchQQ 报错为 nil 却交回 nil 行（(nil, nil) 那一支被走到 ⇒ 用例前置没满足）")
	}
	if hub.MsgType != model.MsgTypeImage {
		t.Errorf("hub.msg_type = %q, want image（官方 content 非空但附件是图片）", hub.MsgType)
	}
	if hub.Content != "看下这张[图片]" {
		t.Errorf("hub.content = %q, want 看下这张[图片]", hub.Content)
	}

	select {
	case got := <-urls:
		if got != "http://gchat.qpic.cn/pic-0.png" {
			t.Errorf("下载用的 url = %q, want 事件里那条 attachments[].url", got)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("M-01 未达成：15s 内没有起过一次 QQ 附件下载")
	}
	hubMsgID := "qq_evt_" + evtID
	wantURL := "/files/qq/" + hubMsgID + "_0"
	select {
	case got := <-stored:
		want := hubMsgID + "_0|0_pic.png"
		if got != want {
			t.Errorf("转存键 = %q, want %q（mediaID 要能定位到具体附件，文件名要带官方原名）", got, want)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("M-01 未达成：15s 内没有完成一次转存")
	}

	row := f4QQLoadHub(t, db, hubMsgID, true)
	if row.MediaURL != wantURL {
		t.Errorf("M-01 未达成：hub.media_url = %q, want %q（临时链接没换成长期 URL）", row.MediaURL, wantURL)
	}
	if got := fmt.Sprint(row.Extra["media_urls"]); got != "["+wantURL+"]" {
		t.Errorf("Extra.media_urls = %s, want [%s]", got, wantURL)
	}
}

// TestM01_QQMultiAttachmentStoresEveryOne 一条消息两张图：两张都要各自转存。
// 只存首张正是 N-10 在 WhatsApp 侧修过的错，QQ 不能重犯。
func TestM01_QQMultiAttachmentStoresEveryOne(t *testing.T) {
	ws, db := f4QQSetup(t)
	evtID := fmt.Sprintf("f4qqmulti%d", time.Now().UnixNano())
	_, stored := f4QQStubMedia(t, "media-")

	raw := f4QQEvent(evtID, "ROBOT1.0_"+evtID, "对比下",
		`[{"url":"http://gchat.qpic.cn/multi-0.png","content_type":"image/png"},`+
			`{"url":"http://gchat.qpic.cn/multi-1.png","content_type":"image/png"}]`)
	hub, err := ws.dispatchQQ(context.Background(), "1", &ParsedPayload{}, raw)
	if err != nil {
		t.Fatalf("dispatchQQ: %v", err)
	}
	if hub == nil {
		t.Fatalf("dispatchQQ 报错为 nil 却交回 nil 行（(nil, nil) 那一支被走到 ⇒ 用例前置没满足）")
	}
	if hub.Content != "对比下[图片][图片]" {
		t.Errorf("正文 = %q, want 对比下[图片][图片]（第二张的痕不能丢）", hub.Content)
	}
	hubMsgID := "qq_evt_" + evtID
	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		select {
		case s := <-stored:
			seen[s] = true
		case <-time.After(15 * time.Second):
			t.Fatalf("M-01 未达成：只完成 %d 次转存，want 2（已见 %v）", i, seen)
		}
	}
	// 断 mediaID|hint 这一整对：只断 mediaID 的下标，文件名 hint 写成常量下标
	// （同名两张在存储侧仍会覆盖成一张）就没人咬得住。
	for i := 0; i < 2; i++ {
		if want := fmt.Sprintf("%s_%d|%d_", hubMsgID, i, i); !seen[want] {
			t.Errorf("两张图都该各自转存且文件名 hint 各带自己的下标，got %v（缺 %s）", seen, want)
		}
	}

	row := f4QQLoadHub(t, db, hubMsgID, true)
	wantURLs := "[" + "/files/qq/" + hubMsgID + "_0" + " " + "/files/qq/" + hubMsgID + "_1" + "]"
	if got := fmt.Sprint(row.Extra["media_urls"]); got != wantURLs {
		t.Errorf("Extra.media_urls = %s, want 两张都在且按序（%s）", got, wantURLs)
	}
	if row.MediaURL != "/files/qq/"+hubMsgID+"_0" {
		t.Errorf("hub.media_url = %q, want 首张", row.MediaURL)
	}
}

// TestM01_QQTextOnlyDoesNotFetchMedia 纯文本不得起任何下载腿。
func TestM01_QQTextOnlyDoesNotFetchMedia(t *testing.T) {
	ws, db := f4QQSetup(t)
	evtID := fmt.Sprintf("f4qqtxt%d", time.Now().UnixNano())
	called := 0
	prevFetch, prevStore := qqMediaFetchFn, qqMediaStoreFn
	t.Cleanup(func() { qqMediaFetchFn, qqMediaStoreFn = prevFetch, prevStore })
	qqMediaFetchFn = func(ctx context.Context, rawURL string) ([]byte, string, error) {
		called++
		return nil, "", fmt.Errorf("stub: 不该被调用")
	}
	qqMediaStoreFn = func(ctx context.Context, channel, mediaID string, b []byte, contentType, hint string) (string, error) {
		called++
		return "", nil
	}

	raw := f4QQEvent(evtID, "ROBOT1.0_"+evtID, "就一句话", "")
	hub, err := ws.dispatchQQ(context.Background(), "1", &ParsedPayload{}, raw)
	if err != nil {
		t.Fatalf("dispatchQQ: %v", err)
	}
	if hub == nil {
		t.Fatalf("dispatchQQ 报错为 nil 却交回 nil 行（(nil, nil) 那一支被走到 ⇒ 用例前置没满足）")
	}
	if hub.MsgType != model.MsgTypeText || hub.Content != "就一句话" {
		t.Errorf("纯文本被改写了: type=%q content=%q", hub.MsgType, hub.Content)
	}
	time.Sleep(500 * time.Millisecond)
	if called != 0 {
		t.Errorf("纯文本起了 %d 次媒体腿，want 0", called)
	}
	row := f4QQLoadHub(t, db, "qq_evt_"+evtID, false)
	if row.MediaURL != "" {
		t.Errorf("纯文本行不该有 media_url，got %q", row.MediaURL)
	}
}

// TestM01_QQDownloadFailureKeepsPlaceholderRow 下载失败只丢媒体，不能把正文/类型一起拖没，
// 也不能留下半截 media_urls。
func TestM01_QQDownloadFailureKeepsPlaceholderRow(t *testing.T) {
	ws, db := f4QQSetup(t)
	evtID := fmt.Sprintf("f4qqfail%d", time.Now().UnixNano())
	prevFetch, prevStore := qqMediaFetchFn, qqMediaStoreFn
	t.Cleanup(func() { qqMediaFetchFn, qqMediaStoreFn = prevFetch, prevStore })
	qqMediaFetchFn = func(ctx context.Context, rawURL string) ([]byte, string, error) {
		return nil, "", fmt.Errorf("stub: CDN 404")
	}
	qqMediaStoreFn = func(ctx context.Context, channel, mediaID string, b []byte, contentType, hint string) (string, error) {
		t.Error("stub: 下载失败时不该继续转存")
		return "", nil
	}

	raw := f4QQEvent(evtID, "ROBOT1.0_"+evtID, "",
		`[{"url":"http://gchat.qpic.cn/gone.png","content_type":"image/png"}]`)
	hub, err := ws.dispatchQQ(context.Background(), "1", &ParsedPayload{}, raw)
	if err != nil {
		t.Fatalf("dispatchQQ 不能因媒体失败而报错: %v", err)
	}
	row := f4QQLoadHub(t, db, "qq_evt_"+evtID, false)
	if row.MsgType != model.MsgTypeImage || row.Content != "[图片]" {
		t.Errorf("媒体失败后类型/正文丢了: type=%q content=%q hub=%+v", row.MsgType, row.Content, hub)
	}
	if row.MediaURL != "" {
		t.Errorf("下载失败却写了 media_url = %q", row.MediaURL)
	}
}

// TestM01_QQOutboundMsgIDIsOfficialDID 出站被动回复关联 ID 必须是官方 d.id 原值。
func TestM01_QQOutboundMsgIDIsOfficialDID(t *testing.T) {
	ws, _ := f4QQSetup(t)
	evtID := fmt.Sprintf("f4qqmsgid%d", time.Now().UnixNano())
	_, _ = f4QQStubMedia(t, "x-")
	msgID := "ROBOT1.0_" + evtID
	raw := f4QQEvent(evtID, msgID, "在吗", "")

	hub, err := ws.dispatchQQ(context.Background(), "1", &ParsedPayload{}, raw)
	if err != nil {
		t.Fatalf("dispatchQQ: %v", err)
	}
	if hub == nil {
		t.Fatalf("dispatchQQ 报错为 nil 却交回 nil 行（(nil, nil) 那一支被走到 ⇒ 用例前置没满足）")
	}
	if got := QQOutboundMsgID(hub); got != msgID {
		t.Errorf("M-01 未达成：QQOutboundMsgID = %q, want %q"+
			"（内部前缀 qq_ 进出站报文会被平台判为 msg_id 有误，每条 AI 回复都发不出去）", got, msgID)
	}
	// hub.MsgID 与中台落库口径一致（媒体回填按它找行）。
	if hub.MsgID != "qq_evt_"+evtID {
		t.Errorf("hub.MsgID = %q, want qq_evt_%s（与 Ingress 落库 msg_id 同源）", hub.MsgID, evtID)
	}
	db2 := ws.lazyDB()
	row := f4QQLoadHub(t, db2, "qq_evt_"+evtID, false)
	if got, _ := row.Extra["channel_msg_id"].(string); got != msgID {
		t.Errorf("落库 Extra.channel_msg_id = %q, want %q", got, msgID)
	}
}

// TestM01_QQFetchUsesRealAttachmentURLAndBytes 下载腿用真 HTTP 走一遍：
// 只 stub 掉转存、并把取址策略临时放开（默认策略按设计拒绝环回地址，不放开这条正向腿
// 就永远没有证据），证明事件里那条 url 真的被 GET、字节完整到手（不截断、不串号）。
func TestM01_QQFetchUsesRealAttachmentURLAndBytes(t *testing.T) {
	ws, db := f4QQSetup(t)
	prevGuard := qqAttachmentURLGuard
	t.Cleanup(func() { qqAttachmentURLGuard = prevGuard })
	qqAttachmentURLGuard = func(string) error { return nil }

	payload := strings.Repeat("Q", 4096)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, ".png") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte(payload))
	}))
	defer srv.Close()

	prevStore := qqMediaStoreFn
	var gotData []byte
	var gotCT, gotHint string
	t.Cleanup(func() { qqMediaStoreFn = prevStore })
	qqMediaStoreFn = func(ctx context.Context, channel, mediaID string, b []byte, contentType, hint string) (string, error) {
		gotData, gotCT, gotHint = b, contentType, hint
		return "/files/qq/" + mediaID, nil
	}

	evtID := fmt.Sprintf("f4qqreal%d", time.Now().UnixNano())
	raw := f4QQEvent(evtID, "ROBOT1.0_"+evtID, "",
		`[{"url":"`+srv.URL+`/real.png","filename":"real.png","content_type":"image/png"}]`)
	if _, err := ws.dispatchQQ(context.Background(), "1", &ParsedPayload{}, raw); err != nil {
		t.Fatalf("dispatchQQ: %v", err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for gotHint == "" && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if string(gotData) != payload {
		t.Errorf("转存字节数 = %d, want %d（下载腿把文件读残了）", len(gotData), len(payload))
	}
	if gotCT != "image/png" {
		t.Errorf("contentType = %q, want 响应头里的 image/png", gotCT)
	}
	if gotHint != "0_real.png" {
		t.Errorf("文件名 hint = %q, want 0_real.png（带序号防同名覆盖 + 保留官方原名）", gotHint)
	}
	if row := f4QQLoadHub(t, db, "qq_evt_"+evtID, true); row.MediaURL == "" {
		t.Error("media_url 未回填")
	}
}

// TestM01_QQFetchRejectsNonHTTPAndLoopback url 来自事件报文：验签只保证「出自 QQ」，
// 不能把机器人回调端点变成内网探针或本地文件读取口。
func TestM01_QQFetchRejectsNonHTTPAndLoopback(t *testing.T) {
	for _, rawURL := range []string{
		"file:///etc/passwd",
		"http://127.0.0.1:8232/",
		"http://localhost:8232/x.png",
		"ftp://gchat.qpic.cn/x.png",
		"   ",
	} {
		data, ct, err := FetchQQAttachment(context.Background(), rawURL)
		if err == nil {
			t.Errorf("M-01 未达成：%q 竟然放行（ct=%s len=%d）", rawURL, ct, len(data))
		}
	}
	// 反向闸：策略不能是「一律拒绝」，否则上面五条全是白给的绿，真实 QQ 链接也永远下不来。
	for _, ok := range []string{
		"http://gchat.qpic.cn/gchatpic-new/direct/123456/0/xxx_.png?w=1024",
		"https://pb1.qqphotoss.com/xxx.jpg",
	} {
		if err := rejectInternalAttachmentURL(ok); err != nil {
			t.Errorf("官方 CDN 链接被误拒：%q → %v", ok, err)
		}
	}
}

// TestM01_QQOversizedAttachmentSkippedBeforeDownload 官方 size 超过入站上限时不起下载
// （否则一条消息就能拉满 64MB 内存）。
func TestM01_QQOversizedAttachmentSkippedBeforeDownload(t *testing.T) {
	ws, db := f4QQSetup(t)
	evtID := fmt.Sprintf("f4qqbig%d", time.Now().UnixNano())
	called := 0
	prevFetch, prevStore := qqMediaFetchFn, qqMediaStoreFn
	t.Cleanup(func() { qqMediaFetchFn, qqMediaStoreFn = prevFetch, prevStore })
	qqMediaFetchFn = func(ctx context.Context, rawURL string) ([]byte, string, error) {
		called++
		return []byte("x"), "image/png", nil
	}
	qqMediaStoreFn = func(ctx context.Context, channel, mediaID string, b []byte, contentType, hint string) (string, error) {
		called++
		return "/files/qq/" + mediaID, nil
	}
	raw := f4QQEvent(evtID, "ROBOT1.0_"+evtID, "",
		fmt.Sprintf(`[{"url":"http://gchat.qpic.cn/big.png","content_type":"image/png","size":%d}]`, maxInboundMediaBytes+1))
	if _, err := ws.dispatchQQ(context.Background(), "1", &ParsedPayload{}, raw); err != nil {
		t.Fatalf("dispatchQQ: %v", err)
	}
	row := f4QQLoadHub(t, db, "qq_evt_"+evtID, false)
	if row.MsgType != model.MsgTypeImage {
		t.Errorf("类型仍要是 image，got %q", row.MsgType)
	}
	time.Sleep(500 * time.Millisecond)
	if called != 0 {
		t.Errorf("超限附件起了 %d 次媒体腿，want 0", called)
	}
	if row.MediaURL != "" {
		t.Errorf("超限附件却写了 media_url = %q", row.MediaURL)
	}
}

// f4QQOpenGuardAndLimit 放开取址策略并按需压低字节上限（两条腿都要真 HTTP 才有效）。
func f4QQOpenGuard(t *testing.T, limit int64) {
	t.Helper()
	prevGuard, prevLimit := qqAttachmentURLGuard, qqMaxMediaBytes
	t.Cleanup(func() { qqAttachmentURLGuard, qqMaxMediaBytes = prevGuard, prevLimit })
	qqAttachmentURLGuard = func(string) error { return nil }
	if limit > 0 {
		qqMaxMediaBytes = limit
	}
}

// TestM01_QQTruncatedDownloadIsRejected 响应体超过上限时必须整体拒收，
// 而不是把截断后的前半段当完整图片转存（LimitReader 静默截断，存下去就是坏文件）。
func TestM01_QQTruncatedDownloadIsRejected(t *testing.T) {
	ws, db := f4QQSetup(t)
	f4QQOpenGuard(t, 100)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("Z", 200)))
	}))
	defer srv.Close()

	storedCalled := 0
	prevStore := qqMediaStoreFn
	t.Cleanup(func() { qqMediaStoreFn = prevStore })
	qqMediaStoreFn = func(ctx context.Context, channel, mediaID string, b []byte, contentType, hint string) (string, error) {
		storedCalled++
		return "/files/qq/" + mediaID, nil
	}

	evtID := fmt.Sprintf("f4qqtrunc%d", time.Now().UnixNano())
	raw := f4QQEvent(evtID, "ROBOT1.0_"+evtID, "",
		`[{"url":"`+srv.URL+`/big.png","filename":"big.png","content_type":"image/png"}]`)
	if _, err := ws.dispatchQQ(context.Background(), "1", &ParsedPayload{}, raw); err != nil {
		t.Fatalf("dispatchQQ: %v", err)
	}
	time.Sleep(800 * time.Millisecond)
	if storedCalled != 0 {
		t.Errorf("200 字节 > 上限 100 却转存了 %d 次（存进去的就是半张图）", storedCalled)
	}
	if row := f4QQLoadHub(t, db, "qq_evt_"+evtID, false); row.MediaURL != "" {
		t.Errorf("拒收后仍写了 media_url = %q", row.MediaURL)
	}
}

// TestM01_QQNon200DownloadIsNotStored CDN 回 404/403 时那段错误页不是媒体字节。
func TestM01_QQNon200DownloadIsNotStored(t *testing.T) {
	ws, db := f4QQSetup(t)
	f4QQOpenGuard(t, 0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"code":40034102,"message":"resource gone"}`, http.StatusNotFound)
	}))
	defer srv.Close()

	storedCalled := 0
	prevStore := qqMediaStoreFn
	t.Cleanup(func() { qqMediaStoreFn = prevStore })
	qqMediaStoreFn = func(ctx context.Context, channel, mediaID string, b []byte, contentType, hint string) (string, error) {
		storedCalled++
		return "/files/qq/" + mediaID, nil
	}

	evtID := fmt.Sprintf("f4qq404%d", time.Now().UnixNano())
	raw := f4QQEvent(evtID, "ROBOT1.0_"+evtID, "",
		`[{"url":"`+srv.URL+`/gone.png","filename":"gone.png","content_type":"image/png"}]`)
	if _, err := ws.dispatchQQ(context.Background(), "1", &ParsedPayload{}, raw); err != nil {
		t.Fatalf("dispatchQQ: %v", err)
	}
	time.Sleep(800 * time.Millisecond)
	if storedCalled != 0 {
		t.Errorf("404 响应体被当媒体转存了 %d 次", storedCalled)
	}
	row := f4QQLoadHub(t, db, "qq_evt_"+evtID, false)
	if row.Content != "[图片]" || row.MsgType != model.MsgTypeImage {
		t.Errorf("下载失败仍要保住类型与占位符，got type=%q content=%q", row.MsgType, row.Content)
	}
}
