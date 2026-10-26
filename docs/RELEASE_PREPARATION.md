# 发布准备：配置与安装部署逻辑核查（用户端 · 平台端）

> 目的：把**用户端（hivemtk 仓）**与**平台端（hivemtk-platform 仓）**的配置文件、数据配置逻辑、安装部署逻辑核对成一份事实清单，作为**发布代码到服务器**前的准备依据。
> 本文只做"核查与清单"，不复述安装步骤——安装步骤见 [DEPLOYMENT_GUIDE.md](DEPLOYMENT_GUIDE.md)、[operations/MERCHANT_DEPLOYMENT.md](operations/MERCHANT_DEPLOYMENT.md)。
> 核查日期：2026-10-18（分支 master）。端口以 [PORT_REGISTRY.md](PORT_REGISTRY.md) + `user-server/internal/config/ports.go` 为唯一权威。

---

## 0. 架构基线（决定一切发布动作）

- **2026-08-17 重构后：Docker 只承载数据层（PG + Redis），user-server、platform-server、推理栈、前端全部跑宿主机进程**。生产 Dockerfile 已删除，仓内仅剩 dev 用 `user-server/Dockerfile.dev`。
- **两个独立 git 仓**：
  - 用户端：`…/go/hivemtk/hivemtk`（本文以 `R/` 代指）——user-server + user-web + embed-sdk + 数据层 compose；
  - 平台端：`…/go/hivemtk/hivemtk-platform`（本文以 `P/` 代指）——platform-server + platform-web + platform-contributor + 平台 PG compose。
- **发布一律手工，仓库内无发布脚本**（2026-10-23：`scripts/deploy-user.sh` / `scripts/post_deploy_check.sh` 已删除）：本地构建 → 上传到线上服务器指定目录 → 重启服务。官网发布步骤见 `website/README.md`「发布」章节；后端发布见本文 §发布流程。
- **线上主机**：`101.35.252.112:57122`（`~/.ssh/config` 别名 `mtk-prod`），站点/服务根 `/www/wwwroot/hivemtk/{website,user-web,user-server,platform-web,platform-contributor,platform-server}`；HTTPS 证书在宝塔（`/www/server/panel/vhost/cert/xapptool.cn/`）；数据库是同机容器 `mtk-postgres`(127.0.0.1:8202) / `mtk-redis`(127.0.0.1:8203)，平台端 `mtk-platform-postgres`(8201)。
- **仓内没有 systemd unit、没有生效的 nginx 配置、没有 CI 部署任务**——进程守护与反代由站点自建，仓内只有模板（`docs/operations/reverse-proxy/*.template`）。
- **在服外部依赖（别当死代码删）**：官网 `website/index.html` 内联注入 `https://work.xapptool.cn/embed/marketing-chat-widget.iife.js`（右下角客服，2026-10-23 实测 200/11984B）。判死规约、依赖清单与发布后自查命令见 `~/Documents/www/go/hivemtk/deploy/README.md` §9（`deploy/` 已于 2026-10-26 迁入工作区根，不入 git）。
- 平台端对用户端是**可选接入**：`PLATFORM_ENABLED=true` 才启用，默认关。

---

## 1. 用户端（user-server）配置逻辑

### 1.1 配置文件清单

| 文件 | 是否入库 | 作用 |
|---|---|---|
| `R/.env-example`（73 键） | ✅ | **根环境变量模板**，`make install` 复制为 `R/.env` |
| `R/.env`（72 键） | ❌ gitignore | 本机真值；**比 example 多 `MASTER_KEY`、`SEED_PASSWORD`、`HIVEMTK_ADMIN_PASS` 三键** |
| `R/user-server/config.yaml` | ✅（入库，无 .example） | 主配置：platform_api / external / security / logging / database / vector_database / inference / storage / i18n |
| `R/user-server/config/platform.yaml` | ✅ | 平台集成（仅 `PLATFORM_ENABLED=true` 时读）：api_url / secret / admin_password |
| `R/user-server/.env`（4 键） | ❌ gitignore | `VISITOR_TOKEN_SECRET` / `USER_JWT_SECRET` / `POSTGRES_PASSWORD` / `FIELD_ENCRYPTION_KEY`（模板 `.env.example`） |
| `R/user-server/config/.merchant_api_secret`、`.merchant_key` | ❌ 0600 | 文件型密钥 |
| `R/.env.geo.local` | ❌ | 本地私有覆盖（**含真实 LLM API key**），未发现产码读取点 |
| `.env/admin.json`（或 `ADMIN_CONFIG_FILE`） | — | 管理员 UI 行为配置（无密码字段） |

无 `configs/` 目录、无 toml、无 viper、主服务**无命令行 flag**。

### 1.2 加载顺序（优先级）

1. **`.env` 注入**：`cmd/api/main.go:111` `utils.LoadDotEnv(".env")` —— **路径相对进程 CWD**，不覆盖已有环境变量。即：在 `user-server/` 目录起进程时读到的是 `user-server/.env`；**根 `.env` 必须由启动 shell 注入**（`set -a && source .env && set +a` 后再启动，原 `deploy-user.sh` 的手工等价做法）。
2. **JWT 密钥**：`internal/pkg/utils/jwt.go` `loadJWTSecret()` → `USER_JWT_SECRET` > `JWT_SECRET`，缺失或 <32 字符直接 **panic**。⚠️ `config.yaml:43 security.jwt_secret` **没有任何 Go 代码读取**（已核实），配了也不生效；JWT 有效期硬编码 24h，`JWT_EXPIRE` **无读取点**（R0 批次已从 `.env` / `.env-example` 删除，代码无任何变化）。
3. **`config.yaml` 查找**：`internal/config/server.go` `loadAppConfigOnce()` 顺序 `config.yaml` → `../config.yaml` → `user-server/config.yaml` → `<exeDir>/config.yaml` → `<exeDir>/../config.yaml`；全失败回落 Docker 网络默认（`postgres-user:8202`）。
4. **占位符插值**：`${VAR:default}` 语法；**总优先级：环境变量 > config.yaml > 代码默认值**（例外：`PUBLIC_BASE_URL` env > yaml > `X-Forwarded-*` > Host；proxy：yaml > `HTTP(S)_PROXY` env > 直连）。
5. **密钥文件通道**：`HIVEMTK_SECRETS_DIR`（默认 `/run/secrets`）注入 env；**`MASTER_KEY` 缺失时非 dev 姿态直接 `os.Exit(1)`**。dev 判定：`APP_ENV` > `MODE` > `GIN_MODE=debug`。
6. **DB 密码只认 env `POSTGRES_PASSWORD`**（`internal/pkg/db/db.go` 缺失即 panic；config.yaml 的 password 字段已移除）。
7. **Redis**：`REDIS_HOST` 为空 ⇒ 不启用（缓存/健康检查降级）；端口默认 8203。
8. **DB 参数中心（运行期热改）**：启动 seed `config_params` 表并注入下层包；**LLM 提供商/路由真相源在库表** `llm_providers` / `llm_routing_rules`，config.yaml 的 `inference.llm` 仅兜底。

### 1.3 关键配置项（发布相关）

| 项 | 来源 | 示例/要求 |
|---|---|---|
| DB host/port/user/dbname | `config.yaml database.postgres` | `127.0.0.1:8202`、`admin`、`user_db`；`DB_HOST/DB_PORT` env 可覆盖 |
| DB password | env `POSTGRES_PASSWORD` | 强密码，**唯一来源** |
| JWT 密钥 | env `USER_JWT_SECRET` 或 `JWT_SECRET` | ≥32 字符，二选一 |
| 字段加密 | env `FIELD_ENCRYPTION_KEY` | ≥32；**不可直接轮换**（先解密存量） |
| 主加密根 | env `MASTER_KEY` | ≥32 字节，**非 dev 缺失即退出**；⚠️ 不在 `.env-example` 里 |
| 平台集成 | `PLATFORM_ENABLED` / `PLATFORM_API_HOST` / `MERCHANT_API_SECRET` / `PLATFORM_ADMIN_PASSWORD` | 开态必配，且与平台端**两侧同值** |
| 租户开关 | env `TENANT_MODE` | `off`（默认，离线单机）/ `on`（SaaS 多租户）；未知值按 off 并告警 |
| 公网基座 | env `PUBLIC_BASE_URL`（> yaml `external.public_base_url`） | https、无尾斜杠；留空 → Telegram 等降级 polling |
| 监听 | env `SERVER_HOST` / `PORT` | 默认 `0.0.0.0:8204` |
| 对象存储 | env `QINIU_*` | 留空时上传口返回 503 |
| 推理栈 | `config.yaml inference` + `.env` 的 `LLM_*/EMBEDDING_*/RERANK_*` | ⚠️ **本机特例：`inference.llm.base_url=127.0.0.1:8787`（本机回环网关），换机发布必须核对改回 8207**（`config.yaml:87` 注释自述） |
| 日志 | `config.yaml logging` | json / `logs/user-server.log` / max 200MB |
| 安装锁 | env `INSTALL_LOCK_PATH` | **生产必须显式绝对路径**，否则 CWD 相对、换目录重启会铸新 install_id |

生产必配/场景必配/仅存在于代码里的高危键三张全表见 `docs/DEPLOYMENT_GUIDE.md` §6.1–§6.2。注意：生产代码读约 **179+ 个 env 键**，仅 72 个在 `.env-example`，`scripts/env-coverage.baseline` 登记了 91 个债——**不要假设文档已全覆盖**。

### 1.4 数据配置逻辑（DB 初始化）

| 环节 | 方式 | 位置 |
|---|---|---|
| 扩展与核心表 | **手动 SQL，仅首次**：`migrations/init-user-db.sql`（vector/uuid-ossp、knowledge_embeddings 1024 维等） | docker 挂载 `docker-entrypoint-initdb.d/01-init.sql` 或 `psql -f` |
| 业务表 | **每次启动 GORM AutoMigrate**（`allModels()`）；存量表加列走版本化迁移 | `internal/pkg/db/migrate.go` |
| 编号 SQL `002~058`（55 个） | **Go 不会自动执行**，仅供精确复现历史结构手工按序跑 | `migrations/` |
| 启动期升级 | `ExecuteUpgrade("v1.0.0","v1.0.0")` **固定空跑** | `cmd/api/main.go:389-400` |
| 参数中心 seed | 启动即 seed `config_params` + 分组元数据 + Wire 注入 | `main.go:151-260` |
| 安装向导 | `GET /api/system/init-status`（公开）；`POST /api/system/init-admin` 等三条挂 `SetupOnlyGuard`（已初始化 → 403） | `internal/router/admin_routes.go:57-66`、`internal/middleware/init_guard.go` |
| init-admin | 落库 + **写 `install.lock`**（失败即报错）；请求体含 `agreement_version` | `internal/service/auth.go:653/877`、`internal/system/install/install.go` |
| tenants 表 | `TENANT_MODE=on` 时**先建 tenants 行再插超管**（reg_mode=offline）；off 不产生租户行 | `auth.go:867-916` |
| install.lock | 字段 `install_id/install_time/admin_username/initialized/version`；`INSTALL_LOCK_PATH` > `./install.lock`（CWD 相对）；⚠️ `version` 字段当前**无写入方**，恒空 | `internal/system/install/install.go` |
| 演示种子（可选） | `go run ./cmd/seed` 或 `scripts/bootstrap.sh`；口令链 `SEED_PASSWORD` > `ADMIN_PASSWORD` > 公开默认 `Seed@123456` | `user-server/cmd/seed` |
| 超管口令守卫 | `system_users` id=1 触发器禁改/删；换口令必须 `scripts/rotate-admin-password.sh --with-env` | `v3_36_0_admin_password_guard_migration.go` |

---

## 2. 平台端（hivemtk-platform）配置逻辑

> platform-server **不在本仓**，在 `P/ = …/go/hivemtk/hivemtk-platform`（remote gitee.com/xhpmayun/hivemtk-platform）。

### 2.1 配置文件清单

| 文件 | 是否入库 | 作用 |
|---|---|---|
| `P/platform-server/config.yaml` | ✅ | 宿主机部署主配置（PG host=`127.0.0.1`、port 8201） |
| `P/platform-server/config.docker.yaml` | ✅ | 容器开发配置（PG host=`postgres-platform`），由 `SERVER_CONFIG` env 选用 |
| `P/.env.example`（13+ 键） | ✅ | 模板：POSTGRES_*、JWT_SECRET、PLATFORM_ADMIN_PASSWORD、MERCHANT_API_SECRET、CORS 等 |
| `P/.env`、`P/platform-server/.env` | ❌ gitignore | 真值 |
| `P/platform-web/.env.development` / `.env.production` | ✅ | 只有 `VITE_API_BASE_URL=/`（同源相对路径）、`VITE_USE_MOCK` —— **生产无密钥** |

### 2.2 加载顺序（优先级）

1. `cmd/api/main.go:40` `config.LoadDotEnv(".env")`（CWD 相对，不覆盖已有环境变量 → 编排注入优先）。
2. `SERVER_CONFIG` env 可整体替换配置文件路径；否则按 `config.yaml` → `../` → `../../` → `/etc/marketing/` 回退（`internal/config/config_viper.go:120-132`）。
3. 读文件后先做 `${VAR}` / `${VAR:-default}` 展开，再喂 viper；viper env 覆盖前缀 `MARKETING_`（实际很少用）。
4. **fail-closed 校验：`POSTGRES_PASSWORD` / `PLATFORM_ADMIN_PASSWORD` / `MERCHANT_API_SECRET` / `JWT_SECRET` 四项任一为空 → 启动报错**（`parseConfig()`）。
5. JWT secret：`JWT_SECRET`（备选 `PLATFORM_JWT_SECRET`），<32 字符拒绝；expire 默认 86400 秒。
6. ⚠️ DSN 由 host/port/user/password/dbname **重组**，config.yaml 里的 `dsn` 字段实际不被 `db.Init` 使用。
7. DB 密码同样**只认 `POSTGRES_PASSWORD`**（compose 里 `${POSTGRES_PASSWORD:?}` 强制校验）。

**平台端未使用**：Redis（有常量无使用处）、对象存储（仅本地 `./uploads`，`STORAGE_LOCAL_BASE_DIR`）、短信、真实支付通道（`PaymentChannel` 为占位表）、SMTP（有结构体无业务代码）。

### 2.3 关键配置项

| 项 | 来源 | 示例/要求 |
|---|---|---|
| 端口 | `config.yaml server.port` | `8205`（唯一源 `internal/config/ports.go`） |
| DB | `config.yaml database.*` + env `POSTGRES_PASSWORD` | `127.0.0.1:8201` / `admin` / `platform_db` |
| 平台超管 | env `PLATFORM_ADMIN_PASSWORD` | 用户名默认 `admin`（`admin.username`），密码必须 env 注入；仅库内零管理员时 seed 首个 |
| 商户共享密钥 | env `MERCHANT_API_SECRET` | ≥32 字符，HMAC 签名；**与用户端同值** |
| JWT | env `JWT_SECRET`（备选 `PLATFORM_JWT_SECRET`） | ≥32 字符 |
| 贡献者 JWT | env `CONTRIBUTOR_JWT_SECRET` | 独立 ≥32（.env.example 未列出但 .env 有） |
| CORS | env `CORS_ALLOW_ORIGINS_PLATFORM` | 同源部署可空 |
| 上传目录 | env `STORAGE_LOCAL_BASE_DIR` | 缺省 `./uploads` |
| 模块种子覆盖 | env `MODULE_SEED_FILE` | 优先于内嵌 `module_catalog_seed.json` |
| 请求体上限 | env `MAX_JSON_BODY_MB` | — |
| **协议版本** | **代码常量，不在配置** | `CurrentAgreementVersion = "2026-10-17.v1"`（`service/tenant_registration_service.go:24`）；资产 schema `"1.0"` |

### 2.4 数据初始化

1. **SQL init（仅首次建卷）**：`P/migrations/init-platform-db.sql`（221 行）挂进 `docker-entrypoint-initdb.d/01-init.sql`——建 merchants / platform_admins / leads / platform_installs / platform_heartbeats / site_contact_config 等，并 INSERT 一条站点联系默认行。
2. **每次启动**：GORM AutoMigrate（约 34 模型，失败仅 warning）+ **`idempotentDDL` 幂等手写 DDL 兜底**（`ADD COLUMN IF NOT EXISTS` 补齐 module_catalog、tenant_registrations、orders 等——AutoMigrate 因历史约束命名漂移会静默中断，故有此层）。
3. **管理员 seed**：`EnsureSeededAdmin()` 仅当库内零管理员时用 `PLATFORM_ADMIN_PASSWORD` bcrypt 建首个超管；**改密码后以库为准**。
4. **模块目录 seed**：内嵌 JSON，需管理端 API `POST /platform/module-catalog/seed` **手动触发**，非启动自动。
5. **资产市场示例**：`go run ./cmd/seed`（25 个示例资产 + demo_contributor）或 SQL `migrations/032_industry_assets_platform_seed.sql`（幂等）。

### 2.5 前端（platform-web）

- Vue3 + Vite 8 + Element Plus；dev 端口 8212，proxy `/platform|/api|/public|/merchant-api|/files` → `localhost:8205`（`/api/manage/config-params` 特例 → 8204）。
- 生产 `VITE_API_BASE_URL=/`（同源相对路径，无 CORS 依赖）；`npm ci && npm run build` → `platform-web/dist/`。
- **两条部署路径并存**：
  1. **宝塔静态站点**（发布脚本主推）：`scripts/deploy-platform.sh` rsync `dist/` → `$WEBROOT_PLATFORM_WEB`，宝塔 nginx 同源反代 → `127.0.0.1:8205`；
  2. **Go 直接托管**：`platform-server/internal/router/frontend_static.go` 由后端进程直接 serve `platform-web/dist`（`PLATFORM_WEB_DIST` env → `../platform-web/dist` → `./platform-web-dist`），SPA fallback + `/assets` 长缓存；`platform-contributor/dist` 挂 `/contributor/`。
- platform-web 的 Dockerfile + nginx 已于 2026-09-03 删除（退役）；**仓内无现成 nginx vhost 模板**。

---

## 3. 两端对接点

- 用户端读 `R/user-server/config/platform.yaml`（仅 `PLATFORM_ENABLED=true`）：`api_url: "${PLATFORM_API_HOST}"`、`secret: "${MERCHANT_API_SECRET}"`、`admin_password: "${PLATFORM_ADMIN_PASSWORD}"`。
- 地址优先级：`PLATFORM_API_HOST` > `PLATFORM_API_URL` > yaml `api_url`。
- **共享密钥两侧必须同值**：`MERCHANT_API_SECRET`、`PLATFORM_ADMIN_PASSWORD`（轮换时用 `scripts/rotate-secrets.sh merchant_hmac` 双侧同步）。
- 上报/心跳用 `install_id` 作为商户凭证（`internal/service/tenant.go currentInstallID`）。

---

## 4. 安装部署逻辑

### 4.1 用户端部署产物

| 产物 | 要点 |
|---|---|
| `R/Makefile`（634 行） | `install`（cp .env-example→.env + web/sdk build + 推理栈 + db-up）、`db-up/down/ps/logs`、**`db-backup`/`db-restore FILE=`**、`user-build`/`web-build`/`sdk-build`、`inference-host-*`、`audit`/`audit-artifacts`。⚠️ **`up/down/restart/logs/ps/build` 旧目标只有 `.PHONY 名、无规则`**，敲了"退出 0 但什么都不做" |
| `R/docker-compose.yml` | **仅数据层**：`mtk-postgres`（pgvector/pg15，127.0.0.1:8202，挂 `init-user-db.sql`，卷 `mtk_user_pg_data`）、`mtk-redis`（redis:7-alpine，8203，`--requirepass ${REDIS_PASSWORD:?}`）。密码缺失即拒启 |
| 用户端后端发布 | **手工三步**（脚本已删除）：`CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o user-server-bin ./cmd/api` → `rsync` 到 `mtk-prod:/www/wwwroot/hivemtk/user-server/` → 远端 `set -a && source .env && set +a` 后重启进程，30s 内 `curl 127.0.0.1:8204/api/health` 校验 |
| `scripts/bootstrap.sh` | 首次初始化：等健康 → 跑 027/028 SQL → Go seed → Python 知识库种子 → 核对 `install.lock.initialized` → admin 登录冒烟 |
| 部署后验收 | **手工**（脚本已删除）：按 G0–G4 分组逐项 `curl` 健康端点 + `docker ps` 看容器 + 关键日志 `tail`，判据见 `docs/operations/KNOWLEDGE_GROUP_DEPLOY.md` §5 |
| 密钥轮换 | `scripts/rotate-secrets.sh`（db_user/db_platform/merchant_hmac/jwt_user）、`rotate-admin-password.sh`、`scripts/ops/rotate-*.sh` |
| 反代模板 | `docs/operations/reverse-proxy/nginx.conf.template`（`http2 off`、SSE `buffering off` 24h、WS Upgrade 透传）+ caddy/traefik/frpc 模板 |
| K8s | `deploy/helm/hivemtk/` 骨架（**未经 staging 实测**，targetPort 8204，Secret 5 键） |
| CI | `.github/workflows/` 15 个：质量门 + `release.yml`（tag→bridge zip + SBOM + Release，**不产二进制、无镜像构建**）；`website-pages.yml` 是唯一真"部署"（GitHub Pages） |
| 健康检查 | `GET /healthz`、`/readyz`、`/health`（`internal/router/router.go:200-202`） |

### 4.2 平台端部署产物

| 产物 | 要点 |
|---|---|
| `P/Makefile` | `install`（复制 .env + 起 PG）、`up/down/logs/ps`、`backup/restore`、`server-build`（CGO_ENABLED=0 → `platform-server/bin/`）、`dev`（air） |
| `P/docker-compose.yml` | **仅一个服务 `postgres-platform`**（pgvector:pg15，127.0.0.1:8201，卷 `mtk_platform_pg_data`，限 1C/1G，`${POSTGRES_PASSWORD:?}` 强制校验） |
| `P/scripts/deploy-platform.sh`（481 行） | 四模式 `--admin-web` / `--contributor` / `--api` / `--all`；`--preflight-only` / `--dry-run` / `--frpc-template`。`--api`：git push → 远端 pull → **本地交叉编译 linux/amd64** → scp → 远端 compose up（仅 PG）→ pkill + `source .env` + nohup → `curl 127.0.0.1:8205/health`。**`DEPLOY_HOST` 无默认值必须显式传** |
| 远端目录 | 默认 `REMOTE_DEPLOY_DIR=/www/wwwroot/hivemtk-platform` |
| 发布文档 | `P/发布流程.md`（端口表、env 必填表、手动上线步骤、排错） |
| CI | `platform-ci.yml` **从未执行**（仓在 Gitee，GitHub 无执行体，文件头注释明示）；无 systemd、无生产 Dockerfile（仅 `Dockerfile.dev`） |

### 4.3 构建口径

| 服务 | 入口 | 命令 | 产物 | Go |
|---|---|---|---|---|
| user-server | `R/user-server/cmd/api/main.go` | `make user-build`（`CGO_ENABLED=0 go build -o bin/user-server ./cmd/api`） | `user-server/bin/user-server`（~73MB） | go 1.26.0 / toolchain 1.26.9 |
| platform-server | `P/platform-server/cmd/api/main.go` | `make -C hivemtk-platform server-build`；远端用 `GOOS=linux GOARCH=amd64` 交叉编译 | `platform-server/bin/platform-server` | go 1.25.0 / toolchain 1.26.6 |
| user-web | — | `make web-build` | `user-web/dist`（可由 user-server 托管，`embed_static_routes.go`） | Node |
| platform-web | — | `npm ci && npm run build` | `platform-web/dist` | Node 20 |

---

## 5. 发布到服务器：准备清单

### 5.1 必带文件（git 拉代码不会带过去）

- [ ] 根 `R/.env`（72 键，**gitignore**；含 `MASTER_KEY`、`SEED_PASSWORD`——这两个**不在 `.env-example` 里**，照 example 起生产会 `os.Exit(1)`）
- [ ] `R/user-server/.env`（4 键，gitignore）——与根 `.env` **不同名键会同时生效**
- [ ] `P/.env`（平台端，gitignore）+ `P/platform-server/.env`（如用）
- [ ] `.env.geo.local`（如需真实 LLM key，**建议先轮换**——曾含明文第三方 key）
- [ ] `install.lock`（若为既有商户迁移；否则新装由 init-admin 生成）
- [ ] `user-server/config.yaml`、`user-server/config/platform.yaml`（入库，但需按目标机核对，见 5.3）

### 5.2 必配密钥（fail-fast 项）

| 键 | 要求 | 失败形态 |
|---|---|---|
| `POSTGRES_PASSWORD` | 强密码 | compose `:?` 拒启 / Go panic |
| `REDIS_PASSWORD` | ≥32 | compose `:?` 拒启 |
| `JWT_SECRET` 或 `USER_JWT_SECRET` | ≥32（一把即可） | 启动 panic |
| `MASTER_KEY` | ≥32 字节 | **非 dev 直接 `os.Exit(1)`** |
| `FIELD_ENCRYPTION_KEY` | ≥32；轮换前先解密存量 | 存量 api_logs/audit/SMTP 凭据失效 |
| `PLATFORM_ADMIN_PASSWORD` + `MERCHANT_API_SECRET` | 仅平台开态，两侧同值 | 平台端启动报错 / 用户端对接失败 |
| `SEED_PASSWORD` | 对外必设（默认 `Seed@123456` 是公开值） | 演示口令泄露 |

### 5.3 换机核对项

- [ ] `config.yaml inference.llm.base_url=127.0.0.1:8787` 是**本机回环网关特例** → 换机改回 `8207`（`config.yaml:87` 注释）
- [ ] `DB_HOST/DB_PORT`（默认 `127.0.0.1:8202`）、`REDIS_HOST/PORT`、平台 `8201` 是否与目标机一致
- [ ] `PUBLIC_BASE_URL`（https、无尾斜杠）+ `REQUIRE_PRIVATE_NETWORK=true` 联动（公网 IP 会拒启）
- [ ] `INSTALL_LOCK_PATH=绝对路径`（否则 CWD 相对，换目录重启铸新 install_id）
- [ ] `TENANT_MODE`（off/on）符合该实例形态
- [ ] `SERVER_HOST`（默认 0.0.0.0，如需收回网口设 127.0.0.1）
- [ ] `GIN_MODE=release`；`APP_ENV` 不设 = 生产最严（`ALLOW_INSECURE_*` 三把旁路生产误设=拒启）

### 5.4 启动命令（env 注入是关键差异）

```bash
# 用户端：根 .env 由 shell 注入，进程 CWD=user-server
cd "$DEPLOY_DIR" && set -a && source .env && set +a && cd user-server && \
  nohup ../user-server/bin/user-server > logs/user-server.log 2>&1 &

# 平台端（deploy-platform.sh 同款）
cd "$REMOTE_DEPLOY_DIR" && set -a && source .env && set +a && cd platform-server && \
  nohup ./bin/platform-server > logs/platform.log 2>&1 &
```

⚠️ 不要照抄 `DEPLOYMENT_GUIDE.md` §5 的 `cd user-server && ./bin/user-server`（未 source 根 `.env`，进程只读到 `user-server/.env`）。

### 5.5 验收

- [ ] `curl 127.0.0.1:8204/api/health`、`/healthz`、`/readyz`（用户端）；`curl 127.0.0.1:8205/health`（平台端）
- [ ] `curl 127.0.0.1:8204/api/system/init-status` → `initialized:true`
- [ ] 手工跑 G0–G4 验收（健康端点 + 容器 + 日志，判据见 `docs/operations/KNOWLEDGE_GROUP_DEPLOY.md` §5）
- [ ] `bash scripts/check-admin-default-credential.sh`（超管默认口令检查）
- [ ] `make db-backup` 出一份基线备份

### 5.6 版本号 / 协议版本同步点（发版时）

| 位置 | 值 |
|---|---|
| `user-server/cmd/api/main.go` swagger 注解 + `docs/{docs.go,swagger.json,swagger.yaml}` | **`3.42.0`**（全仓唯一对外应用版本号） |
| `user-server/internal/migration/migrations/*.go` `Version()` | `v1.1.0 … v3.60.0`（新增迁移须发版带上） |
| 用户协议版本 `service/tenant.go serviceCurrentAgreementVersion` / 平台侧 `tenant_registration_service.go` | `2026-10-17.v1`（**两侧需一致**） |
| 桥接协议 `internal/channelgw/protocol.go CurrentProtocolVersion` | `2`（改动影响扩展与后端配套发布） |
| `install.lock` `version` 字段 | 恒空（无写入方，心跳回落 `unknown`） |
| `deploy/helm/hivemtk/Chart.yaml` / 扩展 `manifest.json` / 各 `package.json` | 0.1.0 / 1.0.0 / 0.0.0 |
| `CHANGELOG.md` | 只有 `[未发布]` 段；**git tag = 0** |

### 5.7 已知坑速查

1. `.env` 全部 gitignore → 发布手工带；`.env-example` 缺 `MASTER_KEY`/`SEED_PASSWORD`（已补进 .env-example）。
2. `make up/down/restart` 等旧目标**无规则**，退出 0 但什么都不做（已改：敲了显式 exit 1 并提示用 db-up/db-down/db-logs）。
3. `config.yaml security.jwt_secret` **无代码读取点**，配了无效；`.env` 的 `JWT_EXPIRE` 同样无读取点，R0 批次已删该键（现只剩文档页示例文案）。
4. 编号 SQL（002~058）**Go 不自动执行**；建表主通道是 AutoMigrate + 首次 init SQL。
5. 无 systemd / 无生效 nginx / 无 CI 部署——守护与反代站点自建（仓内只有模板；已新增 deploy/systemd/* 模板）。
6. 旧云端已退役，`DEPLOY_HOST` 无默认值。
7. `FIELD_ENCRYPTION_KEY` 不可直接轮换；超管口令有 DB 触发器守卫，必须用 `rotate-admin-password.sh`。
8. `ONEID_SALT` 必须在有客户数据前定，改值存量全错位。
9. user-server 实际会托管 `../user-web/dist`（`embed_static_routes.go`），与 `DEPLOYMENT_GUIDE.md` §5"不含静态托管段"**存在文档漂移**。
10. 平台端 `platform-ci.yml` 从未执行（Gitee 仓、GitHub workflow），**别把绿勾当质量门**。
11. 健康检查假绿 → 已改硬失败（SKIP_HEALTHCHECK 逃生）。
12. 备份不可用 → 已修 make db-backup/-restore 且发布前自动备份。
13. pkill 误杀 → 两端已防护。
14. 无守护 → 新增 deploy/systemd/* 模板。
15. MASTER_KEY/SEED_PASSWORD 缺失 → 已补进 .env-example。
16. 跨仓漂移：scripts/check-cross-repo-drift.sh。

---

## 6. 与已有文档的边界

| 已有文档 | 覆盖 | 本文不重复 |
|---|---|---|
| `docs/DEPLOYMENT_GUIDE.md`（530 行） | 六步安装、env 全表 §6、三种发布模式、备份/升级回滚 | 安装步骤 |
| `docs/operations/MERCHANT_DEPLOYMENT.md` | 商户 5 步交付 | 交付流程 |
| `docs/handbook/OPERATIONS-FOR-HUMANS.md` | 人读 SOP | 日常操作 |
| `user-server/docs/dev/DEVELOPMENT.md` §9.4 | 构建面部署检查清单 | 构建细节 |
| `P/发布流程.md` | 平台端手动上线步骤 | 平台端操作步骤 |
| `docs/operations/AI_AGENT_PERF_DEPLOY.md` §三~§四 | 灰度发布与紧急回滚方法论 | 发布执行 |
| `docs/PORT_REGISTRY.md` | 端口唯一权威 | — |

本文定位：**发布前的配置/数据/部署逻辑核查 + 准备清单**，是上述操作文档的"前置核对层"。
