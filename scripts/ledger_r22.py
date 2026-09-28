#!/usr/bin/env python3
"""生成 R22 闭合台账快照（一轮一份；历史快照只追加不改写）。

条目的判定（status / attack / 腿 / 复验）是人写的，**quote 一律从 spec 原文按节切片**——
手抄原文必假红（norm 抹掉空白后差一个全角标点就点不到），所以取「该节第 n 个条目前若干字」。

**这一份是常驻副本**：R22.jsonl 首轮由 `/tmp/build_r22.py` 生成，那支脚本按 §7.28 批20b 立下的
口径（"/tmp 日志只是这一趟的事后誊本，不作为可复核的承诺；常驻证据只有脚本那一份文件"）不算证据
——台账自己就是那条口径的适用对象：不在仓里，下一轮谁也复现不出"这 38 条的 quote 到底是从哪一行切的"，
读数只剩 `## 九` 里那句转述。搬进来时改掉的一处：根目录从推导得到，**不硬编码仓名**
（`Path(__file__).resolve().parent.parent`；改名克隆必须照样能跑，见 [[gate-scripts-project-root-depth]]）。

跑法（走统一入口，`--inventory` 与 `--chars` 都在那边）：
    python3 scripts/build-review-ledger.py r22 docs/superpowers/specs/ledger/R22.jsonl
"""
from __future__ import annotations

import importlib.util
import json
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
SPEC_REL = "docs/superpowers/specs/2026-09-19-browser-automation-write-ledger-design.md"
LEDGER = ROOT / "docs/superpowers/specs/ledger/R22.jsonl"
LINES = (ROOT / SPEC_REL).read_text(encoding="utf-8").splitlines()

SEC8 = "8. 批14 同行调研台账（六维度取证 + 对本仓的实证纠正）"
H83 = ("8.3 差距矩阵与取舍（21 行，每条左列都经本泳道读码复核，未复核的一律不进；#11 为"
       "「被子 agent 证伪故不进矩阵」的反例记录；#20、#21 为批16b/批16c 自审新增、"
       "左列不是同行口径而是本仓缺陷形状）")
SEC6 = "6. 记录为独立工作项（本批不修）"
S728 = "7.28 批22：裁剪不等于证据消失（A6 落地：digest-before-delete + 永不裁剪的自证小表）"
H728 = "八、登记（看过、本批不做）"
S729 = "7.29 批23：二次审核线结论复验收口（§6 移交四项 + §8.3-9/10/17/18/19 + §7.28 八-3）"
H729 = "十三、本轮没跑的 / 仍然开着的"
S730 = ("7.30 批24（+批24b）：B 链路出站回 DOM 复核（§8.3-8 落码）与\"会话归属\""
        "六处同口径")
H730 = "十、本批不做的"

RUN16 = "docs/superpowers/specs/ledger/logs/B24sendverify/run2-16cells"
DEDUP = "docs/superpowers/specs/ledger/logs/R23dedup"
SSE = "docs/superpowers/specs/ledger/logs/R23sseack"
TEETH = "docs/superpowers/specs/ledger/logs/R22-teeth"
RET = "docs/superpowers/specs/ledger/logs/R23retention/"
# 批25（二次审核首轮）新增的两族：本票各泳道的整跑取证 + 键形电池自己的日志目录
LANES = "docs/superpowers/specs/ledger/logs/R22-lanes"
DKS = "docs/superpowers/specs/ledger/logs/R22dedupkey"


def head_range(marker: str, occ: int = 1) -> tuple[int, int]:
    """返回该标题（含 ##/###，按子串匹配）名下的行区间；occ 选第几份同名节。

    必须能选份：`### 八、登记（看过、本批不做）` 在本 spec 里有两份（§7.24 与 §7.28），
    取 idx[0] 会把 §7.28 的条目切成 §7.24 的原文——闭合门的 quote 判据把两份并成一个池子，
    所以切错了它照样绿（该族的门侧修法见 check-review-closeout.py:check_claim）。
    """
    idx = [i for i, l in enumerate(LINES)
           if re.match(r"^#{2,6}\s", l) and marker in l]
    if len(idx) < occ:
        raise SystemExit(f"标题点不到（要第 {occ} 份，只有 {len(idx)} 份）：{marker}")
    k = idx[occ - 1]
    lvl = len(re.match(r"^(#+)", LINES[k]).group(1))
    lo = k + 1
    hi = len(LINES)
    for j in range(k + 1, len(LINES)):
        m = re.match(r"^(#{2,6})\s", LINES[j])
        if m and len(m.group(1)) <= lvl:
            hi = j
            break
    return lo, hi


def heading_of(marker: str) -> str:
    """按子串取某个 ## 标题的完整文本（手抄批次标题必错一个字，指针就点不到）。"""
    for l in LINES:
        m = re.match(r"^##\s+(.*)$", l)
        if m and marker in m.group(1):
            return m.group(1).strip()
    raise SystemExit(f"## 标题点不到：{marker}")


def sub_heading_of(marker: str, occ: int = 1) -> str:
    """按子串取某个 `###` 及以下标题的完整文本（指针要落在**这一节**，不能图省事填父批次节：
    填父节等于把整批几十条承诺当成一个池子，quote 撞进哪条门都不管——同名歧义那格反例的同一族病）。
    """
    idx = [i for i, l in enumerate(LINES)
           if re.match(r"^#{3,6}\s", l) and marker in l]
    if len(idx) < occ:
        raise SystemExit(f"### 标题点不到（要第 {occ} 份，只有 {len(idx)} 份）：{marker}")
    return re.sub(r"^#+\s*", "", LINES[idx[occ - 1]]).strip()


def nth_entry(marker: str, pat: str, n: int, take: int = 44, occ: int = 1) -> str:
    """该节内第 n 个以 pat 起头的条目首行前 take 字。"""
    lo, hi = head_range(marker, occ)
    hits = [LINES[i] for i in range(lo, hi) if re.match(pat, LINES[i])]
    if len(hits) < n:
        raise SystemExit(f"{marker} 内 {pat} 只有 {len(hits)} 条，要第 {n} 条")
    return re.sub(r"^\s*(?:[-*+]|\d+[.)])\s+", "", hits[n - 1].lstrip("|")).strip()[:take]


SEP_ROW = re.compile(r"^\|[ :|-]+\|$")


def nth_table_row(marker: str, n: int, must_say: str, take: int = 44, occ: int = 1) -> str:
    """该节内**数据行**第 n 条（跳过表头与分隔行）首格起的前 take 字，并断言它写着 `must_say`。

    为什么不复用 `nth_entry(r"^\\| ")`：分隔行 `|---|---|` 在 `|` 后没有空格，按 `^\\| ` 数会被漏掉，
    于是"第 6 条"实际指到第 7 行——本轮第一次生成就是这么把 R22-29 的指针漂到了隔壁那格
    （22 格保留期电池）上，而闭合门对"quote 是否指向**写这条时心里想的那句**"完全无感（它只验
    是原文子串）。所以这里把行号换成"数据行序号"，再钉一个 `must_say` 关键字：索引一漂就停机，
    不许悄悄指到别的承诺上。
    """
    lo, hi = head_range(marker, occ)
    rows = [LINES[i] for i in range(lo, hi)
            if LINES[i].startswith("|") and not SEP_ROW.match(LINES[i].strip())]
    if len(rows) <= n:  # rows[0] 是表头
        raise SystemExit(f"{marker} 内只有 {len(rows) - 1} 条数据行，要第 {n} 条")
    line = rows[n]
    if must_say not in line:
        raise SystemExit(f"{marker} 第 {n} 条数据行里没有 {must_say!r}：{line[:60]!r}"
                         "（行号漂了 ⇒ 指针会指到另一格承诺上，宁可停机）")
    return line.lstrip("|").strip()[:take]


UNESCAPED_PIPE = re.compile(r"(?<!\\)\|")


def row_cell(n: int, col: int = 4, take: int = 44) -> str:
    """取 §8.3 第 n 行第 col 列的前 take 字。

    切列必须排掉转义竖线：第 17 行左列写着 `contentHash(channel\\|conversationId\\|content)`，
    按裸 `|` 切会把这一列劈成三片、列索引整体右移 ⇒ "取舍"列取回 `conversationId\\` 这么一段碎渣。
    碎渣仍是 spec 原文的子串、长度也过 `check_claim` 的 12 字下限，所以闭合门照样绿——而这条 quote
    已经指不到任何承诺了（切片防手抄的前提是它切的就是那一列）。
    """
    for i in range(*head_range("8.3 差距矩阵")):
        if LINES[i].startswith(f"| {n} |"):
            return [c.strip() for c in UNESCAPED_PIPE.split(LINES[i])][col][:take]
    raise SystemExit(f"§8.3 第 {n} 行找不到")


def slog(script: str, expect: str) -> dict:
    return {"script": f"scripts/{script}", "invocation": f"python3 scripts/{script}",
            "expect": expect}


def repro(script: str, expect: str) -> dict:
    return {"cmd": f"python3 scripts/{script}", "expect": expect}


KILL = "末行『…格逐刀被杀，无存活』"
GLEG = {"pkg": "./internal/service/", "name": "TestIngress_ContentDedupWindowExpires"}
ELEG = {"pkg": "./internal/service/", "name": "TestHandleIngress_SelfEchoDecisionPersistsNothing"}

# id, 行号, attack, status, 电池脚本, first_seen
ROWS = [
    ("R22-01", 1, "把 actionabilityCheck 里 stable 的连续两帧 boundingBox 比对摘掉（三份内联任一份），"
                  "或把点后身份复核的结果吞成 ok", "fixed", "mut_actionability_b17.py"),
    ("R22-02", 2, "把 ok 从 *bool 改回非空 bool（存量下发帧的假 ✓ 回来），或写步不再落 write_confirm",
     "fixed", "mut_command_log_ok_b20b.py"),
    ("R22-03", 3, "回收 inflight 时重置尝试计数（30s 一跳就永远撞不到顶＝永久重推）",
     "fixed", "mut_push_budget_b20d.py"),
    ("R22-04", 4, "摘掉 SentCache 的读取侧 TTL 判据（只剩条数上限、无时间界）",
     "fixed", "mut_push_budget_b20d.py"),
    ("R22-05", 5, "D1：放行不比对 payload_hash；D4：摘掉空载荷前置拒绝", "fixed",
     "mut_review_r22_teeth.py"),
    ("R22-06", 6, "把 digest 落库挪到 DELETE 之后（摘要写不上也照删），或不给 0 行那次扫描留痕，"
                  "或短路「摘要声明行数≠实删行数即撤销本批」那道守卫（D23）",
     "fixed", "mut_retention_a6.py"),
    ("R22-07", 7, "不做心跳续期：把 30s 认领窗改成任意值，看有没有腿红", "refuted", None),
    ("R22-08", 8, "V1 判据退化成文本命中、V12–V16 摘掉任一处会话归属判据、V4 让回查未见参与 ack、"
                  "V10 把三态洗成二态", "fixed", "mut_send_verify_b24.py"),
    ("R22-09", 9, "K1 键尾去掉会话维、K2/K3 调用方不传界、K6/K7 仓储侧不接界、"
                  "K4/K5 落库失败不退坑、T1 窗口无界", "fixed", "mut_ingest_dedup_r23.py"),
    ("R22-10", 10, "L1：带平台稳定消息 id 的事件仍走内容窗口", "fixed", "mut_ingest_dedup_r23.py"),
    ("R22-11", 11, "子 agent 三条断言：Extra 是 json:\"-\"、reAckDeliveredOnCacheHit、"
                   "ErrOutboundAckScopeMismatch", "refuted", None),
    ("R22-12", 12, "recordSubmitState 写失败只 Warn 不返回 error（台账没落＝下一轮闸门整体消失）",
     "fixed", "mut_ledger_b16.py"),
    ("R22-13", 13, "guardResubmit 查询失败 return nil 放行（抖动期闸门消失）", "fixed",
     "mut_ledger_b16.py"),
    ("R22-14", 14, "D2：judge 帧 Action 改名（审计面按名字取不到）；D2b：正文泄进 d7_wait 帧",
     "fixed", "mut_review_r22_teeth.py"),
    ("R22-15", 15, "D3：d7_wait 帧改名 ⇒ 跨进程查证读不到挂起", "fixed", "mut_review_r22_teeth.py"),
    ("R22-16", 16, "M30 只钳 retries、M31 只漏失败路径落账、把 unknown 折回 none", "fixed",
     "mut_ledger_b16c.py"),
    ("R22-17", 17, "K8–K11：编号恒 0、编号过宽、正则不两头锚死、后缀参与精确判等", "fixed",
     "mut_ingest_dedup_r23.py"),
    ("R22-18", 18, "K4/K5：把 Blocked 的 return 挪回 persistMessage 之前（单条或批量任一支）",
     "fixed", "mut_ingest_dedup_r23.py"),
    ("R22-19", 19, "P1 嗅探从前缀退化成子串、P2 删掉一条结论短语", "refuted", None),
    ("R22-20", 20, "把 ON CONFLICT 换成覆盖写、认领者不回读、键不全照发、"
                   "writeClaimRepo==nil 放行", "fixed", "mut_write_claim_a12.py"),
    ("R22-21", 21, "M27/M28：一个符号被四处消费却只测一格 ⇒ 按消费点逐格下刀", "fixed",
     "mut_ledger_b16c.py"),
]

REFUTED = {
    7: "复跑批20d 电池里回收 inflight 那一格：30s 可见性超时改成任意值都有腿红（红因点名「回收不重置计数」），"
       "实测既有机制已承担「租约不会永久被占」这件事 ⇒ 心跳续期剩下的收益只有「少重推几轮」的延迟，"
       "注码看不到任何无人守的面",
    11: "三条断言逐条 grep 复验：Extra 在 protocol.go:157 带 json:\"extra,omitempty\"、HTTP 侧 "
       "handler_http.go:770 真带出 extra；另两个符号全仓 grep 0 命中，git log --all -S 也查无出处 "
       "⇒ 不是「没修」，是「根本没有这件东西」",
    19: "注码 P1（HasPrefix 改 Contains）红在 wantFalse 那半、P2（删一条结论短语）红在 wantTrue 那半，"
       "两格各读红因确认方向 ⇒ 误判面已被前缀闭集关死；换枚举要动 InboxIngressResult 及全部产出点，"
       "换来的仍是字符串换字符串。恢复时机写在 §7.29 六",
}

ITEMS6 = [
    ("R22-22", 1, "K1：键尾去掉 conversation_id", "mut_ingest_dedup_r23.py"),
    ("R22-23", 2, "K2/K6：hub 层内容查找不带时间界（调用方与仓储侧各一刀）", "mut_ingest_dedup_r23.py"),
    ("R22-24", 3, "L1：官方事件 id 与内容哈希同权重", "mut_ingest_dedup_r23.py"),
    ("R22-25", 4, "S1–S4：SSE 形态不排 _pendingAck；排水两份实现各改一边", "mut_sse_ack_r23.py"),
]

ITEMS730 = {
    "R22-33": ("结论位进 write_confirm 那一步", "V3/V8 注码：回声帧不带结论位 / 帧构造不透传 callerExtra",
               "refuted",
               "实测 send_verified 在 user-server/ 全树 grep 0 命中（第一版用 grep --include=*.go 报出的 0 "
               "是 zsh 把参数当 glob 吃掉的假零，改用 ripgrep 重跑才算数）⇒ 结论位今天无人读；"
               "write_confirm 的语义是「平台侧确认到某段文本」，与「窗口内计数增加」不同源。"
               "恢复时机：一旦审计导出或 UI 要按结论位过滤，本条即刻重开"),
    "R22-34": ("设备腿：真机上「平台静默吞」是否存在、2500ms 在慢机型上够不够", "无（要用户的宿主与真机）",
               "blocked", None),
    "R22-35": ("§8.3 row 8 的同文本并发改动判成一次",
               "V1/V11 注码：把计数判据换成文本命中、基线改在点击之后取",
               "refuted",
               "注码 V1 与 V11 两格各自被杀，红因分别点名基线取点与计数判据 ⇒ 已知代价的方向是"
               "「少报一次已发」，不会造成双发（不可撤销那一侧有红线①守着）；修它要先给五家各写"
               "「我的气泡」选择器，而 §六 实测五渠道零 per-platform 代码"),
}


def entry(iid, sec, quote, attack, status, evidence, first_seen, nxt=""):
    return {"kind": "item", "id": iid, "claim": {"section": sec, "quote": quote},
            "attack": attack, "status": status, "evidence": evidence,
            "next": nxt, "first_seen": first_seen}


def fixed(leg_script, expect, logs=None):
    ev = {"legs": [slog(leg_script, expect)]}
    if logs:
        ev["logs"] = logs
    else:
        ev["repro"] = repro(leg_script, expect)
    return ev


items = []
for iid, n, attack, status, script in ROWS:
    quote = row_cell(n)
    seen = "2026-09-22" if n == 8 else "2026-09-19"
    if status == "refuted":
        it = entry(iid, H83, quote, attack, "refuted", {"reverify": REFUTED[n]}, seen)
    elif n == 8:
        it = entry(iid, H83, quote, attack, "fixed",
                   fixed(script, "判定行『16 格逐刀被杀，无存活』＋末行四文件逐字节还原 md5 一致",
                         [f"{RUN16}/battery-full-16cells.txt", f"{RUN16}/V16.log",
                          f"{RUN16}/V12.log", f"{RUN16}/CONTROL.log"]), seen)
    elif n == 5 or n == 14 or n == 15:
        it = entry(iid, H83, quote, attack, "fixed",
                   fixed(script, "七刀逐格被杀，红名并集恰好等于该格预测名单（多一条算连带面）",
                         [f"{TEETH}/D1.log", f"{TEETH}/D2.log", f"{TEETH}/D3.log"]), seen)
    elif n == 2:
        # 批25 lane 3 续刀：M7/M8 两格补进电池后整电池复跑（6→8 格），读数随 §五 一起进台账。
        it = entry(iid, H83, quote, attack, "fixed",
                   {"legs": [slog(script, "末行『===== 判定：8/8 格，逐格被杀，无存活 =====』；"
                                          "M7/M8 是 lane 3 续刀补的两格（失败回包帧 verdict 折成常量），"
                                          "控制组 service settled=6 / migrations settled=4 放刀前现测")],
                    "logs": [f"{LANES}/mut_command_log_ok_b20b-r22lane3-full8.log",
                             f"{LANES}/a6a2-oldlegs-under-knife-r22lane3.log",
                             f"{LANES}/a6a2-newlegs-fail-text-r22lane3.log"]}, seen)
    elif n == 6:
        it = entry(iid, H83, quote, attack, "fixed",
                   {"legs": [slog(script, "末行『===== 电池判定：Go 28 格逐格被杀，无存活（全电池） =====』；"
                                          "D19–D22 是 lane 3 续刀补的四格（摘要跨连接 / RowsBefore 换累计 /"
                                          " 链头接最旧 / ok 的 t、f 折一态），D23 是收口轮补的行数守卫短路"
                                          "（声明 2 行、实删 3 行那一批照样提交），控制组 rc=0 total=30 skip=0"
                                          " 放刀前现测；27 格旧格与新格同趟复跑，不沿用上一趟读数")],
                    "logs": [f"{LANES}/mut_retention_a6-r22close-full28.log",
                             f"{LANES}/mut_retention_a6-d23cell2-r22close.log",
                             f"{LANES}/mut_retention_a6-d23-cell-r22close.log",
                             f"{LANES}/mut_retention_a6-r22lane3-full27.log",
                             f"{LANES}/a6a2-oldlegs-under-knife-r22lane3.log",
                             f"{LANES}/a6a2-newlegs-fail-text-r22lane3.log"]}, seen)
    else:
        it = entry(iid, H83, quote, attack, "fixed",
                   fixed(script, f"{KILL}；本格点的是 §8.3 第 {n} 行那条承诺对应的注码族"), seen)
    items.append(it)

BASN = "./internal/browser_automation/service/"
SVCN = "./internal/service/"


def gleg(pkg: str, name: str) -> dict:
    return {"pkg": pkg, "name": name}


def sleg(script: str, expect: str) -> dict:
    return {"script": f"scripts/{script}", "invocation": f"python3 scripts/{script}",
            "expect": expect}


# ---------------- 批25（二次审核首轮）新增：§8.3 五行「取舍写到了、腿没长到」的格子 ----------------
# 这一族与 ROWS 的区别不在**承诺**（同一条 §8.3 行早由 R22-01…21 记过一次终态），而在**覆盖的形状**：
# 那几行的取舍文案各自点名了一条验收口径（"各一份分支各一刀"、"按消费点拆刀"…），本轮逐刀复跑
# 发现口径的其中一半从未被任何腿红过。协议 §3 的终态是记在承诺轴上的，一条承诺不能被改两次
# ⇒ 另立条目，旧行终态不动（它记的是"这一格当时怎么取舍"，不是"这一格有几刀"）。
# reconcile 只在"归属某 ## 节的行数 < 该节钉住的条目数"时红 ⇒ 同一节多出行不炸门。
NEW25 = [
    ("R22-39", 18,
     "批次路径那句 `if !decision.IsSelfEcho` 改成无条件落库：该行取舍自己写明「单条与批次各一份分支"
     "各一刀（格 K4/K5）」，可 K4/K5 两刀打的都是 dup 侧（判重也要落库），**回声侧**（回声绝不落库）"
     "在批次分支上无腿 ⇒ 整包注码实测存活，同一句 AI 话术在会话里存两遍。"
     "（首轮那趟的「存活」还叠了一层覆盖假象：新腿文件没进克隆，见 `battery-18cells-echo-half.log`；"
     "把它列入 `OWN_UNTRACKED` 后 K13 才由无腿变有腿，读数在 run2 那份日志）",
     [gleg(SVCN, "TestHandleIngressBatch_SelfEchoPersistsNothingAndDupPersists"),
      sleg("mut_ingest_dedup_r23.py",
           f"{KILL}；本条点的是回声侧 K12/K13（dup 侧那两刀＝R22-18 的 K4/K5）")],
     [f"{DEDUP}/battery-18cells-run2-batch-echo-leg-in-clone.log",
      f"{DEDUP}/battery-18cells-echo-half.log",
      f"{LANES}/service-full-mut-batchecho.log",
      f"{LANES}/service-channelgw-gate-full.log"]),
    ("R22-40", 20,
     "A12 那一行的取舍点了四种坏形状（键不全照发 / 闸门没接线放行 / 撞约束不判持有者 / 认领者不回读），"
     "唯独**占坑本身报错**这一支没点：把 `ClaimWriteSlot` 调用后的 `if err != nil { return err }` "
     "改成 `return nil`（拿不到独占结论也照发＝并发下双发），该电池 15 条腿全绿",
     [gleg(BASN, "TestWriteClaimRepoErrorRefusesDispatch"),
      sleg("mut_write_claim_a12.py", f"{KILL}；本条点的是 C16（报错吞成放行）")],
     [f"{LANES}/mut_write_claim_a12-run.log"]),
    ("R22-41", 17,
     "扩展侧 `_dedupKey` 的**键形**：注释把它写成硬承诺（「0 必须保持旧字节…键形一变就等于全部重报"
     "一遍」），而该形状全仓零断言——把 `occurrence > 0` 注成 `>= 0`（首条变 `base#0`），"
     "已实跑的 occurrence 四腿全打在 `_canonicalMsgId` 的 event_id 上 ⇒ 整包 766 passed、rc=0 不红",
     [sleg("mut_dedupkey_shape_r22.py",
           "判定行『2 格逐刀被杀，无存活』＋红集合**恰好等于**各格预测名单（D1 三条、D2 首两条）"
           "＋末行源文件逐字节还原 md5 一致")],
     [f"{DKS}/battery-full-2cells.log", f"{LANES}/bridge-vitest-mutant-before-newlegs.txt",
      f"{LANES}/bridge-vitest-mutant-with-newlegs.txt",
      f"{LANES}/bridge-vitest-mutant-dedupkey-only.txt"]),
    ("R22-42", 21,
     "「一处符号多处消费要按消费点逐格拆刀」这条口径在 D7 超时那一格没执行：`waitForConfirm` 返回的"
     "`confirmWaitTimedOut` 结论在 executor 的 `switch` 里被吞掉（`case <-timer.C` 之后不再区分、"
     "直接走到不可逆提交点）不红，`why` 里把「确认预算到头」写成「执行预算用尽」（运维去调错旋钮）"
     "也不红 ⇒ 电池 G1 消费点 / G2 归因 / G3 判据本体三刀各绑一条断言",
     [gleg(BASN, "TestWSE2E_D7ConfirmTimeoutVerdictConsumedByCaller"),
      sleg("mut_d7verdict_b20g.py", f"{KILL}；三刀共用同一条腿是**有意的**：各自打的是那条腿里"
                                   "三条不同的断言（放行 / 文案 / 结论本体），同族提示照实打印")],
     [f"{LANES}/mut_d7verdict_b20g-run.log"]),
    ("R22-43", 10,
     "生产者侧那一格：钉钉把官方 msgId 原样写进 `Extra[\"channel_msg_id\"]` 这件事无腿——本行承诺的两个"
     "下游（内容窗口守卫、出站回环识别）都只**读** Extra，三条既有腿手工塞 `evt.Extra` 不经任何生产者"
     "⇒ 把 `dingtalk_app.go` 那行改成空串、或整列不写，全仓照样绿",
     [gleg(SVCN, "TestDingTalkInboundChannelMsgIDIsOfficialMsgID"),
      sleg("mut_dingtalk_msgid_r22lane.py", f"{KILL}；P1 值改空串 / P2 整列不写")],
     [f"{LANES}/mut_dingtalk_msgid_r22lane-run2-widened-lane.log"]),
]
for iid, n, attack, legs, logs in NEW25:
    items.append(entry(iid, H83, row_cell(n), attack, "fixed",
                       {"legs": legs, "logs": logs}, "2026-09-23"))

for iid, n, attack, script in ITEMS6:
    script_logs = (f"{SSE}/S1.log", f"{SSE}/CONTROL.log") if script == "mut_sse_ack_r23.py" \
        else (f"{DEDUP}/K1-redis-key-loses-conversation.log", f"{DEDUP}/CONTROL.log")
    items.append(entry(iid, SEC6, nth_entry(SEC6, r"^\d+[.)] ", n), attack, "fixed",
                       fixed(script, f"{KILL}（本条是 §6 移交第 {n} 项的落点）", list(script_logs)),
                       "2026-09-20"))

# ---------------- §7.24 八、登记 三条（与 §7.28 的小节同名 ⇒ 指针填所属 ## 节）----------------
S724 = heading_of("7.24 批20b")
B724 = [
    ("R22-26", 1, "把 §4.4/§0/§5.4 任一处改回旧版本号或旧表数，看离线链接门与人工复核谁先红", "fixed"),
    ("R22-27", 2, "M2/M4 注码：让写失败时少落一行 / 整帧不落，看哪条腿红", "refuted"),
    ("R22-28", 3, "把 evidence 正文塞回流里（I5 导出面就带出正文），看零正文那条断言红", "refuted"),
]
for iid, n, attack, status in B724:
    if status == "refuted":
        rv = ("注码复验：M2/M4 两格实测「少行的形状」有腿红（红因点名计数列），而「整帧不落」这一格今天"
              "仍无人守——它是 §8.3 第 13 行同款的有意不对称：写失败可见、可人工重跑，双发不可见、"
              "不可撤销；且该面属 A6 裁剪自证那条线，本轮已在 §7.28 落成 22 格电池"
              if iid == "R22-27" else
              "注码复验：把回包正文塞进流侧字段后「审计帧零正文」那条腿红（红因点名 I5 导出面），"
              "实测结论住在 judge 帧、证据住在可变行是两条分开的断言，不是一处漏写")
        items.append(entry(iid, S724, nth_entry(H728, r"^- ", n, occ=1), attack, "refuted",
                           {"reverify": rv}, "2026-09-22"))
    else:
        items.append(entry(iid, S724, nth_entry(H728, r"^- ", n, occ=1), attack, "fixed",
                           fixed("check-md-links-offline.py",
                                 "判定行：已入仓 md 的相对内链 0 断链（文档回灌不得留悬空引用）"),
                           "2026-09-22"))

# ---------------- §7.28 八、登记 三条（批22 自己那份同名小节，同一条链的另一半）----------------
R_GAP_LEG = {"pkg": "./internal/browser_automation/repository/",
             "name": "TestA6DigestRecordsSeqGap"}
A6_LOG = f"{RET}battery-full-22cells.log"
A6_D18 = f"{RET}run2-d18-cell.log"
B728 = [
    ("R22-36", 1, "把摘要的 row_count 折成 last_seq-first_seq+1（少一帧时区间照样连续＝断号被算术抹平），"
                  "看有没有腿红", "fixed"),
    ("R22-37", 2, "把写侧链哈希退化成 chain = batch_digest（前驱不参与＝补一段假历史零代价）", "refuted"),
    ("R22-38", 3, "V1–V6 各一刀：空来源放行 / 来源列写空串 / 入口传死值 / 不看禁用开关 / "
                  "截断列宽放开 / 启动门不看装配齐", "fixed"),
]
for iid, n, attack, status in B728:
    quote = nth_entry(H728, r"^- ", n, occ=2)
    if iid == "R22-36":
        ev = {"legs": [R_GAP_LEG,
                       slog("mut_retention_a6.py",
                            "D18 格判「杀掉」且红名点名 TestA6DigestRecordsSeqGap")],
              "logs": [A6_LOG, A6_D18]}
        items.append(entry(iid, S728, quote, attack, "fixed", ev, "2026-09-22"))
    elif iid == "R22-37":
        rv = ("注码复验：链的自洽在写侧已有牙——电池 D3（链哈希不看前一条）实测红在 "
              "TestA6ChainLinksAcrossRuns，D4/D5 各自点名同一条腿（读数见 logs/R23retention/"
              "battery-full-22cells.log），读侧链序由 D14 点名 TestA6ExportPayloadCarriesAuditDigests；"
              "而「离线全链重算」这一族函数全仓 grep 只有写侧 auditChainHash 一个定义 + 一个调用点"
              "（command_log.go:134/205），无任何读侧消费者 ⇒ 再写一个只有测试会调的校验函数，"
              "守的是仓内没人读的面，本条不做不是漏")
        items.append(entry(iid, S728, quote, attack, "refuted", {"reverify": rv}, "2026-09-22"))
    else:
        ev = {"legs": [
            {"pkg": "./internal/browser_automation/repository/",
             "name": "TestR23PruneRefusesEmptyCutoffSource"},
            {"pkg": "./internal/browser_automation/repository/",
             "name": "TestR23PruneRunRecordsCutoffSource"},
            {"pkg": "./internal/browser_automation/service/",
             "name": "TestR23PruneOncePassesSourceToRepo"},
            {"pkg": "./internal/migration/migrations/",
             "name": "TestAuditPruneCutoffSourceMigration_UpAndShape"},
            slog("mut_retention_a6.py", "V1–V6 六格逐格被杀，红名各自点名自己的 R23 腿")],
            "logs": [A6_LOG]}
        items.append(entry(iid, S728, quote, attack, "fixed", ev, "2026-09-22"))

# §7.29 十三 三条 + §十二 证据表一行
# （第 1 项原本钉在 §十三 的"16 格全量还没跑"那条上；那一格跑完后它不再是一句承诺，
#  锚点跟着搬去 §十二 那张表里被填了实测读数的第 6 行——**换锚点不换条目**，否则同一条
#  承诺会在两个终态之间漂移。这里用 (节标记, 形状, 第几条) 三元组代替原来的单个序号。）
M12 = "十二、证据"
B729 = [
    ("R22-29", (M12, "tablerow", 4, "18 格（含 T1"),
     "R1–R3 / T1 各格注码（K/L/T 三族；格数不写死在这里，以 `--check` 现算）",
     "fixed", "mut_ingest_dedup_r23.py"),
    ("R22-30", (H729, "bullet", 1, ""), "任一泳道把 internal/service 整包跑断、或只拿 -run 子集当门",
     "fixed", None),
    ("R22-31", (H729, "bullet", 2, ""), "无（要用户的宿主与设备）", "blocked", None),
    ("R22-32", (H729, "bullet", 3, ""),
     "注码到那两条分支上：releaseInboundDedup 吞掉错误、归一化 SQL 去掉 COLLATE", "refuted", None),
]
for iid, (mark, kind, n, must_say), attack, status, script in B729:
    quote = (nth_table_row(mark, n, must_say) if kind == "tablerow"
             else nth_entry(mark, r"^- ", n))
    sec = sub_heading_of(mark)
    if status == "fixed" and script:
        ev = fixed(script, f"{KILL}；控制组 total 放刀前现测（共享树下不写死常量）",
                   [f"{DEDUP}/battery-18cells-run2-batch-echo-leg-in-clone.log"])
        it = entry(iid, sec, quote, attack, "fixed", ev, "2026-09-22")
    elif iid == "R22-30":
        ev = {"legs": [GLEG, ELEG],
              "repro": {"cmd": "cd user-server && go test -p 1 -count=1 -timeout 40m ./internal/service/",
                        "expect": "ok 行且无 FAIL；整包含新补的跨界腿与等价类腿"}}
        it = entry(iid, sec, quote, attack, "fixed", ev, "2026-09-22")
    elif status == "blocked":
        it = entry(iid, sec, quote, attack, "blocked",
                   {"owner": "用户（要本机宿主与真机设备）", "due": "2026-10-31"}, "2026-09-22",
                   "用户接上宿主后跑 scripts/e2e_browser_real.py 的伪造域名档，读数回写 §7.29 十三 与本条")
    else:
        rv = ("注码复验：把那两处各自注坏一次（releaseInboundDedup 改成吞掉错误、归一化那条 SQL 去掉 "
              "COLLATE），整电池跑出来 settled 不变、无腿红 ⇒ 构造性无覆盖。替代腿与原因写在 "
              "mut_ingest_dedup_r23.py docstring 的「已知未覆盖」，各自指到一条真在跑的腿")
        it = entry(iid, sec, quote, attack, "refuted", {"reverify": rv}, "2026-09-22")
    items.append(it)

# §7.30 十 三条
for iid, n in (("R22-33", 1), ("R22-34", 2), ("R22-35", 3)):
    key, attack, status, rv = ITEMS730[iid]
    quote = nth_entry(H730, r"^- ", n)
    assert quote.replace("`", "")[:2] == key[:2], \
        f"{iid} 指针对不上：{quote[:20]!r} vs {key[:20]!r}"
    if status == "blocked":
        items.append(entry(iid, H730, quote, attack, "blocked",
                           {"owner": "用户（要真机与慢机型）", "due": "2026-10-31"}, "2026-09-22",
                           "真机回归时按 §7.30 九 的 repro 复跑 mut_send_verify_b24.py，"
                           "并把结论位读数与 sendVerifyMs 实测摆动记进设备腿与本条"))
    else:
        items.append(entry(iid, H730, quote, attack, "refuted", {"reverify": rv}, "2026-09-22"))

# ---------------- meta：coverage 由门自己的函数现算，不手抄 ----------------
p = importlib.util.spec_from_file_location("crc", ROOT / "scripts" / "check-review-closeout.py")
crc = importlib.util.module_from_spec(p)
p.loader.exec_module(crc)
lines, bs = crc.read_spec(ROOT / SPEC_REL)
tot = crc.coverage_totals(lines, crc.own_segments(lines, bs))
LEDGER_SECS = {SEC6, S724, S728, S729, S730, SEC8}
OOS = ("该节的可数条目是某一功能批的验收实证叙述：其承诺在该批落地时各自配了腿并跑进 "
       "scripts/mut_*.py 常驻电池；本轮的承诺轴由 §6 移交四项 / §7.24 八登记 / §7.28 八登记 / "
       "§7.29 十三 / §7.30 十 / §8.3 矩阵六处抽轴收口，此处不重复计入（重复计入会让同一条承诺有两个终态）")
# §九 不能套用上面那句泛化理由：它不是"某一功能批的验收实证叙述"，而是本轮 A–E 五相的**过程读数**。
# 拿共用理由盖过去，等于让"本节新增/改写了什么"永远不会惊动任何人——理由要按它自己的性质写。
OOS_SEC9 = ("本节记的是二次审核协议 A–E 五相**执行过程的读数**（哪一相、哪一路、跑出什么数）与"
            "越界改动清单，不是任何功能批对外说过的承诺：没有可指认的判据符号，注码进过程里"
            "不会让任何腿红。它的承接面是协议自己那五相流程（R22-protocol.jsonl 的 §4/§5 两条 "
            "out-of-scope 理由）与协议 §8 的五条验收判据，那些承诺在 R22-protocol.jsonl 里逐条"
            "有终态；在此处重复计入会让同一句话有两个终态")
SEC9_PREFIX = "9. "
coverage = []
for sec, n in sorted(tot.items()):
    if n == 0:
        continue
    if sec in LEDGER_SECS:
        coverage.append({"section": sec, "total": n, "status": "ledger"})
    elif sec.startswith(SEC9_PREFIX):
        coverage.append({"section": sec, "total": n, "status": "out-of-scope", "reason": OOS_SEC9})
    else:
        coverage.append({"section": sec, "total": n, "status": "out-of-scope", "reason": OOS})

def pin(section: str, heading: str, marker: str, occ: int, shape: str, note: str) -> dict:
    """计数钉的数由**现测**，不手写：spec 一改（含并行 lane 往同名节里加条目）就先在这里显形，
    而不是把一枚过期钉写进台账、等闭合门在下一趟里喊红。occ 选第几份同名节，理由见 head_range。
    """
    lo, hi = head_range(marker, occ)
    n = crc.count_shape(lines, lo, hi, shape)
    if not n:
        raise SystemExit(f"计数钉现测为 0：{marker} / {shape}（锚点或形状写错了，钉成 0 等于没钉）")
    return {"section": section, "heading": heading, "shape": shape, "count": n, "note": note}


scope = [
    pin(SEC8, H83, "8.3 差距矩阵", 1, "tablerow",
        "§8.3 全部 21 行：每行一条取舍，都要有终态"),
    pin(SEC6, SEC6, SEC6, 1, "numbered", "§6 批11 移交四项"),
    pin(S724, H728, H728, 1, "bullet",
        "批20b 自登记三条（与 §7.28 那份同名，按节键不并）"),
    pin(S728, H728, H728, 2, "bullet",
        "批22 自登记三条（同名的另一份 §八 属 §7.24，按节键不并）"),
    pin(S729, H729, H729, 1, "bullet", "批23 §十三 仍开着的三条（第四条「16 格全量」已跑完，锚点搬进 §十二 证据表）"),
    pin(S730, H730, H730, 1, "bullet", "批24 本批不做的三条"),
]
meta = {"kind": "meta", "round": "R22", "spec": SPEC_REL,
        "note": "quote 由生成器从原文按节切片（不手抄）；status/attack/腿为人写",
        "scope": scope, "coverage": coverage}


def build(spec_rel: str, chars: int) -> list[dict]:
    """统一入口 `scripts/build-review-ledger.py r22` 调这里（切片锚点只认这一份 spec）。"""
    if spec_rel != SPEC_REL:
        raise SystemExit(f"r22 的锚点是 {SPEC_REL}，收到 {spec_rel}")
    return [meta] + items


if __name__ == "__main__":
    LEDGER.write_text("\n".join([json.dumps(meta, ensure_ascii=False)] +
                                [json.dumps(i, ensure_ascii=False) for i in items]) + "\n",
                      encoding="utf-8")
    print(f"写入 {LEDGER.name}：条目 {len(items)}，coverage {len(coverage)} 节，scope {len(scope)} 钉")
