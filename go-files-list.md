# user-server Go 源码清单（注释清理任务 · 任务二）

- 范围：`user-server/` 下全部非测试 Go 源码（`*.go`，排除 `*_test.go`）
- 排除：`test/`、`tests/`、`docs/`（生成物 swagger）、`vendor/`
- 基线 commit：`029ccf7a`（master，与 `gitee-upstream/master` 同步，0/0）

## 清理口径（每个文件统一按此判）

**只保留功能性注释**——读完代码后，注释若在解释「代码本身讲不清的事」才留。

删除（属于历史变更注释 / 插装 / 门禁注释 / 推理理由 / 元叙述）：

| 类别 | 判据 | 例子 |
|---|---|---|
| 历史变更叙事 | 讲「以前是…后来改成…」而非当前行为 | 「曾经是全局一份 cache」「2026-10-12 实测」 |
| 变更批次/任务编号 | `R5`/`T-P3-03`/`B-2`/`001`/`AC②`/`N-9` 等轮次标识 | 「（R5 租户化）」「AC③ 的过滤救不了它」 |
| 门禁/插装注释 | 解释「CI 门怎么查」「谁在巡检」「豁免在哪登记」 | 「CI 里的覆盖率测试」「真实兜底是巡检清单」 |
| 推理理由展开 | 长篇论证「为什么这样设计」且结论已写在代码里 | 5 行以上的取舍辩论 |
| 元叙述/自述 | 讲「这一格/这一卡/这条注释」的来路、给下一个人的信 | 「这一卡立此判据时等的那个东西」 |
| 重复代码语义 | 注释只是把下一行代码翻译一遍 | 「// 遍历所有行」之于 `for ... range` |

保留（功能性）：

- **契约与不变量**：长度/单位/取值范围、字符数与字节数、必须成对调用、只能整体替换不可原地改。
- **不可从代码推出的事实**：为何用这张表而非那张、为何单例、为何走直连不走 seam、某键在本仓无写入方。
- **导出的对外语义**：公开 API 的错误含义（400/404/409 各自对应）、字段单位、幂等保证。
- **口径约定**：两个近似概念的确切分界（如「A 与 B 是两条不同管道，禁止合并」）。
- **编译/工具指令**：`//go:build`、`//go:generate`、`//go:embed`、`//nolint`、`// Deprecated:`、`// 五层架构归属`（架构门按它判误命名的 `_test.go`）。

**硬约束**：只改注释。一个字都不许动代码——用 `codeguard`（token 级比对）逐文件证明。

## 统计

- 文件数：**1939**
- 注释块数：**19347**
- 注释行数：**47560**

| 注释块数 | 文件数 | 注释行数 |
|---|---|---|
| 0（无注释） | 100 | 0 |
| 1-3 | 468 | 3325 |
| 4-10 | 787 | 12190 |
| 11-30 | 482 | 19501 |
| 31+ | 102 | 12544 |

## 目录分布

| 目录 | 文件数 | 注释块 | 注释行 |
|---|---|---|---|
| `internal/service` | 372 | 5304 | 13236 |
| `internal/repository` | 197 | 2060 | 4104 |
| `internal/controller` | 185 | 1940 | 5324 |
| `internal/model` | 171 | 1114 | 3169 |
| `internal/migration/migrations` | 97 | 524 | 1055 |
| `internal/app` | 53 | 371 | 1908 |
| `internal/aiagent/agent/tooluse` | 52 | 591 | 1250 |
| `internal/dto` | 45 | 296 | 397 |
| `internal/router` | 43 | 241 | 868 |
| `internal/geo/service` | 42 | 537 | 996 |
| `internal/middleware` | 37 | 322 | 1051 |
| `internal/aiagent/rag/retrieval` | 32 | 304 | 672 |
| `internal/aiagent/knowledge/service` | 29 | 153 | 292 |
| `internal/geo/controller` | 27 | 157 | 260 |
| `internal/browser_automation/service` | 25 | 621 | 1549 |
| `internal/aiagent/agent/runtime` | 24 | 230 | 485 |
| `internal/aiagent/llm` | 23 | 294 | 771 |
| `internal/geo/model` | 22 | 66 | 113 |
| `internal/pkg/utils` | 19 | 138 | 386 |
| `internal/geo/repository` | 18 | 82 | 113 |
| `internal/service/confidence` | 16 | 120 | 405 |
| `cmd/seed` | 14 | 94 | 302 |
| `internal/ops/service` | 12 | 139 | 182 |
| `internal/browser_automation/model` | 11 | 73 | 196 |
| `internal/browser_automation/repository` | 11 | 87 | 225 |
| `internal/service/feedback_loop` | 11 | 58 | 170 |
| `internal/websocket` | 11 | 78 | 194 |
| `internal/aiagent/agent/portcontract` | 10 | 42 | 146 |
| `internal/platform` | 10 | 112 | 268 |
| `internal/bridge` | 9 | 110 | 388 |
| `internal/content/service` | 9 | 90 | 110 |
| `internal/ops/repository` | 9 | 146 | 186 |
| `internal/service/translation` | 9 | 84 | 314 |
| `internal/aiagent/knowledge/repository` | 8 | 106 | 184 |
| `internal/aiagent/rag/customer_service` | 8 | 107 | 180 |
| `internal/config` | 8 | 75 | 291 |
| `internal/geo/dto` | 8 | 41 | 45 |
| `internal/ops/controller` | 8 | 70 | 84 |
| `internal/service/humanize` | 8 | 83 | 174 |
| `internal/content/model` | 7 | 31 | 31 |
| `internal/pkg/sso` | 7 | 35 | 50 |
| `internal/service/trace_learning` | 7 | 32 | 58 |
| `internal/aiagent/eval` | 6 | 48 | 115 |
| `internal/aiagent/knowledge/controller` | 6 | 49 | 72 |
| `internal/browser_automation/controller` | 6 | 79 | 151 |
| `internal/content/controller` | 6 | 94 | 123 |
| `internal/content/repository` | 6 | 53 | 55 |
| `internal/email/service` | 6 | 88 | 198 |
| `internal/ops/model` | 6 | 39 | 50 |
| `internal/aiagent/rag/cache` | 5 | 35 | 64 |
| `internal/cache` | 5 | 76 | 101 |
| `internal/pkg/i18n` | 5 | 25 | 52 |
| `internal/pkg/mail` | 5 | 23 | 91 |
| `internal/storage` | 5 | 33 | 105 |
| `internal/browser_automation/dto` | 4 | 35 | 66 |
| `internal/channelgw` | 4 | 41 | 137 |
| `internal/kbconnector` | 4 | 19 | 34 |
| `internal/pkg/cron` | 4 | 34 | 78 |
| `internal/pkg/tenant` | 4 | 35 | 118 |
| `cmd/api` | 3 | 49 | 170 |
| `internal/pkg/db` | 3 | 56 | 307 |
| `internal/pkg/metrics` | 3 | 57 | 90 |
| `internal/pkg/pagination` | 3 | 20 | 36 |
| `internal/aiagent/agent/lifecycle` | 2 | 28 | 87 |
| `internal/aiagent/mcp` | 2 | 37 | 51 |
| `internal/aiagent/rag/eval` | 2 | 15 | 40 |
| `internal/approval` | 2 | 20 | 55 |
| `internal/browser_automation/platform` | 2 | 40 | 67 |
| `internal/capregistry` | 2 | 19 | 21 |
| `internal/channelbot/core` | 2 | 30 | 89 |
| `internal/cron` | 2 | 6 | 7 |
| `internal/domain/asset` | 2 | 5 | 5 |
| `internal/etl` | 2 | 16 | 29 |
| `internal/event` | 2 | 26 | 96 |
| `internal/migration` | 2 | 23 | 30 |
| `internal/monitor` | 2 | 21 | 50 |
| `internal/pkg/sla` | 2 | 13 | 16 |
| `internal/pkg/tenancyidx` | 2 | 57 | 111 |
| `internal/pkg/tracing` | 2 | 53 | 83 |
| `internal/pkg/utils/response` | 2 | 29 | 75 |
| `internal/pkg/utils/text` | 2 | 6 | 6 |
| `internal/secrets` | 2 | 15 | 55 |
| `internal/service/churn` | 2 | 8 | 24 |
| `scripts` | 2 | 2 | 45 |
| `cmd/bridge-mock` | 1 | 1 | 25 |
| `cmd/embedding-server` | 1 | 7 | 33 |
| `cmd/eval-trace` | 1 | 0 | 0 |
| `cmd/gen-module-bindings` | 1 | 3 | 36 |
| `cmd/gen-module-route-patterns` | 1 | 2 | 22 |
| `cmd/geo-run` | 1 | 1 | 1 |
| `cmd/importfaq` | 1 | 1 | 22 |
| `cmd/minttoken` | 1 | 0 | 0 |
| `cmd/nm-host` | 1 | 45 | 102 |
| `cmd/perf` | 1 | 1 | 13 |
| `cmd/pwtool` | 1 | 3 | 8 |
| `cmd/routeinspect` | 1 | 0 | 0 |
| `cmd/seedkw` | 1 | 0 | 0 |
| `cmd/syncscripts` | 1 | 1 | 10 |
| `internal/aiagent/embedding` | 1 | 12 | 32 |
| `internal/aiagent/knowledge/model` | 1 | 1 | 1 |
| `internal/aiagent/llm/selfconsistency` | 1 | 12 | 44 |
| `internal/aiagent/rag/core` | 1 | 25 | 29 |
| `internal/aiagent/rag/incremental` | 1 | 8 | 42 |
| `internal/aiagent/rag/service` | 1 | 15 | 16 |
| `internal/aiagent/vector` | 1 | 27 | 29 |
| `internal/browser_automation/platform/douyin` | 1 | 14 | 23 |
| `internal/browser_automation/platform/xianyu` | 1 | 15 | 25 |
| `internal/browser_automation/platform/xiaohongshu` | 1 | 12 | 20 |
| `internal/channelbot/qq` | 1 | 60 | 117 |
| `internal/channelbot/telegram` | 1 | 70 | 152 |
| `internal/channelbot/whatsapp` | 1 | 23 | 54 |
| `internal/content/dto` | 1 | 9 | 9 |
| `internal/contract` | 1 | 2 | 34 |
| `internal/domain/errors` | 1 | 20 | 43 |
| `internal/geo` | 1 | 1 | 37 |
| `internal/identity` | 1 | 8 | 27 |
| `internal/integration/templates` | 1 | 2 | 6 |
| `internal/pkg/apperr` | 1 | 16 | 19 |
| `internal/pkg/crypto` | 1 | 4 | 5 |
| `internal/pkg/dbencrypt` | 1 | 6 | 24 |
| `internal/pkg/dbstate` | 1 | 3 | 8 |
| `internal/pkg/errhttp` | 1 | 3 | 10 |
| `internal/pkg/featureflag` | 1 | 21 | 68 |
| `internal/pkg/goldentest` | 1 | 10 | 15 |
| `internal/pkg/httpclient` | 1 | 4 | 7 |
| `internal/pkg/kbrelease` | 1 | 21 | 113 |
| `internal/pkg/messageid` | 1 | 3 | 5 |
| `internal/pkg/security` | 1 | 6 | 37 |
| `internal/pkg/shared/service` | 1 | 5 | 6 |
| `internal/pkg/testutil` | 1 | 26 | 151 |
| `internal/pkg/testutil/testmigrate` | 1 | 2 | 12 |
| `internal/pkg/textutil` | 1 | 4 | 5 |
| `internal/pkg/tgbot` | 1 | 16 | 47 |
| `internal/pkg/timeutil` | 1 | 10 | 48 |
| `internal/pkg/trace` | 1 | 25 | 74 |
| `internal/pkg/traceparent` | 1 | 8 | 40 |
| `internal/pkg/utils/account` | 1 | 0 | 0 |
| `internal/pkg/utils/async` | 1 | 5 | 19 |
| `internal/pkg/utils/bcrypt` | 1 | 2 | 2 |
| `internal/pkg/utils/bm25` | 1 | 3 | 3 |
| `internal/pkg/utils/fingerprint` | 1 | 3 | 7 |
| `internal/pkg/utils/functions` | 1 | 0 | 0 |
| `internal/pkg/utils/httpget` | 1 | 1 | 1 |
| `internal/pkg/utils/logger` | 1 | 22 | 32 |
| `internal/pkg/utils/pagination` | 1 | 9 | 38 |
| `internal/pkg/utils/type` | 1 | 1 | 1 |
| `internal/pkg/utils/url` | 1 | 2 | 6 |
| `internal/pkg/utils/urlguard` | 1 | 5 | 27 |
| `internal/reach/card/template` | 1 | 16 | 23 |
| `internal/reporting` | 1 | 5 | 21 |
| `internal/repository/scope` | 1 | 8 | 42 |
| `internal/safeprompt` | 1 | 11 | 21 |
| `internal/security` | 1 | 3 | 3 |
| `internal/service/humanize/ai` | 1 | 8 | 39 |
| `internal/service/humanize/behavioral` | 1 | 6 | 30 |
| `internal/storage/storagetest` | 1 | 6 | 33 |
| `internal/system/install` | 1 | 22 | 69 |
| `scripts/demo_naked_goroutine` | 1 | 0 | 0 |

## 逐文件清单

状态列：`[ ]` 待处理 · `[x]` 已完成。处理时**逐个文件**读取全文、由 LLM 梳理注释、
只保留功能性注释，其余删除；**不得修改任何代码**。

| # | 状态 | 文件 | 注释块 | 注释行 | doc块 | 行内块 |
|---|---|---|---|---|---|---|
| 1 | [x] | `cmd/api/main.go` | 43 → 25 | 152 (160) → 56 | 43 → 25 | 0 |
| 2 | [x] | `cmd/api/serve_unix.go` | 4 → 4 | 14 → 9 | 4 → 4 | 0 |
| 3 | [x] | `cmd/api/serve_windows.go` | 2 → 2 | 4 → 4 | 2 → 2 | 0 |
| 4 | [x] | `cmd/bridge-mock/main.go` | 1 → 1 | 25 → 25 | 1 → 1 | 0 |
| 5 | [x] | `cmd/embedding-server/main.go` | 7 → 7 | 33 → 21 | 7 → 7 | 0 |
| 6 | [x] | `cmd/eval-trace/main.go` | 0 → 0 | 0 → 0 | 0 → 0 | 0 |
| 7 | [x] | `cmd/gen-module-bindings/main.go` | 3 → 3 | 36 → 11 | 3 → 3 | 0 |
| 8 | [x] | `cmd/gen-module-route-patterns/main.go` | 2 → 2 | 22 → 8 | 2 → 2 | 0 |
| 9 | [x] | `cmd/geo-run/main.go` | 1 → 0 | 1 → 0 | 1 → 0 | 0 |
| 10 | [x] | `cmd/importfaq/main.go` | 1 → 1 | 22 → 20 | 1 → 1 | 0 |
| 11 | [x] | `cmd/minttoken/main.go` | 0 → 0 | 0 → 0 | 0 → 0 | 0 |
| 12 | [x] | `cmd/nm-host/main.go` | 45 → 45 | 102 → 82 | 45 → 45 | 0 |
| 13 | [x] | `cmd/perf/main.go` | 1 → 1 | 13 → 13 | 1 → 1 | 0 |
| 14 | [x] | `cmd/pwtool/main.go` | 3 → 3 | 8 → 8 | 3 → 3 | 0 |
| 15 | [x] | `cmd/routeinspect/main.go` | 0 → 0 | 0 → 0 | 0 → 0 | 0 |
| 16 | [x] | `cmd/seed/main.go` | 6 → 6 | 47 → 43 | 6 → 6 | 0 |
| 17 | [x] | `cmd/seed/seed_ai_agents.go` | 3 → 3 | 23 → 23 | 3 → 3 | 0 |
| 18 | [x] | `cmd/seed/seed_assets.go` | 2 → 1 | 11 → 10 | 2 → 1 | 0 |
| 19 | [x] | `cmd/seed/seed_clues.go` | 2 → 1 | 7 → 6 | 2 → 1 | 0 |
| 20 | [x] | `cmd/seed/seed_customers.go` | 9 → 8 | 18 → 17 | 9 → 8 | 0 |
| 21 | [x] | `cmd/seed/seed_faq_sop.go` | 19 → 19 | 41 → 31 | 19 → 19 | 0 |
| 22 | [x] | `cmd/seed/seed_helpers.go` | 15 → 15 | 32 → 24 | 15 → 15 | 0 |
| 23 | [x] | `cmd/seed/seed_llm_logs.go` | 9 → 6 | 19 → 16 | 9 → 6 | 0 |
| 24 | [x] | `cmd/seed/seed_messages.go` | 2 → 1 | 8 → 7 | 2 → 1 | 0 |
| 25 | [x] | `cmd/seed/seed_rag_product.go` | 5 → 5 | 23 → 10 | 5 → 5 | 0 |
| 26 | [x] | `cmd/seed/seed_reach.go` | 1 → 1 | 15 → 15 | 1 → 1 | 0 |
| 27 | [x] | `cmd/seed/seed_sessions.go` | 6 → 6 | 6 → 6 | 6 → 6 | 0 |
| 28 | [x] | `cmd/seed/seed_stats.go` | 9 → 4 | 27 → 10 | 9 → 4 | 0 |
| 29 | [x] | `cmd/seed/seed_users.go` | 6 → 5 | 25 → 20 | 6 → 5 | 0 |
| 30 | [x] | `cmd/seedkw/main.go` | 0 → 0 | 0 → 0 | 0 → 0 | 0 |
| 31 | [x] | `cmd/syncscripts/main.go` | 1 → 1 | 10 → 5 | 1 → 1 | 0 |
| 32 | [x] | `internal/aiagent/agent/lifecycle/active.go` | 18 → 14 | 49 → 26 | 18 → 14 | 0 |
| 33 | [x] | `internal/aiagent/agent/lifecycle/lifecycle.go` | 10 → 10 | 38 → 16 | 10 → 10 | 0 |
| 34 | [x] | `internal/aiagent/agent/portcontract/aftersale.go` | 5 → 3 | 9 → 4 | 5 → 3 | 0 |
| 35 | [x] | `internal/aiagent/agent/portcontract/asset_bundle.go` | 9 → 9 | 36 → 15 | 9 → 9 | 0 |
| 36 | [x] | `internal/aiagent/agent/portcontract/customer.go` | 4 → 4 | 19 → 6 | 4 → 4 | 0 |
| 37 | [x] | `internal/aiagent/agent/portcontract/doc.go` | 1 → 1 | 27 → 8 | 1 → 1 | 0 |
| 38 | [x] | `internal/aiagent/agent/portcontract/followup.go` | 2 → 2 | 12 → 3 | 2 → 2 | 0 |
| 39 | [x] | `internal/aiagent/agent/portcontract/journey_reach.go` | 3 → 3 | 9 → 4 | 3 → 3 | 0 |
| 40 | [x] | `internal/aiagent/agent/portcontract/logistics.go` | 9 → 5 | 10 → 6 | 9 → 5 | 0 |
| 41 | [x] | `internal/aiagent/agent/portcontract/order.go` | 2 → 2 | 7 → 3 | 2 → 2 | 0 |
| 42 | [x] | `internal/aiagent/agent/portcontract/session.go` | 3 → 3 | 11 → 4 | 3 → 3 | 0 |
| 43 | [x] | `internal/aiagent/agent/portcontract/tool_result.go` | 4 → 3 | 6 → 4 | 4 → 3 | 0 |
| 44 | [x] | `internal/aiagent/agent/runtime/alignment_stage.go` | 7 → 2 | 9 → 2 | 7 → 2 | 0 |
| 45 | [x] | `internal/aiagent/agent/runtime/asset_bundle_loader.go` | 4 → 3 | 13 → 4 | 4 → 3 | 0 |
| 46 | [x] | `internal/aiagent/agent/runtime/chat_history_redis.go` | 4 → 4 | 26 → 6 | 4 → 4 | 0 |
| 47 | [x] | `internal/aiagent/agent/runtime/context.go` | 28 → 8 | 30 → 9 | 28 → 8 | 0 |
| 48 | [x] | `internal/aiagent/agent/runtime/dataflow_orchestrator.go` | 19 → 8 | 31 → 9 | 19 → 8 | 0 |
| 49 | [x] | `internal/aiagent/agent/runtime/event_subscriber.go` | 2 → 2 | 10 → 3 | 2 → 2 | 0 |
| 50 | [x] | `internal/aiagent/agent/runtime/gatekeeper_stage.go` | 11 → 5 | 29 → 9 | 11 → 5 | 0 |
| 51 | [x] | `internal/aiagent/agent/runtime/global.go` | 3 → 3 | 4 → 3 | 3 → 3 | 0 |
| 52 | [x] | `internal/aiagent/agent/runtime/idempotency.go` | 6 → 5 | 17 → 10 | 6 → 5 | 0 |
| 53 | [x] | `internal/aiagent/agent/runtime/inference_checkpoint.go` | 14 → 13 | 32 → 20 | 14 → 13 | 0 |
| 54 | [x] | `internal/aiagent/agent/runtime/inference_cycle.go` | 20 → 6 | 46 → 8 | 20 → 6 | 0 |
| 55 | [x] | `internal/aiagent/agent/runtime/inference_types.go` | 20 → 5 | 25 → 5 | 20 → 5 | 0 |
| 56 | [x] | `internal/aiagent/agent/runtime/intent_extractor.go` | 3 → 3 | 10 → 4 | 3 → 3 | 0 |
| 57 | [x] | `internal/aiagent/agent/runtime/local_llm_client.go` | 4 → 3 | 18 → 5 | 4 → 3 | 0 |
| 58 | [x] | `internal/aiagent/agent/runtime/perception_stage.go` | 12 → 3 | 20 → 4 | 12 → 3 | 0 |
| 59 | [x] | `internal/aiagent/agent/runtime/planner_stage.go` | 6 → 1 | 6 → 1 | 6 → 1 | 0 |
| 60 | [x] | `internal/aiagent/agent/runtime/publisher.go` | 4 → 3 | 19 → 4 | 4 → 3 | 0 |
| 61 | [ ] | `internal/aiagent/agent/runtime/publisher_knowledge.go` | 5 | 18 | 5 | 0 |
| 62 | [ ] | `internal/aiagent/agent/runtime/reviewer_stage.go` | 5 | 5 | 5 | 0 |
| 63 | [ ] | `internal/aiagent/agent/runtime/runtime.go` | 3 | 4 | 3 | 0 |
| 64 | [ ] | `internal/aiagent/agent/runtime/stages.go` | 13 | 24 | 13 | 0 |
| 65 | [ ] | `internal/aiagent/agent/runtime/stream.go` | 24 | 34 | 24 | 0 |
| 66 | [ ] | `internal/aiagent/agent/runtime/token_budget.go` | 0 | 0 | 0 | 0 |
| 67 | [ ] | `internal/aiagent/agent/runtime/types.go` | 13 | 55 | 13 | 0 |
| 68 | [ ] | `internal/aiagent/agent/tooluse/adapter_helpers.go` | 0 | 0 | 0 | 0 |
| 69 | [ ] | `internal/aiagent/agent/tooluse/auto_register.go` | 3 | 10 | 3 | 0 |
| 70 | [ ] | `internal/aiagent/agent/tooluse/browser_tools.go` | 26 | 30 | 26 | 0 |
| 71 | [ ] | `internal/aiagent/agent/tooluse/business_tools.go` | 26 | 40 | 26 | 0 |
| 72 | [ ] | `internal/aiagent/agent/tooluse/card_show_tool.go` | 6 | 8 | 6 | 0 |
| 73 | [ ] | `internal/aiagent/agent/tooluse/circuit_breaker.go` | 30 | 53 | 30 | 0 |
| 74 | [ ] | `internal/aiagent/agent/tooluse/customer_ports.go` | 0 | 0 | 0 | 0 |
| 75 | [ ] | `internal/aiagent/agent/tooluse/customer_tools.go` | 32 | 62 | 32 | 0 |
| 76 | [ ] | `internal/aiagent/agent/tooluse/db_audit_persister.go` | 39 | 101 | 39 | 0 |
| 77 | [ ] | `internal/aiagent/agent/tooluse/dead_letter.go` | 20 | 58 | 20 | 0 |
| 78 | [ ] | `internal/aiagent/agent/tooluse/decorator.go` | 18 | 24 | 18 | 0 |
| 79 | [ ] | `internal/aiagent/agent/tooluse/decorator_approval.go` | 10 | 43 | 10 | 0 |
| 80 | [ ] | `internal/aiagent/agent/tooluse/decorator_audit.go` | 7 | 14 | 7 | 0 |
| 81 | [ ] | `internal/aiagent/agent/tooluse/decorator_dnc.go` | 9 | 22 | 9 | 0 |
| 82 | [ ] | `internal/aiagent/agent/tooluse/decorator_permission.go` | 0 | 0 | 0 | 0 |
| 83 | [ ] | `internal/aiagent/agent/tooluse/decorator_ratelimit.go` | 0 | 0 | 0 | 0 |
| 84 | [ ] | `internal/aiagent/agent/tooluse/decorator_retry.go` | 0 | 0 | 0 | 0 |
| 85 | [ ] | `internal/aiagent/agent/tooluse/decorator_timeout.go` | 0 | 0 | 0 | 0 |
| 86 | [ ] | `internal/aiagent/agent/tooluse/discovery.go` | 35 | 35 | 35 | 0 |
| 87 | [ ] | `internal/aiagent/agent/tooluse/double_intercept.go` | 12 | 21 | 12 | 0 |
| 88 | [ ] | `internal/aiagent/agent/tooluse/executor.go` | 44 | 84 | 44 | 0 |
| 89 | [ ] | `internal/aiagent/agent/tooluse/feedback_decorator.go` | 19 | 69 | 19 | 0 |
| 90 | [ ] | `internal/aiagent/agent/tooluse/fingerprint.go` | 0 | 0 | 0 | 0 |
| 91 | [ ] | `internal/aiagent/agent/tooluse/knowledge_tools.go` | 18 | 20 | 18 | 0 |
| 92 | [ ] | `internal/aiagent/agent/tooluse/loop_guard.go` | 18 | 44 | 18 | 0 |
| 93 | [ ] | `internal/aiagent/agent/tooluse/param_validator.go` | 2 | 13 | 2 | 0 |
| 94 | [ ] | `internal/aiagent/agent/tooluse/permission_highrisk.go` | 17 | 48 | 17 | 0 |
| 95 | [ ] | `internal/aiagent/agent/tooluse/permission_whitelist.go` | 14 | 39 | 14 | 0 |
| 96 | [ ] | `internal/aiagent/agent/tooluse/ports.go` | 11 | 11 | 11 | 0 |
| 97 | [ ] | `internal/aiagent/agent/tooluse/private_message_tools.go` | 3 | 13 | 3 | 0 |
| 98 | [ ] | `internal/aiagent/agent/tooluse/provider.go` | 17 | 47 | 17 | 0 |
| 99 | [ ] | `internal/aiagent/agent/tooluse/reach_ports.go` | 9 | 15 | 9 | 0 |
| 100 | [ ] | `internal/aiagent/agent/tooluse/reach_service_registry.go` | 5 | 10 | 5 | 0 |
| 101 | [ ] | `internal/aiagent/agent/tooluse/reach_tools.go` | 10 | 12 | 10 | 0 |
| 102 | [ ] | `internal/aiagent/agent/tooluse/reach_tools_email.go` | 0 | 0 | 0 | 0 |
| 103 | [ ] | `internal/aiagent/agent/tooluse/reach_tools_feishu.go` | 1 | 1 | 1 | 0 |
| 104 | [ ] | `internal/aiagent/agent/tooluse/reach_tools_noop.go` | 0 | 0 | 0 | 0 |
| 105 | [ ] | `internal/aiagent/agent/tooluse/reach_tools_sms.go` | 0 | 0 | 0 | 0 |
| 106 | [ ] | `internal/aiagent/agent/tooluse/reach_tools_social.go` | 0 | 0 | 0 | 0 |
| 107 | [ ] | `internal/aiagent/agent/tooluse/reach_tools_telegram.go` | 1 | 1 | 1 | 0 |
| 108 | [ ] | `internal/aiagent/agent/tooluse/reach_tools_util.go` | 0 | 0 | 0 | 0 |
| 109 | [ ] | `internal/aiagent/agent/tooluse/reach_tools_web.go` | 1 | 1 | 1 | 0 |
| 110 | [ ] | `internal/aiagent/agent/tooluse/reach_tools_wecom.go` | 0 | 0 | 0 | 0 |
| 111 | [ ] | `internal/aiagent/agent/tooluse/reach_tools_weixin.go` | 0 | 0 | 0 | 0 |
| 112 | [ ] | `internal/aiagent/agent/tooluse/reach_tools_whatsapp.go` | 1 | 1 | 1 | 0 |
| 113 | [ ] | `internal/aiagent/agent/tooluse/registry.go` | 23 | 32 | 23 | 0 |
| 114 | [ ] | `internal/aiagent/agent/tooluse/result_cache.go` | 13 | 30 | 13 | 0 |
| 115 | [ ] | `internal/aiagent/agent/tooluse/risk_gate.go` | 28 | 98 | 28 | 0 |
| 116 | [ ] | `internal/aiagent/agent/tooluse/stream_state_machine.go` | 20 | 50 | 20 | 0 |
| 117 | [ ] | `internal/aiagent/agent/tooluse/tool.go` | 19 | 28 | 19 | 0 |
| 118 | [ ] | `internal/aiagent/agent/tooluse/tool_risk.go` | 7 | 35 | 7 | 0 |
| 119 | [ ] | `internal/aiagent/agent/tooluse/tool_router.go` | 17 | 27 | 17 | 0 |
| 120 | [ ] | `internal/aiagent/embedding/embedding.go` | 12 | 32 | 12 | 0 |
| 121 | [ ] | `internal/aiagent/eval/chrf_eval.go` | 5 | 17 | 5 | 0 |
| 122 | [ ] | `internal/aiagent/eval/dialogue_gold.go` | 13 | 31 | 13 | 0 |
| 123 | [ ] | `internal/aiagent/eval/evaluator.go` | 8 | 17 | 8 | 0 |
| 124 | [ ] | `internal/aiagent/eval/judge_calibration.go` | 5 | 13 | 5 | 0 |
| 125 | [ ] | `internal/aiagent/eval/llm_judge.go` | 9 | 25 | 9 | 0 |
| 126 | [ ] | `internal/aiagent/eval/ragas.go` | 8 | 12 | 8 | 0 |
| 127 | [ ] | `internal/aiagent/knowledge/controller/error_helper.go` | 0 | 0 | 0 | 0 |
| 128 | [ ] | `internal/aiagent/knowledge/controller/knowledge_base.go` | 4 | 4 | 4 | 0 |
| 129 | [ ] | `internal/aiagent/knowledge/controller/knowledge_merchant.go` | 6 | 20 | 6 | 0 |
| 130 | [ ] | `internal/aiagent/knowledge/controller/knowledge_workspace.go` | 37 | 46 | 37 | 0 |
| 131 | [ ] | `internal/aiagent/knowledge/controller/ports.go` | 0 | 0 | 0 | 0 |
| 132 | [ ] | `internal/aiagent/knowledge/controller/rag_config.go` | 2 | 2 | 2 | 0 |
| 133 | [ ] | `internal/aiagent/knowledge/model/aliases.go` | 1 | 1 | 1 | 0 |
| 134 | [ ] | `internal/aiagent/knowledge/repository/kb_document.go` | 7 | 7 | 7 | 0 |
| 135 | [ ] | `internal/aiagent/knowledge/repository/knowledge_chunk.go` | 21 | 62 | 21 | 0 |
| 136 | [ ] | `internal/aiagent/knowledge/repository/knowledge_document.go` | 22 | 53 | 22 | 0 |
| 137 | [ ] | `internal/aiagent/knowledge/repository/knowledge_import_log.go` | 8 | 10 | 8 | 0 |
| 138 | [ ] | `internal/aiagent/knowledge/repository/knowledge_merchant.go` | 18 | 18 | 18 | 0 |
| 139 | [ ] | `internal/aiagent/knowledge/repository/knowledge_openapi.go` | 10 | 10 | 10 | 0 |
| 140 | [ ] | `internal/aiagent/knowledge/repository/knowledge_search_log.go` | 13 | 13 | 13 | 0 |
| 141 | [ ] | `internal/aiagent/knowledge/repository/rag_config.go` | 7 | 11 | 7 | 0 |
| 142 | [ ] | `internal/aiagent/knowledge/service/constants.go` | 19 | 37 | 19 | 0 |
| 143 | [ ] | `internal/aiagent/knowledge/service/embedding_dim_guard.go` | 0 | 0 | 0 | 0 |
| 144 | [ ] | `internal/aiagent/knowledge/service/knowledge.go` | 16 | 29 | 16 | 0 |
| 145 | [ ] | `internal/aiagent/knowledge/service/knowledge_base.go` | 6 | 9 | 6 | 0 |
| 146 | [ ] | `internal/aiagent/knowledge/service/knowledge_base_helpers.go` | 1 | 3 | 1 | 0 |
| 147 | [ ] | `internal/aiagent/knowledge/service/knowledge_base_import.go` | 9 | 16 | 9 | 0 |
| 148 | [ ] | `internal/aiagent/knowledge/service/knowledge_helpers.go` | 1 | 6 | 1 | 0 |
| 149 | [ ] | `internal/aiagent/knowledge/service/knowledge_merchant.go` | 4 | 13 | 4 | 0 |
| 150 | [ ] | `internal/aiagent/knowledge/service/knowledge_merchant_batch.go` | 11 | 17 | 11 | 0 |
| 151 | [ ] | `internal/aiagent/knowledge/service/knowledge_merchant_chunk.go` | 6 | 12 | 6 | 0 |
| 152 | [ ] | `internal/aiagent/knowledge/service/knowledge_merchant_external.go` | 11 | 14 | 11 | 0 |
| 153 | [ ] | `internal/aiagent/knowledge/service/knowledge_merchant_feedback.go` | 4 | 4 | 4 | 0 |
| 154 | [ ] | `internal/aiagent/knowledge/service/knowledge_merchant_playground.go` | 4 | 4 | 4 | 0 |
| 155 | [ ] | `internal/aiagent/knowledge/service/knowledge_merchant_token.go` | 10 | 11 | 10 | 0 |
| 156 | [ ] | `internal/aiagent/knowledge/service/knowledge_security.go` | 1 | 6 | 1 | 0 |
| 157 | [ ] | `internal/aiagent/knowledge/service/knowledge_service_contextual.go` | 0 | 0 | 0 | 0 |
| 158 | [ ] | `internal/aiagent/knowledge/service/knowledge_service_import.go` | 1 | 4 | 1 | 0 |
| 159 | [ ] | `internal/aiagent/knowledge/service/knowledge_service_log.go` | 1 | 4 | 1 | 0 |
| 160 | [ ] | `internal/aiagent/knowledge/service/knowledge_service_pipeline.go` | 1 | 5 | 1 | 0 |
| 161 | [ ] | `internal/aiagent/knowledge/service/knowledge_service_reindex.go` | 3 | 3 | 3 | 0 |
| 162 | [ ] | `internal/aiagent/knowledge/service/knowledge_statistics.go` | 18 | 21 | 18 | 0 |
| 163 | [ ] | `internal/aiagent/knowledge/service/rag_config.go` | 7 | 12 | 7 | 0 |
| 164 | [ ] | `internal/aiagent/knowledge/service/rag_config_account.go` | 3 | 3 | 3 | 0 |
| 165 | [ ] | `internal/aiagent/knowledge/service/rag_config_rag.go` | 3 | 7 | 3 | 0 |
| 166 | [ ] | `internal/aiagent/knowledge/service/rag_searcher.go` | 9 | 42 | 9 | 0 |
| 167 | [ ] | `internal/aiagent/knowledge/service/rag_searcher_bm25.go` | 1 | 3 | 1 | 0 |
| 168 | [ ] | `internal/aiagent/knowledge/service/rag_searcher_products.go` | 1 | 5 | 1 | 0 |
| 169 | [ ] | `internal/aiagent/knowledge/service/rag_searcher_types.go` | 2 | 2 | 2 | 0 |
| 170 | [ ] | `internal/aiagent/knowledge/service/rag_searcher_vector.go` | 0 | 0 | 0 | 0 |
| 171 | [ ] | `internal/aiagent/llm/dispatcher.go` | 30 | 57 | 30 | 0 |
| 172 | [ ] | `internal/aiagent/llm/dispatcher_alert.go` | 12 | 27 | 12 | 0 |
| 173 | [ ] | `internal/aiagent/llm/dispatcher_cache.go` | 4 | 12 | 4 | 0 |
| 174 | [ ] | `internal/aiagent/llm/dispatcher_dispatch.go` | 21 | 69 | 21 | 0 |
| 175 | [ ] | `internal/aiagent/llm/dispatcher_observability.go` | 17 | 86 | 17 | 0 |
| 176 | [ ] | `internal/aiagent/llm/dispatcher_persistence.go` | 10 | 27 | 10 | 0 |
| 177 | [ ] | `internal/aiagent/llm/dispatcher_register.go` | 0 | 0 | 0 | 0 |
| 178 | [ ] | `internal/aiagent/llm/dispatcher_registry.go` | 8 | 13 | 8 | 0 |
| 179 | [ ] | `internal/aiagent/llm/embed_dim.go` | 2 | 2 | 2 | 0 |
| 180 | [ ] | `internal/aiagent/llm/embedding.go` | 18 | 55 | 18 | 0 |
| 181 | [ ] | `internal/aiagent/llm/embedding_global_config.go` | 7 | 47 | 7 | 0 |
| 182 | [ ] | `internal/aiagent/llm/embedding_hash.go` | 5 | 9 | 5 | 0 |
| 183 | [ ] | `internal/aiagent/llm/embedding_registry.go` | 3 | 4 | 3 | 0 |
| 184 | [ ] | `internal/aiagent/llm/fallback_tree.go` | 12 | 38 | 12 | 0 |
| 185 | [ ] | `internal/aiagent/llm/llm.go` | 23 | 50 | 23 | 0 |
| 186 | [ ] | `internal/aiagent/llm/llm_platform_proxy.go` | 21 | 57 | 21 | 0 |
| 187 | [ ] | `internal/aiagent/llm/provider_crypto.go` | 0 | 0 | 0 | 0 |
| 188 | [ ] | `internal/aiagent/llm/provider_failover.go` | 39 | 66 | 39 | 0 |
| 189 | [ ] | `internal/aiagent/llm/rate_limit_error.go` | 1 | 8 | 1 | 0 |
| 190 | [ ] | `internal/aiagent/llm/react_adapter.go` | 9 | 54 | 9 | 0 |
| 191 | [ ] | `internal/aiagent/llm/selfconsistency/selfconsistency.go` | 12 | 44 | 12 | 0 |
| 192 | [ ] | `internal/aiagent/llm/trace_context.go` | 33 | 48 | 33 | 0 |
| 193 | [ ] | `internal/aiagent/llm/trace_query.go` | 7 | 14 | 7 | 0 |
| 194 | [ ] | `internal/aiagent/llm/trace_sink.go` | 12 | 28 | 12 | 0 |
| 195 | [ ] | `internal/aiagent/mcp/http.go` | 7 | 11 | 7 | 0 |
| 196 | [ ] | `internal/aiagent/mcp/server.go` | 30 | 40 | 30 | 0 |
| 197 | [ ] | `internal/aiagent/rag/cache/answer_cache_model.go` | 10 | 11 | 10 | 0 |
| 198 | [ ] | `internal/aiagent/rag/cache/metrics.go` | 0 | 0 | 0 | 0 |
| 199 | [ ] | `internal/aiagent/rag/cache/service.go` | 10 | 20 | 10 | 0 |
| 200 | [ ] | `internal/aiagent/rag/cache/store_pg.go` | 14 | 31 | 14 | 0 |
| 201 | [ ] | `internal/aiagent/rag/cache/vector.go` | 1 | 2 | 1 | 0 |
| 202 | [ ] | `internal/aiagent/rag/core/rag_engine.go` | 25 | 29 | 25 | 0 |
| 203 | [ ] | `internal/aiagent/rag/customer_service/dialog_manager.go` | 24 | 28 | 24 | 0 |
| 204 | [ ] | `internal/aiagent/rag/customer_service/interfaces.go` | 22 | 22 | 22 | 0 |
| 205 | [ ] | `internal/aiagent/rag/customer_service/llm_adapter.go` | 6 | 7 | 6 | 0 |
| 206 | [ ] | `internal/aiagent/rag/customer_service/pg_dialog_manager.go` | 1 | 2 | 1 | 0 |
| 207 | [ ] | `internal/aiagent/rag/customer_service/prompt_templates.go` | 1 | 10 | 1 | 0 |
| 208 | [ ] | `internal/aiagent/rag/customer_service/quality_assessor.go` | 7 | 7 | 7 | 0 |
| 209 | [ ] | `internal/aiagent/rag/customer_service/rag_customer.go` | 21 | 21 | 21 | 0 |
| 210 | [ ] | `internal/aiagent/rag/customer_service/response_generator.go` | 25 | 83 | 25 | 0 |
| 211 | [ ] | `internal/aiagent/rag/eval/lightweight.go` | 9 | 24 | 9 | 0 |
| 212 | [ ] | `internal/aiagent/rag/eval/rag_eval.go` | 6 | 16 | 6 | 0 |
| 213 | [ ] | `internal/aiagent/rag/incremental/incremental_indexer.go` | 8 | 42 | 8 | 0 |
| 214 | [ ] | `internal/aiagent/rag/retrieval/bge_m3_vectorizer.go` | 12 | 27 | 12 | 0 |
| 215 | [ ] | `internal/aiagent/rag/retrieval/cached_embedding_client.go` | 10 | 16 | 10 | 0 |
| 216 | [ ] | `internal/aiagent/rag/retrieval/contextual_retrieval.go` | 5 | 22 | 5 | 0 |
| 217 | [ ] | `internal/aiagent/rag/retrieval/default_implementations.go` | 18 | 21 | 18 | 0 |
| 218 | [ ] | `internal/aiagent/rag/retrieval/hybrid_searcher.go` | 10 | 16 | 10 | 0 |
| 219 | [ ] | `internal/aiagent/rag/retrieval/hyde_generator.go` | 6 | 18 | 6 | 0 |
| 220 | [ ] | `internal/aiagent/rag/retrieval/index_manager.go` | 18 | 21 | 18 | 0 |
| 221 | [ ] | `internal/aiagent/rag/retrieval/index_manager_factory.go` | 3 | 4 | 3 | 0 |
| 222 | [ ] | `internal/aiagent/rag/retrieval/interfaces.go` | 9 | 9 | 9 | 0 |
| 223 | [ ] | `internal/aiagent/rag/retrieval/lexical_retriever.go` | 2 | 2 | 2 | 0 |
| 224 | [ ] | `internal/aiagent/rag/retrieval/llm_chat.go` | 8 | 20 | 8 | 0 |
| 225 | [ ] | `internal/aiagent/rag/retrieval/llm_chat_intent.go` | 8 | 21 | 8 | 0 |
| 226 | [ ] | `internal/aiagent/rag/retrieval/lru_cache.go` | 9 | 10 | 9 | 0 |
| 227 | [ ] | `internal/aiagent/rag/retrieval/markdown_chunk_strategy.go` | 2 | 2 | 2 | 0 |
| 228 | [ ] | `internal/aiagent/rag/retrieval/multi_query_generator.go` | 6 | 15 | 6 | 0 |
| 229 | [ ] | `internal/aiagent/rag/retrieval/pgvector_index_ops.go` | 6 | 18 | 6 | 0 |
| 230 | [ ] | `internal/aiagent/rag/retrieval/pgvector_session.go` | 3 | 8 | 3 | 0 |
| 231 | [ ] | `internal/aiagent/rag/retrieval/query_rewriter.go` | 8 | 25 | 8 | 0 |
| 232 | [ ] | `internal/aiagent/rag/retrieval/query_weight.go` | 3 | 8 | 3 | 0 |
| 233 | [ ] | `internal/aiagent/rag/retrieval/rag_retrieval.go` | 18 | 22 | 18 | 0 |
| 234 | [ ] | `internal/aiagent/rag/retrieval/redis_cache.go` | 2 | 6 | 2 | 0 |
| 235 | [ ] | `internal/aiagent/rag/retrieval/redis_client.go` | 6 | 10 | 6 | 0 |
| 236 | [ ] | `internal/aiagent/rag/retrieval/rerank.go` | 49 | 158 | 49 | 0 |
| 237 | [ ] | `internal/aiagent/rag/retrieval/rerank_advanced.go` | 31 | 72 | 31 | 0 |
| 238 | [ ] | `internal/aiagent/rag/retrieval/reranker_adapter.go` | 1 | 4 | 1 | 0 |
| 239 | [ ] | `internal/aiagent/rag/retrieval/rrf_fusion.go` | 5 | 16 | 5 | 0 |
| 240 | [ ] | `internal/aiagent/rag/retrieval/three_tier.go` | 14 | 17 | 14 | 0 |
| 241 | [ ] | `internal/aiagent/rag/retrieval/three_tier_adapter.go` | 4 | 5 | 4 | 0 |
| 242 | [ ] | `internal/aiagent/rag/retrieval/translation_cache.go` | 12 | 38 | 12 | 0 |
| 243 | [ ] | `internal/aiagent/rag/retrieval/vec_pg.go` | 0 | 0 | 0 | 0 |
| 244 | [ ] | `internal/aiagent/rag/retrieval/vector_retriever.go` | 5 | 19 | 5 | 0 |
| 245 | [ ] | `internal/aiagent/rag/retrieval/vectorizer.go` | 11 | 22 | 11 | 0 |
| 246 | [ ] | `internal/aiagent/rag/service/rag.go` | 15 | 16 | 15 | 0 |
| 247 | [ ] | `internal/aiagent/vector/vector_model.go` | 27 | 29 | 27 | 0 |
| 248 | [ ] | `internal/app/agent_checkpoint_wiring.go` | 5 | 13 | 5 | 0 |
| 249 | [ ] | `internal/app/agent_lifecycle_wiring.go` | 15 | 55 | 15 | 0 |
| 250 | [ ] | `internal/app/agent_llm_params_wiring.go` | 2 | 18 | 2 | 0 |
| 251 | [ ] | `internal/app/agent_tool_params_wiring.go` | 1 | 18 | 1 | 0 |
| 252 | [ ] | `internal/app/approval_runtime_wiring.go` | 15 | 78 | 15 | 0 |
| 253 | [ ] | `internal/app/approval_wiring.go` | 18 | 132 | 18 | 0 |
| 254 | [ ] | `internal/app/bad_case_wiring.go` | 4 | 19 | 4 | 0 |
| 255 | [ ] | `internal/app/bill_wiring.go` | 5 | 32 | 5 | 0 |
| 256 | [ ] | `internal/app/business_notifier_wiring.go` | 2 | 15 | 2 | 0 |
| 257 | [ ] | `internal/app/cache_params_wiring.go` | 1 | 13 | 1 | 0 |
| 258 | [ ] | `internal/app/channelbot_params_wiring.go` | 1 | 9 | 1 | 0 |
| 259 | [ ] | `internal/app/collection_wiring.go` | 14 | 73 | 14 | 0 |
| 260 | [ ] | `internal/app/confidence_params_wiring.go` | 1 | 12 | 1 | 0 |
| 261 | [ ] | `internal/app/customer_datastore_adapter.go` | 1 | 1 | 1 | 0 |
| 262 | [ ] | `internal/app/email_runtime_wiring.go` | 6 | 28 | 6 | 0 |
| 263 | [ ] | `internal/app/embedding_params_wiring.go` | 1 | 14 | 1 | 0 |
| 264 | [ ] | `internal/app/event_bus.go` | 2 | 11 | 2 | 0 |
| 265 | [ ] | `internal/app/faq_cache_wiring.go` | 5 | 27 | 5 | 0 |
| 266 | [ ] | `internal/app/feedback_sink_adapter.go` | 5 | 17 | 5 | 0 |
| 267 | [ ] | `internal/app/frontend_ui_params_wiring.go` | 1 | 14 | 1 | 0 |
| 268 | [ ] | `internal/app/globals.go` | 5 | 5 | 5 | 0 |
| 269 | [ ] | `internal/app/human_task_sla_wiring.go` | 7 | 28 | 7 | 0 |
| 270 | [ ] | `internal/app/human_task_wiring.go` | 5 | 31 | 5 | 0 |
| 271 | [ ] | `internal/app/inbox_sales_session_pagination_wechat_params_wiring.go` | 3 | 30 | 3 | 0 |
| 272 | [ ] | `internal/app/inference_orchestrator_wiring.go` | 6 | 38 | 6 | 0 |
| 273 | [ ] | `internal/app/integration_reach_adapter.go` | 54 | 165 | 54 | 0 |
| 274 | [ ] | `internal/app/kb_release_wiring.go` | 5 | 35 | 5 | 0 |
| 275 | [ ] | `internal/app/lock_idempotency_params_wiring.go` | 2 | 9 | 2 | 0 |
| 276 | [ ] | `internal/app/misc_params_wiring.go` | 1 | 26 | 1 | 0 |
| 277 | [ ] | `internal/app/operation_log_subscriber.go` | 3 | 12 | 3 | 0 |
| 278 | [ ] | `internal/app/opportunity_wiring.go` | 7 | 30 | 7 | 0 |
| 279 | [ ] | `internal/app/order_draft_wiring.go` | 35 | 150 | 35 | 0 |
| 280 | [ ] | `internal/app/p1_params_wiring.go` | 1 | 13 | 1 | 0 |
| 281 | [ ] | `internal/app/payment_wiring.go` | 7 | 38 | 7 | 0 |
| 282 | [ ] | `internal/app/permission_wiring.go` | 11 | 61 | 11 | 0 |
| 283 | [ ] | `internal/app/quote_wiring.go` | 10 | 58 | 10 | 0 |
| 284 | [ ] | `internal/app/reach_gate_wiring.go` | 27 | 152 | 27 | 0 |
| 285 | [ ] | `internal/app/reach_sender_wiring.go` | 3 | 16 | 3 | 0 |
| 286 | [ ] | `internal/app/reach_tool_wiring.go` | 1 | 7 | 1 | 0 |
| 287 | [ ] | `internal/app/recovery_worker_wiring.go` | 2 | 25 | 2 | 0 |
| 288 | [ ] | `internal/app/reporting_params_wiring.go` | 1 | 10 | 1 | 0 |
| 289 | [ ] | `internal/app/sales_engine_factory.go` | 7 | 36 | 7 | 0 |
| 290 | [ ] | `internal/app/sales_middleware_knowledge_wecom_channelgw_params_wiring.go` | 4 | 19 | 4 | 0 |
| 291 | [ ] | `internal/app/sales_workbench_wiring.go` | 3 | 23 | 3 | 0 |
| 292 | [ ] | `internal/app/slo_wiring.go` | 2 | 4 | 2 | 0 |
| 293 | [ ] | `internal/app/telemetry_workflow_params_wiring.go` | 1 | 18 | 1 | 0 |
| 294 | [ ] | `internal/app/tool_audit_wiring.go` | 14 | 65 | 14 | 0 |
| 295 | [ ] | `internal/app/tool_circuit_breaker_wiring.go` | 15 | 88 | 15 | 0 |
| 296 | [ ] | `internal/app/tool_executor_adapter.go` | 4 | 6 | 4 | 0 |
| 297 | [ ] | `internal/app/tool_executor_wiring.go` | 8 | 67 | 8 | 0 |
| 298 | [ ] | `internal/app/tool_provider_wiring.go` | 9 | 10 | 9 | 0 |
| 299 | [ ] | `internal/app/upload_body_limit_params_wiring.go` | 2 | 20 | 2 | 0 |
| 300 | [ ] | `internal/app/url_params_wiring.go` | 1 | 14 | 1 | 0 |
| 301 | [ ] | `internal/approval/decision_counter.go` | 9 | 23 | 9 | 0 |
| 302 | [ ] | `internal/approval/whitelist.go` | 11 | 32 | 11 | 0 |
| 303 | [ ] | `internal/bridge/account.go` | 5 | 6 | 5 | 0 |
| 304 | [ ] | `internal/bridge/account_repo.go` | 9 | 34 | 9 | 0 |
| 305 | [ ] | `internal/bridge/bridge_helpers.go` | 3 | 9 | 3 | 0 |
| 306 | [ ] | `internal/bridge/channel.go` | 0 | 0 | 0 | 0 |
| 307 | [ ] | `internal/bridge/frames.go` | 6 | 11 | 6 | 0 |
| 308 | [ ] | `internal/bridge/handler_http.go` | 30 | 98 | 30 | 0 |
| 309 | [ ] | `internal/bridge/reach_adapter.go` | 3 | 8 | 3 | 0 |
| 310 | [ ] | `internal/bridge/sse.go` | 40 | 169 | 40 | 0 |
| 311 | [ ] | `internal/bridge/tenant_binding.go` | 14 | 53 | 14 | 0 |
| 312 | [ ] | `internal/browser_automation/controller/bind_err.go` | 12 | 32 | 12 | 0 |
| 313 | [ ] | `internal/browser_automation/controller/cron.go` | 8 | 8 | 8 | 0 |
| 314 | [ ] | `internal/browser_automation/controller/host.go` | 12 | 28 | 12 | 0 |
| 315 | [ ] | `internal/browser_automation/controller/platform.go` | 4 | 4 | 4 | 0 |
| 316 | [ ] | `internal/browser_automation/controller/session.go` | 18 | 37 | 18 | 0 |
| 317 | [ ] | `internal/browser_automation/controller/task.go` | 25 | 42 | 25 | 0 |
| 318 | [ ] | `internal/browser_automation/dto/cron.go` | 1 | 1 | 1 | 0 |
| 319 | [ ] | `internal/browser_automation/dto/session.go` | 1 | 3 | 1 | 0 |
| 320 | [ ] | `internal/browser_automation/dto/step_action.go` | 4 | 16 | 4 | 0 |
| 321 | [ ] | `internal/browser_automation/dto/task.go` | 29 | 46 | 29 | 0 |
| 322 | [ ] | `internal/browser_automation/model/audit_digest.go` | 11 | 61 | 11 | 0 |
| 323 | [ ] | `internal/browser_automation/model/command_log.go` | 5 | 14 | 5 | 0 |
| 324 | [ ] | `internal/browser_automation/model/cron.go` | 3 | 6 | 3 | 0 |
| 325 | [ ] | `internal/browser_automation/model/llm_plan.go` | 3 | 6 | 3 | 0 |
| 326 | [ ] | `internal/browser_automation/model/outreach_dedupe.go` | 2 | 3 | 2 | 0 |
| 327 | [ ] | `internal/browser_automation/model/outreach_receipt.go` | 4 | 16 | 4 | 0 |
| 328 | [ ] | `internal/browser_automation/model/profile_health.go` | 2 | 5 | 2 | 0 |
| 329 | [ ] | `internal/browser_automation/model/session.go` | 6 | 11 | 6 | 0 |
| 330 | [ ] | `internal/browser_automation/model/step.go` | 17 | 26 | 17 | 0 |
| 331 | [ ] | `internal/browser_automation/model/task.go` | 19 | 32 | 19 | 0 |
| 332 | [ ] | `internal/browser_automation/model/write_claim.go` | 1 | 16 | 1 | 0 |
| 333 | [ ] | `internal/browser_automation/platform/blockdetect.go` | 5 | 19 | 5 | 0 |
| 334 | [ ] | `internal/browser_automation/platform/douyin/douyin.go` | 14 | 23 | 14 | 0 |
| 335 | [ ] | `internal/browser_automation/platform/platform.go` | 35 | 48 | 35 | 0 |
| 336 | [ ] | `internal/browser_automation/platform/xianyu/xianyu.go` | 15 | 25 | 15 | 0 |
| 337 | [ ] | `internal/browser_automation/platform/xiaohongshu/xiaohongshu.go` | 12 | 20 | 12 | 0 |
| 338 | [ ] | `internal/browser_automation/repository/audit_digest.go` | 2 | 9 | 2 | 0 |
| 339 | [ ] | `internal/browser_automation/repository/command_log.go` | 12 | 56 | 12 | 0 |
| 340 | [ ] | `internal/browser_automation/repository/cron.go` | 5 | 7 | 5 | 0 |
| 341 | [ ] | `internal/browser_automation/repository/llm_plan.go` | 6 | 10 | 6 | 0 |
| 342 | [ ] | `internal/browser_automation/repository/outreach_dedupe.go` | 7 | 7 | 7 | 0 |
| 343 | [ ] | `internal/browser_automation/repository/outreach_receipt.go` | 7 | 13 | 7 | 0 |
| 344 | [ ] | `internal/browser_automation/repository/profile_health.go` | 5 | 5 | 5 | 0 |
| 345 | [ ] | `internal/browser_automation/repository/session.go` | 15 | 27 | 15 | 0 |
| 346 | [ ] | `internal/browser_automation/repository/step.go` | 9 | 22 | 9 | 0 |
| 347 | [ ] | `internal/browser_automation/repository/task.go` | 12 | 50 | 12 | 0 |
| 348 | [ ] | `internal/browser_automation/repository/write_claim.go` | 7 | 19 | 7 | 0 |
| 349 | [ ] | `internal/browser_automation/service/audit_timeline.go` | 5 | 27 | 5 | 0 |
| 350 | [ ] | `internal/browser_automation/service/brain.go` | 36 | 55 | 36 | 0 |
| 351 | [ ] | `internal/browser_automation/service/brain_budget.go` | 10 | 15 | 10 | 0 |
| 352 | [ ] | `internal/browser_automation/service/brain_constants.go` | 3 | 5 | 3 | 0 |
| 353 | [ ] | `internal/browser_automation/service/brain_prompts.go` | 12 | 23 | 12 | 0 |
| 354 | [ ] | `internal/browser_automation/service/brain_reliability.go` | 6 | 16 | 6 | 0 |
| 355 | [ ] | `internal/browser_automation/service/campaign_budget.go` | 5 | 35 | 5 | 0 |
| 356 | [ ] | `internal/browser_automation/service/cron.go` | 18 | 46 | 18 | 0 |
| 357 | [ ] | `internal/browser_automation/service/executor.go` | 202 | 490 | 202 | 0 |
| 358 | [ ] | `internal/browser_automation/service/executor_selfheal.go` | 7 | 22 | 7 | 0 |
| 359 | [ ] | `internal/browser_automation/service/feedback.go` | 22 | 51 | 22 | 0 |
| 360 | [ ] | `internal/browser_automation/service/hand.go` | 23 | 56 | 23 | 0 |
| 361 | [ ] | `internal/browser_automation/service/host_registry.go` | 49 | 101 | 49 | 0 |
| 362 | [ ] | `internal/browser_automation/service/host_token.go` | 7 | 16 | 7 | 0 |
| 363 | [ ] | `internal/browser_automation/service/jev.go` | 47 | 96 | 47 | 0 |
| 364 | [ ] | `internal/browser_automation/service/outreach_dedupe.go` | 9 | 21 | 9 | 0 |
| 365 | [ ] | `internal/browser_automation/service/outreach_receipt.go` | 12 | 54 | 12 | 0 |
| 366 | [ ] | `internal/browser_automation/service/profile_fuse.go` | 2 | 8 | 2 | 0 |
| 367 | [ ] | `internal/browser_automation/service/retention.go` | 8 | 25 | 8 | 0 |
| 368 | [ ] | `internal/browser_automation/service/session.go` | 21 | 41 | 21 | 0 |
| 369 | [ ] | `internal/browser_automation/service/stale_reconcile.go` | 12 | 37 | 12 | 0 |
| 370 | [ ] | `internal/browser_automation/service/storage_util.go` | 3 | 3 | 3 | 0 |
| 371 | [ ] | `internal/browser_automation/service/task.go` | 41 | 94 | 41 | 0 |
| 372 | [ ] | `internal/browser_automation/service/timeouts.go` | 28 | 81 | 28 | 0 |
| 373 | [ ] | `internal/browser_automation/service/write_ledger.go` | 33 | 131 | 33 | 0 |
| 374 | [ ] | `internal/cache/cache.go` | 1 | 1 | 1 | 0 |
| 375 | [ ] | `internal/cache/global.go` | 4 | 14 | 4 | 0 |
| 376 | [ ] | `internal/cache/manager.go` | 23 | 24 | 23 | 0 |
| 377 | [ ] | `internal/cache/memory.go` | 25 | 35 | 25 | 0 |
| 378 | [ ] | `internal/cache/redis.go` | 23 | 27 | 23 | 0 |
| 379 | [ ] | `internal/capregistry/capregistry.go` | 14 | 15 | 14 | 0 |
| 380 | [ ] | `internal/capregistry/channels.go` | 5 | 6 | 5 | 0 |
| 381 | [ ] | `internal/channelbot/core/apierror.go` | 6 | 16 | 6 | 0 |
| 382 | [ ] | `internal/channelbot/core/core.go` | 24 | 73 | 24 | 0 |
| 383 | [ ] | `internal/channelbot/qq/qq.go` | 60 | 117 | 60 | 0 |
| 384 | [ ] | `internal/channelbot/telegram/telegram.go` | 70 | 152 | 70 | 0 |
| 385 | [ ] | `internal/channelbot/whatsapp/whatsapp.go` | 23 | 54 | 23 | 0 |
| 386 | [ ] | `internal/channelgw/pipeline.go` | 2 | 13 | 2 | 0 |
| 387 | [ ] | `internal/channelgw/protocol.go` | 17 | 74 | 17 | 0 |
| 388 | [ ] | `internal/channelgw/registry.go` | 12 | 22 | 12 | 0 |
| 389 | [ ] | `internal/channelgw/ws.go` | 10 | 28 | 10 | 0 |
| 390 | [ ] | `internal/config/admin.go` | 6 | 20 | 6 | 0 |
| 391 | [ ] | `internal/config/init.go` | 1 | 4 | 1 | 0 |
| 392 | [ ] | `internal/config/platform.go` | 11 | 53 | 11 | 0 |
| 393 | [ ] | `internal/config/ports.go` | 5 | 14 | 5 | 0 |
| 394 | [ ] | `internal/config/server.go` | 42 | 163 | 42 | 0 |
| 395 | [ ] | `internal/config/sso.go` | 1 | 1 | 1 | 0 |
| 396 | [ ] | `internal/config/website.go` | 6 | 20 | 6 | 0 |
| 397 | [ ] | `internal/config/ws_origin.go` | 3 | 16 | 3 | 0 |
| 398 | [ ] | `internal/content/controller/ai_content.go` | 16 | 16 | 16 | 0 |
| 399 | [ ] | `internal/content/controller/batch_operation.go` | 26 | 34 | 26 | 0 |
| 400 | [ ] | `internal/content/controller/marketing_flow.go` | 14 | 15 | 14 | 0 |
| 401 | [ ] | `internal/content/controller/material.go` | 17 | 37 | 17 | 0 |
| 402 | [ ] | `internal/content/controller/script_template.go` | 11 | 11 | 11 | 0 |
| 403 | [ ] | `internal/content/controller/template_market.go` | 10 | 10 | 10 | 0 |
| 404 | [ ] | `internal/content/dto/material.go` | 9 | 9 | 9 | 0 |
| 405 | [ ] | `internal/content/model/ai_content.go` | 6 | 6 | 6 | 0 |
| 406 | [ ] | `internal/content/model/batch_operation_history.go` | 2 | 2 | 2 | 0 |
| 407 | [ ] | `internal/content/model/market_template.go` | 4 | 4 | 4 | 0 |
| 408 | [ ] | `internal/content/model/marketing_flow.go` | 9 | 9 | 9 | 0 |
| 409 | [ ] | `internal/content/model/material.go` | 4 | 4 | 4 | 0 |
| 410 | [ ] | `internal/content/model/script_template.go` | 6 | 6 | 6 | 0 |
| 411 | [ ] | `internal/content/model/workflow.go` | 0 | 0 | 0 | 0 |
| 412 | [ ] | `internal/content/repository/ai_content.go` | 4 | 4 | 4 | 0 |
| 413 | [ ] | `internal/content/repository/batch_operation.go` | 6 | 8 | 6 | 0 |
| 414 | [ ] | `internal/content/repository/marketing_flow.go` | 22 | 22 | 22 | 0 |
| 415 | [ ] | `internal/content/repository/material.go` | 0 | 0 | 0 | 0 |
| 416 | [ ] | `internal/content/repository/material_category.go` | 0 | 0 | 0 | 0 |
| 417 | [ ] | `internal/content/repository/script_template.go` | 21 | 21 | 21 | 0 |
| 418 | [ ] | `internal/content/service/ai_content.go` | 24 | 24 | 24 | 0 |
| 419 | [ ] | `internal/content/service/batch_operation.go` | 20 | 20 | 20 | 0 |
| 420 | [ ] | `internal/content/service/marketing_flow.go` | 7 | 8 | 7 | 0 |
| 421 | [ ] | `internal/content/service/marketing_flow_action.go` | 1 | 6 | 1 | 0 |
| 422 | [ ] | `internal/content/service/marketing_flow_condition.go` | 2 | 2 | 2 | 0 |
| 423 | [ ] | `internal/content/service/material.go` | 3 | 3 | 3 | 0 |
| 424 | [ ] | `internal/content/service/script_template.go` | 16 | 25 | 16 | 0 |
| 425 | [ ] | `internal/content/service/script_template_sync.go` | 5 | 9 | 5 | 0 |
| 426 | [ ] | `internal/content/service/template_market.go` | 12 | 13 | 12 | 0 |
| 427 | [ ] | `internal/contract/stream_engine.go` | 2 | 34 | 2 | 0 |
| 428 | [ ] | `internal/controller/account.go` | 1 | 1 | 1 | 0 |
| 429 | [ ] | `internal/controller/admin_config.go` | 3 | 10 | 3 | 0 |
| 430 | [ ] | `internal/controller/agent_attribution.go` | 4 | 4 | 4 | 0 |
| 431 | [ ] | `internal/controller/agent_co_pilot.go` | 11 | 15 | 11 | 0 |
| 432 | [ ] | `internal/controller/agent_kb_binding.go` | 10 | 14 | 10 | 0 |
| 433 | [ ] | `internal/controller/agent_settings_config.go` | 4 | 4 | 4 | 0 |
| 434 | [ ] | `internal/controller/agent_status.go` | 11 | 19 | 11 | 0 |
| 435 | [ ] | `internal/controller/ai_agent.go` | 16 | 47 | 16 | 0 |
| 436 | [ ] | `internal/controller/ai_suggestion.go` | 4 | 4 | 4 | 0 |
| 437 | [ ] | `internal/controller/ai_tool_config_ctrl.go` | 9 | 16 | 9 | 0 |
| 438 | [ ] | `internal/controller/alert_rule.go` | 11 | 94 | 11 | 0 |
| 439 | [ ] | `internal/controller/anomaly_login_detector.go` | 6 | 47 | 6 | 0 |
| 440 | [ ] | `internal/controller/app_config.go` | 16 | 17 | 16 | 0 |
| 441 | [ ] | `internal/controller/approval.go` | 16 | 64 | 16 | 0 |
| 442 | [ ] | `internal/controller/asset_bundle.go` | 19 | 44 | 19 | 0 |
| 443 | [ ] | `internal/controller/asset_market.go` | 17 | 24 | 17 | 0 |
| 444 | [ ] | `internal/controller/auth.go` | 40 | 143 | 40 | 0 |
| 445 | [ ] | `internal/controller/auth_register.go` | 7 | 31 | 7 | 0 |
| 446 | [ ] | `internal/controller/backup.go` | 16 | 24 | 16 | 0 |
| 447 | [ ] | `internal/controller/bad_case.go` | 21 | 49 | 21 | 0 |
| 448 | [ ] | `internal/controller/base_card.go` | 4 | 7 | 4 | 0 |
| 449 | [ ] | `internal/controller/bill.go` | 35 | 201 | 35 | 0 |
| 450 | [ ] | `internal/controller/bridge_account.go` | 3 | 6 | 3 | 0 |
| 451 | [ ] | `internal/controller/bridge_capabilities.go` | 3 | 26 | 3 | 0 |
| 452 | [ ] | `internal/controller/bridge_token.go` | 3 | 20 | 3 | 0 |
| 453 | [ ] | `internal/controller/capabilities.go` | 1 | 1 | 1 | 0 |
| 454 | [ ] | `internal/controller/card_stats_factory.go` | 4 | 16 | 4 | 0 |
| 455 | [ ] | `internal/controller/channel_overview.go` | 4 | 7 | 4 | 0 |
| 456 | [ ] | `internal/controller/channel_ownership.go` | 0 | 0 | 0 | 0 |
| 457 | [ ] | `internal/controller/chat_channel.go` | 9 | 23 | 9 | 0 |
| 458 | [ ] | `internal/controller/chat_public.go` | 16 | 83 | 16 | 0 |
| 459 | [ ] | `internal/controller/chat_ws.go` | 5 | 57 | 5 | 0 |
| 460 | [ ] | `internal/controller/chat_ws_hub.go` | 19 | 84 | 19 | 0 |
| 461 | [ ] | `internal/controller/clue.go` | 4 | 37 | 4 | 0 |
| 462 | [ ] | `internal/controller/clue_score.go` | 7 | 40 | 7 | 0 |
| 463 | [ ] | `internal/controller/community.go` | 16 | 16 | 16 | 0 |
| 464 | [ ] | `internal/controller/config_param.go` | 10 | 38 | 10 | 0 |
| 465 | [ ] | `internal/controller/config_param_runtime.go` | 13 | 66 | 13 | 0 |
| 466 | [ ] | `internal/controller/csat.go` | 11 | 11 | 11 | 0 |
| 467 | [ ] | `internal/controller/customer.go` | 8 | 52 | 8 | 0 |
| 468 | [ ] | `internal/controller/customer_360.go` | 24 | 52 | 24 | 0 |
| 469 | [ ] | `internal/controller/customer_event.go` | 12 | 86 | 12 | 0 |
| 470 | [ ] | `internal/controller/customer_journey.go` | 8 | 8 | 8 | 0 |
| 471 | [ ] | `internal/controller/customer_oneid.go` | 15 | 75 | 15 | 0 |
| 472 | [ ] | `internal/controller/customer_queue.go` | 7 | 10 | 7 | 0 |
| 473 | [ ] | `internal/controller/customer_rfm.go` | 7 | 37 | 7 | 0 |
| 474 | [ ] | `internal/controller/customer_service_plus.go` | 26 | 27 | 26 | 0 |
| 475 | [ ] | `internal/controller/customer_session.go` | 20 | 22 | 20 | 0 |
| 476 | [ ] | `internal/controller/customer_session_blacklist.go` | 4 | 17 | 4 | 0 |
| 477 | [ ] | `internal/controller/customer_session_routing.go` | 6 | 6 | 6 | 0 |
| 478 | [ ] | `internal/controller/customer_session_takeover.go` | 6 | 17 | 6 | 0 |
| 479 | [ ] | `internal/controller/dashboard_sse.go` | 8 | 20 | 8 | 0 |
| 480 | [ ] | `internal/controller/data_export.go` | 7 | 9 | 7 | 0 |
| 481 | [ ] | `internal/controller/debug_routes.go` | 1 | 1 | 1 | 0 |
| 482 | [ ] | `internal/controller/dialogue_memory.go` | 18 | 18 | 18 | 0 |
| 483 | [ ] | `internal/controller/dictionary.go` | 7 | 23 | 7 | 0 |
| 484 | [ ] | `internal/controller/dingtalk_app_account.go` | 11 | 16 | 11 | 0 |
| 485 | [ ] | `internal/controller/dnc.go` | 7 | 7 | 7 | 0 |
| 486 | [ ] | `internal/controller/domain_pool.go` | 27 | 157 | 27 | 0 |
| 487 | [ ] | `internal/controller/douyin_card.go` | 9 | 9 | 9 | 0 |
| 488 | [ ] | `internal/controller/douyin_card_stats.go` | 4 | 4 | 4 | 0 |
| 489 | [ ] | `internal/controller/email_draft.go` | 7 | 7 | 7 | 0 |
| 490 | [ ] | `internal/controller/email_jobs.go` | 8 | 8 | 8 | 0 |
| 491 | [ ] | `internal/controller/email_list.go` | 8 | 8 | 8 | 0 |
| 492 | [ ] | `internal/controller/email_open_tracker.go` | 5 | 9 | 5 | 0 |
| 493 | [ ] | `internal/controller/email_send.go` | 1 | 11 | 1 | 0 |
| 494 | [ ] | `internal/controller/email_smtp.go` | 7 | 7 | 7 | 0 |
| 495 | [ ] | `internal/controller/email_tracking.go` | 9 | 32 | 9 | 0 |
| 496 | [ ] | `internal/controller/email_unsubscribe.go` | 10 | 23 | 10 | 0 |
| 497 | [ ] | `internal/controller/embedding_config.go` | 5 | 23 | 5 | 0 |
| 498 | [ ] | `internal/controller/error_helper.go` | 2 | 2 | 2 | 0 |
| 499 | [ ] | `internal/controller/faq.go` | 11 | 52 | 11 | 0 |
| 500 | [ ] | `internal/controller/feature_flag.go` | 18 | 18 | 18 | 0 |
| 501 | [ ] | `internal/controller/feishu_account.go` | 11 | 22 | 11 | 0 |
| 502 | [ ] | `internal/controller/followup.go` | 13 | 40 | 13 | 0 |
| 503 | [ ] | `internal/controller/glossary.go` | 9 | 15 | 9 | 0 |
| 504 | [ ] | `internal/controller/handoff_chain.go` | 9 | 12 | 9 | 0 |
| 505 | [ ] | `internal/controller/help_center.go` | 9 | 9 | 9 | 0 |
| 506 | [ ] | `internal/controller/human_task.go` | 28 | 63 | 28 | 0 |
| 507 | [ ] | `internal/controller/i18n_stats.go` | 10 | 17 | 10 | 0 |
| 508 | [ ] | `internal/controller/inbox.go` | 31 | 60 | 31 | 0 |
| 509 | [ ] | `internal/controller/integration.go` | 21 | 45 | 21 | 0 |
| 510 | [ ] | `internal/controller/intent.go` | 19 | 63 | 19 | 0 |
| 511 | [ ] | `internal/controller/kb_connector.go` | 8 | 10 | 8 | 0 |
| 512 | [ ] | `internal/controller/kb_connectors.go` | 7 | 7 | 7 | 0 |
| 513 | [ ] | `internal/controller/kb_release.go` | 36 | 110 | 36 | 0 |
| 514 | [ ] | `internal/controller/knowledge_base.go` | 20 | 54 | 20 | 0 |
| 515 | [ ] | `internal/controller/kuaishou_card.go` | 13 | 13 | 13 | 0 |
| 516 | [ ] | `internal/controller/kuaishou_card_stats.go` | 4 | 4 | 4 | 0 |
| 517 | [ ] | `internal/controller/lead_mining.go` | 2 | 2 | 2 | 0 |
| 518 | [ ] | `internal/controller/live_code.go` | 17 | 17 | 17 | 0 |
| 519 | [ ] | `internal/controller/llm_provider.go` | 8 | 26 | 8 | 0 |
| 520 | [ ] | `internal/controller/llm_routing.go` | 24 | 87 | 24 | 0 |
| 521 | [ ] | `internal/controller/mcp.go` | 2 | 13 | 2 | 0 |
| 522 | [ ] | `internal/controller/mcp_credential.go` | 7 | 11 | 7 | 0 |
| 523 | [ ] | `internal/controller/menu.go` | 7 | 20 | 7 | 0 |
| 524 | [ ] | `internal/controller/message_hub.go` | 11 | 11 | 11 | 0 |
| 525 | [ ] | `internal/controller/migration.go` | 9 | 23 | 9 | 0 |
| 526 | [ ] | `internal/controller/module_llm.go` | 7 | 33 | 7 | 0 |
| 527 | [ ] | `internal/controller/module_market.go` | 13 | 31 | 13 | 0 |
| 528 | [ ] | `internal/controller/module_paging.go` | 2 | 6 | 2 | 0 |
| 529 | [ ] | `internal/controller/notification.go` | 6 | 11 | 6 | 0 |
| 530 | [ ] | `internal/controller/objection_handler.go` | 7 | 7 | 7 | 0 |
| 531 | [ ] | `internal/controller/obs_config.go` | 10 | 13 | 10 | 0 |
| 532 | [ ] | `internal/controller/operation_log.go` | 11 | 18 | 11 | 0 |
| 533 | [ ] | `internal/controller/operator.go` | 3 | 16 | 3 | 0 |
| 534 | [ ] | `internal/controller/opportunity.go` | 36 | 212 | 36 | 0 |
| 535 | [ ] | `internal/controller/order_draft.go` | 26 | 103 | 26 | 0 |
| 536 | [ ] | `internal/controller/param_parse.go` | 0 | 0 | 0 | 0 |
| 537 | [ ] | `internal/controller/password_reset.go` | 5 | 24 | 5 | 0 |
| 538 | [ ] | `internal/controller/permission.go` | 5 | 11 | 5 | 0 |
| 539 | [ ] | `internal/controller/platform.go` | 15 | 26 | 15 | 0 |
| 540 | [ ] | `internal/controller/proactive_reach.go` | 9 | 17 | 9 | 0 |
| 541 | [ ] | `internal/controller/prompt.go` | 8 | 13 | 8 | 0 |
| 542 | [ ] | `internal/controller/public_tenant.go` | 1 | 6 | 1 | 0 |
| 543 | [ ] | `internal/controller/qq_account.go` | 14 | 29 | 14 | 0 |
| 544 | [ ] | `internal/controller/quick_reply.go` | 7 | 7 | 7 | 0 |
| 545 | [ ] | `internal/controller/quote.go` | 45 | 243 | 45 | 0 |
| 546 | [ ] | `internal/controller/r39_extras.go` | 9 | 12 | 9 | 0 |
| 547 | [ ] | `internal/controller/r44_gap_endpoints.go` | 34 | 35 | 34 | 0 |
| 548 | [ ] | `internal/controller/r48_growth.go` | 14 | 14 | 14 | 0 |
| 549 | [ ] | `internal/controller/r48_session_tools.go` | 10 | 10 | 10 | 0 |
| 550 | [ ] | `internal/controller/rag_eval.go` | 11 | 13 | 11 | 0 |
| 551 | [ ] | `internal/controller/rag_health.go` | 5 | 19 | 5 | 0 |
| 552 | [ ] | `internal/controller/rag_product.go` | 0 | 0 | 0 | 0 |
| 553 | [ ] | `internal/controller/rag_recall_monitor.go` | 11 | 58 | 11 | 0 |
| 554 | [ ] | `internal/controller/rate_quota.go` | 3 | 4 | 3 | 0 |
| 555 | [ ] | `internal/controller/reach_pipeline.go` | 18 | 18 | 18 | 0 |
| 556 | [ ] | `internal/controller/recovery_queue.go` | 9 | 72 | 9 | 0 |
| 557 | [ ] | `internal/controller/redirect.go` | 5 | 20 | 5 | 0 |
| 558 | [ ] | `internal/controller/role.go` | 5 | 12 | 5 | 0 |
| 559 | [ ] | `internal/controller/rule_engine.go` | 7 | 7 | 7 | 0 |
| 560 | [ ] | `internal/controller/sales_cockpit.go` | 4 | 4 | 4 | 0 |
| 561 | [ ] | `internal/controller/sales_persona.go` | 5 | 21 | 5 | 0 |
| 562 | [ ] | `internal/controller/sales_workbench.go` | 9 | 26 | 9 | 0 |
| 563 | [ ] | `internal/controller/script_library.go` | 10 | 10 | 10 | 0 |
| 564 | [ ] | `internal/controller/security_audit.go` | 5 | 5 | 5 | 0 |
| 565 | [ ] | `internal/controller/security_rules.go` | 4 | 4 | 4 | 0 |
| 566 | [ ] | `internal/controller/session_tag.go` | 6 | 6 | 6 | 0 |
| 567 | [ ] | `internal/controller/short_link.go` | 9 | 28 | 9 | 0 |
| 568 | [ ] | `internal/controller/short_link_stats.go` | 5 | 5 | 5 | 0 |
| 569 | [ ] | `internal/controller/slo.go` | 3 | 4 | 3 | 0 |
| 570 | [ ] | `internal/controller/smart_router.go` | 7 | 9 | 7 | 0 |
| 571 | [ ] | `internal/controller/sms.go` | 23 | 57 | 23 | 0 |
| 572 | [ ] | `internal/controller/sms_delivery_tracker.go` | 9 | 49 | 9 | 0 |
| 573 | [ ] | `internal/controller/sms_unsubscribe.go` | 11 | 33 | 11 | 0 |
| 574 | [ ] | `internal/controller/sop.go` | 25 | 83 | 25 | 0 |
| 575 | [ ] | `internal/controller/sop_heatmap.go` | 3 | 17 | 3 | 0 |
| 576 | [ ] | `internal/controller/sop_template.go` | 10 | 10 | 10 | 0 |
| 577 | [ ] | `internal/controller/sse_dashboard.go` | 9 | 15 | 9 | 0 |
| 578 | [ ] | `internal/controller/sso.go` | 14 | 35 | 14 | 0 |
| 579 | [ ] | `internal/controller/subscription.go` | 4 | 13 | 4 | 0 |
| 580 | [ ] | `internal/controller/support_metrics.go` | 3 | 3 | 3 | 0 |
| 581 | [ ] | `internal/controller/system_config.go` | 5 | 6 | 5 | 0 |
| 582 | [ ] | `internal/controller/system_info.go` | 5 | 23 | 5 | 0 |
| 583 | [ ] | `internal/controller/system_init.go` | 12 | 60 | 12 | 0 |
| 584 | [ ] | `internal/controller/system_monitor.go` | 0 | 0 | 0 | 0 |
| 585 | [ ] | `internal/controller/system_ops.go` | 8 | 11 | 8 | 0 |
| 586 | [ ] | `internal/controller/system_user.go` | 7 | 9 | 7 | 0 |
| 587 | [ ] | `internal/controller/telegram_account.go` | 22 | 43 | 22 | 0 |
| 588 | [ ] | `internal/controller/telegram_gate.go` | 9 | 9 | 9 | 0 |
| 589 | [ ] | `internal/controller/tenant.go` | 10 | 18 | 10 | 0 |
| 590 | [ ] | `internal/controller/tiktok_card.go` | 1 | 4 | 1 | 0 |
| 591 | [ ] | `internal/controller/tool_integration_config.go` | 4 | 4 | 4 | 0 |
| 592 | [ ] | `internal/controller/trace.go` | 3 | 9 | 3 | 0 |
| 593 | [ ] | `internal/controller/trace_learning.go` | 5 | 10 | 5 | 0 |
| 594 | [ ] | `internal/controller/tuning.go` | 19 | 19 | 19 | 0 |
| 595 | [ ] | `internal/controller/typing_predict.go` | 7 | 20 | 7 | 0 |
| 596 | [ ] | `internal/controller/unified_message.go` | 14 | 18 | 14 | 0 |
| 597 | [ ] | `internal/controller/upload.go` | 10 | 30 | 10 | 0 |
| 598 | [ ] | `internal/controller/user.go` | 5 | 5 | 5 | 0 |
| 599 | [ ] | `internal/controller/user_segment.go` | 12 | 22 | 12 | 0 |
| 600 | [ ] | `internal/controller/webhook.go` | 29 | 117 | 29 | 0 |
| 601 | [ ] | `internal/controller/wechat.go` | 16 | 32 | 16 | 0 |
| 602 | [ ] | `internal/controller/wechat_body_limit.go` | 1 | 6 | 1 | 0 |
| 603 | [ ] | `internal/controller/wecom.go` | 16 | 19 | 16 | 0 |
| 604 | [ ] | `internal/controller/wecom_health.go` | 14 | 18 | 14 | 0 |
| 605 | [ ] | `internal/controller/whatsapp.go` | 11 | 12 | 11 | 0 |
| 606 | [ ] | `internal/controller/whatsapp_cloud_account.go` | 9 | 19 | 9 | 0 |
| 607 | [ ] | `internal/controller/whatsapp_group_messaging.go` | 17 | 22 | 17 | 0 |
| 608 | [ ] | `internal/controller/workflow_orchestrator.go` | 12 | 127 | 12 | 0 |
| 609 | [ ] | `internal/controller/xianyu_card.go` | 13 | 14 | 13 | 0 |
| 610 | [ ] | `internal/controller/xianyu_card_stats.go` | 5 | 10 | 5 | 0 |
| 611 | [ ] | `internal/controller/xiaohongshu_card.go` | 10 | 10 | 10 | 0 |
| 612 | [ ] | `internal/controller/xiaohongshu_card_stats.go` | 3 | 4 | 3 | 0 |
| 613 | [ ] | `internal/cron/domain_health_job.go` | 3 | 4 | 3 | 0 |
| 614 | [ ] | `internal/cron/live_code_rotator.go` | 3 | 3 | 3 | 0 |
| 615 | [ ] | `internal/domain/asset/entity.go` | 4 | 4 | 4 | 0 |
| 616 | [ ] | `internal/domain/asset/validator.go` | 1 | 1 | 1 | 0 |
| 617 | [ ] | `internal/domain/errors/codes.go` | 20 | 43 | 20 | 0 |
| 618 | [ ] | `internal/dto/account.go` | 1 | 2 | 1 | 0 |
| 619 | [ ] | `internal/dto/asset_bundle.go` | 17 | 34 | 17 | 0 |
| 620 | [ ] | `internal/dto/asset_market.go` | 4 | 4 | 4 | 0 |
| 621 | [ ] | `internal/dto/auth.go` | 0 | 0 | 0 | 0 |
| 622 | [ ] | `internal/dto/card_stats.go` | 4 | 7 | 4 | 0 |
| 623 | [ ] | `internal/dto/channel_overview.go` | 4 | 4 | 4 | 0 |
| 624 | [ ] | `internal/dto/clue.go` | 2 | 5 | 2 | 0 |
| 625 | [ ] | `internal/dto/clue_score.go` | 7 | 7 | 7 | 0 |
| 626 | [ ] | `internal/dto/community.go` | 14 | 14 | 14 | 0 |
| 627 | [ ] | `internal/dto/confidence.go` | 7 | 18 | 7 | 0 |
| 628 | [ ] | `internal/dto/customer_rfm.go` | 6 | 6 | 6 | 0 |
| 629 | [ ] | `internal/dto/domain_pool.go` | 6 | 7 | 6 | 0 |
| 630 | [ ] | `internal/dto/douyincard.go` | 7 | 7 | 7 | 0 |
| 631 | [ ] | `internal/dto/douyincardstats.go` | 7 | 11 | 7 | 0 |
| 632 | [ ] | `internal/dto/emaildraft.go` | 4 | 4 | 4 | 0 |
| 633 | [ ] | `internal/dto/emailjobs.go` | 4 | 4 | 4 | 0 |
| 634 | [ ] | `internal/dto/emaillist.go` | 4 | 4 | 4 | 0 |
| 635 | [ ] | `internal/dto/emailsend.go` | 2 | 2 | 2 | 0 |
| 636 | [ ] | `internal/dto/emailsmtp.go` | 0 | 0 | 0 | 0 |
| 637 | [ ] | `internal/dto/feedback_loop.go` | 10 | 20 | 10 | 0 |
| 638 | [ ] | `internal/dto/filters.go` | 1 | 6 | 1 | 0 |
| 639 | [ ] | `internal/dto/glossary.go` | 7 | 7 | 7 | 0 |
| 640 | [ ] | `internal/dto/humanize.go` | 10 | 17 | 10 | 0 |
| 641 | [ ] | `internal/dto/integration_template.go` | 4 | 4 | 4 | 0 |
| 642 | [ ] | `internal/dto/intent.go` | 1 | 1 | 1 | 0 |
| 643 | [ ] | `internal/dto/kuaishoucard.go` | 7 | 7 | 7 | 0 |
| 644 | [ ] | `internal/dto/kuaishoucardstats.go` | 7 | 10 | 7 | 0 |
| 645 | [ ] | `internal/dto/layer_chunk.go` | 9 | 22 | 9 | 0 |
| 646 | [ ] | `internal/dto/lead_mining.go` | 1 | 3 | 1 | 0 |
| 647 | [ ] | `internal/dto/livecode.go` | 12 | 12 | 12 | 0 |
| 648 | [ ] | `internal/dto/memory.go` | 2 | 2 | 2 | 0 |
| 649 | [ ] | `internal/dto/obs_config.go` | 5 | 9 | 5 | 0 |
| 650 | [ ] | `internal/dto/recovery_queue.go` | 12 | 16 | 12 | 0 |
| 651 | [ ] | `internal/dto/sales.go` | 30 | 39 | 30 | 0 |
| 652 | [ ] | `internal/dto/shortlink.go` | 11 | 11 | 11 | 0 |
| 653 | [ ] | `internal/dto/shortlink_stats.go` | 10 | 10 | 10 | 0 |
| 654 | [ ] | `internal/dto/sms.go` | 12 | 12 | 12 | 0 |
| 655 | [ ] | `internal/dto/sop.go` | 10 | 13 | 10 | 0 |
| 656 | [ ] | `internal/dto/tiktok_card.go` | 10 | 11 | 10 | 0 |
| 657 | [ ] | `internal/dto/user.go` | 0 | 0 | 0 | 0 |
| 658 | [ ] | `internal/dto/workflow_orchestrator.go` | 3 | 3 | 3 | 0 |
| 659 | [ ] | `internal/dto/xianyucard.go` | 5 | 5 | 5 | 0 |
| 660 | [ ] | `internal/dto/xianyucardstats.go` | 8 | 8 | 8 | 0 |
| 661 | [ ] | `internal/dto/xiaohongshucard.go` | 5 | 5 | 5 | 0 |
| 662 | [ ] | `internal/dto/xiaohongshucardstats.go` | 4 | 4 | 4 | 0 |
| 663 | [ ] | `internal/email/service/email_draft.go` | 11 | 14 | 11 | 0 |
| 664 | [ ] | `internal/email/service/email_drain_worker.go` | 9 | 25 | 9 | 0 |
| 665 | [ ] | `internal/email/service/email_jobs.go` | 15 | 15 | 15 | 0 |
| 666 | [ ] | `internal/email/service/email_list.go` | 13 | 13 | 13 | 0 |
| 667 | [ ] | `internal/email/service/email_send.go` | 35 | 125 | 35 | 0 |
| 668 | [ ] | `internal/email/service/email_smtp.go` | 5 | 6 | 5 | 0 |
| 669 | [ ] | `internal/etl/document_processor.go` | 8 | 8 | 8 | 0 |
| 670 | [ ] | `internal/etl/document_text.go` | 8 | 21 | 8 | 0 |
| 671 | [ ] | `internal/event/bus.go` | 14 | 44 | 14 | 0 |
| 672 | [ ] | `internal/event/types.go` | 12 | 52 | 12 | 0 |
| 673 | [ ] | `internal/geo/controller/alert.go` | 6 | 15 | 6 | 0 |
| 674 | [ ] | `internal/geo/controller/citation.go` | 5 | 7 | 5 | 0 |
| 675 | [ ] | `internal/geo/controller/competitor.go` | 6 | 6 | 6 | 0 |
| 676 | [ ] | `internal/geo/controller/config.go` | 5 | 8 | 5 | 0 |
| 677 | [ ] | `internal/geo/controller/content.go` | 10 | 18 | 10 | 0 |
| 678 | [ ] | `internal/geo/controller/decision.go` | 4 | 8 | 4 | 0 |
| 679 | [ ] | `internal/geo/controller/dict.go` | 4 | 4 | 4 | 0 |
| 680 | [ ] | `internal/geo/controller/geo_pusher_config.go` | 1 | 1 | 1 | 0 |
| 681 | [ ] | `internal/geo/controller/geo_schema_template.go` | 1 | 1 | 1 | 0 |
| 682 | [ ] | `internal/geo/controller/geo_site.go` | 1 | 1 | 1 | 0 |
| 683 | [ ] | `internal/geo/controller/index_tracker.go` | 4 | 4 | 4 | 0 |
| 684 | [ ] | `internal/geo/controller/job.go` | 6 | 10 | 6 | 0 |
| 685 | [ ] | `internal/geo/controller/kb.go` | 8 | 14 | 8 | 0 |
| 686 | [ ] | `internal/geo/controller/keyword.go` | 7 | 12 | 7 | 0 |
| 687 | [ ] | `internal/geo/controller/keyword_enhance.go` | 4 | 6 | 4 | 0 |
| 688 | [ ] | `internal/geo/controller/keyword_mining.go` | 6 | 13 | 6 | 0 |
| 689 | [ ] | `internal/geo/controller/opportunity.go` | 3 | 4 | 3 | 0 |
| 690 | [ ] | `internal/geo/controller/platform.go` | 8 | 14 | 8 | 0 |
| 691 | [ ] | `internal/geo/controller/probe.go` | 17 | 28 | 17 | 0 |
| 692 | [ ] | `internal/geo/controller/push.go` | 4 | 5 | 4 | 0 |
| 693 | [ ] | `internal/geo/controller/report.go` | 10 | 19 | 10 | 0 |
| 694 | [ ] | `internal/geo/controller/seed_term.go` | 3 | 4 | 3 | 0 |
| 695 | [ ] | `internal/geo/controller/site.go` | 6 | 7 | 6 | 0 |
| 696 | [ ] | `internal/geo/controller/tech_metrics.go` | 7 | 15 | 7 | 0 |
| 697 | [ ] | `internal/geo/controller/verification.go` | 5 | 8 | 5 | 0 |
| 698 | [ ] | `internal/geo/controller/visibility.go` | 5 | 6 | 5 | 0 |
| 699 | [ ] | `internal/geo/controller/workflow.go` | 11 | 22 | 11 | 0 |
| 700 | [ ] | `internal/geo/doc.go` | 1 | 37 | 1 | 0 |
| 701 | [ ] | `internal/geo/dto/content.go` | 5 | 5 | 5 | 0 |
| 702 | [ ] | `internal/geo/dto/keyword.go` | 5 | 5 | 5 | 0 |
| 703 | [ ] | `internal/geo/dto/keyword_enhance.go` | 2 | 2 | 2 | 0 |
| 704 | [ ] | `internal/geo/dto/knowledge.go` | 5 | 5 | 5 | 0 |
| 705 | [ ] | `internal/geo/dto/platform.go` | 8 | 9 | 8 | 0 |
| 706 | [ ] | `internal/geo/dto/report.go` | 4 | 4 | 4 | 0 |
| 707 | [ ] | `internal/geo/dto/verification.go` | 2 | 2 | 2 | 0 |
| 708 | [ ] | `internal/geo/dto/workflow.go` | 10 | 13 | 10 | 0 |
| 709 | [ ] | `internal/geo/model/alert.go` | 1 | 2 | 1 | 0 |
| 710 | [ ] | `internal/geo/model/competitor.go` | 1 | 1 | 1 | 0 |
| 711 | [ ] | `internal/geo/model/config.go` | 5 | 5 | 5 | 0 |
| 712 | [ ] | `internal/geo/model/content.go` | 4 | 4 | 4 | 0 |
| 713 | [ ] | `internal/geo/model/crawler_visit.go` | 1 | 1 | 1 | 0 |
| 714 | [ ] | `internal/geo/model/daily_stats.go` | 1 | 4 | 1 | 0 |
| 715 | [ ] | `internal/geo/model/dict.go` | 12 | 16 | 12 | 0 |
| 716 | [ ] | `internal/geo/model/entity.go` | 2 | 8 | 2 | 0 |
| 717 | [ ] | `internal/geo/model/index_tracking.go` | 2 | 6 | 2 | 0 |
| 718 | [ ] | `internal/geo/model/job_run.go` | 1 | 2 | 1 | 0 |
| 719 | [ ] | `internal/geo/model/keyword.go` | 8 | 8 | 8 | 0 |
| 720 | [ ] | `internal/geo/model/knowledge.go` | 1 | 1 | 1 | 0 |
| 721 | [ ] | `internal/geo/model/probe.go` | 1 | 5 | 1 | 0 |
| 722 | [ ] | `internal/geo/model/push_record.go` | 2 | 5 | 2 | 0 |
| 723 | [ ] | `internal/geo/model/pusher_config.go` | 1 | 4 | 1 | 0 |
| 724 | [ ] | `internal/geo/model/query_chain.go` | 5 | 9 | 5 | 0 |
| 725 | [ ] | `internal/geo/model/schema_template.go` | 3 | 6 | 3 | 0 |
| 726 | [ ] | `internal/geo/model/seed_term.go` | 2 | 6 | 2 | 0 |
| 727 | [ ] | `internal/geo/model/site.go` | 1 | 4 | 1 | 0 |
| 728 | [ ] | `internal/geo/model/source_catalog.go` | 1 | 5 | 1 | 0 |
| 729 | [ ] | `internal/geo/model/verification.go` | 2 | 2 | 2 | 0 |
| 730 | [ ] | `internal/geo/model/workflow.go` | 9 | 9 | 9 | 0 |
| 731 | [ ] | `internal/geo/repository/alert.go` | 2 | 2 | 2 | 0 |
| 732 | [ ] | `internal/geo/repository/competitor.go` | 1 | 1 | 1 | 0 |
| 733 | [ ] | `internal/geo/repository/config.go` | 6 | 11 | 6 | 0 |
| 734 | [ ] | `internal/geo/repository/content.go` | 4 | 4 | 4 | 0 |
| 735 | [ ] | `internal/geo/repository/crawler_visit.go` | 15 | 25 | 15 | 0 |
| 736 | [ ] | `internal/geo/repository/daily_stats.go` | 2 | 6 | 2 | 0 |
| 737 | [ ] | `internal/geo/repository/dict.go` | 11 | 16 | 11 | 0 |
| 738 | [ ] | `internal/geo/repository/entity.go` | 3 | 4 | 3 | 0 |
| 739 | [ ] | `internal/geo/repository/job_run.go` | 4 | 4 | 4 | 0 |
| 740 | [ ] | `internal/geo/repository/keyword.go` | 2 | 2 | 2 | 0 |
| 741 | [ ] | `internal/geo/repository/keyword_group.go` | 4 | 4 | 4 | 0 |
| 742 | [ ] | `internal/geo/repository/knowledge.go` | 2 | 2 | 2 | 0 |
| 743 | [ ] | `internal/geo/repository/probe.go` | 2 | 2 | 2 | 0 |
| 744 | [ ] | `internal/geo/repository/query_chain.go` | 6 | 10 | 6 | 0 |
| 745 | [ ] | `internal/geo/repository/seed_term.go` | 8 | 9 | 8 | 0 |
| 746 | [ ] | `internal/geo/repository/source_catalog.go` | 3 | 4 | 3 | 0 |
| 747 | [ ] | `internal/geo/repository/verification.go` | 4 | 4 | 4 | 0 |
| 748 | [ ] | `internal/geo/repository/workflow.go` | 3 | 3 | 3 | 0 |
| 749 | [ ] | `internal/geo/service/alert.go` | 8 | 11 | 8 | 0 |
| 750 | [ ] | `internal/geo/service/browser_publisher.go` | 1 | 3 | 1 | 0 |
| 751 | [ ] | `internal/geo/service/citation.go` | 17 | 23 | 17 | 0 |
| 752 | [ ] | `internal/geo/service/citation_export.go` | 2 | 2 | 2 | 0 |
| 753 | [ ] | `internal/geo/service/competitor.go` | 0 | 0 | 0 | 0 |
| 754 | [ ] | `internal/geo/service/config.go` | 5 | 7 | 5 | 0 |
| 755 | [ ] | `internal/geo/service/content.go` | 9 | 14 | 9 | 0 |
| 756 | [ ] | `internal/geo/service/crawler.go` | 5 | 22 | 5 | 0 |
| 757 | [ ] | `internal/geo/service/decision_analytics.go` | 17 | 28 | 17 | 0 |
| 758 | [ ] | `internal/geo/service/decision_executors.go` | 8 | 12 | 8 | 0 |
| 759 | [ ] | `internal/geo/service/dict.go` | 12 | 23 | 12 | 0 |
| 760 | [ ] | `internal/geo/service/entity_extractor.go` | 3 | 16 | 3 | 0 |
| 761 | [ ] | `internal/geo/service/geo_audit.go` | 7 | 7 | 7 | 0 |
| 762 | [ ] | `internal/geo/service/geo_audit_live.go` | 5 | 7 | 5 | 0 |
| 763 | [ ] | `internal/geo/service/index_tracker.go` | 24 | 33 | 24 | 0 |
| 764 | [ ] | `internal/geo/service/intent_matrix.go` | 6 | 9 | 6 | 0 |
| 765 | [ ] | `internal/geo/service/job_manager.go` | 38 | 118 | 38 | 0 |
| 766 | [ ] | `internal/geo/service/kb.go` | 10 | 10 | 10 | 0 |
| 767 | [ ] | `internal/geo/service/keyword.go` | 11 | 17 | 11 | 0 |
| 768 | [ ] | `internal/geo/service/keyword_enhance.go` | 5 | 7 | 5 | 0 |
| 769 | [ ] | `internal/geo/service/keyword_mining.go` | 63 | 140 | 63 | 0 |
| 770 | [ ] | `internal/geo/service/keyword_pipeline.go` | 20 | 43 | 20 | 0 |
| 771 | [ ] | `internal/geo/service/keyword_seeds.go` | 17 | 42 | 17 | 0 |
| 772 | [ ] | `internal/geo/service/llm.go` | 9 | 12 | 9 | 0 |
| 773 | [ ] | `internal/geo/service/metrics.go` | 17 | 20 | 17 | 0 |
| 774 | [ ] | `internal/geo/service/monitor_crawler.go` | 10 | 25 | 10 | 0 |
| 775 | [ ] | `internal/geo/service/opportunity.go` | 11 | 11 | 11 | 0 |
| 776 | [ ] | `internal/geo/service/platform.go` | 9 | 9 | 9 | 0 |
| 777 | [ ] | `internal/geo/service/prompt_fanout.go` | 6 | 10 | 6 | 0 |
| 778 | [ ] | `internal/geo/service/prompt_manager.go` | 6 | 16 | 6 | 0 |
| 779 | [ ] | `internal/geo/service/prompts.go` | 15 | 26 | 15 | 0 |
| 780 | [ ] | `internal/geo/service/publish_pipeline.go` | 17 | 21 | 17 | 0 |
| 781 | [ ] | `internal/geo/service/push.go` | 39 | 72 | 39 | 0 |
| 782 | [ ] | `internal/geo/service/report.go` | 5 | 5 | 5 | 0 |
| 783 | [ ] | `internal/geo/service/scheduler.go` | 9 | 31 | 9 | 0 |
| 784 | [ ] | `internal/geo/service/search_probe.go` | 23 | 42 | 23 | 0 |
| 785 | [ ] | `internal/geo/service/seed_term.go` | 5 | 10 | 5 | 0 |
| 786 | [ ] | `internal/geo/service/site.go` | 21 | 33 | 21 | 0 |
| 787 | [ ] | `internal/geo/service/techconfig.go` | 8 | 10 | 8 | 0 |
| 788 | [ ] | `internal/geo/service/verification.go` | 3 | 3 | 3 | 0 |
| 789 | [ ] | `internal/geo/service/visibility.go` | 15 | 30 | 15 | 0 |
| 790 | [ ] | `internal/geo/service/workflow.go` | 16 | 16 | 16 | 0 |
| 791 | [ ] | `internal/identity/normalize.go` | 8 | 27 | 8 | 0 |
| 792 | [ ] | `internal/integration/templates/presets.go` | 2 | 6 | 2 | 0 |
| 793 | [ ] | `internal/kbconnector/ima.go` | 3 | 8 | 3 | 0 |
| 794 | [ ] | `internal/kbconnector/provider.go` | 10 | 11 | 10 | 0 |
| 795 | [ ] | `internal/kbconnector/sync.go` | 5 | 9 | 5 | 0 |
| 796 | [ ] | `internal/kbconnector/webhttp.go` | 1 | 6 | 1 | 0 |
| 797 | [ ] | `internal/middleware/ai_crawler_monitor.go` | 2 | 4 | 2 | 0 |
| 798 | [ ] | `internal/middleware/api_logger.go` | 1 | 9 | 1 | 0 |
| 799 | [ ] | `internal/middleware/app_key_auth.go` | 10 | 39 | 10 | 0 |
| 800 | [ ] | `internal/middleware/audit.go` | 20 | 24 | 20 | 0 |
| 801 | [ ] | `internal/middleware/body_limit.go` | 8 | 57 | 8 | 0 |
| 802 | [ ] | `internal/middleware/bridge_ingress_guard.go` | 1 | 2 | 1 | 0 |
| 803 | [ ] | `internal/middleware/brute_force.go` | 12 | 36 | 12 | 0 |
| 804 | [ ] | `internal/middleware/context.go` | 1 | 1 | 1 | 0 |
| 805 | [ ] | `internal/middleware/data_scope.go` | 6 | 43 | 6 | 0 |
| 806 | [ ] | `internal/middleware/init_guard.go` | 4 | 26 | 4 | 0 |
| 807 | [ ] | `internal/middleware/install_status.go` | 10 | 18 | 10 | 0 |
| 808 | [ ] | `internal/middleware/jwt.go` | 7 | 12 | 7 | 0 |
| 809 | [ ] | `internal/middleware/lang_resolver.go` | 2 | 17 | 2 | 0 |
| 810 | [ ] | `internal/middleware/locale.go` | 1 | 5 | 1 | 0 |
| 811 | [ ] | `internal/middleware/ltc_gate.go` | 6 | 39 | 6 | 0 |
| 812 | [ ] | `internal/middleware/mcp_guard.go` | 17 | 83 | 17 | 0 |
| 813 | [ ] | `internal/middleware/metrics.go` | 4 | 14 | 4 | 0 |
| 814 | [ ] | `internal/middleware/mfa.go` | 7 | 18 | 7 | 0 |
| 815 | [ ] | `internal/middleware/module_gate.go` | 15 | 45 | 15 | 0 |
| 816 | [ ] | `internal/middleware/module_route_patterns.go` | 4 | 23 | 4 | 0 |
| 817 | [ ] | `internal/middleware/module_route_supplement.go` | 4 | 31 | 4 | 0 |
| 818 | [ ] | `internal/middleware/ownership.go` | 13 | 52 | 13 | 0 |
| 819 | [ ] | `internal/middleware/permission.go` | 8 | 8 | 8 | 0 |
| 820 | [ ] | `internal/middleware/ports.go` | 9 | 12 | 9 | 0 |
| 821 | [ ] | `internal/middleware/public_tenant.go` | 7 | 21 | 7 | 0 |
| 822 | [ ] | `internal/middleware/rate_quota.go` | 17 | 66 | 17 | 0 |
| 823 | [ ] | `internal/middleware/ratelimit.go` | 8 | 14 | 8 | 0 |
| 824 | [ ] | `internal/middleware/require_admin.go` | 1 | 12 | 1 | 0 |
| 825 | [ ] | `internal/middleware/route_capability.go` | 9 | 25 | 9 | 0 |
| 826 | [ ] | `internal/middleware/route_policy.go` | 19 | 69 | 19 | 0 |
| 827 | [ ] | `internal/middleware/sanitize.go` | 12 | 20 | 12 | 0 |
| 828 | [ ] | `internal/middleware/subscription_guard.go` | 21 | 52 | 21 | 0 |
| 829 | [ ] | `internal/middleware/tenant.go` | 3 | 12 | 3 | 0 |
| 830 | [ ] | `internal/middleware/trace.go` | 5 | 24 | 5 | 0 |
| 831 | [ ] | `internal/middleware/visitor_rate_limit.go` | 4 | 8 | 4 | 0 |
| 832 | [ ] | `internal/middleware/webhook_signature.go` | 39 | 95 | 39 | 0 |
| 833 | [ ] | `internal/middleware/webhook_tenant.go` | 5 | 15 | 5 | 0 |
| 834 | [ ] | `internal/migration/migrations/a_domain_p1_migration.go` | 7 | 7 | 7 | 0 |
| 835 | [ ] | `internal/migration/migrations/aggregation_watermark_pk_migration.go` | 8 | 12 | 8 | 0 |
| 836 | [ ] | `internal/migration/migrations/ai_agent_asset_binding_migration.go` | 7 | 10 | 7 | 0 |
| 837 | [ ] | `internal/migration/migrations/ai_agent_kb_binding_migration.go` | 7 | 7 | 7 | 0 |
| 838 | [ ] | `internal/migration/migrations/ai_agent_logic_migration.go` | 7 | 7 | 7 | 0 |
| 839 | [ ] | `internal/migration/migrations/ai_agent_schema_migration.go` | 8 | 11 | 8 | 0 |
| 840 | [ ] | `internal/migration/migrations/ai_perf_faq_sop_layer_migration.go` | 7 | 8 | 7 | 0 |
| 841 | [ ] | `internal/migration/migrations/alert_rule_migration.go` | 7 | 13 | 7 | 0 |
| 842 | [ ] | `internal/migration/migrations/amount_money_migration.go` | 7 | 10 | 7 | 0 |
| 843 | [ ] | `internal/migration/migrations/asset_bundle_migration.go` | 7 | 7 | 7 | 0 |
| 844 | [ ] | `internal/migration/migrations/auth_security_migration.go` | 7 | 8 | 7 | 0 |
| 845 | [ ] | `internal/migration/migrations/bridge_account_migration.go` | 1 | 1 | 1 | 0 |
| 846 | [ ] | `internal/migration/migrations/bridge_channel_normalize_migration.go` | 1 | 2 | 1 | 0 |
| 847 | [ ] | `internal/migration/migrations/compliance_d_e_migration.go` | 7 | 7 | 7 | 0 |
| 848 | [ ] | `internal/migration/migrations/confidence_migration.go` | 7 | 12 | 7 | 0 |
| 849 | [ ] | `internal/migration/migrations/customer_session_updated_at_migration.go` | 7 | 7 | 7 | 0 |
| 850 | [ ] | `internal/migration/migrations/down_drop_policy.go` | 1 | 13 | 1 | 0 |
| 851 | [ ] | `internal/migration/migrations/feedback_loop_migration.go` | 7 | 12 | 7 | 0 |
| 852 | [ ] | `internal/migration/migrations/h_p1_migration.go` | 7 | 7 | 7 | 0 |
| 853 | [ ] | `internal/migration/migrations/humanize_evaluator_migration.go` | 7 | 11 | 7 | 0 |
| 854 | [ ] | `internal/migration/migrations/initial_schema.go` | 20 | 22 | 20 | 0 |
| 855 | [ ] | `internal/migration/migrations/knowledge_search_log_product_optional_migration.go` | 7 | 7 | 7 | 0 |
| 856 | [ ] | `internal/migration/migrations/knowledge_vector_migration.go` | 7 | 19 | 7 | 0 |
| 857 | [ ] | `internal/migration/migrations/knowledge_weight_migration.go` | 7 | 7 | 7 | 0 |
| 858 | [ ] | `internal/migration/migrations/l_p1_migration.go` | 8 | 9 | 8 | 0 |
| 859 | [ ] | `internal/migration/migrations/llm_routing_logs_extend_migration.go` | 7 | 7 | 7 | 0 |
| 860 | [ ] | `internal/migration/migrations/llm_routing_logs_migration.go` | 7 | 7 | 7 | 0 |
| 861 | [ ] | `internal/migration/migrations/llm_separate_pricing_migration.go` | 9 | 34 | 9 | 0 |
| 862 | [ ] | `internal/migration/migrations/llm_usage_records_migration.go` | 8 | 10 | 8 | 0 |
| 863 | [ ] | `internal/migration/migrations/m_p1_migration.go` | 7 | 7 | 7 | 0 |
| 864 | [ ] | `internal/migration/migrations/merchant_id_nullable_migration.go` | 7 | 11 | 7 | 0 |
| 865 | [ ] | `internal/migration/migrations/multilingual_i18n_migration.go` | 7 | 7 | 7 | 0 |
| 866 | [ ] | `internal/migration/migrations/multilingual_i18n_p13_migration.go` | 7 | 7 | 7 | 0 |
| 867 | [ ] | `internal/migration/migrations/rag_hybrid_migration.go` | 7 | 12 | 7 | 0 |
| 868 | [ ] | `internal/migration/migrations/rag_monitoring_migration.go` | 7 | 10 | 7 | 0 |
| 869 | [ ] | `internal/migration/migrations/session_id_length_migration.go` | 7 | 8 | 7 | 0 |
| 870 | [ ] | `internal/migration/migrations/shortlink_columns_migration.go` | 7 | 7 | 7 | 0 |
| 871 | [ ] | `internal/migration/migrations/sop_executor_migration.go` | 7 | 7 | 7 | 0 |
| 872 | [ ] | `internal/migration/migrations/sop_heatmap_index_migration.go` | 7 | 10 | 7 | 0 |
| 873 | [ ] | `internal/migration/migrations/telegram_polling_lock_migration.go` | 7 | 17 | 7 | 0 |
| 874 | [ ] | `internal/migration/migrations/tenant_column_batch_migration.go` | 24 | 35 | 24 | 0 |
| 875 | [ ] | `internal/migration/migrations/tenant_global_email_unique_migration.go` | 8 | 24 | 8 | 0 |
| 876 | [ ] | `internal/migration/migrations/tenant_table_migration.go` | 7 | 10 | 7 | 0 |
| 877 | [ ] | `internal/migration/migrations/tenant_unique_index_migration.go` | 7 | 15 | 7 | 0 |
| 878 | [ ] | `internal/migration/migrations/unmultitenant_migration.go` | 7 | 8 | 7 | 0 |
| 879 | [ ] | `internal/migration/migrations/user_blacklist_migration.go` | 7 | 11 | 7 | 0 |
| 880 | [ ] | `internal/migration/migrations/v3_15_kb_unification_migration.go` | 7 | 7 | 7 | 0 |
| 881 | [ ] | `internal/migration/migrations/v3_16_drop_rag_safety_audit_migration.go` | 7 | 7 | 7 | 0 |
| 882 | [ ] | `internal/migration/migrations/v3_17_drop_rag_alerts_migration.go` | 7 | 7 | 7 | 0 |
| 883 | [ ] | `internal/migration/migrations/v3_18_1_legacy_web_purge_migration.go` | 0 | 0 | 0 | 0 |
| 884 | [ ] | `internal/migration/migrations/v3_18_channel_unify_migration.go` | 0 | 0 | 0 | 0 |
| 885 | [ ] | `internal/migration/migrations/v3_19_drop_cdp_auto_reply_migration.go` | 2 | 10 | 2 | 0 |
| 886 | [ ] | `internal/migration/migrations/v3_20_live_code_click_logs_migration.go` | 2 | 7 | 2 | 0 |
| 887 | [ ] | `internal/migration/migrations/v3_21_bridge_metrics_migration.go` | 2 | 7 | 2 | 0 |
| 888 | [ ] | `internal/migration/migrations/v3_22_0_customer_id_standardize_migration.go` | 2 | 5 | 2 | 0 |
| 889 | [ ] | `internal/migration/migrations/v3_22_1_soft_delete_migration.go` | 0 | 0 | 0 | 0 |
| 890 | [ ] | `internal/migration/migrations/v3_22_2_enum_optimize_migration.go` | 0 | 0 | 0 | 0 |
| 891 | [ ] | `internal/migration/migrations/v3_22_3_intent_merge_migration.go` | 1 | 2 | 1 | 0 |
| 892 | [ ] | `internal/migration/migrations/v3_23_0_unified_id_widen_migration.go` | 0 | 0 | 0 | 0 |
| 893 | [ ] | `internal/migration/migrations/v3_25_0_customer_owner_agent_migration.go` | 7 | 10 | 7 | 0 |
| 894 | [ ] | `internal/migration/migrations/v3_26_0_reach_tables_migration.go` | 7 | 13 | 7 | 0 |
| 895 | [ ] | `internal/migration/migrations/v3_27_0_sop_timer_sink_columns_migration.go` | 6 | 6 | 6 | 0 |
| 896 | [ ] | `internal/migration/migrations/v3_28_0_email_smtp_password_encrypt_migration.go` | 0 | 0 | 0 | 0 |
| 897 | [ ] | `internal/migration/migrations/v3_29_0_sop_executed_nodes_migration.go` | 1 | 10 | 1 | 0 |
| 898 | [ ] | `internal/migration/migrations/v3_30_0_bandit_reflux_log_migration.go` | 1 | 8 | 1 | 0 |
| 899 | [ ] | `internal/migration/migrations/v3_31_0_embedding_source_migration.go` | 0 | 0 | 0 | 0 |
| 900 | [ ] | `internal/migration/migrations/v3_32_0_handoff_outcome_migration.go` | 1 | 5 | 1 | 0 |
| 901 | [ ] | `internal/migration/migrations/v3_33_0_agent_checkpoint_migration.go` | 1 | 5 | 1 | 0 |
| 902 | [ ] | `internal/migration/migrations/v3_34_0_churn_score_migration.go` | 1 | 5 | 1 | 0 |
| 903 | [ ] | `internal/migration/migrations/v3_35_0_telegram_group_gate_migration.go` | 1 | 1 | 1 | 0 |
| 904 | [ ] | `internal/migration/migrations/v3_36_0_admin_password_guard_migration.go` | 3 | 14 | 3 | 0 |
| 905 | [ ] | `internal/migration/migrations/v3_37_0_browser_automation_migration.go` | 6 | 6 | 6 | 0 |
| 906 | [ ] | `internal/migration/migrations/v3_38_0_browser_command_log_migration.go` | 1 | 2 | 1 | 0 |
| 907 | [ ] | `internal/migration/migrations/v3_39_0_browser_task_platform_migration.go` | 1 | 2 | 1 | 0 |
| 908 | [ ] | `internal/migration/migrations/v3_40_0_browser_audit_retry_tz_migration.go` | 1 | 7 | 1 | 0 |
| 909 | [ ] | `internal/migration/migrations/v3_41_0_operation_log_encrypt_fields_migration.go` | 7 | 18 | 7 | 0 |
| 910 | [ ] | `internal/migration/migrations/v3_42_0_browser_require_confirm_migration.go` | 1 | 3 | 1 | 0 |
| 911 | [ ] | `internal/migration/migrations/v3_43_0_browser_write_ledger_confirm_budget_migration.go` | 1 | 11 | 1 | 0 |
| 912 | [ ] | `internal/migration/migrations/v3_44_0_browser_command_log_ok_tristate_migration.go` | 4 | 19 | 4 | 0 |
| 913 | [ ] | `internal/migration/migrations/v3_45_0_message_hub_outbound_push_budget_migration.go` | 4 | 19 | 4 | 0 |
| 914 | [ ] | `internal/migration/migrations/v3_46_0_browser_audit_prune_cutoff_source_migration.go` | 3 | 14 | 3 | 0 |
| 915 | [ ] | `internal/migration/migrations/v3_47_0_config_param_group_key_unique_migration.go` | 4 | 20 | 4 | 0 |
| 916 | [ ] | `internal/migration/migrations/v3_48_0_browser_task_copy_text_migration.go` | 3 | 11 | 3 | 0 |
| 917 | [ ] | `internal/migration/migrations/v3_49_0_browser_profile_health_migration.go` | 2 | 9 | 2 | 0 |
| 918 | [ ] | `internal/migration/migrations/v3_50_0_browser_outreach_dedupe_migration.go` | 2 | 10 | 2 | 0 |
| 919 | [ ] | `internal/migration/migrations/v3_51_0_browser_task_campaign_budget_migration.go` | 3 | 16 | 3 | 0 |
| 920 | [ ] | `internal/migration/migrations/v3_52_0_browser_outreach_receipt_migration.go` | 2 | 12 | 2 | 0 |
| 921 | [ ] | `internal/migration/migrations/v3_53_0_failover_policy_to_config_param_migration.go` | 6 | 30 | 6 | 0 |
| 922 | [ ] | `internal/migration/migrations/v3_54_0_embedding_global_to_config_param_migration.go` | 6 | 23 | 6 | 0 |
| 923 | [ ] | `internal/migration/migrations/v3_55_0_menu_grant_ceiling_prune_migration.go` | 10 | 42 | 10 | 0 |
| 924 | [ ] | `internal/migration/migrations/v3_56_0_clue_geo_capture_type_split_migration.go` | 9 | 43 | 9 | 0 |
| 925 | [ ] | `internal/migration/migrations/v3_63_0_tenant_init_compliance_columns_migration.go` | 4 | 14 | 4 | 0 |
| 926 | [ ] | `internal/migration/migrations/v3_64_0_tenant_subscription_mirror_migration.go` | 3 | 14 | 3 | 0 |
| 927 | [ ] | `internal/migration/migrations/v3_65_0_platform_kv_tenant_split_migration.go` | 15 | 37 | 15 | 0 |
| 928 | [ ] | `internal/migration/migrations/v3_66_0_tenant_site_config_and_feature_flags_migration.go` | 12 | 28 | 12 | 0 |
| 929 | [ ] | `internal/migration/migrations/wecom_webhook_fields_migration.go` | 7 | 8 | 7 | 0 |
| 930 | [ ] | `internal/migration/migrations/workflow_visual_orchestrator_migration.go` | 7 | 8 | 7 | 0 |
| 931 | [ ] | `internal/migration/registry.go` | 8 | 8 | 8 | 0 |
| 932 | [ ] | `internal/migration/service.go` | 15 | 22 | 15 | 0 |
| 933 | [ ] | `internal/model/account.go` | 0 | 0 | 0 | 0 |
| 934 | [ ] | `internal/model/aftersale.go` | 1 | 4 | 1 | 0 |
| 935 | [ ] | `internal/model/agent_kb_binding.go` | 3 | 26 | 3 | 0 |
| 936 | [ ] | `internal/model/agent_status.go` | 2 | 2 | 2 | 0 |
| 937 | [ ] | `internal/model/ai_agent.go` | 12 | 31 | 12 | 0 |
| 938 | [ ] | `internal/model/ai_sales_champion.go` | 26 | 61 | 26 | 0 |
| 939 | [ ] | `internal/model/ai_suggestion.go` | 2 | 2 | 2 | 0 |
| 940 | [ ] | `internal/model/ai_tool_config.go` | 8 | 8 | 8 | 0 |
| 941 | [ ] | `internal/model/alert_rule.go` | 11 | 23 | 11 | 0 |
| 942 | [ ] | `internal/model/approval_request.go` | 21 | 82 | 21 | 0 |
| 943 | [ ] | `internal/model/asset_bundle.go` | 12 | 35 | 12 | 0 |
| 944 | [ ] | `internal/model/backup.go` | 6 | 6 | 6 | 0 |
| 945 | [ ] | `internal/model/bad_case.go` | 32 | 66 | 32 | 0 |
| 946 | [ ] | `internal/model/bandit_reflux_log.go` | 2 | 9 | 2 | 0 |
| 947 | [ ] | `internal/model/bill.go` | 21 | 139 | 21 | 0 |
| 948 | [ ] | `internal/model/bridge_account.go` | 1 | 4 | 1 | 0 |
| 949 | [ ] | `internal/model/bridge_protocol.go` | 0 | 0 | 0 | 0 |
| 950 | [ ] | `internal/model/chat_channel.go` | 3 | 9 | 3 | 0 |
| 951 | [ ] | `internal/model/churn_score.go` | 1 | 4 | 1 | 0 |
| 952 | [ ] | `internal/model/clue.go` | 1 | 2 | 1 | 0 |
| 953 | [ ] | `internal/model/clue_score.go` | 7 | 18 | 7 | 0 |
| 954 | [ ] | `internal/model/community.go` | 6 | 6 | 6 | 0 |
| 955 | [ ] | `internal/model/confidence.go` | 12 | 16 | 12 | 0 |
| 956 | [ ] | `internal/model/config_param.go` | 6 | 37 | 6 | 0 |
| 957 | [ ] | `internal/model/config_param_group.go` | 2 | 14 | 2 | 0 |
| 958 | [ ] | `internal/model/config_param_tenant.go` | 3 | 16 | 3 | 0 |
| 959 | [ ] | `internal/model/cron_job_lease.go` | 1 | 6 | 1 | 0 |
| 960 | [ ] | `internal/model/csat.go` | 5 | 6 | 5 | 0 |
| 961 | [ ] | `internal/model/customer.go` | 9 | 17 | 9 | 0 |
| 962 | [ ] | `internal/model/customer_channel.go` | 3 | 10 | 3 | 0 |
| 963 | [ ] | `internal/model/customer_do_not_contact.go` | 13 | 23 | 13 | 0 |
| 964 | [ ] | `internal/model/customer_event.go` | 7 | 7 | 7 | 0 |
| 965 | [ ] | `internal/model/customer_long_term_memory.go` | 3 | 4 | 3 | 0 |
| 966 | [ ] | `internal/model/customer_rfm.go` | 9 | 26 | 9 | 0 |
| 967 | [ ] | `internal/model/customer_segment.go` | 0 | 0 | 0 | 0 |
| 968 | [ ] | `internal/model/customer_session.go` | 5 | 5 | 5 | 0 |
| 969 | [ ] | `internal/model/customer_tag.go` | 7 | 7 | 7 | 0 |
| 970 | [ ] | `internal/model/customer_tag_assignment.go` | 3 | 3 | 3 | 0 |
| 971 | [ ] | `internal/model/data_scope.go` | 18 | 57 | 18 | 0 |
| 972 | [ ] | `internal/model/dialogue_eval_run.go` | 9 | 15 | 9 | 0 |
| 973 | [ ] | `internal/model/dictionary.go` | 3 | 25 | 3 | 0 |
| 974 | [ ] | `internal/model/dingtalk_app_account.go` | 1 | 5 | 1 | 0 |
| 975 | [ ] | `internal/model/domain_pool.go` | 6 | 16 | 6 | 0 |
| 976 | [ ] | `internal/model/douyin_card.go` | 2 | 2 | 2 | 0 |
| 977 | [ ] | `internal/model/douyin_card_activity.go` | 2 | 2 | 2 | 0 |
| 978 | [ ] | `internal/model/email_draft.go` | 2 | 2 | 2 | 0 |
| 979 | [ ] | `internal/model/email_jobs.go` | 2 | 2 | 2 | 0 |
| 980 | [ ] | `internal/model/email_list.go` | 2 | 2 | 2 | 0 |
| 981 | [ ] | `internal/model/email_send.go` | 2 | 12 | 2 | 0 |
| 982 | [ ] | `internal/model/email_smtp.go` | 0 | 0 | 0 | 0 |
| 983 | [ ] | `internal/model/email_tracking.go` | 6 | 12 | 6 | 0 |
| 984 | [ ] | `internal/model/email_unsubscribe.go` | 3 | 9 | 3 | 0 |
| 985 | [ ] | `internal/model/faq_entry.go` | 3 | 34 | 3 | 0 |
| 986 | [ ] | `internal/model/feature_flag.go` | 3 | 11 | 3 | 0 |
| 987 | [ ] | `internal/model/feedback_learning.go` | 11 | 14 | 11 | 0 |
| 988 | [ ] | `internal/model/feedback_loop.go` | 21 | 41 | 21 | 0 |
| 989 | [ ] | `internal/model/feedback_record.go` | 2 | 2 | 2 | 0 |
| 990 | [ ] | `internal/model/feishu.go` | 6 | 9 | 6 | 0 |
| 991 | [ ] | `internal/model/glossary.go` | 5 | 27 | 5 | 0 |
| 992 | [ ] | `internal/model/human_task.go` | 38 | 160 | 38 | 0 |
| 993 | [ ] | `internal/model/humanize_score.go` | 13 | 28 | 13 | 0 |
| 994 | [ ] | `internal/model/integration.go` | 23 | 69 | 23 | 0 |
| 995 | [ ] | `internal/model/integration_template.go` | 6 | 12 | 6 | 0 |
| 996 | [ ] | `internal/model/intent_example.go` | 1 | 1 | 1 | 0 |
| 997 | [ ] | `internal/model/intent_log.go` | 3 | 8 | 3 | 0 |
| 998 | [ ] | `internal/model/kb_connector.go` | 7 | 14 | 7 | 0 |
| 999 | [ ] | `internal/model/kb_document.go` | 3 | 3 | 3 | 0 |
| 1000 | [ ] | `internal/model/kb_merchant.go` | 6 | 7 | 6 | 0 |
| 1001 | [ ] | `internal/model/kb_release.go` | 37 | 112 | 37 | 0 |
| 1002 | [ ] | `internal/model/kb_workspace.go` | 15 | 27 | 15 | 0 |
| 1003 | [ ] | `internal/model/knowledge_base.go` | 5 | 69 | 5 | 0 |
| 1004 | [ ] | `internal/model/kuaishou_card.go` | 1 | 1 | 1 | 0 |
| 1005 | [ ] | `internal/model/kuaishou_card_activity.go` | 2 | 2 | 2 | 0 |
| 1006 | [ ] | `internal/model/layer_decision_log.go` | 2 | 23 | 2 | 0 |
| 1007 | [ ] | `internal/model/lead_mining_config.go` | 2 | 2 | 2 | 0 |
| 1008 | [ ] | `internal/model/learning_insight.go` | 2 | 8 | 2 | 0 |
| 1009 | [ ] | `internal/model/live_code.go` | 3 | 3 | 3 | 0 |
| 1010 | [ ] | `internal/model/live_code_click_log.go` | 4 | 6 | 4 | 0 |
| 1011 | [ ] | `internal/model/live_code_qr.go` | 3 | 3 | 3 | 0 |
| 1012 | [ ] | `internal/model/live_code_qr_stat.go` | 2 | 2 | 2 | 0 |
| 1013 | [ ] | `internal/model/llm_provider.go` | 5 | 18 | 5 | 0 |
| 1014 | [ ] | `internal/model/llm_routing_log.go` | 3 | 25 | 3 | 0 |
| 1015 | [ ] | `internal/model/llm_routing_rule.go` | 2 | 13 | 2 | 0 |
| 1016 | [ ] | `internal/model/local_asset.go` | 4 | 4 | 4 | 0 |
| 1017 | [ ] | `internal/model/login_event.go` | 4 | 13 | 4 | 0 |
| 1018 | [ ] | `internal/model/low_quality_sample.go` | 2 | 2 | 2 | 0 |
| 1019 | [ ] | `internal/model/mcp_credential.go` | 1 | 5 | 1 | 0 |
| 1020 | [ ] | `internal/model/memory.go` | 4 | 4 | 4 | 0 |
| 1021 | [ ] | `internal/model/menu.go` | 14 | 66 | 14 | 0 |
| 1022 | [ ] | `internal/model/menu_capability.go` | 7 | 30 | 7 | 0 |
| 1023 | [ ] | `internal/model/menu_capability_routes.go` | 3 | 26 | 3 | 0 |
| 1024 | [ ] | `internal/model/message.go` | 0 | 0 | 0 | 0 |
| 1025 | [ ] | `internal/model/message_event.go` | 3 | 9 | 3 | 0 |
| 1026 | [ ] | `internal/model/message_trace.go` | 3 | 33 | 3 | 0 |
| 1027 | [ ] | `internal/model/module_bindings.go` | 60 | 118 | 60 | 0 |
| 1028 | [ ] | `internal/model/module_tables.go` | 34 | 55 | 34 | 0 |
| 1029 | [ ] | `internal/model/msg_hourly_summary.go` | 2 | 6 | 2 | 0 |
| 1030 | [ ] | `internal/model/notification.go` | 3 | 16 | 3 | 0 |
| 1031 | [ ] | `internal/model/obs_config.go` | 1 | 1 | 1 | 0 |
| 1032 | [ ] | `internal/model/operation_log.go` | 2 | 2 | 2 | 0 |
| 1033 | [ ] | `internal/model/opportunity.go` | 26 | 168 | 26 | 0 |
| 1034 | [ ] | `internal/model/order.go` | 0 | 0 | 0 | 0 |
| 1035 | [ ] | `internal/model/order_draft.go` | 6 | 24 | 6 | 0 |
| 1036 | [ ] | `internal/model/password_history.go` | 4 | 11 | 4 | 0 |
| 1037 | [ ] | `internal/model/password_reset_token.go` | 6 | 14 | 6 | 0 |
| 1038 | [ ] | `internal/model/payment.go` | 25 | 105 | 25 | 0 |
| 1039 | [ ] | `internal/model/platform_account_config.go` | 3 | 3 | 3 | 0 |
| 1040 | [ ] | `internal/model/platform_config_kv.go` | 2 | 17 | 2 | 0 |
| 1041 | [ ] | `internal/model/quick_reply.go` | 2 | 2 | 2 | 0 |
| 1042 | [ ] | `internal/model/quick_reply_folder.go` | 1 | 1 | 1 | 0 |
| 1043 | [ ] | `internal/model/quote.go` | 25 | 130 | 25 | 0 |
| 1044 | [ ] | `internal/model/rag_metrics.go` | 4 | 17 | 4 | 0 |
| 1045 | [ ] | `internal/model/rag_product.go` | 8 | 20 | 8 | 0 |
| 1046 | [ ] | `internal/model/rag_recall_monitor_snapshot.go` | 2 | 9 | 2 | 0 |
| 1047 | [ ] | `internal/model/rag_session.go` | 0 | 0 | 0 | 0 |
| 1048 | [ ] | `internal/model/reach_compliance_log.go` | 3 | 3 | 3 | 0 |
| 1049 | [ ] | `internal/model/reach_delayed_outbound.go` | 9 | 11 | 9 | 0 |
| 1050 | [ ] | `internal/model/rfm_rule.go` | 6 | 6 | 6 | 0 |
| 1051 | [ ] | `internal/model/rich_card.go` | 5 | 7 | 5 | 0 |
| 1052 | [ ] | `internal/model/role.go` | 6 | 25 | 6 | 0 |
| 1053 | [ ] | `internal/model/sales_event.go` | 7 | 54 | 7 | 0 |
| 1054 | [ ] | `internal/model/security_alert.go` | 5 | 13 | 5 | 0 |
| 1055 | [ ] | `internal/model/security_audit.go` | 4 | 5 | 4 | 0 |
| 1056 | [ ] | `internal/model/security_rules.go` | 6 | 7 | 6 | 0 |
| 1057 | [ ] | `internal/model/session_message.go` | 2 | 2 | 2 | 0 |
| 1058 | [ ] | `internal/model/session_tag.go` | 2 | 2 | 2 | 0 |
| 1059 | [ ] | `internal/model/short_link.go` | 2 | 2 | 2 | 0 |
| 1060 | [ ] | `internal/model/short_link_access.go` | 2 | 2 | 2 | 0 |
| 1061 | [ ] | `internal/model/sla.go` | 1 | 1 | 1 | 0 |
| 1062 | [ ] | `internal/model/sm_list.go` | 0 | 0 | 0 | 0 |
| 1063 | [ ] | `internal/model/sms.go` | 8 | 8 | 8 | 0 |
| 1064 | [ ] | `internal/model/sms_number_portability.go` | 3 | 11 | 3 | 0 |
| 1065 | [ ] | `internal/model/sms_tracking.go` | 11 | 27 | 11 | 0 |
| 1066 | [ ] | `internal/model/sms_unsubscribe.go` | 3 | 10 | 3 | 0 |
| 1067 | [ ] | `internal/model/sop_executor.go` | 5 | 5 | 5 | 0 |
| 1068 | [ ] | `internal/model/sop_template.go` | 2 | 20 | 2 | 0 |
| 1069 | [ ] | `internal/model/sso_identity.go` | 1 | 1 | 1 | 0 |
| 1070 | [ ] | `internal/model/stats.go` | 4 | 4 | 4 | 0 |
| 1071 | [ ] | `internal/model/support_metrics.go` | 4 | 5 | 4 | 0 |
| 1072 | [ ] | `internal/model/system_config.go` | 1 | 5 | 1 | 0 |
| 1073 | [ ] | `internal/model/system_config_kv.go` | 2 | 10 | 2 | 0 |
| 1074 | [ ] | `internal/model/system_user.go` | 11 | 24 | 11 | 0 |
| 1075 | [ ] | `internal/model/telegram_gate.go` | 24 | 33 | 24 | 0 |
| 1076 | [ ] | `internal/model/tenant.go` | 17 | 35 | 17 | 0 |
| 1077 | [ ] | `internal/model/tenant_module.go` | 27 | 87 | 27 | 0 |
| 1078 | [ ] | `internal/model/tenant_subscription_mirror.go` | 12 | 32 | 12 | 0 |
| 1079 | [ ] | `internal/model/tiktok_card.go` | 4 | 4 | 4 | 0 |
| 1080 | [ ] | `internal/model/tool_audit.go` | 3 | 14 | 3 | 0 |
| 1081 | [ ] | `internal/model/trace_eval.go` | 2 | 10 | 2 | 0 |
| 1082 | [ ] | `internal/model/unified_message.go` | 18 | 18 | 18 | 0 |
| 1083 | [ ] | `internal/model/upgrade.go` | 7 | 7 | 7 | 0 |
| 1084 | [ ] | `internal/model/user.go` | 2 | 2 | 2 | 0 |
| 1085 | [ ] | `internal/model/user_blacklist.go` | 2 | 10 | 2 | 0 |
| 1086 | [ ] | `internal/model/user_mfa.go` | 4 | 12 | 4 | 0 |
| 1087 | [ ] | `internal/model/web_vital.go` | 6 | 15 | 6 | 0 |
| 1088 | [ ] | `internal/model/wechat_account.go` | 8 | 8 | 8 | 0 |
| 1089 | [ ] | `internal/model/wechat_message.go` | 1 | 1 | 1 | 0 |
| 1090 | [ ] | `internal/model/wecom.go` | 12 | 12 | 12 | 0 |
| 1091 | [ ] | `internal/model/whatsapp_account.go` | 0 | 0 | 0 | 0 |
| 1092 | [ ] | `internal/model/whatsapp_draft.go` | 0 | 0 | 0 | 0 |
| 1093 | [ ] | `internal/model/whatsapp_group_message.go` | 3 | 3 | 3 | 0 |
| 1094 | [ ] | `internal/model/whatsapp_job.go` | 0 | 0 | 0 | 0 |
| 1095 | [ ] | `internal/model/whatsapp_message_queue.go` | 4 | 4 | 4 | 0 |
| 1096 | [ ] | `internal/model/whatsapp_message_template.go` | 3 | 3 | 3 | 0 |
| 1097 | [ ] | `internal/model/whatsapp_queue.go` | 0 | 0 | 0 | 0 |
| 1098 | [ ] | `internal/model/whatsapp_session.go` | 0 | 0 | 0 | 0 |
| 1099 | [ ] | `internal/model/workflow_orchestrator.go` | 8 | 21 | 8 | 0 |
| 1100 | [ ] | `internal/model/xianyu_card.go` | 2 | 2 | 2 | 0 |
| 1101 | [ ] | `internal/model/xianyu_card_activity.go` | 2 | 2 | 2 | 0 |
| 1102 | [ ] | `internal/model/xiaohongshu_card.go` | 2 | 2 | 2 | 0 |
| 1103 | [ ] | `internal/model/xiaohongshu_card_activity.go` | 2 | 2 | 2 | 0 |
| 1104 | [ ] | `internal/monitor/handler.go` | 1 | 12 | 1 | 0 |
| 1105 | [ ] | `internal/monitor/monitor.go` | 20 | 38 | 20 | 0 |
| 1106 | [ ] | `internal/ops/controller/ab_experiment.go` | 19 | 20 | 19 | 0 |
| 1107 | [ ] | `internal/ops/controller/ai_productivity.go` | 5 | 5 | 5 | 0 |
| 1108 | [ ] | `internal/ops/controller/churn_prediction.go` | 13 | 18 | 13 | 0 |
| 1109 | [ ] | `internal/ops/controller/conversion_funnel.go` | 5 | 5 | 5 | 0 |
| 1110 | [ ] | `internal/ops/controller/custom_report.go` | 10 | 10 | 10 | 0 |
| 1111 | [ ] | `internal/ops/controller/dashboard_screen.go` | 9 | 14 | 9 | 0 |
| 1112 | [ ] | `internal/ops/controller/ltc_rates.go` | 3 | 6 | 3 | 0 |
| 1113 | [ ] | `internal/ops/controller/performance_testing.go` | 6 | 6 | 6 | 0 |
| 1114 | [ ] | `internal/ops/model/ab_experiment.go` | 8 | 8 | 8 | 0 |
| 1115 | [ ] | `internal/ops/model/churn_prediction.go` | 8 | 8 | 8 | 0 |
| 1116 | [ ] | `internal/ops/model/custom_report.go` | 10 | 10 | 10 | 0 |
| 1117 | [ ] | `internal/ops/model/dashboard_screen.go` | 4 | 4 | 4 | 0 |
| 1118 | [ ] | `internal/ops/model/ltc_rates.go` | 8 | 19 | 8 | 0 |
| 1119 | [ ] | `internal/ops/model/performance_test_result.go` | 1 | 1 | 1 | 0 |
| 1120 | [ ] | `internal/ops/repository/ab_experiment.go` | 29 | 29 | 29 | 0 |
| 1121 | [ ] | `internal/ops/repository/ai_productivity.go` | 12 | 15 | 12 | 0 |
| 1122 | [ ] | `internal/ops/repository/churn_prediction.go` | 30 | 30 | 30 | 0 |
| 1123 | [ ] | `internal/ops/repository/conversion_funnel.go` | 18 | 46 | 18 | 0 |
| 1124 | [ ] | `internal/ops/repository/custom_report.go` | 12 | 12 | 12 | 0 |
| 1125 | [ ] | `internal/ops/repository/dashboard_screen.go` | 29 | 31 | 29 | 0 |
| 1126 | [ ] | `internal/ops/repository/ltc_rates.go` | 7 | 13 | 7 | 0 |
| 1127 | [ ] | `internal/ops/repository/performance_testing.go` | 6 | 6 | 6 | 0 |
| 1128 | [ ] | `internal/ops/repository/stats.go` | 3 | 4 | 3 | 0 |
| 1129 | [ ] | `internal/ops/service/ab_experiment.go` | 24 | 25 | 24 | 0 |
| 1130 | [ ] | `internal/ops/service/ab_stats.go` | 12 | 22 | 12 | 0 |
| 1131 | [ ] | `internal/ops/service/ai_productivity.go` | 7 | 7 | 7 | 0 |
| 1132 | [ ] | `internal/ops/service/churn_prediction.go` | 25 | 26 | 25 | 0 |
| 1133 | [ ] | `internal/ops/service/conversion_funnel.go` | 11 | 30 | 11 | 0 |
| 1134 | [ ] | `internal/ops/service/custom_report.go` | 17 | 17 | 17 | 0 |
| 1135 | [ ] | `internal/ops/service/custom_report_export.go` | 6 | 7 | 6 | 0 |
| 1136 | [ ] | `internal/ops/service/dashboard_screen.go` | 22 | 26 | 22 | 0 |
| 1137 | [ ] | `internal/ops/service/ltc_rates.go` | 5 | 9 | 5 | 0 |
| 1138 | [ ] | `internal/ops/service/performance_testing.go` | 6 | 6 | 6 | 0 |
| 1139 | [ ] | `internal/ops/service/report_filter.go` | 1 | 4 | 1 | 0 |
| 1140 | [ ] | `internal/ops/service/web_vital.go` | 3 | 3 | 3 | 0 |
| 1141 | [ ] | `internal/pkg/apperr/apperr.go` | 16 | 19 | 16 | 0 |
| 1142 | [ ] | `internal/pkg/cron/cron.go` | 8 | 16 | 8 | 0 |
| 1143 | [ ] | `internal/pkg/cron/emaillistcron.go` | 17 | 53 | 17 | 0 |
| 1144 | [ ] | `internal/pkg/cron/snooze_recovery_cron.go` | 4 | 4 | 4 | 0 |
| 1145 | [ ] | `internal/pkg/cron/wecom_quota_reset_cron.go` | 5 | 5 | 5 | 0 |
| 1146 | [ ] | `internal/pkg/crypto/field.go` | 4 | 5 | 4 | 0 |
| 1147 | [ ] | `internal/pkg/db/db.go` | 6 | 29 | 6 | 0 |
| 1148 | [ ] | `internal/pkg/db/migrate.go` | 47 | 245 | 47 | 0 |
| 1149 | [ ] | `internal/pkg/db/tenant_id_normalize.go` | 3 | 33 | 3 | 0 |
| 1150 | [ ] | `internal/pkg/dbencrypt/dbencrypt.go` | 6 | 24 | 6 | 0 |
| 1151 | [ ] | `internal/pkg/dbstate/dbstate.go` | 3 | 8 | 3 | 0 |
| 1152 | [ ] | `internal/pkg/errhttp/errhttp.go` | 3 | 10 | 3 | 0 |
| 1153 | [ ] | `internal/pkg/featureflag/flag.go` | 21 | 68 | 21 | 0 |
| 1154 | [ ] | `internal/pkg/goldentest/goldentest.go` | 10 | 15 | 10 | 0 |
| 1155 | [ ] | `internal/pkg/httpclient/httpclient.go` | 4 | 7 | 4 | 0 |
| 1156 | [ ] | `internal/pkg/i18n/backend_phrases.go` | 2 | 5 | 2 | 0 |
| 1157 | [ ] | `internal/pkg/i18n/lang_ctx.go` | 10 | 18 | 10 | 0 |
| 1158 | [ ] | `internal/pkg/i18n/lang_detect.go` | 1 | 8 | 1 | 0 |
| 1159 | [ ] | `internal/pkg/i18n/locale.go` | 8 | 15 | 8 | 0 |
| 1160 | [ ] | `internal/pkg/i18n/messages.go` | 4 | 6 | 4 | 0 |
| 1161 | [ ] | `internal/pkg/kbrelease/kbrelease.go` | 21 | 113 | 21 | 0 |
| 1162 | [ ] | `internal/pkg/mail/attachment.go` | 11 | 49 | 11 | 0 |
| 1163 | [ ] | `internal/pkg/mail/mail.go` | 4 | 6 | 4 | 0 |
| 1164 | [ ] | `internal/pkg/mail/open_pixel.go` | 1 | 5 | 1 | 0 |
| 1165 | [ ] | `internal/pkg/mail/template.go` | 0 | 0 | 0 | 0 |
| 1166 | [ ] | `internal/pkg/mail/unsubscribe.go` | 7 | 31 | 7 | 0 |
| 1167 | [ ] | `internal/pkg/messageid/identity.go` | 3 | 5 | 3 | 0 |
| 1168 | [ ] | `internal/pkg/metrics/bridge.go` | 2 | 2 | 2 | 0 |
| 1169 | [ ] | `internal/pkg/metrics/bridge_sink.go` | 6 | 9 | 6 | 0 |
| 1170 | [ ] | `internal/pkg/metrics/metrics.go` | 49 | 79 | 49 | 0 |
| 1171 | [ ] | `internal/pkg/pagination/cursor.go` | 19 | 35 | 19 | 0 |
| 1172 | [ ] | `internal/pkg/pagination/extract.go` | 0 | 0 | 0 | 0 |
| 1173 | [ ] | `internal/pkg/pagination/reflect.go` | 1 | 1 | 1 | 0 |
| 1174 | [ ] | `internal/pkg/security/visitor_token.go` | 6 | 37 | 6 | 0 |
| 1175 | [ ] | `internal/pkg/shared/service/system_stats.go` | 5 | 6 | 5 | 0 |
| 1176 | [ ] | `internal/pkg/sla/default.go` | 3 | 6 | 3 | 0 |
| 1177 | [ ] | `internal/pkg/sla/slo.go` | 10 | 10 | 10 | 0 |
| 1178 | [ ] | `internal/pkg/sso/adapter.go` | 3 | 7 | 3 | 0 |
| 1179 | [ ] | `internal/pkg/sso/dingtalk.go` | 2 | 2 | 2 | 0 |
| 1180 | [ ] | `internal/pkg/sso/feishu.go` | 2 | 2 | 2 | 0 |
| 1181 | [ ] | `internal/pkg/sso/generic.go` | 2 | 2 | 2 | 0 |
| 1182 | [ ] | `internal/pkg/sso/oidc.go` | 21 | 32 | 21 | 0 |
| 1183 | [ ] | `internal/pkg/sso/util.go` | 3 | 3 | 3 | 0 |
| 1184 | [ ] | `internal/pkg/sso/wecom.go` | 2 | 2 | 2 | 0 |
| 1185 | [ ] | `internal/pkg/tenancyidx/tenancyidx.go` | 16 | 41 | 16 | 0 |
| 1186 | [ ] | `internal/pkg/tenancyidx/tenant_callback.go` | 41 | 70 | 41 | 0 |
| 1187 | [ ] | `internal/pkg/tenant/fanout.go` | 7 | 25 | 7 | 0 |
| 1188 | [ ] | `internal/pkg/tenant/keys.go` | 4 | 14 | 4 | 0 |
| 1189 | [ ] | `internal/pkg/tenant/lookup.go` | 3 | 18 | 3 | 0 |
| 1190 | [ ] | `internal/pkg/tenant/tenant.go` | 21 | 61 | 21 | 0 |
| 1191 | [ ] | `internal/pkg/testutil/testdb.go` | 26 | 151 | 26 | 0 |
| 1192 | [ ] | `internal/pkg/testutil/testmigrate/testmigrate.go` | 2 | 12 | 2 | 0 |
| 1193 | [ ] | `internal/pkg/textutil/truncate.go` | 4 | 5 | 4 | 0 |
| 1194 | [ ] | `internal/pkg/tgbot/tgbot.go` | 16 | 47 | 16 | 0 |
| 1195 | [ ] | `internal/pkg/timeutil/timeutil.go` | 10 | 48 | 10 | 0 |
| 1196 | [ ] | `internal/pkg/trace/trace.go` | 25 | 74 | 25 | 0 |
| 1197 | [ ] | `internal/pkg/traceparent/traceparent.go` | 8 | 40 | 8 | 0 |
| 1198 | [ ] | `internal/pkg/tracing/langfuse_attrs.go` | 3 | 10 | 3 | 0 |
| 1199 | [ ] | `internal/pkg/tracing/tracing.go` | 50 | 73 | 50 | 0 |
| 1200 | [ ] | `internal/pkg/utils/account/account.go` | 0 | 0 | 0 | 0 |
| 1201 | [ ] | `internal/pkg/utils/async/background.go` | 5 | 19 | 5 | 0 |
| 1202 | [ ] | `internal/pkg/utils/backoff.go` | 9 | 34 | 9 | 0 |
| 1203 | [ ] | `internal/pkg/utils/bcrypt/bcrypt.go` | 2 | 2 | 2 | 0 |
| 1204 | [ ] | `internal/pkg/utils/bm25/bm25.go` | 3 | 3 | 3 | 0 |
| 1205 | [ ] | `internal/pkg/utils/constants.go` | 19 | 22 | 19 | 0 |
| 1206 | [ ] | `internal/pkg/utils/device_parser.go` | 5 | 12 | 5 | 0 |
| 1207 | [ ] | `internal/pkg/utils/encryption.go` | 4 | 4 | 4 | 0 |
| 1208 | [ ] | `internal/pkg/utils/error_code.go` | 10 | 21 | 10 | 0 |
| 1209 | [ ] | `internal/pkg/utils/error_handler.go` | 10 | 10 | 10 | 0 |
| 1210 | [ ] | `internal/pkg/utils/errors.go` | 5 | 16 | 5 | 0 |
| 1211 | [ ] | `internal/pkg/utils/fingerprint/fingerprint.go` | 3 | 7 | 3 | 0 |
| 1212 | [ ] | `internal/pkg/utils/functions/functions.go` | 0 | 0 | 0 | 0 |
| 1213 | [ ] | `internal/pkg/utils/gin_context.go` | 8 | 22 | 8 | 0 |
| 1214 | [ ] | `internal/pkg/utils/httpget/get.go` | 1 | 1 | 1 | 0 |
| 1215 | [ ] | `internal/pkg/utils/jwt.go` | 12 | 14 | 12 | 0 |
| 1216 | [ ] | `internal/pkg/utils/jwt_blacklist.go` | 1 | 1 | 1 | 0 |
| 1217 | [ ] | `internal/pkg/utils/jwt_revocation.go` | 6 | 48 | 6 | 0 |
| 1218 | [ ] | `internal/pkg/utils/keyword.go` | 1 | 1 | 1 | 0 |
| 1219 | [ ] | `internal/pkg/utils/logger.go` | 19 | 20 | 19 | 0 |
| 1220 | [ ] | `internal/pkg/utils/logger/logger.go` | 22 | 32 | 22 | 0 |
| 1221 | [ ] | `internal/pkg/utils/mask_pii.go` | 4 | 7 | 4 | 0 |
| 1222 | [ ] | `internal/pkg/utils/pagination.go` | 15 | 91 | 15 | 0 |
| 1223 | [ ] | `internal/pkg/utils/pagination/pagination.go` | 9 | 38 | 9 | 0 |
| 1224 | [ ] | `internal/pkg/utils/parse_int.go` | 3 | 12 | 3 | 0 |
| 1225 | [ ] | `internal/pkg/utils/pointer.go` | 2 | 2 | 2 | 0 |
| 1226 | [ ] | `internal/pkg/utils/recover.go` | 4 | 47 | 4 | 0 |
| 1227 | [ ] | `internal/pkg/utils/response/errors.go` | 5 | 6 | 5 | 0 |
| 1228 | [ ] | `internal/pkg/utils/response/response.go` | 24 | 69 | 24 | 0 |
| 1229 | [ ] | `internal/pkg/utils/text/text.go` | 4 | 4 | 4 | 0 |
| 1230 | [ ] | `internal/pkg/utils/text/tokens.go` | 2 | 2 | 2 | 0 |
| 1231 | [ ] | `internal/pkg/utils/type/type.go` | 1 | 1 | 1 | 0 |
| 1232 | [ ] | `internal/pkg/utils/url/ssrf.go` | 2 | 6 | 2 | 0 |
| 1233 | [ ] | `internal/pkg/utils/urlguard/urlguard.go` | 5 | 27 | 5 | 0 |
| 1234 | [ ] | `internal/pkg/utils/warnlog.go` | 1 | 2 | 1 | 0 |
| 1235 | [ ] | `internal/platform/asset_market_adapter.go` | 3 | 4 | 3 | 0 |
| 1236 | [ ] | `internal/platform/asset_market_client.go` | 7 | 15 | 7 | 0 |
| 1237 | [ ] | `internal/platform/client.go` | 20 | 65 | 20 | 0 |
| 1238 | [ ] | `internal/platform/contributor_client.go` | 14 | 27 | 14 | 0 |
| 1239 | [ ] | `internal/platform/disabled_client.go` | 1 | 12 | 1 | 0 |
| 1240 | [ ] | `internal/platform/heartbeat_sender.go` | 4 | 15 | 4 | 0 |
| 1241 | [ ] | `internal/platform/llm_proxy_client.go` | 12 | 41 | 12 | 0 |
| 1242 | [ ] | `internal/platform/module_client.go` | 40 | 55 | 40 | 0 |
| 1243 | [ ] | `internal/platform/sync.go` | 10 | 19 | 10 | 0 |
| 1244 | [ ] | `internal/platform/tenant_code.go` | 1 | 15 | 1 | 0 |
| 1245 | [ ] | `internal/reach/card/template/template.go` | 16 | 23 | 16 | 0 |
| 1246 | [ ] | `internal/reporting/window.go` | 5 | 21 | 5 | 0 |
| 1247 | [ ] | `internal/repository/ab_exposure.go` | 2 | 3 | 2 | 0 |
| 1248 | [ ] | `internal/repository/account.go` | 1 | 1 | 1 | 0 |
| 1249 | [ ] | `internal/repository/aftersale.go` | 8 | 11 | 8 | 0 |
| 1250 | [ ] | `internal/repository/agent_attribution.go` | 8 | 15 | 8 | 0 |
| 1251 | [ ] | `internal/repository/agent_checkpoint_repo.go` | 6 | 11 | 6 | 0 |
| 1252 | [ ] | `internal/repository/agent_kb_binding.go` | 14 | 17 | 14 | 0 |
| 1253 | [ ] | `internal/repository/agent_status.go` | 17 | 23 | 17 | 0 |
| 1254 | [ ] | `internal/repository/ai_agent.go` | 44 | 60 | 44 | 0 |
| 1255 | [ ] | `internal/repository/ai_performance_repo.go` | 6 | 6 | 6 | 0 |
| 1256 | [ ] | `internal/repository/ai_suggestion.go` | 6 | 6 | 6 | 0 |
| 1257 | [ ] | `internal/repository/ai_tool_account_binding_repo.go` | 10 | 10 | 10 | 0 |
| 1258 | [ ] | `internal/repository/ai_tool_config_repo.go` | 11 | 11 | 11 | 0 |
| 1259 | [ ] | `internal/repository/alert_history.go` | 4 | 4 | 4 | 0 |
| 1260 | [ ] | `internal/repository/alert_rule.go` | 2 | 2 | 2 | 0 |
| 1261 | [ ] | `internal/repository/approval_request.go` | 21 | 111 | 21 | 0 |
| 1262 | [ ] | `internal/repository/asset_bundle_repo.go` | 5 | 15 | 5 | 0 |
| 1263 | [ ] | `internal/repository/asset_bundle_version_log_repo.go` | 3 | 6 | 3 | 0 |
| 1264 | [ ] | `internal/repository/asset_loader_repo.go` | 6 | 7 | 6 | 0 |
| 1265 | [ ] | `internal/repository/backup.go` | 18 | 18 | 18 | 0 |
| 1266 | [ ] | `internal/repository/backup_data.go` | 4 | 5 | 4 | 0 |
| 1267 | [ ] | `internal/repository/bad_case.go` | 29 | 72 | 29 | 0 |
| 1268 | [ ] | `internal/repository/bandit_allocator.go` | 12 | 29 | 12 | 0 |
| 1269 | [ ] | `internal/repository/bill.go` | 24 | 114 | 24 | 0 |
| 1270 | [ ] | `internal/repository/bridge_offline_replay_repo.go` | 18 | 62 | 18 | 0 |
| 1271 | [ ] | `internal/repository/bridge_online_predicate.go` | 2 | 13 | 2 | 0 |
| 1272 | [ ] | `internal/repository/champion_dialogue.go` | 7 | 30 | 7 | 0 |
| 1273 | [ ] | `internal/repository/channel_overview.go` | 28 | 34 | 28 | 0 |
| 1274 | [ ] | `internal/repository/chat_channel.go` | 16 | 28 | 16 | 0 |
| 1275 | [ ] | `internal/repository/churn_score.go` | 4 | 4 | 4 | 0 |
| 1276 | [ ] | `internal/repository/clue.go` | 3 | 3 | 3 | 0 |
| 1277 | [ ] | `internal/repository/clue_query.go` | 3 | 4 | 3 | 0 |
| 1278 | [ ] | `internal/repository/clue_score.go` | 4 | 4 | 4 | 0 |
| 1279 | [ ] | `internal/repository/community.go` | 0 | 0 | 0 | 0 |
| 1280 | [ ] | `internal/repository/confidence_repositories.go` | 41 | 42 | 41 | 0 |
| 1281 | [ ] | `internal/repository/config_param.go` | 14 | 53 | 14 | 0 |
| 1282 | [ ] | `internal/repository/config_param_tenant.go` | 9 | 13 | 9 | 0 |
| 1283 | [ ] | `internal/repository/cron_job_lease.go` | 6 | 16 | 6 | 0 |
| 1284 | [ ] | `internal/repository/csat.go` | 12 | 24 | 12 | 0 |
| 1285 | [ ] | `internal/repository/csplus_ops_repo.go` | 14 | 15 | 14 | 0 |
| 1286 | [ ] | `internal/repository/customer.go` | 5 | 17 | 5 | 0 |
| 1287 | [ ] | `internal/repository/customer_do_not_contact.go` | 6 | 8 | 6 | 0 |
| 1288 | [ ] | `internal/repository/customer_event.go` | 5 | 5 | 5 | 0 |
| 1289 | [ ] | `internal/repository/customer_profile_insight.go` | 5 | 5 | 5 | 0 |
| 1290 | [ ] | `internal/repository/customer_rfm.go` | 2 | 2 | 2 | 0 |
| 1291 | [ ] | `internal/repository/customer_session.go` | 47 | 142 | 47 | 0 |
| 1292 | [ ] | `internal/repository/customer_tag.go` | 2 | 2 | 2 | 0 |
| 1293 | [ ] | `internal/repository/customer_tag_assignment.go` | 3 | 7 | 3 | 0 |
| 1294 | [ ] | `internal/repository/dashboard_stats.go` | 9 | 9 | 9 | 0 |
| 1295 | [ ] | `internal/repository/data_export_repo.go` | 8 | 8 | 8 | 0 |
| 1296 | [ ] | `internal/repository/data_scope_owner.go` | 4 | 24 | 4 | 0 |
| 1297 | [ ] | `internal/repository/db_util.go` | 1 | 2 | 1 | 0 |
| 1298 | [ ] | `internal/repository/delayed_outbound_repo.go` | 16 | 26 | 16 | 0 |
| 1299 | [ ] | `internal/repository/dialogue_memory.go` | 3 | 3 | 3 | 0 |
| 1300 | [ ] | `internal/repository/dictionary.go` | 6 | 13 | 6 | 0 |
| 1301 | [ ] | `internal/repository/dingtalk_app.go` | 2 | 4 | 2 | 0 |
| 1302 | [ ] | `internal/repository/domain_pool.go` | 13 | 13 | 13 | 0 |
| 1303 | [ ] | `internal/repository/douyin_card.go` | 3 | 4 | 3 | 0 |
| 1304 | [ ] | `internal/repository/douyin_card_stats.go` | 6 | 8 | 6 | 0 |
| 1305 | [ ] | `internal/repository/email_account_repo.go` | 9 | 10 | 9 | 0 |
| 1306 | [ ] | `internal/repository/email_draft.go` | 2 | 2 | 2 | 0 |
| 1307 | [ ] | `internal/repository/email_jobs.go` | 2 | 2 | 2 | 0 |
| 1308 | [ ] | `internal/repository/email_list.go` | 2 | 2 | 2 | 0 |
| 1309 | [ ] | `internal/repository/email_send.go` | 6 | 25 | 6 | 0 |
| 1310 | [ ] | `internal/repository/email_smtp.go` | 0 | 0 | 0 | 0 |
| 1311 | [ ] | `internal/repository/email_tracking.go` | 4 | 7 | 4 | 0 |
| 1312 | [ ] | `internal/repository/email_unsubscribe.go` | 4 | 5 | 4 | 0 |
| 1313 | [ ] | `internal/repository/faq_entry.go` | 18 | 64 | 18 | 0 |
| 1314 | [ ] | `internal/repository/feature_flag.go` | 5 | 7 | 5 | 0 |
| 1315 | [ ] | `internal/repository/feedback_learning.go` | 12 | 35 | 12 | 0 |
| 1316 | [ ] | `internal/repository/feedback_loop.go` | 35 | 46 | 35 | 0 |
| 1317 | [ ] | `internal/repository/feedback_record.go` | 6 | 15 | 6 | 0 |
| 1318 | [ ] | `internal/repository/feishu.go` | 36 | 51 | 36 | 0 |
| 1319 | [ ] | `internal/repository/generic.go` | 24 | 28 | 24 | 0 |
| 1320 | [ ] | `internal/repository/glossary_repo.go` | 13 | 23 | 13 | 0 |
| 1321 | [ ] | `internal/repository/growth_repo.go` | 29 | 32 | 29 | 0 |
| 1322 | [ ] | `internal/repository/handoff_chain_repo.go` | 14 | 34 | 14 | 0 |
| 1323 | [ ] | `internal/repository/help_center.go` | 16 | 16 | 16 | 0 |
| 1324 | [ ] | `internal/repository/help_center_repo.go` | 17 | 21 | 17 | 0 |
| 1325 | [ ] | `internal/repository/human_task.go` | 41 | 125 | 41 | 0 |
| 1326 | [ ] | `internal/repository/humanize_low_quality_collector.go` | 3 | 6 | 3 | 0 |
| 1327 | [ ] | `internal/repository/humanize_repositories.go` | 27 | 38 | 27 | 0 |
| 1328 | [ ] | `internal/repository/i18n_stats_repo.go` | 7 | 8 | 7 | 0 |
| 1329 | [ ] | `internal/repository/integration.go` | 61 | 86 | 61 | 0 |
| 1330 | [ ] | `internal/repository/integration_template.go` | 1 | 1 | 1 | 0 |
| 1331 | [ ] | `internal/repository/intent_example.go` | 5 | 5 | 5 | 0 |
| 1332 | [ ] | `internal/repository/intent_recognition.go` | 21 | 24 | 21 | 0 |
| 1333 | [ ] | `internal/repository/kb_document_chunk_repo.go` | 10 | 19 | 10 | 0 |
| 1334 | [ ] | `internal/repository/kb_mcp_repo.go` | 2 | 2 | 2 | 0 |
| 1335 | [ ] | `internal/repository/kb_release.go` | 54 | 165 | 54 | 0 |
| 1336 | [ ] | `internal/repository/knowledge.go` | 4 | 8 | 4 | 0 |
| 1337 | [ ] | `internal/repository/knowledge_base.go` | 16 | 36 | 16 | 0 |
| 1338 | [ ] | `internal/repository/kuaishou_card.go` | 2 | 2 | 2 | 0 |
| 1339 | [ ] | `internal/repository/kuaishou_card_stats.go` | 5 | 8 | 5 | 0 |
| 1340 | [ ] | `internal/repository/layer_decision_log.go` | 8 | 8 | 8 | 0 |
| 1341 | [ ] | `internal/repository/lead_mining_config.go` | 1 | 1 | 1 | 0 |
| 1342 | [ ] | `internal/repository/live_code.go` | 3 | 3 | 3 | 0 |
| 1343 | [ ] | `internal/repository/live_code_click_log.go` | 2 | 3 | 2 | 0 |
| 1344 | [ ] | `internal/repository/live_code_qr.go` | 2 | 2 | 2 | 0 |
| 1345 | [ ] | `internal/repository/llm_wallet.go` | 5 | 9 | 5 | 0 |
| 1346 | [ ] | `internal/repository/local_asset_repo.go` | 5 | 5 | 5 | 0 |
| 1347 | [ ] | `internal/repository/login_risk.go` | 3 | 6 | 3 | 0 |
| 1348 | [ ] | `internal/repository/low_quality_sample.go` | 3 | 6 | 3 | 0 |
| 1349 | [ ] | `internal/repository/macro_repo.go` | 8 | 9 | 8 | 0 |
| 1350 | [ ] | `internal/repository/macro_session_ops_repo.go` | 12 | 16 | 12 | 0 |
| 1351 | [ ] | `internal/repository/memory.go` | 5 | 6 | 5 | 0 |
| 1352 | [ ] | `internal/repository/message.go` | 0 | 0 | 0 | 0 |
| 1353 | [ ] | `internal/repository/message_hub_inbox.go` | 4 | 8 | 4 | 0 |
| 1354 | [ ] | `internal/repository/message_hub_inbox_assignment.go` | 12 | 18 | 12 | 0 |
| 1355 | [ ] | `internal/repository/message_hub_inbox_conversation.go` | 20 | 34 | 20 | 0 |
| 1356 | [ ] | `internal/repository/message_hub_inbox_conversation_query.go` | 8 | 27 | 8 | 0 |
| 1357 | [ ] | `internal/repository/message_hub_inbox_ctx.go` | 2 | 8 | 2 | 0 |
| 1358 | [ ] | `internal/repository/message_hub_inbox_media.go` | 3 | 17 | 3 | 0 |
| 1359 | [ ] | `internal/repository/message_hub_inbox_message.go` | 7 | 28 | 7 | 0 |
| 1360 | [ ] | `internal/repository/message_hub_inbox_outbound.go` | 11 | 57 | 11 | 0 |
| 1361 | [ ] | `internal/repository/message_hub_inbox_stats.go` | 2 | 8 | 2 | 0 |
| 1362 | [ ] | `internal/repository/message_hub_inbox_syncgap.go` | 0 | 0 | 0 | 0 |
| 1363 | [ ] | `internal/repository/message_queue.go` | 13 | 28 | 13 | 0 |
| 1364 | [ ] | `internal/repository/message_trace_cleanup.go` | 8 | 9 | 8 | 0 |
| 1365 | [ ] | `internal/repository/msg_hourly_summary.go` | 8 | 16 | 8 | 0 |
| 1366 | [ ] | `internal/repository/notification.go` | 13 | 20 | 13 | 0 |
| 1367 | [ ] | `internal/repository/obs_config.go` | 8 | 51 | 8 | 0 |
| 1368 | [ ] | `internal/repository/office_hours.go` | 6 | 7 | 6 | 0 |
| 1369 | [ ] | `internal/repository/operation_log.go` | 8 | 19 | 8 | 0 |
| 1370 | [ ] | `internal/repository/opportunity.go` | 27 | 116 | 27 | 0 |
| 1371 | [ ] | `internal/repository/order.go` | 1 | 1 | 1 | 0 |
| 1372 | [ ] | `internal/repository/order_draft.go` | 22 | 68 | 22 | 0 |
| 1373 | [ ] | `internal/repository/password_history.go` | 2 | 2 | 2 | 0 |
| 1374 | [ ] | `internal/repository/password_reset_token.go` | 16 | 27 | 16 | 0 |
| 1375 | [ ] | `internal/repository/payment.go` | 20 | 68 | 20 | 0 |
| 1376 | [ ] | `internal/repository/platform_api_client.go` | 2 | 2 | 2 | 0 |
| 1377 | [ ] | `internal/repository/platform_config_kv.go` | 3 | 6 | 3 | 0 |
| 1378 | [ ] | `internal/repository/proactive_reach_repo.go` | 8 | 17 | 8 | 0 |
| 1379 | [ ] | `internal/repository/prompt.go` | 10 | 11 | 10 | 0 |
| 1380 | [ ] | `internal/repository/prompt_iterator.go` | 7 | 19 | 7 | 0 |
| 1381 | [ ] | `internal/repository/quick_reply.go` | 7 | 7 | 7 | 0 |
| 1382 | [ ] | `internal/repository/quick_reply_folder.go` | 6 | 6 | 6 | 0 |
| 1383 | [ ] | `internal/repository/quote.go` | 31 | 137 | 31 | 0 |
| 1384 | [ ] | `internal/repository/r44_gap.go` | 47 | 50 | 47 | 0 |
| 1385 | [ ] | `internal/repository/rag_eval.go` | 17 | 18 | 17 | 0 |
| 1386 | [ ] | `internal/repository/rag_health.go` | 2 | 4 | 2 | 0 |
| 1387 | [ ] | `internal/repository/rag_metrics.go` | 5 | 10 | 5 | 0 |
| 1388 | [ ] | `internal/repository/rag_recall_monitor.go` | 5 | 11 | 5 | 0 |
| 1389 | [ ] | `internal/repository/reach_compliance_log_repo.go` | 4 | 4 | 4 | 0 |
| 1390 | [ ] | `internal/repository/reach_pipeline.go` | 23 | 55 | 23 | 0 |
| 1391 | [ ] | `internal/repository/rfm_rule.go` | 20 | 20 | 20 | 0 |
| 1392 | [ ] | `internal/repository/role_menu.go` | 3 | 5 | 3 | 0 |
| 1393 | [ ] | `internal/repository/sales_cockpit_repo.go` | 9 | 10 | 9 | 0 |
| 1394 | [ ] | `internal/repository/sales_event.go` | 6 | 9 | 6 | 0 |
| 1395 | [ ] | `internal/repository/sales_persona.go` | 13 | 16 | 13 | 0 |
| 1396 | [ ] | `internal/repository/scope/tenant.go` | 8 | 42 | 8 | 0 |
| 1397 | [ ] | `internal/repository/script_library.go` | 14 | 25 | 14 | 0 |
| 1398 | [ ] | `internal/repository/security_audit.go` | 14 | 18 | 14 | 0 |
| 1399 | [ ] | `internal/repository/security_rules.go` | 6 | 6 | 6 | 0 |
| 1400 | [ ] | `internal/repository/session_ai.go` | 7 | 7 | 7 | 0 |
| 1401 | [ ] | `internal/repository/session_chain_repo.go` | 25 | 27 | 25 | 0 |
| 1402 | [ ] | `internal/repository/session_message.go` | 17 | 36 | 17 | 0 |
| 1403 | [ ] | `internal/repository/session_tag.go` | 7 | 7 | 7 | 0 |
| 1404 | [ ] | `internal/repository/short_link.go` | 4 | 5 | 4 | 0 |
| 1405 | [ ] | `internal/repository/short_link_access.go` | 2 | 2 | 2 | 0 |
| 1406 | [ ] | `internal/repository/sm_list.go` | 1 | 1 | 1 | 0 |
| 1407 | [ ] | `internal/repository/sms.go` | 2 | 5 | 2 | 0 |
| 1408 | [ ] | `internal/repository/sms_delivery.go` | 4 | 7 | 4 | 0 |
| 1409 | [ ] | `internal/repository/sms_tracking.go` | 4 | 9 | 4 | 0 |
| 1410 | [ ] | `internal/repository/sms_unsubscribe.go` | 2 | 2 | 2 | 0 |
| 1411 | [ ] | `internal/repository/sop.go` | 36 | 43 | 36 | 0 |
| 1412 | [ ] | `internal/repository/sop_exec_event.go` | 4 | 12 | 4 | 0 |
| 1413 | [ ] | `internal/repository/sop_template.go` | 15 | 34 | 15 | 0 |
| 1414 | [ ] | `internal/repository/sop_timer.go` | 15 | 56 | 15 | 0 |
| 1415 | [ ] | `internal/repository/sso_identity.go` | 2 | 2 | 2 | 0 |
| 1416 | [ ] | `internal/repository/support_metrics.go` | 5 | 5 | 5 | 0 |
| 1417 | [ ] | `internal/repository/system_config.go` | 5 | 16 | 5 | 0 |
| 1418 | [ ] | `internal/repository/system_config_kv.go` | 9 | 30 | 9 | 0 |
| 1419 | [ ] | `internal/repository/system_stats.go` | 3 | 10 | 3 | 0 |
| 1420 | [ ] | `internal/repository/system_user.go` | 16 | 36 | 16 | 0 |
| 1421 | [ ] | `internal/repository/telegram_gate.go` | 13 | 60 | 13 | 0 |
| 1422 | [ ] | `internal/repository/telegram_polling_lock.go` | 14 | 42 | 14 | 0 |
| 1423 | [ ] | `internal/repository/tenant.go` | 14 | 26 | 14 | 0 |
| 1424 | [ ] | `internal/repository/tenant_module.go` | 14 | 47 | 14 | 0 |
| 1425 | [ ] | `internal/repository/tenant_subscription_mirror.go` | 6 | 10 | 6 | 0 |
| 1426 | [ ] | `internal/repository/tiktok_card.go` | 2 | 2 | 2 | 0 |
| 1427 | [ ] | `internal/repository/tool_audit_repo.go` | 10 | 26 | 10 | 0 |
| 1428 | [ ] | `internal/repository/trace_learning_repo.go` | 8 | 8 | 8 | 0 |
| 1429 | [ ] | `internal/repository/unified_message.go` | 8 | 8 | 8 | 0 |
| 1430 | [ ] | `internal/repository/upgrade.go` | 16 | 16 | 16 | 0 |
| 1431 | [ ] | `internal/repository/user.go` | 0 | 0 | 0 | 0 |
| 1432 | [ ] | `internal/repository/user_blacklist.go` | 7 | 7 | 7 | 0 |
| 1433 | [ ] | `internal/repository/user_mfa.go` | 3 | 5 | 3 | 0 |
| 1434 | [ ] | `internal/repository/user_tag.go` | 3 | 3 | 3 | 0 |
| 1435 | [ ] | `internal/repository/wechat_account_repo.go` | 10 | 10 | 10 | 0 |
| 1436 | [ ] | `internal/repository/wecom.go` | 53 | 63 | 53 | 0 |
| 1437 | [ ] | `internal/repository/wecom_account_health_quota.go` | 1 | 13 | 1 | 0 |
| 1438 | [ ] | `internal/repository/whatsapp.go` | 0 | 0 | 0 | 0 |
| 1439 | [ ] | `internal/repository/whatsapp_template.go` | 9 | 9 | 9 | 0 |
| 1440 | [ ] | `internal/repository/workflow_orchestrator.go` | 26 | 26 | 26 | 0 |
| 1441 | [ ] | `internal/repository/xianyu_card.go` | 2 | 2 | 2 | 0 |
| 1442 | [ ] | `internal/repository/xianyu_card_stats.go` | 8 | 9 | 8 | 0 |
| 1443 | [ ] | `internal/repository/xiaohongshu_card.go` | 2 | 2 | 2 | 0 |
| 1444 | [ ] | `internal/repository/xiaohongshu_card_stats.go` | 5 | 19 | 5 | 0 |
| 1445 | [ ] | `internal/router/admin_routes.go` | 5 | 18 | 5 | 0 |
| 1446 | [ ] | `internal/router/approval_routes.go` | 2 | 10 | 2 | 0 |
| 1447 | [ ] | `internal/router/asset_bundle_routes.go` | 0 | 0 | 0 | 0 |
| 1448 | [ ] | `internal/router/auth_routes.go` | 1 | 3 | 1 | 0 |
| 1449 | [ ] | `internal/router/bad_case_routes.go` | 2 | 10 | 2 | 0 |
| 1450 | [ ] | `internal/router/bill_routes.go` | 2 | 21 | 2 | 0 |
| 1451 | [ ] | `internal/router/browser_automation_routes.go` | 39 | 51 | 39 | 0 |
| 1452 | [ ] | `internal/router/business_routes.go` | 10 | 31 | 10 | 0 |
| 1453 | [ ] | `internal/router/card_routes.go` | 0 | 0 | 0 | 0 |
| 1454 | [ ] | `internal/router/channel_overview_routes.go` | 0 | 0 | 0 | 0 |
| 1455 | [ ] | `internal/router/chat_routes.go` | 4 | 12 | 4 | 0 |
| 1456 | [ ] | `internal/router/competitor_routes.go` | 0 | 0 | 0 | 0 |
| 1457 | [ ] | `internal/router/config_param_routes.go` | 1 | 10 | 1 | 0 |
| 1458 | [ ] | `internal/router/content_routes.go` | 0 | 0 | 0 | 0 |
| 1459 | [ ] | `internal/router/dictionary_routes.go` | 2 | 12 | 2 | 0 |
| 1460 | [ ] | `internal/router/embed_static_routes.go` | 0 | 0 | 0 | 0 |
| 1461 | [ ] | `internal/router/files_guard.go` | 2 | 27 | 2 | 0 |
| 1462 | [ ] | `internal/router/frontend_aliases.go` | 5 | 32 | 5 | 0 |
| 1463 | [ ] | `internal/router/geo_routes.go` | 10 | 18 | 10 | 0 |
| 1464 | [ ] | `internal/router/health.go` | 9 | 19 | 9 | 0 |
| 1465 | [ ] | `internal/router/human_task_routes.go` | 2 | 10 | 2 | 0 |
| 1466 | [ ] | `internal/router/kb_release_routes.go` | 2 | 15 | 2 | 0 |
| 1467 | [ ] | `internal/router/knowledge_ports.go` | 0 | 0 | 0 | 0 |
| 1468 | [ ] | `internal/router/ltc_routes.go` | 12 | 51 | 12 | 0 |
| 1469 | [ ] | `internal/router/menu_routes.go` | 2 | 8 | 2 | 0 |
| 1470 | [ ] | `internal/router/middleware_ports.go` | 5 | 17 | 5 | 0 |
| 1471 | [ ] | `internal/router/module_gate_adapter.go` | 2 | 13 | 2 | 0 |
| 1472 | [ ] | `internal/router/module_routes.go` | 7 | 29 | 7 | 0 |
| 1473 | [ ] | `internal/router/opportunity_routes.go` | 2 | 17 | 2 | 0 |
| 1474 | [ ] | `internal/router/order_draft_routes.go` | 10 | 36 | 10 | 0 |
| 1475 | [ ] | `internal/router/platform_routes.go` | 1 | 3 | 1 | 0 |
| 1476 | [ ] | `internal/router/quote_routes.go` | 2 | 20 | 2 | 0 |
| 1477 | [ ] | `internal/router/router.go` | 53 | 184 | 53 | 0 |
| 1478 | [ ] | `internal/router/self_service_routes.go` | 0 | 0 | 0 | 0 |
| 1479 | [ ] | `internal/router/service_routes.go` | 12 | 29 | 12 | 0 |
| 1480 | [ ] | `internal/router/sso_routes.go` | 2 | 10 | 2 | 0 |
| 1481 | [ ] | `internal/router/swagger.go` | 2 | 10 | 2 | 0 |
| 1482 | [ ] | `internal/router/system_routes.go` | 0 | 0 | 0 | 0 |
| 1483 | [ ] | `internal/router/system_user_routes.go` | 0 | 0 | 0 | 0 |
| 1484 | [ ] | `internal/router/tenant_routes.go` | 4 | 14 | 4 | 0 |
| 1485 | [ ] | `internal/router/tool_debug_routes.go` | 25 | 96 | 25 | 0 |
| 1486 | [ ] | `internal/router/workflow_orchestrator_routes.go` | 0 | 0 | 0 | 0 |
| 1487 | [ ] | `internal/router/ws.go` | 2 | 32 | 2 | 0 |
| 1488 | [ ] | `internal/safeprompt/safeprompt.go` | 11 | 21 | 11 | 0 |
| 1489 | [ ] | `internal/secrets/aesgcm.go` | 9 | 30 | 9 | 0 |
| 1490 | [ ] | `internal/secrets/fileloader.go` | 6 | 25 | 6 | 0 |
| 1491 | [ ] | `internal/security/network_exposure_guard.go` | 3 | 3 | 3 | 0 |
| 1492 | [ ] | `internal/service/ab_experiment.go` | 13 | 13 | 13 | 0 |
| 1493 | [ ] | `internal/service/abtest_loader.go` | 2 | 2 | 2 | 0 |
| 1494 | [ ] | `internal/service/account.go` | 0 | 0 | 0 | 0 |
| 1495 | [ ] | `internal/service/account_domain.go` | 8 | 10 | 8 | 0 |
| 1496 | [ ] | `internal/service/aftersale.go` | 6 | 17 | 6 | 0 |
| 1497 | [ ] | `internal/service/aftersale_client.go` | 6 | 6 | 6 | 0 |
| 1498 | [ ] | `internal/service/agent_attribution.go` | 7 | 15 | 7 | 0 |
| 1499 | [ ] | `internal/service/agent_checkpoint.go` | 11 | 30 | 11 | 0 |
| 1500 | [ ] | `internal/service/agent_co_pilot.go` | 8 | 20 | 8 | 0 |
| 1501 | [ ] | `internal/service/agent_kb_binding.go` | 12 | 28 | 12 | 0 |
| 1502 | [ ] | `internal/service/agent_loader.go` | 5 | 5 | 5 | 0 |
| 1503 | [ ] | `internal/service/agent_settings_config.go` | 5 | 7 | 5 | 0 |
| 1504 | [ ] | `internal/service/agent_status.go` | 18 | 33 | 18 | 0 |
| 1505 | [ ] | `internal/service/ai_agent.go` | 37 | 74 | 37 | 0 |
| 1506 | [ ] | `internal/service/ai_agent_domain.go` | 11 | 11 | 11 | 0 |
| 1507 | [ ] | `internal/service/ai_debounce.go` | 0 | 0 | 0 | 0 |
| 1508 | [ ] | `internal/service/ai_suggestion.go` | 5 | 5 | 5 | 0 |
| 1509 | [ ] | `internal/service/ai_tagger.go` | 11 | 12 | 11 | 0 |
| 1510 | [ ] | `internal/service/ai_tool_config.go` | 10 | 10 | 10 | 0 |
| 1511 | [ ] | `internal/service/alert_checker.go` | 18 | 38 | 18 | 0 |
| 1512 | [ ] | `internal/service/alert_dispatcher.go` | 11 | 11 | 11 | 0 |
| 1513 | [ ] | `internal/service/alert_rule.go` | 14 | 14 | 14 | 0 |
| 1514 | [ ] | `internal/service/anomaly_login_detector.go` | 14 | 21 | 14 | 0 |
| 1515 | [ ] | `internal/service/approval_request.go` | 64 | 269 | 64 | 0 |
| 1516 | [ ] | `internal/service/approval_sweep.go` | 13 | 32 | 13 | 0 |
| 1517 | [ ] | `internal/service/asset_bundle.go` | 6 | 20 | 6 | 0 |
| 1518 | [ ] | `internal/service/asset_bundle_mapper.go` | 4 | 4 | 4 | 0 |
| 1519 | [ ] | `internal/service/asset_bundle_port_adapter.go` | 8 | 15 | 8 | 0 |
| 1520 | [ ] | `internal/service/asset_bundle_submit.go` | 2 | 4 | 2 | 0 |
| 1521 | [ ] | `internal/service/asset_loader_base.go` | 5 | 6 | 5 | 0 |
| 1522 | [ ] | `internal/service/asset_market.go` | 1 | 1 | 1 | 0 |
| 1523 | [ ] | `internal/service/asset_resolver.go` | 4 | 14 | 4 | 0 |
| 1524 | [ ] | `internal/service/assignment.go` | 9 | 19 | 9 | 0 |
| 1525 | [ ] | `internal/service/audience_selector.go` | 27 | 66 | 27 | 0 |
| 1526 | [ ] | `internal/service/auth.go` | 73 | 252 | 73 | 0 |
| 1527 | [ ] | `internal/service/auto_tagger.go` | 8 | 8 | 8 | 0 |
| 1528 | [ ] | `internal/service/backup.go` | 22 | 26 | 22 | 0 |
| 1529 | [ ] | `internal/service/bad_case.go` | 39 | 108 | 39 | 0 |
| 1530 | [ ] | `internal/service/bandit_reward_reflux_cron.go` | 1 | 1 | 1 | 0 |
| 1531 | [ ] | `internal/service/behavioral_humanize.go` | 9 | 30 | 9 | 0 |
| 1532 | [ ] | `internal/service/bill.go` | 37 | 141 | 37 | 0 |
| 1533 | [ ] | `internal/service/bill_global.go` | 4 | 14 | 4 | 0 |
| 1534 | [ ] | `internal/service/bridge_offline_replay.go` | 38 | 97 | 38 | 0 |
| 1535 | [ ] | `internal/service/bridge_outbound.go` | 6 | 22 | 6 | 0 |
| 1536 | [ ] | `internal/service/bridge_outbound_markdown.go` | 8 | 26 | 8 | 0 |
| 1537 | [ ] | `internal/service/bridge_token.go` | 4 | 11 | 4 | 0 |
| 1538 | [ ] | `internal/service/card_stats.go` | 1 | 11 | 1 | 0 |
| 1539 | [ ] | `internal/service/card_stats_adapters.go` | 7 | 12 | 7 | 0 |
| 1540 | [ ] | `internal/service/channel_error.go` | 42 | 81 | 42 | 0 |
| 1541 | [ ] | `internal/service/channel_media.go` | 16 | 32 | 16 | 0 |
| 1542 | [ ] | `internal/service/channel_overview.go` | 7 | 12 | 7 | 0 |
| 1543 | [ ] | `internal/service/chat_channel.go` | 28 | 77 | 28 | 0 |
| 1544 | [ ] | `internal/service/chat_visitor.go` | 29 | 93 | 29 | 0 |
| 1545 | [ ] | `internal/service/churn/bgnbd.go` | 4 | 15 | 4 | 0 |
| 1546 | [ ] | `internal/service/churn/fit.go` | 4 | 9 | 4 | 0 |
| 1547 | [ ] | `internal/service/churn_score.go` | 8 | 8 | 8 | 0 |
| 1548 | [ ] | `internal/service/churn_score_job.go` | 5 | 5 | 5 | 0 |
| 1549 | [ ] | `internal/service/clue.go` | 7 | 19 | 7 | 0 |
| 1550 | [ ] | `internal/service/clue_score.go` | 13 | 20 | 13 | 0 |
| 1551 | [ ] | `internal/service/clue_score_mapper.go` | 0 | 0 | 0 | 0 |
| 1552 | [ ] | `internal/service/collection_hook.go` | 6 | 17 | 6 | 0 |
| 1553 | [ ] | `internal/service/collection_job.go` | 72 | 219 | 72 | 0 |
| 1554 | [ ] | `internal/service/collection_reward.go` | 8 | 25 | 8 | 0 |
| 1555 | [ ] | `internal/service/community.go` | 0 | 0 | 0 | 0 |
| 1556 | [ ] | `internal/service/confidence/ab_testing.go` | 6 | 16 | 6 | 0 |
| 1557 | [ ] | `internal/service/confidence/aggregator.go` | 8 | 16 | 8 | 0 |
| 1558 | [ ] | `internal/service/confidence/beta_calibration.go` | 7 | 41 | 7 | 0 |
| 1559 | [ ] | `internal/service/confidence/calibrator.go` | 10 | 20 | 10 | 0 |
| 1560 | [ ] | `internal/service/confidence/conformal_calibrator.go` | 9 | 19 | 9 | 0 |
| 1561 | [ ] | `internal/service/confidence/conformal_predictor.go` | 9 | 79 | 9 | 0 |
| 1562 | [ ] | `internal/service/confidence/dynamic_threshold.go` | 5 | 31 | 5 | 0 |
| 1563 | [ ] | `internal/service/confidence/golden_section.go` | 3 | 14 | 3 | 0 |
| 1564 | [ ] | `internal/service/confidence/handoff_decision.go` | 6 | 15 | 6 | 0 |
| 1565 | [ ] | `internal/service/confidence/platt_scaling.go` | 8 | 46 | 8 | 0 |
| 1566 | [ ] | `internal/service/confidence/signal_collector.go` | 4 | 16 | 4 | 0 |
| 1567 | [ ] | `internal/service/confidence/temperature_scaler.go` | 6 | 15 | 6 | 0 |
| 1568 | [ ] | `internal/service/confidence/threshold_policy.go` | 6 | 10 | 6 | 0 |
| 1569 | [ ] | `internal/service/confidence/threshold_policy_mapper.go` | 0 | 0 | 0 | 0 |
| 1570 | [ ] | `internal/service/confidence/veto_rule.go` | 25 | 52 | 25 | 0 |
| 1571 | [ ] | `internal/service/confidence/weighted_aggregator.go` | 8 | 15 | 8 | 0 |
| 1572 | [ ] | `internal/service/confidence_init.go` | 4 | 14 | 4 | 0 |
| 1573 | [ ] | `internal/service/config_param.go` | 47 | 173 | 47 | 0 |
| 1574 | [ ] | `internal/service/config_param_group_seeds.go` | 2 | 17 | 2 | 0 |
| 1575 | [ ] | `internal/service/config_param_seeds.go` | 23 | 111 | 23 | 0 |
| 1576 | [ ] | `internal/service/config_param_tenant_scope.go` | 13 | 39 | 13 | 0 |
| 1577 | [ ] | `internal/service/context_budget.go` | 24 | 39 | 24 | 0 |
| 1578 | [ ] | `internal/service/courier_client.go` | 4 | 4 | 4 | 0 |
| 1579 | [ ] | `internal/service/csat.go` | 15 | 25 | 15 | 0 |
| 1580 | [ ] | `internal/service/customer.go` | 21 | 25 | 21 | 0 |
| 1581 | [ ] | `internal/service/customer_360.go` | 7 | 23 | 7 | 0 |
| 1582 | [ ] | `internal/service/customer_360_profiles.go` | 2 | 3 | 2 | 0 |
| 1583 | [ ] | `internal/service/customer_domain.go` | 31 | 31 | 31 | 0 |
| 1584 | [ ] | `internal/service/customer_domain_helpers.go` | 10 | 31 | 10 | 0 |
| 1585 | [ ] | `internal/service/customer_event_helper.go` | 2 | 6 | 2 | 0 |
| 1586 | [ ] | `internal/service/customer_identity.go` | 14 | 22 | 14 | 0 |
| 1587 | [ ] | `internal/service/customer_identity_conflict.go` | 4 | 6 | 4 | 0 |
| 1588 | [ ] | `internal/service/customer_identity_normalize.go` | 7 | 7 | 7 | 0 |
| 1589 | [ ] | `internal/service/customer_journey.go` | 33 | 70 | 33 | 0 |
| 1590 | [ ] | `internal/service/customer_orchestrator.go` | 15 | 34 | 15 | 0 |
| 1591 | [ ] | `internal/service/customer_queue.go` | 9 | 9 | 9 | 0 |
| 1592 | [ ] | `internal/service/customer_rfm.go` | 20 | 31 | 20 | 0 |
| 1593 | [ ] | `internal/service/customer_rfm_cron.go` | 5 | 7 | 5 | 0 |
| 1594 | [ ] | `internal/service/customer_rfm_mapper.go` | 0 | 0 | 0 | 0 |
| 1595 | [ ] | `internal/service/customer_service_plus.go` | 39 | 56 | 39 | 0 |
| 1596 | [ ] | `internal/service/customer_session.go` | 21 | 62 | 21 | 0 |
| 1597 | [ ] | `internal/service/customer_session_blacklist.go` | 7 | 18 | 7 | 0 |
| 1598 | [ ] | `internal/service/customer_session_cron.go` | 7 | 12 | 7 | 0 |
| 1599 | [ ] | `internal/service/customer_session_domain.go` | 3 | 3 | 3 | 0 |
| 1600 | [ ] | `internal/service/customer_session_routing.go` | 4 | 4 | 4 | 0 |
| 1601 | [ ] | `internal/service/customer_session_scope.go` | 8 | 14 | 8 | 0 |
| 1602 | [ ] | `internal/service/customer_session_takeover.go` | 8 | 26 | 8 | 0 |
| 1603 | [ ] | `internal/service/dashboard_sse_stats.go` | 15 | 23 | 15 | 0 |
| 1604 | [ ] | `internal/service/data_export.go` | 6 | 18 | 6 | 0 |
| 1605 | [ ] | `internal/service/data_scope.go` | 3 | 8 | 3 | 0 |
| 1606 | [ ] | `internal/service/dialogue_memory.go` | 10 | 12 | 10 | 0 |
| 1607 | [ ] | `internal/service/dictionary.go` | 14 | 38 | 14 | 0 |
| 1608 | [ ] | `internal/service/dictionary_seeds.go` | 9 | 26 | 9 | 0 |
| 1609 | [ ] | `internal/service/dingtalk.go` | 3 | 8 | 3 | 0 |
| 1610 | [ ] | `internal/service/dingtalk_app.go` | 32 | 77 | 32 | 0 |
| 1611 | [ ] | `internal/service/dingtalk_media.go` | 12 | 39 | 12 | 0 |
| 1612 | [ ] | `internal/service/do_not_contact.go` | 12 | 35 | 12 | 0 |
| 1613 | [ ] | `internal/service/domain_health.go` | 5 | 15 | 5 | 0 |
| 1614 | [ ] | `internal/service/domain_pool.go` | 9 | 14 | 9 | 0 |
| 1615 | [ ] | `internal/service/douyin_card.go` | 3 | 3 | 3 | 0 |
| 1616 | [ ] | `internal/service/douyin_card_stats.go` | 3 | 5 | 3 | 0 |
| 1617 | [ ] | `internal/service/douyin_integration.go` | 1 | 1 | 1 | 0 |
| 1618 | [ ] | `internal/service/douyin_lead_miner.go` | 7 | 15 | 7 | 0 |
| 1619 | [ ] | `internal/service/douyin_media.go` | 49 | 104 | 49 | 0 |
| 1620 | [ ] | `internal/service/email.go` | 9 | 20 | 9 | 0 |
| 1621 | [ ] | `internal/service/email_open_tracker.go` | 16 | 41 | 16 | 0 |
| 1622 | [ ] | `internal/service/email_tracking.go` | 19 | 31 | 19 | 0 |
| 1623 | [ ] | `internal/service/email_unsubscribe.go` | 13 | 27 | 13 | 0 |
| 1624 | [ ] | `internal/service/emotion_strategy.go` | 7 | 8 | 7 | 0 |
| 1625 | [ ] | `internal/service/event_tracker.go` | 20 | 35 | 20 | 0 |
| 1626 | [ ] | `internal/service/faq.go` | 27 | 75 | 27 | 0 |
| 1627 | [ ] | `internal/service/feature_flag.go` | 24 | 31 | 24 | 0 |
| 1628 | [ ] | `internal/service/feedback_learner.go` | 10 | 12 | 10 | 0 |
| 1629 | [ ] | `internal/service/feedback_learning.go` | 15 | 21 | 15 | 0 |
| 1630 | [ ] | `internal/service/feedback_learning_cron.go` | 2 | 2 | 2 | 0 |
| 1631 | [ ] | `internal/service/feedback_loop/bandit_allocator.go` | 8 | 45 | 8 | 0 |
| 1632 | [ ] | `internal/service/feedback_loop/bandit_reward_reflux.go` | 5 | 7 | 5 | 0 |
| 1633 | [ ] | `internal/service/feedback_loop/champion_dialogue_analyzer.go` | 4 | 8 | 4 | 0 |
| 1634 | [ ] | `internal/service/feedback_loop/feedback_collector.go` | 7 | 22 | 7 | 0 |
| 1635 | [ ] | `internal/service/feedback_loop/gepa_attribution.go` | 7 | 14 | 7 | 0 |
| 1636 | [ ] | `internal/service/feedback_loop/prompt_iterator.go` | 3 | 15 | 3 | 0 |
| 1637 | [ ] | `internal/service/feedback_loop/sop_auto_optimizer.go` | 3 | 8 | 3 | 0 |
| 1638 | [ ] | `internal/service/feedback_loop/sop_optimization_gate.go` | 1 | 1 | 1 | 0 |
| 1639 | [ ] | `internal/service/feedback_loop/sop_variant.go` | 3 | 9 | 3 | 0 |
| 1640 | [ ] | `internal/service/feedback_loop/types.go` | 16 | 34 | 16 | 0 |
| 1641 | [ ] | `internal/service/feedback_loop/validation.go` | 1 | 7 | 1 | 0 |
| 1642 | [ ] | `internal/service/feedback_loop_cron.go` | 8 | 11 | 8 | 0 |
| 1643 | [ ] | `internal/service/feedback_loop_init.go` | 4 | 6 | 4 | 0 |
| 1644 | [ ] | `internal/service/feedback_loop_runtime_params.go` | 11 | 46 | 11 | 0 |
| 1645 | [ ] | `internal/service/feishu.go` | 37 | 83 | 37 | 0 |
| 1646 | [ ] | `internal/service/feishu_whatsapp_cloud_facade.go` | 16 | 16 | 16 | 0 |
| 1647 | [ ] | `internal/service/followup_reminder.go` | 21 | 36 | 21 | 0 |
| 1648 | [ ] | `internal/service/group_silence.go` | 13 | 18 | 13 | 0 |
| 1649 | [ ] | `internal/service/guard.go` | 5 | 29 | 5 | 0 |
| 1650 | [ ] | `internal/service/handoff_chain.go` | 7 | 13 | 7 | 0 |
| 1651 | [ ] | `internal/service/handoff_cond_tree.go` | 4 | 14 | 4 | 0 |
| 1652 | [ ] | `internal/service/health_weights_runtime_params.go` | 7 | 27 | 7 | 0 |
| 1653 | [ ] | `internal/service/help_center.go` | 12 | 12 | 12 | 0 |
| 1654 | [ ] | `internal/service/human_escalation.go` | 21 | 58 | 21 | 0 |
| 1655 | [ ] | `internal/service/human_task.go` | 98 | 288 | 98 | 0 |
| 1656 | [ ] | `internal/service/human_task_sla_job.go` | 32 | 68 | 32 | 0 |
| 1657 | [ ] | `internal/service/humanize/ab_metrics.go` | 18 | 45 | 18 | 0 |
| 1658 | [ ] | `internal/service/humanize/ab_persist.go` | 1 | 6 | 1 | 0 |
| 1659 | [ ] | `internal/service/humanize/abtest_stats.go` | 8 | 25 | 8 | 0 |
| 1660 | [ ] | `internal/service/humanize/ai/detector.go` | 8 | 39 | 8 | 0 |
| 1661 | [ ] | `internal/service/humanize/behavioral/behavioral.go` | 6 | 30 | 6 | 0 |
| 1662 | [ ] | `internal/service/humanize/llm_scorer.go` | 7 | 7 | 7 | 0 |
| 1663 | [ ] | `internal/service/humanize/rule_scorer.go` | 3 | 5 | 3 | 0 |
| 1664 | [ ] | `internal/service/humanize/service.go` | 30 | 54 | 30 | 0 |
| 1665 | [ ] | `internal/service/humanize/tfidf_phrase.go` | 6 | 14 | 6 | 0 |
| 1666 | [ ] | `internal/service/humanize/types.go` | 10 | 18 | 10 | 0 |
| 1667 | [ ] | `internal/service/humanize_init.go` | 5 | 27 | 5 | 0 |
| 1668 | [ ] | `internal/service/humanize_polisher.go` | 5 | 5 | 5 | 0 |
| 1669 | [ ] | `internal/service/inbox.go` | 7 | 17 | 7 | 0 |
| 1670 | [ ] | `internal/service/inbox_ingress.go` | 24 | 57 | 24 | 0 |
| 1671 | [ ] | `internal/service/inbox_ingress_ingest.go` | 8 | 38 | 8 | 0 |
| 1672 | [ ] | `internal/service/inbox_ingress_outbound.go` | 6 | 18 | 6 | 0 |
| 1673 | [ ] | `internal/service/inbox_ingress_persist.go` | 3 | 11 | 3 | 0 |
| 1674 | [ ] | `internal/service/inbox_scope.go` | 20 | 29 | 20 | 0 |
| 1675 | [ ] | `internal/service/init_storage.go` | 2 | 10 | 2 | 0 |
| 1676 | [ ] | `internal/service/integration.go` | 22 | 82 | 22 | 0 |
| 1677 | [ ] | `internal/service/integration_template.go` | 14 | 15 | 14 | 0 |
| 1678 | [ ] | `internal/service/intent_fewshot.go` | 9 | 17 | 9 | 0 |
| 1679 | [ ] | `internal/service/intent_metrics.go` | 23 | 33 | 23 | 0 |
| 1680 | [ ] | `internal/service/intent_recognition.go` | 40 | 115 | 40 | 0 |
| 1681 | [ ] | `internal/service/intent_recognition_fine.go` | 15 | 31 | 15 | 0 |
| 1682 | [ ] | `internal/service/intent_speculative.go` | 1 | 11 | 1 | 0 |
| 1683 | [ ] | `internal/service/kb_canary.go` | 14 | 69 | 14 | 0 |
| 1684 | [ ] | `internal/service/kb_connector_pull.go` | 9 | 10 | 9 | 0 |
| 1685 | [ ] | `internal/service/kb_connectors.go` | 8 | 8 | 8 | 0 |
| 1686 | [ ] | `internal/service/kb_mcp_admin.go` | 4 | 4 | 4 | 0 |
| 1687 | [ ] | `internal/service/kb_release.go` | 52 | 148 | 52 | 0 |
| 1688 | [ ] | `internal/service/knowledge_base.go` | 18 | 47 | 18 | 0 |
| 1689 | [ ] | `internal/service/knowledge_update.go` | 6 | 7 | 6 | 0 |
| 1690 | [ ] | `internal/service/kuaishou_card.go` | 3 | 3 | 3 | 0 |
| 1691 | [ ] | `internal/service/kuaishou_card_stats.go` | 5 | 5 | 5 | 0 |
| 1692 | [ ] | `internal/service/layer.go` | 5 | 11 | 5 | 0 |
| 1693 | [ ] | `internal/service/lead_intent_llm.go` | 8 | 26 | 8 | 0 |
| 1694 | [ ] | `internal/service/lead_miner_adapters.go` | 8 | 8 | 8 | 0 |
| 1695 | [ ] | `internal/service/lead_miner_unified.go` | 21 | 33 | 21 | 0 |
| 1696 | [ ] | `internal/service/lead_mining.go` | 13 | 26 | 13 | 0 |
| 1697 | [ ] | `internal/service/live_code.go` | 2 | 2 | 2 | 0 |
| 1698 | [ ] | `internal/service/llm_failover.go` | 18 | 18 | 18 | 0 |
| 1699 | [ ] | `internal/service/llm_platform_usage.go` | 12 | 37 | 12 | 0 |
| 1700 | [ ] | `internal/service/llm_routing.go` | 33 | 82 | 33 | 0 |
| 1701 | [ ] | `internal/service/llm_wallet.go` | 19 | 31 | 19 | 0 |
| 1702 | [ ] | `internal/service/local_asset.go` | 16 | 24 | 16 | 0 |
| 1703 | [ ] | `internal/service/lock_idempotency_params.go` | 10 | 27 | 10 | 0 |
| 1704 | [ ] | `internal/service/login_risk.go` | 11 | 26 | 11 | 0 |
| 1705 | [ ] | `internal/service/logistics_port_adapter.go` | 3 | 3 | 3 | 0 |
| 1706 | [ ] | `internal/service/ltc_config.go` | 63 | 173 | 63 | 0 |
| 1707 | [ ] | `internal/service/ltc_flag.go` | 2 | 9 | 2 | 0 |
| 1708 | [ ] | `internal/service/macros.go` | 10 | 10 | 10 | 0 |
| 1709 | [ ] | `internal/service/marketing_flow_sms_init.go` | 1 | 1 | 1 | 0 |
| 1710 | [ ] | `internal/service/memory_decay.go` | 5 | 11 | 5 | 0 |
| 1711 | [ ] | `internal/service/memory_facade.go` | 7 | 10 | 7 | 0 |
| 1712 | [ ] | `internal/service/memory_system.go` | 30 | 58 | 30 | 0 |
| 1713 | [ ] | `internal/service/menu.go` | 14 | 67 | 14 | 0 |
| 1714 | [ ] | `internal/service/message.go` | 0 | 0 | 0 | 0 |
| 1715 | [ ] | `internal/service/message_hub.go` | 62 | 101 | 62 | 0 |
| 1716 | [ ] | `internal/service/message_hub_summary_agg.go` | 6 | 11 | 6 | 0 |
| 1717 | [ ] | `internal/service/message_queue.go` | 1 | 6 | 1 | 0 |
| 1718 | [ ] | `internal/service/message_trace_cleanup_cron.go` | 0 | 0 | 0 | 0 |
| 1719 | [ ] | `internal/service/mfa.go` | 21 | 32 | 21 | 0 |
| 1720 | [x] | `internal/service/module_catalog.go` | 73 | 305 | 73 | 0 |
| 1721 | [ ] | `internal/service/module_market.go` | 28 | 58 | 28 | 0 |
| 1722 | [ ] | `internal/service/module_regulatory.go` | 6 | 16 | 6 | 0 |
| 1723 | [ ] | `internal/service/module_runtime_filter.go` | 27 | 153 | 27 | 0 |
| 1724 | [ ] | `internal/service/module_sync_runner.go` | 9 | 38 | 9 | 0 |
| 1725 | [ ] | `internal/service/nlp_keywords.go` | 4 | 6 | 4 | 0 |
| 1726 | [ ] | `internal/service/notification.go` | 11 | 19 | 11 | 0 |
| 1727 | [ ] | `internal/service/notify_business.go` | 15 | 46 | 15 | 0 |
| 1728 | [ ] | `internal/service/objection_handler.go` | 14 | 20 | 14 | 0 |
| 1729 | [ ] | `internal/service/obs_config.go` | 6 | 29 | 6 | 0 |
| 1730 | [ ] | `internal/service/office_hours.go` | 9 | 10 | 9 | 0 |
| 1731 | [ ] | `internal/service/oneid_merge_preview.go` | 8 | 16 | 8 | 0 |
| 1732 | [ ] | `internal/service/oneid_merge_rule.go` | 9 | 19 | 9 | 0 |
| 1733 | [ ] | `internal/service/openapi.go` | 14 | 14 | 14 | 0 |
| 1734 | [ ] | `internal/service/operation_log_domain.go` | 15 | 17 | 15 | 0 |
| 1735 | [ ] | `internal/service/opportunity.go` | 71 | 240 | 71 | 0 |
| 1736 | [ ] | `internal/service/opportunity_assign.go` | 18 | 60 | 18 | 0 |
| 1737 | [ ] | `internal/service/opportunity_convert.go` | 44 | 131 | 44 | 0 |
| 1738 | [ ] | `internal/service/opportunity_roster.go` | 6 | 28 | 6 | 0 |
| 1739 | [ ] | `internal/service/order.go` | 4 | 4 | 4 | 0 |
| 1740 | [ ] | `internal/service/order_draft.go` | 53 | 159 | 53 | 0 |
| 1741 | [ ] | `internal/service/order_draft_helpers.go` | 0 | 0 | 0 | 0 |
| 1742 | [ ] | `internal/service/order_draft_store.go` | 27 | 75 | 27 | 0 |
| 1743 | [ ] | `internal/service/order_draft_sweep.go` | 17 | 55 | 17 | 0 |
| 1744 | [ ] | `internal/service/password_policy.go` | 17 | 46 | 17 | 0 |
| 1745 | [ ] | `internal/service/password_reset.go` | 11 | 24 | 11 | 0 |
| 1746 | [ ] | `internal/service/payment.go` | 67 | 169 | 67 | 0 |
| 1747 | [ ] | `internal/service/payment_global.go` | 4 | 16 | 4 | 0 |
| 1748 | [ ] | `internal/service/payment_webhook.go` | 11 | 73 | 11 | 0 |
| 1749 | [ ] | `internal/service/permission.go` | 10 | 15 | 10 | 0 |
| 1750 | [ ] | `internal/service/permission_check.go` | 16 | 44 | 16 | 0 |
| 1751 | [ ] | `internal/service/platform_account.go` | 13 | 20 | 13 | 0 |
| 1752 | [ ] | `internal/service/proactive_reach.go` | 63 | 121 | 63 | 0 |
| 1753 | [ ] | `internal/service/prompt.go` | 8 | 8 | 8 | 0 |
| 1754 | [ ] | `internal/service/qq_account.go` | 28 | 39 | 28 | 0 |
| 1755 | [ ] | `internal/service/qq_media.go` | 11 | 33 | 11 | 0 |
| 1756 | [ ] | `internal/service/qq_webhook_bootstrap.go` | 9 | 26 | 9 | 0 |
| 1757 | [ ] | `internal/service/quick_reply.go` | 10 | 14 | 10 | 0 |
| 1758 | [ ] | `internal/service/quote.go` | 83 | 241 | 83 | 0 |
| 1759 | [ ] | `internal/service/quote_global.go` | 7 | 16 | 7 | 0 |
| 1760 | [ ] | `internal/service/quote_send.go` | 62 | 251 | 62 | 0 |
| 1761 | [ ] | `internal/service/r44_gap_services.go` | 46 | 48 | 46 | 0 |
| 1762 | [ ] | `internal/service/r48_growth.go` | 24 | 25 | 24 | 0 |
| 1763 | [ ] | `internal/service/rag_eval_auto.go` | 7 | 17 | 7 | 0 |
| 1764 | [ ] | `internal/service/rag_eval_cron.go` | 4 | 5 | 4 | 0 |
| 1765 | [ ] | `internal/service/rag_health.go` | 10 | 15 | 10 | 0 |
| 1766 | [ ] | `internal/service/rag_metrics.go` | 21 | 37 | 21 | 0 |
| 1767 | [ ] | `internal/service/rag_product.go` | 1 | 3 | 1 | 0 |
| 1768 | [ ] | `internal/service/rag_recall_monitor.go` | 11 | 17 | 11 | 0 |
| 1769 | [ ] | `internal/service/reach_gcra_limiter.go` | 4 | 8 | 4 | 0 |
| 1770 | [ ] | `internal/service/reach_pipeline.go` | 1 | 12 | 1 | 0 |
| 1771 | [ ] | `internal/service/reach_pipeline_alert.go` | 1 | 1 | 1 | 0 |
| 1772 | [ ] | `internal/service/reach_pipeline_claim.go` | 3 | 14 | 3 | 0 |
| 1773 | [ ] | `internal/service/reach_pipeline_dispatch.go` | 4 | 11 | 4 | 0 |
| 1774 | [ ] | `internal/service/reach_pipeline_job.go` | 3 | 3 | 3 | 0 |
| 1775 | [ ] | `internal/service/reach_pipeline_ratelimit.go` | 6 | 8 | 6 | 0 |
| 1776 | [ ] | `internal/service/reach_pipeline_shared.go` | 2 | 3 | 2 | 0 |
| 1777 | [ ] | `internal/service/reach_pipeline_steps.go` | 1 | 2 | 1 | 0 |
| 1778 | [ ] | `internal/service/reach_send_pipeline.go` | 10 | 20 | 10 | 0 |
| 1779 | [ ] | `internal/service/reach_send_pipeline_compliance.go` | 9 | 12 | 9 | 0 |
| 1780 | [ ] | `internal/service/reach_send_pipeline_components.go` | 14 | 16 | 14 | 0 |
| 1781 | [ ] | `internal/service/reach_send_pipeline_quiethours.go` | 8 | 15 | 8 | 0 |
| 1782 | [ ] | `internal/service/reach_send_pipeline_stats.go` | 5 | 6 | 5 | 0 |
| 1783 | [ ] | `internal/service/reach_send_pipeline_steps.go` | 1 | 3 | 1 | 0 |
| 1784 | [ ] | `internal/service/recovery_queue.go` | 18 | 30 | 18 | 0 |
| 1785 | [ ] | `internal/service/recovery_queue_mapper.go` | 0 | 0 | 0 | 0 |
| 1786 | [ ] | `internal/service/recovery_queue_worker.go` | 42 | 114 | 42 | 0 |
| 1787 | [ ] | `internal/service/repurchase_engine.go` | 14 | 16 | 14 | 0 |
| 1788 | [ ] | `internal/service/role.go` | 6 | 16 | 6 | 0 |
| 1789 | [ ] | `internal/service/sales_action_trigger.go` | 11 | 36 | 11 | 0 |
| 1790 | [ ] | `internal/service/sales_cockpit.go` | 6 | 11 | 6 | 0 |
| 1791 | [ ] | `internal/service/sales_engine.go` | 6 | 9 | 6 | 0 |
| 1792 | [ ] | `internal/service/sales_engine_adapters.go` | 2 | 2 | 2 | 0 |
| 1793 | [ ] | `internal/service/sales_engine_agent.go` | 2 | 20 | 2 | 0 |
| 1794 | [ ] | `internal/service/sales_engine_agent_asset_bundle.go` | 3 | 8 | 3 | 0 |
| 1795 | [ ] | `internal/service/sales_engine_agentloop.go` | 19 | 64 | 19 | 0 |
| 1796 | [ ] | `internal/service/sales_engine_agentloop_guard.go` | 1 | 1 | 1 | 0 |
| 1797 | [ ] | `internal/service/sales_engine_feedback.go` | 0 | 0 | 0 | 0 |
| 1798 | [ ] | `internal/service/sales_engine_i18n.go` | 4 | 11 | 4 | 0 |
| 1799 | [ ] | `internal/service/sales_engine_parallel.go` | 3 | 11 | 3 | 0 |
| 1800 | [ ] | `internal/service/sales_engine_playbook.go` | 4 | 16 | 4 | 0 |
| 1801 | [ ] | `internal/service/sales_engine_recall.go` | 2 | 3 | 2 | 0 |
| 1802 | [ ] | `internal/service/sales_engine_transfer.go` | 2 | 4 | 2 | 0 |
| 1803 | [ ] | `internal/service/sales_event_stats.go` | 31 | 38 | 31 | 0 |
| 1804 | [ ] | `internal/service/sales_mapper.go` | 5 | 5 | 5 | 0 |
| 1805 | [ ] | `internal/service/sales_persona.go` | 7 | 7 | 7 | 0 |
| 1806 | [ ] | `internal/service/sales_playbook.go` | 15 | 20 | 15 | 0 |
| 1807 | [ ] | `internal/service/sales_trace_emit.go` | 2 | 5 | 2 | 0 |
| 1808 | [ ] | `internal/service/sales_workbench.go` | 20 | 32 | 20 | 0 |
| 1809 | [ ] | `internal/service/script_ab.go` | 18 | 26 | 18 | 0 |
| 1810 | [ ] | `internal/service/script_loader.go` | 2 | 2 | 2 | 0 |
| 1811 | [ ] | `internal/service/security_audit.go` | 6 | 7 | 6 | 0 |
| 1812 | [ ] | `internal/service/security_rules.go` | 8 | 11 | 8 | 0 |
| 1813 | [ ] | `internal/service/segment.go` | 4 | 19 | 4 | 0 |
| 1814 | [ ] | `internal/service/session_ai.go` | 4 | 4 | 4 | 0 |
| 1815 | [ ] | `internal/service/session_assignment.go` | 10 | 15 | 10 | 0 |
| 1816 | [ ] | `internal/service/session_chain.go` | 29 | 33 | 29 | 0 |
| 1817 | [ ] | `internal/service/session_tag.go` | 7 | 7 | 7 | 0 |
| 1818 | [ ] | `internal/service/short_link.go` | 10 | 15 | 10 | 0 |
| 1819 | [ ] | `internal/service/sm_list.go` | 0 | 0 | 0 | 0 |
| 1820 | [ ] | `internal/service/smart_cs_orchestrator.go` | 45 | 186 | 45 | 0 |
| 1821 | [ ] | `internal/service/smart_router.go` | 6 | 12 | 6 | 0 |
| 1822 | [ ] | `internal/service/sms.go` | 12 | 31 | 12 | 0 |
| 1823 | [ ] | `internal/service/sms_delivery_tracker.go` | 17 | 45 | 17 | 0 |
| 1824 | [ ] | `internal/service/sms_helpers.go` | 0 | 0 | 0 | 0 |
| 1825 | [ ] | `internal/service/sms_tracking.go` | 12 | 36 | 12 | 0 |
| 1826 | [ ] | `internal/service/sms_unsubscribe.go` | 10 | 26 | 10 | 0 |
| 1827 | [ ] | `internal/service/sop.go` | 7 | 14 | 7 | 0 |
| 1828 | [ ] | `internal/service/sop_abtest.go` | 8 | 21 | 8 | 0 |
| 1829 | [ ] | `internal/service/sop_approval_resume.go` | 45 | 177 | 45 | 0 |
| 1830 | [ ] | `internal/service/sop_compensation.go` | 29 | 103 | 29 | 0 |
| 1831 | [ ] | `internal/service/sop_condition.go` | 9 | 35 | 9 | 0 |
| 1832 | [ ] | `internal/service/sop_dispatcher.go` | 32 | 83 | 32 | 0 |
| 1833 | [ ] | `internal/service/sop_entry_policy.go` | 8 | 9 | 8 | 0 |
| 1834 | [ ] | `internal/service/sop_heatmap.go` | 8 | 8 | 8 | 0 |
| 1835 | [ ] | `internal/service/sop_loader.go` | 2 | 2 | 2 | 0 |
| 1836 | [ ] | `internal/service/sop_node_executor.go` | 20 | 61 | 20 | 0 |
| 1837 | [ ] | `internal/service/sop_node_executors.go` | 18 | 65 | 18 | 0 |
| 1838 | [ ] | `internal/service/sop_outbox_dispatcher.go` | 25 | 85 | 25 | 0 |
| 1839 | [ ] | `internal/service/sop_reach_send.go` | 29 | 98 | 29 | 0 |
| 1840 | [ ] | `internal/service/sop_runtime_params.go` | 6 | 25 | 6 | 0 |
| 1841 | [ ] | `internal/service/sop_scheduler.go` | 24 | 63 | 24 | 0 |
| 1842 | [ ] | `internal/service/sop_template.go` | 23 | 64 | 23 | 0 |
| 1843 | [ ] | `internal/service/sse_hub.go` | 37 | 64 | 37 | 0 |
| 1844 | [ ] | `internal/service/sso.go` | 20 | 61 | 20 | 0 |
| 1845 | [ ] | `internal/service/sso_tenant.go` | 10 | 58 | 10 | 0 |
| 1846 | [ ] | `internal/service/subscription.go` | 26 | 54 | 26 | 0 |
| 1847 | [ ] | `internal/service/support_metrics.go` | 7 | 8 | 7 | 0 |
| 1848 | [ ] | `internal/service/system_config.go` | 8 | 15 | 8 | 0 |
| 1849 | [ ] | `internal/service/system_init.go` | 4 | 14 | 4 | 0 |
| 1850 | [ ] | `internal/service/system_monitor.go` | 0 | 0 | 0 | 0 |
| 1851 | [ ] | `internal/service/system_monitor_unix.go` | 1 | 1 | 1 | 0 |
| 1852 | [ ] | `internal/service/system_monitor_windows.go` | 1 | 1 | 1 | 0 |
| 1853 | [ ] | `internal/service/system_user.go` | 29 | 77 | 29 | 0 |
| 1854 | [ ] | `internal/service/telegram_gate.go` | 84 | 266 | 84 | 0 |
| 1855 | [ ] | `internal/service/telegram_lead_miner.go` | 7 | 22 | 7 | 0 |
| 1856 | [ ] | `internal/service/telegram_lead_outreach.go` | 0 | 0 | 0 | 0 |
| 1857 | [ ] | `internal/service/telegram_media.go` | 10 | 41 | 10 | 0 |
| 1858 | [ ] | `internal/service/telegram_polling.go` | 21 | 77 | 21 | 0 |
| 1859 | [ ] | `internal/service/telegram_polling_lock.go` | 9 | 51 | 9 | 0 |
| 1860 | [ ] | `internal/service/telegram_webhook_bootstrap.go` | 8 | 57 | 8 | 0 |
| 1861 | [ ] | `internal/service/tenant.go` | 44 | 108 | 44 | 0 |
| 1862 | [ ] | `internal/service/tenant_scope.go` | 2 | 5 | 2 | 0 |
| 1863 | [ ] | `internal/service/tenant_starter_kit.go` | 12 | 37 | 12 | 0 |
| 1864 | [ ] | `internal/service/tiktok_card.go` | 4 | 4 | 4 | 0 |
| 1865 | [ ] | `internal/service/tool_integration_config.go` | 3 | 10 | 3 | 0 |
| 1866 | [ ] | `internal/service/tool_ports_adapter.go` | 10 | 14 | 10 | 0 |
| 1867 | [ ] | `internal/service/trace_learning/adjuster.go` | 3 | 12 | 3 | 0 |
| 1868 | [ ] | `internal/service/trace_learning/aggregator.go` | 1 | 7 | 1 | 0 |
| 1869 | [ ] | `internal/service/trace_learning/cron.go` | 5 | 8 | 5 | 0 |
| 1870 | [ ] | `internal/service/trace_learning/evaluator.go` | 2 | 3 | 2 | 0 |
| 1871 | [ ] | `internal/service/trace_learning/insights.go` | 9 | 16 | 9 | 0 |
| 1872 | [ ] | `internal/service/trace_learning/service.go` | 5 | 5 | 5 | 0 |
| 1873 | [ ] | `internal/service/trace_learning/types.go` | 7 | 7 | 7 | 0 |
| 1874 | [ ] | `internal/service/translation/eval.go` | 7 | 19 | 7 | 0 |
| 1875 | [ ] | `internal/service/translation/fallback_bridge.go` | 19 | 69 | 19 | 0 |
| 1876 | [ ] | `internal/service/translation/fewshot.go` | 8 | 46 | 8 | 0 |
| 1877 | [ ] | `internal/service/translation/glossary.go` | 15 | 56 | 15 | 0 |
| 1878 | [ ] | `internal/service/translation/glossary_mapper.go` | 3 | 4 | 3 | 0 |
| 1879 | [ ] | `internal/service/translation/lang_config_resolver.go` | 9 | 37 | 9 | 0 |
| 1880 | [ ] | `internal/service/translation/post_validator.go` | 4 | 26 | 4 | 0 |
| 1881 | [ ] | `internal/service/translation/pretranslate.go` | 7 | 25 | 7 | 0 |
| 1882 | [ ] | `internal/service/translation/stats.go` | 12 | 32 | 12 | 0 |
| 1883 | [ ] | `internal/service/tuning.go` | 3 | 6 | 3 | 0 |
| 1884 | [ ] | `internal/service/typing_predict.go` | 4 | 10 | 4 | 0 |
| 1885 | [ ] | `internal/service/unified_message.go` | 4 | 4 | 4 | 0 |
| 1886 | [ ] | `internal/service/user.go` | 1 | 1 | 1 | 0 |
| 1887 | [ ] | `internal/service/webhook.go` | 40 | 143 | 40 | 0 |
| 1888 | [ ] | `internal/service/webhook_ai.go` | 14 | 29 | 14 | 0 |
| 1889 | [ ] | `internal/service/webhook_channel_douyin.go` | 21 | 78 | 21 | 0 |
| 1890 | [ ] | `internal/service/webhook_channel_feishu.go` | 12 | 41 | 12 | 0 |
| 1891 | [ ] | `internal/service/webhook_channel_qq.go` | 12 | 26 | 12 | 0 |
| 1892 | [ ] | `internal/service/webhook_channel_telegram.go` | 32 | 74 | 32 | 0 |
| 1893 | [ ] | `internal/service/webhook_channel_wecom.go` | 25 | 66 | 25 | 0 |
| 1894 | [ ] | `internal/service/webhook_channel_whatsapp.go` | 12 | 29 | 12 | 0 |
| 1895 | [ ] | `internal/service/webhook_dedup.go` | 8 | 37 | 8 | 0 |
| 1896 | [ ] | `internal/service/webhook_event_key.go` | 6 | 25 | 6 | 0 |
| 1897 | [ ] | `internal/service/webhook_inbox.go` | 1 | 2 | 1 | 0 |
| 1898 | [ ] | `internal/service/webhook_outbound.go` | 54 | 127 | 54 | 0 |
| 1899 | [ ] | `internal/service/webhook_recovery.go` | 5 | 25 | 5 | 0 |
| 1900 | [ ] | `internal/service/webhook_runtime_params.go` | 6 | 38 | 6 | 0 |
| 1901 | [ ] | `internal/service/wechat.go` | 23 | 48 | 23 | 0 |
| 1902 | [ ] | `internal/service/wechat_inbound_media.go` | 9 | 33 | 9 | 0 |
| 1903 | [ ] | `internal/service/wecom.go` | 21 | 23 | 21 | 0 |
| 1904 | [ ] | `internal/service/wecom_account_health.go` | 25 | 72 | 25 | 0 |
| 1905 | [ ] | `internal/service/wecom_integration.go` | 13 | 15 | 13 | 0 |
| 1906 | [ ] | `internal/service/whatsapp.go` | 9 | 12 | 9 | 0 |
| 1907 | [ ] | `internal/service/whatsapp_template.go` | 0 | 0 | 0 | 0 |
| 1908 | [ ] | `internal/service/whatsapp_tier.go` | 27 | 29 | 27 | 0 |
| 1909 | [ ] | `internal/service/workflow_dispatcher.go` | 8 | 12 | 8 | 0 |
| 1910 | [ ] | `internal/service/workflow_loader.go` | 2 | 2 | 2 | 0 |
| 1911 | [ ] | `internal/service/workflow_node_executor.go` | 15 | 23 | 15 | 0 |
| 1912 | [ ] | `internal/service/workflow_node_executors.go` | 11 | 18 | 11 | 0 |
| 1913 | [ ] | `internal/service/workflow_orchestrator.go` | 19 | 31 | 19 | 0 |
| 1914 | [ ] | `internal/service/ws_agent_executor.go` | 6 | 10 | 6 | 0 |
| 1915 | [ ] | `internal/service/xianyu_card.go` | 2 | 2 | 2 | 0 |
| 1916 | [ ] | `internal/service/xianyu_card_stats.go` | 2 | 2 | 2 | 0 |
| 1917 | [ ] | `internal/service/xiaohongshu_card.go` | 4 | 4 | 4 | 0 |
| 1918 | [ ] | `internal/service/xiaohongshu_card_stats.go` | 2 | 2 | 2 | 0 |
| 1919 | [ ] | `internal/storage/attachment_source.go` | 3 | 24 | 3 | 0 |
| 1920 | [ ] | `internal/storage/backup_source.go` | 2 | 19 | 2 | 0 |
| 1921 | [ ] | `internal/storage/factory.go` | 1 | 1 | 1 | 0 |
| 1922 | [ ] | `internal/storage/local.go` | 15 | 38 | 15 | 0 |
| 1923 | [ ] | `internal/storage/storage.go` | 12 | 23 | 12 | 0 |
| 1924 | [ ] | `internal/storage/storagetest/storagetest.go` | 6 | 33 | 6 | 0 |
| 1925 | [ ] | `internal/system/install/install.go` | 22 | 69 | 22 | 0 |
| 1926 | [ ] | `internal/websocket/ack_tracker.go` | 9 | 17 | 9 | 0 |
| 1927 | [ ] | `internal/websocket/envelope.go` | 3 | 5 | 3 | 0 |
| 1928 | [ ] | `internal/websocket/handler.go` | 13 | 17 | 13 | 0 |
| 1929 | [ ] | `internal/websocket/hub.go` | 27 | 35 | 27 | 0 |
| 1930 | [ ] | `internal/websocket/lang_inject.go` | 0 | 0 | 0 | 0 |
| 1931 | [ ] | `internal/websocket/notify.go` | 8 | 30 | 8 | 0 |
| 1932 | [ ] | `internal/websocket/seq.go` | 0 | 0 | 0 | 0 |
| 1933 | [ ] | `internal/websocket/seq_redis.go` | 3 | 4 | 3 | 0 |
| 1934 | [ ] | `internal/websocket/types.go` | 4 | 26 | 4 | 0 |
| 1935 | [ ] | `internal/websocket/visitor_handler.go` | 8 | 56 | 8 | 0 |
| 1936 | [ ] | `internal/websocket/visitor_registry.go` | 3 | 4 | 3 | 0 |
| 1937 | [ ] | `scripts/demo_naked_goroutine/main.go` | 0 | 0 | 0 | 0 |
| 1938 | [ ] | `scripts/integration_smoke.go` | 1 | 31 | 1 | 0 |
| 1939 | [ ] | `scripts/telegram_smoke.go` | 1 | 14 | 1 | 0 |

