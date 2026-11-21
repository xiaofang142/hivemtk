package middleware

import (
	"net/http"
	"strings"

	"hivemtk-user/internal/pkg/utils/logger"

	"github.com/gin-gonic/gin"
)

// MerchantConnectionPath is relative to the authenticated /api group.
const MerchantConnectionPath = "/v1/integrations/merchant-connections"

// MerchantCallbackPath is relative to the public /api group.
const MerchantCallbackPath = "/v1/integrations/merchant-callbacks"

const merchantCallbackLogPath = "/api" + MerchantCallbackPath + "/:provider"
const merchantCallbackAccessLogged = "merchant_callback_access_logged"

func IsMerchantCallbackPath(path string) bool {
	const root = "/api" + MerchantCallbackPath
	return path == root || strings.HasPrefix(path, root+"/")
}

func merchantSafeLogPath(path string) string {
	if IsMerchantCallbackPath(path) {
		return merchantCallbackLogPath
	}
	return path
}

// Authorization inputs can arrive in malformed bodies or in an unexpected query.
// Never read payloads for these exact paths, including before authentication.
func merchantConnectionMetadataOnly(path string) bool {
	if IsMerchantCallbackPath(path) {
		return true
	}
	switch strings.TrimSuffix(path, "/") {
	case "/api" + MerchantConnectionPath, "/api" + MerchantConnectionPath + "/start", "/api" + MerchantConnectionPath + "/complete",
		"/api" + MerchantConnectionPath + "/refresh", "/api" + MerchantConnectionPath + "/revoke":
		return true
	default:
		return false
	}
}

// MerchantCallbackAccessLog runs before early policy rejection and omits even IP/trace
// header-derived values. The ordinary API logger skips the already covered request.
func MerchantCallbackAccessLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !IsMerchantCallbackPath(c.Request.URL.Path) {
			c.Next()
			return
		}
		c.Set(merchantCallbackAccessLogged, true)
		merchantPrivateResponseHeaders(c)
		defer func() {
			if recover() != nil {
				c.AbortWithStatus(http.StatusInternalServerError)
			}
			logMerchantCallbackStatus(c)
		}()
		if c.Request.Method != http.MethodPost {
			c.Header("Allow", http.MethodPost)
			c.AbortWithStatus(http.StatusMethodNotAllowed)
			return
		}
		c.Next()
	}
}

func logMerchantCallbackStatus(c *gin.Context) {
	status := c.Writer.Status()
	log := logger.Ctx(c.Request.Context())
	event := log.Info()
	if status >= 500 {
		event = log.Error()
	} else if status >= 400 {
		event = log.Warn()
	}
	event.Str("event", "api_interaction").Str("path", merchantCallbackLogPath).Int("status", status).Msg("api_interaction")
}

func merchantPrivateResponseHeaders(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
}

// Recovery preserves the usual Gin recovery except where its request dump can expose OAuth data.
func Recovery() gin.HandlerFunc {
	standard := gin.Recovery()
	sensitive := MerchantConnectionRecovery()
	return func(c *gin.Context) {
		if merchantConnectionMetadataOnly(c.Request.URL.Path) {
			sensitive(c)
			return
		}
		standard(c)
	}
}

// MerchantConnectionRecovery runs inside the loggers so they can record the recovered status.
func MerchantConnectionRecovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		if merchantConnectionMetadataOnly(path) {
			merchantPrivateResponseHeaders(c)
			defer func() {
				if recover() != nil {
					c.AbortWithStatus(http.StatusInternalServerError)
					if !IsMerchantCallbackPath(path) {
						logger.Ctx(c.Request.Context()).Error().Str("path", path).
							Int("status", c.Writer.Status()).Msg("merchant_connection_recovered")
					}
				}
			}()
		}
		c.Next()
	}
}
