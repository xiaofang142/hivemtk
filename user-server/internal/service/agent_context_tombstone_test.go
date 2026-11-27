package service

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
)

func TestAgentContextTombstoneApplyOrder(t *testing.T) {
	calls := make([]string, 0)
	service := NewAgentContextTombstoneService(MessageTombstoneHooks{
		Persist: func(context.Context, model.MessageTombstone) error {
			calls = append(calls, "persist")
			return nil
		},
		InvalidateSummary: func(context.Context, model.MessageTombstone) error {
			calls = append(calls, "summary")
			return nil
		},
		InvalidateNative: func(context.Context, model.MessageTombstone) error {
			calls = append(calls, "native")
			return nil
		},
	})
	if err := service.Apply(tenant.WithTenantID(context.Background(), 7), validAgentContextTombstone()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, []string{"persist", "summary", "native"}) {
		t.Fatalf("失效钩子顺序错误：%#v", calls)
	}
}

func TestAgentContextTombstoneDuplicateStillInvalidates(t *testing.T) {
	calls := make([]string, 0)
	service := NewAgentContextTombstoneService(MessageTombstoneHooks{
		Persist: func(context.Context, model.MessageTombstone) error {
			return ErrAgentContextTombstoneExists
		},
		InvalidateSummary: func(context.Context, model.MessageTombstone) error {
			calls = append(calls, "summary")
			return nil
		},
		InvalidateNative: func(context.Context, model.MessageTombstone) error {
			calls = append(calls, "native")
			return nil
		},
	})
	if err := service.Apply(tenant.WithTenantID(context.Background(), 7), validAgentContextTombstone()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, []string{"summary", "native"}) {
		t.Fatalf("重复 tombstone 仍必须执行幂等失效：%#v", calls)
	}
}

func TestAgentContextTombstoneStopsOnInvalidationFailure(t *testing.T) {
	nativeCalled := false
	wantErr := errors.New("summary unavailable")
	service := NewAgentContextTombstoneService(MessageTombstoneHooks{
		Persist: func(context.Context, model.MessageTombstone) error { return nil },
		InvalidateSummary: func(context.Context, model.MessageTombstone) error {
			return wantErr
		},
		InvalidateNative: func(context.Context, model.MessageTombstone) error {
			nativeCalled = true
			return nil
		},
	})
	if err := service.Apply(tenant.WithTenantID(context.Background(), 7), validAgentContextTombstone()); !errors.Is(err, wantErr) {
		t.Fatalf("摘要失效失败必须原样返回，实际 %v", err)
	}
	if nativeCalled {
		t.Fatal("摘要未失效前不得继续失效 native generation")
	}
}

func TestAgentContextTombstoneRequiresAllHooks(t *testing.T) {
	service := NewAgentContextTombstoneService(MessageTombstoneHooks{
		Persist: func(context.Context, model.MessageTombstone) error { return nil },
	})
	if err := service.Apply(tenant.WithTenantID(context.Background(), 7), validAgentContextTombstone()); err == nil {
		t.Fatal("缺少失效钩子必须 fail-closed")
	}
}

func TestAgentContextTombstoneValidation(t *testing.T) {
	service := NewAgentContextTombstoneService(MessageTombstoneHooks{
		Persist:           func(context.Context, model.MessageTombstone) error { return nil },
		InvalidateSummary: func(context.Context, model.MessageTombstone) error { return nil },
		InvalidateNative:  func(context.Context, model.MessageTombstone) error { return nil },
	})
	item := validAgentContextTombstone()
	item.TombstoneType = "unknown"
	if err := service.Apply(tenant.WithTenantID(context.Background(), 7), item); err == nil {
		t.Fatal("未知 tombstone 类型必须拒绝")
	}
	item = validAgentContextTombstone()
	item.SourceCursor = ""
	if err := service.Apply(tenant.WithTenantID(context.Background(), 7), item); err == nil {
		t.Fatal("缺少来源游标必须拒绝")
	}
}

func TestTombstoneInvalidatesCursor(t *testing.T) {
	if !TombstoneInvalidatesCursor("hub:10", "hub:9") {
		t.Fatal("tombstone 游标早于摘要/native 游标时必须失效")
	}
	if TombstoneInvalidatesCursor("hub:9", "hub:10") {
		t.Fatal("tombstone 游标晚于摘要/native 游标时不得误失效")
	}
	if !TombstoneInvalidatesCursor("hub:10", "hub:10") {
		t.Fatal("同游标 tombstone 必须失效")
	}
}

func validAgentContextTombstone() model.MessageTombstone {
	return model.MessageTombstone{
		TenantID: 7, Platform: "telegram", ChannelAccountID: 3,
		ConversationID: "group-1", MessageID: "hub:9", TombstoneType: model.MessageTombstoneRecalled,
		EffectiveAt: time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC), SourceCursor: "hub:9",
	}
}
