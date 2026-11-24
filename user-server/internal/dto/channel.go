package dto

import "time"

// ChannelOption 渠道下拉项（所有内部渠道选择统一走本结构）。
type ChannelOption struct {
	ID      uint   `json:"id"`
	Name    string `json:"name"`
	Code    string `json:"code"`
	Type    string `json:"type"`
	Enabled bool   `json:"enabled"`
}

// ChannelAccountOption 渠道账号下拉项（FR-029 唯一出参形态）。
type ChannelAccountOption struct {
	ID               uint   `json:"id"`
	ChannelID        uint   `json:"channel_id"`
	Name             string `json:"name"`
	IdentifyCode     string `json:"identify_code"`
	Type             string `json:"type"`
	Status           int    `json:"status"`
	SelfIdentifyName string `json:"self_identify_name"`
	// 两类智能体绑定（FR-024/025）：绑定关系收口在渠道账号，智能体管理里不再绑账号。
	AutoReplyAgentID  *uint `json:"auto_reply_agent_id"`
	LeadMiningAgentID *uint `json:"lead_mining_agent_id"`
}

// ChannelAccountDetail 渠道账号完整出参（管理面编辑回填用，FR-024/027）。
// 比 ChannelAccountOption 多带 remark / owner_user_ids / data_scope / 时间戳。
type ChannelAccountDetail struct {
	ID                uint     `json:"id"`
	ChannelID         uint     `json:"channel_id"`
	Name              string   `json:"name"`
	Remark            string   `json:"remark"`
	SelfIdentifyName  string   `json:"self_identify_name"`
	Type              string   `json:"type"`
	AutoReplyAgentID  *uint    `json:"auto_reply_agent_id"`
	LeadMiningAgentID *uint    `json:"lead_mining_agent_id"`
	OwnerUserIDs      []string `json:"owner_user_ids"`
	DataScope         string   `json:"data_scope"`
	IdentifyCode      string   `json:"identify_code"`
	Status            int      `json:"status"`
	CreatedAt         string   `json:"created_at,omitempty"`
	UpdatedAt         string   `json:"updated_at,omitempty"`
}

// ChannelIdentifyCodeView 识别码出参（明文仅管理面返回，解析侧只按 code 查）。
type ChannelIdentifyCodeView struct {
	ID        uint       `json:"id"`
	ChannelID uint       `json:"channel_id"`
	Code      string     `json:"code"`
	Version   int        `json:"version"`
	ResetAt   *time.Time `json:"reset_at,omitempty"`
}

// ChannelSave 渠道新增/编辑入参。
type ChannelSave struct {
	Name    string `json:"name" binding:"required"`
	Code    string `json:"code" binding:"required"`
	Type    string `json:"type"`
	Enabled *bool  `json:"enabled"`
	Remark  string `json:"remark"`
}

// ChannelAccountSave 渠道账号新增/编辑入参（两类智能体绑定收口在此）。
type ChannelAccountSave struct {
	ChannelID         uint     `json:"channel_id" binding:"required"`
	Name              string   `json:"name" binding:"required"`
	Remark            string   `json:"remark"`
	SelfIdentifyName  string   `json:"self_identify_name"`
	Type              string   `json:"type"`
	AutoReplyAgentID  *uint    `json:"auto_reply_agent_id"`
	LeadMiningAgentID *uint    `json:"lead_mining_agent_id"`
	OwnerUserIDs      []string `json:"owner_user_ids"`
	DataScope         string   `json:"data_scope"`
	Status            *int     `json:"status"`
}

// ChannelListQuery 渠道列表查询条件（Controller 入参，Service 负责映射为仓储选项）。
type ChannelListQuery struct {
	Keyword string
	Enabled *bool
	Limit   int
	Offset  int
}

// ChannelAccountListQuery 渠道账号列表/下拉查询条件。
type ChannelAccountListQuery struct {
	ChannelID uint
	Status    *int
	Keyword   string
	IDs       []uint
	Limit     int
	Offset    int
}
