package service

import (
	"bytes"

	"context"

	"encoding/json"

	"fmt"

	"io"

	"net/http"

	"net/url"

	"os"
	"regexp"
	"strconv"

	"strings"

	"sync"

	"time"

	"hivemtk-user/internal/model"

	_db "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"

	agent_runtime "hivemtk-user/internal/aiagent/agent/runtime"
	"hivemtk-user/internal/pkg/tracing"
	"hivemtk-user/internal/repository"

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
	// （回收语义属产品决策，见 2026-09-19 审计第七轮）。
	delayedSendingStuckThreshold = time.Hour
)

// DelayedOutboundReply 别名（模型已收敛到 model 层，表 reach_delayed_outbound）
type DelayedOutboundReply = model.DelayedOutboundReply

func isAIReplyQuietHours(t time.Time) bool {
	if os.Getenv("DISABLE_AI_QUIET_HOURS") != "" {
		return false
	}
	return inQuietHoursWindow(t, aiReplyQuietStartHour, aiReplyQuietEndHour)
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
		SendAt:         nextQuietHoursRelease(time.Now(), aiReplyQuietEndHour),
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
		at = nextQuietHoursRelease(at, aiReplyQuietEndHour)
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
	// "下一次有人入队"才被顺带启动的消费者取走（T-P0-07）。抢占式 pending→sending
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
		picked, err2 = s.delayedRepo.ListSendingByID(ctx, ids, delayedOutboundBatchSize)
		if err2 != nil || len(picked) == 0 {
			return
		}
	}
	for i := range picked {
		s.replayDelayedOutbound(ctx, &picked[i])
	}
}

func (s *WebhookService) replayDelayedOutbound(ctx context.Context, rec *DelayedOutboundReply) {
	channel := WebhookChannel(rec.Platform)
	now := time.Now()
	isRetry := rec.Kind == model.DelayedKindSendRetry

	if isRetry && rec.Attempts >= sendRetryMaxAttempts {
		s.abandonReplay(ctx, rec, fmt.Sprintf("重投次数用尽（%d 次）", rec.Attempts))
		return
	}
	if isRetry && s.messageHubRepo != nil && rec.ConversationID != "" {
		// 旧内容不补投：等待期间坐席或新一轮 AI 回复已经出手，再投这条过期回复等于双份答案。
		unreplied, _, err := s.messageHubRepo.HasUnrepliedCustomerMessage(ctx, rec.ConversationID, InboxReplyWindow)
		if err == nil && !unreplied {
			if ferr := s.delayedRepo.FinishReplay(ctx, rec.ID, model.DelayedStatusSuperseded, time.Now(), ""); ferr != nil {
				logger.Ctx(ctx).Warn().Err(ferr).Uint("id", rec.ID).Msg("[H-3] 重放改判 superseded 回写失败")
			}
			logger.Ctx(ctx).Info().Uint("id", rec.ID).Str("conv_id", rec.ConversationID).
				Msg("[H-3] 会话已被回复，队列中的旧出站不再补投（superseded）")
			return
		}
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
	case sent, !isRetry:
		// 投递成功按 sent 收口；quiet hours 首发维持一次性语义（这条回复是按
		// "次日首发"设计的，失败已由 sendOutbound 落失败轨迹，再叠重试等于改掉窗口定义）。
		if err := s.delayedRepo.MarkSent(ctx, rec.ID, time.Now()); err != nil {
			logger.Ctx(ctx).Warn().Err(err).Uint("id", rec.ID).Msg("[H-3] 延迟回复状态回写失败")
		}
	default:
		ce := AsChannelError(sendErr)
		if ce != nil && ce.Retryable {
			attempts := rec.Attempts + 1
			if err := s.delayedRepo.ScheduleRetry(ctx, rec.ID, nextSendRetryAt(now, attempts, ce), ce.Raw); err != nil {
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
	if err := s.delayedRepo.FinishReplay(ctx, rec.ID, model.DelayedStatusFailed, time.Now(), reason); err != nil {
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
		s.outboundSendFailed(ctx, channel, accountID, hubMsg, content, err)
	}

	// markSendFailedLogged 用于"这一支没有可归属的轨迹行"（渠道不在出站分支表里 ⇒
	// 平台词表必然校验不过）：只把事实说清楚并留结构化日志，不尝试写库。
	markSendFailedLogged := func(err error) {
		sendErr = err
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
		go s.ingressSvc.RecheckUnrepliedAndTrigger(context.WithoutCancel(ctx), hubMsg.ConversationID, "")
	}()
	// 分支表里只有飞书与 TG 真把富卡下发（各自一个 `for _, card := range cards`）；
	// 其余渠道的 outbound 载荷没有卡片载体，这批卡走到这里就是整批丢掉，而文本回复照标 sent。
	// 没有痕迹的话，管理端与运维看到的都是一次成功出站 ⇒ 丢弃必须出声，且**只在这一处**出声：
	// 各渠道分支里再打一条就成了同一事件两行日志，告警侧按行数计数会翻倍。
	// 桥接五族额外把计数写进出站行（那一族的观测面就是那一行，见下面 bridge 分支）。
	if n := len(cards); n > 0 && channel != ChannelFeishu && channel != ChannelTelegram {
		drop := logger.Ctx(ctx).Warn().
			Str("module", "outbound").
			Str("channel", string(channel)).
			Str("account_id", accountID).
			Int("cards_dropped", n)
		if hubMsg != nil {
			drop = drop.Str("conversation_id", hubMsg.ConversationID)
		}
		drop.Msg("outbound channel has no card transport, rich cards dropped")
	}
	switch channel {
	case ChannelWeCom:

		if s.integration == nil {
			markSendFailed(preSendFailure(channel, CategoryBadRequest, "企微出站客户端未接线（integration 为 nil），本进程无法投递"))
			return
		}
		accID, err := strconv.ParseUint(accountID, 10, 64)
		if err != nil || accID == 0 {
			markSendFailed(preSendFailure(channel, CategoryBadRequest,
				fmt.Sprintf("企微出站账号 id 非法(account_id=%q)，无法定位发送账号", accountID)))
			return
		}
		if _, err := s.integration.SendMessage(ctx, &WeComSendRequest{
			AccountID:      uint(accID),
			ExternalUserID: p.Sender,
			MsgType:        "text",
			Content:        content,
			IsAIReply:      true,
			AIAgent:        "sales_engine",
		}); err != nil {
			markSendFailed(err)
		} else {
			sent = true
		}
	case ChannelFeishu:
		if s.feishuIntegration == nil {
			s.feishuIntegration = NewFeishuIntegrationService(s.lazyDB())
		}
		accID, err := strconv.ParseUint(accountID, 10, 64)
		if err != nil || accID == 0 {
			markSendFailed(preSendFailure(channel, CategoryBadRequest,
				fmt.Sprintf("飞书出站账号 id 非法(account_id=%q)，无法定位发送账号", accountID)))
			return
		}

		target := p.Sender
		idType := "open_id"
		if hubMsg != nil && hubMsg.IsGroup && hubMsg.GroupID != "" {
			target = hubMsg.GroupID
			idType = "open_chat_id"
		}
		outConv := ""
		if hubMsg != nil {
			outConv = hubMsg.ConversationID
		}
		if err := s.feishuIntegration.SendMessage(ctx, uint(accID), target, content, idType, outConv); err != nil {
			markSendFailed(err)
		} else {
			sent = true
		}
		// 富卡片随行下发（card.show 等工具产出），映射为飞书 interactive 卡片
		for _, card := range cards {
			cardJSON, merr := feishuCardToInteractiveJSON(&card)
			if merr != nil {
				logger.Ctx(ctx).Warn().Err(merr).Str("channel", "feishu").Msg("feishu card marshal failed, skip")
				continue
			}
			if cerr := s.feishuIntegration.SendInteractiveCard(ctx, uint(accID), target, cardJSON, idType, outConv); cerr != nil {
				logger.Ctx(ctx).Error().Err(cerr).Str("channel", "feishu").Str("open_id", target).Msg("outbound feishu card failed")
			} else {
				sent = true
			}
		}
	case ChannelTelegram:
		if s.tgIntegration == nil {
			s.tgIntegration = NewTelegramIntegrationService(s.lazyDB())
		}
		accID, err := strconv.ParseUint(accountID, 10, 64)
		if err != nil || accID == 0 {
			markSendFailed(preSendFailure(channel, CategoryBadRequest,
				fmt.Sprintf("TG 出站账号 id 非法(account_id=%q)，无法定位 bot", accountID)))
			return
		}

		var chatID int64
		if hubMsg != nil && hubMsg.ConversationID != "" {
			chatID, _ = strconv.ParseInt(hubMsg.ConversationID, 10, 64)
		}
		if chatID == 0 {
			markSendFailed(preSendFailure(channel, CategoryBadRequest,
				fmt.Sprintf("TG 会话键无法解析为 chat_id(conversation_id=%q)", convIDOrEmpty(hubMsg))))
			return
		}

		// 群聊增强：@mention 原发言人 + reply-to 消息
		// 私聊保持原样
		sendContent := content
		sendOpts := telegram.SendMessageOptions{DisableWebPreview: true}
		if hubMsg != nil && hubMsg.IsGroup {
			if meta := extractTelegramReplyMetaFromCtx(ctx); meta != nil && meta.TriggerReason != "start" {
				sendContent, sendOpts = buildTelegramGroupReply(content, meta)
			}
		}

		if err := s.tgIntegration.SendMessageEx(ctx, uint(accID), chatID, sendContent, sendOpts); err != nil {
			markSendFailed(err)
		} else {
			sent = true
		}

		for _, card := range cards {
			if err := s.tgIntegration.SendCard(ctx, uint(accID), chatID, &card); err != nil {
				logger.Ctx(ctx).Error().Err(err).Str("channel", "telegram").Int64("chat_id", chatID).Msg("outbound card failed")
			} else {
				sent = true
			}
		}
	case ChannelQQ:
		accID, err := strconv.ParseUint(accountID, 10, 64)
		if err != nil || accID == 0 {
			markSendFailed(preSendFailure(channel, CategoryBadRequest,
				fmt.Sprintf("QQ 出站账号 id 非法(account_id=%q)，无法定位机器人", accountID)))
			return
		}
		convID := ""
		if hubMsg != nil && hubMsg.ConversationID != "" {
			convID = hubMsg.ConversationID
		}
		if convID == "" {
			markSendFailed(preSendFailure(channel, CategoryBadRequest, "QQ 出站缺少会话 id，无被动回复目标"))
			return
		}
		// 被动回复关联原消息 ID（QQ 平台 5 分钟窗口 5 条被动回复额度）。
		// 惰性单例：msg_seq 计数与 access_token 缓存必须在多次出站间保持，
		// 每次新建实例会导致 seq 恒为 1（平台按重复丢弃）+ token 重复获取。
		if s.qqIntegration == nil {
			s.qqIntegration = NewQQIntegrationService(s.lazyDB())
		}
		if err := s.qqIntegration.SendMessage(ctx, uint(accID), convID, QQOutboundMsgID(hubMsg), content); err != nil {
			markSendFailed(err)
		} else {
			sent = true
		}
	case ChannelWhatsapp:
		if s.waIntegration == nil {
			s.waIntegration = NewWhatsAppCloudIntegrationService(s.lazyDB())
		}
		accID, err := strconv.ParseUint(accountID, 10, 64)
		if err != nil || accID == 0 {
			markSendFailed(preSendFailure(channel, CategoryBadRequest,
				fmt.Sprintf("WhatsApp 出站账号 id 非法(account_id=%q)，无法定位 WABA", accountID)))
			return
		}

		s.ensureReposFromDB(ctx)
		if s.messageHubRepo != nil && hubMsg != nil && hubMsg.ConversationID != "" {
			if last, qerr := s.messageHubRepo.GetLastInboundByConversation(ctx, hubMsg.ConversationID); qerr == nil && last != nil && !last.SentAt.IsZero() {
				if time.Since(last.SentAt) > 24*time.Hour {
					// 超 24h 客服窗口：自由文本会被 Meta 拒收，降级走预审批模板兜底
					//（模板名可用环境变量按账号/全局配置；未配置模板时维持原失败语义）。
					tplName := os.Getenv("WHATSAPP_FALLBACK_TEMPLATE")
					if tplName == "" {
						logger.Ctx(ctx).Warn().
							Str("channel", "whatsapp").Str("account_id", accountID).
							Str("to", p.Sender).
							Time("last_inbound_at", last.SentAt).
							Msg("[WhatsApp] 超出 24h 客服窗口且未配置 WHATSAPP_FALLBACK_TEMPLATE，AI 文本回复不可送达，标记失败")
						markSendFailed(preSendFailure(channel, CategoryWindowExpired,
							fmt.Sprintf("WhatsApp 24h 客服窗口已关闭(最后入站 %s)且未配置 WHATSAPP_FALLBACK_TEMPLATE，自由文本不可送达",
								last.SentAt.Format("2006-01-02 15:04:05"))))
						return
					}
					logger.Ctx(ctx).Info().
						Str("channel", "whatsapp").Str("account_id", accountID).
						Str("to", p.Sender).
						Str("template", tplName).
						Msg("[WhatsApp] 超出 24h 客服窗口，AI 回复降级为模板消息发送")
					if terr := s.waIntegration.SendTemplateMessage(ctx, uint(accID), p.Sender, content, &WhatsAppTemplatePayload{
						TemplateName: tplName,
						Language:     os.Getenv("WHATSAPP_FALLBACK_TEMPLATE_LANG"),
					}); terr != nil {
						markSendFailed(terr)
					} else {
						sent = true
					}
					return
				}
			}
		}

		if err := s.waIntegration.SendMessage(ctx, uint(accID), p.Sender, content); err != nil {
			markSendFailed(err)
		} else {
			sent = true
		}
	case ChannelDingTalk:

		webhookURL := ""
		var expiredAt int64
		if hubMsg != nil && hubMsg.Extra != nil {
			if v, ok := hubMsg.Extra["session_webhook"].(string); ok {
				webhookURL = v
			}
			switch t := hubMsg.Extra["session_webhook_expired_at"].(type) {
			case int64:
				expiredAt = t
			case float64:
				expiredAt = int64(t)
			}
		}
		if webhookURL == "" {
			logger.Ctx(ctx).Error().Str("channel", "dingtalk").Str("account_id", accountID).
				Str("conv_id", convIDOrEmpty(hubMsg)).
				Msg("[DingTalk] 缺少 sessionWebhook（回调未携带或非机器人消息），AI 回复无法送达")
			markSendFailed(preSendFailure(channel, CategoryWindowExpired,
				"钉钉回调未携带 sessionWebhook（非机器人消息或平台未下发），无可用的回复地址"))
			return
		}

		if expiredAt > 1_000_000_000_000 {
			expiredAt /= 1000
		}
		if expiredAt > 0 && time.Now().Unix() > expiredAt {
			logger.Ctx(ctx).Warn().Str("channel", "dingtalk").Str("account_id", accountID).
				Int64("expired_at", expiredAt).
				Msg("[DingTalk] sessionWebhook 已过期，AI 回复无法送达（用户重新发言可恢复）")
			markSendFailed(preSendFailure(channel, CategoryWindowExpired,
				fmt.Sprintf("钉钉 sessionWebhook 已过期(expired_at=%d)，被动回复窗口已关闭", expiredAt)))
			return
		}

		u, perr := url.Parse(webhookURL)
		if perr != nil || !loadDingtalkWebhookHostAllowed()(u) {
			logger.Ctx(ctx).Error().Str("channel", "dingtalk").Str("account_id", accountID).
				Msg("[DingTalk] sessionWebhook 域名非法（仅允许 *.dingtalk.com），拒绝发送")
			markSendFailed(preSendFailure(channel, CategoryBadRequest,
				"钉钉 sessionWebhook 域名不在允许清单(*.dingtalk.com)内，拒绝向外部地址投递"))
			return
		}
		payload := map[string]any{
			"msgtype": "text",
			"text":    map[string]string{"content": content},
		}
		body, _ := json.Marshal(payload)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
		if err != nil {
			logger.Ctx(ctx).Error().Err(err).Str("channel", "dingtalk").Msg("build dingtalk reply request failed")
			markSendFailed(preSendFailure(channel, CategoryBadRequest,
				fmt.Sprintf("钉钉回复请求构造失败: %v", err)))
			return
		}
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			logger.Ctx(ctx).Error().Err(err).Str("channel", "dingtalk").Msg("dingtalk reply send failed")
			markSendFailed(err)
			return
		}
		defer func() { _ = resp.Body.Close() }()
		respBody, _ := io.ReadAll(resp.Body)
		var dtResult struct {
			Errcode int    `json:"errcode"`
			Errmsg  string `json:"errmsg"`
		}
		if err := json.Unmarshal(respBody, &dtResult); err != nil || dtResult.Errcode != 0 {
			logger.Ctx(ctx).Error().Int("http_status", resp.StatusCode).Int("errcode", dtResult.Errcode).
				Str("errmsg", dtResult.Errmsg).
				Msg("[DingTalk] AI 回复出站被平台拒绝")
			if err != nil {
				markSendFailed(fmt.Errorf("dingtalk sessionWebhook 响应不可解析(status=%d): %w", resp.StatusCode, err))
			} else {
				markSendFailed(fmt.Errorf("dingtalk sessionWebhook errcode=%d errmsg=%s", dtResult.Errcode, dtResult.Errmsg))
			}
			return
		}
		sent = true
	case ChannelWechat:

		if s.wechatIntegration == nil {
			s.wechatIntegration = NewWechatService(s.lazyDB())
		}
		accID, err := strconv.ParseUint(accountID, 10, 64)
		if err != nil {
			accID = 0
		}
		if _, err := s.wechatIntegration.SendCustomMessage(ctx, uint(accID), p.Sender, "text", content); err != nil {
			logger.Ctx(ctx).Error().Err(err).Str("channel", "wechat").Str("account_id", accountID).
				Str("open_id", p.Sender).Msg("[Wechat] AI 回复出站失败")
			markSendFailed(err)
		} else {
			sent = true
		}
	case ChannelDouyin, ChannelXiaohongshu, ChannelTiktok, ChannelXianyu, ChannelKuaishou:

		if hubMsg != nil {
			outMsg := &model.MessageHub{
				MsgID:          ContentHashMsgID(string(channel), hubMsg.ConversationID, content),
				Platform:       string(channel),
				AccountID:      accountID,
				Direction:      "outbound",
				Status:         "pending",
				MsgType:        "text",
				SenderID:       accountID,
				ReceiverID:     hubMsg.SenderID,
				Content:        content,
				ConversationID: hubMsg.ConversationID,
				IsGroup:        hubMsg.IsGroup,
				GroupID:        hubMsg.GroupID,
				IsAIReply:      true,
				AIAgent:        "sales_engine",
				IsRead:         true,
				SentAt:         time.Now(),
			}
			outMsg.TraceID = tracing.LinkOutboundTraceID(ctx, hubMsg.ConversationID)

			outMsg.DedupHash = ContentHashWithSender(string(channel), accountID, content)

			outMsg.Extra = model.JSONMap{
				"dm_target":    "conv",
				"scenario":     "auto_reply",
				"triggered_by": "ai_dispatch",
			}
			if agentID := extractAgentIDFromCtx(ctx); agentID != "" {
				outMsg.Extra["agent_id"] = agentID
			}
			if result := HandleResultFromContext(ctx); result != nil {
				if result.Confidence > 0 {
					outMsg.Extra["confidence"] = result.Confidence
				}
				if result.SalesResponse != nil && result.SalesResponse.Intent != nil && result.SalesResponse.Intent.IntentType != "" {
					outMsg.Extra["intent"] = string(result.SalesResponse.Intent.IntentType)
				}
				if result.HandlerType != "" {
					outMsg.Extra["handler_type"] = string(result.HandlerType)
				}
			}
			// 桥接族的出库载体只有文本列（content + msg_type=text），富卡到这里没有可挂的下游
			// 通道，整批丢掉是既有行为；但"丢了"必须留痕。出声由函数入口那道统一门负责
			// （本族同样会走到），这里只补管理端读的那一行 —— 计数落库，界面上才分得清
			// "这单本来没卡"和"这单把卡丢了"。
			if n := len(cards); n > 0 {
				outMsg.Extra["cards_dropped"] = n
			}
			if undeliverable, reason := isBridgeChannelUndeliverableLocal(accountID, outMsg.ConversationID); undeliverable {
				outMsg.Status = "failed"
				if outMsg.Extra == nil {
					outMsg.Extra = model.JSONMap{}
				}
				outMsg.Extra["undeliverable_reason"] = reason
				outMsg.Extra["scenario"] = "undeliverable"
				logger.Ctx(ctx).Warn().
					Str("module", "bridge").
					Str("channel", string(channel)).
					Str("account_id", accountID).
					Str("conversation_id", outMsg.ConversationID).
					Str("reason", reason).
					Msg("bridge outbound target undeliverable; marked failed instead of pending")
			}

			persisted := outMsg
			if _, err := s.delayedRepo.CreateMessageHubIdempotent(ctx, outMsg); err != nil {
				retryMsg := &model.MessageHub{
					MsgID:          outMsg.MsgID + ":" + hubMsg.ConversationID,
					Platform:       outMsg.Platform,
					AccountID:      outMsg.AccountID,
					Direction:      "outbound",
					Status:         outMsg.Status,
					MsgType:        outMsg.MsgType,
					SenderID:       outMsg.SenderID,
					ReceiverID:     outMsg.ReceiverID,
					Content:        outMsg.Content,
					ConversationID: outMsg.ConversationID,
					IsGroup:        outMsg.IsGroup,
					GroupID:        outMsg.GroupID,
					IsAIReply:      outMsg.IsAIReply,
					AIAgent:        outMsg.AIAgent,
					IsRead:         outMsg.IsRead,
					SentAt:         outMsg.SentAt,
					TraceID:        outMsg.TraceID,
					DedupHash:      outMsg.DedupHash,
				}
				if _, err2 := s.delayedRepo.CreateMessageHubIdempotent(ctx, retryMsg); err2 != nil {
					logger.Ctx(ctx).Warn().Err(err).Str("module", "bridge").Str("channel", string(channel)).Msg("failed to persist bridge outbound reply to message_hub")
				} else {
					persisted = retryMsg
				}
			}
			if s.inboxConvRepo != nil && persisted.ID != 0 {

				if err := s.inboxConvRepo.UpsertFromMessage(ctx, repository.UpsertFromMessageInput{
					Platform:           persisted.Platform,
					AccountID:          persisted.AccountID,
					CustomerID:         persisted.ReceiverID,
					ConversationID:     persisted.ConversationID,
					LastMessageID:      persisted.ID,
					LastMessagePreview: persisted.Content,
					LastMessageAt:      persisted.SentAt,
					LastMessageFrom:    "ai",
				}); err != nil {
					logger.Ctx(ctx).Warn().Err(err).Str("module", "bridge").Msg("failed to upsert inbox_conversations for outbound")
				}
			}

			if GlobalSSEPublisher != nil && persisted != nil && persisted.ID != 0 &&
				(persisted.Status == "pending" || persisted.Status == "inflight") {
				logger.Ctx(ctx).Debug().
					Int("hub_id", int(persisted.ID)).
					Str("channel", string(channel)).
					Str("account_id", accountID).
					Str("conv_id", persisted.ConversationID).
					Msg("[SSE] webhook_outbound calling GlobalSSEPublisher")
				go func() {
					defer func() {
						if r := recover(); r != nil {
							logger.Ctx(ctx).Error().Any("error", r).Msg("[SSE] GlobalSSEPublisher panic recovered in webhook_outbound")
						}
					}()
					GlobalSSEPublisher(
						string(channel), accountID,
						uint64(persisted.ID),
						persisted.ConversationID,
						persisted.MsgType,
						persisted.ReceiverID,
						persisted.Content,
						persisted.IsAIReply,
						persisted.SentAt,
					)
				}()
			}

			enqueueAbnormal := ""
			enqueueStatus := tracing.StatusOk
			if persisted.Status == "failed" {
				enqueueStatus = tracing.StatusAbnormal
				enqueueAbnormal = "目标不可达（占位账号/未知账号），标记 failed 而非 pending，避免污染下行出库队列"
			}

			ctx = tracing.WithCarrier(ctx, &tracing.Carrier{
				TraceID:        persisted.TraceID,
				ConversationID: persisted.ConversationID,
				AccountID:      persisted.AccountID,
				Channel:        persisted.Platform,
			})

			outSpan := tracing.Start(ctx, tracing.NodeOutboundEnqueue).
				Input(map[string]any{
					"channel":     string(channel),
					"account_id":  accountID,
					"conv_id":     persisted.ConversationID,
					"content_len": len(persisted.Content),
					"is_ai_reply": persisted.IsAIReply,
				}).
				Expected("AI 回复落库 message_hub(status=pending)，进入下行出库队列").
				MsgID(persisted.MsgID)
			outSpan.Output(map[string]any{
				"msg_id": persisted.MsgID,
				"status": persisted.Status,
				"id":     persisted.ID,
			})
			var outSpanErr error
			if enqueueStatus == tracing.StatusAbnormal {
				outSpanErr = fmt.Errorf("%s", enqueueAbnormal)
			}
			outSpan.End(nil, outSpanErr)

			// sent 的口径（D-02）：= 这条回复确实交接给了桥接出库队列
			// （message_hub 落库成功且状态可被扩展领取），**不等于**客户已收到
			// ——真达由扩展 ack 走 UpdateDeliveryStatus 收口。
			// 未落库 / 被标 failed / 无入站上下文都不能算成功，否则重放 worker
			// 会把这条 MarkSent 掉，回复静默丢失。
			switch {
			case persisted.ID == 0:
				markSendFailed(&ChannelError{
					Category:  CategoryUnknown,
					Retryable: true,
					Raw:       "桥接出站未写入 message_hub(channel=" + string(channel) + " conv=" + persisted.ConversationID + ")",
				})
			case persisted.Status == "failed":
				reason, _ := persisted.Extra["undeliverable_reason"].(string)
				// 桥接族的失败轨迹就是它自己那一行（status=failed + scenario=undeliverable +
				// 原因），这里只补结构化错误与日志；再走一次轨迹落库会变成同一会话两行出站。
				markSendFailedLogged(preSendFailure(channel, CategoryBadRequest,
					"桥接目标不可达: "+reason))
			default:
				sent = true
			}
		} else {
			logger.Ctx(ctx).Error().Str("channel", string(channel)).Str("account_id", accountID).
				Msg("[Bridge] 缺少入站消息上下文（无 conversation 目标），AI 回复未出库")
			markSendFailed(preSendFailure(channel, CategoryBadRequest,
				fmt.Sprintf("%s 桥接出站缺少入站上下文，无 conversation 目标，未出库", channel)))
		}
	default:

		logger.Ctx(ctx).Warn().Str("channel", string(channel)).Str("account_id", accountID).Msg("unsupported outbound channel, skipped")
		markSendFailedLogged(preSendFailure(channel, CategoryBadRequest,
			fmt.Sprintf("出站不支持该渠道(%s)：webhookInboundCapable 与 sendOutbound 分支表已漂移", channel)))
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
