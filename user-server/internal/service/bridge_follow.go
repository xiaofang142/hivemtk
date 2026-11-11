// bridge_follow.go —— 新用户（非关注好友）事件登记 + 自动关注策略（M-6，2026-11-11）。
//
// 职责两条：
//  1. 登记（默认开、无开关）：扩展端命中关注关系信号（extra.new_user_unfollowed，
//     经 channelgw 显式白名单透传）的桥接入站消息，按会话幂等登记首触事件
//     （bridge_follow_events，供运营观测），失败仅留日志不阻断入库/AI 主链路。
//  2. 策略（默认关）：参数中心 bridge/follow_user_enabled=true 且渠道在
//     bridge/follow_user_channels 清单内、信号可动作（非互关已建立）时，经既有
//     outbox 通路（DeliverBridgeOutboundMedia，msg_type=follow_user、content 空）
//     下发关注动作行；动作本身由扩展 adapter.followUser 执行（开关二次校验 +
//     拟人限速 + 会话冷却 + 已关注态词表防 toggle 误点）。
//
// 触发路径不阻塞 AI 回复主链路：调用方（inbox_ingress）以 utils.SafeGo 异步旁路
// 触发本服务的 TriggerFollowUser；本服务内不做任何同步等待。
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// FollowUserEnabledParam / FollowUserChannelsParam 是「自动关注动作」的唯一开关源。
//
// M-6（2026-11-11）：识别信号（new_user_unfollowed）只做登记观测，动作默认关——
// 关注是不可逆的对外写动作，且各渠道关注按钮词表未经真机校准。开启前必须先完成
// 真机校准（见任务 M-6 真机缺口清单）。默认值与 config_params 里
// bridge/follow_user_enabled 的 DefaultValue 同值，改动要一起改。
const (
	FollowUserEnabledParam    = "follow_user_enabled"
	FollowUserChannelsParam   = "follow_user_channels"
	FollowUserChannelsDefault = `["douyin","tiktok","bilibili"]`
)

// FollowActionMsgType 自动关注动作行的 outbox msg_type（扩展端 downlink.js
// FOLLOW_ACTION_MSG_TYPE 同值，改动两端同步）。content 恒空 ⇒ 行 msg_id 按
// ContentHashMsgIDTyped(channel, conv, "follow_user", "", "") 确定性生成，
// 同会话重复触发被 message_hub (platform,msg_id,conversation_id) 唯一索引天然去重。
const FollowActionMsgType = "follow_user"

// deliverFollowActionFn 关注动作行入队出口。生产恒为 DeliverBridgeOutboundMedia
// （既有 outbox 通路：message_hub pending 行 + SSE 推送，HTTP 轮询/SSE 双传输可取）；
// 独立成包级变量只为测试注入（策略闸门测试不需要真库）。
var deliverFollowActionFn = DeliverBridgeOutboundMedia

// FollowUserEnabled 读自动关注总开关（默认 false）。GlobalConfigParam nil-safe。
func FollowUserEnabled(ctx context.Context) bool {
	return GlobalConfigParam().GetBool(ctx, "bridge", FollowUserEnabledParam, false)
}

// FollowUserChannels 读允许自动关注的渠道清单（JSON 字符串数组）。
// 参数未装配/解析失败 → 回落默认清单（与未装配同形）；空数组合法 = 全渠道关。
func FollowUserChannels(ctx context.Context) []string {
	raw := GlobalConfigParam().GetString(ctx, "bridge", FollowUserChannelsParam, FollowUserChannelsDefault)
	return parseFollowUserChannels(raw)
}

// parseFollowUserChannels 把参数值解析为渠道清单（fail-open 到默认清单，M-5 同款）。
func parseFollowUserChannels(raw string) []string {
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		logger.Warnf("[ConfigParam] bridge/%s 解析失败（%v），回落默认清单", FollowUserChannelsParam, err)
		out = nil
	}
	if out == nil {
		_ = json.Unmarshal([]byte(FollowUserChannelsDefault), &out)
	}
	return out
}

// FollowSignalActionable 判断关注信号是否值得触发关注动作：
// mutual_follow（互相关注已建立）不需要回关；其余信号（对方关注了我们/明确非好友/
// 未关注 tab 归属/好友申请）都可动作。未知来源保守判 false（不打动作）。
func FollowSignalActionable(signal string) bool {
	switch signal {
	case model.FollowSignalMutual:
		return false
	case model.FollowSignalPeerFollow, model.FollowSignalNotMutual,
		model.FollowSignalUnfollowedTab, model.FollowSignalFriendRequest:
		return true
	}
	return false
}

// BridgeFollowService 新用户首触登记 + 自动关注策略执行。
type BridgeFollowService struct {
	repo  repository.BridgeFollowEventRepository
	nowFn func() time.Time
}

// NewBridgeFollowService 创建服务（repo 为空 = 整体不生效，调用方跳过）。
func NewBridgeFollowService(repo repository.BridgeFollowEventRepository) *BridgeFollowService {
	return &BridgeFollowService{repo: repo, nowFn: time.Now}
}

// SetNowForTest 测试注入时钟。
func (s *BridgeFollowService) SetNowForTest(fn func() time.Time) { s.nowFn = fn }

// RegisterUnfollowedFirstTouch 登记首触事件（每会话一条，dedup_key 幂等）。
// 返回 (是否新登记, error)。repo 为 nil 时 no-op（false, nil）。
func (s *BridgeFollowService) RegisterUnfollowedFirstTouch(ctx context.Context, event *model.MessageEvent, signal string) (bool, error) {
	if s == nil || s.repo == nil || event == nil {
		return false, nil
	}
	if event.Channel == "" || event.ConversationID == "" {
		return false, nil
	}
	detail := map[string]any{
		"account_id":  resolveAccountID(event),
		"event_id":    event.EventID,
		"sender_type": event.SenderType,
	}
	if event.SenderName != "" {
		detail["sender_name"] = event.SenderName
	}
	detailJSON, _ := json.Marshal(detail)
	row := &model.BridgeFollowEvent{
		Channel:        event.Channel,
		AccountID:      resolveAccountID(event),
		ConversationID: event.ConversationID,
		SenderName:     event.SenderName,
		EventType:      model.BridgeFollowEventFirstTouch,
		Signal:         signal,
		Result:         "registered",
		Detail:         string(detailJSON),
		MsgID:          event.EventID,
		DedupKey:       firstTouchDedupKey(event.Channel, event.ConversationID),
	}
	inserted, err := s.repo.InsertIdempotent(ctx, row)
	if err != nil {
		return false, err
	}
	if inserted {
		logger.Ctx(ctx).Info().
			Str("module", "bridge_follow").
			Str("channel", event.Channel).
			Str("conv_id", event.ConversationID).
			Str("signal", signal).
			Msg("[BridgeFollow] 新用户（非关注好友）首触登记")
	}
	return inserted, nil
}

// RecordFollowAction 登记自动关注动作审计行（动作行已成功入队后调用）。
func (s *BridgeFollowService) RecordFollowAction(ctx context.Context, channel, accountID, conversationID, senderName, signal, actionMsgID, trigger string) error {
	if s == nil || s.repo == nil {
		return nil
	}
	detail := map[string]any{"trigger": trigger, "account_id": accountID}
	if senderName != "" {
		detail["sender_name"] = senderName
	}
	detailJSON, _ := json.Marshal(detail)
	row := &model.BridgeFollowEvent{
		Channel:        channel,
		AccountID:      accountID,
		ConversationID: conversationID,
		SenderName:     senderName,
		EventType:      model.BridgeFollowEventFollowAction,
		Signal:         signal,
		Result:         "enqueued",
		Detail:         string(detailJSON),
		MsgID:          actionMsgID,
		DedupKey:       fmt.Sprintf("fa:%s:%s:%d", channel, conversationID, s.nowFn().UnixMilli()),
	}
	if _, err := s.repo.InsertIdempotent(ctx, row); err != nil {
		return err
	}
	logger.Ctx(ctx).Info().
		Str("module", "bridge_follow").
		Str("channel", channel).
		Str("conv_id", conversationID).
		Str("signal", signal).
		Str("action_msg_id", actionMsgID).
		Msg("[BridgeFollow] 自动关注动作行已入队（审计登记）")
	return nil
}

// TriggerFollowUser 策略执行：入队 follow_user 动作行 + 审计登记。
// 策略开关与渠道清单已由调用方（inbox_ingress）预检；这里只做入队与留痕。
// 失败返回 error（调用方日志留痕），绝不 panic/阻塞。
func (s *BridgeFollowService) TriggerFollowUser(ctx context.Context, channel, accountID, conversationID, senderName, signal, sourceEventID, trigger string) error {
	if s == nil || s.repo == nil {
		return nil
	}
	if channel == "" || accountID == "" || conversationID == "" {
		return fmt.Errorf("bridge_follow: channel/account_id/conversation_id required")
	}
	hubID, actionMsgID, err := deliverFollowActionFn(ctx, channel, accountID, conversationID, FollowActionMsgType, "", "", sourceEventID)
	if err != nil {
		// 动作行 msg_id 确定性（content 空）：同会话已有 pending/sent 动作行时唯一索引冲突
		// 到这里 —— 属「已触发过」的正常幂等，不打错误级。
		logger.Ctx(ctx).Info().
			Str("module", "bridge_follow").
			Str("channel", channel).
			Str("conv_id", conversationID).
			Err(err).
			Msg("[BridgeFollow] 关注动作行入队未落库（多为同会话已有动作行，幂等跳过）")
		return err
	}
	return s.RecordFollowAction(ctx, channel, accountID, conversationID, senderName, signal, actionMsgID, triggerOf(trigger, hubID))
}

func triggerOf(trigger string, _ uint64) string {
	if trigger == "" {
		return "first_touch"
	}
	return trigger
}

// firstTouchDedupKey 首触事件幂等键：每 (渠道, 会话) 一条。
func firstTouchDedupKey(channel, conversationID string) string {
	return fmt.Sprintf("uft:%s:%s", channel, conversationID)
}
