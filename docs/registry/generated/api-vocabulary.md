# 平台词汇表与权限矩阵（生成于 registry.yaml，勿手改）

## 词汇表

| 词汇 | 中文 | 说明 |
|---|---|---|
| Team / Tenant | 团队（租户） | teams 表锚点；team_id=0 为平台侧保留值；存量行回填默认团队 |
| Account / User | 账户（用户） | system_users；role ∈ {admin, team_admin, user}；team_id 归属租户 |
| Role | 角色 | P0 用内置角色轴（admin/team_admin/user）；P1 起 roles/permissions 表化 |
| Permission | 权限点 | 资源.动作 命名（如 customer.read）；由本文件矩阵生成 |
| Scope | 数据范围 | all/department/team/self；P0 落 team 维度（scope.TeamScoped） |
| Client ID | MCP 客户端标识 | mcp_credentials；1:N 挂钥匙；绑定账户继承团队 |
| API Key | API 密钥 | mcp_credential_keys；明文双轨（sha256 校验 + AES-GCM 可查看） |
| MCP | MCP 工具入口 | POST /api/mcp（JSON-RPC），MCPGuard 唯一门 |
| Skill | Skill 注册表 | mcp_skills；绑定凭证；allowed_tools 同步凭证 scope |
| Channel | 渠道 | 清单以 internal/capregistry 为准（此处不重复维护） |
| Plan / Entitlement | 计费档位（权益） | teams.plan（free/pro/enterprise）；P0 只登记，P2 起消费 |

## 角色

| 角色 | 中文 | 说明 |
|---|---|---|
| super_admin | 超级管理员（平台） | role=admin；跨团队全量直通（需求 3） |
| team_admin | 团队管理员 | role=team_admin；本团队用户与配置 + 全部本团队业务数据 |
| member | 成员 | role=user；按授权读写本团队数据 |
| read_only_auditor | 只读审计 | P1 落地；只读 + 审计页 |

## 权限矩阵

| 权限点 | 中文 | super_admin | team_admin | member | read_only_auditor |
|---|---|---|---|---|---|
| `customer.read` | 客户-读 | ✅ | ✅ | ✅ | ✅ |
| `customer.write` | 客户-写 | ✅ | ✅ | ✅ | — |
| `customer.delete` | 客户-删 | ✅ | ✅ | — | — |
| `inbox.read` | 收件箱-读 | ✅ | ✅ | ✅ | ✅ |
| `inbox.write` | 收件箱-写 | ✅ | ✅ | ✅ | — |
| `clue.read` | 线索-读 | ✅ | ✅ | ✅ | ✅ |
| `clue.write` | 线索-写 | ✅ | ✅ | ✅ | — |
| `kb.read` | 知识库-读 | ✅ | ✅ | ✅ | ✅ |
| `kb.write` | 知识库-写 | ✅ | ✅ | ✅ | — |
| `opportunity.read` | 商机-读 | ✅ | ✅ | ✅ | ✅ |
| `opportunity.write` | 商机-写 | ✅ | ✅ | ✅ | — |
| `reach.send` | 触达-外发 | ✅ | ✅ | — | — |
| `mcp.credential.manage` | MCP凭证-管理 | ✅ | ✅ | — | — |
| `api_key.reveal` | 密钥-查看明文 | ✅ | ✅ | — | — |
| `audit.read` | 审计-读 | ✅ | ✅ | — | ✅ |
| `audit.export` | 审计-导出 | ✅ | ✅ | — | — |
| `team.manage` | 团队-管理（跨团队） | ✅ | — | — | — |
| `team.member.manage` | 团队-成员管理（本团队） | ✅ | ✅ | — | — |

## API Key 状态机

| 状态 | 中文 | 说明 |
|---|---|---|
| `active` | 启用 | 可认证 |
| `disabled` | 禁用 | 管理员手动，可再启用 |
| `suspended` | 暂停 | 欠费/违规系统级停用，enable 可恢复 |
| `revoked` | 吊销 | 终态，仅可（软）删除 |
