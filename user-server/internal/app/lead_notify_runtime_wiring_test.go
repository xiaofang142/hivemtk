package app

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/service"
)

// TestInitLeadNotifyRuntimeNilDBReturnsNil 无库句柄不得装配运行时。
func TestInitLeadNotifyRuntimeNilDBReturnsNil(t *testing.T) {
	StopLeadNotifyRuntime()
	if got := InitLeadNotifyRuntime(nil); got != nil {
		t.Fatalf("db=nil 不应装配，got %v", got)
	}
}

// TestInitLeadNotifyRuntimeExplicitDBAndReplace 显式 DB 装配可消费空 outbox，重复 Init 先停旧运行时。
func TestInitLeadNotifyRuntimeExplicitDBAndReplace(t *testing.T) {
	StopLeadNotifyRuntime()
	db := testutil.NewTestDB(t, &model.LeadNotifyOutbox{}, &model.LeadNotifyDelivery{}, &model.SystemUser{}, &model.ChannelAccount{}, &model.Channel{})
	first := InitLeadNotifyRuntime(db)
	if first == nil || first.Service() == nil || first.Worker() == nil {
		t.Fatalf("显式 DB 应装配 service/worker：%+v", first)
	}
	if GetLeadNotifyService() != first.Service() || service.NewLeadNotifyService() != first.Service() {
		t.Fatal("运行时、全局服务与默认通知出口必须复用同一实例")
	}
	t.Cleanup(StopLeadNotifyRuntime)
	if _, err := first.Worker().RunOnce(context.Background()); err != nil {
		t.Fatalf("装配 worker 空轮失败：%v", err)
	}
	second := InitLeadNotifyRuntime(db)
	if second == nil || second == first {
		t.Fatalf("重复 Init 应返回新运行时：first=%p second=%p", first, second)
	}
	if _, err := second.Worker().RunOnce(context.Background()); err != nil {
		t.Fatalf("替换后 worker 空轮失败：%v", err)
	}
}
