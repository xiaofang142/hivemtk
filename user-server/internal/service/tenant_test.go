package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	bizerr "hivemtk-user/internal/domain/errors"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

func repositoryFilter(page, pageSize int, keyword string) repository.TenantFilter {
	return repository.TenantFilter{Page: page, PageSize: pageSize, Keyword: keyword}
}

// SaaS 开户与登录通道测试（specs/001-saas-multi-tenant T034）
//
// 覆盖：开户成功 / 参数错 4101 / 邮箱全局重 4103 / off 模式 4104 / 限流 4105 /
//       邮箱登录通道 / 用户名跨租户歧义 4108 / 租户停用登录受限 4107

func setupTenantTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database := testutil.NewTestDB(t, &model.Tenant{}, &model.SystemUser{},
		// 这里的用例要走完邮箱登录，而 MFA 状态查询 fail-closed：表不在就是拒登。
		&model.UserMFA{},
	)
	db.SetTestDB(database)
	return database
}

// withTenantMode 切换双形态并在用例结束后还原，避免污染同包其他用例
func withTenantMode(t *testing.T, m tenant.Mode) {
	t.Helper()
	prev := tenant.CurrentMode()
	tenant.SetMode(m)
	t.Cleanup(func() { tenant.SetMode(prev) })
}

func validRegisterReq() RegisterTenantRequest {
	// 监管登记四项（公司名/负责人/电话/**地址**）是开户必填 —— 见
	// validateRegulatoryFields。这里补齐是因为需求要求「租户开户必须收集」，
	// **不是**为了让测试变绿而放宽校验。邮箱已降选填（003 T012）。
	return RegisterTenantRequest{
		Name:     "某某科技",
		Username: "admin",
		Email:    "a@b.com",
		Password: "admin" + "12345",

		CompanyName:      "某某科技",
		LegalPerson:      "张三",
		Phone:            "13800000000",
		Address:          "杭州市西湖区XX路1号",
		RegulatoryAck:    true,
		AgreementVersion: "2026-10-17.v1",
	}
}

func mustBizCode(t *testing.T, err error) int {
	t.Helper()
	var be *bizerr.BizError
	if !errors.As(err, &be) {
		t.Fatalf("期望业务错误，实际: %v", err)
	}
	return be.Code
}

// TestTenantService_Register_Success 开户成功：租户 + 管理员齐活，签发带租户的令牌
func TestTenantService_Register_Success(t *testing.T) {
	withTenantMode(t, tenant.ModeOn)
	setupTenantTestDB(t)
	svc := NewTenantService()

	req := validRegisterReq()
	got, err := svc.RegisterTenant(context.Background(), req, "10.0.0.1", "TestUA/1.0")
	if err != nil {
		t.Fatalf("开户失败: %v", err)
	}
	if got.TenantID == 0 {
		t.Error("tenant_id 应非零")
	}
	if !strings.HasPrefix(got.TenantCode, "t") || len(got.TenantCode) != 7 {
		t.Errorf("租户短码形态异常: %q", got.TenantCode)
	}
	if got.Token == "" {
		t.Error("应直接签发登录令牌")
	}

	// 租户行落库且状态正常
	var tn model.Tenant
	if err := db.GetDB().First(&tn, got.TenantID).Error; err != nil {
		t.Fatalf("租户行未落库: %v", err)
	}
	if !model.TenantIsActive(&tn) || tn.Plan != "free" || tn.Name != req.Name {
		t.Errorf("租户行字段不符: %+v", tn)
	}
	// 003 T010：静默合规采集三列随开户落库（UA 存 sha256 hex）
	if tn.InitIP != "10.0.0.1" {
		t.Errorf("init_ip 应为 10.0.0.1，实际 %q", tn.InitIP)
	}
	if tn.InitUA == "" || len(tn.InitUA) != 64 {
		t.Errorf("init_ua 应为 sha256 hex（64 字符），实际 %q", tn.InitUA)
	}
	if tn.InitDeviceFingerprint == "" {
		t.Error("init_device_fingerprint 不应为空")
	}

	// 管理员行归属新租户、密码已哈希
	// 查询带新租户 ctx（on 模式 system_users 是租户表，callback fail-closed）
	adminCtx := tenant.WithTenantID(context.Background(), got.TenantID)
	var admin model.SystemUser
	if err := db.GetDB().WithContext(adminCtx).Where("tenant_id = ? AND username = ?", got.TenantID, req.Username).
		First(&admin).Error; err != nil {
		t.Fatalf("管理员行未落库: %v", err)
	}
	if admin.Role != model.SystemUserRoleAdmin {
		t.Errorf("开户管理员角色应为 admin，实为 %q", admin.Role)
	}
	if admin.Password == req.Password {
		t.Error("密码必须哈希存储")
	}

	// 令牌携带新租户归属
	claims, err := utils.NewJWTUtils(utils.DefaultJWTConfig).ParseToken(got.Token)
	if err != nil {
		t.Fatalf("解析令牌失败: %v", err)
	}
	if claims.TenantID != got.TenantID {
		t.Errorf("令牌租户应为 %d，实为 %d", got.TenantID, claims.TenantID)
	}
}

// TestTenantService_Register_ParamInvalid 参数非法 → 4101
func TestTenantService_Register_ParamInvalid(t *testing.T) {
	withTenantMode(t, tenant.ModeOn)
	setupTenantTestDB(t)
	svc := NewTenantService()

	cases := map[string]func(r *RegisterTenantRequest){
		"名称过短":    func(r *RegisterTenantRequest) { r.Name = "短" },
		"用户名字符非法": func(r *RegisterTenantRequest) { r.Username = "bad user!" },
		"用户名过短":   func(r *RegisterTenantRequest) { r.Username = "ab" },
		"邮箱格式错误":  func(r *RegisterTenantRequest) { r.Email = "not-an-email" },
		"密码过短":    func(r *RegisterTenantRequest) { r.Password = "short" },
		"密码超长":    func(r *RegisterTenantRequest) { r.Password = strings.Repeat("x", 65) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			req := validRegisterReq()
			mutate(&req)
			_, err := svc.RegisterTenant(context.Background(), req, "10.0.0.2", "TestUA/1.0")
			if code := mustBizCode(t, err); code != bizerr.CodeTenantParamInvalid {
				t.Errorf("期望 4101，实为 %d", code)
			}
		})
	}
}

// TestTenantService_Register_EmailTakenGlobally 邮箱跨租户全局重复 → 4103
func TestTenantService_Register_EmailTakenGlobally(t *testing.T) {
	withTenantMode(t, tenant.ModeOn)
	setupTenantTestDB(t)
	svc := NewTenantService()

	if _, err := svc.RegisterTenant(context.Background(), validRegisterReq(), "10.0.0.3", "TestUA/1.0"); err != nil {
		t.Fatalf("首次开户失败: %v", err)
	}
	second := validRegisterReq()
	second.Name = "另一家公司"
	second.Username = "admin2"
	_, err := svc.RegisterTenant(context.Background(), second, "10.0.0.3", "TestUA/1.0")
	if code := mustBizCode(t, err); code != bizerr.CodeTenantEmailTaken {
		t.Errorf("期望 4103（邮箱全局重），实为 %d", code)
	}
}

// TestTenantService_Register_OfflineModeClosed 离线模式注册关闭 → 4104
func TestTenantService_Register_OfflineModeClosed(t *testing.T) {
	withTenantMode(t, tenant.ModeOff)
	setupTenantTestDB(t)
	svc := NewTenantService()

	_, err := svc.RegisterTenant(context.Background(), validRegisterReq(), "10.0.0.4", "TestUA/1.0")
	if code := mustBizCode(t, err); code != bizerr.CodeTenantRegisterClosed {
		t.Errorf("期望 4104，实为 %d", code)
	}
}

// TestTenantService_Register_RateLimited 同源高频注册 → 4105
func TestTenantService_Register_RateLimited(t *testing.T) {
	withTenantMode(t, tenant.ModeOn)
	setupTenantTestDB(t)
	svc := NewTenantService()

	clientKey := "10.0.0.5"
	var gotLimited bool
	for i := 0; i < registerBurst+2; i++ {
		req := validRegisterReq()
		req.Email = fmt.Sprintf("dup%d@b.com", i)
		_, err := svc.RegisterTenant(context.Background(), req, clientKey, "TestUA/1.0")
		if err == nil {
			continue
		}
		if mustBizCode(t, err) == bizerr.CodeTenantRegisterRateLimited {
			gotLimited = true
			break
		}
	}
	if !gotLimited {
		t.Errorf("同源 %d 次开户应触发 4105 限流", registerBurst+2)
	}
}

// TestTenantService_LoginByEmail 邮箱登录通道 → 直接登录成功
func TestTenantService_LoginByEmail(t *testing.T) {
	withTenantMode(t, tenant.ModeOn)
	setupTenantTestDB(t)
	ts := NewTenantService()

	req := validRegisterReq()
	created, err := ts.RegisterTenant(context.Background(), req, "10.0.0.6", "TestUA/1.0")
	if err != nil {
		t.Fatalf("开户失败: %v", err)
	}

	as := NewAuthService()
	// 用户名留空，纯邮箱通道
	resp, err := as.Login(context.Background(), &LoginRequest{Email: req.Email, Password: req.Password})
	if err != nil {
		t.Fatalf("邮箱登录失败: %v", err)
	}
	if resp.User.ID == 0 {
		t.Error("登录响应应含用户信息")
	}
	if resp.TenantID != created.TenantID {
		t.Errorf("登录响应租户应为 %d，实为 %d", created.TenantID, resp.TenantID)
	}
	if resp.TenantCode != created.TenantCode {
		t.Errorf("登录响应短码应为 %s，实为 %s", created.TenantCode, resp.TenantCode)
	}
}

// TestTenantService_LoginAmbiguousUsername 用户名跨租户歧义 → 4108
func TestTenantService_LoginAmbiguousUsername(t *testing.T) {
	withTenantMode(t, tenant.ModeOn)
	setupTenantTestDB(t)
	ts := NewTenantService()

	// 两个租户、同一用户名（用户名唯一性是租户内语义）
	for i, mail := range []string{"first@b.com", "second@b.com"} {
		req := validRegisterReq()
		req.Email = mail
		req.Name = fmt.Sprintf("公司%d", i)
		if _, err := ts.RegisterTenant(context.Background(), req, fmt.Sprintf("10.0.0.%d", 10+i), "TestUA/1.0"); err != nil {
			t.Fatalf("开户失败(%s): %v", mail, err)
		}
	}

	as := NewAuthService()
	_, err := as.Login(context.Background(), &LoginRequest{Username: "admin", Password: "admin" + "12345"})
	if code := mustBizCode(t, err); code != bizerr.CodeTenantAmbiguousUsername {
		t.Errorf("期望 4108（用户名跨租户歧义），实为 %d", code)
	}
}

// TestTenantService_LoginSuspendedTenant 租户停用后登录受限 → 4107，数据保留
//
// 先开一个首租户再开被测租户：首租户＝平台运营方租户，不允许停用
// （防护与其回归见 service.UpdateTenantStatus / tenant_first_tenant_protection_test.go），
// 直接停用第一个租户会撞上那道防护，测的就不是「停用受限」而是「停用被拒」了。
func TestTenantService_LoginSuspendedTenant(t *testing.T) {
	withTenantMode(t, tenant.ModeOn)
	setupTenantTestDB(t)
	ts := NewTenantService()

	operatorReq := validRegisterReq()
	operatorReq.Email = "operator@b.com"
	operatorReq.Name = "平台运营方"
	if _, err := ts.RegisterTenant(context.Background(), operatorReq, "10.0.0.6", "TestUA/1.0"); err != nil {
		t.Fatalf("首租户（平台运营方）开户失败: %v", err)
	}

	req := validRegisterReq()
	created, err := ts.RegisterTenant(context.Background(), req, "10.0.0.7", "TestUA/1.0")
	if err != nil {
		t.Fatalf("开户失败: %v", err)
	}
	if err := ts.UpdateTenantStatus(context.Background(), created.TenantID, model.TenantStatusSuspended); err != nil {
		t.Fatalf("停用租户失败: %v", err)
	}

	// 停用语义：登录被拒
	as := NewAuthService()
	_, err = as.Login(context.Background(), &LoginRequest{Email: req.Email, Password: req.Password})
	if code := mustBizCode(t, err); code != bizerr.CodeTenantSuspended {
		t.Errorf("期望 4107（租户已停用），实为 %d", code)
	}

	// 数据保留：租户行与管理员行仍在
	var tn model.Tenant
	if err := db.GetDB().First(&tn, created.TenantID).Error; err != nil {
		t.Errorf("租户行应保留: %v", err)
	}
	// 查询带新租户 ctx（on 模式 system_users 租户表 fail-closed）
	suspendCtx := tenant.WithTenantID(context.Background(), created.TenantID)
	var users int64
	db.GetDB().WithContext(suspendCtx).Model(&model.SystemUser{}).Where("tenant_id = ?", created.TenantID).Count(&users)
	if users != 1 {
		t.Errorf("管理员数据应保留（1 条），实为 %d", users)
	}
}

// TestTenantService_GetTenantInfo 租户信息取自 ctx 而非请求参数
func TestTenantService_GetTenantInfo(t *testing.T) {
	withTenantMode(t, tenant.ModeOn)
	setupTenantTestDB(t)
	ts := NewTenantService()

	req := validRegisterReq()
	created, err := ts.RegisterTenant(context.Background(), req, "10.0.0.8", "TestUA/1.0")
	if err != nil {
		t.Fatalf("开户失败: %v", err)
	}
	ctx := tenant.WithTenantID(context.Background(), created.TenantID)
	info, err := ts.GetTenantInfo(ctx)
	if err != nil {
		t.Fatalf("取租户信息失败: %v", err)
	}
	if info.TenantID != created.TenantID || info.Name != req.Name || !model.IsValidTenantStatus(info.Status) {
		t.Errorf("租户信息不符: %+v", info)
	}
}

// TestTenantService_ListTenants 运营清单：分页 + 关键词 + 活跃度概览
func TestTenantService_ListTenants(t *testing.T) {
	withTenantMode(t, tenant.ModeOn)
	setupTenantTestDB(t)
	ts := NewTenantService()

	for i, mail := range []string{"one@b.com", "two@b.com", "three@b.com"} {
		req := validRegisterReq()
		req.Email = mail
		req.Name = fmt.Sprintf("公司%d", i)
		if _, err := ts.RegisterTenant(context.Background(), req, fmt.Sprintf("10.0.1.%d", i+1), "TestUA/1.0"); err != nil {
			t.Fatalf("开户失败: %v", err)
		}
	}

	rows, total, err := ts.ListTenants(context.Background(), repositoryFilter(1, 10, ""))
	if err != nil {
		t.Fatalf("列清单失败: %v", err)
	}
	if total != 3 {
		t.Errorf("期望 3 个租户，实为 %d", total)
	}
	for _, r := range rows {
		if r.Activity.MemberCount != 1 {
			t.Errorf("租户 %s 成员数应为 1，实为 %d", r.Code, r.Activity.MemberCount)
		}
	}

	// 分页：page_size=1 时 total 恒为全量，rows 只含当页（LIMIT/OFFSET 真生效）
	rows, total, err = ts.ListTenants(context.Background(), repositoryFilter(2, 1, ""))
	if err != nil {
		t.Fatalf("分页列清单失败: %v", err)
	}
	if total != 3 || len(rows) != 1 {
		t.Errorf("第 2 页应 total=3 rows=1，实为 total=%d rows=%d", total, len(rows))
	}
	// 越过末页：rows 为空但 total 不丢（前端靠 total 算页数）
	rows, total, err = ts.ListTenants(context.Background(), repositoryFilter(99, 1, ""))
	if err != nil {
		t.Fatalf("越界分页失败: %v", err)
	}
	if total != 3 || len(rows) != 0 {
		t.Errorf("越界页应 total=3 rows=0，实为 total=%d rows=%d", total, len(rows))
	}
	// 参数缺省（Page/PageSize=0）：仓储侧钳成有界默认，绝不整表兜底
	rows, total, err = ts.ListTenants(context.Background(), repositoryFilter(0, 0, ""))
	if err != nil {
		t.Fatalf("缺省分页参数失败: %v", err)
	}
	if total != 3 || len(rows) == 0 || len(rows) > 20 {
		t.Errorf("缺省参数应回有界首页，实为 total=%d rows=%d", total, len(rows))
	}

	// 关键词过滤
	rows, total, err = ts.ListTenants(context.Background(), repositoryFilter(1, 10, "公司1"))
	if err != nil {
		t.Fatalf("按关键词列清单失败: %v", err)
	}
	if total != 1 || len(rows) != 1 || rows[0].Name != "公司1" {
		t.Errorf("关键词过滤不符: total=%d rows=%+v", total, rows)
	}
}
