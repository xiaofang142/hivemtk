package service

import (
	"context"
	"fmt"
	"time"

	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

type BridgeOfflineReplayService struct {
	repo *repository.BridgeOfflineReplayRepository
}

// NewBridgeOfflineReplayService 创建离线回扫服务
func NewBridgeOfflineReplayService() *BridgeOfflineReplayService {
	return &BridgeOfflineReplayService{repo: repository.NewBridgeOfflineReplayRepository()}
}

// NewBridgeOfflineReplayServiceWithDB 注入 DB（测试用）
func (s *BridgeOfflineReplayService) WithDB(d *gorm.DB) *BridgeOfflineReplayService {
	s.repo = repository.NewBridgeOfflineReplayRepositoryWithDB(d)
	return s
}

// OfflineChannel 离线渠道快照
type OfflineChannel struct {
	Platform     string    `json:"platform"`
	AccountID    string    `json:"account_id"`
	OfflineSince time.Time `json:"offline_since"`
}

// ReplayStats 回扫统计
type ReplayStats struct {
	ScannedChannels  int              `json:"scanned_channels"`
	OfflineChannels  int              `json:"offline_channels"`
	ReplayedMessages int64            `json:"replayed_messages"`
	FailedMessages   int64            `json:"failed_messages"`
	OfflineSnapshots []OfflineChannel `json:"offline_snapshots,omitempty"`
	StartedAt        time.Time        `json:"started_at"`
	FinishedAt       time.Time        `json:"finished_at"`
}

// DetectOfflineChannels 检测离线渠道
//
// 判定规则：bridge_metrics 中最近 10 分钟内无新消息到达 → 视为离线
// （同时 fallback 到 bridge_accounts 中 status != "online" 的渠道）
func (s *BridgeOfflineReplayService) DetectOfflineChannels(ctx context.Context) ([]OfflineChannel, error) {
	if s.repo == nil {
		return nil, nil
	}

	stats, err := s.repo.GroupBridgeMetricsLastSeen(ctx)
	if err != nil {

		logger.Warnf("[BridgeReplay] bridge_metrics 查询失败，fallback bridge_accounts: %v", err)
		accs, err2 := s.repo.ListNonOnlineBridgeAccounts(ctx)
		if err2 != nil {
			return nil, err2
		}
		now := time.Now()
		out := make([]OfflineChannel, 0, len(accs))
		for _, a := range accs {
			if now.Sub(a.UpdatedAt) > 10*time.Minute {
				out = append(out, OfflineChannel{
					Platform:     a.Platform,
					AccountID:    a.AccountID,
					OfflineSince: a.UpdatedAt,
				})
			}
		}
		return out, nil
	}

	threshold := time.Now().Add(-10 * time.Minute)
	out := make([]OfflineChannel, 0)
	for _, st := range stats {
		if st.LastSeen.Before(threshold) {
			out = append(out, OfflineChannel{
				Platform:     st.Platform,
				AccountID:    st.AccountID,
				OfflineSince: st.LastSeen,
			})
		}
	}
	return out, nil
}

// ReplayDelayedOutbound 重放某个渠道累积的离线消息
//
// 从 reach_delayed_outbound 取到期的 pending 行，先抢占（pending→sending）再投到
// DeliverBridgeOutbound 出站管道，成功收口 sent、失败回到 pending 累计 attempts。
// 收口写失败必须报警：旧实现用 `_ =` 吞掉 42703，行永远停在 pending，
// 同一条消息每 5 分钟重投一次。
func (s *BridgeOfflineReplayService) ReplayDelayedOutbound(ctx context.Context, platform, accountID string, limit int) (replayed, failed int64) {
	if s.repo == nil {
		return 0, 0
	}
	msgs, err := s.repo.ListPendingDelayedOutbound(ctx, platform, accountID, limit)
	if err != nil {
		logger.Warnf("[BridgeReplay] 查询 reach_delayed_outbound 失败: %v", err)
		return 0, 0
	}
	for _, m := range msgs {
		if len(m.Cards) > 0 {
			// 带富卡体的回复交给 H-3 主链路投递：桥接管道只发文本，这里强投等于丢卡。
			continue
		}
		if m.Attempts >= sendRetryMaxAttempts {
			if aerr := s.repo.MarkDelayedOutboundAbandoned(ctx, m.ID,
				fmt.Sprintf("离线回扫重投次数用尽（%d 次）", m.Attempts)); aerr != nil {
				failed++
				logger.Warnf("[BridgeReplay] 判弃失败 id=%d: %v", m.ID, aerr)
			}
			continue
		}
		won, cerr := s.repo.ClaimDelayedOutboundForReplay(ctx, m.ID)
		if cerr != nil {
			failed++
			logger.Warnf("[BridgeReplay] 抢占失败 id=%d: %v", m.ID, cerr)
			continue
		}
		if !won {
			// 主链路已把这条抢去投递了，这里再投就是双发。
			continue
		}
		err := DeliverBridgeOutbound(ctx, m.Platform, m.AccountID, m.ConversationID, "text", m.Content, "")
		if err != nil {
			failed++
			logger.Warnf("[BridgeReplay] 重放失败 id=%d err=%v", m.ID, err)
			if werr := s.repo.MarkDelayedOutboundReplayFailed(ctx, m.ID, err.Error()); werr != nil {
				logger.Warnf("[BridgeReplay] 失败回写未落库 id=%d: %v（行留在 sending，由主链路观测回收）", m.ID, werr)
			}
			continue
		}
		replayed++
		if werr := s.repo.MarkDelayedOutboundReplayed(ctx, m.ID); werr != nil {
			logger.Warnf("[BridgeReplay] 成功回写未落库 id=%d: %v（已送达客户，行留在 sending 仅为状态漂移）", m.ID, werr)
		}
	}
	return replayed, failed
}

// RunOnce 执行一次完整的离线回扫
// 供 cron 调用（每 5 分钟）
func (s *BridgeOfflineReplayService) RunOnce(ctx context.Context) ReplayStats {
	startedAt := time.Now()
	stats := ReplayStats{StartedAt: startedAt}

	channels, err := s.DetectOfflineChannels(ctx)
	if err != nil {
		logger.Warnf("[BridgeReplay] DetectOfflineChannels 出错: %v", err)
	}
	stats.ScannedChannels = len(channels)
	stats.OfflineChannels = len(channels)
	stats.OfflineSnapshots = channels

	perChannelLimit := 50
	for _, ch := range channels {
		r, f := s.ReplayDelayedOutbound(ctx, ch.Platform, ch.AccountID, perChannelLimit)
		stats.ReplayedMessages += r
		stats.FailedMessages += f
	}

	stats.FinishedAt = time.Now()
	logger.Infof("[BridgeReplay] 离线回扫完成: offline=%d replayed=%d failed=%d duration=%s",
		stats.OfflineChannels, stats.ReplayedMessages, stats.FailedMessages,
		stats.FinishedAt.Sub(startedAt).Round(time.Millisecond))
	return stats
}
