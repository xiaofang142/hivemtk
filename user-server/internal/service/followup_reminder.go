package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
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
	ID string `json:"id"`
	// TenantID 租户隔离键（FA-26）：所有读写按 ctx 租户过滤，跨租户隐藏存在性。
	TenantID    uint             `json:"tenant_id"`
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
	// store 持久仓储（FA-27）：nil = 内存 map（仅测试/装配缺席降级）；
	// 生产三条链（HTTP 今日跟进/回款钩子/AI 工具）必须注入同一权威存储。
	store repository.FollowUpReminderStore
}

// NewFollowUpService 创建跟进服务（内存存储：仅供单测与显式降级使用）。
func NewFollowUpService(journey *CustomerJourneyService) *FollowUpService {
	return &FollowUpService{
		reminders: make(map[string]*Reminder),
		journey:   journey,
	}
}

// NewFollowUpServiceWithStore 创建持久化跟进服务（FA-27 生产装配口）。
// 三条生产链传同一 repository.FollowUpReminderRepository（同一 DB 句柄）即共享权威存储。
func NewFollowUpServiceWithStore(journey *CustomerJourneyService, store repository.FollowUpReminderStore) *FollowUpService {
	return &FollowUpService{
		reminders: make(map[string]*Reminder),
		journey:   journey,
		store:     store,
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
	// FA-26：行归属取 ctx 租户（缺省档落 default:1，与全仓 default:1 口径一致）。
	if id, ok := tenant.FromContext(ctx); ok && id > 0 {
		r.TenantID = id
	}
	if s.store != nil {
		if err := s.store.Create(ctx, reminderToRow(r)); err != nil {
			return nil, fmt.Errorf("提醒持久化失败：%w", err)
		}
		return r, nil
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

// fetchReminder 取提醒（FA-26：行在当前 ctx 租户范围内才可见；store 路径由仓储
// scoped 查询保证，map 路径在此显式比对租户）。
func (s *FollowUpService) fetchReminder(ctx context.Context, reminderID string) (*Reminder, error) {
	if s.store != nil {
		row, err := s.store.GetByID(ctx, reminderID)
		if err != nil {
			// 与 map 路径同一文案：跨租户/不存在都叫"不存在"，不泄露存在性差异。
			return nil, fmt.Errorf("提醒 %s 不存在", reminderID)
		}
		return rowToReminder(row), nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.reminders[reminderID]
	if !ok {
		return nil, fmt.Errorf("提醒 %s 不存在", reminderID)
	}
	if id, ok := tenant.FromContext(ctx); ok && id > 0 && r.TenantID != id {
		// 跨租户隐藏存在性：与"不存在"同一个错误，不泄露他人待办。
		return nil, fmt.Errorf("提醒 %s 不存在", reminderID)
	}
	return r, nil
}

// persistReminder 把对 r 的变更落回权威存储（store 或内存 map）。
func (s *FollowUpService) persistReminder(ctx context.Context, r *Reminder) error {
	if s.store != nil {
		return s.store.Update(ctx, reminderToRow(r))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reminders[r.ID] = r
	return nil
}

// authorizeReminderOperator FA-26：普通用户只能操作自己的待办，管理员豁免。
func authorizeReminderOperator(r *Reminder, operatorID string, isAdmin bool) error {
	if isAdmin {
		return nil
	}
	if operatorID == "" || r.OwnerID != operatorID {
		return fmt.Errorf("%w：跟进提醒 %s 属于他人", ErrFollowUpForbidden, r.ID)
	}
	return nil
}

// ErrFollowUpForbidden 无权操作他人跟进提醒（HTTP 层映射 403）。
var ErrFollowUpForbidden = errors.New("无权操作该提醒")

// CompleteWithResult 标记跟进完成（带结果，自动推进旅程 + 仪表盘实时更新）
// 商业产品级业务流：销售点"完成跟进"+ 选择结果 → 4 件事自动发生
//  1. 跟进状态更新为 done
//  2. 客户旅程自动推进到目标阶段
//  3. 销售事件流实时记录这次跟进（用于销售排行、漏斗、跟进率）
//  4. 客户档案更新（最后跟进时间、跟进次数）
//
// 返回更新后的跟进详情。
//
// 本签名是系统内部链（AI 工具/回款钩子/单测）用的口：调用方以系统权威代行，
// 租户仍按 ctx 过滤；HTTP 写口一律走 CompleteWithResultFor 带操作者授权。
func (s *FollowUpService) CompleteWithResult(ctx context.Context, reminderID string, result FollowUpResult, note string) error {
	return s.completeWithResult(ctx, reminderID, "", false, result, note)
}

// CompleteWithResultFor HTTP 写口：带操作者身份的完成跟进（FA-26 授权矩阵）。
// 操作者身份缺失即拒绝：授权入口不提供"空操作者 = 系统权威"的旁路。
func (s *FollowUpService) CompleteWithResultFor(ctx context.Context, reminderID, operatorID string, isAdmin bool, result FollowUpResult, note string) error {
	if operatorID == "" && !isAdmin {
		return ErrFollowUpForbidden
	}
	return s.completeWithResult(ctx, reminderID, operatorID, isAdmin, result, note)
}

// withOperator=true 才做操作者校验：Complete/Cancel 系统内部链（AI 工具/回款钩子）
// 以系统权威代行，租户仍按 ctx 过滤；HTTP 写口（*For）恒带身份。
func (s *FollowUpService) completeWithResult(ctx context.Context, reminderID, operatorID string, isAdmin bool, result FollowUpResult, note string) (err error) {
	withOperator := operatorID != "" || isAdmin
	r, err := s.fetchReminder(ctx, reminderID)
	if err != nil {
		return err
	}
	if withOperator {
		if err := authorizeReminderOperator(r, operatorID, isAdmin); err != nil {
			return err
		}
	}
	// 状态守卫（A11）：done/canceled 不允许再完成 —— 该函数锁外会推进旅程并
	// 记销售事件，重复调用一次就多记一笔排行/漏斗数据；错误串带"已处理"供
	// HTTP 层映射 409。
	if r.Status != "pending" {
		return fmt.Errorf("提醒 %s 已处理", reminderID)
	}
	now := time.Now()
	r.CompletedAt = &now
	r.Status = "done"
	r.Description = r.Description + "\n[完成结果] " + string(result) + " | " + note
	if err := s.persistReminder(ctx, r); err != nil {
		return fmt.Errorf("提醒完成状态持久化失败：%w", err)
	}
	customerID := r.CustomerID
	ownerID := r.OwnerID

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

// Cancel 取消跟进（系统内部链口，租户按 ctx 过滤；HTTP 写口走 CancelFor）。
func (s *FollowUpService) Cancel(ctx context.Context, reminderID string) error {
	return s.cancel(ctx, reminderID, "", false)
}

// CancelFor HTTP 写口：带操作者身份的取消跟进（FA-26 授权矩阵）。
func (s *FollowUpService) CancelFor(ctx context.Context, reminderID, operatorID string, isAdmin bool) error {
	if operatorID == "" && !isAdmin {
		return ErrFollowUpForbidden
	}
	return s.cancel(ctx, reminderID, operatorID, isAdmin)
}

func (s *FollowUpService) cancel(ctx context.Context, reminderID, operatorID string, isAdmin bool) error {
	withOperator := operatorID != "" || isAdmin
	r, err := s.fetchReminder(ctx, reminderID)
	if err != nil {
		return err
	}
	if withOperator {
		if err := authorizeReminderOperator(r, operatorID, isAdmin); err != nil {
			return err
		}
	}
	// 状态守卫（A11）：终态（done/canceled）不可再改写，否则已完成的跟进会被
	// 回改成 canceled、完成记录悬空。
	if r.Status != "pending" {
		return fmt.Errorf("提醒 %s 已处理", reminderID)
	}
	r.Status = "canceled"
	return s.persistReminder(ctx, r)
}

// ListPending 列出待办（按截止时间排序；FA-26：按 ctx 租户过滤 + 负责人过滤）
func (s *FollowUpService) ListPending(ctx context.Context, ownerID string, limit int) []*Reminder {
	if s.store != nil {
		rows, err := s.store.ListPending(ctx, ownerID, limit)
		if err != nil {
			// 读侧故障不得伪装成"零待办"静默吞掉：记日志后仍返回空，但留痕可查。
			logger.Errorf("[followup] ListPending 读取失败 owner=%q: %v", ownerID, err)
			return []*Reminder{}
		}
		out := make([]*Reminder, 0, len(rows))
		for _, row := range rows {
			out = append(out, rowToReminder(row))
		}
		return out
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	tenantID, hasTenant := tenant.FromContext(ctx)
	pending := make([]*Reminder, 0)
	for _, r := range s.reminders {
		if r.Status != "pending" {
			continue
		}
		if hasTenant && r.TenantID != tenantID {
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

// ListOverdue 列出已逾期（FA-26：按 ctx 租户过滤）
func (s *FollowUpService) ListOverdue(ctx context.Context, ownerID string) []*Reminder {
	if s.store != nil {
		rows, err := s.store.ListOverdue(ctx, ownerID, time.Now())
		if err != nil {
			logger.Errorf("[followup] ListOverdue 读取失败 owner=%q: %v", ownerID, err)
			return []*Reminder{}
		}
		out := make([]*Reminder, 0, len(rows))
		for _, row := range rows {
			out = append(out, rowToReminder(row))
		}
		return out
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	tenantID, hasTenant := tenant.FromContext(ctx)
	now := time.Now()
	overdue := make([]*Reminder, 0)
	for _, r := range s.reminders {
		if r.Status != "pending" {
			continue
		}
		if hasTenant && r.TenantID != tenantID {
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
	dayStart := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
	dayEnd := dayStart.Add(24 * time.Hour)
	if s.store != nil {
		rows, err := s.store.ListRange(ctx, ownerID, dayStart, dayEnd)
		if err != nil {
			logger.Errorf("[followup] GetDailyCalendar 读取失败 owner=%q: %v", ownerID, err)
			return []*Reminder{}
		}
		out := make([]*Reminder, 0, len(rows))
		for _, row := range rows {
			out = append(out, rowToReminder(row))
		}
		return out
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	tenantID, hasTenant := tenant.FromContext(ctx)
	cal := make([]*Reminder, 0)
	for _, r := range s.reminders {
		if r.Status != "pending" {
			continue
		}
		if hasTenant && r.TenantID != tenantID {
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

// GetWeeklyCalendar 获取每周日历（按天分组；FA-26：按 ctx 租户过滤）
func (s *FollowUpService) GetWeeklyCalendar(ctx context.Context, ownerID string, startOfWeek time.Time) map[string][]*Reminder {
	weekStart := time.Date(startOfWeek.Year(), startOfWeek.Month(), startOfWeek.Day(), 0, 0, 0, 0, startOfWeek.Location())
	result := make(map[string][]*Reminder)
	var all []*Reminder
	if s.store != nil {
		weekEnd := weekStart.AddDate(0, 0, 7)
		rows, err := s.store.ListRange(ctx, ownerID, weekStart, weekEnd)
		if err != nil {
			logger.Errorf("[followup] GetWeeklyCalendar 读取失败 owner=%q: %v", ownerID, err)
			rows = nil
		}
		all = make([]*Reminder, 0, len(rows))
		for _, row := range rows {
			all = append(all, rowToReminder(row))
		}
	} else {
		s.mu.RLock()
		tenantID, hasTenant := tenant.FromContext(ctx)
		for _, r := range s.reminders {
			if r.Status != "pending" {
				continue
			}
			if hasTenant && r.TenantID != tenantID {
				continue
			}
			if ownerID != "" && r.OwnerID != ownerID {
				continue
			}
			all = append(all, r)
		}
		s.mu.RUnlock()
	}
	for i := 0; i < 7; i++ {
		dayStart := weekStart.AddDate(0, 0, i)
		dayEnd := dayStart.Add(24 * time.Hour)
		key := dayStart.Format("2006-01-02")
		result[key] = []*Reminder{}
		for _, r := range all {
			if !r.DueAt.Before(dayStart) && r.DueAt.Before(dayEnd) {
				result[key] = append(result[key], r)
			}
		}
	}
	return result
}

// reminderToRow / rowToReminder 服务域对象与持久行的互转（字段一一对应）。
func reminderToRow(r *Reminder) *model.FollowUpReminder {
	return &model.FollowUpReminder{
		TenantID: r.TenantID, ID: r.ID, CustomerID: r.CustomerID, OneID: r.OneID,
		OwnerID: r.OwnerID, Type: string(r.Type), Priority: int(r.Priority),
		Title: r.Title, Description: r.Description, DueAt: r.DueAt,
		CreatedAt: r.CreatedAt, CompletedAt: r.CompletedAt, Status: r.Status,
		SOPName: r.SOPName, AutoHandle: r.AutoHandle, Channel: r.Channel,
	}
}

func rowToReminder(row *model.FollowUpReminder) *Reminder {
	return &Reminder{
		TenantID: row.TenantID, ID: row.ID, CustomerID: row.CustomerID, OneID: row.OneID,
		OwnerID: row.OwnerID, Type: ReminderType(row.Type), Priority: ReminderPriority(row.Priority),
		Title: row.Title, Description: row.Description, DueAt: row.DueAt,
		CreatedAt: row.CreatedAt, CompletedAt: row.CompletedAt, Status: row.Status,
		SOPName: row.SOPName, AutoHandle: row.AutoHandle, Channel: row.Channel,
	}
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
