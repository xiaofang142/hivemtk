package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"
	"hivemtk-user/internal/pkg/utils/logger"
)

// EntityExtractorService 实体抽取服务
//
// 从 GeoKnowledgeDocument 中通过 LLM 批量抽取实体（产品/人物/组织/地点/概念）
// 和实体关系（is_a/used_for/competitor_of/part_of）。写入 geo_entities /
// geo_entity_relations。
//
// 依赖注入：GeoEntityRepository（实体持久化）、GeoKnowledgeDocumentRepository
// （文档读取）、LLM Dispatcher（模型调用）。全部接口化，便于测试。
type EntityExtractorService struct {
	entityRepo    repository.GeoEntityRepository
	kbRepo        repository.GeoKnowledgeDocumentRepository
	llmDispatcher *llm.Dispatcher
	promptManager *PromptManager
}

// NewEntityExtractorService 创建实体抽取服务
func NewEntityExtractorService(
	entityRepo repository.GeoEntityRepository,
	kbRepo repository.GeoKnowledgeDocumentRepository,
	llmDispatcher *llm.Dispatcher,
) *EntityExtractorService {
	return &EntityExtractorService{
		entityRepo:    entityRepo,
		kbRepo:        kbRepo,
		llmDispatcher: llmDispatcher,
		promptManager: NewPromptManager(),
	}
}

type extractedEntity struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Aliases     []string `json:"aliases"`
	Description string   `json:"description"`
	Confidence  float64  `json:"confidence"`
}

type extractedRelation struct {
	EntityA  string `json:"entity_a"`
	EntityB  string `json:"entity_b"`
	Relation string `json:"relation"`
}

type extractResponse struct {
	Entities  []extractedEntity   `json:"entities"`
	Relations []extractedRelation `json:"relations"`
}

// ExtractFromDocument 从指定知识库文档抽取实体和关系
//
// 1. 拉取文档内容；
// 2. 调用 LLM 做结构化抽取（JSON mode）；
// 3. 将实体批量写入 geo_entities；
// 4. 按实体名→ID 回查，写入 geo_entity_relations。
func (s *EntityExtractorService) ExtractFromDocument(ctx context.Context, docID string) error {
	doc, err := s.kbRepo.GetByID(ctx, docID)
	if err != nil {
		return fmt.Errorf("加载知识库文档失败: %w", err)
	}
	if strings.TrimSpace(doc.Content) == "" {
		return fmt.Errorf("文档内容为空，跳过抽取")
	}

	prompt := s.promptManager.EntityExtractPrompt(ctx, doc.Title, doc.Content)

	req := llm.DispatchRequest{
		Scenario:    llm.ScenarioHighQuality,
		Prompt:      prompt,
		MaxTokens:   4000,
		JSONMode:    true,
		Temperature: 0.1,
	}
	resp, err := s.llmDispatcher.Dispatch(ctx, req)
	if err != nil {
		return fmt.Errorf("LLM 抽取失败: %w", err)
	}

	var parsed extractResponse
	preview := resp.Content
	if len(preview) > 500 {
		preview = preview[:500]
	}
	if err := json.Unmarshal([]byte(resp.Content), &parsed); err != nil {
		// 原始返回只落服务端日志：模型供应商的兜底话术（如「当前客服系统繁忙」）直接弹给用户
		// 既看不懂也误导（曾出现 invalid character 'æ' 这类乱码片段），前端只给可执行提示。
		logger.Warnf("[GEO] 实体抽取失败：LLM 返回非 JSON（doc=%s, err=%v, raw=%s）", docID, err, preview)
		return fmt.Errorf("实体抽取失败：模型返回内容不是合法 JSON，请稍后重试；若持续失败请检查「模型提供商」配置")
	}

	entities := make([]*model.GeoEntity, 0, len(parsed.Entities))
	for _, e := range parsed.Entities {
		if e.Confidence <= 0 || e.Confidence > 1 {
			e.Confidence = 0.8
		}
		ent := &model.GeoEntity{
			Name:        strings.TrimSpace(e.Name),
			Type:        e.Type,
			Aliases:     mustJSON(e.Aliases),
			Description: e.Description,
			Confidence:  e.Confidence,
		}
		if ent.Name == "" {
			continue
		}
		entities = append(entities, ent)
	}

	if len(entities) > 0 {
		if err := s.entityRepo.BatchCreate(ctx, entities); err != nil {
			return fmt.Errorf("批量写入实体失败: %w", err)
		}
	}

	// BatchCreate 由 PostgreSQL RETURNING 回填 ID，直接按本次写入的实体建映射。
	// 不再回查 List：那会按 created_at 取首页，本次实体被挤出分页时关系全丢，
	// 还会把库里同名的旧实体错认成本次抽取结果。
	nameToID := make(map[string]uint, len(entities))
	for _, e := range entities {
		if e.ID != 0 {
			nameToID[e.Name] = e.ID
		}
	}

	for _, r := range parsed.Relations {
		aID, okA := nameToID[r.EntityA]
		bID, okB := nameToID[r.EntityB]
		if !okA || !okB || aID == 0 || bID == 0 || strings.TrimSpace(r.Relation) == "" {
			continue
		}
		rel := &model.GeoEntityRelation{
			EntityAID: aID,
			EntityBID: bID,
			Relation:  r.Relation,
		}
		if err := s.entityRepo.UpsertRelation(ctx, rel); err != nil {
			logger.Warnf("[GEO Entity] 写入实体关系失败 doc=%s rel=%s-%s(%s): %v",
				docID, r.EntityA, r.EntityB, r.Relation, err)
		}
	}

	return nil
}

func mustJSON(v any) []byte {
	if v == nil {
		return []byte("null")
	}
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("null")
	}
	return b
}
