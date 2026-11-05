package health

// 健康检查控制器（基础设施路由；2026-11-05 二次审计：从 router 迁入）。
// 不 import gorm：数据库探活由装配点注入 DBProbe，controller 不持有数据库句柄。

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/app"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/response"
)

// Pinger Redis 健康检查接口（避免强制依赖 go-redis 模块）。
type Pinger interface {
	Ping(ctx context.Context) error
}

// DBProbe 数据库探活：configured=false 表示未装配；err 非空表示不可用。
type DBProbe func(ctx context.Context) (configured bool, err error)

// Handler 健康检查。
type Handler struct {
	redis Pinger
	db    DBProbe
}

func New(redis Pinger, db DBProbe) *Handler {
	return &Handler{redis: redis, db: db}
}

func inferenceStatus() string {
	f := app.GetGlobalProviderFailover()
	if f == nil {
		return "not_configured"
	}
	all := f.GetAllHealth()
	if len(all) == 0 {
		return "not_configured"
	}
	for _, h := range all {
		if h.Status == llm.ProviderStatusUp {
			return "up"
		}
	}
	return "down"
}

func embeddingStatus() (string, string) {
	svc := llm.NewEmbeddingService()
	if svc == nil {
		return "not_configured", ""
	}
	base := svc.DefaultConfig().BaseURL
	if base == "" {
		return "not_configured", ""
	}
	u, err := url.Parse(base)
	if err != nil {
		return "down", "invalid embedding base url: " + err.Error()
	}

	// 云端 https：TCP 443 握手成功 ≠ 服务可用（LB/网关收下连接但后端 5xx 时 TCP 照样通），
	// 必须做真实 HTTP GET 才能探出 5xx / 协议层故障。
	// 判定口径：拿到任意 HTTP 响应（2xx/3xx/4xx，含 401 鉴权拒绝——说明服务在响应）= up；
	// 5xx 或连接失败/超时 = down。GET 配置的 base 路径（不带 key）：
	// DashScope 无免鉴权健康路径，/compatible-mode/v1 等会回 401/404/405，均视为「可达」。
	if u.Scheme == "https" {
		return probeEmbeddingHTTP(u)
	}

	// 本地 http（如 127.0.0.1:8208 / mtk-embedding:8208）：保持历史 TCP dial 口径不变，
	// 避免本机 8208 未启动时的判定语义变化（连接被拒 → down，与改造前一致）。
	host := u.Hostname()
	port := u.Port()
	if port == "" {
		// URL 未显式带端口时按 scheme 取默认端口（https→443、http→80），
		// 不能再硬编码 8208——否则云端 embedding URL（如 https://dashscope.aliyuncs.com/...）
		// 会被误探 8208 端口而误报 down。本地配置都带显式端口（:8208），行为不受影响。
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 2*time.Second)
	if err != nil {
		return "down", "embedding unreachable: " + err.Error()
	}
	utils.WarnErrKV("router.health.embedClose", conn.Close(),
		"host", host, "port", port)
	return "up", ""
}

// probeEmbeddingHTTP 对 https embedding base 做真实 HTTP GET 探活（2s 超时）。
// 2xx/3xx/4xx → up（401 说明服务在响应）；5xx / 连接失败 / 超时 → down。
func probeEmbeddingHTTP(u *url.URL) (string, string) {
	client := &http.Client{Timeout: 2 * time.Second}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, u.String(), nil)
	if err != nil {
		return "down", "invalid embedding probe request: " + err.Error()
	}
	resp, err := client.Do(req)
	if err != nil {
		return "down", "embedding unreachable: " + err.Error()
	}
	defer func() { _ = resp.Body.Close() }()
	// 只排掉开头一段，避免把响应体攒在内存里（也别 Close 不读：连接无法复用无所谓，健康检查低频）
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= http.StatusInternalServerError {
		return "down", fmt.Sprintf("embedding http status=%d", resp.StatusCode)
	}
	return "up", ""
}

// Check 全维度健康检查：应用、数据库、Redis、推理栈 四层状态。
func (h *Handler) Check(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()

	checks := gin.H{}
	overallOK := true

	dbStatus := "ok"
	dbErr := ""
	configured, probeErr := h.db(ctx)
	if !configured {
		dbStatus = "not_configured"
	} else if probeErr != nil {
		dbStatus = "down"
		dbErr = probeErr.Error()
		overallOK = false
	}
	checks["database"] = gin.H{"status": dbStatus, "error": dbErr}

	redisStatus := "not_configured"
	redisErr := ""
	if h.redis != nil {
		redisStatus = "ok"
		if err := h.redis.Ping(ctx); err != nil {
			redisStatus = "down"
			redisErr = err.Error()
			overallOK = false
		}
	}
	checks["redis"] = gin.H{"status": redisStatus, "error": redisErr}

	infStatus := inferenceStatus()
	infErr := ""
	if infStatus == "down" {
		overallOK = false
		infErr = "no healthy LLM provider"
	}
	checks["inference"] = gin.H{"status": infStatus, "error": infErr}

	embStatus, embErr := embeddingStatus()
	checks["embedding"] = gin.H{"status": embStatus, "error": embErr}
	if embStatus == "down" && os.Getenv("HEALTH_EMBEDDING_CRITICAL") == "true" {
		overallOK = false
	}

	data := gin.H{
		"status":    "ok",
		"timestamp": time.Now().Unix(),
		"service":   "user-server",
		"checks":    checks,
	}
	if !overallOK {
		data["status"] = "degraded"
		response.ErrorWithBusinessCode(c, 50301, "service degraded", data)
		return
	}
	response.Success(c, data, "ok")
}

// Live 存活性检查（轻量级，不检查依赖）。
func (h *Handler) Live(c *gin.Context) {
	response.Success(c, gin.H{
		"status":    "alive",
		"timestamp": time.Now().Unix(),
	}, "ok")
}

// Ready 就绪性检查（检查依赖）。
func (h *Handler) Ready(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 1*time.Second)
	defer cancel()

	configured, dbErr := h.db(ctx)
	if !configured {
		notReadyResponse(c, 50302, "database not initialized")
		return
	}
	if dbErr != nil {
		notReadyResponse(c, 50303, "database: "+dbErr.Error())
		return
	}
	if h.redis != nil {
		if err := h.redis.Ping(ctx); err != nil {
			notReadyResponse(c, 50304, "redis: "+err.Error())
			return
		}
	}
	if infStatus := inferenceStatus(); infStatus == "down" {
		notReadyResponse(c, 50305, "inference: no healthy LLM provider")
		return
	}
	if embStatus, embErr := embeddingStatus(); embStatus == "down" && os.Getenv("HEALTH_EMBEDDING_CRITICAL") == "true" {
		notReadyResponse(c, 50306, "embedding: "+embErr)
		return
	}
	response.Success(c, gin.H{
		"status":    "ready",
		"timestamp": time.Now().Unix(),
	}, "ok")
}

func notReadyResponse(c *gin.Context, code int, reason string) {
	response.Envelope(c, http.StatusServiceUnavailable, gin.H{
		"code":    code,
		"message": reason,
		"data": gin.H{
			"status": "not_ready",
			"reason": reason,
		},
	})
}
