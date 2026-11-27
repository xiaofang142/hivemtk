package middleware

import (
	"context"
	"crypto/subtle"
	"net/url"
	"os"
	"strings"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/repository"
)

// identifyCodeHeader / identifyCodeQuery 是三路消息入口统一的识别码携带形状。
// 旧 Bridge 凭证头（X-Bridge-Token / bridge_token）在 T058 一次性替换后不再被
// 读取——旧客户端请求会以「缺少识别码」401 被拒（SC-005：旧凭证识别成功率 0%）。
const (
	identifyCodeHeader = "X-Channel-Identify-Code"
	identifyCodeQuery  = "identify_code"
)

// channelIdentifyCodeCandidates 取当前有效账号识别码集合（权威源 = channel_accounts），
// 并兼容存量渠道级码（VALID 并集）。凭证闸门运行在租户解析之前，仓储显式切系统作用域。
func channelIdentifyCodeCandidates() []string {
	repo := repository.NewChannelIdentifyCodeRepository()
	codes, err := repo.ValidCodes(context.Background())
	if err != nil {
		return nil
	}
	return codes
}

// bridgeCredentialCandidates 是 MCP 入口回落链路使用的凭证候选源（历史 seam 名）。
// 切换后它的真实来源就是渠道识别码表，不再读旧 Bridge token KV。
var bridgeCredentialCandidates = channelIdentifyCodeCandidates

func extractIdentifyCode(c *gin.Context) string {
	if v := c.GetHeader(identifyCodeHeader); v != "" {
		return strings.TrimSpace(v)
	}
	if v := c.Query(identifyCodeQuery); v != "" {
		if dec, err := url.QueryUnescape(v); err == nil {
			return strings.TrimSpace(dec)
		}
		return strings.TrimSpace(v)
	}
	return ""
}

func BridgeIngressGuard() gin.HandlerFunc {
	return bridgeIngressGuard(channelIdentifyCodeCandidates)
}

func bridgeIngressGuard(candidatesForRequest func() []string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// G-07：off 是单机内网的历史逃生阀，多租户下无论候选码是否为空都拒绝，
		// 避免配置漂移后悄悄退化成无鉴权入口。
		if tenant.IsMultiTenant() && strings.EqualFold(os.Getenv("BRIDGE_INGEST_AUTH"), "off") {
			response.Error(c, 503, "多租户模式禁止 BRIDGE_INGEST_AUTH=off：请使用账号识别码鉴权")
			c.Abort()
			return
		}
		candidates := candidatesForRequest()
		if len(candidates) == 0 {
			// fail-closed：识别码未生成时默认拒绝。历史部署可显式 BRIDGE_INGEST_AUTH=off
			// 恢复旧的"无鉴权放行"行为（仅限可信内网单机）。
			// G-07：多租户（TENANT_MODE=on）下这条逃生阀会让任意调用方绕过账号识别码闸，
			// 仅凭 tenant_code 把消息写进别的租户——必须硬禁，不能靠运维自觉。
			authOff := strings.EqualFold(os.Getenv("BRIDGE_INGEST_AUTH"), "off")
			if !authOff {
				response.Error(c, 503, "桥接通道未配置账号识别码（请在账号识别码页重置，或显式 BRIDGE_INGEST_AUTH=off 以接受无鉴权模式）")
				c.Abort()
				return
			}
			if tenant.IsMultiTenant() {
				response.Error(c, 503, "多租户模式禁止 BRIDGE_INGEST_AUTH=off：请先生成账号识别码")
				c.Abort()
				return
			}
			c.Next()
			return
		}

		got := extractIdentifyCode(c)
		if got == "" {
			response.Error(c, 401, "缺少 "+identifyCodeHeader)
			c.Abort()
			return
		}

		ok := false
		for _, want := range candidates {
			if subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1 {
				ok = true
				break
			}
		}
		if !ok {
			response.Error(c, 401, "账号识别码无效")
			c.Abort()
			return
		}

		// 下游租户解析继续使用已验证的凭据，再从 URL 剥离 query。
		c.Request.Header.Set(identifyCodeHeader, got)
		// 只减少下游 URL 的暴露；已抵达的反代访问日志不受此处控制。
		if c.Query(identifyCodeQuery) != "" {
			q := c.Request.URL.Query()
			q.Del(identifyCodeQuery)
			c.Request.URL.RawQuery = q.Encode()
		}
		c.Next()
	}
}
