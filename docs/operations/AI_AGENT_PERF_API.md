# AI 智能体性能优化 API 文档

> **版本:** 1.0  
> **日期:** 2026-07-31  
> **维护:** HiveMTK 团队

本文档描述 `user-server` AI 智能体性能优化（5 阶段并行 + 双层架构 + HTTP 长轮询）的 API 接口、DTO 协议、FeatureFlag 配置、性能指标、鉴权方式和限流规则。

> ⚠️ **本文档是设计稿，不是现网契约。** 2026-09-22 把下文每条"现在如此"的断言逐条对代码核过，
> 结论如下（每条都是仓内 grep/读源码数出来的，不是推断）：
>
> | 位置 | 文档说法 | 代码实测 |
> |------|----------|----------|
> | §一 / §九 | `POST /api/v1/ai/chat`、`GET /api/v1/ai/chat/poll`、`/api/v1/ai/health`、`/api/v1/ai/features` | `internal/router/` 里 **0 处注册**；按本文档实现调用方一律 404。真实的智能体对话入口只有三个：`POST /api/ai-agents/:id/test`（body `{customer_id, message}`，`controller/ai_agent.go:414-437`）、`GET /ws/chat`（`router/ws.go:36`，走 `SalesEngine.HandleStream`）、`GET /api/ws/visitor`（`router/chat_routes.go:51`）；渠道侧由 webhook 触发，没有 REST 同步对话端点 |
> | §六 | 示例监听 `localhost:8080`，响应体是 `{trace_id, session_id, reply, layer, reason, wall_ms, model, tokens, llm_skipped, steps}` | user-server 实际监听 **8204**（`docs/PORT_REGISTRY.md` / `config.yaml`），8080 无从生效；响应外层是 `{code:0, message, data}`（`internal/pkg/utils/response/response.go:40-48`），`data` 为 `dto.SalesResponse`（`dto/sales.go:302-331`）——**没有** `trace_id`/`session_id`/`layer`/`reason`/`wall_ms`/`model`/`tokens`/`llm_skipped` 这些键，对应实名为 `latency_ms`/`llm_model`/`cost_tokens`，Layer 决策只在流式 chunk 与 `layer_decision_logs` 里出现 |
> | §三 | 5 个开关、默认"性能优化全开" | `internal/pkg/featureflag/flag.go:61-70` 注册 **6 个**（多出 `sse_bridge`），且 parallel/stream/layer1/fallback_chain/debug_log **默认全为 false**，只有 `sse_bridge` 默认 true |
> | §三 | `viper.WatchConfig + SIGHUP 热加载`、`systemctl reload` 即回滚 | 全仓无 `viper.WatchConfig`、无 `SIGHUP` 处理（`signal.Notify` 只在 `cmd/bridge-mock`）；机制是启动读 env + 每 5s 轮询**本进程已有**的 env ⇒ 改 `FF_*` 必须**重启进程**，`reload` 不生效 |
> | §五 | `reason` 取 `faq_hit/sop_hit/fallback/layer1_disabled`，`layer` 取 `layer1/layer2/fallback_template/fallback_cache` | reason 真值集是 `internal/dto/layer_chunk.go:18-29` 的 10 个常量（文档少列 `low_confidence_skip` 等）；layer 只有 `layer1`/`layer2` 两值，`fallback_template`/`fallback_cache` **不存在** |
> | §5.3 | reason 落库值 `faq_match/sop_template/llm_response/7b_fail_3b/cache_hit` | 前四个作为 reason 字面量 **0 命中**（`sop_template` 只在 `cmd/seed/seed_assets.go` 当资产编码用），落库写的是上面那批 `faq_hit` 系列 |
> | §七 | `X-Auth-Token` + `tk_live_*/tk_svc_*/tk_ops_*` + `auth-service` 签发 | 四个符号全仓 **0 命中**；现网鉴权是 `Authorization: Bearer <JWT>`（`internal/middleware/jwt.go:38`），FeatureFlag 的真实读接口是 `/api/feature-flags`（`internal/router/business_routes.go:161`，`AdminAuthMiddleware` 保护） |
> | §八 | 四层限流 20/10/5 req/s + 单 session 30 req/60s；响应体 `{"error":"rate_limited",...}`；`AI_RATE_LIMIT_DISABLED` 熔断 | 实装：全局 per-IP 令牌桶 `RPS:1000 / BucketSize:20000`（`internal/router/router.go:205`，豁免 `/api/bridge/ingest`、`/api/ws/channel`）+ 按路径 `rate_quota.go` + 访客 `PerIPRPS:20`；429 体是 `{"code":429,"msg":"请求过于频繁，请稍后再试","retry_after":5}`；无 `X-RateLimit-*` 头；`AI_RATE_LIMIT_DISABLED` **0 命中** |
> | §十一 | 5 个 Prometheus 指标名 | 仓内无 Prometheus 依赖，5 个指标名 **0 命中**；有自研 `internal/pkg/metrics` + `MetricsMiddleware`，但**没有任何注册点**（只在注释里示例），故 `/metrics` 不暴露；可观测面只有 §5.3 的 `layer_decision_logs` 落库 |
> | §三 | 6 个开关都当作可用旋钮 | 只有 `FF_PARALLEL`/`FF_LAYER1`/`FF_DEBUG_LOG` 有非测试消费点；`FF_STREAM` 无人读，`FF_FALLBACK_CHAIN` 只被没接进链路的降级树读，`FF_SSE_BRIDGE` 只作为 `sse_enabled` 上报（详见 §三 消费表） |
> | §十 | 4 级降级链（7B→3B→Redis 24h 缓存→10 条模板）、连续 3 次失败禁用 5 分钟 | 降级树 `DecisionTree` 非测试 0 调用点＝没接线；缓存在 PG 表 `rag_answer_cache` 且无 TTL；模板是 1 条可配字符串；熔断实为 5 次失败 / 60s（`provider_failover.go:33-34`） |
> | §十四 | `AI_LAYER1_DISABLED` 等 7 个错误码 | **0 命中**；真实词表是 `internal/pkg/utils/error_code.go` 的 36 个 `ErrorCode`，错误信封为 `{code, message, data?}`（限流中间件的 429 是裸 JSON，不用信封，见 §八） |
> | §十二 | FAQ 工具链 | **这条为真**：`scripts/extract_faq.py`、`scripts/faq_seed.json`、`user-server/cmd/importfaq/main.go` 均在仓（`extract_faq.py:28` 的 `DEFAULT_INPUT` 是本机绝对路径，跑时要传参覆盖） |
>
> 真实存在并可跑的部分：`SalesEngine.Handle`（`internal/service/sales_engine.go:180`）、
> 5 阶段并行（`sales_engine_parallel.go:18` 的 `PhaseParallel = "0_phase_parallel"`）、
> `LayerRouter`（`internal/service/layer.go:33`）与 `faqHitThresh = 0.6`（`internal/service/faq.go:23`）、
> `sopHitThresh = 0.65`（`layer.go:20`）、`dto.LayerDecision` 字段表、`layer_decision_logs` 列。
> 保留设计稿正文是为了留下决策依据；要用哪一段，先按上表把口径换过来。

---

## 一、接口列表

AI 智能体性能优化交付两类对外接口：REST 同步返回 + HTTP 长轮询增量获取。

| 通道 | Method | Path | 用途 | 设计稿状态 | **实测状态（2026-09-22）** |
|------|--------|------|------|------|------|
| REST | POST | `/api/v1/ai/chat` | 同步返回完整回复 | **保留** | 🔴 未注册；等价调用面是 `POST /api/ai-agents/:id/test` 或 `GET /ws/chat` |
| REST | GET | `/api/v1/ai/chat/poll?session_id=xxx` | 长轮询增量获取（30s 超时） | **保留** | 🔴 未注册；增量只在 `GET /ws/chat` 上按帧下发 |
| REST | GET | `/api/v1/ai/health` | AI Agent 健康检查 | 新增 | 🔴 未注册；有全局 `/health`、`/healthz`、`/readyz`（`router.go:196-198`），但没有 AI 专属健康检查 |
| REST | GET | `/api/v1/ai/features` | 查询当前 FeatureFlag 状态 | 新增 | 🔴 未注册；DB 版开关在 `GET /api/feature-flags`（`business_routes.go:161`，需管理员），**env 版 6 个开关没有任何查询端点** |

> TG / WeCom / Feishu / Xianyu 等外部渠道 webhook 继续走 `controller/ai_agent.go` 老路径，不受本次改造影响。
> （实测：入站消息进的是 `controller/webhook.go` → `WebhookController.SetSalesEngine` →
> `service/webhook.go`，`controller/ai_agent.go` 是智能体 CRUD + `/test`，不是 webhook 老路径。）

---

## 二、改造背景与目标

### 2.1 业务问题
- 客服对话 wall time 平均 **19.6s**（7B Q5 本地 LLM 实测）
- 主要瓶颈：Step 3 意图识别 LLM 串行阻塞 + Step 6 候选回复 1 次 LLM 调用 + 9 步流水线无重叠

### 2.2 量化目标
| 指标 | 改造前 | 目标 | 降幅 |
|------|--------|------|------|
| P50 wall time | 19.6s | < 1.5s | 92% |
| P90 wall time | 49.5s | < 5s | 90% |
| LCP 首字时间 | 19.6s | < 500ms (WS) | 97% |
| LLM 调用次数 | 2次/对话 | ≤1次/对话 | 50% |
| 规则/模板命中率 | 42% | > 75% | 33pp |

---

## 三、FeatureFlag 开关

> 下表是**设计意图**（灰度怎么排），不是当前出厂默认值。代码实测：`internal/pkg/featureflag/flag.go:64-69`
> 注册 6 个 flag，其中 5 个默认 `false`，`sse_bridge` 默认 `true`；要让某项生效必须显式
> `FF_<NAME>=1` 后**重启进程**（详见本节末"热加载的真相"）。

| 开关 | 含义 | 设计目标默认 | **代码实际默认** | 推荐灰度 | 紧急回滚 |
|------|------|---------|----------|----------|----------|
| `FF_PARALLEL` | 启用 SalesEngine 5 阶段并行化 | `1` (开启) | `0` | 0% → 5% → 25% → 50% → 100% | `FF_PARALLEL=0` |
| `FF_STREAM` | 启用 WebSocket 流式输出 (已弃用, 使用 HTTP 长轮询) | `0` (关闭) | `0` | - | 已下线 |
| `FF_LAYER1` | 启用 Layer1 FAQ/SOP 模板 SkipLLM | `1` (开启) | `0` | 同上 | `FF_LAYER1=0` |
| `FF_FALLBACK_CHAIN` | 启用 4 级降级链 (7B→3B→缓存→模板) | `1` (开启) | `0` | 50% 起步 | `FF_FALLBACK_CHAIN=0` |
| `FF_DEBUG_LOG` | 输出 phase 详细日志 (Steps 含 debug) | `0` (关闭) | `0` | 内部观察用 | `FF_DEBUG_LOG=0` |
| `FF_SSE_BRIDGE` | Bridge 出站用 SSE（true）还是长轮询（false） | 设计稿未列 | `1` | - | `FF_SSE_BRIDGE=0` |

> 6 个开关的名字到 env 的映射是 `FF_` + flag 名大写（`flag.go:157` 的 `EnvNameOf`），
> 即 `parallel`→`FF_PARALLEL`、`sse_bridge`→`FF_SSE_BRIDGE`。
> 另有一套**给运维后台用的 DB 版 FeatureFlag**（表 `feature_flags`，接口 `/api/feature-flags`，
> `internal/router/business_routes.go:161`），与上面这套 env flag 不是同一个东西，别混用。

**这 6 个开关里，真正会改变运行行为的只有 3 个**（逐个查 `featureflag.Get(...)` 的非测试调用点）：

| 开关 | 消费点（非测试） | 结论 |
|------|------------------|------|
| `FF_PARALLEL` | `service/sales_engine_parallel.go:316` | **有效**：关掉即回到串行 |
| `FF_LAYER1` | `service/layer.go:103` | **有效**：关掉时 `LayerRouter` 直接判 Layer2 并落一条 `reason=layer1_disabled` |
| `FF_DEBUG_LOG` | `service/layer.go:192,242`、`sales_engine_parallel.go:122,132` 等 | **有效**：只影响日志详细度 |
| `FF_STREAM` | 无（`Get("stream")` 全仓 0 命中） | **空开关**：注册了但没人读，改它没有任何效果 |
| `FF_FALLBACK_CHAIN` | 仅 `aiagent/llm/fallback_tree.go:106` | **间接空**：唯一读它的 `DecisionTree.Decide` 在非测试代码里 **0 调用点**（`NewDecisionTree` 同样 0 命中），整棵降级树没接进请求链路 |
| `FF_SSE_BRIDGE` | 仅 `controller/bridge_capabilities.go:45` | **只上报不改变行为**：把值作为 `sse_enabled` 字段回报给 bridge，user-server 自身不因它切换 SSE/轮询 |

⇒ 也就是说：本文档 §十 的降级链和 §九 的流式通道都**不由这些开关控制**；实际生效的降级面在
`aiagent/llm/dispatcher.go` + `provider_failover.go`（见 §十 实测口径），流式实际走 WS（见 §九 实测口径）。

**热加载的真相（原稿写的是 viper + SIGHUP，代码里没有）：** `flag.go:34` 的 `PollInterval = 5s`
起一个后台 poller 周期性重读 `os.Getenv`。进程环境变量在进程存活期内不会自己变，
所以"改 env 不重启即生效"只在**测试里直接改 env** 或将来接了真正的注入源时成立；
部署面上改 `FF_*` 需要 `systemctl restart user-server`（或容器重建）。
全仓没有 `viper.WatchConfig`，也没有 `SIGHUP` 处理（`signal.Notify` 仅出现在 `cmd/bridge-mock`）。

**完整环境变量配置（推荐放进 systemd EnvironmentFile 或 k8s ConfigMap）：**

```bash
# /etc/hivemtk/ai-agent.env
FF_PARALLEL=1
FF_STREAM=0  # 已弃用, 使用 HTTP 长轮询
FF_LAYER1=1
FF_FALLBACK_CHAIN=1
FF_DEBUG_LOG=0
```

**一键回滚（改完 env 后重启进程，5 秒级）：**

```bash
export FF_PARALLEL=0
export FF_LAYER1=0
export FF_FALLBACK_CHAIN=0
systemctl restart user-server  # 不是 reload：reload 不发 SIGHUP 给 flag，见上
```

---

## 四、SalesEngine 5 阶段并行化

> **实测口径（2026-09-22）**：本节流程图为真，两处口径要换：
> ① 标题的"5 阶段"在代码里只有 **3 个 phase 标记**（`0_phase_parallel` / `1_phase_serial` /
> `2_phase_async`，`sales_engine_parallel.go:18-20`），"5"是 4.1 图里把并行 fan-out 的 4 个任务也计数；
> ② 4.2 的"开关关闭时回退到原 9 步串行"对不上现在的步骤表——`internal/service/` 里能数到的具名步骤有
> **16 个**（`1_resolve_customer`…`7_polish`、`9_feedback_learn`，中间还插了
> `0.5_layer1_fastpath`/`3.5_clarify`/`3.5_transfer_check`/`5.5_match_script`/`5.6_playbook*`/
> `6.5_behavioral`/`7.5_humanize_eval`），且**没有 `8_*` 步骤**（编号直接从 7 跳到 9）。
> "9"是**编号约定**而非步数：`fullchain_e2e_phase5_test.go:51` 也只断言 `len(resp.Steps) >= 7` 且含
> `9_feedback_learn`。"9 步编排"另一处出现在 `controller/webhook.go:482`，指 `SmartCSOrchestrator.HandleIncoming`，
> 与 SalesEngine 的步骤编号不是同一套计数。
> Phase 0 的 4 任务并行（`sales_engine_parallel.go:155,165,175,202`）与收割 10ms 超时
> （`:131` 的 `time.After(10 * time.Millisecond)`）为真。

### 4.1 数据流

```
[入站消息]
    ↓
[Phase 0: 并行 fan-out]  ←  errgroup.WithContext
    ├─ resolveCustomer     ─┐
    ├─ recallMemory        ─┤ 4 任务并行
    ├─ IntentSpeculative   ─┤ (LLM 异步落库)
    └─ recallRAG           ─┘
    ↓
[Phase 1: 串行决策]
    ├─ 3.5 shouldTransfer
    ├─ 4    matchSOP
    ├─ 5.5  matchScript
    ├─ 5.6  playbook
    └─ 6    generateCandidate (Layer1 优先 → Layer2 LLM)
    ↓
[Phase 2: 异步收割]
    └─ 收割 IntentSpeculative LLM 结果 (10ms 超时, 不阻塞)
    ↓
[出站 SalesResponse]
```

### 4.2 接口

`SalesEngine.Handle(ctx, req) (*SalesResponse, error)`  
- 开关关闭时回退到原 9 步串行（向后兼容）
- 开关开启时走 5 阶段并行

### 4.3 步骤日志

`resp.Steps` 包含执行详情（可观测性）：

```go
// 与 internal/dto/sales.go:85-92 逐字段一致（Extra 是 any，不是 map）
type SalesStepLog struct {
    Step      string `json:"step"`        // "0_phase_parallel" / "1_phase_serial" / "2_phase_async"
    Status    string `json:"status"`      // 实发字面量: "ok" / "fail" / "skip" / "timeout"
    LatencyMs int    `json:"latency_ms"`
    Detail    string `json:"detail,omitempty"`
    Error     string `json:"error,omitempty"`
    Extra     any    `json:"extra,omitempty"`
}
```

---

## 五、双层架构 (Layer1 + Layer2)

### 5.1 决策逻辑

```
LayerRouter.Route(ctx, req) -> *LayerDecision      （下面每行右端是代码实际行为，layer.go:87-212）
    ↓
0. defer: 无论走哪条分支，都异步落一条 layer_decision_logs（layer.go:96-101）
    ↓
1. FF_LAYER1 关闭 -> Layer2, reason=layer1_disabled, 直接返回
    ↓
2. FAQ 匹配（两条实现，按注入的依赖择一）:
     a) r.faqSvc != nil 且 UserMessage 非空 -> MatchByAgent(agentID, msg, 3)，比较 top.Score
     b) 否则 r.faqRepo != nil 且 AgentID>0 -> MatchByAgent(...)，比较 top.Confidence
   命中阈值 faqHitThresh=0.6 -> Layer1 + SkipLLM + reason=faq_hit，返回
   有候选但没过阈值 -> 只把 reason 置为 low_confidence_skip，继续往下（不再返回）
    ↓
3. SOP 模板匹配：需 sopSvc!=nil && AgentID>0 && Intent 非空且 != IntentUnknown
   MatchByAgent 后 top.Confidence >= sopHitThresh=0.65 -> Layer1 + SkipLLM + reason=sop_hit
   渲染失败(BuildLayer1Reply 报错或空) -> 改判 Layer2 + reason=fallback 并返回
    ↓
4. 兜底 -> Layer2, SkipLLM=false；reason 若仍为空则置 fallback
   （即步骤 2 留下的 low_confidence_skip 会一路带到落库行里）
```

两个实测要点：**`AgentID == 0` 时 FAQ/SOP 两条匹配全部跳过**（`layer.go:109` 的
`if req.AgentID == 0 {}` 是个空分支，等价于"没有智能体就没有 Layer1"）；
DTO 里另外 5 个 reason 常量（`confidence_high`/`confidence_low`/`no_faq`/`no_sop`/`intent_unknown`）
在 `layer.go` 里**从不被赋值**，落库也看不到它们。

### 5.2 LayerDecision DTO

字段名/类型与 `internal/dto/layer_chunk.go:32-43` 一致（这节是实测过的，可直接当契约用）：

```go
type LayerDecision struct {
    Layer      string  // "layer1" / "layer2"
    SkipLLM    bool    // true = Layer1 命中
    Reply      string  // Layer1 命中时的模板回复
    Reason     string  // 取值见下，不是随意字符串
    Confidence float64
    FAQID      uint
    SOPID      uint
    Intent     string
    WallMs     int
    Metadata   string
}
```

`Reason` 的真实取值集合是 `internal/dto/layer_chunk.go:18-29` 的 10 个常量：
`faq_hit` / `sop_hit` / `confidence_high` / `confidence_low` / `fallback` /
`layer1_disabled` / `no_faq` / `no_sop` / `intent_unknown` / `low_confidence_skip`。
`Layer` 只有 `layer1` / `layer2` 两值（`layer_chunk.go:12-15`）。

### 5.3 决策日志（落库 layer_decision_logs）

以 `internal/model/layer_decision_log.go:30-45` 与开发库实测列宽为准
（`information_schema.columns`，建表 DDL 里的 `session_id VARCHAR(50)` 已被启动期 AutoMigrate 收敛到 120）：

| 字段 | 类型 | 实际写入值（实测） |
|------|------|------|
| `id` | bigserial | 自增 |
| `trace_id` | varchar(64) | **永远形如 `lr-<unixnano>`**：`LayerRouter.traceFunc` 无 setter、包外不可赋值，`traceID()` 恒走兜底分支（`layer.go:249-254`）。而响应体/`llm_routing_logs` 的 trace_id 来自 `tracing.*FromContext`（UUID v4，`internal/pkg/trace/trace.go:93-95`）。两张表**无法按 trace_id join**；模型注释原文写的是"与 `llm_routing_logs.trace_id` 对齐"，2026-09-22 已就地更正（`model/layer_decision_log.go:19-20`），代码层面的这个坑仍未修（要能 join 需给 `LayerRouter` 注入 ctx 里的 trace） |
| `session_id` | varchar(120) | `RouteRequest.SessionID` |
| `customer_id` | varchar(64) | `RouteRequest.CustomerID` |
| `layer` | varchar(32) | 只有 `layer1` / `layer2`（`dto.Layer1`/`dto.Layer2`），无 `fallback_template`、`fallback_cache` |
| `reason` | varchar(64) | 只有 5 个值会被写入：`layer1_disabled` / `faq_hit` / `sop_hit` / `low_confidence_skip` / `fallback`。DTO 里另外 5 个常量（`confidence_high`/`confidence_low`/`no_faq`/`no_sop`/`intent_unknown`）全仓非测试 0 引用，落库不会出现。文档旧版列举的 `faq_match`/`sop_template`/`llm_response`/`7b_fail_3b`/`cache_hit` 均非真实写入值 |
| `intent` | varchar(64) | `intentType(req.Intent)`，Layer1 FAQ 命中时被 `Entry.Intent` 覆盖 |
| `conf_in` | numeric(5,4) | 决策前 `req.Intent.Confidence`；`req.Intent` 为 nil 时写 0 |
| `conf_out` | numeric(5,4) | 决策后 `decision.Confidence`（只有 FAQ/SOP 命中分支会赋非 0） |
| `wall_ms` | int | `Route` 全程耗时，defer 里补写 |
| `llm_skipped` | boolean | = `decision.SkipLLM`（指针字段，恒非 null） |
| `extra` | text | 固定格式 `faq_id=%d sop_id=%d` |
| `created_at` | timestamptz | `autoCreateTime` |
| `deleted_at` | timestamptz | 软删列（`v3_22_1_soft_delete_migration.go:53` 补的） |

写入时机：`Route` 每次调用都在 `defer` 里落一条（`layer.go:96-101`），即"layer1 关闭走 Layer2"也会留一行 `reason=layer1_disabled`；写库走 `utils.SafeGo` 异步，失败只在 `FF_DEBUG_LOG=1` 时 Warn，不回传调用方。

读取侧：`GetByTraceID` / `StatsByLayer` / `StatsByIntent` / `Recent` / `LLMSkippedCount`（`repository/layer_decision_log.go`）除测试外**没有任何调用方**——没有 HTTP 端点、没有看板消费这张表；全仓非测试引用只有 `service/layer.go` 里的构造与写入。

---

## 六、请求/响应示例 (curl)

> ⚠️ **本节全部 curl 与 Python 示例都不可跑**（含 6.1 的两段、6.2 的三段和下面的长轮询客户端）：
> 路径 `/api/v1/ai/chat`、`/api/v1/ai/chat/poll`、`/api/v1/ai/chat/cancel`
> 未注册，`X-Auth-Token: tk_live_abc123` 这套头不被识别（真实是 `Authorization: Bearer <JWT>`），
> 请求体字段名也不对：入站消息字段是 `user_message`（`dto/sales.go:285`），
> 根本没有 `text`、`stream_mode`、`metadata` 三个键（`SalesRequest` 全部键见 `dto/sales.go:281-299`）。
> 响应体里的 `trace_id/layer/reason/wall_ms/model/tokens/llm_skipped` 同样不在 `dto.SalesResponse` 上
> （实名 `latency_ms`/`llm_model`/`cost_tokens`，Layer 只在流式 chunk 与落库表里）。
> 可直接照抄的调用面见 §一 实测列；本节保留作设计意图记录。
>
> **6.1 的可用替代（REST，同步返回一条完整回复）：**
>
> ```bash
> # 登录拿 JWT -> POST /api/ai-agents/<agentID>/test
> curl -X POST "http://127.0.0.1:8204/api/ai-agents/1/test" \
>   -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
>   -d '{"customer_id":"c-9988","message":"韵达发货吗"}'
> # 200: {"code":0,"message":"测试成功","data":{ ...dto.SalesResponse... }}
> ```
>
> （字段名是 `message` 而非 `text`：`controller/ai_agent.go:421-424`；`customer_id` 可省。）
>
> **6.2 的可用替代（WS 增量）：** `GET /ws/chat?token=<JWT>` 握手后发
> `{"type":"chat","user_message":"...","platform":"web"}`（`controller/chat_ws.go:311-317`），
> 服务端逐帧回 `dto.StreamChunk`；WS 的 `trace_id` 是第三种格式 `<毫秒>-<16位hex>`
> （`chat_ws.go:319-326`），与 UUID、`lr-<unixnano>` 都不同。
> 主动取消：客户端关连接即可，`cancel` 类型不下发（见 §九）。

### 6.1 REST `/api/v1/ai/chat` (curl)

**请求：**

```bash
curl -X POST http://127.0.0.1:8204/api/v1/ai/chat \
  -H "Content-Type: application/json" \
  -H "X-Auth-Token: tk_live_abc123" \
  -H "X-Request-Id: req-20260731-0001" \
  -d '{
    "session_id": "s-20260731-001",
    "customer_id": "c-9988",
    "text": "韵达发货吗",
    "stream_mode": false,
    "metadata": {
      "channel": "web"
    }
  }'
```

**响应 (Layer1 命中 / 同步返回)：**

```json
{
  "trace_id": "t-20260731-abc123",
  "session_id": "s-20260731-001",
  "reply": "亲，韵达不发的哦，我们默认发邮政/顺丰～",
  "layer": "layer1",
  "reason": "faq_hit",
  "intent": "logistics",
  "confidence": 0.92,
  "wall_ms": 87,
  "model": "",
  "tokens": 0,
  "llm_skipped": true,
  "steps": [
    {"step": "0_phase_parallel", "status": "ok", "latency_ms": 23, "detail": "customer+memory+intent+rag"},
    {"step": "1_phase_serial", "status": "ok", "latency_ms": 5, "detail": "Layer1 FAQ hit, skip LLM"},
    {"step": "2_phase_async", "status": "skip", "latency_ms": 0}
  ]
}
```

**响应 (Layer2 LLM 兜底)：**

```json
{
  "trace_id": "t-20260731-def456",
  "session_id": "s-20260731-002",
  "reply": "可以的亲，这边推荐您看一下热销款 #A102…",
  "layer": "layer2",
  "reason": "llm_response",
  "intent": "product_recommend",
  "confidence": 0.78,
  "wall_ms": 1843,
  "model": "qwen-7b-q5",
  "tokens": 86,
  "llm_skipped": false,
  "steps": [...]
}
```

### 6.2 HTTP 长轮询 `/api/v1/ai/chat/poll`

**发起长轮询请求（同步返回首包，后续增量通过轮询获取）：**

```bash
# 1. 发起对话 (POST /api/v1/ai/chat)
curl -X POST http://127.0.0.1:8204/api/v1/ai/chat \
  -H "X-Auth-Token: tk_live_abc123" \
  -H "Content-Type: application/json" \
  -d '{
    "session_id": "s-20260731-001",
    "customer_id": "c-9988",
    "text": "你好,有什么推荐吗",
    "stream_mode": true
  }'
# 返回: {"session_id":"s-20260731-001","response":"...","wall_ms":1843,"...}

# 2. 长轮询获取增量 (最多 30s 超时)
curl http://127.0.0.1:8204/api/v1/ai/chat/poll?session_id=s-20260731-001 \
  -H "X-Auth-Token: tk_live_abc123"
# 返回增量 chunks:
# {"type":"start","trace_id":"t-20260731-xyz789","session_id":"s-20260731-001","intent":"product_recommend","layer":"layer2"}
# {"type":"delta","text":"亲，您好！"}
# {"type":"delta","text":"推荐您看看热销款 #A102"}
# {"type":"final","text":"亲，您好！推荐您看看热销款 #A102","wall_ms":1843,"layer":"layer2","model":"qwen-7b-q5","tokens":42,"steps":3}

# 3. 主动取消
curl -X POST http://127.0.0.1:8204/api/v1/ai/chat/cancel \
  -H "X-Auth-Token: tk_live_abc123" \
  -d '{"session_id":"s-20260731-001"}'
```

**Python 客户端示例（requests 长轮询）：**

```python
import requests, json, time

def chat_with_poll(session_id, text, token):
    # 发起对话
    resp = requests.post("http://127.0.0.1:8204/api/v1/ai/chat", json={
        "session_id": session_id,
        "customer_id": "c-9988",
        "text": text,
        "stream_mode": True,
    }, headers={"X-Auth-Token": token})
    
    # 长轮询获取增量
    while True:
        poll = requests.get(
            "http://127.0.0.1:8204/api/v1/ai/chat/poll",
            params={"session_id": session_id},
            headers={"X-Auth-Token": token},
            timeout=30
        )
        for chunk in poll.json().get("chunks", []):
            if chunk["type"] == "delta":
                print(chunk["text"], end="", flush=True)
            elif chunk["type"] == "final":
                print(f"\n[wall_ms={chunk['wall_ms']}] done")
                return
```

---

## 七、鉴权方式

> **实测口径（2026-09-22）**：本节 `tk_live_*` / `tk_svc_*` / `tk_ops_*`、`X-Auth-Token`、
> `auth-service` 四个符号在 `user-server` 全仓 **0 命中**，`/api/v1/ai/*` 也不存在，所以照本节实现的
> 客户端无法工作。真实机制是：`Authorization: Bearer <JWT>`（HS256，`internal/middleware/jwt.go:38-51`；
> WebSocket 握手带不上 header，允许 `?token=` 兜底，`jwt.go:39-43`），解析后写入 gin context 的是
> `user_id` / `license_id` / `role` / `data_scope`，**token 里没有 `agent_id` claim**，
> 也就没有本节 7.4 的 `403 agent_mismatch`；智能体隔离改为在 service 层按 `agent_id` 入参显式过滤
> （`service/faq.go:194`、`service/sop_template.go:153` 的注释即此约束）。下表保留为设计稿。

AI Agent 通道对**前端 ChatWidget**、**内部服务**、**第三方渠道** 三类调用方采用不同鉴权策略。

### 7.1 Token 类型矩阵

| 调用方 | Token 类型 | 传递方式 | 鉴权位置 |
|--------|-----------|---------|----------|
| 网页 ChatWidget (REST) | `tk_live_xxx` (会话级 JWT) | `X-Auth-Token` Header | 边缘网关 + user-server 中间件 |
| 内部服务 (TG/WeCom) | `tk_svc_xxx` (服务级) | `X-Auth-Token` Header | internal middleware |
| 运维查询 (`/api/v1/ai/features`) | `tk_ops_xxx` (RBAC) | `X-Auth-Token` Header | admin middleware |

**平台侧上报（出站，不属于上表的调用方）**：仅当 `PLATFORM_ENABLED=true` 时，user-server 默认每 3 分钟
主动 `POST {平台地址}/api/platform/heartbeat` 上报安装/指标统计，走普通 HTTP 请求、
不带任何签名或 JWT（`internal/platform/client.go:480`）。默认配置下该开关为 false，整条链路不装配、
零出站请求（`internal/config/platform.go` 的 `LoadPlatform` 早退）。

### 7.2 Token 格式

```
tk_live_<user>_<session>_<signature>
tk_svc_<service>_<env>_<signature>
tk_ops_<user>_<role>_<signature>
```

> 所有 token 由 `auth-service` 签发，HS256 签名，TTL 默认 1h，过期自动 401。

### 7.3 HTTP 鉴权方式

```http
POST /api/v1/ai/chat HTTP/1.1
Host: <你的-user-server-地址>:8204
Content-Type: application/json
X-Auth-Token: tk_live_user001_s9988_eyJhbGciOiJIUzI1NiJ9...
X-Request-Id: req-20260731-0001
```

鉴权失败返回：

| HTTP Code | 含义 | 处理 |
|-----------|------|------|
| `401` | Token 缺失/过期/签名错误 | 客户端重新获取 token |
| `403` | Token 有效但无权访问该 session | 客户端清理本地状态 |
| `429` | 触发限流 (见下文) | 客户端退避重试 |

### 7.4 智能体知识库隔离

- 每个 `tk_live_xxx` token 内嵌 `agent_id` claim
- 所有 FAQ / SOP / LayerDecisionLog 查询强制 `WHERE owner_agent_id IN (...)`
- 越权访问立即返回 `403 agent_mismatch`，**写入审计日志**

---

## 八、限流规则

> **实测口径（2026-09-22）**：下面 8.1~8.3 的三层阈值、`X-RateLimit-*` 响应头和
> `AI_RATE_LIMIT_DISABLED` 熔断都**没有实装**（三者在 `user-server` 全仓 0 命中）。现网真实限流是：
> 全局 per-IP 令牌桶 `RPS:1000 / BucketSize:20000`（`internal/router/router.go:205-212`，
> 豁免 `/api/bridge/ingest`、`/api/ws/channel`）+ 按路径配额表 `rate_quota.go` + 访客通道
> `PerIPRPS:20`（`middleware/visitor_rate_limit.go:24`，另有按渠道的限流）。三者 429 响应体各不相同：
> 全局桶 `{code:429,msg:"请求过于频繁，请稍后再试",retry_after:5}`（`ratelimit.go:164-169`）、
> 路径配额 `{code:429,msg:"该接口请求过于频繁，请稍后再试",path,rps,retry_after:1}`（`rate_quota.go:54-60`）、
> 访客通道 `{code:429,message:"…请求过于频繁…"}`（用 `message` 而非 `msg`，且无 `retry_after`，
> `visitor_rate_limit.go:142-146` / `154-158`）。三者都**没有** 8.2 里的 `X-RateLimit-*` 头，也不发
> `Retry-After`（本仓该头只出现在登录防爆破 `middleware/brute_force.go:146` 和 webhook `controller/webhook.go:156`）。
> 要临时放开只能改代码里的阈值后重启进程，没有环境变量开关。8.1 里"L4 网关 10 req/s"与
> `middleware/ratelimit.go:26-30` 的 `DefaultRateLimitConfig.RPS:10` 数值只是巧合：该默认值除测试外
> **0 引用**，实际生效的是 `router.go:205-212` 显式传入的 1000。

为防止恶意流量击穿 LLM 推理栈，AI Agent 通道在 3 个层级实施限流。

### 8.1 限流策略表

| 层级 | 维度 | 算法 | 默认阈值 | 超限行为 |
|------|------|------|---------|----------|
| **L7 边缘** | 每 IP 每秒 | 令牌桶 (token bucket) | 20 req/s | `429` + `Retry-After: 1s` |
| **L4 网关** | 每 Token 每秒 | 滑动窗口 (sliding window) | 10 req/s | `429` + 退避 2s |
| **L1 进程** | 全局 LLM 调用 | 漏桶 (leaky bucket) | 5 req/s | 排队 + 60s 超时 |
| **L1 进程** | 单 session 60s 请求 | 计数器 (counter) | 30 req/60s | `429` + 锁定 60s |

### 8.2 限流响应头

```http
HTTP/1.1 429 Too Many Requests
Content-Type: application/json
Retry-After: 2
X-RateLimit-Limit: 10
X-RateLimit-Remaining: 0
X-RateLimit-Reset: 1722441600

{"error":"rate_limited","scope":"token","retry_after_ms":2000}
```

### 8.3 内部降级开关

```bash
# 全局限流熔断 (P0 故障时启用)
export AI_RATE_LIMIT_DISABLED=1
systemctl reload user-server
```

> 该开关仅作为 **P0 故障逃逸** 用，平时保持关闭。

---

## 九、HTTP 长轮询增量协议

> **实测口径（2026-09-22）**：本节整条 HTTP 长轮询通道未实装——`/api/v1/ai/chat`、`/api/v1/ai/chat/poll`
> 在 `internal/router/` 里 0 注册，仓内也没有 `poll` 端点。增量协议本身是**真的**，但载体是
> WebSocket：`GET /ws/chat`（`router/ws.go:49`）→ `SalesEngine.HandleStream`（`sales_engine.go:509`）
> → 每个 chunk 立刻 `conn.WriteMessage(websocket.TextMessage, …)` 单帧下发（`controller/chat_ws.go:237`）。
> 因此 9.4 的"每 50ms 一批 / 客户端轮询间隔 200ms"这两个数字在代码里 **0 命中**（`chat_ws.go:241` 的
> ticker 只用于 WS 心跳 ping，不做攒批），
> 只有 9.3 的 `start/delta/final/error` 四类对应 `dto.StreamChunk`（`dto/layer_chunk.go:102-115`）实存；
> `cancel` 类型有常量 `ChunkTypeCancel` 但**没有任何下发点**（全仓非测试 0 引用）。

### 9.1 端点

- `POST /api/v1/ai/chat` — 发起对话
- `GET /api/v1/ai/chat/poll?session_id=xxx` — 长轮询获取增量（30s 超时）

### 9.2 协议 (JSON over HTTP)

```json
// 客户端请求
POST /api/v1/ai/chat
{"session_id":"s1","customer_id":"c1","text":"你好","stream_mode":true}

// 同步响应
{"session_id":"s1","response":"...","wall_ms":null}

// 长轮询增量响应 (GET /api/v1/ai/chat/poll?session_id=s1)
{"chunks":[
  {"type":"start","trace_id":"t-123","intent":"greeting","layer":"layer1"},
  {"type":"delta","text":"你好"},
  {"type":"delta","text":"，" },
  {"type":"delta","text":"有什么能帮你？"},
  {"type":"final","text":"完整回复","steps":[...],"wall_ms":1234,"layer":"layer2","model":"qwen-7b","tokens":42}
]}
```

### 9.3 Chunk 类型

| `type` | 含义 | `dto.StreamChunk` 里的实际字段（除 `type` 外全部 `omitempty`） |
|--------|------|------|
| `start` | 流开始 | `trace_id`, `intent`, `layer`, `step` |
| `delta` | 增量文本 | `text`, `step`, `layer`, `metadata` |
| `final` | 流结束 | `text`, `steps`, `wall_ms`, `layer`, `model`, `tokens` |
| `error` | 错误 | `error`, `trace_id`, `step`（**无** `retry_after_ms`，该键全仓 0 命中） |
| `cancel` | 取消 | 常量存在、无下发点（当前不可达） |

`StreamChunk` 全部字段：`type/trace_id/text/intent/step/steps/wall_ms/layer/model/tokens/error/metadata`；
`session_id` 不在结构里，9.2 示例中的 `"session_id"` 与 `"chunks":[…]` 包裹层都是设计稿，
现网是"一帧一个 chunk"的裸 JSON。

### 9.4 LCP 优化

- **首包** (LCP): Layer1 命中 → `<100ms` 返回完整响应
- **增量**: Layer2 LLM → 长轮询分批返回（每 50ms 一批，客户端轮询间隔 200ms）
- **fallback**: 60s 仍无 LLM 响应 → 返回 `default_template` 兜底

---

## 十、智能降级链

> **实测口径（2026-09-22）**：本节这棵树**只有类型定义、没有接进请求链路**——
> `internal/aiagent/llm/fallback_tree.go:19-36` 定义了 `LevelPrimary/LevelSecondary/LevelCache/LevelTemplate`
> （级别名 `primary_7b`/`secondary_3b`/`cache`/`template`），但 `NewDecisionTree` 与 `DecisionTree.Decide`
> 在非测试代码里 **0 调用点**，`FF_FALLBACK_CHAIN` 也只被这棵没人调用的树读（见 §三 开关消费表）。
> 真正在跑的降级面是 `aiagent/llm/dispatcher.go` 的 `route.Provider + route.Fallbacks` 顺序重试
> （`dispatcher.go:302` 打"兜底启用 provider"日志）+ `provider_failover.go` 的健康位/熔断 + 一条 `TemplateReply`。
> 10.1/10.2 与代码的出入：
>
> - `Cache Hit (Redis 24h TTL)`：缓存不在 Redis，是 PG 表 `rag_answer_cache` 的精确 + 向量语义查
>   （`internal/aiagent/rag/cache/store_pg.go:90,112`），全仓该包内 **无 TTL / 过期时间常量**，
>   失效靠 `kb_id` + `prompt_version` 换版，不按时间淘汰。
> - `Default Template (内置 10 条兜底话术)`：实装的是**一条**可配字符串
>   `TemplateReply: "抱歉，当前服务暂时繁忙，请稍后再试或联系人工客服。"`（`provider_failover.go:68`），
>   "10 条"在仓内 0 命中。
> - "连续 3 次失败 → 临时禁用 5 分钟"：真实默认是 `DefaultFailureThreshold = 5`、
>   `DefaultCircuitOpenDuration = 60s`（`provider_failover.go:33-34`），达阈值置 `ProviderStatusDown`。
> - `MaxLatency` 这一条为真：它是路由规则字段（`dispatcher.go:58`），本地/云端各场景在
>   `dispatcher_register.go:116-122`、`:207-213` 里给了 3000~6000ms 的实际取值。

### 10.1 4 级降级

```
请求 → Provider "default" (本地 7B)
       ↓ 失败/超时
       Provider "3b_local" (本地 3B 兜底)
       ↓ 失败/超时
       Cache Hit (Redis 24h TTL)
       ↓ 失败/超时
       Default Template (内置 10 条兜底话术)
```

### 10.2 触发条件

- 单 provider P99 延迟 > 配置的 `MaxLatency`
- 连续 3 次失败 → 临时禁用 5 分钟（自动恢复）
- token_rate 异常突增 → 切到缓存或模板

---

## 十一、性能指标

> **实测口径（2026-09-22）**：下表 5 个指标名在仓内 **0 命中**，`go.mod` 也没有 Prometheus 依赖。
> 仓内确实有**自研**指标注册表（`internal/pkg/metrics`，`Handler()` 输出 `text/plain; version=0.0.4`
> 的 exposition 格式）和 HTTP 埋点中间件（`middleware/metrics.go:85`），但两者在非测试代码里
> **0 注册点**——`r.Use(middleware.MetricsMiddleware())` 与 `r.GET("/metrics", …)` 只以注释形式存在于
> 该文件用法说明（`metrics.go:10-11`），所以 `/metrics` 不会被暴露，`api_logger.go:22` 里跳过 `/metrics`
> 的前缀当前无对应路由。唯一可查的性能数据是 `layer_decision_logs`（`wall_ms`/`llm_skipped`，见 §5.3）
> 与 `llm_routing_logs`，取数方式是 SQL。要按本节接监控，需要先埋 `ai_agent_*` 指标并把中间件挂上路由。

### 11.1 核心指标

| 指标名 | 类型 | 标签 |
|--------|------|------|
| `ai_agent_wall_time_seconds` | Histogram | agent_type, layer, intent |
| `ai_agent_lcp_time_seconds` | Histogram | agent_type, poll_mode |
| `ai_agent_layer_decision_total` | Counter | layer, reason |
| `ai_agent_llm_call_total` | Counter | scenario, model, result |
| `ai_agent_fallback_total` | Counter | from_layer, to_layer, reason |

> 指标采集由 `layer_decision_logs` 表落库审计; 不依赖外部监控面板/告警通道, 故障排查通过 SQL 查询即可。

---

## 十二、FAQ 数据集

### 12.1 数据源

从 `E_commerce_Customer_Service/test_clean_v2.jsonl` 自动提取 Top 50 高频问答。

### 12.2 提取脚本

```bash
python3 scripts/extract_faq.py
# 生成 scripts/faq_seed.json (50 条)
```

### 12.3 导入工具

```bash
cd hivemtk/user-server
go run cmd/importfaq/main.go -input ../scripts/faq_seed.json
# 输出: [OK] / [SKIP] / [FAIL] + 统计
```

### 12.4 分类

| 分类 | 关键词 | 占比 |
|------|--------|------|
| logistics | 邮/快递/发货/韵达 | ~20% |
| pricing | 价格/优惠/折扣 | ~12% |
| aftersales | 退/换/退款 | ~10% |
| product | 尺码/颜色/材质 | ~10% |
| order | 活动/促销/订单 | ~8% |
| general | 其他 | ~40% |

---

## 十三、兼容性

### 13.1 向后兼容

逐条按 2026-09-22 代码实测给结论（前 3 条是"相对改造前"的历史口径，只能核到"入口/字段今天还在"，
核不到"没改过"）：

- 🟡 `SalesEngine.Handle` 入口签名不变 → 入口在，签名实测为
  `Handle(ctx context.Context, req *SalesRequest) (*SalesResponse, error)`（`sales_engine.go:180`）。
  "不变"需对照改造前版本，当前代码无法自证。
- 🟡 `SalesResponse` 字段全部保留 → 当前字段清单见 `dto/sales.go:302-331`（含 `Steps`、`Confidence`、
  `SendPlan` 等）。同样只能核到"今天还在"。
- 🟡 `llm_routing_logs` 落库格式不变 → 表与 `TraceID` 列在（`model/llm_routing_log.go:33`），历史口径同上。
- 🟢 PG schema 新增 3 表 → **为真**：`ai_perf_faq_sop_layer_migration.go:80` 等建
  `faq_entries` / `sop_templates` / `layer_decision_logs`。但"不改现有表"要打折扣：启动期 AutoMigrate
  会把建表 DDL 的 `layer_decision_logs.session_id VARCHAR(50)` 收敛成模型声明的 `varchar(120)`
  （开发库实测列宽已是 120），且 `v3_22_1_soft_delete_migration.go:53` 后续给这张新表补了 `deleted_at`。
- 🔴 Controller 路由新增 `/api/v1/ai/chat/poll` → **未实现**，`internal/router/` 里 0 注册（见 §一/§九 实测口径）。
- 🔴 FeatureFlag 默认全开启 → **与代码相反**：6 个 flag 里 5 个默认 `false`，只有 `sse_bridge` 默认 `true`
  且它只用于上报（见 §三 消费表）。默认部署下并行和 Layer1 都是关的，"关闭时回退到旧版 9 步串行"
  应读作"出厂即处于回退态"。

### 13.2 升级路径

> 口径修正：第 1 步"FeatureFlag 全开启（性能优化生效）"与出厂默认相反（见 13.1 最后一条），
> 且这里的"灰度 5%"**没有实现载体**——`internal/pkg/featureflag/` 只有 `flag.go` 一个实现文件，
> 没有任何按比例/按用户/按实例取模的分流代码，env 开关是进程级全量生效，
> 所以第 3~5 步实际是"按部署实例逐台改 env 后重启"，不是流量百分比灰度。

1. 部署新代码 → FeatureFlag 全开启（性能优化生效）
2. 跑 FAQ 提取 + 导入 → 验证 Layer1 命中
3. 灰度 5% `FF_PARALLEL=1` → 观察 P50/P90
4. 灰度 5% `FF_LAYER1=1` → 观察 P50/Layer1 命中率
5. 逐步放量到 100%

---

## 十四、错误码

> **实测口径（2026-09-22）**：下表 7 个 `AI_*` 码在仓内 **0 命中**，`internal/` 也没有任何一处按它们返回。
> 真实的错误码词表是 `internal/pkg/utils/error_code.go` 的 **36 个** `ErrorCode` 常量，
> 响应体为 `{code, message, data?}`（键名是 `message` 不是 `msg`；`response.go:82-105`），
> `code` 取字符串码，HTTP 状态由 `errorCodeFromHTTPCode` 反推（`response.go:176-196`）。
> 下表按"设计意图 → 现网等价物"读：
>
> | 设计稿错误码 | 现网等价行为 |
> |------|------|
> | `AI_LAYER1_DISABLED` | 不是错误：`LayerRouter` 照常返回 Layer2，并在 `layer_decision_logs` 落 `reason=layer1_disabled` |
> | `AI_FAQ_NOT_FOUND` / `AI_SOP_RENDER_FAIL` | 不是错误：Layer1 未命中即静默走 Layer2（`layer.go` 无错可返回） |
> | `AI_LLM_TIMEOUT` | LLM 超时是 `provider_failover` 的失败计数 + 熔断，对外表现为 `reply` 为空或模板话术；HTTP 侧最接近 `TIMEOUT_1004` / `SERVICE_UNAVAILABLE_6003` |
> | `AI_ALL_FALLBACK_FAIL` | 返回 `provider_failover.go:68` 的那一条 `TemplateReply` 文本，HTTP 200，无错误码 |
> | `AI_RATE_LIMITED` | 中间件直接吐 429 裸 JSON（`{code:429,msg/retry_after}`，见 §八），**不走** `Response` 信封；若走信封则映射为 `INSUFFICIENT_QUOTA_5003`（`response.go:188-189`） |
> | `AI_AGENT_MISMATCH` | 无此码；智能体隔离在 service 层按 `agent_id` 入参过滤，越权目前表现为"查不到数据"而非 403 |

> 另注：LLM 超时时长以配置为准 —— `config.yaml:111` 是 `timeout_seconds: 720`，
> 而 OpenAI 兼容客户端默认 `RequestTimeout: 60`（`aiagent/llm/llm.go:599`，单位秒），
> 表中"LLM 60s 超时"只对后者成立。

| 错误 | 含义 | 处理 |
|------|------|------|
| `AI_LAYER1_DISABLED` | FF_LAYER1=0 | 显式回退到 Layer2 |
| `AI_FAQ_NOT_FOUND` | FAQ 库空 | 走 SOP 模板 |
| `AI_SOP_RENDER_FAIL` | 模板渲染失败 | 走 Layer2 LLM |
| `AI_LLM_TIMEOUT` | LLM 60s 超时 | 走降级链 |
| `AI_ALL_FALLBACK_FAIL` | 4 级全失败 | 返回默认错误模板 |
| `AI_RATE_LIMITED` | 触发限流 | 客户端按 `Retry-After` 退避 |
| `AI_AGENT_MISMATCH` | 越权访问其他智能体数据 | 拒绝 + 审计日志 |

---

**版本:** v1.1（v1.0 为 2026-07-31 设计稿；v1.1 只加"实测口径"，未改写设计意图正文）  
**最后更新:** 2026-09-22  
**审查:** HiveMTK 架构组  
**v1.1 核对范围:** §一~§十四 + §13.1/§13.2 每条"现在如此"式断言，逐条 grep/读源码/查开发库核过；
判定汇总在文首表格，正文各节标题下有"实测口径"块。设计意图保留原文，不追改。
