//go:build ignore
// +build ignore

// gen_env_reference.go - 机械生成 docs/ENV_REFERENCE.md（A-17 收口，2026-11-09）
//
// 背景：用户端代码直读 161 个环境键、.env-example 只文档了 80 个；平台端 31 vs 25。
// 逐个补进 example 会让 example 变成事实上的文档（难维护、双源易漂移），
// 改为「脚本可再生成的集中参考文档」：
//
//	go run ./scripts/gen_env_reference.go          # 生成 docs/ENV_REFERENCE.md
//	go run ./scripts/gen_env_reference.go -check   # 校验文档与代码一致（CI 用）
//
// 提取口径（与 checklist A-17 审计一致）：
//   - 扫 internal/ cmd/ 的非 _test.go 文件中 os.Getenv("KEY") / os.LookupEnv("KEY")
//   - 叠加 config.yaml 里 ${VAR:default} 展开引用的键
//   - 排除清单：纯测试脚手架键（LOADER_TEST_* 等只出现在 _test.go，天然被排除；
//     TESTDB_*/TEST_ADMIN_* 等 testutil/CI 专用键单列"测试基建"节不混入部署键）
//
// 文档带指纹头（# key-count: N），-check 比对指纹防止文档烂掉。
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
)

var envRefRe = regexp.MustCompile(`os\.(Getenv|LookupEnv)\("([A-Z][A-Z_0-9]+)"\)`)
var envConstRe = regexp.MustCompile(`os\.(Getenv|LookupEnv)\(([A-Z][A-Za-z0-9_]*)\)`)
// 常量声明形态宽松匹配：const XxxYyy = "ENV_KEY"（XxxYyy 以 Env/EnvKey/FlagEnv 结尾优先，
// 但不做后缀硬限定——宽松提取后由「被 os.Getenv(常量名) 引用」这个条件自然收口，
// 不会把无关常量误算进键集）。
var constDeclRe = regexp.MustCompile(`(?:const\s+)?([A-Z][A-Za-z0-9_]*)\s+(?:[A-Za-z0-9_.\[\]*]+\s+)?=\s*"([A-Z][A-Z_0-9]{2,})"`)
var yamlVarRe = regexp.MustCompile(`\$\{([A-Z][A-Z_0-9]+)[:}]`)

// testInfraKeys 测试基建键：只被 testutil/测试脚本消费，不属于部署配置面。
var testInfraKeys = map[string]bool{
	"POSTGRES_TEST_DSN": true, "POSTGRES_TEST_HOST": true, "POSTGRES_TEST_PORT": true,
	"POSTGRES_TEST_PASSWORD": true, "POSTGRES_TEST_DBNAME": true, "USER_POSTGRES_TEST_HOST_PORT": true,
	"TESTDB_CLOSE_TIMEOUT": true, "TESTDB_CONFIG_CHILD": true, "TESTDB_LEGACY": true, "TESTDB_TIMING": true,
	"TEST_ADMIN_USERNAME": true, "TEST_ADMIN_PASSWORD": true, "SKIP_BROWSER_TESTS": true,
	"CI": true, "GITHUB_ACTIONS": true, "UPDATE_GOLDEN": true, "UPDATE_WIRE_MANIFEST": true,
}

type keyInfo struct {
	Name    string
	Sources []string // "internal/xxx.go:12" 或 "config.yaml"
}

func goFiles(root string) ([]string, error) {
	var out []string
	walkErr := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == "vendor" || info.Name() == "node_modules" || info.Name() == "bin" {
				return filepath.SkipDir
			}
			return nil
		}
		name := info.Name()
		if strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
			out = append(out, p)
		}
		return nil
	})
	return out, walkErr
}

func collect() (map[string]*keyInfo, error) {
	keys := map[string]*keyInfo{}
	add := func(name, src string) {
		if testInfraKeys[name] {
			return
		}
		k, ok := keys[name]
		if !ok {
			k = &keyInfo{Name: name}
			keys[name] = k
		}
		k.Sources = append(k.Sources, src)
	}

	// 常量间接引用：os.Getenv(SomeEnvConst) —— 先收集常量声明映射。
	// （auth_register.go 的 EnvSiteTenantCode 等 35+ 个键走此形态，直读正则会漏。）
	constMap := map[string]string{} // constName -> envKey
	for _, dir := range []string{"internal", "cmd"} {
		files, err := goFiles(dir)
		if err != nil {
			return nil, fmt.Errorf("扫描 %s: %w", dir, err)
		}
		for _, f := range files {
			fh, err := os.Open(f)
			if err != nil {
				continue
			}
			sc := bufio.NewScanner(fh)
			sc.Buffer(make([]byte, 1024*1024), 1024*1024)
			ln := 0
			for sc.Scan() {
				ln++
				line := sc.Text()
				for _, m := range constDeclRe.FindAllStringSubmatch(line, -1) {
					constMap[m[1]] = m[2]
				}
			}
			fh.Close()
		}
	}

	for _, dir := range []string{"internal", "cmd"} {
		files, err := goFiles(dir)
		if err != nil {
			return nil, fmt.Errorf("扫描 %s: %w", dir, err)
		}
		for _, f := range files {
			fh, err := os.Open(f)
			if err != nil {
				continue
			}
			sc := bufio.NewScanner(fh)
			sc.Buffer(make([]byte, 1024*1024), 1024*1024)
			ln := 0
			for sc.Scan() {
				ln++
				line := sc.Text()
				for _, m := range envRefRe.FindAllStringSubmatch(line, -1) {
					add(m[2], fmt.Sprintf("%s:%d", f, ln))
				}
				for _, m := range envConstRe.FindAllStringSubmatch(line, -1) {
					if key, ok := constMap[m[2]]; ok {
						add(key, fmt.Sprintf("%s:%d", f, ln))
					}
				}
			}
			fh.Close()
		}
	}

	// config.yaml 的 ${VAR:default}
	for _, y := range []string{"config.yaml"} {
		if _, err := os.Stat(y); err != nil {
			continue
		}
		fh, err := os.Open(y)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(fh)
		ln := 0
		for sc.Scan() {
			ln++
			for _, m := range yamlVarRe.FindAllStringSubmatch(sc.Text(), -1) {
				if m[1] == "VAR" { // ${VAR:...} 文档示例占位
					continue
				}
				add(m[1], fmt.Sprintf("%s:%d", y, ln))
			}
		}
		fh.Close()
	}
	return keys, nil
}

// securityKeys 安全相关键：文档里单独标注（部署者可配面里最需要看见的）。
var securityKeys = map[string]string{
	"JWT_SECRET":                       "登录 JWT 签名密钥（≥32 字符）",
	"USER_JWT_SECRET":                  "登录 JWT 签名密钥（config.yaml 展开，≥32 字符）",
	"USER_WEB_DIST":                    "（见 example）",
	"MASTER_KEY":                       "字段加密主密钥；非 dev 环境缺失即拒绝启动",
	"FIELD_ENCRYPTION_KEY":             "字段级加密密钥",
	"BRIDGE_INGEST_TOKEN":              "桥接入栈令牌；未配置 503 fail-closed",
	"BRIDGE_INGEST_TOKEN_PREV":         "桥接令牌轮换期的旧令牌（双读）",
	"BRUTE_FORCE_DISABLED":             "危险：置 1/true 关闭登录爆破防护（仅诊断临时用）",
	"WS_AGENT_ALLOW_ALL_USERS":         "危险：置 true 跳过 WS 坐席身份闸（仅本地调试）",
	"ALLOW_INSECURE_TELEGRAM_WEBHOOK":  "危险：允许 Telegram webhook 免签（dev 双护栏）",
	"ALLOW_INSECURE_WEBHOOK":           "危险：允许 webhook 免 HTTPS（dev）",
	"MARKETING_WEBHOOK_ALLOW_INSECURE": "危险：营销 webhook 免签（dev）",
	"ALLOW_SELF_RESTART":               "允许进程自重启（离线升级面）",
	"MERCHANT_API_SECRET":              "商户 API 共享 HMAC 密钥（平台端）",
	"MERCHANT_STATE_DIR":               "商户身份密钥文件目录",
	"ONEID_SALT":                       "OneID 身份归并盐",
	"MCP_TOKEN":                        "MCP 网关令牌",
	"MCP_TOKEN_PREV":                   "MCP 令牌轮换旧值（双读）",
	"EMAIL_WEBHOOK_SECRET":             "邮件 webhook HMAC 密钥",
	"GEO_GITHUB_TOKEN":                 "GitHub 部署令牌",
	"EPAY_API_KEY":                     "易支付商户密钥（入库加密，依赖 PAYMENT_ENC_KEY）",
	"EPAY_PID":                         "易支付商户 ID",
	"EPAY_GATEWAY_URL":                 "易支付网关地址",
	"EPAY_MOCK":                        "危险：true 开 mock 收银台（release 模式启动即失败）",
	"PAYMENT_ENC_KEY":                  "支付密钥加密主键",
	"LLM_ENC_KEY":                      "LLM provider 密钥加密主键（平台端）",
	"JWT_SECRET_OLD":                   "JWT 轮换旧密钥（tokens_invalid_before 语义）",
	"VISITOR_TOKEN_SECRET":             "访客 token HMAC 密钥",
	"QINIU_SECRET_KEY":                 "七牛云 secret",
	"SMTP_PASSWORD":                    "SMTP 口令",
	"QQ_EMAIL_PASSWORD":                "QQ 邮箱授权码",
	"FEISHU_APP_SECRET":                "飞书应用 secret",
	"NOTION_API_KEY":                   "Notion API key",
	"OPENAI_API_KEY":                   "OpenAI 兼容端点密钥（LLM 池）",
	"EMBEDDING_API_KEY":                "Embedding 端点密钥",
	"RERANK_API_KEY":                   "Rerank 端点密钥",
	"DEEPL_API_KEY":                    "DeepL 翻译密钥",
	"CONTRIBUTOR_JWT_SECRET":           "贡献者门户独立 JWT 密钥（平台端）",
	"PLATFORM_JWT_SECRET":              "平台管理端 JWT 密钥（平台端）",
}

func repoRoot() string {
	if runtime.GOOS == "windows" {
		return "."
	}
	return "."
}

func generate(check bool) error {
	keys, err := collect()
	if err != nil {
		return err
	}
	names := make([]string, 0, len(keys))
	for k := range keys {
		names = append(names, k)
	}
	sort.Strings(names)

	var b strings.Builder
	fmt.Fprintf(&b, "# 环境变量参考（机械生成，勿手改）\n\n")
	fmt.Fprintf(&b, "> 由 `scripts/gen_env_reference.go` 扫描 internal/ cmd/（非 _test.go）与 config.yaml 生成。\n")
	fmt.Fprintf(&b, "> 再生成：`go run ./scripts/gen_env_reference.go`；CI 校验：`go run ./scripts/gen_env_reference.go -check`。\n")
	fmt.Fprintf(&b, "> 测试基建专用键（POSTGRES_TEST_* / TESTDB_* / TEST_ADMIN_* / CI 等）不属部署配置面，未收录。\n")
	fmt.Fprintf(&b, "> <!-- envref-fingerprint: %d keys -->\n\n", len(names))
	fmt.Fprintf(&b, "| 键 | 说明 | 读取点 |\n|---|---|---|\n")
	for _, n := range names {
		k := keys[n]
		desc := ""
		if d, ok := securityKeys[n]; ok {
			desc = d
		}
		srcs := k.Sources
		sort.Strings(srcs)
		max := 3
		if len(srcs) > max {
			srcs = append(srcs[:max], fmt.Sprintf("…共 %d 处", len(k.Sources)))
		}
		fmt.Fprintf(&b, "| `%s` | %s | `%s` |\n", n, desc, strings.Join(srcs, `<br>`))
	}

	out := filepath.Join("docs", "ENV_REFERENCE.md")
	if check {
		cur, rerr := os.ReadFile(out)
		if rerr != nil {
			return fmt.Errorf("文档不存在：%w（运行生成命令一次）", rerr)
		}
		if string(cur) != b.String() {
			return fmt.Errorf("docs/ENV_REFERENCE.md 与代码扫描结果不一致：请重新生成并提交")
		}
		fmt.Println("ENV_REFERENCE.md 与代码一致 ✓")
		return nil
	}
	if err := os.MkdirAll("docs", 0o755); err != nil {
		return err
	}
	return os.WriteFile(out, []byte(b.String()), 0o644)
}

func main() {
	check := flag.Bool("check", false, "校验已生成文档与扫描结果一致")
	flag.Parse()
	_ = repoRoot
	if err := generate(*check); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
