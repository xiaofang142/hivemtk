# Bridge 桥接系统企业运维手册 (Runbook)

> **版本:** 1.0
> **日期:** 2026-08-15
> **维护:** HiveMTK 运维组
> **适用范围:** HiveBridge Chrome 扩展（user-web/bridge）+ 桥接后端接口（user-server `/api/bridge/*`）+ 宿主机推理栈
> **配套文档:** [SLA/SLO](SLA_SLO.md) · [高可用部署](HA_DEPLOYMENT.md) · [灾难恢复](DR_RECOVERY.md) · [AI 智能体部署](AI_AGENT_PERF_DEPLOY.md)

---

## 0. 拓扑速览与告警入口

### 0.1 组件与端口

| 组件 | 位置 | 端口 / 协议 | 启停方式 |
|------|------|-------------|----------|
| user-server (Go) | `user-server/` | :8204 HTTP | `make dev`（air 热更新）/ systemd `user-server` |
| PostgreSQL 15+ | Docker | :8202 | `make db-up / db-down` |
| Redis 7 | Docker | :8203 | `make db-up / db-down` |
| LLM (llama-server) | 宿主机 | :8207/v1 | `make inference-host-up / down` |
| Embedding (TEI/llama) | 宿主机 | :8208/v1 | 同上 |
| Rerank | 宿主机 | :8209 | 同上 |
| HiveBridge 扩展 | 浏览器端 | 三通道 HTTP | 扩展管理页加载/刷新 |

### 0.2 桥接三通道协议（HiveBridge ↔ user-server）

| 通道 | 方向 | 说明 |
|------|------|------|
| uplink | 扩展 → 服务端 | 上报会话/消息（`X-Bridge-Token` 头） |
| outbox | 扩展 → 服务端 | 拉取待下发消息（长轮询） |
| ack | 扩展 → 服务端 | 确认已下发（`AckOutboundItem` 原子化 `UPDATE...RETURNING`） |

### 0.3 一键巡检命令

```bash
make inference-host-status   # 检查 8204/8207/8208/8209 四个端点连通性
make db-ps                   # 检查 PG + Redis 容器
bash scripts/bridge-monitor.sh   # 桥接健康巡检（如存在）
```

### 0.4 出站行的状态语义（读积压数字前先看这节）

`message_hub` 里 `direction='outbound'` 的每一行代表"服务端欠客户的一条回复"。状态含义与写入方：

| status | 含义 | 谁写 |
|--------|------|------|
| `pending` | 欠交付，等待被认领/补投 | 建行默认值 |
| `inflight` | 已被某次轮询认领，30s 租约内 | 三条取行路径的 CAS |
| `delivered` | 桥端 ack 已下发；直接投递渠道在发送 API 成功后由写侧结算 | ack 接口 / 各渠道 Send |
| `failed` | 终态，不再重推 | 两条自动结算路径（见下）或桥端主动 ack failed |
| `send_failed` | 渠道侧返回失败（走 trace 链路，不进队列） | `PushSendFailureTrace` |

两条**自动**把行判成 `failed` 的路径，靠 `push_error` 列区分：

| `push_error` | 触发条件 | 上界/阈值配置 |
|--------------|----------|----------------|
| `outbound_push_exhausted` | 账号还在拉，但同一条推 20 次仍失败 | 常量 `MaxOutboundPushAttempts`（代码内，无旋钮） |
| `outbound_orphan_expired` | 账号连续 `outbound_orphan_ttl` 未注册/未同步，且行本身也老于此阈值 | `bridge.outbound_orphan_ttl`（秒，缺省 604800＝7 天；0＝停用） |

两种判弃都**不写 `sent_at`**（这条从未交付出去，盖上时间戳会让后续回复被误判成"自己发过的回显"而吞掉）。

**孤儿结算默认只报数**：`bridge.outbound_orphan_dry_run` 缺省 `true`，后台回扫每轮只在日志里打印
`[BridgeReplay] 孤儿出站结算 dry-run（未写库）: ttl=… groups=… candidates=… skipped_reachable=…`，
一行都不改。放量步骤＝读着这行日志确认候选口径无误 → `config_params` 里把该键置 `false` → 下一轮真正落 `failed`。
为什么默认要停在报数侧：这道写入不可逆，而回扫是 5 分钟一跑的后台任务，开发态存盘即热重载进真实例、连真库
（2026-09-28 实测：闸门上线前的一轮 cron 把 38 行历史 pending 直接烧成 failed，事后已逐行还原）。

`unreachable_channels` 一格不等于"没人连 SSE"：轮询式下发的账号是靠 `last_sync_at` 落在宽限窗内才算在线的。

---

## 1. 故障一：服务起不来（user-server / 推理栈）

### 1.1 现象
- `curl http://127.0.0.1:8204/health` 返回非 200 或连接拒绝
- 页面 502 / 无法登录
- `make dev` 报错退出

### 1.2 根因（按概率排序）
1. `.env` 缺失或密钥不合法（`PLATFORM_JWT_SECRET` 少于 32 字符会被安全检查拦截）
2. 端口被占用（8204 / 8202 / 8203 / 8207-8209）
3. Go 依赖未下载 / 编译错误
4. 配置文件 `config.yaml` 语法错误

### 1.3 排查步骤
```bash
# 1. 端口占用
lsof -i :8204 -i :8202 -i :8203 -i :8207 -i :8208 -i :8209

# 2. 查看进程
ps aux | grep -E "user-server|air|llama-server" | grep -v grep

# 3. 检查 .env 是否存在、密钥长度
[ -f .env ] && echo ".env 存在" || echo ".env 缺失 → cp .env-example .env"
awk -F= '/PLATFORM_JWT_SECRET/ {print length($2)" 字符"}' .env

# 4. 查看服务日志（systemd 部署时）
journalctl -u user-server -n 100 --no-pager
```

### 1.4 恢复命令
```bash
# 方式 A：开发热更新
cd user-server && air          # 或 make dev

# 方式 B：systemd 生产部署
sudo systemctl restart user-server
sudo systemctl status user-server --no-pager

# 方式 C：Docker 全栈
make docker-up

# 数据层单独拉起（若 PG/Redis 未起）
make db-up
```

### 1.5 责任人
应用运维工程师（On-call A 角）；首次 15 分钟内响应。

---

## 2. 故障二：数据库断连（PostgreSQL / Redis）

### 2.1 现象
- 接口大面积 5xx，日志报 `connection refused` / `pq: could not connect`
- 桥接 uplink/ack 写入失败
- 登录超时

### 2.2 根因
1. 容器崩溃或 OOM（内存不足被杀）
2. 磁盘满导致 PG 无法写 WAL（见故障六）
3. 连接池耗尽（`max_open_conns` 过小）
4. Redis 密码变更后 `.env` 未同步

### 2.3 排查步骤
```bash
# 1. 容器状态
make db-ps

# 2. 容器日志（OOM / 报错）
make db-logs

# 3. PG 连通性
curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:8204/health

# 4. 连接数（连接池是否耗尽）
docker compose -f docker-compose.yml exec -T mtk-postgres \
  psql -U admin -d user_db -c \
  "SELECT count(*), state FROM pg_stat_activity GROUP BY state;"
```

### 2.4 恢复命令
```bash
# 1. 重启数据层
make db-down && make db-up
sleep 5

# 2. 验证
curl -s http://127.0.0.1:8204/health

# 3. 若连接池耗尽，调整 user-server/config.yaml：
#   database.max_open_conns: 50
#   database.max_idle_conns: 10
# 然后重启 user-server（见故障一）

# 4. Redis 密码校验（.env 与容器一致）
docker compose -f docker-compose.yml exec -T mtk-redis \
  redis-cli -a "$REDIS_PASSWORD" ping   # 期望 PONG
```

### 2.5 责任人
DBA / 运维工程师；数据库故障 P1 级 30 分钟恢复目标。

---

## 3. 故障三：LLM 不可用（推理栈掉线）

### 3.1 现象
- AI 回复超时 / 报错，桥接自动回复停滞
- 日志报 LLM 连接失败或 `slots_in_use` 打满
- `make inference-host-status` 显示 :8207 非 200

### 3.2 根因
1. llama-server 进程崩溃或内存不足被 OOM Killer 杀掉
2. 模型文件缺失 / 路径错误
3. 上下文槽位打满（并发过高）
4. GPU / CPU 资源被其他进程抢占

### 3.3 排查步骤
```bash
# 1. 端点健康
for p in 8207 8208 8209; do \
  echo "$p: $(curl -s -o /dev/null -w '%{http_code}' --max-time 3 http://127.0.0.1:$p/health)"; \
done

# 2. 进程
make inference-host-ps

# 3. 日志（LLM 推理日志，tail 观察报错）
make inference-host-logs

# 4. 槽位占用
curl -s http://127.0.0.1:8207/metrics | grep -E "slots_(idle|n_slots)" || true
```

### 3.4 恢复命令
```bash
# 1. 重启推理栈
make inference-host-down && make inference-host-up
sleep 5

# 2. 预热（避免首请求慢）
make inference-host-warmup

# 3. 端到端 smoke test
make inference-host-test

# 4. 降级策略：FeatureFlag 关闭并行/流式（见 AI_AGENT_PERF_DEPLOY.md 4.1）
export FF_PARALLEL=0 FF_STREAM=0 FF_LAYER1=0
sudo systemctl reload user-server   # SIGHUP 触发配置热加载
```

### 3.5 责任人
AI 平台工程师；LLM 故障 P1 级。

---

## 4. 故障四：桥接全渠道掉线（HiveBridge 失联）

### 4.1 现象
- 所有渠道（抖音/小红书/TikTok/闲鱼）扩展图标变灰，无自动回复
- user-server 日志无 uplink/outbox 心跳
- popup 健康度面板显示熔断（circuit-breaker open）

### 4.2 根因
1. 桥接 Token 失效 / 被吊销（`X-Bridge-Token` 头认证失败——401 有两种，原因在服务端响应体里：
   `缺少 X-Bridge-Token`＝头没发出去，`bridge token 无效`＝头发出去了但值不对，处置动作不同）
2. user-server `/api/bridge/*` 路由不可达（未开 / 被反代拦截）
3. 扩展侧死开关（Dead Man's Switch）触发，自动停摆
4. 账号被平台风控（高频巡检触发）

### 4.3 排查步骤
```bash
# 1. 桥接接口可达性
# 闸门只读 X-Bridge-Token（发 Authorization: Bearer 会 401，别误判成"服务没起来"）；
# outbox 还要求 channel + account_id，缺参是 400。200=通、400=通但少参、401=凭证不对、000=没起来。
curl -s -o /dev/null -w "%{http_code}" -H "X-Bridge-Token: <TOKEN>" \
  "http://127.0.0.1:8204/api/bridge/outbox?channel=douyin&account_id=<ACCOUNT_ID>"

# 2. 查看桥接相关日志
journalctl -u user-server --no-pager | grep -iE "bridge|uplink|outbox|ack" | tail -50

# 3. 检查扩展 popup 健康度面板：熔断状态 / 延迟 P50/P95 / 错码分布
# 4. 检查浏览器控制台：chrome://extensions → 背景页 Console
#    SSE 建连被拒时，控制台现在带服务端回的原因（`SSE 连接失败: HTTP 401 — bridge token 无效`），
#    不再只剩一个状态码数字。见 user-web/bridge/src/core/sse-fetch-client.js 的 buildSSEError。
#    4xx（408/429 除外）判为不可重试：循环会打一条
#    `SSE 被服务端拒绝（不可重试），降为 60s 慢重连`（downlink.js），
#    此后每 60s 才再试一次——这是预期降档，不是"卡住不动"，别按重连失败去重启服务。
#    在选项页把 Token 改对后不需要刷新页面，下一个 60s 试探自己会接上。

# 5. 检查 Token 是否过期：平台端账号管理 → 重置桥接 Token
```

### 4.4 恢复命令
```bash
# 1. 在平台端重置/重新颁发桥接 Token
# 2. 扩展 popup → 重新配置 Token → 保存（chrome.storage 热更新监听生效）
# 3. 若熔断器已 open：等待冷却窗口，或点击 popup「紧急停止」→ 重新「启动」
# 4. 若平台风控：调低巡检频率（30-60s 轮间隔 / 6 会话每轮 / 120s 会话冷却）
```

### 4.5 责任人
桥接运维工程师；全渠道掉线 P1 级 2 小时恢复目标。

---

## 5. 故障五：内存暴涨

### 5.1 现象
- 宿主机内存使用率 > 90%，`free -h` 显示接近耗尽
- 扩展侧页面卡顿、OOM（`oom-patrol` 相关报错）
- llama-server / user-server 被 OOM Killer 杀掉

### 5.2 根因
1. LLM 并发槽位过多 + 上下文窗口过大
2. 扩展侧 RateLimiter LRU 缓存 / `_pendingAck` 堆积（TTL 未生效）
3. 推理栈 dev/prod 模型档位与机器内存不匹配
4. 连接池 / 缓存无上限

### 5.3 排查步骤
```bash
# 1. 内存占用 Top 进程
ps aux --sort=-%mem | head -10

# 2. 推理栈模型档位（确认是否误用 prod 档）
cat scripts/inference-host/models.env

# 3. 扩展侧：popup 健康度面板查看请求堆积；后台日志 grep pendingAck
# 4. 检查 OOM 记录
dmesg | grep -i "out of memory" | tail -10
```

### 5.4 恢复命令
```bash
# 1. 立即回收：重启推理栈（释放 llama 内存）
make inference-host-down && make inference-host-up

# 2. 切换更小模型档（dev：Qwen2.5-3B Q4_K_M）
HIVEMTK_PROFILE=dev make inference-host-models
HIVEMTK_PROFILE=dev make inference-host-up

# 3. 收紧 LLM 参数（config.yaml）
#   inference.llm.max_tokens: 512
#   inference.llm.context_size: 2048

# 4. 扩展侧：等待 _pendingAck TTL（24h）自清理；必要时卸载重装扩展清空状态
```

### 5.5 责任人
运维工程师 + 前端工程师（扩展侧）；P2 级 4 小时。

---

## 6. 故障六：磁盘满

### 6.1 现象
- `df -h` 显示 `/` 使用率 100%
- PG 写 WAL 失败 → 数据库只读 / 拒绝连接
- 备份失败、日志无法写入

### 6.2 根因
1. 日志文件无限增长（`$HOME/.hivemtk/runtime/*.log`）
2. PG WAL 堆积（未开启归档清理）
3. 模型文件 / 备份文件占用
4. 扩展 `dist/` 构建产物累积

### 6.3 排查步骤
```bash
# 1. 磁盘使用率
df -h

# 2. 大目录定位
du -sh $HOME/.hivemtk/runtime user-server user-web/bridge/dist 2>/dev/null | sort -h

# 3. 日志大小
ls -lh $HOME/.hivemtk/runtime/*.log 2>/dev/null

# 4. PG 数据目录
docker compose -f docker-compose.yml exec -T mtk-postgres df -h /var/lib/postgresql
```

### 6.4 恢复命令
```bash
# 1. 清理旧日志（保留最近 7 天）
find $HOME/.hivemtk/runtime -name "*.log" -mtime +7 -delete

# 2. 清理旧备份（保留最近 30 天）
find . -maxdepth 1 -name "backup_*.sql" -mtime +30 -delete

# 3. 清理扩展构建产物（按需）
rm -rf user-web/bridge/dist user-web/bridge/dist-zip

# 4. 日志轮转（建议配置 logrotate，示例 /etc/logrotate.d/hivemtk）：
#   $HOME/.hivemtk/runtime/*.log {
#       daily
#       rotate 7
#       compress
#       missingok
#   }
```

### 6.5 责任人
系统运维工程师；磁盘满 P1 级 1 小时恢复目标。

---

## 7. 故障升级矩阵

| 故障 | 级别 | 首次响应 | 恢复目标 | 升级路径 |
|------|------|---------|---------|---------|
| 服务起不来 | P1 | 15 min | 1 h | 运维 → 后端组 |
| 数据库断连 | P1 | 15 min | 30 min | 运维 → DBA |
| LLM 不可用 | P1 | 15 min | 1 h | AI 平台工程师 |
| 桥接全渠道掉线 | P1 | 15 min | 2 h | 桥接运维 → 前端组 |
| 内存暴涨 | P2 | 30 min | 4 h | 运维 → 前端组 |
| 磁盘满 | P1 | 15 min | 1 h | 系统运维 |

> 桥接全渠道掉线判定为 P1：直接影响自动获客/自动回复核心链路。其余参考 [SLA/SLO](SLA_SLO.md) 约定。

---

## 8. 运维检查清单（建议频率）

| 频率 | 动作 | 命令 |
|------|------|------|
| 每小时 | 端点巡检 | `make inference-host-status` |
| 每小时 | LLM 槽位水位 | `curl -s :8207/metrics \| grep slots` |
| 每日 | 数据库备份 | `make db-backup` |
| 每日 | 磁盘水位 | `df -h` |
| 每日 | 桥接日志异常扫描 | `grep -iE "error\|fail\|timeout" $HOME/.hivemtk/runtime/*.log` |
| 每周 | 备份恢复演练 | 见 [DR_RECOVERY.md](DR_RECOVERY.md) |
| 每周 | 依赖漏洞扫描 | Dependabot PR 合并 |

---

**版本历史**
- v1.0 (2026-08-15)：初版，覆盖 6 类高频故障（起不来/断连/LLM 掉线/桥接全掉/内存/磁盘），命令基于 Makefile 与宿主机推理栈实测。
