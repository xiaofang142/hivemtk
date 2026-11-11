# Quickstart：站内消息通知模块验证指南

> 端到端验证手册。实现完成后按本文逐条跑，每条都有明确预期。
> 接口细节见 [contracts/platform-notice-api.md](./contracts/platform-notice-api.md)
> 与 [contracts/user-notice-api.md](./contracts/user-notice-api.md)。

## 前置

```bash
cd /Users/xiaofang/Documents/www/go/hivemtk/hivemtk

# Go 测试必须显式导出测试库口令（与 .env 的 POSTGRES_PASSWORD 不同名，不自动继承）
export POSTGRES_TEST_HOST=127.0.0.1 POSTGRES_TEST_PORT=8202 \
       POSTGRES_TEST_PASSWORD="$(grep '^POSTGRES_PASSWORD=' ../.env | cut -d= -f2)"
```

> ⚠️ 看到 `SQLSTATE 28P01` 或 `初始化进程级测试库失败` ⇒ **环境失败，不是代码 bug**。
> 另外 `internal/.../testdb.go` 在连接失败时会 `t.Skipf` 一律跳过 ⇒ 造出**假绿**。
> 见到"全过"先确认不是被跳过（看输出里有没有 `SKIP`）。

## 一、构建与静态门禁

```bash
cd user-server && go build ./... && go vet ./internal/... && gofmt -l internal/ | head
cd ../hivemtk-platform/platform-server && go build ./... && go vet ./... && gofmt -l internal/ | head
```

**预期**：三条命令均无输出（gofmt 列出任何文件都算失败）。

## 二、单测

```bash
cd user-server
export POSTGRES_TEST_HOST=127.0.0.1 POSTGRES_TEST_PORT=8202 \
       POSTGRES_TEST_PASSWORD="$(grep '^POSTGRES_PASSWORD=' ../.env | cut -d= -f2)"
go test ./internal/service/... ./internal/repository/... ./internal/controller/... 2>&1 | tail -40
```

**必须覆盖的三个断言点**（本模块的核心风险全在这三处）：

| 断言 | 归属 |
|---|---|
| 同一平台消息重复投递 → 用户端只有一行 | 幂等 |
| 两个租户各自投递 → 彼此看不到对方的行 | 隔离 |
| 用户甲已读 → 用户乙的未读数不变 | 按人已读 |

**预期**：`ok` 或 `[no test files]`，**无 FAIL、无 SKIP**。

## 三、本地端到端（拉起两侧服务）

### 3.1 平台端发布

```bash
# 平台超管登录（口令来自平台栈 .env 的 PLATFORM_ADMIN_PASSWORD seed 的首个超管；用户栈自 L-1 2026-11-11 起不再读取该键）
curl -s -X POST http://127.0.0.1:8205/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"Admin123"}'
```

拿 `data.token`，然后：

```bash
TOKEN=<上一步的 token>

# 发布一条"全体"警告消息
curl -s -X POST http://127.0.0.1:8205/platform/message/send \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"title":"E2E 验证公告","content":"这是一条端到端验证消息。","level":"warning","targets":[]}'
```

**预期**：`{"code":0,"data":{"id":N,"title":"E2E 验证公告",...},"message":"发送站内信成功"}`

**校验 level 真的生效**（旧实现恒为 info）：

```bash
curl -s "http://127.0.0.1:8205/platform/message/list?page=1&page_size=1" \
  -H "Authorization: Bearer $TOKEN" | grep -o '"level":"[^"]*"'
```

**预期**：`"level":"warning"`（不是 `info`）。

### 3.2 用户端收到

等一个同步周期（3 分钟），或重启 user-server 触发首轮同步。然后：

```bash
# 用户端列表（需要用户端 JWT）
curl -s "http://127.0.0.1:8204/api/auth/notifications?page=1&page_size=20" \
  -H "Authorization: Bearer <用户端 token>"
```

**预期**：列表含 `title` 为 "E2E 验证公告" 的项，且
`"source":"platform"`、`"level":"warning"`、`"type":"platform_notice"`、`"user_id":0`。

**幂等复核**：再等一个周期，重新拉列表。

**预期**：**条数不变**（仍是 1 条，不是 2 条）。

### 3.3 已读按人隔离

```bash
# 用户甲标记已读
curl -s -X POST "http://127.0.0.1:8204/api/auth/notifications/<id>/read" \
  -H "Authorization: Bearer <用户甲 token>"

# 用户甲的未读数
curl -s "http://127.0.0.1:8204/api/auth/notifications/unread-count" \
  -H "Authorization: Bearer <用户甲 token>"

# 用户乙的未读数
curl -s "http://127.0.0.1:8204/api/auth/notifications/unread-count" \
  -H "Authorization: Bearer <用户乙 token>"
```

**预期**：甲的未读数减少 1；**乙的未读数不变**（这是 SC-003 的判定点）。

### 3.4 跨租户隔离

用租户甲的用户令牌拉列表。

**预期**：列表里**只有**本租户的消息，看不到租户乙的消息。

## 四、发布上线（生产）

```bash
# 1. 交叉编译（本地二进制直接传会 exec format error）
cd user-server && GOOS=linux GOARCH=amd64 go build -o ../deploy/out/user-server ./cmd/api
cd ../hivemtk-platform/platform-server && GOOS=linux GOARCH=amd64 go build -o ../deploy/out/platform-server ./cmd/api

# 2. 传到生产并重建镜像（只 up -d 会跑旧镜像旧二进制）
scp -P 57122 deploy/out/user-server root@101.35.252.112:/www/wwwroot/hivemtk/user-server/
ssh -p 57122 root@101.35.252.112 'cd /www/wwwroot/hivemtk && docker compose up -d --build user-server'

# 3. 验二进制内容（并发泳道共用 deploy/out，发布后必须验）
ssh -p 57122 root@101.35.252.112 \
  "docker exec mtk-user-server sh -c 'ls -la /app/user-server 2>/dev/null || ls -la /app'"
```

## 五、生产端到端验证

```bash
# 平台发布（生产）
ssh -p 57122 root@101.35.252.112 \
  "docker exec mtk-platform-postgres psql -U admin -d platform_db -h 127.0.0.1 -p 8201 \
   -c \"select id,title,level,targets,published_at from platform_messages order by id desc limit 5;\""

# 平台查投递台账
ssh -p 57122 root@101.35.252.112 \
  "docker exec mtk-platform-postgres psql -U admin -d platform_db -h 127.0.0.1 -p 8201 \
   -c 'select * from notice_delivery_logs order by id desc limit 10;'"

# 用户端查落地
ssh -p 57122 root@101.35.252.112 \
  "docker exec mtk-postgres psql -U admin -d user_db -h 127.0.0.1 -p 8202 \
   -c \"select id,tenant_id,user_id,type,source,level,title,source_msg_id from notifications where source='platform' order by id desc limit 10;\""

# 用户端同步日志
ssh -p 57122 root@101.35.252.112 \
  "docker logs mtk-user-server --tail 200 2>&1 | grep notice-sync | tail -10"
```

**预期**：
- `platform_messages` 有新行，`level` 非 info，`published_at` 非空；
- `notifications` 里 `source='platform'` 的行按 `tenant_id` 正确分布，
  **每租户同一 `source_msg_id` 只有一行**；
- 同步日志有 `[notice-sync]` 关键字。

## 六、离线形态回归（PLATFORM_ENABLED=false）

```bash
cd user-server
PLATFORM_ENABLED=false go run ./cmd/api &
# 观察日志：不得出现 notice-sync 的平台请求报错
```

**预期**：日志出现 `[notice-sync] 已跳过（平台联动未启用）`，
**不得**有任何指向平台的出站请求报错。

## 七、前端门禁

```bash
cd user-web && npm run test
cd ../../hivemtk-platform/platform-web && npm run build
```

**预期**：Vitest 全绿；platform-web 构建无错误。
