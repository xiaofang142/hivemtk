// Package delivery 定义推理外发的持久受理与外部确认契约，不依赖运行时或服务层。
package delivery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"hivemtk-user/internal/pkg/tenant"
)

type State string

const (
	Intended  State = "intended"
	Blocked   State = "blocked"
	Sending   State = "sending"
	Unknown   State = "unknown"
	Accepted  State = "accepted"
	Confirmed State = "confirmed"
	Rejected  State = "rejected"
	Absent    State = "absent" // 仅外部查询结果，不是账本状态。
)

var (
	ErrTenant      = errors.New("delivery_tenant_required")
	ErrIdentity    = errors.New("delivery_identity_required")
	ErrConflict    = errors.New("delivery_intent_conflict")
	ErrUnavailable = errors.New("delivery_store_unavailable")
	ErrNotFound    = errors.New("delivery_not_found")
	ErrLease       = errors.New("delivery_lease_lost")
)

type Intent struct {
	MessageID    string `json:"message_id"`
	Channel      string `json:"channel"`
	AccountID    string `json:"account_id"`
	CustomerID   string `json:"customer_id"`
	SessionID    string `json:"session_id"`
	AgentID      uint   `json:"agent_id"`
	AgentCode    string `json:"agent_code"`
	AgentVersion int    `json:"agent_version"`
	Kind         string `json:"kind"` // reply 或 handoff。
	Content      string `json:"content"`
}

type Record struct {
	NewAccepted    bool       `json:"-"` // 仅本次持久状态迁移，不在重复查询时再次计数。
	NewConfirmed   bool       `json:"-"`
	Key            string     `json:"key"`
	State          State      `json:"state"`
	Kind           string     `json:"kind"`
	SendAttempts   int        `json:"send_attempts"` // 持久化发送尝试；外部受理次数以回执为准。
	AcceptedCount  int        `json:"accepted_count"`
	ConfirmedCount int        `json:"confirmed_count"`
	ReceiptID      string     `json:"receipt_id,omitempty"`
	ErrorCode      string     `json:"error_code,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	AcceptedAt     *time.Time `json:"accepted_at,omitempty"`
	ConfirmedAt    *time.Time `json:"confirmed_at,omitempty"`
}

type Gateway interface {
	Submit(context.Context, Intent) (*Record, error)
	Get(context.Context, string) (*Record, error)
	Retry(context.Context, string) (*Record, error)
}

type Receipt struct {
	State State
	ID    string
}

// Sender 的实现必须把同一租户及键的 Send 原子去重，并保证 Lookup 的 Absent 是权威结果。
// 不具备这两项能力的旧发布器/交接触发器不能适配为可靠发送端。
type Sender interface {
	Send(context.Context, string, Intent) (Receipt, error)
	Lookup(context.Context, string, Intent) (Receipt, error)
}

type Policy interface {
	AuthorizeDelivery(context.Context, Intent) (bool, error)
}

// Identity 不包含正文：同一消息同一动作不能靠修改正文获得新键。
func Identity(ctx context.Context, intent Intent) (string, error) {
	id, ok := tenant.FromContext(ctx)
	if !ok {
		return "", ErrTenant
	}
	for _, v := range []string{intent.MessageID, intent.Channel, intent.AccountID, intent.CustomerID, intent.SessionID, intent.AgentCode} {
		if strings.TrimSpace(v) == "" || len(v) > 512 || strings.TrimSpace(v) != v {
			return "", ErrIdentity
		}
	}
	if intent.AgentVersion < 0 || (intent.Kind != "reply" && intent.Kind != "handoff") {
		return "", ErrIdentity
	}
	copy := intent
	copy.Content = ""
	body, _ := json.Marshal(struct {
		Tenant uint
		Intent Intent
	}{id, copy})
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

func Fingerprint(intent Intent) string {
	body, _ := json.Marshal(intent)
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
