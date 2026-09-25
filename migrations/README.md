# 数据库迁移执行顺序

> 适用范围：hivemtk 用户端 PostgreSQL 数据库（`user_db` / `mtk-postgres` 容器）

## 总览

本目录包含用户端的所有 SQL 迁移文件，分两类：

| 类型 | 文件 | 说明 |
|------|------|------|
| **基础初始化** | `init-user-db.sql` | 启用扩展（pgvector / uuid-ossp）+ 核心表结构（知识库 / RAG 产品） |
| **增量迁移** | `002_*.sql` ~ `033_*.sql` | 按版本顺序应用的功能迁移 |

## 执行顺序

```text
 1. init-user-db.sql                       ← 基础结构（首次部署必跑）
 2. 002_ai_content.sql                     ← AI 内容生成
 3. 003_unified_message.sql                ← 统一消息
 4. 004_customer_session.sql               ← 客户会话
 5. 005_rfm_user_segment.sql               ← RFM 用户分群
 6. 006_custom_reports.sql                 ← 自定义报表
 7. 007_integration.sql                    ← 集成
 8. 008_ab_test.sql                        ← A/B 测试
 9. 009_churn_prediction.sql               ← 客户流失预测
10. 010_satisfaction_surveys.sql           ← 满意度调研
11. 011_ai_sales_champion.sql              ← AI 销冠
12. 012_rag_enhancement.sql                ← RAG 增强
13. 013_version_offline_fields.sql         ← 版本/离线字段
14. 014_site_contact_config.sql            ← 站点联系配置
15. 015_init_flow_enhancement.sql          ← 初始化流程增强
16. 016_merchants_key_length.sql           ← 商户密钥长度
17. 017_customer_service_enhancements.sql  ← 客服增强
18. 018_cde_p1_gap_fixes.sql               ← C/D/E 域 P1 缺口修复
    -- 019-023 历史空号（已合并入 018 或回退），跳过
19. 024_asset_market.sql                   ← 资产市场（用户端 3 张表）
20. 025_unify_system_users.sql             ← 统一 system_users（DROP team_users）
21. 026_customer_rfm_index.sql             ← customer_rfm 索引
22. 027_user_blacklist.sql                 ← 用户拉黑（TTL + 软删除）
    -- 028 历史空号（已合并入 027），跳过
23. 029_livecode_click_log.sql             ← 活码点击日志
24. 030_asset_market_seed.sql              ← 资产市场种子数据
25. 031_platform_cs_rag_seed.sql           ← 平台客服 RAG 种子
26. 032_industry_assets_local_seed.sql     ← 行业资产本地种子
27. 033_industry_ai_agents_seed.sql        ← 行业 AI 智能体种子
    -- 034-056 为后续功能/索引迁移（bridge_metrics、enum、soft_delete 等），编号连续，此处不逐一列举
28. 057_api_logs_encrypt_fields.sql        ← OPT-SEC-04 api_logs ip/user_agent 扩列（配合应用层加密）
29. 058_operation_logs_encrypt_fields.sql  ← OPT-SEC-04 operation_logs ip/user_agent 扩列 text
```

## 关键约束

- **顺序性**：必须按上述编号顺序执行；后续迁移可能引用前序表的字段、索引、约束。
- **幂等性**：所有迁移文件均使用 `CREATE TABLE IF NOT EXISTS` / `ADD COLUMN IF NOT EXISTS` / `CREATE INDEX IF NOT EXISTS` 等幂等语法，可安全重跑。
- **`init-user-db.sql` 与 002-033 的关系**：
  - `init-user-db.sql` 创建**核心基础设施表**（pgvector 扩展、知识库向量表、RAG 产品表等），是其他迁移的前置。
  - `002-033` 创建**业务功能表**，必须在 `init-user-db.sql` 之后执行。

## Docker / 自动执行路径

`docker-compose.yml` 中 `mtk-postgres` 容器的 `initdb.d` 机制：

- 仅 `init-user-db.sql` 通过 `docker-entrypoint-initdb.d/` 自动执行（容器首次创建时）。
- `002-033` **没有自动执行路径**：`user-server` 启动时跑的是 `user-server/internal/pkg/db/migrate.go`
  （GORM AutoMigrate 建表 + `internal/migration/migrations/*.go` 注册的 Go 迁移任务），它从不读 `.sql` 文件。
- 因此本目录的 `.sql` 只有三种落地方式：① 容器首次初始化时的 `initdb.d`（只挂了 `init-user-db.sql`）；
  ② `scripts/bootstrap.sh` 里点名执行的两份（`027_user_blacklist`、`028_customer_tags_uuid`）；
  ③ 人工 `psql -v ON_ERROR_STOP=1 -f migrations/<NNN>_xxx.sql`。
- 升级时不需要"重跑迁移"，因为服务重启只会重跑 Go 侧迁移任务并按表/列存在性跳过；`.sql` 要人工按需执行。

## 平台端迁移

平台端（独立仓库 `hivemtk-platform`）的数据库迁移**不在本目录**。  
平台端使用 `platform_db` 独立数据库，建表与迁移在 `platform-server/internal/utils/db/db.go`
（GORM AutoMigrate）及该仓库自己的 `migrations/` 里；本仓库既不放也不代跑平台端迁移。

## 故障排查

| 现象 | 可能原因 | 修复方法 |
|------|----------|----------|
| `relation "xxx" does not exist` | 迁移未按顺序执行 | 按 `002 → 033` 顺序重跑 |
| `column "xxx" already exists` | 迁移被部分执行后中断 | 使用 `IF NOT EXISTS` 重新执行（已支持幂等） |
| `extension "vector" is not available` | pgvector 镜像未启用 | 确认使用 `pgvector/pgvector:pg15` 镜像 |
| `permission denied for extension vector` | 数据库非 superuser | 切换 `admin` / `postgres` 角色建库后，普通用户使用 |

## 开源协议

本目录文件以 **AGPL-3.0** 发布，详见 [../LICENSE](../LICENSE) 与 [../NOTICE](../NOTICE)。
