#!/usr/bin/env python3
"""T-P9-02（G-5 知识库变更流程 / 发布制）牙齿电池：逐刀拆掉本卡的每句承诺，必须有人红。

## 为什么本卡需要电池

交付面横跨七层：叶子闸门包（`internal/pkg/kbrelease`）、三张表的
model / repository / service / controller / app 装配 / router 挂载，加上**既有召回与写入通路的
15 个读侧拼接点、2 个写侧打戳点、1 个就地改写守卫、1 个 409 映射**
（`aiagent/knowledge/{repository,service,controller}` 与 `aiagent/rag/retrieval`）。
四个点数是 2026-09-28 现测，复算命令：
  `grep -rn "kbrelease\.\(AndVisible\|WhereVisible\)(" internal/aiagent | grep -v _test | wc -l` = 15
  （`rag/retrieval` 11 + `knowledge/service` 4）；打戳 = `knowledge_chunk.go` 的 Create/BatchCreate；
  就地改写 = 同一个 `guardDirectWrite` 被 Update/Delete 两个方法共用（**守卫一处、消费两口**：
  摘掉函数体两口一起漏，所以本卡按消费口配格、不按守卫语句配格）。
用例绿只证明"写下的断言成立"，不证明"断言看得见这一层的失效"。本卡最坏的一类失效恰好是
**删掉它没有任何用例会红**，而它的表现全在线上：

- 摘掉任一召回点的 `kbrelease.AndVisible()` ⇒ 那一路检索照样有结果、照样绿，
  而"未发布的内容当场可检"就是 AC① 的直接反例；
- 摘掉 `StampForWrite` 那一句 ⇒ 导入的内容带着 `kb_version=0` 落库，
  从此**永远**不受闸门管（0 的语义是"没进发布制"），且无声；
- 摘掉 `recalled_version` 那道发布禁令 ⇒ "回滚后再发布"把刚撤下的语料静默放回来，
  没有任何一次批准记录它回来过。

## 判据形状（照本仓既有电池的规矩）

- **test 族**（全部格子）：注码后跑该层那一个过滤器，要求
  ① `settled == 控制组`（一条用例 panic 会带走整个二进制，那时 settled 会掉，必须判 BROKEN 而不是"杀掉了"）；
  ② 红名集合**恰好等于**推演的那几条（多红=连带面没写清，少红=判据没开火）；
  ③ 红因里要点名本格那条断言的话术（`expect_reason`）——只判"红了"可能红在别处。
     红因写成 `a|b|c` 若干个 token，判据是**同一行输出里同时含全部 token**；前缀分三档：
     裸 token 在本包用例文件里核、`@` 在本格注码的产码文件里核、`~` 是运行期才拼得出的串
     （格式化后的值、子测试名），静态核不了 ⇒ `--check` 把它们逐条点名披露，凭据只能来自真跑日志。
- 本卡**没有 panic 族**：五层里没有任何"撤掉 recover 就该逃逸"的承诺（发布是一个事务，
  失败就是没发生，不需要 recover 兜）。

## 三条 AC 各自的格

- **AC①（未批准的变更不进线上检索）**：`kb_change_requests.content` 在 apply 之前一行都不进
  `knowledge_chunks` ⇒ L/RP 组的"落库只在发布事务内"那一族；读侧闸门 ⇒ RR/KS/LF 组的 11 个拼接点；
  写侧打戳 ⇒ KR 组与 RP 组的桶号族。
- **AC②（回滚只是指针回拨，一个字节都不碰语料）**：RP 组的 `movePointer` 族 +
  `RollbackTouchesNoCorpus` 那一族断言（逐字段比快照）。
- **AC③（谁改的、为什么改、什么时候生效，全查得到）**：LF/RP/SV/CT 组的留痕族
  （subject_key 形状、改前改后两格、动作名、字段不全必拒）。

## 不配格的八处（写明，别把"没数到"印成"没问题"）

1. `service.SubmitChange` 里 `approvals.Submit` 失败后的 `if err != nil { return }`：
   摘掉它就走 `if appr == nil` 那一格（SV11），两条判据同形 ⇒ "同一判据两处写"的冗余。
   可观测的是"不落变更行"这件事，而 SV11 那一刀已经把它钉住了（摘 SV11 会红）。
2. `kbrelease.StampForWrite` 的 `if pid == "" { continue }`：空归属时 `EnsureDraftStamp` 本来就
   回 `(0, nil)`，`if v <= 0` 那一格会接着跳过 ⇒ 摘掉这一句与不摘，落库结果逐字节相同。
   守卫的是"少一次点查"，不是"少一次放行"（性能面，无行为差）。
3. `app.InitKBReleaseRuntime` 里 `if !approvalsFromGlobal` 与 `if !svc.Available()` 两句：
   纯出声（Warnf），没有任何行为分支挂在它们后面。装配面本身由 AP 组三格覆盖。
4. `kbrelease.EnsureDraftStamp` 的 `if got <= 0` 那句**错误文案**：走到它需要分配 SQL 自己
   失效，而那正是 LF14 那一刀（`AllocateDraftSQL` 的 `draft_version = 0` 幂等键）的失效形状
   ⇒ 已按"注码点在 SQL、判据在 KSVC/KREPO 真库用例"配成格（RP13），这里不重复一刀。
5. `router/router.go` 的"挂载必须晚于底座装配"：这是一条**源码形状**断言
   （`TestKBReleaseRoutes_AssemblyOrderInRouter`），要把它改坏只能把 `setupKBReleaseRoutes(auth)`
   搬进 `auth` 还不存在的更早作用域 ⇒ 编译必坏，注码不成立。RT2 那一刀（底座先于审批运行时）
   是同一条门的另一条腿，那条腿**可以**在同作用域内交换 ⇒ 已配格。
6. `controller` 里十二处 `if !c.svc.Available() { c.unavailable(ctx) }` 的**逐出口**存在性：
   摘掉任意一处只会让那一个出口落到服务层（服务层第一句同样是 `Available()` 判断、
   回同一个 sentinel、控制层 `replyError` 映回同一个 503）⇒ 对外形状一模一样。
   CT1 那一刀打在共用的 `unavailable()` 上，十二处出口一起红，这才是"503 这个码"的担当。
7. `kbrelease.WhereVisible` 的**判空半边**（`p != ""` 换成 `p != "" || true`）：GORM 的
   `BuildCondition` 对空串直接 `return nil`（statement.go:294），而 `getInstance()` 在
   `clone == 0` 时返回**同一个**指针（gorm.go:426）⇒ 从 `.Model().Where()` 出来的链上再挂一次
   `Where("")` 既不新增条件也不换实例，"SQL 里有没有判据"和"是不是同一条链"两处断言都量不到它。
   20260928-160651 那一轮实测 `LF13 SURVIVED 红=— settled=14/14`，坐实它是等价变异而不是用例没写。
   LF13 因此改打同一句的**另一半**（`p != ""` → `p == ""`，开闸时不挂谓词）——那一半在 `ToSQL` 里
   看得见。判空半边守的是"GORM 换了版本以后 `Where("")` 会不会报错"（见函数注释），只有注释在守。
8. `repository.LockRelease` 里 `if rel.ID == 0 { return ErrKBReleaseNotFound }` 那一格：它紧跟
   同一事务内的 `INSERT ... ON CONFLICT (product_id) DO NOTHING`，读回来必然是非 0 的号 ⇒
   这一分支只在"库被外部动过"时才走得到，测试树里没有任何通路能让它开火（20260928-163857
   实测 `RP4 SURVIVED 红=— settled=19/19`，坐实不可达而不是用例没写）。RP4 因此改打
   **同一个承诺的可达那一半**（`GetRelease` 里同形状的判空，用例 `kb_release_test.go:214` 直接消费）。
   这一格守的是"账目被外部改过时宁可停下"，属防御性分支，只有注释与 code review 在守。

## 口径

- 只在 `git clone --shared` 出来的私有克隆里注码，脏文件按 `git status --porcelain -uall` 覆盖进去。
  `LANE_PATHS` 取**整个 `user-server/internal`** 而不是本卡 28 个文件的名单：装配层
  `app/kb_release_wiring.go` 调的 `approvalSubmitReader` 只存在于另一条泳道未提交的
  `app/quote_wiring.go` 里，只覆盖本卡文件会得到一份**编译不过**的克隆（BUILD-BROKEN 会伪装成
  "变异坏了"）。代价是别条泳道的脏状态一起进克隆 —— 控制组不干净就停机（rc=4）兜住这一风险。
- 控制组**现测**：红名与 settled 都不写死（共享树下别人加用例会让写死的数字漂）；
  本卡有 **11 个过滤器**，任一个控制组不干净就整体停机，不放刀。
- 一格多处编辑在内存里叠完一次写盘；每刀还原后逐文件比 md5，不等即停机。
- BUILD-BROKEN / settled 掉 / 红而不点名 ⇒ 一律 BROKEN，不计入杀掉。
- 环境前提不满足（盘、测试库）退 ENV-BROKEN 并**不**印"全杀"。
  逐格跑的是**带真库**的包（repository / aiagent 三个包 / controller 的 RealStack），
  所以 8232 连不上就是整批假红 ⇒ 放刀前必过 TCP 探活（8232 是端口转发，没有 unix socket，
  `nc -U` 那一族的"不可达"是探法错了，不是库没了）。
- 逐格原始输出落 `docs/.../ledger/logs/P902/<tag>/`，tag 默认取本地时间戳，复跑不覆盖上一轮。
- `--selftest` 先证"驱动会说不了"：三格合成刀（无效果变异必须报 SURVIVED、期望写歪必须报
  BROKEN、编译坏必须报 BUILD-BROKEN）。**改完驱动先跑这一档再放整族**。

用法：
    python3 scripts/mut_kb_release_p902.py --selftest
    python3 scripts/mut_kb_release_p902.py --check
    python3 scripts/mut_kb_release_p902.py --only SV17,RP11
    python3 scripts/mut_kb_release_p902.py                     # 全族
"""
from __future__ import annotations

import argparse
import hashlib
import os
import re
import shutil
import subprocess
import sys
import time
from pathlib import Path

from mut_dispose import dispose, workdir
from redact import scrub

ROOT = Path(__file__).resolve().parent.parent
US = "user-server"
SRV = f"{US}/internal"
LANE_PATHS = [SRV]
DEFAULT_LOGS = "docs/superpowers/specs/ledger/logs/P902"
ANSI = re.compile(r"\x1b\[[0-9;]*m")

MODEL = f"{SRV}/model/kb_release.go"
REPO = f"{SRV}/repository/kb_release.go"
SVC = f"{SRV}/service/kb_release.go"
CTRL = f"{SRV}/controller/kb_release.go"
ROUTERGO = f"{SRV}/router/kb_release_routes.go"
MAINROUTER = f"{SRV}/router/router.go"
WIRE = f"{SRV}/app/kb_release_wiring.go"
LEAF = f"{SRV}/pkg/kbrelease/kbrelease.go"
KREPO = f"{SRV}/aiagent/knowledge/repository/knowledge_chunk.go"
KSVC = f"{SRV}/aiagent/knowledge/service/rag_searcher.go"
KBM25 = f"{SRV}/aiagent/knowledge/service/rag_searcher_bm25.go"
KVEC = f"{SRV}/aiagent/knowledge/service/rag_searcher_vector.go"
KCTRL = f"{SRV}/aiagent/knowledge/controller/knowledge_merchant.go"
HYB = f"{SRV}/aiagent/rag/retrieval/hybrid_searcher.go"
LEX = f"{SRV}/aiagent/rag/retrieval/lexical_retriever.go"
VEC = f"{SRV}/aiagent/rag/retrieval/vector_retriever.go"

PKG_MODEL = "./internal/model/"
PKG_REPO = "./internal/repository/"
PKG_SVC = "./internal/service/"
PKG_CTRL = "./internal/controller/"
PKG_ROUTER = "./internal/router/"
PKG_APP = "./internal/app/"
PKG_LEAF = "./internal/pkg/kbrelease/"
PKG_KREPO = "./internal/aiagent/knowledge/repository/"
PKG_KSVC = "./internal/aiagent/knowledge/service/"
PKG_KCTRL = "./internal/aiagent/knowledge/controller/"
PKG_RR = "./internal/aiagent/rag/retrieval/"
PKG_ALL = (PKG_MODEL, PKG_REPO, PKG_SVC, PKG_CTRL, PKG_ROUTER, PKG_APP, PKG_LEAF,
           PKG_KREPO, PKG_KSVC, PKG_KCTRL, PKG_RR)

F_MODEL = "^TestKBChangeModel"
F_REPO = "^TestKBReleaseRepo"
F_SVC = "^TestKBRelease_"
F_CTRL = "^TestKBReleaseController"
F_ROUTER = "^TestKBReleaseRoutes"
F_APP = "^TestInitKBReleaseRuntime"
F_LEAF = "^TestKbRelease_"
F_KREPO = "^TestKbRepo_"
F_KSVC = "^TestKbGate_"
F_KCTRL = "^TestKM_ChunkWriteErrorMapsGovernedTo409$"
F_RR = "^TestKbGate_"

CONTROLS = ((PKG_MODEL, F_MODEL), (PKG_REPO, F_REPO), (PKG_SVC, F_SVC), (PKG_CTRL, F_CTRL),
            (PKG_ROUTER, F_ROUTER), (PKG_APP, F_APP), (PKG_LEAF, F_LEAF),
            (PKG_KREPO, F_KREPO), (PKG_KSVC, F_KSVC), (PKG_KCTRL, F_KCTRL), (PKG_RR, F_RR))

MD = "TestKBChangeModel_"
RP = "TestKBReleaseRepo_"
SV = "TestKBRelease_"
CT = "TestKBReleaseController_"
RT = "TestKBReleaseRoutes_"
AP = "TestInitKBReleaseRuntime_"
LF = "TestKbRelease_"
KR = "TestKbRepo_"
KG = "TestKbGate_"
KM = "TestKM_ChunkWriteErrorMapsGovernedTo409"


# code, 说明, 注码文件, [(旧, 新)], 包, 过滤器, 期望红名, 期望红因, 判据族
def cells() -> list[tuple]:
    return [
        # ---------------- model：值域、跃迁表、表名、审计动作 ----------------
        ("M1", "动作值域少一项（retire 成了非法动作）", MODEL,
         [("var KBChangeOps = []string{KBChangeOpAdd, KBChangeOpRevise, KBChangeOpRetire}",
           "var KBChangeOps = []string{KBChangeOpAdd, KBChangeOpRevise}")],
         PKG_MODEL, F_MODEL, {f"{MD}OpValueDomain"}, "动作值域应为 3 项", "test"),
        ("M2", "状态值域少一项（withdrawn 从契约里消失）", MODEL,
         [("\tKBChangeStatusWithdrawn,\n}", "}")],
         PKG_MODEL, F_MODEL, {f"{MD}StatusValueDomain", f"{MD}TransitionTableCoversStatuses"},
         "状态值域应为 3 项", "test"),
        ("M3", "applied 不再是终态（已发布的能直接撤回）", MODEL,
         [("\tKBChangeStatusApplied:   {},", "\tKBChangeStatusApplied:   {KBChangeStatusWithdrawn: true},"),],
         PKG_MODEL, F_MODEL, {f"{MD}TransitionTable"}, '~"applied"→"withdrawn"|KBChangeTransitionAllowed(', "test"),
        ("M4", "跃迁表少一个键（新加状态忘了开行）", MODEL,
         [("\tKBChangeStatusWithdrawn: {},\n", "")],
         PKG_MODEL, F_MODEL, {f"{MD}TransitionTableCoversStatuses"}, "两边不同源", "test"),
        ("M5", "pending 不能直接 apply（发布这条路被断）", MODEL,
         [("\t\tKBChangeStatusApplied:   true,", "\t\tKBChangeStatusApplied:   false,")],
         PKG_MODEL, F_MODEL, {f"{MD}TransitionTable"}, '~"pending"→"applied"|KBChangeTransitionAllowed(', "test"),
        ("M6", "表名漂移（闸门片段写死的是另一个）", MODEL,
         [('func (KBChangeRequest) TableName() string { return "kb_change_requests" }',
           'func (KBChangeRequest) TableName() string { return "kb_change_request" }')],
         PKG_MODEL, F_MODEL, {f"{MD}TableNames"}, "写死的是后者", "test"),
        ("M7", "审计动作值域里出现重复", MODEL,
         [("\tKBAuditGoverned,\n}", "\tKBAuditGoverned,\n\tKBAuditRestored,\n}")],
         PKG_MODEL, F_MODEL, {f"{MD}AuditActionsAreDistinct"}, "审计动作", "test"),
        ("M8", "状态判据与值域分叉（rejected 被判成合法）", MODEL,
         [("func IsValidKBChangeStatus(s string) bool {\n\tfor _, v := range KBChangeStatuses {\n\t\tif v == s {\n\t\t\treturn true\n\t\t}\n\t}\n\treturn false\n}",
           "func IsValidKBChangeStatus(s string) bool { return s != \"\" }")],
         PKG_MODEL, F_MODEL, {f"{MD}StatusValueDomain"}, "状态值域里出现了", "test"),

        # ---------------- 叶子闸门：档位解析、谓词形状、快路径 ----------------
        ("LF1", "shadow 这个档位名失联（观察期变成真拦）", LEAF,
         [("\tcase \"shadow\", \"observe\", \"watch\", \"log\", \"report\":\n\t\treturn modeShadow",
           "\tcase \"shadow\", \"observe\", \"watch\", \"log\", \"report\":\n\t\treturn modeOff")],
         PKG_LEAF, F_LEAF, {f"{LF}parseMode", f"{LF}ShadowsOrOn", f"{LF}ModeIsReadPerCall"},
         "shadow 档 ShadowsOrOn 必须为真", "test"),
        ("LF2", "on 不再等于开闸（显式写 on 的人拿不到拦截）", LEAF,
         [("\tcase \"on\", \"enforce\", \"active\":", "\tcase \"enforce\", \"active\":")],
         PKG_LEAF, F_LEAF,
         {f"{LF}parseMode", f"{LF}PredicateShape", f"{LF}ShadowsOrOn",
          f"{LF}ModeIsReadPerCall", f"{LF}WhereVisible", f"{LF}UnknownValueWarns"},
         "谓词应以 NOT EXISTS ( 起头", "test"),
        ("LF3", "布尔式真值直接拿到 on（按习惯写 =true 就改了生产召回）", LEAF,
         [("\t\tif b {\n\t\t\treturn modeShadow", "\t\tif b {\n\t\t\treturn modeOn")],
         PKG_LEAF, F_LEAF, {f"{LF}parseMode", f"{LF}PredicateEmptyUnlessOn"},
         "时 GateOn 必须为假", "test"),
        ("LF4", "认不出的值保守回 off 这条失效（拼错就开闸）", LEAF,
         [("\tlogger.Warnf(\"[kb-release] %s=%q 无法识别 ⇒ 按 off 处理（召回不受版本闸门约束）；可用值：off|shadow|on\", FlagEnv, raw)\n\treturn modeOff",
           "\tlogger.Warnf(\"[kb-release] %s=%q 无法识别 ⇒ 按 off 处理（召回不受版本闸门约束）；可用值：off|shadow|on\", FlagEnv, raw)\n\treturn modeShadow")],
         PKG_LEAF, F_LEAF, {f"{LF}UnknownValueWarns", f"{LF}parseMode"},
         "应保守回 off", "test"),
        ("LF5", "认不出的值不再出声（运维以为闸门开着）", LEAF,
         [("\tlogger.Warnf(\"[kb-release] %s=%q 无法识别 ⇒ 按 off 处理（召回不受版本闸门约束）；可用值：off|shadow|on\", FlagEnv, raw)\n", "")],
         PKG_LEAF, F_LEAF, {f"{LF}UnknownValueWarns"}, "告警行数", "test"),
        ("LF6", "可见性判据少了 NOT（把该显示的当隐藏）", LEAF,
         [("return \"NOT \" + hiddenPredicate", "return hiddenPredicate")],
         PKG_LEAF, F_LEAF, {f"{LF}PredicateShape", f"{LF}WhereVisible"},
         "谓词应以 NOT EXISTS ( 起头", "test"),
        ("LF7", "闸门对所有库生效（丢了 governed 半边）", LEAF,
         [("\t  AND kr.governed\n", "")],
         PKG_LEAF, F_LEAF, {f"{LF}PredicateShape"}, "谓词缺少片段", "test"),
        ("LF8", "退役半边失效（被下线的仍可见）", LEAF,
         [("\t           AND knowledge_chunks.retired_version <= kr.effective_version))",
           "\t           AND knowledge_chunks.retired_version >= 0))")],
         PKG_LEAF, F_LEAF, {f"{LF}PredicateShape"}, "谓词缺少片段", "test"),
        ("LF9", "AndVisible 不带前导 AND（拼进 SQL 就是语法错）", LEAF,
         [("return \" AND \" + p", "return p")],
         PKG_LEAF, F_LEAF, {f"{LF}PredicateShape"}, "AndVisible 应自带前导 AND", "test"),
        ("LF10", "AndVisible 在关闸时返回非空（空串塞进 SQL 尾巴）", LEAF,
         [("\tif p == \"\" {\n\t\treturn \"\"\n\t}\n", "")],
         PKG_LEAF, F_LEAF, {f"{LF}PredicateEmptyUnlessOn"}, "时拼接片段必须为空串", "test"),
        ("LF11", "GateOn 判据变松（shadow 档也拦人）", LEAF,
         [("func GateOn() bool { return modeValue() == modeOn }",
           "func GateOn() bool { return modeValue() != modeOff }")],
         PKG_LEAF, F_LEAF,
         {f"{LF}PredicateEmptyUnlessOn", f"{LF}ModeIsReadPerCall",
          f"{LF}DirectWriteBlockedOffEvenWithRow"},
         "shadow 档 GateOn 必须为假", "test"),
        ("LF12", "ShadowsOrOn 漏掉 shadow（影子期不打版本戳）", LEAF,
         [("return m == modeShadow || m == modeOn", "return m == modeOn")],
         PKG_LEAF, F_LEAF, {f"{LF}ShadowsOrOn", f"{LF}ModeIsReadPerCall"},
         "时 ShadowsOrOn=", "test"),
        ("LF13", "WhereVisible 开闸时不挂谓词（该藏的线上内容全量召回）", LEAF,
         [("\tif p := VisiblePredicate(); p != \"\" {", "\tif p := VisiblePredicate(); p == \"\" {")],
         PKG_LEAF, F_LEAF, {f"{LF}WhereVisible"}, "闸门开着时 SQL 里必须出现可见性判据", "test"),
        ("LF14", "待发布桶分配的幂等键写反（同批导入拿到不同号）", LEAF,
         [("WHERE s.product_id = ? AND s.governed AND s.draft_version = 0",
           "WHERE s.product_id = ? AND s.governed AND s.draft_version <> 0")],
         PKG_REPO, F_REPO,
         {f"{RP}EnsureDraftStampConcurrent", f"{RP}StampForWriteRealRows",
          f"{RP}CountChunksInForce", f"{RP}PublishPublishesDraftBucket"},
         "~kb_releases 分配后 draft_version 仍为 0", "test"),
        ("LF15", "off 档也打版本戳（关闸失效，导入被挡在桶里）", LEAF,
         [("\tif !ShadowsOrOn() {\n\t\treturn nil\n\t}", "\tif !ShadowsOrOn() && false {\n\t\treturn nil\n\t}")],
         PKG_LEAF, F_LEAF, {f"{LF}StampForWriteOffLeavesRowsAlone"}, "off 档打戳不该报错", "test"),
        ("LF16", "未治理库的 0 也被写回版本列（放行口变拦截口）", LEAF,
         [("\t\tif v <= 0 {\n\t\t\tcontinue\n\t\t}", "\t\tif v < 0 {\n\t\t\tcontinue\n\t\t}")],
         PKG_REPO, F_REPO, {f"{RP}StampForWriteRealRows"}, "未治理库的已编号行要原样不动", "test"),
        ("LF17", "影子读数在无货时也出声（0 条会被隐藏也报）", LEAF,
         [("\tif n > 0 {", "\tif n >= 0 {")],
         PKG_KSVC, F_KSVC, {f"{KG}ShadowSilentWhenNothingWouldHide"}, "读数为 0 却出现", "test"),
        ("LF18", "影子统计在 off 档也跑（关闸那次白付一次查询）", LEAF,
         [("\tif modeValue() != modeShadow || len(ids) == 0 {", "\tif len(ids) == 0 {")],
         PKG_KSVC, F_KSVC, {f"{KG}ShadowLogsWouldHide"}, "档不该出 shadow 声", "test"),
        ("LF19", "就地改写的 off 守卫失效（没开闸也拦编辑入口）", LEAF,
         [("\tif db == nil || productID == \"\" || !GateOn() {",
           "\tif db == nil || productID == \"\" {")],
         PKG_LEAF, F_LEAF, {f"{LF}DirectWriteBlockedOffEvenWithRow"}, "时不该拒也不该报错", "test"),
        ("LF20", "拒绝文案不再指路（运营拿到一句没有出口的错）", LEAF,
         [('var ErrGovernedDirectWrite = errors.New("该知识库已启用发布制：请通过知识变更流程提交（新增/修订/下线），由审批与发布使其生效")',
           'var ErrGovernedDirectWrite = errors.New("禁止写入")')],
         PKG_LEAF, F_LEAF, {f"{LF}ErrGovernedDirectWriteIsStandalone"}, "拒绝文案缺少", "test"),

        # ---------------- repository：真表才证得出的事 ----------------
        ("RP1", "裸 SQL 建行顺带开启治理（看一眼就把库推进发布制）", REPO,
         [("VALUES (?, false, 0, 0, 0, 0, 0, ?, now(), now())", "VALUES (?, true, 0, 0, 0, 0, 0, ?, now(), now())")],
         # 连带面（20260928-171053 实测四条全红，逐条都是"governed 被建行那一刀翻成 true"的真后果，
         # 不是注码打偏）：LockReleaseEnsuresRow 直读那一列；SetGovernedIdempotent 的首次启用变成
         # 幂等空转（痕 0 条）；PublishJudgments 的"只建行、没进发布制要拒"那一格不再拒；
         # StampForWriteRealRows 里当"未治理库"用的那个 product 被建成了治理中（打戳真写了 1）。
         PKG_REPO, F_REPO, {f"{RP}LockReleaseEnsuresRow", f"{RP}PublishJudgments",
                             f"{RP}SetGovernedIdempotent", f"{RP}StampForWriteRealRows"},
         "governed=false", "test"),
        ("RP2", "建行幂等键换列（同一个库建出两行）", REPO,
         [("ON CONFLICT (product_id) DO NOTHING", "ON CONFLICT (id) DO NOTHING")],
         PKG_REPO, F_REPO,
         # 这一处坏到全包：kb_releases 那一行是所有读写的入口行，实测 11 条一起红。
         # 判"哪一条才是本格的证人"没有意义，红集合按实测登记（名单只含本卡前缀，不会漂）。
         {f"{RP}LockReleaseEnsuresRow", f"{RP}CountChunksInForce", f"{RP}PointerMovesRefuseCleanRows",
          f"{RP}PublishAddStampsAndAdvances", f"{RP}PublishJudgments", f"{RP}PublishPublishesDraftBucket",
          f"{RP}PublishRejectsBadChanges", f"{RP}PublishRolledBackWhenChangeNotPending",
          f"{RP}ReviseAndRetireShape", f"{RP}RollbackTouchesNoCorpus", f"{RP}SetGovernedIdempotent"},
         "~建行失败|~idx_kb_releases_product_id", "test"),
        ("RP3", "锁脱离事务也能用（FOR UPDATE 立刻释放）", REPO,
         [("\tif tx == nil {\n\t\treturn nil, errors.New(\"kb_release repository: LockRelease 必须在事务内调用\")",
           "\tif tx == nil {\n\t\ttx = r.db")],
         PKG_REPO, F_REPO, {f"{RP}LockReleaseEnsuresRow"}, "tx=nil 必须报错", "test"),
        ("RP4", "查无此行不再报 NotFound（零值行冒充记录）", REPO,
         [("\tif err != nil {\n\t\treturn nil, err\n\t}\n\tif rel.ID == 0 {",
           "\tif err != nil {\n\t\treturn nil, err\n\t}\n\tif rel.ID < 0 {")],
         PKG_REPO, F_REPO, {f"{RP}LockReleaseEnsuresRow"}, "没有行时必须报", "test"),
        ("RP5", "幂等启停也空转一次写（changed_by/updated_at 被没改动的点击盖掉）", REPO,
         [("\t\tif before == governed {", "\t\tif before == governed && false {")],
         PKG_REPO, F_REPO, {f"{RP}SetGovernedIdempotent"}, "重复启用不该改写行", "test"),
        # 与 RP5 分层：这一格砍的是事务**外**那句，坏法是"审计里同一次启用记了三次"。
        ("RP35", "没真的改变也留痕（重复启用在审计里记成启用两次）", REPO,
         [("\tif before != governed {", "\tif before == governed {")],
         PKG_REPO, F_REPO, {f"{RP}SetGovernedIdempotent"}, "三次启用只该留一条痕", "test"),
        ("RP6", "留痕的改前改后写反", REPO,
         [("\t\t\tOldValue:   fmt.Sprintf(\"%t\", before),", "\t\t\tOldValue:   fmt.Sprintf(\"%t\", governed),")],
         PKG_REPO, F_REPO, {f"{RP}SetGovernedIdempotent"}, "留痕的改前改后写反或缺项", "test"),
        ("RP7", "受治理名单把所有库都列进去", REPO,
         [("\t\tWhere(\"governed\").\n", "")],
         PKG_REPO, F_REPO, {f"{RP}SetGovernedIdempotent"}, "ListReleases 只该给出治理中的库", "test"),
        ("RP8", "查无此条变成错误（404 与 500 分不开）", REPO,
         [("\tif errors.Is(err, gorm.ErrRecordNotFound) {\n\t\treturn nil, nil\n\t}",
           "\tif errors.Is(err, gorm.ErrRecordNotFound) {\n\t\treturn nil, err\n\t}")],
         PKG_REPO, F_REPO, {f"{RP}ChangeViews"}, "查无此条要是 (nil,nil)", "test"),
        ("RP9", "变更列表的页长夹住失效（一次拉全表）", REPO,
         [("\tlimit := f.Limit\n\tif limit <= 0 || limit > 200 {\n\t\tlimit = 50\n\t}\n\tvar rows []model.KBChangeRequest",
           "\tlimit := f.Limit\n\tif limit < 0 || limit > 200000 {\n\t\tlimit = 50\n\t}\n\tvar rows []model.KBChangeRequest")],
         PKG_REPO, F_REPO, {f"{RP}ClampsAreExact", f"{RP}ChangeViews"}, "上界外侧 201 夹到默认 50", "test"),
        ("RP10", "留痕读取的页长夹住失效", REPO,
         [("\tif limit <= 0 || limit > 200 {\n\t\tlimit = 50\n\t}\n\tvar rows []model.KBChangeAuditLog",
           "\tif limit < 0 || limit > 200000 {\n\t\tlimit = 50\n\t}\n\tvar rows []model.KBChangeAuditLog")],
         PKG_REPO, F_REPO, {f"{RP}ClampsAreExact"}, "ListAuditBySubject(limit=", "test"),
        ("RP11", "待办清单把已落地的也算进去", REPO,
         [("\t\tWhere(\"product_id = ? AND status = ?\", productID, model.KBChangeStatusPending).",
           "\t\tWhere(\"product_id = ?\", productID).")],
         PKG_REPO, F_REPO, {f"{RP}WithdrawCAS"}, "撤回后仍出现在待办里", "test"),
        ("RP12", "撤回不带 CAS 条件（已发布的能被改回待处理）", REPO,
         [("\t\tWhere(\"id = ? AND status = ?\", id, model.KBChangeStatusPending).",
           "\t\tWhere(\"id = ?\", id).")],
         PKG_REPO, F_REPO, {f"{RP}WithdrawCAS"}, "重复撤回要 CAS 拒掉", "test"),
        ("RP13", "发布不查治理开关（未进发布制的库也能发）", REPO,
         [("\t\tif !rel.Governed {", "\t\tif !rel.Governed && false {")],
         PKG_REPO, F_REPO, {f"{RP}PublishJudgments"}, "@该库没有待发布内容", "test"),
        ("RP14", "跨号发布禁令撤掉（回滚掉的语料被静默放回）", REPO,
         [("\t\tif rel.RecalledVersion > rel.EffectiveVersion {",
           "\t\tif rel.RecalledVersion > rel.EffectiveVersion && false {")],
         PKG_REPO, F_REPO, {f"{RP}PublishJudgments", f"{RP}RollbackTouchesNoCorpus"},
         "禁令没开火", "test"),
        ("RP15", "桶号可以等于在服号（绕过审批直接上线）", REPO,
         [("\t\tif bucket <= rel.EffectiveVersion {", "\t\tif bucket < rel.EffectiveVersion - 1000 {")],
         PKG_REPO, F_REPO, {f"{RP}PublishJudgments"}, "不高于在服版本", "test"),
        ("RP16", "空批也走一次发布（审计里多出不存在的一次发布）", REPO,
         [("\t\tif len(in.Changes) == 0 && rel.DraftVersion == 0 {",
           "\t\tif len(in.Changes) == 0 && rel.DraftVersion == 0 && false {")],
         PKG_REPO, F_REPO, {f"{RP}PublishJudgments"}, "空移指针会留下一次不存在的发布", "test"),
        ("RP17", "add 可以没有内容", REPO,
         [("\t\t\t\tif c.Content == \"\" {\n\t\t\t\t\treturn fmt.Errorf(\"变更 %s：add 必须有内容\", c.ID)",
           "\t\t\t\tif c.Content == \"\\x00never\" {\n\t\t\t\t\treturn fmt.Errorf(\"变更 %s：add 必须有内容\", c.ID)")],
         # 令牌取自真实红因（`要的错误 "必须有内容" 没出现，实得 …`）：摘掉内容判据后
         # 同一批变更会撞在更后面的"变更不在待处理态"上，红因不是"内容"两个字。
         PKG_REPO, F_REPO, {f"{RP}PublishRejectsBadChanges"}, "没出现，实得", "test"),
        ("RP18", "修订已退役的分段也放行", REPO,
         [("\t\t\t\tif old.RetiredVersion != 0 {", "\t\t\t\tif old.RetiredVersion < 0 {")],
         PKG_REPO, F_REPO, {f"{RP}PublishRejectsBadChanges"}, "已退役目标要拒", "test"),
        ("RP19", "修订可以指向别的库", REPO,
         [("\t\t\t\tif old.ProductID != c.ProductID {\n\t\t\t\t\treturn fmt.Errorf(\"%w：chunk %d 属于库 %s，与本变更的库 %s 不符\",\n\t\t\t\t\t\tErrKBReleaseTargetConflict, c.TargetChunkID, old.ProductID, c.ProductID)\n\t\t\t\t}\n\t\t\t\tdocID := c.DocumentID",
           "\t\t\t\tdocID := c.DocumentID")],
         PKG_REPO, F_REPO, {f"{RP}PublishRejectsBadChanges"}, "与本变更的库|没出现", "test"),
        ("RP20", "下线可以指向别的库", REPO,
         [("\t\t\t\tif old.ProductID != c.ProductID {\n\t\t\t\t\treturn fmt.Errorf(\"%w：chunk %d 属于库 %s，与本变更的库 %s 不符\",\n\t\t\t\t\t\tErrKBReleaseTargetConflict, c.TargetChunkID, old.ProductID, c.ProductID)\n\t\t\t\t}\n\t\t\t\tif err := r.retireChunk",
           "\t\t\t\tif err := r.retireChunk")],
         PKG_REPO, F_REPO, {f"{RP}PublishRejectsBadChanges"}, "与本变更的库|没出现", "test"),
        ("RP21", "修订的目标行不存在被静默跳过（AC① 的反面）", REPO,
         [("\t\t\t\tif old == nil {\n\t\t\t\t\treturn fmt.Errorf(\"%w：要修订的 chunk %d 不存在\", ErrKBReleaseTargetConflict, c.TargetChunkID)\n\t\t\t\t}",
           "\t\t\t\tif old == nil {\n\t\t\t\t\tcontinue\n\t\t\t\t}")],
         PKG_REPO, F_REPO, {f"{RP}PublishRejectsBadChanges"}, "判据拒了却仍移了指针", "test"),
        ("RP22", "重复下线同一条被静默放过", REPO,
         [("\tif res.RowsAffected == 0 {\n\t\treturn fmt.Errorf(\"chunk %d 不存在或已被退役，无法再次下线\", chunkID)",
           "\tif res.RowsAffected < 0 {\n\t\treturn fmt.Errorf(\"chunk %d 不存在或已被退役，无法再次下线\", chunkID)")],
         PKG_REPO, F_REPO, {f"{RP}ReviseAndRetireShape"}, "第二次下线同一条要报错", "test"),
        ("RP23", "修订后的新行落在别的分段号上（上下文邻接断了）", REPO,
         [("newID, err := r.insertChunk(ctx, tx, c, docID, bucket, old.ChunkIndex)",
           "newID, err := r.insertChunk(ctx, tx, c, docID, bucket, old.ChunkIndex+1000)")],
         PKG_REPO, F_REPO, {f"{RP}ReviseAndRetireShape"}, "修订新行要落在原位并带生效号", "test"),
        ("RP24", "修订的文档归属不继承老行", REPO,
         [("\t\t\t\tdocID := c.DocumentID\n\t\t\t\tif docID == 0 {\n\t\t\t\t\tdocID = old.DocumentID\n\t\t\t\t}",
           "\t\t\t\tdocID := c.DocumentID")],
         # 令牌指向我这轮补的那条腿（变更不带归属、老行在另一个文档）；上一版拿"文档归属要
         # 从被替换那行继承"这句要红，可那句的夹具自带归属，删掉继承分支也照样绿。
         PKG_REPO, F_REPO, {f"{RP}ReviseAndRetireShape"}, "不带归属的修订要落到被替换那行的文档与号位上", "test"),
        ("RP25", "版本戳没真的写进语料行", REPO,
         [("\t\tlen([]rune(c.Content)), version, c.ID,", "\t\tlen([]rune(c.Content)), 0, c.ID,")],
         # ReviseAndRetireShape 也在这条通路上（修订写的是同一条 INSERT 的同一个参数位：
         # 20260928-171053 实测它红在 :727"修订新行要落在原位并带生效号"），不是连带意外。
         PKG_REPO, F_REPO, {f"{RP}PublishAddStampsAndAdvances", f"{RP}ReviseAndRetireShape"},
         "版本戳错", "test"),
        ("RP26", "发布状态回写不带 CAS（撤回与发布抢同一行）", REPO,
         [("\t\t\t\tWhere(\"id = ? AND status = ?\", c.ID, model.KBChangeStatusPending).",
           "\t\t\t\tWhere(\"id = ?\", c.ID).")],
         PKG_REPO, F_REPO, {f"{RP}PublishRolledBackWhenChangeNotPending"}, "要的是发布回滚", "test"),
        ("RP27", "同一次发布里两条 add 抢同一个分段号", REPO,
         [("\tif n, ok := nextIdx[docID]; ok {\n\t\tnextIdx[docID] = n + 1",
           "\tif n, ok := nextIdx[docID]; ok {\n\t\tnextIdx[docID] = n")],
         PKG_REPO, F_REPO, {f"{RP}PublishAddStampsAndAdvances"}, "分段号要互不相同", "test"),
        ("RP28", "回滚顺手改写了语料的版本戳（AC② 破了）", REPO,
         [("\t\tif err := tx.WithContext(ctx).Table(\"kb_releases\").\n\t\t\tWhere(\"id = ?\", rel.ID).\n\t\t\tUpdateColumns(map[string]any{\n\t\t\t\t\"effective_version\": to,",
           "\t\tif err := tx.WithContext(ctx).Table(\"knowledge_chunks\").Where(\"product_id = ?\", productID).UpdateColumn(\"kb_version\", 999).Error; err != nil {\n\t\t\treturn err\n\t\t}\n\t\tif err := tx.WithContext(ctx).Table(\"kb_releases\").\n\t\t\tWhere(\"id = ?\", rel.ID).\n\t\t\tUpdateColumns(map[string]any{\n\t\t\t\t\"effective_version\": to,")],
         PKG_REPO, F_REPO, {f"{RP}RollbackTouchesNoCorpus"},
         "AC② 破了", "test"),
        ("RP29", "第二次回滚被允许（穿到更老的历史）", REPO,
         [("\t\t\tif rel.PreviousVersion <= 0 || rel.PreviousVersion >= rel.EffectiveVersion {",
           "\t\t\tif rel.PreviousVersion <= 0 || rel.PreviousVersion >= rel.EffectiveVersion && false {")],
         PKG_REPO, F_REPO, {f"{RP}RollbackTouchesNoCorpus"},
         "第二次回滚要拒", "test"),
        ("RP30", "没有撤下号也能放回（凭空造一版）", REPO,
         [("\t\t\tif rel.RecalledVersion <= rel.EffectiveVersion {",
           "\t\t\tif rel.RecalledVersion <= rel.EffectiveVersion && false {")],
         PKG_REPO, F_REPO, {f"{RP}PointerMovesRefuseCleanRows"},
         "没回滚过就该答", "test"),
        ("RP31", "回滚与放回写同一种留痕（审计读不出方向）", REPO,
         [("\t\t\taction = model.KBAuditRestored", "\t\t\taction = model.KBAuditRollback")],
         PKG_REPO, F_REPO, {f"{RP}RollbackTouchesNoCorpus"}, "放回留痕", "test"),
        ("RP32", "在服数不看闸门（页面说有待发布、实际搜不到）", REPO,
         [("\tif pred := kbrelease.VisiblePredicate(); pred != \"\" {\n\t\tq = q.Where(pred)",
           "\tif pred := kbrelease.VisiblePredicate(); pred != \"\" && false {\n\t\tq = q.Where(pred)")],
         PKG_REPO, F_REPO, {f"{RP}CountChunksInForce", f"{RP}PublishPublishesDraftBucket"},
         "未发布前它不该在服", "test"),
        ("RP33", "字段不全的留痕行被收下（AC③ 少一个出处）", REPO,
         [("\tif log.SubjectKey == \"\" || log.Action == \"\" || log.Actor == \"\" {",
           "\tif log.SubjectKey == \"\" && false {")],
         PKG_REPO, F_REPO, {f"{RP}AuditShape"}, "字段不全的审计行要拒", "test"),
        ("RP34", "发布留痕的 subject_key 丢了前缀（按号查不到全部动作）", REPO,
         [("\t\t\t\tSubjectKey: \"change:\" + c.ID,", "\t\t\t\tSubjectKey: c.ID,")],
         PKG_REPO, F_REPO, {f"{RP}PublishAddStampsAndAdvances"}, "的发布留痕", "test"),

        # ---------------- service：资格判定与留痕 ----------------
        ("SV1", "底座可用性不看审批服务（能提交但永远没人能批）", SVC,
         [("return s != nil && s.store != nil && s.store.Available() && s.approvals != nil",
           "return s != nil && s.store != nil && s.store.Available()")],
         PKG_SVC, F_SVC, {f"{SV}UnavailableEverywhere"}, "不该是可用", "test"),
        ("SV2", "product_id 不再去空白（带空格的库号各记一套账）", SVC,
         [("\tout.ProductID = strings.TrimSpace(out.ProductID)\n", "")],
         PKG_SVC, F_SVC, {f"{SV}SubmitInputNormalization"}, "去空白/归一未做全", "test"),
        ("SV3", "op 不再归一小写（ADD 被判成非法动作）", SVC,
         [("out.Op = strings.ToLower(strings.TrimSpace(out.Op))", "out.Op = strings.TrimSpace(out.Op)")],
         PKG_SVC, F_SVC, {f"{SV}SubmitInputNormalization"}, "合法入参不该报错", "test"),
        ("SV4", "变更可以不写理由（AC③ 的落点被抹掉）", SVC,
         [("\tif out.Reason == \"\" {", "\tif out.Reason == \"\\x00never\" {")],
         PKG_SVC, F_SVC, {f"{SV}SubmitInputNormalization"}, "应报错而没有", "test"),
        ("SV5", "动作值域闸失效（未知 op 也能提交）", SVC,
         [("\tif !model.IsValidKBChangeOp(out.Op) {", "\tif !model.IsValidKBChangeOp(out.Op) && false {")],
         PKG_SVC, F_SVC, {f"{SV}SubmitInputNormalization"}, "应报错而没有", "test"),
        ("SV6", "add 也能带 target_chunk_id", SVC,
         [("\t\tif out.TargetChunkID != 0 {", "\t\tif out.TargetChunkID < 0 {")],
         PKG_SVC, F_SVC, {f"{SV}SubmitInputNormalization"}, "应报错而没有", "test"),
        ("SV7", "retire 也能带 content（下线顺手改了正文）", SVC,
         [("\t\tif out.Content != \"\" {", "\t\tif out.Content != \"\" && false {")],
         PKG_SVC, F_SVC, {f"{SV}SubmitInputNormalization"}, "应报错而没有", "test"),
        ("SV8", "revise 不指定被替换的分段也能提交", SVC,
         [("\t\tif out.TargetChunkID == 0 {\n\t\t\treturn out, fmt.Errorf(\"%w: revise 必须指定被替换的 target_chunk_id\",",
           "\t\tif out.TargetChunkID < 0 {\n\t\t\treturn out, fmt.Errorf(\"%w: revise 必须指定被替换的 target_chunk_id\",")],
         PKG_SVC, F_SVC, {f"{SV}SubmitInputNormalization"}, "应报错而没有", "test"),
        ("SV9", "变更号超列宽被截断使用", SVC,
         [("\tif len(id) > kbChangeIDMaxLen {", "\tif len(id) > kbChangeIDMaxLen*100 {")],
         PKG_SVC, F_SVC, {f"{SV}ChangeIDErrsOnOverflow"}, "超长变更号应报错", "test"),
        ("SV10", "审批入队的三列写错（结论挂到别的对象上）", SVC,
         [("\t\tSubjectType: KBChangeApprovalSubjectType,", "\t\tSubjectType: \"kb_change_request\",")],
         PKG_SVC, F_SVC, {f"{SV}SubmitChangeHappyPath"}, "审批命名键错", "test"),
        ("SV11", "审批回空记录也当成功（留下没有结论的变更行）", SVC,
         [("\t\treturn nil, fmt.Errorf(\"%w: 变更 %s 的审批入队返回空记录\", ErrKBReleaseApprovalMissing, id)",
           "\t\tappr = &model.ApprovalRequest{ID: \"apr_synth\"}")],
         PKG_SVC, F_SVC, {f"{SV}SubmitChangeNilApprovalWithoutError"}, "应判 ErrKBReleaseApprovalMissing", "test"),
        ("SV12", "变更行落库时状态初值写成 applied（还没发布就算已生效）", SVC,
         [("\t\tStatus:        model.KBChangeStatusPending,", "\t\tStatus:        model.KBChangeStatusApplied,")],
         PKG_SVC, F_SVC, {f"{SV}SubmitChangeHappyPath"}, "变更行形状错", "test"),
        ("SV13", "submitted 留痕不写了（AC③ 少一条）", SVC,
         [("\t\tAction:     model.KBAuditSubmitted,", "\t\tAction:     model.KBAuditPublished,")],
         PKG_SVC, F_SVC, {f"{SV}SubmitChangeHappyPath"}, "submitted 留痕缺失或形状错", "test"),
        ("SV14", "跃迁表被绕过（applied 也能直接撤回）", SVC,
         [("\tif !model.KBChangeTransitionAllowed(ch.Status, model.KBChangeStatusWithdrawn) {",
           "\tif !model.KBChangeTransitionAllowed(ch.Status, model.KBChangeStatusWithdrawn) && false {")],
         PKG_SVC, F_SVC, {f"{SV}WithdrawChange"}, "报错要给出下一步", "test"),
        ("SV15", "CAS 落败被当成成功（与发布抢同一行时静默覆盖）", SVC,
         [("\tif !ok {\n\t\t// 上面刚判过能撤", "\tif !ok && false {\n\t\t// 上面刚判过能撤")],
         PKG_SVC, F_SVC, {f"{SV}WithdrawChange"}, "CAS 没中应报撤不了", "test"),
        ("SV16", "撤回不记留痕（谁按的查不出来）", SVC,
         [("\t\tAction:     model.KBAuditWithdrawn,", "\t\tAction:     model.KBAuditSubmitted,")],
         PKG_SVC, F_SVC, {f"{SV}WithdrawChange"}, "留痕", "test"),
        ("SV17", "发布不先问治理（你没开门被说成屋里是空的）", SVC,
         [("\tif rel == nil || !rel.Governed {", "\tif rel == nil {")],
         PKG_SVC, F_SVC, {f"{SV}PublishPendingRequiresGoverned"}, "期望未进发布制", "test"),
        ("SV18", "读指针的故障被归成没有发布行", SVC,
         [("\trel, err := s.store.GetRelease(ctx, productID)\n\tif errors.Is(err, repository.ErrKBReleaseNotFound) {",
           "\trel, err := s.store.GetRelease(ctx, productID)\n\tif errors.Is(err, repository.ErrKBReleaseNotGoverned) {")],
         PKG_SVC, F_SVC, {f"{SV}PublishPendingArgsAndStoreErrors"}, "没有发布行该按未进发布制回答", "test"),
        ("SV19", "审批三列比对少一列（给别的对象开门）", SVC,
         [("\t\tif appr.SubjectType != KBChangeApprovalSubjectType || appr.SubjectID != ch.ID ||\n\t\t\tappr.PolicyKey != KBChangeApprovalPolicyKey {",
           "\t\tif appr.SubjectType != KBChangeApprovalSubjectType || appr.SubjectID != ch.ID {")],
         PKG_SVC, F_SVC, {f"{SV}PublishPendingVerdicts"}, "unknown 分账缺项", "test"),
        ("SV20", "没批的也算进本次发布（AC① 正面破口）", SVC,
         [("\t\tcase model.ApprovalStatusApproved:", "\t\tcase model.ApprovalStatusApproved, model.ApprovalStatusPending:"),
          ("\t\tcase model.ApprovalStatusPending:\n\t\t\toutcome.SkippedPending = append(outcome.SkippedPending, ch.ID)\n", "")],
         PKG_SVC, F_SVC, {f"{SV}PublishPendingVerdicts", f"{SV}PublishPendingNothingApprovedSkipsTransaction"},
         "included 应恰好是", "test"),
        ("SV21", "认不出的审批状态不再保守（不当成不明）", SVC,
         [("\t\tdefault:\n\t\t\t// 未知状态一律不当\"批了\"（口径同 quote_send 的 verdict unknown ⇒ 不发）",
           "\t\tcase \"never\":\n\t\t\t// 未知状态一律不当\"批了\"（口径同 quote_send 的 verdict unknown ⇒ 不发）")],
         PKG_SVC, F_SVC, {f"{SV}PublishPendingVerdicts"}, "skipped_expired=", "test"),
        ("SV22", "读不到结论时继续发布（把故障说成没批）", SVC,
         [("\t\tif gerr != nil {", "\t\tif gerr != nil && false {")],
         PKG_SVC, F_SVC, {f"{SV}PublishPendingApprovalReadFailureStopsEverything"},
         "读结论失败必须整次发布停下", "test"),
        ("SV23", "一条都没批也进事务（留下一次空发布的痕）", SVC,
         [("\tif len(intent.Changes) == 0 && rel.DraftVersion == 0 {",
           "\tif len(intent.Changes) == 0 && rel.DraftVersion == 0 && false {")],
         PKG_SVC, F_SVC, {f"{SV}PublishPendingNothingApprovedSkipsTransaction"},
         "一条都没批：无结果、无错误", "test"),
        ("SV24", "有桶却没有变更时不再真发布（导入的内容永远上不了线）", SVC,
         [("\tif len(intent.Changes) == 0 && rel.DraftVersion == 0 {",
           "\tif len(intent.Changes) == 0 {")],
         PKG_SVC, F_SVC, {f"{SV}PublishPendingDraftBucketWithoutChanges"},
         "有桶就得进事务把指针推过去", "test"),
        ("SV25", "retire 那条也去补向量（空内容占一次调用）", SVC,
         [("\t\tif ap.AppliedChunkID == 0 || ap.Content == \"\" {", "\t\tif ap.Content == \"\" {")],
         PKG_SVC, F_SVC, {f"{SV}EmbedAppliedAfterCommit"}, "补算入参错", "test"),
        ("SV26", "没有新行也调用向量通路", SVC,
         [("\tif len(chunks) == 0 {", "\tif len(chunks) < 0 {")],
         PKG_SVC, F_SVC, {f"{SV}EmbedAppliedAfterCommit"}, "没有新行时不该调用向量通路", "test"),
        ("SV27", "回滚与放回走同一条仓储通路（方向串了）", SVC,
         [("\tif forward {\n\t\treturn s.store.Restore(ctx, productID, actor)\n\t}\n\treturn s.store.Rollback(ctx, productID, actor)",
           "\tif forward {\n\t\treturn s.store.Rollback(ctx, productID, actor)\n\t}\n\treturn s.store.Restore(ctx, productID, actor)")],
         PKG_SVC, F_SVC, {f"{SV}PointerMovesGoToStore"}, "Rollback=", "test"),
        ("SV28", "发布/回滚不校验库号列宽（一句数据库报错）", SVC,
         [("\tif len(productID) > kbChangeProductMaxLen {\n\t\treturn \"\", \"\", fmt.Errorf(\"%w: product_id 长 %d 超列宽 %d\",",
           "\tif len(productID) > kbChangeProductMaxLen * 100 {\n\t\treturn \"\", \"\", fmt.Errorf(\"%w: product_id 长 %d 超列宽 %d\",")],
         PKG_SVC, F_SVC, {f"{SV}WriteArgs", f"{SV}PointerMovesGoToStore"}, "超列宽库号该在入参层挡住", "test"),
        ("SV29", "留痕可以不给操作者（追责没有出处）", SVC,
         [("\tif actor == \"\" {", "\tif actor == \"\\x00never\" {")],
         PKG_SVC, F_SVC, {f"{SV}WriteArgs", f"{SV}PublishPendingArgsAndStoreErrors", f"{SV}SetGoverned"},
         "空操作者应判入参", "test"),
        ("SV30", "没有发布行时回 404 而不是未启用（前端读成库不存在）", SVC,
         [("\tif errors.Is(err, repository.ErrKBReleaseNotFound) {\n\t\treturn nil, nil\n\t}",
           "\tif errors.Is(err, repository.ErrKBReleaseNotFound) {\n\t\treturn nil, err\n\t}")],
         PKG_SVC, F_SVC, {f"{SV}Views"}, "没有发布行该回 (nil,nil)", "test"),
        ("SV31", "统计视图把影子读数当成真拦读数", SVC,
         [("\tShadowStats: mode != \"on\"", "\tShadowStats: mode == \"on\"")],
         PKG_SVC, F_SVC, {f"{SV}StatsFlagsShadowReadings"}, "读数应为", "test"),
        ("SV32", "留痕读口接受空 subject_key（整张事件流倒给用户）", SVC,
         [("\tif strings.TrimSpace(subjectKey) == \"\" {", "\tif strings.TrimSpace(subjectKey) == \"\\x00never\" {")],
         PKG_SVC, F_SVC, {f"{SV}Views"}, "空 subject_key 应判入参", "test"),
        ("SV33", "全局登记处不清空（撤掉装配后端点仍回 200）", SVC,
         [("func SetGlobalKBReleaseService(s *KBReleaseService) { globalKBReleaseSvc.Store(s) }",
           "func SetGlobalKBReleaseService(s *KBReleaseService) { if s != nil { globalKBReleaseSvc.Store(s) } }")],
         PKG_SVC, F_SVC, {f"{SV}GlobalRegistryRoundTrip"}, "传 nil 应撤掉登记", "test"),

        # ---------------- controller：状态码、值域闸、响应形状 ----------------
        ("CT1", "底座不可用回 500（运维该报装配却被领着改请求）", CTRL,
         [("response.Error(ctx, http.StatusServiceUnavailable,\n\t\t\"知识库变更底座不可用（未装配或缺少 DB 句柄），本次未读到任何发布状态\")",
           "response.Error(ctx, http.StatusInternalServerError,\n\t\t\"知识库变更底座不可用（未装配或缺少 DB 句柄），本次未读到任何发布状态\")")],
         PKG_CTRL, F_CTRL, {f"{CT}UnavailableEveryExit"}, "期望 503", "test"),
        ("CT2", "提交不带身份也放行（留痕里是一句空串的谁改的）", CTRL,
         [("\t\tresponse.Error(ctx, http.StatusUnauthorized, \"提交变更必须带操作者身份（未登录或会话里没有 user id）\")\n\t\treturn",
           "\t\toperator = \"anonymous\"")],
         PKG_CTRL, F_CTRL, {f"{CT}WriteRequiresOperator"}, "~期望 401，实得 200", "test"),
        ("CT3", "发布口的 401 写成 403", CTRL,
         [("response.Error(ctx, http.StatusUnauthorized, \"发布必须带操作者身份（留痕里的 changed_by 是追责依据）\")",
           "response.Error(ctx, http.StatusForbidden, \"发布必须带操作者身份（留痕里的 changed_by 是追责依据）\")")],
         PKG_CTRL, F_CTRL, {f"{CT}WriteRequiresOperator"}, "期望 401", "test"),
        ("CT4", "状态筛选器的值域闸失效（打错字母被说成没有这种变更）", CTRL,
         [("\tif status != \"\" && !model.IsValidKBChangeStatus(status) {",
           "\tif status != \"\" && !model.IsValidKBChangeStatus(status) && false {")],
         PKG_CTRL, F_CTRL, {f"{CT}ListQueryTranslation"}, "~期望 400，实得 200", "test"),
        ("CT5", "动作筛选器的值域闸失效", CTRL,
         [("\tif op != \"\" && !model.IsValidKBChangeOp(op) {",
           "\tif op != \"\" && !model.IsValidKBChangeOp(op) && false {")],
         PKG_CTRL, F_CTRL, {f"{CT}ListQueryTranslation"}, "~期望 400，实得 200", "test"),
        ("CT6", "查询里的库号长度闸失效", CTRL,
         [("\tif len(productID) > service.KBReleaseProductMaxLen {",
           "\tif len(productID) > service.KBReleaseProductMaxLen * 100 {")],
         PKG_CTRL, F_CTRL, {f"{CT}ListQueryTranslation"}, "~期望 400，实得 200", "test"),
        ("CT7", "page=0 被静默纠正成第一页", CTRL,
         [("\t\tif err != nil || n < 1 {\n\t\t\tresponse.Error(ctx, http.StatusBadRequest, \"page 必须是从 1 起的整数\")",
           "\t\tif err != nil || n < -1 {\n\t\t\tresponse.Error(ctx, http.StatusBadRequest, \"page 必须是从 1 起的整数\")")],
         PKG_CTRL, F_CTRL, {f"{CT}ListQueryTranslation"}, "期望 400，实得", "test"),
        ("CT8", "page_size 非法被静默纠正", CTRL,
         [("\t\tif err != nil || n < 1 {\n\t\t\tresponse.Error(ctx, http.StatusBadRequest, \"page_size 必须是正整数\")",
           "\t\tif err != nil || n < -1 {\n\t\t\tresponse.Error(ctx, http.StatusBadRequest, \"page_size 必须是正整数\")")],
         PKG_CTRL, F_CTRL, {f"{CT}ListQueryTranslation"}, "期望 400，实得", "test"),
        ("CT9", "路径参数长度闸失效（免费的慢查询）", CTRL,
         [("const kbChangeIDParamMaxLen = 64", "const kbChangeIDParamMaxLen = 1 << 30")],
         PKG_CTRL, F_CTRL, {f"{CT}ParamGates"}, "~期望 400，实得 500", "test"),
        ("CT10", "库号闸按字节数而不是字数（64 个汉字被拒）", CTRL,
         [("\tif utf8.RuneCountInString(product) > service.KBReleaseProductMaxLen {",
           "\tif len(product) > service.KBReleaseProductMaxLen {")],
         PKG_CTRL, F_CTRL, {f"{CT}ProductParamCountsRunes"}, "个汉字", "test"),
        ("CT11", "留痕的 limit 非法被静默纠正", CTRL,
         [("\t\tif err != nil || n < 1 {\n\t\t\tresponse.Error(ctx, http.StatusBadRequest, \"limit 必须是正整数\")",
           "\t\tif err != nil || n < -1 {\n\t\t\tresponse.Error(ctx, http.StatusBadRequest, \"limit 必须是正整数\")")],
         PKG_CTRL, F_CTRL, {f"{CT}ParamGates"}, "~期望 400，实得 500", "test"),
        ("CT12", "变更列表空结果回 null（前端得先判类型）", CTRL,
         [("\t\tlist = []model.KBChangeRequest{}", "\t\tlist = nil")],
         PKG_CTRL, F_CTRL, {f"{CT}ListQueryTranslation"}, "data.list 该是空数组", "test"),
        ("CT13", "留痕列表空结果回 null", CTRL,
         [("\t\trows = []model.KBChangeAuditLog{}", "\t\trows = nil")],
         PKG_CTRL, F_CTRL, {f"{CT}AuditSubjectKeys"}, "无留痕时 data.list|data.list 该是空数组", "test"),
        ("CT14", "受治理名单空结果回 null", CTRL,
         [("\t\trows = []model.KBRelease{}\n", "")],
         PKG_CTRL, F_CTRL, {f"{CT}ListReleases"}, "零行时 data 该是空数组", "test"),
        ("CT15", "发布行缺席时回 404（读成库不存在）", CTRL,
         [("response.Success(ctx, gin.H{\"product_id\": product, \"exists\": false, \"governed\": false}, \"ok\")",
           "response.Error(ctx, http.StatusNotFound, \"发布记录不存在\")")],
         PKG_CTRL, F_CTRL, {f"{CT}GetReleaseAbsent", f"{CT}RealStack"}, "~期望 200，实得 404", "test"),
        ("CT16", "启停治理漏字段时当成启用（静默扩大治理范围）", CTRL,
         [("\t\tresponse.Error(ctx, http.StatusBadRequest, `请求体缺少 governed 布尔字段（形如 {\"governed\":true}）`)\n\t\treturn",
           "\t\tbody.Governed = new(bool)\n\t\t*body.Governed = true")],
         PKG_CTRL, F_CTRL, {f"{CT}BadBodies"}, "坏请求体不该走到仓储", "test"),
        ("CT17", "闸门档位回显把读侧与写侧并成一格", CTRL,
         [("\t\t\"stamps_write\": kbrelease.ShadowsOrOn(),", "\t\t\"stamps_write\": kbrelease.GateOn(),")],
         PKG_CTRL, F_CTRL, {f"{CT}GateModes"}, "~stamps_write:false", "test"),
        ("CT18", "底座不在时名单回空数组（把读不到说成一个都没有）", CTRL,
         [("\t\tsnap[\"governed_products\"] = nil\n\t\tsnap[\"note\"]",
           "\t\tsnap[\"governed_products\"] = []string{}\n\t\tsnap[\"note\"]")],
         PKG_CTRL, F_CTRL, {f"{CT}GateAnswersWithoutStore"}, "该是 null", "test"),
        ("CT19", "名单读取失败时不出声（回一个空名单）", CTRL,
         [("\t\tsnap[\"governed_products\"] = nil\n\t\tsnap[\"governed_note\"] = \"受治理库名单读取失败：\" + err.Error()",
           "\t\tsnap[\"governed_products\"] = []string{}")],
         PKG_CTRL, F_CTRL, {f"{CT}GateReadFailureStillAnswers"}, "少了读取失败的出声", "test"),
        ("CT20", "变更号取错（按号取数取到别的行）", CTRL,
         [("row, err := c.svc.GetChange(ctx.Request.Context(), id)",
           "row, err := c.svc.GetChange(ctx.Request.Context(), id+\"x\")")],
         PKG_CTRL, F_CTRL, {f"{CT}GetChange404"}, "按号取数取错了", "test"),
        ("CT21", "审批没给结论回 409（该报装配的成了改请求）", CTRL,
         [("\tcase errors.Is(err, service.ErrKBReleaseApprovalMissing):\n\t\tresponse.Error(ctx, http.StatusServiceUnavailable, err.Error())",
           "\tcase errors.Is(err, service.ErrKBReleaseApprovalMissing):\n\t\tresponse.Error(ctx, http.StatusConflict, err.Error())")],
         PKG_CTRL, F_CTRL, {f"{CT}ReplyErrorTable"}, "期望 503", "test"),
        ("CT22", "跨号发布禁令映成 500（运营会重试一个注定失败的动作）", CTRL,
         [("\t\terrors.Is(err, service.ErrKBReleaseRecallBan),\n", "")],
         PKG_CTRL, F_CTRL, {f"{CT}ReplyErrorTable"}, "发布禁令|~期望 409，实得 500", "test"),
        ("CT23", "就地改写被拒映成 500", CTRL,
         [("\t\terrors.Is(err, kbrelease.ErrGovernedDirectWrite):", "\t\tfalse:")],
         PKG_CTRL, F_CTRL, {f"{CT}ReplyErrorTable"}, "已进发布制不许直写|~期望 409，实得 500", "test"),
        ("CT24", "500 把内部错误原文回显出去（响应体放大器）", CTRL,
         [("response.Error(ctx, http.StatusInternalServerError, \"知识库变更操作失败\")",
           "response.Error(ctx, http.StatusInternalServerError, err.Error())")],
         PKG_CTRL, F_CTRL, {f"{CT}ReplyErrorTable"}, "500 该回固定文案", "test"),
        ("CT25", "提交口把 target_chunk_id 丢了", CTRL,
         [("\t\tTargetChunkID: body.TargetChunkID,", "\t\tTargetChunkID: 0,")],
         PKG_CTRL, F_CTRL, {f"{CT}SubmitShape", f"{CT}SubmitInputJudgments"},
         "报错文案该含|retire 不该带 content", "test"),
        ("CT26", "值域出口自己抄了一份 ops（库里加了选不到）", CTRL,
         [("\t\t\"ops\":                 model.KBChangeOps,", "\t\t\"ops\":                 []string{\"add\", \"revise\"},")],
         PKG_CTRL, F_CTRL, {f"{CT}Taxonomy"}, "两处不同源了", "test"),
        ("CT27", "值域出口开始依赖底座（最需要时不给答案）", CTRL,
         [("func (c *KBReleaseController) Taxonomy(ctx *gin.Context) {\n\tresponse.Success(ctx, gin.H{",
           "func (c *KBReleaseController) Taxonomy(ctx *gin.Context) {\n\tif !c.svc.Available() {\n\t\tc.unavailable(ctx)\n\t\treturn\n\t}\n\tresponse.Success(ctx, gin.H{")],
         PKG_CTRL, F_CTRL, {f"{CT}TaxonomyWithoutStore"}, "taxonomy 仍该回 200", "test"),
        ("CT28", "变更留痕的 subject_key 丢了前缀", CTRL,
         [("\trows, ok := c.audit(ctx, \"change:\"+id)", "\trows, ok := c.audit(ctx, id)")],
         PKG_CTRL, F_CTRL, {f"{CT}AuditSubjectKeys", f"{CT}RealStack"}, "subject_key 该是", "test"),
        ("CT29", "发布出口把 result 吞了（空发布与没发生分不开）", CTRL,
         [("response.Success(ctx, gin.H{\"verdicts\": verdicts, \"result\": result}, \"ok\")",
           "response.Success(ctx, gin.H{\"verdicts\": verdicts, \"result_x\": result}, \"ok\")")],
         PKG_CTRL, F_CTRL, {f"{CT}PublicNullVsEmpty", f"{CT}PublishGovernanceAndBucket",
                            f"{CT}PublishIntentCarriesApprovedOnly", f"{CT}RealStack"},
         "result 该是 null", "test"),
        ("CT30", "回滚与放回并成一条通路", CTRL,
         [("\t\tres, err = c.svc.Restore(ctx.Request.Context(), product, operator)\n\t} else {\n\t\tres, err = c.svc.Rollback(ctx.Request.Context(), product, operator)",
           "\t\tres, err = c.svc.Rollback(ctx.Request.Context(), product, operator)\n\t} else {\n\t\tres, err = c.svc.Restore(ctx.Request.Context(), product, operator)")],
         PKG_CTRL, F_CTRL, {f"{CT}PointerMovesNotInterchangeable"}, "~rollback 期望 200，实得 500", "test"),
        ("CT31", "撤回口不校验跃迁（把服务层的 409 说成 200）", CTRL,
         [("\tcase errors.Is(err, service.ErrKBChangeNotWithdrawable),\n\t\terrors.Is(err, service.ErrKBReleaseNotGoverned),",
           "\tcase errors.Is(err, service.ErrKBReleaseNotGoverned),")],
         PKG_CTRL, F_CTRL, {f"{CT}WithdrawPaths", f"{CT}ReplyErrorTable", f"{CT}RealStack"},
         "~期望 409，实得 500", "test"),

        # ---------------- router 挂载 / app 装配 ----------------
        ("RT1", "挂载不读全局登记处（路由对着 nil 底座挂上）", ROUTERGO,
         [("\tsvc := service.GlobalKBReleaseService()",
           "\tvar svc *service.KBReleaseService")],
         PKG_ROUTER, F_ROUTER, {f"{RT}AssemblyAndStopAreSymmetric", f"{RT}StaticSegmentsWin"},
         "装配后新挂的路由应 200", "test"),
        ("RT2", "gate 静态段被参数段抢走（档位读不回来）", CTRL,
         [("\t\trel.GET(\"/gate\", c.Gate)", "\t\trel.GET(\"/gate-\", c.Gate)")],
         PKG_ROUTER, F_ROUTER, {f"{RT}StaticSegmentsWin", f"{RT}UnassembledAnswers503WithoutData"},
         "被 :product 抢走了", "test"),
        ("RT3", "变更底座装在了审批运行时之前（两份审批服务分家）", MAINROUTER,
         [("\t// 而变更行在发布之前一行都不进 knowledge_chunks ⇒ 装上不等于线上有变化。\n\tapp.InitKBReleaseRuntime(gormDB)\n",
           "\t// 而变更行在发布之前一行都不进 knowledge_chunks ⇒ 装上不等于线上有变化。\n"),
          ("\tapp.InitApprovalRuntime(gormDB)",
           "\tapp.InitKBReleaseRuntime(gormDB)\n\tapp.InitApprovalRuntime(gormDB)")],
         PKG_ROUTER, F_ROUTER, {f"{RT}AssemblyOrderInRouter"}, "装在了审批运行时之前", "test"),
        ("RT4", "挂载挂到了鉴权组之外（六个写入口匿名可用）", MAINROUTER,
         [("\t\tsetupKBReleaseRoutes(auth)", "\t\tsetupKBReleaseRoutes(public)")],
         PKG_ROUTER, F_ROUTER, {f"{RT}AssemblyOrderInRouter"}, "实参形状不再是", "test"),
        ("AP1", "无 DB 句柄时不清空全局（路由对着未装配回 200）", WIRE,
         [("\t\tservice.SetGlobalKBReleaseService(nil)", "\t\tservice.SetGlobalKBReleaseService(service.GlobalKBReleaseService())")],
         PKG_APP, F_APP, {f"{AP}NilDBClearsGlobal"}, "仍留有实例", "test"),
        ("AP2", "撤销装配没清全局（旗子关掉后端点照旧服务）", WIRE,
         [("func StopKBReleaseRuntime() {\n\tservice.SetGlobalKBReleaseService(nil)\n}",
           "func StopKBReleaseRuntime() {\n\tservice.SetGlobalKBReleaseService(service.GlobalKBReleaseService())\n}")],
         PKG_APP, F_APP, {f"{AP}RegistersUsableGlobal"}, "StopKBReleaseRuntime 没清全局", "test"),
        ("AP3", "装配出来的实例没登记到全局（路由取到 nil）", WIRE,
         [("\tservice.SetGlobalKBReleaseService(svc)\n\n\tmode := kbrelease.ModeForLog()",
           "\n\tmode := kbrelease.ModeForLog()")],
         PKG_APP, F_APP, {f"{AP}RegistersUsableGlobal", f"{AP}UsesGlobalApprovalService",
                          f"{AP}WithoutGlobalApprovalStillEnqueues"}, "不是同一个实例", "test"),

        # ---------------- 写侧打戳与就地改写守卫（既有通路） ----------------
        ("KR1", "单条插入不打版本戳（AC① 的无声绕过口）", KREPO,
         [("\tif err := kbrelease.StampOneForWrite(ctx, r.db, chunk); err != nil {\n\t\treturn err\n\t}\n", "")],
         PKG_KREPO, F_KREPO, {f"{KR}WriteStampingRoutesToDraftBucket"}, "库里那行 kb_version", "test"),
        ("KR2", "批量插入不打版本戳", KREPO,
         [("\tif err := kbrelease.StampForWrite(ctx, r.db, chunks); err != nil {\n\t\treturn err\n\t}\n", "")],
         PKG_KREPO, F_KREPO, {f"{KR}WriteStampingRoutesToDraftBucket"}, "受管库的批量行该进桶", "test"),
        ("KR3", "打戳失败被静默放过（读不到归属就当没进发布制）", KREPO,
         [("\tif err := kbrelease.StampOneForWrite(ctx, r.db, chunk); err != nil {\n\t\treturn err\n\t}",
           "\tif err := kbrelease.StampOneForWrite(ctx, r.db, chunk); err != nil {\n\t\t_ = err\n\t}")],
         PKG_KREPO, F_KREPO, {f"{KR}WriteStampingRoutesToDraftBucket"}, "归属账读不动时 Create 仍成功了", "test"),
        ("KR4", "就地改写在 shadow 档也被拦（观察期改了写语义）", KREPO,
         [("func (r *KnowledgeChunkRepository) guardDirectWrite(ctx context.Context, productID string, id uint64) error {\n\tif !kbrelease.GateOn() {\n\t\treturn nil\n\t}",
           "func (r *KnowledgeChunkRepository) guardDirectWrite(ctx context.Context, productID string, id uint64) error {\n\tif !kbrelease.ShadowsOrOn() {\n\t\treturn nil\n\t}"),
          ("\tblocked, err := kbrelease.DirectWriteBlocked(ctx, r.db, productID)\n\tif err != nil {\n\t\treturn err\n\t}",
           "\tblocked, err := kbrelease.DirectWriteBlocked(ctx, r.db, productID)\n\tif err != nil {\n\t\treturn err\n\t}\n\tif !kbrelease.GateOn() {\n\t\tblocked = productID != \"\"\n\t}")],
         PKG_KREPO, F_KREPO, {f"{KR}GuardDirectWrite"}, "shadow 档不该拦就地改写", "test"),
        ("KR5", "删除不看闸门（已上线内容被没有留痕的动作撤下）", KREPO,
         [("\tif err := r.guardDirectWrite(ctx, \"\", id); err != nil {\n\t\treturn err\n\t}",
           "\t_ = r.guardDirectWrite(ctx, \"\", id)")],
         PKG_KREPO, F_KREPO, {f"{KR}GuardDirectWrite"}, "应回 ErrGovernedDirectWrite", "test"),
        ("KR6", "回查归属时把没有这一行当成错误（凭空多一个 404）", KREPO,
         [("\t\tif errors.Is(err, gorm.ErrRecordNotFound) {\n\t\t\treturn nil\n\t\t}",
           "\t\tif errors.Is(err, gorm.ErrRecordNotFound) {\n\t\t\treturn err\n\t\t}")],
         PKG_KREPO, F_KREPO, {f"{KR}GuardDirectWrite"}, "删不存在的 id 不该报错", "test"),
        ("KR7", "整批删除的告警数了全部行（把重建也报成丢在服内容）", KREPO,
         [("\tpred := kbrelease.VisiblePredicate()\n\tif pred == \"\" {\n\t\treturn nil\n\t}",
           "\tpred := \"1 = 1\"\n\tif pred == \"\" {\n\t\treturn nil\n\t}")],
         PKG_KREPO, F_KREPO, {f"{KR}BatchDeleteWarnsOnlyInForceRows"}, "告警行数", "test"),
        ("KR8", "批量删除的告警撤掉（丢了多少没人知道）", KREPO,
         [("\tif inForce > 0 {", "\tif inForce < 0 {")],
         PKG_KREPO, F_KREPO, {f"{KR}BatchDeleteWarnsOnlyInForceRows"}, "告警行数", "test"),
        ("KR9", "归属账读不动时删除照样执行（拿不准就放行）", KREPO,
         [("\t\tWhere(where, arg).Where(pred).Count(&inForce).Error; err != nil {\n\t\treturn err\n\t}",
           "\t\tWhere(where, arg).Where(pred).Count(&inForce).Error; err != nil {\n\t\treturn nil\n\t}")],
         PKG_KREPO, F_KREPO, {f"{KR}BatchDeleteWarnsOnlyInForceRows"}, "归属账读不动时删除仍成功了", "test"),

        # ---------------- 读路径闸门：11 个拼接点 + 2 处影子出声 ----------------
        ("KS1", "bm25 全库召回不挂闸门（未发布内容当场可检）", KBM25,
         [("\tq := kbrelease.WhereVisible(s.db.WithContext(ctx).\n\t\tTable(\"knowledge_chunks\").\n\t\tSelect(\"id, document_id, content\").\n\t\tWhere(\"embedding IS NULL OR embedding IS NOT NULL\"))",
           "\tq := s.db.WithContext(ctx).\n\t\tTable(\"knowledge_chunks\").\n\t\tSelect(\"id, document_id, content\").\n\t\tWhere(\"embedding IS NULL OR embedding IS NOT NULL\")")],
         PKG_KSVC, F_KSVC, {f"{KG}RagSearcherSitesHideUnpublished"}, "档的可见集合不对", "test"),
        ("KS2", "bm25 按库召回不挂闸门", KBM25,
         [("\tq := kbrelease.WhereVisible(s.db.WithContext(ctx).\n\t\tTable(\"knowledge_chunks\").\n\t\tSelect(\"id, document_id, content\").\n\t\tWhere(\"product_id = ?\", productID))",
           "\tq := s.db.WithContext(ctx).\n\t\tTable(\"knowledge_chunks\").\n\t\tSelect(\"id, document_id, content\").\n\t\tWhere(\"product_id = ?\", productID)")],
         PKG_KSVC, F_KSVC, {f"{KG}RagSearcherSitesHideUnpublished"}, "档的可见集合不对", "test"),
        ("KS3", "向量召回（按库）不挂闸门", KVEC,
         [("AND product_id = ?` + kbrelease.AndVisible() + `", "AND product_id = ?` + \"\" + `")],
         PKG_KSVC, F_KSVC, {f"{KG}RagSearcherSitesHideUnpublished"}, "档的可见集合不对", "test"),
        ("KS4", "向量召回（全库）不挂闸门", KVEC,
         [("AND embedding_source = 'tei'` + kbrelease.AndVisible() + `", "AND embedding_source = 'tei'` + \"\" + `")],
         PKG_KSVC, F_KSVC, {f"{KG}RagSearcherSitesHideUnpublished"}, "档的可见集合不对", "test"),
        ("KS5", "通用那一路的影子读数没了", KSVC,
         [("\tkbrelease.LogWouldHide(ctx, s.db, ids, \"\", \"rankRAGChunks\")\n", "")],
         PKG_KSVC, F_KSVC, {f"{KG}ShadowLogsWouldHide", f"{KG}ShadowSilentWhenNothingWouldHide"},
         "档该有 1 行 shadow 读数", "test"),
        ("KS6", "商家那一路的读数被通用那一路顶替（label 写串）", KSVC,
         [("kbrelease.LogWouldHide(ctx, s.db, ids, \"\", \"rankMerchantChunks\")",
           "kbrelease.LogWouldHide(ctx, s.db, ids, \"\", \"rankRAGChunks\")")],
         PKG_KSVC, F_KSVC, {f"{KG}ShadowLogsWouldHide"}, "档该有 1 行 shadow 读数", "test"),
        ("KC1", "就地改写的拒绝被映成 400（运营照着改请求重试）", KCTRL,
         [("\tif errors.Is(err, kbrelease.ErrGovernedDirectWrite) {\n\t\tresponse.Error(c, http.StatusConflict, err.Error())",
           "\tif errors.Is(err, kbrelease.ErrGovernedDirectWrite) {\n\t\tresponse.Error(c, http.StatusBadRequest, err.Error())")],
         PKG_KCTRL, F_KCTRL, {KM}, "实得", "test"),
        ("KC2", "这条出口不再认发布制的拒绝", KCTRL,
         [("\tif errors.Is(err, kbrelease.ErrGovernedDirectWrite) {", "\tif errors.Is(err, kbrelease.ErrGovernedDirectWrite) && false {")],
         PKG_KCTRL, F_KCTRL, {KM}, "实得", "test"),
        ("RR1", "hybrid 向量那一路不挂闸门", HYB,
         [("\tsql += kbrelease.AndVisible()\n", "\tsql += \"\"\n")],
         PKG_RR, F_RR, {f"{KG}RetrievalSitesHideUnpublished"}, "档召回", "test"),
        ("RR2", "hybrid 词面那一路不挂闸门（带库过滤）", HYB,
         [("\tsql += kbrelease.AndVisible() + \" ORDER BY score DESC LIMIT ?\"", "\tsql += \"\" + \" ORDER BY score DESC LIMIT ?\"")],
         PKG_RR, F_RR, {f"{KG}RetrievalSitesHideUnpublished"}, "档召回", "test"),
        ("RR3", "hybrid 兜底那一路不挂闸门", HYB,
         [("\tsql += kbrelease.AndVisible() + \" ORDER BY id DESC LIMIT ?\"", "\tsql += \"\" + \" ORDER BY id DESC LIMIT ?\"")],
         PKG_RR, F_RR, {f"{KG}RetrievalSitesHideUnpublished"}, "档召回", "test"),
        ("RR4", "lexical tsquery 带库过滤不挂闸门", LEX,
         [("\t\tsql += \" AND product_id = ?\" + kbrelease.AndVisible() + \" ORDER BY score DESC LIMIT ?\"",
           "\t\tsql += \" AND product_id = ?\" + \"\" + \" ORDER BY score DESC LIMIT ?\"")],
         PKG_RR, F_RR, {f"{KG}RetrievalSitesHideUnpublished"}, "档召回", "test"),
        ("RR5", "lexical tsquery 全库不挂闸门", LEX,
         [("\t\tsql += kbrelease.AndVisible() + \" ORDER BY score DESC LIMIT ?\"",
           "\t\tsql += \"\" + \" ORDER BY score DESC LIMIT ?\"")],
         PKG_RR, F_RR, {f"{KG}RetrievalSitesHideUnpublished"}, "档召回", "test"),
        ("RR6", "lexical 兜底带库过滤不挂闸门", LEX,
         [("\t\tsql += \" AND product_id = ?\" + kbrelease.AndVisible() + \" ORDER BY id DESC LIMIT ?\"",
           "\t\tsql += \" AND product_id = ?\" + \"\" + \" ORDER BY id DESC LIMIT ?\"")],
         PKG_RR, F_RR, {f"{KG}RetrievalSitesHideUnpublished"}, "档召回", "test"),
        ("RR7", "lexical 兜底全库不挂闸门", LEX,
         [("\t\tsql += kbrelease.AndVisible() + \" ORDER BY id DESC LIMIT ?\"",
           "\t\tsql += \"\" + \" ORDER BY id DESC LIMIT ?\"")],
         PKG_RR, F_RR, {f"{KG}RetrievalSitesHideUnpublished"}, "档召回", "test"),
        ("RR8", "vector 检索器 SearchVector 带库过滤不挂闸门", VEC,
         [("\t\tif kbID != \"\" {\n\t\t\tsql += \" AND product_id = ?\" + kbrelease.AndVisible() + \" ORDER BY embedding <=> ?::vector LIMIT ?\"",
           "\t\tif kbID != \"\" {\n\t\t\tsql += \" AND product_id = ?\" + \"\" + \" ORDER BY embedding <=> ?::vector LIMIT ?\"")],
         PKG_RR, F_RR, {f"{KG}RetrievalSitesHideUnpublished"}, "档召回", "test"),
        ("RR9", "vector 检索器 SearchVector 全库不挂闸门", VEC,
         [("\t\t\targs = append(args, kbID, vecLiteral, topK)\n\t\t} else {\n\t\t\tsql += kbrelease.AndVisible() + \" ORDER BY embedding <=> ?::vector LIMIT ?\"",
           "\t\t\targs = append(args, kbID, vecLiteral, topK)\n\t\t} else {\n\t\t\tsql += \"\" + \" ORDER BY embedding <=> ?::vector LIMIT ?\"")],
         PKG_RR, F_RR, {f"{KG}RetrievalSitesHideUnpublished"}, "档召回", "test"),
        ("RR10", "vector 检索器 Retrieve 带库过滤不挂闸门", VEC,
         [("\t\tif productID != \"\" {\n\t\t\tsql += \" AND product_id = ?\" + kbrelease.AndVisible() + \" ORDER BY embedding <=> ?::vector LIMIT ?\"",
           "\t\tif productID != \"\" {\n\t\t\tsql += \" AND product_id = ?\" + \"\" + \" ORDER BY embedding <=> ?::vector LIMIT ?\"")],
         PKG_RR, F_RR, {f"{KG}RetrievalSitesHideUnpublished"}, "档召回", "test"),
        ("RR11", "vector 检索器 Retrieve 全库不挂闸门", VEC,
         [("\t\t\targs = append(args, productID, vecLiteral, topK)\n\t\t} else {\n\t\t\tsql += kbrelease.AndVisible() + \" ORDER BY embedding <=> ?::vector LIMIT ?\"",
           "\t\t\targs = append(args, productID, vecLiteral, topK)\n\t\t} else {\n\t\t\tsql += \"\" + \" ORDER BY embedding <=> ?::vector LIMIT ?\"")],
         PKG_RR, F_RR, {f"{KG}RetrievalSitesHideUnpublished"}, "档召回", "test"),
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
    """期望红名必须在**本格要跑的那个包**里有定义（按包核才算核住）。"""
    return any(f"func {name}(" in p.read_text(encoding="utf-8") for p in test_files(pkg))


def reason_tokens(spec: str) -> list[str]:
    """红因 = 若干 token 用 | 连接；判据是「同一行输出里同时出现全部 token」。"""
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
        """克隆已经建起来之后的中止路：先回收私有克隆，再出声（否则一轮 50–90MB 留在临时目录）。"""
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
    # 构建用的 GOOS=linux 绝不能带进测试：交叉编译出的测试二进制在 mac 上 exec 不了。
    for k in ("GOOS", "GOARCH"):
        env.pop(k, None)
    # 闸门档位必须由夹具自己 t.Setenv 决定：宿主机上残留的 FF_LTC_KB_CHANGE_GATE 会把
    # "off 档"那一整族用例改成真拦（假红）或把 on 档改成不打戳（假绿）。
    env.pop("FF_LTC_KB_CHANGE_GATE", None)
    env["CGO_ENABLED"] = "0"
    env.setdefault("GIN_MODE", "test")
    # 本卡的向量补算腿走 fake embedding 桩；没有这一句时它会去连真 TEI（不可达即整包红）。
    env.setdefault("EMBEDDING_ALLOW_FALLBACK", "true")
    env.setdefault("GOCACHE", os.environ.get("P902_MUT_GOCACHE", "/tmp/gocache-r45mut"))
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
    """TCP 探活。8232 是端口转发（127.0.0.1:8232 -> 容器 8202），没有 /tmp 下的 unix socket
    ⇒ 用 `nc -U` 那一族探会**恒**报不可达，据此停机就是把探法错误读成库没了。"""
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
    那条分支" ⇒ 这一类只有真跑照得出来，`--check` 绿**不是**"电池有牙"。
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
        if pkg not in PKG_ALL:
            bad.append(f"{code}：包路径不在本卡十一个被测包里：{pkg}")
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
        ("ST1 无效果变异", [("KBChangeOpAdd    = \"add\"", "KBChangeOpAdd    = \"add\"")],
         {"TestKBChangeModel_TableNames"}, "SURVIVED"),
        ("ST2 期望写歪", [("KBChangeStatusApplied:   {},", "KBChangeStatusApplied:   {KBChangeStatusWithdrawn: true},"),],
         {"TestKBChangeModel_NoSuchLeg"}, "BROKEN"),
        ("ST3 编译坏", [("KBChangeOpAdd    = \"add\"", "KBChangeOpAdd = ")],
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
    ap = argparse.ArgumentParser(description="T-P9-02 知识库变更流程变异电池")
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
    # --only 里的名字必须都是表里真有的格：`LF1..LF20` 这类"区间写法"在这里不会展开，
    # 悄悄少跑只会让下一位读到"本趟跑了 1/161 格"还以为是整族读数（2026-09-28 实测踩到）。
    if only:
        known = {code for code, *_ in cells()}
        unknown = sorted(only - known)
        if unknown:
            print(f"!! --only 里有 {len(unknown)} 个名字不在格子表里（区间写法不会展开）：{unknown}")
            sys.exit(4)
    st = os.statvfs("/tmp")
    if st.f_bavail * st.f_frsize < 10 * 1024 ** 3:
        print("ENV-BROKEN：/tmp 空闲不足 10 GB（编译缓存写满盘带来的红全是假红）")
        return 4

    logs = ROOT / args.logs / args.tag
    logs.mkdir(parents=True, exist_ok=True)
    from battlog import tee_to
    tee_to(logs / "00-run.log")

    tmp, owned = workdir(args.clone, prefix="p902mut-", repo_root=ROOT)
    tmp.mkdir(parents=True, exist_ok=True)
    rc = 3
    try:
        head = subprocess.run(["git", "-C", str(ROOT), "rev-parse", "--short", "HEAD"],
                              capture_output=True, text=True).stdout.strip()
        load = subprocess.run(["uptime"], capture_output=True, text=True).stdout.strip()
        print(f"私有作业目录：{tmp}\n逐格日志目录：{logs}")
        print(f"取证基线：HEAD={head} 工作树={ROOT} {load} 日志 tag={args.tag}")

        clone = prepare(tmp, owned)
        env = test_env(clone)
        if not pg_ready(env):
            print(f"ENV-BROKEN：测试库 {env['POSTGRES_TEST_HOST']}:{env['POSTGRES_TEST_PORT']} 连不上 ⇒ "
                  "所有『真表/真冲突/并发』腿都会 SKIP，本趟不放刀")
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

        # -------- 控制组：十一个过滤器各现测一次 settled 与红名（任一不干净就停机）--------
        controls: dict[str, dict] = {}
        for pkg, filt in CONTROLS:
            c = run_go(clone, pkg, filt, env)
            key = pkg + filt
            (logs / f"control_{pkg.replace('./internal/', '').replace('./', '').replace('/', '_')}.log").write_text(
                scrub(c["out"]))
            print(f"[控制组 {pkg}] rc={c['rc']} settled={c['settled']} skip={c['skipped']} 红名={c['red'] or '—'}")
            if c["rc"] != 0 or c["settled"] == 0 or c["skipped"] or c["red"] or c["panicked"]:
                print("\n".join(causes(c["out"])))
                print("ENV-BROKEN：控制组不干净——后面所有红/绿都不可信，停机")
                return 4
            controls[key] = c

        # -------- 逐格注码 --------
        problems: list[str] = []
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
                        raise SystemExit(f"{code} 还原后 md5 与开刀前不一致，停机（后面全是脏树读数）")
                (logs / f"{code}.log").write_text(scrub(r["out"]))
                status = classify(r, controls[pkg + filt]["settled"], expect_red, expect_reason, judge)
                detail = f"红={','.join(r['red']) or '—'} settled={r['settled']}/{controls[pkg + filt]['settled']}"
            print(f"{code:<5} {desc[:46]:<48} {status:<30} {detail}")
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
