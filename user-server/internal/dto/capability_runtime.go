package dto

// CapabilityRuntimeView 单条能力的运行视图（D01/F01）。
//
// 判定分层，禁止用一个 enabled 掩盖全部状态：
//   - implemented：本二进制存在该平台×能力的真实执行路径（capregistry 声明 ≠ 实现）；
//   - configured：已配置可用账号/连接器（实现存在但未配置时 configured=false，而非 implemented=false）；
//   - ready：implemented && configured；
//   - reason_code：非 ready 时的机器可读原因（"no_runtime_path" / "not_configured_no_account"）。
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
