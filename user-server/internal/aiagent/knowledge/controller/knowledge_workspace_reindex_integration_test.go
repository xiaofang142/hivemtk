package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"hivemtk-user/internal/aiagent/knowledge/model"
	"hivemtk-user/internal/pkg/testutil"
)

func TestKnowledgeWorkspaceReindexRespondsAfterPersistence(t *testing.T) {
	for _, batch := range []bool{false, true} {
		for _, missingSource := range []bool{false, true} {
			t.Run(fmt.Sprintf("batch=%t/missing=%t", batch, missingSource), func(t *testing.T) {
				db := testutil.NewTestDB(t, &model.RagProduct{}, &model.KnowledgeDocument{}, &model.KnowledgeChunk{})
				t.Cleanup(func() { testutil.ResetSchema(t, db, &model.KnowledgeChunk{}) })
				if err := db.Exec(`ALTER TABLE knowledge_chunks
					ADD COLUMN IF NOT EXISTS embedding vector,
					ADD COLUMN IF NOT EXISTS embed_status varchar(16) NOT NULL DEFAULT 'pending'`).Error; err != nil {
					t.Fatal(err)
				}
				var requests atomic.Int32
				embedder := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests.Add(1)
					vec := make([]float32, 1024)
					vec[0] = 1
					w.Header().Set("Content-Type", "application/json")
					if err := json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"index": 0, "embedding": vec}}}); err != nil {
						t.Error(err)
					}
				}))
				defer embedder.Close()
				product := model.RagProduct{ID: "fa13-http", Name: "reindex HTTP test", IsActive: true}
				product.EmbeddingProviderConfig.BaseURL = embedder.URL
				product.EmbeddingProviderConfig.APIType = "openai"
				product.EmbeddingProviderConfig.Model = "bge-m3"
				product.EmbeddingProviderConfig.Dimension = 1024
				if err := db.Create(&product).Error; err != nil {
					t.Fatal(err)
				}
				doc := model.KnowledgeDocument{ProductID: product.ID, Title: "HTTP test", EmbedStatus: model.EmbedStatusPending}
				if err := db.Create(&doc).Error; err != nil {
					t.Fatal(err)
				}
				if !missingSource {
					if err := db.Create(&model.KnowledgeChunk{DocumentID: doc.ID, ProductID: product.ID, Content: "HTTP chunk"}).Error; err != nil {
						t.Fatal(err)
					}
				}
				ctrl := NewKnowledgeWorkspaceController(nil)
				router := gin.New()
				router.POST("/documents/:id/reindex", ctrl.ReindexDocument)
				router.POST("/products/:product_id/rebuild-index", ctrl.RebuildProductIndex)
				path := fmt.Sprintf("/documents/%d/reindex?product_id=%s", doc.ID, product.ID)
				if batch {
					path = "/products/" + product.ID + "/rebuild-index"
				}
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, path, nil))
				var persisted model.KnowledgeDocument
				if err := db.First(&persisted, doc.ID).Error; err != nil {
					t.Fatal(err)
				}
				if persisted.ReindexAttempts != 1 {
					t.Fatalf("persisted attempts = %d, want 1", persisted.ReindexAttempts)
				}
				if missingSource {
					if recorder.Code != http.StatusInternalServerError || persisted.EmbedStatus != model.EmbedStatusFailed || persisted.ErrorMsg == "" || requests.Load() != 0 {
						t.Fatalf("failed response=%d %s status=%s error=%s requests=%d", recorder.Code, recorder.Body, persisted.EmbedStatus, persisted.ErrorMsg, requests.Load())
					}
					return
				}
				if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "重建完成") || persisted.EmbedStatus != model.EmbedStatusIndexed || requests.Load() != 1 {
					t.Fatalf("success response=%d %s status=%s requests=%d", recorder.Code, recorder.Body, persisted.EmbedStatus, requests.Load())
				}
				var ready int64
				if err := db.Table("knowledge_chunks").Where("document_id = ? AND embedding IS NOT NULL", doc.ID).Count(&ready).Error; err != nil || ready != 1 {
					t.Fatalf("response preceded vector persistence: ready=%d err=%v", ready, err)
				}
			})
		}
	}
}
