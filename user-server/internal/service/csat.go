// csat.go CSAT 满意度服务（五层 L3）
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/timeutil"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// CSATService 满意度调查服务
type CSATService struct {
	repo    *repository.CSATSurveyRepository
	session *repository.CustomerSessionRepository
	kv      repository.SystemConfigKVRepository
	now     func() time.Time
}

// NewCSATService 构造
func NewCSATService() *CSATService {
	return &CSATService{
		repo:    repository.NewCSATSurveyRepository(),
		session: repository.NewCustomerSessionRepository(),
		kv:      repository.NewSystemConfigKVRepository(),
		now:     time.Now,
	}
}

func (s *CSATService) Trigger(ctx context.Context, sessionID, triggeredBy string) (*model.CSATSurvey, error) {
	if triggeredBy == "" {
		triggeredBy = "manual"
	}
	sess, err := s.session.GetBySessionID(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("会话不存在: %w", err)
	}
	survey, err := s.repo.UpsertBySession(ctx, &model.CSATSurvey{
		SessionID:   sessionID,
		OneID:       sess.OneID,
		Status:      model.CSATStatusSent,
		TriggeredBy: triggeredBy,
	})
	if err != nil {
		return nil, err
	}
	// 评分已回收的调查单不得退回 sent（MarkSent 的 SQL 里带同一道守卫，双保险）：
	// 重触发只补发邀请，不改回收状态——否则客户打过分的调查单会从
	// 「按 status=responded 统计」的看板与差评列表里凭空消失。
	if survey.Status != model.CSATStatusResponded {
		if err := s.repo.MarkSent(ctx, sessionID); err != nil {
			return nil, err
		}
		survey.Status = model.CSATStatusSent
		now := s.now()
		survey.SentAt = &now
	} else if triggeredBy != "manual" {
		logger.Ctx(ctx).Info().
			Str("module", "csat").
			Str("session_id", sessionID).
			Msg("skip csat outbound: 该会话评分已回收，自动重触发不再打扰客户")
		return survey, nil
	}

	platform := string(sess.Platform)
	accountID := sess.AccountID
	if accountID == "" {
		logger.Ctx(ctx).Warn().
			Str("module", "csat").
			Str("session_id", sessionID).
			Msg("skip csat outbound: session has empty account_id")
		return survey, nil
	}

	ratingMsg := "本次会话已结束，请为我们的服务打分 ⭐ 1-5 分（回复数字即可）"

	if isBridgeChannel(platform) {
		if err := DeliverBridgeOutbound(ctx, platform, accountID, sessionID, "text", ratingMsg, "csat-trigger"); err != nil {
			logger.Ctx(ctx).Error().Err(err).
				Str("module", "csat").
				Str("channel", platform).
				Str("account_id", accountID).
				Str("session_id", sessionID).
				Msg("csat bridge outbound failed (non-fatal; survey already persisted)")
		} else {
			logger.Ctx(ctx).Info().
				Str("module", "csat").
				Str("channel", platform).
				Str("account_id", accountID).
				Str("session_id", sessionID).
				Msg("csat rating invite pushed via bridge outbound")
		}
	} else {

		logger.Ctx(ctx).Info().
			Str("module", "csat").
			Str("channel", platform).
			Str("account_id", accountID).
			Str("session_id", sessionID).
			Uint("survey_id", survey.ID).
			Msg("CSAT survey created (state=sent, non-bridge channel; awaiting frontend/webhook to pick up)")
	}

	return survey, nil
}

// Submit 提交评分（公开端点：客户提交后回写统计）
func (s *CSATService) Submit(ctx context.Context, sessionID string, score int, comment string) (*model.CSATSurvey, error) {
	if score < 1 || score > 5 {
		return nil, fmt.Errorf("score 必须在 1-5")
	}
	return s.repo.SubmitResponse(ctx, sessionID, score, comment)
}

// csatWindowSince 把看板传来的 window 口径换成"业务日边界"的下界，返回 nil 表示全量。
//
// 边界一律走 timeutil 的业务时区（PG 会话钉在 CST，Go 按宿主机时区格式化，
// 用自然时刻当边界会让同一天的评分被时区裂脑切走一半）。
func csatWindowSince(window string, now time.Time) *time.Time {
	switch window {
	case "month":
		day := timeutil.StartOfBusinessDay(now)
		first := time.Date(day.Year(), day.Month(), 1, 0, 0, 0, 0, day.Location())
		return &first
	case "week", "7d":
		start := timeutil.StartOfBusinessDay(now.AddDate(0, 0, -6))
		return &start
	case "30d":
		start := timeutil.StartOfBusinessDay(now.AddDate(0, 0, -29))
		return &start
	default: // "" / all / 未知口径：不筛，保持历史行为
		return nil
	}
}

// csatWindowLabel 回显统计口径，让卡片副标题说真话（前端不许自己硬写"本月"）
func csatWindowLabel(window string) string {
	switch window {
	case "month":
		return "本月"
	case "week", "7d":
		return "近 7 天"
	case "30d":
		return "近 30 天"
	default:
		return "全部"
	}
}

// negativeThreshold 差评阈值：唯一事实源是模板 low_threshold（默认 3）。
// 看板的"差评数"与差评列表必须共用它，否则会出现卡片按 2 星算、列表按 3 星列的两套答案。
func (s *CSATService) negativeThreshold(ctx context.Context) int {
	threshold := 3
	if v, ok := s.GetTemplate(ctx)["low_threshold"].(float64); ok && v > 0 {
		threshold = int(v)
	}
	return threshold
}

// Stats 统计（window: month|week|7d|30d|空=全量）
//
// 好评率/差评数在这里从**同一份**分布派生：分布已经按窗口过滤过一次，
// 再数一遍比让前端拿 avg_score 反推更可靠（前端拿不到逐档人数就必然口径漂移）。
func (s *CSATService) Stats(ctx context.Context, window string) (map[string]any, error) {
	stats, err := s.repo.Stats(ctx, csatWindowSince(window, s.now()))
	if err != nil {
		return nil, err
	}
	dist, _ := stats["distribution"].([]repository.CSATDistRow)
	responded, _ := stats["responded"].(int64)
	threshold := s.negativeThreshold(ctx)

	var positive, negative int64
	for _, d := range dist {
		switch {
		case d.Score >= 4:
			positive += d.Count
		case d.Score <= threshold:
			negative += d.Count
		}
	}
	rate := 0.0
	if responded > 0 {
		rate = math.Round(float64(positive)/float64(responded)*10000) / 100
	}
	avg, _ := stats["avg_score"].(float64)

	stats["avg_score"] = math.Round(avg*100) / 100
	stats["positive_rate"] = rate
	stats["negative_count"] = negative
	stats["threshold"] = threshold
	stats["window"] = csatWindowLabel(window)
	return stats, nil
}

// Trend 趋势
func (s *CSATService) Trend(ctx context.Context, days int) ([]map[string]any, error) {
	return s.repo.Trend(ctx, days)
}

// Negative 差评列表（阈值取模板 low_threshold，默认 3）
func (s *CSATService) Negative(ctx context.Context, limit int) ([]repository.NegativeRow, int, error) {
	threshold := s.negativeThreshold(ctx)
	list, err := s.repo.ListNegative(ctx, threshold, limit)
	if list == nil {
		// 空结果序列化成 []，不是 null：消费端（看板、坐席端）都按数组遍历，
		// null 会让"今天还没有差评"和"接口坏了"长得一模一样。
		list = []repository.NegativeRow{}
	}
	return list, threshold, err
}

// GetTemplate 模板配置（未配置回退默认）
func (s *CSATService) GetTemplate(ctx context.Context) map[string]any {
	out := map[string]any{}
	raw, err := s.kv.Get(ctx, model.CSATTemplateKey)
	if err == nil && raw != "" {
		if err := json.Unmarshal([]byte(raw), &out); err == nil && len(out) > 0 {
			return out
		}
	}
	_ = json.Unmarshal([]byte(model.CSATDefaultTemplate), &out)
	return out
}

// SaveTemplate 保存模板配置
func (s *CSATService) SaveTemplate(ctx context.Context, tpl map[string]any) error {
	raw, err := json.Marshal(tpl)
	if err != nil {
		return err
	}
	_, err = s.kv.Upsert(ctx, model.CSATTemplateKey, string(raw))
	return err
}
