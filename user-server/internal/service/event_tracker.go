package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	bizerr "hivemtk-user/internal/domain/errors"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/dbstate"
	"hivemtk-user/internal/pkg/timeutil"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
	"runtime/debug"
	"time"
)

// EventTracker 事件追踪服务
type EventTracker struct {
	repo         repository.CustomerEventRepository
	customerRepo repository.CustomerRepository
	autoTagger   *AutoTagger
	orchestrator *CustomerOrchestrator
	disableAsync bool
}

// NewEventTracker 创建事件追踪服务实例
func NewEventTracker(customerService *CustomerService) *EventTracker {
	return &EventTracker{
		repo:         repository.NewCustomerEventRepository(),
		customerRepo: repository.NewCustomerRepository(),
		autoTagger:   NewAutoTagger(),
	}
}

// SetOrchestrator 注入客户业务编排层（F-）。
// 未注入时 Track 仅完成事件落库与 autoTagger，不联动旅程阶段迁移。
func (s *EventTracker) SetOrchestrator(o *CustomerOrchestrator) {
	s.orchestrator = o
}

// DisableAsync 禁用异步处理（用于测试）
func (s *EventTracker) DisableAsync(ctx context.Context) {
	s.disableAsync = true
}

// EventDTO 事件数据传输对象
type EventDTO struct {
	CustomerID  string            `json:"customer_id"`
	EventType   model.EventType   `json:"event_type"`
	EventSource model.EventSource `json:"event_source"`
	EventData   map[string]any    `json:"event_data"`
}

// 事件表的三列长度上限（customer_events.customer_id varchar(64) /
// event_type varchar(32) / event_source varchar(32)）。超长时 PG 会回 22001，
// 旧实现把整段 SQL 原文透给前端，这里提前拦成参数错误。
const (
	maxEventCustomerIDLen = 64
	maxEventTypeLen       = 32
	maxEventSourceLen     = 32
)

// validateEventFields 校验事件标识字段的取值与长度。
func validateEventFields(dto *EventDTO) error {
	if n := utf8.RuneCountInString(dto.CustomerID); n > maxEventCustomerIDLen {
		return bizerr.New(bizerr.CodeParamInvalid,
			fmt.Sprintf("请求参数错误：customer_id 不能超过 %d 个字符（当前 %d）", maxEventCustomerIDLen, n))
	}
	if dto.EventType == "" {
		return bizerr.New(bizerr.CodeParamInvalid, "请求参数错误：event_type 不能为空")
	}
	if n := utf8.RuneCountInString(string(dto.EventType)); n > maxEventTypeLen {
		return bizerr.New(bizerr.CodeParamInvalid,
			fmt.Sprintf("请求参数错误：event_type 不能超过 %d 个字符（当前 %d）", maxEventTypeLen, n))
	}
	if n := utf8.RuneCountInString(string(dto.EventSource)); n > maxEventSourceLen {
		return bizerr.New(bizerr.CodeParamInvalid,
			fmt.Sprintf("请求参数错误：event_source 不能超过 %d 个字符（当前 %d）", maxEventSourceLen, n))
	}
	return nil
}

// Track 追踪通用事件
func (s *EventTracker) Track(ctx context.Context, dto *EventDTO) error {
	if dto == nil || strings.TrimSpace(dto.CustomerID) == "" {
		return ErrInvalidDTO
	}
	dto.CustomerID = strings.TrimSpace(dto.CustomerID)
	if err := validateEventFields(dto); err != nil {
		return err
	}

	event := &model.CustomerEvent{
		CustomerID:  dto.CustomerID,
		EventType:   dto.EventType,
		EventSource: dto.EventSource,
		OccurredAt:  time.Now(),
	}

	if dto.EventData != nil {
		if err := SetCustomerEventData(event, dto.EventData); err != nil {
			return err
		}
	}

	if err := s.repo.Record(ctx, event); err != nil {
		return err
	}

	if !s.disableAsync {
		// 测试收尾/装配早期 dbstate 可能未就绪——无库窗口里的异步打标没有意义，
		// 与其在协程里 recover 一条 nil-DB panic 栈，不如派发前显式跳过并留痕。
		// （Record 本身已在上面同步路径上报错返回，这里只管异步面。）
		if dbstate.Get() == nil {
			logger.Warnf("event_tracker: DB 未就绪，跳过事件 %s 的异步打标", event.CustomerID)
			return nil
		}
		// fire-and-forget 异步处理不能复用请求 ctx：HTTP handler 返回即取消，异步任务会被掐断半途
		asyncCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
		go func() {
			defer cancel()
			defer func() {
				if r := recover(); r != nil {
					logger.Errorf("event_tracker: AutoTagger.ProcessEvent recovered from panic: %v\nstack: %s", r, debug.Stack())
				}
			}()
			if err := s.autoTagger.ProcessEvent(asyncCtx, event); err != nil {
				logger.Errorf("AutoTagger.ProcessEvent error: %v", err)
			}
		}()
		if s.orchestrator != nil {
			go func() {
				defer func() {
					if r := recover(); r != nil {
						logger.Errorf("event_tracker: orchestrator.OnCustomerEvent recovered from panic: %v", r)
					}
				}()
				s.orchestrator.OnCustomerEvent(asyncCtx, dto.CustomerID, event)
			}()
		}
	}

	return nil
}

// TrackPageView 追踪页面浏览
func (s *EventTracker) TrackPageView(ctx context.Context, customerID, url, title string) error {
	return s.Track(ctx, &EventDTO{
		CustomerID:  customerID,
		EventType:   model.EventTypePageView,
		EventSource: model.EventSourceWebsite,
		EventData: map[string]any{
			"url":   url,
			"title": title,
		},
	})
}

// TrackClick 追踪点击事件
func (s *EventTracker) TrackClick(ctx context.Context, customerID, element, target string) error {
	return s.Track(ctx, &EventDTO{
		CustomerID:  customerID,
		EventType:   model.EventTypeClick,
		EventSource: model.EventSourceWebsite,
		EventData: map[string]any{
			"element": element,
			"target":  target,
		},
	})
}

// TrackPurchase 追踪购买事件
func (s *EventTracker) TrackPurchase(ctx context.Context, customerID string, amount float64, items []string) error {
	return s.Track(ctx, &EventDTO{
		CustomerID:  customerID,
		EventType:   model.EventTypePurchase,
		EventSource: model.EventSourceWebsite,
		EventData: map[string]any{
			"amount": amount,
			"items":  items,
		},
	})
}

// GetEventHistory 获取客户事件历史
func (s *EventTracker) GetEventHistory(ctx context.Context, customerID string, limit int) ([]*model.CustomerEvent, error) {
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}

	return s.repo.GetByCustomerID(ctx, customerID, limit)
}

// GetStats 获取事件统计
//
// start/end 是日粒度字符串，两条边界都必须按**业务时区**落地：
//   - time.Parse("2006-01-02", …) 返回 UTC 零点，而本仓 PG 会话时区钉在 CST，
//     拿去和 occurred_at 比较会整体后移 8 小时（每天头 8 小时的记录被切掉）；
//   - 仓储用的是 `occurred_at <= end`，end 若停在结束日 00:00 就会把结束日**整天**
//     静默丢掉 —— 与第二十六轮 short_link 那处同一个形状。
func (s *EventTracker) GetStats(ctx context.Context, start, end string) (*repository.EventStats, error) {
	startTime, err := timeutil.ParseBusinessDate(start)
	if err != nil {
		startTime = timeutil.TrailingMonthStart(time.Now())
	}

	endTime, err := timeutil.ParseBusinessDate(end)
	if err != nil {
		endTime = time.Now()
	} else {
		endTime = timeutil.EndOfDay(endTime)
	}

	return s.repo.GetStats(ctx, startTime, endTime)
}

// TrackSignup 追踪注册事件
func (s *EventTracker) TrackSignup(ctx context.Context, customerID, signupMethod string) error {
	return s.Track(ctx, &EventDTO{
		CustomerID:  customerID,
		EventType:   model.EventTypeSignup,
		EventSource: model.EventSourceWebsite,
		EventData: map[string]any{
			"signup_method": signupMethod,
		},
	})
}

// TrackLogin 追踪登录事件
func (s *EventTracker) TrackLogin(ctx context.Context, customerID, loginMethod string) error {
	return s.Track(ctx, &EventDTO{
		CustomerID:  customerID,
		EventType:   model.EventTypeLogin,
		EventSource: model.EventSourceWebsite,
		EventData: map[string]any{
			"login_method": loginMethod,
		},
	})
}

// TrackAddToCart 追踪加购事件
func (s *EventTracker) TrackAddToCart(ctx context.Context, customerID, productID, productName string, price float64, quantity int) error {
	return s.Track(ctx, &EventDTO{
		CustomerID:  customerID,
		EventType:   model.EventTypeAddToCart,
		EventSource: model.EventSourceWebsite,
		EventData: map[string]any{
			"product_id":   productID,
			"product_name": productName,
			"price":        price,
			"quantity":     quantity,
		},
	})
}

// RecordReachEvent 将触达结果回流为客户事件 (F。
//
// 实现 ReachEventRecorder 接口，由 reach_send_pipeline.defaultSendPipeline.Send
// 在触达完成后调用（成功/失败均记录），补全 CDP 数据回流闭环。
//
// 设计：
//   - 事件类型 = "reach"，事件来源 = channel（sms/email/wecom/...）
//   - EventData 记录 message_id / success / error，便于后续分析触达效果
//   - 失败仅记录日志，不向上游返回错误（best-effort，不阻塞触达主流程）
func (s *EventTracker) RecordReachEvent(ctx context.Context, customerID, channel, messageID string, success bool, errMsg string) error {
	if customerID == "" {
		return nil
	}
	eventType := model.EventType("reach")
	eventSource := model.EventSource(channel)
	if eventSource == "" {
		eventSource = model.EventSourceWebsite
	}
	eventData := map[string]any{
		"channel":    channel,
		"message_id": messageID,
		"success":    success,
	}
	if errMsg != "" {
		eventData["error"] = errMsg
	}
	if err := s.Track(ctx, &EventDTO{
		CustomerID:  customerID,
		EventType:   eventType,
		EventSource: eventSource,
		EventData:   eventData,
	}); err != nil {
		logger.Errorf("[F-P1-91] RecordReachEvent failed customer=%s channel=%s err=%v",
			customerID, channel, err)
		return err
	}
	return nil
}

// TrackWithEventData 追踪带自定义数据的事件
func (s *EventTracker) TrackWithEventData(ctx context.Context, customerID, eventType, eventSource string, eventData map[string]any) error {
	dto := &EventDTO{
		CustomerID: customerID,
		EventType:  model.EventType(eventType),
	}

	if eventSource != "" {
		dto.EventSource = model.EventSource(eventSource)
	}

	if eventData != nil {
		dto.EventData = eventData
	}

	return s.Track(ctx, dto)
}

// GetEventCount 获取客户事件数量
func (s *EventTracker) GetEventCount(ctx context.Context, customerID string) (int64, error) {
	events, err := s.repo.GetByCustomerID(ctx, customerID, -1)
	if err != nil {
		return 0, err
	}
	return int64(len(events)), nil
}

func (s *EventTracker) ListGlobalEvents(ctx context.Context, eventType string, limit, offset int) ([]*model.CustomerEvent, int64, error) {
	return s.repo.ListGlobal(ctx, eventType, limit, offset)
}

// DeleteByCustomerID 删除指定客户的所有事件，返回删除条数
func (s *EventTracker) DeleteByCustomerID(ctx context.Context, customerID string) (int64, error) {
	return s.repo.DeleteByCustomerID(ctx, customerID)
}

// SerializeEventData 序列化事件数据为 JSON 字符串
func SerializeEventData(data map[string]any) (string, error) {
	if data == nil {
		return "{}", nil
	}
	jsonData, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	return string(jsonData), nil
}
