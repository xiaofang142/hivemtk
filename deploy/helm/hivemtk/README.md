# HiveMtk Helm Chart

> **任务编号**: OPT-MISC-01
> **状态**: 骨架 (skeleton) — 尚未经 staging 实测
> **创建日期**: 2026-08-16
> **2026-09-22 订正**: 本行原先写 "`helm lint` 通过"，但 lint 只查 chart 结构与取值合法性，
> 恰好查不到这个骨架当时真正的两个致命伤（端口与环境变量下面 §已知限制 有账）。
> 本次改动的验证方式也一并写清：本机 **没有 helm**（`which helm` 与 `/opt/homebrew/bin`、
> `/usr/local/bin`、`~/bin` 四处均无），所以**没有**复跑 `helm lint` / `helm template`；
> 做到的是①用 `python3 -c yaml.safe_load` 解析改后的 `values.yaml` 与 `Chart.yaml`（都通过，
> 且逐项打印回读了 targetPort / 两个探针 / env 名单 / ingress.enabled / secrets.keys），
> ②每个数值都回查了产码出处（见 §已知限制 的逐条列法）。
> ⇒ 装之前请自己在有 helm 的机器上跑一遍下面两条。

## 包含内容

```
hivemtk/
├── Chart.yaml              # Chart 元数据
├── values.yaml             # 默认配置
├── README.md               # 本文件
└── templates/
    ├── deployment.yaml     # user-server Deployment
    ├── service.yaml        # ClusterIP Service
    └── ingress.yaml        # 路由规则
```

## 快速开始

```bash
# 1) 渲染预览
helm template hivemtk ./deploy/helm/hivemtk

# 2) 静态检查
helm lint ./deploy/helm/hivemtk

# 3) 安装到 k8s (需先准备 secrets)
#    这五个 key 一个都不能少：values.yaml 的 env 段按这五个名字做 secretKeyRef，
#    而 db-password / jwt-secret / master-key 三条属于"缺了进程直接退出"那类
#    （见 §已知限制 第 2、3 条；field-encryption-key 不在此列，它缺了只在用到时报错）。
#    DB 端口不在这张单子里：它不是密钥，走 values.yaml 的 env → DB_PORT 明文值。
kubectl create secret generic hivemtk-secrets \
  --from-literal=db-host=postgres \
  --from-literal=db-password=YOUR_PG_PASSWORD \
  --from-literal=jwt-secret=$(openssl rand -hex 32) \
  --from-literal=master-key=$(openssl rand -hex 32) \
  --from-literal=field-encryption-key=$(openssl rand -hex 32)

helm install hivemtk ./deploy/helm/hivemtk \
  --namespace hivemtk --create-namespace
```

## 自定义 values

```bash
helm install hivemtk ./deploy/helm/hivemtk \
  -f my-prod-values.yaml \
  --set replicaCount=3 \
  --set image.tag=v0.2.0
```

最小 `my-prod-values.yaml`:

```yaml
replicaCount: 3
ingress:
  hosts:
    - host: api.example.com
      paths:
        - path: /
          pathType: Prefix
  tls:
    - secretName: hivemtk-tls
      hosts:
        - api.example.com
```

## 已知限制 / 待办

### 2026-09-22 修掉的四处（都属"照 README 装一遍就起不来"，不是调优项）

1. **容器口 / 探针口是 8080，产码听的是 8204。**
   `DefaultListenPort = "8204"`（`user-server/internal/config/ports.go:8`，
   `cmd/api/main.go:462` 只有在 `PORT` 有值时才覆盖，而本 chart 的 env 从来没给过 `PORT`）
   ⇒ 旧 `values.yaml` 的 `service.targetPort: 8080`、两个探针的 `port: 8080` 和
   `deployment.yaml` 的 `containerPort: 8080` 三处一起把一个没人监听的口写进了探针，
   Pod 会一直 NotReady。现在三处都是 8204，`containerPort` 改成从 `.Values.service.targetPort` 取
   （模板里再写死一遍 = 同一数字两处维护，正是本 chart「配置外置」原则 2 禁止的那种）。
   防回流断言在 `user-server/internal/config/ports_test.go`（它读这三个文件对账）。
2. **env 段漏了三条密钥，其中两条不给就起不来。** 旧 env 只有
   `GIN_MODE`/`LOG_LEVEL`/`DB_HOST`/`DB_PASSWORD`
   （那个 `DB_PASSWORD` 的名字本身也是错的 ⇒ 第 3 条）：
   `MASTER_KEY` 缺失时 `cmd/api/main.go:183-189` 在非 development 下 `os.Exit(1)`，而本 chart 设了
   `GIN_MODE=release` ⇒ `config.IsDevelopmentEnv()`（`internal/config/init.go:44-58`：只认
   `APP_ENV`/`MODE`，两者为空才退到 `GIN_MODE=="debug"`）返回 false，正走那条拒绝启动的分支；
   `JWT_SECRET` 缺失或短于 32 字符时 `internal/pkg/utils/jwt.go:63-72` 直接 panic
   （那个"固定测试密钥"分支只在 go test 下可达）。而 README 的 `kubectl create secret` 里
   本来就有 `jwt-secret`，只是没有任何东西把它变成 env ⇒ 挂上了也读不到。
   现在 `env` 段补齐 `JWT_SECRET`/`MASTER_KEY`/`FIELD_ENCRYPTION_KEY` 三条 secretKeyRef，
   create-secret 样例补 `master-key`。
   （映射关系是**显式**的：env 名由 `env[].name` 决定，Secret 里那个带连字符的名字写在
   `valueFrom.secretKeyRef.key` 里，两者不靠大小写/连字符转换对上 —— 所以
   `field-encryption-key`（Secret key）→ `FIELD_ENCRYPTION_KEY`（env 名）这条是本文件里
   一行明写映射，不是任何自动规则。少写这一行映射，app 就读不到，也不会报错。）
3. **`db-password` 这个 Secret 被注入成了一个产码不读的变量名。** 骨架版写的是
   `- name: DB_PASSWORD` + `key: db-password`，看着两边都对，实际上环境变量这一侧的名字必须是
   `POSTGRES_PASSWORD`：`internal/pkg/db/db.go:38-44` 先取 `database.postgres.password`
   （这个键已按私域合规 §7.2 从 `config.yaml` 移除 ⇒ 恒为空），再退到
   `os.Getenv("POSTGRES_PASSWORD")`，两处都空就 `panic("数据库连接密码缺失…")`。
   今日实测 `DB_PASSWORD` 在 `user-server` 的 Go 代码里**零** `os.Getenv` 命中
   （全仓唯一还在用这个名字的是 `scripts/bridge-monitor.sh:52`，而它是**赋值的一侧**：
   `DB_PASSWORD="${BRIDGE_DB_PASSWORD:-${POSTGRES_PASSWORD:-}}"`，喂的是 bridge 那个进程，
   不构成 user-server 的读取点），
   而 `docs/operations/secret_rotation.md:25` 的登记表写的正是 `POSTGRES_PASSWORD` ⇒ chart 是这处漂移的
   唯一源头。装出来的故障形态是"Secret 建了、Pod 起了、InitDB panic"，报的还是密码缺失，
   跟"名字写错了"看着不像一回事。现在 `user-server/config.yaml:57-59` 的注释也同步成
   `POSTGRES_PASSWORD`（原先那句"宿主机运行时由环境变量 DB_PASSWORD 注入"同样是错的）。
   断言：改回旧名、或整条删掉，都会被 `ports_test.go` 的 `EnvCarriesFailFastSecrets` 拦下。
4. **`ingress.enabled: true` + `host: api.hivemtk.io`，而 `hivemtk.io` 不解析。**
   实测 `dig +short hivemtk.io A` 返回空；同文件 `Chart.yaml` 的 `home: https://hivemtk.io`、
   `sources: https://github.com/hivemtk/hivemtk` 也全是空的（`gh api repos/hivemtk/hivemtk`、
   `gh api users/hivemtk` 都 404 —— 那个 org 不存在），`maintainers.email` 同域，收不到信。
   现在 `ingress.enabled` 默认 false、host 换成 README §自定义 values 早就在用的占位符
   `api.example.com`，`Chart.yaml` 的 home/sources 改成本批离线部署后的真实对外面
   （Pages 官网 + Gitee 主仓 / GitHub 镜像），email 字段删掉。

### 仍然存在的限制

- [ ] **未实现** HPA (autoscaling.enabled 骨架默认 false)
- [ ] **未实现** PodDisruptionBudget (podDisruptionBudget.enabled 骨架默认 false)
- [ ] **未实现** ServiceAccount / NetworkPolicy
- [ ] **未实现** PostgreSQL/Redis 子 Chart 依赖
- [ ] **未实现** platform-server / user-web 子 Chart
- [ ] **未测试** helm lint 之外的 staging 验证 (后续 OPT-MISC-04 ~ OPT-MISC-06 实施)
- [ ] **未集成** secrets 轮换 (OPT-SEC-08 策略文档另议)

## 设计原则

1. **最小骨架原则**: 仅含 Deployment + Service + Ingress, 后续按 OPT 任务增量
2. **配置外置**: 所有可变项集中在 `values.yaml`, 不在模板硬编码
3. **Secret 零明文**: 密钥引用 `existingSecret` (`hivemtk-secrets`), 真实密钥由外部系统 (Vault / ExternalSecrets) 同步
4. **标签规范**: 遵循 [k8s 通用标签](https://kubernetes.io/docs/concepts/overview/working-with-objects/common-labels/)
