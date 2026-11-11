# 巡检 SQL 手册：layer_decision_logs 与审计操作日志

> 任务来源：RM-1⑦（P3 文档项）。基线：证据复核 2026-11-11。
> 所有行号锚均指 `user-server/` 仓内文件（hivemtk 用户仓），改动代码后请先核对锚点再照抄结论。
> 本文全部 SQL 为 PostgreSQL 方言、只读（SELECT），可直接粘贴执行；不包含任何 DELETE/UPDATE。

---

## 1. 范围与命名澄清：`audit_logs` 这张表不存在

**结论先行：本实例中没有名为 `audit_logs` 的表。** 任务名里的「audit_logs」是历史误称；
真实的审计落库表按 GORM `TableName()` 是 `operation_logs`。三类证据：

1. 实测证据：`docs/operations/SLA_SLO.md` 第 99 行——「没有 audit_logs 这张表（实测
   ERROR: relation "audit_logs" does not exist）」；第 109 行「操作审计（表名
   operation_logs，不是 audit_logs）」。
2. 代码证据：审计中间件写库链最终落到 `model.OperationLog`（`TableName()` 返回
   `operation_logs`，`internal/model/operation_log.go:25-27`）；全仓没有任何 Go 模型的
   `TableName()` 返回 `audit_logs`。
3. 迁移证据：`migrations/041_merchant_key_length_align.sql:15-26` 对 `audit_logs` 的引用
   只是防御性 DO 块（表不存在时静默 no-op），没有任何 `CREATE TABLE audit_logs`。

代码注释里仍有两处写 `audit_logs` 的陈旧说法（`internal/middleware/audit.go:283`、
`internal/middleware/api_logger.go:255`），以本文为准，不要照注释找表。

因此本文覆盖两张真实表：

| 表 | 角色 | 模型 | 保留期 |
|---|---|---|---|
| `layer_decision_logs` | AI 双层路由决策自观测（Layer1 FAQ/SOP 命中 vs Layer2 LLM 兜底） | `internal/model/layer_decision_log.go` | 30 天（参数可调） |
| `operation_logs` | 管理操作审计（HTTP 写操作的登录态审计面） | `internal/model/operation_log.go` | 90 天（参数可调） |

---

## 2. 连接方式

与 [SLA_SLO.md](SLA_SLO.md) 6.1 节同一套连法（业务库 8202；8217 是仅 `go test` 用的
fsync=off 专用测试库，见 [PORT_REGISTRY](../PORT_REGISTRY.md)，不要对它巡检生产结论）：

```bash
cd /opt/hivemtk && set -a && . ./.env && set +a

docker compose exec -T mtk-postgres psql \
  -U "${POSTGRES_USER:-admin}" -d "${USER_DB_NAME:-user_db}" -h 127.0.0.1 -p 8202
```

下文所有 SQL 都假定在该 psql 会话里执行。租户隔离开启（多租户模式）时，两表均有
`tenant_id` 列；示例聚合默认跨租户，需要按租户拆分时自行加
`WHERE tenant_id = <id>`。

---

## 3. 表一：layer_decision_logs（AI 分层路由决策日志）

### 3.1 表结构与索引

建表来源是 Go 迁移（不是 SQL 文件迁移）：
`internal/migration/migrations/ai_perf_faq_sop_layer_migration.go:75-94`（CREATE TABLE +
5 个索引，版本 2026-07-31）。此后两次结构演进：`v3_22_1_soft_delete_migration.go:53`
补 `deleted_at` 软删列；`migrations/055_session_id_length_align.sql:17` 把 `session_id`
从 varchar(50) 放宽到 varchar(120)。表结构以
`internal/model/layer_decision_log.go:31-48`（struct）与 `:51`（TableName）为权威，
启动时经 AutoMigrate 对齐（注册点 `internal/pkg/db/migrate.go:344`）。

| 列 | 类型 | 说明（证据：模型头注释 layer_decision_log.go:19-30） |
|---|---|---|
| id | bigint PK | 自增主键 |
| tenant_id | bigint NOT NULL DEFAULT 1 | 租户列（tenant_column_batch_migration 幂等补列） |
| trace_id | varchar(64)，有索引 | 本表自造的 `lr-<unixnano>`（layer.go:251-256 兜底分支），与 llm_routing_logs 的 UUID v4 trace_id 不同值域，**两表无法按它 join** |
| session_id | varchar(120)，有索引 | 会话维度 |
| customer_id | varchar(64)，有索引 | 客户维度 |
| layer | varchar(32)，有索引 | 命中层，取值只有 `layer1` / `layer2`（dto/layer_chunk.go:8-9） |
| reason | varchar(64) NOT NULL | 决策原因，实际写入集见 3.3 Q4 注释 |
| intent | varchar(64)，有索引 | 关联意图 |
| conf_in / conf_out | numeric(5,4) | 决策前/后置信度 |
| wall_ms | int | 本次决策 wall time（ms） |
| llm_skipped | boolean NOT NULL DEFAULT false | Layer1 命中跳过 LLM 时为 true |
| extra | text | 拼接的 faq_id/sop_id（layer.go:237） |
| created_at | timestamptz，有索引 | 写入时刻（保留清理度量列） |
| deleted_at | timestamptz，有索引 | 软删列；保留清理走 Unscoped 物理删 |

索引共 8 个（含主键，开发库 pg_indexes 实测口径，模型注释 layer_decision_log.go:14-17）：
pkey + trace_id / session_id / customer_id / intent / layer / created_at / deleted_at
（另有 tenant_id 单列索引来自 GORM `index` tag）。

### 3.2 写入链与当前装配状态

写链：`LayerRouter.Route`（`internal/service/layer.go:86`）的 defer 每次调用落一行
（layer.go:95-99）→ `record()`（layer.go:217-249）异步（`utils.SafeGo`，写入失败仅
debug_log 开启时 Warnf，layer.go:243-247）→ `LayerDecisionLogRepository.Record`
（`internal/repository/layer_decision_log.go:28-30`，单条 INSERT）。

Route 的唯一业务调用点是销冠引擎流式路径
（`internal/service/sales_engine.go:676`），且整条通道受特性开关 `layer1`
（默认 false，`internal/pkg/featureflag/flag.go:77`）控制，`SetLayerRouter`
（sales_engine.go:184-186）目前只有测试接线
（`internal/controller/webhook_qq_fullchain_test.go:309-310`）。

**由此推论（巡检判读的前提）**：当前生产装配下本表写入量可能恒为 0，这是「接线休眠」
的正常态，不是写入故障。只有当 `layer1` 开启且 `SetLayerRouter` 被装配调用后，
写入速率才是「每条流式销冠消息一行」。开关关闭时 Route 仍会落行，reason 恒为
`layer1_disabled`——判断写入是否健康必须先看 reason 分布再下结论。

读侧只有 `GetByTraceID` / `Recent`（repository/layer_decision_log.go:33-56），
服务于近期排障，无 HTTP 管理端点。

### 3.3 巡检 SQL

Q1 总行数与磁盘体积（看存量基数）：

```sql
SELECT count(*) AS total_rows,
       pg_size_pretty(pg_total_relation_size('layer_decision_logs')) AS total_size;
```

Q2 写入增速（近 24h 与前一个 24h 对比；接线休眠期两列都应为 0）：

```sql
SELECT count(*) FILTER (WHERE created_at >= now() - interval '24 hours') AS last_24h,
       count(*) FILTER (WHERE created_at >= now() - interval '48 hours'
                       AND created_at <  now() - interval '24 hours') AS prev_24h
FROM layer_decision_logs;
```

Q3 写入断流检测（最新一行距今多少小时；空表显示 `-infinity`）：

```sql
SELECT latest_row,
       round((extract(epoch FROM (now() - latest_row)) / 3600.0)::numeric, 1) AS hours_since_last
FROM (SELECT coalesce(max(created_at), '-infinity'::timestamptz) AS latest_row
      FROM layer_decision_logs) t;
```

Q4 状态分布（近 7 天按 layer × reason；判读健康度的主要依据）：

```sql
SELECT layer, reason, count(*) AS cnt,
       round(100.0 * count(*) / sum(count(*)) OVER (), 1) AS pct
FROM layer_decision_logs
WHERE created_at >= now() - interval '7 days'
GROUP BY layer, reason
ORDER BY cnt DESC;
```

reason 的合法取值只有 5 个（`internal/dto/layer_chunk.go:14-18`）：`faq_hit`、
`sop_hit`、`fallback`、`layer1_disabled`、`low_confidence_skip`；其余 dto.Reason* 常量
当前无赋值点（模型注释 layer_decision_log.go:24-25 同口径）。出现第 6 种取值即异常。

Q5 性能剖面（近 7 天：耗时分位与 LLM 跳过率）：

```sql
SELECT count(*) AS samples,
       round(avg(wall_ms)::numeric, 1) AS avg_ms,
       percentile_cont(0.5)  WITHIN GROUP (ORDER BY wall_ms) AS p50_ms,
       percentile_cont(0.95) WITHIN GROUP (ORDER BY wall_ms) AS p95_ms,
       round(100.0 * count(*) FILTER (WHERE llm_skipped) / NULLIF(count(*), 0), 1) AS llm_skip_pct
FROM layer_decision_logs
WHERE created_at >= now() - interval '7 days';
```

Q6 滞留/积压（软删残留 + 超保留窗未清理行数；窗口 30 天应与参数
`retention.layer_decision_logs_days` 一致，被调大过就同步改这里的 interval）：

```sql
SELECT count(*) FILTER (WHERE deleted_at IS NOT NULL) AS soft_deleted_rows,
       count(*) FILTER (WHERE created_at < now() - interval '30 days') AS beyond_retention
FROM layer_decision_logs;
```

Q7 每日写入趋势（近 14 天，看增速拐点）：

```sql
SELECT created_at::date AS day, count(*) AS rows
FROM layer_decision_logs
WHERE created_at >= now() - interval '14 days'
GROUP BY 1 ORDER BY 1;
```

### 3.4 阈值与处置指引

| 指标 | 阈值 | 处置 |
|---|---|---|
| Q3 hours_since_last | 接线期 > 24h 无新行 | 查服务日志 `[LayerRouter] record failed`（仅 debug_log 开启时可见，layer.go:244-246）与 DB 连接池告警；确认 `layer1` 开关与 `SetLayerRouter` 装配是否还在。休眠期 0 行是正常态，不算断流 |
| Q4 pct（layer1_disabled） | ≈100% 且持续多日 | 特性开关 `layer1` 处于关闭（默认态）。若是「以为开了实验」的巡检发现，去参数/开关管理核对，不是数据故障 |
| Q4 pct（fallback） | 接线期 > 80% 且 layer1 已开启 | FAQ/SOP 命中链路失效：查 faq_entries / sop_templates 命中数与阈值（sopHitThresh=0.65，layer.go:13；FAQ 阈值 layer.go:113/145） |
| Q4 出现未知 reason 值 | 任何一行 | 代码新写入点未按 dto 常量收口，按 3.3 Q4 注释的 5 值集合对账 |
| Q6 beyond_retention | > 0 且逐日增长 | 保留清理 cron 未跑（见第 5 节），查日志 `[retention_cleanup_cron]`（retention_cleanup_cron.go:71-74 有 failed/skipped 输出） |
| Q7 单日 rows | 环比突增 5 倍以上 | 上游消息量暴涨或 Route 被重复接线；结合 message 域会话量对账 |

---

## 4. 表二：operation_logs（管理操作审计日志）

### 4.1 表结构与索引

表由 AutoMigrate 创建（注册点 `internal/pkg/db/migrate.go:168`；SQL 迁移里没有
CREATE TABLE），结构权威是 `internal/model/operation_log.go:6-22`（struct）与
`:25-27`（TableName）。追加索引两条：
`idx_operation_logs_user_ts (user_id, created_at DESC)`
（`migrations/042_metrics_retention.sql:51-53`）、
`idx_operation_logs_module_created (module, created_at DESC)`
（`migrations/043_short_links_index_and_pool.sql:55`）；另有 tenant_id / user_id /
created_at 单列索引来自 GORM tag。

| 列 | 类型 | 说明 |
|---|---|---|
| id | bigint PK | 自增主键 |
| tenant_id | bigint NOT NULL DEFAULT 1 | 租户列 |
| user_id | bigint NOT NULL，有索引 | 操作者（审计中间件只落登录态写请求） |
| username | varchar(50) | 操作者名 |
| action | varchar(50) NOT NULL | create / update / delete（方法映射，audit.go:365-376） |
| module / resource | varchar(50) | 从请求路径推导（audit.go:442-466；/api/{domain}/{id} 形状回落域名，历史缺陷修复见 audit.go:378-391 注释） |
| resource_id | varchar(50) | 路径首个 ID 段（audit.go:469-477） |
| detail | text | JSON 字符串：method/path/query(已脱敏)/status_code/latency/request，status>=400 时附 error 响应体（audit.go:279-297） |
| old_value / new_value | text | 变更前后值（回滚链路使用） |
| ip / user_agent | text | **应用层加密**，新写入为 `enc:v1:{base64}` 密文（058 迁移 + `internal/repository/operation_log.go:43-49`）；存量明文行读取时透传兼容 |
| created_at | timestamptz，有索引 | 写入时刻（保留清理度量列） |

注意：`migrations/042_metrics_retention.sql:54` 的表注释写「保留期 180 天」是旧口径，
现行为 90 天（第 5 节参数中心为准），表注释未随 CC-04 更新，不要照它定阈值。

### 4.2 写入链

```
AuditMiddleware（全局注册 router.go:268；仅 POST/PUT/DELETE/PATCH 且登录态 user_id 非空才落，
  排除 /api/health、/api/system/info、/api/team/logs —— audit.go:202-208、253-263）
  → saveAuditLog（audit.go:154-168：内存队列容量 1000，满则降级同步写并计 droppedCount）
  → processAuditLogs（audit.go:82-113：批 50 或 5s 定时刷盘；间隔可配，参数
     middleware.audit_flush_interval，config_param_seeds.go:405-408）
  → saveAuditBatch（audit.go:115-152：失败按指数退避重试 3 次，仍失败则
     logger.Errorf "[audit] N 条审计日志在 3 次重试后仍写入失败"）
  → operationLogSink（router/middleware_ports.go:16-31，装配注入 :121）
  → OperationLogService.Create → repository Create（IP/UserAgent 加密落库）
```

读侧/管理端点（`internal/controller/operation_log.go`，路由注册
`internal/router/frontend_aliases.go:590-596`）：列表 `GET /api/operation-logs`
（支持 user_id/action/module/start_time/end_time 筛选与 keyset 游标）、详情
`GET /api/operation-logs/{id}`、统计 `GET /api/operation-logs/statistics`、CSV 导出
`GET /api/operation-logs/export`、手动清理 `POST /api/operation-logs/clean`
（before_date / days，缺省 90 天，controller/operation_log.go:238-283）。

### 4.3 巡检 SQL

Q1 总行数与磁盘体积：

```sql
SELECT count(*) AS total_rows,
       pg_size_pretty(pg_total_relation_size('operation_logs')) AS total_size;
```

Q2 写入增速（近 24h 与前一个 24h 对比）：

```sql
SELECT count(*) FILTER (WHERE created_at >= now() - interval '24 hours') AS last_24h,
       count(*) FILTER (WHERE created_at >= now() - interval '48 hours'
                       AND created_at <  now() - interval '24 hours') AS prev_24h
FROM operation_logs;
```

Q3 写入断流检测（最新一行距今多少小时）：

```sql
SELECT latest_row,
       round((extract(epoch FROM (now() - latest_row)) / 3600.0)::numeric, 1) AS hours_since_last
FROM (SELECT coalesce(max(created_at), '-infinity'::timestamptz) AS latest_row
      FROM operation_logs) t;
```

判读前提：本表只落「登录态写请求」，纯浏览/只读日不产生行；静默期 hours 偏大未必是
故障，要结合当日是否有管理操作判断。

Q4 高频操作分布（近 7 天 Top 20，看是否有陌生 module/action 混入）：

```sql
SELECT module, action, count(*) AS cnt
FROM operation_logs
WHERE created_at >= now() - interval '7 days'
GROUP BY module, action
ORDER BY cnt DESC
LIMIT 20;
```

Q5 失败操作 Top（近 7 天 status_code >= 400；detail 由中间件 json.Marshal 落库
（audit.go:297），`LIKE '{%'` 是对历史非常规行的防御，空 detail 转 NULL 后不计数）：

```sql
SELECT module, action, count(*) AS total,
       count(*) FILTER (
         WHERE (NULLIF(detail, '')::jsonb ->> 'status_code')::int >= 400
       ) AS failed
FROM operation_logs
WHERE created_at >= now() - interval '7 days'
  AND detail LIKE '{%'
GROUP BY module, action
HAVING count(*) FILTER (
         WHERE (NULLIF(detail, '')::jsonb ->> 'status_code')::int >= 400
       ) > 0
ORDER BY failed DESC
LIMIT 20;
```

Q6 整体失败率（近 24h；空集时 fail_pct 返回 NULL 而不是报错）：

```sql
SELECT count(*) AS total,
       count(*) FILTER (
         WHERE (NULLIF(detail, '')::jsonb ->> 'status_code')::int >= 400
       ) AS failed,
       round(100.0 * count(*) FILTER (
         WHERE (NULLIF(detail, '')::jsonb ->> 'status_code')::int >= 400
       ) / NULLIF(count(*), 0), 2) AS fail_pct
FROM operation_logs
WHERE created_at >= now() - interval '24 hours'
  AND detail LIKE '{%';
```

Q7 滞留/积压（超保留窗 90 天未清理行数；窗口被调大过就同步改 interval）：

```sql
SELECT count(*) AS beyond_retention
FROM operation_logs
WHERE created_at < now() - interval '90 days';
```

Q8 加密覆盖抽查（近 7 天：新写入 ip 应全部是 `enc:v1:` 前缀密文；明文增量说明
MASTER_KEY 缺失导致降级明文，见 repository/operation_log.go:44-47）：

```sql
SELECT count(*) FILTER (WHERE ip LIKE 'enc:v1:%') AS encrypted,
       count(*) FILTER (WHERE ip IS NOT NULL AND ip <> ''
                        AND ip NOT LIKE 'enc:v1:%') AS plaintext
FROM operation_logs
WHERE created_at >= now() - interval '7 days';
```

Q9 每日写入趋势（近 14 天）：

```sql
SELECT created_at::date AS day, count(*) AS rows
FROM operation_logs
WHERE created_at >= now() - interval '14 days'
GROUP BY 1 ORDER BY 1;
```

### 4.4 阈值与处置指引

| 指标 | 阈值 | 处置 |
|---|---|---|
| Q3 hours_since_last | > 24h 且当日确有管理写操作 | 查服务日志 `[audit]`：队列满降级（audit.go:164）与重试耗尽（audit.go:144，`[audit] N 条审计日志在 3 次重试后仍写入失败`）；对应 [TROUBLESHOOTING.md](../TROUBLESHOOTING.md) 5.3「审计日志偶发缺失」 |
| Q6 fail_pct | > 5% 持续 24h | 用 Q5 定位 module/action，再走管理台 `GET /api/operation-logs?module=<x>` 看明细；detail.error 里是原始失败响应体 |
| Q7 beyond_retention | > 0 且逐日增长 | 保留清理 cron 未跑（见第 5 节）；应急可用手动端点 `POST /api/operation-logs/clean`（`{"days": 90}`，与自动调度幂等叠加不冲突，config_param_seeds.go:1030-1032 说明） |
| Q8 plaintext | 7 天内出现新明文 | 检查 `FIELD_ENCRYPTION_KEY`/MASTER_KEY 环境变量；注意 [DEPLOYMENT_GUIDE.md](../DEPLOYMENT_GUIDE.md) 的告诫——该密钥不可直接轮换，会使已加密字段无法解密 |
| Q1 total_rows 增速 | 长期逼近保留窗容量预期 | 调参前先读参数中心描述：调大保留更久但表体积线性上涨，调小省空间但审计可回看变短 |

---

## 5. 保留期清理衔接（巡检不重复发明清理）

两表都挂在同一个进程内保留清理看门狗上（CC-04），**巡检只核对清理是否收敛，
禁止在巡检脚本里另写 DELETE**：

- 机制：`internal/service/retention_cleanup_cron.go:48-60`——进程启动即自启（`init()`
  装配 :93-99），24h 周期逐表触发；单条 DELETE 每批最多 1000 行
  （`id IN (SELECT id ... LIMIT)` 圈批，retention_cleanup.go:67-89），逐租户 Fanout，
  带 `deleted_at` 的表一律 Unscoped 物理删（retention_cleanup.go:65-66）。
- 两表规格：`internal/service/retention_cleanup_tables.go`——
  `layer_decision_logs` 30 天（:62-68）、`operation_logs` 90 天（:92-101）。
- 参数（管理台参数中心 `retention` 组，改完无需重启、每轮热读，参数缓存 TTL 60s）：
  - `retention.layer_decision_logs_days`（种子 config_param_seeds.go:1018-1021，默认 30）
  - `retention.operation_logs_days`（种子 config_param_seeds.go:1030-1032，默认 90）
- 参数非法（<=0）不会「永不清理」而是回落代码兜底并 Warnf
  （retention_cleanup.go:113-119）；要停清理应摘调度，不是配 0。
- 本仓无独立 Prometheus exporter（[TROUBLESHOOTING.md](../TROUBLESHOOTING.md) 398 行：
  指标落库 + SQL 巡检趋势是现状），所以本文的 SQL 就是当前的巡检手段。

---

## 6. 巡检频率与清单

| 频率 | 执行项 |
|---|---|
| 每周 | operation_logs：Q1/Q2/Q3/Q6/Q7/Q9；两表健康 Q10（下） |
| 每月 | layer_decision_logs：Q1-Q7（接线休眠期 Q2/Q3 快速过即可）；operation_logs：Q4/Q5/Q8 |
| 事件驱动 | 审计缺失投诉、磁盘告警、接口批量失败时全量跑 |

Q10 两表健康总览（行数/死元组/autovacuum；n_dead_tup 长期 > n_live_tup 的 20% 说明
清理 DELETE 后 autovacuum 跟不上，DBA 侧安排 VACUUM ANALYZE）：

```sql
SELECT relname, n_live_tup, n_dead_tup,
       pg_size_pretty(pg_total_relation_size(relid)) AS total_size,
       last_autovacuum
FROM pg_stat_user_tables
WHERE relname IN ('layer_decision_logs', 'operation_logs')
ORDER BY relname;
```

---

## 7. 已知盲区（如实声明）

1. **审计写入失败的进程内计数未接监控**：`GetAuditDroppedCount`
   （middleware/audit.go:572-575，队列满降级计数 :77-80）目前无任何生产读点，
   只能靠日志与本文 Q3 间接发现丢失。
2. **登录/登出专项审计入口未接线**：`LogLogin`/`LogLogout`/`LogCustom`
   （audit.go:523-563）无生产调用点（仅测试引用），登录审计实际依赖 AuditMiddleware
   对登录写请求的通用落库。
3. **layer_decision_logs 写失败可观测性弱**：record 失败只在 debug_log 开启时
   Warnf（layer.go:243-247），关闭开关时静默丢弃，Q3 断流检测是唯一兜底。
4. **陈旧注释两处**：audit.go:283 与 api_logger.go:255 仍写「audit_logs」，
   以本文第 1 节为准；042 迁移的表注释「保留期 180 天」是旧口径（见 4.1）。
5. **本文 SQL 未做真库执行验证**：编写环境（worktree 隔离机）无 docker/psql、
   8217 专用测试库未启动，无法连库验证；SQL 语法经人工按 PG 15 方言目检
   （重点：`FILTER` 聚合、`NULLIF` 防除零、`percentile_cont`、`extract(epoch)`
   的 numeric 收尾）。首次在真实库执行时请先跑 Q1/Q3 这类无 JOIN 简单语句验证连通。
