package service

import (
	"context"
	"regexp"
	"sort"
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
// 若无后缀则原样返回。用于出站回声检测：bridge 采集把 AI 回复从 DOM 抓回时可能
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

	// 出站回声检测（base hash 比对）：bridge 采集把 AI 出站回复从 DOM 抓回当 inbound 时，
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
				// 网页渠道（抖音等）AI 的一条回复常被拆成多条气泡发送，扩展采集会把
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
					// 字符多重集比对（2026-11-14 生产实证 4836）：抖音 DOM 会把自家出站气泡
					// 抓成**交错拼接**（4836 长度 204 ≈ 出站 107 的字符交错双写），与任何
					// 「等值/包含」判据全部错位——等值不等、包含不连、剥后非空。字符频率
					// 与顺序无关：
					//   - 同频（入站排序 == 出站排序）：同一句话被打乱重排，必为回声；
					//   - 双倍频（入站排序 == 出站排序×2）：同一句话完整双份拼接，必为回声。
					// 真实客户消息恰好与本账号某条近期出站同字符集（或恰为双倍）的概率趋零，
					// 与「等值/包含」判据同一取舍。长度护栏：出站 <16 字不参与（同剥离护栏），
					// 防短出站（"好的~"）频率撞车误吞真实发言。
					if len([]rune(outNorm)) >= 16 {
						inSorted := sortedRunes(norm)
						outSorted := sortedRunes(outNorm)
						if inSorted == outSorted || inSorted == doubleSorted(outSorted) {
							return &IngressDecision{Blocked: true, IsSelfEcho: true, Reason: "self-echo(recent outbound char-multiset match)"}, nil
						}
						// 频率相似度判据（2026-11-14 第三形态 4848 实证）：混合变形（双份中
						// 一份缺 7 字等）既非同频也非整倍——改为逐字符频率重合度：出站全部
						// 字符在入站中的频率覆盖率 ≥85% 即判回声。真实客户引用+追问的追问
						// 部分会把覆盖率拉低，实测放行（见 echo_multiset_test）。
						if cover, ok := charCoverage(inSorted, outSorted); ok && cover >= 0.85 {
							return &IngressDecision{Blocked: true, IsSelfEcho: true, Reason: "self-echo(recent outbound char-frequency ≥85%)"}, nil
						}
					}
				}
				if strippedAny && len([]rune(remainder)) < 4 {
					return &IngressDecision{Blocked: true, IsSelfEcho: true, Reason: "self-echo(recent outbound contained in inbound)"}, nil
				}
				// 线程容器：整段线程的结尾就是本账号最近发出去的话。逐条剥离后仍会剩下
				// 客户更早的发言（≥4 字），上面的「剩余近空」判据会放行；这里用「入站尾部
				// N 字原样出现在近期同会话出站文本里」判定这段内容以我方发言收尾——客户
				// 在这段里没有新话，不能再触发一次回复。短入站不参与，避免吞掉「可以吗？」。
				if inRunes := []rune(norm); len(inRunes) >= echoTailWindowRunes {
					tail := string(inRunes[len(inRunes)-echoTailWindowRunes:])
					for i := range rows {
						outNorm := normalizeEchoText(rows[i].Content)
						if len([]rune(outNorm)) < echoTailMinOutboundRunes {
							continue
						}
						if strings.Contains(outNorm, tail) {
							return &IngressDecision{Blocked: true, IsSelfEcho: true, Reason: "self-echo(recent outbound tail match)"}, nil
						}
					}
				}
			}
		}
	}

	// 错归去重闸（批：Bridge 重复消息根治）：裸 msg_id（内容哈希，无 #N 后缀）在错归
	// 时间窗内已在任意会话入库，而本次事件却又报了不同会话——线上实证（tz3rmdj 2026-10-22
	// 20:27:01/03）这是扩展多窗口采集把 A 会话消息错归到 B 会话的上报：同一批 4 条 2 秒内
	// 二次入库、conversation_id 与 sender_name 整体错配，且因唯一索引含 conversation_id 而
	// 双双落库，触发重复回复。这种「同 hash 跨会话」在错归窗内不是真实新消息，判重复拦截。
	// 带 #N 后缀的（上报方明确断言独立消息）与带 channel_msg_id 的（平台给了真实身份）不在此列。
	if s.hubRepo != nil && event.ConversationID != "" && !eventAssertsDistinctMessage(event.EventID) {
		misID := event.EventID
		if chanMsgID != "" {
			misID = chanMsgID
		}
		if misID != "" && !occurrenceMsgIDRe.MatchString(misID) {
			if dup, derr := s.hubRepo.GetRecentByMsgIDAnyConv(ctx, misID, time.Now().Add(-InboundMisattribWindow)); derr == nil && dup != nil && dup.ConversationID != event.ConversationID {
				return &IngressDecision{Blocked: true, IsDup: true,
					Reason: "duplicate(same msg_id ingested in another conversation within misattribution window)"}, nil
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
		// F1（002-multi-type-messages，2026-11-10）：媒体消息的判重输入并入 msg_type 与
		// media_url —— content 只到「[图片]」占位符粒度，两张不同图必须各自成键。
		// 文本消息输入逐字节不变（见 MediaAwareDedupInput）。
		dedupHash := ContentHashWithSender(event.Channel, s.senderKeyForDedup(event), MediaAwareDedupInput(event.MsgType, event.MediaURL, content))
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

const (
	// 尾部命中取入站最后 12 字：短到能穿过「可以吗？」这类真实短回复，又长到不会被
	// 常见套话偶然命中。参与比较的出站文本同样要求至少 12 字。
	echoTailWindowRunes = 12

	// InboundMisattribWindow 错归去重窗：裸 msg_id（内容哈希）跨会话二次上报的判定窗。
	// 线上实证的错归间隔是 2 秒（同一批 DOM 采集重扫）；取 2 分钟足以覆盖扩展一轮采集
	// 抖动，又远小于客户在两个真实会话里发出字节级相同内容的合理间隔下限。
	InboundMisattribWindow   = 2 * time.Minute
	echoTailMinOutboundRunes = 12
)

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

// sortedRunes 返回归一化文本的字符多重集签名（排序后拼接，与顺序无关）。
// 供自回显的字符多重集比对用（2026-11-14 4836 交错双写实证）。
func sortedRunes(s string) string {
	rs := []rune(s)
	sort.Slice(rs, func(i, j int) bool { return rs[i] < rs[j] })
	return string(rs)
}

// doubleSorted 返回签名 s 的「双倍频」签名（每字符出现次数×2）。
// 入站文本恰为某条出站的完整双份拼接（含交错形态）时，其多重集必为出站的二倍。
func doubleSorted(s string) string {
	rs := []rune(s)
	dbl := make([]rune, 0, len(rs)*2)
	for _, r := range rs {
		dbl = append(dbl, r, r)
	}
	return string(dbl)
}

// charCoverage 计算入站签名对出站签名的逐字符频率覆盖率：出站每个字符的入站出现
// 次数（封顶出站次数）之和 / 出站总长。ok=false 表示入站短于出站 70%（覆盖判据
// 无意义，直接放行给其他判据）。两个签名均已排序 → 线性双指针。
func charCoverage(inSorted, outSorted string) (float64, bool) {
	in, out := []rune(inSorted), []rune(outSorted)
	if len(out) == 0 || len(in) < len(out)*7/10 {
		return 0, false
	}
	covered := 0
	i, j := 0, 0
	for j < len(out) {
		if i >= len(in) || in[i] > out[j] {
			return float64(covered) / float64(len(out)), true // 出站该字符入站不足
		}
		if in[i] == out[j] {
			covered++
			j++
			i++
		} else { // in[i] < out[j]：入站有多余字符，消耗之
			i++
		}
	}
	return float64(covered) / float64(len(out)), true
}
