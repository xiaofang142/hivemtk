package service

import (
	"context"
	"errors"
	"fmt"

	kbrepo "hivemtk-user/internal/aiagent/knowledge/repository"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

type RagProductService struct {
	repo *kbrepo.RagConfigRepository
}

func NewRagProductService(db *gorm.DB) *RagProductService {
	return &RagProductService{repo: kbrepo.NewRagConfigRepository(db)}
}

// NewRagProductServiceFromGlobal 全局 DB 装配入口。
// 供 router 装配层调用，controller 不直连 gorm（depguard controller-layer 规则）。
// gorm 句柄经 repository 层 GetDB() 访问器获取，service 自身不 import pkg/db。
func NewRagProductServiceFromGlobal() *RagProductService {
	return NewRagProductService(repository.GetDB())
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
