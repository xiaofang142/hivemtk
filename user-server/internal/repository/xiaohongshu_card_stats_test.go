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

func setupXiaohongshuCardStatsRepo(t *testing.T) (XiaohongshuCardStatsRepository, context.Context) {
	t.Helper()
	db := testutil.NewTestDB(t,
		&model.XiaohongshuCard{},
		&model.XiaohongshuCardActivity{},
	)
	repo := NewXiaohongshuCardStatsRepository(db)
	return repo, context.Background()
}

func newTestCard(t *testing.T, repo XiaohongshuCardStatsRepository, title string, viewCount int, isActive bool) *model.XiaohongshuCard {
	t.Helper()
	r := repo.(*xiaohongshuCardStatsRepository)
	card := &model.XiaohongshuCard{
		Title:       title,
		ViewCount:   viewCount,
		Tags:        "test,card",
		RedirectURL: "https://example.com/" + title,
	}
	require.NoError(t, r.db.Create(card).Error)

	require.NoError(t, r.db.Model(card).Update("is_active", isActive).Error)
	return card
}

func newTestActivity(t *testing.T, repo XiaohongshuCardStatsRepository, cardID, userID uint, actType string, createdAt time.Time) *model.XiaohongshuCardActivity {
	t.Helper()
	r := repo.(*xiaohongshuCardStatsRepository)
	act := &model.XiaohongshuCardActivity{
		CardID:       cardID,
		UserID:       userID,
		ActivityType: actType,
		IPAddress:    "192.168.1.100",
		UserAgent:    "Mozilla/5.0",
		CreatedAt:    createdAt,
	}
	require.NoError(t, r.db.Create(act).Error)
	return act
}

func TestXiaohongshuCardStats_NewRepository(t *testing.T) {
	db := testutil.NewTestDB(t, &model.XiaohongshuCard{}, &model.XiaohongshuCardActivity{})
	repo := NewXiaohongshuCardStatsRepository(db)
	assert.NotNil(t, repo)
}

func TestXiaohongshuCardStats_GetCardByID_Found(t *testing.T) {
	repo, ctx := setupXiaohongshuCardStatsRepo(t)
	card := newTestCard(t, repo, "我的卡片", 0, true)

	got, err := repo.GetCardByID(ctx, card.ID)
	require.NoError(t, err)
	assert.Equal(t, card.ID, got.ID)
	assert.Equal(t, "我的卡片", got.Title)
	assert.True(t, got.IsActive)
}

func TestXiaohongshuCardStats_GetCardByID_NotFound(t *testing.T) {
	repo, ctx := setupXiaohongshuCardStatsRepo(t)

	got, err := repo.GetCardByID(ctx, 99999)
	assert.Error(t, err)
	assert.Nil(t, got)
}

func TestXiaohongshuCardStats_CountTotalCards(t *testing.T) {
	repo, ctx := setupXiaohongshuCardStatsRepo(t)

	n, err := repo.CountTotalCards(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), n)

	newTestCard(t, repo, "A", 0, true)
	newTestCard(t, repo, "B", 0, false)
	newTestCard(t, repo, "C", 0, true)

	n, err = repo.CountTotalCards(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(3), n)
}

func TestXiaohongshuCardStats_CountActiveCards(t *testing.T) {
	repo, ctx := setupXiaohongshuCardStatsRepo(t)

	newTestCard(t, repo, "A", 0, true)
	newTestCard(t, repo, "B", 0, false)
	newTestCard(t, repo, "C", 0, true)

	n, err := repo.CountActiveCards(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(2), n)
}

func TestXiaohongshuCardStats_CountCardViews(t *testing.T) {
	repo, ctx := setupXiaohongshuCardStatsRepo(t)
	card := newTestCard(t, repo, "C1", 0, true)

	for i := 0; i < 5; i++ {
		newTestActivity(t, repo, card.ID, uint(10+i), "view", time.Now())
	}
	for i := 0; i < 2; i++ {
		newTestActivity(t, repo, card.ID, uint(20+i), "click", time.Now())
	}

	n, err := repo.CountCardViews(ctx, card.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(5), n, "只统计 activity_type=view 的")
}

func TestXiaohongshuCardStats_CountTotalViews(t *testing.T) {
	repo, ctx := setupXiaohongshuCardStatsRepo(t)

	c1 := newTestCard(t, repo, "C1", 0, true)
	c2 := newTestCard(t, repo, "C2", 0, true)

	for i := 0; i < 3; i++ {
		newTestActivity(t, repo, c1.ID, uint(1+i), "view", time.Now())
	}
	for i := 0; i < 2; i++ {
		newTestActivity(t, repo, c2.ID, uint(1+i), "view", time.Now())
	}
	newTestActivity(t, repo, c1.ID, uint(99), "click", time.Now())

	n, err := repo.CountTotalViews(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(5), n)
}

func TestXiaohongshuCardStats_GetTopCards(t *testing.T) {
	repo, ctx := setupXiaohongshuCardStatsRepo(t)

	newTestCard(t, repo, "low", 10, true)
	newTestCard(t, repo, "high", 100, true)
	newTestCard(t, repo, "mid", 50, true)

	list, err := repo.GetTopCards(ctx, 2)
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.GreaterOrEqual(t, list[0].ViewCount, list[1].ViewCount)
}

func TestXiaohongshuCardStats_GetTopCards_Limit(t *testing.T) {
	repo, ctx := setupXiaohongshuCardStatsRepo(t)

	newTestCard(t, repo, "A", 1, true)
	newTestCard(t, repo, "B", 2, true)
	newTestCard(t, repo, "C", 3, true)

	list, err := repo.GetTopCards(ctx, 1)
	require.NoError(t, err)
	assert.Len(t, list, 1)
}

func TestXiaohongshuCardStats_GetTopCards_Empty(t *testing.T) {
	repo, ctx := setupXiaohongshuCardStatsRepo(t)

	list, err := repo.GetTopCards(ctx, 5)
	require.NoError(t, err)
	assert.Len(t, list, 0)
}

func TestXiaohongshuCardStats_CreateActivity(t *testing.T) {
	repo, ctx := setupXiaohongshuCardStatsRepo(t)
	card := newTestCard(t, repo, "ACT", 0, true)

	act := &model.XiaohongshuCardActivity{
		CardID:       card.ID,
		UserID:       1,
		Username:     "u1",
		ActivityType: "view",
		IPAddress:    "10.0.0.1",
		UserAgent:    "test",
		Content:      "hello",
	}
	err := repo.CreateActivity(ctx, act)
	require.NoError(t, err)
	assert.NotZero(t, act.ID)

	activities, err := repo.GetRecentActivitiesByCard(ctx, card.ID, 10)
	require.NoError(t, err)
	assert.Len(t, activities, 1)
	assert.Equal(t, "view", activities[0].ActivityType)
}

func TestXiaohongshuCardStats_IncrementCardViewCount(t *testing.T) {
	repo, ctx := setupXiaohongshuCardStatsRepo(t)
	card := newTestCard(t, repo, "INC", 5, true)

	err := repo.IncrementCardViewCount(ctx, card.ID)
	require.NoError(t, err)

	got, err := repo.GetCardByID(ctx, card.ID)
	require.NoError(t, err)
	assert.Equal(t, 6, got.ViewCount)

	err = repo.IncrementCardViewCount(ctx, card.ID)
	require.NoError(t, err)
	got, _ = repo.GetCardByID(ctx, card.ID)
	assert.Equal(t, 7, got.ViewCount)
}

func TestXiaohongshuCardStats_IncrementCardViewCount_NonExistent(t *testing.T) {
	repo, ctx := setupXiaohongshuCardStatsRepo(t)

	err := repo.IncrementCardViewCount(ctx, 99999)
	assert.NoError(t, err)
}

func TestXiaohongshuCardStats_GetRecentActivitiesByCard(t *testing.T) {
	repo, ctx := setupXiaohongshuCardStatsRepo(t)
	card := newTestCard(t, repo, "REC", 0, true)

	for i := 0; i < 6; i++ {
		newTestActivity(t, repo, card.ID, uint(1+i), "view", time.Now().Add(-time.Duration(6-i)*time.Hour))
	}
	for i := 0; i < 2; i++ {
		newTestActivity(t, repo, card.ID, uint(10+i), "click", time.Now())
	}

	acts, err := repo.GetRecentActivitiesByCard(ctx, card.ID, 3)
	require.NoError(t, err)
	assert.Len(t, acts, 3, "limit 应生效且只返回 view")
}

func TestXiaohongshuCardStats_GetRecentActivitiesByCard_Empty(t *testing.T) {
	repo, ctx := setupXiaohongshuCardStatsRepo(t)
	card := newTestCard(t, repo, "EMPTY", 0, true)

	acts, err := repo.GetRecentActivitiesByCard(ctx, card.ID, 10)
	require.NoError(t, err)
	assert.Len(t, acts, 0)
}

func TestXiaohongshuCardStats_GetRecentActivities(t *testing.T) {
	repo, ctx := setupXiaohongshuCardStatsRepo(t)

	c1 := newTestCard(t, repo, "C1", 0, true)
	c2 := newTestCard(t, repo, "C2", 0, true)

	newTestActivity(t, repo, c1.ID, 1, "view", time.Now())
	newTestActivity(t, repo, c2.ID, 2, "view", time.Now().Add(-time.Hour))
	newTestActivity(t, repo, c1.ID, 3, "click", time.Now())

	acts, err := repo.GetRecentActivities(ctx, 10)
	require.NoError(t, err)
	assert.Len(t, acts, 2, "只返回 activity_type=view")
}

func TestXiaohongshuCardStats_GetRecentActivities_Limit(t *testing.T) {
	repo, ctx := setupXiaohongshuCardStatsRepo(t)
	card := newTestCard(t, repo, "LIM", 0, true)

	for i := 0; i < 10; i++ {
		newTestActivity(t, repo, card.ID, uint(1+i), "view", time.Now().Add(-time.Duration(10-i)*time.Minute))
	}

	acts, err := repo.GetRecentActivities(ctx, 3)
	require.NoError(t, err)
	assert.Len(t, acts, 3)
}

func TestXiaohongshuCardStats_GetCardDailyStats_Day(t *testing.T) {
	repo, ctx := setupXiaohongshuCardStatsRepo(t)
	card := newTestCard(t, repo, "D1", 0, true)

	today := time.Now()
	yesterday := today.Add(-24 * time.Hour)

	newTestActivity(t, repo, card.ID, 1, "view", today)
	newTestActivity(t, repo, card.ID, 2, "view", today)
	newTestActivity(t, repo, card.ID, 3, "click", today)
	newTestActivity(t, repo, card.ID, 4, "view", yesterday)

	stats, err := repo.GetCardDailyStats(ctx, card.ID, "", "", "day")
	require.NoError(t, err)
	require.NotEmpty(t, stats)

	total := 0
	for _, s := range stats {
		total += s.Count
	}
	assert.Equal(t, 4, total)
}

// TestXiaohongshuCardStats_GetCardDailyStats_Week 覆盖 week 分支的真实分组行为。
//
// 这一支原本用 MySQL 的 YEARWEEK()，在本仓的 PG 上必然报错，
// 而旧断言 assert.Error 把「报错」当成了期望结果，等于把缺陷钉死在测试里。
// 改成 DATE_TRUNC('week', …) 后，测试要断言的是「跨周的两条记录落在两个桶里」。
func TestXiaohongshuCardStats_GetCardDailyStats_Week(t *testing.T) {
	repo, ctx := setupXiaohongshuCardStatsRepo(t)
	card := newTestCard(t, repo, "W1", 0, true)

	now := time.Now()
	newTestActivity(t, repo, card.ID, 1, "view", now)
	// 相隔 14 天必定跨周（也跨月），用它保证「两个桶」不是碰运气。
	newTestActivity(t, repo, card.ID, 2, "view", now.Add(-14*24*time.Hour))

	stats, err := repo.GetCardDailyStats(ctx, card.ID, "", "", "week")
	require.NoError(t, err)
	require.Len(t, stats, 2, "相隔 14 天的两条应分到两个周桶")

	dates := make(map[string]bool, len(stats))
	total := 0
	for _, s := range stats {
		assert.Len(t, s.Date, 10, "week 分支按 ISO 周首日输出 YYYY-MM-DD")
		assert.Regexp(t, `^\d{4}-\d{2}-\d{2}$`, s.Date)
		dates[s.Date] = true
		total += s.Count
	}
	assert.Len(t, dates, 2)
	assert.Equal(t, 2, total)
}

// TestXiaohongshuCardStats_GetCardDailyStats_Month 覆盖 month 分支的真实分组行为（同上，原为 DATE_FORMAT）。
func TestXiaohongshuCardStats_GetCardDailyStats_Month(t *testing.T) {
	repo, ctx := setupXiaohongshuCardStatsRepo(t)
	card := newTestCard(t, repo, "M1", 0, true)

	now := time.Now()
	newTestActivity(t, repo, card.ID, 1, "view", now)
	// 40 天 > 任何月份长度，必定跨月。
	newTestActivity(t, repo, card.ID, 2, "view", now.Add(-40*24*time.Hour))

	stats, err := repo.GetCardDailyStats(ctx, card.ID, "", "", "month")
	require.NoError(t, err)
	require.Len(t, stats, 2, "相隔 40 天的两条应分到两个月桶")

	seen := make(map[string]bool, len(stats))
	for _, s := range stats {
		assert.Regexp(t, `^\d{4}-\d{2}$`, s.Date, "month 分支输出 YYYY-MM")
		seen[s.Date] = true
	}
	assert.Len(t, seen, 2)
	assert.Contains(t, seen, timeutil.BusinessDate(now)[:7], "本月桶必须存在")
}

func TestXiaohongshuCardStats_GetCardDailyStats_Default(t *testing.T) {
	repo, ctx := setupXiaohongshuCardStatsRepo(t)
	card := newTestCard(t, repo, "DEF", 0, true)

	newTestActivity(t, repo, card.ID, 1, "view", time.Now())

	stats, err := repo.GetCardDailyStats(ctx, card.ID, "", "", "")
	require.NoError(t, err)
	require.NotEmpty(t, stats)
	assert.Equal(t, 1, stats[0].Count)
}

func TestXiaohongshuCardStats_GetCardDailyStats_WithDateRange(t *testing.T) {
	repo, ctx := setupXiaohongshuCardStatsRepo(t)
	card := newTestCard(t, repo, "DR", 0, true)

	now := time.Now()
	newTestActivity(t, repo, card.ID, 1, "view", now)
	newTestActivity(t, repo, card.ID, 2, "view", now.Add(-2*24*time.Hour))
	newTestActivity(t, repo, card.ID, 3, "view", now.Add(-30*24*time.Hour))

	start := timeutil.BusinessDate(now.Add(-7 * 24 * time.Hour))
	end := timeutil.BusinessDate(now.Add(24 * time.Hour))

	stats, err := repo.GetCardDailyStats(ctx, card.ID, start, end, "day")
	require.NoError(t, err)

	total := 0
	for _, s := range stats {
		total += s.Count
	}
	assert.Equal(t, 2, total, "应只包含范围内的 2 条")
}

// TestXiaohongshuCardStats_GetCardDailyStats_EndDateIsInclusive 钉住「结束日整天算在区间内」。
//
// 旧写法 `created_at <= 'YYYY-MM-DD'` 在 PG 里等于 `<= 当日 00:00`，
// 结束日那一整天的数据被静默丢掉；改成 `< (:date + 1 day)` 后，
// 起止同为「今天」的区间必须能查到今天的记录。
func TestXiaohongshuCardStats_GetCardDailyStats_EndDateIsInclusive(t *testing.T) {
	repo, ctx := setupXiaohongshuCardStatsRepo(t)
	card := newTestCard(t, repo, "END", 0, true)

	now := time.Now()
	newTestActivity(t, repo, card.ID, 1, "view", now)
	today := timeutil.BusinessDate(now)

	stats, err := repo.GetCardDailyStats(ctx, card.ID, today, today, "day")
	require.NoError(t, err)

	total := 0
	for _, s := range stats {
		// day 分支直接取 DATE(created_at)，驱动返回的是 date→time.Time，落到字符串字段上
		// 形如 "2026-09-20T00:00:00Z"（week/month 走 TO_CHAR 才是纯日期串）；
		// 这里只比前 10 位，格式不统一的问题另记。
		require.GreaterOrEqual(t, len(s.Date), 10)
		assert.Equal(t, today, s.Date[:10], "day 分支的桶必须落在业务日当天")
		total += s.Count
	}
	assert.Equal(t, 1, total, "起止同为今天的区间必须包含今天")
}

func TestXiaohongshuCardStats_GetCardDailyStats_NoData(t *testing.T) {
	repo, ctx := setupXiaohongshuCardStatsRepo(t)
	card := newTestCard(t, repo, "NO", 0, true)

	stats, err := repo.GetCardDailyStats(ctx, card.ID, "", "", "day")
	require.NoError(t, err)
	assert.Len(t, stats, 0)
}

func TestXiaohongshuCardStats_GetOverallDailyStats_All(t *testing.T) {
	repo, ctx := setupXiaohongshuCardStatsRepo(t)

	c1 := newTestCard(t, repo, "O1", 0, true)
	c2 := newTestCard(t, repo, "O2", 0, true)

	newTestActivity(t, repo, c1.ID, 1, "view", time.Now())
	newTestActivity(t, repo, c2.ID, 2, "view", time.Now())
	newTestActivity(t, repo, c1.ID, 3, "click", time.Now())

	stats, err := repo.GetOverallDailyStats(ctx, "", "", "day")
	require.NoError(t, err)
	require.NotEmpty(t, stats)

	total := 0
	for _, s := range stats {
		total += s.Count
	}
	assert.Equal(t, 3, total)
}

func TestXiaohongshuCardStats_GetOverallDailyStats_Week(t *testing.T) {
	repo, ctx := setupXiaohongshuCardStatsRepo(t)
	c1 := newTestCard(t, repo, "OW", 0, true)
	now := time.Now()
	newTestActivity(t, repo, c1.ID, 1, "view", now)
	newTestActivity(t, repo, c1.ID, 2, "view", now.Add(-14*24*time.Hour))

	stats, err := repo.GetOverallDailyStats(ctx, "", "", "week")
	require.NoError(t, err, "week 分支已改为 PG 的 DATE_TRUNC，不该再报错")
	assert.Len(t, stats, 2)
}

func TestXiaohongshuCardStats_GetOverallDailyStats_Month(t *testing.T) {
	repo, ctx := setupXiaohongshuCardStatsRepo(t)
	c1 := newTestCard(t, repo, "OM", 0, true)
	now := time.Now()
	newTestActivity(t, repo, c1.ID, 1, "view", now)
	newTestActivity(t, repo, c1.ID, 2, "view", now.Add(-40*24*time.Hour))

	stats, err := repo.GetOverallDailyStats(ctx, "", "", "month")
	require.NoError(t, err, "month 分支已改为 PG 的 TO_CHAR，不该再报错")
	assert.Len(t, stats, 2)
}

func TestXiaohongshuCardStats_GetOverallDailyStats_WithDateRange(t *testing.T) {
	repo, ctx := setupXiaohongshuCardStatsRepo(t)

	c1 := newTestCard(t, repo, "ODR", 0, true)
	now := time.Now()
	newTestActivity(t, repo, c1.ID, 1, "view", now)
	newTestActivity(t, repo, c1.ID, 2, "view", now.Add(-30*24*time.Hour))

	start := timeutil.BusinessDate(now.Add(-7 * 24 * time.Hour))
	end := timeutil.BusinessDate(now.Add(24 * time.Hour))

	stats, err := repo.GetOverallDailyStats(ctx, start, end, "day")
	require.NoError(t, err)

	total := 0
	for _, s := range stats {
		total += s.Count
	}
	assert.Equal(t, 1, total)
}

func TestXiaohongshuCardStats_GetOverallDailyStats_Empty(t *testing.T) {
	repo, ctx := setupXiaohongshuCardStatsRepo(t)

	stats, err := repo.GetOverallDailyStats(ctx, "", "", "day")
	require.NoError(t, err)
	assert.Len(t, stats, 0)
}
