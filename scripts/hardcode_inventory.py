#!/usr/bin/env python3
"""硬编码点位清单生成器：把三份扫描产物合成一份可分工、可验收的全量清单。

输入（均为 scripts/hardcode_sweep*.py 的产物，落在 hardcode-sweep/ 下）：
  hardcode-sweep.json            Go 侧 CONFIG/DATA/CN/INLINE_NUM
  hardcode-sweep-classified.json Go 侧 CONFIG 的 TUNABLE/PROTOCOL/DEPLOY/INFRA/FIELD_LIT 归类
  hardcode-sweep-web.json        user-web 侧 CN/CONFIG/DATA/URL/TUNE

输出：
  hardcode-sweep/INVENTORY.md       人读清单（按域分组 + 优先级）
  hardcode-sweep/INVENTORY.csv      机读清单（每行一个点位，带建议 group/key）

设计口径（与 check-config-param-readpoints.py 的纪律一致）：
  - 「登记」不等于「生效」。本脚本只负责"点名 + 指路"：给每个点位一个建议 group/key 和
    一条施工指引，不假装已经接线。
  - 不做的事：不改生产代码、不改种子、不动数据库。本脚本是只读的。
  - 归类宁严勿宽：宁可把点位标成 NEEDS_REVIEW 让人看，也不自动塞进 TUNABLE 假装确定。

用法：
  python3 scripts/hardcode_inventory.py            # 生成 MD + CSV
  python3 scripts/hardcode_inventory.py --summary  # 只打印汇总统计
"""

from __future__ import annotations

import argparse
import csv
import json
import os
import re
import sys
from collections import Counter, defaultdict

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from path_guard import safe_resolve  # noqa: E402  路径闸：CSV 输出不得越出仓库

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
OUT_DIR = os.path.join(ROOT, "hardcode-sweep")
SERVER = "user-server/"

# ---------------------------------------------------------------------------
# 域 → config_params group 映射。
#
# 已存在的 group（21 个，来自 config_param_seeds.go）不改名；本表只是在其中做路由，
# 命不中已有 group 的落到 DEFAULT_GROUP。需要新 group 时必须先在种子里登记，
# 否则会出现「清单说该进 X group、库里却没有 X group」的悬空引用。
#
# 顺序敏感：自上而下第一个命中的前缀生效。
# ---------------------------------------------------------------------------
GROUP_RULES: list[tuple[str, str]] = [
    # AI / LLM / 工具
    ("internal/aiagent/agent/tooluse/", "agent_tool"),
    ("internal/aiagent/llm/", "agent_llm"),
    ("internal/aiagent/eval/", "agent_llm"),
    ("internal/aiagent/agent/runtime/", "agent_llm"),
    ("internal/aiagent/knowledge/", "knowledge"),
    ("internal/aiagent/rag/", "knowledge"),
    ("internal/aiagent/mcp/", "agent_tool"),
    # 消息中台 / 收件箱
    ("internal/service/inbox", "inbox_sales"),
    ("internal/service/message_hub", "inbox_sales"),
    ("internal/service/outbound", "inbox_sales"),
    # 工作流 / SOP / Webhook
    ("internal/service/sop", "workflow"),
    ("internal/service/webhook", "workflow"),
    ("internal/service/workflow", "workflow"),
    ("internal/service/approval", "workflow"),
    # 反馈闭环 / 置信度 / AB
    ("internal/service/feedback_loop/", "confidence"),
    ("internal/service/confidence/", "confidence"),
    ("internal/service/ab_", "confidence"),
    ("internal/service/script_ab", "confidence"),
    ("internal/service/agent_attribution", "confidence"),
    ("internal/service/llm_routing", "confidence"),
    ("internal/service/intention/", "confidence"),
    ("internal/service/intent", "confidence"),
    ("internal/service/humanize", "human_task"),
    ("internal/service/persona", "human_task"),
    ("internal/service/human_escalation", "human_task"),
    ("internal/service/human_task", "human_task"),
    # RAG 服务侧
    ("internal/service/rag", "knowledge"),
    ("internal/service/kb_", "knowledge"),
    ("internal/service/knowledge", "knowledge"),
    # 健康 / 配额 / 遥测
    ("internal/service/domain_health", "telemetry"),
    ("internal/service/wecom_account_health", "wecom"),
    ("internal/service/whatsapp_tier", "telemetry"),
    ("internal/service/alert_", "telemetry"),
    ("internal/service/agent_status", "telemetry"),
    ("internal/service/sse_hub", "bridge"),
    ("internal/service/telegram_polling_lock", "channelgw"),
    ("internal/service/ltc_config", "sales"),
    # 采集 / 催收
    ("internal/service/collection_job", "reach"),
    ("internal/service/reach_", "reach"),
    ("internal/geo/service/", "knowledge"),
    # 渠道
    ("internal/channelbot/", "channelgw"),
    ("internal/channelgw/", "channelgw"),
    ("internal/bridge/", "bridge"),
    # 邮件
    ("internal/email/", "misc"),
    ("internal/service/email_", "misc"),
    # 分页 / 通用工具
    ("internal/pkg/utils/pagination", "pagination"),
    ("internal/pkg/pagination/", "pagination"),
    ("internal/pkg/textutil", "misc"),
    ("internal/pkg/utils/constants", "misc"),
    ("internal/pkg/featureflag", "telemetry"),
    ("internal/pkg/security/", "middleware"),
    ("internal/pkg/sla", "telemetry"),
    ("internal/pkg/sso/", "middleware"),
    ("internal/pkg/kbrelease", "knowledge"),
    ("internal/middleware/", "middleware"),
    ("internal/controller/", "misc"),
    ("internal/repository/", "misc"),
    ("internal/model/", "misc"),
    ("internal/service/", "misc"),
    ("internal/ops/service/", "misc"),
    ("internal/browser_automation/", "misc"),
    ("internal/cache/", "cache"),
    # user-web 前端运行期阈值：独立成一个 group，避免和后端 misc 混在一起互相污染 key
    ("user-web/src/utils/agentSocket", "frontend_ws"),
    ("user-web/src/utils/chatSocket", "frontend_ws"),
    ("user-web/src/", "frontend_ui"),
]
DEFAULT_GROUP = "misc"

# ---------------------------------------------------------------------------
# 优先级：P0 = 已经有人抱怨/出事故过或安全相关；P1 = 门禁阈值类，改一次影响全局；
# P2 = 运维调优类；P3 = 开发默认值，几乎不会改。
# 判据是"改了以后有没有人想改"，不是"代码好不好看"。
# ---------------------------------------------------------------------------
P0_HINTS = re.compile(
    r"maxupload|maxuploadsize|bodylimit|maxjsonbody|allowinsecure|password|token|ttl|"
    r"secret|circu|lock|retry|limit|quota|rate|auth|sign|upload",
    re.I,
)
P1_HINTS = re.compile(
    r"threshold|max|min|window|interval|ttl|cache|concurren|cooldown|"
    r"score|weight|confidence|similar|ratio|sla|expire",
    re.I,
)


def load(name: str) -> dict:
    path = os.path.join(OUT_DIR, name)
    if not os.path.exists(path):
        sys.exit(f"缺少输入 {path}，请先运行对应的 hardcode_sweep*.py")
    with open(path, encoding="utf-8") as fh:
        return json.load(fh)


def group_for(file_path: str) -> str:
    p = file_path
    if p.startswith(SERVER):
        p = p[len(SERVER):]
    for prefix, group in GROUP_RULES:
        if p.startswith(prefix):
            return group
    return DEFAULT_GROUP


def key_for(name: str, file_path: str) -> str:
    """常量名 → snake_case 配置键。保持可读、可 grep、且和种子 key 风格一致。

    ALL_CAPS_SNAKE（JS 里的 MAX_RECONNECT_DELAY_MS）必须原样小写，不能按驼峰切，
    否则会切出 m_a_x_... 这种没人愿意在数据库里看的键。
    """
    if re.fullmatch(r"[A-Z][A-Z0-9_]*", name):
        k = name.lower()
    else:
        k = re.sub(r"(?<!^)(?=[A-Z])", "_", name).lower()
    k = re.sub(r"[^a-z0-9_]+", "_", k).strip("_")
    k = re.sub(r"_+", "_", k)
    if not k:
        k = "unnamed"
    # key 全局唯一约束下，加文件域前缀避免同义常量互相撞车
    return f"{group_for(file_path)}_{k}"[:100]


def priority_for(name: str, bucket: str) -> str:
    if bucket == "TUNABLE":
        if P0_HINTS.search(name):
            return "P0"
        if P1_HINTS.search(name):
            return "P1"
        return "P2"
    return "P3"


def collect_wired_fallback_names(base: str) -> set[str]:
    """扫出**已经被读成参数中心兜底值**的常量名。

    直接复用 G4（scripts/check-hardcode-tunable.py）的判据：常量出现在
    Get*(...) 的兜底实参位，或直连范式里 Get* 兜底传 0 而随后回落到该常量。

    为什么清单要专门区分这一类：接线时按 3.1a 的约定**保留原常量名**（避免把
    一次参数化放大成一片 diff），所以接完之后这些常量名字里依然带
    Limit/TTL/Threshold，看起来和没接的一模一样。原清单把它们全算进 TUNABLE，
    于是接线做完后「可行动点位」不降反升 —— 台账失真，连带把验收标准 2 也变成
    一个永远达不成的数。按名字配对救不了这个（名字与 key 毫无关系，实参位置不会）。
    """
    # 门脚本文件名带连字符（check-hardcode-tunable.py），不是合法模块名，
    # 只能按路径加载。
    import importlib.util

    gate_path = os.path.join(os.path.dirname(os.path.abspath(__file__)), "check-hardcode-tunable.py")
    spec = importlib.util.spec_from_file_location("check_hardcode_tunable", gate_path)
    gate = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(gate)
    collect_fallback_idents = gate.collect_fallback_idents

    out: set[str] = set()
    for dirpath, dirnames, filenames in os.walk(base):
        rel = os.path.relpath(dirpath, base).replace(os.sep, "/")
        if any(rel == s or rel.startswith(s + "/") or s in rel.split("/") for s in SKIP_SEGMENTS):
            continue
        dirnames[:] = [d for d in dirnames if d not in ("vendor", "migrations")]
        for fn in filenames:
            if not fn.endswith(".go") or fn.endswith("_test.go"):
                continue
            try:
                text = open(os.path.join(dirpath, fn), encoding="utf-8", errors="ignore").read()
            except OSError:
                continue
            out |= collect_fallback_idents(text)
    return out


SKIP_SEGMENTS = {"cmd/seed", "vendor", "migrations", "test", "tests", "tmp", "backups"}


def collect_web_wired_fallback_names(base: str) -> set[str]:
    """扫出前端**已经坐在 rt* 兜底实参位上**的常量名。

    判据不另立一套：直接复用 G1（scripts/check-config-param-readpoints.py）的前端读取点采集，
    取其中「第 3 个实参是常量名」的那部分。两处各写一份正则的话，清单说「这条已接」而门说
    「这条没人读」（或反过来），两边数字没法互相校验——Go 侧的兜底实参判据就是同一个道理共用
    G4 的 collect_fallback_idents。

    第三个实参是字面量的（`rtNum(..., 'material_max_size_mb', 10)`）不进名单：那说明前端确实
    读了这条参数，但值写在调用点，常量本身仍是硬编码，正该留在待接桶里。
    与 Go 侧不同的一处：前端没有 `Default*` 命名约定，所以只出 WIRED，不出 PARAM_FALLBACK。
    """
    import importlib.util
    from pathlib import Path

    gate_path = os.path.join(os.path.dirname(os.path.abspath(__file__)), "check-config-param-readpoints.py")
    spec = importlib.util.spec_from_file_location("check_config_param_readpoints", gate_path)
    gate = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(gate)
    names: set[str] = set()
    for idents in gate.collect_frontend_readpoints(Path(base)).values():
        names |= idents
    return names

# G4 门对 `Default*` 前缀的豁免口径（参数中心兜底值的约定标记），清单沿用同一判据，
# 不另立一套 —— 两处口径分叉的话，台账说「已参数化」而门说「这是硬编码」，
# 两边数字没法互相校验。
DEFAULT_PREFIX = re.compile(r"^Default")


def rows_from_go(sweep: dict, classified: dict, wired: set[str]) -> list[dict]:
    by_key = {(r["file"], r["line"], r["name"]): r for r in classified.get("rows", [])}
    out = []
    for c in sweep.get("config_consts", []):
        meta = by_key.get((c["file"], c["line"], c["name"]), {})
        bucket = meta.get("bucket", "NEEDS_REVIEW")
        if bucket not in ("TUNABLE", "PROTOCOL", "DEPLOY", "INFRA"):
            continue
        # 已经接进参数中心的，从 TUNABLE 里摘出来单独记一类。
        # WIRED 不是「不处理」而是「已处理」—— 混进 TUNABLE 会让存量清单不降反升。
        if bucket == "TUNABLE" and c["name"] in wired:
            bucket = "WIRED"
        # `Default*` 前缀是 3.1 五批接线统一留下的形态标记（G4 的 DEFAULT_PREFIX
        # 豁免就是为它设的）：名字带 Default 是因为它在 provider 注入缺失时当兜底。
        # classify 会按名字形状把它判成 FIELD_LIT 而漏出清单，于是「已参数化」
        # 与「纯默认值」在这里混成一条。单独归一类，台账才能读出参数化的真实覆盖面。
        elif bucket == "TUNABLE" and DEFAULT_PREFIX.match(c["name"]):
            bucket = "PARAM_FALLBACK"
        grp = group_for(c["file"])
        out.append(
            {
                "side": "user-server",
                "kind": "CONFIG",
                "bucket": bucket,
                "priority": "DONE" if bucket in ("WIRED", "PARAM_FALLBACK") else priority_for(c["name"], bucket),
                "file": c["file"],
                "line": c["line"],
                "name": c["name"],
                "value": c.get("value", ""),
                "suggest_group": grp,
                "suggest_key": key_for(c["name"], c["file"]) if bucket == "TUNABLE" else "",
                "note": meta.get("why", "") or {
                    "WIRED": "已接进参数中心（常量被读成 Get* 的兜底实参）",
                    "PARAM_FALLBACK": "Default* 前缀＝参数中心兜底值的约定标记（G4 DEFAULT_PREFIX 豁免同一口径）",
                }.get(bucket, ""),
            }
        )
    # DATA：只收真正的字典型/提示词常量，局部变量赋值不进清单
    # `Keys` 词缀是补的：`LTCKnownThresholdKeys`（thresholds 下必须逐条给出的四个键名）
    # 是真键名清单，但词缀表里没有 `Keys`，于是整行被静默丢弃 —— 台账把它列进「流程步骤类」
    # 而 CSV 里没有这一行，两份清单对不上。这类词缀表**天生追不上命名**，
    # 所以改动它时要把「新命中哪些行」实际列出来核一遍，别只加词。
    # 词缀表只保证「名字像字典」，不保证这行是**包级常量声明**：
    # `intent.Categories = []string{"social"}` 这类结构体字段赋值（dialog_manager.go 的 if 链 6 处）
    # 会被同一张词缀表捞进来，于是「业务字典」桶里混进 6 行不是字典的东西。
    # 现要求等号左侧是一个裸标识名（可带 var/const）且等于扫描器给出的名字，才算字典声明。
    bare_lhv = re.compile(r"^\s*(?:var\s+|const\s+)?([A-Za-z_]\w*)\s*=")
    dictish = re.compile(
        r"(Statuses|Status|Stages|Stage|Outcomes|Levels|Types|Kinds|Actions|Labels|Sources|"
        r"Priorities|Layers|Ops|Columns|Dimensions|Categories|Intents|Steps|Templates|"
        r"Policies|Rules|Flags|Roles|Scenarios|Rows|Options|Keys|Set|List|Dict)\w*\s*=",
    )
    for d in sweep.get("data_literals", []):
        text = d.get("text", "")
        if not dictish.search(text):
            continue
        lhv = bare_lhv.match(text)
        if lhv is None or lhv.group(1) != d["name"]:
            continue
        grp = group_for(d["file"])
        out.append(
            {
                "side": "user-server",
                "kind": "DATA",
                "bucket": "DICTIONARY",
                "priority": "P2",
                "file": d["file"],
                "line": d["line"],
                "name": d["name"],
                "value": d.get("text", "")[:160],
                "suggest_group": grp,
                "suggest_key": key_for(d["name"], d["file"]),
                "note": "业务字典/枚举，进 DB 需先做「字典表」模型，不是 config_params 的字符串项",
            }
        )
    return out


def cn_bucket(it: dict) -> str:
    f = it.get("file", "")
    ctx = it.get("ctx") or ""
    if f.startswith("cmd/"):
        return "TOOL_SEED_COPY"
    if re.search(r"logger\.|\blog\.|Printf|Sprintf|Infof|Errorf|Warnf|Debugf|Tracef", ctx):
        return "LOG"
    if re.search(r"Reply|Suggestion|Content|Reason|Explain|Detail|Tips|Hint", ctx):
        return "AI_OUTPUT_REPLY"
    return "API_MESSAGE"


def rows_from_web(web: dict, wired: set[str]) -> list[dict]:
    out = []
    for it in web.get("cn_strings", []):
        b = cn_bucket(it)
        if b == "TOOL_SEED_COPY":
            continue
        out.append(
            {
                "side": "user-web",
                "kind": "CN",
                "bucket": b,
                # 文案缺口单列 I18N 优先级：它和"阈值该入 config_params"是两条不同的工单，
                # 混在 P0-P3 里会让阈值那条被 7000+ 条文案淹没。
                "priority": "I18N-A" if b == "API_MESSAGE" else "I18N-B",
                "file": it["file"],
                "line": it["line"],
                "name": it.get("attr", ""),
                "value": it["text"],
                "suggest_group": "i18n",
                "suggest_key": "",
                "note": "i18n 词条缺口：走 vue-i18n / internal/pkg/i18n，不进 config_params",
            }
        )
    for it in web.get("config_consts", []):
        # 与 Go 侧同一判据：常量坐在 rt* 的兜底实参位 = 这条点位已经接完。
        bucket = "WIRED" if it["name"] in wired else "TUNABLE"
        out.append(
            {
                "side": "user-web",
                "kind": "CONFIG",
                "bucket": bucket,
                "priority": "DONE" if bucket == "WIRED" else priority_for(it["name"], "TUNABLE"),
                "file": it["file"],
                "line": it["line"],
                "name": it["name"],
                "value": it.get("value", ""),
                "suggest_group": group_for(it["file"]),
                "suggest_key": "" if bucket == "WIRED" else key_for(it["name"], it["file"]),
                "note": (
                    "已接进参数中心（常量坐在 rt* 的兜底实参位，值来自 /api/config/runtime）"
                    if bucket == "WIRED"
                    else "前端阈值，需经 /api/manage/config-params 暴露后由前端读"
                ),
            }
        )
    return out


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--summary", action="store_true")
    args = ap.parse_args()

    sweep = load("hardcode-sweep.json")
    classified = load("hardcode-sweep-classified.json")
    web = load("hardcode-sweep-web.json")

    wired = collect_wired_fallback_names(os.path.join(ROOT, "user-server"))
    print(f"=== 已接进参数中心的兜底常量: {len(wired)} 个（从 TUNABLE 里单独摘出） ===")
    wired_web = collect_web_wired_fallback_names(os.path.join(ROOT, "user-web", "src"))
    print(f"=== 前端已坐在 rt* 兜底实参位的常量: {len(wired_web)} 个 ===")
    rows = rows_from_go(sweep, classified, wired) + rows_from_web(web, wired_web)

    summary = Counter((r["side"], r["kind"], r["bucket"]) for r in rows)
    prio = Counter(r["priority"] for r in rows)
    grp = Counter(r["suggest_group"] for r in rows if r["kind"] == "CONFIG")

    print("=== 清单规模 ===")
    print(f"总点位: {len(rows)}")
    for (side, kind, bucket), n in sorted(summary.items(), key=lambda x: -x[1]):
        print(f"  {side:12} {kind:8} {bucket:16} {n:6}")
    print("=== 优先级 ===")
    for k in ("P0", "P1", "P2", "P3", "I18N-A", "I18N-B"):
        print(f"  {k}: {prio[k]}")
    print("=== CONFIG 建议 group 分布 ===")
    for k, n in grp.most_common():
        print(f"  {k:14} {n}")

    if args.summary:
        return 0

    # CSV 路径过闸：realpath+commonpath 必须留在仓库根内。
    csv_path = safe_resolve("hardcode-sweep/INVENTORY.csv", ROOT)
    fields = [
        "side", "kind", "bucket", "priority", "file", "line",
        "name", "value", "suggest_group", "suggest_key", "note",
    ]
    with open(csv_path, "w", newline="", encoding="utf-8-sig") as fh:
        w = csv.DictWriter(fh, fieldnames=fields)
        w.writeheader()
        for r in sorted(rows, key=lambda x: (x["priority"], x["side"], x["kind"], x["file"], x["line"])):
            w.writerow(r)
    print(f"\n已写出 {csv_path}")
    print(f"（MD 清单由 docs/HARDCODE_INVENTORY.md 承载，正文只放汇总与热点，不重复 CSV 的每一行）")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
