# HiveMtk 故障排查手册

> **定位**：按「用户遇到的现象」组织。先在[快速索引表](#七快速索引表)找现象，再跳到对应条目。
> **事实基准**：所有端口、命令、变量名均与源码核对过；已知的系统限制如实标注，不粉饰。
> 配套文档：[部署运维手册](DEPLOYMENT_GUIDE.md) · [产品功能总览](marketing-features/README.md)

---

## 目录

1. [安装启动类](#一安装启动类)
2. [登录访问类](#二登录访问类)
3. [AI 功能类](#三ai-功能类)
4. [渠道接入类](#四渠道接入类)
5. [数据一致性类](#五数据一致性类)
6. [资源与前端类](#六资源与前端类)
7. [快速索引表](#七快速索引表)

---

## 一、安装启动类

### 1.1 启动即 panic：`panic: [SECURITY] USER_JWT_SECRET ...`

**症状**：user-server 启动秒退，stderr 含 `panic: [SECURITY] USER_JWT_SECRET 未配置或长度不足`。

**根因**：JWT 密钥缺失或短于 32 字符时，`loadJWTSecret` 主动 panic——这是防呆设计，不是 bug。

**修复**：

```bash
# .env 中设置（openssl rand -hex 32 生成）
JWT_SECRET=<64位随机hex>
```

**避免**：不要把测试专用值 `test-jwt-secret-do-not-use-in-prod-32+chars` 用到生产——它只在 test 模式放行。

### 1.2 后端起不来：PostgreSQL 连接被拒

**症状**：日志报 `dial tcp 127.0.0.1:8202: connect: connection refused`。

**根因排查顺序**：

1. 数据层容器没起来：`make db-ps` 查看 mtk-postgres 是否 healthy；
2. 容器启动失败：最常见是 `.env` 里 `POSTGRES_PASSWORD` 没改（缺密码容器直接退出），用 `make db logs` 看原因；
3. 端口写错：**宿主机侧永远是 8202**（不是 5432），检查 `.env` 的 `DB_PORT`。

**修复**：

```bash
make db-up && make db-ps          # 等 healthy
ss -tlnp | grep 8202              # 确认监听
psql -h 127.0.0.1 -p 8202 -U admin -d user_db -c "SELECT 1"
```

### 1.3 Redis 容器反复重启

**症状**：`docker ps -a` 显示 mtk-redis 反复 Restarting。

**根因**：`REDIS_PASSWORD` 未设置或过弱。本项目 docker-compose 对 Redis 有密码强校验。

**修复**：`.env` 设置强密码后 `make db-down && make db-up`。

### 1.4 初始化 SQL 报错或漏表

**症状**：执行 init 脚本报错；或启动后某些表不存在。

**事实说明**：

- 初始化脚本是 `migrations/init-user-db.sql`（启用 vector 扩展、建知识库核心表）；
- 其余业务表由 user-server 启动时的 GORM AutoMigrate 自动创建，**启动一次就补齐**；
- 项目中**没有** `./cmd/migrate` 这样的独立迁移命令，网上教程若让你跑它是错的；
- 编号脚本（001~055）仅在需要精确复现历史结构时手工按序执行，日常部署不需要。

**修复**：确认连接参数正确后重新执行：

```bash
PGPASSWORD=<POSTGRES_PASSWORD> psql -h 127.0.0.1 -p 8202 -U admin -d user_db \
  -f migrations/init-user-db.sql
```

### 1.5 模型下载失败 / llama-server 起不来

**症状**：`make inference-host-models` 中断；或 `inference-host-status` 显示某端口不通。

**根因**：HuggingFace 网络不通（国内需镜像）、磁盘不足、GGUF 文件下载不完整。

**修复**：

```bash
df -h                              # 模型目录预留 ≥10GB
make inference-host-models         # 支持断点续传，重跑即可
make inference-host-status         # 逐个确认 8207/8208/8209
```

### 1.6 前端构建 OOM：`JavaScript heap out of memory`

**症状**：`make web-build` 时 Node 进程被杀。

**修复**：

```bash
cd user-web
NODE_OPTIONS=--max-old-space-size=4096 npm run build
```

---

## 二、登录访问类

### 2.1 页面能开但接口全报 CORS 错误

**症状**：浏览器控制台大量 `blocked by CORS policy`；Network 面板请求无 `Access-Control-Allow-Origin` 响应头。

**根因**：前端 Origin 不在白名单。CORS 中间件是白名单模式，不会反射任意来源。

**修复**：`.env` 追加（逗号分隔，含协议和端口，重启生效）：

```bash
CORS_ALLOW_ORIGINS_USER="https://chat.example.com,https://admin.example.com"
```

**避免**：不要配 `*` 与携带凭证共存，浏览器会直接拒绝。

### 2.2 客服工作台 WebSocket 连不上（401/403）

**症状**：工作台实时消息不动；控制台 WS 连接立即关闭；服务端日志 `[ws upgrade failed]`。

**根因**（按概率排序）：

1. token 过期或未传——浏览器 WebSocket API 不能带自定义 header，token 必须走 query 参数；
2. query 里 `agent_id` 与 token 内 `user_id` 不一致（鉴权强制对齐）；
3. 反向代理层 未透传 Upgrade 头。

**修复**：

```js
const ws = new WebSocket(`wss://chat.example.com/ws?agent_id=${userId}&token=${token}`)
```

反向代理层 参考配置见[部署运维手册 §七 模式 B](DEPLOYMENT_GUIDE.md)（关键是 `/ws/` location 的 Upgrade 头与 86400s 读超时）。

### 2.3 登录后频繁被踢出

**根因**：多实例部署但 `JWT_SECRET` 不一致，或近期轮换过密钥使旧登录态全部失效。

**修复**：所有实例使用同一 `JWT_SECRET`；轮换密钥安排在低峰期并提前公告（所有人需重新登录）。

---

## 三、AI 功能类

### 3.1 客户发消息，AI 完全不回复

**排查链路**（自上而下，命中即停）：

| 步骤 | 检查 | 命令/位置 |
|------|------|----------|
| 1 | 推理栈活着吗 | `make inference-host-status`（8207/8208/8209 全绿才算活） |
| 2 | Embedding 单独通吗 | `curl http://127.0.0.1:8208/v1/models` |
| 3 | user-server 日志有推理报错吗 | `logs/user-server.log` 搜 `inference` 或 `llama` |
| 4 | 该场景配置了转人工/SOP 拦截吗 | 后台对应渠道与意图配置页 |

**常见根因**：推理栈根本没启动（第 4 步装完就忘了 `make inference-host-up`）；或机器重启后 llama-server 没有自启（当前无 systemd 单元，需要自行配置进程守护）。

### 3.2 AI 回复很慢，首次尤其慢

**事实**：

- 本地 CPU 推理（无 GPU）生成速度有限，属正常现象；
- llama-server 冷启动首个请求显著偏慢，属正常现象。

**缓解**：

```bash
make inference-host-warmup    # 启动后预热一次
```

长期方案：换 prod 档位更小量化模型、加 GPU，或在后台「LLM 路由」切换响应更快的提供商配置。

### 3.3 知识库检索结果为空或不相关

**排查顺序**：

1. Embedding 服务是否在线（8208）：向量化和检索都依赖它；
2. 文档是否真的导入成功：后台知识库页面看文档状态与 chunk 数量；
3. 维度是否匹配：全链路固定 **1024 维**（bge-m3 ↔ pgvector vector(1024)）。若你手动改过 embedding 模型导致维度不一致，检索会静默失败——改回 bge-m3 或重建 `knowledge_embeddings` 表。

**注意一个已知限制**：混合检索的短路逻辑在冷启动场景可能漏召回（详见官方文档已知限制 G9），表现为"刚导入的知识搜不到，过段时间又能搜到"。临时缓解：重启后跑一次预热查询。

### 3.4 该转人工的时候没转

**事实披露**：置信度判定存在已知缺陷——不同引擎的置信度口径不统一（G4）、异议处理置信度阈值硬编码（G8）。表现为：低置信消息偶发未触发转人工。

**临时缓解**：在 SOP 中对高风险意图显式配置"必转人工"规则，绕过置信度自动判定。

**根治**：等待置信度统一重构（路线图内，暂无时间承诺）。

### 3.5 新客户欢迎语没发出去

**事实披露**：greeting 规则当前不可达（已知限制 G3）——规则配置了也不会触发。

**临时替代**：用 SOP 的首条消息节点实现同等效果。

---

## 四、渠道接入类

### 4.1 Telegram / 飞书 / 钉钉收不到消息

**症状**：渠道显示已绑定，但访客消息进不来。

**根因**：被动回调渠道需要公网 HTTPS Webhook 地址。`PUBLIC_BASE_URL` 未设置时，系统拿不到真实公网地址（默认推导出 `localhost:8204`，平台无法投递）。

**修复**：

```bash
# .env 设置（https、不带路径、不带尾斜杠）
PUBLIC_BASE_URL=https://chat.example.com
```

重启后到渠道页重新保存一次绑定以触发 Webhook 注册。

**降级说明**：留空时这些渠道自动降级为轮询（polling）模式——能用，但禁止水平扩展，仅适合开发/单实例。

#### 4.1.1 Telegram webhook 真伪信号与验签链路（2026-09-08 实战）

**真伪区分三层机制**（按官方 core.telegram.org/bots/webhooks + Bot API）：

1. `secret_token`（主方案，已启用）：setWebhook 传入后 TG 每次推送带 `X-Telegram-Bot-Api-Secret-Token` 头；服务端与 `telegram_accounts.webhook_secret` 恒等比对（`webhook.go` Verify → telegram case）。⚠️ `.env` 的 `ALLOW_INSECURE_WEBHOOK=true` 会在 `WebhookService.Verify` 入口对**所有渠道**验签全局短路，生产必须删除（启动护栏只拦显式非 dev 环境，APP_ENV 未设=会被当 dev 放行）。
2. 来源 IP 白名单（纵深补充）：TG 源 IP 段 149.154.160.0/20 + 91.108.4.0/22（官方明示可能变化）。本部署 frp 隧道下应用层只见隧道出口 IP，白名单只能配在云端 nginx。
3. `update_id` 幂等：TG 对非 2xx 响应重复投递（update 保存 ≤24h）；服务端以 `tg_upd_<update_id>` 作 eventID 进 Redis SetNX 去重（仅 5 分钟 TTL，长窗口重发可能二次触发业务，DB 层 `uni_message_hub_platform_msg_conv` 唯一索引兜底插入）。

**云端 nginx 上白名单的宝塔陷阱（2026-09-08 事故复盘）**：宝塔主配置 `nginx.conf` 末尾是 `include /www/server/panel/vhost/nginx/*.conf;`（http 级通配）。若在 `/www/server/panel/vhost/nginx/` 目录下放只含 `allow/deny` 的片段文件（如做 include 复用），会被 http 级加载 → **全站所有 vhost 被 IP 白名单拦成 403**；且该文件 `nginx -t` 前已生成，`nginx -t` 测不出引用错误。事故当日已回滚。**规则**：allow/deny 片段文件只能放 `extension/<域名>/` 目录（该域名 vhost 内 include），或直接内联进 location；严禁落在 `vhost/nginx/*.conf` 通配范围内。

**验签失败排查**：`{"verify_failed":true,"reason":"missing header"}`=请求未带 secret 头（伪造或 TG 未收到 secret_token 注册）；`signature mismatch`=头存在但不匹配（secret 轮换后未重挂 webhook）。修复后用 `getWebhookInfo` 看 `last_error_message` 确认 TG 投递面恢复。

### 4.2 批量任务卡在 running 不动

**症状**：批量发送 job 长时间 `running` 且 `success + failed < total`。

**根因**：目标账号连接失效后资源未释放；或调度 goroutine 曾 panic（现已由 SafeGo 兜底，进程不会死但该 job 会停摆）。

**修复**：

```bash
grep -E 'whatsapp|panic' logs/user-server.log | tail -50
# 手动重置 job 让其续跑（先确认账号连接已恢复）
psql -h 127.0.0.1 -p 8202 -U admin -d user_db \
  -c "UPDATE whatsapp_jobs SET status='pending' WHERE id=<job_id>"
```

### 4.3 平台侧桥接通道行为异常（小红书/抖音等）

**症状**：桥接消息重复、延迟大、或掉线循环。

**排查**：

1. 出站通道模式：feature flag `sse_bridge` 默认开启（SSE），若网络中间层（部分反代/CDN）缓冲 SSE 流会导致延迟堆积，可设 `FF_SSE_BRIDGE=0` 回退长轮询验证；
2. flag 是热加载的（约 5 秒生效），改环境变量无需重启。

---

## 五、数据一致性类

### 5.1 定时任务/SOP 节点被重复执行

**症状**：同一 SOP 节点触发两次，客户收到重复消息。

**事实**：多实例并发取任务时若无行锁会重复派发。当前代码已启用 `SELECT ... FOR UPDATE SKIP LOCKED` 修复此问题。

**验证方法**：开两个 user-server 实例，观察日志 `[outbox] processed due timers` 的 fired_count 总和应等于数据库实际触发行数。若你仍在单实例部署且看到重复，优先怀疑上游渠道重推而非 Outbox。

### 5.2 表结构与代码对不上：`column does not exist` / `relation already exists`

**事实**：项目采用双轨制——GORM AutoMigrate（启动时自动）+ 编号 SQL 脚本（历史精确复现用）。两者幂等设计，正常情况不会冲突。

**冲突场景**：手工执行过编号脚本的一部分，又让 AutoMigrate 补齐，个别索引/约束声明两边不一致。

**修复思路**：

```sql
-- 看具体差异
\d <表名>
-- 多数情况下删掉冲突的手工对象，让下次启动的 AutoMigrate 统一重建
DROP INDEX IF EXISTS <冲突索引名>;
```

然后重启 user-server。**不存在 `USER_AUTO_MIGRATE=false` 这类开关**，AutoMigrate 无法关闭。

### 5.3 审计日志偶发缺失

**事实**：审计写入带指数退避重试（3 次）。极端情况下（DB 抖动/磁盘满）仍会丢，日志会留下证据。

**排查**：搜索服务端日志 `[audit] N 条审计日志在 3 次重试后仍写入失败`，按其指向的原因（连接池耗尽/磁盘满/死锁）处理。

### 5.4 误执行 DROP 类迁移导致数据丢失

**教训条目**（源自 025 号迁移的历史事故）：任何含 `DROP TABLE` 的手工迁移，执行前必须先备份：

```sql
CREATE TABLE system_users_backup_20260826 AS SELECT * FROM system_users;
```

冒烟验证通过后再执行 DROP。出事时从备份表 INSERT 还原。**这也是为什么[部署运维手册 §十](DEPLOYMENT_GUIDE.md)要求先有 pg_dump 再做任何结构性变更。**

---

## 六、资源与前端类

### 6.1 数据层容器被 OOM 杀掉（Exited 137）

**症状**：`docker ps -a` 显示 `Exited (137)`；内核日志含 `Memory cgroup out of memory`。

**事实**：mtk-postgres 限制 768M、mtk-redis 限制 512M（docker-compose.yml 明确设定）。数据量增长后 PG 可能顶到上限。

**修复**：编辑 docker-compose.yml 上调 limits 并 `make db-up` 重建容器。注意应用与推理跑在宿主机不受此限，宿主机整体内存规划见[部署运维手册 §二](DEPLOYMENT_GUIDE.md)。

### 6.2 浏览器端异常：localStorage 配额溢出

**症状**：登录态丢失、界面偏好（菜单折叠/主题）频繁重置；控制台 `QuotaExceededError`。

**修复**：清空该站点 localStorage 后重新登录。长期方案在路线图（i18n 包体懒加载）。

### 6.3 想接 Prometheus 但找不到 /metrics

**事实**：**本系统当前没有 `/metrics` 端点**，这不是故障。可用的健康端点只有三个：

```bash
curl http://127.0.0.1:8204/healthz    # 存活
curl http://127.0.0.1:8204/readyz     # 就绪
curl http://127.0.0.1:8204/health     # 含依赖详情
```

监控方案现状：指标定时落库到数据库表（bridge_metrics 等），通过 SQL 巡检趋势。Prometheus exporter 在路线图中，暂未实现。

### 6.4 想看决策细节但日志里什么都没有

**事实**：分层决策/RAG 查询的调试日志由 feature flag `debug_log` 控制，**生产默认关闭**。

**临时打开**（热加载，约 5 秒生效，无需重启）：

```bash
# user-server 进程环境中注入
FF_DEBUG_LOG=1     # 打开；改回 0 或删除即关闭
```

flag 家族一览（均为 `FF_<大写名>` 格式，默认关，`sse_bridge` 默认开）：`parallel` / `stream` / `layer1` / `fallback_chain` / `debug_log` / `sse_bridge`。

---

## 七、快速索引表

| 现象 | 条目 |
|------|------|
| 启动 panic 提到 SECURITY/JWT | 1.1 |
| 连不上库 / connection refused | 1.2 |
| Redis 容器反复重启 | 1.3 |
| 表缺失 / 初始化报错 | 1.4 |
| 模型下载失败 | 1.5 |
| npm build OOM | 1.6 |
| CORS 报错 | 2.1 |
| WebSocket 断开 401/403 | 2.2 |
| 频繁掉登录 | 2.3 |
| AI 不回复 | 3.1 |
| AI 回复慢 | 3.2 |
| 知识库检索空/不准 | 3.3 |
| 该转人工没转 | 3.4 |
| 欢迎语不触发 | 3.5 |
| Telegram/飞书/钉钉收不到消息 | 4.1 |
| 批量任务卡 running | 4.2 |
| 桥接消息重复/延迟 | 4.3 |
| SOP 节点重复执行 | 5.1 |
| column/relation 冲突 | 5.2 |
| 审计日志缺失 | 5.3 |
| DROP 迁移误删数据 | 5.4 |
| 容器 Exited(137) | 6.1 |
| localStorage 配额溢出 | 6.2 |
| /metrics 不存在 | 6.3 |
| 决策日志为空 | 6.4 |

---

## 附：本文档的使用边界

- 本手册只覆盖**已核实**的故障模式。文中标注"已知限制"的条目（3.4/3.5 及引用的 G 编号）属于产品现状而非故障，修不修以路线图为准；
- 遇到本手册未覆盖的现象：先收集三层证据再求助——`logs/user-server.log` 相关片段、`make inference-host-status` 输出、复现步骤；
- 不要依据网上第三方教程操作本项目：端口（8202/8203/8204 系列）、健康端点（无 /metrics）、迁移方式（无独立 migrate 命令）均与常见 Go 项目惯例不同，一律以本仓库源码为准。
