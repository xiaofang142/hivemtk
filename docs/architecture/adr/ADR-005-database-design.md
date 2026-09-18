# ADR-005: 数据库设计（关系建模 + 索引策略）

- **状态**：✅ Accepted（主规范见 [DATABASE_SCHEMA_DEEP_DIVE.md](../DATABASE_SCHEMA_DEEP_DIVE.md)）
- **范围**：所有后端服务的数据建模
- **原始编号**：DOC-DB-001

## 背景

项目早期多个模块在 gorm.Model 基础上各自分散添加字段，导致：

- 公共字段（created_by、deleted_at）未统一
- 软删除缺失
- 索引策略不一致，性能问题在数据量增长后集中爆发
- 字段命名歧义（如 `name` 与 `title`、`desc` 与 `description`）

## 决策

**主规范见 [DATABASE_SCHEMA_DEEP_DIVE.md](../DATABASE_SCHEMA_DEEP_DIVE.md)**，包含以下统一约定：

1. **基类嵌入**：所有表统一嵌入 `BaseModel`，提供 id/created_at/updated_at/deleted_at
2. **单租户**：项目为本地/私域单租户部署，无 `merchant_id` 字段（依据见主规范 [DATABASE_SCHEMA_DEEP_DIVE.md](../DATABASE_SCHEMA_DEEP_DIVE.md) §3.1/§3.2；旧引用 ADR-003 已作废且编号不复用）
3. **软删除**：使用 gorm 的 `gorm.DeletedAt`，禁止硬删除（除审计日志）
4. **字段命名**：
   - 时间：`xxx_at`（如 `created_at`、`published_at`）
   - 布尔：`is_xxx` / `has_xxx` / `can_xxx`
   - 外键：`xxx_id`（如 `customer_id`）
   - 关联表：复数（如 `customers`）
5. **索引规范**：
   - 单列索引仅在 `status` / `created_at` 这类高频过滤字段
   - 复合索引遵循最左前缀，列顺序按选择性从高到低
   - JSONB 字段必须建 GIN 索引
6. **审计字段**：`created_by` / `updated_by` 必填，记录到 `users.id`

## 后果

### 正面

- 跨服务查询时表结构一致
- 软删除由 DB 层兜底（即使应用层 bug 也不会物理丢失数据）
- 索引策略统一，DBA 审阅可批量化

### 负面

- 老表迁移需要 `ALTER TABLE`，需要 downtime 窗口

## 落地

- `DATABASE_SCHEMA_DEEP_DIVE.md` 第 2~4 章
- `hivemtk/migrations/` 全部 SQL 遵循上述规范
- CI 中 `check-db-conventions.sh` 静态扫描（命名/索引/字段）

## 关联

- ADR-001：五层架构（数据流约束）
- ADR-009：错误处理（DB 错误码映射）
- ADR-011：聊天 widget 嵌入（外部 ID 关联策略）
