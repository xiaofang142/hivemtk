# 多租户 / RBAC / SaaS 化分期实施方案

> 状态：方案待审批。审批后按阶段实施，每阶段独立可交付、可回滚。
> 现状基线（2026-10-10）：系统为单租户共享库，无租户概念；注册用户角色=user，
> 看到的是同一份客户/会话/线索数据——这不是越权 Bug，是架构现状。

## 现状盘点（决定改造量的三个事实）

1. **无租户模型**：`system_users` 无 team_id；客户/会话/线索/订单等业务表无租户列。
2. **角色只有全局轴**：`system_users.role` ∈ {admin, manager, staff, customer_service, user}，全部是"这一个部署实例"内的角色。
3. **数据访问层收敛良好（有利条件）**：核心业务读写集中在 repository/service 层，且有 scope 包（`internal/repository/scope`）先例——租户过滤有统一注入口，不需要改几百处 SQL。

## 分期方案

### P0 数据隔离地基（1 周量级）
- 新表 `teams`（id/name/owner_user_id/status/plan）；`system_users` 加 `team_id`（NULL=平台侧）。
- 新角色 `team_admin`（团队管理员）：仅能管本团队用户与配置。
- **租户上下文注入**：JWT 携带 team_id；`scope` 包新增 `WithTeam(ctx)`，核心业务表
  （customers / conversations / message_hub / clues / opportunities / kb 文档 / 触达记录）
  统一追加 `team_id = current` 过滤与写入回填。
- **隔离红线**：未带 team_id 的查询在 P0 阶段逐表清零（CI 加守卫：新增裸查 customers 等表必须带 scope）。
- 超级管理员（platform admin）走 `team_id IS NULL` 直通，可查改全量（满足需求 3）。
- 存量数据归入 `默认团队`（迁移一次性回填 team_id）。

### P1 RBAC 权限模型（1 周量级）
- 表：`roles`（可配置角色）、`permissions`（资源.动作，如 customer.read / reach.send / kb.write）、
  `role_permissions`、`user_roles`（一用户可多角色，替代单 role 字段）。
- 内置角色：超级管理员（全量+跨团队）、团队管理员（本团队用户/配置/全部业务数据）、
  成员（按授权读写）、只读审计。
- 中间件：`RequirePermission("customer.read")` 替换现有 `RequireAdminMiddleware` 的粗粒度判断；
  现有 admin/* 路由逐一映射到权限点（保持默认行为不回退）。
- 前端：菜单与按钮按权限点显隐（复用现有 requiresAuth 机制扩展为 requiresPermission）。

### P2 SaaS 化（2 周量级）
- 注册流程升级：注册即创建团队（注册人=该团队管理员，满足"每个团队有自己的管理员"）；
  支持邀请码/审批两种准入模式（复用 enable_register 开关 + 新增 register_mode 配置）。
- 租户生命周期：启用/停用/配额（团队级用户数上限、消息量配额，超配限流）。
- 品牌化：团队级站点名/Logo 覆盖全局默认（复用 system_config 加 team 维度）。
- 计费预留：teams.plan 字段 + 用量计量表（先记录不强推支付）。

### 明确不做 / 后议
- 数据库分库分表（P0 的行级隔离已满足中小规模；规模上来再议）。
- SQLite（维持不建议结论：RAG 依赖 pgvector）。

## 风险与回滚
- 每阶段独立分支+独立部署验证；P0 迁移含 team_id 回填，回滚=迁移降级脚本。
- 隔离守卫 CI 化（防回归），与现有 date-bucket/arch 门同规格。
