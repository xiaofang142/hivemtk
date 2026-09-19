package dto

// RecoveryEnqueueRequest 入队请求
//
// Content 是消费 worker 唯一认的外发文案（见 service.RecoveryMessage）：
// 留空可以入队，但 worker 只会一直跳过它 —— 入队 ≠ 会发出去，这一点必须在字段注释里说明白，
// 否则运维会以为"排进去了就一定会触达"。
type RecoveryEnqueueRequest struct {
	CustomerID string `json:"customer_id" binding:"required"`
	UnifiedID  string `json:"unified_id"`
	Account    string `json:"account"`
	Reason     string `json:"reason"`
	Strategy   string `json:"strategy"`
	Priority   int    `json:"priority"`
	// Content 外发文案；worker 只发有文案的项
	Content string `json:"content"`
	// Subject 邮件主题
	Subject string `json:"subject"`
	// TemplateID 渠道侧模板 ID
	TemplateID string `json:"template_id"`
	// Params 模板参数
	Params map[string]string `json:"params"`
	// PreferredChannels 期望渠道（按序），留空由触达服务选路
	PreferredChannels []string `json:"preferred_channels"`
	// MaxAttempts 最大尝试次数；0 表示用默认 3
	MaxAttempts int `json:"max_attempts"`
}

// RecoveryMarkAttemptRequest 触达尝试请求
type RecoveryMarkAttemptRequest struct {
	Channel   string `json:"channel" binding:"required"`
	Result    string `json:"result"`
	Stage     string `json:"stage" binding:"required"`
	NextDelay int    `json:"next_delay_seconds"`
}

// RecoveryMarkRecoveredRequest 标记挽回成功
type RecoveryMarkRecoveredRequest struct {
	RecoveryValue int64 `json:"recovery_value"`
}

// RecoveryQueueResponse 单条响应
type RecoveryQueueResponse struct {
	ID            uint64  `json:"id"`
	CustomerID    string  `json:"customer_id"`
	UnifiedID     string  `json:"unified_id"`
	Account       string  `json:"account"`
	Reason        string  `json:"reason"`
	Strategy      string  `json:"strategy"`
	Priority      int     `json:"priority"`
	Stage         string  `json:"stage"`
	Attempts      int     `json:"attempts"`
	MaxAttempts   int     `json:"max_attempts"`
	LastChannel   string  `json:"last_channel"`
	LastResult    string  `json:"last_result"`
	RecoveryValue int64   `json:"recovery_value"`
	MetaJSON      string  `json:"meta_json"`
	LastAttemptAt *string `json:"last_attempt_at"`
	NextAttemptAt *string `json:"next_attempt_at"`
	RecoveredAt   *string `json:"recovered_at"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
}

// RecoveryQueueListResponse 列表响应
type RecoveryQueueListResponse struct {
	List     []*RecoveryQueueResponse `json:"list"`
	Total    int64                    `json:"total"`
	Page     int                      `json:"page"`
	PageSize int                      `json:"page_size"`
}

// RecoveryDistributionResponse 阶段分布
type RecoveryDistributionResponse struct {
	Distribution map[string]int64 `json:"distribution"`
	Total        int64            `json:"total"`
}
