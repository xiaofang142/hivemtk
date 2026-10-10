package service

import (
	"context"
	"regexp"
	"time"
	"unicode"

	"errors"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"

	"strings"

	"gorm.io/gorm"
)

type IngressDecision struct {
	Blocked    bool
	IsSelfEcho bool
	IsDup      bool
	Reason     string
	// DedupKey 只填「本次调用真的占到的」那把内容窗口键（SetNX 抢到才算我占的）。
	// 入口是占坑在前、副作用在后，落库失败时必须由 releaseInboundDedup 把坑退回去，
	// 否则上游原样重投的第二次会被自己留下的键判成重复（§8.3-9(c)）。
	// 被判重复的那一次没有占新坑，所以这里留空——不能去删别人那把还在生效的键。
	DedupKey string
}

func (s *InboxIngressService) resolveSenderKey(event *model.MessageEvent) string {
	if event.SenderName != "" {
		return event.SenderName
	}
	if event.SenderID != "" {
		return event.SenderID
	}
	if event.ConversationID != "" {
		return event.ConversationID
	}
	return "unknown"
}

func resolveAccountID(event *model.MessageEvent) string {
	if event.Extra != nil {
		if v, ok := event.Extra["account_id"].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

func channelMsgIDOf(event *model.MessageEvent) string {
	if event == nil || event.Extra == nil {
		return ""
	}
	id, _ := event.Extra["channel_msg_id"].(string)
	if id == "" || strings.HasPrefix(id, "wa-out-") || strings.HasPrefix(id, "tg-out-") {
		return ""
	}
	return id
}

// occurrenceMsgIDRe 是桥接扩展声明「这是同一内容的第 n+1 次出现、它是独立的一条」的身份形状：
// `mh:<8位hex>#<n>`（n≥1）。裸 `mh:<8位hex>`（无后缀）是首条，字节与旧版一致，不在此列。
// 正则两头锚死，且必须与扩展 channel-adapter.js::_canonicalMsgId / types.js::contentHash 同源：
// 放宽这一个字符，内容维度的去重就对整个入站流失效。
var occurrenceMsgIDRe = regexp.MustCompile(`^mh:[0-9a-f]{8}#[1-9][0-9]*$`)

// stripOccurrenceSuffix 剥离 event_id 尾部的 `#N` 发生次数后缀，返回 base hash。
// 若无后缀则原样返回。用于出站回声检测：bridge 巡检把 AI 回复从 DOM 抓回时可能
// 因同内容重复出现而追加 #N 后缀，剥掉后才能与出站 msg_id（无后缀）对齐。
func stripOccurrenceSuffix(eventID string) string {
	if idx := strings.LastIndex(eventID, "#"); idx > 0 {
		base := eventID[:idx]
		if occurrenceMsgIDRe.MatchString(eventID) {
			return base
		}
	}
	return eventID
}

// eventAssertsDistinctMessage 判断上报方是否用身份本身否定了「内容即身份」这条假设。
// 为真时只放弃内容维度的嗅探（入口 Redis 内容窗口 + 落库钩子2.5 的三个内容查找），
// msg_id 精确判等（钩子2）与 DB 唯一索引照旧 —— 同一帧重发仍只有一行。
func eventAssertsDistinctMessage(eventID string) bool {
	return occurrenceMsgIDRe.MatchString(eventID)
}

func (s *InboxIngressService) senderKeyForDedup(event *model.MessageEvent) string {
	sk := s.resolveSenderKey(event)
	if event.SenderType == "self" || event.SenderType == "agent" {
		if acc := resolveAccountID(event); acc != "" {
			sk = acc
		}
	}
	return sk
}

func (s *InboxIngressService) senderDefinitelyDiffers(event *model.MessageEvent, ob *model.MessageHub) bool {
	if event == nil || ob == nil {
		return false
	}
	switch event.SenderType {
	case "self", "agent":
		return false
	}
	inSender := strings.TrimSpace(event.SenderID)
	outSender := strings.TrimSpace(ob.SenderID)
	if inSender == "" || outSender == "" {
		return false
	}
	return inSender != outSender
}

func (s *InboxIngressService) interceptInbound(ctx context.Context, event *model.MessageEvent) (*IngressDecision, error) {
	if s.hubRepo == nil {

		return &IngressDecision{}, nil
	}
	content := strings.TrimSpace(event.Content)
	if content == "" || event.Channel == "" {

		return &IngressDecision{}, nil
	}

	chanMsgID := channelMsgIDOf(event)
	if chanMsgID != "" && s.hubRepo != nil {
		accID := resolveAccountID(event)
		if event.ConversationID != "" && accID != "" {

			if _, err := s.hubRepo.GetOutgoingByPlatformMsgIDInConv(ctx, event.Channel, accID, event.ConversationID, chanMsgID); err == nil {
				return &IngressDecision{Blocked: true, IsSelfEcho: true, Reason: "self-echo(platform msg_id exact match)"}, nil
			}
		} else if _, err := s.hubRepo.GetOutgoingByPlatformMsgID(ctx, event.Channel, accID, chanMsgID); err == nil {
			return &IngressDecision{Blocked: true, IsSelfEcho: true, Reason: "self-echo(platform msg_id exact match)"}, nil
		}
	}

	// 出站回声检测（base hash 比对）：bridge 巡检把 AI 出站回复从 DOM 抓回当 inbound 时，
	// event_id 就是出站 msg_id 的 ContentHash（可能追加 #N 后缀）。剥离后缀后与出站 msg_id
	// 精确比对，命中即判定为自回声。此检查不依赖 sender_name / content / 时间窗口，
	// 是对下面三项内容维度嗅探的兜底加固。
	if event.EventID != "" && s.hubRepo != nil {
		baseID := stripOccurrenceSuffix(event.EventID)
		if existing, err := s.hubRepo.GetByMsgID(ctx, baseID); err == nil && existing != nil && existing.Direction == "outbound" {
			return &IngressDecision{Blocked: true, IsSelfEcho: true, Reason: "self-echo(event_id base hash match)"}, nil
		}
	}

	if ob, oerr := s.hubRepo.GetOutboundByPlatformSenderContentConv(ctx, event.Channel, event.SenderName, content, event.ConversationID); oerr == nil && ob != nil && !s.senderDefinitelyDiffers(event, ob) {
		return &IngressDecision{Blocked: true, IsSelfEcho: true, Reason: "self-echo(matched outbound by platform+sender_name+content)"}, nil
	}

	if event.ConversationID != "" && s.hubRepo != nil {
		rows, rerr := s.hubRepo.ListRecentOutboundInConv(ctx, event.Channel, resolveAccountID(event), event.ConversationID, time.Now().Add(-InboxOutboundEchoWindow), 20)
		if rerr == nil && len(rows) > 0 {
			norm := normalizeEchoText(content)
			if norm != "" {
				// 网页渠道（抖音等）AI 的一条回复常被拆成多条气泡发送，扩展巡检会把
				// 相邻气泡拼成一整条抓回，与任何单条出站记录都不再全等——回声就这样
				// 穿过等值闸触发二次回复（自回复循环）。
				// 剥离判定：把入站文本剥掉全部近期出站（≥16 字者才参与剥离），剩下的
				// 实质内容不足以构成一句真实发言（<4 字，如只剩标点/表情）时判回声。
				// 16 字护栏防止"好的~"这类短出站误剥；剥离后要求剩余近空，则客户
				// 引用我们某句话再追问的场景（剩余为自己的问题原文）不会被误吞。
				remainder := norm
				strippedAny := false
				for i := range rows {
					outNorm := normalizeEchoText(rows[i].Content)
					if outNorm == norm {
						return &IngressDecision{Blocked: true, IsSelfEcho: true, Reason: "self-echo(recent outbound normalized match)"}, nil
					}
					if len([]rune(outNorm)) >= 16 && strings.Contains(remainder, outNorm) {
						remainder = strings.ReplaceAll(remainder, outNorm, "")
						strippedAny = true
					}
				}
				if strippedAny && len([]rune(remainder)) < 4 {
					return &IngressDecision{Blocked: true, IsSelfEcho: true, Reason: "self-echo(recent outbound contained in inbound)"}, nil
				}
			}
		}
	}

	// 内容窗口去重只对「平台不给稳定消息 ID」的渠道有独立价值。带 channel_msg_id 的事件
	// 必须跳过：客户连发两条同样内容（或一次推两张图片、占位正文同为 "[图片]"）是两条真实
	// 消息，按内容相同就拦会让第二条既不入库也不回复（N-14 实测）。平台 at-least-once 重投
	// 本来由 message_hub 的 (platform, msg_id, conversation_id) 唯一索引 + isDuplicateKey 幂等兜底。
	// 键必须带 conversation_id：入口这一层是「命中即丢」，没有唯一索引兜底，不带会话就是同一发送者在
	// 两个会话里说同一句话时第二条被吞（客户侧「说了没回」）。webhook_dedup.go 里「严禁加入
	// conversationID」那句约束只针对 msg_id / dedup_hash 两列的 DB 语义，不覆盖这把 Redis 键。
	// 带发生次数后缀的身份（§8.3-17）同样跳过：上报方已经按可见顺序确认这是独立的一条，
	// 再按内容拦就等于把第二条真实消息吞在入库前。
	claimedKey := ""
	if s.cache != nil && chanMsgID == "" && !eventAssertsDistinctMessage(event.EventID) {
		dedupHash := ContentHashWithSender(event.Channel, s.senderKeyForDedup(event), content)
		dupKey := InboxSenderContentDedupKey + dedupHash + ":" + event.ConversationID
		ok, derr := s.cache.SetNX(ctx, dupKey, "1", s.contentDedupWindow())
		if derr == nil {
			if !ok {
				return &IngressDecision{Blocked: true, IsDup: true, Reason: "duplicate(channel+sender+content) within window"}, nil
			}
			claimedKey = dupKey
		}
	}

	return &IngressDecision{DedupKey: claimedKey}, nil
}

// releaseInboundDedup 把 interceptInbound 占到的内容窗口键退回去：占坑与副作用不在一个事务里
// （Redis 与 PG 之间没有共同事务），所以只能靠调用方在落库失败时补偿删除。
// 删除失败不阻断业务——那意味着最坏情况退回改这条之前的形态：窗口 TTL 内的重投被判重复，
// TTL（InboxContentDedupTTL）一过自愈，比把整条入站链路 fail-close 掉划算。
func (s *InboxIngressService) releaseInboundDedup(ctx context.Context, decision *IngressDecision) {
	if s.cache == nil || decision == nil || decision.DedupKey == "" {
		return
	}
	if err := s.cache.Delete(ctx, decision.DedupKey); err != nil {
		logger.Ctx(ctx).Warn().Err(err).Str("module", "inbox").
			Str("dedup_key", decision.DedupKey).
			Msg("[Inbox] 落库失败后的内容窗口键释放失败，等 TTL 自愈")
	}
}

func isDuplicateKey(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate key") ||
		strings.Contains(msg, "unique constraint") ||
		strings.Contains(msg, "commit unexpectedly resulted in rollback") ||
		errors.Is(err, gorm.ErrDuplicatedKey)
}

func groupNameOf(event *model.MessageEvent) string {
	if event == nil {
		return ""
	}
	if event.Extra != nil {
		if v, ok := event.Extra["group_name"]; ok {
			if s, _ := v.(string); s != "" {
				return s
			}
		}
	}
	return ""
}

func normalizeEchoText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.Is(unicode.Cf, r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
