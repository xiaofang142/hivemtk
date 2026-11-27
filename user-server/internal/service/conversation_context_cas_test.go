package service

import (
	"context"
	"errors"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

func TestConversationContextCASRetriesWithoutLosingConcurrentTags(t *testing.T) {
	stored := &model.ConversationContext{
		ID: 9, TenantID: 1, ChannelAccountID: 7, ExternalConversationID: "conv-1",
		Version: 1, SummaryVersion: 1, Tags: []string{"existing"},
	}
	attempts := 0
	load := func() (*model.ConversationContext, error) {
		copy := *stored
		copy.Tags = append([]string{}, stored.Tags...)
		return &copy, nil
	}
	save := func(c *model.ConversationContext) error {
		attempts++
		if attempts == 1 {
			stored.Tags = append(stored.Tags, "concurrent")
			stored.Version++
			return repository.ErrConversationContextVersionConflict
		}
		stored.Tags = append([]string{}, c.Tags...)
		stored.Version = c.Version
		return nil
	}
	err := mutateConversationContextCAS(context.Background(), load, save, func(c *model.ConversationContext) (bool, error) {
		c.Tags = append(c.Tags, "mine")
		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("CAS 冲突后必须重读重试，实际保存 %d 次", attempts)
	}
	if len(stored.Tags) != 3 || stored.Tags[0] != "existing" || stored.Tags[1] != "concurrent" || stored.Tags[2] != "mine" {
		t.Fatalf("并发标签丢失：%#v", stored.Tags)
	}
}

func TestConversationContextCASStopsAfterBoundedConflicts(t *testing.T) {
	attempts := 0
	load := func() (*model.ConversationContext, error) {
		return &model.ConversationContext{ID: 9, Version: 1}, nil
	}
	save := func(c *model.ConversationContext) error {
		attempts++
		return repository.ErrConversationContextVersionConflict
	}
	err := mutateConversationContextCAS(context.Background(), load, save, func(c *model.ConversationContext) (bool, error) {
		c.Summary = "changed"
		return true, nil
	})
	if !errors.Is(err, repository.ErrConversationContextVersionConflict) {
		t.Fatalf("持续冲突必须返回哨兵错误，实际 %v", err)
	}
	if attempts != 5 {
		t.Fatalf("CAS 重试必须有界，实际 %d 次", attempts)
	}
}

func TestConversationContextCASDoesNotSaveUnchangedMutation(t *testing.T) {
	attempts := 0
	err := mutateConversationContextCAS(context.Background(), func() (*model.ConversationContext, error) {
		return &model.ConversationContext{ID: 9, Version: 1}, nil
	}, func(c *model.ConversationContext) error {
		attempts++
		return nil
	}, func(c *model.ConversationContext) (bool, error) {
		return false, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 0 {
		t.Fatalf("无变化 mutation 不得写库，实际 %d 次", attempts)
	}
}

func TestAppendConversationMessageAtomicTracksSourceCursor(t *testing.T) {
	c := &model.ConversationContext{Messages: model.ConversationMessages{{MessageID: "old", Content: "旧消息"}}}
	changed, err := appendConversationMessageAtomic(c, model.ConversationMessage{MessageID: "hub:42", Content: "新消息"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("真实新消息必须产生变化")
	}
	if len(c.Messages) != 1 || c.Messages[0].MessageID != "hub:42" {
		t.Fatalf("有界保留错误：%#v", c.Messages)
	}
	if c.SourceCursor != "hub:42" || c.SourceCursorHash != HashConversationSourceCursor("hub:42") {
		t.Fatalf("来源游标未更新：cursor=%q hash=%q", c.SourceCursor, c.SourceCursorHash)
	}
	changed, err = appendConversationMessageAtomic(c, model.ConversationMessage{MessageID: "hub:42", Content: "重复"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("重复消息不得再次写库")
	}
	if _, err := appendConversationMessageAtomic(c, model.ConversationMessage{MessageID: "history_9"}, 1); err == nil {
		t.Fatal("合成消息 ID 必须拒绝")
	}
}

func TestSetConversationSummaryAtCursorTracksVersion(t *testing.T) {
	c := &model.ConversationContext{SummaryVersion: 4, SourceCursor: "hub:1", SourceCursorHash: HashConversationSourceCursor("hub:1")}
	changed, err := setConversationSummary(c, "新摘要", "hub:2")
	if err != nil {
		t.Fatal(err)
	}
	if !changed || c.Summary != "新摘要" || c.SummaryVersion != 5 {
		t.Fatalf("摘要版本未更新：changed=%v summary=%q version=%d", changed, c.Summary, c.SummaryVersion)
	}
	if c.SourceCursor != "hub:2" || c.SourceCursorHash != HashConversationSourceCursor("hub:2") {
		t.Fatalf("摘要来源游标未更新：%+v", c)
	}
	changed, err = setConversationSummary(c, "新摘要", "hub:2")
	if err != nil {
		t.Fatal(err)
	}
	if changed || c.SummaryVersion != 5 {
		t.Fatalf("相同摘要不得重复递增版本：changed=%v version=%d", changed, c.SummaryVersion)
	}
}
