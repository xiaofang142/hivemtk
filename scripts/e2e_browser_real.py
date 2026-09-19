#!/usr/bin/env python3
"""e2e_browser_real.py — 浏览器自动化 A 链路真机任务集（编排→执行→监测→审计核验）。

覆盖 spec 2026-09-19 §二.4 的 A 链路任务集：
  xhs 读链路 / xhs 三段式写 / D7 人工确认闸门 / douyin 读 / xianyu 读 / cron 入口 / Brain 模式，
  每条跑完都从 GET /sessions/:id/export 逐项核验审计包（会话+步+命令流+LLM 成本账）。

前提（任一缺失即自动降级为 preflight：不依赖 Host 的契约项照跑，其余记 SKIP 并打印处置指引）：
  1) user-server 已启动（r29 起含 D7 require_confirm 列与 /sessions/:id/confirm）
  2) Chrome 已加载 user-web/browser_automation/dist 且 SW 存活 → host/status count>0
     （改过扩展代码后必须在 chrome://extensions 点一次「重新加载」，host/status version 应为 1.5.0）
  3) 该 Chrome 内小红书/抖音/闲鱼处于登录态

写链路安全闸：post_comment 会在真实账号下留一条公开评论（他人可见、平台侧不可撤回）。
因此默认只跑读链路 + D7 的「挂起→中止」路径（证明未提交）；
真发评论必须显式 --allow-write（届时 D7 用例走「挂起→确认放行」并核验 finalize 证据）。

写腿真机闭环（零外溢）：真实平台写腿要登录态，故闸门/三段式的**设备级**语义长期只能靠 WS 测证。
本脚本另带「本地夹具腿」——在服务端进程内起一个 stdlib HTTP 夹具页，按小红书适配器的选择器契约实现
（platform 列只是选适配器的标签，无域名守卫，ValidateURL 只校验 scheme），于是不可逆点击发生在
**我们自己的页面**上：真实 Chrome + 真实 CDP trusted 输入 + 真实 prep→闸门→send→verify 全链路。
夹具页每次提交都 POST /log 回服务端，这份 ledger 是**带外预言机**——判「提交了几次、闸门放行前有没有提交」
只认它，不认页面自己说渲染了。五场景：放行 / 中止 / 超时 / 无闸门全自动 / 平台静默吞（点击落地但刻意不提交）。

本脚本的每条校验都由 `scripts/e2e_browser_reverse.py` 反向测过（翻转期望 / 抽掉证据 / 真造一次泄漏），
因为「恒真的断言」跑一万遍也是绿的——那正是 F8、F9 两次翻车的共同形状。

用法：
  python3 scripts/e2e_browser_real.py [--base-url http://127.0.0.1:8204] [--token JWT]
                                      [--allow-write] [--keep] [--only 名称子串]
                                      [--cdp-port 9333]
退出码 = FAIL 数（0 全绿；SKIP 不计失败）。
"""
import argparse
import http.server
import json
import os
import subprocess
import sys
import threading
import time
import urllib.error
import urllib.parse
import urllib.request

DEFAULT_BASE = "http://127.0.0.1:8204"
TERMINAL = {"completed", "failed", "stopped"}
COMMENT_TEXT = "这套搭配的配色很耐看，收藏了 🌿"  # 无链接无联系方式=平台低风控文本


# ---------------------------------------------------------------- HTTP 封装
class API:
    def __init__(self, base, token):
        self.base = base.rstrip("/")
        self.token = token

    def call(self, method, path, body=None, timeout=60):
        url = self.base + path
        data = json.dumps(body).encode() if body is not None else None
        req = urllib.request.Request(url, data=data, method=method)
        req.add_header("Content-Type", "application/json")
        if self.token:
            req.add_header("Authorization", "Bearer " + self.token)
        try:
            with urllib.request.urlopen(req, timeout=timeout) as r:
                return r.status, json.loads(r.read().decode() or "{}")
        except urllib.error.HTTPError as e:
            raw = e.read().decode() or "{}"
            try:
                return e.code, json.loads(raw)
            except json.JSONDecodeError:
                return e.code, {"raw": raw}
        except Exception as e:  # 连接层失败也要变成结构化结果，不让脚本半路崩
            return 0, {"error": str(e)}

    def ok(self, method, path, body=None, timeout=60):
        status, payload = self.call(method, path, body, timeout)
        return status == 200 and payload.get("code") == 0, payload


def mint_admin_token(repo_root):
    """用仓库自带的 cmd/minttoken 铸一枚本地 admin JWT（不落盘、不打印）。"""
    srv = os.path.join(repo_root, "user-server")
    if not os.path.isdir(srv):
        return ""
    try:
        out = subprocess.run(["go", "run", "./cmd/minttoken"], cwd=srv,
                             capture_output=True, text=True, timeout=180)
        return out.stdout.strip().splitlines()[-1] if out.stdout.strip() else ""
    except Exception:
        return ""


# ---------------------------------------------------------------- 结果记账
class Report:
    def __init__(self):
        self.rows = []

    def add(self, verdict, name, detail=""):
        self.rows.append((verdict, name, detail))
        print("%-5s %s%s" % (verdict, name, ("  :: " + detail) if detail else ""), flush=True)

    def count(self, verdict):
        return sum(1 for v, _, _ in self.rows if v == verdict)


# ---------------------------------------------------------------- 任务编排
def task_baseline_public():
    """真机基线腿：无登录墙页面，逐个原语都必须在真实 Chrome 上成立。

    平台腿（xhs/douyin/xianyu）的成败受真实账号登录态影响，本腿不依赖任何账号，
    用来把「传输+原语实现是否真的通」与「平台是否给了登录态」两件事分开。
    """
    return {
        "name": "E2E-真机基线读链路", "task_type": "one_shot", "platform": "",
        "url": "https://example.com/", "loop_count": 1,
        "delay_ms": 500, "timeout_sec": 180,
        "steps": [
            {"action": "open_tab", "target": "https://example.com/"},
            {"action": "wait_for_selector", "selector": "h1", "timeout_ms": 8000},
            {"action": "assert", "assert_kind": "contains_text", "value": "Example Domain", "timeout_ms": 5000},
            {"action": "query", "query_kind": "count", "target": "a"},
            {"action": "extract", "selectors": {"title": "h1"}},
            {"action": "snapshot"},
            {"action": "markdown"},
            {"action": "screenshot"},
            {"action": "scroll", "direction": "down", "amount": 300},
            {"action": "close_tab"},
        ],
    }


def task_interact_public():
    """真机交互腿（模拟真人搜索）：无账号依赖的公开站点上跑完整「键入→点击→跳转→断言」链。

    这一腿专门覆盖基线腿没碰的三件事：CDP trusted 键入、会跳转的真实点击（F6a navigated
    证据）、跳转后再定位（wait_for_selector/assert）。百度首页是纯公开表单，登录态无关。
    """
    return {
        "name": "E2E-真机交互搜索腿", "task_type": "one_shot", "platform": "",
        "url": "https://www.baidu.com/", "loop_count": 1,
        "delay_ms": 800, "timeout_sec": 180,
        "steps": [
            {"action": "open_tab", "target": "https://www.baidu.com/"},
            {"action": "wait_for_selector", "selector": "input#kw", "timeout_ms": 10000},
            {"action": "type", "target": "input#kw", "value": "浏览器自动化", "clear_first": True},
            {"action": "click", "target": "#su"},
            {"action": "wait_for_selector", "selector": "#content_left", "timeout_ms": 12000},
            {"action": "assert", "assert_kind": "contains_text", "value": "浏览器自动化", "timeout_ms": 6000},
            {"action": "query", "query_kind": "count", "target": "div.result"},
            {"action": "extract", "selectors": {"first_result": "div.result h3"}},
            {"action": "snapshot"},
            {"action": "close_tab"},
        ],
    }


def task_read_xhs():
    return {
        "name": "E2E-xhs读链路", "task_type": "one_shot", "platform": "xiaohongshu",
        "url": "https://www.xiaohongshu.com/explore", "loop_count": 1,
        "delay_ms": 1200, "timeout_sec": 180,
        "steps": [
            {"action": "open_tab", "target": "https://www.xiaohongshu.com/explore"},
            {"action": "wait_for_selector", "selector": "#root", "timeout_ms": 8000},
            {"action": "scroll", "direction": "down", "amount": 600},
            {"action": "snapshot"},
            {"action": "markdown"},
            {"action": "query", "query_kind": "count", "target": "section.note-item"},
            {"action": "close_tab"},
        ],
    }


def task_read_douyin():
    return {
        "name": "E2E-douyin读链路", "task_type": "one_shot", "platform": "douyin",
        "url": "https://www.douyin.com/?recommend=1", "loop_count": 1,
        "delay_ms": 1500, "timeout_sec": 180,
        "steps": [
            {"action": "open_tab", "target": "https://www.douyin.com/?recommend=1"},
            {"action": "wait_for_selector", "selector": "div[data-e2e=\"feed-card\"]", "timeout_ms": 10000},
            {"action": "snapshot"},
            {"action": "extract", "selectors": {"first_card": "div[data-e2e=\"feed-card\"] h2"}},
            {"action": "close_tab"},
        ],
    }


def task_read_xianyu():
    return {
        "name": "E2E-xianyu读链路", "task_type": "one_shot", "platform": "xianyu",
        "url": "https://www.goofish.com/search?q=%E6%89%8B%E6%9C%BA%E5%A3%B3", "loop_count": 1,
        "delay_ms": 1500, "timeout_sec": 180,
        "steps": [
            {"action": "open_tab", "target": "https://www.goofish.com/search?q=%E6%89%8B%E6%9C%BA%E5%A3%B3"},
            {"action": "snapshot"},
            {"action": "query", "query_kind": "exists", "target": ".search-result"},
            {"action": "close_tab"},
        ],
    }


def task_write_xhs(require_confirm):
    return {
        "name": ("E2E-xhs三段式-D7确认放行" if require_confirm else "E2E-xhs三段式全自动"),
        "task_type": "one_shot", "platform": "xiaohongshu",
        "url": "https://www.xiaohongshu.com/explore", "loop_count": 1,
        "delay_ms": 1500, "timeout_sec": 240, "require_confirm": require_confirm,
        "steps": [
            {"action": "open_tab", "target": "https://www.xiaohongshu.com/explore"},
            {"action": "wait_for_selector", "selector": "#root", "timeout_ms": 8000},
            {"action": "click", "target": "section.note-item a", "continue_on_error": True},
            {"action": "post_comment", "value": COMMENT_TEXT},
            {"action": "close_tab"},
        ],
    }


def task_brain_xhs():
    return {
        "name": "E2E-xhs-Brain只读", "task_type": "one_shot", "platform": "xiaohongshu",
        "url": "https://www.xiaohongshu.com/explore", "loop_count": 1,
        "delay_ms": 1000, "timeout_sec": 300, "brain_mode": True,
        "brain_goal": "打开小红书发现页，读出页面上任意一条笔记的标题（snapshot 或 query 即可），"
                      "不要点击任何写操作按钮、不要发表评论，拿到标题就 done。",
        "steps": [],
    }


# ---------------------------------------------------------- 本地写腿夹具（零外溢）
# 选择器契约来自服务端适配器（platform/xiaohongshu/xiaohongshu.go Locators()），夹具只是
# 把它在本地页上实现一遍——**不是桩**：适配器仍按真实路径下发，扩展仍按真实路径注入。
# 三条硬约束（源自扩展注入实现，改页面前先核 primitives.js）：
#   1) 输入框必须是 contenteditable（走 needs_trusted → CDP 逐字键入，与真实 xhs 同路径；
#      用 <textarea> 会走合成赋值，绕开拟人键入，等于没测）；
#   2) 发送按钮文本含「发送」且距输入框 ≤3 层祖先（injPostCommentSend 的 depth<4 上溯）；
#   3) 提交必须**同步**写进 .parent-comment .note-text（后台 tab 里 rAF 不触发、定时器被节流），
#      且该节点内只能有评论正文（verify 的 own 判据按去空白全等）。
# 页面静态文案绝不出现评论正文——verify 有「整页兜底搜索」，含正文就能零提交假绿。
FIXTURE_HTML = """<!doctype html><html lang="zh-CN"><head><meta charset="utf-8">
<title>写腿夹具</title><style>
body{font:14px/1.6 -apple-system;margin:16px}
#card{border:1px solid #ddd;padding:12px;max-width:520px}
.content-edit{border:1px solid #bbb;padding:8px;min-height:44px}
.bottom{margin-top:8px}.submit{padding:6px 18px}
.parent-comment{border-top:1px solid #eee;margin-top:6px}
.note-text{color:#222}.meta{color:#999;margin-left:8px}
</style></head><body>
<div id="card">
  <div class="content-edit"><p class="content-textarea" contenteditable="true"></p></div>
  <div class="bottom"><button class="submit" type="button">发送</button></div>
</div>
<div class="comments-container"><div class="comment-count">已有 <span id="n">0</span> 条提交</div></div>
<script>
// 页内事件追踪：写腿排障的唯一现场证物——「扩展到底有没有把 trusted 事件送进这个页面、
// 落在哪个坐标、命中了哪个节点」只有页面自己能回答（CDP 侧看不全，服务端只看得到回包）。
// 追踪经 /log 以 kind=trace 上行，与 kind=post 的提交记账严格分流（预言机语义不受污染）。
window.__trace = [];
const __push = (e) => window.__trace.push({
  k: e.type, x: Math.round(e.clientX), y: Math.round(e.clientY),
  t: (String(e.target && (e.target.className || e.target.tagName)) || '').slice(0, 24),
});
['pointerdown', 'mousedown', 'mouseup', 'click', 'keydown'].forEach((k) => document.addEventListener(k, __push, true));
// 触达记账（kind=touched）与 trace 分开：trace 走 1.5s 批量上行，页面可能在批量前就被回收，
// 「零触达」判据若建立在 trace 上会因竞态而恒真。input 事件同步即时上行，才是硬预言机。
// TI 存成常量而不是每次重新 querySelector：?noinput=1 腿会把整张卡片摘掉，
// 之后按选择器再查会拿到 null 而在 interval 里抛错（trace 断供），存下的节点引用照常可读。
const TI = document.querySelector('.content-textarea');
TI.addEventListener('input', function () {
  fetch('/log', {method: 'POST', headers: {'Content-Type': 'application/json'},
                 body: JSON.stringify({kind: 'touched', text: (this.innerText || '').slice(0, 40)})});
}, true);
setInterval(() => {
  if (!window.__trace.length) return;
  const batch = window.__trace.splice(0, window.__trace.length);
  fetch('/log', {method: 'POST', headers: {'Content-Type': 'application/json'},
                 body: JSON.stringify({kind: 'trace', events: batch,
                   typed: (TI.innerText || '').slice(0, 24),
                   comments: document.querySelectorAll('.parent-comment').length})});
}, 1500);
// ?inbox=1（Leg X / 容器内假绿腿）：把输入卡片整体搬进 .comments-container 里面——
// 这是小红书的真实 DOM 形态。只搬位置、不改任何提交与记账逻辑（监听器已绑在节点上，
// 随节点一起搬家），于是判据仍然只有带外 ledger：会话若照样 completed+verified=true，
// 就是「未提交草稿被容器主分支当成已发布评论」的假绿。
if (/[?&]inbox=1/.test(location.search)) {
  var cc = document.querySelector('.comments-container');
  cc.insertBefore(document.getElementById('card'), cc.firstChild);
}
document.querySelector('.submit').addEventListener('click', function () {
  var t = (document.querySelector('.content-textarea').innerText || '').trim();
  if (!t) return;
  // swallow 模式复现「平台静默吞」：本函数被调用即证明 trusted 点击已落到按钮上，
  // 但页面不产出评论 DOM、草稿原地留在输入框里——finalize 若还报 verified=true 就是假绿。
  if (/[?&]swallow=1/.test(location.search)) {
    fetch('/log', {method: 'POST', headers: {'Content-Type': 'application/json'},
                   body: JSON.stringify({kind: 'swallowed', text: t})});
    return;
  }
  var row = document.createElement('div');
  row.className = 'parent-comment';
  var body = document.createElement('span'); body.className = 'note-text'; body.textContent = t;
  var meta = document.createElement('span'); meta.className = 'meta'; meta.textContent = '刚刚';
  row.appendChild(body); row.appendChild(meta);
  document.querySelector('.comments-container').appendChild(row);  // 同步入 DOM
  var c = document.getElementById('n'); c.textContent = (+c.textContent + 1);
  fetch('/log', {method: 'POST', headers: {'Content-Type': 'application/json'},
                 body: JSON.stringify({kind: 'post', text: t})});    // 带外预言机
});
// ?noinput=1（批7「从未发生」腿）：把评论卡片整体摘除，只留评论区容器。
// 派生写步（type 命中平台注册的 comment_input）于是必然 element_not_found——
// 「从未在页面上发生」的动作不许记成提交尝试，否则用户改好选择器重跑仍被闸门拦死。
// 摘除排在所有监听器注册之后，前面的记账逻辑一行不动（判据只多不少）。
if (/[?&]noinput=1/.test(location.search)) document.getElementById('card').remove();
</script></body></html>"""


class Fixture:
    """夹具服务：GET / 出页面，POST /log 记账，GET /ledger 读账（预言机在脚本侧，不在页面侧）。"""

    def __init__(self):
        self.rows = []
        outer = self

        class Handler(http.server.BaseHTTPRequestHandler):
            def log_message(self, *_a):
                pass

            def _send(self, code, body, ctype):
                raw = body.encode()
                self.send_response(code)
                self.send_header("Content-Type", ctype)
                self.send_header("Content-Length", str(len(raw)))
                self.end_headers()
                self.wfile.write(raw)

            def do_GET(self):
                path = urllib.parse.urlparse(self.path).path
                if path == "/ledger":
                    self._send(200, json.dumps({"rows": outer.rows}), "application/json")
                elif path == "/healthz":
                    self._send(200, "ok", "text/plain")
                else:
                    self._send(200, FIXTURE_HTML, "text/html; charset=utf-8")

            def do_POST(self):
                n = int(self.headers.get("Content-Length") or 0)
                try:
                    rec = json.loads(self.rfile.read(n).decode() or "{}")
                except json.JSONDecodeError:
                    rec = {}
                outer.rows.append({
                    "kind": str(rec.get("kind") or "post"),
                    "text": str(rec.get("text") or ""),
                    "events": rec.get("events") or [],
                    "typed": str(rec.get("typed") or ""),
                    "comments": rec.get("comments"),
                    "ts": round(time.time(), 3),
                })
                self._send(200, json.dumps({"ok": True, "n": len(outer.rows)}), "application/json")

        self.srv = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.port = self.srv.server_address[1]
        threading.Thread(target=self.srv.serve_forever, daemon=True).start()

    @property
    def url(self):
        return "http://127.0.0.1:%d/" % self.port

    def submissions(self, nonce):
        """带外预言机：只认 kind=post 的提交记账（trace 上行不得混入）。"""
        return [r for r in self.rows if r["kind"] == "post" and nonce in r["text"]]

    def trace(self):
        """页内事件流（排障用，非判据）。"""
        return [r for r in self.rows if r["kind"] == "trace"]

    def swallowed(self, nonce):
        """swallow 腿正判据：点击已落到按钮（处理器被调用）但页面刻意未提交。"""
        return [r for r in self.rows if r["kind"] == "swallowed" and nonce in r["text"]]

    def touched(self, nonce=""):
        """零触达判据：输入框收到过 input（=trusted 键入真的进过页面），同步上行无批量竞态。
        逐字键入的中间帧只含半截文本，故默认不按 nonce 过滤（给了才过滤，用于「末帧含全文」的正控）。
        """
        rows = [r for r in self.rows if r["kind"] == "touched"]
        return [r for r in rows if nonce in r["text"]] if nonce else rows

    def reset(self):
        self.rows.clear()

    def stop(self):
        self.srv.shutdown()
        self.srv.server_close()


F10_DRAIN_BUDGET_SEC = 12


def cdp_fixture_tabs(cdp_port, fixture_url):
    with urllib.request.urlopen("http://127.0.0.1:%d/json/list" % cdp_port, timeout=10) as r:
        targets = json.loads(r.read().decode())
    return [t["id"] for t in targets
            if t.get("type") == "page" and str(t.get("url", "")).startswith(fixture_url) and t.get("id")]


def cdp_close_fixture_tabs(rep, name, cdp_port, fixture_url):
    """F10 的 CDP 半边锁：审计帧只证明「回收被记录过」，tab 真的消失才算闭环。
    中止/超时腿没有 close_tab 步，泄漏的是 tab + chrome.debugger 附着，逐腿必须归零。
    只按夹具 URL 前缀匹配，其他 tab（含用户真实页面）一律不碰。"""
    if not cdp_port:
        rep.add("WARN", name + " :: 夹具 tab 零泄漏", "未给 --cdp-port，泄漏的 tab 需手动确认")
        return
    deadline = time.time() + F10_DRAIN_BUDGET_SEC
    while True:
        try:
            left = cdp_fixture_tabs(cdp_port, fixture_url)
        except Exception as e:
            rep.add("WARN", name + " :: 夹具 tab 零泄漏", "CDP 不可达 %s" % str(e)[:120])
            return
        if not left or time.time() >= deadline:
            break
        time.sleep(1)
    for tid in left:  # 判红也兜底回收，不把泄漏留给下一腿
        try:
            urllib.request.urlopen("http://127.0.0.1:%d/json/close/%s" % (cdp_port, tid),
                                   timeout=10).read()
        except Exception:
            pass
    rep.add("PASS" if not left else "FAIL", name + " :: 夹具 tab 零泄漏",
            "残留=%d%s" % (len(left),
                           "（%ds 内未自行关闭，已兜底回收）" % F10_DRAIN_BUDGET_SEC if left else ""))


def task_write_fixture(fixture, require_confirm, nonce, timeout_sec, swallow=False, inbox=False):
    """夹具写腿：url 与 open_tab 都指向本地夹具页，platform 标签仍用 xiaohongshu（选适配器）。

    timeout_sec 由场景决定：放行/中止腿要留足预算，超时腿必须显著小于轮询预算才能稳定撞闸。
    delay_ms=0 关掉步间 ±30% 随机抖动（确定性优先，拟人时序仍由扩展内部 TIMING 负责）。
    swallow=True 走「平台静默吞」变体：点击落地但页面刻意不提交，专测 finalize 的假绿防线。
    inbox=True 把输入卡片搬进评论区容器内（小红书真实形态）；与 swallow 合用即 Leg X——
    未提交的草稿就在「已发布评论」该出现的地方，verify 若还认它就是容器主分支假绿。
    """
    qs = ("swallow=1" if swallow else "") + ("&" if swallow and inbox else "") + ("inbox=1" if inbox else "")
    page = fixture.url + ("?" + qs if qs else "")
    return {
        "name": "E2E-夹具写腿-%s" % nonce,
        "task_type": "one_shot", "platform": "xiaohongshu",
        "url": page, "loop_count": 1,
        "delay_ms": 0, "timeout_sec": timeout_sec, "require_confirm": require_confirm,
        "steps": [
            {"action": "open_tab", "target": page},
            {"action": "wait_for_selector", "selector": ".comments-container", "timeout_ms": 2000},
            {"action": "post_comment", "value": "夹具写腿链路验证 %s" % nonce},
            {"action": "close_tab"},
        ],
    }, "夹具写腿链路验证 %s" % nonce


def task_derived_write_fixture(fixture, nonce, timeout_sec, noinput=False):
    """批7 派生写腿：编排里**没有** post_comment，「不可逆写」由 type 命中评论框 + 回车表达。

    这正是批7 之前的盲区：旧的写原语判定只认 action 名，这类「隐形提交步」既有重试资格、
    又不进台账、双发闸对它视而不见。夹具页刻意不绑 Enter 处理器 → 键入回车永远不会真的提交，
    于是带外 ledger 恒 0，本腿判据全落在「服务端怎么给这一步记账」上（is_write / submit_state）。
    noinput=True 切到 ?noinput=1 形态（评论框被摘除）→ 定位必然失败 = 动作从未发生。
    """
    page = fixture.url + ("?noinput=1" if noinput else "")
    return {
        "name": "E2E-夹具派生写步-%s" % nonce,
        "task_type": "one_shot", "platform": "xiaohongshu",
        "url": page, "loop_count": 1,
        "delay_ms": 0, "timeout_sec": timeout_sec, "require_confirm": False,
        "steps": [
            {"action": "open_tab", "target": page},
            {"action": "wait_for_selector", "selector": ".comments-container", "timeout_ms": 2000},
            {"action": "type", "target": ".content-textarea", "value": "派生写步链路验证 %s" % nonce,
             "submit_on_enter": True},
            {"action": "close_tab"},
        ],
    }, "派生写步链路验证 %s" % nonce


# ---------------------------------------------------------------- 执行与观测
WALL_PATTERNS = ("wappass.baidu.com", "passport.", "/captcha", "verify.", "checkcode",
                 "anquan", "risk", "/login")


def server_side_wall(err, logs):
    """服务端自己判出的风控墙：error_msg 含「平台风控拦截」且确有 blocked=true 的探测帧背书。
    这条归因比脚本的启发式更硬——判据来自平台适配器的选择器命中，不是我们猜 URL。
    真机实测：xhs 对当前 IP 直接 300012「IP存在风险」→ 会话被这样终止（session 319）。
    """
    if "平台风控拦截" not in err:
        return ""
    hits = [p for p in (_payload(l) for l in (logs or []) if l.get("action") == "block_detect")
            if p.get("blocked")]
    if not hits:
        return ""
    return "服务端探测器命中（blocked=true）→ 归因=平台风控墙 :: %s" % str(hits[-1].get("url"))[:110]


def wall_evidence(logs, task_payload):
    """第三方反爬墙判据。两条**同时**成立才认定「被站点挡住」，缺一就按本链路故障判红：
      1) 点击之后的探测帧 URL 里带上了我们键入的词 → 键入/点击/跳转 三步真的成了；
      2) 之后的 URL 命中已知验证/登录页特征 → 是站点把我们挡在结果页外。
    把「平台设墙」与「我们的链路坏了」混成同一个 FAIL，会把下一位排障的人带去改错代码。
    """
    kw = next((str(s.get("value") or "") for s in (task_payload or {}).get("steps", [])
               if s.get("action") == "type"), "")
    if not kw:
        return ""
    urls = [_payload(l).get("url") or "" for l in logs or [] if l.get("action") == "block_detect"]
    nav = [u for u in urls if kw in u or urllib.parse.quote(kw) in u]
    walls = [u for u in urls if any(p in u for p in WALL_PATTERNS)]
    if not nav or not walls:
        return ""
    return ("归因=第三方验证墙（键入/点击/跳转 三步已证成，非本链路故障）："
            "键入词已在跳转 URL（%d 帧）→ 随后被验证页挡住：%s" % (len(nav), walls[-1][:110]))


def fetch_audit(api, session_id):
    ok, got = api.ok("GET", "/api/browser-automation/sessions/%s/export" % session_id)
    return (got.get("data") or {}) if ok else None


def run_and_watch(api, rep, task_payload, name, allow_write, created_ids, step_timeout=300,
                  gate=None, fixture=None, cdp_port=0):
    """建任务→发布→执行→轮询终态→审计核验。返回 (session_id, status)。

    gate=None 保持历史语义（真平台腿：completed=PASS、stopped=WARN、其余 FAIL，闸门动作看
    --allow-write）。给了 gate 就按期望判——夹具写腿用「终态 + 错误文案 + ledger 计数 + 证据」
    四件事一次锁死，光看终态会把「没提交」和「提交了」判成同一个结果。
    """
    gate = gate or {}
    okc, created = api.ok("POST", "/api/browser-automation/tasks", task_payload)
    if not okc:
        rep.add("FAIL", name + " :: 建任务", json.dumps(created, ensure_ascii=False)[:200])
        return None, None
    task_id = created["data"]["id"]
    created_ids.append(task_id)
    api.ok("POST", "/api/browser-automation/tasks/%s/publish" % task_id)

    okr, ran = api.ok("POST", "/api/browser-automation/tasks/%s/run" % task_id, {})
    if not okr:
        msg = str(ran.get("message", ran))
        rep.add("FAIL", name + " :: 下发", msg[:200])
        return None, None
    session_id = ran["data"]["session_id"]

    if fixture is not None:
        fixture.reset()  # nonce 逐腿唯一，reset 只让 ledger 读数干净
    sess = poll_session(api, rep, session_id, step_timeout, task_payload, allow_write, gate, fixture,
                        name)
    status = (sess or {}).get("status")
    err = str((sess or {}).get("error_msg") or "")
    want_status = gate.get("expect_status", "completed")
    audit = fetch_audit(api, session_id)
    logs = ((audit or {}).get("command_log") or [])
    detail = "session=%s status=%s want=%s err=%s" % (session_id, status, want_status, err[:160])
    if gate:
        verdict = "PASS" if status == want_status else "FAIL"
    else:
        verdict = "PASS" if status == "completed" else ("WARN" if status == "stopped" else "FAIL")
        if verdict == "FAIL":
            wall = server_side_wall(err, logs) or wall_evidence(logs, task_payload)
            if wall:
                verdict, detail = "WARN", detail + " || " + wall
    rep.add(verdict, name + " :: 终态", detail)
    if gate.get("expect_err") and gate["expect_err"] not in err:
        rep.add("FAIL", name + " :: 错误文案", "期望含「%s」实得「%s」" % (gate["expect_err"], err[:160]))
    if "ledger" in gate:
        subs = fixture.submissions(gate["nonce"]) if fixture else []
        rep.add("PASS" if len(subs) == gate["ledger"] else "FAIL",
                name + " :: 夹具 ledger（带外预言机）",
                "nonce=%s 提交=%d 期望=%d%s" % (gate["nonce"], len(subs), gate["ledger"],
                                               "（>期望=双发）" if len(subs) > gate["ledger"] else ""))
    audit_check(rep, name, audit, gate, fixture)
    if gate.get("cleanup_fixture_tabs") and fixture is not None:
        cdp_close_fixture_tabs(rep, name, cdp_port, fixture.url)
    return session_id, sess


def run_resubmit_gate_leg(api, rep, task_payload, name, nonce, fixture, created_ids, cdp_port, gate):
    """台账双发闸的设备级锁：同一任务连跑两次，第二次必须在「任何页面注入之前」被拦下。

    首跑走标准夹具腿（终态 + ledger + 证据 + tab 回收一次锁死）；二次运行只认四件事：
    终态 failed、拒绝文案、带外 ledger 不再增长、页内零 trusted 触达。
    「零触达」必须配一条同预言机的正控（首跑要有触达帧），否则恒真的判据等于没判。
    """
    sid, sess = run_and_watch(api, rep, task_payload, name + "（首跑）", False, created_ids,
                              gate=gate, fixture=fixture, cdp_port=cdp_port)
    touched1 = fixture.touched()
    rep.add("PASS" if touched1 else "FAIL", name + " :: 首跑页内有触达（零触达判据的正控）",
            "input 帧=%d 期望≥1（=0 说明预言机是死的，后面的零触达不可信）" % len(touched1))
    task_id = (sess or {}).get("task_id")
    if not task_id:
        rep.add("FAIL", name + " :: 二次运行", "首跑未回读到 task_id（sid=%s）" % sid)
        return
    fixture.reset()  # 二次运行的判据由此变成「ledger 必须恒 0」，双发一眼可辨
    okr, ran = api.ok("POST", "/api/browser-automation/tasks/%s/run" % task_id, {})
    if not okr or not (ran.get("data") or {}).get("session_id"):
        rep.add("FAIL", name + " :: 二次运行下发", json.dumps(ran, ensure_ascii=False)[:200])
        return
    sid2 = ran["data"]["session_id"]
    sess2 = poll_session(api, rep, sid2, 60, task_payload, False, {}, fixture, name) or {}
    st2 = sess2.get("status")
    err2 = str(sess2.get("error_msg") or "")
    rep.add("PASS" if st2 == "failed" else "FAIL", name + " :: 二次运行终态",
            "session=%s status=%s 期望=failed（台账闸拦截）err=%s" % (sid2, st2, err2[:160]))
    rep.add("PASS" if "拒绝执行" in err2 else "FAIL", name + " :: 二次运行文案",
            "期望含「拒绝执行」实得「%s」" % err2[:160])
    subs = len(fixture.submissions(nonce))
    rep.add("PASS" if subs == 0 else "FAIL", name + " :: 二次运行零提交",
            "ledger=%d 期望=0（>0 即双发，闸门失效）" % subs)
    # 页侧带外零触达：闸门若排在注入之后，输入框必然收过 trusted 键入（input 同步上行，无批量竞态）。
    # 不用 command_log 当这个判据——步级 command/event 是执行器记账，与「帧有没有送到页面」无关
    # （真机 session357 实测：被拦下的那次运行照样留有一对 post_comment command/event，event 带拒绝文案）。
    left = fixture.touched()
    rep.add("PASS" if not left else "FAIL", name + " :: 二次运行页内零触达",
            "input 帧=%d 草稿=%s 期望=0（闸门必须排在任何注入之前）" % (
                len(left), (left[0]["text"] if left else "")[:24]))
    cdp_close_fixture_tabs(rep, name, cdp_port, fixture.url)


# ------------------------------------------------- 批7：写步属性化 / 豁免 设备级锁
def _b7_step(audit, idx):
    """审计包里按 step_index 取那一行。取不到回 {}，让上层以「字段缺失」判红而不是抛异常。"""
    for s in (audit or {}).get("steps") or []:
        if s.get("step_index") == idx:
            return s
    return {}


def _b7_rerun(api, rep, name, task_id, task_payload, fixture):
    """同任务二次下发（人工重跑语义：RetryCount 归 0，闸门不许豁免）。返回 (sid, 终态 dict)。"""
    okr, ran = api.ok("POST", "/api/browser-automation/tasks/%s/run" % task_id, {})
    sid = (ran.get("data") or {}).get("session_id") if okr else None
    if not sid:
        rep.add("FAIL", name + " :: 二次运行下发", json.dumps(ran, ensure_ascii=False)[:200])
        return None, {}
    return sid, (poll_session(api, rep, sid, 90, task_payload, False, {}, fixture, name) or {})


def run_derived_write_leg(api, rep, task_payload, name, nonce, fixture, created_ids, cdp_port):
    """批7 属性化的设备级锁：编排里没有 post_comment 的「隐形写步」也进台账、也被闸门拦。

    夹具页刻意不绑 Enter 处理器 → 页面永远不会提交，所以「ledger 恒 0」在这里是**期望值**。
    要证的是服务端怎么给这一步记账（is_write 落库 + submit_state=sent），以及第二次运行
    一个帧都没下发。旧的判定只认 action 名，这条腿在改前必红（is_write=False、无台账、二轮照发）。
    """
    gate = {"action": "none", "expect_status": "completed", "ledger": 0, "nonce": nonce,
            "cleanup_fixture_tabs": True}
    sid, sess = run_and_watch(api, rep, task_payload, name, False, created_ids,
                              gate=gate, fixture=fixture, cdp_port=cdp_port)
    row = _b7_step(fetch_audit(api, sid), 2)
    rep.add("PASS" if row.get("is_write") else "FAIL", name + " :: 派生写判定落库",
            "type+回车命中注册 comment_input → is_write=%s（只认 action 名的旧判定这里必为假）"
            % row.get("is_write"))
    rep.add("PASS" if row.get("submit_state") == "sent" else "FAIL", name + " :: 派生写台账",
            "submit_state=%s want sent（这类原语没有回查通路，记成 verified 就是又一处假绿）"
            % row.get("submit_state"))
    touched1 = fixture.touched()
    rep.add("PASS" if touched1 else "FAIL", name + " :: 首跑页内有触达（零触达判据的正控）",
            "input 帧=%d 期望≥1（=0 说明预言机是死的，后面的零触达不可信）" % len(touched1))
    task_id = (sess or {}).get("task_id")
    if not task_id:
        rep.add("FAIL", name + " :: 二次运行", "首跑未回读到 task_id（sid=%s）" % sid)
    else:
        fixture.reset()
        sid2, sess2 = _b7_rerun(api, rep, name, task_id, task_payload, fixture)
        err2 = str(sess2.get("error_msg") or "")
        rep.add("PASS" if sess2.get("status") == "failed" else "FAIL", name + " :: 二次运行终态",
                "session=%s status=%s 期望=failed（派生写步同样受双发闸保护）err=%s" % (
                    sid2, sess2.get("status"), err2[:160]))
        rep.add("PASS" if "拒绝执行" in err2 else "FAIL", name + " :: 二次运行文案",
                "期望含「拒绝执行」实得「%s」" % err2[:160])
        left = fixture.touched()
        rep.add("PASS" if not left else "FAIL", name + " :: 二次运行页内零触达",
                "input 帧=%d 草稿=%s 期望=0（闸门排在任何注入之前）" % (
                    len(left), (left[0]["text"] if left else "")[:24]))
    cdp_close_fixture_tabs(rep, name, cdp_port, fixture.url)


def run_never_executed_write_leg(api, rep, task_payload, name, nonce, fixture, created_ids, cdp_port):
    """批7 台账留空的设备级锁——**过拦**的反向测试（判据与上一条相反）。

    ?noinput=1 页面上评论框被摘除，派生写步必然 element_not_found：动作从未在页面上发生，
    台账就必须留空，第二次运行才还能照常跑到同一个定位失败上。若把它记成提交尝试，用户
    改好选择器重跑会被一个从没发生过的动作永久拦死、只能换新任务——过拦的代价是真机可感的。
    """
    gate = {"action": "none", "expect_status": "failed", "expect_err": "element_not_found",
            "ledger": 0, "nonce": nonce, "cleanup_fixture_tabs": True}
    sid, sess = run_and_watch(api, rep, task_payload, name, False, created_ids,
                              gate=gate, fixture=fixture, cdp_port=cdp_port)
    row = _b7_step(fetch_audit(api, sid), 2)
    rep.add("PASS" if row.get("is_write") else "FAIL", name + " :: 失败步仍算写步",
            "is_write=%s（写判定看编排，不看这一轮的结果）" % row.get("is_write"))
    rep.add("PASS" if not row.get("submit_state") else "FAIL", name + " :: 从未发生不记尝试",
            "submit_state=%r 期望空（记成 sent/unattributed 就把一次干净的定位失败永久拦死）"
            % row.get("submit_state"))
    rep.add("PASS" if not fixture.touched() else "FAIL", name + " :: 首跑页内零触达",
            "input 帧=%d 期望=0（元素不存在时连键入都不该落地）" % len(fixture.touched()))
    task_id = (sess or {}).get("task_id")
    if not task_id:
        rep.add("FAIL", name + " :: 二次运行", "首跑未回读到 task_id（sid=%s）" % sid)
    else:
        fixture.reset()
        sid2, sess2 = _b7_rerun(api, rep, name, task_id, task_payload, fixture)
        err2 = str(sess2.get("error_msg") or "")
        rep.add("PASS" if "element_not_found" in err2 else "FAIL", name + " :: 二次运行仍失败在定位",
                "status=%s err=%s 期望=element_not_found" % (sess2.get("status"), err2[:160]))
        rep.add("PASS" if "拒绝执行" not in err2 else "FAIL", name + " :: 二次运行未被误拦",
                "实得「%s」期望不含拒绝文案（台账留空才允许改好选择器后原任务重跑）" % err2[:160])
    cdp_close_fixture_tabs(rep, name, cdp_port, fixture.url)


B7_RETRY_SCAN_BUDGET_SEC = 240  # retry_delay 30s + 扫描器 60s tick + 一轮执行 + 余量


def _b7_wait_retry_session(api, task_id, first_sid, timeout=B7_RETRY_SCAN_BUDGET_SEC):
    """等任务级自动重试轮真的起来并收口。sessions 列表按 id 倒序，取本任务除首轮外的最新一条。"""
    deadline = time.time() + timeout
    while time.time() < deadline:
        ok, got = api.ok("GET", "/api/browser-automation/sessions?page=1&limit=50")
        rows = ((got.get("data") or {}).get("list") or []) if ok else []
        cands = [s for s in rows
                 if s.get("task_id") == task_id and s.get("id") != first_sid]
        if cands and cands[0].get("status") in TERMINAL:
            return cands[0]
        time.sleep(3)
    return None


def run_retry_skip_write_leg(api, rep, task_payload, name, nonce, fixture, created_ids, cdp_port):
    """批7 重试豁免的设备级锁，两头都判：不重发（防双发）+ 不判绿（防假绿）。

    首轮走 swallow 页 → 点击落地但页面不提交 → 台账停在 unattributed、会话 failed →
    任务级自动重试（retry_on_fail）起来后，第二轮遇到这条「已尝试但未验证」的写步：
      · 必须整步跳过——页侧零 touched / 零 swallowed / 审计里连一条 post_comment 命令帧都不该有；
      · 但**不许**因此换来一个 completed 会话。本轮什么都没证明，判绿就是批6 立项要消灭的假绿。
    豁免的意义仍在：只读步照常重放（success_steps≥2），重试出得了循环而不必重发评论。
    """
    gate = {"action": "none", "expect_status": "failed", "expect_err": "验证未通过",
            "ledger": 0, "nonce": nonce, "evidence": True, "verified": False,
            "cleanup_fixture_tabs": True}
    sid, sess = run_and_watch(api, rep, task_payload, name + "（首轮）", False, created_ids,
                              gate=gate, fixture=fixture, cdp_port=cdp_port)
    audit1 = fetch_audit(api, sid)
    row1 = _b7_step(audit1, 2)
    task_id = (sess or {}).get("task_id")
    if row1.get("submit_state") != "unattributed":
        rep.add("FAIL", name + " :: 首轮台账=unattributed",
                "实得 %r（前置条件不成立，后面的豁免判据无意义）" % row1.get("submit_state"))
    sw1 = len(fixture.swallowed(nonce))
    rep.add("PASS" if sw1 == 1 else "FAIL", name + " :: 首轮点击落地（重发判据的正控）",
            "swallowed=%d 期望=1（=0 说明页面根本没被点到，后面的「第二轮没点」是恒真判据）" % sw1)
    if not task_id:
        rep.add("FAIL", name + " :: 自动重试轮", "首轮未回读到 task_id（sid=%s）" % sid)
        cdp_close_fixture_tabs(rep, name, cdp_port, fixture.url)
        return
    fixture.reset()  # 从此页侧读数只属于第二轮
    s2 = _b7_wait_retry_session(api, task_id, sid)
    if not s2:
        rep.add("FAIL", name + " :: 自动重试轮起来",
                "%ds 内没等到第二条 session（重试扫描器/next_retry_at 链路故障）" % B7_RETRY_SCAN_BUDGET_SEC)
        cdp_close_fixture_tabs(rep, name, cdp_port, fixture.url)
        return
    err2 = str(s2.get("error_msg") or "")
    rep.add("PASS" if s2.get("status") == "failed" else "FAIL", name + " :: 重试轮终态",
            "session=%s status=%s 期望=failed（未验证的提交不能靠跳过换绿）err=%s" % (
                s2.get("id"), s2.get("status"), err2[:160]))
    rep.add("PASS" if "无法证明" in err2 else "FAIL", name + " :: 重试轮判红归因",
            "期望含「无法证明」实得「%s」" % err2[:160])
    audit2 = fetch_audit(api, s2.get("id"))
    row2 = _b7_step(audit2, 2)
    rep.add("PASS" if row2.get("status") == "skipped" else "FAIL", name + " :: 重试轮写步跳过",
            "status=%s 期望=skipped err=%s" % (row2.get("status"), str(row2.get("error_msg"))[:120]))
    logs2 = (audit2 or {}).get("command_log") or []
    n_cmd2 = len([l for l in logs2
                  if l.get("action") == "post_comment" and l.get("direction") == "command"])
    rep.add("PASS" if n_cmd2 == 0 else "FAIL", name + " :: 重试轮零下发（服务端记账面）",
            "post_comment 命令帧=%d 期望=0（闸门必须排在命令记账与下发之前）" % n_cmd2)
    rep.add("PASS" if not fixture.touched() else "FAIL", name + " :: 重试轮页内零触达",
            "input 帧=%d 期望=0" % len(fixture.touched()))
    rep.add("PASS" if not fixture.swallowed(nonce) else "FAIL", name + " :: 重试轮零二次点击",
            "swallowed=%d 期望=0（>0 即双发）" % len(fixture.swallowed(nonce)))
    rep.add("PASS" if (s2.get("success_steps") or 0) >= 2 else "FAIL", name + " :: 只读步照常重放",
            "success_steps=%s 期望≥2（open_tab+wait_for_selector 跑完了才收口）"
            % s2.get("success_steps"))
    cdp_close_fixture_tabs(rep, name, cdp_port, fixture.url)


def poll_session(api, rep, session_id, timeout, task_payload, allow_write, gate=None, fixture=None,
                 name=""):
    """监测循环：读 session 详情；D7 挂起时决定放行/中止/不动。

    gate["action"] 优先于 --allow-write（夹具腿要能主动选 none 以复现「确认超时未放行」）。
    放行前必须先取一次 ledger——「闸门未放行就没有提交」才是这条腿真正要证的事。
    """
    gate = gate or {}
    action = gate.get("action") or ("confirm" if allow_write else "stop")
    deadline = time.time() + timeout
    decided = False
    last = None
    while time.time() < deadline:
        ok, got = api.ok("GET", "/api/browser-automation/sessions/%s" % session_id)
        if not ok:
            time.sleep(2)
            continue
        last = got.get("data") or {}
        if task_payload.get("require_confirm") and not decided and last.get("confirm_pending"):
            decided = True
            nonce = gate.get("nonce")
            if fixture is not None and nonce:
                pre = len(fixture.submissions(nonce))
                rep.add("PASS" if pre == 0 else "FAIL", name + " :: 挂起期零提交",
                        "confirm_pending=true 时 ledger=%d（必须 0）" % pre)
            if action == "confirm":
                okc, conf = api.ok("POST", "/api/browser-automation/sessions/%s/confirm" % session_id, {})
                print("      D7 放行 → confirmed=%s (%s)" % (
                    (conf.get("data") or {}).get("confirmed"), conf.get("message")), flush=True)
                if not okc:
                    return last
            elif action == "stop":
                api.ok("POST", "/api/browser-automation/sessions/%s/stop" % session_id,
                       {"reason": "e2e：未授权真发评论，确认点前中止"})
                print("      D7 未授权写 → 已在确认点前中止", flush=True)
            else:
                print("      D7 不动闸门 → 等任务预算耗尽（确认超时腿）", flush=True)
        if last.get("status") in TERMINAL:
            return last
        time.sleep(2)
    return last


def _payload(frame):
    """审计帧的 payload 落库形态在 dict / JSON 串之间漂移过（导出口径），断言前先归一。"""
    p = (frame or {}).get("payload")
    if isinstance(p, str):
        try:
            p = json.loads(p)
        except ValueError:
            return {}
    return p if isinstance(p, dict) else {}


def audit_check(rep, name, d, gate=None, fixture=None):
    """I5 审计包逐项核验：会话+步+命令流齐备，步数与指标一致。

    `d` 由调用方取一次（run_and_watch 也要用同一份帧做归因），此处不再重复请求导出。
    """
    gate = gate or {}
    if d is None:
        rep.add("FAIL", name + " :: 审计导出", "导出不可读（见上一行）")
        return
    missing = [k for k in ("session", "steps", "command_log", "llm_plans", "exported_at") if k not in d]
    if missing:
        rep.add("FAIL", name + " :: 审计包字段", "缺 " + ",".join(missing))
        return
    steps = d["steps"] or []
    logs = d["command_log"] or []
    rep.add("PASS" if steps and logs else "FAIL", name + " :: 审计包完整性",
            "steps=%d command_log=%d llm_plans=%d metrics=%s/%s/%s" % (
                len(steps), len(logs), len(d["llm_plans"] or []),
                d["session"].get("success_steps"), d["session"].get("failed_steps"),
                d["session"].get("total_steps")))
    # 写链路：post_comment 证据必须落 extracted_data（finalize 归因可追溯）
    if any(s.get("action") == "post_comment" for s in steps):
        raw = (d["session"] or {}).get("extracted_data")
        ev = {}
        try:
            ev = json.loads(raw) if isinstance(raw, str) else (raw or {})
        except json.JSONDecodeError:
            ev = {}
        pcs = ev.get("post_comment") or []
        if not pcs:
            # 夹具腿的「未放行」场景**要求**无证据：闸门拦在提交点前，留了证据就是没拦住
            if gate.get("evidence") is False:
                rep.add("PASS", name + " :: 写证据", "闸门未放行 → 无 post_comment 证据（符合期望）")
            else:
                rep.add("FAIL", name + " :: 写证据", "extracted_data 无 post_comment 证据")
        elif gate.get("evidence") is False:
            rep.add("FAIL", name + " :: 写证据",
                    "期望未提交却留下证据：%s" % json.dumps(pcs[-1], ensure_ascii=False)[:200])
        else:
            last = pcs[-1] if isinstance(pcs, list) else pcs
            detail = "verified=%s send_error=%s" % (last.get("verified"), last.get("send_error"))
            # 证据是双层信封：扩展回 {ok,posted,verified,evidence:{容器/条目命中}}，finalize 原样入库。
            # 必须剥到内层——读外层则 own/containers 恒为缺省，真假绿判据形同虚设（真机实测踩过）。
            outer = last.get("evidence") or {}
            evi = outer.get("evidence") if isinstance(outer.get("evidence"), dict) else outer
            detail += " evidence=%s" % json.dumps(evi, ensure_ascii=False)[:160]
            if gate.get("verified", True):
                # 假绿防护：只有「容器命中 + 恰等本文」才证明是**我们这条**评论
                fine = bool(last.get("verified")) and evi.get("containers", 0) >= 1 and evi.get("own") is True
            else:
                # 静默吞腿（F11b 反向锁）：草稿仍留在输入框里，verified 必须为 false、
                # 且不得出现恰等本文的条目命中——报了 true 就是整页兜底假绿回归。
                fine = (last.get("verified") is False and evi.get("own") is not True
                        and not last.get("send_error"))
                if fine and fixture is not None:
                    swallowed = len(fixture.swallowed(gate.get("nonce", "")))
                    fine = swallowed == 1
                    detail += " 点击落地=%d（须 1：证明是页面吞了而非没点到）" % swallowed
            rep.add("PASS" if fine else "FAIL", name + " :: 写证据", detail)
    # 步序红线：三段式必须 prep 早于 send（命令流按 seq 单调）
    seqs = [(l.get("seq"), l.get("direction"), l.get("action")) for l in logs if l.get("action")]
    pc = [s for s in seqs if s[2] == "post_comment"]
    if pc and len(pc) > 4:
        rep.add("WARN", name + " :: 写步命令数", "post_comment 命令帧 %d 条（重试=双发风险面）" % len(pc))
    if not gate:
        return

    # F10 唯一性（审计半边）：一条腿的 tab 回收途径必须恰好一条。
    # 跑完 close_tab 的腿：会话结束时 ChromeTabID 已归零，兜底回收若再补一刀就是双关；
    # 中止/超时腿：没有 close_tab 步，只有兜底回收能关掉 tab + 解除 debugger 附着。
    want_close = gate.get("expect_status") == "completed"
    n_cmd = len([l for l in logs if l.get("action") == "close_tab" and l.get("direction") == "command"])
    n_evt = len([l for l in logs if l.get("action") == "close_tab" and l.get("direction") == "event"])
    cleanups = [l for l in logs if l.get("action") == "session_tab_cleanup"]
    tid_ok = [l for l in cleanups if l.get("ok") and _payload(l).get("tab_id")]
    if want_close:
        fine = n_cmd == 1 and n_evt == 1 and not cleanups
    else:
        fine = n_cmd == 0 and len(cleanups) == 1 and len(tid_ok) == 1
    rep.add("PASS" if fine else "FAIL", name + " :: F10 tab 回收唯一性",
            "close_tab(命令/事件)=%d/%d 兜底回收=%d(带 tab_id 且 ok=%d) 期望=%s" % (
                n_cmd, n_evt, len(cleanups), len(tid_ok),
                "1/1 + 无兜底" if want_close else "0 + 恰好 1 次兜底"))

    # F11c：干净页面上的探测帧必须是「探测成功且未拦截」。修复前无 tab 时先发探测再判空，
    # 帧里 ok=false，既污染审计又让「页面正常」被误读成「检测失败」。
    probes = [l for l in logs if l.get("action") == "block_detect"]
    bad = [l for l in probes if not l.get("ok") or _payload(l).get("blocked")]
    rep.add("PASS" if probes and not bad else "FAIL", name + " :: F11c 探测帧语义",
            "block_detect=%d 异常帧=%d%s" % (len(probes), len(bad),
                                             "（空页无帧=探测未落地）" if not probes else ""))


# ---------------------------------------------------------------- preflight
def preflight(api, rep, created_ids):
    """不依赖 Host 的契约项：无论扩展是否在线都必须成立。"""
    st, health = api.call("GET", "/api/health")
    rep.add("PASS" if st == 200 else "FAIL", "preflight :: 服务健康", "http=%s" % st)

    okh, hs = api.ok("GET", "/api/browser-automation/host/status")
    hosts = (hs.get("data") or {}).get("hosts") or []
    # Host 在线判定只认 host/status：run 是异步下发，离线不会 409（真机实证，见下方离线契约）
    online = bool(hosts)
    rep.add("PASS" if okh else "FAIL", "preflight :: host/status 可读",
            "count=%s versions=%s" % ((hs.get("data") or {}).get("count"),
                                      [h.get("version") for h in hosts]))
    # F3：online 只证明「注册在场」，servable 才证明「应用面确曾回包」——真机三次踩到
    # online=true 而命令帧有去无回。两栏并排打印，误判健康就没那么容易。
    rep.add("PASS" if any(h.get("servable") for h in hosts) else "WARN",
            "preflight :: Host 可服务证据",
            "servable=%s last_cmd_ok_at=%s" % ([h.get("servable") for h in hosts],
                                               [h.get("last_cmd_ok_at") for h in hosts]))

    okp, plats = api.ok("GET", "/api/browser-automation/platforms")
    pdata = (plats.get("data") or {})
    plist = pdata.get("list") if isinstance(pdata, dict) else pdata
    ids = sorted(str(p.get("identifier")) for p in (plist or []) if isinstance(p, dict))
    rep.add("PASS" if okp and ids else "FAIL", "preflight :: 平台注册表", ",".join(map(str, ids)))

    # D7 列 + 开关贯通：建一条 require_confirm=true 的任务，回读必须为 true
    payload = dict(task_read_xhs())
    payload["name"] = "E2E-preflight-D7开关"
    payload["require_confirm"] = True
    okc, created = api.ok("POST", "/api/browser-automation/tasks", payload)
    if not okc:
        rep.add("FAIL", "preflight :: D7 建任务", json.dumps(created, ensure_ascii=False)[:200])
        return online, None
    tid = created["data"]["id"]
    created_ids.append(tid)
    okg, got = api.ok("GET", "/api/browser-automation/tasks/%s" % tid)
    rb = (got.get("data") or {})
    rep.add("PASS" if rb.get("require_confirm") is True else "FAIL",
            "preflight :: require_confirm 落库回读", "=%s" % rb.get("require_confirm"))

    # 下发契约：run 是异步的（HTTP 200 + session_id），Host 离线不当场 409，
    # 而是由执行器把 ErrHostOffline 落到 session.error_msg —— 预检按在线与否分别核验。
    api.ok("POST", "/api/browser-automation/tasks/%s/publish" % tid)
    st, ran = api.call("POST", "/api/browser-automation/tasks/%s/run" % tid, {})
    probe_sid = (ran.get("data") or {}).get("session_id")
    if st != 200 or not probe_sid:
        rep.add("FAIL", "preflight :: 下发", "http=%s %s" % (st, str(ran)[:160]))
    elif online:
        rep.add("PASS", "preflight :: Host 在线，任务已下发", "session=%s" % probe_sid)
        # 单用户同时只允许一个 running（ErrUserBusy）→ 预检会话必须先让位再跑浏览器腿
        wait_terminal(api, probe_sid, 120)
    else:
        sess = wait_terminal(api, probe_sid, 30)
        err = str((sess or {}).get("error_msg") or "")
        rep.add("PASS" if (sess or {}).get("status") == "failed" and "未连接" in err else "FAIL",
                "preflight :: Host 离线契约（200 下发 + 执行期引导）",
                "session=%s status=%s err=%s" % (probe_sid, (sess or {}).get("status"), err[:120]))

    # cron 入口（注册/列出/停用/注销不依赖 Host；到点真触发由 service/cron_test.go 覆盖）
    # 服务端契约：仅 task_type=cron 的任务可挂触发器（其他类型返回「仅 cron 类型任务…」），
    # 表达式接受 5 段（入库前 toSixField 自动补秒位）。
    cron_task = dict(task_read_xhs())
    cron_task.update({"name": "E2E-preflight-cron载体", "task_type": "cron"})
    okct, ct = api.ok("POST", "/api/browser-automation/tasks", cron_task)
    cron_tid = (ct.get("data") or {}).get("id")
    if not okct or not cron_tid:
        rep.add("FAIL", "preflight :: cron 载体任务", json.dumps(ct, ensure_ascii=False)[:160])
    else:
        created_ids.append(cron_tid)
        okcr, cron = api.ok("POST", "/api/browser-automation/cron",
                            {"task_id": cron_tid, "cron_expr": "0 4 1 1 *",
                             "time_zone": "Asia/Shanghai", "enabled": True})
        cdata = cron.get("data") or {}
        cron_id = cdata.get("id")
        okl, lst2 = api.ok("GET", "/api/browser-automation/cron?page=1&limit=50")
        listed = [c for c in ((lst2.get("data") or {}).get("list") or []) if c.get("id") == cron_id]
        rep.add("PASS" if okcr and cron_id and listed and cdata.get("enabled") else "FAIL",
                "preflight :: cron 入口注册可见",
                "cron_id=%s listed=%s enabled=%s expr=%s" % (
                    cron_id, bool(listed), cdata.get("enabled"), cdata.get("cron_expr")))
        if cron_id:
            api.ok("POST", "/api/browser-automation/cron/%s/disable" % cron_id, {})
            api.ok("DELETE", "/api/browser-automation/cron/%s" % cron_id)

    # confirm 误报闸门：对确无待确认点的会话必须 confirmed=false（不是 500、也不是假放行）
    confirm_target = probe_sid
    if not confirm_target:
        oks0, lst0 = api.ok("GET", "/api/browser-automation/sessions?page=1&limit=1")
        items0 = (lst0.get("data") or {}).get("list") or []
        confirm_target = items0[0].get("id") if items0 else None
    if confirm_target:
        okcf, conf = api.ok("POST", "/api/browser-automation/sessions/%s/confirm" % confirm_target, {})
        rep.add("PASS" if okcf and (conf.get("data") or {}).get("confirmed") is False else "FAIL",
                "preflight :: confirm 无误报", "session=%s %s" % (
                    confirm_target, str(conf.get("message"))[:100]))
    else:
        rep.add("WARN", "preflight :: confirm 无误报", "库内无会话可探测")

    # 审计导出的存量会话可用性（R27 I5 交付面）
    oks, lst = api.ok("GET", "/api/browser-automation/sessions?page=1&limit=1")
    items = (lst.get("data") or {}).get("list") or []
    if items:
        side = items[0].get("id")
        oke, exp = api.ok("GET", "/api/browser-automation/sessions/%s/export" % side)
        keys = sorted((exp.get("data") or {}).keys())
        rep.add("PASS" if oke and "command_log" in keys else "FAIL",
                "preflight :: 存量会话审计导出", "session=%s keys=%s" % (side, ",".join(keys)))
    return online, probe_sid


def wait_terminal(api, session_id, timeout):
    """轮询会话到终态；超时返回最后一次详情（调用方自行判定）。"""
    deadline = time.time() + timeout
    last = None
    while time.time() < deadline:
        ok, got = api.ok("GET", "/api/browser-automation/sessions/%s" % session_id)
        if ok:
            last = got.get("data") or {}
            if last.get("status") in TERMINAL:
                return last
        time.sleep(1.5)
    return last


def print_host_action_needed(hosts):
    print("""
── Host 未在线，浏览器腿需要一次人工动作（脚本其余项已全部跑完）──────────────
  1) Chrome 打开 chrome://extensions → 「HiveMTK Browser Automation」→ 点右上角重新加载
     （dist/ 已随批5 重建为 1.5.0；不重载会踩 SW ScriptCache 旧代码陷阱）
  2) 确认小红书/抖音/闲鱼网页处于登录态（扩展寄生在真实 Profile，登录态只能真人给）
  3) 重跑本脚本：读链路+D7 中止路径无需额外授权
     真发评论的写链路（三段式/D7 确认放行）需再加 --allow-write
──────────────────────────────────────────────────────────────────────────────""", flush=True)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--base-url", default=os.environ.get("BASE_URL", DEFAULT_BASE))
    ap.add_argument("--token", default=os.environ.get("HIVE_MTK_JWT", ""))
    ap.add_argument("--allow-write", action="store_true", help="授权在真实平台发表评论")
    ap.add_argument("--keep", action="store_true", help="保留本次创建的任务（默认收尾归档）")
    ap.add_argument("--only", default="", help="只跑名称含该子串的浏览器腿用例")
    ap.add_argument("--cdp-port", type=int, default=int(os.environ.get("CDP_PORT", "9333")),
                    help="Chrome 远程调试端口，仅用于回收夹具泄漏的 tab（按夹具 URL 前缀匹配）")
    ap.add_argument("--no-fixture", action="store_true", help="跳过本地夹具写腿（默认必跑）")
    args = ap.parse_args()

    repo_root = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
    token = args.token or mint_admin_token(repo_root)
    if not token:
        print("!! 无 JWT：--token 或 HIVE_MTK_JWT 或可 go run ./cmd/minttoken 铸本地 admin token")
        return 2
    api = API(args.base_url, token)
    rep = Report()
    created_ids = []

    print("=== preflight（不依赖 Host 的契约项）===", flush=True)
    host_online_at_preflight, _probe_sid = preflight(api, rep, created_ids)

    st, hs = api.call("GET", "/api/browser-automation/host/status")
    hosts = (hs.get("data") or {}).get("hosts") or []

    fixture = None if args.no_fixture else Fixture()
    # 用例表在分支外建一次：SKIP 腿与运行腿共用同一份标签，杜绝两处清单漂移
    cases = []
    if fixture is not None:
        # 夹具写腿排在最前：不依赖任何账号登录态，是写链路上唯一可复跑的**设备级**证据
        # （真平台写腿要登录态，长期只能靠 WS 测证），五场景把 D7 的三种结局+全自动+静默吞一次跑齐。
        specs = [
            # (名称, 需 D7 闸门, 闸门动作, 期望终态, 期望错误文案, 期望 ledger, 期望有证据, 期望 verified, 预算秒, 静默吞页, 输入框在容器内)
            ("夹具写腿·D7 挂起→确认放行", True, "confirm", "completed", "", 1, True, True, 90, False, False),
            ("夹具写腿·D7 挂起→用户中止", True, "stop", "stopped", "评论未提交", 0, False, False, 90, False, False),
            ("夹具写腿·D7 挂起→预算耗尽", True, "none", "failed", "评论未提交", 0, False, False, 20, False, False),
            ("夹具写腿·无闸门全自动提交", False, "none", "completed", "", 1, True, True, 90, False, False),
            # F11b 真机反向锁：点击确实落到按钮上、页面刻意不提交 → verify 只能说「没见到评论」。
            # 若哪天整页兜底又被翻回 true，这条腿就是唯一会红的那条。
            ("夹具写腿·平台静默吞（点击落地未提交）", False, "none", "failed",
             "验证未通过", 0, True, False, 90, True, False),
            # Leg X 真机常驻反向锁（批6 立项证据）：同一形态再加一层——草稿就躺在容器里面。
            # 上一条款不到容器主分支（输入框在容器外），本条专门条款不到就双绿的容器主分支。
            # 判据仍是带外 ledger=0 + verified=false；旧实现下这条必红（真机 session343 实测
            # completed + verified=true + evidence 仅 {containers:1}——命中的正是容器里那条草稿）。
            ("夹具写腿·静默吞 + 输入框在容器内（Leg X）", False, "none", "failed",
             "验证未通过", 0, True, False, 90, True, True),
        ]
        for (nm, need_confirm, action, want_status, want_err, ledger_n, evidence,
             want_verified, tmo, swallow, inbox) in specs:
            nonce = "q" + os.urandom(3).hex()  # 只用 0-9a-f，避开需 shift 的符号键
            payload, _text = task_write_fixture(fixture, need_confirm, nonce, tmo, swallow, inbox)
            cases.append({"name": nm, "payload": payload, "gate": {
                "action": action, "expect_status": want_status, "expect_err": want_err,
                "ledger": ledger_n, "evidence": evidence, "verified": want_verified,
                "nonce": nonce, "cleanup_fixture_tabs": True}})
        # 台账闸（批6）：双发的唯一硬证据是「同任务二次运行时，一个写帧都没下发」。
        # WS 测里这条靠 fakeExtension.countOf 证，设备侧必须再证一遍——真扩展/真 host 的帧序才是终局。
        dnonce = "q" + os.urandom(3).hex()
        dpayload, _dtext = task_write_fixture(fixture, False, dnonce, 90)
        cases.append({"name": "夹具写腿·同文本二次运行（台账双发闸）", "payload": dpayload,
                      "double_send": True, "gate": {
                          "action": "none", "expect_status": "completed", "expect_err": "",
                          "ledger": 1, "evidence": True, "verified": True,
                          "nonce": dnonce, "cleanup_fixture_tabs": True}})
        # 批7 三条设备级锁：写判定从「一个 action 名」升级成「一类原语属性」之后，
        # 属性化本身（A）、台账不许过拦（B）、豁免不许换成假绿（C）各判一头。
        anonce = "q" + os.urandom(3).hex()
        apayload, _atext = task_derived_write_fixture(fixture, anonce, 60)
        cases.append({"name": "夹具派生写步·无 post_comment 的隐形写也进台账并拦二次",
                      "payload": apayload, "leg": run_derived_write_leg, "nonce": anonce})
        bnonce = "q" + os.urandom(3).hex()
        bpayload, _btext = task_derived_write_fixture(fixture, bnonce, 60, noinput=True)
        cases.append({"name": "夹具派生写步·元素不存在时台账留空不误拦重跑",
                      "payload": bpayload, "leg": run_never_executed_write_leg, "nonce": bnonce})
        cnonce = "q" + os.urandom(3).hex()
        cpayload, _ctext = task_write_fixture(fixture, False, cnonce, 90, swallow=True)
        cpayload.update({"retry_on_fail": True, "retry_delay_sec": 30, "max_retry_times": 1})
        cases.append({"name": "夹具写腿·自动重试轮跳过未验证写步且判红",
                      "payload": cpayload, "leg": run_retry_skip_write_leg, "nonce": cnonce})
    cases += [
        {"name": "真机基线读链路（无登录墙）", "payload": task_baseline_public(), "gate": None},
        {"name": "真机交互搜索腿（键入+点击+跳转+断言）", "payload": task_interact_public(), "gate": None},
        {"name": "xhs 读链路", "payload": task_read_xhs(), "gate": None},
        {"name": "douyin 读链路", "payload": task_read_douyin(), "gate": None},
        {"name": "xianyu 读链路", "payload": task_read_xianyu(), "gate": None},
        {"name": "Brain 模式（只读目标）", "payload": task_brain_xhs(), "gate": None},
    ]
    if args.allow_write:
        cases.append({"name": "xhs 三段式写（全自动）", "payload": task_write_xhs(False), "gate": None})
        cases.append({"name": "D7 确认闸门（挂起→放行）", "payload": task_write_xhs(True), "gate": None})
    else:
        cases.append({"name": "D7 确认闸门（挂起→中止，不提交）", "payload": task_write_xhs(True),
                      "gate": None})
    if not (host_online_at_preflight and hosts):
        print("\n=== 浏览器腿（需要 Host 在线）===", flush=True)
        for c in cases:
            rep.add("SKIP", c["name"], "Host 未在线")
        print_host_action_needed(hosts)
    else:
        print("\n=== 浏览器腿（Host 在线 version=%s）===" % (hosts[0].get("version")), flush=True)
        if fixture is not None:
            print("      夹具页：%s（写腿的不可逆点击只发生在本地页面上）" % fixture.url, flush=True)
        for c in cases:
            if args.only and args.only not in c["name"]:
                continue
            if c.get("double_send"):
                run_resubmit_gate_leg(api, rep, c["payload"], c["name"], c["gate"]["nonce"],
                                      fixture, created_ids, args.cdp_port, c["gate"])
                continue
            leg = c.get("leg")  # 批7：自带编排的多轮腿（二次运行 / 自动重试轮），判据在腿里
            if leg is not None:
                leg(api, rep, c["payload"], c["name"], c["nonce"], fixture, created_ids,
                    args.cdp_port)
                continue
            run_and_watch(api, rep, c["payload"], c["name"], args.allow_write, created_ids,
                          gate=c.get("gate"), fixture=fixture, cdp_port=args.cdp_port)
    if fixture is not None:
        fixture.stop()

    print("\n=== 汇总 ===", flush=True)
    rep.add("SKIP", "MCP 入口",
            "browser_task 工具需由 MCP host（Qoder/agent 侧）发起；服务端接线=router 的 "
            "tooluse.SetBrowserTaskRunner，本脚本无 HTTP 面可调")
    print("PASS=%d FAIL=%d WARN=%d SKIP=%d" % (
        rep.count("PASS"), rep.count("FAIL"), rep.count("WARN"), rep.count("SKIP")), flush=True)
    if not args.keep and created_ids:
        for tid in created_ids:
            api.ok("POST", "/api/browser-automation/tasks/%s/archive" % tid, {})
        print("本次创建的 %d 条任务已归档（--keep 可保留）" % len(created_ids), flush=True)
    return rep.count("FAIL")


if __name__ == "__main__":
    sys.exit(main())
