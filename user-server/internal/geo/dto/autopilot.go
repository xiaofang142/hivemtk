package dto

// AutopilotCreateRequest 一键自动驾驶启动请求。
// 只有一个必填项 keyword——傻瓜式约束：其余全部可选。
type AutopilotCreateRequest struct {
	Keyword      string `json:"keyword" binding:"required"`
	SiteURL      string `json:"site_url"`
	ArticleCount int    `json:"article_count"`
}

// AutopilotRunResponse 运行记录响应（steps 由模型 JSON 字段直出）
type AutopilotRunResponse struct {
	ID           string  `json:"id"`
	Keyword      string  `json:"keyword"`
	SiteURL      string  `json:"site_url"`
	ArticleCount int     `json:"article_count"`
	Status       string  `json:"status"`
	Progress     int     `json:"progress"`
	CurrentStep  string  `json:"current_step"`
	Steps        string  `json:"steps"`
	Summary      string  `json:"summary"`
	Error        string  `json:"error"`
	StartedAt    string  `json:"started_at"`
	FinishedAt   *string `json:"finished_at,omitempty"`
}

// AutopilotListResponse 列表响应
type AutopilotListResponse struct {
	List  []AutopilotRunResponse `json:"list"`
	Total int64                  `json:"total"`
}
