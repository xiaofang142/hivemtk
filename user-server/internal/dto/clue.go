package dto

type GetClueListResponse struct {
	Total int64               `json:"total"`
	List  []*ClueListResponse `json:"list"`
}

type ClueListResponse struct {
	ID       string `json:"id"`
	SourceID string `json:"source_id"`
	Account  string `json:"account"`
	IsVerify int64  `json:"is_verify"`
	Type     int64  `json:"type"`
	Name     string `json:"name"`
	City     string `json:"city"`
	Address  string `json:"address"`
}

type GetClueRequest struct {
	Page     int `form:"page"`
	PageSize int `form:"limit"`
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
}
