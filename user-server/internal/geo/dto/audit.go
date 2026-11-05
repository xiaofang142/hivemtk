package dto

// AuditSiteRequest 站点体检请求。
// scope=page 为单页（v2 当前实现）；scope=site 由后续全站审计扩展消费。
type AuditSiteRequest struct {
	URL      string `json:"url" binding:"required"`
	Brand    string `json:"brand"`
	Scope    string `json:"scope"`
	MaxPages int    `json:"max_pages"`
}

// AuditContentRequest 内容体检请求。
// content 为空且 url 非空时，服务端抓取页面正文后按 25 因子评分。
type AuditContentRequest struct {
	URL          string `json:"url"`
	Title        string `json:"title"`
	Content      string `json:"content"`
	MetaDesc     string `json:"meta_desc"`
	SchemaJSONLD string `json:"schema_jsonld"`
	Brand        string `json:"brand"`
	Live         bool   `json:"live"`
}

// AuditRunBrief 列表项（不返回 categories/fixes 大 JSON）。
type AuditRunBrief struct {
	ID            string `json:"id"`
	Kind          string `json:"kind"`
	Scope         string `json:"scope"`
	URL           string `json:"url"`
	Brand         string `json:"brand"`
	RubricVersion string `json:"rubric_version"`
	Score         int    `json:"score"`
	MaxScore      int    `json:"max_score"`
	Grade         string `json:"grade"`
	Source        string `json:"source"`
	PageCount     int    `json:"page_count"`
	CreatedAt     string `json:"created_at"`
}

// AuditRunListResponse 历史分页结果。
type AuditRunListResponse struct {
	List  []AuditRunBrief `json:"list"`
	Total int64           `json:"total"`
}
