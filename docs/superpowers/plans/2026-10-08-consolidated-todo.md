# 全项目未完成功能/计划 · 汇总 TODO 清单（2026-10-08）

> 来源：①`docs/superpowers/plans/2026-09-30-outreach-p0-backlog.md`（自动触达 P0，进行中）
> ②`docs/replan-2026-09/新规划任务清单.md`（P0–P9 共 56 卡，T-P9-03/04 未收口）
> ③`docs/governance/78-OPTIMIZATION-TASKS.md`（86 条，未完成 11 + 部分完成 5）
> ④`docs/architecture/MASTER_FEATURE_INVENTORY.md`（实测缺口列）
> ⑤`.sisyphus/cycle_state.json`（known_issues 遗留）
> 逐项已用 grep/源码核实，非文档转述。

---

## A. 自动触达 P0（browser_automation × JEV）— 代码级缺口，本次全部收口

前置：`docs/superpowers/specs/2026-09-30-outreach-product-design.md`

### A1 Chunk3 剩余：触达去重接线 ✅ **本轮完成**
- [x] A1-1 `executeStepWithRetry` 加 `pageURL` 尾参（3 调用点：653 显式 `""`、926 brain 传 pageURL、test57 `""`）
- [x] A1-2 `dispatchStep` 加 `outreachCtx` 尾参（唯一调用方 1116）
- [x] A1-3 写步去重检查点：`writeKey` 之后、`guardResubmit` 之前，`resolveOutreachDedupe` 命中 → `finishStep skipped` + 审计帧绿返
- [x] A1-4 `verified` 分支插 `recordOutreachDedupeSend`（warn-only，不判红）
- [x] A1-5 迁移 `v3_50_0_browser_outreach_dedupe_migration.go`：建 `browser_outreach_dedupe`（四元组唯一键）+ 测试 + `initial_schema.go` 注册（现行最新 v3.49.0）
- [x] A1-6 routes 装配：`WithDB` + `SetOutreachDedupeRepository`

### A2 Chunk4：活动预算 + 时间线可观测 ✅ **本轮完成**
- [x] A2-1 task 模型加活动预算字段（现 `model/task.go` 只有 D7 确认/执行两条时间预算，无活动级 token/成本预算）
- [x] A2-2 dispatcher 调用点预算扣减（超预算 → 优雅停，非 panic）
- [x] A2-3 审计帧每步补 model / token / latency 三字段

### A3 Chunk5：触达回执留存 ✅ **本轮完成**
- [x] A3-1 DONE 收口：截图 + 帖子链接 + 文案快照入库
- [x] A3-2 前端：`user-web/src/views/browserAutomation/Detail.vue` 展示回执

---

## B. 死代码 / 未接线资产（已逐个 grep 核实）

| 项 | 实测 | 处置 |
|---|---|---|
| `NewAIAgentController` | defs=1，非测试调用 0，测试引用 0 | 删除 |
| `NewChannelAgentBindingController` | defs=1，调用 0/0 | 删除 |
| `NewCustomerServiceAgentController` | defs=1，调用 0/0（`*WithService` 在用） | 删除 |
| `NewCustomerServiceControllerWithService` | defs=1，调用 0/0（普通版在 `service_routes.go:74`） | 删除 |
| `NewPromptControllerWithService` | defs=1，调用 0/0（普通版在 `business_routes.go:324`） | 删除 |
| `NewUserController` | defs=1，非测试调用 0，测试引用 18 | 评估：测试自造，评估删或接线 |
| `NewChatWSHub` | defs=1，非测试调用 0，测试引用 18 | 同上 |
| `NewDashboardSSEController` | defs=1，非测试调用 0，测试引用 11；**3 个 handler（StreamEventStream/Snapshot/Metrics）无 `RegisterRoutes`，运行时 404** | **接线**（真实未接线资产） |
| `internal/aiagent/rag/customer_service/quality_assessor.go` `GetQualityMetrics` | 全 0.0 死桩，零调用方 | 删接口或接线 |
| `email_tracking.go` `ClickRedirect` 注释「取 query url」 | 实现只读 DB target，注释漂移 | 改注释 |

---

## C. 新规划剩余（`docs/replan-2026-09/新规划任务清单.md`）

P0/P1/P2…P8 共 52 卡均有「执行结果」回灌，仅剩：
- [x] **T-P9-03**（本轮核实：脚本里 `run_ltc` 早已完整落地，L0 总闸快照 → L12 链路事件共 29 条 `check()`，写操作均带【DB】断言，无需新写） `scripts/api_verify_full.py` 新增 `ltc` 子命令：线索→商机→报价→审批→webhook 回款→赢单→复购，每端点【入参】【返回】【预期】【DB】四维
- [x] **T-P9-04** 第六步全量一次性回归（§2 的 12 步）

---

## D. 优化任务清单（`docs/governance/78-OPTIMIZATION-TASKS.md`）

### D1 本次可快速收口
- [x] **OPT-FE-07 残项①** `website` 锁文件 vite `8.1.4` → 与其余 6 个对齐 `8.3.0`
- [x] **OPT-FE-07 残项②** `scripts/check-vite-version.sh` 已建未注册 → 接入 `make audit` / CI
- [x] **OPT-DOC-EXT-4** `.github/workflows/dco.yml` 触发条件 `on: pull_request`，而本项目直推 master ⇒ 永不触发；改 `on: push`
- [ ] **OPT-SEC-04 残项**（本轮未吞：`audit_logs` 加密是 schema 级改造，与 A 段 browser_automation 热区同批改会让回归面失真，独立排期） `audit_logs` 敏感字段加密（`api_logs` 侧已由 `migrations/057_api_logs_encrypt_fields.sql` 覆盖）

### D2 需跨日工程量（本次不吞，登记在册）
- OPT-FE-04 补 i18n 缺失 key（P1）
- OPT-FE-05 抽 `@hivemtk/http` 共享包（P2）
- OPT-FE-06 platform-web 补 i18n（P2）
- OPT-FE-12 user-web PWA 评估（4d，P3）
- OPT-FE-13 a11y 接入（P2）
- OPT-FE-14 首屏 LCP 监控（P2）
- OPT-DOC-04 平台端 Swagger 覆盖 100%（现 7/13 = 54%）
- OPT-DOC-05 用户端 Swagger 覆盖 80%+（现 31/208 = 15%）
- OPT-SEC-06 10w+ chunks RAG 压测（2d）
- OPT-SEC-08 secrets 支持 Vault / KMS（3d）

### D3 文档自身失真（需回写）
- [x] 「🚧 未完成清单（11 项）」表仍列 `OPT-FE-03`，但正文条目标 `[x] ✅ 2026-09-15 闭环` ⇒ 汇总表与正文互相矛盾。
  **本轮已回写**：从该表移除 `OPT-FE-03` 行并把表头计数从 11 改为 10。

---

## E. 功能盘点实测缺口（`docs/architecture/MASTER_FEATURE_INVENTORY.md`）

| 项 | 实测缺口 | 规模 |
|---|---|---|
| 用户画像推导 | `Tags/Interests/RiskLevel/PreferredTime` **恒空串**；Clue Level 恒 `"warm"` | 中 |
| 异议响应编排 | `UseLLM` 字段定义未实现（TECH_DEBT M9 称已删字段 ⇒ 文档漂移，需二选一定案） | 小 |
| 分渠道验签 | `wechat` secrets 恒空 → 永远失败；抖音泛化 HMAC 与官方口径不同 | 中（安全） |
| 消息下发 | `msg_id` 内容哈希致同会话同文案无法重发 | 中 |

---

## F. 技术调研已决策未开发（`docs/tech-research/DECISIONS.md` D1–D12）

12 条全部标「已决策-待开发」，规模大（schema 变更 + 第三方组件），需独立排期：
D1 sparse 头 / D2 `sop_executions.executed_nodes` / D3 Saga 试点 / D4 回流 job /
D5 failsafe-go 熔断 / D6 DBOS spike / D7 greeting 词条拆分 / D8 `ToolResult.ErrorCode` /
D9 循环检测 / D10 异议 LLM 兜底 / D11 配置层收敛 / D12 权限矩阵外置 / D13 Redis WS seq。

---

## 本轮执行范围裁定

本轮**一次做完** = A（3 Chunk 全收口）+ B（死代码删除 + DashboardSSE 接线 + 注释纠偏）
+ C（T-P9-03 + T-P9-04）+ D1（4 项快速收口）。

D2 / E / F 登记在册不吞：E 中「分渠道验签」属安全项，与 A 的写步去重共处 browser_automation
热区，同批改动会让回归面失真；D2/F 均为跨日工程，须独立排期而非搭车。

---

## 本轮执行结果（2026-10-08）

### A 自动触达 P0 —— 三个 Chunk 全部收口

**Chunk3 触达去重接线**（`executor.go` + `outreach_dedupe.go` + `v3.50.0`）
- `executeStepWithRetry` / `dispatchStep` 各加尾参；检查点排在 `writeStepKey` 之后、`guardResubmit` 之前
- 命中 ⇒ `finishStep skipped` + `dedupe_hit/outreach_already_sent` 审计帧 + **绿返**（errMsg 空 ⇒ 不计成败、不中断）
- 语义修正一处（原实现的实质缺陷）：`resolveOutreachDedupe` 未命中时原返回 `nil`，导致「键未冻结 ⇒ verified 后无行可落 ⇒ 去重永不起作用」。改为命中与未命中**都**返回冻结的键，只有「压根没查成」才为 nil
- 迁移 `v3.50.0` 建 `browser_outreach_dedupe`（四元组唯一键 `uk_outreach_dedupe`），Down 不删表（去重行是「已触达」唯一持久证据）
- 测试 `outreach_dedupe_wiring_test.go` 5 例：判据打在**库里的步行 + 命令日志**上，不看 helper 返回值

**Chunk4 活动预算 + 时间线可观测**（`campaign_budget.go` + `audit_timeline.go` + `v3.51.0`）
- 口径定案：**「触达活动 = 任务本体」**（cron/loop/retry 的所有 session 累计到同一行 `browser_tasks`），预算因此是任务行上的持久累计额度。Executor 是进程级单例，硬约束「禁内存计数」在这里同样成立
- `BrowserTask` 加 4 字段（`TokenBudget` / `CampaignKey` / `CampaignActBudget` / `CampaignActUsed`，0/空 = 不限）
- 扣减是**一条条件更新语句**判出来的（`WHERE ... AND campaign_act_used < campaign_act_budget` → `RowsAffected`），不是 check-then-act：多副本超发一次 = 不可逆动作真发出去
- 三条 fail-open 各自有具体理由（未配置 / repo 未接线 / DB 报错）：预算的失败形态是多打扰几次，锁死整条触达线的失败形态更大
- `BrainService` 加 `LastPlanModel()`，每步 event 帧补 `llm_model` + `llm_tokens` + `latency_ms`；非 Brain 模式**不写该字段**（空串会被读成「模型名叫空」）
- 测试：service 11 例（含纯函数层与审计元数据层）+ repository 4 例（并发 8 腿抢 3 条预算，判 `granted==3 && used==3`，专门否掉 check-then-act）

**Chunk5 触达回执留存**（`outreach_receipt.go` × 3 层 + `v3.52.0` + 前端）
- **不并进去重表、不复用 `extracted_data`**，三条理由写进 model 头注释：去重表唯一键 + `OnConflict{DoNothing}` 会静默吞插入 ⇒ 「已发出但无回执」与「这步没执行过」不可区分（而回执的唯一用途正是回答这个区分）；去重行是闸门输入、回执行闸门输出；JSONB 追加数组无法按帖子链接/文案检索
- 落点铁律：`captureSessionReceipts` 排在 `cleanupSessionTab` **之前** —— tab 一回收，`captureVisibleTab` 就只能截到用户自己的页面（静默假内容，比没有更坏）
- `verified=false` 的 unattributed **照样落回执**：回执只留绿的等于替平台做了裁决，而双发闸正是因为分不清才把人叫来
- 回执的帖子链接**不挂在去重层是否装配上**（去重键没冻结时自己拍一次只读快照兜底）—— 回执完整性不该被闸门层的接线状态决定
- 读侧 `GET /api/browser-automation/tasks/:id/receipts`，归属校验先过任务再查回执（回执行无 `user_id` 列）
- 测试：service 7 例 + repository 4 例

### B 死代码 / 未接线资产
- 删除 5 个零引用构造函数（`NewAIAgentController` / `NewChannelAgentBindingController` / `NewCustomerServiceAgentController` / `NewCustomerServiceControllerWithService` / `NewPromptControllerWithService`）；`*WithService` 版本在用，保留
- **接线 `DashboardSSEController`**：3 个 handler（`StreamEventStream` / `Snapshot` / `Metrics`）此前无任何装配点，实时驾驶舱 SSE 运行时一律 404。加签 `setupSSEDashboardRoutes(auth, gormDB)` + 三条路由；补 `dashboard_sse_wiring_test.go` 判「handler 名 + 具体 URL + 控制器实例化 + statsSvc 注入」四件事都在位
- 未动：`NewUserController` / `NewChatWSHub`（测试引用各 18 处，删构造会让测试编译不过，属另一决策面）

### D1 快速收口
- **OPT-FE-07**：website 锁文件 `8.1.4` → `8.3.0`（现 7/7 pin `^8.1.1`、7/7 lock `8.3.0`）；护栏 `check-vite-version.sh` 注册进 `make audit`
- **OPT-DOC-EXT-4**：`dco.yml` 补 `on: push: branches: [master]`。**阻断强度按事件分档**而非照抄 PR 侧硬阻断 —— 实测本仓现有历史 30 个 commit **零个**带 Signed-off-by，若 push 侧也硬阻断，第一次直推就会把这条线永久锁死；补触发面的目的是让闸跑起来，不是让闸变成没人能过的墙。同时新增 `range` step 统一解析两种事件的 commit 区间（PR 用 base..head，push 用 before..sha 且对「首推 before 全零」「before 被 GC」两种形态各留回落）

### 全量回归读数（T-P9-04）

| 项 | 读数 |
|---|---|
| `go build ./...`（CGO_ENABLED=0） | rc=0 |
| `go vet ./...` | rc=0 |
| `gofmt -l`（全仓 go 文件） | 0 行 —— **顺带修掉 HEAD 上预存的 5 个文件**（`model/profile_health.go`、`geo/service/{citation,opportunity,opportunity_test,search_probe}.go`，纯字段对齐空白，按规则2「发现即修复」当轮清零） |
| browser_automation 全包 / migration / router / controller | 全 ok（service 220s、repository 36s、controller 98s、migrations 23s、router 14s） |
| 前端 `lint:check` | rc=0（0 errors / 18959 warnings，warn 为项目基线，不阻断） |
| 前端 `vitest run` | **30 files / 413 tests 全通过** |
| 前端 `i18n:check` / `check-i18n-coverage.cjs` / `build` | 全部 rc=0 |
| `make audit` 19 道静态闸 | 全 rc=0 —— **期间修掉 2 处预存红灯**（见下） |

**回归期间按规则2 当轮修复的 2 处预存红灯**（均为存量缺陷，非本轮引入）：
1. `check-env-coverage.py` rc=1：6 个生产读取的 env 键未进文档面。**处置方式是补文档而不是登记进基线** —— 基线表自己在头部写明「本表的作用是挡住新增，不是给存量发合格证」，理由一律是「待补文档」。在 `docs/DEPLOYMENT_GUIDE.md` §6.2 补 `BROWSER_JEV_ENABLED` / `BROWSER_JEV_ENDPOINT` / `BROWSER_JEV_API_KEY` / `BROWSER_JEV_MODEL` / `BACKUP_BASE_DIR` / `RESTORE_TMP_DIR` 六行后转绿（读数：生产读取 192 · 已文档化 88 · 红 0）
2. `check-shell-cjk-expansion.sh` rc=1：`scripts/inference-host/start-laya.sh:41` 的 `$LAYA_DIR` 紧跟中文，`$VAR` 后接中文在 bash 3.2 下会按字节吞掉变量名。按闸门给的改法加花括号（`${LAYA_DIR}`，语义不变）后回到基线

### 未吞项（理由在册）
- **OPT-SEC-04 残项**（`audit_logs` 敏感字段加密）：schema 级改造，与 A 段 browser_automation 热区同批改会让回归面失真
- **D2**（OPT-FE-04/05/06/12/13/14、OPT-DOC-04/05、OPT-SEC-06/08）与 **E/F**：跨日工程量，须独立排期
- **E 的分渠道验签**（wechat secrets 恒空 / 抖音 HMAC 口径）：安全项，且与 A 段共处同一热区，本轮刻意不与去重改动同批
- `NewUserController` / `NewChatWSHub`：测试各引用 18 处，删构造会破测试编译，属独立决策面
