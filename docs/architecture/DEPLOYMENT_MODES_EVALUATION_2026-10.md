# 部署形态评估：SaaS 多租户 / 离线单机 / 与平台联动（2×2 矩阵）

> 日期：2026-10-20 ｜ 范围：`user-server` 全量 Go 代码 + 部署开关装配 + 两套部署文档
> 事实源：`specs/001-saas-multi-tenant/`、`docs/architecture/部署方案_用户端.md`、
> `docs/architecture/SaaS_MULTI_TENANT.md`、`hivemtk-platform/docs/architecture/部署方案_平台端与用户端.md`
> 性质：**评估 + 本轮已闭环项**。第 6 节方案需批准后走 spec-kit 施工；第 3 节缺陷本轮已修。

---

## 0. 一句话结论

**"两套部署方案"这个提法本身不准确**——部署形态由**两个正交开关**决定，实际是 **4 种组合**，
不是 2 种。这不是吹毛求疵：漏掉的那一格（`TENANT_MODE=on` + `PLATFORM_ENABLED=false`）
在现有文档里**从未被描述过**，而它恰好是最容易出问题的格子（SaaS 隔离生效、但平台侧能力全空）。

**隔离机制本体是对的**——业务代码零手工隔离，判据唯一（表有没有 `tenant_id` 列）。
本轮真正的问题不是"设计错了"，而是三类：

| # | 问题 | 性质 | 状态 |
|---|---|---|---|
| 1 | 文档把部署形态写成"一项开关"，漏掉 `PLATFORM_ENABLED` 那一轴 | 事实源漂移 | **已修** |
| 2 | `PLATFORM_ENABLED` 拼错 ⇒ 启动日志**说谎**（说"未设置"，实际是"拼错了"） | 可运维性 | **已修** |
| 3 | 4 种组合里有一格（`on+false`）的降级契约**从未被写下来** | 契约缺失 | 本轮补齐（本文 §2.2） |

**"降侵入"的空间已经不大**（[SAAS_MULTI_TENANT_EVALUATION_2026-10.md](SAAS_MULTI_TENANT_EVALUATION_2026-10.md) §3 有量化：
真正冗余的隔离代码 ≈ 0–3 处），**"降耦合"的重点应放在"能力耦合"**——即"谁能豁免隔离"，
目前靠约定而不是靠类型。那份文档已论证，本轮不重复。

---

## 1. 角度一 · 形态识别：部署形态不是"两套方案"，是 2×2 矩阵

### 1.1 两个开关的真实位置

| 开关 | 解析点 | 取值 | 决定什么 |
|---|---|---|---|
| `TENANT_MODE` | `internal/pkg/tenant/tenant.go:46` `ResolveMode` | `off`（默认）/ `on` | **租户形态**：off=离线单机（全数据归默认租户 1）；on=SaaS 多租户（自助开户 + 行级隔离 + 后台按租户扇出） |
| `PLATFORM_ENABLED` | `internal/config/platform.go:85` `PlatformEnabled` | `false`（默认）/ `true` | **平台联动**：false=纯本地（不加载平台配置、不注册商户、不心跳、**零出站请求**）；true=向 platform-server 上报安装/心跳/开户登记，并启用资产市场/模块下发/LLM 钱包 |

两者的分支点数（生产代码，排除测试）：

| 开关 | 分支点数 | 分布 |
|---|---|---|
| `TENANT_MODE` | **91** | service 48、controller 11、middleware 9、pkg/tenant 5、repository 3、app 3、router 2、其余 10 |
| `PLATFORM_ENABLED` | **26** | service 20、config 3、platform 1、pkg/shared/service 1、controller 1 |

### 1.2 4 种组合的真值表

| | `PLATFORM_ENABLED=false` | `PLATFORM_ENABLED=true` |
|---|---|---|
| **`TENANT_MODE=off`**<br>（离线单机） | **① 纯离线单机**（默认形态）<br>零出站请求；不加载平台配置、不注册商户、不心跳；租户自助注册关闭（业务码 4104）；`/setup` 向导可用 | **② 单机 + 联平台**<br>① 的一切 + 向 platform-server 上报安装信息与心跳；资产市场可用 |
| **`TENANT_MODE=on`**<br>（SaaS 多租户） | **③ SaaS 无平台联动** ← *本文新增的一格*<br>自助开户可用；租户隔离生效；开户登记标 `platform_sync=skipped`（**一个出站请求都不发**）；模块市场/订单/LLM 钱包按"平台未配置"降级 | **④ 云端 SaaS 全功能**<br>自助开户 + 租户隔离 + 资产市场 + 模块下发 + LLM 钱包 + 安装/心跳上报 |

> **形态①与形态④是"两套部署方案"这个提法想表达的东西**；
> 形态②与形态③是两条**没人写过文档、也没人跑过验证**的中间路径。

### 1.3 两个开关的错向**故意不对称**（这点很重要，别当成 bug 修掉）

| 开关 | 拼错的后果 | 处理 | 判据 |
|---|---|---|---|
| `TENANT_MODE` | SaaS 部署降级成单机 ⇒ **数据损坏**（所有租户的数据写进默认租户 1，全程不报错） | **拒绝启动**（fail-closed） | `tenant.InitFromEnv` 返回 error ⇒ `main.go:127` `log.Fatalf` |
| `PLATFORM_ENABLED` | 只是少一个可选功能（资产市场/模块下发/心跳），业务数据完好 | **保守关闭**（fail-safe，不影响启动） | `PlatformEnabled()` 非真值 ⇒ false |

**为什么不对称是对的**：
`PLATFORM_ENABLED` 拼错绝不能让一台本地私有化部署**因为一个可选组件的开关而起不来**，
也绝不能因为拼错就**朝一个可能已下线的地址发请求**（`PlatformURL()` 关闭态恒空串，
且有 `TestPlatformURLNeverFallsBackToAnyDomain` 钉死"绝不回落任何默认域名"）。
`TENANT_MODE` 拼错则是数据损坏，必须拒绝启动。

> 这条差异**已有测试锁住**（`internal/config/platform_enabled_test.go:34` 明确把 `tru`/`ture` 列为"应判关闭"）。
> 本轮**没有**改这个判真方向——改它会破坏一条正确的既有行为锁。

---

## 2. 角度二 · 2×2 四格的行为契约（本轮补齐文档缺失的一格）

### 2.1 形态①（off + false）· 纯离线单机

| 维度 | 行为 | 代码依据 |
|---|---|---|
| 出站请求 | **零** | `main.go:366` `PLATFORM_ENABLED` 关 ⇒ 整条平台链路不装配（不读配置、不注册商户、不心跳） |
| 租户上下文 | 全部请求绑默认租户 1 | `middleware/tenant.go:27` off 分支 `ContextWithDefault` |
| 隔离 callback | 直通，SQL 与历史版本逐字节等价 | `pkg/tenancyidx/tenant_callback.go` off 模式旁路 |
| 自助注册 | 关闭（业务码 4104） | `service/tenant.go:97` `CodeTenantRegisterClosed` |
| 单机注册 | **开放**（登录页 `/auth/register`） | `controller/auth_register.go:35` |
| 初始化 | `/setup` 向导（`install.lock` 判"已初始化"） | `middleware/init_guard.go:80` |
| 资产市场/平台上报类端点 | 403 + 一句照着做就能开通的话 | `controller/asset_market.go:56` `rejectPlatformDisabled` |
| 模块市场 | 灌入**赠送集**（基础模块 + 全部 0 元付费模块） | `service/module_catalog.go:343` 离线降级 |

### 2.2 形态③（on + false）· SaaS 无平台联动 ← **本轮新增的契约**

这一格此前**只存在于代码里，不存在于任何文档**。补齐如下：

| 维度 | 行为 | 代码依据 |
|---|---|---|
| 出站请求 | **零** | 同形态①（平台链路不装配） |
| 租户隔离 | 生效（`TENANT_MODE=on` 的全部机制） | `pkg/tenancyidx` |
| 自助开户 | **开放**（`POST /api/tenant/register`） | `service/tenant.go:96` |
| 开户登记上报平台 | **不发请求**，标 `platform_sync=skipped` | `service/tenant.go:456` `PlatformEnabled()` 假 ⇒ 标 skipped |
| 开户**不**被平台不可达阻断 | 平台失败只标 `failed` + 记日志，**不回滚注册** | `service/tenant.go:497` / `service/auth.go:829` |
| 开户赠送 | 失败仅 Warn 不阻断（赠送可后补，开户不可逆） | `service/tenant.go:200` `provisionTenantStarterKit` |
| 资产市场写操作 | 403 + 开通指引 | `controller/asset_market.go:56` |
| 模块市场 | 同形态①灌赠送集 | `service/module_catalog.go:343` |
| LLM 钱包/套餐 | 平台未配置 ⇒ 降级（`ErrPlatformNotConfigured`） | `internal/platform/disabled_client.go` |
| 后台任务 | 按**活跃租户扇出**（不是单租户） | `pkg/tenant/fanout.go:50` |
| 初始化态 | 恒"已初始化"（无单机 setup 概念） | `middleware/init_guard.go:45` / `controller/system_init.go:49` |
| `install.lock` | **不参与**（install.lock 只服务形态①的 setup 向导） | 同上 |

> **这一格的正确定位**：它是"SaaS 隔离 + 无商业化"的组合——租户自助开户能用，
> 但模块市场/资产市场/LLM 钱包这些**平台下发的商业能力**全空。
> 语义上等于"纯 SaaS 隔离内核，不接商业化"。**必须能跑通且不卡死**，
> 否则一个"买了隔离没买商业化"的客户会装完就发现什么都用不了。

### 2.3 形态②（off + true）· 单机 + 联平台

形态①的一切 + 平台联动的全部能力。**注意它与形态①的唯一结构差异**：
`tenant.IsMultiTenant()` 恒假 ⇒ 隔离 callback 直通、租户上下文恒默认租户 1、
自助开户关闭；但 `PLATFORM_ENABLED` 为真 ⇒ 安装上报/心跳/资产市场/模块下发全部启用。

**这格最容易出事的点**：`off+true` 时商户注册请求会走 `registerMerchantAs`（`internal/platform/sync.go:91`），
带**商户签名**打向平台 —— 而平台的 `/merchant-api/*` 全部挂 `MerchantAuth()`。
签名密钥对不上时注册失败，但那只是 `logger.Warn`（`sync.go:96` 明确写"可忽略"），**不影响启动**。

### 2.4 形态④（on + true）· 云端 SaaS 全功能

形态③的一切 + 平台联动的全部能力。开户口 `/api/tenant/register` 会同步上报登记信息到平台
（`service/tenant.go:491` `RegisterTenant`），**失败不回滚**（标 `failed`，运营可手工补录）。

---

## 3. 角度三 · 本轮修的两处缺陷

### D1（已修）`PLATFORM_ENABLED` 拼错 ⇒ 启动日志说谎

**症状**：运维把开关拼成 `PLATFORM_ENABLED=treu`，启动日志打的是
`[启动] 平台集成关闭（PLATFORM_ENABLED 未设置）`——**这句话是假的**。运维据此认定"平台没接"，
于是去查平台端、查网络、查账号，**永远不会想到是自己的开关名拼错了**。
而真相是：整个平台集成（商户注册、资产市场、模块下发、LLM 钱包、安装上报）**静默失效**。

**为什么不能改成"拼错就拒绝启动"**：
`PLATFORM_ENABLED` 拼错只导致**少一个可选功能**，业务数据完好；
一台本地私有化部署不该因为一个可选组件的开关名拼错就起不来。
判真方向必须朝"关"——这条已被既有测试
`TestPlatformEnabledAcceptsOnlyTrueLike`（`platform_enabled_test.go:34`）锁住，
本轮**没有**动它。

**修法**（保持判真方向不变，只让日志说实话）：
- 新增 `config.ResolvePlatformEnabled(env) (enabled bool, known bool)`：
  已知关闭（`false`/`off`/`0`/`no`/空）⇒ `(false, true)`；
  已知开启（`true`/`on`/`1`/`yes`）⇒ `(true, true)`；
  **拼错** ⇒ `(false, false)`。
- `PlatformEnabled()` 改为它的薄封装，**判真结果逐位不变**（`TestPlatformEnabledMatchesResolvePlatformEnabled` 钉死两者永不分叉）。
- 启动日志分两句说：已知关闭 ⇒ 原来的 Info；**拼错 ⇒ Warn 并点名实际值** +
  说明"需要联平台请改成 true —— 拼错会让商户注册、资产市场、模块下发全部静默失效"。

**回归**：`internal/config/platform_enabled_resolve_test.go`（2 例，三态语义 + 两入口不分叉）。

### D2（已修）部署文档漏掉 `PLATFORM_ENABLED` 那一轴

`docs/architecture/部署方案_用户端.md` §十一 写的是"由 `.env` **一项开关**决定"，
只覆盖 `TENANT_MODE`。而 `PLATFORM_ENABLED` 决定的是**出站行为**——
对"这套系统会不会往公网发请求"这个最敏感的问题，文档给的是**不完整**的口径。

**修法**：§十一 补 §十一.1，写清 2×2 矩阵、四个组合的行为差异、
两个开关**为什么故意不对称**（错向不同 ⇒ 处置不同），并把拼错的日志行为写进去。

---

## 4. 角度四 · 装配时序与 fail-closed 口径

### 4.1 两个开关的装配时点

| 开关 | 装配点 | 必须在什么之前 | 漏装的后果 |
|---|---|---|---|
| `TENANT_MODE` | `cmd/api/main.go:126` `tenant.InitFromEnv()` | `db.InitDB` / `router.Setup` | 进程永远停在默认 off，环境变量**静默失效** |
| `PLATFORM_ENABLED` | `cmd/api/main.go:366` `config.PlatformEnabled()` | `config.LoadPlatform` | 同上（平台链路不装配） |

`TENANT_MODE` 的拼错 ⇒ `log.Fatalf` **拒绝启动**（`main.go:127`）；
`PLATFORM_ENABLED` 的拼错 ⇒ 保守关闭 + Warn（§3 D1）。

### 4.2 各层 fail-closed 的一致性

| 场景 | 行为 | 依据 |
|---|---|---|
| `on` 模式 ctx 缺租户 | 报错（隔离 callback 拒绝执行） | `pkg/tenancyidx` |
| `on` 模式租户解析失败（伪造回调/越权令牌） | 拒绝 + 安全日志，**绝不落默认租户** | `middleware/public_tenant.go:52` |
| `on` 模式后台扇出枚举器未装配 | 拒绝执行（不静默跑全表） | `pkg/tenant/fanout.go:58` |
| `on` 模式 `install.lock` 缺失 | **不**判"未安装"（否则干净浏览器首访被推进 /setup） | `controller/system_init.go:49` |
| 平台不可达（开户登记） | **不**回滚注册，只标 `failed` | `service/tenant.go:497` |

> 注意最后一条与前几条**方向相反**且是**有意的**：
> 隔离是**安全边界**（错 = 数据串租户 ⇒ 拒绝）；平台登记是**商业联动**（错 = 少一条上报 ⇒ 不阻断）。
> 两者混为一谈才会写出"平台不可达 ⇒ 开户失败"这种把可选依赖变成硬依赖的错误。

---

## 5. 角度五 · 性能与可运维性

| 维度 | 现状 | 判断 |
|---|---|---|
| 每次查询开销 | callback 多注入一条 `WHERE tenant_id = ?`；登记表是 map 查表，O(1) | 可忽略 |
| 索引可用性 | 唯一索引已复合为 `(tenant_id, 原列…)` ⇒ 租户过滤走**索引前缀** | 良好 |
| 非租户表 | 17 张平台表不注入条件 | 良好 |
| 写放大 | Create 多一次 `field.Set`（反射） | 可忽略 |
| 形态切换开销 | 切换 `TENANT_MODE` **不需要**改数据（存量行已带 `tenant_id`，默认租户 1） | 零迁移成本 |
| 开关可观测性 | `TENANT_MODE` 启动时打 Info；`PLATFORM_ENABLED` 打 Info/Warn（**本轮修**） | 已补齐 |

**结论**：性能不是当前矛盾，不构成重构理由。

---

## 6. 降侵入 / 降耦合的方案矩阵（承接多租户评估的主线）

部署形态这一层的"降侵入"已基本到位（形态由**环境变量**决定，代码里**零**形态分支的
`if deploy == "saas"` 式硬编码）。剩下的耦合是**能力耦合**——与多租户评估 §4-(ii) 同源，
不在本文重复论证。部署形态层面唯一还有价值的一步是：

| 方案 | 内容 | 成本 | 判断 |
|---|---|---|---|
| **E. 形态矩阵契约门禁** ✅ **已落地** | 断言 4 种组合的**关键判据**（零出站、开户码、隔离注入、降级提示）符合本文 §2 的表格 | 已交付 | `user-server/internal/app/deployment_matrix_contract_test.go`：四格 × 六条行为（出站 / 租户上下文 / 自助开户 / 初始化态 / 后台扇出 / 隔离 callback）。两个轴正交（租户那五条在两列数值必须相同）也是断言的一部分 |
| F. 部署形态显式化 | 引入 `DeploymentMode` 枚举（`saas`/`standalone`/`saas-standalone`…）替代两个布尔 | 高：91 + 26 个分支点要重写 | ❌ 性价比不成立 |
| G. 每形态一套配置模板 | 提供 4 份 `.env` 模板 | 低 | ⏸ 可选，文档已能表达 |

---

## 7. 本轮已交付 vs 待批准

### 已交付（本轮闭环，已进回归）

| 项 | 位置 |
|---|---|
| `PLATFORM_ENABLED` 拼错 ⇒ 启动日志说谎（**已修**） | `internal/config/platform.go` 新增 `ResolvePlatformEnabled`；`cmd/api/main.go` 启动日志分两句说 |
| 三态语义回归（2 例） | `internal/config/platform_enabled_resolve_test.go` |
| 部署文档漏 `PLATFORM_ENABLED` 那一轴（**已修**） | `docs/architecture/部署方案_用户端.md` §十一 + 新增 §十一.1 |
| 2×2 矩阵的**行为契约补齐**（形态③首次成文） | 本文 §2 |
| 2×2 矩阵的**行为契约变成可执行断言**（形态③④不再只是文档） | `user-server/internal/app/deployment_matrix_contract_test.go` |
| **跨租户越权**（**已修**，对抗性跑出来的） | `internal/middleware/platform_operator.go` + `internal/router/router.go` + `internal/service/tenant.go`；见 `.specify/bugs/cross-tenant-admin-privilege-escalation/`（档案已移出当前工作区，见 git 历史） |
| **守卫过度收紧**（**已修**，二次审核打出来的） | admin 档按数据域拆两档：`internal/router/router.go` + `system_routes.go` + `module_routes.go`；门禁 `internal/router/platform_operator_scope_test.go`；见 `.specify/bugs/platform-operator-guard-over-tightened/`（档案已移出当前工作区，见 git 历史） |

### 待批准（本文第 6 节）

（原「方案 E 形态矩阵契约门禁」已落地，见上表末行与 `internal/app/deployment_matrix_contract_test.go`。）
方案 F/G 维持不采纳（F 性价比不成立；G 文档已能表达）。

> **注意两份真相**：本节「行为契约表」与门禁
> `internal/router/platform_operator_scope_test.go` 的「端点归属表」是**两张独立的表**。
> 后续若要收口，应让门禁直接读本文的表格（或反过来由表格生成门禁），
> 否则一处改了另一处没改 —— 而这类漂移**编译照过、单测照绿**，只有活体才看得见。

---

## 9. 2×2 矩阵的本地实跑结果（2026-10-21）

四格全部在**真实 PG + 真实 user-server（8204）**上跑通，每格都验了形态判据端点与该形态特有的行为。

| 格 | TENANT_MODE | PLATFORM_ENABLED | 启动 | 形态判据 | 关键行为验证 |
|---|---|---|---|---|---|
| ① | off | false | ✅ | `tenant_mode=false` / `edition=opensource` | 自助开户 ⇒ **4104**（注册未开放）；单机注册 ✅ 成功（JWT `tenant_id=1`）；**零出站**（日志只有一条"平台集成关闭"） |
| ② | off | true | ✅ | `tenant_mode=false` | 平台配置装配 `api_url=https://manageapi.xapptool.cn`；**心跳上报成功**（免签通道 ⇒ 线上平台 200）；签名通道 401 且**不阻断启动** |
| ③ | on | false | ✅ | `tenant_mode=true` / `edition=saas` | 自助开户 ✅（租户 5/6）；`platform_sync=skipped` + **零出站**；`init-status` 恒 `INITIALIZED` |
| ④ | on | true | ✅ | `tenant_mode=true` | 自助开户 ✅（租户 7）；心跳上报成功；开户登记上报平台 ⇒ 签名通道 401 ⇒ `platform_sync=failed`（**记录了原因，但注册不阻断**） |

### 9.1 跑出来的真缺陷：跨租户越权

形态③/④ 的对抗性检查打出一处**真实跨租户越权**（非环境问题）：

> 任意**租户**的 admin 都能 `GET /api/manage/tenants`（看到全部租户的
> 公司名/负责人/电话/邮箱/地址）、`PATCH /api/manage/tenants/:id/status`（**停用其他租户**）、
> `POST /api/system/restart`（**重启整个实例**）。

根因：「admin」这一个角色在 SaaS 模式下承载了两种语义（平台运营方 / 租户管理员），
而 `systemAdmin` 组的守卫只判 `role == "admin"`——租户 admin 也是 admin。
路由注释本来就写着这些端点"**必须硬超管守卫**"——守卫是漏的，不是设计如此。

**注意区分**：**行级隔离机制本身没破**——`system_config` 只读到 `tenant_id=6`、
`system/stats` 的 `total_users=1`（全库 21 个用户）。泄露面**只在全局表**
（15 张无 `tenant_id` 的表，`tenants` 是其中最要命的一张）**与实例级操作**上。

**修法**（用户已确认方向：平台超管 = 首个租户）：
新增 `middleware.PlatformOperatorMiddleware()`——角色判定**之上**再加一道
"调用者所属租户是不是本实例首个租户（`tenants` 表 ID 最小）"；
`off` 模式恒真（离线单机零回归）；判定不出首租户 ⇒ **拒绝**（权限边界上"查不到"不等于"放行"）。
另加**死锁保护**：首租户不允许被停用（否则平台运营方把自己锁在门外且无人能救），但允许重新启用。

完整评估与三件套：`.specify/bugs/cross-tenant-admin-privilege-escalation/`（档案已移出当前工作区，见 git 历史）。

### 9.2 线上平台的实测口径

| 通道 | 签名 | 实测 |
|---|---|---|
| `POST /api/platform/install` | 免签 | ✅ 200（**但 payload 字段不全会 500**「保存安装信息失败」，带全字段才 200） |
| `POST /api/platform/heartbeat` | 免签 | ✅ 200（本地形态②/④ 实跑「上报心跳成功」） |
| `/merchant-api/*` 全组 | HMAC-SHA256 + `X-Merchant-Key`/`X-Timestamp`/`X-Signature` | ❌ **401「签名错误」**——线上**活的** `MERCHANT_API_SECRET` 与本仓 `hivemtk-platform/.env` 里那份**不一致**（两种签名都 401） |

⇒ 本地联线上平台时：**免签通道可通**；签名通道必然 401，除非拿到线上真实 secret。
**这条不影响本地启动**——商户注册失败只 `logger.Warn`（`internal/platform/sync.go:96` 明确写"可忽略"），
开户登记失败也只标 `platform_sync=failed` 不回滚注册。
**这正是要验的那条真不变量：平台联不上时，SaaS 开户与隔离仍然完整可用。**

### 9.3 顺带修的编译红

跑 2×2 矩阵时发现本仓工作区有**另一个 agent 进程并发改 `internal/ops/**`**
（A/B 实验仓储的 ctx 化重构），中途留下一个编译红：
`ABExperimentResult` 模型缺 `CreatedAt` 字段（库里 `created_at` 列存在且 NOT NULL，
但 Go 模型没声明，同文件其余三个模型都声明了）。按"发现即修"补上该字段后编译恢复。

### 9.4 二次审核打出的第二处缺陷：守卫**过度收紧**（误伤租户内管理功能）

§9.1 的越权修复方向是对的（跨租户确实堵住了），但**施加范围错了**：
守卫被套在**整个 `systemAdmin` 路由组**上，而那一组里混着操作**租户表**的端点。
二次审核拿**真实租户令牌**复验，8 条端点被误打成 403：

| 端点 | 落库 | 归属 |
|---|---|---|
| `/api/system/config` | `system_config`（租户表） | 租户档 |
| `/api/agent/tool-integrations` | `system_config_kv`（租户表） | 租户档 |
| `/api/agent/settings` | `system_config_kv`（租户表） | 租户档 |
| `/api/obs/config` | `obs_config`（租户表） | 租户档 |
| `/api/manage/config-params` | 平台目录 + **本租户覆盖** | 租户档 |
| `/api/admin/config` | 进程静态配置 | 租户档 |
| `/api/modules/sync-logs` | `module_sync_logs`（租户表） | 租户档 |
| `/api/sso/config` | `system_config_kv` 的 `sso.providers`（租户表） | 租户档 |

⇒ **租户管理员在自己的租户里，除业务功能外所有管理配置页面全废。**

**根因（本仓最值得记住的一条）**：
**守卫按"路由组"施加，而权限边界实际按"数据域"划分。**
「是不是平台运营方」与「这个端点该不该被平台运营方守卫」是**两个不同的问题**。

而且两个方向的代价**不对称**：平台运营方守卫是"降敌意"方向——
套错档位会**误伤合法功能**（本轮），漏套会**漏放越权**（§9.1）。
所以两档都必须按**端点操作的数据落在哪张表**来分，不能按"这路由组看起来像管理端"分。

**修法**：admin 档按数据域拆成两档（`tenantAdmin` 普通 admin 守卫 /
`systemAdmin` 平台运营方守卫），并加**常驻门禁**把归属钉成两张表
（`internal/router/platform_operator_scope_test.go`）——
门禁做了**反向验证**（把端点挪错档位 ⇒ 必须红），否则它只是一张摆设的清单。

**为什么这类回归常规测试抓不到**：守卫本身是对的、编译照过、既有单测照绿；
只有**拿真实租户令牌打活体端点**才看得见。

完整评估：`.specify/bugs/platform-operator-guard-over-tightened/`（档案已移出当前工作区，见 git 历史）。


---

## 8. 附：本轮执行的命令与判定口径

```bash
# 编译与静态
cd user-server && go build ./...
make lint          # golangci-lint 2.10.0
make vet
make audit

# 2×2 矩阵的本地实跑（前置任务三）
# 形态判定端点：GET /api/public/config  → {tenant_mode, edition, cloud_url, enable_register}
# 形态④/②：POST /api/platform/install + /api/platform/heartbeat 走免签通道上报线上平台
# 形态③/①：断言零出站（安装上报不发生）
```

**环境注记**：
- 本机 `golangci-lint` 装在 `$(go env GOPATH)/bin`，**不在 PATH** ⇒ `make lint` 报
  "本机没有 golangci-lint"。需 `export PATH="$(go env GOPATH)/bin:$PATH"`。
- 仓库 `.env` 的 `POSTGRES_PASSWORD` 是占位符，与本机容器 `mtk-postgres` 的真实口令**不一致**；
  测试库口令须从容器取（见仓规 CLAUDE.md §3.5）。
