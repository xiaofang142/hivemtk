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
// 但严禁加入 conversationID——跨会话同内容(不同发送者)必须可区分，且复合唯一索引
// (msg_id, conversation_id) 已为跨会话同内容留出空间。
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
	set, err := cache.GetGlobalCache().SetNX(ctx, key, "1", WebhookDedupTTL)
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
		b = &tokenBucket{
			capacity:   WebhookRateBurst,
			refillRate: float64(WebhookRateLimit),
			tokens:     float64(WebhookRateBurst),
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

// MaxWebhookBody 渠道回调请求体硬上限（第十六轮审计，2026-09-19）。
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
