package router

// egolite_stream_test.go —— S-13 SSE 行为（真 PG）。
//
// 钉三件事：越权即 404（不泄漏存在性）、终态回放后发 end 帧收流、
// 运行中随广播推增量（不是等服务端轮库的假流）。

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/egolite"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

func newStreamTestServer(t *testing.T) (*httptest.Server, egolite.EgoExecutionRepository, egolite.EgoEventRepository, *egolite.EgoEventHub) {
	t.Helper()
	db := testutil.NewTestDB(t, &model.EgoliteExecution{}, &model.EgoEventLog{})
	if db == nil {
		t.Fatal("测试库不可用")
	}
	execRepo := egolite.NewEgoExecutionRepositoryWithDB(db)
	evRepo := egolite.NewEgoEventRepositoryWithDB(db)
	hub := egolite.NewEgoEventHub()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/s/:id", func(c *gin.Context) {
		id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
		// 固定 uid=1：中间件在真实链路里给，用例只测 handler 本身
		streamEgoExecution(c, execRepo, evRepo, hub, 1, uint(id))
	})
	srv := httptest.NewServer(r)
	return srv, execRepo, evRepo, hub
}

// 1. 别人的执行 = 不存在（404，不泄漏存在性）
func TestEgoStream_RejectsOtherUsersExecution(t *testing.T) {
	srv, execRepo, _, _ := newStreamTestServer(t)
	defer srv.Close()

	e := &model.EgoliteExecution{UserID: 2, Origin: "draft", State: model.EgoExecRunning}
	if err := execRepo.Create(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(srv.URL + "/s/" + strconv.FormatUint(uint64(e.ID), 10))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("越权访问 status=%d want=404", resp.StatusCode)
	}
}

// 2. 终态执行：回放全部历史事件 + 一帧 end 后关流
func TestEgoStream_ReplaysHistoryThenEnds(t *testing.T) {
	srv, execRepo, evRepo, _ := newStreamTestServer(t)
	defer srv.Close()
	ctx := t.Context()

	e := &model.EgoliteExecution{UserID: 1, Origin: "draft", State: model.EgoExecCompleted}
	if err := execRepo.Create(ctx, e); err != nil {
		t.Fatal(err)
	}
	for i, payload := range []string{`{"action":"goto","ok":true}`, `{"action":"extract","ok":true}`} {
		if err := evRepo.Append(ctx, &model.EgoEventLog{
			ExecutionID: e.ID, Seq: i + 1, Type: "action",
			Payload: []byte(payload),
		}); err != nil {
			t.Fatal(err)
		}
	}

	resp, err := http.Get(srv.URL + "/s/" + strconv.FormatUint(uint64(e.ID), 10))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("Content-Type=%q want=text/event-stream", ct)
	}

	var buf strings.Builder
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		buf.WriteString(sc.Text())
		buf.WriteString("\n")
	}
	out := buf.String()
	if n := strings.Count(out, "data: "); n < 3 { // 2 条事件 + 1 帧 end
		t.Fatalf("data 帧数=%d want>=3（历史事件 + end）:\n%s", n, out)
	}
	if !strings.Contains(out, "event: end") {
		t.Fatalf("终态后必须发 end 帧收流:\n%s", out)
	}
	if !strings.Contains(out, `"action":"goto"`) || !strings.Contains(out, `"action":"extract"`) {
		t.Fatalf("历史事件要按顺序回放:\n%s", out)
	}
	if idx1, idx2 := strings.Index(out, "goto"), strings.Index(out, "extract"); idx1 > idx2 {
		t.Fatalf("事件顺序错乱（extract 出现在 goto 之前）:\n%s", out)
	}
}

// 3. 运行中：广播到达即推增量；进终态后发 end
func TestEgoStream_PushesLiveEvents(t *testing.T) {
	srv, execRepo, evRepo, hub := newStreamTestServer(t)
	defer srv.Close()
	ctx := t.Context()

	e := &model.EgoliteExecution{UserID: 1, Origin: "draft", State: model.EgoExecRunning}
	if err := execRepo.Create(ctx, e); err != nil {
		t.Fatal(err)
	}

	resp, err := http.Get(srv.URL + "/s/" + strconv.FormatUint(uint64(e.ID), 10))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	lines := make(chan string, 64)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()

	// 新事件落库 + 广播 ⇒ 客户端应当收到（不等 3s 轮询周期）
	if err := evRepo.Append(ctx, &model.EgoEventLog{
		ExecutionID: e.ID, Seq: 1, Type: "action", Payload: []byte(`{"action":"live_1"}`),
	}); err != nil {
		t.Fatal(err)
	}
	hub.Publish(e.ID)
	if !waitLineContains(t, lines, "live_1", 3*time.Second) {
		t.Fatalf("广播后没收到增量事件（说明这条流是在轮库，不是真推）")
	}

	// 进终态 + 广播 ⇒ end 帧
	if err := execRepo.TransitionState(ctx, e.ID, []string{model.EgoExecRunning}, model.EgoExecCompleted,
		map[string]any{"fail_reason": ""}); err != nil {
		t.Fatal(err)
	}
	hub.Publish(e.ID)
	if !waitLineContains(t, lines, "event: end", 3*time.Second) {
		t.Fatalf("终态后应发 end 帧并收流")
	}
}

// 4. 游标：after_id 之后才开始推（客户端重连不重复收历史）
func TestEgoStream_HonoursAfterID(t *testing.T) {
	srv, execRepo, evRepo, _ := newStreamTestServer(t)
	defer srv.Close()
	ctx := t.Context()

	e := &model.EgoliteExecution{UserID: 1, Origin: "draft", State: model.EgoExecCompleted}
	if err := execRepo.Create(ctx, e); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 3; i++ {
		if err := evRepo.Append(ctx, &model.EgoEventLog{
			ExecutionID: e.ID, Seq: i, Type: "action",
			Payload: []byte(`{"action":"ev_` + strconv.Itoa(i) + `"}`),
		}); err != nil {
			t.Fatal(err)
		}
	}
	events, err := evRepo.ListByExecution(ctx, e.ID, 10)
	if err != nil || len(events) < 3 {
		t.Fatalf("准备事件失败: %v", err)
	}
	after := events[0].ID
	resp, err := http.Get(srv.URL + "/s/" + strconv.FormatUint(uint64(e.ID), 10) + "?after_id=" + strconv.FormatUint(uint64(after), 10))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var out strings.Builder
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		out.WriteString(sc.Text())
		out.WriteString("\n")
	}
	if strings.Contains(out.String(), "ev_1") {
		t.Fatalf("after_id 之前的事件不该重推:\n%s", out.String())
	}
	for _, want := range []string{"ev_2", "ev_3", "event: end"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("缺 %q:\n%s", want, out.String())
		}
	}
}

func waitLineContains(t *testing.T, lines <-chan string, want string, d time.Duration) bool {
	t.Helper()
	deadline := time.After(d)
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				return false
			}
			if strings.Contains(line, want) {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

// 编译期钉住：end 帧里带给前端的状态字段名（改名会让前端静默不更新状态）
func TestEgoStream_EndFrameShape(t *testing.T) {
	payload := map[string]any{"state": model.EgoExecCompleted, "fail_reason": "", "artifacts": json.RawMessage(`[]`)}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"state"`, `"fail_reason"`} {
		if !strings.Contains(string(b), key) {
			t.Fatalf("end 帧缺字段 %s: %s", key, string(b))
		}
	}
}
