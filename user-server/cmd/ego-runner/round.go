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
)

// buildOpsScript 把 ops 数组编译成一段固定模板脚本（不接受任意代码拼接）。
func buildOpsScript(spec taskSpec, dir string) string {
	ops := "[]"
	if len(spec.Ops) > 0 {
		ops = string(spec.Ops)
	}
	obs := "{}"
	if len(spec.Observe) > 0 {
		obs = string(spec.Observe)
	}
	body := fmt.Sprintf(`
const __OPS__ = %s;
const __OBSERVE_SPEC__ = Object.assign(
  { snapshot: true, scope: "only_within_viewport", max_targets: 40, max_text: 1200, screenshot: "on_error" },
  %s || {}
);
const __ROUND_NO__ = %d;
const __WRITE_OPS__ = ["click", "fill", "press", "select", "upload"];

async function __main__() {
  const task = await taskSpace(__SPACE_NAME__ || ("hivemtk-" + __EXEC__));
  // handoff 恢复：space 已交还用户时，agent 侧重新接管（specs/016 真机验证过的原语）。
  try {
    if (typeof listTaskSpaces === "function" && typeof takeOverTaskSpace === "function") {
      const spaces = await listTaskSpaces();
      const found = (spaces || []).find((s) => String(s.id) === String(task.spaceId));
      if (found && String(found.ownership || "").indexOf("Delegated") >= 0) {
        await takeOverTaskSpace(found.id);
      }
    }
  } catch (e) {}
  const managed = await task.pages();
  let page = managed.find((p) => p.label === "p1") || managed[0];
  if (!page) page = await task.newPage();

  let err = null;
  let extract = null;
  const artifacts = [];
  let actions = 0;
  const t0 = Date.now();

  const finishRound = async (extra) => {
    const payload = Object.assign({
      type: "round_result",
      ok: !err,
      round: __ROUND_NO__,
      space_id: String(task.spaceId || ""),
      url: "",
      title: "",
      viewport: { w: 0, h: 0 },
      targets: [],
      text: "",
      text_truncated: false,
      extract: extract,
      artifacts: artifacts,
      usage: { ms: Date.now() - t0, actions: actions },
      error: err,
      fatal: false,
      handoff: false
    }, extra || {});
    try { payload.url = await page.url(); } catch (e) {}
    try { payload.title = await page.title(); } catch (e) {}
    try {
      const info = await page.info();
      payload.viewport = { w: Number(info.w || 0), h: Number(info.h || 0) };
    } catch (e) {}
    try {
      let scope = __OBSERVE_SPEC__.scope || "only_within_viewport";
      if (scope === "viewport") scope = "only_within_viewport";
      const snap = await page.snapshot({ scope: scope });
      const lines = String(snap).split("\n");
      let last = -1;
      let text = "";
      for (const line of lines) {
        const node = line.match(/^\s*([A-Za-z0-9_]+)\s*\[(.*)\]\s*$/);
        if (node) {
          const attrs = node[2];
          const rm = attrs.match(/ref=(\d+)/);
          if (rm) {
            const lm = attrs.match(/loc=([^,\]]+)/);
            payload.targets.push({ ref: "e" + rm[1], role: node[1], name: "", loc: lm ? lm[1] : "" });
            last = payload.targets.length - 1;
          } else {
            last = -1;
          }
          continue;
        }
        const tm = line.match(/^\s*text\s+"(.*)"\s*$/);
        if (tm) {
          if (last >= 0 && !payload.targets[last].name) payload.targets[last].name = tm[1];
          text += (text ? "\n" : "") + tm[1];
        }
      }
      const maxTargets = Number(__OBSERVE_SPEC__.max_targets || 40);
      if (payload.targets.length > maxTargets) payload.targets = payload.targets.slice(0, maxTargets);
      const maxText = Number(__OBSERVE_SPEC__.max_text || 1200);
      if (text.length > maxText) { text = text.slice(0, maxText); payload.text_truncated = true; }
      payload.text = text;
    } catch (e) {}
    console.log(JSON.stringify(payload));
  };

  for (let i = 0; i < __OPS__.length; i++) {
    const op = __OPS__[i];
    try {
      const isWrite = __WRITE_OPS__.indexOf(op.op) >= 0;
      if (isWrite && __CONFIRM_POLICY__ === "require") {
        const allowed = await __awaitConfirm__(op.label || (op.op + " " + (op.target || "")));
        if (!allowed) {
          err = { code: "CONFIRM_DENIED", msg: "写确认被拒绝或超时", step: i };
          break;
        }
      }
      if (op.op === "goto") {
        await __gotoChecked__(page, op.url);
        await page.waitForLoadState().catch(() => {});
      } else if (op.op === "click") {
        await page.click(op.target, { label: op.label || "click" });
      } else if (op.op === "fill") {
        await page.fill(op.target, op.value);
      } else if (op.op === "press") {
        if (op.target) await page.press(op.target, op.key);
        else await page.keyboard.press(op.key);
      } else if (op.op === "select") {
        if (op.key) await page.selectOption(op.target, { label: op.key });
        else await page.selectOption(op.target, op.value);
      } else if (op.op === "scroll") {
        await page.mouse.wheel(0, Number(op.dy || 0), { label: op.label || "scroll" });
      } else if (op.op === "wait_for") {
        const timeout = Number(op.timeout_ms || 15000);
        if (op.selector) await page.waitForSelector(op.selector, { timeout: timeout, state: "visible" });
        else if (op.url) await page.waitForURL(op.url, { timeout: timeout });
        else await page.waitForTimeout(timeout);
      } else if (op.op === "extract") {
        const selector = String(op.target || op.selector || "");
        if (selector.indexOf("text=") === 0 || selector.indexOf("loc=") === 0) {
          throw new Error("extract 仅支持 css= 或普通 CSS 选择器；观测里的 loc= 请用于 click/fill");
        }
        const arg = { selector: selector.replace(/^css=/, ""), fields: op.fields || ["text"], limit: Number(op.limit || 20) };
        extract = await page.evaluate(({ selector, fields, limit }) => {
          const nodes = Array.from(document.querySelectorAll(selector)).slice(0, limit);
          return nodes.map((n) => {
            const row = {};
            fields.forEach((f) => {
              if (f === "text") row.text = (n.textContent || "").trim();
              else if (f === "href") row.href = (n.querySelector("a") && n.querySelector("a").href) || n.getAttribute("href") || "";
              else if (f === "html") row.html = n.innerHTML;
              else row[f] = n.getAttribute(f) || "";
            });
            return row;
          });
        }, arg);
      } else if (op.op === "upload") {
        await page.setInputFiles(op.target, op.path);
      } else if (op.op === "screenshot") {
        const p = await page.screenshot({ path: __ARTIFACT_DIR__ + "/shot-" + Date.now() + ".png", fullPage: !!op.full_page });
        artifacts.push({ kind: "screenshot", path: p });
      } else if (op.op === "handoff") {
        console.log(JSON.stringify({ type: "handoff", reason: op.reason || "", round: __ROUND_NO__, space_id: String(task.spaceId || "") }));
        await task.handOff();
        await finishRound({ handoff: true });
        return;
      } else if (op.op === "finish") {
        await task.finish({ keep: op.keep || [] });
        await finishRound({ handoff: false, ok: !err });
        return;
      } else {
        throw new Error("unsupported op: " + op.op);
      }
      actions++;
    } catch (e) {
      err = { code: "OP_FAILED", msg: String(e && e.message ? e.message : e), step: i };
      break;
    }
  }

  if (err && __OBSERVE_SPEC__.screenshot === "on_error") {
    try {
      const p = await page.screenshot({ path: __ARTIFACT_DIR__ + "/error-" + Date.now() + ".png" });
      artifacts.push({ kind: "screenshot", path: p });
    } catch (e) {}
  }
  await finishRound({});
}

await __main__();
`, ops, obs, spec.Round)
	return body
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
	defer os.RemoveAll(dir)
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
	defer f.Close()
	cmd.Stdin = f
	out, _ := cmd.Output()
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "{") && json.Valid([]byte(line)) {
			r.send(eventLine{Type: "runner", Execution: spec.ExecutionID, Payload: json.RawMessage(line)})
		}
	}
}
