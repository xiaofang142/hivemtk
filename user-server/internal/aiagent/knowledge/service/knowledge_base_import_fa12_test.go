package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	knowledgeModel "hivemtk-user/internal/aiagent/knowledge/model"
	"hivemtk-user/internal/aiagent/llm"
	ragretrieval "hivemtk-user/internal/aiagent/rag/retrieval"
	"hivemtk-user/internal/etl"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
)

type fa12DocumentStore struct {
	doc             *knowledgeModel.KBDocument
	updateCalls     []context.Context
	updateErrors    []error
	statusUpdates   []model.KBDocumentStatus
	statusErrorMsgs []string
	getErr          error
}

func (f *fa12DocumentStore) Create(context.Context, *knowledgeModel.KBDocument) error {
	return nil
}

func (f *fa12DocumentStore) GetByID(context.Context, uint) (*knowledgeModel.KBDocument, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.doc, nil
}

func (f *fa12DocumentStore) List(context.Context, int, int) ([]*knowledgeModel.KBDocument, int64, error) {
	return nil, 0, nil
}

func (f *fa12DocumentStore) UpdateStatusFields(ctx context.Context, _ uint, fields map[string]any) error {
	f.updateCalls = append(f.updateCalls, ctx)
	if status, ok := fields["status"].(model.KBDocumentStatus); ok {
		f.statusUpdates = append(f.statusUpdates, status)
	}
	if errorMsg, ok := fields["error_msg"].(string); ok {
		f.statusErrorMsgs = append(f.statusErrorMsgs, errorMsg)
	}
	if len(f.updateErrors) > 0 {
		err := f.updateErrors[0]
		f.updateErrors = f.updateErrors[1:]
		if err != nil {
			return err
		}
	}
	if status, ok := fields["status"].(model.KBDocumentStatus); ok {
		f.doc.Status = status
	}
	if errorMsg, ok := fields["error_msg"].(string); ok {
		f.doc.ErrorMsg = errorMsg
	}
	return nil
}

func (f *fa12DocumentStore) Transaction(context.Context, func(*gorm.DB) error) error {
	return errors.New("transaction is not used by FA-12 test")
}

func newFA12KnowledgeBaseService(store *fa12DocumentStore) *KnowledgeBaseService {
	svc := newKnowledgeBaseServiceWithDocumentStore(store)
	svc.processor = etl.NewDocumentProcessor(&etl.ProcessingConfig{
		ChunkSize:         512,
		ChunkOverlap:      50,
		MinChunkSize:      1,
		MaxLengthPerChunk: 1000,
		MinLengthPerChunk: 1,
	})
	svc.vector = ragretrieval.NewVectorizer(1, &fa12EmbeddingClient{})
	svc.indexer = ragretrieval.NewInMemoryIndexManager(1)
	return svc
}

type fa12EmbeddingClient struct {
	err error
}

func (f *fa12EmbeddingClient) Embed(_ context.Context, _ *llm.EmbeddingConfig, texts []string) ([][]float32, error) {
	if f.err != nil {
		return nil, f.err
	}
	vectors := make([][]float32, len(texts))
	for i := range vectors {
		vectors[i] = []float32{1}
	}
	return vectors, nil
}

func (f *fa12EmbeddingClient) EmbedOne(context.Context, *llm.EmbeddingConfig, string) ([]float32, error) {
	if f.err != nil {
		return nil, f.err
	}
	return []float32{1}, nil
}

func (f *fa12EmbeddingClient) DefaultConfig() *llm.EmbeddingConfig {
	return &llm.EmbeddingConfig{Dimension: 1}
}

// 状态写入失败时必须持久化 failed，且处理链的每次状态写仍携带原租户。
func TestProcessContent_StateWriteFailurePersistsFailedWithTenant(t *testing.T) {
	store := &fa12DocumentStore{
		doc:          &knowledgeModel.KBDocument{ID: 12, Title: "crawler", Status: knowledgeModel.KBDocumentStatusPending},
		updateErrors: []error{errors.New("temporary state write failure")},
	}
	svc := newFA12KnowledgeBaseService(store)
	ctx := tenant.WithTenantID(context.Background(), 42)

	svc.processContent(ctx, store.doc, strings.Repeat("tenant scoped content ", 10))

	if store.doc.Status != knowledgeModel.KBDocumentStatusFailed {
		t.Fatalf("状态写失败后应持久化 failed，实际为 %q", store.doc.Status)
	}
	if !strings.Contains(store.doc.ErrorMsg, "处理中") {
		t.Fatalf("failed 状态应保留处理状态写错误，实际为 %q", store.doc.ErrorMsg)
	}
	for i, updateCtx := range store.updateCalls {
		got, ok := tenant.FromContext(updateCtx)
		if !ok || got != 42 {
			t.Fatalf("第 %d 次状态写丢失租户上下文，got=%d ok=%v", i, got, ok)
		}
	}
}

// 最终 indexed 状态写失败时也必须转成可重试的 failed 终态。
func TestProcessContent_FinalStateWriteFailurePersistsFailed(t *testing.T) {
	store := &fa12DocumentStore{
		doc:          &knowledgeModel.KBDocument{ID: 13, Title: "crawler", Status: knowledgeModel.KBDocumentStatusPending},
		updateErrors: []error{nil, errors.New("final state write failure")},
	}
	svc := newFA12KnowledgeBaseService(store)

	svc.processContent(tenant.WithTenantID(context.Background(), 7), store.doc, strings.Repeat("final state content ", 10))

	if store.doc.Status != knowledgeModel.KBDocumentStatusFailed {
		t.Fatalf("最终状态写失败后应持久化 failed，实际为 %q", store.doc.Status)
	}
	if !strings.Contains(store.doc.ErrorMsg, "已索引") {
		t.Fatalf("failed 状态应保留最终状态写错误，实际为 %q", store.doc.ErrorMsg)
	}
}

// 向量化失败必须形成持久 failed 终态，不能留下 processing/pending。
func TestProcessContent_VectorFailurePersistsFailed(t *testing.T) {
	store := &fa12DocumentStore{
		doc: &knowledgeModel.KBDocument{ID: 14, Title: "crawler", Status: knowledgeModel.KBDocumentStatusPending},
	}
	svc := newFA12KnowledgeBaseService(store)
	svc.vector = ragretrieval.NewVectorizer(1, &fa12EmbeddingClient{err: errors.New("embedding unavailable")})

	svc.processContent(tenant.WithTenantID(context.Background(), 8), store.doc, strings.Repeat("vector failure content ", 10))

	if store.doc.Status != knowledgeModel.KBDocumentStatusFailed {
		t.Fatalf("向量化失败后应持久化 failed，实际为 %q", store.doc.Status)
	}
	if !strings.Contains(store.doc.ErrorMsg, "向量化失败") {
		t.Fatalf("failed 状态应保留向量错误，实际为 %q", store.doc.ErrorMsg)
	}
}

// 空分片和索引器错误均须进入 failed，并能从相同正文重试恢复。
func TestProcessContent_ChunkAndIndexFailureCanRetry(t *testing.T) {
	for _, name := range []string{"empty_chunks", "index_error"} {
		t.Run(name, func(t *testing.T) {
			store := &fa12DocumentStore{doc: &knowledgeModel.KBDocument{ID: 15, Content: strings.Repeat("retry body ", 20)}}
			svc := newFA12KnowledgeBaseService(store)
			content := store.doc.Content
			if name == "empty_chunks" {
				content = ""
			} else {
				svc.indexer = ragretrieval.NewInMemoryIndexManager(2)
			}
			ctx := tenant.WithTenantID(context.Background(), 7)
			svc.processContent(ctx, store.doc, content)
			if store.doc.Status != knowledgeModel.KBDocumentStatusFailed || store.doc.ErrorMsg == "" {
				t.Fatalf("处理失败必须留下 failed+原因，doc=%+v", store.doc)
			}
			svc.indexer = ragretrieval.NewInMemoryIndexManager(1)
			svc.ProcessDocumentByID(ctx, store.doc.ID)
			if store.doc.Status != knowledgeModel.KBDocumentStatusIndexed || store.doc.ErrorMsg != "" {
				t.Fatalf("相同正文重试未恢复，doc=%+v", store.doc)
			}
		})
	}
}

// 文档读取失败或处理异常仍使用传入租户上下文持久化失败，不能悄悄返回成功。
func TestProcessDocumentByID_ReadAndPanicFailure(t *testing.T) {
	for _, name := range []string{"read_error", "panic"} {
		t.Run(name, func(t *testing.T) {
			store := &fa12DocumentStore{doc: &knowledgeModel.KBDocument{ID: 16, Content: "panic body"}}
			svc := newFA12KnowledgeBaseService(store)
			if name == "read_error" {
				store.getErr = errors.New("controlled read unavailable")
			} else {
				svc.processor = nil
			}
			svc.ProcessDocumentByID(tenant.WithTenantID(context.Background(), 8), store.doc.ID)
			if store.doc.Status != knowledgeModel.KBDocumentStatusFailed {
				t.Fatalf("读取/异常失败必须留下 failed，doc=%+v", store.doc)
			}
			for _, ctx := range store.updateCalls {
				if id, _ := tenant.FromContext(ctx); id != 8 {
					t.Fatalf("失败回写丢租户，got=%d", id)
				}
			}
		})
	}
}

// 上传入口同样不能在状态写失败时继续 QA，也不能留下虚假的 indexed 状态。
func TestProcessDocumentAsync_StateFailureStopsQA(t *testing.T) {
	for _, name := range []string{"processing", "indexed"} {
		t.Run(name, func(t *testing.T) {
			store := &fa12DocumentStore{doc: &knowledgeModel.KBDocument{ID: 17, Status: knowledgeModel.KBDocumentStatusPending}}
			store.updateErrors = []error{errors.New("state unavailable")}
			if name == "indexed" {
				store.updateErrors = []error{nil, errors.New("state unavailable")}
			}
			svc := newFA12KnowledgeBaseService(store)
			qaCalls := 0
			svc.qaEnqueue = func(uint, uint) { qaCalls++ }
			path := filepath.Join(t.TempDir(), "document.txt")
			if err := os.WriteFile(path, []byte("controlled upload body"), 0600); err != nil {
				t.Fatalf("写本地夹具失败: %v", err)
			}
			svc.processDocumentAsyncWithQA(tenant.WithTenantID(context.Background(), 8), store.doc.ID, path, true)
			if store.doc.Status != knowledgeModel.KBDocumentStatusFailed || qaCalls != 0 {
				t.Fatalf("状态写失败应阻止QA并持久化failed，status=%s qaCalls=%d", store.doc.Status, qaCalls)
			}
		})
	}
}

// 爬虫处理上下文必须脱离请求取消链，但保留源租户归属。
func TestCrawlerProcessingContext_PreservesTenantAfterCancellation(t *testing.T) {
	requestCtx, cancel := context.WithCancel(tenant.WithTenantID(context.Background(), 99))
	cancel()

	procCtx := crawlerProcessingContext(requestCtx, 42)
	got, ok := tenant.FromContext(procCtx)
	if !ok || got != 42 {
		t.Fatalf("处理上下文应恢复源租户 42，got=%d ok=%v", got, ok)
	}
	if procCtx.Err() != nil {
		t.Fatalf("处理上下文不应继承请求取消，err=%v", procCtx.Err())
	}
}
