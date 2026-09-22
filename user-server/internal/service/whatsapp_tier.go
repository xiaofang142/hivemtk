package service

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"sync"
	"time"

	"hivemtk-user/internal/pkg/utils/logger"
)

// Tier WhatsApp 消息分层
type Tier string

const (
	TierMarketing      Tier = "marketing"
	TierUtility        Tier = "utility"
	TierAuthentication Tier = "authentication"
)

// 公开可调常量（对齐"冷启动 200~500 条/天、≤10~20/min+抖动"语义）
const (
	// TierMinRatePerMin 每分钟补币速率下限（条/min）
	TierMinRatePerMin = 12.0
	// TierDayCap 单 (peer,tier) 24h 滚动窗发送上限
	TierDayCap = 250
	// TierDayWindow 日上限窗口时长
	TierDayWindow = 24 * time.Hour
)

// ClassifyTierOf 模板类别优先，否则按意图关键词映射（默认 utility）
func ClassifyTierOf(templateCategory, intent string) Tier {
	switch strings.ToLower(strings.TrimSpace(templateCategory)) {
	case "marketing":
		return TierMarketing
	case "utility":
		return TierUtility
	case "authentication", "auth":
		return TierAuthentication
	}
	lower := strings.ToLower(intent)
	switch {
	case waContainsAny(lower, []string{"验证码", "otp", "verification", "登录校验", "one-time code"}):
		return TierAuthentication
	case waContainsAny(lower, []string{"营销", "唤醒", "促活", "推广", "优惠", "活动", "marketing", "promo", "winback", "re-engage"}):
		return TierMarketing
	case waContainsAny(lower, []string{"订单", "物流", "发货", "签收", "order", "shipping", "logistics", "delivery"}):
		return TierUtility
	default:
		return TierUtility
	}
}

func waContainsAny(s string, subs []string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

type tierBucket struct {
	tokens   float64
	lastAt   time.Time
	dayStart time.Time
	dayCount int
}

// TierPacer 分层发送节奏器：按 (peerKey, tier) 独立令牌桶，超限拒绝不阻塞
type TierPacer struct {
	// MinRatePerMin 补币速率（条/min），<=0 时回落 TierMinRatePerMin
	MinRatePerMin float64
	// DayCap 24h 窗上限，<=0 时回落 TierDayCap
	DayCap int
	// TierQuotas per-tier 24h/peer 配额覆盖表（nil 用默认；查不到或 <=0 视为不限）
	TierQuotas map[WATier]int
	// QualityDowngrade 评级转黄标志：true 时速率减半（≥50% 降速语义）
	QualityDowngrade bool

	jitterFn func() float64

	mu      sync.Mutex
	buckets map[string]*tierBucket
}

// per-tier 24h/peer 默认配额：marketing 1、utility 4、authentication 不限
const (
	TierQuotaMarketing = 1
	TierQuotaUtility   = 4
)

func (p *TierPacer) tierQuota(tier WATier) int {
	if p.TierQuotas != nil {
		return p.TierQuotas[tier]
	}
	switch tier {
	case WATierMarketing:
		return TierQuotaMarketing
	case WATierUtility:
		return TierQuotaUtility
	default:
		return 0
	}
}

// NewTierPacer 构造（jitter 默认固定 1.0，确定性优先；生产可用 SetJitterFn 注入随机抖动）
func NewTierPacer() *TierPacer {
	return &TierPacer{
		MinRatePerMin: TierMinRatePerMin,
		DayCap:        TierDayCap,
		jitterFn:      func() float64 { return 1.0 },
		buckets:       make(map[string]*tierBucket),
	}
}

// SetJitterFn 注入速率抖动因子（返回 [min,max) 区间乘数）
func (p *TierPacer) SetJitterFn(fn func() float64) {
	if fn == nil {
		return
	}
	p.jitterFn = fn
}

// DefaultJitter 默认随机抖动因子 [0.85, 1.15)
func DefaultJitter() float64 { return 0.85 + 0.3*rand.Float64() }

// QualityDowngradeFactor 质量降档系数（Exported 可调；生效速率 = 原速率 × 该系数）
const QualityDowngradeFactor = 0.5

func (p *TierPacer) effectiveRate(now time.Time) float64 {
	rate := p.MinRatePerMin
	if rate <= 0 {
		rate = TierMinRatePerMin
	}
	if p.QualityDowngrade {
		rate *= QualityDowngradeFactor
	}
	rate *= p.jitterFn()
	if rate < 0.01 {
		rate = 0.01
	}
	_ = now
	return rate
}

// Enforce 发送前判定：允许消耗一枚令牌；超限直接拒绝并返回建议重试间隔。
// now 由调用方注入，纯确定性便于测试与回放。
func (p *TierPacer) Enforce(peerKey string, tier Tier, now time.Time) (allow bool, retryAfter time.Duration) {
	key := peerKey + "|" + string(tier)
	p.mu.Lock()
	defer p.mu.Unlock()

	rate := p.effectiveRate(now)
	capacity := rate
	if capacity < 1 {
		capacity = 1
	}
	dayCap := p.DayCap
	if dayCap <= 0 {
		dayCap = TierDayCap
	}

	b, ok := p.buckets[key]
	if !ok {
		b = &tierBucket{tokens: capacity, lastAt: now, dayStart: now}
		p.buckets[key] = b
	}

	if now.Sub(b.dayStart) >= TierDayWindow {
		b.dayStart = now
		b.dayCount = 0
	}

	if b.dayCount >= dayCap {
		return false, TierDayWindow - now.Sub(b.dayStart)
	}

	if q := p.tierQuota(tier); q > 0 && b.dayCount >= q {
		return false, TierDayWindow - now.Sub(b.dayStart)
	}

	if elapsed := now.Sub(b.lastAt).Minutes(); elapsed > 0 {
		b.tokens += elapsed * rate
		if b.tokens > capacity {
			b.tokens = capacity
		}
	}
	b.lastAt = now

	if b.tokens < 1 {
		retry := time.Duration((1 - b.tokens) / rate * float64(time.Minute))
		if retry < time.Second {
			retry = time.Second
		}
		return false, retry
	}
	b.tokens--
	b.dayCount++
	return true, 0
}

var (
	tierPacerOnce sync.Once
	tierPacer     *TierPacer
)

// GetGlobalTierPacer 获取全局节奏器（惰性单例；QualityDowngrade 由评级回调置位）
func GetGlobalTierPacer() *TierPacer {
	tierPacerOnce.Do(func() {
		tierPacer = NewTierPacer()
		tierPacer.SetJitterFn(DefaultJitter)
	})
	return tierPacer
}

func enforceWhatsAppTierPacing(peerKey, templateCategory string, now time.Time) error {
	tier := ClassifyTierOf(templateCategory, "")
	allow, retryAfter := GetGlobalTierPacer().Enforce(peerKey, tier, now)
	if !allow {
		logger.Warnf("[R-7] WhatsApp pacing 拒绝 peer=%s tier=%s retryAfter=%v", peerKey, tier, retryAfter)
		// 节奏器自己算出的等待值必须以 Duration 传出去：拼成 retry_after=1m0s 再让上层
		// 从串里反解，等于把一个已知的结构事实降级成文案（N-11①）。
		return &ChannelError{
			Channel:    string(ChannelWhatsapp),
			Category:   CategoryRateLimited,
			Retryable:  true,
			RetryAfter: retryAfter,
			Raw:        fmt.Sprintf("whatsapp pacing rejected: peer=%s tier=%s retry_after=%s", peerKey, tier, retryAfter),
		}
	}
	return nil
}

// WATier 规格命名别名（同 Tier）
type WATier = Tier

// WATier 常量别名
const (
	WATierMarketing      = TierMarketing
	WATierUtility        = TierUtility
	WATierAuthentication = TierAuthentication
)

// 规格常量（与 TierMinRatePerMin/TierDayCap 同值）
const (
	// MinPerMinute 每分钟补币速率下限（条/min）
	MinPerMinute = 12
	// DayCap 单桶 24h 窗上限
	DayCap = 250
)

// ClassifyTier 规格签名入口：仅按模板类别分层（marketing/utility/authentication，未知回落 utility）
func ClassifyTier(category string) WATier {
	return ClassifyTierOf(category, "")
}

// ClassifyWATier 规格命名入口：模板类别优先，其次意图关键词映射（语义同 ClassifyTierOf）
func ClassifyWATier(templateCategory, intent string) WATier {
	return ClassifyTierOf(templateCategory, intent)
}

const tierGlobalPeer = "_global_tier"

// EnforceTier 规格签名 per-tier 限流入口：Enforce(tier, now)→(allow, retryAfter)
func (p *TierPacer) EnforceTier(tier WATier, now time.Time) (allow bool, retryAfter time.Duration) {
	return p.Enforce(tierGlobalPeer, tier, now)
}
