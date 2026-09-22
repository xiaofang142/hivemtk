package service

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
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
	SkippedOffline   int              `json:"skipped_offline_channels"`
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

// --- 补投门：扩展此刻收不收得到消息 ---------------------------------------
//
// 单看 status 不能当这道门的判据：
//   - 入站通道的 upsert 与心跳会按「最后收到的渠道键」把整串账号刷成 online，
//     扩展只是没连 SSE 时它们照样显示在线（webhook 类渠道甚至根本没有 SSE）。
//
// 但也**不能只看 SSE 订阅表**：下行有两条路（user-web/bridge/src/core/polling-loop.js），
// 服务端支持 SSE 时挂流、否则每 1.5s 轮询 GET /api/bridge/outbox，
// 运维可按 docs/TROUBLESHOOTING.md 用 FF_SSE_BRIDGE=0 强制回退轮询。只认订阅会让
// 轮询模式下所有延后出站被逐轮跳过且不报错。
//
// 因此真值由 bridge 侧的 BridgeChannelOnline 给出（活订阅 OR 宽限窗内同步过，
// 轮询与心跳都会刷新同步时间）。service 不能 import bridge（循环依赖），
// 故由 bridge 的装配口 SetOutboxQuerier 注入探针，与认领器同一处、同一口径。

// bridgeChannelOnlineProbe 该渠道账号此刻是否收得到消息。带 ctx：探针在 bridge 侧
// 要读账号行的在线位（轮询式下发不留订阅，只有 DB 里刷过的同步时间），读库得挂在调用方 ctx 上。
//
// 读写只走紧随其后的三扇 accessor（bridgeProbeMu 守，包内其余位置直读 0 处）：补投门在
// cron 的 RunOnce 协程里逐渠道读它，而用例逐格装/拆探针。
var (
	bridgeProbeMu sync.RWMutex

	bridgeChannelOnlineProbe func(ctx context.Context, channel, accountID string) bool
)

func loadBridgeChannelOnlineProbe() func(ctx context.Context, channel, accountID string) bool {
	bridgeProbeMu.RLock()
	defer bridgeProbeMu.RUnlock()
	return bridgeChannelOnlineProbe
}

func storeBridgeChannelOnlineProbe(fn func(ctx context.Context, channel, accountID string) bool) {
	bridgeProbeMu.Lock()
	defer bridgeProbeMu.Unlock()
	bridgeChannelOnlineProbe = fn
}

// probeWarned 探针缺件是否已告警过：缺件是一次性装配问题，不该每轮每渠道刷一条。
var probeWarned atomic.Bool

// SetBridgeChannelOnlineProbe 由 bridge 包在装配 SSE 出站口时调用一次。
func SetBridgeChannelOnlineProbe(fn func(ctx context.Context, channel, accountID string) bool) {
	storeBridgeChannelOnlineProbe(fn)
	logger.Info("[BridgeReplay] 可达性探针已注册：延后出站仅补投给收得到的渠道")
}

// bridgeChannelOnline 探针缺件时放行：装配缺件是「门没加」，不是「所有渠道都离线」，
// 后者会把可达的延后出站永久扣在待办集合里。缺件必须留痕而不是静默兜底。
func bridgeChannelOnline(ctx context.Context, channel, accountID string) bool {
	// 取一次再判空再调用：读两遍的话「判空时非空、调用时已被换回 nil」会当场 panic ——
	// 拆探针的用例与跑在协程里的补投门正是这种形状。
	probe := loadBridgeChannelOnlineProbe()
	if probe == nil {
		if !probeWarned.Swap(true) {
			logger.Warn("[BridgeReplay] SSE 在线探针未注册，补投门退化为全量放行（掉线渠道的历史行会被烧进判弃）")
		}
		return true
	}
	return probe(ctx, channel, accountID)
}

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
		// 补投门：另一端收不到（既没挂 SSE、也没在宽限窗内轮询/心跳过）就整条渠道跳过、一行都不碰，
		// 行留在 pending 等下一次可达的那轮；进去走一遍状态机只会把历史行烧成判弃。
		if !bridgeChannelOnline(ctx, ch.Platform, ch.AccountID) {
			stats.SkippedOffline++
			continue
		}
		r, f := s.ReplayDelayedOutbound(ctx, ch.Platform, ch.AccountID, perChannelLimit)
		stats.ReplayedMessages += r
		stats.FailedMessages += f
	}

	stats.FinishedAt = time.Now()
	// 计数名用「不可达」而不是「无订阅者」：轮询式下发的账号没有订阅，
	// 它是在线位过期才落到这一格的，取证时按"没连 SSE"读会找错方向。
	logger.Infof("[BridgeReplay] 回扫完成: scanned=%d online=%d offline=%d unreachable_channels=%d replayed=%d failed=%d duration=%s",
		stats.ScannedChannels, stats.OnlineChannels, stats.OfflineChannels, stats.SkippedOffline,
		stats.ReplayedMessages, stats.FailedMessages,
		stats.FinishedAt.Sub(startedAt).Round(time.Millisecond))
	return stats
}
