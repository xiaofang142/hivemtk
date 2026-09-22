package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"syscall"
	"testing"
	"time"

	"hivemtk-user/internal/channelbot/core"
)

// 批F-4b / M-01：Telegram 入站富媒体归一层 + getFile/下载腿。
// 官方口径逐条引自 https://core.telegram.org/bots/api（#message / #file / #getfile）。

func newTestClient(t *testing.T, base string) *Client {
	t.Helper()
	c := &Client{token: "123456:TEST-token-leak", apiBase: base}
	c.BaseClient = core.NewBaseClient(core.WithTimeout(3 * time.Second))
	return c
}

// raw 造一个 *json.RawMessage（TGMessage.Story 用指针以区分「无故事」与「空故事」）。
func raw(s string) *json.RawMessage {
	r := json.RawMessage(s)
	return &r
}

func TestM01_TelegramMediaKindTable(t *testing.T) {
	ref := func(id string) *TGMediaRef { return &TGMediaRef{FileID: id} }
	cases := []struct {
		name        string
		msg         TGMessage
		wantKind    string
		wantPlace   string
		wantRefIDs  []string
		wantStoryOK bool
	}{
		{
			// 官方：「Message is an animation… when this field is set, the document field will also be set」
			// ⇒ GIF 同时带 document，判序错了就把动图存成压缩包。
			name:       "animation 与 document 同时置位时按 animation",
			msg:        TGMessage{Animation: &TGMediaRef{FileID: "anim_1", FileName: "a.gif", MimeType: "image/gif"}, Document: &TGMediaRef{FileID: "doc_1", FileName: "a.gif"}},
			wantKind:   KindVideo,
			wantPlace:  "[动图]",
			wantRefIDs: []string{"anim_1"},
		},
		{
			// 官方：「Message is a live photo… when this field is set, the photo field will also be set」
			name:       "live_photo 与 photo 同时置位时按 live_photo",
			msg:        TGMessage{LivePhoto: &TGMediaRef{FileID: "lp_1"}, Photo: []TGMediaRef{{FileID: "p_small", Width: 90, Height: 90}}},
			wantKind:   KindVideo,
			wantPlace:  "[实况照片]",
			wantRefIDs: []string{"lp_1"},
		},
		{
			name:       "video",
			msg:        TGMessage{Video: &TGMediaRef{FileID: "v_1", FileName: "v.mp4"}},
			wantKind:   KindVideo,
			wantPlace:  "[视频]",
			wantRefIDs: []string{"v_1"},
		},
		{
			name:       "video_note 圆视频",
			msg:        TGMessage{VideoNote: &TGMediaRef{FileID: "vn_1"}},
			wantKind:   KindVideo,
			wantPlace:  "[圆视频]",
			wantRefIDs: []string{"vn_1"},
		},
		{
			name:       "voice 落 audio 类",
			msg:        TGMessage{Voice: &TGMediaRef{FileID: "vo_1"}},
			wantKind:   KindAudio,
			wantPlace:  "[语音]",
			wantRefIDs: []string{"vo_1"},
		},
		{
			name:       "audio",
			msg:        TGMessage{Audio: &TGMediaRef{FileID: "au_1", FileName: "song.mp3"}},
			wantKind:   KindAudio,
			wantPlace:  "[音频]",
			wantRefIDs: []string{"au_1"},
		},
		{
			name:       "sticker 按 image 渲染",
			msg:        TGMessage{Sticker: &TGMediaRef{FileID: "st_1"}},
			wantKind:   KindImage,
			wantPlace:  "[表情]",
			wantRefIDs: []string{"st_1"},
		},
		{
			// 官方：photo = 「Array of PhotoSize. Message is a photo, available sizes of the photo」
			// ⇒ 四个尺寸是同一张图，只存最大档。
			name:      "photo 多尺寸只取最大档",
			msg:       TGMessage{Photo: []TGMediaRef{{FileID: "p_s", FileSize: 1000}, {FileID: "p_m", FileSize: 5000}, {FileID: "p_l", FileSize: 90000}, {FileID: "p_x", FileSize: 40000}}},
			wantKind:  KindImage,
			wantPlace: "[图片]",
			// 顺序也要断：只断「里面有 p_l」会让「四个尺寸全存」的实现在这里蒙过去。
			wantRefIDs: []string{"p_l"},
		},
		{
			name:       "document 带文件名进占位符",
			msg:        TGMessage{Document: &TGMediaRef{FileID: "doc_1", FileName: "报价单.pdf"}},
			wantKind:   KindFile,
			wantPlace:  "[文件] 报价单.pdf",
			wantRefIDs: []string{"doc_1"},
		},
		{
			name:       "document 无文件名",
			msg:        TGMessage{Document: ref("doc_2")},
			wantKind:   KindFile,
			wantPlace:  "[文件]",
			wantRefIDs: []string{"doc_2"},
		},
		{
			// 官方 Story 对象的媒体未在本仓验证可按 file_id 取回 ⇒ 只留占位符、不给 refs，
			// 否则工作台拿到一个永远下载不动的引用。
			name:        "story 只占位不下载",
			msg:         TGMessage{Story: raw(`{"chat":{"id":-100},"date":1700000000}`)},
			wantKind:    KindText,
			wantPlace:   "[转发故事]",
			wantRefIDs:  nil,
			wantStoryOK: true,
		},
		{
			name:       "纯文本无可展示媒体",
			msg:        TGMessage{Text: "你好"},
			wantKind:   "",
			wantPlace:  "",
			wantRefIDs: nil,
		},
		{
			// file_id 为空的容器不算媒体：拿它去 getFile 只会换来一个必然失败的请求。
			name:       "容器在但 file_id 空",
			msg:        TGMessage{Video: &TGMediaRef{FileName: "no-id.mp4"}},
			wantKind:   "",
			wantPlace:  "",
			wantRefIDs: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kind, placeholder, refs, ok := tc.msg.mediaKind()
			if tc.wantKind == "" {
				if ok {
					t.Fatalf("期望无媒体，实际 ok=true kind=%s place=%q", kind, placeholder)
				}
				return
			}
			if !ok {
				t.Fatalf("期望有媒体，实际 ok=false")
			}
			if kind != tc.wantKind {
				t.Errorf("kind=%q, want %q", kind, tc.wantKind)
			}
			if placeholder != tc.wantPlace {
				t.Errorf("placeholder=%q, want %q", placeholder, tc.wantPlace)
			}
			gotIDs := make([]string, 0, len(refs))
			for _, r := range refs {
				gotIDs = append(gotIDs, r.FileID)
			}
			if strings.Join(gotIDs, ",") != strings.Join(tc.wantRefIDs, ",") {
				t.Errorf("refIDs=%v, want %v", gotIDs, tc.wantRefIDs)
			}
			if tc.wantStoryOK && len(refs) != 0 {
				t.Errorf("story 不应给出可下载引用，实际 %d 条", len(refs))
			}
		})
	}
}

func TestM01_TelegramLargestPhotoFallsBackToPixels(t *testing.T) {
	// 官方：PhotoSize.file_size 是 Optional，width/height 必填 ⇒ 全零 file_size 时按像素面积选。
	sizes := []TGMediaRef{
		{FileID: "p_90", Width: 90, Height: 60},
		{FileID: "p_320", Width: 320, Height: 200},
		{FileID: "p_800", Width: 800, Height: 500},
	}
	if got := largestPhotoSize(sizes); got.FileID != "p_800" {
		t.Errorf("got %q, want p_800", got.FileID)
	}
	// 混合档：file_size 与像素面积口径不能互相顶替——官方 PhotoSize 三件套都在，
	// 原件体积更大才是"最大档"（像素只作 file_size 缺失时的退路）。
	mixed := []TGMediaRef{
		{FileID: "a", Width: 5000, Height: 5000, FileSize: 100},
		{FileID: "b", Width: 10, Height: 10, FileSize: 9000},
	}
	if got := largestPhotoSize(mixed); got.FileID != "b" {
		t.Errorf("got %q, want b（file_size 优先；像素面积只在 file_size 缺失时计分）", got.FileID)
	}
	// 全缺 file_size 时按像素面积。
	pixels := []TGMediaRef{
		{FileID: "p_90", Width: 90, Height: 60},
		{FileID: "p_800", Width: 800, Height: 500},
	}
	if got := largestPhotoSize(pixels); got.FileID != "p_800" {
		t.Errorf("got %q, want p_800", got.FileID)
	}
}

func TestM01_TelegramInboundCaptionAndPlaceholder(t *testing.T) {
	t.Run("媒体件正文取 caption 并保留占位符", func(t *testing.T) {
		m := &TGMessage{Caption: "这是本月报价 ", Photo: []TGMediaRef{{FileID: "p", FileSize: 10}}}
		inb := m.Inbound()
		if inb.MsgType != KindImage {
			t.Errorf("MsgType=%q, want image", inb.MsgType)
		}
		if inb.Content != "这是本月报价[图片]" {
			t.Errorf("Content=%q, want %q", inb.Content, "这是本月报价[图片]")
		}
		if len(inb.Media) != 1 || inb.Media[0].FileID != "p" {
			t.Errorf("Media=%+v", inb.Media)
		}
	})
	t.Run("无 caption 的纯图只剩占位符", func(t *testing.T) {
		inb := (&TGMessage{Document: &TGMediaRef{FileID: "d"}}).Inbound()
		if inb.Content != "[文件]" {
			t.Errorf("Content=%q", inb.Content)
		}
	})
	t.Run("文本件取 text", func(t *testing.T) {
		inb := (&TGMessage{Text: "hi", Caption: "不应生效"}).Inbound()
		if inb.MsgType != KindText || inb.Content != "hi" || inb.Media != nil {
			t.Errorf("inb=%+v", inb)
		}
	})
	t.Run("本仓未承载的媒体形态仍带出 caption", func(t *testing.T) {
		// 官方 paid media 之类只保证 caption；丢了等于客户说的话没人看见。
		inb := (&TGMessage{Caption: "看看这个"}).Inbound()
		if inb.MsgType != KindText || inb.Content != "看看这个" {
			t.Errorf("inb=%+v", inb)
		}
	})
}

func TestM01_TelegramHubMsgIDPrecedence(t *testing.T) {
	cases := []struct {
		name string
		up   Update
		want string
	}{
		{
			name: "update_id 优先（官方对同一 Update 重投保持 update_id 不变）",
			up:   Update{UpdateID: 991, Message: &TGMessage{MessageID: 7}},
			want: "tg_upd_7_991",
		},
		{
			name: "无 update_id 退到消息 ID",
			up:   Update{Message: &TGMessage{MessageID: 7}},
			want: "tg_7_7",
		},
		{
			name: "edited_message 同构",
			up:   Update{EditedMessage: &TGMessage{MessageID: 8}},
			want: "tg_7_8",
		},
		{
			name: "channel_post 也算消息",
			up:   Update{ChannelPost: &TGMessage{MessageID: 9}},
			want: "tg_7_9",
		},
		{
			// 回调没有 message_id（回调消息只带 chat）⇒ 用官方 callback_query.id，
			// 与 ToInbound 的 MessageID 同键，否则 dispatch 内存视图与中台落库行分叉。
			name: "callback 用回调 ID",
			up:   Update{CallbackQuery: &TGCallbackQuery{ID: "cb_abc"}},
			want: "tg_cb_7_cb_abc",
		},
		{
			name: "message_id 为 0 时不产出伪键",
			up:   Update{Message: &TGMessage{MessageID: 0}},
			want: "",
		},
		{
			name: "空 Update 无键",
			up:   Update{},
			want: "",
		},
		{
			// 官方 update_id 是每个 bot 各自计数 ⇒ 两个号推同一个 update_id 是常态。
			// 键里不带账号时第二条会被中台当重复事件丢弃（实测坐实，见下方跨账号用例）。
			name: "同 update_id 不同账号各得各的键",
			up:   Update{UpdateID: 5, Message: &TGMessage{MessageID: 1}},
			want: "tg_upd_9_5",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			acct := "7"
			if tc.name == "同 update_id 不同账号各得各的键" {
				acct = "9"
			}
			if got := tc.up.HubMsgID(acct); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestM01_TelegramCallbackInboundSharesContent(t *testing.T) {
	// dispatch 的内存 hub 与 Ingress 落库必须同一份正文，否则工作台与 AI 看到的不是同一条。
	up := &Update{CallbackQuery: &TGCallbackQuery{ID: "cb1", Data: "opportunity_yes", From: &TGUser{ID: 5}}}
	inb := up.ToInbound("42")
	if inb == nil {
		t.Fatal("回调应有入站视图")
	}
	if inb.MessageID != "tg_cb_42_cb1" {
		t.Errorf("MessageID=%q", inb.MessageID)
	}
	if inb.MsgType != "text" {
		t.Errorf("MsgType=%q", inb.MsgType)
	}
}

// --- getFile / 下载腿：打 httptest，验证真实字节链路 ---

func TestM01_TelegramGetFileHitsBotMethodPath(t *testing.T) {
	var gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_, _ = w.Write([]byte(`{"ok":true,"result":{"file_id":"f1","file_unique_id":"u1","file_size":12,"file_path":"documents/file_1.pdf"}}`))
	}))
	defer srv.Close()

	f, err := newTestClient(t, srv.URL).GetFile(context.Background(), "f1")
	if err != nil {
		t.Fatalf("GetFile: %v", err)
	}
	if gotPath == "" {
		t.Fatal("服务端未收到请求")
	}
	if !strings.HasSuffix(gotPath, "/getFile") || !strings.Contains(gotPath, "/bot123456:TEST-token-leak/") {
		t.Errorf("path=%q, want /bot<token>/getFile", gotPath)
	}
	if !strings.Contains(gotBody, `"file_id":"f1"`) {
		t.Errorf("body=%q 未带 file_id", gotBody)
	}
	if f.FilePath != "documents/file_1.pdf" || f.FileSize != 12 {
		t.Errorf("f=%+v", f)
	}
}

func TestM01_TelegramGetFileRejectsEmptyFileID(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer srv.Close()
	if _, err := newTestClient(t, srv.URL).GetFile(context.Background(), "   "); err == nil {
		t.Fatal("空 file_id 必须报错")
	}
	if called {
		t.Error("空 file_id 不该发出请求（官方会以一段无信息量的 400 回）")
	}
}

func TestM01_TelegramGetFileErrors(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		body     string
		wantSub  string
		notWant  []string
		wantCall bool
	}{
		{
			// 官方 file_path 是 Optional：拿不到路径就没有可下载的东西。
			// 返回空串等于把 "" 拼成 /file/bot<token>/ 这个不存在的接口，错误还来得更晚更怪。
			name:    "ok 但无 file_path",
			status:  200,
			body:    `{"ok":true,"result":{"file_id":"f1","file_size":9}}`,
			wantSub: "file_path",
		},
		{
			name:    "ok:false 只带 description",
			status:  200,
			body:    `{"ok":false,"description":"Bad Request: file is too big"}`,
			wantSub: "file is too big",
		},
		{
			name:    "非 200",
			status:  502,
			body:    `bad gateway`,
			wantSub: "502",
		},
		{
			name:    "响应不是 JSON",
			status:  200,
			body:    `<html>proxy</html>`,
			wantSub: "parse",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			_, err := newTestClient(t, srv.URL).GetFile(context.Background(), "f1")
			if err == nil {
				t.Fatalf("期望报错，实际 nil（body=%s）", tc.body)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("err=%q 不含 %q", err.Error(), tc.wantSub)
			}
		})
	}
}

func TestM01_TelegramDownloadFileUsesFilePathPrefix(t *testing.T) {
	var gotPath string
	body := []byte("%PDF-1.4 fake bytes")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	data, ct, err := newTestClient(t, srv.URL).DownloadFile(context.Background(), "documents/file_1.pdf", 0)
	if err != nil {
		t.Fatalf("DownloadFile: %v", err)
	}
	// 官方下载域是 https://api.telegram.org/file/bot<token>/<file_path>
	// —— 与接口调用段 /bot<token>/<method> 不是同一个前缀，写成后者必然 404。
	if want := "/file/bot123456:TEST-token-leak/documents/file_1.pdf"; gotPath != want {
		t.Errorf("path=%q, want %q", gotPath, want)
	}
	if string(data) != string(body) {
		t.Errorf("data=%q", data)
	}
	if ct != "application/pdf" {
		t.Errorf("contentType=%q", ct)
	}
}

func TestM01_TelegramDownloadTruncationDetected(t *testing.T) {
	// 官方上限 20 MB；本处用小 maxBytes 验同一机制：读满即判定超限。
	// LimitReader 静默截断会把半截原件当完整件转存 —— 图片只渲染一半、zip 直接损坏，
	// 而事后从存储里看不出少了一段，比不存更难发现。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 64)))
	}))
	defer srv.Close()
	if _, _, err := newTestClient(t, srv.URL).DownloadFile(context.Background(), "f", 10); err == nil {
		t.Fatal("超出 maxBytes 必须报错")
	} else if !strings.Contains(err.Error(), "larger than") {
		t.Errorf("err=%q", err)
	}

	// 边界：正好等于上限不算残（否则最大件永远存不进）。
	exact := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("y", 10)))
	}))
	defer exact.Close()
	data, _, err := newTestClient(t, exact.URL).DownloadFile(context.Background(), "f", 10)
	if err != nil {
		t.Fatalf("恰好等于上限应通过: %v", err)
	}
	if len(data) != 10 {
		t.Errorf("len=%d", len(data))
	}
}

func TestM01_TelegramDownloadPathGuard(t *testing.T) {
	var hit int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit++ }))
	defer srv.Close()
	c := newTestClient(t, srv.URL)
	for _, bad := range []string{"", "   ", "/etc/passwd", "http://evil.example/a", "https://evil.example/a", "../secret", "documents/../secret"} {
		if _, _, err := c.DownloadFile(context.Background(), bad, 100); err == nil {
			t.Errorf("file_path=%q 必须被拒", bad)
		}
	}
	if hit != 0 {
		t.Errorf("非法路径不应发请求，实际发了 %d 次", hit)
	}
}

func TestM01_TelegramDownloadNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	if _, _, err := newTestClient(t, srv.URL).DownloadFile(context.Background(), "f", 100); err == nil {
		t.Fatal("403 必须报错")
	} else if !strings.Contains(err.Error(), "403") {
		t.Errorf("err=%q", err)
	}
}

func TestM01_TelegramTokenNeverInError(t *testing.T) {
	// 官方把 bot token 放在 URL 路径里（/bot<token>/<method>），于是标准库的 *url.Error
	// 会把含 token 的完整 URL 写进错误串。这些错误串会随 logger.Err(err) 落日志与
	// 观测链路 ⇒ 一次网络抖动就把长期凭证写进日志集中存储。
	// 断言口径：任何从本客户端流出的错误都不含 token 原文。
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	base := srv.URL
	srv.Close() // 端口已关：GetFile / DownloadFile 都只能拿到传输层错误

	c := newTestClient(t, base)
	_, err1 := c.GetFile(context.Background(), "f1")
	if err1 == nil {
		t.Fatal("期望传输层错误")
	}
	_, _, err2 := c.DownloadFile(context.Background(), "documents/f.pdf", 100)
	if err2 == nil {
		t.Fatal("期望传输层错误")
	}
	_, err3 := c.SendMessage(context.Background(), 1, "hi", SendMessageOptions{})
	if err3 == nil {
		t.Fatal("期望传输层错误")
	}
	for i, err := range []error{err1, err2, err3} {
		if strings.Contains(err.Error(), c.token) {
			t.Errorf("第 %d 个错误串泄漏 bot token: %q", i+1, err.Error())
		}
		if !strings.Contains(err.Error(), "refused") {
			t.Errorf("第 %d 个错误串不像传输层错误（可能没走到真实请求）: %q", i+1, err.Error())
		}
		// 脱敏只能改文本，不能改判据：出站归类要按底层错误决定可重试性，
		// 若脱敏把错误链换成裸 errors.New，这里就断了（表现是限流/抖动被误判成不可重试）。
		var errno syscall.Errno
		if !errors.As(err, &errno) {
			t.Errorf("第 %d 个错误丢了底层 errno，归类链断了: %q", i+1, err.Error())
		}
	}
}

func TestM01_TelegramScrubKeepsUntokenedErrorIntact(t *testing.T) {
	// 只对确实含 token 的错误做包装：给每个错误都换一层壳会让上游的判据无谓地变深。
	c := newTestClient(t, "http://127.0.0.1:1")
	plain := errors.New("boom")
	if got := c.scrubToken(plain); got != plain {
		t.Errorf("不含 token 的错误应原样返回，得到 %+v", got)
	}
	if got := c.scrubToken(nil); got != nil {
		t.Errorf("nil 必须原样返回，得到 %+v", got)
	}
	empty := &Client{token: ""}
	if got := empty.scrubToken(plain); got != plain {
		t.Errorf("无 token 客户端不该改动错误，得到 %+v", got)
	}
}

func TestM01_TelegramMaxDownloadConstant(t *testing.T) {
	// 官方：「The maximum file size to download is 20 MB」⇒ 单位口径要钉住，
	// 写成 20<<10（20KB）会让所有稍大的图都存不进。
	if MaxDownloadFileBytes != 20*1024*1024 {
		t.Errorf("MaxDownloadFileBytes=%d", MaxDownloadFileBytes)
	}
	if got := fmt.Sprintf("%d", MaxDownloadFileBytes); got != "20971520" {
		t.Errorf("got %s", got)
	}
}
