package channelgw

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// CS-07 渠道支持等级公开收口（2026-11-07）：注册表事实与扩展侧/前端提示面逐项对照，
// 防止「注册即全支持」的过度宣传。三道闸：
//
//  1. Experimental 双侧同步：Go 注册表 ChannelSpec.Experimental ⇔ 扩展
//     EXPERIMENTAL_CHANNELS（user-web/bridge/src/core/constants.js，B8 注释承诺的
//     「同步维护」从此有测试钉住，单向漂移即红）。
//  2. 缺 adapter 夹具：Default 里每个注册渠道（personal_wx 除外——桌面伴生是外部
//     工程，扩展内刻意无适配器）必须有扩展适配器文件 user-web/bridge/src/channels/<name>.js；
//     谁在 Default 注册了新渠道却没带适配器，测试先红，禁止先宣称全支持。
//  3. 合成注册降级语义：运行时 Register 进来的渠道若标 Experimental，必须以
//     IsExperimental 如实暴露；未注册名不得命中；不得污染 Default。

// repoRootFromCurrentFile 从本测试文件位置向上回溯到仓库根（含 user-web 的那层）。
func repoRootFromCurrentFile(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("取工作目录失败: %v", err)
	}
	// channelgw 包位于 <root>/user-server/internal/channelgw，向上 3 级到 <root>。
	for i := 0; i < 3; i++ {
		dir = filepath.Dir(dir)
	}
	if _, err := os.Stat(filepath.Join(dir, "user-web", "bridge", "src", "core", "constants.js")); err != nil {
		t.Fatalf("仓库根定位失败（%s 下无 user-web/bridge）: %v", dir, err)
	}
	return dir
}

// TestExperimentalFlagsParityWithBridgeExtension 闸 1：Go 与扩展的实验集合必须一致。
// 口径：personal_wx 的适配器在外部伴生工程（扩展 content script 不会为它启动），
// 不参与扩展侧实验告警，属于 Go 单侧 Experimental 的合法例外；其余两侧必须逐词相等。
func TestExperimentalFlagsParityWithBridgeExtension(t *testing.T) {
	root := repoRootFromCurrentFile(t)
	raw, err := os.ReadFile(filepath.Join(root, "user-web", "bridge", "src", "core", "constants.js"))
	if err != nil {
		t.Fatalf("读取扩展常量失败: %v", err)
	}
	re := regexp.MustCompile(`EXPERIMENTAL_CHANNELS\s*=\s*Object\.freeze\(\{([^}]*)\}`)
	m := re.FindSubmatch(raw)
	if m == nil {
		t.Fatal("constants.js 中未找到 EXPERIMENTAL_CHANNELS 声明（结构变更请同步本测试）")
	}
	bridgeExperimental := map[string]bool{}
	for _, pair := range strings.Split(string(m[1]), ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		kv := strings.SplitN(pair, ":", 2)
		if len(kv) != 2 {
			t.Fatalf("EXPERIMENTAL_CHANNELS 出现非键值片段 %q", pair)
		}
		key := strings.TrimSpace(kv[0])
		val := strings.TrimSpace(kv[1])
		if val != "true" {
			continue
		}
		bridgeExperimental[key] = true
	}

	// 扩展不覆盖的 Go 单侧实验渠道：仅允许桌面伴生外部件。
	goSingleSidedExperimental := map[string]bool{"personal_wx": true}

	goExperimental := map[string]bool{}
	for _, name := range Default.Names() {
		if Default.IsExperimental(name) {
			goExperimental[name] = true
		}
	}

	for name := range goExperimental {
		if goSingleSidedExperimental[name] {
			if bridgeExperimental[name] {
				t.Errorf("渠道 %s 适配器在外部伴生工程，扩展 EXPERIMENTAL_CHANNELS 不应包含它（扩展不会为它启动 content script）", name)
			}
			continue
		}
		if !bridgeExperimental[name] {
			t.Errorf("渠道 %s 在 Go 注册表标 Experimental，但扩展 EXPERIMENTAL_CHANNELS 未同步（扩展侧会缺失实验告警）", name)
		}
	}
	for name := range bridgeExperimental {
		if !goExperimental[name] {
			t.Errorf("渠道 %s 在扩展标实验，但 Go 注册表未标 Experimental（服务端会按生产口径宣传）", name)
		}
	}
	if len(bridgeExperimental) == 0 {
		t.Fatal("扩展实验集合为空，解析逻辑或常量被误改")
	}
}

// TestDefaultRegisteredChannelsHaveBridgeAdapter 闸 2：注册 ≠ 全支持——缺适配器先红。
func TestDefaultRegisteredChannelsHaveBridgeAdapter(t *testing.T) {
	root := repoRootFromCurrentFile(t)
	// personal_wx 是唯一例外：服务端协议先行注册，适配器是外部桌面伴生（CH-11①），
	// 扩展 src/channels/ 里刻意没有 personal_wx.js——出现即说明有人把伴生塞进了扩展。
	companionExternal := map[string]bool{
		"personal_wx": true,
	}
	// canonical 渠道名 → 扩展适配器文件名（历史命名差异显式登记，避免掩盖真缺口）。
	adapterFile := map[string]string{
		"xiaohongshu": "xhs",
	}
	for _, name := range Default.Names() {
		base := name
		if adapterFile[name] != "" {
			base = adapterFile[name]
		}
		adapterPath := filepath.Join(root, "user-web", "bridge", "src", "channels", base+".js")
		_, err := os.Stat(adapterPath)
		if companionExternal[name] {
			if err == nil {
				t.Errorf("渠道 %s 的适配器应在外部伴生工程，扩展 src/channels/ 出现 %s.js 属口径违规（个人微信与扩展桥接必须分格）", name, base)
			}
			continue
		}
		if err != nil {
			t.Errorf("渠道 %s 已在 Default 注册但缺扩展适配器 %s（注册项不等于全渠道支持，缺适配器不得宣传收发可用）", name, adapterPath)
		}
	}
}

// TestSyntheticRegistrationDegradedNotPromoted 闸 3：合成注册的实验语义不被宣传成生产。
func TestSyntheticRegistrationDegradedNotPromoted(t *testing.T) {
	r := NewRegistry()
	r.Register(ChannelSpec{
		Name:         "synthetic_demo",
		Transports:   []Transport{TransportHTTP, TransportWebSocket},
		Label:        "合成渠道",
		Experimental: true,
	})
	if !r.IsChannel("synthetic_demo") {
		t.Fatal("合成渠道应已注册")
	}
	if !r.Supports("synthetic_demo", TransportHTTP) || !r.Supports("synthetic_demo", TransportWebSocket) {
		t.Error("合成渠道应支持声明的传输")
	}
	if !r.IsExperimental("synthetic_demo") {
		t.Error("合成实验渠道必须经 IsExperimental 如实暴露，禁止静默转正")
	}
	if Default.IsChannel("synthetic_demo") {
		t.Error("合成注册不得污染 Default 全局注册表")
	}
	if Default.IsExperimental("synthetic_demo") {
		t.Error("未注册进 Default 的名称不应命中 IsExperimental")
	}

	// 未标实验的合成注册走「正式」口径：不命中 IsExperimental（防止读取方把一切
	// 注册项默认当实验，也防止把正式渠道误标）。
	r.Register(ChannelSpec{Name: "synthetic_prod", Transports: []Transport{TransportHTTP}, Label: "合成正式"})
	if r.IsExperimental("synthetic_prod") {
		t.Error("未标 Experimental 的注册项不应命中 IsExperimental")
	}
}
