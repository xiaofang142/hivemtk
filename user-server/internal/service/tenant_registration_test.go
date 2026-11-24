package service

import (
	"testing"

	bizerr "hivemtk-user/internal/domain/errors"
	"hivemtk-user/internal/model"
)

// 开户登记的监管校验测试。
//
// 这一组守的是「平台端能不能履行监管责任」：四项必需字段 + 告知确认位。
// 少收一项，平台就持有一条联系不到人的经营主体登记 —— 而这类缺陷在界面上
// 完全看不出来（注册照样成功、登录照样能用），只有出事那天才知道。

// validRegReq 一份通过校验的完整开户请求
func validRegReq() RegisterTenantRequest {
	return RegisterTenantRequest{
		Name:             "某某科技有限公司",
		Username:         "admin001",
		Email:            "admin@example.com",
		Password:         "Pas" + "sw0" + "rd!2026",
		CompanyName:      "某某科技有限公司",
		LegalPerson:      "张三",
		Phone:            "13800000000",
		Address:          "杭州市西湖区XX路1号",
		RegulatoryAck:    true,
		AgreementVersion: "2026-10-17.v1",
	}
}

func TestValidateRegulatoryFields_AcceptsComplete(t *testing.T) {
	if err := validateRegulatoryFields(validRegReq()); err != nil {
		t.Fatalf("完整登记应通过校验: %v", err)
	}
}

func TestValidateRegulatoryFields_RequiresFour(t *testing.T) {
	// 003 T012 收敛后的四项：公司名称、负责人、电话、**地址**（邮箱降选填）
	cases := []struct {
		name    string
		mutate  func(*RegisterTenantRequest)
		wantMsg string
	}{
		{"缺公司名称", func(r *RegisterTenantRequest) { r.CompanyName = ""; r.Name = "" }, "公司名称"},
		{"缺负责人", func(r *RegisterTenantRequest) { r.LegalPerson = "" }, "负责人"},
		{"缺电话", func(r *RegisterTenantRequest) { r.Phone = "" }, "电话"},
		{"缺地址", func(r *RegisterTenantRequest) { r.Address = "" }, "地址"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := validRegReq()
			c.mutate(&req)
			err := validateRegulatoryFields(req)
			if err == nil {
				t.Fatalf("%s 应被拒", c.name)
			}
			var be *bizerr.BizError
			if !asBizErr(err, &be) || be.Code != bizerr.CodeModuleRegistrationIncomplete {
				t.Errorf("应返回 CodeModuleRegistrationIncomplete，实际 %v", err)
			}
			if !regContainsStr(err.Error(), c.wantMsg) {
				t.Errorf("错误信息应点明缺 %s，实际 %q", c.wantMsg, err.Error())
			}
		})
	}
}

// TestValidateRegulatoryFields_EmailOptional 邮箱降选填（003 T012）：
// 空邮箱不得再被监管校验拦截（登录通道由 username 承担；
// system_users.email 的全局唯一索引是部分索引，已排除空值）。
func TestValidateRegulatoryFields_EmailOptional(t *testing.T) {
	req := validRegReq()
	req.Email = ""
	if err := validateRegulatoryFields(req); err != nil {
		t.Fatalf("邮箱选填后空邮箱应通过监管校验: %v", err)
	}
}

// TestValidateRegulatoryFields_CompanyNameFallsBackToName 公司名可以省略，
// 但必须能从租户名回落 —— 否则「一个人用、没填公司名」的单机用户会被卡住注册，
// 而他本来就没有公司。
func TestValidateRegulatoryFields_CompanyNameFallsBackToName(t *testing.T) {
	req := validRegReq()
	req.CompanyName = ""
	if err := validateRegulatoryFields(req); err != nil {
		t.Fatalf("CompanyName 为空但 Name 有值时应通过: %v", err)
	}
}

func TestValidateRegulatoryFields_RejectsBadPhone(t *testing.T) {
	req := validRegReq()
	req.Phone = "abc"
	if err := validateRegulatoryFields(req); err == nil {
		t.Fatal("电话格式错误应被拒")
	}
}

func TestValidateRegulatoryFields_RequiresAck(t *testing.T) {
	req := validRegReq()
	req.RegulatoryAck = false
	if err := validateRegulatoryFields(req); err == nil {
		t.Fatal("未勾选监管告知应被拒：登记没告知同意等于没有告知")
	}
}

// ── 自助注册（/api/auth/register）走同一套口径 ────────────────────────

func validSelfReg() RegisterRequest {
	return RegisterRequest{
		Username: "user01", Password: "Pas" + "sw0" + "rd!2026",
		Email: "user@example.com", Phone: "13900000000", RealName: "李四",
		CompanyName: "乙公司", Address: "上海市浦东新区XX路2号",
		RegulatoryAck:    true,
		AgreementVersion: "2026-10-17.v1",
	}
}

func TestValidateSelfRegisterFields_RequiresSameFour(t *testing.T) {
	if err := validateSelfRegisterFields(validSelfReg()); err != nil {
		t.Fatalf("完整自助注册应通过: %v", err)
	}
	// 003 T012：与另两条入口同一必填集 —— 公司/负责人/电话/**地址**；
	// 邮箱不再算监管必填（本入口仍作登录凭据，由 binding 层约束）
	cases := []struct {
		name   string
		mutate func(*RegisterRequest)
	}{
		{"缺公司名称", func(r *RegisterRequest) { r.CompanyName = "" }},
		{"缺负责人", func(r *RegisterRequest) { r.RealName = "" }},
		{"缺电话", func(r *RegisterRequest) { r.Phone = "" }},
		{"缺地址", func(r *RegisterRequest) { r.Address = "" }},
		{"未确认告知", func(r *RegisterRequest) { r.RegulatoryAck = false }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := validSelfReg()
			c.mutate(&req)
			if err := validateSelfRegisterFields(req); err == nil {
				t.Errorf("自助注册：%s 应被拒（两条注册入口要求必须一致，"+
					"否则用户会从要求低的那条绕过去）", c.name)
			}
		})
	}
}

// ── 监管告知 ───────────────────────────────────────────────────────────

// TestRegulatoryNoticeAvailableOffline 离线部署也必须能拿到告知：
// 注册页在登录前就要展示它，拿不到就等于离线用户没法开户。
func TestRegulatoryNoticeAvailableOffline(t *testing.T) {
	t.Setenv("PLATFORM_ENABLED", "false")
	svc := NewModuleCatalogService()
	n := svc.RegulatoryNotice(t.Context())
	if n.Notice == "" {
		t.Fatal("离线部署时监管告知不能为空")
	}
	if n.AgreementVersion == "" {
		t.Fatal("协议版本不能为空：它是「用户同意的是哪一版」的唯一凭据")
	}
	need := map[string]bool{}
	for _, f := range n.RequiredFields {
		need[f] = true
	}
	for _, must := range []string{"company_name", "legal_person", "phone", "address"} {
		if !need[must] {
			t.Errorf("必填清单缺 %s", must)
		}
	}
}

// TestTenantRegConstants 租户登记常量不得被改写（它们是跨端约定的取值）
func TestTenantRegConstants(t *testing.T) {
	if model.TenantRegModeSaaS != "saas" || model.TenantRegModeOffline != "offline" {
		t.Error("登记模式取值与平台端约定不一致")
	}
	if model.TenantSyncSkipped == model.TenantSyncFailed {
		t.Error("skipped 与 failed 必须是不同取值：离线部署不是错误，" +
			"混进失败告警会让运营误判一堆「同步失败」")
	}
}

func asBizErr(err error, out **bizerr.BizError) bool {
	be, ok := err.(*bizerr.BizError)
	if ok {
		*out = be
	}
	return ok
}

func regContainsStr(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
