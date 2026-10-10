# 全面数据库驱动配置方案（CONFIG_DB_DRIVEN_PLAN）

> 配套清单：`docs/HARDCODE_INVENTORY.md` + `hardcode-sweep/INVENTORY.csv`
> 适用工程：`hivemtk/user-server`（Go）+ `hivemtk/user-web`（Vue 3）
> 架构约束：五层 `Router → Controller → Service → Repository → Model`，本方案不破坏该约束

---

## 1. 目标与非目标

### 目标

1. **所有"运维想改"的标量有单一事实源**：库里一改就生效，不改代码、不重启。
2. **登记即接线**：进了参数中心页面就必须在生产代码里有读取点，否则门禁 rc=1。
3. **默认值只有一个出处**：`DefaultParamDefs()` 是唯一定义源，Go 常量、`defaultConfig()`、
   前端 `DEFAULT_LIMIT` 全部退化为"兜底 fallback"，不再各写一份。
4. **审计可追**：每次改动有 actor / old / new / 时间（现成 `config_param_audit_logs`）。

### 非目标（明确不做，避免范围失控）

- ❌ 不把**错误码、协议版本、端口/DSN、向量维度**做成可改配置
  （理由见 `HARDCODE_INVENTORY.md` §5；它们是契约或部署期变量）。
- ❌ 不把**中文文案**塞进参数中心。文案是 i18n 词条，生命周期与数值阈值不同
  （一个要翻译、一个要单位与范围校验）。
- ❌ 不把**业务字典/状态机**塞进参数中心。它们需要有序、标签、多语言，走字典表。
- ❌ 不追求"零硬编码常量"。Go 里应保留**编译期兜底值**：DB 不可用时服务要能起来，
  兜底值与 `DefaultValue` 一致，由测试钉住。

---

## 2. 现状诊断：4 套机制并存，能力不对等

| 机制 | 表 | 结构 | 默认值元数据 | 范围校验 | 审计 | 统一注册表 | 前端可读 | 读点数 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| **`config_params`** | `config_params` | 标量 KV | ✅ | ✅（duration 分支有漏洞） | ✅ | ✅ `DefaultParamDefs()` | ✅（`/api/manage/config-params`） | **43 / 115** |
| `system_config` | `system_config` | **固定 16 列单行** | ❌ 散在 `defaultConfig()` | ❌ 手写 clamp | ❌ | ❌ | ✅ | — |
| `system_config_kv` | `system_config_kv` | JSON blob | ❌ 散在各 service 常量 | ❌ | ❌ | ❌ | ❌ | 20+ 处各塞一个 blob |
| `feature_flags` | `feature_flags` | 结构化 + 灰度 | ✅ | — | ✅ | ✅ `RegisterCodeReference` | ❌ | — |

**四个致命问题**：

1. **登记 ≠ 接线**：115 条登记只有 43 条有人读，72 条僵尸（63%）。
   参数中心页面把 72 条渲染成可编辑控件，运维改完看到"保存成功"，但生产代码不读。
   这比对运维说"这个功能还没做"更糟。
2. **已暴露但没人读**：`system_config.MaxUploadSizeMB`（`gorm:"default:50"`，运维可改）
   与 `controller/upload.go:22 MaxUploadSize = 10MB` 并存，
   **上传链路一处都不读前者**。改配置页面显示成功、实际永远 10MB 拦截。
3. **key 全局唯一而 schema 说是复合**：gorm tag `uniqueIndex:idx_group_key`，
   实测库是 `UNIQUE(key)` 单列。同名跨 group 的第二条会被 `logger.Warnf` 静默丢弃。
4. **无默认值事实源**：同一个概念在 Go 常量、`defaultConfig()`、前端常量、
   甚至 3 个不同调用点各有一份默认值（`max_tokens` = 500/800/2048）。

---

## 3. 目标架构

```
                        ┌─────────────────────────────────────────┐
   运维改配置 ─────────► │  /api/manage/config-params （唯一写入口） │
                        └──────────────┬──────────────────────────┘
                                       │ UpdateValue（写库 + 审计 + 失效缓存）
                                       ▼
   ┌───────────────────────────────────────────────────────────────────┐
   │  ParamRegistry（唯一定义源，代码内）                              │
   │   DefaultParamDefs()  —— 现有 config_param_seeds.go 扩成全量      │
   │   + ReadPoints（编译期声明：每个 def 对应哪个读取函数）          │
   └──────────────┬────────────────────────────────────────────────────┘
                  │ SeedConfigParams（启动期 upsert，只刷定义列不碰 value）
                  ▼
   ┌────────────────────────────┐   ┌─────────────────────────────────┐
   │ config_params（标量 KV）    │   │ dictionaries（字典/状态机）     │ 新增
   │  阈值/上限/超时/TTL/开关    │   │  有序项 + 多语标签 + 状态迁移    │
   └────────────┬───────────────┘   └─────────────────────────────────┘
                │ ConfigParamService（60s TTL 缓存，按 group.key）
      ┌─────────┼──────────┬──────────────┐
      ▼         ▼          ▼              ▼
   Go Service  中间件      前端 /api/…    门禁脚本
```

### 3.1 `config_params` 只做"标量 KV"，加 3 列不加结构

新增列（**不改已有列语义，不破坏 AutoMigrate**）：

| 列 | 类型 | 用途 |
| --- | --- | --- |
| `owner` | `varchar(64)` | 归属服务/模块，如 `inbox_ingress`。用于"谁该来接线"的责任归属 |
| `source_ref` | `varchar(255)` | 硬编码原位置，如 `internal/service/inbox_ingress.go:30`。接完线后可一键自查 |
| `wired_at` | `timestamptz` | 首次接线的提交号/时间。用于区分"僵尸"和"已接线" |

> 不引入 `json` 列存结构化配置 —— `system_config_kv` 已经是这条路，
> 20+ 个 JSON blob 已经证明它必然退化成没人看得懂的泥潭。

### 3.2 `ParamDef` 扩展（在 `config_param_seeds.go` 内，不新建包）

```go
type ParamDef struct {
    Group, Key, Name, Description string
    ValueType                     string // int|float|bool|duration|string
    DefaultValue                  string
    Min, Max, Step                *string
    ReadOnly, Restart             bool
    Category                      string

    // 新增
    Owner      string // 归属模块，对应新增列 owner
    SourceRef  string // 硬编码原位置 file:line，对应新增列 source_ref
    Wiring     string // 读取点函数名，如 "inbox.Ingress.lockTTL()"；空 = 未接线
}
```

`Wiring` 非空即"声明已接线"，`check-config-param-readpoints.py` 的判据从
**「有 `Get*` 调用 或 Name 含『未接线』」** 升级为
**「`Wiring` 非空 且 该函数确实存在 且 已撤掉『未接线』标注」**——
从"扫字符串形状"升级为"声明与实现双向核对"。

### 3.3 收敛边界（哪些东西搬到 `config_params`）

| 现状 | 去向 | 判据 |
| --- | --- | --- |
| `system_config` 16 列 | **保留**（站点身份/品牌/开关，是单行实体不是 KV） | 扩展要改 model+migration 的，就不适合 KV 化 |
| `system_config_kv` 的 `password_policy` / `security.rules` / `handoff_rules` 等 **结构化规则** | **保留 KV**（它们是文档，不是数值） | 规则集合不是标量 |
| `system_config_kv` 里的**散落默认值** | 提到 `DefaultParamDefs()`，KV 只存覆盖值 | 默认值必须在定义源里有一份 |
| `feature_flags` | **保留独立表**（灰度百分比 + 分桶，语义与 bool 不同） | 不要用 bool 参数模拟灰度 |
| 硬编码阈值（346 个点位） | **进 `config_params`** | 见清单 §2 |
| 业务字典（44 项） | **新建 `dictionaries` 表** | 需要有序 + 标签 + 多语言 |
| 中文文案 | **i18n 词条**，不进配置 | 见清单 §4 |

### 3.4 新增表：`dictionaries`（业务字典，阶段四才建）

```go
type Dictionary struct {           // 表 dictionaries
    ID        uint
    Domain    string  // human_task / opportunity / payment ...
    Code      string  // 字典项代码，全局唯一
    LabelZh   string
    LabelEn   string
    SortOrder int     // 有序阶段用
    Group     string  // 次级分组（如 fix_layer）
    Meta      string  // JSON：颜色/图标/终态标记
    Enabled   bool
}
type DictionaryTransition struct { // 表 dictionary_transitions：状态机
    ID         uint
    Domain     string
    FromCode   string
    ToCode     string
    RequiredRole string
}
```

**前置要求**：字典可配 = 状态可被运维改 = 任何"当前状态是什么"的判断都要走查表。
改造前必须有一份"状态被写死在 if 里的位置"的全量清单，否则改一个状态会静默改变历史数据的语义。
**这就是为什么字典表排在阈值接线之后**——阈值错了能立刻看出来，字典错了不能。

---

## 4. 阶段计划

### 阶段 0：前置修复（阻塞项，必须先做）

> **进度（2026-10-10）：0.1 ~ 0.5 全部完成并已提交。**

| # | 动作 | 验收 | 状态 |
| --- | --- | --- | --- |
| 0.1 | 对齐 `config_param_test.go` 的 `defaultParamDefsWant = 114` 与脚本实测 115 | `go test ./internal/service/ -run TestSeedConfigParams` 绿 | ✅ |
| 0.2 | 修 `validateValue` 的 `duration` 分支：`ParseDuration` 失败不应 `return nil`，应按"秒数 float"解析并继续校验（与 `GetDuration` 的读取口径对齐） | 新增用例：非法 duration + 设了 min/max → 拒绝写入 | ✅ 另抽出 `parseDurationSeconds` 供读写两侧共用；min/max 自身解析失败不再吞错；删掉「无 min/max 就早退」导致类型校验被跳过的口子 |
| 0.3 | 修索引漂移：要么把 gorm tag 改成单列 `uniqueIndex` 与实际一致，要么写 migration 把它改成 `(param_group,key)` 复合。**倾向后者**（复合索引更符合设计意图，且 key 命名不用再带 group 前缀） | migration 幂等可重跑；`TestDefaultParamDefsKeysGloballyUnique` 按新口径改写 | ✅ 核实发现 migration v3.47.0 早已注册（`initial_schema.go:207`），漂移只在存量库；真正没闭环的是审计行只记 key（已加 `param_group` 列）、唯一性用例盯单列比库更严（已按 `(group,key)` 改写）、seed 失败只 `Warnf`（已改为逐条 Error + 汇总返回 error） |
| 0.4 | 修 `NewSystemConfigKVRepository()` nil DB panic → 改 nil-safe 或全量改用 `WithDB` 装配 | `TestFallbackNilDB` 覆盖 | ✅ 三方法统一返回哨兵错 `ErrKVStoreUnavailable`；`browser_automation_routes.go:38` 改走 `WithDB` 装配 |
| 0.5 | 修 `MaxUploadSizeMB` 无人读的实证 bug：`upload.go` 改读 `system_config.MaxUploadSizeMB`（保留 `MaxUploadSize` 常量为兜底） | 上传 11MB 文件在配置调大后成功；API 验证通过 | ✅ 优先级链 env > 库 > 10MB 兜底，逐格钉死在 `upload_maxsize_test.go` |

> 0.5 单独列，因为它是**已暴露给运维却完全不生效**的配置，比 72 条僵尸更值得优先修。
> 0.5 落地时确认的一处取舍：`ResolveUploadMaxBytes` 刻意**不**走 `GetConfig`——那条路读不到行时
> 会拿 `defaultConfig()` 的 50MB 顶上，而上传链路真正生效的兜底是 10MB；跟着走等于把存量站点
> 的上限悄悄放宽 5 倍。50MB 与 `middleware/body_limit.go:36` 注释声明的「本仓最大合法单文件 50MB」一致，
> 说明 10MB 才是那个偏离产品意图的硬编码。

### 阶段一：72 条僵尸参数接线（投入产出比最高）

> **进度（2026-10-10）：阶段一全部 8 批已入库（8/8）。**
> misc(16) / confidence(5/7) / agent_llm(7) / cache(5) / agent_tool(4/5) / telemetry(4)+workflow(4) /
> inbox_sales(4)+session(3)+pagination(1) / sales(1)+middleware(2)+knowledge(2)+channelgw(1)。
> 门禁读数从 `wired=40 / UNDECLARED=72` 变为 `wired=102 / 已声明未接线=13 / UNDECLARED=0 / 声明过期=0`。
>
> **入库位置（如实记录，勿按 commit message 找）**：
> - 第 1 批 misc(16) → `c75c78b6 feat(config-params): 阶段一第1批 —— misc 组 16 条僵尸参数接线`
> - 第 2 批 confidence(5/7) → **`86b92349`**。这批的 17 个文件当时已在暂存区等待提交，
>   被一次并行的他人提交（`chore(scripts): 删孤儿重复门 check-enum-consistency.py`）一并带走，
>   因此 commit message 与内容不符。内容已逐文件核对完整（17/17），且在干净检出上全绿，
>   故不改写已推送的历史，只在此登记归属。
> - 第 3 批 agent_llm(7) → `97ebaefb feat(config-params): 阶段一第3批 —— agent_llm 组 7 条僵尸参数接线`
> - 第 4 批 cache(5) → `8bcc0443 feat(config-params): 阶段一第4批 —— cache 组 5 条僵尸参数接线`
> - 第 5 批 agent_tool(4/5) → `5fb6f400 feat(config-params): 阶段一第5批 —— agent_tool 组 4 条僵尸参数接线`
> - 第 6 批 telemetry(4)+workflow(4) → `21dff6d0 feat(config-params): 阶段一第6批 —— telemetry(4)+workflow(4) 组 8 条僵尸参数接线`
> - 第 7 批 inbox_sales(4)+session(3)+pagination(1) → `7a0ae7b0 feat(config-params): 阶段一第7批 —— inbox_sales(4)+session(3)+pagination(1) 组接线`
> - 第 8 批 sales(1)+middleware(2)+knowledge(2)+channelgw(1) → 本提交。
> - 台账回填另计：`2f2ba9cb`（第 3 批）、以及各批随附的 docs 提交。
>
> **第 8 批查出一个门禁管不到的 bug（比 72 条僵尸更隐蔽，值得单独记）**
>
> `channelgw.ws_push_interval` 之前被种子里标着「未接线」，本批去接线时发现它其实**有读取点**
> —— `internal/channelgw/ws.go` 的 `runtimeWSPushIntervalDefault` 真真切切在调
> `GetDuration(ctx, "channelgw", <key>, …)`。但那个 `<key>` 是 `"ws_push_interval_default"`，
> 而种子里登记的是 `"ws_push_interval"`。多一个 `_default` 后缀，于是
> **每次读都 miss、每次都回落到兜底值 2s**：参数在管理台改得动、页面显示保存成功、
> 代码就是读不到。同组另外 4 条 key（`ws_register_timeout` / `ws_read_idle_timeout` /
> `ws_write_timeout` / `ws_pipeline_timeout`）都是对的，所以这是一处单点笔误。
>
> **这类故障 readpoints 门结构上抓不到**：它检查的是「种子 key 有没有读取点」，
> 而这里读取点有，只是用的不是那个 key。反向的「登记了没人读」它能抓（判 UNDECLARED），
> 正向的「有人读但读的是别的 key」它一个字都看不出来。本批已改正 key，
> 并补 `TestRuntimeWSPushIntervalReadsSeededKey`（真库改值 → 断言读取函数读到改后的值，
> key 再写错就会红）把这类回归钉住。
>
> **第 8 批：8 条目标里接了 6 条，另 2 条经核实确实没有生产消费点**
> （同第 2/5/7 批口径：如实保留「未接线」标注并把 Description 改写为写明真实原因）：
> - `wecom.error_rate_degrade`：`WeComErrorRateDegradeThreshold = 0.3` 只出现在常量声明和
>   一句**撒谎的注释**里（「运行时通过 GlobalConfigParam() 读取」）—— 根本没有对应的读取函数。
>   现存实现 `computeHealthScore` 的成功率判定是写死的三档（`<50` 扣 30 / `<80` 扣 15 /
>   `<95` 扣 5），单一「错误率阈值」表达不了这种递减分档；硬套进来只能新造第四档或改掉现有
>   扣分行为，所以没接。**顺带修掉那句撒谎的注释**（规则2：不留"已发现但不改"的东西）。
> - `sales.insight_limit`：`trace_learning.TopInsights` 的 `limit` 是**入参不是默认值**，
>   全仓唯一调用方是 `insights_test.go`（且三处都硬传 3），生产链路没有任何地方取这个数量。
>
> **第 8 批顺带确认的一处「配额降级半接线」**：`wecom.quota_degrade` 是真接线
> （`runtimeWeComQuotaDegrade` 有真实 `GetFloat`），但 `computeHealthScore` 的配额三档
> （`>0.95` 扣 25 / `>quotaDegrade` 扣 15 / `>0.7` 扣 5）里只有中间一档读了参数，
> **上下两档 0.95 / 0.7 仍是写死的**。所以把 `quota_degrade` 调到 0.95 以上时，
> 上面那档会先命中、参数等于失效。这属于「新增点位」（阶段二/三范围），本批只如实记录不扩范围。
>
> **第 7 批：16 条目标里只接了 8 条，另 8 条经核实确实没有生产消费点**
> （同第 2/5 批口径：如实保留「未接线」标注并把 Description 改写为写明真实原因，
> 不为让门变绿而凭空造读取点）：
> - `pagination.page_max_size` / `page_default_size`：对应的
>   `internal/pkg/utils/pagination.go` `ParsePagination` / `ParsePaginationOffset`
>   **全树只有 `pagination_test.go` 调用**，零生产调用方；`page_default_size` 在
>   `ParseCursorParams` 里那条兜底分支又被 operation_log / security_audit / customer
>   三个调用方传的正数 `defaultLimit` 遮蔽。顺带发现种子写 100、代码写 200 本就不一致。
> - `wechat.chat_ws_ping_period` / `chat_ws_pong_wait` / `chat_ws_write_wait`：常量在
>   `internal/controller/chat_ws.go` 有读取点，但承载它的
>   `internal/router/ws.go` `RegisterWSRoutes` **全树零调用方**（`GET /ws/chat`
>   从未注册，实测落到 SPA 的 NoRoute 兜底）。另：后端根本没有微信渠道 WS 客户端，
>   `internal/channelbot` 下只有 core/qq/telegram/whatsapp，种子「微信渠道 WS」本身名不副实。
> - `bridge.polling_max_timeout` / `polling_default_timeout` /
>   `bridge.max_reply_content_bytes`：承第 6 批已核实，长轮询未实现，
>   `maxReplyContentBytes` 自带 `//nolint:unused`。
>
> **第 7 批的又一处「种子与代码不一致」**：`session.max_delay_seconds` 的种子写
> 「触达流水线排程允许的最大延迟，超过视为放弃」，实现
> `internal/content/service/marketing_flow.go handleDelay` 其实是「营销流程 delay 节点
> 单次等待时长的上限，超过则截断」。代码里没有「超期放弃」这个概念，已连接线带改写一并对齐。
>
> **第 7 批踩到的一个坑（值得写下来）**：seam setter 的 `nil` 语义是「**不注入**」
> （`if fn != nil` 守卫，防装配顺序错时把兜底值顶掉），不是「复位」。按直觉写
> 「传 nil 后应该回到兜底」的用例会红，而且红得有理有据。本批复用了前六批的同一范式，
> 于是测试里统一改成两件事：① 把「传 nil 等于撤销注入」改写成「传 nil 是空操作」并
> 先重新注入一个已知的非兜底值再断言；② `t.Cleanup` 里同包直接写包内 `provider` 变量复位
> （生产 setter 复位不了，测试能）。**这个坑不是本批独有的，前六批的 seam 测试也写了
> 「传 nil 等于撤销注入」，只是它们的断言恰好在 provider 仍是被注入值的位置才没红。**
>
> **第 6 批的两条「种子与代码不一致」处置**（这是接线时才发现的、写死没人看得出来的漂移）：
> 1. `telemetry.trace_sink_buffer` 种子写 8192，代码兜底是 `aiagent/llm/trace_sink.go` 的 2048。
>    取**代码的 2048** 回填种子，理由：参数中心的默认值语义是「没人配置时系统现在的行为」，
>    不是「更合理的目标值」。照抄 8192 等于升级即把每个 trace sink 的缓冲内存翻 4 倍，
>    而丢弃事件只在缓冲区满时暴露 —— 越晚暴露、越难定位。
> 2. `telemetry.geo_position_window` 的 Name/Description 与实现完全不是一回事：种子写
>    「Geo 地理位置统计的滑动窗口大小」，实现 `geo/service/metrics.go dedupMatches` 里
>    比较的是**字符偏移**（`m.start - prevEnd > window`），语义是「相邻两处品牌词提及
>    间隔小于该字符数即算同一处」。已一并改写，否则配的人会按"条数"去理解一个字符参数。
>
>
> **第 4 批踩到的三个坑（都写下来）**：
> 1. `internal/controller/platform.go` 的 `platformCacheTTL` 原本是**函数内的局部 const**，
>    只改名不提到包级，新加的包级 `PlatformCacheTTL()` 读不到它 —— 只跑
>    `go build ./internal/controller/` 才暴露，只 `go vet` 挑过的子包漏掉了这个包。
>    **凡是加 seam 的包，都要单独 `go build` 一次，不能只 vet 自己"以为相关"的那几个。**
> 2. `internal/aiagent/rag/retrieval` 的**包名是 `ragretrieval`**，导入必须写别名；
>    同目录的 `NewTranslationCache(client, ttl, prefix)` 参数顺序也容易记反。
> 3. `git worktree add` 到 `/tmp/xxx` 时，若该路径被更早的会话留成过 worktree，
>    git 会报「路径已存在」而**不切换 base**，导致把文件倒进了一个旧 commit 的检出里，
>    验证结论完全无效。**每次用新目录名；add 之后必须 `git worktree list` 确认 HEAD 对得上。**
>
> **教训（写下来防止再犯）**：暂存区是**共享**的，不属于某一条泳道。跨多个工作阶段把文件
>   长期留在暂存区，等于把「我下一条 commit 会带上它们」这件事暴露给任何并行提交的人。
>   正确姿势是**验完立刻提交**，把暂存区停留时间压到最短；`git commit` 前先
>   `git diff --cached --name-only` 核一遍自己到底要带谁走。
>
> **工作区随时可能是半成品状态**：本轮多次撞见他人正在编辑、留下未使用 import 导致
>   `internal/service` 编译不过。碰到这种共享工作区的破损，正确做法是**把自己的文件倒进
>   干净 worktree 验证**（`git worktree add --detach /tmp/xxx HEAD` + `git show ":$f" >`），
>   既能证明「我的改动是绿的」，又不去动别人正在写的东西。
> confidence 组 7 条里只接了 5 条：`persona_default_threshold` 与 `persona_max_retry` 的实现
> `internal/service/persona_evaluator.go` 已随 `9a5f716c` 整体删除（749 行实现 + 958 行测试），
> 代码里不存在等价能力，**不为了凑数凭空造一个读取口**，两条如实保留「未接线」标注，
> 并在 Description 里写明「实现已随 9a5f716c 删除」以及「若要暴露重生成上限请另立
> `humanize_max_retry` 而不是改这条」——免得后来人以为只是漏接线。
> 余下 2 条 UNDECLARED 是 `cache.faq_ttl` 与 `agent_llm.default_semantic_threshold`，
> 它们的唯一读取点在 `app/faq_cache_wiring.go`——一个尚未入库的在途文件，属于别人正在做的
> FAQ 语义缓存特性（该特性还要改 `rag/cache/service.go` 的构造签名并连带两个测试文件）。
> **不为了让门变绿而把别人的半成品特性带进本提交**；等对方入库后这两条自然转绿。

- **动作**：清单 §7 对照表右列的常量，逐个换成 `config.GetInt/GetDuration/GetBool/GetFloat/GetString`，
  保留原常量名做 fallback（避免大面积改调用点）。
- **接线范式**（以 `InboxLockTTL` 为例，符合五层架构：常量在 Model 层、读取在 Repository/Service 层）：

```go
// internal/service/inbox_ingress.go
func InboxLockTTL(ctx context.Context) time.Duration {
    d, _ := GlobalConfigParam().GetDuration(ctx, InboxIngressGroup, "inbox_lock_ttl", InboxLockTTL)
    return d
}
```

- **同步**：种子条目 `Wiring` 字段填上函数名，**撤掉 Name 里的「（未接线）」与
  Description 里的「【当前不生效，改了也没人读】」**——门禁第 2 条会检查标注是否过期。
- **验收**：
  - `check-config-param-readpoints.py` 输出 `未接线且已声明=0`
  - 每个域一个回归：改库 → 60s 内行为变化（`configParamTTL = 60s`）→ 改回

- **第 1 批（misc 组 16 条）实际落地形态与原计划的出入**：

| 原计划 | 实际落地 | 为什么改 |
| --- | --- | --- |
| 每条常量旁写一个 `GetXxx(ctx)` | 统一 `const xxxDefault` + `var xxxProvider` + `SetXxxProvider(fn)` + 包级 `xxx()` | 点位横跨 `model` / `repository` / `middleware` / `pkg` / `ops`，这些包**不能反向依赖 `service`**（Model 层禁止 import 上层），就地接线只能各造函数变量。装配层 `internal/app/misc_params_wiring.go` 是唯一能同时看见全部 16 条的地方 |
| 逐个替换调用点 | 只改**取值那一行**，调用点结构不动 | 16 条里有几条的常量被多处引用（`text_truncate_max_bytes` 在 `pkg/tracing` 等），逐点替换会把 diff 撑大到无法 review |
| — | provider 传 nil 视为不注入；`xxx()` 拿到非正数一律回落兜底 | 装配顺序错时不能把兜底值顶掉；`make(chan, 0)` 会死锁、上限取到 0 会把内容全截没 |
| — | `backup_page_size` 按「单页上限 clamp」接入，保留默认分页 10 | 种子默认 1000 只在「上限」语义下说得通。若当「默认分页大小」接入，升级后备份列表默认每页会从 10 行静默变成 1000 行 |
| — | `sop_scheduler_interval` 不与 `utils.LongTimeout` 共用常量 | 一个是外部调用超时、一个是调度周期，共用会让「改超时」意外改掉调度频率 |

### 阶段二：P0 新增 58 个点位入库

按 §2.1 四类分四个 PR，每 PR 一个类：

| PR | 内容 | 关键点 |
| --- | --- | --- |
| 2a | 上传/body 上限（`upload_max_size_mb`、`max_json_body_mb`） | **合并三份副本**为 1 键；`system_config.MaxUploadSizeMB` 保留为兼容列，读新键优先 |
| 2b | Webhook 验签降级开关（**不新增参数键，改为收敛护栏强度**） | 方案原文「单键覆盖 14 个硬编码点、默认改 `false`」经实测**前提全部不成立**，实际处置见下方「2b 已落地」；正确地讲这是「全面数据库驱动」的一个受控例外 |
| 2c | 锁与幂等 TTL（收件箱/消息中台/人工接管/TG 轮询锁，13 个点位） | **已落地**：13 个点位逐条核实后只接得动 6 条，新建 `lock` 组 7 条（含 1 条刻意不接）；`message_hub_idem_ttl` 单独标 `Restart=true`；`TG 轮询锁`那条早在 1.1 已接、不重复；`human_escalation` 三条是死代码不建种子。实际处置见下方「2c 已落地」 |
| 2d | 渠道与配额上限（QQ/TG 长度、SSE 每 IP 连接、配额降级阈值，15 个点位） | **已落地**：15 个候选逐条核实后接 10 条，新建 `channelbot`（5）+ `sse`（3）+ `bridge`（2）三组种子。`internal/service` 反向 import `channelbot/{qq,telegram,core}`，这三个子包走 provider seam + 装配层注入；`service/sse_hub.go` 与 `bridge/sse.go` 本就能直接调 `GlobalConfigParam()`，走直连不造 seam。实际处置见下方「2d 已落地」 |

- 每条种子的 `Min/Max` 必填（`config_params` 已支持），避免运维填出 `max_tokens=999999`。
- `ValueType` 用 `duration` 的，`Step` 填合理步长（如 `30s`）。
- **验收**：`scripts/api_verify_full.py` 跑通 + 每个 PR 附"改值→行为变化→改回"三步证据。

#### 2a 已落地（2026-10-10）

上传/body 上限两条入库，`wired 102→104`、种子条目 115→117、锚点同步到 117。

**上传链最终优先级**（`internal/controller/upload.go` 的 `resolveUploadMaxSize`）：

```
env UPLOAD_MAX_SIZE > misc.upload_max_size_mb > system_config.max_upload_size_mb（兼容列）> 10MB 代码兜底
```

- `misc.upload_max_size_mb` **默认 0**，语义是「不覆盖，沿用下游」，`Min: "0"`。这个默认值不是偷懒，是刻意取舍的两个反面各错一遍：填 `10` 会把运维已在「系统配置」页调好的 200MB 又压回 10MB（把阶段 0.5 刚修好的 bug 原样退回）；填 `50` 则让新装站点从 10MB 静默放宽 5 倍。填 0 时存量与新装站点行为与上线前**逐字节一致**。
- 兼容列保留在第二顺位：存量站点已经在那儿调过值，新键一上线就把它顶掉会造成「配置莫名失效」。
- 该键是**每请求读**，改完即时生效（上传入口不停服）。
- 配套 `mbToBytes(mb)` 辅助：非正数原样返回 0，即「0 = 未配置」是本链上下游共用的哨兵值，混进换算会把 0 变成"0 字节"从而打死所有上传。

**body 链最终优先级**（`internal/middleware/body_limit.go` 的 `BodyLimitFromEnv`）：

```
env MAX_JSON_BODY_MB > middleware.max_json_body_mb > DefaultMaxJSONBodyMB(8)
```

- 默认 8 必须保持：`TestGlobalDefaultDoesNotTightenExistingCaps` 钉死全局默认值 ≥ webhook 可调顶格 4MB，调小会把运维已放行的高段请求截断。
- `Min: "1"` **有意不给 0**：env 侧 `<=0` 的「不限制」是部署层排障应急开关，参数中心是运维日常调的面，再开一个关掉全局 body 封顶的口子风险大于收益。Description 里写明「临时放开请用 env 设 0」。
- `Restart: true` —— 这是 **`ParamDef.Restart` 字段首次被真正用上**（此前 115 条种子一条都没填）：`router.Setup` 里 `r.Use(middleware.BodyLimit(...))` 是**装配期读一次**，标成 false 会让运维改完看不到任何生效提示。
- `maxMultipartMemoryMB` 保持独立不联动：它是把 gin 默认 32MB 主动调小的内存优化，跟随 body 上限会顺带把每个并发上传的内存放大。

**接线范式**：`middleware.max_json_body_mb` 走 provider 注入（middleware 不能反向依赖 service）；`misc.upload_max_size_mb` 走读取函数直连参数中心（读取点在 controller，本来就能依赖 service），两者的正确性分别由 `internal/app/upload_body_limit_params_wiring_test.go` 与 `internal/controller/upload_param_limit_test.go` 的真库行为测试盯住。

**未纳入本批**（如实记录，避免后人误以为已覆盖）：前端第三份副本 `user-web/src/views/system/MaterialLibrary.vue:323` 的 `maxSize = 10` 属阶段 3.3；`knowledge_base_import.go:35 MaxUploadFileSize = 50<<20` 已在阶段 1.8 作为 `knowledge.max_upload_file_size` 单独入库，与本条不是同一个上限。

#### 2b 已落地（2026-10-10）——「不 DB 化」是一个受控例外

**方案原文的三处前提经实测全部不成立，如实记录以免后人按错的方向再走一遍**：

| 原文说法 | 实测 |
| --- | --- |
| 14 个硬编码点 | **只有 3 个真实读取点**（`internal/service/webhook.go:509`、`internal/channelbot/telegram/telegram.go:463`、`internal/content/service/marketing_flow_action.go:703`）；其余 11 处是错误文案/日志里提到变量名 |
| 默认 `true`（硬编码开着） | 三处**本来就 fail-closed**（只认字面量 `"true"`，且都要求「该账号压根没配密钥」时才可能豁免），不存在要改的默认值 |
| 单键可覆盖 | 三把开关护栏强度分三档（见下），语义各不相同，**一个键覆盖不了** |

三把开关的真实护栏强度（这是本批真正要解决的问题）：

| 变量 | 原护栏 | 风险 |
| --- | --- | --- |
| `ALLOW_INSECURE_WEBHOOK` | 启动护栏：`APP_ENV` 非开发值 ⇒ `log.Fatalf` 拒绝启动 | 挡得住 |
| `ALLOW_INSECURE_TELEGRAM_WEBHOOK` | **完全没有**，仅 `logger.Warnf` | **生产误设 ⇒ 验签被静默跳过，启动不报错**（本批修的真实缺陷） |
| `MARKETING_WEBHOOK_ALLOW_INSECURE` | 运行时判 `config.IsDevelopmentEnv()` | 挡得住，但只挡运行时，启动不报 |

**为什么不做成 DB 参数**（这是本节的核心结论，比"多接一条线"重要得多）：

把验签绕过做成数据库可改，等于让 admin API 的写操作能在**运行时翻转签名校验** —— 相比现在的"env + 启动 `log.Fatalf` 护栏"是**安全回退**：DB 改完即时生效、不需要重启、没有启动拦截、且改完那一刻没有任何日志。env 的代价（要重发一次部署）恰恰是它作为安全闸的屏障。

另一个更朴素的理由：**DB 镜像 env 状态会造假值**。若为了让控制台"看得见"而建一条种子行，它的 `Value` 要么是启动时写入的（只反映最后一次重启时的 env，与当前进程实际读到的 env 可能不同），要么永远静态 false（控制台显示"未开启"但 env 可能正开着）。前者需要 ReadOnly 的键在启动时被写入（`ReadOnly` 与「服务端自写」自相矛盾），后者是纯误导。因此**不给这三处加 `config_params` 种子行**，`wired` 计数不变。

**实际落地的四件事**：

1. **修真缺陷**：`telegram.go` 旁路从裸 `os.Getenv(...)=="true"` 改为 `insecureTelegramWebhookAllowed()`（开关 **且** `config.IsDevelopmentEnv()`），与 marketing 那处同口径。
2. **补第二处缺陷**：`internal/channelbot/whatsapp/whatsapp.go` 的 `VerifyWebhook` 在空 secret 时从 **fail-open 改为 fail-closed**（旧注释写"与项目既有行为一致"，实际是本仓唯一一处无开关、无护栏、无审计痕迹的验签绕过）。改后打 `logger.Warnf`。
3. **扩启动护栏**：`insecureWebhookStartupError` 加第 5 个参数 `allowInsecureTelegram`，`guardInsecureWebhookAtStartup` 同时读两个变量 —— 生产误设任一都拒绝启动。
4. **钉住语义**：`"TRUE"`/`"1"`/`"yes"`/`"true "`（带尾空格）**不算开启**，测试逐格覆盖，避免将来有人图省事改成 `strconv.ParseBool` 放宽口径。

**这是「全面数据库驱动」的一个受控例外，不是漏做**：判定标准是——**能不能让运行时的一个写操作翻掉安全闸**。阈值、TTL、上限都是"调坏了我看得见、改回来就行"；验签旁路不是，翻错的代价是外部伪造回调直接进业务。凡属此类的一律留在 env + 启动护栏，本批次起在两份文档中逐条登记。

#### 2c 已落地（2026-10-10）

锁与幂等 TTL 入库，新建 `lock` 组 7 条（117→124），`wired 104→110`、锚点同步到 124。

**13 个点位逐条核实后，只有 6 条接得动**：

| 种子 key | 默认（秒） | Restart | 代码点位 |
| --- | --- | --- | --- |
| `lock.inbox_human_ttl` | 86400 | false | `internal/service/inbox_ingress.go:28` `InboxLockTTL`；读取点 `:227`（`LockSessionForHuman` 的 `cache.Set`）、`:251`（`RenewSessionHumanLock` 的 `ttl<=0` 兜底）、**`:231` 的裸 `24*time.Hour`** |
| `lock.inbox_pending_ttl` | 300 | false | 同文件 `InboxPendingTTL`；读取点 `:290` `AppendPendingMessage` 的 `cache.LPush`（生产调用方 `internal/service/ai_debounce.go:68`） |
| `lock.inbox_content_dedup_ttl` | 300 | false | 同文件 `InboxContentDedupTTL`；经既有 `contentDedupWindow()`（`:140-145`，字段优先、零值回落）在 `inbox_ingress_ingest.go:167` 的 `SetNX` |
| `lock.ingest_lock_ttl` | 25 | false | 同文件 `IngestLockTTL`；读取点 `:635` `withIngestLock` 的 `SetNX`（生产调用方 `inbox_ingress_persist.go:137`） |
| `lock.ai_processing_ttl` | 120 | false | 同文件 `InboxAIProcessingTTL`；读取点 `:600` `markAIProcessing`（生产调用方 `:483`/`:585`，防重复回复的并发闸） |
| `lock.message_hub_idem_ttl` | 86400 | **true** | `internal/service/message_hub.go:172` `MessageHubDefaultIdemTTL`；读取点 `:255`（构造期 `idemTTL` 字段初值）与 `:373`（`cache.Set`） |
| `lock.ai_lock_ttl` | 15 | false | **刻意不接**，见下 |

`internal/service/inbox_ingress.go:231` 有一处**裸 `24*time.Hour`**（人类锁 reason 键的 TTL），与人类锁同一把但走字面量。本批一并接上与锁共用同一参数：理由先于锁过期 = 被接管会话在界面上显示不出「为什么被接管」，理由比锁活得久 = 一段无主数据，两者都不值得单开一个参数。

**`lock.message_hub_idem_ttl` 是本组唯一 `Restart: true`**：它的读取点在账号服务的构造期（`NewMessageHubServiceWithDB` 的 `idemTTL` 字段初值），而账号服务有 6 处构造点全在启动时完成（`qq_account.go:146`、`wecom_integration.go:34`、`feishu.go:115/562/932`、`email.go:55/63`），运行时改值对已启动的账号不生效，必须重启。同一条链上已有 `WithIdemTTL`（`message_hub.go:262`）可显式覆盖，但全树无生产调用。其余 5 条都是每请求/每次调用读，`Restart: false` 热生效。

**`lock.ai_lock_ttl` 刻意不接**：唯一读取点 `:265` 在 `tryAcquireAILock`，该函数自带 `//nolint:unused //// 仅被 *_test.go 引用，生产路径未用`，`ReleaseAILock`/`IsSessionAIBusy` 同样无生产调用。真正在跑的并发闸是 `ai_processing_ttl` 那条。保留「（未接线）」标注并在 Description 写明这层关系——**去掉标注测试会先红**（`TestAILockTTLStaysUnwired` 反向钉死）。

**`human_escalation.go` 三条常量不新增种子，只在文档记死代码事实**：`HumanLockDefaultTTL`/`HumanLockReasonTTL`/`LockExpiryCheckInterval`（`human_escalation.go:22/25/27`）所在的 `HumanEscalationManager` 在生产链路上死了 —— `NewHumanEscalationManager` 唯一生产引用为零（只有 `human_escalation_test.go` 19 处调用），`TriggerCensorshipEscalation`/`IsSessionLockedForHuman`/`StartLockExpiryChecker` 全树零生产调用。这是「运行时有同名锁、但实现不在这个文件」的典型：真正在跑的是 `inbox_ingress.go` 的 `InboxHumanLockKey` 系列（`hivemtk:lock:human:`）。给一条死代码建参数只会让人以为改它有用。

**`misc.polling_lock_stale_threshold` 早在 1.1 已接**，本批不重复。

**顺手修掉一个真 bug**：`internal/cache/memory.go` 的 `PopAll` 不判过期。`Get`/`Exists`/`LRange`/`scanExpired` 全都判 `item.expiration`，唯独 `PopAll` 只判 `listMode` 和 `len(listItems)`，所以 `LPush(key, v, 60ms)` 写入的列表过了 TTL 仍被原样 Pop 出来。影响面：只在内存缓存（`cache.NewMemoryCache()`，无 Redis 时的回退路径）上显形；Redis 路径由 `EXPIRE` 保证不受影响。对应业务：AI 防抖暂存的入站消息本该随 TTL 过期，内存回退下会被迟到的 Pop 重新投给 AI。修法是补一段与 `Get`/`Exists` 逐字同构的过期判断（`IsZero() && Before(time.Now())`），顺带把过期项从 LRU 摘掉。**这个 bug 是 `TestPendingMessagesExpireByConfiguredTTL` 首跑红暴露的**——不是因为接线写错，是「写对了才看得见底下有坑」。

**`lock` 组是方案新增的一个 group**（原计划倾向塞进 `session` 组）：这些是「正确性」型参数（改错 = 死锁或重复处理，不是调优），与 `session` 组「交互窗口」型混在一起会让运维在调 TTL 时误判影响面。每条 Description 都写「改小/改大会出什么事」。6 个 seam 集中在新建的 `internal/service/lock_idempotency_params.go` 单文件里：常量原本分处 `message_hub.go`/`inbox_ingress.go`/`inbox_ingress_ingest.go` 三处，散着看不出「锁的世界观」全貌。

**验证**：gofmt 空、`go build ./...`、`go vet ./...` 全过；`go test ./internal/cache/ ./internal/app/` 绿（app 100s）、`internal/service` 全包 **ok 852s**。门禁 `wired 110 / 已声明未接线 14 / UNDECLARED 0 / STALE 0`，rc=0，种子 124 条。行为级测试覆盖 6 条（`lock_idempotency_behavior_test.go`）：锁在注入的小 TTL 后真的消失、TTL 内第二次获取真的失败（防重复回复）、结构体字段非零时仍优先于 provider。

#### 2d 已落地（2026-10-10）

渠道与配额上限入库，新建 `channelbot`（5）+ `sse`（3）+ `bridge`（2）三组种子（124→134），`wired 110→120`、锚点同步到 134。

**15 个候选逐条核实，接 10 条**：

| 种子 key | 默认 | Restart | 代码点位与消费点 |
| --- | --- | --- | --- |
| `channelbot.qq_message_max_len` | 2000 | false | `internal/channelbot/qq/qq.go:38` `QQMessageMaxLen`；`SendMessage` 分段 + `splitQQMessage` 兜底，生产调用方 `service/qq_account.go:258` |
| `channelbot.tg_message_max_length` | 4096 | false | `telegram.go:26`；`SendMessage` + `splitMessage` 兜底，生产调用方 `service/telegram_gate.go` 6 处 |
| `channelbot.tg_inline_rows_max` | 100 | false | `telegram.go:27`；`buildInlineKeyboard` 截断 |
| `channelbot.tg_inline_buttons_per_row_max` | 8 | false | `telegram.go:28`；`buildInlineKeyboard` 截断 |
| `channelbot.http_timeout` | 30s | **true** | `channelbot/core/core.go:26`；`NewBaseClient` 构造期读两处（`:108` `HTTPClient.Timeout`、`:124` 字段） |
| `sse.heartbeat_interval` | 15s | false | `service/sse_hub.go:27`；`:392` `time.NewTicker` |
| `sse.max_conn_per_ip` | 5 | false | `sse_hub.go:28`；`:152` 每 IP 连接闸 |
| `sse.client_buffer_size` | 100 | **true** | `sse_hub.go:62`；`NewSSEClient` 构造期 `make(chan,…)` |
| `bridge.sse_max_backlog_events` | 1000 | false | `bridge/sse.go:57`；`:770` 回放截断 |
| `bridge.sse_bus_buffer_size` | 100 | **true** | `bridge/sse.go:58`；`:178` `buffer:` 构造期 |

三条 `Restart: true` 全是**构造期读取**：账号 HTTP 客户端、SSE 客户端事件信道、SSE 广播总线缓冲。运行时改值只对新建对象生效，已连接的客户端拿不到，必须重启——这一点写进 Description，否则运维改完看不到效果会以为又出僵尸参数。

**不接的 5 条**：`qq.CallbackOpVerify=13`（QQ 回调 event op 码，属协议常量不可调）；`bridge.sse_heartbeat_interval` / `bridge.sse_max_stream_duration`（**早已 DB 驱动**，`runtimeSSEHeartbeatInterval`/`runtimeSSEMaxStreamDuration` 已在读，不重复接线）；`wecom.error_rate_degrade`（承 1.8：`computeHealthScore` 的成功率判定是写死三档，单一阈值表达不了，保留未接线标注）。

**架构判断：两个子域走两条路，不要强行统一**。`internal/service` 反向 import `channelbot/{qq,telegram,core}`（`webhook.go:14-15`、`qq_account.go:14-15`、`telegram_gate.go:14`、`channel_error.go:12`、`feishu.go:24-25`），这三个子包若 import service 即成环 ⇒ 必须走 **provider seam + 装配层注入**（`internal/app/channelbot_params_wiring.go`，5 条显式闭包）；而 `internal/service/sse_hub.go` 与 `internal/bridge/sse.go` 本就能直接调 `GlobalConfigParam()`（`bridge/sse.go` 已有 `runtimeSSE*` 先例）⇒ 走**直连**，不造 seam。硬给后者造 seam 会多出 5 个一辈子只被读一次的函数。

**顺带修掉 1.8 发现的「配额降级半接线」**：`service/wecom_account_health.go` 的 `computeHealthScore` 原本有两套独立阈值——`quotaRate > 0.95` 写死扣 25 分，`quotaRate > quotaDegrade`（读 `wecom.quota_degrade`，默认 0.9）扣 15 分。运维把参数调到 0.95 以上时，最高档先命中，参数根本轮不到生效——**「已登记 + 已暴露 UI + 就是不改行为」比僵尸参数更隐蔽**。修法是引入 `quotaCriticalRate := 0.95; if quotaDegrade > quotaCriticalRate { quotaCriticalRate = quotaDegrade }`，取两者较大值：默认 0.9 < 0.95 时行为与写死逐档一致（零回归），调高后两档同时抬高。测试 `wecom_quota_degrade_tiers_test.go` 用 4 组参数值 × 9 个 `quotaRate` 采样点逐档钉住。

**种子的 Min/Max 一律贴平台硬上限**：QQ 2000、TG 4096、TG inline 100 行 × 8 钮都是平台自己的限制，填超了平台直接拒收。`TestChannelBotSeedsStayWithinPlatformCaps` 反向钉死「种子默认值与 `Max` 都等于平台上限」。

**本轮新踩的坑（前几批的 seam 测试可能也埋着）**：`SetXxxProvider(nil)` 是**空操作，不是复位**（`if fn != nil` 守卫）。在 `core`/`qq`/`telegram` 三个新测试里写 `t.Cleanup(func(){ SetXxxProvider(nil) })` 导致用例间串味（`TestNewBaseClientPicksUpTimeoutSeam` 读到上一个用例注入的 `90s`）。**正确做法是 `t.Cleanup` 里直接写包内 provider 变量**（`httpTimeoutProvider = nil` 等），同包测试是唯一能真正摘掉 provider 的地方。

**验证**：gofmt 空、`go build ./...`、`go vet ./...` 全过；`go test ./internal/app/`（ok 38s）、`./internal/channelbot/...`（4 包全 ok）、`./internal/bridge/`（ok 9.5s）、service 相关用例全绿。门禁 **`wired 120 / 已声明未接线 14 / UNDECLARED 0 / STALE 0`，rc=0，种子 134 条**。行为级测试：`splitQQMessage`/`splitMessage` 真按配置值分段、`buildInlineKeyboard` 真按配置值截断、`NewBaseClient` 真拿到配置超时、`WithTimeout(120s)` 仍赢过注入值。

### 阶段三：P1 130 个点位 + 前端接线

- 后端 130 个按 group 打包 PR。

#### 3.1 第一批已落地（2026-10-10）：9 条

**本批范围**（不是把 130 条一次做完，而是先取"有真实生产消费点 + 语义清楚"的一批）：

| group.key | 默认 | 代码点位 | 接线方式 |
| --- | --- | --- | --- |
| `confidence.humanize_boundary_low` | 0.70 | `internal/service/humanize/service.go` `DefaultBoundaryLow` | provider seam |
| `confidence.humanize_boundary_high` | 0.85 | 同文件 `DefaultBoundaryHigh` | provider seam |
| `confidence.humanize_sample_rate` | 0.10 | 同文件 `DefaultSampleRate` | provider seam |
| `confidence.humanize_max_retry` | 3 | 同文件 `DefaultMaxRetry` | provider seam |
| `sales.audience_default_limit` | 200 | `internal/service/audience_selector.go` `DefaultAudienceLimit` | provider seam |
| `sales.audience_max_limit` | 500 | 同文件 `MaxAudienceLimit` | provider seam |
| `misc.edit_lock_ttl` | 300 | `internal/service/customer_service_plus.go` `EditLockTTL` | provider seam |
| `memory.l1_ttl_hours` | 86400 | `internal/service/memory_system.go` `L1TTLHours` | provider seam |
| `geo.default_visibility_days` | 30 | `internal/geo/service/visibility.go` 原写死 `q.Days = 30` | **直连**，不造 seam |

**三条判断，写在这里免得下轮重新论证一遍**：

1. **humanize 四条与 `DefaultThreshold` 是同一套判据**。阈值定"谁算不达标"、边界定
   "哪些算边缘样本值得送 LLM"、采样率定"其中送多少"、重试定"重写几轮"。阈值早在
   阶段 1.2 接上了，剩下三条散着没人读参数中心，于是"改一个旋钮只动了四分之一"。
2. **`geo` 走直连而不是 seam**。`internal/geo/service` 早已 import `internal/service`
   （`llm.go`、`keyword_pipeline.go`），没有环可成；再造一层 provider 只是多一层转发。
   判据统一为「**读取点在不在 `internal/service` 包内**」：在则 seam，不在则直连。
3. **`q.Days <= 0 || q.Days > 365` 那一个条件原本兼任两件事**——"没给"和"给超了"共用
   同一个兜底 30。拆开后：没给走参数中心，给超了仍夹 365（查询成本闸，一次要回 365 行
   按引擎的聚合，不该跟着默认值一起被放大）。

**三个 float 参数的合法区间并不相同，必须逐条声明**（本轮写错过一次，把 0 当坏值）：

| 参数 | 合法区间 | 0 的语义 |
| --- | --- | --- |
| `humanize_boundary_low` | `[0,1)` | 0 = "所有样本都算边缘"，**合法** |
| `humanize_boundary_high` | `(0,1]` | 0 会让"边缘区间"这个概念消失，**非法，回落** |
| `humanize_sample_rate` | `[0,1]` | 0 = "一个都不送 LLM"，**合法** |

**顺带修掉 master 上一个既有红灯**：`config_param_test.go` 的 `TestDefaultParamDefsCount`
只把 `DefaultValue == ""` 一律判坏，而 PR#39 引入的 `lead.industry_profile`（自由文本行业
画像）合法地没有默认值——HEAD 上这条用例本来就是红的。改成"只对 `value_type != string`
要求有可解析默认值"，并把空默认的豁免面收成一条**具名清单**（`TestDefaultParamDefsEmptyDefaultRatchet`），
而不是"凡是 string 都放行"——否则这道门对自由文本的放行会变成又一个改了没人读的入口。

**仍未修的 master 红灯（他的文件，未动）**：`TestD12_NoNewLegacyKVDirectQuery` 在纯净 HEAD
上同样红，报的是 `internal/aiagent/llm/embedding_global_config.go` 直查 `system_config_kv`。
那是他自己的 D12 在途工作（`agent_llm.provider_failover_policy` / `embedding.global_override`），
往白名单里加一条就等于替一个真实违规盖章，等他本人定夺。

**门禁**：`wired 131 → 133`、`已声明未接线 14`、`UNDECLARED 0`、`STALE 0`，rc=0。
种子条目 138 → 147。**`internal/service` 全包从"2 红"变成"0 红"**：除上面那条
`TestDefaultParamDefsCount` 外，纯净 HEAD 上的 `TestD12_NoNewLegacyKVDirectQuery` 也红了
（报他自己的 `embedding_global_config.go` 直查 `system_config_kv`），已随他的 D12 收尾自行转绿。
- **前端**：15 个真阈值走一个 `GET /api/manage/config-params/public?group=frontend_ws` 之类的
  轻量只读端点（**不要把 `/api/manage/config-params` 原样暴露给终端用户**，那是管理端全量列表），
  前端在 Pinia 里建一个 `useRuntimeConfig()` 一次性拉取并缓存。
- 顺手把前端 1752 处自造默认分页/时间窗统一到 `pagination` group（分批，先改 `src/api/*.js` 的默认值，再改 `views/**` 的 `pageSize||20` 兜底）。

### 阶段四：字典表 + i18n（单独立项，不与阈值混做）

- 44 个 Go 字典 + `user-web/src/constants/*.js` 24 个字典**合并为一套 DB 字典**。
- 文案缺口（Go 9013 + web 7205 条 `API_MESSAGE`）按域批量补 i18n 词条，
  后端 `internal/pkg/i18n` 从 265 条扩到覆盖高频业务消息。

### 阶段五：把字典/文案从"清点"变成"进不来"

见 §6 门禁。

---

## 5. 前端读取配置的最小契约

```
GET  /api/manage/config-params              # 管理端全量（现有，admin）
PUT  /api/manage/config-params/:group/:key  # 改值（现有，admin）
POST /api/manage/config-params/:group/:key/reset
GET  /api/manage/config-params/audit-logs
```

新增（阶段三）：

```
GET  /api/config/runtime?groups=frontend_ws,frontend_ui,pagination
→ {"code":0,"data":{"frontend_ws":{"ping_interval_ms":"25000", ...}}, "message":"ok"}
```

要点：
- 只暴露 `ValueType in (int,float,bool,duration)` 且 `ReadOnly=false` 的项，**只出 value，不出
  description/source_ref**（避免把内部路径泄露给终端用户）。
- 前端拿到的值一律是**字符串**，与 `param_value` 列类型一致；类型转换在前端做，
  避免后端为两种客户端出两种 schema。
- 前端对未知 key 有 fallback 常量（现状），所以**新增参数不需要发版前端即可生效**。

---

## 6. 门禁清单（防回退，这是方案能不能活下来的关键）

沿用 `check-config-param-readpoints.py` 的「登记 ≠ 接线，靠机器守住」思路，扩成 6 道：

| # | 脚本（已有/新增） | 判据 | rc |
| --- | --- | --- | --- |
| G1 | `check-config-param-readpoints.py`（**改造**） | 每条 def 要么 `Wiring` 非空且函数存在，要么 Name 带「未接线」 | 1 |
| G2 | 同上 | 带「未接线」标注但已有 `Wiring` ⇒ 声明烂掉 | 1 |
| G3 | `check-config-param-defaults.py`（**新增**） | 每个 `DefaultValue` 必须在代码里至少有一处 fallback 常量且**值相等**（防"库里改了默认、代码兜底还是老值"） | 1 |
| G4 | `check-hardcode-tunable.py`（**新增**） | 跑 `hardcode_classify.py`，TUNABLE 且 `Owner` 已有归属的点位不得出现在新增代码里（**只卡新增，不卡存量**，存量靠阶段一二三四消化） | 1 |
| G5 | `check-config-param-keystyle.py`（**新增**） | key 命名规范 + 全局唯一 + group 必须在种子 group 白名单内 | 1 |
| G6 | `check-i18n-coverage.py`（**新增**，阶段四） | 域内中文文案必须走词条；按域设覆盖率基线，只许涨不许跌 | 1 |

全部接进 `make audit`。CI 触发 paths 必须包含各脚本 + `config_param_seeds.go`
（否则改判据不触这道门——这是现有 `check-config-param-readpoints.py` 注释里明确记过的教训）。

**G4 是整套方案的关键**：只卡新增不卡存量，才能让"新增硬编码"在 review 前就被挡住，
而存量靠阶段一二三四慢慢消化。一上来就卡全量会让所有 PR 全红，实际结果一定是大家学会绕过门禁。

---

## 7. 风险与回滚

| 风险 | 概率 | 影响 | 缓解 |
| --- | --- | --- | --- |
| 参数被改坏导致线上事故（尤其锁 TTL、熔断阈值） | 中 | 高 | ① 所有"正确性类"参数（TTL/锁/熔断）配 `Min/Max`；② P0/P1 逐条 PR 不打包；③ 每 PR 附"改值→观察→改回"证据；④ `config_param_audit_logs` 可回滚到 old |
| 60s 缓存导致"改了不生效"的二次误导 | **高** | 中 | 接线 PR 的验收必须显式验证"改库 → ≤60s 生效"；必要时对 P0 参数单独缩短 TTL |
| 参数中心变成"什么都往里塞"的垃圾桶 | **高** | 中 | G5 强制 key 规范与 group 白名单；§1 非目标写死；DATA 类强制走字典表 |
| 启动期 seed 撞唯一索引被静默丢弃 | 中 | 高 | 阶段 0.3 修索引；G5 校验唯一性；`SeedConfigParams` 的 `logger.Warnf` 升级为返回 error 让启动失败 |
| 前端无 `t()` 兜底导致白屏 | 低 | 中 | 前端所有读配置处保留原常量 fallback；新增 `useRuntimeConfig()` 失败即回落常量 |
| 72 条僵尸接线改动面过大，一次 PR 难 review | **高** | 中 | 按 group 拆 8 个 PR（见 §7 阶段一分组），每个 PR ≤ 15 个点位 |

---

## 8. 验收标准（方案生效的判据）

1. `python3 scripts/check-config-param-readpoints.py` → `未接线且已声明=0`（当前 72）。
2. `python3 scripts/hardcode_inventory.py --summary` → TUNABLE 唯一点位数从 **346 → 0**
   （前端真阈值 15 → 0）。
3. `make audit` 六道门全绿，且 G4 只卡新增。
4. **人工验收清单（每次发版前抽 3 条）**：
   - 改 `inbox.lock_ttl` → 60s 内收件箱锁行为变化
   - 改 `allow_insecure_webhook` → 60s 内新 webhook 开始/停止验签
   - 改 `pagination.page_max_size` → 前端列表页实际每页条数变化
5. 参数中心页面上**不存在**任何标着「（未接线）」的条目（全部接线或下架）。

---

## 9. 与已有门禁/规范的关系

- 复用 `scripts/check-config-param-readpoints.py` 的纪律与注释风格（它已把"读点口径看不见什么"
  写进文件头——本方案新增的 G3/G4 同样必须写清自己的盲区）。
- 五层架构不受影响：`ParamRegistry` 是 Service 层的东西，
  `dictionaries` 走标准 `Model → Repository → Service → Controller → Router`。
- 不引入新框架：不加 ORM 之外的依赖，不改 `config_params` 已有列语义，
  新增列全部可空并给默认值，**旧数据零迁移风险**。
