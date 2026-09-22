package main

import (
	"net"
	"testing"
)

// TestDefaultPortDocsConsistency 验证 user-server 兜底端口常量与文档源一致。
//
// 文档源：
//   - user-server/docs/dev/DEVELOPMENT.md §2.4 端口对照表
//     | 8204 | user-server | Gin HTTP |
//     | 8203 | Redis       |
//   - user-server/cmd/api/main.go DefaultListenPort=8204
//   - user-web/bridge/src/core/constants.js DEFAULT_USER_SERVER.port=8204
//
// 调整请同步：
//   - DEVELOPMENT.md §2.4
//   - user-server/cmd/api/main.go DefaultListenPort
//   - user-web/bridge/src/core/constants.js DEFAULT_USER_SERVER.port
//   - user-web/bridge/docs/DEFAULTS.md §2.1
func TestDefaultPortDocsConsistency(t *testing.T) {
	t.Run("DefaultListenPort", func(t *testing.T) {
		if DefaultListenPort != "8204" {
			t.Errorf("DefaultListenPort 应为 8204（DEVELOPMENT.md §2.4 + main.go），实际 %s", DefaultListenPort)
		}
	})
	t.Run("DefaultRedisPort", func(t *testing.T) {
		if DefaultRedisPort != "8203" {
			t.Errorf("DefaultRedisPort 应为 8203（DEVELOPMENT.md §2.4），实际 %s", DefaultRedisPort)
		}
	})
	t.Run("NonSoftStartup", func(t *testing.T) {
		if DefaultListenPort == "" {
			t.Error("DefaultListenPort 不允许为空（禁软启动）")
		}
		if DefaultRedisPort == "" {
			t.Error("DefaultRedisPort 不允许为空（禁软启动）")
		}
	})
	t.Run("AlignWithBridge", func(t *testing.T) {
		if DefaultListenPort != "8204" {
			t.Error("DefaultListenPort 与 bridge DEFAULT_USER_SERVER.port(8204) 不一致")
		}
	})
}

// TestListenAddrResolution 锁住监听地址的组装口径。
//
// 加 SERVER_HOST 入口的起因：docs/DEPLOYMENT_GUIDE.md §6.2 的加固路线①写的是
// 「不碰口令，先把网口收回 127.0.0.1」，而实测 main.go 里地址是 "0.0.0.0:"+port
// 硬编码、全仓无任何 HOST/SERVER_HOST/LISTEN_HOST 读取点 ⇒ 那条路线当时无法执行。
// 本用例把两件事钉住：①入口存在且两半各管一半；②默认值仍是 0.0.0.0
// （加开关不许顺手改运行期行为；要收回本机必须显式设 SERVER_HOST）。
func TestListenAddrResolution(t *testing.T) {
	cases := []struct{ name, host, port, want string }{
		{"both_default", "", "", "0.0.0.0:8204"},
		{"port_only", "", "9999", "0.0.0.0:9999"},
		{"host_only", "127.0.0.1", "", "127.0.0.1:8204"},
		{"both_set", "127.0.0.1", "8204", "127.0.0.1:8204"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := resolveListenAddr(c.host, c.port); got != c.want {
				t.Errorf("resolveListenAddr(%q, %q) = %q，期望 %q", c.host, c.port, got, c.want)
			}
		})
	}
	t.Run("DefaultHostUnchanged", func(t *testing.T) {
		if DefaultListenHost != "0.0.0.0" {
			t.Errorf("默认监听主机必须是 0.0.0.0（收回本机只能靠显式设 SERVER_HOST），实际 %q", DefaultListenHost)
		}
	})
	// ActuallyBinds 证明的不只是字符串拼对，而是真 socket 真按那个主机落：
	// 端口传 0 让内核挑，再读回监听地址的主机半。地址被硬编码回 0.0.0.0 时第一腿会红。
	t.Run("ActuallyBinds", func(t *testing.T) {
		// 读数按"内核真绑到哪"判，不按入参字面比：net.Listen("tcp","0.0.0.0:0") 回读到的
		// 是 IPv4-mapped 形态的 "::"（本机实测），拿 "0.0.0.0" 去比会得到一条假红。
		// 覆盖面口径：default_is_any_interface 这一腿只证明"缺省没被收到本机"，
		// 它**杀不掉**"少写 host 兜底"那类变异（":0" 在内核侧同样是 unspecified），
		// 那一类由上面的精确串用例（both_default / port_only）负责。
		t.Run("loopback", func(t *testing.T) {
			ln, err := net.Listen("tcp", resolveListenAddr("127.0.0.1", "0"))
			if err != nil {
				t.Fatalf("net.Listen(SERVER_HOST=127.0.0.1) 失败：%v", err)
			}
			defer ln.Close()
			if got := ln.Addr().(*net.TCPAddr).IP.String(); got != "127.0.0.1" {
				t.Errorf("SERVER_HOST=127.0.0.1 时实际绑到 %s", got)
			}
		})
		t.Run("default_is_any_interface", func(t *testing.T) {
			ln, err := net.Listen("tcp", resolveListenAddr("", "0"))
			if err != nil {
				t.Fatalf("缺省监听启动失败：%v", err)
			}
			defer ln.Close()
			if ip := ln.Addr().(*net.TCPAddr).IP; !ip.IsUnspecified() {
				t.Errorf("不设 SERVER_HOST 必须仍绑全网卡（加开关不许顺手改运行期行为），实际 %s", ip)
			}
		})
	})
}
