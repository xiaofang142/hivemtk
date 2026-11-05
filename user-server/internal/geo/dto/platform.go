package dto

import "time"

// PlatformInfo 支持的发布平台信息
type PlatformInfo struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	URL         string `json:"url"`
	AuthType    string `json:"auth_type"`
	Enabled     bool   `json:"enabled"`
	// Capability 真实能力标记：real_api / cookie_gray / stub / disabled
	Capability string `json:"capability"`
	// HasAccount DB 中是否已配置该平台的有效账号
	HasAccount bool `json:"has_account"`
}

// PlatformAccountResponse 平台账号响应（脱敏：绝不回显明文凭据）
type PlatformAccountResponse struct {
	ID             string    `json:"id"`
	Platform       string    `json:"platform"`
	AccountID      string    `json:"account_id"`
	AccountName    string    `json:"account_name"`
	Status         string    `json:"status"`
	HasCredentials bool      `json:"has_credentials"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// SavePlatformAccountRequest 保存平台账号请求
type SavePlatformAccountRequest struct {
	Platform  string `json:"platform" binding:"required"`
	AccountID string `json:"account_id"`
	// AccountName 是展示名；前端三段式表单没有该字段。留空时服务端回落到
	// Platform（同平台默认账号），不能把「没填展示名」判成非法请求。
	AccountName string            `json:"account_name"`
	Status      string            `json:"status"`
	Credentials map[string]string `json:"credentials"`
	// 兼容前端现有的三段式表单：按平台映射到 Credentials。
	// github: user=app_key, access_token=secret；wordpress: username/password；
	// webhook: secret；其它平台 api_key/access_token。
	AppKey string `json:"app_key"`
	Secret string `json:"secret"`
	// Extra 是 JSON 对象字符串，用于 endpoint/url 等平台专属配置。
	Extra string `json:"extra"`
}

// PublishRequest 平台发布请求
type PublishRequest struct {
	ArticleID string `json:"article_id" binding:"required"`
	Platform  string `json:"platform" binding:"required"`
	Repo      string `json:"repo"`
	Path      string `json:"path"`
	Branch    string `json:"branch"`
	// AuthToken 兼容回退字段：服务端优先取用已保存账号凭据，其次环境变量，
	// 仅两者皆缺时才使用本字段（避免 token 明文传输与日志残留）
	AuthToken string `json:"auth_token"`
	CommitMsg string `json:"commit_message"`
	Filename  string `json:"filename"`
}

// PublishResponse 平台发布响应
type PublishResponse struct {
	ID           string `json:"id"`
	Status       string `json:"status"`
	PublishedURL string `json:"published_url"`
	Message      string `json:"message,omitempty"`
}
