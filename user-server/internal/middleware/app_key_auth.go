package middleware

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"hivemtk-user/internal/pkg/tenant"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

// AppKeyResolve AppKey 软解析中间件（不强制鉴权，放行语义如下）
//
// 解析顺序：
//  1. 请求带 X-Chat-App-Key -> 解析出 channel 注入上下文（保留溯源能力）
//  2. 请求带 X-Chat-Channel-Id Header 或 body.channel_id -> 用 channel_id 反查 channel
//  3. 都未提供 -> 注入 DEFAULT channel（"default"）并放行
//  4. 渠道禁用返回 403（防误用）
//  5. Origin 白名单不匹配仅记录（X-Origin-Not-Allowed），不阻断访问
//
// 适用场景：用户自己部署本系统后，作为通道嵌入到自有网站，无需 AppKey 鉴权。
// AppKey 仍保留为渠道的软标识（用于日志追踪 + 未来多渠道管理），但不是强制凭证。
// resolver 由装配层注入（service.ChatChannelService 的适配器）。
func AppKeyResolve(resolver ChatChannelResolver) gin.HandlerFunc {
	return func(c *gin.Context) {
		if IsTestMode && testModeGate() {
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
				// 访客链租户解析：channel 行自带租户归属（写入时行自证），此处绑定请求 ctx——
				// /api/chat/public/* 全链的 GORM 访问自动按租户隔离。
				// off 模式 channel.TenantID 恒为默认租户 1，等价单租户现状。
				if channel.TenantID != 0 {
					c.Request = c.Request.WithContext(tenant.WithTenantID(c.Request.Context(), channel.TenantID))
				}
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
		// 路径参数槽位：/channel/:app_key/info 这类把 app_key 建立在路径上的公开口
		// （安装引导「测试 API 连通性」）。此前只认 header/query/body，路径参数被
		// 漏掉——该口在库里没有名为 default 的渠道时永远 401（2026-11-10 实测回归）。
		if channelID == "" {
			if pk := c.Param("app_key"); pk != "" {
				channelID = pk
			}
		}
		if channelID == "" {
			channelID = DefaultChannelID
		}

		// ref 槽位（X-Chat-Channel-Id / channel_id query / body.channel_id）里放的
		// 可能是 app_key：安装引导生成的嵌入代码与 embed-sdk 都以 appKey 作为
		// channel_ref 传入该槽位。解析语义与 service.VisitorChatService.resolveChannel
		// 一致——先按 channel_id 列解析，失败再按 app_key 列回退。
		resolvedViaAppKey := false
		channel, err := resolver.ResolveByChannelID(c.Request.Context(), channelID)
		if err != nil {
			if ch, err2 := resolver.ResolveByAppKey(c.Request.Context(), channelID); err2 == nil {
				channel, err = ch, nil
				resolvedViaAppKey = true
			}
		}
		if err != nil || channel == nil {

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
		if resolvedViaAppKey {
			c.Set("chat_app_key", channelID)
		}
		// 访客链租户绑定：channel 行自带租户归属（写入时行自证），与上方 app_key 分支同语义。
		// channel_id 槽位解析成功的请求同样要绑定租户，否则 on 模式下会在下游 GORM
		// 隔离 callback 处 fail-closed（如 agent_statuses 拒绝无租户上下文的查询）。
		if channel.TenantID != 0 {
			c.Request = c.Request.WithContext(tenant.WithTenantID(c.Request.Context(), channel.TenantID))
		}
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

// ingressAPIKeyEnv 是入口密钥的环境变量名。读取处与 503 文案**共用这一个符号**：
// 此前文案印的是 INGRESS_SECRET、代码读的是 INGRESS_API_KEY，运维照提示配置后仍然 503。
const ingressAPIKeyEnv = "INGRESS_API_KEY"

// IngressSecretAuth 强制校验 X-Ingress-Secret Header 的中间件
// 用于保护内部消息入口（如 /api/chat/ingress），防匿名消息注入 AI 管道
// 密钥来源：环境变量 INGRESS_API_KEY —— 未配置时**一律拒绝**（503），不存在"开发环境默认放行"
func IngressSecretAuth() gin.HandlerFunc {
	secret := strings.TrimSpace(os.Getenv(ingressAPIKeyEnv))
	return func(c *gin.Context) {
		if IsTestMode && testModeGate() {
			c.Next()
			return
		}
		// fail-closed：secret 未配置时拒绝，防止受保护端点退化为无鉴权
		if secret == "" {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"code":    503,
				"message": "入口密钥未配置（" + ingressAPIKeyEnv + "），已拒绝访问",
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
