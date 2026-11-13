package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"hivemtk-user/internal/config"
	bizerr "hivemtk-user/internal/domain/errors"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/platform"
	"hivemtk-user/internal/repository"
)

// LLM 钱包（用户端视图）
//
// 一期只做「查询 + 展示 + 下单充值」，**不做本地扣减**：
// 扣减的权威在平台侧（llm_proxy_service 转发时实时扣），本地再扣一遍就是
// 两本账，对不齐。用户在本地看到的余额是平台下发的镜像，仅用于展示与
// 「余额不足」的快速失败提示。
type LLMWalletService struct {
	repo repository.LLMWalletRepository
}

// NewLLMWalletService 构造
func NewLLMWalletService() *LLMWalletService {
	return &LLMWalletService{repo: repository.NewLLMWalletRepository()}
}

// NewLLMWalletServiceWithRepo 带显式仓储（测试注入）
func NewLLMWalletServiceWithRepo(repo repository.LLMWalletRepository) *LLMWalletService {
	return &LLMWalletService{repo: repo}
}

// WalletView 钱包视图
type WalletView struct {
	MerchantKey     string  `json:"merchant_key"`
	InputRemaining  int64   `json:"input_tokens_remaining"`
	OutputRemaining int64   `json:"output_tokens_remaining"`
	Low             bool    `json:"low"`
	Expired         bool    `json:"expired"`
	Exists          bool    `json:"exists"`
	PlatformEnabled bool    `json:"platform_enabled"`
	ExpiresAt       *string `json:"expires_at,omitempty"`
	UpdatedAt       string  `json:"updated_at,omitempty"`
	// LocalRemainder 本地已知的剩余 token（预扣用；一期恒 0，因为不做本地扣减）
	LocalRemainder int64 `json:"local_remainder"`
}

// Wallet 查询平台余额并落本地镜像
func (s *LLMWalletService) Wallet(ctx context.Context) (*WalletView, error) {
	if !config.PlatformEnabled() {
		// 离线：读本地镜像（最后一次在线查询落下的余额快照）。有镜像 → 可解释
		// 余额 + synced_at（UpdatedAt 透出，前端展示"数据同步于"）；没有镜像
		// → 空钱包（exists=false），而不是报错。
		//
		// **不冒充平台判定**：Low/Expired 恒零值 —— 镜像表没有这两列，离线时
		// 无从判定"是否低量/已过期"，宁可留空也不拿旧快照编一个结论出来
		// （旧快照的 Low=0 不代表现在不低，编出来的判定会误导充值决策）。
		// 本地预检阻断不做（镜像新鲜度无保障，误拦比放行更贵）。
		if mirror, err := s.repo.GetMirror(ctx); err == nil && mirror != nil {
			return &WalletView{
				InputRemaining:  mirror.InputTokensRemaining,
				OutputRemaining: mirror.OutputTokensRemaining,
				Exists:          true,
				PlatformEnabled: false,
				UpdatedAt:       mirror.SyncedAt.Format(time.RFC3339),
			}, nil
		}
		return &WalletView{PlatformEnabled: false}, nil
	}
	// tenant_code 由 ModuleClient.Wallet 从 ctx 解析（口径同 currentTenantCode）：
	// 平台钱包按 (merchant_key, tenant_code) 每租户一行，这里展示的余额就是
	// 本租户自己的那一段。
	pw, err := platform.NewModuleClient().Wallet(ctx)
	if err != nil {
		return nil, bizerr.Wrap(bizerr.CodeModulePlatformUnavail, "查询 LLM 余额失败（平台不可达）", err)
	}
	if pw.Exists {
		_ = s.repo.ApplyMirror(ctx, pw.InputRemaining, pw.OutputRemaining)
	}
	v := &WalletView{
		MerchantKey:     pw.MerchantKey,
		InputRemaining:  pw.InputRemaining,
		OutputRemaining: pw.OutputRemaining,
		Low:             pw.Low,
		Expired:         pw.Expired,
		Exists:          pw.Exists,
		PlatformEnabled: true,
	}
	return v, nil
}

// PlatformUsage 平台垫资用量明细分页（刀2 最小读面；租户作用域由隔离 callback
// 按 ctx 注入）。
//
// 这是 platform_llm_usage_records 的**唯一读端**（此前全仓零读面）：商户在
// 本地看得见"谁在什么场景用了多少、按什么口径折了多少钱"。行结构=既有记录列
// 原样透出（model json tag 即契约），读侧不重算不回填 —— 成本三列是写入时点
// 的整数分快照。
func (s *LLMWalletService) PlatformUsage(ctx context.Context, page, pageSize int) ([]*model.PlatformLLMUsageRecord, int64, error) {
	return repository.NewPlatformLLMUsageRepository().ListUsageRecords(ctx, page, pageSize)
}

// PlanView 套餐视图
type PlanView struct {
	PlanCode              string `json:"plan_code"`
	Name                  string `json:"name"`
	Summary               string `json:"summary"`
	InputTokens           int64  `json:"input_tokens"`
	OutputTokens          int64  `json:"output_tokens"`
	InputPricePer1kCents  int64  `json:"input_price_per_1k_cents"`
	OutputPricePer1kCents int64  `json:"output_price_per_1k_cents"`
	PriceCents            int64  `json:"price_cents"`
	ValidDays             int    `json:"valid_days"`
}

// Plans 可见套餐
func (s *LLMWalletService) Plans(ctx context.Context) ([]PlanView, error) {
	if !config.PlatformEnabled() {
		return []PlanView{}, nil
	}
	rows, err := platform.NewModuleClient().Plans(ctx)
	if err != nil {
		return nil, bizerr.Wrap(bizerr.CodeModulePlatformUnavail, "查询 LLM 套餐失败", err)
	}
	out := make([]PlanView, 0, len(rows))
	for _, r := range rows {
		out = append(out, PlanView(r))
	}
	return out, nil
}

// TxnView 流水视图
type TxnView struct {
	CreatedAt          string `json:"created_at"`
	Reason             string `json:"reason"`
	DeltaInput         int64  `json:"delta_input"`
	DeltaOutput        int64  `json:"delta_output"`
	BalanceInputAfter  int64  `json:"balance_input_after"`
	BalanceOutputAfter int64  `json:"balance_output_after"`
	Note               string `json:"note"`
}

// Txns 钱包流水
func (s *LLMWalletService) Txns(ctx context.Context, page, pageSize int) ([]TxnView, int64, error) {
	if !config.PlatformEnabled() {
		return []TxnView{}, 0, nil
	}
	// tenant_code 口径同 Wallet（ModuleClient 内部从 ctx 解析，本租户流水）
	rows, total, err := platform.NewModuleClient().WalletTxns(ctx, page, pageSize)
	if err != nil {
		return nil, 0, bizerr.Wrap(bizerr.CodeModulePlatformUnavail, "查询 LLM 流水失败", err)
	}
	out := make([]TxnView, 0, len(rows))
	for _, r := range rows {
		v := TxnView{
			Reason:             strOf(r["reason"]),
			Note:               strOf(r["note"]),
			DeltaInput:         int64Of(r["delta_input"]),
			DeltaOutput:        int64Of(r["delta_output"]),
			BalanceInputAfter:  int64Of(r["balance_input_after"]),
			BalanceOutputAfter: int64Of(r["balance_output_after"]),
			CreatedAt:          strOf(r["created_at"]),
		}
		out = append(out, v)
	}
	return out, total, nil
}

// LLMOrderInput LLM 套餐下单入参
type LLMOrderInput struct {
	PlanCode      string `json:"plan_code"`
	ClientOrderNo string `json:"client_order_no"`
	ContactPhone  string `json:"contact_phone"`
	ContactEmail  string `json:"contact_email"`
	Remark        string `json:"remark"`
}

// LLMOrderResult 下单结果
type LLMOrderResult struct {
	OrderNo      string `json:"order_no"`
	Status       string `json:"status"`
	TotalCents   int64  `json:"total_amount_cents"`
	PaymentGuide string `json:"payment_guide"`
	Duplicate    bool   `json:"duplicate"`
	// Payment 平台下发的支付参数段（epay：pay_url/params/method/mock）。
	// 与模块/订阅订单同一形状：有值 → 前端直接开收银台；为空 → 才回落
	// payment_guide 线下指引（平台关渠道 enabled 的零代码回滚路径）。
	Payment *platform.OrderPayment `json:"payment,omitempty"`
}

// CreateOrder 购买 LLM 套餐
//
// 与功能模块订单同一条链路（平台端 order_type=llm → MarkPaid → 钱包入账），
// 区别只是订单行类型不同。**这里不做任何扣费**。
func (s *LLMWalletService) CreateOrder(ctx context.Context, in LLMOrderInput) (*LLMOrderResult, error) {
	if !config.PlatformEnabled() {
		return nil, ErrPlatformUnavailable
	}
	if strings.TrimSpace(in.PlanCode) == "" {
		return nil, bizerr.New(bizerr.CodeParamInvalid, "请选择套餐")
	}
	// 联系方式不再前置强拦（与模块下单同口径，003 T018）：在线支付不需要先加好友，
	// 是否有 payment 段要等平台返回才知道。线下回落路径才要求留联系方式，
	// 判定放在平台响应之后（见下方 res.Payment == nil 分支）。
	clientNo := strings.TrimSpace(in.ClientOrderNo)
	if clientNo == "" {
		clientNo = newClientOrderNo()
	}
	res, err := platform.NewModuleClient().CreateOrder(ctx, platform.CreateOrderInput{
		ClientOrderNo: clientNo,
		OrderType:     "llm",
		PlanCode:      in.PlanCode,
		ContactPhone:  in.ContactPhone,
		ContactEmail:  in.ContactEmail,
		Remark:        in.Remark,
		// 平台端 LLM 订单按 (merchant_key, tenant_code) 归属每租户一行：
		// 本套餐入的是**本租户**的钱包。取不到租户归属时为空（omitempty 不上行），
		// 平台按旧行为兜底。
		TenantCode: currentTenantCode(ctx),
		// 买家终端 IP（mapi clientip，口径同模块/续费单）。
		ClientIP: platform.ClientIPFromContext(ctx),
	})
	if err != nil {
		return nil, bizerr.Wrap(bizerr.CodeModulePlatformUnavail, "提交订单失败（平台不可达）", err)
	}
	// 平台未下发 payment 段且订单仍待支付 = 走线下路径，此时才要求联系方式：
	// 没有联系人，运营无法完成线下收款，订单只能悬着。
	// 0 元单平台直通 fulfilled（修订 #9）没有"等付款"环节，一并放行。
	// 用同一 client_order_no 重试会命中平台幂等键，不会重复下单。
	if res.Payment == nil && !orderSettled(res.Status) &&
		strings.TrimSpace(in.ContactPhone) == "" && strings.TrimSpace(in.ContactEmail) == "" {
		return nil, ErrNoContactForPayment
	}
	guide := res.PaymentGuide
	if orderSettled(res.Status) {
		// 已入账的单不挂"联系运营完成付款"指引（口径同模块单）
		guide = ""
	} else if strings.TrimSpace(guide) == "" {
		if g, gerr := platform.NewModuleClient().PaymentGuide(ctx); gerr == nil {
			guide = g
		} else {
			guide = "订单已提交，请按付款指引完成付款；付款确认后余额自动入账。"
		}
	}
	// 本地订单镜像（与订阅续费同口径）：订单页可见、可「去支付」重开收银台。
	// 不落镜像时用户关掉收银台就再也找不到这笔单，只能重新下单。
	items, _ := json.Marshal(res.Items)
	if err := NewModuleCatalogService().order.Upsert(ctx, &model.ModuleOrder{
		OrderNo: res.OrderNo, ClientOrderNo: clientNo,
		OrderType: "llm", Status: res.Status,
		TotalAmountCents: res.TotalCents, Currency: "CNY",
		Items: string(items), PaymentGuide: guide,
	}); err != nil {
		logger.Warnf("[llm] 套餐订单镜像落库失败（不影响下单结果）: %v", err)
	}
	return &LLMOrderResult{
		OrderNo: res.OrderNo, Status: res.Status,
		TotalCents: res.TotalCents, PaymentGuide: guide,
		Duplicate: res.Duplicate, Payment: res.Payment,
	}, nil
}

// IsPlatformLLMProviderAvailable 平台是否下发了可用的 provider（用于前端提示）
func (s *LLMWalletService) IsPlatformLLMProviderAvailable(ctx context.Context) bool {
	if !config.PlatformEnabled() {
		return false
	}
	rows, err := platform.NewModuleClient().Providers(ctx)
	return err == nil && len(rows) > 0
}

// PlatformProviders 平台下发的 provider 元信息（无 Key）
func (s *LLMWalletService) PlatformProviders(ctx context.Context) ([]platform.PlatformLLMProvider, error) {
	if !config.PlatformEnabled() {
		return nil, ErrPlatformUnavailable
	}
	rows, err := platform.NewModuleClient().Providers(ctx)
	if err != nil {
		return nil, bizerr.Wrap(bizerr.CodeModulePlatformUnavail, "拉取平台模型失败", err)
	}
	return rows, nil
}

var (
	_ = errors.Is
	_ = model.TenantModuleActive
	_ = json.Marshal
)

func strOf(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func int64Of(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	}
	return 0
}
