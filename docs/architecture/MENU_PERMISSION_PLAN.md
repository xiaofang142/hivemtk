# 菜单与权限设计 (MENU PERMISSION PLAN)

> **源文件**: `user-server/internal/router/*.go` 全部路由文件 · `frontend_aliases.go` · `model/role.go`
> **适用范围**: `hivemtk` 管理端 / 客服端菜单 + 权限矩阵
> **前置**: [USER_SYSTEM.md](./USER_SYSTEM.md) 角色定义

---

## 1. 权限分层模型

```
┌─────────────────────────────────────────────────────────┐
│                     AdminAuthMiddleware                  │
│              (仅 role == "admin" 放行)                   │
├─────────────────────────────────────────────────────────┤
│                                                         │
│    staff / customer_service  →  普通 auth.Group        │
│    (登录即可访问，写操作通过业务层二次校验)               │
│                                                         │
├─────────────────────────────────────────────────────────┤
│                 PermissionMiddleware                     │
│    细粒度 "module:action" 检查（委托 PermChecker）      │
└─────────────────────────────────────────────────────────┘
```

---

## 2. 管理端菜单 → 权限矩阵

### 2.1 系统管理（仅 admin）

| 菜单 | 后端路由 | 中间件 | 代码证据 |
|------|---------|--------|---------|
| 用户管理 | `/api/system/users` | RequireAdminMiddleware | `system_user_routes.go:14` |
| 角色管理 | `/api/system/roles` | RequireAdminMiddleware | `system_user_routes.go:29` |
| 权限管理 | `/api/system/permissions` | RequireAdminMiddleware | `system_user_routes.go:42` |
| 应用配置 | 全局应用设置写操作 | AdminAuthMiddleware | `service_routes.go:141` |
| AI 工具管理 | `/api/ai-tools` | AdminAuthMiddleware | `system_routes.go:226` |
| 自动化规则 | `/api/recovery-queue` 等 | AdminAuthMiddleware | `system_routes.go` + `frontend_aliases.go:553` |

### 2.2 渠道与消息（admin + cs）

| 菜单 | 后端路由 | admin | cs | staff | 中间件 |
|------|---------|:---:|:---:|:---:|--------|
| 微信配置 | `/api/wecom` | ✅ | ❌ | ❌ | AdminAuthMiddleware |
| WhatsApp | `/api/whatsapp` | ✅ | ❌ | ❌ | AdminAuthMiddleware |
| 企微渠道总览 | 渠道管理写操作 | ✅ | ❌ | ❌ | AdminAuthMiddleware |
| 客服会话 | `/api/chat/*` 读 | ✅ | ✅ | ❌ | 普通 auth |
| 会话标签 | `/api/session-tags` | ✅ 写 | ✅ 读 | ❌ | AdminAuthMiddleware 保护写 |

### 2.3 LLM / 知识库（admin 写 + cs 读）

| 菜单 | 后端路由 | admin | cs | staff | 中间件 |
|------|---------|:---:|:---:|:---:|--------|
| LLM 模型配置 | `/api/llm` | ✅ | ❌ | ❌ | AdminAuthMiddleware |
| Prompts | `/api/prompts` | ✅ 写 | ✅ 读 | ❌ | AdminAuthMiddleware 保护写 |
| SOP 模板 | `/api/sop` | ✅ 写 | ✅ 读 | ❌ | AdminAuthMiddleware 保护写 |
| 快捷回复 | `/api/quick-replies` | ✅ 写 | ✅ 读 | ❌ | AdminAuthMiddleware 保护写 |
| 知识库 API | `/api/knowledge/*` | ✅ | ✅ | ❌ | 业务层 `owner_agent_id` 隔离 |

### 2.4 营销 / 渠道推广（admin + staff 读）

| 菜单 | 后端路由 | admin | cs | staff | 中间件 |
|------|---------|:---:|:---:|:---:|--------|
| 活码 (短链) | `/api/short-links` 写 | ✅ | ❌ | ❌ | AdminAuthMiddleware |
| 域名池 | `/api/domainpool` | ✅ | ❌ | ❌ | AdminAuthMiddleware |
| 自动化营销 | `/api/marketing-flows` | ✅ | ❌ | ❌ | AdminAuthMiddleware |
| 实验管理 | `/api/ab-experiments` | ✅ | ❌ | ❌ | AdminAuthMiddleware |
| 竞品情报 | `/api/competitor` | ✅ | ❌ | ❌ | AdminAuthMiddleware |

### 2.5 运维 / 系统级（仅 admin）

| 菜单 | 后端路由 | 中间件 | 代码证据 |
|------|---------|--------|---------|
| 邮件 SMTP | `/api/email/smtp` | AdminAuthMiddleware（写） | `frontend_aliases.go:292` |
| SMS 网关 | `/api/sms/*` | AdminAuthMiddleware（全部） | `frontend_aliases.go:300` |
| 对象存储配置 | `/api/obs/config` | AdminAuthMiddleware（写） | `frontend_aliases.go:480` |
| CSAT 模板 | `/api/csat/template` PUT | AdminAuthMiddleware | `frontend_aliases.go:504` |
| DNC 全局退订 | 全部 | AdminAuthMiddleware | `frontend_aliases.go:541` |
| 外部 API Key 绑定 | AI 工具端点 | AdminAuthMiddleware | `system_routes.go:212` |
| Tool Debug | `/api/tool-debug` | AdminAuthMiddleware | `tool_debug_routes.go:37` |
| 配置参数 | `/api/config-params` | AdminAuthMiddleware（写） | `config_param_routes.go` |

### 2.6 内容 / 运营（三档均可，按 scope 隔离）

| 菜单 | 路由 | admin | cs | staff | 隔离方式 |
|------|------|:---:|:---:|:---:|---------|
| 线索 | `/api/leads/*` | 全量 | 自己 agent | 自己 agent | `scope/tenant.go` |
| 客户 | `/api/customers/*` | 全量 | 自己 agent | 自己 agent | `scope/tenant.go` |
| 画像 | `/api/user-segment/rfm` | 全量 | 读 | 读 | 业务层过滤 |
| 社群 | `/api/community` | admin 角色 | member | member | 社群内 role |

---

## 3. 前端菜单生成规则

前端 `platform-web` 登录后拉 `/api/system/users/me`（或权限接口），返回当前 user 的 role + permission list，再按以下规则动态渲染：

```go
// 伪代码 —— 前端渲染条件
switch role {
case "admin":
    showAllMenus()
case "customer_service":
    hide(["系统管理", "AI 工具", "LLM 配置", "域名池"])
case "staff":
    hide(["系统管理", "AI 工具", "LLM 配置", "域名池", "短链", "自动化营销"])
}
```

---

## 4. 别名路由的权限加固（R51+ 引入）

`frontend_aliases.go` 把前端习惯用的 `/reach/pipelines` `/domain-pool` 等路径注册为**别名路由**，但这些路径**默认 staff 也能写**。故对 15+ 关键别名路由加了二次 AdminAuthMiddleware：

| 别名路由 | 保护原因 | 代码位置 |
|---------|---------|---------|
| `/reach/pipelines` 写 | 防 staff 绕过主路由改 LLM 配置 | L203 |
| `/email/smtp` 写 | 防 staff 改 SMTP 凭据劫持邮件 | L292 |
| `/sms/*` 全部 | 防 staff 滥发短信 / 改网关 | L300 |
| `/short-links` 写 | 防 staff 改 target_url → 钓鱼 | L382 |
| `/obs/config` 写 | 防 staff 改 OBS 配置泄漏数据 | L480 |
| `/csat/template` PUT | 防 staff 改 CSAT 问卷模板 | L504 |
| DNC 全局退订 | 合规核心，防 staff 误删黑名单 | L541 |
| 自动化规则引擎 CRUD | 防 staff 误触发自动化 / 绕过熔断 | L553 |
| `/domain-pool` 写 | 域名池是公司级基础设施 | L593 |

**规则**：别名路由如果业务上需要 admin-only，必须在 `doRegAdmin` 注册，不能用普通 `doReg`。

---

## 5. 新增路由的权限清单 Checklist

开发新业务路由时：

> 说明：以下为**新增路由时逐次自查的检查单**，非待完成的开发任务，故常态保持未勾选。

- [ ] 业务 scope 是全量还是 owner_agent_id 隔离？
- [ ] 写操作是否需要 admin-only？
- [ ] 别名路由路径（如果有）是否需要 `doRegAdmin`？
- [ ] 测试模式下是否需要中间件短路？（`middleware.IsTestMode && testing.Testing()`）
- [ ] Repository 层是否用 `scope.WithTenantScope(ctx, role, agentID)`？

---

## 6. 相关文件索引

| 文件 | 职责 |
|------|------|
| `user-server/internal/model/role.go` | 角色常量 + 归一化 |
| `user-server/internal/router/system_user_routes.go` | `/system/users` 等权限路由 |
| `user-server/internal/router/frontend_aliases.go` | 别名路由 15+ admin-only 保护点 |
| `user-server/internal/router/system_routes.go` | AI 工具 / 运维 admin-only |
| `user-server/internal/router/service_routes.go` | 快捷回复 / 会话标签 admin-only |
| `user-server/internal/middleware/permission.go` | PermissionMiddleware + PermChecker 接口 |
| `user-server/internal/middleware/require_admin.go` | RequireAdminMiddleware |
| `user-server/internal/repository/scope/tenant.go` | tenant scope 角色隔离 |
| `docs/architecture/USER_SYSTEM.md` | 用户体系（角色 / 表结构 / Token / 安全机制） |
