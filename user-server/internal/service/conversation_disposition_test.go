package service

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
)

func TestConversationDispositionTransitions(t *testing.T) {
	now := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		from       string
		transition DispositionTransition
		want       string
		wantErr    bool
	}{
		{name: "pending_to_processing", from: model.ConversationDispositionPending, transition: DispositionTransition{Status: model.ConversationDispositionProcessing}, want: model.ConversationDispositionProcessing},
		{name: "processing_to_replied", from: model.ConversationDispositionProcessing, transition: DispositionTransition{Status: model.ConversationDispositionReplied, ReplyMessageID: "out-1", DeliveredAt: &now}, want: model.ConversationDispositionReplied},
		{name: "reply_requires_delivery", from: model.ConversationDispositionProcessing, transition: DispositionTransition{Status: model.ConversationDispositionReplied, ReplyMessageID: "out-1"}, wantErr: true},
		{name: "pending_cannot_reply", from: model.ConversationDispositionPending, transition: DispositionTransition{Status: model.ConversationDispositionReplied, ReplyMessageID: "out-1", DeliveredAt: &now}, wantErr: true},
		{name: "skip_requires_reason", from: model.ConversationDispositionPending, transition: DispositionTransition{Status: model.ConversationDispositionSkipped}, wantErr: true},
		{name: "pending_to_skipped", from: model.ConversationDispositionPending, transition: DispositionTransition{Status: model.ConversationDispositionSkipped, Reason: "not_actionable"}, want: model.ConversationDispositionSkipped},
		{name: "processing_to_merged", from: model.ConversationDispositionProcessing, transition: DispositionTransition{Status: model.ConversationDispositionMerged, Reason: "covered_by_newer_reply"}, want: model.ConversationDispositionMerged},
		{name: "replied_is_terminal", from: model.ConversationDispositionReplied, transition: DispositionTransition{Status: model.ConversationDispositionSkipped, Reason: "late"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := model.ConversationMessageDisposition{Status: tt.from, Version: 2}
			if tt.from == model.ConversationDispositionReplied {
				d.ReplyMessageID = "out-1"
				d.DeliveredAt = &now
			}
			changed, err := applyDispositionTransition(&d, tt.transition)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("期望拒绝转换，实际 changed=%v status=%q", changed, d.Status)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !changed || d.Status != tt.want {
				t.Fatalf("转换结果错误：changed=%v status=%q want=%q", changed, d.Status, tt.want)
			}
		})
	}
}

func TestConversationDispositionReplyRequiresConfirmedReceipt(t *testing.T) {
	now := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	d := model.ConversationMessageDisposition{Status: model.ConversationDispositionProcessing}
	changed, err := applyDispositionTransition(&d, DispositionTransition{
		Status: model.ConversationDispositionReplied, ReplyMessageID: "out-1", DeliveredAt: &now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !changed || d.ReplyMessageID != "out-1" || d.DeliveredAt == nil || !d.DeliveredAt.Equal(now) {
		t.Fatalf("已送达回执未落账：%+v", d)
	}
	changed, err = applyDispositionTransition(&d, DispositionTransition{
		Status: model.ConversationDispositionReplied, ReplyMessageID: "out-1", DeliveredAt: &now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("相同送达回执不得重复转换")
	}
}

func TestConversationReplyWatermarkRequiresContiguousDeliveredReplies(t *testing.T) {
	now := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	items := []model.ConversationMessageDisposition{
		{MessageCursor: "hub:12", Status: model.ConversationDispositionReplied, ReplyMessageID: "out-12", DeliveredAt: &now},
		{MessageCursor: "hub:10", Status: model.ConversationDispositionReplied, ReplyMessageID: "out-10", DeliveredAt: &now},
		{MessageCursor: "hub:9", Status: model.ConversationDispositionPending},
		{MessageCursor: "hub:2", Status: model.ConversationDispositionReplied, ReplyMessageID: "out-2", DeliveredAt: &now},
	}
	if got := ComputeConversationReplyWatermark(items); got != "hub:2" {
		t.Fatalf("pending 缺口后不得推进水位，got %q", got)
	}
	items[2].Status = model.ConversationDispositionReplied
	items[2].ReplyMessageID = "out-9"
	items[2].DeliveredAt = &now
	if got := ComputeConversationReplyWatermark(items); got != "hub:12" {
		t.Fatalf("连续且确认送达的回复应推进到最新游标，got %q", got)
	}
	items[3].DeliveredAt = nil
	if got := ComputeConversationReplyWatermark(items); got != "" {
		t.Fatalf("缺少送达确认不得推进水位，got %q", got)
	}
}

func TestLateDispositionDecision(t *testing.T) {
	now := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	window := 30 * time.Minute
	tests := []struct {
		name      string
		cursor    string
		watermark string
		createdAt time.Time
		want      string
		reason    string
	}{
		{name: "already_covered", cursor: "hub:9", watermark: "hub:10", createdAt: now.Add(-time.Minute), want: model.ConversationDispositionMerged, reason: "covered_by_newer_reply"},
		{name: "unreplied_in_window", cursor: "hub:11", watermark: "hub:10", createdAt: now.Add(-time.Minute), want: model.ConversationDispositionPending, reason: "late_unreplied"},
		{name: "unreplied_expired", cursor: "hub:11", watermark: "hub:10", createdAt: now.Add(-time.Hour), want: model.ConversationDispositionSkipped, reason: "late_window_expired"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision, err := DecideLateConversationDisposition(model.ConversationMessageDisposition{
				MessageCursor: tt.cursor, CreatedAt: tt.createdAt,
			}, tt.watermark, now, window)
			if err != nil {
				t.Fatal(err)
			}
			if decision.Status != tt.want || decision.Reason != tt.reason || !decision.Late {
				t.Fatalf("迟到处置错误：%+v", decision)
			}
		})
	}
}

type fakeDispositionStore struct {
	items      map[string]model.ConversationMessageDisposition
	conflict   bool
	saveCalls  int
	lastTenant uint
}

func (f *fakeDispositionStore) key(ref ConversationDispositionRef) string {
	return ref.Platform + "|" + strconv.FormatUint(uint64(ref.ChannelAccountID), 10) + "|" + ref.ConversationID + "|" + ref.MessageID
}

func (f *fakeDispositionStore) Get(_ context.Context, tenantID uint, platform string, accountID uint, conversationID, messageID string) (*model.ConversationMessageDisposition, error) {
	f.lastTenant = tenantID
	item, ok := f.items[f.key(ConversationDispositionRef{Platform: platform, ChannelAccountID: accountID, ConversationID: conversationID, MessageID: messageID})]
	if !ok || item.TenantID != tenantID {
		return nil, nil
	}
	copy := item
	return &copy, nil
}

func (f *fakeDispositionStore) Save(_ context.Context, item *model.ConversationMessageDisposition) error {
	f.saveCalls++
	if f.conflict {
		return ErrConversationDispositionVersionConflict
	}
	if item.ID == 0 {
		item.ID = uint(len(f.items) + 1)
		item.Version = 1
		f.items[f.key(ConversationDispositionRef{Platform: item.Platform, ChannelAccountID: item.ChannelAccountID, ConversationID: item.ConversationID, MessageID: item.MessageID})] = *item
		return nil
	}
	existing, ok := f.items[f.key(ConversationDispositionRef{Platform: item.Platform, ChannelAccountID: item.ChannelAccountID, ConversationID: item.ConversationID, MessageID: item.MessageID})]
	if !ok || existing.TenantID != item.TenantID {
		return ErrConversationDispositionVersionConflict
	}
	item.Version = existing.Version + 1
	f.items[f.key(ConversationDispositionRef{Platform: item.Platform, ChannelAccountID: item.ChannelAccountID, ConversationID: item.ConversationID, MessageID: item.MessageID})] = *item
	return nil
}

func (f *fakeDispositionStore) List(_ context.Context, tenantID uint, platform string, accountID uint, conversationID string) ([]model.ConversationMessageDisposition, error) {
	items := make([]model.ConversationMessageDisposition, 0)
	for _, item := range f.items {
		if item.TenantID == tenantID && item.Platform == platform && item.ChannelAccountID == accountID && item.ConversationID == conversationID {
			items = append(items, item)
		}
	}
	return items, nil
}

func TestConversationDispositionServicePersistsTransitions(t *testing.T) {
	store := &fakeDispositionStore{items: map[string]model.ConversationMessageDisposition{}}
	svc := NewConversationDispositionService(store)
	ctx := tenant.WithTenantID(context.Background(), 7)
	ref := ConversationDispositionRef{Platform: "telegram", ChannelAccountID: 3, ConversationID: "dm-1", MessageID: "hub:1"}
	item, err := svc.Ensure(ctx, model.ConversationMessageDisposition{
		Platform: ref.Platform, ChannelAccountID: ref.ChannelAccountID, ConversationID: ref.ConversationID,
		MessageID: ref.MessageID, MessageCursor: "hub:1", BusLLMSessionIDBlind: "blind-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if item.Status != model.ConversationDispositionPending || item.Version != 1 || item.TenantID != 7 {
		t.Fatalf("首次入站状态错误：%+v", item)
	}
	if _, err := svc.Ensure(ctx, model.ConversationMessageDisposition{Platform: ref.Platform, ChannelAccountID: ref.ChannelAccountID, ConversationID: ref.ConversationID, MessageID: ref.MessageID, MessageCursor: "hub:1", BusLLMSessionIDBlind: "blind-1"}); err != nil {
		t.Fatal(err)
	}
	if store.saveCalls != 1 {
		t.Fatalf("重复 Ensure 不得重复写库，实际 %d 次", store.saveCalls)
	}
	if _, err := svc.Transition(ctx, ref, 1, DispositionTransition{Status: model.ConversationDispositionProcessing}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	replied, err := svc.Transition(ctx, ref, 2, DispositionTransition{Status: model.ConversationDispositionReplied, ReplyMessageID: "out-1", DeliveredAt: &now})
	if err != nil {
		t.Fatal(err)
	}
	if replied.Status != model.ConversationDispositionReplied || replied.Version != 3 {
		t.Fatalf("送达转换未持久化：%+v", replied)
	}
	watermark, err := svc.ReplyWatermark(ctx, ConversationDispositionScope{Platform: ref.Platform, ChannelAccountID: ref.ChannelAccountID, ConversationID: ref.ConversationID})
	if err != nil {
		t.Fatal(err)
	}
	if watermark != "hub:1" {
		t.Fatalf("回复水位错误：%q", watermark)
	}
}

func TestConversationDispositionServiceRejectsCASConflict(t *testing.T) {
	store := &fakeDispositionStore{items: map[string]model.ConversationMessageDisposition{}, conflict: true}
	svc := NewConversationDispositionService(store)
	ctx := tenant.WithTenantID(context.Background(), 7)
	_, err := svc.Ensure(ctx, model.ConversationMessageDisposition{
		Platform: "telegram", ChannelAccountID: 3, ConversationID: "dm-1", MessageID: "hub:1",
		MessageCursor: "hub:1", BusLLMSessionIDBlind: "blind-1",
	})
	if !errors.Is(err, ErrConversationDispositionVersionConflict) {
		t.Fatalf("存储冲突必须原样返回，实际 %v", err)
	}
}
