#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""常驻电池的门：每一族的产物里必须写着"这轮读数测的是哪一笔字节"。

为什么立这道门（本轮修前现数，修后由 A5 的末行自己印）：取证落点迁进仓库树之后，
`docs/superpowers/specs/ledger/logs/` 下 15 个族共 562 份常驻日志、66 个轮次目录，按短语
`基线字节` 命中只有 P701 的 12 份、R30 的 1 份
⇒ 其余族的读数**在树里查不到它测于哪一笔提交**。两种失效各有形状，都得拦：
  ① 顺序失效——AiTrigger／DBPool 的驱动里本来就有这一行，却打印在 `tee_to()` **之前**：
     终端看得见、产物里没有（13/14 份产物零命中就是这么来的），改代码的人以为已经记了；
  ② 根本没有——其余驱动压根没这行，产物目录名只带墙钟戳。

两道轴，缺一不可：
  轴一（源码形状，便宜、放刀前就能判）：见 `check()` 的 A1/A2/A3。只看**代码行**——
      docstring 与行尾注释里的 `identity()`／`基线字节` 是散文，不是调用点，拿原文判会把
      已改对的驱动判成红（DBPool 的模块 docstring 第 22 行就是这句话）。
  轴二（产物实测，A4）：直接读树里该族**最近一轮**的产物，要求其中至少一份印着 `基线字节`。
      形状轴量不到的两件事由它兜住：跨代码路径的先后（`--check-tree` 那一支本就不落产物，
      身份行天然在 tee 之前）、以及"改了写路径却没跑过"（p703 族：证据写进树的能力是本轮新加的，
      没跑就没有产物）。只锁形状＝交付一份没验过的常驻件，所以 A4 才算闭合。

判据（逐枚驱动，一枚都不许跳过）：
  A1 证据打开点：`tee_to(` 或 `.write_text(` 在代码行里存在（本件的 `--check` 分流不许在 tee 之前 return）。
  A2 身份发射点：代码行里有 `identity(` 调用**或**字面 `基线字节`（P701/R30 用带自有上下文的 print，
      不强行换成助手函数——判据要能区分"没做"与"用别的方式做了"）；且产物打开之后还有一行发射：
      tee 型要求存在 `发射行号 > 首个 tee_to(`，write_text 型要求存在 `发射行号 < 某次 write_text(`。
  A3 取证落点：`.gitignore` 里有该族的**成对**例外行（目录行 + `**/*.log` 行）——只有一行等于
      "目录能建、文件不入库"，读数照样蒸发。
  A4 产物实测：该族在树里的最近一轮目录存在，且其中 ≥1 份文件印着 `基线字节`。
  A5 族归属对账：`logs/` 下每一族目录必须有归属——要么由某枚驱动反推出来（进 A4），要么在
      `NOT_A_BATTERY` 里写明"这族不是电池读数"的理由（空理由、过期条目、双身份都判红）。
      没有这一格，A4 的判据对象只来自驱动正文：树里长出一族没有驱动的目录时产物轴从来不看它，
      "两轴全绿"就成了对那假族的绿（本轮实测：磁盘 16 族、门只判 14 族，差的两族即由此登记补足）。
  A6 口径与机制对账（两半，缺一半都留假绿）：
    A6a 声明与机制相反——驱动把来树文件 `shutil.copy2()` 盖进克隆，却仍声明
        『来树未入库字节不进本轮读数』（`CLAIM_HEAD_ONLY`）。2026-09-28 复查抓到 B17action／P503／R22
        三枚正是这一形——那句话是**假身份行**，而 A4 只认"产物里有没有这句"，认不出这句是假的。
    A6b 机制无份数——同一形（`copy2` ＋ 落点从 `clone` 派生）却**一处现测份数都没有**：不传 `overlay=`，
        也没有手写的"已在 HEAD n/N"／"覆盖 N 个"。这一支比 A6a 更安静：它不撒谎，只是让身份行说不出
        本轮读了来树几份，读数从此无法复算（修 A6a 时现测：R30／P701／P703 三支全靠手写份数过关）。
        两支的正解同一条：让份数成为**读数**而不是声明（`identity(..., overlay=[...])` 或手写现测行）。

两份台账（判据对象之外的两批"确实没达标、但不该由本泳道代改"的常驻件，各带上界与过期判据）：
  D0–D4 债务台账（`DEFERRED`）：有代码内树内落点、但驱动里没有身份发射点的**他人泳道电池**——
      族照样实测，实测值只用来判条目是否过期（D4），不计入本门的合格／不合格。
  O1–O5 树外落点台账（`OFFTREE`）：**连落点都不在仓库树**的常驻电池（判读只印 stdout，或树内路径
      只在 docstring 里引用别人族的那份）。它们进不了 `drivers()`，于是两轴都看不见它们——
      O1 拦"盘上有、名单外"的静默排除，O2 上界，O3／O4 过期（改名／已补代码内落点），O5 空理由。
      末行把两个数一起印（进门 N 枚＋登记 M 枚＝候选现数），不许只报进门的那批冒充全量。
  A7 名字轴（对象集＝候选全量，不是只进门的那批）：**调用了本文件里根本没定义的名字**。
      它与前六类都不同轴——前面量"产物与口径"，这一轴量"这份脚本能不能跑到它那行判据"。
      `python3 -m py_compile` 只判语法，故这一族**编译绿**、跑到那行才 `NameError`；本机
      `pyflakes` 能抓，但它没注册进任何门（Makefile／workflows 现数 0 命中）＝全靠手动。
      立项读数（2026-09-28 二次合并）：对方在 `mut_db_poolcfg.py` 的 tee 之前加了一行
      ``print(f"基线字节：克隆 HEAD `{tip(clone)}`…")``，而 `def tip` 只在那棵树里（本树该文件
      现数 0 处）⇒ 照抄即把 4 刀电池变成一开跑就炸的死件。合并把它丢了，并按同一形状反向注码
      验证本轴有牙（注入 `tip_absent()` 到 `mut_hub_media_backfill.py` 第 333 行 ⇒ rc=1 点名）。

反向自测（`--selftest`）：形状轴的每一格都绑定"哪条判据分支该开火"，A4 用真临时目录做正反两格，
A5 用假集合做正反格（含"过期豁免"与"空理由"两支），A6 做两红七正控（含"该声明只在注释里 ⇒
抹散文后不许判红"与"就地注码的 copy2 ⇒ 不开火"，防止把改对了的驱动判坏），豁免那一族另配
"同形但产物没带着 ⇒ 照旧红"——否则这道门和它拦的那批驱动犯的是同一个错（恒报没问题）。
"""
from __future__ import annotations

import argparse
import ast
import builtins
import io
import re
import sys
import tempfile
from pathlib import Path
from tokenize import COMMENT, generate_tokens

ROOT = Path(__file__).resolve().parents[1]
LOGROOT = "docs/superpowers/specs/ledger/logs/"
PHRASE = "基线字节"
# mut_dispose.py 是"挡危险 --clone"的公共闸，本身不产证据，不在成员名单里。
EXCLUDE = {"mut_dispose.py"}

EMIT = re.compile(r"\bidentity\(|" + PHRASE)

# 身份行里那句"本轮只读克隆 HEAD"的口径短语。它是一句**声明**，而声明可以假：
# 只要同一枚驱动又把工作树文件 `copy2` 进克隆，这句话就与机制相反（A6a 判红）。
CLAIM_HEAD_ONLY = "不进本轮读数"
# "把来树字节盖进克隆"的机制形状：落点从 `clone` 派生 ＋ 一次 `shutil.copy2(`。
# 只认这两个形状的组合，不认 `clone` 这个词——b16 那三枚就地注码的驱动也有 `clone`
# 字样（0 处克隆内落点赋值），它们的 `copy2` 是把文件备份进 `bak/`，不是覆盖进克隆。
CLONE_DST = re.compile(r"=\s*clone\s*/")
# 覆盖一旦发生，份数必须**现测**进产物（否则读数无法复算）。三种已存在的写法都算合格：
# 走 `identity(..., overlay=[...])`、手写"本卡文件已在 HEAD n/N"、打印"覆盖 {len(...)} 个…"。
QUANTIFY = ("overlay=", "已在 HEAD", "覆盖 {")

# 树里确实有轮次目录、但**不是常驻电池**的族：它们的产物由人工执行的命令重定向而来，
# 没有一枚驱动可以挂身份行。A5（族归属对账）靠这份名单放行它们，理由必须写实——
# 空字符串等于"用一行代码关掉一格判据"。
NOT_A_BATTERY = {
    "CIwiring": "某几轮 `make audit`／逐门复跑的手工重定向读数，身份在正文的 SHA 叙述里，无驱动可挂",
    "Transcript": "check-secret-containment.py `--transcripts` 那一面的一次性普查读数（键名与次数），"
                  "产物由人工命令行重定向落盘，不是电池格子",
    "b-phase": "R22 复审泳道 b 阶段的**分车道 findings**（`lane-2-d7-gate-findings.md` 等），"
               "手写结论文件、由人工执行重定向落盘；提到它的三枚刀具（`mut_d7gate_r22lane.py`／"
               "`mut_outbound_claim_r22lane.py`／`mut_sentcache_r22lane.py`）都只在散文里引用它，"
               "前两者代码内没有仓库落点、第三枚的落点是 `R22-lanes`",
    "c-phase": "同泳道 c 阶段 findings，同上",
    "R22-closefinal": "R22 复审收口轮的逐门复跑输出与账本读数（人工命令行重定向），无电池驱动",
    "R22-phaseE": "R22 复审 E 阶段一次性读数（人工重定向），无电池驱动",
    "R22-protocol": "二次检查协议（second-review-protocol）自身的校准／演练读数，人工执行落盘",
    "R22closeout": "R22 收口轮的一次性补测读数，人工重定向，无驱动可挂",
    "R23retention": "R23 保留策略一次性核查读数，人工重定向，无驱动可挂",
    "R23dedup": "R23 入站去重首轮九格的逐格日志（`CONTROL.log`／`K1…K9-*.log`），由人工命令行"
                "重定向落盘；`mut_ingest_dedup_r23.py` 的代码里没有仓库内落点（只在 docstring 里"
                "引用这一族），故它不进成员名单——这条登记同时是本门的一处口径盲区：argv 指进树内"
                "的一次性读数不会有驱动归属",

    "R25full": "R25 全量复跑的一次性读数（不带格名），人工重定向，无驱动可挂",
    "R25full2": "R25 全量复跑第二趟的一次性读数，人工重定向，无驱动可挂",
    "R26media": "R26 媒体批次的一次性锚点／核查读数，人工重定向，无驱动可挂",
    "R27media": "R27 媒体批次（协程快照站点普查）的一次性读数，人工重定向，无驱动可挂",
    "calib": "53300 连接容量标定趟（`cap-ab-53300-probe.py` 之前的手工容器标定），人工重定向",
    "calib2": "同上的第二趟标定，人工重定向",
    "dentproof-anchor": "锚点预检的一次性\"凹痕证明\"读数（证明参数真进了装架），人工重定向",
    "wiring-anchor": "门禁注册接线核查的一次性锚点读数，人工重定向",
}

# 合并带进来的**他人泳道**常驻电池：它们确实把逐格产物写进仓库树，但驱动里没有身份发射点
# （有的连族目录都还没在树里长出来）。补齐要改别人的驱动＋重跑别人的电池（每枚一趟 Go/vitest
# 全族），不属于本轮范围；这份登记是**债务台账**而不是放行开关：
#   · 条目必须写实（族名要与该驱动代码内落点对得上、理由不许空）；
#   · 数量有上界（`DEFERRED_MAX`）⇒ 新增一枚无身份行的常驻电池照样红，只能减不能加；
#   · 一旦驱动补了发射点、或该族最近一轮产物实测到 `基线字节`，条目即过期 ⇒ 判红，必须删。
DEFERRED = {
    "mut_send_verify_b24.py": (
        "B24sendverify",
        "无身份发射点；树里 `run2-16cells` 18 份产物按短语命中 0 份 ⇒ 读数测于哪一笔字节无从查起"),
    "mut_dedupkey_shape_r22.py": (
        "R22dedupkey",
        "无身份发射点；族目录只有 4 份无戳的一次性产物 ⇒ 合入门禁前需该泳道自补 `battlog.identity`"),
    "mut_review_r22_teeth.py": (
        "R22-teeth",
        "无身份发射点（旧规则把它 docstring 里引用的 `logs/R25` 当成族，判错了对象）；"
        "该族 10 份产物按短语命中 0 份"),
    "mut_sse_ack_r23.py": (
        "R23sseack",
        "无身份发射点；`first-run` 7 份产物按短语命中 0 份"),
    "mut_bad_case_p803.py": (
        "P803",
        "驱动已有身份发射点，但取证落点从未迁进仓库树（族目录不存在）且 `.gitignore` 无成对例外 ⇒ "
        "迁树＋开例外后要实测这一族的最近一轮产物"),
    "mut_sentcache_r22lane.py": (
        "R22-lanes",
        "无身份发射点；该族是**平铺落点**（驱动把逐格日志直接写进族目录、没有轮次子目录），"
        "31 份产物按短语命中 0 份"),
}
DEFERRED_MAX = 6
# 债务登记的族也算"有归属"（A5），但不进 A4 实测（实测必红，红的是别人的债）。
TS_ROUND = re.compile(r"^\d{8}-\d{6}$")

# **树外落点台账**：合并带进来的另 18 枚 `scripts/mut_*.py` 常驻电池，代码行里没有仓库内取证落点
# （14 枚通篇不提 `LOGROOT`，判读只印到 stdout；4 枚只在 docstring 里引用**别人族**里人工重定向存的
# 一份读数）。它们因此进不了 `drivers()`——两轴都没有判据对象。这**不等于它们合格**：
# 从前这里是一个静默的 `return False`，末行只印"20 枚驱动"，读报告的人以为树里的常驻电池全被判过，
# 实际是 37 枚里 18 枚从没进过门。补齐要改该泳道驱动＋重跑全族（每枚一趟 Go/vitest），不属本轮；
# 这份登记是债务台账而不是放行开关：
#   · 盘上冒出一枚既没进门、又没登记 ⇒ O1 开火（静默排除这一形本身被拦下）；
#   · 数量有上界（`OFFTREE_MAX`）⇒ 只能减不能加；
#   · 条目里的驱动一旦补了**代码内**落点（自动进门），或文件已不在盘上，条目即过期 ⇒ 判红必须删。
OFFTREE = {
    "mut_brain_action_b19h.py": "判读只印 stdout、代码内无树内落点（B19h 扩展动作批）",
    "mut_command_log_ok_b20b.py": "判读只印 stdout、代码内无树内落点（B20b 指令日志批）",
    "mut_cron_err_b19c.py": "判读只印 stdout、代码内无树内落点（B19c 定时任务错误批）",
    "mut_d7verdict_b20g.py": "判读只印 stdout、代码内无树内落点（B20g D7 判定批）",
    "mut_dependency_b19.py": "判读只印 stdout、代码内无树内落点（B19 依赖批）",
    "mut_dingtalk_msgid_r22lane.py": "判读只印 stdout、代码内无树内落点（R22 泳道钉钉 msgId 臂）",
    "mut_extension_auth_b19h.py": "判读只印 stdout、代码内无树内落点（B19h 扩展鉴权批）",
    "mut_host_gate_b19f.py": "判读只印 stdout、代码内无树内落点（B19f Host 闸门批）",
    "mut_prune_batch_b19g.py": "判读只印 stdout、代码内无树内落点（B19g 清理批）",
    "mut_push_budget_b20d.py": "判读只印 stdout、代码内无树内落点（B20d 推送预算批）",
    "mut_session_err_b19d.py": "判读只印 stdout、代码内无树内落点（B19d 会话错误批）",
    "mut_step_cap_b19e.py": "判读只印 stdout、代码内无树内落点（B19e 步长上限批）",
    "mut_submit_enter_b18.py": "判读只印 stdout、代码内无树内落点（B18 提交回车批）",
    "mut_write_claim_a12.py": "判读只印 stdout、代码内无树内落点（A12 写回声明批）",
    "mut_d7gate_r22lane.py": "代码内无落点；docstring 引用的 `logs/b-phase/lane-2-d7-gate-findings.md`"
                             " 是**别人族**里人工重定向的分析件，不是本驱动的产物",
    "mut_outbound_claim_r22lane.py": "代码内无落点；docstring 引用的 `logs/b-phase/lane-5-outbound-ack-findings.md`"
                                     " 同上（旧规则正是把它判成 `b-phase` 族、张冠李戴的那一枚）",
    "mut_ingest_dedup_r23.py": "代码内无落点；docstring 引用的 `logs/R23dedup/` 九格是首轮人工重定向的读数",
    "mut_retention_a6.py": "代码内无落点；树里那份 `logs/R22-lanes/mut_retention_a6-d23-cell-r22close.log`"
                           " 是 stdout 重定向的单份读数，非驱动自己写盘",
}
OFFTREE_MAX = 18


def offtree_accounting(candidates: list, admitted: set, registry: dict, cap: int) -> list:
    """树外落点登记的四条要求（返回判红说明）：不许有残骸、不许变大、条目不许过期、理由不许空。

    与 `debt_accounting` 同一套牙齿，但对象是**没进门的那批**：`admits()` 判 False 是"两轴无对象"，
    不是一个绿读数。少了 O1，门可以把任意多枚常驻件安静地留在名单外；少了 O3／O4，登记就成了永久豁免。
    """
    bad = []
    unlisted = sorted({p.name for p in candidates} - admitted - set(registry))
    for name in unlisted:
        bad.append(f"O1 `{name}` 在盘上、既没进门也没登记 ⇒ 静默排除（本门从头到尾没看过它）；"
                   "补 `battlog.identity`＋树内落点后自动进门，否则必须写进 `OFFTREE` 并说明为什么")
    if len(registry) > cap:
        bad.append(f"O2 树外落点登记 {len(registry)} 枚 > 上界 {cap} ⇒ 登记面只许减不许增")
    on_disk = {p.name for p in candidates}
    for name, reason in sorted(registry.items()):
        if name not in on_disk:
            bad.append(f"O3 条目 `{name}` 已不在 `scripts/mut_*.py` 里 ⇒ 登记过期，删条目")
            continue
        if name in admitted:
            bad.append(f"O4 条目 `{name}` 过期：该驱动现在已有代码内树内落点、已自动进门 ⇒ 删条目，"
                       "让 A1／A2／A4 直接判它")
            continue
        if not reason.strip():
            bad.append(f"O5 条目 `{name}` 理由为空 ⇒ 豁免不许留空条目")
    return bad


def debt_accounting(deferred: dict, fam_by_driver: dict, verdicts: dict, cap: int) -> list:
    """债务登记的三条要求：不许变大、理由不许空、补齐了就必须删（返回判红的说明）。

    没有这一格，登记就成了"用一张表关掉一格判据"：上界管住新增，族名对账管住指错对象，
    产物实测管住过期条目——该泳道一旦补了身份行并跑出一轮带戳产物，条目不删就判红。
    """
    bad = []
    if len(deferred) > cap:
        bad.append(f"D0 债务登记 {len(deferred)} 枚 > 上界 {cap} ⇒ 登记面只许减不许增，"
                   "新增一枚无身份行的常驻电池应当去补 `battlog.identity`，不是加条目")
    for name, (fam, reason) in sorted(deferred.items()):
        if name not in fam_by_driver:
            bad.append(f"D1 条目 `{name}` 已不在成员名单 ⇒ 登记过期（驱动改名或落点迁走），删条目")
            continue
        if not reason.strip():
            bad.append(f"D2 条目 `{name}` 理由为空 ⇒ 豁免不许留空条目")
        if fam not in fam_by_driver[name]:
            bad.append(f"D3 条目 `{name}` 登记的族 `{fam}` 与其代码内落点"
                       f"（{', '.join(sorted(fam_by_driver[name])) or '无'}）不符 ⇒ 指错了对象")
        if verdicts.get(fam, ("", ""))[0] == "有身份行":
            bad.append(f"D4 条目 `{name}` 过期：族 `{fam}` 最近一轮产物已实测到身份行 ⇒ 删条目，"
                       "让 A4 直接判它")
    return bad


def families_of(code) -> set:
    """一枚驱动**代码行**里声明的仓库内取证落点族名（可以有多枚）。

    只认代码行，与 A2 那条"散文不是发射点"同源：合并带进来的 `mut_sentcache_r22lane.py`
    在 docstring 里引用了 `logs/b-phase/lane-5-outbound-ack-findings.md`（它复述的上一轮承诺），
    旧规则取正文第一处命中 ⇒ 把**别人族**当自己的落点判 A3／A4，而它真正的
    `LOGDIR = …/logs/R22-lanes` 从来没进过这道门。另四枚（`mut_d7gate_r22lane.py` 等）更极端：
    全篇只有 prose 引用、代码里根本没有仓库内落点 ⇒ 两轴都没有判据对象，交给 `OFFTREE` 台账
    （它们是常驻件，只是取证在 stdout／别人族里，**不是合格**）。
    """
    out = set()
    for _, ln in code:
        for m in re.finditer(re.escape(LOGROOT) + r"([A-Za-z0-9_-]+)", ln):
            out.add(m.group(1))
    return out



def tree_families() -> list:
    """`logs/` 下现存的族目录名（轴二的地面集合，不来自任何名单）。"""
    base = ROOT / LOGROOT
    if not base.is_dir():
        return []
    return sorted(p.name for p in base.iterdir() if p.is_dir())


def accounting(covered: set, registered: dict, on_disk: list) -> list:
    """A5：树里每一族必须有归属——要么是某枚驱动的取证落点，要么在 `NOT_A_BATTERY` 里写明理由。

    这一格管的是**门的覆盖集合与磁盘集合之差**：A4 的族是从驱动正文反推出来的，
    树里长出一族没有驱动登记的目录（比如有人把一次性读数 `tee` 进 `logs/`），
    整条 A4 就从来不看它——"两轴全绿"于是成了对一族产物的假证。
    """
    bad = []
    for fam in on_disk:
        if fam in covered:
            continue
        reason = registered.get(fam)
        if reason is None:
            bad.append(f"A5 族 `{fam}` 在树里有轮次目录，却既无驱动归属、也未登记为『非电池』"
                       "⇒ 本门的产物轴从来不看它")
        elif not reason.strip():
            bad.append(f"A5 族 `{fam}` 登记为非电池但理由为空 ⇒ 豁免不许留空条目")
    for fam in sorted(set(registered) - set(on_disk)):
        bad.append(f"A5 非电池名单里的 `{fam}` 在树里已没有轮次目录 ⇒ 豁免条目过期，要么删要么改成驱动族")
    for fam in sorted(set(registered) & covered):
        bad.append(f"A5 族 `{fam}` 既有驱动归属又登记为非电池 ⇒ 两种身份不能同时成立")
    return bad


def candidates() -> list[Path]:
    """盘上全部常驻电池候选（`mut_*.py` 去 `EXCLUDE` ＋ 探针件），**不做任何筛选**。

    分成两数报（`drivers()` 那一份进两轴判、这一份用来查静默排除）：从前只有 `drivers()`，
    "成员名单现取"听起来像全覆盖，实际是"覆盖到代码行里声明了树内落点的那些"。
    """
    out = [p for p in sorted((ROOT / "scripts").glob("mut_*.py")) if p.name not in EXCLUDE]
    probe = ROOT / "scripts/cap-ab-53300-probe.py"
    if probe.exists():
        out.append(probe)
    return out


def drivers() -> list[Path]:
    """进门的那批：自己**在代码行里**声明了仓库内取证落点的常驻电池。

    判据来自"它自己声明了仓库内的取证落点"（`families_of` 在代码行里找）——只有散文引用不算，
    否则会把**别人族**里的读数当自己的落点判 A3／A4（合并带入的 `mut_sentcache_r22lane.py` 正是这一形）。
    **解析失败的照旧进门**：读不懂代码行就判不了落点，向红偏置。
    落点不在树的那些**不是合格**，它们进 `OFFTREE` 台账，由 `offtree_accounting` 的 O1–O5 说话。
    """
    return [p for p in candidates() if admits(p)]


def admits(p: Path) -> bool:
    text = p.read_text(encoding="utf-8")
    if LOGROOT not in text:
        return False
    code = code_lines(text)
    return code is None or bool(families_of(code))


def code_lines(text: str):
    """把 docstring 与行尾注释抹成空行，返回 `[(行号, 代码文本)]`（行号不变，便于点名）。

    抹不掉或解析不了 ⇒ 返回 `None`：这道门对"读不懂的驱动"向红偏置，绝不静默放行。
    """
    lines = text.splitlines()
    blanked = [False] * (len(lines) + 2)
    try:
        tree = ast.parse(text)
    except SyntaxError:
        return None
    for node in ast.walk(tree):
        body = getattr(node, "body", None)
        # `Lambda.body` 是表达式不是语句列表 ⇒ 只认 list 型 body，否则 `body[0]` 直接 TypeError
        if not isinstance(body, list) or not body:
            continue
        first = body[0]
        if (isinstance(first, ast.Expr) and isinstance(first.value, ast.Constant)
                and isinstance(first.value.value, str)):
            for i in range(first.lineno, min(first.end_lineno, len(lines)) + 1):
                blanked[i] = True
    try:
        for tok in generate_tokens(io.StringIO(text).readline):
            if tok.type == COMMENT:
                ln = tok.start[0]
                if 1 <= ln <= len(lines) and not blanked[ln]:
                    lines[ln - 1] = lines[ln - 1][: tok.start[1]]
    except Exception:
        return None
    return [(i, ln) for i, ln in enumerate(lines, 1) if not blanked[i]]


def gitignore_pairs() -> set:
    """`.gitignore` 里已开例外的族名集合，且必须是**成对**（目录行与 `**/*.log` 行都在）。

    只出现一次的族不算数：`!…/logs/X/` 放行目录、`!…/logs/X/**/*.log` 才放行文件，
    缺后者等于"每轮建了目录、里面一份不入库"。
    """
    text = (ROOT / ".gitignore").read_text(encoding="utf-8")
    dirs, logs = set(), set()
    for line in text.splitlines():
        m = re.fullmatch(r"!docs/superpowers/specs/ledger/logs/([^/]+)/", line.strip())
        if m:
            dirs.add(m.group(1))
        m = re.fullmatch(r"!docs/superpowers/specs/ledger/logs/([^/]+)/\*\*/\*\.log", line.strip())
        if m:
            logs.add(m.group(1))
    return dirs & logs


def check(code, pairs: set, family, order_cleared: bool = False) -> list:
    """一枚驱动的轴一判据：返回"为什么这一枚不合格"的行列表（空＝合格）。

    `code` 必须是 `code_lines()` 的输出（散文已抹）；`None` 由调用方先挡掉。
    两种"产物打开"的形状各自要求相反的先后：
      · tee 型（`tee_to()` 之后 `print` 才进得了日志）⇒ 发射行必须**晚于**首个 tee；
      · write_text 型（探针件把行收集进 `lines` 再整体落盘）⇒ 发射行必须**早于**某次写盘。
    拿同一把尺量两种形状，就会把对的判成红、把错的判成绿。

    `order_cleared`：该族最近一轮**产物**实测已带着身份行。行号先后量不到"发射语句在某个函数里、
    而那函数在 tee 之后才被调用"（P701 就是这一形：打印在 `prepare()` 内，`main()` 先 tee 再调它）。
    这时由轴二作证据豁免——只豁免先后这一支；缺发射点、缺打开点、缺成对例外都不豁免。
    """
    bad = []
    # `family` 可以是一枚族名（预检自测里的单形输入）也可以是 `families_of()` 的集合。
    fams = {family} if isinstance(family, str) else set(family)
    tee = [i for i, ln in code if "tee_to(" in ln]
    wr = [i for i, ln in code if ".write_text(" in ln]
    emit = [i for i, ln in code if EMIT.search(ln)]
    if not emit:
        bad.append(f"A2 代码行里没有身份发射点（`identity(` 或字面 `{PHRASE}`）："
                   "产物里查不到这轮读的是哪一笔字节")
    elif tee:
        if not any(e > min(tee) for e in emit):
            bad.append(f"A2 顺序失效：tee 之后没有任何身份发射行（首个 `tee_to(` 在第 {min(tee)} 行，"
                       f"发射行在 {emit}）⇒ 身份行早于产物打开，只进终端不进日志")
    elif wr:
        if not any(e < w for e in emit for w in wr):
            bad.append(f"A2 顺序失效：身份发射行 {emit} 不早于任何一次 `.write_text(` {wr}"
                       "⇒ 身份行没赶上那份产物")
    else:
        bad.append("A1 代码行里既不 `tee_to(` 也不 `.write_text(`：产物打开点找不到，身份行无处可落")
    # A3 对**每一枚**代码内落点族都判（一枚驱动写两族时，第二族不许因为"第一族有成对例外"就过关）。
    for fam in (sorted(fams) if fams else [family]):
        if fam and fam not in pairs:
            bad.append(f"A3 族 `{fam}` 在 `.gitignore` 里没有成对例外行（目录行 + `**/*.log` 行）")
    # A6：身份行的**口径**不许与装架机制相反。2026-09-28 复查抓到三枚驱动（B17action／P503／R22）
    # 一边把 `ROOT/rel` 用 `shutil.copy2()` 盖进克隆、一边在 `extra` 里写"来树未入库字节不进本轮
    # 读数"——A4 量"有没有这句"，量不到"这句是不是假的"，所以这句话必须靠形状对账才守得住。
    text = "\n".join(ln for _, ln in code)
    overlaid = "shutil.copy2(" in text and CLONE_DST.search(text) is not None
    if overlaid and CLAIM_HEAD_ONLY in text:
        bad.append(f"A6a 装架把来树文件覆盖进克隆（`shutil.copy2(`）却声明『{CLAIM_HEAD_ONLY}』"
                   "⇒ 身份行与机制相反；改传 `identity(..., overlay=[...])` 让份数现测")
    if overlaid and not any(m in text for m in QUANTIFY):
        bad.append("A6b 覆盖来树字节进了克隆，全驱动却没有一个现测份数"
                   f"（`{'`／`'.join(QUANTIFY)}` 三者皆无）"
                   "⇒ 身份行说不出本轮究竟读了来树几份，读数无法复算")
    if order_cleared:
        bad = [b for b in bad if "顺序失效" not in b]
    return bad


BUILTIN_NAMES = set(dir(builtins))


def undefined_calls(text: str) -> list:
    """A7：调用了本文件里**根本没定义**的名字（AST 名表并集，返回不合格行；空＝合格）。

    为什么补这一轴：`python3 -m py_compile` 只判语法——2026-09-28 二次合并带进 `mut_db_poolcfg.py`
    的一行 `print(f"基线字节：克隆 HEAD `{tip(clone)}`…")` 编译**绿**（实测注入副本 py_compile rc=0），
    跑到那一行才 NameError；而 `def tip` 只存在于对方那棵树（本树该文件现数 0 处、`git show
    2c765be1:scripts/mut_db_poolcfg.py` 那份 1 处）。本机 `pyflakes` CLI 能抓这一族（实测报
    `undefined name 'tip'`），但它没注册进任何门（`grep -rn 'pyflakes|flake8|ruff' Makefile
    .github/workflows/*.yml` 现数 0 命中）⇒ 全靠人手动跑＝等于没有。AST 只用标准库，CI 与本地同一条判据。

    名表取**全树并集**（任何 `def`/`class`、`import`、`Store` 位置的 `Name`、函数与 lambda 形参、
    `except … as`、`global`/`nonlocal`）：这样"局部变量当函数调"那类真缺陷会**漏判**，而"名字在文件
    别处定义"绝不**误伤**——向漏判偏置、不向假红偏置（共享树上判据宁窄勿宽）。属性调用
    （`p.read_text(`、`re.compile(`）不在判据内：名字归谁要看接收者，不是本文件的事。
    """
    try:
        tree = ast.parse(text)
    except SyntaxError as exc:
        return [f"A7 解析失败（ast.parse：{exc.msg} 第 {exc.lineno} 行）⇒ 名字轴读不到，向红偏置"]
    defined = set()
    for n in ast.walk(tree):
        if isinstance(n, (ast.FunctionDef, ast.AsyncFunctionDef, ast.ClassDef)):
            defined.add(n.name)
        elif isinstance(n, (ast.Import, ast.ImportFrom)):
            for a in n.names:
                defined.add((a.asname or a.name).split(".")[0])
        elif isinstance(n, ast.Name) and isinstance(n.ctx, ast.Store):
            defined.add(n.id)
        elif isinstance(n, ast.arg):
            defined.add(n.arg)
        elif isinstance(n, ast.ExceptHandler) and n.name:
            defined.add(n.name)
        elif isinstance(n, (ast.Global, ast.Nonlocal)):
            defined.update(n.names)
    bad = []
    for n in ast.walk(tree):
        if isinstance(n, ast.Call) and isinstance(n.func, ast.Name):
            f = n.func.id
            if f not in defined and f not in BUILTIN_NAMES:
                bad.append(f"A7 第 {n.lineno} 行调用本文件里未定义的名字 `{f}`"
                           "⇒ 编译绿、跑到那行才炸（合并带进来的悬空引用正是这一形）")
    return bad


def artifact_verdict(family: str):
    """轴二：该族最近一轮产物里到底有没有那句身份行。返回 `(判定, 说明)`。"""
    fam = ROOT / LOGROOT / family
    if not fam.is_dir():
        return "无轮次目录", f"{LOGROOT}{family}/ 不存在 ⇒ 本族的常驻产物从未落进仓库树"
    rounds = sorted((p for p in fam.iterdir() if p.is_dir()), key=lambda p: p.name)
    if not rounds:
        # 平铺落点（`R22-lanes` 那一形：驱动把逐格日志直接写进族目录，没有轮次子目录）：
        # 产物**在树里**，量不到"最近一轮"不等于没证据 ⇒ 直接量族目录里的文件。
        flat = [p for p in fam.iterdir() if p.is_file()]
        if flat:
            hit = [p.name for p in flat if PHRASE in p.read_text(encoding="utf-8", errors="replace")]
            if not hit:
                return "缺身份行", (f"平铺落点（无轮次目录）共 {len(flat)} 份产物，"
                                    f"按短语 `{PHRASE}` 命中 0 份 ⇒ 这轮读数测于哪一笔字节无从查起")
            return "有身份行", (f"平铺落点（无轮次目录）共 {len(flat)} 份产物，"
                                f"{len(hit)} 份带 `{PHRASE}`（{', '.join(sorted(hit)[:3])}"
                                f"{' 等' if len(hit) > 3 else ''}）")
        return "无轮次目录", f"{LOGROOT}{family}/ 下没有任何轮次目录"
    # "最近一轮"必须优先认**带时间戳**的目录：`20260928-141245` 这类按名字序＝按时间序，
    # 而合并带进来的手工命名轮次（`closeout`／`first-run`／`run2-16cells`）字母序排在数字之后，
    # 旧规则取末尾 ⇒ 把本族真读数遮掉（本轮实测：R22 族带着身份行的 `20260928-141245`
    # 被判成"最近一轮 `closeout` 命中 0 份"）。全族都没有带戳轮次时才回退名字序。
    dated = [p for p in rounds if TS_ROUND.match(p.name)]
    last = dated[-1] if dated else rounds[-1]
    undated = len(rounds) - len(dated)
    files = [p for p in last.rglob("*") if p.is_file()]
    hit = [p.name for p in files if PHRASE in p.read_text(encoding="utf-8", errors="replace")]
    if not hit:
        return "缺身份行", (f"最近一轮 `{last.name}` 共 {len(files)} 份产物，"
                            f"按短语 `{PHRASE}` 命中 0 份 ⇒ 这轮读数测于哪一笔字节无从查起")
    return "有身份行", (f"最近一轮 `{last.name}` 共 {len(files)} 份产物，"
                        f"{len(hit)} 份带 `{PHRASE}`（{', '.join(sorted(hit)[:3])}"
                        f"{' 等' if len(hit) > 3 else ''}）"
                        + (f"｜另有 {undated} 个无时间戳轮次不参与『最近』判定" if undated else ""))


def selftest() -> int:
    """反向格各自绑定"哪条判据分支该开火"，只判关键字会在拧错地方的时候假绿。"""
    cases = [
        ("T1 缺身份行 ⇒ A2 开火（不许因为 tee 在就放行）",
         "tee_to(LOGDIR / '00-run.log')\nprint('判定')\n", {"X"}, "X", "A2 代码行里没有身份发射点"),
        ("T2 身份行早于 tee ⇒ A2 顺序分支开火",
         "print(identity(ROOT))\ntee_to(LOGDIR / '00-run.log')\n", {"X"}, "X", "顺序失效"),
        ("T3 族只有目录行 ⇒ A3 开火（目录能建、文件不入库）",
         "tee_to(LOGDIR / '00-run.log')\nidentity(ROOT)\n", set(), "X", "A3 族"),
        ("T4 正控制：tee 在前、身份在后、族成对 ⇒ 合格",
         "tee_to(LOGDIR / '00-run.log')\nidentity(ROOT)\n", {"X"}, "X", ""),
        ("T5 只有 tee 没有身份行 ⇒ A2 开火（本族的原始失效形状）",
         "tee_to(LOGDIR / '00-run.log')\n", {"X"}, "X", "A2 代码行里没有身份发射点"),
        ("T6 write_text 型：身份行早于写盘 ⇒ 合格（探针件的正当形状）",
         "lines.append(identity(ROOT))\nsummary.write_text(x)\n", {"X"}, "X", ""),
        ("T7 write_text 型：身份行排在**所有**写盘之后 ⇒ 顺序分支开火",
         "a.write_text(x)\nb.write_text(y)\nlines.append(identity(ROOT))\n", {"X"}, "X", "顺序失效"),
        ("T8 既无 tee 也无 write_text ⇒ A1 开火（身份行无处可落）",
         "identity(ROOT)\n", {"X"}, "X", "A1 代码行里既不"),
        ("T9 tee 与 write_text 都有、身份在 tee 之前 ⇒ 顺序分支开火（混形不许放行）",
         "identity(ROOT)\ntee_to(LOGDIR / '00-run.log')\nsummary.write_text(x)\n", {"X"}, "X",
         "顺序失效"),
        # —— 散文不许当调用点（本轮实测踩到的两类假阳）——
        ("T10 `identity(` 只出现在行尾注释里 ⇒ A2 开火（注释不是调用点）",
         "tee_to(LOGDIR / '00-run.log')\nprint(x)  # 这里补一行 identity(ROOT) 的说明\n",
         {"X"}, "X", "A2 代码行里没有身份发射点"),
        ("T11 `基线字节` 只在模块 docstring 里 ⇒ A2 开火（散文不是发射点）",
         '"""驱动说明：基线字节＝克隆 HEAD，身份行由 identity() 打印。\n第二行"""\n'
         "tee_to(LOGDIR / '00-run.log')\nprint('判定')\n", {"X"}, "X",
         "A2 代码行里没有身份发射点"),
        ("T12 正控制：docstring 提到身份行、代码里 tee 在前 print 在后 ⇒ 合格",
         '"""说明：本件在 tee 之后打印 基线字节。\n第二行"""\n'
         "tee_to(LOGDIR / '00-run.log')\nprint('基线字节：克隆 HEAD ' + head)\n", {"X"}, "X", ""),
        ("T13 另一条代码路径上的身份行不许替 tee 路径作证 ⇒ 顺序分支仍开火",
         "if args.check_tree:\n    identity(ROOT)\n    return do_check()\n"
         "tee_to(LOGDIR / '00-run.log')\nprint('判定')\n", {"X"}, "X", "顺序失效"),
        # 本轮实测的门自身缺陷：`Lambda.body` 是表达式不是语句列表，`body[0]` 直接 TypeError。
        # 门崩了不等于门红了——这一格要求带 lambda 的驱动照样判得动。
        ("T14 驱动里有 `lambda: …` ⇒ 门不崩且照常判（正控制）",
         "HANDLERS = {'a': lambda: run()}\ntee_to(LOGDIR / '00-run.log')\nprint('基线字节')\n",
         {"X"}, "X", ""),
    ]
    failed = 0
    # `ran` 是**跑过一格记一格**的现数。汇总行从前是 `len(cases) + 3 + len(ex) + …`：那个写死的
    # `3` 对应轴二内联块，而那块本轮实测已经有 **6** 格（T15–T17＋T35–T37）⇒ 2026-09-28 现跑
    # 实际印 55 行 ✓、末行却说 52 格。自报格数比真跑的少＝"自测覆盖面"这个读数在说谎，
    # 与本节第 17 段抓到的收尾闸"九格"同一族，改成由格子自己计数。
    ran = 0

    def say(cond_ok: bool, msg: str) -> None:
        nonlocal ran, failed
        ran += 1
        if cond_ok:
            print(f"  ✓ {msg}")
        else:
            print(f"  ✗ {msg}")
            failed += 1

    for name, src, pairs, family, want in cases:
        ran += 1
        code = code_lines(src)
        bad = check(code, pairs, family) if code is not None else ["解析失败"]
        joined = " / ".join(bad)
        if want and want not in joined:
            print(f"  ✗ {name}：期望点名『{want}』，实际 {joined or '（判为合格）'}")
            failed += 1
        elif not want and bad:
            print(f"  ✗ {name}：正控制被判红：{joined}")
            failed += 1
        else:
            print(f"  ✓ {name}")

    # 轴二的六格（原先在汇总行里写死成 `+ 3`，那是立项时的格数）：A4 用真临时目录证明
    # "没目录／有目录无身份行／有身份行／无戳目录遮蔽／全族无戳回退／平铺落点"各有不同判定。
    global ROOT
    real_root = ROOT
    with tempfile.TemporaryDirectory() as td:
        try:
            ROOT = Path(td)
            empt = artifact_verdict("NoSuchFamily")
            say("无轮次目录" in empt[0],
                f"T15 族目录不存在 ⇒ A4 报『无轮次目录』（不是放行），实测 {empt[0]}")
            fam = Path(td) / LOGROOT / "X"
            (fam / "r1").mkdir(parents=True)
            (fam / "r1" / "00-run.log").write_text("判定：全杀\n", encoding="utf-8")
            got = artifact_verdict("X")
            say("缺身份行" in got[0],
                f"T16 最近一轮无身份行 ⇒ A4 报『缺身份行』，实测 {got[0]}")
            (fam / "r2").mkdir()
            (fam / "r2" / "00-run.log").write_text("基线字节：`abc1234`｜未入库字节 0 处\n",
                                                   encoding="utf-8")
            got = artifact_verdict("X")
            say("有身份行" in got[0],
                f"T17 正控制：最近一轮带身份行 ⇒ A4 放行，实测 {got[0]}")
            # 本轮合并实测到的门自身缺陷：无时间戳目录（`closeout`）按名字序排在 `2026…` 之后，
            # 旧规则取末尾 ⇒ 把带着身份行的真读数遮掉。
            (fam / "closeout").mkdir()
            (fam / "closeout" / "00-run.log").write_text("判定：全杀\n", encoding="utf-8")
            got = artifact_verdict("X")
            say("有身份行" in got[0] and "r2" in got[1],
                "T35 带时间戳轮次优先（无戳的 `closeout` 不遮蔽真读数）"
                f"（应认 r2，实测 {got[0]}／{got[1]}）")
            named = Path(td) / LOGROOT / "Y"
            (named / "first-run").mkdir(parents=True)
            (named / "first-run" / "00-run.log").write_text("判定：全杀\n", encoding="utf-8")
            got = artifact_verdict("Y")
            say("缺身份行" in got[0] and "first-run" in got[1],
                "T36 全族无带戳轮次 ⇒ 回退名字序取 `first-run`"
                f"（实测 {got[0]}／{got[1]}）")
            flat = Path(td) / LOGROOT / "Z"
            flat.mkdir(parents=True)
            (flat / "K1.log").write_text("判定：杀掉\n", encoding="utf-8")
            got = artifact_verdict("Z")
            say("缺身份行" in got[0] and "平铺落点" in got[1],
                "T37 平铺落点按族目录内的文件实测（`R22-lanes` 那一形，不许报『无轮次目录』）"
                f"（实测 {got[0]}／{got[1]}）")
        finally:
            ROOT = real_root

    # 轴一"先后"那一支的豁免（order_cleared）：轴二实测到该族最近一轮产物已带着身份行。
    # 三格合起来证明豁免是**有界的**——只撤"先后"，缺发射点/缺成对例外照样红。
    ex_src = "def prepare(tmp):\n    print('基线字节：克隆 HEAD ' + head)\n" \
             "def main():\n    tee_to(LOGDIR / '00-run.log')\n    prepare(tmp)\n"
    ex = [
        ("T18 正控制（P701 那一形）：发射在函数体内、调用点在 tee 之后 ⇒ 产物实测豁免'先后'",
         check(code_lines(ex_src), {"X"}, "X", order_cleared=True), []),
        ("T19 同一形状但产物没带着 ⇒ '先后'照旧红（豁免只认轴二的事实）",
         check(code_lines(ex_src), {"X"}, "X", order_cleared=False), ["顺序失效"]),
        ("T20 豁免不许顺手撤掉 A3：族无成对例外 ⇒ 仍红",
         check(code_lines(ex_src), set(), "X", order_cleared=True), ["A3 族"]),
        ("T21 豁免不许撤掉'根本没有发射点'：只有 tee ⇒ 仍红",
         check(code_lines("tee_to(LOGDIR / '00-run.log')\n"), {"X"}, "X",
               order_cleared=True), ["A2 代码行里没有身份发射点"]),
    ]
    for name, got, want in ex:
        ran += 1
        joined = " / ".join(got)
        if all(w in joined for w in want) and (want or not got):
            print(f"  ✓ {name}")
        else:
            print(f"  ✗ {name}：期望 {want}，实际 {joined or '（判为合格）'}")
            failed += 1

    # A5（族归属对账）：判据对象是"门看到的族集合"与"磁盘上的族集合"之差。
    # 这四格各自绑定一支：未登记的新族／登记了但树里已没有（过期豁免）／理由为空／双身份。
    ac = [
        ("T22 树里长出无驱动、无登记的族 ⇒ A5 开火（A4 从来不看它）",
         accounting({"X"}, {"Y": "非电池的理由"}, ["X", "Y", "Z"]), ["A5 族 `Z`"]),
        ("T23 正控制：每族要么有驱动要么有带理由的登记 ⇒ 合格",
         accounting({"X"}, {"Z": "一次性普查读数"}, ["X", "Z"]), []),
        ("T24 登记的族在树里已消失 ⇒ A5 开火（豁免条目过期）",
         accounting({"X"}, {"Z": "曾经的一次性读数"}, ["X"]), ["A5 非电池名单里的 `Z`"]),
        ("T25 理由为空 ⇒ A5 开火（拿空串关掉一格）",
         accounting(set(), {"Z": "   "}, ["Z"]), ["理由为空"]),
        ("T25b 同一族既有驱动又登记为非电池 ⇒ A5 开火（双身份）",
         accounting({"Z"}, {"Z": "既是电池又登记"}, ["Z"]), ["两种身份"]),
    ]
    for name, got, want in ac:
        ran += 1
        joined = " / ".join(got)
        if all(w in joined for w in want) and (want or not got):
            print(f"  ✓ {name}")
        else:
            print(f"  ✗ {name}：期望 {want}，实际 {joined or '（判为合格）'}")
            failed += 1
    # A6（口径与机制对账）：本轮复查抓到 b17/p503/R22 三枚驱动**把来树字节 copy2 进克隆**，
    # 身份行却写"来树未入库字节不进本轮读数"。A4 只认"有没有这句"，认不出这句是假的。
    ov_src = ("def prepare(clone):\n    tgt = clone / rel\n    shutil.copy2(src, tgt)\n"
              "tee_to(LOGDIR / '00-run.log')\n"
              "identity(ROOT, extra='｜本轮读私有克隆的 HEAD（来树未入库字节不进本轮读数）')\n")
    oc = [
        ("T26 覆盖来树字节却声明『不进本轮读数』⇒ A6 开火（假身份行，A4 量不到）",
         check(code_lines(ov_src), {"X"}, "X"), ["A6a 装架把来树文件覆盖进克隆"]),
        ("T27 正控制：同样有 copy2 装架，但身份行走 `overlay=` 现测份数 ⇒ A6 不开火",
         check(code_lines(ov_src.replace(
             "identity(ROOT, extra='｜本轮读私有克隆的 HEAD（来树未入库字节不进本轮读数）')",
             "identity(ROOT, overlay=files)")), {"X"}, "X"), []),
        ("T28 正控制：纯克隆 HEAD（无 copy2 装架）＋该声明 ⇒ A6 不开火（声明与机制相符）",
         check(code_lines(ov_src.replace("    shutil.copy2(src, tgt)\n", "    pass\n")),
               {"X"}, "X"), []),
        ("T29 该声明只在注释里 ⇒ 抹散文后不许凭它判红（防把改对了的驱动判坏）",
         check(code_lines(ov_src.replace("identity(ROOT, extra='｜本轮读私有克隆的 HEAD（来树未入库字节不进本轮读数）')",
                                         "identity(ROOT, overlay=files)  # 旧口径说不进本轮读数")),
               {"X"}, "X"), []),
    ]
    # A6b：A6a 的反面那一半——覆盖来树字节、身份行里却**一个可复算的份数都没有**。
    # 这一格不是假想：本轮修 A6a 时现测，`r30`／`p701`／`p703` 三支的手写身份行之所以过关，
    # 全靠它们各有一处现测份数；把那一处删掉，产物就只剩"读克隆 HEAD"这句无法核对的话。
    ovq_src = ("def prepare(clone):\n    tgt = clone / rel\n    shutil.copy2(src, tgt)\n"
               "tee_to(LOGDIR / '00-run.log')\nidentity(ROOT)\n")
    oc += [
        ("T30 覆盖来树字节、身份行无任何现测份数 ⇒ A6b 开火（读数无法复算）",
         check(code_lines(ovq_src), {"X"}, "X"), ["A6b"]),
        ("T31 正控制：同一装架，身份行走 `identity(..., overlay=...)` ⇒ A6b 不开火",
         check(code_lines(ovq_src.replace("identity(ROOT)", "identity(ROOT, overlay=mods)")),
               {"X"}, "X"), []),
        ("T32 正控制：手写行含『已在 HEAD n/N』的可复算量 ⇒ A6b 不开火",
         check(code_lines(ovq_src.replace("identity(ROOT)",
                                          "print(f'基线字节：本卡文件已在 HEAD {in_head}/{len(NEW)}')")),
               {"X"}, "X"), []),
        ("T33 正控制：打印『覆盖 {len(mods)} 个脏文件』⇒ A6b 不开火",
         check(code_lines(ovq_src.replace("identity(ROOT)",
                                          "print(f'基线字节：覆盖 {len(mods)} 个脏文件进克隆')")),
               {"X"}, "X"), []),
        ("T34 正控制：就地注码的 `copy2`（备份进 `bak/`，无克隆内落点）⇒ A6b 不开火",
         check(code_lines(ovq_src.replace("    tgt = clone / rel\n", "    tgt = bak / rel\n")),
               {"X"}, "X"), []),
    ]
    for name, got, want in oc:
        ran += 1
        joined = " / ".join(got)
        if all(w in joined for w in want) and (want or not got):
            print(f"  ✓ {name}")
        else:
            print(f"  ✗ {name}：期望 {want}，实际 {joined or '（判为合格）'}")
            failed += 1
    # 族归属（`families_of`）与债务台账（`debt_accounting`）：本轮合并带进来的两类失效。
    prose_src = ('"""上一轮的承诺见 docs/superpowers/specs/ledger/logs/b-phase/lane-5.md。\n'
                 '第二行"""\nLOGDIR = REPO / "docs/superpowers/specs/ledger/logs/R22-lanes"\n'
                 "tee_to(LOGDIR / '00-run.log')\n")
    dc = [
        ("T38 族名只从代码行取：docstring 里引用的**别人族**不算落点",
         families_of(code_lines(prose_src)), {"R22-lanes"}),
        ("T39 落点只写在 docstring／注释里 ⇒ 无代码内落点（不进 A1／A2，但必须进 `OFFTREE` 台账）",
         families_of(code_lines('"""一次性刀具：上一轮读数见 '
                                'docs/superpowers/specs/ledger/logs/b-phase/x.md。\n第二行"""\n'
                                "# docs/superpowers/specs/ledger/logs/b-phase/y.md\n"
                                "print(done)\n")), set()),
        ("T40 一枚驱动写两族 ⇒ 两族都进判据对象",
         families_of(code_lines(prose_src + 'OUT2 = "docs/superpowers/specs/ledger/logs/P803"\n')),
         {"R22-lanes", "P803"}),
    ]
    for name, got, want in dc:
        ran += 1
        if got == want:
            print(f"  ✓ {name}")
        else:
            print(f"  ✗ {name}：期望 {want}，实际 {got}")
            failed += 1

    multi_src = prose_src + 'OUT2 = "docs/superpowers/specs/ledger/logs/P803"\nidentity(ROOT)\n'
    mc = [
        ("T41 多族驱动里没开例外的第二族 ⇒ A3 逐族开火（不许拿第一族的例外放行）",
         check(code_lines(multi_src), {"R22-lanes"}, {"R22-lanes", "P803"}), ["A3 族 `P803`"]),
        ("T41b 正控制：两族都开了成对例外 ⇒ A3 不开火",
         check(code_lines(multi_src), {"R22-lanes", "P803"}, {"R22-lanes", "P803"}), []),
    ]
    for name, got, want in mc:
        ran += 1
        joined = " / ".join(got)
        if all(w in joined for w in want) and (want or not got):
            print(f"  ✓ {name}")
        else:
            print(f"  ✗ {name}：期望 {want}，实际 {joined or '（判为合格）'}")
            failed += 1

    # 债务台账：登记面上界／指错对象／过期条目三格，加一格"正控制＝如实登记且未补齐"。
    fbd = {"mut_x.py": {"Fam"}}
    ok_debt = {"mut_x.py": ("Fam", "无身份发射点；该族产物按短语命中 0 份")}
    db = [
        ("T42 正控制：条目如实（族对得上、理由写实、产物实测仍缺）⇒ 台账不开火",
         debt_accounting(ok_debt, fbd, {"Fam": ("缺身份行", "")}, 1), []),
        ("T43 登记面超上界 ⇒ D0 开火（只许减不许增）",
         debt_accounting(ok_debt, fbd, {"Fam": ("缺身份行", "")}, 0), ["D0 债务登记"]),
        ("T44 条目对应的驱动已不在名单 ⇒ D1 开火（登记过期）",
         debt_accounting(ok_debt, {}, {"Fam": ("缺身份行", "")}, 5), ["D1 条目"]),
        ("T45 理由为空 ⇒ D2 开火",
         debt_accounting({"mut_x.py": ("Fam", "  ")}, fbd, {}, 5), ["D2 条目"]),
        ("T46 登记的族与驱动代码内落点不符 ⇒ D3 开火（指错对象）",
         debt_accounting({"mut_x.py": ("OtherFam", "理由写实")}, fbd, {}, 5), ["D3 条目"]),
        ("T47 该族最近一轮产物实测到身份行 ⇒ D4 开火（补齐了就必须删条目）",
         debt_accounting(ok_debt, fbd, {"Fam": ("有身份行", "读数见产物")}, 5), ["D4 条目"]),
    ]
    for name, got, want in db:
        ran += 1
        joined = " / ".join(got)
        if all(w in joined for w in want) and (want or not got):
            print(f"  ✓ {name}")
        else:
            print(f"  ✗ {name}：期望 {want}，实际 {joined or '（判为合格）'}")
            failed += 1

    # 树外落点台账：拦的是"没进门也没登记"这一形——`admits()` 判 False 只是"两轴无对象"，
    # 从前它等于安静放行，末行报的"20 枚驱动"读起来像全量。五格四红一正控。
    cand = [Path("scripts/mut_in.py"), Path("scripts/mut_out.py")]
    ok_off = {"mut_out.py": "判读只印 stdout、代码内无树内落点"}
    ot = [
        ("T48 正控制：候选要么进门、要么如实登记 ⇒ 台账不开火",
         offtree_accounting(cand, {"mut_in.py"}, ok_off, 1), []),
        ("T49 盘上一枚既没进门也没登记 ⇒ O1 开火（静默排除就是本门的洞）",
         offtree_accounting(cand, {"mut_in.py"}, {}, 5), ["O1 `mut_out.py`"]),
        ("T50 登记面超上界 ⇒ O2 开火（只许减不许增）",
         offtree_accounting(cand, {"mut_in.py"}, ok_off, 0), ["O2 树外落点登记"]),
        ("T51 条目对应的驱动已不在盘上 ⇒ O3 开火（登记过期）",
         offtree_accounting([Path("scripts/mut_in.py")], {"mut_in.py"}, ok_off, 5), ["O3 条目"]),
        ("T52 条目里那枚已补代码内落点、自动进门 ⇒ O4 开火（补齐了就必须删条目）",
         offtree_accounting(cand, {"mut_in.py", "mut_out.py"}, ok_off, 5), ["O4 条目"]),
        ("T53 理由为空 ⇒ O5 开火（豁免不许留空条目）",
         offtree_accounting(cand, {"mut_in.py"}, {"mut_out.py": "  "}, 5), ["O5 条目"]),
    ]
    for name, got, want in ot:
        ran += 1
        joined = " / ".join(got)
        if all(w in joined for w in want) and (want or not got):
            print(f"  ✓ {name}")
        else:
            print(f"  ✗ {name}：期望 {want}，实际 {joined or '（判为合格）'}")
            failed += 1
    # 名字轴 A7：一格红（本轮合并实测到的那一形）＋三格"不许误伤"的正控。
    nm = [
        ("T58 调用只存在于对方树里的函数（`tip(clone)`，def 缺席）⇒ A7 开火",
         undefined_calls("def prepare(dst):\n"
                         '    print(f"基线字节：`{tip(dst)}`")\n'
                         "    return dst\n"),
         ["A7 第 2 行调用本文件里未定义的名字 `tip`"]),
        ("T59 正控制：本文件里定义的函数／内建／属性调用都不算悬空 ⇒ 不开火",
         undefined_calls("import re\n\n"
                         "def helper(v):\n    return v\n\n"
                         "def main(p):\n"
                         "    print(helper('x'), len('y'), re.compile('a'), p.read_text())\n"),
         []),
        ("T60 正控制：形参／局部 def／except as／global 登记的名字不误伤（名表取全树并集）",
         undefined_calls("import json\n\n"
                         "def run(fn, arg):\n"
                         "    def wrap(v):\n        return fn(v)\n"
                         "    try:\n        return wrap(json.loads(arg))\n"
                         "    except ValueError as exc:\n        return str(exc)\n"),
         []),
        ("T61 解析失败 ⇒ A7 点名『解析失败』而不是静默合格（向红偏置）",
         undefined_calls("def f(:\n"), ["A7 解析失败"]),
    ]
    for name, got, want in nm:
        ran += 1
        joined = " / ".join(got)
        if all(w in joined for w in want) and (want or not got):
            print(f"  ✓ {name}")
        else:
            print(f"  ✗ {name}：期望 {want}，实际 {joined or '（判为合格）'}")
            failed += 1
    print(f"===== 预检自测：现数 {ran} 格（跑过一格记一格，不写分组数），失败 {failed} 格 =====")
    return 1 if failed else 0


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--selftest", action="store_true", help="只跑内存反向格（不读仓库树）")
    ap.add_argument("--quiet-ok", action="store_true", help="全合格时只印末行")
    ap.add_argument("--no-artifact-axis", action="store_true",
                    help="只跑轴一（源码形状）：改驱动中途用，闭合判据仍靠默认两轴")
    args = ap.parse_args()
    if args.selftest:
        return selftest()

    pairs = gitignore_pairs()
    if not pairs:
        print("RED-UNNAMED：`.gitignore` 里一枚族的例外都没解析到 ⇒ 这道门没有判据对象")
        return 1
    ds = drivers()
    if not ds:
        print(f"RED-UNNAMED：scripts/ 下没有声明 `{LOGROOT}<族>` 落点的常驻驱动 ⇒ 名单枚举本身失守")
        return 1
    cands = candidates()

    bad = 0
    seen_families = []
    fam_by_driver = {}
    for p in ds:
        fams = families_of(code_lines(p.read_text(encoding="utf-8")) or [])
        fam_by_driver[p.name] = fams
        seen_families.extend(sorted(fams))
    deferred_families = {f for f, _ in DEFERRED.values()}
    # 轴二先算：它是"产物里到底有没有"的地面事实，轴一的先后疑点由它作证据豁免（见 check 的 order_cleared）。
    # 债务登记的族**照样实测**，但实测值只用来判"条目是否过期"（`debt_accounting` 的 D4），
    # 不进 A4 的合格／不合格账（红的债记在别人名下，不该由本门的红代替它说话）。
    verdicts = {f: artifact_verdict(f) for f in sorted(set(seen_families))}
    if args.no_artifact_axis:
        verdicts = {}

    for p in ds:
        text = p.read_text(encoding="utf-8")
        fams = fam_by_driver[p.name]
        label = ", ".join(sorted(fams)) or "未声明"
        code = code_lines(text)
        if code is None:
            bad += 1
            print(f"  ✗ {p.name}（族 {label}）\n        A0 解析失败（ast/tokenize）"
                  "⇒ 判据读不到代码行，向红偏置")
            continue
        if p.name in DEFERRED:
            fam, reason = DEFERRED[p.name]
            if not args.quiet_ok:
                print(f"  ⚠ {p.name}（族 {fam}）｜他人泳道债务登记：{reason}")
            continue
        cleared = any(verdicts.get(f, ("", ""))[0] == "有身份行" for f in fams)
        why = check(code, pairs, fams, order_cleared=cleared)
        if why:
            bad += 1
            print(f"  ✗ {p.name}（族 {label}）")
            for w in why:
                print(f"        {w}")
        else:
            emit_lines = [i for i, ln in code if EMIT.search(ln)]
            tee_lines = [i for i, ln in code if "tee_to(" in ln]
            waived = (cleared and tee_lines and max(emit_lines) < min(tee_lines))
            if not args.quiet_ok:
                print(f"  ✓ {p.name}（族 {label}）"
                      + ("｜顺序疑点已由最近一轮产物实测豁免（发射在函数体内、调用点在 tee 之后）"
                         if waived else ""))

    # 名字轴 A7：对象集是**候选全量**（38 枚），不是只进门的 20 枚——悬空引用与"落点在树外"
    # 是两件不相干的事，树外落点那批同样会在真跑时 NameError。
    name_hits = 0
    for p in cands:
        for w in undefined_calls(p.read_text(encoding="utf-8")):
            bad += 1
            name_hits += 1
            print(f"  ✗ 名字轴 {p.name}：{w}")
    if not name_hits and not args.quiet_ok:
        print(f"  ✓ 名字轴 A7：{len(cands)} 枚候选逐枚 AST 名表对账，无悬空调用")

    for w in debt_accounting(DEFERRED, fam_by_driver, verdicts, DEFERRED_MAX):
        bad += 1
        print(f"  ✗ 债务台账 {w}")

    # 树外落点台账：盘上的常驻件必须**要么进门、要么在 `OFFTREE` 里写明为什么没进门**，
    # 两个都不在就是静默排除（本门上一版的形状：末行只印进门的那批，剩下的没人知道有多少）。
    offtree_bad = offtree_accounting(cands, {p.name for p in ds}, OFFTREE, OFFTREE_MAX)
    for w in offtree_bad:
        bad += 1
        print(f"  ✗ 树外落点台账 {w}")
    if not offtree_bad and not args.quiet_ok:
        print(f"  ✓ 树外落点台账：{len(OFFTREE)} 枚（上界 {OFFTREE_MAX}）"
              f"｜候选 {len(cands)} 枚＝进门 {len(ds)} 枚＋登记 {len(OFFTREE)} 枚")

    # 轴二：按族实测最近一轮产物（一枚族只判一次，多枚驱动共用同族时不重复计红）。
    # 债务登记的族由台账那一格说话（D4 判过期），这里只印实测读数、不计入合格／不合格。
    on_disk = []
    if not args.no_artifact_axis:
        for family in sorted(set(seen_families)):
            verdict, note = verdicts[family]
            if family in deferred_families:
                if not args.quiet_ok:
                    print(f"  ⚠ 产物 A4〔{family}〕（债务登记）{note}")
                continue
            if verdict == "有身份行":
                if not args.quiet_ok:
                    print(f"  ✓ 产物 A4〔{family}〕{note}")
            else:
                bad += 1
                print(f"  ✗ 产物 A4〔{family}〕{note}")
        # A5：族归属对账——门的判据对象必须等于磁盘上的族集合，
        # 差额只由 `NOT_A_BATTERY` 里带理由的登记补足（A4 的族是从驱动正文反推的，
        # 树里长出一族没有驱动的目录时，整条产物轴从来不看它）。
        on_disk = tree_families()
        for w in accounting(set(seen_families), NOT_A_BATTERY, on_disk):
            bad += 1
            print(f"  ✗ {w}")
        if not args.quiet_ok:
            extra = sorted(set(on_disk) & set(NOT_A_BATTERY) - set(seen_families))
            print(f"  ✓ 归属 A5：磁盘 {len(on_disk)} 族＝驱动 {len(set(seen_families))} 族"
                  + (f"＋非电池登记 {len(extra)} 族（{', '.join(extra)}）" if extra else "，无未登记族"))

    print(f"===== 字节身份行门：{len(ds)} 枚驱动（成员现取）／"
          f"树外落点登记 {len(OFFTREE)} 枚（候选现数 {len(cands)} 枚）／"
          f"名字轴现数 {len(cands)} 枚／"
          f"{len(set(seen_families))} 族产物实测"
          + (f"／磁盘 {len(on_disk)} 族归属对账" if on_disk else "（产物轴未开")
          + f"，{bad} 项不合格 =====")
    return 1 if bad else 0


if __name__ == "__main__":
    sys.exit(main())
