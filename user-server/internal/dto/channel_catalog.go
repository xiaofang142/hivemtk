package dto

// ChannelCatalogItem 统一渠道目录中的渠道项（仅已启用）。
type ChannelCatalogItem struct {
	ID      uint   `json:"id"`
	Code    string `json:"code"`
	Name    string `json:"name"`
	Label   string `json:"label"`
	Enabled bool   `json:"enabled"`
	Type    string `json:"type"`
}

// ChannelAccountCatalogItem 统一渠道目录中的账号项（仅已启用且所属渠道已启用）。
type ChannelAccountCatalogItem struct {
	ID           uint   `json:"id"`
	ChannelID    uint   `json:"channel_id"`
	ChannelCode  string `json:"channel_code"`
	Name         string `json:"name"`
	Label        string `json:"label"`
	IdentifyCode string `json:"identify_code"`
	// SelfIdentifyName 每个账号的上报发送者名称标记（自他判断弱提示）。
	SelfIdentifyName string `json:"self_identify_name"`
	Type             string `json:"type"`
}

// ChannelCatalogResponse 一次返回渠道列表 + 渠道下账号列表（统一 API 出参）。
type ChannelCatalogResponse struct {
	Channels []ChannelCatalogItem        `json:"channels"`
	Accounts []ChannelAccountCatalogItem `json:"accounts"`
}

// ChannelToggleSave 租户级渠道开关入参。
type ChannelToggleSave struct {
	Enabled *bool `json:"enabled" binding:"required"`
}

// ChannelAccountManagedSaveDTO 管理面账号编辑入参（nil=不改，非 nil=明确写入）。
type ChannelAccountManagedSaveDTO struct {
	ChannelID        *uint   `json:"channel_id"`
	Name             *string `json:"name"`
	Remark           *string `json:"remark"`
	SelfIdentifyName *string `json:"self_identify_name"`
	Type             *string `json:"type"`
}

// ChannelAccountStatusSave 账号启停入参。
type ChannelAccountStatusSave struct {
	Status *int `json:"status" binding:"required"`
}

// ChannelAuthorizationPairSave 单个账号授权配对入参（行业范围 + 角色）。
type ChannelAuthorizationPairSave struct {
	AccountID  uint     `json:"account_id" binding:"required"`
	Industries []string `json:"industries"`
	Role       string   `json:"role"`
}

// ChannelAuthorizationSave 覆盖式保存某员工在当前租户下的账号授权。
type ChannelAuthorizationSave struct {
	Accounts []ChannelAuthorizationPairSave `json:"accounts" binding:"required"`
}
