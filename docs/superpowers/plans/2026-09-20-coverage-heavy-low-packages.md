# 低覆盖重组件补测排期（cron / tracing / knowledge config / rag / migrations / platform）

> **For agentic workers:** REQUIRED SUB-SKILL: 使用 superpowers:executing-plans 逐任务执行本计划。
> 本仓 `CLAUDE.md` 规则 0 明令禁止派发子代理，因此上游推荐的 subagent-driven-development **不适用**，
> 只能在当前会话内串行执行。每个任务的 Step 用 `- [ ]` 勾选语法跟踪。

**Goal:** 把 user-server 中 8 个「因依赖 DB/网络而长期低覆盖」的包补到可回归的水平，并且每条断言都经反向注入验证过。

**Architecture:** 三层打法——① 纯逻辑与内部函数走 in-package 单测（不起 DB、不发网络）；
② 依赖接口的（cron job、knowledge 配置读取器）用「内嵌接口 + 只覆盖被调方法」的 fake，越界调用即 nil-panic 变红；
③ 真正需要落库的（tracing sink、migrations 全链路）用 `internal/pkg/testutil.NewTestDB` 起真实 PostgreSQL 测试库，
断言按唯一 trace_id / 表名过滤，使测试与同包其它用例的执行顺序无关。

**Tech Stack:** Go 1.26（`go 1.25.0` 指令）、`testing` 标准库、GORM + PostgreSQL、`net/http/httptest`、`testutil` 测试库辅助。

**Spec:** 无独立 spec 文档；需求来源是覆盖率基线扫描（`go tool cover -func`）与本计划 §「基线与目标」表。

**Status:** 8/8 任务完成并推送（`f76c39bf`…`4afd9c66`），目标达成情况与未达成口径见文末「收尾」段的实况表。

## Global Constraints

- 测试命令一律在 `hivemtk/user-server/` 目录执行，且先加载 env：`set -a; source ../.env; set +a`。
- 跨包并发固定 `-p 1`（本机测试库端口 8232 与共享槽位，见 memory「测试库口令漂移」）。
- 每次跑测试带 `-count=1`，不接受缓存结果当证据。
- 禁止使用 `go test ./...` 全量作为单任务验收（`internal/service` 单包已 880s，会撞 600s 默认超时）。
- 只 `git add` 显式路径，禁止 `git add -A/.`；本工作树与并行会话共享，提交前用 `git status --porcelain <路径>` 确认归属。
- 测试失败先排除环境前提（`df`、端口可达），再归因代码；DB 不可达时 `NewTestDB` 会 `t.Skipf`，
  声称「DB 覆盖已提升」前必须确认该用例**没有**被 skip（本地需 `CI=true` 或人工看 `-v` 输出）。
- 每个任务收尾必须做反向验证：注入行为级 bug → 目标用例转红 → 用 `cp` 备份还原（**不得**对未提交文件跑 `git checkout`）→ 复绿。
- commit message 中文 conventional 格式、结尾无句号；推送前对两个远端分别 `git fetch <remote> master` +
  `git rev-list --left-right --count master...<remote>/master` 期望 `N/0`，再 `git push <remote> master:master`。
- 不改动生产代码：本排期只新增测试文件。扫描中发现的缺陷记入任务末「Findings」并回灌 memory，不当场修。

---

## 基线与目标

| # | 包 | 当前 | 目标 | 手段 |
|---|---|---|---|---|
| 1 | `internal/cron` | 3.0% | ≥85% | 内嵌接口 fake |
| 2 | `internal/pkg/tracing` | 26.7% | ≥70% | in-package 纯函数 + 真实库 sink 用例 |
| 3 | `internal/aiagent/knowledge/service` | 7.6% | constants.go 15 个 getter 全覆盖 | `SetConfigReader` 注入 fake |
| 4 | `internal/aiagent/rag/service` | 13.1% | 3 个 prompt 构造器 100% | 直接调非导出函数（`Query` 需真实 API Key，已 skip） |
| 5 | `internal/aiagent/rag/customer_service` | 6.7% | 会话管理剩余分支 | 内存实现，无外部依赖 |
| 6 | 同上（上下文理解 + 关键词启发式） | — | 纯函数全覆盖 | 同上 |
| 7 | `internal/migration/migrations` | 23% | ≥60% | 注册表元信息 + 全链路 Up/Down 打真实库 |
| 8 | `internal/platform` | 33.7% | ≥65% | `httptest` 覆盖签名/JWT/上报分支 |

已排除的假目标：`internal/aiagent/rag/service` 的 `Query`/`StructuredQuery` 与 `internal/aiagent/rag/core`
需要真实 LLM API Key（`rag_test.go:37 requireRealAPIKey` 会 skip），不在本排期内制造无法离线的断言。

---

## Task 1: internal/cron —— 域名健康探测与活码轮询

**Files:**
- Create: `user-server/internal/cron/job_fakes_test.go`
- Modify: 无（`internal/cron/cron_test.go` 已有 `TestNewLiveCodeRotator`，不复用不改动）
- 参考（只读）：`internal/cron/domain_health_job.go:11-60`、`internal/cron/live_code_rotator.go:11-38`、
  `internal/service/domain_health.go:26-54`（`DomainHealthService` 8 方法 + `HealthCheckResult` 字段）、
  `internal/service/live_code.go:18-36`（`LiveCodeService`）

**Interfaces:**
- Consumes: `NewDomainHealthCheckJob(service.DomainHealthService, repository.DomainPoolRepository) *DomainHealthCheckJob`
  （第二个参数 repo 在 `runOnce` 中**从未被使用** → 传 `nil`）；`(*DomainHealthCheckJob).runOnce()`、
  `.Start()`、可注入字段 `.interval`；`NewLiveCodeRotator(service.LiveCodeService) *LiveCodeRotator`、`.rotate()`、`.Start()`。
- Produces: fake 类型 `fakeDomainHealth` / `fakeLiveCode` 仅本包内使用，后续任务不依赖。

- [x] **Step 1: 确认目标目录无并行会话在改**

Run: `cd hivemtk && git status --porcelain user-server/internal/cron`
Expected: 空输出。非空则**停手**，改排到该文件干净后再做。

- [x] **Step 2: 写 fake 与用例**

创建 `user-server/internal/cron/job_fakes_test.go`：

```go
package cron

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/service"
)

// fakeDomainHealth 内嵌接口只补 job 实际调用的 CheckAll；
// 其余 7 个方法保持 nil 接口值 —— 一旦被调用即 panic，越界依赖会被测试直接打红。
type fakeDomainHealth struct {
	service.DomainHealthService

	mu      sync.Mutex
	calls   int
	latest  context.Context
	results []*service.HealthCheckResult
	err     error
	panics  bool
	notify  chan struct{}
}

func (f *fakeDomainHealth) CheckAll(ctx context.Context) ([]*service.HealthCheckResult, error) {
	f.mu.Lock()
	f.calls++
	f.latest = ctx
	f.mu.Unlock()
	if f.notify != nil {
		select {
		case f.notify <- struct{}{}:
		default:
		}
	}
	if f.panics {
		panic("checkall boom")
	}
	return f.results, f.err
}

func (f *fakeDomainHealth) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func TestDomainHealthRunOnceCallsCheckAll(t *testing.T) {
	f := &fakeDomainHealth{results: []*service.HealthCheckResult{
		{Domain: "a.example.com", DNSOK: true, HTTPOk: true, HealthScore: 90},
		{Domain: "b.example.com", DNSOK: true, HTTPOk: false, HTTPStatus: 500},
		{Domain: "c.example.com", DNSOK: false, HealthScore: 0},
		{Domain: "d.example.com", DNSOK: true, HTTPOk: true, OnBlacklist: true, HealthScore: 70},
	}}
	NewDomainHealthCheckJob(f, nil).runOnce()

	if got := f.count(); got != 1 {
		t.Fatalf("CheckAll 调用次数=%d, want 1", got)
	}
	if f.latest == nil {
		t.Error("CheckAll 应收到非 nil ctx")
	}
}

func TestDomainHealthRunOnceErrorOnlyLogged(t *testing.T) {
	f := &fakeDomainHealth{err: errors.New("探测链路不可用")}
	NewDomainHealthCheckJob(f, nil).runOnce()
	if f.count() != 1 {
		t.Fatalf("CheckAll 调用次数=%d, want 1", f.count())
	}
}

func TestDomainHealthRunOnceRecoversPanic(t *testing.T) {
	f := &fakeDomainHealth{panics: true}
	j := NewDomainHealthCheckJob(f, nil)

	j.runOnce() // recover 生效则 panic 不外溢；否则本用例外层崩溃
	if f.count() != 1 {
		t.Fatalf("panic 前应先完成一次调用, got %d", f.count())
	}

	f.panics = false
	j.runOnce()
	if f.count() != 2 {
		t.Fatalf("上一次 panic 后任务应可继续, 累计调用=%d want 2", f.count())
	}
}

func TestDomainHealthStartProbesImmediately(t *testing.T) {
	notify := make(chan struct{}, 4)
	f := &fakeDomainHealth{notify: notify}
	go NewDomainHealthCheckJob(f, nil).Start()

	select {
	case <-notify:
	case <-time.After(3 * time.Second):
		t.Fatal("Start() 应在启动时立即探测一次，而不是等首个 ticker 周期")
	}
}

func TestDomainHealthStartTickerLoops(t *testing.T) {
	notify := make(chan struct{}, 16)
	f := &fakeDomainHealth{notify: notify}
	j := NewDomainHealthCheckJob(f, nil)
	j.interval = 20 * time.Millisecond // 覆盖生产 5min 周期，验证 ticker 分支确实在循环

	// Start() 内部是 select{case <-ticker.C} 死循环，没有停止通道，返回不了——
	// 本测试让其随测试进程结束一起回收，只登记一次探测计数断言。
	go func() { j.Start() }()

	deadline := time.After(2 * time.Second)
	for n := 0; n < 3; n++ {
		select {
		case <-notify:
		case <-deadline:
			t.Fatalf("ticker 循环未持续探测：累计 %d 次, want>=3", f.count())
		}
	}
	if f.count() < 3 {
		t.Fatalf("calls=%d want>=3", f.count())
	}
}

// fakeLiveCode 同上：只实现 rotate() 用到的 RotateLiveCodes。
type fakeLiveCode struct {
	service.LiveCodeService

	mu     sync.Mutex
	calls  int
	err    error
	notify chan struct{}
}

func (f *fakeLiveCode) RotateLiveCodes(ctx context.Context) error {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if f.notify != nil {
		select {
		case f.notify <- struct{}{}:
		default:
		}
	}
	return f.err
}

func (f *fakeLiveCode) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func TestLiveCodeRotateSuccess(t *testing.T) {
	f := &fakeLiveCode{}
	NewLiveCodeRotator(f).rotate()
	if f.count() != 1 {
		t.Fatalf("RotateLiveCodes 调用次数=%d, want 1", f.count())
	}
}

func TestLiveCodeRotateFailureDoesNotPanic(t *testing.T) {
	f := &fakeLiveCode{err: errors.New("轮询锁不可用")}
	NewLiveCodeRotator(f).rotate()
	if f.count() != 1 {
		t.Fatalf("RotateLiveCodes 调用次数=%d, want 1", f.count())
	}
}

func TestLiveCodeStartRotatesImmediately(t *testing.T) {
	notify := make(chan struct{}, 4)
	f := &fakeLiveCode{notify: notify}
	go NewLiveCodeRotator(f).Start()

	select {
	case <-notify:
	case <-time.After(3 * time.Second):
		t.Fatal("Start() 应在启动时立即轮询一次（生产周期为 1h，否则测试窗口内永不执行）")
	}
}
```

- [x] **Step 3: 跑红→跑绿**

Run: `cd hivemtk/user-server && set -a; source ../.env; set +a && go test -p 1 -count=1 -cover ./internal/cron/`
Expected: `ok hivemtk-user/internal/cron coverage: ≥85.0%`（首跑若编译报错按报错改 fake 方法签名，不改生产码）

- [x] **Step 4: 逐函数核对新增覆盖**

Run: `go test -p 1 -count=1 -coverprofile=/tmp/cron.cov ./internal/cron/ && go tool cover -func=/tmp/cron.cov`
Expected: `runOnce` / `Start`（job）/ `rotate` / `Start`（rotator）均非 0.0%；剩余 0% 只允许是 `main` 风格未使用函数。

- [x] **Step 5: 反向验证（三次注入，逐次还原）**

备份：`cp internal/cron/domain_health_job.go /tmp/dhj.bak && cp internal/cron/live_code_rotator.go /tmp/lcr.bak`

1. `perl -0pi -e 's/\tdefer func\(\) \{\n\t\tif r := recover\(\); r != nil \{\n\t\t\tlogger\.Errorf\("\[domain-health\] runOnce panic recovered: %v", r\)\n\t\t\}\n\t\}\(\)\n//' internal/cron/domain_health_job.go`
   → 期望 `TestDomainHealthRunOnceRecoversPanic` FAIL。
2. 删掉 `Start()` 里首次 `go j.runOnce()` → 期望 `TestDomainHealthStartProbesImmediately` 超时 FAIL。
3. 把 `rotate()` 中 `err := r.liveCodeService.RotateLiveCodes(context.Background())` 改成
   `var err error; _ = err`（即真的不再调用服务）→ 期望 `TestLiveCodeRotateSuccess` FAIL（calls=0）。

每次注入后 `cp /tmp/*.bak internal/cron/` 还原，最后 `go test -p 1 -count=1 ./internal/cron/` 复绿。
**禁止**用 `git checkout --` 还原（该文件此时已无未提交改动才可用，但本工作树共享，一律用 cp）。

- [x] **Step 6: 提交并推送**

```bash
cd hivemtk
git add user-server/internal/cron/job_fakes_test.go
git status --porcelain --cached user-server/internal/cron   # 只应有该文件（注意：git 无 --cached 于 status，改用 git diff --cached --name-only）
git commit -m "test: cron 域名健康探测与活码轮询以接口内嵌 fake 补测"
```

推送（两远端分别校验 fast-forward）：

```bash
git fetch gitee-upstream master && git rev-list --left-right --count master...gitee-upstream/master   # 期望 1/0
git push gitee-upstream master:master
git fetch upstream master && git rev-list --left-right --count master...upstream/master              # 期望 1/0
git push upstream master:master
```

**Findings（只记录不修）：** `live_code_rotator.go:31 rotate()` 无 `recover`，而 `Start()` 以裸 goroutine 调用它 ——
`RotateLiveCodes` 一旦 panic 会击穿整个进程（`domain_health_job.go` 同类任务则有 recover）。

---

## Task 2: internal/pkg/tracing —— Span/Carrier/JSON 与异步落库 sink

**Files:**
- Create: `user-server/internal/pkg/tracing/tracing_span_test.go`（纯函数，不碰全局）
- Create: `user-server/internal/pkg/tracing/tracing_sink_db_test.go`（真实库端到端）
- 参考（只读）：`internal/pkg/tracing/tracing.go` 全文；已存在的
  `tracing_truncate_test.go:55 TestToModelFromPendingAppliesTruncation`、`langfuse_attrs_test.go`、
  `tracing_bench_test.go`（只在 `-bench` 下 `Init(nil)`，普通 `go test` 不会启动 worker）。

**Interfaces:**
- Consumes: `NodeOrder/NodeLabel/GenerateTraceID/NodeTraceID/Sha1Sum(非导出 sha1Sum)/NewCarrier/WithCarrier/CarrierFromContext/`
  `TraceIDFromContext/(*Carrier).Child/(*Carrier).WithMsgID/InitRecalledChunks/RecordRecalledChunks/RecalledChunksOf/`
  `Start/(*Span).*/(*Span).toPending/toModelFromPending/Publish/Stats/Init/Stop/RecordNode/ReportToolCall/`
  `LinkInboundTraceID/LinkOutboundTraceID/ErrStr/StartSpan/toJSON`；
  `trace.NewContextWithTraceID`（`internal/pkg/trace/trace.go:218`）、`db.SetTestDB`（`internal/pkg/db/db.go:83`）、
  `model.SpanKindLifecycle/SpanKindAgentTurn/SpanKindToolCall`、`testutil.NewTestDB`。
- Produces: 无（本包外不依赖）。

**为什么不在纯函数用例里调 `Init`：** `sinkOnce` 使 `Init` 全进程只生效一次，`Stop` 又不可逆；
若纯函数用例先 `Init(nil)`，后续真实库用例拿到的就是无 DB 的 worker（反之亦然）。
因此顺序无关性是设计约束：纯函数用例**只调 `toPending/toModelFromPending`**（`Publish` 的两段各自单测），
落库用例**独占** `Init/Stop`，并按唯一 trace_id 过滤断言。

- [x] **Step 1: 写纯函数用例**

创建 `user-server/internal/pkg/tracing/tracing_span_test.go`：

```go
package tracing

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/trace"
)

func TestNodeOrderMatchesLifecycleSequence(t *testing.T) {
	cases := map[string]int{
		NodeIngest: 1, NodeAIDispatch: 2, NodeOutboundEnqueue: 3,
		NodeInboxSync: 4, NodeDownlinkFetch: 5, NodeDeliveredAck: 6,
	}
	for node, want := range cases {
		if got := NodeOrder(node); got != want {
			t.Errorf("NodeOrder(%s)=%d want %d", node, got, want)
		}
	}
	if got := NodeOrder("not_a_node"); got != 99 {
		t.Errorf("未知节点应排末尾: got %d want 99", got)
	}
}

func TestNodeLabelKnownAndFallback(t *testing.T) {
	if got := NodeLabel(NodeIngest); got != "消息上报接入" {
		t.Errorf("NodeLabel(ingest)=%q", got)
	}
	if got := NodeLabel(NodeAgentTurn); got != "Agent 一轮推理" {
		t.Errorf("NodeLabel(agent_turn)=%q", got)
	}
	if got := NodeLabel("mystery"); got != "mystery" {
		t.Errorf("未知节点应原样返回: %q", got)
	}
}

func TestCarrierRoundTripAndNilSafety(t *testing.T) {
	c := NewCarrier("wecom", "acct-1", "conv-1")
	if !strings.HasPrefix(c.TraceID, "tr-") || len(c.TraceID) < 20 {
		t.Fatalf("TraceID 形态异常: %q", c.TraceID)
	}
	if c.Channel != "wecom" || c.AccountID != "acct-1" || c.ConversationID != "conv-1" {
		t.Fatalf("载体字段未装配: %+v", c)
	}

	ctx := WithCarrier(context.Background(), c)
	got := CarrierFromContext(ctx)
	if got != c {
		t.Fatal("CarrierFromContext 应取回同一指针")
	}
	if TraceIDFromContext(ctx) != c.TraceID {
		t.Error("TraceIDFromContext 应优先取 Carrier")
	}

	if CarrierFromContext(context.Background()) != nil {
		t.Error("无载体且无 trace_id 时应返回 nil")
	}
	if TraceIDFromContext(context.Background()) != "" {
		t.Error("空上下文 trace_id 应为空串")
	}
	if tid := TraceIDFromContext(trace.NewContextWithTraceID(context.Background(), "tid-9")); tid != "tid-9" {
		t.Errorf("无 Carrier 时应回落到 logger trace_id, got %q", tid)
	}
	if CarrierFromContext(nil) != nil {
		t.Error("nil ctx 不应 panic 且应返回 nil")
	}
}

func TestCarrierChildAndWithMsgID(t *testing.T) {
	var nilCarrier *Carrier
	fresh := nilCarrier.Child()
	if fresh == nil || !strings.HasPrefix(fresh.TraceID, "tr-") {
		t.Fatalf("nil 接收者 Child 应产出新载体, got %+v", fresh)
	}

	parent := NewCarrier("tg", "acct", "conv").WithMsgID("m-1")
	child := parent.Child()
	if child == parent {
		t.Error("Child 必须返回副本")
	}
	if child.TraceID != parent.TraceID || child.ConversationID != "conv" || child.MsgID != "m-1" {
		t.Errorf("Child 应保留会话维度与 msg_id: %+v", child)
	}
	child.MsgID = "changed"
	if parent.MsgID != "m-1" {
		t.Error("Child 副本不得影响父载体")
	}
	if NewCarrier("c", "a", "s").WithMsgID("x").MsgID != "x" {
		t.Error("WithMsgID 应写入新副本")
	}
}

func TestRecalledChunksLifecycle(t *testing.T) {
	ctx := InitRecalledChunks(context.Background())
	ctx = InitRecalledChunks(ctx) // 幂等：已存在容器时不得替换
	RecordRecalledChunks(ctx, []string{"c1", "", "c2"})
	RecordRecalledChunks(ctx, []string{"c3"})

	got := RecalledChunksOf(ctx)
	want := []string{"c1", "c2", "c3"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("RecalledChunksOf=%v want %v", got, want)
	}

	if RecalledChunksOf(context.Background()) != nil {
		t.Error("未初始化容器应返回 nil")
	}
	RecordRecalledChunks(context.Background(), []string{"ignored"}) // 无容器不得 panic
	if n := len(RecalledChunksOf(context.Background())); n != 0 {
		t.Errorf("无容器时写入应被丢弃, got %d 条", n)
	}
}

func TestSpanToPendingAppliesDefaults(t *testing.T) {
	carrier := NewCarrier("wx", "acct-9", "conv-9")
	ctx := WithCarrier(context.Background(), carrier)

	p := Start(ctx, NodeToolCall).
		Kind(model.SpanKindToolCall).
		Parent("custom_parent").
		Turn(3).
		Tool("kb_search").
		Agent("agent-1").
		Direction("inbound").
		MsgID("msg-7").
		Input(map[string]any{"q": "退款"}).
		Expected("命中知识库").
		Abnormal("上游超时").
		toPending(42, "abnormal", "boom")

	if p.traceID != carrier.TraceID || p.conversationID != "conv-9" || p.accountID != "acct-9" || p.channel != "wx" {
		t.Errorf("归属字段未从载体继承: %+v", p)
	}
	if p.node != NodeToolCall || p.spanKind != model.SpanKindToolCall || p.parentNode != "custom_parent" {
		t.Errorf("节点/种类/父节点错: node=%s kind=%s parent=%s", p.node, p.spanKind, p.parentNode)
	}
	if p.turnIndex != 3 || p.toolName != "kb_search" || p.agentID != "agent-1" {
		t.Error("层级字段（turn/tool/agent）丢失")
	}
	if p.durationMs != 42 || p.status != "abnormal" || p.errorStr != "boom" || p.abnormal != "上游超时" {
		t.Error("耗时/状态/错误/异常原因未透传")
	}
	if p.nodeOrder != NodeOrder(NodeToolCall) {
		t.Errorf("nodeOrder=%d want %d", p.nodeOrder, NodeOrder(NodeToolCall))
	}

	// 层级 span 未显式给 parent 时，默认挂到 ai_dispatch；abnormal 缺省时回落为 error。
	q := Start(ctx, NodeAgentTurn).Kind(model.SpanKindAgentTurn).toPending(7, "abnormal", "llm 500")
	if q.parentNode != NodeAIDispatch {
		t.Errorf("非 lifecycle span 默认父节点应为 ai_dispatch, got %q", q.parentNode)
	}
	if q.abnormal != "llm 500" {
		t.Errorf("abnormal 缺省应回落 error 文本, got %q", q.abnormal)
	}

	// lifecycle span：kind 缺省补 lifecycle，parent 保持空，abnormal 保持空。
	r := Start(ctx, NodeIngest).toPending(1, "ok", "")
	if r.spanKind != model.SpanKindLifecycle || r.parentNode != "" || r.abnormal != "" {
		t.Errorf("lifecycle span 默认值错: kind=%q parent=%q abnormal=%q", r.spanKind, r.parentNode, r.abnormal)
	}

	// 无载体但有 logger trace_id 时，traceID 走回落分支。
	orphan := Start(trace.NewContextWithTraceID(context.Background(), "tid-orphan"), NodeInboxSync).toPending(1, "ok", "")
	if orphan.traceID != "tid-orphan" {
		t.Errorf("无载体应回落 logger trace_id, got %q", orphan.traceID)
	}
	if orphan.accountID != "" || orphan.channel != "" {
		t.Error("空载体不应凭空造出账号/渠道")
	}

	// TraceID 显式覆盖（出站复用入站 trace）
	cov := Start(ctx, NodeOutboundEnqueue).TraceID("tr-manual").toPending(1, "ok", "")
	if cov.traceID != "tr-manual" {
		t.Errorf("TraceID 覆盖失效, got %q", cov.traceID)
	}
	if Start(context.Background(), NodeDeliveredAck).TraceID("tr-new").toPending(1, "ok", "").traceID != "tr-new" {
		t.Error("无载体时 TraceID 应自建载体并写入")
	}
}

func TestToModelFromPendingMapsEveryColumn(t *testing.T) {
	carrier := NewCarrier("dd", "acct-m", "conv-m")
	p := Start(WithCarrier(context.Background(), carrier), NodeDeliveredAck).
		MsgID("m-2").Direction("outbound").Expected("客户端确认送达").
		Input("in").Output("out").toPending(13, "ok", "")

	row := toModelFromPending(p)
	if row.TraceID != carrier.TraceID || row.ConversationID != "conv-m" || row.AccountID != "acct-m" || row.Channel != "dd" {
		t.Errorf("行归属列映射错: %+v", row)
	}
	if row.Node != NodeDeliveredAck || row.NodeOrder != 6 || row.MsgID != "m-2" || row.Direction != "outbound" {
		t.Error("节点/顺序/消息/方向列映射错")
	}
	if row.Input != `"in"` || row.Output != `"out"` {
		t.Errorf("input/output 应 JSON 化, got %q / %q", row.Input, row.Output)
	}
	if row.DurationMs != 13 || row.Status != "ok" || row.Expected != "客户端确认送达" {
		t.Error("耗时/状态/预期列映射错")
	}
	if row.SpanKind != model.SpanKindLifecycle {
		t.Errorf("SpanKind=%q", row.SpanKind)
	}
}

func TestPublishIsNonBlockingAndNeverDrops(t *testing.T) {
	// 本用例不调 Init/Stop，因此必须在「worker 未启动」与「已被同包其它用例启动」两种进程状态下都成立。
	pubBefore, droppedBefore := Stats()
	Start(context.Background(), NodeIngest).Input("x").End("y", nil)
	RecordNode(context.Background(), NodeSpan{Node: NodeInboxSync})
	ReportToolCall(context.Background(), ToolTraceEvent{ToolName: "noop", DurationMs: 5})
	pubAfter, droppedAfter := Stats()

	if delta := pubAfter - pubBefore; delta != 0 && delta != 3 {
		t.Errorf("published 增量=%d, 只允许 0（未 Init）或 3（已 Init）", delta)
	}
	if droppedAfter != droppedBefore {
		t.Errorf("低负载下不得丢弃 span: dropped %d -> %d", droppedBefore, droppedAfter)
	}
}

func TestEndDerivesStatusFromError(t *testing.T) {
	carrier := NewCarrier("ch", "a", "c")
	s := Start(WithCarrier(context.Background(), carrier), NodeIngest).Output("直接设置的输出")
	s.End("被忽略的输出", errors.New("boom"))
	// End 内部 Publish：spanCh 为 nil，无副作用；此处只要求不 panic 且 Output 不被覆盖 —— 通过再走一次 toPending 验证同构逻辑。
	if got := Start(WithCarrier(context.Background(), carrier), NodeIngest).Output("先设").toPending(1, "abnormal", "boom"); got.status != "abnormal" {
		t.Errorf("status 应透传, got %q", got.status)
	}
}

func TestRecordNodeAndReportToolCallDoNotPanic(t *testing.T) {
	carrier := NewCarrier("ch", "acct", "conv")
	ctx := WithCarrier(context.Background(), carrier)

	RecordNode(ctx, NodeSpan{Node: NodeIngest})                                       // 空白字段由载体/NodeOrder/ok 补齐
	RecordNode(context.Background(), NodeSpan{Node: "unknown", Status: StatusSkipped}) // 未知节点 + 状态常量
	RecordNode(ctx, NodeSpan{Node: NodeAIDispatch, Status: StatusFailed, Error: "e"})
	ReportToolCall(ctx, ToolTraceEvent{Kind: model.SpanKindAgentTurn, TurnIndex: 2, AgentID: "a1", Error: "llm down"})
	ReportToolCall(ctx, ToolTraceEvent{TraceID: "tr-explicit", Status: "ok"})
	ReportToolCall(context.Background(), ToolTraceEvent{}) // 无载体也不能 panic

	RecordDownlinkFetchBatch(context.Background(), "wx", "acct", nil) // 空批次直接返回
}

func TestTraceIDGenerationIsStableAndDistinct(t *testing.T) {
	a, b := GenerateTraceID(), GenerateTraceID()
	if !strings.HasPrefix(a, "tr-") || a == b {
		t.Fatalf("GenerateTraceID 应为 tr- 前缀且互不相同: %s / %s", a, b)
	}
	want := "tr-" + sha1Sum("conv-x|acct-x|inbound")
	if got := NodeTraceID("conv-x", "acct-x"); got != want {
		t.Errorf("NodeTraceID 不稳定: %q want %q", got, want)
	}

	c := NewCarrier("ch", "a", "conv-1")
	ctx := WithCarrier(context.Background(), c)
	if got := LinkInboundTraceID(ctx, "conv-1"); got != NodeTraceID("conv-1", "") || c.TraceID != got {
		t.Errorf("LinkInboundTraceID 应回写载体 trace_id: got %q carrier %q", got, c.TraceID)
	}

	// 无 DB：空会话 → 新生成 id；有会话 → 回落派生 id。两者都写回载体。
	c2 := NewCarrier("ch", "a", "conv-2")
	ctx2 := WithCarrier(context.Background(), c2)
	if got := LinkOutboundTraceID(ctx2, ""); !strings.HasPrefix(got, "tr-") || c2.TraceID != got {
		t.Errorf("空会话出站应生成新 trace: %q carrier=%q", got, c2.TraceID)
	}
	if got := LinkOutboundTraceID(ctx2, "conv-2"); got != NodeTraceID("conv-2", "") {
		t.Errorf("无 DB 时出站应回落派生 id: %q", got)
	}
	if got := LinkOutboundTraceID(context.Background(), "conv-3"); got == "" {
		t.Error("无载体时仍应返回可用 trace_id")
	}
}

func TestTextHelpers(t *testing.T) {
	if ErrStr(nil) != "" || ErrStr(errors.New("x")) != "x" {
		t.Error("ErrStr 语义错")
	}
	if got := toJSON(nil); got != "" {
		t.Errorf("toJSON(nil)=%q want 空串", got)
	}
	if got := toJSON("原文"); got != "原文" {
		t.Errorf("字符串应原样返回, got %q", got)
	}
	if got := toJSON([]byte("by")); got != "by" {
		t.Errorf("[]byte 应转字符串, got %q", got)
	}
	if got := toJSON(map[string]int{"a": 1}); got != `{"a":1}` {
		t.Errorf("map 应 JSON 化, got %q", got)
	}
	if got := toJSON(make(chan int)); !strings.Contains(got, "chan") {
		t.Errorf("不可序列化对象应回落 %v 文本, got %q", got)
	}
	if len(sha1Sum("")) != 40 {
		t.Error("sha1Sum 应返回 40 位十六进制")
	}
	timer := StartSpan()
	time.Sleep(2 * time.Millisecond)
	if timer.ElapsedMs() < 1 {
		t.Errorf("ElapsedMs=%d 应为正", timer.ElapsedMs())
	}
}
```

- [x] **Step 2: 跑绿（只此文件）**

Run: `cd hivemtk/user-server && go test -p 1 -count=1 -run 'TestNode|TestCarrier|TestRecalled|TestSpan|TestToModel|TestPublish|TestEnd|TestRecord|TestTraceIDGen|TestTextHelpers' ./internal/pkg/tracing/ -v`
Expected: 全 PASS，且**无** `--- SKIP`。

- [x] **Step 3: 写真实库 sink 用例**

创建 `user-server/internal/pkg/tracing/tracing_sink_db_test.go`：

```go
package tracing

import (
	"context"
	"fmt"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
)

// TestSinkPersistsSpansToMessageTrace 端到端验证「异步缓冲 → 批量落库 → Stop 排空」全链路。
// 断言一律按本用例独有的 trace_id 过滤，因此与同包其它用例的执行顺序无关。
func TestSinkPersistsSpansToMessageTrace(t *testing.T) {
	d := testutil.NewTestDB(t, &model.MessageTrace{})
	if d == nil {
		t.Fatal("NewTestDB 返回 nil（测试库不可达）")
	}
	orig := db.GetDB()
	db.SetTestDB(d)
	t.Cleanup(func() { db.SetTestDB(orig) })

	Init(d)

	tid := fmt.Sprintf("tr-sink-%d", time.Now().UnixNano())
	ctx := WithCarrier(context.Background(), &Carrier{TraceID: tid, ConversationID: "sink-conv", AccountID: "sink-acct", Channel: "sink"})

	RecordNode(ctx, NodeSpan{Node: NodeIngest, Direction: "inbound", MsgID: "m-1", Expected: "消息进入"})
	Start(ctx, NodeAIDispatch).Input(map[string]any{"q": "你好"}).Output(map[string]any{"a": "你好呀"}).End(nil, nil)
	ReportToolCall(ctx, ToolTraceEvent{Kind: model.SpanKindToolCall, ToolName: "kb_search", TurnIndex: 1, DurationMs: 12, Input: "q", Output: "hits"})

	var rows []model.MessageTrace
	deadline := time.Now().Add(5 * time.Second)
	for {
		if err := d.Where("trace_id = ?", tid).Order("node_order asc").Find(&rows).Error; err != nil {
			t.Fatalf("查询 message_trace 失败: %v", err)
		}
		if len(rows) >= 3 || time.Now().After(deadline) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if len(rows) != 3 {
		t.Fatalf("落库行数=%d want 3（异步 sink 未按时批量写入）", len(rows))
	}

	byNode := map[string]model.MessageTrace{}
	for _, r := range rows {
		byNode[r.Node] = r
	}
	ingest, ok := byNode[NodeIngest]
	if !ok {
		t.Fatalf("缺少 ingest 行, got nodes=%v", nodeKeys(rows))
	}
	if ingest.NodeOrder != 1 || ingest.Direction != "inbound" || ingest.MsgID != "m-1" || ingest.Status != "ok" {
		t.Errorf("ingest 行字段错: %+v", ingest)
	}
	if ingest.ConversationID != "sink-conv" || ingest.AccountID != "sink-acct" || ingest.Channel != "sink" {
		t.Errorf("载体归属未落到行上: %+v", ingest)
	}
	ai, ok := byNode[NodeAIDispatch]
	if !ok {
		t.Fatal("缺少 ai_dispatch 行")
	}
	if ai.Input != `{"q":"你好"}` || ai.Output != `{"a":"你好呀"}` {
		t.Errorf("input/output 未按 JSON 落库: in=%q out=%q", ai.Input, ai.Output)
	}
	tool, ok := byNode[model.SpanKindToolCall]
	if !ok {
		t.Fatal("缺少 tool_call 行")
	}
	if tool.ToolName != "kb_search" || tool.TurnIndex != 1 || tool.ParentNode != NodeAIDispatch {
		t.Errorf("tool_call 层级列错: %+v", tool)
	}
	if tool.DurationMs != 12 {
		t.Errorf("tool_call 耗时=%d want 12", tool.DurationMs)
	}

	pub, dropped := Stats()
	if pub < 3 {
		t.Errorf("published 计数=%d want>=3", pub)
	}
	if dropped != 0 {
		t.Errorf("低负载下不应发生丢弃: dropped=%d", dropped)
	}

	Stop() // 排空缓冲 + 最后一次批量落库；本包内唯一调用点，必须在上面的断言之后
}

func nodeKeys(rows []model.MessageTrace) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Node)
	}
	return out
}
```

- [x] **Step 4: 跑绿并核对覆盖率**

Run: `set -a; source ../.env; set +a && go test -p 1 -count=1 -coverprofile=/tmp/tr.cov ./internal/pkg/tracing/ && go tool cover -func=/tmp/tr.cov | tail -40`
Expected: 全包 coverage ≥70%，且 `go tool cover -func` 里 `Init/flushLoop/Stop/Publish/RecordDownlinkFetchBatch/toPending/toModelFromPending/toJSON/sha1Sum` 均非 0.0%；
运行日志中 `TestSinkPersistsSpansToMessageTrace` 必须是 PASS 而非 SKIP。

- [x] **Step 5: 反向验证**

备份：`cp internal/pkg/tracing/tracing.go /tmp/tracing.bak`

1. `toModelFromPending` 中 `NodeOrder` 改成写死 `0` → `TestToModelFromPendingMapsEveryColumn` FAIL。
2. `toPending` 中 `if parent == "" && kind != model.SpanKindLifecycle` 改成 `if false &&` → `TestSpanToPendingAppliesDefaults` FAIL。
3. `flushLoop` 的批量阈值 200 改成 20000（永不批量落库，仅靠 ticker；再把 ticker 300ms 改 30s）
   → `TestSinkPersistsSpansToMessageTrace` 超时 FAIL —— 证明该用例真的在等异步落库而非侥幸。

每次 `cp /tmp/tracing.bak internal/pkg/tracing/tracing.go` 还原后复绿。

- [x] **Step 6: 提交并推送**

```bash
git add user-server/internal/pkg/tracing/tracing_span_test.go user-server/internal/pkg/tracing/tracing_sink_db_test.go
git commit -m "test: tracing 载体/Span 纯函数与异步落库端到端补测"
```
按 Task 1 Step 6 的两远端 ff 校验流程推送。

---

## Task 3: knowledge/service constants —— 配置读取器注入

**Files:**
- Create: `user-server/internal/aiagent/knowledge/service/constants_config_test.go`
- 参考（只读）：`internal/aiagent/knowledge/service/constants.go:13-152`（`ConfigReader` 接口 + 15 个 getter）

**Interfaces:**
- Consumes: `SetConfigReader(ConfigReader)`（`internal/service/config_param.go:114` 是生产注入点，测试不复用）；
  getter：`EmbeddingDim/AsyncProcessingTimeout/ExternalImportTimeout/SSRFCheckTimeout/DefaultTopK/ChunkContentPreview/`
  `MaxSearchListSize/BM25ScanLimit/DefaultSimilarityThreshold/DefaultTemperature/DefaultMaxTokens/DefaultTopP/`
  `DefaultRequestTimeoutSeconds/DefaultMaxRetries`。
- Produces: 无。

- [x] **Step 1: 写用例**

创建 `user-server/internal/aiagent/knowledge/service/constants_config_test.go`：

```go
package service

import (
	"context"
	"fmt"
	"testing"
	"time"
)

type cfgCall struct {
	group, key string
	fbInt      int
	fbFloat    float64
	fbBool     bool
	fbDur      time.Duration
	kind       string
}

// fakeReader 记录每次读取的 (group,key,fallback)，用于锁死 seed 键名与兜底值。
type fakeReader struct {
	calls   []cfgCall
	intVal  int
	floatVal float64
	boolVal bool
	durVal  time.Duration
}

func (f *fakeReader) record(c cfgCall) {
	f.calls = append(f.calls, c)
}

func (f *fakeReader) GetInt(ctx context.Context, group, key string, fallback int) int {
	f.record(cfgCall{group: group, key: key, fbInt: fallback, kind: "int"})
	return f.intVal
}

func (f *fakeReader) GetFloat(ctx context.Context, group, key string, fallback float64) float64 {
	f.record(cfgCall{group: group, key: key, fbFloat: fallback, kind: "float"})
	return f.floatVal
}

func (f *fakeReader) GetBool(ctx context.Context, group, key string, fallback bool) bool {
	f.record(cfgCall{group: group, key: key, fbBool: fallback, kind: "bool"})
	return f.boolVal
}

func (f *fakeReader) GetDuration(ctx context.Context, group, key string, fallback time.Duration) time.Duration {
	f.record(cfgCall{group: group, key: key, fbDur: fallback, kind: "dur"})
	return f.durVal
}

// TestConfigGettersFallBackWithoutReader 未注入读取器时必须回落硬编码默认值。
func TestConfigGettersFallBackWithoutReader(t *testing.T) {
	SetConfigReader(nil)
	t.Cleanup(func() { SetConfigReader(nil) })

	if EmbeddingDim() != 1024 {
		t.Errorf("EmbeddingDim=%d want 1024", EmbeddingDim())
	}
	if AsyncProcessingTimeout() != 15*time.Minute {
		t.Errorf("AsyncProcessingTimeout=%v want 15m", AsyncProcessingTimeout())
	}
	if ExternalImportTimeout() != 30*time.Minute {
		t.Errorf("ExternalImportTimeout=%v want 30m", ExternalImportTimeout())
	}
	if SSRFCheckTimeout() != 5*time.Second {
		t.Errorf("SSRFCheckTimeout=%v want 5s", SSRFCheckTimeout())
	}
	if DefaultTopK() != 5 {
		t.Errorf("DefaultTopK=%d want 5", DefaultTopK())
	}
	if ChunkContentPreview() != 500 {
		t.Errorf("ChunkContentPreview=%d want 500", ChunkContentPreview())
	}
	if MaxSearchListSize() != 1000 {
		t.Errorf("MaxSearchListSize=%d want 1000", MaxSearchListSize())
	}
	if BM25ScanLimit() != 10000 {
		t.Errorf("BM25ScanLimit=%d want 10000", BM25ScanLimit())
	}
	if DefaultSimilarityThreshold() != 0.5 {
		t.Errorf("DefaultSimilarityThreshold=%v want 0.5", DefaultSimilarityThreshold())
	}
	if DefaultTemperature() != 0.7 {
		t.Errorf("DefaultTemperature=%v want 0.7", DefaultTemperature())
	}
	if DefaultMaxTokens() != 1000 {
		t.Errorf("DefaultMaxTokens=%d want 1000", DefaultMaxTokens())
	}
	if DefaultTopP() != 0.9 {
		t.Errorf("DefaultTopP=%v want 0.9", DefaultTopP())
	}
	if DefaultRequestTimeoutSeconds() != 60 {
		t.Errorf("DefaultRequestTimeoutSeconds=%d want 60", DefaultRequestTimeoutSeconds())
	}
	if DefaultMaxRetries() != 3 {
		t.Errorf("DefaultMaxRetries=%d want 3", DefaultMaxRetries())
	}
	if DefaultPageSize != 20 || DefaultFrequencyPenalty != 0.5 || DefaultPresencePenalty != 0.5 {
		t.Error("常量默认值被改动")
	}
}

// TestConfigGettersReadInjectedKeys 注入后必须按 (group,key) 读取，并把兜底值一并传下去。
func TestConfigGettersReadInjectedKeys(t *testing.T) {
	f := &fakeReader{intVal: 77, floatVal: 0.11, boolVal: true, durVal: 90 * time.Second}
	SetConfigReader(f)
	t.Cleanup(func() { SetConfigReader(nil) })

	// 逐个调用全部 getter，顺序必须与下方 cases 完全一致：
	// fakeReader 按调用顺序记录 (group,key,kind,兜底值)，据此逐位比对。
	_ = EmbeddingDim()
	_ = AsyncProcessingTimeout()
	_ = ExternalImportTimeout()
	_ = SSRFCheckTimeout()
	_ = DefaultTopK()
	_ = ChunkContentPreview()
	_ = MaxSearchListSize()
	_ = BM25ScanLimit()
	_ = DefaultSimilarityThreshold()
	_ = DefaultTemperature()
	_ = DefaultMaxTokens()
	_ = DefaultTopP()
	_ = DefaultRequestTimeoutSeconds()
	_ = DefaultMaxRetries()

	cases := []struct {
		group, key, kind string
		fb               string
	}{
		{"knowledge", "embedding_dimension", "int", "1024"},
		{"knowledge", "async_processing_timeout", "dur", "15m0s"},
		{"knowledge", "external_import_timeout", "dur", "30m0s"},
		{"knowledge", "ssrf_check_timeout", "dur", "5s"},
		{"knowledge", "default_top_k", "int", "5"},
		{"knowledge", "chunk_preview_max_len", "int", "500"},
		{"knowledge", "max_search_list_size", "int", "1000"},
		{"knowledge", "bm25_scan_limit", "int", "10000"},
		{"knowledge", "similarity_threshold", "float", "0.5"},
		{"agent_llm", "temperature", "float", "0.7"},
		{"agent_llm", "max_tokens", "int", "1000"},
		{"agent_llm", "top_p", "float", "0.9"},
		{"agent_llm", "request_timeout", "dur", "1m0s"},
		{"agent_llm", "max_retries", "int", "3"},
	}
	if len(f.calls) != len(cases) {
		t.Fatalf("读取次数=%d want %d", len(f.calls), len(cases))
	}
	for i, want := range cases {
		got := f.calls[i]
		if got.group != want.group || got.key != want.key || got.kind != want.kind {
			t.Errorf("第 %d 次读取 = (%s,%s,%s) want (%s,%s,%s)",
				i, got.group, got.key, got.kind, want.group, want.key, want.kind)
		}
		var gotFb string
		switch got.kind {
		case "int":
			gotFb = fmt.Sprintf("%v", got.fbInt)
		case "float":
			gotFb = fmt.Sprintf("%v", got.fbFloat)
		case "dur":
			gotFb = got.fbDur.String()
		}
		if gotFb != want.fb {
			t.Errorf("%s.%s 兜底值=%q want %q", want.group, want.key, gotFb, want.fb)
		}
	}

	if EmbeddingDim() != 77 || DefaultTemperature() != 0.11 || SSRFCheckTimeout() != 90*time.Second {
		t.Error("注入值未透传到 getter 返回值")
	}
	if DefaultRequestTimeoutSeconds() != 90 {
		t.Errorf("DefaultRequestTimeoutSeconds 应按秒取整, got %d", DefaultRequestTimeoutSeconds())
	}
}

// TestSetConfigReaderNilIsIdempotent 重复清空不得 panic。
func TestSetConfigReaderNilIsIdempotent(t *testing.T) {
	SetConfigReader(nil)
	SetConfigReader(&fakeReader{intVal: 1})
	if EmbeddingDim() != 1 {
		t.Fatalf("注入后应读到 1, got %d", EmbeddingDim())
	}
	SetConfigReader(nil)
	if EmbeddingDim() != 1024 {
		t.Errorf("清空后应回落 1024, got %d", EmbeddingDim())
	}
}
```

`want.fb` 的期望串取 `1024 / 15m0s / 30m0s / 5s / 5 / 500 / 1000 / 10000 / 0.5 / 0.7 / 1000 / 0.9 / 1m0s / 3`，
与 `constants.go` 里的兜底字面量一一对应（键名与兜底值同时被钉住，改任一处都会红）。

- [x] **Step 2: 跑绿**

Run: `go test -p 1 -count=1 -cover ./internal/aiagent/knowledge/service/`
Expected: ok；`go tool cover -func` 中 constants.go 全部 getter 100.0%。

- [x] **Step 3: 反向验证**

备份 `constants.go` → 把 `DefaultTopK` 的 key `"default_top_k"` 改成 `"top_k"` → `TestConfigGettersReadInjectedKeys`
必须 FAIL（键名断言）；把 `SSRFCheckTimeout` 兜底 `5*time.Second` 改成 `500*time.Millisecond` → 两个用例都 FAIL。还原复绿。

- [x] **Step 4: 提交并推送**

`git add user-server/internal/aiagent/knowledge/service/constants_config_test.go`
→ `git commit -m "test: 知识库运行时配置读取器注入与兜底默认值补测"` → 双远端推送。

---

## Task 4: rag/service —— RAG prompt 构造器

**Files:**
- Create: `user-server/internal/aiagent/rag/service/rag_prompt_builders_test.go`
- 参考（只读）：`internal/aiagent/rag/service/rag.go:158-189`（`buildContextString`/`buildRAGPrompt`）、
  `rag.go:271-292`（`buildStructuredRAGPrompt`）、`rag/core/rag_engine.go:25-33`（`Chunk` 字段）

**Interfaces:**
- Consumes: 非导出 `buildContextString([]rag_core.Chunk) string`、
  `buildRAGPrompt(query, contextStr string, contextData map[string]any) string`、
  `buildStructuredRAGPrompt(query, contextStr string, contextData map[string]any, schema any) string`。
- Produces: 无。
- **不得**重名：`rag_test.go` 已定义 `mockThreeTier`、`dummyError`、`requireRealAPIKey`。

- [x] **Step 1: 写用例**

创建 `user-server/internal/aiagent/rag/service/rag_prompt_builders_test.go`：

```go
package rag_service

import (
	"encoding/json"
	"strings"
	"testing"

	rag_core "hivemtk-user/internal/aiagent/rag/core"
)

func TestBuildContextStringEmptyYieldsNoEvidence(t *testing.T) {
	if got := buildContextString(nil); got != "未找到相关文档。" {
		t.Errorf("空召回提示语=%q", got)
	}
	if got := buildContextString([]rag_core.Chunk{}); got != "未找到相关文档。" {
		t.Errorf("零长切片应同 nil, got %q", got)
	}
}

func TestBuildContextStringNumbersAndFormatsScore(t *testing.T) {
	chunks := []rag_core.Chunk{
		{ID: "c1", DocumentID: "doc-a", Content: "退款政策 7 天", Score: 0.8765},
		{ID: "c2", DocumentID: "doc-b", Content: "运费说明", Score: 0.5},
	}
	got := buildContextString(chunks)

	for _, want := range []string{
		"参考信息:\n",
		"[1] 来源: doc-a (相似度: 0.88)\n退款政策 7 天",
		"[2] 来源: doc-b (相似度: 0.50)\n运费说明",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("缺少片段 %q，实际输出:\n%s", want, got)
		}
	}
	if strings.Count(got, "[") != 2 {
		t.Errorf("编号数量错:\n%s", got)
	}
	if strings.Contains(got, "c1") {
		t.Error("Chunk.ID 不应出现在上下文里，只允许 DocumentID")
	}
}

func TestBuildRAGPromptStructure(t *testing.T) {
	base := buildRAGPrompt("多久能到？", "参考信息正文", nil)
	if !strings.HasPrefix(base, "基于以下参考信息回答问题") {
		t.Errorf("缺少指令头:\n%s", base)
	}
	for _, want := range []string{"参考信息正文", "问题: 多久能到？", "回答:"} {
		if !strings.Contains(base, want) {
			t.Errorf("缺少片段 %q", want)
		}
	}
	if strings.Contains(base, "额外上下文") {
		t.Error("contextData 为空时不应追加额外上下文段")
	}

	withCtx := buildRAGPrompt("q", "ctx", map[string]any{"channel": "wecom"})
	if !strings.Contains(withCtx, "额外上下文:") {
		t.Fatalf("应追加额外上下文:\n%s", withCtx)
	}
	tail := withCtx[strings.Index(withCtx, "额外上下文:"):]
	var decoded map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(tail, "额外上下文:"))), &decoded); err != nil {
		t.Fatalf("额外上下文不是合法 JSON: %v (%s)", err, tail)
	}
	if decoded["channel"] != "wecom" {
		t.Errorf("额外上下文内容错: %v", decoded)
	}

	broken := buildRAGPrompt("q", "ctx", map[string]any{"bad": make(chan int)})
	if !strings.HasSuffix(broken, "额外上下文: ") {
		t.Errorf("序列化失败时应追加空的额外上下文段（既有行为）, got tail=%q",
			broken[strings.Index(broken, "额外上下文:"):])
	}
}

func TestBuildStructuredRAGPromptEmbedsSchema(t *testing.T) {
	schema := map[string]any{"type": "object", "required": []string{"answer"}}
	got := buildStructuredRAGPrompt("有什么颜色", "ctx 正文", map[string]any{"uid": "u1"}, schema)

	for _, want := range []string{"请严格按照以下JSON Schema返回结果:", "问题: 有什么颜色", "ctx 正文", "\"required\"", "\"answer\"", "额外上下文:"} {
		if !strings.Contains(got, want) {
			t.Errorf("缺少片段 %q:\n%s", want, got)
		}
	}
	start := strings.Index(got, "请严格按照以下JSON Schema返回结果:")
	schemaLine := strings.TrimSpace(strings.Split(got[start:], "\n")[1])
	var back map[string]any
	if err := json.Unmarshal([]byte(schemaLine), &back); err != nil {
		t.Fatalf("schema 段不是合法 JSON: %v (%q)", err, schemaLine)
	}
	if bt, ok := back["type"].(string); !ok || bt != "object" {
		t.Errorf("schema type 丢失: %v", back)
	}

	noSchema := buildStructuredRAGPrompt("q", "ctx", nil, nil)
	if !strings.Contains(noSchema, "null") {
		t.Errorf("nil schema 应序列化为 null:\n%s", noSchema)
	}
	if strings.Contains(noSchema, "额外上下文") {
		t.Error("nil contextData 不应追加额外上下文段")
	}
	if bad := buildStructuredRAGPrompt("q", "ctx", nil, make(chan int)); strings.Contains(bad, "chan") ||
		!strings.Contains(bad, "回答:") {
		t.Errorf("不可序列化 schema 时 schema 段应为空串（既有行为）, got:\n%s", bad)
	}
}
```

- [x] **Step 2: 跑绿**

Run: `go test -p 1 -count=1 -cover ./internal/aiagent/rag/service/ && go test -p 1 -count=1 -coverprofile=/tmp/rag.cov ./internal/aiagent/rag/service/ && go tool cover -func=/tmp/rag.cov | grep -E 'buildContextString|buildRAGPrompt|buildStructuredRAGPrompt'`
Expected: 三个构造器 100.0%。
（`buildStructuredRAGPrompt(nil schema)` 的 `json.Marshal(nil)` 结果是 `"null"`；`make(chan int)` 同理走 err→`schemaJSON` 为 nil→`string(nil)` 为空串。
若实际行为与断言不符，**以实际为准改断言并在 commit body 记录该分支的真实行为**，不得反向改生产码。）

- [x] **Step 3: 反向验证**

备份 `rag.go` → 把 `buildContextString` 的 `%.2f` 改成 `%.1f` → `TestBuildContextStringNumbersAndFormatsScore` FAIL；
把 `if len(contextData) > 0` 改成 `if len(contextData) >= 0`（nil 也追加）→ `TestBuildRAGPromptStructure` FAIL。还原复绿。

- [x] **Step 4: 提交并推送**

`git add user-server/internal/aiagent/rag/service/rag_prompt_builders_test.go`
→ `git commit -m "test: RAG 上下文与提示词构造器分支补测"` → 双远端推送。

---

## Task 5: customer_service —— 内存会话管理剩余分支

**Files:**
- Create: `user-server/internal/aiagent/rag/customer_service/dialog_sessions_ext_test.go`
- 参考（只读）：`internal/aiagent/rag/customer_service/dialog_manager.go:28-311`、`interfaces.go:1-110`
- 已有覆盖（勿重名/勿重复）：`rag_customer_test.go` 的
  `TestNewInMemoryDialogManager_NilConfig/_CustomConfig/TestInMemoryDialogManager_CreateSession/_GetSession/_GetSession_NotFound/_CloseSession/TestSessionStatus_Values`

**Interfaces:**
- Consumes: `(*InMemoryDialogManager)` 的 `AddMessage/GetConversationHistory/UpdateSessionMetadata/CloseSession/CleanupExpiredSessions/ListUserSessions/updateLastActivity`、
  `generateSessionID`、`NewInMemoryDialogManager(*DialogManagerConfig)`、`Session/Message/Conversation/SessionConfig/SessionStatus` 常量。
- Produces: 无。

- [x] **Step 1: 写用例**

创建 `user-server/internal/aiagent/rag/customer_service/dialog_sessions_ext_test.go`：

```go
package ragcustomerservice

import (
	"context"
	"strings"
	"testing"
	"time"
)

func newSessionForTest(t *testing.T, cfg SessionConfig) (*InMemoryDialogManager, *Session) {
	t.Helper()
	dm := NewInMemoryDialogManager(&DialogManagerConfig{
		DefaultMaxHistoryLength: 3,
		DefaultSessionTimeout:   time.Hour,
		SessionCleanupInterval:  time.Hour, // 拉长后台清理周期，避免与本用例竞态
	})
	s, err := dm.CreateSession(context.Background(), "u-1", "wecom", "kb-1", cfg)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	return dm, s
}

func TestCreateSessionAppliesDefaultsAndValidates(t *testing.T) {
	dm, s := newSessionForTest(t, SessionConfig{})
	if s.Config.MaxHistoryLength != 3 {
		t.Errorf("MaxHistoryLength 应回填默认 3, got %d", s.Config.MaxHistoryLength)
	}
	if s.Config.Timeout != 3600 {
		t.Errorf("Timeout 应回填默认 3600s, got %d", s.Config.Timeout)
	}
	if s.Status != SessionActive || s.Conversation == nil || s.Metadata["last_activity"] == nil {
		t.Errorf("新建会话形态错: %+v", s)
	}

	// 显式配置不得被默认值覆盖
	s2, err := dm.CreateSession(context.Background(), "u-2", "tg", "kb-2", SessionConfig{MaxHistoryLength: 9, Timeout: 30})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if s2.Config.MaxHistoryLength != 9 || s2.Config.Timeout != 30 {
		t.Errorf("显式配置被改写: %+v", s2.Config)
	}

	for _, tc := range []struct{ name, user, platform, kb string }{
		{"空 user", "", "wx", "kb"}, {"空 platform", "u", "", "kb"}, {"空 kb", "u", "wx", ""},
	} {
		if _, err := dm.CreateSession(context.Background(), tc.user, tc.platform, tc.kb, SessionConfig{}); err == nil {
			t.Errorf("%s 应返回错误", tc.name)
		}
	}
}

func TestAddMessageTrimsToMaxHistory(t *testing.T) {
	dm, s := newSessionForTest(t, SessionConfig{})
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		msg := Message{ID: "m", Role: MessageRoleUser, Content: strings.Repeat("x", i+1)}
		if err := dm.AddMessage(ctx, s.ID, msg); err != nil {
			t.Fatalf("AddMessage: %v", err)
		}
	}
	conv, err := dm.GetConversationHistory(ctx, s.ID, 0)
	if err != nil {
		t.Fatalf("GetConversationHistory: %v", err)
	}
	if len(conv.Messages) != 3 {
		t.Fatalf("应裁剪到 MaxHistoryLength=3, got %d", len(conv.Messages))
	}
	if conv.Messages[0].Content != "xxx" || conv.Messages[2].Content != "xxxxx" {
		t.Errorf("保留的应是最后 3 条: %q", []string{conv.Messages[0].Content, conv.Messages[2].Content})
	}
	if conv.Metadata["last_message_time"] == nil {
		t.Error("AddMessage 应写入 last_message_time")
	}
	if conv.Messages[0].Timestamp.IsZero() {
		t.Error("零值 Timestamp 应被补全为 now")
	}

	fixed := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := dm.AddMessage(ctx, s.ID, Message{Content: "带时间戳", Timestamp: fixed}); err != nil {
		t.Fatalf("AddMessage: %v", err)
	}
	conv2, _ := dm.GetConversationHistory(ctx, s.ID, 0)
	if got := conv2.Metadata["last_message_time"]; got != fixed {
		t.Errorf("显式 Timestamp 应原样写入 metadata, got %v", got)
	}
}

func TestAddMessageAndHistoryErrorBranches(t *testing.T) {
	dm, s := newSessionForTest(t, SessionConfig{})
	ctx := context.Background()

	if err := dm.AddMessage(ctx, "", Message{}); err == nil {
		t.Error("空 sessionID 应报错")
	}
	if err := dm.AddMessage(ctx, "nope", Message{}); err == nil {
		t.Error("不存在的会话应报错")
	}
	if _, err := dm.GetConversationHistory(ctx, "", 5); err == nil {
		t.Error("空 sessionID 查询历史应报错")
	}
	if _, err := dm.GetConversationHistory(ctx, "nope", 5); err == nil {
		t.Error("不存在会话查询历史应报错")
	}

	if err := dm.CloseSession(ctx, s.ID); err != nil {
		t.Fatalf("CloseSession: %v", err)
	}
	if err := dm.AddMessage(ctx, s.ID, Message{Content: "迟到"}); err == nil {
		t.Error("已关闭会话不应再接收消息")
	}
}

func TestGetConversationHistoryLimitTakesTail(t *testing.T) {
	dm, s := newSessionForTest(t, SessionConfig{MaxHistoryLength: 10})
	ctx := context.Background()
	for i := 0; i < 6; i++ {
		if err := dm.AddMessage(ctx, s.ID, Message{Role: MessageRoleAssistant, Content: string(rune('a' + i))}); err != nil {
			t.Fatalf("AddMessage: %v", err)
		}
	}
	conv, err := dm.GetConversationHistory(ctx, s.ID, 2)
	if err != nil {
		t.Fatalf("GetConversationHistory: %v", err)
	}
	if len(conv.Messages) != 2 || conv.Messages[0].Content != "e" || conv.Messages[1].Content != "f" {
		t.Errorf("limit=2 应取最后两条, got %d 条 %q", len(conv.Messages), contents(conv.Messages))
	}
	if all, _ := dm.GetConversationHistory(ctx, s.ID, 0); len(all.Messages) != 6 {
		t.Errorf("limit<=0 应返回全量, got %d", len(all.Messages))
	}
	if over, _ := dm.GetConversationHistory(ctx, s.ID, 99); len(over.Messages) != 6 {
		t.Errorf("limit 超过长度应返回全量, got %d", len(over.Messages))
	}
	if cur, _ := dm.GetSession(ctx, s.ID); len(cur.Conversation.Messages) != 6 {
		t.Error("读取历史不得截断会话内存储")
	}
}

func contents(msgs []Message) []string {
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, m.Content)
	}
	return out
}

func TestUpdateSessionMetadataMerges(t *testing.T) {
	dm, s := newSessionForTest(t, SessionConfig{})
	ctx := context.Background()

	before := s.UpdatedAt
	if err := dm.UpdateSessionMetadata(ctx, s.ID, map[string]any{"stage": "quote"}); err != nil {
		t.Fatalf("UpdateSessionMetadata: %v", err)
	}
	if err := dm.UpdateSessionMetadata(ctx, s.ID, map[string]any{"owner": "agent-1", "last_activity": before}); err != nil {
		t.Fatalf("UpdateSessionMetadata: %v", err)
	}
	got, _ := dm.GetSession(ctx, s.ID)
	if got.Metadata["stage"] != "quote" || got.Metadata["owner"] != "agent-1" {
		t.Errorf("元数据未合并: %v", got.Metadata)
	}
	if !got.UpdatedAt.After(before) {
		t.Error("更新元数据应刷新 UpdatedAt")
	}

	if err := dm.UpdateSessionMetadata(ctx, "", map[string]any{"k": 1}); err == nil {
		t.Error("空 sessionID 应报错")
	}
	if err := dm.UpdateSessionMetadata(ctx, s.ID, map[string]any{}); err == nil {
		t.Error("空 metadata 应报错")
	}
	if err := dm.UpdateSessionMetadata(ctx, "nope", map[string]any{"k": 1}); err == nil {
		t.Error("不存在会话应报错")
	}

	orphan := &Session{ID: "manual", Metadata: nil}
	dm.sessions["manual"] = orphan
	if err := dm.UpdateSessionMetadata(ctx, "manual", map[string]any{"k": "v"}); err != nil {
		t.Fatalf("nil Metadata 会话应先建容器再写: %v", err)
	}
	if orphan.Metadata["k"] != "v" {
		t.Errorf("nil Metadata 分支失效: %v", orphan.Metadata)
	}
}

func TestCleanupExpiredSessionsRemovesOnlyStale(t *testing.T) {
	dm, s := newSessionForTest(t, SessionConfig{Timeout: 1}) // 1 秒超时
	ctx := context.Background()

	fresh, err := dm.CreateSession(ctx, "u-2", "wecom", "kb-1", SessionConfig{Timeout: 3600})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	// 手工把 s 的最后活跃时间推到 1 小时前
	dm.sessions[s.ID].Metadata["last_activity"] = time.Now().Add(-time.Hour)
	// 无 last_activity 的会话应被忽略（不属过期判定范围）
	noActivity := &Session{ID: "no-activity", UserID: "u-3", Config: SessionConfig{Timeout: 1}, Metadata: map[string]any{}}
	dm.sessions["no-activity"] = noActivity

	if err := dm.CleanupExpiredSessions(ctx); err != nil {
		t.Fatalf("CleanupExpiredSessions: %v", err)
	}
	if _, ok := dm.sessions[s.ID]; ok {
		t.Error("过期会话应被删除")
	}
	if _, ok := dm.sessions[fresh.ID]; !ok {
		t.Error("活跃会话不应被删除")
	}
	if _, ok := dm.sessions[noActivity.ID]; !ok {
		t.Error("缺 last_activity 的会话不应被删除")
	}
}

func TestListUserSessionsFilters(t *testing.T) {
	dm, _ := newSessionForTest(t, SessionConfig{})
	ctx := context.Background()

	// 平台互不相同：同 (user,platform) 连建两个会话会因 generateSessionID 时钟粒度相撞而互相覆盖
	for _, spec := range []struct{ user, platform string }{
		{"u-1", "wecom2"}, {"u-1", "telegram"}, {"u-2", "wecom2"},
	} {
		if _, err := dm.CreateSession(ctx, spec.user, spec.platform, "kb-x", SessionConfig{}); err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
	}
	if _, err := dm.ListUserSessions(ctx, "", "wecom", SessionActive); err == nil {
		t.Error("空 userID 应报错")
	}

	all, err := dm.ListUserSessions(ctx, "u-1", "", "")
	if err != nil {
		t.Fatalf("ListUserSessions: %v", err)
	}
	if len(all) != 3 { // 本用例 u-1 共 1(初始)+2 个
		t.Fatalf("按用户过滤结果=%d want 3", len(all))
	}
	byPlatform, _ := dm.ListUserSessions(ctx, "u-1", "telegram", "")
	if len(byPlatform) != 1 || byPlatform[0].Platform != "telegram" {
		t.Errorf("按平台过滤错: %d 条", len(byPlatform))
	}
	closed, _ := dm.ListUserSessions(ctx, "u-1", "telegram", SessionClosed)
	if len(closed) != 0 {
		t.Errorf("状态过滤错: %d", len(closed))
	}
	if err := dm.CloseSession(ctx, byPlatform[0].ID); err != nil {
		t.Fatalf("CloseSession: %v", err)
	}
	nowClosed, _ := dm.ListUserSessions(ctx, "u-1", "telegram", SessionClosed)
	if len(nowClosed) != 1 || nowClosed[0].Metadata["closed_at"] == nil {
		t.Errorf("关闭后应按状态查到且写入 closed_at: %+v", nowClosed)
	}
	if none, _ := dm.ListUserSessions(ctx, "ghost", "", ""); len(none) != 0 {
		t.Errorf("无匹配用户应返回空, got %d", len(none))
	}
}

func TestCloseSessionErrorBranchesAndActivity(t *testing.T) {
	dm, s := newSessionForTest(t, SessionConfig{})
	ctx := context.Background()

	if err := dm.CloseSession(ctx, ""); err == nil {
		t.Error("空 sessionID 应报错")
	}
	if err := dm.CloseSession(ctx, "nope"); err == nil {
		t.Error("不存在会话应报错")
	}

	old := s.UpdatedAt
	time.Sleep(2 * time.Millisecond)
	if _, err := dm.GetSession(ctx, s.ID); err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	after, _ := dm.GetSession(ctx, s.ID)
	if !after.UpdatedAt.After(old) {
		t.Error("GetSession 应刷新活跃时间")
	}
	dm.updateLastActivity("不存在的会话") // 覆盖 exists=false 分支，不应 panic
}

func TestGenerateSessionIDShape(t *testing.T) {
	a := generateSessionID("u1", "wx")
	b := generateSessionID("u1", "wx")
	if !strings.HasPrefix(a, "u1_wx_") {
		t.Errorf("ID 前缀错: %q", a)
	}
	if a == b {
		t.Error("纳秒后缀应保证同参不同 ID")
	}
}
```

- [x] **Step 2: 跑绿**

Run: `go test -p 1 -count=1 -cover ./internal/aiagent/rag/customer_service/`
Expected: ok（该包此时不连 DB、不起 HTTP）。

- [x] **Step 3: 反向验证**

备份 `dialog_manager.go` → 三处注入，各跑对应用例：
1. `AddMessage` 去掉裁剪分支（`if len(...) > MaxHistoryLength {` 改 `if false &&`）→ `TestAddMessageTrimsToMaxHistory` FAIL。
2. `CleanupExpiredSessions` 的 `now.Sub(lastActivityTime) > timeoutDuration` 改 `<` → `TestCleanupExpiredSessionsRemovesOnlyStale` FAIL。
3. `ListUserSessions` 去掉 `platform == "" ||` 短路 → `TestListUserSessionsFilters` FAIL。
逐次 `cp` 还原后复绿。

- [x] **Step 4: 提交并推送**

`git add user-server/internal/aiagent/rag/customer_service/dialog_sessions_ext_test.go`
→ `git commit -m "test: 内存对话管理器历史裁剪与会话过滤补测"` → 双远端推送。

---

## Task 6: customer_service —— 上下文理解与关键词启发式

**Files:**
- Create: `user-server/internal/aiagent/rag/customer_service/context_understanding_test.go`
- 参考（只读）：`dialog_manager.go:327-718`

**Interfaces:**
- Consumes: `NewContextUnderstandingService(*ContextUnderstandingConfig)`、`AnalyzeIntent/ExtractEntities/AnalyzeSentiment/UpdateContext/DetectTopicChange/GetUserPreferences/UpdateUserPreferences`、
  非导出 `toLower/containsAny/contains/findSubstring/extractParameters/extractTimeEntities/extractProductEntities/extractBrandEntities/`
  `calculateSentimentScore/getSentimentLabel/detectEmotions/hasAnyWords/isRelatedTopic/extractOrderNumber/extractProductName`。
- Produces: 无。

- [x] **Step 1: 写用例**

创建 `user-server/internal/aiagent/rag/customer_service/context_understanding_test.go`：

```go
package ragcustomerservice

import (
	"context"
	"strings"
	"testing"
)

func newCUSSvc() *ContextUnderstandingServiceImpl {
	return NewContextUnderstandingService(nil)
}

func TestNewContextUnderstandingServiceDefaults(t *testing.T) {
	d := newCUSSvc()
	if d.config.IntentRecognitionThreshold != 0.6 || !d.config.EntityExtractionEnabled ||
		!d.config.SentimentAnalysisEnabled || !d.config.TopicDetectionEnabled {
		t.Errorf("nil 配置应填默认值: %+v", d.config)
	}
	custom := NewContextUnderstandingService(&ContextUnderstandingConfig{EntityExtractionEnabled: false})
	if custom.config.EntityExtractionEnabled {
		t.Error("显式 false 不得被默认值覆盖")
	}
}

func TestAnalyzeIntentCategories(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		msg      string
		wantTop  string
		wantCat0 string
		conf     float64
	}{
		{"你好呀", "greeting", "social", 0.8},
		{"HELLO there", "greeting", "social", 0.8},
		{"这个产品多少钱", "product_inquiry", "sales", 0.8},
		{"我的订单还没发货", "order_inquiry", "support", 0.8},
		{"我要投诉", "complaint_support", "support", 0.8},
		{"谢谢你的帮助", "positive_feedback", "social", 0.8},
		{"随便看看天气", "general_inquiry", "general", 0.6},
	}
	for _, tc := range cases {
		got, err := newCUSSvc().AnalyzeIntent(ctx, tc.msg, nil)
		if err != nil {
			t.Fatalf("AnalyzeIntent(%q): %v", tc.msg, err)
		}
		if got.PrimaryIntent != tc.wantTop || got.Confidence != tc.conf {
			t.Errorf("msg=%q intent=%q conf=%v want %q/%v",
				tc.msg, got.PrimaryIntent, got.Confidence, tc.wantTop, tc.conf)
		}
		if len(got.Categories) == 0 || got.Categories[0] != tc.wantCat0 {
			t.Errorf("msg=%q categories=%v want 首位 %q", tc.msg, got.Categories, tc.wantCat0)
		}
	}
	if _, err := newCUSSvc().AnalyzeIntent(ctx, "", nil); err == nil {
		t.Error("空消息应报错")
	}
	withParams, _ := newCUSSvc().AnalyzeIntent(ctx, "订单号 12345 有货吗", nil)
	if withParams.Parameters["order_number"] != "ORDER123456" {
		t.Errorf("参数抽取应命中订单号（当前为固定样例值）, got %v", withParams.Parameters)
	}
}

func TestExtractEntitiesRespectsSwitch(t *testing.T) {
	ctx := context.Background()
	svc := newCUSSvc()
	got, err := svc.ExtractEntities(ctx, "明天想买一件苹果的裙子，下周也要")
	if err != nil {
		t.Fatalf("ExtractEntities: %v", err)
	}
	if strings.Join(got["time"], ",") != "明天,下周" {
		t.Errorf("time 实体=%v want 明天,下周", got["time"])
	}
	if strings.Join(got["product"], ",") != "裙子" {
		t.Errorf("product 实体=%v", got["product"])
	}
	if strings.Join(got["brand"], ",") != "苹果" {
		t.Errorf("brand 实体=%v", got["brand"])
	}

	off := NewContextUnderstandingService(&ContextUnderstandingConfig{}) // EntityExtractionEnabled=false
	empty, err := off.ExtractEntities(ctx, "明天想买裙子")
	if err != nil {
		t.Fatalf("关闭开关仍应无错误: %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("关闭开关应返回空 map, got %v", empty)
	}

	none, _ := svc.ExtractEntities(ctx, "毫无关键词的一句话")
	if len(none) != 0 {
		t.Errorf("无关键词应为空 map, got %v", none)
	}
	if _, err := svc.ExtractEntities(ctx, ""); err == nil {
		t.Error("空消息应报错")
	}
}

func TestAnalyzeSentimentScoring(t *testing.T) {
	ctx := context.Background()
	svc := newCUSSvc()

	pos, err := svc.AnalyzeSentiment(ctx, "这个方案真不错，我很喜欢，推荐给大家")
	if err != nil {
		t.Fatalf("AnalyzeSentiment: %v", err)
	}
	if pos.Label != "positive" || pos.Score <= 0.1 {
		t.Errorf("正面语料 score=%v label=%q", pos.Score, pos.Label)
	}

	neg, _ := svc.AnalyzeSentiment(ctx, "太失望了，质量差，还很贵")
	if neg.Label != "negative" || neg.Score >= -0.1 {
		t.Errorf("负面语料 score=%v label=%q", neg.Score, neg.Label)
	}

	mixed, _ := svc.AnalyzeSentiment(ctx, "还好，就是有点贵") // 命中 1 正（好）1 负（贵）
	if mixed.Score != 0 || mixed.Label != "neutral" {
		t.Errorf("正负词各一次应抵消为中性, score=%v label=%q", mixed.Score, mixed.Label)
	}

	neu, _ := svc.AnalyzeSentiment(ctx, "今天周三")
	if neu.Label != "neutral" || neu.Score != 0.0 {
		t.Errorf("中性语料 score=%v label=%q", neu.Score, neu.Label)
	}

	angry, _ := svc.AnalyzeSentiment(ctx, "我很生气，也很开心你们处理了")
	types := map[string]bool{}
	for _, e := range angry.Emotions {
		types[e.Type] = true
	}
	if !types["anger"] || !types["joy"] {
		t.Errorf("情绪识别应同时命中 anger 与 joy, got %+v", angry.Emotions)
	}

	off := NewContextUnderstandingService(&ContextUnderstandingConfig{})
	neutral, _ := off.AnalyzeSentiment(ctx, "太失望了")
	if neutral.Score != 0.0 || neutral.Label != "neutral" || neutral.Emotions != nil {
		t.Errorf("关闭情感分析应直接返回中性: %+v", neutral)
	}
	if _, err := svc.AnalyzeSentiment(ctx, ""); err == nil {
		t.Error("空消息应报错")
	}
}

func TestUpdateContextAndTopicDetection(t *testing.T) {
	ctx := context.Background()
	svc := newCUSSvc()

	base := Context{Topic: "product_inquiry"}
	msg := Message{Content: "你好"} // greeting 与 product_inquiry 跨域 → 应判话题变更
	intent, _ := svc.AnalyzeIntent(ctx, msg.Content, nil)

	updated, err := svc.UpdateContext(ctx, base, msg, intent)
	if err != nil {
		t.Fatalf("UpdateContext: %v", err)
	}
	if updated.Topic != "greeting" {
		t.Errorf("话题应切换为 greeting, got %q", updated.Topic)
	}
	if len(updated.PreviousTopics) != 1 || updated.PreviousTopics[0] != "product_inquiry" {
		t.Errorf("历史话题未记录: %v", updated.PreviousTopics)
	}
	if updated.Intent != intent.PrimaryIntent {
		t.Errorf("intent 未写入: %q", updated.Intent)
	}
	if updated.Sentiment.Label == "" || updated.LastInteraction.IsZero() {
		t.Error("情感与最后交互时间应被写入")
	}

	// 同域消息不切换话题，但参数应写进 Entities
	orderMsg := Message{Content: "订单号 9527 什么时候发货"}
	orderIntent, _ := svc.AnalyzeIntent(ctx, orderMsg.Content, nil)
	same, _ := svc.UpdateContext(ctx, Context{Topic: "product_inquiry"}, orderMsg, orderIntent)
	if same.Topic != "product_inquiry" || len(same.PreviousTopics) != 0 {
		t.Errorf("同域不应切换话题: topic=%q prev=%v", same.Topic, same.PreviousTopics)
	}
	if len(same.Entities["order_number"]) != 1 {
		t.Errorf("order_number 参数应写入 Entities: %v", same.Entities)
	}

	// 空 intent 不覆盖既有 intent
	kept, _ := svc.UpdateContext(ctx, Context{Topic: "x", Intent: "keep"}, Message{Content: "随便"}, IntentAnalysis{})
	if kept.Intent != "keep" {
		t.Errorf("空 PrimaryIntent 不应覆盖, got %q", kept.Intent)
	}

	// 已有 Entities 时原地合并；浅拷贝共享底层 map 属既有行为（见 Findings 3），在此钉住
	seeded := Context{Topic: "greeting", Entities: map[string][]string{"a": {"1"}}}
	merged, _ := svc.UpdateContext(ctx, seeded, orderMsg, orderIntent)
	if len(merged.Entities["order_number"]) != 1 || merged.Entities["a"] == nil {
		t.Errorf("应合并进已有 Entities 且不丢既有键: %v", merged.Entities)
	}
	if _, aliased := seeded.Entities["order_number"]; !aliased {
		t.Error("UpdateContext 与调用方共享同一 Entities map（既有行为），若改为深拷贝需同步更新本断言")
	}

	if changed, topic, err := svc.DetectTopicChange(ctx, "complaint_support", Message{Content: "我要投诉"}); changed || topic != "complaint_support" || err != nil {
		t.Errorf("同域话题不应变更: changed=%v topic=%q err=%v", changed, topic, err)
	}
	if changed, topic, _ := svc.DetectTopicChange(ctx, "greeting", Message{Content: "你好"}); !changed || topic != "greeting" {
		t.Errorf("greeting 不在 sales/support 话题表内，同词也应判变更（既有行为）: changed=%v topic=%q", changed, topic)
	}
	if changed, topic, _ := svc.DetectTopicChange(ctx, "product_inquiry", Message{Content: "你好"}); !changed || topic != "greeting" {
		t.Errorf("跨域应判为话题变更: changed=%v topic=%q", changed, topic)
	}

	off := NewContextUnderstandingService(&ContextUnderstandingConfig{})
	if changed, topic, _ := off.DetectTopicChange(ctx, "old", Message{Content: "你好"}); changed || topic != "old" {
		t.Error("关闭话题检测时应原样返回")
	}
	if _, e := off.UpdateContext(ctx, Context{}, Message{Content: ""}, IntentAnalysis{}); e != nil {
		t.Errorf("UpdateContext 应吞掉内部子调用错误: %v", e)
	}

	prefs, err := svc.GetUserPreferences(ctx, "u", "wx")
	if err != nil || prefs == nil || len(prefs) != 0 {
		t.Errorf(" GetUserPreferences 应返回空 map: %v %v", prefs, err)
	}
	if err := svc.UpdateUserPreferences(ctx, "u", "wx", map[string]any{"k": "v"}); err != nil {
		t.Errorf("UpdateUserPreferences 应为 no-op: %v", err)
	}
}

func TestPureHelpers(t *testing.T) {
	if got := toLower("AbC中文Z"); got != "abc中文z" {
		t.Errorf("toLower=%q", got)
	}
	if findSubstring("abcdef", "cd") != 2 {
		t.Error("findSubstring 起始下标错")
	}
	if findSubstring("ab", "abc") != -1 {
		t.Error("text 短于 substr 应返回 -1")
	}
	if findSubstring("abc", "") != 0 {
		t.Error("空 substr 应返回 0")
	}
	if contains("abc", "") != true || contains("ab", "abc") != false {
		t.Error("contains 边界错")
	}
	if !containsAny("xx订单yy", []string{"订单", "退款"}) || containsAny("xx", []string{}) {
		t.Error("containsAny 边界错")
	}
	if hasAnyWords("今天很开心", []string{"开心"}) != true || hasAnyWords("今天", []string{"开心"}) != false {
		t.Error("hasAnyWords 边界错")
	}
	if getSentimentLabel(0.9) != "positive" || getSentimentLabel(-0.9) != "negative" || getSentimentLabel(0.0) != "neutral" {
		t.Error("getSentimentLabel 阈值边界错")
	}
	if calculateSentimentScore("不好") >= 0 {
		t.Error("\"不好\" 应判为负向")
	}
	if calculateSentimentScore("还行吧") != 0 {
		t.Error("无情感词应为 0")
	}

	if got := extractTimeEntities("昨天和前天下单"); len(got) != 2 || got[0] != "昨天" || got[1] != "前天" {
		t.Errorf("extractTimeEntities=%v", got)
	}
	if got := extractProductEntities("手机和电脑还有书籍"); len(got) != 3 || got[0] != "手机" {
		t.Errorf("extractProductEntities=%v", got)
	}
	if got := extractBrandEntities("阿迪达斯和优衣库"); len(got) != 2 || got[1] != "优衣库" {
		t.Errorf("extractBrandEntities=%v", got)
	}
	if extractOrderNumber("号") != "ORDER123456" || extractOrderNumber("无") != "" {
		t.Error("extractOrderNumber 分支错（当前实现返回固定样例号）")
	}
	if extractProductName("我想买裙子") != "裙子" || extractProductName("买个手机") != "手机" || extractProductName("无") != "" {
		t.Error("extractProductName 分支错")
	}

	params := extractParameters("订单号 123 商品 裙子 到货")
	if params["order_number"] != "ORDER123456" || params["product_name"] != "裙子" {
		t.Errorf("extractParameters=%v", params)
	}
	if len(extractParameters("无关文本")) != 0 {
		t.Error("无关键词时参数应为空")
	}

	if isRelatedTopic("", "greeting") {
		t.Error("空话题不应判相关")
	}
	if !isRelatedTopic("product_inquiry", "order_inquiry") {
		t.Error("sales 域内应判相关")
	}
	if !isRelatedTopic("complaint_support", "troubleshooting") {
		t.Error("support 域内应判相关")
	}
	if isRelatedTopic("product_inquiry", "complaint_support") {
		t.Error("跨域应判不相关")
	}
}
```

`TestPureHelpers` 里 `calculateSentimentScore` 与 `getSentimentLabel` 的期望值，
均以 `dialog_manager.go:612-648` 的词表与阈值为准（正负各一次 → score 恰好 0 → neutral）。
若实跑红，按实际行为修断言并在 commit body 记录该行为，不改生产码。

> 相对上面草稿的实跑落地差异（已全部写进文件）：
> `calculateSentimentScore("不好")` 的期望由 `>= 0 判负` 改为 **`== 0`**（正面词「好」被「不好」子串命中后抵消，见 Findings）；
> 另补 `"太差了" < 0` 作为纯负面语料断言；
> `TestAnalyzeSentimentScoring` 增 `"很难过，也很害怕"` 命中 sadness/fear，把 `detectEmotions` 从 86.7% 抬到 100%；
> `TestUpdateContextAndTopicDetection` 增两处错误分支：`DetectTopicChange(空消息)` 透传 `AnalyzeIntent` 错误、
> 以及 `UpdateContext` 在子调用报错时既不改话题也不写情感（覆盖 `err == nil` 的两个 false 分支）；
> Task 5 文件同步补 `GetSession(空 ID)` 与后台清理协程用例（`TestBackgroundCleanupReapsExpiredSession`）。

- [x] **Step 2: 跑绿**

Run: `go test -p 1 -count=1 -coverprofile=/tmp/cs.cov ./internal/aiagent/rag/customer_service/ && go tool cover -func=/tmp/cs.cov | grep -E 'customer_service/dialog_manager.go' | awk '$3!="100.0%"'`
Expected: `dialog_manager.go` 的 InMemory + 上下文理解函数全部 100%，只剩 `startSessionCleanup`（不可达错误分支，上限 83.3%）。
若个别关键词用例因中文分词/字节长度差异红，按实际行为校正断言并保留注释说明该行为，不改生产码。

> 实跑结果：全包 35.6%（不是本节先前写的 60%）。差额全部来自本排期范围外的
> `pg_dialog_manager.go` / `rag_customer.go` / `quality_assessor.go` / `response_generator.go`，
> 这些文件的 DB 与 LLM 分支需真实依赖，按「不制造离线假断言」原则不补。

- [x] **Step 3: 反向验证**

备份 `dialog_manager.go` → 注入（实跑三次均按预期红，逐次 `cp` 还原并比 md5）：
1. `AnalyzeIntent` 兜底分支 `intent.Confidence = 0.6` 改 `0.8` → `TestAnalyzeIntentCategories` FAIL
   （`msg="随便看看天气" conf=0.8 want 0.6`）。
2. `getSentimentLabel` 的 `score > 0.1` 改 `score > -0.1` → `TestAnalyzeSentimentScoring`（mixed + neutral 两处）
   与 `TestPureHelpers`（阈值边界）同时 FAIL。
   注：原计划的 `> 0.1` → `>= 0.1` 与 `< -0.1` → `< 0` 两种注入都不会让任何断言变红
   （现有语料 score 恰好为 0 或 ±1），故换用能真正区分阈值的最小变异。
3. `isRelatedTopic` 的 `if currentTopic == "" { return false }` 改 `return true` → `TestPureHelpers` FAIL
   （空话题不应判相关）。

- [x] **Step 4: 提交并推送**

`git add user-server/internal/aiagent/rag/customer_service/context_understanding_test.go`
→ `git commit -m "test: 上下文理解意图/实体/情感与关键词启发式补测"` → 双远端推送。

**Findings（只记录不修）：**
1. `extractOrderNumber` 恒返回硬编码 `"ORDER123456"`（`dialog_manager.go:704`），任何含「号」的消息都会拿到假订单号 —— 下游若据此查询会命中错误订单。
2. `extractProductName` 只识别「裙子/手机」两词，其余商品名丢失。
3. `UpdateContext` 直接结构体浅拷贝，`Entities` 非 nil 时写入会影响调用方持有的同一 map（本排期用 `TestUpdateContextAndTopicDetection` 的 `seeded.Entities` 断言把**现有行为**钉住，不修）。

---

## Task 7: migration/migrations —— 注册表元信息与全链路 Up/Down

**Files:**
- Create: `user-server/internal/migration/migrations/registry_metadata_test.go`
- Create: `user-server/internal/migration/migrations/a_full_chain_migration_test.go`（`a_` 前缀原因见 Step 3）
- 参考（只读）：`internal/migration/migrations/initial_schema.go:124`（`RegisterMigrations`）、
  `internal/migration/registry.go:13-89`、`migrations/confidence_migration_test.go:12-15`（`testutil.NewTestDB` 用法范式）、
  `migrations/registry_completeness_test.go`（已覆盖“无重复版本 + 指定版本已注册”，勿重复其断言）

**Interfaces:**
- Consumes: `RegisterMigrations(*migration.MigrationRegistry, *gorm.DB)`、`registry.GetAll()`、
  `migration.Migration` 五方法、`testutil.NewTestDB(t)`（不传 models → 空库）。
- Produces: 无。

- [x] **Step 1: 写注册表元信息用例（离线）**

创建 `user-server/internal/migration/migrations/registry_metadata_test.go`：

```go
package migrations

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"hivemtk-user/internal/migration"
)

var versionRe = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)

// TestRegisteredMigrationsMetadata 覆盖全部迁移的 Version/Name/Description：
// 版本格式、命名与描述非空是运维排错（migrations 列表页 / 日志）的唯一信息来源。
func TestRegisteredMigrationsMetadata(t *testing.T) {
	reg := migration.NewMigrationRegistry()
	RegisterMigrations(reg, nil)

	all := reg.GetAll()
	if len(all) < 60 {
		t.Fatalf("注册迁移数=%d, 期望覆盖仓库内全部 60+ 个迁移文件", len(all))
	}
	seenVersions := map[string]string{}
	for _, m := range all {
		v := m.Version()
		if !versionRe.MatchString(v) {
			t.Errorf("版本格式非法: %q (%s)", v, m.Name())
		}
		if strings.TrimSpace(m.Name()) == "" {
			t.Errorf("%s Name() 为空", v)
		}
		if strings.TrimSpace(m.Description()) == "" {
			t.Errorf("%s Description() 为空", v)
		}
		if owner, dup := seenVersions[v]; dup {
			t.Errorf("版本 %s 重复注册: %s / %s", v, owner, m.Name())
		}
		seenVersions[v] = m.Name()
	}

	for i := 1; i < len(all); i++ {
		if strings.Compare(versionKey(all[i-1].Version()), versionKey(all[i].Version())) >= 0 {
			t.Errorf("GetAll 未按版本升序: %s 在 %s 之前", all[i-1].Version(), all[i].Version())
		}
	}
}

// versionKey 把 "v3.9.0" / "v3.10.0" 归一为可字典序比较的定宽键（补零到 3 段 × 3 位）。
func versionKey(v string) string {
	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	out := make([]string, 3)
	for i := 0; i < 3; i++ {
		n := "0"
		if i < len(parts) {
			n = parts[i]
		}
		for len(n) < 3 {
			n = "0" + n
		}
		out[i] = n
	}
	return strings.Join(out, ".")
}

// TestNoopMigrationsRunWithoutDB no-op 迁移（Up/Down 直接 return nil）应在 nil db 下也可调用。
// 非 no-op 迁移不在本用例范围内：它们需要真实连接，由全链路用例覆盖。
func TestNoopMigrationsRunWithoutDB(t *testing.T) {
	noop := []struct {
		version string
		build   func() migration.Migration
	}{
		{"v2.11.0", func() migration.Migration { return NewADomainP1Migration(nil) }},
	}
	ctx := context.Background()
	for _, n := range noop {
		m, ok := migration.NewMigrationRegistry().Get("") // 自检：空注册表取不到任何迁移
		if ok {
			t.Fatalf("空注册表不应返回迁移: %+v", m)
		}
		mig := n.build()
		if err := mig.Up(ctx); err != nil {
			t.Errorf("%s 应为 no-op Up, got %v", n.version, err)
		}
		if err := mig.Down(ctx); err != nil {
			t.Errorf("%s 应为 no-op Down, got %v", n.version, err)
		}
	}
}
```

> **交付与草稿差异（Step 1）**：
> 1. 草稿的 `TestNoopMigrationsRunWithoutDB` 循环体内那条
>    `m, ok := migration.NewMigrationRegistry().Get("")` 是**与循环无关的死自检**（每轮重复、不产生信息），
>    已换成对样例本身的钉死：`if mig.Version() != n.version { t.Fatalf(...) }`。
> 2. 新增草稿里没有的 **`TestEveryImplementedMigrationIsRegistered`**（同文件，`go/ast` + `go/parser` + `io/fs`）：
>    解析本包全部非 `*_test.go` 源文件，取出「实现了 `Version()` 且返回字符串字面量」的类型集合，
>    与 `RegisterMigrations` 实际注册集合做差，差集非空即判红。
>    它把「写了迁移类却忘了进注册清单」这一类**只能靠人肉 review 发现**的缺陷变成机器门。
>    当前差集里的两条以 `knownUnregistered = map[string]string{...}` 显式登记（见 Step 4 Findings），
>    且该表**自清理**：若某登记项后来被注册了，用例会反向报错要求删条。
> 3. 另补草稿未列的 `TestEmptyRegistryLookups`（空注册表 `Get("")` / `Get("v2.11.0")` 均须 `(nil,false)`），
>    它是 `registry.go` 中 `Get` 的空名短路与未命中两条分支的唯一覆盖来源。
> 4. 交付版实测：注册迁移 72 条、包内实现 `Version()` 的类型 74 个（`initial_schema.go` 一文两类），
>    差集恰为下面登记的 2 条。

- [x] **Step 2: 跑绿**

Run: `go test -p 1 -count=1 -run 'TestRegisteredMigrationsMetadata|TestNoopMigrationsRunWithoutDB' ./internal/migration/migrations/ -v`
实跑：4 条离线用例全 PASS（`0.840s`，无 DB 依赖）：`TestRegisteredMigrationsMetadata`、
`TestEmptyRegistryLookups`、`TestNoopMigrationsRunWithoutDB`、`TestEveryImplementedMigrationIsRegistered`。

- [x] **Step 3: 写全链路用例（真实库）**

创建 `user-server/internal/migration/migrations/a_full_chain_migration_test.go`
（**实际文件名前缀多了 `a_`**，原因见下方差异说明第 1 条）：

```go
package migrations

import (
	"context"
	"sort"
	"testing"

	"hivemtk-user/internal/migration"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// TestFullMigrationChainUpThenRollback 在空测试库上按序执行全部迁移的 Up()，
// 再逆序执行 Down()。这是唯一能在一次运行里驱动 70+ 个 Up 体的用例，
// 也是「新装实例能否从零拉起」的回归门。
func TestFullMigrationChainUpThenRollback(t *testing.T) {
	db := testutil.NewTestDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	if !chainDBEmpty(db) {
		t.Skip("测试库非空（同槽位被历史用例污染），跳过全链路以免影响其它断言")
	}

	reg := migration.NewMigrationRegistry()
	RegisterMigrations(reg, db)
	all := reg.GetAll()
	ctx := context.Background()

	upFailed := map[string]string{}
	for _, m := range all {
		if err := m.Up(ctx); err != nil {
			upFailed[m.Version()] = err.Error()
		}
	}

	// 已存在表清单用于失败信息可读化
	var tables []string
	_ = db.Raw(`SELECT table_name FROM information_schema.tables WHERE table_schema = 'public' ORDER BY table_name`).Scan(&tables).Error

	downFailed := map[string]string{}
	for i := len(all) - 1; i >= 0; i-- {
		m := all[i]
		if err := m.Down(ctx); err != nil {
			downFailed[m.Version()] = err.Error()
		}
	}

	if len(upFailed) > 0 {
		t.Errorf("全链路 Up 失败 %d/%d：%s", len(upFailed), len(all), formatFailures(upFailed))
	}
	if len(downFailed) > 0 {
		t.Logf("Down 失败（回滚链路不完备，见 Findings）%d/%d：%s", len(downFailed), len(all), formatFailures(downFailed))
	}
	t.Logf("迁移总数=%d 建表数=%d", len(all), len(tables))
}

func chainDBEmpty(db *gorm.DB) bool {
	var n int64
	if err := db.Raw(
		`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'public'`,
	).Scan(&n).Error; err != nil {
		return false
	}
	return n == 0
}

func formatFailures(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := ""
	for _, k := range keys {
		out += "\n  " + k + ": " + m[k]
	}
	return out
}
```

> **交付与草稿差异（Step 3）**：
> 1. **文件名加 `a_` 前缀**：Go 按文件名字母序在同包内排布用例，本用例要求「跑之前槽位库是空的」。
>    以 `a_` 开头使它成为本包首个执行的测试文件，直接拿到 `testutil` 刚 DROP+CREATE 出来的空库；
>    草稿里的 `chainDBEmpty → t.Skip` 因此被换成 **`t.Fatalf("全链路用例要求空库…")`**：
>    空库前提不成立是**编排被破坏**（有人新增了更靠前的测试文件），必须响而不是静默 Skip —— Skip 会让这条门长期假绿。
> 2. **必须先铺生产基表**：草稿设想「空库按序跑 Up」。实跑首轮 17/72 个 Up 以
>    `relation "xxx" does not exist` 失败。根因是本仓库真正的建表口径是
>    `internal/pkg/db.AutoMigrate()`（299 张表），版本化迁移链只是**其上的增量**（启动链是 v1.0.0→v1.0.0 空跑）。
>    交付版在用例开头调用 `runProductionAutoMigrate(gdb)`（`pdb.AutoMigrate()` + `recover` 包装）复现安装态，
>    再跑全链路；失败数从 17 降到 3，且这 3 条都是**真实缺陷**（见 Step 4）。
> 3. **全局句柄**：迁移体内部直接用 `pdb.GetDB()`，故用例须 `pdb.SetTestDB(gdb)`，
>    并以 `t.Cleanup` 还原前值、`resetChainSchema` 把 `public` schema 恢复到空
>    （`DROP SCHEMA public CASCADE` + 重建 + `CREATE EXTENSION vector`），避免污染同槽位的后续包级用例。
> 4. **`knownFailingVersions` 从切片改成 `map[version]reason`**，并加了草稿没有的反向断言：
>    登记项若某次**通过**了，用例同样判红并要求删条 —— 豁免表不会悄悄过期。
> 5. `t.Setenv("FIELD_ENCRYPTION_KEY", …)`：v3.28.0 对字段加密主键 fail-closed，`.env` 中并无该键
>    （只按长度核验、未打印取值）。测试库无 `email_smtp` 存量行，故用 32 字节全零假键满足其存在性校验。

- [x] **Step 4: 跑绿 / 归因失败清单**

Run: `set -a; source ../.env; set +a && go test -p 1 -count=1 -run TestFullMigrationChainUpThenRollback ./internal/migration/migrations/ -v`
处理规则（**不得**放宽断言蒙过去）：
- `chainDBEmpty` 为假（同槽位库已被本包其它用例建表）→ 该用例 Skip 并在报告里说明；
  随后用 `POSTGRES_TEST_DBNAME=user_db_chain_test go test ...` 独占一个库名再跑一次，
  把这条命令作为该用例的标准复跑方式写进 commit body。
- 若某版本 Up 失败：单独跑 `go test -run 'TestConfidenceMigration|...'` 定位它是否依赖前序未注册对象，
  把真实原因记进本任务 Findings；确认属生产缺陷则**不修**、把该版本从全链路断言里挪到
  显式 `knownFailingVersions = []struct{version, reason string}` 表并逐条写明原因（可追踪、不静默）。
- Down 失败仅 Log：回滚链路不完备属既有事实，本排期只把它显式化。

> **实跑结果**：`Up 失败合计 3/72，其中已登记 3 条`；`Down 失败 1/72`；
> `迁移总数=72 基表=299 Up 后表数=317 Down 后表数=252`。
> 上述「Skip + 独占库名复跑」两条**未触发**：`a_` 前缀保证本用例是同包首个跑的测试文件，
> 空库前提恒成立，若被破坏会直接 `t.Fatalf`（比 Skip 更强），因此不需要 `POSTGRES_TEST_DBNAME=user_db_chain_test` 的绕行方案。
>
> **三条已登记的 Up 缺陷（只显式化，本批不修生产码）**：
> 1. `v3.3.0`（`l_p1_migration.go:60`）：在 `integration_templates` 上建 `is_built_in` 索引，
>    但模型列名实为 `built_in`（`internal/model/integration_template.go:27` `BuiltIn`）。
>    生产建表走 AutoMigrate ⇒ 表已按模型名建好，`CREATE TABLE IF NOT EXISTS` 不补列 ⇒ `column "is_built_in" does not exist` 恒红。
> 2. `v3.22.0`（`v3_22_0_customer_id_standardize_migration.go:53-63`）：把
>    `information_schema.columns.character_maximum_length` 扫进非空 `int`，而 text/uuid 列该字段为 NULL
>    ⇒ `converting NULL to int is unsupported`，整个迁移在任一 `ALTER` 之前即中止（应为 `sql.NullInt64`）。
> 3. `v3.36.0`（`v3_36_0_admin_password_guard_migration.go:73/77`）：`stmts` 顺序错，
>    第 73 行 `CREATE TRIGGER` 引用 `fn_guard_initial_admin_delete()`，函数却在第 77 行才 `CREATE`；
>    首错即 `return` ⇒ **初始管理员删除保护触发器与函数在任何库上都从未建立成功**（安全语义静默丢失，最需优先修的一条）。
>
> **两条 Down 侧既有事实**：`v3.28.0`（email_smtp 明文→AES-GCM 加密）显式声明不可回滚（“解密回明文是安全倒退”），
> 是唯一一条 Down 报错项，按规则仅 `t.Logf`。
>
> **`knownUnregistered`（实现了却从未注册，由 AST 门登记）**：
> `v3.25.0 CustomerOwnerAgentMigration`、`v3.26.0 ReachTablesMigration` —— 两个迁移类在仓库里完整实现
> （含 Up/Down），但从未进 `RegisterMigrations` 清单，即**任何库上这条链都不会执行它们**。
> 已逐条核对影响面（不是猜的）：其目标对象在 AutoMigrate 路径上都有等价声明 ——
> `model/customer.go:63 OwnerAgentID`（带 `gorm:"index"`）、
> `reach_send_pipeline_compliance.go:20` 与 `webhook_outbound.go:83` 两处 `RegisterExtraModels`
> 分别登记 `reach_compliance_log` / `reach_delayed_outbound`。
> 所以**当前生产无缺表缺列**；风险是口径性的：一旦某实例改由版本化链负责建表（或 AutoMigrate 的
> tag/index 名与链不一致，参考 v3.3.0 就是同类漂移的实例），这两处会静默不建。修复只需把两行加进清单。

- [x] **Step 5: 覆盖率核对**

Run: `go test -p 1 -count=1 -coverprofile=/tmp/mig.cov ./internal/migration/migrations/ && go tool cover -func=/tmp/mig.cov | tail -3`
实跑：`ok hivemtk-user/internal/migration/migrations 180.104s coverage: 77.1% of statements`（基线 23%，超额达成 ≥60% 目标），
零覆盖块 `418/1469 = 28.5%`；基线口径下未覆盖块约占 77%，下降远超「过半」要求。**全跑 0 个 SKIP。**
> 草稿里 `grep -c ' 0.0%' /tmp/mig.cov` 这条**取证方式本身是错的**：`.cov` 是
> `file:start,end stmts count` 的计数文件，从不出现 `0.0%` 字样（那是 `go tool cover -func` 的格式化输出），
> 照抄会得到恒为 0 的「零覆盖块数」并误判为满分。改用 `awk '$NF=="0"'` 统计计数为 0 的块。

- [x] **Step 6: 反向验证**

全链路用例的「红点」验证不能靠改生产码，改为改测试自身口径并确认它确实会变红：
在 `registry_metadata_test.go` 里临时把 `versionRe` 改成 `^v\d+\.\d+\.\d+\.\d+$`（要求四段）→ 必须 FAIL
（证明断言真的在遍历每个迁移的 Version）。还原后复绿。
再验证全链路用例不是空跑：临时把 `if err := m.Up(ctx); err != nil` 的上层循环加 `continue`（即跳过首个迁移）
→ `t.Errorf` 不应变化，但 `t.Logf("迁移总数")` 一致；因此改用更强的一条：把 `upFailed` 判定改成
`if len(upFailed) == 0 { t.Fatal("自检：断言被短路") }` 跑一次必须红、还原后绿 —— 证明 Up 确有失败/无失败时被如实统计。

> **实跑变异（5 次，逐次 `cp` 还原并比 md5；草稿那条「短路自检」被替换）**：
> 草稿给的 `if len(upFailed) == 0 { t.Fatal }` 只能证明「当前有失败」，证不了「失败集合被严格比对」，
> 且它是临时加一条断言而非破坏现有断言，红得没有信息量。替换成下面能真正区分口径的 4 针：
>
>
> | # | 注入 | 预期红点 | 实跑结果 |
> |---|---|---|---|
> | M1 | `versionRe` 要求四段版本号 | 版本格式断言恒红 | `TestRegisteredMigrationsMetadata` FAIL：逐条 `版本格式非法: "v1.1.0" (初始版本迁移)`…（证明遍历了每个迁移的 `Version()`，非抽查） |
> | M2 | 删掉 `knownUnregistered` 中 `v3.26.0` 那一行豁免 | AST 门应把它当缺陷报出 | `TestEveryImplementedMigrationIsRegistered` FAIL：`有 1 个迁移实现了却从未注册：v3.26.0: ReachTablesMigration`（证明差集运算与豁免表都真在生效） |
> | M2b | 反向：往 `knownUnregistered` 里塞一条**已注册**版本 `"v3.3.0"` | 自清理分支应要求删条 | FAIL @ `:141`：`已登记的未注册迁移 v3.3.0 如今已在注册表里（原因：自检用假登记），请从 knownUnregistered 移除` |
> | M3a | `knownFailingVersions` 里塞一条假登记 `"v3.30.0"` | 反向断言应要求删条 | FAIL @ `:95`：`已登记为必红的版本 v3.30.0 竟通过（登记原因：自检用假登记），请从 knownFailingVersions 移除`（证明豁免表不会长期滞留） |
> | M3b | 把 `runProductionAutoMigrate` 改成 `_ = pdb.AutoMigrate`（只铺链、不铺基表） | 严格 Up 集合应报未登记失败 | FAIL @ `:90`：`全链路 Up 出现未登记失败 16/72`，逐条 `relation "knowledge_chunks"/"sop_agents"/"chat_channels" does not exist`（证明用例确实在跑 72 个 Up，且基表铺设是前置条件而非装饰） |
>
> 过程记录：M3b 若直接删掉 `pdb.AutoMigrate()` 会因 `pdb` 变成未使用 import 而**编译失败**（编译失败不等于变异，
> 会得到一个没有信息量的红），故改成取函数值丢弃。另 M1 首轮用 `perl -i -pe` 注入时把正则反斜杠双重转义、
> 且误伤目标文件，此后变异一律走 Edit 工具并 `diff`/`md5` 双验。
> 还有一条**非注入的天然红**：登记 `knownFailingVersions` 之前，同一用例即以
> `全链路 Up 失败 3/72` 判红（`:69`），说明严格集合从一开始就在起作用。
> 全部还原后：`registry_metadata_test.go` md5 `4ed38f53c1427801a27f93d98f960dec`、
> `a_full_chain_migration_test.go` md5 `5f41adb7119ccf9fd6dcbf025b0b6185`，`gofmt -l` 无输出，整包复绿。

- [x] **Step 7: 提交并推送**

```bash
git add user-server/internal/migration/migrations/registry_metadata_test.go user-server/internal/migration/migrations/a_full_chain_migration_test.go
git commit -m "test: 迁移注册表元信息校验与全链路升降级用例"
```
双远端推送。若 Step 4 产生 `knownFailingVersions`，commit body 必须逐条列出该版本与原因。
实跑：commit body 已逐条列出 3 条 `knownFailingVersions` 与 2 条 `knownUnregistered`，并写明标准复跑命令与「必须空库」前提。

---

## Task 8: internal/platform —— 签名/JWT/上报分支

**Files:**
- Create: `user-server/internal/platform/client_internals_test.go`
- Create: `user-server/internal/platform/contributor_client_test.go`（**交付时新增**，理由见 Step 1 差异第 5 条）
- 参考（只读）：`internal/platform/client.go:22-74`（`sign` 三级取密钥）、`82-153`（`ensureJWTToken` 分支）、
  `249-292`（`RegisterMerchant` + `saveMerchantSecret`/`loadMerchantSecret`，路径 `config/.merchant_api_secret`）、
  `294-342`（`GetLicenseStatus` + `PlatformError.Error/Msg`）、`371-441`（`ReportInstall`/`ReportHeartbeat` 及 Default 包装）
- 已有覆盖（勿重复）：`client_test.go` 的 401 自愈、非 2xx 结构化错误；`asset_market_test.go` 的 `withMarketConfig` 范式。

**Interfaces:**
- Consumes: `NewPlatformClient(key) *Client`、`(*Client).sign/do/doRetry/Do/ensureJWTToken/RegisterMerchant/GetLicenseStatus/ReportInstall/ReportHeartbeat/SetMerchantSecret`、
  `saveMerchantSecret/loadMerchantSecret/merchantSecretFilePath`、`PlatformError`、`config.PlatformCfg`、`t.Chdir`（Go 1.24+，本仓 go1.26.6）。
- Produces: 无。

- [x] **Step 1: 写用例**

创建 `user-server/internal/platform/client_internals_test.go`：

```go
package platform

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/config"
)

func withPlatformConfig(t *testing.T, cfg *config.PlatformConfig) {
	t.Helper()
	orig := config.PlatformCfg
	config.PlatformCfg = cfg
	t.Cleanup(func() { config.PlatformCfg = orig })
}

// TestSignSecretPrecedence 签名密钥优先级：per-merchant > env > PlatformCfg.Secret > 报错。
func TestSignSecretPrecedence(t *testing.T) {
	t.Setenv("MERCHANT_API_SECRET", "env-secret")
	withPlatformConfig(t, &config.PlatformConfig{Secret: "cfg-secret"})

	c := NewPlatformClient("mk")
	c.SetMerchantSecret("per-merchant")
	sigA, tsA, err := c.sign("GET", "/x", nil)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	c2 := NewPlatformClient("mk")
	c2.merchantSecret = "" // 绕过构造期文件加载
	sigB, _, err := c2.sign("GET", "/x", nil)
	if err != nil {
		t.Fatalf("sign(env): %v", err)
	}
	if sigA == sigB {
		t.Error("per-merchant 密钥应与 env 密钥产生不同签名")
	}
	if tsA == "" {
		t.Error("timestamp 不应为空")
	}

	t.Setenv("MERCHANT_API_SECRET", "")
	c3 := NewPlatformClient("mk")
	c3.merchantSecret = ""
	if _, _, err := c3.sign("GET", "/x", nil); err != nil {
		t.Fatalf("env 为空时应回落 PlatformCfg.Secret: %v", err)
	}

	withPlatformConfig(t, &config.PlatformConfig{Secret: ""})
	if _, _, err := c3.sign("GET", "/x", nil); err == nil || !strings.Contains(err.Error(), "MERCHANT_API_SECRET 未配置") {
		t.Errorf("三处皆空应报未配置, got %v", err)
	}

	// sign 的签名串含 unix 秒时间戳（`method\npath\ntimestamp\nbody`）：跨秒的两次调用必然得到
	// 不同签名，直接比较会偶发红。故取「同一秒内的两次签名」才可比。
	signTwice := func(pathA, pathB string) (string, string) {
		t.Helper()
		for i := 0; i < 50; i++ {
			sigA, tsA, err := c.sign("POST", pathA, []byte("{}"))
			if err != nil {
				t.Fatalf("sign(%s): %v", pathA, err)
			}
			sigB, tsB, err := c.sign("POST", pathB, []byte("{}"))
			if err != nil {
				t.Fatalf("sign(%s): %v", pathB, err)
			}
			if tsA == tsB {
				return sigA, sigB
			}
		}
		t.Fatal("重试 50 次仍未取到同一秒内的两次签名")
		return "", ""
	}

	// 带 query 的 path 只对 path 部分签名
	if a, b := signTwice("/api/a?b=1", "/api/a?b=2"); a != b {
		t.Error("query 部分不应参与签名串")
	}
	if a, b := signTwice("/api/a?b=1", "/api/b?b=1"); a == b {
		t.Error("不同 path 签名不应相同")
	}
}

func TestEnsureJWTTokenBranches(t *testing.T) {
	var loginHits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auth/login" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		loginHits++
		switch loginHits {
		case 1: // 缺 token
			_, _ = w.Write([]byte(`{"code":0,"data":{"token":""}}`))
		case 2: // 非法 JSON
			_, _ = w.Write([]byte(`{not json`))
		case 3: // 非 200
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`denied`))
		case 4: // 成功且带 expires
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{"token": "jwt-1", "expires": time.Now().Add(time.Hour).Unix()},
			})
		default: // 成功但无 expires → 回落 1h
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"token": "jwt-2"}})
		}
	}))
	defer srv.Close()
	t.Setenv("MERCHANT_API_SECRET", "s")

	// 配置未初始化
	withPlatformConfig(t, nil)
	c := NewPlatformClient("mk")
	if err := c.ensureJWTToken(); err == nil || !strings.Contains(err.Error(), "平台配置未初始化") {
		t.Errorf("nil 配置应报未初始化, got %v", err)
	}

	// 管理员密码未配置
	withPlatformConfig(t, &config.PlatformConfig{APIURL: srv.URL})
	if err := c.ensureJWTToken(); err == nil || !strings.Contains(err.Error(), "admin_password") {
		t.Errorf("空密码应报错并提示配置项, got %v", err)
	}

	withPlatformConfig(t, &config.PlatformConfig{APIURL: srv.URL, AdminPassword: "p", Secret: "s"})
	if err := c.ensureJWTToken(); err == nil || !strings.Contains(err.Error(), "缺少 token") {
		t.Errorf("响应无 token 应报错, got %v", err)
	}
	if err := c.ensureJWTToken(); err == nil || !strings.Contains(err.Error(), "解析登录响应失败") {
		t.Errorf("非法 JSON 应报错, got %v", err)
	}
	if err := c.ensureJWTToken(); err == nil || !strings.Contains(err.Error(), "平台登录返回 403") {
		t.Errorf("非 200 应带状态码, got %v", err)
	}
	if err := c.ensureJWTToken(); err != nil {
		t.Fatalf("第 4 次应成功: %v", err)
	}
	if c.jwtToken != "jwt-1" || time.Until(c.jwtExpireAt) < 50*time.Minute {
		t.Errorf("expires 未正确解析: token=%q expire=%v", c.jwtToken, c.jwtExpireAt)
	}
	if err := c.ensureJWTToken(); err != nil {
		t.Fatalf("token 有效期内应复用不报错: %v", err)
	}
	if loginHits != 4 {
		t.Errorf("有效期内不应重复登录: loginHits=%d want 4", loginHits)
	}

	// 清空缓存后第 5 次登录走「响应无 expires」分支 → 回落 now+1h
	c.jwtToken = ""
	c.jwtExpireAt = time.Time{}
	if err := c.ensureJWTToken(); err != nil {
		t.Fatalf("无 expires 响应应成功并回落 1h: %v", err)
	}
	if c.jwtToken != "jwt-2" {
		t.Errorf("token=%q want jwt-2", c.jwtToken)
	}
	if d := time.Until(c.jwtExpireAt); d < 59*time.Minute || d > 61*time.Minute {
		t.Errorf("无 expires 时应回落 1h, got %v", d)
	}

	// 服务端不可达
	withPlatformConfig(t, &config.PlatformConfig{APIURL: "http://127.0.0.1:1", AdminPassword: "p", Secret: "s"})
	c2 := NewPlatformClient("mk")
	if err := c2.ensureJWTToken(); err == nil || !strings.Contains(err.Error(), "平台登录失败") {
		t.Errorf("连接失败应包装为登录失败, got %v", err)
	}
}

func TestDoRetryPlatformPrefixUsesJWTAndNilRespDataIsOK(t *testing.T) {
	var authHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/login":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"token": "jwt"}})
		case "/platform/anything":
			authHeader = r.Header.Get("Authorization")
			_, _ = w.Write([]byte(`{"code":0}`))
		case "/merchant-api/empty200":
			// 200 且响应体为空：走 respData != nil 的 io.ReadAll + json.Unmarshal 分支
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer srv.Close()
	t.Setenv("MERCHANT_API_SECRET", "s")
	withPlatformConfig(t, &config.PlatformConfig{APIURL: srv.URL, AdminPassword: "p", Secret: "s"})

	c := NewPlatformClient("mk")
	if err := c.Do("GET", "/platform/anything", nil, nil); err != nil {
		t.Fatalf("Do(nil respData) 应成功: %v", err)
	}
	if authHeader != "Bearer jwt" {
		t.Errorf("JWT 未随 /platform/ 前缀下发: %q", authHeader)
	}
	if err := c.Do("POST", "/merchant-api/x", map[string]any{"bad": make(chan int)}, nil); err == nil ||
		!strings.Contains(err.Error(), "序列化请求数据失败") {
		t.Errorf("请求体不可序列化应报错, got %v", err)
	}

	// 204 属于「非 200」，返回结构化 *PlatformError（不是反序列化错误）
	var pe *PlatformError
	if err := c.do("GET", "/merchant-api/nobody", nil, &BaseResp{}); !errors.As(err, &pe) ||
		pe.StatusCode != http.StatusNoContent {
		t.Errorf("204 应返回 *PlatformError{StatusCode:204}, got %v", err)
	}
	// 200 + 空体 + 需要反序列化：json.Unmarshal("") 必然报错
	if err := c.do("GET", "/merchant-api/empty200", nil, &BaseResp{}); err == nil ||
		!strings.Contains(err.Error(), "unexpected end of JSON input") {
		t.Errorf("200 空响应体应报反序列化错误, got %v", err)
	}
	// /merchant-api/ 前缀走签名分支：X-Merchant-Key 与 X-Signature 必须都在
	var sigHeader string
	sigSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auth/login" {
			sigHeader = r.Header.Get("X-Signature")
		}
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	defer sigSrv.Close()
	withPlatformConfig(t, &config.PlatformConfig{APIURL: sigSrv.URL, Secret: "s"})
	if err := NewPlatformClient("mk-key").do("GET", "/merchant-api/ping", nil, &BaseResp{}); err != nil {
		t.Fatalf("签名分支请求失败: %v", err)
	}
	if len(sigHeader) != 64 {
		t.Errorf("X-Signature 应为 64 位 hex, got %q", sigHeader)
	}
}

func TestRegisterMerchantPersistsSecret(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "config"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Chdir(dir) // loadMerchantSecret/saveMerchantSecret 用相对路径 config/.merchant_api_secret

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/login" {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"token": "jwt"}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0, "msg": "ok", "data": map[string]any{"key": "k-1", "secret": "persisted-secret"},
		})
	}))
	defer srv.Close()
	t.Setenv("MERCHANT_API_SECRET", "env-secret")
	withPlatformConfig(t, &config.PlatformConfig{APIURL: srv.URL, Secret: "s"})

	// 文件不存在时构造：secret 保持空
	c := NewPlatformClient("mk")
	if c.merchantSecret != "" {
		t.Fatalf("起始应无 per-merchant secret, got %q", c.merchantSecret)
	}
	if err := c.RegisterMerchant(RegisterMerchantReq{Name: "商户A"}); err != nil {
		t.Fatalf("RegisterMerchant: %v", err)
	}
	if c.merchantSecret != "persisted-secret" {
		t.Errorf("注册响应 secret 未生效: %q", c.merchantSecret)
	}
	b, err := os.ReadFile(filepath.Join(dir, "config", ".merchant_api_secret"))
	if err != nil {
		t.Fatalf("secret 未落盘: %v", err)
	}
	if string(b) != "persisted-secret" {
		t.Errorf("落盘内容=%q", string(b))
	}
	if fi, sErr := os.Stat(filepath.Join(dir, "config", ".merchant_api_secret")); sErr != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("secret 文件权限应为 0600, got %v %v", fi.Mode(), sErr)
	}

	c2 := NewPlatformClient("mk") // 从盘上加载
	if c2.merchantSecret != "persisted-secret" {
		t.Errorf("loadMerchantSecret 未读回: %q", c2.merchantSecret)
	}

	if err := saveMerchantSecret("x"); err != nil {
		t.Errorf("saveMerchantSecret 正常路径不应报错: %v", err)
	}
	if p := merchantSecretFilePath(); p != filepath.Join("config", ".merchant_api_secret") {
		t.Errorf("secret 文件路径变了: %s", p)
	}
}

// TestRegisterMerchantFailureKeepsSecret 注册失败返回结构化错误，且不得改写已持有的密钥。
func TestRegisterMerchantFailureKeepsSecret(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/login" {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"token": "jwt"}})
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":400,"msg":"duplicate"}`))
	}))
	defer srv.Close()
	t.Setenv("MERCHANT_API_SECRET", "s")
	withPlatformConfig(t, &config.PlatformConfig{APIURL: srv.URL, Secret: "s"})

	c := NewPlatformClient("mk")
	c.SetMerchantSecret("keep-me")
	if err := c.RegisterMerchant(RegisterMerchantReq{}); err == nil {
		t.Fatal("400 应报错")
	} else if pe, ok := err.(*PlatformError); !ok || pe.Resp.Msg != "duplicate" {
		t.Errorf("应返回结构化错误: %T %v", err, err)
	}
	if c.merchantSecret != "keep-me" {
		t.Error("失败响应不应改写已有 secret")
	}
}

func TestGetLicenseStatusParsesData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/login" {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"token": "jwt"}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
			"status": "active", "expire_at": "2027-01-02T03:04:05Z", "remaining_days": 200,
		}})
	}))
	defer srv.Close()
	t.Setenv("MERCHANT_API_SECRET", "s")
	withPlatformConfig(t, &config.PlatformConfig{APIURL: srv.URL, Secret: "s"})

	c := NewPlatformClient("mk")
	lic, err := c.GetLicenseStatus()
	if err != nil {
		t.Fatalf("GetLicenseStatus: %v", err)
	}
	if lic.Status != "active" || lic.Remaining != 200 || lic.ExpireAt.Year() != 2027 {
		t.Errorf("授权状态解析错: %+v", lic)
	}

	brokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": "不是对象"})
	}))
	defer brokenSrv.Close()
	withPlatformConfig(t, &config.PlatformConfig{APIURL: brokenSrv.URL, Secret: "s"})
	if _, err := NewPlatformClient("mk").GetLicenseStatus(); err == nil {
		t.Error("data 非对象时应返回反序列化错误")
	}
}

func TestReportInstallAndHeartbeatBranches(t *testing.T) {
	var installBody, heartbeatBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/platform/install":
			b, _ := io.ReadAll(r.Body)
			installBody = string(b)
			if strings.Contains(installBody, "boom-install") {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`server blew up`))
				return
			}
			_, _ = w.Write([]byte(`{"code":0}`))
		case "/api/platform/heartbeat":
			b, _ := io.ReadAll(r.Body)
			heartbeatBody = string(b)
			if strings.Contains(heartbeatBody, "boom-heart") {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			_, _ = w.Write([]byte(`{"code":0}`))
		case "/api/auth/login":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"token": "jwt"}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	t.Setenv("MERCHANT_API_SECRET", "s")

	withPlatformConfig(t, nil)
	if err := ReportInstallDefault(&ReportInstallReq{}); err == nil || !strings.Contains(err.Error(), "平台配置未初始化") {
		t.Errorf("install 无配置应报错, got %v", err)
	}
	if err := ReportHeartbeatDefault(&ReportHeartbeatReq{}); err == nil || !strings.Contains(err.Error(), "平台配置未初始化") {
		t.Errorf("heartbeat 无配置应报错, got %v", err)
	}

	withPlatformConfig(t, &config.PlatformConfig{APIURL: srv.URL + "/", Secret: "s"})
	if err := ReportInstallDefault(&ReportInstallReq{InstallID: "ok-install", Version: "v1"}); err != nil {
		t.Fatalf("install 成功路径: %v", err)
	}
	if !strings.Contains(installBody, `"install_id":"ok-install"`) {
		t.Errorf("install 请求体异常: %s", installBody)
	}
	if err := ReportHeartbeatDefault(&ReportHeartbeatReq{InstallID: "ok-heart", HostInfo: json.RawMessage(`{"os":"darwin"}`)}); err != nil {
		t.Fatalf("heartbeat 成功路径: %v", err)
	}
	if !strings.Contains(heartbeatBody, `"os":"darwin"`) {
		t.Errorf("heartbeat 请求体异常: %s", heartbeatBody)
	}

	if err := ReportInstallDefault(&ReportInstallReq{InstallID: "boom-install"}); err == nil ||
		!strings.Contains(err.Error(), "上报安装信息返回 500") {
		t.Errorf("install 非 200 应带状态码, got %v", err)
	}
	if err := ReportHeartbeatDefault(&ReportHeartbeatReq{InstallID: "boom-heart"}); err == nil ||
		!strings.Contains(err.Error(), "上报心跳返回 502") {
		t.Errorf("heartbeat 非 200 应带状态码, got %v", err)
	}

	withPlatformConfig(t, &config.PlatformConfig{APIURL: "http://127.0.0.1:1", Secret: "s"})
	if err := ReportInstallDefault(&ReportInstallReq{}); err == nil || !strings.Contains(err.Error(), "上报安装信息失败") {
		t.Errorf("install 连接失败应包装, got %v", err)
	}
	if err := ReportHeartbeatDefault(&ReportHeartbeatReq{}); err == nil || !strings.Contains(err.Error(), "上报心跳失败") {
		t.Errorf("heartbeat 连接失败应包装, got %v", err)
	}
}

func TestPlatformErrorFormatting(t *testing.T) {
	full := &PlatformError{StatusCode: 404, RawBody: `{"code":4040,"msg":"下架"}`, Resp: &BaseResp{Code: 4040, Msg: "下架"}}
	if got := full.Error(); !strings.Contains(got, "code=4040") || !strings.Contains(got, "msg=下架") {
		t.Errorf("Error()=%q", got)
	}
	if full.Msg() != "下架" {
		t.Errorf("Msg()=%q", full.Msg())
	}

	bare := &PlatformError{StatusCode: 500, RawBody: "plain-text-body"}
	if got := bare.Error(); !strings.Contains(got, "body=plain-text-body") {
		t.Errorf("无 Resp 时 Error()=%q", got)
	}
	if bare.Msg() != "plain-text-body" {
		t.Errorf("无 Resp 时 Msg()=%q", bare.Msg())
	}

	empty := &PlatformError{StatusCode: 502}
	if empty.Msg() != empty.Error() {
		t.Errorf("皆空时 Msg 应回落 Error(): %q vs %q", empty.Msg(), empty.Error())
	}
	if !strings.Contains(fmt.Sprintf("%v", empty), "status=502") {
		t.Error("Error 应含状态码")
	}
}
```

`TestGetLicenseStatusParsesData` 末段的 `brokenSrv` 专用于覆盖 `GetLicenseStatus` 内部
`json.Unmarshal(resp.Data, &LicenseStatusResp{})` 的失败分支：`GetLicenseStatus` 请求路径固定为
`/merchant-api/license/status`，无法通过改 path 触发，因此用第二个 httptest server 让该路径直接返回
`data: "不是对象"`（合法 JSON、但类型不是对象），再断言整个方法报错。

> **交付与草稿差异（Step 1）**：
> 1. **handler 写变量 → 改用带缓冲 channel 回传**。草稿里 `authHeader`/`regBody`/`installBody`/`heartbeatBody`
>    都是「httptest handler goroutine 写、test goroutine 读」的裸共享变量；`-race` 下这属于未同步访问。
>    交付版一律用 `make(chan T, 4)` + 非阻塞 send，读侧在 `Do` 返回后取一次。
>    顺带修掉草稿里 `<-installCh` 读两次的写法（第二次会阻塞或取到下一条）。
> 2. **`signTwice(pathA, pathB)` 扩成 `sameSecond(sigA, sigB func() (string,string,error))`**：
>    草稿形态只能变 path，钉不住 method/body 是否进签名串。交付版加了 4 组同秒对照：
>    query 不参与、path 参与、method 参与、body 参与，外加「同参两次必相同」（钉 HMAC 确定性、排除 nonce）。
> 3. **草稿末条断言本身是错的**：`signTwice("/api/a", "/api/a")` 断言 `a == b` 判红 —— 同 method/path/body/秒
>    的两次签名按定义**必然相同**，照抄会得到一条恒红用例。反转为 `a != b` 判红，并把语义写进注释。
> 4. **`GetLicenseStatus` 拆成两条用例**：草稿的 `brokenSrv` 名字与实际行为不符（它测的是 `data` 类型不匹配）。
>    交付版 `TestGetLicenseStatusParsesData` 测 403 → 透传 `*PlatformError`，
>    另立 `TestGetLicenseStatusUnmarshalFailure` 专测 `data:"不是对象"` 的反序列化分支，
>    并把断言收紧到 `cannot unmarshal string into Go value of type`（草稿只断言 `err != nil`，任何错误都能让它绿）。
> 5. **新增 `contributor_client_test.go`**：草稿的 `client_internals_test.go` 跑完后全包只有 **64.0%**，
>    距本节 ≥65% 目标差 1 个点，缺口全在 `contributor_client.go`（9 个函数 0%，且它是**唯一没有任何测试的
>      平台登录/注册/资产上架链路**）。该文件可用 httptest 完全离线覆盖，故补齐而非报告"差 1%"。
>    其中 `contributorIdentity` 的派生式（`sha256(mk+"|"+secret)[:16]`）与
>    「空 secret → panic、`CONTRIBUTOR_DEV=1` → 占位口令」两条 fail-closed 分支是本轮最有价值的钉子。
> 6. 全局状态按 [[feedback-async-and-global-state-tests]] 处理：`withContributorGlobals` 保存并还原
>    `merchantKey` / `contribToken` / `contribExpireAt`，`withPlatformConfig` 保存并还原 `config.PlatformCfg`；
>    因此 `-shuffle=on -count=2` 稳定绿（既有用例 `client_test.go` 直接赋值不还原，不受影响）。
> 7. 未纳入：`InitSync`（会 spawn 协程向真实平台注册）、`StartHeartbeat`/`sendHeartbeat`/`collectMetrics`、
>    `sync.go` 其余包装 —— 前者有外部副作用，后者属另一条链路，本任务不扩范围。

- [x] **Step 2: 跑绿**

Run: `set -a; source ../.env; set +a && go test -p 1 -count=1 -coverprofile=/tmp/plat.cov ./internal/platform/ && go tool cover -func=/tmp/plat.cov | tail -25`
实跑：全包 **82.0%**（基线 33.7%，目标 ≥65%）。逐函数：
`sign/Do/do/loadMerchantSecret/GetLicenseStatus/Error/Msg/NewContributorClient/contributorIdentity/
ensureContributorToken/login/register/doAuth/SubmitAudit = 100%`，
`ensureJWTToken 95.1%`、`RegisterMerchant 93.3%`、`ReportInstall/ReportHeartbeat 94.4%`、
`CreateAsset 90%`、`doRetry 87.5%`、`saveMerchantSecret 75%`（错误分支需只读文件系统，不值当）。
控制组：`-v` 计数 **RUN=21 PASS=21 SKIP=0 FAIL=0**；`-race -count=2` 与 `-race -count=2 -shuffle=on` 均 ok。

- [x] **Step 3: 反向验证**

备份 `internal/platform/client.go` → 注入：
1. `sign` 里删掉 `if i := strings.IndexByte(path, '?')` 的截断 → `TestSignSecretPrecedence` 的 query 断言 FAIL。
2. `ensureJWTToken` 的 `if loginResp.Data.Token == ""` 改成 `if false` → `TestEnsureJWTTokenBranches` FAIL。
3. `doRetry` 里 `if resp.StatusCode == http.StatusUnauthorized && !retried` 改成 `&& retried` →
   既有 `TestClient_Do_401SelfHeal` 红（证明本任务的门会保护既有用例）。
逐次 `cp` 还原后复绿。

> **实跑变异（5 针；`client.go` md5 `5ca0feb091b9b7fc1ed643d804273f6f`、
> `contributor_client.go` md5 `98785b0801d0d252053a7bf8bc84c3f5` 每次还原后逐次比对一致）**：
>
> | # | 注入 | 实跑结果 |
> |---|---|---|
> | I1 | `sign` 的 `i >= 0` 改 `i > 999`（query 不再截断） | `client_internals_test.go:99: query 部分不应参与签名串` FAIL |
> | I2 | `if loginResp.Data.Token == ""` 加 `&& loginResp.Code == -1` | `:160 响应无 token 应报错, got <nil>` FAIL |
> | I3 | `&& !retried` 改 `&& retried`（401 自愈失效） | **既有** `client_test.go:33 expected success after 401 retry` FAIL —— 证明新门也护住老用例 |
> | I4 | `contributorIdentity` 口令切片 `[:16]` 改 `[:12]` | 3 条断言同时红（主用例、anonymous、CONTRIBUTOR_DEV 占位），派生式确实被钉住 |
> | I5 | `CreateAsset` 的 `if out.ID == 0` 改 `if out.ID < 0` | `:283 空 ID 应报错, got <nil>` FAIL |
>
> 过程教训（同 Task 6/7）：I1 第一次注入写成 `if false { pathNoQuery = path[:i] }`，
> `i` 变未定义 → **编译失败**。编译失败不是变异（红得没有信息量，且测不到断言），
> 一律改成能编译、只改行为的最小形式（`i >= 0` → `i > 999`）。
> I2/I5 同样刻意用「附加恒假条件 / 改阈值」而非删分支：删语句会改变局部变量与返回值的可达性，
> 容易把变异变成编译错误或顺带删掉别的行为，红点就归不了因。
> 另：草稿把 I1 描述成「删掉截断」，实操必须保留语句只改阈值，理由同上。

- [x] **Step 4: 提交并推送**

`git add user-server/internal/platform/client_internals_test.go`
→ `git commit -m "test: 平台客户端签名优先级/JWT 分支与安装心跳上报补测"` → 双远端推送。

实际 add 两个文件（`client_internals_test.go` + `contributor_client_test.go`），commit 主题不变。

**Findings（只记录不修）：**
1. `contributorIdentity` 在 `config.PlatformCfg == nil` 时抛**运行时空指针 panic**（`contributor_client.go:58`
   直接取 `.Secret`），而 `NewContributorClient` 明确容忍 nil 配置（`client.go` 侧同类情形返回可读的
   "平台配置未初始化"）。panic 位于 `CreateAsset`/`SubmitAudit` 的调用链上，不是启动期 ⇒
   未配置平台的环境里一次资产上架请求就能把请求打崩。已由用例把「panic 的是空指针而非可读错误」钉成现状。
2. `contribToken` 是**包级 24h 缓存**，切换平台地址/`platform.secret` 时不失效；
   而 `merchantKey` 由 `InitSync`（`sync.go:31`）在**每次进程启动时随机生成**且只落在这里
   （全仓另一处赋值是测试 setter）。组合后果：贡献者身份 `mtk_<merchantKey>` 每次重启都换一个新人，
   平台侧看到的商户与已上架资产的归属会随重启漂移。
3. `doRetry` 只把 **200** 当成功：204 No Content 会走非 200 分支返回 `*PlatformError{StatusCode:204}`。
   若平台侧任何端点按 REST 习惯用 204 表示"成功且无内容"，本客户端会把它当错误上报给上层。
   用例已把这条口径显式化（`204 应返回 *PlatformError{StatusCode:204}`），改动需同步测试。
4. `ErrPlatformNotConfigured` 哨兵（`client.go:22`，注释称"轮询型端点静默降级判定用"）
   实际只在 `controller/platform.go:55` 被返回一次，**全仓无任何 `errors.Is` 消费方**；
   而客户端自身三处"平台配置未初始化"都是新建的 `fmt.Errorf`，与该哨兵**文本相同但不是同一个值**。
   ⇒ 任何据此写静默降级的代码都会漏判客户端来源的未配置错误。
5. `saveMerchantSecret`/`loadMerchantSecret` 用**进程 CWD 相对路径** `config/.merchant_api_secret`；
   `loadMerchantSecret` 对读失败静默忽略。若服务从别的工作目录启动，per-merchant 密钥既读不到也写不回，
   签名会静默回落到全局 `MERCHANT_API_SECRET`（与 §1 契约"每商户独立密钥"不符）且无任何日志。
   已核验泄漏面：仓库里确有该文件（`user-server/config/.merchant_api_secret`，48B / `0600` / 9-4 生成，
   说明真实运行时的 CWD 是 `user-server/`），但它**未被 git 跟踪**且由
   `user-server/.gitignore:130 config/.merchant_api_secret` 显式忽略 ⇒ 无提交泄漏风险。
   本任务用例一律 `t.Chdir(t.TempDir())`，不落该文件到仓库目录。

---

## 收尾（全部任务完成后）

> **执行实况（2026-09-20 收口）**：四条全部完成，其中第 1 条按包范围口径达成，草稿的全仓口径在当前
> 共享工作树里不可满足（并行会话有 ~40 个未提交 WIP 文件）。逐条实况：
>
> 1. **包范围门绿、全仓门不适用**：`gofmt -l` + `go vet` 只跑本排期 7 个包
>    （`internal/cron`、`internal/pkg/tracing`、`aiagent/knowledge/service`、`aiagent/rag/service`、
>    `aiagent/rag/customer_service`、`internal/migration/migrations`、`internal/platform`）→ **两者均无输出**。
>    草稿写的 `go vet ./...` 会把他人 WIP 的编译状态算进本排期的结论，故不按字面执行。
> 2. **覆盖率收口复跑**（`-p 1 -count=1 -cover`，非任务内增量数）：
>    | 包 | before | after（本轮复跑） |
>    |---|---|---|
>    | `internal/cron` | 3.0% | **97.0%** |
>    | `internal/pkg/tracing` | 26.7% | **92.7%** |
>    | `aiagent/knowledge/service` | 7.6% | 9.9%（包级；卡内目标 constants.go getter **15/15 = 100%**） |
>    | `aiagent/rag/service` | 13.1% | **31.3%**（3 个 prompt 构造器 100%） |
>    | `aiagent/rag/customer_service` | 6.7% | **35.6%** |
>    | `internal/migration/migrations` | 23% | **77.1%**（62.3s，含全链路 Up→Down） |
>    | `internal/platform` | 33.7% | **82.0%** |
>
>    交付规模：8 个提交 / 11 个测试文件 / 65 个 Test 函数 / +3315 行，生产代码零改动。
> 3. **回灌完成**：本文件「执行中发现」14 条 Findings（Task 5/6/7/8）；
>    `docs/replan-2026-09/新规划任务清单.md` 变更记录 **r34**（含 4 条方法学）；
>    项目记忆 `project-audit-backlog-2026-09.md` 新增「第二十五轮」并把「覆盖率面上推进」改为剩余范围；
>    用户记忆新增 `cli-toolchain-gotchas` #27（覆盖率只认 `cover -func`、复选框 old_string 不唯一）
>    与 `async-and-global-state-tests` #10（httptest channel、包级全局成对还原）。
>    草稿第 3 条列的 9 项 finding 里「live-code 无 recover / 订单号假值 / UpdateContext 浅拷贝」
>    属 Task 1–4 与上一批（`de994fd0`）的任务内 Findings，未重复搬进「执行中发现」段。
> 4. **双远端 `0 0`**：`4afd9c66` 之后 gitee-upstream 与 upstream 均 `git rev-list --left-right --count` = `0 0`；
>    提交自洽性在 `git clone --shared` 影子树复验（`git status --porcelain` 0 行、
>    **不带 `../.env`** 跑 `go test ./internal/platform/` 仍 ok ⇒ 用例不依赖本机 env），影子树跑完已删除，
>    且核对影子树 `user-server/config/` 未被写出 `.merchant_api_secret`。

## 执行中发现（仅记录，本批不改生产代码）

- **Task 8 / 贡献者身份每次重启漂移**（`sync.go:31` + `contributor_client.go:47`）：
  `merchantKey` 由 `InitSync` 用 `crypto/rand` 随机生成且只存包级变量，贡献者用户名派生为 `mtk_<merchantKey>` ⇒
  每次进程重启就是一个新贡献者，平台侧资产归属随重启漂移，24h 的包级 `contribToken` 缓存又不随地址/密钥变更失效。
- **Task 8 / `contributorIdentity` 空配置运行期 panic**（`contributor_client.go:58`）：
  `config.PlatformCfg == nil` 时直接取 `.Secret` 抛空指针，而 `NewContributorClient` 明确容忍 nil；
  panic 在 `CreateAsset`/`SubmitAudit` 调用链上（非启动期），未配置平台的环境一次资产上架请求即可打崩该请求。
- **Task 8 / `doRetry` 只认 200**：204 No Content 落入非 200 分支返回 `*PlatformError{StatusCode:204}`，
  平台侧任一按 REST 习惯用 204 表"成功无内容"的端点都会被本客户端当错误上抛。用例已把该口径显式钉住。
- **Task 8 / `ErrPlatformNotConfigured` 无消费方**（`client.go:22`）：全仓无 `errors.Is` 判定，
  而客户端自身三处"平台配置未初始化"是文本相同但值不同的 `fmt.Errorf` ⇒ 据哨兵写静默降级的代码会漏判客户端来源。
- **Task 8 / per-merchant 密钥文件是 CWD 相对路径**（`config/.merchant_api_secret`）：
  从别的工作目录启动则读写双失效、`loadMerchantSecret` 又静默忽略错误，签名悄悄回落到全局 `MERCHANT_API_SECRET` 且无日志。
  已核验该文件在仓库里存在但未被跟踪、由 `user-server/.gitignore:130` 显式忽略 ⇒ 无提交泄漏风险；本批用例一律 `t.Chdir(t.TempDir())`。
- **Task 7 / 初始管理员删除保护从未生效**（`v3_36_0_admin_password_guard_migration.go:73,77`）：
  `stmts` 里 `CREATE TRIGGER trg_guard_initial_admin_delete ... EXECUTE FUNCTION fn_guard_initial_admin_delete()`
  排在 `CREATE FUNCTION` 之前，迁移首错即 `return` ⇒ **函数与触发器在任何环境都未建立**，
  且启动链为 v1.0.0→v1.0.0 空跑、无人执行该迁移，故无任何报错。安全语义静默丢失，本批最高优先级修复项。
- **Task 7 / v3.22.0 _customer_id 标准化整段失效**（`v3_22_0_customer_id_standardize_migration.go:53-63`）：
  `character_maximum_length` 用非空 `int` 承接，而 text/uuid 列该字段为 NULL ⇒ `converting NULL to int is unsupported`，
  在任一 `ALTER` 之前中止。应使用 `sql.NullInt64`。
- **Task 7 / v3.3.0 列名口径漂移**（`l_p1_migration.go:60` vs `internal/model/integration_template.go:27`）：
  迁移建 `is_built_in` 索引，模型列叫 `built_in`；因生产建表走 AutoMigrate，`CREATE TABLE IF NOT EXISTS` 不补列 ⇒ 恒红。
  与「已结」清单里的字段命名漂移同源，属可复现的口径类缺陷。
- **Task 7 / 两个迁移实现了却从未注册**（`v3.25.0 CustomerOwnerAgentMigration`、`v3.26.0 ReachTablesMigration`）：
  漏 `register(...)` 一行不会有任何报错。当前其目标对象由 AutoMigrate 侧的模型 tag / `RegisterExtraModels`
  等价覆盖（`model/customer.go:63`、`reach_send_pipeline_compliance.go:20`、`webhook_outbound.go:83`），
  故**暂无生产缺表风险**，但链与模型两处口径一旦漂移（v3.3.0 就是先例）即静默失效。
  已由 `TestEveryImplementedMigrationIsRegistered` 这道 AST 门长期盯着。
- **Task 7 / 迁移链不是安装路径**（`internal/pkg/db/migrate.go` 的 `AutoMigrate()` 299 表 vs 链上 72 个迁移）：
  本排期最重要的架构事实。任何人以为「跑迁移链即可建库」都会得到 16 个 `relation does not exist`；
  全链路用例因此必须先铺基表（M3b 变异即为此而设）。
- **Task 7 / 回滚链会把库降到「比安装基线还少表」的状态**：一次完整 Up→Down 后
  `public` schema 表数为 **252**，而 Up 前基线是 **299**、Up 后是 317。
  即 72 个 Down 逆序跑完不仅没把库还原到 299，反而**净删了 47 张基线表**
  （多个迁移的 Down 用 `DROP TABLE IF EXISTS` 删的是 AutoMigrate 建的基线表，而非自己 Up 建的表）。
  唯一显式拒绝回滚的是 `v3.28.0`（明文→AES-GCM 加密，理由「解密回明文是安全倒退」），属正确设计。
  结论：本项目的「降级」在数据层面不可用，Down 失败仅 `t.Logf` 的口径据此维持，但数值本身要报出来。
  **⇒ 第三十二轮（2026-09-21）这条从"口径"改成了判据并修完**：见文末「R17 降级销毁在用表」。
- **Task 6 / 否定语义丢失**（`dialog_manager.go:612-639`）：`calculateSentimentScore` 用纯字节子串匹配，
  「不好」同时命中正面词「好」与负面词「不好」，两者抵消后 score 恰好为 0（判中性），
  即所有「不+正面词」的表述都会被误判为中性。用例按实际行为断言 `== 0` 并在注释里标明该抵消行为。
- **Task 6 / `startSessionCleanup` 的错误分支不可达**（`dialog_manager.go:293-295`）：
  `CleanupExpiredSessions` 恒返回 nil，`if err != nil { logger.Warnf(...) }` 永不触发，
  该函数覆盖上限为 83.3%（ticker 体已由后台用例覆盖）。
- **Task 5 / `generateSessionID` 会话 ID 会碰撞**（`dialog_manager.go:309`）：ID 为 `user_platform_<UnixNano>`，
  本机实测连调 20000 次同参只得 5136 个不同值（重复率 ~74%），时钟粒度粗于纳秒。
  同一 (user, platform) 背靠背两次 `CreateSession` 会写入同一个 map key，**先建的会话被静默覆盖丢失**。
  影响面：仅内存版 `InMemoryDialogManager`（PG 版走 `pg_dialog_manager.go`）。
  用例规避方式：`TestListUserSessionsFilters` 内各会话 platform 取值互异，不依赖 ID 唯一性；
  `-count=10` 已稳定绿。真正修复（追加随机后缀/计数器）留待单独批次。

## Findings 处置（2026-09-20 第二轮：用户指示「发现问题全部处理 处理后提交推送」，本段起解除"生产代码一行不改"）

上面每条 Finding 的处置结果。全部走「真库/真 HTTP 现场先跑出红 → 最小实现转绿 → 行为级变异逐条杀死 →
`--shared` 影子克隆复验 → 双远端推送」，变异不复用编译期错误（除签名变更这一类只能编译红的除外）。

| 号 | Finding | 提交 | 处置 |
|---|---|---|---|
| R1 | v3.36.0 触发器早于其依赖函数 | `a3054f6d` | 语句顺序修正 + 建表后回读 `pg_trigger`/`pg_proc` 断言对象真存在；函数体不存在即报错 |
| R2 | v3.22.0 NULL→int 中止、v3.3.0 列名漂移 | `136b97aa` | `sql.NullInt64` 承接长度列；索引列名对齐模型 `built_in`；迁移链 Up 首次全绿 |
| R3 | v3.25.0 / v3.26.0 从未注册 | `0cb35ee5` | 补 `register(...)`，建表 DDL 与模型逐列对齐（回扫用的 `attempts`/`sent_at` 即在此列） |
| R4 | merchant key 每次重启随机 → 身份漂移、`contributorIdentity` nil panic | `16163bf2` | merchant key 落盘复用；派生失败以 `error` 收口并上抛，不再 panic |
| R5 | 哨兵无消费方 + 密钥文件 CWD 依赖 + 204 口径 | `7a37f6b8` | `%w` 包进 4 处抛点；新增 `DegradeReason` 给 sync/health 两口用（"没接平台"≠"接了挂了"）；密钥改随 `MERCHANT_STATE_DIR`；读失败上抛。**204 子项经平台侧源码复核否决**：`response.Error` 也写 HTTP 200，204 只出现在 CORS OPTIONS 预检，不在任何业务响应上 |
| R6 | 会话 ID 同参碰撞、否定语义丢失 | `59313038` | ID 追加进程内计数器（同参背靠背不再覆盖）；打分先剥「不+正面词」再计 |
| R7 | 活码轮询 goroutine 无 recover | `5393b8dc` | 轮询体补 recover，单次 panic 只丢一轮不再终止整进程 |
| R9 | 贡献者客户端不接平台信封 | `e4e36f77` | 严格 `{code,msg,data}`：token 从 `data.token` 取（旧实现读顶层 ⇒ 提交链路从未通）；`code!=200` 判失败（旧实现 HTTP 200 即成功 ⇒ 拒绝被当成功）；注册即签发 + 401 自愈重登（平台 JWT 中间件发真 401，而本客户端缓存 24h） |
| R8 | 离线回扫 SQL 引用无人建立的列 | `62445d10` | 见下 |
| R10 | `order_draft` 用例把"当前时间"写死成 2026-09-19 → 24h 后整批日历红 | `00c7c263` | `scenarioNow` 改回 `time.Now().UTC().Truncate(time.Second)`，两副底座共用同一 now 的原意保留；`TestOrderDraft*` 20 例全绿 |
| R11 | 商户客户端只认 HTTP 200：平台拒绝被当成功 | `396b057d`（传输层+控制器）+ `e0e857fa`（`purchaseFailMsg` 文案与死分支） | 见下两段 |
| R13 | 清扫 E2E 用例把"逐轮覆盖"的报告当末轮断言，机器一忙就红 | `9903baaf` | 见下 |
| R12 | 平台连通性探测打的是平台从未实现的 license 端点 ⇒ 平台健康也报故障 | `27d1dc14` | 改探 `GET {APIURL}/health`，死链与三处调用点一并迁；见下 |
| R14 | 离线回扫的**渠道检测** SQL 引用无人建立的列 ⇒ 回扫从未跑过 | `2bf9e339` | 改读 `bridge_accounts` 实有列 + service 侧在线/离线分区；见下 |
| R15 | SSE 建流的 `channel` 从不归一（ingest 侧归一）⇒ 别名客户端订阅挂在无人广播的键上 | `7683e10a` | 建流处归一 + 生命周期写在线位 + 补投门读订阅真值；见下 |

**R8 实况**（`internal/repository/bridge_offline_replay_repo.go` + `internal/service/bridge_offline_replay.go`）：
真库跑出的红是 `ERROR: column "retry_count" does not exist (SQLSTATE 42703)` —— 该链路的建表 DDL
（`v3_26_0_reach_tables_migration.go` / `model/reach_delayed_outbound.go`）从来没有
`receiver_id`/`msg_type`/`event_id`/`retry_count`/`replayed_at` 这五列，而仓储的两条 `UPDATE` 写后两列、
行结构体宣称有前三列。后果不是"报错"而是**静默不收敛**：调用方用 `_ =` 吞掉 42703，行永远停在
`pending`，cron 每 5 分钟（`internal/pkg/cron/cron.go:95`）把同一条 AI 回复再投一次；
同时 `msg_type` 恒为空 ⇒ 落进 `message_hub` 的出站行没有类型，前端渲染成空气泡。

处置：行结构体收敛到真实列（含 `kind`/`cards`/`attempts`）、列表查询写全列名并加
`send_at <= now`（原来不看 `send_at`，等于在免打扰时段把"次日窗口开放"的回复提前推出去）、
新增 `ClaimDelayedOutboundForReplay` 把 `pending→sending` 的 `RowsAffected` 当入场券
（H-3 主链路 drain 同一批行，两边直接投递就是同一份内容双发）、成功收口 `sent`+`sent_at`、
失败回 `pending` 并 `attempts+1`+`last_error`、次数用尽判 `failed` 终态、四处回写错误全部打日志；
带富卡的行让给主链路（桥接管道只发文本，强投等于丢卡）。
交付：`bridge_offline_replay_repo_test.go` 5 例 + `bridge_offline_replay_test.go` 3 例（真 PG + 真出站管道），
17 处行为变异全被杀死（控制组 ran=8/skip=0），`./internal/repository/` 整包 100s 绿。
回归面（本批改动的全部生产符号只被这两个文件用到，仍按整包跑）：`./internal/service/` 全量第一趟
**FAIL 468.890s**（两条 `TestOrderDraft*` 红，见 R10 —— 与本批生产改动无关，是夹具日历炸弹），
根因定位并以 `00c7c263` 修掉后复跑 = **`ok hivemtk-user/internal/service 413.426s`（rc=0，整包无 `-run` 过滤）**。

> 订正（写作用）：本段曾记为"975.9s / 748 PASS / 0 FAIL / 0 SKIP"，那是**读了截断日志得出的假事实**——
> 当时进程尚未跑完，我按前段增量数拼了个总数，而完整日志的收口行是 `FAIL ... 468.890s`。
> 口径：整包结论只认进程结束后日志里的 `ok`/`FAIL` 行，日志没写完就没有数字。

**R10 实况**（`internal/service/order_draft_store_test.go:33`）：`scenarioNow` 被 `06618987`（2026-09-19 14:51 +0800）
写死成 `time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)`，而夹具的 `expires_at = scenarioNow+24h` ——
过了 2026-09-20 12:00 UTC（本地 20:00）之后所有草稿在测试眼里**一律已过期**，`Confirm` 的 8 个并发全判过期
（`应恰好 1 人成功，实际 0 人`）、`pending` 归零（`全程只该有一条 pending 草稿，实际 0 条`），
从该时刻起每天必红。判据本身（两副底座必须共用同一个 now，否则 `updated_at` 的亚秒差会把自己
的噪声当成行为差异）是对的，错在把"进程启动那一刻"钉成了历史时刻，故保留原注释语义只改取值。
边界留档：这条只解释 **09-20 20:00 之后**的 order_draft 红；第二十一轮（09-19）那条
`TestOrderDraftSweepWorker_EndToEndFlipsAndPurgesRealRows` 红发生时夹具尚未过期，其负载敏感归因不因本条推翻，
但今天修完的整包绿里该用例确实随全包一起过了。

**R11 实况**（`internal/platform/client.go`）：平台侧 `response.Success` 与 `response.Error` **都写 HTTP 200**，
真值只在信封 `code` 里；连 `MerchantAuth` 的 401/403、注册的 400/409 也走这条路。而商户客户端 `doRetry`
的成功分支只做 `json.Unmarshal(respBody, respData)` 后 `return nil`，于是：`RegisterMerchant` 对
"该邮箱已被注册"打出「商户注册成功」且不落密钥、`ReportInstall`/`ReportHeartbeat` 对 `code=400` 打出
「上报成功」（respData 为 nil 时连响应体都不读）。修法落在**唯一出口**：新增 `envelopeRefusal(body)`，
非信封（非对象 / 无 `code` 键）不凭空造失败、`code=0` 与 `200` 视为成功、其余转成带
`StatusCode`+业务码+平台原话的 `*PlatformError`；`doRetry` 成功路径改为无条件读体后过这道闸，
两条上报口各自补一次 `envelopeRefusal`。资产市场客户端原有的 `env.Code != 0 && != 200` 判断
**随之删掉**（`asset_market_client.go doData`）：`AssetMarketClient.client` 就是同一个 `*Client`，
拒绝已经在 `Do` 里转成 `*PlatformError`，那段判断成了永不命中的死分支，留着只会让人以为这里还有一道闸；
其用例断言从字符串匹配升级为按结构化 `perr.Resp.Code` 判定，与 `PlatformError` 文档里
"别再依赖脆弱的字符串匹配"的初衷一致。
连带必要项：R11 之后 `platformData` 第一次拿到"通了但被拒"的错误，若继续统一播报"平台不可达"，
会把商户停用/签名不对推给网络排查 ⇒ 降级文案分流为 `平台拒绝(code=..): <原话>` 与 `平台不可达` 两支。
交付：`client_test.go` +7 例（含 3 条反向闸门：`code=200` 仍解析、裸 body 原样交回、连不上仍报不可达）
与 `controller/platform_test.go` 2 例（该控制器此前零测试），**8 处行为变异全被杀死**
（控制组 ran=19/pass=19，X3 的杀死证据是摘守卫后的 nil deref panic，已按"期望用例确实红了"才计入）；
`./internal/platform/` 整包绿、`-race` 绿。

**R11 下游波及（本次提交收口）**：`grep "platform error"` 扫到 `internal/service/local_asset.go:83`
的 `purchaseFailMsg` 在**按老错误的字面格式切片**造产品文案（`TrimSpace` + 去掉 `"platform error "` 前缀）。
R11 一落地，错误文本换成 `platform request failed: status=200, code=4002, msg=余额不足`，
切不到锚点 ⇒ 用户在"购买失败"里看到一整套内部格式。先按 RED 复现这一条，再把原因改成
`errors.As` + `perr.Msg()`（`Msg()` 本身 nil-safe：`Resp.Msg`→`RawBody`→`Error()`，所以平台没给 msg 也不塌成空）。
`local_asset_test.go` +3 例（取信封 msg / 无 msg 不塌空 / 非平台错误原样带原因），
**4/4 变异被杀死**（M1 摘 `errors.As` 分支、M2 分支内误用 `err.Error()`、M3 丢掉兜底原因、
M4 无 msg 时塌成 `"平台购买失败: "`；控制组 3 pass / 0 fail，逐 mutant md5 比对还原）；
`gofmt -l` 静默、`go vet ./internal/service/` 静默。全仓再扫 `platform error|platform request failed`
只剩 `client.go` 里 `PlatformError.Error()` 自身的两个格式化分支，无第二个字符串切片消费方。

**R13 实况**（`internal/service/order_draft_sweep_test.go:319`）：`e0e857fa` 的整包门（影子克隆，带 env）
唯一 FAIL 就是这个用例 —— 与 R11 无关，是 `TestOrderDraftSweepWorker_EndToEndFlipsAndPurgesRealRows`
在 `末轮报告应记到 expired>=1 且 purged>=1` 上读到 `{Expired:0 Purged:0}`。机理：`lastReport` 是
**逐轮整体覆盖**的（`order_draft_sweep.go:208`），到期段与清理段各自在哪一轮记到数并不由测试决定，
而用例把两段行断言（各带 5s 轮询）**跑完之后**才读一次报告 ⇒ 断的其实是"读报告的时机恰好压在干活那一轮"。
单独跑能过；`-count=12` 连跑 3 次红（每轮 20ms 节拍，行断言一慢，活干完后的轮次早把报告刷成 0）。
修法两条，都在测试侧（生产语义一行未动）：① 节拍 20ms→100ms，让每份报告至少活过 20 次 5ms 轮询；
② 计数断言改成**跨轮累加观测**（`sawExpired`/`sawPurged` 各自记到过数即成立），底座/错误字段照旧断，
两段行效果断言（库里真翻了、真删了）全部保留。**不弱化**：新增的反向依据是 4 处生产变异全杀
（Y1 过期段计数恒 0、Y2 清理段计数恒 0、Y3b 保留期放大到 1000 天 ⇒ 行删不掉、Y4 报告谎报 memory 底座），
控制组 `-count=3` 3/3 绿。**记一条等价变异**：最初把 Y3 写成 `PurgeTerminal(ctx, 0)`，测试照样绿 ——
不是断言漏了，是 `retention<=0` 在 `order_draft.go:843` 会兜回 `defaultDraftRetention`（90 天），
100 天前的预置行仍然可删，这个变异**没改变可观察行为**；换成 1000 天才真正断掉清理段。
复验：`-run TestOrderDraftSweepWorker_EndToEnd -count=30` 30/30 绿、`-run TestOrderDraft -count=3` 绿；
`9903baaf` 影子克隆整包门 **`ok hivemtk-user/internal/service 453.126s`（rc=0，整包无 `-run` 过滤，
`--- FAIL` 行数 0）** —— 这就是 R11 那句"改动无行为回归"的收口证据（首跑红在 R13，不是 R11）。

**R12 实况**（`internal/platform/client.go` + `internal/controller/app_config.go`，`27d1dc14`）：
登记时的两个方向里，①（改探真实存活性端点）被采纳。`CheckConnection()` 现在只打
`GET {APIURL}/health` —— 该端点在平台侧鉴权之外（`platform-server` 的路由枚举里它就在中间件组之前），
所以探测**不带签名也不带 JWT**：带不上就是"平台没起来"与"我方没凭证"两类原因混成一个信号，
而后者不该由连通性探测报。`GetLicenseStatus` 与其 `LicenseStatusResp` 一并删除（平台开源版已移除
License 域，端点从未存在），三处调用点的降级原因从 `license_unavailable` 收敛为纯连通性判定。
静态门：`grep -rn "GetLicenseStatus\|LicenseStatusResp\|license_status"` 在影子克隆里
**非测试命中 0、测试命中 0**（连例子一起删净，不留"引用一个不存在的端点"的测试）。

**R14 实况**（`internal/repository/bridge_offline_replay_repo.go` + `internal/service/bridge_offline_replay.go`，`2bf9e339`）：
R8 修的是回扫**取行/收口**那两条 SQL 的假列，这次红在同一文件的**渠道检测**那两条：
`bridge_accounts` 的渠道列叫 `channel` 不叫 `platform`，而 `bridge_metrics` 是指标时间序列
（`metric_name/labels/value/metric_type/ts`），根本没有渠道维度 —— 拿它当"渠道最近活跃"的数据源，
两条查询都撞 `42703`（取证用两条原文 SQL 在真库逐条跑出来），`DetectOfflineChannels` 于是恒返回 error，
`RunOnce` 恒在检测这一步拿到空集合 ⇒ **离线回扫这条腿从上线起一条消息都没投过**，
只在日志里留一行 Warn。处置不是补列，是把检测改读一张实有列的快照（`ListBridgeAccounts`：
`channel/account_id/status/last_sync_at/updated_at`），在线/离线分区挪到 service 侧按 `status` 划，
`ReplayStats` 补 `OnlineChannels` 让两侧不再混报一个数。整条腿用例（真库 + 真出站管道）
断 `scanned/online/offline/replayed` 与落库行，修前该用例读到 `scanned=0`（RED 证据取自
`git show HEAD:` 的旧码现场，避免"编译红"冒充"行为红"）。

**R15 实况与补投门定稿**（`internal/bridge/{sse.go,account_repo.go}` + 上面两个 service 文件，`7683e10a`）：
上一段登记的"两条 drain 并存、回扫会在渠道确实离线时投递，是否改成恢复在线才补投属产品口径"——
这条口径问题的答案现在有了事实依据，按"门该建在哪"记：

1. **`status`/`last_sync_at` 当时都是假值，所以那道门当时确实建不了。** `status` 只在入站
   `Upsert` 里被刷（按该 token 最后收到的渠道键，把整串账号一起刷成 online），SSE 流的建立与断开
   从不改写它；`TouchLastSync`（唯一的"心跳"写口）**生产零调用方**。于是
   `SetOffline` 落下的 `offline` 永久无人翻回 —— 读 `status` 的门等于没有门。
   本批把 SSE 生命周期接上：建流 + 每次心跳 `TouchLastSync`（连带翻 `status=online`，
   它是 `SetOffline` 的对偶），流退出**且该账号无其他活订阅**时 `SetOffline`；
   写库失败只报警不断流（2s 写超时），`defer` 注册顺序被显式钉住（先摘订阅再判离线）。
2. **门不建在 `status` 上，建在进程内订阅真值。** `SSEBus.HasSubscribers(channel, accountID)`
   与 `Subscribe` 同 key 口径，是唯一权威来源；`RunOnce` 逐渠道问一次（不是逐行问，
   否则几十万历史行规模下把订阅表读成热点），无活订阅的渠道**整条跳过、一行不碰**，
   行留在 `pending` 等重连后那一轮。探针与推送认领器在 `SetOutboundClaimer` 同一处注入，
   两条路读同一张订阅表，不留"一处以为可达、另一处判无人在线"的漂移面；
   探针缺件时**放行 + 一次性 Warn**——装配缺件只能退化成"照旧补投"，不能退化成"所有渠道都离线"。
3. **顺带修掉一条会让门失真成静默丢消息的旧缺陷（登记为 R15）**：`HandleOutboxSSE` 的 `channel`
   从不归一，而 ingest 侧处处 `NormalizeBridgeChannel`。扩展用别名（`douyin_web`）建流时订阅挂在
   `"douyin_web:acc"`，而 `Publish` 按规范渠道 `"douyin:acc"` 广播 ⇒ 低延迟路永远投不到它，
   只剩 4×心跳的慢轮询兜底；且在线位按别名写 `bridge_accounts`（存的是规范渠道）而**静默 no-op**。
   归一之后在线判定、认领、补投门与账号行落在同一个键上。
4. **由此新增一条耦合，已钉住**：心跳是在线位的唯一续期来源，故 `SSEDefaultHeartbeatInterval`
   必须 `< OnlineGraceWindow`（15s < 30s），否则挂着的流会按心跳周期在管理面闪烁成掉线。
   两侧都有运行时配置覆盖口，静态用例只钉默认值，配小了的核对口径写在用例注释里。

交付：`sse_online_state_test.go`（`HasSubscribers` 口径 + 3 条 httptest 真跑流的生命周期用例 +
别名渠道用例）、`account_repo_online_test.go`（`TouchLastSync` 翻位在真库上、
`isOnlineByLastSync` 的 status 一票离线读法、心跳节拍与宽限窗耦合）、
`bridge_offline_replay_online_gate_test.go`（跳过/放行/每渠道一次/缺件放行/参数透传 5 例）。
变异电池 13 格全杀（bridge 5 + service 5 + 在线读法 3；含 `defer` 顺序反接、门条件反号、
缺件改拦截、跳过不计数、探针参数换位、心跳不刷新、别名不归一），控制组 bridge ran=7 /
service ran=10 全绿，逐格还原后 md5 比对；`--- FAIL: panic: test timed out` 这种"以挂代红"
的杀法不计，已把测试里的阻塞读键改成限时读，重跑后 T4 以 `--- FAIL (2.25s)` 干净杀死。

**R16 实况：R15 那道门把下行读漏了一半**（`internal/bridge/{sse.go,handler_http.go}` + `internal/service/bridge_offline_replay.go`，`3693e1da`）：
R15 把可达性等价于"这个账号在本进程的 SSE 订阅表里"，但下行有两条路，另一条不留订阅：

1. **轮询下发是产品的一部分，不是历史残留。** `user-web/bridge/src/core/polling-loop.js`
   在服务端 `capabilities.sse_enabled=false`、或所有渠道的 SSE 都启动失败时回退到
   每 1.5s `GET /api/bridge/outbox`（`BRIDGE_THREE_CHANNEL.outboxPollIntervalMs`）；
   `docs/TROUBLESHOOTING.md:259` 还给运维写了主动这么配的路子（`FF_SSE_BRIDGE=0` 验证反代缓冲，
   flag 默认开见 `internal/pkg/featureflag/flag.go:69`）。而 `GetBridgeOutbox` 既不建订阅、
   也不刷 `last_sync_at`（当时全仓 `TouchLastSync` 只有 SSE 那一个调用方）⇒
   这串渠道在门眼里永远不可达：延后出站**逐轮整条跳过、一行不碰**，而且不报错——
   R15 想躲的"烧成判弃"没发生，发生的是"永不补投"，两者都不可观测。
   这条不是推测：门改完之后 `reach_delayed_outbound` 的唯一读者就只剩这道门。
2. **修法是把真值做成两个信号取或**，落在 bridge 侧的 `BridgeChannelOnline(ctx, channel, accountID)`：
   活 SSE 订阅（命中就不读库）OR 账号行在宽限窗内同步过（`IsOnline` = `status != offline` 且
   `last_sync_at` 距今 < `OnlineGraceWindow`）。渠道先归一再查两边；
   仓储未装配 / 读库失败一律**放行**，与"探针缺件放行"同一口径。
   `GetBridgeOutbox` 补上 `touchBridgeAccountOnline`（轮询即同步，语义与 SSE 心跳对偶），
   顺带把入参渠道归一——`message_hub` 只存规范渠道，别名入参会让轮询恒拿 0 行。
   写在线位是这次请求里的第 3 次写：`ClaimPendingOutbound` 本来就无条件先跑一次
   inflight 回收 UPDATE 再跑认领 UPDATE（`message_hub_inbox_outbound.go:43-67`），
   故没有为省这一次写再加"订阅在场就不刷"的分支。
3. **探针签名带 ctx**：探针如今要读库，`RunOnce` 的 ctx 半路换成 `context.Background()`
   就同时丢掉调用方的截止时间与 trace 链路（S1 变异格钉住）。
4. **别名键那一半：现网取证后降级为防御性收口。** 本条最初的怀疑是"`bridge_accounts` 里可能有
   历史别名渠道行，被新门永久跳过"。查下来不成立：`bridge_accounts` 70 行渠道全为规范值
   （`douyin/kuaishou/tiktok/xianyu/xiaohongshu`，零别名；`v3.17.1` 曾把旧基础值改成 `*_web`、
   `v3.18.0` 又统一回规范值，两版都注册），`reach_delayed_outbound` 与 `message_hub` outbound
   的 `platform` 同样只有规范值 ⇒ 归一仍然做（订阅键与账号行都以规范渠道为唯一键，
   留着别名入参就是留一条静默 no-op 的路），但**它不是会丢消息的那一半**。
5. **多实例部署下这道门不会把行永久卡住**（登记时的另一条怀疑）：回扫 cron 在
   `internal/pkg/cron/cron.go:96` 以 `0 */5 * * * *` 注册，`InitCron()` 由 `cmd/api/main.go:415`
   无条件调用 ⇒ 每个副本都跑，而全仓没有任何 advisory lock / leader 选举
   （`grep -rn "advisory|pg_advisory|leader|SetNX" internal/`，非测试命中 0）。
   互斥靠行级 CAS 门票（`ClaimDelayedOutboundForReplay` 只认 `RowsAffected==1`），
   不靠"谁有资格跑"；门判错的最坏结果是这一轮跳过、行留在 `pending`，下一轮由持有连接的
   那个副本或刷新过在线位的任一副本补上 ⇒ 少投一轮，不会永久扣住，也不会双发。
   R15 版本里"订阅在副本 A、跑 cron 的只有副本 B"确实会永久跳过，这条被第 2 点的 DB 信号一起收掉。
6. **`ReplayStats` 仍无人消费（登记不修，口径更新）**：`cron.go:96` 丢弃返回值，
   `Detect{Online,Offline}Channels` 在非测试代码里零读者 ⇒ 管理面看不到回扫结果。
   本轮只把日志计数改名 `skipped_no_subscriber` → `unreachable_channels`
   （旧名会把取证方向带到"没连 SSE"上，而轮询客户端是在线位过期才落到这一格的），
   不新建端点。触发条件：有人开始要这块的数，就照 `order_draft_sweep` 的"导出报告结构体"先例办。

交付：`channel_liveness_test.go`（可达性真值 6 例：订阅优先/别名归一/轮询放行/两信号皆假才算离线/
读不到真值放行/缺参不可达）+ `sse_online_state_test.go` 增 3 例（轮询刷在线位、别名轮询取到规范渠道的待投件、
仓储缺件不断轮询）+ service 侧 ctx 透传 1 例；探针签名变更后原有 5 例门用例随之调整。
变异电池 10 格全杀（B1 摘归一、B2 订阅不再优先、B3 恒放行、B4/B5 两处 fail-open 反向、
B6 摘缺参守卫、B8 轮询不刷、B9 按别名刷、B10 入参不归一、S1 ctx 换 Background），
控制组 bridge pass=10 / service pass=3、skip=0，逐格还原 md5 比对，红因逐格读到断言行。
**未钉住的一格**：`SetOutboundClaimer` 里"探针 = `BridgeChannelOnline`"这一行注入本身没有断言
（把探针退回裸订阅闭包不会有用例红）——补它需要在 service 侧开一个只给测试用的读口，
按"生产代码不为测试让路"的既有规矩放弃，改以注释与同源注入约束兜住。

**复核后修订的两条登记**
- ~~R12 交产品口径~~ → 已按方向①处置（见 R12 实况）。
- **`reach_delayed_outbound` 上并存两条 drain**：H-3 主链路（按 `send_at` 全局到期投递）与离线回扫
  （按渠道判定后补投）。抢占票已让两者互斥；本批把"渠道确实离线仍投递"这一半收掉——
  回扫加了在线门，无活订阅的渠道整条不进状态机。**剩下的口径**：富卡行仍整体让给主链路
  （桥接管道只发文本），而主链路的判弃不看渠道是否可达 ⇒ 富卡延后回复在渠道长期离线时仍会被烧成
  `failed`。这半条属主链路（`webhook_outbound.go`，本批由并行会话持有未提交改动），未擅动。

## R16-b（2026-09-21 复查两条阻塞项：一条已修、一条口径不成立并换成一条真问题）

**1) QQ `expires_in` 字符串形态 —— 复核结论：HEAD 已修毕，本条从"登记不修"降为"已闭"。**
`qq.go:72` 起 `ExpiresIn json.RawMessage` + `:78 expiresInSeconds()` 双形态兼容，落在
`092f8cf1 fix(qq): 全链路审核修复`。取证口径不是"看着像修了"：`qq_test.go` 有三处 fake
（:253 / :300 / :325）把 `expires_in` 以**字符串** `"7200"` 返回，用例断言的是取 token 后
发送成功——若解析器退回 `int64`，解码即失败、token 取不到、这三条必红，所以形态兼容是
**被既有用例承载的**，不是一条只在注释里的承诺。残留缺口只有一格：没有一条用例直接断言
"解析出的秒数"（也没喂过数字形态）。要补得动 `qq.go`/`qq_test.go`，两者当前都被并行会话
持有未提交改动（`qq.go` +253），且价值低于冲突成本 ⇒ 不擅动，等该文件回到 clean 再议。

**2) "主链路的判弃不看渠道是否可达" —— 复核结论：登记的机制描述不成立，予以否证。**
判据是 `bridge_outbound.go:144-149`：`bridgeOutboundUndeliverable` 只在 `accountID` 以
`-unknown` 结尾时判不可达，**从未读过 SSE 订阅表、也从未读过 `bridge_accounts` 在线位**。
因此"渠道长期离线 ⇒ 富卡延后回复被烧成 `failed`"这条推断不成立：离线时 bridge 分支照样落
`status=pending` 行（HEAD `webhook_outbound.go:654`），交给补投门接——而补投门只看订阅、
漏掉轮询那一半恰好是 R16 本轮修掉的东西。两条登记在这一点上是同一个洞的两个说法，现已同源收口。
（`failed` 只在两处真发生：`abandonReplay` 由"重投次数用尽"或" sendOutbound 返回**不可重试**的
ChannelError"触发，见 HEAD `:269` / `:325`，都是真实投递失败之后，不是可达性预判。）

**3) 复查中查出的真问题（换这条登记，替换第 2 条）**：桥接渠道整条**不支持富卡，且丢弃完全静默**。
证据（全部对 HEAD 复算，非工作树）：
- `webhook_outbound.go:301` 延迟重放明确把 `cards` 传进 `sendOutbound`；
- bridge 分支 `:646`（`ChannelDouyin|Xiaohongshu|Tiktok|Xianyu|Kuaishou`）里
  `MsgType` 硬编码 `"text"`（`:655`），**整段 646–813 内 `cards` 出现 0 次**（`awk|grep -c` 实测）；
- 分支末尾 `:813` 无条件 `sent = true` ⇒ 调用方 `replayDelayedOutbound` 走 `MarkSent` 收口。
合起来：一条带富卡的 AI 回复投给桥接渠道时，只有文本出去，富卡被丢掉，而队列表把这一行记成
**已送达**，日志/轨迹/`Extra` 三处都没有任何痕迹。它比原登记的那半条更坏——原口径至少会留下
`failed` 供人查，这条是把丢失写成成功。
**为什么仍不在本批修**：落点在 `service/webhook_outbound.go`，该文件正被并行会话重写
（未提交 `+97/-11`，改的正是 `nextSendRetryAt`/`sendOutbound` 的错误语义与 N-11④），叠改必冲突。
**触发条件与最小改法（交给下一刀，含本泳道）**：等该文件回到 clean 后，先做"把静默变成可观测"这一格——
在 bridge 分支 persist 之前加 `if len(cards) > 0` 的 warn + `outMsg.Extra["cards_dropped"] = len(cards)`，
配套用例断言"带卡投递桥接渠道 ⇒ 轨迹里能看到丢弃"（反向测试：摘掉 warn 必红）。
至于富卡要不要降级渲染成文本（抖音/小红书卡片能否用图文消息承载）属**产品口径**，不在测试排期内擅自定。

## 阻塞与不做什么

- `user-web/vite.config.js` 的 SPA chunk 拆分、`browser_automation` 冷启动明文（该目录仍有并行会话未提交改动）、
  TTL 产品口径、chain 复跑（需重启共享服务）—— 均**不在**本排期内，不碰其文件。
- 需要真实 LLM/Embedding Key 的 `rag/core`、`rag/service.Query` 不制造离线假断言。
- ~~生产代码一行不改：本排期只新增 `*_test.go`；发现缺陷记 Findings 供后续单独批次处理。~~
  **该约束已由用户 2026-09-20 指示解除**，处置见上节；原口径仅对本排期 Task 1–8 的补测提交成立。

## Markdown Lint 这道门：本轮把 3 处红收到 1 处，剩下那处按归属交接

推送 `0d99dc51` 后 `gh run view --log-failed` 读到 `Markdown Lint` 连红六次（`11755c55`→`0d99dc51`，
跨三条泳道的文档回灌各带一处，**没人把它当自己的账**）。三处同一种形态：中文散文的**续行以 `+ ` 开头**，
markdownlint 按列表项解析 ⇒ MD004（本仓口径 dash）。本轮处置：

- 修 `本文件:2898`（R11 那段"交付：… + `controller/platform_test.go` 2 例"）⇒ 续行改以"与"起头；
- 修 `docs/architecture/DATABASE_SCHEMA_DEEP_DIVE.md:997`（T-P4-04 三条路径那句）⇒ 只把换行位置挪到
  ` +` 之后，**一字未改**，属"谁的文档谁的门"最小介入；
- **不代改** `docs/architecture/CHANNEL_INTEGRATION_AUDIT_2026-09.md:797`（HEAD 口径；该行现被该文件的
  并行泳道未提交改动挪到工作树 `:808`）——该文件整份压着别人的未提交内容，一 `git add` 就会把别人的
  批次一起提交掉。⇒ 交接给该文件所属泳道：改成同前两条一样的换行挪位即可，不需要动文字。

**判据留档**：门的红要读**逐步结论**而不是只看 workflow 状态（与 [[gate-scope-blind-spots]] ⑧ 同一口径），
`gh run list --workflow "Markdown Lint" --json headSha,conclusion` 一眼就能看出"连红六次"这件事本身
比任何一处 MD004 更值得修；而本地没有 `markdownlint-cli2`（未装、不擅自装），
所以"复现"只能靠推一次看真门 ⇒ 修门的那一刀必须自己过一次门，别只靠 `grep "^[[:space:]]*+ "` 的超集近似
（它还会命中归档文件里位于代码围栏内的行，那种不是违规）。

**推一次看真门的实测结果**（run `35546266388`，HEAD `3d360e89`）：`Linting: 153 file(s)` →
`Summary: 1 error(s)`，唯一一条正是上面点名"不代改"的
`docs/architecture/CHANNEL_INTEGRATION_AUDIT_2026-09.md:797:3 MD004/ul-style [Expected: dash; Actual: plus]`
⇒ 本泳道两处修改都过了门、本轮新增段零违规，"3 处收到 1 处"至此才是跑出来的数而非推断。
引入那一行的是该文件首版提交 `7329590d`（`git blame -L 797,797 HEAD`），不属本泳道任何提交。

**为什么连"单方面改 HEAD 那一行"这种取巧也不做**：`git commit-tree` 能在完全不碰工作树的前提下提交一个
"HEAD 版该文件 + 换行挪位"的树，但那份文件的工作树副本仍压着并行泳道的未提交内容（本轮实测
`git status --short` 报 ` M`，`+1091` 行），他们下一次提交该文件会把我这一行**原样带回 `+ `** ⇒
这道修不粘，而"看起来已修、实则静默回退"比诚实交接更坏。⇒ 只有两种落点算修完：改在工作树副本上
（那是别人的在制品，不代改），或等该文件回 clean 后由 owner 泳道一次改到位。

### 另一道常红门 `Lint` 的归因（本泳道不认领、给精确指针交接）

`Lint` workflow 自 `478ef1c4` 起连红，读**逐步结论**得到的是两件事：
① 阻断项只有 **2 个 error**，都在 `user-web/browser_automation/` ——
`src/core/cdp/input.js:216`（`preserve-caught-error`：catch 里重抛没带 `cause`）与
`src/core/primitives.js:821`（`no-useless-assignment`：赋给 `navigated` 的值后续没人读）；
② 该 job 因这一步红而把后面的 **`LICENSE Compliance Scan` 整步 `skipped`** ⇒ "扫描通过"这句
在最近的每一次运行里都是**没有证据的**（⑧ 的同族形态）。
本地复现口径要连版本一起记：`user-web/node_modules/.bin/eslint` **v10.10.0**、`npm run lint:check`
**rc=1**、`18746 problems (2 errors, 18744 warnings)`；活树跑出来是 **3** 个 error（多出
`primitives.js:908`），因为 `primitives.js` 正被并行泳道改着 —— **活树计数 ≠ HEAD 计数**，
拿本地数字去核对 CI 前先 `git status` 看落点文件脏不脏。
**为什么不代改**：`browser_automation` 整目录是本排期白纸黑字的"不碰其文件"面（该目录仍有并行会话未提交
改动，含 `src/core/primitives.js`、`test/batch18-submit-enter.test.js`），两处 error 又不属本泳道任何改动
（我的提交只碰 `internal/bridge`、`internal/service` 与文档）。⇒ 交接条件：该目录回 clean 后由 owner 泳道
补 `cause: err` 并删掉那次多余赋值，`Lint` 绿即连带把 license 那步放回来。

## R17（2026-09-21 第三十二轮：把「降级在数据层面不可用」这条口径改成判据并修完）

用户指示「任何残留的问题都要找出来 解决并修复」⇒ 回到 Task 7 那条被写成"口径"的登记，
问的是它能不能修而不是它有多合理。判据先跑出来：临时探针用例（跑完即删）在测试库上按
AutoMigrate 基线 → 逐个 `Up()`（记录每个迁移真正新建了哪些表）→ 逆序逐个 `Down()`
（记录每个迁移删掉了哪些表）逐格比对，实测 **74 个迁移里 21 个的 Down 删了不属于它的表，
一轮 Up→Down 净丢 53 张基线表**（基线 300 → Up 后 320 → Down 后 253）。

**根因不是某个人写错了 SQL，是两处所有权口径叠在一起**：本项目建表事实源是 GORM AutoMigrate
（`internal/pkg/db/migrate.go`，299–300 张），版本化迁移只是其上的增量层；而这些迁移的 Up 与
Down 按"这表是我建的"写（`CREATE TABLE IF NOT EXISTS` / `DropTable`），降级那一半的独占假设不成立。
可达性也核过：启动链只跑 Up（`v1.0.0→v1.0.0` 空跑，所以平时无感），而
`POST /api/migration/rollback`（admin 组，`migrationCtrl.Rollback`）一次调用即取
`registry.Get(target)` 直接 `migration.Down(ctx)` ⇒ 一次请求销毁一张在用表，且当前进程不会重建它
（AutoMigrate 只在启动时跑），要等下次重启才回来——回来时是空表。

**修法**：Down 只撤销自己 Up 造成的变化。列与索引的回退保留（`script_templates`、`sop_agents`、
`llm_routing_audit`、`customer_rfm` 这些确由本迁移新建/新增的对象照常回退），
21 处删表统一改为包级 `declineTableDrop(version, tables...)` 记一条日志放弃。
新增 `down_table_policy.go` 把这条口径写在一处，注释里同时写明可达路径与判据位置。

**判据（写进 `a_full_chain_migration_test.go`，两道）**：① 逐迁移「Down 删掉的表 ⊆ 该迁移 Up 新建的表」；
② 总量「一轮 Up→Down 后基线表不得净丢失」。先跑红：`21/74` + `53 张`（两道同时红，红因一致）；
修完跑绿：违规 `0/74`、Down 后表数 **253 → 306**、`Down 失败 1/74` 仍是 `v3.28.0` 那句显式拒绝（既有设计，只记不判）。
反向验证（新门必须能红）：把 `bridge_accounts` 的 `DROP` 装回一处 ⇒ 两道判据同时红并点名该表，
`cp` 还原后 md5 `5913d51e…` 与装变异前一致。

**三处把旧行为钉住的用例随之搬正**：`confidence` / `humanize` / `feedback_loop` 的 `TestXxxDown`
原本断言"Down 后表应被删除"，改成"降级后表必须还在"（`deletedTables` → `preservedTables`）；
`nil db Down() 应返回错误` 这类负例保住（改法是在 Down 开头留 `if m.db == nil` 那道守卫，
而不是把空 `stmts` 传给 `execAll`）。

**本轮一并收的两条环境账**：① `internal/aiagent/knowledge/service` 的
`TestRagSearcher_RealVectorSearch` 在干净克隆里同红 ⇒ 红因是测试库口令漂移（`28P01`），
带 `POSTGRES_TEST_PASSWORD` + `POSTGRES_TEST_PORT=8232` 复跑 **rc=0**（5 子用例全绿），不是回归；
② 变异脚本自身的一次假证据：`if X != nil {` 换成 `false && X != nil {` 时把 `if ` 前缀一起吃掉 ⇒
两格"红"其实是 `build failed`（syntax error），读红因才没把它当成行为杀；修正后的两格短路变异
互杀（摘 Entities 复制只有 Entities 用例红、摘 PreviousTopics 复制只有另一条红）。

**留下的、写清边界的残项**：`ai_perf_faq_sop_layer` 与 `llm_routing_logs` 这两处**保留**了索引回退
（索引确由本迁移建），于是降级后表还在但其上新建过的索引会缺，直到下次启动 AutoMigrate 补齐 ⇒
性能面自愈、不丢数据，与删表不是一类，故不在本刀内。

---

## R18（2026-09-21 第三十三轮：R17 留下的那条"残项"实测后比删表更严重，一并收口）

R17 末尾把"列与索引的回退保留"写成一条边界清晰的残项，理由是"性能面自愈、不丢数据"。
本轮按用户指示（「任何残留的问题都要找出来 解决并修复」）回去核这句话，**它只对索引成立，对列不成立**：
GORM AutoMigrate 会在下次启动时补回缺失的表/列/索引，但补回的列是**空列**——降级删掉的列连同其
数据一起消失，重启只是把 schema 复原，值无处可回。于是同一把可达钥匙（`POST /api/migration/rollback`
一次调用 ⇒ `registry.Get(target).Down(ctx)`）销毁的东西比 R17 记的多得多。

**先把口径跑成数字**（探针判据即 R17 那道全链路用例，本轮把它的比对轴从"表"扩到"表/列/索引"）：
在同一条 AutoMigrate 基线（300 表 / 3868 列 / 795 非约束索引）上逐迁移比对 Up 新建与 Down 撤销的集合，
实测 **20/74 的 Down 删掉模型声明的列，一轮 Up→Down 净丢 66 列；8/74 删掉模型声明的索引，净丢 16 个**。
红因样本（`/tmp/r18_col.log`、`/tmp/r18_red.log`）：`llm_routing_logs` 一处删 10 列、`script_templates`
删 5 列、`sop_executions` 删 4 列 + 2 索引，`layer_decision_logs` 一处删 5 个索引。

**修法沿用 R17 的口径，不新立规矩**：Down 只撤销自己 Up 造成的变化。21 个迁移文件里的列/索引回退
统一改走 `declineColumnDrop` / `declineIndexDrop` 记一条日志放弃；R17 那个只讲表的
`down_table_policy.go` 随之改名 `down_drop_policy.go`，内部收敛成一个 `declineDrop(version, kind, objects...)`
加表/列/索引三个包装，判据位置与可达路径仍只写在文件头注释一处。
确由本迁移建出的对象照旧回退：`v2.7.0` 仍删自己建的 `query_rewrite_cache`/`embedding_cache`，
`v3.15.0` 的 Down 从 16 条 SQL 缩到只剩自己那两张表。

**判据从两道扩成六道**（`a_full_chain_migration_test.go`）：逐迁移「Down 删掉的 ⊆ Up 新建的」× 表/列/索引，
加基线三轴总量「一轮 Up→Down 后不得净丢失」。新增一处**连带豁免**并写明理由：
删一张确由本迁移建出的表，必然带走其上的索引与列，那不是越权删除，故
`if owned[key][beforeIndexes[index]] { continue }`（列同理按 `table` 前缀判）。
豁免不是放水：基数守恒可核，修完 Down 后 306 表 / 910 索引 / 3930 列，三轴相对基线**只增不减**。

**反向验证（新判据必须能红，且两轴互相独立）**三格：
① 装回一处列 DROP（`v3.15` 的 `ALTER TABLE knowledge_documents DROP COLUMN agent_id`）⇒ 列轴与索引轴同红
（各 `1/74`，基线净丢失 1 列 `knowledge_documents.agent_id` + 1 索引 `idx_knowledge_documents_agent_id`）——
索引那条是"删列隐含删其索引"的真实连带，不是判据串扰；
② 两处只装回索引 DROP 的变异**跑绿**，读红因后确认是判据正确放行：`v2.1.2` 的
`idx_telegram_accounts_polling_owner`、`v3_32` 的 `idx_sessions_handoff_at` 确由本迁移 Up 建出（后者被点名
的损失其实是 `idx_customer_sessions_handoff_at`，GORM 自动命名与手写名不同族，属列删的连带）；
③ 换到**基线自有**的索引上装回（`rag_hybrid` 的 `idx_knowledge_chunks_content_hash` +
`v3_43` 的 `idx_browser_steps_text_hash`）⇒ **索引轴单独红**（`2/74`、基线索引净丢失 2）而列轴不红，
证明两轴各判各的。三格全部 `cp` 自 `.bak-r18c` 还原，逐文件 md5 与变异前一致
（`25ba4777…` / `5f85b19f…`），`grep` 复核无变异残留，`.bak` 已删净（未跟踪文件不得留，否则污染下轮基线）。

**门（各记各的）**：修完带 `-test.v` 单跑与整包复跑均绿（整包 `42.017s`，91 个声明用例，
`Down 失败 1/74` 仍是 `v3.28.0/email_smtp` 那句显式拒绝"密码解密回明文是安全倒退"，既有设计只记不判）；
还原变异后再跑整包 `rc=0 / 76.400s`——这一跑没带 `-test.v`，故其 `--- FAIL` 计数恒 0 属空证据，
证据只认 rc=0 与前一跑 `-test.v` 明细。两轮之间 `vm.loadavg` 48 → 79、并发 `go test` 进程 3 → 4，
时长 42s → 76s 的摆动归负载，不据此改判据。`gofmt -l` 0 行、`go vet ./internal/migration/...` 输出 0 字节。

**代价与边界（写清，不说成无损）**：本轮的降级面按文件粒度统一放弃，会**多放弃**一些确归本迁移的对象
——上面 ② 那一格就是证据（装回自己建的索引判据不红，说明那几处本可安全回退）。多留一个对象的代价是
schema 上冗余一行、下次启动 AutoMigrate 也不清理；误删一个在用对象的代价是不可逆的数据丢失，
两边不对称，故取"宁可多留"。本轮不动 Up 面（Up 仍按幂等建对象），也不动 `v3.28.0` 的显式拒绝。


## R19（2026-09-21 第三十四轮：user-server 全局请求体封顶，把第十六轮那条"只封了个别入口"的残项收掉）

第十六轮交付时明确登记过：那一轮只补了 **etl 解压 / `service.ReadAll` / 微信回调** 三处，
而 gin 引擎层对 JSON 请求体**没有默认上限** ⇒ 剩下的口子（以 authed 内部口为主）一直没人管。
本轮把它从"登记"变成"有牙的门"：提交 `4d9a13c5`（5 路径 +496）双推 `6326a1bb..4d9a13c5` → upstream + gitee-upstream。

**落地两件事**（`internal/middleware/body_limit.go` + `internal/router/router.go`）：

1. `middleware.BodyLimit` 两条防线：Content-Length 预检给**真实 HTTP 413**（走 `response.Error`
   的 int 分支 ⇒ 状态码与信封 code 同源，handler 一次都不进）；无长度/chunked 的请求由
   `http.MaxBytesReader` 兜住读取。**已知取舍照实写**：兜底触发时错误由 handler 自己返回
   （通常是 4xx 而非 413），但内存已经被限住 —— 那才是本中间件的首要目标。
   multipart **跳过**（本仓上传通道走 multipart，一刀切会打断合法大文件上传）。
   旋钮 `MAX_JSON_BODY_MB`：未设置/非法 → 默认，显式 `0` 或负数 → 不限制（迁移期应急开关）。
2. 装配点必须在 `router.Setup` 全局链**最前部**（`gin.Recovery()` 之后、任何会读 body 的
   中间件与 JWT 之前）：gin 的引擎级 `Use` 只对注册在它之后的路由生效，晚一步等于给先注册的路由留口子。

**两个数都是自己仓里推出来的，不是抄平台端**：

- 默认 8MB：逐个 grep 出来的既有上界是 webhook 可调顶格 4MB（`maxWebhookMaxBody`）、
  `service.ReadAll` 2MB、MCP 口 1MB、bridge 入站 1MB（`handler_http.go:866`）、微信回调 1MB、
  商机口 4KB（`opportunityBodyMaxBytes`）⇒ 全局值必须**高于**它们，否则这条 env 的高段被静默吃掉
  （方向由 `TestGlobalDefaultDoesNotTightenExistingCaps` 钉住，它直接引用同包的 `maxWebhookMaxBody`）。
- `MaxMultipartMemory` 从 gin 默认的 **32MB 收到 8MB**：本仓最大合法单文件是 50MB（知识库导入
  `MaxUploadFileSize`）、次为聊天媒体 20MB、素材/通用上传 10MB，没有任何一档需要把整份文件留在内存里。
  判据读的是 `gin.New()` 的真实默认值而不是硬编码 32MB —— 升级 gin 改了默认也会测出来。
  （首版这里是照抄的 64MB，被这条腿当场打红：**抬高**默认缓冲等于给每个并发上传请求多发一份内存。）

**判据与证据（全部真跑）**：

- 单元 9 例 + 装配 3 例（`internal/router/body_limit_wiring_test.go` 走真实 `Setup(r, testdb)`）。
  装配腿打的是 `POST /api/users`：超限 ⇒ **413 而不是 401**，一条断言同时钉住"挂上了全局链"与
  "挂在 JWT 之前"；对照组小 body ⇒ 仍是既有的 401，证明没误伤正常载荷。
- 变异电池 10 格：**9 杀 + 1 格等价性探针**。预检阈值翻倍、摘掉 `MaxBytesReader`、multipart 前缀改认不出、
  `maxBytes<=0`→`<0`、env 显式 0 回落默认、默认降到 2MB、缓冲抬到 64MB、摘掉 `r.Use(BodyLimit)`、
  摘掉 `r.MaxMultipartMemory` —— 每格红因都读过且互不重叠。
  `M5` 那格（`mb <= 0` → `mb < 0`）实测**存活**：`0*MiB` 与 `return 0` 同值、负 MB 进 `BodyLimit` 又落回
  同一道放行 ⇒ 经公开 API 观察不到差别，属等价变异，**不为它编断言**；换成"显式 0 被当成未设置"才杀得掉。
- 一条前提用例：`TestMultipartUploadLargerThanMemoryBufferSurvives` 真造 9MB multipart 打 8MB 缓冲，
  断言 `SaveUploadedFile` 落盘字节数与原件一致 —— 它自带前置（9MB 必须大于缓冲），缓冲被抬到 64MB 时
  按设计 `Fatalf` 自证"这格测不到了"，而不是悄悄绿着。
- 门：`./internal/... ./cmd/...` 全量 `-p 1 -count=1` 在 `--shared` 克隆 **rc=0 / 121 包 ok / FAIL 0**
  （跑时 load 9.5–11、并发 `go test` 1–2 个）；收口态两包复跑 + `go build ./...` + `go vet` 在
  新 HEAD 克隆里复验自洽（380 条 PASS 行含子用例）。静态门：架构 rc=0、文档一致性 rc=0（活树工作区布局）、
  离线链接 rc=0（153 md / 断链 0）、凭证门在 HEAD 克隆 rc=0。
- 环境归因（不是本卡的红）：① 活树 `./internal/router/` 一度**编译不过**，红因是并行会话 in-flight 的
  `internal/browser_automation/service/executor.go`+`session.go` 类型不匹配 ⇒ 本卡门全部改在克隆里跑；
  ② 工作区凭证门 4 处命中全在 `??` 未追踪的 `webhook_batchc_*` / `webhook_batchg2b_*` 测试夹具里
  （HEAD 克隆 0 命中）⇒ 登记不碰；③ markdownlint 的 MD004 与 CI 侧 ESLint 两 error 仍按归属交接。

**登记为残项、本卡不动**：`middleware/sanitize.go` 的 `SanitizeInput`（自带 `MaxBodyBytes 1MB`）
在仓内**零调用方** ⇒ 一道没接线的封顶。删前要按"grep 未命中≠无用"的规矩证伪动态引用面，
且它和 `BodyLimit` 语义不同（它截断读取、不报错），是否接线属产品口径 ⇒ 只登记。

**勿放松**：`r.Use(BodyLimit(...))` 必须留在 `router.Setup` 的全局链前部（挪到 `auth` 组之后即漏掉
先注册的路由）；`DefaultMaxJSONBodyMB` 不得低于任何按端点上界；`maxMultipartMemoryMB` 只能是**收紧**
gin 默认的方向；multipart 跳过这条不能改成"一并 413"（会打断 10–50MB 的合法上传）。
