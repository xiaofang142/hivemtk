package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"hivemtk-user/internal/geo/dto"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"
	"hivemtk-user/internal/pkg/utils/logger"

	"gorm.io/gorm"
)

// KBService GEO 品牌知识库服务（迁移自 AIGEOTOOLS kb/service.go）
type KBService struct {
	docRepo repository.GeoKnowledgeDocumentRepository
	llm     *LLMAdapter
}

// NewKBService 创建知识库服务
func NewKBService(dr repository.GeoKnowledgeDocumentRepository, adapter *LLMAdapter) *KBService {
	return &KBService{docRepo: dr, llm: adapter}
}

// Save 新增/更新文档
func (s *KBService) Save(ctx context.Context, req *dto.SaveKnowledgeDocumentRequest) (*model.GeoKnowledgeDocument, error) {
	metadataJSON := ""
	if len(req.Metadata) > 0 {
		b, err := json.Marshal(req.Metadata)
		if err != nil {
			return nil, fmt.Errorf("序列化 metadata 失败: %w", err)
		}
		metadataJSON = string(b)
	}

	if req.ID != "" {
		existing, err := s.docRepo.GetByID(req.ID)
		if err == nil {
			existing.Title = req.Title
			existing.Content = req.Content
			existing.DocType = req.DocType
			existing.Metadata = metadataJSON
			if err := s.docRepo.Update(existing); err != nil {
				return nil, err
			}
			return existing, nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}

	doc := &model.GeoKnowledgeDocument{
		Title:    req.Title,
		Content:  req.Content,
		DocType:  req.DocType,
		Metadata: metadataJSON,
	}
	if err := s.docRepo.Create(doc); err != nil {
		return nil, err
	}
	return doc, nil
}

// Get 获取文档详情
func (s *KBService) Get(ctx context.Context, id string) (*model.GeoKnowledgeDocument, error) {
	return s.docRepo.GetByID(id)
}

// List 文档列表
func (s *KBService) List(ctx context.Context) ([]*model.GeoKnowledgeDocument, error) {
	return s.docRepo.GetList()
}

// Delete 删除文档
func (s *KBService) Delete(ctx context.Context, id string) error {
	return s.docRepo.Delete(id)
}

// Search 关键词检索（标题命中 0.6 + 内容命中 0.4，迁移自 AIGEOTOOLS keyword fallback 路径）
func (s *KBService) Search(ctx context.Context, q string, limit int) ([]*dto.KBSearchResult, error) {
	if limit <= 0 {
		limit = 10
	}
	docs, err := s.docRepo.GetList()
	if err != nil {
		return nil, err
	}
	ql := strings.ToLower(strings.TrimSpace(q))
	if ql == "" {
		return nil, fmt.Errorf("检索词不能为空")
	}

	results := make([]*dto.KBSearchResult, 0, len(docs))
	for _, d := range docs {
		titleMatch := strings.Contains(strings.ToLower(d.Title), ql)
		contentMatch := strings.Contains(strings.ToLower(d.Content), ql)
		if !titleMatch && !contentMatch {
			continue
		}
		score := 0.0
		if titleMatch {
			score += 0.6
		}
		if contentMatch {
			score += 0.4
		}
		results = append(results, &dto.KBSearchResult{
			KnowledgeDocumentResponse: toDocResponse(d),
			Score:                     score,
		})
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Score > results[j].Score })
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

// Ask RAG 问答：检索 Top5 片段作为上下文交给 LLM
func (s *KBService) Ask(ctx context.Context, question string) (*dto.KBAskResponse, error) {
	if s.llm == nil {
		return nil, fmt.Errorf("未配置 LLM 提供商")
	}

	docs, err := s.Search(ctx, question, 5)
	if err != nil {
		return nil, err
	}

	snippets := make([]string, 0, len(docs))
	sources := make([]string, 0, len(docs))
	for _, r := range docs {
		snippets = append(snippets, "## "+r.Title+"\n"+r.Content)
		sources = append(sources, r.Title)
	}

	resp, err := s.llm.Generate(ctx, "你是知识库问答助手。请基于以下提供的事实片段回答用户问题，若不足以回答请坦诚说明。", "事实片段：\n"+strings.Join(snippets, "\n\n")+"\n\n用户问题："+question, 0.3, 2000)
	if err != nil {
		logger.Warn(fmt.Sprintf("[GEO KB] LLM 不可用，降级为知识库检索摘要: question=%s", question))
		return &dto.KBAskResponse{
			Answer:   heuristicKBAnswer(question, snippets),
			Sources:  sources,
			Provider: "heuristic_fallback",
			Model:    "retrieval_v1",
		}, nil
	}
	return &dto.KBAskResponse{
		Answer:   resp.Content,
		Sources:  sources,
		Provider: resp.Provider,
		Model:    resp.Model,
	}, nil
}

// GetContextForGeneration 为内容生成提供参考上下文（含可信度标注）
func (s *KBService) GetContextForGeneration(ctx context.Context, query string) (string, error) {
	docs, err := s.Search(ctx, query, 5)
	if err != nil {
		return "", err
	}
	var builder strings.Builder
	builder.WriteString("参考上下文：\n\n")
	for _, r := range docs {
		builder.WriteString(fmt.Sprintf("### [%s] (相关性: %.0f%%)\n", r.Title, r.Score*100))
		builder.WriteString(r.Content)
		builder.WriteString("\n\n")
	}
	return builder.String(), nil
}

func toDocResponse(d *model.GeoKnowledgeDocument) dto.KnowledgeDocumentResponse {
	return dto.KnowledgeDocumentResponse{
		ID:        d.ID,
		Title:     d.Title,
		Content:   d.Content,
		DocType:   d.DocType,
		CreatedAt: d.CreatedAt,
		UpdatedAt: d.UpdatedAt,
	}
}

// heuristicKBAnswer LLM 不可用时基于知识库检索结果拼装的降级回答
func heuristicKBAnswer(question string, snippets []string) string {
	if len(snippets) == 0 {
		return "当前知识库暂无与问题相关的内容，无法回答。请尝试其他问题或补充相关文档。"
	}
	const maxSnippets = 3
	const snippetLimit = 200
	var sb strings.Builder
	sb.WriteString("根据知识库检索到的相关文档，内容概要：\n\n")
	count := len(snippets)
	if count > maxSnippets {
		count = maxSnippets
	}
	for i := 0; i < count; i++ {
		content := snippets[i]
		if r := []rune(content); len(r) > snippetLimit {
			content = string(r[:snippetLimit]) + "…"
		}
		sb.WriteString("- ")
		sb.WriteString(content)
		sb.WriteString("\n")
	}
	return sb.String()
}
