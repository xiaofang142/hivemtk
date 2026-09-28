#!/usr/bin/env python3
"""批22（A6）+ 批23（界来源）变异电池：裁剪前摘要与 cutoff 来源的**每一句承诺**逐格验牙。

为什么要有这条电池（而不是"12 条腿全绿"就算完）：
- A6 的承诺是一串**链**：摘要先落库 → 落不进就不许删 → 摘要如实描述被删的那些行 →
  行与行之间按内容折指纹 → 批与批之间按链哈希相接 → 每次扫描（含 0 行的那种）留一行痕
  → 痕里的计数与算术闭合 → 读侧按链序导出。任一环松掉，库里的读数照样"有摘要"，
  但那份摘要已经不能证明它自己说的那件事。全绿只证明每一环单独存在，不证明各自有牙。
- D3/D4/D5 三刀打的是同一段赋值（`prevChain, prevSeq, ordinal = ...`）的三个分量：
  链哈希、seq 接续、批序。它们混在一起就是"有人只改了一个字段"时另外两个没人看。
- D1 与 D2 方向不同：D1 是"根本不写摘要"（少一份凭据，9 条腿一起红），D2 是"写不上也照删"
  （fail-close 折成 fail-open）。D2 只点名"读得到、写不进"那一腿：旁路补记在没有拦路约束的
  库里**照样写成功**，所以描述类腿（R_COVER/R_CLOSE）看不见它——这不是漏格，是这一刀的失效面
  本来就窄到只有那一条腿能承接。expect 首轮写成三条、实跑"红了但没点出另外两条"，按同一纪律收回。
- **D2 的注码形状是量出来的，不是拍的**：最初那版只把 `tx.Create` 的错误吞掉（`if err != nil {}`），
  首轮实跑判"存活=洞"。红因读完实现才看清：摘要 INSERT 与 DELETE 在**同一个 PG 事务**里，
  一条语句失败后该事务即进入 aborted 态，后续 DELETE 一并报错回滚——"吞错"根本改变不了库里
  的读数，那是一格**无后果的注码**（按口径它不算杀，也不算洞，只能换形状）。真正能产出
  "行没了、摘要没落"这个坏形状的是**把摘要挪出事务、并且不看它的错**（旁路补记），故 D2 注这一刀。
  顺带记一句实现事实：本腿的 fail-close 实际有两层——`return err` 与 PG 的原子性，摘掉前一层
  库里仍是安全的，所以**只有**两层一起松掉才是那件坏事，注码必须打在两层上。
- D12/D13/D14 打的是读侧三跳（服务层取不取 → 控制器导不导 → 按什么序导）。
  写侧全对而导出这一路漏一跳，代价是"裁过的会话导出来的包里 command_log 为空、
  却说不出为什么为空"——正是本卡立项要拆穿的那类沉默。

- **V1–V6（批23）打的是「这次裁剪的界从哪来」这一条链**，合进本电池而不是另起一份：它和 A6 是同一条
  治理路径上的两次修复，共用两个入口（`PruneBefore` 与 `pruneAuditOnce`），拆成两份会让"一次裁剪"的
  两半各测各的，而这一条链的坏形状恰恰跨半——参数接住了却不落库（V2）只有仓储侧腿看得见，落库了但
  上游传的是死值（V3）只有服务层侧腿看得见。V5/V6 补的是两处"看着像保险、其实有后果"的取值：
  截断上限放开后超长 env 值会让留痕 INSERT 失败（裁剪被一个记账字段堵死），启动门不看仓储是否装配齐
  则是一小时后在后台 goroutine 里 panic 带走整个进程。`pruneAuditOnce`/`retentionEnabled` 之所以
  从 goroutine 里抽成函数，就是为了让 V3/V4/V6 这三格可达——不抽出来，那三句承诺永远只能靠读代码信。

- **C 相续刀（lane 3）把 B 相只读攻击线里"仓内没格"的四把 A6 刀做成 D19-D22**，逐条对过既有格：
  D19 ≠ D2（D2 松两层＝挪出事务 + 不看错；D19 只换连接、错照旧报，能红它的只有"摘要写成、
  删除失败、整事务回滚"那一幕）；D21 ≠ D14（D14 打读侧导出序，D21 打写侧取链头的 ORDER BY ordinal）；
  D20 ≠ D16（D16 恒 0，D20 换成同源累计值）；D22 ≠ D11（同一段 CASE 的两把刀：D11 折 nil/false，
  D22 保留 NULL 分支、把 true/false 折成一态，D11 那条腿当场绿）。
  **动手前对 B 相那张表的三处订正**（原表自陈"候选均未实际注码运行"）：
  ① D19：B 相正文点名的 `TestA6PruneFailsCloseWhenDigestWriteBlocked` 杀不掉这一刀（那条腿的约束
     挡住的是 INSERT 本身，摘要挪到哪条连接上都照样先报错、照样回滚），承接得住的只有
     `TestA6PruneFailsCloseWhenDeleteFailsMidRun`（C 相任务表里已经改成它了，这里记一句为什么）；
  ② D20：该格**不挂**在 `TestA6RunRowClosesArithmeticUnderTwoBatches` 上——那条腿自己的注释就写明
     在能写下 run 行的任何确定性路径上 total 恒等于 rowsBefore（入口计数谓词 == 窗口/删除谓词）。
     注码实跑先确认存活不是取证假象（它跑了、PASS），再按"存活即缺口"补窄腿
     `TestA6RunRowCountsEntryWindowNotLateCrossing`：用行级触发器造出"裁剪进行中才越界的那一行"，
     让入口数到 5、实删 6，两值当场分家；
  ③ D22：预判存活兑现了（`TestA6DigestSeparatesUnackedFromFailedFrame` 对照的是 nil vs false，
     折掉 t/f 后仍不同），补的窄腿是 `TestA6DigestSeparatesPassedFromFailedFrame`（true vs false）。

口径（沿用批16/17/18/19x/20c/20d/20f 电池）：
- 控制组必须 rc==0、total>0、skip==0、且不许有任何红名；
- 每格断言 PASS+FAIL==控制组数（一条用例 panic 会带走整个二进制，"FAIL=1"看着像杀其实没跑完）；
- BUILD FAILED / panic / 红而没点名 一律判 BROKEN，**不计入杀掉**（编译红不是牙）；
- 锚点命中必须恰好一次，注码先过 gofmt -e；多处注码先在**内存里累加**、最后一次落盘；
  还原后逐文件比 md5；只在私有 --shared 克隆里注码，绝不碰共享工作树。

已知**未覆盖**的格子（写在这里而不是悄悄不留痕）：
- 批23 的 `cutoff_source` **列本身**（DDL / NOT NULL / 默认值）不在本电池管辖：克隆里的表由 testutil
  按模型标签直建，摘掉 v3.46.0 迁移的 `ADD COLUMN` 不会让本电池任何一格红。那一格由
  `v3_46_0_browser_audit_prune_cutoff_source_migration_test.go` 的 `_UpAndShape`（真 PG 往返）与
  `scripts/check_model_migration.py` 管，见批23 报告。
- `TestA6PruneFailsCloseWithoutDigestTable`（缺表那条）**没有任何一格能杀掉它**，这不是漏注码：
  它的判据是"表不在时 PruneBefore 必须报错且行不消失"，而这条路径上先报错的是
  `lastAuditDigest` 的 SELECT——D1（不写摘要）与 D2（旁路补记）都改不动它的读数，它照样绿。
  真正管住"摘要没落库却把行删了"这个坏形状的是 `TestA6PruneFailsCloseWhenDigestWriteBlocked`
  （D2 点名的就是它）。缺表那条留在测试里锁的是**另一个前提**（摘要表整体不在时同样不许删），
  与"有没有牙"无关——把这类"前置条件腿"当成有牙腿来点名单，就会写出永远兑现不了的 expect（首轮实跑即如此）。
- D8（分批上界形同虚设）只点名 `TestA6DigestSplitsWithDeleteBatches` 与批19g 那条分批锁：
  首轮 expect 里另写了链腿与三态腿，实跑"红了但没点出它们"——那两腿各自只有 2 条摘要、
  窗口再大也折不出两个批次，**看不到 LIMIT 变了**。是 expect 写宽了，不是变异没后果，
  故按"改名式变异要断言判据命中"的同一纪律收回 expect 而不是改注码。
- **D23 是本电池最后一格，它的夹具形状被实测改过一次**（登记在这里而不是悄悄换掉）。
  `if res.RowsAffected != w.RowCount { 撤销本批 }` 此前"没有一格钉它"，当时写下的理由是"摘掉 `if` 的
  坏形状是摘要描述失实但仍落库，要钉它得多造一次夹具"。造夹具时**第一版被判不可表达**：想让同一条
  DELETE 少删一行，最直接的是 BEFORE DELETE 触发器在删掉首行时把末行的 created_at 推回未到期——
  实跑红在**未注码的控制组**上，红因不是守卫而是数据库：
  `retention_a6_test.go:903: 撤销原因不含 "摘要声明 3 行、实删 2 行"：ERROR: tuple to be updated was
  already modified by an operation triggered by the current command (SQLSTATE 27000)`
  （取证日志 `docs/superpowers/specs/ledger/logs/R22-lanes/mut_retention_a6-d23-cell-r22close.log`，
  它记录的是**被否掉的形状**，故保留不覆盖）。PG 不允许一条命令改动"已被本命令触发的操作改过"的元组
  ⇒ 同一条语句内分家根本走不到行数比较那一步，报的是数据库的错。
  第二版把分家挪到**同一事务的两条语句之间**（窗口 `ORDER BY session_id`、循环逐 session 先落摘要再删行）：
  actor 会话（id 小）那条 DELETE 的 AFTER DELETE 触发器，把**另一个**会话里一条未到期、id 落在其区间中间、
  且 id ≤ 入口高水位的行补写成超期 ⇒ victim 的摘要仍是"进事务之前"那次扫描的 2 行，它的 DELETE 实删 3 行
  ⇒ 守卫开火、整事务回滚（两条摘要与所有行都回去）。腿名
  `TestA6PruneRollsBackWhenDigestUnderstatesDeletedRows`，与 D20 那条"分家发生在批之间"的腿各钉一句：
  那条管 rows_before 记的是入口那一瞬，这条管描述失实的那一批不许留凭据。
  触发器必须**只开火一次**：AFTER ROW 为每条被删行各触发一次，同一个 UPDATE 第二次就重新撞上 27000，
  所以判据里 gate 在 `OLD.session_id = actor`，victim 自己的三行由同一条 gate 挡在门外。
- `COALESCE(...)`（可空列折成空串，防 concat_ws 跳参数导致字段左移）无可达见证：
  哈希字段清单里只有 `ok` 真会为 NULL，而它走的是 CASE 分支（D11 已锁住三态）；
  step_id / task_id 在模型上是 uint（恒有值），payload 在所有种子里非空。
  摘掉 COALESCE 不改变任何一条腿的读数。这一格是**面向未来加列**的保险，不是当前行为。
- `migrate.go` 里三张新表（BrowserWriteClaim / BrowserAuditDigest / BrowserAuditPruneRun）
  的登记不受本电池管辖：克隆里的表由 testutil 按模型标签直建，摘掉登记不会让任何腿红。
  那一格由 scripts/check_model_migration.py 管，反向验证（逐张摘登记 → 该门必须红并点名）
  见批次报告。
- router 里 `sessionSvc.SetAuditDigestRepository(digestRepo)` 那一行同理：Go 用例都是就地
  new 服务，没有任何一条走 router.Setup，摘掉它全绿。那一格由
  scripts/check-unwired-assets.sh 的第 22 项登记管（摘行 → 该门必须红），见批次报告。

用法：python3 scripts/mut_retention_a6.py [--keep] [--clone DIR] [--cells D1,D2,...]

--cells 是**取证侧的分包**，不改判据：一轮 28 格（22 格 D + 6 格 V）× 每格一次全量跑，在单次
10 分钟的命令预算里跑不完（实跑过一次：控制组刚跑完就被 SIGTERM 掐掉，日志只剩一行）。拆成两趟时
**每趟各自重建克隆、各自重测控制组**，两趟都必须"逐格被杀"才算全杀；报告里要写清是分几趟跑的，
而不是"一轮全杀"。格数的算法随批走：批23 那份日志是 22 格（那时还没有 D18），C 相续刀（lane 3）
之前磁盘上是 23 格，加 D19-D22 四格后是 27 格，收口轮（D23，行数守卫）再加一格到 28——
**引用旧日志里的"22 格"或"27 格"当本电池的格数会少算**。
"""
from __future__ import annotations

import argparse
import hashlib
import os
import re
import shutil
import subprocess
import sys
from pathlib import Path
from mut_dispose import dispose, workdir

# 脚本在 <repo>/scripts/ 下 ⇒ 根 = 上一级。**不硬编码仓名**（改名克隆必须照样能跑）。
ROOT = Path(__file__).resolve().parent.parent

CMDLOG_REL = Path("user-server/internal/browser_automation/repository/command_log.go")
READER_REL = Path("user-server/internal/browser_automation/repository/audit_digest.go")
MODEL_REL = Path("user-server/internal/browser_automation/model/audit_digest.go")
SVC_REL = Path("user-server/internal/browser_automation/service/session.go")
CTL_REL = Path("user-server/internal/browser_automation/controller/session.go")
RETENTION_REL = Path("user-server/internal/browser_automation/service/retention.go")
# 槽名 → 仓内相对路径。只此一份：电池自己按它拼克隆里的目标文件，
# scripts/anchor-preflight.py 按它在**工作树**上预验锚点（不建克隆、不跑用例）。
# 两处各写一份字典就会漂（漂了表现为"预检绿、电池说锚点没命中"）。
SLOT_FILES = {"cmdlog": CMDLOG_REL, "reader": READER_REL, "model": MODEL_REL,
              "svc": SVC_REL, "ctl": CTL_REL, "retention": RETENTION_REL}
# 本泳道脏 .go 的枚举范围。手写清单必然漏（漏一个用旧签名的文件 = 克隆里 [build failed]，
# 电池整个失声），所以按目录取 git status。
LANE_PATHS = ["user-server/internal/browser_automation",
              "user-server/internal/pkg/db",
              "user-server/internal/router"]

GO_PKGS = ["./internal/browser_automation/repository/",
           "./internal/browser_automation/service/",
           "./internal/browser_automation/controller/"]
# 只跑本批的腿：仓储全部 A6* 腿 + 批19g 那条分批锁（它管"收回分批"这一刀）、服务层导出归并、
# 控制器导出载荷、批23 界来源（R23*）。跑整包会把无关用例的红混进"杀掉"名单，
# 看着像牙其实是被别人撞红的。
GO_RUN = "TestA6|TestB19GCommandLog|TestSessionExportAggregates|TestR23"

# 期望被杀的腿名（简称常量，避免手抖打错整串）
R_COVER = "TestA6DigestCoversDeletedRows"
R_CLOSE = "TestA6DigestClosesOverRunRow"
R_CHAIN = "TestA6ChainLinksAcrossRuns"
R_GAP = "TestA6DigestRecordsSeqGap"
R_PAYLOAD = "TestA6BatchDigestSensitiveToPayload"
R_FAILCLOSE = "TestA6PruneFailsCloseWithoutDigestTable"
R_WBLOCK = "TestA6PruneFailsCloseWhenDigestWriteBlocked"
R_EMPTY = "TestA6EmptyWindowStillWritesRunRow"
R_REPRO = "TestA6DigestReproducibleFromRowContent"
R_SPLITS = "TestA6DigestSplitsWithDeleteBatches"
R_B19G = "TestB19GCommandLogPruneIsBatched"
R_OK3 = "TestA6DigestSeparatesUnackedFromFailedFrame"
R_STORE = "TestA6DigestChainIdempotentAtStorage"
# C 相续刀（lane 3，A6 四刀 D19-D22）新点名的四条腿：
# 前两条 02:47 就写进了 retention_a6_chain_prune_test.go（当时仓内没格钉它们），
# 后两条是本泳道按"存活即缺口"补的窄腿（各自在注码下红过一次、还原后绿）。
R_MIDFAIL = "TestA6PruneFailsCloseWhenDeleteFailsMidRun"
R_CHAIN3 = "TestA6ChainLinksToNewestDigestAcrossThreeRounds"
R_OKTF = "TestA6DigestSeparatesPassedFromFailedFrame"
R_LATECROSS = "TestA6RunRowCountsEntryWindowNotLateCrossing"
# D23（摘要声明与实删分家那一幕）点名的腿：与 R_LATECROSS 同表、同机制（AFTER DELETE 触发器），
# 但分家发生在**同一事务的两条语句之间**而非两条语句之内——PG 不允许一条命令改动已被本命令
# 触发的操作改过的元组（SQLSTATE 27000，第一版的 BEFORE 夹具实测撞在这句上，见该腿注释）。
R_GUARDMISMATCH = "TestA6PruneRollsBackWhenDigestUnderstatesDeletedRows"
S_EXPORT = "TestSessionExportAggregates"
C_PAYLOAD = "TestA6ExportPayloadCarriesAuditDigests"
C_STATE = "TestA6ExportDistinguishesUnwiredFromNothingPruned"
# 批23（§7.28 八-3）界来源五条腿
R23_RECORD = "TestR23PruneRunRecordsCutoffSource"
R23_REFUSE = "TestR23PruneRefusesEmptyCutoffSource"
R23_WIRE = "TestR23PruneOncePassesSourceToRepo"
R23_ZERO = "TestR23PruneOnceDisabledByZero"
R23_SHAPE = "TestR23RetentionCutoffSourceShape"
R23_BOOT = "TestR23RetentionBootGate"

ANSI = re.compile(r"\x1b\[[0-9;]*m")
CONTROL = {"go": 0}

# ---------------------------------------------------------------- 锚点（取完整语句含缩进）
# 仓储写侧
A_CREATE = ("\t\t\t\tif err := tx.Create(&d).Error; err != nil {\n"
            "\t\t\t\t\treturn err\n"
            "\t\t\t\t}\n")
A_CREATE_SKIP = "\t\t\t\t_ = d // 变异：摘要不落库，直接删\n"
# D2 的形状见文件头：吞掉事务内 INSERT 的错在 PG 下**无后果**（事务已 aborted，DELETE 一起回滚），
# 所以这一格注的是能真正产出"行没了、摘要没落"的旁路补记：写从 tx 里挪出去，且不看错。
A_CREATE_SIDE = "\t\t\t\t_ = r.db.WithContext(ctx).Create(&d) // 变异：摘要挪出事务、写不上也不管\n"
# D19 与 D2 打的不是同一格：D2 松的是两层（挪出事务 + 不看错），D19 只把摘要写到**另一条连接**上，
# 错误照旧照传、报错照旧上抛——于是能红它的只有「摘要写成、删除失败、整事务回滚」那一幕
# （retention_a6_chain_prune_test.go 的行级触发器夹具）。A6 四刀里这一刀的失效面最窄。
A_CREATE_OUTSIDE = ("\t\t\t\tif err := r.db.WithContext(ctx).Create(&d).Error; err != nil {\n"
                    "\t\t\t\t\treturn err\n"
                    "\t\t\t\t}\n")
A_PREV = ("\t\t\t\t\tprevChain, prevSeq, ordinal = prev.ChainHash, prev.LastSeq, prev.Ordinal+1\n")
A_PREV_NOCHAIN = "\t\t\t\t\tprevChain, prevSeq, ordinal = \"\", prev.LastSeq, prev.Ordinal+1\n"
A_PREV_NOSEQ = "\t\t\t\t\tprevChain, prevSeq, ordinal = prev.ChainHash, 0, prev.Ordinal+1\n"
A_PREV_NOORD = "\t\t\t\t\tprevChain, prevSeq, ordinal = prev.ChainHash, prev.LastSeq, 1\n"
A_LIMIT = "Raw(pruneWindowSQL, cutoff, highwater, pruneBatchRows)"
A_ROWHASH_PAYLOAD = "\tCOALESCE(payload::text, '{}'),\n"
A_ROWHASH_TZ = "\t(created_at AT TIME ZONE 'UTC')::text), 'utf8')), 'hex')`"
A_ROWHASH_TZ_RAW = "\tcreated_at::text), 'utf8')), 'hex')`"
A_ROWHASH_OK = "\tCASE WHEN ok IS NULL THEN 'n' WHEN ok THEN 't' ELSE 'f' END,\n"
A_ROWHASH_OK_FOLD = "\tCASE WHEN ok THEN 't' ELSE 'f' END,\n"
# D11 与 D22 是同一段 CASE 的**两把不同的刀**：D11 折掉 nil 与 false（无回执被说成回执为否），
# D22 保留 NULL 分支、把 true 与 false 折成一态（'n'/'t' 两态仍分得开，D11 那条腿照样绿）——
# 后者折掉的正是 A2 那根「下发了 / 成了」的轴，红它的是 TestA6DigestSeparatesPassedFromFailedFrame。
A_ROWHASH_OK_TF_FOLD = "\tCASE WHEN ok IS NULL THEN 'n' ELSE 't' END,\n"
# D21：链头取"最新一条"退化成"最旧一条"（lastAuditDigest 的 ORDER BY ordinal）。
A_LASTDIGEST_DESC = 'tx.Where("session_id = ?", sessionID).Order("ordinal DESC").First(&d)'
A_LASTDIGEST_ASC = 'tx.Where("session_id = ?", sessionID).Order("ordinal ASC").First(&d)'
A_RUNROW = ("\tif err := r.db.WithContext(ctx).Create(&run).Error; err != nil {\n"
            "\t\treturn total, err\n"
            "\t}\n")
A_RUNROW_SKIP = "\t_ = run // 变异：不写扫描留痕\n"
# 三个空格不是手抖：批23 往同一个 struct 字面量里加了 `CutoffSource:`（更长的键名），gofmt 把整块
# 重新对齐 ⇒ 按旧的一个空格写锚点会命中 0 次（全电池在注码前置检查就停机，实跑踩过）。
A_ROWSBEFORE = "\t\tRowsBefore:   rowsBefore,\n"
# D16 是"入口行数不记"（恒 0），D20 是"入口行数换成收口时的累计删除数"：后者在
# 「没人中途改数据」的任何一次成功裁剪上与实现逐字节同值（入口计数谓词 == 窗口/删除谓词），
# 所以钉它的不是那条两批次的大腿（那条腿自己的注释就写了它杀不掉这一刀），
# 而是造出「裁剪进行中才越界的那一行」的 TestA6RunRowCountsEntryWindowNotLateCrossing。
A_ROWSBEFORE_TOTAL = "\t\tRowsBefore:   total,\n"
# D23 打的是「摘要声明的行数与实删的行数不符即撤销本批」那道守卫。注法写成 `&& false`
# 而不是删掉整块：`fmt` 在本文件里只有这一处用到，删块会让克隆编译不过（BROKEN 不算杀）。
A_ROWCOUNT_GUARD = "\t\t\t\tif res.RowsAffected != w.RowCount {\n"
A_ROWCOUNT_GUARD_DENT = "\t\t\t\tif res.RowsAffected != w.RowCount && false {\n"
A_DIGESTCOUNT = "\t\tdigests += len(window)\n"
# D18 打的不是"少算一行"，而是**把行数折成区间的算术长度**：seq 4 从未落库时，
# 区间 1-5 照样"看起来完整"，断号被算术抹平——§7.28 八-1 那句"守的是区间长度 ≠ 行数"
# 说的就是这一格。原形状（w.RowCount 来自 SQL 的 count(*)）与注码形状在"无断号"的夹具上
# 读数相同，所以只有真带断号的夹具（TestA6DigestRecordsSeqGap）能分辨它。
A_ROWCOUNT = "\t\t\t\t\tRowCount:      w.RowCount,\n"
A_ROWCOUNT_SPAN = "\t\t\t\t\tRowCount: int64(w.LastSeq - w.FirstSeq + 1),\n"
# 仓储读侧 / 模型 / 服务 / 控制器
A_ORDER = 'Where("session_id = ?", sessionID).Order("ordinal ASC")'
A_IDX_1 = "uniqueIndex:uk_browser_audit_digests_session_ordinal,priority:1"
A_IDX_2 = "uniqueIndex:uk_browser_audit_digests_session_ordinal,priority:2"
A_SVC_READ = "\tif s.digestRepo != nil {\n"
A_CTL_KEY = '\t\t"audit_digests": digests,\n'
# 批23（§7.28 八-3）：裁剪界的**来源**这一条链的三处落点
A_SOURCE_GUARD = ('\tif cutoffSource == "" {\n'
                  '\t\treturn 0, ErrPruneCutoffSourceRequired\n'
                  "\t}\n")
A_SOURCE_GUARD_SKIP = "\t// 变异：摘掉「空来源即拒绝」，退回成界说什么就是什么\n"
A_SOURCE_VALUE = "\t\tCutoffSource: cutoffSource,\n"
A_SOURCE_VALUE_BLANK = '\t\tCutoffSource: "",\n'
A_SVC_SOURCE = "cmdLogRepo.PruneBefore(ctx, cutoff, source)"
A_SVC_SOURCE_HARD = 'cmdLogRepo.PruneBefore(ctx, cutoff, "default:90")'
A_SVC_DISABLE = ("\tif days <= 0 {\n"
                 "\t\treturn // 运行中被改成 0：本轮起不再裁剪（口径同启动时的那道门）\n"
                 "\t}\n")
A_SVC_DISABLE_SKIP = "\t// 变异：不看开关值，0（禁用）也照裁\n"
A_SOURCE_LEN = "\tcutoffSourceMaxLen = 64\n"
A_SOURCE_LEN_WIDE = "\tcutoffSourceMaxLen = 1024\n"
A_BOOT_GATE = ("\tif cmdLogRepo == nil || planRepo == nil {\n"
               "\t\treturn false\n"
               "\t}\n")
A_BOOT_GATE_SKIP = "\t// 变异：不看仓储是否装配齐\n"


def md5_bytes(p: Path) -> str:
    return hashlib.md5(p.read_bytes()).hexdigest()


def read(path: Path) -> str:
    return path.read_text(encoding="utf-8")


def verdict(r: dict, expect_total: int) -> str:
    """三态判定：杀掉 / 存活=洞 / BROKEN（红了但判不了）。"""
    if r["rc"] == 0 and not r["killed"]:
        return "存活=洞"
    if r.get("panicked") or r.get("buildfailed") or not r["killed"]:
        return "BROKEN=判不了"
    if r["total"] != expect_total or r["failed"] < 1 or r["skipped"] > 0:
        return "BROKEN=判不了"
    return "杀掉"


def dup_report(kills: dict) -> None:
    seen = {}
    for code, names in kills.items():
        key = tuple(sorted(names))
        seen.setdefault(key, []).append(code)
    for key, codes in seen.items():
        if len(codes) > 1 and key:
            print("[Go] 同族（同一批用例被多格杀掉，判据可能重叠）：" + "≈".join(codes)
                  + f" → {' '.join(sorted(key))[:120]}")


def lane_overlays() -> list[str]:
    r = subprocess.run(["git", "-C", str(ROOT), "status", "--porcelain", "-uall", "--"] + LANE_PATHS,
                       capture_output=True, text=True, timeout=300)
    if r.returncode != 0:
        raise SystemExit("git status 失败，拿不到本泳道脏文件清单：" + r.stderr[-200:])
    out = []
    for line in r.stdout.splitlines():
        p = line[3:].split(" -> ")[-1].strip().strip('"')
        if p.endswith(".go"):
            out.append(p)
    if not out:
        raise SystemExit("脏文件清单为空——克隆里跑的是 HEAD，测不到本批改动（宁可停机也别假绿）")
    return out


def syntax_ok_go(src: str) -> tuple[bool, str]:
    p = subprocess.run(["gofmt", "-e"], input=src, capture_output=True, text=True, timeout=120)
    return p.returncode == 0, p.stderr.strip()


def go_prepare(dst: Path) -> Path:
    clone = dst / "clone"
    if clone.exists():
        raise SystemExit(f"{clone} 已存在（换 --clone 目录或先删）")
    r = subprocess.run(["git", "clone", "--shared", "--no-checkout", str(ROOT), str(clone)],
                       capture_output=True, text=True, timeout=900)
    if r.returncode != 0:
        raise SystemExit("克隆失败：" + (r.stdout + r.stderr)[-400:])
    # 分支从**工作树**读，不从克隆读：--no-checkout 的克隆里 HEAD 是未 born 的符号引用，
    # `branch --show-current` 可能给空串 ⇒ 退回 master，而本仓当前分支未必是 master。
    b = subprocess.run(["git", "-C", str(ROOT), "branch", "--show-current"],
                       capture_output=True, text=True, timeout=60)
    branch = b.stdout.strip() or "master"
    c = subprocess.run(["git", "checkout", "-f", branch], cwd=clone,
                       capture_output=True, text=True, timeout=900)
    if c.returncode != 0:
        raise SystemExit("checkout 失败：" + (c.stdout + c.stderr)[-400:])
    for rel in lane_overlays():
        src = ROOT / rel
        if not src.exists():
            raise SystemExit(f"覆盖源缺失：{src}")
        tgt = clone / rel
        tgt.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(src, tgt)
        if md5_bytes(src) != md5_bytes(tgt):
            raise SystemExit(f"覆盖后 md5 不一致（装错树/写盘失败）：{rel}")
    # .env 不进 git ⇒ 克隆里没有则依赖 DB 的用例会 skip，控制组就不干净（skip==0 是硬门）。
    hostenv = ROOT / "user-server" / ".env"
    if hostenv.exists():
        shutil.copy2(hostenv, clone / "user-server" / ".env")
    return clone


def go_run(clone: Path) -> dict:
    root = clone / "user-server"
    env = dict(os.environ)
    env.setdefault("GIN_MODE", "test")
    env.setdefault("GOCACHE", "/tmp/gocache-a6mut")
    env.setdefault("POSTGRES_TEST_PORT", "8232")
    envf = root / ".env"
    if envf.exists():
        for line in read(envf).splitlines():
            if line.startswith("POSTGRES_PASSWORD=") and "POSTGRES_TEST_PASSWORD" not in env:
                env["POSTGRES_TEST_PASSWORD"] = line.split("=", 1)[1].strip()
    p = subprocess.run(["go", "test", "-p", "1", "-count=1", "-v"] + GO_PKGS + ["-run", GO_RUN],
                       cwd=root, capture_output=True, text=True, timeout=1800, env=env)
    out = ANSI.sub("", p.stdout + p.stderr)
    killed = sorted(set(re.findall(r"^    --- FAIL: (\S+)", out, re.M)) |
                    set(re.findall(r"^--- FAIL: (\S+)", out, re.M)))
    passed = len(re.findall(r"^--- PASS: ", out, re.M))
    failed = len(re.findall(r"^--- FAIL: ", out, re.M))
    skipped = len(re.findall(r"^--- SKIP: ", out, re.M))
    return {"rc": p.returncode, "killed": killed, "total": passed + failed + skipped,
            "passed": passed, "failed": failed, "skipped": skipped, "out": out,
            "panicked": bool(re.search(r"^panic: |^fatal error: ", out, re.M)),
            "buildfailed": "[build failed]" in out or "cannot use" in out or "undefined:" in out}


def cells() -> list[tuple[str, str, list[tuple[str, str, str]], tuple[str, ...]]]:
    """(格, 说明, [(文件槽, 原文, 注码), ...], 该红的腿)"""
    return [
        # ---- 写侧：摘要作为删除的前置条件
        ("D1", "摘要根本不落库（裁剪退回成纯删除，凭据整份消失）",
         [("cmdlog", A_CREATE, A_CREATE_SKIP)], (R_COVER, R_CLOSE)),
        ("D2", "摘要挪出事务旁路补记、写不上也不管（fail-close 折成 fail-open）",
         [("cmdlog", A_CREATE, A_CREATE_SIDE)], (R_WBLOCK,)),
        ("D19", "摘要写在另一条连接上、错照旧报（删除回滚了摘要还在＝说谎的凭据）",
         [("cmdlog", A_CREATE, A_CREATE_OUTSIDE)], (R_MIDFAIL,)),
        # ---- 写侧：链的三个分量各自一格
        ("D3", "链哈希不看前一条（补一段假历史零代价）",
         [("cmdlog", A_PREV, A_PREV_NOCHAIN)], (R_CHAIN,)),
        ("D4", "prev_seq 不接上一条的尾巴（seq 区间首尾不接，中间那段无从交代）",
         [("cmdlog", A_PREV, A_PREV_NOSEQ)], (R_CHAIN,)),
        ("D5", "批序永远写 1（重放同一次裁剪不再撞约束，而是并行出第二条历史）",
         [("cmdlog", A_PREV, A_PREV_NOORD)], (R_CHAIN,)),
        ("D21", "链头改接最老一条摘要（三轮之后新摘要撞回 ordinal=2、整轮回滚）",
         [("cmdlog", A_LASTDIGEST_DESC, A_LASTDIGEST_ASC)], (R_CHAIN3,)),
        ("D15", "摘要表的 (session, ordinal) 唯一索引退回普通索引",
         [("model", A_IDX_1, "index:ix_a6_session"), ("model", A_IDX_2, "index:ix_a6_ordinal")],
         (R_STORE,)),
        # ---- 写侧：批摘要对内容的敏感性
        ("D9", "payload 不进逐行哈希（摘要证不了删掉的是哪些内容）",
         [("cmdlog", A_ROWHASH_PAYLOAD, "")], (R_PAYLOAD,)),
        ("D10", "created_at 按会话时区渲染（同一批行在 UTC 主库与异地副本上折出不同指纹）",
         [("cmdlog", A_ROWHASH_TZ, A_ROWHASH_TZ_RAW)], (R_REPRO,)),
        ("D11", "ok 的三态折成两态（无回执被说成回执为否）",
         [("cmdlog", A_ROWHASH_OK, A_ROWHASH_OK_FOLD)], (R_OK3,)),
        ("D22", "ok 的 t/f 折成一态（回执为是与回执为否同指纹，A2 那根轴在摘要里复活）",
         [("cmdlog", A_ROWHASH_OK, A_ROWHASH_OK_TF_FOLD)], (R_OKTF,)),
        # ---- 写侧：批次切分与算术闭合
        ("D8", "分批上界形同虚设（一条 DELETE 吃全表：收回 b19g 的分批承诺）",
         [("cmdlog", A_LIMIT, "Raw(pruneWindowSQL, cutoff, highwater, int64(1)<<40)")],
         (R_SPLITS, R_B19G)),
        ("D16", "入口行数不记（RowsBefore 恒 0：闭合算术失去分母）",
         [("cmdlog", A_ROWSBEFORE, "\t\tRowsBefore: 0,\n")], (R_CLOSE,)),
        ("D20", "入口行数换成收口累计（rows_before==rows_pruned 从一句断言退化成恒等式）",
         [("cmdlog", A_ROWSBEFORE, A_ROWSBEFORE_TOTAL)], (R_LATECROSS,)),
        ("D23", "行数守卫短路（声明 2 行、实删 3 行的那一批照样提交＝说谎的凭据落进永不可裁的表）",
         [("cmdlog", A_ROWCOUNT_GUARD, A_ROWCOUNT_GUARD_DENT)], (R_GUARDMISMATCH,)),
        ("D17", "留痕按批计数而不是按摘要行（凭条数查账会少一半）",
         [("cmdlog", A_DIGESTCOUNT, "\t\tdigests++\n")], (R_CLOSE,)),
        ("D18", "行数折成区间算术长度（断号被抹平：§7.28 八-1 的「区间长度 ≠ 行数」）",
         [("cmdlog", A_ROWCOUNT, A_ROWCOUNT_SPAN)], (R_GAP,)),
        ("D7", "扫描不留痕（0 行的那次扫描再也自证不了「该界内无到期行」）",
         [("cmdlog", A_RUNROW, A_RUNROW_SKIP)], (R_EMPTY, R_CLOSE)),
        # ---- 读侧：从库到导出包的三跳
        ("D14", "读侧按入库序返回（链序丢失，离线包得自己重排才能接链）",
         [("reader", A_ORDER, 'Where("session_id = ?", sessionID).Order("id ASC")')],
         (C_PAYLOAD,)),
        ("D12", "服务层不去读摘要（导出包对「被裁掉的那段」彻底沉默）",
         [("svc", A_SVC_READ, "\tif s.digestRepo != nil && false {\n")], (S_EXPORT, C_PAYLOAD)),
        ("D13", "控制器换了个键名（最后一跳丢契约，前端按 audit_digests 读永远读不到）",
         [("ctl", A_CTL_KEY, '\t\t"audit_cmds": digests,\n')], (C_PAYLOAD, C_STATE)),
        # ---- 批23（§7.28 八-3）：界的「来源」这一条链，从入口到落库逐跳一格
        ("V1", "空来源不再拒绝（缺来源的裁剪照跑，库里多一条说不清从哪来的界）",
         [("cmdlog", A_SOURCE_GUARD, A_SOURCE_GUARD_SKIP)], (R23_REFUSE,)),
        ("V2", "留痕行的来源列写成空串（参数接住了却不落库＝这一列永远查不出东西）",
         [("cmdlog", A_SOURCE_VALUE, A_SOURCE_VALUE_BLANK)], (R23_RECORD,)),
        ("V3", "扫描入口传死值而不是现算的来源（改了 env，库里仍写着第一天那个界从哪来）",
         [("retention", A_SVC_SOURCE, A_SVC_SOURCE_HARD)], (R23_WIRE,)),
        ("V4", "扫描不看禁用开关（0=禁用折成「界=今天」的全量裁剪）",
         [("retention", A_SVC_DISABLE, A_SVC_DISABLE_SKIP)], (R23_ZERO,)),
        ("V5", "来源截断的列宽放开（超长 env 值让留痕 INSERT 失败，裁剪被一个记账字段堵死）",
         [("retention", A_SOURCE_LEN, A_SOURCE_LEN_WIDE)], (R23_SHAPE,)),
        ("V6", "启动门不看仓储是否装配齐（nil 仓储起 goroutine，一小时后后台 panic 带走进程）",
         [("retention", A_BOOT_GATE, A_BOOT_GATE_SKIP)], (R23_BOOT,)),
    ]


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--keep", action="store_true")
    ap.add_argument("--clone", default="")
    ap.add_argument("--cells", default="", help="逗号分隔的格名；留空=全跑")
    args = ap.parse_args()
    want = [c.strip().upper() for c in args.cells.split(",") if c.strip()]
    unknown = [c for c in want if c not in {d[0] for d in cells()}]
    if unknown:
        raise SystemExit("--cells 里有未知格名：" + " ".join(unknown) + "（宁可停机也别悄悄少跑几格）")

    tmp, owned = workdir(args.clone or None, prefix="a6mut-", repo_root=ROOT)
    tmp.mkdir(parents=True, exist_ok=True)
    print(f"私有作业目录：{tmp}", flush=True)
    if want:
        print(f"[Go] 本趟只跑 {len(want)} 格：{' '.join(want)}", flush=True)
    problems: list[str] = []

    clone = go_prepare(tmp)
    files = {slot: clone / rel for slot, rel in SLOT_FILES.items()}
    for name, p in files.items():
        if not p.exists():
            raise SystemExit(f"注码目标文件不在克隆里：{p}")
    originals = {name: read(p) for name, p in files.items()}
    basemd5 = {name: md5_bytes(p) for name, p in files.items()}

    prepared = []
    for code, desc, edits, expect in cells():
        if want and code not in want:
            continue
        acc: dict[str, str] = {}
        for slot, old, new in edits:
            cur = acc.get(slot, originals[slot])
            hit = cur.count(old)
            if hit != 1:
                raise SystemExit(f"{code} 锚点在 {slot} 里命中 {hit} 次（要求恰好 1）：{old[:70]!r}")
            if old == new:
                raise SystemExit(f"{code} 注码无效（原文与注码后一致）")
            acc[slot] = cur.replace(old, new, 1)
        for slot, src in acc.items():
            if src == originals[slot]:
                raise SystemExit(f"{code} 注码无效（{slot} 替换后与原文件一致）")
            ok, err = syntax_ok_go(src)
            if not ok:
                raise SystemExit(f"{code} 注码语法坏，跑出来只会是 build failed：{err[:160]}")
        prepared.append((code, desc, acc, expect))
    print(f"\n[Go] 注码前置：{len(prepared)} 格锚点各命中一次 + 注码后语法可解析")

    r = go_run(clone)
    CONTROL["go"] = r["total"]
    print(f"[Go] 控制组 rc={r['rc']} total={r['total']} passed={r['passed']} "
          f"skip={r['skipped']} FAIL={r['killed']}")
    if r["rc"] != 0 or r["total"] == 0 or r["skipped"] > 0 or r["killed"]:
        print(r["out"][-4000:])
        raise SystemExit("[Go] 控制组不干净——后面所有红/绿都不可信")

    gkill: dict[str, set] = {}
    for code, desc, acc, expect in prepared:
        for slot, src in acc.items():
            files[slot].write_text(src, encoding="utf-8")
        for slot in acc:
            if md5_bytes(files[slot]) == basemd5[slot]:
                raise SystemExit(f"{code} 注码未生效（{slot} 与原内容一致）")
        r = go_run(clone)
        v = verdict(r, CONTROL["go"])
        if v == "杀掉" and not all(e in r["killed"] for e in expect):
            miss = [e for e in expect if e not in r["killed"]]
            v = f"红了但没点出 {' '.join(miss)}"
        print(f"{code:<4} {desc[:58]:<60} {v:<7} total={r['total']} fail={r['failed']} "
              f"skip={r['skipped']} ｜ " + " | ".join(k[:60] for k in r["killed"][:3]))
        if not v.startswith("杀掉"):
            problems.append(f"[Go] {code} {v}：{desc}")
            print(r["out"][-3000:])
        gkill[code] = set(r["killed"])
        for slot in acc:
            files[slot].write_text(originals[slot], encoding="utf-8")
        for slot in acc:
            if md5_bytes(files[slot]) != basemd5[slot]:
                raise SystemExit(f"{code} 还原后 md5 不一致，停机")

    dup_report(gkill)
    print("[Go] 已全量还原（md5 一致）")

    dispose(tmp, owned=owned, keep=args.keep, repo_root=ROOT)
    if problems:
        print("\n===== 电池判定：有洞 =====")
        for x in problems:
            print("  ✗", x)
        return 1
    print(f"\n===== 电池判定：Go {len(prepared)} 格逐格被杀，无存活"
          + ("（本趟是 --cells 子集，不等于全电池）" if want else "（全电池）") + " =====")
    return 0


if __name__ == "__main__":
    sys.exit(main())
