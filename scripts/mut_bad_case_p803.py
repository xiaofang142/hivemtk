#!/usr/bin/env python3
"""T-P8-03（G-2 Bad Case 闭环）牙齿电池：逐刀拆掉本卡的每句承诺，必须有人红。

## 为什么本卡需要电池

T-P8-03 交付的是五层代码（model 值域与迁移表 / repository 真表读写 / service 状态机与
门槛 / controller 形状与状态码 / app 装配 + orchestrator 留痕接缝）加 64 条顶层用例
（六个 *_test.go 里 `^func Test` 的现测和；两个数都是**本轮交付时**的快照，重数一次：
`grep -c '^func Test' user-server/internal/*/*bad_case*_test.go` 与 `python3 -c` 取 `len(cells())`）。
用例绿只证明"写下的断言成立"，不证明"断言看得见这一层的失效"——本卡最坏的一类失效恰好是
**删掉它没有任何用例会红**（下表 45 格，一处承诺一格，另有八处"注了也没人红"的登记见末尾）：

- 把 `BadCaseLabelIntentMisjudge` 接到 knowledge 层：七类目仍各有层、四层仍可达，
  上面两段循环照旧绿，而"该找算法还是该找知识库"这份交付物本身写歪了；
- 把 `badCaseMaxPageSize` 的夹住去掉：夹具只有 6 行，"不超过上限"永远成立；
- 把 `note == ""` 的必填挡掉：打标仍然成功，只是评测集里多了一批"某人说它不对"的样本。

前三条已经在用例侧补了腿（`wantLayer` 逐类目点名、`TestBadCaseRepo_ClampsAreExact`
用 2005 行把上限越过、`TestBadCaseService_Label` 断缺依据必须报错），这里再逐刀证明
**那些腿真有牙**。

## 判据形状

- **test 族**（绝大多数）：注码后跑该层那一个过滤器，要求
  ① `settled == 控制组`（一条用例 panic 会带走整个二进制，那时 settled 会掉，必须判 BROKEN 而不是"杀掉了"）；
  ② 红名集合**恰好等于**推演的那几条（多红=连带面没写清，少红=判据没开火）；
  ③ 红因里要点名本格那条断言的话术（`expect_reason`）——只判"红了"可能红在别处。
     红因写成 `a|b|c` 若干个 token，判据是**同一行输出里同时含全部 token**（整段 in out
     会把两条不相干的断言行拼成一条绿）；前缀分三档：裸 token 在本包用例文件里核、
     `@` 在本格注码的产码文件里核（错误文本由产码抛、用例用 %v 印出）、
     `~` 是运行期才拼得出的串（格式化后的值、子测试名），静态核不了 ⇒ `--check` 把它们
     逐条点名披露，凭据只能来自真跑日志。
- **panic 族**（O4 撤 recover）：这一刀的"红"是把测试进程打崩（recover 撤掉后 panic 逃逸到
  runtime），拿不到 `--- FAIL` ⇒ 判据换成"进程因本刀注入的那句 panic 而死"：
  `panic:` 出现 **且** 输出里有 `留痕里炸了`（那是用例自己 panic 的串，不是别处的崩溃）。
  这条刀**不**要求 settled==控制组，那是它必然做不到的。

## 不配格的八处（写明，别把"没数到"印成"没问题"）

1. `clause.Locking{Options: "SKIP LOCKED"}`：摘掉它并发两批仍互不相交（第二批会**阻塞**到
   第一批提交，然后按 `status='labeled'` 重读而看不到已被取走的行）⇒ 它是"不阻塞"而不是
   "不重复"的担当，`ClaimForExportIsDisjointUnderConcurrency` 断的是后者，给它配格只会配出一条
   没有含义的红。失效形状是导出排队，本卡没有那个面。
2. `ApplyAction` 里 `RowsAffected != 1` 的整笔回滚、`ClaimForExport` 里同形状的两处：
   都在"锁到手之后"，正常路径永远走不到（走到就是 PG 行为变了），注码只能在**用例侧**造前置，
   而那等于测我自己写的桩。登记为纵深防御，不配格。
3. `ClaimForExport` 的 `eval_set_id = ''` 那半个条件：单独摘掉不可观测——已导出的行同时被
   改成了 `status='exported'`，`status='labeled'` 那半个条件已经把它们挡在外面（两列是同一件事
   的两个写法）。摘 `status='labeled'` 那半边是可观测的，已配成 R8。
4. `markBadCase` 的 `resp == nil || in == nil` 守卫：唯一的拆法是让它往下走，而下一步就是
   `resp.Reply` 的空指针 panic，panic 发生在**起协程之前**（接缝自己的 recover 还兜不到）⇒
   测试进程直接死，判据无法与 O4 区分。`Gates` 的两条 nil 用例已经从行为侧断言"不留痕"，
   这一刀的"有牙"要靠那条用例而不是这里。
5. `ExportEvalSet` 的控制器侧 `body.Limit < 0` 早退：服务层与仓储层各还有一道（S4 已配格），
   摘掉控制器这一道仍然回 400 ⇒ 不可观测。属"同一判据三处写"的冗余，登记不配格。
6. `setupBadCaseRoutes` 里 `svc == nil` 的那条 Infof：纯日志，无行为。
7. 控制器 `parseIntParam` 的 `n < 1` 那道早退（原 C5 格）：**跑过才看得出来不是判据没牙**。
   摘掉它之后 `page=0` 仍回 400、仍不带 list——仓储自己还有一道（`ListFilters` 的
   "页码非正/页长非正"两档），而控制器把那道错误同样映射成 400。两层的对外形状一模一样，
   用例能断言的（状态码＋不带数据）都不断言不出来，只剩错误文案不同（"page 必须是从 1 起的整数"
   vs 仓储的入参错误），为一句文案配格等于把契约钉死在措辞上。⇒ 与第 5 处同一类"同一判据两处写"，
   登记不配格。第 1 轮实测：`SURVIVED=洞`，红名集合为空（`docs/superpowers/specs/ledger/logs/P803/20260925-122510/C5.log`）。
8. `saveAISuggestion` 不再自己跑一次聚合（本卡把 confidence 改成入参，消掉"一轮落两条
   `confidence_signals`"这个存量缺陷）：**这句承诺注不出格**。要观测"聚合跑了几次"只有一条路
   ——数 `confidence_signals` 的行数，而 `repository.NewConfidenceSignalRepository()` 没有
   `WithDB` 变体（`internal/repository/confidence_repositories.go:17`），它在 `Create` 时取
   **进程级全局句柄** ⇒ 配这样一条用例要么把全局句柄指到本用例的影子库（正是本二进制反复出事
   的"全局 DB 句柄泄漏"面，本轮 router 包那条跨用例泄漏就是同一族），要么为可注入而改生产代码。
   现状是**靠结构挡住**：`saveAISuggestion(…, confidence float64)` 手里没有算置信度的能力，
   要退回两次聚合必须显式重新调 `extractConfidence`，那一行在 diff 里看得见。
   登记为"无机检、靠形状"，行数实证转给 T-P8-04（那张要按信号数算指标，届时必须先有行数断言）。

## 口径（照本仓既有电池的规矩）

- 只在 `git clone --shared` 出来的私有克隆里注码，脏文件按 `git status --porcelain -uall` 覆盖进去；
  `--clone` 走 `mut_dispose.workdir/dispose/dispose_at_exit` 三道闸（不许指着自己的工作树，
  也不许在装架之后的中止路上把私有克隆留在盘上）；
- 控制组**现测**：红名与 settled 都不写死（共享树下别人加用例会让写死的数字漂）；
- 一格多处编辑在内存里叠完一次写盘；每刀还原后逐文件比 md5，不等即停机；
- BUILD-BROKEN / settled 掉 / 红而不点名 ⇒ 一律 BROKEN，不计入杀掉；
- 环境前提不满足（盘、测试库）退 ENV-BROKEN 并**不**印"全杀"；
  跑用例前必过 pg_isready（本地 8232 口令会与 .env 漂移 ⇒ 漂了就是整批假红）；
- 逐格原始输出落 `docs/.../ledger/logs/P803/<tag>/`，tag 默认取本地时间戳，复跑不覆盖上一轮；
- `--selftest` 先证"驱动会说不了"：三格合成刀（无效果变异必须报 SURVIVED、期望写歪必须报
  BROKEN、编译坏必须报 BUILD-BROKEN）。**改完驱动先跑这一档再放整族**。

用法：
    python3 scripts/mut_bad_case_p803.py --selftest          # 驱动自己的反向证明（三格，快）
    python3 scripts/mut_bad_case_p803.py --check             # 静态预检：锚点命中数/红因字面量/红名是否存在
    python3 scripts/mut_bad_case_p803.py --only S4,R6        # 只跑指定格（终态会写明本趟是子集）
    python3 scripts/mut_bad_case_p803.py                     # 全族
"""
from __future__ import annotations

import argparse
import hashlib
import os
import re
import shutil
import subprocess
import time
from pathlib import Path

from mut_dispose import dispose, dispose_at_exit, leave_for_evidence, workdir
from redact import scrub

ROOT = Path(__file__).resolve().parent.parent
US = "user-server"
SRV = f"{US}/internal"
LANE_PATHS = [SRV]
DEFAULT_LOGS = "docs/superpowers/specs/ledger/logs/P803"
ANSI = re.compile(r"\x1b\[[0-9;]*m")

MODEL = f"{SRV}/model/bad_case.go"
REPO = f"{SRV}/repository/bad_case.go"
SVC = f"{SRV}/service/bad_case.go"
CTRL = f"{SRV}/controller/bad_case.go"
ORCH = f"{SRV}/service/smart_cs_orchestrator.go"
WIRE = f"{SRV}/app/bad_case_wiring.go"

PKG_MODEL, PKG_REPO, PKG_SVC, PKG_ROUTER, PKG_APP = (
    "./internal/model/", "./internal/repository/", "./internal/service/",
    "./internal/router/", "./internal/app/")
F_MODEL = "^TestBadCase"
F_REPO = "^TestBadCaseRepo"
F_SVC = "^TestBadCase"
F_ROUTER = "^TestBadCaseRoutes"
F_APP = "^TestInitBadCaseRuntime|^TestBadCaseMarkerFor"

M = "TestBadCase_"
MR = "TestBadCaseRepo_"
MS = "TestBadCase"


# code, 说明, 注码文件, [(旧, 新)], 包, 过滤器, 期望红名, 期望红因, 判据族
def cells() -> list[tuple]:
    return [
        # ---------------- model 层：值域、迁移表、类目→层、幂等键 ----------------
        ("M1", "exported 不再是终态（判完的样本能回炉）", MODEL,
         [("BadCaseStatusExported:  nil,", "BadCaseStatusExported:  {BadCaseStatusLabeled},")],
         PKG_MODEL, F_MODEL, {f"{M}Transitions"}, "~exported, labeled|CanBadCaseTransition(", "test"),
        ("M2", "dismissed 可重开（同一现场判两次）", MODEL,
         [("BadCaseStatusDismissed: nil,", "BadCaseStatusDismissed:  {BadCaseStatusPending},")],
         PKG_MODEL, F_MODEL, {f"{M}Transitions"}, "~dismissed, pending|CanBadCaseTransition(", "test"),
        ("M3", "类目接错责任层（intent 接到 knowledge）", MODEL,
         [("BadCaseLabelIntentMisjudge:  BadCaseFixLayerIntent,", "BadCaseLabelIntentMisjudge:  BadCaseFixLayerKnowledge,")],
         PKG_MODEL, F_MODEL, {f"{M}EveryLabelHasAFixLayerAndEveryLayerIsReachable"}, "的责任层是", "test"),
        ("M4", "类目没有责任层（少一条映射）", MODEL,
         [("\tBadCaseLabelKBStale:         BadCaseFixLayerKnowledge,\n", "")],
         PKG_MODEL, F_MODEL, {f"{M}EveryLabelHasAFixLayerAndEveryLayerIsReachable"}, "没有责任层", "test"),
        ("M5", "自动来源缺 message_id 的键守卫反向", MODEL,
         [("if source != BadCaseSourceManual && strings.TrimSpace(messageID) == \"\" {",
           "if source == BadCaseSourceManual && strings.TrimSpace(messageID) == \"\" {")],
         PKG_MODEL, F_MODEL, {f"{MS}DedupKey"}, "必须判不可用", "test"),
        ("M6", "幂等键里不含来源（两判据抢同一条键）", MODEL,
         [("return source + \"|\" + sessionID + \"|\" + messageID, true",
           "return sessionID + \"|\" + messageID, true")],
         PKG_MODEL, F_MODEL, {f"{MS}DedupKey"}, "共用了同一条幂等键", "test"),
        ("M7", "值域外的状态被判成已知", MODEL,
         [("func IsKnownBadCaseStatus(v string) bool { return badCaseIn(BadCaseStatuses, v) }",
           "func IsKnownBadCaseStatus(v string) bool { return v != \"\" }")],
         PKG_MODEL, F_MODEL, {f"{M}StatusesAreClosed"}, "被判成已知", "test"),

        # ---------------- repository 层：真表才证得了的事 ----------------
        ("R1", "冲突识别不认约束名（主键冲突读成已存在）", REPO,
         [('return strings.Contains(msg, "23505") && strings.Contains(msg, "bad_cases_dedup_key")',
           'return strings.Contains(msg, "23505")')],
         PKG_REPO, F_REPO, {f"{MR}ConflictIdentityIsExact"}, "主键冲突被读成", "test"),
        ("R2", "写回白名单少一列（判定依据落不了地）", REPO,
         [('"status", "label", "label_note", "labeler_id", "labeled_at",',
           '"status", "label", "labeler_id", "labeled_at",')],
         PKG_REPO, F_REPO, {f"{MR}ApplyActionWriteWhitelist", f"{MR}ApplyActionExpectsStatus"},
         "判定结论没落库", "test"),
        ("R3", "写回白名单多了现场列（判完还能改现场）", REPO,
         [('"fix_layer", "eval_set_id", "exported_at", "cancel_reason", "updated_at",',
           '"fix_layer", "eval_set_id", "exported_at", "cancel_reason", "updated_at", "query_text",')],
         PKG_REPO, F_REPO, {f"{MR}ApplyActionWriteWhitelist"}, "现场抄本被事后改写", "test"),
        ("R4", "值域外的状态过滤器不再报错（空列表冒充业务结论）", REPO,
         [('return nil, fmt.Errorf("%w: 未知状态 %q", ErrBadCaseInputInvalid, s)', "_ = s")],
         PKG_REPO, F_REPO, {f"{MR}ListFilters"}, "未知状态|应判入参非法", "test"),
        ("R5", "默认视图从只列 pending 放开", REPO,
         [('db = db.Where("status = ?", model.BadCaseStatusPending)',
           'db = db.Where("status <> ?", model.BadCaseStatusDismissed)')],
         PKG_REPO, F_REPO, {f"{MR}ListFilters"}, "默认视图应 3 条 pending", "test"),
        ("R6", "单页上限的夹住失效", REPO,
         [("if size > badCaseMaxPageSize {", "if size > 1 << 30 {")],
         PKG_REPO, F_REPO, {f"{MR}ClampsAreExact"}, "单页取回", "test"),
        ("R7", "单次导出上限的夹住失效", REPO,
         [("if limit > badCaseMaxExportRows {", "if limit > 1 << 30 {")],
         PKG_REPO, F_REPO, {f"{MR}ClampsAreExact"}, "一次导出取走", "test"),
        ("R8", "导出条件丢了「必须已打标」（pending 被灌进评测集）", REPO,
         [('Where("status = ? AND eval_set_id = ?", model.BadCaseStatusLabeled, "").',
           'Where("eval_set_id = ?", "").')],
         PKG_REPO, F_REPO, {f"{MR}ClaimForExport"}, "第二次导出失败|@导出取走", "test"),
        ("R9", "空批不报 AlreadyClaimed（静默空集）", REPO,
         [("return ErrBadCaseAlreadyClaimed", "return nil")],
         PKG_REPO, F_REPO, {f"{MR}ClaimForExport"}, "AlreadyClaimed", "test"),
        ("R10", "聚合读数少键（前端的 0 变 undefined）", REPO,
         [("for _, s := range model.BadCaseStatuses {\n\t\tout[s] = 0\n\t}\n", "")],
         PKG_REPO, F_REPO, {f"{MR}CountByStatusKeysAreAlwaysPresent"}, "空表时键数应为", "test"),
        ("R11", "值域外的脏 label 被悄悄丢行", REPO,
         [("unknown += rw.N", "_ = rw.N")],
         PKG_REPO, F_REPO, {f"{MR}CountByFixLayer"}, "脏 label 没进 unknown", "test"),

        # ---------------- service 层：门槛、状态机、导出 ----------------
        ("S1", "零命中判据失效（无本之木不再被记）", SVC,
         [("if in.RetrievedCount <= 0 {", "if in.RetrievedCount < 0 {")],
         PKG_SVC, F_SVC, {f"{MS}ShouldMark", f"{MS}MarkBadCase_Gates",
                          f"{MS}MarkBadCase_SourceComesFromServiceGate", f"{MS}Service_MarkAuto",
                          # 三类轮次那条用例的四条对照组也走零命中这条路（0 chunks + 置信度在阈值之上），
                          # 所以这道刀必然连带它红 —— 按"一判据多腿消费"widening，不缩期望名单。
                          f"{MS}MarkBadCase_TurnsWithNoAnswerAreNotMarked"},
         "source=|zero_hit", "test"),
        ("S2", "门槛边界改成 <=（等于阈值也算低质）", SVC,
         [("if in.Confidence < threshold {", "if in.Confidence <= threshold {")],
         PKG_SVC, F_SVC, {f"{MS}ShouldMark", f"{MS}MarkBadCase_Gates",
                          f"{MS}MarkBadCase_UsesEffectiveConfidence"}, "~ok=true want=false", "test"),
        ("S3", "阈值缺位时的兜底撤掉（整条自动标记静默停摆）", SVC,
         [("if threshold <= 0 {\n\t\tthreshold = badCaseFallbackThreshold()\n\t}\n\tif in.RetrievedCount <= 0 {",
           "if in.RetrievedCount <= 0 {")],
         PKG_SVC, F_SVC, {f"{MS}ShouldMark", f"{MS}ShouldMark_ZeroThresholdNeverDisablesMarking",
                          f"{MS}Marker_NilWhenUnavailableAndSwallowsMarkError",
                          f"{MS}Service_MarkAuto", f"{MS}Service_MarkRequiresMessageID"}, "兜底没生效", "test"),
        ("S4", "负 limit 又当成「没给」（静默导出默认档那一批）", SVC,
         [("if limit == 0 {", "if limit <= 0 {")],
         PKG_SVC, F_SVC, {f"{MS}Service_ExportEvalSet"}, "负 limit", "test"),
        ("S5", "打标可以不给依据", SVC,
         [('if note == "" {', 'if note == "\\x00never" {')],
         PKG_SVC, F_SVC, {f"{MS}Service_Label"}, "空依据|应判入参非法", "test"),
        ("S6", "责任层不再由类目派生（写死成 knowledge）", SVC,
         [("fixLayer := model.FixLayerOfLabel(label)", "fixLayer := model.BadCaseFixLayerKnowledge")],
         PKG_SVC, F_SVC, {f"{MS}Service_Label"}, "责任层没派生", "test"),
        ("S7", "撤销可以不给理由", SVC,
         [('if strings.TrimSpace(reason) == "" {', 'if strings.TrimSpace(reason) == "never" {')],
         PKG_SVC, F_SVC, {f"{MS}Service_Dismiss"}, "空理由|应判入参非法", "test"),
        ("S8", "CAS 落败被当成成功（重复打标静默覆盖）", SVC,
         [("if !applied {", "if !applied && false {")],
         PKG_SVC, F_SVC, {f"{MS}Service_Label"}, "重复打标应判 Transition", "test"),
        ("S9", "底座未装配也造出标记器（关闸失效）", SVC,
         [("if svc == nil || !svc.Available() {", "if svc == nil {")],
         PKG_SVC, F_SVC, {f"{MS}Marker_NilWhenUnavailableAndSwallowsMarkError",
                          f"{MS}Service_UnavailableRejectsEveryEntry"}, "底座不可用时应回 nil", "test"),
        ("S10", "已判率分母口径写歪（闭环转没转看不出来）", SVC,
         [("if status != model.BadCaseStatusPending {", "if status == model.BadCaseStatusExported {")],
         PKG_SVC, F_SVC, {f"{MS}Service_ListAndStats"}, "已判率应为", "test"),
        ("S11", "自动标记缺 message_id 变成静默跳过", SVC,
         [('return false, fmt.Errorf("%w: 自动标记必须有 message_id（session=%s source=%s），否则同键互吞",\n\t\t\tErrBadCaseInputInvalid, in.SessionID, source)',
           "return false, nil")],
         PKG_SVC, F_SVC, {f"{MS}Service_MarkRequiresMessageID"}, "应判入参非法", "test"),

        # ---------------- 编排器接缝：预门槛 / 新 ctx / 超时同源 / recover ----------------
        ("O1", "先判门槛再起协程这道预筛撤掉（每轮白起一条协程）", ORCH,
         [("if _, _, ok := ShouldMark(input); !ok {", "if _, _, ok := ShouldMark(input); !ok && false {")],
         PKG_SVC, F_SVC, {f"{MS}MarkBadCase_Gates", f"{MS}MarkBadCase_PregateMatchesServiceGate",
                          f"{MS}MarkBadCase_UsesEffectiveConfidence"}, "不该留痕却交了", "test"),
        ("O2", "留痕沿用请求 ctx（请求一返回就写不进库）", ORCH,
         [("ctx, cancel := context.WithTimeout(context.Background(), badCaseMarkTimeout)",
           "ctx, cancel := context.WithTimeout(ctx, badCaseMarkTimeout)")],
         PKG_SVC, F_SVC, {f"{MS}MarkBadCase_UsesFreshContextNotRequestContext"}, "继承了请求 ctx", "test"),
        ("O3", "留痕超时与草稿超时不同源", ORCH,
         [("const badCaseMarkTimeout = 10 * time.Second", "const badCaseMarkTimeout = 12 * time.Second")],
         PKG_SVC, F_SVC, {f"{MS}MarkTimeoutSharesOneSource"}, "不同源", "test"),
        ("O4", "留痕的 recover 撤掉（panic 逃逸到主链路）", ORCH,
         [('if r := recover(); r != nil {\n\t\t\t\tlogger.Warnf("[bad-case] 留痕 panic 已 recover',
           'if r := "no-recover"; r != "" {\n\t\t\t\tlogger.Warnf("[bad-case] 留痕 panic 已 recover')],
         PKG_SVC, F_SVC, {f"{MS}MarkBadCase_MarkerPanicIsRecovered"}, "留痕里炸了", "panic"),
        # ---- AC③ 的"这一轮根本没有答案"闸：三个信号各一刀，再加一刀"整句调用删掉" ----
        # 四条的红名都是同一条用例（TestBadCaseMarkBadCase_TurnsWithNoAnswerAreNotMarked），
        # 但拆成四刀是必要的：三个 if 是并列的三类轮次，合成一刀的话谁被删掉都看不出来。
        ("O5", "转人工那一轮不再排除（系统公告被记成知识缺口）", ORCH,
         [("if resp.TransferredToHuman {", "if resp.TransferredToHuman && false {")],
         PKG_SVC, F_SVC, {f"{MS}MarkBadCase_TurnsWithNoAnswerAreNotMarked"}, "不该留痕却交了", "test"),
        ("O6", "追问澄清那一轮不再排除（含糊的问句算到知识库头上）", ORCH,
         [("resp.Intent.IntentType == IntentClarify {", "resp.Intent.IntentType == IntentGreeting {")],
         PKG_SVC, F_SVC, {f"{MS}MarkBadCase_TurnsWithNoAnswerAreNotMarked"}, "不该留痕却交了", "test"),
        ("O7", "生成失败那一轮不再排除（兜底文案当成模型答案收进评测集）", ORCH,
         [('step.Step == "6_generate_candidate" && step.Status == "fail" {',
           'step.Step == "6_generate_candidate" && step.Status == "fail_but_never" {')],
         PKG_SVC, F_SVC, {f"{MS}MarkBadCase_TurnsWithNoAnswerAreNotMarked"}, "不该留痕却交了", "test"),
        ("O8", "这道闸整句不再被调用（helper 还在、留痕照旧，没人拦那三类轮次）", ORCH,
         [("\tif badCaseTurnNotAnAnswer(resp) {\n\t\treturn\n\t}\n", "")],
         PKG_SVC, F_SVC, {f"{MS}MarkBadCase_TurnsWithNoAnswerAreNotMarked"}, "不该留痕却交了", "test"),

        # ---------------- 装配层 ----------------
        ("A1", "无 DB 句柄时不清空全局（路由对着未装配回 200）", WIRE,
         [("service.SetGlobalBadCaseService(nil)", "service.SetGlobalBadCaseService(service.GlobalBadCaseService())")],
         PKG_APP, F_APP, {"TestInitBadCaseRuntime_NilDBClearsGlobal",
          "TestBadCaseMarkerFor_BoundAtAttachTimeNotPerCall"}, "仍留有实例", "test"),
        ("A2", "attach 在未装配时谎报成功", WIRE,
         [("if marker == nil {", "if marker == nil && false {")],
         PKG_APP, F_APP, {"TestBadCaseMarkerFor_NilWhenNotAssembled"}, "不该报成功", "test"),
        ("A3", "标记器不取全局那份（装配与使用两套状态）", WIRE,
         [("return service.BadCaseMarker(service.GlobalBadCaseService())", "return service.BadCaseMarker(nil)")],
         PKG_APP, F_APP, {"TestBadCaseMarkerFor_WritesThroughProductionClosure",
          "TestBadCaseMarkerFor_BoundAtAttachTimeNotPerCall"}, "标记器不该为 nil", "test"),

        # ---------------- HTTP 出口（跑在 router 包的用例里） ----------------
        ("C1", "底座不可用回 500 而不是 503", CTRL,
         [("response.Error(ctx, http.StatusServiceUnavailable,", "response.Error(ctx, http.StatusInternalServerError,")],
         PKG_ROUTER, F_ROUTER, {f"{MS}Routes_UnavailableIs503WithDataless",
                               f"{MS}Routes_SetupUsesGlobalRegistry"}, "应 503", "test"),
        ("C2", "缺身份回 403 而不是 401", CTRL,
         [('response.Error(ctx, http.StatusUnauthorized, "打标必须带操作者身份（未登录或会话里没有 user id）")',
           'response.Error(ctx, http.StatusForbidden, "打标必须带操作者身份（未登录或会话里没有 user id）")')],
         PKG_ROUTER, F_ROUTER, {f"{MS}Routes_IdentityIsRequiredOnWriteEntries"}, "应 401", "test"),
        ("C3", "记录不存在回 500（前端会无限打圈）", CTRL,
         [('errors.Is(err, service.ErrBadCaseNotFound):\n\t\tresponse.Error(ctx, http.StatusNotFound, err.Error())',
           'errors.Is(err, service.ErrBadCaseNotFound):\n\t\tresponse.Error(ctx, http.StatusInternalServerError, err.Error())')],
         PKG_ROUTER, F_ROUTER, {f"{MS}Routes_LabelAndDismissCodes"}, "应 404", "test"),
        ("C4", "路径参数长度上限失效（响应体放大器）", CTRL,
         [("const badCaseIDParamMaxLen = 64", "const badCaseIDParamMaxLen = 1 << 30")],
         PKG_ROUTER, F_ROUTER, {f"{MS}Routes_GetByID"}, "过长 id 应 400", "test"),
        ("C6", "筛选器里的空值被透传（队列打不开）", CTRL,
         [('if v = strings.TrimSpace(v); v != "" {', 'if v = strings.TrimSpace(v); true {')],
         PKG_ROUTER, F_ROUTER, {f"{MS}Routes_QueryParams"}, "应回默认视图 200", "test"),
    ]


# ---------------------------------------------------------------- 工具

def read(path: Path) -> str:
    return path.read_text(encoding="utf-8")


def md5_bytes(path: Path) -> str:
    return hashlib.md5(path.read_bytes()).hexdigest()


def pkg_dir(pkg: str) -> Path:
    return ROOT / US / pkg[2:]


def test_files(pkg: str | None = None) -> list[Path]:
    base = pkg_dir(pkg) if pkg else (ROOT / SRV)
    return sorted(base.rglob("*_test.go"))


def red_defined(name: str, pkg: str) -> bool:
    """期望红名必须在**本格要跑的那个包**里有定义。

    早先的版本在全仓 *_test.go 里找：名字在别的包存在也算通过，于是本格的过滤器根本跑不到它，
    红集合永远对不上（预检绿、真跑必 BROKEN）。按包核才算核住。
    """
    return any(f"func {name}(" in p.read_text(encoding="utf-8") for p in test_files(pkg))


def reason_tokens(spec: str) -> list[str]:
    """红因表达式 = 若干 token 用 | 连接；判据是「同一行输出里同时出现全部 token」。

    为什么要同行：整段 `in out` 能把两条不相干的红因行拼成一条绿（一条断言说了两句真话
    不等于它说了这一句）。前缀语义：
      裸 token —— 必须能在**本包的用例文件**里查到（那是断言话术，红了就会印出来）；
      @      —— 必须在**本格注码那份产码**里查到（错误文本由产码抛出、用例用 %v 印出）；
      ~      —— 运行期才拼得出来的串（格式化后的值、子测试名），静态核不了，
               预检把它单独计数并披露，真跑的日志才是它的凭据。
    """
    return [t for t in (spec.split("|") if spec else []) if t]


def bare(tok: str) -> str:
    return tok[1:] if tok[:1] in ("~", "@") else tok


def reason_in_tests(tok: str, pkg: str) -> bool:
    return any(bare(tok) in p.read_text(encoding="utf-8") for p in test_files(pkg))


def reason_in_prod(tok: str, rel: str) -> bool:
    return bare(tok) in read(ROOT / rel)


def reason_ok(r: dict, spec: str) -> bool:
    toks = [bare(t) for t in reason_tokens(spec)]
    if not toks:
        return True
    return any(all(t in line for t in toks) for line in r["out"].splitlines())


def apply_edits(path: Path, original: str, edits: list[tuple[str, str]]) -> tuple[bool, str]:
    text = original
    for old, new in edits:
        if text.count(old) != 1:
            return False, f"锚点命中 {text.count(old)} != 1：{old[:70]!r}"
        text = text.replace(old, new, 1)
    path.write_text(text, encoding="utf-8")
    return True, ""


def lane_overlays() -> tuple[list[str], list[str]]:
    r = subprocess.run(["git", "-C", str(ROOT), "status", "--porcelain", "-uall", "--"] + LANE_PATHS,
                       capture_output=True, text=True, timeout=300)
    if r.returncode != 0:
        raise SystemExit("git status 失败，拿不到脏文件清单：" + r.stderr[-200:])
    mods, dels = [], []
    for line in r.stdout.splitlines():
        st = line[:2]
        p = line[3:].split(" -> ")[-1].strip().strip('"')
        (dels if "D" in st else mods).append(p)
    if not mods:
        raise SystemExit("脏文件清单为空——克隆里跑的是 HEAD，测不到本批改动（宁可停机也别假绿）")
    return mods, dels


def prepare(dst: Path, owned: bool = False) -> Path:

    def bail(msg: str) -> None:
        """克隆已经建起来之后的中止路：先回收私有克隆，再出声。

        收尾闸原先只接在 `main()` 的出口上，装架函数里克隆之后的每一条 raise 都把整份
        私有克隆留在临时目录（一轮 50–70MB，而磁盘常态 98% 满）。2026-09-28 在
        `mut_bill_p701.py` 上实测一次 DIRTY 停机留 72M，这一族按同一形状补齐。
        三条**不**走这里："已存在"（那份 clone/ 不是本电池建的）、"克隆失败"（目录归属
        还没定）、"md5 不一致"（"覆盖后还是不对"的字节只活在克隆里，删了就只剩一句
        "当时红过"——与 `main()` 侧还原校验同一取舍）。
        """
        if (dst / "clone").exists():
            dispose(dst, owned=owned, keep=False, repo_root=ROOT)
        raise SystemExit(msg)
    clone = dst / "clone"
    if clone.exists():
        raise SystemExit(f"{clone} 已存在（换 --clone 目录或先删）")
    r = subprocess.run(["git", "clone", "--shared", "--no-checkout", str(ROOT), str(clone)],
                       capture_output=True, text=True, timeout=900)
    if r.returncode != 0:
        raise SystemExit("克隆失败：" + (r.stdout + r.stderr)[-400:])
    b = subprocess.run(["git", "checkout", "-f", "master"], cwd=clone,
                       capture_output=True, text=True, timeout=900)
    if b.returncode != 0:
        bail("checkout 失败：" + (b.stdout + b.stderr)[-400:])
    mods, dels = lane_overlays()
    for rel in mods:
        src = ROOT / rel
        if not src.exists():
            bail(f"覆盖源缺失：{src}")
        tgt = clone / rel
        tgt.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(src, tgt)
    for rel in dels:
        (clone / rel).unlink(missing_ok=True)
    print(f"覆盖 {len(mods)} 个脏文件、同步 {len(dels)} 个删除进克隆")
    for env in (ROOT / US / ".env", ROOT / ".env"):
        if env.exists():
            shutil.copy2(env, clone / US / ".env")
            break
    return clone


def db_password(root: Path) -> str:
    if os.environ.get("POSTGRES_TEST_PASSWORD"):
        return os.environ["POSTGRES_TEST_PASSWORD"]
    for env in (root / US / ".env", root / ".env"):
        if env.exists():
            for line in read(env).splitlines():
                if line.startswith("POSTGRES_PASSWORD="):
                    return line.split("=", 1)[1].strip()
    return ""


def test_env(clone: Path) -> dict:
    env = dict(os.environ)
    # 构建用的 GOOS=linux 绝不能带进测试：那是交叉编译，测试二进制在 mac 上 exec 不了。
    for k in ("GOOS", "GOARCH"):
        env.pop(k, None)
    env["CGO_ENABLED"] = "0"
    env.setdefault("GIN_MODE", "test")
    env.setdefault("GOCACHE", os.environ.get("P803_MUT_GOCACHE", "/tmp/gocache-r45mut"))
    env.setdefault("POSTGRES_TEST_HOST", "127.0.0.1")
    env.setdefault("POSTGRES_TEST_PORT", "8232")
    env.setdefault("POSTGRES_TEST_USER", "admin")
    if not env.get("POSTGRES_TEST_PASSWORD"):
        pw = db_password(clone)
        if not pw:
            raise SystemExit("ENV-BROKEN：拿不到 POSTGRES_TEST_PASSWORD（.env 不在位、环境也没给）")
        env["POSTGRES_TEST_PASSWORD"] = pw
    return env


def run_go(clone: Path, pkg: str, filt: str, env: dict, timeout: int = 2400) -> dict:
    argv = ["go", "test", pkg, "-run", filt, "-count", "1", "-v", "-timeout", "20m"]
    p = subprocess.run(argv, cwd=clone / US, capture_output=True, text=True, timeout=timeout, env=env)
    out = ANSI.sub("", p.stdout + p.stderr)
    top = lambda kind: len(re.findall(rf"^--- {kind}: ", out, re.M))
    return {"rc": p.returncode, "out": out,
            "settled": top("PASS") + top("FAIL") + top("SKIP"),
            "skipped": top("SKIP"),
            "red": sorted({m.split("/")[0] for m in re.findall(r"^--- FAIL: (\S+)", out, re.M)}),
            "panicked": bool(re.search(r"^panic: |^fatal error: ", out, re.M)),
            "buildfailed": "[build failed]" in out or "undefined:" in out
                           or "declared and not used" in out or "not enough arguments" in out
                           or "imported and not used" in out or "expected " in out and "found " in out}


def causes(out: str) -> list[str]:
    keep = [l.strip()[:240] for l in out.splitlines()
            if re.match(r"^\s{2,}\S+\.go:\d+:", l) or "panic:" in l]
    return keep[:8]


def pg_ready(env: dict) -> bool:
    import socket
    host = env.get("POSTGRES_TEST_HOST", "127.0.0.1")
    port = int(env.get("POSTGRES_TEST_PORT", "8232"))
    try:
        with socket.create_connection((host, port), timeout=3):
            return True
    except OSError:
        return False


# ---------------------------------------------------------------- 预检

def preflight() -> int:
    """静态自检：不放刀、不编译、不跑用例，只核磁盘上能核的三件事。

    边界要写明：②只挡"字面量根本不存在"，挡不住"字面量在仓库里查得到、但不是这格该开火的
    那条分支"（那是 P1 那类假定性红）⇒ 这一类只有真跑照得出来，`--check` 绿**不是**"电池有牙"。
    """
    bad: list[str] = []
    texts: dict[str, str] = {}
    seen_codes: set[str] = set()
    runtime_tokens: list[str] = []
    for code, desc, rel, edits, pkg, filt, expect_red, expect_reason, judge in cells():
        if code in seen_codes:
            bad.append(f"{code}：格名重复")
        seen_codes.add(code)
        if not rel.startswith(SRV):
            bad.append(f"{code}：注码目标 {rel} 不在本泳道路径里")
        p = ROOT / rel
        if not p.exists():
            bad.append(f"{code}：注码目标文件不存在：{rel}")
            continue
        text = texts.setdefault(rel, p.read_text(encoding="utf-8"))
        for old, new in edits:
            n = text.count(old)
            if n != 1:
                bad.append(f"{code}：{rel} 的锚点命中 {n} != 1：{old[:60]!r}")
            if old == new:
                bad.append(f"{code}：锚点与新值相同（无效果变异）")
        if not expect_red:
            bad.append(f"{code}：没写期望红名")
        for leg in sorted(expect_red):
            if not red_defined(leg, pkg):
                bad.append(f"{code}：期望红名 {leg} 在 {pkg} 的 *_test.go 里没有 func 定义")
        if judge == "test" and not expect_reason:
            bad.append(f"{code}：test 族必须给红因（只判红不等证据）")
        for tok in reason_tokens(expect_reason):
            if tok[:1] == "~":
                runtime_tokens.append(f"{code}:{tok}")
                continue
            if tok[:1] == "@":
                if not reason_in_prod(tok, rel):
                    bad.append(f"{code}：@红因 {tok!r} 不在注码文件 {rel} 里")
                continue
            if not reason_in_tests(tok, pkg):
                bad.append(f"{code}：红因 {tok!r} 在 {pkg} 的用例文件里查不见")
        if pkg not in ("./internal/model/", "./internal/repository/", "./internal/service/",
                       "./internal/router/", "./internal/app/"):
            bad.append(f"{code}：包路径不在本卡五个被测包里：{pkg}")
    n = len(cells())
    print(f"预检 {n} 格：" + ("全部通过（锚点唯一、红名在本包有定义、红因 token 在位）"
                          if not bad else f"{len(bad)} 处问题"))
    for b in bad:
        print("  !! " + b)
    if runtime_tokens:
        print(f"  注意：{len(runtime_tokens)} 个 ~ 红因是运行期才拼得出的串，静态核不了："
              + ", ".join(runtime_tokens))
    print("提醒：--check 只核磁盘上能核的事，不替代真跑；~ 那一档的凭据是真跑日志里的那一行。")
    return 1 if bad else 0


# ---------------------------------------------------------------- 驱动自身的反向证明

def selftest(logs: Path, clone: Path, env: dict) -> int:
    """三格合成刀，证明本驱动**会说不了**：无效果变异→SURVIVED、期望写歪→BROKEN、编译坏→BUILD-BROKEN。

    为什么要有这一档：一个只会印"杀掉"的计数器比没有电池更坏——它把"没测"印成"测过了"。
    打 model 包（无 DB、跑得快），且**不碰本仓的产码字节**（变异写进克隆里那份，跑完按 md5 还原）。
    """
    checks = [
        ("ST1 无效果变异", [("BadCaseStatusPending   = \"pending\"", "BadCaseStatusPending   = \"pending\"")],
         {"TestBadCase_StatusesAreClosed"}, "SURVIVED"),
        ("ST2 期望写歪", [("BadCaseStatusPending:   {BadCaseStatusLabeled, BadCaseStatusDismissed},",
                            "BadCaseStatusPending:   {BadCaseStatusLabeled},")],
         {"TestBadCase_NoSuchLeg"}, "BROKEN"),
        ("ST3 编译坏", [("BadCaseStatusPending   = \"pending\"", "BadCaseStatusPending = ")],
         set(), "BUILD-BROKEN"),
    ]
    rel = MODEL
    path = clone / rel
    original = read(path)
    base = md5_bytes(path)
    filt_ok = True
    for name, edits, expect_red, want in checks:
        ok, why = apply_edits(path, original, edits)
        if not ok:
            print(f"[selftest] {name}: PATCH-BROKEN（夹具锚点坏了）{why}")
            filt_ok = False
            continue
        try:
            r = run_go(clone, PKG_MODEL, F_MODEL, env)
        finally:
            path.write_text(original, encoding="utf-8")
            if md5_bytes(path) != base:
                leave_for_evidence("还原后 md5 不一致：现场只活在克隆里")
                raise SystemExit(f"[selftest] {name} 还原后 md5 不一致，停机")
        (logs / f"selftest_{want}.log").write_text(scrub(r["out"]))
        got = classify(r, None, expect_red, "", "test")
        mark = "OK " if got.startswith(want) else "BAD"
        print(f"[selftest] {mark} {name}: 期望驱动报 {want}，实际报 {got}")
        if not got.startswith(want):
            filt_ok = False
    print("[selftest] " + ("驱动能报出三种不成立态，可以放整族" if filt_ok else "驱动自己不合格：修驱动，别放整族"))
    return 0 if filt_ok else 1


def classify(r: dict, control: int, expect_red: set[str], expect_reason: str,
             judge: str, control_panic: int | None = None) -> str:
    if r["buildfailed"]:
        return "BUILD-BROKEN：编译坏（修变异不修期望）"
    if judge == "panic":
        if not r["panicked"]:
            return "SURVIVED=洞：撤掉 recover 之后没有任何 panic 逃逸"
        if not reason_ok(r, expect_reason):
            return f"BROKEN=红因而非预期：进程崩了但输出里没有同行含 {expect_reason!r} 的行"
        return "杀掉（panic 族）"
    if r["panicked"]:
        return "RAN-BROKEN：用例 panic 带走整个二进制，settled 不可信"
    if control is not None and r["settled"] != control:
        return f"RAN-BROKEN：settled={r['settled']} != 控制组 {control}"
    if r["skipped"]:
        return f"BROKEN=没跑完：{r['skipped']} 条 SKIP（测试库不可达会走这条）"
    if not r["red"]:
        return "SURVIVED=洞：没有用例被打红 ⇒ 这条承诺只有注释在守"
    if set(r["red"]) != expect_red:
        return f"BROKEN=红集合不符：期望={sorted(expect_red)} 实际={r['red']}"
    if not reason_ok(r, expect_reason):
        return f"BROKEN=红因而非预期：红名对了但没有任何一行同时含 {expect_reason!r}"
    return "杀掉"


# ---------------------------------------------------------------- main

def main() -> int:
    ap = argparse.ArgumentParser(description="T-P8-03 Bad Case 闭环变异电池")
    ap.add_argument("--clone", default=None, help="私有作业目录（默认自动开临时目录；不许指工作树）")
    ap.add_argument("--keep", action="store_true", help="收尾不删私有克隆")
    ap.add_argument("--only", default="", help="只跑指定格（逗号分隔）；终态会写明本趟是子集")
    ap.add_argument("--check", action="store_true", help="只做静态预检，不装架不放刀")
    ap.add_argument("--selftest", action="store_true", help="驱动自身的反向证明（三格）")
    ap.add_argument("--logs", default=DEFAULT_LOGS)
    ap.add_argument("--tag", default=time.strftime("%Y%m%d-%H%M%S"))
    args = ap.parse_args()

    if args.check:
        return preflight()

    only = {s.strip() for s in args.only.split(",") if s.strip()}
    st = os.statvfs("/tmp")
    if st.f_bavail * st.f_frsize < 10 * 1024 ** 3:
        print("ENV-BROKEN：/tmp 空闲不足 10 GB（编译缓存写满盘带来的红全是假红）")
        return 4

    logs = ROOT / args.logs / args.tag
    logs.mkdir(parents=True, exist_ok=True)
    from battlog import tee_to
    tee_to(logs / "00-run.log")

    tmp, owned = workdir(args.clone, prefix="p803mut-", repo_root=ROOT)
    tmp.mkdir(parents=True, exist_ok=True)
    rc = 3
    try:
        head = subprocess.run(["git", "-C", str(ROOT), "rev-parse", "--short", "HEAD"],
                              capture_output=True, text=True).stdout.strip()
        load = subprocess.run(["uptime"], capture_output=True, text=True).stdout.strip()
        print(f"私有作业目录：{tmp}\n逐格日志目录：{logs}")
        print(f"取证基线：HEAD={head} 工作树={ROOT} {load} 日志 tag={args.tag}")

        clone = prepare(tmp, owned)
        dispose_at_exit(tmp, owned=owned, keep=args.keep, repo_root=ROOT)
        env = test_env(clone)
        if not pg_ready(env):
            print(f"ENV-BROKEN：测试库 {env['POSTGRES_TEST_HOST']}:{env['POSTGRES_TEST_PORT']} 连不上 ⇒ "
                  "所有『空表/真冲突/并发』腿都会 SKIP，本趟不放刀")
            return 4
        print(f"测试库：{env['POSTGRES_TEST_HOST']}:{env['POSTGRES_TEST_PORT']} 可达（口令不回显）")

        all_cells = cells()
        mutated = sorted({c[2] for c in all_cells})
        files = {rel: clone / rel for rel in mutated}
        originals = {rel: read(p) for rel, p in files.items()}
        base = {rel: md5_bytes(p) for rel, p in files.items()}
        print("本轮基线字节（注码前 == 还原后 才算干净）：")
        for rel in mutated:
            print(f"  {base[rel]}  {rel}")

        if args.selftest:
            return selftest(logs, clone, env)

        # -------- 控制组：五个过滤器各现测一次 settled 与红名 --------
        controls: dict[str, dict] = {}
        problems: list[str] = []
        for pkg, filt in ((PKG_MODEL, F_MODEL), (PKG_REPO, F_REPO), (PKG_SVC, F_SVC),
                          (PKG_ROUTER, F_ROUTER), (PKG_APP, F_APP)):
            c = run_go(clone, pkg, filt, env)
            key = pkg + filt
            (logs / f"control_{key.replace('./internal/', '').replace('/', '_').replace('^', '')}.log").write_text(
                scrub(c["out"]))
            print(f"[控制组 {pkg}] rc={c['rc']} settled={c['settled']} skip={c['skipped']} 红名={c['red'] or '—'}")
            if c["rc"] != 0 or c["settled"] == 0 or c["skipped"] or c["red"] or c["panicked"]:
                print("\n".join(causes(c["out"])))
                print("ENV-BROKEN：控制组不干净——后面所有红/绿都不可信，停机")
                return 4
            controls[key] = c

        # -------- 逐格注码 --------
        killed = ran = 0
        for code, desc, rel, edits, pkg, filt, expect_red, expect_reason, judge in all_cells:
            if only and code not in only:
                continue
            ran += 1
            ok, why = apply_edits(files[rel], originals[rel], edits)
            if not ok:
                status, detail = "PATCH-BROKEN", why
            else:
                try:
                    r = run_go(clone, pkg, filt, env)
                finally:
                    files[rel].write_text(originals[rel])
                    if md5_bytes(files[rel]) != base[rel]:
                        leave_for_evidence("还原后 md5 不一致：现场只活在克隆里")
                        raise SystemExit(f"{code} 还原后 md5 与开刀前不一致，停机（后面全是脏树读数）")
                (logs / f"{code}.log").write_text(scrub(r["out"]))
                status = classify(r, controls[pkg + filt]["settled"], expect_red, expect_reason, judge)
                detail = f"红={','.join(r['red']) or '—'} settled={r['settled']}/{controls[pkg + filt]['settled']}"
            print(f"{code:<4} {desc[:44]:<46} {status:<28} {detail}")
            if status.startswith("杀掉"):
                killed += 1
            else:
                problems.append(f"{code} {status}：{desc}")
                for line in causes(r["out"] if ok else ""):
                    print(f"      红因: {line}")

        leftover = []
        for rel, p in files.items():
            if md5_bytes(p) != base[rel]:
                leftover.append(rel)
        if leftover:
            problems.append(f"还原残留：{leftover}")

        subset = f"（本趟只跑了 {ran}/{len(all_cells)} 格，是子集，**不构成全族读数**）" if only else ""
        print(f"\n结果：杀掉 {killed}/{ran} 格 {subset}")
        if problems:
            print("未杀/坏格清单：")
            for p in problems:
                print("  !! " + p)
            print("终态：本卡电池**未**全杀——上面每一条要么补腿、要么按'不配格'那一节写明理由。")
            rc = 1
        elif not only and ran == len(all_cells):
            print("终态：全部格子杀掉，逐文件 md5 已复原，控制组与取证日志见上面的 tag。")
            rc = 0
        else:
            print("终态：跑到的格子全杀，但本趟是子集/未覆盖全族，不写'全杀'。")
            rc = 0 if ran else 1
        return rc
    finally:
        dispose(tmp, owned=owned, keep=args.keep, repo_root=ROOT)


if __name__ == "__main__":
    raise SystemExit(main())
