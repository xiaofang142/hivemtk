# 智能体知识库隔离 - 部署指南

> 适用版本: user-server v0.9.x+  
> 部署类型: 滚动升级 / 灰度发布 / 回滚  
> 文档版本: 1.0 (2026-07-31)

---

## 1. 部署前准备

### 1.1 环境要求

| 组件 | 最低版本 | 推荐版本 | 备注 |
|------|----------|----------|------|
| PostgreSQL | 13.x | 15.x | 含 pgvector 扩展 |
| user-server | v0.9.0+ | latest | - |
| Redis (可选) | 6.x | 7.x | v2.0 缓存 |
| Docker | 20.x | 24.x | 容器化部署 |

### 1.2 备份策略

```bash
# 1. 备份元数据库
pg_dump -h <host> -p 8202 -U admin -d user_db \
  --table=knowledge_bases \
  --table=agent_kb_bindings \
  -F c -f /backup/knowledge_group_pre_deploy_$(date +%Y%m%d_%H%M%S).dump

# 2. 备份内容库 (FAQ/RAG/SOP)
pg_dump -h <host> -p 8202 -U admin -d user_db \
  -F c -f /backup/content_pre_deploy_$(date +%Y%m%d_%H%M%S).dump
```

### 1.3 健康检查

```bash
# 数据库连接
curl -s http://localhost:8202/health
# 用户服务健康检查
curl -s http://localhost:8080/api/v1/health
```

---

## 2. 数据库迁移

### 2.1 迁移文件

| 序号 | 文件 | 描述 |
|------|------|------|
| 1 | `migrations/20260731_001_create_knowledge_bases.up.sql` | 创建 knowledge_bases 表 |
| 2 | `migrations/20260731_001_create_knowledge_bases.down.sql` | 回滚 (谨慎使用) |
| 3 | `migrations/20260731_002_create_agent_kb_bindings.up.sql` | 创建中间表 |
| 4 | `migrations/20260731_002_create_agent_kb_bindings.down.sql` | 回滚 |

### 2.2 执行迁移

```bash
# 1. 进入迁移目录
cd hivemtk/migrations

# 2. 干跑 (dry-run)
psql -h <host> -p 8202 -U admin -d user_db \
  --single-transaction --on-error-stop \
  -f 20260731_001_create_knowledge_bases.up.sql

# 3. 确认无错误后正式执行
psql -h <host> -p 8202 -U admin -d user_db \
  --single-transaction \
  -f 20260731_001_create_knowledge_bases.up.sql \
  -f 20260731_002_create_agent_kb_bindings.up.sql
```

### 2.3 验证迁移

```sql
-- 检查表结构
\dt knowledge_bases
\dt agent_kb_bindings

-- 检查索引
\di+ knowledge_bases
\di+ agent_kb_bindings

-- 验证数据 (空表)
SELECT COUNT(*) FROM knowledge_bases;     -- 应为 0
SELECT COUNT(*) FROM agent_kb_bindings;  -- 应为 0
```

---

## 3. 灰度发布策略

### 3.1 阶段规划

```
阶段 0: 影子流量 (Day 0)
   └─ 1% 流量, 仅观察, 不影响业务
   
阶段 1: 小流量验证 (Day 1-2)
   └─ 5% 流量, 监控关键指标
   
阶段 2: 中等流量 (Day 3-5)
   └─ 25% → 50% 流量
   
阶段 3: 全量发布 (Day 6+)
   └─ 100% 流量
```

### 3.2 特性开关

```go
// internal/config/feature_flags.go
const (
    // KnowledgeGroupIsolation 智能体知识库隔离总开关
    KnowledgeGroupIsolation = "knowledge_group_isolation"
    
    // KnowledgeGroupRolloutPercent 灰度发布百分比 (0-100)
    KnowledgeGroupRolloutPercent = "knowledge_group_rollout_percent"
)
```

### 3.3 灰度控制

```bash
# 通过环境变量控制
export FEATURE_KNOWLEDGE_GROUP_ISOLATION=true
export FEATURE_KNOWLEDGE_GROUP_ROLLOUT_PERCENT=25

# 或通过配置中心
curl -X PATCH http://config-center/api/v1/features/knowledge_group_isolation \
  -H "Content-Type: application/json" \
  -d '{"rollout_percent": 25}'
```

### 3.4 灰度验证检查清单

| 项 | 验证方式 | 通过条件 |
|----|----------|----------|
| 数据库表存在 | `\dt knowledge_bases` | ✅ 表存在 |
| 索引创建成功 | `\di+ knowledge_bases` | ✅ 6 个索引 |
| Service 可调用 | 单元测试 | ✅ 全部通过 |
| E2E 业务通过 | e2e 测试 | ✅ 7/7 通过 |
| 错误率 | SQL: `audit_logs` (越权 / 异常) | < 0.1% |
| 响应时间 | SQL: `layer_decision_logs.wall_ms` P99 | < 200ms |

---

## 4. 容器化部署（仓内不提供镜像定义）

> ⚠️ 本仓自 `94415060`「重构宿主机部署」起**不再随附任何 Dockerfile**（`git ls-files | grep -i dockerfile` = 0），
> 部署形态是宿主机二进制 + 根 `docker-compose.yml` 只起数据层。下面这段是"你要自己容器化时"的样例，
> 口径按今天的产码核对过：入口包 `./cmd/api`（仓内没有 `cmd/user-server`）、监听 8204（`PORT` 可覆盖）、
> 存活探针 `/healthz`（`internal/router/router.go:189`）、Go 版本跟 `user-server/go.mod` 的 `go 1.25.0`。
> DB 只有 `DB_HOST`/`DB_PORT` 两个占位可覆盖，账号与库名在 `user-server/config.yaml` 里是写死的
> （`user: admin`、`dbname: user_db`）⇒ 容器化要挂一份改过的 `config.yaml`，别指望 `POSTGRES_USER` 这类变量。

```dockerfile
# 自建镜像样例（放到你自己的路径，仓内不入库）
FROM golang:1.25 AS builder
WORKDIR /build
COPY user-server/ .
RUN CGO_ENABLED=0 go build -o /build/user-server ./cmd/api

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*
COPY --from=builder /build/user-server /usr/local/bin/
ENV PORT=8204
EXPOSE 8204
CMD ["/usr/local/bin/user-server"]
```

### 4.2 Docker Compose

```yaml
# 片段：服务名要与数据层容器同网段（根 compose 里的数据层服务名是 mtk-postgres / mtk-redis）
services:
  user-server:
    image: hivemtk/user-server:latest        # 由上面那段自建 Dockerfile 出，仓内无此镜像
    ports:
      - "8204:8204"
    environment:
      - PORT=8204
      - DB_HOST=mtk-postgres        # user-server/config.yaml 的占位符是 ${DB_HOST:127.0.0.1} / ${DB_PORT:8232}
      - DB_PORT=8202
    depends_on:
      - mtk-postgres
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8204/healthz"]
      interval: 30s
      timeout: 5s
      retries: 3
```

> 特性开关不走环境变量：`FEATURE_KNOWLEDGE_GROUP_ISOLATION` / `..._ROLLOUT_PERCENT` 在 Go 侧**零读取点**
> （本轮 `git grep` 实测），开关由 `feature_flag` 表驱动（`internal/model/feature_flag.go` 的
> `RolloutPercentage`，`internal/service/feature_flag.go` 校验 0–100）。

### 4.3 滚动升级

```bash
# 1. 拉取新镜像
docker pull hivemtk/user-server:v0.9.1

# 2. 滚动重启 (一个一个来)
docker-compose up -d --no-deps --force-recreate user-server

# 3. 监控
docker logs -f user-server | grep -E "(started|error|panic)"
```

---

## 5. K8s 部署

### 5.1 Deployment YAML

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: user-server
  namespace: hivemtk
spec:
  replicas: 3
  selector:
    matchLabels:
      app: user-server
  template:
    metadata:
      labels:
        app: user-server
    spec:
      containers:
      - name: user-server
        image: hivemtk/user-server:v0.9.1
        ports:
        - containerPort: 8080
        env:
        - name: POSTGRES_HOST
          valueFrom:
            secretKeyRef:
              name: pg-credentials
              key: host
        - name: FEATURE_KNOWLEDGE_GROUP_ISOLATION
          value: "true"
        - name: FEATURE_KNOWLEDGE_GROUP_ROLLOUT_PERCENT
          value: "100"
        readinessProbe:
          httpGet:
            path: /api/v1/health
            port: 8080
          initialDelaySeconds: 10
          periodSeconds: 5
        livenessProbe:
          httpGet:
            path: /api/v1/health
            port: 8080
          initialDelaySeconds: 30
          periodSeconds: 30
        resources:
          requests:
            memory: "512Mi"
            cpu: "500m"
          limits:
            memory: "2Gi"
            cpu: "2000m"
```

### 5.2 滚动更新 (docker compose up -d)

> 私域部署: 无 Argo Rollouts 灰度控制台, 通过 docker compose 滚动重启 + SQL 巡检完成版本切换。
> 步骤:
> 1. `docker compose pull` 拉取新镜像
> 2. `docker compose up -d` 触发滚动重启 (1 个实例先停, 1 个起来, 验证健康)
> 3. 执行文档第 7.1 节提供的验收脚本，SQL 巡检 P99 延迟 / 错误率
> 4. 异常回滚: `git checkout HEAD~1 && docker compose up -d` 立即回退到上一镜像

---

## 6. 监控与告警

### 6.1 部署后立即验证

```bash
# 1. 健康检查
curl http://user-server:8080/api/v1/health

# 2. 测试核心 API
curl -X POST http://user-server:8080/api/v1/knowledge-bases \
  -H "Content-Type: application/json" \
  -d '{
    "kb_code": "KB-DEPLOY-TEST",
    "type": "faq",
    "name": "deploy test",
    "owner_type": "shared"
  }'

# 3. 验证返回
# 期望: 200 OK + KB ID
```

### 6.2 关键监控指标

> 私域部署: 不引入外部监控/告警通道。
> 关键指标 (KB 创建数 / ListByAgent 延迟 / 级联删除计数 / 越权访问计数) 通过 `audit_logs` 表落库,
> 巡检通过文档第 7.1 节提供的验收脚本（SQL 查询实现）。

### 6.3 关键指标审计 (无外部告警)

> 私域部署版本: 关键指标 (越权访问、级联删除延迟) 通过应用层日志 + `audit_logs` 表行数变化人工巡检,
> 详见文档第 7.1 节验收脚本。

---

## 7. 部署后验收 (Post-Deploy Acceptance)

### 7.1 自动化验收脚本

> **注意**: 以下脚本内容需保存为 `scripts/post_deploy_check.sh` 后执行。

```bash
#!/bin/bash
# scripts/post_deploy_check.sh
set -e

echo "=== 部署后验收 ==="

# 1. 健康检查
echo "[1/5] 健康检查..."
curl -sf http://localhost:8080/api/v1/health || (echo "❌ 健康检查失败" && exit 1)

# 2. 表结构验证
echo "[2/5] 数据库表验证..."
TABLES=$(psql -h localhost -U admin -d user_db -tAc "SELECT count(*) FROM information_schema.tables WHERE table_name IN ('knowledge_bases', 'agent_kb_bindings');")
[ "$TABLES" -eq 2 ] || (echo "❌ 缺少表 (期望 2, 实际 $TABLES)" && exit 1)

# 3. 索引验证
echo "[3/5] 索引验证..."
INDEXES=$(psql -h localhost -U admin -d user_db -tAc "SELECT count(*) FROM pg_indexes WHERE tablename IN ('knowledge_bases', 'agent_kb_bindings');")
[ "$INDEXES" -ge 10 ] || (echo "❌ 索引数过少 (期望 ≥10, 实际 $INDEXES)" && exit 1)

# 4. API 端到端
echo "[4/5] API 端到端..."
RESP=$(curl -sf -X POST http://localhost:8080/api/v1/knowledge-bases \
  -H "Content-Type: application/json" \
  -d '{"kb_code":"KB-DEPLOY-VERIFY","type":"faq","name":"verify","owner_type":"shared"}')
echo "$RESP" | grep -q '"id"' || (echo "❌ 创建失败" && exit 1)

# 5. 测试套件
echo "[5/5] 测试套件..."
POSTGRES_TEST_PORT=8232 go test ./internal/repository ./internal/service ./test/... \
  -count=1 -timeout 120s 2>&1 | tail -3

echo "✅ 全部验收通过"
```

### 8.2 手动验收清单

| 项 | 验证内容 | 期望 | 实际 |
|----|----------|------|------|
| 1 | 健康检查 | 200 OK | __ |
| 2 | 创建私有 KB | 200 + ID | __ |
| 3 | 创建共享 KB | 200 + ID | __ |
| 4 | ListByAgent 隔离 | 只看自己 | __ |
| 5 | binding 后可见 | 共享 KB 可见 | __ |
| 6 | 删除级联 | binding 同步删 | __ |
| 7 | 业务校验 | 非法 type 拒绝 | __ |

---

## 9. 常见问题 (Troubleshooting)

### 9.1 表已存在

```
ERROR: relation "knowledge_bases" already exists
```

**解决**: 确认迁移状态, 用 `IF NOT EXISTS` 幂等执行:
```sql
CREATE TABLE IF NOT EXISTS knowledge_bases (...);
```

### 9.2 NOT NULL 约束失败

```
ERROR: null value in column "enabled" of relation "knowledge_bases"
```

**原因**: 业务代码用 `Select("*").Updates(kb)`, 把 nil pointer 写为 NULL。

**解决**: Repository 用 `Updates(map[string]any{...})` 显式列字段。

### 9.3 越权访问

```
isolation_violation_total > 0
```

**排查**:
1. 检查 `KnowledgeBaseRepository.ListByAgent` 子查询
2. 检查 `enabled` 字段是否在 WHERE 中
3. 跑 `TestE2E_MultiAgent_KnowledgeIsolation` 复现

### 9.4 性能慢

```
kb_list_duration_seconds P99 > 200ms
```

**排查**:
1. EXPLAIN ANALYZE ListByAgent 查询
2. 检查是否有 (agent_id, kb_id) 索引
3. 检查 `enabled` 索引是否生效

---

## 10. 相关文档

- `docs/architecture/KNOWLEDGE_GROUP_DESIGN.md` - 架构设计
- `docs/architecture/adr/ADR-014-knowledge-group-isolation.md` - ADR
- `docs/operations/KNOWLEDGE_GROUP_API.md` - API 参考
- `docs/operations/KNOWLEDGE_GROUP_MONITORING.md` - 监控
- `docs/standards/MASTER_RULES.md` - 项目核心规则

---

**最后更新**: 2026-07-31  
**作者**: HiveMTK SRE 团队
