package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
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
type OrderDraftService struct {
	mu sync.RWMutex

	drafts     map[string]*OrderDraft
	byCustomer map[string][]*OrderDraft
	byOwner    map[string][]*OrderDraft

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

// NewOrderDraftService 创建订单草稿服务
func NewOrderDraftService(cfg *OrderDraftConfig) *OrderDraftService {
	if cfg == nil {
		cfg = &OrderDraftConfig{}
	}
	if cfg.DefaultExpiry == 0 {
		cfg.DefaultExpiry = 7 * 24 * time.Hour
	}
	return &OrderDraftService{
		drafts:        make(map[string]*OrderDraft),
		byCustomer:    make(map[string][]*OrderDraft),
		byOwner:       make(map[string][]*OrderDraft),
		defaultExpiry: cfg.DefaultExpiry,

		scriptConversionHook: defaultScriptConversionHook,
	}
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

	existing := s.findPendingDraftByProduct(ctx, intent.CustomerID, intent.ProductName)
	if existing != nil {
		if intent.Quantity > 0 {
			existing.Quantity += intent.Quantity
			existing.TotalAmount = roundMoney(existing.UnitPrice * float64(existing.Quantity))
		}
		if intent.UnitPrice > 0 {
			existing.UnitPrice = intent.UnitPrice
			existing.TotalAmount = roundMoney(existing.UnitPrice * float64(existing.Quantity))
		}
		if intent.Confidence > existing.Confidence {
			existing.Confidence = intent.Confidence
		}
		existing.UpdatedAt = time.Now()
		existing.Metadata["last_intent_id"] = intent.RawText
		return existing
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

	s.mu.Lock()
	s.drafts[draft.ID] = draft
	s.byCustomer[draft.CustomerID] = append(s.byCustomer[draft.CustomerID], draft)
	s.byOwner[draft.OwnerID] = append(s.byOwner[draft.OwnerID], draft)
	s.mu.Unlock()

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

	s.mu.Lock()
	s.drafts[draft.ID] = draft
	s.byCustomer[draft.CustomerID] = append(s.byCustomer[draft.CustomerID], draft)
	s.byOwner[draft.OwnerID] = append(s.byOwner[draft.OwnerID], draft)
	s.mu.Unlock()

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
	s.mu.Lock()
	draft, ok := s.drafts[draftID]
	if !ok {
		s.mu.Unlock()
		return nil, fmt.Errorf("草稿 %s 不存在", draftID)
	}
	if draft.Status != DraftStatusPending {
		s.mu.Unlock()
		return nil, fmt.Errorf("草稿状态为 %s，不可确认", draft.Status)
	}
	if time.Now().After(draft.ExpiresAt) {
		draft.Status = DraftStatusExpired
		s.mu.Unlock()
		return nil, fmt.Errorf("草稿已过期")
	}
	now := time.Now()
	draft.Status = DraftStatusConfirmed
	draft.ConfirmedAt = &now
	draft.UpdatedAt = now
	if confirmedBy == "" {
		confirmedBy = draft.OwnerID
	}
	orderID := ""
	s.mu.Unlock()

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
			s.mu.Lock()
			draft.OrderID = orderID
			s.mu.Unlock()
		}
	} else {
		orderID = generateTempOrderID()
		result.OrderID = orderID
		s.mu.Lock()
		draft.OrderID = orderID
		s.mu.Unlock()
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
	s.mu.Lock()
	draft, ok := s.drafts[draftID]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("草稿 %s 不存在", draftID)
	}
	if draft.Status != DraftStatusPending {
		s.mu.Unlock()
		return fmt.Errorf("草稿状态为 %s，不可取消", draft.Status)
	}
	now := time.Now()
	draft.Status = DraftStatusCancelled
	draft.CancelledAt = &now
	draft.UpdatedAt = now
	draft.CancelReason = reason
	if cancelledBy != "" {
		draft.Metadata["cancelled_by"] = cancelledBy
	}
	s.mu.Unlock()

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
	s.mu.Lock()
	defer s.mu.Unlock()
	draft, ok := s.drafts[draftID]
	if !ok {
		return fmt.Errorf("草稿 %s 不存在", draftID)
	}
	if draft.Status != DraftStatusPending {
		return fmt.Errorf("草稿状态为 %s，不可编辑", draft.Status)
	}
	if updates.ProductName != nil && *updates.ProductName != "" {
		draft.ProductName = *updates.ProductName
	}
	if updates.Quantity != nil && *updates.Quantity > 0 {
		draft.Quantity = *updates.Quantity
	}
	if updates.UnitPrice != nil && *updates.UnitPrice >= 0 {
		draft.UnitPrice = *updates.UnitPrice
	}
	if updates.Note != nil {
		draft.Note = *updates.Note
	}
	draft.TotalAmount = draft.UnitPrice * float64(draft.Quantity)
	draft.UpdatedAt = time.Now()
	return nil
}

// GetByID 根据 ID 查询草稿
func (s *OrderDraftService) GetByID(ctx context.Context, draftID string) *OrderDraft {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.drafts[draftID]
}

// ListPending 列出待确认草稿（销售工作台首页）
// 商业产品级：销售每天打开系统，第一眼看到"我有多少待确认草稿"，按优先级排序
func (s *OrderDraftService) ListPending(ctx context.Context, ownerID string, limit int) []*OrderDraft {
	s.mu.RLock()
	defer s.mu.RUnlock()
	pending := make([]*OrderDraft, 0)
	now := time.Now()
	for _, d := range s.drafts {
		if d.Status != DraftStatusPending {
			continue
		}
		if ownerID != "" && d.OwnerID != ownerID {
			continue
		}
		if now.After(d.ExpiresAt) {
			continue
		}
		pending = append(pending, d)
	}
	sort.Slice(pending, func(i, j int) bool {
		if pending[i].Confidence != pending[j].Confidence {
			return pending[i].Confidence > pending[j].Confidence
		}
		if pending[i].TotalAmount != pending[j].TotalAmount {
			return pending[i].TotalAmount > pending[j].TotalAmount
		}
		return pending[i].ExpiresAt.Before(pending[j].ExpiresAt)
	})
	if limit > 0 && len(pending) > limit {
		pending = pending[:limit]
	}
	return pending
}

// ListByCustomer 列出客户的所有草稿（含历史）
func (s *OrderDraftService) ListByCustomer(ctx context.Context, customerID string) []*OrderDraft {
	s.mu.RLock()
	defer s.mu.RUnlock()
	drafts := s.byCustomer[customerID]
	out := make([]*OrderDraft, len(drafts))
	copy(out, drafts)
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}

// ListByOwner 列出销售负责的所有草稿
func (s *OrderDraftService) ListByOwner(ctx context.Context, ownerID string) []*OrderDraft {
	s.mu.RLock()
	defer s.mu.RUnlock()
	drafts := s.byOwner[ownerID]
	out := make([]*OrderDraft, len(drafts))
	copy(out, drafts)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Status == DraftStatusPending && out[j].Status != DraftStatusPending {
			return true
		}
		if out[i].Status != DraftStatusPending && out[j].Status == DraftStatusPending {
			return false
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}

// ExpireOverdue 批量过期超时草稿（定期调用）
// 商业产品级：7 天未确认的草稿自动过期，避免销售工作台堆积无用草稿
// 返回被过期的草稿数
func (s *OrderDraftService) ExpireOverdue(ctx context.Context) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	count := 0
	for _, d := range s.drafts {
		if d.Status != DraftStatusPending {
			continue
		}
		if now.After(d.ExpiresAt) {
			d.Status = DraftStatusExpired
			d.UpdatedAt = now
			count++
			if s.stats != nil {
				s.stats.RecordOrderDraft(ctx, OrderDraftEvent{
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
	}
	return count
}

func (s *OrderDraftService) findPendingDraftByProduct(ctx context.Context, customerID, productName string) *OrderDraft {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, d := range s.drafts {
		if d.CustomerID != customerID {
			continue
		}
		if d.Status != DraftStatusPending {
			continue
		}
		if d.ProductName == productName ||
			strings.Contains(d.ProductName, productName) ||
			strings.Contains(productName, d.ProductName) {
			return d
		}
	}
	return nil
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
