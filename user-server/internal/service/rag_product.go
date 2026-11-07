package service

import (
	"context"
	"errors"
	"fmt"

	kbrepo "hivemtk-user/internal/aiagent/knowledge/repository"
	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

type RagProductService struct {
	repo *kbrepo.RagConfigRepository
}

func NewRagProductService(repo *kbrepo.RagConfigRepository) *RagProductService {
	return &RagProductService{repo: repo}
}

// NewRagProductServiceFromGlobal 全局 DB 装配入口。
// 供 router 装配层调用，controller 不直连 gorm（depguard controller-layer 规则）。
// AV-04c 批次6：gorm 句柄的解析收回仓储层，service 不再触碰 DB 句柄；
// AV-09 回归修复：原批次6 在根 repository 包的 NewRagConfigRepositoryFromGlobal 桥
// （root repository → kbrepo）令 knowledge/repository 的测试经 testmigrate→
// migration→根 repository 闭合成 test-only import 环，桥撤除；装配改走批次7
// 刀4 的 kbrepo.NewRagConfigRepositoryDefault（句柄在 kbrepo 仓储内解析，与
// knowledge/service 既有装配同源，连接与解析时刻和原写法完全一致）。
func NewRagProductServiceFromGlobal() *RagProductService {
	return NewRagProductService(kbrepo.NewRagConfigRepositoryDefault())
}

func (s *RagProductService) List(ctx context.Context) ([]*model.RagProduct, error) {
	return s.repo.ListAllRagProducts(ctx)
}

func (s *RagProductService) Get(ctx context.Context, id string) (*model.RagProduct, error) {
	p, err := s.repo.GetRagProductForUpdate(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("rag product not found")
		}
		return nil, err
	}
	return p, nil
}

func (s *RagProductService) Create(ctx context.Context, p *model.RagProduct) error {
	if p.VectorTable == "" {
		p.VectorTable = "rag_vectors_" + p.ID
	}
	return s.repo.CreateRagProductWithVectorTable(ctx, p)
}

func (s *RagProductService) Update(ctx context.Context, p *model.RagProduct) error {
	return s.repo.UpdateRagProduct(ctx, p)
}

func (s *RagProductService) Delete(ctx context.Context, id string) error {
	return s.repo.DeleteRagProductByID(ctx, id)
}

func (s *RagProductService) Stats(ctx context.Context) (map[string]any, error) {
	products, err := s.repo.ListAllRagProducts(ctx)
	if err != nil {
		return nil, fmt.Errorf("list rag products for stats: %w", err)
	}
	var totalDocs, totalChunks int64
	var active int64
	for _, p := range products {
		totalDocs += int64(p.DocCount)
		totalChunks += p.ChunkCount
		if p.IsActive {
			active++
		}
	}
	return map[string]any{
		"total":        int64(len(products)),
		"active":       active,
		"inactive":     int64(len(products)) - active,
		"total_docs":   totalDocs,
		"total_chunks": totalChunks,
	}, nil
}
