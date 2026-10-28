package browsercore

// CompileOpsScript 把 ops 编译成固定模板脚本（服务端/runnner 共用唯一实现）。
//
// 前置（由 ego-runner 的 preamble 注入，见 cmd/ego-runner/main.go）：
//   __EXEC__ / __SPACE_NAME__ / __SPACE_ID__ / __CONFIRM_FILE__ / __CONFIRM_TTL_SEC__
//   __CONFIRM_POLICY__ / __ARTIFACT_DIR__ / __DOMAINS__ / __awaitConfirm__ / __gotoChecked__
//
// 纪律：脚本不接受任何调用方拼接的 JS；ops 只作为 JSON 数组嵌入，动作分发在模板内闭合。
import (
	"encoding/json"
	"fmt"
)

// CompileOpsScript 生成一轮（或一串）动作的完整脚本体。
func CompileOpsScript(ops []Op, observe ObserveSpec, round int) string {
	if ops == nil {
		ops = []Op{}
	}
	observe.Normalize()
	opsJSON, err := json.Marshal(ops)
	if err != nil {
		opsJSON = []byte("[]")
	}
	obsJSON, err := json.Marshal(observe)
	if err != nil {
		obsJSON = []byte("{}")
	}
	return fmt.Sprintf(`
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
`, string(opsJSON), string(obsJSON), round)
}
