// security_access_failure.go 未授权访问尝试的部署级聚合载荷。
//
// 存储载体是 platform_config_kv（部署级 KV，无租户维度）：未授权访问的"归属人"
// 是攻击者而不是任何租户，落租户表要么串库要么 fail-closed 整批拒写（R-8 同一学费）。
// 遥测按键 `security_access_failures_v1` 聚合，写入方是 middleware/security_recorder.go
// （JWT 401 五类），读取方是 service/security_overview.go（安全总览端点）。
package model

import "encoding/json"

// SecurityAccessFailureKVKey platform_config_kv 里的聚合键。
const SecurityAccessFailureKVKey = "security_access_failures_v1"

// JWT 401 的五类失败形态（与 middleware/jwt.go 的五个 abort 点一一对应）。
const (
	SecurityAccessKindTokenMissing  = "token_missing"
	SecurityAccessKindTokenFormat   = "token_format"
	SecurityAccessKindTokenInvalid  = "token_invalid"
	SecurityAccessKindTokenExpired  = "token_expired"
	SecurityAccessKindStatusChanged = "status_changed"
)

// SecurityAccessFailureDay 一天的失败聚合计数。
type SecurityAccessFailureDay struct {
	Kinds map[string]int64 `json:"kinds"` // 失败形态 → 次数
	Paths map[string]int64 `json:"paths"` // 路径前缀（两段）→ 次数
}

// SecurityAccessFailurePayload kv 里存的整包：日期 → 当日聚合。只保留最近 7 天。
type SecurityAccessFailurePayload struct {
	Days map[string]*SecurityAccessFailureDay `json:"days"`
}

// NewSecurityAccessFailurePayload 空载荷。
func NewSecurityAccessFailurePayload() *SecurityAccessFailurePayload {
	return &SecurityAccessFailurePayload{Days: map[string]*SecurityAccessFailureDay{}}
}

// ParseSecurityAccessFailurePayload 宽松解析：空串/坏 JSON 回空载荷（遥测读侧
// 不因历史脏数据 500），只有调用方自己区分"没数据"与"坏了"。
func ParseSecurityAccessFailurePayload(raw string) *SecurityAccessFailurePayload {
	p := NewSecurityAccessFailurePayload()
	if raw == "" {
		return p
	}
	_ = json.Unmarshal([]byte(raw), p)
	if p.Days == nil {
		p.Days = map[string]*SecurityAccessFailureDay{}
	}
	return p
}
