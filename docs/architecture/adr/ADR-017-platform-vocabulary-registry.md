# ADR-017 统一平台词汇表与渠道注册表

- **状态**：Proposed（本轮仅落地设计；实施改动面大，按 2026-10-11 指示排期在后）
- **决策日期**：2026-10-11
- **影响范围**：渠道集成（reach/inbox）、MCP 工具面、配置参数、管理端 UI、API 文档
- **设计来源**：借自 catbus（`C:\myproject\catbus`）的 `src/core/vocab.ts` + `src/core/registry.ts` + `src/core/capabilities.ts` 模式（一份通用词汇表 + 每渠道声明式注册，帮助/能力矩阵/校验全部由注册表生成）

---

## 1. 背景：渠道逻辑的"每渠道一套代码"现状

同一业务动作（发消息、收消息、撤回、查状态）在每个渠道里都是独立实现，且散落在多处：

| 重复点 | 证据 |
|---|---|
| 渠道分发 switch | `internal/app/integration_reach_adapter.go` 三段巨型 switch（telegram/feishu/wecom/whatsapp，L568-575 / L698-781 / L831-870）；`未完成任务.md` §1（Recall 逆向召回）还要再加一轮 |
| 凭证模型 | 六种并存的渠道凭证口径：MCP 凭证对（mcp_guard）、单值 MCP_TOKEN、桥接 token、ChatChannel app_key+secret、ExternalKBConnector 类型注册表、渠道内嵌 key |
| 出参形状 | 每渠道各自拼 JSON，错误措辞/字段名不一致，管理端每个渠道写一套展示 |
| 文档/UI/校验 | 渠道能力（支持撤回吗？限流多少？）没有机器可读的单一事实源，前端硬编码 |

新增一个渠道 = 改 N 处；审计/限流/文案逐渠道补课，遗漏面大。

## 2. 决策

引入**统一词汇表（vocabulary）+ 声明式渠道注册表（registry）**：渠道不再写"分发代码"，只声明"我支持词汇表里的哪些动作、有什么限制"；分发、校验、能力矩阵、文档、UI 表单全部由注册表派生。

> **已有一层**：`internal/capregistry`（7c618867）已实现"渠道×能力"的声明式注册表 + 黄金文件测试（docs/capabilities.md）。本 ADR 不另起炉灶，而是在它之上补齐**动作词汇层**（capregistry 现在只有"有没有"的粒度，缺"动作/参数/出参/危险语义"的粒度）。

### 2.1 词汇表（`internal/platform/vocab`，并入 capregistry 亦可）

全平台共享的动作词汇 + 标准参数/出参/错误语义（Go 结构体，编译期即校验）：

```go
// 通用动作词汇（首批，按业务动词命名，渠道无关）
//   message.send / message.recall / message.list
//   customer.search / customer.get / customer.update
//   kb.search / kb.add_doc
// 每个动作声明：标准参数、出参类型、是否危险（confirm 语义）、是否分页/流式
type Action struct {
    Name       string            // "message.send"
    Params     []ParamSpec       // 标准参数（渠道可声明不支持的子集）
    Output     OutputType        // 统一出参 envelope 的 data 形状
    Danger     bool              // 外发/删除类：调用方必须显式确认，审计单独标记
    Paged      bool
}
```

### 2.2 渠道注册表（`internal/platform/registry`）

每个渠道一份声明（数据，不是代码路径）：

```go
type ChannelDecl struct {
    ID        string            // "telegram"
    Aliases   []string
    Supports  map[string]SupportLevel // "message.send": Full / Partial / None
    Unsupported []string        // 标准参数里本渠道不支持的（如 whatsapp 不支持 recall）
    Limits    LimitSpec         // 限流/单条长度/召回窗口（TG 48h、飞书·企微 24h、WA 无召回——直接来自 未完成任务.md §1）
    Adapter   ChannelAdapter    // 唯一必须写的代码：参数→渠道 API 的映射
}
```

`resolveRegistry()` 在启动期校验每个声明（未知动作、参数既声明支持又在 Unsupported、限流缺失等），错误 fail-fast——与 catbus `resolveCommands` 同思路（启动即暴露，不留到运行期）。

### 2.3 派生面（一次声明，多处生成）

- **分发**：`integration_reach_adapter.go` 的 switch 收敛为 `registry.Dispatch(action, channel, params)`，渠道差异全部在 Adapter 内
- **能力矩阵**：`docs/capabilities.md` 与 `/api/capabilities` 由注册表生成 + **一致性测试**（文件过期即红，借 catbus gen:capabilities + doc 测试）
- **管理端 UI**：渠道配置表单、能力标签由注册表接口渲染，不再逐渠道硬编码
- **错误码**：统一 `code/message/hint` 信封（借 catbus errors.ts）：`UNSUPPORTED` 必须列出该渠道**实际支持**的动作集，`AUTH_EXPIRED` 与 `AUTH_REQUIRED` 分开（与 MCP guard 2026-10-11 的失败原因分级同口径）

### 2.4 凭证口径统一

注册表加 `CredentialKind` 枚举，把六种并存口径逐步归一到两种形状：
- `pair`（client_id + key，1:N 钥匙，复用 2026-10-11 的 mcp_credential_keys 机制）
- `single`（轮换槽位 token，沿用 MCP_TOKEN/MCP_TOKEN_PREV 语义）

## 3. 迁移计划（分四步，每步独立可交付）

1. **词汇表+注册表骨架**：在 `internal/capregistry` 里加动作维度（Action/Vocab 声明），先把 telegram/feishu/wecom/whatsapp 四渠道的**能力声明**填进去（不改任何运行路径），黄金文件同步重生成
2. **reach 分发收敛**：`integration_reach_adapter.go` 三段 switch 改走 registry.Dispatch（行为等价，回归=既有触达测试全绿）
3. **Recall 接入**：`未完成任务.md` §1 的渠道召回直接以 ChannelDecl.Limits 实现（第一个"注册表新能力"案例，验证模式成立）
4. **凭证归一 + UI/文档派生**：CredentialKind 落地，管理端渠道页改读注册表

## 4. 备选方案

- **维持现状**：每渠道一套代码。成本=每次加能力都要 N 处改动 + 审计/文档漂移；Recall 再来一轮即不可持续
- **接口抽取（Go interface + 各渠道实现）**：只解决分发一处，能力声明/文档/UI 派生仍各自为政；注册表方案是其超集

## 5. 后果

- 新渠道成本从"改 N 处"降到"一份声明 + 一个 Adapter"
- 能力矩阵/文档/校验由单一事实源生成，漂移由测试兜住
- 一次性迁移成本集中在第 2 步（switch 收敛，行为等价可回归验证）
- 与 MCP 鉴权体系（1:N 钥匙、状态机、审计）天然对接：渠道凭证只是 `pair` 形状的又一个使用者
