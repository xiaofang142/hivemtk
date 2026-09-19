package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// roundMoney 金额四舍五入到分，抑制 float64 乘法二进制误差（存储层 NUMERIC(12,2)）
func roundMoney(v float64) float64 {
	return math.Round(v*100) / 100
}

// DraftStatus 草稿状态
type DraftStatus string

const (
	DraftStatusPending   DraftStatus = "pending"
	DraftStatusConfirmed DraftStatus = "confirmed"
	DraftStatusCancelled DraftStatus = "cancelled"
	DraftStatusExpired   DraftStatus = "expired"
)

// OrderDraft 订单草稿
type OrderDraft struct {
	ID           string         `json:"id"`
	CustomerID   string         `json:"customer_id"`
	OneID        string         `json:"one_id,omitempty"`
	OwnerID      string         `json:"owner_id"`
	ProductName  string         `json:"product_name"`
	ProductID    string         `json:"product_id,omitempty"`
	Category     string         `json:"category"`
	Quantity     int            `json:"quantity"`
	UnitPrice    float64        `json:"unit_price"`
	TotalAmount  float64        `json:"total_amount"`
	Confidence   float64        `json:"confidence"`
	Source       string         `json:"source"`
	SourceText   string         `json:"source_text"`
	IntentID     string         `json:"intent_id,omitempty"`
	Status       DraftStatus    `json:"status"`
	OrderID      string         `json:"order_id,omitempty"`
	Note         string         `json:"note,omitempty"`
	CancelReason string         `json:"cancel_reason,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	ExpiresAt    time.Time      `json:"expires_at"`
	ConfirmedAt  *time.Time     `json:"confirmed_at,omitempty"`
	CancelledAt  *time.Time     `json:"cancelled_at,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

// CreateDraftRequest 手动创建草稿请求
type CreateDraftRequest struct {
	CustomerID  string  `json:"customer_id"`
	OneID       string  `json:"one_id,omitempty"`
	OwnerID     string  `json:"owner_id"`
	ProductName string  `json:"product_name"`
	ProductID   string  `json:"product_id,omitempty"`
	Category    string  `json:"category,omitempty"`
	Quantity    int     `json:"quantity"`
	UnitPrice   float64 `json:"unit_price"`
	Note        string  `json:"note,omitempty"`
}

// DraftUpdates 草稿更新字段
type DraftUpdates struct {
	ProductName *string  `json:"product_name,omitempty"`
	Quantity    *int     `json:"quantity,omitempty"`
	UnitPrice   *float64 `json:"unit_price,omitempty"`
	Note        *string  `json:"note,omitempty"`
}

// DraftConfirmResult 草稿确认结果
type DraftConfirmResult struct {
	Draft         *OrderDraft  `json:"draft"`
	OrderID       string       `json:"order_id"`
	Order         *orderRecord `json:"order,omitempty"`
	StageAdvanced string       `json:"stage_advanced"`
	FollowUpID    string       `json:"followup_id,omitempty"`
}

type orderRecord struct {
	ID          string    `json:"id"`
	AccountID   string    `json:"account_id"`
	ProductName string    `json:"product_name"`
	Quantity    int       `json:"quantity"`
	UnitPrice   float64   `json:"unit_price"`
	TotalAmount float64   `json:"total_amount"`
	Status      string    `json:"status"`
	Source      string    `json:"source"`
	CreatedAt   time.Time `json:"created_at"`
}

// OrderDraftService 订单草稿服务
//
// 状态存放交给 draftStore（内存一副 / DB 一副，见 order_draft_store.go）。
// 本文件因此只保留业务判据（去重规则、置信度加成、状态机、事件口径），
// 不再持有 map —— 这正是 T-P2-01 的落点：业务规则与"数据活多久"分开。
type OrderDraftService struct {
	// mu 只保护 CreateFromIntent 的"查待确认草稿 → 合并或新建"这一段。
	//
	// 原来这里是一把盖住全部读写的大锁 + 三个 map；现在收窄到唯一一处**跨两次存储
	// 调用**的复合操作：不锁住它就会有两个并发意向各自查不到对方、各建一份草稿，
	// 销售工作台于是看到两条重复项（存量代码即如此，见改动前的 RLock/Lock 序列）。
	// DB 那一副另有部分唯一索引 uq_order_draft_pending 兜跨进程的同一情形。
	mu sync.Mutex

	store draftStore

	orderService *OrderService
	journey      *CustomerJourneyService
	stats        *SalesEventStatsService
	followup     *FollowUpService
	trigger      *SalesActionTrigger

	scriptConversionHook func(ctx context.Context, oneID, conversationID, outcome string)

	defaultExpiry time.Duration
}

func (s *OrderDraftService) SetScriptConversionHook(hook func(ctx context.Context, oneID, conversationID, outcome string)) {
	s.scriptConversionHook = hook
}

// OrderDraftConfig 草稿服务配置
type OrderDraftConfig struct {
	DefaultExpiry time.Duration
}

// NewOrderDraftService 创建订单草稿服务（内存态底座）。
//
// 默认仍是内存：本卡的 AC③ 要求"现有调用方零改动通过编译"，而存量 25 个用例把
// 返回的 *OrderDraft 当活对象用（改完字段直接断言列表/过期结果）。要 durable 底座
// 请显式走 NewOrderDraftServiceWithDB —— 两条路的可观察差异由契约测试钉住。
func NewOrderDraftService(cfg *OrderDraftConfig) *OrderDraftService {
	return newOrderDraftService(cfg, newMemoryDraftStore())
}

// NewOrderDraftServiceWithDB 用持久化底座创建服务（T-P2-01 的交付物）。
//
// 生产装配点当前**不存在**（整条销售草稿竖今天没有构造点，见 T-P2-06），所以本函数
// 今天只有测试在调 —— 这是事实，不是遗漏：底座先落地、装配另开卡，是为了不在
// "连对象都没人 new"的代码上叠加"重启不丢"这种可对外宣称的能力。
//
// db 为 nil 时退回内存底座并出声告警（不静默、不 panic）。
func NewOrderDraftServiceWithDB(cfg *OrderDraftConfig, db *gorm.DB) *OrderDraftService {
	return newOrderDraftService(cfg, newDraftStoreForDB(db))
}

// NewOrderDraftServiceWithRepo 用指定仓库创建（注入 mock / 复用测试库句柄时用）。
func NewOrderDraftServiceWithRepo(cfg *OrderDraftConfig, repo repository.OrderDraftRepository) *OrderDraftService {
	return newOrderDraftService(cfg, newDBDraftStore(repo))
}

// NewOrderDraftServiceShadow 用影子底座创建服务（FF_LTC_ORDER_DRAFT_DB=shadow，见 T-P2-06）。
//
// 读走内存、写镜像到 DB：对调用方零行为变化，但库里会攒出真实行，
// 于是"换 DB 底座前后读到的东西对不对得上"这件事有对照可看。
// 注意 Durable() 在这一档是 false（权威那份仍是内存）。
func NewOrderDraftServiceShadow(cfg *OrderDraftConfig, db *gorm.DB) *OrderDraftService {
	return newOrderDraftService(cfg, newShadowDraftStoreForDB(db))
}

// StoreKind 当前用的是哪副底座："memory" / "db" / "shadow"。
//
// 给观察端点用：只有 Durable() 一个布尔的话，"影子期库里已经攒了 30 行"与
// "真落库了"这两种状态回显完全一样，而它们的运维含义相反。
func (s *OrderDraftService) StoreKind() string {
	switch s.store.(type) {
	case *dbDraftStore:
		return DraftStoreKindDB
	case *shadowDraftStore:
		return DraftStoreKindShadow
	default:
		return DraftStoreKindMemory
	}
}

// OrderDraftMirrorStatus 影子底座的对照读数（非影子底座时不会用到）。
type OrderDraftMirrorStatus struct {
	// Available 镜像句柄在不在。false = 影子档但没拿到 DB（已退回纯内存），
	// 与"句柄在、只是还没写进东西"是两回事。
	Available bool
	// RowCounts 库侧各状态行数。读不动时为 nil，配合 ReadError 看。
	RowCounts map[string]int64
	// ReadError 库侧计数读取失败的原因（空 = 读到了）。
	ReadError string
	// Failures / LastError 镜像写失败的累计数与最后一条原因。
	Failures  int64
	LastError string
}

// MirrorStatus 读影子底座的对照状态。第二个返回值为 false 表示当前底座不是影子。
func (s *OrderDraftService) MirrorStatus(ctx context.Context) (*OrderDraftMirrorStatus, bool) {
	shadow, ok := s.store.(*shadowDraftStore)
	if !ok {
		return nil, false
	}
	st := &OrderDraftMirrorStatus{Available: shadow.mirrorAvailable()}
	st.Failures, st.LastError = shadow.MirrorStats()
	if !st.Available {
		return st, true
	}
	counts, err := shadow.mirrorCounts(ctx)
	if err != nil {
		st.ReadError = err.Error()
		return st, true
	}
	st.RowCounts = counts
	return st, true
}

func newOrderDraftService(cfg *OrderDraftConfig, store draftStore) *OrderDraftService {
	if cfg == nil {
		cfg = &OrderDraftConfig{}
	}
	if cfg.DefaultExpiry == 0 {
		cfg.DefaultExpiry = 7 * 24 * time.Hour
	}
	return &OrderDraftService{
		store:         store,
		defaultExpiry: cfg.DefaultExpiry,

		scriptConversionHook: defaultScriptConversionHook,
	}
}

// Durable 报告草稿是否跨进程重启保留（观测端点/启动日志用）。
//
// 只有布尔是不够的：一个服务"接口全通、数据重启就没"和"已经落库"从外部看一模一样，
// 必须有一个地方能问出来。
func (s *OrderDraftService) Durable() bool { return s.store != nil && s.store.durable() }

// DraftStatusCounts 各状态草稿条数（"是否还在无界增长"的可见入口）。
func (s *OrderDraftService) DraftStatusCounts(ctx context.Context) (map[string]int64, error) {
	return s.store.statusCounts(ctx)
}

func defaultScriptConversionHook(ctx context.Context, oneID, conversationID, outcome string) {
	if scriptABSvc == nil {
		return
	}
	svc := getScriptABService()
	if svc == nil {
		return
	}
	_ = svc.RecordConversion(ctx, oneID, conversationID, outcome)
}

// SetOrderService 注入订单服务
func (s *OrderDraftService) SetOrderService(ctx context.Context, svc *OrderService) {
	s.orderService = svc
}

// SetJourney 注入客户旅程服务
func (s *OrderDraftService) SetJourney(ctx context.Context, j *CustomerJourneyService) {
	s.journey = j
}

// SetStats 注入销售事件统计服务（H2：替代原 SalesDashboard）
func (s *OrderDraftService) SetStats(ctx context.Context, svc *SalesEventStatsService) {
	s.stats = svc
}

// SetFollowUp 注入跟进服务
func (s *OrderDraftService) SetFollowUp(ctx context.Context, f *FollowUpService) {
	s.followup = f
}

// SetTrigger 注入销售动作触发器
func (s *OrderDraftService) SetTrigger(ctx context.Context, t *SalesActionTrigger) {
	s.trigger = t
}

// CreateFromIntent 从订单意向自动生成草稿（-11 核心入口）
// 商业产品级业务流：AI 谈单时提取到"光子嫩肤 3 次 2280 元"→
//  1. 自动生成草稿（pending 状态）
//  2. 通知销售（在"待确认草稿"列表里出现）
//  3. 仪表盘记录 draft_created 事件
//  4. 销售点"确认"即可生成正式订单
//
// 去重：同一客户同一产品的 pending 草稿不会重复创建（数量累加到现有草稿）
func (s *OrderDraftService) CreateFromIntent(ctx context.Context, intent *OrderIntent, ownerID string) *OrderDraft {
	if intent == nil || intent.CustomerID == "" || intent.ProductName == "" {
		return nil
	}
	if ownerID == "" {
		ownerID = "system"
	}

	// 整段"查→合并或新建"在一把锁里：见结构体上 mu 的注释。
	s.mu.Lock()
	defer s.mu.Unlock()

	mergeInto := func(id string) (*OrderDraft, bool) {
		existing, applied, err := s.store.mutateIfPending(ctx, id, func(d *OrderDraft) {
			if d.Metadata == nil {
				d.Metadata = make(map[string]any)
			}
			if intent.Quantity > 0 {
				d.Quantity += intent.Quantity
				d.TotalAmount = roundMoney(d.UnitPrice * float64(d.Quantity))
			}
			if intent.UnitPrice > 0 {
				d.UnitPrice = intent.UnitPrice
				d.TotalAmount = roundMoney(d.UnitPrice * float64(d.Quantity))
			}
			if intent.Confidence > d.Confidence {
				d.Confidence = intent.Confidence
			}
			d.UpdatedAt = time.Now()
			d.Metadata["last_intent_id"] = intent.RawText
		})
		if err != nil {
			logger.Errorf("[order-draft] 合并意向到草稿 %s 失败 ⇒ 本次 intent 不记入草稿（不伪记为已合并）：%v", id, err)
			return nil, false
		}
		return existing, applied
	}

	candidates, err := s.findPendingDraftByProduct(ctx, intent.CustomerID, intent.ProductName)
	if err != nil {
		logger.Errorf("[order-draft] 去重查询失败 ⇒ 本次 intent 不建草稿（宁缺勿重）：%v", err)
		return nil
	}
	if candidates != nil {
		if merged, ok := mergeInto(candidates.ID); ok {
			return merged
		}
		// 合并落败 = 那条草稿在读取后已被确认/取消 ⇒ 它不再是"待确认"的那一个，
		// 本次意向按新建处理（原实现无并发对手，所以从未暴露这一支）。
	}

	now := time.Now()
	quantity := intent.Quantity
	if quantity <= 0 {
		quantity = 1
	}
	unitPrice := intent.UnitPrice
	totalAmount := roundMoney(unitPrice * float64(quantity))

	conf := intent.Confidence
	if unitPrice > 0 {
		conf += 0.1
	}
	if quantity > 1 {
		conf += 0.05
	}
	if conf > 0.99 {
		conf = 0.99
	}

	draft := &OrderDraft{
		ID:          generateDraftID(),
		CustomerID:  intent.CustomerID,
		OneID:       intent.OneID,
		OwnerID:     ownerID,
		ProductName: intent.ProductName,
		ProductID:   intent.ProductID,
		Category:    intent.Category,
		Quantity:    quantity,
		UnitPrice:   unitPrice,
		TotalAmount: totalAmount,
		Confidence:  conf,
		Source:      "ai_chat",
		SourceText:  intent.RawText,
		IntentID:    intent.RawText,
		Status:      DraftStatusPending,
		CreatedAt:   now,
		UpdatedAt:   now,
		ExpiresAt:   now.Add(s.defaultExpiry),
		Metadata: map[string]any{
			"category":   intent.Category,
			"raw_intent": intent.RawText,
		},
	}

	if err := s.store.put(ctx, draft); err != nil {
		if errors.Is(err, repository.ErrOrderDraftPendingConflict) {
			// 部分唯一索引挡下了"同客户同产品两条 pending"（跨进程才会走到这里）。
			// 冲突行必然满足模糊匹配，重查一次按合并处理即可。
			if other, e := s.findPendingDraftByProduct(ctx, intent.CustomerID, intent.ProductName); e == nil && other != nil {
				if merged, ok := mergeInto(other.ID); ok {
					return merged
				}
			}
			logger.Errorf("[order-draft] %s 的 pending 草稿冲突且重查未命中 ⇒ 本次 intent 丢弃", intent.ProductName)
			return nil
		}
		logger.Errorf("[order-draft] 草稿落库失败 ⇒ 本次意向未记为草稿（不静默当成成功）：%v", err)
		return nil
	}

	if s.stats != nil {
		s.stats.RecordOrderDraft(ctx, OrderDraftEvent{
			DraftID:     draft.ID,
			CustomerID:  draft.CustomerID,
			OwnerID:     draft.OwnerID,
			ProductName: draft.ProductName,
			Amount:      draft.TotalAmount,
			Action:      "created",
			Source:      "ai_chat",
			Confidence:  draft.Confidence,
			OccurredAt:  now,
		})
	}
	return draft
}

// CreateDraftsFromSalesResponse 从一条 AI 谈单响应里提取订单意向并建草稿
// （T-P2-06 的生产入口：装配之前，order_drafts 表没有任何写入方）。
//
// 三点口径，都是照着 SalesActionTrigger 里那段既有逻辑抄的，不另起一套判据：
//   - 提取文本 = 客户已说出口的需求/预算 + AI 的回复（见 draftExtractionText）；
//     刻意**不**扫 RAG 命中块与话术模板 —— 那是"库里写了什么价"而不是"这单谈成了什么"，
//     扫进来会让一条无关的知识库条目凭空造出一张客户没提过的草稿；
//   - 每个意向走 CreateFromIntent（含同客户同产品去重合并、置信度加成、7 天到期），
//     所以本方法不会绕过 T-P2-01 立的那两套并发保护；
//   - 逐条意向独立成败：某一条落库失败只丢那一条，其余照常，返回值里只含有草稿的。
//
// extractor 为 nil 时不做任何事并返回 nil：宁可不建草稿，也不要在装配漏注入时
// 悄悄换一套提取规则（正则的 product 名单是会改的，改在两份代码里就是两个口径）。
func (s *OrderDraftService) CreateDraftsFromSalesResponse(
	ctx context.Context,
	extractor *OrderIntentExtractor,
	customerID, ownerID string,
	resp *SalesResponse,
) []*OrderDraft {
	if extractor == nil || resp == nil || customerID == "" {
		return nil
	}
	intents := extractor.ExtractFromText(ctx, customerID, draftExtractionText(resp))
	if len(intents) == 0 {
		return nil
	}
	drafts := make([]*OrderDraft, 0, len(intents))
	for i := range intents {
		if d := s.CreateFromIntent(ctx, &intents[i], ownerID); d != nil {
			drafts = append(drafts, d)
		}
	}
	return drafts
}

// draftExtractionText 拼出订单意向提取要读的文本（唯一真源：SalesActionTrigger 与本方法都用它）。
func draftExtractionText(resp *SalesResponse) string {
	text := resp.Reply
	if resp.Memory != nil {
		if resp.Memory.Demand != "" {
			text = resp.Memory.Demand + " " + text
		}
		if resp.Memory.Budget != "" {
			text = resp.Memory.Budget + " " + text
		}
	}
	return text
}

// CreateManual 销售手动创建草稿
func (s *OrderDraftService) CreateManual(ctx context.Context, req *CreateDraftRequest) (*OrderDraft, error) {
	if req == nil {
		return nil, errors.New("请求不能为空")
	}
	if req.CustomerID == "" {
		return nil, errors.New("客户 ID 不能为空")
	}
	if req.ProductName == "" {
		return nil, errors.New("产品名称不能为空")
	}
	if req.Quantity <= 0 {
		req.Quantity = 1
	}
	if req.UnitPrice < 0 {
		return nil, errors.New("单价不能为负数")
	}
	if req.OwnerID == "" {
		req.OwnerID = "manual"
	}

	now := time.Now()
	draft := &OrderDraft{
		ID:          generateDraftID(),
		CustomerID:  req.CustomerID,
		OneID:       req.OneID,
		OwnerID:     req.OwnerID,
		ProductName: req.ProductName,
		ProductID:   req.ProductID,
		Category:    req.Category,
		Quantity:    req.Quantity,
		UnitPrice:   req.UnitPrice,
		TotalAmount: roundMoney(req.UnitPrice * float64(req.Quantity)),
		Confidence:  1.0,
		Source:      "manual",
		Status:      DraftStatusPending,
		Note:        req.Note,
		CreatedAt:   now,
		UpdatedAt:   now,
		ExpiresAt:   now.Add(s.defaultExpiry),
		Metadata:    make(map[string]any),
	}

	if err := s.store.put(ctx, draft); err != nil {
		logger.Errorf("[order-draft] 手动草稿落库失败 ⇒ 明确回错给调用方（不返回一份没存下的草稿）：%v", err)
		return nil, fmt.Errorf("草稿保存失败: %w", err)
	}

	if s.stats != nil {
		s.stats.RecordOrderDraft(ctx, OrderDraftEvent{
			DraftID:     draft.ID,
			CustomerID:  draft.CustomerID,
			OwnerID:     draft.OwnerID,
			ProductName: draft.ProductName,
			Amount:      draft.TotalAmount,
			Action:      "created",
			Source:      "manual",
			Confidence:  1.0,
			OccurredAt:  now,
		})
	}
	return draft, nil
}

// Confirm 销售一键确认草稿 → 创建正式订单（-11 核心入口）
// 商业产品级业务流：销售在"待确认草稿"列表里点"确认" → 4 件事自动发生：
//  1. 创建正式订单（如果 orderService 注入）
//  2. 客户旅程推到"成交"（如果 journey 注入）
//  3. 销售事件统计记录订单（如果 stats 注入）
//  4. 自动安排 7 天后售后回访（如果 followup 注入）
//
// 返回：DraftConfirmResult（订单 ID + 阶段 + 跟进 ID）便于前端展示
func (s *OrderDraftService) Confirm(ctx context.Context, draftID, confirmedBy string) (*DraftConfirmResult, error) {
	cur, err := s.store.get(ctx, draftID)
	if err != nil {
		return nil, fmt.Errorf("读取草稿 %s 失败: %w", draftID, err)
	}
	if cur == nil {
		return nil, fmt.Errorf("草稿 %s 不存在", draftID)
	}
	if cur.Status != DraftStatusPending {
		return nil, fmt.Errorf("草稿状态为 %s，不可确认", cur.Status)
	}
	now := time.Now()
	if now.After(cur.ExpiresAt) {
		// 与实现前一致：就地刷成 expired 后报错，**不**记 expired 统计事件
		// （ExpireOverdue 会记）。两条路口径不同是既有事实，改它会动仪表盘历史数字，
		// 不属本卡范围 —— 已在卡面执行结果里登记为待决项。
		if _, applied, e := s.store.mutateIfPending(ctx, draftID, func(d *OrderDraft) {
			d.Status = DraftStatusExpired
			d.UpdatedAt = now
		}); e != nil {
			logger.Errorf("[order-draft] 过期草稿 %s 状态回写失败：%v", draftID, e)
		} else if !applied {
			logger.Warnf("[order-draft] 草稿 %s 在判过期与回写之间被并发改动 ⇒ 仍按已过期拒绝本次确认", draftID)
		}
		return nil, fmt.Errorf("草稿已过期")
	}
	// 判 pending 与翻 confirmed 是一次加锁读里的一个动作（见 repository.MutatePending）：
	// 分成两步 = 两个销售同时点确认时会各建一张订单。
	draft, applied, err := s.store.mutateIfPending(ctx, draftID, func(d *OrderDraft) {
		d.Status = DraftStatusConfirmed
		d.ConfirmedAt = &now
		d.UpdatedAt = now
	})
	if err != nil {
		return nil, fmt.Errorf("确认草稿 %s 失败: %w", draftID, err)
	}
	if !applied {
		fresh, _ := s.store.get(ctx, draftID)
		if fresh == nil {
			return nil, fmt.Errorf("草稿 %s 不存在", draftID)
		}
		return nil, fmt.Errorf("草稿状态为 %s，不可确认", fresh.Status)
	}
	if confirmedBy == "" {
		confirmedBy = draft.OwnerID
	}
	orderID := ""

	result := &DraftConfirmResult{
		Draft:         draft,
		StageAdvanced: string(StageWon),
	}

	if s.orderService != nil {
		order, err := s.createOrderFromDraft(ctx, draft)
		if err != nil {
			return result, fmt.Errorf("创建订单失败: %w", err)
		}
		if order != nil {
			orderID = order.ID
			result.OrderID = orderID
			result.Order = &orderRecord{
				ID:          order.ID,
				AccountID:   order.AccountID,
				ProductName: draft.ProductName,
				Quantity:    draft.Quantity,
				UnitPrice:   draft.UnitPrice,
				TotalAmount: draft.TotalAmount,
				Status:      "pending",
				Source:      "draft",
				CreatedAt:   now,
			}
		}
	} else {
		orderID = generateTempOrderID()
		result.OrderID = orderID
	}

	if orderID != "" {
		draft.OrderID = orderID
		if err := s.store.put(ctx, draft); err != nil {
			// 订单已经建出来了，这里只能出声而不能回滚：草稿侧缺 order_id 关联是可修的，
			// 把已成功建单的回答成"失败"会让销售再点一次，那才是真事故。
			logger.Errorf("[order-draft] 订单 %s 已创建但草稿 %s 的关联未落库 ⇒ 需人工对齐：%v", orderID, draftID, err)
		}
	}

	if s.journey != nil {
		_, _ = s.journey.Transition(ctx, draft.CustomerID, StageWon, "draft_confirm", confirmedBy,
			"草稿确认自动成单: "+draft.ProductName, map[string]any{
				"draft_id":     draft.ID,
				"order_id":     orderID,
				"amount":       draft.TotalAmount,
				"confirmed_by": confirmedBy,
			})
	}

	if s.stats != nil {
		s.stats.RecordOrder(ctx, OrderEvent{
			OrderID:     orderID,
			CustomerID:  draft.CustomerID,
			OwnerID:     draft.OwnerID,
			Amount:      draft.TotalAmount,
			ProductName: draft.ProductName,
			IsAIHandled: draft.Source == "ai_chat",
			OrderedAt:   now,
		})
		s.stats.RecordOrderDraft(ctx, OrderDraftEvent{
			DraftID:     draft.ID,
			CustomerID:  draft.CustomerID,
			OwnerID:     draft.OwnerID,
			ProductName: draft.ProductName,
			Amount:      draft.TotalAmount,
			Action:      "confirmed",
			Source:      draft.Source,
			Confidence:  draft.Confidence,
			OccurredAt:  now,
		})
	}

	if s.followup != nil {
		r, _ := s.followup.Schedule(ctx, draft.CustomerID, draft.OwnerID,
			ReminderAfterSaleCare, 7*24*time.Hour, &ScheduleOptions{
				Title:       "售后回访: " + draft.ProductName,
				Description: fmt.Sprintf("草稿 %s 确认后自动安排（订单 %s）", draft.ID, orderID),
				Priority:    PriorityNormal,
				AutoHandle:  true,
			})
		if r != nil {
			result.FollowUpID = r.ID
		}
	}

	if s.scriptConversionHook != nil && draft.OneID != "" {
		oneID := draft.OneID
		convID, _ := draft.Metadata["conversation_id"].(string)
		hook := s.scriptConversionHook
		go func() {
			defer func() { _ = recover() }()
			dctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			hook(dctx, oneID, convID, "purchase")
		}()
	}

	return result, nil
}

// Cancel 销售取消草稿
// 商业产品级：客户改变主意、价格谈崩、重复草稿 → 销售主动取消
// 取消时记录原因，便于后续分析"哪些产品/价格/阶段容易被取消"
func (s *OrderDraftService) Cancel(ctx context.Context, draftID, reason, cancelledBy string) error {
	now := time.Now()
	draft, applied, err := s.store.mutateIfPending(ctx, draftID, func(d *OrderDraft) {
		d.Status = DraftStatusCancelled
		d.CancelledAt = &now
		d.UpdatedAt = now
		d.CancelReason = reason
		if cancelledBy != "" {
			if d.Metadata == nil {
				d.Metadata = make(map[string]any)
			}
			d.Metadata["cancelled_by"] = cancelledBy
		}
	})
	if err != nil {
		return fmt.Errorf("取消草稿 %s 失败: %w", draftID, err)
	}
	if !applied {
		cur, e := s.store.get(ctx, draftID)
		if e != nil {
			return fmt.Errorf("读取草稿 %s 失败: %w", draftID, e)
		}
		if cur == nil {
			return fmt.Errorf("草稿 %s 不存在", draftID)
		}
		return fmt.Errorf("草稿状态为 %s，不可取消", cur.Status)
	}

	if s.stats != nil {
		s.stats.RecordOrderDraft(ctx, OrderDraftEvent{
			DraftID:     draft.ID,
			CustomerID:  draft.CustomerID,
			OwnerID:     draft.OwnerID,
			ProductName: draft.ProductName,
			Amount:      draft.TotalAmount,
			Action:      "cancelled",
			Source:      draft.Source,
			Confidence:  draft.Confidence,
			OccurredAt:  now,
		})
	}
	return nil
}

// Edit 草稿编辑（价格/数量/产品名/备注）
// 商业产品级：销售在确认前可能需要修改价格（如客户砍价）或调整数量
func (s *OrderDraftService) Edit(ctx context.Context, draftID string, updates DraftUpdates) error {
	_, applied, err := s.store.mutateIfPending(ctx, draftID, func(d *OrderDraft) {
		if updates.ProductName != nil && *updates.ProductName != "" {
			d.ProductName = *updates.ProductName
		}
		if updates.Quantity != nil && *updates.Quantity > 0 {
			d.Quantity = *updates.Quantity
		}
		if updates.UnitPrice != nil && *updates.UnitPrice >= 0 {
			d.UnitPrice = *updates.UnitPrice
		}
		if updates.Note != nil {
			d.Note = *updates.Note
		}
		d.TotalAmount = d.UnitPrice * float64(d.Quantity)
		d.UpdatedAt = time.Now()
	})
	if err != nil {
		return fmt.Errorf("编辑草稿 %s 失败: %w", draftID, err)
	}
	if applied {
		return nil
	}
	// 落败两种情形分开报：不存在和状态不允许，销售工作台上的提示文案不一样（口径不变）。
	cur, e := s.store.get(ctx, draftID)
	if e != nil {
		return fmt.Errorf("读取草稿 %s 失败: %w", draftID, e)
	}
	if cur == nil {
		return fmt.Errorf("草稿 %s 不存在", draftID)
	}
	return fmt.Errorf("草稿状态为 %s，不可编辑", cur.Status)
}

// GetByID 根据 ID 查询草稿。
//
// 三种结果必须分得开（T-P2-06 ③）：
//   - (draft, nil) 命中；
//   - (nil, nil)   确实没有这条草稿 —— 与仓储口径一致（GetByID 不存在返回 (nil, nil)）；
//   - (nil, err)   读不动（连接/SQL 故障）。
//
// 改签名前它把第三类 logger.Errorf 之后回 nil，于是"库挂了"在调用方看来与
// "草稿不存在"逐字相同：销售工作台上表现为"这条草稿凭空消失了"，而事实是查不了。
func (s *OrderDraftService) GetByID(ctx context.Context, draftID string) (*OrderDraft, error) {
	return s.store.get(ctx, draftID)
}

// ListPending 列出待确认草稿（销售工作台首页）
// 商业产品级：销售每天打开系统，第一眼看到"我有多少待确认草稿"，按优先级排序
//
// 读失败回 error 且列表为 nil，不回空切片：空列表的含义是"今天没有待确认草稿"，
// 那是一句业务结论，拿一次查询故障去支撑它等于让销售停止处理本该处理的单。
func (s *OrderDraftService) ListPending(ctx context.Context, ownerID string, limit int) ([]*OrderDraft, error) {
	return s.store.listPending(ctx, ownerID, time.Now(), limit)
}

// ListByCustomer 列出客户的所有草稿（含历史）。错误口径同 ListPending。
func (s *OrderDraftService) ListByCustomer(ctx context.Context, customerID string) ([]*OrderDraft, error) {
	return s.store.listByCustomer(ctx, customerID)
}

// ListByOwner 列出销售负责的所有草稿。错误口径同 ListPending。
func (s *OrderDraftService) ListByOwner(ctx context.Context, ownerID string) ([]*OrderDraft, error) {
	return s.store.listByOwner(ctx, ownerID)
}

// ExpireOverdue 批量过期超时草稿（由 OrderDraftSweepWorker 定时调用）
// 商业产品级：7 天未确认的草稿自动过期，避免销售工作台堆积无用草稿
//
// 错误一并返回（T-P2-06 ②）：扫描中断时已翻的条数仍然有效，调用方（worker）要把
// "翻了几条"和"为什么停在半路"分开报，只回一个 int 的话这两件事会被读成同一件。
func (s *OrderDraftService) ExpireOverdue(ctx context.Context) (int, error) {
	now := time.Now()
	var event func(*OrderDraft)
	if s.stats != nil {
		stats := s.stats
		event = func(d *OrderDraft) {
			stats.RecordOrderDraft(ctx, OrderDraftEvent{
				DraftID:     d.ID,
				CustomerID:  d.CustomerID,
				OwnerID:     d.OwnerID,
				ProductName: d.ProductName,
				Amount:      d.TotalAmount,
				Action:      "expired",
				Source:      d.Source,
				Confidence:  d.Confidence,
				OccurredAt:  now,
			})
		}
	}
	// 过期是**落库的状态翻转**（内存副同样翻转自己的 map）：只计数不翻状态会让
	// 下一轮扫描重新数到同一批草稿，"无界增长"从内存搬到 DB。
	// 出错时不再就地 logger 一遍：错误已经回给调用方，worker 会带上轮次信息统一出声。
	return s.store.expireOverdue(ctx, now, event)
}

// PurgeTerminal 清理超保留期的终态草稿（cancelled / expired），返回删除行数。
//
// 过期只把 pending 搬进终态，不删行 —— 终态行会永久堆积。保留期内它们是
// "为什么这单没成"的分析材料（CancelReason / stats 事件），过期的则是纯体积，
// 所以留一个有边界的清理入口而不是留一个不断增长的历史表。
//
// retention <= 0 走 defaultDraftRetention（90 天）。失败连同已删行数一起回给调用方：
// 返回 0 会被读成"没有要清的"，而事实可能是"一条都没清掉"。
func (s *OrderDraftService) PurgeTerminal(ctx context.Context, retention time.Duration) (int, error) {
	if retention <= 0 {
		retention = defaultDraftRetention
	}
	return s.store.purgeTerminal(ctx, time.Now().Add(-retention))
}

func (s *OrderDraftService) findPendingDraftByProduct(ctx context.Context, customerID, productName string) (*OrderDraft, error) {
	candidates, err := s.store.pendingByCustomer(ctx, customerID)
	if err != nil {
		return nil, err
	}
	for _, d := range candidates {
		if d.ProductName == productName ||
			strings.Contains(d.ProductName, productName) ||
			strings.Contains(productName, d.ProductName) {
			return d, nil
		}
	}
	return nil, nil
}

func (s *OrderDraftService) createOrderFromDraft(ctx context.Context, draft *OrderDraft) (*orderRecord, error) {
	if s.orderService == nil {
		return nil, errors.New("orderService 未注入")
	}
	priceStr := fmt.Sprintf("%.2f", draft.TotalAmount)
	order, err := s.orderService.CreateOrderFromRequest(ctx, toOrderModel(draft, priceStr))
	if err != nil {
		return nil, err
	}
	if order == nil {
		return nil, errors.New("订单创建返回为空")
	}
	return &orderRecord{
		ID:          order.ID,
		AccountID:   order.AccountID,
		ProductName: draft.ProductName,
		Quantity:    draft.Quantity,
		UnitPrice:   draft.UnitPrice,
		TotalAmount: draft.TotalAmount,
		Status:      "pending",
		Source:      "draft",
		CreatedAt:   time.Now(),
	}, nil
}

var draftCounter int64

func generateDraftID() string {
	n := atomic.AddInt64(&draftCounter, 1)
	return fmt.Sprintf("draft_%d_%d", time.Now().UnixNano(), n)
}

var orderCounter int64

func generateTempOrderID() string {
	n := atomic.AddInt64(&orderCounter, 1)
	return fmt.Sprintf("ord_%d_%d", time.Now().UnixNano(), n)
}
