# HiveMTK 端口注册表（权威单一文档源）

> 本文件是仓库所有端口/URL 的**唯一权威文档**。\
> 代码单一源：`hivemtk/user-server/internal/config/ports.go`\
> 审计脚本：`hivemtk/scripts/audit-cross-package-ports.sh`\
> 改动流程：先改 ports.go → 跑审计脚本 → 同步本文件 → 同步 .env-example / docker-compose.yml / config.yaml 注释\
> **严禁不经流程随意改端口**——改完必须 `bash scripts/audit-cross-package-ports.sh` 通过

***

## 一、端口对照表

| 端口       | 服务               | 模式            | 容器内       | 宿主机默认                           | 宿主机可覆盖                         | 配置键                            | 说明                                    |
| -------- | ---------------- | ------------- | --------- | ------------------------------- | ------------------------------ | ------------------------------ | ------------------------------------- |
| **8202** | PostgreSQL       | Docker 数据层    | ✅ 固定 8202 | 8202（docker-compose 默认）         | `USER_POSTGRES_HOST_PORT`      | `DB_PORT`（config.yaml 默认 8232） | **容器内固定 8202**，宿主机映射可改                |
| **8232** | PostgreSQL       | 宿主机直连（Dev）    | —         | 8232（ports.go DefaultDBPortDev） | `DB_PORT`（config.yaml 默认 8232） | `DB_PORT`                      | 避开 8202（历史遗留其他服务占用），**只在宿主机直连模式使用**   |
| **8203** | Redis            | Docker 数据层    | ✅ 8203    | 8203                            | `REDIS_HOST_PORT`              | `REDIS_PORT`                   | 与容器内同号                                |
| **8204** | user-server API  | Docker（dev 热重载） | ✅ 8204 | 127.0.0.1:8204（docker-proxy 转发） | `PORT`（服务端）+ `USER_SERVER_PORT`（脚本侧） | `PORT` / `SERVER_HOST`         | 主 API + WebSocket + Swagger。容器 `mtk-user-server-dev`（`docker-compose.dev.yml`，air 监听源码 bind-mount 即时重编）。**禁止宿主机 `make dev`/air 同起抢 8204**（见 §2 模式 C）。`SERVER_HOST=127.0.0.1` 收回本机（读点 `cmd/api/main.go` `resolveListenAddr`）。注意 Go 进程只读 `PORT`：`USER_SERVER_PORT` 只被 `bootstrap.sh` / `deploy-user.sh` / `rotate-secrets.sh` 拿来拼 curl 目标，**挪不动服务监听端口**，改端口两个必须一起改 |
| **8205** | platform API     | 独立服务          | —         | 8205                            | `PLATFORM_API_HOST`（host:port） | `PLATFORM_API_URL`             | 平台端，与用户端物理隔离                          |
| **8206** | Chromium CDP（可选） | 宿主机           | —         | 8206                            | `CDP_PORT`                     | —                              | 截图/PDF 功能用，未启用可不占用                    |
| **8207** | LLM 推理           | 宿主机 llama-server | —      | 8207                            | `LLM_BASE_URL`                 | `inference.llm.base_url`       | OpenAI 兼容，served name 取 `.env` 的 `LLM_SERVED_NAME`（现值 Qwen2.5-3B-Instruct） |
| **8208** | Embedding 推理     | 宿主机 llama     | —         | 8208                            | `EMBEDDING_BASE_URL`           | `inference.embedding.base_url` | **私域部署强制本地**（数据不出域）                   |
| **8209** | Rerank 推理        | 宿主机 llama     | —         | 8209                            | `RERANK_BASE_URL`              | `inference.rerank.base_url`    | 与 Embedding 同属 RAG 链路                 |

### 前端开发端口（外部依赖，不在 ports.go）

| 端口       | 用途                            | 配置位置                                  |
| -------- | ----------------------------- | ------------------------------------- |
| **8211** | Vite dev server（user-web，宿主机） | `user-web/vite.config.js`（port: 8211） |
| **8213** | Vite dev server（website，宿主机） | `website/vite.config.js`（port: 8213） |
| **8214** | Vite dev server（embed-sdk 预览，宿主机） | `embed-sdk/vite.config.js`（port: 8214，strictPort） |
| 8080 | 浏览器 WebSocket origin fallback | `internal/config/ws_origin.go`（允许列表）  |

> 平台端前端（另一仓库 `hivemtk-platform`）：platform-web=**8212**、platform-contributor=**8215**，均宿主机 vite。contributor 挨着 embed-sdk（8214/8215 相邻 82 段，见 platform 仓对应 DEVELOPMENT.md）。

***

## 二、两种部署模式的端口取值

```
┌─────────────────────────────────────────────────────────────────────┐
│ 模式 C（当前唯一 dev 模式，2026-09-29 起）：后端容器热重载 + 前端宿主机 │
│                                                                     │
│   后端 user-server = 容器 mtk-user-server-dev（air，源码 bind-mount）│
│     docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d user-server-dev
│   容器内 PG    = mtk-postgres:8202（compose 网络别名）               │
│   宿主机 PG 映射 = 127.0.0.1:8232（仅给宿主机工具直连用）            │
│   前端 vite 全在宿主机（8211/8213/8214），经 127.0.0.1:8204 连后端   │
│                                                                     │
│   铁律：后端只跑容器，不跑宿主机。宿主机 `make dev`/air 会与         │
│   docker-proxy 双绑 8204，必须二选一时永远保留容器、停宿主机。       │
├─────────────────────────────────────────────────────────────────────┤
│ 模式 A（已退役）：Docker Compose（PG+Redis 容器化，user-server 宿主机）│
│ 模式 B（已退役）：宿主机直连（PG+Redis+user-server 全宿主机）        │
│   仅留作故障排查时的临时手段，用完即回模式 C。                       │
└─────────────────────────────────────────────────────────────────────┘
```

**当前本机实际运行（模式 C）**：

- `mtk-postgres` 容器内 8202 → 宿主机 **127.0.0.1:8232**（.env 覆盖）

- `mtk-redis` 容器内 8203 → 宿主机 **127.0.0.1:8203**

- `mtk-user-server-dev` 容器（air 热重载）监听 **127.0.0.1:8204**（docker-proxy 转发）

- 前端宿主机：user-web **8211**、website **8213**、embed-sdk **8214**（均 `npm run dev`）

- LLM/Embedding/Rerank **未启动**（环境变量指向 8207/8208/8209 但服务未起）

***

## 三、端口改动铁律

### 3.1 禁止随意改

```
✗ 不要在 shell 里临时改端口测试后忘了恢复
✗ 不要同时在 .env、.env-example、config.yaml、ports.go 四处瞎改
✗ 不要改完不跑审计脚本就提交
✗ 不要在宿主机起后端（make dev / air / go run）——后端只跑 docker dev 容器，与 docker-proxy 双绑 8204 必冲突
✗ 不要挪前端 vite 端口（8211/8213/8214/8212/8215 已定死，contributor 与 embed-sdk 必须相邻 82 段）

√ 改之前先读本文件 §2 确认当前运行模式
√ 改 ports.go → 跑 bash scripts/audit-cross-package-ports.sh → 同步本文件注释
√ 改完在 PR 描述里注明改了哪几个端口 + 为什么
```

### 3.2 改动流程

```
1. 确认要改的是「容器内端口」还是「宿主机映射端口」
   - 容器内（如 postgres - port=8202）：改 docker-compose.yml command 行 + ports.go DefaultDBPortDocker
   - 宿主机映射（如 USER_POSTGRES_HOST_PORT）：改 .env + .env-example + docker-compose.yml ports 映射 + ports.go DefaultDBPortDev
   - user-server 监听（`PORT`，可选 `SERVER_HOST`）：改 ports.go DefaultListenPort（宿主默认值）+ .env + .env-example

2. 代码层（必须先改）：
   vi hivemtk/user-server/internal/config/ports.go   ← 改常量值
   vi hivemtk/user-server/config.yaml               ← 改 default env var 占位符
   vi hivemtk/docker-compose.yml                    ← 改 ports 映射
   vi hivemtk/.env-example                          ← 改示例值

3. 跑审计：
   cd hivemtk && bash scripts/audit-cross-package-ports.sh
   # 输出：✓ user-server ports.go 全部常量已对齐
   #       ✓ docker-compose.yml 端口映射与 ports.go 一致
   #       ✓ .env-example 默认值与 ports.go 一致
   #       ✗ 某处硬编码 8202 但 ports.go 说 8232 → 修！

4. 更新本文档：
   改上方端口对照表 → 改 §2 模式说明 → 加一条「变更记录」

5. git commit -m "chore(port): XXX 端口 82XX → 82YY，原因 ZZZ"
```

### 3.3 历史教训

| 日期      | 问题                                        | 根因                                                              | 修复                                                             |
| ------- | ----------------------------------------- | --------------------------------------------------------------- | -------------------------------------------------------------- |
| 2026-09 | CV 验收时 user-server 报 EOF 大面积假失败           | shell 里临时 export DB\_PORT=8232，但 docker-compose 还映射 8202；两边各说各话 | export 必须与 .env 一致，改完立刻 `docker compose down && up -d` 让容器重新映射 |
| 2026-08 | 种子 FAQ 回答里 PG 端口前后矛盾                      | 代码常量 8202（Docker）和 8232（Dev）双轨存在，文档没声明当前环境用哪种                   | 新增本文件，FAQ 回答引用本文件                                              |
| 2026-07 | 前端 vite 硬写 localhost:8080 导致 WebSocket 跨域 | <!-- deploy-claim:historical --> ws\_origin.go 允许列表漏了 8080                                       | 8080 已加入允许列表（但仍建议统一到 8204）                                     |

***

## 四、配置来源优先级（端口/URL）

```
user-server 启动时：
  环境变量 DB_PORT > config.yaml db.port 默认值 > ports.go DefaultDBPortDev/DefaultDBPortDocker

LLM 提供商：
  DB 表 llm_providers（LoadProvidersFromDB）> config.yaml inference.*.base_url > ports.go 默认值

Redis：
  环境变量 REDIS_PORT > 硬编码 8203（ports.go DefaultRedisPort）
```

***

## 五、变更记录

| 日期      | 改了什么                                | 原因                                | PR |
| ------- | ----------------------------------- | --------------------------------- | -- |
| 2026-09 | 新建本文件                               | 解决 CV 验收时多处端口漂移混乱问题               | —  |
| 2026-09 | docker-compose.yml 注释补全 PG 映射逻辑     | 避免"8202→8202" vs "8202→8232" 双轨误导 | —  |
| 2026-09 | .env-example PG 节明确区分 Dev/Docker 模式 | 新同学看 .env-example 知道 8232 是故意的    | —  |
| 2026-09 | 后端进容器（模式 C）：user-server 改 docker dev 热重载；embed-sdk 5174→8214 | 后端容器化定案；前端端口统一 82 段且 contributor/embed-sdk 相邻 | —  |

