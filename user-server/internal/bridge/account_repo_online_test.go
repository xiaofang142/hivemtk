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
