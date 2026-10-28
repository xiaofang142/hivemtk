package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// 真机 E2E（默认跳过）：EGO_E2E=1 时用真实 ego-browser 跑一轮 ops。
// 验证：ops 模板可执行、观测能解析出 targets、round_result 终帧可被服务端消费。
func TestOpsRoundE2E(t *testing.T) {
	if os.Getenv("EGO_E2E") != "1" {
		t.Skip("设置 EGO_E2E=1 且本机装有 ego lite 时运行")
	}
	bin, err := exec.LookPath("ego-browser")
	if err != nil {
		t.Skip("ego-browser 不在 PATH")
	}
	ops, _ := json.Marshal([]map[string]any{
		{"op": "goto", "url": "https://example.com"},
		{"op": "extract", "target": "css=a[href]", "fields": []string{"text", "href"}, "limit": 3},
	})
	spec := taskSpec{
		ExecutionID: "ego-e2e-1", Mode: "ops", Round: 1,
		SpaceName: "hivemtk-e2e-ops", Ops: ops, ConfirmPolicy: "none",
		Domains: []string{"example.com"},
	}
	r := &runner{conf: &runnerConf{EgoBin: bin}}
	path, cleanup, err := r.assembleScript(spec)
	if err != nil {
		t.Fatalf("组装失败: %v", err)
	}
	defer cleanup()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cmd := exec.Command(bin, "nodejs")
	cmd.Stdin = f
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ego-browser 执行失败: %v\n%s", err, out)
	}
	var got map[string]any
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) == nil && m["type"] == "round_result" {
			got = m
		}
	}
	if got == nil {
		t.Fatalf("没有 round_result 终帧:\n%s", out)
	}
	if got["ok"] != true {
		t.Fatalf("ok=%v err=%v", got["ok"], got["error"])
	}
	targets, _ := got["targets"].([]any)
	if len(targets) == 0 {
		t.Fatalf("观测 targets 为空: %s", out)
	}
	if ex, _ := got["extract"].([]any); len(ex) == 0 {
		t.Fatalf("extract 为空: %s", out)
	}
	t.Logf("space_id=%v targets=%d extract=%d", got["space_id"], len(targets), len(got["extract"].([]any)))
}
