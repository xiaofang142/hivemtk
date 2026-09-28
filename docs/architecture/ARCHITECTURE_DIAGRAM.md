# HiveMtk 系统级架构图（C4 Model）

> **版本**：v1.0（2026-08-16）
> **方法论**：C4 Model（Context / Container / Component / Deployment）
> **范围**：HiveMtk 用户端（user-server + user-web + 推理栈）

---

## 一、Context（系统上下文）

```mermaid
graph TD
    subgraph 系统外部
        A[访客浏览器<br/>公网]
        B[客户员工<br/>内网/VPN]
        C[平台端<br/>独立仓库 hivemtk-platform]
    end

    subgraph HiveMtk 用户端
        direction LR
        H[HiveMtk 用户端<br/>私域独立部署,单租户<br/>对话核心 / 知识库RAG / AI销冠 / 渠道触达]
    end

    A -- "HTTPS / 长轮询" --> H
    B -- "HTTPS" --> H
    C -- "低频心跳<br/>许可同步" --> H
```

**边界声明**：

| 角色 | 边界 | 说明 |
|------|------|------|
| **访客** | 系统外 | 通过企业官网嵌入 Chat Widget 接入 |
| **客户员工** | 系统外 | 通过浏览器访问 user-web 管理后台 |
| **平台端** | 系统外 | 独立仓库 `hivemtk-platform`，仅做许可/产品元数据同步 |
| **HiveMtk 用户端** | **本系统** | 单租户私域部署，无 `merchant_id` 字段 |

---

## 二、Container（容器视图）

```mermaid
graph TB
    Visitor[访客浏览器]

    subgraph 客户本地 ["客户本地（用户端部署）"]
        direction TB

        subgraph 应用层
            UserServer[user-server<br/>Go + Gin<br/>port: 8204]
            UserWeb[user-web / embed-sdk<br/>静态资源<br/>user-server 托管]
            UserWeb --> UserServer
        end

        subgraph 数据层
            PG[PostgreSQL<br/>user_db<br/>port: 8202<br/>Docker]
            Redis[Redis 7<br/>port: 8203<br/>Token / 缓存<br/>Docker]
        end

        subgraph 宿主机 llama-server ["宿主机 llama-server 推理栈"]
            LLM[llama-server :8207<br/>LLM<br/>Qwen2.5-3B-Instruct dev档]
            Emb[llama-server :8208<br/>Embedding<br/>bge-m3 1024维]
            Rerank[llama-server :8209<br/>Rerank<br/>bge-reranker-v2-m3]
        end

        UserServer --> PG
        UserServer --> Redis
        UserServer --> LLM
        UserServer --> Emb
        UserServer --> Rerank
    end

    Platform[平台端<br/>独立仓库 hivemtk-platform]

    Visitor -- "HTTPS / HTTP 长轮询" --> UserServer
    UserServer -- "HTTPS 低频心跳" --> Platform
```

### 2.1 关键端口

| 端口 | 容器/进程 | 协议 | 说明 |
|------|-----------|------|------|
| 8202 | mtk-postgres (Docker) | TCP | 用户端数据库 |
| 8203 | mtk-redis (Docker) | TCP | 用户端缓存 |
| 8204 | user-server (宿主机) | HTTP | 对话核心 API + 长轮询 |
| 8207 | llama-server (宿主机) | HTTP | LLM 推理 (OpenAI 兼容) |
| 8208 | llama-server (宿主机) | HTTP | Embedding (1024 维) |
| 8209 | llama-server (宿主机) | HTTP | Rerank |

> **铁律**：user-server 依赖宿主机 llama.cpp 提供推理，先启动 llama-server 再起 user-server。

### 2.2 容器/进程技术选型

| 组件 | 选型 | 理由 |
|------|------|------|
| user-server | Go 1.25 + Gin + GORM | 高性能、静态编译、运维简单 |
| user-web | Vue 3 + Vite + Element Plus | 现代前端栈、构建快 |
| PostgreSQL | pgvector 官方镜像 | 1024 维向量原生支持 |
| Redis | redis:7-alpine | 轻量、Token 存储 |
| 推理栈 | llama.cpp (宿主机进程) | OpenAI 兼容协议、统一二进制 |

---

## 三、Component（组件视图）

user-server 内部组件（五层架构）：

```mermaid
graph TD
    subgraph user-server ["user-server (Go)"]
        direction TB

        L1["L1 Router + Middleware<br/>URL 路由注册<br/>JWT 鉴权 / TraceID / metrics"]
        L2["L2 Controller (HTTP Handlers)<br/>解析参数、调用 Service<br/>返回 JSON 响应"]
        L3["L3 Service (业务编排)<br/>AI 销冠引擎 / RAG 检索 / 触达编排<br/>业务规则 / 事务控制"]
        L4["L4 Repository (数据访问)<br/>封装 GORM CRUD<br/>子查询 / 事务 / 索引优化"]
        L5["L5 Model + DTO<br/>GORM 实体（无业务方法）<br/>DTO 传输对象"]

        L1 --> L2 --> L3 --> L4 --> L5

        subgraph 横向能力包
            direction LR
            PkgLogger[pkg/logger]
            PkgCache[pkg/cache]
            PkgMetrics[pkg/metrics]
        end
    end

    横向能力包 -.-> L1
    横向能力包 -.-> L2
    横向能力包 -.-> L3
    横向能力包 -.-> L4
    横向能力包 -.-> L5
```

**详细规范**：参见 [GO_FIVE_LAYER_ARCHITECTURE.md](GO_FIVE_LAYER_ARCHITECTURE.md)

---

## 四、Deployment（部署视图）

### 4.1 私域部署标准模式

```mermaid
graph TD
    Internet[互联网]

    subgraph 入口层
        CF["Cloudflare / 反向代理层（可选）<br/>域名解析 / SSL 终结 / 反向代理"]
    end

    subgraph 客户本地服务器
        subgraph 宿主机进程
            UserServer["user-server<br/>Go 进程 :8204"]
            LlamaHost["llama-server<br/>:8207 / :8208 / :8209"]
        end

        subgraph Docker
            MtkPostgres[mtk-postgres<br/>命名卷持久化]
            MtkRedis[mtk-redis]
        end
    end

    Internet -- "HTTPS 443/80" --> CF
    CF -- "HTTPS" --> UserServer
    UserServer --> LlamaHost
    UserServer --> MtkPostgres
    UserServer --> MtkRedis
```

### 4.2 FRP 穿透模式（NAT 内网）

```mermaid
graph TD
    Browser[访客浏览器<br/>chat.example.com]

    subgraph 云端
        Frps[frps<br/>服务端]
    end

    subgraph 客户本地
        Frpc[frpc<br/>客户端]
        UserServer["user-server :8204"]
    end

    Browser -- "HTTPS" --> Frps
    Frps -- "frp 隧道" --> Frpc
    Frpc -- "本地映射" --> UserServer
```

详见 [FRP私域部署指南.md](FRP私域部署指南.md)

### 4.3 启动顺序

```bash
# 1. 启动数据层（PG + Redis 容器，网络 mtk-user-network）
make db-up

# 2. 启动宿主机推理栈（llama-server :8207/:8208/:8209）
make inference-host-up

# 3. 启动用户端（宿主机 Go 进程）
make dev

# 4. 验证
curl http://localhost:8204/health
```

**禁止反过来启动**：user-server 依赖宿主机 llama-server 提供 Embedding/Rerank/LLM 推理能力，先起 user-server 会导致 RAG 不可用。

---

## 五、关键规则（命名铁律）

1. **平台层统一称「智能体」(AIAgent)**：禁止在文档/UI/代码注释中混用「AI 客服 / AI 销售」
2. **命名禁用清单**：禁止「机器人/助手」等称呼智能体
3. **`AIAgent.AgentType`**（sales / customer_service / hybrid）仅作内部子类型
4. **embedding 私域部署强制本地**：使用 llama.cpp + BAAI/bge-m3，维度 1024；禁止静默降级到云端 LLM 厂商 / hash 伪向量
5. **实时通道**：HTTP 长轮询（`/api/v1/ai/chat/poll`），WebSocket 已弃用

---

## 六、修订历史

| 版本 | 日期 | 修订人 | 内容 |
|------|------|--------|------|
| v1.0 | 2026-08-16 | @architects | 初版 C4 架构图（合并散落图表） |
