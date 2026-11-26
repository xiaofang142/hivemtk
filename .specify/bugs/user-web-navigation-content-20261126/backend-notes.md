# 后端导航重组、授权与模块核验

基线：`de4396bc1`；日期：2026-11-26。范围限于后端权威菜单、模块过滤、HTTP 模块补充表及三个独立回归文件。本轮不修改前端/locale，不提交、不推送、不发布，不执行线上数据迁移。

## 交付与权威树

`user-server/internal/model/menu.go` 已直接重排为八个一级入口，前端应消费后端返回的树：

`workspace → customer → aiAgent → reach → knowledge → geoTools → analytics → system`

销售入口、销售智能及电商操作台归客户与销售；主动触达及素材库归触达运营；FAQ、知识库和 SOP 模板归知识中心；SOP 智能体仍归智能体；质量看板归被动应答；自动化中心归客服工作台。MQ 与看板归系统运维，业务账号授权归团队，多账号聚合与客服渠道归渠道与集成；浏览器自动化归系统管理；GEO 独立为顶级，信源补位任务归生产。

保留了全部 250 个节点 key、206 个业务叶子的 key/Path，以及含带页目录在内的 212 个 key/Path。通过读取 `git show de4396bc1:user-server/internal/model/menu.go` 与新树逐项对比确认无增减；回归测试同时固定排序后指纹：

| 基线集合 | 数量 | SHA-256（每行 `key\tPath`，角色集合则每行 key） |
|---|---:|---|
| 业务叶子 | 206 | `91063c0197b374f4af50b3845060bb99cdc81894fae5d0688405b539806c83c5` |
| 全部带 Path 节点 | 212 | `d2ebdb07c746cb5307667249dc3aec4cf6a5a3103ef3d0254352c4c9e20d8e49` |
| admin 完整授权上界 | 250 | `5bc5bccdf69856ea800cff323593d8aa2e9caecd6548048382fb2ed234445ae6` |
| customer_service 完整授权上界 | 60 | `7c9a953a104e2734102d4fe7834c2a313fc41d0e87886f469cf6eb8fd065297a` |
| staff 完整授权上界 | 65 | `5da0c1b34358f782c4bcbeaa88ae43d3cd7a489c3f4c18f3ec7a658921378621` |

客服 40 个、员工 46 个可授权业务叶子的集合也与旧树完全一致。素材库移出原 admin 祖先后，叶子显式设置 admin；FAQ/知识库显式保留 admin/customer_service/staff 上界；客服工作台仍只允许 admin/customer_service，未放宽给 staff；MQ、自动化中心保持显式 admin。

## 线上存量授权：无需迁移

无需改写 `role_menu_permissions`，无需补授新目录，无需批量重新 seed。原因与服务行为如下：

1. 表内保存业务 `menu_key`，不保存父路径；本轮没有删除或重命名 key。
2. `MenuService.grantedKeysFor` 对每次读取执行「已有分配 ∩ EligibleMenuKeys」。侧栏、授权回显及 `GrantProvider` 共用该集合。
3. `FilterMenuTree` 会为命中的叶子保留必要祖先。因此仅保存 `faqKb` 的旧授权也能返回新祖先 `knowledge`，但 `GrantProvider` 和授权回显仍只有 `faqKb`，不会增加知识中心的其他叶子。
4. 只在全表首次为空时使用既有默认 seed；全表非空、当前角色无授权时仍返回空集合，不因重组补权。

`service/menu_navigation_test.go` 覆盖客服/员工多叶子授权、仅 FAQ、仅反馈闭环、存量表中角色零授权；断言侧栏只含原叶子和必要祖先、授权回显与能力面恰为原 key 集合，且 `BulkInsert` 与 `ReplaceRoleKeys` 调用数均为 0。另有遗留越界行测试：普通角色旧行即使含素材库、MQ、自动化中心等 admin 叶子，读取仍无授予，重新授予素材库也被拒绝。

生产 `service/menu.go` 无需改动；没有添加菜单数据迁移。上述结论针对本次菜单重组；没有连接或修改线上授权表。

## 模块过滤与商品依赖

`FilterMenuTreeByModule` 改为逐节点裁剪：纯目录由有效后代决定保留；带 Path 目录的默认页单独按自身模块过滤；若默认页关闭但仍有其他模块后代，则清空该目录 Path、保留可用后代。过滤产生新树，不修改 `SystemMenuTree`。

原算法会把带 Path 目录的模块结果传给全部后代，这会制造未声明的模块条件。本轮移除该传递。当前真实 `aiAgent` 是纯目录，原先就不会因目录所属 ai-agent 被关闭而挡住 inbox 子页；不把这一点误写成旧线上现象。带 Path 跨模块行为由专门回归复现；真实 GEO 也有单独测试：只启用 geo 时返回完整 GEO 顶级入口与默认页，不要求 analytics。

测试另外遍历 24 个模块各自的声明依赖闭包，核对全部业务叶子的可达性只由自身模块决定；只开基础模块时，客服会话、坐席状态、快捷回复、FAQ、话术、LLM 路由、素材库与自动化中心仍可达。

**ModuleMenuBindings、ModuleCatalogMeta.DependsOn、价格、计费周期均未变。** FAQ 仍归基础 knowledge；knowledgeBase/SOP 模板/质量看板仍归 ai-sales；电商仍归 reach-social；销售四页仍归 sales-crm；客服/MQ 仍归 inbox；素材库和自动化中心仍归 core。目录移动没有新增购买条件。

### 导出 seed 的用途

本目录的 `module_catalog_seed.json` 是 `user-server/cmd/gen-module-bindings` 的导出与核验产物：24 模块、7 基础/17 收费、250 节点。它包含模块元数据、依赖和菜单 key 绑定，**不包含菜单标题、Path 或父子关系，也不是菜单授权数据迁移**。八组导航及本次最终标题直接来自新编译的 `SystemMenuTree`；更新平台 seed 不能替代上线后端权威树。

生成命令（在 `user-server/` 执行）：

```bash
go run ./cmd/gen-module-bindings -out ../.specify/bugs/user-web-navigation-content-20261126/module_catalog_seed.json
```

与外仓实际存在的以下两份 seed 比较，24 个 binding 记录完全一致，菜单 key 集合一致；只有顶层 `menu_keys` 的遍历顺序随新树改变，该数组用于集合覆盖校验，平台不从中重建导航顺序：

- `hivemtk-platform/platform-server/config/module_catalog_seed.json`
- `hivemtk-platform/platform-server/internal/service/module_catalog_seed.json`

`.specify` 下的文件不会被任何一端自动加载。平台 `ModuleCatalogService.Seed` 依次读 `MODULE_SEED_FILE`、`config/module_catalog_seed.json`、内嵌 `internal/service/module_catalog_seed.json`；仅将文件放在 `.specify` 下不会改变线上数据。本轮没有改写外仓。

### 线上平台同步前的只读校验

2026-11-26 经 `ssh mtk-prod`，对 `mtk-platform-postgres/platform_db` 执行 `BEGIN READ ONLY` → 查询三表 → `ROLLBACK`；没有执行写入或 seed。结果：

| 核验项 | 导出预期 | 线上实际 | 差异 |
|---|---:|---:|---|
| `module_catalog` | 24 模块 | 24 模块 | 模块 key、category、价格一致；基础模块空计费周期在平台创建时默认为 yearly |
| `module_dependencies` | 53 边 | 53 边 | 缺失 0、多余 0 |
| `module_menu_bindings` | 250 对绑定 | 254 对绑定 | 缺失 0、多余 4、重复 key 0 |

四条多余绑定为 `core → backupEnhanced`、`core → bridgeToken`、`customer → unifiedMessage`、`knowledge → ragMainConfig`。这些 key 在本轮基线和新树中都不存在，是先前菜单清理后留在平台的存量记录，本次重排没有新增这项差异。它们不会从新 `SystemMenuTree` 中生成侧栏或获得 `EligibleMenuKeys` 授权，但平台绑定表与当前注册表确实尚未精确一致，不能宣称平台已同步完成。

**结论：本次无需角色授权迁移，也无需修改已购模块、价格或依赖；平台若验收为精确的当前清单，仍须清除上述四条历史绑定。** 主助手负责平台写入，先将已经核对的 250 节点 seed 同步至线上平台实际生效的读取来源，再调用实际注册的 `POST /platform/module-catalog/seed`。可使用本地两份平台 seed 或本目录导出物；它们的绑定内容一致，但本轮没有验证线上进程实际读取的文件/内嵌版本，不能直接把本地文件一致推断为线上 seed 一致。若有 `MODULE_SEED_FILE`，须优先核对它，避免低优先级文件被覆盖。

该入口不接收上传 JSON 正文，而是读取上述文件来源。`Seed` 对已有模块跳过创建、通过 `ReplaceMenuBindings` 按模块替换绑定，故可清掉四条旧记录；它不会重写已有商品价格，也不改已购授权。`SeedDependencies` 只补边不删边，本次依赖已精确一致，无需额外修正。

平台写入后的验收应再次只读比对线上三表：24 模块、250 对绑定、53 边，绑定缺失/多余均为 0。`GET /platform/module-catalog/seed/coverage` 仅校验读取到的 seed 文件内声明，并不查询线上绑定表，不能替代这项数据库验收。交接时尚未执行平台写入，后续实际同步结果见下节。

### 线上平台目录已同步并查库验收

主助手核实运行容器没有 `MODULE_SEED_FILE` 或磁盘覆盖文件，实际读取内嵌 seed；覆盖接口确认 250 个节点，无重复。备份四张表后，复用平台管理员会话调用 `POST /platform/module-catalog/seed`，返回 HTTP 200、`code: 200`、`目录已同步`、`modules: 24`。

再次只读查库并与备份逐行比较：24 个模块、250 对绑定、53 条依赖、589 条租户模块授权；恰好移除上述四条绑定，没有新增绑定。模块目录（含价格）、依赖和授权全部与备份一致。备份和前后快照位于 `/www/wwwroot/hivemtk/platform-server/backups/navigation-20261126-084141/`；`module-catalog-before.sql` SHA-256 为 `04a5170cf8c13b15ea73d6fa46e5dc4d6d0c01b0cf45be2bfbbaf9bf87950189`。未修改平台源码或 seed 读取配置。

### 应用启动同步的方向与覆盖范围

- 用户端 `cmd/api/main.go` 在装配路由前调用 `StartModuleSync` / `WaitFirstSync`。首轮与后续周期同步调用 `ModuleCatalogService.Sync`，通过 `GET /merchant-api/modules/sync` **从平台拉取**，写本地 `tenant_modules` 镜像。它既不上传本地 seed，也不修改平台 `module_catalog` / `module_menu_bindings` / `module_dependencies`，所以重启用户端不能清除平台那四条旧绑定。
- 离线 `bootstrapOffline` 在已有本地数据时仅补缺失赠送模块，保留已有行；同样不负责平台目录或角色授权迁移。
- `MenuService.GetUserMenuTree` 直接对编译期 `SystemMenuTree` 先裁模块再裁角色，菜单位置不依赖镜像的 `menu_keys`；`MyModules` 也优先返回编译期 `ModuleMenuBindings`。因此没有任何需要靠启动同步来补救的本次 key/归属变化；线上平台历史残留应通过上一节的显式目录同步纠正。

## CRM / 线索治理页内入口与 HTTP 门禁

CRM 主数据及线索治理叶子均保留 admin 上界。现有 `MenuCapabilitySupplement` 已覆盖 CRM 客户/联系人/供应商/服务商、去重、合并审计、导入导出、360、跨行业查询，以及线索规则、通知路由、消息/客户、判定命中和线索智能体读口；线索写口继续受既有 admin 路由策略及 `AdminAuthMiddleware` 保护。

补充测试向真实 `MenuService.GrantProvider` 注入带越界 key 的旧分配行，再经 `RoutePolicyGuard` 发出 18 类代表请求：客服与员工为 HTTP 403 且不执行业务 handler，admin 通过授权门禁。现有 router 集成门也已验证生产装配的真实守卫链、能力规则可命中及菜单授权驱动接口访问。

核验发现模块路径表此前漏收 CRM、线索治理与电商命名空间，已在原有 `middleware/module_route_supplement.go` 补齐：

| HTTP 命名空间 | 模块 |
|---|---|
| `/api/manage/crm/**` | customer |
| `/api/manage/leads/**` | customer |
| `/api/manage/lead-mining-agents/**` | customer |
| `/api/lead-mining/**` | customer |
| `/api/v1/commerce/**` | reach-social |

能力面归属、菜单模块、HTTP 模块三处一致。模块执行回归明确设为多租户模式后验证关闭时不执行业务、开通后通过；私域 `TENANT_MODE=off` 的既有订阅闸放行口径保持不变。

## 前端需使用的标题

本轮仅改后端中文兜底标题，前端 locale 由主助手同步：

| key | 标题 |
|---|---|
| customer | 客户与销售 |
| aiAgent | 智能体与客服 |
| system | 系统管理 |
| geoTools | GEO 增长 |
| agentSupport | 工具与 SOP |
| siteConfig | 系统配置与运维 |
| teamGroup | 团队与权限 |
| automationCenter | 客服自动化 |
| geoCrawlerStats | UA 探测记录 |
| permissionGroup | 渠道与集成 |
| geoBrandConfig | AI 爬虫访问文件 |
| geoConfig | 品牌与站点资料 |
| geoDecisionReport | 信源补位任务 |
| emailSmtp | 发件账号 |
| emailInfo | 邮箱服务商参考 |

## 验证证据

先红：`go test ./internal/model -run '^TestMenuNavigation_' -count=1` 真实报出七组、旧标题、素材库缺显式 admin 以及带页目录连带裁剪错误；新增模块归属断言进一步报出 CRM/线索治理/电商的 HTTP owner 为空。修复未通过改写预期绕过。

已通过的检查：

- 新增导航回归（model/service/middleware 三包）；完整叶子/Path、全部角色上界、存量叶子授权、模块依赖闭包、CRM/线索 admin 门禁、能力面与 HTTP owner 均有断言。
- 三包全量 `go test ./internal/model ./internal/service ./internal/middleware -count=1`：model `13.852s`、service `417.441s`、middleware `40.520s` 全部通过；在本机 `127.0.0.1:8217` 注入测试库密码，未打印秘密。该全量运行早于最后三项标题调整，最终标题另由同名导航回归检查。
- 最终名称对齐后的扩大定向回归：`go test ./internal/model ./internal/service ./internal/middleware -run '^Test(MenuNavigation_|MenuService_|ModuleMenuBindings|FilterMenuTreeByModule|DefaultMenuKeys|EligibleMenuKeys|ModuleDepends|ClosureOfModules|ModuleCatalog|ModuleRoute|ModuleOwnerOf|CapabilityGrant|RoutePolicyGuard|MarketAPIs|SelfProvidedLLM)' -count=1`：model `0.633s`、service `2.101s`、middleware `1.632s` 全部通过。
- 最终名称对齐后重新执行 `go build ./... && go vet ./...`，退出 0。
- `go test ./internal/router -run '^(TestRouteGuardChain_(Capability|AdminOnly|StaffRejected)|TestGeoRoutesAllModuleGated|TestModuleRoutePatternsCoverShippedRoutes|TestRBACMenuGrants_)' -count=1`：`ok hivemtk-user/internal/router 2.699s`，使用本机测试库凭据，未接触线上。
- `python3 scripts/check-layer-boundary.py`：1785 个非测试文件，违规 0。
- `bash scripts/check-architecture.sh`：通过；既有 service interface 警告单独保留。
- `bash user-server/scripts/check-gorm-only.sh`：业务包 `.Exec`/`.Raw` 违规 0。
- `git diff --check`：通过。

限制与未通过项：

- 一次扩大筛选条件的检查选中了三个无关 DB 集成子用例，因该命令未注入测试库密码触发 `test password is missing` 安全门；未发起连接，不属于本次菜单回归失败。
- 裸 goroutine 门禁仍报 61 处；逐文件与 HEAD 比较没有新增，均在本轮修改面外。未扩展修改其他会话文件，不宣称该全仓门禁通过。
- 按全局要求尝试 graphify 重建，环境报 `ModuleNotFoundError: No module named 'graphify'`；未安装新依赖。
- 本后端任务未执行浏览器点验；页面集成由主助手继续，不把 HTTP 单测视为线上 UI 验证。
