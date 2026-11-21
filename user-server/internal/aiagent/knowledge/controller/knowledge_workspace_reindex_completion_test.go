package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
)

// 记录真正发送 200 时的数据库终态，避免只靠 goroutine 调度时机判断是否提前成功。
type reindexCompletionWriter struct {
	*httptest.ResponseRecorder
	completed func() bool
	early     bool
}

func (w *reindexCompletionWriter) WriteHeader(code int) {
	if code == http.StatusOK && !w.completed() {
		w.early = true
	}
	w.ResponseRecorder.WriteHeader(code)
}

// 非空产品受控 embedding 未完成前不得返回 success；发出 200 时必须已有可查询 indexed 终态。
func TestRebuildProductIndex_NonemptyWaitsForEmbeddingCompletion(t *testing.T) {
	f := setupReindexControllerFixture(t, false)
	started, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list", "data": []map[string]any{{"index": 0, "embedding": make([]float32, 1024)}}, "model": "synthetic",
		})
	}))
	t.Cleanup(provider.Close)
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	// 此 fixture 的专属 schema 内补 text-backed vector；不安装扩展或修改业务库。
	fixtureDDL := f.db.WithContext(tenant.WithSystemScope(f.ctx))
	if err := fixtureDDL.Exec(`CREATE DOMAIN vector AS text`).Error; err != nil {
		t.Fatal(err)
	}
	if err := fixtureDDL.Exec(`ALTER TABLE knowledge_chunks ADD COLUMN embedding text, ADD COLUMN embed_status text`).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.WithContext(f.ctx).Model(&model.RagProduct{}).Where("id = ?", f.productID).
		Updates(map[string]any{"emb_base_url": provider.URL, "emb_model": "synthetic", "emb_dimension": 1024}).Error; err != nil {
		t.Fatal(err)
	}
	doc := model.KnowledgeDocument{TenantID: 413, ProductID: f.productID, Title: "controlled completion", Metadata: "{}"}
	if err := f.db.WithContext(f.ctx).Create(&doc).Error; err != nil {
		t.Fatal(err)
	}
	chunk := model.KnowledgeChunk{TenantID: 413, DocumentID: doc.ID, ProductID: f.productID, Content: "controlled embedding input", Metadata: "{}"}
	if err := f.db.WithContext(f.ctx).Create(&chunk).Error; err != nil {
		t.Fatal(err)
	}
	readDoc := func() (model.KnowledgeDocument, error) {
		var stored model.KnowledgeDocument
		err := f.db.WithContext(f.ctx).Where("id = ?", doc.ID).First(&stored).Error
		return stored, err
	}
	recorder := &reindexCompletionWriter{ResponseRecorder: httptest.NewRecorder(), completed: func() bool {
		stored, err := readDoc()
		return err == nil && stored.EmbedStatus == model.EmbedStatusIndexed && stored.ReindexRetryCount == 1
	}}
	req := httptest.NewRequest(http.MethodPost, "/products/"+f.productID+"/rebuild-index", nil).WithContext(f.ctx)
	done := make(chan struct{})
	go func() {
		f.router.ServeHTTP(recorder, req)
		close(done)
	}()
	select {
	case <-started:
	case <-done:
		t.Fatalf("embedding 完成前已返回响应，code=%d body=%s", recorder.Code, recorder.Body.String())
	case <-time.After(5 * time.Second):
		t.Fatal("controller 未到达受控 embedding 屏障")
	}
	stored, err := readDoc()
	if err != nil || stored.EmbedStatus != model.EmbedStatusProcessing || stored.ReindexRetryCount != 1 {
		t.Fatalf("屏障处应保持 processing/1，doc=%+v err=%v", stored, err)
	}
	select {
	case <-done:
		t.Fatal("embedding 仍阻塞时不得返回响应")
	default:
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("释放 embedding 后 controller 未结束")
	}
	var response struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if recorder.early || recorder.Code != http.StatusOK || response.Code != 0 || response.Message != "重建完成" {
		t.Fatalf("200 必须在 indexed 落库后发出，early=%v code=%d body=%s", recorder.early, recorder.Code, recorder.Body.String())
	}
	stored, err = readDoc()
	if err != nil || stored.EmbedStatus != model.EmbedStatusIndexed || stored.ReindexRetryCount != 1 {
		t.Fatalf("响应后必须可查询 indexed/1，doc=%+v err=%v", stored, err)
	}
}

var _ http.ResponseWriter = (*reindexCompletionWriter)(nil)
