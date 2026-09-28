package repository

// 契约锁：会话进终态时 duration_ms 必须等于这次执行的真实时长。
//
// 立项依据（真机只读腿走查）：监控页把耗时渲染成 `duration_ms / 1000`，而全仓写侧
// 只写 status/completed_at——duration_ms 从建行到终态一次都没被赋过值（grep 全仓仅
// model 的字段声明命中），于是每条已结束会话在监控页上恒显示 0.0s，用户无法分辨
// 9 步的只读抓取和 90 分钟的挂死重试，「慢在哪一次执行」这条排查线索整体消失。
//
// 时长必须算在**同一条 UPDATE 里**而不是「先读 started_at 再写差值」：
// completed_at 与 started_at 都在库里，差值只有数据库在同一时刻读同一行才拿得准；
// 应用侧读完再写会把自己看到的旧值当成终点，而批量收口（断连钩子/对账器）一次要结
// 多行，每行的起点各不相同——共用一个 now 减出来的差值会把慢的那条压成快的那条。
//
// 起点取 COALESCE(started_at, created_at)：排队中被取消/被拦阻的会话从未进过 active，
// 但它确实占用了一次任务生命周期，留 0 等于把「排队 40 分钟后被 Host 离线收口」
// 显示成瞬时结束。
import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/browser_automation/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

const (
	durUser       = uint(791001)
	durSessionID  = uint(791101)
	durTaskID     = uint(791201)
	durTaskOther  = uint(791202)
	durQueuedID   = uint(791102)
	durNoStartID  = uint(791103)
	durStartedA   = uint(791104)
	durStartedB   = uint(791105)
	durStartedC   = uint(791106)
	durStaleTaskA = uint(791107)
	durSkewID     = uint(791108)
)

func durRepo(t *testing.T) (BrowserSessionRepository, *gorm.DB) {
	t.Helper()
	db := testutil.NewTestDB(t, &model.BrowserSession{})
	if db == nil {
		t.Fatal("测试库不可达：时长是库里算出来的，Skip 等于没锁")
	}
	return NewBrowserSessionRepositoryWithDB(db), db
}

// durSeed 造一条已存在若干时长的会话：startedAt<0 表示从未进过 active（只有 created_at）。
// 起点直接写进 DB（不走 API），这样断言打在「那条 UPDATE 自己算出的差值」上，
// 而不是测试侧再减一遍——测试自己减出来的期望值会跟着实现一起错。
func durSeed(t *testing.T, db *gorm.DB, id uint, taskID uint, status string, sinceCreate, sinceStart time.Duration) {
	t.Helper()
	now := time.Now()
	s := model.BrowserSession{
		ID:     id,
		TaskID: taskID,
		UserID: durUser,
		Status: status,
	}
	created := now.Add(-sinceCreate)
	s.CreatedAt = created
	if sinceStart > 0 {
		started := now.Add(-sinceStart)
		s.StartedAt = &started
	}
	if err := db.Create(&s).Error; err != nil {
		t.Fatalf("种子会话 %d 落库失败: %v", id, err)
	}
}

func durRead(t *testing.T, db *gorm.DB, id uint) *model.BrowserSession {
	t.Helper()
	// 独立零值 struct：复用已填充的 struct 再 First 会把旧字段并进 WHERE（record not found 假红）。
	var s model.BrowserSession
	if err := db.Where("id = ?", id).First(&s).Error; err != nil {
		t.Fatalf("读回会话 %d 失败: %v", id, err)
	}
	return &s
}

// assertDur 断言落在预期区间内（上下各留 1s 抖动余量：落库与读回之间的往返不可压缩）。
func assertDur(t *testing.T, id uint, got int64, want time.Duration) {
	t.Helper()
	lo := int64((want - 1*time.Second) / time.Millisecond)
	hi := int64((want + 1*time.Second) / time.Millisecond)
	if lo < 0 {
		lo = 0
	}
	if got < lo || got > hi {
		t.Errorf("会话 %d duration_ms=%d，期望落在 [%d,%d]（真实时长约 %v）", id, got, lo, hi, want)
	}
}

// 1) completed：以 started_at 为起点算出真实执行时长。
func TestSessionDurationOnCompleted(t *testing.T) {
	repo, db := durRepo(t)
	ctx := context.Background()
	durSeed(t, db, durSessionID, durTaskID, "active", 7*time.Second, 5*time.Second)
	if err := repo.UpdateStatus(ctx, durSessionID, "completed", ""); err != nil {
		t.Fatalf("UpdateStatus 失败: %v", err)
	}
	got := durRead(t, db, durSessionID)
	assertDur(t, durSessionID, got.DurationMs, 5*time.Second)
	if got.CompletedAt == nil {
		t.Error("completed_at 仍为空：终态时间戳与时长必须同源，否则两个数会各自漂")
	}
}

// 2) 非终态（active）不写时长：在途会话的耗时是「到目前为止」的移动值，
// 提前落库会让监控页把一条还在跑的会话显示成已结束。
func TestSessionDurationNotWrittenBeforeTerminal(t *testing.T) {
	repo, db := durRepo(t)
	ctx := context.Background()
	durSeed(t, db, durQueuedID, durTaskOther, "created", 3*time.Second, 0)
	if err := repo.UpdateStatus(ctx, durQueuedID, "active", ""); err != nil {
		t.Fatalf("UpdateStatus(active) 失败: %v", err)
	}
	got := durRead(t, db, durQueuedID)
	if got.StartedAt == nil {
		t.Fatal("active 应写 started_at")
	}
	if got.DurationMs != 0 {
		t.Errorf("在途会话 duration_ms 应保持 0，got %d", got.DurationMs)
	}
}

// 3) 排队中被判死（从未进 active）：以 created_at 为起点，不得留 0。
// 这一格同时锁住 GREATEST：时钟回拨时差值不得为负——负耗时在页面上是「-2.0s」这种
// 用户无法解释的值，比 0 更糟。
func TestSessionDurationForNeverStarted(t *testing.T) {
	repo, db := durRepo(t)
	ctx := context.Background()
	durSeed(t, db, durNoStartID, durTaskOther, "created", 4*time.Second, 0)
	if err := repo.UpdateStatus(ctx, durNoStartID, "failed", "host 离线"); err != nil {
		t.Fatalf("UpdateStatus(failed) 失败: %v", err)
	}
	got := durRead(t, db, durNoStartID)
	assertDur(t, durNoStartID, got.DurationMs, 4*time.Second)
}

// 4) 批量收口必须逐行各算各的：断连钩子一次结掉该用户所有在途会话，
// 三条起点各不相同，共用一个差值就是把慢的那条压成快的那条。
func TestFailRunningByUserPerRowDuration(t *testing.T) {
	repo, db := durRepo(t)
	ctx := context.Background()
	durSeed(t, db, durStartedA, durTaskID, "active", 9*time.Second, 2*time.Second)
	durSeed(t, db, durStartedB, durTaskOther, "active", 9*time.Second, 6*time.Second)
	durSeed(t, db, durStartedC, durTaskID, "created", 3*time.Second, 0)
	affected, err := repo.FailRunningByUser(ctx, durUser, "host disconnected")
	if err != nil {
		t.Fatalf("FailRunningByUser 失败: %v", err)
	}
	if len(affected) != 3 {
		t.Fatalf("应收口 3 条，got %d", len(affected))
	}
	assertDur(t, durStartedA, durRead(t, db, durStartedA).DurationMs, 2*time.Second)
	assertDur(t, durStartedB, durRead(t, db, durStartedB).DurationMs, 6*time.Second)
	assertDur(t, durStartedC, durRead(t, db, durStartedC).DurationMs, 3*time.Second)
}

// 5) 对账器收口（僵尸会话）同样要留时长：它正是「挂了一小时」那一类，
// 时长是它唯一对用户有用的信息。
func TestFailStaleUnfinishedWritesDuration(t *testing.T) {
	repo, db := durRepo(t)
	ctx := context.Background()
	durSeed(t, db, durStaleTaskA, durTaskID, "active", 20*time.Minute, 15*time.Minute)
	real, err := repo.FailStaleUnfinished(ctx, durTaskID, time.Now().Add(-time.Minute), "stale reconcile")
	if err != nil {
		t.Fatalf("FailStaleUnfinished 失败: %v", err)
	}
	if real != 1 {
		t.Fatalf("应收口 1 条，got %d", real)
	}
	got := durRead(t, db, durStaleTaskA).DurationMs
	lo := int64((14 * time.Minute) / time.Millisecond)
	hi := int64((17 * time.Minute) / time.Millisecond)
	if got < lo || got > hi {
		t.Errorf("僵尸会话 duration_ms=%d，期望约 15min（[%d,%d]）", got, lo, hi)
	}
}

// 6) 时钟回拨（started_at 落在未来）不得给出负耗时。
// 偏移取「+10min」这种确定值，而非真改系统时钟：负数在监控页是「-2.0s」，
// 用户既读不出时长也读不出「这次执行有问题」，比 0 更糟。
func TestSessionDurationNeverNegative(t *testing.T) {
	repo, db := durRepo(t)
	ctx := context.Background()
	now := time.Now()
	skewed := now.Add(10 * time.Minute)
	if err := db.Create(&model.BrowserSession{
		ID: durSkewID, TaskID: durTaskOther, UserID: durUser, Status: "active",
		CreatedAt: now.Add(-time.Minute), StartedAt: &skewed,
	}).Error; err != nil {
		t.Fatalf("种子会话落库失败: %v", err)
	}
	if err := repo.UpdateStatus(ctx, durSkewID, "completed", ""); err != nil {
		t.Fatalf("UpdateStatus 失败: %v", err)
	}
	if got := durRead(t, db, durSkewID).DurationMs; got != 0 {
		t.Errorf("起点晚于终点的会话 duration_ms 应夹为 0，got %d", got)
	}
}
