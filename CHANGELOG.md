# 更新日志 (Changelog)

本项目所有重要变更都会记录在此文件。版本号遵循 [语义化版本](https://semver.org/lang/zh-CN/)。

格式基于 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，
本项目遵循 [语义化版本](https://semver.org/lang/zh-CN/) 规范。

## [未发布] - 2026-Q3

### 新增 (Added)

#### 005 · 主动触达合规策略（地区×行业可配置）（2026-10-23，`specs/005-reach-compliance-policy`）

- **策略承载**：参数中心新键 `reach/compliance_policy_json`（`json` 类型，写侧已有 JSON 语法校验、读侧 60s TTL 缓存、变更有操作审计）。**零新表、零新路由、零新前端页面**
- **判定器**：`internal/service/reach_compliance_policy.go`（纯函数式，不碰 DB）支持 `allowed_channels`/`denied_channels`/`quiet_hours_start|end`/`daily_limit`/`require_opt_in` 五类规则，地区键大写、行业键小写；拒绝优先于延后、延后取最早释放点、未命中地区规则可按 `unknown_region_deny` 放行或拒绝
- **同步发送管线**新增第 9 步 `compliance_policy`（`permission` 之后、`rate_limit` 之前）：拒绝即终态（`ErrSendComplianceDenied` + 原因码）、静默窗口复用既有 R-4 延迟队列、`nil` 判定器时该步 `skipped`（**默认零行为变更**）
- **proactive 外发出口**（工具/API/cron 共用 `enforceOutboundGates`）同一判据：授权 → 合规两道顺序固定；同步路径无延迟队列，静默窗口按拒绝返回并给出「可延后至」时刻；新增哨兵 `ErrReachComplianceDenied` 供队列侧终止而非退避
- **审计留痕**：`reach_compliance_log` 扩 5 列（`decision`/`reason`/`region`/`industry`/`policy_scope`），事后可回答「这次为什么被拒」
- **日上限计数**：只在**成功委派给渠道之后**自增（`RecordSent`），被拒/延后不烧配额；进程内计数（与 `MemoryQuietHoursQueue` 同级语义，重启归零）
- **顺带修复**：`.env-example` 补登 `TRUSTED_PROXIES`（HEAD 上 `check-env-coverage.py` 的红：可信反代中间件已读该键但文档面没有）
- **验证**：`go build ./...` 绿、`go vet` 绿、`make audit` 全绿、合规相关 31 个用例全绿（不依赖测试库）

#### 006 · Wave 2 止血切片：五道判据门 + 桥接选择器失效检测（2026-10-24，`specs/006-wave2-gates-and-selector-health`）

共同主题：这五项治的都是同一类病——**功能在跑、CI 在绿，而它其实什么都没守住**。

- **A2-a 依赖策略门**（`scripts/check-dependency-policy.py` + `scripts/dependency-policy.baseline`，自检 10 枚控制）：**不重复** `govulncheck` / `npm audit` 的 CVE 面（它们已在 CI 里跑），只守结构上抓不到的三类 —— 弃维护库的**引用面收敛**（基线 `module | seam | review | why`，`*_test.go` 不计引用面，引用面变宽即 `SEAM_LEAK`）、**到期复查日**（`REVIEW_OVERDUE`，"登记一次就永远躺着"是有时效的决定）、**声明形状**（`latest`/`*`/URL/相对路径 replace 即 `BAD_DECL`）与**禁入名单**。基线是**手工合约**（刻意不给 `--refresh-baseline`：能自动刷新的 seam 基线等于引用面变宽时跑一下就绿）。`lib/pq` 显式 `OUT_OF_SCOPE` 并每次打印原因（它是 model 层的**类型依赖**，收敛＝改字段类型+迁移，不是搬 import）
- **A2-b SBOM 产物断言**（`scripts/check-sbom-artifacts.py`，自检 19 枚控制）：分两段 —— `--workflow` 纯静态核对 `sbom.yml`（三份期望产物都被生成、每步声明 `working-directory`、格式是 `spdx-json`、产物不落在被扫描目录内、每个产物被 upload 收录/每个上传路径有生成步、upload 有 `retention-days`、挂 Release 必须有 `contents: write`），`--dir` 做产物级断言（SPDX 结构、`packages` 非空、`SPDXID` 唯一、**每份清单的直接依赖全部出现在产物里**）。口径边界：**只对账直接依赖**（`go.mod` 非 indirect 行、`package.json` 的 `dependencies`），devDeps 与传递依赖只打印不判（syft 各 cataloger 收录口径不一致，写成硬判据会得到"本机能过 CI 上红"的门）；名称匹配 purl 优先、**只做精确匹配**（模糊匹配会把"少了 x"变成"因为有 y 所以放行"）
- **A2-c WhatsApp 接口化**（`internal/service/whatsapp_provider.go` 新增，`whatsapp.go` 重写为纯委派）：`gowa` 收敛到**唯一 seam**；`WhatsAppProvider`（Kind/SendText/SendTemplate）+ 独立 `WhatsAppLoginProvider`（登录只有个人版扫码需要，Cloud API 没这个概念）；`whatsAppProviderFor` **保留旧判据** `Type=="cloud" && PhoneID!="" && Token!=""`（历史数据里存在 `cloud` 但凭据为空的账号，按类型直接走 Cloud 会把"能发"变成 500）；个人版不支持模板消息时返回 `ErrWhatsAppProviderUnsupported` 而**不退化成文本**
- **A4-a 核心域分档覆盖率**（`scripts/check-core-coverage.py` + `scripts/core-coverage.baseline`，自检 8 枚控制）：把覆盖率从**全仓一个数**（OPT-CI-06 的 22%）拆成**每个核心包一条底线** —— 全仓平均看不到"核心包掉 20 点、边缘包涨回来"。8 条显式前缀（service/repository/controller/model/reach/approval/browser_automation/aiagent），`BELOW_FLOOR`/`UNREGISTERED`(新核心包必须有底线)/`NOT_IN_PROFILE`(**没测到 ≠ 0%**)/`STALE` 四类判据；棘轮**只涨**（`--refresh` 算低时保持原值并打印拒绝），真下调要 `--allow-drop <包名>` 在提交信息里签字
- **B1-a 桥接选择器失效检测**：平台改版后扩展仍在跑、只是再也读不到消息，后端看到的是"这个账号今天没客户"。**分工是本功能最重要的一条设计——扩展侧只上报事实（attempts/hits），判定与阈值全在服务端**（阈值走参数中心 `bridge/selector_health_degraded_below`，兜底 60，改阈值**不需要发扩展版本**；让扩展判阈值会在各版本客户端出现 N 套互相矛盾的规则）
  - 前端：`src/core/selector-health.js`（无 IO 计数器 / 多标签页合并 / **按已发批次扣减**而不是清空）+ `channel-adapter.js` 在 `getMessageItems`、`getConversationList` 两个**胶水方法**上埋点（逐渠道 hooks 埋点＝5 份口径且新增渠道必漏）+ `background/index.js` 用 `chrome.alarms` 1 分钟 flush（MV3 service worker 会被挂起，setInterval 不可靠）；新用例 20 例，bridge 全量 878 例全过
  - 后端：`model/repository/service/controller` 四层 —— `(channel, field)` 唯一、计数**单调累计**（扩展会离线/被休眠，快照会漏掉整段离线时段）、`first_seen_at`/`last_report_at` 支持"这条读数是历史还是现在"；写入先 update 后 create（并发首建撞 23505 补加，不写 `ON CONFLICT (tenant_id,...)` —— 那条唯一索引由 `tenancyidx.Normalize` 在迁移期重建，列序是运维事实）；`hits > attempts` **拒收而不静默夹平**，增量**等比夹取**（夹不是丢：丢掉分母会让命中率偏高，正好掩盖问题）；判定顺序固定 样本不足⇒`warming` → 零命中⇒`broken` → 低于阈值⇒`degraded` → `ok`
  - 路由刻意分两组：`POST /api/bridge/selector-health` 挂桥接凭证组（扩展没有登录态），`GET` 挂 auth 组（扩展凭证不该读得到全渠道运营读数）
  - 四个 Go 常量（`SelectorHealthMinAttempts`/`MaxItemsPerReport`/`MaxDeltaPerReport`/`BridgeSelectorHealthListLimit`）**刻意不入参数中心**，写进 `hardcode-tunable.baseline` 并附理由：两端协议形状与请求边界，单边可调只会让扩展与服务端互相打架
- **接线**：`make audit` 末尾加 A2-a / A2-b(`--workflow`) / A4-a（无 profile 时**大声 SKIP**、不阻断）；`user-server-ci.yml` static gates 加 dependency-policy 与 sbom-artifacts 两步（均含 `--self-check`）、coverage 作业加 core-coverage 两步、push+PR 的 `paths` 各加 7 条（判据脚本与基线必须进触发面，否则"只改判据"的提交不重跑）；`sbom.yml` 加 `pull_request.paths` 与 `--dir .` 断言步
- **顺带修复**：`model/module_tables.go` 补登 `bridge_selector_health`（`TestEveryModelTableHasModuleOwner` 钉死"一张表都不许无主"，新表不登记会当场红）
- **顺带修复（第二轮全量实测暴露）**：`model/module_tables.go` 补登 `bridge_metrics`（无租户语义的运维指标时间序列）与 `email_accounts`（全模块共用的 SMTP 账号配置源，非 reach-email 运行数据）——两表由并发 lane 搬进 model 包后漏登记，`TestEveryModelTableHasModuleOwner` 当场红；`service/arch_gorm_guard_test.go` 棘轮 169→168（现树实测 168 处，棘轮基线不许高于现实）
- **遗留收口（同日第二轮）**：A2-b `--dir` 在真实 syft v1.55.0 产物上实跑通过（干净提交树 + 无 node_modules 的 CI 同形场景，go 与 frontend 直接依赖全覆盖；bridge 零运行时依赖属合法形状），实跑暴露并修正 `manifest_deps` 的判据混淆（清单缺失=红、零依赖=说明后跳过对账），`--self-check` 19 枚复跑全过；`hardcode-tunable.baseline` 补登 geo 两常量（`ContentFormatAttributionSampleFloor` 报表统计口径 / `BatchGenerateMaxKeywords` 同步批契约护栏，附"为什么不入库"），`make audit` 全绿；完整全仓 profile（CI 同款单条命令）实测与 44 条基线一致、核心域 50.7% 无回落
- **第三轮：多角色对抗性审查（安全/并发/测试/架构/产品/CI 六角色交叉）**，发现 7 项、修复 5 项、2 项记决策点（详见 `specs/006/.../delivery.md` 第三轮表）：
  - **R1（安全）已修**：`settleKnown` 守卫不对称——回款/冲销重投撞上被退款行占用的 `channel_ref` 会误复用退款行（`Reused=true` 吞掉真回款）或把退款行 `MarkReversed`。顶上加 `existing.Status==refunded ⇒ ErrPaymentRefReuse` 拒，并补 `TestPaymentServiceRejectsRefundSquattingChannelRef`（回款+冲销两路重投 + 行数/状态不变）
  - **R3/R7（测试）已修**：SBOM 门空依赖盲区——删空 user-web 的 58 条依赖只会 note 跳过 rc=0。零依赖豁免收紧为白名单 `ZERO_DEP_MANIFESTS={user-web/bridge/package.json}`，非白名单清单 `dependencies` 为空 ⇒ 新判据 `EMPTY_DEPS` 红；自检 16→19 枚（bridge 空=note 不红、frontend 空=EMPTY_DEPS、清单缺失而产物在=MISSING_FILE 的 None 分支），docstring 与全仓 9 处「16 枚」文档同步改 19
  - **R5（产品）已修**：`email_accounts` 归 core 的注释过度声称——改为引用面实测口径（EmailService 密码重置/注册通知/proactive/告警 EmailSender/customer_service_plus、channel_overview 计数、backup 白名单；reach-email 走 `email_smtp` 不读它）
  - **R6（CI）已修**：`hardcode-tunable --refresh-baseline` 全量重写会吞掉手工补登段（B1-a 四条 + geo 两条及其理由），下次 `make audit` 又红。改为保留手工段（首个 `# ──` 到文件尾原样续写、自动段名字去重）；实测刷新还伴随分类器漂移（`InboundMisattribWindow` 被丢、`DefaultMaxKeys` 加入），基线文件保持原样不提交
  - **R2（并发）记决策点**：`RecordPartialRefund`「退款额 ≤ 净到账」校验与 `Create` 之间无锁，同账单并发退款可总超额。修法需跨层事务（`SELECT … FOR UPDATE`）或 DDL，且 E-10 尚无入口（见 R4），接线时并入
  - **R4（架构）记决策点**：E-10 实现已入库但无 controller/router 接线、退款 webhook 契约形状未定；当前行为=回款口大声拒 `status=refunded` 不静默丢。本提交补入此前 untracked 的 `payment_partial_refund_test.go`（实现必须带测试，零遗留）

#### 007–013 · Wave 3 + Wave 4 全量交付（2026-10-24，主智能体调度 7 条子泳道并行）

一次交付剩余 11 个改进项（D2/D1/B2/C1/C2/A1/A3/F1/F2/F3/C4），每条泳道独立走 规划→设计→调研→论证→开发→测试→交付，交付四件套在 `specs/007~013`。集成时主智能体补齐前端路由懒装配接线（`pathToModule` 深链映射 + `moduleNames` 白名单），后端/前端 build、gofmt、vet、纯单测全绿：

- **007 D2+B2 观测与渠道健康**（`specs/007`）：`/api/observability/dashboard` 统一看板口径——LLM 成本/送达率/漏斗转化/渠道账号/选择器健康/封禁域名六节聚合（repository 六方法 + service 纯函数判定 `classifyChannelHealth` 顺序固定 + controller/router），前端 `observabilityDashboard` 看板页（requiresAdmin）。真库 SQL 落地受「并发 lane 占用测试库」约束未实跑，已声明
- **008 C1+C2 部署与推理**（`specs/008`）：`scripts/deploy-wizard.py`（纯 Python3 stdlib，交互向导 + `--check` 只读自检，`.env` 原子写、占位值自动换强随机）`--check` 实测 rc=0；C2 推理降级档 `internal/app/inference_tier.go`（档位枚举/HTTP 探测/启动期定档，云端回退默认 **false** 合规基线，Embedding 不分档本地）——决策点：云端判据与 `registerCloudProvidersFromConfig` 一致、探测钉 `/health`
- **009 F1+F3+C4 文档收口**（`specs/009`）：4 层导航 README（architecture/operations/standards/specs）+ 后端文案 i18n 口径 `BACKEND_MESSAGE_I18N_POLICY.md`（127 项缺口清单）+ 多租户复核 `TENANT_MODE_REVIEW.md`（245 带租户表/12 豁免，勘误「业务表无需多租户字段」旧口径）；i18n 三道门禁全绿
- **010 A1 executor 拆分**（`specs/010`）：`browser_automation/service/executor.go` 1901 行 → 8 文件 1978 行（函数体按锚点逐字节搬运、公开 API 零改动），7 个既有测试仅改读取文件、新增 6 例纯函数测试。135 个 DB 用例（2026-10-29 真 PG 复跑：`./internal/browser_automation/...` 全绿 rc=0，service 241.9s、0 FAIL）
- **011 D1 Lead-to-Cash 闭环**（`specs/011`）：`service/ltc_pipeline.go` 状态机（quote_draft→price_confirmed→order_created→bill_derived，恰向前一步无回退）+ 域门映射 + CAS 幂等愈合 + 回款 callout（编译期断言 `*PaymentService` 满足 `ltcPaymentRecorder`）；状态是派生态不建新表、订单走 DTO；18 测试纯内存全绿
- **012 A3 模块契约注册表**（`specs/012`）：`internal/modulekit/` 契约接口 + 内存注册表（7 哨兵、fail-closed 生命周期、RWMutex 并发安全）+ 2 内建示例模块；10 测试 + `-race` 全绿。决策点：`auth_plugin.so` 不迁移（外部产物无源码，独立切片）
- **013 F2 模板技能市场**（`specs/013`）：后端 `template_market` 契约 + 内存假仓储（13 条种子：9 模板 + 4 技能）+ service 过滤/详情 fail-closed；前端独立市场页 + 3 例渲染测试（vitest 3/3）+ `npm run build` 绿。决策点：写侧/支付审核闭环留待立项、`market_catalog_entries` 表只记不建

**集成决策（主智能体）**：① 前端懒装配深链修复——`observability-dashboard`/`template-market` 两个连字符路径补 `pathToModule` 映射、`templateMarket` 进 `moduleNames`，否则刷新/深链落 404；② 后端菜单行（menus 表）与观测路由 policy/adminOnly 收紧、`tests/e2e/routes_user.tsv` 补行留待 lane 空闲时接线（`menu.go` 与 e2e 路由面均为并发 lane 在途）；③ 全仓 `go vet ./...` 唯一红点 `test/integration/knowledge_base_crud_test.go` 的 `CountByAgent` 是并发 lane 在途重构（`service/knowledge_base.go` M）造成，非本批引入，未触碰

#### 003 · SaaS 计费二期（2026-10-19，`.specify/003-saas-billing-v2`）

- **定价改版（显式覆盖 002 年度收费口径）**：功能模块 **23 个全部 0 元**（注册/初始化自动赠予开通，`FreeToEnable`），**浏览器自动化 ¥99 买断**（`billing_cycle=once`，9900 分、不写 `ExpiresAt`，由旧建议 1680 元/年 改为 ¥99，2026-10-19 用户裁决），软件订阅 **149 元/月仅按月**，新商户 **15 天体验期**（`SUBSCRIPTION_TRIAL_DAYS` 默认 15）
- **订阅制**：镜像表 `tenant_subscription_mirror`（sync 写入）+ `SubscriptionGuard` 中间件按 `current_period_end`/`trial_expires_at` **惰性本地判定**（与平台可达性无关，无 cron）；平台端 `tenant_subscriptions` / `subscription_grants` 经 sync 下发 `subscription` 段
- **易支付（用户端侧）**：下单返回 `payment` 段（`pay_url`/二维码）+ 收银台轮询（3s×40 封顶）+ `GET /api/module-market/orders/:no/pay-status` 直查；在线支付不再强制线下联系人（线下路径保持）
- **续费墙**：`expired` 整站业务锁定，业务码 **4305**（`CodeSubscriptionExpired`）→ 续费墙页 + 顶栏横幅；白名单含登录/public config/模块市场/订阅与支付查询，**客户面（widget/chat/bridge/webhook）显式不锁**
- **初始化赠送**：`provisionTenantStarterKit` 幂等克隆 1 个租户级智能体（`default_sales` 副本）+ 1 个起步资产包（`starter-bundle`），失败仅 Warn 不阻断开户（on/off 两态）
- **静默合规采集**：`/setup` 与注册落 `init_ip` / `init_ua`（sha256）/ `init_device_fingerprint`，无用户可见提示；初始化字段收敛为**公司/负责人/电话/地址必填**，邮箱与执照号改选填
- **离线闸口径调整**：`bootstrapOffline` 走 `offlineGiftModuleRows()`——开 **23 个 0 元 gift 模块**、**锁浏览器自动化**（替代 002「只开 `BaseModuleKeys`」），存量升级幂等补行
- **官网云端版入口**：`content.js` 单点常量 `CLOUD_URL`（`https://saas.hivemtk.cn` 占位，上线前替换）→ hero/navbar/pricing CTA/footer 四入口；`PLATFORM_CLOUD_URL` 环境变量可覆盖；`website/TERMINOLOGY.md` 「云端版」转正
- **文档收口**：`MODULAR_MODULES.md` §3.5 覆盖声明与 FR-5.7 离线勘误、`LLM_BILLING_FLOW.md` §七 支付/订阅现状、ADR-004/005 旧口径勘误、新增 `docs/operations/EPAY_LIVE_CHECKLIST.md`（上线前真人支付验收清单）、两仓 `docs/INDEX.md` 补录 002/003

#### 永动审计循环 R1–R72 (2026-09-08/12，六圈 72 轮，21 发现 21 修复)
- 机制：`scripts/audit-loop/`（PLAN.md 12 角度手册 + state.json 机器状态 + STATE.md 报告），ZCode 自动化每 30 分钟一轮，发现问题当轮修复、每轮提交推送
- **R1 security**：删除 email_tracking/email_unsubscribe 死代码默认 HMAC 密钥常量
- **R2 authz**：访客 WebSocket 无 token 连接 fail-closed 语义固化（防凭 session_id 冒连他人会话）
- **R3 architecture**：命名违规 12 处清零（5 个 manage 控制器并入同域文件 + 7 个去冗余后缀）、debug_routes 响应协议归一、rag_product 服务 Repository 化（9 处直连 DB）
- **R4 error-handling**：wecom RefreshAccount 忽略错误回查修复、前端补全局 errorHandler/unhandledrejection 兜底
- **R5 concurrency**：channelMediaFollowers 包级 map 并发读写补 RWMutex（三渠道媒体转存并发读）
- **R6 data-integrity**：0 缺陷轮（事务覆盖/唯一约束/金额存储/恢复原子性/迁移幂等全部核查通过）
- **R7 api-contract**：cmd/seed 缺命令入口致 go build 失败，补 main.go（Seeder 框架 + helpers）；前后端契约对照 831 调用 0 UNMATCHED
- **R8 frontend**：eslint 工具链启用收尾，src errors 163 → 0（含 2 个真逻辑 bug：computed 漏 .value、意图过滤缺 ||）
- **R9 perf**：ai_tool/role 列表页 N+1 批量化（IN 查询/GROUP BY），ExternalCustomer 补复合索引
- **R10 test-coverage**：channelbot/core 补测 0→3 用例；全包 go test 零失败
- **R11 config-deploy**：商户签名密钥键名统一 MERCHANT_API_SECRET（文档侧与代码不一致会致启动报错）
- **R12 docs-consistency**（第一圈收官）：修复 marketing-features README 失效相对路径引用，check-doc-consistency.sh 唯一 error 清零；CHANGELOG 补录审计循环机制
- **第二圈 R13–R22**（security/authz/architecture/error-handling/concurrency/data-integrity/api-contract/frontend/perf/test-coverage）：连续十个 0 缺陷轮——第一圈 15 项修复全部回归在位，N+1 候选 31→27（R9 修复直接效果），契约复跑 0/831 UNMATCHED，eslint errors 保持 0，vitest 174 用例全过
- **第三圈 R25–R36**：全部 0 代码缺陷（R36 修复 3 项功能连贯性问题：访客关闭会话拦截/评分回流 CSAT/菜单去重与 hidden 冲突）；R35 期间同事完成 L4 存量收敛大批次（44 文件，service 直连 DB 152→38 处）
- **第四圈 R37–R48**：0 新增代码缺陷（R48 功能完善度专项修复 R36 遗留 + 确认 webhook 双通道设计语义）；R43 起契约随 browser-automation 新功能同步增长（831→872 调用）保持 0 UNMATCHED
- **第五圈 R49–R60**：全部 0 代码缺陷收官（R49 收编前会话遗留批次 + 修复测试断言口径）；R50 起 browser-automation 全栈功能入库，契约/守卫/架构三面保持对齐；L4 基线进一步降至 35 处
- **第六圈 R61–R72**：全部 0 代码缺陷收官——第一圈 21 项修复经六圈复扫全部稳固，全仓 go test 零失败、eslint errors=0、契约 872 调用 0 UNMATCHED、端口/键名三方一致持续保持

### 安全 (Security)

#### 用户端质量飞轮 (2026-08-17 多角度轰炸测试 + 修复)
- **客户创建输入校验** (`internal/service/customer.go` + `internal/controller/customer.go`)
  - 拒绝空 body（全部标识符为空）→ 400 `INVALID_PARAM_1001`，杜绝脏数据入库
  - 校验手机号格式（中国 11 位 `^1[3-9]\d{9}$`）→ 400
  - 校验邮箱格式 → 400
  - 已存在客户的更新只覆盖**非空**字段（避免部分更新把其它标识符清空）
  - **并发竞争幂等**：PG 23505 → 重新 FindByIdentity 返回已存在行，避免并发 10 个请求 → 4 个 500（10 并发测试已绿，DB 仍 1 行）
- **暴力守卫语义修正** (`internal/middleware/brute_force.go`)
  - 移除 `BruteForceGuard` 中重复自增的 `entry.failures++` 与 `failures = 0` 死代码（双计数导致 3 次失败即锁，与配置的 `MaxFailures=5` 不一致）
  - 守卫职责收敛为"判定是否已锁定"；真实计数/加锁由 `RecordBruteForceFailure` 单点维护
  - 删除无人调用的 `getBruteForceEntry`（返回临时 struct，自增后被丢弃，误导性强）
- **多角色越权修复**（二轮发现 5 类 P0 漏洞，全部 admin only）
  - **LLM 路由** (`internal/router/service_routes.go`)：Models/Strategies/SceneRouting/Fallback/Provider 写操作 admin only；staff 可注入恶意 base_url 重定向全公司 LLM 流量到 evil.com 窃取对话数据
  - **第三方集成** (`internal/router/business_routes.go`)：Create/Update/Delete/Test/Sync admin only；含 corp_secret 等敏感凭据
  - **企微账号** (`internal/router/chat_routes.go`)：Create/Update/Delete/Sync/Send/Refresh admin only
  - **chat-channels 渠道** (`internal/router/chat_routes.go`)：Create/Update/Delete/RotateKey/ResetSecret admin only；含 app_key/app_secret
  - **AB 实验** (`internal/router/business_routes.go`)：Create/Update/Delete/Start/Pause/Stop admin only
  - **WhatsApp** (`internal/router/platform_routes.go`)：CreateAccount/CreateJob/DeleteJob/StartLogin 等 admin only
  - **前端别名路由** (`internal/router/frontend_aliases.go`)：新增 `doRegAdmin` 工具，确保 alias 路径同样受保护
- **回归单测** (`internal/middleware/brute_force_test.go` + `internal/service/customer_test.go`)
  - `BruteForceGuard` 3 用例：纯请求不锁 / 5 次失败第 6 次 429 / Clear 循环不锁
  - `CustomerService.CreateOrUpdate` 4 用例：空 body 拒绝 / 5 种格式错误拒绝 / 纯第三方 ID 通过 / 部分更新保留其他字段
- **多角度负面测试脚本** (`tests/e2e/deep_security.sh`, 54 用例)
  - 鉴权 / Token / 登出 / 用户枚举 / 暴力 / CORS 反射 / IDOR / 客户创建校验 / 文件上传类型 + 路径穿越 + 魔术数字 / 边界 / 桥接端点 / **多角色越权 32 用例** 全部 PASS
- **SSE 端点编译占位** (`internal/bridge/sse_stub.go`)：另一迭代 v3 SSE 端点骨架引用了未定义类型（`SSEHandler`/`NewSSEHandler`/`SSEEvent`），本轮加 stub 让 `go build ./...` 恢复 EXIT=0

### 新增 (Added)

#### Bridge 桥接架构 (Chrome 扩展 + 三通道 HTTP 协议)
- **M2-P1 Popup 增强**:健康度面板/告警横幅/紧急停止/多账号/错误码友好化
  - `src/popup/health.js` 实时健康度面板（circuit-breaker 状态、延迟 P50/P95、错码分布、死开关）
  - `src/popup/alert-banner.js` 熔断/无响应时自动弹红色横幅
  - `src/popup/emergency-stop.js` 一键停所有桥接活动
  - `src/popup/error-messages.js` 18 种错误码中英文双语 + 解决建议
- **M1-P0 全面升级** (桥接代码质量 10 项 P0 修复):
  - 协议升级强制 `conversation_id` 入参（v1/v2 兼容）
  - `AckOutboundItem.Error` 字段透传
  - `req.Status` 决定终态（delivered/failed）
  - 单 SQL `UPDATE...RETURNING` 原子化合并 Get+Update
  - 500 条 IN 性能基准（< 200ms 阈值）
  - 跨账号探测安全测试（not_in_scope 状态）
  - PROTOCOL 常量共享单源化
  - `not_found` 区分 GC 回收 / 归属错
  - `_pendingAck` 最大重试次数 + 指数退避（10 次 / 1s→60s cap / 24h TTL）
- **M0 桥接架构重构**:
  - 三通道独立 HTTP（uplink / outbox / ack 相互独立）
  - Token 从 URL 迁移到 Authorization Header
  - 熔断器（Circuit Breaker）三态机
  - RateLimiter LRU+TTL 防内存泄漏
  - 死开关（Dead Man's Switch）健康度上报
  - 人类化模块（贝塞尔曲线鼠标轨迹/正态分布键入延迟）
  - 配置热更新（chrome.storage 监听）
  - 端到端请求 ID 追踪

#### 企业级合规
- 审计中间件（who/when/from_ip/action/resource/result/trace_id）
- 敏感数据脱敏中间件（手机/邮箱/身份证）

#### 文档
- `BRAINSTORM_2026-08-15_8D_REPORT.md` 8 维度报告（产品/企业/开源/代码质量）
- `bridge_10_of_10_task_list.md` 4 维度任务清单（44 项 / 283h）

### 变更 (Changed)

- **前端 popup** 全新 UI：紧急停止/多账号/健康度面板/告警横幅
- **后端 ack 协议** v1→v2 平滑升级（conversation_id 过滤/failed 终态/Error 字段）
- **巡检风控** 30-60s 轮间隔 + 6 个会话/轮 + 120s 会话冷却 + 3-5s 切换间隔
- **Token 传输** URL query → Authorization Header

### 修复 (Fixed)

- 双巡检冲突（删除 PollingLoop._patrol，保留 BaseAdapter._startPatrolAuto）
- `OutboxMessage.Extra` 字段缺失（前端主动私信路由失效）
- `account_id` 缺失时 default 兜底（垃圾数据风险）
- mergedEvent.EventID 使用新 UUID（幂等性丢失）
- ack 失败导致用户收到重复消息
- 高频巡检触发平台风控（30-60s 节奏调整）
- 16 个 `getPeerName FULL DIAG` 冗余打印删除

### 弃用 (Deprecated)

- PollingLoop 双巡检（保留 L1 巡检作唯一源）
- WebSocket 桥接协议（改为 HTTP 长轮询）
- Token in URL query（迁 Authorization Header）

### 安全 (Security)

- 桥接 Token 强制 Header 传输
- 跨账号探测返回 `not_in_scope`（不告知归属，防越权信息泄露）

---

## 历史版本 (Historical Releases)

> 历史版本见 [GitHub Releases](https://github.com/xiaofang142/hivemtk/releases) ·
> [Gitee Releases](https://gitee.com/xhpmayun/hivemtk/releases)

### 2026-Q2 早期迭代 (v0.1 - v0.4)

#### v0.4 (2026-Q2 末)
##### 新增
- **宿主机推理栈迁移方案**:llama.cpp (Qwen2.5) + TEI (bge-m3 + bge-reranker-v2-m3) 从容器化迁移到宿主机,节省 CPU/内存、提升推理性能
- 5 阶段并行 + 双层架构 + WebSocket 流式输出(FeatureFlag 灰度控制)
- 4 级 LLM 降级链(7B → 3B → 规则 → 兜底回复)
##### 变更
- 推理栈端口固定为 LLM :8207 / Embedding :8208 / Rerank :8209
- 支持 dev / prod 模型档切换(Qwen2.5-3B / 14B)

#### v0.3 (2026-Q2 中)
##### 新增
- 94 个营销业务模块(marketing-features 文档体系)
- 三级 RAG 检索:粗排(向量召回) + 精排(bge-reranker) + LLM 改写(HyDE / Query Rewriter)
- 多智能体协作:被动应答智能体 + 主动触达智能体(ADR-013)
- AI 销冠:话术模板 + RAG + 自动跟进
- 七端社媒触达:抖音/快手/小红书/闲鱼/TikTok/企业微信/邮件
##### 变更
- 后端 Go 五层架构(Controller → Service → Repository → Model → DTO)硬约束落地,`scripts/check-architecture.sh` 入库 CI

#### v0.2 (2026-Q2 初)
##### 新增
- 41 个 ReAct 智能体工具注册表
- 统一 CDP 客户视图 + 统一消息中心
- 营销自动化 SOP 可视化编排
- 企业微信 SCRM 多账号聚合 + 离职继承
##### 安全
- JWT 鉴权 + 角色权限 + 行级数据权限(data_scope)

#### v0.1 (2026-Q2 初,首个可运行版本)
##### 新增
- 初始 Go 后端(gin + GORM + pgvector)+ Vue 3 前端
- PostgreSQL 15 + pgvector(1024 维) + Redis 7 数据层
- 5 分钟一键部署(`make install`)
- AGPL-3.0 开源,发布 GitHub / Gitee 双仓库

> 📌 各版本具体 commit 记录见 [GitHub Releases](https://github.com/xiaofang142/hivemtk/releases) · [Gitee Releases](https://gitee.com/xhpmayun/hivemtk/releases)

---

**图例**:
- 🆕 新增
- 🔧 变更
- 🐛 修复
- ⚠️ 弃用
- 🔒 安全
- 📚 文档
