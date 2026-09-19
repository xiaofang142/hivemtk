#!/usr/bin/env python3
"""e2e_browser_reverse.py — 反向测：证明 e2e_browser_real.py 的每条校验「真的能红」。

为什么要有这个脚本：夹具写腿的五场景靠断言说话，而断言本身没人断言。批5g 收口时三条
新校验（F10 tab 回收唯一性 / F11c 探测帧语义 / 第三方墙归因）如果写成恒真式，跑一万遍
都是绿的——F8（ack 字段名从未被执行）与 F9（文档形态必假红）就是同一课的两次教训。

它不新建任务、不改数据，只读历史会话审计包，外加（可选）在 CDP 上真开一个夹具 tab。
会话样本一律**现扫现认**（按形状匹配，不写死 id），库里换一批会话也不会失效。

用法：
  python3 scripts/e2e_browser_real.py --base-url http://127.0.0.1:8299   # 先跑出正例
  python3 scripts/e2e_browser_reverse.py --base-url http://127.0.0.1:8299 [--cdp-port 9333]
退出码 = 反向测不成立的条数（0 = 每条校验都被证明能红）。
"""
import argparse
import copy
import json
import os
import subprocess
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import e2e_browser_real as E  # noqa: E402

SCAN_PAGE_SIZE = 200   # 服务端 ListSessionReq.Limit 上限（400 直接 INVALID_PARAM）
SCAN_PAGES = 3


def audit_lines(rep_name, d, gate):
    rep = E.Report()
    E.audit_check(rep, rep_name, d, gate)
    return rep.rows


def pick(rows, key):
    return [r for r in rows if key in r[1]]


def collect(api):
    """按形状认领四类样本：走完 close_tab 的腿 / 被兜底回收的腿 / 风控墙腿 / F11c 修复前的坏帧腿。"""
    ids = []
    for page in range(1, SCAN_PAGES + 1):
        ok, lst = api.ok("GET", "/api/browser-automation/sessions?page=%d&limit=%d"
                         % (page, SCAN_PAGE_SIZE))
        items = ((lst.get("data") or {}).get("list") or [])
        ids += [s.get("id") for s in items if s.get("id")]
        if len(items) < SCAN_PAGE_SIZE:
            break
    got = {"closed": None, "cleaned": None, "wall": None, "badprobe": None}
    for sid in ids:
        d = E.fetch_audit(api, sid)
        logs = ((d or {}).get("command_log") or [])
        if not logs:
            continue
        nc = len([l for l in logs if l.get("action") == "close_tab" and l.get("direction") == "command"])
        cl = [l for l in logs if l.get("action") == "session_tab_cleanup" and l.get("ok")
              and E._payload(l).get("tab_id")]
        err = str(((d or {}).get("session") or {}).get("error_msg") or "")
        bad = [l for l in logs if l.get("action") == "block_detect"
               and (not l.get("ok") or E._payload(l).get("blocked"))]
        if got["closed"] is None and nc == 1 and not cl:
            got["closed"] = (sid, d, logs)
        if got["cleaned"] is None and nc == 0 and len(cl) == 1:
            got["cleaned"] = (sid, d, logs)
        if got["wall"] is None and "平台风控拦截" in err and bad:
            got["wall"] = (sid, d, logs)
        if got["badprobe"] is None and any(not l.get("ok") for l in bad):
            got["badprobe"] = (sid, d, logs)
        if all(got.values()):
            break  # 四类样本齐了就收工（每号一次导出请求，别扫穿全库）
    return got, ids


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--base-url", default=os.environ.get("BASE_URL", E.DEFAULT_BASE))
    ap.add_argument("--token", default=os.environ.get("HIVE_MTK_JWT", ""))
    ap.add_argument("--cdp-port", type=int, default=0,
                    help="给了才做 CDP 零泄漏反向测（会在该 Chrome 上真开并关掉一个夹具 tab）")
    args = ap.parse_args()
    token = args.token
    if not token:
        repo_root = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
        token = E.mint_admin_token(repo_root)
    if not token:
        print("!! 无 JWT：--token 或 HIVE_MTK_JWT 或可 go run ./cmd/minttoken 铸本地 admin token")
        return 2
    api = E.API(args.base_url, token)
    bad = []

    samples, ids = collect(api)
    print("扫描 %d 条会话，认领样本：%s" % (
        len(ids), {k: (v[0] if v else None) for k, v in samples.items()}))

    print("\n[R1] F10 tab 回收唯一性：期望翻转两向都必须红")
    if samples["closed"] and samples["cleaned"]:
        for key, want in (("closed", "stopped"), ("cleaned", "completed")):
            sid, d, _ = samples[key]
            r = pick(audit_lines("翻转%d" % sid, d, {"expect_status": want}), "F10")
            print("  样本 %d 按「%s」判 → %s" % (sid, want, r[0][0] if r else "无行"))
            if not r or r[0][0] != "FAIL":
                bad.append("R1/%s" % key)
    else:
        bad.append("R1 缺样本")

    print("\n[R2] F11c 探测帧语义：含 ok=false 探测帧的会话必须判红（夹具腿是干净页，出现即回归）")
    if samples["badprobe"]:
        sid, d, logs = samples["badprobe"]
        r = pick(audit_lines("坏帧%d" % sid, d, {"expect_status": "completed"}), "F11c")
        why = [str(E._payload(l).get("snapshot_error"))[:70] for l in logs
               if l.get("action") == "block_detect" and not l.get("ok")]
        print("  样本 %d → %s %s（该帧 ok=false 的自述原因：%s）" % (
            sid, r[0][0] if r else "无行", r[0][2] if r else "", why[:1]))
        if not r or r[0][0] != "FAIL":
            bad.append("R2")
    else:
        # 库里已无修复前数据时，用构造帧验同一谓词（不放过「零帧也算红」这一支）
        probes = [{"action": "block_detect", "ok": False, "payload": {"blocked": False}}]
        r = pick(audit_lines("构造帧", {"session": {}, "steps": [{"action": "x"}],
                                       "command_log": probes, "llm_plans": [], "exported_at": 0},
                             {"expect_status": "completed"}), "F11c")
        print("  无历史坏帧样本 → 构造帧判定：%s %s" % (r[0][0] if r else "无行", r[0][2] if r else ""))
        if not r or r[0][0] != "FAIL":
            bad.append("R2")

    print("\n[R3] 墙归因：真数据必须归因，逐条抽掉证据必须回到无归因")
    wall = samples["wall"]
    if wall:
        sid, d, logs = wall
        err = str(((d or {}).get("session") or {}).get("error_msg") or "")
        sw = E.server_side_wall(err, logs)
        print("  真数据 session %d：%s" % (sid, "归因成立 → 终态记 WARN" if sw else "无归因（坏：真墙竟不归因）"))
        if not sw:
            bad.append("R3/真数据")
        no_hit = [l for l in logs if not E._payload(l).get("blocked")]
        for label, got in (("抽掉 blocked=true 帧", E.server_side_wall(err, no_hit)),
                           ("抽掉风控错误文案", E.server_side_wall("", logs)),
                           ("换成干净夹具腿帧", E.server_side_wall(err, []))):
            print("  %s：%s" % (label, "仍归因（坏：门槛太松）" if got else "无归因 → FAIL（符合期望）"))
            if got:
                bad.append("R3/" + label)
    else:
        print("  库内无风控墙样本，跳过（真机跑一轮 xhs 读腿即产生）")
    typed = E.task_interact_public()
    print("\n[R3b] 脚本侧 type 腿墙归因：三条证据缺一即回到无归因")
    bside = None
    for sid in ids:
        d = E.fetch_audit(api, sid)
        logs = ((d or {}).get("command_log") or [])
        if E.wall_evidence(logs, typed):
            bside = (sid, logs)
            break
    if bside:
        sid, logs = bside
        print("  真数据 session %d：归因成立 → 终态记 WARN" % sid)
        no_wall = [l for l in logs if "wappass" not in json.dumps(E._payload(l))]
        no_nav = copy.deepcopy(logs)
        for l in no_nav:
            p = E._payload(l)
            if p.get("url"):
                l["payload"] = dict(p, url="https://example.com/s?wd=unrelated")
        for label, got in (("抽掉验证页帧", E.wall_evidence(no_wall, typed)),
                           ("抽掉键入词命中", E.wall_evidence(no_nav, typed)),
                           ("换成无 type 步任务", E.wall_evidence(logs, E.task_baseline_public()))):
            print("  %s：%s" % (label, "仍归因（坏：门槛太松）" if got else "无归因 → FAIL（符合期望）"))
            if got:
                bad.append("R3b/" + label)
    else:
        print("  库内无 type 腿被墙样本，跳过")

    if args.cdp_port:
        print("\n[R4] 夹具 tab 零泄漏：真开一个不回收的夹具 tab，必须判红")
        fx = E.Fixture()
        node = "/tmp/hivemtk_reverse_open.mjs"
        with open(node, "w") as f:
            f.write(OPEN_PROBE_JS)
        try:
            subprocess.run(["node", node, str(args.cdp_port), "close", fx.url],
                           capture_output=True, text=True, timeout=60)
            out = subprocess.run(["node", node, str(args.cdp_port), "open", fx.url],
                                 capture_output=True, text=True, timeout=60)
            time.sleep(2)
            matched = E.cdp_fixture_tabs(args.cdp_port, fx.url)
            print("  %s\n  前缀匹配到夹具 tab=%d（必须 >0，否则这条校验是空转）" % (
                out.stdout.strip().splitlines()[-1] if out.stdout.strip() else out.stderr[:120], len(matched)))
            rep = E.Report()
            t0 = time.time()
            E.cdp_close_fixture_tabs(rep, "反向测·故意泄漏", args.cdp_port, fx.url)
            v, det = rep.rows[-1][0], rep.rows[-1][2]
            print("  判定=%s 耗时=%.1fs（须接近预算 %ds） :: %s" % (v, time.time() - t0,
                                                                  E.F10_DRAIN_BUDGET_SEC, det))
            if not matched or v != "FAIL":
                bad.append("R4")
            print("  兜底回收后残留=%d（须 0）" % len(E.cdp_fixture_tabs(args.cdp_port, fx.url)))
        finally:
            fx.stop()
            os.remove(node)

    print("\n反向测结论：%s" % ("每条新增校验都被证明能红" if not bad else "未能判红：" + str(bad)))
    return len(bad)


OPEN_PROBE_JS = """
const [,, port, mode, url] = process.argv;
const base = 'http://127.0.0.1:' + port;
const hard = setTimeout(() => { console.log('TIMEOUT'); process.exit(3); }, 25000);
const call = (wsUrl, method, params) => new Promise((res, rej) => {
  const ws = new WebSocket(wsUrl);
  const t = setTimeout(() => { ws.close(); rej(new Error('ws 超时')); }, 12000);
  ws.onopen = () => ws.send(JSON.stringify({id: 1, method, params: params || {}}));
  ws.onmessage = (ev) => { const m = JSON.parse(ev.data);
    if (m.id === 1) { clearTimeout(t); ws.close(); res(m); } };
  ws.onerror = (e) => { clearTimeout(t); rej(new Error('ws error ' + (e.message || ''))); };
});
const ver = await (await fetch(base + '/json/version')).json();
if (mode === 'open') {
  const r = await call(ver.webSocketDebuggerUrl, 'Target.createTarget', {url});
  console.log('createTarget -> ' + JSON.stringify(r.result || r.error).slice(0, 160));
} else {
  const tabs = await (await fetch(base + '/json/list')).json();
  for (const t of tabs) {
    if (t.type === 'page' && (t.url === 'about:blank' || t.url.startsWith(url))) {
      await call(ver.webSocketDebuggerUrl, 'Target.closeTarget', {targetId: t.id});
      console.log('closed ' + t.id + ' ' + t.url.slice(0, 40));
    }
  }
}
clearTimeout(hard);
"""

if __name__ == "__main__":
    sys.exit(main())
