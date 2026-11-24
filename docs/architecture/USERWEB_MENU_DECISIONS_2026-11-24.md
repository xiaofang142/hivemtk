# user-web 菜单头脑风暴决策与最新菜单清单（2026-11-24）

> 配套：[全页面路由清单](./USERWEB_ROUTE_INVENTORY_2026-11-24.md)（93 模块 / 266 路由）。
> 事实源：前端 `user-web/src/router/**` + 后端下发注册表 `user-server/internal/model/menu.go`（`GET /api/menus`）。
> 判据：一个页面有没有存在价值看「独立业务对象 + 独立主操作 + 独立 API 面」；
> 同一工作流的列表/详情/编辑/统计不另设一级入口，但保留路由深链。

## 1. 现状盘点结论

- 266 条前端路由中，200 条既有路径在后端下发菜单树里有登记（新增 8 条后共 208 条）；
  菜单 → 路由方向 **0 条断链**（每个下发 path 都有真实页面）。
- 路由 → 菜单方向存在三类缺口：
  1. **今日新增的 CRM / 线索治理 / 渠道统一 / 电商操作模块**（`crm`、`leads`、`channels`、
     `commerce` 四个模块共 17 条页面）只有深链，后端菜单树没有入口 —— 非超管即使被授予
     角色也无法从侧栏进入，能力面守卫因此没有 menu_key 可以挂。
  2. **历史交付漏接线**的独立能力页（网站爬取、QA 审核、能力矩阵、能力诊断、延迟出站、
     客户同意等部分已在树内）中仍有 4 条页面无菜单入口。
  3. **正常的工作流深链**（详情/编辑/统计卡/构建器/安装引导/订阅墙/注册页等）本来就不该
     出现在菜单里，保留路由 + 面包屑归属即可。

## 2. 头脑风暴决策（逐类）

| 类别 | 页面 | 决策 | 理由 |
|---|---|---|---|
| 新增 CRM 主数据 | `/crm/list`、`/crm/dedupe`、`/crm/profile360/:id?`、`/crm/import-export`、`/crm/contacts`、`/crm/suppliers`、`/crm/service-providers`、`/crm/companies` | **合并为一个菜单** `crmMaster`，默认 `/crm/list`；其余为列表内工作流/实体子页 | 同一「客户主数据」对象，去重/360/导入导出/实体列表是同族动作；沿用第三轮审计「列表内完成表单操作」口径，不铺 8 个侧栏项 |
| 线索治理 | `/leads/rules`、`/leads/notify-routes`、`/leads/evidence`、`/leads/archive`、`/leads/scoped-list`、`/leads/mining-agents` | **合并为一个菜单** `leadGovernance`，默认 `/leads/rules` | 同一线索域的配置/证据/归档/权限视图，且已有「线索列表/线索统计/线索挖掘」入口；一个菜单入口承接即可 |
| 渠道统一 | `/channels/manage`、`/channels/accounts` | **并入「权限设置」组** 2 个菜单 `channelManage`、`channelAccount` | T057 明确「旧散落账号配置收敛到渠道账号管理」；渠道识别码已在该组，三者同属渠道接入治理 |
| 电商操作台 | `/commerce/operations` | **并入「触达运营」组** 1 个菜单 `commerceOperations` | 独立业务对象（平台店铺订单）+ 独立 API 面 `/api/v1/commerce`，有操作/证据/复核闭环，不是别的页面的详情 |
| 桥接凭证退役页 | `/bridge/token` | **不新增菜单**，保留路由供旧书签/兼容 | 页面自述「旧桥接凭证协议已退役」且指向渠道识别码页；能力归 `bridgeExtension`，不复活入口 |
| 知识爬取 / QA 审核 | `/knowledge/crawler-sources`、`/knowledge/qa-candidates` | **各补一个菜单**，归「知识库工作台」 | 独立对象（爬取源、QA 候选）+ 独立 API 面，属知识入库/审核流水线的必要入口 |
| 能力矩阵 | `/cap/matrix` | **补一个菜单**，归「系统设置/站点配置」 | 与 `capabilityDiagnostics` 是一对（声明 vs 运行态），诊断已在树内，矩阵缺入口 |
| 延迟出站管理 | `/reachPipeline/deferred-outbound` | **保留既有菜单**（注册表中已有 `reachDeferredOutbound`） | D02 合规窗口暂缓触达，独立对象 + 可取消动作；本次只核对接线不重复新增 |
| 客户同意与偏好 | `/customer360/consents` | 已在树内（D04） | 保留 |
| 工作流深链 | 详情/编辑/统计卡/构建器/安装引导/订阅墙/注册页等 | **不新增菜单**，用 `routeOwners` 归到父列表面包屑 | 菜单给深链会重复入口且无独立业务对象（沿用 2026-11-20 二审口径） |
| 重复皮 | `operationLog/enhanced`、各卡片统计页 | **不新增菜单** | 前者由操作日志列表内按钮进入；后者归各卡片列表深链 |

## 3. 最新菜单清单（本轮新增/调整节点）

| 顶级分组 | 二级组 | 菜单 key | 标题 | 路由 | Roles | 模块归属 |
|---|---|---|---|---|---|---|
| 客户中心 | — | `crmMaster` | 客户主数据 | `/crm/list` | admin | ModuleCustomer |
| 客户中心 | 线索治理 | `leadGovernance` | 线索治理 | `/leads/rules` | admin | ModuleCustomer |
| 触达运营 | — | `commerceOperations` | 电商操作台 | `/commerce/operations` | admin | ModuleReachSocial |
| 智能体 / 主动触达 | — | `reachDeferredOutbound`（已存在，本次确认保留） | 延迟出站管理 | `/reachPipeline/deferred-outbound` | admin | ModuleReachAuto |
| 知识中心 / 知识库工作台 | — | `knowledgeCrawlerSources` | 网站爬取 | `/knowledge/crawler-sources` | admin | ModuleKnowledge |
| 知识中心 / 知识库工作台 | — | `knowledgeQaCandidates` | QA 审核队列 | `/knowledge/qa-candidates` | admin | ModuleKnowledge |
| 系统设置 / 站点配置 | — | `capabilityMatrix` | 能力矩阵 | `/cap/matrix` | admin | ModuleCore |
| 系统设置 / 权限设置 | — | `channelManage` | 渠道管理 | `/channels/manage` | admin | ModuleCore |
| 系统设置 / 权限设置 | — | `channelAccount` | 渠道账号管理 | `/channels/accounts` | admin | ModuleCore |

> 其余 200 条既有菜单路径全部保留，路径不变。完整下发树以
> `user-server/internal/model/menu.go` 为准。

## 4. 下发鉴权模块设计（本轮同步）

1. **侧栏可见性 = 菜单授予**：`EligibleMenuKeys(role)` 按 `Roles` 词表把新节点圈定给 admin；
   库里 `role_menu_permissions` 若整表为空，`DefaultMenuKeys` 出厂即含新 key。
2. **接口授权 = 菜单能力面**：新模块的 `/api/manage/crm/*`、`/api/manage/leads/*`、
   `/api/lead-mining/*`、`/api/channel-accounts/*`、`/api/channels/*`、`/api/identify-codes/*`、
   `/api/v1/commerce/*` 新增能力规则，归属上表对应 menu_key；非超管须被授予该菜单才放行，
   与菜单勾选一处生效。
3. **功能开关 = 模块归属**：新 key 纳入 `ModuleMenuBindings`（客户/触达/知识/基座对应模块），
   没开通对应模块时菜单与接口同时不可见。
4. **路由数据**：前端 `routeOwners` 补新深链到父列表的归属；后端 `menu_capability_routes.go`
   由 `menu-route-role-audit.py --emit-go` 重新生成（335 → 354 条，新增入口由审计脚本
   自动推导），`--check-go` 门禁绿；CRM/线索治理/电商操作台这类前端经 API 包装模块调用、
   调用图追溯不到的接口面，在 `menu_capability_supplement.go` 手工补充并逐条注明理由，
   与生成件「同名形状以补充件为准」的既定口径一致。

## 5. 未做什么（如实）

- 未删除任何既有路由/页面；未改前端路由 path；本轮只补菜单入口与下发授权数据。
- 未对线上租户的真实 `role_menu_permissions` 数据做写操作（那属于线上数据变更，需单独授权
  与运维窗口）；本轮改的是**下发的注册表与能力面源码**，发布后对新租户/空表立即生效，
  存量租户按 service 的收口口径由既有迁移/seed 处理。
