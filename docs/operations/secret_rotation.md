# HiveMtk 密钥轮换策略

> **配套规则**: [MASTER_RULES.md](../standards/MASTER_RULES.md)

---

## 一、目的

定期轮换所有长期凭证，降低泄露风险。

---

## 二、轮换清单

| 密钥 | 环境变量 | 频率 | 存储位置 |
|------|----------|------|----------|
| JWT 签名 | `JWT_SECRET` | 90 天 | `.env` 文件 |
| 商户 HMAC | `MERCHANT_HMAC_KEY` | 180 天 | 数据库 |
| 字段加密 | `FIELD_ENCRYPTION_KEY` | 180 天 | `.env` 文件 |
| 数据库密码 | `POSTGRES_PASSWORD` | 180 天 | `.env` 文件 |
| Redis 密码 | `REDIS_PASSWORD` | 365 天 | `redis.conf` |

---

## 三、轮换流程

### 3.1 准备阶段

```bash
# 1. 生成新密钥
openssl rand -hex 32

# 2. 备份当前配置
cp .env .env.backup

# 3. 通知相关人员
```

### 3.2 执行轮换

#### JWT 密钥 (90天)

```bash
# 1. 编辑 .env 文件
# JWT_SECRET_OLD=旧值
# JWT_SECRET=新值

# 2. 重启服务
docker compose restart user-server

# 3. 验证新密钥生效
curl -H "Authorization: Bearer <新token>" http://localhost:8204/api/v1/health

# 4. 7天后移除旧密钥
```

#### 数据库密码 (180天)

```bash
# 1. 修改 PostgreSQL 密码
psql -c "ALTER USER hivemtk PASSWORD '新密码';"

# 2. 更新 .env 文件中的 POSTGRES_PASSWORD

# 3. 重启服务
docker compose restart user-server
```

#### Redis 密码 (365天)

```bash
# 1. 修改 Redis 密码
redis-cli CONFIG SET requirepass "新密码"

# 2. 更新 .env 文件中的 REDIS_PASSWORD

# 3. 重启服务
docker compose restart user-server
```

### 3.3 验证

```bash
# 检查服务健康状态
curl http://localhost:8204/healthz

# 检查日志有无错误
docker compose logs --tail=50 user-server | grep -i error
```

---

## 四、紧急轮换

**触发**: 密钥泄露

```bash
# 1. 立即吊销
# 编辑 .env 移除泄露的密钥

# 2. 生成新密钥
NEW_SECRET=$(openssl rand -hex 32)

# 3. 更新配置并重启
# .env: JWT_SECRET=$NEW_SECRET
docker compose restart user-server

# 4. 验证服务正常
curl http://localhost:8204/healthz
```

---

## 五、检查命令

```bash
# 查看密钥文件权限
ls -la .env
chmod 600 .env

# 检查密钥使用时长
# 通过 last_modified 时间戳判断
stat .env
```

---

## 六、/run/secrets 文件注入（OPT-SEC-08）

除 `.env` / 容器 env 外，user-server 支持从**挂载卷文件**读取密钥（Vault / KMS /
ExternalSecrets 通常以文件或卷形态分发密钥），实现位置
`user-server/internal/secrets/fileloader.go`，启动流程：

```
secrets.LoadFiles(dir)   →   secrets.InitFromEnv()   →   AES-256-GCM 就绪
   文件注入 env                从 env 取 MASTER_KEY
```

`dir` 取值：环境变量 `HIVEMTK_SECRETS_DIR` 非空时用之，否则默认 `/run/secrets`。
目录不存在时静默跳过（返回 0），不影响本地开发与 compose 部署。

### 6.1 文件格式约定

| 扩展名 | 规则 | 示例 |
|--------|------|------|
| `*.txt` | 文件名去扩展名 = 变量名，文件首行非空内容 = 值 | `/run/secrets/MASTER_KEY.txt` → `MASTER_KEY` |
| `*.env` | 标准 `KEY=VALUE` 逐行；`#` 注释；支持 `export` 前缀与包裹引号 | `/run/secrets/hivemtk.env` |

其他扩展名文件与子目录一律忽略。

### 6.2 优先级

**已显式设置的 env 优先，文件不覆盖**。即：

```
容器 env / -e 参数  >  /run/secrets/*.txt|*.env 文件  >  未设置
```

便于临时轮换验证：`docker run -e MASTER_KEY=<新值>` 即可覆盖挂载文件内容，无需改镜像。

### 6.3 Kubernetes Secret 挂载示例

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: hivemtk-secrets
  namespace: hivemtk
type: Opaque
stringData:
  # key 名带扩展名，挂载后即为 fileloader 可消费的文件名
  MASTER_KEY.txt: "<32+ 字节主密钥>"
  hivemtk.env: |
    JWT_SECRET=<jwt-secret>
    FIELD_ENCRYPTION_KEY=<fek>
```

Helm chart 已内建该挂载（`deploy/helm/hivemtk`，`secrets.enabled=true` 时把
`secrets.name` 指向的 Secret 只读挂载到 `secrets.mountPath`，默认 `/run/secrets`）：

```bash
kubectl -n hivemtk create secret generic hivemtk-secrets \
  --from-file=MASTER_KEY.txt=./MASTER_KEY.txt \
  --from-file=hivemtk.env=./hivemtk.env

helm upgrade --install hivemtk ./deploy/helm/hivemtk \
  --set secrets.enabled=true \
  --set secrets.name=hivemtk-secrets
```

挂载路径自定义（如非只读 root 环境）：在 `values.env` 追加
`{ name: HIVEMTK_SECRETS_DIR, value: /etc/hivemtk/secrets }` 并同步 `secrets.mountPath`。

### 6.4 轮换操作

```bash
# 1. 更新 Secret（滚动触发 Pod 重建）
kubectl -n hivemtk create secret generic hivemtk-secrets \
  --from-file=MASTER_KEY.txt=./MASTER_KEY.new --dry-run=client -o yaml | kubectl apply -f -

# 2. 确认注入生效（启动日志应出现 "[secrets] 从 /run/secrets 注入 N 个密钥环境变量"）
kubectl -n hivemtk logs deploy/hivemtk-user-server | grep '\[secrets\]'

# 3. MASTER_KEY 轮换后需按第三节流程重新加密依赖字段（旧密文 Decrypt 失败会原样返回）
```

> 注意：`MASTER_KEY` 变更后，数据库中既有 `enc:v1:` 密文无法解密；`api_logs`
> 类日志字段读取出口会原样返回密文（不报错），LLM provider 类字段将以空 key 显式失败。
> 生产轮换需配合一次性重加密任务。

---

## 七、参考

- NIST SP 800-57: Key Management
- OWASP Cryptographic Storage Cheat Sheet
