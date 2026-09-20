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
	OnlineChannels   int              `json:"online_channels"`
	OfflineChannels  int              `json:"offline_channels"`
	ReplayedMessages int64            `json:"replayed_messages"`
	FailedMessages   int64            `json:"failed_messages"`
	OfflineSnapshots []OfflineChannel `json:"offline_snapshots,omitempty"`
	StartedAt        time.Time        `json:"started_at"`
	FinishedAt       time.Time        `json:"finished_at"`
}

// partitionBridgeChannels 按 status 把渠道划成在线/离线两批。
//
// 两侧必须互斥且并集为全集：旧实现里"离线"取自一条永远报 42703 的 bridge_metrics
// 聚合查询、"在线"根本没有这一侧，于是回扫既检不出渠道，也谈不上投递。
// status 的可靠性由 SSE 生命周期写在线位负责（连接/心跳刷新、断开置离线）。
func partitionBridgeChannels(rows []repository.BridgeChannelRow) (on, off []OfflineChannel) {
	for _, a := range rows {
		ch := OfflineChannel{Platform: a.Channel, AccountID: a.AccountID, OfflineSince: a.UpdatedAt}
		if a.Status == bridgeStatusOffline {
			off = append(off, ch)
			continue
		}
		on = append(on, ch)
	}
	return on, off
}

// bridgeStatusOffline bridge_accounts.status 的离线态字面量（与 bridge 包 Upsert/SetOffline 同值）
const bridgeStatusOffline = "offline"

// detectBridgeChannels 取一次渠道账号快照，切成在线/离线两批。
func (s *BridgeOfflineReplayService) detectBridgeChannels(ctx context.Context) (on, off []OfflineChannel, err error) {
	if s.repo == nil {
		return nil, nil, nil
	}
	rows, err := s.repo.ListBridgeAccounts(ctx)
	if err != nil {
		return nil, nil, err
	}
	on, off = partitionBridgeChannels(rows)
	return on, off, nil
}

// DetectOfflineChannels 判定为离线的渠道（供报告与观测）。
func (s *BridgeOfflineReplayService) DetectOfflineChannels(ctx context.Context) ([]OfflineChannel, error) {
	_, off, err := s.detectBridgeChannels(ctx)
	return off, err
}

// DetectOnlineChannels 判定为在线的渠道（补投目标）。
func (s *BridgeOfflineReplayService) DetectOnlineChannels(ctx context.Context) ([]OfflineChannel, error) {
	on, _, err := s.detectBridgeChannels(ctx)
	return on, err
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

	online, offline, err := s.detectBridgeChannels(ctx)
	if err != nil {
		logger.Warnf("[BridgeReplay] 渠道快照读取失败: %v", err)
	}
	channels := make([]OfflineChannel, 0, len(online)+len(offline))
	channels = append(channels, online...)
	channels = append(channels, offline...)
	stats.ScannedChannels = len(channels)
	stats.OnlineChannels = len(online)
	stats.OfflineChannels = len(offline)
	stats.OfflineSnapshots = offline

	perChannelLimit := 50
	for _, ch := range channels {
		r, f := s.ReplayDelayedOutbound(ctx, ch.Platform, ch.AccountID, perChannelLimit)
		stats.ReplayedMessages += r
		stats.FailedMessages += f
	}

	stats.FinishedAt = time.Now()
	logger.Infof("[BridgeReplay] 回扫完成: scanned=%d online=%d offline=%d replayed=%d failed=%d duration=%s",
		stats.ScannedChannels, stats.OnlineChannels, stats.OfflineChannels,
		stats.ReplayedMessages, stats.FailedMessages,
		stats.FinishedAt.Sub(startedAt).Round(time.Millisecond))
	return stats
}
