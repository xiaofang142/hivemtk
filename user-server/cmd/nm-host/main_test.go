package main

// 批9 测试：NM Host 的三条卫生线——
//  1. 入帧尺寸闸门（超限整帧丢弃并保持分帧、长度头越界判流错位、且绝不按声明长度先分配）
//  2. 进程级唯一读泵（WS 重连不丢帧、不并发抢读同一个 bufio.Reader）
//  3. token 不进 URL、不进日志（只留 sha256 前 8 位指纹）

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

const testWaitTimeout = 3 * time.Second

// writeFrameBytes 组一条 native messaging 帧（4 字节 native-order 长度头 + body）
func writeFrameBytes(body []byte) []byte {
	h := make([]byte, 4)
	binary.NativeEndian.PutUint32(h, uint32(len(body)))
	return append(h, body...)
}

func feedFrame(t *testing.T, w io.Writer, body string) {
	t.Helper()
	if _, err := w.Write(writeFrameBytes([]byte(body))); err != nil {
		t.Fatalf("写入 stdin 流失败: %v", err)
	}
}

// —— 1. 入帧尺寸闸门 ——

// TestReadNativeFrameDropsOversizedFrameAndKeepsFraming 超限帧必须「整帧读完丢弃」：
// 只判断而不消费字节，下一条正常帧就会从半截开始读——那是全链路乱码，比丢一条命令严重得多。
// 同时要求从帧体前缀捞出 req_id（否则服务端只能干等 30s 超时，
// 超时计数还会把应用面误判成假死并触发 Host 重启）。
func TestReadNativeFrameDropsOversizedFrameAndKeepsFraming(t *testing.T) {
	oversize := make([]byte, nmMaxInboundFrameBytes+1024)
	head := []byte(`{"req_id":"req-77","data":"`)
	copy(oversize, head)
	for i := len(head); i < len(oversize); i++ {
		oversize[i] = 'a'
	}
	next := []byte(`{"req_id":"req-78","ok":true}`)
	stream := append(writeFrameBytes(oversize), writeFrameBytes(next)...)

	r := bufio.NewReader(bytes.NewReader(stream))
	_, err := readNativeFrame(r)
	var tooLarge *frameTooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("超限帧应返回 *frameTooLargeError，got %T: %v", err, err)
	}
	if tooLarge.reqID != "req-77" {
		t.Errorf("未从超限帧前缀捞出 req_id，got %q", tooLarge.reqID)
	}
	if tooLarge.length != uint32(len(oversize)) {
		t.Errorf("length=%d want=%d", tooLarge.length, len(oversize))
	}
	if !strings.Contains(tooLarge.Error(), "nm_frame_too_large") {
		t.Errorf("错误文案丢了 nm_frame_too_large 锚点: %s", tooLarge.Error())
	}

	frame, err := readNativeFrame(r)
	if err != nil {
		t.Fatalf("丢弃超限帧后的下一条正常帧读取失败（分帧被破坏）: %v", err)
	}
	if frame["req_id"] != "req-78" {
		t.Errorf("帧边界错位：读到 req_id=%v want req-78", frame["req_id"])
	}
}

// headerOnlyReader 只交得出 4 字节长度头，之后再被读一次就记下「伸进正文了」。
// 计数挂在 src 层而不是 bufio 层：bufio 会预取，按字节数分不清是谁读的；
// 按「是否发生第二次底层读」判才等价于「判死之前有没有去抽正文」。
type headerOnlyReader struct {
	hdr        []byte
	served     bool
	pastHeader *atomic.Bool
}

func (h *headerOnlyReader) Read(p []byte) (int, error) {
	if !h.served {
		h.served = true
		n := copy(p, h.hdr)
		return n, nil
	}
	h.pastHeader.Store(true)
	return 0, io.EOF
}

// TestReadNativeFrameDesyncHeaderTerminates 长度头超过 Chrome 上限=流已错位，必须判死；
// 并且**不能先按声明长度分配缓冲**（旧实现的防御阈值恰好放过 2GiB，等于先 OOM 再拒绝），
// 也不能去抽正文（判死意味着这条流上的每一个后续字节都不可信，读了才是真错位）。
func TestReadNativeFrameDesyncHeaderTerminates(t *testing.T) {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)

	hdr := make([]byte, 4)
	binary.NativeEndian.PutUint32(hdr, uint32(2*1024*1024*1024)) // 2GiB
	past := &atomic.Bool{}
	r := bufio.NewReader(&headerOnlyReader{hdr: hdr, pastHeader: past})
	_, err := readNativeFrame(r)
	if !errors.Is(err, errStreamDesync) {
		t.Fatalf("2GiB 长度头应判 errStreamDesync，got %v", err)
	}
	if past.Load() {
		t.Error("判死后仍去读正文：越界判定必须发生在消费任何帧体字节之前")
	}

	runtime.ReadMemStats(&after)
	if delta := int64(after.HeapAlloc) - int64(before.HeapAlloc); delta > 64<<20 {
		t.Errorf("判死前分配了 %d 字节——越界检查必须在 make 之前", delta)
	}
}

// TestWriteNativeFrameOutboundCap 命令帧超 1MiB 必须明确报错（Chrome 会直接断 port）。
func TestWriteNativeFrameOutboundCap(t *testing.T) {
	big := map[string]any{"action": "type", "value": strings.Repeat("x", nmMaxOutboundFrameBytes+16)}
	err := writeNativeFrame(big)
	if err == nil {
		t.Fatal("超 1MiB 命令帧应报错")
	}
	if !strings.Contains(err.Error(), "nm_frame_too_large_outbound") {
		t.Errorf("错误锚点丢失: %v", err)
	}

	// 正常帧：写出的字节流必须能被 readNativeFrame 原样读回（长度头 native-order 没写反）
	old := os.Stdout
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = pw
	small := map[string]any{"req_id": "r-1", "action": "click"}
	writeErr := writeNativeFrame(small)
	_ = pw.Close()
	os.Stdout = old
	if writeErr != nil {
		t.Fatalf("正常命令帧被误拒: %v", writeErr)
	}
	frame, err := readNativeFrame(bufio.NewReader(pr))
	if err != nil {
		t.Fatalf("回读命令帧失败: %v", err)
	}
	if frame["req_id"] != "r-1" || frame["action"] != "click" {
		t.Errorf("命令帧往返变形: %v", frame)
	}
}

// —— 2. 进程级唯一读泵 ——

// countingReader 记录同一时刻进入底层 Read 的最大并发数。
// bufio.Reader 不是并发安全的：两条泵共用它会把长度头读成半截。
type countingReader struct {
	src      io.Reader
	inFlight atomic.Int32
	maxSeen  atomic.Int32
}

func (c *countingReader) Read(p []byte) (int, error) {
	n := c.inFlight.Add(1)
	for {
		seen := c.maxSeen.Load()
		if n <= seen || c.maxSeen.CompareAndSwap(seen, n) {
			break
		}
	}
	defer c.inFlight.Add(-1)
	return c.src.Read(p)
}

// TestReconnectKeepsFramesAndSingleReader WS 断开重连期间：
//  1. 断连窗口里 Chrome 送达的回帧必须被下一条连接接走（旧设计：老泵把它写进已关闭的
//     conn 后自灭，这一帧静默丢失，服务端只能等命令超时）；
//  2. 底层 stdin 在任意时刻只有一条泵在读（旧设计：每次重连再开一条，并发撕流）。
func TestReconnectKeepsFramesAndSingleReader(t *testing.T) {
	srv, accepted := newFakeUserServer(t)
	defer srv.Close()

	stdin, stdinWriter := io.Pipe()
	cr := &countingReader{src: stdin}
	inbound := make(chan stdinEvent, nmInboundBufferFrames)
	stdinDone := make(chan struct{})
	startStdinPump(bufio.NewReader(cr), inbound, stdinDone)

	client1 := dialFake(t, srv)
	conn1 := <-accepted
	exit1 := make(chan bool, 1)
	go func() { exit1 <- pumpLoop(client1, inbound, stdinDone) }()

	feedFrame(t, stdinWriter, `{"req_id":"before-drop","ok":true}`)
	if got := readWS(t, conn1); got["req_id"] != "before-drop" {
		t.Fatalf("第一条连接没收到回帧，got %v", got)
	}

	// 连接断：关本地连接 → pumpLoop 从读侧察觉并返回
	_ = client1.Close()
	select {
	case exit := <-exit1:
		if exit {
			t.Fatal("WS 断开不该判进程退出（stdin 还活着）")
		}
	case <-time.After(testWaitTimeout):
		t.Fatal("pumpLoop 没从 WS 断开中返回")
	}

	// 断连窗口里到达的帧：此时没有任何消费端
	feedFrame(t, stdinWriter, `{"req_id":"during-gap","ok":true}`)

	client2 := dialFake(t, srv)
	conn2 := <-accepted
	exit2 := make(chan bool, 1)
	go func() { exit2 <- pumpLoop(client2, inbound, stdinDone) }()
	if got := readWS(t, conn2); got["req_id"] != "during-gap" {
		t.Fatalf("重连后没接走断连窗口的回帧，got %v", got)
	}

	feedFrame(t, stdinWriter, `{"req_id":"after-reconnect","ok":true}`)
	if got := readWS(t, conn2); got["req_id"] != "after-reconnect" {
		t.Errorf("重连后的新帧丢失，got %v", got)
	}

	if max := cr.maxSeen.Load(); max > 1 {
		t.Errorf("同一时刻有 %d 条读泵在抢 stdin（bufio.Reader 非并发安全，帧会被撕开）", max)
	}
	_ = client2.Close()
	_ = stdinWriter.Close()
	select {
	case <-stdinDone:
	case <-time.After(testWaitTimeout):
		t.Fatal("stdin 结束后读泵未宣布终止")
	}
}

// TestPumpLoopRepliesOversizedDropWithReqID 超限帧要在边缘就地回一条带 req_id 的明确失败。
// 不回错的代价不只是「等超时」：noteCmdTimeout 连续 2 条即判应用面假死并下发 shutdown 帧，
// 一条大快照会让好端端的 Host 被重启。
func TestPumpLoopRepliesOversizedDropWithReqID(t *testing.T) {
	srv, accepted := newFakeUserServer(t)
	defer srv.Close()
	client := dialFake(t, srv)
	conn := <-accepted

	inbound := make(chan stdinEvent, nmInboundBufferFrames)
	stdinDone := make(chan struct{})
	exit := make(chan bool, 1)
	go func() { exit <- pumpLoop(client, inbound, stdinDone) }()

	inbound <- stdinEvent{drop: &frameTooLargeError{length: nmMaxInboundFrameBytes + 1, reqID: "req-big"}}
	got := readWS(t, conn)
	if got["req_id"] != "req-big" {
		t.Fatalf("丢弃回包没带上被丢帧的 req_id: %v", got)
	}
	if ok, _ := got["ok"].(bool); ok {
		t.Errorf("丢弃回包必须 ok=false: %v", got)
	}
	if e, _ := got["error"].(string); !strings.Contains(e, "nm_frame_too_large") {
		t.Errorf("丢弃回包错误锚点丢失: %v", e)
	}
	// 捞不到 req_id 时只落 stderr，不往服务端塞一条无法归因的帧
	inbound <- stdinEvent{drop: &frameTooLargeError{length: nmMaxInboundFrameBytes + 1}}
	assertNoFrameWithin(t, conn, 200*time.Millisecond)
	_ = client.Close()
	<-exit
}

// TestPumpLoopLogsOutboundRejectToStderr 命令帧超 Chrome 上限时被就地拒绝：回包给服务端之外，
// host 侧必须留一条同因现场。真机腿实测「回包有、日志无痕」——只查 host 日志会把
// 「这条命令为什么没到扩展」查成没发生过，与入帧丢弃那条边缘纪律不对称。
func TestPumpLoopLogsOutboundRejectToStderr(t *testing.T) {
	srv, accepted := newFakeUserServer(t)
	defer srv.Close()
	client := dialFake(t, srv)
	conn := <-accepted

	oldStderr := os.Stderr
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stderr = pw
	t.Cleanup(func() { os.Stderr = oldStderr })
	captured := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, pr)
		captured <- buf.String()
	}()

	inbound := make(chan stdinEvent, nmInboundBufferFrames)
	stdinDone := make(chan struct{})
	exit := make(chan bool, 1)
	go func() { exit <- pumpLoop(client, inbound, stdinDone) }()

	big := map[string]any{"req_id": "req-out", "action": "type",
		"value": strings.Repeat("x", nmMaxOutboundFrameBytes+16)}
	if err := conn.WriteJSON(big); err != nil {
		t.Fatalf("下发超大命令帧失败: %v", err)
	}
	got := readWS(t, conn)
	if got["req_id"] != "req-out" {
		t.Errorf("拒绝回包没带上被拒帧的 req_id: %v", got)
	}
	if ok, _ := got["ok"].(bool); ok {
		t.Errorf("拒绝回包必须 ok=false: %v", got)
	}
	e, _ := got["error"].(string)
	if !strings.HasPrefix(e, "chrome_write: ") || !strings.Contains(e, "nm_frame_too_large_outbound") {
		t.Errorf("拒绝回包错误锚点丢失: %v", e)
	}

	_ = pw.Close()
	os.Stderr = oldStderr
	var log string
	select {
	case log = <-captured:
	case <-time.After(testWaitTimeout):
		t.Fatal("host stderr 未收敛")
	}
	if !strings.Contains(log, "nm_frame_too_large_outbound") {
		t.Errorf("stderr 缺超限原因，got: %q", log)
	}
	if !strings.Contains(log, "req_id=req-out") {
		t.Errorf("stderr 未就地归因到 req_id，got: %q", log)
	}
	// host 日志不得带 token（本机 token 是环境变量注入，这里只验不该出现的字段名）
	if strings.Contains(log, testSecretToken) {
		t.Errorf("stderr 泄漏 token: %q", log)
	}
	_ = client.Close()
	<-exit
}

// TestStdinEOFExitsProcess stdin 关闭（扩展重载/浏览器退出）后，即使 WS 还活着，
// pumpLoop 也必须走「换进程」出口——否则 host 变成长驻的僵尸注册：WS 在线、stdio 已死。
func TestStdinEOFExitsProcess(t *testing.T) {
	srv, accepted := newFakeUserServer(t)
	defer srv.Close()
	client := dialFake(t, srv)
	<-accepted

	inbound := make(chan stdinEvent, nmInboundBufferFrames)
	stdinDone := make(chan struct{})
	stdin, stdinWriter := io.Pipe()
	startStdinPump(bufio.NewReader(stdin), inbound, stdinDone)
	exit := make(chan bool, 1)
	go func() { exit <- pumpLoop(client, inbound, stdinDone) }()

	_ = stdinWriter.Close() // Chrome 断了 port
	select {
	case <-stdinDone:
	case <-time.After(testWaitTimeout):
		t.Fatal("stdin EOF 后读泵未终止")
	}
	_ = client.Close()
	select {
	case exited := <-exit:
		if !exited {
			t.Error("stdin 已死时 pumpLoop 应返回 true（退出进程），got false")
		}
	case <-time.After(testWaitTimeout):
		t.Fatal("pumpLoop 未返回")
	}
}

// —— 3. token 脱敏 ——

const testSecretToken = "bh_26_supersecretvalue0123456789abcdef"

func TestDialAddrStripsTokenQuery(t *testing.T) {
	got, err := dialAddr("ws://127.0.0.1:8299/api/browser/host-ws?token=" + testSecretToken)
	if err != nil {
		t.Fatalf("dialAddr: %v", err)
	}
	if strings.Contains(got, testSecretToken) || strings.Contains(got, "token=") {
		t.Errorf("拨号 URL 仍带明文 token: %s", got)
	}
	if !strings.Contains(got, "/api/browser/host-ws") {
		t.Errorf("剥 token 时把路径也弄坏了: %s", got)
	}
	// 其它查询参数不能被顺手删掉
	got2, err := dialAddr("ws://127.0.0.1:8299/api/browser/host-ws?foo=1&token=" + testSecretToken)
	if err != nil {
		t.Fatalf("dialAddr(2): %v", err)
	}
	if !strings.Contains(got2, "foo=1") || strings.Contains(got2, testSecretToken) {
		t.Errorf("应只删 token 保留其它参数: %s", got2)
	}
	// 无 token 时原样返回（不改写用户配的地址）
	const plain = "ws://127.0.0.1:8204/api/browser/host-ws"
	if out, err := dialAddr(plain); err != nil || out != plain {
		t.Errorf("无 token 的 URL 被改写了: %s (%v)", out, err)
	}
}

func TestTokenFingerprintAndScrub(t *testing.T) {
	fp := tokenFingerprint(testSecretToken)
	if len(fp) != 8 || strings.ContainsAny(fp, "ghijklmnopqrstuvwxyz") {
		t.Errorf("指纹应为 8 位小写十六进制，got %q", fp)
	}
	if fp != tokenFingerprint(testSecretToken) {
		t.Error("指纹不稳定（同 token 两次结果不同）")
	}
	if strings.HasPrefix(testSecretToken, fp) {
		t.Error("指纹里不应含 token 明文前缀")
	}
	if tokenFingerprint("bh_26_other") == fp {
		t.Error("不同 token 指纹相同——脱敏失效")
	}
	logged := scrubToken("dial ws://127.0.0.1:8299/api/browser/host-ws?token="+testSecretToken+" refused", testSecretToken, fp)
	if strings.Contains(logged, testSecretToken) {
		t.Errorf("日志兜底擦除失败: %s", logged)
	}
	if !strings.Contains(logged, "token#"+fp) {
		t.Errorf("擦除后应留指纹供关联: %s", logged)
	}
	if scrubToken("plain", "", fp) != "plain" {
		t.Error("空 token 不应改动字符串")
	}
}

// —— 静态接线锁：防止「代码还在、调用点丢了」这类假绿 ——

// TestSourceWiringIsLive 三条卫生线各自的落点必须真的接在主链路上。
// 锁的是**调用指针**而不是出现次数：计数式断言挡不住「语句还在、条件被抽空」。
func TestSourceWiringIsLive(t *testing.T) {
	src := readSrc(t, "main.go")
	locks := []struct {
		name      string
		anchor    string
		badIfGone string
	}{
		{"入帧上限判定", "if length > nmMaxInboundFrameBytes {", "上限判定被摘掉=大帧直通服务端"},
		{"Chrome 上限越界判死", "int64(length) > nmChromeFrameCeilingBytes", "越界不再判死=撕裂的流继续读"},
		{"读泵只在 runLoop 起一次", "startStdinPump(bufio.NewReader(os.Stdin), inbound, stdinDone)", "读泵退回 pumpLoop 内=重连撕流"},
		{"token 不进 URL", "runLoop(cleanAddr, token, tokenFingerprint(token))", "改回传 addr=明文 token 上日志"},
		{"WS 写口串行", "writeMu.Lock()", "转发泵与错误回包并发写同一条 conn"},
		{"断连时叫停并等待转发泵", "closeAndJoin()", "转发泵悬在新连接上=并发写/撕流"},
	}
	for _, l := range locks {
		if !strings.Contains(src, l.anchor) {
			t.Errorf("%s：主链路里找不到锚点 %q（%s）", l.name, l.anchor, l.badIfGone)
		}
	}
	// readNativeFrame 只应有一处调用点（第二处就是并发的另一条泵）
	if n := strings.Count(src, "readNativeFrame("); n != 2 {
		t.Errorf("readNativeFrame 出现 %d 处（定义 + 唯一调用点），多出的调用点意味着又开了第二条泵", n)
	}
	// 读泵必须在「连接循环」之前起：锚点文本还在、位置挪进 for 里，
	// 就等于每次重连再起一条泵——containment 检查看不出来，只能按源码顺序判。
	pumpAt := strings.Index(src, "startStdinPump(bufio.NewReader(os.Stdin), inbound, stdinDone)")
	dialAt := strings.Index(src, "websocket.DefaultDialer.Dial(addr, hdr)")
	if pumpAt < 0 || dialAt < 0 {
		t.Fatalf("读泵/拨号锚点丢失（pump=%d dial=%d），顺序锁失去依据", pumpAt, dialAt)
	}
	if pumpAt > dialAt {
		t.Error("读泵起在拨号之后=回到连接循环内，重连会叠加并发读泵")
	}
}

// TestHostRegistryReadLimitHasHeadroomOverHostCap 服务端 read limit 必须**大于**
// nm-host 的边缘上限，否则谁先判取决于时序，Host 连接会被服务端按协议违规掐掉。
func TestHostRegistryReadLimitHasHeadroomOverHostCap(t *testing.T) {
	reg := readSrc(t, "../../internal/browser_automation/service/host_registry.go")
	if !strings.Contains(reg, "c.conn.SetReadLimit(hostFrameReadLimit)") {
		t.Error("readLoop 不再使用 hostFrameReadLimit（回到内联魔数=两端上限脱钩）")
	}
	tm := readSrc(t, "../../internal/browser_automation/service/timeouts.go")
	m := regexp.MustCompile(`hostFrameReadLimit\s*=\s*(\d+)\s*<<\s*(\d+)`).FindStringSubmatch(tm)
	if m == nil {
		t.Fatal("timeouts.go 找不到 hostFrameReadLimit 定义（A6 单表收口被破坏）")
	}
	base, _ := strconv.ParseInt(m[1], 10, 64)
	shift, _ := strconv.ParseInt(m[2], 10, 64)
	limit := base << shift
	if limit <= int64(nmMaxInboundFrameBytes) {
		t.Errorf("hostFrameReadLimit=%d ≤ nm-host 边缘上限 %d：超限帧将由服务端掐断整条连接", limit, nmMaxInboundFrameBytes)
	}
}

// —— helpers ——

func newFakeUserServer(t *testing.T) (*httptest.Server, chan *websocket.Conn) {
	t.Helper()
	accepted := make(chan *websocket.Conn, 4)
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		accepted <- c
	}))
	t.Cleanup(srv.Close)
	return srv, accepted
}

func dialFake(t *testing.T, srv *httptest.Server) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/browser/host-ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, http.Header{"Authorization": []string{"Bearer " + testSecretToken}})
	if err != nil {
		t.Fatalf("拨号假服务端失败: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// readWS 读一帧 JSON，带超时——测试挂死比断言失败更难查。
func readWS(t *testing.T, c *websocket.Conn) map[string]any {
	t.Helper()
	type result struct {
		frame map[string]any
		err   error
	}
	ch := make(chan result, 1)
	go func() {
		var f map[string]any
		_ = c.SetReadDeadline(time.Now().Add(testWaitTimeout))
		err := c.ReadJSON(&f)
		ch <- result{f, err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("读 WS 帧失败: %v", r.err)
		}
		return r.frame
	case <-time.After(testWaitTimeout + time.Second):
		t.Fatal("readWS 超时未返回")
		return nil
	}
}

// assertNoFrameWithin 该时间窗内不该有任何帧到达（无 req_id 的丢弃事件不外发）。
func assertNoFrameWithin(t *testing.T, c *websocket.Conn, d time.Duration) {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(d))
	var f map[string]any
	if err := c.ReadJSON(&f); err == nil {
		t.Errorf("不该外发的帧发出去了: %v", f)
	}
}

var srcCache sync.Map // path -> string（跨用例复用，避免每个锁都重读磁盘）

func readSrc(t *testing.T, rel string) string {
	t.Helper()
	if v, ok := srcCache.Load(rel); ok {
		return v.(string)
	}
	data, err := os.ReadFile(rel)
	if err != nil {
		t.Fatalf("读源文件 %s 失败（静态接线锁失去依据）: %v", rel, err)
	}
	src := string(data)
	srcCache.Store(rel, src)
	return src
}
