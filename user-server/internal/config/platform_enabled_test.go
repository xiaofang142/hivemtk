package config

import (
	"context"
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
	withEmptyPlatformHostProvider(t)
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
	body := "api_url: \"\"\nsecret: \"\"\n"
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

	// 开态：必填校验必须生效。缺 secret 必须报错且点名该字段，报不点名等于校验形同虚设。
	//（admin_password 曾是必填项，2026-11-11 L-1 方案A 随 JWT 代理通道删除而下线。）
	//
	// api_url 不在这份必填清单里：地址可以来自参数中心 platform.api_host
	// （管理端在线改），装配期 yaml 的 api_url 本来就允许为空。只判 yaml 会让
	// "在数据库里配地址"这条路径在启动期直接报错 —— 功能还没上线就先把门堵死。
	// 地址的必填性由「解析后的最终地址为空」来判（见下面的 TestLoadPlatformAddressRequired）。
	t.Setenv("PLATFORM_ENABLED", "true")
	text := "api_url: http://127.0.0.1:8205\nadmin_username: admin\n"
	if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	PlatformCfg = nil
	err := LoadPlatform(p)
	if err == nil {
		t.Fatal("开启态缺 secret 必须报错")
	}
	if !strings.Contains(err.Error(), "secret") {
		t.Fatalf("缺 secret 时报错必须点名该字段，实际=%v", err)
	}
	if PlatformCfg != nil {
		t.Fatal("开启态校验失败时不得装配半截 PlatformCfg（缺 secret）")
	}
}

// withEmptyPlatformHostProvider 把平台地址 provider 置为「参数中心无值」。
//
// 为什么不能靠 SetPlatformHostProvider(nil) 复位：nil 按设计是空操作
// （防装配顺序把兜底顶掉），于是装过真 provider 的用例会跨用例泄漏 ——
// 另一条用例的「yaml 地址应生效」读到的就是上一条装上的参数值。
// 与 internal/config/url_params_test.go 的 withEmptyURLProviders 同一范式。
func withEmptyPlatformHostProvider(t *testing.T) {
	t.Helper()
	empty := func(context.Context) string { return "" }
	SetPlatformHostProvider(empty)
	t.Cleanup(func() { SetPlatformHostProvider(empty) })
}

// TestLoadPlatformAddressRequired 钉住「地址」的必填判定落在**最终解析结果**上。
//
// 三条腿：
//  1. yaml 的 api_url 空 + 环境变量空 + 参数中心空 ⇒ 报错（没有任何来源）
//  2. yaml 的 api_url 空但参数中心有值 ⇒ **不报错**（这正是本次落库的目的）
//  3. yaml 的 api_url 有值 ⇒ 不报错
func TestLoadPlatformAddressRequired(t *testing.T) {
	withEmptyPlatformHostProvider(t)
	t.Cleanup(func() { PlatformCfg = nil })

	p := filepath.Join(t.TempDir(), "platform.yaml")
	if err := os.WriteFile(p, []byte("secret: s3cr3t\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PLATFORM_CONFIG_PATH", p)
	t.Setenv("PLATFORM_API_HOST", "")
	t.Setenv("PLATFORM_API_URL", "")
	t.Setenv("PLATFORM_ENABLED", "true")

	// 1. 三个来源全空 ⇒ 报错，且报错要说清去哪里配
	PlatformCfg = nil
	SetPlatformHostProvider(func(ctx context.Context) string { return "" })
	err := LoadPlatform(p)
	if err == nil {
		t.Fatal("开启态且三处来源全空时必须报错（地址是出站目标，不能留空）")
	}
	if !strings.Contains(err.Error(), "platform.api_host") {
		t.Fatalf("报错必须点名参数中心的键（运维要在管理端改它），实际=%v", err)
	}

	// 2. yaml 空但参数中心有值 ⇒ 不报错
	PlatformCfg = nil
	SetPlatformHostProvider(func(ctx context.Context) string { return "https://new-platform.example.com" })
	if err := LoadPlatform(p); err != nil {
		t.Fatalf("参数中心配了地址时不得报错（这正是落库的目的），实际=%v", err)
	}

	// 3. yaml 有值 ⇒ 不报错
	if err := os.WriteFile(p, []byte("api_url: http://127.0.0.1:8205\nsecret: s3cr3t\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	PlatformCfg = nil
	SetPlatformHostProvider(func(ctx context.Context) string { return "" })
	if err := LoadPlatform(p); err != nil {
		t.Fatalf("yaml 配了地址时不得报错，实际=%v", err)
	}
}

// TestPlatformURLPriority 钉住地址解析的优先级，尤其是「参数中心压过 env」这条
// 与 urls.* 组方向相反的规则。
//
// 为什么这条必须钉死：参数中心优先是**反直觉**的（urls.* 那几条都是 env 优先），
// 而它恰恰是本次改造的核心 —— 用户改地址必须能压住部署时留在 .env 里的旧值。
// 一旦有人"顺手改成和 urls.* 一致"，管理端改地址就会静默不生效，
// 而页面上回的是「保存成功」。
func TestPlatformURLPriority(t *testing.T) {
	orig := PlatformCfg
	withEmptyPlatformHostProvider(t)
	t.Cleanup(func() { PlatformCfg = orig })
	PlatformCfg = nil

	t.Setenv("PLATFORM_ENABLED", "true")
	t.Setenv("PLATFORM_API_HOST", "http://env.example.com")
	t.Setenv("PLATFORM_API_URL", "")

	// 参数中心 > env
	SetPlatformHostProvider(func(ctx context.Context) string { return "https://param.example.com" })
	if got := PlatformURL(); got != "https://param.example.com" {
		t.Fatalf("参数中心应压过 env，实际=%q", got)
	}

	// 参数中心空 ⇒ 回落 env
	SetPlatformHostProvider(func(ctx context.Context) string { return "" })
	if got := PlatformURL(); got != "http://env.example.com" {
		t.Fatalf("参数中心为空时应回落 env，实际=%q", got)
	}

	// 参数中心空 + env 空 ⇒ 回落已装配的 yaml
	PlatformCfg = &PlatformConfig{APIURL: "http://yaml.example.com"}
	if got := PlatformURL(); got != "http://yaml.example.com" {
		t.Fatalf("参数中心与 env 都空时应回落 yaml，实际=%q", got)
	}

	// 关态：即使参数中心有值也必须恒空（"没接平台"是合法值）
	t.Setenv("PLATFORM_ENABLED", "false")
	SetPlatformHostProvider(func(ctx context.Context) string { return "https://param.example.com" })
	if got := PlatformURL(); got != "" {
		t.Fatalf("关态必须屏蔽参数中心里的地址，实际=%q", got)
	}
}

// TestPlatformURLForIgnoresTenantScope 钉住「平台地址不按租户分叉」这条语义。
//
// 同一个进程里两个租户拿到同一个地址是**正确**的：平台地址是部署级事实。
// 这个测试把该语义写死，防止将来有人"顺手"给 PlatformURLFor 加租户维度。
func TestPlatformURLForIgnoresTenantScope(t *testing.T) {
	orig := PlatformCfg
	withEmptyPlatformHostProvider(t)
	t.Cleanup(func() { PlatformCfg = orig })
	PlatformCfg = nil

	t.Setenv("PLATFORM_ENABLED", "true")
	t.Setenv("PLATFORM_API_HOST", "")
	t.Setenv("PLATFORM_API_URL", "")
	SetPlatformHostProvider(func(ctx context.Context) string { return "https://param.example.com" })

	// 带不同租户绑定的 ctx 读到同一个地址
	ctxA := context.Background()
	ctxB := context.WithValue(ctxA, struct{}{}, "other")
	if got := PlatformURLFor(ctxA); got != PlatformURLFor(ctxB) {
		t.Fatalf("平台地址不应随 ctx 变化：%q vs %q", PlatformURLFor(ctxA), PlatformURLFor(ctxB))
	}
}

// TestLoadPlatformEnabledLoadsFully 保证开关没有把正常路径一起关掉。
func TestLoadPlatformEnabledLoadsFully(t *testing.T) {
	orig := PlatformCfg
	withEmptyPlatformHostProvider(t)
	t.Cleanup(func() { PlatformCfg = orig })
	PlatformCfg = nil

	p := filepath.Join(t.TempDir(), "platform.yaml")
	body := "api_url: http://127.0.0.1:8205\nsecret: s3cr3t\nadmin_username: admin\nlog_report_interval: 30\nlicense_sync_interval: 60\n"
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
