// Package service - GDPR DSAR 数据导出（G6）
//
// 给定 customer_id，导出该用户的所有数据打包成 JSON。
// 导出范围：customers 主表 + customer_sessions + session_messages + customer_tags + memory_items
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// DataExportService GDPR DSAR 数据导出服务
type DataExportService struct {
	repo *repository.DataExportRepository
}

// NewDataExportService 创建服务实例
func NewDataExportService() *DataExportService {
	return &DataExportService{
		repo: repository.NewDataExportRepository(repository.GetDB()),
	}
}

// ExportBundle DSAR 导出数据包
type ExportBundle struct {
	ExportedAt time.Time `json:"exported_at"`
	CustomerID string    `json:"customer_id"`

	Customer    *model.Customer          `json:"customer"`
	Sessions    []*model.CustomerSession `json:"sessions"`
	Messages    []*model.SessionMessage  `json:"messages"`
	Tags        []*model.CustomerTag     `json:"tags"`
	MemoryItems []*model.MemoryItem      `json:"memory_items"`

	MessageCount    int `json:"message_count"`
	MemoryItemCount int `json:"memory_item_count"`
}

// Export 执行 DSAR 数据导出
//
// 流程：
//  1. 按 customer_id 查 customers 主表记录
//  2. 用 unified_id 关联 customer_sessions（客户会话的 one_id）
//  3. 按 session_id 批量拉 session_messages
//  4. customer_tags 全局拉取（标签表不直接关联客户，导出客户涉及的标签定义）
//  5. memory_items 按 customer_id 拉
//
// 注意：敏感字段（如 phone_hash）按 GORM json:"-" tag 自动排除，不会出现在输出 JSON 中。
func (s *DataExportService) Export(ctx context.Context, customerID string) (*ExportBundle, error) {
	if customerID == "" {
		return nil, fmt.Errorf("DSAR_001: customer_id 不能为空")
	}

	bundle := &ExportBundle{
		ExportedAt: time.Now().UTC(),
		CustomerID: customerID,
	}

	customer, err := s.repo.GetCustomer(ctx, customerID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("DSAR_002: 客户 %s 不存在", customerID)
		}
		return nil, fmt.Errorf("DSAR_003: 查询客户失败: %w", err)
	}
	bundle.Customer = customer

	sessions, err := s.repo.ListSessionsByOneID(ctx, customer.UnifiedID)
	if err != nil {
		return nil, fmt.Errorf("DSAR_004: 查询会话失败: %w", err)
	}
	bundle.Sessions = sessions

	if len(sessions) > 0 {
		sessionIDs := make([]string, 0, len(sessions))
		for _, s := range sessions {
			sessionIDs = append(sessionIDs, s.SessionID)
		}
		messages, err := s.repo.ListMessagesBySessionIDs(ctx, sessionIDs)
		if err != nil {
			return nil, fmt.Errorf("DSAR_005: 查询消息失败: %w", err)
		}
		bundle.Messages = messages
		bundle.MessageCount = len(messages)
	}

	tags, err := s.repo.ListAllTags(ctx)
	if err != nil {
		return nil, fmt.Errorf("DSAR_006: 查询标签失败: %w", err)
	}
	bundle.Tags = tags

	memories, err := s.repo.ListMemoriesByCustomer(ctx, customerID)
	if err != nil {
		return nil, fmt.Errorf("DSAR_007: 查询记忆条目失败: %w", err)
	}
	bundle.MemoryItems = memories
	bundle.MemoryItemCount = len(memories)

	return bundle, nil
}

// ExportJSON 导出为 JSON 字节流（供 controller 直接写入 ResponseWriter）
func (s *DataExportService) ExportJSON(ctx context.Context, customerID string) ([]byte, error) {
	bundle, err := s.Export(ctx, customerID)
	if err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("DSAR_008: JSON 序列化失败: %w", err)
	}
	return data, nil
}
