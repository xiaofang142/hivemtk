package middleware

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// AppKeyResolve AppKey 软解析中间件（不再强制鉴权）
//
// 设计原则（私域部署优化）：
//  1. 不再强制要求 X-Chat-App-Key Header
//  2. 如果请求带 X-Chat-App-Key -> 解析出 channel 注入上下文（保留溯源能力）
//  3. 如果请求带 X-Chat-Channel-Id Header 或 body.channel_id -> 用 channel_id 反查 channel
//  4. 都未提供 -> 注入 DEFAULT channel（"default"）并放行
//  5. 渠道禁用返回 403（防误用）
//  6. 不再做 Origin 白名单阻断（仅记录，不影响访问）
//
// 使用方式：r.Group("/api/chat/public").Use(AppKeyResolve(resolver))
//
// 适用场景：用户自己部署本系统后，作为通道嵌入到自有网站，无需 AppKey 鉴权。
// AppKey 仍保留为渠道的软标识（用于日志追踪 + 未来多渠道管理），但不再作为强制凭证。
// resolver 由装配层注入（service.ChatChannelService 的适配器）。
func AppKeyResolve(resolver ChatChannelResolver) gin.HandlerFunc {
	return func(c *gin.Context) {
		if IsTestMode && testing.Testing() {
			c.Next()
			return
		}

		appKey := strings.TrimSpace(c.GetHeader("X-Chat-App-Key"))
		channelIDHeader := strings.TrimSpace(c.GetHeader("X-Chat-Channel-Id"))
		channelIDQuery := strings.TrimSpace(c.Query("channel_id"))

		if appKey != "" {
			channel, err := resolver.ResolveByAppKey(c.Request.Context(), appKey)
			if err == nil {
				if !channel.Active {
					c.JSON(http.StatusForbidden, gin.H{
						"code":    403,
						"message": "渠道已被禁用",
					})
					c.Abort()
					return
				}
				c.Set("chat_channel", channel)
				c.Set("chat_channel_id", channel.ChannelID)
				c.Set("chat_app_key", appKey)
				c.Next()
				return
			}
		}

		channelID := channelIDHeader
		if channelID == "" {
			channelID = channelIDQuery
		}
		if channelID == "" {
			if bid := extractBodyChannelID(c); bid != "" {
				channelID = bid
			}
		}
		if channelID == "" {
			channelID = DefaultChannelID
		}

		channel, err := resolver.ResolveByChannelID(c.Request.Context(), channelID)
		if err != nil {

			c.JSON(http.StatusUnauthorized, gin.H{
				"code":    401,
				"message": "渠道未配置或不可用: " + channelID,
			})
			c.Abort()
			return
		}

		if !channel.Active {
			c.JSON(http.StatusForbidden, gin.H{
				"code":    403,
				"message": "渠道已被禁用",
			})
			c.Abort()
			return
		}

		origin := c.GetHeader("Origin")
		if origin != "" {
			origins := channel.AllowedOrigins
			allowed := false
			for _, o := range origins {
				if o == "*" || strings.EqualFold(o, origin) {
					allowed = true
					break
				}
			}
			if !allowed && len(origins) > 0 {
				c.Header("X-Origin-Not-Allowed", origin)
			}
		}

		c.Set("chat_channel", channel)
		c.Set("chat_channel_id", channel.ChannelID)
		c.Next()
	}
}

// GetChatChannel 从上下文获取 chat channel（由 AppKeyResolve 注入）
// 即使没有真实 channel，也会返回一个 placeholder（含 ChannelID）
func GetChatChannel(c *gin.Context) (*ChatChannelView, bool) {
	v, ok := c.Get("chat_channel")
	if !ok {
		return nil, false
	}
	ch, ok := v.(*ChatChannelView)
	return ch, ok
}

// GetChatChannelID 软获取 channel_id（一定返回非空）
// 如果上下文中没有 channel_id，会从 X-Chat-Channel-Id Header / query 中提取，最后 fallback 到 DefaultChannelID
func GetChatChannelID(c *gin.Context) string {
	if v, ok := c.Get("chat_channel_id"); ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	if h := strings.TrimSpace(c.GetHeader("X-Chat-Channel-Id")); h != "" {
		return h
	}
	if q := strings.TrimSpace(c.Query("channel_id")); q != "" {
		return q
	}
	return DefaultChannelID
}

func extractBodyChannelID(c *gin.Context) string {
	if c.Request == nil || c.Request.Body == nil {
		return ""
	}
	if ct := c.GetHeader("Content-Type"); !strings.Contains(ct, "application/json") {
		return ""
	}
	if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead {
		return ""
	}
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return ""
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	var probe struct {
		ChannelID string `json:"channel_id"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return ""
	}
	return strings.TrimSpace(probe.ChannelID)
}

// DefaultChannelID 默认渠道 ID（无渠道时使用）
// 私域部署：单租户，所有未指定 channel 的访客都归入此 channel
const DefaultChannelID = "default"

// IngressSecretAuth 强制校验 X-Ingress-Secret Header 的中间件
// 用于保护内部消息入口（如 /api/chat/ingress），防匿名消息注入 AI 管道
// 密钥来源：环境变量 INGRESS_API_KEY（未配置时开发环境默认放行，生产需配置）
func IngressSecretAuth() gin.HandlerFunc {
	secret := strings.TrimSpace(os.Getenv("INGRESS_API_KEY"))
	return func(c *gin.Context) {
		if IsTestMode && testing.Testing() {
			c.Next()
			return
		}
		// fail-closed：secret 未配置时拒绝，防止受保护端点退化为无鉴权
		if secret == "" {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"code":    503,
				"message": "入口 secret 未配置（INGRESS_SECRET），已拒绝访问",
			})
			c.Abort()
			return
		}
		provided := strings.TrimSpace(c.GetHeader("X-Ingress-Secret"))
		if provided == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(secret)) != 1 {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code":    401,
				"message": "无效的入口凭证",
			})
			c.Abort()
			return
		}
		c.Next()
	}
}
