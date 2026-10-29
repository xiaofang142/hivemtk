package main

// installer_consistency_test.go —— 钉住两个平台安装器的**契约一致**（specs/015 C7）。
//
// Windows 安装器（install.ps1）无法在 macOS CI 上执行，但"同一份 conf 形状、
// 同一组参数、同一套卸载语义"是必须成立的契约。这里以文本判据锁住四条：
//  1. conf 键集合一致（缺一个 ⇒ 该平台 runner 起来就报"conf 缺 token"或读不到端口）；
//  2. 两个安装器都提供"只核对不安装"模式；
//  3. 自启注册项同名（cn.hivemtk.ego-runner），卸载器能按名摘掉；
//  4. 卸载都保留 conf（重装即复用），不删用户配置。

import (
	"os"
	"strings"
	"testing"
)

func readScript(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("../../scripts/ego-runner/" + name)
	if err != nil {
		t.Fatalf("读 %s 失败: %v", name, err)
	}
	return string(b)
}

func TestInstaller_ConfKeysMatch(t *testing.T) {
	sh := readScript(t, "install.sh")
	ps := readScript(t, "install.ps1")
	keys := []string{"server_url", "token", "device_id", "loopback_only", "ego_server_name"}
	for _, k := range keys {
		if !strings.Contains(sh, k) {
			t.Errorf("install.sh 未写 conf 键 %q", k)
		}
		if !strings.Contains(ps, k) {
			t.Errorf("install.ps1 未写 conf 键 %q", k)
		}
	}
}

func TestInstaller_CheckModeAndLabels(t *testing.T) {
	sh := readScript(t, "install.sh")
	ps := readScript(t, "install.ps1")
	if !strings.Contains(sh, "--check") || !strings.Contains(ps, "-Check") {
		t.Fatal("两个安装器都必须有只核对不安装的模式")
	}
	for _, name := range []string{"install.sh", "uninstall.sh", "install.ps1", "uninstall.ps1"} {
		if !strings.Contains(readScript(t, name), "cn.hivemtk.ego-runner") {
			t.Errorf("%s 未使用统一的自启标签 cn.hivemtk.ego-runner", name)
		}
	}
	// Windows 侧必须同时具备计划任务与 HKCU 回退（老系统/受限权限）
	if !strings.Contains(ps, "Register-ScheduledTask") || !strings.Contains(ps, "CurrentVersion\\Run") {
		t.Error("install.ps1 需要计划任务 + HKCU Run 双路径")
	}
	// 卸载保留 conf
	for _, name := range []string{"uninstall.sh", "uninstall.ps1"} {
		s := readScript(t, name)
		if !strings.Contains(s, "conf") {
			t.Errorf("%s 应说明 conf 保留", name)
		}
		if strings.Contains(s, "rm -f \"${CONF}\"") || strings.Contains(s, "Remove-Item -Force $Conf") {
			t.Errorf("%s 不得删除 conf", name)
		}
	}
}
