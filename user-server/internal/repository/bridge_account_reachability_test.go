// bridge_account_reachability_test.go 钉住"这台桥接账号此刻够得着吗"这一条口径的两个读侧：
// 渠道总览的在线计数与主动触达的选号。
//
// 两侧历史上都按 bridge_accounts.status='online' 判，而那一列只在 SSE 正常收尾时被 SetOffline
// 改回离线：扩展崩溃、浏览器被杀、断网都不走那条路径，列就粘在 online 上。
// 于是一屏"在线"和"发出去了"可以同时成立，而客户一条也没收到。
package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

const reachabilityChannel = "douyin"

func newReachabilityDB(t *testing.T) *gorm.DB {
	t.Helper()
	return testutil.NewTestDB(t, &model.BridgeAccount{})
}

func TestCountBridgeOnline_UsesLastSyncGraceNotStickyStatus(t *testing.T) {
	database := newReachabilityDB(t)
	now := time.Now()
	staleAt := now.Add(-time.Hour)
	fresh, stale := &now, &staleAt

	seedBridgeAccount(t, database, reachabilityChannel, "a-fresh", "online", fresh)    // 真在线
	seedBridgeAccount(t, database, reachabilityChannel, "b-sticky", "online", stale)   // 粘住的在线位：列还写着 online，早就不同步了
	seedBridgeAccount(t, database, reachabilityChannel, "c-offline", "offline", fresh) // 刚正常断开：宽限窗内也不算在线
	seedBridgeAccount(t, database, "xiaohongshu", "other-fresh", "online", fresh)      // 别的渠道，不许串进本渠道计数

	// grace 传 30 秒（与 bridge/online_grace_window 默认值同口径）
	got, err := NewChannelOverviewRepository(database).CountBridgeOnline(context.Background(), reachabilityChannel, 30)
	if err != nil {
		t.Fatalf("CountBridgeOnline 报错: %v", err)
	}
	if got != 1 {
		t.Errorf("按 grace 口径应只算 a-fresh，得 1；实得 %d（把粘住的 online 列或窗口外的行算进来了）", got)
	}

	// 窗口必须真的参与判定：放宽到 2 小时后 b-sticky 才够格，c-offline 仍不算。
	wide, err := NewChannelOverviewRepository(database).CountBridgeOnline(context.Background(), reachabilityChannel, 7200)
	if err != nil {
		t.Fatalf("CountBridgeOnline(宽窗口) 报错: %v", err)
	}
	if wide != 2 {
		t.Errorf("窗口放宽到 7200s 应把 b-sticky 纳入（期望 2），实得 %d ⇒ grace 参数没进判定", wide)
	}
}

func TestFindActiveAccountID_BridgeSkipsStickyOnlineAccount(t *testing.T) {
	database := newReachabilityDB(t)
	now := time.Now()
	staleAt := now.Add(-time.Hour)
	fresh, stale := &now, &staleAt

	seedBridgeAccount(t, database, reachabilityChannel, "b-sticky", "online", stale)
	seedBridgeAccount(t, database, reachabilityChannel, "a-fresh", "online", fresh)

	got, err := NewProactiveReachRepository(database).FindActiveAccountID(context.Background(), reachabilityChannel, 30)
	if err != nil {
		t.Fatalf("FindActiveAccountID 报错: %v", err)
	}
	if got != "a-fresh" {
		t.Errorf("应选此刻仍在同步的 a-fresh，实得 %q", got)
	}

	// 只剩早已不同步的那台 ⇒ 必须报"查无可用账号"，让上层落一次可见的失败，
	// 而不是把消息交给一台永远不会来取的机器（那正是永不结算的 pending 行的来源）。
	if err := database.WithContext(context.Background()).
		Where("account_id = ?", "a-fresh").Delete(&model.BridgeAccount{}).Error; err != nil {
		t.Fatalf("删掉可达账号失败: %v", err)
	}
	_, err = NewProactiveReachRepository(database).FindActiveAccountID(context.Background(), reachabilityChannel, 30)
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("只剩粘住在线位的账号时应返回 ErrRecordNotFound，实得 %v", err)
	}

	// 同一台账号在放宽窗口后重新可用：证明选号判据跟着窗口走，不是把某台写死放行。
	picked, err := NewProactiveReachRepository(database).FindActiveAccountID(context.Background(), reachabilityChannel, 7200)
	if err != nil {
		t.Fatalf("宽窗口下 FindActiveAccountID 报错: %v", err)
	}
	if picked != "b-sticky" {
		t.Errorf("窗口放宽后应能选到 b-sticky，实得 %q", picked)
	}
}
