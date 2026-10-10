package controller

import (
	"testing"
	"time"
)

// TestSSOCookieTTLSeam SSO Cookie 有效期的接线三格。
func TestSSOCookieTTLSeam(t *testing.T) {
	restore := ssoCookieTTLProvider
	t.Cleanup(func() { ssoCookieTTLProvider = restore })

	if got, want := SSOCookieTTL, 5*time.Minute; got != want {
		t.Fatalf("兜底值 = %v，与参数中心 misc.sso_cookie_ttl 的 DefaultValue（300 秒 = %v）不一致", got, want)
	}

	SetSSOCookieTTLProvider(nil)
	if got := getSSOCookieTTL(); got != SSOCookieTTL {
		t.Fatalf("nil 注入时 = %v，期望回落 %v", got, SSOCookieTTL)
	}

	SetSSOCookieTTLProvider(func() time.Duration { return 30 * time.Minute })
	if got := getSSOCookieTTL(); got != 30*time.Minute {
		t.Fatalf("注入后 = %v，期望 30m", got)
	}
}

// TestBackupPageSizeCapSeam 备份列表单页上限的接线三格。
func TestBackupPageSizeCapSeam(t *testing.T) {
	restore := backupPageSizeCapProvider
	t.Cleanup(func() { backupPageSizeCapProvider = restore })

	if got, want := DefaultBackupPageSizeCap, 1000; got != want {
		t.Fatalf("兜底值 = %v，与参数中心 misc.backup_page_size 的 DefaultValue（%d）不一致", got, want)
	}

	SetBackupPageSizeCapProvider(nil)
	if got := backupPageSizeCap(); got != DefaultBackupPageSizeCap {
		t.Fatalf("nil 注入时 = %v，期望回落 %d", got, DefaultBackupPageSizeCap)
	}

	SetBackupPageSizeCapProvider(func() int { return 50 })
	if got := backupPageSizeCap(); got != 50 {
		t.Fatalf("注入后 = %v，期望 50", got)
	}
	SetBackupPageSizeCapProvider(func() int { return 0 })
	if got := backupPageSizeCap(); got != DefaultBackupPageSizeCap {
		t.Fatalf("注入 0 时 = %v，期望回落 %d", got, DefaultBackupPageSizeCap)
	}
}

// TestClampBackupPageSize 分页钳制的三格。
//
// 这里刻意把"默认每页 10 条"和"单页上限"分开验：种子里那条的默认值是 1000，
// 当成默认分页大小接的话，升级后备份列表会从每页 10 行静默变成每页 1000 行。
func TestClampBackupPageSize(t *testing.T) {
	restore := backupPageSizeCapProvider
	t.Cleanup(func() { backupPageSizeCapProvider = restore })

	if got := clampBackupPageSize(0); got != defaultBackupPageSize {
		t.Errorf("0 = %d，期望回落默认 %d", got, defaultBackupPageSize)
	}
	if got := clampBackupPageSize(-5); got != defaultBackupPageSize {
		t.Errorf("负数 = %d，期望回落默认 %d", got, defaultBackupPageSize)
	}
	if got := clampBackupPageSize(30); got != 30 {
		t.Errorf("上限之内不该动：%d", got)
	}
	if got := clampBackupPageSize(99999); got != DefaultBackupPageSizeCap {
		t.Errorf("超上限 = %d，期望截到 %d", got, DefaultBackupPageSizeCap)
	}

	SetBackupPageSizeCapProvider(func() int { return 20 })
	if got := clampBackupPageSize(99999); got != 20 {
		t.Errorf("上限改成 20 后超限应截到 20，实际 %d", got)
	}
	if got := clampBackupPageSize(20); got != 20 {
		t.Errorf("正好等于上限应放行，实际 %d", got)
	}
}

// TestAtoiOrDefault 脏入参不得回落 0：page_size=0 会让仓储查出零行，页面看着像"没有数据"。
func TestAtoiOrDefault(t *testing.T) {
	if got := atoiOrDefault("", 10); got != 10 {
		t.Errorf("空串 = %d，期望 10", got)
	}
	if got := atoiOrDefault("abc", 10); got != 10 {
		t.Errorf("脏字符 = %d，期望 10", got)
	}
	if got := atoiOrDefault("-3", 10); got != -3 {
		t.Errorf("负数应原样返回交给上层钳制，= %d", got)
	}
	if got := atoiOrDefault("25", 10); got != 25 {
		t.Errorf("合法值 = %d，期望 25", got)
	}
}
