package model

import (
	"time"

	"gorm.io/gorm"
)

// 短信送达状态
const (
	SmsStatusPending   = "pending"
	SmsStatusSent      = "sent"
	SmsStatusDelivered = "delivered"
	SmsStatusFailed    = "failed"
	SmsStatusRetryable = "retryable"
)

// 短信错误码前缀
//
// 合规/可观测性：
//   - ERR_6xxx：可重试错误（网关超时、运营商内部错误、流量控制等临时性故障）
//   - ERR_4xxx：不可重试错误（号码无效、黑名单、内容违规等永久性故障）
//   - ERR_5xxx：可重试但需延时（运营商限流）
const (
	SmsErrorCodeGatewayTimeout   = "ERR_6001"
	SmsErrorCodeProviderInternal = "ERR_5001"
	SmsErrorCodeRateLimited      = "ERR_5002"
	SmsErrorCodeInvalidPhone     = "ERR_4001"
	SmsErrorCodeBlacklisted      = "ERR_4002"
	SmsErrorCodeContentViolation = "ERR_4003"
	SmsErrorCodeSubscriberFreq   = "ERR_4004"
)

// SmsDeliveryStatus 短信送达状态记录
//
// 每个 message_id 一条记录，运营商状态报告 webhook 触发更新
// 用于到达率统计、失败重试、合规审计
type SmsDeliveryStatus struct {
	ID          uint           `gorm:"primarykey" json:"id"`
	MessageID   string         `gorm:"type:varchar(64);uniqueIndex;not null" json:"messageId"`
	Phone       string         `gorm:"type:varchar(20);index;not null" json:"phone"`
	JobID       string         `gorm:"type:varchar(36);index" json:"jobId"`
	Provider    string         `gorm:"type:varchar(50)" json:"provider"`
	Status      string         `gorm:"type:varchar(20);index;not null" json:"status"`
	ErrorCode   string         `gorm:"type:varchar(20)" json:"errorCode"`
	ErrorMsg    string         `gorm:"type:text" json:"errorMsg"`
	IsRetryable bool           `gorm:"default:false" json:"isRetryable"`
	RetryCount  int            `gorm:"default:0" json:"retryCount"`
	MaxRetry    int            `gorm:"default:3" json:"maxRetry"`
	SentAt      *time.Time     `json:"sentAt"`
	DeliveredAt *time.Time     `json:"deliveredAt"`
	ReceivedAt  time.Time      `gorm:"not null" json:"receivedAt"`
	CreatedAt   time.Time      `json:"createdAt"`
	UpdatedAt   time.Time      `json:"updatedAt"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName 显式指定表名
func (*SmsDeliveryStatus) TableName() string {
	return "sms_delivery_statuses"
}

// DefaultSmsMaxRetry 短信最大重试次数的代码兜底值，与参数中心
// `misc.sms_max_retry` 的 DefaultValue 一致。
const DefaultSmsMaxRetry = 3

// smsMaxRetryProvider 由装配层注入；未注入时回落 DefaultSmsMaxRetry。
//
// 放在 model 而不是 service：重试次数是这条记录自身的字段缺省，在 BeforeCreate 里补齐
// 才是它成立的位置。Model 不 import 任何上层包，接线靠函数变量（与
// repository/telegram_polling_lock.go 同一个办法）。
var smsMaxRetryProvider = func() int { return DefaultSmsMaxRetry }

// GetSmsMaxRetry 返回当前生效的短信最大重试次数（非正数一律回落默认值）。
func GetSmsMaxRetry() int {
	if n := smsMaxRetryProvider(); n > 0 {
		return n
	}
	return DefaultSmsMaxRetry
}

// SetSmsMaxRetryProvider 注入读取函数（装配层调用；测试可注入桩）。nil 视为不注入。
func SetSmsMaxRetryProvider(fn func() int) {
	if fn != nil {
		smsMaxRetryProvider = fn
	}
}

// BeforeCreate 创建前补全接收时间
func (s *SmsDeliveryStatus) BeforeCreate(tx *gorm.DB) error {
	if s.ReceivedAt.IsZero() {
		s.ReceivedAt = time.Now()
	}
	if s.MaxRetry == 0 {
		s.MaxRetry = GetSmsMaxRetry()
	}
	return nil
}

// SmsJobMetric 短信任务指标（每 job_id 一条）
//
// delivery_rate = total_delivered / total_sent * 100
// failure_rate  = total_failed   / total_sent * 100
type SmsJobMetric struct {
	ID             uint           `gorm:"primarykey" json:"id"`
	JobID          string         `gorm:"type:varchar(36);uniqueIndex;not null" json:"jobId"`
	TotalSent      int64          `gorm:"default:0" json:"totalSent"`
	TotalDelivered int64          `gorm:"default:0" json:"totalDelivered"`
	TotalFailed    int64          `gorm:"default:0" json:"totalFailed"`
	TotalRetried   int64          `gorm:"default:0" json:"totalRetried"`
	DeliveryRate   float64        `gorm:"type:numeric(5,2);default:0" json:"deliveryRate"`
	FailureRate    float64        `gorm:"type:numeric(5,2);default:0" json:"failureRate"`
	CreatedAt      time.Time      `json:"createdAt"`
	UpdatedAt      time.Time      `json:"updatedAt"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName 显式指定表名
func (*SmsJobMetric) TableName() string {
	return "sms_job_metrics"
}
