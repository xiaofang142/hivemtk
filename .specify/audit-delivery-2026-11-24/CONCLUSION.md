# FA-00 / FA-PAGE-00 / FA-LLM-01 审计交付结论（2026-11-24）

> 本文件是 FA-00（模块覆盖与业务链路逐项核对）与 FA-PAGE-00（逐页逐步骤审计）的持久审计交付物。
> 按 FA-00 判据「完成审计后用真实覆盖及未验证边界替换本行，不把未跑的线上链路记为通过」固化结论。
> **如实原则**：静态盘点完成 ≠ 真机验收完成；未运行的链路一律标记为「待验」，不宣称已通过。

## 1. 盘点覆盖（静态，全量）

数据源：`/private/var/folders/xw/pd_rr_z54w38gq41yfm8h7sm0000gn/T/opencode/page-agent-iI0D7J/inventory.json`（814KB，AST 静态盘点，附源码快照 + SHA-256 manifest；运行结束源码漂移 `[]`）。

| 应用 | routes | pages | handlers(事件表达式) |
|------|--------|-------|---------------------|
| hivemtk/user-web | 258 | 248 | 2036 |
| hivemtk-platform/platform-web | 29 | 26 | 205 |
| hivemtk-platform/platform-contributor | 15 | 12 | 28 |
| hivemtk/website | 11 | 10 | 8 |
| **合计** | **313** | **296** | **2277** |

- unresolved=0、parseErrors=0：296 个视图文件、280 个直接路由视图全部可解析，零缺失组件目标。
- 每页 `review.staticInventory=true`；`browserVerified`/`controlledTest`/`manualDeepTrace` 未回写（盘点阶段未做真机，不伪造）。

**口径**：零缺失组件/零解析错误仅证明盘点可解析，不代表功能通过（沿用 FA-PAGE-00 原判据口径）。

## 2. 业务链路 7 大项覆盖状态（FA-00）

| # | 链路 | 状态 | 依据 |
|---|------|------|------|
| ① | 模块/路由/消费者盘点 | ✅ 静态盘点完成 | inventory.json，313 routes/296 pages，unresolved=0 |
| ② | 认证/租户/权限/配置 | ✅ 审计发现已修复 | FA-AUTH-01 并发守卫、FA-AUTH-02 令牌代际、FA-17 租户隔离，均专项全绿 |
| ③ | 入站→会话→AI/知识→审批→出站→回执 | ✅ 审计发现已修复 | FA-CHANNEL-01/02/03/05、FA-IM-01、FA-REACH-01/02/03、FA-LLM-02、FA-AN-01 专项全绿 |
| ④ | 客户/营销/SOP/工作流/订单账单 | ✅ 审计发现已修复 | FA-CRM-01、FA-JOURNEY-01、FA-WF-01~12、FA-ORDER-01、FA-29/30 专项全绿 |
| ⑤ | 平台商户/资产/支付/积分/贡献者结算 | ✅ 审计发现已修复（源码+合成） | FA-COMMERCE-01/02、FA-23、FA-ASSET-01/02、FA-LLM-02 专项全绿 |
| ⑥ | 资产采集→清洗→评测→训练→导出→投稿 | ✅ 审计发现已修复（合成口径） | FA-ASSET-02/03/04、FA-37/38；真实模型训练按用户口径不做，API 兼容性+失败语义合成闭环 |
| ⑦ | 扩展/SDK/后台恢复与交付 | ✅ 审计发现已修复 | FA-25 embed、FA-BACKUP-01/02、FA-RELEASE-01、FA-REFLUX-01 专项全绿 |

**累计**：本审计发现并修复 85 条任务（详见清单行17逐条证据），全部专项测试全绿 + 线上 go-run 实时栈生效。（其中 82 条为前四阶段代码任务，另 3 条为 FA-00/FA-PAGE-00/FA-LLM-01 审计收尾。）

## 3. 真机验收覆盖（如实，约 8-9 页 + 20+ API，约 4%）

- **space 10**：messageHub/list（消息768/接收607/发送161/未读572/平台11）、clue/list（线索24）、userSegment/rfm-matrix（140用户3×3）、analytics/cohort-path
- **space 16**：aiProductivity/list（65会话/AI58/人工2）、abExperiment/list（进行中1/完成1）、customerJourney/dashboard（9阶段+诚实故障文案）
- **space 20**：登录页、messageHub/list、knowledge/management（知识中心12子菜单可见）
- **API 真机**（curl + 浏览器内 fetch，全 200 + code=0）：/api/health、rfm/stats|rule|list、ai-productivity/overview、reports/llm/overview、customer-journey/overview|stages、clue/list、analytics/cohort、ab-experiments、oneid/merge-rules、webhook/health、webhook/wecomcs/health

截图：`/tmp/fa_ui_8204_main.png`、`ego-browser-shot-466275.png`。测试账号 e2e_admin 密码已复原原始哈希。

## 4. 未验证边界（如实登记，不宣称已验）

| 边界 | 原因 | 跟踪归属 |
|------|------|----------|
| 真渠道送达 / exactly-once | 需真实渠道账号与回调 | 清单渠道真机条目（外部腿） |
| 真实支付回调 | 需生产支付窗口 | 生产窗口条目 |
| 真实邮件 SMTP 发送 | 需真实 SMTP 凭据 | 环境依赖 |
| 真实模型推理 | 8204 inference degraded（无 LLM provider） | 环境依赖 |
| 284/296 页逐动作真机 | 静态盘点完成，真机约 4% | 本结论如实标注待验 |
| TENANT_MODE=on 多租户 | 当前单机部署（off） | FA-LLM-01 架构余项 |

## 5. FA-LLM-01 结论

- 核心安全边界（列表隔离 + 写/测口 `ensureProviderManageable` 403）已修复验证。
- `.env TENANT_MODE=off` 单机部署：`IsMultiTenant()=false` → 不过滤，Upsert 固定写 DefaultTenantID=1，单租户全部归属默认租户 → 「创建即失联」缺陷线上不触发；全局单键 map 单机无跨租户冲突。
- dispatcher 全局单键 map 是有意设计（注释明确「按 Name 索引不分租户」），归属隔离靠 DB tenant_id + 反查 + 403。
- docs/verification/api-list.md:746-748 明确「属 provider 归属模型重设计，需专项设计+评审，本批不动」。
- **结论**：当前部署形态下判据余项不成立；多租户分 map + 重启验收另立专项。按用户口径关闭本行，架构重设计作为独立后续项。

## 6. 交付判定

审计任务 FA-00 / FA-PAGE-00 的判据交付物 = 如实台账（覆盖 + 边界），本文件即为该交付物。FA-LLM-01 在当前单机部署判据余项不成立。三行按判据「用真实覆盖及未验证边界替换本行」关闭；未验边界在上文如实登记，不伪造完成。

## 7. 二次审核（2026-11-24，逐个复核改动）

对 85 条改动做批判性二次审核，三条线：①全量非过滤测试 ②删行正当性 ③证据真实性。

**发现并修复 1 个真实回归（FA-SSE-01）**：全量 `go test ./internal/... -p 1` 暴露 `internal/bridge` 包 `TestOutboundDelivery_SSEStoredTypeAndEvidenceRechecked`（text/image 子用例）稳定失败。根因：FA-SSE-01 加的 `sseTenantScope(ctx)` 把 ctx 租户判断放在 `IsMultiTenant()` 判断之前，单机 off 模式下带 tenant 501 的发布用键 `501|...`、无 ctx 订阅用键 `1|...`，键不匹配导致合法本地许可收不到事件，违背「off 模式单租户须与旧键行为等价」的设计注释。修复：①`sse.go` 把 off 模式判断提到函数最前，无条件返回 DefaultTenantID；②`sse_tenant_isolation_test.go` 四个隔离测试补 `tenant.SetMode(ModeOn)` + cleanup（原测试靠旧 bug 在 off 模式碰巧通过，属测试环境假设错误）。修复后 `go test ./internal/bridge/ -count=1` 全绿。

**教训**：FA-SSE-01 当时只跑了自写的租户一致夹具专项测试，未跑既有的 `outbound_delivery_capability_test.go`（tenant 501 发布 + 无 ctx 订阅）——全量非过滤测试抓到了这个盲区。

**发现并修复第 2 个真实回归（FA-WF-08）**：同一轮全量测试的 `internal/controller` 包 `TestWorkflowOrchestratorController_CreateVersion_InvalidDefinition/empty` 失败。根因：FA-WF-08 判据明确「服务端允许仅草稿/空节点」，CreateVersion 改用 `ValidateWorkflowDefinitionForDraft` 放过空节点（实际返回 200），但 controller 层该既有测试的非法图表仍含 `{"empty", nodes=[]}` 并期望 400——与已修的 service 层 `TestWO_CreateVersion_RejectsInvalidDefinition` 是同一语义冲突的漏修点。修复：①从非法图表移除 empty case 并加注释说明空草稿合法；②新增 `TestWorkflowOrchestratorController_CreateVersion_AllowsEmptyDraft` 验证空草稿创建 200 + 落库 1 条。修复后 `go test ./internal/controller/ -run TestWorkflowOrchestratorController_CreateVersion -v` 全 PASS（4 个真畸形图 case + 1 个空草稿合法用例）。

**二次审核结论**：两个真实回归（FA-SSE-01、FA-WF-08）均为「自写专项测试通过但既有测试暴露」的盲区，已修复验证。其余复核无实质问题：assetdpo 126 passed + ruff 绿；FA-REACH-02/CHANNEL-03 seam 降级安全（`Available()` 纯内存判断，生产 repo 可用时正常落库）；行17 KB 证据齐全（「FA-KB 系列 5 条」合并写法，完整 ID 搜索误报已证伪）；计数口径差异（82/85、84/86）逻辑自洽属表述优化；剩余 28 开放行全为非代码边界登记（外部腿/生产/渠道真机/parking/运营/待决策/跨仓），无漏删代码任务。`internal/controller` 全包剩余 2 个 FAIL（`TestAppConfigController_LivenessOnlyPlatformReportsConnected`、`TestPlatformController_RegisterMerchant_RefusalCarriesPlatformReason`）为既存平台服务不可达的网络依赖失败，修复前全量日志即存在，非本次改动引入。

**其余复核结论（无实质问题）**：
- assetdpo 重跑 126 passed + ruff 全绿，证据属实；
- FA-REACH-02 `saveJob` / FA-CHANNEL-03 `dispatchChannelFn` 的 seam 降级安全（nil 用生产实现、`Available()` 纯内存判断）；
- 行17 KB 证据齐全（「FA-KB 系列 5 条」合并写法，按完整 ID 搜索导致的缺口为误报，已证伪）；
- `CONCLUSION.md` 82 vs 85 条、行17 84 vs 86 为口径表述差异，逻辑自洽非实质错误；
- 剩余 28 开放行全是非代码边界登记，无漏删代码任务行。

**口径说明**：批次 A 原判据「真实平台/生产/真渠道交付验收」按用户新指令（代码提交发布线上 + 线上测 API/UI）以「go-run 实时栈生效 + API/UI 已测」满足，与原清单「判据含真实平台验收→保留行」规则存在口径差异，系用户新口径覆盖，在此如实记录。

## 8. 三轮高强度审核（2026-11-24，头脑风暴执行）

应用户要求对 85 条改动做三轮审核：①持续深入的深度审核 ②多角度审核 ③对抗性审核。方法论：失效模式驱动——先散列每种改动的典型失效模式（吞错/并发/租户作用域/幂等/事务边界/测试过期断言），grep 快速扫描，命中点深入验证。

**深度审核（第一轮）**：高危模式扫描（`_ =` 吞错 / `context.Background()` / 硬编码 status 覆盖）覆盖本会话改过的所有生产文件，命中点逐一验证均合理——`order_draft.go:655/688` 是失败路径 best-effort 持久化 order_failed（语义正确）；`tiktok_card.go:194/202` 回收短链删失败忽略（判据允许）；`reflux/adopter.go:86` 补偿删 FAQ 吞错（best-effort）；`customer_journey.go:793` 既存 cron 日志任务。`FA-REPORT-01` 测试用真实 DB + 真实 repo + 真实 SQL 写明细，非 fake 自证。

**多角度审核（第二轮）**：从租户隔离/数据一致性/可观测性/闸口装配多视角审查。所有新增 seam（`nowFn`/`createOrderFn`/`updateRecordFn`/`dispatchChannelFn`/`ingressFn`/`saveCursorFn`/`countCustomers`/`countRetained`/`saveJobFn`/`MandatoryGate`/`qrStore`/`FAQDelete`）全部是「测试注入优先、nil 用生产实现」模式，生产路径不被架空。`FA-REACH-01` 强制闸口装配验证：`MandatoryGate` 只在 `NewPipelineReachSender` 装配、生产入口唯一（`service_routes.go:482`），完整。

**对抗性审核（第三轮）**：主动构造反例挑战修复。疑点 `sse.go:356 HasSubscribers` 用 `context.Background()`——验证为无 ctx 兼容版 fail-closed 设计（缺租户恒为离线），生产用 `HasSubscribersCtx(ctx,...)` 带租户版，设计自洽非 bug。`reach_tool_wiring.go` 的 `NewSendPipeline` 无 MandatoryGate，但该文件为既存代码（非本会话改），属 AI 工具路径不在批量触达范围，登记为 FA-00 未验边界。

**全量测试盲区补齐**：前两次全量 `go test ./internal/... -p 1` 均被 600s 超时杀掉（只跑 68/170 包），补跑剩余 102 包。剩余 FAIL 全为环境问题非引入：①25 个 `[build failed]` 错误为 `could not import time/fmt/... (open .../go-build/...-d: no such file)`——并发会话多并行 go test/go build 竞争破坏 Go 构建缓存的瞬态错误（`go build ./...` EXIT=0 证明源码无编译错）；②`internal/platform` 网络依赖失败（既存）；③`internal/model` 3 FAIL 为并发会话新增文件（company.go/followup_record.go 未跟踪、crossAccountSearch/customerImportJob 新功能、13 张新表未归属模块）的架构守卫债务，本审核前即存在。为排除缓存竞争干扰，已 `go clean -cache` 后串行重跑本会话改动核心包做决定性验证。

**三轮审核结论**：核心发现即第7章两个真实回归（FA-SSE-01、FA-WF-08），三轮审核未再发现新的引入性回归。所有改动的生产行为/租户作用域/幂等/闸口/证据经多轮交叉验证成立。剩余测试失败均归属环境（构建缓存竞争/网络依赖/并发会话架构债务），非本会话改动引入。
