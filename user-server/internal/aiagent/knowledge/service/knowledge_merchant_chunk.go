package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"hivemtk-user/internal/aiagent/knowledge/model"
	"hivemtk-user/internal/pkg/utils/logger"
)

// GetDocumentChunks 列出文档分段（支持分页）。
//
// token 可选：携带外部系统 API Token 时校验文档归属是否与授权产品一致
// （越权 IDOR 防护，与 UpdateChunk/DeleteChunk 一致）；留空表示由 JWT 管理员调用
// （拥有全局权限，不校验 product 归属）。
func (s *KnowledgeMerchantService) GetDocumentChunks(ctx context.Context, documentID uint64, page, pageSize int, token string) ([]model.KnowledgeChunk, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = DefaultPageSize
	}
	if token != "" {
		doc, derr := s.docRepo.GetByID(ctx, documentID)
		if derr != nil {
			return nil, 0, fmt.Errorf("文档不存在: %w", derr)
		}
		tok, terr := s.ValidateToken(ctx, token)
		if terr != nil {
			return nil, 0, terr
		}
		if tok.ProductID != "*" && tok.ProductID != "" && tok.ProductID != doc.ProductID {
			return nil, 0, fmt.Errorf("文档 %d 不属于 Token 授权产品 %s", documentID, tok.ProductID)
		}
	}
	return s.chunkRepo.PageByDocumentID(ctx, documentID, page, pageSize)
}

// UpdateChunkRequest 更新分段内容
type UpdateChunkRequest struct {
	ChunkID uint64 `json:"chunk_id"`
	Content string `json:"content"`
	Token   string `json:"-"`
}

// UpdateChunk 更新分段
func (s *KnowledgeMerchantService) UpdateChunk(ctx context.Context, req *UpdateChunkRequest) error {
	if req.ChunkID == 0 {
		return errors.New("chunk_id 不能为空")
	}
	if strings.TrimSpace(req.Content) == "" {
		return errors.New("内容不能为空")
	}
	chunk, err := s.chunkRepo.GetByID(ctx, req.ChunkID)
	if err != nil {
		return fmt.Errorf("分段不存在: %w", err)
	}
	if req.Token != "" {
		tok, terr := s.ValidateToken(ctx, req.Token)
		if terr != nil {
			return terr
		}
		if tok.ProductID != "*" && tok.ProductID != "" && tok.ProductID != chunk.ProductID {
			return fmt.Errorf("分段 %d 不属于 Token 授权产品 %s", chunk.ID, tok.ProductID)
		}
	}
	chunk.Content = req.Content
	chunk.CharCount = len(req.Content)
	chunk.TokenCount = len(strings.Fields(req.Content))
	h := sha256.Sum256([]byte(req.Content))
	chunk.ContentHash = hex.EncodeToString(h[:])
	chunk.EmbeddingID = ""
	if err := s.chunkRepo.Update(ctx, chunk); err != nil {
		return err
	}
	if err := s.kbService.EmbedAndPersistChunks(ctx, chunk.ProductID, []model.KnowledgeChunk{*chunk}); err != nil {
		logger.Errorf("[knowledge] 分段更新后重新向量化失败: %v", err)
	}
	return nil
}

// DeleteChunk 删除分段
// token 可选：携带外部系统 API Token 时校验分段归属是否与授权产品一致（越权 IDOR 防护）；
// 留空表示由 JWT 管理员调用（拥有全局权限）。
func (s *KnowledgeMerchantService) DeleteChunk(ctx context.Context, chunkID uint64, token string) error {
	if chunkID == 0 {
		return errors.New("chunk_id 不能为空")
	}
	if token != "" {
		chunk, err := s.chunkRepo.GetByID(ctx, chunkID)
		if err != nil {
			return fmt.Errorf("分段不存在: %w", err)
		}
		tok, terr := s.ValidateToken(ctx, token)
		if terr != nil {
			return terr
		}
		if tok.ProductID != "*" && tok.ProductID != "" && tok.ProductID != chunk.ProductID {
			return fmt.Errorf("分段 %d 不属于 Token 授权产品 %s", chunk.ID, tok.ProductID)
		}
	}
	return s.chunkRepo.Delete(ctx, chunkID)
}

// SplitChunkRequest 拆分分段
type SplitChunkRequest struct {
	ChunkID uint64   `json:"chunk_id"`
	Parts   []string `json:"parts"`
}

// SplitChunk 拆分分段为多段
func (s *KnowledgeMerchantService) SplitChunk(ctx context.Context, req *SplitChunkRequest) error {
	if req.ChunkID == 0 {
		return errors.New("chunk_id 不能为空")
	}
	if len(req.Parts) < 2 {
		return errors.New("至少需要 2 段")
	}
	// D-31：必须先校验再删。原实现先删原分段、再在插入循环里 `continue` 跳过空白段——
	// 传 ["  ", ""] 时原分段被删掉、新分段一个都不插，接口还回"拆分成功"，
	// 文档就此静默丢失全部分段。空白段一律拒绝，而不是悄悄丢弃。
	for _, part := range req.Parts {
		if strings.TrimSpace(part) == "" {
			return errors.New("拆分内容不能为空")
		}
	}
	chunk, err := s.chunkRepo.GetByID(ctx, req.ChunkID)
	if err != nil {
		return err
	}
	if err := s.chunkRepo.Delete(ctx, chunk.ID); err != nil {
		return err
	}
	docID := chunk.DocumentID
	productID := chunk.ProductID
	newChunks := make([]model.KnowledgeChunk, 0, len(req.Parts))
	baseIndex := chunk.ChunkIndex
	for i, part := range req.Parts {
		if strings.TrimSpace(part) == "" {
			continue
		}
		h := sha256.Sum256([]byte(part))
		newChunks = append(newChunks, model.KnowledgeChunk{
			DocumentID:  docID,
			ProductID:   productID,
			ChunkIndex:  baseIndex + i,
			Content:     part,
			ContentHash: hex.EncodeToString(h[:]),
			CharCount:   len(part),
			TokenCount:  len(strings.Fields(part)),
		})
	}
	if err := s.chunkRepo.BatchCreate(ctx, newChunks); err != nil {
		return err
	}
	if err := s.kbService.EmbedAndPersistChunks(ctx, productID, newChunks); err != nil {
		logger.Errorf("[knowledge] 分段重切后重新向量化失败: %v", err)
	}
	return nil
}
