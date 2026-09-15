# user-server 代码规范

> **规则级别**: ⭐⭐ 项目级开发文档
> **关联文档**:
> - 架构图: [./ARCHITECTURE.md](./ARCHITECTURE.md)
> - 代码开发手册: [./DEVELOPMENT.md](./DEVELOPMENT.md)
> - 功能清单: [./FEATURES.md](./FEATURES.md)
> - 五层架构硬约束（最高规则）: [../../docs/architecture/GO_FIVE_LAYER_ARCHITECTURE.md](../../../docs/architecture/GO_FIVE_LAYER_ARCHITECTURE.md)
> - 用户体系规范: [../../docs/architecture/USER_SYSTEM.md](../../../docs/architecture/USER_SYSTEM.md)

本文档汇总 `user-server` 工程的代码规范，覆盖 **五层架构约束、命名规范、错误处理、日志、数据库、缓存、WebSocket、AI 调用、安全** 九大主题。
所有规范为**强制**（⭐⭐⭐ 最高规则请直接阅读 [GO_FIVE_LAYER_ARCHITECTURE.md](../../../docs/architecture/GO_FIVE_LAYER_ARCHITECTURE.md)），违反将被 `scripts/check-architecture.sh` 与 `golangci-lint`（含 `depguard` 规则）阻断。

---

## 一、五层架构约束

### 1.1 分层职责

```
cmd ──▶ router ──▶ controller ──▶ service ──▶ repository ──▶ model
                                                   │
                                                   ▼
                                                 db (GORM)
```

| 层 | 包路径 | 职责 | 禁止 |
| --- | --- | --- | --- |
| L1 | `cmd/api/` | 装配（config → logger → db → migration → middleware → router） | 业务逻辑、SQL、Redis 调用 |
| L2 | `internal/router/` | URL ↔ Controller 绑定 + 中间件挂载 | 业务逻辑、闭包写业务判断 |
| L3 | `internal/controller/` | 解析入参 → 调 Service → 序列化响应 | 业务逻辑、SQL、Redis、跨 Service 编排、事务控制、直接引用 repository / model |
| L4 | `internal/service/` | 业务规则、跨实体编排、事务（`tx.Begin`）、缓存策略、调用 aiagent | 直接调 `db.GetDB()` / redis / SQL |
| L5 | `internal/repository/` | 封装 GORM / pgvector / Redis 基础操作 | 业务逻辑、跨表事务编排、调 service |
| 横向 | `internal/model/` | 表结构映射、字段约束（gorm tag）、UUID 生成、密码哈希 Hook | 任何业务方法、跨表查询、调其他层 |
| 横向 | `internal/dto/` | 入参校验（binding tag）、响应序列化结构 | 引用 service / repository / model 业务方法 / db |
| 横向 | `internal/middleware/` | Gin 中间件（jwt / audit / trace / ratelimit / mfa / permission / locale） | 业务逻辑 |
| 横向 | `internal/aiagent/` | AI 能力层（agent / llm / rag / embedding / vector / eval / knowledge） | 调业务 Service（避免循环依赖） |

### 1.2 依赖方向（强制）

- **正向依赖**：cmd → router → controller → service → repository → model
- **aiagent 被 Service 调用**，**不调** 业务 Service
- **DTO 可引用 model**（作为嵌套响应），但**不调** model 的业务方法（model 不应有业务方法）
- **任何反向依赖、跨层调用、绕层调用都是违规**

### 1.3 Linter 强制规则（`.golangci.yml`）

```yaml
linters-settings:
  depguard:
    rules:
      controller-layer:
        files: ["$all"]
        deny:
          - pkg: "marketing/internal/model"
            desc: "controller 不得直接依赖 model，应通过 service / DTO"
          - pkg: "marketing/internal/repository"
            desc: "controller 不得直接依赖 repository，应通过 service"
      model-layer:
        files: ["marketing/internal/model"]
        deny:
          - pkg: "marketing/internal/service"
          - pkg: "marketing/internal/controller"
          - pkg: "marketing/internal/repository"
```

### 1.4 架构合规检查

```bash
# CI 阻断式检查（9 项）
bash ../../scripts/check-architecture.sh
```

检查项（9 项）：① Controller 反向依赖（repository / db / gorm / `c.JSON`）；② Service 反向依赖（controller/router、直访 db、SQL 字符串拼接）；③ Repository 反向依赖（service / dto）；④ Model 业务方法（仅允许 GORM Hook / `TableName`）；⑤ DTO 反向引用（service/repository/db/业务方法）；⑥ 文件命名规范（版本/扩展/日期后缀、`utils.go`/`common.go`/`helpers.go`、controller/service/repository/dto 冗余后缀、源文件误命名 `_test.go`）；⑦ Service interface 规范（导出 interface + 小写 struct 实现）；⑧ Repository interface 规范（导出 interface + 小写 struct 实现）；⑨ Repository 上下文透传（导出方法必须含 `ctx context.Context`）。**注**：`import cycle` 由 `go build` / `go vet` 检测，不在本脚本范围内。

---

## 二、命名规范

### 2.1 文件命名

| 文件类型 | 命名规则 | 示例 |
| --- | --- | --- |
| Controller | `<domain>.go` | `sms.go` / `user.go` / `ai_agent.go` |
| Service | `<domain>.go` | `sms.go` / `ai_agent.go` |
| Repository | `<domain>.go` | `user.go` / `ai_agent.go` |
| Model | `<table_name>.go` | `user.go` / `sms_template.go` |
| DTO | `<domain>.go` | `user.go` / `sms.go` |
| 路由 | `<domain>_routes.go` | `sms_routes.go` / `business_routes.go` |
| 迁移 | `<feature>_migration.go` | `confidence_migration.go` |
| 测试 | `<source>_test.go` | `user_test.go` / `confidence_migration_test.go` |

> ⚠️ **后缀禁令**：`check-architecture.sh` 会将 `*_controller.go` / `*_service.go` / `*_repository.go` / `*_dto.go` 视为冗余后缀违规（后缀与包名重复）。Controller / Service / Repository / DTO 文件统一命名为裸 `<domain>.go`。

### 2.2 禁止命名

⛔ 以下命名**绝对禁止**：

- `utils.go` / `common.go` / `helpers.go`（除跨业务复用的明确辅助外）
- `<name>_v1.go` / `<name>_v2.go` / `<name>_new.go` 等版本后缀
- `<name>_stub.go` / `<name>_ext.go` 等扩展后缀
- `<name>_2026-07-22.go` 等时间戳后缀
- `utils/` / `common/` 包目录（零散函数外迁到 `pkg/utils/<具名包>/`）

### 2.3 标识符命名

| 类型 | 规则 | 示例 |
| --- | --- | --- |
| 包名 | 全小写、单数、与目录同名 | `service` / `repository` / `model` |
| 导出标识符 | 驼峰首字母大写 | `CustomerTag` / `NewCustomerTagService` |
| 非导出标识符 | 驼峰首字母小写 | `customerTag` / `createTag` |
| 常量 | 全大写下划线 | `MaxRetries` / `DefaultTimeout` |
| 接口 | 名词 + er 后缀（行为接口）或纯名词 | `Repository` / `Dispatcher` |
| 枚举 | 类型前缀 + 业务名 | `ScenarioIntentRecognize` / `AgentTypeSales` |
| Context 参数 | 始终为 `ctx context.Context` 作为首个参数 | `func (s *Svc) Create(ctx context.Context, ...)` |

### 2.4 数据库命名

| 类型 | 规则 | 示例 |
| --- | --- | --- |
| 表名 | snake_case 复数 | `customer_tags` / `sms_templates` |
| 字段名 | snake_case | `created_at` / `customer_id` |
| 索引名 | `idx_<table>_<col>[_desc]` | `idx_signals_session` / `idx_signals_band` |
| 外键名 | `fk_<table>_<ref>` | `fk_handoff_signals` |
| 唯一约束 | `uq_<table>_<col>` | `uq_customer_tags_name` |

### 2.5 禁止版本后缀（来自项目记忆）

- 禁止在模型名称中使用版本后缀（如 `v1`、`v2`）
- 禁用时间戳后缀（如 `*_2026-07-17`）

---

## 三、错误处理

### 3.1 错误创建

- **包级哨兵错误**：`var ErrNotFound = errors.New("record not found")`
- **类型化错误**：`type ValidationError struct{ Field, Msg string }`
- **动态错误**：`fmt.Errorf("create customer_tag failed: %w", err)`

### 3.2 错误包装（强制 `%w`）

```go
// ✅ 正确：用 %w 包装，保留错误链
if err := s.repo.Create(ctx, tag); err != nil {
    return fmt.Errorf("create customer_tag failed: %w", err)
}

// ❌ 错误：用 %v 丢失错误链
return fmt.Errorf("create customer_tag failed: %v", err)

// ❌ 错误：直接返回原始错误，丢失上下文
return err
```

### 3.3 错误判定

```go
// ✅ 用 errors.Is 判定哨兵错误
if errors.Is(err, repository.ErrNotFound) { ... }

// ✅ 用 errors.As 判定类型化错误
var ve *ValidationError
if errors.As(err, &ve) { ... }

// ❌ 禁止字符串匹配
if strings.Contains(err.Error(), "not found") { ... }
```

### 3.4 多错误合并

```go
// Go 1.20+ errors.Join
return errors.Join(err1, err2, err3)
```

### 3.5 Controller 错误响应

```go
// ✅ 标准：通过 response 包统一响应
if err := ctrl.svc.Create(ctx, &req); err != nil {
    response.Error(c, http.StatusBadRequest, "创建失败", err.Error())
    return
}
response.Success(c, tag, "创建成功")

// ❌ 禁止：直接 c.JSON
c.JSON(500, gin.H{"error": err.Error()})
```

### 3.6 Panic / Recover

- **禁止**业务代码用 panic 表达错误
- **允许**panic 的场景：编译期断言（`var _ interface{} = (*X)(nil)`）、不可恢复的初始化失败（`log.Fatal`）
- **recover**：仅 `gin.Recovery()` 中间件使用，业务代码不得 recover 后静默吞错

---

## 四、日志

### 4.1 日志框架

- **zerolog** 驱动，配置见 `config.yaml` 的 `logging:` 段
- 全局日志器: `logger.InitLogger(cfg)` 在 `main.go` 启动时初始化
- 日志级别: `debug` / `info` / `warn` / `error`
- 输出格式: `json`（生产，便于采集）/ `console`（本地，带颜色）
- 输出目标: `stdout` / `file` / `both`（文件超过 `max_size` MB 自动滚动，保留 1 份备份）

### 4.2 日志 API

```go
import "marketing/internal/pkg/utils/logger"

// 基础
logger.Info("User Server Starting")
logger.Infof("IS_TEST_MODE env: %s", os.Getenv("IS_TEST_MODE"))
logger.Errorf("[migration] ExecuteUpgrade 启动失败：%v", err)

// 带 ctx（自动附带 trace_id）
logger.InfofContext(ctx, "[CustomerTagService] created tag id=%d", tag.ID)
logger.ErrorfContext(ctx, "[CustomerTagService] create failed: %v", err)
```

### 4.3 trace_id 全链路追踪

- `middleware/trace.go` 在最早中间件为每个请求生成 `trace_id`，注入 `context.Context`
- Service / Repository / Dispatcher 调用必须 `ctx` 透传
- 日志通过 `logger.*Context(ctx, ...)` 自动附带 `trace_id`
- LLM 调用落库至 `llm_routing_logs` 表，含 `trace_id` / `provider` / `latency_ms` / `success` / `error_msg`

### 4.4 敏感数据脱敏

- **禁止**在日志中打印明文：`password` / `api_key` / `secret` / `access_token` / `refresh_token`
- **禁止**用 ` %+v` / `%#v` 打印完整 struct（可能含敏感字段）

### 4.5 日志内容规范

- **必须**包含足够定位问题的上下文：模块名（前缀 `[ModuleName]`）、关键 ID、操作结果
- **禁止**循环内大量打印日志（用 `debug` 级别 + 采样）
- **禁止**日志中带 emoji（生产环境采集器可能不兼容）
- 日志前缀示例: `[CustomerTagService]` / `[migration]` / `[platform]` / `[event]`

### 4.6 审计日志

- `middleware/audit.go` 通过自带 `auditLogChan`（1000 缓冲）+ `processAuditLogs` goroutine 批量调用 `repository.OperationLogRepository.Create()` 直接异步落库到 `operation_logs` 表（每 50 条或 5s 一批，带 3 次指数退避重试）
- **不经过 Event Bus**；Event Bus 中的 `OperationLogSubscriber` 当前为备用通道，仅接收业务侧主动 `Publish(operation.log, ...)` 的事件
- 审计日志持久化至 `operation_logs` 表（操作者 / 时间 / IP / 资源 / 动作）
- **禁止**业务代码同步写 `operation_logs` 表（应通过 `middleware/audit.go` 的 `auditLogChan` 异步通道）

---

## 五、数据库

### 5.1 GORM 使用

- **WithContext**（强制）：所有 GORM 调用必须 `db.WithContext(ctx)`，保证 trace_id 透传与超时取消
- **禁止** `db.Raw()` 用中文关键词（GORM 会静默返回 0 行，详见已知坑），改用 `LIKE '%' || ? || '%'` 或全文索引
- **禁止** `db.Where("col = '" + val + "'")`（SQL 注入），必须用参数化 `db.Where("col = ?", val)`
- **text[] 数组**：GORM 不支持原生数组扫描，用 `array_to_string(images, ',')` 转字符串后 scan

### 5.2 pgvector

- 向量维度硬性 1024（`config.yaml` 的 `vector_database.pgvector.dimension`）
- 向量表: `knowledge_embeddings`，HNSW 索引
- Embedding 模型: `bge-m3`（默认），维度 1024
- **禁止**写入维度不匹配的向量（pgvector 会报错）

### 5.3 事务

- **事务边界**：Service 是唯一允许 `tx.Begin()` 的层
- Repository 必须提供 `*WithTx(ctx, tx, ...)` 版本配合跨实体事务
- **禁止** Controller 控制事务
- **禁止** Repository 跨表事务编排

```go
// ✅ Service 控制事务
err := s.db.Transaction(func(tx *gorm.DB) error {
    if err := s.repoA.CreateWithTx(ctx, tx, a); err != nil { return err }
    if err := s.repoB.UpdateWithTx(ctx, tx, b); err != nil { return err }
    return nil
})
```

### 5.4 迁移

- 所有 DDL 必须幂等：`CREATE TABLE IF NOT EXISTS` / `CREATE INDEX IF NOT EXISTS`
- `Down()` 必须保护业务数据（仅删表，不删有价值的历史数据）
- 迁移文件命名: `<feature>_migration.go`，禁止版本后缀 / 时间戳后缀
- 新增迁移必须在 `migrations.RegisterMigrations` 末尾注册

### 5.5 数据迁移要求（来自项目记忆）

- **不能**以"脏数据"为理由跳过数据
- 迁移前必须 `pg_dump` 备份，保留 30 天
- 双向回滚 SQL 必须提前生成
- 需要反复验证

### 5.6 索引规范

- 高频查询字段必须建索引（如 `customer_id` / `session_id` / `created_at`）
- 复合索引字段顺序：等值条件在前、范围条件在后
- 时间序列索引: `CREATE INDEX idx_<table>_<time> ON <table>(created_at DESC)`
- **禁止**对低基数字段单独建索引（如 `status` / `gender`）

---

## 六、缓存

### 6.1 缓存抽象

- `cache.Manager` 统一接口: `Get` / `Set` / `SetNX` / `LPush` / `RPush`
- 后端: `RedisCache`（REDIS_HOST 配置时启用）/ `MemoryCache`（兜底）
- 全局单例: `cache.InitGlobalCache(redisClient)` / `cache.GetGlobalCache()`

### 6.2 缓存读写规则

- **Service 控制缓存策略**（TTL / 防穿透 / 防击穿）
- **Repository 不得直接读写 Redis 业务缓存**（仅允许访问向量缓存等基础数据）
- **禁止** Controller 直接调 cache

### 6.3 缓存键命名

- 格式: `<module>:<entity>:<key>`，如 `customer_tag:list` / `customer_tag:id:123`
- TTL 单位: 秒，禁止 0 或负数（`setCache` 会忽略）

### 6.4 缓存一致性

- 写入后立即更新缓存（不可异步，避免脏读）
- 删除后立即清缓存（`cache.Del(key)`）
- 重要数据用 `SetNX` 防击穿（互斥锁）

### 6.5 缓存清理

- `llm.Dispatcher.StartCacheJanitor(ctx, 60*time.Second)` 后台 ticker 每 60s 清理过期项
- 仅内存缓存需要 janitor（Redis 自带 TTL 过期）

---

## 七、WebSocket

### 7.1 架构

- `websocket/hub.go` · Hub 管理所有客户端连接
- `websocket/handler.go` · WSHandler 管理坐席连接
- `websocket/visitor_handler.go` · VisitorWSHandler 管理访客连接
- `websocket/seq.go` · 序号生成（单连接内单调递增）
- `websocket/ack_tracker.go` · ACK 跟踪（离线消息重发队列）
- `websocket/notify.go` · 通知封装（生产方调用）
- `websocket/envelope.go` · 协议帧

### 7.2 鲁棒性要求（来自项目记忆）

WebSocket 实现需确保：
- **重连**：客户端断线后自动重连，服务端保留 session
- **离线补发**：`ack_tracker.go` 维护重发队列，未 ack 的消息在重连后补发
- **有序性**：`seq.go` 保证单连接内消息单调递增
- **ACK 机制**：每条消息必须 ack，未 ack 视为未送达

### 7.3 消息发送

```go
// ✅ 通过 notify.Send 统一发送（自动分配 seq + 等待 ack）
notify.Send(ctx, userID, envelope)

// ❌ 禁止直接调 hub.Broadcast（绕过 seq / ack）
hub.Broadcast(payload)
```

### 7.4 每客户端配置

- 独立 send chan，缓冲 256
- 30s 心跳
- 离线消息由 `ack_tracker.go` 维护重发队列

---

## 八、AI 调用

### 8.1 LLM Dispatcher

- 全局单例: `llm.GetGlobalDispatcher()`（`main.go` 启动时 `InitGlobalDispatcherWithDB` 装配）
- 调用入口: `dispatcher.Dispatch(ctx, scenario, prompt)`
- **必须**指定 `DispatchScenario`（7 个枚举之一）：
  - `ScenarioIntentRecognize` · 意图识别
  - `ScenarioSOPReply` · SOP 销冠回复
  - `ScenarioObjection` · 异议处理
  - `ScenarioFriendlyChat` · 拟人寒暄
  - `ScenarioLongSummary` · 长文本总结
  - `ScenarioHighQuality` · 高质量回复
  - `ScenarioLowCost` · 低成本批量

### 8.2 本地优先原则

- 默认 provider: `default` → 宿主机 llama-server（`http://127.0.0.1:8207/v1`）
- 本地 provider `QualityScore=0.99`，高于所有场景的 `MinQuality` 门槛
- 云端厂商（deepseek / qwen / gpt-4o / glm-4 / kimi）**默认禁用**，仅在配置 `api_key` 且 `enabled=true` 时启用
- **禁止**云端作为默认路由（避免空密钥 401 风暴、数据出域）

### 8.3 超时全链路对齐

- 单一配置源: `inference.llm.timeout_seconds`（默认 180s，开发模式 720s）
- 派生: `dispatcher.MaxLatency` = `llm_service.httpClient.Timeout` = `sales_engine.agentLoopTotalTimeout`
- **禁止**硬编码超时常量（会导致父级 ctx 提前 cancel 子级 LLM 调用）

### 8.4 ReAct 适配器

- 本地 Qwen2.5 不支持 OpenAI Function Calling
- `NoFC=true` 时启用 ReAct 适配器，通过 Thought/Action/Action Input 文本协议完成工具调用
- 配置优先级: 用户显式 `no_fc: true/false` > URL 启发式（本地默认 true，云端默认 false）

### 8.5 模型调用日志（强制字段，来自项目记忆）

每次 LLM 调用必须落库至 `llm_routing_logs` 表，包含以下字段：

| 字段 | 说明 |
| --- | --- |
| `trace_id` | 全链路追踪 ID |
| `scenario` | 业务场景（7 个枚举之一） |
| `provider` | provider 名称（default / deepseek / qwen / gpt-4o / glm-4 / kimi / cache） |
| `model` | 模型名称 |
| `model_type` | `local` / `cloud` |
| `vendor` | 厂商（通过 `inferVendor(BaseURL)` 自动判定） |
| `base_url` | 推理服务地址（用于出域审计） |
| `is_fallback` | 是否为降级调用 |
| `from_cache` | 是否缓存命中 |
| `latency_ms` | 调用延时 |
| `success` | 是否成功 |
| `error_msg` | 错误信息 |
| `prompt_tokens` / `completion_tokens` / `total_tokens` | token 估算 |
| `prompt_cost` / `completion_cost` / `total_cost` | 成本估算 |
| `token_source` | `actual` / `estimated` / `missing` |
| `estimator` | 估算器（如 `empty_fallback`） |
| `source` | 来源（如 `cache`） |

### 8.6 缓存命中

- 缓存命中时构造 `LogEntry(provider=cache, from_cache=true, source=cache)` 落库
- **每次 Dispatch 决策（无论成功/失败/降级）必须落库**

### 8.7 失败处理

- 失败调用必须落库并标记 `is_fallback=true`
- 用于降级率统计和出域审计
- 本地模型响应为空时标记 `Source=missing` / `Estimator=empty_fallback` 用于告警
- `token_source=missing` 占比超阈值需触发告警（缓存命中不计入 missing 占比）

### 8.8 Token 计量

- **本地模型**: 请求侧+响应侧字符加权估算
- **云端模型**: 优先使用 API 返回的 actual 数据，缺失时降级为估算
- 计量维度: 按 `(model_type, vendor, token_source)` 三维聚合
- 本地模型按推理硬件成本计费，云端模型按 prompt/completion 分别计费

---

## 九、安全

### 9.1 认证与授权

- **JWT**: `middleware/jwt.go` 解析 token，注入 `user_id` 到 ctx
- **MFA**: `middleware/mfa.go` 双因素验证
- **角色**: `admin` / `customer_service` / `staff` 三档（详见 [USER_SYSTEM.md](../../../docs/architecture/USER_SYSTEM.md)）
- **二元管控**: `enabled` 字段（禁用后无法登录）+ 角色（决定前端入口可见性）
- **超管默认全权限**: 前端不拦截 + 后端不拦截，仅靠"账号存在 + 启用"判断

### 9.2 敏感数据

- 数据库密码 / JWT 密钥 / 对象存储密钥**全部**通过环境变量注入
- **禁止**任何明文密钥写入受 Git 跟踪的文件（`config.yaml` 用 `${ENV_VAR}` 占位）
- 密码入库用 bcrypt 加密，**禁止**明文存储

### 9.3 限流与防滥用

- `middleware/ratelimit.go` · IP 限流 + API Key 限流（默认 RPS=10，桶=100）
- `middleware/brute_force.go` · 登录暴力破解防御
- Redis 配置时启用分布式限流；未配置时回退进程内限流

### 9.4 审计

- `middleware/audit.go` 全局审计中间件
- 完整审计日志持久化至 `operation_logs` 表（操作者 / 时间 / IP / 资源 / 动作）
- 操作日志异步落库（通过 `auditLogChan` + `processAuditLogs` goroutine + `repository.OperationLogRepository.Create()`，**不经过 Event Bus**）
- **禁止**业务代码同步写 `operation_logs` 表

### 9.5 私域合规基线

- 业务数据**仅本地存储**，无任何外发通道（除显式配置的渠道回调）
- 本地推理栈默认（`inference.llm.mode=local` / `inference.embedding.mode=local`），数据不出域
- 云端 LLM 仅在显式配置 `api_key` 且 `enabled=true` 时启用，作为可选 fallback
- 完整审计日志 + 敏感数据脱敏日志

### 9.6 Webhook 入站

- 统一入口: `/api/webhook/{platform}/{id}`
- 渠道 API 出站仅当用户配置对应渠道账号时才出站
- **禁止** Webhook handler 直接调 Service（应通过 Event Bus 异步）

### 9.7 /metrics 端点保护

- 仅允许 loopback / 私有网段访问
- 外部需配置 `METRICS_TOKEN` 走 Bearer 鉴权
- **禁止**暴露给公网

---

## 十、Event Bus

### 10.1 设计原则

- **best-effort 投递**：队列满时丢弃新事件并日志告警，**不阻塞主流程**
- **关键路径不依赖 Event Bus**：SalesEngine 主链路、订单创建等必须同步走 Service 编排
- **至少一次投递**：worker 池保证订阅者收到事件（可能重复，订阅者需幂等）

### 10.2 队列配置

- 普通队列: 1024 缓冲，2 worker
- 关键队列（`criticalQueue`）: 客户消息专用，4 worker

### 10.3 Topic 命名

- 格式: `<domain>.<entity>.<action>`，如 `customer.message.received` / `knowledge.document.changed` / `operation.log`

### 10.4 订阅者注册

- 在 `main.go` 的 `registerEventSubscribers()` 中注册
- **禁止**业务代码运行时动态订阅（避免僵尸订阅者）

---

## 十一、Import Cycle 处理

### 11.1 禁止循环依赖

- `service` 包与 `tooluse` 包之间**不能**循环依赖
- `aiagent` **被** Service 调用，**不调** 业务 Service

### 11.2 解耦方案

通过 `internal/aiagent/agent/portcontract` 抽出 interface 解耦（参考 `tool_ports_adapter.go` 现有 Port 模式）：

```go
// portcontract/port.go
type ToolExecutorPort interface {
    Execute(ctx context.Context, toolName string, args map[string]any) (string, error)
}

// service 侧实现 Port，注入到 aiagent
// aiagent 仅依赖 interface，不依赖 service 包
```

### 11.3 自闭子模块

为避免循环依赖，以下子模块独立成包：
- `service/confidence/` · 置信度聚合
- `service/humanize/` · 拟人度评估
- `service/feedback_loop/` · 反馈学习闭环
- `service/i18n/` · 多语言
- `service/self_learning/` · 自我学习

---

## 十二、禁止项汇总

### 12.1 命名禁止

⛔ `utils.go` / `common.go` / `helpers.go`（除跨业务复用的明确辅助外）
⛔ `<name>_v1.go` / `<name>_v2.go` / `<name>_new.go`（版本后缀）
⛔ `<name>_stub.go` / `<name>_ext.go`（扩展后缀）
⛔ `<name>_2026-07-22.go`（时间戳后缀）
⛔ `utils/` / `common/` 包目录
⛔ 模型名称含版本后缀（如 `v1`、`v2`）

### 12.2 架构禁止

⛔ Controller 直接引用 `repository` / `model` / `db.GetDB()`
⛔ Service 直接调 `db.GetDB()` / `redis` / SQL
⛔ Model 含业务方法、跨表查询、调其他层
⛔ DTO 反向引用 `service` / `repository`
⛔ Router 写业务逻辑、闭包写业务判断
⛔ `aiagent` 调业务 `service`（循环依赖）
⛔ Repository 跨表事务编排
⛔ Repository 调 service

### 12.3 安全禁止

⛔ 明文密钥写入受 Git 跟踪的文件
⛔ 日志打印明文密码 / token / 密钥
⛔ ` %+v` / `%#v` 打印完整 struct（可能含敏感字段）
⛔ 业务代码同步写 `operation_logs` 表
⛔ `/metrics` 暴露给公网
⛔ Webhook handler 直接调 Service（应通过 Event Bus）

### 12.4 AI 调用禁止

⛔ 跳过 Dispatcher 直接调 LLM API
⛔ 硬编码超时常量（破坏全链路对齐）
⛔ 云端 provider 作为默认路由
⛔ 未配置 `api_key` 却启用云端
⛔ Dispatch 决策不落库（无论成功/失败/降级）
⛔ 缓存命中不构造 `LogEntry` 落库

### 12.5 测试禁止

⛔ 跳过测试（`t.Skip`）除非显式标注原因
⛔ 异常处理代替修复
⛔ 并发 TRUNCATE 共享测试库
⛔ 单接口少于 5 个测试用例

---

## 十三、相关文档导航

| 主题 | 文档路径 |
| --- | --- |
| 架构图（模块 / 时序 / 子系统） | [./ARCHITECTURE.md](./ARCHITECTURE.md) |
| 代码开发手册（环境 / 启动 / 调试 / 部署） | [./DEVELOPMENT.md](./DEVELOPMENT.md) |
| 功能清单（按业务域分组） | [./FEATURES.md](./FEATURES.md) |
| 五层架构硬约束 + 编码模板（最高规则） | [../../docs/architecture/GO_FIVE_LAYER_ARCHITECTURE.md](../../../docs/architecture/GO_FIVE_LAYER_ARCHITECTURE.md) |
| 用户/角色/授权三模块 | [../../docs/architecture/USER_SYSTEM.md](../../../docs/architecture/USER_SYSTEM.md) |
| 菜单与权限设计 | [../../docs/architecture/MENU_PERMISSION_PLAN.md](../../../docs/architecture/MENU_PERMISSION_PLAN.md) |
| LLM 路由策略 | [../../docs/marketing-features/llm-routing.md`llm-routing.md` |
| 工程级 README | [../README.md](../../README.md) |

---

最近更新日期: 2026-07-26
