package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// 第十八轮（2026-09-19）：中间件必须拦住"账号已被禁用/删除/改密/改角色"后仍被持有的旧令牌。

// signTokenWithIat 用生产同一套密钥/签发者铸一把指定 iat 的令牌。
// 不复用 GenerateToken 是因为它把 iat 钉在 now，无法稳定构造"吊销之前签发"的样本。
func signTokenWithIat(t *testing.T, userID uint, role string, iat time.Time) string {
	t.Helper()
	cfg := utils.DefaultJWTConfig
	claims := utils.CustomClaims{
		UserID:   userID,
		Username: "u-" + strconv.FormatUint(uint64(userID), 10),
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(iat.Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(iat),
			NotBefore: jwt.NewNumericDate(iat.Add(-time.Minute)),
			Issuer:    cfg.Issuer,
		},
	}
	s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(cfg.SecretKey))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return s
}

func markRevoked(t *testing.T, userID uint, cutoff int64) {
	t.Helper()
	if err := cache.GetGlobalCache().Set(context.Background(),
		"jwt:revoked_before:"+strconv.FormatUint(uint64(userID), 10),
		strconv.FormatInt(cutoff, 10), time.Hour); err != nil {
		t.Fatalf("set marker: %v", err)
	}
}

func newProtectedRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/ping", JWTAuthMiddleware(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"role": c.GetString("role")})
	})
	return r
}

func doAuthed(r *gin.Engine, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// TestJWTAuthMiddleware_BlocksRevokedToken 吊销水位线之后的受保护请求必须 401。
func TestJWTAuthMiddleware_BlocksRevokedToken(t *testing.T) {
	prev := IsTestMode
	IsTestMode = false
	defer func() { IsTestMode = prev }()

	r := newProtectedRouter()
	const uid = uint(910001)
	old := time.Now().Add(-2 * time.Hour)
	token := signTokenWithIat(t, uid, "admin", old)

	// 先证明未吊销时可用（否则这条测试可能因为别的原因一直 401 而假绿）
	if w := doAuthed(r, token); w.Code != http.StatusOK {
		t.Fatalf("前置条件失败：未吊销时应 200，实际 %d %s", w.Code, w.Body.String())
	}

	markRevoked(t, uid, time.Now().Unix())
	w := doAuthed(r, token)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("吊销后旧令牌必须 401，实际 %d %s", w.Code, w.Body.String())
	}
}

// TestJWTAuthMiddleware_AllowsTokenIssuedAfterRevocation 吊销之后重新登录拿到的令牌不受影响。
func TestJWTAuthMiddleware_AllowsTokenIssuedAfterRevocation(t *testing.T) {
	prev := IsTestMode
	IsTestMode = false
	defer func() { IsTestMode = prev }()

	r := newProtectedRouter()
	const uid = uint(910002)
	cutoff := time.Now().Unix()
	markRevoked(t, uid, cutoff)

	fresh := signTokenWithIat(t, uid, "user", time.Unix(cutoff+5, 0))
	if w := doAuthed(r, fresh); w.Code != http.StatusOK {
		t.Fatalf("吊销后新签发的令牌应 200，实际 %d %s", w.Code, w.Body.String())
	}
}

// TestJWTAuthMiddleware_BlocksBlacklistedSingleToken 登出（单令牌黑名单）路径回归。
func TestJWTAuthMiddleware_BlocksBlacklistedSingleToken(t *testing.T) {
	prev := IsTestMode
	IsTestMode = false
	defer func() { IsTestMode = prev }()

	r := newProtectedRouter()
	const uid = uint(910003)
	token := signTokenWithIat(t, uid, "admin", time.Now())

	if w := doAuthed(r, token); w.Code != http.StatusOK {
		t.Fatalf("前置条件失败：未登出时应 200，实际 %d", w.Code)
	}
	utils.BlacklistJWT(token)
	if w := doAuthed(r, token); w.Code != http.StatusUnauthorized {
		t.Fatalf("登出后的令牌必须 401，实际 %d", w.Code)
	}
}

// TestOptionalAuthMiddleware_RevokedTokenDegradesToAnonymous
// 可选认证不得把已吊销/已登出令牌的身份写进上下文（退化为匿名，而不是放行成 admin）。
func TestOptionalAuthMiddleware_RevokedTokenDegradesToAnonymous(t *testing.T) {
	prev := IsTestMode
	IsTestMode = false
	defer func() { IsTestMode = prev }()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/opt", OptionalAuthMiddleware(), func(c *gin.Context) {
		uid, has := c.Get("user_id")
		role := c.GetString("role")
		c.JSON(http.StatusOK, gin.H{"has": has, "uid": uid, "role": role})
	})

	const uid = uint(910004)
	markRevoked(t, uid, time.Now().Unix())
	token := signTokenWithIat(t, uid, "admin", time.Now().Add(-time.Hour))

	req := httptest.NewRequest(http.MethodGet, "/opt", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("可选认证应退化为匿名而非报错，实际 %d", w.Code)
	}
	if body := w.Body.String(); body != `{"has":false,"role":"","uid":null}` {
		t.Fatalf("被吊销令牌不得写入身份，实际响应：%s", body)
	}
}
