package service

import (
	"context"

	"encoding/json"

	"fmt"

	"net/url"

	"regexp"
	"sort"

	"strings"

	"sync"

	"time"

	"hivemtk-user/internal/model"

	_db "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"

	agent_runtime "hivemtk-user/internal/aiagent/agent/runtime"

	"hivemtk-user/internal/channelbot/telegram"
)

const (
	aiReplyQuietStartHour = 23
	aiReplyQuietEndHour   = 7

	delayedOutboundPollInterval = 30 * time.Second
	delayedOutboundBatchSize    = 20
	// delayedOutboundTTL 免打扰延迟出站的存活期：send_at 超期 24h 仍 pending 的 AI 回复
	// 判 expired 不再补投（服务长时间停摆后集中重放时，过期回复的打扰大于价值）。
	delayedOutboundTTL = 24 * time.Hour
	// delayedSendingStuckThreshold 悬挂观测阈值：正常一轮派发在秒级收敛，send_at 超期
	// 1h 仍停在 sending 说明进程在投递中途崩溃。仅打 warn 供运维核对，不回收、不改状态
	// （回收语义属产品决策）。
	delayedSendingStuckThreshold = time.Hour
)

// DelayedOutboundReply 别名（模型已收敛到 model 层，表 reach_delayed_outbound）
type DelayedOutboundReply = model.DelayedOutboundReply

// aiReplyQuietHoursStart / aiReplyQuietHoursEnd 每请求读参数中心（60s TTL 缓存），
// 越界（<0 或 >23）回落代码默认。读取点形状：GetInt( 与 group/key 字面量同一行，
// 满足 check-config-param-readpoints.py 硬闸。scope=全局（config_params 无租户维）。
// 与 reach.quiet_hours_*（触达管线）是两条不同管道，各一组键，禁止合并。
func aiReplyQuietHoursStart(ctx context.Context) int {
	v := GlobalConfigParam().GetInt(ctx, "webhook", "ai_quiet_hours_start", aiReplyQuietStartHour)
	if v < 0 || v > 23 {
		return aiReplyQuietStartHour
	}
	return v
}

func aiReplyQuietHoursEnd(ctx context.Context) int {
	v := GlobalConfigParam().GetInt(ctx, "webhook", "ai_quiet_hours_end", aiReplyQuietEndHour)
	if v < 0 || v > 23 {
		return aiReplyQuietEndHour
	}
	return v
}

// WhatsApp 回退模板（B7，2026-11-17）：WHATSAPP_FALLBACK_TEMPLATE(_LANG) 环境变量退役，
// 入参数中心 channelbot 组（24h 客服窗口外的降级模板名与语言）。
func whatsappFallbackTemplate(ctx context.Context) string {
	return GlobalConfigParam().GetString(ctx, "channelbot", "whatsapp_fallback_template", "")
}

func whatsappFallbackTemplateLang(ctx context.Context) string {
	return GlobalConfigParam().GetString(ctx, "channelbot", "whatsapp_fallback_template_lang", "")
}

// AIAQuietHoursDisabledParam 是「AI 回复勿扰总开关」的参数键（agent_llm 组）。
// SaaS 配置面改造（B8，2026-11-17）：DISABLE_AI_QUIET_HOURS 环境变量退役。
const AIAQuietHoursDisabledParam = "ai_quiet_hours_disabled"

func aiQuietHoursDisabled(ctx context.Context) bool {
	return GlobalConfigParam().GetBool(ctx, "agent_llm", AIAQuietHoursDisabledParam, false)
}

// DisableAIQuietHoursForTest 测试装配口，替代已退役的 DISABLE_AI_QUIET_HOURS 环境变量
// （仓内 9 处测试曾用它把勿扰窗顶掉）。与 SetGlobalForTest 同一先例：helper 放产码
// 文件（不 import testing），跨包测试才能装——channelgw / controller 的测试都以
// `service.DisableAIQuietHoursForTest` 调它，故不能像只被本包测试调用的那 14 扇
// setter 一样搬进 _test.go（搬进去外部包的测试编译期就看不见了）。它是**导出**符号，
// golangci `unused`（run.tests:false）只报未导出死代码，留在产码文件不会红。
//
// 实现走 aiReplyQuietHoursFn 替换缝而非全局配置桩：配置桩按「无租户前缀」塞缓存键，
// 而测试（channelgw CS-06 等）跑在 tenant.ModeOn + 租户 ctx 下，getString 查的是
// `tenant:<id>:agent_llm.…`，桩键必然 miss——勿扰窗照样激活，测试在 23:00–07:00
// 窗口内必红（2026-11-17 024 B1 收口实测）。判定缝是全租户无关的单点，替换它
// 对两种租户形态都成立。
//
// 体内一律经 storeAIReplyQuietHoursFn / loadAIReplyQuietHoursFn——本函数不得直摸
// 全局（check-seam-guard.py 判据 3：accessor 之外 0 处直引）。2026-11-19 二次审核收口：
// 此前直摸 4 处，是并行会话"把函数搬进 _test.go"未遂方案的根因（方向对、落点错——
// 正解是写侧 accessor 随调用链进产码，见 storeAIReplyQuietHoursFn 注释）。
func DisableAIQuietHoursForTest(disabled bool) func() {
	orig := loadAIReplyQuietHoursFn()
	if disabled {
		storeAIReplyQuietHoursFn(func(time.Time) bool { return false })
	} else {
		storeAIReplyQuietHoursFn(isAIReplyQuietHours)
	}
	return func() {
		storeAIReplyQuietHoursFn(orig)
	}
}

func isAIReplyQuietHours(t time.Time) bool {
	if aiQuietHoursDisabled(context.Background()) {
		return false
	}
	// 判定在异步排水/重投链上被调用，无请求 ctx 可用，走 context.Background()。
	// 参数中心 60s TTL 缓存，故不会每条消息打库。
	// AI 回复免打扰维持 cstZone（J-12 分层：客户时区只进触达管线静默窗口；
	// 本函数无客户档案上下文，属会话域节奏控制）。
	return inQuietHoursWindow(t, aiReplyQuietHoursStart(context.Background()), aiReplyQuietHoursEnd(context.Background()), cstZone)
}

// aiReplyQuietHoursFn 免打扰判定的可换点（测试装替身，生产走 isAIReplyQuietHours）。
//
// 读写只走下面那对 accessor（aiReplyQuietHoursMu 守，包内其余位置直读 0 处）：它被两条
// **异步链**读到 —— 延后出站排水循环在协程里经 nextSendRetryAt 与 sendOutbound 各读一次，
// 而入站用例排队重投时会换掉它。var 本身在协程前快照挡不住：快照冻的是本地变量，
// 函数体里那一句读的仍是同一个全局地址。
var (
	aiReplyQuietHoursMu sync.RWMutex

	aiReplyQuietHoursFn = isAIReplyQuietHours
)

func loadAIReplyQuietHoursFn() func(time.Time) bool {
	aiReplyQuietHoursMu.RLock()
	defer aiReplyQuietHoursMu.RUnlock()
	return aiReplyQuietHoursFn
}

// storeAIReplyQuietHoursFn 写侧 accessor。按本仓 seam 边界规则（见
// scripts/check-seam-guard.py 文件头口径边界）"写侧同时被产码调用才留在本家文件"——
// DisableAIQuietHoursForTest（导出测试缝，产码文件）是它的产码调用方，故它随迁至此，
// 不再住 seam_guard_setters_test.go（注册表 setter 列已同步改回本家形态）。
// 竞态不变量不变：同一全局的每一次读写都被 aiReplyQuietHoursMu synchronize。
func storeAIReplyQuietHoursFn(fn func(time.Time) bool) {
	aiReplyQuietHoursMu.Lock()
	defer aiReplyQuietHoursMu.Unlock()
	aiReplyQuietHoursFn = fn
}

type delayedReplayCtxKey struct{}

// DelayedReplayToContext 标记 ctx 为延迟队列重放路径
func DelayedReplayToContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, delayedReplayCtxKey{}, true)
}

func isDelayedReplay(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, ok := ctx.Value(delayedReplayCtxKey{}).(bool)
	return ok && v
}

func init() { _db.RegisterExtraModels(&DelayedOutboundReply{}) }

// =============================================================================
// B5 单一 sender 能力位（CH-37b刀3 批准决议）：AI 出站桥接分支的渠道集合 =
// channelgw registry OutboundPath=bridge_outbox 能力位的运行时投影。
//
// sendOutbound 的桥接分支此前在 case 里硬编码九个渠道常量，与 channelgw registry
// 各维护一份清单，新增渠道要改两处。现在集合由注册表 init 装配
// （channelgw/registry.go：service.SetBridgeOutboxChannels(Default.OutboundChannels(...))），
// 分支只做能力查询（bridgeOutboxCapable）：新渠道只登记 registry 即自动获得出站路径。
// 集合恒等（双向：registry 有能力但出站不认 = 红；出站认但 registry 无能力 = 红）
// 由 channelgw/registry_parity_test.go TestBridgeOutboxParityWithWebhookOutbound 钉住。
//
// Sender 契约（B5）：桥接出站行 SenderID ≡ accountID——AI 路径（本文件桥接分支）
// 与人工/触达路径（bridge_outbound.go DeliverBridgeOutbound）一致，重试/重放不换 sender，
// 见 TestSendOutbound_Bridge_SenderIDEqualsAccountID / TestDeliverBridgeOutbound_SenderIDEqualsAccountID。
// =============================================================================

var (
	bridgeOutboxMu       sync.RWMutex
	bridgeOutboxChannels = map[string]struct{}{}
)

// SetBridgeOutboxChannels 用 registry OutboundPath=bridge_outbox 的渠道名集合重建
// AI 出站桥接分支的判定集（由 channelgw/registry.go init 装配，运行时单一来源）。
// 与 bridge/channel.go 对人工/触达白名单的 SetBridgeChannels 同一装配模式。
// 空注入忽略（防误清空：装配时序异常时宁可维持上一份集合，也不悄悄断掉 AI 出站）。
func SetBridgeOutboxChannels(names []string) {
	if len(names) == 0 {
		logger.Errorf("[B-5] SetBridgeOutboxChannels called with empty names; keep previous set")
		return
	}
	m := make(map[string]struct{}, len(names))
	for _, n := range names {
		if n == "" {
			continue
		}
		m[n] = struct{}{}
	}
	if len(m) == 0 {
		return
	}
	bridgeOutboxMu.Lock()
	defer bridgeOutboxMu.Unlock()
	bridgeOutboxChannels = m
}

// BridgeOutboxChannels 返回 AI 出站桥接分支当前认定的渠道集合（排序副本）。
// parity 断言/诊断读取口；判定本体走 bridgeOutboxCapable。
func BridgeOutboxChannels() []string {
	bridgeOutboxMu.RLock()
	defer bridgeOutboxMu.RUnlock()
	names := make([]string, 0, len(bridgeOutboxChannels))
	for n := range bridgeOutboxChannels {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// bridgeOutboxCapable 该渠道的 AI 出站是否走桥接出库队列（registry 能力查询）。
// webhook 直发渠道与未注册渠道一律 false（走 default 的不支持分支，路径不变）。
func bridgeOutboxCapable(channel WebhookChannel) bool {
	bridgeOutboxMu.RLock()
	defer bridgeOutboxMu.RUnlock()
	_, ok := bridgeOutboxChannels[string(channel)]
	return ok
}

func (s *WebhookService) enqueueDelayedOutbound(ctx context.Context, channel WebhookChannel, accountID string, p *ParsedPayload, content string, hubMsg *model.MessageHub, cards []model.RichCard) bool {
	if s.delayedRepo == nil {
		logger.Ctx(ctx).Warn().Str("channel", string(channel)).Msg("[H-3] db 未初始化，quiet hours 延迟入队失败，按原路径直接发送")
		return false
	}
	rec := &DelayedOutboundReply{
		Platform:       string(channel),
		AccountID:      accountID,
		SenderID:       p.Sender,
		Content:        content,
		Cards:          delayedCardsPayload(cards),
		ConversationID: convIDOrEmpty(hubMsg),
		SendAt:         nextQuietHoursRelease(time.Now(), aiReplyQuietHoursEnd(ctx), cstZone),
		Status:         model.DelayedStatusPending,
		Kind:           model.DelayedKindQuietHours,
	}
	if err := s.delayedRepo.CreatePending(ctx, rec); err != nil {
		logger.Ctx(ctx).Error().Err(err).Str("channel", string(channel)).Msg("[H-3] 延迟入队失败，按原路径直接发送")
		return false
	}
	logger.Ctx(ctx).Info().
		Str("channel", string(channel)).
		Str("conv_id", rec.ConversationID).
		Time("send_at", rec.SendAt).
		Msg("[H-3] AI 回复命中 quiet hours(23:00-7:00)，进入延迟队列次日首发")
	s.startDelayedOutboundDispatch()
	return true
}

// delayedCardsPayload 富卡片随延迟记录一起落库，重放时原样下发。
func delayedCardsPayload(cards []model.RichCard) model.JSONMap {
	if len(cards) == 0 {
		return nil
	}
	raw, err := json.Marshal(cards)
	if err != nil {
		return nil
	}
	return model.JSONMap{"cards": json.RawMessage(raw)}
}

const (
	// sendRetryMaxAttempts 一条回复进入持久化重试队列后的最大重投次数。
	// 与渠道客户端内部的 3 次退避不叠加计算：那一层只覆盖分钟级抖动，
	// 这一层覆盖"进程还在、通道长时间不通"，次数再多也只是把过期内容投给客户。
	sendRetryMaxAttempts = 3
)

// sendRetryBackoffs 第 n 次失败后的等待时长，超出表长按最后一档封顶。
var sendRetryBackoffs = []time.Duration{
	60 * time.Second,
	2 * time.Minute,
	4 * time.Minute,
}

// nextSendRetryAt 计算第 attempts 次失败后的重投时刻：渠道等待值与退避表取大（只顺延不提前），
// 再避开免打扰时段。
//
// 渠道开了口让等 5 分钟时按表 60s 重投，等于在冷却期里再撞一次封禁（N-11④）；
// 反过来渠道只说等 30s 时也不能比表更早，否则等于把这条回复的投递时序交给一句空话。
func nextSendRetryAt(now time.Time, attempts int, ce *ChannelError) time.Time {
	idx := attempts
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sendRetryBackoffs) {
		idx = len(sendRetryBackoffs) - 1
	}
	delay := sendRetryBackoffs[idx]
	if ce != nil && ce.RetryAfter > delay {
		delay = ce.RetryAfter
	}
	at := now.Add(delay)
	// 免打扰时段内重投等于在深夜打扰客户，和首发同规则顺延到窗口开放。
	// 取一次再调用：不在持锁期间跑可换出去的策略（见 loadAIReplyQuietHoursFn）。
	if loadAIReplyQuietHoursFn()(at) {
		at = nextQuietHoursRelease(at, aiReplyQuietHoursEnd(context.Background()), cstZone)
	}
	return at
}

// enqueueSendRetry 实时投递失败后把这条已生成的回复转入持久化重试队列。
//
// 为什么当场重试不够：断网/通道故障常持续数分钟到更久，进程内重试（渠道客户端的 3 次
// 退避）用完就彻底放弃，而客户侧最后一条仍是入站行 —— 只能等客户自己再问一次。
// 落库后即使服务重启，30s 轮询也会把到期记录投出去。
func (s *WebhookService) enqueueSendRetry(ctx context.Context, channel WebhookChannel, accountID string, p *ParsedPayload, content string, hubMsg *model.MessageHub, cards []model.RichCard, sendErr error) {
	if s.delayedRepo == nil {
		return
	}
	ce := AsChannelError(sendErr)
	if ce == nil || !ce.Retryable {
		return
	}
	now := time.Now()
	rec := &DelayedOutboundReply{
		Platform:       string(channel),
		AccountID:      accountID,
		SenderID:       p.Sender,
		Content:        content,
		Cards:          delayedCardsPayload(cards),
		ConversationID: convIDOrEmpty(hubMsg),
		SendAt:         nextSendRetryAt(now, 0, ce),
		Status:         model.DelayedStatusPending,
		Kind:           model.DelayedKindSendRetry,
		LastError:      ce.Raw,
	}
	if err := s.delayedRepo.CreatePending(ctx, rec); err != nil {
		logger.Ctx(ctx).Error().Err(err).Str("channel", string(channel)).
			Msg("[H-3] 投递失败重试入队失败，本条回复仅保留失败轨迹")
		return
	}
	logger.Ctx(ctx).Warn().
		Str("channel", string(channel)).
		Str("conv_id", rec.ConversationID).
		Uint("id", rec.ID).
		Time("send_at", rec.SendAt).
		Msg("[H-3] AI 回复投递失败（可重试），已进入持久化重试队列")
	s.startDelayedOutboundDispatch()
}

var delayedDispatchStop chan struct{}

func (s *WebhookService) startDelayedOutboundDispatch() {
	// 先把库里已到期的回复立刻投一轮，再挂 ticker。否则服务重启后到期项要等到
	// "下一次有人入队"才被顺带启动的消费者取走。抢占式 pending→sending
	// 保证这一轮与 ticker、与其他实例之间不会重复投递。
	s.dispatchDueDelayedOutbound(context.Background())

	dispatchOnce.Do(func() {
		delayedDispatchStop = make(chan struct{})

		utils.SafeGo(context.Background(), "webhook_outbound.delayed_dispatch", func(ctx context.Context) {
			ticker := time.NewTicker(delayedOutboundPollInterval)
			defer ticker.Stop()
			for {
				select {
				case <-delayedDispatchStop:
					return
				case <-ticker.C:
					s.dispatchDueDelayedOutbound(ctx)
				}
			}
		})
	})
}

var dispatchOnce sync.Once

func (s *WebhookService) dispatchDueDelayedOutbound(ctx context.Context) {
	if s.delayedRepo == nil {
		return
	}
	// 平台级后台派发循环：reach_delayed_outbound 是租户表，TENANT_MODE=on 时无租户
	// ctx 的 ExpireStale（update）、CountStuckSending（query）、PickDueForUpdate/
	// PluckDueIDs/MarkSendingIfPending 与 replayDelayedOutbound 的落库都会被隔离
	// callback fail-closed（生产日志实证："[H-3] 延迟出站 TTL 判过期失败，本轮跳过判期但
	// 不阻断重放: [tenant] 表 reach_delayed_outbound 需要租户上下文但 ctx 未绑定（update）"、
	// "sending 悬挂行观测查询失败: ...（query）"）。消费者由 context.Background() 启动
	// （startDelayedOutboundDispatch），没有 HTTP 请求 ctx 可绑，走 tenant.WithSystemScope
	// 这条平台级豁免通道；不放宽任何带请求 ctx 的路径（本函数无 HTTP 调用方）。
	ctx = tenant.WithSystemScope(ctx)
	now := time.Now()
	if n, err := s.delayedRepo.ExpireStale(ctx, now.Add(-delayedOutboundTTL)); err != nil {
		logger.Ctx(ctx).Warn().Err(err).Msg("[H-3] 延迟出站 TTL 判过期失败，本轮跳过判期但不阻断重放")
	} else if n > 0 {
		logger.Ctx(ctx).Info().Int64("expired", n).Msg("[H-3] 超 TTL 仍 pending 的延迟出站已判 expired，跳过补投")
	}
	if n, err := s.delayedRepo.CountStuckSending(ctx, now.Add(-delayedSendingStuckThreshold)); err != nil {
		logger.Ctx(ctx).Warn().Err(err).Msg("[H-3] sending 悬挂行观测查询失败，忽略（不影响派发）")
	} else if n > 0 {
		logger.Ctx(ctx).Warn().Int64("stuck_sending", n).
			Msg("[H-3] 存在超阈值仍为 sending 的延迟出站行：疑似投递中进程崩溃，需人工核对该会话是否已送达（本日志只观测，未回收）")
	}
	picked, err := s.delayedRepo.PickDueForUpdate(ctx, now, delayedOutboundBatchSize)
	if err != nil {

		picked = nil
		ids, err2 := s.delayedRepo.PluckDueIDs(ctx, now, delayedOutboundBatchSize)
		if err2 != nil || len(ids) == 0 {
			return
		}
		ok, err2 := s.delayedRepo.MarkSendingIfPending(ctx, ids)
		if err2 != nil || !ok {
			return
		}
		picked, err2 = s.delayedRepo.ClaimSendingRows(ctx, ids, now)
		if err2 != nil || len(picked) == 0 {
			return
		}
	}
	for i := range picked {
		s.replayDelayedOutbound(ctx, &picked[i])
	}
	// 租约/悬挂回收（D03）：sending 不再是"只观测不处理"的永久悬挂态。
	// 历史无租约行与租约过期行在此判定：会话已被回复 → superseded；
	// 无法确认外部结果 → unknown 待人工核对。绝不自动重发（可能双发）。
	s.recoverStuckDelayedOutbound(ctx, now)
}

// recoverStuckDelayedOutbound 回收两类 sending 悬挂行（D03）：
//   - legacy：无租约字段的历史行（send_at 超阈值）——无法判断是否已投出，直接 unknown；
//   - 租约过期行：先查会话是否已被回复（superseded 可安全判定"不需要再投"），
//     否则 unknown。所有转移都带原因入 last_error，人工可经 ResolveUnknown 收敛。
func (s *WebhookService) recoverStuckDelayedOutbound(ctx context.Context, now time.Time) {
	legacy, err := s.delayedRepo.ListLegacyStuckSending(ctx, now.Add(-delayedSendingStuckThreshold), delayedOutboundBatchSize)
	if err != nil {
		logger.Ctx(ctx).Warn().Err(err).Msg("[H-3] legacy sending 悬挂行查询失败，忽略本轮回收")
		legacy = nil
	}
	for i := range legacy {
		if err := s.delayedRepo.MarkSendingUnknown(ctx, legacy[i].ID, "", "历史 sending 悬挂（无租约字段），核对前不重发"); err != nil {
			logger.Ctx(ctx).Warn().Err(err).Uint("id", legacy[i].ID).Msg("[H-3] legacy 悬挂行转 unknown 失败")
			continue
		}
		logger.Ctx(ctx).Warn().Uint("id", legacy[i].ID).Str("conv_id", legacy[i].ConversationID).
			Msg("[H-3] 历史 sending 悬挂行已转 unknown 待人工核对（legacy_unknown，不自动重发）")
	}

	expired, err := s.delayedRepo.ListExpiredLeaseSending(ctx, now, delayedOutboundBatchSize)
	if err != nil {
		logger.Ctx(ctx).Warn().Err(err).Msg("[H-3] 租约过期 sending 行查询失败，忽略本轮回收")
		return
	}
	for i := range expired {
		rec := &expired[i]
		if s.messageHubRepo != nil && rec.ConversationID != "" {
			unreplied, _, err := s.messageHubRepo.HasUnrepliedCustomerMessage(ctx, rec.ConversationID, InboxReplyWindow)
			if err == nil && !unreplied {
				// 会话已被回复：这条内容无论如何都不该再投（superseded 是安全终态，
				// 即便外部其实发成功过，也只是"已收到且已被回复"，无双发风险）。
				if err := s.delayedRepo.MarkSendingUnknown(ctx, rec.ID, rec.ClaimToken, "租约过期且会话已被回复"); err != nil {
					logger.Ctx(ctx).Warn().Err(err).Uint("id", rec.ID).Msg("[H-3] 过期租约行转 unknown 失败")
					continue
				}
				if err := s.delayedRepo.ResolveUnknown(ctx, rec.ID, model.DelayedStatusSuperseded, now, "租约过期且会话已被回复，自动判定不再补投"); err != nil {
					logger.Ctx(ctx).Warn().Err(err).Uint("id", rec.ID).Msg("[H-3] 过期租约行自动改判 superseded 失败")
					continue
				}
				logger.Ctx(ctx).Info().Uint("id", rec.ID).Str("conv_id", rec.ConversationID).
					Msg("[H-3] 租约过期的延迟出站因会话已被回复改判 superseded")
				continue
			}
		}
		if err := s.delayedRepo.MarkSendingUnknown(ctx, rec.ID, rec.ClaimToken, "投递租约过期，外部发送结果未知"); err != nil {
			logger.Ctx(ctx).Warn().Err(err).Uint("id", rec.ID).Msg("[H-3] 租约过期行转 unknown 失败")
			continue
		}
		logger.Ctx(ctx).Warn().Uint("id", rec.ID).Str("conv_id", rec.ConversationID).
			Msg("[H-3] 投递租约过期的延迟出站已转 unknown 待人工核对（不自动重发，防双发）")
	}
}

// resolveDelayedReplayTenant 统一延迟重放租户仲裁：反查错误/歧义立即返回，绝不回退行值；
// 只有反查未命中 (0, nil) 才允许使用 rec.TenantID。
func (s *WebhookService) resolveDelayedReplayTenant(ctx context.Context, rec *DelayedOutboundReply) (context.Context, error) {
	if tid, ok := tenant.FromContext(ctx); ok && tid != 0 {
		return ctx, nil
	}
	if s == nil || s.bridgeAccountTenantRepo == nil {
		return ctx, fmt.Errorf("bridge account tenant repository unavailable")
	}
	resolved, err := s.bridgeAccountTenantRepo.ResolveTenantID(ctx, string(rec.Platform), rec.AccountID)
	if err != nil {
		return ctx, fmt.Errorf("resolve replay tenant: %w", err)
	}
	if resolved != 0 {
		return tenant.OnlyTenant(ctx, resolved), nil
	}
	if rec.TenantID != 0 {
		return tenant.OnlyTenant(ctx, rec.TenantID), nil
	}
	return ctx, fmt.Errorf("replay tenant unresolved")
}

func (s *WebhookService) replayDelayedOutbound(ctx context.Context, rec *DelayedOutboundReply) {
	if s == nil || s.delayedRepo == nil || ctx == nil || ctx.Err() != nil || rec == nil {
		return
	}
	// 租户反查是权威；运行错误/歧义立即 abandon，不能借 rec.TenantID 掩盖反查故障。
	ctx, err := s.resolveDelayedReplayTenant(ctx, rec)
	if err != nil {
		s.abandonReplay(ctx, rec, err.Error())
		return
	}
	current, err := s.delayedRepo.ConfirmReplayLease(ctx, rec, time.Now())
	if err != nil || current == nil {
		logger.Ctx(ctx).Warn().Uint("id", rec.ID).Msg("[H-3] 延迟重放租约或身份未确认，未发起投递")
		return
	}
	rec = current
	channel := WebhookChannel(rec.Platform)
	now := time.Now()
	isRetry := rec.Kind == model.DelayedKindSendRetry

	if isRetry && rec.Attempts >= sendRetryMaxAttempts {
		s.abandonReplay(ctx, rec, fmt.Sprintf("重投次数用尽（%d 次）", rec.Attempts))
		return
	}
	_, answered, permissionErr := newDelayedReplayPermission(s.delayedRepo, s.ingressSvc).confirm(ctx, rec)
	if permissionErr != nil {
		logger.Ctx(ctx).Warn().Uint("id", rec.ID).Msg("[H-3] 延迟回复会话或人工权限未确认，未发起投递")
		return
	}
	if answered {
		if ferr := s.delayedRepo.FinishReplay(ctx, rec.ID, rec.ClaimToken, model.DelayedStatusSuperseded, time.Now(), ""); ferr != nil {
			logger.Ctx(ctx).Warn().Uint("id", rec.ID).Msg("[H-3] 旧答案作废回写未完成")
		}
		return
	}

	hubMsg := &model.MessageHub{
		ConversationID: rec.ConversationID,
		SenderID:       rec.SenderID,
		SentAt:         rec.CreatedAt,
	}
	p := &ParsedPayload{EventID: fmt.Sprintf("delayed-%d", rec.ID), Sender: rec.SenderID}

	var cards []model.RichCard
	if rec.Cards != nil {
		if raw, ok := rec.Cards["cards"]; ok {
			if data, err := json.Marshal(raw); err == nil {
				_ = json.Unmarshal(data, &cards)
			}
		}
	}

	sent, sendErr := s.sendOutbound(DelayedReplayToContext(ctx), channel, rec.AccountID, p, rec.Content, hubMsg, cards)

	switch {
	case sent:
		// 只有确实交给下游才按 sent 收口；这仍不代表平台已收件。
		// 回写带 claim_token 栅栏：租约过期被重认领后，旧 worker 的回写 0 行拒绝。
		if err := s.delayedRepo.MarkSent(ctx, rec.ID, rec.ClaimToken, time.Now()); err != nil {
			logger.Ctx(ctx).Warn().Err(err).Uint("id", rec.ID).Msg("[H-3] 延迟回复状态回写失败")
		}
	case !isRetry:
		// 免打扰首发仍只尝试一次。失败不能伪报成功，也不补触发新的回答或自动重投。
		if sendErr == nil {
			if err := s.delayedRepo.MarkSendingUnknown(ctx, rec.ID, rec.ClaimToken, "首发未取得成功交接事实"); err != nil {
				logger.Ctx(ctx).Warn().Err(err).Uint("id", rec.ID).Msg("[H-3] 首发未知结果回写失败")
			}
			return
		}
		if err := s.delayedRepo.FinishReplay(ctx, rec.ID, rec.ClaimToken, model.DelayedStatusFailed, now, sendErr.Error()); err != nil {
			logger.Ctx(ctx).Warn().Err(err).Uint("id", rec.ID).Msg("[H-3] 首发失败结果回写失败")
		}
	default:
		ce := AsChannelError(sendErr)
		if ce != nil && ce.Retryable {
			attempts := rec.Attempts + 1
			if err := s.delayedRepo.ScheduleRetry(ctx, rec.ID, rec.ClaimToken, nextSendRetryAt(now, attempts, ce), ce.Raw); err != nil {
				logger.Ctx(ctx).Warn().Err(err).Uint("id", rec.ID).Msg("[H-3] 重投失败改排下一次异常")
			}
			logger.Ctx(ctx).Warn().Uint("id", rec.ID).Int("attempts", attempts).
				Msg("[H-3] 延迟出站重投仍失败，已按退避改排")
			return
		}
		reason := "投递未成功且无渠道错误"
		if ce != nil {
			reason = ce.Raw
		}
		s.abandonReplay(ctx, rec, reason)
	}
}

// abandonReplay 把重放行收口为终态 failed，并给这条客户消息留最后一次补触发机会：
// 重试通道放弃后，会话里仍是未回复的入站行，不能就此没人应答。
func (s *WebhookService) abandonReplay(ctx context.Context, rec *DelayedOutboundReply, reason string) {
	if err := s.delayedRepo.FinishReplay(ctx, rec.ID, rec.ClaimToken, model.DelayedStatusFailed, time.Now(), reason); err != nil {
		logger.Ctx(ctx).Warn().Err(err).Uint("id", rec.ID).Msg("[H-3] 重放终态回写失败")
	}
	logger.Ctx(ctx).Error().Uint("id", rec.ID).Str("conv_id", rec.ConversationID).
		Str("reason", reason).Msg("[H-3] 延迟出站放弃重投，改判 failed")
	if s.ingressSvc == nil || rec.ConversationID == "" {
		return
	}
	s.ingressSvc.RecheckUnrepliedAndTrigger(context.WithoutCancel(ctx), rec.ConversationID, "")
}

// sendOutbound 投递一条 AI 回复。
//
// 返回值是投递事实：sent 表示至少有一跳成功；sendErr 是"这一条回复没能交出去"的原因，
// 前置不满足（缺 sessionWebhook、窗口过期、账号 id 非法、域名非法、渠道不支持）也要构造
// 一个不可重试的 *ChannelError 带回去 —— 返回 (false, nil) 等于向调用方谎报"没有发生失败"：
// 重放 worker 只能记一句"无渠道错误"，失败轨迹与类别就此丢失。
// 调用方据 sendErr 决定是否进入持久化重试通道（只重试 Retryable 的那批）。
func (s *WebhookService) sendOutbound(ctx context.Context, channel WebhookChannel, accountID string, p *ParsedPayload, content string, hubMsg *model.MessageHub, cards []model.RichCard) (sent bool, sendErr error) {
	tenantID, _ := tenant.FromContext(ctx)
	if hubMsg != nil && hubMsg.TenantID != 0 {
		tenantID = hubMsg.TenantID
	}
	resolvedAccountID, resolveErr := resolveOutboundAccount(ctx, tenantID, channel, accountID)
	if resolveErr != nil {
		sendErr = preSendFailure(channel, CategoryBadRequest, resolveErr.Error())
		recordOutboundSendFailed(channel, sendErr)
		return false, sendErr
	}
	accountID = resolvedAccountID
	if err := authorizeOutboundSubmission(ctx, channel, accountID, hubMsg); err != nil {
		recordOutboundSendFailed(channel, err)
		return false, err
	}

	if !isDelayedReplay(ctx) && loadAIReplyQuietHoursFn()(time.Now()) {
		if s.enqueueDelayedOutbound(ctx, channel, accountID, p, content, hubMsg, cards) {
			return false, nil
		}
	}

	if !agent_runtime.ClaimReply(p.EventID) {
		logger.Ctx(ctx).Info().Str("event_id", p.EventID).Msg("skip duplicate outbound (event already replied)")

		return false, nil
	}

	ctx, cancel := context.WithTimeout(ctx, utils.MediumTimeout)
	defer func() {
		if !sent {
			agent_runtime.ReleaseReply(p.EventID)
		}
		cancel()
	}()

	// markSendFailed 统一失败口径：先记录真实错误供收尾判断是否入重试队列，
	// 再走原有 outboundSendFailed（写终态失败轨迹 / 发布授权告警）。
	markSendFailed := func(err error) {
		sendErr = err
		recordOutboundSendFailed(channel, err) // specs/024 T1205：失败打点旁路，不改口径
		s.outboundSendFailed(ctx, channel, accountID, hubMsg, content, err)
	}

	// markSendFailedLogged 用于"这一支没有可归属的轨迹行"（渠道不在出站分支表里 ⇒
	// 平台词表必然校验不过）：只把事实说清楚并留结构化日志，不尝试写库。
	markSendFailedLogged := func(err error) {
		sendErr = err
		recordOutboundSendFailed(channel, err) // specs/024 T1205：失败打点旁路，不改口径
		s.outboundSendFailed(ctx, channel, accountID, nil, content, err)
	}

	// 收尾（defer 保证各渠道的提前 return 分支同样覆盖）：
	//  1. 可重试的投递失败 → 同步落一条 send_retry 到延迟出站队列，到期重投同一份内容；
	//     必须先入队再放行 recheck，否则补触发看不到这条待投记录，会另生成一份回复。
	//  2. 释放 AI 处理中标记；实时路径补一次「未回复」检查。
	//     重放路径不做补触发：这条记录本身就是待投回复，重投结果由 replayDelayedOutbound 收口。
	defer func() {
		if sendErr != nil && !sent && !isDelayedReplay(ctx) {
			s.enqueueSendRetry(ctx, channel, accountID, p, content, hubMsg, cards, sendErr)
		}
		if s.ingressSvc == nil || hubMsg == nil || hubMsg.ConversationID == "" {
			return
		}
		s.ingressSvc.ReleaseAIProcessingFlag(ctx, hubMsg.ConversationID)
		if isDelayedReplay(ctx) {
			return
		}
		// SafeGo：出站消息链上的补触发 panic 不能击穿进程
		utils.SafeGo(ctx, "outbound.recheck_unreplied", func(_ context.Context) {
			s.ingressSvc.RecheckUnrepliedAndTrigger(context.WithoutCancel(ctx), hubMsg.ConversationID, "")
		})
	}()
	// 出站渲染只做一次（specs/024 T402/T404）：格式转换 + 富卡降级 + 溢出计数在这里收口，
	// 之后 sender 拿到的 content 已是目标格式，不得再次转义/清洗。
	// 放在分派之前的原因（R5/CH-37b 刀3）：defer 里的 enqueueSendRetry 捕获同一个 content 变量，
	// 清洗必须已生效，重试才会投递与首投一致的内容。
	originalCardCount := len(cards)
	rendered, renderErr := RenderForChannel(string(channel), model.MsgTypeText, content, cards)
	if renderErr != nil {
		markSendFailed(preSendFailure(channel, CategoryBadRequest, renderErr.Error()))
		return
	}
	content = rendered.Text
	cards = rendered.Cards
	cardsFallbackCount := 0
	if originalCardCount > 0 && len(cards) == 0 {
		cardsFallbackCount = originalCardCount
		fallback := logger.Ctx(ctx).Info().
			Str("module", "outbound").
			Str("channel", string(channel)).
			Str("account_id", accountID).
			Int("cards_fallback_count", cardsFallbackCount)
		if hubMsg != nil {
			fallback = fallback.Str("conversation_id", hubMsg.ConversationID)
		}
		fallback.Msg("outbound rich cards rendered as complete text")
	}
	if rendered.Overflow > 0 {
		// 只上报不切分：切分归渠道自己（QQ 在 channelbot/qq 内按官方上限切）。
		outboundTextOverflowTotal.WithLabel(string(channel)).Inc() // specs/024 T1205
		logger.Ctx(ctx).Warn().
			Str("module", "outbound").
			Str("channel", string(channel)).
			Str("account_id", accountID).
			Int("overflow_chars", rendered.Overflow).
			Msg("outbound_text_overflow")
	}

	sender, ok := s.outboundSenderFor(channel)
	if !ok {
		logger.Ctx(ctx).Warn().Str("channel", string(channel)).Str("account_id", accountID).Msg("unsupported outbound channel, skipped")
		markSendFailedLogged(preSendFailure(channel, CategoryBadRequest,
			fmt.Sprintf("出站不支持该渠道(%s)：webhookInboundCapable 与 sendOutbound 分支表已漂移", channel)))
		return
	}
	res := sender.Send(ctx, &OutboundRequest{
		Channel:            channel,
		AccountID:          accountID,
		Payload:            p,
		Content:            content,
		Cards:              cards,
		Hub:                hubMsg,
		CardsFallbackCount: cardsFallbackCount,
		Fail:               markSendFailed,
		FailUntracked:      markSendFailedLogged,
	})
	if res != nil {
		sent = res.Sent
	}
	if !sent && sendErr == nil {
		// R5 不变量（specs/024 T101 二次审核补）：sender 必须显式表态——
		// 既没发成功也没调 Fail/FailUntracked 就返回，失败口径就会漏
		//（重试不入队、审计不落行，调用方拿到「没发也没错」）。
		// 兜成可重试的 unknown 失败：宁可多一次重试，也不能静默丢回复。
		markSendFailedLogged(preSendFailure(channel, CategoryUnknown,
			"sender reported neither success nor failure"))
	}
	return
}

func isBridgeChannelUndeliverableLocal(accountID, conversationID string) (bool, string) {
	return bridgeOutboundUndeliverable(accountID, conversationID)
}

type handleResultCtxKey struct{}

func convIDOrEmpty(h *model.MessageHub) string {
	if h == nil {
		return ""
	}
	return h.ConversationID
}

// preSendFailure 构造"请求根本没发出去"这一类的渠道错误：这类失败由中台侧的前置条件决定，
// 重投同一份内容必然同样失败，所以一律不可重试。
//
// Category 必须显式给且不能是 CategoryUnknown：AsChannelError 对已判定类别的错误原样放行，
// 对未判定类别的会拿 Raw 文案重新判档并连带改写 Retryable —— 手工设定的"不可重试"
// 就会在这一点上被文案左右（文案换个说法，重试行为就变了）。
func preSendFailure(channel WebhookChannel, category ChannelErrorCategory, reason string) *ChannelError {
	return &ChannelError{Channel: string(channel), Category: category, Retryable: false, Raw: reason}
}

// dingtalkWebhookHostAllowed sessionWebhook 域名白名单。读写只走下面那对 accessor
// （dingtalkHostMu 守，包内其余位置直读 0 处）：sendOutbound 在延后出站的协程里读它，
// 而它同时是入站用例换主机名时的靶子 —— 同 aiReplyQuietHoursFn 那一类第二跳竞态。
var (
	dingtalkHostMu sync.RWMutex

	dingtalkWebhookHostAllowed = func(u *url.URL) bool {
		return u != nil && (u.Host == "oapi.dingtalk.com" || strings.HasSuffix(u.Host, ".dingtalk.com"))
	}
)

func loadDingtalkWebhookHostAllowed() func(*url.URL) bool {
	dingtalkHostMu.RLock()
	defer dingtalkHostMu.RUnlock()
	return dingtalkWebhookHostAllowed
}

// HandleResultToContext 把 HandleResult 注入 ctx，供 sendOutbound 取出补字段。
func HandleResultToContext(ctx context.Context, r *HandleResult) context.Context {
	if ctx == nil || r == nil {
		return ctx
	}
	return context.WithValue(ctx, handleResultCtxKey{}, r)
}

// HandleResultFromContext 从 ctx 取出 HandleResult（注入方未设时返回 nil）。
func HandleResultFromContext(ctx context.Context) *HandleResult {
	if ctx == nil {
		return nil
	}
	if r, ok := ctx.Value(handleResultCtxKey{}).(*HandleResult); ok {
		return r
	}
	return nil
}

type agentIDCtxKey struct{}

// AgentIDToContext 把 agentID 注入 ctx（多 AI 智能体路由时填充）。
func AgentIDToContext(ctx context.Context, agentID string) context.Context {
	if ctx == nil || agentID == "" {
		return ctx
	}
	return context.WithValue(ctx, agentIDCtxKey{}, agentID)
}

func extractAgentIDFromCtx(ctx context.Context) string {
	if ctx == nil {
		return "unknown"
	}
	if v, ok := ctx.Value(agentIDCtxKey{}).(string); ok && v != "" {
		return v
	}
	return "unknown"
}

// ─── Telegram 群回复 @mention 上下文工具 ────────────────────────────

type telegramReplyMetaCtxKey struct{}

// TelegramReplyMeta 群回复 @mention 原发言人 + 触发原因
type TelegramReplyMeta struct {
	FromUsername  string // Telegram @username（可能为空 → fallback 用 fromName）
	FromName      string // 真实姓名（HTML 转义后的展示名）
	FromUserID    int64  // tg://user?id= 链接用
	ReplyToMsgID  int64  // ReplyToMessageID（Telegram int64，0 表示不引用）
	TriggerReason string // "mention" | "opportunity" | "private" | "start"
}

// TelegramReplyMetaToContext 把群回复元信息注入 ctx
func TelegramReplyMetaToContext(ctx context.Context, meta *TelegramReplyMeta) context.Context {
	if ctx == nil || meta == nil {
		return ctx
	}
	return context.WithValue(ctx, telegramReplyMetaCtxKey{}, meta)
}

func extractTelegramReplyMetaFromCtx(ctx context.Context) *TelegramReplyMeta {
	if ctx == nil {
		return nil
	}
	if v, ok := ctx.Value(telegramReplyMetaCtxKey{}).(*TelegramReplyMeta); ok {
		return v
	}
	return nil
}

// buildTelegramGroupReply 把 AI 回复内容包装成 Telegram 群聊友好格式：
//  1. 顶部 @mention 原发言人（<a href="tg://user?id=X">@username</a>）
//  2. ReplyToMessageID 引用原消息（群内对话更直观）
//  3. ParseModeHTML 确保 @mention 渲染成可点击链接
func buildTelegramGroupReply(content string, meta *TelegramReplyMeta) (string, telegram.SendMessageOptions) {
	opts := telegram.SendMessageOptions{
		ParseMode:                 "HTML",
		DisableMarkdownConversion: true, // 我们自己写 HTML（@mention + escape AI 回复）
		DisableWebPreview:         true,
	}
	if meta == nil {
		// 无 meta 也走 HTML escape（防御 LLM XSS）
		return htmlEscapeAndPreserveMarkdown(content), opts
	}
	if meta.ReplyToMsgID > 0 {
		opts.ReplyToMessageID = meta.ReplyToMsgID
	}

	// 1. 构造 @mention 头部（我们自己写的 HTML，可控安全）
	var mentionPrefix string
	displayName := meta.FromName
	if displayName == "" {
		displayName = meta.FromUsername
	}
	escapedDisplayName := htmlEscapeText(displayName)
	if escapedDisplayName == "" {
		escapedDisplayName = "朋友"
	}
	if meta.FromUserID > 0 {
		// 可点击链接 @mention
		mentionPrefix = fmt.Sprintf(`<a href="tg://user?id=%d">@%s</a> `, meta.FromUserID, escapedDisplayName)
	} else if meta.FromUsername != "" {
		mentionPrefix = "@" + htmlEscapeText(meta.FromUsername) + " "
	} else {
		mentionPrefix = escapedDisplayName + " "
	}

	// 2. AI 回复做 escape + 保留 **bold** 等 Markdown → HTML
	escapedContent := htmlEscapeAndPreserveMarkdown(content)

	// 3. 给商机触发加轻量引导提示
	var leadHint string
	if meta.TriggerReason == "opportunity" {
		leadHint = "\n💡 看到你对这个话题感兴趣，我是 HiveMtk 的 AI 助手，想帮你进一步了解~"
	}

	full := mentionPrefix + escapedContent + leadHint
	return full, opts
}

// htmlEscapeAndPreserveMarkdown 先 escape <>&" 防 XSS，再把 **bold** → <b>、`code` → <code>
// 对应 Telegram HTML parse_mode 支持的标签
func htmlEscapeAndPreserveMarkdown(s string) string {
	if s == "" {
		return ""
	}
	// Step 1: 完整 escape
	s = htmlEscapeText(s)
	// Step 2: 恢复 Markdown → HTML（和底层 markdownToTelegramHTML 一致）
	s = regexp.MustCompile(`\*\*(.+?)\*\*`).ReplaceAllString(s, "<b>$1</b>")
	s = regexp.MustCompile(``+"`"+`(.+?)`+"`").ReplaceAllString(s, "<code>$1</code>")
	s = regexp.MustCompile(`\*(.+?)\*`).ReplaceAllString(s, "<i>$1</i>")
	return s
}

// htmlEscapeText Telegram HTML parse_mode 需要的最小转义
func htmlEscapeText(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '&':
			b.WriteString("&amp;")
		case '"':
			b.WriteString("&quot;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
