package service

import (
	"context"
	"fmt"
	"sort"
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
	// 孤儿结算一格：候选行数与实际落 failed 的行数并列，二者不等就是探针挡下的量。
	// OrphanDryRun 记下这一轮跑在哪一侧：读报表的人必须先知道 settled=0 是"没得结算"
	// 还是"闸门关着"，否则 0 会被当成队列已经干净。
	OrphanDryRun        bool      `json:"orphan_dry_run"`
	OrphanCandidateRows int64     `json:"orphan_candidate_rows"`
	OrphanSkippedOnline int64     `json:"orphan_skipped_reachable_rows"`
	OrphanSettledRows   int64     `json:"orphan_settled_rows"`
	StartedAt           time.Time `json:"started_at"`
	FinishedAt          time.Time `json:"finished_at"`
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

	// 补投只能救「还会回来」的渠道；不会回来的那批必须在这一轮末尾结算掉，否则队列对它们是无限的。
	//
	// 缺省只报数不写：这道 UPDATE 不可逆（真回复从此离开待办集合），而回扫是每 5 分钟自动跑的
	// 后台任务 —— 开发态存盘即热重载进真实例、连的是真库，"先人工确认再放量"如果只靠流程约定，
	// 实际等于没有闸门（实测：闸门上线前的一轮 cron 就把 38 行历史 pending 直接烧成 failed）。
	// 要真结算，运维读到达标口径后把 bridge.outbound_orphan_dry_run 置 false。
	orphanDryRun := bridgeOrphanSettlementDryRun(ctx)
	orphan, oerr := s.SettleOrphanBridgeOutbound(ctx, bridgeOrphanOutboundTTL(ctx), orphanDryRun)
	switch {
	case oerr != nil:
		// 结算失败只是这一轮没清掉，行仍在；报错而不是静默，是为了让"积压只增不减"可归因。
		logger.Warnf("[BridgeReplay] 孤儿出站结算失败: %v", oerr)
	case orphan.ProbeMissing:
		logger.Warnf("[BridgeReplay] 可达性探针未注册，本轮跳过孤儿出站结算（候选 %d 行保持原状）", orphan.CandidateRows)
	case orphanDryRun:
		logger.Infof("[BridgeReplay] 孤儿出站结算 dry-run（未写库）: ttl=%ds groups=%d candidates=%d skipped_reachable=%d —— 确认口径后置 bridge.outbound_orphan_dry_run=false 才会真正落 failed",
			orphan.TTLSeconds, orphan.Groups, orphan.CandidateRows, orphan.SkippedReachable)
	case orphan.SettledRows > 0 || orphan.SkippedReachable > 0:
		logger.Infof("[BridgeReplay] 孤儿出站结算: ttl=%ds groups=%d candidates=%d skipped_reachable=%d settled=%d",
			orphan.TTLSeconds, orphan.Groups, orphan.CandidateRows, orphan.SkippedReachable, orphan.SettledRows)
	}
	stats.OrphanCandidateRows = orphan.CandidateRows
	stats.OrphanSkippedOnline = orphan.SkippedReachable
	stats.OrphanSettledRows = orphan.SettledRows
	stats.OrphanDryRun = orphanDryRun

	stats.FinishedAt = time.Now()
	// 计数名用「不可达」而不是「无订阅者」：轮询式下发的账号没有订阅，
	// 它是在线位过期才落到这一格的，取证时按"没连 SSE"读会找错方向。
	logger.Infof("[BridgeReplay] 回扫完成: scanned=%d online=%d offline=%d unreachable_channels=%d replayed=%d failed=%d duration=%s",
		stats.ScannedChannels, stats.OnlineChannels, stats.OfflineChannels, stats.SkippedOffline,
		stats.ReplayedMessages, stats.FailedMessages,
		stats.FinishedAt.Sub(startedAt).Round(time.Millisecond))
	return stats
}

// --- 孤儿出站结算 ----------------------------------------------------------

const (
	// BridgeOutboundOrphanDefaultTTL 阈值缺省值：配置项缺失或读不动时用它。
	BridgeOutboundOrphanDefaultTTL = 7 * 24 * time.Hour
	// bridgeOutboundOrphanParam 配置项（group=bridge，duration 语义=秒）。
	bridgeOutboundOrphanParam = "outbound_orphan_ttl"
	// bridgeOutboundOrphanDryRunParam 配置项：true＝只报数不写库（缺省），false＝真结算。
	bridgeOutboundOrphanDryRunParam = "outbound_orphan_dry_run"
	// bridgeOutboundOrphanReason 写进 message_hub.push_error 的原因，
	// 与重推到界那条（outbound_push_exhausted）同列同规格，事后按这一列就能分清两种判弃。
	bridgeOutboundOrphanReason = "outbound_orphan_expired"
	// bridgeOutboundOrphanBatchLimit 一轮最多结算多少行：五分钟的节拍上下限够用，
	// 也让一次装配事故（阈值被配成分钟级）不至于在一轮里刷掉整张表。
	bridgeOutboundOrphanBatchLimit = 1000
)

// OrphanSettlement 一轮孤儿结算的报数。dry-run 与实跑共用同一判定，只差最后那条 UPDATE。
type OrphanSettlement struct {
	TTLSeconds       int64 `json:"ttl_seconds"`
	Groups           int   `json:"groups"`
	CandidateRows    int64 `json:"candidate_rows"`
	SkippedReachable int64 `json:"skipped_reachable_rows"`
	SettledRows      int64 `json:"settled_rows"`
	DryRun           bool  `json:"dry_run"`
	// ProbeMissing：可达性探针没装时整轮不结算。这道写入不可逆，判据未知就当"全部可达"，
	// 代价是一轮空转；反过来当"全不可达"的代价是误烧真回复，且没有任何日志能追回。
	ProbeMissing bool `json:"probe_missing"`
	// SkippedDisabled：阈值 <=0（配置里写 0 即停用本结算）。
	SkippedDisabled bool `json:"skipped_disabled"`
}

// bridgeOrphanOutboundTTL 读阈值。GlobalConfigParam 本身 nil-safe（未装配时返回走
// fallback 的空壳实例），所以这里不需要再判一次实例存不存在。
func bridgeOrphanOutboundTTL(ctx context.Context) time.Duration {
	return GlobalConfigParam().GetDuration(ctx, "bridge", bridgeOutboundOrphanParam, BridgeOutboundOrphanDefaultTTL)
}

// bridgeOrphanSettlementDryRun 读"只报数"开关；读不到配置按 true。
//
// 缺省偏保守与探针缺失那条同口径：不可逆写入在判据未知时一律不动手。
func bridgeOrphanSettlementDryRun(ctx context.Context) bool {
	return GlobalConfigParam().GetBool(ctx, "bridge", bridgeOutboundOrphanDryRunParam, true)
}

// bridgeChannelNames 已注册的桥接渠道名（排序只为让日志与用例断言稳定）。
func bridgeChannelNames() []string {
	names := make([]string, 0, len(bridgeChannels))
	for n := range bridgeChannels {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// SettleOrphanBridgeOutbound 结算「桥接渠道里再没有人会来拉」的出站行。
//
// 为什么必须有人结算：MaxOutboundPushAttempts 那道 20 次上界只在三条取行路径里生效，
// 而取行的前提是有账号来拉。账号一旦不再注册/不再同步，那批行既不会被认领、也就永远不会
// 累计 attempts —— 队列对它们是无限的。用户侧看到的事实是「AI 生成过回复、客户永远收不到」，
// 而巡检里的积压数字随天数只增不减，永远清不掉。
//
// dryRun=true 时只报数不写：这是一条不可逆的写入（把真回复标成 failed），上线前必须先报数。
func (s *BridgeOfflineReplayService) SettleOrphanBridgeOutbound(ctx context.Context, ttl time.Duration, dryRun bool) (OrphanSettlement, error) {
	out := OrphanSettlement{TTLSeconds: int64(ttl.Seconds()), DryRun: dryRun}
	if ttl <= 0 {
		out.SkippedDisabled = true
		return out, nil
	}
	if s.repo == nil {
		return out, nil
	}
	platforms := bridgeChannelNames()
	if len(platforms) == 0 {
		return out, nil
	}
	groups, err := s.repo.ListOrphanOutboundGroups(ctx, platforms, ttl)
	if err != nil {
		return out, err
	}
	probe := loadBridgeChannelOnlineProbe()
	if probe == nil {
		out.ProbeMissing = true
		out.CandidateRows = sumOrphanRows(groups)
		return out, nil
	}
	settleTargets := make([]repository.OrphanOutboundGroup, 0, len(groups))
	for _, g := range groups {
		out.Groups++
		out.CandidateRows += g.Rows
		if probe(ctx, g.Platform, g.AccountID) {
			// 此刻收得到（挂着 SSE，或宽限窗内同步过）就不烧：让下一轮回扫照常补投。
			out.SkippedReachable += g.Rows
			continue
		}
		settleTargets = append(settleTargets, g)
	}
	if dryRun || len(settleTargets) == 0 {
		return out, nil
	}
	settled, err := s.repo.SettleOrphanOutbound(ctx, settleTargets, ttl, bridgeOutboundOrphanReason, bridgeOutboundOrphanBatchLimit)
	if err != nil {
		return out, err
	}
	out.SettledRows = settled
	return out, nil
}

func sumOrphanRows(groups []repository.OrphanOutboundGroup) int64 {
	var n int64
	for _, g := range groups {
		n += g.Rows
	}
	return n
}
