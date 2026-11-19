package repository

import (
	"context"
	"testing"
)

// 未初始化 DB 是基础设施故障，不能等同正常查询没有记录。
func TestContactConsentRepository_UninitializedReadFails(t *testing.T) {
	repo := NewContactConsentRepository(nil)
	if ev, err := repo.LatestEvent(context.Background(), "one-fault", "marketing", "wecom"); err == nil || ev != nil {
		t.Errorf("未初始化事件查询应返回错误与空结果，实际 ev=%+v err=%v", ev, err)
	}
	if pref, err := repo.GetPreference(context.Background(), "one-fault", "marketing", "wecom"); err == nil || pref != nil {
		t.Errorf("未初始化偏好查询应返回错误与空结果，实际 pref=%+v err=%v", pref, err)
	}
}
