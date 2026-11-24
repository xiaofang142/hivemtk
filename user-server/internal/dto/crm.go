package dto

// CRMCustomerSaveRequest 客户新增/编辑入参。
type CRMCustomerSaveRequest struct {
	Name            string `json:"name"`
	Phone           string `json:"phone"`
	Email           string `json:"email"`
	Industry        string `json:"industry"`
	SourceAccountID string `json:"source_account_id"`
	SourceChannel   string `json:"source_channel"`
	OwnerUserID     uint   `json:"owner_user_id"`
	Tags            string `json:"tags"`
}

// CRMEntitySaveRequest 联系人/供应商/服务商共用入参。
type CRMEntitySaveRequest struct {
	Name             string `json:"name" binding:"required"`
	CustomerID       string `json:"customer_id" binding:"required"`
	ChannelAccountID *uint  `json:"channel_account_id,omitempty"`
	ContactInfo      string `json:"contact_info"`
	Role             string `json:"role"`
	Remark           string `json:"remark"`
	Source           string `json:"source"`
	SourceRef        string `json:"source_ref"`
}

// CRMDedupeScanRequest 去重扫描入参。
type CRMDedupeScanRequest struct {
	MaxCustomers    int  `json:"max_customers"`
	NameThreshold   int  `json:"name_threshold"`
	SameAccountOnly bool `json:"same_account_only"`
}

// CRMMergeRequest 合并客户入参。
type CRMMergeRequest struct {
	PrimaryID    string `json:"primary_id" binding:"required"`
	SecondaryID  string `json:"secondary_id" binding:"required"`
	OperatorName string `json:"operator_name"`
	Reason       string `json:"reason"`
	Source       string `json:"source"`
}

// CRMImportRowDTO 一行客户导入数据。
type CRMImportRowDTO struct {
	Name            string `json:"name"`
	Phone           string `json:"phone"`
	Email           string `json:"email"`
	Industry        string `json:"industry"`
	SourceAccountID string `json:"source_account_id"`
	SourceChannel   string `json:"source_channel"`
	OwnerUserID     uint   `json:"owner_user_id"`
	Tags            string `json:"tags"`
}

// CRMImportRequest 客户导入入参。
type CRMImportRequest struct {
	Rows     []CRMImportRowDTO `json:"rows" binding:"required"`
	FileName string            `json:"file_name"`
}

// CRMArchiveClueRequest US2 归档对接入参（T031）。
type CRMArchiveClueRequest struct {
	ClueID          string `json:"clue_id" binding:"required"`
	CustomerID      string `json:"customer_id"`
	Phone           string `json:"phone"`
	Email           string `json:"email"`
	WechatOpenID    string `json:"wechat_open_id"`
	DouyinOpenID    string `json:"douyin_open_id"`
	XiaohongshuID   string `json:"xiaohongshu_id"`
	Name            string `json:"name"`
	Industry        string `json:"industry"`
	SourceAccountID string `json:"source_account_id"`
	SourceChannel   string `json:"source_channel"`
	OwnerUserID     uint   `json:"owner_user_id"`
	Summary         string `json:"summary"`
	EvidenceJSON    string `json:"evidence_json"`
}
