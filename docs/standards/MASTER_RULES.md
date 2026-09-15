# HiveMtk 项目核心规则

> 本项目的编码规范和开发准则。适用于单商户本地部署场景。

---

## 1. 架构规则

| 规则 | 说明 |
|------|------|
| 五层架构 | Controller → Service → Repository 单向依赖 |
| Controller | 仅做参数绑定，不写业务逻辑 |
| Service | 必须通过 Repository 访问数据库 |
| 配置管理 | 所有配置通过 `internal/config/` 加载 |

## 2. 安全规则

| 规则 | 说明 |
|------|------|
| 鉴权 | 必须在网关层完成（JWT） |
| 密码 | bcrypt cost = 10，≥ 12 位 |
| SQL | 强制参数化，禁止拼接 |
| 日志 | 敏感信息自动脱敏 |
| 密钥 | 从环境变量加载，禁止硬编码 |

## 3. 编码规范

### Go 后端

| 项目 | 规范 |
|------|------|
| 版本 | Go 1.21+ |
| 框架 | Gin + GORM |
| 错误处理 | 必须显式 `if err != nil` |
| 上下文 | 函数首参 `ctx context.Context` |

### 前端 (Vue 3)

| 项目 | 规范 |
|------|------|
| 构建 | Vite |
| 状态 | Pinia |
| UI | Element Plus |
| HTTP | 统一走 `utils/request.js` |

### 数据库

| 项目 | 规范 |
|------|------|
| 字符集 | UTF-8 |
| 时间 | TIMESTAMPTZ |
| 软删除 | deleted_at 字段 |
| 索引 | 高频查询字段加索引 |

## 4. 提交规范

### Conventional Commits

```
<type>(<scope>): <subject>

feat    新功能
fix     修复
docs    文档
refactor 重构
test    测试
chore   杂项
```

### PR 流程

1. Fork 仓库 → 创建特性分支
2. 本地验证：`make ci-local`
3. 签署 DCO：`git commit -s`
4. 推送 + 创建 PR
5. 2 位 Review + Approve
6. Squash Merge

## 5. 版本管理

- 语义化版本：`MAJOR.MINOR.PATCH`
- MINOR 版本向后兼容
- 破坏性变更在 MAJOR 版本

## 6. 相关文档

- [架构设计](../architecture/)
- [部署指南](../operations/)
- [API 文档](../../user-server/api)

---

*最后更新: 2026-08-16*
