package config

import (
	"strings"
	"testing"
)

// TestPortsConstants 验证 ports.go 中所有端口 / URL 常量：
//  1. 数值字面量与 DEVELOPMENT.md §2.4 端口对照表字面一致
//  2. 所有 URL 常量都派生自端口常量（不允许裸字面量）
//  3. 禁止空值（禁软启动）
//
// 文档源：
//   - user-server/docs/dev/DEVELOPMENT.md §2.4 端口对照表
//   - user-server/docs/dev/DEVELOPMENT.md §2.4 各应用启动描述
//   - user-web/bridge/src/core/constants.js DEFAULT_USER_SERVER.port
//   - user-server/cmd/api/main.go DefaultListenPort=8204
func TestPortsConstants(t *testing.T) {
	t.Run("PortValues", func(t *testing.T) {
		cases := []struct {
			name string
			got  string
			want string
			doc  string
		}{
			{"DefaultListenPort", DefaultListenPort, "8204", "DEVELOPMENT.md §2.4 | 8204 | user-server"},
			{"DefaultRedisPort", DefaultRedisPort, "8203", "DEVELOPMENT.md §2.4 | 8203 | Redis"},
			{"DefaultPlatformPort", DefaultPlatformPort, "8205", "DEVELOPMENT.md §2.4 | 8205 | platform-server"},
			{"DefaultChromiumCDPPort", DefaultChromiumCDPPort, "8206", "DEVELOPMENT.md §2.4 | 8206 | chromium CDP"},
		}
		for _, c := range cases {
			if c.got != c.want {
				t.Errorf("%s 应为 %s（%s），实际 %s", c.name, c.want, c.doc, c.got)
			}
		}
	})
	t.Run("NumericPortValues", func(t *testing.T) {
		cases := []struct {
			name string
			got  int
			want int
		}{
			{"DefaultDBPortDev", DefaultDBPortDev, 8232},
			{"DefaultDBPortDocker", DefaultDBPortDocker, 8202},
			{"DefaultLLMPort", DefaultLLMPort, 8207},
			{"DefaultEmbeddingPort", DefaultEmbeddingPort, 8208},
			{"DefaultRerankPort", DefaultRerankPort, 8209},
		}
		for _, c := range cases {
			if c.got != c.want {
				t.Errorf("%s 应为 %d，实际 %d", c.name, c.want, c.got)
			}
		}
	})
	t.Run("URLDerivedFromPort", func(t *testing.T) {
		cases := []struct {
			name   string
			url    string
			port   string
			prefix string
			suffix string
		}{
			{"DefaultUserServerBaseURL", DefaultUserServerBaseURL, DefaultListenPort, "http://localhost:", ""},
			{"DefaultPlatformBaseURL", DefaultPlatformBaseURL, DefaultPlatformPort, "http://localhost:", ""},
			{"DefaultRemoteDebugURL", DefaultRemoteDebugURL, DefaultChromiumCDPPort, "http://localhost:", ""},
		}
		for _, c := range cases {
			want := c.prefix + c.port + c.suffix
			if c.url != want {
				t.Errorf("%s 应为 %s（= %s + %s），实际 %s",
					c.name, want, c.prefix+c.port, c.suffix, c.url)
			}
		}
	})
	t.Run("InferenceBaseURLsContainPort", func(t *testing.T) {
		cases := []struct {
			name string
			url  string
			port string
		}{
			{"DefaultLLMBaseURLDev", DefaultLLMBaseURLDev, "8207"},
			{"DefaultEmbeddingBaseURLDev", DefaultEmbeddingBaseURLDev, "8208"},
			{"DefaultRerankBaseURLDev", DefaultRerankBaseURLDev, "8209"},
		}
		for _, c := range cases {
			if !strings.Contains(c.url, ":"+c.port+"/") {
				t.Errorf("%s 应包含端口 :%s/，实际 %s", c.name, c.port, c.url)
			}
		}
	})
	t.Run("DockerBaseURLsContainPort", func(t *testing.T) {
		cases := []struct {
			name string
			url  string
			port string
		}{
			{"DefaultLLMBaseURLDocker", DefaultLLMBaseURLDocker, "8207"},
			{"DefaultEmbeddingBaseURLDocker", DefaultEmbeddingBaseURLDocker, "8208"},
			{"DefaultRerankBaseURLDocker", DefaultRerankBaseURLDocker, "8209"},
		}
		for _, c := range cases {
			if !strings.Contains(c.url, ":"+c.port+"/") {
				t.Errorf("%s 应包含端口 :%s/，实际 %s", c.name, c.port, c.url)
			}
		}
	})
	t.Run("BGEBaseURLsDerivedFromEmbeddingPort", func(t *testing.T) {
		cases := []struct {
			name string
			url  string
		}{
			{"DefaultBGEBaseURLDev", DefaultBGEBaseURLDev},
			{"DefaultBGEBaseURLDocker", DefaultBGEBaseURLDocker},
		}
		for _, c := range cases {
			if !strings.Contains(c.url, ":8208/v1") {
				t.Errorf("%s 应包含 :8208/v1（与 embedding 端口对齐），实际 %s", c.name, c.url)
			}
		}
	})
	// 所有 URL 常量的单一清单：NonEmpty 与 NoRetiredOnlineDomain 两条判据共用，
	// 免得新增常量时只补一处、另一处静默漏检。
	constants := map[string]string{
		"DefaultListenPort":             DefaultListenPort,
		"DefaultRedisPort":              DefaultRedisPort,
		"DefaultPlatformPort":           DefaultPlatformPort,
		"DefaultChromiumCDPPort":        DefaultChromiumCDPPort,
		"DefaultUserServerBaseURL":      DefaultUserServerBaseURL,
		"DefaultPlatformBaseURL":        DefaultPlatformBaseURL,
		"DefaultWebsiteBaseURL":         DefaultWebsiteBaseURL,
		"DefaultRemoteDebugURL":         DefaultRemoteDebugURL,
		"DefaultLLMBaseURLDev":          DefaultLLMBaseURLDev,
		"DefaultEmbeddingBaseURLDev":    DefaultEmbeddingBaseURLDev,
		"DefaultRerankBaseURLDev":       DefaultRerankBaseURLDev,
		"DefaultLLMBaseURLDocker":       DefaultLLMBaseURLDocker,
		"DefaultEmbeddingBaseURLDocker": DefaultEmbeddingBaseURLDocker,
		"DefaultRerankBaseURLDocker":    DefaultRerankBaseURLDocker,
		"DefaultBGEBaseURLDev":          DefaultBGEBaseURLDev,
		"DefaultBGEBaseURLDocker":       DefaultBGEBaseURLDocker,
		"DefaultOllamaBaseURL":          DefaultOllamaBaseURL,
	}
	t.Run("NonEmpty", func(t *testing.T) {
		all := constants
		for name, v := range all {
			if v == "" {
				t.Errorf("%s 不允许为空（禁软启动）", name)
			}
		}
	})
	// 已到期不续费的线上域（5 个 hive\*.xapptool 域）不得再出现在任何默认常量里——
	// 那是"配置缺失时偷偷打公网"的唯一入口。仓内文案层面的清扫由
	// scripts/check-no-xapptool.sh 覆盖，这条只管运行期默认值。
	// retired 用拼接写：避免本文件被自己拦下（闸的白名单因此不用为测试开口子）。
	t.Run("NoRetiredOnlineDomain", func(t *testing.T) {
		const retired = "xapptool" + ".cn"
		for name, v := range constants {
			if strings.Contains(v, retired) {
				t.Errorf("%s 指向已下线线上域 %q（实际 %q）：默认值只能是 localhost / 容器名 / GitHub Pages", name, retired, v)
			}
		}
	})
}

// TestPortsConstants_AlignWithBridge 验证 user-server 端口与 bridge constants 隐式一致。
//
// 文档源：
//   - user-web/bridge/src/core/constants.js DEFAULT_USER_SERVER.port=8204
//   - user-web/bridge/src/core/constants.js DEFAULT_USER_SERVER.baseUrl="http://localhost:8204"
//
// 该测试以字符串字面量形式锁住两侧数字一致（无法跨包 import 桥接代码，
// 故采用「显式字面断言」防止文档漂移）。
//
// 调整时必须同步：bridge constants.js + DEVELOPMENT.md §2.4 + 本测试。
func TestPortsConstants_AlignWithBridge(t *testing.T) {
	t.Run("UserServerPort", func(t *testing.T) {
		if DefaultListenPort != "8204" {
			t.Errorf("DefaultListenPort 应为 8204（与 bridge DEFAULT_USER_SERVER.port 对齐），实际 %s", DefaultListenPort)
		}
	})
	t.Run("UserServerBaseURL", func(t *testing.T) {
		want := "http://localhost:8204"
		if DefaultUserServerBaseURL != want {
			t.Errorf("DefaultUserServerBaseURL 应为 %s（与 bridge DEFAULT_USER_SERVER.baseUrl 对齐），实际 %s",
				want, DefaultUserServerBaseURL)
		}
	})
}
