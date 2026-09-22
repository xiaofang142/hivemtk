package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"hivemtk-user/internal/model"
)

// 下载转存腿（M-01 钉钉）：downloadCode 会过期（官方未公布时限，只有「下载码有误或者已经过期」
// 的错误码），所以入站当场必须换成可长期访问的 URL，并回填到本行 message_hub。
//
// 装替身的顺序在这里是要害：承载腿的 f4DtSetup 自己会把 dtMediaFetchFn/dtMediaStoreFn
// 换成「不发起真实下载/不写长期存储」的报错替身，所以必须先 f4DtSetup 再装本文件的替身，
// 反过来写会静默被覆盖（下载腿变成 0 次调用，计数断言还会因为同样的覆盖而假绿）。

func TestM01_DingTalkMediaIsStoredAndBackfilled(t *testing.T) {
	svc, id, db := f4DtSetup(t)

	fetched := make(chan string, 4)
	var robotCodes []string
	dtMediaFetchFn = func(ctx context.Context, appKey, appSecret, robotCode, downloadCode string) ([]byte, string, error) {
		if appKey == "" || appSecret == "" {
			return nil, "", fmt.Errorf("stub: 账号凭证没传下来")
		}
		if robotCode == "" {
			return nil, "", fmt.Errorf("stub: robotCode 是官方必填项，回调里没带下来")
		}
		robotCodes = append(robotCodes, robotCode)
		fetched <- downloadCode
		return []byte("media-bytes-of-" + downloadCode), "image/png", nil
	}
	dtMediaStoreFn = func(ctx context.Context, channel, mediaID string, data []byte, contentType, filenameHint string) (string, error) {
		if !bytes.Contains(data, []byte(mediaID)) {
			return "", fmt.Errorf("stub: 字节不属于 %s", mediaID)
		}
		return "/files/" + channel + "/" + mediaID, nil
	}

	headers := dtRobotHeaders(f4DtSecret, time.Now().UnixMilli())
	nonce := fmt.Sprintf("f4dts%d", time.Now().UnixNano())
	dc := "dc-" + nonce + "-pic"
	body := f4DtEnvelope("p-"+nonce, "picture", `,"content":{"downloadCode":"`+dc+`","width":"1","height":"1"}`, nonce)
	if err := svc.ReceiveMessage(context.Background(), id, []byte(body), nil, headers); err != nil {
		t.Fatalf("ReceiveMessage: %v", err)
	}

	select {
	case got := <-fetched:
		if got != dc {
			t.Errorf("下载用的 downloadCode = %q，want %q", got, dc)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("M-01 未达成：15s 内没有起过一次钉钉媒体下载")
	}

	row := f4DtLoadHubByMsgID(t, db, "dt-"+fmt.Sprint(id)+"-p-"+nonce, true)
	if want := "/files/dingtalk/" + dc; row.MediaURL != want {
		t.Errorf("M-01 未达成：hub.media_url = %q，want %q（Extra=%v）", row.MediaURL, want, row.Extra)
	}
	if got, _ := f4DtExtraString(row.Extra, "media_download_code"); got != dc {
		t.Errorf("回填后 Extra.media_download_code 被抹掉了：got %q want %q", got, dc)
	}
	if len(robotCodes) != 1 || robotCodes[0] != "robot-f4" {
		t.Errorf("robotCode 应取自回调携带的值，got %v", robotCodes)
	}
}

// TestM01_DingTalkRichTextStoresEveryPicture 富文本一次带两张图：两张都要各自转存，
// 且 URL 全量留痕（media_url 只有一个位置，放首条；其余进 Extra.media_urls）。
// 只存首条正是 N-10 在 WhatsApp 侧修过的错，钉钉不能重犯。
func TestM01_DingTalkRichTextStoresEveryPicture(t *testing.T) {
	svc, id, db := f4DtSetup(t)

	fetched := make(chan string, 8)
	stored := make(chan string, 8)
	dtMediaFetchFn = func(ctx context.Context, appKey, appSecret, robotCode, downloadCode string) ([]byte, string, error) {
		fetched <- downloadCode
		return []byte("bytes-" + downloadCode), "image/jpeg", nil
	}
	dtMediaStoreFn = func(ctx context.Context, channel, mediaID string, data []byte, contentType, filenameHint string) (string, error) {
		stored <- mediaID
		return "/files/" + channel + "/" + mediaID, nil
	}

	headers := dtRobotHeaders(f4DtSecret, time.Now().UnixMilli())
	nonce := fmt.Sprintf("f4dtrt%d", time.Now().UnixNano())
	c1, c2 := "dc-"+nonce+"-a", "dc-"+nonce+"-b"
	body := f4DtEnvelope("rt-"+nonce, "richText",
		`,"content":{"richText":[{"text":"看这两张"},{"downloadCode":"`+c1+`","type":"picture"},{"downloadCode":"`+c2+`","type":"picture"}]}`, nonce)
	if err := svc.ReceiveMessage(context.Background(), id, []byte(body), nil, headers); err != nil {
		t.Fatalf("ReceiveMessage: %v", err)
	}

	for i := 0; i < 2; i++ {
		select {
		case <-fetched:
		case <-time.After(15 * time.Second):
			t.Fatalf("M-01 未达成：富文本两张图只起了 %d 次下载", i)
		}
	}
	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		select {
		case s := <-stored:
			seen[s] = true
		case <-time.After(15 * time.Second):
			t.Fatalf("M-01 未达成：只完成 %d 次转存，want 2（已见 %v）", i, seen)
		}
	}
	if !seen[c1] || !seen[c2] {
		t.Errorf("两张图都该各自转存，got %v（want %s + %s）", seen, c1, c2)
	}

	row := f4DtLoadHubByMsgID(t, db, "dt-"+fmt.Sprint(id)+"-rt-"+nonce, true)
	if row.MediaURL != "/files/dingtalk/"+c1 {
		t.Errorf("hub.media_url = %q，want 首张 %q", row.MediaURL, "/files/dingtalk/"+c1)
	}
	if got := fmt.Sprint(row.Extra["media_urls"]); got != "["+"/files/dingtalk/"+c1+" "+"/files/dingtalk/"+c2+"]" {
		t.Errorf("Extra.media_urls = %s，want 两张都在（第二张的长期 URL 不能丢）", got)
	}
	if row.Content != "看这两张[图片][图片]" {
		t.Errorf("富文本正文 = %q，want 看这两张[图片][图片]", row.Content)
	}
}

// TestM01_DingTalkMediaSurvivesRequestCancel 转存必须活到响应之后：钉钉回调是同步链路
// （WebhookController.DingTalkReceive → ReceiveMessage），而官方要求 3 秒内回 200，
// 请求 ctx 在 handler 返回那一刻就 Done()。若异步转存沿用这个 ctx（utils.SafeGo 不解耦取消链），
// 下载会在响应之后立刻被取消——media_url 永久为空，而测试里传的是 context.Background()，
// 这条缺陷在用例中不可能暴露（N-10 的同类：只有把「谁取消了我」测出来才算测到）。
func TestM01_DingTalkMediaSurvivesRequestCancel(t *testing.T) {
	svc, id, db := f4DtSetup(t)

	dtMediaFetchFn = func(ctx context.Context, appKey, appSecret, robotCode, downloadCode string) ([]byte, string, error) {
		// 模拟一次真实网络往返：期间 ctx 一被取消就失败，跟 httpclient 的行为一致。
		select {
		case <-ctx.Done():
			return nil, "", ctx.Err()
		case <-time.After(300 * time.Millisecond):
			return []byte("media-bytes-of-" + downloadCode), "image/png", nil
		}
	}
	dtMediaStoreFn = func(ctx context.Context, channel, mediaID string, data []byte, contentType, filenameHint string) (string, error) {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "/files/" + channel + "/" + mediaID, nil
	}

	headers := dtRobotHeaders(f4DtSecret, time.Now().UnixMilli())
	nonce := fmt.Sprintf("f4dtcx%d", time.Now().UnixNano())
	dc := "dc-" + nonce + "-cx"
	body := f4DtEnvelope("cx-"+nonce, "picture", `,"content":{"downloadCode":"`+dc+`"}`, nonce)

	ctx, cancel := context.WithCancel(context.Background())
	if err := svc.ReceiveMessage(ctx, id, []byte(body), nil, headers); err != nil {
		cancel()
		t.Fatalf("ReceiveMessage: %v", err)
	}
	cancel() // 等价于 handler 写完响应：请求 ctx 到此为止

	row := f4DtLoadHubByMsgID(t, db, "dt-"+fmt.Sprint(id)+"-cx-"+nonce, true)
	if want := "/files/dingtalk/" + dc; row.MediaURL != want {
		t.Errorf("M-01 未达成：请求 ctx 取消后 hub.media_url = %q，want %q（异步转存不该随请求一起死）",
			row.MediaURL, want)
	}
}

// TestM01_DingTalkMediaSkippedWithoutRobotCode 回调没带 robotCode 时不得发起下载（官方必填项，
// 缺了必然 400），但正文与类型仍要正常入库。
func TestM01_DingTalkMediaSkippedWithoutRobotCode(t *testing.T) {
	svc, id, db := f4DtSetup(t)
	called := 0
	dtMediaFetchFn = func(ctx context.Context, appKey, appSecret, robotCode, downloadCode string) ([]byte, string, error) {
		called++
		return nil, "", fmt.Errorf("stub: 不该被调用")
	}
	dtMediaStoreFn = func(ctx context.Context, channel, mediaID string, data []byte, contentType, filenameHint string) (string, error) {
		called++
		return "", nil
	}

	headers := dtRobotHeaders(f4DtSecret, time.Now().UnixMilli())
	nonce := fmt.Sprintf("f4dtnc%d", time.Now().UnixNano())
	body := `{"conversationType":"1","senderStaffId":"staff-f4` + nonce +
		`","conversationId":"cid-` + nonce + `","msgId":"nr-` + nonce +
		`","msgtype":"picture","content":{"downloadCode":"dc-` + nonce + `-nr"}}`
	if err := svc.ReceiveMessage(context.Background(), id, []byte(body), nil, headers); err != nil {
		t.Fatalf("ReceiveMessage: %v", err)
	}
	row := f4DtLoadHubByMsgID(t, db, "dt-"+fmt.Sprint(id)+"-nr-"+nonce, false)
	if row.MsgType != model.MsgTypeImage || row.Content != "[图片]" {
		t.Errorf("缺 robotCode 时正文/类型仍要落库，got type=%q content=%q", row.MsgType, row.Content)
	}
	if got, _ := f4DtExtraString(row.Extra, "media_download_code"); got == "" {
		t.Errorf("downloadCode 仍要留痕（后续人工/重试可复用），got Extra=%v", row.Extra)
	}
	time.Sleep(500 * time.Millisecond)
	if called != 0 {
		t.Errorf("缺 robotCode 却起了 %d 次下载，want 0", called)
	}
}

// f4DtDrain 取空一个 channel 里已攒下的全部值。只在"回填已经发生"之后调用：
// persistDingTalkMediaAsync 的循环跑完才会回填，所以读 media_url 成功就等于循环已收尾，
// 此刻的计数才是确定的（不需要再 sleep 猜它跑完了没）。
func f4DtDrain(ch chan string) []string {
	out := []string{}
	for {
		select {
		case v := <-ch:
			out = append(out, v)
		default:
			return out
		}
	}
}

// TestM01_DingTalkPartialTransferFailureKeepsSuccessfulPicture 富文本两张图，第一张下载失败：
// 第二张仍要转存并回填，且 media_url 放的是**首个成功转存**的链接（不是官方顺序的第一张）。
// 这条同时钉住两个失效形状：① 一失败就整批放弃（continue 改成 return）② 把失败的坑位留在结果里。
func TestM01_DingTalkPartialTransferFailureKeepsSuccessfulPicture(t *testing.T) {
	svc, id, db := f4DtSetup(t)

	fetched := make(chan string, 8)
	stored := make(chan string, 8)
	nonce := fmt.Sprintf("f4dtpf%d", time.Now().UnixNano())
	c1, c2 := "dc-"+nonce+"-a", "dc-"+nonce+"-b"
	dtMediaFetchFn = func(ctx context.Context, appKey, appSecret, robotCode, downloadCode string) ([]byte, string, error) {
		fetched <- downloadCode
		if downloadCode == c1 {
			return nil, "", errors.New("stub: 下载码有误或者已经过期")
		}
		return []byte("bytes-" + downloadCode), "image/png", nil
	}
	dtMediaStoreFn = func(ctx context.Context, channel, mediaID string, data []byte, contentType, filenameHint string) (string, error) {
		stored <- mediaID
		return "/files/" + channel + "/" + mediaID, nil
	}

	headers := dtRobotHeaders(f4DtSecret, time.Now().UnixMilli())
	body := f4DtEnvelope("pf-"+nonce, "richText",
		`,"content":{"richText":[{"text":"两张"},{"downloadCode":"`+c1+`","type":"picture"},{"downloadCode":"`+c2+`","type":"picture"}]}`, nonce)
	if err := svc.ReceiveMessage(context.Background(), id, []byte(body), nil, headers); err != nil {
		t.Fatalf("ReceiveMessage: %v", err)
	}

	row := f4DtLoadHubByMsgID(t, db, "dt-"+fmt.Sprint(id)+"-pf-"+nonce, true)
	if got, want := f4DtDrain(fetched), []string{c1, c2}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("下载尝试 = %v, want %v（第一张失败不该带走第二张）", got, want)
	}
	if got := f4DtDrain(stored); len(got) != 1 || got[0] != c2 {
		t.Errorf("转存的下载码 = %v, want 只有 %s", got, c2)
	}
	if want := "/files/dingtalk/" + c2; row.MediaURL != want {
		t.Errorf("hub.media_url = %q, want 首个成功转存 %q", row.MediaURL, want)
	}
	if got := fmt.Sprint(row.Extra["media_urls"]); got != "["+"/files/dingtalk/"+c2+"]" {
		t.Errorf("Extra.media_urls = %s, want 只含成功那张（失败的不占位、不留 downloadCode）", got)
	}
	if row.Content != "两张[图片][图片]" {
		t.Errorf("富文本正文 = %q, want 两张[图片][图片]（占位符不能因转存失败而变）", row.Content)
	}
}

// TestM01_DingTalkPartialStoreFailureKeepsSuccessfulPicture 同上，但挂的是第二条腿（下载成功、
// 写长期存储失败）：两个失败分支各写一次 `continue`，只测一个的话另一个被改成 `return` 无人报警。
func TestM01_DingTalkPartialStoreFailureKeepsSuccessfulPicture(t *testing.T) {
	svc, id, db := f4DtSetup(t)

	fetched := make(chan string, 8)
	stored := make(chan string, 8)
	nonce := fmt.Sprintf("f4dtst%d", time.Now().UnixNano())
	c1, c2 := "dc-"+nonce+"-a", "dc-"+nonce+"-b"
	dtMediaFetchFn = func(ctx context.Context, appKey, appSecret, robotCode, downloadCode string) ([]byte, string, error) {
		fetched <- downloadCode
		return []byte("bytes-" + downloadCode), "image/png", nil
	}
	dtMediaStoreFn = func(ctx context.Context, channel, mediaID string, data []byte, contentType, filenameHint string) (string, error) {
		stored <- mediaID
		if mediaID == c1 {
			return "", errors.New("stub: 长期存储写入失败")
		}
		return "/files/" + channel + "/" + mediaID, nil
	}

	headers := dtRobotHeaders(f4DtSecret, time.Now().UnixMilli())
	body := f4DtEnvelope("st-"+nonce, "richText",
		`,"content":{"richText":[{"text":"两张"},{"downloadCode":"`+c1+`","type":"picture"},{"downloadCode":"`+c2+`","type":"picture"}]}`, nonce)
	if err := svc.ReceiveMessage(context.Background(), id, []byte(body), nil, headers); err != nil {
		t.Fatalf("ReceiveMessage: %v", err)
	}

	row := f4DtLoadHubByMsgID(t, db, "dt-"+fmt.Sprint(id)+"-st-"+nonce, true)
	if got, want := f4DtDrain(fetched), []string{c1, c2}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("下载尝试 = %v, want %v（第一张转存失败不该带走第二张）", got, want)
	}
	if want := "/files/dingtalk/" + c2; row.MediaURL != want {
		t.Errorf("hub.media_url = %q, want 首个成功转存 %q", row.MediaURL, want)
	}
	if got := fmt.Sprint(row.Extra["media_urls"]); got != "["+"/files/dingtalk/"+c2+"]" {
		t.Errorf("Extra.media_urls = %s, want 只含转存成功那张", got)
	}
}

// TestM01_DingTalkAllTransfersFailedWriteNoMedia 两张全失败：一行都不许写。
// 半途写的话，工作台看到的是"有媒体但点开是空"，比留着 [图片] 占位符更糟。
func TestM01_DingTalkAllTransfersFailedWriteNoMedia(t *testing.T) {
	svc, id, db := f4DtSetup(t)

	fetched := make(chan string, 8)
	stored := make(chan string, 8)
	dtMediaFetchFn = func(ctx context.Context, appKey, appSecret, robotCode, downloadCode string) ([]byte, string, error) {
		fetched <- downloadCode
		return nil, "", errors.New("stub: 下载码有误或者已经过期")
	}
	dtMediaStoreFn = func(ctx context.Context, channel, mediaID string, data []byte, contentType, filenameHint string) (string, error) {
		stored <- mediaID
		return "/files/" + channel + "/" + mediaID, nil
	}

	headers := dtRobotHeaders(f4DtSecret, time.Now().UnixMilli())
	nonce := fmt.Sprintf("f4dtaf%d", time.Now().UnixNano())
	c1, c2 := "dc-"+nonce+"-a", "dc-"+nonce+"-b"
	body := f4DtEnvelope("af-"+nonce, "richText",
		`,"content":{"richText":[{"text":"全挂"},{"downloadCode":"`+c1+`","type":"picture"},{"downloadCode":"`+c2+`","type":"picture"}]}`, nonce)
	if err := svc.ReceiveMessage(context.Background(), id, []byte(body), nil, headers); err != nil {
		t.Fatalf("ReceiveMessage: %v", err)
	}

	for i := 0; i < 2; i++ {
		select {
		case <-fetched:
		case <-time.After(15 * time.Second):
			t.Fatalf("只起了 %d 次下载，want 2", i)
		}
	}
	// 两次下载都失败之后，回填那一步要么已经跑完、要么根本不会跑；给它一个响应窗口。
	time.Sleep(500 * time.Millisecond)

	row := f4DtLoadHubByMsgID(t, db, "dt-"+fmt.Sprint(id)+"-af-"+nonce, false)
	if row.MediaURL != "" {
		t.Errorf("全失败却把 hub.media_url 写成 %q, want 空", row.MediaURL)
	}
	if v, ok := row.Extra["media_urls"]; ok {
		t.Errorf("全失败却写了 Extra.media_urls = %v, want 不存在", v)
	}
	if got := f4DtDrain(stored); len(got) != 0 {
		t.Errorf("全失败却转存了 %v, want 无", got)
	}
	if _, ok := f4DtExtraString(row.Extra, "media_download_code"); !ok {
		t.Errorf("转存失败也不能抹掉入站时的 media_download_code（人工补转存要用它）")
	}
}

// TestM01_DingTalkSameDownloadCodeTransfersOnce 同一个下载码出现多次（富文本里同一张图被
// 拆成两段、或顶层与 content 各带一份）只转存一次：多转一次是白花一次外网往返，
// 而 Extra.media_urls 里重复的同一 URL 会让工作台渲染出两张一样的图。
func TestM01_DingTalkSameDownloadCodeTransfersOnce(t *testing.T) {
	svc, id, db := f4DtSetup(t)

	fetched := make(chan string, 8)
	stored := make(chan string, 8)
	dtMediaFetchFn = func(ctx context.Context, appKey, appSecret, robotCode, downloadCode string) ([]byte, string, error) {
		fetched <- downloadCode
		return []byte("bytes-" + downloadCode), "image/png", nil
	}
	dtMediaStoreFn = func(ctx context.Context, channel, mediaID string, data []byte, contentType, filenameHint string) (string, error) {
		stored <- mediaID
		return "/files/" + channel + "/" + mediaID, nil
	}

	headers := dtRobotHeaders(f4DtSecret, time.Now().UnixMilli())
	nonce := fmt.Sprintf("f4dtdp%d", time.Now().UnixNano())
	shared := "dc-" + nonce + "-same"
	body := f4DtEnvelope("dp-"+nonce, "richText",
		`,"content":{"downloadCode":"`+shared+`","richText":[{"downloadCode":"`+shared+
			`","type":"picture"},{"downloadCode":"`+shared+`","type":"picture"}]}`, nonce)
	if err := svc.ReceiveMessage(context.Background(), id, []byte(body), nil, headers); err != nil {
		t.Fatalf("ReceiveMessage: %v", err)
	}

	row := f4DtLoadHubByMsgID(t, db, "dt-"+fmt.Sprint(id)+"-dp-"+nonce, true)
	if got := f4DtDrain(fetched); len(got) != 1 || got[0] != shared {
		t.Errorf("下载调用 = %v, want 同一个码只起 1 次", got)
	}
	if got := f4DtDrain(stored); len(got) != 1 {
		t.Errorf("转存调用 = %v, want 1 次", got)
	}
	if got := fmt.Sprint(row.Extra["media_urls"]); got != "["+"/files/dingtalk/"+shared+"]" {
		t.Errorf("Extra.media_urls = %s, want 一条不重复的 URL", got)
	}
}
