package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
)

// ModuleSnapshot 平台下发的模块快照（GET /merchant-api/modules/sync）
type ModuleSnapshot struct {
	MerchantKey string             `json:"merchant_key"`
	ActiveKeys  []string           `json:"active_keys"`
	MenuKeys    []string           `json:"menu_keys"`
	Catalog     []ModuleCatalogRow `json:"catalog"`
	// Subscription 订阅段（003 T021 · ADR-M8 #5）：用户端据此写镜像并本地锁/放行。
	Subscription *SubscriptionSnapshot `json:"subscription,omitempty"`
	SyncedAt     string                `json:"synced_at"`
}

// SubscriptionSnapshot 平台下发的订阅段（status/current_period_end/trial_expires_at/price 快照）
type SubscriptionSnapshot struct {
	Status           string `json:"status"`                       // trial | active | expired
	CurrentPeriodEnd string `json:"current_period_end,omitempty"` // RFC3339 UTC
	TrialExpiresAt   string `json:"trial_expires_at,omitempty"`   // RFC3339 UTC
	PriceCents       int64  `json:"price_cents"`
}

// ModuleCatalogRow 目录里的一行
type ModuleCatalogRow struct {
	ModuleKey    string            `json:"module_key"`
	Name         string            `json:"name"`
	Category     string            `json:"category"`
	PriceCents   int64             `json:"price_cents"`
	BillingCycle string            `json:"billing_cycle"`
	Icon         string            `json:"icon"`
	Summary      string            `json:"summary"`
	Description  string            `json:"description"`
	SortOrder    int               `json:"sort_order"`
	DependsOn    []string          `json:"depends_on"`
	Options      []ModuleOptionRow `json:"options"`
	MenuKeys     []string          `json:"menu_keys"`
	Owned        bool              `json:"owned"`
	OwnedStatus  string            `json:"owned_status,omitempty"`
	// OwnedSource 开通来源（平台下发）。空值时用户端按模块分类回落，
	// 不猜成"用户购买" —— 猜错的后果见 service.sourceOf 的注释。
	OwnedSource string `json:"owned_source,omitempty"`
}

// ModuleOptionRow 子选项
type ModuleOptionRow struct {
	OptionKey       string `json:"option_key"`
	OptionName      string `json:"option_name"`
	PriceDeltaCents int64  `json:"price_delta_cents"`
	Enabled         bool   `json:"enabled"`
}

// ModuleMarketRow 商户市场里的一个模块（GET /merchant-api/module-market）
type ModuleMarketRow struct {
	Modules []ModuleCatalogRow `json:"modules"`
}

// ModuleOrderItem 订单行
type ModuleOrderItem struct {
	ItemType       string `json:"item_type"`
	ItemKey        string `json:"item_key"`
	ItemName       string `json:"item_name"`
	UnitPriceCents int64  `json:"unit_price_cents"`
	Quantity       int64  `json:"quantity"`
	AmountCents    int64  `json:"amount_cents"`
	Snapshot       string `json:"snapshot"`
}

// OrderPayment 平台下单响应里的支付参数段（epay：pay_url/params/method/mock）
type OrderPayment struct {
	GatewayURL string            `json:"gateway_url"`
	PayURL     string            `json:"pay_url"`
	Params     map[string]string `json:"params"`
	Method     string            `json:"method"` // GET=跳转, POST=表单
	Mock       bool              `json:"mock"`
}

// ModuleOrderResult 下单结果
type ModuleOrderResult struct {
	OrderNo      string            `json:"order_no"`
	Status       string            `json:"status"`
	TotalCents   int64             `json:"total_amount_cents"`
	Items        []ModuleOrderItem `json:"items"`
	PaymentGuide string            `json:"payment_guide"`
	Duplicate    bool              `json:"duplicate"`
	// Payment 支付参数段（003 T018）：epay 通道就绪时平台下发；否则为空 →
	// 用户端回落线下付款指引（平台关渠道 enabled 的零代码回滚路径）。
	Payment *OrderPayment `json:"payment,omitempty"`
}

// PlatformLLMWallet 平台 LLM 余额（商户视角）
type PlatformLLMWallet struct {
	MerchantKey     string `json:"merchant_key"`
	InputRemaining  int64  `json:"input_tokens_remaining"`
	OutputRemaining int64  `json:"output_tokens_remaining"`
	Low             bool   `json:"low"`
	Expired         bool   `json:"expired"`
	Exists          bool   `json:"exists"`
}

// PlatformLLMPlan 平台 LLM 套餐
type PlatformLLMPlan struct {
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

// PlatformLLMProvider 平台下发的 provider 元信息（**不含 API Key**）
type PlatformLLMProvider struct {
	ProviderName string `json:"provider_name"`
	DisplayName  string `json:"display_name"`
	Model        string `json:"model"`
	APIType      string `json:"api_type"`
	Vendor       string `json:"vendor"`
	PlanCode     string `json:"plan_code"`
	Source       string `json:"source"`
}

// TenantRegistrationPayload 开户登记上报
type TenantRegistrationPayload struct {
	MerchantKey      string `json:"merchant_key"`
	InstallID        string `json:"install_id"`
	TenantCode       string `json:"tenant_code"`
	CompanyName      string `json:"company_name"`
	LegalPerson      string `json:"legal_person"`
	Phone            string `json:"phone"`
	Email            string `json:"email"`
	LicenseNo        string `json:"license_no"`
	Address          string `json:"address"`
	RegMode          string `json:"reg_mode"`
	AgreementVersion string `json:"agreement_version"`
	RegulatoryAck    bool   `json:"regulatory_ack"`

	// 003 T010 静默合规采集（UA 存 sha256 hex；平台端 tenant_registrations 同步落列）
	InitIP                string `json:"init_ip,omitempty"`
	InitUA                string `json:"init_ua,omitempty"`
	InitDeviceFingerprint string `json:"init_device_fingerprint,omitempty"`
}

// ModuleClient 功能模块 / 订单 / LLM 计费 的商户端客户端
type ModuleClient struct {
	client *Client
}

// NewModuleClient 构造模块客户端
func NewModuleClient() *ModuleClient {
	key := osGetenv("PLATFORM_MERCHANT_KEY")
	if key == "" {
		key = GetMerchantKey()
	}
	if key == "" {
		key = "local-dev-merchant"
	}
	return &ModuleClient{client: NewPlatformClient(key)}
}

type clientIPCtxKey struct{}

// WithClientIP 把本次请求的终端 IP 放进 ctx，供下单/取支付参数的调用链透传给平台。
func WithClientIP(ctx context.Context, ip string) context.Context {
	if strings.TrimSpace(ip) == "" {
		return ctx
	}
	return context.WithValue(ctx, clientIPCtxKey{}, strings.TrimSpace(ip))
}

// ClientIPFromContext 读终端 IP；没有（后台任务/off 模式）返回空串。
func ClientIPFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	s, _ := ctx.Value(clientIPCtxKey{}).(string)
	return strings.TrimSpace(s)
}

// do 执行一次平台请求并解出 data
func (c *ModuleClient) do(method, path string, req any, out any) error {
	return c.doWithContext(context.Background(), method, path, req, out)
}

// doWithContext 同 do，但把调用方 ctx 传播到 HTTP 请求（取消/超时生效）。
func (c *ModuleClient) doWithContext(ctx context.Context, method, path string, req any, out any) error {
	var env struct {
		Code int             `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	if err := c.client.DoContext(ctx, method, path, req, &env); err != nil {
		return err
	}
	if out == nil || len(env.Data) == 0 {
		return nil
	}
	return json.Unmarshal(env.Data, out)
}

// SyncModules 拉取模块下发快照。
//
// tenantCode 为当前租户短码：平台端订阅已按 (merchant_key, tenant_code) 每租户
// 一段下发，不带 code 平台不知道返回哪一段。空串 = 取不到租户归属（后台任务
// 无绑定 / off 模式无租户行），此时请求照发（?tenant_code=）、平台按旧行为
// 兜底 —— 取不到 code 绝不允许报错。
func (c *ModuleClient) SyncModules(ctx context.Context, tenantCode string) (*ModuleSnapshot, error) {
	var snap ModuleSnapshot
	path := "/merchant-api/modules/sync?tenant_code=" + url.QueryEscape(tenantCode)
	if err := c.doWithContext(ctx, "GET", path, nil, &snap); err != nil {
		return nil, err
	}
	return &snap, nil
}

// Market 拉取模块市场
//
// tenantCode 为当前租户短码：SaaS 下所有租户共享 merchant_key，平台端的
// "已拥有"判定不带租户维度就会出现"租户 A 买过的模块在租户 B 的市场里
// 也显示已拥有"。空串 = 取不到租户归属（后台任务无绑定 / off 模式），
// 平台按商户级行兜底。
func (c *ModuleClient) Market(ctx context.Context, tenantCode string) (*ModuleMarketRow, error) {
	var m ModuleMarketRow
	path := "/merchant-api/module-market?tenant_code=" + url.QueryEscape(tenantCode)
	if err := c.do("GET", path, nil, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// PaymentGuide 拉取付款指引
func (c *ModuleClient) PaymentGuide(ctx context.Context) (string, error) {
	var out struct {
		PaymentGuide string `json:"payment_guide"`
	}
	if err := c.do("GET", "/merchant-api/module-market/payment-guide", nil, &out); err != nil {
		return "", err
	}
	return out.PaymentGuide, nil
}

// CreateOrderInput 下单入参
type CreateOrderInput struct {
	ClientOrderNo string                     `json:"client_order_no"`
	OrderType     string                     `json:"order_type"`
	ModuleKeys    []string                   `json:"module_keys"`
	PlanCode      string                     `json:"plan_code"`
	Options       map[string]map[string]bool `json:"options"`
	ContactName   string                     `json:"contact_name"`
	ContactPhone  string                     `json:"contact_phone"`
	ContactEmail  string                     `json:"contact_email"`
	Remark        string                     `json:"remark"`
	// ClientIP 发起下单的终端 IP。平台侧 mapi（易支付 API 下单口）把它当
	// clientip 必填项；这份值在请求体里**被 HMAC 签名覆盖**，伪造即验签失败。
	// 取不到（后台任务无请求上下文）时空串不上行，平台回落自己看到的来源。
	ClientIP string `json:"clientip,omitempty"`
	// TenantCode 当前租户短码（平台端按 (merchant_key, tenant_code) 归属订单行）。
	// 订阅类订单（order_type=subscription，用户端续费 Renew）与 LLM 套餐订单
	// （order_type=llm）必带；模块等其余类型暂不强填（空值 omitempty 不上行，
	// 平台走旧行为兜底）。取不到租户归属时为空，平台对空值同样按旧行为兜底，
	// 不报错。
	TenantCode string `json:"tenant_code,omitempty"`
}

// CreateOrder 提交订单
func (c *ModuleClient) CreateOrder(ctx context.Context, in CreateOrderInput) (*ModuleOrderResult, error) {
	var out ModuleOrderResult
	if err := c.do("POST", "/merchant-api/orders", in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateOrderPayment 给**已下的**待支付订单重新申请在线支付参数（订单页「去支付」）。
//
// 与 CreateOrder 是同一条平台腿（attachEpayPayment），只是把"下单"换成"这单还要付"。
// 没有它，用户关掉收银台标签页之后订单页只剩付款指引（联系运营），一张本可在线
// 完成的订单只能退回线下。平台侧按 out_trade_no 现算参数，不产生第二张订单；
// 非待支付 / 0 元 / 通道未配置会以 HTTP 非 200 返回，由调用方映射提示。
func (c *ModuleClient) CreateOrderPayment(ctx context.Context, orderNo string) (*ModuleOrderResult, error) {
	var out ModuleOrderResult
	path := "/merchant-api/orders/" + url.PathEscape(orderNo) + "/pay"
	// body 带上买家终端 IP（签名覆盖）：取支付参数和下单是同一条 mapi 腿，
	// 同样要 clientip。没有它平台只能用自己看到的来源（用户端服务器 IP）。
	body := struct {
		ClientIP string `json:"clientip,omitempty"`
	}{ClientIP: ClientIPFromContext(ctx)}
	if err := c.do("POST", path, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListOrders 商户订单列表
func (c *ModuleClient) ListOrders(ctx context.Context, status string, page, pageSize int) ([]map[string]any, int64, error) {
	q := url.Values{}
	if status != "" {
		q.Set("status", status)
	}
	q.Set("page", fmt.Sprintf("%d", page))
	q.Set("page_size", fmt.Sprintf("%d", pageSize))
	var out struct {
		List  []map[string]any `json:"list"`
		Total int64            `json:"total"`
	}
	if err := c.do("GET", "/merchant-api/orders?"+q.Encode(), nil, &out); err != nil {
		return nil, 0, err
	}
	return out.List, out.Total, nil
}

// GetOrder 商户订单详情
func (c *ModuleClient) GetOrder(ctx context.Context, orderNo string) (map[string]any, error) {
	var out map[string]any
	if err := c.do("GET", "/merchant-api/orders/"+url.PathEscape(orderNo), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ---------------------------------------------------------- LLM 计费

// Wallet 拉取平台 LLM 余额。
//
// 带 ?tenant_code=<当前租户 code>：平台端钱包已按 (merchant_key, tenant_code)
// 每租户一行，不带 code 平台不知道展示哪一段。空串（取不到租户归属）照发
// （?tenant_code=）、平台按旧行为兜底 —— 取不到 code 绝不允许报错。
func (c *ModuleClient) Wallet(ctx context.Context) (*PlatformLLMWallet, error) {
	var w PlatformLLMWallet
	path := "/merchant-api/llm/wallet?tenant_code=" + url.QueryEscape(TenantCodeFromContext(ctx))
	if err := c.do("GET", path, nil, &w); err != nil {
		return nil, err
	}
	return &w, nil
}

// WalletTxns 拉取钱包流水（tenant_code 口径同 Wallet）
func (c *ModuleClient) WalletTxns(ctx context.Context, page, pageSize int) ([]map[string]any, int64, error) {
	q := url.Values{}
	q.Set("page", fmt.Sprintf("%d", page))
	q.Set("page_size", fmt.Sprintf("%d", pageSize))
	q.Set("tenant_code", TenantCodeFromContext(ctx))
	var out struct {
		List  []map[string]any `json:"list"`
		Total int64            `json:"total"`
	}
	if err := c.do("GET", "/merchant-api/llm/wallet/txns?"+q.Encode(), nil, &out); err != nil {
		return nil, 0, err
	}
	return out.List, out.Total, nil
}

// Plans 拉取可见套餐
func (c *ModuleClient) Plans(ctx context.Context) ([]PlatformLLMPlan, error) {
	var out struct {
		List []PlatformLLMPlan `json:"list"`
	}
	if err := c.do("GET", "/merchant-api/llm/plans", nil, &out); err != nil {
		return nil, err
	}
	return out.List, nil
}

// Providers 拉取下发的平台 provider 元信息（无 Key）
func (c *ModuleClient) Providers(ctx context.Context) ([]PlatformLLMProvider, error) {
	var out struct {
		List []PlatformLLMProvider `json:"list"`
	}
	if err := c.do("GET", "/merchant-api/llm/providers", nil, &out); err != nil {
		return nil, err
	}
	return out.List, nil
}

// ---------------------------------------------------------- 开户登记

// RegulatoryNotice 拉取监管告知（未登录可取）
func (c *ModuleClient) RegulatoryNotice(ctx context.Context) (notice, agreementVersion string, required []string, err error) {
	var out struct {
		Notice           string   `json:"notice"`
		AgreementVersion string   `json:"agreement_version"`
		RequiredFields   []string `json:"required_fields"`
	}
	if err2 := c.do("GET", "/merchant-api/tenant/notice", nil, &out); err2 != nil {
		return "", "", nil, err2
	}
	return out.Notice, out.AgreementVersion, out.RequiredFields, nil
}

// RegisterTenant 提交开户登记
func (c *ModuleClient) RegisterTenant(ctx context.Context, p TenantRegistrationPayload) (string, error) {
	var out struct {
		ID string `json:"id"`
	}
	if err := c.do("POST", "/merchant-api/tenant/register", p, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

// osGetenv 便于测试替换
var osGetenv = func(k string) string { return strings.TrimSpace(os.Getenv(k)) }
