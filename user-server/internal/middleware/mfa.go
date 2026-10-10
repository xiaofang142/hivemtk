package middleware

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

// MFAVerifyContextKey 上下文键：表示请求已通过 MFA 验证
const MFAVerifyContextKey = "mfa_verified"

const (
	// DefaultMfaRecentVerifyTTL 是参数中心 middleware.mfa_recent_verify_ttl 的兜底值。
	DefaultMfaRecentVerifyTTL = 5 * time.Minute
	mfaRecentVerifyKeyFmt     = "mfa:verified:%d"
)

var (
	mfaRecentVerify      = make(map[uint]time.Time)
	mfaRecentVerifyMutex sync.RWMutex
	// mfaRecentVerifyTTLProvider 由 internal/app 的参数装配层注入。
	// 传 nil 表示不注入（装配顺序错时不该把兜底值顶掉）。
	mfaRecentVerifyTTLProvider = func() time.Duration { return DefaultMfaRecentVerifyTTL }
)

// SetMfaRecentVerifyTTLProvider 注入 MFA 二次验证宽限期读取器，仅装配层调用。
func SetMfaRecentVerifyTTLProvider(fn func() time.Duration) {
	if fn != nil {
		mfaRecentVerifyTTLProvider = fn
	}
}

// MFARecentVerifyTTL 返回生效中的 MFA 宽限期。
// 这是一段「已经过了二次验证」的免打扰窗口：调长它等于把敏感操作的二次验证要求放宽。
func MFARecentVerifyTTL() time.Duration {
	ttl := mfaRecentVerifyTTLProvider()
	if ttl <= 0 {
		return DefaultMfaRecentVerifyTTL
	}
	return ttl
}

func MarkMFAVerified(userID uint) {
	now := time.Now()

	key := fmt.Sprintf(mfaRecentVerifyKeyFmt, userID)
	if err := cache.GetGlobalCache().Set(context.Background(), key, now.Unix(), MFARecentVerifyTTL()); err != nil {
		logger.Warnf("MarkMFAVerified Redis Set failed, fallback to memory: %v", err)

		mfaRecentVerifyMutex.Lock()
		mfaRecentVerify[userID] = now
		mfaRecentVerifyMutex.Unlock()
	}

	mfaCleanupTrigger.Do(func() {
		go mfaCleanupLoop()
	})
}

var mfaCleanupTrigger sync.Once

func mfaCleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		cleanupExpiredMFAVerified()
	}
}

func IsMFAVerifiedRecently(userID uint) bool {

	key := fmt.Sprintf(mfaRecentVerifyKeyFmt, userID)
	val, err := cache.GetGlobalCache().Get(context.Background(), key)
	if err == nil && val != "" {

		var ts int64
		if _, scanErr := fmt.Sscanf(val, "%d", &ts); scanErr == nil {
			verifiedAt := time.Unix(ts, 0)
			if time.Since(verifiedAt) < MFARecentVerifyTTL() {
				return true
			}
		}
	}

	mfaRecentVerifyMutex.RLock()
	t, ok := mfaRecentVerify[userID]
	mfaRecentVerifyMutex.RUnlock()
	if !ok {
		return false
	}
	return time.Since(t) < MFARecentVerifyTTL()
}

func cleanupExpiredMFAVerified() {
	mfaRecentVerifyMutex.Lock()
	defer mfaRecentVerifyMutex.Unlock()
	now := time.Now()
	for k, v := range mfaRecentVerify {
		if now.Sub(v) >= MFARecentVerifyTTL() {
			delete(mfaRecentVerify, k)
		}
	}
}

// RequireMFARecent 中间件：要求用户在最近 5 分钟内通过 MFA 验证
// 用于敏感操作（修改密码、修改邮箱、修改权限等）
//
// 用法：
//
//	r.PUT("/api/users/:id/password",
//	    middleware.JWTAuthMiddleware(),
//	    middleware.RequireMFARecent(),
//	    controller.ResetPassword)
func RequireMFARecent() gin.HandlerFunc {
	return func(c *gin.Context) {
		userIDRaw, exists := c.Get("user_id")
		if !exists {
			response.Error(c, http.StatusUnauthorized, "未找到用户信息")
			c.Abort()
			return
		}
		userID, ok := userIDRaw.(uint)
		if !ok {
			response.Error(c, http.StatusInternalServerError, "用户 ID 类型错误")
			c.Abort()
			return
		}

		if !IsMFAVerifiedRecently(userID) {
			response.Error(c, http.StatusForbidden, "需要二次验证（MFA）才能执行此操作")
			c.Abort()
			return
		}

		c.Set(MFAVerifyContextKey, true)
		c.Next()
	}
}

// RequireMFAEnabled 中间件：要求用户已启用 MFA 才能访问
// 用于：高敏感接口（如导出全部客户数据、批量删除）
func RequireMFAEnabled(mfaEnabledFunc func(userID uint) (bool, error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		userIDRaw, exists := c.Get("user_id")
		if !exists {
			response.Error(c, http.StatusUnauthorized, "未找到用户信息")
			c.Abort()
			return
		}
		userID, ok := userIDRaw.(uint)
		if !ok {
			response.Error(c, http.StatusInternalServerError, "用户 ID 类型错误")
			c.Abort()
			return
		}

		enabled, err := mfaEnabledFunc(userID)
		if err != nil {
			response.Error(c, http.StatusInternalServerError, "查询 MFA 状态失败")
			c.Abort()
			return
		}
		if !enabled {
			response.Error(c, http.StatusForbidden, "此操作要求启用 MFA")
			c.Abort()
			return
		}

		c.Next()
	}
}
