package config

import (
	"os"
	"path/filepath"
	"regexp"
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

// TestHelmChartAlignsWithCodePorts 把 deploy/helm/hivemtk 那份 chart 拉进端口对账，
// 并检查 chart 自己的两个文件之间映射闭合。
//
// 为什么要读文件而不是比常量：2026-09-22 实测旧 chart 有两条"装上去就起不来"的错，
// 而它们都是**只在文本里**才看得见的那类——
//   - values.yaml 的 service.targetPort 与两个探针 port、deployment.yaml 的 containerPort
//     全写死 8080，可产码听的是 DefaultListenPort=8204，且本 chart 的 env 不给 PORT；
//   - env 段没有 MASTER_KEY / JWT_SECRET，二者缺失分别走 os.Exit(1)（cmd/api/main.go:183-189，
//     GIN_MODE=release 下 IsDevelopmentEnv() 为 false）与 panic（utils/jwt.go:63-72）。
//
// helm lint 抓不到这两条（它不读产码），staging 也没跑过 ⇒ 只能把对账钉在这里。
// 路径用 ../../../ 相对包目录（go test 的工作目录就是包目录），仓内先例见
// internal/app/email_runtime_wiring_test.go 一族。
func TestHelmChartAlignsWithCodePorts(t *testing.T) {
	chartDir := filepath.Join("..", "..", "..", "deploy", "helm", "hivemtk")
	readFile := func(name string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(chartDir, name))
		if err != nil {
			t.Fatalf("读 %s 失败：chart 若被移动/改名，请同步本测试的路径（%s）: %v", name, chartDir, err)
		}
		return string(b)
	}
	values := readFile("values.yaml")
	deployment := readFile(filepath.Join("templates", "deployment.yaml"))
	readme := readFile("README.md")

	t.Run("ServiceTargetPort", func(t *testing.T) {
		got := regexp.MustCompile(`(?m)^\s+targetPort:\s*(\d+)`).FindStringSubmatch(values)
		if got == nil {
			t.Fatal("values.yaml 里找不到 service.targetPort，对账失去对象")
		}
		if got[1] != DefaultListenPort {
			t.Errorf("chart 的 service.targetPort=%s 与 DefaultListenPort=%s 不一致："+
				"容器实际听的是产码那个口", got[1], DefaultListenPort)
		}
	})

	t.Run("ProbePorts", func(t *testing.T) {
		topKey := regexp.MustCompile(`^[a-zA-Z]`)
		portLine := regexp.MustCompile(`^\s+port:\s*(\d+)`)
		inProbe := false
		seen := 0
		for _, line := range strings.Split(values, "\n") {
			if topKey.MatchString(line) {
				inProbe = strings.HasPrefix(line, "livenessProbe:") || strings.HasPrefix(line, "readinessProbe:")
				continue
			}
			if !inProbe {
				continue
			}
			m := portLine.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			seen++
			if m[1] != DefaultListenPort {
				t.Errorf("探针 port=%s 不是 DefaultListenPort=%s ⇒ kubelet 探一个没人听的口，Pod 恒 NotReady",
					m[1], DefaultListenPort)
			}
		}
		if seen != 2 {
			t.Errorf("liveness/readiness 应各有一格 port，实测 %d 格（values.yaml 结构变了就同步本断言，别删）", seen)
		}
	})

	t.Run("ContainerPortNotHardcoded", func(t *testing.T) {
		if regexp.MustCompile(`containerPort:\s*\d+`).MatchString(deployment) {
			t.Error("templates/deployment.yaml 把 containerPort 写成了数字：同一个端口两处维护，" +
				"改 values.service.targetPort 时容器口不动 ⇒ 探针又哑。请写 {{ .Values.service.targetPort }}")
		}
		if !strings.Contains(deployment, "containerPort: {{ .Values.service.targetPort }}") {
			t.Error("templates/deployment.yaml 的 containerPort 不再从 values 取，对账失去意义")
		}
	})

	// 下面三条钉的是"env 与 Secret 名单两文件各写一半"这一类：
	// values.yaml 里每个 secretKeyRef.key 都得有人真的创建出来，README 少写一行
	// --from-literal 就是一个起不来的 Pod；反过来 MASTER_KEY/JWT_SECRET 一旦从 env 段
	// 掉出去，故障形态是 CrashLoopBackOff，跟 chart 看起来"配好了"完全对不上。
	t.Run("EnvCarriesFailFastSecrets", func(t *testing.T) {
		names := regexp.MustCompile(`(?m)^\s+-\s+name:\s+(\w+)\s*$`).FindAllStringSubmatch(values, -1)
		have := map[string]bool{}
		for _, m := range names {
			have[m[1]] = true
		}
		// 三条都是"不给就起不来"，缺一条就是一台 CrashLoopBackOff 的集群：
		// 变量名同样要逐字对上读取点，DB_PASSWORD 那种看着更像名字的写法在产码里零读取点。
		cases := []struct{ name, site string }{
			{"JWT_SECRET", "internal/pkg/utils/jwt.go:63-72 panic"},
			{"MASTER_KEY", "cmd/api/main.go:183-189 非 development 分支 os.Exit(1)"},
			{"POSTGRES_PASSWORD", "internal/pkg/db/db.go:38-44 panic（config.yaml 已不留 password 字段）"},
		}
		for _, c := range cases {
			if !have[c.name] {
				t.Errorf("values.yaml 的 env 段没有 %s：缺了进程直接退出（%s）", c.name, c.site)
			}
		}
		if have["DB_PASSWORD"] {
			t.Error("values.yaml 注入了 DB_PASSWORD：产码只读 POSTGRES_PASSWORD" +
				"（internal/pkg/db/db.go:38-44），这个旧名是 chart 骨架期的错，注入等于没注入")
		}
	})

	t.Run("SecretKeysCreatedInReadme", func(t *testing.T) {
		keys := regexp.MustCompile(`(?m)^\s+key:\s+([a-z0-9-]+)\s*$`).FindAllStringSubmatch(values, -1)
		if len(keys) == 0 {
			t.Fatal("values.yaml 里没有任何 secretKeyRef.key，对账失去对象")
		}
		for _, m := range keys {
			if !strings.Contains(readme, "--from-literal="+m[1]+"=") {
				t.Errorf("values.yaml 引用了 Secret key %q，但 README §快速开始 的 kubectl create secret "+
					"样例没创建它 ⇒ 照 README 装出来的 Pod 读不到这个值", m[1])
			}
		}
	})
}
