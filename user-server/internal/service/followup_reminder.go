package service

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// ReminderType 提醒类型
type ReminderType string

const (
	ReminderFirstContact  ReminderType = "first_contact"
	ReminderQuoteFollowup ReminderType = "quote_followup"
	ReminderAfterSaleCare ReminderType = "after_sale_care"
	ReminderRepurchase    ReminderType = "repurchase"
	ReminderReactivation  ReminderType = "reactivation"
	ReminderCustom        ReminderType = "custom"
)

// ReminderPriority 优先级
type ReminderPriority int

const (
	PriorityLow ReminderPriority = iota
	PriorityNormal
	PriorityHigh
	PriorityUrgent
)

// Reminder 跟进提醒
type Reminder struct {
	ID          string           `json:"id"`
	CustomerID  string           `json:"customer_id"`
	OneID       string           `json:"one_id"`
	OwnerID     string           `json:"owner_id"`
	Type        ReminderType     `json:"type"`
	Priority    ReminderPriority `json:"priority"`
	Title       string           `json:"title"`
	Description string           `json:"description"`
	DueAt       time.Time        `json:"due_at"`
	CreatedAt   time.Time        `json:"created_at"`
	CompletedAt *time.Time       `json:"completed_at,omitempty"`
	Status      string           `json:"status"`
	SOPName     string           `json:"sop_name,omitempty"`
	AutoHandle  bool             `json:"auto_handle"`
	Channel     string           `json:"channel,omitempty"`
}

// FollowUpService 跟进提醒服务
type FollowUpService struct {
	mu        sync.RWMutex
	reminders map[string]*Reminder
	journey   *CustomerJourneyService
	stats     *SalesEventStatsService
}

// NewFollowUpService 创建跟进服务
func NewFollowUpService(journey *CustomerJourneyService) *FollowUpService {
	return &FollowUpService{
		reminders: make(map[string]*Reminder),
		journey:   journey,
	}
}

// SetStats 注入销售事件统计服务（可选，H2：替代原 SalesDashboard）
// 商业产品级：跟进完成后自动记录事件，保证业绩数据实时
func (s *FollowUpService) SetStats(ctx context.Context, svc *SalesEventStatsService) {
	s.stats = svc
}

// Schedule 安排跟进
func (s *FollowUpService) Schedule(ctx context.Context, customerID, ownerID string, rType ReminderType, dueIn time.Duration, opts *ScheduleOptions) (*Reminder, error) {
	if opts == nil {
		opts = &ScheduleOptions{}
	}
	r := &Reminder{
		ID:          generateReminderID(),
		CustomerID:  customerID,
		OneID:       opts.OneID,
		OwnerID:     ownerID,
		Type:        rType,
		Priority:    opts.Priority,
		Title:       opts.Title,
		Description: opts.Description,
		DueAt:       time.Now().Add(dueIn),
		CreatedAt:   time.Now(),
		Status:      "pending",
		SOPName:     opts.SOPName,
		AutoHandle:  opts.AutoHandle,
		Channel:     opts.Channel,
	}
	if r.Title == "" {
		r.Title = string(rType)
	}
	if r.Priority == 0 {
		r.Priority = PriorityNormal
	}
	s.mu.Lock()
	s.reminders[r.ID] = r
	s.mu.Unlock()
	return r, nil
}

// ScheduleOptions 安排选项
type ScheduleOptions struct {
	OneID       string
	Priority    ReminderPriority
	Title       string
	Description string
	Note        string
	SOPName     string
	AutoHandle  bool
	Channel     string
}

var reminderCounter int64

func generateReminderID() string {
	reminderCounter++
	return fmt.Sprintf("rem_%d_%d", time.Now().UnixNano(), reminderCounter)
}

// Complete 完成跟进
func (s *FollowUpService) Complete(ctx context.Context, reminderID string) error {
	return s.CompleteWithResult(ctx, reminderID, FollowUpResultContacted, "")
}

// FollowUpResult 跟进结果
// 商业产品级业务流：销售点击"完成跟进"时，必须记录跟进结果
// 才能驱动客户旅程自动推进 + 销售事件实时记录
type FollowUpResult string

const (
	FollowUpResultContacted  FollowUpResult = "contacted"
	FollowUpResultInterested FollowUpResult = "interested"
	FollowUpResultQuoted     FollowUpResult = "quoted"
	FollowUpResultConverted  FollowUpResult = "converted"
	FollowUpResultRejected   FollowUpResult = "rejected"
	FollowUpResultLost       FollowUpResult = "lost"
	FollowUpResultNoResponse FollowUpResult = "no_response"
)

// FollowUpResultInfo 跟进结果元信息（影响客户旅程推进）
var FollowUpResultInfo = map[FollowUpResult]struct {
	TargetStage JourneyStage
	Weight      int
	IsPositive  bool
}{
	FollowUpResultContacted:  {StageContact, 1, true},
	FollowUpResultInterested: {StageInterested, 3, true},
	FollowUpResultQuoted:     {StageQuoted, 5, true},
	FollowUpResultConverted:  {StageWon, 10, true},
	FollowUpResultRejected:   {StageLost, -2, false},
	FollowUpResultLost:       {StageLost, -5, false},
	FollowUpResultNoResponse: {StageSleeping, -1, false},
}

// CompleteWithResult 标记跟进完成（带结果，自动推进旅程 + 仪表盘实时更新）
// 商业产品级业务流：销售点"完成跟进"+ 选择结果 → 4 件事自动发生
//  1. 跟进状态更新为 done
//  2. 客户旅程自动推进到目标阶段
//  3. 销售事件流实时记录这次跟进（用于销售排行、漏斗、跟进率）
//  4. 客户档案更新（最后跟进时间、跟进次数）
//
// 返回更新后的跟进详情
func (s *FollowUpService) CompleteWithResult(ctx context.Context, reminderID string, result FollowUpResult, note string) error {
	s.mu.Lock()
	r, ok := s.reminders[reminderID]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("提醒 %s 不存在", reminderID)
	}
	// 状态守卫（A11）：done/canceled 不允许再完成 —— 该函数锁外会推进旅程并
	// 记销售事件，重复调用一次就多记一笔排行/漏斗数据；错误串带"已处理"供
	// HTTP 层映射 409。
	if r.Status != "pending" {
		s.mu.Unlock()
		return fmt.Errorf("提醒 %s 已处理", reminderID)
	}
	now := time.Now()
	r.CompletedAt = &now
	r.Status = "done"
	r.Description = r.Description + "\n[完成结果] " + string(result) + " | " + note
	customerID := r.CustomerID
	ownerID := r.OwnerID
	s.mu.Unlock()

	if s.journey != nil {
		s.journey.Touch(ctx, customerID, "followup")
		if info, ok := FollowUpResultInfo[result]; ok {
			_, _ = s.journey.Transition(ctx, customerID, info.TargetStage, "followup", ownerID,
				"跟进完成自动推进: "+string(result)+" | "+note, map[string]any{
					"reminder_id": reminderID,
					"result":      string(result),
					"note":        note,
				})
		}
	}

	if s.stats != nil {
		s.stats.RecordFollowUp(ctx, FollowUpEvent{
			CustomerID: customerID,
			OwnerID:    ownerID,
			Result:     string(result),
			OccurredAt: now,
		})
	}

	return nil
}

// Cancel 取消跟进
func (s *FollowUpService) Cancel(ctx context.Context, reminderID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.reminders[reminderID]
	if !ok {
		return fmt.Errorf("提醒 %s 不存在", reminderID)
	}
	// 状态守卫（A11）：终态（done/canceled）不可再改写，否则已完成的跟进会被
	// 回改成 canceled、完成记录悬空。
	if r.Status != "pending" {
		return fmt.Errorf("提醒 %s 已处理", reminderID)
	}
	r.Status = "canceled"
	return nil
}

// ListPending 列出待办（按截止时间排序）
func (s *FollowUpService) ListPending(ctx context.Context, ownerID string, limit int) []*Reminder {
	s.mu.RLock()
	defer s.mu.RUnlock()
	pending := make([]*Reminder, 0)
	for _, r := range s.reminders {
		if r.Status != "pending" {
			continue
		}
		if ownerID != "" && r.OwnerID != ownerID {
			continue
		}
		pending = append(pending, r)
	}
	sort.Slice(pending, func(i, j int) bool {
		if pending[i].Priority != pending[j].Priority {
			return pending[i].Priority > pending[j].Priority
		}
		return pending[i].DueAt.Before(pending[j].DueAt)
	})
	if limit > 0 && len(pending) > limit {
		pending = pending[:limit]
	}
	return pending
}

// ListOverdue 列出已逾期
func (s *FollowUpService) ListOverdue(ctx context.Context, ownerID string) []*Reminder {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := time.Now()
	overdue := make([]*Reminder, 0)
	for _, r := range s.reminders {
		if r.Status != "pending" {
			continue
		}
		if ownerID != "" && r.OwnerID != ownerID {
			continue
		}
		if r.DueAt.Before(now) {
			overdue = append(overdue, r)
		}
	}
	return overdue
}

// GetDailyCalendar 获取每日日历
// 商业逻辑：返回指定日期范围内（默认当天 0 点到 24 点）的所有待办
// 需处理 DueAt 恰好等于 dayEnd 的边界情况，且对 ownerID 过滤过于严格
func (s *FollowUpService) GetDailyCalendar(ctx context.Context, ownerID string, date time.Time) []*Reminder {
	s.mu.RLock()
	defer s.mu.RUnlock()
	dayStart := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
	dayEnd := dayStart.Add(24 * time.Hour)
	cal := make([]*Reminder, 0)
	for _, r := range s.reminders {
		if r.Status != "pending" {
			continue
		}
		if ownerID != "" && r.OwnerID != ownerID {
			continue
		}
		if !r.DueAt.Before(dayStart) && !r.DueAt.After(dayEnd) {
			cal = append(cal, r)
		}
	}
	sort.Slice(cal, func(i, j int) bool {
		return cal[i].DueAt.Before(cal[j].DueAt)
	})
	return cal
}

// GetWeeklyCalendar 获取每周日历（按天分组）
func (s *FollowUpService) GetWeeklyCalendar(ctx context.Context, ownerID string, startOfWeek time.Time) map[string][]*Reminder {
	s.mu.RLock()
	defer s.mu.RUnlock()
	weekStart := time.Date(startOfWeek.Year(), startOfWeek.Month(), startOfWeek.Day(), 0, 0, 0, 0, startOfWeek.Location())
	result := make(map[string][]*Reminder)
	for i := 0; i < 7; i++ {
		dayStart := weekStart.AddDate(0, 0, i)
		dayEnd := dayStart.Add(24 * time.Hour)
		key := dayStart.Format("2006-01-02")
		result[key] = []*Reminder{}
		for _, r := range s.reminders {
			if r.Status != "pending" {
				continue
			}
			if ownerID != "" && r.OwnerID != ownerID {
				continue
			}
			if !r.DueAt.Before(dayStart) && r.DueAt.Before(dayEnd) {
				result[key] = append(result[key], r)
			}
		}
	}
	return result
}

// AutoScheduleByJourney 根据旅程阶段自动安排跟进
func (s *FollowUpService) AutoScheduleByJourney(ctx context.Context, customerID, ownerID string) (*Reminder, error) {
	if s.journey == nil {
		return nil, fmt.Errorf("journey service not configured")
	}
	state := s.journey.GetState(ctx, customerID)
	meta := StageMetas[state.CurrentStage]
	if meta == nil || meta.DefaultFollowup == 0 {
		return nil, nil
	}
	rType := ReminderType(meta.RecommendedSOP)
	if rType == "" {
		rType = ReminderCustom
	}
	return s.Schedule(ctx, customerID, ownerID, rType, meta.DefaultFollowup, &ScheduleOptions{
		Title:       "阶段跟进: " + meta.Label,
		Description: meta.Description,
		Priority:    PriorityNormal,
		SOPName:     meta.RecommendedSOP,
		AutoHandle:  meta.AllowAIHandle,
	})
}

// ScheduleForStage 阶段迁移时安排跟进
func (s *FollowUpService) ScheduleForStage(ctx context.Context, customerID, ownerID string, toStage JourneyStage) (*Reminder, error) {
	meta := StageMetas[toStage]
	if meta == nil || meta.DefaultFollowup == 0 {
		return nil, nil
	}
	var rType ReminderType
	switch toStage {
	case StageQuoted:
		rType = ReminderQuoteFollowup
	case StageWon:
		rType = ReminderAfterSaleCare
	case StageAfterSale:
		rType = ReminderRepurchase
	case StageRepurchase, StageSleeping:
		rType = ReminderReactivation
	default:
		rType = ReminderFirstContact
	}
	return s.Schedule(ctx, customerID, ownerID, rType, meta.DefaultFollowup, &ScheduleOptions{
		Title:       "跟进: " + meta.Label,
		Description: meta.Description,
		Priority:    PriorityNormal,
		SOPName:     meta.RecommendedSOP,
		AutoHandle:  meta.AllowAIHandle,
	})
}
