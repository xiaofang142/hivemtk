package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// round 脚本组装的三条纪律，每条都对应一次真实断裂：
//
//	① params / space_name 必须写进脚本文本 —— ego-browser 内嵌 Node 不继承 env、
//	  cwd 恒为 /（F13 实测），下发字段若只到 spec 就等于丢；
//	② 完整性永远查（script_hash 对 entry 字节），拦"装完之后本机被改"；
//	③ 验签只在 manifest 带签名时要求 —— 本实例发布的资产没有第三方签名环节，
//	  无条件验签会让"资产中心点运行"必然失败（曾实测：market 执行全灭）。
//
// 这组用例不打 DB、不起 WS，只钉组装函数的语义。

// newTestRunner 造一个 workspace 里已装好一个资产的 runner（形状对齐 assets.go 的 installAsset）
func newTestRunner(t *testing.T, assetID, entry, script string) (*runner, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home) // verifyAssetManifestAt 会读 ~/.hivemtk/ego_trust.pem，隔离掉
	ws := filepath.Join(t.TempDir(), "ego-skills")

	assetDir := filepath.Join(ws, "learnings", assetID)
	entryPath := filepath.Join(assetDir, filepath.FromSlash(entry))
	if err := os.MkdirAll(filepath.Dir(entryPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entryPath, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(script))
	manifest := map[string]any{
		"id":      assetID,
		"name":    "测试资产",
		"version": "1.0.0",
		"domains": []string{"example.com"},
		"hivemtk": map[string]any{
			"entry":       entry,
			"source":      "pack",
			"asset_id":    assetID,
			"asset_ver":   "1",
			"script_hash": hex.EncodeToString(sum[:]),
		},
	}
	mb, _ := json.MarshalIndent(manifest, "", "  ")
	if err := os.WriteFile(filepath.Join(assetDir, "manifest.json"), mb, 0o600); err != nil {
		t.Fatal(err)
	}
	return &runner{conf: &runnerConf{WorkspaceDir: ws}}, ws
}

func TestAssembleScriptInjectsParamsAndSpaceIntoText(t *testing.T) {
	const entry = "tools/echo.js"
	const src = "console.log(JSON.stringify({ type: 'action', seen: __PARAMS__ }));\n"
	r, _ := newTestRunner(t, "echo-tool", entry, src)

	spec := taskSpec{
		ExecutionID: "ego-42-1700000000000",
		AssetID:     "echo-tool",
		Entry:       entry,
		Origin:      "market",
		Params:      json.RawMessage(`{"keyword":"hivemtk","n":2}`),
		SpaceName:   "github-检索",
		Round:       1,
	}
	path, cleanup, err := r.assembleScript(spec)
	if err != nil {
		t.Fatalf("组装失败: %v", err)
	}
	defer cleanup()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)

	// ① 参数与 space 必须出现在文本里（唯一可行的注入方式）
	if !strings.Contains(got, `"keyword":"hivemtk"`) {
		t.Errorf("params 未写进脚本文本（env 不穿透，只能内联）:\n%s", got)
	}
	if !strings.Contains(got, `const __SPACE_NAME__ = "github-检索";`) {
		t.Errorf("space_name 未写进脚本文本（多轮续跑的唯一衔接点）:\n%s", got)
	}
	if !strings.Contains(got, `const __EXEC__ = "ego-42-1700000000000";`) {
		t.Errorf("__EXEC__ 未注入（脚本打点无法回填执行键）:\n%s", got)
	}
	// ② entry 原文必须原样在内（包装不得改写脚本语义）
	if !strings.Contains(got, strings.TrimSpace(src)) {
		t.Errorf("entry 脚本内容被改写:\n%s", got)
	}
	// ③ 前导必须在 entry 之前（脚本要用 __PARAMS__ 时不能是 TDZ）
	if strings.Index(got, "const __PARAMS__") > strings.Index(got, strings.TrimSpace(src)) {
		t.Error("前导声明应出现在 entry 之前")
	}
}

func TestAssembleScriptDefaultsEmptyParamsToObject(t *testing.T) {
	r, _ := newTestRunner(t, "ro-tool", "tools/ro.js", "console.log(JSON.stringify({type:'action'}));\n")
	for _, raw := range []json.RawMessage{nil, json.RawMessage(`null`), json.RawMessage(`  `)} {
		path, cleanup, err := r.assembleScript(taskSpec{
			ExecutionID: "ego-1-1", AssetID: "ro-tool", Entry: "tools/ro.js",
			Origin: "market", Params: raw,
		})
		if err != nil {
			t.Fatalf("params=%q 时组装失败: %v", string(raw), err)
		}
		b, _ := os.ReadFile(path)
		cleanup()
		// 脚本里引用 __PARAMS__ 时不能是 undefined/null（否则 obj.key 直接抛）
		if !strings.Contains(string(b), "const __PARAMS__ = {};") {
			t.Errorf("params=%q 应降级为 {}，实际:\n%s", string(raw), string(b))
		}
	}
}

func TestAssembleScriptRejectsTamperedEntry(t *testing.T) {
	r, ws := newTestRunner(t, "tamper-me", "tools/t.js", "console.log('orig');\n")
	// 装完之后本机被改（manifest 的 script_hash 没变）
	p := filepath.Join(ws, "learnings", "tamper-me", "tools", "t.js")
	if err := os.WriteFile(p, []byte("console.log('backdoored');\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := r.assembleScript(taskSpec{
		ExecutionID: "ego-2-2", AssetID: "tamper-me", Entry: "tools/t.js", Origin: "market",
	})
	if err == nil || !strings.Contains(err.Error(), "hash") {
		t.Fatalf("篡改后的 entry 必须被完整性校验拦下，实际 err=%v", err)
	}
}

func TestAssembleScriptSignedManifestNeedsTrustKey(t *testing.T) {
	r, ws := newTestRunner(t, "signed-asset", "tools/s.js", "console.log('s');\n")
	// 在 manifest 上补一个假签名：进入"来源认证"分支，而本机没有信任公钥
	mp := filepath.Join(ws, "learnings", "signed-asset", "manifest.json")
	var m map[string]any
	raw, _ := os.ReadFile(mp)
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	hivemtk, _ := m["hivemtk"].(map[string]any)
	hivemtk["sig_alg"] = "ed25519"
	hivemtk["signature"] = "AAAA"
	mb, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(mp, mb, 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := r.assembleScript(taskSpec{
		ExecutionID: "ego-3-3", AssetID: "signed-asset", Entry: "tools/s.js", Origin: "market",
	})
	if err == nil || !strings.Contains(err.Error(), "信任公钥") {
		t.Fatalf("带签名但无信任公钥必须 fail-close，实际 err=%v", err)
	}
}

func TestAssembleScriptDraftSkipsAssetVerification(t *testing.T) {
	r, _ := newTestRunner(t, "unused", "tools/u.js", "")
	path, cleanup, err := r.assembleScript(taskSpec{
		ExecutionID: "ego-4-4", Origin: "draft",
		DraftScript: "console.log(JSON.stringify({type:'action', draft:true}));\n",
		Params:      json.RawMessage(`{"a":1}`),
	})
	if err != nil {
		t.Fatalf("draft 不该走资产校验: %v", err)
	}
	defer cleanup()
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "draft:true") || !strings.Contains(string(b), `"a":1`) {
		t.Errorf("draft 脚本应原样内联并带 params:\n%s", string(b))
	}
}

// ---------- C16 开始/结束行强约束（调研 §六「输出协议软契约」收口） ----------
//
// 契约：start 恰好一行且先于任何作者代码；end 在终止时必有（抛错路径 ok:false 后
// 原样 rethrow，退出码语义不变）；顶层 import/export 的脚本退化为尾部追加。
// 手打同型帧由发布门 EGO-R034 拦（重复帧=契约破坏面）——本组用例钉注入器的形状。

func TestAssembleInjectsAssetStartEndFrames(t *testing.T) {
	const entry = "tools/framed.js"
	const src = "console.log(JSON.stringify({ type: 'action', hello: 1 }));\n"
	r, _ := newTestRunner(t, "framed-tool", entry, src)
	path, cleanup, err := r.assembleScript(taskSpec{
		ExecutionID: "ego-7-1", AssetID: "framed-tool", Entry: entry,
		Origin: "market", Round: 2,
	})
	if err != nil {
		t.Fatalf("组装失败: %v", err)
	}
	defer cleanup()
	b, _ := os.ReadFile(path)
	got := string(b)

	// start 恰好一次，且先于 entry（先于作者代码是契约的一半）
	if n := strings.Count(got, `event: "start"`); n != 1 {
		t.Fatalf("start 帧应恰好注入一次，实得 %d:\n%s", n, got)
	}
	if strings.Index(got, `event: "start"`) > strings.Index(got, strings.TrimSpace(src)) {
		t.Fatalf("start 帧必须先于 entry 代码:\n%s", got)
	}
	// start 帧带执行键与轮次（服务端对齐脚本侧日志用）
	if !strings.Contains(got, `execution_id: __EXEC__`) || !strings.Contains(got, `round: 2`) {
		t.Fatalf("start 帧必须带 execution_id/round:\n%s", got)
	}
	// end 两个分支（成功/抛错）都在注入模板里：抛错路径也要有 end 行（ok:false + rethrow）
	if !strings.Contains(got, `event: "end", ok: true`) {
		t.Fatalf("成功路径 end 帧缺失:\n%s", got)
	}
	if !strings.Contains(got, `event: "end", ok: false`) || !strings.Contains(got, "throw __egoEndErr;") {
		t.Fatalf("抛错路径 end 帧（ok:false + 原样 rethrow）缺失:\n%s", got)
	}
}

func TestAssembleTopLevelImportFallsBackToAppend(t *testing.T) {
	// 顶层 import 只能出现在模块顶层，块级包裹会 SyntaxError——必须退化为尾部追加
	// （弱保证：抛错路径没有 end 行，runner 终态帧/退出码兜底）。
	const entry = "tools/imp.js"
	const src = "import x from \"https://cdn.example.com/x.js\";\nconsole.log(x);\n"
	r, _ := newTestRunner(t, "imp-tool", entry, src)
	path, cleanup, err := r.assembleScript(taskSpec{
		ExecutionID: "ego-8-1", AssetID: "imp-tool", Entry: entry, Origin: "market", Round: 1,
	})
	if err != nil {
		t.Fatalf("含顶层 import 的脚本应能组装（退化模式）: %v", err)
	}
	defer cleanup()
	b, _ := os.ReadFile(path)
	got := string(b)
	if strings.Contains(got, "catch (__egoEndErr)") {
		t.Fatalf("含顶层 import 的脚本不许块级包裹（会语法错）:\n%s", got)
	}
	if !strings.Contains(got, `event: "end", ok: true`) {
		t.Fatalf("退化模式仍要有尾部 end 行:\n%s", got)
	}
	if !strings.Contains(got, `event: "start"`) {
		t.Fatalf("退化模式 start 帧照常注入:\n%s", got)
	}
}

func TestAssembleTopLevelExportFallsBackToAppend(t *testing.T) {
	const entry = "tools/exp.js"
	const src = "export const marker = 1;\nconsole.log(marker);\n"
	r, _ := newTestRunner(t, "exp-tool", entry, src)
	path, cleanup, err := r.assembleScript(taskSpec{
		ExecutionID: "ego-9-1", AssetID: "exp-tool", Entry: entry, Origin: "market", Round: 1,
	})
	if err != nil {
		t.Fatalf("含顶层 export 的脚本应能组装: %v", err)
	}
	defer cleanup()
	b, _ := os.ReadFile(path)
	if strings.Contains(string(b), "catch (__egoEndErr)") {
		t.Fatalf("含顶层 export 的脚本不许块级包裹:\n%s", string(b))
	}
}

func TestAssembleOpsModeGetsAssetFrames(t *testing.T) {
	// ops 轮次（Mode=ops）同走 assembleScript，start/end 帧契约一致——
	// round_result 是轮结果，asset start/end 是执行起止，两层各司其职。
	r, _ := newTestRunner(t, "ops-tool", "tools/o.js", "console.log('unused');\n")
	path, cleanup, err := r.assembleScript(taskSpec{
		ExecutionID: "ego-10-1", Origin: "market", Mode: "ops",
		Ops: json.RawMessage(`[{"op":"goto","url":"https://example.com"}]`),
	})
	if err != nil {
		t.Fatalf("ops 组装失败: %v", err)
	}
	defer cleanup()
	b, _ := os.ReadFile(path)
	got := string(b)
	if !strings.Contains(got, `event: "start"`) || !strings.Contains(got, `event: "end", ok: true`) {
		t.Fatalf("ops 模式也必须注入 start/end 帧:\n%s", got)
	}
}
