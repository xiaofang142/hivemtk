package service

import (
	"context"
	"strconv"
	"testing"
	"time"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/repository"

	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"
)

// 第十八轮（2026-09-19）：RefreshToken 生命周期回归。
//
// 旧实现＝"把旧令牌 claims 原样抄一遍 + 重新签名"，全程不回源，
// 于是被禁用/被删除/被降权的账号只要每 24h 刷一次就能**永久**保留原权限。
// 本文件锁死新语义：刷新必须回源、必须看账号状态、必须用库内当前权限重新签发。

// fakeLifecycleUserRepo 只实现本组用例需要的 GetByID，其余方法走内嵌接口（未实现即 panic，
// 一旦实现悄悄多调了别的方法，测试会炸出来而不是静默放过）。
type fakeLifecycleUserRepo struct {
	repository.SystemUserRepository
	users map[uint]*model.SystemUser
}

func (f *fakeLifecycleUserRepo) GetByID(_ context.Context, id uint) (*model.SystemUser, error) {
	if u, ok := f.users[id]; ok {
		return u, nil
	}
	return nil, gorm.ErrRecordNotFound
}

func newLifecycleAuthService(users map[uint]*model.SystemUser) *AuthService {
	return &AuthService{
		jwtUtils:       utils.NewJWTUtils(utils.DefaultJWTConfig),
		systemUserRepo: &fakeLifecycleUserRepo{users: users},
	}
}

func signTokenWithIat(t *testing.T, userID uint, username, role, dataScope string, iat time.Time) string {
	t.Helper()
	cfg := utils.DefaultJWTConfig
	claims := utils.CustomClaims{
		UserID:    userID,
		Username:  username,
		Role:      role,
		DataScope: dataScope,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(iat.Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(iat),
			Issuer:    cfg.Issuer,
		},
	}
	s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(cfg.SecretKey))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return s
}

func markRevokedCache(t *testing.T, userID uint, cutoff int64) {
	t.Helper()
	key := "jwt:revoked_before:" + strconv.FormatUint(uint64(userID), 10)
	if err := cache.GetGlobalCache().Set(context.Background(), key, strconv.FormatInt(cutoff, 10), time.Hour); err != nil {
		t.Fatalf("set marker: %v", err)
	}
}

func activeUser(id uint) *model.SystemUser {
	return &model.SystemUser{
		ID: id, Username: "u" + strconv.FormatUint(uint64(id), 10),
		Password: "x", Role: model.SystemUserRoleAdmin, Status: 1, Enabled: true,
		DataScope: "all",
	}
}

func TestRefreshToken_RejectsDisabledUser(t *testing.T) {
	const uid = uint(920001)
	u := activeUser(uid)
	u.Enabled = false
	svc := newLifecycleAuthService(map[uint]*model.SystemUser{uid: u})

	old := signTokenWithIat(t, uid, u.Username, "admin", "all", time.Now())
	if _, err := svc.RefreshToken(context.Background(), old); err == nil {
		t.Fatalf("被禁用账号的令牌不得续期")
	}
}

func TestRefreshToken_RejectsStatusDisabledUser(t *testing.T) {
	const uid = uint(920002)
	u := activeUser(uid)
	u.Status = 0
	svc := newLifecycleAuthService(map[uint]*model.SystemUser{uid: u})

	old := signTokenWithIat(t, uid, u.Username, "admin", "all", time.Now())
	if _, err := svc.RefreshToken(context.Background(), old); err == nil {
		t.Fatalf("Status!=1 的账号不得续期")
	}
}

func TestRefreshToken_RejectsDeletedUser(t *testing.T) {
	const uid = uint(920003)
	svc := newLifecycleAuthService(map[uint]*model.SystemUser{})

	old := signTokenWithIat(t, uid, "ghost", "admin", "all", time.Now())
	if _, err := svc.RefreshToken(context.Background(), old); err == nil {
		t.Fatalf("账号已删除时不得续期")
	}
}

// TestRefreshToken_ReissuesCurrentPrivileges 令牌里的权限必须来自库内当前值，
// 而不是旧令牌里那份（否则降级/改数据范围要等到自然过期才生效）。
func TestRefreshToken_ReissuesCurrentPrivileges(t *testing.T) {
	const uid = uint(920004)
	u := activeUser(uid)
	u.Role = model.SystemUserRoleUser
	u.DataScope = "dept"
	u.DepartmentID = 77
	svc := newLifecycleAuthService(map[uint]*model.SystemUser{uid: u})

	// 旧令牌仍是 admin/all（模拟"降权发生在令牌签发之后"）
	old := signTokenWithIat(t, uid, u.Username, "admin", "all", time.Now())
	newToken, err := svc.RefreshToken(context.Background(), old)
	if err != nil {
		t.Fatalf("合法刷新失败: %v", err)
	}
	claims, err := utils.NewJWTUtils(utils.DefaultJWTConfig).ParseToken(newToken)
	if err != nil {
		t.Fatalf("解析新令牌: %v", err)
	}
	if claims.Role != model.SystemUserRoleUser {
		t.Fatalf("新令牌角色应为库内当前值 %q，实际 %q", model.SystemUserRoleUser, claims.Role)
	}
	if claims.DataScope != "dept" {
		t.Fatalf("新令牌数据范围应为库内当前值 dept，实际 %q", claims.DataScope)
	}
	if claims.DepartmentID != 77 {
		t.Fatalf("新令牌部门应为库内当前值 77，实际 %d", claims.DepartmentID)
	}
}

func TestRefreshToken_RejectsRevokedToken(t *testing.T) {
	const uid = uint(920005)
	u := activeUser(uid)
	svc := newLifecycleAuthService(map[uint]*model.SystemUser{uid: u})

	old := signTokenWithIat(t, uid, u.Username, "admin", "all", time.Now().Add(-time.Hour))
	markRevokedCache(t, uid, time.Now().Unix())
	if _, err := svc.RefreshToken(context.Background(), old); err == nil {
		t.Fatalf("已吊销水位线之前的令牌不得续期")
	}
}

// TestRefreshToken_RotatesOnce 旧令牌刷新一次后立即作废（防止新旧两把并行可用）。
func TestRefreshToken_RotatesOnce(t *testing.T) {
	const uid = uint(920006)
	u := activeUser(uid)
	svc := newLifecycleAuthService(map[uint]*model.SystemUser{uid: u})

	old := signTokenWithIat(t, uid, u.Username, "admin", "all", time.Now())
	if _, err := svc.RefreshToken(context.Background(), old); err != nil {
		t.Fatalf("首次刷新应成功: %v", err)
	}
	if _, err := svc.RefreshToken(context.Background(), old); err == nil {
		t.Fatalf("已轮换掉的旧令牌不得再次刷新")
	}
}
