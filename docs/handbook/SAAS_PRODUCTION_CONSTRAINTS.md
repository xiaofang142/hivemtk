# SaaS 生产部署约束与教训（003 生产联调沉淀）

> **定位**：xapptool.cn 云端 SaaS 实例（TENANT_MODE=on）上线联调（2026-10-19）抓出的
> 全部活体缺陷与由此确立的硬约束。**每一条都对应一次真实翻车**——改相关代码前必读。
> **配套验证**：`scripts/prod_e2e_verify.sh`（25 断言全链 e2e，真实网关签名同构回调）。
> **事实基准**：全部来自本轮生产联调实测，无推测。

---

## 一、多租户编码硬约束（违反必炸，mock 测不出来）

### 1.1 所有仓储查询必须 `WithContext(ctx)`

on 模式下 GORM tenancyidx 回调从 ctx 取租户注入 `WHERE tenant_id=?`；**丢 ctx =
fail-closed 5000**（"需要租户上下文但 ctx 未绑定"）。

- 真实事故：`AgentStatusRepository` 8 处查询直接 `r.db.XXX()` 无 WithContext
  → 生产 `GET /api/agents/all` 恒 5000（commit 496215c8 修复）。
- **规则**：仓储方法签名有 ctx 的，方法体内第一个调用必须是 `r.db.WithContext(ctx)`。
  禁止 `r.db.Where(...)` / `r.db.Find(...)` 裸链。code review 见到裸链直接打回。

### 1.2 禁止"部署级单行表"设计

001 多租户迁移给**所有表**都加了 `tenant_id` 列且 tenancyidx 对其生效注入——
任何"全实例共享一行"的表设计在第二个租户出现时读写全错。

- 真实事故：`tenant_subscription_mirror` 旧行锚 `id=1` 单行——tenant#3 注册后
  写撞主键（id=1 已被占）、读 `WHERE id=1 AND tenant_id=3` 恒空 → 订阅状态永远
  no_data（commit 67c2f18f 改为每租户一行 `id=tenant_id`）。
- 本地 e2e 未暴露的原因：测试租户恰好是 id=1，单行锚点撞对了。
- **规则**：新表要么每租户一行（行锚=tenant_id），要么显式声明为系统表并走
  `tenant.WithSystemScope(ctx)` 全链路（不允许"默认没 tenant_id"的假设）。

### 1.3 map 更新的键必须是 GORM 列名（snake_case），不是 json 名

- 真实事故：`tenant.go` 用 `"platform_sync_error"`（json tag 名）做 map 更新键，
  实际列名是 `platform_sync_err` → 开户登记上报失败被静默回滚。
- **规则**：`Updates(map[string]any{...})` 的键 = 数据库列名。改模型字段时全仓
  grep 同名 map 键。

### 1.4 幂等检查必须判 `(nil, nil)`，不只判 err

- 真实事故：`GetByClientOrderNo` 未找到返回 `(nil, nil)`，调用方只判 `err == nil`
  就解引用 → **首单必 panic 500**。
- **规则**：仓储"未找到返回 (nil, nil)"的约定下，调用方必须 `err == nil && x != nil`。

---

## 二、SaaS 首访路由（init-status 语义）

### 2.1 约束：tenant_mode=on 的实例恒"已初始化"

- **问题史**：`/api/system/init-status` 读 `install.lock` 判定；docker 直部的 SaaS
  实例从未跑 /setup 向导 → 恒 `NOT_INSTALLED` → 干净浏览器首访被路由守卫推进
  /setup 单机安装向导（用户反复报告的"老问题"）。
- **现行契约**（commit 033bbed3）：
  - 后端：`tenant_mode=on` 时 init-status 恒返回 `initialized=true / INITIALIZED`
    （SaaS 无"单机未安装"概念，租户经 /register 自助开户；install.lock 只服务
    离线单机）。
  - 前端：`bootstrapState.ensureBootstrap()` 里 `tenantMode=true` 强制
    `initialized=true`（双保险：接口失败/滞后也不把人送进 /setup）。
  - 首访动线：`work.xapptool.cn` → /login（tenant_code 可选 + "没有账号？注册
    商户账号"入口）→ /register 完整开户登记。
- **规则**：改 init-status / 路由守卫 / bootstrapState 任一处，必须同步核对另外
  两处；离线单机（off 模式）行为不得被连带改变（仍走 setup 向导）。

### 2.2 登录契约

- on 模式登录 `tenant_code` **可选**：提供则定向租户 scope 查用户名，缺省走
  全局（邮箱→用户名）。多租户重名场景靠 tenant_code 区分。
- 注册字段契约：`name/username/email/password` + 监管四项
  `company_name/legal_person/phone/address`（+`license_no` 选填、
  `regulatory_ack=true`、`agreement_version`）。字段名错一个就 4101。

### 2.2b 注册的两条路径（填不填租户代码分流）

注册页（`/#/register`）现在有**一条入口、两条后端路径**，由「填没填租户代码」决定：

| 表单 | 后端 | 语义 | 响应 |
|---|---|---|---|
| 填了租户代码 | `POST /api/auth/register` | **加入已有商户**：账号落进该租户下，不建新租户 | `{token, user, tenant_id, tenant_code}` —— 直接自动登录 |
| 留空 | `POST /api/tenant/register` | **新商户开户**：顺带创建租户 | `{tenant_id, tenant_code, token}` —— 提示新租户代码 |

- 「加入已有商户」这条后端路径**早就存在**（`resolvePublicTenantCtx` 按短码绑租户，
  on 模式必填 tenant_code），此前前端 0 处调用 —— 商户给员工开通账号只能走后台，
  现在前台可直接自助注册进自己的租户。
- **字段名有坑**：加入路径用 `real_name`（负责人），开户路径用 `legal_person`；
  开户路径还要 `name`（租户展示名）。前端已按各自口径分别发。
- 租户代码填错 → 后端 4106（`CodeTenantNotFound`），前端据此给「租户代码不存在，
  请向所属商户索取正确的租户代码」。
- 租户代码输入框**只在 `tenant_mode=on` 显示**，私域独立部署（off）没有租户概念。

### 2.3 租户代码（tenant_code）丢了怎么找

- **租户代码只在开户成功那一次露出**：`/register` 注册完成后弹一次
  「您的租户代码：xxx，请妥善保存」。之后登录页不再回显，登录响应与 JWT 里
  **只有 tenant_id、没有 tenant_code** ⇒ 换台机器/清了 localStorage 就找不回来。
- **现已在两个页面常驻展示**（2026-10-22 加）：
  - 「个人中心」`/#/profile` —— 基础信息卡片里的「租户代码」行（等宽字体 + 复制按钮）；
  - 「站点设置」`/#/system/config` —— 基础信息区的「租户代码」行。
  - 两处都只在 `tenant_mode=on` 的 SaaS 形态显示（与登录页「租户代码」输入框同口径）；
    私域独立部署（off）登录页没有这个输入框，展示短码只会误导，故整块隐藏。
- **接口口径**：`GET /api/tenant/info` → `{tenant_id, tenant_code, name, status}`。
  租户**取自 JWT 的 tenant_id claim**、不是请求参数 ⇒ 任何登录用户读到的都是
  自己这一家，天然防越权；带登录态即可查：

  ```bash
  curl -s https://work.xapptool.cn/api/tenant/info \
    -H "Authorization: Bearer <登录后的 token>"
  ```

- **注意**：`tenant_code` 是租户**短码**（`t` + 6 位低歧义字符，如 `tz3rmdj`），
  不是 `tenant_id`（数据库自增 id）。后者在 JWT 里，前者只在上面这个接口里。
- **落库口径（2026-10-22 修）**：登录响应确实带 `tenant_id`/`tenant_code`
  （MFA 第二步 `/api/auth/mfa/verify` 同样带），前端落进 `localStorage.user_info`。
  但 `stores/user.js` 的 `setUserInfo` 原先对每个字段都是 `info.x || ''`，
  而个人资料页刷新用户信息时只传 `{id, username, email, role}` ⇒
  `tenant_id`/`tenant_code` 两个 key 不存在，被 `|| ''` 覆盖成空串 ——
  **打开一次个人中心，登录时记下的租户代码就没了**（连带 `HelpCenter.vue`
  拼 `tenant_code` 查询参数也变空）。
  修法：租户字段按「缺失就沿用旧值」合并（`info.tenant_code ?? 旧值`）；
  换账号残留由 `logout()` 显式清空兜住。

---

## 三、平台集成装配链（缺一环全断）

### 3.1 user-server 容器必须有 `config/platform.yaml`

- **问题史**：`LoadPlatform("config/platform.yaml")` 必读；Dockerfile 只 COPY
  `config.yaml` 不拷 `platform.yaml` → 容器内文件缺失 → `PlatformCfg=nil` →
  商户上报/续费下单/模块同步全部"平台配置未初始化"。
- **现行契约**（commit 见 deploy 仓）：Dockerfile COPY `config/platform.yaml`
  ＋ deploy.sh 同步该文件到服务器 ＋ 服务器 .env 必须有
  `MERCHANT_API_SECRET`（yaml 里 `${MERCHANT_API_SECRET}` 展开用）。
  `PLATFORM_ADMIN_PASSWORD` 已随 L-1（2026-11-11 删 `/platform/*` JWT 代理通道）退出用户端
  装配链——platform.yaml 无该展开行、user 栈 .env 不再需要；平台栈 .env 仍需要它 seed
  首个平台超管。
- **规则**：user-server 新增任何"仓库内配置文件依赖"，必须同时改
  Dockerfile COPY + deploy.sh rsync，否则线上容器缺文件。

### 3.2 服务器 .env 必备键（user 栈 / platform 栈）

| 栈 | 键 | 说明 |
|---|---|---|
| user | `TENANT_MODE=on` | SaaS 形态开关（配 P0 装配修复后才真正生效） |
| user | `PLATFORM_ENABLED=true` + `PLATFORM_API_HOST=https://manageapi.xapptool.cn` | 平台集成 |
| user | `MERCHANT_API_SECRET` | 必须与 platform 栈**同值**（HMAC 对齐） |
| user | `MASTER_KEY` | 生产必需（首次生成永不覆盖） |
| platform | `EPAY_GATEWAY_URL/EPAY_PID/EPAY_API_KEY/EPAY_TRADE_TYPES` | 易支付真实凭据 |
| platform | `PAYMENT_ENC_KEY` | 通道 api_key 加密 |
| platform | `PLATFORM_PUBLIC_URL=https://manageapi.xapptool.cn` | 回调可达前提 |

> `PLATFORM_ADMIN_PASSWORD` 原列在 user 栈（platform.yaml 展开用）；L-1（2026-11-11）删除
> JWT 代理通道后 user 栈不再读取该键，仅 platform 栈 `.env` 需要（seed 首个平台超管）。

### 3.3 env 改动必须 `docker compose up -d` 重建

`docker restart` **不重读 env_file**——改 .env 后必须 compose up -d 重建容器。

### 3.4 平台通道启用顺序：先 GET 再 PUT

env 兜底行只在 `GET /platform/payments/channels`（List）时创建；直接 PUT
enable 时行可能不存在或三元组为空 → 400"启用前必须配置网关地址、商户号 pid
与密钥"。**运营操作顺序：先 GET（触发兜底灌行）→ 再 PUT {"enabled":true}**。
另：PUT 是部分更新，空串会保持旧值（e54e610 修复了"部分更新清空三元组"）。

---

## 四、生产数据播种（首次上线/新环境）

### 4.1 播种清单与操作方式

| 数据 | 操作 | 注意 |
|---|---|---|
| admin 密码 | SQL UPDATE `admin_users.password_hash`（Go bcrypt `$2a$` 预生成） | 服务器无 htpasswd/python3；hash 含 `$` 用单引号 |
| 模块目录 | `POST /platform/module-catalog/seed`（admin JWT） | Seed() 走 loadCatalogSeed 三级回退（MODULE_SEED_FILE → 磁盘 → 内嵌），a4d60ea 修复 |
| LLM 套餐 | `POST /platform/llm/plans` ×N，`status=published` | 扣减按 token 数（输入输出分列），per-1k 单价只做成本展示 |
| 订阅配置 | `PUT /platform/subscription/config` | 900 分 + trial_days=15 |
| 资产包 | 本地库 COPY 导出 → 生产 `\copy ... FROM` 导入 | 见 4.2 列序坑 |
| 支付通道 | GET → PUT enable | 见 3.4 |

### 4.2 跨环境 COPY 导入：必须显式列清单

本地开发库与生产库**列序不同**（如 contributors.tokens_invalid_before 位置不同、
assets 缺 rating 列后补）——CSV 按位置 COPY 必错。`\copy table(col1,col2,...) FROM`
按名对齐。导入前先 diff 两边 `information_schema.columns`。

### 4.3 服务器环境事实

- 容器内 postgres 监听 **8201/8202**（不是 5432）：`psql -h 127.0.0.1 -p 8201`
- 服务器**无 python3/htpasswd**；md5 用 `md5sum`，本机 macOS 用 `md5 -q`
- SSH 高频连会触发限连冷却（Connection closed/banner timeout）——批量脚本合并
  成单次 `ssh bash -s` 执行，别连环 ssh

---

## 五、验证方法论（为什么 mock 全绿 ≠ 生产能过）

本轮 mock e2e 25/25 绿但生产首跑 9/37——mock 掩盖的四类问题：

1. **装配缺口**：单测 `SetMode` 注入掩盖 `InitFromEnv` 从未被调用（TENANT_MODE
   静默失效）；容器缺 platform.yaml 掩盖在单测（本地有文件）。
2. **单行锚点巧合**：本地测试租户 id=1 恰好匹配镜像表单行锚。
3. **首单路径**：mock 先建数据再断言，首单 panic 路径不可见。
4. **匿名路径**：mock 带 token，匿名逛市场被租户 fail-closed 5000 不可见。

**规则**：
- 上线前必跑 `scripts/prod_e2e_verify.sh`（或先在预发跑）：注册新租户（真实
  id 不是 1）→ trial → 下单 → 真实网关 pay_url → 自签回调（与易支付密码学同构，
  用商户密钥 MD5 签名，覆盖验签/金额/幂等/开通全链且不花真钱）→ 激活 → 续期 →
  active → 平台端 fulfilled。
- e2e 断言顺序 = 业务依赖顺序：先 sync（拉平台镜像）再查订阅状态。
- 空响应 ≠ 失败：跨境/重启窗口 curl 静默超时，脚本要带重试（3 次 + sleep）。

---

## 六、本轮修复提交索引（供 git archaeology）

| commit | 仓 | 内容 |
|---|---|---|
| 00a2e998 | user | TENANT_MODE 装配缺口等 4 处 P0（InitFromEnv 接线/首单 panic/匿名市场/列名） |
| e54e610 | platform | 通道部分更新清空三元组 + config env 展开 |
| a4d60ea | platform | Seed() 三级回退（容器无磁盘种子文件致 seed 恒 0） |
| 67c2f18f | user | 订阅镜像改每租户一行（单行设计第二租户起全错） |
| 496215c8 | user | AgentStatusRepository 8 处补 WithContext |
| 033bbed3 | user | SaaS init-status 恒已初始化（首访不再进 /setup） |
| cf20bc6e | user-web | 移除 setup 向导隐私上报文案 |
| bc4d6593 | user | prod_e2e_verify.sh 归档 |

deploy 仓（非 git）：Dockerfile COPY platform.yaml、deploy.sh 同步该文件。

---

## 七、日常检查清单（新环境上线 / 疑似回潮时逐项过）

1. `curl https://workapi.xapptool.cn/api/public/config` → `tenant_mode:true` 且
   `edition:saas`
2. `curl https://workapi.xapptool.cn/api/system/init-status` → `initialized:true`
3. 干净浏览器（无 localStorage）打开 work.xapptool.cn → **应直达 /login**
4. 平台端登录 admin → 模块目录 24 个（browser-automation=9900 其余 0）、
   订阅配置 900/15、LLM 套餐 published、支付通道 enabled 且 mock=false
5. 跑 `scripts/prod_e2e_verify.sh` → 25/25
6. user-server 日志 grep "平台配置加载失败" / "需要租户上下文" → 应为空
   （后台任务的 reach/sop/email 周期性租户报错属已知噪声，见部署 README）
