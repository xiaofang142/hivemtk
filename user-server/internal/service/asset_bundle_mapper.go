package service

import (
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
)

// AssetBundleMessagesFromDTO dto 消息切片 → model JSONB 消息数组
func AssetBundleMessagesFromDTO(msgs []dto.AssetBundleMessage) model.AssetBundleMessages {
	if msgs == nil {
		return nil
	}
	out := make(model.AssetBundleMessages, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, model.AssetBundleMessage{
			Role:       m.Role,
			Content:    m.Content,
			ToolCallID: m.ToolCallID,
			Name:       m.Name,
		})
	}
	return out
}

// AssetBundleMessagesToDTO model JSONB 消息数组 → dto 消息切片
func AssetBundleMessagesToDTO(msgs model.AssetBundleMessages) []dto.AssetBundleMessage {
	if msgs == nil {
		return nil
	}
	out := make([]dto.AssetBundleMessage, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, dto.AssetBundleMessage{
			Role:       m.Role,
			Content:    m.Content,
			ToolCallID: m.ToolCallID,
			Name:       m.Name,
		})
	}
	return out
}

// FromAssetBundleModel 实体 → 响应视图
func FromAssetBundleModel(b *model.AssetBundle) *dto.AssetBundleView {
	if b == nil {
		return nil
	}
	return &dto.AssetBundleView{
		ID:                 b.ID,
		AssetID:            b.AssetID,
		Title:              b.Title,
		Description:        b.Description,
		Author:             b.Author,
		Version:            b.Version,
		Scope:              string(b.Scope),
		Status:             string(b.Status),
		Industry:           b.Industry,
		Language:           b.Language,
		Tags:               []string(b.Tags),
		Messages:           AssetBundleMessagesToDTO(b.Messages),
		Examples:           []any(b.Examples),
		SupportedLanguages: []string(b.SupportedLanguages),
		UseCount:           b.UseCount,
		Rating:             b.Rating,
		RatingCount:        b.RatingCount,
		CoverImage:         b.CoverImage,
		PlatformAssetID:    b.PlatformAssetID,
		PlatformStatus:     b.PlatformStatus,
		PlatformReviewNote: b.PlatformReviewNote,
		CreatedAt:          b.CreatedAt,
		UpdatedAt:          b.UpdatedAt,
	}
}

// FromAssetBundleModelList 批量转换 实体 → 响应视图
func FromAssetBundleModelList(list []*model.AssetBundle) []*dto.AssetBundleView {
	out := make([]*dto.AssetBundleView, 0, len(list))
	for _, b := range list {
		out = append(out, FromAssetBundleModel(b))
	}
	return out
}
