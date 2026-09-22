# HiveMtk 密钥轮换策略

> **配套规则**: [MASTER_RULES.md](../standards/MASTER_RULES.md)

---

## 一、目的

定期轮换所有长期凭证，降低泄露风险。

---

## 二、轮换清单

> 本表的"环境变量名/存储位置"列在 2026-09-19 逐项对代码做过 `grep` 核对：
> 原表写的 `MERCHANT_HMAC_KEY` 在 `user-server` 全仓 0 命中（真实键名是 `MERCHANT_API_SECRET`，
> 且不在数据库而在 `.env`），已按实测更正。

| 密钥 | 环境变量 | 频率 | 存储位置（实测） |
|------|----------|------|----------|
| JWT 签名（用户端） | `USER_JWT_SECRET` | 90 天 | 根 `.env` **与** `user-server/.env`（后者覆盖前者，见二A.2） |
| JWT 签名（商户/平台侧） | `JWT_SECRET` | 90 天 | 各自服务目录的 `.env` |
| 商户 HMAC | `MERCHANT_API_SECRET` | 180 天 | `.env`（user 与 platform 两侧必须同值）；仅平台集成开启时参与出站签名 |
| 字段加密 | `FIELD_ENCRYPTION_KEY` | 180 天 | `.env` 或 `/run/secrets`（见第六节） |
| 数据库密码 | `POSTGRES_PASSWORD` | 180 天 | `.env` + `user-server/.env` + `assetdpo/.env`(`HIVE_DB_PASSWORD`)，同一个 role |
| Redis 密码 | `REDIS_PASSWORD` | 365 天 | `.env` + `redis.conf` |

> 本表原来还有一行「平台授权签名 `PLATFORM_LICENSE_SECRET`｜180 天」。2026-09-21 复核：
> user-server 与 platform-server 两侧生产代码都**没有任何读取点**（字面量 grep 零命中，
> `scripts/check-env-coverage.py` 数到的 180 个「生产代码读取键」里也没有它），授权流程下线后它不再是在用凭证，
> 因此从轮换表里删掉。它**仍留在下面的泄露面表**里——值进过公开仓历史，这件事不因为代码不再读它而消失；
> `scripts/rotate-secrets.sh` 的 `license` 行同样保留（那张表是泄露取证登记，不是"在用凭证清单"）。
>
> 2026-09-22 实测补记：`.env` 里的 4 行 `PLATFORM_LICENSE_SECRET`（`hivemtk/.env` 同文件重复键两处、
> `hivemtk-platform/.env`、`hivemtk-platform/platform-server/.env`）已删除。删除前先跑
> `--dry-run license`，脚本按"写后校验失败…未全部更新"退 1——它把"键根本不存在"当成了"写进去没生效"，
> 于是 `--all-burned` 会因这条已下线凭证永久红。`scripts/rotate-secrets.sh` 已改为先探测落点是否真的有这个键：
> 无一处有则 warn+skip（退 0），写后校验只核"原本就有这个键"的落点。
> 改完复跑三腿：`--dry-run license` 退 0、`--dry-run merchant_hmac` 退 0 且印「备份 2 条」、
> `--dry-run --all-burned` 退 0（三条在用的照常轮换 + license 跳过）；
> 反向验证把 `write_key` 改成空操作后 `--dry-run merchant_hmac` 退 1 并逐个点名两个落点，
> 证明新校验仍有牙（真实 `.env` 全程只读，演练只写 `mktemp -d` 副本）。

---

## 二A、本机实测泄露面（2026-09-19，T-P0-05）

### 二A.1 判据与结果

判据不是"文件里还有没有"，而是"**公开仓的历史里能不能翻出来**"：

```bash
# 逐个真值在公开仓历史里做 pickaxe（只数命中提交数，不打印明文）。
# 81955cfc 是"清除跟踪文件明文"那次提交，它本身会因删除真值而被 pickaxe 命中，
# 所以以它的前一个版本为界来查，才等于"是否曾经公开暴露过"。
cd hivemtk
grep -E '^[A-Za-z0-9_]+=' .env | sort -u | while IFS='=' read -r k v; do
  case "$k" in *PASSW*|*SECRET*|*TOKEN*|*KEY*) ;; *) continue ;; esac
  [ ${#v} -ge 8 ] || continue
  printf '%-26s 曾公开命中=%s\n' "$k" "$(git log 81955cfc~1 --format=%h -S"$v" | wc -l | tr -d ' ')"
done
```

| 凭证 | 公开仓历史命中 | 本机是否正在用该值 | 结论 |
|------|---|---|---|
| `POSTGRES_PASSWORD`（用户库） | 9 个提交（最早 2026-07-23） | 是（`:8204` 进程 env 实测为该值） | 必须轮换 |
| `MERCHANT_API_SECRET` | 2 个提交（2026-07-31） | 是，且 **user/platform 两侧同值**＝共享密钥 | 必须轮换（两侧一起） |
| `PLATFORM_LICENSE_SECRET` | 3 个提交含该值（`89f34e78` 2026-07-31 与 `e1d0ca9c` 2026-08-01 写入 `.env-example`，`81955cfc` 2026-09-19 清除）；前两枚已在 `upstream/master`（GitHub）与 `gitee-upstream/master` 上＝**已公开** | 本机侧已从 4 处 `.env` 行中删除（2026-09-22），HEAD 树里 `git grep` 该值命中 0 | 无需"换新值"：它不再被任何代码读取。历史里那两枚提交只能靠改写历史或接受其公开；若你自建的平台端曾拿它做过签名校验，那侧按作废处理 |
| 平台库 `POSTGRES_PASSWORD` | 1 个提交（`user-server/tests/e2e/deep_lib.sh`，跨仓串味） | 是（`:8205`） | 必须轮换 |
| `USER_JWT_SECRET`（64 hex，根 `.env`） | 4 个提交 | **否**——真正在用的是 `user-server/.env` 里另一枚（57 字符），且该值 pickaxe 0 命中 | 本机无需轮换；部署侧若用的是被提交的那枚则需轮换 |
| `REDIS_PASSWORD` / `TG_BOT_TOKEN` / `VISITOR_TOKEN_SECRET` / `MASTER_KEY` / `PLATFORM_ADMIN_PASSWORD` / `DS_API_KEY` | 0 | 是 | 未进过版本库，按常规周期轮换即可 |
| `QINIU_*` / `EMBEDDING_API_KEY` / `RERANK_API_KEY` / 邮箱口令 | — | `.env` 里为空值 | 未配置，无泄露面 |

两点反直觉结论，值得留档：

1. **泄露的是"文件态"还是"运行态"要分开判**。已提交的 JWT 值本机根本没在用（被
   `user-server/.env` 覆盖），所以换它能带来的收益是零、代价是全员掉线；
   而看起来"只是本地开发库"的 `POSTGRES_PASSWORD` 却是运行态真值，且与部署机同源的概率高。
2. **跨仓串味**：私有 platform 库的口令是从 **公开** user 仓的 e2e 脚本里泄出去的。
   只扫"本仓自己的 .env 键"会漏掉这一类，因此 `check-secrets.sh` 的 A 项要求把
   相邻仓的 `.env` 也喂进来比对（`ENV_FILE=../hivemtk-platform/platform-server/.env bash scripts/check-secrets.sh`）。

### 二A.2 处置状态（2026-09-19 用户拍板）

- 已做：清除跟踪文件里的明文（`81955cfc`）+ 上防复发闸门 `scripts/check-secrets.sh`。
- **暂不做：口令轮换与 git 历史改写**（F1 决策：暂不处置）。轮换工具已备好但默认拒绝执行，
  需人工显式授权：`ROTATE_AUTHORIZED=1 bash scripts/rotate-secrets.sh --all-burned`。
- 不改历史的理由（记录在此，避免下次重新论证）：这些值已经公开可查，改写只是把"可查"变成
  "不可查"，而**轮换是把"可用"变成"不可用"**——只有后者能真正终止泄露的价值；
  同时 `filter-repo` 会作废所有既有克隆与 CI 缓存，收益为零、代价为共享现场破坏。
  标准处置顺序即"rotate, don't erase"。
- 未完成：顶层非 git 目录 `scripts/` 里那份 `bulk_seed.py` 仍含同一枚明文（不在任何闸门覆盖内），
  该残留已移交审计会话 C 的定时修复任务（见 `docs/audit-2026-09-19-sessionC.md` F2），此处不重复修。

### 二A.3 为什么这些脚本命令不能手敲

一次轮换要同时落到 4 个 `.env`、1 次 `ALTER USER`、2 个运行中服务的重启。少做任何一步，
得到的是"配置文件与真实口令漂移"的状态——表现为上千条测试假红而不是显式报错
（2026-09-18 实际踩过）。所以要么全做、要么不做，用 `scripts/rotate-secrets.sh`：
它在切口令前检测并发测试/灌数作业并拒绝（互斥），写后逐键校验重复键全部更新，
`ALTER` 前后备份旧值到仓外 `700` 目录（放仓内会被自家闸门判为泄露），
失败可用 `--rollback <备份目录>` 一步退回。

---


## 三、轮换流程

### 3.1 准备阶段

```bash
# 1. 生成新密钥
openssl rand -hex 32

# 2. 备份当前配置（备份文件不要放进仓内：check-secrets.sh 会按工作区内容扫，仓内副本即视为泄露）
cp .env /tmp/env.backup.$(date +%s)

# 3. 通知相关人员（同一套口令被几个服务读，见二A.1 表格的"存储位置"列）
```

### 3.2 执行轮换

日常一律走脚本，下面的手工命令是"脚本不可用时的等价步骤"，且必须整组做完：

#### JWT 密钥 (90天)

```bash
# 1. 先确认"线上实际用的是哪一枚密钥"——根 .env 与 user-server/.env 都有 USER_JWT_SECRET，
#    后加载的覆盖先加载的；改了没在用的那枚，等于没换。做法：拿一个真 token，逐个候选验签。
set -a && . .env && set +a
TOKEN=$(curl -s -X POST http://127.0.0.1:8204/api/auth/login -H 'Content-Type: application/json' \
  -d '{"username":"'"${HIVEMTK_ADMIN:-e2e_admin}"'","password":"'"$HIVEMTK_ADMIN_PASS"'"}' \
  | python3 -c 'import sys,json;print(json.load(sys.stdin)["data"]["token"])')
python3 - "$TOKEN" "$(awk -F= '/^USER_JWT_SECRET=/{print $2; exit}' .env)" \
                   "$(awk -F= '/^USER_JWT_SECRET=/{print $2; exit}' user-server/.env)" <<'PY'
import sys, hmac, hashlib, base64
h, p, s = sys.argv[1].split('.')
for i, secret in enumerate(sys.argv[2:], 1):
    sig = base64.urlsafe_b64encode(hmac.new(secret.encode(), f"{h}.{p}".encode(),
                                            hashlib.sha256).digest()).rstrip(b'=').decode()
    print(f"候选{i}: {'✅ 这就是线上生效的签名密钥' if sig == s else '❌ 不匹配（改它不会生效）'}")
PY
#   ↑ 2026-09-19 实测：候选1（根 .env）❌ 不匹配，候选2（user-server/.env）✅ 生效

# 2. 改对应那枚 .env 的值（新旧两份都建议留注释一行，便于回滚），然后重启服务：
#    user-server 不是 compose 服务（docker-compose.yml 里只有 mtk-postgres / mtk-redis），
#    本地是裸进程，env 只在启动时读一次，不重启等于没换
kill "$(lsof -nP -tiTCP:8204 -sTCP:LISTEN | head -1)"
( cd user-server && set -a && . ../.env && [ -f .env ] && . ./.env && set +a \
  && nohup ./bin/user-server >> /tmp/user-server.log 2>&1 & )

# 3. 正向验证：新 token 能过鉴权
TOKEN=$(curl -s -X POST http://127.0.0.1:8204/api/auth/login -H 'Content-Type: application/json' \
  -d '{"username":"'"${HIVEMTK_ADMIN:-e2e_admin}"'","password":"'"$HIVEMTK_ADMIN_PASS"'"}' \
  | python3 -c 'import sys,json;print(json.load(sys.stdin)["data"]["token"])')
curl -s -o /dev/null -w '新token=%{http_code}\n' -H "Authorization: Bearer $TOKEN" \
  http://127.0.0.1:8204/api/auth/current-user          # 期望 200

# 4. 反向验证（这一步才是"换成功"的证据；只验正向不足以说明旧密钥已失效）
curl -s -o /dev/null -w '伪造/旧token=%{http_code}\n' \
  -H "Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.notarealsig" \
  http://127.0.0.1:8204/api/auth/current-user          # 期望 401（2026-09-19 实测 401）
```

#### 数据库密码 (180天)

```bash
# 角色名是 admin（不是 hivemtk；容器 POSTGRES_USER=admin），宿主端口 8232 → 容器内 8202
NEW=$(openssl rand -hex 24)
docker exec mtk-postgres sh -c "PGPORT=8202 psql -U admin -d postgres -c \"ALTER USER admin WITH PASSWORD '$NEW'\""

# 三个读同一 role 的文件要同步（漏一个就是"口令漂移"，表现为测试大面积假红而非显式报错）
#   hivemtk/.env POSTGRES_PASSWORD（同文件内有重复行，两条都要改）
#   hivemtk/user-server/.env POSTGRES_PASSWORD
#   assetdpo/.env HIVE_DB_PASSWORD
# 平台库是另一个容器/另一个口令：mtk-platform-postgres，容器内端口 8201

# 校验：新口令连得上、旧口令连不上
psql -h 127.0.0.1 -p 8232 -U admin -d user_db -c 'select 1'   # 输旧口令应失败，新口令应成功

# 再按 3.2 的重启方式拉起 user-server
```

#### Redis 密码 (365天)

```bash
redis-cli -a "$OLD" CONFIG SET requirepass "$NEW"   # 运行时生效
# 同步 .env 的 REDIS_PASSWORD 与 redis.conf 的 requirepass，然后重启读它的服务
```

### 3.3 验证

```bash
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8204/health    # 200（含 Redis/DB 依赖检查）
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8204/healthz   # 200（仅存活）
tail -50 /tmp/user-server.log | grep -i error
python3 scripts/api_verify_full.py      # 三端四维度验收，见 CLAUDE.md 的 API 测试验收规则
```

---

## 四、紧急轮换（密钥已泄露）

口令已经在公开历史里 = 已泄露，处理顺序是**先终止可用性（轮换），再考虑可查性（改历史）**：

```bash
# 1. 用一条命令把"曾公开命中"的集合全换掉（会改 4 个 .env + ALTER + 重启，见二A.3）
ROTATE_AUTHORIZED=1 bash scripts/rotate-secrets.sh --all-burned

# 2. 校验
bash scripts/check-secrets.sh
python3 scripts/api_verify_full.py

# 3. 失败回退
bash scripts/rotate-secrets.sh --rollback <脚本打印的备份目录>
```

`git filter-repo` + 强推**不是**本场景的推荐动作，理由见二A.2。
部署机（如 `hiveuser.*`）不会因本机轮换而变安全：同名凭证要在部署侧各自轮换一遍，
且部署侧轮换后本机的旧值才算彻底失效。

---

## 五、检查命令

```bash
# 查看密钥文件权限
ls -la .env && chmod 600 .env
stat .env

# 防复发闸门：A 项按本机 .env 真值逐字节比对，B 项扫字面量赋值模式
bash scripts/check-secrets.sh
# 跨仓比对（本仓文件 vs 相邻仓的 .env 真值，用于抓"私有仓口令从公开仓泄露"这类串味）
ENV_FILE=../hivemtk-platform/platform-server/.env bash scripts/check-secrets.sh
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
