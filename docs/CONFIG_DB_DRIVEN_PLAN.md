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

| # | 动作 | 验收 |
| --- | --- | --- |
| 0.1 | 对齐 `config_param_test.go` 的 `defaultParamDefsWant = 114` 与脚本实测 115 | `go test ./internal/service/ -run TestSeedConfigParams` 绿 |
| 0.2 | 修 `validateValue` 的 `duration` 分支：`ParseDuration` 失败不应 `return nil`，应按"秒数 float"解析并继续校验（与 `GetDuration` 的读取口径对齐） | 新增用例：非法 duration + 设了 min/max → 拒绝写入 |
| 0.3 | 修索引漂移：要么把 gorm tag 改成单列 `uniqueIndex` 与实际一致，要么写 migration 把它改成 `(param_group,key)` 复合。**倾向后者**（复合索引更符合设计意图，且 key 命名不用再带 group 前缀） | migration 幂等可重跑；`TestDefaultParamDefsKeysGloballyUnique` 按新口径改写 |
| 0.4 | 修 `NewSystemConfigKVRepository()` nil DB panic → 改 nil-safe 或全量改用 `WithDB` 装配 | `TestFallbackNilDB` 覆盖 |
| 0.5 | 修 `MaxUploadSizeMB` 无人读的实证 bug：`upload.go` 改读 `system_config.MaxUploadSizeMB`（保留 `MaxUploadSize` 常量为兜底） | 上传 11MB 文件在配置调大后成功；API 验证通过 |

> 0.5 单独列，因为它是**已暴露给运维却完全不生效**的配置，比 72 条僵尸更值得优先修。

### 阶段一：72 条僵尸参数接线（投入产出比最高）

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

### 阶段二：P0 新增 58 个点位入库

按 §2.1 四类分四个 PR，每 PR 一个类：

| PR | 内容 | 关键点 |
| --- | --- | --- |
| 2a | 上传/body 上限（`upload_max_size_mb`、`max_json_body_mb`） | **合并三份副本**为 1 键；`system_config.MaxUploadSizeMB` 保留为兼容列，读新键优先 |
| 2b | Webhook 验签降级开关（`allow_insecure_webhook` 单键，覆盖 14 个硬编码点） | **默认值改为 `false`**；此项属安全默认值修正，需单独发版说明 |
| 2c | 锁与幂等 TTL（收件箱/消息中台/人工接管/TG 轮询锁，13 个点位） | TTL 是正确性不是调优；必须带 `Restart=false`（要能热改） |
| 2d | 渠道与配额上限（QQ/TG 长度、SSE 每 IP 连接、配额降级阈值，15 个点位） | — |

- 每条种子的 `Min/Max` 必填（`config_params` 已支持），避免运维填出 `max_tokens=999999`。
- `ValueType` 用 `duration` 的，`Step` 填合理步长（如 `30s`）。
- **验收**：`scripts/api_verify_full.py` 跑通 + 每个 PR 附"改值→行为变化→改回"三步证据。

### 阶段三：P1 130 个点位 + 前端接线

- 后端 130 个按 group 打包 PR。
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