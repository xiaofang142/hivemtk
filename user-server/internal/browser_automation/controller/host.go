package controller

import (
	"net/http"
	"strings"
	"time"

	"hivemtk-user/internal/browser_automation/service"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/pkg/utils/response"
	hrepo "hivemtk-user/internal/repository"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// HostController NM Host 状态（admin 全量 / 登录用户读自己）+ Host WS 入口
type HostController struct {
	registry *service.HostRegistry
	kvRepo   hrepo.SystemConfigKVRepository
}

func NewHostController(registry *service.HostRegistry, kvRepo hrepo.SystemConfigKVRepository) *HostController {
	return &HostController{registry: registry, kvRepo: kvRepo}
}

// hostHandshakeReadDeadline register 帧握手读时限（A6）：Host 连上后 10s 内必须报
// register 帧，否则判僵尸连接关闭——与 service/timeouts.go 同族，跨包不引私有常量。
const hostHandshakeReadDeadline = 10 * time.Second

// GetStatus GET /browser-automation/host/status
// admin 看全量、其他登录用户只看自己，但响应形状一致（count/hosts 恒在，见 StatusFor）。
func (c *HostController) GetStatus(ctx *gin.Context) {
	isAdmin := false
	if role, _ := ctx.Get("role"); role == "admin" {
		isAdmin = true
	}
	response.Success(ctx, c.registry.StatusFor(ctx.GetUint("user_id"), isAdmin), "ok")
}

// ResetToken POST /browser-automation/host/token/reset
// 生成新 token 并写入 KV（旧值滚入 _prev，双 token 轮换，参照 bridge token 机制）
func (c *HostController) ResetToken(ctx *gin.Context) {
	token, err := service.RotateHostToken(ctx.Request.Context(), c.kvRepo)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "重置 token 失败: "+err.Error())
		return
	}
	response.Success(ctx, gin.H{"token": token}, "已重置（旧 token 24h 内仍可用）")
}

const hostTokenUpgraderBufferSize = 4096

var hostUpgrader = websocket.Upgrader{
	ReadBufferSize:  hostTokenUpgraderBufferSize,
	WriteBufferSize: hostTokenUpgraderBufferSize,
	// Host 是本机进程，无浏览器 Origin；非空 Origin 一律拒绝
	CheckOrigin: func(r *http.Request) bool {
		return r.Header.Get("Origin") == ""
	},
}

// HostWSHandler Host WebSocket 入口（双层防护：token + 本地回环 IP）
type HostWSHandler struct {
	registry *service.HostRegistry
	kvRepo   hrepo.SystemConfigKVRepository
}

func NewHostWSHandler(registry *service.HostRegistry, kvRepo hrepo.SystemConfigKVRepository) *HostWSHandler {
	return &HostWSHandler{registry: registry, kvRepo: kvRepo}
}

// Handle GET /api/browser/host-ws?token=xxx（或 Authorization: Bearer xxx）
// 握手协议：连接后第一条帧必须为 {"type":"register","version":"1.0.0","pid":N}，
// 且 token 必须能解析出归属 user_id（token 形如 "bh_<userID>_<rand>"，admin 生成时绑定）。
func (h *HostWSHandler) Handle(ctx *gin.Context) {
	// 1. IP 白名单：仅本地回环（frp 回源取 X-Real-IP，非回环即拒绝）
	ip := clientIPOf(ctx)
	if ip != "127.0.0.1" && ip != "::1" {
		logger.Warnf("[BrowserHostWS] 拒绝非本地连接 ip=%s", ip)
		response.Error(ctx, http.StatusForbidden, "Host 通道仅限本机连接")
		return
	}

	// 2. token 校验（fail-closed：未配置即拒绝）
	token := extractHostToken(ctx)
	userID, ok := service.ValidateHostToken(ctx.Request.Context(), h.kvRepo, token)
	if !ok {
		response.Error(ctx, http.StatusUnauthorized, "Host token 无效")
		return
	}

	conn, err := hostUpgrader.Upgrade(ctx.Writer, ctx.Request, nil)
	if err != nil {
		logger.Warnf("[BrowserHostWS] 协议升级失败: %v", err)
		return
	}

	// 3. 读 register 帧（握手时限：与 service/timeouts.go 同族，A6 收口；
	// 跨包不共享常量——引用另一包的私有预算表收益不抵耦合）
	_ = conn.SetReadDeadline(time.Now().Add(hostHandshakeReadDeadline))
	var reg struct {
		Type    string `json:"type"`
		Version string `json:"version"`
		PID     int    `json:"pid"`
	}
	if err := conn.ReadJSON(&reg); err != nil || reg.Type != "register" {
		logger.Warnf("[BrowserHostWS] register 帧无效 user=%d: %v", userID, err)
		_ = conn.Close()
		return
	}
	_ = conn.SetReadDeadline(time.Time{})

	h.registry.Register(userID, reg.Version, reg.PID, conn)
	// Register 内部启动 readLoop；连接断开时 close() → onDisconnect 钩子清理 running sessions
}

func extractHostToken(ctx *gin.Context) string {
	auth := ctx.GetHeader("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
	}
	return strings.TrimSpace(ctx.Query("token"))
}

func clientIPOf(ctx *gin.Context) string {
	if v := strings.TrimSpace(ctx.GetHeader("X-Real-IP")); v != "" {
		return v
	}
	if v := strings.TrimSpace(ctx.GetHeader("X-Forwarded-For")); v != "" {
		parts := strings.Split(v, ",")
		return strings.TrimSpace(parts[0])
	}
	return ctx.ClientIP()
}
