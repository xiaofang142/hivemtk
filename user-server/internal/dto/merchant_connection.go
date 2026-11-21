package dto

import "time"

// MerchantConnectionTarget selects an expected shop. Only the provider exchange can verify it.
type MerchantConnectionTarget struct {
	Provider string `json:"provider"`
	ShopID   string `json:"shop_id"`
}

type MerchantConnectionCompleteRequest struct {
	MerchantConnectionTarget
	Code  string `json:"code"`
	State string `json:"state"`
}

type MerchantConnectionRefreshRequest struct {
	MerchantConnectionTarget
	CredentialVersion int64 `json:"credential_version"`
}

type MerchantConnectionStartResponse struct {
	AuthorizationURL string    `json:"authorization_url"`
	ExpiresAt        time.Time `json:"expires_at"`
}

type MerchantConnectionResponse struct {
	Provider          string     `json:"provider"`
	ShopID            string     `json:"shop_id,omitempty"`
	CredentialRef     uint       `json:"credential_ref,omitempty"`
	CredentialVersion int64      `json:"credential_version,omitempty"`
	ConnectionState   string     `json:"connection_state"`
	ReasonCode        string     `json:"reason_code,omitempty"`
	Scopes            []string   `json:"scopes,omitempty"`
	ExpiresAt         *time.Time `json:"expires_at,omitempty"`
	RefreshAvailable  bool       `json:"refresh_available"`
	ProductionAllowed bool       `json:"production_allowed"`
}
