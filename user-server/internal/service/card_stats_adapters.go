package service

import (
	"context"
	"fmt"
	"time"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/timeutil"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// 平台标识常量（与 model.PlatformXxx 保持一致）
const (
	PlatformCardStatsDouyin      = "douyin"
	PlatformCardStatsKuaishou    = "kuaishou"
	PlatformCardStatsXiaohongshu = "xiaohongshu"
	PlatformCardStatsXianyu      = "xianyu"
	PlatformCardStatsTiktok      = "tiktok"
)

type douyinCardStatsAdapter struct {
	inner DouyinCardStatsService
}

// NewPlatformDouyinCardStatsAdapter 创建抖音统一接口适配器
func NewPlatformDouyinCardStatsAdapter(inner DouyinCardStatsService) PlatformCardStatsService {
	return &douyinCardStatsAdapter{inner: inner}
}

func (a *douyinCardStatsAdapter) Platform() string { return PlatformCardStatsDouyin }

func (a *douyinCardStatsAdapter) GetCardStats(ctx context.Context, req *dto.PlatformCardStatsRequest) (*dto.PlatformCardStatsResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("请求不能为空")
	}
	inner := &dto.DouyinCardStatsRequest{
		CardID:    req.CardID,
		StartDate: req.StartDate,
		EndDate:   req.EndDate,
		GroupBy:   req.GroupBy,
	}
	resp, err := a.inner.GetCardStats(ctx, inner)
	if err != nil {
		return nil, err
	}
	return &dto.PlatformCardStatsResponse{
		Platform:       PlatformCardStatsDouyin,
		CardID:         resp.CardID,
		Title:          resp.Title,
		ViewCount:      resp.ViewCount,
		DailyStats:     resp.DailyStats,
		RecentActivity: resp.RecentActivity,
	}, nil
}

func (a *douyinCardStatsAdapter) GetOverallStats(ctx context.Context, req *dto.PlatformCardOverallStatsRequest) (*dto.PlatformCardOverallStatsResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("请求不能为空")
	}
	inner := &dto.DouyinCardOverallStatsRequest{
		GroupBy:   req.GroupBy,
		StartDate: req.StartDate,
		EndDate:   req.EndDate,
	}
	resp, err := a.inner.GetOverallStats(ctx, inner)
	if err != nil {
		return nil, err
	}
	return &dto.PlatformCardOverallStatsResponse{
		Platform:       PlatformCardStatsDouyin,
		TotalCards:     resp.TotalCards,
		ActiveCards:    resp.ActiveCards,
		TotalViews:     resp.TotalViews,
		PopularCards:   resp.PopularCards,
		DailyStats:     resp.DailyStats,
		RecentActivity: resp.RecentActivity,
	}, nil
}

func (a *douyinCardStatsAdapter) RecordActivity(ctx context.Context, cardID uint, userID uint, action string, username, ipAddress, userAgent string) error {
	return a.inner.RecordActivity(ctx, cardID, userID, action, username, ipAddress, userAgent)
}

type xiaohongshuCardStatsAdapter struct {
	inner XiaohongshuCardStatsService
}

// NewPlatformXiaohongshuCardStatsAdapter 创建小红书统一接口适配器
func NewPlatformXiaohongshuCardStatsAdapter(inner XiaohongshuCardStatsService) PlatformCardStatsService {
	return &xiaohongshuCardStatsAdapter{inner: inner}
}

func (a *xiaohongshuCardStatsAdapter) Platform() string { return PlatformCardStatsXiaohongshu }

func (a *xiaohongshuCardStatsAdapter) GetCardStats(ctx context.Context, req *dto.PlatformCardStatsRequest) (*dto.PlatformCardStatsResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("请求不能为空")
	}
	inner := &dto.XiaohongshuCardStatsRequest{
		CardID:    req.CardID,
		StartDate: req.StartDate,
		EndDate:   req.EndDate,
		GroupBy:   req.GroupBy,
	}
	resp, err := a.inner.GetCardStats(ctx, inner)
	if err != nil {
		return nil, err
	}
	return &dto.PlatformCardStatsResponse{
		Platform:       PlatformCardStatsXiaohongshu,
		CardID:         resp.CardID,
		Title:          resp.Title,
		ViewCount:      resp.ViewCount,
		DailyStats:     resp.DailyStats,
		RecentActivity: resp.RecentActivity,
	}, nil
}

func (a *xiaohongshuCardStatsAdapter) GetOverallStats(ctx context.Context, req *dto.PlatformCardOverallStatsRequest) (*dto.PlatformCardOverallStatsResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("请求不能为空")
	}
	inner := &dto.XiaohongshuCardOverallStatsRequest{
		GroupBy:   req.GroupBy,
		StartDate: req.StartDate,
		EndDate:   req.EndDate,
	}
	resp, err := a.inner.GetOverallStats(ctx, inner)
	if err != nil {
		return nil, err
	}
	return &dto.PlatformCardOverallStatsResponse{
		Platform:       PlatformCardStatsXiaohongshu,
		TotalCards:     resp.TotalCards,
		ActiveCards:    resp.ActiveCards,
		TotalViews:     resp.TotalViews,
		PopularCards:   resp.PopularCards,
		DailyStats:     resp.DailyStats,
		RecentActivity: resp.RecentActivity,
	}, nil
}

func (a *xiaohongshuCardStatsAdapter) RecordActivity(ctx context.Context, cardID uint, userID uint, action string, username, ipAddress, userAgent string) error {
	return a.inner.RecordActivity(ctx, cardID, userID, action, username, ipAddress, userAgent)
}

type kuaishouCardStatsAdapter struct {
	inner *KuaishouCardStatsService
}

// NewPlatformKuaishouCardStatsAdapter 创建快手统一接口适配器
func NewPlatformKuaishouCardStatsAdapter(inner *KuaishouCardStatsService) PlatformCardStatsService {
	return &kuaishouCardStatsAdapter{inner: inner}
}

func (a *kuaishouCardStatsAdapter) Platform() string { return PlatformCardStatsKuaishou }

func (a *kuaishouCardStatsAdapter) GetCardStats(ctx context.Context, req *dto.PlatformCardStatsRequest) (*dto.PlatformCardStatsResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("请求不能为空")
	}
	startDate, endDate := parseDateRange(req.StartDate, req.EndDate)
	inner := &dto.KuaishouCardStatsRequest{
		CardID:    req.CardID,
		StartDate: startDate,
		EndDate:   endDate,
		GroupBy:   req.GroupBy,
	}
	resp, err := a.inner.GetCardStats(ctx, inner)
	if err != nil {
		return nil, err
	}
	return &dto.PlatformCardStatsResponse{
		Platform:   PlatformCardStatsKuaishou,
		CardID:     resp.CardID,
		Title:      resp.CardTitle,
		ViewCount:  resp.TotalViews,
		DailyStats: resp.DailyStats,
	}, nil
}

func (a *kuaishouCardStatsAdapter) GetOverallStats(ctx context.Context, req *dto.PlatformCardOverallStatsRequest) (*dto.PlatformCardOverallStatsResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("请求不能为空")
	}
	startDate, endDate := parseDateRange(req.StartDate, req.EndDate)
	inner := &dto.KuaishouCardOverallStatsRequest{
		StartDate: startDate,
		EndDate:   endDate,
		GroupBy:   req.GroupBy,
		Limit:     req.Limit,
	}
	resp, err := a.inner.GetOverallStats(ctx, inner)
	if err != nil {
		return nil, err
	}
	return &dto.PlatformCardOverallStatsResponse{
		Platform:       PlatformCardStatsKuaishou,
		TotalCards:     resp.TotalCards,
		ActiveCards:    resp.ActiveCards,
		TotalViews:     resp.TotalViews,
		PopularCards:   resp.PopularCards,
		DailyStats:     resp.DailyStats,
		RecentActivity: resp.RecentActivities,
	}, nil
}

func (a *kuaishouCardStatsAdapter) RecordActivity(ctx context.Context, cardID uint, userID uint, action string, username, ipAddress, userAgent string) error {
	_ = userID
	_ = username
	return a.inner.RecordActivity(ctx, cardID, action, ipAddress, userAgent, "")
}

type xianyuCardStatsAdapter struct {
	inner XianyuCardStatsService
}

// NewPlatformXianyuCardStatsAdapter 创建闲鱼统一接口适配器
func NewPlatformXianyuCardStatsAdapter(inner XianyuCardStatsService) PlatformCardStatsService {
	return &xianyuCardStatsAdapter{inner: inner}
}

func (a *xianyuCardStatsAdapter) Platform() string { return PlatformCardStatsXianyu }

func (a *xianyuCardStatsAdapter) GetCardStats(ctx context.Context, req *dto.PlatformCardStatsRequest) (*dto.PlatformCardStatsResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("请求不能为空")
	}
	startDate, endDate := normalizeDateRange(req.StartDate, req.EndDate)
	resp, err := a.inner.GetCardStats(ctx, req.CardID, startDate, endDate)
	if err != nil {
		return nil, err
	}
	return &dto.PlatformCardStatsResponse{
		Platform:       PlatformCardStatsXianyu,
		CardID:         resp.CardID,
		Title:          resp.Title,
		ViewCount:      resp.ViewCount,
		ClickCount:     resp.ClickCount,
		ShareCount:     resp.ShareCount,
		DailyStats:     resp.DailyStats,
		RecentActivity: resp.RecentActivity,
	}, nil
}

func (a *xianyuCardStatsAdapter) GetOverallStats(ctx context.Context, req *dto.PlatformCardOverallStatsRequest) (*dto.PlatformCardOverallStatsResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("请求不能为空")
	}
	startDate, endDate := normalizeDateRange(req.StartDate, req.EndDate)
	resp, err := a.inner.GetOverallStats(ctx, startDate, endDate)
	if err != nil {
		return nil, err
	}
	return &dto.PlatformCardOverallStatsResponse{
		Platform:       PlatformCardStatsXianyu,
		TotalCards:     resp.TotalCards,
		ActiveCards:    resp.ActiveCards,
		TotalViews:     resp.TotalViews,
		TotalClicks:    resp.TotalClicks,
		TotalShares:    resp.TotalShares,
		PopularCards:   resp.PopularCards,
		DailyStats:     resp.DailyStats,
		RecentActivity: resp.RecentActivity,
	}, nil
}

func (a *xianyuCardStatsAdapter) RecordActivity(ctx context.Context, cardID uint, userID uint, action string, username, ipAddress, userAgent string) error {
	_ = userID
	_ = username
	switch action {
	case "view":
		return a.inner.RecordView(ctx, cardID, ipAddress, userAgent, "")
	case "click":
		return a.inner.RecordClick(ctx, cardID, ipAddress, userAgent, "")
	case "share":
		return a.inner.RecordShare(ctx, cardID, ipAddress, userAgent, "")
	default:
		return a.inner.RecordView(ctx, cardID, ipAddress, userAgent, "")
	}
}

type tiktokCardStatsAdapter struct {
	inner    TikTokCardService
	activity repository.TikTokCardRepository
}

// NewPlatformTiktokCardStatsAdapter 创建 TikTok 统一接口适配器
//
// 注：保留 gormDB *gorm.DB 入参以维持向后兼容（router 装配不改动），
// 内部在构造函数中实例化 repository，service struct 不直接持有 *gorm.DB。
func NewPlatformTiktokCardStatsAdapter(inner TikTokCardService, gormDB *gorm.DB) PlatformCardStatsService {
	return &tiktokCardStatsAdapter{inner: inner, activity: repository.NewTikTokCardRepository(gormDB)}
}

func (a *tiktokCardStatsAdapter) Platform() string { return PlatformCardStatsTiktok }

func (a *tiktokCardStatsAdapter) GetCardStats(ctx context.Context, req *dto.PlatformCardStatsRequest) (*dto.PlatformCardStatsResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("请求不能为空")
	}
	resp, err := a.inner.Stats(ctx, req.CardID)
	if err != nil {
		return nil, err
	}
	daily := make([]dto.DailyStat, 0, len(resp.DailyStats))
	for _, d := range resp.DailyStats {
		daily = append(daily, dto.DailyStat{
			Date: d.Date,
			View: int(d.ViewCount),
		})
	}
	return &dto.PlatformCardStatsResponse{
		Platform:   PlatformCardStatsTiktok,
		CardID:     resp.CardID,
		Title:      resp.Title,
		ViewCount:  resp.ViewCount,
		DailyStats: daily,
	}, nil
}

func (a *tiktokCardStatsAdapter) GetOverallStats(ctx context.Context, req *dto.PlatformCardOverallStatsRequest) (*dto.PlatformCardOverallStatsResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("请求不能为空")
	}
	resp, err := a.inner.StatsOverall(ctx)
	if err != nil {
		return nil, err
	}
	daily := make([]dto.DailyStat, 0, len(resp.DailyStats))
	for _, d := range resp.DailyStats {
		daily = append(daily, dto.DailyStat{
			Date: d.Date,
			View: int(d.ViewCount),
		})
	}
	popular := make([]dto.PopularCard, 0, len(resp.PopularCards))
	for _, p := range resp.PopularCards {
		popular = append(popular, dto.PopularCard{
			ID:        p.ID,
			Title:     p.Title,
			ViewCount: int(p.ViewCount),
			CreatedAt: p.CreatedAt,
		})
	}
	recent := make([]dto.Activity, 0, len(resp.RecentActivity))
	for _, r := range resp.RecentActivity {
		recent = append(recent, dto.Activity{
			CardID:    0,
			Action:    r.Action,
			Username:  r.Username,
			CreatedAt: r.CreatedAt,
		})
	}
	return &dto.PlatformCardOverallStatsResponse{
		Platform:       PlatformCardStatsTiktok,
		TotalCards:     int(resp.TotalCards),
		ActiveCards:    int(resp.ActiveCards),
		TotalViews:     int(resp.TotalViews),
		PopularCards:   popular,
		DailyStats:     daily,
		RecentActivity: recent,
	}, nil
}

func (a *tiktokCardStatsAdapter) RecordActivity(ctx context.Context, cardID uint, userID uint, action string, username, ipAddress, userAgent string) error {
	if err := a.inner.RecordView(ctx, cardID, ipAddress, userAgent); err != nil {
		return err
	}
	if a.activity != nil && (userID > 0 || username != "") {
		uidStr := fmt.Sprintf("%d", userID)
		ua := userAgent
		if username != "" {
			ua = "[" + username + "] " + userAgent
		}
		activity := &model.TikTokCardActivity{
			CardID:       cardID,
			ActivityType: action,
			UserID:       uidStr,
			IPAddress:    ipAddress,
			UserAgent:    ua,
			Platform:     PlatformCardStatsTiktok,
		}
		if err := a.activity.CreateActivity(ctx, activity); err != nil {
			return err
		}
	}
	return nil
}

func parseDateRange(start, end string) (time.Time, time.Time) {
	var s, e time.Time
	if start != "" {
		if t, err := time.Parse("2006-01-02", start); err == nil {
			s = t
		}
	}
	if end != "" {
		if t, err := time.Parse("2006-01-02", end); err == nil {
			e = t
		}
	}
	return s, e
}

func normalizeDateRange(start, end string) (string, string) {
	// 默认窗口必须用业务日（CST）：这两个字符串会被下推到 `created_at::date`，
	// 而 PG 会话时区钉在 Asia/Shanghai，用宿主机时区（CI/容器常为 UTC）的
	// Format 会在 UTC 16:00–23:59 之间让窗口整体错一天。
	now := time.Now()
	if end == "" {
		end = timeutil.BusinessDate(now)
	}
	if start == "" {
		start = timeutil.BusinessDate(now.AddDate(0, 0, -30))
	}
	return start, end
}
