# JEV v1.1（统一调度/新鲜度/熔断）Implementation Plan

> **For agentic workers:** REQUIRED: Execute in current session (no subagents available). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** JEV 脱离 BROWSER_JEV_* 直连孤岛，走统一 LLM Dispatcher，并补齐新鲜度与熔断。

**Architecture:** 新增 `llm.ScenarioJevChoice` + 默认路由；`JevClient.Choose` 改调 `DispatchStructured`（prompt 文本化 choice + 本地 validateChoice 五项）；`planRound` 返回 outcome 结构（brain/jev token 分账、jevAttempted/OK）；`executeBrain` 持 session 级 JEV 熔断与独立预算；过期 choice 弃用记 `jev_stale`。

**Tech Stack:** Go, 现有 llm Dispatcher, GORM（browser_llm_plans kind=jev_choice）。

---

## Chunk 1：dispatcher 新增 jev_choice scenario

**Files:**
- Modify: `user-server/internal/aiagent/llm/dispatcher.go:18-32`（+1 const）
- Modify: `user-server/internal/aiagent/llm/dispatcher_register.go:115-127,205-218`（两处路由表各 +1 行）
- Create test: `user-server/internal/aiagent/llm/dispatcher_jevchoice_test.go`

- [ ] Step 1: 加单测（先红）
```go
func TestJevChoiceRouteRegistered(t *testing.T) {
	d := NewDispatcher(NewLLMService())
	r := d.GetRoute(ScenarioJevChoice)
	if r == nil { t.Fatal("jev_choice 路由缺失") }
	if r.MaxLatency != 5000 { t.Fatalf("MaxLatency=%d want 5000", r.MaxLatency) }
}
```
- [ ] Step 2: 运行，确认 FAIL（scenario 未定义）。
Run: `go test ./internal/aiagent/llm/ -run TestJevChoiceRouteRegistered -count=1`
Expected: FAIL
- [ ] Step 3: 加 const + 两处路由行（default 路由：deepseek→qwen-turbo，MaxLatency 5000，MinQuality 0.7；localFirst 同口径 prim→fallback）。
- [ ] Step 4: 运行，PASS。Run: 同上。Expected: PASS。
- [ ] Step 5: Commit（chunk1）。

## Chunk 2：jev.go  transport 迁移到 dispatcher

**Files:**
- Modify: `user-server/internal/browser_automation/service/jev.go`（JevClient 结构体/Choose/构造；删 HTTP 直调；+prompt 构造；+recordJevPlan 调用）
- Modify: `user-server/internal/browser_automation/service/brain.go`（+recordJevPlan 方法，kind=jev_choice）
- Modify: `user-server/internal/browser_automation/service/jev_test.go`（fake dispatcher 重写 HTTP fake 相关用例；纯函数用例不动）

- [ ] Step 1: 写 fake dispatcher（先红）
```go
type fakeJevDispatcher struct {
	content string
	usage   llm.TokenUsage
	err     error
	calls   int
}
func (f *fakeJevDispatcher) DispatchStructured(ctx context.Context, req llm.DispatchRequest, schema any) (*llm.DispatchResult, error) {
	f.calls++
	if f.err != nil { return nil, f.err }
	if err := json.Unmarshal([]byte(f.content), schema); err != nil { return nil, err }
	return &llm.DispatchResult{Provider: "fake", Model: "fake-m", Usage: f.usage}, nil
}
```
用例：CLICK 成功产单步且 tokens=usage 合计且不调 Brain；非法回退；BLOCKED/DONE；默认关闭零行为。
- [ ] Step 2: 运行新用例，FAIL（接口不存在）。
Run: `go test ./internal/browser_automation/service/ -run 'TestPlanRoundJev' -count=1`
Expected: FAIL
- [ ] Step 3: 实现：
  - `type jevDispatcher interface { DispatchStructured(ctx, llm.DispatchRequest, any) (*llm.DispatchResult, error) }`（*llm.Dispatcher 原生满足）。
  - JevClient{cfg, dispatcher}；NewJevClientFromEnv 用 GlobalDispatcher + env 覆盖（endpoint/key/model 有值 → AddProvider("jev_env_override")+SetRoute(jev_choice→它)；endpoint 须 OpenAI-compatible，注释写明）。
  - Ready() = Enabled && dispatcher != nil（key 不再是必需，本地网关免 key；无 env 时 Enabled=false 保持关闭）。
  - Choose：5s ctx 超时 + 至多 1 次重试（isRetryableLLMError）；prompt=goal + `<page_snapshot>`隔离的 state 文本 + questions 准则；schema struct{Operation, ClickTarget jevChoiceRecord}；validateChoice 两项；token 由 result.Usage 实计返回。
  - NewJevClientWithConfig(cfg, dispatcher jevDispatcher)（nil→Global）。
- [ ] Step 4: 重写 jev_test.go 中 4 处 planRound+fakeServer 用例为 fake dispatcher 版；删 jevFakeServer（如无他用）；纯函数用例不动。
- [ ] Step 5: 运行全 service 包，PASS。
Run: `go test ./internal/browser_automation/service/ -count=1`
Expected: PASS
- [ ] Step 6: Commit（chunk2）。

## Chunk 3：planRound 新鲜度 + 熔断 + 分账

**Files:**
- Modify: `user-server/internal/browser_automation/service/jev.go`（planRound 签名→planOutcome；新鲜度比对；jev_stale 帧）
- Modify: `user-server/internal/browser_automation/service/executor.go:785-793`（调用方适配 + session 级 jevFails/jevOff/jevTokens + 独立预算）
- Modify: `user-server/internal/browser_automation/service/jev_test.go`（outcome 断言 + stale 回退 + 熔断用例）

```go
type planOutcome struct {
	stepsJSON            []byte
	done                 bool
	terminal             string
	brainTokens, jevTokens int
	jevAttempted, jevOK  bool
}
type jevSessionState struct { off bool; fails int; tokensUsed int }
const ( jevMaxSessionFails = 3; jevSessionTokenBudget = 20000 )
```

- [ ] Step 1: 用例先红：stale（hand 注入可控 snapshot 的 fake？planRound 经 e.hand.snapshot 重拍——测试构造 Executor{hand: stubHand}；查 Hand 结构是否可 stub，不可则 hand==nil 跳过新鲜度，stale 用例改为 dispatcher 层单测 + 集成断言 jev_stale 分支可达）。
  现实简化：`e.hand==nil` 时跳过新鲜度（测试/单测路径），生产 hand 常驻。stale 用例：Executor 配 hand stub（若 Hand 为 struct 则包内可直接造 Hand{...}? 需看 Hand 定义；若复杂，stale 分支用可导出的纯函数 `isJevStale(old, new string) bool` 覆盖 + planRound 传 hand==nil 跳过）。
- [ ] Step 2: 实现 planRound 新签名 + 新鲜度 + outcome；executor.go 调用方适配（ brainTokens 进 tokenUsed；jevTokens 进独立预算，超限 off；!jevOK&&jevAttempted 累 fails，≥3 off；off 后 planRound 直接 Brain）。
- [ ] Step 3: 运行 service 包 PASS；`go vet ./internal/browser_automation/...` 零输出。
- [ ] Step 4: Commit（chunk3）。

## Chunk 4：门禁 + 落盘 + 推送

- [ ] Step 1: `go vet ./internal/browser_automation/...` 零输出；`gofmt -l` 干净。
- [ ] Step 2: `go test ./internal/browser_automation/... -count=1` 全绿（需 `POSTGRES_TEST_PORT=8232` + `POSTGRES_TEST_PASSWORD=<.env 的 POSTGRES_PASSWORD>`，如仓库惯例；DB 相关用例无库必红属环境问题非代码问题）。
- [ ] Step 2b: `go test ./internal/aiagent/llm/ -count=1` 全绿（同上测试库环境；含 chunk1 新增的两个 jev_choice 路由用例）。
- [ ] Step 3: 设计文档 §6 验收口径补一行（v1.1 口径），commit + `git fetch` 双远端 + 直推 `gitee-upstream`/`upstream` master。

**与实际实现的偏差（评审结论 accepted）：**
- Chunk 2 计划写"validateChoice 两项"——实际沿用 ultrafast 五项校验（CLICK 无候选拒绝等），以实现为准，计划不改代码。
- Chunk 3 计划写 planRound 老签名假设——实际磁盘签名为 `(ctx,task,session,snap,pageURL,st,history,seq)(stepsJSON,done,terminal,planTokens,err)`，已按实际签名改 `planOutcome` 版。
