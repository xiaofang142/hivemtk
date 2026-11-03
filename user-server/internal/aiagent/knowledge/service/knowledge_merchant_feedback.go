package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"hivemtk-user/internal/aiagent/knowledge/model"
	"hivemtk-user/internal/aiagent/knowledge/repository"
)

// SubmitFeedbackRequest 反馈请求
type SubmitFeedbackRequest struct {
	ProductID  string `json:"product_id"`
	Query      string `json:"query"`
	DocumentID uint64 `json:"document_id"`
	ChunkID    uint64 `json:"chunk_id"`
	Rating     int    `json:"rating"`
	Comment    string `json:"comment"`
	Operator   string `json:"operator"`
	SessionID  string `json:"session_id"`
}

// SubmitFeedback 提交反馈
func (s *KnowledgeMerchantService) SubmitFeedback(ctx context.Context, req *SubmitFeedbackRequest) error {
	if req.Query == "" {
		return errors.New("query 不能为空")
	}
	if req.Rating < -1 || req.Rating > 1 {
		return errors.New("rating 必须在 [-1, 0, 1]")
	}
	if s.db == nil {
		return nil
	}
	s.ensureReposFromDB()
	h := sha256.Sum256([]byte(req.Query))
	fb := &model.KnowledgeFeedback{
		ProductID: req.ProductID,
		Query:     req.Query,
		QueryHash: hex.EncodeToString(h[:]),
		Rating:    req.Rating,
		Comment:   req.Comment,
		Operator:  req.Operator,
		SessionID: req.SessionID,
	}
	if req.DocumentID > 0 {
		d := req.DocumentID
		fb.DocumentID = &d
	}
	if req.ChunkID > 0 {
		c := req.ChunkID
		fb.ChunkID = &c
	}
	return s.feedbackRepo.Create(ctx, fb)
}

// ListFeedbacksRequest 反馈列表查询
type ListFeedbacksRequest struct {
	ProductID string `json:"product_id"`
	Page      int    `json:"page"`
	PageSize  int    `json:"page_size"`
	Rating    int    `json:"rating"`
	// Keyword 按查询文本模糊搜索（前端「搜索」输入框）
	Keyword string `json:"keyword"`
}

// ListFeedbacks 反馈列表
func (s *KnowledgeMerchantService) ListFeedbacks(ctx context.Context, req *ListFeedbacksRequest) ([]model.KnowledgeFeedback, int64, error) {
	if s.db == nil {
		return nil, 0, nil
	}
	s.ensureReposFromDB()
	filter := repository.FeedbackListFilter{
		ProductID: req.ProductID,
		Page:      req.Page,
		PageSize:  req.PageSize,
		Keyword:   req.Keyword,
	}
	if req.Rating >= -1 && req.Rating <= 1 {
		filter.Rating = req.Rating
		filter.HasRating = true
	}
	return s.feedbackRepo.List(ctx, filter)
}

// FeedbackStats 评价分布（全量，不随分页变化）
func (s *KnowledgeMerchantService) FeedbackStats(ctx context.Context, productID string) (repository.FeedbackStats, error) {
	if s.db == nil {
		return repository.FeedbackStats{}, nil
	}
	s.ensureReposFromDB()
	return s.feedbackRepo.Stats(ctx, productID)
}
