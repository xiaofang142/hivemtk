#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""T-P7-03 变异电池：逾期识别 + 催收提醒外发 + 升级待办。

一、这族电池在证什么
    不是"覆盖率"，是**判据有没有牙**：把生产代码逐处改成它最可能的坏法，
    如果整套用例仍然全绿，那这一处判据就是写在注释里的 —— 它拦不住任何东西。
    每一格都点名"哪一条用例必须红"（expect）；红了别的不算杀掉（RED-UNNAMED）。

二、本卡的四趟复核顺序（第二趟就是这张表）
    第一趟 逐格对着 AC 写注码 ⇒ 判"这条判据存在吗"；
    第二趟 逐格问"注完码之后，仓库里到底有没有一条断言会红" ⇒ 判"这条判据有牙吗"；
    第三趟 锚点搬家（本卡改过 model/bill.go 与 repository/bill.go，p701 的 K/R 族锚点
            必须在新字节下重核 ⇒ 实测：`anchor-preflight.py` 对 p701 的 72 格报 0 处问题，
            本卡没有让它失效；下一卡若再动这两份文件的这几行，仍以该门为准）；
    第四趟 已知不覆盖面逐条给理由（写在下面第五节），不许留成"以后再说"。

三、形状与约定（与 p701 逐字同源，改一处要改两处）
    格子 = (代号, 说明, kind, runner, 默认文件, [(旧, 新) | (文件, 旧, 新)], expect)
      · kind: "go" 跑用例 / "gate" 只跑 scripts/check-unwired-assets.sh
      · runner 见 RUNNERS；两文件叠打的格子把文件写进三元组
      · 所有注码在内存里叠完一次写盘（逐格 apply() 会把前一格还原掉）
      · 还原逐文件比 md5，不一致就停机（证据比结论重要）
      · 控制组在放刀之前**现测**（共享工作树里别人的用例会漂）
      · 顶层 PASS + FAIL 必须等于控制组顶层数（panic 带不走计数就带不走这里）
      · `--check` 只验锚点，另外把"注完码字节没变"单独列成一类（永不开火的格子）

四、为什么有些格子是"改名"而不是"删掉"
    台账那四条（25a–25d）锁的是"某个目录里存在某个调用"这句话。删掉调用会让包编不过，
    而门根本不看编译 —— 于是"删掉"与"改名"对门来说是同一件事，改名那一版还额外证明了
    **这条 grep 会被改名瞎掉**（这正是它作为锁的真实弱点，也是一格该做的事）。
    五把依赖的构造点那一格（25a）用 `NewCollectionJobForP703(` 这种"看着像但正则不认"的
    形状：callpat `NewCollectionJob\\(` 命中数必须掉到 0，门才会报漂移。

五、已知不覆盖面（每条都给理由，不是待办）
    1) `go j.loop(ctx)` 整行摘掉：Running() 仍然为真、Start 的日志仍然印，
       唯一能发现"协程没起"的是等一个间隔（默认 6h）之后的读数。用例面上只有把它写成
       真等 30 分钟的定时断言才能抓到 —— 那是一条天天自己红的假红源。
       这一格由 ledger 25b（装配点在不在）+ 观测端点的 running 读数共同承担，不由用例承担。
    2) （第二趟复核改判：这一格已收编为 W15，不再是"不覆盖"）原先登记的理由是"app 侧没有
       半装配夹具，所以取反在 app runner 里 0==0"——它只说对了"造不出 available=false 的实例"，
       说错了判据在哪：off 档那条用例**直接断言 `snap.Available`**，取反后它就是红的。
       登记一条"不覆盖"之前要先把那句断言读一遍，不能从"夹具造不出某种实例"推出"判据没牙"。
       Available 的五项合取仍由 S45 钉（逐把摘都会红），W14 现在也改成注 nil 实参而不是删实参。
    3) `snap.RemindedTotal` / `EscalatedTotal` 的抄录行：off 档下两个数恒为 0，
       把赋值删掉或改成常量 0 在 app runner 里同形。跨轮累计由 service 侧
       S20/S21 两格钉住（remindedAll/escalatedAll 各摘一次都会红）。
    4) 端口接口"变宽"（给 collectionBillScanner 加一个方法）：四个假件立刻编不过，
       判据类别是 BUILD-BROKEN，不算干净杀掉。TestCollectionJobPortSurfacesAreNarrow
       的反射比是**评审锁**（改端口必须同时改假件与这张表），不是变异目标。
    5) `Where("due_at IS NOT NULL AND due_at < ?", cutoff)` 里那半句 `IS NOT NULL` 摘掉：
       PostgreSQL 里 `NULL < x` 求值为 NULL ⇒ 那批行的筛选结果一字不变，
       而 `Undated` 走的是另一条独立查询。这是一次**等价变异**，不是洞。
    6) 正文文案改字（"已逾期"改成"逾期已满"）：只有 TestCollectionOverdueDaysDisplayFloorsRatherThanRounds
       那格认这两个字，其余断言按账单号与金额检索。文案的字面值不作为判据锁 ——
       可复现性锁的是"同输入同输出"，不是"这句话不许改"。
    7) collectionGateNote 的两条分支文案：它同时进快照与日志，
       内容由 W03/W04 两格（挂没挂上）钉住，具体措辞不钉。

六、跑这一族电池的环境前提（第二趟复核踩到的）
    本机 `xcode-select -p` 若指向完整的 Xcode.app 而它的许可没同意，任何 cgo 编译都会以
    `# runtime/cgo: You have not agreed to the Xcode license` 失败 ⇒ 这族电池的每个 runner
    都判成 BUILD-BROKEN（控制组那一步就会停机，不会假绿，但要认得出是环境不是代码）。
    正解是 `export DEVELOPER_DIR=/Library/Developer/CommandLineTools` 后重跑；
    `CGO_ENABLED=0` **不等价**（挽回/巡检那一族的 `-race` 腿需要 cgo）。

七、第二趟复核（判据有没有牙）逐格结论
    112 格首跑（取证 logs/p703-mut1-run1.log，测于 HEAD `c730c432`）：105 杀 / 2 存活 /
    4 红而未点名 / 1 计数不平 / 1 BUILD-BROKEN，`battery_rc=1`。逐格定案：
      · S13 存活 = **等价变异**，不是洞：只摘 `continue` 会落进紧跟其后的 `if !claimed`，
        而假件在报错时回的就是 claimed=false ⇒ 仍然不发。真 fail-open 的形状是"把报错
        当成占到了"，注码已改成翻 claimed；升级那条路的同一处另有 S57（两处分刀，不是一处）。
      · S46 存活 = **真洞**，按 TDD 补了 DETACHED 那条用例（先红后绿），并顺手把发布位
        的抄件补上（setLast 里 `copied := *r`）——锁只订得住指针，订不住它指向的那格；
        两处抄件两处刀：S46（读侧）与 S58（写侧）。〔那条补出来的用例**第一版自己空转**，
        第三趟复跑时 S46 又活了一次，判据形状见 §八〕
      · S25/S36/S39 三格是**expect 点错了人**（不是判据没牙）：红的那条才是喂到那条分支的
        用例。S25 的 BLOCKED 那条只喂三种哨兵错误，永远走不到 default；S36 的 GRACE 用的是
        不换夏令时的 FixedZone，Add 与 AddDate 在它身上同形（ ⇒ 另补 S59 给 GRACE 一架真实的刀）；
        S39 的 MISSINGID 喂的是"两键都空"，&& 与 || 同形。
      · S55 的"计数不平"是**测试自己 panic**（`claim.sets[1]` 越界）把二进制带走了：这一趟只
        留下 22 条 RUN 行 / 7 顶层 PASS + 12 顶层 FAIL，而控制组是 57 条 RUN / 42 顶层 PASS
        ⇒ 后面 35 条腿压根没跑，被点名的 SHADOW 就在没跑的那一截里。变异本身有牙（12 条用例
        因它而红）。修法不是改期望，而是给三条用例补前置 Fatalf（见测试里那三处
        `t.Fatalf("…前置不成立…")`）——前置不成立时后面的断言没有对象，而一条 panic 会跨用例
        带走整个二进制。
      · W14 第一版注码"删掉整块实参"编不过（少实参），BUILD-BROKEN 证不到任何事；真在现网
        发生得了的形状是"接了一把 nil 进去"，接口参数允许 nil，编译期无人报警。
    同轮另修：app 那条并发腿的假判据（`!snap.Assembled && snap.Mode != ""` 永远不成立，
    未装配臂按设计就是回显 env）换成 Assembled/UnassembledHint 一致性 + 闸门 mode 非空；
    reach-gate 三份包级全局补 `reachGateMu`（`-race` 实跑 6 块竞争 ⇒ 上锁后 0 块，
    反向摘锁复测又出 2 块，取证 logs/p703-race1/leg-race*.log）。
    **这一条判据的牙不在本电池里**：电池的 app runner 不带 `-race`（逐格跑整包 `-race` 会把
    共享盘写满、并把没还原的树留在原地），所以摘锁这类洞靠 CI 的 `Unit tests -race
    (user-server core)` 作业（`go list ./... | grep -vx hivemtk-user/internal/service` 覆盖
    internal/app，且这条腿没有 `testing.Short()` 跳过）＋上面那次就地反向测兜住。
    静态门 `check-async-global-read.py` 订不到这一类：它只看协程体里的**字面**裸读，
    而这一处是"协程里调了一个读全局的函数"，站点数为 0 的那一趟它照样绿。

八、第三趟复跑（改过夹具之后必须整族重来，取证 logs/p703-mut2-run1.log）
    116 格测于 HEAD `39e329da`：115 杀 / 1 存活（S46）/ 0 红而未点名 / 0 BUILD-BROKEN；
    控制组现测 app ran=10、svc ran=58（比首跑各多 1 条 = 本批新增的两条用例）。
    **S46 又活了一次，这次不是产码的洞，是上一轮补的那条用例自己空转**：它的期望值取自
    `base := job.LastReport()` —— 在"读侧不抄件"这个变异下 `base`、`probe`、`again` 指的是
    **同一格**，于是 `again.Overdue != base.Overdue` 恒假。
    教训：判"某处没抄件"的用例，基准值不许从**待判的那把 accessor** 里读（读了就等于让
    被污染的那一份自证干净）；这类洞最硬的断言是**指针同一性**（两次读必须拿到两个格子），
    它不依赖任何字段值，换字段、换顺序、改期望都绕不过。已改成 `snap := *job.LastReport()`
    值快照 + `if a, b := job.LastReport(), job.LastReport(); a == b`；反向取证：就地注 S46
    那一刀 ⇒ 两条断言各红一次（collection_job_test.go:1312 与 :1321），还原后 md5 一致。
    写侧那一刀（S58）从一开始就靠指针同一性杀 ⇒ 它首跑即 KILLED，反证了"值断言会空转、
    身份断言不会"。
"""

import argparse
import hashlib
import os
import re
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
US = "user-server"
ANSI = re.compile(r"\x1b\[[0-9;]*m")

# —— 本卡新建的文件（只用于"工作树 vs HEAD"的漂移守卫）——
NEW_FILES = [
    f"{US}/internal/model/bill_chased_test.go",
    f"{US}/internal/repository/bill_overdue_scan_test.go",
    f"{US}/internal/service/collection_job.go",
    f"{US}/internal/service/collection_job_test.go",
    f"{US}/internal/app/collection_wiring.go",
    f"{US}/internal/app/collection_wiring_test.go",
    # 第二趟复核改的是闸门那两份（补 reachGateMu 与其读侧），它们不属于本卡新建的文件，
    # 但**这一趟的结论依赖它们的字节**：没提交就跑电池，app runner 测的是"上一版没锁的字节"，
    # 而结论会写成"并发腿已修"。放进这里 ⇒ prepare 直接拒跑，不给这个错机会。
    f"{US}/internal/app/reach_gate_wiring.go",
    f"{US}/internal/app/approval_wiring.go",
]

# —— 本卡落在既有文件里的插入：提交后它们**已经在 HEAD 里**，
# prepare 不打补丁，只逐条断言"在 HEAD 的这份文件里恰好出现一次"。
MY_ANCHORS = [
    (f"{US}/cmd/api/main.go", "\tif collectionJob := app.InitCollectionRuntime(db.GetDB()); collectionJob != nil {\n"),
    (f"{US}/internal/router/ltc_routes.go", '\tadmin.GET("/collection", handleCollectionStatusGet)\n'),
    (f"{US}/internal/app/collection_wiring.go", "func InitCollectionRuntime(db *gorm.DB) *service.CollectionJob {"),
    ("scripts/check-unwired-assets.sh", '  "25|催收任务的装配入口'),
    ("scripts/check-unwired-assets.sh", '  "25|催收腿在启动路径上的装配点'),
    ("scripts/check-unwired-assets.sh", '  "25|催收观测端点的挂载入口'),
    ("scripts/check-unwired-assets.sh", '  "25|催收这条外发路径的审批闸门装配点'),
]

MODEL = f"{US}/internal/model/bill.go"
REPOF = f"{US}/internal/repository/bill.go"
SVC = f"{US}/internal/service/collection_job.go"
WIRE = f"{US}/internal/app/collection_wiring.go"
ROUTES = f"{US}/internal/router/ltc_routes.go"
MAIN = f"{US}/cmd/api/main.go"
LEDGER = "scripts/check-unwired-assets.sh"

RUNNERS = {
    "model": (f"./internal/model/", "^TestBillChased"),
    "db": (f"./internal/pkg/db/", "^TestBill"),
    "repo": (f"./internal/repository/", "^TestBillRepository_ScanOverdue"),
    "svc": (f"./internal/service/", "^TestCollection"),
    "app": (f"./internal/app/", "^(TestInitCollectionRuntime|TestGetCollectionSnapshot)"),
    "route": (f"./internal/router/", "^TestCollection"),
    "cmd": (f"./cmd/api/", "^TestCollectionJobMounted"),
}

CALIBRE = "TestCollectionDocumentedContractSurfaceIsExact"
CHASED = "TestBillChasedStatusesCoverUnsettledOnly"
DOMAIN = "TestBillChasedStatusesAreInBillDomain"
KNOWN = "TestBillChasedStatusKnownRejectsUnknown"
INDEX = "TestBillChasedDueAtIndexExists"
SELECT = "TestBillRepository_ScanOverdueSelectsByCutoffAndStatus"
ORDER = "TestBillRepository_ScanOverdueOrdersOldestFirst"
LIMIT = "TestBillRepository_ScanOverdueRejectsNonPositiveLimit"
NOTEMPTY = "TestBillRepository_ScanOverdueFailureIsNotEmptyResult"
FOLLOW = "TestBillRepository_ScanOverdueFollowsTheChasedDomain"
ESCALATE = "TestCollectionEscalatesAfterThresholdAndStopsReminding"
SHADOW = "TestCollectionShadowSendsDryRunAndWritesNothing"
BLOCKED = "TestCollectionBlockedReasonsAreCountedApart"
MISSINGID = "TestCollectionMissingIdentitySendsNothingAndKeepsNoWindow"
FALLBACK = "TestCollectionFallsBackToCustomerIDWhenOneIDIsAbsent"
HALFROUND = "TestCollectionLastReportIsNeverAHalfRound"
DETACHED = "TestCollectionPublishedRoundIsDetachedFromBothHands"
SENDFAIL = "TestCollectionSendFailureReleasesTheWindow"
ESCCA = "TestCollectionEscalateClaimUnavailableSubmitsNothing"
ABSWINDOW = "TestCollectionCutoffIsAnAbsoluteWindowNotCalendarDays"
GRACE = "TestCollectionCutoffAppliesTheDocumentedGrace"
OFFAPP = "TestInitCollectionRuntime_OffModeAssemblesWithoutStarting"
SHADOWAPP = "TestInitCollectionRuntime_ShadowModeStartsAndStops"
GATEAPP = "TestInitCollectionRuntime_AttachesReachGate"
NILAPP = "TestInitCollectionRuntime_NilDBClearsRuntime"
CALIBRE_APP = "TestGetCollectionSnapshot_CarriesTheDocumentedCalibre"
TOTALS_APP = "TestGetCollectionSnapshot_CarriesCumulativeTotals"
VIEW = "TestCollectionStatusView_NamesTheFirstBlocker"
COND = "TestCollectionStatusView_CarriesEverySnapshotField"
CONDKEY = "TestCollectionStatusView_ConditionalKeysAppearOnlyWhenSet"
ENDPOINT = "TestCollectionStatusEndpoint"
STARTUP = "TestCollectionJobMountedAfterRouterSetup"

CELLS = [
    # ————— 值域层（runner=model：催收集与它的判定函数）—————
    ("M01", "催收集少一格 partial ⇒ 收了一部分仍欠的单永远没人催", "go", "model", MODEL,
     [("var BillStatusesChased = []string{\n\tBillStatusOpen, BillStatusPartial,\n}",
       "var BillStatusesChased = []string{\n\tBillStatusOpen,\n}")],
     CHASED),
    ("M02", "催收集多一格 paid ⇒ 已结清的人被催", "go", "model", MODEL,
     [("var BillStatusesChased = []string{\n\tBillStatusOpen, BillStatusPartial,\n}",
       "var BillStatusesChased = []string{\n\tBillStatusOpen, BillStatusPartial, BillStatusPaid,\n}")],
     CHASED),
    ("M03", "催收集里写进一个不属于账单值域的字面量", "go", "model", MODEL,
     [("var BillStatusesChased = []string{\n\tBillStatusOpen, BillStatusPartial,\n}",
       'var BillStatusesChased = []string{\n\tBillStatusOpen, "overdue",\n}')],
     DOMAIN),
    ("M04", "BillChasedStatusKnown 恒真（守卫形同虚设）", "go", "model", MODEL,
     [("\tfor _, v := range BillStatusesChased {\n\t\tif v == s {\n\t\t\treturn true\n\t\t}\n\t}\n\treturn false\n}",
       "\tfor _, v := range BillStatusesChased {\n\t\t_ = v\n\t}\n\treturn true\n}")],
     KNOWN),
    ("M05", "BillChasedStatusKnown 恒假（一切都不催）", "go", "model", MODEL,
     [("\tfor _, v := range BillStatusesChased {\n\t\tif v == s {\n\t\t\treturn true\n\t\t}\n\t}\n\treturn false\n}",
       "\tfor _, v := range BillStatusesChased {\n\t\t_ = v\n\t}\n\treturn false\n}")],
     KNOWN),
    ("M06", "逐字比改成忽略大小写（'OPEN' 被认成可催收）", "go", "model", MODEL,
     [('import "time"', 'import (\n\t"strings"\n\t"time"\n)'),
      ("\tfor _, v := range BillStatusesChased {\n\t\tif v == s {",
       "\tfor _, v := range BillStatusesChased {\n\t\tif strings.EqualFold(v, s) {")],
     KNOWN),
    # 索引三格打在 model 的 tag 上，runner 必须是 db：
    # 唯一读得到索引形状的是 pg_index 那一条查询（model 包里没有任何库可查）。
    ("M07", "DueAt 上的复合索引 tag 摘掉（催收每轮一次全表扫）", "go", "db", MODEL,
     [('DueAt *time.Time `gorm:"index:idx_bills_status_due,priority:2" json:"due_at"`',
       'DueAt *time.Time `json:"due_at"`')],
     INDEX),
    ("M08", "两格的索引名不一致 ⇒ 复合索引散成两个单列索引", "go", "db", MODEL,
     [('DueAt *time.Time `gorm:"index:idx_bills_status_due,priority:2" json:"due_at"`',
       'DueAt *time.Time `gorm:"index:idx_bills_due_status,priority:2" json:"due_at"`')],
     INDEX),
    ("M09", "priority 对调 ⇒ 索引变成 (due_at, status)，状态前缀失位", "go", "db", MODEL,
     [('DueAt *time.Time `gorm:"index:idx_bills_status_due,priority:2" json:"due_at"`',
       'DueAt *time.Time `gorm:"index:idx_bills_status_due,priority:1" json:"due_at"`'),
      ('Status string `gorm:"type:varchar(16);index:idx_bills_status_due,priority:1" json:"status"`',
       'Status string `gorm:"type:varchar(16);index:idx_bills_status_due,priority:2" json:"status"`')],
     INDEX),

    # ————— 仓储层：逾期扫描（runner=repo）—————
    ("R01", "催收集少一格，SQL 跟着少捞一类", "go", "repo", MODEL,
     [("var BillStatusesChased = []string{\n\tBillStatusOpen, BillStatusPartial,\n}",
       "var BillStatusesChased = []string{\n\tBillStatusOpen,\n}")],
     SELECT),
    # 双事实源漂移：SQL 里抄一份字面量 + 催收集后来长一格 ⇒ 新那一格静默不催。
    # 单摘任何一边都证不到这一句（字面量与集合今天恰好相等时是等价变异），
    # 所以这一格是**两文件叠打**：注完之后 model 说三格、SQL 只捞两格。
    ("R02", "SQL 抄成字面量之后催收集扩容 ⇒ 两处各说各话", "go", "repo", REPOF,
     [('Where("status IN ?", model.BillStatusesChased).\n\t\tWhere("due_at IS NOT NULL AND due_at < ?", cutoff).',
       'Where("status IN (\'open\',\'partial\')").\n\t\tWhere("due_at IS NOT NULL AND due_at < ?", cutoff).'),
      (MODEL, "var BillStatusesChased = []string{\n\tBillStatusOpen, BillStatusPartial,\n}",
       "var BillStatusesChased = []string{\n\tBillStatusOpen, BillStatusPartial, BillStatusVoided,\n}")],
     FOLLOW),
    ("R03", "cutoff 比较从严格早于改成不早于 ⇒ 宽限期最后一天就被催", "go", "repo", REPOF,
     [('Where("due_at IS NOT NULL AND due_at < ?", cutoff).',
       'Where("due_at IS NOT NULL AND due_at <= ?", cutoff).')],
     SELECT),
    ("R04", "多取一行的 +1 摘掉 ⇒ 截断永远读不出来", "go", "repo", REPOF,
     [("\t\tLimit(limit + 1).\n", "\t\tLimit(limit).\n")],
     ORDER),
    ("R05", "截断判据 > 写成 >= ⇒ 每一轮都谎报还有没看完的", "go", "repo", REPOF,
     [("\ttruncated := len(rows) > limit\n", "\ttruncated := len(rows) >= limit\n")],
     ORDER),
    ("R06", "排序方向反过来 ⇒ 封顶精确砍掉欠得最久的那批", "go", "repo", REPOF,
     [('Order("due_at ASC, id ASC").', 'Order("due_at DESC, id ASC").')],
     ORDER),
    ("R07", "次级排序键摘掉 ⇒ 同账期的两张单留下哪张由物理序决定", "go", "repo", REPOF,
     [('Order("due_at ASC, id ASC").', 'Order("due_at ASC").')],
     ORDER),
    ("R08", "封顶值非法判据 <=0 写成 <0 ⇒ limit=0 变成一次全表捞", "go", "repo", REPOF,
     [("\tif limit <= 0 {\n", "\tif limit < 0 {\n")],
     LIMIT),
    ("R09", "取回多一行却不截断 ⇒ 一轮多发一条", "go", "repo", REPOF,
     [("\tif truncated {\n\t\trows = rows[:limit]\n\t}", "\tif truncated {\n\t}")],
     ORDER),
    ("R10", "缺句柄读成\"今天没人逾期\"（而不是出声）", "go", "repo", REPOF,
     [("func (r *billRepo) ScanOverdue(ctx context.Context, cutoff time.Time, limit int) (*BillOverdueScan, error) {\n\tif err := r.require(); err != nil {\n\t\treturn nil, err\n\t}",
       "func (r *billRepo) ScanOverdue(ctx context.Context, cutoff time.Time, limit int) (*BillOverdueScan, error) {\n\tif err := r.require(); err != nil {\n\t\treturn &BillOverdueScan{}, nil\n\t}")],
     NOTEMPTY),
    ("R11", "查询失败被读成空集合 ⇒ 库故障伪装成\"没人逾期\"", "go", "repo", REPOF,
     [("\t\tLimit(limit + 1).\n\t\tFind(&rows).Error; err != nil {\n\t\treturn nil, err\n\t}",
       "\t\tLimit(limit + 1).\n\t\tFind(&rows).Error; err != nil {\n\t\treturn &BillOverdueScan{}, nil\n\t}")],
     NOTEMPTY),
    ("R12", "账期未定的计数谓词写反 ⇒ 把\"没定账期\"数成\"定了账期\"", "go", "repo", REPOF,
     [('\t\tWhere("due_at IS NULL").\n', '\t\tWhere("due_at IS NOT NULL").\n')],
     SELECT),
    ("R13", "账期未定那一趟计数根本不发 ⇒ 视图里这批单消失", "go", "repo", REPOF,
     [("\tvar undated int64\n\tif err := r.db.WithContext(ctx).Model(&model.Bill{}).\n\t\tWhere(\"status IN ?\", model.BillStatusesChased).\n\t\tWhere(\"due_at IS NULL\").\n\t\tCount(&undated).Error; err != nil {\n\t\treturn nil, err\n\t}\n",
       "\tvar undated int64\n")],
     SELECT),

    # ————— 服务层：一轮催收（runner=svc）—————
    ("S01", "off 档短路那一支摘掉 ⇒ 运营关旗子却仍在扫描", "go", "svc", SVC,
     [("\tif j.mode == RecoveryWorkerModeOff {\n\t\treport.DependencyError = \"worker mode off\"",
       "\tif false && j.mode == RecoveryWorkerModeOff {\n\t\treport.DependencyError = \"worker mode off\"")],
     "TestCollectionOffModeRunsNoRound"),
    ("S02", "半装配的一轮静默成功 ⇒ 缺件与\"今天没单\"同形", "go", "svc", SVC,
     [("\t\treturn report, fmt.Errorf(\"collection job: %s\", report.DependencyError)",
       "\t\treturn report, nil")],
     "TestCollectionJobNilSafety"),
    ("S03", "阶段闸门那一支摘掉 ⇒ 第二把锁失位", "go", "svc", SVC,
     [("if on, reason := j.stages.StageActive(ctx, LTCStageCollection); !on {",
       "if on, reason := j.stages.StageActive(ctx, LTCStageCollection); false && !on {")],
     "TestCollectionStageOffSkipsTheWholeRound"),
    ("S04", "扫描失败不报错 ⇒ 库故障读成一轮空转", "go", "svc", SVC,
     [("\t\treturn report, fmt.Errorf(\"collection job: 逾期扫描失败: %w\", err)",
       "\t\treturn report, nil")],
     "TestCollectionScanFailureIsReportedNotReadAsQuiet"),
    ("S05", "nil 读数被采信 ⇒ \"没扫到\"与\"扫了个空\"分不开", "go", "svc", SVC,
     [("\tif scan == nil {\n\t\treport.ScanError = \"扫描回了 nil 读数\"",
       "\tif false && scan == nil {\n\t\treport.ScanError = \"扫描回了 nil 读数\"")],
     "TestCollectionNilScanReadingIsRefused"),
    ("S06", "逾期数没抄进报告 ⇒ 端点上的 overdue 恒 0", "go", "svc", SVC,
     [("\treport.Overdue = len(scan.Overdue)\n", "")],
     "TestCollectionRemindsOncePerCustomerPerRound"),
    ("S07", "账期未定的读数没抄进报告", "go", "svc", SVC,
     [("\treport.Undated = scan.Undated\n", "")],
     "TestCollectionUndatedAndTruncatedSurfaceInTheReport"),
    ("S08", "截断读数没抄进报告 ⇒ 这一轮看到的不是完整世界", "go", "svc", SVC,
     [("\treport.Truncated = scan.Truncated\n", "")],
     "TestCollectionUndatedAndTruncatedSurfaceInTheReport"),
    ("S09", "两格\"扫到但不该催\"的计数串了 ⇒ 排障方向整个反过来", "go", "svc", SVC,
     [("\t\tif bill == nil || bill.DueAt == nil {\n\t\t\treport.SkippedNotOverdue++",
       "\t\tif bill == nil || bill.DueAt == nil {\n\t\t\treport.SkippedNoIdentity++")],
     "TestCollectionBillTheJobWontChaseIsCountedApart"),
    ("S10", "宽限期边界 < 写成 <= ⇒ 第 3 天整被当成还没逾期", "go", "svc", SVC,
     [("\t\tif overdueFor < CollectionGraceDays*24*time.Hour {",
       "\t\tif overdueFor <= CollectionGraceDays*24*time.Hour {")],
     "TestCollectionGraceBoundaryIsTheFirstDayToChase"),
    ("S11", "升级线边界 >= 写成 > ⇒ 第 14 天整还在自动催", "go", "svc", SVC,
     [("\t\tif overdueFor >= CollectionEscalateAfterDays*24*time.Hour {",
       "\t\tif overdueFor > CollectionEscalateAfterDays*24*time.Hour {")],
     "TestCollectionEscalateBoundaryIsTheFirstDayToHandOff"),
    ("S12", "升级之后不 continue ⇒ 同一条单既投待办又自动催", "go", "svc", SVC,
     [("\t\t\tj.escalate(ctx, bill, who, now, report)\n\t\t\tcontinue",
       "\t\t\tj.escalate(ctx, bill, who, now, report)\n\t\t\t_ = bill")],
     ESCALATE),
    ("S13", "取锁失败 fail-open ⇒ 锁服务一抖就重复催款", "go", "svc", SVC,
     # 只摘 `continue` 的那一版是**等价变异**，实测存活：摘掉之后控制流落进紧跟其后的
     # `if !claimed`，而假件在报错时回的就是 claimed=false ⇒ 仍然不发。真 fail-open 的
     # 形状是"把报错当成占到了"，所以注码必须把 claimed 一起翻掉。
     [("\t\t\t\treport.ClaimUnavailable++\n\t\t\t\tcontinue",
       "\t\t\t\treport.ClaimUnavailable++\n\t\t\t\tclaimed = true")],
     "TestCollectionClaimUnavailableIsFailClosed"),
    ("S14", "提醒窗内占不到锁照样催 ⇒ 七天窗形同虚设", "go", "svc", SVC,
     [("\t\t\tif !claimed {\n\t\t\t\treport.RemindersHeld++",
       "\t\t\tif false && !claimed {\n\t\t\t\treport.RemindersHeld++")],
     "TestCollectionDoesNotRemindTwiceWithinWindow"),
    ("S15", "升级窗内占不到锁照样投 ⇒ 三十天窗形同虚设", "go", "svc", SVC,
     [("\tif !claimed {\n\t\treport.EscalationHeld++",
       "\tif false && !claimed {\n\t\treport.EscalationHeld++")],
     "TestCollectionEscalationIsOncePerWindow"),
    ("S16", "shadow 也去占窗 ⇒ 观察档把真发的那批单锁死了", "go", "svc", SVC,
     [("\t\tvar winKey, winToken string\n\t\tif !j.shadow() {",
       "\t\tvar winKey, winToken string\n\t\tif true {")],
     SHADOW),
    ("S17", "enforce 也带 DryRun ⇒ 一条都发不出去而读数全绿", "go", "svc", SVC,
     [("\treq.DryRun = shadow\n", "\treq.DryRun = true\n")],
     "TestCollectionRemindsOverdueBillThroughTheReachSeam"),
    ("S18", "shadow 不带 DryRun ⇒ 观察档真的打扰了客户", "go", "svc", SVC,
     [("\treq.DryRun = shadow\n", "\treq.DryRun = false\n")],
     SHADOW),
    ("S19", "shadow 的预计值并进真值 ⇒ \"到底发出去几条\"没了答案", "go", "svc", SVC,
     [("\t\treport.WouldRemind++\n\t\treturn", "\t\treport.WouldRemind++")],
     SHADOW),
    ("S20", "累计已催不计数 ⇒ 端点恒回 0（草稿那一课）", "go", "svc", SVC,
     [("\treport.Reminded++\n\tj.remindedAll.Add(1)", "\treport.Reminded++")],
     "TestCollectionRoundCountersAreCumulative"),
    ("S21", "累计已升级不计数", "go", "svc", SVC,
     [("\t\treport.Escalated++\n\t\tj.escalatedAll.Add(1)", "\t\treport.Escalated++")],
     "TestCollectionEscalatedTotalIsCumulative"),
    ("S22", "退订不单独记 ⇒ 与渠道抖动混成一格", "go", "svc", SVC,
     [("\tcase errors.Is(err, ErrDoNotContact):\n\t\treport.BlockedByDNC++\n", "")],
     BLOCKED),
    ("S23", "闸门拒不单独记 ⇒ 看不出要补授权", "go", "svc", SVC,
     [("\tcase errors.Is(err, ErrReachApprovalDenied):\n\t\treport.BlockedByApproval++\n", "")],
     BLOCKED),
    ("S24", "冷却不单独记 ⇒ 看不出下一轮再来就行", "go", "svc", SVC,
     [("\tcase errors.Is(err, ErrReachCooldown):\n\t\treport.BlockedByCooldown++\n", "")],
     BLOCKED),
    ("S25", "兜底那一格没了 ⇒ 未知失败静默消失", "go", "svc", SVC,
     [("\tdefault:\n\t\treport.Failed++\n\t}", "\t}")],
     # 原先点名 BLOCKED，实测那条不会红：它喂的三种错各有自己的 case，永远走不到 default。
     # 真会走进兜底臂的是"渠道回了个不认识的东西"，只有 SENDFAIL 那条喂这种错。
     SENDFAIL),
    ("S26", "退订也还窗 ⇒ 已退订的人每天被重打扰", "go", "svc", SVC,
     [("\tif errors.Is(sendErr, ErrDoNotContact) {\n\t\treturn\n\t}",
       "\tif false && errors.Is(sendErr, ErrDoNotContact) {\n\t\treturn\n\t}")],
     BLOCKED),
    ("S27", "shadow 的预计升级并进真值 ⇒ 观察档真的投了待办", "go", "svc", SVC,
     [("\t\treport.WouldEscalate++\n\t\treturn", "\t\treport.WouldEscalate++")],
     SHADOW),
    ("S28", "待办投递失败不处理 ⇒ 占着窗静默消失三十天", "go", "svc", SVC,
     [("\tif serr != nil {\n\t\t// 待办没落成", "\tif serr == nil {\n\t\t// 待办没落成")],
     "TestCollectionEscalateSubmitFailureReleasesWindow"),
    ("S29", "created/复用 判反 ⇒ 复用的那条被算成新增", "go", "svc", SVC,
     [("\tif created {\n\t\treport.Escalated++", "\tif !created {\n\t\treport.Escalated++")],
     "TestCollectionTaskReuseIsNotAFailure"),
    ("S30", "待办钉到商机上 ⇒ 同商机两张逾期单共用一条待办", "go", "svc", SVC,
     [("\t\tSubjectID:  bill.ID,", "\t\tSubjectID:  bill.OpportunityID,")],
     ESCALATE),
    ("S31", "待办跳转指回报价 ⇒ 接手的人点开的是另一件事", "go", "svc", SVC,
     [("\t\tPayloadRef: collectionBillPayloadRefPrefix + bill.ID,",
       '\t\tPayloadRef: "/api/quote/" + bill.QuoteID,')],
     ESCALATE),
    ("S32", "kind 写错一格 ⇒ 三类分离视图里这条永远不出现", "go", "svc", SVC,
     [("\t\tKind:        model.HumanTaskKindCollectionEscalation,",
       '\t\tKind:        model.HumanTaskKindCollectionEscalation + "x",')],
     ESCALATE),
    ("S33", "标题里没账单号 ⇒ 值班的人还得跳两回去拼是哪张单", "go", "svc", SVC,
     [('Title:      fmt.Sprintf("催收升级：应收单 %s 逾期 %d 天", bill.ID, days),',
       'Title:      fmt.Sprintf("催收升级：逾期 %d 天", days),')],
     ESCALATE),
    ("S34", "待办上的身份抄成 customer_id ⇒ 跨渠道归一后的那个人看不见", "go", "svc", SVC,
     [("\t\tOneID:    who.oneID,", "\t\tOneID:    who.customerID,")],
     ESCALATE),
    ("S35", "待办截止算成此刻 ⇒ 投出去那一刻就已经逾期", "go", "svc", SVC,
     [("\tsla := now.Add(CollectionEscalateResponseWindow)", "\tsla := now")],
     ESCALATE),
    ("S36", "cutoff 改用 AddDate ⇒ 跨时区算出两个逾期定义", "go", "svc", SVC,
     [("\treturn now.Add(-CollectionGraceDays * 24 * time.Hour)",
       "\treturn now.AddDate(0, 0, -CollectionGraceDays)")],
     # 不是 GRACE 那条：夹具用的 FixedZone 永不夏令时 ⇒ Add 与 AddDate 在它身上给出同一瞬间
     # （那条判据本身是死锁，用例里已写明）。有牙的是换真回拨时区并把期望写死成 UTC 时刻的那条。
     ABSWINDOW),
    ("S37", "没有商机来路的单被读成\"没身份\"而不是\"缺链路\"", "go", "svc", SVC,
     [("\tif strings.TrimSpace(bill.OpportunityID) == \"\" {",
       "\tif false && strings.TrimSpace(bill.OpportunityID) == \"\" {")],
     "TestCollectionBillWithoutQuoteLineageIsAFailure"),
    ("S38", "商机读不到被记成取数失败 ⇒ 两种\"没身份\"混成一格", "go", "svc", SVC,
     [("\tif opp == nil {\n\t\treturn none, nil\n\t}",
       "\tif opp == nil {\n\t\treturn none, errors.New(\"collection: 商机读不到\")\n\t}")],
     MISSINGID),
    ("S39", "身份判空 && 写成 || ⇒ 只有 one_id 的单被丢掉", "go", "svc", SVC,
     [("func (i collectionIdentity) empty() bool { return i.customerID == \"\" && i.oneID == \"\" }",
       "func (i collectionIdentity) empty() bool { return i.customerID == \"\" || i.oneID == \"\" }")],
     # 不是 MISSINGID 那条：它喂的是"两个键都空"的单，&& 与 || 在它身上同形（实测红了的是 FALLBACK）。
     FALLBACK),
    ("S40", "身份键摘掉 fallback ⇒ 两笔钱并进同一条催款信", "go", "svc", SVC,
     [("\tif i.oneID != \"\" {\n\t\treturn i.oneID\n\t}\n\treturn i.customerID\n}",
       "\treturn i.oneID\n}")],
     FALLBACK),
    ("S41", "逾期天数改成四舍五入 ⇒ 显示说越了线而判据还没", "go", "svc", SVC,
     [("\treturn int(now.Sub(*bill.DueAt) / (24 * time.Hour))",
       "\treturn int((2*now.Sub(*bill.DueAt) + 24*time.Hour) / (2 * 24 * time.Hour))")],
     "TestCollectionOverdueDaysDisplayFloorsRatherThanRounds"),
    ("S42", "响应窗从 3 天漂到 3 小时", "go", "svc", SVC,
     [("CollectionEscalateResponseWindow = 3 * 24 * time.Hour",
       "CollectionEscalateResponseWindow = 3 * time.Hour")],
     CALIBRE),
    ("S43", "宽限期漂到 0 ⇒ 账期一过就催（款还在路上）", "go", "svc", SVC,
     [("\tCollectionGraceDays = 3", "\tCollectionGraceDays = 0")],
     ABSWINDOW),
    ("S44", "升级线漂到宽限期之内 ⇒ 两档互斥失位", "go", "svc", SVC,
     [("\tCollectionEscalateAfterDays = 14", "\tCollectionEscalateAfterDays = 2")],
     "TestCollectionLadderThresholdsAreOrdered"),
    ("S45", "Available 合取少一项 ⇒ 半装配被报成可用", "go", "svc", SVC,
     [("return j != nil && j.bills != nil && j.opps != nil && j.reach != nil && j.tasks != nil && j.stages != nil",
       "return j != nil && j.bills != nil && j.opps != nil && j.reach != nil && j.stages != nil")],
     "TestCollectionJobNilSafety"),
    ("S46", "LastReport 交出活的指针 ⇒ 读侧读到写一半的一轮", "go", "svc", SVC,
     [("\tcopied := *j.last\n\treturn &copied", "\treturn j.last")],
     DETACHED),
    ("S47", "收尾那一轮不发布 ⇒ 端点永远看不到完成的一轮", "go", "svc", SVC,
     [("\tj.logRound(report)\n\tj.setLast(report)", "\tj.logRound(report)")],
     HALFROUND),
    ("S48", "startedFlag 不置 ⇒ 观测面把在跑的读成没跑", "go", "svc", SVC,
     [("\t\tj.startedFlag = true\n", "")],
     "TestCollectionStartRaisesTheIntervalFloor"),
    ("S49", "间隔下限漂到 1 分钟 ⇒ 两轮抢同一张单，读数全是 held", "go", "svc", SVC,
     [("\tcollectionJobMinInterval = 30 * time.Minute", "\tcollectionJobMinInterval = 1 * time.Minute")],
     CALIBRE),
    ("S50", "默认单轮封顶漂到 500 ⇒ 积压那天把出站队列打满", "go", "svc", SVC,
     [("\tcollectionJobDefaultBatch    = 20", "\tcollectionJobDefaultBatch    = 500")],
     CALIBRE),
    ("S51", "默认轮询间隔漂到 1 小时", "go", "svc", SVC,
     [("\tcollectionJobDefaultInterval = 6 * time.Hour", "\tcollectionJobDefaultInterval = 1 * time.Hour")],
     CALIBRE),
    ("S52", "提醒窗漂到 0 ⇒ 每一轮都再催一次", "go", "svc", SVC,
     [("\tCollectionReminderWindow = 7 * 24 * time.Hour", "\tCollectionReminderWindow = 0")],
     CALIBRE),
    ("S53", "主开关变量名漂一个字母", "go", "svc", SVC,
     [('CollectionJobFlagEnv = "FF_LTC_COLLECTION_JOB"', 'CollectionJobFlagEnv = "FF_LTC_COLLECTION_JOBS"')],
     CALIBRE),
    ("S54", "两把锁前缀合成一把 ⇒ 催过与升过互相覆盖", "go", "svc", SVC,
     [('collectionEscalateKeyPrefix = "mtk:collection:escalate:"', 'collectionEscalateKeyPrefix = "mtk:collection:"')],
     CALIBRE),
    ("S55", "shadow 判定写反成 enforce", "go", "svc", SVC,
     [("func (j *CollectionJob) shadow() bool { return j.mode == RecoveryWorkerModeShadow }",
       "func (j *CollectionJob) shadow() bool { return j.mode == RecoveryWorkerModeEnforce }")],
     SHADOW),
    ("S56", "待办跳转前缀漂一个字母", "go", "svc", SVC,
     [('collectionBillPayloadRefPrefix = "/api/bill/"', 'collectionBillPayloadRefPrefix = "/api/bills/"')],
     ESCALATE),
    # S13 的对偶：提醒那条路的 fail-open 由 S13 钉，升级这条路有自己的一处 err 分支，
    # 摘掉哪一处都只影响另一条路 ⇒ 两格必须分开（一处符号两处消费，合格就是假覆盖）。
    ("S57", "升级取锁失败 fail-open ⇒ 锁服务一抖就多投一条待办", "go", "svc", SVC,
     [("\t\treport.ClaimUnavailable++\n\t\treturn\n\t}",
       "\t\treport.ClaimUnavailable++\n\t\tclaimed = true\n\t}")],
     ESCCA),
    # 发布位与读取位各有一件抄件，两件是两处独立的代码、两把独立的刀（见 setLast 的注释：
    # 锁只订得住指针，订不住它指向的那格）。
    ("S58", "发布不抄件 ⇒ RunOnce 的返回值与端点读的是同一格", "go", "svc", SVC,
     [("\tcopied := *r\n\tj.mu.Lock()\n\tj.last = &copied\n\tj.mu.Unlock()",
       "\tj.mu.Lock()\n\tj.last = r\n\tj.mu.Unlock()")],
     DETACHED),
    ("S59", "宽限期按小时算 ⇒ 三天宽限变成三小时", "go", "svc", SVC,
     [("\treturn now.Add(-CollectionGraceDays * 24 * time.Hour)",
       "\treturn now.Add(-CollectionGraceDays * time.Hour)")],
     GRACE),

    # ————— 装配层（runner=app）—————
    ("W01", "快照把宽限期与升级线两格抄反", "go", "app", WIRE,
     [("\t\tGraceDays:         service.CollectionGraceDays,\n\t\tEscalateAfterDays: service.CollectionEscalateAfterDays,",
       "\t\tGraceDays:         service.CollectionEscalateAfterDays,\n\t\tEscalateAfterDays: service.CollectionGraceDays,")],
     CALIBRE_APP),
    ("W02", "快照里的旗子名抄成了封顶变量名 ⇒ 运维改错变量", "go", "app", WIRE,
     [("\t\tFlagEnv:           service.CollectionJobFlagEnv,", "\t\tFlagEnv:           service.CollectionJobBatchEnv,")],
     CALIBRE_APP),
    ("W03", "闸门检查这一格没报 ⇒ 运维不知道外发受不受约束", "go", "app", WIRE,
     [("\tsnap.ReachGateChecked = true\n", "")],
     GATEAPP),
    ("W04", "闸门读数谎报为真", "go", "app", WIRE,
     [("\tsnap.ReachGated = rt.reachGated\n", "\tsnap.ReachGated = true\n")],
     GATEAPP),
    ("W05", "Running 谎报 ⇒ off 档看起来在跑", "go", "app", WIRE,
     [("\tsnap.Running = rt.job.Running()\n", "\tsnap.Running = true\n")],
     OFFAPP),
    ("W06", "Assembled 恒假 ⇒ 装了也报没装", "go", "app", WIRE,
     [("\tsnap.Assembled = true\n", "")],
     OFFAPP),
    ("W07", "mode 读回 env 而不是那台实例 ⇒ 下限被抬过之后两处各说各话", "go", "app", WIRE,
     [("\tsnap.Mode = string(rt.job.Mode())\n", "\tsnap.Mode = string(service.CollectionJobModeFromEnv())\n")],
     "TestGetCollectionSnapshot_ReadsTheMountedInstanceNotTheEnv"),
    ("W08", "最近一轮读数不抄 ⇒ 端点答不出上一轮干了什么", "go", "app", WIRE,
     [("\tsnap.Last = rt.job.LastReport()\n", "")],
     TOTALS_APP),
    ("W09", "重复装配不停上一份 ⇒ 两台各自扫同一批逾期单", "go", "app", WIRE,
     [("\tif prev != nil {\n\t\tprev.job.Stop(context.Background())\n\t}", "\t_ = prev")],
     "TestInitCollectionRuntime_RepeatedInitStopsPreviousInstance"),
    ("W10", "无句柄时不早退 ⇒ 拿着 nil 句柄继续装", "go", "app", WIRE,
     [("\tif db == nil {\n\t\tlogger.Warnf(\"[collection] ⚠️ 无 DB 句柄", "\tif false && db == nil {\n\t\tlogger.Warnf(\"[collection] ⚠️ 无 DB 句柄")],
     NILAPP),
    ("W11", "装好了却不起协程 ⇒ 实例在、每轮永不发生", "go", "app", WIRE,
     [("\trt := &CollectionRuntime{job: job, reachGated: gated, reachNote: collectionGateNote(gated)}\n\tjob.Start(context.Background())",
       "\trt := &CollectionRuntime{job: job, reachGated: gated, reachNote: collectionGateNote(gated)}")],
     SHADOWAPP),
    ("W12", "闸门没挂上也报挂上（装配点谎报）", "go", "app", WIRE,
     [("\tgated := AttachReachGate(collectionReach)", "\tgated := true\n\t_ = collectionReach")],
     GATEAPP),
    ("W13", "off 档也起协程 ⇒ 运营关旗子这条腿照样跑", "go", "app", SVC,
     [("\t\tif j.mode == RecoveryWorkerModeOff {\n\t\t\tlogger.Infof(\"[CollectionJob] %s=off ⇒ 未启动",
       "\t\tif false && j.mode == RecoveryWorkerModeOff {\n\t\t\tlogger.Infof(\"[CollectionJob] %s=off ⇒ 未启动")],
     OFFAPP),
    ("W14", "五把依赖少接一把（待办投递口）", "go", "app", WIRE,
     # 第一版注码是"把整块实参删掉"，实测判为 BUILD-BROKEN（少一个实参编不过）——那证不到
     # 任何事。真在现网发生得了的形状是"接了一把 nil 进去"：接口参数允许 nil，编译期无人报警。
     [("\t\tservice.NewHumanTaskService(\n\t\t\trepository.NewHumanTaskRepositoryWithDB(db),\n\t\t\tservice.GlobalConfigParam(),\n\t\t),\n",
       "\t\tnil,\n")],
     OFFAPP),
    # Available 这一格是端点上"没开 / 开了但缺件"两态的唯一分界（视图层 Rt02 就靠它选一臂）。
    ("W15", "快照里的 Available 取反 ⇒ 装齐了报缺件、缺件了报装齐", "go", "app", WIRE,
     [("\tsnap.Available = rt.job.Available()", "\tsnap.Available = !rt.job.Available()")],
     OFFAPP),

    # ————— 视图层（runner=route）—————
    ("Rt01", "未装配那一臂摘掉 ⇒ \"没装\"与\"装了但缺件\"同形", "go", "route", ROUTES,
     [("\tcase !snap.Assembled:", "\tcase false && !snap.Assembled:")],
     VIEW),
    ("Rt02", "缺件那一臂摘掉", "go", "route", ROUTES,
     [("\tcase !snap.Available:", "\tcase false && !snap.Available:")],
     VIEW),
    ("Rt03", "旗子 off 那一臂摘掉 ⇒ 运营选的状态被读成故障", "go", "route", ROUTES,
     [("\tcase snap.Mode == string(service.RecoveryWorkerModeOff):",
       "\tcase false && snap.Mode == string(service.RecoveryWorkerModeOff):")],
     VIEW),
    ("Rt04", "协程没起那一臂摘掉", "go", "route", ROUTES,
     [("\tcase !snap.Running:", "\tcase false && !snap.Running:")],
     VIEW),
    ("Rt05", "阶段没开那一臂摘掉 ⇒ 第二把锁失位", "go", "route", ROUTES,
     [("\tcase !snap.StageOn:", "\tcase false && !snap.StageOn:")],
     VIEW),
    ("Rt06", "shadow 那一臂摘掉 ⇒ 观察档看起来在真发", "go", "route", ROUTES,
     [("\tcase snap.Mode == string(service.RecoveryWorkerModeShadow):",
       "\tcase false && snap.Mode == string(service.RecoveryWorkerModeShadow):")],
     VIEW),
    # 顺序格：三拍换两臂的位置（中间过一根哨兵），注完之后"阶段没开"先于"旗子 off"报。
    ("Rt07", "两臂优先级互换 ⇒ 该去开旗子的时候让人去改 ltc.config", "go", "route", ROUTES,
     [("\tcase snap.Mode == string(service.RecoveryWorkerModeOff):\n\t\tblocker, note = CollectionBlockerModeOff,",
       "\tcase snap.Mode == \"___SWAP___\":\n\t\tblocker, note = CollectionBlockerModeOff,"),
      ("\tcase !snap.StageOn:\n\t\tblocker, note = CollectionBlockerStageOff,",
       "\tcase snap.Mode == string(service.RecoveryWorkerModeOff):\n\t\tblocker, note = CollectionBlockerStageOff,"),
      ("\tcase snap.Mode == \"___SWAP___\":\n\t\tblocker, note = CollectionBlockerModeOff,",
       "\tcase !snap.StageOn:\n\t\tblocker, note = CollectionBlockerModeOff,")],
     VIEW),
    ("Rt08", "will_send_now 判据取反 ⇒ 端点说的与判的是两件事", "go", "route", ROUTES,
     [('"will_send_now": blocker == "",', '"will_send_now": blocker != "",')],
     VIEW),
    ("Rt09", "last 那一格恒不给 ⇒ 上一轮发生了什么读不到", "go", "route", ROUTES,
     [("\tif snap.Last != nil {\n\t\tv[\"last\"] = snap.Last\n\t}", "\tif false && snap.Last != nil {\n\t\tv[\"last\"] = snap.Last\n\t}")],
     CONDKEY),
    ("Rt10", "空值也照给 ⇒ 读侧分不清\"没话说\"与\"说了个空\"", "go", "route", ROUTES,
     [("\tif snap.UnassembledHint != \"\" {", "\tif true {")],
     CONDKEY),
    ("Rt11", "六格读数里少抄一格 ⇒ 运维在端点上找不到它", "go", "route", ROUTES,
     [('\t\t"stage_on":            snap.StageOn,\n', '')],
     COND),
    ("Rt12", "blocker 取值改名（前端与手册按串检索）", "go", "route", ROUTES,
     [('CollectionBlockerNotAssembled      = "not_assembled"', 'CollectionBlockerNotAssembled      = "no_collection"')],
     "TestCollectionBlockerStringsAreTheAPIClientContract"),
    ("Rt13", "催收端点从 admin 组里摘掉", "go", "route", ROUTES,
     [('\tadmin.GET("/collection", handleCollectionStatusGet)\n', "")],
     ENDPOINT),

    # ————— 启动路径（runner=cmd）—————
    ("CMD1", "催收腿整条不装（编译过、用例全绿、没人被催）", "go", "cmd", MAIN,
     [("\tif collectionJob := app.InitCollectionRuntime(db.GetDB()); collectionJob != nil {\n\t\tdefer collectionJob.Stop(context.Background())\n\t}\n\n", "")],
     STARTUP),
    ("CMD2", "装了却没挂 Stop ⇒ 关停时这一台还在发下一轮", "go", "cmd", MAIN,
     [("\t\tdefer collectionJob.Stop(context.Background())\n", "")],
     STARTUP),
    ("CMD3", "装配挪到 router.Setup 之前 ⇒ 外发闸门静默不挂", "go", "cmd", MAIN,
     [("\tif collectionJob := app.InitCollectionRuntime(db.GetDB()); collectionJob != nil {\n\t\tdefer collectionJob.Stop(context.Background())\n\t}\n\n", ""),
      ("\trouter.Setup(r, db.GetDB())\n",
       "\tif collectionJob := app.InitCollectionRuntime(db.GetDB()); collectionJob != nil {\n\t\tdefer collectionJob.Stop(context.Background())\n\t}\n\trouter.Setup(r, db.GetDB())\n")],
     STARTUP),

    # ————— 台账门（runner=gate：只跑 check-unwired-assets.sh）—————
    ("G01", "台账 25a：装配点改名（grep 会瞎），门必须报漂移", "gate", "gate", WIRE,
     [("job := service.NewCollectionJob(", "job := service.NewCollectionJobForP703(")],
     "催收任务的装配入口"),
    ("G02", "台账 25b：摘掉 main.go 那一行，门必须报漂移", "gate", "gate", MAIN,
     [("\tif collectionJob := app.InitCollectionRuntime(db.GetDB()); collectionJob != nil {\n\t\tdefer collectionJob.Stop(context.Background())\n\t}\n\n", "")],
     "催收腿在启动路径上的装配点"),
    ("G03", "台账 25c：摘掉挂载那一行，门必须报漂移", "gate", "gate", ROUTES,
     [('\tadmin.GET("/collection", handleCollectionStatusGet)\n', "")],
     "催收观测端点的挂载入口"),
    ("G04", "台账 25d：闸门装配点那个变量改名，门必须报漂移（15b 的对偶）", "gate", "gate", WIRE,
     [("\tcollectionReach := service.NewProactiveReachService(db, nil)",
       "\tcollReach := service.NewProactiveReachService(db, nil)"),
      ("\tservice.BindProactiveReachSenders(collectionReach, db)",
       "\tservice.BindProactiveReachSenders(collReach, db)"),
      ("\tgated := AttachReachGate(collectionReach)", "\tgated := AttachReachGate(collReach)"),
      ("\t\tcollectionReach,", "\t\tcollReach,")],
     "催收这条外发路径的审批闸门装配点"),
]

TALLY = ("KILLED", "SURVIVED", "RED-UNNAMED", "BUILD-BROKEN", "ENV-BROKEN", "NO-RUN")


def md5_bytes(p: Path) -> str:
    return hashlib.md5(p.read_bytes()).hexdigest()


def read(path: Path) -> str:
    return path.read_text(encoding="utf-8")


def sub_once(text: str, old: str, new: str, tag: str) -> str:
    n = text.count(old)
    if n != 1:
        raise SystemExit(f"{tag} 锚点命中 {n} 次（要求恰好 1 次）：{old[:90]!r}")
    return text.replace(old, new, 1)


def cell_rels(cell) -> set[str]:
    """本格会写到盘上的文件集合（默认 cell[4]，三元组点名谁就打谁）。"""
    return {cell[4]} | {p[0] for p in cell[5] if len(p) == 3}


def apply_cell(originals: dict[str, str], cell) -> dict[str, str]:
    """把一格的全部注码**在内存里叠完**再返回，中途任何一锚不命中都不留下半个字节。"""
    code, _desc, _kind, _runner, rel, pairs, _expect = cell
    out: dict[str, str] = {}
    for pair in pairs:
        old_rel, old, new = (rel, pair[0], pair[1]) if len(pair) == 2 else pair
        base = out.get(old_rel, originals[old_rel])
        out[old_rel] = sub_once(base, old, new, f"{code}@{old_rel.split('/')[-1]}")
    return out


def prepare(dst: Path) -> Path:
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
        raise SystemExit("checkout 失败：" + (b.stdout + b.stderr)[-400:])
    for rel in MY_ANCHOR_KEYS:
        src = clone / rel
        if not src.exists():
            raise SystemExit(f"锚点所在文件在 HEAD 里不存在：{rel}")
    for rel, needle in MY_ANCHORS:
        n = read(clone / rel).count(needle)
        if n != 1:
            raise SystemExit(f"装配锚点在 HEAD 的 {rel} 里命中 {n} 次（要恰好 1 次）：{needle!r}\n"
                             "⇒ 克隆拿到的不是本卡提交的那棵树，或那一处在 HEAD 里被写了两遍。")
    in_head = 0
    drifted: list[str] = []
    for rel in NEW_FILES:
        src = ROOT / rel
        if not src.exists():
            raise SystemExit(f"覆盖源缺失：{src}")
        tgt = clone / rel
        if tgt.exists():
            # 提交之后文件已在 HEAD 里：这时**测 HEAD 的字节**，别再拿工作树覆盖 ——
            # 共享工作树里随时压着并行泳道的未提交改动，覆盖进去就等于把他们的改动算进我的判据。
            in_head += 1
            if md5_bytes(src) != md5_bytes(tgt):
                drifted.append(rel)
            continue
        tgt.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(src, tgt)
    head = subprocess.run(["git", "rev-parse", "--short", "HEAD"], cwd=clone,
                          capture_output=True, text=True).stdout.strip() or "?"
    print(f"基线字节：克隆 HEAD `{head}`；本卡文件已在 HEAD {in_head}/{len(NEW_FILES)}"
          + ("" if in_head == len(NEW_FILES) else "（余下从工作树取，属未提交态）")
          + f"；装配锚点 {len(MY_ANCHORS)} 处各命中 1 次")
    if drifted:
        raise SystemExit("这些文件 HEAD 里有、工作树里被改过且**未提交**，电池测的是 HEAD："
                         + ", ".join(drifted) + "\n先提交这一格再看电池结论。")
    hostenv = ROOT / US / ".env"
    if hostenv.exists():
        shutil.copy2(hostenv, clone / US / ".env")
    return clone


MY_ANCHOR_KEYS = tuple(sorted({rel for rel, _ in MY_ANCHORS}))


def env_for(root: Path) -> dict:
    env = dict(os.environ)
    env.setdefault("GIN_MODE", "test")
    env.setdefault("GOFLAGS", "-mod=mod")
    envf = root / ".env"
    if envf.exists():
        for line in read(envf).splitlines():
            if line.startswith("POSTGRES_PASSWORD=") and "POSTGRES_TEST_PASSWORD" not in env:
                env["POSTGRES_TEST_PASSWORD"] = line.split("=", 1)[1].strip()
    env.setdefault("POSTGRES_TEST_PORT", "8232")
    return env


def go_run(clone: Path, runner: str):
    pkg, run = RUNNERS[runner]
    root = clone / US
    try:
        p = subprocess.run(["go", "test", pkg, "-run", run, "-count=1", "-v", "-timeout", "25m"],
                           cwd=root, capture_output=True, text=True, timeout=1800, env=env_for(root))
        out, rc = p.stdout + p.stderr, p.returncode
    except subprocess.TimeoutExpired as e:
        out, rc = (e.stdout or "") + (e.stderr or ""), -9
    out = ANSI.sub("", out)
    killed = sorted(set(re.findall(r"^    --- FAIL: ([^\s/]+)", out, re.M)) |
                    set(re.findall(r"^--- FAIL: (\S+)", out, re.M)))
    passed = len(re.findall(r"^--- PASS: (\S+)", out, re.M)) + \
        len(re.findall(r"^    --- PASS: (\S+)", out, re.M))
    # 守恒式两边都必须是**顶层**口径：`ran` 只数顶层 === RUN，把 `    --- PASS` 算进来
    # 的 passed 不能拿去和它比（凡有子测试的包必然"多算"，那条断言会天天假红）。
    top_pass = len(re.findall(r"^--- PASS: (\S+)", out, re.M))
    ran = len(re.findall(r"^=== RUN\s+(\S+)", out, re.M))
    skipped = len(re.findall(r"^--- SKIP: (\S+)", out, re.M))
    return rc, killed, ran, skipped, passed, top_pass, out


def gate_run(clone: Path):
    r = subprocess.run(["bash", "scripts/check-unwired-assets.sh"], cwd=clone,
                       capture_output=True, text=True, timeout=900, env=dict(os.environ))
    return r.returncode, ANSI.sub("", r.stdout + r.stderr)


def red_evidence(out: str, expect: str, limit: int = 9000) -> str:
    """把"为什么红"按用例抽出来 —— 不给尾巴切片。

    `out[-N:]` 在这一族里恰好吞掉排在最前面的那个失败块（用例按字母序跑，先红的那条
    往往就是要点名的那条），而它是这一格里唯一变了的东西。这里先印点名用例的整块正文，
    再印其余红块，最后给出 panic / 收尾行；没红可印时（SURVIVED）只给结论行。
    """
    fails = [m.group(1) for m in re.finditer(r"(?m)^\s*--- FAIL: (\S+)", out)]
    marks = [(m.start(), m.group(1)) for m in re.finditer(r"(?m)^=== RUN\s+(\S+)", out)]
    blocks: list[tuple[int, str]] = []
    for i, (pos, name) in enumerate(marks):
        hit = [f for f in fails if f == name or f.startswith(name + "/")]
        if not hit:
            continue
        end = marks[i + 1][0] if i + 1 < len(marks) else len(out)
        blocks.append((0 if name == expect else 1, out[pos:end].rstrip()))
    blocks.sort(key=lambda b: b[0])
    chunks, used, omitted = [], 0, 0
    for _, b in blocks:
        if used > limit - 400:
            omitted += 1
            continue
        chunks.append(b)
        used += len(b)
    if not marks:  # 台账门那种非 go-test 输出：整段本身就是证据
        return out[-limit:]
    tail = [ln for ln in out.splitlines()
            if ln.startswith("panic:") or ln.startswith("FAIL") or ln.startswith("ok ")
            or ln.startswith("# ") or ln.startswith("\t")]
    text = "\n\n".join(chunks) or "(没有红块可抽：这一格既没 FAIL 也没 build 报错)"
    if omitted:
        text += f"\n（另有 {omitted} 块红因超出篇幅没打印）"
    if tail:
        text += "\n-- 收尾/报错行 --\n" + "\n".join(tail[-25:])
    return text


def classify(rc, killed, ran, skipped, out, control, expect=""):
    if "connection refused" in out or "dial tcp" in out or "no such host" in out:
        return "ENV-BROKEN"
    if "[build failed]" in out or "undefined:" in out or "declared and not used" in out or rc == -9:
        return "BUILD-BROKEN"
    if skipped > 0:
        return "ENV-BROKEN"
    if ran == 0:
        return "NO-RUN"
    if killed:
        # 点名才算杀掉：红了别的不算（那恰好是"这一格没牙"的机器形态）。
        if expect and expect not in killed:
            return "RED-UNNAMED"
        return "KILLED"
    if ran < control:
        return "NO-RUN"
    if rc != 0:
        return "RED-UNNAMED"
    return "SURVIVED"


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--keep", action="store_true")
    ap.add_argument("--clone", default="")
    ap.add_argument("--cells", default="", help="只跑这些代号（逗号分隔）")
    ap.add_argument("--check", action="store_true", help="只校验锚点命中数，不跑用例")
    args = ap.parse_args()

    cells = CELLS
    if args.cells:
        want = [c.strip() for c in args.cells.split(",") if c.strip()]
        cells = [c for c in CELLS if c[0] in set(want)]
        missing = set(want) - {c[0] for c in cells}
        if missing:
            raise SystemExit(f"未知代号：{sorted(missing)}")

    tmp = Path(args.clone or tempfile.mkdtemp(prefix="p703mut-"))
    tmp.mkdir(parents=True, exist_ok=True)
    print(f"私有作业目录：{tmp}")
    clone = prepare(tmp)

    def sweep() -> None:
        """除了 --keep，正常出口与"可复现"的停机出口都把私有克隆带走。

        **md5 不一致那一支刻意不扫**：那份"还原之后还是不对"的字节是唯一证据。
        """
        if not args.keep:
            shutil.rmtree(tmp, ignore_errors=True)

    rels = sorted(set().union(*(cell_rels(c) for c in cells))
                  | set(MY_ANCHOR_KEYS) | set(NEW_FILES))
    files = {rel: clone / rel for rel in rels}
    originals = {rel: read(p) for rel, p in files.items()}
    basemd5 = {rel: md5_bytes(p) for rel, p in files.items()}

    if args.check:
        bad = 0
        for cell in cells:
            try:
                mutated = apply_cell(originals, cell)
            except SystemExit as e:
                bad += 1
                print(f"  ✗ {e}")
                continue
            for rel, t in mutated.items():
                if t == originals[rel]:
                    bad += 1
                    print(f"  ✗ {cell[0]}@{rel} 注码打完了而字节没变（这一格永不开火）")
        print(f"锚点校验：{len(cells)} 格，{bad} 格锚点有问题")
        sweep()
        return 1 if bad else 0

    controls: dict[str, int] = {}
    control_top: dict[str, int] = {}
    for name in sorted({c[3] for c in cells}):
        if name == "gate":
            rc, out = gate_run(clone)
            ok = rc == 0
            controls["gate"] = 0
            print(f"控制组[gate] {'CLEAN' if ok else 'DIRTY'} rc={rc}")
            if not ok:
                print(out[-4000:])
                sweep()
                raise SystemExit("控制组[gate] 不干净：台账门在克隆里就报漂移，"
                                 "后面所有 G* 格的红/绿都不可信")
            continue
        rc, killed, ran, skipped, passed, top_pass, out = go_run(clone, name)
        bad = rc != 0 or skipped or killed
        controls[name] = ran
        control_top[name] = top_pass
        print(f"控制组[{name}] {'DIRTY' if bad else 'CLEAN'} rc={rc} ran={ran} "
              f"PASS={passed} skip={skipped} FAIL={killed}")
        if bad:
            print(out[-4000:])
            sweep()
            raise SystemExit(f"控制组[{name}] 不干净——它下游所有格子的红/绿都不可信")

    problems: list[str] = []
    tally = {k: 0 for k in TALLY}
    killmap: dict[str, set] = {}
    for cell in cells:
        code, desc, kind, runner, _rel, _pairs, expect = cell
        whys: list[str] = []
        try:
            mutated = apply_cell(originals, cell)
        except SystemExit as e:
            problems.append(str(e))
            continue
        for prel, t in mutated.items():
            files[prel].write_text(t, encoding="utf-8")

        if kind == "gate":
            rc, out = gate_run(clone)
            if rc == 0:
                v = "SURVIVED"
            elif expect in out:
                v = "KILLED"
            else:
                v = "RED-UNNAMED"
            killed = [expect] if v == "KILLED" else []
            ran = 1
        else:
            rc, killed, ran, skipped, passed, top_pass, out = go_run(clone, runner)
            v = classify(rc, killed, ran, skipped, out, controls[runner], expect)

        tally[v] += 1
        killmap[code] = set(killed)
        print(f"{code:<5} {desc[:58]:<60} {v} ran={ran} FAIL={len(killed)} rc={rc}")
        # 第二条断言：**顶层 PASS + FAIL == 控制组数**。只看"点名那条红了"会放过
        # 注码让别的用例 panic 中止、二进制提前退出这一种形状。
        if kind == "go" and v in ("KILLED", "RED-UNNAMED") and \
                top_pass + len(killed) != control_top[runner]:
            problems.append(f"{code} 计数不平：顶层 PASS={top_pass} + FAIL={len(killed)} ≠ 控制组的 "
                            f"{control_top[runner]}（有用例被 panic 带走，或压根没参与这一趟）")
            whys.append("计数不平")
        if v == "KILLED" and kind == "go" and ran < controls[runner]:
            problems.append(f"{code} 杀了但 ran={ran}<{controls[runner]}：疑似 panic 中止")
            whys.append("panic 中止")
        if v == "SURVIVED":
            problems.append(f"{code} 存活 = 洞：{desc}")
        if v == "RED-UNNAMED":
            problems.append(f"{code} 红了但没点出 {expect}：{sorted(killed)[:4]}")
        if v in ("BUILD-BROKEN", "ENV-BROKEN", "NO-RUN"):
            problems.append(f"{code} 判为 {v}（不是干净的「杀掉」）：{desc}")
        # 这几类都得靠"为什么"才定得下来。第一版只在 BROKEN 三类打印输出，于是日志里留下
        # 一句"红了但没点出 X"，却没有任何一处看得出**为什么红**（S55 就是这么被读成"计数
        # 不平"的，真因是测试自己 panic 带走了后面的腿）。尾巴切片一律不用：`out[-N:]` 吞掉
        # 的正是排在最前面那条红块（用例按字母序跑，要点名的那条常常最先红）。
        if v in ("SURVIVED", "RED-UNNAMED", "BUILD-BROKEN", "ENV-BROKEN", "NO-RUN") or whys:
            print(f"----- {code} 证据（{v}{'／' + '+'.join(whys) if whys else ''}）-----\n"
                  f"{red_evidence(out, expect)}")

        for prel in mutated:
            files[prel].write_text(originals[prel], encoding="utf-8")
            if md5_bytes(files[prel]) != basemd5[prel]:
                raise SystemExit(f"{code} 还原后 {prel} 的 md5 不一致，停机")

    items = sorted(killmap.items())
    for i in range(len(items)):
        for j in range(i + 1, len(items)):
            if items[i][1] and items[i][1] == items[j][1]:
                print(f"  [同族] {items[i][0]} 与 {items[j][0]} 杀掉的用例集合相同（{len(items[i][1])} 条）")

    print("\n计数：", " ".join(f"{k}={tally[k]}" for k in TALLY), f"格子数={len(cells)}")
    print("全部格子已还原（逐文件 md5 与基线一致）")

    sweep()
    if problems:
        print("\n===== 电池判定：有洞 =====")
        for x in problems:
            print("  ✗", x)
        return 1
    print(f"\n===== 电池判定：{len(cells)} 格逐格核验，无未登记存活 =====")
    return 0


if __name__ == "__main__":
    sys.exit(main())