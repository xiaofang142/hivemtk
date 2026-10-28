package egolite

// 契约门测试（specs/017①④⑤⑥）：历史评估 2026-10-27 六探针中仍开放的四项回归钉。
// ① 错版本可组装：版本钉扎 + 批准 hash 快照；④ Start(draft) 绕扫描门；⑤ exit0 即
// completed 无业务证据 → EGO-R033 审计告警行；⑥ loopback 门依赖 IP 短路放行。

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// ---------- ⑥ 回环门 ----------

type stubKVRepo struct{ vals map[string]string }

func (s *stubKVRepo) Available() bool { return true }
func (s *stubKVRepo) Get(_ context.Context, key string) (string, error) {
	v, ok := s.vals[key]
	if !ok {
		return "", errors.New("not found")
	}
	return v, nil
}
func (s *stubKVRepo) Upsert(_ context.Context, key, value string) (string, error) {
	s.vals[key] = value
	return value, nil
}
func (s *stubKVRepo) EnsureTable(_ context.Context) error { return nil }

func loopbackCtx(t *testing.T, remoteAddr string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	req := httptest.NewRequest(http.MethodGet, "/api/egolite/runner-ws", nil)
	req.RemoteAddr = remoteAddr
	c.Request = req
	return c
}

// TestLoopbackGate_Matrix 门启用与否 × 来源 IP 形状的完整矩阵。
// 修前缺陷：门开 + 合法非回环 IP ⇒ loopbackIfConfigured 返回 false ⇒ Handle 短路放行。
func TestLoopbackGate_Matrix(t *testing.T) {
	h := NewEgoRunnerWSHandler(NewEgoRunnerRegistry(), &stubKVRepo{vals: map[string]string{}})
	cases := []struct {
		name       string
		kv         string
		remoteAddr string
		wantReject bool
	}{
		{"门关+非回环IP放行", "", "192.168.1.5:40000", false},
		{"门关+回环IP放行", "", "127.0.0.1:40000", false},
		{"门开+回环IP放行", "true", "127.0.0.1:40000", false},
		{"门开+合法非回环IP拒", "true", "192.168.1.5:40000", true},
		{"门开+畸形RemoteAddr拒(fail-closed)", "true", "malformed", true},
		{"门开但KV值非true放行(形态乙)", "false", "192.168.1.5:40000", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hkv := NewEgoRunnerWSHandler(NewEgoRunnerRegistry(), &stubKVRepo{vals: map[string]string{
				"egolite_runner_loopback_only": tc.kv,
			}})
			if tc.kv == "" {
				hkv = NewEgoRunnerWSHandler(NewEgoRunnerRegistry(), &stubKVRepo{vals: map[string]string{}})
			}
			c := loopbackCtx(t, tc.remoteAddr)
			if got := hkv.loopbackRejected(c); got != tc.wantReject {
				t.Fatalf("loopbackRejected=%v want %v（kv=%q remote=%q）", got, tc.wantReject, tc.kv, tc.remoteAddr)
			}
		})
	}
	_ = h // 基线 handler 仅确认零配置构造可用
}

// ---------- ④ draft 直发扫描门 ----------

func TestStart_DraftScanGate(t *testing.T) {
	_, _, flowRepo, _, _, svc := newExecTestEnv(t)
	ctx := context.Background()

	passed := &model.EgoliteFlow{UserID: 3, Name: "passed", DomainsJSON: []byte(`["github.com"]`), Script: "console.log(1)", Status: model.EgoFlowDraft, ScanPassed: true}
	if err := flowRepo.Create(ctx, passed); err != nil {
		t.Fatal(err)
	}
	blocked := &model.EgoliteFlow{UserID: 3, Name: "blocked", DomainsJSON: []byte(`["github.com"]`), Script: "console.log(1)", Status: model.EgoFlowDraft, ScanPassed: false}
	if err := flowRepo.Create(ctx, blocked); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Start(ctx, 3, StartRequest{Origin: "draft", FlowID: blocked.ID}); !errors.Is(err, ErrFlowScanGate) {
		t.Fatalf("未过扫描门的 draft 直发应 ErrFlowScanGate，实得 %v", err)
	}
	e, err := svc.Start(ctx, 3, StartRequest{Origin: "draft", FlowID: passed.ID})
	if err != nil {
		t.Fatalf("已过扫描门的 draft 直发应放行，实得 %v", err)
	}
	if e.State != model.EgoExecCreated {
		t.Fatalf("runner 不在线时应留 created，实得 %s", e.State)
	}
}

// ---------- ① 版本钉扎 + 批准 hash 快照 ----------

func upsertPublishedAsset(t *testing.T, ctx context.Context, repo EgoAssetRepository, code string, version int, hash string) *model.EgoliteAsset {
	t.Helper()
	a := &model.EgoliteAsset{
		AssetCode: code, Name: "t", Version: version, Entry: "tools/main.js",
		Script: "console.log(1)", ScriptHash: hash,
		Status: model.EgoAssetPublished, ScanPassed: true,
		Source: model.EgoAssetSourceFlow, PublisherUserID: 1,
	}
	out, err := repo.Upsert(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestStart_MarketVersionPin(t *testing.T) {
	_, _, _, assetRepo, _, svc := newExecTestEnv(t)
	ctx := context.Background()
	upsertPublishedAsset(t, ctx, assetRepo, "pin-asset", 3, "aa"+repeatHash())

	if _, err := svc.Start(ctx, 7, StartRequest{Origin: "market", AssetID: "pin-asset", AssetVersion: "2", Entry: "tools/main.js"}); !errors.Is(err, ErrAssetVersionConflict) {
		t.Fatalf("客户端报旧版本应 ErrAssetVersionConflict，实得 %v", err)
	}
	e, err := svc.Start(ctx, 7, StartRequest{Origin: "market", AssetID: "pin-asset", Entry: "tools/main.js"})
	if err != nil {
		t.Fatalf("缺省版本应放行并钉扎当前版本，实得 %v", err)
	}
	if e.AssetVer != "3" {
		t.Fatalf("缺省版本应钉扎为当前批准版本 3，实得 %s", e.AssetVer)
	}
}

// ---------- ① dispatch 批准快照（script_hash 下发 + 版本漂移判 failed） ----------

func TestDispatch_MarketSnapshotAndDrift(t *testing.T) {
	execRepo, evRepo, _, assetRepo, registry, svc := newExecTestEnv(t)
	ctx := context.Background()
	a := upsertPublishedAsset(t, ctx, assetRepo, "snap-asset", 2, "bb"+repeatHash())

	// 执行 1：runner 未上线时 Start ⇒ 留 created（漂移场景的起点）
	e1, err := svc.Start(ctx, 7, StartRequest{Origin: "market", AssetID: "snap-asset", Entry: "tools/main.js"})
	if err != nil {
		t.Fatal(err)
	}
	if e1.State != model.EgoExecCreated {
		t.Fatalf("runner 不在线时应留 created，实得 %s", e1.State)
	}

	// 真 ws 连接（捕获服务端）让 registry 有在线 runner
	frames := make(chan []byte, 4)
	srv := httptest.NewServer(captureWSHandler(t, frames))
	t.Cleanup(srv.Close)
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if _, _, err := registry.Register(7, RunnerMeta{DeviceID: "d1", conn: conn}); err != nil {
		t.Fatal(err)
	}

	// 执行 2：在线 runner 下发 ⇒ 状态机推进 + 帧捕获
	e2, err := svc.Start(ctx, 7, StartRequest{Origin: "market", AssetID: "snap-asset", Entry: "tools/main.js"})
	if err != nil {
		t.Fatal(err)
	}
	got2, err := execRepo.GetByID(ctx, e2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got2.State != model.EgoExecRunning && got2.State != model.EgoExecDispatching {
		t.Fatalf("在线 runner 下发后应为 dispatching/running，实得 %s", got2.State)
	}

	// 帧内容断言（服务端捕获）：spec 必须携带服务端批准 hash 快照（runner 交叉核对的数据源）
	select {
	case raw := <-frames:
		var frame struct {
			Type string `json:"type"`
			Spec struct {
				ScriptHash  string `json:"script_hash"`
				AssetVer    string `json:"asset_version"`
				AssetID     string `json:"asset_id"`
				DraftScript string `json:"draft_script"`
			} `json:"spec"`
		}
		if err := json.Unmarshal(raw, &frame); err != nil {
			t.Fatalf("帧不是合法 JSON: %v", err)
		}
		if frame.Type != "task_spec" || frame.Spec.AssetID != "snap-asset" {
			t.Fatalf("帧形状异常: type=%s asset=%s", frame.Type, frame.Spec.AssetID)
		}
		if frame.Spec.ScriptHash != a.ScriptHash || frame.Spec.AssetVer != "2" {
			t.Fatalf("spec 应带批准 hash 快照 script_hash=%s asset_version=2，实得 %s / %s", a.ScriptHash, frame.Spec.ScriptHash, frame.Spec.AssetVer)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("2s 内未捕获 task_spec 帧")
	}

	// 版本漂移：Start 之后资产更新 ⇒ Redispatch 判 failed（批准快照失效）
	a.Version = 3
	if _, err := assetRepo.Upsert(ctx, a); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Redispatch(ctx, 7, e1.ID); err != nil {
		t.Fatalf("Redispatch 本身不报错（失败落在状态机）: %v", err)
	}
	got, err := execRepo.GetByID(ctx, e1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != model.EgoExecFailed || !strings.Contains(got.FailReason, "版本") {
		t.Fatalf("版本漂移后应 failed 且 fail_reason 提版本，实得 %s / %s", got.State, got.FailReason)
	}
	_ = evRepo
}

func captureWSHandler(t *testing.T, frames chan<- []byte) http.Handler {
	t.Helper()
	up := websocket.Upgrader{}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		for { // 捕获 registry conn 下发的所有帧，供测试断言
			_, msg, err := c.ReadMessage()
			if err != nil {
				return
			}
			frames <- msg
		}
	})
}

// ---------- ⑤ completed 证据口径（EGO-R033） ----------

func TestFinishFromDone_EvidenceWarn(t *testing.T) {
	execRepo, evRepo, _, _, registry, svc := newExecTestEnv(t)
	ctx := context.Background()

	mkExec := func(t *testing.T) *model.EgoliteExecution {
		t.Helper()
		e := &model.EgoliteExecution{UserID: 5, FlowID: 0, AssetID: "a", Origin: "market", State: model.EgoExecRunning}
		if err := execRepo.Create(ctx, e); err != nil {
			t.Fatal(err)
		}
		return e
	}

	// 场景 A：done 前无任何结构化事件 ⇒ completed + EGO-R033 告警行
	ea := mkExec(t)
	if err := svc.IngestEvent(ctx, 5, RunnerEvent{Seq: 1, Type: "done", ExecutionID: EgoExecKey(ea.ID), ExitCode: 0}); err != nil {
		t.Fatal(err)
	}
	got, _ := execRepo.GetByID(ctx, ea.ID)
	if got.State != model.EgoExecCompleted {
		t.Fatalf("exit 0 应 completed，实得 %s", got.State)
	}
	evs, _ := evRepo.ListByExecution(ctx, ea.ID, 100)
	if len(evs) != 2 {
		t.Fatalf("应恰有 done + EGO-R033 告警两行，实得 %d", len(evs))
	}
	var warnPayload map[string]any
	if err := json.Unmarshal(evs[1].Payload, &warnPayload); err != nil || warnPayload["code"] != "EGO-R033" {
		t.Fatalf("第二行应为 EGO-R033 告警，实得 %s / %v", evs[1].Type, warnPayload)
	}

	// 场景 B：有结构化证据行 ⇒ completed，无告警行
	eb := mkExec(t)
	if err := svc.IngestEvent(ctx, 5, RunnerEvent{Seq: 1, Type: "action", ExecutionID: EgoExecKey(eb.ID), Payload: json.RawMessage(`{"ok":true}`)}); err != nil {
		t.Fatal(err)
	}
	if err := svc.IngestEvent(ctx, 5, RunnerEvent{Seq: 2, Type: "done", ExecutionID: EgoExecKey(eb.ID), ExitCode: 0}); err != nil {
		t.Fatal(err)
	}
	gotB, _ := execRepo.GetByID(ctx, eb.ID)
	if gotB.State != model.EgoExecCompleted {
		t.Fatalf("exit 0 应 completed，实得 %s", gotB.State)
	}
	evsB, _ := evRepo.ListByExecution(ctx, eb.ID, 100)
	if len(evsB) != 2 {
		t.Fatalf("有证据时不应追加告警行（action+done 两行），实得 %d", len(evsB))
	}
	_ = registry
}

// repeatHash 返回 62 字符的占位 hash（长度形状正确即可，不参与真校验）
func repeatHash() string { return strings.Repeat("0", 60) }
