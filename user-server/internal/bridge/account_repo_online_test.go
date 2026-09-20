// account_repo_online_test.go bridge_accounts 在线位的写入口径。
package bridge

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func seedBridgeAccountRow(t *testing.T, db *gorm.DB, channel, accountID, status string, lastSyncAt *time.Time) {
	t.Helper()
	acc := &model.BridgeAccount{Channel: channel, AccountID: accountID, Status: status, LastSyncAt: lastSyncAt}
	if err := db.WithContext(context.Background()).Create(acc).Error; err != nil {
		t.Fatalf("造渠道账号行失败: %v", err)
	}
}

// TestTouchLastSync_MarksChannelOnline 心跳即在线：TouchLastSync 只刷时间戳时，
// status 会停在 SetOffline 写下的 "offline"，管理面与回扫门都读不到真在线。
func TestTouchLastSync_MarksChannelOnline(t *testing.T) {
	db := testutil.NewTestDB(t, &model.BridgeAccount{})
	repo := NewBridgeAccountRepository(db)
	ctx := context.Background()

	now := time.Now()
	stale := now.Add(-2 * time.Hour)
	seedBridgeAccountRow(t, db, "douyin", "acc-touch", "offline", &stale)

	if err := repo.TouchLastSync(ctx, "douyin", "acc-touch"); err != nil {
		t.Fatalf("TouchLastSync: %v", err)
	}

	var got model.BridgeAccount
	if err := db.WithContext(ctx).Where("channel = ? AND account_id = ?", "douyin", "acc-touch").
		First(&got).Error; err != nil {
		t.Fatalf("回读账号行失败: %v", err)
	}
	if got.Status != "online" {
		t.Errorf("心跳后 status=%q want online（时间戳刷新必须连带翻在线位）", got.Status)
	}
	if got.LastSyncAt == nil || !got.LastSyncAt.After(stale) {
		t.Errorf("心跳未推进 last_sync_at: %v want > %v", got.LastSyncAt, stale)
	}
}

// TestTouchLastSync_UnknownAccountNoop 不存在的渠道不得报错：心跳写的是订阅者的状态，
// 账号行尚未注册（首帧未到）时静默通过即可。
func TestTouchLastSync_UnknownAccountNoop(t *testing.T) {
	db := testutil.NewTestDB(t, &model.BridgeAccount{})
	repo := NewBridgeAccountRepository(db)
	if err := repo.TouchLastSync(context.Background(), "douyin", "acc-none"); err != nil {
		t.Errorf("无对应账号行时 TouchLastSync 报错: %v", err)
	}
}

// TestIsOnlineByLastSync_StatusShortCircuits 管理面在线读法：status=offline 一票判离线
// （哪怕时间戳是刚刚写的），status=online 才看宽限窗。
//
// 这一条决定 Task 3 的接线是否真的可读：SSE 断开时 SetOffline 落的正是"刚刚"的时间戳，
// 若时间戳优先，断开后会再假在线 30s。
func TestIsOnlineByLastSync_StatusShortCircuits(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	fresh := now.Add(-2 * time.Second)
	older := now.Add(-2 * time.Minute)

	if isOnlineByLastSync(ctx, &fresh, "offline", now) {
		t.Error("status=offline 仍被判在线（刚断开的时间戳会盖掉断开事实）")
	}
	if !isOnlineByLastSync(ctx, &fresh, "online", now) {
		t.Error("在线且时间戳新鲜却判离线")
	}
	if isOnlineByLastSync(ctx, &older, "online", now) {
		t.Error("超出宽限窗仍判在线")
	}
	if isOnlineByLastSync(ctx, nil, "online", now) {
		t.Error("从未同步过的账号判在线")
	}
}

// TestHeartbeatCadenceWithinOnlineGraceWindow 心跳间隔必须落在在线宽限窗内：
// 心跳是在线位的唯一续期来源，间隔 > 窗口 ⇒ 明明挂着的流会被管理面判成掉线闪烁。
//
// 两侧默认值（15s / 30s）都有运行时配置覆盖口（sse.heartbeat_interval、
// bridge.online_grace_window），这里只能钉住默认值；配成违反关系时的现象是
// 在线态按心跳周期闪烁，不是静默失效——改配置时按这条口径核对。
func TestHeartbeatCadenceWithinOnlineGraceWindow(t *testing.T) {
	if SSEDefaultHeartbeatInterval >= OnlineGraceWindow {
		t.Errorf("心跳 %v ≥ 在线宽限窗 %v ⇒ 在线位会在两次心跳之间掉成离线",
			SSEDefaultHeartbeatInterval, OnlineGraceWindow)
	}
}
