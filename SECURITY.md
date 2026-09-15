# 安全策略 (Security Policy)

> 本文件适用于 HiveMtk 用户端仓库（`hivemtk`）。如发现安全漏洞，请按本流程报告。

---

## 1. 支持版本 (Supported Versions)

| 版本 | 状态 | 安全修复支持 |
|------|------|------------|
| `main` 分支（最新） | ✅ 维护中 | ✅ 接受安全修复 |
| 最新 Release tag | ✅ 维护中 | ✅ 接受安全修复 |
| 历史 Release tag | ⚠️ 不维护 | ❌ 请升级到最新版本 |

> 本项目处于早期阶段，仅对 `main` 分支与最新 Release 提供安全修复支持，不维护历史版本的回补。

---

## 2. 安全报告流程 (Reporting a Vulnerability)

### 2.1 报告渠道

**请勿在公开 Issue 中提交安全漏洞**。请通过以下私密渠道之一报告：

- **邮箱**：`jideilvluoqun@gmail.com`
- **Gitee 私信**：[@xhpmayun](https://gitee.com/xhpmayun)
- **GitHub Security Advisory**：使用 GitHub 私密漏洞报告功能
  - GitHub 仓库：[xiaofang142/hivemtk](https://github.com/xiaofang142/hivemtk)
  - 路径：`Security` → `Report a vulnerability`

### 2.2 报告内容

为便于快速响应，请在报告中包含：

- **漏洞类型**（SQL 注入 / XSS / 越权 / 信息泄露 / RCE / SSRF / 其它）
- **受影响版本**（commit hash 或 tag）
- **复现步骤**（最小可复现 PoC）
- **影响评估**（受影响的资产 / 数据 / 业务）
- **建议修复方案**（可选）
- **报告者联系方式**（用于后续协调与致谢）

### 2.3 响应时效

| 阶段 | 时限 | 动作 |
|------|------|------|
| 收到确认 | 24 小时内 | 邮件回执确认收到 |
| 初步评估 | 3 个工作日内 | 评估漏洞等级（Critical / High / Medium / Low） |
| 修复方案 | 7 个工作日内 | 提供修复方案或缓解措施（Critical / High 优先） |
| 修复发布 | 30 天内 | 发布修复版本（特殊情况可延长，并持续同步进度） |
| 公开披露 | 修复发布后 90 天 | 在征得报告者同意后公开披露 |

---

## 3. 漏洞披露策略 (Disclosure Policy)

### 3.1 协调披露 (Coordinated Disclosure)

本项目采用**协调披露**策略：

1. 报告者私密报告漏洞
2. 维护团队评估并开发修复
3. 修复版本发布后，**同步告知报告者**
4. 在修复发布后 90 天，或在征得报告者明确同意后，**公开披露漏洞细节与修复方案**
5. 公开披露渠道：GitHub Security Advisory + Gitee Issue（脱敏）

### 3.2 报告者致谢

经报告者同意后，我们会在以下位置致谢：

- GitHub Security Advisory 致谢栏
- README.md 致谢章节（重大漏洞）

### 3.3 不视为安全漏洞的情况

以下情况不视为安全漏洞，请通过普通 Issue 提交：

- 私域部署后**未修改默认密钥**（POSTGRES_PASSWORD / JWT_SECRET 等）导致的安全问题
- 私域部署后**未配置反向代理 / HTTPS** 导致的传输安全问题
- 用户**主动配置错误**导致的功能异常
- 第三方依赖的已知漏洞（请通过 `go mod update` 或 `npm audit fix` 升级）
- 性能 / 可用性问题（非安全语义）
- 与本项目无关的渠道平台（抖音 / 微信 / Telegram 等）政策问题

---

## 4. 安全基线 (Security Baseline)

### 4.1 私域部署强制密钥

部署时**必须**修改以下密钥（默认值会在 `make install` 时自动生成，但仍建议人工核对）：

| 密钥 | 环境变量 | 生成命令 |
|------|---------|---------|
| PostgreSQL 密码 | `POSTGRES_PASSWORD` | `openssl rand -hex 24` |
| Redis 密码 | `REDIS_PASSWORD` | `openssl rand -hex 24` |
| JWT 签名密钥 | `JWT_SECRET` | `openssl rand -hex 32` |
| 平台代理密码 | `PLATFORM_ADMIN_PASSWORD` | 自定义强密码（与平台端 .env 保持一致） |

### 4.2 鉴权模型

- **JWT**: 用户端使用 JWT 鉴权，超管 + role + data_scope 注入 JWT
- **AppKey 软解析**: 私域部署无强鉴权（基线），依赖网络边界 + JWT
- **行级权限**: data_scope 字段控制数据可见范围（详见 [docs/marketing-features/row-level-security.md`row-level-security.md`）
- **公开路由**: Webhook / 追踪像素 / 退订页等公开路由按渠道签名鉴权

### 4.3 凭据加密

- AppSecret / Token / AESKey / AK / SK 等敏感凭据使用 **AES-256-GCM** 加密存储
- 返回时统一掩码（如 `app_secret_masked`）
- 详见各渠道账号管理文档

### 4.4 合规提示

每次主动触达发送前，服务端日志会打印 `[COMPLIANCE]` 合规提示（不可关闭），提醒操作者遵守各渠道平台规则。详见 [README.md 合规声明](README.md)。

---

## 5. 安全最佳实践 (Best Practices)

### 5.1 部署侧

- 在反向代理（反向代理层 / Caddy）层强制 HTTPS
- 配置 CSP / X-Frame-Options / X-Content-Type-Options 等安全响应头
- 限制 Webhook 入站 IP 白名单（Postmark / SendCloud / 各渠道）
- 数据库端口不暴露公网（容器内端口 8202，宿主机映射 8232，建议改为内网或 127.0.0.1）
- Redis 端口不暴露公网（容器内端口 8203）
- 本地推理栈端口（llama-server :8207 LLM / :8208 Embedding / :8209 Rerank）不暴露公网

### 5.2 运维侧

- 定期备份 PostgreSQL（`make backup`）
- 监控异常登录告警（详见 [docs/marketing-features/anomaly-login-detector.md`anomaly-login-detector.md`）
- 监控操作日志（详见 [docs/marketing-features/operation-log.md`operation-log.md`）
- 监控安全审计（详见 [docs/marketing-features/security-audit.md`security-audit.md`）
- 升级前阅读升级指南（`docs/marketing-features/upgrade.md`，编写中）

### 5.3 开发侧

- 严格遵循五层架构（Controller → Service → Repository → Model → DTO），**禁止跨层调用**
- Controller 仅做参数解析 / 调 service / 统一响应
- 输入校验通过 `binding:"required"` + service 层双重校验
- 敏感操作（启停 / 改密 / 删除）落 `operation_logs` 审计
- 行级过滤必须调用 `middleware.ApplyDataScope`
- 不在日志中打印明文凭据

---

## 6. 安全相关功能模块文档

| 文档 | 说明 |
|------|------|
| [docs/marketing-features/auth-login-jwt.md`auth-login-jwt.md` | 登录认证与 JWT 鉴权 |
| [docs/marketing-features/permission-system.md`permission-system.md` | 角色管理 + 授权管理 + 菜单权限 |
| [docs/marketing-features/row-level-security.md`row-level-security.md` | 行级权限 / 数据范围 |
| [docs/marketing-features/anomaly-login-detector.md`anomaly-login-detector.md` | 异常登录预警 |
| [docs/marketing-features/security-audit.md`security-audit.md` | 安全审计 |
| [docs/marketing-features/operation-log.md`operation-log.md` | 操作日志（事件总线订阅） |
| [docs/marketing-features/trace-dashboard.md`trace-dashboard.md` | 全链路追踪驾驶舱 |

---

## 7. 联系与社区

| 渠道 | 入口 | 说明 |
|------|------|------|
| 🐛 安全漏洞 | `jideilvluoqun@gmail.com` | 私密报告 |
| 💬 一般问题 | [Gitee Issues](https://gitee.com/xhpmayun/hivemtk/issues) | 公开讨论 |
| 📧 商务合作 | `jideilvluoqun@gmail.com` | 企业级支持 |

---

## License

本项目以 AGPL-3.0 发布，详见 [LICENSE](LICENSE) 与 [NOTICE](NOTICE)。
