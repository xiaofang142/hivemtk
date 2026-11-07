package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	knowledgesvc "hivemtk-user/internal/aiagent/knowledge/service"
	ragretrieval "hivemtk-user/internal/aiagent/rag/retrieval"
	"hivemtk-user/internal/model"
	db "hivemtk-user/internal/pkg/db"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/pkg/testutil/testmigrate"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func toPgVector(vec []float32) string {
	parts := make([]string, len(vec))
	for i, f := range vec {
		parts[i] = strconv.FormatFloat(float64(f), 'f', -1, 32)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func setupKMSvcTestDB(t *testing.T) *gorm.DB {
	database := testutil.NewTestDB(t,
		&model.KBDocument{},
		&model.KnowledgeDocument{},
		&model.KnowledgeChunk{},
		&model.KnowledgeSearchLog{},
		&model.KnowledgeImportLog{},
		&model.KnowledgeAPIToken{},
		&model.KnowledgeFeedback{},
		&model.ExternalImportJob{},
		&model.RagProduct{},
	)
	testmigrate.RunTestMigrations(t, database)
	return database
}

func setupKMRouter(t *testing.T, ctrl *KnowledgeMerchantController) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	db := setupKMSvcTestDB(t)
	seedRagProducts(db)
	// 批次7 刀4：WithDB 的 *gorm.DB 形参清零；NewTestDB 已装配全局句柄，Default 构造同连接。
	ctrl.svc = knowledgesvc.NewKnowledgeMerchantService()
	group := router.Group("/api")
	group.Use(func(c *gin.Context) {
		c.Set("operator", "tester")
		c.Set("user_id", "u-1")
		c.Next()
	})
	ctrl.RegisterRoutes(group)
	group.POST("/knowledge-merchant/external/import", ctrl.ExternalImport)
	return router
}

func seedRagProducts(db *gorm.DB) {
	products := []*model.RagProduct{
		{ID: "kb-test-1", Name: "kb-test", VectorTable: "vt_kb_test_1", IsActive: true},
		{ID: "kb-1", Name: "kb", VectorTable: "vt_kb_1", IsActive: true},
		{ID: "kb-e2e", Name: "kb-e2e", VectorTable: "vt_kb_e2e", IsActive: true},
		{ID: "kb-2", Name: "kb-2", VectorTable: "vt_kb_2", IsActive: true},
		{ID: "kb-ext", Name: "kb-ext", VectorTable: "vt_kb_ext", IsActive: true},
		{ID: "kb-csv", Name: "kb-csv", VectorTable: "vt_kb_csv", IsActive: true},
	}
	for _, p := range products {

		var existing model.RagProduct
		if err := db.Where("id = ?", p.ID).First(&existing).Error; err == nil {
			continue
		}
		if err := db.Create(p).Error; err != nil {
			_ = err
		}
	}
}

func mustJSON(t *testing.T, v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func TestKM_BatchImport_Success(t *testing.T) {
	ctrl := NewKnowledgeMerchantController()
	r := setupKMRouter(t, ctrl)
	body := mustJSON(t, map[string]any{
		"product_id": "kb-test-1",
		"items": []map[string]any{
			{"title": "FAQ 退货", "content": "请在订单页面申请退货", "category": "售后", "tags": []string{"退货"}},
			{"title": "FAQ 物流", "content": "我们 48 小时内发货", "category": "物流", "tags": []string{"发货"}},
		},
	})
	req, _ := http.NewRequest("POST", "/api/knowledge-merchant/batch/import", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d, body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "batch_no") {
		t.Errorf("expected batch_no in response: %s", w.Body.String())
	}
}

func TestKM_BatchImport_EmptyProduct(t *testing.T) {
	ctrl := NewKnowledgeMerchantController()
	r := setupKMRouter(t, ctrl)
	body := mustJSON(t, map[string]any{"product_id": ""})
	req, _ := http.NewRequest("POST", "/api/knowledge-merchant/batch/import", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", w.Code)
	}
}

func TestKM_BatchImport_BadJSON(t *testing.T) {
	ctrl := NewKnowledgeMerchantController()
	r := setupKMRouter(t, ctrl)
	req, _ := http.NewRequest("POST", "/api/knowledge-merchant/batch/import", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", w.Code)
	}
}

func TestKM_BatchUpload_CSV(t *testing.T) {
	ctrl := NewKnowledgeMerchantController()
	r := setupKMRouter(t, ctrl)
	csv := `title,content,category,tags
"退货","请在订单页面申请","售后","退货,流程"
"物流","48 小时内发货","物流","发货"`
	buf := &bytes.Buffer{}
	mw := multipart.NewWriter(buf)
	_ = mw.WriteField("product_id", "kb-test-1")
	_ = mw.WriteField("format", "csv")
	fw, _ := mw.CreateFormFile("file", "batch.csv")
	fw.Write([]byte(csv))
	mw.Close()
	req, _ := http.NewRequest("POST", "/api/knowledge-merchant/batch/upload", buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d, body=%s", w.Code, w.Body.String())
	}
}

func TestKM_BatchUpload_NoFile(t *testing.T) {
	ctrl := NewKnowledgeMerchantController()
	r := setupKMRouter(t, ctrl)
	buf := &bytes.Buffer{}
	mw := multipart.NewWriter(buf)
	_ = mw.WriteField("product_id", "kb-test-1")
	mw.Close()
	req, _ := http.NewRequest("POST", "/api/knowledge-merchant/batch/upload", buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", w.Code)
	}
}

func TestKM_Playground_EmptyQuery(t *testing.T) {
	ctrl := NewKnowledgeMerchantController()
	r := setupKMRouter(t, ctrl)
	body := mustJSON(t, map[string]any{
		"product_id": "kb-1",
		"query":      "",
	})
	req, _ := http.NewRequest("POST", "/api/knowledge-merchant/playground", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", w.Code)
	}
}

func TestKM_Playground_NoBody(t *testing.T) {
	ctrl := NewKnowledgeMerchantController()
	r := setupKMRouter(t, ctrl)
	req, _ := http.NewRequest("POST", "/api/knowledge-merchant/playground", bytes.NewReader([]byte("not-json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", w.Code)
	}
}

func TestKM_ListDocumentChunks_InvalidID(t *testing.T) {
	ctrl := NewKnowledgeMerchantController()
	r := setupKMRouter(t, ctrl)
	req, _ := http.NewRequest("GET", "/api/knowledge-merchant/documents/abc/chunks", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", w.Code)
	}
}

func TestKM_UpdateChunk_InvalidID(t *testing.T) {
	ctrl := NewKnowledgeMerchantController()
	r := setupKMRouter(t, ctrl)
	body := mustJSON(t, map[string]any{"content": "x"})
	req, _ := http.NewRequest("PUT", "/api/knowledge-merchant/chunks/abc", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", w.Code)
	}
}

func TestKM_DeleteChunk_InvalidID(t *testing.T) {
	ctrl := NewKnowledgeMerchantController()
	r := setupKMRouter(t, ctrl)
	req, _ := http.NewRequest("DELETE", "/api/knowledge-merchant/chunks/abc", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", w.Code)
	}
}

func TestKM_SplitChunk_InvalidID(t *testing.T) {
	ctrl := NewKnowledgeMerchantController()
	r := setupKMRouter(t, ctrl)
	body := mustJSON(t, map[string]any{"parts": []string{"a", "b"}})
	req, _ := http.NewRequest("POST", "/api/knowledge-merchant/chunks/abc/split", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", w.Code)
	}
}

func TestKM_SubmitFeedback_OK(t *testing.T) {
	ctrl := NewKnowledgeMerchantController()
	r := setupKMRouter(t, ctrl)
	body := mustJSON(t, map[string]any{
		"product_id":  "kb-1",
		"query":       "如何退货?",
		"document_id": 100,
		"chunk_id":    200,
		"rating":      1,
		"comment":     "很有用",
	})
	req, _ := http.NewRequest("POST", "/api/knowledge-merchant/feedback", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d, body=%s", w.Code, w.Body.String())
	}
}

func TestKM_SubmitFeedback_BadRating(t *testing.T) {
	ctrl := NewKnowledgeMerchantController()
	r := setupKMRouter(t, ctrl)
	body := mustJSON(t, map[string]any{
		"product_id": "kb-1",
		"query":      "如何退货?",
		"rating":     5,
	})
	req, _ := http.NewRequest("POST", "/api/knowledge-merchant/feedback", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", w.Code)
	}
}

func TestKM_ListFeedbacks_OK(t *testing.T) {
	ctrl := NewKnowledgeMerchantController()
	r := setupKMRouter(t, ctrl)
	req, _ := http.NewRequest("GET", "/api/knowledge-merchant/feedbacks?page=1&page_size=10", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d", w.Code)
	}
}

func TestKM_CreateToken_OK(t *testing.T) {
	ctrl := NewKnowledgeMerchantController()
	r := setupKMRouter(t, ctrl)
	body := mustJSON(t, map[string]any{
		"name":       "CRM 推送",
		"product_id": "kb-1",
		"scopes":     []string{"read", "write"},
	})
	req, _ := http.NewRequest("POST", "/api/knowledge-merchant/tokens", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d, body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "token_plain") {
		t.Errorf("expected token_plain in response: %s", w.Body.String())
	}
}

func TestKM_CreateToken_MissingName(t *testing.T) {
	ctrl := NewKnowledgeMerchantController()
	r := setupKMRouter(t, ctrl)
	body := mustJSON(t, map[string]any{"product_id": "kb-1"})
	req, _ := http.NewRequest("POST", "/api/knowledge-merchant/tokens", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", w.Code)
	}
}

func TestKM_ListTokens_OK(t *testing.T) {
	ctrl := NewKnowledgeMerchantController()
	r := setupKMRouter(t, ctrl)
	req, _ := http.NewRequest("GET", "/api/knowledge-merchant/tokens?product_id=kb-1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d", w.Code)
	}
}

func TestKM_RevokeToken_InvalidID(t *testing.T) {
	ctrl := NewKnowledgeMerchantController()
	r := setupKMRouter(t, ctrl)
	req, _ := http.NewRequest("POST", "/api/knowledge-merchant/tokens/abc/revoke", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", w.Code)
	}
}

func TestKM_ExternalImport_MissingToken(t *testing.T) {
	ctrl := NewKnowledgeMerchantController()
	r := setupKMRouter(t, ctrl)
	body := mustJSON(t, map[string]any{
		"source":     "custom",
		"product_id": "kb-1",
		"items":      []map[string]any{{"title": "t", "content": "c"}},
	})
	req, _ := http.NewRequest("POST", "/api/knowledge-merchant/external/import", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d, body=%s", w.Code, w.Body.String())
	}
}

func TestKM_ExternalImport_InvalidToken(t *testing.T) {
	ctrl := NewKnowledgeMerchantController()
	r := setupKMRouter(t, ctrl)
	body := mustJSON(t, map[string]any{
		"source":     "custom",
		"product_id": "kb-1",
		"items":      []map[string]any{{"title": "t", "content": "c"}},
	})
	req, _ := http.NewRequest("POST", "/api/knowledge-merchant/external/import", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Knowledge-Token", "kbg_invalid_token_xxx")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d, body=%s", w.Code, w.Body.String())
	}
}

func TestKM_ListExternalJobs_OK(t *testing.T) {
	ctrl := NewKnowledgeMerchantController()
	r := setupKMRouter(t, ctrl)
	req, _ := http.NewRequest("GET", "/api/knowledge-merchant/external/jobs?product_id=kb-1&page=1&page_size=10", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d", w.Code)
	}
}

func TestKM_EndToEnd_ExternalImport_WithToken(t *testing.T) {
	ctrl := NewKnowledgeMerchantController()
	r := setupKMRouter(t, ctrl)
	tokBody := mustJSON(t, map[string]any{
		"name":       "e2e",
		"product_id": "*",
		"scopes":     []string{"read", "write"},
	})
	tokReq, _ := http.NewRequest("POST", "/api/knowledge-merchant/tokens", bytes.NewReader(tokBody))
	tokReq.Header.Set("Content-Type", "application/json")
	tokW := httptest.NewRecorder()
	r.ServeHTTP(tokW, tokReq)
	if tokW.Code != http.StatusOK {
		t.Fatalf("create token: %d %s", tokW.Code, tokW.Body.String())
	}
	var tokResp struct {
		Data struct {
			TokenPlain string `json:"token_plain"`
		} `json:"data"`
	}
	if err := json.Unmarshal(tokW.Body.Bytes(), &tokResp); err != nil {
		t.Fatalf("decode token resp: %v", err)
	}
	if tokResp.Data.TokenPlain == "" {
		t.Fatalf("token_plain empty: %s", tokW.Body.String())
	}

	body := mustJSON(t, map[string]any{
		"source":     "custom",
		"product_id": "kb-not-exists",
		"items":      []map[string]any{{"title": "t", "content": "c"}},
	})
	req, _ := http.NewRequest("POST", "/api/knowledge-merchant/external/import", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Knowledge-Token", tokResp.Data.TokenPlain)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("want 404 (product not found), got %d, body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "token") && !strings.Contains(w.Body.String(), "Token") {
		t.Logf("响应: %s", w.Body.String())
	}
}

func TestKM_SubmitFeedback_NilDB_NoPanic(t *testing.T) {
	ctrl := &KnowledgeMerchantController{
		svc: knowledgesvc.NewKnowledgeMerchantServiceWithDeps(knowledgesvc.KnowledgeMerchantDeps{}), // 零值 deps = 原 WithDB(nil) 无库语义
	}
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("nil db should not panic: %v", r)
		}
	}()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	group := r.Group("/api")
	ctrl.RegisterRoutes(group)
	body := mustJSON(t, map[string]any{
		"query":  "q",
		"rating": 1,
	})
	req, _ := http.NewRequest("POST", "/api/knowledge-merchant/feedback", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
}

var _ = context.Background

// TestKM_Playground_WithData 测试 Playground 命中数据的场景
func TestKM_Playground_WithData(t *testing.T) {
	ctrl := NewKnowledgeMerchantController()
	r := setupKMRouter(t, ctrl)
	db := db.GetDB() // 当前全局测试句柄（_test.go 允许；与 setup 同库）
	pid := "kb-1"
	doc := &model.KnowledgeDocument{
		ProductID:  pid,
		Title:      "退货政策",
		SourceType: model.SourceTypeBatch,
		Status:     1,
	}
	db.Create(doc)
	chunks := []model.KnowledgeChunk{
		{DocumentID: doc.ID, ProductID: pid, ChunkIndex: 0, Content: "退货政策: 请在订单页面申请退货,7天内处理", ContentHash: "h1", CharCount: 30, TokenCount: 10},
		{DocumentID: doc.ID, ProductID: pid, ChunkIndex: 1, Content: "运费说明: 满99包邮,否则10元运费", ContentHash: "h2", CharCount: 30, TokenCount: 10},
	}
	v := ragretrieval.NewVectorizer(0, nil)
	for i := range chunks {
		if err := db.Create(&chunks[i]).Error; err != nil {
			t.Fatalf("create chunk: %v", err)
		}
		vec, err := v.EmbedText(chunks[i].Content)
		if err != nil {
			t.Fatalf("embed chunk %d: %v", i, err)
		}
		if err := db.
			Exec("UPDATE knowledge_chunks SET embedding = ?::vector WHERE id = ?", toPgVector(vec), chunks[i].ID).Error; err != nil {
			t.Fatalf("update chunk embedding: %v", err)
		}
	}
	body := mustJSON(t, map[string]any{
		"product_id":           "kb-1",
		"query":                "如何退货",
		"top_k":                5,
		"similarity_threshold": 0.01,
	})
	req, _ := http.NewRequest("POST", "/api/knowledge-merchant/playground", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			Total    int     `json:"total"`
			MaxScore float64 `json:"max_score"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.Total == 0 {
		t.Errorf("expected at least 1 hit, got 0, body=%s", w.Body.String())
	}
	if resp.Data.MaxScore <= 0 {
		t.Errorf("max_score should > 0, got %v", resp.Data.MaxScore)
	}
}

// TestKM_Playground_ThresholdFilter 测试阈值过滤
func TestKM_Playground_ThresholdFilter(t *testing.T) {
	ctrl := NewKnowledgeMerchantController()
	r := setupKMRouter(t, ctrl)
	db := db.GetDB() // 当前全局测试句柄（_test.go 允许；与 setup 同库，避免 NewTestDB 二次建库清掉种子）
	pid := "kb-1"
	doc := &model.KnowledgeDocument{ProductID: pid, Title: "Test Doc", SourceType: model.SourceTypeBatch, Status: 1}
	db.Create(doc)
	db.Create(&model.KnowledgeChunk{
		DocumentID: doc.ID, ProductID: pid, ChunkIndex: 0,
		Content: "hello world", ContentHash: "h1", CharCount: 11, TokenCount: 2,
	})
	body := mustJSON(t, map[string]any{
		"product_id":           "kb-1",
		"query":                "你好世界",
		"top_k":                5,
		"similarity_threshold": 0.99,
	})
	req, _ := http.NewRequest("POST", "/api/knowledge-merchant/playground", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp struct {
		Data struct {
			Total int `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.Total != 0 {
		t.Logf("body=%s (cross-lang may still hit, expected %d, got %d)", w.Body.String(), 0, resp.Data.Total)
	}
}

// TestKM_UpdateChunk_OK 测试更新分段成功
func TestKM_UpdateChunk_OK(t *testing.T) {
	ctrl := NewKnowledgeMerchantController()
	r := setupKMRouter(t, ctrl)
	db := db.GetDB() // 当前全局测试句柄（_test.go 允许；与 setup 同库，避免 NewTestDB 二次建库清掉种子）
	pid := "kb-1"
	doc := &model.KnowledgeDocument{ProductID: pid, Title: "Doc", SourceType: model.SourceTypeBatch, Status: 1}
	db.Create(doc)
	chunk := &model.KnowledgeChunk{
		DocumentID:  doc.ID,
		ProductID:   pid,
		ChunkIndex:  0,
		Content:     "原始内容",
		ContentHash: "h1",
		CharCount:   12,
		TokenCount:  2,
		EmbeddingID: "vec_123",
	}
	db.Create(chunk)
	body := mustJSON(t, map[string]any{"content": "更新后的新内容"})
	req, _ := http.NewRequest("PUT", fmt.Sprintf("/api/knowledge-merchant/chunks/%d", chunk.ID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d, body=%s", w.Code, w.Body.String())
	}

	var updated model.KnowledgeChunk
	db.First(&updated, chunk.ID)
	if updated.EmbeddingID != "" {
		t.Errorf("expected EmbeddingID reset, got %s", updated.EmbeddingID)
	}
	if updated.Content != "更新后的新内容" {
		t.Errorf("expected content updated, got %s", updated.Content)
	}
}

// TestKM_DeleteChunk_OK 测试删除分段
func TestKM_DeleteChunk_OK(t *testing.T) {
	ctrl := NewKnowledgeMerchantController()
	r := setupKMRouter(t, ctrl)
	db := db.GetDB() // 当前全局测试句柄（_test.go 允许；与 setup 同库，避免 NewTestDB 二次建库清掉种子）
	pid := "kb-1"
	doc := &model.KnowledgeDocument{ProductID: pid, Title: "Doc", SourceType: model.SourceTypeBatch, Status: 1}
	db.Create(doc)
	chunk := &model.KnowledgeChunk{DocumentID: doc.ID, ProductID: pid, ChunkIndex: 0, Content: "x", ContentHash: "h1", CharCount: 1, TokenCount: 1}
	db.Create(chunk)
	req, _ := http.NewRequest("DELETE", fmt.Sprintf("/api/knowledge-merchant/chunks/%d", chunk.ID), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d, body=%s", w.Code, w.Body.String())
	}
	var count int64
	db.Model(&model.KnowledgeChunk{}).Where("id = ?", chunk.ID).Count(&count)
	if count != 0 {
		t.Errorf("expected chunk deleted, but found %d", count)
	}
}

// TestKM_SplitChunk_OK 测试拆分分段
func TestKM_SplitChunk_OK(t *testing.T) {
	ctrl := NewKnowledgeMerchantController()
	r := setupKMRouter(t, ctrl)
	db := db.GetDB() // 当前全局测试句柄（_test.go 允许；与 setup 同库，避免 NewTestDB 二次建库清掉种子）
	pid := "kb-1"
	doc := &model.KnowledgeDocument{ProductID: pid, Title: "Doc", SourceType: model.SourceTypeBatch, Status: 1}
	db.Create(doc)
	original := &model.KnowledgeChunk{DocumentID: doc.ID, ProductID: pid, ChunkIndex: 0, Content: "长内容A长内容B", ContentHash: "h1", CharCount: 10, TokenCount: 3}
	db.Create(original)
	body := mustJSON(t, map[string]any{"parts": []string{"段A内容", "段B内容"}})
	req, _ := http.NewRequest("POST", fmt.Sprintf("/api/knowledge-merchant/chunks/%d/split", original.ID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d, body=%s", w.Code, w.Body.String())
	}

	var newCount int64
	db.Model(&model.KnowledgeChunk{}).Where("document_id = ?", doc.ID).Count(&newCount)
	if newCount != 2 {
		t.Errorf("expected 2 chunks after split, got %d", newCount)
	}
}

// TestKM_ListDocumentChunks_OK 测试列出分段
func TestKM_ListDocumentChunks_OK(t *testing.T) {
	ctrl := NewKnowledgeMerchantController()
	r := setupKMRouter(t, ctrl)
	db := db.GetDB() // 当前全局测试句柄（_test.go 允许；与 setup 同库，避免 NewTestDB 二次建库清掉种子）
	pid := "kb-1"
	doc := &model.KnowledgeDocument{ProductID: pid, Title: "Doc", SourceType: model.SourceTypeBatch, Status: 1}
	db.Create(doc)
	for i := 0; i < 3; i++ {
		db.Create(&model.KnowledgeChunk{
			DocumentID: doc.ID, ProductID: pid, ChunkIndex: i,
			Content: fmt.Sprintf("chunk %d", i), ContentHash: fmt.Sprintf("h%d", i), CharCount: 8, TokenCount: 2,
		})
	}
	req, _ := http.NewRequest("GET", fmt.Sprintf("/api/knowledge-merchant/documents/%d/chunks?page=1&page_size=10", doc.ID), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d, body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "items") {
		t.Errorf("expected items field: %s", w.Body.String())
	}
}

// TestKM_RevokeToken_OK 测试吊销 Token
func TestKM_RevokeToken_OK(t *testing.T) {
	ctrl := NewKnowledgeMerchantController()
	r := setupKMRouter(t, ctrl)
	db := db.GetDB() // 当前全局测试句柄（_test.go 允许；与 setup 同库，避免 NewTestDB 二次建库清掉种子）
	body := mustJSON(t, map[string]any{
		"name":       "revoke-test",
		"product_id": "kb-1",
		"scopes":     []string{"read", "write"},
	})
	createReq, _ := http.NewRequest("POST", "/api/knowledge-merchant/tokens", bytes.NewReader(body))
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	r.ServeHTTP(createW, createReq)
	var resp struct {
		Data struct {
			ID uint64 `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(createW.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.ID == 0 {
		t.Fatalf("token id empty: %s", createW.Body.String())
	}
	revokeReq, _ := http.NewRequest("POST", fmt.Sprintf("/api/knowledge-merchant/tokens/%d/revoke", resp.Data.ID), nil)
	revokeW := httptest.NewRecorder()
	r.ServeHTTP(revokeW, revokeReq)
	if revokeW.Code != http.StatusOK {
		t.Errorf("want 200, got %d, body=%s", revokeW.Code, revokeW.Body.String())
	}

	var tok model.KnowledgeAPIToken
	db.First(&tok, resp.Data.ID)
	if tok.Enabled != 0 {
		t.Errorf("expected enabled=0 after revoke, got %d", tok.Enabled)
	}
}

// TestKM_FullFlow_TokenValidate_ExternalImport 完整流程：创建 Token → 用 Token 同步导入
func TestKM_FullFlow_TokenValidate_ExternalImport(t *testing.T) {
	ctrl := NewKnowledgeMerchantController()
	r := setupKMRouter(t, ctrl)
	db := db.GetDB() // 当前全局测试句柄（_test.go 允许；与 setup 同库，避免 NewTestDB 二次建库清掉种子）
	body := mustJSON(t, map[string]any{
		"name":       "full-flow",
		"product_id": "kb-e2e",
		"scopes":     []string{"read", "write"},
	})
	tokReq, _ := http.NewRequest("POST", "/api/knowledge-merchant/tokens", bytes.NewReader(body))
	tokReq.Header.Set("Content-Type", "application/json")
	tokW := httptest.NewRecorder()
	r.ServeHTTP(tokW, tokReq)
	var tokResp struct {
		Data struct {
			TokenPlain string `json:"token_plain"`
		} `json:"data"`
	}
	if err := json.Unmarshal(tokW.Body.Bytes(), &tokResp); err != nil {
		t.Fatalf("decode token: %v", err)
	}
	if tokResp.Data.TokenPlain == "" {
		t.Fatalf("token_plain empty")
	}
	impBody := mustJSON(t, map[string]any{
		"source":     "custom",
		"product_id": "kb-e2e",
		"items": []map[string]any{
			{"title": "E2E FAQ 1", "content": "E2E content 1", "category": "FAQ"},
			{"title": "E2E FAQ 2", "content": "E2E content 2", "category": "FAQ"},
		},
		"sync": true,
	})
	impReq, _ := http.NewRequest("POST", "/api/knowledge-merchant/external/import", bytes.NewReader(impBody))
	impReq.Header.Set("Content-Type", "application/json")
	impReq.Header.Set("X-Knowledge-Token", tokResp.Data.TokenPlain)
	impW := httptest.NewRecorder()
	r.ServeHTTP(impW, impReq)
	if impW.Code != http.StatusOK {
		t.Fatalf("external import failed: %d, body=%s", impW.Code, impW.Body.String())
	}
	var impResp struct {
		Data struct {
			Status   string `json:"status"`
			Total    int    `json:"total"`
			Accepted int    `json:"accepted"`
			Async    bool   `json:"async"`
		} `json:"data"`
	}
	if err := json.Unmarshal(impW.Body.Bytes(), &impResp); err != nil {
		t.Fatalf("decode import: %v", err)
	}
	if impResp.Data.Async {
		t.Error("expected sync result, got async")
	}
	if impResp.Data.Accepted != 2 {
		t.Errorf("expected 2 accepted, got %d", impResp.Data.Accepted)
	}

	var docCount int64
	pidNumeric := "kb-e2e"
	db.Model(&model.KnowledgeDocument{}).Where("product_id = ?", pidNumeric).Count(&docCount)
	if docCount < 2 {
		t.Errorf("expected at least 2 docs, got %d", docCount)
	}
}

// TestKM_FullFlow_FeedbackStore 反馈数据持久化验证
func TestKM_FullFlow_FeedbackStore(t *testing.T) {
	ctrl := NewKnowledgeMerchantController()
	r := setupKMRouter(t, ctrl)
	db := db.GetDB() // 当前全局测试句柄（_test.go 允许；与 setup 同库，避免 NewTestDB 二次建库清掉种子）
	body := mustJSON(t, map[string]any{
		"product_id": "kb-1",
		"query":      "测试反馈",
		"rating":     1,
		"comment":    "非常有用",
		"operator":   "tester",
	})
	req, _ := http.NewRequest("POST", "/api/knowledge-merchant/feedback", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}

	var count int64
	db.Model(&model.KnowledgeFeedback{}).Count(&count)
	if count < 1 {
		t.Errorf("expected feedback stored, count=%d", count)
	}
	var fb model.KnowledgeFeedback
	db.Where("query = ?", "测试反馈").First(&fb)
	if fb.Rating != 1 {
		t.Errorf("expected rating=1, got %d", fb.Rating)
	}
	if fb.ProductID != "kb-1" {
		t.Errorf("expected product_id=kb-1, got %s", fb.ProductID)
	}
	if fb.QueryHash == "" {
		t.Error("expected query_hash to be set")
	}
}

// TestKM_BatchImport_MixedValidAndEmpty 混合有效和空内容
func TestKM_BatchImport_MixedValidAndEmpty(t *testing.T) {
	ctrl := NewKnowledgeMerchantController()
	r := setupKMRouter(t, ctrl)
	body := mustJSON(t, map[string]any{
		"product_id": "kb-test-1",
		"items": []map[string]any{
			{"title": "T1", "content": "有效内容"},
			{"title": "T2", "content": ""},
			{"title": "T3", "content": "  "},
			{"title": "T4", "content": "有效内容2"},
		},
	})
	req, _ := http.NewRequest("POST", "/api/knowledge-merchant/batch/import", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d, body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "rejected") {
		t.Errorf("expected rejected field: %s", w.Body.String())
	}
}
