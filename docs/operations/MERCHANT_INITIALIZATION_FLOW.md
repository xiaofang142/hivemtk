# HiveMtk 用户端 - 首次启动初始化流程

> 用户端独立部署的初始化流程（开源版）

---

## 一、流程概览

```
docker compose up -d
        │
        ▼
user-server 启动 ──▶ 读取 install.lock
        │                      │
        │                  不存在
        │                      │
        │                      ▼
        │              EnsureInstallID 生成 install_id
        │              写入最小 install.lock（initialized=false）
        │                      │
        │                      ▼
        │              State = NOT_INSTALLED
        │              InitGuard 仅放行白名单 API
        │                      │
        │                      ▼
        │              监听 POST /api/system/init-admin
        │              等待管理员提交超管账号
        │                      │
        │                      ▼
        │              service.AuthService.InitAdmin：
        │                - 强密码校验 + 用户名唯一性
        │                - 写入 system_users 表
        │                - 回写 install.lock（admin_username + initialized=true）
        │                      │
        │                      ▼
        │              State = HAS_ADMIN → INITIALIZED
        │                      │
        ▼                      │
user-server 重启校验 install.lock   ◀────────┘
        │
        ▼
   install.lock.initialized == true？
        │
    ┌───┴────┐
   是        否
    │        │
    │        └─▶ 重新进入初始化流程
    │
    ▼
 监听 POST /api/system/init-complete
 标记初始化完成（推进到 INITIALIZED）
        │
        ▼
 系统就绪（不再强制首登改密）
```

> **状态机**：`NOT_INSTALLED` → `HAS_ADMIN`（已写超管） → `INITIALIZED`（`initialized=true`）
> 详见 `internal/system/install/install.go` 的 `GetStatus()`。

---

## 二、关键文件

| 文件 | 作用 |
|------|------|
| `install.lock` | 部署凭证（最小字段：`install_id` / `install_time` / `admin_username` / `initialized` / `version`） |
| `internal/system/install/install.go` | install.lock 读写 + 状态机查询（带 2 秒内存缓存） |
| `internal/controller/system_init.go` | HTTP API：`GET /api/system/init-status` + `POST /api/system/init-complete` |
| `internal/controller/auth.go` | `POST /api/system/init-admin`（由 AuthController.InitAdmin 提供） |
| `internal/middleware/init_guard.go` | 初始化保护中间件：未完成初始化时仅放行白名单 API |
| `internal/middleware/license_checker.go` | install.lock 状态查询封装（开源版不校验 LicenseKey） |

> **开源版变更**：
> - 移除原 `init-license` 步骤与 LicenseKey 字段
> - 移除 `must_change_password` 强制改密机制（commit 65079e5）
> - 移除 `PLATFORM_LICENSE_SECRET` HMAC 签名与 7 天免费试用

---

## 三、详细步骤

### 3.1 启动 user-server

```bash
docker compose up -d user-server
```

容器启动后，user-server 会：

1. 读取 `/app/data/install.lock`（路径优先级：`INSTALL_LOCK_PATH` 环境变量 > `./install.lock`）
2. **若不存在**：
   - 调用 `EnsureInstallID()` 生成 32 位 `install_id`（`ins-` + 16 字节随机十六进制）
   - 写入最小 install.lock（`initialized=false`）
   - 进入 `NOT_INSTALLED` 状态
3. **若存在**：
   - 解析 install.lock，按字段推断状态：
     - `admin_username != ""` 且 `initialized == true` → `INITIALIZED`（直接进入正常工作模式）
     - `admin_username != ""` 且 `initialized == false` → `HAS_ADMIN`（需调用 `init-complete`）
     - 否则 → `NOT_INSTALLED`（进入初始化模式）

> **InitGuard 中间件**：未 `INITIALIZED` 时拦截所有非白名单业务 API，引导前端跳转 `/setup`。
> 白名单：`/api/system/init-status` / `/api/system/init-admin` / `/api/system/init-complete` / `/health` 等。

### 3.2 浏览器访问初始化页面

访问 `http://<your-server-ip>:8204/setup`：

```
┌────────────────────────────────────────────┐
│  欢迎使用 HiveMtk                            │
│  请创建超级管理员账号                          │
│                                            │
│  用户名: [admin_____________]              │
│  密码:   [_________________]                │
│  确认密码: [_________________]              │
│  姓名:   [_________________]                │
│  邮箱:   [_________________]                │
│  手机:   [_________________]                │
│                                            │
│  [   创建账号   ]                           │
└────────────────────────────────────────────┘
```

> 开源版无需 LicenseKey。手机号、邮箱、姓名均为选填，作为商户联系信息上报平台端。

### 3.3 创建超管账号

提交 `POST /api/system/init-admin`（由 `AuthController.InitAdmin` 处理）：

```json
{
  "username": "admin",
  "password": "YourStrongPassword!",
  "email": "admin@example.com",
  "real_name": "超级管理员",
  "contact_phone": "13800138000"
}
```

服务端流程（`service.AuthService.InitAdmin`）：

1. 加载 install.lock
2. 校验用户名唯一性（DB 查询）
3. 强密码校验（≥ 8 字符）
4. bcrypt 哈希密码，写入 `system_users` 表
5. **同步 install.lock**：写入 `admin_username` + `initialized=true`
6. 返回成功，state 推进到 `INITIALIZED`

### 3.4 完成初始化

调用 `POST /api/system/init-complete`（由 `SystemInitController.InitComplete` 处理）：

- 前置校验：`HasInstallLockAdmin() == true`（必须先创建超管）
- 写入 `install.lock.initialized = true`（幂等：已 `INITIALIZED` 时重复调用无副作用）
- 返回 `next_action: "login"` 引导跳转登录页

> 此接口用于前端向导完成时显式标记；若 `init-admin` 已自动写入 `initialized=true`，此步骤可省略。

### 3.5 登录

```
http://<your-server-ip>:8204/login
```

使用刚创建的超管账号登录（JWT 鉴权）。开源版**不再强制首登改密**，登录后直接进入系统主页。

### 3.6 系统就绪

完成上述步骤后：

- 系统正式可用
- 所有功能（AI / RAG / 客服 / 营销）全开放，无 License 范围限制
- 平台端心跳上报为 best-effort：失败仅 Warn，不影响本地业务

---

## 四、install.lock 文件结构（开源版精简）

```json
{
  "install_id": "ins-a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4",
  "install_time": "2026-07-24T10:00:00Z",
  "admin_username": "admin",
  "initialized": true,
  "version": "1.0.0"
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `install_id` | string | 一次安装的唯一标识（`ins-` + 16 字节随机十六进制） |
| `install_time` | string | 安装时间（UTC RFC3339） |
| `admin_username` | string | 超管账号（创建后写入） |
| `initialized` | bool | 是否已完成初始化向导 |
| `version` | string | 客户端版本号（用于统计上报） |

> **不包含**：`license_key` / `expires_at` / `company` / `contact_email` / `signature` 等授权相关字段（已移除）。

---

## 五、平台端心跳上报（best-effort）

user-server 初始化完成后，会通过 `PLATFORM_API_URL` 向平台端低频上报心跳与安装信息：

- `POST /api/platform/install` — 安装信息上报（一次性）
- `POST /api/platform/heartbeat` — 周期性心跳

特性：

- 失败仅 `Warn` 日志，**不阻塞**本地业务
- 平台端不可达时，user-server 仍正常运行
- 用于平台端统计商户活跃度与版本分布，不用于授权校验

---

## 六、安全

- `install.lock` 不含敏感凭证（无 HMAC 签名、无 LicenseKey）
- 超管密码使用 bcrypt 哈希存储（cost=10）
- JWT 鉴权：登录后下发 token，后续 API 携带 `Authorization: Bearer <token>`
- 迁移机器后 `install.lock` 可直接复制（无需重新初始化，但建议重新生成 `install_id`）
- 卸载软件会保留 `install.lock`（在 `/app/data/` 命名卷中），重装不丢

---

## 七、相关代码

| 文件 | 作用 |
|------|------|
| `internal/system/install/install.go` | install.lock 读写 + 状态机（`GetStatus` / `MarkAdminInitialized` / `EnsureInstallID`） |
| `internal/controller/system_init.go` | `GET /api/system/init-status` + `POST /api/system/init-complete` |
| `internal/controller/auth.go` | `POST /api/system/init-admin`（`AuthController.InitAdmin`） |
| `internal/service/auth.go` | `AuthService.InitAdmin`：超管创建主逻辑 |
| `internal/middleware/init_guard.go` | 初始化保护中间件（未 `INITIALIZED` 时拦截业务 API） |
| `internal/middleware/license_checker.go` | install.lock 状态查询封装（开源版不校验 LicenseKey） |
| `internal/model/system_user.go` | `system_users` 模型（已移除 `must_change_password` 字段） |

---

## 八、相关文档

- 部署手册：[MERCHANT_DEPLOYMENT.md](MERCHANT_DEPLOYMENT.md)
- 部署方案：[../architecture/部署方案_用户端.md](../architecture/部署方案_用户端.md)
- 平台端 / 用户端分工：`hivemtk-platform/docs/architecture/部署方案_平台端与用户端.md`（跨仓库路径：属 hivemtk-platform 仓，本仓 checkout 内不可点）
