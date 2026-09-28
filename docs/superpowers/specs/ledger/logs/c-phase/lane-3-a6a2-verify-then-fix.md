# C 相续刀 · lane 3：A6 四刀 + A2 一刀，逐条做成常驻格并跑

## 为什么还有这一刀要跑

`docs/superpowers/specs/ledger/logs/b-phase/lane-3-log-prune-findings.md` 是 B 相只读攻击线的五条候选
（自陈「候选均未实际注码运行」）。02:47 那批按其中三条各写了新腿
（`internal/browser_automation/repository/retention_a6_chain_prune_test.go`），
但**这五把刀在仓内电池里没有一格**：`scripts/mut_retention_a6.py` 末次改于 09-22 23:48（早于新腿），
`scripts/mut_command_log_ok_b20b.py` 末次改于 09-22 11:25。既有格钉的是**相邻的另一把刀**，
逐条对过（这条判断本身也要在动手前复核，发现我判错了要指出）：

| 既有格 | 它注的刀 | lane 3 的刀 | 同不同意 |
|---|---|---|---|
| D11 | ok 三态折两态（`nil` 与 `false` 同指纹） | `CASE ... WHEN ok THEN 't' ELSE 'f'` 删掉 ⇒ **true 与 false** 同指纹 | 不同刀 |
| D14 | 读侧按入库序返回 | 链头 `Order("ordinal DESC")→ASC` | 不同刀 |
| D16 | `RowsBefore: 0` | `RowsBefore: total`（恒等式变恒真） | 不同刀 |
| D2 | 摘要挪出事务、写不上也不管 | `tx.Create(&d)→r.db.WithContext(ctx).Create(&d)`（摘要与 DELETE 拆两条连接） | 不同刀 |
| b20b M1 | **command 帧**退回带结论 | **失败回包（event 帧）**的 `verdict(err == nil)→verdict(true)` | 不同刀 |

## 五条刀 ↔ 该红的腿（行号来自 02:2x 的报告，动手前按内容重定位）

| 刀 | 注码 | 该红的腿 |
|---|---|---|
| D19 | `internal/browser_automation/repository/command_log.go` 摘要落库 `tx.Create(&d)` → 换 `r.db.WithContext(ctx).Create(&d)`（事务内改成事务外另取连接） | `TestA6PruneFailsCloseWhenDeleteFailsMidRun`（chain_prune:197） |
| D20 | 同文件 `RowsBefore: rowsBefore` → `RowsBefore: total` | `TestA6RunRowClosesArithmeticUnderTwoBatches`（chain_prune:290） |
| D21 | 同文件 `Order("ordinal DESC")` → `Order("ordinal ASC")` | `TestA6ChainLinksToNewestDigestAcrossThreeRounds`（chain_prune:79） |
| D22 | 同文件 `CASE WHEN ok IS NULL THEN 'n' WHEN ok THEN 't' ELSE 'f' END` → `CASE WHEN ok IS NULL THEN 'n' ELSE 't' END` | 现测：B 相说只有 `TestA6DigestSeparatesUnackedFromFailedFrame`（retention_a6_test.go:586），而它对照的是 nil/false ⇒ **预判存活**。存活就是真缺口，按下面「存活怎么处置」办 |
| M7 | `internal/browser_automation/service/executor.go` 失败回包那两处 `verdict(err == nil)` / `verdict(false)`（`:1308`、`:1077` 附近，按内容定位）各自单独一刀 | 预判同样存活：`TestB20BCommandFramesCarryNoVerdict` 只钉 command 帧。存活就补腿 |

**动手前先点腿**：`go test -list '^Test<腿名>$' ./internal/browser_automation/repository/`（service 包同理），
点不到就是腿不存在，停下回报，不许给没跑的腿建格。

## 环境闸门

这批腿全部走真库（`testutil` 同族口径：连不上 PG 会 Skip 且整包绿）。开工先跑不注码的五条腿，
要求逐条 `--- PASS`；出现 `--- SKIP` ⇒ 先按 `pg_isready -h localhost -p 8232` 证库在，
口令只从 `user-server/.env` 用 awk 精确取 `POSTGRES_PASSWORD` 喂 `POSTGRES_TEST_PASSWORD`
（`.env` 有重复键，**永不打印口令**）。拿不到库就停下回报。

## 交付形态：改既有电池，不是一次性脚本

- D19–D22 加进 `scripts/mut_retention_a6.py` 的 `cuts()`，**照它既有格形状**（锚点完整语法块、
  命中必须恰好一次、`gofmt -e` 过一遍、还原比 md5、控制组放刀前现测不写死）。
- M7 加进 `scripts/mut_command_log_ok_b20b.py`。
- 两枚电池各自**全量跑**（22→26 格、6→7 格）：加完格只跑新格不算结论，旧格必须同趟复跑，
  因为它们共用的锚点会被上一次改动漂掉。
- 电池只在私有 `git clone --shared --no-checkout` 里注码，**绝不碰共享工作树**；
  本泳道新增/改动的脏文件（含 `retention_a6_chain_prune_test.go` 这类未跟踪新腿）必须进 overlay 名单——
  **这一条踩过**：新腿文件没进克隆，电池把「克隆里没这条腿」读成「刀存活」。

## 存活怎么处置（D22/M7 大概率走这条路）

1. 先证明存活不是取证假象：红因里没有该腿的名字 ⇒ 看它是否**根本没跑**（`-run` 名单漂、
   overlay 没带上、包编译 BROKEN）。排除后才算真存活。
2. 真存活 ⇒ 缺口成立，补一条窄腿：
   - D22：断「同一窗口里 ok=true 与 ok=false 两帧必须落进不同摘要指纹」（拿真 `pruneWindowSQL`/`auditRowHashSQL` 复算，
     不许把 SQL 常量抄进测试——抄了就是自证）。
   - M7：断「下发失败时 event 回包帧的 ok 必须是 false 且不是 nil」，走真 `dispatchStep` 出路，
     不许手工往仓储里塞一行再造期望。
   两条腿各自**必须红过一次**（注码下红、还原后绿），读数逐字进日志。
3. 补完重跑**整电池**（不是那一格）。
4. 结构上补不动（例如断言只能靠把 SQL 抄进测试）⇒ 记 `refuted` 并写清哪一面挡住（符号 + 文件:行），
   不许写「待评估」。

## 边界（协议 §5）

- 只许动：`scripts/mut_retention_a6.py`、`scripts/mut_command_log_ok_b20b.py`、
  `docs/superpowers/specs/ledger/logs/R22-lanes/`（新日志），以及**确实存活时**的
  `internal/browser_automation/repository/retention_a6_test.go` /
  `internal/browser_automation/service/` 下的 b20b 测试文件。
- 不许动 `command_log.go` / `executor.go` 的**共享工作树**版本（注码只在克隆里）；
  不许动 `internal/service/**`、`user-web/bridge/**`（另两条泳道在跑）；
  禁止 `git stash` / `git checkout` / `git restore` / 整文件快照写回。
- 不许为了「我的刀红了」去改既有腿的期望或判据。

## 收口口径与回报

整包全量归 E 相合并门禁，本泳道不跑。日志名不许带结论（红写 red、绿写 green）。
回报 ≤400 字：五把刀各「腿名 → 注码 → 实际读数逐字 → 杀掉/存活 → 若存活：新腿文件:行 + 它红过的读数」，
末行写「本轮没跑的」。
