package main

// round.go 通用执行能力（ops 轮次）的 runner 侧实现。
//
// 设计纪律（internal-docs/ego-browser-通用执行能力-技术方案与开发清单-2026-10-28.md）：
//   - 默认路径不接受模型裸写 JS：ops 是闭合动作集，由本文件编译成固定模板；
//   - 所有页面动作走 ego-browser 官方白名单 API；
//   - 观测只回结构化 targets/text（整页 snapshot 回灌不进 4000 字节工具结果）；
//   - 写动作在编译期插入 __awaitConfirm__（复用 specs/014 的事前阻塞确认闸）。

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"hivemtk-user/internal/browsercore"
)

// buildOpsScript 把服务端下发的 ops 交给共享编译器（browsercore.CompileOpsScript）。
// runner 只负责解 JSON，不维护第二份模板——避免"服务端预览/沉淀流程"与"真机执行"漂移。
func buildOpsScript(spec taskSpec, dir string) string {
	var ops []browsercore.Op
	if len(spec.Ops) > 0 {
		_ = json.Unmarshal(spec.Ops, &ops)
	}
	obs := browsercore.DefaultObserveSpec()
	if len(spec.Observe) > 0 {
		_ = json.Unmarshal(spec.Observe, &obs)
	}
	obs.Normalize()
	return browsercore.CompileOpsScript(ops, obs, spec.Round)
}

// finishSpec 收尾帧（会话 finish：保留 keep 指定的 Page，关闭其余 agent 页）
type finishSpec struct {
	ExecutionID string   `json:"execution_id"`
	SpaceID     string   `json:"space_id"`
	SpaceName   string   `json:"space_name"`
	Keep        []string `json:"keep"`
}

// runFinish 执行官方 finish（best-effort）：空间不存在则如实跳过，不新建空 space。
func (r *runner) runFinish(spec finishSpec) {
	keep := "[]"
	if len(spec.Keep) > 0 {
		if b, err := json.Marshal(spec.Keep); err == nil {
			keep = string(b)
		}
	}
	script := fmt.Sprintf(`
const __KEEP__ = %s;
const __WANT_ID__ = %q;
const __WANT_NAME__ = %q;
try {
  const spaces = (typeof listTaskSpaces === "function") ? await listTaskSpaces() : [];
  const found = (spaces || []).find((s) =>
    (__WANT_ID__ && String(s.id) === String(__WANT_ID__)) ||
    (__WANT_NAME__ && s.name === __WANT_NAME__));
  if (!found) {
    console.log(JSON.stringify({ type: "runner", ok: true, action: "finish", skipped: "space_not_found" }));
  } else {
    const t = await taskSpace(found.id);
    await t.finish({ keep: __KEEP__ });
    console.log(JSON.stringify({ type: "runner", ok: true, action: "finish", space_id: String(found.id) }));
  }
} catch (e) {
  console.log(JSON.stringify({ type: "runner", ok: false, action: "finish", error: String(e && e.message ? e.message : e) }));
}
`, keep, spec.SpaceID, spec.SpaceName)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	dir, err := os.MkdirTemp("", "ego-finish-*")
	if err != nil {
		r.send(eventLine{Type: "runner", Execution: spec.ExecutionID, Payload: mustJSON(map[string]any{"ok": false, "action": "finish", "error": err.Error()})})
		return
	}
	defer func() { _ = os.RemoveAll(dir) }() // 临时目录善后清理，错误无从处置
	path := filepath.Join(dir, "finish.mjs")
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		r.send(eventLine{Type: "runner", Execution: spec.ExecutionID, Payload: mustJSON(map[string]any{"ok": false, "action": "finish", "error": err.Error()})})
		return
	}
	cmd := exec.CommandContext(ctx, r.conf.EgoBin, egoNodeArgs(r.conf)...)
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }() // stdin 喂完即弃，关闭错误无从处置
	cmd.Stdin = f
	out, _ := cmd.Output()
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "{") && json.Valid([]byte(line)) {
			r.send(eventLine{Type: "runner", Execution: spec.ExecutionID, Payload: json.RawMessage(line)})
		}
	}
}
