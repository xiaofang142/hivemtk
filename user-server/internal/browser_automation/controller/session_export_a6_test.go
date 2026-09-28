package controller

// 批22（A6）最后一跳：导出的 HTTP 载荷里到底有没有那一份摘要。
//
// 服务层用例（session_export_test.go）只证明 SessionExport 回了这个值；控制器把六元组
// 摊成 gin.H 的那一行是另一个断点。这一跳断掉的代价不是报错，而是**说不出话**：
// 一个裁过的会话导出来 command_log 为空，读者分不清「确实没被裁、只是没内容」和
// 「装配时漏了一行 setter」。所以这里除了正锁，还专门锁了 null / [] 这两态。
//
// 造数据一律走真库（testutil.NewTestDB）而不是假仓储：摘要行的 batch_digest 是库内
// sha256 折出来的十六进制串，导出这条路上任何一处把它换掉/吞掉，都得在 JSON 上看见。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"hivemtk-user/internal/browser_automation/model"
	barepo "hivemtk-user/internal/browser_automation/repository"
	basvc "hivemtk-user/internal/browser_automation/service"
	"hivemtk-user/internal/pkg/testutil"

	"github.com/gin-gonic/gin"
)

const (
	b22Owner    = uint(782221)
	b22SessionA = uint(782222)
	b22SessionB = uint(782223) // 一条从没被裁过的会话：用来量「装配了但空」那一态
)

// b22ExportData 打一次 Export，返回响应体的 data 分量（原始 map，不经 handler 之外的翻译）。
func b22ExportData(t *testing.T, c *SessionController, id string) map[string]any {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := b22Do(t, c.Export, http.MethodGet, "/api/browser-automation/sessions/"+id+"/export", id)
	var envelope struct {
		Code int            `json:"code"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("导出响应不是合法 JSON（%v）：%s", err, rec.Body.String())
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("导出 HTTP %d，want 200：%s", rec.Code, rec.Body.String())
	}
	return envelope.Data
}

// b22Digests 取 data.audit_digests，并保留「这个键到底是 null、[]、还是压根不存在」的区分
// ——三态正是本文件要锁的东西，所以这里不能先 json.Unmarshal 到 []T 把差异抹平。
func b22Digests(t *testing.T, data map[string]any) (raw json.RawMessage, present bool) {
	t.Helper()
	v, ok := data["audit_digests"]
	if !ok {
		return nil, false
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("audit_digests 无法再序列化：%v", err)
	}
	return b, true
}

func TestA6ExportPayloadCarriesAuditDigests(t *testing.T) {
	db := testutil.NewTestDB(t, &model.BrowserSession{}, &model.BrowserStep{},
		&model.BrowserCommandLog{}, &model.BrowserAuditDigest{}, &model.BrowserAuditPruneRun{})
	if db == nil {
		t.Fatal("测试库不可达：导下载荷这一跳无法判定（Skip 等于没锁）")
	}
	if err := db.Create(&model.BrowserSession{ID: b22SessionA, TaskID: 1, UserID: b22Owner, Status: "completed"}).Error; err != nil {
		t.Fatalf("造会话失败：%v", err)
	}
	// 两条摘要、故意按 ordinal 逆序入库：导出必须按**链序**给出，否则读者拿到的是一份
	// 需要自己重排才能首尾相接的清单（链式摘要不排序就等于没链）。
	seeds := []*model.BrowserAuditDigest{
		{SessionID: b22SessionA, RowCount: 3, FirstSeq: 4, LastSeq: 6, PrevSeq: 3, Ordinal: 2,
			BatchDigest: "bb", PrevChainHash: "h1", ChainHash: "h2", Cutoff: b22Cutoff(t)},
		{SessionID: b22SessionA, RowCount: 3, FirstSeq: 1, LastSeq: 3, PrevSeq: 0, Ordinal: 1,
			BatchDigest: "aa", PrevChainHash: "", ChainHash: "h1", Cutoff: b22Cutoff(t)},
	}
	for i := range seeds {
		if err := db.Create(seeds[i]).Error; err != nil {
			t.Fatalf("造摘要失败：%v", err)
		}
	}

	sessionRepo := barepo.NewBrowserSessionRepositoryWithDB(db)
	stepRepo := barepo.NewBrowserStepRepositoryWithDB(db)
	svc := basvc.NewSessionService(sessionRepo, stepRepo, nil)
	svc.SetCommandLogRepository(barepo.NewBrowserCommandLogRepositoryWithDB(db))
	svc.SetAuditDigestRepository(barepo.NewBrowserAuditDigestRepositoryWithDB(db))

	data := b22ExportData(t, NewSessionController(svc), "782222")
	raw, present := b22Digests(t, data)
	if !present {
		t.Fatalf("导出载荷没有 audit_digests 键：导出的审计包没有能力说明「被裁掉的那段是什么」。keys=%v", b22Keys(data))
	}
	var rows []map[string]any
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("audit_digests 不是数组：%s", raw)
	}
	if len(rows) != 2 {
		t.Fatalf("摘要行数 %d want 2（一条会话被裁两批就得导出两批）：%s", len(rows), raw)
	}
	if got := rows[0]["ordinal"]; got != float64(1) {
		t.Errorf("首行 ordinal=%v want 1：导出顺序必须是链序，逆序等于把断链检查留给读者", got)
	}
	if got := rows[1]["ordinal"]; got != float64(2) {
		t.Errorf("次行 ordinal=%v want 2", got)
	}
	// 逐行钉内容：只数行数等于什么都没锁（少导出一列、或把 digest 换成别的表，照样 2 行）
	for i, want := range []map[string]any{
		{"session_id": float64(b22SessionA), "ordinal": float64(1), "batch_digest": "aa", "chain_hash": "h1", "prev_chain_hash": "", "row_count": float64(3), "first_seq": float64(1), "last_seq": float64(3), "prev_seq": float64(0)},
		{"session_id": float64(b22SessionA), "ordinal": float64(2), "batch_digest": "bb", "chain_hash": "h2", "prev_chain_hash": "h1", "row_count": float64(3), "first_seq": float64(4), "last_seq": float64(6), "prev_seq": float64(3)},
	} {
		for k, v := range want {
			if got := rows[i][k]; got != v {
				t.Errorf("第 %d 行 %s=%v want %v", i+1, k, got, v)
			}
		}
	}
	// 链的接续关系要在这份离线包里当场可读（拿包的人手上没有库）
	if rows[0]["prev_chain_hash"] != "" || rows[1]["prev_chain_hash"] != rows[0]["chain_hash"] {
		t.Errorf("链序断裂：%v → %v", rows[0]["chain_hash"], rows[1]["prev_chain_hash"])
	}
}

// 三态锁：`键不存在`（控制器漏了那一行）/ `null`（服务没接摘要读侧）/ `[]`（接了，这个会话确实没被裁）
// 必须是三种可区分的输出。把后两态压成同一个值，等于让「装配缺一行」冒充「历史本来干净」——
// 那正是本卡立项要拆穿的那类沉默。
func TestA6ExportDistinguishesUnwiredFromNothingPruned(t *testing.T) {
	db := testutil.NewTestDB(t, &model.BrowserSession{}, &model.BrowserStep{},
		&model.BrowserCommandLog{}, &model.BrowserAuditDigest{}, &model.BrowserAuditPruneRun{})
	if db == nil {
		t.Fatal("测试库不可达：三态判读无法进行")
	}
	if err := db.Create(&model.BrowserSession{ID: b22SessionB, TaskID: 1, UserID: b22Owner, Status: "completed"}).Error; err != nil {
		t.Fatalf("造会话失败：%v", err)
	}
	build := func(withDigest bool) *SessionController {
		svc := basvc.NewSessionService(barepo.NewBrowserSessionRepositoryWithDB(db),
			barepo.NewBrowserStepRepositoryWithDB(db), nil)
		svc.SetCommandLogRepository(barepo.NewBrowserCommandLogRepositoryWithDB(db))
		if withDigest {
			svc.SetAuditDigestRepository(barepo.NewBrowserAuditDigestRepositoryWithDB(db))
		}
		return NewSessionController(svc)
	}

	dataWired := b22ExportData(t, build(true), "782223")
	rawWired, present := b22Digests(t, dataWired)
	if !present {
		t.Fatalf("装配齐全时导出没有 audit_digests 键：keys=%v", b22Keys(dataWired))
	}
	if string(rawWired) != "[]" {
		t.Errorf("装配了摘要读侧、该会话没被裁过 → audit_digests=%s，want []（空数组是「查过、确实没有」这句断言）", rawWired)
	}

	rawUnwired, present2 := b22Digests(t, b22ExportData(t, build(false), "782223"))
	if !present2 {
		t.Fatal("未装配时连键都没了：前端无从判断这一项是漏装还是不存在")
	}
	if string(rawUnwired) != "null" {
		t.Errorf("未装配摘要读侧 → audit_digests=%s，want null（[] 会把它谎称成「这个会话没被裁过」）", rawUnwired)
	}
	if string(rawWired) == string(rawUnwired) {
		t.Errorf("两态折叠成 %s：漏一行 setter 与干净的库再也分不开", rawWired)
	}
}

// b22Do 与 b19cCall 同一形状，区别是它交回**原始 recorder**：本文件判的是键在不在、
// 是 null 还是 []，errBody 那层结构体恰好会把这两种输出折成同一个零值。
func b22Do(t *testing.T, h gin.HandlerFunc, method, path, idParam string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest(method, path, nil)
	ctx.Params = gin.Params{{Key: "id", Value: idParam}}
	ctx.Set("user_id", b22Owner)
	h(ctx)
	return w
}

// b22Cutoff 取一个必然在过去的时间界：cutoff 列是 not null，且它进的是摘要行的读数，
// 不参与本文件任何判据，只要非零即可。
func b22Cutoff(t *testing.T) time.Time {
	t.Helper()
	return time.Now().Add(-90 * 24 * time.Hour).UTC()
}

func b22Keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
