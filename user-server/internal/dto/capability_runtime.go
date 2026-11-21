package dto

import "time"

// CapabilityRuntimeView 单条能力的运行视图（D01/F01）。
//
// 判定分层，禁止用一个 enabled 掩盖全部状态：
//   - implemented：本二进制存在该平台×能力的真实执行路径（capregistry 声明 ≠ 实现）；
//   - configured：存在账号/连接器配置；该字段只描述配置状态；
//   - ready：桥接发送需要当前精确店铺动作授权，其他现有能力沿用 implemented && configured；
//   - reason_code：非 ready 时的机器可读原因，包括已配置但缺店铺映射的 capability_mapping_missing。
type CapabilityRuntimeView struct {
	Platform    string `json:"platform"`
	Capability  string `json:"capability"`
	Via         string `json:"via"`
	Note        string `json:"note,omitempty"`
	Implemented bool   `json:"implemented"`
	Configured  bool   `json:"configured"`
	Ready       bool   `json:"ready"`
	ReasonCode  string `json:"reason_code,omitempty"`
}

// CapabilityRuntime 全部登记能力的运行视图
type CapabilityRuntime struct {
	Capabilities []CapabilityRuntimeView `json:"capabilities"`
	Total        int                     `json:"total"`
	ReadyCount   int                     `json:"ready_count"`
}

// CapabilityCellResolveRequest queries one exact shop/action cell. No authorization
// evidence or secrets are accepted over this read-only request.
type CapabilityCellResolveRequest struct {
	ShopID  string `form:"shop_id" binding:"required,max=128"`
	Product string `form:"product" binding:"required,max=64"`
	Region  string `form:"region" binding:"required,max=32"`
	Action  string `form:"action" binding:"required,max=64"`
	Carrier string `form:"carrier" binding:"required,max=64"`
}

type CapabilityCellOption struct {
	Product string `json:"product"`
	Region  string `json:"region"`
	Action  string `json:"action"`
	Carrier string `json:"carrier"`
}

// CapabilityCellSummary is the allowlisted admin display and export payload.
type CapabilityCellSummary struct {
	CapabilityCellOption
	TenantID                     uint      `json:"tenant_id"`
	ShopID                       string    `json:"shop_id"`
	ExecutionMode                string    `json:"execution_mode"`
	ConnectionState              string    `json:"connection_state"`
	Status                       string    `json:"status"`
	ProductionAllowed            bool      `json:"production_allowed"`
	RequiredScopes               []string  `json:"required_scopes"`
	ContractVersion              string    `json:"contract_version"`
	DocumentationVersion         string    `json:"documentation_version"`
	ObservedScopes               []string  `json:"observed_scopes,omitempty"`
	ObservedVersion              string    `json:"observed_version,omitempty"`
	ObservedDocumentationVersion string    `json:"observed_documentation_version,omitempty"`
	StateLimits                  []string  `json:"state_limits"`
	ReasonCode                   string    `json:"reason_code"`
	Owner                        string    `json:"owner"`
	NextStep                     string    `json:"next_step"`
	ValidUntil                   time.Time `json:"valid_until"`
}
