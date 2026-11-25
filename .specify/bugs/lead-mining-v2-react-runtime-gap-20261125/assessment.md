# Bug Assessment: Lead-mining v2 ReAct runtime is installed but not operational

- **Slug**: `lead-mining-v2-react-runtime-gap-20261125`
- **Created**: 2026-11-25
- **HEAD**: `2c5c53e3ded742ac0c6ebb1944529aaed18973fd`
- **Source**: `docs/未完成任务清单.md` §3.30 `LM-20261125-02`，并核对 `LM-20261125-07` 的配置执行要求
- **Verdict**: valid
- **Severity**: high
- **Production availability**: **ReAct evidence path is not production-usable**; the legacy three-way detection + structured judge remains available as fallback.

## Executive Verdict

| Sub-agent claim | Verdict | Evidence |
|---|---|---|
| Hook is installed but resolver is empty | **成立** | `router/router.go:819-827` 在生产装配点调用 `InstallLeadReactDispatcher(LeadReactResolver{})`；`lead_react.go:37-61` 的三个工具在 resolver 函数为 nil 时统一返回“ReAct 取证工具未装配” |
| Hook ignores `LeadJudgeContext` | **成立** | `SetLeadReactHook` 的闭包收到 `jctx` 后只做 nil 检查，随后调用 `runLeadReactLoop(ctx, tools, dispatch)`，没有把 `jctx` 传下去（`lead_react.go:90-98`） |
| ReAct starts from a fixed prompt with no history/scope | **成立** | `runLeadReactLoop` 的起始 `messages` 只有一句通用协议提示（`lead_react.go:136`），不含当前消息、历史、规则、账号、租户、会话或群信息 |
| NoFC and native ToolCall adaptation conflict | **成立，且有两条具体失败路径** | Dispatcher 对 NoFC 会把文本 Action 适配成 `ToolCalls` 并把 `Content` 改成 Thought；生产 hook 只返回 `Content`，随后可能被解析成 Final Answer。原生 FC 返回 `ToolCalls` 时 `Content` 可能为空，hook 丢弃 `ToolCalls` 后直接解析失败 |
| Existing tests use fake dispatcher and miss production path | **成立** | `lead_react_test.go` 直接向 `runLeadReactLoop` 注入文本 dispatch，绕过 `InstallLeadReactDispatcher` 与真实 Dispatcher/ReActAdapter；未发现 `InstallLeadReactDispatcher`、`SetLeadReactHook` 或 `JudgeWithContext` 的测试 |

补充确认：`LeadJudgeContext` 的 `ReactEvidence/ReactDegraded` 只进入最终判定 prompt，没有合并进 `LeadJudgement.Degraded`，也没有进入判定记录的 `EvidenceComplete/Degraded`。这属于 LM-03 的结果契约缺口，但会放大本 lane 的生产可见性问题。

## Actual Call Chain

1. `cmd/api/main.go:367` 初始化全局 LLM dispatcher；`main.go:805` 调用 `router.Setup`。
2. `router/router.go:819-827` 创建 `LeadMiningService`，随后执行：
   ```go
   service.InstallLeadReactDispatcher(service.LeadReactResolver{})
   ```
   因全局 dispatcher 已初始化，`InstallLeadReactDispatcher` 会实际安装 hook，而不是走 nil 关闭分支。
3. 入站消息持久化后，`inbox_ingress_persist.go:154-160` 调 `LeadDiscoveryService.Process`。
4. `lead_discovery.go:29-37` 的 `Enabled` 只检查渠道账号状态与 `LeadMiningAgentID != nil && >0`；**不读取绑定的 `AIAgent` 配置**。
5. `Process` 最终调用 `LeadMiningService.Enqueue`；worker 调 `process`（`lead_mining.go:334-439`）。
6. `process` 固定租户、解析规则/闸门、读取历史，然后 `buildLeadJudgeContext` 组装当前消息、共享上下文、群角色和三路检测证据（`lead_judge_context.go:256-364`）。
7. 生产 judge 是 `dispatcherJudge`，实现 `JudgeWithContext`；`process` 通过可选接口调用富上下文路径（`lead_mining.go:409-415`）。
8. `JudgeWithContext` 先调全局 `leadReactHook(ctx, jctx)`（`lead_judge_context.go:123-133`）。
9. hook 丢弃 `jctx` 的业务内容，调 `runLeadReactLoop`；循环从固定 user prompt 开始，每轮向 production dispatch 传三个工具定义。
10. production dispatch 使用 `ScenarioLowCost`、`MaxTokens=512`、`Temperature=0.2`，且不传 `AgentID`、persona、system prompt、provider/model 配置（`lead_react.go:110-122`）。
11. 循环解析文本 Action、执行白名单工具、把 Observation 以 `role=user` 回灌；最多 3 轮。
12. 回到 `JudgeWithContext` 后，ReAct 证据/降级标记写入 `jctx`，最终 structured judge 使用 `ScenarioLowCost`、固定 `1024/0.2`，系统提示由 `buildLeadJudgeSystemPrompt(jctx)` 生成。
13. `recordLeadMiningJudgement` 只把三路 Hits 与 `jd.Degraded` 写入记录；`ReactDegraded` 未参与机器状态。

## Detailed Evidence

### 1. Production resolver is empty

- 唯一生产调用点：`router/router.go:826`。
- 注释明确写着“resolver 暂不接具体仓储”，并期望工具返回未装配错误后走三路主链。
- `LeadReactResolver` 只有三个函数字段；没有任何生产实现：
  - `Customer360`
  - `HistoricalLeads`
  - `Dictionary`
- `defaultLeadReactTools` 对 nil 函数返回固定错误 `ReAct 取证工具未装配`。

因此，即使模型正确输出 Action，生产工具也不会读取任何真实数据。

### 2. `LeadJudgeContext` is ignored

`SetLeadReactHook` 的闭包：

```go
leadReactHook = func(ctx context.Context, jctx *LeadJudgeContext) ([]string, bool) {
    if jctx == nil {
        return nil, true
    }
    collected, degraded, err := runLeadReactLoop(ctx, tools, dispatch)
    ...
}
```

`jctx` 除 nil 外完全未使用。`runLeadReactLoop` 的签名也没有上下文参数。

这直接违反 LM-02 的“输入当前消息/历史证据与冻结配置”。

### 3. Fixed prompt has no task, history, or scope

`runLeadReactLoop` 只初始化：

```go
messages := []llm.ChatMessage{{
    Role: "user",
    Content: "如需取证请按 ReAct 协议输出 Action；证据充分请输出 Final Answer。",
}}
```

缺失：

- 当前消息与发送人；
- 双向/群聊历史；
- 规则描述、关键词、语义关键词；
- tenant/platform/account/conversation/group；
- 已收集的三路检测证据；
- 绑定 Agent 的 persona/system prompt；
- 明确的取证任务与允许范围。

模型只能根据一句通用协议提示“自由发挥”，无法进行任务相关取证。

### 4. NoFC / native ToolCall conflict

#### 4.1 NoFC provider path

`dispatcher_dispatch.go:195-205`：

- `provider.NoFC=true && len(req.Tools)>0` 时进入 ReAct mode；
- 把工具描述包装进 system prompt；
- 清空原生 `config.Tools`。

`react_adapter.go:147-170`：

- 文本含 Action 时，`AdaptResult` 设置 `result.ToolCalls`、`FinishReason="tool_calls"`；
- 如果解析到 Thought，则把 `result.Content` 改成 **Thought 文本**。

但 `InstallLeadReactDispatcher` 只返回 `result.Content`，丢弃 `ToolCalls`。随后 `runLeadReactLoop` 对这个 Thought 文本调 `ParseReActResponse`。该解析器对无协议文本会降级为 `IsFinal=true`（`react_adapter.go:71-75`，测试 `react_adapter_test.go:59-74` 明确固定该行为）。

结果：模型输出标准 `Thought + Action` 时，生产 hook 可能把 Thought 当 Final Answer，**不执行工具、不收集证据、也不标记 degraded**。

#### 4.2 Native FC provider path

- `provider.NoFC=false` 时保留原生 `Tools`。
- 模型可能返回 `tool_calls` 且 `Content` 为空。
- hook 丢弃 `ToolCalls` 只返回空 `Content`。
- `ParseReActResponse("")` 返回错误，循环直接 degraded，原生工具调用不会执行。

#### 4.3 Observation role conflict

`BuildObservationMessage` 固定返回 `role=user`（`react_adapter.go:178-188`）。这对 NoFC 文本 ReAct 可用，但对原生 FC 应使用 `role=tool` 并关联 assistant `tool_calls`。当前实现没有区分协议。

### 5. Tool argument parsing is also broken

`lead_react.go:154`：

```go
args := strings.Trim(parsed.ActionInput, `"{} `)
```

工具 schema 声明 JSON 对象 `{"query":"..."}`，但这里用 `Trim` 删除首尾字符，而不是 `json.Unmarshal`。例如 `{"query":"u1"}` 会变成类似 `query":"u1` 的损坏字符串。现有 fake resolver 忽略 args，所以测试没有发现。

此外，args 中没有可信 scope；resolver 只收到 `ctx + string`，无法从模型输入安全派生 tenant/account/conversation 边界。

### 6. Bound Agent configuration does not enter execution

- `ChannelAccount.LeadMiningAgentID` 只在 `LeadDiscoveryService.Enabled` 中作为布尔开关使用。
- `AIAgent` 已存在 `Persona/SystemPrompt/LLMModel/LLMProviderConfig/Temperature/MaxTokens/Status/Version` 等字段（`model/ai_agent.go:35-94`）。
- `AIAgentService.LoadContextForRun` 已存在权威配置加载 seam（`service/ai_agent.go:236-294`）。
- 但 `buildLeadJudgeContext`、`JudgeWithContext`、ReAct dispatch 均未调用它。
- `LeadMiningConfig.Model` 也只被 DTO 读写，判定链无消费点。
- 所有 LLM 请求固定 `ScenarioLowCost`，不设置 `DispatchRequest.AgentID`，导致平台计量也无法归因到线索 Agent。

因此“改模型/提示词可观察、停用生效、同次分析冻结版本”当前不成立。

### 7. Existing tests bypass the production runtime

- `lead_react_test.go:12-62` 直接调用 `runLeadReactLoop`，注入返回预写 ReAct 文本的 `dispatch` func。
- 该 fake 不经过 `Dispatcher.callProvider`、NoFC 分支、`ReActAdapter.AdaptResult` 或 native `ToolCalls`。
- `lead_mining_test.go:225-233` 的 `fakeJudge` 只实现旧 `Judge`，所以 `process` 不会调用 `JudgeWithContext`，也不会触发 ReAct hook。
- `lead_judge_context_test.go` 只测消息合并与 prompt 拼接。
- 全仓未找到 `InstallLeadReactDispatcher`、`SetLeadReactHook`、`JudgeWithContext` 的测试调用。

现有 GREEN 只能证明“假 dispatch 的文本循环”可用，不能证明生产 ReAct 链路可用。

## Reproduction Conditions

### Static reproduction (already established)

1. 正常 API 启动，全局 dispatcher 非 nil，且 `bridgeIngressSvc != nil`。
2. 某渠道账号 `status=1` 且 `lead_mining_agent_id>0`。
3. 入站消息通过规则闸门并取到非空历史。
4. `dispatcherJudge.JudgeWithContext` 调用已安装 hook。
5. 观察第一轮 ReAct dispatch 请求：只有固定 user prompt；三个 resolver 全部 nil。

### Runtime reproduction to run in a future approved test stage

No DB/build/test was run in this assessment. The following are deterministic test fixtures, not claimed runtime results:

- **Empty resolver**：让 fake provider 返回 `Action: query_historical_leads`；生产路径应观察到工具返回“未装配”，无法产生真实历史证据。
- **NoFC Action loss**：`ProviderConfig.NoFC=true`，fake OpenAI-compatible server 返回 `Thought: ...\nAction: query_historical_leads\nAction Input: {"query":"u1"}`；当前生产 hook 预期拿不到 ToolCall，循环可能提前 Final Answer。
- **Native FC loss**：`NoFC=false`，server 返回 `choices[0].message.tool_calls` 且 `content` 为空；当前 hook 预期解析失败并 degraded。
- **Context omission**：向 hook 传带当前消息/规则/账号/历史的 `LeadJudgeContext`，捕获 dispatch 请求；当前请求不含这些字段。
- **Config omission**：账号绑定启用状态且 persona/model 唯一的 Agent；捕获 ReAct 与最终 judge 请求；当前 `AgentID` 为空、prompt/model 未按 Agent 配置变化。
- **Argument corruption**：Action Input 为合法 JSON；在 resolver 记录收到的 args；当前不是 `u1`。

## Production Availability Judgment

- **Legacy lead mining main chain**: available. Rule gate, history fetch, three-way detection, structured judge, persistence and fallback still exist.
- **ReAct evidence chain**: **not production-usable**.
  - No real resolver is wired.
  - No task/history/scope reaches the loop.
  - NoFC and native FC responses are not safely adapted.
  - Tool arguments are corrupted.
  - Agent configuration is ignored.
  - Failure may be silent rather than fail-visible.
- The router comment’s intended behavior—“工具未装配 → 明确降级 → 走三路主链”—only holds when the model happens to emit a parseable Action and the loop reaches the tool. It does not make ReAct itself operational.

## Scope

### In scope

- `LM-20261125-02`: ReAct receives current task and wires real authorized read-only resolvers.
- Runtime consumption of `lead_mining_agent_id` configuration needed to make ReAct/judge use the bound Agent.
- Protocol adaptation between NoFC text ReAct and native ToolCall.
- Tests that exercise the production dispatcher/adapter path.

### Adjacent but not to be silently absorbed

- `LM-20261125-01`: history/debounce keys must include account. The current `ListRecentByConversation` and `ListRecentByGroup` indeed lack account predicates; ReAct must consume the corrected scope contract, not invent another history path.
- `LM-20261125-03`: `ReactDegraded` must flow into machine-validity and downstream side effects. This lane should expose a typed ReAct outcome, but candidate/notification/opportunity gating remains LM-03.
- `LM-20261125-07`: full effective-config precedence and snapshot policy. This lane needs a minimal frozen Agent runtime snapshot; the broader rule/legacy/config precedence belongs to LM-07.

## Five-Layer Minimal Fix Surface

### Router

- Replace `LeadReactResolver{}` with a Service-owned production constructor, e.g. `service.NewLeadReactResolver(...)`.
- Router only assembles dependencies; no SQL/business logic.
- Do not make Router call repositories directly.

### Service

- `lead_react.go`:
  - Pass `*LeadJudgeContext` (or a narrower immutable `LeadReactTask`) into the loop.
  - Build task-specific system/user messages from current message, bounded history, rule, hits and trusted scope.
  - Change dispatch seam to return a normalized result containing `Content`, `ToolCalls`, `FinishReason` and protocol mode; never reduce to `Content` only.
  - Parse native ToolCalls first; use text ReAct only for NoFC.
  - JSON-decode Action Input and extract `query`; reject unknown/scope-changing fields.
  - Use `role=tool` + tool_call_id for native FC; use Observation user message for NoFC.
  - Add context timeout and preserve collected observations/degraded reason.
- `lead_judge_context.go` / `lead_mining.go`:
  - Resolve and freeze bound Agent runtime once per analysis: AgentID, version, persona/system prompt, provider/model, temperature/max tokens, status.
  - Pass trusted scope (tenant, platform, canonical account, conversation/group/sender) separately from model-controlled args.
  - Record a typed ReAct outcome and make at least `ReactDegraded` visible to the judge result contract.
- New Service resolver implementation:
  - `query_customer_360`: resolve identity only inside the source account/business scope, then return a minimal authorized 360 summary.
  - `query_historical_leads`: query judgements by tenant + platform + account + conversation/group/sender.
  - `query_dictionary`: read the shared business dictionary; no tenant data leakage.

### Repository

- Add only the missing scoped read methods:
  - account-aware recent conversation/group history (or consume LM-01’s corrected methods);
  - scoped historical judgement lookup by account + conversation/group/sender;
  - account-scoped customer identity lookup if existing `FindByIdentityAll` cannot safely enforce source-account boundary.
- Reuse existing dictionary and customer repositories where their contracts are sufficient.
- No write/outbound tool in the ReAct whitelist.

### Model

- No schema change is required for the core runtime fix.
- Reuse `AIAgent`, `ChannelAccount`, `LeadMiningJudgement`, `ConversationContext`, `Dictionary`.
- If machine-readable ReAct status must be persisted, prefer existing `ContextJSON`/`Degraded` fields first; schema expansion belongs to LM-03.

### Controller

- No Controller change is required for this runtime bug.
- Do not add a new public endpoint merely to test wiring.

## TDD Plan

All DB tests must use `run-user-db-check-cwd.sh`, `-p 1 -parallel 1`, and assert 0 SKIP. No real outbound/UI.

### Stage A — production-path RED

1. `TestInstallLeadReactDispatcher_EmptyResolverVisible`
   - Use a test dispatcher/provider; install empty resolver.
   - Force Action; assert a typed unavailable/degraded outcome.
   - Current code may silently return Final Answer on NoFC Thought output.
2. `TestLeadReactHook_UsesLeadJudgeContext`
   - Inject a capturing dispatch through `SetLeadReactHook`.
   - Assert first request contains current message, bounded history, rule and trusted scope.
3. `TestRunLeadReactLoop_NoFCActionNotLost`
   - Use real `llm.Dispatcher` with `NoFC=true` and a local fake HTTP server returning standard Thought/Action.
   - Assert resolver runs once and observation is fed back.
4. `TestRunLeadReactLoop_NativeToolCallExecuted`
   - Use real `llm.Dispatcher` with `NoFC=false`; fake server returns native `tool_calls`.
   - Assert tool execution and `role=tool` observation.
5. `TestRunLeadReactLoop_JSONActionInput`
   - Return `{"query":"u1"}`; assert resolver receives exactly `u1`, and extra `tenant_id/account_id` fields are rejected or ignored.
6. `TestLeadMiningAgentConfig_EntersReActAndJudge`
   - Seed enabled bound Agent with unique persona/system prompt/model/AgentID.
   - Capture both ReAct and final judge requests; assert frozen config is consumed and `DispatchRequest.AgentID` is set.
7. `TestLeadReactOutcome_PropagatesDegraded`
   - Resolver fails; assert final judgement/record machine state is degraded/incomplete, not only prompt text.

### Stage B — resolver and scope RED/GREEN

8. `TestLeadReactResolver_Customer360Scoped`
   - Same identity exists under account A and B; A task must not read B.
9. `TestLeadReactResolver_HistoricalLeadsScoped`
   - Same conversation/group/sender IDs under two accounts; only source-account judgements returned.
10. `TestLeadReactResolver_CrossTenantDenied`
    - Missing/wrong tenant or revoked scope returns explicit denial and no rows.
11. `TestLeadReactResolver_DictionaryReadOnly`
    - Valid term returns dictionary evidence; unknown term is explicit; no write path exists.
12. `TestLeadReactLoop_TimeoutAndRoundCap`
    - Slow provider and repeated Actions stop deterministically with degraded status and partial observations.

### Stage C — regression

- Keep existing text-loop whitelist/failure tests, but route them through the normalized protocol layer.
- Add a `dispatcherJudge` test proving `JudgeWithContext` invokes ReAct before structured judgement.
- Run affected Service/Repository packages; classify unrelated full-service failures separately.

## Risks & Considerations

- Do not let model-provided args choose tenant/account/conversation. Scope must come from trusted `LeadJudgeContext`.
- Do not treat all resolver failures as fatal: LM-02/LM-03 require required/optional evidence policy. Start with fail-visible degraded, then let LM-03 define business validity.
- Avoid a second LLM client; reuse global dispatcher and its provider failover/audit.
- Avoid storing hidden chain-of-thought as acceptance evidence; store tool name, normalized query, observation reference, status and reason.
- The global hook is process-wide. It must remain stateless with respect to tenant/account; per-call task/scope must be passed explicitly.
- Fixing NoFC/native adaptation in shared `react_adapter.go` can affect other agents; prefer a normalized wrapper in lead-mining first, with shared-adapter changes only if regression tests prove necessary.
- Provider/model override is not currently represented in `DispatchRequest`; adding it is a cross-cutting dispatcher change. The minimal safe path is to resolve the bound Agent to an existing dispatcher provider/model or add a narrowly validated override seam, never pass raw API keys through task context.

## Open Questions

- Should `query_customer_360` return the full existing 360 DTO or a minimal whitelisted summary? Full DTO may include cross-account sessions; minimal summary is safer for the first slice.
- Should ReAct run for every message or only after local three-way signals/ambiguity? This is a cost/policy decision for LM-02/LM-03, not required to prove the runtime bug.
- Where should typed ReAct status be persisted? Existing judgement `Degraded/ContextJSON` may suffice initially; schema change should wait for LM-03.

## Assessment Limits

- This is a static source-path assessment. No PostgreSQL, build, test, UI, real outbound, deploy, commit or push was performed.
- HEAD was checked repeatedly and remained `2c5c53e3ded742ac0c6ebb1944529aaed18973fd`.
- Runtime reproduction is intentionally deferred to an approved fix/test stage.

## 10. Main Adjudication — Approved Runtime Slice

Verdict: valid/high. Approve a focused runtime/protocol/resolver slice; do not merge AIAgent persona/provider/model injection into this lane.

Approved decisions:

1. **Text-only ReAct**: the lead-mining service owns the system prompt and text protocol. Do not populate `DispatchRequest.Tools`; do not depend on native `tool_calls`, shared `ReActAdapter`, or `NoFC` system-prompt rewriting. The dispatcher seam may return the full `*llm.DispatchResult`, but this lane executes only `Content` using strict `Action`/`Final Answer` parsing. Both `NoFC=true` and `NoFC=false` production-dispatcher tests must pass with `Tools` omitted.
2. **Trusted bounded task**: extend `LeadJudgeContext` with `ConversationID`, `SenderID`, and a typed bounded `LeadReactTask` (or equivalent immutable view) containing current message, bounded recent messages, rule/hits, platform/account/channel-account/conversation/group/sender. Tenant remains solely `context.Context` + `tenant.OnlyTenant`. Build prompts with explicit length/count bounds and deterministic truncation.
3. **Strict args**: replace `strings.Trim` with `json.Unmarshal` into a query-only struct. Reject malformed JSON and reject/ignore any model-supplied tenant/account/conversation/sender fields; resolver scope always comes from the trusted task.
4. **Real resolvers only**:
   - `query_historical_leads`: add a repository method scoped by tenant + platform + account + conversation/group/sender, with a bounded limit and optional query text match. Same IDs under another account/tenant must never be returned.
   - `query_customer_360`: return a minimal whitelisted account-scoped session summary only (identity, session count/status counts, last-seen and bounded recent session metadata). Add an explicit repository query filtered by platform + account + user/sender before applying limit. Do not call the broad `Customer360Service.GetCustomer360` and do not expose messages, orders, clues or other-account data.
   - `query_dictionary`: remove from the whitelist in this slice. Existing seeds contain only workflow status dictionaries and there is no safe lead-term search source. Record it as explicitly unsupported rather than returning fabricated evidence.
5. **Wiring**: Router installs the service-owned production resolver constructor only when its required read dependencies are complete. Empty/missing resolver must be visible as unavailable/degraded, not silently run with fabricated tools. No new global mutable per-tenant state.
6. **Degraded persistence**: typed ReAct outcome (evidence references, status/reason, degraded) must flow from hook → structured judge → persisted judgement. Degraded ReAct sets `Degraded=true` and `EvidenceComplete=false`; do not store hidden chain-of-thought.
7. **Separate later lane**: bound `lead_mining_agent_id` persona/SystemPrompt/provider/model/version injection remains deferred per requirements. This lane does not add provider/model overrides or controller/schema/UI/outbound changes.

Required TDD before implementation:

- Production dispatcher with `NoFC=true` and `NoFC=false`, fake HTTP provider, `Tools` omitted: Action is parsed, resolver runs, observation is returned, Final Answer is used.
- First prompt contains current message, bounded history/rule/scope; oversized history is truncated deterministically.
- Strict JSON query; malformed JSON, scope override fields, unknown tool and >3 rounds/timeout all yield visible degraded outcomes.
- Real PostgreSQL: historical leads same conversation/group/sender under accounts A/B and tenants A/B return only the trusted source; customer360 minimal summary never reads/exposes B.
- Empty resolver/no dependency and resolver DB errors are fail-visible; no false successful evidence.
- `dispatcherJudge` invokes ReAct before structured judgement; persisted record reflects ReactDegraded/EvidenceComplete.
- Legacy fallback/structured-judge regressions remain green.

Allowed scope: `service/lead_react.go`, `service/dispatcher_judge.go`, `service/lead_mining.go` (record propagation), new service resolver/task files, `repository/lead_mining_judgement.go`, `repository/customer_session.go`, Router resolver construction, and focused tests. No shared `llm/react_adapter.go`, no schema/migration, no controller/UI, no Agent config injection. Use the existing bug-lane assessment → fix → test reports, RED before production changes, serial PostgreSQL, and stop on any HEAD/external-file drift.
