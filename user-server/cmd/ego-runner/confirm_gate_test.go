package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 事前阻塞写确认闸（specs/014-egolite-blocking-confirm-gate）的 runner 侧语义。
// 这组用例不打 WS、不起 ego-browser，只钉三件事：
//  ① 注入契约——裁决文件/TTL/helper 必须写进脚本文本（env 不穿透，F13）；
//  ② 裁决落地——allow 放行、deny 写文件并杀进程、错键忽略（fail-close）；
//  ③ 键匹配——服务端行键 ego-<row> 与 dispatch 键 ego-<row>-<ms> 的前缀语义。
//
// 每条都对应一次真实断裂或一次定案（见 tasks.md T001-T007）。

func TestAssembleInjectsConfirmContract(t *testing.T) {
	r, _ := newTestRunner(t, "gate-tool", "tools/g.js", "console.log(JSON.stringify({type:'action'}));\n")
	spec := taskSpec{
		ExecutionID: "ego-77-1700000000077", AssetID: "gate-tool", Entry: "tools/g.js",
		Origin: "market", ConfirmTTL: 45,
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

	// 裁决文件路径必须注入，且与 runner 侧派生路径一致（同一目录、同名文件）
	wantPath := confirmDecisionPath(path)
	if !strings.Contains(got, "const __CONFIRM_FILE__ = "+fmt.Sprintf("%q", wantPath)+";") {
		t.Errorf("__CONFIRM_FILE__ 未注入或与派生路径不一致（want %s）:\n%s", wantPath, got)
	}
	if !strings.Contains(got, "const __CONFIRM_TTL_SEC__ = 45;") {
		t.Errorf("spec.ConfirmTTL=45 应原样注入:\n%s", got)
	}
	// helper 必须在前导里定义，且依赖 node:fs/promises 动态导入（E5：无环境可依赖）
	if !strings.Contains(got, "async function __awaitConfirm__(label)") {
		t.Errorf("__awaitConfirm__ helper 未注入:\n%s", got)
	}
	if !strings.Contains(got, `import("node:fs/promises")`) {
		t.Errorf("helper 必须动态导入 node:fs/promises（E5 实测约束）:\n%s", got)
	}
	// entry 原文原样在内（包装不得改写脚本语义），且在 helper 之后
	if !strings.Contains(got, "console.log(JSON.stringify({type:'action'}));") {
		t.Errorf("entry 脚本内容被改写:\n%s", got)
	}
	if strings.Index(got, "__awaitConfirm__(label)") > strings.Index(got, "console.log(JSON.stringify({type:'action'}));") {
		t.Error("helper 定义应出现在 entry 之前（脚本可能直接调用）")
	}
}

func TestAssembleConfirmTTLDefault(t *testing.T) {
	r, _ := newTestRunner(t, "ttl-tool", "tools/t.js", "")
	path, cleanup, err := r.assembleScript(taskSpec{
		ExecutionID: "ego-8-8", AssetID: "ttl-tool", Entry: "tools/t.js", Origin: "market",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "const __CONFIRM_TTL_SEC__ = 300;") {
		t.Errorf("ConfirmTTL 缺省应注入 300（与服务端默认一致）:\n%s", string(b))
	}
}

func TestExecKeyMatches(t *testing.T) {
	cases := []struct {
		dispatched, frameID string
		want                bool
		note                string
	}{
		{"ego-9-123", "ego-9", true, "行键命中 dispatch 键（此前精确相等必假——abort/confirm 全被丢弃）"},
		{"ego-9-123", "ego-9-123", true, "全等照常命中"},
		{"ego-91-1", "ego-9", false, "无 - 分隔不得命中（兄弟行隔离）"},
		{"ego-9", "ego-9-123", false, "反向（帧键更长）不命中"},
		{"ego-9-123", "", false, "空帧键拒绝"},
		{"", "ego-9", false, "空在途键拒绝"},
	}
	for _, tc := range cases {
		if got := execKeyMatches(tc.dispatched, tc.frameID); got != tc.want {
			t.Errorf("execKeyMatches(%q,%q)=%v, want %v（%s）",
				tc.dispatched, tc.frameID, got, tc.want, tc.note)
		}
	}
}

// newExecWithDecision 造一个带裁决文件与可观测 cancel 的在途执行
func newExecWithDecision(t *testing.T, execID string) (*runner, *execContext, *bool) {
	t.Helper()
	canceled := false
	ec := &execContext{
		spec:         taskSpec{ExecutionID: execID},
		cancel:       func() { canceled = true },
		decisionPath: filepath.Join(t.TempDir(), confirmDecisionFile),
	}
	r := &runner{conf: &runnerConf{WorkspaceDir: t.TempDir()}}
	r.current = ec
	return r, ec, &canceled
}

func TestHandleConfirmFrameAllow(t *testing.T) {
	r, ec, canceled := newExecWithDecision(t, "ego-9-123")
	r.handleConfirmFrame("ego-9", "allow")
	b, err := os.ReadFile(ec.decisionPath)
	if err != nil || !strings.Contains(string(b), `"allow"`) {
		t.Fatalf("allow 应写裁决文件，文件=%q err=%v", string(b), err)
	}
	if *canceled {
		t.Error("allow 不得取消执行进程（放行通道）")
	}
}

func TestHandleConfirmFrameDeny(t *testing.T) {
	r, ec, canceled := newExecWithDecision(t, "ego-9-123")
	r.handleConfirmFrame("ego-9-123", "deny")
	b, err := os.ReadFile(ec.decisionPath)
	if err != nil || !strings.Contains(string(b), `"deny"`) {
		t.Fatalf("deny 应写裁决文件（让脚本有机会打出 ok:false 审计行），文件=%q err=%v", string(b), err)
	}
	if !*canceled {
		t.Error("deny 必须 fail-close 取消执行进程")
	}
}

func TestHandleConfirmFrameMismatchIgnored(t *testing.T) {
	r, ec, canceled := newExecWithDecision(t, "ego-91-1")
	r.handleConfirmFrame("ego-9", "allow") // 兄弟行键
	if _, err := os.Stat(ec.decisionPath); !os.IsNotExist(err) {
		t.Errorf("错键不得写裁决文件: %v", err)
	}
	if *canceled {
		t.Error("错键不得取消执行")
	}
	// 未知 decision 值同样不动作（fail-close：不认识的裁决不能当放行用）
	r.handleConfirmFrame("ego-91", "maybe")
	if _, err := os.Stat(ec.decisionPath); !os.IsNotExist(err) {
		t.Errorf("未知 decision 不得写裁决文件: %v", err)
	}
}

func TestEgoNodeArgs(t *testing.T) {
	plain := egoNodeArgs(&runnerConf{})
	if len(plain) != 1 || plain[0] != "nodejs" {
		t.Errorf("无 server_name 应只有 nodejs 子命令，实际 %v", plain)
	}
	named := egoNodeArgs(&runnerConf{EgoServerName: "work-a"})
	if len(named) != 2 || named[0] != "--ego-server-name=work-a" || named[1] != "nodejs" {
		t.Errorf("--ego-server-name 必须前置在 nodejs 之前（官方全局选项，E3），实际 %v", named)
	}
}

func TestLoadConfEgoServerName(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	confFile := filepath.Join(t.TempDir(), "ego_runner.conf")
	content := "server_url=ws://127.0.0.1:8204\ntoken=eg_1_abc\ndevice_id=11111111-1111-1111-1111-111111111111\nego_server_name=work-a\n"
	if err := os.WriteFile(confFile, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EGO_RUNNER_CONF", confFile)
	c, err := loadConf()
	if err != nil {
		t.Fatal(err)
	}
	if c.EgoServerName != "work-a" {
		t.Errorf("ego_server_name 未解析: %q", c.EgoServerName)
	}
}
