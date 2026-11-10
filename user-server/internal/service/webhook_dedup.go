package service

import (
	"context"

	"crypto/sha1"

	"crypto/sha256"

	"encoding/hex"

	"fmt"

	"os"

	"strconv"

	"strings"

	"sync"

	"time"

	"hivemtk-user/internal/model"

	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"

	"hash/fnv"
	"hivemtk-user/internal/cache"
	"io"
)

type webhookJob struct {
	event   *model.WebhookEvent
	raw     []byte
	header  map[string]string
	source  string
	channel WebhookChannel
	account string
	payload *ParsedPayload
}

type tokenBucket struct {
	mu         sync.Mutex
	capacity   int
	refillRate float64
	tokens     float64
	lastRefill time.Time
	lastAccess time.Time
}

// 七个常量的含义与取值影响见 config_param_seeds.go 的 webhook 组。它们现在只是兜底值：
// 运行时读的是 webhook_runtime_params.go 里的参数中心/注入函数。
const (
	WebhookDedupTTL = 5 * time.Minute

	WebhookWorkerCount = 4

	WebhookQueueSize = 512

	WebhookRateLimit = 30

	WebhookRateBurst = 60

	WebhookMaxRetries = 3

	WebhookReplyConcurrency = 32
)

func webhookEnvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}

func groupNameFromHub(hub *model.MessageHub) string {
	if hub == nil || hub.Extra == nil {
		return ""
	}
	if v, ok := hub.Extra["group_name"]; ok {
		if s, _ := v.(string); s != "" {
			return s
		}
	}
	return ""
}

func sortStrings(a []string) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j-1] > a[j]; j-- {
			a[j-1], a[j] = a[j], a[j-1]
		}
	}
}

func sha1Hex(b []byte) string {
	h := sha1.Sum(b)
	return hex.EncodeToString(h[:])
}

func ContentHashMsgID(channel, conversationID, content string) string {

	s := channel + "|" + strings.TrimSpace(content)
	h := fnv.New32a()
	h.Write([]byte(s))
	return fmt.Sprintf("mh:%08x", h.Sum32())
}

// ContentHashMsgIDTyped 带消息类型的统一 msg_id 哈希（002-multi-type-messages）。
//
// 背景：非文本消息的 content 经常是占位符（「[图片]」），media_url 才是这条消息的
// 真正身份。旧口径 ContentHashMsgID(channel, conversationID, content) 对同会话的
// 两张不同图片产出完全相同的 msg_id ⇒ 入站幂等（钩子2 msg_id 判等）把第二条当
// 重复吞掉，消息蒸发。
//
// 口径：
//   - msgType 为空或 "text"（不区分大小写）：与 ContentHashMsgID 逐字节一致——
//     文本消息的 msg_id 全部沿用旧口径，存量行/前端旧哈希不迁移不重算；
//   - 非文本：输入并入 msgType 与 mediaURL（channel|msgType|mediaURL|content），
//     前端 types.js 的 typedContentHash 必须与本函数逐字节一致。
func ContentHashMsgIDTyped(channel, conversationID, msgType, mediaURL, content string) string {
	t := strings.ToLower(strings.TrimSpace(msgType))
	if t == "" || t == "text" {
		return ContentHashMsgID(channel, conversationID, content)
	}
	s := channel + "|" + t + "|" + strings.TrimSpace(mediaURL) + "|" + strings.TrimSpace(content)
	h := fnv.New32a()
	h.Write([]byte(s))
	return fmt.Sprintf("mh:%08x", h.Sum32())
}

// mediaContextTag 非文本消息的媒体上下文词，与直连渠道占位符词表（feishuInboundPlaceholders 等）
// 及前端 types.js::mediaPlaceholder 同源同义：image→[图片]、audio→[语音]。
// voice 在桥端五渠道解析器里原样上报（douyin/tiktok/xhs/xianyu 的 msg_type='voice'），
// 必须在此收录——triggerAIForEvent 拿到的是未归一的 event.MsgType，落库路径的
// InboundHubMsgType 归一（voice→audio）救不了这条调用链。未知类型兜 `[<类型>]`，
// 与 feishuInboundPlaceholder 的兜底口径一致。
var mediaContextTags = map[string]string{
	model.MsgTypeImage:    "[图片]",
	model.MsgTypeVideo:    "[视频]",
	model.MsgTypeAudio:    "[语音]",
	"voice":               "[语音]",
	model.MsgTypeFile:     "[文件]",
	model.MsgTypeLink:     "[链接]",
	model.MsgTypeCard:     "[卡片]",
	model.MsgTypeLocation: "[位置]",
}

func mediaContextTag(msgType string) string {
	if tag, ok := mediaContextTags[msgType]; ok {
		return tag
	}
	return "[" + msgType + "]"
}

// AppendMediaContextToContent 把媒体信息拼进 AI 触发的 content（FR5.3，002-multi-type-messages）。
//
// 非文本入站消息的 content 常是占位符（「[图片]」），AI/坐席只知道「有图」却不知道图在哪。
// 本函数在 content 后追加 `[<类型词>] <media_url>`，签名与文本链路零破坏：
//   - msgType 为空/text，或 mediaURL 为空：原样返回（文本消息逐字节不变）；
//   - content 已含该 URL（双重拼接防御）：原样返回——上游渠道把 URL 写进正文的
//     形态（如某些 webhook 把 caption 与链接合并下发）不能再追加一份，否则
//     AI 上下文出现重复 URL，多轮后逐轮膨胀；
//   - content 非空：content + "\n" + 类型词 + " " + URL（换行保证「正文+附件」可解析）；
//   - content 为空：不带前导换行（纯媒体无正文的直连渠道会走到这里）。
//
// 桥接路径（inbox_ingress.go triggerAIForEvent）与直连路径（webhook_ai.go
// triggerSmartOrchestrator）共用本函数，保证两条漏斗给 AI 的上下文同一口径。
func AppendMediaContextToContent(content, msgType, mediaURL string) string {
	t := strings.ToLower(strings.TrimSpace(msgType))
	if t == "" || t == model.MsgTypeText {
		return content
	}
	u := strings.TrimSpace(mediaURL)
	if u == "" {
		return content
	}
	// 幂等防御：正文里已带同一 URL 时不再追加（防双重拼接，对抗性复核 2026-10-24）。
	if u != "" && strings.Contains(content, u) {
		return content
	}
	tag := mediaContextTag(t) + " " + u
	if content == "" {
		return tag
	}
	return content + "\n" + tag
}

// mediaAwareDedupSeparator MediaAwareDedupInput 的段分隔符。用 \x1f（ASCII Unit Separator）：
// 任何平台正文/媒体 URL 里都不会出现，杜绝「正文尾 + 类型首」拼接出与真实消息同哈希的歧义。
const mediaAwareDedupSeparator = "\x1f"

// MediaAwareDedupInput 内容窗口去重键（Redis）与 message_hub.dedup_hash 列共用的哈希输入。
//
// 002-multi-type-messages 收口（2026-11-10 审计 F1）：非文本消息的 content 常是「[图片]」
// 占位符，media_url 才是这条消息的身份。只按 content 判重时，同一客户在同一会话 5 分钟
// 窗口内连发的两张不同图 dupKey 完全相同 —— 第二张被判 duplicate 落库留痕但 AI 永远
// 看不到（「发了图不回」）。非文本消息把 msg_type 与 media_url 并入输入段；
// 文本/空类型/system/recall 与旧口径逐字节一致（存量行为不变，dedup_hash 列不重算）。
//
// 调用点：interceptInbound（Redis 窗口键）与 persistMessage（dedup_hash 列）——
// 两处必须同源，否则键与列各说各话，排查时无法对账。
func MediaAwareDedupInput(msgType, mediaURL, content string) string {
	t := strings.ToLower(strings.TrimSpace(msgType))
	if t == "" || t == model.MsgTypeText || t == "system" || t == "recall" {
		return strings.TrimSpace(content)
	}
	return strings.TrimSpace(content) + mediaAwareDedupSeparator + t + mediaAwareDedupSeparator + strings.TrimSpace(mediaURL)
}

// ContentHashWithSender 统一收件去重哈希（渠道 + 发送者名称 + 消息内容）。
//
// 这是「渠道+发送者+消息内容」唯一去重依据的权威实现，前端（types.js::sharedContentHash）
// 与后端必须逐字节一致：
//   - 算法：FNV-1a 32 位，输入 channel|senderName|TrimSpace(content)，UTF-8 字节
//   - 输出：mh:<8位hex>
//
// 设计要点（与 ContentHashMsgID 的区别）：
//   - ContentHashMsgID 仅含渠道+内容，无法区分「AI 自己发的」与「客户复述了 AI 的原话」，
//     会导致客户复述被误判为回显而丢失（回环去重误杀）。
//   - 本函数把发送者纳入哈希，使「平台自己发出的消息」与「客户发的消息」拥有不同的去重键，
//     从而真正基于 (渠道,发送者,内容) 三元组做去重/自他判定。
//
// 发送者名称以服务端权威判定为准（见 inbox_ingress.senderKeyForDedup）：前端 patrol 上报的
// sender_type/sender_name 不可信（无法可靠分辨自他），服务端在入库前通过 DB 回查 message_hub
// 出站(outbound)行判定「自己消息」，再以账号(platform 身份)回填发送者，保证自/他区分不依赖前端标签。
//
// 注意：content 仍做 TrimSpace（与 ContentHashMsgID 保持一致，兼容首尾空白差异），
// 但严禁加入 conversationID——这句约束的范围是「这个哈希作为 msg_id / dedup_hash 两列的值」：
// 跨会话同内容必须落同一哈希，复合唯一索引 (msg_id, conversation_id) 才留得出位置。
// 它不覆盖 Redis 入口去重键：那把键在 inbox_ingress_ingest.go 里把 conversation_id 追加在哈希
// 之外（键 = 前缀 + 本哈希 + ":" + 会话），命中即丢、没有索引兜底，键不带会话就会跨会话互吞。
// 哈希本身与前端 types.js::sharedContentHash 逐字节一致，改不得。
func ContentHashWithSender(channel, senderName, content string) string {
	s := channel + "|" + senderName + "|" + strings.TrimSpace(content)
	h := fnv.New32a()
	h.Write([]byte(s))
	return fmt.Sprintf("mh:%08x", h.Sum32())
}

func (s *WebhookService) isDuplicate(ctx context.Context, eventID string) bool {
	if eventID == "" {
		return false
	}
	key := "mtk:webhook:dedup:" + eventID
	set, err := cache.GetGlobalCache().SetNX(ctx, key, "1", webhookDedupTTL(ctx))
	if err != nil {
		logger.Ctx(ctx).Warn().Err(err).Str("event_id", eventID).Msg("[webhook] dedup 后端异常，放行")
		return false
	}
	if !set {

		logger.Ctx(ctx).Debug().Str("event_id", eventID).Msg("[webhook] dedup hit")
		return true
	}
	return false
}

func (s *WebhookService) allowRate(ctx context.Context, key string) bool {
	s.rlMu.Lock()

	if s.rlBuckets == nil {
		s.rlBuckets = make(map[string]*tokenBucket)
	}
	b, ok := s.rlBuckets[key]
	if !ok {
		// 令牌桶按 key 懒构造，桶只在内存里存活到清理器收走它为止，所以改参数对
		// 后续新建的桶立即生效，已存在的桶继续用旧值——限流本来就不追求瞬切。
		burst := webhookRateBurst(ctx)
		b = &tokenBucket{
			capacity:   burst,
			refillRate: float64(webhookRateLimit(ctx)),
			tokens:     float64(burst),
			lastRefill: time.Now(),
			lastAccess: time.Now(),
		}
		s.rlBuckets[key] = b
	}
	b.lastAccess = time.Now()
	s.rlMu.Unlock()
	return b.allow(ctx)
}

func (s *WebhookService) startRLJanitor(ctx context.Context) {

	utils.SafeGo(ctx, "webhook_dedup.rl_janitor", func(ctx context.Context) {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.rlMu.Lock()
				cutoff := time.Now().Add(-5 * time.Minute)
				for k, b := range s.rlBuckets {
					if b.lastAccess.Before(cutoff) {
						delete(s.rlBuckets, k)
					}
				}
				s.rlMu.Unlock()
			}
		}
	})
}

func (s *WebhookService) generateEventID(ctx context.Context, channel WebhookChannel, accountID string, body []byte) string {
	h := sha256.Sum256([]byte(string(channel) + ":" + accountID + ":" + string(body)))
	return fmt.Sprintf("evt_%s", hex.EncodeToString(h[:8]))
}

func (s *WebhookService) genMessageID(ctx context.Context, channel WebhookChannel, accountID string, p *ParsedPayload) string {
	h := sha256.Sum256([]byte(string(channel) + ":" + accountID + ":" + p.Sender + ":" + p.Content + ":" + p.EventID))

	return fmt.Sprintf("msg_%s", hex.EncodeToString(h[:])[:22])
}

// TruncateForStore 截断防止 raw_data 过大
func (s *WebhookService) TruncateForStore(ctx context.Context, body []byte) string {
	const max = 64 * 1024
	if len(body) <= max {
		return string(body)
	}
	return string(body[:max]) + "...[truncated]"
}

func (s *WebhookService) getAccountSecret(ctx context.Context, platform, accountID string) (string, error) {
	if s.accountRepo == nil {
		return "", nil
	}

	if acc, err := s.accountRepo.GetByPlatformAndAccount(ctx, platform, accountID); err == nil && acc != nil {
		return acc.APISecret, nil
	}
	acc, err := s.accountRepo.GetByPlatform(ctx, platform)
	if err != nil || acc == nil {
		return "", nil
	}
	return acc.APISecret, nil
}

// PendingCount 待处理事件数
func (s *WebhookService) PendingCount(ctx context.Context) int64 {
	if s.eventRepo == nil {
		return 0
	}
	c, _ := s.eventRepo.CountUnprocessed(ctx)
	return c
}

// QueueLen 队列长度
func (s *WebhookService) QueueLen(ctx context.Context) int { return len(s.queue) }

// MaxWebhookBody 渠道回调请求体硬上限。
// /api/webhook/* 是**公网未认证入口**（验签必须先拿到完整 body 计算 HMAC，
// 读体必然发生在任何鉴权之前），此前 io.ReadAll 不设限 ⇒ 任意匿名者
// 持续 stream 大 body 即可打爆进程内存。各渠道官方事件 payload 远小于
// 64KB（媒体走 URL 不走 base64），2MB 给合法流量留一个数量级余量；
// 超限体被截断后 JSON 解析/验签必失败，走既有 400 分支——fail-closed。
const MaxWebhookBody int64 = 2 << 20

// ReadAll 读取渠道回调原始 body（带硬上限，见 MaxWebhookBody）
func ReadAll(r io.Reader) ([]byte, error) { return io.ReadAll(io.LimitReader(r, MaxWebhookBody)) }

func getString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}
