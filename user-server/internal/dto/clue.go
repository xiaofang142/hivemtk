package dto

type GetClueListResponse struct {
	Total int64               `json:"total"`
	List  []*ClueListResponse `json:"list"`
}

type ClueListResponse struct {
	ID                 string `json:"id"`
	SourceID           string `json:"source_id"`
	Account            string `json:"account"`
	IsVerify           int64  `json:"is_verify"`
	Type               int64  `json:"type"`
	Name               string `json:"name"`
	City               string `json:"city"`
	Address            string `json:"address"`
	Desc               string `json:"desc"`
	OwnerAccount       string `json:"owner_account"`
	CreateTime         int64  `json:"create_time"`
	UpdatedAt          int64  `json:"updated_at"`
	IntentScore        int64  `json:"intent_score"`
	IsOpportunity      int64  `json:"is_opportunity"`
	MessageID          string `json:"message_id"`
	ConversationID     string `json:"conversation_id"`
	OneID              string `json:"one_id"`
	IsGroup            bool   `json:"is_group"`
	GroupID            string `json:"group_id"`
	GroupName          string `json:"group_name"`
	Level              string `json:"level"`
	LeadRole           string `json:"lead_role"`
	Status             string `json:"status"`
	EvidenceJSON       string `json:"evidence_json,omitempty"`
	Summary            string `json:"summary,omitempty"`
	FollowupSuggestion string `json:"followup_suggestion,omitempty"`
	LastJudgedAt       *int64 `json:"last_judged_at,omitempty"`
	SourceChannel      string `json:"source_channel,omitempty"`
	SourceAccountID    string `json:"source_account_id,omitempty"`
	ConversationType   string `json:"conversation_type,omitempty"`
	FeedbackStatus     string `json:"feedback_status,omitempty"`
	FeedbackReason     string `json:"feedback_reason,omitempty"`
	FeedbackBy         string `json:"feedback_by,omitempty"`
	FeedbackAt         *int64 `json:"feedback_at,omitempty"`
}

// GetClueRequest 列表筛选条件全部下推到 DB，避免只在前端过滤当前页。
// Type / IsVerify 用指针区分"没传"与"传了 0"（未验证＝0）。
type GetClueRequest struct {
	Page             int    `form:"page"`
	PageSize         int    `form:"limit"`
	Keyword          string `form:"keyword"`
	Type             *int64 `form:"type"`
	IsVerify         *int64 `form:"is_verify"`
	Start            int64  `form:"start"`
	End              int64  `form:"end"`
	LeadRole         string `form:"lead_role"`
	Status           string `form:"status"`
	SourceChannel    string `form:"source_channel"`
	SourceAccountID  string `form:"source_account_id"`
	IsGroup          *bool  `form:"is_group"`
	OwnerAccount     string `form:"owner_account"`
	ConversationType string `form:"conversation_type"`
}

// GetClueStatisticsRequest 线索统计时间窗（unix 秒）；0 表示不设该侧边界。
type GetClueStatisticsRequest struct {
	Start int64 `form:"start"`
	End   int64 `form:"end"`
}

type DeleteClueRequest struct {
	ID string `uri:"id" binding:"required"`
}

// ImportClueRequest 导入线索请求
type ImportClueRequest struct {
	Name           string `json:"name"`
	Account        string `json:"account"`
	Type           string `json:"type"`
	City           string `json:"city"`
	Address        string `json:"address"`
	Desc           string `json:"desc"`
	SourceID       string `json:"source_id"`
	OneID          string `json:"one_id"`
	ConversationID string `json:"conversation_id"`
	OwnerAccount   string `json:"owner_account"`
	IsVerify       int64  `json:"is_verify"`
	IntentScore    int64  `json:"intent_score"`
	IsOpportunity  int64  `json:"is_opportunity"`
	// SourceEvent 固定来源/活动/UTM/告知文本版本；SourceEvidenceRef 是已保存许可证据引用。
	// MarketingConsent 只是提交选项，无 SourceEvidenceRef 不得进入营销受众。
	SourceEvent       string `json:"source_event"`
	SourceEvidenceRef string `json:"source_evidence_ref"`
	MarketingConsent  bool   `json:"marketing_consent"`
}

// UpdateClueRequest 单条线索编辑/改派请求。
// 指针语义：nil = 不改；显式串 = 更新（空串 = 清空该字段，如清除跟进人）。
// account/type 不在白名单内：二者参与来源去重与渠道语义，禁止单条改写。
type UpdateClueRequest struct {
	Name         *string `json:"name"`
	City         *string `json:"city"`
	Address      *string `json:"address"`
	Desc         *string `json:"desc"`
	OwnerAccount *string `json:"owner_account"`
}
