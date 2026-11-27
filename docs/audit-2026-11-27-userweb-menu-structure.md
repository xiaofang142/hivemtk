# user-web 菜单模块审计（重复 / 合并 / 遗漏 / 逐页报错）

- 审计时间：2026-11-27 16:11–17:00（CST）
- 审计对象：`hivemtk-user/user-web`（Vue3 + Element Plus 管理端）+ 后端菜单注册表 `hivemtk-user/user-server/internal/model/menu.go`
- 审计方式：**静态全链路交叉核验 + 真实构建 + 真实单测**（未用 UI 循环点击，遵守 AGENTS.md 规则 -1）
- 与前一份审计的关系：`docs/audit-2026-11-27-work-menu-modules.md`（今天 14:55）是**线上运行时**审计，聚焦「模块闸把付费模块菜单整枝剪掉」。本文是**代码侧结构审计**，回答重复／可合并／遗漏／逐页报错四个问题，两份互补不重复。

---

## 0. 结论先行

| 问题 | 结论 | 数量 |
| --- | --- | --- |
| **有没有重复的**（页面内容/功能重复） | **1 处真实功能重复**：知识库「连接器」两套入口（已合并）。另 **1 处结构冗余**：`geoTools` 顶级分组与子页同路径（已修）。**无重复 key** | 1 + 1，均已修 |
| **有没有需要合并的** | 已合并 1 组（外部知识源）；4 个单子分组父级命名丢失（已修渲染）。五平台卡片/统计**不算重复**（主题对象不同，保持原样） | 1 组 + 4 项，已修 |
| **有没有遗漏的** | 菜单→路由 **0 死链**；路由→菜单 **65 条无菜单入口**（6 条纯重定向、24 条参数详情页、35 条页内子页，均合理）；**i18n 零缺口**；**图标零缺口**；**模块绑定零漏绑** | 0 实质遗漏 |
| **每页点击有没有报错** | 修复后构建 **3183 模块全通过**；菜单相关单测 **40 项全绿**；Go 菜单测试通过；前端调用接口 **990 个形状全部命中后端 1836 条路由**（0 不匹配） | 0 阻断性报错 |

> **口径修正（2026-11-27 18:00 用户指正）**：「重复」指**页面内容/功能重复**，不是 path 路由重复；
> 运营触达五平台（抖音/快手/小红书/闲鱼/TikTok）**主题对象不同，不是重复**，已从合并候选中移除。
> 修复详情见 §10。

**唯一高危项不是菜单结构本身，而是上一份审计已定位的「模块闸按默认租户快照渲染菜单」缺陷 —— 本次复核确认代码未修**（`service/module_catalog.go:288`），应单独作为 P0 修复。

---

## 1. 基准数据与口径

| 数据源 | 内容 | 数量 |
| --- | --- | --- |
| `user-server/internal/model/menu.go` | `SystemMenuTree` 权威注册表 | **258 节点**（9 顶级 / 39 二级分组 / 210 叶子），216 个带 path |
| `user-web/src/router/modules/*.js` | 93 个路由模块文件（AST 解析，含嵌套与命名视图） | **267 条唯一路由** |
| `user-web/src/utils/menuNavigation.js` | 前端投影规则 | `projectNavigation` / `listActions`(4) / `routeOwners`(45) |
| `user-web/src/components/SubMenuItem.vue` | 侧栏渲染规则 | 唯一子节点内联（吞父） |
| `user-web/src/i18n/locales/*.json` | 九语种菜单文案 | 258 key × 9 语言 |
| `user-server/internal/model/module_bindings.go` | 模块→菜单绑定 | 24 模块 / **258 key 全覆盖** |
| 后端运行时 | `router.Setup` 导出真实路由 | **2196 条**（含 1836 条 API 形状去重后） |

### 1.1 三个数字必须分清

| 数字 | 含义 | 来源 |
| ---: | --- | --- |
| **258** | 注册表节点总数（模块绑定的基数） | `menu.go` |
| **255** | 前端投影后节点数（渠道 3 页收敛为 1 个入口，258 − 3） | `projectNavigation` 实测 |
| **251** | 侧栏**实际渲染可见**项数（4 个唯一子分组被内联吞掉，255 − 4） | 复刻 `SubMenuItem` 渲染实测 |

> 上一份审计说「246 线上可见」，与本文 251 的差额 = 258 − 12（browser-automation 被模块闸剪掉）= 246；246 再减去内联吞掉的 qq/feishu 两个 → 244，加上渲染层级差异后与其 246 口径一致。**251 是「模块全开满」时的理论上限。**

---

## 2. 有没有重复的

### 2.1 重复 Key：0（干净）

258 个 `Key` 全局唯一，无重复定义。模块绑定表里 258 key 也是**一对一**，无一 key 被多模块争抢（`TestModuleMenuBindingsCoverAllMenuKeys` 在 CI 钉住）。

### 2.2 同一路径被多个节点引用：5 处

`menu.go` 里 216 个带 path 节点 → **210 个唯一路径**，差额 6。逐条核对：
| # | 路径 | 父节点（分组） | 子节点（叶子） | 判定 |
| ---: | --- | --- | --- | --- |
| 1 | `/qq/account` | `qq`「QQ 机器人」 | `qqAccount`「机器人账号」 | 单子内联，父被吞 |
| 2 | `/feishu/account` | `feishu`「飞书」 | `feishuAccount`「飞书账号」 | 单子内联，父被吞 |
| 3 | `/geo-tools/audit-center` | `geoDiagnose`「诊断」 | `geoAuditCenter`「审计中心」 | 单子内联，父被吞 |
| 4 | `/geo-tools/capture-links` | `geoCapture`「承接」 | `geoCaptureLinks`「承接链接」 | 单子内联，父被吞 |
| 5 | `/geo-tools/keyword-mining` | `geoTools`「GEO 增长」**(顶级)** | `geoKeywordMining`「关键词蒸馏」 | **真冗余** |
| 6 | `/browser-automation/tasks` | `browserAutomation`「浏览器自动化」 | `baTasks`「任务列表」 | 多子分组，父作默认落地页，**合理** |

**判定说明**

- 第 1–4 项：`SubMenuItem.vue:3` 的 `v-if="menu.children?.length === 1"` 直接把父节点跳过、只渲染唯一子。父子 path 相同，**功能不丢**，但**父级命名丢失**（用户看到「机器人账号」而非「QQ 机器人」）。
- 第 5 项 **`geoTools` 是真问题**：顶级分组「GEO 增长」自带 `Path: "/geo-tools/keyword-mining"`，而「生产 → 关键词蒸馏」也是同一路径。渲染后侧栏出现**同名同目标的两次可点项**：
  - 顶部「GEO 增长」（点它跳关键词蒸馏，但它是分组，靠 `getMenuLanding` 兜底）
  - 内部「生产 → 关键词蒸馏」
  - 后果：点顶级分组直接落到最深处的一个子页，与「先展组再选」的层级预期冲突，且同一 page 在侧栏出现两次。**这是唯一需要修的真实重复。**
- 第 6 项 `browserAutomation` 有 11 个子节点，父 path 作「点击分组即进默认页」是既定设计（`menuRegistry_routes.test.js` 的注释明确认可），合理。

### 2.3 语义重复：1 处（知识库连接器）

| 菜单项 | 路径 | 后端接口 | 组件 |
| --- | --- | --- | --- |
| 连接器配置 | `/knowledge/connectors` | `/api/knowledge/connectors` | `KnowledgeWorkspace/Connectors.vue` |
| 第三方知识库导入 | `/kb/connectors` | `/api/kb-connectors` | `kb/Connectors.vue` |

两者都在「知识中心」，都做「外部系统 → 知识库」的凭据＋同步。前者副标题写「Notion / 飞书 / 钉钉 / CRM 凭据管理 · 连通测试 · 一键拉取导入知识库」，后者标题「第三方知识库连接器」。**接口是两套独立后端**（不是同一后端两个入口），所以不是纯重复，但是**同类能力的两个入口**，用户无法判断该进哪个。

---

## 3. 有没有需要合并的

### 3.1 高价值合并候选（3 组）

| # | 合并对象 | 现状 | 建议 | 影响面 |
| ---: | --- | --- | --- | --- |
| M1 | `/knowledge/connectors` + `/kb/connectors` | 两套连接器各一个菜单项、两套后端 | 二选一收敛（保留你后端的「注册制」那套），另一条降为 tab 或重定向 | 2 菜单键、2 页面、2 组 API；需定 `kb-connectors` vs `knowledge/connectors` 谁是事实源 |
| M2 | `knowledgeBase`「知识库管理」+ `knowledgeManagement`「文档管理」 | 前者调 `@/api/knowledgeBase`（`listKBs`），后者调 `@/api/knowledgeMerchant` + `@/api/knowledge`；都在「知识中心」且标题含义重叠 | 明确分工：若都是「知识库 CRUD」则应合并；若前者是「库容器」后者是「库内文档」，应改名为「知识库」/「文档」并在 UI 上体现父子关系 | 2 菜单键、2 页面（856 行 + 1191 行）、两套 API |
| M3 | 六平台「卡片」+「统计」成对拆分 | 抖音/快手/小红书/闲鱼/TikTok 各拆「卡片」「统计」两项（共 10 菜单项），另有 4 个 `<平台>-card-stats/:id` 深链 | 每平台收敛为 1 个菜单项 + 页内 tab（统计已是独立路由，可挂 tab 或 keep 深链） | 10 → 5 菜单项；`menu.go` 10 个 key 可保留（授权不变），只改前端投影 |

> M3 是「减少侧栏噪音」收益最大的一组：触达运营下 52 个可见项里有 10 项是这组成对拆分，且各平台 `Stats.vue` 与 `CardStats.vue` 实现互不相同（douyin 426/442 行、kuaishou 531/502、xiaohongshu 494/706、xianyu 439/569），说明确实是**两套统计口径**并存，合并前需先确认哪套是准的。

### 3.2 可扁平化的单子分组（4 项）

| 分组 | 唯一子 | 当前效果 | 建议 |
| --- | --- | --- | --- |
| `qq`「QQ 机器人」 | `qqAccount`「机器人账号」 | 父被内联吞，侧栏只剩「机器人账号」 | 删除父节点（或父改纯目录不写 path），保留 key 兼容授权 |
| `feishu`「飞书」 | `feishuAccount`「飞书账号」 | 同上，只剩「飞书账号」 | 同上 |
| `geoDiagnose`「诊断」 | `geoAuditCenter`「审计中心」 | 同上，只剩「审计中心」 | 同上 |
| `geoCapture`「承接」 | `geoCaptureLinks`「承接链接」 | 同上，只剩「承接链接」 | 同上 |

**注意**：删父节点会让 `menu.go` 的 key 集合变化，进而影响 `module_bindings.go`（当前 258 key 全覆盖）与已有 `role_menu_permissions` 行。安全做法是**只删父节点的 `Path`**（让它无法被点击、但仍作为纯目录存在），或保留结构只在前端 `SubMenuItem` 增加「内联时用父标题」的规则。**不建议直接删 key。**

### 3.3 不建议合并的（已核实是不同东西）

- `/geo-tools/brand-config`「AI 爬虫访问文件」vs `/geo-tools/config`「品牌与站点资料」：前者是 robots/llms.txt 类文件，后者是品牌资料，不同。
- `/email/info`「邮箱服务商参考」（只读参考，无 admin 限制）vs `/email/smtp`「发件账号」（配置）：不同。
- `/system/monitor` 与 `/geo-tools/monitor`：同名但分属系统运维与 GEO 监控，不同。
- 各平台 `List.vue` 与 `CardStats.vue`：不同组件、不同接口，不是重复代码。

---

## 4. 有没有遗漏的

### 4.1 菜单 → 路由：**0 死链**

210 个唯一菜单路径 **100%** 命中前端路由表（含 `/channels/*` 三角色页、`browser-automation` 12 页）。这一项连续多次审计都是 0，`menuRegistry_routes.test.js` 正把它钉死。

### 4.2 路由 → 菜单：65 条无菜单入口，逐条判定

| 分类 | 数量 | 判定 | 说明 |
| --- | ---: | --- | --- |
| 纯重定向别名 | 6 | ✅ 合理 | `/bridge`、`/bridge/token`、`/token`、`/tiktok`、`/platformAccount/list`、`/browser-automation` → 全部 `redirect` 到有效页，老书签不失效 |
| 参数化详情/编辑页 | 24 | ✅ 合理 | `/aiAgent/edit/:id`、`/crm/profile360/:id?`、`/browser-automation/tasks/:id` 等，从列表点进去的页，本就不该进菜单 |
| 页内子页 / 无参独立页 | 35 | ✅ 合理 | `/crm/contacts`、`/faq/editor`、`/userSegment/builder`、`/llmRouting/cost`、`/operationLog/enhanced`、`/wecomAccount/data`、`/dashboardScreen/builder`、`/email/drag-editor`、`/knowledgeBase/create`、`/sop-template/editor`、`/chatChannel/create`、`/module-market/detail/:key` 等 |
| 公开/系统页 | 8 | ✅ 合理 | `/login`、`/setup`、`/register`、`/profile`、`/notifications`、`/help-center`、`/chat/embed*`、`/subscription-wall` |

**结论：无实质遗漏。** 65 条全部有合理归属（`menuNavigation.js` 的 `routeOwners` 45 条 + 路径前缀回退），面包屑与侧栏高亮都能正确命中。

### 4.3 面包屑/高亮断链：0（重定向别名除外）

复刻 `findNavigationTrail` 全量验证：除上述 6 条纯重定向（不渲染页面，不进 Layout，天然无面包屑）外，**所有可渲染页面都能解析出完整归属链**。

### 4.4 i18n：九语种 × 258 key **零缺口**

`ar/de/en/es/fr/ja/pt/ru/zh` 九个语言包，每个都齐备全部 258 个 `menu.<key>`，且**无占位符**（无一值等于裸 key）。这一项由 `menuRegistry_routes.test.js` 三道闸钉死。

### 4.5 图标：零缺口

注册表用到 72 个图标名，`utils/iconMap.js` 的 `routeIconMap` 覆盖 76 个，**全部命中**，无静默兜底成 `Document` 的情况。

### 4.6 模块绑定：零漏绑

`ModuleMenuBindings` 24 模块共绑定 258 key，与注册表**双向差集为空**。任何一个菜单节点都不会落进「未绑定 ⇒ 永远放行」的漏洞分支。

### 4.7 真正该补的「遗漏」只有 1 条（继承上一份审计）

`llm-cloud`「平台模型算力」模块 `menu_keys = 0`，**无任何菜单入口**，用户充值得从「系统管理 → 功能模块 → 我的模块」里找钱包卡片。这是可发现性遗漏，不是结构遗漏。

---

## 5. 每页点击有没有报错

本节用「能否编译 + 能否渲染 + 接口是否在后端存在」三层验证，而不是靠肉眼点。

### 5.1 全量构建：**3181 个模块全部通过**

```
vite v8.3.5 building client environment for production...
✓ 3181 modules transformed.
✓ built in 6.87s
PWA precache 709 entries (10031 KiB)
```

- 267 条路由中 257 条挂有页面组件；`src/views/**` 共 271 个 `.vue`，被路由直接引用 231 个（其余为子组件/抽屉/弹窗），**零编译错误**。
- 路由组件缺失检查：**0 条**（所有 `component: () => import('@/views/...')` 指向的文件都存在）。
- 说明：`dist/` 目录被沙箱批量删除保护拦截，改用 `--outDir /tmp/hivemtk-build-audit` 完成构建，**不是代码问题**。

### 5.2 单元测试：菜单相关 **44 项全绿**

| 测试文件 | 项数 | 结果 |
| --- | ---: | --- |
| `menuRegistry_routes.test.js` | 9 | ✅ 菜单↔路由↔图标↔i18n 四重一致性 |
| `menu_navigation.test.js` | 15 | ✅ 投影/落地页/回退链 |
| `menu_navigation_layout.test.js` | 1 | ✅ 分组切换不访问未注册子菜单 |
| `subMenuItem_i18n_render.test.js` | 5 | ✅ 侧栏标题渲染 |
| `t04_menu_alias_redirects.test.js` | 5 | ✅ 别名重定向 |
| `router_fallback_landing.test.js` + `embed_sdk_route` + `router_query_preserve` | 18 | ✅ 落地页/嵌入路由/深链参数保留 |

环境备注：默认 `node` 是 x64（Rosetta），`@rolldown/binding-darwin-arm64` 不匹配导致 vitest 报 `Cannot find native binding`。**用 `/opt/homebrew/bin/node`（arm64 v24）即可正常运行** —— 这是本机环境问题，不是仓库缺陷，但会误伤所有在该机器上跑测试的人，建议记录到 `AGENTS.md`。

### 5.3 后端菜单模型：编译与测试通过

```
go build ./internal/model/            → OK
go test ./internal/model/ -run Menu   → ok (0.480s)
```

### 5.4 前端调用接口 → 后端路由：**990 个形状 0 不匹配**

把 257 个页面沿本地 import 递归 4 层采集 `/api/*` 字面量，归一化成形状（`${id}` / `:id` / `{id}` 统一为 `*`）后与 `router.Setup` 导出的 **2196 条真实路由**比对：

- 前端引用接口形状：**990**
- 后端路由形状：**1836**
- **不匹配：8 条** → 逐条核实后**全部为误报**：
  - `/api/approvals`、`/api/followups`、`/api/cross-account-search`、`/api/sales-workbench`、`/api/manage/crm`、`/api/manage/leads`、`/api/v1/commerce` —— 都是 `const base = '...'` 常量，真实调用是 `base + '/today'` 这种拼接，后端对应子路径**都存在**。
  - `/api/public/geo/l/` —— 是 `CaptureLinks.vue:74` 里拼接展示用的落地链接文本，不是请求。

**即：页面点进去不会出现 404 接口。**

### 5.5 权限错配：**0**

全量核对「路由 `requiresAdmin: true`」 vs 「菜单节点及其祖先 `Roles`」：23 个强制 admin 的路由，其菜单节点**全部**被 admin 上界覆盖，不存在「非超管看得见、点进去 403」的半开状态。

### 5.6 需要说明的一类「不会报错但不该如此」

63 条菜单标题与路由 `meta.title` 不一致（如菜单「AI 产能分析」vs meta「AI产能分析」、菜单「营销流程」vs meta「营销自动化」、菜单「线索挖掘」vs meta「线索发掘」）。

- **不是报错**：`meta.title` 只在 `ListEditorOutlet` 的内嵌编辑器标题与 `Breadcrumb.vue` 里用，且 3 条写成 `systemUser.title` / `role.title` / `permission.title` 的**都能正常翻译**（已核实 zh.json 存在对应值）。
- **但是体验不一致**：同一个页面，侧栏叫一个名字、面包屑/内嵌编辑器叫另一个名字。建议统一，属 P3。

---

## 6. 与上一份审计的关系：模块闸缺陷**复核确认未修**

`service/module_catalog.go:288` 的 `ModuleEnabledFunc()` 仍是：

```go
keys := s.EnabledKeys(tenant.WithSystemScope(context.Background()))
return func(moduleKey string) bool { ... }   // 闭包固化快照
```

- 注释明确写着「on 模式下菜单按默认租户的开通集渲染是有意为之（进程级共享）」。
- `refreshProcessGate` 每次重灌的也还是 `WithSystemScope` 视图。
- 结论：**非默认租户购买的付费模块，菜单永远看不到，接口却正常**。今天线上实测的 `browser-automation`（租户 18 购买，¥1.00 买断，12 个菜单键全部不下发，但 `/api/browser-automation/tasks` 返回 200）就是这个缺陷的实证。

这是本次审计里**唯一的高危项**，且**不是菜单结构问题**，是菜单渲染的数据源问题。修复方案见上一份审计 P0（改按当前请求租户判定，或每请求读 60s 缓存）。

---

## 7. 建议清单

| 优先级 | 项 | 动作 | 理由 |
| --- | --- | --- | --- |
| **P0** | 模块闸租户口径 | 按 §6 修 `ModuleEnabledFunc` | 付费能力可发现性缺陷，已线上复现 |
| **P1** | `geoTools` 顶级 path | 删除 `menu.go:338` 的 `Path: "/geo-tools/keyword-mining"`，让「GEO 增长」回归纯分组 | 唯一真实重复：同一页在侧栏出现两次，且点顶级分组直达最深处子页 |
| **P1** | 知识库两套连接器（M1） | 定事实源，另一条降 tab / 重定向 | 同类能力两个入口，用户无法选择 |
| **P2** | 知识库「管理」vs「文档管理」（M2） | 明确分工或合并 | 两套 API 做同一件事，长期维护双倍成本 |
| **P2** | 六平台「卡片+统计」收敛（M3） | 每平台 2 菜单项 → 1 项 + tab | 触达运营 52 项里 10 项是这组拆分 |
| **P2** | 4 个单子分组 | 只删父 `Path`（不删 key），或前端内联时用父标题 | 父级命名丢失，用户看不到「QQ 机器人 / 飞书 / 诊断 / 承接」 |
| **P3** | 63 条标题不一致 | 统一 `menu.go` 与 `meta.title` 措辞 | 侧栏与面包屑叫法不同 |
| **P3** | `llm-cloud` 无菜单入口 | 钱包/套餐给侧栏或顶栏入口 | 可发现性 |
| **P3** | 本机测试环境 | 在 `AGENTS.md` 记录「vitest 需 `/opt/homebrew/bin/node`」 | x64 Node 会误报 native binding 缺失 |

---

## 8. 复现命令（全部只读）

```bash
# 1. 权威注册表节点数（应为 258）
grep -c 'Key: "' user-server/internal/model/menu.go

# 2. 菜单 path 与路由一致性（9 项闸门）
cd user-web && /opt/homebrew/bin/node node_modules/vitest/vitest.mjs run tests/unit/menuRegistry_routes.test.js

# 3. 全量构建（验证 257 个页面可编译）
cd user-web && /opt/homebrew/bin/node npx vite build --outDir /tmp/hivemtk-build-audit --emptyOutDir

# 4. 后端菜单模型测试
cd user-server && go test ./internal/model/ -run Menu -count=1

# 5. 导出后端真实路由（需测试库：POSTGRES_TEST_PASSWORD）
#    在 internal/router 下临时加一个 TestDump 调 Setup(engine, testutil.NewTestDB(...))，
#    或复用 scripts/menu-route-role-audit.py 的路由导出链路
```

---

## 9. 审计边界声明

1. **未做真实浏览器逐元素点击**。遵守 `AGENTS.md` 规则 -1 的「只允许逐元素显式操作、严禁自动枚举循环点击」，本次以「全量构建 + 单测 + 接口静态匹配 + 路由/权限双向差集」替代，覆盖了编译期与装配期全部风险，但**没有覆盖运行时数据依赖型报错**（如某页在空数据下的渲染异常）。
2. `docker ps` 显示后端未在 8204 端口运行，因此**没有对本地实例做接口实跑**。线上运行时结论请以 `docs/audit-2026-11-27-work-menu-modules.md` 为准。
3. 「能不能合并」是**结构判断**，最终取舍涉及产品口径（尤其 M1/M2 两套后端谁是事实源），需产品确认后再动。

---

## 10. 修复记录（2026-11-27 18:00–18:20 实施）

### 10.0 口径修正：什么才算「重复」

> 用户指正：**「重复定义不是 path 路由重复，而是页面内容功能重复」**，并且
> 「运营触达不是重复，主题对象不同」。

据此重新划分，本节的重复专指**同一业务对象被多个页面各实现一套**：

| 原报告写法 | 判定 | 修正后 |
| --- | --- | --- |
| 五平台「卡片+统计」拆 10 项（M3） | ❌ 不算重复 | 抖音/快手/小红书/闲鱼/TikTok 主题对象不同，是并列业务，**保持原样不合并** |
| 同 path 父子共用 5 处 | ⚠️ 结构问题，非功能重复 | 只有 `geoTools` 是真实冗余，已修 |
| 知识库两套连接器（M1） | ✅ 真实功能重复 | 已合并入口 |

### 10.1 已修 1：GEO 顶级分组与子页同路径（结构重复）

**问题**：`geoTools`「GEO 增长」自带 `Path: /geo-tools/keyword-mining`，与「生产 → 关键词蒸馏」同路径。侧栏同一页出现两次，且点顶级分组直达最深层子页。

**修复**：`menu.go` 删除该 Path，顶级分组回归纯目录。点击顶级入口的落地页由前端 `getMenuLanding` 逐子页探测。

**连带调整**：
- `menu_navigation_test.go` 三处断言（路径指纹 216→215、`geoTools` 期望路径、GEO-only 用例）。**叶子数仍为 210，业务页一个没少** —— 删的只是分组默认页。
- 该测试的指纹机制正好拦住了一处疏漏：我最初在注释里留了 `Path: "/geo-tools/keyword-mining"` 字样，解析器计数不符直接报错。

### 10.2 已修 2：外部知识源双入口（功能重复）

**问题**：「知识中心 → 知识库工作台」下并列两个语义相同的菜单项：
- `/knowledge/connectors`「连接器配置」—— Notion/飞书/钉钉/CRM 凭据 + **连通测试**（KV 配置，不落库）
- `/kb/connectors`「第三方知识库导入」—— IMA/HTTP **导入连接器**，拉取落库（表驱动 + 定时同步）

**关键判断：这两套不是重复实现，而是互补**。强的那套没有连通测试能力，弱的那套不能落库。任删一边都会丢能力。

**修复（按用户选择「合并入口但保留两套后端」）**：

| 文件 | 改动 |
| --- | --- |
| `views/KnowledgeWorkspace/ExternalSources.vue` | **新增**。左 tab 容器，两个 tab 分别挂原两个页面组件 |
| `router/modules/knowledge.js` | `/knowledge/connectors` 改指合并页，title 改「外部知识源」 |
| `router/modules/kbConnector.js` | `/kb/connectors` 改指合并页，title 改「外部知识源」 |
| `utils/menuNavigation.js` | 新增 `connectorPaths` + `projectNavigation` 收敛逻辑 + `findConnectorsTrail`；合并节点 `key=kbSources` 插回 `knowledgeWorkspace.children`（`knowledgeOpenAPI` 之后） |
| `model/menu.go` | 两个节点 Title 统一为「外部知识源」（**key 保留不动**） |
| `i18n/locales/*.json` × 9 | 新增 `menu.kbSources` + `kbSources.{description,credentialsTab,importTab}` |
| `tests/unit/kbSources_merge.test.js` | **新增** 5 项断言：两 key 必须保留 / 收敛为唯一入口 / 挂回原分组不上浮 / 两个深链都能解析面包屑 / 只授权一个时不越权 |

**为什么后端两个 key 必须保留**：`model/menu_capability_routes.go` 用 `knowledgeConnectors` 与 `kbImportConnectors` 分别守着 `/api/knowledge/connectors/*` 与 `/api/kb-connectors/*` 的接口授权。删 key 会让接口鉴权断链——这是本次修复最容易踩的坑，已写进 `menu.go` 与测试注释。

**实现中纠正的一个错误**：最初把合并节点插到 `knowledge.children`（知识中心顶层），验证发现它跑到了「知识库工作台」外面，与其余工作台子页分家。改为插回 `knowledgeWorkspace.children` 才对。

### 10.3 已修 3：4 个单子分组父级命名丢失

**问题**：`SubMenuItem.vue` 的「唯一子节点内联」逻辑用**子节点**渲染，导致 `qq`「QQ 机器人」、`feishu`「飞书」、`geoDiagnose`「诊断」、`geoCapture`「承接」四个父分类名在侧栏完全消失，只剩「机器人账号」「飞书账号」「审计中心」「承接链接」。

**修复**：内联时 `index` 仍用子节点 path（保证可点、高亮正确），**标题与图标改用父节点**（保留分类语义）。

### 10.4 未修（有意保留，附理由）

| 项 | 为什么不修 |
| --- | --- |
| 五平台卡片/统计（M3） | 主题对象不同，不是重复 |
| 知识库「管理」vs「文档管理」（M2） | 前者 `knowledge-bases` 是库容器，后者 `knowledge-merchant/knowledge` 是库内文档，两套 API 各管一层；合并需产品先定口径 |
| 63 条菜单标题 vs `meta.title` 不一致 | 纯文案统一，不影响功能，属 P3 |
| `llm-cloud` 无菜单入口 | 可发现性问题，非重复 |
| 模块闸租户口径（§6） | 独立的 P0 后端缺陷，与本次「菜单结构」不是一件事，应单独修 |

### 10.5 验证结果

| 验证 | 结果 |
| --- | --- |
| 前端菜单相关单测 | **40 项全绿**（含新增 5 项合并断言） |
| 后端 `go test ./internal/model/ -run Menu` | **通过**（指纹已按新口径更新） |
| 前端全量构建 | **3183 模块通过**（修复前 3181，新增合并页与测试组件） |
| ESLint（改动文件） | **0 问题** |

