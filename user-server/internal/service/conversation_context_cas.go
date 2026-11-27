package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

const conversationContextCASMaxAttempts = 5

type conversationContextMutation func(*model.ConversationContext) (bool, error)

// mutateConversationContextCAS 在调用方提供的读口上重试版本冲突，避免 get→modify→save 丢并发更新。
func mutateConversationContextCAS(
	ctx context.Context,
	load func() (*model.ConversationContext, error),
	save func(*model.ConversationContext) error,
	mutate conversationContextMutation,
) error {
	if load == nil || save == nil || mutate == nil {
		return errors.New("共享上下文 CAS 依赖不完整")
	}
	for range conversationContextCASMaxAttempts {
		if err := ctx.Err(); err != nil {
			return err
		}
		current, err := load()
		if err != nil {
			return err
		}
		if current == nil {
			return errors.New("共享上下文为空")
		}
		changed, err := mutate(current)
		if err != nil {
			return err
		}
		if !changed {
			return nil
		}
		if err := save(current); err != nil {
			if errors.Is(err, repository.ErrConversationContextVersionConflict) {
				continue
			}
			return err
		}
		return nil
	}
	return repository.ErrConversationContextVersionConflict
}

// HashConversationSourceCursor 生成稳定来源游标指纹；空游标不产生伪证据。
func HashConversationSourceCursor(cursor string) string {
	cursor = strings.TrimSpace(cursor)
	if cursor == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(cursor))
	return hex.EncodeToString(sum[:])
}

func appendConversationMessageAtomic(c *model.ConversationContext, msg model.ConversationMessage, bound int) (bool, error) {
	if c == nil {
		return false, errors.New("共享上下文为空")
	}
	id := strings.TrimSpace(msg.MessageID)
	if id == "" || strings.HasPrefix(id, "history_") {
		return false, errors.New("共享消息缺少真实源消息 ID")
	}
	for _, existing := range c.Messages {
		if existing.MessageID == id {
			return false, nil
		}
	}
	if c.Messages == nil {
		c.Messages = model.ConversationMessages{}
	}
	msg.MessageID = id
	c.Messages = append(c.Messages, msg)
	if bound > 0 && len(c.Messages) > bound {
		c.Messages = append(model.ConversationMessages{}, c.Messages[len(c.Messages)-bound:]...)
	}
	c.SourceCursor = id
	c.SourceCursorHash = HashConversationSourceCursor(id)
	return true, nil
}

func setConversationSummary(c *model.ConversationContext, summary, cursor string) (bool, error) {
	if c == nil {
		return false, errors.New("共享上下文为空")
	}
	cursor = strings.TrimSpace(cursor)
	if c.Summary == summary && (cursor == "" || cursor == c.SourceCursor) {
		return false, nil
	}
	c.Summary = summary
	if cursor != "" {
		c.SourceCursor = cursor
		c.SourceCursorHash = HashConversationSourceCursor(cursor)
	}
	if c.SummaryVersion <= 0 {
		c.SummaryVersion = 1
	} else {
		c.SummaryVersion++
	}
	return true, nil
}
