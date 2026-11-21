package dto

// PilotContract 是单次试点的版本化配置草稿，不是平台执行授权。
type PilotContract struct {
	Version              string            `json:"version"`
	Revision             uint64            `json:"revision"`
	InstallID            string            `json:"install_id"`
	TenantID             uint              `json:"tenant_id"`
	MerchantType         string            `json:"merchant_type"`
	Shop                 PilotShop         `json:"shop"`
	ReadScope            []string          `json:"read_scope"`
	WriteScope           []string          `json:"write_scope"`
	FundsLimit           *PilotFundsLimit  `json:"funds_limit"`
	TestAuthorizer       string            `json:"test_authorizer"`
	ObservationMinutes   int               `json:"observation_minutes"`
	RollbackOwner        string            `json:"rollback_owner"`
	Action               PilotAction       `json:"action"`
	Dependencies         []PilotDependency `json:"dependencies"`
	ProviderMode         string            `json:"provider_mode"`
	EngineeringMinutes   int               `json:"engineering_minutes"`
	PlatformApprovalWait string            `json:"platform_approval_wait"`
}

type PilotShop struct {
	Platform         string `json:"platform"`
	ShopID           string `json:"shop_id"`
	OwnerRef         string `json:"owner_ref"`
	AuthorizationRef string `json:"authorization_ref"`
}

type PilotFundsLimit struct {
	Currency    string `json:"currency"`
	AmountMinor *int64 `json:"amount_minor"`
}

type PilotAction struct {
	Name             string   `json:"name"`
	OrderID          string   `json:"order_id"`
	Postconditions   []string `json:"postconditions"`
	ResponsibleOwner string   `json:"responsible_owner"`
}

type PilotDependency struct {
	ID           string `json:"id"`
	CloseCommit  string `json:"close_commit"`
	TestEvidence string `json:"test_evidence"`
}

type PilotInstallation struct {
	InstallID   string `json:"install_id"`
	Version     string `json:"version"`
	Initialized bool   `json:"initialized"`
}

type PilotCapabilityDeclaration struct {
	Platform   string `json:"platform"`
	Capability string `json:"capability"`
	Via        string `json:"via"`
}

type PilotContractResponse struct {
	Contract           PilotContract                `json:"contract"`
	Installation       PilotInstallation            `json:"installation"`
	TenantID           uint                         `json:"tenant_id"`
	Configured         bool                         `json:"configured"`
	BaselineAvailable  bool                         `json:"baseline_available"`
	Readiness          string                       `json:"readiness"`
	ProductionGate     string                       `json:"production_gate"`
	ValidationSignal   string                       `json:"validation_signal"`
	Simulated          bool                         `json:"simulated"`
	Blockers           []string                     `json:"blockers"`
	ProductionBlockers []string                     `json:"production_blockers"`
	Capabilities       []PilotCapabilityDeclaration `json:"capabilities"`
	UpdatedBy          uint                         `json:"updated_by"`
	UpdatedAt          string                       `json:"updated_at"`
}
