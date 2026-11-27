package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"hivemtk-user/internal/model"
)

var (
	ErrAgentContextTombstoneExists = errors.New("消息 tombstone 已存在")
	ErrAgentContextTombstoneHooks  = errors.New("消息 tombstone 失效钩子不完整")
	ErrAgentContextTombstoneInput  = errors.New("消息 tombstone 输入无效")
)

// MessageTombstoneHooks 将 tombstone 持久化、摘要失效和 native generation 失效解耦。
// 三个钩子都必须幂等；持久化先成功，后续失败可由调用方凭同一 tombstone 重试。
type MessageTombstoneHooks struct {
	Persist           func(context.Context, model.MessageTombstone) error
	InvalidateSummary func(context.Context, model.MessageTombstone) error
	InvalidateNative  func(context.Context, model.MessageTombstone) error
}

type AgentContextTombstoneService struct {
	hooks MessageTombstoneHooks
}

func NewAgentContextTombstoneService(hooks MessageTombstoneHooks) *AgentContextTombstoneService {
	return &AgentContextTombstoneService{hooks: hooks}
}

func (s *AgentContextTombstoneService) Apply(ctx context.Context, item model.MessageTombstone) error {
	tid, err := conversationDispositionTenantID(ctx)
	if err != nil {
		return err
	}
	if err := validateAgentContextTombstone(item, tid); err != nil {
		return err
	}
	if s == nil || s.hooks.Persist == nil || s.hooks.InvalidateSummary == nil || s.hooks.InvalidateNative == nil {
		return ErrAgentContextTombstoneHooks
	}
	if err := s.hooks.Persist(ctx, item); err != nil && !errors.Is(err, ErrAgentContextTombstoneExists) {
		return fmt.Errorf("持久化消息 tombstone: %w", err)
	}
	if err := s.hooks.InvalidateSummary(ctx, item); err != nil {
		return fmt.Errorf("失效共享摘要: %w", err)
	}
	if err := s.hooks.InvalidateNative(ctx, item); err != nil {
		return fmt.Errorf("失效 native generation: %w", err)
	}
	return nil
}

// TombstoneInvalidatesCursor 判断受影响消息是否包含在摘要/native generation 的游标范围内。
func TombstoneInvalidatesCursor(targetCursor, tombstoneCursor string) bool {
	targetCursor = strings.TrimSpace(targetCursor)
	tombstoneCursor = strings.TrimSpace(tombstoneCursor)
	if targetCursor == "" || tombstoneCursor == "" {
		return false
	}
	return compareConversationDispositionCursors(tombstoneCursor, targetCursor) <= 0
}

func validateAgentContextTombstone(item model.MessageTombstone, tenantID uint) error {
	if item.TenantID == 0 || item.TenantID != tenantID || strings.TrimSpace(item.Platform) == "" ||
		item.ChannelAccountID == 0 || strings.TrimSpace(item.ConversationID) == "" ||
		strings.TrimSpace(item.MessageID) == "" || strings.TrimSpace(item.SourceCursor) == "" ||
		item.EffectiveAt.IsZero() {
		return ErrAgentContextTombstoneInput
	}
	switch item.TombstoneType {
	case model.MessageTombstoneEdited, model.MessageTombstoneDeleted,
		model.MessageTombstoneRecalled, model.MessageTombstoneRedacted:
		return nil
	default:
		return ErrAgentContextTombstoneInput
	}
}
