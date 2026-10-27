package sms

import (
	"context"
	"errors"
	"hivemtk-user/internal/identity"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

const smsMaxRetryCount = 3

var smsRetryableErrorPrefixes = []string{"ERR_6", "ERR_5"}

var smsNonRetryableErrorPrefixes = []string{"ERR_4"}

// DeliveryReportRequest 运营商状态报告 webhook 请求体
type DeliveryReportRequest struct {
	MessageID   string `json:"messageId" binding:"required"`
	Phone       string `json:"phone"`
	JobID       string `json:"jobId"`
	Provider    string `json:"provider"`
	Status      string `json:"status" binding:"required"`
	ErrorCode   string `json:"errorCode"`
	ErrorMsg    string `json:"errorMsg"`
	SentAt      string `json:"sentAt"`
	DeliveredAt string `json:"deliveredAt"`
}

// SmsTrackingService 短信追踪服务
//
// 职责：
//   - RecordDeliveryReport：接收运营商状态报告 webhook
//   - IsRetryable：判断错误码是否可重试
//   - RetryFailedMessages：重试可重试的失败消息
type SmsTrackingService struct {
	repo repository.SmsTrackingRepository
}

// NewSmsTrackingService 创建短信追踪服务
func NewSmsTrackingService(repo repository.SmsTrackingRepository) *SmsTrackingService {
	if repo == nil {
		repo = repository.NewSmsTrackingRepository(nil)
	}
	return &SmsTrackingService{repo: repo}
}

// RecordDeliveryReport 记录运营商状态报告
//
// 行为：
//  1. 若 message_id 已存在：更新状态（webhook 可能多次推送同一消息的中间状态）
//  2. 若 message_id 不存在：创建新记录
//  3. 根据错误码自动判定 is_retryable
//  4. failed 状态且 is_retryable=true 时，状态置为 retryable 等待定时任务重试
func (s *SmsTrackingService) RecordDeliveryReport(ctx context.Context, req *DeliveryReportRequest) error {
	if req.MessageID == "" {
		return errors.New("message_id 不能为空")
	}
	if req.Status == "" {
		return errors.New("status 不能为空")
	}

	status := normalizeSmsStatus(req.Status)

	isRetryable := false
	if status == model.SmsStatusFailed {
		isRetryable = s.IsRetryable(ctx, req.ErrorCode)
		if isRetryable {
			status = model.SmsStatusRetryable
		}
	}

	now := time.Now()
	sentAt, _ := parseSmsTime(req.SentAt)
	deliveredAt, _ := parseSmsTime(req.DeliveredAt)

	existing, err := s.repo.GetByMessageID(ctx, req.MessageID)
	if err != nil {
		logger.Errorf("查询短信送达状态失败 message_id=%s: %v", req.MessageID, err)
		return err
	}

	if existing != nil {
		existing.Status = status
		existing.ErrorCode = req.ErrorCode
		existing.ErrorMsg = req.ErrorMsg
		existing.IsRetryable = isRetryable
		existing.Provider = req.Provider
		if req.Phone != "" {
			existing.Phone = identity.NormalizePhone(req.Phone)
		}
		if req.JobID != "" {
			existing.JobID = req.JobID
		}
		if sentAt != nil {
			existing.SentAt = sentAt
		}
		if deliveredAt != nil {
			existing.DeliveredAt = deliveredAt
		}
		existing.ReceivedAt = now
		return s.repo.UpdateStatus(ctx, existing)
	}

	record := &model.SmsDeliveryStatus{
		MessageID:   req.MessageID,
		Phone:       identity.NormalizePhone(req.Phone),
		JobID:       req.JobID,
		Provider:    req.Provider,
		Status:      status,
		ErrorCode:   req.ErrorCode,
		ErrorMsg:    req.ErrorMsg,
		IsRetryable: isRetryable,
		RetryCount:  0,
		MaxRetry:    smsMaxRetryCount,
		SentAt:      sentAt,
		DeliveredAt: deliveredAt,
		ReceivedAt:  now,
	}
	return s.repo.CreateStatus(ctx, record)
}

// TenantIDByMessageID 系统作用域反查回执归属租户（发送侧预落的 message_id 行）
func (s *SmsTrackingService) TenantIDByMessageID(ctx context.Context, messageID string) (uint, bool, error) {
	return s.repo.TenantIDByMessageID(ctx, messageID)
}

// IsRetryable 判断错误码是否可重试
//
// 规则：
//   - ERR_6xxx（网关超时）→ 可重试
//   - ERR_5xxx（运营商内部错误 / 限流）→ 可重试
//   - ERR_4xxx（号码无效 / 黑名单 / 内容违规）→ 不重试
//   - 空错误码 → 不重试（保守策略）
//   - 其他未知错误码 → 不重试（保守策略）
func (s *SmsTrackingService) IsRetryable(ctx context.Context, errorCode string) bool {
	if errorCode == "" {
		return false
	}
	for _, prefix := range smsNonRetryableErrorPrefixes {
		if strings.HasPrefix(errorCode, prefix) {
			return false
		}
	}
	for _, prefix := range smsRetryableErrorPrefixes {
		if strings.HasPrefix(errorCode, prefix) {
			return true
		}
	}
	return false
}

// RetryFailedMessages 重试可重试的失败消息
//
// 由定时任务调用（每 5 分钟一次）
// 返回值：实际重试的消息数 + 错误（如果有）
// 注意：本方法只标记 retry_count 自增和状态变更，实际重新发送由调用方（短信服务）执行
func (s *SmsTrackingService) RetryFailedMessages(ctx context.Context, batchSize int) (int, error) {
	if batchSize <= 0 {
		batchSize = 100
	}

	statuses, err := s.repo.ListRetryableStatuses(ctx, batchSize)
	if err != nil {
		logger.Errorf("查询可重试短信失败: %v", err)
		return 0, err
	}

	retried := 0
	for _, st := range statuses {
		if st.RetryCount >= st.MaxRetry {
			st.Status = model.SmsStatusFailed
			st.IsRetryable = false
			st.ErrorMsg = "已达最大重试次数：" + st.ErrorMsg
			if err := s.repo.UpdateStatus(ctx, st); err != nil {
				logger.Errorf("更新短信重试状态失败 message_id=%s: %v", st.MessageID, err)
				continue
			}
			continue
		}

		st.RetryCount++
		if err := s.repo.UpdateStatus(ctx, st); err != nil {
			logger.Errorf("更新短信重试计数失败 message_id=%s: %v", st.MessageID, err)
			continue
		}
		retried++
	}

	return retried, nil
}

// ListJobStatuses 分页查询任务的送达状态
func (s *SmsTrackingService) ListJobStatuses(ctx context.Context, jobID string, page, limit int) ([]*model.SmsDeliveryStatus, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 500 {
		limit = 20
	}
	return s.repo.ListStatusesByJob(ctx, jobID, page, limit)
}

// ListPhoneStatuses 分页查询手机号的送达状态
func (s *SmsTrackingService) ListPhoneStatuses(ctx context.Context, phone string, page, limit int) ([]*model.SmsDeliveryStatus, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 500 {
		limit = 20
	}
	phone = identity.NormalizePhone(phone)
	return s.repo.ListStatusesByPhone(ctx, phone, page, limit)
}

func normalizeSmsStatus(status string) string {
	status = strings.ToUpper(strings.TrimSpace(status))
	switch status {
	case "DELIV", "DELIVERED", "SUCCESS", "OK", "REACH":
		return model.SmsStatusDelivered
	case "FAIL", "FAILED", "ERROR", "UNDELIV", "UNDELIVERED":
		return model.SmsStatusFailed
	case "PENDING", "WAITING", "SENDING":
		return model.SmsStatusPending
	case "SENT", "ACCEPTD", "ACCEPTED":
		return model.SmsStatusSent
	default:
		return strings.ToLower(status)
	}
}

func parseSmsTime(s string) (*time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}

	formats := []string{
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02 15:04:05Z07:00",
		"2006-01-02",
	}
	for _, f := range formats {
		if t, err := time.ParseInLocation(f, s, time.Local); err == nil {
			return &t, nil
		}
	}

	return nil, errors.New("unsupported time format")
}
