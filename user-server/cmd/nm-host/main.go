// Go NM Host — Chrome Native Messaging Host（Chrome fork 的子进程，非 daemon）
//
// 职责（设计文档 §5）：
//  1. stdin/stdout 走 Chrome Native Messaging：4 字节 native-order 长度头 + JSON
//     （写方向 Host→扩展单帧 ≤1MiB；读方向扩展→Host ≤4GB）
//  2. 同时作为 WebSocket client 连 user-server 统一端口 /api/browser/host-ws
//  3. 主循环：WS 读命令帧 → 写帧给 Chrome 扩展 → 读扩展回帧 → WS 回传 {req_id,...}
//
// 生命周期：由 Chrome 通过 connectNative 启动；stdin 关闭（port 断开/浏览器退出）即退出。
package main

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

var (
	wsURL    = envOr("HIVE_MTK_WS_URL", "ws://127.0.0.1:8204/api/browser/host-ws")
	hostPort = envOr("HIVE_MTK_PORT", "8204")
	// hostVersion 与扩展 manifest.json version 同步维护（R17：Chrome SW ScriptCache 缓存陷阱
	// 导致旧扩展代码常驻——host/status 版本号是「新代码是否生效」的快速排查锚点）
	hostVersion = envOr("HIVE_MTK_HOST_VERSION", "1.2.0")
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
// 帧限制：host→extension 单条 ≤1MiB（官方硬限制）
func writeNativeFrame(msg map[string]any) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if len(body) > 1*1024*1024 {
		return fmt.Errorf("frame 超过 Chrome host→extension 1MiB 限制")
	}
	header := make([]byte, 4)
	binary.NativeEndian.PutUint32(header, uint32(len(body)))
	if _, err := os.Stdout.Write(header); err != nil {
		return err
	}
	_, err = os.Stdout.Write(body)
	return err
}

// readNativeFrame 从 stdin（Chrome 管控）读 4 字节 native-order 长度头 + JSON body
func readNativeFrame(r *bufio.Reader) (map[string]any, error) {
	header := make([]byte, 4)
	if _, err := readFull(r, header); err != nil {
		return nil, err
	}
	length := binary.NativeEndian.Uint32(header)
	if length > 4*1024*1024*1023/2 { // 防御性上限（官方 extension→host 4GB）
		return nil, fmt.Errorf("frame 过大: %d", length)
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

func main() {
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
	if !strings.Contains(addr, "token=") {
		sep := "?"
		if strings.Contains(addr, "?") {
			sep = "&"
		}
		addr = addr + sep + "token=" + token
	}

	runLoop(addr, token)
}

// runLoop 连 user-server WS，双通道泵循环；断线指数退避重连
func runLoop(addr, token string) {
	backoff := 2 * time.Second
	reader := bufio.NewReader(os.Stdin)

	for {
		hdr := http.Header{"Authorization": []string{"Bearer " + token}}
		conn, _, err := websocket.DefaultDialer.Dial(addr, hdr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[nm-host] 连 user-server 失败: %v（%s 后重试）\n", err, backoff)
			time.Sleep(backoff)
			if backoff < 60*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = 2 * time.Second
		fmt.Fprintln(os.Stderr, "[nm-host] 已连接 user-server:", addr)

		// 注册帧（hostVersion 与扩展 manifest 同步维护：Chrome SW ScriptCache 缓存排查锚点）
		pid, _ := strconv.Atoi(fmt.Sprint(os.Getpid()))
		_ = conn.WriteJSON(map[string]any{"type": "register", "version": hostVersion, "pid": pid})

		exit := pumpLoop(conn, reader)
		_ = conn.Close()
		if exit {
			// stdin 关闭：Chrome 已断开 port（扩展重载/浏览器退出），进程退出
			fmt.Fprintln(os.Stderr, "[nm-host] stdin 关闭，退出")
			return
		}
		fmt.Fprintln(os.Stderr, "[nm-host] WS 断开，重连中...")
	}
}

// pumpLoop 双向泵：WS → stdout（Chrome），stdin → WS（Chrome 回包）。
// 返回 true 表示 stdin 已关闭应退出进程；false 表示 WS 断开应重连。
func pumpLoop(conn *websocket.Conn, reader *bufio.Reader) bool {
	// pumpStdin: Chrome 扩展回帧 → WS
	stdinClosed := make(chan struct{})
	go func() {
		defer close(stdinClosed)
		for {
			resp, err := readNativeFrame(reader)
			if err != nil {
				return // stdin EOF/错误
			}
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteJSON(resp); err != nil {
				return
			}
		}
	}()

	// 主循环: WS 命令帧 → stdout
	for {
		var cmd map[string]any
		if err := conn.ReadJSON(&cmd); err != nil {
			select {
			case <-stdinClosed:
				return true
			default:
			}
			return false
		}
		if err := writeNativeFrame(cmd); err != nil {
			// 扩展通道坏了：回错误帧给 server，让 server 侧命令快速失败
			reqID, _ := cmd["req_id"].(string)
			_ = conn.WriteJSON(map[string]any{"req_id": reqID, "ok": false, "error": "chrome_write: " + err.Error()})
		}
	}
}
