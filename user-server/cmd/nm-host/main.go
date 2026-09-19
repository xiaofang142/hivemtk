// Go NM Host — Chrome Native Messaging Host（Chrome fork 的子进程，非 daemon）
//
// 职责（设计文档 §5）：
//  1. stdin/stdout 走 Chrome Native Messaging：4 字节 native-order 长度头 + JSON
//     （写方向 Host→扩展单帧 ≤1MiB 是官方硬限；读方向扩展→Host 官方上限 64MiB，
//     本链路在边缘压到 4MiB，理由见 nmMaxInboundFrameBytes 注释）
//  2. 同时作为 WebSocket client 连 user-server 统一端口 /api/browser/host-ws
//  3. 主循环：WS 读命令帧 → 写帧给 Chrome 扩展 → 读扩展回帧 → WS 回传 {req_id,...}
//
// 生命周期：由 Chrome 通过 connectNative 启动；stdin 关闭（port 断开/浏览器退出）即退出。
package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// 帧尺寸上限（批9）。三个量级各司其职，别混用一个数：
const (
	// nmMaxOutboundFrameBytes Host→扩展：Chrome 官方硬限 1MiB，超限 Chrome 直接断 port。
	nmMaxOutboundFrameBytes = 1 << 20
	// nmMaxInboundFrameBytes 扩展→Host 在本链路的封顶。Chrome 官方上限其实是 64MiB
	// （旧注释与旧防御值按「4GB」写，那是文档误传，w3c/webextensions#849 已勘误）。
	// 压到 4MiB 是为了让**边缘**先判：回包还要过服务端 WS read limit
	// （service/timeouts.go hostFrameReadLimit）。把超限帧塞给服务端的后果不是这一条失败，
	// 而是服务端按协议违规掐断整条 Host 连接、连带该用户所有在途命令一起死；
	// 在 host 侧丢弃则只损失这一条，并且还能带着 req_id 明确回错。
	nmMaxInboundFrameBytes = 4 << 20
	// nmChromeFrameCeilingBytes Chrome 不可能写出比这更长的帧；声明长度越界只能是 stdio
	// 流已错位（长度头被撕裂/混进脏字节），继续读只会读出更多垃圾 → 判流损坏退出，
	// 由扩展 onDisconnect 重连拉起全新进程。
	nmChromeFrameCeilingBytes = 64 << 20
	// nmReqIDScanBytes 丢弃超限帧时从帧体前缀里捞 req_id 的窗口（req_id 由本 host 注入在
	// 命令帧里、扩展回包把它排在最前，8KiB 足够覆盖）。
	nmReqIDScanBytes = 8 << 10
	// nmWSWriteTimeout 回包写服务端的时限（原为内联 10s，收口成常量便于归因与变异）。
	nmWSWriteTimeout = 10 * time.Second
)

var (
	wsURL    = envOr("HIVE_MTK_WS_URL", "ws://127.0.0.1:8204/api/browser/host-ws")
	hostPort = envOr("HIVE_MTK_PORT", "8204")
	// hostVersion 与扩展 manifest.json version 同步维护（R17：Chrome SW ScriptCache 缓存陷阱
	// 导致旧扩展代码常驻——host/status 版本号是「新代码是否生效」的快速排查锚点）
	hostVersion = envOr("HIVE_MTK_HOST_VERSION", "1.5.0")
)

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// loadToken 从环境或 ~/.hivemtk/nm_host.conf 读 Host token
func loadToken() string {
	if v := strings.TrimSpace(os.Getenv("HIVE_MTK_HOST_TOKEN")); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(home + "/.hivemtk/nm_host.conf")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "token=") {
			return strings.TrimSpace(strings.TrimPrefix(line, "token="))
		}
	}
	return ""
}

// writeNativeFrame 写 4 字节 native-order 长度头 + JSON body 到 stdout（Chrome 管控）
// 帧限制：host→extension 单条 ≤1MiB（官方硬限，超限 Chrome 直接断 port）
func writeNativeFrame(msg map[string]any) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if len(body) > nmMaxOutboundFrameBytes {
		return fmt.Errorf("nm_frame_too_large_outbound: 命令帧 %d 字节 > Chrome host→extension 上限 %d 字节",
			len(body), nmMaxOutboundFrameBytes)
	}
	header := make([]byte, 4)
	binary.NativeEndian.PutUint32(header, uint32(len(body)))
	if _, err := os.Stdout.Write(header); err != nil {
		return err
	}
	_, err = os.Stdout.Write(body)
	return err
}

// frameTooLargeError 超限入帧：已按声明长度整帧读完丢弃（stdio 分帧保持完好），
// 并从帧体前缀尽量捞出 req_id，好让这条命令在服务端拿到**明确失败**而不是干等超时。
type frameTooLargeError struct {
	length uint32
	reqID  string
}

func (e *frameTooLargeError) Error() string {
	if e.reqID == "" {
		return fmt.Sprintf("nm_frame_too_large: 扩展回帧 %d 字节 > 本链路上限 %d 字节（整帧已丢弃，未能归因 req_id）",
			e.length, nmMaxInboundFrameBytes)
	}
	return fmt.Sprintf("nm_frame_too_large: 扩展回帧 %d 字节 > 本链路上限 %d 字节（整帧已丢弃）",
		e.length, nmMaxInboundFrameBytes)
}

// errStreamDesync 长度头越界：stdio 流已错位，帧边界不可信，这条 host 进程没救了。
var errStreamDesync = errors.New("nm_stdio_desync")

// reqIDRe 从被丢弃帧体的前缀里定位 req_id（扩展回帧由 native-messaging.js 统一封装，
// req_id 在最前，8KiB 窗口足够）。
var reqIDRe = regexp.MustCompile(`"req_id"\s*:\s*"([^"]{1,64})"`)

func extractReqID(body []byte) string {
	if m := reqIDRe.FindSubmatch(body); m != nil {
		return string(m[1])
	}
	return ""
}

// readNativeFrame 从 stdin（Chrome 管控）读 4 字节 native-order 长度头 + JSON body。
// 三种出口必须分清（批9）：
//   - 正常帧 → (frame, nil)
//   - 超限帧 → (nil, *frameTooLargeError)：**整帧已读完丢弃**，流边界仍然对齐，读泵可继续
//   - 长度头超过 Chrome 上限 → (nil, errStreamDesync)：流已错位，读泵终止
func readNativeFrame(r *bufio.Reader) (map[string]any, error) {
	header := make([]byte, 4)
	if _, err := readFull(r, header); err != nil {
		return nil, err
	}
	length := binary.NativeEndian.Uint32(header)
	if int64(length) > nmChromeFrameCeilingBytes {
		return nil, fmt.Errorf("%w: 长度头声明 %d 字节，超过 Chrome 扩展→host 上限 %d 字节",
			errStreamDesync, length, uint32(nmChromeFrameCeilingBytes))
	}
	if length > nmMaxInboundFrameBytes {
		return nil, dropOversizedFrame(r, length)
	}
	body := make([]byte, length)
	if _, err := readFull(r, body); err != nil {
		return nil, err
	}
	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// dropOversizedFrame 把超限帧整帧消费掉（保持后续帧边界）并从前缀里捞 req_id。
// 分配上限只有 nmReqIDScanBytes——旧实现的问题正在于「先按 length 建大缓冲再判断」，
// 一个 2GiB 的长度声明就是一次 OOM；现在越界判定在分配之前。
func dropOversizedFrame(r *bufio.Reader, length uint32) error {
	scan := make([]byte, nmReqIDScanBytes)
	n, err := readFull(r, scan)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if remaining := int64(length) - int64(n); remaining > 0 {
		if _, err := io.CopyN(io.Discard, r, remaining); err != nil {
			return fmt.Errorf("%w: 丢弃超限帧途中断流: %v", errStreamDesync, err)
		}
	}
	return &frameTooLargeError{length: length, reqID: extractReqID(scan[:n])}
}

func readFull(r *bufio.Reader, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := r.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// hostUsage 非 Chrome 启动时的唯一出路：只读信息，绝不连接服务端。
const hostUsage = "用法: hivemtk_browser_nm_host [--version|--help]\n" +
	"本程序由 Chrome 扩展经 connectNative 自动拉起（Chrome 会把 chrome-extension://<id>/ 作为第一个参数传入），不需要手工常驻运行。\n"

// guardInvocation F1（批5e 真机踩坑登记）：Chrome 启动的 NM host 必定带
// argv[1]="chrome-extension://<id>/"；缺这个参数就不是 Chrome 拉起的。
// 旧版 main() 完全不读 os.Args，任何参数（含 --version）都会被忽略后直接连接注册，
// 把扩展正在服务的那条连接的注册位抢走——host/status 照旧显示 count=1/在线，
// 而命令帧送进一条没有 Chrome 端口的连接，表现为连续 30s 超时（session253），
// 只能等 A5 判病自愈（代价 2×30s）。故非 Chrome 启动只允许 --version/--help 两条
// 只读出路，其余一律 fails-loudly 且不连服务端。
func guardInvocation() {
	if len(os.Args) > 1 && strings.HasPrefix(os.Args[1], "chrome-extension://") {
		return // Chrome 启动 → 正常服务模式
	}
	for _, a := range os.Args[1:] {
		switch a {
		case "--version", "-v":
			fmt.Println(hostVersion)
			os.Exit(0)
		case "--help", "-h":
			fmt.Fprint(os.Stderr, hostUsage)
			os.Exit(0)
		}
	}
	fmt.Fprintf(os.Stderr, "[nm-host] 拒绝启动： argv=%v 中没有 Chrome 传入的扩展 origin（chrome-extension://…），"+
		"独立运行会抢走扩展 Host 的注册位。\n%s", os.Args[1:], hostUsage)
	os.Exit(2)
}

// tokenFingerprint 日志关联用的 token 指纹（sha256 前 8 位十六进制）。
// 批9：token 是 Host 的长期凭据。旧实现把它拼进 addr 再整条打印，而 NM host 的 stderr
// 会进 Chrome 的 native messaging host 日志文件——等于把凭据落盘给任何读得到该日志的人。
// 指纹够把「同一次配置的两条日志」对起来，反推不出原值。
func tokenFingerprint(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])[:8]
}

// scrubToken 兜底擦除：第三方库（websocket 拨号错误）可能回显完整 URL。
func scrubToken(s, token, fp string) string {
	if token == "" {
		return s
	}
	return strings.ReplaceAll(s, token, "token#"+fp)
}

// dialAddr 剥掉 URL 上的 token 查询参数。服务端 extractHostToken 先读 Authorization 头、
// query 只是兜底，host 侧凭头鉴权即可，URL 上不必再留明文（也就不必担心谁把 URL 打进日志）。
// 没有 token 参数时原样返回，不改写用户配的地址。
func dialAddr(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	q := u.Query()
	if q.Get("token") == "" {
		return raw, nil
	}
	q.Del("token")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func main() {
	guardInvocation()
	token := loadToken()
	if token == "" {
		// NM Host 的 stderr 仅用于调试日志（stdout 是协议通道）
		fmt.Fprintln(os.Stderr, "[nm-host] 未找到 HIVE_MTK_HOST_TOKEN，请在 ~/.hivemtk/nm_host.conf 配置 token=...")
		// Chrome 的 NM 要求 host 不能立即退出，否则扩展报 disconnected；
		// 保持空转等待配置后由扩展重连触发重启。
		time.Sleep(30 * time.Second)
		os.Exit(1)
	}

	addr := wsURL
	if !strings.HasPrefix(addr, "ws") {
		addr = "ws://127.0.0.1:" + hostPort + "/api/browser/host-ws"
	}
	cleanAddr, err := dialAddr(addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[nm-host] HIVE_MTK_WS_URL 解析失败: %v（30s 后退出，等扩展重连）\n", err)
		time.Sleep(30 * time.Second)
		os.Exit(1)
	}

	runLoop(cleanAddr, token, tokenFingerprint(token))
}

// stdinEvent 进程级读泵的一次产出：frame 与 drop 二选一。
type stdinEvent struct {
	frame map[string]any
	drop  *frameTooLargeError // 超限帧：已被整帧丢弃，帧边界仍然对齐
}

// nmInboundBufferFrames 读泵到连接消费端之间的缓冲帧数。
// WS 重连窗口里 Chrome 送达的回帧不丢（端口是活的，只是暂时没人取），下一条连接接上就继续送；
// 缓冲写满时读泵阻塞——背压交给 Chrome 的 stdio 管道，而不是无限吃内存。
const nmInboundBufferFrames = 8

// startStdinPump 起**唯一**一条 stdin 读泵，goroutine 活到进程结束。
// 批9 修正：旧实现把读泵开在 pumpLoop 里，WS 每重连一次就多一条 goroutine 复用同一个
// bufio.Reader——bufio 不是并发安全的，两条泵会把同一条字节流撕开（长度头读到半截），
// 而且老泵醒来的第一件事是先抢走一帧再往已关闭的 conn 上写，那条命令在服务端只能干等超时。
// 终止（EOF/流错位）时先在 stderr 记因，再关 stdinDone 与 out。
func startStdinPump(r *bufio.Reader, out chan<- stdinEvent, stdinDone chan<- struct{}) {
	go func() {
		defer func() {
			close(stdinDone)
			close(out)
		}()
		for {
			frame, err := readNativeFrame(r)
			if err != nil {
				var tooLarge *frameTooLargeError
				if errors.As(err, &tooLarge) {
					out <- stdinEvent{drop: tooLarge}
					continue
				}
				fmt.Fprintf(os.Stderr, "[nm-host] stdin 读泵终止: %v\n", err)
				return
			}
			out <- stdinEvent{frame: frame}
		}
	}()
}

// runLoop 连 user-server WS，双通道泵循环；断线指数退避重连。
// stdin 读泵只在这里起一次（见 startStdinPump 注释），重连只换消费端。
func runLoop(addr, token, fp string) {
	backoff := 2 * time.Second
	inbound := make(chan stdinEvent, nmInboundBufferFrames)
	stdinDone := make(chan struct{})
	startStdinPump(bufio.NewReader(os.Stdin), inbound, stdinDone)

	for {
		hdr := http.Header{"Authorization": []string{"Bearer " + token}}
		conn, _, err := websocket.DefaultDialer.Dial(addr, hdr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[nm-host] 连 user-server 失败: %s（%s 后重试）\n", scrubToken(err.Error(), token, fp), backoff)
			time.Sleep(backoff)
			if backoff < 60*time.Second {
				backoff *= 2
			}
			select {
			case <-stdinDone:
				return // Chrome 已断开，重连没有意义
			default:
			}
			continue
		}
		backoff = 2 * time.Second
		fmt.Fprintf(os.Stderr, "[nm-host] 已连接 user-server: %s（token#%s）\n", addr, fp)

		// 注册帧（hostVersion 与扩展 manifest 同步维护：Chrome SW ScriptCache 缓存排查锚点）
		pid, _ := strconv.Atoi(fmt.Sprint(os.Getpid()))
		_ = conn.WriteJSON(map[string]any{"type": "register", "version": hostVersion, "pid": pid})

		exit := pumpLoop(conn, inbound, stdinDone)
		_ = conn.Close()
		if exit {
			// stdin 关闭：Chrome 已断开 port（扩展重载/浏览器退出），进程退出
			fmt.Fprintln(os.Stderr, "[nm-host] stdin 关闭，退出")
			return
		}
		fmt.Fprintln(os.Stderr, "[nm-host] WS 断开，重连中...")
	}
}

// pumpLoop 双向泵：WS → stdout（Chrome），stdin（进程级读泵） → WS（Chrome 回包）。
// 返回 true 表示 stdin 已关闭/该换进程应退出；false 表示 WS 断开应重连。
func pumpLoop(conn *websocket.Conn, inbound <-chan stdinEvent, stdinDone <-chan struct{}) bool {
	// 本连接的 WS 写口串行化：转发泵送回包、主循环送错误回包，两条路径会并发写同一条 conn。
	var writeMu sync.Mutex
	writeJSON := func(v any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(nmWSWriteTimeout))
		return conn.WriteJSON(v)
	}

	forwardDone := make(chan struct{})
	stopForward := make(chan struct{})
	closeAndJoin := func() {
		close(stopForward)
		<-forwardDone
	}
	go func() {
		defer close(forwardDone)
		for {
			select {
			case <-stopForward:
				return
			case <-stdinDone:
				return
			case ev, ok := <-inbound:
				if !ok {
					return
				}
				if ev.drop != nil {
					// 超限帧在边缘就地归因：有 req_id 就回一条明确失败，
					// 不让那条命令在服务端陪跑到 30s 超时（超时计数还会误判应用面假死）。
					fmt.Fprintf(os.Stderr, "[nm-host] %s\n", ev.drop.Error())
					if ev.drop.reqID != "" {
						_ = writeJSON(map[string]any{"req_id": ev.drop.reqID, "ok": false, "error": ev.drop.Error()})
					}
					continue
				}
				if err := writeJSON(ev.frame); err != nil {
					return // conn 已坏：主循环从读侧察觉并重连，帧留在读泵缓冲里不丢
				}
			}
		}
	}()

	// 主循环: WS 命令帧 → stdout
	for {
		var cmd map[string]any
		if err := conn.ReadJSON(&cmd); err != nil {
			// 连接断了：先叫停本连接的转发泵再等新连接。
			// 不等它——它可能正握着 writeMu 往这条已死的 conn 上写，悬到新连接上就是并发写。
			closeAndJoin()
			select {
			case <-stdinDone:
				return true
			default:
			}
			return false
		}
		// R27-2 自愈控制帧：服务端判应用面假死后下发（转发前拦截），host 主动退出——
		// Chrome 感知 port 死 → SW onDisconnect 重连 → connectNative 拉起全新 host 进程。
		// 只重连 WS（僵尸注册：WS 活应用死）不足以端到端恢复，必须换进程。
		if action, _ := cmd["action"].(string); action == "__host_shutdown__" {
			fmt.Fprintf(os.Stderr, "[nm-host] 收到 shutdown 控制帧（reason=%v），主动退出触发 Chrome 重拉\n", cmd["reason"])
			close(stopForward) // 只叫停不 join：换进程的延迟不该被一次在途写吃掉
			return true
		}
		if err := writeNativeFrame(cmd); err != nil {
			// 扩展通道坏了：回错误帧给 server，让 server 侧命令快速失败
			reqID, _ := cmd["req_id"].(string)
			// 与入帧丢弃同一条边缘纪律：就地留现场。服务端那侧只有一条回包，
			// host 侧日志是排障副本——真机腿实测回包归因有、stderr 无痕，
			// 事后只看 host 日志会把「谁丢了这条命令」查成没发生过。
			fmt.Fprintf(os.Stderr, "[nm-host] %s（req_id=%s）\n", err, reqID)
			_ = writeJSON(map[string]any{"req_id": reqID, "ok": false, "error": "chrome_write: " + err.Error()})
		}
	}
}
