# HiveMtk SaaS 多租户全链路检查、清理、测试与回归 — Stage 1 第 1 批实体（#19 ScriptTemplate / ScriptCategory / ScriptRecommend）司法级四关审查与 MUT 变异复验报告

> **生成时间**：2026-10-21  
> **关联主计划**：[`e:\HiveMtk\saas-multitenant-master-test-plan.md`](file:///e:/HiveMtk/saas-multitenant-master-test-plan.md)  
> **关联进度台账**：[`E:\HiveMtk\hivemtk-master\model-list.md`](file:///E:/HiveMtk/hivemtk-master/model-list.md) / [`E:\HiveMtk\hivemtk-master\docs\api-test-progress.md`](file:///E:/HiveMtk/hivemtk-master/docs/api-test-progress.md)  
> **隔离沙盒已验证补丁**：[`C:\Users\Administrator\.gemini\antigravity\brain\94eea91f-4d2c-431a-9d1c-88ea3cb8e628\scratch\stage1_entity19_verified.patch`](file:///C:/Users/Administrator/.gemini/antigravity/brain/94eea91f-4d2c-431a-9d1c-88ea3cb8e628/scratch/stage1_entity19_verified.patch)  
> **源码零改动状态**：主工作区 `E:\HiveMtk\hivemtk-master` 保持 **0 行 `.go` / `.vue` 业务源码改动**（仅更新测试进度台账与报告文档）。

---

## 一、专家团多智能体编排报告（Orchestration Report — Stage 1 Batch 01）

### 1.1 编排拓扑与专家分工

| 专家子智能体角色 | 会话 ID | 赋能领域专家 | 核心职责与交付物 | 状态 |
|---|---|---|---|---|
| **`stage1-arch-ddl-auditor`** | `conversation://3310e027-e3b3-4241-b234-d7fb095ec8c6` | `@backend-specialist` + `@database-architect` | **关卡 4.1**（五层架构全链路调用追踪与跨包旁路审计）、**关卡 4.2**（GORM-only 与三处 DDL 真相源 + `mtk-postgres` 物理表对账）、**关卡 4.4**（四网普查死代码与前后端契约漂移） | ✅ 已完成 |
| **`stage1-security-primitives-auditor`** | `conversation://90a26108-4225-4a95-893f-557aed5770dc` | `@security-auditor` + `@backend-specialist` | **关卡 4.3**（SaaS 多租户归属核验 + 六大跨实体高危原语 P1～P6 逐行源码审计与安全修复规格设计） | ✅ 已完成 |
| **`stage1-isolated-test-runner`** | `conversation://abe9848a-66f2-4b52-a13a-46af5e79973a` | `@test-engineer` + `@devops-engineer` | **关卡 4.5**（`:8204`/`:8304` 真实双实例 cURL 探针 + 独立 Git Worktree 沙盒修复与 3 套 `TENANT_MODE=on` 契约锁测试 + **MUT-1 ～ MUT-8** 变异反向验证 + 导出补丁并无痕销毁沙盒） | ✅ 已完成 |
| **`orchestrator`（主控编排与复核）** | `conversation://94eea91f-4d2c-431a-9d1c-88ea3cb8e628` | `@orchestrator` + `@qa-automation-engineer` | 交叉复核三路专家证据、更新 `model-list.md`（`#19` `[x]`）与 `docs/api-test-progress.md`、归档报告并验证主仓源码零污染 | ✅ 已完成 |

---

## 二、【Stage 1 单批实体审查交接卡】（Entity `#19`）

### 2.1 实体档案与表归属概览

| 序号 | Model 文件路径 | 结构体名称 | 物理表名 | 表级归属分类 | `tenant_id` 索引声明 | 审查状态 |
|---|---|---|---|---|---|---|
| **#19.1** | [`user-server/internal/content/model/script_template.go`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/model/script_template.go#L11-L30) | [`ScriptTemplate`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/model/script_template.go#L11-L30) | `script_templates` | **租户级业务表** | `TenantID uint` (`gorm:"index;not null;default:1"`) | ✅ `[x]` 已完成 |
| **#19.2** | [`user-server/internal/content/model/script_template.go`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/model/script_template.go#L33-L45) | [`ScriptCategory`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/model/script_template.go#L33-L45) | `script_categories` | **租户级业务表** | `TenantID uint` (`gorm:"index;not null;default:1"`) | ✅ `[x]` 已完成 |
| **#19.3** | [`user-server/internal/content/model/script_template.go`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/model/script_template.go#L48-L60) | [`ScriptRecommend`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/model/script_template.go#L48-L60) | `script_recommendations` | **租户级业务表（死表待清理）** | `TenantID uint` (`gorm:"index;not null;default:1"`) | ✅ `[x]` 已完成 |

---

### 2.2 关卡 4.1：五层架构合规检查（`GO_FIVE_LAYER_ARCHITECTURE.md`）

#### （1）全链路调用链拓扑
```
Frontend:
  ├─ user-web/src/views/scriptTemplate/List.vue (直调 axios: GET/POST/PUT/DELETE /api/script-templates, POST /api/scripts/sync-to-library)
  ├─ user-web/src/views/scriptTemplate/AbStats.vue (调用 getScriptList -> GET /api/scripts)
  ├─ user-web/src/views/aiAgent/Edit.vue (调用 getScriptList -> GET /api/scripts)
  └─ user-web/src/api/scriptTemplate.js (封装 /api/scripts* 10 个导出函数)
        │
        ▼
Router:
  ├─ user-server/internal/router/business_routes.go:164-176 (挂载 /api/scripts* 10 条主路由)
  ├─ user-server/internal/router/content_routes.go:34-46 (同构路由，SetupRouter 未挂载，仅单测引用)
  └─ user-server/internal/router/frontend_aliases.go:192-199 (挂载 /api/script-templates* 7 条 GET 别名路由)
        │
        ▼
Controller:
  └─ user-server/internal/content/controller/script_template.go (ScriptTemplateController, 10 个 Handler)
        │
        ▼
Service:
  ├─ user-server/internal/content/service/script_template.go (ScriptTemplateService, 11 个方法)
  ├─ user-server/internal/content/service/script_template_sync.go (ScriptTemplateSyncService.SyncToLibrary)
  └─ [跨模块适配器] user-server/internal/service/sales_engine_adapters.go:24-36 (scriptLookupAdapter.MatchScript)
        │
        ▼
Repository:
  ├─ user-server/internal/content/repository/script_template.go (ScriptTemplateRepository / ScriptCategoryRepository / ScriptRecommendRepository)
  └─ [跨模块旁路写] user-server/internal/repository/champion_dialogue.go:133-155 (InsertScriptTemplate 直写 .Table("script_templates"))
        │
        ▼
Model:
  └─ user-server/internal/content/model/script_template.go (ScriptTemplate / ScriptCategory / ScriptRecommend)
```

#### （2）逐项合规核查与发现的架构违规（4 项）
1. **Model 纯净度（合规）**：[`internal/content/model/script_template.go`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/model/script_template.go#L1-L69) 仅包含结构体字段、GORM Tag 与 3 个 `TableName()` 方法，无任何业务逻辑或 DB 调用。
2. **违规 ①：无参构造函数内部暗摸 `_db.GetDB()` + 双入口反模式**：
   - [`NewScriptTemplateRepository()`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/repository/script_template.go#L17-L19)、[`NewScriptCategoryRepository()`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/repository/script_template.go#L106-L108)、[`NewScriptRecommendRepository()`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/repository/script_template.go#L140-L142) 均为无参构造并直接调用 `_db.GetDB()`；
   - [`NewScriptTemplateService()`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/service/script_template.go#L28-L34) 无参构造；
   - [`NewScriptTemplateSyncService()`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/service/script_template_sync.go#L23-L28) 与 [`NewScriptTemplateSyncServiceWithDB(db *gorm.DB)`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/service/script_template_sync.go#L31-L37) 并存双入口。
3. **违规 ②：Service 层直接持有 `*gorm.DB` 并跨层执行 GORM 查询/写入**：
   - [`ScriptTemplateSyncService`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/service/script_template_sync.go#L17-L20) 持有 `db *gorm.DB`，其 [`upsertOne`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/service/script_template_sync.go#L67-L94) 在 Service 层直接调用 `s.db.Where(...).First(&lib)`、`s.db.Create(&lib)`、`s.db.Save(&lib)` 读写 `mainmodel.ScriptLibrary`（`script_library` 表）。
4. **违规 ③：跨模块 Repository 旁路裸写 `.Table("script_templates")` 且漏传 `NOT NULL` 列导致必崩**：
   - [`internal/repository/champion_dialogue.go:133-155`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/repository/champion_dialogue.go#L133-L155) 的 [`InsertScriptTemplate`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/repository/champion_dialogue.go#L133-L155) 绕过 `content/repository`，使用 `r.db.WithContext(ctx).Table("script_templates").Create(row)` 直接插入 `map[string]any`，且仅填了 `"title": s.Title` 而**漏填了非空列 `"name"`**，在 PostgreSQL 真实库（`name varchar(100) NOT NULL`）下触发 `ERROR: null value in column "name" of relation "script_templates" violates not-null constraint (SQLSTATE 23502)`。
5. **违规 ④：跨模块 Service 适配器丢弃 `ctx`**：
   - [`internal/service/sales_engine_adapters.go:24-36`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/service/sales_engine_adapters.go#L24-L36) 的 [`scriptLookupAdapter.MatchScript(ctx, intent, stage, _)`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/service/sales_engine_adapters.go#L24-L36) 接收了 `ctx`，但在第 33 行调用 `a.svc.GetTemplateList("", stage, 1, 20)` 与第 35 行调用 `a.svc.RecommendScript("sales-engine", intent)` 时将 `ctx` 彻底丢弃。

---

### 2.3 关卡 4.2：GORM 驱动与 DDL 权威面对账（`check-gorm-only.sh`）

1. **GORM-Only 检查**：
   - 运行 `bash user-server/scripts/check-gorm-only.sh` 绿灯通过（0 处 `.Raw(` / `.Exec(`）。
2. **SQL 拼接、LIKE 通配符逃逸与分页未夹紧缺陷**：
   - **LIKE 通配符未转义**：[`ScriptTemplateRepository.SearchTemplates`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/repository/script_template.go#L89-L90) 使用 `"%" + keyword + "%"`，用户输入 `%` 或 `_` 会退化为全表通配匹配；
   - **分页未夹紧（Unclamped Pagination）**：[`GetAll`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/repository/script_template.go#L47-L48)、[`GetPublicTemplates`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/repository/script_template.go#L77-L78)、[`SearchTemplates`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/repository/script_template.go#L93-L94) 直接计算 `offset := (page - 1) * pageSize` 并调用 `Offset(offset).Limit(pageSize)`，未拦截 `page < 1`（产生负 Offset）与 `pageSize > 100`（如 `SyncToLibrary` 硬传 `pageSize = 10000`）；
   - **`Count(&total)` 错误被吞**：[`script_template.go:45, 74, 91`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/repository/script_template.go#L45) 均忽略了 `.Count(&total).Error`。
3. **三处 DDL 真相源与 `mtk-postgres` 物理库对账**：
   - **真相源 ①（Model Tag）vs 真相源 ②（`internal/pkg/db/migrate.go`）**：[`migrate.go:349-351`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/pkg/db/migrate.go#L349-L351) 在 `allModels()` 中完整登记了 `&contentmodel.ScriptTemplate{}`、`&contentmodel.ScriptCategory{}`、`&contentmodel.ScriptRecommend{}`，经 `tenancyidx.Normalize` + `db.AutoMigrate` 自动建表并生成 `idx_script_templates_tenant_id`、`idx_script_categories_tenant_id`、`idx_script_recommendations_tenant_id`。
   - **真相源 ③（SQL 迁移与模块表登记）物理漂移发现**：
     - **漂移 A（幽灵表 `script_recommends`）**：[`migrations/init-user-db.sql:130-135`](file:///E:/HiveMtk/hivemtk-master/migrations/init-user-db.sql#L130-L135) 创建了表名为 **`script_recommends`** 的表（含外键 `script_recommends_script_id_fkey REFERENCES script_templates(id)`），而 Go Model [`ScriptRecommend.TableName()`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/model/script_template.go#L66-L68) 返回 **`script_recommendations`**。经 `mtk-postgres` 实测核查，数据库中同时存在 `script_recommendations`（含 `tenant_id`，0 行）与幽灵表 `script_recommends`（无 `tenant_id`，0 行，但持有指向 `script_templates(id)` 的外键约束，导致对 `script_templates` 执行 `TRUNCATE` 时若不带 `script_recommends` 会报 FK 约束错）。
     - **漂移 B（`module_tables.go` 子包扫描盲区）**：[`internal/model/module_tables_test.go:17`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/model/module_tables_test.go#L17) 的 `scanModelTables` 仅扫描 `internal/model/*.go`，未扫描 `internal/content/model/*.go` 等子包，导致 `script_templates`、`script_categories`、`script_recommendations` 均未纳入 `ModuleTables` 映射。

---

### 2.4 关卡 4.3：SaaS 多租户约束与「六大跨实体高危原语」狙击（P1～P6）

| 原语编号 | 原语名称 | 命中状态 | 源码级精确证据 (`file:line`) | 严重度 |
|---|---|---|---|---|
| **P1** | **断链 `ctx`** | 🔴 **全面命中** | 1. [`internal/content/repository/script_template.go`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/repository/script_template.go#L22-L165)：3 个 Repository 共 **15 个方法（100%）** 均无 `ctx context.Context` 参数，直接裸调 `r.db.Create` / `r.db.First` / `r.db.Model` / `r.db.Save` / `r.db.Delete`。<br>2. [`internal/content/service/script_template.go`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/service/script_template.go#L37-L214)：`ScriptTemplateService` 共 **11 个方法（100%）** 均无 `ctx` 参数。<br>3. [`internal/content/service/script_template_sync.go:47-94`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/service/script_template_sync.go#L47-L94)：`SyncToLibrary(ctx)` 虽接收 `ctx`，但在第 52 行 `s.templateRepo.GetAll("", "", 1, 10000)` 及第 78/82/89 行 `s.db.Where/Create/Save` 处 **4 次全部丢弃 `ctx`**。<br>4. [`internal/content/controller/script_template.go`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/controller/script_template.go#L27-L194)：前 9 个 Handler 均未向 Service 传递 `ctx.Request.Context()`。 | **P0 阻断** |
| **P2** | **异步丢租户** | 🟢 **未命中** | `script_template` 全链路无 `go func` 异步协程。 | 无 |
| **P3** | **`gorm.Save()` 跨租户搬行与死行复活** | 🔴 **高危命中（3 处）** | 1. [`ScriptTemplateRepository.Update`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/repository/script_template.go#L55-L57)：`return r.db.Save(template).Error`。<br>2. [`ScriptCategoryRepository.Update`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/repository/script_template.go#L125-L127)：`return r.db.Save(category).Error`。<br>3. [`ScriptTemplateSyncService.upsertOne`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/service/script_template_sync.go#L89)：`s.db.Save(&lib)`。<br>**危害**：在 GORM PostgreSQL 驱动下，带主键 `Save` 当被租户 Scope 过滤命中 0 行时，会回退生成 `INSERT INTO ... ON CONFLICT ("id") DO UPDATE SET "tenant_id"="excluded"."tenant_id", "deleted_at"=...`，**直接将软删死行复活或将 B 租户行篡改为 A 租户所有**！（已在 MUT-2 实测复现） | **P0 高危** |
| **P4** | **0 行静默成功 / 查败返非空零值指针 / 吞错返 200** | 🔴 **高危命中（7 处）** | 1. **查败返非空零值结构体指针**：[`ScriptTemplateRepository.GetByID`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/repository/script_template.go#L27-L33) 在 `First` 报错时仍 `return &template, err`（非 `nil`）。<br>2. **Delete / Update / 计次 0 行静默成功**：[`ScriptTemplateRepository.Delete`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/repository/script_template.go#L60-L62)、[`IncrementUsage`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/repository/script_template.go#L65-L68)、[`ScriptCategoryRepository.Delete`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/repository/script_template.go#L130-L132)、[`ScriptRecommendRepository.MarkAsUsed`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/repository/script_template.go#L162-L165) 均只取 `.Error` 未检查 `RowsAffected == 0`。<br>3. **吞掉 `[tenant]` 门禁错误伪造 `200 OK`**：[`ScriptTemplateService.RecommendScript`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/service/script_template.go#L176) 写为 `templates, _, _ := s.templateRepo.SearchTemplates("", 1, 100)`，直接吞掉 `SearchTemplates` 的 `[tenant]` 报错，导致 `:8204` (`TENANT_MODE=on`) 下 `POST /api/scripts/recommend` 返回 `HTTP 200 {"code":0,"message":"获取推荐成功","data":null}` 假正常！ | **P0 高危** |
| **P5** | **错误折叠与内部闸文案回显** | 🔴 **高危命中（9 处）** | 1. **500 折叠为 404 + 泄露原文**：[`GetTemplateByID`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/controller/script_template.go#L75-L78) 对所有错误一律 `response.Error(ctx, 404, err.Error())`，将 `[tenant]` 未绑定 500 错误折叠为 404 并回显内部表名。<br>2. **500 折叠为 400 + 泄露原文**：[`UpdateTemplate`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/controller/script_template.go#L101-L103)、[`DeleteTemplate`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/controller/script_template.go#L118-L120) 一律返回 `400 + err.Error()`。<br>3. **500 直吐 `[tenant]` 内部原文**：[`CreateTemplate`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/controller/script_template.go#L52)、[`GetCategories`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/controller/script_template.go#L129)、[`CreateCategory`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/controller/script_template.go#L145)、[`SearchTemplates`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/controller/script_template.go#L164)、[`GetPublicTemplates`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/controller/script_template.go#L190)、[`SyncToLibrary`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/controller/script_template.go#L201) 全部调用 `response.Error(ctx, 500, err.Error())`。 | **P0 高危** |
| **P6** | **同租户 IDOR 水平越权与 `float64` 身份断言** | 🔴 **高危命中（3 处）** | 1. **`userID.(uint)` 裸断言 Panic**：[`CreateTemplate`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/controller/script_template.go#L34-L35) 执行 `userID, _ := ctx.Get("user_id")` 后直接裸写 `userID.(uint)`，当 `user_id` 为 `float64` 或 `nil` 时直接触发 `runtime.panicdottypeE` 崩溃！<br>2. **缺失 `IsSystem` 系统模板保护**：[`UpdateTemplate`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/service/script_template.go#L96-L127) 与 [`DeleteTemplate`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/service/script_template.go#L130-L140) 未检查 `template.IsSystem`，任何普通用户可篡改或删除系统预置模板。<br>3. **缺失 `CreatedBy` 同租户水平越权（IDOR）守卫**：[`DeleteTemplate`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/service/script_template.go#L135) 甚至残留占位代码 `_ = template`，未校验 `template.CreatedBy == userID \|\| isAdmin`，同租户普通用户 A2 可随意篡改/删除 A1 创建的私有模板。 | **P0 高危** |

---

### 2.5 关卡 4.4：僵尸代码四网普查（Census Before Delete）

#### （1）四网引用对照矩阵
| 层级 / 结构体 | 方法 / 路由 | 网① 仓储调用 | 网② 服务调用 | 网③ 路由挂载 | 网④ 前端 (`user-web/src`) 调用 | 处置结论 |
|---|---|---|---|---|---|---|
| `ScriptTemplateRepository` | `Create` / `GetByID` / `GetAll` / `Update` / `Delete` / `GetPublicTemplates` / `SearchTemplates` | Service 正常调用 | Controller 正常调用 | `/api/scripts*` 已挂载 | `scriptTemplate.js` / `List.vue` / `AbStats.vue` / `aiAgent/Edit.vue` | **保留并修复 P1/P3/P4** |
| `ScriptTemplateRepository` | [`IncrementUsage`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/repository/script_template.go#L65-L68) | 仅 `UseTemplate` 调 | `UseTemplate` **0 调用** | **未挂载路由** | `scriptTemplate.js:35` `useScript` 定义了 `POST /api/scripts/:id/use`，但 `.vue` **0 调用** | **清理死方法**（或记入遗留决策项） |
| `ScriptCategoryRepository` | `GetAll` | `GetCategories` 调 | `GetCategories` 调 | `GET /api/scripts/categories` | `scriptTemplate.js:61` `getScriptCategories` | **保留并修复 P1** |
| `ScriptCategoryRepository` | [`Create`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/repository/script_template.go#L112) / [`Update`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/repository/script_template.go#L125) / [`Delete`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/repository/script_template.go#L130) | `Update`/`Delete` **0 调用**；`Create` 仅 `CreateCategory` 调 | `CreateCategory` 仅死 Handler 调 | **未挂载 `POST /api/scripts/categories`** | `scriptTemplate.js:69` 定义了 `createScriptCategory`，但 `.vue` **0 调用** | **清理死方法** `Update`/`Delete`/`CreateCategory` |
| `ScriptRecommendRepository` | [`Create`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/repository/script_template.go#L145) / [`GetBySessionID`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/repository/script_template.go#L150) / [`MarkAsUsed`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/repository/script_template.go#L162) | **全仓 0 生产调用** | `s.recommendRepo` 注入后 **0 读写** | 无 | 无 | **清理死仓储 `ScriptRecommendRepository` 及 `s.recommendRepo` 字段**；`ScriptRecommend` 表归入文末「遗留决策项」 |

#### （2）前后端契约失配与遗留决策项（5 项，已记入 `model-list.md` 文末）
1. **`ScriptRecommend` (`script_recommendations`) 持久化推荐日志半成品 vs 幽灵表 `script_recommends`**：`RecommendScript` 仅在内存中构造切片返回，从未落库；且 `migrations/init-user-db.sql:130` 建了错名表 `script_recommends`（带外键指向 `script_templates(id)`）。
2. **`List.vue` vs `/api/script-templates` 写路由别名缺失**：[`List.vue:247,264,267`](file:///E:/HiveMtk/hivemtk-master/user-web/src/views/scriptTemplate/List.vue#L247) 直接请求 `DELETE/PUT/POST /api/script-templates*`，但 [`frontend_aliases.go:192-199`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/router/frontend_aliases.go#L192-L199) 只注册了 GET 别名，导致前端页面点击新建/编辑/删除在路由层落空（404）。
3. **`List.vue` 字段名失配与 `variables` 数组覆盖污染**：`List.vue` 绑定 `scenario`（后端 DTO 为 `title` requires `required`）、`useCount`（后端 `usage_count`）、`effectiveness`（后端 `rating`）、`updatedAt`（后端 `updated_at`）；且 `List.vue:200,224` 将 `row.variables`（逗号字符串）原地 `.split(',')` 赋回 `form.variables`，提交时产生 JSON 类型错误。
4. **`AbStats.vue` 下拉数据源错位**：`AbStats.vue:158` 用 `getScriptList`（`script_templates` 表）填充 `script_id`，但后端 `ScriptAbStatsService` 校验的是 `script_library` 表主键。
5. **未接线产品特性路由**：`user-web/src/api/scriptTemplate.js` 导出的 `useScript` (`POST /api/scripts/:id/use`) 与 `createScriptCategory` (`POST /api/scripts/categories`) 在后端 `business_routes.go` 未挂载且 `.vue` 无调用。

---

### 2.6 关卡 4.5：双实例基线探针、沙盒 `TENANT_MODE=on` 契约锁与【MUT-1 ～ MUT-8 变异探针反向验证】

#### （1）Step 1：现网双实例（`:8204` `TENANT_MODE=on` vs `:8304` `TENANT_MODE=off`）真实 cURL 基线探针实录

| 序号 | 请求端点与方法 | `:8204` (`TENANT_MODE=on`, `TOKEN_A1` `tenant_id=2`) 实测响应 | `:8304` (`TENANT_MODE=off`, `TOKEN_OFF`) 实测响应 | 实测定性 |
|---|---|---|---|---|
| 1 | `GET /api/scripts` | `HTTP 500` `{"code":500,"message":"[tenant] 表 script_templates 需要租户上下文但 ctx 未绑定（query）"}` | `HTTP 200` `{"code":0,"message":"获取成功","data":{"list":[],"page":1,"page_size":20,"total":0}}` | 🔴 **P1 断链 + P5 泄露内部闸原文** |
| 2 | `GET /api/scripts/1` | `HTTP 404` `{"code":404,"message":"[tenant] 表 script_templates 需要租户上下文但 ctx 未绑定（query）"}` | `HTTP 404` `{"code":404,"message":"record not found"}` | 🔴 **P1 断链 + P5 将 500 折叠为 404 并泄露原文** |
| 3 | `POST /api/scripts` | `HTTP 500` `{"code":500,"message":"[tenant] 表 script_templates 需要租户上下文但 ctx 未绑定（create）"}` | `HTTP 200`（实测后已物理清除并恢复 `0` 行） | 🔴 **P1 断链 + P5 泄露内部闸原文** |
| 4 | `PUT /api/scripts/1` | `HTTP 500` `{"code":500,"message":"[tenant] 表 script_templates 需要租户上下文但 ctx 未绑定（query）"}` | `HTTP 400` `{"code":400,"message":"record not found"}` | 🔴 **P1 断链 + P5 状态码错配** |
| 5 | `DELETE /api/scripts/1` | `HTTP 500` `{"code":500,"message":"[tenant] 表 script_templates 需要租户上下文但 ctx 未绑定（query）"}` | `HTTP 400` `{"code":400,"message":"record not found"}` | 🔴 **P1 断链 + P5 状态码错配** |
| 6 | `GET /api/scripts/categories` | `HTTP 500` `{"code":500,"message":"[tenant] 表 script_categories 需要租户上下文但 ctx 未绑定（query）"}` | `HTTP 200` `{"code":0,"message":"获取成功","data":[]}` | 🔴 **P1 断链 + P5 泄露内部闸原文** |
| 7 | `GET /api/scripts/public` | `HTTP 500` `{"code":500,"message":"[tenant] 表 script_templates 需要租户上下文但 ctx 未绑定（query）"}` | `HTTP 200` `{"code":0,"message":"获取成功","data":{"list":[]...}}` | 🔴 **P1 断链 + P5 泄露内部闸原文** |
| 8 | `GET /api/scripts/search?keyword=价格` | `HTTP 500` `{"code":500,"message":"[tenant] 表 script_templates 需要租户上下文但 ctx 未绑定（query）"}` | `HTTP 200` `{"code":0,"message":"搜索成功","data":{"list":[]...}}` | 🔴 **P1 断链 + P5 泄露内部闸原文** |
| 9 | `POST /api/scripts/recommend` | `HTTP 200` `{"code":0,"message":"获取推荐成功","data":null}` | `HTTP 200` `{"code":0,"message":"获取推荐成功","data":null}` | 🔴 **P4 吞掉 `[tenant]` 报错伪造 `200 OK`！** |
| 10 | `POST /api/scripts/sync-to-library` | `HTTP 500` `{"code":500,"message":"[tenant] 表 script_templates 需要租户上下文但 ctx 未绑定（query）"}` | `HTTP 200` `{"code":0,"message":"成功","data":{"created":0,"total":0,"updated":0}}` | 🔴 **P1 丢弃 `ctx` + P5 泄露内部闸原文** |

#### （2）Step 2：独立 Git Worktree 沙盒契约锁测试矩阵（29 个测试 / 子测试全绿）
在独立沙盒 `wt-stage1-19` 中编写并验证了三层 `TENANT_MODE=on` 契约锁测试：
- **`internal/content/repository/script_template_tenant_test.go`**（`go test` **PASS 0.602s**，共 9 个顶层/子测试）：
  - `TestScriptTemplateReadsAreTenantFactored`（`GetAll_Scoping_And_BareCtx` / `GetByID_Scoping_BareCtx_And_NilPointerOnError` / `GetPublic_And_Search_With_LikeEscape_And_PagingClamp`）
  - `TestScriptTemplateWritesAreTenantFactored`（`Create_AutoFillsTenant_And_BareCtxRejected` / `Update_SelectColumns_CrossTenantBlocked_NoSaveResurrection` / `Delete_CrossTenantBlocked_RowsAffectedZeroSentinel`）
  - `TestScriptCategoryReadsAndWritesAreTenantFactored`
- **`internal/content/service/script_template_tenant_test.go`**（`go test` **PASS 0.243s**，共 3 个顶层测试）：
  - `TestScriptTemplateServiceRecommendPropagatesError`
  - `TestScriptTemplateServiceOwnershipAndSystemGuard`
  - `TestScriptTemplateSyncServiceTenantIsolated`
- **`internal/content/controller/script_template_tenant_test.go`**（`go test` **PASS 0.728s**，共 17 个顶层/子测试）：
  - `TestScriptTemplateReadsSeparateMissingFromUnboundAndIsolateTenants`（覆盖全部 5 个读/推荐接口的 A/B 租户隔离、缺失 404 vs 未绑定 500 脱敏分离）
  - `TestScriptTemplateWritesIdentityOwnershipAndSystemGuard`（覆盖 `float64` `user_id` 兼容、未登录 401、系统模板 400 拦截、同租户非本人 403 IDOR 拦截、Admin 越权放行、跨租户 404 遮挡、`SyncToLibrary` 租户隔离与 500 脱敏）

#### （3）Step 3：【MUT-1 ～ MUT-8】变异探针反向验证实录

| 变异编号 | 针对原语 | 变异操作（模拟回退缺陷） | `go test` 实测变红原文（节选） | 回退后状态 |
|---|---|---|---|---|
| **MUT-1** | **P1 断链 `ctx`** | 摘掉 `ScriptTemplateRepository.GetAll` 中的 `.WithContext(ctx)` | `--- FAIL: TestScriptTemplateReadsAreTenantFactored/GetAll_Scoping_And_BareCtx` (`script_template_tenant_test.go:118: A 租户 GetAll 失败: [tenant] 表 script_templates 需要租户上下文但 ctx 未绑定（query）`) | ✅ 已回退并转绿 |
| **MUT-2** | **P3 `gorm.Save` 夺行与死行复活** | 将 `ScriptTemplateRepository.Update` 改回 `r.db.WithContext(ctx).Save(template).Error` | `--- FAIL: TestScriptTemplateWritesAreTenantFactored/Update_SelectColumns_CrossTenantBlocked_NoSaveResurrection` (`script_template_tenant_test.go:241: 跨租户 Update 应返回 ErrScriptTemplateMissing，实得 <nil>`；`:244: B 行不应被跨租户篡改，实得 tenant_id=101 title="CrossTenantHijack"`；`:277: 已软删行不应被复活，实得 deleted_at={Time:0001-01-01... Valid:false}`) | ✅ 已回退并转绿 |
| **MUT-3** | **P4 Delete 0 行静默成功** | 摘掉 `ScriptTemplateRepository.Delete` 中的 `RowsAffected == 0` 哨兵检查 | `--- FAIL: TestScriptTemplateWritesAreTenantFactored/Delete_CrossTenantBlocked_RowsAffectedZeroSentinel` (`script_template_tenant_test.go:260: 跨租户 Delete 应返回 ErrScriptTemplateMissing，实得 <nil>`；`:271: 重复 Delete 已删行应返回 ErrScriptTemplateMissing，实得 <nil>`) | ✅ 已回退并转绿 |
| **MUT-4** | **P4 `GetByID` 查败返非空指针** | 将 `ScriptTemplateRepository.GetByID` 失败分支改回 `return &template, err` | `--- FAIL: TestScriptTemplateReadsAreTenantFactored/GetByID_Scoping_BareCtx_And_NilPointerOnError` (`script_template_tenant_test.go:153: 跨租户 GetByID 失败时必须返回 nil 指针，实得 &{TenantID:0 ID:0 ...}`) | ✅ 已回退并转绿 |
| **MUT-5** | **P4 `RecommendScript` 吞错返 200** | 将 `RecommendScript` 改回 `templates, _, _ := s.templateRepo.SearchTemplates(...)` 忽略 `err` | `--- FAIL: TestScriptTemplateServiceRecommendPropagatesError` (`script_template_tenant_test.go:86: 裸 ctx 下 RecommendScript 必须透传 [tenant] 错误，实得 err=<nil>`) & `--- FAIL: TestScriptTemplateReadsSeparateMissingFromUnboundAndIsolateTenants/POST_recommend_unbound_returns_500_not_200` (`:178: 应回 500，实得 200 body={"code":0,"message":"获取推荐成功","data":null}`) | ✅ 已回退并转绿 |
| **MUT-6** | **P6 `userID.(uint)` 裸断言 Panic** | 将 `CreateTemplate` 改回 `rawUID, _ := ctx.Get("user_id"); userID := rawUID.(uint)` | `--- FAIL: TestScriptTemplateWritesIdentityOwnershipAndSystemGuard/POST_Create_supports_float64_user_id_and_rejects_missing_auth` (`panic recovered: interface conversion: interface {} is float64, not uint` -> `script_template_tenant_test.go:211: float64 user_id 应兼容返回 200，实得 500 body=`) | ✅ 已回退并转绿 |
| **MUT-7** | **P6 缺失 `IsSystem` 与 `CreatedBy` IDOR 守卫** | 注释掉 `UpdateTemplate` 与 `DeleteTemplate` 中的 `IsSystem` 与 `CreatedBy != userID` 校验 | `--- FAIL: TestScriptTemplateServiceOwnershipAndSystemGuard` (`:118: 修改系统模板应报 ErrInvalidInput，实得 <nil>`；`:126: 非创建者普通用户修改他人模板应报 ErrForbidden，实得 <nil>`) & `--- FAIL: TestScriptTemplateWritesIdentityOwnershipAndSystemGuard` (`:225: 应回 400，实得 200`；`:234: 同租户他人修改应回 403，实得 200`) | ✅ 已回退并转绿 |
| **MUT-8** | **P5 500 折叠为 404 及泄露 `[tenant]` 原文** | 将 `GetTemplateByID` 改回 `response.Error(ctx, 404, err.Error())`，`GetTemplateList` 改回 `response.Error(ctx, 500, err.Error())` | `--- FAIL: TestScriptTemplateReadsSeparateMissingFromUnboundAndIsolateTenants` (`:169: 响应体泄露内部错误原文: {"code":500,"message":"[tenant] 表 script_templates 需要租户上下文但 ctx 未绑定（query）"}`；`:172: GET /api/scripts/:id 裸 ctx 应回 500，实得 404`) | ✅ 已回退并转绿 |

#### （4）Step 4：沙盒全套门禁复核与无痕清理结果
- `grep -rn "MUT-" internal/`：**0 命中**（无任何变异残留）
- `go test -count=1 ./internal/content/repository/... ./internal/content/service/... ./internal/content/controller/...`：**全部 PASS**（`0.602s` / `0.243s` / `0.728s`）
- `go vet ./internal/content/... ./internal/service/... ./internal/repository/... ./internal/app/...`：**0 告警**
- `go build ./...`：**0 错误**
- `bash scripts/check-gorm-only.sh`：**`[check-gorm-only] OK`（0 违规）**
- 沙盒已通过 `git worktree remove --force` 销毁，完整补丁已保存至 [`stage1_entity19_verified.patch`](file:///C:/Users/Administrator/.gemini/antigravity/brain/94eea91f-4d2c-431a-9d1c-88ea3cb8e628/scratch/stage1_entity19_verified.patch)，主工作区 `git status -s` 验证无任何 `.go`/`.vue` 改动。

---

## 三、待用户统一审批的【缺陷修复卡】（Entity `#19`）

### 缺陷修复卡 D-19-01：`content` 话术模板模块全链路 `ctx` 断链、`.Save` 夺行复活、0 行静默与吞错返 200（P1/P3/P4/P5）
- **涉及文件**：
  - [`user-server/internal/content/repository/script_template.go`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/repository/script_template.go)
  - [`user-server/internal/content/service/script_template.go`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/service/script_template.go)
  - [`user-server/internal/content/service/script_template_sync.go`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/service/script_template_sync.go)
  - [`user-server/internal/content/controller/script_template.go`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/controller/script_template.go)
  - [`user-server/internal/service/sales_engine_adapters.go`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/service/sales_engine_adapters.go)
  - [`user-server/internal/app/sales_engine_factory.go`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/app/sales_engine_factory.go)
- **核心修复内容**（已封装于 [`stage1_entity19_verified.patch`](file:///C:/Users/Administrator/.gemini/antigravity/brain/94eea91f-4d2c-431a-9d1c-88ea3cb8e628/scratch/stage1_entity19_verified.patch)）：
  1. 收口 `NewScriptTemplateRepository(db *gorm.DB)`、`NewScriptCategoryRepository(db *gorm.DB)`、`NewScriptTemplateService(db *gorm.DB)`、`NewScriptTemplateSyncService(db *gorm.DB)` 为显式 `*gorm.DB` 注入，清理零引用的 `ScriptRecommendRepository` 及死方法。
  2. 为全部 Repository 与 Service 方法补齐首参 `ctx context.Context` 并调用 `.WithContext(ctx)`；修复 `SyncToLibrary` 内部 4 处丢弃 `ctx` 的问题，并将 `pageSize=10000` 改为按 `pageSize=100` 分页拉取。
  3. 将 `Update` 的 `.Save(template)` 改为 `.Model(&model.ScriptTemplate{}).Where("id = ?", template.ID).Select(...).Updates(...)` + `RowsAffected == 0 -> ErrScriptTemplateMissing`；`Delete` 补齐 `RowsAffected == 0 -> ErrScriptTemplateMissing`；`GetByID` 失败返回 `(nil, err)`；`SearchTemplates` 增加 `escapeScriptLike`（`ESCAPE '\'`）与 `normalizeScriptPaging`。
  4. 修复 `RecommendScript` 吞掉 `SearchTemplates` 错误的缺陷；Controller 统一使用 `errhttp.HandleDBError` 将 `ErrScriptTemplateMissing` 映射为 404、`[tenant]` 未绑定映射为脱敏 500。

### 缺陷修复卡 D-19-02：`ScriptTemplate` 身份类型断言 Panic、系统模板保护与同租户水平越权（P6）
- **涉及文件**：
  - [`user-server/internal/content/controller/script_template.go`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/controller/script_template.go#L27-L125)
  - [`user-server/internal/content/service/script_template.go`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/content/service/script_template.go#L96-L140)
- **核心修复内容**：
  1. Controller 引入 `flowCaller(ctx)`（调用 `convertToUint` 兼容 `uint`/`int`/`int64`/`float64`/`string`），`CreateTemplate` 在 `userID == 0` 时返回 `401 未授权`，彻底消除 `userID.(uint)` panic。
  2. `UpdateTemplate(ctx, id, userID, isAdmin, req)` 与 `DeleteTemplate(ctx, id, userID, isAdmin)` 增加 `template.IsSystem` 拦截（返回 `utils.ErrInvalidInput` -> HTTP 400）及 `!isAdmin && (userID == 0 || template.CreatedBy != userID)` 归属校验（返回 `utils.ErrForbidden` -> HTTP 403）。

### 缺陷修复卡 D-19-03：跨模块 `champion_dialogue.go` 旁路直写 `script_templates` 漏传非空列 `name`
- **涉及文件**：[`user-server/internal/repository/champion_dialogue.go:138-149`](file:///E:/HiveMtk/hivemtk-master/user-server/internal/repository/champion_dialogue.go#L138-L149)
- **核心修复内容**：在 `InsertScriptTemplate` 构造的 `row` 映射中补齐 `"name": s.Title`，消除 PostgreSQL `SQLSTATE 23502` 非空约束崩溃（后续可进一步收口为调用 `content/repository.ScriptTemplateRepository.Create`）。
