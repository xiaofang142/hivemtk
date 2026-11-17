package storage

// 文件 URL 签名（SaaS 审计 A1 / A12）。判据全部落在"活体输出"上：
// 形态（URL 里有没有租户段）、往返（签→验）、拒绝（篡改/过期/无密钥）。

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/pkg/tenant"
)

// withSigner 装配一个可控的签名器（ttl=0 ⇒ 不过期），用例结束后清回惰性装配态。
func withSigner(t *testing.T, ttl time.Duration) {
	t.Helper()
	if err := ConfigureFileURLSigner(testFileURLSecret, ttl); err != nil {
		t.Fatalf("装配签名器失败: %v", err)
	}
	t.Cleanup(func() { resetFileURLSignerForTest() })
}

func onMode(t *testing.T) {
	t.Helper()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })
}

// off 模式：不签名、URL 与历史逐字节一致（离线单机零回归）。
// 2026-11-17 审计 P3-2：off 模式同样签发（默认租户归属），URL 形状从
// /files/<path> 变为 /files/<path>?s=<token>；签名可回验且路径段逐字节一致。
func TestPublicURL_OffModeSigned(t *testing.T) {
	withSigner(t, 0)
	tenant.SetMode(tenant.ModeOff)

	d := NewLocalDriver("/tmp/uploads", "")
	for _, path := range []string{
		"materials/2026/10/a.pdf",
		"knowledge-base/20261023/legacy.pdf",
	} {
		got := d.PublicURL(path)
		wantPrefix := "/files/" + path + "?s="
		if !strings.HasPrefix(got, wantPrefix) {
			t.Errorf("off 模式 PublicURL(%q) = %q，期望前缀 %q", path, got, wantPrefix)
			continue
		}
		sig := got[len(wantPrefix):]
		tid, rel, err := VerifyFileURL("/"+path, sig, time.Now())
		if err != nil {
			t.Errorf("off 模式签名验不过: %v", err)
			continue
		}
		if rel != path || tid != tenant.DefaultTenantID {
			t.Errorf("签名回验不符: rel=%q tid=%d，期望 rel=%q tid=%d", rel, tid, path, tenant.DefaultTenantID)
		}
	}
}

// on 模式：URL 里没有租户段（A12），租户与路径都能从签名里原样取回。
func TestPublicURL_OnModeSignedWithoutTenantSegment(t *testing.T) {
	withSigner(t, 0)
	onMode(t)

	d := NewLocalDriver("/tmp/uploads", "")
	url := d.PublicURL("tenant_5/materials/2026/10/a.pdf")

	if !strings.HasPrefix(url, "/files/materials/2026/10/a.pdf?s=") {
		t.Fatalf("on 模式 PublicURL 形态不对：%q（期望 /files/<相对路径>?s=<token>）", url)
	}
	if strings.Contains(url, "tenant_5") || strings.Contains(url, "tenant_") {
		t.Fatalf("URL 里仍然看得到租户段：%q（A12 要求 URL 不含可枚举租户段）", url)
	}

	rel := strings.TrimPrefix(strings.SplitN(url, "?", 2)[0], "/files/")
	sig := strings.TrimPrefix(strings.SplitN(url, "?", 2)[1], "s=")
	tid, path, err := VerifyFileURL(rel, sig, time.Now())
	if err != nil {
		t.Fatalf("自签的 URL 验不过：%v", err)
	}
	if tid != 5 || path != "materials/2026/10/a.pdf" {
		t.Fatalf("签名解出 (tenant=%d, path=%q)，期望 (5, materials/2026/10/a.pdf)", tid, path)
	}
}

// 路径不带租户段（系统级上传、存量根域文件）⇒ 不签名，形态不变。
func TestPublicURL_NoTenantSegmentStaysPlain(t *testing.T) {
	withSigner(t, 0)
	onMode(t)

	d := NewLocalDriver("/tmp/uploads", "")
	for _, path := range []string{
		"materials/2026/10/a.pdf",
		"knowledge-base/20261023/legacy.pdf",
		"attachments/tenant_5/a.pdf", // 租户段不在首段：不是租户目录
	} {
		if got, want := d.PublicURL(path), "/files/"+path; got != want {
			t.Errorf("PublicURL(%q) = %q，期望 %q（首段不是租户目录就不签名）", path, got, want)
		}
	}
}

// 篡改：换路径段 / 改签名字节 / 换租户号，一律验不过。
func TestVerifyFileURL_RejectsTampering(t *testing.T) {
	withSigner(t, 0)
	onMode(t)

	_, sig, ok := SignStoredPath("tenant_5/materials/2026/10/a.pdf")
	if !ok {
		t.Fatal("签名没发生")
	}
	if _, _, err := VerifyFileURL("materials/2026/10/b.pdf", sig, time.Now()); !errors.Is(err, ErrFileURLInvalid) {
		t.Errorf("换掉路径段后应 ErrFileURLInvalid，实际 %v", err)
	}
	bad := []byte(sig)
	bad[len(bad)/2] ^= 0x01
	if _, _, err := VerifyFileURL("materials/2026/10/a.pdf", string(bad), time.Now()); !errors.Is(err, ErrFileURLInvalid) {
		t.Errorf("改一个签名字节后应 ErrFileURLInvalid，实际 %v", err)
	}
	if _, _, err := VerifyFileURL("materials/2026/10/a.pdf", "", time.Now()); !errors.Is(err, ErrFileURLInvalid) {
		t.Errorf("无签名应 ErrFileURLInvalid，实际 %v", err)
	}
	// 另一个部署秘密签出来的 token（换密钥 = 撤销），必须验不过
	if err := ConfigureFileURLSigner("another-deployment-secret-32+chars-long!!", 0); err != nil {
		t.Fatalf("换密钥失败: %v", err)
	}
	if _, _, err := VerifyFileURL("materials/2026/10/a.pdf", sig, time.Now()); !errors.Is(err, ErrFileURLInvalid) {
		t.Errorf("换密钥后旧 token 应失效（ErrFileURLInvalid），实际 %v", err)
	}
}

// 时效：ttl>0 时过期即拒，且判据是"现在"而不是"签署时"。
func TestVerifyFileURL_ExpiresWhenTTLSet(t *testing.T) {
	withSigner(t, time.Hour)
	onMode(t)

	_, sig, ok := SignStoredPath("tenant_5/materials/2026/10/a.pdf")
	if !ok {
		t.Fatal("签名没发生")
	}
	if _, _, err := VerifyFileURL("materials/2026/10/a.pdf", sig, time.Now()); err != nil {
		t.Fatalf("未过期时应通过，实际 %v", err)
	}
	if _, _, err := VerifyFileURL("materials/2026/10/a.pdf", sig, time.Now().Add(2*time.Hour)); !errors.Is(err, ErrFileURLExpired) {
		t.Errorf("两小时后应 ErrFileURLExpired，实际 %v", err)
	}
}

// 无密钥：签不出凭证。此时 on 模式的 /files 会因"验不过"而全 404（断链而非泄漏），
// 且 SignStoredPath 明确返回 ok=false 让 PublicURL 退回明文并有响亮报错。
func TestSignStoredPath_NoSigner(t *testing.T) {
	withSigner(t, 0)
	resetFileURLSignerForTest() // 回到"未装配 + 环境无密钥"的状态
	t.Setenv("FILE_URL_SECRET", "")
	t.Setenv("USER_JWT_SECRET", "")
	t.Setenv("JWT_SECRET", "")
	// 测试进程自带兜底密钥（testFileURLSecret），所以这里手工把签名器置空来模拟生产缺配置。
	fileURLMu.Lock()
	fileURLInitDone = true
	fileURLActive = nil
	fileURLMu.Unlock()
	onMode(t)

	if _, _, ok := SignStoredPath("tenant_5/materials/a.pdf"); ok {
		t.Fatal("没有密钥时不该签出 token")
	}
	d := NewLocalDriver("/tmp/uploads", "")
	if got, want := d.PublicURL("tenant_5/materials/a.pdf"), "/files/tenant_5/materials/a.pdf"; got != want {
		t.Errorf("无密钥时 PublicURL 应退回明文路径 %q，实际 %q", want, got)
	}
	if _, _, err := VerifyFileURL("materials/a.pdf", "anything", time.Now()); !errors.Is(err, ErrFileURLSignerUnavailable) {
		t.Errorf("无密钥时应 ErrFileURLSignerUnavailable，实际 %v", err)
	}
}

// splitTenantPath 的整段匹配（边界：tenant_51 不是 tenant_5 的后代、tenant_x 不是租户目录）。
func TestSplitTenantPath_SegmentMatching(t *testing.T) {
	cases := []struct {
		in     string
		tid    uint
		rest   string
		expect bool
	}{
		{"tenant_5/materials/a.pdf", 5, "materials/a.pdf", true},
		{"/tenant_5/materials/a.pdf", 5, "materials/a.pdf", true},
		{"tenant_51/materials/a.pdf", 51, "materials/a.pdf", true}, // 51 是另一个租户，不是 5 的后代
		{"tenant_x/materials/a.pdf", 0, "", false},
		{"tenant_0/materials/a.pdf", 0, "", false},
		{"tenant_5", 0, "", false},
		{"materials/a.pdf", 0, "", false},
		{"attachments/tenant_5/a.pdf", 0, "", false},
	}
	for _, tc := range cases {
		tid, rest, ok := splitTenantPath(tc.in)
		if ok != tc.expect || tid != tc.tid || rest != tc.rest {
			t.Errorf("splitTenantPath(%q) = (%d,%q,%v)，期望 (%d,%q,%v)",
				tc.in, tid, rest, ok, tc.tid, tc.rest, tc.expect)
		}
	}
}

var _ = context.Background
