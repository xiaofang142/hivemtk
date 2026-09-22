package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/channelbot/telegram"
	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// 批F-4b / M-01（Telegram 半场）：修复前 TGMessage 只声明 Text/Caption，官方七个媒体容器
// （photo/video/video_note/voice/audio/document/sticker，外加 animation 与 live_photo）
// 在解析阶段就被丢掉，MsgType 又写死 text ⇒ 客户发的图片最终落成一行的 "[private]"。
//
// 官方口径（core.telegram.org/bots/api）：媒体的 file_id 必须先 getFile 换 file_path，
// 再按 https://api.telegram.org/file/bot<token>/<file_path> 下载；「link will be valid for at
// least 1 hour」「The maximum file size to download is 20 MB」。

// f4TGStubMedia 换掉下载/转存两条腿，返回「(token,file_id) 调用记录」与「mediaID|文件名 hint」通道。
//
// 桩里的字节 = data + fileID，而用例的 file_id 一律形如 …_0 / …_1（下标写在 id 里），
// 于是「mediaID 尾号 ↔ 字节里的 id」就是每次转存拿到的是确实那条媒体的证据。
func f4TGStubMedia(t *testing.T, data string) (fetched chan [2]string, stored chan string) {
	t.Helper()
	prevFetch, prevStore := tgMediaFetchFn, tgMediaStoreFn
	t.Cleanup(func() { tgMediaFetchFn, tgMediaStoreFn = prevFetch, prevStore })
	fetched = make(chan [2]string, 8)
	stored = make(chan string, 8)
	tgMediaFetchFn = func(ctx context.Context, token, fileID string) ([]byte, string, error) {
		fetched <- [2]string{token, fileID}
		return []byte(data + fileID), "image/png", nil
	}
	tgMediaStoreFn = func(ctx context.Context, channel, mediaID string, b []byte, contentType, filenameHint string) (string, error) {
		if channel != "telegram" {
			return "", fmt.Errorf("stub: channel = %q, want telegram", channel)
		}
		i := strings.LastIndex(mediaID, "_")
		if i < 0 {
			return "", fmt.Errorf("stub: mediaID %q 没带媒体下标", mediaID)
		}
		idx := mediaID[i+1:]
		if !strings.Contains(string(b), "_"+idx) {
			return "", fmt.Errorf("stub: 字节与 mediaID 对不上（拿错素材了）mediaID=%s data=%s", mediaID, b)
		}
		stored <- mediaID + "|" + filenameHint
		return "/files/" + channel + "/" + mediaID, nil
	}
	return fetched, stored
}

// f4TGSetup 起一条 TG 入站管线并种一个带 token 的账号（id 固定 1：NewTestDB 每次给全新库）。
func f4TGSetup(t *testing.T, token string) (*WebhookService, *gorm.DB) {
	t.Helper()
	db := setupTelegramTestDB(t)
	acc := &model.TelegramAccount{AccountName: "媒体号", BotToken: token, Status: 1}
	if err := db.Create(acc).Error; err != nil {
		t.Fatalf("seed telegram account: %v", err)
	}
	ws := NewWebhookService(db)
	t.Cleanup(func() { ws.Stop(context.Background()) })
	return ws, db
}

// f4TGUpdate 拼一条私聊消息 Update；mediaJSON 形如 `"photo":[{...}]`，空表示纯文本。
func f4TGUpdate(updateID, msgID int64, caption, mediaJSON string) []byte {
	m := `{"message_id":` + fmt.Sprint(msgID) +
		`,"from":{"id":67890,"first_name":"Bob"},"chat":{"id":12345,"type":"private"},"date":1700000000`
	if caption != "" {
		m += `,"caption":"` + caption + `"`
	}
	if mediaJSON != "" {
		m += `,` + mediaJSON
	}
	return []byte(`{"update_id":` + fmt.Sprint(updateID) + `,"message":` + m + `}}`)
}

// f4TGWaitMediaURL 轮询读回：media_url 由异步转存回填，直读会把竞态误读成缺陷。
func f4TGWaitMediaURL(t *testing.T, db *gorm.DB, msgID string, want bool) model.MessageHub {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var hub model.MessageHub
	var err error
	for {
		hub = model.MessageHub{}
		err = db.Where("platform = ? AND msg_id = ?", "telegram", msgID).First(&hub).Error
		if err == nil && (hub.MediaURL != "") == want {
			break
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("读不到 hub 行 msg_id=%s: %v", msgID, err)
	}
	return hub
}

func TestM01_TelegramImageIsStoredAndBackfilled(t *testing.T) {
	const token = "777:AAA-tg-token"
	ws, db := f4TGSetup(t, token)
	upd := time.Now().UnixNano() % 1e9
	fetched, stored := f4TGStubMedia(t, "bytes-")

	raw := f4TGUpdate(upd, 501, "看下这张",
		`"photo":[{"file_id":"pic_0_small","file_size":10},{"file_id":"pic_0","file_size":90000}]`)
	p := &ParsedPayload{}
	hub, _, err := ws.dispatchTelegram(context.Background(), "1", p, raw)
	if err != nil {
		t.Fatalf("dispatchTelegram: %v", err)
	}
	// AI 侧读的是 p.Content，工作台读的是 hub.Content ⇒ 两者必须同源，
	// 否则「客户发了什么」在两条链路上是两个答案。
	if p.Content != "看下这张[图片]" {
		t.Errorf("p.Content = %q, want 看下这张[图片]（与 hub.content 同源，AI 才知道有图）", p.Content)
	}
	if hub.MsgType != model.MsgTypeImage {
		t.Errorf("hub.msg_type = %q, want image（官方 photo 容器）", hub.MsgType)
	}
	if hub.Content != "看下这张[图片]" {
		t.Errorf("hub.content = %q, want 看下这张[图片]", hub.Content)
	}
	hubMsgID := fmt.Sprintf("tg_upd_1_%d", upd)
	if hub.MsgID != hubMsgID {
		t.Errorf("hub.MsgID = %q, want %q（与 Ingress 落库键同源，否则回填永远找不到那行）", hub.MsgID, hubMsgID)
	}

	wantURL := "/files/telegram/" + hubMsgID + "_0"
	select {
	case got := <-fetched:
		if got[0] != token {
			t.Errorf("getFile 用的 token = %q, want 账号里的 %q", got[0], token)
		}
		// 官方 photo 是「同一张图的多个可用尺寸」⇒ 归一层必须挑最大档，而不是数组首项。
		if got[1] != "pic_0" {
			t.Errorf("下载了 file_id = %q, want 最大档 pic_0（取首档会把 10 字节的缩略图当原件存）", got[1])
		}
	case <-time.After(15 * time.Second):
		t.Fatal("M-01 未达成：15s 内没有起过一次 TG 媒体下载")
	}
	select {
	case got := <-stored:
		if want := hubMsgID + "_0|0_"; got != want {
			t.Errorf("转存键 = %q, want %q", got, want)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("M-01 未达成：15s 内没有完成一次转存")
	}

	row := f4TGWaitMediaURL(t, db, hubMsgID, true)
	if row.MediaURL != wantURL {
		t.Errorf("M-01 未达成：hub.media_url = %q, want %q（1 小时临时链接没换成长期 URL）", row.MediaURL, wantURL)
	}
	if got := fmt.Sprint(row.Extra["media_urls"]); got != "["+wantURL+"]" {
		t.Errorf("Extra.media_urls = %s, want [%s]", got, wantURL)
	}
	// file_id 官方可复用（与 WA 的 7 天 media_id 不同）⇒ 留痕，链接过期后还能重取。
	if got := fmt.Sprint(row.Extra["media_file_ids"]); got != "[pic_0]" {
		t.Errorf("Extra.media_file_ids = %s, want [pic_0]", got)
	}
	if row.MsgType != model.MsgTypeImage || row.Content != "看下这张[图片]" {
		t.Errorf("落库行的类型/正文被改写: type=%q content=%q", row.MsgType, row.Content)
	}
}

func TestM01_TelegramDocumentKeepsFileName(t *testing.T) {
	ws, db := f4TGSetup(t, "777:tok")
	upd := time.Now().UnixNano() % 1e9
	_, stored := f4TGStubMedia(t, "m-")

	raw := f4TGUpdate(upd, 502, "报价在这",
		`"document":{"file_id":"doc_0_x.pdf","file_size":2048,"file_name":"报价单.pdf","mime_type":"application/pdf"}`)
	hub, _, err := ws.dispatchTelegram(context.Background(), "1", &ParsedPayload{}, raw)
	if err != nil {
		t.Fatalf("dispatchTelegram: %v", err)
	}
	if hub.MsgType != model.MsgTypeFile {
		t.Errorf("msg_type = %q, want file", hub.MsgType)
	}
	if hub.Content != "报价在这[文件] 报价单.pdf" {
		t.Errorf("content = %q, want 报价在这[文件] 报价单.pdf（官方原名要留在正文里）", hub.Content)
	}
	hubMsgID := fmt.Sprintf("tg_upd_1_%d", upd)
	select {
	case got := <-stored:
		// hint 带序号 + 官方原名：同一批次多张同名文件在存储侧才不会互相覆盖。
		if want := hubMsgID + "_0|0_报价单.pdf"; got != want {
			t.Errorf("转存键 = %q, want %q", got, want)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("M-01 未达成：文档没转存")
	}
	if row := f4TGWaitMediaURL(t, db, hubMsgID, true); row.MediaURL == "" {
		t.Error("media_url 未回填")
	}
}

// TestM01_TelegramVoiceIsAudioKind 官方 voice 与 audio 都归到本仓 audio 类
// （hub 的 msg_type 集合里没有 voice，写个没人认得的值等于工作台渲染成未知类型）。
func TestM01_TelegramVoiceIsAudioKind(t *testing.T) {
	ws, db := f4TGSetup(t, "777:tok")
	upd := time.Now().UnixNano() % 1e9
	_, _ = f4TGStubMedia(t, "v-")
	raw := f4TGUpdate(upd, 503, "", `"voice":{"file_id":"vo_0_ogg","file_size":640}`)
	hub, _, err := ws.dispatchTelegram(context.Background(), "1", &ParsedPayload{}, raw)
	if err != nil {
		t.Fatalf("dispatchTelegram: %v", err)
	}
	if hub.MsgType != model.MsgTypeAudio {
		t.Errorf("msg_type = %q, want audio", hub.MsgType)
	}
	if hub.Content != "[语音]" {
		t.Errorf("content = %q, want [语音]", hub.Content)
	}
	if row := f4TGWaitMediaURL(t, db, fmt.Sprintf("tg_upd_1_%d", upd), true); row.Content != "[语音]" {
		t.Errorf("落库正文 = %q, want [语音]", row.Content)
	}
}

// TestM01_TelegramGIFNotMistakenForDocument 官方：animation 置位时 document 也会置位。
// 判序反了就把动图存成压缩包（工作台渲染成下载链接，客户看到的是"一个文件"）。
func TestM01_TelegramGIFNotMistakenForDocument(t *testing.T) {
	ws, _ := f4TGSetup(t, "777:tok")
	upd := time.Now().UnixNano() % 1e9
	_, _ = f4TGStubMedia(t, "g-")
	raw := f4TGUpdate(upd, 504, "",
		`"animation":{"file_id":"an_0_gif","file_size":1200,"file_name":"cat.gif","mime_type":"image/gif"},`+
			`"document":{"file_id":"doc_0_gif","file_size":1200,"file_name":"cat.gif"}`)
	hub, _, err := ws.dispatchTelegram(context.Background(), "1", &ParsedPayload{}, raw)
	if err != nil {
		t.Fatalf("dispatchTelegram: %v", err)
	}
	if hub.MsgType != model.MsgTypeVideo {
		t.Errorf("msg_type = %q, want video（GIF 在官方归 animation）", hub.MsgType)
	}
	if hub.Content != "[动图]" {
		t.Errorf("content = %q, want [动图]", hub.Content)
	}
}

// TestM01_TelegramTextOnlyDoesNotFetchMedia 纯文本不得起任何下载腿（每条文本消息都发一次
// getFile 等于白烧一轮 RTT 与限流预算）。
func TestM01_TelegramTextOnlyDoesNotFetchMedia(t *testing.T) {
	ws, db := f4TGSetup(t, "777:tok")
	upd := time.Now().UnixNano() % 1e9
	called := 0
	prevFetch, prevStore := tgMediaFetchFn, tgMediaStoreFn
	t.Cleanup(func() { tgMediaFetchFn, tgMediaStoreFn = prevFetch, prevStore })
	tgMediaFetchFn = func(ctx context.Context, token, fileID string) ([]byte, string, error) {
		called++
		return nil, "", fmt.Errorf("stub: 不该被调用")
	}
	tgMediaStoreFn = func(ctx context.Context, channel, mediaID string, b []byte, contentType, hint string) (string, error) {
		called++
		return "", nil
	}

	body := []byte(`{"update_id":` + fmt.Sprint(upd) +
		`,"message":{"message_id":505,"from":{"id":1,"first_name":"B"},"chat":{"id":2,"type":"private"},"date":1700000000,"text":"就一句话"}}`)
	hub, _, err := ws.dispatchTelegram(context.Background(), "1", &ParsedPayload{}, body)
	if err != nil {
		t.Fatalf("dispatchTelegram: %v", err)
	}
	if hub.MsgType != model.MsgTypeText || hub.Content != "就一句话" {
		t.Errorf("纯文本被改写了: type=%q content=%q", hub.MsgType, hub.Content)
	}
	time.Sleep(500 * time.Millisecond)
	if called != 0 {
		t.Errorf("纯文本起了 %d 次媒体腿，want 0", called)
	}
	row := f4TGWaitMediaURL(t, db, fmt.Sprintf("tg_upd_1_%d", upd), false)
	if row.MediaURL != "" {
		t.Errorf("纯文本行不该有 media_url，got %q", row.MediaURL)
	}
}

// TestM01_TelegramDownloadFailureKeepsPlaceholderRow 下载失败只丢媒体：占位符与类型必须留下，
// 且不能写出半截 media_urls。
func TestM01_TelegramDownloadFailureKeepsPlaceholderRow(t *testing.T) {
	ws, db := f4TGSetup(t, "777:tok")
	upd := time.Now().UnixNano() % 1e9
	prevFetch, prevStore := tgMediaFetchFn, tgMediaStoreFn
	t.Cleanup(func() { tgMediaFetchFn, tgMediaStoreFn = prevFetch, prevStore })
	tgMediaFetchFn = func(ctx context.Context, token, fileID string) ([]byte, string, error) {
		return nil, "", fmt.Errorf("stub: getFile 404")
	}
	tgMediaStoreFn = func(ctx context.Context, channel, mediaID string, b []byte, contentType, hint string) (string, error) {
		t.Error("stub: 下载失败时不该继续转存")
		return "", nil
	}

	raw := f4TGUpdate(upd, 506, "", `"photo":[{"file_id":"p_0","file_size":10}]`)
	hub, _, err := ws.dispatchTelegram(context.Background(), "1", &ParsedPayload{}, raw)
	if err != nil {
		t.Fatalf("dispatchTelegram 不能因媒体失败而报错: %v", err)
	}
	if hub == nil {
		t.Fatal("hub 为空：媒体失败把整条入站也拖没了")
	}
	time.Sleep(500 * time.Millisecond)
	row := f4TGWaitMediaURL(t, db, fmt.Sprintf("tg_upd_1_%d", upd), false)
	if row.MsgType != model.MsgTypeImage || row.Content != "[图片]" {
		t.Errorf("媒体失败后类型/正文丢了: type=%q content=%q", row.MsgType, row.Content)
	}
	if row.MediaURL != "" {
		t.Errorf("下载失败却写了 media_url = %q", row.MediaURL)
	}
	if _, ok := row.Extra["media_urls"]; ok {
		t.Errorf("下载失败却写了 Extra.media_urls = %v", row.Extra["media_urls"])
	}
}

// TestM01_TelegramMissingTokenSkipsPersist 账号没有 bot token 时 getFile 必然 401：
// 不起下载腿，占位符照留（凭证缺失是配置问题，不能变成一条错误媒体）。
func TestM01_TelegramMissingTokenSkipsPersist(t *testing.T) {
	ws, db := f4TGSetup(t, "")
	upd := time.Now().UnixNano() % 1e9
	called := 0
	prevFetch, prevStore := tgMediaFetchFn, tgMediaStoreFn
	t.Cleanup(func() { tgMediaFetchFn, tgMediaStoreFn = prevFetch, prevStore })
	tgMediaFetchFn = func(ctx context.Context, token, fileID string) ([]byte, string, error) {
		called++
		return []byte("x"), "image/png", nil
	}
	tgMediaStoreFn = func(ctx context.Context, channel, mediaID string, b []byte, contentType, hint string) (string, error) {
		called++
		return "/files/telegram/" + mediaID, nil
	}
	raw := f4TGUpdate(upd, 507, "", `"photo":[{"file_id":"p_0","file_size":10}]`)
	if _, _, err := ws.dispatchTelegram(context.Background(), "1", &ParsedPayload{}, raw); err != nil {
		t.Fatalf("dispatchTelegram: %v", err)
	}
	time.Sleep(500 * time.Millisecond)
	if called != 0 {
		t.Errorf("凭证缺失仍起了 %d 次媒体腿，want 0", called)
	}
	if row := f4TGWaitMediaURL(t, db, fmt.Sprintf("tg_upd_1_%d", upd), false); row.MediaURL != "" {
		t.Errorf("凭证缺失却写了 media_url = %q", row.MediaURL)
	}
}

// TestM01_TelegramOversizedMediaSkippedBeforeDownload 官方 file_size 已超上限时不起下载
// （20MB 是官方口径，但不先拦就等于让一条消息把 20MB 拉进内存再丢掉）。
func TestM01_TelegramOversizedMediaSkippedBeforeDownload(t *testing.T) {
	ws, db := f4TGSetup(t, "777:tok")
	upd := time.Now().UnixNano() % 1e9
	prevLimit := tgMaxMediaBytes
	t.Cleanup(func() { tgMaxMediaBytes = prevLimit })
	tgMaxMediaBytes = 100
	called := 0
	prevFetch, prevStore := tgMediaFetchFn, tgMediaStoreFn
	t.Cleanup(func() { tgMediaFetchFn, tgMediaStoreFn = prevFetch, prevStore })
	tgMediaFetchFn = func(ctx context.Context, token, fileID string) ([]byte, string, error) {
		called++
		return []byte("x"), "image/png", nil
	}
	tgMediaStoreFn = func(ctx context.Context, channel, mediaID string, b []byte, contentType, hint string) (string, error) {
		called++
		return "/files/telegram/" + mediaID, nil
	}
	raw := f4TGUpdate(upd, 508, "", fmt.Sprintf(`"video":{"file_id":"v_0","file_size":%d}`, 101))
	hub, _, err := ws.dispatchTelegram(context.Background(), "1", &ParsedPayload{}, raw)
	if err != nil {
		t.Fatalf("dispatchTelegram: %v", err)
	}
	if hub.MsgType != model.MsgTypeVideo {
		t.Errorf("类型仍要是 video，got %q", hub.MsgType)
	}
	time.Sleep(500 * time.Millisecond)
	if called != 0 {
		t.Errorf("超限媒体起了 %d 次媒体腿，want 0", called)
	}
	if row := f4TGWaitMediaURL(t, db, fmt.Sprintf("tg_upd_1_%d", upd), false); row.MediaURL != "" {
		t.Errorf("超限媒体却写了 media_url = %q", row.MediaURL)
	}
}

// f4TGOpenLimit 压低下载上限（真实链路用例用，配合小夹具）。
func f4TGOpenLimit(t *testing.T, limit int64) {
	t.Helper()
	prev := tgMaxMediaBytes
	t.Cleanup(func() { tgMaxMediaBytes = prev })
	if limit > 0 {
		tgMaxMediaBytes = limit
	}
}

// f4TGAPIStub 起一个假的 api.telegram.org：getFile 给 file_path，下载域给字节。
// 返回被请求到的路径序列（含 token 段与 /file/bot 前缀），供用例核对官方口径。
func f4TGAPIStub(t *testing.T, payload string, declaredSize int64, dlStatus int) (base string, paths *sync.Map) {
	t.Helper()
	paths = &sync.Map{}
	var seq []string
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seq = append(seq, r.URL.Path)
		mu.Unlock()
		paths.Store(r.URL.Path, true)
		switch {
		case strings.HasSuffix(r.URL.Path, "/getFile"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"ok":true,"result":{"file_id":"x","file_size":%d,"file_path":"photos/file_1.jpg"}}`, declaredSize)
		case strings.Contains(r.URL.Path, "/file/bot"):
			if dlStatus != 0 && dlStatus != http.StatusOK {
				w.WriteHeader(dlStatus)
				_, _ = w.Write([]byte(`{"error":"gone"}`))
				return
			}
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte(payload))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	prevBase := tgAPIBaseOverride
	t.Cleanup(func() { tgAPIBaseOverride = prevBase })
	tgAPIBaseOverride = srv.URL
	return srv.URL, paths
}

// TestM01_TelegramRealGetFileThenDownload 端到端真 HTTP：事件字节 → 账号 token → getFile →
// 下载 → 转存 → 回填。只 stub 转存这一条腿，其余走真实实现（下载没真跑过就等于没测）。
func TestM01_TelegramRealGetFileThenDownload(t *testing.T) {
	const token = "777:REAL-tok"
	ws, db := f4TGSetup(t, token)
	upd := time.Now().UnixNano() % 1e9
	payload := strings.Repeat("J", 4096)
	_, paths := f4TGAPIStub(t, payload, int64(len(payload)), 0)

	var mu sync.Mutex
	var gotData []byte
	var gotCT, gotHint string
	prevStore := tgMediaStoreFn
	t.Cleanup(func() { tgMediaStoreFn = prevStore })
	tgMediaStoreFn = func(ctx context.Context, channel, mediaID string, b []byte, contentType, hint string) (string, error) {
		mu.Lock()
		gotData, gotCT, gotHint = b, contentType, hint
		mu.Unlock()
		return "/files/telegram/" + mediaID, nil
	}

	hubMsgID := fmt.Sprintf("tg_upd_1_%d", upd)
	raw := f4TGUpdate(upd, 509, "实拍", `"photo":[{"file_id":"real_0","file_size":4096}]`)
	if _, _, err := ws.dispatchTelegram(context.Background(), "1", &ParsedPayload{}, raw); err != nil {
		t.Fatalf("dispatchTelegram: %v", err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		mu.Lock()
		hint := gotHint
		mu.Unlock()
		if hint != "" || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	mu.Lock()
	data, ct := gotData, gotCT
	mu.Unlock()
	if string(data) != payload {
		t.Errorf("转存字节 = %d, want %d（下载腿把原件读残了）", len(data), len(payload))
	}
	if ct != "image/jpeg" {
		t.Errorf("contentType = %q, want 响应头里的 image/jpeg", ct)
	}
	if gotHint != "0_" {
		t.Errorf("文件名 hint = %q, want 0_（photo 无官方原名，仍要带序号）", gotHint)
	}
	// 官方下载域是 /file/bot<token>/<file_path>，与接口段的 /bot<token>/<method> 不同前缀。
	if _, ok := paths.Load("/bot" + token + "/getFile"); !ok {
		t.Errorf("没走到 getFile：请求序列 %v", paths)
	}
	if _, ok := paths.Load("/file/bot" + token + "/photos/file_1.jpg"); !ok {
		t.Error("没走到官方下载域 /file/bot<token>/<file_path>")
	}
	if row := f4TGWaitMediaURL(t, db, hubMsgID, true); row.MediaURL != "/files/telegram/"+hubMsgID+"_0" {
		t.Errorf("media_url = %q", row.MediaURL)
	}
}

// TestM01_TelegramRealTruncationRejected 响应体比上限大时必须整体拒收，
// 而不是把截断后的前半段当完整图片转存（LimitReader 静默截断，存下去就是坏文件）。
func TestM01_TelegramRealTruncationRejected(t *testing.T) {
	ws, db := f4TGSetup(t, "777:tok")
	upd := time.Now().UnixNano() % 1e9
	f4TGOpenLimit(t, 100)
	// getFile 报的 file_size 在上限内（真实场景：元数据与原件不一致），但响应体更大。
	_, _ = f4TGAPIStub(t, strings.Repeat("Z", 200), 50, 0)
	storedCalled := 0
	prevStore := tgMediaStoreFn
	t.Cleanup(func() { tgMediaStoreFn = prevStore })
	tgMediaStoreFn = func(ctx context.Context, channel, mediaID string, b []byte, contentType, hint string) (string, error) {
		storedCalled++
		return "/files/telegram/" + mediaID, nil
	}
	raw := f4TGUpdate(upd, 510, "", `"photo":[{"file_id":"t_0","file_size":50}]`)
	if _, _, err := ws.dispatchTelegram(context.Background(), "1", &ParsedPayload{}, raw); err != nil {
		t.Fatalf("dispatchTelegram: %v", err)
	}
	time.Sleep(1500 * time.Millisecond)
	if storedCalled != 0 {
		t.Errorf("200 字节 > 上限 100 却转存了 %d 次（存进去的就是半张图）", storedCalled)
	}
	if row := f4TGWaitMediaURL(t, db, fmt.Sprintf("tg_upd_1_%d", upd), false); row.MediaURL != "" {
		t.Errorf("拒收后仍写了 media_url = %q", row.MediaURL)
	}
}

// TestM01_TelegramRealDownloadNon200NotStored 下载域回 403 时那段错误页不是媒体字节。
func TestM01_TelegramRealDownloadNon200NotStored(t *testing.T) {
	ws, db := f4TGSetup(t, "777:tok")
	upd := time.Now().UnixNano() % 1e9
	_, _ = f4TGAPIStub(t, "", 10, http.StatusForbidden)
	storedCalled := 0
	prevStore := tgMediaStoreFn
	t.Cleanup(func() { tgMediaStoreFn = prevStore })
	tgMediaStoreFn = func(ctx context.Context, channel, mediaID string, b []byte, contentType, hint string) (string, error) {
		storedCalled++
		return "/files/telegram/" + mediaID, nil
	}
	raw := f4TGUpdate(upd, 511, "", `"photo":[{"file_id":"n_0","file_size":10}]`)
	if _, _, err := ws.dispatchTelegram(context.Background(), "1", &ParsedPayload{}, raw); err != nil {
		t.Fatalf("dispatchTelegram: %v", err)
	}
	time.Sleep(1500 * time.Millisecond)
	if storedCalled != 0 {
		t.Errorf("403 响应体被当媒体转存了 %d 次", storedCalled)
	}
	row := f4TGWaitMediaURL(t, db, fmt.Sprintf("tg_upd_1_%d", upd), false)
	if row.Content != "[图片]" || row.MsgType != model.MsgTypeImage {
		t.Errorf("下载失败仍要保住类型与占位符，got type=%q content=%q", row.MsgType, row.Content)
	}
}

// TestM01_TelegramFetchRejectsOversizedDeclaredSize FetchTelegramMedia 这条腿单独测：
// getFile 已报明体积时不必再把 20MB 拉下来。
func TestM01_TelegramFetchRejectsOversizedDeclaredSize(t *testing.T) {
	ws, _ := f4TGSetup(t, "777:tok")
	_ = ws
	f4TGOpenLimit(t, 100)
	_, _ = f4TGAPIStub(t, strings.Repeat("Q", 20), 5000, 0)
	data, ct, err := FetchTelegramMedia(context.Background(), "777:tok", "big_0")
	if err == nil {
		t.Errorf("声明体积 5000 > 上限 100 却放行（ct=%s len=%d）", ct, len(data))
	} else if !strings.Contains(err.Error(), "exceeds limit") {
		t.Errorf("err = %q, want 含 exceeds limit", err)
	}
}

// TestM01_TelegramInboundMsgTypeInHubSet 落库类型必须是 hub 认得的那几个取值之一。
// 写个 "voice"/"animation" 之类的官方名，工作台按集合分支渲染会整条落到默认分支。
func TestM01_TelegramInboundMsgTypeInHubSet(t *testing.T) {
	allowed := map[string]bool{
		model.MsgTypeText: true, model.MsgTypeImage: true, model.MsgTypeFile: true,
		model.MsgTypeAudio: true, model.MsgTypeVideo: true,
	}
	for _, tc := range []struct {
		name  string
		media string
	}{
		{"photo", `"photo":[{"file_id":"a","file_size":1}]`},
		{"video", `"video":{"file_id":"a","file_size":1}`},
		{"video_note", `"video_note":{"file_id":"a","file_size":1}`},
		{"voice", `"voice":{"file_id":"a","file_size":1}`},
		{"audio", `"audio":{"file_id":"a","file_size":1}`},
		{"document", `"document":{"file_id":"a","file_size":1}`},
		{"sticker", `"sticker":{"file_id":"a","file_size":1}`},
		{"animation", `"animation":{"file_id":"a","file_size":1}`},
		{"live_photo", `"live_photo":{"file_id":"a","file_size":1}`},
	} {
		ws, db := f4TGSetup(t, "777:tok")
		upd := time.Now().UnixNano() % 1e9
		_, _ = f4TGStubMedia(t, "x-")
		raw := f4TGUpdate(upd, 600, "", tc.media)
		hub, _, err := ws.dispatchTelegram(context.Background(), "1", &ParsedPayload{}, raw)
		if err != nil {
			t.Fatalf("%s dispatchTelegram: %v", tc.name, err)
		}
		if !allowed[hub.MsgType] {
			t.Errorf("%s 落库类型 %q 不在 hub 取值集合里", tc.name, hub.MsgType)
		}
		if hub.MsgType == model.MsgTypeText {
			t.Errorf("%s 被判成 text（等于又走回「丢媒体」的老路）", tc.name)
		}
		_ = db
	}
}

// TestM01_TelegramEditedMediaKeepsType edited_message 与 message 同构：编辑后的图片
// 不能再退回空正文。
func TestM01_TelegramEditedMediaKeepsType(t *testing.T) {
	ws, db := f4TGSetup(t, "777:tok")
	upd := time.Now().UnixNano() % 1e9
	_, _ = f4TGStubMedia(t, "e-")
	body := `{"update_id":` + fmt.Sprint(upd) + `,"edited_message":{"message_id":700,` +
		`"from":{"id":1,"first_name":"B"},"chat":{"id":2,"type":"private"},"date":1700000000,` +
		`"caption":"改过了","photo":[{"file_id":"ed_0","file_size":10}]}}`
	hub, _, err := ws.dispatchTelegram(context.Background(), "1", &ParsedPayload{}, []byte(body))
	if err != nil {
		t.Fatalf("dispatchTelegram: %v", err)
	}
	if hub.MsgType != model.MsgTypeImage || hub.Content != "改过了[图片]" {
		t.Errorf("type=%q content=%q, want image / 改过了[图片]", hub.MsgType, hub.Content)
	}
	hubMsgID := fmt.Sprintf("tg_upd_1_%d", upd)
	if hub.MsgID != hubMsgID {
		t.Errorf("hub.MsgID = %q, want %q", hub.MsgID, hubMsgID)
	}
	if row := f4TGWaitMediaURL(t, db, hubMsgID, true); row.MsgType != model.MsgTypeImage {
		t.Errorf("落库类型 = %q, want image", row.MsgType)
	}
}

// TestM01_TelegramSameUpdateIDAcrossAccounts 官方 update_id 只在**单个 bot** 内唯一
// ⇒ 两个 bot 推来同一个 update_id 是完全正常的。回填按 (platform, account_id, msg_id) 找行，
// 少一个 account_id 条件就会把 A 号的原件写进 B 号那一行（N-10 的跨账号变体）。
func TestM01_TelegramSameUpdateIDAcrossAccounts(t *testing.T) {
	db := setupTelegramTestDB(t)
	for _, seed := range []struct {
		id    uint
		token string
	}{
		{1, "1:tok-one"}, {2, "2:tok-two"},
	} {
		acc := model.TelegramAccount{AccountName: fmt.Sprintf("号%d", seed.id), BotToken: seed.token, Status: 1}
		acc.ID = seed.id
		if err := db.Create(&acc).Error; err != nil {
			t.Fatalf("seed account %d: %v", seed.id, err)
		}
	}
	ws := NewWebhookService(db)
	t.Cleanup(func() { ws.Stop(context.Background()) })

	const sharedUpdate = int64(4242424)
	prevFetch, prevStore := tgMediaFetchFn, tgMediaStoreFn
	t.Cleanup(func() { tgMediaFetchFn, tgMediaStoreFn = prevFetch, prevStore })
	tgMediaFetchFn = func(ctx context.Context, token, fileID string) ([]byte, string, error) {
		return []byte(token + "|" + fileID), "image/png", nil
	}
	tgMediaStoreFn = func(ctx context.Context, channel, mediaID string, b []byte, contentType, hint string) (string, error) {
		// 长期 URL 里带上取到该媒体的账号凭证 ⇒ 一旦回填串号，两行的 URL 就会互相打脸。
		return "/files/telegram/" + strings.ReplaceAll(string(b), "|", "_"), nil
	}

	// 先派 B 号（2），让它的 hub 行 id 更小：漏了 account_id 的实现按 First() 会稳定挑到这一行。
	for _, tc := range []struct {
		account, fileID string
	}{
		{"2", "f_two"},
		{"1", "f_one"},
	} {
		raw := f4TGUpdate(sharedUpdate, 800+int64(tc.account[0]), "",
			`"photo":[{"file_id":"`+tc.fileID+`","file_size":10}]`)
		if _, _, err := ws.dispatchTelegram(context.Background(), tc.account, &ParsedPayload{}, raw); err != nil {
			t.Fatalf("dispatch account %s: %v", tc.account, err)
		}
	}

	for _, tc := range []struct {
		account, token, fileID string
	}{
		{"2", "2:tok-two", "f_two"},
		{"1", "1:tok-one", "f_one"},
	} {
		var hub model.MessageHub
		hubMsgID := "tg_upd_" + tc.account + "_" + strconv.FormatInt(sharedUpdate, 10)
		deadline := time.Now().Add(15 * time.Second)
		for {
			hub = model.MessageHub{}
			err := db.Where("platform = ? AND account_id = ? AND msg_id = ?", "telegram", tc.account, hubMsgID).
				First(&hub).Error
			if err == nil && hub.MediaURL != "" {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("账号 %s 的行没回填: err=%v hub=%+v", tc.account, err, hub)
			}
			time.Sleep(50 * time.Millisecond)
		}
		want := "/files/telegram/" + tc.token + "_" + tc.fileID
		if hub.MediaURL != want {
			t.Errorf("账号 %s 的 media_url = %q, want %q（回填找行漏了 account_id 就会串到别的号）",
				tc.account, hub.MediaURL, want)
		}
		if got := fmt.Sprint(hub.Extra["media_file_ids"]); got != "["+tc.fileID+"]" {
			t.Errorf("账号 %s 的 Extra.media_file_ids = %s, want [%s]", tc.account, got, tc.fileID)
		}
	}
}

// TestM01_TelegramBackfillScopedToAccount 回填找行的 account_id 条件不是装饰。
//
// hub 的唯一键是 (platform, msg_id, conversation_id)，**不含 account_id** ⇒ 同一个
// msg_id 在两个账号下各留一行，是 schema 允许的状态（N-16 的根因就在这把键上）。
// 当前 HubMsgID 把 accountID 编进了 msg_id，所以 dispatch 走不出这个状态；但按键
// 收窄这件事发生在写入点，一旦键形如上游那两处单点（幂等键、dispatch 内存键）回退，
// 漏了 account_id 的回填就会把 A 号原件写进 B 号的行——媒体链接跨号，且**不会报错**。
// 因此这里直接铺两行同 msg_id、不同 account_id 的 hub 记录，单独验写入侧的收敛。
func TestM01_TelegramBackfillScopedToAccount(t *testing.T) {
	db := setupTelegramTestDB(t)
	ws := NewWebhookService(db)
	t.Cleanup(func() { ws.Stop(context.Background()) })

	const sharedMsgID = "tg_upd_collide_999"
	for _, seed := range []struct {
		account, conv string
		id            uint
	}{
		// B 号先落库、id 更小：漏了 account_id 时 First() 会稳定挑到这一行。
		{account: "2", conv: "oc_b", id: 1},
		{account: "1", conv: "oc_a", id: 2},
	} {
		row := model.MessageHub{
			ID: seed.id, Platform: "telegram", MsgID: sharedMsgID,
			AccountID: seed.account, Direction: "inbound", Status: "pending",
			MsgType: model.MsgTypeImage, SenderID: "u_" + seed.account,
			ConversationID: seed.conv, SentAt: time.Now(),
		}
		if err := db.Create(&row).Error; err != nil {
			t.Fatalf("seed hub row account %s: %v", seed.account, err)
		}
	}
	// 反向闸：夹具必须真的造出"同 msg_id 两账号两行"，否则本用例是空跑。
	var cnt int64
	if err := db.Model(&model.MessageHub{}).Where("platform = ? AND msg_id = ?", "telegram", sharedMsgID).
		Count(&cnt).Error; err != nil {
		t.Fatalf("count seeded rows: %v", err)
	}
	if cnt != 2 {
		t.Fatalf("夹具须造出同 msg_id 的 2 行（唯一键含 conversation_id 才允许），实际 %d", cnt)
	}

	ws.backfillTGMedia(context.Background(), "1", sharedMsgID,
		[]string{"/files/telegram/only_for_account_1"}, []string{"file_id_of_1"})

	for _, tc := range []struct {
		account, conv, want string
	}{
		{"1", "oc_a", "/files/telegram/only_for_account_1"},
		{"2", "oc_b", ""}, // 串号就会看到这里非空
	} {
		var hub model.MessageHub
		if err := db.Where("platform = ? AND account_id = ? AND msg_id = ? AND conversation_id = ?",
			"telegram", tc.account, sharedMsgID, tc.conv).
			First(&hub).Error; err != nil {
			t.Fatalf("read back account %s: %v", tc.account, err)
		}
		if hub.MediaURL != tc.want {
			t.Errorf("账号 %s 的 media_url = %q, want %q（回填未按 account_id 收敛就会串号）",
				tc.account, hub.MediaURL, tc.want)
		}
	}
}

// TestM01_TelegramServiceFetchSeamTypes 编译期钉住 seam 的签名（换实现时先在这里露出来）。
func TestM01_TelegramServiceFetchSeamTypes(t *testing.T) {
	var f func(context.Context, string, string) ([]byte, string, error) = FetchTelegramMedia
	if f == nil {
		t.Fatal("FetchTelegramMedia 签名变了")
	}
	var refs []telegram.TGMediaRef
	refs = append(refs, telegram.TGMediaRef{FileID: "a"})
	if len(refs) != 1 {
		t.Fatal("TGMediaRef 不可用")
	}
}
