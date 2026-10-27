package utils

import "testing"

// TestMessageRecallCodes_AreDistinct409 撤回冲突的两条原因必须各有一个码。
//
// 修前控制器用 response.Error(c, 409, …)：HTTP 码会被 errorCodeFromHTTPCode
// 折成 DUPLICATE_ENTRY_3003（「重复的记录」），而撤回一条消息既没写重复键
// 也和"重复"无关 —— 按码分流的前端会把"渠道不支持"当成"重复提交"。
// 这里同时钉住"两条原因不同码"（修法不同：人工去平台删 vs 只能人工处理）。
func TestMessageRecallCodes_AreDistinct409(t *testing.T) {
	unsupported := GetErrorCodeConfig(ErrorCodeMessageRecallUnsupported)
	expired := GetErrorCodeConfig(ErrorCodeMessageRecallWindowExpired)

	if unsupported.HTTPCode != 409 || expired.HTTPCode != 409 {
		t.Fatalf("撤回冲突码必须落 409，实际 %d / %d", unsupported.HTTPCode, expired.HTTPCode)
	}
	if unsupported.Code == expired.Code {
		t.Fatalf("渠道不支持与窗口过期不能共用一个码：%s", unsupported.Code)
	}
	for _, code := range []ErrorCode{ErrorCodeMessageRecallUnsupported, ErrorCodeMessageRecallWindowExpired} {
		if code == ErrorCodeDuplicateEntry {
			t.Fatal("撤回冲突码被折成了 DUPLICATE_ENTRY_3003")
		}
		if GetErrorCodeConfig(code).Message == "" {
			t.Fatalf("%s 缺注册表文案（未命中注册表会回退 UNKNOWN_1000）", code)
		}
		if GetErrorCodeConfig(code).Code != code {
			t.Fatalf("%s 注册表自引用不一致：%s", code, GetErrorCodeConfig(code).Code)
		}
	}
}

// TestDomainConflictAndUpstreamCodes 覆盖 409/410/502 的专属码域。
//
// 这三档此前都被 HTTP 反查折成通用码：409 → DUPLICATE_ENTRY_3003（重复记录）、
// 410 → LEGACY_GONE_4100（墓碑路由）、502 → UNKNOWN_1000（未知错误）。
// 但"执行已结束/草稿已被确认"不是重复提交，"扫码登录下线"不是路由消失，
// "渠道凭据错"也不是未知错误——前端按码分流会全部走错分支。
func TestDomainConflictAndUpstreamCodes(t *testing.T) {
	cases := []struct {
		code ErrorCode
		http int
	}{
		{ErrorCodeWorkflowStateConflict, 409},
		{ErrorCodeOrderDraftStateConflict, 409},
		{ErrorCodeWhatsappLoginRetired, 410},
		{ErrorCodeUpstreamFailure, 502},
	}
	seen := map[ErrorCode]bool{}
	for _, tc := range cases {
		cfg := GetErrorCodeConfig(tc.code)
		if cfg.HTTPCode != tc.http {
			t.Fatalf("%s 应落 %d，实际 %d", tc.code, tc.http, cfg.HTTPCode)
		}
		if cfg.Code != tc.code || cfg.Message == "" {
			t.Fatalf("%s 注册表条目不完整: %+v", tc.code, cfg)
		}
		if seen[tc.code] {
			t.Fatalf("重复码: %s", tc.code)
		}
		seen[tc.code] = true
		if tc.code == ErrorCodeDuplicateEntry || tc.code == ErrorCodeUnknown {
			t.Fatalf("%s 不得折回通用码", tc.code)
		}
	}
}
