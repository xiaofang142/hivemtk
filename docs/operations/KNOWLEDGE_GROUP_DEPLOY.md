# 智能体知识库隔离 - 部署指南

> 适用版本: 本特性随迁移 `v3.15.0` 进主干（建表那条，见 §2.1），此后 v3.22.1 / v3.22.2 各改一刀  
> 部署类型: 宿主机二进制升级 / 灰度发布 / 迁移回滚  
> 文档版本: 1.1 (2026-09-22)
>
> ⚠️ 本文件 1.0 版（`34840307`「企业级文档」那一批写入）里有一批"照着做一定失败"的内容：
> 不存在的迁移文件名、不存在的表名、不存在的指标名、一个从未存在过的 `config-center` 服务，
> 以及一段和 `deploy/helm/hivemtk` 相互矛盾的 K8s YAML。
> 2026-09-22 逐条按当日产码 + 本机活库重测后订正，每处都留了"原先是什么、为什么错"的账
> （清剿同类问题的第二遍可跳过取证，但请不要跳过核对：结论会随产码漂移）。

---

## 1. 部署前准备

### 1.1 环境要求

| 组件 | 最低版本 | 推荐版本 | 备注 |
|------|----------|----------|------|
| PostgreSQL | 13.x | 15.x | 含 pgvector 扩展（根 `docker-compose.yml` 用的镜像就是 `pgvector/pgvector:pg15`） |
| user-server | 迁移注册表 ≥ `v3.15.0` | 当前主干 | 仓内没有"产品版本号"这一面：`v0.9.x` 这种写法找不到出处。今天复算的命中面（命令要把本文档自己排掉，否则会被本文档那几条"这个 tag 不存在"的订正行污染）：`grep -rn "v0\.9" docs/ *.md | grep -v KNOWLEDGE_GROUP_DEPLOY` 只命中 `THIRD_PARTY_LICENSES.md:86` 那个第三方依赖 `github.com/pkg/errors`；产码、`deploy/helm/`、compose 与 Dockerfile 面（仓内无 Dockerfile）对 `v0\.9` 零命中 ⇒ 对得上的版本面只有迁移号与 `deploy/helm/hivemtk/Chart.yaml:19` 的 `appVersion: "0.1.0"` |
| Redis | 6.x | 7.x | **能不起，但不等于"可选"**：Redis 挂掉时各处缓存/限流按各自策略降级进程内（如 `internal/service/reach_gcra_limiter.go:81`），进程照跑；可 `/readyz` 会因 `50304` 恒返 503（`internal/router/health.go:156` 起）⇒ 上编排器时别把它当"没有也行" |
| Docker | 20.x | 24.x | 只有仓内数据层用到：根 compose 只起 PG + Redis，应用侧部署形态见 §4 |

### 1.2 备份策略

端口口径与 §1.3 同，且**不是**照 compose 的默认值：根 `docker-compose.yml` 写的是
`127.0.0.1:${USER_POSTGRES_HOST_PORT:-8202}:8202`，而仓内 `.env` 把
`USER_POSTGRES_HOST_PORT` 覆盖成了 `8232`（和 `user-server/config.yaml:67` 的 `DB_PORT` 默认值一致）
⇒ 本机今日实测：宿主机上听的是 **8232**（`pg_isready -p 8232` rc=0、`-p 8202` rc=2 没有响应；
`docker ps` 那行是 `127.0.0.1:8232->8202/tcp`，容器内才是 8202）。下面两条形如
`pg_dump -p "$PGPORT"`，先把 `PGPORT` 定成你这套实例实际听的那个口，别照抄字面量。

```bash
PGPORT=8202                                   # 或 8232，见上
mkdir -p /backup                              # 两个 -f 都写死在 /backup 下，目录不存在 = 命令失败
set -a; . ./.env; set +a                      # POSTGRES_PASSWORD 从这里来（.env 里的实际键名，见 §7.1）
export PGPASSWORD="$POSTGRES_PASSWORD"

# 1. 备份这两张表
pg_dump -h 127.0.0.1 -p "$PGPORT" -U admin -d user_db \
  --table=knowledge_bases \
  --table=agent_kb_bindings \
  -F c -f /backup/knowledge_group_pre_deploy_$(date +%Y%m%d_%H%M%S).dump

# 2. 备份内容库 (FAQ/RAG/SOP)
pg_dump -h 127.0.0.1 -p "$PGPORT" -U admin -d user_db \
  -F c -f /backup/content_pre_deploy_$(date +%Y%m%d_%H%M%S).dump
```

> `-U admin` / `-d user_db` 不是可覆盖的风格选择：`user-server/config.yaml:68,69` 里 `user` 与
> `dbname` 是写死的字面量（只有 `host`/`port` 留了 `${DB_HOST:}` / `${DB_PORT:}` 占位），
> 所以库里的角色名与库名就跟着这两个值。改它们要连配置文件一起改。

### 1.3 健康检查

```bash
# 数据库连接（宿主机的口按 §1.2：本机 .env 把它覆盖成了 8232，8202 上今日实测没有响应）
pg_isready -h 127.0.0.1 -p 8232 -U admin    # rc=0 accepting / rc=2 down
# 注意别用 curl 打这个口：PG 上没有 HTTP 服务，只会得到一句连接错误、说不清是库坏了还是问错了
# 用户服务健康检查：/health 探 DB+Redis（任一 down 返 503），/healthz 只报存活
curl -sf http://127.0.0.1:8204/health
```

> 路径与端口按今天的路由表核过：服务监听 8204（`PORT` 可覆盖，默认值在
> `internal/config/ports.go:8` 的 `DefaultListenPort`），根上有**三条**探针（`router.go:188-190`）：
> `/health` 是人读的依赖汇总（`HealthCheck`，`health.go:72`，DB `SELECT 1` + Redis Ping）、
> `/healthz` 只报存活（`LivenessCheck`，`health.go:135`，不碰任何依赖）、
> `/readyz` 是**给编排器用的**就绪判定（`ReadinessCheck`，`health.go:156`，除 DB/Redis 外还看
> LLM provider 与 embedding），业务码把坏的那格点名到列（今日实测五档：`50302` 库没初始化、
> `50303` 库探测失败、`50304` redis、`50305` 无健康 LLM provider、`50306` embedding，
> `health.go:162-180`）。
> 还有第四条在 `/api` 下面：`GET /api/health`（`admin_routes.go:37` 的 public 组 →
> `systemInfoCtrl.Health`，只回 `data.status = "ok"`、不探任何依赖），`scripts/deploy-user.sh:51` 的
> `HEALTH_URL` 用的就是它 ⇒ 看到"发布脚本的健康检查过了"不等于"依赖是好的"。
> 本机今日实测四条全 200（`/health` 与 `/readyz` 回 `data.checks` / `data.status`，
> `/api/health` 回 `{"code":0,"message":"ok","data":{"status":"ok"}}`，四条外层都是同一个信封，
> 区别只在 `data` 里有没有依赖项）。
> 本节原先写的是 `:8202/health` 与 `:8080/api/v1/health`，
> 三条都是死路：8202 无 HTTP、8080 无人监听、`/api/v1` 前缀不存在（今日实测依次 `rc=7` / `rc=7` / `404`）
> ⇒ **没有 `/api/v1/*` 这一级前缀**，业务路由挂在 `/api` 下（`router.go:285,309`）。

---

## 2. 数据库迁移

### 2.1 建表口径：服务启动期自动执行，不需要手工 psql

两张表由服务启动时跑的迁移注册表建立 ——
`cmd/api/main.go:257-258` 把 `migrations.RegisterMigrations` 交给 `MigrationService` 并 `ExecuteUpgrade`，
其中三条落在本特性上：

| 迁移 | 文件 | 做什么 |
|------|------|--------|
| `v3.15.0` | `internal/migration/migrations/v3_15_kb_unification_migration.go` | `CREATE TABLE IF NOT EXISTS knowledge_bases`（含 `kb_code` 与 `uq_knowledge_bases_kb_code`）+ `agent_kb_bindings`，并给 faq/sop/knowledge_documents 三表加 `agent_id` |
| `v3.22.1` | `internal/migration/migrations/v3_22_1_soft_delete_migration.go:32` | 给 `knowledge_bases` 等核心表加 `deleted_at`（软删，§6.1 的 DELETE 走的就是它） |
| `v3.22.2` | `internal/migration/migrations/v3_22_2_enum_optimize_migration.go:107` | 把 `type` / `owner_type` 换成 PG 枚举 `kb_type_enum` / `kb_owner_type_enum` |

> ⚠️ 本节旧版表格列的四个文件 —— `migrations/20260731_001_create_knowledge_bases.up.sql`、
> 它的 `.down.sql`、`migrations/20260731_002_create_agent_kb_bindings.up.sql` 与 `.down.sql` ——
> **从来没有存在过**：`git log --all -S"20260731_001_create_knowledge_bases" --name-only` 的命中文件只有本文档
> （`34840307`「企业级文档」那一批写进来，`7a26965a` 清了同批的监控/告警段落却漏了这里），
> `migrations/` 下也没有任何 `20260731_*` 文件名。⇒ 照抄旧版 §2.2 的两条 `psql -f` 只会得到
> `did not find any file matching...`，而它下面那句"确认无错误后正式执行"就永远走不到。

> `migrations/035_knowledge_base.sql` 是个**真文件**，但同样别拿它建表：
> ① 没有任何脚本执行它 —— 全仓唯一的 psql 应用点在 `scripts/bootstrap.sh:141`，名单只有 `027`/`028`，
>    它只在 `docs/architecture/adr/ADR-007-rag-retrieval.md:78` 被提过一次名字；
> ② 它的形状与实际库不一致：列名写的是 `is_enabled`、没有 `kb_code`、`type`/`owner_type` 用 CHECK 约束而不是枚举。
>    实际实例今日实测：列为 `id kb_code type name description owner_type owner_agent_id member_count doc_count
>    enabled created_at updated_at deleted_at`，`type` 的 `udt_name` 是 `kb_type_enum`，
>    `pg_constraint` 只剩 `knowledge_bases_pkey` 与 `uq_knowledge_bases_kb_code`（`chk_kb_type` 不在了）。
> 真用它建了表，`v3.15` 的 `CREATE TABLE IF NOT EXISTS` 会因为"表已存在"整段跳过，
> 缺 `kb_code` 这件事要等到服务第一次写 KB 才炸 —— 比建表时就炸难查得多。

### 2.2 确认迁移跑过

```bash
# 1) 启动日志里找 v3.15 的分步痕迹（该迁移每步都打 [v3.15] 前缀）
grep "\[v3.15\]" <user-server 日志> | tail -20

# 2) 或直接问库（端口口径见 §1.2：本机实例是 8232，compose 的 8202 只是容器内侧）
PGPASSWORD="$POSTGRES_PASSWORD" psql -h 127.0.0.1 -p 8232 -U admin -d user_db -c "\d knowledge_bases"
```

### 2.3 回滚

`v3.15` 有配套的 `Down()`（`v3_15_kb_unification_migration.go:165`，先 `declineIndexDrop` /
`declineColumnDrop` 摘索引和 `agent_id` 列再删表），但它**不是**手工 psql 的口子，
入口是管理端接口：`POST /api/migration/rollback`，体 `{"target_version":"v3.14.0"}`
（`internal/router/system_routes.go:122-125` 的 admin 组，还叠加 `AdminAuthMiddleware`；
请求字段名取自 `internal/controller/migration.go:130-132`）。
接口是 auth 组内的，token 取法见 §6.1。

### 2.4 验证迁移

```sql
-- 检查表结构
\dt knowledge_bases
\dt agent_kb_bindings

-- 检查索引（本机活库实测两表各 6 条：knowledge_bases 侧
-- knowledge_bases_pkey / uq_knowledge_bases_kb_code / idx_kb_type / idx_kb_owner_agent /
-- idx_kb_enabled / idx_knowledge_bases_deleted_at）
\di+ knowledge_bases
\di+ agent_kb_bindings

-- 验证数据 (空表)
SELECT COUNT(*) FROM knowledge_bases;     -- 应为 0
SELECT COUNT(*) FROM agent_kb_bindings;  -- 应为 0
```

> 注意最后一条：`应为 0` 只在**从没写过 KB 的全新库**上成立。它不是被 seed 灌的 ——
> `scripts/seed/expand_knowledge_base.py` 只写 `knowledge_documents` / `knowledge_chunks`
> （该文件 614 / 622 行的两条 INSERT），`user-server/cmd/seed/` 里 `knowledge_bases` 零命中；
> 真正会留行的是回归与 e2e 夹具走 API 建的那些。本机实例清掉本批验收脚本自己造的 3 行之后
> 仍有 6 行（`KB-REG` / `KB-REG2` / `KB-AUTO-BIND-5` / `KB-FINAL-6` / `r43-e2e2` / `r85_kb`，属别的泳道夹具，不动）。
> ⇒ 把"应为 0"当硬闸在开发实例上必红，它只在验收新装环境时有意义。

---

## 3. 灰度发布策略

### 3.1 阶段规划

```
阶段 0: 影子流量 (Day 0)
   └─ 1% 流量, 仅观察, 不影响业务
   
阶段 1: 小流量验证 (Day 1-2)
   └─ 5% 流量, 监控关键指标
   
阶段 2: 中等流量 (Day 3-5)
   └─ 25% → 50% 流量
   
阶段 3: 全量发布 (Day 6+)
   └─ 100% 流量
```

### 3.2 特性开关

> ⚠️ 本节写的是"隔离特性落地后"的拨闸口径，**今天拨动它不会打开任何代码路径**：
> 键名 `knowledge_group_isolation` / `knowledge_group_rollout_percent` 在 Go 侧零引用
> （`git grep -n "knowledge_group_isolation\|GroupIsolation" -- '*.go'` 实测 0 命中），
> 且旧版本这里贴的那段"出处 `internal/config/feature_flags.go`"的常量块指向的是**一个从未存在过的文件**
> （`git log --all --diff-filter=A -- .../feature_flags.go` 两种路径前缀均 0 命中，仓内现在也没有同名文件）。
> ⇒ 保留这一节是为了说明开关机制长什么样；真要上线该特性，得先在产码里登记这两个键。

开关不落在代码常量里，而是 `feature_flags` 表的行（`internal/model/feature_flag.go:26` 的 `TableName()`，
迁移 `migrations/051_feature_flags.sql:4`），灰度比例就是该行的 `rollout_percentage` 列（0–100，
`internal/service/feature_flag.go` 校验）。

### 3.3 灰度控制

```bash
BASE=http://127.0.0.1:8204
# TOKEN 取法见 §6.1（/api/* 整组要 JWT，管理端那几条还叠加 AdminAuthMiddleware）

# 1) 登记开关（键名必填，比例可给）
curl -sf -X POST $BASE/api/feature-flags \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"key":"knowledge_group_isolation","name":"知识库隔离","enabled":true,"rollout_percentage":25}'

# 2) 调灰度：按 flag 的 id 而不是 key（路由 business_routes.go:167-178，体字段 percentage 必填）
curl -sf -X POST $BASE/api/feature-flags/<id>/rollout \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"percentage": 50}'

# 3) 验证评估结果（服务端按 attributes 命中比例给结论）
curl -sf -X POST $BASE/api/feature-flags/evaluate \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"key":"knowledge_group_isolation","attributes":{}}'
```

> 上面三段用的是 `curl -sf`，这是本文档 §7.1 点名过的"吞红"写法：口令过期 / 权限不够 / 体字段拼错
> 时 `-f` 让 curl 静默退 22，屏幕上只有命令本身、没有红因。今天这一族的正确读数是这样取的
> （本批就是这么核的）：`curl -s -w '\n%{http_code}'`，先看码再看体。

> 环境变量这条路**不存在**：`FEATURE_KNOWLEDGE_GROUP_ISOLATION` / `..._ROLLOUT_PERCENT` 在服务侧零读取点，
> 旧版本这里的 `export FEATURE_...` 与 `curl -X PATCH http://config-center/...` 都指向没有的东西。
> `config-center` 是本文档自己造出来的主机名，不是被删掉的服务：它由 `34840307`（企业级文档那一批）写入本文档两处，
> `7a26965a` 清了其中一处、漏了 §3.3 这条 curl，而两仓其它任何产码 / compose / 部署清单里它一次都没出现过
> （`git log --all -S"config-center" --name-only` 只指向本文档）。

### 3.4 灰度验证检查清单

| 项 | 验证方式 | 通过条件 |
|----|----------|----------|
| 数据库表存在 | `\dt knowledge_bases` | ✅ 表存在 |
| 索引创建成功 | `\di+ knowledge_bases` | ✅ 6 个索引 |
| Service 可调用 | 单元测试 | ✅ 全部通过 |
| E2E 业务通过 | e2e 测试 | ✅ 7/7 通过 |
| 灰度命中有痕迹 | SQL: `feature_flag_eval_logs` | 每次 `/evaluate` 落 1 行——**但 KB 隔离这一格永远查不到数**，见下面的实跑说明 |
| 响应时间 | SQL: `layer_decision_logs.wall_ms` P99 | < 200ms（本机 0 行，同下） |

```sql
-- 灰度命中痕迹：/evaluate 每次异步写一行（internal/service/feature_flag.go:304-313）
SELECT count(*) FROM feature_flag_eval_logs WHERE flag_key = 'knowledge_group_isolation';
-- 谁改过开关：feature_flag_audit_logs（同表族，Action / ActorID / Detail）
SELECT flag_key, action, actor_id, created_at FROM feature_flag_audit_logs
 WHERE flag_key = 'knowledge_group_isolation' ORDER BY id DESC LIMIT 5;
```

> **这两条 SQL 查不到"隔离有没有生效"，一行都查不到**。三层原因，逐层都是本机实跑：
>
> 1. 本批取证**之前**，`knowledge_group_isolation` 这个 key 从没被登记过：`feature_flags` 共 4 行，
>    key 是 `deep_test_flag` / `staff_canary_999` / `test.feature` / `x`（全是历轮测试遗留）；
>    `feature_flag_eval_logs` 共 1,586 行，落在 `r51_rollout_50`(1,582) / `r39_test_flag`(3) /
>    `r39_flag2`(1) 三个 key 上 ⇒ 当时按上面的 SQL 查，两条都是 0。
> 2. 本批把 §3.3 那三级真跑了一遍（登记 → rollout 50 → evaluate）：三个请求都 **200**，
>    `/evaluate` 返回 `{"code":0,"data":{"key":"knowledge_group_isolation","enabled":true,"reason":"rollout"}}`，
>    异步评估日志落了 1 行（`enabled=true / reason=rollout / context_id=anonymous`，`context_id`
>    是 attributes 里没给 context 时的默认值），审计表落了 `create,rollout,delete` 三行。
>    跑完把 flag 行删了 ⇒ `feature_flags` 仍是 4 行，但这两张**只追加**的日志表里现在**有**这个 key 的行，
>    而且它们指向一个已经不在 `feature_flags` 里的 key（删 flag 不级联清日志）。
>    ⇒ 今天复跑这两条 SQL 会得到 `1` 和 `3`，**这两个数同样与本特性无关**，别拿它们当"灰度在跑"。
> 3. 最根本的：`knowledge_group_isolation` 在 Go 侧零引用（§3.2），而 KB 隔离是产码里**无条件**生效的
>    （`repository/knowledge_base.go:133-141` 那条 WHERE 就是它，前面没有任何开关门）
>    ⇒ 要验隔离，用 §7.2 的第 4/5 项（两个 agent 各建一条、互查可见集），别在这里数行。

> 这张表旧版还有两行是死的 / 半死的，别再照着查：
> ① **"错误率 | SQL: `audit_logs`"** —— 库里**没有 `audit_logs` 这张表**
> （`information_schema.tables WHERE table_name='audit_logs'` 实测 0 行，`information_schema` 里
> 带 audit 的只有 `config_param_audit_logs` / `feature_flag_audit_logs` / `llm_routing_audit` /
> `security_audit_items` / `security_audits` / `tool_call_audits`，Go 侧 `"audit_logs"` 字面量零命中）。
> 真正记"谁在什么时候改了什么东西"的是 **`operation_logs`**（`internal/model/operation_log.go:23` 的
> `TableName()`，行数随每次写请求增长、口径见 §6.2 那条"会漂"的说明），写入方是 `AuditMiddleware()`（`internal/middleware/audit.go:169`，
> 在 `router.go:211` 全局注册）。它有两道门槛，直接决定"错误率能不能从库里数出来"：
> 只记**写方法**（`audit.go:201-203`：非 POST/PUT/DELETE/PATCH 直接 return），
> 且**要求上下文里已有 `user_id`**（`audit.go:205,208-210`）⇒ 匿名 401 那类压根不落库。
> 在这两道门内的拒绝是有痕迹的：本机实测 `detail` 里带 `"status_code":403` 的行有 **887** 条，
> 所以"越权只能看访问日志"这句旧结论**不成立**，正确口径是 §6.3 的那条 SQL。
> 但**隔离判定本身没有任何计数器**：`isolation_violation_total` 在产码里零命中（今日实测），
> `middleware/ownership.go:157-162` 命中时就是 `AbortWithStatusJSON(403)` 一句返回，
> 不打点、不写专用表；而 GET 侧（`KnowledgeBaseRepository.ListByAgent` 那条读路径）
> 既不走审计门、也就没有任何行 ⇒ 想量"读侧越权尝试次数"目前只能靠 `logs/user-server.log`
> 里的 `event=api_interaction` 行（§8.3）。
> ② **响应时间那一列是真的但本机没数**：`layer_decision_logs` 存在、`wall_ms` 是该模型的真实列
> （`internal/model/layer_decision_log.go:40`），可这张表在本实例实测 **0 行**
> ⇒ 在它上面算 P99 只会得到 `count=0` 而不是"达标"，跑之前先确认有 CS 会话流量。
> 逐请求时延有现成的两个来源，见 §8.4。
> ③ 表里那 6 个索引、e2e 的 7 个用例今日都核过：`agent_kb_bindings` 与 `knowledge_bases` 各 6，
> `user-server/test/e2e/agent_kb_e2e_test.go` 恰好 7 个 `func Test`（含 §8.3 点名的
> `TestE2E_MultiAgent_KnowledgeIsolation`，`agent_kb_e2e_test.go:72`）。

---

## 4. 容器化部署（仓内不提供镜像定义）

> ⚠️ 本仓自 `94415060`「重构宿主机部署」起**不再随附任何 Dockerfile**（`git ls-files | grep -i dockerfile` = 0），
> 部署形态是宿主机二进制 + 根 `docker-compose.yml` 只起数据层。下面这段是"你要自己容器化时"的样例，
> 口径按今天的产码核对过：入口包 `./cmd/api`（仓内没有 `cmd/user-server`）、监听 8204（`PORT` 可覆盖）、
> 存活探针 `/healthz`（`internal/router/router.go:189`）、Go 版本跟 `user-server/go.mod` 的 `go 1.25.0`。
> DB 只有 `DB_HOST`/`DB_PORT` 两个占位可覆盖，账号与库名在 `user-server/config.yaml` 里是写死的
> （`user: admin`、`dbname: user_db`）⇒ 容器化要挂一份改过的 `config.yaml`，别指望 `POSTGRES_USER` 这类变量。

### 4.1 自建镜像样例

```dockerfile
# 自建镜像样例（放到你自己的路径，仓内不入库）
FROM golang:1.25 AS builder
WORKDIR /build
COPY user-server/ .
RUN CGO_ENABLED=0 go build -o /build/user-server ./cmd/api

FROM debian:bookworm-slim
# curl 是给 4.2 的 healthcheck 用的：bookworm-slim 出厂不带 curl，只装 ca-certificates 会让探针恒失败
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl && rm -rf /var/lib/apt/lists/*
COPY --from=builder /build/user-server /usr/local/bin/
ENV PORT=8204
EXPOSE 8204
CMD ["/usr/local/bin/user-server"]
```

### 4.2 Docker Compose

```yaml
# 片段：服务名要与数据层容器同网段（根 compose 里的数据层服务名是 mtk-postgres / mtk-redis）
services:
  user-server:
    image: hivemtk/user-server:latest        # 由上面那段自建 Dockerfile 出，仓内无此镜像
    ports:
      - "8204:8204"
    environment:
      - PORT=8204
      - DB_HOST=mtk-postgres        # user-server/config.yaml 的占位符是 ${DB_HOST:127.0.0.1} / ${DB_PORT:8232}
      - DB_PORT=8202
    depends_on:
      - mtk-postgres
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8204/healthz"]
      interval: 30s
      timeout: 5s
      retries: 3
```

> 特性开关不走环境变量：`FEATURE_KNOWLEDGE_GROUP_ISOLATION` / `..._ROLLOUT_PERCENT` 在 Go 侧**零读取点**
> （本轮 `git grep` 实测），开关由 `feature_flags` 表驱动（`internal/model/feature_flag.go:26` 的
> `TableName()`、`RolloutPercentage` 列，`internal/service/feature_flag.go` 校验 0–100），
> 管理入口是 `/api/feature-flags`（`internal/router/business_routes.go:167-178`，另叠加 `AdminAuthMiddleware`）⇒ 见 §3.3。

### 4.3 升级 / 回滚入口：`scripts/deploy-user.sh`

`docker pull hivemtk/user-server:v0.9.1` 这一条（旧版 §4.3 的第一步）在本仓是**双重不存在**：
镜像名没有任何构建方（§4 头注：仓内无 Dockerfile），`v0.9.1` 这个 tag 也没有出处（§1.1 那条版本号说明）。
今天真实存在的升级入口只有一个，就是宿主机形态的发布脚本：

```bash
# 只重构建并重启本地 API（不碰前端、不推远端）
./scripts/deploy-user.sh --api-only

# 先看它将要执行什么，不真做
./scripts/deploy-user.sh --api-only --dry-run
```

它做的事（`scripts/deploy-user.sh:195-235`，逐条对应）：
`CGO_ENABLED=0 go build -o bin/user-server ./cmd/api` → `pkill -f` 三代可能残留的进程名 +
`lsof -ti tcp:8204 | xargs kill` 兜底 → `set -a; source .env` 后 `nohup` 起新进程，
日志重定向到 `user-server/logs/user-server.log` → 打 `$HEALTH_URL`（默认
`http://127.0.0.1:8204/api/health`，见 §1.3 第四条探针）等健康；超时就 warn 并让你去看那个日志。
`SKIP_HEALTHCHECK=1` 可跳过等待，`USER_SERVER_PORT` 可换口。

> 健康检查用的是 `/api/health` 那条**不探依赖**的探针 ⇒ 它过了只说明进程起来了；
> 要确认 DB/Redis 也活着，另打 `/health`（§1.3）。
> 回滚 = 用旧提交再走一遍同一脚本；但**迁移不会跟着回滚**（它是启动期往前追平的），
> 数据侧回滚的口子是 §2.3 那条 `POST /api/migration/rollback`。
> 真要把 user-server 也容器化（§4 的镜像样例），升级/回滚就变成"换 tag + 重建容器"，
> 那 compose 文件得由你自己写 —— 仓内那份只起数据层（`mtk-postgres` / `mtk-redis` 两个服务）。

---

## 5. K8s 部署

### 5.1 用仓内 chart，别再抄一份 Deployment

本节 1.0 版手写了一整段 `kind: Deployment` YAML。它在仓里是**第二份**K8s 定义 ——
`deploy/helm/hivemtk/`（`Chart.yaml` + `values.yaml` + `templates/{deployment,service,ingress}.yaml`）
才是本仓登记的那份（OPT-MISC-01），而两份恰好互相矛盾，抄文档的人拿到的是错的那份：

| 那段 YAML 写的 | 产码 / chart 实际 |
|----------------|-------------------|
| `readinessProbe: /health` | `/readyz`（`router.go:190`）。`/health` 是人读的依赖汇总，`health.go:156` 起才是给编排器的那条 |
| env 只有 `PORT` / `DB_HOST` / `DB_PORT` | 少 `MASTER_KEY` 与 `JWT_SECRET` 就是 `os.Exit(1)` / `panic`（`cmd/api/main.go:183-189`、`internal/pkg/utils/jwt.go:63-72`）⇒ CrashLoopBackOff；少 `POSTGRES_PASSWORD` 同理（`internal/pkg/db/db.go:38-44`） |
| `secretKeyRef: name: pg-credentials`、`key: host` | 全仓只有本节出现过 `pg-credentials`（`grep -rn pg-credentials` 除本文件外 0 命中）——没有任何东西创建它；chart 用的是 `hivemtk-secrets`，key 叫 `db-host` |
| `image: hivemtk/user-server:v0.9.1` | 仓内无镜像定义、无该 tag（§4 头注、§1.1） |

⇒ 现在这一节只给指向，数值以 chart 为准：

```bash
helm template hivemtk ./deploy/helm/hivemtk            # 渲染预览
helm lint ./deploy/helm/hivemtk                        # 结构检查
helm install hivemtk ./deploy/helm/hivemtk -n hivemtk --create-namespace
```

端口（`service.targetPort` 与两个探针的 `port` 都等于 `DefaultListenPort=8204`）、
探针路径、env 名单这三件事由 `user-server/internal/config/ports_test.go` 的
`TestHelmChartAlignsWithCodePorts` 跟产码对账（它直接读 `values.yaml` / `deployment.yaml` /
`README.md` 三个文件）⇒ 改 chart 不必担心文档漂移，跑 `go test ./internal/config/` 就会拦。
这道门自己的反向核验（2026-09-22，跑在只含已提交字节的克隆里）：基线格全绿，随后 7 格注码
（`targetPort`→8080 / 单个探针口→8080 / `containerPort`→写死数字 / env 名 `MASTER_KEY`→改一名 /
追加旧名 `DB_PASSWORD` / README 删一条 `--from-literal` ）逐格红在**该红的那个子用例**上。
其中 README 那一格第一遍是**无效变异**：只把行尾换成占位、判据子串还在 ⇒ 门当然不红，
第二遍整行删掉才拿到红因 —— 改值式变异必须断言"判据串的命中数变 0"，否则测的是注码本身。
chart 自身的状态与限制（**它是骨架、从没在真集群上跑过，本机也没有 helm 可 lint**）写在
`deploy/helm/hivemtk/README.md`，装之前先读那一页的"2026-09-22 修掉的四处"。

### 5.2 滚动更新（本机形态，不是 compose）

> 私域部署: 无 Argo Rollouts 灰度控制台。旧版这里写的"`docker compose up -d` 触发滚动重启 +
> `git checkout HEAD~1 && docker compose up -d` 回退镜像"两条都落不了地：根 `docker-compose.yml`
> 里只有 `mtk-postgres` / `mtk-redis` 两个服务（今日实测 services 名单），
> **没有 user-server 这一格**，`docker compose up -d user-server` 只会报 no such service；
> 而"回退镜像"那半句隐含的"数据跟着回去"也不成立 —— 迁移是启动期往前追平的，二进制退回上一版
> 不会把表结构带回去。

今天的做法：

1. 升级 / 回退二进制：`./scripts/deploy-user.sh --api-only`（过程与日志位置见 §4.3；
   回退就是切到上一个提交再走一遍这条命令）。
2. 起服务后先 `curl -sf $BASE/health`（带依赖，见 §1.3），再跑 §7.1 的验收脚本。
3. 数据侧要退：`POST /api/migration/rollback`（§2.3）。**先备份**（§1.2），
   `v3.15` 的 `Down()` 会删列删表。
4. 观察窗口看两个面：`user-server/logs/user-server.log`（逐请求 `event=api_interaction` + `latency_ms`，
   `internal/middleware/api_logger.go:74,98`，`router.go:209` 注册）与 `operation_logs` 表（写操作审计，§6.3）。

---

## 6. 监控与告警

### 6.1 部署后立即验证

```bash
BASE=http://127.0.0.1:8204

# 1. 健康检查（/health 带 DB+Redis 探测，任一不可用返 503）
curl -sf $BASE/health

# 2. 测试核心 API —— /api/* 整组挂了 JWTAuthMiddleware，不带 token 一律 401；
#    口令从环境取（bootstrap.sh 那条链：SEED_PASSWORD > ADMIN_PASSWORD > 公开默认值，
#    scripts/bootstrap.sh:63-64 是同一个口径），别写进文档
TOKEN=$(curl -sf -X POST $BASE/api/auth/login -H 'Content-Type: application/json' \
  -d "{\"username\":\"admin\",\"password\":\"$ADMIN_PASSWORD\"}" | jq -r '.data.token')
curl -sf -X POST $BASE/api/knowledge-bases \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{
    \"kb_code\": \"KB-DEPLOY-TEST-$(date +%s)\",
    \"type\": \"faq\",
    \"name\": \"deploy test\",
    \"owner_type\": \"shared\"
  }"

# 3. 验证返回
# 期望: code=0 + data.id（响应外壳是 {code, message, data}，见 internal/pkg/utils/response/response.go:48）
# kb_code 上压着 UNIQUE 索引 uq_knowledge_bases_kb_code，且它是**非 partial** 索引
# （pg_indexes 实测：CREATE UNIQUE INDEX ... USING btree (kb_code)，没有 WHERE deleted_at IS NULL）
# ⇒ 用例名必须带一次性尾巴；而 §7.1 结尾那条 DELETE 是软删（模型带 gorm.DeletedAt），
#   它只把行从列表里摘掉，**腾不出这个 kb_code**：同一个 code 之后再建永远撞唯一索引。
```

> "重复 kb_code 会长什么样"今日在这台实例上留下了可查的证据（本批验收脚本自己造的）：
> `operation_logs` 里那行的 `detail` 是
> `{"error":{"code":"INTERNAL_ERROR_6002","message":"ERROR: duplicate key value violates unique constraint \"uq_knowledge_bases_kb_code\" (SQLSTATE 23505)"},"status_code":500,...}`
> ⇒ 撞唯一约束的返回码是 **500 + INTERNAL_ERROR_6002**，不是 4xx 的"名字已存在"。
> 巡检脚本里如果只看 HTTP 码分档，会把一次纯粹的键名冲突读成服务端故障；
> 想确认是不是这一类，就去 `operation_logs.detail` 里按 `path` 捞（§6.3 的 SQL）。

### 6.2 关键指标从哪儿读（没有外部监控/告警通道）

> 私域部署不接 Prometheus / Grafana / 告警通道（`7a26965a` 那一批把仓内的 dashboard、
> 告警规则与 `KNOWLEDGE_GROUP_MONITORING.md` 一起删了；`user-server/go.mod` 里没有 prometheus 依赖，
> `middleware.MetricsMiddleware()` 虽然有实现，但全仓**没有任何注册点** —— 今日实测 `grep -rn
> "MetricsMiddleware" --include="*.go"` 只命中它自己的定义与注释 ⇒ 它不产出任何可读的 `/metrics`）。
> 所以这里给的是"能真跑出一条数"的两个读数面，不是指标名清单。

| 想看的量 | 唯一真实来源 | 现成的读法 |
|----------|--------------|------------|
| KB 建/删/改了多少次 | `operation_logs`（写方法才记，见 §6.3） | SQL：`detail::jsonb->>'path' LIKE '/api/knowledge-bases%'` |
| 接口耗时（含读侧 `ListByAgent`） | `user-server/logs/user-server.log` 的 `event=api_interaction` 行，字段 `latency_ms` / `status` / `path`（`internal/middleware/api_logger.go:74,98`，注册点 `router.go:209`） | §8.4 的 jq 管道 |
| 越权/拒绝 | 写侧：`operation_logs` 的 `status_code`；读侧：只有 `api_interaction` 的 `status` | §6.3 SQL ②、§8.3 |
| 灰度命中痕迹 | `feature_flag_eval_logs` / `feature_flag_audit_logs` | §3.4 的 SQL（本机这个 key 永远 0 行，原因见那里的实跑说明） |

本机今日实测这两个面都有数（**收尾**读数，见下一段的"为什么会漂"）：`user-server.log` 里
`api_interaction` 行 19,905 条，`operation_logs` 41,966 行（其中走 KB 接口的写请求 105 条，
`detail->>'latency'` 的 POST 档 P99 = 36.66ms）。
注意单位不同名也不同：审计 `detail` 里那个叫 `latency`（毫秒，`audit.go:229`），
日志里那个叫 `latency_ms`（`api_logger.go:98`）—— 两个都不是旧版 §6.2 写的 `audit_logs` 表。

> **这几个绝对数一定会漂，别把它们当断言**：审计是"每个写请求加一行"，而 §7.1 的验收脚本
> 自己就跑一次 POST + 一次 DELETE，本批取证（§7.2 那几轮 + 一次 PUT 反向）把它从 71 条推到 105 条。
> 复跑时**只有形状是可核对的**：三档方法名齐全、KB 侧 403 仍为 0、P99 在同一量级；
> 谁要的是"多少行"这种量，那只能是当次快照，写清日期与前面跑过几遍验收。

### 6.3 关键指标审计 SQL（无外部告警）

```bash
set -a; . ./.env; set +a
export PGPASSWORD="$POSTGRES_PASSWORD"
PSQL="psql -h 127.0.0.1 -p 8232 -U admin -d user_db -tAc"   # 端口口径见 §1.2
```

```sql
-- ① 本特性的写操作计数（建 / 软删）
-- 必须按 detail->>'path' 过滤：module / resource 两列存的是 URL 的第 3 段，
-- /api/knowledge-bases 落成 'unknown'、/api/knowledge-bases/16 落成 '16'
-- （audit.go:312-326 按 parts[3] 取值，且这个形状被 internal/middleware/audit_test.go:164-193 钉住）
-- left(detail,1)='{' 是防脏值：不是 JSON 的行（如登录事件）直接 ::jsonb 会让整条查询报错
SELECT (detail::jsonb->>'method') AS method, count(*) AS n,
       percentile_cont(0.99) WITHIN GROUP (ORDER BY (detail::jsonb->>'latency')::int) AS p99_ms
  FROM operation_logs
 WHERE left(detail,1)='{' AND detail::jsonb->>'path' LIKE '/api/knowledge-bases%'
 GROUP BY 1;
-- 本机实跑（收尾快照，三档合计 105 行）：POST 68 / P99 36.66ms，PUT 15 / 6.72ms，DELETE 22 / 28.11ms
-- 行数是会随验收次数涨的（§6.2 末段），可核的是形状：三档方法名齐全、写侧 P99 在几十毫秒量级

-- ② 写侧被拒（403）的总量与分布 —— 越权巡检就查这个
SELECT detail::jsonb->>'path' AS p, count(*) AS n
  FROM operation_logs
 WHERE left(detail,1)='{' AND (detail::jsonb->>'status_code')='403'
 GROUP BY 1 ORDER BY 2 DESC LIMIT 10;
-- 本机实跑：全库 887 行 403（TOP 10 落在 sop-templates / geo / whatsapp / community 等路径上），
-- knowledge-bases 侧 0 行；887 与 0 今日复跑两次一致（绝对数也会漂，漂的方向是"只增"）
-- ⇒ "0" 是这台实例当前的真实读数，不是"没有这个面"；隔离一旦被绕过，这里会长出行

-- ③ 丢日志降级计数（审计是异步批量落库，channel 满了会丢）
-- 读数在进程侧：middleware.GetAuditDroppedCount()（audit.go:434）/ AuditManager.GetDroppedCount()（:52）
-- ⇒ 它不落库、也没有 /metrics 出口，只能靠日志里的 `[audit]` 前缀。全仓这个前缀有三条产码点
--   （今日实测）：audit.go:97 "丢弃 %d 条审计日志"（channel 满）、:118 "%d 条…重试后仍写入失败"、
--   :138 "同步落库失败（已记入降级计数）"；另有 service/customer.go:423 一条 `[audit][WARN]`
--   走的是 fmt.Printf，不属于这个降级计数——grep 会一起捞到，别看混。
--    grep -n '\[audit\]' user-server/logs/user-server.log
--    grep -c '\[audit\]' user-server/logs/user-server.log   # 今日实测 0：这台实例一次都没丢过
-- ⇒ 注意这是**行数**不是**条数**：:97 一行里带的是 "%d 条"（一次报一批），
--   所以"丢了多少条审计"只能把这些行的数字相加，别拿 grep -c 的数当降级量。
```

> 巡检靠上面的 SQL 与 §7.1 的脚本，没有自动告警：出问题不会有人被叫醒，
> 频率由部署方自己定（建议至少跟一次发布，见 §4.3）。

---

## 7. 部署后验收 (Post-Deploy Acceptance)

### 7.1 自动化验收脚本

脚本在仓内：`scripts/post_deploy_check.sh`（2026-09-22 本批随本文档一并落仓）。
旧版这里写的是"把下面这一整块存成该路径再跑"——那等于让每个部署方手抄 90 行，
而这几步的写法每一格都有一个"看着对、实际吞红"的形态（见下表最后一列），
手抄时抄掉任一格，得到的就是一份会假绿的验收脚本。

在**仓库根目录**执行（`.env`、`user-server/` 这些相对路径按仓库根算）：

```bash
bash scripts/post_deploy_check.sh      # 退出码 0＝五项全过；不过的那格把红因印在屏幕上
```

| 步 | 判据 | 这一格为什么长这样 |
|----|------|--------------------|
| 1 | `GET ${BASE}/health` 必须 200 | `/health` 打 DB+Redis，任一不可用即 503（`health.go:72` 起）⇒ 这一格红是"依赖没起"，不是知识库的事。`BASE` 默认 `http://127.0.0.1:8204`，要改走 env |
| 2 | 两张表都在 | `knowledge_bases` + `agent_kb_bindings` 由启动迁移建（§2.1），判据查 `information_schema.tables` |
| 3 | 索引数 ≥10 | 本机活库实测 12 条（两表各 6，全名单见 §2.4）⇒ 下界留两格余量给版本差，掉到 10 以下才是真缺 |
| 4 | 登录 → 建一条 KB → 删掉 | 三处吞红形态：① 口令链必须与 seed 同序（`bootstrap.sh:63` 是 SEED\_PASSWORD > ADMIN\_PASSWORD > 公开默认值，反排在两个都设过的实例上必 401）；② 旧写法 `curl -sf \| jq` 在口令错时让 curl 退 22、jq 拿空输入，`set -e` 下整条静默死掉，屏幕上只有步骤标题没有红因；③ `kb_code` 上那个唯一索引是**非 partial** 的（§6.1）⇒ 用例名带一次性尾巴，且结尾的软删腾不出这个 code |
| 5 | `go test ./internal/repository ./internal/service ./test/...` | 必须 `cd user-server`；`-timeout 2500s`（service 单包实测 450–880s，旧写法的 120s 永远跑不完）；**不许**在结尾接 `2>&1 \| tail -3` —— 那会把 go test 的退出码换成 tail 的，测试红了脚本照样打印"全部验收通过"；`POSTGRES_TEST_PORT` 的读取点是 `internal/pkg/testutil/testdb.go:68` |

> **第 5 步的预算**：整跑一遍在本机要留 ≥45 分钟，且磁盘得够 —— go build cache 写不下去时
> 会以"编译失败"的形态混进红因里，判红之前先 `df`。
>
> **本批落仓前真跑过一次整步**（2026-09-22，同一条命令 `-p 1 -count=1 -timeout 2500s`）：
> 退出码 **1**，四格里 `internal/repository` 101s / `test/e2e` 7s / `test/integration` 13s 全绿，
> `internal/service` 590s 里唯一一条红是
> `TestOrderDraftSweepWorker_EndToEndFlipsAndPurgesRealRows`（判据读数"累计 expired=0 purged=0
> 轮次=49"）。这条红**与知识库无关、也不是本批引入**：它断言的是"定时节拍把库里到期草稿翻成
> expired / 删掉过保留期的终态行"，同机其它二进制共用 `POSTGRES_TEST_PORT=8232` 那套测试库时就会
> 把它打断 —— 既有归因连同三条证据写在 `docs/architecture/CHANNEL_INTEGRATION_AUDIT_2026-09.md`
> 的"那条红的归因"段：同参数第二次全量跑该包绿 / 隔离复跑 `-count=5` 五次全过 /
> 红的窗口内 `ps` 观测到两个并行会话的测试二进制与本轮共用 `POSTGRES_TEST_PORT=8232`。
> 本批按同口径复跑：隔离腿 `-run TestOrderDraftSweepWorker_EndToEndFlipsAndPurgesRealRows`
> 带 env 单跑 rc=0（5.7s，与全量那次的 5.06s 同量级 ⇒ 不是"根本没跑到"）。
> ⇒ 交付口径：1–4 步实测通过；第 5 步"跑到完"，红的这一格按上述既有归因处理，不记成本批缺陷。

**脚本自身的反向核验**（2026-09-22，验的是"它会不会假绿"）：11 格注码全红且红因逐格对得上 ——
仓库根无 `.env`、API 连不上、PG 口连不上、`/health` 返 503、登录返 401（红因里要带出
`UNAUTHORIZED_2001`）、200 但 `data.token` 为 null、创建返 500（带出 `INTERNAL_ERROR_6002`）、
建成功但响应无 `data.id`、以及 `set -e` 下 `VAR=$(curl 失败)` 确实会走 `||` 分支而不是静默退出；
控制组（真实例 1–4 步）绿。注码脚本与桩 API 在仓外的取证目录，未随本批落仓。

### 7.2 手动验收清单

第 1–6 项都在开发实例上真跑过（2026-09-22，API 8204 / 库 8232 那个 dev 库），"期望"一列写的是
**当日实测读数**而不是设想值；跑的时候按 §1.2 的口径自己 source `.env`。
第 7 项分两档，看下表最后一条的括注（活实例跑的不是本批改后的二进制）。

> 取数前先看这一格：`GET /api/knowledge-bases/by-agent/:aid` 的 `data` 是**裸数组**
> （控制器把 `svc.ListByAgent` 的切片直接交给 `response.Success(ctx, list)`，
> `controller/knowledge_base.go:146-151`）⇒ `jq '.data.list[]'` 会报
> `Cannot index array with string "list"` 且什么都取不到，看着像"列表是空的、隔离生效了"。
> 正确取法：`jq -r '.data[].id'`。这一条是本批自己踩出来的（第一版取证脚本就是这么错的）。

| 项 | 怎么验 | 期望（＝今日实测） | 实际 |
|----|--------|---------------------|------|
| 1 | `curl -s $BASE/health` | 200；任一依赖不可用即 503（`health.go:72`） | ☐ |
| 2 | `POST /api/knowledge-bases`，体带 `owner_type:"private"` + `owner_agent_id:<aid>` | 200 且 `data.id` 非空；**同一事务里**多出一条 `agent_kb_bindings`（`role=primary`、`enabled=true`）—— private 走的是 `CreateWithBinding`（`service/knowledge_base.go:114-122`）。实测：`owner_agent_id=31` + `type=sop` 那次拿到 `id=24`，绑定行 `31/sop/primary/true` | ☐ |
| 3 | 同上，`owner_type:"shared"` 且**不带** `owner_agent_id` | 200 且 `data.id` 非空；不产生绑定行（实测 `id=23` 那次 `count(*) FROM agent_kb_bindings WHERE kb_id=23` = 0） | ☐ |
| 4 | `GET /api/knowledge-bases/by-agent/:aid`（取 `.data[].id`） | 只见**两类**：该 agent 自己的 private、以及**已 bind 给它**的 shared（`repository/knowledge_base.go:133-141` 的 WHERE 是 `enabled = true AND (owner_agent_id = ? OR (owner_type = 'shared' AND id IN (子查询)))`）。实测：agent31 可见集 `[26]`（自己的 private）、agent30 `[ ]`（空），第 3 项那条 unbound shared 谁都没看见 | ☐ |
| 5 | `POST /api/knowledge-bases/:id/bind`，体带 `agent_id` | 200；再跑第 4 项，这条 shared 出现在该 agent 的列表里、且**不会**出现在别人那里（实测把 shared 25 bind 给 31 后：agent31 `[26 25]`、agent30 仍 `[]`）。shared 不是"人人可见"，**没 bind 就查不到**，这点最容易验错。补测 `enabled`：把该 shared 置 `enabled=false` ⇒ 从 agent31 列表消失（**绑定行仍在**，是 KB 那侧的开关把它滤掉的）；改回 `true` 又出现 | ☐ |
| 6 | `DELETE /api/knowledge-bases/:id` | 200；`knowledge_bases` 那行**物理还在**、`deleted_at` 置位（模型带 `gorm.DeletedAt`）；该 kb 的 `agent_kb_bindings` 行**物理消失**（绑定模型没有软删列）⇒ 级联是真的，但两张表的"删"不是一回事。实测 `id=24`：`count(*)=1` / `deleted_at IS NOT NULL = t` / 绑定行 `0` | ☐ |
| 7 | 建档时故意写错入参 | 非法 `type`（如 `product`）⇒ **400** `INVALID_PARAM_1001`，消息 `type 非法: product (faq/rag/sop)`；`name` 为空 ⇒ 400（控制器 binding 先拦，消息带 `Field validation for 'Name' failed`）；`owner_type` 取值非法（如 `team`）⇒ 400。**但组合不合法这两格：改前 500、改后 400**，而这条 400 的读数来自 HTTP 层用例（`internal/controller/knowledge_base_owner_validation_test.go`，见 §8.5），**不是**这台活实例——它跑的是 9/19 起的 `bin/user-server.r39`，本批不重启共享开发实例；今天打它实测两格仍返 `500 INTERNAL_ERROR_6002` | ☐ |

> 第 6 项那句"物理还在"不是学术洁癖：`uq_knowledge_bases_kb_code` 是**非 partial** 唯一索引，
> 软删不掉那行 ⇒ `kb_code` 永久占位（§6.1）。验收库里跑过一遍就该把行清掉，
> 否则第二天别人复跑第 2/3 项会撞一个看起来像代码坏了的 500。
> 上面几轮取证造的 `KB-MC-*` / `KB-MCI-*` / `KB-MCE-*` 行都已软删，并把留下的绑定行清干净
> （收尾读数：`deleted_at IS NULL` 的 KB 6 条、`agent_kb_bindings` 4 条，与取证前一致）。


---

## 8. 常见问题 (Troubleshooting)

### 8.1 表已存在 / 表结构与文档不符

表由启动期 Go 迁移建（v3.15.0，`internal/migration/migrations/v3_15_kb_unification_migration.go`），
而那条迁移写的是 `CREATE TABLE IF NOT EXISTS`（`:46`、`:72`）⇒ **"relation already exists" 从这条迁移里出不来**。
真撞上只有两种情形，别用同一个药方：

1. **库里有人手工建过同名表**（或从别的分支拷过 DDL）：迁移会静默跳过，于是"跑过迁移"与
   "表结构对"不是一回事。核对：
   ```sql
   SELECT version, name, status FROM migration_records WHERE version LIKE 'v3.15%' OR version LIKE 'v3.22%';
   SELECT column_name, data_type, udt_name, is_nullable FROM information_schema.columns
    WHERE table_name='knowledge_bases' ORDER BY ordinal_position;
   ```
   本机实跑：v3.15.0 与 v3.22.1/v3.22.2 都是 `completed`；`type` 是 `kb_type_enum`、
   `owner_type` 是 `kb_owner_type_enum`（枚举值 faq/rag/sop、private/shared），
   `description`/`member_count`/`doc_count`/`enabled` 都 NOT NULL 带默认值。
   对不上就是手工表污染，`DROP TABLE` 后重启让迁移重建（先按 §1.2 备份）。
2. **迁移中途失败**：`migration_records.status` 停在非 completed；同一张表还可能有半套索引。
   先看进程日志里 `[v3.15]` 的那几行（每步都打了 `n/7`），它停在哪一步就是哪一步的 SQL 挂了。

### 8.2 NOT NULL 约束失败

```
ERROR: null value in column "enabled" of relation "knowledge_bases"
```

**成因形状**：GORM 的 `Select("*").Updates(struct)` 会把结构体里的 nil 指针原样写成 NULL，
而 `enabled`/`description` 这些列是 NOT NULL（读数见 §8.1）。

**这一列在 knowledge_bases 上已经不存在这个形状**：仓储层的 Update 用的是显式列清单
`Updates(map[string]any{...})`（`repository/knowledge_base.go:161-174`）——旧版文档把这写成
"解决：应当改用 map"，其实那是既成事实，改的是**状态描述**。

同一形状在同表族的邻居上还在：`repository/agent_kb_binding.go:125`、`sop_template.go:234`、
`faq_entry.go:278` 三处仍是 `Select("*").Updates(x)`。今天没爆是因为调用方总把 `Enabled` 填上
（模型是 `*bool` + `default:true;not null`）。⇒ 改这三张表的写路径时**别顺带传 nil**，
真要动就照 KB 那格改成显式清单。

### 8.3 怀疑越权（读了别人的 KB）

先删掉旧版这句：**"`isolation_violation_total > 0`" 是一个不存在的指标**（仓内零命中，
也没有任何 `/metrics` 出口，见 §6.2）。真实的排查面是这两个：

1. **写侧（POST/PUT/DELETE/PATCH）有落库**：`operation_logs` 的 `detail` 里带 `status_code`，
   403 就是被 `middleware/ownership.go:157-162` 拒掉的。按路径分布数一遍：
   ```sql
   SELECT detail::jsonb->>'path' AS p, count(*) AS n FROM operation_logs
    WHERE left(detail,1)='{' AND (detail::jsonb->>'status_code')='403'
    GROUP BY 1 ORDER BY 2 DESC LIMIT 10;
   ```
   本机今日：全库 887 行 403，`/api/knowledge-bases*` 侧 **0 行**。注意"0"是这台实例的读数，
   不是"没有这个面"——隔离被绕过时这里会长出行。
2. **读侧（GET）没有 403 落库面**：`GET /api/knowledge-bases/by-agent/:aid` 走的是
   `repository/knowledge_base.go:127` 的 `ListByAgent`，越权的表现不是被拒，而是
   **WHERE 少滤了一层**（返回本该看不见的行）⇒ 只能拿同一个 `aid` 的返回集与库里
   `owner_agent_id`/`agent_kb_bindings` 对账，或者跑
   `user-server/test/e2e/agent_kb_e2e_test.go` 里的 `TestE2E_MultiAgent_KnowledgeIsolation`
   （`:72`，它建两个 agent 各一条 private + 一条 shared，断言彼此的可见集）。
   逐请求的观测口子只有 NDJSON 那一条（§8.4）。

> 反过来提醒一次：`operation_logs` 的 `module` / `resource` 两列存的是 URL 第 3 段
> （`audit.go:312-326`），对 KB 这两条路由没有判别力（`/api/knowledge-bases` 存成 `unknown`，
> `/api/knowledge-bases/16` 存成 `16`）⇒ 过滤一律走 `detail::jsonb->>'path'`。

### 8.4 觉得 ListByAgent 慢

旧版这里写的 `kb_list_duration_seconds P99 > 200ms` 同样是个不存在的指标（§6.2）。
现成的读数只有两条路：

1. **分档 P99（写侧）**：`operation_logs` 的 `detail->>'latency'` 只对写请求有值，
   口径与实跑读数见 §6.3 SQL ①。
2. **逐请求（含 GET）**：NDJSON 日志，`latency_ms` 字段：
   ```bash
   # 1) 该路径的样本量与 P50/P99
   grep '"event":"api_interaction"' user-server/logs/user-server.log \
     | grep '"path":"/api/knowledge-bases' > /tmp/kb.ndjson
   wc -l < /tmp/kb.ndjson
   jq -s 'map(.latency_ms) | sort | {n:length, p50:.[length/2|floor], p99:.[length*0.99|floor]}' /tmp/kb.ndjson
   # 2) 慢的那些落在哪个方法上
   jq -r 'select(.latency_ms>100) | [.method,.path,.latency_ms] | @tsv' /tmp/kb.ndjson | sort | uniq -c | sort -rn | head
   ```
   本机实跑（同一份 /tmp/kb.ndjson）：样本 94 行 ⇒ `{n:94, p50:5, p99:78}`，
   min 0 / max 78 ⇒ **第 2) 条命令输出是空的**（这个实例上没有一条 >100ms 的 KB 请求），
   空输出是"没有慢请求"，不是命令坏了。方法分布上 GET 与写方法都在（`GET /api/knowledge-bases/by-agent/31` 5 行），
   所以这条口子确实覆盖读侧——这是它与 §6.3 那条 SQL 的唯一分工差。
   > 窗口口径：这个文件**没有轮转机制**（`grep -rn lumberjack user-server/` = 0 命中，仓内也没有
   > logrotate 配置），它是 `scripts/deploy-user.sh:213` 用 `> logs/user-server.log` 重定向起来的
   > ⇒ **每次重启清空一次**。本机今日这份的第一行时间是 `2026-09-19T04:45`（＝上一次重启），
   > 共 98,928 行。所以这里的 P99 是"自上次发布以来"的量，别拿它当长窗口基线；
   > 要长窗口得自己加 logrotate，或按 §1.2 的备份口径把文件抄出去再切窗口。

真慢再往下：`EXPLAIN (ANALYZE, BUFFERS)` 那条 `owner_agent_id = ? OR (owner_type='shared' AND id IN (SELECT ...))`
（形状见 §8.3 第 2 条），并核这两个索引在不在：
`idx_kb_owner_agent`、`uq_agent_kb_bindings`（覆盖 `(agent_id, kb_id)`，实测名单见 §2.4 的 12 条）。
注意这个 WHERE 里 `OR` + 子查询的组合，PG 经常选择对 `knowledge_bases` 走全表扫 ⇒
`enabled=true` 的过滤是否走上 `idx_kb_enabled` 也要在计划里看，不要只看索引存在。

### 8.5 建档 / 改档入参错返 500 而不是 4xx

不是配置问题，是错误没包成"入参错"。`response.ErrorFromDB`（`internal/pkg/utils/response/response.go:140` 起）
分两档判：先 `errors.Is`（`ErrInvalidInput`/`ErrUnauthorized`/`ErrForbidden`），再拿**错误消息的子串**
去一张写死的名单里比（`response.go:169-181`：`invalid input`、`非法`、`参数`、`不能为空`、`缺失`、
`required`、`validation`、`取值`、`格式`、`校验`、`不符合`…）。两边都不命中才掉进兜底 500
`INTERNAL_ERROR_6002`。

KB 这条链上今日实测的落点（活实例＝改前的 `bin/user-server.r39`，所以它反映的是**改前**行为）：

| 入参 | 消息 | 改前 HTTP | 为什么 |
|------|------|-----------|--------|
| `type:"product"` | `type 非法: product (faq/rag/sop)` | 400 | 撞名单里的 `非法` |
| `name:""` | 控制器 binding 先拦（`Field validation for 'Name' failed`） | 400 | 压根没进 service |
| `owner_type:"team"`（POST） | `owner_type 非法: team (private/shared)` | 400 | 同样撞 `非法` |
| `owner_type:"private"` 且不给 `owner_agent_id` | `owner_type=private 时 owner_agent_id 必填` | **500** | `必填` 不在名单里（名单里是 `不能为空`，不是它） |
| `owner_type:"shared"` 却带 `owner_agent_id` | `owner_type=shared 时 owner_agent_id 必为空` | **500** | 同上，`必为空` 也不在名单里 |

⇒ 真正需要修的是**最后两格**，而它们在 POST 与 PUT 上是**两份各自独立**的校验代码
（`service/knowledge_base.go` 里 CreateKB 与 UpdateKB 各写了一遍，措辞一模一样）。
本批（2026-09-22）两族四格都包上了 `utils.ErrInvalidInput`，并在 HTTP 层钉了用例：
`internal/controller/knowledge_base_owner_validation_test.go` —— POST 三格 400 + PUT 两格 400，
两个入口各一格合法 200 作控制组。反向测过两次，都是"注码那一侧红、另一侧仍绿"：
把 CreateKB 的包装退回裸 `errors.New` 后 POST 用例红、红因正是"实际 500"；
把 UpdateKB 的两格退回裸 `errors.New`（消息与 CreateKB 完全同字，注码要按函数体切，
整文件 replace 会打错位置——本批第一次注码就是这么打偏成假绿的）后 PUT 用例两格红、Create 用例仍绿。
改完两次都用 `cp` 备份 + md5 比回原值，再整组跑绿。

PUT 上还有一个**没有对称缺陷**但值得知道的形状：`UpdateKB` 的 `switch ot` 没有 `default` 分支
（`service/knowledge_base.go:265-279`）⇒ 非法 `owner_type` 不在 service 层拒，而是走到 PG 的
`kb_owner_type_enum` 上被拦，返回 `400 INVALID_PARAM_1001` + 消息
`ERROR: invalid input value for enum kb_owner_type_enum: "team" (SQLSTATE 22P02)`
（今日活实例实测，且那一行的 `owner_type` **仍是原值**，没写脏）。
换 SQLite 之类的后端就没有这道拦了 ⇒ 别把"能改出来"当成"入参校验过"。

**同一族的另一格没改**：`kb_code` 撞唯一约束仍是 500（§6.1）。那是数据库错误到 HTTP 码的
映射缺失（缺 23505 → 409/400 这一档），改它要动 `ErrorFromDB` 这个所有模块共用的分档函数
⇒ 已按缺陷移交，不在本批动。

---

## 9. 相关文档

| 文档 | 状态 |
|------|------|
| `docs/architecture/adr/ADR-014-knowledge-group-isolation.md` | 在 |
| `docs/operations/KNOWLEDGE_GROUP_API.md` | 在 |
| `docs/standards/MASTER_RULES.md` | 在 |
| `docs/architecture/KNOWLEDGE_GROUP_DESIGN.md` | **已删**（`2a990cd1`，2026-08-16）——旧版这条是死链 |
| `docs/operations/KNOWLEDGE_GROUP_MONITORING.md` | **已删**（`7a26965a`，2026-07-31）——监控口径以本文 §6 为准，也别再去找它 |

---

**最后更新**: 2026-09-22（离线部署批：§1/§2/§4/§5/§6/§7 按本机实跑读数重写，
§6/§9 里那两个不存在的指标名清除，验收脚本 `scripts/post_deploy_check.sh` 落仓）  
**作者**: HiveMTK SRE 团队
