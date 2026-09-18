package service

import (
	"context"
	"sort"
	"sync"
	"time"
)

// RFMType RFM 类型
type RFMType string

const (
	RFMTYPEChampion    RFMType = "champion"
	RFMTYPELoyal       RFMType = "loyal"
	RFMTYPEPotential   RFMType = "potential"
	RFMTYPENewbie      RFMType = "newbie"
	RFMTYPEAttention   RFMType = "attention"
	RFMTYPEHibernating RFMType = "hibernating"
	RFMTYPELost        RFMType = "lost"
)

// RFMScore RFM 评分
type RFMScore struct {
	CustomerID  string    `json:"customer_id"`
	Recency     int       `json:"recency"`
	Frequency   int       `json:"frequency"`
	Monetary    float64   `json:"monetary"`
	R           int       `json:"r"`
	F           int       `json:"f"`
	M           int       `json:"m"`
	RFMScore    int       `json:"rfm_score"`
	Segment     RFMType   `json:"segment"`
	LastOrderAt time.Time `json:"last_order_at"`
	ComputedAt  time.Time `json:"computed_at"`
}

// RepurchasePrediction 复购预测
type RepurchasePrediction struct {
	CustomerID     string    `json:"customer_id"`
	Probability    float64   `json:"probability"`
	PredictedDays  int       `json:"predicted_days"`
	Reason         string    `json:"reason"`
	RecommendedSOP string    `json:"recommended_sop"`
	PredictedAt    time.Time `json:"predicted_at"`
}

// RepurchaseEngine 复购引擎
type RepurchaseEngine struct {
	mu      sync.RWMutex
	scores  map[string]*RFMScore
	history map[string][]PurchaseEvent
}

// PurchaseEvent 购买事件
type PurchaseEvent struct {
	OrderID     string    `json:"order_id"`
	CustomerID  string    `json:"customer_id"`
	Amount      float64   `json:"amount"`
	ProductID   string    `json:"product_id"`
	ProductName string    `json:"product_name"`
	Category    string    `json:"category"`
	OrderedAt   time.Time `json:"ordered_at"`
}

// NewRepurchaseEngine 创建复购引擎
func NewRepurchaseEngine() *RepurchaseEngine {
	return &RepurchaseEngine{
		scores:  make(map[string]*RFMScore),
		history: make(map[string][]PurchaseEvent),
	}
}

// RecordPurchase 记录购买
func (e *RepurchaseEngine) RecordPurchase(ctx context.Context, event PurchaseEvent) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.history[event.CustomerID] = append(e.history[event.CustomerID], event)
}

// ComputeRFM 计算 RFM
func (e *RepurchaseEngine) ComputeRFM(ctx context.Context, customerID string) *RFMScore {
	e.mu.RLock()
	defer e.mu.RUnlock()
	events := e.history[customerID]
	if len(events) == 0 {
		return &RFMScore{
			CustomerID: customerID,
			Segment:    RFMTYPENewbie,
			ComputedAt: time.Now(),
		}
	}
	now := time.Now()
	mostRecent := events[0].OrderedAt
	for _, ev := range events {
		if ev.OrderedAt.After(mostRecent) {
			mostRecent = ev.OrderedAt
		}
	}
	recency := int(now.Sub(mostRecent).Hours() / 24)
	frequency := len(events)
	var monetary float64
	for _, ev := range events {
		monetary += ev.Amount
	}
	r := scoreRecency(recency)
	f := scoreFrequency(frequency)
	m := scoreMonetary(monetary)
	score := &RFMScore{
		CustomerID:  customerID,
		Recency:     recency,
		Frequency:   frequency,
		Monetary:    monetary,
		R:           r,
		F:           f,
		M:           m,
		RFMScore:    r*100 + f*10 + m,
		Segment:     e.classifyRFM(ctx, r, f, m),
		LastOrderAt: mostRecent,
		ComputedAt:  now,
	}
	e.scores[customerID] = score
	return score
}

func scoreRecency(days int) int {
	switch {
	case days <= 7:
		return 5
	case days <= 30:
		return 4
	case days <= 60:
		return 3
	case days <= 90:
		return 2
	default:
		return 1
	}
}

func scoreFrequency(times int) int {
	switch {
	case times >= 10:
		return 5
	case times >= 5:
		return 4
	case times >= 3:
		return 3
	case times >= 2:
		return 2
	default:
		return 1
	}
}

func scoreMonetary(amount float64) int {
	switch {
	case amount >= 10000:
		return 5
	case amount >= 5000:
		return 4
	case amount >= 2000:
		return 3
	case amount >= 500:
		return 2
	default:
		return 1
	}
}

func (e *RepurchaseEngine) classifyRFM(ctx context.Context, r, f, m int) RFMType {
	switch {
	case r >= 4 && f >= 4 && m >= 4:
		return RFMTYPEChampion
	case r >= 3 && f >= 3:
		return RFMTYPELoyal
	case f <= 2 && r >= 3:
		return RFMTYPEPotential
	case r >= 4:
		return RFMTYPENewbie
	case r == 2 || r == 3:
		return RFMTYPEAttention
	case r == 1:
		if f >= 1 {
			return RFMTYPEHibernating
		}
		return RFMTYPELost
	default:
		return RFMTYPEHibernating
	}
}

// Predict 预测复购概率
func (e *RepurchaseEngine) Predict(ctx context.Context, customerID string) *RepurchasePrediction {
	rfm := e.ComputeRFM(ctx, customerID)
	probability := 0.0
	var predictedDays int // switch 各分支（含 default）均赋值
	reason := ""
	sop := ""
	switch rfm.Segment {
	case RFMTYPEChampion:
		probability = 0.85
		predictedDays = 14
		reason = "高价值老客，3 月内大概率复购"
		sop = "vip_benefits"
	case RFMTYPELoyal:
		probability = 0.65
		predictedDays = 30
		reason = "忠诚客户，1 月内可能复购"
		sop = "loyalty_reward"
	case RFMTYPEPotential:
		probability = 0.4
		predictedDays = 60
		reason = "潜力客户，2 月内可能转化"
		sop = "education_content"
	case RFMTYPEAttention:
		probability = 0.25
		predictedDays = 90
		reason = "需关注，3 月内可能流失"
		sop = "reactivation"
	case RFMTYPEHibernating:
		probability = 0.1
		predictedDays = 180
		reason = "已沉睡，需强激活"
		sop = "win_back"
	default:
		probability = 0.02
		predictedDays = 365
		reason = "已流失，重新激活成本高"
		sop = "win_back"
	}
	return &RepurchasePrediction{
		CustomerID:     customerID,
		Probability:    probability,
		PredictedDays:  predictedDays,
		Reason:         reason,
		RecommendedSOP: sop,
		PredictedAt:    time.Now(),
	}
}

// ListReactivationCandidates 列出需要激活的客户
// 商业逻辑：3-12 个月未购 + 金额 > 0 的客户均纳入候选
func (e *RepurchaseEngine) ListReactivationCandidates(ctx context.Context, limit int) []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	for cid := range e.history {
		if _, ok := e.scores[cid]; !ok {
			e.computeRFMLocked(ctx, cid)
		}
	}
	candidates := make([]string, 0)
	for cid, score := range e.scores {
		if score.Segment == RFMTYPEHibernating || score.Segment == RFMTYPEAttention || score.Segment == RFMTYPELost {
			candidates = append(candidates, cid)
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		return e.scores[candidates[i]].Monetary > e.scores[candidates[j]].Monetary
	})
	if limit > 0 && len(candidates) > limit {
		candidates = candidates[:limit]
	}
	return candidates
}

func (e *RepurchaseEngine) computeRFMLocked(ctx context.Context, customerID string) *RFMScore {
	events := e.history[customerID]
	if len(events) == 0 {
		return nil
	}
	now := time.Now()
	mostRecent := events[0].OrderedAt
	for _, ev := range events {
		if ev.OrderedAt.After(mostRecent) {
			mostRecent = ev.OrderedAt
		}
	}
	recency := int(now.Sub(mostRecent).Hours() / 24)
	frequency := len(events)
	var monetary float64
	for _, ev := range events {
		monetary += ev.Amount
	}
	r := scoreRecency(recency)
	f := scoreFrequency(frequency)
	m := scoreMonetary(monetary)
	score := &RFMScore{
		CustomerID:  customerID,
		Recency:     recency,
		Frequency:   frequency,
		Monetary:    monetary,
		R:           r,
		F:           f,
		M:           m,
		RFMScore:    r*100 + f*10 + m,
		Segment:     e.classifyRFM(ctx, r, f, m),
		LastOrderAt: mostRecent,
		ComputedAt:  now,
	}
	e.scores[customerID] = score
	return score
}

// GenerateReactivationPlan 生成激活计划（多波次）
type ReactivationWave struct {
	Wave       int       `json:"wave"`
	Channel    string    `json:"channel"`
	SOP        string    `json:"sop"`
	MessageTpl string    `json:"message_tpl"`
	SendAt     time.Time `json:"send_at"`
	WaitDays   int       `json:"wait_days"`
}

// GenerateReactivationPlan 生成多波次激活计划
// 商业逻辑：第 1 波 7 天后（轻触达：问候+福利）→ 14 天后（强激活：限时优惠）→ 30 天后（最后触达）
func (e *RepurchaseEngine) GenerateReactivationPlan(ctx context.Context, customerID string) []ReactivationWave {
	rfm := e.ComputeRFM(ctx, customerID)
	now := time.Now()
	plan := []ReactivationWave{}
	if rfm.Segment == RFMTYPEChampion || rfm.Segment == RFMTYPELoyal {
		return plan
	}
	plan = append(plan, ReactivationWave{
		Wave:       1,
		Channel:    "wechat",
		SOP:        "friendly_check_in",
		MessageTpl: "亲，好久不见，我们有新的产品/服务上线，专属老客户福利见您私聊",
		SendAt:     now.Add(3 * 24 * time.Hour),
		WaitDays:   3,
	})
	plan = append(plan, ReactivationWave{
		Wave:       2,
		Channel:    "wechat",
		SOP:        "limited_offer",
		MessageTpl: "我们为您准备了一个专属优惠，仅限今日，错过不再有",
		SendAt:     now.Add(14 * 24 * time.Hour),
		WaitDays:   7,
	})
	plan = append(plan, ReactivationWave{
		Wave:       3,
		Channel:    "sms",
		SOP:        "final_outreach",
		MessageTpl: "我们仍然惦记您，期待再次服务您",
		SendAt:     now.Add(30 * 24 * time.Hour),
		WaitDays:   16,
	})
	return plan
}

// TriggerJourney 触发旅程（结合客户旅程服务）
func (e *RepurchaseEngine) TriggerJourney(ctx context.Context, customerID string, journey *CustomerJourneyService) error {
	rfm := e.ComputeRFM(ctx, customerID)

	var targetStage JourneyStage
	switch rfm.Segment {
	case RFMTYPEChampion, RFMTYPELoyal:
		targetStage = StageRepurchase
	case RFMTYPEPotential, RFMTYPENewbie:
		targetStage = StageContact
	case RFMTYPEAttention, RFMTYPEHibernating:
		targetStage = StageSleeping
	default:
		targetStage = StageLost
	}
	_, err := journey.Transition(ctx, customerID, targetStage, "rfm_engine", "system", "基于 RFM 自动分层: "+string(rfm.Segment), nil)
	return err
}
