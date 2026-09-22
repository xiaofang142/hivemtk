# HiveMtk 用户端 - 首次启动初始化流程

> 用户端独立部署的初始化流程（开源版）

---

## 一、流程概览

```
docker compose up -d
        │
        ▼
user-server 启动 ──▶ 装配数据库超管探针（不写文件）
        │                      │
        │              首次读 install.lock（GetStatus）
        │                      │
        │                  不存在 / 读不懂
        │                      │
        │                      ▼
        │              查库有无超管：无 ⇒ State = NOT_INSTALLED
        │              （此时磁盘上没有任何 install.lock，
        │                install_id 也还不存在）
        │                      │
        │                      ▼
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
        │                - 首次铸 install_id 并落 install.lock
        │                  （admin_username + initialized=true）
        │                      │
        │                      ▼
        │              State = INITIALIZED
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
| `internal/middleware/install_status.go` | install.lock 状态查询封装（无任何授权概念） |

> **开源版变更**：
> - 移除原 `init-license` 步骤与 LicenseKey 字段
> - 移除 `must_change_password` 强制改密机制（commit 65079e5）
> - 移除 `PLATFORM_LICENSE_SECRET` HMAC 签名与按到期日倒计时的试用提示

---

## 三、详细步骤

### 3.1 启动 user-server

```bash
docker compose up -d user-server
```

容器启动后，user-server 只做一件事：把"库里首个超管是谁"这个探测函数装配进安装态模块
（`cmd/api/main.go` 里的 `install.SetAdminProbe(...)`）。**启动不写 install.lock，也不铸 `install_id`。**

1. 之后每次判状态（`GetStatus`，InitGuard 每个请求都会走一次）先读
   `install.lock`（路径优先级：`INSTALL_LOCK_PATH` 环境变量 > `./install.lock`；默认值相对的是
   **进程 CWD**，所以它落在哪取决于从哪里启动，详见 §四「落盘位置」），带 2 秒内存缓存，缓存按生效路径记账
2. **文件不存在 / 读不懂**：查库
   - 库里**有**超管 → 判 `INITIALIZED`，并把这份 lock 回填落盘（能捞出来的键原样保住，其余重建）
   - 库里**没有**超管 → 判 `NOT_INSTALLED`，磁盘上仍然什么都没有
3. **文件存在且解析成功**，按字段推断：
   - `admin_username != ""` 且 `initialized == true` → `INITIALIZED`（直接进入正常工作模式）
   - `admin_username != ""` 且 `initialized == false` → 再查库：库里有超管 → `INITIALIZED` 并回填；
     库里没有 → `HAS_ADMIN`（需调用 `init-complete`）
   - `admin_username == ""` → 再查库：库里有超管 → `INITIALIZED` 并回填；库里没有 → `NOT_INSTALLED`

> `install_id` 由**首次落盘**（即 `init-admin` 走到 `MarkAdminInitialized`）铸出：
> `ins-` + 16 字节随机十六进制，共 36 字符。在此之前 `install.GetStatus().InstallID` 是空串，
> 心跳协程读到空身份就不发——这也是"未初始化不上报"的实现方式。
>
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

> 无需任何授权码：手机号、邮箱、姓名均为选填，仅在平台集成开启（`PLATFORM_ENABLED=true`）时作为商户联系信息上报本地 platform-server。

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
- 所有功能（AI / RAG / 客服 / 营销）全开放，不存在按授权解锁的功能开关
- 平台集成关闭（默认）时不向任何地址上报；开启后心跳也是 best-effort：失败仅 Warn，不影响本地业务

---

## 四、install.lock 文件结构（开源版精简）

```json
{
  "install_id": "ins-a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4",
  "install_time": "2026-07-24T10:00:00Z",
  "admin_username": "admin",
  "initialized": true,
  "version": ""
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `install_id` | string | 一次安装的唯一标识（`ins-` + 16 字节随机十六进制，共 36 字符） |
| `install_time` | string | 安装时间（UTC RFC3339） |
| `admin_username` | string | 超管账号（创建后写入） |
| `initialized` | bool | 是否已完成初始化向导 |
| `version` | string | 客户端版本号，仅供统计；**当前代码里没有任何写入方**（本仓库无构建版本号常量），所以自安装实例上恒为空串，心跳侧回落成 `unknown`。仅从旧版／外部工具迁移过来的 lock 才可能带值 |

> **不包含**：`license_key` / `expires_at` / `company` / `contact_email` / `signature` 等授权相关字段（本版无授权流程，install.lock 里从未有这些键）。

### 落盘位置：只有两条，且默认那条是 CWD 相对的

`install.GetInstallLockPath()` 的优先级就两条：`INSTALL_LOCK_PATH` 环境变量 > `./install.lock`。

**没有 `/app/data/install.lock` 这一档。** 那是 `user-server/Dockerfile` 时代的形状，该文件已随
`94415060`「重构宿主机部署」（2026-08-17）删除（见 `user-server/docs/dev/DEVELOPMENT.md` §9.2）；
今天的根 `docker-compose.yml` 只提供 `mtk-postgres` / `mtk-redis` 两个容器，user-server 跑在宿主机，
没有任何挂载点叫 `/app/data`。

因为默认值 `./install.lock` 相对的是**进程 CWD**，它实际落在哪里完全由启动位置决定：

| 启动方式 | 锁文件实际位置 |
|---|---|
| `cd user-server && ./bin/user-server`、`make dev`（air 工作目录 `user-server/`） | `user-server/install.lock` |
| 同一二进制在仓库根启动 | 仓库根 `./install.lock` |

这不是假想风险。本仓开发机 `find . -name install.lock` 实测同时存在三份、`install_id` 各不相同
（仓库根 / `user-server/` / `user-server/internal/controller/`），而 8204 上跑着的那台经
`GET /api/system/init-status` 回读的正是 `user-server/` 那份对应的号——换 CWD 启动过的那两次，
各自铸了自己的新号。三份文件都被 `.gitignore` 的 `install.lock` 条目忽略，所以在版本控制里完全隐形。

**结论：生产部署必须显式把 `INSTALL_LOCK_PATH` 设成绝对路径**（例：`/var/lib/hivemtk/install.lock`），
并确保该目录随实例持久化（放在临时卷里＝每次发布换一次身份）。理由：

- `install_id` 是这台实例在平台侧的唯一身份。平台端 `UpsertMerchantByInstallID` 只按
  `GetByInstallID(install_id)` 判重，`device_fingerprint` 仅作为字段存下、不参与判重，
  所以换号必然多出一个新商户行，旧商户的活跃度与版本历史再也接不上。
- 新进程读不到锁时，`GetStatus` 仍会按上面第 2 条用库里超管回填并**铸新号**，
  本地业务一切正常、不报任何错——唯一的声音是心跳侧那条 WARN（见 §五）。
- 平台端关闭（默认离线形态）时换号只影响本机统计，不影响任何功能；上面这条代价仅在
  `PLATFORM_ENABLED=true` 且平台端在用时才成立。

---

## 五、平台端心跳上报（可选，默认关闭）

平台端是可选本地组件。`PLATFORM_ENABLED` 未开启（默认）时本节整条链路不装配：不读平台配置、不注册商户、不起心跳，不发一个请求。

开启后（`PLATFORM_ENABLED=true` + 配好 `config/platform.yaml` 的 `api_url`），user-server 初始化完成后向平台端低频上报：

- `POST /api/platform/install` — 安装信息上报（一次性）
- `POST /api/platform/heartbeat` — 周期性心跳（默认每 3 分钟）

特性：

- 失败仅 `Warn` 日志，**不阻塞**本地业务
- 平台端不可达时，user-server 仍正常运行
- 只用于平台端统计商户活跃度与版本分布；本版没有任何"上报了才能用"的判定
- 心跳取身份走 `install.GetStatus()` 而不是裸读文件：`install.lock` 坏着时会先自愈再上报。
  早先的写法是 `install.Load()` 一旦解析失败就 `return`，那台实例从此一帧心跳都不发且不留日志
  （`install_id` 为空的"未初始化"仍然不发，这是设计意图）
- 身份是**刚铸的**（`Status.Reminted`）时，上报前先打一条 WARN：磁盘上没有可用身份而库里已经装着
  超管，说明这台机器的锁丢了（最常见就是换 CWD 启动，见 §四），平台侧会把它记成一个新装商户。
  这条告警一次重铸只响一次（新号已落盘，下一次读走的就是正常路径）；`Reminted` 带 `json:"-"`，
  只在进程内交给告警腿，`/api/system/init-status` 与心跳请求体的字段集都不因它变化。

---

## 六、安全

- `install.lock` 不含敏感凭证（无 HMAC 签名、无授权码）
- 超管密码使用 bcrypt 哈希存储（cost=10）
- JWT 鉴权：登录后下发 token，后续 API 携带 `Authorization: Bearer <token>`
- 迁移机器/换启动目录时，把 `install.lock` 一起搬走并把 `INSTALL_LOCK_PATH` 指向它，就仍是同一个
  安装身份（库里有没有超管都判 `INITIALIZED`，见 §三）。**别顺手重铸 `install_id`**：平台侧纯按它
  记商户，换号＝旧商户的活跃与版本历史断掉（`device_fingerprint` 不参与判重）
- 它只是一个普通文件，位置见 §四：没有哪个命名卷、哪次卸载会替你留着它。文件丢了而库里有超管，
  服务照样正常起（走库兜底回填），代价就是身份换了一枚、心跳侧响一条 WARN

---

## 七、相关代码

| 文件 | 作用 |
|------|------|
| `internal/system/install/install.go` | install.lock 读写 + 状态机（`GetStatus` / `Load` / `Save` / `MarkAdminInitialized`；损坏文件按键自愈，落盘走临时文件 + rename） |
| `internal/controller/system_init.go` | `GET /api/system/init-status` + `POST /api/system/init-complete` |
| `internal/controller/auth.go` | `POST /api/system/init-admin`（`AuthController.InitAdmin`） |
| `internal/service/auth.go` | `AuthService.InitAdmin`：超管创建主逻辑 |
| `internal/middleware/init_guard.go` | 初始化保护中间件（未 `INITIALIZED` 时拦截业务 API） |
| `internal/middleware/install_status.go` | install.lock 状态查询封装（无任何授权概念） |
| `internal/model/system_user.go` | `system_users` 模型（已移除 `must_change_password` 字段） |

---

## 八、相关文档

- 部署手册：[MERCHANT_DEPLOYMENT.md](MERCHANT_DEPLOYMENT.md)
- 部署方案：[../architecture/部署方案_用户端.md](../architecture/部署方案_用户端.md)
- 平台端 / 用户端分工：`hivemtk-platform/docs/architecture/部署方案_平台端与用户端.md`（跨仓库路径：属 hivemtk-platform 仓，本仓 checkout 内不可点）
