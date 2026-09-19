package repository

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/pkg/timeutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 抖音统计仓储此前**没有任何**测试：week/month 分支用 MySQL 函数、日期区间两头都有偏差，
// 全部无人值守。这里补上的是改动本身需要的最小断言面（分桶数量 + 区间端点）。

func setupDouyinCardStatsRepo(t *testing.T) (DouyinCardStatsRepository, context.Context) {
	t.Helper()
	db := testutil.NewTestDB(t,
		&model.DouyinCard{},
		&model.DouyinCardActivity{},
	)
	return NewDouyinCardStatsRepository(db), context.Background()
}

func newDouyinActivity(t *testing.T, repo DouyinCardStatsRepository, cardID, userID uint, action string, at time.Time) {
	t.Helper()
	r := repo.(*douyinCardStatsRepository)
	require.NoError(t, r.db.Create(&model.DouyinCardActivity{
		CardID:    cardID,
		UserID:    userID,
		Action:    action,
		IPAddress: "10.0.0.8",
		CreatedAt: at,
	}).Error)
}

// TestDouyinCardStats_GroupByBuckets 三种分组都要真出数：
// 原 week/month 实现是 MySQL 的 YEARWEEK()/DATE_FORMAT()，在 PG 上直接报错。
func TestDouyinCardStats_GroupByBuckets(t *testing.T) {
	repo, ctx := setupDouyinCardStatsRepo(t)
	const cardID = uint(70001)

	now := time.Now()
	newDouyinActivity(t, repo, cardID, 1, "view", now)
	newDouyinActivity(t, repo, cardID, 2, "view", now.Add(-14*24*time.Hour)) // 必定跨周
	newDouyinActivity(t, repo, cardID, 3, "view", now.Add(-40*24*time.Hour)) // 必定跨月

	dayStats, err := repo.GetCardDailyStats(ctx, cardID, "", "", "day")
	require.NoError(t, err)
	assert.Len(t, dayStats, 3, "三条记录在三个不同日历日")

	weekStats, err := repo.GetCardDailyStats(ctx, cardID, "", "", "week")
	require.NoError(t, err, "week 分支必须是可查询的，不是报错")
	assert.Len(t, weekStats, 3, "三条分别相隔 14/26 天，落在三个 ISO 周桶")
	for _, s := range weekStats {
		assert.Regexp(t, `^\d{4}-\d{2}-\d{2}$`, s.Date)
	}

	monthStats, err := repo.GetCardDailyStats(ctx, cardID, "", "", "month")
	require.NoError(t, err, "month 分支必须是可查询的，不是报错")
	assert.Len(t, monthStats, 2, "相隔 40 天落在两个月桶")
	seen := map[string]bool{}
	for _, s := range monthStats {
		assert.Regexp(t, `^\d{4}-\d{2}$`, s.Date)
		seen[s.Date] = true
	}
	assert.True(t, seen[timeutil.BusinessDate(now)[:7]], "本月桶必须在结果里")
}

// TestDouyinCardStats_DateRangeBoundaries 钉住两条边界口径：
// 结束日整天计入；start / end 各自独立生效（原实现要求两者同时非空）。
func TestDouyinCardStats_DateRangeBoundaries(t *testing.T) {
	repo, ctx := setupDouyinCardStatsRepo(t)
	const cardID = uint(70002)

	now := time.Now()
	newDouyinActivity(t, repo, cardID, 1, "view", now)
	newDouyinActivity(t, repo, cardID, 2, "view", now.Add(-2*24*time.Hour))

	today := timeutil.BusinessDate(now)
	twoDaysAgo := timeutil.BusinessDate(now.Add(-2 * 24 * time.Hour))

	sum := func(stats []DouyinCardStatsTempStat) int {
		total := 0
		for _, s := range stats {
			total += s.Count
		}
		return total
	}

	both, err := repo.GetCardDailyStats(ctx, cardID, twoDaysAgo, today, "day")
	require.NoError(t, err)
	assert.Equal(t, 2, sum(both), "起止覆盖两天时两条都要算进来")

	// 只给 end：上界必须单独生效（结束日整天算在内，之后的那一天不算）。
	onlyEnd, err := repo.GetCardDailyStats(ctx, cardID, "", twoDaysAgo, "day")
	require.NoError(t, err)
	assert.Equal(t, 1, sum(onlyEnd), "end 落在前天时，只该有前天那一条")

	// 只给 start：下界必须单独生效，上界不设限。
	onlyStart, err := repo.GetCardDailyStats(ctx, cardID, twoDaysAgo, "", "day")
	require.NoError(t, err)
	assert.Equal(t, 2, sum(onlyStart), "只给 start 也必须生效")

	// 整个区间设在未来 ⇒ 空；反向确认过滤器没有整体失效。
	future := timeutil.BusinessDate(now.Add(24 * time.Hour))
	empty, err := repo.GetCardDailyStats(ctx, cardID, future, future, "day")
	require.NoError(t, err)
	assert.Equal(t, 0, sum(empty))
}

// TestDouyinCardStats_OverallGroupBy 总体统计走同一套分组/区间代码，至少不能回退成报错。
func TestDouyinCardStats_OverallGroupBy(t *testing.T) {
	repo, ctx := setupDouyinCardStatsRepo(t)
	const cardID = uint(70003)
	newDouyinActivity(t, repo, cardID, 1, "view", time.Now())

	for _, groupBy := range []string{"day", "week", "month", ""} {
		stats, err := repo.GetOverallDailyStats(ctx, "", "", groupBy)
		require.NoError(t, err, "groupBy=%s", groupBy)
		require.Len(t, stats, 1, "groupBy=%s", groupBy)
		assert.Equal(t, 1, stats[0].Count, "groupBy=%s", groupBy)
	}
}
