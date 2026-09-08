package middleware

import (
	"context"
	"crypto/subtle"
	"net/url"
	"os"
	"strings"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/repository"
)

func bridgeTokenCandidates() []string {
	out := []string{}
	kvRepo := repository.NewSystemConfigKVRepository()
	ctx := context.Background()
	if v, err := kvRepo.Get(ctx, "bridge_ingest_token"); err == nil && strings.TrimSpace(v) != "" {
		out = append(out, strings.TrimSpace(v))
	}
	if v, err := kvRepo.Get(ctx, "bridge_ingest_token_prev"); err == nil && strings.TrimSpace(v) != "" {
		out = append(out, strings.TrimSpace(v))
	}
	if len(out) > 0 {
		return out
	}
	if v := strings.TrimSpace(os.Getenv("BRIDGE_INGEST_TOKEN")); v != "" {
		out = append(out, v)
	}
	if v := strings.TrimSpace(os.Getenv("BRIDGE_INGEST_TOKEN_PREV")); v != "" {
		out = append(out, v)
	}
	return out
}

func extractBridgeToken(c *gin.Context) string {
	if v := c.GetHeader("X-Bridge-Token"); v != "" {
		return strings.TrimSpace(v)
	}
	if v := c.Query("bridge_token"); v != "" {
		if dec, err := url.QueryUnescape(v); err == nil {
			return strings.TrimSpace(dec)
		}
		return strings.TrimSpace(v)
	}
	return ""
}

func BridgeIngressGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		candidates := bridgeTokenCandidates()
		if len(candidates) == 0 {
			// fail-closed：token 未配置时默认拒绝。历史部署可显式 BRIDGE_INGEST_AUTH=off
			// 恢复旧的"无鉴权放行"行为（仅限可信内网）。
			if !strings.EqualFold(os.Getenv("BRIDGE_INGEST_AUTH"), "off") {
				response.Error(c, 503, "桥接通道需配置 BRIDGE_INGEST_TOKEN（或显式 BRIDGE_INGEST_AUTH=off 以接受无鉴权模式）")
				c.Abort()
				return
			}

			c.Next()
			return
		}

		got := extractBridgeToken(c)
		if got == "" {
			response.Error(c, 401, "缺少 X-Bridge-Token")
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
			response.Error(c, 401, "bridge token 无效")
			c.Abort()
			return
		}

		if c.Query("bridge_token") != "" {
			q := c.Request.URL.Query()
			q.Del("bridge_token")
			c.Request.URL.RawQuery = q.Encode()
		}
		c.Next()
	}
}
