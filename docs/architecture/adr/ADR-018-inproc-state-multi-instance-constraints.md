# ADR-018：user-server 进程内状态的多实例部署约束与 Redis 化路线

| 字段 | 内容 |
|------|------|
| 编号 | ADR-018 |
| 标题 | user-server 限流/防爆破四处进程内状态的多实例部署约束、影响量化与 Redis 化触发路线（审计 N-3，对齐平台端 L-6 同类项） |
| 状态 | ✅ Accepted（约束登记即生效；Redis 化改造为**待触发项，未实施**） |
| 决策者 | @maintainer-team（触发条件与优先级为架构口径；fail 口径留待实施时评审） |
| 日期 | 2026-11-11 |
| 适用范围 | `user-server/internal/middleware/{brute_force,rate_quota,visitor_rate_limit,mfa,ratelimit}.go`、`user-server/internal/router/{router,chat_routes,admin_routes,self_service_routes}.go`、`deploy/helm/hivemtk/` |
| **关联文档** | [MIDDLEWARE_ORDER.md](../../../user-server/docs/architecture/MIDDLEWARE_ORDER.md)（中间件装配顺序）、[HA_DEPLOYMENT.md](../../operations/HA_DEPLOYMENT.md)（现行单机部署形态）、[helm chart README](../../../deploy/helm/hivemtk/README.md)（HPA 骨架现状）、[ADR-008](ADR-008-reach-rate-limit.md)（触达限流，本文范围外）、[DEPLOYMENT_GUIDE.md](../../DEPLOYMENT_GUIDE.md) |

## 1. 背景

2026-11-11 审计登记 **N-3**：user-server 多处限流/防爆破状态是**进程内**实现（内存 map / 本地令牌桶），
多实例部署下会静默退化。同批平台端登记同类项 **L-6**（platform-server，由平台仓任务另行处理，
本文 §8 只登记边界，不重复其内容）。

「存在水平扩缩意图」的证据链（如实陈述，不夸大现状）：

- `deploy/helm/hivemtk/values.yaml:185-191` 有 `autoscaling` 骨架（`enabled: false`，minReplicas 2 / maxReplicas 10）；
- `deploy/helm/hivemtk/Chart.yaml:10` 把 HPA 列入未实现注释；
- `deploy/helm/hivemtk/README.md:129` 自证「**未实现** HPA (autoscaling.enabled 骨架默认 false)」；
- `templates/` 下只有 deployment / ingress / service，**没有 hpa.yaml**（挂起项 OPS-1 登记）。

即：**意图存在（骨架 + OPS-1），当前无生效 HPA**。现行唯一生产形态是单机单实例
（宿主机 user-server 进程 + docker 数据层，见 HA_DEPLOYMENT.md §1）。本文回答四件事：
哪些防线在 ≥2 实例时会退化、退化多少、什么时候必须改造、改造按什么顺序做。

## 2. 证据现状复核（2026-11-11 实测，树 = master b8d14c17）

> 审计登记行号与现树有漂移（N-2 刚改过相邻代码），逐处按现树重新定位；机制全部亲验，
> 其中第 4 处（MFA）出现**反证**，约束清单已如实调整（见 §2.4 / §9）。

| # | 防线 | 状态存储 | Redis 部署下多实例语义 | 证据（文件:行，现树） |
|---|------|----------|------------------------|----------------------|
| 1 | brute force 防爆破锁定 | 进程内 map | 各实例独立计数与锁定（未覆盖） | `brute_force.go:24-31` |
| 2 | PathQuota 路径配额限流判定 | 进程内令牌桶 | 各实例独立计数（未覆盖；Redis 仅读数快照） | `rate_quota.go:270-283` |
| 3 | 访客双维限流（四参数） | 进程内 map | 各实例独立计数（未覆盖） | `visitor_rate_limit.go:36-44`、装配 `chat_routes.go:36-46` |
| 4 | MFA recent 二次验证宽限 | **Redis 主 + 内存降级回退** | 主路径跨实例共享（反证，见 §2.4） | `mfa.go:27-33,52-104` |
| 对照 | 全站 RateLimitMiddleware | Redis 分钟计数 | **已共享（已覆盖）** | `ratelimit.go:265-277`，挂载 `router.go:248` |

### 2.1 brute force 防爆破锁定（未覆盖，恒进程内）

- 状态本体：`bruteForceProtector`（`sync.RWMutex` + `map[string]*bruteForceEntry`，键 `ClientIP|endpoint`），
  包级单例 `globalBruteForce`（`user-server/internal/middleware/brute_force.go:24-31`；审计登记 25-30，
  漂移 1 行，机制未变）。**全文件无任何 Redis 引用**。
- 参数：`DefaultBruteForceConfig`（`brute_force.go:112-116`：窗口 15min、MaxFailures 5、锁定 30min），
  倍乘锁定 ×1/2/4/8（`brute_force.go:190-201`）。N-2 只改了 janitor 协程收口（`brute_force.go:52-69` SafeGo），
  未触及存储机制。
- 判定：`BruteForceGuard` 只查锁定（`brute_force.go:128-164`）；计数单点在控制器失败回调
  `RecordBruteForceFailure`（`brute_force.go:168-202`）。
- 挂载 6 个鉴权端点：`admin_routes.go:49`（auth.login）、`admin_routes.go:51`（auth.mfa）、
  `admin_routes.go:55`（auth.register）、`self_service_routes.go:17`（register）、
  `self_service_routes.go:18`（forgot-password）、`self_service_routes.go:19`（reset-password）。

### 2.2 PathQuota 本地令牌桶（未覆盖；Redis 只做读数快照）

- 代码自证：`rate_quota.go:270-272` 注释「Redis 部署下仍按分钟聚合计数写 Redis……**限流判定本身走本地桶**
  （多实例部署时各自计数，见任务边界说明）」（审计登记 270 行，漂移至 271-272）。
- 判定本体：`getPathQuotaManager().allow(clientKey, path, cfg)`（`rate_quota.go:283`）——
  `pathQuotaLimiterManager` 进程内每客户端×每路径一个 `rate.Limiter` 桶，清理协程每 5 分钟
  （`rate_quota.go:226-242`）。
- Redis 的角色：`rate_quota.go:275-281` 的 `Incr` 只写**跨租户聚合读数键**
  `mtk:ratelimit:path:{path}:{minute}`（供 `GET /api/system/rate-quota` 快照），不参与判定。

### 2.3 访客双维限流（未覆盖，恒进程内）

- 装配期构造：`chat_routes.go:36-46`（审计登记 34-44，漂移 2 行，机制未变）——参数中心四参数
  装配期读一次塞进限流器构造，改完需重启（种子 `ratelimit.visitor_*` 标 `Restart=true`）。
- 状态本体：`visitorRateLimiter` 两张进程内 map（`ipLimiters` / `channelLimiters`，
  `golang.org/x/time/rate` 的 `rate.Limiter`），包级单例（`visitor_rate_limit.go:36-44`）。
  **全文件无任何 Redis 引用**。
- 「四维」指四个参数（PerIPRPS / PerIPBucket / PerChannelRPS / PerChannelBucket，默认 20/40/100/200，
  `visitor_rate_limit.go:23-29`），判定本体是每 IP + 每 channel 两个桶（防爬虫 + 防脚本滥用）。

### 2.4 MFA recent（反证：主路径已 Redis 化，约束如实降格）

- 审计原登记「MFA recent map（`mfa.go:23-30`）恒进程内」。**现树不成立**：
  - 内存 map 仍在（`mfa.go:27-33` 的 `mfaRecentVerify`，行号漂移 23-30 → 27-33）；
  - 但主路径已切 Redis：`MarkMFAVerified` 先写 `cache.GetGlobalCache().Set`（`mfa.go:56`），
    Redis 写失败才回退内存（`mfa.go:57-61`）；`IsMFAVerifiedRecently` 先读 Redis（`mfa.go:85-95`），
    读失败/无值才回退内存（`mfa.go:97-103`）。
- 结论：多实例下**主语义已跨实例共享**，「恒进程内」不成立。剩余约束收窄为 **Redis 故障降级窗口内
  的可用性问题**（实例间回退 map 不互通，方向 fail-closed：另一实例要求重新二次验证），
  不适用「阈值 ×N」口径。已从「必须 Redis 化」清单移入「语义登记」清单（§3.4 / §5.4）。

### 2.5 对照：全站 RateLimitMiddleware 已 Redis-aware（已覆盖）

- `ratelimit.go:265-266`：`RateLimiter.Allow` 首行 `if !cache.GlobalIsRedis()` 分支——Redis 部署走
  分钟键 `mtk:ratelimit:{clientKey}:{minute}` 的 `Incr` 计数（`ratelimit.go:270-277`），
  计数后端异常 **fail-open 放行**（`ratelimit.go:273-275`）；非 Redis（单机）部署退回进程内桶。
- 全局挂载：`router.go:248` `r.Use(middleware.RateLimitMiddleware(...))`（默认 1000 RPS / 20000 bucket，
  装配期读参，豁免 `/api/bridge/ingest` 与 `/api/ws/channel`）。

## 3. 约束与影响量化（N 实例）

前提：负载均衡按请求均匀分发（k8s Service / HPA 默认轮询或随机）。按源 IP 哈希的粘性会话
可缓解以下退化，但**不根治**——扩缩容、实例摘除、节点重启都会触发 rehash，粘性失效后退化立即恢复。

### 3.1 brute force（资金/数据安全线）

| 影响 | 单实例语义 | N 实例语义（无粘性） |
|------|-----------|---------------------|
| 触发锁定所需失败次数 | `MaxFailures = 5` 次/窗口 | ≈ **N × 5** 次（失败计数被轮询摊薄到各实例） |
| 锁定后的放行率 | 0%（全 429） | **(N-1)/N** 的请求落到未锁定实例，正常放行 |
| 攻击者等效成本 | 基准 | 撞库尝试成本降为 **1/N**，锁定威慑退化为 1/N |

受影响的是 6 个鉴权端点（§2.1）：登录、MFA 验证、注册×2、忘记密码、密码重置。
撞库直接踩**资金/数据安全线**——这是四处中唯一「安全防线失效」级别的影响，Redis 化优先级第一（§7）。

### 3.2 PathQuota 路径配额

- 每客户端每路径实际配额 ≈ **N × RPS**（桶参数取 `ratelimit.path_quotas` 的 RPS/BucketSize，
  均匀分发下各实例各自吐满），昂贵端点保护弱化 N 倍。
- **监控与执行口径劈叉**：Redis 聚合读数键（`rate_quota.go:275-281`）能看到真实全站用量，
  读数口会显示「已超配额」，而判定各实例独立、实际没拦——运维看到告警但 429 不发生，排查成本高。

### 3.3 访客双维限流

- 公开无鉴权面（`/api/chat/public/*`，无 JWT 无 API Key），是滥用面最大的一组入口。
- 每 IP 实际 **20 × N** RPS（防爬虫）、每 channel 实际 **100 × N** RPS（防脚本滥用）；
  爬虫/脚本滥用阈值放大 N 倍。

### 3.4 MFA recent（不适用 ×N，登记降级语义）

- Redis 健康时：宽限状态跨实例共享，无约束。
- Redis 故障降级窗口：各实例回退各自的本地 map（`mfa.go:57-61,97-103`），实例间不互通——
  用户在实例 A 通过二次验证、请求落到实例 B 时被要求**重新二次验证**。方向是 **fail-closed**
  （收紧），属可用性损伤，**无安全敞口**。
- 注意方向差异：全站限流的 Redis 异常是 fail-open 放行（`ratelimit.go:273-275`），MFA 降级是
  fail-closed 拒绝——两条防线的降级语义相反，运维认知要分开建。

## 4. 与全站限流的口径区分（哪些已覆盖、哪些没有）

| 防线 | 判定状态存储 | Redis 部署下多实例 | 证据 |
|------|-------------|-------------------|------|
| 全站 RateLimitMiddleware | Redis 分钟键 `Incr` | ✅ 共享 | `ratelimit.go:265-277`（挂载 `router.go:248`） |
| brute force 防爆破 | 进程内 map | ❌ 各实例独立 | `brute_force.go:24-31` |
| PathQuota 判定 | 进程内桶（Redis 仅聚合读数） | ❌ 各实例独立 | `rate_quota.go:270-283` |
| 访客双维限流 | 进程内 map | ❌ 各实例独立 | `visitor_rate_limit.go:36-44` |
| MFA recent 宽限 | Redis 主 + 内存降级 | ✅ 共享（降级窗口例外） | `mfa.go:52-104` |

范围外（避免误判为漏登记）：触达通道限流（ADR-008，L1-L3）走 service 层后台任务路径，
不是 HTTP 请求中间件，其 Redis 滑动窗口口径见 ADR-008，不在本文清单。

## 5. Redis 化方案（待触发，未实施）

总原则（对齐仓内既有抽象，不新增依赖）：

1. 复用 `internal/cache` 的 `Cache` 接口（`cache.go:9-27`）——`Incr`（分钟计数）、
   `Set`（锁定 TTL）、`SetNX` / `ReleaseLock`（互斥）、`Get` 原语已齐备。
2. 判定范式对齐全站限流 `Allow()` 的「分钟键 `Incr` + `cur <= 上限`」写法（`ratelimit.go:270-277`）。
3. 非 Redis 部署保留现有进程内实现（单实例语义不变，回退分支即现状代码）。
4. 参数读取口径不动：访客四参与全站参数同为装配期读一次（`Restart=true`），Redis 化只改
   状态存储，不改参数热读/重启生效的设计。
5. 进程内清理协程（`brute_force.go:47-87` janitor、`visitor_rate_limit.go:70-97` cleanupLoop、
   `rate_quota.go:213-242` cleanupLoop）在 Redis 分支下退化为无操作（TTL 由 Redis 兜底），
   仅非 Redis 回退分支保留。

### 5.1 P1：brute force Redis 化

- 失败计数键：`mtk:bruteforce:fail:{endpoint}:{ip}:{窗口起点epoch}`，`Incr` + TTL = Window（15min），
  窗口语义对齐现有 `firstAt` 重置（`brute_force.go:182-186`）。
- 锁定键：`mtk:bruteforce:lock:{endpoint}:{ip}`，`Set` TTL = 锁定时长；倍乘 ×1/2/4/8 直接乘进
  TTL（对齐 `brute_force.go:190-201`）。
- 判定：`BruteForceGuard` 先查锁定键，命中即 429 + `Retry-After`（对齐 `brute_force.go:148-160`）。
- 辅助读口同步切：`IsBruteForceLocked` / `GetBruteForceFailureCount`（`brute_force.go:214-239`，
  login_events 风险信息用）改读 Redis；Redis 故障时返回 false / 0（与 fail 口径同向）。
- **fail 口径决策点（实施时必须显式拍板）**：
  - fail-open（对齐全站限流 `ratelimit.go:273-275`）：Redis 故障 = 锁定全失效，登录面可用性不受影响；
  - fail-closed：Redis 故障 = 登录/MFA/密码重置全部 429，安全优先但登录面单点化。
  - 默认建议 fail-open + 显式 `Warnf` 告警打点（与全站口径一致，避免登录面新增单点依赖）；
    若业务方认定撞库风险高于登录可用性，再翻案为 fail-closed。**本文不代拍板**。
- janitor（`brute_force.go:47-87`）在 Redis 分支下无需清理（TTL 兜底）。

### 5.2 P2：访客限流 Redis 化

- 分钟键范式：`mtk:ratelimit:visitor:ip:{ip}:{minute}`（TTL 1min，判定 `cur <= PerIPRPS*60`）、
  `mtk:ratelimit:visitor:ch:{channelID}:{minute}`（`cur <= PerChannelRPS*60`）。
- 键形说明：公开面无租户上下文，键不带租户段（与全站限流 `scopedRateLimitKey` 的租户前缀无关，
  键形稳定）；前缀纳入既有 `mtk:ratelimit:*` 家族，与 §5.3 的判定键、§2.2 的聚合读数键三套键域互不混用。
- 非 Redis 部署回退现有本地桶（`visitor_rate_limit.go:99-121`）。

### 5.3 P3：PathQuota 判定 Redis 化

- 判定键：`mtk:ratelimit:pathq:{path}:{clientKey}:{minute}`（TTL 1min，`cur <= RPS*60`）。
  **必须带 clientKey**（含租户段，沿用 `scopedRateLimitKey`）——与现有跨租户聚合读数键
  `mtk:ratelimit:path:{path}:{minute}`（`rate_quota.go:275-281`，刻意不带租户段）严格分离，勿复用。
- 每 clientKey×分钟一个 Incr 键，替换本地桶判定（`rate_quota.go:283`）；429 响应体与
  `recordQuotaTrigger` 打点逻辑不变。
- 非 Redis 部署回退现有本地桶。

### 5.4 MFA recent：无改造项

- 主路径已 Redis（§2.4）。仅两条登记：
  1. 监控口径：对 `mfa.go` 的 Redis 读失败（触发内存回退的分支）计数告警——降级窗口即多实例
     可用性风险窗口；
  2. 语义口径：降级方向是 fail-closed（要求重新二次验证），无安全敞口；若未来出现
     「降级窗口内放行面扩大」的改造诉求，必须重新评审（当前方向是收紧，无需动作）。

## 6. 触发条件

满足**任一**即按 §7 优先级启动 Redis 化改造：

1. helm chart 的 `autoscaling.enabled` 置 true 且 hpa.yaml 落地（OPS-1 实施），`minReplicas >= 2`；
2. 任何部署形态（手工多副本 / docker 复制 / swarm / k8s）出现 **≥2 个 user-server 实例共享同一
   入口与同一数据层**；
3. 多可用区 / 多节点高可用方案立项（HA_DEPLOYMENT.md 从单机形态扩展为多实例拓扑时）。

不触发说明：

- 单实例部署（当前默认形态）不触发，本文约束在单实例下零影响；
- 改造前置检查：确认部署已启用 Redis（`cache.GlobalIsRedis() == true`）。Redis 未启用的多副本
  部署属配置错误（数据层本就共享不了），应先修配置再做实例扩展。

## 7. 优先级与实施顺序

| 优先级 | 防线 | 理由 |
|--------|------|------|
| **P1** | brute force（§5.1） | 撞库踩资金/数据安全线；锁定退化 1/N 是四处中唯一「安全防线失效」级影响；受影响的是 6 个鉴权端点 |
| P2 | 访客限流（§5.2） | 公开无鉴权面、滥用面最大，但影响是容量/滥用而非直接资金损失 |
| P3 | PathQuota 判定（§5.3) | 昂贵端点保护弱化 N 倍，但全站 Redis-aware 限流（`router.go:248`，1000 RPS 全局帽）已对同流量兜一层 |
| 已覆盖 | MFA recent（§2.4） | 主路径已 Redis 化，无改造项（§5.4 仅登记语义） |

## 8. 平台端 L-6 边界（交叉引用，不重复）

- 平台端同类项（platform-server 的进程内状态多实例约束）由**平台仓任务**单独登记与改造
  （登记号 L-6），其清单、证据与方案以平台仓文档为准，本文不收录、不链接——跨仓相对链接在
  CI 干净检出中必然断链（见 `scripts/check-md-links-offline.py` 的「跨出仓库」判定）。
- 两端共享、应保持同口径的两项框架（各自在自己的仓里成文）：
  1. **触发条件定义**（本文 §6）：两端共用「≥2 实例共享同一入口与同一数据层」这一定义，
     避免一端已改造、另一端仍按单实例假设运行；
  2. **fail-open / fail-closed 决策框架**（本文 §5.1）：两端 Redis 化时各自的 fail 口径可以不同
     （可用性 vs 安全的权衡点不同），但必须各自显式拍板并成文，禁止「默认继承另一端」。

## 9. 后果

**正面**：

- 四处约束逐一落到 文件:行 证据，多实例退化有量化口径（阈值 ×N、锁定 1/N）；
- 触发条件前置成文——扩容决策出现时（HPA 落地 / 多副本立项）改造清单已就绪，
  避免「先扩容、后想起限流语义」的窗口期；
- 优先级把安全线（brute force）排第一，且有明确理由链。

**负面 / 未决**：

- Redis 化**未实施**：本文只登记路线与触发条件，不改任何代码；§5 方案在触发后仍需按
  spec-kit 流程走实现；
- fail 口径（§5.1 决策点）留待实施时评审拍板；
- helm HPA 尚未实现（骨架 `enabled: false`，OPS-1 挂起），触发条件短期不会满足；
- **审计口径修正**：N-3 原「四处恒进程内」经复核修正为「**三处恒进程内**（brute force /
  PathQuota 判定 / 访客限流）+ **一处已 Redis 化**（MFA recent，降级回退仍进程内）」——
  MFA 不适用阈值 ×N 量化，已按反证如实调整（§2.4），不为凑数虚报。
