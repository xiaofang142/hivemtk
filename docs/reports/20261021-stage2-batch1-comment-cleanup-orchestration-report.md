# HiveMtk SaaS 多租户全链路测试 — Stage 2（任务二：`user-server` 模块废弃变更注释清理）持续汇总报告暨专家团多智能体编排报告（Batch 1 `#1`～`#10` ～ Batch 6 `#51`～`#60`）

- **最后更新**：2026-10-21 18:21 (UTC+8)
- **所属阶段**：Stage 2 —— 任务二：`user-server` 模块废弃变更注释清理（零代码变动安全清洗）
- **当前累计进度**：已完成 **Batch 1 (`#1`～`#10`)** ～ **Batch 6 (`#51`～`#60`)**，共 **60 / 1,923** 个 Go 源码文件
- **主控依据**：[`saas-multitenant-master-test-plan.md`](file:///e:/HiveMtk/saas-multitenant-master-test-plan.md)、[`go-files-list.md`](file:///e:/HiveMtk/hivemtk-master/go-files-list.md)
- **编排模式**：`/orchestrate` + `/teamwork-preview` 多智能体协同流水线（`@orchestrator` 统筹、`@code-archaeologist` 逐块考古审计、`@backend-specialist` 后端契约把关、`@test-engineer` 隔离沙箱 AST/Token 零改动核验与编译测试）
- **P0 铁律合规状态**：✅ **主工作区 `E:\HiveMtk\hivemtk-master` 保持 0 字节 Go/Vue 业务源码改动**（全部 `.go` 注释清洗、AST/Token 校验、`git diff -U0`、`go build` / `go vet` / `go test` 均在独立隔离沙箱 Worktree 中完成并导出补丁归档，便于保存、查阅及后续统一审批合入）

---

## 一、多智能体协同编排架构与归档产物总览

### 1.1 专家团多智能体协同分工

| 智能体角色 | 会话 ID | 职责边界 | 当前累计交付成果 |
|---|---|---|---|
| **主控编排官 (`@orchestrator`)** | 当前主会话 (`b4f9ef04-28e3-4254-b48d-eb873f98a05f`) | 统筹 Stage 2 批次推进，交叉复核源码级测试与门禁依赖（[`startup_order_test.go`](file:///e:/HiveMtk/hivemtk-master/user-server/cmd/api/startup_order_test.go)、[`defaults_test.go`](file:///e:/HiveMtk/hivemtk-master/user-server/cmd/api/defaults_test.go)、[`cmd/nm-host/main_test.go`](file:///e:/HiveMtk/hivemtk-master/user-server/cmd/nm-host/main_test.go)、[`cmd/nm-host/host_config_test.go`](file:///e:/HiveMtk/hivemtk-master/user-server/cmd/nm-host/host_config_test.go)、[`active_test.go`](file:///e:/HiveMtk/hivemtk-master/user-server/internal/aiagent/agent/lifecycle/active_test.go)、[`scripts/check-deploy-claims.py`](file:///e:/HiveMtk/hivemtk-master/scripts/check-deploy-claims.py)），更新 [`go-files-list.md`](file:///e:/HiveMtk/hivemtk-master/go-files-list.md) 台账与本汇总报告 | 完成 `#1`～`#60` 复核，台账 `#1`～`#60` 标记为 `[x]` |
| **代码考古审计专家 (`stage2-comment-archaeologist`)** | [`86109bed-ba1d-4782-ae37-69d475ca072d`](conversation://86109bed-ba1d-4782-ae37-69d475ca072d) | 只读扫描 `#1`～`#60` 共 445 个注释块（台账基线 1,322 行，含 Swagger 共 1,330 行），逐块对照 6 类必删噪音（N1～N6）与 5 类必留功能注释（F1～F5）建立去留矩阵 | 产出 Batch 1～6 完整逐块考古矩阵与源码门禁防呆清单 |
| **隔离沙箱清洗与验证专家 (`stage2-isolated-sandbox-verifier`)** | [`2db241b8-b8e8-47cf-aa98-ef06b6e630f3`](conversation://2db241b8-b8e8-47cf-aa98-ef06b6e630f3) | 在独立 Git Worktree 沙箱中逐文件用编辑器精细清理注释，运行 `go/scanner` Token 流 SHA-256 比对、`git diff -U0` 逐行核验、`go build ./...` / `go vet ./...` / `go test` 验证并导出分批及累计补丁 | **60/60 文件非注释 Token 流 100% 一致（78,594 tokens 零差异）**；编译/静态检查/单测 100% PASS |

### 1.2 隔离沙箱已验证补丁与校验脚本归档清单

| 产物类别 | 文件路径 | 覆盖范围 | 核验状态 |
|---|---|---|---|
| **Batch 1 独立补丁** | [`stage2_batch1_files1_to_10_verified.patch`](file:///C:/Users/Administrator/.gemini/antigravity/brain/b4f9ef04-28e3-4254-b48d-eb873f98a05f/scratch/stage2_batch1_files1_to_10_verified.patch) | `#1` ～ `#10`（7 个文件有注释改动，3 个无改动） | ✅ Token 流 100% 一致 · 编译/单测 PASS |
| **Batch 2 独立补丁** | [`stage2_batch2_files11_to_20_verified.patch`](file:///C:/Users/Administrator/.gemini/antigravity/brain/b4f9ef04-28e3-4254-b48d-eb873f98a05f/scratch/stage2_batch2_files11_to_20_verified.patch) | `#11` ～ `#20`（5 个文件有注释改动，5 个无改动） | ✅ Token 流 100% 一致 · 编译/单测 PASS |
| **Batch 3 独立补丁** | [`stage2_batch3_files21_to_30_verified.patch`](file:///C:/Users/Administrator/.gemini/antigravity/brain/b4f9ef04-28e3-4254-b48d-eb873f98a05f/scratch/stage2_batch3_files21_to_30_verified.patch) | `#21` ～ `#30`（7 个文件有注释改动，3 个无改动） | ✅ Token 流 100% 一致 · 编译/单测 PASS |
| **Batch 4 独立补丁** | [`stage2_batch4_files31_to_40_verified.patch`](file:///C:/Users/Administrator/.gemini/antigravity/brain/b4f9ef04-28e3-4254-b48d-eb873f98a05f/scratch/stage2_batch4_files31_to_40_verified.patch) | `#31` ～ `#40`（10 个文件全部完成注释净化） | ✅ Token 流 100% 一致 · 编译/单测 PASS |
| **Batch 5 独立补丁** | [`stage2_batch5_files41_to_50_verified.patch`](file:///C:/Users/Administrator/.gemini/antigravity/brain/b4f9ef04-28e3-4254-b48d-eb873f98a05f/scratch/stage2_batch5_files41_to_50_verified.patch) | `#41` ～ `#50`（10 个文件全部完成注释净化） | ✅ Token 流 100% 一致 · 编译/单测 PASS |
| **Batch 6 独立补丁** | [`stage2_batch6_files51_to_60_verified.patch`](file:///C:/Users/Administrator/.gemini/antigravity/brain/b4f9ef04-28e3-4254-b48d-eb873f98a05f/scratch/stage2_batch6_files51_to_60_verified.patch) | `#51` ～ `#60`（10 个文件全部完成注释净化） | ✅ Token 流 100% 一致 · 编译/单测 PASS |
| **Stage 2 累计合集补丁 (`#1`～`#30`)** | [`stage2_cumulative_files1_to_30_verified.patch`](file:///C:/Users/Administrator/.gemini/antigravity/brain/b4f9ef04-28e3-4254-b48d-eb873f98a05f/scratch/stage2_cumulative_files1_to_30_verified.patch) | `#1` ～ `#30` 全量累计（`19 files changed, 93 insertions(+), 341 deletions(-)`） | ✅ 历史归档 |
| **Stage 2 累计合集补丁 (`#1`～`#40`)** | [`stage2_cumulative_files1_to_40_verified.patch`](file:///C:/Users/Administrator/.gemini/antigravity/brain/b4f9ef04-28e3-4254-b48d-eb873f98a05f/scratch/stage2_cumulative_files1_to_40_verified.patch) | `#1` ～ `#40` 全量累计（`29 files changed, 148 insertions(+), 522 deletions(-)`） | ✅ 历史归档 |
| **Stage 2 累计合集补丁 (`#1`～`#50`)** | [`stage2_cumulative_files1_to_50_verified.patch`](file:///C:/Users/Administrator/.gemini/antigravity/brain/b4f9ef04-28e3-4254-b48d-eb873f98a05f/scratch/stage2_cumulative_files1_to_50_verified.patch) | `#1` ～ `#50` 全量累计（`39 files changed, 180 insertions(+), 673 deletions(-)`） | ✅ 历史归档 |
| **Stage 2 累计合集补丁 (`#1`～`#60`)** | [`stage2_cumulative_files1_to_60_verified.patch`](file:///C:/Users/Administrator/.gemini/antigravity/brain/b4f9ef04-28e3-4254-b48d-eb873f98a05f/scratch/stage2_cumulative_files1_to_60_verified.patch) | `#1` ～ `#60` 全量累计（`49 files changed, 216 insertions(+), 842 deletions(-)`） | ✅ 一键审批合入就绪 |
| **Batch 1 Token 校验器** | [`verify_tokens.go`](file:///C:/Users/Administrator/.gemini/antigravity/brain/b4f9ef04-28e3-4254-b48d-eb873f98a05f/scratch/verify_tokens.go) | `#1` ～ `#10` (`12,388` tokens) | ✅ SHA-256 10/10 匹配 |
| **Batch 2 Token 校验器** | [`verify_tokens_batch2.go`](file:///C:/Users/Administrator/.gemini/antigravity/brain/b4f9ef04-28e3-4254-b48d-eb873f98a05f/scratch/verify_tokens_batch2.go) | `#11` ～ `#20` (`22,810` tokens) | ✅ SHA-256 10/10 匹配 |
| **Batch 3 Token 校验器** | [`verify_tokens_batch3.go`](file:///C:/Users/Administrator/.gemini/antigravity/brain/b4f9ef04-28e3-4254-b48d-eb873f98a05f/scratch/verify_tokens_batch3.go) | `#21` ～ `#30` (`23,601` tokens) | ✅ SHA-256 10/10 匹配 |
| **Batch 4 Token 校验器** | [`verify_tokens_batch4.go`](file:///C:/Users/Administrator/.gemini/antigravity/brain/b4f9ef04-28e3-4254-b48d-eb873f98a05f/scratch/verify_tokens_batch4.go) | `#31` ～ `#40` (`2,070` tokens) | ✅ SHA-256 10/10 匹配 |
| **Batch 5 Token 校验器** | [`verify_tokens_batch5.go`](file:///C:/Users/Administrator/.gemini/antigravity/brain/b4f9ef04-28e3-4254-b48d-eb873f98a05f/scratch/verify_tokens_batch5.go) | `#41` ～ `#50` (`8,162` tokens) | ✅ SHA-256 10/10 匹配 |
| **Batch 6 Token 校验器** | [`verify_tokens_batch6.go`](file:///C:/Users/Administrator/.gemini/antigravity/brain/b4f9ef04-28e3-4254-b48d-eb873f98a05f/scratch/verify_tokens_batch6.go) | `#51` ～ `#60` (`9,563` tokens) | ✅ SHA-256 10/10 匹配 |

### 1.3 阶段累计指标汇总表（`#1`～`#60`）

| 批次 | 文件范围 | 注释块（清理前 → 清理后） | 注释行（清理前 → 清理后） | 净删噪音行数 | 非注释代码 Token 数 | Token 流 SHA-256 一致率 | `go build` / `go vet` / `go test` |
|---|---|---|---|---:|---:|---|---|
| **Batch 1** | `#1` ～ `#10` (`cmd/api`..`cmd/importfaq`) | `64 块` → `45 块` | `309 行` (含 Swagger `317 行`) → `154 行` | `-163 行` (`-51.4%`) | `12,388` == `12,388` | **10 / 10 (100%)** | **100% PASS** (`13/13` tests) |
| **Batch 2** | `#11` ～ `#20` (`cmd/minttoken`..`cmd/seed/seed_customers.go`) | `71 块` → `68 块` | `229 行` → `202 行` | `-27 行` (`-11.8%`) | `22,810` == `22,810` | **10 / 10 (100%)** | **100% PASS** (`14/14` unit tests) |
| **Batch 3** | `#21` ～ `#30` (`cmd/seed/seed_faq_sop.go`..`cmd/seedkw/main.go`) | `72 块` → `62 块` | `196 行` → `139 行` | `-57 行` (`-29.1%`) | `23,601` == `23,601` | **10 / 10 (100%)** | **100% PASS** (`cmd/seed` unit tests) |
| **Batch 4** | `#31` ～ `#40` (`cmd/syncscripts`..`portcontract/logistics.go`) | `62 块` → `52 块` | `219 行` → `93 行` | `-126 行` (`-57.5%`) | `2,070` == `2,070` | **10 / 10 (100%)** | **100% PASS** (`16/16` unit tests) |
| **Batch 5** | `#41` ～ `#50` (`portcontract/order.go`..`runtime/gatekeeper_stage.go`) | `84 块` → `40 块` | `172 行` → `53 行` | `-119 行` (`-69.2%`) | `8,162` == `8,162` | **10 / 10 (100%)** | **100% PASS** (`113/113` tests/subtests) |
| **Batch 6** | `#51` ～ `#60` (`runtime/global.go`..`runtime/publisher.go`) | `92 块` → `45 块` | `197 行` → `64 行` | `-133 行` (`-67.5%`) | `9,563` == `9,563` | **10 / 10 (100%)** | **100% PASS** (`112/112` tests/subtests) |
| **累计总计** | **`#1` ～ `#60`（共 60 个文件）** | **`445 块` → `312 块`** | **`1,322 行` (`1,330 行`) → `705 行`** | **`-625 行` (`-47.0%`)** | **`78,594` == `78,594`** | **60 / 60 (100%)** | **100% PASS（零错误零警告）** |

---

## 二、【Stage 2 注释清理批次交接卡 —— Batch 1（`#1`～`#10`）】

### 2.1 Batch 1 逐文件清理明细与 Token 零改动核验表

| # | 文件路径 | 清理前注释块 / 行数 | 清理后注释块 / 行数 | 非注释 Token 数 | Token 流 SHA-256（清理前 == 清理后） | 删除的 6 类噪音摘要（N1～N6） | 保留的 5 类核心功能注释摘要（F1～F5） |
|---:|---|---|---|---|---|---|---|
| **1** | [`cmd/api/main.go`](file:///e:/HiveMtk/hivemtk-master/user-server/cmd/api/main.go) | `43 块` / `152 行`（含 Swagger `160 行`） | `25 块` / `56 行` | `4,209` | `9eb4356d754860c5e2ab085271ab9a00bd1c0a37cc7044a75933349f4f20fa89` ✅ | 删除 `001`、`003 活体联调`、`W-6`、`W-2`、`T-P1-02`、`W-5`、`T-P3-07`、`W-1`、`T-P7-03`、`T-P1-01`、`阶段二 2a/2c`、`阶段三 3.1 P1`、`R2`、`F3`、`OPT-SEC-08`、`FR-5.6` 等批次号；删除 14 处重复的 `Wire*ConfigParams` 注释块；删除“原来写死 59 defaults”、“曾经就放在这里”、“那会让测试 fixture 全红”、“TestCollectionJobMountedAfterRouterSetup 把这两件事钉成断言”等门禁/历史叙事 | **F5**: 完整保留 Swagger `@title`～`@description` 9 行注解；<br>**F1**: 保留 `DefaultListenPort` 派生契约、`TENANT_MODE` 必须在 `db.InitDB`/`router.Setup` 前初始化且 fail-closed、工具审计/Telegram/GateSweeper 的 LIFO `defer` 停机顺序、`InstallPlatformUsageSink` → `LoadPlatform` → `InitSync` → `SyncPlatformProviders` → `ReloadProvidersFromDB` 时序契约、`InitAgentCheckpointStore` 与 `StartModuleSync` 必须在 `router.Setup` 前、`InitRecoveryWorker` 与 `InitCollectionRuntime` 必须在 `router.Setup`（`InitGlobalToolExecutor`）后挂载外发审批闸门；<br>**F2**: 保留 `tenant.WithSystemScope` 启动期迁移豁免原因、`msg_hourly_summary` 唯一写入方说明 |
| **2** | [`cmd/api/serve_unix.go`](file:///e:/HiveMtk/hivemtk-master/user-server/cmd/api/serve_unix.go) | `4 块` / `14 行` | `4 块` / `9 行` | `77` | `00fa916d21493e08deca85a5018d36f45e1cc7461591d46e8fa3d780c326a871` ✅ | 删除 `F2（批5e 真机实测更正）：kill -TERM 对卡住的旧实例慢，根因不是「没有 os/signal」。真机踩到的是...` 历史踩坑叙事 | **F5**: 保留 `//go:build unix`；<br>**F2**: 保留 `endless` 下被 Hijack 的 WS/SSE 不触发 `WaitGroup.Done` 导致 `DefaultHammerTime` 双跑窗口及 `15s` 缩短契约（含 `USR2`/`HUP` 信号区别）、不可直接调 `srv.Serve(ln)`（否则 `EndlessListener` 断言 panic）；<br>**F1**: 保留 `ReadHeaderTimeout=10s`/`IdleTimeout=120s` 且不设全局 `ReadTimeout`/`WriteTimeout` 的 SSE/大上传保护契约 |
| **3** | [`cmd/api/serve_windows.go`](file:///e:/HiveMtk/hivemtk-master/user-server/cmd/api/serve_windows.go) | `2 块` / `4 行` | `2 块` / `4 行` | `69` | `af201ff73fcf5754eb4a3488707dd1c40c03ef9811d3782c648215a0ca94cbaf` ✅ | 无噪音注释（0 行删除） | **F5**: 保留 `//go:build windows`；<br>**F1**: 保留与 Unix 端对齐的 `ReadHeaderTimeout` / `IdleTimeout` 契约说明 |
| **4** | [`cmd/bridge-mock/main.go`](file:///e:/HiveMtk/hivemtk-master/user-server/cmd/bridge-mock/main.go) | `1 块` / `25 行` | `1 块` / `25 行` | `1,482` | `5b5e41289bf56a3b511621d59a8e61e4b8a5b9d3b8e4f91d8c80205c6f89f87c` ✅ | 无噪音注释（0 行删除） | **F3**: 完整保留包级 CLI 使用说明与 3 类联调测试 `curl` 命令示例（长轮询阻塞/唤醒、入站去重幂等、登录态上报） |
| **5** | [`cmd/embedding-server/main.go`](file:///e:/HiveMtk/hivemtk-master/user-server/cmd/embedding-server/main.go) | `7 块` / `33 行` | `7 块` / `21 行` | `1,547` | `ddaaef109a9a7044e8599ef27e467dc14c21506e0604bf31a928ab9575953c7c` ✅ | 删除“用于替代無法拉取的 ghcr.io TEI 镜像”历史背景及已过时的“768 维 float32”性能跑分描述（当前代码实际默认 `bge-m3` 1024 维） | **F3**: 保留 OpenAI 兼容 `/v1/embeddings` 协议请求/响应格式、`EMBEDDING_PORT=8208` 启动方式及 5 个结构体导出注释；<br>**F2**: 保留 `n-gram TF-IDF + 确定性随机投影` 算法说明与 `Usage` 按 rune 数估算的语义说明 |
| **6** | [`cmd/eval-trace/main.go`](file:///e:/HiveMtk/hivemtk-master/user-server/cmd/eval-trace/main.go) | `0 块` / `0 行` | `0 块` / `0 行` | `344` | `99113d2a2f1f24a72d77d4f6216521b567e19376a3bb81a849fb6fa524902263` ✅ | 原文件无注释（0 行删除） | 原文件无注释，直接核验通过 |
| **7** | [`cmd/gen-module-bindings/main.go`](file:///e:/HiveMtk/hivemtk-master/user-server/cmd/gen-module-bindings/main.go) | `3 块` / `36 行` | `3 块` / `11 行` | `720` | `95843b86f719d3c9b9204543fb4a70a6bc9b18caaa25da3e5b7d5324e1c2b21c` ✅ | 删除“⚠️ 这条路径错了两次，两次命令都打印了‘已生成’。记在这里免得第三次”、“为什么不用相对路径：这条路径错了三次...”等元叙述与测试门禁说明 | **F1/F3**: 保留 `SystemMenuTree` 唯一事实源单向导出契约、`go run ./cmd/gen-module-bindings` 用法与默认输出路径；<br>**F2**: 保留“输出目录不可命名为 `platform-server/seed/`，以免与 `go build ./cmd/seed` 产物同名冲突”的关键事实及 `defaultOutPath` / `isWorkspaceRoot` 函数职责注释 |
| **8** | [`cmd/gen-module-route-patterns/main.go`](file:///e:/HiveMtk/hivemtk-master/user-server/cmd/gen-module-route-patterns/main.go) | `2 块` / `22 行` | `2 块` / `8 行` | `493` | `a6f2c40f1d1f960fe0eb3fa195467262bd712bf9551e3410542c83697254a020` ✅ | 删除“且 middleware 侧的测试会按本表的计数与内容核对”门禁注释及“此前的做法（for _, mk := range cap.MenuKeys { 取第一个命中 }）在 14 条跨模块共用读接口上会闭着眼睛取第一个”历史叙事 | **F1/F3**: 保留 `API 路径形状 → 菜单 key → 模块 key` 两跳推导契约、生成物路径与命令用法；<br>**F4**: 保留跨模块共用读接口归属歧义消解规则（仅 `len(distinct) == 1` 唯一模块时才输出）；<br>**代码保护**: 第 103～126 行 `const header = ...` 原始字符串字面量 100% 原样未动 |
| **9** | [`cmd/geo-run/main.go`](file:///e:/HiveMtk/hivemtk-master/user-server/cmd/geo-run/main.go) | `1 块` / `1 行` | `0 块` / `0 行` | `2,936` | `d533ae1d4bf99e9ae7ec4a28ef0e719be1f2797e87872a769755a560b443fd3c` ✅ | 删除第 66 行唯一历史变更标记 `// ⬇️ 新增 GEO v2 全链路模型` | 无其余注释 |
| **10** | [`cmd/importfaq/main.go`](file:///e:/HiveMtk/hivemtk-master/user-server/cmd/importfaq/main.go) | `1 块` / `22 行` | `1 块` / `20 行` | `511` | `065f221c1413c2bb078cb9b311350a477757db29b25c185fc34e03f2b67dcab5` ✅ | 删除头部 `// 设计依据: AI 智能体性能优化` 批次依据标签及紧随的空注释行 | **F1/F3**: 完整保留包级 CLI 职责说明（Markdown FAQ 解析、按 `question` 唯一性幂等写入 `ai_knowledge_faq`、Embedding 同步生成、`-input` / `-dry-run` 用法示例） |

---

## 三、【Stage 2 注释清理批次交接卡 —— Batch 2（`#11`～`#20`）】

### 3.1 Batch 2 逐文件清理明细与 Token 零改动核验表

| # | 文件路径 | 清理前注释块 / 行数 | 清理后注释块 / 行数 | 非注释 Token 数 | Token 流 SHA-256（清理前 == 清理后） | 删除的 6 类噪音摘要（N1～N6） | 保留的 5 类核心功能注释摘要（F1～F5） |
|---:|---|---|---|---|---|---|---|
| **11** | [`cmd/minttoken/main.go`](file:///e:/HiveMtk/hivemtk-master/user-server/cmd/minttoken/main.go) | `0 块` / `0 行` | `0 块` / `0 行` | `63` | `e27a5d67678b5218...` ✅ | 原文件无注释（0 行删除） | 原文件无注释，直接核验通过 |
| **12** | [`cmd/nm-host/main.go`](file:///e:/HiveMtk/hivemtk-master/user-server/cmd/nm-host/main.go) | `45 块` / `102 行` | `45 块` / `82 行` | `2,428` | `c9a02237330f9205...` ✅ | 删除 `（设计文档 §5）`、`R17`、`F1（真机踩坑登记）`、`session253`、`A5`、`R27-2` 等批次/文档代号（N2）；删除“旧注释与旧防御值按「4GB」写，那是文档误传”、“原为内联 10s，收口成常量便于归因与变异”、“旧实现的问题正在于「先按 length 建大缓冲再判断」”、“旧版 main() 完全不读 os.Args...”、“旧实现把它拼进 addr 再整条打印...”、“修正：旧实现把读泵开在 pumpLoop 里...”、“真机腿实测回包归因有、stderr 无痕”等历史变更叙事（N1/N3/N5） | **F1/F4**: 保留 Chrome Native Messaging 4 字节小端头帧协议与三档尺寸契约（出帧硬限 `1MiB`、入帧业务上限 `4MiB` 对齐服务端 `hostFrameReadLimit`、Chrome 协议流损坏上限 `64MiB`）、`readNativeFrame` 三出口契约、`startStdinPump` 进程级唯一读泵并发不变量、`writeMu` 写串行化与 `closeAndJoin` 停泵不变量；<br>**F2**: 保留 `connectNative` `argv[1]="chrome-extension://<id>/"` 启动来源校验原因、`tokenFingerprint` SHA-256 前 8 位防凭据落盘、`__host_shutdown__` 自愈换进程协议语义 |
| **13** | [`cmd/perf/main.go`](file:///e:/HiveMtk/hivemtk-master/user-server/cmd/perf/main.go) | `1 块` / `13 行` | `1 块` / `13 行` | `569` | `cdcc3b38c253d90a...` ✅ | 无噪音注释（0 行删除） | **F1/F3**: 完整保留压测入口 CLI 用法（`-base`、`-scene`）与安全护栏契约（`PERF_USERNAME`/`PERF_PASSWORD` 必须显式传入，禁止弱口令默认值） |
| **14** | [`cmd/pwtool/main.go`](file:///e:/HiveMtk/hivemtk-master/user-server/cmd/pwtool/main.go) | `3 块` / `8 行` | `3 块` / `8 行` | `169` | `9805afc937d7411c...` ✅ | 无噪音注释（0 行删除） | **F1/F2/F3**: 完整保留 CLI 用法、`publicDemoPassword` 与 `cmd/seed/seed_users.go` `seedPasswordDefault` 同值契约、环境变量 `TrimSpace` 口径对齐说明 |
| **15** | [`cmd/routeinspect/main.go`](file:///e:/HiveMtk/hivemtk-master/user-server/cmd/routeinspect/main.go) | `0 块` / `0 行` | `0 块` / `0 行` | `219` | `8d06b57fd58ef30d...` ✅ | 原文件无注释（0 行删除） | 原文件无注释，直接核验通过 |
| **16** | [`cmd/seed/main.go`](file:///e:/HiveMtk/hivemtk-master/user-server/cmd/seed/main.go) | `6 块` / `47 行` | `6 块` / `43 行` | `1,097` | `69608173b3553998...` ✅ | 删除 `allSeeders` 上方“已实现 users/...；新增 faq_sop...全部 11 个模块均已就绪”的历史进度播报与重复罗列（N1/N6，共 4 行） | **F1/F2/F3**: 保留包级 CLI 用法（`--clean`、`--module`、`--list`）、三条种子写入路径分界（特别是 `031`～`033.sql` 无自动执行路径须手工 `psql` 执行的不可推导事实）、外键依赖顺序（`A→K`）、`Seeder`/`SeedContext` 契约及 `initDB` 独立初始化原因 |
| **17** | [`cmd/seed/seed_ai_agents.go`](file:///e:/HiveMtk/hivemtk-master/user-server/cmd/seed/seed_ai_agents.go) | `3 块` / `23 行` | `3 块` / `23 行` | `9,945` | `3363ea49a03a300b...` ✅ | 无噪音注释（0 行删除） | **F1/F2/F3**: 保留模块 D 15 张表覆盖清单、`ensurePlatformCSRagProduct` 必须在写智能体前执行的时序契约、`platformCSProductID` 与 `031_platform_cs_rag_seed.sql` 及 `scripts/seed/*.py` 的跨文件一致性契约 |
| **18** | [`cmd/seed/seed_assets.go`](file:///e:/HiveMtk/hivemtk-master/user-server/cmd/seed/seed_assets.go) | `2 块` / `11 行` | `1 块` / `10 行` | `4,051` | `c3541c4a3d0a700c...` ✅ | 删除第 636 行 `// 防止未使用 import 警告`（重复 `var _ = json.Marshal` 惯用法语义，N6） | **F3**: 保留模块 G 7 张内容资产表覆盖清单 |
| **19** | [`cmd/seed/seed_clues.go`](file:///e:/HiveMtk/hivemtk-master/user-server/cmd/seed/seed_clues.go) | `2 块` / `7 行` | `1 块` / `6 行` | `1,307` | `c694f58ce371d6ca...` ✅ | 删除第 125 行 `// 5 个等级按位置分布：前 6 个 S，6-12 A...`（完全重复下方 `switch` 分支代码语义，N6） | **F3**: 保留模块 F 3 张线索营销表覆盖清单 |
| **20** | [`cmd/seed/seed_customers.go`](file:///e:/HiveMtk/hivemtk-master/user-server/cmd/seed/seed_customers.go) | `9 块` / `18 行` | `8 块` / `17 行` | `2,962` | `3bf29fdd85c8a24a...` ✅ | 删除第 357 行 `// randInt64 生成 64 位随机整数`（重复私有辅助函数名，N6） | **F2/F3**: 保留模块 B 6 张客户中心 CDP 表覆盖清单、50 名客户的 5 渠道下标切片分布说明及各构建函数职责注释 |

---

## 四、【Stage 2 注释清理批次交接卡 —— Batch 3（`#21`～`#30`）】

### 4.1 Batch 3 逐文件清理明细与 Token 零改动核验表

| # | 文件路径 | 清理前注释块 / 行数 | 清理后注释块 / 行数 | 非注释 Token 数 | Token 流 SHA-256（清理前 == 清理后） | 删除的 6 类噪音摘要（N1～N6） | 保留的 5 类核心功能注释摘要（F1～F5） |
|---:|---|---|---|---|---|---|---|
| **21** | [`cmd/seed/seed_faq_sop.go`](file:///e:/HiveMtk/hivemtk-master/user-server/cmd/seed/seed_faq_sop.go) | `19 块` / `41 行` | `19 块` / `31 行` | `7,023` | `5b702efd12e931a5...` ✅ | 删除头部 `// 设计依据： AI 智能体性能优化` 标签（N2）；去除 `faqUrgency`、`faqSupport`、`sopUrgencyStage`、`sopSupportStage` 4 处冗余的 `// ===...` 装饰横幅（共 8 行）；去除 `sopRepurchaseStage` 中的 `(T-P7-04)` 工单代号（N2） | **F1/F2/F3**: 保留模块 D2 表覆盖清单（`faq_entries` / `sop_templates`）、Layer1 命中后 `SkipLLM` 双层架构与 `intent × stage` 维度说明、各分类/阶段构造函数职责注释，以及 `sopRepurchaseStage` 与跟进调度器 `SOPName=repurchase_reminder` 的跨模块同名对齐契约 |
| **22** | [`cmd/seed/seed_helpers.go`](file:///e:/HiveMtk/hivemtk-master/user-server/cmd/seed/seed_helpers.go) | `15 块` / `32 行` | `15 块` / `24 行` | `721` | `134eee981b90d704...` ✅ | 删除头部已过时的 `SourceOfSeed="seed_demo"` 历史标记描述及 `batchInsert` 中已失效的旧包路径叙述（N1，共净删 8 行） | **F1/F2**: 保留 `seedTag = "[seed-demo]"` 幂等清理契约、`seededRand` 固定种子可复现契约、`zeroVectorString` 对 `NOT NULL vector(1024)` 的占位说明、`batchInsert` 默认 `Omit("embedding")` 与 `batchInsertWithEmbedding` 的配对契约、以及 `cleanByCondition` 使用 `Unscoped()` 物理删除防唯一约束冲突的不变量说明 |
| **23** | [`cmd/seed/seed_llm_logs.go`](file:///e:/HiveMtk/hivemtk-master/user-server/cmd/seed/seed_llm_logs.go) | `9 块` / `19 行` | `6 块` / `16 行` | `2,686` | `5bfec5732b6bf024...` ✅ | 删除 `（来自 project_memory）` 元引用（2 处，N5）、`// 检查基础表是否已建立`（N6）、`// 检查扩展字段是否存在（v3.7.0 扩展迁移）`（N1/N2/N6）、`// 防止 time 未使用警告（time 在 future 扩展中使用）`（N5/N6） | **F1/F2/F3**: 保留模块 I 覆盖表清单（含 `llm_routing_logs` 无 GORM 模型走原生 SQL 的事实）、`trace_id`/`scenario`/`model_type`/`token_source` 关键约束、收集最新 `id` 写入 `ctx.LLMRoutingLogIDs` 供后续统计模块引用的说明 |
| **24** | [`cmd/seed/seed_messages.go`](file:///e:/HiveMtk/hivemtk-master/user-server/cmd/seed/seed_messages.go) | `2 块` / `8 行` | `1 块` / `7 行` | `2,372` | `04940987b5ddde6c...` ✅ | 删除第 332 行 `// 防止未使用 import 警告（json 在某些扩展场景使用）`（N5/N6） | **F3**: 完整保留模块 H 4 张统一消息表覆盖清单 |
| **25** | [`cmd/seed/seed_rag_product.go`](file:///e:/HiveMtk/hivemtk-master/user-server/cmd/seed/seed_rag_product.go) | `5 块` / `23 行` | `5 块` / `10 行` | `260` | `f619521985ceba83...` ✅ | 删除“而在本文件之前，这一行的唯一产码是...结果是：按常规路径装完的库里，智能体绑着一个查无此物的产品...”历史叙事（N1）及“由 scripts/check-deploy-claims.py 规则 8 对着 031 校准”门禁注释（N3） | **F1/F2/F4**: 保留本文件与 `migrations/031_platform_cs_rag_seed.sql` 的职责分界与同源字段契约、`platformCSProductID` / `platformCSVectorTable` / `platformCSRagProduct` 跨文件常量契约、以及 `ensurePlatformCSRagProduct` 不覆盖运行期统计列（`doc_count`/`chunk_count`/`search_count`）且 `Clean` 阶段不删行的关键不变量 |
| **26** | [`cmd/seed/seed_reach.go`](file:///e:/HiveMtk/hivemtk-master/user-server/cmd/seed/seed_reach.go) | `1 块` / `15 行` | `1 块` / `15 行` | `4,972` | `05dc04b901b22bf7...` ✅ | 无噪音注释（0 行删除） | **F3**: 完整保留模块 E 12 张触达运营表覆盖清单 |
| **27** | [`cmd/seed/seed_sessions.go`](file:///e:/HiveMtk/hivemtk-master/user-server/cmd/seed/seed_sessions.go) | `6 块` / `6 行` | `6 块` / `6 行` | `2,446` | `1e26288a4c968d6f...` ✅ | 无噪音注释（0 行删除） | **F3**: 保留模块 C 头部标识及 5 个子表构造函数的数据规模摘要（30 会话 / 10-30 消息 / AI 建议 / 3 黑名单 / 18 快捷回复） |
| **28** | [`cmd/seed/seed_stats.go`](file:///e:/HiveMtk/hivemtk-master/user-server/cmd/seed/seed_stats.go) | `9 块` / `27 行` | `4 块` / `10 行` | `1,827` | `bf7ee45a255d8667...` ✅ | 删除 `R-4 点名的僵尸表` / `R-4 要的是能辨真伪` 批次代号与长篇议论（N2/N4）；删除第 72 行与第 127 行元叙述注释（N5）；删除 `// 引入 ±10% 浮动`、`toAnySlice`、`// 防止 time 未使用警告`（N6）；**原样保留第 73～74 行 `log.Printf` 代码字面量一字未动** | **F2/F3**: 保留模块 J 3 张表清单及 `conversion_funnels` 全仓无读路径（`GET /api/conversion-funnel` 走 `internal/ops/service` 实时聚合，真实阶段词表见 `ops/repository.FunnelStageKey`）的不可推导架构事实 |
| **29** | [`cmd/seed/seed_users.go`](file:///e:/HiveMtk/hivemtk-master/user-server/cmd/seed/seed_users.go) | `6 块` / `25 行` | `5 块` / `20 行` | `1,093` | `5afdddb890ec79be...` ✅ | 删除“关键修复：Clean 阶段不再使用...原实现会把安装向导创建的真实超管一并删掉，引发反复失效问题”（N1）、“也是本常量的历史唯一取值”（N1）及末尾 `// 编译期确保 time 包被引用`（N6） | **F1/F2/F3**: 保留模块 A 演示账号清单、`Clean` 阶段仅按 `[seed-demo]` / 演示手机号段 / `@hivemtk.demo` 清理以防误删真实 `admin` 超管的安全边界、`seedPasswordDefault` / `seedPassword` 优先级契约与 `seedPasswordForLog` 掩码脱敏规则 |
| **30** | [`cmd/seedkw/main.go`](file:///e:/HiveMtk/hivemtk-master/user-server/cmd/seedkw/main.go) | `0 块` / `0 行` | `0 块` / `0 行` | `201` | `4f9c751d716e6e33...` ✅ | 原文件无注释（0 行删除） | 原文件无注释，直接核验通过 |

### 4.2 Batch 3 AST/Token 零代码改动核验输出（`verify_tokens_batch3.go`）

```text
======================================================================================================
File                                   | Blocks (B->A)   | Lines (B->A)    | Tokens     | Non-Comment Token SHA256 (Before == After)
------------------------------------------------------------------------------------------------------
#21 cmd/seed/seed_faq_sop.go           | 19 -> 19        | 41 -> 31        | 7023       | 5b702efd12e931a5... (IDENTICAL)
#22 cmd/seed/seed_helpers.go           | 15 -> 15        | 32 -> 24        | 721        | 134eee981b90d704... (IDENTICAL)
#23 cmd/seed/seed_llm_logs.go          | 9 -> 6          | 19 -> 16        | 2686       | 5bfec5732b6bf024... (IDENTICAL)
#24 cmd/seed/seed_messages.go          | 2 -> 1          | 8 -> 7          | 2372       | 04940987b5ddde6c... (IDENTICAL)
#25 cmd/seed/seed_rag_product.go       | 5 -> 5          | 23 -> 10        | 260        | f619521985ceba83... (IDENTICAL)
#26 cmd/seed/seed_reach.go             | 1 -> 1          | 15 -> 15        | 4972       | 05dc04b901b22bf7... (IDENTICAL)
#27 cmd/seed/seed_sessions.go          | 6 -> 6          | 6 -> 6          | 2446       | 1e26288a4c968d6f... (IDENTICAL)
#28 cmd/seed/seed_stats.go             | 9 -> 4          | 27 -> 10        | 1827       | bf7ee45a255d8667... (IDENTICAL)
#29 cmd/seed/seed_users.go             | 6 -> 5          | 25 -> 20        | 1093       | 5afdddb890ec79be... (IDENTICAL)
#30 cmd/seedkw/main.go                 | 0 -> 0          | 0 -> 0          | 201        | 4f9c751d716e6e33... (IDENTICAL)
------------------------------------------------------------------------------------------------------
TOTAL (#21..#30)                       | 72 -> 62        | 196 -> 139      | 23601      | ALL 10 FILES 100% TOKEN-IDENTICAL
======================================================================================================
```

### 4.3 Batch 3 编译、静态检查与单测输出（`golang:1.26-bookworm`）

1. **`go build ./cmd/...`**：PASS（退出码 `0`）
2. **`go vet ./cmd/...`**：PASS（退出码 `0`）
3. **`go test -v ./cmd/seed/...`**：`TestResolveSeedPassword`、`TestSeedPasswordForLog` **100% PASS (`0.004s`)**

---

## 五、【Stage 2 注释清理批次交接卡 —— Batch 4（`#31`～`#40`）】

### 5.1 Batch 4 逐文件清理明细与 Token 零改动核验表

| # | 文件路径 | 清理前注释块 / 行数 | 清理后注释块 / 行数 | 非注释 Token 数 | Token 流 SHA-256（清理前 == 清理后） | 删除的 6 类噪音摘要（N1～N6） | 保留的 5 类核心功能注释摘要（F1～F5） |
|---:|---|---|---|---|---|---|---|
| **31** | [`cmd/syncscripts/main.go`](file:///e:/HiveMtk/hivemtk-master/user-server/cmd/syncscripts/main.go) | `1 块` / `10 行` | `1 块` / `5 行` | `111` | `b0f4ccc3e97d8b4a...` ✅ | 删除 `（T-5 双体系桥接手动触发入口）` 工单代号（N2）及冗余 `cd hivemtk/user-server` 目录假设（N6） | **F1/F3**: 保留 `script_templates` 按 `(category, title)` 幂等同步至 `script_library` 的核心契约与 `go run ./cmd/syncscripts` 用法 |
| **32** | [`internal/aiagent/agent/lifecycle/active.go`](file:///e:/HiveMtk/hivemtk-master/user-server/internal/aiagent/agent/lifecycle/active.go) | `18 块` / `49 行` | `14 块` / `26 行` | `915` | `b3034aae5bc89d77...` ✅ | 删除 `C7 裁定的落法`、`T-P3-07 / T-P5-03`、`P8`、`T-P5-01`、`本卡刻意不做的事`、`见 §交付说明的欠账登记`、`逐项报错又超出本卡范围`（N1/N2/N4）；删除 `AC② 由 TestActiveNeverImportsConversationEngine 钉住`、`见 TestActiveSessionKeyStaysWithinColumn` 测试门禁注释（N3）；删除 `triggerActiveLifecycle`、2 处 `Mode 实现 AgentLifecycle`、`Run 把一次请求转成会话引擎的一轮应答` 重复代码语义注释（N6） | **F1/F2/F4**: 保留 Active 模式委托 SOP 引擎执行且严禁 import 会话引擎的架构边界、单客户选材 / 首个可解析 SOP 决策 / `one_id` 归因三步契约、`_trigger` 键名对齐、空 `OneID` 不写键防伪归因、`agent_id` 溯源键、`CustomerLookup.GetByID` 未命中返回 `(nil, nil)` 需显式判空契约、`sop_executions.session_id` `varchar(120)` 列宽约束及 `firstParsableSOPID` 跳过非数字脏数据容错契约 |
| **33** | [`internal/aiagent/agent/lifecycle/lifecycle.go`](file:///e:/HiveMtk/hivemtk-master/user-server/internal/aiagent/agent/lifecycle/lifecycle.go) | `10 块` / `38 行` | `10 块` / `16 行` | `153` | `bf61dcd87f3dcc1b...` ✅ | 删除失效 ADR 引用 `docs/architecture/adr/-agent-two-mode.md`（N1/N2）、与实现矛盾的旧版主动模式 ASCII 框图（N1/N6）、`C7 裁定` 与 `TestActiveNeverImportsConversationEngine` 测试名（N2/N3）及 `Mode` 字段注释中的“是本卡的验收条件之一”（N2/N3） | **F1/F3/F4**: 保留 Passive / Active 双模式触发源与职责分界、`AgentLifecycle` 接口、`LifecycleRequest`（`SessionID` 仅被动模式使用、`OneID` 语义）、`LifecycleResult`（`Mode` 回显防静默回退不可见、`ExecutionID`、`OneID`）及 `Resolver` 默认回退契约 |
| **34** | [`internal/aiagent/agent/portcontract/aftersale.go`](file:///e:/HiveMtk/hivemtk-master/user-server/internal/aiagent/agent/portcontract/aftersale.go) | `5 块` / `9 行` | `3 块` / `4 行` | `149` | `f838da8f6d720a9f...` ✅ | 删除 `// 售后类型`、`// AfterSaleView 售后只读视图` 自明注释（N6），精简 `AfterSaleRequest` 与 `AfterSalePort` 之间的重复说明（N6） | **F1/F2/F4**: 保留 `AfterSalePending` 本地记录且真实状态由电商回写、`AfterSaleRequest` 入参语义，以及 `AfterSalePort` 与只读 `OrderPort` 的读写职责分界契约 |
| **35** | [`internal/aiagent/agent/portcontract/asset_bundle.go`](file:///e:/HiveMtk/hivemtk-master/user-server/internal/aiagent/agent/portcontract/asset_bundle.go) | `9 块` / `36 行` | `9 块` / `15 行` | `244` | `8fc7dcc8db8712a2...` ✅ | 删除已不存在的旧文件引用 `service/chat_history_port_adapter.go` 与失真的 `Redis LPush 列表` 描述（实际为 `agent_runtime.ChatHistoryRedisAdapter` 的 `RPush`+`LRange`，N1）；删除 `KnowledgeBaseService（直接实现）` 失真描述（实际由 `service.KnowledgeSearchPortAdapter` 适配，N1）及与 `doc.go` 重复的解耦背景（N6） | **F1/F2/F3**: 保留 `ErrAssetBundleNotEnabled` / `ErrAssetBundleNotFound` 哨兵错误、`WeaveRequestPort` 省略 `Asset` 实体防运行时查库契约、以及 `AssetBundleWeavePort` / `ChatHistoryPort` / `KnowledgeSearchPort` / `LLMChatPort` 的真实实现方与消费方映射契约 |
| **36** | [`internal/aiagent/agent/portcontract/customer.go`](file:///e:/HiveMtk/hivemtk-master/user-server/internal/aiagent/agent/portcontract/customer.go) | `4 块` / `19 行` | `4 块` / `6 行` | `136` | `7ac919cc7f84eeb6...` ✅ | 删除“与历史 service.CustomerDTO 字段对齐”历史演进说明（N1）及 `CustomerProfileView` 中逐行复述结构体字段名的冗余注释（N6） | **F1/F2**: 保留 `ErrCustomerNotFound` 与 `service.ErrCustomerNotFound` 的跨层错误映射契约、`CustomerIdentity` / `CustomerProfileView` 投影语义及 `CustomerPort` 实现/消费方契约 |
| **37** | [`internal/aiagent/agent/portcontract/doc.go`](file:///e:/HiveMtk/hivemtk-master/user-server/internal/aiagent/agent/portcontract/doc.go) | `1 块` / `27 行` | `1 块` / `8 行` | `3` | `d247a8e9da88d3af...` ✅ | 删除装饰性横线与第 1、2 点重复阐述的“切断 import cycle / 单向依赖”冗余段落（N6） | **F1/F3**: 完整保留 `portcontract` 依赖反转方向（`service ──→ portcontract ←── tooluse / agent_runtime`）及启动期通过 `tool_ports_adapter.go` / `asset_bundle_port_adapter.go` 注入的装配契约 |
| **38** | [`internal/aiagent/agent/portcontract/followup.go`](file:///e:/HiveMtk/hivemtk-master/user-server/internal/aiagent/agent/portcontract/followup.go) | `2 块` / `12 行` | `2 块` / `3 行` | `93` | `3be23b91205e1e9e...` ✅ | 删除 `FollowUpScheduleOptions` 中已过时的 `Description` 字段描述（实际字段为 `Priority/Note/Title`，N1）及 `reminderID 为 string 类型` 的签名复述（N6） | **F1/F2**: 保留 `FollowUpScheduleOptions` 投影说明及 `FollowUpPort.ResultInfo` 与 `service.FollowUpResult` 枚举对齐以反查目标 SOP 阶段的核心契约 |
| **39** | [`internal/aiagent/agent/portcontract/journey_reach.go`](file:///e:/HiveMtk/hivemtk-master/user-server/internal/aiagent/agent/portcontract/journey_reach.go) | `3 块` / `9 行` | `3 块` / `4 行` | `46` | `e4de156396d6a9a6...` ✅ | 精简 `JourneyPort` 与 `ReachPipelinePort` 的冗余将来时设想铺垫（N6） | **F1/F4**: 保留 `JourneyPort` 占位接口边界、`ReachSendInput` 投影职责及 `ReachPipelinePort` 未注入时降级走 `ReachAdapter` 单次发送的回退契约 |
| **40** | [`internal/aiagent/agent/portcontract/logistics.go`](file:///e:/HiveMtk/hivemtk-master/user-server/internal/aiagent/agent/portcontract/logistics.go) | `9 块` / `10 行` | `5 块` / `6 行` | `220` | `a2977f9f5fa132f7...` ✅ | 删除 `LogisticsTrackStatus`、`LogisticsTrackRequest`、`NewNoopLogisticsPort`、`Track` 4 处字面复述类型/函数名的噪音注释（N6） | **F1/F2**: 保留 `LogisticsTrackView`（最新在前排序契约）、`LogisticsTrackResult`（本地状态 + 实时轨迹聚合）、`CourierClient`、`LogisticsPort` 及 `NoopLogisticsPort` 未注入兜底契约 |

### 5.2 Batch 4 AST/Token 零代码改动核验输出（`verify_tokens_batch4.go`）

```text
==========================================================================================================================
File                                                 | Blocks (B->A)   | Lines (B->A)    | Tokens     | Non-Comment Token SHA256 (Before == After)
--------------------------------------------------------------------------------------------------------------------------
#31 cmd/syncscripts/main.go                          | 1 -> 1          | 10 -> 5         | 111        | b0f4ccc3e97d8b4a... (IDENTICAL)
#32 internal/aiagent/agent/lifecycle/active.go       | 18 -> 14        | 49 -> 26        | 915        | b3034aae5bc89d77... (IDENTICAL)
#33 internal/aiagent/agent/lifecycle/lifecycle.go    | 10 -> 10        | 38 -> 16        | 153        | bf61dcd87f3dcc1b... (IDENTICAL)
#34 internal/aiagent/agent/portcontract/aftersale.go | 5 -> 3          | 9 -> 4          | 149        | f838da8f6d720a9f... (IDENTICAL)
#35 internal/aiagent/agent/portcontract/asset_bundle.go | 9 -> 9          | 36 -> 15        | 244        | 8fc7dcc8db8712a2... (IDENTICAL)
#36 internal/aiagent/agent/portcontract/customer.go  | 4 -> 4          | 19 -> 6         | 136        | 7ac919cc7f84eeb6... (IDENTICAL)
#37 internal/aiagent/agent/portcontract/doc.go       | 1 -> 1          | 27 -> 8         | 3          | d247a8e9da88d3af... (IDENTICAL)
#38 internal/aiagent/agent/portcontract/followup.go  | 2 -> 2          | 12 -> 3         | 93         | 3be23b91205e1e9e... (IDENTICAL)
#39 internal/aiagent/agent/portcontract/journey_reach.go | 3 -> 3          | 9 -> 4          | 46         | e4de156396d6a9a6... (IDENTICAL)
#40 internal/aiagent/agent/portcontract/logistics.go | 9 -> 5          | 10 -> 6         | 220        | a2977f9f5fa132f7... (IDENTICAL)
--------------------------------------------------------------------------------------------------------------------------
TOTAL (#31..#40)                                     | 62 -> 52        | 219 -> 93       | 2070       | ALL 10 FILES 100% TOKEN-IDENTICAL
==========================================================================================================================
```

### 5.3 Batch 4 编译、静态检查与单测输出（`golang:1.26-bookworm`）

1. **`go build ./cmd/syncscripts/... ./internal/aiagent/agent/lifecycle/... ./internal/aiagent/agent/portcontract/...`**：PASS（退出码 `0`）
2. **`go vet ./cmd/syncscripts/... ./internal/aiagent/agent/lifecycle/... ./internal/aiagent/agent/portcontract/...`**：PASS（退出码 `0`）
3. **`go test -v ./internal/aiagent/agent/lifecycle/... ./internal/aiagent/agent/portcontract/...`**：16 项顶层测试（含 `TestActiveNeverImportsConversationEngine`、`TestActiveSessionKeyStaysWithinColumn`、`TestResolver` 6 个子测试、`TestNoopLogisticsPortTrack`）**100% PASS (`0.012s`)**

---

## 六、【Stage 2 注释清理批次交接卡 —— Batch 5（`#41`～`#50`）】

### 6.1 Batch 5 逐文件清理明细与 Token 零改动核验表

| # | 文件路径 | 清理前注释块 / 行数 | 清理后注释块 / 行数 | 非注释 Token 数 | Token 流 SHA-256（清理前 == 清理后） | 删除的 6 类噪音摘要（N1～N6） | 保留的 5 类核心功能注释摘要（F1～F5） |
|---:|---|---|---|---|---|---|---|
| **41** | [`internal/aiagent/agent/portcontract/order.go`](file:///e:/HiveMtk/hivemtk-master/user-server/internal/aiagent/agent/portcontract/order.go) | `2 块` / `7 行` | `2 块` / `3 行` | `106` | `130734b812f00ef0...` ✅ | 精简 `OrderView` 与 `OrderPort` 之间重复的“只读镜像、不写商业订单”冗余说明（N6） | **F1/F3**: 保留 `OrderView` 投影职责及 `OrderPort` 只读查询外部同步订单（`model.ExternalOrder`）而不直接创建/修改商业订单的核心边界契约 |
| **42** | [`internal/aiagent/agent/portcontract/session.go`](file:///e:/HiveMtk/hivemtk-master/user-server/internal/aiagent/agent/portcontract/session.go) | `3 块` / `11 行` | `3 块` / `4 行` | `125` | `92550e6f39273184...` ✅ | 删除 `（与方向10 安全加固一致）` 批次代号（N2）及与 `doc.go` 重复的 `工具层无需 import service 包` 说明（N6） | **F1/F2**: 保留 `CreateSessionInput` / `SendMessageInput` 投影说明及 `SessionPort` 发送者身份须由服务层鉴权上下文派生、禁止工具层伪造 `SenderType` 的安全约束 |
| **43** | [`internal/aiagent/agent/portcontract/tool_result.go`](file:///e:/HiveMtk/hivemtk-master/user-server/internal/aiagent/agent/portcontract/tool_result.go) | `4 块` / `6 行` | `3 块` / `4 行` | `161` | `6d9e34be194c0c8a...` ✅ | 删除 `两侧各自定义过一次并漂移（runtime 那份缺 error_code/card）` 历史缺陷叙事（N1/N4）及 `ToJSON` 自明注释（N6） | **F1/F3**: 保留 `ToolResult` 定义在叶子包 `portcontract` 供 `tooluse` 与 `agent_runtime` 通过类型别名共享以防循环依赖的契约、`ToolTiming` 及 `WithTiming` 值语义返回副本说明 |
| **44** | [`internal/aiagent/agent/runtime/alignment_stage.go`](file:///e:/HiveMtk/hivemtk-master/user-server/internal/aiagent/agent/runtime/alignment_stage.go) | `7 块` / `9 行` | `2 块` / `2 行` | `1,082` | `3e6e545b7a0cf1cd...` ✅ | 删除 `NewDefaultAlignmentScorer`、`NewDefaultAlignmentScorerWith`、`Name`、`Execute`、`Score` 5 处复述函数名的噪音注释（N6）；修正 `IntentAppreciIntent` 前后矛盾的“把 Intent 转为 Sentiment // 实际不做转换”注释（N1） | **F1**: 保留 `DefaultAlignmentScorer` 6 维拟人度评分器说明与 `IntentAppreciIntent` 映射赞赏意图至 `IntentGreeting` 的契约说明 |
| **45** | [`internal/aiagent/agent/runtime/asset_bundle_loader.go`](file:///e:/HiveMtk/hivemtk-master/user-server/internal/aiagent/agent/runtime/asset_bundle_loader.go) | `4 块` / `13 行` | `3 块` / `4 行` | `932` | `ee18e0edca8158c2...` ✅ | 删除 `NewAssetBundleLoader` 自明注释（N6）、`（文档 §二）` 章节号（N2）及失真的 `5. 输出 AssetContext.Messages` 描述（实际提取 system 消息至 `AssetContext.PromptText`，N1） | **F1/F2**: 保留 `AssetBundleLoader`、`LoaderDefaults` 及 `LoadContext` 织布提取 `PromptText` 与未绑定/未启用时降级为空上下文的真实契约 |
| **46** | [`internal/aiagent/agent/runtime/chat_history_redis.go`](file:///e:/HiveMtk/hivemtk-master/user-server/internal/aiagent/agent/runtime/chat_history_redis.go) | `4 块` / `26 行` | `4 块` / `6 行` | `497` | `e877ce0d24afe33b...` ✅ | 删除失真的 `service 包的 tool_ports_adapter.go 通过 setter 注入` 装配描述（N1）及 `FetchHistory` / `AppendHistory` 中逐行复述函数体的冗余步骤列表（N6） | **F1/F2**: 保留 `ChatHistoryRedisAdapter`、`NewChatHistoryRedisAdapter` 默认条数/TTL 契约，以及 `FetchHistory` / `AppendHistory` 在 Redis 故障时降级不阻塞主链路的容错契约 |
| **47** | [`internal/aiagent/agent/runtime/context.go`](file:///e:/HiveMtk/hivemtk-master/user-server/internal/aiagent/agent/runtime/context.go) | `28 块` / `30 行` | `8 块` / `9 行` | `1,784` | `52de3191f7bc46cc...` ✅ | 删除 `这里曾各自定义过一份并漂移` 历史叙事（N1）及 `ToolTiming`、`Message`、`ToolCall`、`Estimate`、`NewSummarizationCompressor`、`ShouldCompress`、`EstimateTokens`、`Compress`、`ToolCallRecord`、`NewToolCallHistory`、`AddCall`、`GetCall`、`GetResult`、`GetCallsByTool`、`ToMessages`、`Clear`、`Count`、`NewEnhancedInferenceContext`、`AddToolCall`、`GetCompressedHistory` 共 20 处复述类型/方法名的噪音注释（N6） | **F1/F2**: 保留 `ToolResult` 跨层统一结构别名说明、`ContextCompressor`、`TokenEstimator`、`SimpleTokenEstimator`（中英文字符 Token 估算比值规则）、`LLMClient`、`SummarizationCompressor`、`ToolCallHistory`、`EnhancedInferenceContext` 核心类型契约 |
| **48** | [`internal/aiagent/agent/runtime/dataflow_orchestrator.go`](file:///e:/HiveMtk/hivemtk-master/user-server/internal/aiagent/agent/runtime/dataflow_orchestrator.go) | `19 块` / `31 行` | `8 块` / `9 行` | `1,896` | `aafc43e5a640dc42...` ✅ | 删除 `方向7`/`方向9`/`方向5`、`A1-A2`/`A5`/`A6-A9`/`B4`/`A11` 专项/图纸编号（N2）、失真的 `调用方保证 sessionID 存在` 描述（N1）及 `DataFlowStats`、`AssetContext`、`ToolOutput`、`NewCoreDataFlowOrchestrator`、`Set*`、`OrchestratorResult`、`GetStats`、`Trigger` 共 11 处自明注释（N6） | **F1**: 保留 `CoreDataFlowOrchestrator`、`EscalationTrigger`、`AssetLoader`、`ToolRouterExecutor`、`ResponseTrimmer`、`ResponsePublisher`、`Process` 端到端编排主流程及 `EscalationAdapter` 契约说明 |
| **49** | [`internal/aiagent/agent/runtime/event_subscriber.go`](file:///e:/HiveMtk/hivemtk-master/user-server/internal/aiagent/agent/runtime/event_subscriber.go) | `2 块` / `10 行` | `2 块` / `3 行` | `527` | `e6f95789e6b8be23...` ✅ | 删除 `NewEventSubscriber` 中冗余的调用点样板代码（N6）及 `tenantCtx` 中 `（specs/001-saas-multi-tenant T021 行自证）` 工单/自证注释（N2/N3） | **F1/F2**: 保留 `NewEventSubscriber` 订阅 `TopicCustomerMessageReceived` 契约，以及 `tenantCtx` 从事件载荷恢复租户上下文且 `TenantID==0` 兼容默认租户语义的不变量 |
| **50** | [`internal/aiagent/agent/runtime/gatekeeper_stage.go`](file:///e:/HiveMtk/hivemtk-master/user-server/internal/aiagent/agent/runtime/gatekeeper_stage.go) | `11 块` / `29 行` | `5 块` / `9 行` | `1,052` | `c079ad1fbd357c4f...` ✅ | 删除 `与本轮要消灭的那句「回复「人工」」死路话术同一类` 历史轮次叙事（N1/N2/N4）、“为什么要有常量”长篇议论（N4）及 `NewDefaultCrisisDetector`、`Name`、`Execute`、`Detect`、`NewSentimentSlidingWindow`、`Record` 6 处自明注释（N6） | **F1/F2**: 完整保留 `HandoffCustomerNotice` 三条措辞约束（严禁命中 `scripts/simulate/ai_quality.py` `DEGRADED_MARKERS`、仅承诺转接排队不承诺已接通、不暴露内部转接原因）、`HandoffQueueNotice` 无在线坐席时入队且 AI 继续兜底应答的语义分界、`DefaultCrisisDetector`、`SentimentSlidingWindow` 及 `IsDeclining` 契约 |

### 6.2 Batch 5 AST/Token 零代码改动核验输出（`verify_tokens_batch5.go`）

```text
==============================================================================================================================
File                                                       | Blocks (B->A)   | Lines (B->A)    | Tokens     | Non-Comment Token SHA256 (Before == After)
------------------------------------------------------------------------------------------------------------------------------
#41 internal/aiagent/agent/portcontract/order.go           | 2 -> 2          | 7 -> 3          | 106        | 130734b812f00ef0... (IDENTICAL)
#42 internal/aiagent/agent/portcontract/session.go         | 3 -> 3          | 11 -> 4         | 125        | 92550e6f39273184... (IDENTICAL)
#43 internal/aiagent/agent/portcontract/tool_result.go     | 4 -> 3          | 6 -> 4          | 161        | 6d9e34be194c0c8a... (IDENTICAL)
#44 internal/aiagent/agent/runtime/alignment_stage.go      | 7 -> 2          | 9 -> 2          | 1082       | 3e6e545b7a0cf1cd... (IDENTICAL)
#45 internal/aiagent/agent/runtime/asset_bundle_loader.go  | 4 -> 3          | 13 -> 4         | 932        | ee18e0edca8158c2... (IDENTICAL)
#46 internal/aiagent/agent/runtime/chat_history_redis.go   | 4 -> 4          | 26 -> 6         | 497        | e877ce0d24afe33b... (IDENTICAL)
#47 internal/aiagent/agent/runtime/context.go              | 28 -> 8         | 30 -> 9         | 1784       | 52de3191f7bc46cc... (IDENTICAL)
#48 internal/aiagent/agent/runtime/dataflow_orchestrator.go | 19 -> 8         | 31 -> 9         | 1896       | aafc43e5a640dc42... (IDENTICAL)
#49 internal/aiagent/agent/runtime/event_subscriber.go     | 2 -> 2          | 10 -> 3         | 527        | e6f95789e6b8be23... (IDENTICAL)
#50 internal/aiagent/agent/runtime/gatekeeper_stage.go     | 11 -> 5         | 29 -> 9         | 1052       | c079ad1fbd357c4f... (IDENTICAL)
------------------------------------------------------------------------------------------------------------------------------
TOTAL (#41..#50)                                           | 84 -> 40        | 172 -> 53       | 8162       | ALL 10 FILES 100% TOKEN-IDENTICAL
==============================================================================================================================
```

### 6.3 Batch 5 编译、静态检查与单测输出（`golang:1.26-bookworm`）

1. **`go build ./internal/aiagent/agent/portcontract/... ./internal/aiagent/agent/runtime/...`**：PASS（退出码 `0`）
2. **`go vet ./internal/aiagent/agent/portcontract/... ./internal/aiagent/agent/runtime/...`**：PASS（退出码 `0`）
3. **`go test -v ./internal/aiagent/agent/portcontract/... ./internal/aiagent/agent/runtime/...`**：共 113 项测试/子测试（含 `TestToolResultIsTheSharedContractType`、`TestSimpleTokenEstimator_Estimate`、`TestSummarizationCompressor_*`、`TestToolCallHistory_*`、`TestDataFlow_*`、`TestInferenceCycle_*`、`TestDefaultIntentExtractor_*`、`TestPublishKnowledgeDocumentChange_*`、`TestNoopLogisticsPortTrack`）**100% PASS (`1.858s`)**

---

## 七、【Stage 2 注释清理批次交接卡 —— Batch 6（`#51`～`#60`）】

### 7.1 Batch 6 逐文件清理明细与 Token 零改动核验表

| # | 文件路径 | 清理前注释块 / 行数 | 清理后注释块 / 行数 | 非注释 Token 数 | Token 流 SHA-256（清理前 == 清理后） | 删除的 6 类噪音摘要（N1～N6） | 保留的 5 类核心功能注释摘要（F1～F5） |
|---:|---|---|---|---|---|---|---|
| **51** | [`internal/aiagent/agent/runtime/global.go`](file:///e:/HiveMtk/hivemtk-master/user-server/internal/aiagent/agent/runtime/global.go) | `3 块` / `4 行` | `3 块` / `3 行` | `116` | `0970688943b1b2b9...` ✅ | 精简 `InitGlobalRuntime` 中 `便于测试` 门禁措辞与跨包对比（N3/N6） | **F1**: 保留 `InitGlobalRuntime`（支持热重载覆盖）、`GetGlobalRuntime`（未初始化返 `nil`）与 `MustGetGlobalRuntime`（未初始化 `panic`）三函数契约 |
| **52** | [`internal/aiagent/agent/runtime/idempotency.go`](file:///e:/HiveMtk/hivemtk-master/user-server/internal/aiagent/agent/runtime/idempotency.go) | `6 块` / `17 行` | `5 块` / `10 行` | `944` | `aa39c6a5ca3961d5...` ✅ | 删除 `MarkReplied` 函数体内 `// claim 仅用于置位...` 对 `_, _ = b.claim(eventID)` 的防御性冗余解释（N4/N6），压缩 `SetReplyGuardRedis` 与 `ClaimReply` 冗余行 | **F1/F2**: 保留 `SetReplyGuardRedis` 不可达自动降级进程内守卫、`ClaimReply` 空 `eventID` 默认放行、`ReleaseReply` 配合 `claim-before-confirm` 释放重投、`HasReplied` 只读判断供事件总线跳过及 `MarkReplied` 幂等置位契约 |
| **53** | [`internal/aiagent/agent/runtime/inference_checkpoint.go`](file:///e:/HiveMtk/hivemtk-master/user-server/internal/aiagent/agent/runtime/inference_checkpoint.go) | `14 块` / `32 行` | `13 块` / `20 行` | `1,128` | `8a2001e6441d811f...` ✅ | 删除 `（D06 / 新规划 T-P1-01）` 工单号（N2）、`行为与挂载前逐字节一致` 历史对比（N1/N4）及 `checkpointState` 私有结构体自明注释（N6） | **F1/F2/F3**: 保留阶段边界检查点续跑机制、`checkpointStateVersion` 不匹配整体重跑、`StageCheckpointStore` 接口契约、`checkpointContext` 排除 `Payload/AgentCtx/StartTime/EpisodicMemory` 原因、`CheckpointThreadID` 内容寻址且排除 `TraceID` 的原因及 `complete`/`finish` 状态转换契约 |
| **54** | [`internal/aiagent/agent/runtime/inference_cycle.go`](file:///e:/HiveMtk/hivemtk-master/user-server/internal/aiagent/agent/runtime/inference_cycle.go) | `20 块` / `46 行` | `6 块` / `8 行` | `2,205` | `6cfe6f33118e82e4...` ✅ | 删除 `E1 补全`、`T-P1-01`（N2）、`SetMemoryProvider` 失真的 `非并发安全` 描述（实际已加 `c.mu` 锁，N1）、`RunOnce` 过时的 `4 个阶段` 描述（实际含 `审校` 共 5 阶段，N1）、`此前并行执行曾造成数据竞争(race detector 实锤)` 历史叙事（N1）、`原空 if 分支为该语义的残缺写法`（N1）及 12 处自明 getter/setter/error 注释（N6） | **F1/F2**: 保留 `EpisodicMemoryProvider`、`InferenceCycle`、`SetCheckpointStore`、`RunOnce` 5 阶段顺序执行与早退/断点续跑契约、感知与对齐必须串行的数据依赖不变量及 `orderedStageNames` 游标比对契约 |
| **55** | [`internal/aiagent/agent/runtime/inference_types.go`](file:///e:/HiveMtk/hivemtk-master/user-server/internal/aiagent/agent/runtime/inference_types.go) | `20 块` / `25 行` | `5 块` / `5 行` | `760` | `44fa94f539f32dbd...` ✅ | 删除 `来源：方向6 情绪感知模块；本文件先定义枚举，便于测试`（N2/N3）、`AlignmentScore.Total` 失真的 `（加权平均）` 注释（实际为等权算术平均，N1）、`InferenceContext` 失真的 `严格不可变（写时复制）` 注释（实际各阶段通过指针原地修改，N1）及 14 处复述类型/函数名的噪音注释（N6） | **F1/F2**: 保留 `Sentiment`、`SentimentScore`（`0-1` 量纲）、`AlignmentScore`（每维 `1-5` 分）、**`MaxDimension`（关键反直觉契约：返回得分最低即最需提升的维度）** 及修正后的 `InferenceContext` 状态容器说明 |
| **56** | [`internal/aiagent/agent/runtime/intent_extractor.go`](file:///e:/HiveMtk/hivemtk-master/user-server/internal/aiagent/agent/runtime/intent_extractor.go) | `3 块` / `10 行` | `3 块` / `4 行` | `635` | `2d3b8551e2046fd6...` ✅ | 删除 `（资产包模式 §六 协议）` 文档章节号（N2）及 `可单测` 门禁语（N3） | **F1**: 保留 `BusinessIntentResult`（`Intent` 枚举与 `CapturedData` 字符串化契约）、`DefaultIntentExtractor` 与 `Extract` 职责说明 |
| **57** | [`internal/aiagent/agent/runtime/local_llm_client.go`](file:///e:/HiveMtk/hivemtk-master/user-server/internal/aiagent/agent/runtime/local_llm_client.go) | `4 块` / `18 行` | `3 块` / `5 行` | `767` | `38cbebc4f7432ef8...` ✅ | 删除 `LocalLLMConfig` 自明注释（N6），压缩 `NewLocalLLMClient` 与 `Chat` 的多行冗余参数列表（N6） | **F1**: 保留 `LocalLLMClient` 实现 `portcontract.LLMChatPort`、`NewLocalLLMClient` 自动归一化 `BaseURL` 末尾 `/` 与 `/v1` 及默认 `180s` 超时、`Chat` 默认模型与默认 `temperature=0.7` 契约 |
| **58** | [`internal/aiagent/agent/runtime/perception_stage.go`](file:///e:/HiveMtk/hivemtk-master/user-server/internal/aiagent/agent/runtime/perception_stage.go) | `12 块` / `20 行` | `3 块` / `4 行` | `1,518` | `05f9f7ca1979c5ae...` ✅ | 删除 `SlotPattern` 失真的 `槽位正则模式` 注释（实际为子串包含匹配，N1）及 8 处复述构造函数/方法名的噪音注释（N6） | **F1**: 保留 `DefaultPerceptionStage`（无外部 LLM 依赖的关键词规则兜底 + 支持自定义注入）、`KeywordSentimentAnalyzer` 及 `KeywordIntentRecognizer` 契约 |
| **59** | [`internal/aiagent/agent/runtime/planner_stage.go`](file:///e:/HiveMtk/hivemtk-master/user-server/internal/aiagent/agent/runtime/planner_stage.go) | `6 块` / `6 行` | `1 块` / `1 行` | `1,183` | `c199cdd74d8cf131...` ✅ | 删除 `NewDefaultTaskPlanner`、`NewDefaultTaskPlannerWithFAQ`、`Name`、`Execute`、`Plan` 5 处字面复述函数名的噪音注释（N6） | **F1**: 保留 `DefaultTaskPlanner` 结构体说明 |
| **60** | [`internal/aiagent/agent/runtime/publisher.go`](file:///e:/HiveMtk/hivemtk-master/user-server/internal/aiagent/agent/runtime/publisher.go) | `4 块` / `19 行` | `3 块` / `4 行` | `307` | `c46576a7e9bb92ed...` ✅ | 删除 `方向8 核心数据流必备`（N2）、`// 租户行自证（T021）：发布点从入站 ctx 提取`（N2/N3）及冗余参数列表（N6） | **F1**: 保留 `PublishCustomerMessage`（`sessionID` 按 `channelType:customerID` 兜底、`traceID` 自动生成）、`PublishCustomerMessageWithType` 及 `tenantIDFromCtx`（`0` = off 模式/系统级发布）契约 |

### 7.2 Batch 6 AST/Token 零代码改动核验输出（`verify_tokens_batch6.go`）

```text
==============================================================================================================================
File                                                       | Blocks (B->A)   | Lines (B->A)    | Tokens     | Non-Comment Token SHA256 (Before == After)
------------------------------------------------------------------------------------------------------------------------------
#51 internal/aiagent/agent/runtime/global.go               | 3 -> 3          | 4 -> 3          | 116        | 0970688943b1b2b9... (IDENTICAL)
#52 internal/aiagent/agent/runtime/idempotency.go          | 6 -> 5          | 17 -> 10        | 944        | aa39c6a5ca3961d5... (IDENTICAL)
#53 internal/aiagent/agent/runtime/inference_checkpoint.go | 14 -> 13        | 32 -> 20        | 1128       | 8a2001e6441d811f... (IDENTICAL)
#54 internal/aiagent/agent/runtime/inference_cycle.go      | 20 -> 6         | 46 -> 8         | 2205       | 6cfe6f33118e82e4... (IDENTICAL)
#55 internal/aiagent/agent/runtime/inference_types.go      | 20 -> 5         | 25 -> 5         | 760        | 44fa94f539f32dbd... (IDENTICAL)
#56 internal/aiagent/agent/runtime/intent_extractor.go     | 3 -> 3          | 10 -> 4         | 635        | 2d3b8551e2046fd6... (IDENTICAL)
#57 internal/aiagent/agent/runtime/local_llm_client.go     | 4 -> 3          | 18 -> 5         | 767        | 38cbebc4f7432ef8... (IDENTICAL)
#58 internal/aiagent/agent/runtime/perception_stage.go     | 12 -> 3         | 20 -> 4         | 1518       | 05f9f7ca1979c5ae... (IDENTICAL)
#59 internal/aiagent/agent/runtime/planner_stage.go        | 6 -> 1          | 6 -> 1          | 1183       | c199cdd74d8cf131... (IDENTICAL)
#60 internal/aiagent/agent/runtime/publisher.go            | 4 -> 3          | 19 -> 4         | 307        | c46576a7e9bb92ed... (IDENTICAL)
------------------------------------------------------------------------------------------------------------------------------
TOTAL (#51..#60)                                           | 92 -> 45        | 197 -> 64       | 9563       | ALL 10 FILES 100% TOKEN-IDENTICAL
==============================================================================================================================
```

### 7.3 Batch 6 编译、静态检查与单测输出（`golang:1.26-bookworm`）

1. **`go build ./internal/aiagent/agent/runtime/...`**：PASS（退出码 `0`）
2. **`go vet ./internal/aiagent/agent/runtime/...`**：PASS（退出码 `0`）
3. **`go test -v ./internal/aiagent/agent/runtime/...`**：共 112 项测试/子测试（含 `TestInferenceCycle_*`、检查点续跑、`TestValidatePayload`、`TestMergeDecision`、`TestStageResult_Helpers`、`TestDefaultIntentExtractor_*` 等）**100% PASS (`1.826s`)**

---

## 八、下一批次推进预告（Batch 7：`#61` ～ `#70`）

- `#61` `internal/aiagent/agent/runtime/publisher_knowledge.go` (5 块, 18 行)
- `#62` `internal/aiagent/agent/runtime/reviewer_stage.go` (5 块, 5 行)
- `#63` `internal/aiagent/agent/runtime/runtime.go` (3 块, 4 行)
- `#64` `internal/aiagent/agent/runtime/stages.go` (13 块, 24 行)
- `#65` `internal/aiagent/agent/runtime/stream.go` (24 块, 34 行)
- `#66` `internal/aiagent/agent/runtime/token_budget.go` (0 块, 0 行)
- `#67` `internal/aiagent/agent/runtime/types.go` (13 块, 55 行)
- `#68` `internal/aiagent/agent/tooluse/adapter_helpers.go` (0 块, 0 行)
- `#69` `internal/aiagent/agent/tooluse/auto_register.go` (3 块, 10 行)
- `#70` `internal/aiagent/agent/tooluse/browser_tools.go` (26 块, 30 行)
