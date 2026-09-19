package service

import (
	"context"
	"strconv"
	"testing"
	"time"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// 第十八轮（2026-09-19）：吊销**接线点**回归。
//
// utils.RevokeUserTokens 本身有 jwt_revocation_test 覆盖，中间件拦截有
// jwt_revocation_test（middleware 包）覆盖，但"哪些安全事件必须触发吊销"这层接线
// 此前完全无测试——一旦有人把某个调用点删掉/挪进不可达分支，整套吊销机制会静默失效：
// 被禁用/被删除/被改密的账号仍能拿着旧令牌继续访问到自然过期（≤24h）。
// 本文件把每个接线点钉死：事件发生 ⇒ 水位线必须被推到"现在"。
//
// 判定方式：先把该用户的水位线预置成一个远古值（1000000），事件发生后要求
// 水位线被覆盖为 ≥ 用例开始时间。这样即使同秒内连续多次吊销也能正确判定，
// 且 mutation（删掉 RevokeUserTokens 调用）必然让水位线停留在 1000000 ⇒ 必红。

const staleWatermark int64 = 1_000_000

func revokeKeyOf(userID uint) string {
	return "jwt:revoked_before:" + strconv.FormatUint(uint64(userID), 10)
}

// seedStaleWatermark 预置远古水位线，用于证明"水位线前移"确实由被测事件触发。
func seedStaleWatermark(t *testing.T, userID uint) {
	t.Helper()
	if err := cache.GetGlobalCache().Set(context.Background(), revokeKeyOf(userID),
		strconv.FormatInt(staleWatermark, 10), time.Hour); err != nil {
		t.Fatalf("seed watermark: %v", err)
	}
}

// assertWatermarkAdvanced 水位线必须存在且严格大于预置的远古值。
func assertWatermarkAdvanced(t *testing.T, userID uint, start int64) {
	t.Helper()
	val, err := cache.GetGlobalCache().Get(context.Background(), revokeKeyOf(userID))
	if err != nil {
		t.Fatalf("读取水位线失败（说明该安全事件根本没有吊销令牌）：%v", err)
	}
	cutoff, convErr := strconv.ParseInt(val, 10, 64)
	if convErr != nil {
		t.Fatalf("水位线值非法：%q", val)
	}
	if cutoff <= staleWatermark {
		t.Fatalf("水位线仍停留在预置值 %d，事件未触发吊销（期望 > %d）", cutoff, staleWatermark)
	}
	if cutoff < start {
		t.Fatalf("水位线 %d 早于用例开始时间 %d，吊销写入的不是当前时间", cutoff, start)
	}
}

// assertNoRevocation 反向断言：不该吊销的路径绝不能写水位线。
func assertNoRevocation(t *testing.T, userID uint) {
	t.Helper()
	val, err := cache.GetGlobalCache().Get(context.Background(), revokeKeyOf(userID))
	if err != nil {
		return // key 不存在 ⇒ 未吊销，符合预期
	}
	if val == strconv.FormatInt(staleWatermark, 10) {
		return // 仍是预置值 ⇒ 未吊销，符合预期
	}
	t.Fatalf("该路径不应吊销令牌，但水位线已被改写为 %q", val)
}

// fakeRevocationRepo 只实现本组用例涉及的方法，其余方法走内嵌接口（未实现即 panic）。
type fakeRevocationRepo struct {
	repository.SystemUserRepository
	users       map[uint]*model.SystemUser
	deleteErr   error
	setEnabledV *bool
	password    string // UpdatePassword 落库值
}

func (f *fakeRevocationRepo) GetByID(_ context.Context, id uint) (*model.SystemUser, error) {
	if u, ok := f.users[id]; ok {
		return u, nil
	}
	return nil, gorm.ErrRecordNotFound
}

func (f *fakeRevocationRepo) Update(_ context.Context, user *model.SystemUser) error {
	f.users[user.ID] = user
	return nil
}

func (f *fakeRevocationRepo) Delete(_ context.Context, id uint) error { return nil }

func (f *fakeRevocationRepo) DeleteSafe(_ context.Context, id uint) error { return f.deleteErr }

func (f *fakeRevocationRepo) SetEnabled(_ context.Context, id uint, enabled bool) error {
	f.setEnabledV = &enabled
	return nil
}

func (f *fakeRevocationRepo) CountEnabledAdmins(_ context.Context) (int64, error) { return 5, nil }

func (f *fakeRevocationRepo) UpdatePassword(_ context.Context, _ uint, hashed string) error {
	f.password = hashed
	return nil
}

func newFakeRevocationRepo(users ...*model.SystemUser) *fakeRevocationRepo {
	m := make(map[uint]*model.SystemUser, len(users))
	for _, u := range users {
		m[u.ID] = u
	}
	return &fakeRevocationRepo{users: m}
}

func plainUser(id uint, role string) *model.SystemUser {
	return &model.SystemUser{
		ID: id, Username: "rev" + strconv.FormatUint(uint64(id), 10),
		Password: "x", Email: "rev" + strconv.FormatUint(uint64(id), 10) + "@example.com",
		Role: role, Status: 1, Enabled: true, DataScope: "self",
	}
}

// TestSystemUserService_UpdateUser_RevokesOnPrivilegeChange 改角色 / 改状态必须吊销；
// 只改联系方式不得吊销（否则管理员每次补个手机号就把全公司踢下线）。
func TestSystemUserService_UpdateUser_RevokesOnPrivilegeChange(t *testing.T) {
	const uid = uint(930101)
	start := time.Now().Unix()

	svc := NewSystemUserServiceWithRepo(newFakeRevocationRepo(plainUser(uid, model.SystemUserRoleUser)))
	seedStaleWatermark(t, uid)

	if _, err := svc.UpdateUser(context.Background(), uid, &UpdateUserRequest{Phone: "13800000000"}); err != nil {
		t.Fatalf("UpdateUser(仅改电话) 失败: %v", err)
	}
	assertNoRevocation(t, uid)

	if _, err := svc.UpdateUser(context.Background(), uid, &UpdateUserRequest{Role: model.SystemUserRoleAdmin}); err != nil {
		t.Fatalf("UpdateUser(改角色) 失败: %v", err)
	}
	assertWatermarkAdvanced(t, uid, start)

	svc2 := NewSystemUserServiceWithRepo(newFakeRevocationRepo(func() *model.SystemUser {
		u := plainUser(uid, model.SystemUserRoleUser)
		u.Status = 2 // 非 1 的既有状态，便于观察"状态变化"
		return u
	}()))
	seedStaleWatermark(t, uid)
	// 注：UpdateUserRequest.Status 的 0 是"不修改"哨兵（该 API 无法用于禁用），
	// 因此这里用 2→1 表达状态真实变化。
	if _, err := svc2.UpdateUser(context.Background(), uid, &UpdateUserRequest{Status: 1}); err != nil {
		t.Fatalf("UpdateUser(改状态) 失败: %v", err)
	}
	assertWatermarkAdvanced(t, uid, start)

	svc3 := NewSystemUserServiceWithRepo(newFakeRevocationRepo(plainUser(uid, model.SystemUserRoleUser)))
	seedStaleWatermark(t, uid)
	// Status:0 = 未修改 ⇒ 不得吊销（同上，禁用语义走 SetEnabled）
	if _, err := svc3.UpdateUser(context.Background(), uid, &UpdateUserRequest{Status: 0}); err != nil {
		t.Fatalf("UpdateUser(Status=0) 失败: %v", err)
	}
	assertNoRevocation(t, uid)
}

// TestSystemUserService_DeleteUser_RevokesTokens 删除账号必须吊销该 id 的在途令牌。
func TestSystemUserService_DeleteUser_RevokesTokens(t *testing.T) {
	const uid = uint(930102)
	start := time.Now().Unix()

	svc := NewSystemUserServiceWithRepo(newFakeRevocationRepo(plainUser(uid, model.SystemUserRoleUser)))
	seedStaleWatermark(t, uid)

	if err := svc.DeleteUser(context.Background(), uid); err != nil {
		t.Fatalf("DeleteUser 失败: %v", err)
	}
	assertWatermarkAdvanced(t, uid, start)
}

// TestSystemUserService_DeleteByAdmin_RevokesTokens 管理员侧删除同样必须吊销；
// 目标不存在时不得吊销（避免用删除不存在的 id 去踢人）。
func TestSystemUserService_DeleteByAdmin_RevokesTokens(t *testing.T) {
	const uid = uint(930103)
	start := time.Now().Unix()

	svc := NewSystemUserServiceWithRepo(&fakeRevocationRepo{
		users:     map[uint]*model.SystemUser{},
		deleteErr: gorm.ErrRecordNotFound, // 仓储层未包裹时（当前 DeleteSafe 会包裹，见下注）
	})
	seedStaleWatermark(t, uid)
	if err := svc.DeleteByAdmin(context.Background(), 1, uid); err == nil {
		t.Fatalf("删除不存在的用户应报错")
	} else {
		// 观察项（第十八轮登记，非安全）：DeleteSafe 用 fmt.Errorf("query target user: %w")
		// 包裹了 not-found，故 DeleteByAdmin 的 ErrRecordNotFound 分支实际走不到，
		// 上层拿到的是通用错误而非"用户不存在"。仅影响提示文案，不影响吊销语义。
		t.Logf("not-found 路径返回：%v", err)
	}
	assertNoRevocation(t, uid)

	svc2 := NewSystemUserServiceWithRepo(newFakeRevocationRepo())
	seedStaleWatermark(t, uid)
	if err := svc2.DeleteByAdmin(context.Background(), 1, uid); err != nil {
		t.Fatalf("DeleteByAdmin 失败: %v", err)
	}
	assertWatermarkAdvanced(t, uid, start)
}

// TestAuthorizationService_SetEnabled_RevokesOnlyOnDisable 禁用吊销、启用不吊销。
func TestAuthorizationService_SetEnabled_RevokesOnlyOnDisable(t *testing.T) {
	setupAuthServiceTestDB(t) // SetEnabled 结尾写操作审计日志，需要全局测试库
	const uid = uint(930104)
	const actor = uint(930105)
	start := time.Now().Unix()

	svc := NewAuthorizationServiceWithRepo(newFakeRevocationRepo(plainUser(uid, model.SystemUserRoleUser)), nil)
	seedStaleWatermark(t, uid)
	if err := svc.SetEnabled(context.Background(), actor, uid, true); err != nil {
		t.Fatalf("SetEnabled(true) 失败: %v", err)
	}
	assertNoRevocation(t, uid)

	svc2 := NewAuthorizationServiceWithRepo(newFakeRevocationRepo(plainUser(uid, model.SystemUserRoleUser)), nil)
	seedStaleWatermark(t, uid)
	if err := svc2.SetEnabled(context.Background(), actor, uid, false); err != nil {
		t.Fatalf("SetEnabled(false) 失败: %v", err)
	}
	assertWatermarkAdvanced(t, uid, start)
}

// TestAuthorizationService_ResetPassword_RevokesTokens 管理员重置密码＝强制该账号重新登录。
func TestAuthorizationService_ResetPassword_RevokesTokens(t *testing.T) {
	setupAuthServiceTestDB(t) // 结尾写操作审计日志，需要全局测试库
	const uid = uint(930106)
	const actor = uint(930107)
	start := time.Now().Unix()

	svc := NewAuthorizationServiceWithRepo(newFakeRevocationRepo(plainUser(uid, model.SystemUserRoleUser)), nil)
	seedStaleWatermark(t, uid)
	if err := svc.ResetPassword(context.Background(), actor, uid, "N3wSecur3Pwd!"); err != nil {
		t.Fatalf("ResetPassword 失败: %v", err)
	}
	assertWatermarkAdvanced(t, uid, start)
}

// TestSystemUserService_ResetPassword_RevokesTokens 管理员直接重置密码＝该账号全部会话作废。
func TestSystemUserService_ResetPassword_RevokesTokens(t *testing.T) {
	database := setupSystemUserServiceTestDB(t)
	seedInitialAdmin(t, database)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	user := model.SystemUser{
		Username: "revadmin_" + suffix,
		Password: "oldpassword123",
		Email:    "revadmin_" + suffix + "@example.com",
		Role:     model.SystemUserRoleUser,
		Status:   1,
	}
	if err := database.Create(&user).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}

	start := time.Now().Add(-time.Second).Unix()
	svc := NewSystemUserService()
	seedStaleWatermark(t, user.ID)

	if err := svc.ResetPassword(context.Background(), user.ID, "Hv7mKp2LnQ"); err != nil {
		t.Fatalf("ResetPassword 失败: %v", err)
	}
	assertWatermarkAdvanced(t, user.ID, start)
}

// TestPasswordResetService_ResetPassword_RevokesTokens 邮件重置成功＝该账号可能已被攻击者
// 拿到邮箱访问权，旧会话（含攻击者手上那把令牌）必须一并作废。
func TestPasswordResetService_ResetPassword_RevokesTokens(t *testing.T) {
	database := setupSystemUserServiceTestDB(t)
	seedInitialAdmin(t, database)
	if err := database.AutoMigrate(&model.PasswordResetToken{}); err != nil {
		t.Fatalf("migrate reset token: %v", err)
	}
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	user := model.SystemUser{
		Username: "revmail_" + suffix,
		Password: "oldpassword123",
		Email:    "revmail_" + suffix + "@example.com",
		Role:     model.SystemUserRoleUser,
		Status:   1,
	}
	if err := database.Create(&user).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	tok := model.PasswordResetToken{
		UserID:    strconv.FormatUint(uint64(user.ID), 10),
		Token:     "reverse-test-token-" + suffix,
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := database.Create(&tok).Error; err != nil {
		t.Fatalf("seed token: %v", err)
	}

	start := time.Now().Add(-time.Second).Unix()
	svc := NewPasswordResetService(database)
	seedStaleWatermark(t, user.ID)

	if err := svc.ResetPassword(context.Background(), &ResetPasswordRequest{
		Token:       tok.Token,
		NewPassword: "N3wSecur3Pwd!",
	}); err != nil {
		t.Fatalf("ResetPassword 失败: %v", err)
	}
	assertWatermarkAdvanced(t, user.ID, start)
}

// TestAuthService_ChangePassword_RevokesTokens 用户自助改密成功后，
// 该账号其它设备上的旧令牌必须一并作废（密码泄露后的自救动作必须真正切断攻击者会话）。
func TestAuthService_ChangePassword_RevokesTokens(t *testing.T) {
	database := setupAuthServiceTestDB(t)
	seedInitialAdmin(t, database)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	user := &model.SystemUser{
		Username: "revchpwd_" + suffix,
		Password: "OldPass123!",
		Email:    "revchpwd_" + suffix + "@example.com",
		RealName: "Rev Change Pwd",
		Role:     model.SystemUserRoleUser,
		Status:   1,
		Enabled:  true,
	}
	if err := database.Create(user).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}

	start := time.Now().Add(-time.Second).Unix()
	svc := NewAuthService()
	seedStaleWatermark(t, user.ID)

	if err := svc.ChangePassword(context.Background(), user.ID, &ChangePasswordRequest{
		OldPassword: "OldPass123!",
		NewPassword: "N3wSecur3Pwd!",
	}); err != nil {
		t.Fatalf("ChangePassword 失败: %v", err)
	}
	assertWatermarkAdvanced(t, user.ID, start)
}
