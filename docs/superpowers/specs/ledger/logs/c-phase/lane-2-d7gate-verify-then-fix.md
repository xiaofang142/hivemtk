# C 相续刀 · lane 2：D7 闸门四把二手候选刀必须成格跑出来

## 为什么还有这一刀要跑

`docs/superpowers/specs/ledger/logs/b-phase/lane-2-d7-gate-findings.md` 是 B 相只读攻击线给的
四条候选（自称「未验证：候选均未实际注入运行」）。02:43 那批已按四条各写了一条补刀腿
（`internal/browser_automation/service/d7_gate_b20_test.go`，注释里逐条写了「findings#N」），
**但四把刀在仓内任何变异电池里都没有格**——`grep d7PreviewRunes scripts/*.py` 零命中。
本仓口径：腿注释里写「专杀 X」不等于 X 被注过。本泳道的活＝把四把刀做成常驻电池、真跑、出读数。

## 四条腿 ↔ 四把刀（行号是 02:2x 报来的，动手前一律按内容重定位）

| 刀 | 注码（精确原文 → 注码后） | 该红的腿 |
|---|---|---|
| G1 | `internal/browser_automation/service/executor.go` `const d7PreviewRunes = 200` → `= 0` | `TestAwaitConfirmGatePreviewTruncatesSubmittedText` |
| G2 | 同文件 `:509` 那一段 judge 帧字段里的 `"step_index":        stepIndex,` → `... 0,`（**只动 judge 帧，:497 的 d7_wait 帧一个字不许碰**——那是既有腿的靶子，动了会把「新腿有没有牙」证成「旧腿有没有牙」） | `TestConfirmGateFramesBindPayloadAndOmitBody` |
| G3 | `internal/browser_automation/service/session.go:142` `l.Seq > latest.Seq` → `l.Seq < latest.Seq` | `TestGateElsewhereFollowsNewestWaitFrame` |
| G4 | `executor.go:498` `"expires_at": gate.expiresAt.Format(time.RFC3339Nano),` → 写成 `time.Now().Add(24*time.Hour).Format(...)` 的字面等价注码 | `TestAwaitConfirmGateDeadlineMatchesFrame` |

**动手前先做两件事**：① `grep -n "func Test" .../d7_gate_b20_test.go` 确认四条腿真在（名字漂了以真名为准，
并在报告里写明漂了）；② `go test -list '^Test<每条腿>$' ./internal/browser_automation/service/` 逐条点到，
点不到就是「腿不存在」——那要停下回报，别给一条没跑的腿建格。

## 环境闸门（做不到就别往下走）

这批腿走真库/替身两条路。开工先跑**不注码**的四条腿，要求逐条看到 `--- PASS`；
出现 `--- SKIP` ⇒ 库没连上（口令只从 `user-server/.env` 用 awk 精确取 `POSTGRES_PASSWORD`
喂 `POSTGRES_TEST_PASSWORD`，`.env` 有重复键，**永不打印口令**），`pg_isready -h localhost -p 8232` 先证库在。
**拿不到库就停下回报**——交一条「跳过即绿」的电池是本仓最贵的假绿。

## 交付形态：常驻电池，不是一次性脚本

新建 `scripts/mut_d7gate_r22lane.py`，**照抄 `scripts/mut_d7verdict_b20g.py` 的骨架**
（它是同目录同包最近的一版，克隆/overlay/控制组/判格/md5 全在），并守住它的每一条口径：

- 只在私有 `git clone --shared --no-checkout` 里注码，**绝不碰共享工作树**（别的泳道在飞）；
  overlay 从 `git status --porcelain -uall` 取本泳道脏文件，新腿文件必须进 overlay 名单
  （**这一条踩过：新腿没进克隆 ⇒ 电池把「克隆里没有这条腿」读成「刀存活」**）。
- 控制组必须 `rc==0 && total>0 && skip==0 && 无红`，且 total **放刀前现测**，不许写死常量
  （共享树下别的泳道加用例，写死就是假红/假绿）。
- 每格断言 `PASS+FAIL == 控制组数`；BUILD FAILED / panic / 红而没点名 一律 `BROKEN`，**不计入杀掉**。
- 锚点命中必须恰好一次；注码后先过 `gofmt -e` 再跑；还原比 md5。
- 一格可以叠多处注码，但要「内存叠完一次写盘」，不许逐格 apply 把前一格还原掉。

## 读数与处置

日志 `docs/superpowers/specs/ledger/logs/R22-lanes/mut_d7gate_r22lane-run.log`
（**文件名不许带结论**：红就写 red、绿就写 green，别把 rc=0 的复跑存成 `*-red.log`）。
每格三件事逐条写：注码原文 / 实际红因（`--- FAIL` 那行 + `t.Fatalf` 的话，逐字）/ 判「杀掉」或「存活」。

- **存活** ⇒ 缺口成立：补/修那条腿到能杀，重跑整电池（不许只跑那一格当结论）。
- **注码即编译红 / BROKEN** ⇒ 不算牙，换一把语义刀（同一条承诺、能编译、改变行为）再跑，
  并在报告里写明为什么换刀。
- 四条刀全部被杀 ⇒ 报告写「四格逐刀被杀、无存活」，并说明这四条腿自此有常驻格。

## 边界（协议 §5）

- 只许动：`scripts/mut_d7gate_r22lane.py`（新建）、`docs/superpowers/specs/ledger/logs/R22-lanes/`（新日志）、
  以及**确实杀不掉时**的 `internal/browser_automation/service/d7_gate_b20_test.go`。
- 不许动 `executor.go` / `session.go` 的共享工作树版本（注码只发生在克隆里）；
  不许动 `internal/service/**`、`user-web/bridge/**`（另两条泳道在跑）；
  禁止 `git stash` / `git checkout` / `git restore` / 整文件快照写回。
- 不许为了「我的刀红了」去改别人腿的期望。

## 收口口径

电池跑绿只是本子任务的活；**整包全量**归 E 相合并门禁（`go test -count=1 ./internal/browser_automation/...`，
`-timeout 40m`）。本泳道**不要**跑整包全量（共享树下撞库、且 E 相要跑一次为准）。

## 回报格式（一份，≤400 字 + 日志路径）

四条各：腿名 → 注码 → 实际读数（逐字一行）→ 杀掉/存活 → 若存活：改了什么、重跑读数。
末行照例写「本轮没跑的」：整包全量、真机 e2e。
