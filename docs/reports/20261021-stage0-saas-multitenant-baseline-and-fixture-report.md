# HiveMtk SaaS 多租户全链路测试 — Stage 0 阶段报告暨专家团多智能体编排报告

> **报告编号**：`HIVEMTK-QA-STAGE0-20261021`  
> **所属总计划**：[`e:/HiveMtk/saas-multitenant-master-test-plan.md`](file:///e:/HiveMtk/saas-multitenant-master-test-plan.md)  
> **联合编排专家团**：`@[orchestrator]` · `@[devops-engineer]` · `@[backend-specialist]` · `@[database-architect]` · `@[security-auditor]` · `@[test-engineer]`  
> **执行时间**：2026-10-21 14:50 ～ 15:30 (+08:00)  
> **代码基线**：[`E:/HiveMtk/hivemtk-master`](file:///e:/HiveMtk/hivemtk-master)（`HEAD` = `07a7c025`，基于上游 `537d1398` 多租户分档守卫基线）  
> **执行策略与红线合规**：**只记台账不改源码、待阶段汇总审批后统一修复**（`user-server` 与 `user-web` 业务源码 **0 改动**；非本项目端口 `8000/443/8188/3333` **0 触碰**；Windows PowerShell 管道 **0 裸中文乱码**）。

---

## 一、 🎼 专家团多智能体编排报告（Orchestration Report — Stage 0）

### 1.1 任务目标与编排模式
- **阶段目标**：完成【Stage 0：环境底座初始化、2×2 部署形态快速核验与「双租户三角色黄金测试夹具」造数】，为后续 Stage 1（实体四关审查）、Stage 2（注释清理）、Stage 3（API 四维 `curl` 实测）、Stage 4（跨模块长链路与非 HTTP 隔离）、Stage 5（UI 拟人点击测试）提供完备、隔离、可复用的运行环境与 Token 夹具底座。
- **编排模式**：`Coordinator / Orchestration Mode`（主控 `@orchestrator` + 3 个并行专项子智能体 + 独立复核验真）。

### 1.2 派发子智能体清单（Agents Invoked）

| # | 子智能体名称 | 会话追踪链接 | 联合应用专家规范 | 核心职责与交付物 | 状态 |
| :---: | :--- | :--- | :--- | :--- | :---: |
| **1** | `stage0-gate-auditor` | [查看子智能体记录](conversation://c744d778-0c50-4c67-b125-1f9293725b29) | `@backend-specialist`<br>`@database-architect` | 1. 使用 Git Bash 执行 [`check-architecture.sh`](file:///e:/HiveMtk/hivemtk-master/scripts/check-architecture.sh)（10 项五层架构门禁）<br>2. 执行 [`check-gorm-only.sh`](file:///e:/HiveMtk/hivemtk-master/user-server/scripts/check-gorm-only.sh)（GORM-only 裸 SQL 门禁）<br>3. 执行 [`audit_api_contract.py`](file:///e:/HiveMtk/hivemtk-master/scripts/audit_api_contract.py)（前后端 1,063 处 API 调用双向契约审计）<br>4. 盘点 [`model-list.md`](file:///e:/HiveMtk/hivemtk-master/model-list.md) 与 [`docs/api-test-progress.md`](file:///e:/HiveMtk/hivemtk-master/docs/api-test-progress.md) 初始基线计数 | ✅ 已完成 |
| **2** | `stage0-devops-inspector` | [查看子智能体记录](conversation://1242fc10-b185-4cf5-9ae2-2528a499a5de) | `@devops-engineer` | 1. 勘探 `:8202`（Postgres）、`:8203`（Redis）、`:8204`（SaaS 实例）、`:8304`（独立部署实例）、`:8211`（前端）、`:8208`（Embedding）、`:8209`（Reranker）监听状态与 PID<br>2. 核验 [`user-server.exe`](file:///e:/HiveMtk/hivemtk-master/user-server/bin/user-server.exe) 编译时间戳与最新 Commit `07a7c025` 一致性<br>3. 配合主控完成 `:8204` (`TENANT_MODE=on`) 与 `:8304` (`TENANT_MODE=off`) 双实例无黑框后台拉起与 `/health` 四维全绿核验 | ✅ 已完成 |
| **3** | `stage0-security-inspector` | [查看子智能体记录](conversation://6de5b679-4567-4ecc-ad5c-600241590139) | `@security-auditor`<br>`@test-engineer` | 1. 深度审计 [`platform_operator.go`](file:///e:/HiveMtk/hivemtk-master/user-server/internal/middleware/platform_operator.go) 平台运营方守卫与 `tenantAdmin` 路由分档逻辑<br>2. 排查发现初始 `tenants` 表为空的潜在碰撞风险并完成 `tenant_id=1` 平台根租户锚定<br>3. 自助注册创建租户 A (`id=2, t67z9tq`)、租户 B (`id=3, tytmh69`) 及子账号 `qa_a_user`，激活全部 24 个商业模块<br>4. 签发并存盘 5 枚黄金测试 Token，完成平台超管 vs 租户超管 3 项边界实测 | ✅ 已完成 |

### 1.3 验证脚本执行汇总（Verification Scripts Executed）
- [x] `bash scripts/check-architecture.sh` $\rightarrow$ **Exit Code `1`**（检出 1 处基线超限 `BUG-S0-001`：`ai_agent.go` 取 DB 句柄 `7 > 5`）
- [x] `bash user-server/scripts/check-gorm-only.sh` $\rightarrow$ **Exit Code `0`（PASS）**
- [x] `python -X utf8 scripts/audit_api_contract.py` $\rightarrow$ **Exit Code `0`（PASS，`UNMATCHED=0 / 1063`）**
- [x] `python .agents/skills/vulnerability-scanner/scripts/security_scan.py E:\HiveMtk\hivemtk-master --output summary` $\rightarrow$ **Exit Code `0`（Supply chain `[OK]`）**

---

## 二、 第一步：代码与门禁基线体检完整实证

### 2.1 Git 仓库基线状态与最近 5 条提交
- **工作目录**：`E:\HiveMtk\hivemtk-master`（NTFS Junction 指向 `D:\HiveMtk\hivemtk-master`）
- **`git status -s` 输出**：
  ```text
   M docs/api-test-progress.md
  ```
  *(注：初始状态为空，Stage 0 完成后仅更新了测试进度台账 `docs/api-test-progress.md`，Go 与 Vue 业务源码改动为 `0`。)*
- **最新 5 条 Commit (`git log -n 5 --oneline`)**：
  ```text
  07a7c025 fix(router): fix setupFrontendAliases parameter count mismatch from upstream 537d1398
  609e4075 fix(seed): resolve message_hub and inbox garbled question mark characters via UTF-8 patch and archive forensic report
  8d63a165 fix(local): initialize CustomerServiceAgentServiceViaPort repos, merge inbox messages by conversation_id, and fix dashboard SQL group by
  8ed0bdd1 chore(local): patch 3 unmounted RBAC routes, configure Option 2B DeepSeek + local CPU bge-m3, remove leaked secret backup
  537d1398 fix(tenancy): 平台运营方守卫按数据域分档——修上一轮"收紧过头"误伤租户内管理功能
  ```

### 2.2 容器数据库真实凭据核验
通过 `docker inspect mtk-postgres` 实测提取正在运行的 PostgreSQL 容器（端口 `127.0.0.1:8202->8202/tcp`）环境变量：
```text
POSTGRES_USER=admin
POSTGRES_PASSWORD=62878d01386e1bec46379d0c8d226719
POSTGRES_DB=user_db
```

### 2.3 三大静态架构与契约门禁脚本原始输出

#### （1）五层架构门禁：`bash scripts/check-architecture.sh`（Exit Code: `1`）
```text
============================================================
  五层架构合规检查
  目标: user-server
  规范: GO_FIVE_LAYER_ARCHITECTURE.md
============================================================

[1/10] Controller 反向依赖检查...
✅ [L3] controller 未直接引用 repository
✅ [L3] controller 未直接调 db
✅ [L3] controller 未直接引用 gorm
✅ [L3] controller 使用 response.Success/Error 统一响应

[2/10] Service 反向依赖检查...
✅ [L4] service 未引用 controller/router
✅ [L4] service 未直接调 db
⚠️  [L4] 以下 service struct 持有 *gorm.DB 字段(应重构为 Repository 注入):
    /e/HiveMtk/hivemtk-master/user-server/internal/service/customer_session_cron.go:17:	db         *gorm.DB
    /e/HiveMtk/hivemtk-master/user-server/internal/service/message_hub_summary_agg.go:19:	db        *gorm.DB
⚠️  [L4] 参考: 架构文档 GO_FIVE_LAYER_ARCHITECTURE.md §三 L3 职责
❌ [L4] service 取 DB 句柄宽口径与基线不符（新增/超出/条目失效）
    超出: user-server/internal/service/ai_agent.go 当前 7 > 基线 5
✅ [L4] service 未含 SQL 字符串拼接

[3/10] Repository 反向依赖检查...
✅ [L5] repository 未引用 service
✅ [L5] repository 未引用 dto

[4/10] Model 业务方法检查...
✅ [Model] 无业务方法(仅 GORM Hook/TableName)

[5/10] DTO 反向依赖检查...
✅ [DTO] dto 未引用 service/repository
✅ [DTO] dto 未直接调 db
✅ [DTO] 无业务方法(仅 Value/Scan)

[6/10] 文件命名规范检查...
✅ [命名] 无版本/扩展/日期后缀
✅ [命名] 无通用名文件
✅ [命名] controller/service/repository/dto 无冗余后缀
✅ [命名] 无源文件误命名为 _test.go

[7/10] Service interface 规范检查...
✅ [Service] 所有 service.go 都遵循 interface+struct 模式

[8/10] Repository interface 规范检查...
✅ [Repository] 所有 repository.go 都遵循 interface+struct 模式

[9/10] 上下文透传检查(抽查)...
✅ [Repository] 所有导出方法都含 ctx context.Context

[10/10] Config 包位置检查...
✅ [Config] config 包位置合规（不在 internal/pkg/utils/ 下）
✅ [Config] config 包未分裂

============================================================
❌ 架构检查发现 1 个错误,请修复后重新提交
============================================================
```

#### （2）GORM-only 裸 SQL 门禁：`bash user-server/scripts/check-gorm-only.sh`（Exit Code: `0`）
```text
[gorm-only] ✓ 业务包 0 处 .Exec( / .Raw(（migration DDL 与 *_test.go 已豁免）
```

#### （3）前后端 API 契约双向审计：`python -X utf8 scripts/audit_api_contract.py`（Exit Code: `0`）
```text
file-scope 幻影前缀重复捕获已剔除: 1750 条
backend routes: 1754, unresolved: 1
   UNRESOLVED: ('GET', '<UNRESOLVED:engine>/ws/chat')

=== UNMATCHED (frontend -> no backend): 0 / 1063 ===

=== SUSPECT (字面量路径只被 :参数 接住，非阻断): 0 ===

=== BACKEND NOT CALLED BY FRONTEND (candidates): 575 ===
```

### 2.4 核心作战台账基线盘点
1. **[`model-list.md`](file:///e:/HiveMtk/hivemtk-master/model-list.md)**：共 **205 个 Model 文件 / 346 张表声明**；已完成 `[x]` **18 个**（`#1`～`#18`），待审查 `[ ]` **187 个**（`#19` `internal/content/model/script_template.go` ～ `#205` `internal/service/email.go`）。
2. **[`go-files-list.md`](file:///e:/HiveMtk/hivemtk-master/go-files-list.md)**：共 **1,939 个非测试 Go 源文件**（含 19,347 个注释块 / 47,560 行注释）。
3. **[`docs/api-list.md`](file:///e:/HiveMtk/hivemtk-master/docs/api-list.md) & [`docs/api-test-progress.md`](file:///e:/HiveMtk/hivemtk-master/docs/api-test-progress.md)**：共 **110/111 个前端 API 文件 / 1,028 处调用点 / 993 条唯一路由**；已完成 **0 个**，已登记问题 **5 项**（含本轮 Stage 0 新增定位的 `#3`、`#4`、`#5`）。
4. **[`docs/router-list.md`](file:///e:/HiveMtk/hivemtk-master/docs/router-list.md)**：共 **82 个模块文件 / 235 条前端页面路由**。

---

## 三、 第二步：双实例并跑启动与 2×2 部署形态核验完整实证

### 3.1 环境底座校准说明（仅测试库配置校准，零改源码）
在启动双实例前，`stage0-devops-inspector` 与 `stage0-security-inspector` 排查并校准了以下 2 项测试库初始状态：
1. **校准 LLM Failover 健康检查路径**：测试库 `config_params` 表中 `key = 'provider_failover_policy'` 原存 `param_value` 含损坏的反斜杠转义（`{" config\:{\health_check_interval\:30...`），导致 `ProviderFailover.LoadPolicy` 解析 JSON 失败回退至默认 `/health`（DeepSeek API `/v1/health` 返 404，致使 `/health` 报 `50301 service degraded`）。通过 PostgreSQL `jsonb_set` 将 `config.health_check_path` 校准为合法的 `"/models"` 后，`/health` 的 `inference` 检查恢复为 `"up"`，整体状态恢复为 `code: 0, status: "ok"`。
2. **锚定平台运营方根租户 (`tenant_id = 1`)**：测试库原在 `TENANT_MODE=off` 下初始化，`tenants` 表为空（0 行）。若直接切到 `TENANT_MODE=on` 注册新租户，新租户将占用 `id=1` 并被 `PlatformOperatorMiddleware`（取 `MIN(id)`）误判为平台运营方。因此先在 `tenants` 表锚定 `id=1`（`code='local-hivemtk01'`, `name='HiveMtk Platform Root'`）并推进 `tenants_id_seq`，同时为 `tenant_id=1` 激活全部 24 个商业模块（含 `geo` 与 `browser-automation`）。

### 3.2 全栈端口与进程运行状态表

| 端口 | 实例角色与部署形态 | 运行状态 | PID / 容器名 | 启动命令 / 日志路径 |
| :--- | :--- | :---: | :--- | :--- |
| **`:8204`** | **SaaS 多租户实例**<br>(`TENANT_MODE=on`, `PLATFORM_ENABLED=false`) | 🟢 **RUNNING** | PID `18560`<br>(`user-server.exe`) | `PORT=8204 TENANT_MODE=on APP_ENV=development`<br>日志：`D:\HiveMtk\persistent_data\logs\user-server-8204-saas.log` |
| **`:8304`** | **独立部署单机实例**<br>(`TENANT_MODE=off`, `PLATFORM_ENABLED=false`) | 🟢 **RUNNING** | PID `17244`<br>(`user-server.exe`) | `PORT=8304 TENANT_MODE=off APP_ENV=development`<br>日志：`D:\HiveMtk\persistent_data\logs\user-server-8304-standalone.log` |
| **`:8211`** | **前端 `user-web` 服务** | 🟢 **RUNNING** | PID `20764`<br>(`node.exe`) | `"C:\Program Files\nodejs\node.exe" "D:\HiveMtk\persistent_data\serve-web.mjs"` |
| **`:8202`** | **PostgreSQL 15 + pgvector** | 🟢 **HEALTHY** | `mtk-postgres`<br>(PID `19232`) | `127.0.0.1:8202->8202/tcp` (`user_db`) |
| **`:8203`** | **Redis 7** | 🟢 **HEALTHY** | `mtk-redis`<br>(PID `19232`) | `127.0.0.1:8203->8203/tcp` |
| **`:8208`** | **本地 CPU Embedding (`bge-m3`)** | 🟢 **RUNNING** | PID `2808`<br>(`llama-server.exe`) | `127.0.0.1:8208` (`--embeddings --pooling mean --alias bge-m3`) |
| **`:8209`** | **本地 CPU Reranker (`bge-reranker-v2-m3`)** | 🟢 **RUNNING** | PID `20056`<br>(`llama-server.exe`) | `127.0.0.1:8209` (`--reranking --alias bge-reranker-v2-m3`) |

### 3.3 双实例 5 项核心探针实测结果

| # | 验证端点 | 预期行为 | 实测 HTTP 状态码 | 实测响应 JSON 原文 | 结果 |
| :---: | :--- | :--- | :---: | :--- | :---: |
| **1** | `GET http://127.0.0.1:8204/health` | 返回 200，`status: "ok"` | **`200`** | `{"code":0,"message":"ok","data":{"checks":{"database":{"error":"","status":"ok"},"embedding":{"error":"","status":"up"},"inference":{"error":"","status":"up"},"redis":{"error":"","status":"ok"}},"service":"user-server","status":"ok","timestamp":1792566838}}` | ✅ |
| **2** | `GET http://127.0.0.1:8204/api/public/config` | `tenant_mode: true, edition: "saas"` | **`200`** | `{"code":0,"message":"ok","data":{"cloud_url":"https://work.xapptool.cn","edition":"saas","enable_register":false,"site_name":"","tenant_mode":true}}` | ✅ |
| **3** | `GET http://127.0.0.1:8304/health` | 返回 200，`status: "ok"` | **`200`** | `{"code":0,"message":"ok","data":{"checks":{"database":{"error":"","status":"ok"},"embedding":{"error":"","status":"up"},"inference":{"error":"","status":"up"},"redis":{"error":"","status":"ok"}},"service":"user-server","status":"ok","timestamp":1792566838}}` | ✅ |
| **4** | `GET http://127.0.0.1:8304/api/public/config` | `tenant_mode: false, edition: "opensource"` | **`200`** | `{"code":0,"message":"ok","data":{"cloud_url":"https://work.xapptool.cn","edition":"opensource","enable_register":true,"site_name":"","tenant_mode":false}}` | ✅ |
| **5** | `POST http://127.0.0.1:8304/api/tenant/register` | 拦截并返回业务码 `4104` | **`200`** | `{"code":4104,"message":"当前为离线部署模式，注册未开放","data":{}}` | ✅ |

---

## 四、 第三步 & 第四步：「双租户三角色黄金测试夹具」与【Stage 0 底座验收报告卡】

### 4.1 租户 A 与租户 B 开户、子账号创建与 24 模块激活实证

1. **租户 A（主测租户）自助注册 (`POST http://127.0.0.1:8204/api/tenant/register`)**：
   - 签名参数：`name="QA Tenant A"`, `username="qa_a_admin"`, `email="qa_a_admin@qa.example"`, `license_no="QA-TENANT-A-001"`
   - 响应（HTTP `200`）：`{"code":0,"message":"注册成功","data":{"tenant_id":2,"tenant_code":"t67z9tq","token":"..."}}`
   - 商业模块全激活：在 `tenant_modules` 中为 `tenant_id=2` 激活全部 **24 个商业模块**（7 个 `base` + 17 个 `paid`，含 `browser-automation` 与 `geo`），并调用 `POST /api/modules/sync` 刷新内存门禁缓存（HTTP `200`）。
2. **租户 A 普通员工子账号创建 (`POST http://127.0.0.1:8204/api/users`)**：
   - 使用租户 A 管理员 Token 调用，创建 `username="qa_a_user"`, `email="qa_a_user@qa.example"`, `role="user"`, `status=1`
   - 响应（HTTP `200`）：`{"code":0,"message":"创建用户成功","data":{"id":12,"username":"qa_a_user","email":"qa_a_user@qa.example","role":"user","status":1,...}}`
3. **租户 B（越权对照明哨租户）自助注册 (`POST http://127.0.0.1:8204/api/tenant/register`)**：
   - 签名参数：`name="QA Tenant B"`, `username="qa_b_admin"`, `email="qa_b_admin@qa.example"`, `license_no="QA-TENANT-B-001"`
   - 响应（HTTP `200`）：`{"code":0,"message":"注册成功","data":{"tenant_id":3,"tenant_code":"tytmh69","token":"..."}}`
   - 开户事务与 Starter-Kit 物理落盘核验（`mtk-postgres` `user_db` 实查）：
     - `tenants` 表：生成 `id=3, code='tytmh69', name='QA Tenant B', license_no='QA-TENANT-B-001', status=1`
     - `system_users` 表：生成 `id=13, username='qa_b_admin', role='admin', data_scope='all', tenant_id=3`
     - `system_config` 表：生成 `id=2, tenant_id=3, name='QA Tenant B'`
     - `ai_agents` 表：生成 `id=14, tenant_id=3, agent_code='default_sales', name='默认销售智能体'`
     - `asset_bundles` 表：生成 `id=18, tenant_id=3, asset_id='starter-bundle', title='起步资产包', status='active'`
     - `tenant_modules` 表：开户同步生成 7 个基础模块行，并已为其补齐全部 24 个激活模块（`active_count: 72`，即租户 1/2/3 各 24 个模块），确保后续 Stage 3 跨租户攻击测试直达业务 Controller/Service/Repo 层而不被模块门禁提前挡在 4301。

### 4.2 黄金测试夹具 Token 存盘总表

| Token 标识 | 适用实例与模式 | 账号名 (`username`) | 登录邮箱 (`email`) | 密码 | 角色 (`role`) | `user_id` | `tenant_id` | `tenant_code` | Token 物理存盘路径 |
| :--- | :--- | :--- | :--- | :--- | :---: | :---: | :---: | :--- | :--- |
| **`TOKEN_A1`** | `:8204` (`TENANT_MODE=on`) | `qa_a_admin` | `qa_a_admin@qa.example` | `QaPassword@123` | `admin` | **`11`** | **`2`** | `t67z9tq` | `D:\HiveMtk\persistent_data\qa_tokens\TOKEN_A1.txt` |
| **`TOKEN_A2`** | `:8204` (`TENANT_MODE=on`) | `qa_a_user` | `qa_a_user@qa.example` | `QaPassword@123` | `user` | **`12`** | **`2`** | `t67z9tq` | `D:\HiveMtk\persistent_data\qa_tokens\TOKEN_A2.txt` |
| **`TOKEN_B1`** | `:8204` (`TENANT_MODE=on`) | `qa_b_admin` | `qa_b_admin@qa.example` | `QaPassword@123` | `admin` | **`13`** | **`3`** | `tytmh69` | `D:\HiveMtk\persistent_data\qa_tokens\TOKEN_B1.txt` |
| **`TOKEN_OFF`** | `:8304` (`TENANT_MODE=off`) | `admin` | `admin@hivemtk.demo` | `HiveMtk@90d84637f1ce` | `admin` | **`1`** | **`1`** | `""` | `D:\HiveMtk\persistent_data\qa_tokens\TOKEN_OFF.txt` |
| **`TOKEN_ROOT_ON`** | `:8204` (`TENANT_MODE=on`) | `admin` | `admin@hivemtk.demo` | `HiveMtk@90d84637f1ce` | `admin` | **`1`** | **`1`** | `local-hivemtk01` | `D:\HiveMtk\persistent_data\qa_tokens\TOKEN_ROOT_ON.txt` |

- **Token 元数据汇总 JSON**：`D:\HiveMtk\persistent_data\qa_tokens\tokens_meta.json`
- **进度台账同步**：已写入 [`E:\HiveMtk\hivemtk-master\docs\api-test-progress.md`](file:///e:/HiveMtk/hivemtk-master/docs/api-test-progress.md#L18-L28)。

### 4.3 平台运营方守卫 (`PlatformOperatorMiddleware`) vs 租户内超管 (`tenantAdmin`) 边界实测

#### （1）首个租户 (`tenant_id=1`) 管理员访问 `GET /api/manage/tenants` $\rightarrow$ **HTTP `200 OK`**
```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "list": [
      {
        "id": 3,
        "code": "tytmh69",
        "name": "QA Tenant B",
        "status": 1,
        "plan": "free",
        "company_name": "QA Tenant B Co., Ltd.",
        "legal_person": "QA Admin B",
        "phone": "13800138002",
        "email": "qa_b_admin@qa.example",
        "license_no": "QA-TENANT-B-001",
        "reg_mode": "saas",
        "platform_sync": "skipped",
        "activity": { "member_count": 1, "last_active_at": "2026-10-21T15:20:50.547356+08:00" }
      },
      {
        "id": 2,
        "code": "t67z9tq",
        "name": "QA Tenant A",
        "status": 1,
        "plan": "free",
        "company_name": "QA Tenant A Co., Ltd.",
        "legal_person": "QA Admin A",
        "phone": "13800138001",
        "email": "qa_a_admin@qa.example",
        "license_no": "QA-TENANT-A-001",
        "reg_mode": "saas",
        "platform_sync": "skipped",
        "activity": { "member_count": 2, "last_active_at": "2026-10-21T15:20:50.449414+08:00" }
      },
      {
        "id": 1,
        "code": "local-hivemtk01",
        "name": "HiveMtk Platform Root",
        "status": 1,
        "plan": "enterprise",
        "company_name": "HiveMtk Platform Root",
        "legal_person": "PlatformAdmin",
        "phone": "13800000001",
        "email": "admin@hivemtk.demo",
        "license_no": "LOCAL-ROOT-001",
        "reg_mode": "local",
        "platform_sync": "skipped",
        "activity": { "member_count": 9, "last_active_at": "2026-10-21T15:20:50.73634+08:00" }
      }
    ],
    "total": 3
  }
}
```

#### （2）租户 B (`tenant_id=3`) 管理员 `TOKEN_B1` 越权访问 `GET /api/manage/tenants` $\rightarrow$ **HTTP `403 Forbidden`**
```json
{
  "code": 403,
  "message": "权限不足，该操作仅平台运营方可执行"
}
```

#### （3）租户 B (`tenant_id=3`) 管理员 `TOKEN_B1` 访问本租户管理配置 `GET /api/system/config` $\rightarrow$ **HTTP `200 OK`**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": 2,
    "tenant_id": 3,
    "site_name": "QA Tenant B",
    "website_url": "",
    "logo_url": "",
    "theme_color": "#409EFF",
    "seo_keywords": "",
    "seo_description": "",
    "service_phone": "",
    "service_email": "",
    "icp_record": "",
    "police_record": "",
    "enable_register": true,
    "enable_email_marketing": true,
    "enable_rag": true,
    "maintenance_mode": false,
    "max_users": 0,
    "max_upload_size_mb": 50
  }
}
```

---

## 五、 🐛 Stage 0 发现缺陷汇总与源码级根因溯源报告（仅记台账，待汇总审批后统一修复）

> 以下 3 项缺陷均已在 [`docs/api-test-progress.md`](file:///e:/HiveMtk/hivemtk-master/docs/api-test-progress.md#L55-L61) 台账中正式立档（编号 `#3`、`#4`、`#5`），当前严格遵循指令**不改动任何业务源码**。

---

### 🐛 缺陷记录卡 1 — `BUG-S0-001`（五层架构门禁基线超限）
- **台账登记号**：`docs/api-test-progress.md` 已发现问题 `#3`
- **触发入口**：静态架构门禁 `bash scripts/check-architecture.sh`（检查项 `[2/10] Service 反向依赖检查`）
- **故障现象**：
  - 脚本退出码 `1`，报错：
    `❌ [L4] service 取 DB 句柄宽口径与基线不符（新增/超出/条目失效） 超出: user-server/internal/service/ai_agent.go 当前 7 > 基线 5`
  - 同时伴随 2 条结构体持有 `*gorm.DB` 告警：
    - [`user-server/internal/service/customer_session_cron.go#L17`](file:///e:/HiveMtk/hivemtk-master/user-server/internal/service/customer_session_cron.go#L17)
    - [`user-server/internal/service/message_hub_summary_agg.go#L19`](file:///e:/HiveMtk/hivemtk-master/user-server/internal/service/message_hub_summary_agg.go#L19)
- **命中规范**：`GO_FIVE_LAYER_ARCHITECTURE.md` §三（Service 层禁止新增直接获取 `*gorm.DB` 句柄）
- **根因定位**：[`user-server/internal/service/ai_agent.go`](file:///e:/HiveMtk/hivemtk-master/user-server/internal/service/ai_agent.go) 中直接获取 DB 句柄的调用点为 7 处，超出了 [`scripts/check-architecture.sh`](file:///e:/HiveMtk/hivemtk-master/scripts/check-architecture.sh) 锁定的历史豁免基线 `5`。
- **拟定修复方案（待汇总审批后执行）**：
  - 在 Stage 1 审查 `ai_agent.go` 关联实体时，将 `user-server/internal/service/ai_agent.go` 中新增的直接 DB 调用下沉至 `repository/ai_agent.go`，使 `check-architecture.sh` 恢复 `Exit Code = 0`。

---

### 🐛 缺陷记录卡 2 — `BUG-S0-002`（登录风控异步协程丢租户导致 `login_events` 写库被拒）
- **台账登记号**：`docs/api-test-progress.md` 已发现问题 `#4`
- **触发入口**：`POST /api/auth/login`（在 `TENANT_MODE=on` 下任意租户账号登录时触发）
- **故障现象**：
  - HTTP 响应虽为 `200 登录成功`，但 `:8204` 后端日志每次登录均报 3 条错误/告警：
    1. `ERROR: 查询最近失败次数失败: [tenant] 表 login_events 需要租户上下文但 ctx 未绑定（query）；多租户模式下拒绝执行，请经中间件/TenantRunner/行自证提供租户`
    2. `ERROR: 写入 login_events 失败: [tenant] 表 login_events 需要租户上下文但 ctx 未绑定（create）；多租户模式下拒绝执行，请经中间件/TenantRunner/行自证提供租户`
    3. `WARN: [login-risk] 登录风险检测失败`
- **命中缺陷原语**：**原语 P1（断链 ctx）+ 原语 P2（异步丢租户）**
- **根因定位**：[`user-server/internal/controller/auth.go#L51-L107`](file:///e:/HiveMtk/hivemtk-master/user-server/internal/controller/auth.go#L51-L107)：
  1. `POST /api/auth/login` 挂载在公开路由组（未经过 `JWTAuthMiddleware` 与 `TenantMiddleware`），请求进入时 `ctx.Request.Context()` 中**没有任何租户绑定**。
  2. 第 58 行 `resp, err := c.authService.Login(ctx.Request.Context(), &req)` 内部完成账号校验并解析出了 `resp.TenantID`，但并未将 `TenantID` 写回 `ctx.Request.Context()`。
  3. 第 69 行调用 `c.recordLoginRiskAsync(ctx, req.Username, true, "")` 时，第 85 行执行 `tid, hasTenant := tenant.FromContext(ctx.Request.Context())` 恒返回 `0, false`，导致异步协程里的 `dctx` 始终是未绑定租户的裸 `context.Background()`，读写 `login_events` 与 `security_alerts` 表被 GORM 多租户 Callback 100% 拦截。
- **拟定修复方案（待汇总审批后执行）**：
  - 修改 [`user-server/internal/controller/auth.go`](file:///e:/HiveMtk/hivemtk-master/user-server/internal/controller/auth.go#L69-L98) 的 `recordLoginRiskAsync` 签名，显式接收 `tenantID uint`（登录成功传 `resp.TenantID`；登录失败若能从请求或账号解析租户则传入），在协程内当 `tenantID > 0` 时调用 `dctx = tenant.OnlyTenant(dctx, tenantID)`。

---

### 🐛 缺陷记录卡 3 — `BUG-S0-003`（`TENANT_MODE=on` + `PLATFORM_ENABLED=false` 下新注册租户异步模块同步落错默认租户 1）
- **台账登记号**：`docs/api-test-progress.md` 已发现问题 `#5`
- **触发入口**：`POST /api/tenant/register` $\rightarrow$ [`service.ProvisionTenantModules`](file:///e:/HiveMtk/hivemtk-master/user-server/internal/service/module_catalog.go#L742-L759)
- **故障现象**：
  - 在 `TENANT_MODE=on` 且 `PLATFORM_ENABLED=false`（本地多租户演练/私有云多租户模式）下，新注册租户（如租户 A `id=2`、租户 B `id=3`）在 `tenant_modules` 表中仅生成了 7 个 `base` 模块行；后台协程触发的 `NewModuleCatalogService().Sync(c)` 未能将 16 个 0 元免费赠送模块（`source='gift'`）写入新租户名下，而是重复查/写了 `tenant_id=1`。
- **命中缺陷原语**：**原语 P1/P2（`WithSystemScope` 覆盖了调用方显式绑定的 `OnlyTenant` 上下文）**
- **根因定位**：[`user-server/internal/service/module_catalog.go#L355-L368`](file:///e:/HiveMtk/hivemtk-master/user-server/internal/service/module_catalog.go#L355-L368) 与 [`#L686-L733`](file:///e:/HiveMtk/hivemtk-master/user-server/internal/service/module_catalog.go#L686-L733)：
  1. `ProvisionTenantModules`（第 753 行）专门构造了 `bg := tenant.OnlyTenant(context.Background(), tenantID)` 传入 `NewModuleCatalogService().Sync(c)`。
  2. 但 `Sync` 在第 355 行读取 `reqTenantID, reqTenantBound := tenant.FromContext(ctx)` 后，在第 364 行无条件执行了 `ctx = tenant.WithSystemScope(ctx)`，紧接着在第 366 行 `if !config.PlatformEnabled() { return s.bootstrapOffline(ctx, start) }` 把 `SystemScope` 传给了 `bootstrapOffline`。
  3. 在 `SystemScope` 下，`s.repo.ListAll(ctx)` 查出的是全表所有行（因 `tenant_id=1` 已有全部 24 个模块，`have[r.ModuleKey]` 全部命中，`missing` 为 0），且即使写入也会因 GORM 默认值落入 `tenant_id=1`，导致新租户 `reqTenantID` 永远拿不到 `offlineGiftModuleRows`。
- **拟定修复方案（待汇总审批后执行）**：
  - 在 `Sync` 的 `!config.PlatformEnabled()` 分支（或 `bootstrapOffline` 内部），当 `reqTenantBound && reqTenantID > 0` 时，使用 `tenant.OnlyTenant(ctx, reqTenantID)` 作用域执行 `bootstrapOffline`，确保新租户在无平台连接时也能幂等获得 0 元赠送模块集。

---

## 六、 Stage 0 放行检查点签字单与下一阶段衔接指南

### 6.1 Stage 0 放行检查点核对表

| 检查项 | 放行标准 | 实测结果 | 状态 |
| :--- | :--- | :--- | :---: |
| **1. 双实例在线与健康检查** | `:8204` (`tenant_mode: true`) 与 `:8304` (`tenant_mode: false`) 均在线且 `/health` 返回 200 `status: "ok"` | `:8204` (PID `18560`) 与 `:8304` (PID `17244`) 均返回 200 `status: "ok"` | ✅ 通过 |
| **2. 独立版注册门禁拦截** | `:8304` 调用 `POST /api/tenant/register` 必须返回业务码 `4104` | 实测返回 `{"code":4104,"message":"当前为离线部署模式，注册未开放"}` | ✅ 通过 |
| **3. 黄金夹具 Token 齐备** | 获取并持久化 `TOKEN_A1`、`TOKEN_A2`、`TOKEN_B1`、`TOKEN_OFF`（外加平台根超管 `TOKEN_ROOT_ON`），`tenant_id` 清晰区分 | 5 枚 Token 已落盘至 `D:\HiveMtk\persistent_data\qa_tokens\`，分别对应 `tenant_id` `2` / `2` / `3` / `1` / `1` | ✅ 通过 |
| **4. 平台超管 vs 租户超管分档** | 首个租户调 `/api/manage/tenants` 返 `200`；租户 B 调 `/api/manage/tenants` 返 `403`；租户 B 调 `/api/system/config` 返 `200` | 三项实测状态码分别为 `200`、`403`、`200`，响应体与数据库物理行完全对齐 | ✅ 通过 |
| **5. 零源码改动与台账落盘** | 不改动 Go/Vue 业务源码，仅更新测试进度台账并汇总本批次发现缺陷 | `git status -s` 仅 `M docs/api-test-progress.md`；3 项缺陷已完整溯源归档 | ✅ 通过 |

### 6.2 下一阶段执行衔接建议
Stage 0 环境底座与黄金测试夹具已 100% 就绪。按照 [`saas-multitenant-master-test-plan.md`](file:///e:/HiveMtk/saas-multitenant-master-test-plan.md) 流水线，您可随时下达下一阶段指令（继续保持“只审查/测试并记录台账与缺陷报告、不改业务源码”或按需授权）：
- **推进 Stage 1（任务一：五层架构与多租户实体四关审查）**：从 [`model-list.md`](file:///e:/HiveMtk/hivemtk-master/model-list.md) 的 **`#19` (`internal/content/model/script_template.go`)** 开始逐批审查；
- **或推进 Stage 3（任务三：API 双实例 × 双租户交叉 `curl` 实测）**：直接复用本阶段生成的 `TOKEN_A1` / `TOKEN_A2` / `TOKEN_B1` / `TOKEN_OFF`，按 6 层业务拓扑顺序从 L1 底座层模块开始实测。
