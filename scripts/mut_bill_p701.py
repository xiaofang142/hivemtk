#!/usr/bin/env python3
"""T-P7-01 账单派生竖（N-6 回款域第一层）的变异电池：71 格逐格验牙（格数逐卡变动，原因见文末第四趟）。

跑在**私有 --shared 克隆**里，克隆内容就是「HEAD 那一棵树」：本卡的 15 个新建文件与四处
装配插入都随 `66964f9e` 进了 HEAD，所以 prepare 不再往克隆里写任何字节，只逐条断言那四处
装配在 HEAD 里**恰好命中一次**（命中 0 ⇒ 拿错了树；命中 2 ⇒ 同一处在 HEAD 里被写了两遍）。
提交前那一版是「HEAD + 只打本卡 hunk」，因为当时工作树的 migrate.go / router.go /
check-unwired-assets.sh 同时压着别的泳道未提交的改动；那套补丁逻辑连同它的幂等守卫
一起被换掉了，换掉的原因记在 `MY_ANCHORS` 上方。
副产品不变：这一趟跑的就是已提交的那份字节，克隆能编过 = HEAD 自洽的证据。

口径（沿用批16/17/18/19x/20b/20d/22/23 与 b61 电池）：
- 控制组**放刀前现测**且**每 runner 各测一次**：共享树下别的泳道随时往同一批包里加用例，
  写死 total 就把别人的用例算成跑断；
- 每格断言 PASS+FAIL==控制组数，红而没点名单里那条腿 ⇒ RED-UNNAMED，不计入杀掉；
- BUILD FAILED / panic / skip>0 / 连不上库 ⇒ BROKEN 类，单独计数且不算杀掉；
- **例外：kind=build 的格子（当前只有 K72）**。它注的是"接口多开一个方法"，而接口增长在
  Go 里根本没有可编译的代理写法（`var _ BillDeriver = (*service.BillService)(nil)` 与测试替身
  同时断），所以"编不过"就是那条锁的本体结论。为避免"任何语法错都算杀掉"的假绿，
  这一类的 expect 是**必须点名的编译错串**（`does not implement BillDeriver (missing method GetBill)`），
  BUILD-BROKEN 而错串不含它 ⇒ 仍判未杀。注码刻意写成返回 `*service.BillView` 而非 `*model.Bill`：
  后者会额外招出一句 `undefined: model`，红因就不唯一了。
  `TestBillController_SeamIsExactlyTheDocumentedPair`（反射数方法集合）因此永远不会成为某格的
  "杀掉者"——它是给读代码的人看的可读锁，机器判据在 K72。
- 锚点命中必须恰好一次；一格多处注码走 pairs（**内存里叠完一次写盘**，不逐格落盘）；
- 还原后逐文件比 md5，不一致立即停机；
- gate 那几格只跑台账脚本（它本身是个 grep 门，不参与编译），其注码形状在每格 desc 里写明。

首轮（同一批产码，克隆基线 HEAD `cf71ba60`）71 格里活了四格、坏了一格，**五处全是判据侧的**，
逐条记在这里免得下一版重新"发现"一遍：
- K08（json 名 ↔ 列名）活 = 用例自己算列名：把 `gorm:"column"` 与字段名一起改它跟着改 ⇒
  改成从 `schema.NamingStrategy` 取（与 GORM 同一个函数）；
- K92（未装配不挂路由）活 = 夹具自己 new 引擎、绕过 `setupBillRoutes` ⇒ 补一条走真实
  `setup*` 的挂载数断言（route 控制组从 8 变 9，又一次证明控制组不能写死）；
- K38（库故障读成"没有这张单"）活 = 没有一条用例走过 `billOrNil` 的 `err != nil` 那一支 ⇒
  注 cancelled context，断"报错且不回 nil 行"；
- K61（请求体不封顶）活 = **探针本身无牙**：那条 20KB 的 `q` 同时被行号长度上限挡下，
  摘掉封顶照样 400 ⇒ 换成"语义合法而字节超限"的体（前导空白在 TrimSpace 后消失），
  并断言红自体积那一支、派生腿一次都没被叫；
- K72（接缝多开读口）判 BROKEN = 见上面 kind=build 那一条。

已知**未覆盖**（写在这里而不是悄悄不留痕）：
- 「已 accepted 先读账单」那条快速路径**没有格子**：把 `if row.Status == accepted` 那一支整个短路，
  结果仍然是对的 —— 第二次派生会撞 uq_bills_quote_row，然后走并发那一支回读同一行，
  Reused 与账单号都不变。少这一层只是白撞一次 23505，不是错。也就是说这一支今天
  没有独立判据；要么给它加一条"重入不产生任何写入"的断言（数 statements 太脆，不做），
  要么承认它是优化。这里按后者登记。
- 台账 23b（`model.Bill{` 在 service/controller 两处消费）的**真**失效形态是"造行搬去别的包"，
  那要同时新增一个包外构造函数；G5 注的是它的一个等价可编译代理（局部类型别名），
  证的是"callpat 会因改名而瞎"这件事本身，不等于覆盖了 23b 的全部坏法。
- gate 格 G4 的注码**故意不要求可编译**：它只回答"这一行的 callpat 收得拢吗"。
  真要写"派生时顺手标已收"，第一道拦的其实是 service 的 port 面（S17 那格已证）。

第二趟（T-P7-02 已提交之后，HEAD `5b92525c`）：整族在提交字节上重跑，**六格出问题，
六处全在判据侧、没有一处是产码的洞**。这一类失效的形状值得留在文件头上：一张卡的改动
会让上一张卡的探针同时出现"锚点找不到"和"注了码而门不报"两种症状，前者响、后者不响。

- K10 锚点 0 次：paid 在 T-P7-01 是终态，T-P7-02 把结清做成 Σ 回款行的函数之后它必须能回退
  （钱被冲销），voided 成为唯一终态 ⇒ 锚点挪到 `BillStatusVoided:  {},`，判据方向不变；
- K39 锚点 0 次 + 用例名失效：接口在 T-P7-02 长出 ListByQuoteID，"收尾那一行"换了人，
  钉方法集的那条用例也从 …Five 改名成 …Six ⇒ 两处一起跟；
- K92/K93 锚点 0 次：挂载那一行由 `(derive)` 变成 `(derive, read)`，早退判据与末行日志同改；
- **G3 存活（这一格是本次唯一"看着像洞"的）**：23a 的 callpat 扫整个 internal/app，
  而 T-P7-02 起那个目录里有**两处** `NewBillRepositoryWithDB(`（派生腿 + 回款腿）。
  摘掉一处而门仍报 WIRED，是这一行**措辞**下的正确答案，不是失守 —— 它的失效形状从
  "少一个调用点"变成了"这一个目录里一个都不剩"。格子的注码跟着改成两文件一刀（驱动为此
  支持 pairs 的三元组形状），而"只掉一条腿"那半边由 K83（派生腿）与 app 包的
  TestInitPaymentRuntime*（回款腿）在行为面上守。留这一句是因为**下一张卡会让这个数字继续
  涨**：任何"目录级存在性锁"被写成单消费方假设的探针时，都会在第二个消费方落地那天变成
  一台永不开火的枪，而它打印的仍是 KILLED/SURVIVED 里那个看着没毛病的状态。
- **G4 存活**：23e 的反向探针（"写出调用 ⇒ 门必须从 unwired 翻成 wired"）的前提被本卡
  **兑现**了 —— 调用点已经写进 payment.go、那一行已经登记成 wired。留着一格永不开火的
  反向探针就是台账门里的假绿来源，挪成正向：拆掉 payment.go 那一行 ⇒ 门必须报"接线被拆掉"。

顺带记两处**没改**的：K72 打印 `ran=0 FAIL=1` 而判 KILLED 是 kind=build 的既有形状（那条
FAIL 是脚本按点名的编译错串代记的，不是 go test 印的用例名）；`--check` 新增一条"注完码
字节没变 ⇒ 这一格永不开火"的自检，正是本次 G3/G4 那种失效的机器形态。

第三趟（T-P7-03 开发中，**提交前**用 `scripts/anchor-preflight.py` 在工作树上先量出来）：
三格锚点 0 次，全在 `DueAt` 这一格上 —— 这一卡把它的口径从"本层写死 nil"改成了
"由调用方给、本层原样落"，并给它加了复合索引标签：

- K07 锚点里没有 gorm 标签：`DueAt *time.Time` 现在带 `index:idx_bills_status_due,priority:2`
  ⇒ 锚点换成整行，注码方向不变（做成非空 ⇒ 零值时间被逾期扫描读成"早过期几千年"）；
- K50 锚点 `DueAt: nil, // …` 整行没了（换成 `in.DueAt`）⇒ 注码改打"把没给账期写成今天到期"，
  判据仍是 `TestBillDeriveLeavesDueAtNull`。**同时补一格 K50b**：坏法从一种变成两种
  （凭空造 / 把给的丢掉），只留前一格的话"永远回 NULL"这条退路没有探针，而催收腿读的正是它；
- K56 锚点是"入参结构以 QuoteRowID 收尾"，现在收尾的是 DueAt ⇒ 锚点挪到 DueAt 那一行，
  expect 跟着改名后的 `TestBillDeriveInputCarriesRowKeyAndOptionalTerm` 走。

这三处都是"上一卡的探针被下一卡的口径改动挪走"，和第二趟那六格同一族。区别只在于
**这次是在提交前发现的** —— 那个时机就是 `anchor-preflight.py` 的全部用途。

同一趟还查出**驱动自己的两处判据没牙**（不是格子的锚点，是数红/绿的那段代码）：

- `classify` 原先只要输出里出现任何 `--- FAIL` 就判 KILLED，**从不核对 expect 那条腿在不在
  名单里**。文件头上写着"红而没点名单里那条腿 ⇒ RED-UNNAMED，不计入杀掉"，而 RED-UNNAMED
  这一类只在"一条用例都没红、rc 却非零"时才产出 —— 也就是说前两趟报的 KILLED 里，
  任何一格都可能是"注码弄红了别人、本格锁的那条腿照旧绿"。现在点名才算杀掉。
  反向核验 7 例（点名红/别人红/没红/build 格不点名/skip/连不上库/少跑到）当场跑过，7/7 符合预期；
- 头注里第二条承诺（`PASS + FAIL == 控制组数`）从来没实现过。补上时踩到一个口径陷阱：
  `ran` 只数顶层 `=== RUN`，而 `passed` 把 `    --- PASS`（子测试）一起算了，
  两边不同口径的守恒式凡有子测试必假红 ⇒ 守恒式两边都用**顶层**计数（`top_pass`），
  分母取控制组现测的那一份而非写死常量。

这两处修完，前两趟的结论里有几格要从"杀掉"降成"待人工看"—— 所以本卡的计划里
**整族 72 格必须在提交后重跑一遍**，而不是只跑新加的三格。

第四趟（取证落点迁进仓库树之后，在**已提交 tip** 上跑 `--check`）：72 格里三格锚点 0 次，
且三格全在 `bill.go` 的 `in.DueAt` 上。归因不是产码回退，而是这张卡的改动**只提交了一半**：
`23dae260`（T-P7-03，collection 泳道）动了 `model/bill.go` 的复合索引标签与 `repository` 的
方法集改名（…Six→…Seven），而 `service/bill.go` 最近一笔仍是 `66964f9e`（T-P7-01）—— 那一层
"账期由调用方给"至今只躺在共享工作树里，从没进过任何一棵 HEAD。
于是 K07/K39 的新锚点在 tip 上成立，K50/K50b/K56 的新锚点不成立 —— tip 落在两版锚点**之间**，
任何一版整文件都不对。常驻电池跑的就是克隆 HEAD 的字节，所以这里按 tip 逐格取值：
K50/K56 回退到与 HEAD 字节匹配的锚点，K50b 整格撤下（它的判据用例
`TestBillDeriveStoresTheDueAtTheCallerGave` 在 HEAD 里**从未存在过**，留着它就是一台
对着不存在的用例开火的枪），三处原地留注释说明该卡落地那天怎么补。格子数因此是 71 而不是 72。
"""
from __future__ import annotations

import argparse
import hashlib
import os
import re
import shutil
import subprocess
import sys
import tempfile
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
# 逐格原始输出落进仓库树：早先只随 stdout 走、由调用方重定向到 /tmp，重启即蒸发 ⇒
# 台账里的读数没有产物可对。目录带趟次戳、不复用；`.gitignore` 需为本轮次开例外。
LOGDIR = ROOT / "docs/superpowers/specs/ledger/logs/P701" / time.strftime("%Y%m%d-%H%M%S")


from redact import scrub  # 落盘前脱敏：常驻产物要过 gitleaks（见 scripts/redact.py 的 why）
def dump(tag, out):
    LOGDIR.mkdir(parents=True, exist_ok=True)
    (LOGDIR / (re.sub(r"[^A-Za-z0-9_.-]", "-", tag) + ".log")).write_text(scrub(out), encoding="utf-8")
US = "user-server"
ANSI = re.compile(r"\x1b\[[0-9;]*m")

# —— 本卡的新建文件（已随 `66964f9e` 进 HEAD；这里只用于"工作树 vs HEAD"的漂移守卫）——
NEW_FILES = [
    f"{US}/internal/model/bill.go",
    f"{US}/internal/model/bill_test.go",
    f"{US}/internal/repository/bill.go",
    f"{US}/internal/repository/bill_test.go",
    f"{US}/internal/service/bill.go",
    f"{US}/internal/service/bill_global.go",
    f"{US}/internal/service/bill_test.go",
    f"{US}/internal/controller/bill.go",
    f"{US}/internal/controller/bill_test.go",
    f"{US}/internal/app/bill_wiring.go",
    f"{US}/internal/app/bill_wiring_test.go",
    f"{US}/internal/router/bill_routes.go",
    f"{US}/internal/router/bill_routes_test.go",
    f"{US}/internal/pkg/db/bill_migration_test.go",
    f"{US}/internal/pkg/db/migrate_test.go",
]

# —— 本卡在既有文件里的四处插入。提交（`66964f9e`）之后它们**已经在 HEAD 里**，
# 所以 prepare 不再打补丁，改成逐条断言"在 HEAD 的这份文件里恰好出现一次"：
# 克隆走偏（拿到别的树）和锚点重复都会在这里当场停住，而不是让 K20/K90/K91/K95/G1/G2
# 去拿一个"命中 2 次"的红当结论。
#
# 这里原来是 (文件, 旧文本, 新文本) 三元组 + 一条幂等守卫 `count(new)==1 and count(old)==0`。
# 那条守卫**永不成立**：每一处的 old 都是 new 的前缀子串（`&model.QuoteLineItem{},` 就写在
# 那两行里），提交后 HEAD 里 old 也计 1 次 ⇒ 守卫判"没打上"⇒ hunk 被打第二遍 ⇒
# `--check` 报 6 格"锚点命中 2 次"。这是本卡第二处"判据自己没牙"，记在这里免得下一版重犯。
MY_ANCHORS = [
    (f"{US}/internal/pkg/db/migrate.go", "\t\t&model.Bill{},\n"),
    (f"{US}/internal/router/router.go", "\tapp.InitBillRuntime(gormDB)\n"),
    (f"{US}/internal/router/router.go", "\t\tsetupBillRoutes(auth)\n"),
    ("scripts/check-unwired-assets.sh", '  "23|账单仓储的装配入口'),
    ("scripts/check-unwired-assets.sh", '  "23|账单行的生产写入点'),
    ("scripts/check-unwired-assets.sh", '  "23|账单派生腿在启动路径上的装配点'),
    ("scripts/check-unwired-assets.sh", '  "23|账单 HTTP 出口的挂载点'),
    ("scripts/check-unwired-assets.sh", '  "23|账单状态跃迁口的生产调用方'),
]

MODEL = f"{US}/internal/model/bill.go"
DBMIG = f"{US}/internal/pkg/db/migrate.go"
REPO = f"{US}/internal/repository/bill.go"
SVC = f"{US}/internal/service/bill.go"
PSVC = f"{US}/internal/service/payment.go"
CTRL = f"{US}/internal/controller/bill.go"
WIRE = f"{US}/internal/app/bill_wiring.go"
PAYWIRE = f"{US}/internal/app/payment_wiring.go"
RTR = f"{US}/internal/router/router.go"
ROUTES = f"{US}/internal/router/bill_routes.go"
LEDGER = "scripts/check-unwired-assets.sh"

RUNNERS = {
    "model": (f"./internal/model/", "^TestBill"),
    "db": (f"./internal/pkg/db/", "^(TestBill|TestAllModels_)"),
    "repo": (f"./internal/repository/", "^TestBillRepository_"),
    "svc": (f"./internal/service/", "^TestBill"),
    "ctrl": (f"./internal/controller/", "^TestBillController_"),
    "app": (f"./internal/app/", "^TestInitBillRuntime"),
    "route": (f"./internal/router/", "^TestBillRoutes_"),
    "gate": ("", ""),
}
CELLS = [
    # ————— 模型层：值域 / 标签 / 跃迁表（runner=model）—————
    ("K01", "状态值域少一格（voided 掉出去 ⇒ 人工收口无处表达）", "go", "model", MODEL,
     [("\tBillStatusOpen, BillStatusPartial, BillStatusPaid, BillStatusVoided,",
       "\tBillStatusOpen, BillStatusPartial, BillStatusPaid,")],
     "TestBillStatusVocabulary"),
    ("K03", "BillStatusKnown 放行一切（值域校验失守）", "go", "model", MODEL,
     [("\tfor _, v := range BillStatuses {\n\t\tif v == s {\n\t\t\treturn true\n\t\t}\n\t}\n\treturn false\n}",
       "\tfor _, v := range BillStatuses {\n\t\tif v == s {\n\t\t\treturn true\n\t\t}\n\t}\n\treturn true\n}")],
     "TestBillStatusVocabulary"),
    ("K04", "版本行键的唯一索引退化成普通索引（AC① 的库级保证没了）", "go", "model", MODEL,
     [('QuoteRowID string `gorm:"type:text;uniqueIndex:uq_bills_quote_row" json:"quote_row_id"`',
       'QuoteRowID string `gorm:"type:text;index" json:"quote_row_id"`')],
     "TestBillKeyedToExactlyOneQuoteVersion"),
    ("K06", "金额列量程从 numeric(14,2) 漂到 (12,2)（与报价行不再同型）", "go", "model", MODEL,
     [('Amount float64 `gorm:"type:numeric(14,2)" json:"amount"`',
       'Amount float64 `gorm:"type:numeric(12,2)" json:"amount"`')],
     "TestBillKeyColumnWidths"),
    ("K07", "账期列做成非空（零值会被逾期扫描读成「早过期几千年」）", "go", "model", MODEL,
     [("\tDueAt *time.Time `gorm:\"index:idx_bills_status_due,priority:2\" json:\"due_at\"`",
       "\tDueAt time.Time `gorm:\"index:idx_bills_status_due,priority:2\" json:\"due_at\"`")],
     "TestBillSchemaShape"),
    ("K08", "json 名漂成驼峰（对外契约与列名分家）", "go", "model", MODEL,
     [('json:"quote_row_id"`', 'json:"quoteRowId"`')],
     "TestBillJSONNamesMatchColumns"),
    ("K09", "跃迁表少一条边（open 不能再作废）", "go", "model", MODEL,
     [("\tBillStatusOpen:    {BillStatusPartial, BillStatusPaid, BillStatusVoided},",
       "\tBillStatusOpen:    {BillStatusPartial, BillStatusPaid},")],
     "TestBillStatusTransitionsAreDeclared"),
    # 注码落在**唯一剩下的那一格终态**上。T-P7-01 交付时这里打的是 `BillStatusPaid: {},`
    # （当时 paid 也是终态）；T-P7-02 把结清判据实现成 Σ 回款行之后 paid 长出了两条回退边
    # （钱被渠道冲销 ⇒ 欠额回来，账单不能还写着已结清），voided 成为唯一终态。
    # 判据的方向没变（"终态有出边等于没有终态"），变的只是哪一格是终态 ⇒ 改锚点不改期望。
    ("K10", "终态开出出边（作废能回退 = 被收口的历史行被人盖掉）", "go", "model", MODEL,
     [("\tBillStatusVoided:  {},", "\tBillStatusVoided:  {BillStatusOpen},")],
     "TestBillStatusTransitionsAreDeclared"),

    # ————— 建表层：真库里的形状（runner=db）—————
    ("K20", "bills 没登记进 allModels()（表没建、代码全对）", "go", "db", DBMIG,
     [("\t\t&model.Bill{},\n", "")],
     "TestBillRegisteredInAllModels"),
    ("K21", "唯一索引改了名（仓储按名分幂等 ⇒ 认错人）", "go", "db", MODEL,
     [("uniqueIndex:uq_bills_quote_row", "uniqueIndex:uq_bills_quote_row_v2")],
     "TestBillUniqueIndexIsOnVersionRowKey"),
    ("K22", "唯一索引建到商机列上（幂等键成了「一张商机一张账单」）", "go", "db", MODEL,
     [('OpportunityID string `gorm:"type:varchar(64);index" json:"opportunity_id"`',
       'OpportunityID string `gorm:"type:varchar(64);uniqueIndex:uq_bills_quote_row" json:"opportunity_id"`')],
     "TestBillUniqueIndexIsOnVersionRowKey"),
    ("K23", "金额量程漂到 numeric(16,2)（库侧读到的是另一个量程）", "go", "db", MODEL,
     [('Amount float64 `gorm:"type:numeric(14,2)" json:"amount"`',
       'Amount float64 `gorm:"type:numeric(16,2)" json:"amount"`')],
     "TestBillAmountSharesRangeWithQuoteLines"),
    ("K24", "账单号列型改成 varchar(64)（与 quotes.id 的 text 不同型）", "go", "db", MODEL,
     [('ID string `gorm:"type:text;primaryKey" json:"id"`',
       'ID string `gorm:"type:varchar(64);primaryKey" json:"id"`')],
     "TestBillIDIsTextPrimaryKey"),

    # ————— 仓储层（runner=repo）—————
    ("K30", "nil 句柄报告 Available()=true（装配回显说谎）", "go", "repo", REPO,
     [("func (r *billRepo) Available() bool { return r != nil && r.db != nil }",
       "func (r *billRepo) Available() bool { return r != nil }")],
     "TestBillRepository_NilHandleFailsLoudly"),
    ("K31", "23505 只看 SQLSTATE 不看索引名（主键冲突被读成「已派生过」）", "go", "repo", REPO,
     [('\treturn strings.Contains(msg, "23505") && strings.Contains(msg, billQuoteRowConstraint)',
       '\treturn strings.Contains(msg, "23505")')],
     "TestBillRepository_DuplicateQuoteRowIsItsOwnError"),
    ("K32", "幂等键的索引名与模型标签不同源", "go", "repo", REPO,
     [('const billQuoteRowConstraint = "uq_bills_quote_row"',
       'const billQuoteRowConstraint = "uq_bills_quote_row_index"')],
     "TestBillRepository_ConstraintNameIsTheOneOnTheModel"),
    ("K33", "状态跃迁丢掉 WHERE 里的状态条件（CAS 退化成无条件改写）", "go", "repo", REPO,
     [('\t\tWhere("id = ? AND status = ?", id, from).', '\t\tWhere("id = ?", id).')],
     "TestBillRepository_UpdateStatusIsCompareAndSet"),
    ("K34", "起点过期被报成「行不存在」（两个假供词之一）", "go", "repo", REPO,
     [("\tif exists {\n\t\treturn ErrBillStatusConflict\n\t}",
       "\tif exists {\n\t\treturn ErrBillNotFound\n\t}")],
     "TestBillRepository_UpdateStatusIsCompareAndSet"),
    ("K35", "Create 不再校验状态值域（库里没有 CHECK，这唯一的落点也没了）", "go", "repo", REPO,
     [("\tif !model.BillStatusKnown(b.Status) {", "\tif false && !model.BillStatusKnown(b.Status) {")],
     "TestBillRepository_StatusMustBeInTheDomain"),
    ("K36", "空 quote_row_id 放行（幂等键为空 ⇒「派没派生过」没有答案）", "go", "repo", REPO,
     [('\tif strings.TrimSpace(b.QuoteRowID) == "" {', '\tif false && strings.TrimSpace(b.QuoteRowID) == "" {')],
     "TestBillRepository_CreateRejectsMissingIdentity"),
    ("K37", "读不到行报成 error（「没有这张单」与「查不了」分家失败）", "go", "repo", REPO,
     [("\tif errors.Is(err, gorm.ErrRecordNotFound) {\n\t\treturn nil, nil\n\t}",
       "\tif errors.Is(err, gorm.ErrRecordNotFound) {\n\t\treturn nil, err\n\t}")],
     "TestBillRepository_ReadsDistinguishMissingFromFailure"),
    ("K38", "库故障读成「还没有账单」（一次抖动就能长出两张应收）", "go", "repo", REPO,
     [("\tif err != nil {\n\t\treturn nil, err\n\t}\n\treturn row, nil",
       "\tif err != nil {\n\t\treturn nil, nil\n\t}\n\treturn row, nil")],
     "TestBillRepository_ReadsDistinguishMissingFromFailure"),
    # 锚点跟接口一起长出来的那一格走：T-P7-02 在对账读侧补了 ListByQuoteID，
    # 接口的**收尾那一行**因此从 GetByQuoteRowID 挪到了它（用例名同步 Five→Six）。
    # 注的形状不变：给凭证表开一条 Delete = 历史能被抹掉。
    ("K39", "接口上多出一条 Delete（凭证表有了抹掉历史的路）", "go", "repo", REPO,
     [("\tListByQuoteID(ctx context.Context, quoteID string) ([]*model.Bill, error)\n}",
       "\tListByQuoteID(ctx context.Context, quoteID string) ([]*model.Bill, error)\n\n"
       "\tDeleteByID(ctx context.Context, id string) error\n}"),
      ("func (r *billRepo) exists(",
       "func (r *billRepo) DeleteByID(ctx context.Context, id string) error {\n\treturn r.require()\n}\n\n"
       "func (r *billRepo) exists(")],
     "TestBillRepository_MethodSetIsExactlyTheDocumentedSeven"),

    # ————— 服务层：四条判据 + 幂等 + 顺序（runner=svc）—————
    ("K40a", "Available 不看报价存储（半装配报告「能派生」）", "go", "svc", SVC,
     [("\treturn s != nil && s.bills != nil && s.quotes != nil",
       "\treturn s != nil && s.bills != nil")],
     "TestBillServiceAvailabilityAndNilSafety"),
    ("K40b", "Available 不看账单存储（另一半各拆一刀）", "go", "svc", SVC,
     [("\treturn s != nil && s.bills != nil && s.quotes != nil",
       "\treturn s != nil && s.quotes != nil")],
     "TestBillServiceAvailabilityAndNilSafety"),
    ("K41", "状态白名单失效（draft/rejected 也能开应收）", "go", "svc", SVC,
     [("\tif row.Status != model.QuoteStatusAccepted && row.Status != model.QuoteStatusSent {",
       "\tif false {")],
     "TestBillDeriveRequiresSentVersion"),
    ("K43", "链位判据的结果丢掉（读了整条链但不据此拦）", "go", "svc", SVC,
     [("\tif err := checkBillChainPosition(row, versions); err != nil {\n\t\treturn nil, err\n\t}\n",
       "\t_ = checkBillChainPosition(row, versions)\n")],
     "TestBillDeriveRejectsSupersededVersion"),
    ("K44", "链位判据本体失效（同一格的两层各拆一刀）", "go", "svc", SVC,
     [("\tif row.Version != latest {", "\tif false && row.Version != latest {")],
     "TestBillDeriveRejectsSupersededVersion"),
    ("K45", "链上只留一次成交这条判据摘掉（一张报价单长出两张应收）", "go", "svc", SVC,
     [("\t\tif v.Status == model.QuoteStatusAccepted {", "\t\tif false && v.Status == model.QuoteStatusAccepted {")],
     "TestBillDeriveRejectsSecondAcceptedVersionInChain"),
    ("K46", "没有行项目也照开（合计 0.00 的「应收」）", "go", "svc", SVC,
     [("\tif len(lines) == 0 {", "\tif false && len(lines) == 0 {")],
     "TestBillDeriveNeedsLines"),
    ("K47", "跃迁失败继续插账单（顺序那一段的判据）", "go", "svc", SVC,
     [("\t\tif err := s.quotes.UpdateStatus(ctx, row.ID, model.QuoteStatusSent, model.QuoteStatusAccepted); err != nil {\n"
       "\t\t\treturn nil, fmt.Errorf(\"%w（%s，sent→accepted）：%w\", ErrBillStatusStuck, row.ID, err)\n"
       "\t\t}",
       "\t\t_ = s.quotes.UpdateStatus(ctx, row.ID, model.QuoteStatusSent, model.QuoteStatusAccepted)")],
     "TestBillDeriveCASFailureLeavesNoBill"),
    ("K48", "金额只取第一行（AC② 的算法被换成「部分合计」）", "go", "svc", SVC,
     [("\t\tAmount:        quoteSumAmount(lines),", "\t\tAmount:        quoteSumAmount(lines[:1]),")],
     "TestBillAmountEqualsHandComputedTotal"),
    ("K49", "币种不看报价行、恒用默认值", "go", "svc", SVC,
     [("\tcurrency := strings.TrimSpace(row.Currency)", "\tcurrency := model.BillCurrencyDefault")],
     "TestBillDeriveCurrencyFollowsQuote"),
    # 注：这一格的注码形状属于 T-P7-03（`in.DueAt`）。该卡的**产码**（service 侧改由调用方给账期
    # + `TestBillDeriveStoresTheDueAtTheCallerGave`）尚未提交，只在别的泳道工作树里；常驻电池
    # 必须与 HEAD 自洽，故此处保留"本层写死 nil"那一版的锚点。该卡落地那天按 `in.DueAt` 重锚，
    # 并把下面被删掉的 K50b 一并补回（`--check` 会以"锚点命中 0 次"当场提醒，不会静默）。
    ("K50", "凭空造账期（报价域里没有「付款条件」这一格，造出来的是合同条款）", "go", "svc", SVC,
     [("\t\tDueAt:         nil, // 账期未定：本卡没有任何一处定义过付款条件",
       "\t\tDueAt:         &now, // 变异注码")],
     "TestBillDeriveLeavesDueAtNull"),
    ("K51", "并发撞上「已派生」之后回读的是自己那份", "go", "svc", SVC,
     [("\t\t\treturn nil, fmt.Errorf(\"bill: 仓储报\\\"已派生过\\\"而按 quote_row_id=%s 读不到那一行（约束名与索引不同源？须人工核对）\", row.ID)\n"
       "\t\t}\n\t\treturn billViewOf(existing, true), nil",
       "\t\t\treturn nil, fmt.Errorf(\"bill: 仓储报\\\"已派生过\\\"而按 quote_row_id=%s 读不到那一行（约束名与索引不同源？须人工核对）\", row.ID)\n"
       "\t\t}\n\t\treturn billViewOf(bill, true), nil")],
     "TestBillDeriveRacesIntoExisting"),
    ("K52", "写完不回读、直接拿内存那份出视图（numeric 落库后是什么没人知道）", "go", "svc", SVC,
     [("\tstored, err := s.bills.GetByID(ctx, key)", "\tstored, err := bill, error(nil)")],
     "TestBillDeriveReportsReadBackFailure"),
    ("K53", "空行号不做入参校验（报成「不存在」，把人支去查数据）", "go", "svc", SVC,
     [("\tif rowID == \"\" {", "\tif false && rowID == \"\" {")],
     "TestBillServiceAvailabilityAndNilSafety"),
    ("K54", "账单号丢掉 seq（同一纳秒内两次生成撞号）", "go", "svc", SVC,
     [("\treturn fmt.Sprintf(\"b_%d_%d\", now.UnixNano(), seq)", "\treturn fmt.Sprintf(\"b_%d_1\", now.UnixNano())")],
     "TestBillKeyGeneratorIsDeterministic"),
    ("K55", "账单号里带日期串（时区裂脑那条老账在这把键上重演）", "go", "svc", SVC,
     [("\treturn fmt.Sprintf(\"b_%d_%d\", now.UnixNano(), seq)",
       "\treturn fmt.Sprintf(\"b_%s_%d\", now.Format(\"20060102\"), seq)")],
     "TestBillKeyGeneratorIsDeterministic"),
    ("K56", "入参结构多带一格 amount（AC② 当场失去对账对象）", "go", "svc", SVC,
     [("\tQuoteRowID string // 必填：quotes.id（**版本行主键**，不是 quotes.quote_id 逻辑号）\n}",
       "\tQuoteRowID string // 必填：quotes.id（**版本行主键**，不是 quotes.quote_id 逻辑号）\n"
       "\tAmount     float64 // 变异注码\n}")],
     "TestBillDeriveInputHasOnlyTheRowKey"),
    ("K57", "派生腿的存储接口多一个写状态的方法（「顺手标成已收」有了入口）", "go", "svc", SVC,
     [("type billStore interface {\n\tAvailable() bool\n",
       "type billStore interface {\n\tAvailable() bool\n\tUpdateStatus(ctx context.Context, id, from, to string) error\n")],
     "TestBillServicePortSurfacesAreNarrow"),
    ("K58", "重入复用那一支把 Reused 报成 false（新开与早就在了分不开）", "go", "svc", SVC,
     [("\t\tif existing != nil {\n\t\t\treturn billViewOf(existing, true), nil\n\t\t}\n\t\t// existing == nil",
       "\t\tif existing != nil {\n\t\t\treturn billViewOf(existing, false), nil\n\t\t}\n\t\t// existing == nil")],
     "TestBillDeriveIsIdempotentAcrossCalls"),
    ("K59", "未装配时不做前置门（一路走到 nil 存储上）", "go", "svc", SVC,
     [("\tif !s.Available() {\n\t\treturn nil, ErrBillServiceUnavailable\n\t}",
       "\tif s == nil {\n\t\treturn nil, ErrBillServiceUnavailable\n\t}")],
     "TestBillServiceAvailabilityAndNilSafety"),

    # ————— 控制器层（runner=ctrl）—————
    ("K60", "绑定改成宽容模式（递进来的金额被静默丢掉）", "go", "ctrl", CTRL,
     [("\tdec.DisallowUnknownFields()", "\t// dec.DisallowUnknownFields()")],
     "TestBillController_BodyCarriesOnlyTheRowKey"),
    ("K61", "请求体不封顶", "go", "ctrl", CTRL,
     [("\tctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, billBodyMaxBytes)",
       "\t// 变异注码：不封顶")],
     "TestBillController_BodyIsCapped"),
    ("K62", "不再要求操作者身份", "go", "ctrl", CTRL,
     [("\toperator, ok := contextOperatorID(ctx)\n\tif !ok {",
       "\toperator, ok := contextOperatorID(ctx)\n\tif false && !ok {")],
     "TestBillController_RequiresSession"),
    ("K63", "两种 409 共用一个 reason（修法不同却读起来一样）", "go", "ctrl", CTRL,
     [('gin.H{"reason": billReasonNotLatestVersion})', 'gin.H{"reason": billReasonNotSent})')],
     "TestBillController_SentinelsHaveDistinctOutcomes"),
    ("K64", "500 透出底层错误串（索引名与 SQL 片段外泄）", "go", "ctrl", CTRL,
     [('response.Error(ctx, http.StatusInternalServerError, "派生账单失败（底座或数据异常），本次未确认任何成交",',
       'response.Error(ctx, http.StatusInternalServerError, err.Error(),')],
     "TestBillController_UnknownErrorDoesNotLeak"),
    ("K65", "空结果当成功回 200（实现漂了没人知道）", "go", "ctrl", CTRL,
     [('\t\tc.replyError(ctx, errors.New("派生账单返回了空结果而没有给错误"), rowID)',
       '\t\tresponse.Success(ctx, nil, "ok")')],
     "TestBillController_NilResultWithoutErrorIsFailure"),
    ("K66", "行号长度不封顶（回显与下游宽度失去界）", "go", "ctrl", CTRL,
     [("\tif len(rowID) > billRowIDMaxLen {", "\tif false && len(rowID) > billRowIDMaxLen {")],
     "TestBillController_RowIDLengthIsBounded"),
    ("K67", "空行号不做边界校验", "go", "ctrl", CTRL,
     [("\tif rowID == \"\" {", "\tif false && rowID == \"\" {")],
     "TestBillController_MissingOrMalformedBody"),
    ("K68", "Available 不再问服务本身（闸门说谎）", "go", "ctrl", CTRL,
     [("\treturn c != nil && c.derive != nil && c.derive.Available()",
       "\treturn c != nil && c.derive != nil")],
     "TestBillController_UnassembledAnswersFiveOhThree"),
    ("K69", "报价不存在报成 400（换行号这件事没有入口）", "go", "ctrl", CTRL,
     [("response.Error(ctx, http.StatusNotFound, err.Error(), gin.H{\"reason\": billReasonNotFound})",
       "response.Error(ctx, http.StatusBadRequest, err.Error(), gin.H{\"reason\": billReasonNotFound})")],
     "TestBillController_SentinelsHaveDistinctOutcomes"),
    ("K70", "status_stuck 那一档落进 default（五种 409 少一种）", "go", "ctrl", CTRL,
     [("\tcase errors.Is(err, service.ErrBillStatusStuck):",
       "\tcase false && errors.Is(err, service.ErrBillStatusStuck):")],
     "TestBillController_SentinelsHaveDistinctOutcomes"),
    ("K71", "状态码轴与 reason 轴之外：未装配回 404 而不是 503", "go", "ctrl", CTRL,
     [("response.Error(ctx, http.StatusServiceUnavailable,", "response.Error(ctx, http.StatusNotFound,")],
     "TestBillController_UnassembledAnswersFiveOhThree"),
    ("K72", "派生腿的接缝多开一个读口（控制器能查账单了）", "build", "ctrl", CTRL,
     [("type BillDeriver interface {\n\tAvailable() bool\n",
       "type BillDeriver interface {\n\tAvailable() bool\n"
       "\tGetBill(ctx context.Context, id string) (*service.BillView, error)\n")],
     "does not implement BillDeriver (missing method GetBill)"),

    # ————— 装配层（runner=app）—————
    ("K80", "无库时不清全局（端点继续对着上一份实例回 200）", "go", "app", WIRE,
     [("\t\tservice.SetGlobalBillService(nil)\n", "")],
     "TestInitBillRuntimeClearsGlobalWithoutDB"),
    ("K81", "报价句柄取全局隐式句柄（装配顺序决定一切）", "go", "app", WIRE,
     [("\t\trepository.NewQuoteRepositoryWithDB(db),", "\t\trepository.NewQuoteRepository(),")],
     "TestInitBillRuntimeDoesNotDependOnTheGlobalHandle"),
    ("K82", "建好了服务却没登记进全局", "go", "app", WIRE,
     [("\tservice.SetGlobalBillService(svc)\n\n\tok := svc.Available()", "\tok := svc.Available()")],
     "TestInitBillRuntimeAssemblesTheDeriveLeg"),
    ("K83", "两把仓储句柄都没接（Available 为假而路由照挂）", "go", "app", WIRE,
     [("\tsvc := service.NewBillService(\n\t\trepository.NewBillRepositoryWithDB(db),\n"
       "\t\trepository.NewQuoteRepositoryWithDB(db),\n\t)",
       "\tsvc := service.NewBillService(nil, nil)\n\t_ = repository.ErrBillNotFound")],
     "TestInitBillRuntimeAssemblesTheDeriveLeg"),

    # ————— 挂载层（runner=route）—————
    ("K90", "启动路径上整条不挂（库里有账单而前端 404）", "go", "route", RTR,
     [("\t\tsetupBillRoutes(auth)\n", "")],
     "TestBillRoutes_MountedBySetup"),
    ("K91", "挂到了匿名组（任何人都能在任意报价上开应收）", "go", "route", RTR,
     [("\t\tsetupBillRoutes(auth)\n", "\t\tsetupBillRoutes(public)\n")],
     "TestBillRoutes_MountedAtTheRightPlace"),
    ("K95", "router 里那一行 Init 删掉（端点恒 503 而 Go 用例分不清）", "go", "route", RTR,
     [("\tapp.InitBillRuntime(gormDB)\n", "")],
     "TestBillRoutes_LiveThroughRealSetup"),
    # 两半装配（derive / read）由 T-P7-02 带进来：挂载那一行变成两个实参，
    # 早退判据变成 `derive == nil || read == nil`，末尾那行日志也换了措辞。
    # 注的形状不变：**未装配时干脆不挂路由**（把"底座没装"混成"API 不存在"）。
    ("K92", "未装配时干脆不挂路由（「底座没装」与「API 不存在」混成一件事）", "go", "route", ROUTES,
     [("\tcontroller.NewBillController(derive, read).RegisterRoutes(auth)\n\tif derive == nil || read == nil {",
       "\tif derive == nil || read == nil {"),
      ("\tlogger.Infof(\"[Router] bill 账单 API 已连通（可派生=%v，可对账=%v）\", derive.Available(), read.Available())",
       "\tcontroller.NewBillController(derive, read).RegisterRoutes(auth)\n"
       "\tlogger.Infof(\"[Router] bill 账单 API 已连通（可派生=%v，可对账=%v）\", derive.Available(), read.Available())")],
     "TestBillRoutes_UnassembledAnswersFiveOhThree"),
    ("K93", "路由文件里内联一个读口（映射之外的第二处响应）", "go", "route", ROUTES,
     [("\tcontroller.NewBillController(derive, read).RegisterRoutes(auth)\n",
       "\tcontroller.NewBillController(derive, read).RegisterRoutes(auth)\n"
       "\tauth.GET(\"/bill/list\", func(ctx *gin.Context) {\n"
       "\t\tctx.JSON(200, gin.H{\"code\": 0})\n\t})\n")],
     "TestBillRoutes_TableIsExactlyTheDeclaredSet"),
    ("K94", "Swagger 的 @Router 路径写错（契约与真实端点分家）", "go", "route", CTRL,
     [("// @Router       /api/bill [post]", "// @Router       /api/bills [post]")],
     "TestBillRoutes_SwaggerCoversEveryRoute"),

    # ————— 台账门（runner=gate：只跑 check-unwired-assets.sh）—————
    ("G1", "台账 23c：摘掉 router 里的 Init，门必须报漂移", "gate", "gate", RTR,
     [("\tapp.InitBillRuntime(gormDB)\n", "")], "账单派生腿在启动路径上的装配点"),
    ("G2", "台账 23d：摘掉 mount，门必须报漂移", "gate", "gate", RTR,
     [("\t\tsetupBillRoutes(auth)\n", "")], "账单 HTTP 出口的挂载点"),
    # 23a 的 callpat 扫的是**整个 internal/app**，而 T-P7-02 起这个目录里有两处
    # `NewBillRepositoryWithDB(`：派生腿（bill_wiring.go）与回款腿（payment_wiring.go，
    # 它要按 bill_id 跃迁状态）。于是"只摘一处"**不再是这一行的失效形状** —— 门仍报
    # WIRED 是对的（"internal/app 显式构造过账单仓储"这句话为真），掉的那半边牙由
    # K83（派生腿，runner=app）与 app 包的 TestInitPaymentRuntime*（回款腿）守着。
    # 本格因此一刀摘**两处**（pairs 的第二条自带文件名）：那才是这条存在性锁唯一的坏法。
    ("G3", "台账 23a：internal/app 两处 WithDB 构造点全摘，门必须报漂移", "gate", "gate", WIRE,
     [("\t\trepository.NewBillRepositoryWithDB(db),\n", ""),
      (PAYWIRE, "\t\trepository.NewBillRepositoryWithDB(db),\n", "")],
     "账单仓储的装配入口"),
    # 旧格打的是 23e 的**反向**（"写出 bills.UpdateStatus( 调用 ⇒ 门必须从 unwired 翻成
    # wired"。那一格的前提由 T-P7-02 兑现掉了：调用点已经写进 payment.go，行已经登记成
    # wired ⇒ 再加一次调用不改变门的任何一条输出，留着它就是一台永不开火的枪。
    # 挪成正向：拆掉那一行调用 ⇒ 门必须报「已登记的接线被拆掉」。注码仍不要求可编译
    # （删掉 `if` 头会留下一个孤 brace），它与旧格守的是同一句话：这一行的 callpat 收得拢吗。
    ("G4", "台账 23e 正向：拆掉 payment.go 的跃迁调用，门必须报「接线被拆掉」", "gate", "gate", PSVC,
     [("\tif err := s.bills.UpdateStatus(ctx, bill.ID, bill.Status, view.Status); err != nil {\n", "")],
     "账单状态跃迁口的生产调用方"),
    ("G5", "台账 23b：造行改用局部类型别名（callpat 会因改名而瞎），门必须报漂移", "gate", "gate", SVC,
     [("func billViewOf(", "type billRow = model.Bill\n\nfunc billViewOf("),
      ("\tbill := &model.Bill{", "\tbill := &billRow{")],
     "账单行的生产写入点"),
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
    """本格会写到盘上的文件集合。

    pairs 里的注码默认打在 cell[4] 那一份文件上；写成三元组 `(文件, 旧, 新)` 时打在
    点名的那一份上。要这个形状的理由：台账的一条存在性锁扫的是**整个目录**，
    同一个构造函数在目录里长出第二个消费方之后，"摘掉一处"就不再是这条锁的失效形状
    —— 只有一刀摘完才算证到它有牙（G3 是第一例，判据原文写在它下面）。
    """
    return {cell[4]} | {p[0] for p in cell[5] if len(p) == 3}


def apply_cell(originals: dict[str, str], cell) -> dict[str, str]:
    """把一格的全部注码**在内存里叠完**再返回，中途任何一锚不命中都不留下半个字节。

    返回值是 {文件: 注完之后的全文}；未点名的文件不进这张表，也就不会被写盘。
    """
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
            # 但工作树若与 HEAD 不一致，"电池全杀"证的就不是我改过的那份字节 ⇒ 停机点名，不静默。
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
    # top_pass 只数**顶层** PASS：`ran` 数的是顶层 `=== RUN`（子测试那几行带缩进，进不了
    # 这个计数），而 `passed` 把 `    --- PASS` 也一起算了 ⇒ 拿 passed 去和 ran 比守恒，
    # 凡有子测试的包必然"多算"，那条断言会天天假红。守恒式两边都必须是顶层口径。
    top_pass = len(re.findall(r"^--- PASS: (\S+)", out, re.M))
    ran = len(re.findall(r"^=== RUN\s+(\S+)", out, re.M))
    skipped = len(re.findall(r"^--- SKIP: (\S+)", out, re.M))
    return rc, killed, ran, skipped, passed, top_pass, out


def gate_run(clone: Path):
    r = subprocess.run(["bash", "scripts/check-unwired-assets.sh"], cwd=clone,
                       capture_output=True, text=True, timeout=900, env=dict(os.environ))
    return r.returncode, ANSI.sub("", r.stdout + r.stderr)


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
        # 【这一条判据在头两趟里是**缺失**的】原先只判 `if killed: return KILLED`，于是
        # "注码把**别的**用例弄红、而 expect 那条腿照样绿"也算杀掉 —— 那恰好是"这一格没牙"
        # 的机器形态，文件头上写的 RED-UNNAMED 那一类从来没被这条分支产出过。
        # 症状是 T-P7-03 加 K50b 时暴露的：注码打在 `in.DueAt` 上，红的却可能是别的派生用例。
        # 修法是点名：expect 不在 FAIL 名单里 ⇒ RED-UNNAMED，不计入杀掉。
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
    from battlog import tee_to  # 判定行与逐格产物同处一地（LOGDIR/00-run.log）
    tee_to(LOGDIR / "00-run.log")

    cells = CELLS
    if args.cells:
        want = [c.strip() for c in args.cells.split(",") if c.strip()]
        cells = [c for c in CELLS if c[0] in set(want)]
        missing = set(want) - {c[0] for c in cells}
        if missing:
            raise SystemExit(f"未知代号：{sorted(missing)}")

    tmp = Path(args.clone or tempfile.mkdtemp(prefix="p701mut-"))
    tmp.mkdir(parents=True, exist_ok=True)
    print(f"私有作业目录：{tmp}\n逐格日志目录：{LOGDIR}")
    clone = prepare(tmp)

    def sweep() -> None:
        """除了 --keep，正常出口与"可复现"的停机出口都把私有克隆带走。

        以前 `--check` 与控制组不干净那两处是直接 return/raise 走的，把整份克隆留在 /tmp 里：
        一轮整电池几百 MB，而磁盘常态是 99% 满。
        **md5 不一致那一支刻意不扫**：那份"还原之后还是不对"的字节是唯一证据，
        克隆可复现而它不可复现，删了就只剩一句"当时红过"。
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
            dump("00-control-gate", out)
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
        dump(f"00-control-{name}", out)
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
        try:
            mutated = apply_cell(originals, cell)
        except SystemExit as e:
            problems.append(str(e))
            continue
        for prel, t in mutated.items():
            files[prel].write_text(t, encoding="utf-8")

        if kind == "gate":
            rc, out = gate_run(clone)
            dump(code, out)
            if rc == 0:
                v = "SURVIVED"
            elif expect in out:
                v = "KILLED"
            else:
                v = "RED-UNNAMED"
            killed = [expect] if v == "KILLED" else []
            ran = 1
        elif kind == "build":
            # 编译期锁的格子：判据本身就是"编不过"，所以 BUILD-BROKEN 是**期望结论**而不是未杀。
            # 但只认 BUILD-BROKEN 会假绿（任何语法错都算杀掉），因此 expect 写成必须点名的编译错串。
            rc, killed, ran, skipped, passed, top_pass, out = go_run(clone, runner)
            dump(code, out)
            v = classify(rc, killed, ran, skipped, out, controls[runner])
            if v == "BUILD-BROKEN" and expect in out:
                v, killed = "KILLED", [expect]
        else:
            rc, killed, ran, skipped, passed, top_pass, out = go_run(clone, runner)
            dump(code, out)
            v = classify(rc, killed, ran, skipped, out, controls[runner], expect)
            # 头注里承诺过的第二条断言：**PASS + FAIL == 控制组数**。
            # 只看"点名那条红了"会放过一种真实坏法 —— 注码让别的用例 panic 中止，
            # 二进制提前退出，expect 那条恰好排在 panic 之前跑完并红了，于是看着像杀掉。
            # ran < control 拦不住它（panic 影响的是后面的），这一条按"总数守恒"拦。
            if kind == "go" and v in ("KILLED", "RED-UNNAMED") and \
                    top_pass + len(killed) != control_top[runner]:
                problems.append(f"{code} 计数不平：顶层 PASS={top_pass} + FAIL={len(killed)} ≠ 控制组的 "
                                f"{control_top[runner]}（有用例被 panic 带走，或压根没参与这一趟）")

        tally[v] += 1
        killmap[code] = set(killed)
        print(f"{code:<5} {desc[:58]:<60} {v} ran={ran} FAIL={len(killed)} rc={rc}")
        if v == "KILLED" and kind == "go" and ran < controls[runner]:
            problems.append(f"{code} 杀了但 ran={ran}<{controls[runner]}：疑似 panic 中止，红因要人工看")
        if v == "SURVIVED":
            problems.append(f"{code} 存活 = 洞：{desc}")
        if v == "RED-UNNAMED":
            problems.append(f"{code} 红了但没点出 {expect}：{sorted(killed)[:4]}")
        if v in ("BUILD-BROKEN", "ENV-BROKEN", "NO-RUN"):
            problems.append(f"{code} 判为 {v}（不是干净的「杀掉」）：{desc}")
            print(out[-2500:])

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
