package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 平台集成总开关的行为锁。
//
// 背景：hive/hiveuser 线上服务器 2026-09 到期不续费（见
// docs/superpowers/specs/2026-09-21-offline-deployment-design.md），平台端降级为
// 可选本地组件，默认关闭。关闭态下全链路依赖"config.PlatformCfg == nil 即快速失败"
// 这一既有缝，所以这里必须钉死两件事：①默认值是关；②关态绝不产出任何 URL
// （否则调用方会拿一个已下线的线上域去发请求）。

func TestPlatformEnabledDefaultsFalse(t *testing.T) {
	t.Setenv("PLATFORM_ENABLED", "")
	if PlatformEnabled() {
		t.Fatal("PLATFORM_ENABLED 未设置时应为 false（默认纯本地模式）")
	}
}

func TestPlatformEnabledAcceptsOnlyTrueLike(t *testing.T) {
	for _, v := range []string{"true", "TRUE", " True ", "1", "on", "yes", "YES"} {
		t.Setenv("PLATFORM_ENABLED", v)
		if !PlatformEnabled() {
			t.Fatalf("PLATFORM_ENABLED=%q 应判为开启", v)
		}
	}
	// 非真值一律按关处理（含 "yes" 之外的口语值与拼错的 tru）：
	// 开关判错方向必须是"关"，否则默认不续费的环境会意外出站。
	for _, v := range []string{"false", "FALSE", "0", "off", "", "tru", "ture", "no", "1x"} {
		t.Setenv("PLATFORM_ENABLED", v)
		if PlatformEnabled() {
			t.Fatalf("PLATFORM_ENABLED=%q 应判为关闭（非真值一律关）", v)
		}
	}
}

// TestPlatformURLNeverFallsBackToAnyDomain 是本批的防回流主判据：
// 无论开关处于哪个方向、无论配置文件里写了什么，只要没显式给出平台地址，
// PlatformURL() 就必须是空串——历史上这里回落过一个已到期不续费的线上域名。
func TestPlatformURLNeverFallsBackToAnyDomain(t *testing.T) {
	orig := PlatformCfg
	t.Cleanup(func() { PlatformCfg = orig })
	PlatformCfg = nil

	t.Setenv("PLATFORM_CONFIG_PATH", "")
	t.Setenv("PLATFORM_API_HOST", "")
	t.Setenv("PLATFORM_API_URL", "")

	t.Setenv("PLATFORM_ENABLED", "false")
	if got := PlatformURL(); got != "" {
		t.Fatalf("关闭态 PlatformURL 必须为空，实际=%q", got)
	}

	// 开启但没有显式地址：仍然必须为空。这一腿才是"删掉线上域常量"的真正哨兵
	// ——若有人把回落常量加回来，此断言立刻红。
	t.Setenv("PLATFORM_ENABLED", "true")
	if got := PlatformURL(); got != "" {
		t.Fatalf("开启但无显式地址时 PlatformURL 必须为空，实际=%q（疑似又回落了默认域名）", got)
	}

	// 缺文件不算错：开启态但 platform.yaml 不存在时，早于必填校验就返回错误，
	// PlatformCfg 保持 nil，PlatformURL() 仍应稳定为空。
	if err := LoadPlatform(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Fatal("开启态读不到配置文件必须报错")
	}
	if got := PlatformURL(); got != "" {
		t.Fatalf("加载失败后 PlatformURL 必须为空，实际=%q", got)
	}

	t.Setenv("PLATFORM_API_HOST", "http://127.0.0.1:8205")
	if got := PlatformURL(); got != "http://127.0.0.1:8205" {
		t.Fatalf("显式地址应原样生效，实际=%q", got)
	}

	// PlatformCfg 装配后优先于环境变量（与 LoadPlatform 的覆盖顺序一致）
	PlatformCfg = &PlatformConfig{APIURL: "http://127.0.0.1:9999"}
	if got := PlatformURL(); got != "http://127.0.0.1:9999" {
		t.Fatalf("已装配的 PlatformCfg.APIURL 应优先，实际=%q", got)
	}

	// 关掉之后即使 PlatformCfg 还挂着残留，也必须对外表现为"无地址"
	t.Setenv("PLATFORM_ENABLED", "false")
	if got := PlatformURL(); got != "" {
		t.Fatalf("关闭态必须屏蔽一切已装配地址，实际=%q", got)
	}
}

func TestLoadPlatformRequiredFieldsOnlyWhenEnabled(t *testing.T) {
	orig := PlatformCfg
	t.Cleanup(func() { PlatformCfg = orig })

	p := filepath.Join(t.TempDir(), "platform.yaml")
	body := "api_url: \"\"\nsecret: \"\"\nadmin_password: \"\"\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PLATFORM_CONFIG_PATH", p)
	t.Setenv("PLATFORM_API_HOST", "")
	t.Setenv("PLATFORM_API_URL", "")

	// 关态：字段全缺也不报错，且不得装配 PlatformCfg —— nil 就是全链路快速失败的开关本身
	t.Setenv("PLATFORM_ENABLED", "false")
	PlatformCfg = &PlatformConfig{APIURL: "sentinel"}
	if err := LoadPlatform(p); err != nil {
		t.Fatalf("关闭态缺字段不应报错，实际=%v", err)
	}
	if PlatformCfg != nil {
		t.Fatal("关闭态 LoadPlatform 必须把 PlatformCfg 置 nil（残留在外的开关会让出站路径继续跑）")
	}

	// 开态：三段必填校验必须逐个生效。缺一报缺哪一，报不全等于校验形同虚设。
	t.Setenv("PLATFORM_ENABLED", "true")
	for _, missing := range []string{"api_url", "secret", "admin_password"} {
		text := "api_url: http://127.0.0.1:8205\nsecret: s3cr3t\nadmin_username: admin\nadmin_password: pw\n"
		switch missing {
		case "api_url":
			text = "secret: s3cr3t\nadmin_password: pw\n"
		case "secret":
			text = "api_url: http://127.0.0.1:8205\nadmin_password: pw\n"
		case "admin_password":
			text = "api_url: http://127.0.0.1:8205\nsecret: s3cr3t\n"
		}
		if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		PlatformCfg = nil
		err := LoadPlatform(p)
		if err == nil {
			t.Fatalf("开启态缺 %s 必须报错", missing)
		}
		if !strings.Contains(err.Error(), missing) {
			t.Fatalf("缺 %s 时报错必须点名该字段，实际=%v", missing, err)
		}
		if PlatformCfg != nil {
			t.Fatalf("开启态校验失败时不得装配半截 PlatformCfg（缺 %s）", missing)
		}
	}
}

// TestLoadPlatformEnabledLoadsFully 保证开关没有把正常路径一起关掉。
func TestLoadPlatformEnabledLoadsFully(t *testing.T) {
	orig := PlatformCfg
	t.Cleanup(func() { PlatformCfg = orig })
	PlatformCfg = nil

	p := filepath.Join(t.TempDir(), "platform.yaml")
	body := "api_url: http://127.0.0.1:8205\nsecret: s3cr3t\nadmin_username: admin\nadmin_password: pw\nlog_report_interval: 30\nlicense_sync_interval: 60\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PLATFORM_CONFIG_PATH", p)
	t.Setenv("PLATFORM_API_HOST", "")
	t.Setenv("PLATFORM_API_URL", "")
	t.Setenv("PLATFORM_ENABLED", "true")

	if err := LoadPlatform(p); err != nil {
		t.Fatalf("开启态字段齐全应加载成功，实际=%v", err)
	}
	if PlatformCfg == nil || PlatformCfg.APIURL != "http://127.0.0.1:8205" {
		t.Fatalf("开启态应装配 PlatformCfg，实际=%+v", PlatformCfg)
	}
	if got := PlatformURL(); got != "http://127.0.0.1:8205" {
		t.Fatalf("PlatformURL 应取已装配地址，实际=%q", got)
	}
}
