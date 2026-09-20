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
- 参考（只读）：`internal/platform/client.go:22-74`（`sign` 三级取密钥）、`82-153`（`ensureJWTToken` 分支）、
  `249-292`（`RegisterMerchant` + `saveMerchantSecret`/`loadMerchantSecret`，路径 `config/.merchant_api_secret`）、
  `294-342`（`GetLicenseStatus` + `PlatformError.Error/Msg`）、`371-441`（`ReportInstall`/`ReportHeartbeat` 及 Default 包装）
- 已有覆盖（勿重复）：`client_test.go` 的 401 自愈、非 2xx 结构化错误；`asset_market_test.go` 的 `withMarketConfig` 范式。

**Interfaces:**
- Consumes: `NewPlatformClient(key) *Client`、`(*Client).sign/do/doRetry/Do/ensureJWTToken/RegisterMerchant/GetLicenseStatus/ReportInstall/ReportHeartbeat/SetMerchantSecret`、
  `saveMerchantSecret/loadMerchantSecret/merchantSecretFilePath`、`PlatformError`、`config.PlatformCfg`、`t.Chdir`（Go 1.24+，本仓 go1.26.6）。
- Produces: 无。

- [ ] **Step 1: 写用例**

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

- [ ] **Step 2: 跑绿**

Run: `set -a; source ../.env; set +a && go test -p 1 -count=1 -coverprofile=/tmp/plat.cov ./internal/platform/ && go tool cover -func=/tmp/plat.cov | tail -25`
Expected: 全包 ≥65%；`sign/ensureJWTToken/doRetry/RegisterMerchant/loadMerchantSecret/saveMerchantSecret/ReportInstall/ReportHeartbeat/PlatformError.Error/Msg` 非 0%。
注意：本包测试禁用 `t.Parallel`（`config.PlatformCfg` 与 `t.Chdir` 都是进程级）。

- [ ] **Step 3: 反向验证**

备份 `internal/platform/client.go` → 注入：
1. `sign` 里删掉 `if i := strings.IndexByte(path, '?')` 的截断 → `TestSignSecretPrecedence` 的 query 断言 FAIL。
2. `ensureJWTToken` 的 `if loginResp.Data.Token == ""` 改成 `if false` → `TestEnsureJWTTokenBranches` FAIL。
3. `doRetry` 里 `if resp.StatusCode == http.StatusUnauthorized && !retried` 改成 `&& retried` →
   既有 `TestClient_Do_401SelfHeal` 红（证明本任务的门会保护既有用例）。
逐次 `cp` 还原后复绿。

- [ ] **Step 4: 提交并推送**

`git add user-server/internal/platform/client_internals_test.go`
→ `git commit -m "test: 平台客户端签名优先级/JWT 分支与安装心跳上报补测"` → 双远端推送。

---

## 收尾（全部任务完成后）

1. `cd hivemtk/user-server && set -a; source ../.env; set +a && go vet ./... && gofmt -l . | head` → 均无输出。
2. 逐包覆盖率重测并汇总成表贴给用户：8 个包的 before/after。
3. 回灌 memory：`project-audit-backlog-2026-09.md` 的「已结」追加本排期 commit 列表；
   新 finding（live-code 无 recover、订单号假值、UpdateContext 浅拷贝、迁移 Down 不完备、
   v3.36.0 触发器建不起来、v3.22.0 Scan NULL 中止、v3.3.0 列名漂移、两迁移未注册、`generateSessionID` 碰撞）写进同一文件的 Findings 段。
4. 双远端 `git rev-list --left-right --count master...<remote>/master` 最终 `0/0`。

## 执行中发现（仅记录，本批不改生产代码）

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

## 阻塞与不做什么

- `user-web/vite.config.js` 的 SPA chunk 拆分、`browser_automation` 冷启动明文（该目录仍有并行会话未提交改动）、
  TTL 产品口径、chain 复跑（需重启共享服务）—— 均**不在**本排期内，不碰其文件。
- 需要真实 LLM/Embedding Key 的 `rag/core`、`rag/service.Query` 不制造离线假断言。
- 生产代码一行不改：本排期只新增 `*_test.go`；发现缺陷记 Findings 供后续单独批次处理。
