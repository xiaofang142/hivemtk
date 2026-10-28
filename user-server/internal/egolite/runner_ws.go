package egolite

import (
	"context"
	"net/http"
	"strings"
	"time"

	hrepo "hivemtk-user/internal/repository"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// extractRunnerToken 从 query 或 Authorization: Bearer 取 token（host.go extractHostToken 同型）
func extractRunnerToken(ctx *gin.Context) string {
	if t := ctx.Query("token"); t != "" {
		return t
	}
	auth := ctx.GetHeader("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}

// EgoRunnerWSHandler runner WS 入口（调研 §6.5 定案实现）。
//
// 鉴权分层（对照 browser_automation/controller/host.go 三层门的裁剪版）：
//   ① token 门：eg_<userID>_<rand>，KV 双候选 _prev 轮换 + 常量时间比较 + fail-closed（照抄 host_token）；
//   ② 回环门：**按部署形态开关**——服务端配置 egolite_runner_loopback_only=true（形态甲，
//      私域同机）时启用 RemoteAddr 回环校验；形态乙（线上）跳过，由 wss/TLS + device_id 补偿；
//   ③ Origin 门：runner 是本机进程无浏览器 Origin，非空 Origin 拒绝（host.go 同纪律）。
//
// 与 host-ws 的本质差异：无 X-Real-IP 伪造问题时的"采信头"陷阱在这里同样不适用——
// 形态乙下连接必然来自用户公网 IP，任何 IP 顶层校验都做不了，安全全部押在 token 单层
// + 传输层 TLS + device_id 可踢（这是调研 §6.5 已登记的残余风险，非疏漏）。

const runnerHandshakeReadDeadline = 10 * time.Second

// 心跳参数镜像 browser_automation/service/timeouts.go（A6 收口单表：30s ping / 90s 读超时）。
// 没有应用层探测时，确认等待（confirm TTL 默认 300s）、长页面等待这类静默期会被
// 90s 读超时误判成断连——断连钩子会把还在跑的执行整体判 failed，用户点「允许」时
// 放行帧发进的是一条已死的连接。ping/pong 帧两端 readLoop 均已实现（runner 回 pong、
// 服务端 case "pong" 刷新读超时），这里补的只是发送方。
const (
	egoPingInterval      = 30 * time.Second
	egoPingWriteDeadline = 10 * time.Second // 单帧写时限（ping/JSON 同值，timeouts.go 同参）
)

var runnerUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		return r.Header.Get("Origin") == ""
	},
}

type EgoRunnerWSHandler struct {
	registry *EgoRunnerRegistry
	kvRepo   hrepo.PlatformConfigKVRepository
	auth     *egoTokenAuthenticator  // 统一鉴权（令牌行优先，KV 兜底）
	loopback func(*gin.Context) bool // 可注入便于测试；生产用形态开关
	sink     EventSink               // 事件接收器（执行域服务；AttachEventSink 注入）
}

func NewEgoRunnerWSHandler(registry *EgoRunnerRegistry, kvRepo hrepo.PlatformConfigKVRepository) *EgoRunnerWSHandler {
	h := &EgoRunnerWSHandler{registry: registry, kvRepo: kvRepo}
	h.auth = NewEgoTokenAuthenticator(nil, kvRepo)
	h.loopback = h.loopbackIfConfigured
	return h
}

// SetTokenRepository 接入每用户令牌表（多用户各自 runner 的正常路径）。
// 不放进构造器是为了让既有装配与测试保持可用：nil ⇒ 纯 KV 兼容模式。
func (h *EgoRunnerWSHandler) SetTokenRepository(repo EgoRunnerTokenRepository) {
	h.auth = NewEgoTokenAuthenticator(repo, h.kvRepo)
}

// loopbackIfConfigured 形态开关读 KV（egolite_runner_loopback_only）。
// fail-closed 方向：键不存在=形态乙（不启用回环门）——因为线上部署是这个默认形态，
// 键语义是"显式声明同机部署"，不声明就不享受回环门这层防护。
func (h *EgoRunnerWSHandler) loopbackIfConfigured(ctx *gin.Context) bool {
	v, err := h.kvRepo.Get(ctx.Request.Context(), "egolite_runner_loopback_only")
	if err != nil || v != "true" {
		return false
	}
	ip := ctx.RemoteIP()
	if ip == "" || ip == "|" {
		return true // RemoteAddr 畸形 → fail-closed 由下方统一拒绝
	}
	return isLoopbackIP(ip)
}

// Handle GET /api/egolite/runner-ws?token=xxx
// 握手协议：连接后 10s 内第一条帧必须为 register（version/device_id/hostname/ego_version），
// 与 host-ws 握手时限同族（A6 收口）。
func (h *EgoRunnerWSHandler) Handle(ctx *gin.Context) {
	// ① token 门（eg_ 前缀，复用 host_token 的 KV+轮换+常量时间比较机制，
	// 前缀区分见 service.EgoToken 常量；fail-closed：未配置即拒）
	token := extractRunnerToken(ctx)
	userID, ok := h.auth.Authenticate(ctx.Request.Context(), token)
	if !ok {
		ctx.String(http.StatusUnauthorized, "runner token 无效")
		return
	}

	// ② 回环门（形态甲 only）
	if h.loopback(ctx) && !isLoopbackIP(ctx.RemoteIP()) {
		ctx.String(http.StatusForbidden, "runner 通道已配置仅限本机连接（loopback_only=true）")
		return
	}

	conn, err := runnerUpgrader.Upgrade(ctx.Writer, ctx.Request, nil)
	if err != nil {
		return
	}

	// ③ register 握手（时限内读第一帧）
	_ = conn.SetReadDeadline(time.Now().Add(runnerHandshakeReadDeadline))
	var reg struct {
		Type       string `json:"type"`
		Version    string `json:"version"`
		DeviceID   string `json:"device_id"`
		Hostname   string `json:"hostname"`
		EgoVersion string `json:"ego_version"`
		Workspace  string `json:"workspace"`
	}
	if err := conn.ReadJSON(&reg); err != nil || reg.Type != "register" {
		_ = conn.Close()
		return
	}
	_ = conn.SetReadDeadline(time.Time{}) // 清握手时限，进入业务读循环

	registered, reason, err := h.registry.Register(userID, RunnerMeta{
		Version:    reg.Version,
		DeviceID:   reg.DeviceID,
		Hostname:   reg.Hostname,
		EgoVersion: reg.EgoVersion,
		Workspace:  reg.Workspace,
		conn:       conn,
	})
	if err != nil {
		_ = conn.WriteJSON(map[string]any{"type": "rejected", "reason": reason, "error": err.Error()})
		_ = conn.Close()
		return
	}
	_ = conn.WriteJSON(map[string]any{"type": "welcome", "reason": reason})

	// 服务端心跳（pingLoop，host_registry D4a 同型）：30s 一发，runner 自动回 pong，
	// 服务端读循环收到 pong 即刷新 90s 读超时。写失败（半开连对端已死）即关连接，
	// 让读循环走既有断连收尾（Unregister + 钩子）。
	pingStop := make(chan struct{})
	defer close(pingStop)
	go func() {
		t := time.NewTicker(egoPingInterval)
		defer t.Stop()
		for {
			select {
			case <-pingStop:
				return
			case <-t.C:
				if err := registered.SendSpec(map[string]any{"type": "ping"}); err != nil {
					_ = conn.Close()
					return
				}
			}
		}
	}()

	// 业务读循环（事件 ingest / 心跳，runner_events.go）
	h.readEvents(conn, userID)
	h.registry.Unregister(context.Background(), userID, registered)
}
