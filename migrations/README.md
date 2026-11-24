# 数据库迁移执行顺序

> 适用范围：hivemtk-user 用户端 PostgreSQL 数据库（`user_db` / `mtk-postgres` 容器）

## 总览（2026-11-25 快照）

本目录包含用户端初始化 SQL 与离线/人工增量 SQL；**运行期 schema 事实源是 Go 侧的
GORM AutoMigrate + 已注册迁移，不是本目录的 `.sql` 文件**。

| 类型 | 文件 | 说明 |
|------|------|------|
| **基础初始化** | `init-user-db.sql` | 启用扩展（pgvector / uuid-ossp）+ 核心表结构（知识库 / RAG 产品）；容器首次初始化时自动执行 |
| **离线/人工增量 SQL** | 93 份编号 SQL（`002`–`098`，含 `034b`） | 按文件名编号顺序应用的离线对照/人工迁移；`019`–`023` 无文件，`028` 有文件 |
| **运行期 schema 事实源** | `user-server/internal/pkg/db/migrate.go` + `user-server/internal/migration/migrations/` | AutoMigrate 建表/补列；147 个已注册 Go 迁移，最大版本 `v3.113.0`（2026-11-25 实算） |

本目录共 94 份 `.sql`（1 份初始化 + 93 份编号增量）。`097_merged_into_id_widen_varchar64.sql`
与 `098_customer_identity_qq_personal_wx.sql` 目前只有离线 SQL，**尚未进入 Go 注册表**；
不能因为服务重启就假设这两份已执行。

## 执行顺序

SQL 仅供离线部署对照或人工按需执行，不替代运行期 Go schema 链：

1. 全新库先执行 `init-user-db.sql`。
2. 再按文件名编号顺序执行编号 SQL；`034b` 紧随 `034`，`019`–`023` 跳过，`028` 不跳过。
3. 执行前逐份阅读文件并确认目标库状态；不要仅凭编号把所有 SQL 无脑重放。

列出当前顺序（在产品仓根执行）：

```bash
find migrations -maxdepth 1 -type f -name '[0-9][0-9][0-9]*_*.sql' -print | LC_ALL=C sort
```

## 关键约束

- **顺序性**：离线 SQL 按编号顺序应用；后续文件可能引用前序表/字段。Go 迁移顺序由注册表与版本号决定。
- **幂等性**：以每个文件的实际 SQL 为准，不假设所有历史文件都可安全重跑；执行前备份，并使用 `psql -v ON_ERROR_STOP=1`。
- **`init-user-db.sql` 与编号 SQL 的关系**：
  - `init-user-db.sql` 创建**核心基础设施表**（pgvector 扩展、知识库向量表、RAG 产品表等），是其他 SQL 的前置。
  - 编号 SQL 记录**历史业务功能迁移**，离线执行时必须排在 `init-user-db.sql` 之后；当前 schema 仍以 Go 运行期链为准。

## Docker / 自动执行路径

`docker-compose.yml` 中 `mtk-postgres` 容器的 `initdb.d` 机制：

- 仅 `init-user-db.sql` 通过 `docker-entrypoint-initdb.d/` 自动执行（容器首次创建时）。
- 编号 SQL **没有自动执行路径**：`user-server` 启动时跑的是 `user-server/internal/pkg/db/migrate.go`
  （GORM AutoMigrate）及 `user-server/internal/migration/migrations/initial_schema.go` 的
  `RegisterMigrations` 所注册的 Go 迁移任务；它从不读本目录的 `.sql` 文件。
- 因此本目录 SQL 只有三种落地方式：① 容器首次初始化时的 `initdb.d`（只挂了 `init-user-db.sql`）；
  ② `scripts/bootstrap.sh` 里点名执行的两份（`027_user_blacklist`、`028_customer_tags_uuid`）；
  ③ 人工 `psql -v ON_ERROR_STOP=1 -f migrations/<NNN>_xxx.sql`。
- 运行期升级由 AutoMigrate 与 Go 迁移账本决定待执行项；`.sql` 不会随服务重启自动执行。
  `097`/`098` 等未注册 SQL 若目标库需要，必须显式人工执行并单独留证。

## 刷新计数基线

以下命令在产品仓根执行，复算本文的数量口径（编号 SQL 文件数、Go 注册调用数、最大 Go 版本）：

```bash
python3 - <<'PY'
import re
import pathlib

sql = sorted(p for p in pathlib.Path("migrations").glob("*.sql")
             if re.match(r"^\d{3}[a-z]?_.*\.sql$", p.name))
registry = pathlib.Path("user-server/internal/migration/migrations/initial_schema.go").read_text()
registered = re.findall(r"^\s*register\((New[A-Za-z0-9_]+)\(db\)\)", registry, re.M)
versions = []
for p in pathlib.Path("user-server/internal/migration/migrations").glob("*.go"):
    if p.name.endswith("_test.go"):
        continue
    versions += re.findall(r'func\s*\([^)]*\)\s*Version\(\)\s*string\s*\{\s*return\s*"([^"]+)"', p.read_text())
max_version = max(versions, key=lambda v: tuple(int(x) for x in re.findall(r"\d+", v)))
print(f"numbered_sql={len(sql)} first={sql[0].name} last={sql[-1].name}")
print(f"registered_go_migrations={len(registered)} max_version={max_version}")
PY
```

## 平台端迁移

平台端（独立仓库 `hivemtk-platform`）的数据库迁移**不在本目录**。  
平台端使用 `platform_db` 独立数据库，建表与迁移在 `platform-server/internal/utils/db/db.go`
（GORM AutoMigrate）及该仓库自己的 `migrations/` 里；本仓库既不放也不代跑平台端迁移。

## 故障排查

| 现象 | 可能原因 | 修复方法 |
|------|----------|----------|
| `relation "xxx" does not exist` | 迁移未按顺序执行 | 按当前离线文件清单核对目标库；不要使用已过时的旧编号范围 |
| `column "xxx" already exists` | 迁移被部分执行后中断 | 使用 `IF NOT EXISTS` 重新执行（已支持幂等） |
| `extension "vector" is not available` | pgvector 镜像未启用 | 确认使用 `pgvector/pgvector:pg15` 镜像 |
| `permission denied for extension vector` | 数据库非 superuser | 切换 `admin` / `postgres` 角色建库后，普通用户使用 |

## 参考残留（死表）说明（A-16/B-清1 审计标注，2026-11-08）

> **本目录 SQL 不是 schema 事实源。** 生产 schema 由 `user-server/internal/pkg/db/migrate.go` 的
> AutoMigrate 单一事实源生成（missingTables 终局校验兜底）。执行顺序表仅供离线部署对照。

以下 22 张表在 SQL 文件中定义，但**全仓 Go 代码零引用**（既无对应 model/TableName，
也无原生 SQL 访问），属历史设计残留——新部署可安全跳过它们；已部署环境若已建出，
留着无调用方、无风险，可自行 DROP：

ai_sales_logs、assignment_rules、companies、email_lists、email_smtps、
feature_flag_evaluations、knowledge_embeddings（被 knowledge_chunks 取代）、
moments_posts、obs_configs、personal_wechat_accounts、rag_safety_audit_logs、
satisfaction_followups、satisfaction_survey_templates、satisfaction_surveys（被 csat_surveys 取代）、
script_recommends、site_contact_config、user_role、role_definition、role_permission、
role_data_scope（角色已收进 system_users.role 列）

注：rag_sessions / rag_messages 在 SQL 有定义、Go 亦有同名 model（AutoMigrate 管理），
不算死表——列入本说明只为澄清 knowledge_embeddings 的取代关系。

## 开源协议

本目录文件以 **AGPL-3.0** 发布，详见 [../LICENSE](../LICENSE) 与 [../NOTICE](../NOTICE)。
