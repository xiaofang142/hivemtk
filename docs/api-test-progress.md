# API 全量测试进度（任务三）

## 环境

| 项 | 值 |
|---|---|
| 仓库根 | `/Users/xiaofang/Documents/www/go/hivemtk/hivemtk` |
| 后端 | `user-server/`（Go + Gin，五层架构） |
| 前端 API 源 | `user-web/src/api/*.js`（110 个文件） |
| 路由清单 | `docs/api-list.md`（1028 处调用点，993 条唯一 method+url） |
| SaaS 实例 | `http://127.0.0.1:8204`，`TENANT_MODE=on` |
| 独立部署实例 | `http://127.0.0.1:8304`，`TENANT_MODE=off` |
| 数据库 | `127.0.0.1:8202` / `user_db` / 用户 `admin`（容器 `mtk-postgres`） |
| 登录账号 | `uit_admin` / `Hive@Test123`（或本地 `admin@hivemtk.demo` / `HiveMtk@90d84637f1ce`，`id=1, tenant_id=1`） |
| Token 缓存 | `/tmp/hv_token_8204`、`/tmp/hv_token_8304` 及 `D:\HiveMtk\persistent_data\qa_tokens\` |
| 启动环境变量 | `APP_ENV=development`（因 `.env` 的 `MASTER_KEY` 为空，非 development 会 `os.Exit(1)`） |

### Stage 0 双租户三角色黄金测试夹具（2026-10-21 实测落盘）

| Token 标识 | 适用实例与模式 | 账号名 / 邮箱 | 角色 (`role`) | `user_id` | `tenant_id` | `tenant_code` | Token 存盘路径 |
|---|---|---|---|---:|---:|---|---|
| `TOKEN_A1` | `:8204` (`TENANT_MODE=on`) | `qa_a_admin` (`qa_a_admin@qa.example`) | `admin` | `11` | `2` | `t67z9tq` | `D:\HiveMtk\persistent_data\qa_tokens\TOKEN_A1.txt` |
| `TOKEN_A2` | `:8204` (`TENANT_MODE=on`) | `qa_a_user` (`qa_a_user@qa.example`) | `user` | `12` | `2` | `t67z9tq` | `D:\HiveMtk\persistent_data\qa_tokens\TOKEN_A2.txt` |
| `TOKEN_B1` | `:8204` (`TENANT_MODE=on`) | `qa_b_admin` (`qa_b_admin@qa.example`) | `admin` | `13` | `3` | `tytmh69` | `D:\HiveMtk\persistent_data\qa_tokens\TOKEN_B1.txt` |
| `TOKEN_OFF` | `:8304` (`TENANT_MODE=off`) | `admin` (`admin@hivemtk.demo`) | `admin` | `1` | `1` | `""` | `D:\HiveMtk\persistent_data\qa_tokens\TOKEN_OFF.txt` |
| `TOKEN_ROOT_ON` | `:8204` (`TENANT_MODE=on`) | `admin` (`admin@hivemtk.demo`) | `admin` | `1` | `1` | `local-hivemtk01` | `D:\HiveMtk\persistent_data\qa_tokens\TOKEN_ROOT_ON.txt` |

启动命令（两个实例）：

```bash
cd user-server
set +H; CPW=$(cat /tmp/hv_dbpass)
nohup env PORT=8204 TENANT_MODE=on  GIN_MODE=release APP_ENV=development \
  DB_HOST=127.0.0.1 DB_PORT=8202 POSTGRES_PASSWORD="$CPW" go run ./cmd/api > /tmp/hv-on.log 2>&1 &
nohup env PORT=8304 TENANT_MODE=off GIN_MODE=release APP_ENV=development \
  DB_HOST=127.0.0.1 DB_PORT=8202 POSTGRES_PASSWORD="$CPW" go run ./cmd/api > /tmp/hv-off.log 2>&1 &
```

重新取 token：

```bash
for p in 8204 8304; do
  curl -s -X POST http://127.0.0.1:$p/api/auth/login -H 'Content-Type: application/json' \
    -d '{"username":"uit_admin","password":"Hive@Test123"}' \
    | python3 -c 'import sys,json;print(json.load(sys.stdin)["data"]["token"])' > /tmp/hv_token_$p
done
```

## 测试规则

- 每个路由手工构造参数、手工 curl（禁止脚本批量循环）。
- 两个实例都过：SaaS（8204）与独立部署（8304）。
- 响应分三类判定：①调用错误（4xx/5xx、路由未注册、参数名不符）②业务逻辑错误（200 但数据错/租户串号/空实现）③通过。
- 发现问题立即修复，修完回归同一路由。
- 路由清单按 `docs/api-list.md` 文件顺序推进，一个文件一个文件过。

## 已完成

| 文件 | 路由数 | 状态 | 备注 |
|---|---|---|---|
| （待填） | | | |

## 已发现问题

| # | 路由 | 现象 | 根因 | 状态 |
|---|---|---|---|---|
| 1 | 后台任务（非路由） | SaaS 模式下 `sop_outbox` / `reach_dispatcher` 等 worker 日志报 tenant ctx 未绑定 | 后台 goroutine 未注入 system scope 或按租户遍历 | 待确认 |
| 2 | `GET /api/ab-experiments` | SaaS(8204) ⇒ 500 `[tenant] 表 ab_experiments 需要租户上下文但 ctx 未绑定（query）`；独立(8304) ⇒ 200 | on 模式请求链已过 `TenantMiddleware`（否则会是 401），但 handler 侧把 ctx 换掉了（repo 用 `context.Background()` 或没 `db.WithContext(c.Request.Context())`） | 待定位 |
| 3 | 架构门禁 `scripts/check-architecture.sh` | `[L4] service 取 DB 句柄宽口径与基线不符`：`user-server/internal/service/ai_agent.go` 当前 `7 > 基线 5`（另 `customer_session_cron.go:17`、`message_hub_summary_agg.go:19` 持有 `*gorm.DB` 字段告警） | `ai_agent.go` 新增了 2 处直接获取 DB 句柄调用，超出 `check-architecture.sh` 锁定的基线 `5` | Stage 0 已定位（待审批修复） |
| 4 | `POST /api/auth/login`（异步登录风控 `recordLoginRiskAsync`） | SaaS(8204) 登录返回 200，但后台日志必报 `[tenant] 表 login_events 需要租户上下文但 ctx 未绑定（query/create）` | `internal/controller/auth.go:69,85`：`POST /api/auth/login` 位于公开路由组（未经过 `TenantMiddleware`），`tenant.FromContext(ctx.Request.Context())` 恒为 `false`，且未将 `resp.TenantID` 传入 `recordLoginRiskAsync` 绑定 `tenant.OnlyTenant(dctx, resp.TenantID)` | Stage 0 已定位（待审批修复） |
| 5 | `POST /api/tenant/register`（异步模块同步 `ProvisionTenantModules`） | SaaS(8204) + `PLATFORM_ENABLED=false` 下新注册租户仅获得 7 个 `base` 模块，异步 `Sync` 未给新租户写入 16 个 0 元 `gift` 模块 | `internal/service/module_catalog.go:355-368`：`Sync(c)` 在 `line 364` 执行 `ctx = tenant.WithSystemScope(ctx)` 覆盖了 `ProvisionTenantModules` 传入的 `tenant.OnlyTenant(bg, tenantID)`，导致 `bootstrapOffline` 读写落回默认 `tenant_id=1` 而非 `reqTenantID` | Stage 0 已定位（待审批修复） |
| 6 | `/api/scripts*`（10 条路由）与 `/api/script-templates*`（7 条别名路由） | SaaS(8204) 下 `GET/POST /api/scripts`、`GET /api/scripts/categories`、`GET /api/scripts/public`、`GET /api/scripts/search`、`POST /api/scripts/sync-to-library` 全部返回 `500` 且直接将 `[tenant] 表 script_templates/script_categories 需要租户上下文但 ctx 未绑定` 内部错误原文暴露给前端；`GET /api/scripts/:id` 将 `[tenant]` 500 错误折叠为 `404`；`POST /api/scripts/recommend` 吞掉 `SearchTemplates` 的 `[tenant]` 错误伪造 `200 OK + data: null` | `internal/content/repository/script_template.go` 全部 15 个方法与 `internal/content/service/script_template.go` 全部 11 个方法均未接收 `ctx context.Context` 且未调用 `.WithContext(ctx)`；`script_template_sync.go` 接收了 `ctx` 但 4 处 DB 操作全部丢弃；`RecommendScript` 忽略 `SearchTemplates` 返回的 `err` | Stage 1 `#19` 已定位并完成沙盒修复验证（补丁待审批） |
| 7 | `POST /api/scripts`、`PUT /api/scripts/:id`、`DELETE /api/scripts/:id` | ① `POST /api/scripts` 在 `user_id` 为 `float64`（JSON Numbers 反序列化或单测注入）或未注入时触发 `interface conversion: interface {} is float64/nil, not uint` panic（HTTP 500 空响应）；② `PUT/DELETE /api/scripts/:id` 未校验 `IsSystem` 与 `CreatedBy == userID \|\| isAdmin`（`DeleteTemplate` 甚至写了 `_ = template`），存在同租户水平越权（IDOR）与系统预置模板篡改；③ `Update` 使用 `.Save()` 存在跨租户 `ON CONFLICT(id) DO UPDATE` 夺行与软删死行复活风险 | `internal/content/controller/script_template.go:35` 裸写 `userID.(uint)`；`internal/content/service/script_template.go:96-140` 缺失 `IsSystem` 与 `CreatedBy` 守卫；`internal/content/repository/script_template.go:56,126` 及 `script_template_sync.go:89` 使用 `.Save()` 且 `Delete` 未判 `RowsAffected == 0` | Stage 1 `#19` 已定位并完成沙盒修复验证（补丁待审批） |
| 8 | 跨模块 `POST /api/sales-champion/extract`（`InsertScriptTemplate`） | 销冠对话沉淀话术写入 `script_templates` 时触发 PostgreSQL `SQLSTATE 23502: null value in column "name" of relation "script_templates" violates not-null constraint` | `internal/repository/champion_dialogue.go:138-149` 绕过 `content/repository` 直接以 `map[string]any` 写入 `script_templates`，漏传非空列 `"name"` | Stage 1 `#19` 已定位并完成沙盒修复验证（补丁待审批） |
| 9 | 前端 `user-web/src/api/scriptTemplate.js` 与 `views/scriptTemplate/List.vue` 契约失配 | ① `frontend_aliases.go:192-199` 仅挂载了 `/api/script-templates` 的 GET 别名，漏挂 `POST /api/script-templates`、`PUT /api/script-templates/:id`、`DELETE /api/script-templates/:id`；② `List.vue` 表单提交 `scenario`/`variables(逗号字符串 split 为数组)` 并读取 `useCount`/`effectiveness`/`updatedAt`，与后端 DTO `title`/`variables(string)`/`usage_count`/`rating`/`updated_at` 失配 | 前后端字段命名与别名路由未对齐（已作为 `#19` 遗留决策项记入 `model-list.md`） | Stage 1 `#19` 已登记待决策 |
| 10 | `POST /api/geo/alerts/:id/ack`、`DELETE /api/geo/alerts/:id` | ① `:8204` (`on`) 与 `:8304` (`off`) 实测 `POST /api/geo/alerts/999999/ack`（不存在 ID 或跨租户 ID）静默返回 `200 OK {"code":0,"message":"已确认"}` 而非 `404`；② `POST /api/geo/alerts/0/ack` 返回 `200`、`DELETE /api/geo/alerts/0` 返回 `404`，未在入口校验拦截 `id == 0` 返回 `400 invalid id` | `internal/geo/repository/alert.go:65-69` `MarkNotified` 未检查 `res.RowsAffected == 0`；`internal/geo/controller/alert.go:53-64` 缺少 `errors.Is(err, gorm.ErrRecordNotFound)` 映射 404 分支且 `id == 0` 未拦截 | Stage 1 `#20` 已定位并完成沙盒修复验证（补丁待审批） |
| 11 | `GET /api/geo/opportunities`（跨模块调用 `alertRepo`） | ① 当 `ctx` 未绑定租户或 DB 报错时，`OpportunityService.List` 静默吞掉 `s.alertRepo.CountUnread(ctx)` 与 `s.alertRepo.List(ctx, ...)` 的错误，伪造 `0 未读告警`；② 当存在未读告警时，`s.alertRepo.List(ctx, "negative_monitor", "", 1, 5)` 未按 `notified = false` 过滤，将已确认的历史告警混入「负面监控命中尚未确认」提示中 | `internal/geo/service/opportunity.go:147-158` 使用 `if ..., err == nil` 忽略错误且 `List` 未过滤已读状态 | Stage 1 `#20` 已定位并完成沙盒修复验证（补丁待审批） |
| 12 | `POST /api/geo/probe/run-negative`（及 `run-sov`、`run-source-sync`） | 挂载在普通登录租户路由组 `geo`（非 `geoAdmin`），任意普通租户员工调用一次即触发全库所有活跃租户的探针外呼与 `geo_alerts` 写入 | `internal/geo/controller/probe.go:81-87` 忽略了 `ctx.Request.Context()`，直接调用 `GetGeoJobManager().Trigger(JobNegativeMonitor)` → `StartJob` (`job_manager.go:342`) 执行 `tenant.Fanout(context.Background(), nil, ...)` | Stage 1 `#20` 已定位（将在 `#29`/`#32` 联动收口） |
| 13 | `GET/POST/PUT/DELETE /api/geo/competitors*` | ① `:8204` 与 `:8304` 实测 `PUT /api/geo/competitors/999999` 将 `gorm.ErrRecordNotFound`（404）折叠为 `400 Bad Request` 并裸吐内部错误原文 `"record not found"`（未绑租户时裸吐 `[tenant]` 原文）；② `DELETE /api/geo/competitors/999999` 未检查 `RowsAffected == 0`，对不存在或跨租户 ID 静默返回 `200 OK {"code":0,"message":"删除成功"}`；③ `GET/PUT/DELETE /api/geo/competitors/0` 未在入口校验拦截 `id == 0`；④ `Update` 使用 `.Save()` 存在跨租户夺行与软删死行复活风险 | `internal/geo/repository/competitor.go:51-57` `Update` 用 `.Save(c)` 且 `Delete` 未判 `RowsAffected == 0`；`internal/geo/controller/competitor.go:34-93` 未区分 `ErrRecordNotFound`/`23505`/`[tenant]` 且直接回显 `err.Error()` | Stage 1 `#21` 已定位并完成沙盒修复验证（补丁待审批） |
| 14 | `POST /api/geo/competitors`（软删除后同域名重建） | 同一租户删除某竞品（如 `rival.com`）后再次添加同域名竞品时，触发 PostgreSQL `SQLSTATE 23505: duplicate key value violates unique constraint "idx_geo_competitors_domain"` | `GeoCompetitor` 使用 GORM 软删除（`deleted_at`），而 `idx_geo_competitors_domain (tenant_id, domain)` 为非部分唯一索引，`Create` 未检查并复活同租户 `Unscoped()` 墓碑行 | Stage 1 `#21` 已定位并完成沙盒修复验证（补丁待审批） |
| 15 | `POST /api/geo/crawler/run`（竞品爬虫触发与 SSRF） | ① `ReportController.RunCrawler` 裸起 `go func()` 丢弃请求租户 `ctx`，调 `CrawlerMonitorCronSync()` 跨全库所有租户扇出抓取；② `MonitorCrawlerService.doCrawl` 抓取 `https://<comp.Domain><path>` 及 `CheckRedirect` 未调用 `urlguard.ValidateURL` 拦截内网/回环/元数据地址 | `internal/geo/controller/report.go:155-167` 与 `internal/geo/service/monitor_crawler.go:165-170,248-267` | Stage 1 `#21` 已定位（将在 `#24` `crawler_visit.go` 联动收口） |

## 上下文管理纪律（避免反复压缩停顿）

1. 不用 `read` 工具读 `user-web/` 与仓库根下的文件——目录上下文会自动注入 4 个 README（≈15K token/次）。读源码用 `grep` 工具或 shell + `head -c` 限长。
2. 所有 shell 输出必须限长（`| head -c 2000`）；禁止 `ls` 大目录全量输出。
3. 已消费完的工具输出一律 `ctx_reduce` 打标；已结束的区段用 ACP `compress` 折叠。
4. 状态只认本文件（`docs/api-test-progress.md`），压缩后不依赖聊天记录。
5. 一轮多测几条路由，减少回合数 = 减少检查点重注入。
