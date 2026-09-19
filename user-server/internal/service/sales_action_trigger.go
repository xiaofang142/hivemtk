package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// SalesActionTrigger 销售动作触发器
// 把 AI 谈单 / 跟进完成 / 订单创建 三个核心事件
// 自动分发到 标签 / 旅程 / 跟进 / 订单 / 销售事件统计 五个下游组件
type SalesActionTrigger struct {
	mu sync.Mutex

	tagger       *AITagger
	journey      *CustomerJourneyService
	followup     *FollowUpService
	extractor    *OrderIntentExtractor
	stats        *SalesEventStatsService
	draftService *OrderDraftService

	defaultOwnerID string

	history []TriggerRecord
}

// TriggerRecord 触发记录
type TriggerRecord struct {
	EventType  string          `json:"event_type"`
	CustomerID string          `json:"customer_id"`
	Actions    []TriggerAction `json:"actions"`
	OccurredAt time.Time       `json:"occurred_at"`
	Metadata   map[string]any  `json:"metadata,omitempty"`
}

// TriggerAction 单个触发动作
type TriggerAction struct {
	Action     string `json:"action"`
	Target     string `json:"target"`
	Confidence int    `json:"confidence"`
	Result     string `json:"result"`
	Message    string `json:"message,omitempty"`
}

// TriggerConfig 触发器配置
type TriggerConfig struct {
	DefaultOwnerID string
}

// NewSalesActionTrigger 创建销售动作触发器
func NewSalesActionTrigger(
	tagger *AITagger,
	journey *CustomerJourneyService,
	followup *FollowUpService,
	extractor *OrderIntentExtractor,
	stats *SalesEventStatsService,
	cfg *TriggerConfig,
) *SalesActionTrigger {
	if cfg == nil {
		cfg = &TriggerConfig{DefaultOwnerID: "system"}
	}
	return &SalesActionTrigger{
		tagger:         tagger,
		journey:        journey,
		followup:       followup,
		extractor:      extractor,
		stats:          stats,
		defaultOwnerID: cfg.DefaultOwnerID,
	}
}

// SetDraftService 注入订单草稿服务（-11）
func (t *SalesActionTrigger) SetDraftService(ctx context.Context, svc *OrderDraftService) {
	t.draftService = svc
}

// TriggerAfterSales AI 谈单响应后触发（核心入口）
// 商业产品级业务流：
//  1. 自动打标签（行为 / 兴趣 / 阶段）
//  2. 自动推进客户旅程（基于意图）
//  3. 自动提取订单意向（从客户消息 + AI 回复）
//  4. 自动安排跟进（基于阶段）
//  5. 记录到销售事件统计（DB 权威）
func (t *SalesActionTrigger) TriggerAfterSales(ctx context.Context, customerID, ownerID string, resp *SalesResponse) *TriggerRecord {
	if customerID == "" || resp == nil {
		return nil
	}
	if ownerID == "" {
		ownerID = t.defaultOwnerID
	}
	rec := &TriggerRecord{
		EventType:  "sales_response",
		CustomerID: customerID,
		Actions:    make([]TriggerAction, 0, 8),
		OccurredAt: time.Now(),
	}

	if t.tagger != nil {
		tags := t.tagger.TagFromSalesResponse(ctx, customerID, resp)
		for _, tag := range tags {
			rec.Actions = append(rec.Actions, TriggerAction{
				Action:     "tag_applied",
				Target:     tag.Tag,
				Confidence: int(tag.Confidence * 100),
				Result:     "ok",
				Message:    fmt.Sprintf("source=%s category=%s", tag.Source, tag.Category),
			})
		}
		if len(tags) == 0 {
			rec.Actions = append(rec.Actions, TriggerAction{
				Action: "tag_applied", Result: "skip", Message: "无标签可打",
			})
		}
	}

	if t.journey != nil {
		newStage := t.advanceJourneyByIntent(ctx, customerID, resp)
		rec.Actions = append(rec.Actions, TriggerAction{
			Action: "stage_advanced", Target: string(newStage), Result: "ok",
			Message: fmt.Sprintf("intent=%s", safeIntent(resp)),
		})
	}

	if t.extractor != nil {
		// 提取文本的拼法与 OrderDraftService.CreateDraftsFromSalesResponse 同源
		// （draftExtractionText），两处各抄一份的话"AI 谈单产出草稿"就有两个口径。
		intents := t.extractor.ExtractFromText(ctx, customerID, draftExtractionText(resp))
		for _, in := range intents {
			rec.Actions = append(rec.Actions, TriggerAction{
				Action:     "order_intent_extracted",
				Target:     in.ProductName,
				Confidence: int(in.Confidence * 100),
				Result:     "ok",
				Message:    fmt.Sprintf("amount=%.2f qty=%d", in.TotalAmount, in.Quantity),
			})
			if t.draftService != nil {
				draft := t.draftService.CreateFromIntent(ctx, &in, ownerID)
				if draft != nil {
					rec.Actions = append(rec.Actions, TriggerAction{
						Action:     "order_draft_created",
						Target:     draft.ID,
						Confidence: int(draft.Confidence * 100),
						Result:     "ok",
						Message: fmt.Sprintf("product=%s amount=%.2f expires_at=%s",
							draft.ProductName, draft.TotalAmount, draft.ExpiresAt.Format("2006-01-02")),
					})
				}
			}
		}
		if len(intents) == 0 {
			rec.Actions = append(rec.Actions, TriggerAction{
				Action: "order_intent_extracted", Result: "skip", Message: "无产品/价格信号",
			})
		}
	}

	if t.followup != nil {
		shouldSchedule := false
		scheduleType := ReminderFirstContact
		scheduleIn := 1 * time.Hour
		priority := PriorityNormal

		if resp.TransferredToHuman {
			shouldSchedule = true
			scheduleType = ReminderFirstContact
			scheduleIn = 30 * time.Minute
			priority = PriorityUrgent
		} else if resp.Intent != nil {
			switch resp.Intent.IntentType {
			case IntentPurchase:
				shouldSchedule = true
				scheduleType = ReminderQuoteFollowup
				scheduleIn = 1 * time.Hour
				priority = PriorityHigh
			case IntentPriceInquiry, IntentAskProduct:
				shouldSchedule = true
				scheduleType = ReminderQuoteFollowup
				scheduleIn = 2 * time.Hour
				priority = PriorityHigh
			case IntentObjectionPrice:
				shouldSchedule = true
				scheduleType = ReminderFirstContact
				scheduleIn = 4 * time.Hour
				priority = PriorityHigh
			}
		}

		if shouldSchedule {
			r, err := t.followup.Schedule(ctx, customerID, ownerID, scheduleType, scheduleIn, &ScheduleOptions{
				Title:       "AI 触发跟进: " + string(scheduleType),
				Description: "AI 识别客户" + safeIntent(resp) + "后自动安排",
				Priority:    priority,
				AutoHandle:  false,
			})
			if err == nil && r != nil {
				rec.Actions = append(rec.Actions, TriggerAction{
					Action: "followup_scheduled", Target: r.ID, Result: "ok",
					Message: fmt.Sprintf("type=%s due_in=%v", scheduleType, scheduleIn),
				})
			}
		} else {
			rec.Actions = append(rec.Actions, TriggerAction{
				Action: "followup_scheduled", Result: "skip", Message: "当前意图无需新跟进",
			})
		}
	}

	if t.stats != nil {
		t.stats.RecordAIDeal(ctx, AIDealEvent{
			CustomerID:  customerID,
			OwnerID:     ownerID,
			Intent:      safeIntent(resp),
			Replied:     resp.Reply != "" && !resp.TransferredToHuman,
			Transferred: resp.TransferredToHuman,
			CostTokens:  resp.CostTokens,
			LatencyMs:   resp.LatencyMs,
			OccurredAt:  time.Now(),
		})
		rec.Actions = append(rec.Actions, TriggerAction{
			Action: "dashboard_recorded", Target: "ai_deal", Result: "ok",
		})
	}

	t.mu.Lock()
	t.history = append(t.history, *rec)
	if len(t.history) > 1000 {
		t.history = t.history[len(t.history)-1000:]
	}
	t.mu.Unlock()

	return rec
}

func (t *SalesActionTrigger) advanceJourneyByIntent(ctx context.Context, customerID string, resp *SalesResponse) JourneyStage {
	if resp == nil {
		return ""
	}
	state := t.journey.GetState(ctx, customerID)
	current := state.CurrentStage
	if current == "" {
		current = StageStranger
	}
	intent := ""
	if resp.Intent != nil {
		intent = resp.Intent.IntentType
	}

	var target JourneyStage
	switch intent {
	case IntentPurchase:
		if current == StageStranger || current == StageLead {
			target = StageInterested
		} else if current == StageContact {
			target = StageQuoted
		} else {
			target = current
		}
	case IntentPriceInquiry, IntentAskProduct, IntentObjectionPrice:
		switch current {
		case StageStranger:
			target = StageLead
		case StageLead, StageContact:
			target = StageInterested
		default:
			target = current
		}
	case IntentGreeting, IntentSocial:
		if current == StageStranger {
			target = StageLead
		} else {
			target = current
		}
	case IntentChurn, IntentComplaint:
		target = current
	default:
		target = current
	}

	if target == current {
		return current
	}

	_, err := t.journey.Transition(ctx, customerID, target, "ai_chat", "ai",
		"AI 意图自动推进: "+intent, map[string]any{
			"intent":      intent,
			"confidence":  safeConf(resp),
			"transferred": resp.TransferredToHuman,
		})
	if err != nil {
		return current
	}
	return target
}

// TriggerAfterFollowUp 跟进完成时触发
// 商业产品级业务流：
//
//	销售在跟进待办里点击"已完成"
//	  → 记录互动到客户旅程
//	  → 若客户表示"已购买" → 推进到"成交" + 触发售后 SOP
//	  → 若客户表示"无意向" → 推进到"流失"
//	  → 否则 → 推进到下一阶段（按销售操作）
//	  → 记录到销售仪表盘
func (t *SalesActionTrigger) TriggerAfterFollowUp(ctx context.Context, reminderID, customerID, ownerID, result string) *TriggerRecord {
	rec := &TriggerRecord{
		EventType:  "followup_completed",
		CustomerID: customerID,
		Actions:    make([]TriggerAction, 0, 4),
		OccurredAt: time.Now(),
	}

	if t.journey != nil {
		t.journey.Touch(ctx, customerID, "followup")
		target := t.advanceJourneyByFollowUpResult(ctx, customerID, result)
		rec.Actions = append(rec.Actions, TriggerAction{
			Action: "stage_advanced", Target: string(target), Result: "ok",
			Message: "followup result: " + result,
		})
	}

	if t.stats != nil {
		t.stats.RecordFollowUp(ctx, FollowUpEvent{
			CustomerID: customerID,
			OwnerID:    ownerID,
			Channel:    "manual",
			IsAI:       false,
			Result:     result,
			OccurredAt: time.Now(),
		})
		rec.Actions = append(rec.Actions, TriggerAction{
			Action: "dashboard_recorded", Target: "followup", Result: "ok",
		})
	}

	if result == "converted" && t.followup != nil {
		r, err := t.followup.Schedule(ctx, customerID, ownerID, ReminderAfterSaleCare, 7*24*time.Hour, &ScheduleOptions{
			Title:       "售后回访",
			Description: "成交 7 天后自动安排",
			Priority:    PriorityNormal,
			AutoHandle:  true,
		})
		if err == nil && r != nil {
			rec.Actions = append(rec.Actions, TriggerAction{
				Action: "followup_scheduled", Target: r.ID, Result: "ok",
				Message: "售后回访自动安排",
			})
		}
	}

	if result == "converted" && t.journey != nil {
		_, _ = t.journey.Transition(ctx, customerID, StageWon, "followup", ownerID,
			"销售跟进成交", nil)
	}

	if result == "converted" && t.stats != nil {
		amount, productName := t.inferOrderFromJourney(ctx, customerID)
		t.stats.RecordOrder(ctx, OrderEvent{
			OrderID:     "followup-" + reminderID,
			CustomerID:  customerID,
			OwnerID:     ownerID,
			Amount:      amount,
			ProductName: productName,
			IsAIHandled: false,
			OrderedAt:   time.Now(),
		})
		rec.Actions = append(rec.Actions, TriggerAction{
			Action: "order_recorded", Target: "from_followup", Result: "ok",
			Message: fmt.Sprintf("amount=%.2f product=%s", amount, productName),
		})
	}

	t.mu.Lock()
	t.history = append(t.history, *rec)
	if len(t.history) > 1000 {
		t.history = t.history[len(t.history)-1000:]
	}
	t.mu.Unlock()

	return rec
}

func (t *SalesActionTrigger) advanceJourneyByFollowUpResult(ctx context.Context, customerID, result string) JourneyStage {
	state := t.journey.GetState(ctx, customerID)
	current := state.CurrentStage
	if current == "" {
		current = StageStranger
	}
	var target JourneyStage
	switch result {
	case "converted":
		target = StageWon
	case "lost", "rejected":
		target = StageLost
	case "no_reply", "objection":
		target = current
	default:
		target = current
	}
	if target != current {
		_, _ = t.journey.Transition(ctx, customerID, target, "followup", "sales",
			"跟进结果驱动: "+result, map[string]any{"result": result})
	}
	return target
}

// TriggerAfterOrder 订单创建后触发
// 商业产品级业务流：
//
//	订单创建 → 客户旅程推到"成交"
//	        → 触发售后 SOP（7 天后回访）
//	        → 记录到销售仪表盘
//	        → 重置 RFM 分数
//
// isAIHandled（可选）：true=AI 独立成单（销售未介入）；false/未传=人工成单
func (t *SalesActionTrigger) TriggerAfterOrder(ctx context.Context, orderID, customerID, ownerID string, amount float64, productName string, isAIHandled ...bool) *TriggerRecord {
	aiHandled := false
	if len(isAIHandled) > 0 {
		aiHandled = isAIHandled[0]
	}
	rec := &TriggerRecord{
		EventType:  "order_created",
		CustomerID: customerID,
		Actions:    make([]TriggerAction, 0, 4),
		OccurredAt: time.Now(),
	}

	if t.journey != nil {
		_, _ = t.journey.Transition(ctx, customerID, StageWon, "order", ownerID,
			"订单创建自动推进: "+productName, map[string]any{
				"order_id": orderID, "amount": amount, "ai_handled": aiHandled,
			})
		rec.Actions = append(rec.Actions, TriggerAction{
			Action: "stage_advanced", Target: string(StageWon), Result: "ok",
		})
	}

	if t.followup != nil {
		r, err := t.followup.Schedule(ctx, customerID, ownerID, ReminderAfterSaleCare, 7*24*time.Hour, &ScheduleOptions{
			Title:       "售后回访: " + productName,
			Description: "订单 " + orderID + " 创建后自动安排",
			Priority:    PriorityNormal,
			AutoHandle:  true,
		})
		if err == nil && r != nil {
			rec.Actions = append(rec.Actions, TriggerAction{
				Action: "followup_scheduled", Target: r.ID, Result: "ok",
			})
		}
	}

	if t.stats != nil {
		t.stats.RecordOrder(ctx, OrderEvent{
			OrderID:     orderID,
			CustomerID:  customerID,
			OwnerID:     ownerID,
			Amount:      amount,
			ProductName: productName,
			IsAIHandled: aiHandled,
			OrderedAt:   time.Now(),
		})
		rec.Actions = append(rec.Actions, TriggerAction{
			Action: "dashboard_recorded", Target: "order", Result: "ok",
		})
	}

	t.mu.Lock()
	t.history = append(t.history, *rec)
	if len(t.history) > 1000 {
		t.history = t.history[len(t.history)-1000:]
	}
	t.mu.Unlock()

	return rec
}

// GetHistory 获取触发历史（用于审计 + 测试）
func (t *SalesActionTrigger) GetHistory(ctx context.Context, customerID string, limit int) []TriggerRecord {
	t.mu.Lock()
	defer t.mu.Unlock()
	hist := make([]TriggerRecord, 0)
	for i := len(t.history) - 1; i >= 0; i-- {
		if customerID == "" || t.history[i].CustomerID == customerID {
			hist = append(hist, t.history[i])
			if limit > 0 && len(hist) >= limit {
				break
			}
		}
	}
	return hist
}

func (t *SalesActionTrigger) inferOrderFromJourney(ctx context.Context, customerID string) (float64, string) {
	if t.journey == nil {
		return 0, ""
	}
	state := t.journey.GetState(ctx, customerID)
	amount := 0.0
	product := "未指定产品"
	for i := len(state.StageHistory) - 1; i >= 0; i-- {
		ev := state.StageHistory[i]
		if ev.Metadata != nil {
			if v, ok := ev.Metadata["amount"].(float64); ok && v > 0 {
				amount = v
			}
			if v, ok := ev.Metadata["product_name"].(string); ok && v != "" {
				product = v
			}
		}
	}
	return amount, product
}

func safeIntent(resp *SalesResponse) string {
	if resp == nil || resp.Intent == nil {
		return "unknown"
	}
	if resp.Intent.IntentName != "" {
		return resp.Intent.IntentName
	}
	return resp.Intent.IntentType
}

func safeConf(resp *SalesResponse) float64 {
	if resp == nil || resp.Intent == nil {
		return 0
	}
	return resp.Intent.Confidence
}

func init() {
	_ = strings.TrimSpace
}
