package service

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"hivemtk-user/internal/config"
	bizerr "hivemtk-user/internal/domain/errors"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/platform"
	"hivemtk-user/internal/repository"
)

// ============================================================
// 订阅镜像与本地到期判定（003 T022 · ADR-M8 修订 #2/#4）
//
// 锁判定完全本地化：用镜像里的 current_period_end / trial_expires_at 跑与
// 平台 CurrentStatus() 同款的惰性比较 —— now > period_end → 锁，**与平台
// 可达性无关**。fetched_at 只用于 Warn/可观测，不参与锁决策。fail-open
// 仅对「镜像无 period_end 数据」一种情形生效（从未同步到订阅段）。
// ============================================================

// SubscriptionMirrorService 订阅镜像与本地到期判定
type SubscriptionMirrorService struct {
	repo repository.TenantSubscriptionMirrorRepository
}

// NewSubscriptionMirrorService 构造（按次取全局 DB）
func NewSubscriptionMirrorService() *SubscriptionMirrorService {
	return &SubscriptionMirrorService{repo: repository.NewTenantSubscriptionMirrorRepository()}
}

// NewSubscriptionMirrorServiceWithRepo 带显式仓储（测试注入）
func NewSubscriptionMirrorServiceWithRepo(repo repository.TenantSubscriptionMirrorRepository) *SubscriptionMirrorService {
	return &SubscriptionMirrorService{repo: repo}
}

// SaveFromSnapshot sync 消费平台 subscription 段写镜像（system scope 调用方负责）。
//
// 平台段缺失（老平台/读失败）时不写：镜像保留最后已知态 —— 清掉会让
// 有 period_end 的行丢数据、锁判定退化成 fail-open（修订 #2 要堵的洞）。
func (s *SubscriptionMirrorService) SaveFromSnapshot(ctx context.Context, sub *platform.SubscriptionSnapshot) error {
	if sub == nil {
		return nil
	}
	now := time.Now()
	row := &model.TenantSubscriptionMirror{
		Status:     sub.Status,
		PriceCents: sub.PriceCents,
		FetchedAt:  now,
	}
	if v, err := parseRFC3339Ptr(sub.CurrentPeriodEnd); err != nil {
		logger.Warnf("[subscription-mirror] current_period_end 解析失败（按无数据处理）: %v", err)
	} else {
		row.CurrentPeriodEnd = v
	}
	if v, err := parseRFC3339Ptr(sub.TrialExpiresAt); err != nil {
		logger.Warnf("[subscription-mirror] trial_expires_at 解析失败（按无数据处理）: %v", err)
	} else {
		row.TrialExpiresAt = v
	}
	// status 缓存列按本地惰性判定回填，不盲信平台列（权威是两个时间戳的比较）
	row.Status = mirrorStatus(row, now)
	return s.repo.Upsert(ctx, row)
}

// mirrorHasPeriodData 镜像是否携带任何可用于判定的到期数据。
// false = 从未同步到订阅段（fail-open 放行，修订 #2 唯一的 fail-open 情形）。
// 逻辑自 model 下沉（架构门 [Model]：model 只留表映射）。
func mirrorHasPeriodData(m *model.TenantSubscriptionMirror) bool {
	if m == nil {
		return false
	}
	return m.CurrentPeriodEnd != nil || m.TrialExpiresAt != nil
}

// mirrorStatus 惰性判定（与平台 TenantSubscription.CurrentStatus 同款逻辑，独立实现）。
//
// 优先级：current_period_end 在未来 → active（已付费用户不再显示体验期，
// 即使 trial 期还没过完）；否则 trial_expires_at 在未来 → trial；否则 expired。
func mirrorStatus(m *model.TenantSubscriptionMirror, now time.Time) string {
	if m == nil {
		return model.SubscriptionStatusExpired
	}
	if m.CurrentPeriodEnd != nil && m.CurrentPeriodEnd.After(now) {
		return model.SubscriptionStatusActive
	}
	if m.TrialExpiresAt != nil && m.TrialExpiresAt.After(now) {
		return model.SubscriptionStatusTrial
	}
	return model.SubscriptionStatusExpired
}

// parseRFC3339Ptr 空串 → nil；非空按 RFC3339 解析
func parseRFC3339Ptr(s string) (*time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// Snapshot 订阅状态视图（GET /api/subscription/status 返回体）
type SubscriptionSnapshot struct {
	Status           string     `json:"status"` // trial | active | expired | unknown
	CurrentPeriodEnd *time.Time `json:"current_period_end,omitempty"`
	TrialExpiresAt   *time.Time `json:"trial_expires_at,omitempty"`
	PriceCents       int64      `json:"price_cents"`
	FetchedAt        *time.Time `json:"fetched_at,omitempty"`
	// Locked 本地判定结果：true = 业务面将被 4305 拦（expired）
	Locked bool `json:"locked"`
	// NoData 镜像无 period 数据（从未同步到订阅段）→ fail-open，前端不显示墙
	NoData bool `json:"no_data"`
	// Stale fetched_at 超过阈值：可观测提示，不影响锁决策
	Stale bool `json:"stale"`
}

// staleWarnAfter 镜像陈旧告警阈值（只 Warn，不升级为锁，修订 #2）
const staleWarnAfter = time.Hour

// statusRefreshTTL 状态接口回源平台的最小间隔。
//
// 状态接口被墙页、顶部订阅横幅和恢复轮询调用，不能每次都打平台；60s 足够让
// 「平台确认收款」在一分钟内反映到用户眼前，同时把出站请求压到 ≤1 次/分钟/租户。
const statusRefreshTTL = time.Minute

// statusRefreshTimeout 回源超时上限。
//
// 平台客户端整体超时是 5s，对页面加载太长：回源只是「尽力刷新」，2s 拿不到就
// 按镜像最后已知态返回（锁判定本地化，不依赖这次回源）。
const statusRefreshTimeout = 2 * time.Second

// Refresh 回源平台刷新**当前租户**的订阅镜像（状态接口用，尽力而为）。
//
// 为什么需要它：镜像按租户一行（id=tenant_id），而后台 10 分钟周期同步没有
// 租户绑定（只服务默认租户），其他租户的镜像只在它自己调 POST /api/modules/sync
// 时才刷新。于是平台确认收款后，用户刷新订阅页看到的仍是续费前的到期时间
// —— 2026-11-04 生产事故 ORD179377249492384KKX0 的根因。
// 这里让「看状态」这个用户动作本身触发一次回源，TTL 内不重复打平台。
//
// 失败语义：返回错误由调用方记日志，**不得**让状态接口失败 —— 平台不可达时
// 继续用镜像最后已知态（与 IsLocked 的 fail-open 同一取舍方向）。
func (s *SubscriptionMirrorService) Refresh(ctx context.Context) error {
	if !config.PlatformEnabled() {
		return nil
	}
	if row, err := s.repo.Get(ctx); err == nil && row != nil && time.Since(row.FetchedAt) < statusRefreshTTL {
		return nil
	}
	code := currentTenantCode(ctx)
	if tenant.IsMultiTenant() && code == "" {
		// 多租户下解析不出本租户短码：平台会按 tenant_code='' 返回**商户级**订阅段，
		// 写进当前租户的镜像就是串数据（与 Sync 的商户级写入口同一条红线）。
		// 宁可不刷新，等下一次带得上租户的请求。
		return fmt.Errorf("订阅镜像回源跳过：多租户下未解析出 tenant_code")
	}
	rc, cancel := context.WithTimeout(ctx, statusRefreshTimeout)
	defer cancel()
	snap, err := platform.NewModuleClient().SyncModules(rc, code)
	if err != nil {
		return err
	}
	if snap == nil || snap.Subscription == nil {
		// 平台未下发订阅段（老平台/读失败）：保留最后已知态，不清空。
		return nil
	}
	return s.SaveFromSnapshot(ctx, snap.Subscription)
}

// Snapshot 读镜像 + 本地惰性判定（状态 API 与闸共用同一口径）。
func (s *SubscriptionMirrorService) Snapshot(ctx context.Context) (*SubscriptionSnapshot, error) {
	row, err := s.repo.Get(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	out := &SubscriptionSnapshot{Status: "unknown", NoData: true}
	if row == nil {
		return out, nil
	}
	st := mirrorStatus(row, now)
	out.Status = st
	out.Locked = st == model.SubscriptionStatusExpired
	out.NoData = !mirrorHasPeriodData(row)
	if out.NoData {
		// 无 period 数据：fail-open —— 锁决策不生效（修订 #2 唯一放行情形）
		out.Locked = false
	}
	out.CurrentPeriodEnd = row.CurrentPeriodEnd
	out.TrialExpiresAt = row.TrialExpiresAt
	out.PriceCents = row.PriceCents
	if !row.FetchedAt.IsZero() {
		ft := row.FetchedAt
		out.FetchedAt = &ft
		out.Stale = now.Sub(row.FetchedAt) > staleWarnAfter
		if out.Stale {
			logger.Warnf("[subscription-mirror] 镜像已陈旧 %s（fetched_at=%s），仅提示不升级为锁",
				now.Sub(row.FetchedAt).Round(time.Minute), row.FetchedAt.Format(time.RFC3339))
		}
	}
	return out, nil
}

// IsLocked 闸的判定入口：expired → true（锁）；无数据/未到期 → false。
//
// DB 读失败按 **fail-open**（放行）：镜像读失败是基础设施故障，把整个
// 工作台锁死的代价远大于一次短窗口的漏拦（与模块闸「刷新失败沿用上次
// 快照」同一取舍方向，但这里连上次快照都没有，只能放行 + WARN）。
func (s *SubscriptionMirrorService) IsLocked(ctx context.Context) bool {
	snap, err := s.Snapshot(ctx)
	if err != nil {
		logger.Warnf("[subscription-mirror] 读取镜像失败，订阅闸 fail-open 放行: %v", err)
		return false
	}
	return snap.Locked
}

// ---------------------------------------------------------- 续费下单

// SubscriptionRenewResult 续费下单结果（payment 段透传平台）
type SubscriptionRenewResult struct {
	OrderNo      string                     `json:"order_no"`
	Status       string                     `json:"status"`
	TotalCents   int64                      `json:"total_amount_cents"`
	Items        []platform.ModuleOrderItem `json:"items"`
	PaymentGuide string                     `json:"payment_guide"`
	Duplicate    bool                       `json:"duplicate"`
	// Payment 平台下发的支付参数段（epay：pay_url/params/method；无通道时为空，
	// 前端回落 payment_guide 线下指引）
	Payment         *platform.OrderPayment `json:"payment,omitempty"`
	PlatformEnabled bool                   `json:"platform_enabled"`
}

// Renew 订阅续费下单（代理平台 MerchantCreate type=subscription）。
//
// expired 商户也放行（平台修订 #8：subscription 是恢复通道）。
// 0 元订单平台直通 fulfilled（修订 #9），payment 段为空属正常。
func (s *SubscriptionMirrorService) Renew(ctx context.Context, clientOrderNo, contactName, contactPhone, contactEmail, remark string) (*SubscriptionRenewResult, error) {
	if !config.PlatformEnabled() {
		return nil, ErrPlatformUnavailable
	}
	clientNo := strings.TrimSpace(clientOrderNo)
	if clientNo == "" {
		buf := make([]byte, 8)
		if _, err := rand.Read(buf); err != nil {
			clientNo = fmt.Sprintf("SR%d", time.Now().UnixNano())
		} else {
			clientNo = fmt.Sprintf("SR%d%s", time.Now().UnixMilli(), strings.ToUpper(fmt.Sprintf("%x", buf))[:10])
		}
	}
	pc := platform.NewModuleClient()
	res, err := pc.CreateOrder(ctx, platform.CreateOrderInput{
		ClientOrderNo: clientNo,
		OrderType:     "subscription",
		// 平台端订阅按 (merchant_key, tenant_code) 每租户一行：续费单必须带
		// 本租户 code，平台才知道给哪一行顺延。取不到（off 模式无租户行 /
		// ctx 无绑定）传空 —— 平台对空值走旧行为兜底，不报错。
		TenantCode:   currentTenantCode(ctx),
		ContactName:  contactName,
		ContactPhone: contactPhone,
		ContactEmail: contactEmail,
		Remark:       remark,
		ClientIP:     platform.ClientIPFromContext(ctx),
	})
	if err != nil {
		return nil, bizerr.Wrap(bizerr.CodePlatformUnavail, "续费下单失败（平台不可达）", err)
	}
	// 本地订单镜像落一行（type=subscription）：订单页可见 + sync 刷状态同口径。
	items, _ := json.Marshal(res.Items)
	guide := res.PaymentGuide
	if orderSettled(res.Status) {
		// 0 元续费单平台直通 fulfilled：已顺延，不再挂"联系运营付款"指引
		guide = ""
	} else if strings.TrimSpace(guide) == "" {
		guide = "订单已提交，请联系平台运营完成付款；付款确认后订阅自动顺延。"
	}
	if err := NewModuleCatalogService().order.Upsert(ctx, &model.ModuleOrder{
		OrderNo: res.OrderNo, ClientOrderNo: clientNo,
		OrderType: "subscription", Status: res.Status,
		TotalAmountCents: res.TotalCents, Currency: "CNY",
		Items: string(items), PaymentGuide: guide,
	}); err != nil {
		logger.Warnf("[subscription] 续费订单镜像落库失败（不影响下单结果）: %v", err)
	}
	return &SubscriptionRenewResult{
		OrderNo:         res.OrderNo,
		Status:          res.Status,
		TotalCents:      res.TotalCents,
		Items:           res.Items,
		PaymentGuide:    guide,
		Duplicate:       res.Duplicate,
		Payment:         res.Payment,
		PlatformEnabled: true,
	}, nil
}

// PayStatus 直查平台订单状态（GET /api/module-market/orders/:no/pay-status）。
//
// 比 10 分钟 sync 快：支付确认后前端轮询它拿到 paid/fulfilled。
// 返回平台最新 status 字符串。
func (s *ModuleCatalogService) PayStatus(ctx context.Context, orderNo string) (string, error) {
	if !config.PlatformEnabled() {
		return "", ErrPlatformUnavailable
	}
	remote, err := platform.NewModuleClient().GetOrder(ctx, orderNo)
	if err != nil {
		return "", bizerr.Wrap(bizerr.CodePlatformUnavail, "查询订单状态失败（平台不可达）", err)
	}
	// 平台 Detail 返回 {order:{...},items,logs,payments}；List 平铺 —— 两种形状都兜住
	status := ""
	if ord, ok := remote["order"].(map[string]any); ok {
		status = mapStr(ord, "status")
	}
	if status == "" {
		status = mapStr(remote, "status")
	}
	return status, nil
}
