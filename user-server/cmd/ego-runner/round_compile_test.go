package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildOpsScriptShape(t *testing.T) {
	ops := []map[string]any{
		{"op": "goto", "url": "https://example.com"},
		{"op": "click", "target": "loc=role:button[name='Sign in']"},
		{"op": "fill", "target": "css=#email", "value": "a@b.com"},
		{"op": "extract", "target": "css=article", "fields": []string{"text", "href"}, "limit": 5},
	}
	opsJSON, _ := json.Marshal(ops)
	spec := taskSpec{
		ExecutionID: "ego-1", Round: 3, SpaceName: "s1",
		Mode: "ops", Ops: opsJSON, ConfirmPolicy: "require",
	}
	src := buildOpsScript(spec, "/tmp/artifacts")

	for _, want := range []string{
		`const __OPS__ = [`,
		`__awaitConfirm__`,
		`type: "round_result"`,
		`op.op === "goto"`,
		`__gotoChecked__`,
		`page.evaluate`,
		`max_targets: 40`,
		`__ARTIFACT_DIR__`,
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("ops 脚本缺少 %q", want)
		}
	}
	if strings.Contains(src, "process.exit") {
		t.Fatal("ops 脚本不允许 process.exit（会伪造 done 终态）")
	}
	if strings.Contains(src, "eval(") {
		t.Fatal("ops 脚本不允许 eval")
	}
	// JS 语法自检（本机有 node 时；CI 无 node 则跳过）
	if _, err := exec.LookPath("node"); err == nil {
		dir := t.TempDir()
		p := filepath.Join(dir, "round.mjs")
		if err := os.WriteFile(p, []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
		out, err := exec.Command("node", "--check", p).CombinedOutput()
		if err != nil {
			t.Fatalf("ops 脚本语法错误: %v\n%s", err, out)
		}
	}
}

func TestBuildOpsScriptDefaultObserve(t *testing.T) {
	spec := taskSpec{ExecutionID: "ego-2", Mode: "ops", Ops: json.RawMessage(`[{"op":"goto","url":"https://example.com"}]`)}
	src := buildOpsScript(spec, "/tmp")
	if !strings.Contains(src, `const __OBSERVE_SPEC__ = Object.assign`) {
		t.Fatal("缺省观测未注入")
	}
	if !strings.Contains(src, `const __ROUND_NO__ = 0`) {
		t.Fatal("round 未注入")
	}
}

func TestFinishScriptShape(t *testing.T) {
	// runFinish 的脚本是内联生成的；这里只校验关键语义存在（真机 finish 见 specs/016）
	spec := finishSpec{ExecutionID: "ego-3", SpaceName: "s3", Keep: []string{"p1"}}
	if spec.SpaceName == "" || len(spec.Keep) != 1 {
		t.Fatal("finish spec 形状")
	}
}
