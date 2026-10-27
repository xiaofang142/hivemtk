package egolite

import (
	"fmt"
	"regexp"
	"strings"
)

// 静态扫描门（设计文档 A4 / 调研 R1①、E-02）——ego-lite 资产 JS 的**第一道筛**。
//
// 定位纪律（对抗审核 E-02 收口）：
//   - 静态扫描对 JS 动态特性（字符串拼接 require / eval / atob / new Function / Unicode 转义）
//     **原理性失效**，本门只承诺「已知恶意模式样例集全拦」，不承诺 100%；
//   - 主闸门是运行时收敛（runner 受限环境，R4/T13），本门是纵深防御的筛查层；
//   - 双端同源：本包是唯一实现，user-server（上架/保存流程时）与 ego-runner（M2 收敛改造后）
//     共用同一份规则——oneof 白名单双端漂移的教训不重演。
//
// 规则三类：禁止 import/require、禁止动态执行、注入模式（R8：arXiv 2507.14799 防线）。

// ScanRule 单条扫描规则
type ScanRule struct {
	ID          string         // 规则标识（审核报告引用）
	Desc        string         // 人工可读说明
	Pattern     *regexp.Regexp // 匹配模式
	Severity    string         // block（命中即拒）/ warn（进人工审核清单）
}

var scanRules = buildScanRules()

func buildScanRules() []ScanRule {
	rx := func(p string) *regexp.Regexp { return regexp.MustCompile(p) }
	return []ScanRule{
		// —— 禁止模块加载（R1 核心：收敛执行面到 ego helpers + page 原语） ——
		{"EGO-R001", "require 任意模块加载", rx(`(?m)\brequire\s*\(`), "block"},
		{"EGO-R002", "node: 前缀模块（child_process/net/fs/dns/...）", rx(`(?m)(?:import\s+[^;]*from\s*|import\s*\(?|require\s*\(\s*)["']node:`), "block"},
		{"EGO-R003", "child_process 子进程", rx(`(?i)child[_\s]*process|spawnSync|execSync|fork\s*\(`), "block"},
		{"EGO-R004", "import() 动态导入", rx(`(?m)\bimport\s*\(\s*[^)]`), "block"},
		{"EGO-R005", "fs 文件系统访问", rx(`(?m)(?:from\s*|require\s*\(\s*)["']\s*(?:node:)?fs(?:/promises)?["']|writeFile|appendFile|unlinkSync|rmSync|mkdirSync`), "block"},
		{"EGO-R006", "net/http 服务端模块", rx(`(?m)(?:from\s*|require\s*\(\s*)["']\s*(?:node:)?(?:net|http|https|dgram|tls|dns|http2)["']`), "block"},

		// —— 禁止动态执行（混淆 JS 的主通道，全部 block） ——
		// R010：eval( 直呼；负向前瞻排除合法页内原语 $$eval( / $eval(（page.$$eval 是
		// puppeteer/ego 的标准页内求值 API）。Go regexp 不支持 lookahead，用否定字符类实现：
		// eval( 前一个字符不能是 $ 或字母数字下划线（ 已排除字母数字，这里再排 $）。
		{"EGO-R010", "eval 动态执行", rx(`(?:^|[^$\w])eval\s*\(`), "block"},
		{"EGO-R011", "Function 构造器动态执行", rx(`(?m)new\s+Function\s*\(|Function\s*\(\s*["']`), "block"},
		{"EGO-R012", "atob/base64 解码执行组合", rx(`(?i)atob\s*\(|Buffer\.from\([^)]*["']base64["']`), "block"},
		{"EGO-R013", "process 全局（进程面收敛）", rx(`(?m)\bprocess\s*\.\s*(?:binding|dlopen|exit|kill|env\s*\[)`), "block"},
		{"EGO-R014", "globalThis/间接执行器", rx(`(?m)globalThis\s*\[|global\s*\[\s*["'](?:eval|require|Function)["']`), "block"},

		// —— 注入模式（R8：learnings notes 属半可信，资产代码亦扫） ——
		{"EGO-R020", "页面内容直接拼接进指令（提示注入面）", rx(`(?i)(?:snapshot|content|text)\s*[^;\n]{0,40}(?:goal|instruction|command|命令|指令)`), "warn"},
		{"EGO-R021", "忽略/覆盖先前指令类注入话术", rx(`(?i)ignore\s+(?:all\s+)?(?:previous|prior|above)\s+(?:instructions|prompts)|忽略(?:之前|以上|上述)(?:的)?(?:指令|提示)`), "warn"},
		{"EGO-R022", "凭据/密钥硬编码疑似", rx(`(?i)(?:api[_-]?key|secret|password|token)\s*[:=]\s*["'][A-Za-z0-9+/]{20,}["']`), "warn"},

		// —— 外联白名单外的 fetch 由运行时域检兜底（模板层），此处只 warn 提示审核 ——
		{"EGO-R030", "显式 fetch 外联（运行时域检兜底，审核需核对 domains 声明）", rx(`(?m)\bfetch\s*\(`), "warn"},
		{"EGO-R031", "XMLHttpRequest 外联", rx(`(?m)XMLHttpRequest|new\s+WebSocket\s*\(`), "warn"},
	}
}

// ScanVerdict 单条命中
type ScanVerdict struct {
	RuleID   string `json:"rule_id"`
	Desc     string `json:"desc"`
	Severity string `json:"severity"`
	Line     int    `json:"line"`
	Excerpt  string `json:"excerpt"` // 命中行截断（审核展示；≤120 字符）
}

// ScanReport 扫描报告（审核工作台展示形状）
type ScanReport struct {
	Passed   bool          `json:"passed"`   // 无 block 即通过（warn 进人工审核清单）
	Blocked  []ScanVerdict `json:"blocked,omitempty"`
	Warnings []ScanVerdict `json:"warnings,omitempty"`
}

// ScanScript 静态扫描入口（唯一实现，双端共用）。
// src 为 JS 全文。已知残余（诚实登记，调研 R1）：字符串拼接/编码混淆可绕过文本规则，
// 运行时收敛才是主闸门。
func ScanScript(src string) ScanReport {
	report := ScanReport{Passed: true}
	lines := strings.Split(src, "\n")
	for _, rule := range scanRules {
		for i, line := range lines {
			loc := rule.Pattern.FindStringIndex(line)
			if loc == nil {
				continue
			}
			v := ScanVerdict{
				RuleID:   rule.ID,
				Desc:     rule.Desc,
				Severity: rule.Severity,
				Line:     i + 1,
				Excerpt:  truncateStr(line, 120),
			}
			if rule.Severity == "block" {
				report.Passed = false
				report.Blocked = append(report.Blocked, v)
			} else {
				report.Warnings = append(report.Warnings, v)
			}
		}
	}
	return report
}

// ScanManifest 校验 manifest 必备字段（A2：hivemtk 扩展 + domains 强制）。
// 传入解析后的 manifest JSON 形状（由调用方 unmarshal），这里做规则判定。
func ValidateManifestShape(m ManifestShape) error {
	if strings.TrimSpace(m.ID) == "" {
		return fmt.Errorf("manifest 缺 id")
	}
	if len(m.Domains) == 0 {
		return fmt.Errorf("manifest.domains 强制必填（域名白名单）")
	}
	for _, d := range m.Domains {
		if strings.Contains(d, "*") && d != "*" {
			return fmt.Errorf("domains 通配仅允许整站 *，不支持部分通配: %q", d)
		}
	}
	if strings.TrimSpace(m.HivemtkEntry) == "" {
		return fmt.Errorf("manifest.hivemtk.entry 必填（可执行入口）")
	}
	if m.HivemtkCompatMin == "" {
		return fmt.Errorf("manifest.hivemtk.compat_min 必填（版本协商，R3）")
	}
	// 写声明必须显式（可为空数组=纯读资产——但字段本身要出现，由调用方保证解析自显式 JSON）
	return nil
}

// ManifestShape manifest.json 的 hivemtk 关心面（解析后）
type ManifestShape struct {
	ID               string   `json:"id"`
	Domains          []string `json:"domains"`
	HivemtkEntry     string   `json:"-"`
	HivemtkCompatMin string   `json:"-"`
}

func truncateStr(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
