package tracing

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
)

const TimingVersion = 1

// CarrierForHub 将独立poll/ack请求恢复到服务端持久化的同轮归属，不修改父载体。
func CarrierForHub(ctx context.Context, hub *model.MessageHub) *Carrier {
	parent := CarrierFromContext(ctx)
	c := parent.Child()
	if hub == nil {
		return c
	}
	contextTenant, _ := tenant.FromContext(ctx)
	hubTenant := hub.TenantID
	if hubTenant == 0 {
		hubTenant = contextTenant
	}
	if contextTenant != 0 && hubTenant != contextTenant {
		return &Carrier{}
	}
	// 继承的turn只有同一业务范围才可继续使用，不能从另一个会话借关联。
	if parent == nil || (parent.TenantID != 0 && parent.TenantID != hubTenant) || parent.Channel != hub.Platform || parent.AccountID != hub.AccountID || parent.ConversationID != hub.ConversationID {
		c.TurnID, c.AnswerAttempt, c.DeliveryAttempt = "", 0, 0
	}
	c.TenantID = hubTenant
	c.TraceID = hub.TraceID
	// 临时入站对象可能尚无持久追踪；只能保留同一权威入站身份的父追踪。
	// 缺范围或另一条消息不能借用同会话的关联，更不能跨租户继承。
	sameTenant := contextTenant > 0 && hubTenant == contextTenant && parent != nil && parent.TenantID == hubTenant
	completeScope := hub.Platform != "" && hub.AccountID != "" && hub.ConversationID != "" && hub.MsgID != ""
	sameScope := parent != nil && parent.Channel == hub.Platform && parent.AccountID == hub.AccountID && parent.ConversationID == hub.ConversationID
	sameInbound := parent != nil && parent.MsgID == hub.MsgID && parent.Direction == "inbound" && hub.Direction == "inbound"
	if c.TraceID == "" && sameTenant && completeScope && sameScope && sameInbound {
		c.TraceID = parent.TraceID
	}
	c.Channel, c.AccountID, c.ConversationID = hub.Platform, hub.AccountID, hub.ConversationID
	c.MsgID, c.Direction = hub.MsgID, hub.Direction
	if hub.Direction == "inbound" {
		tenantID := hub.TenantID
		if tenantID == 0 {
			tenantID, _ = tenant.FromContext(ctx)
		}
		c.TurnID = BusinessTurnID(tenantID, hub.Platform, hub.AccountID, hub.ConversationID, hub.MsgID)
	} else if hub.Extra != nil {
		if id, ok := hub.Extra["turn_id"].(string); ok && len(id) == 64 {
			if _, err := hex.DecodeString(id); err == nil {
				c.TurnID = id
			}
		}
		c.AnswerAttempt = observationInt(hub.Extra["answer_attempt"])
	}
	c.DeliveryAttempt = hub.PushAttempts
	if hubTenant == 0 || hub.Platform == "" || hub.AccountID == "" || hub.ConversationID == "" {
		c.TurnID, c.AnswerAttempt, c.DeliveryAttempt = "", 0, 0
	}
	return c
}

func observationInt(value any) int {
	switch v := value.(type) {
	case int:
		if v > 0 && v < 1000 {
			return v
		}
	case float64:
		if v > 0 && v < 1000 && v == float64(int(v)) {
			return int(v)
		}
	}
	return 0
}

// BusinessTurnID 只使用完整权威作用域派生同轮身份，不以正文或昵称猜测缺失身份。
func BusinessTurnID(
	tenantID uint,
	channel string,
	accountID string,
	conversationID string,
	inboundID string,
) string {
	if tenantID == 0 || channel == "" || accountID == "" || conversationID == "" || inboundID == "" {
		return ""
	}
	data, _ := json.Marshal([]any{tenantID, channel, accountID, conversationID, inboundID})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// WithAnswerAttempt 返回副本，工具层继承生成尝试但不修改父上下文。
func (c *Carrier) WithAnswerAttempt(attempt int) *Carrier {
	cp := c.Child()
	cp.AnswerAttempt = attempt
	return cp
}

// observationTimes 保持调用方的操作时间；缺测不由写库时间反推。
func observationTimes(started, completed *time.Time, measured bool, durationMs int64) (*time.Time, *time.Time, bool) {
	if durationMs < 0 {
		return nil, nil, false
	}
	if started != nil && completed != nil {
		if completed.Before(*started) {
			return nil, nil, false
		}
		start, end := *started, *completed
		return &start, &end, true
	}
	return nil, nil, measured
}

func sensitiveTraceKey(key string) bool {
	normalized := strings.ToLower(strings.NewReplacer("-", "", "_", "", " ", "").Replace(key))
	return strings.Contains(normalized, "password") || strings.Contains(normalized, "secret") ||
		strings.Contains(normalized, "authorization") || strings.Contains(normalized, "cookie") ||
		strings.Contains(normalized, "apikey") || strings.Contains(normalized, "merchantkey") ||
		strings.Contains(normalized, "token")
}

func scrubTraceValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, child := range v {
			normalized := strings.ToLower(strings.NewReplacer("-", "", "_", "", " ", "").Replace(key))
			isCount := normalized == "prompttokens" || normalized == "completiontokens" || normalized == "totaltokens"
			number, numeric := child.(float64)
			if isCount && numeric && number >= 0 && number == float64(int64(number)) {
				out[key] = child
			} else if sensitiveTraceKey(key) {
				out[key] = "[redacted]"
			} else {
				out[key] = scrubTraceValue(child)
			}
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, child := range v {
			out[i] = scrubTraceValue(child)
		}
		return out
	case string:
		var nested any
		trimmed := strings.TrimSpace(v)
		if (strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")) && json.Unmarshal([]byte(trimmed), &nested) == nil {
			data, _ := json.Marshal(scrubTraceValue(nested))
			return string(data)
		}
		return safeTraceText(v)
	default:
		return value
	}
}

// safeTraceText 对可能带凭证的非结构化文本保守摘要；不保存凭证片段。
func safeTraceText(value string) string {
	lower := strings.ToLower(value)
	for _, marker := range []string{"bearer ", "authorization", "password", "cookie", "access_token", "api_key", "merchant_key", "x-bridge-token", "x-channel-identify-code", "identify_code", "secret="} {
		if strings.Contains(lower, marker) {
			return "[redacted credential-bearing text]"
		}
	}
	return value
}

// tracePayloadJSON 保留既有授权学习载荷，移除凭证；巨大JSON整体摘要而非破损截断。
func tracePayloadJSON(value any) string {
	if value == nil {
		return ""
	}
	var raw []byte
	switch v := value.(type) {
	case string:
		raw = []byte(v)
	case []byte:
		raw = v
	default:
		var err error
		raw, err = json.Marshal(value)
		if err != nil {
			return fmt.Sprintf(`{"serialization_failed":true,"type":%q}`, fmt.Sprintf("%T", value))
		}
	}
	var decoded any
	if json.Unmarshal(raw, &decoded) == nil {
		raw, _ = json.Marshal(scrubTraceValue(decoded))
	} else {
		raw = []byte(safeTraceText(string(raw)))
	}
	if len(raw) <= 8192 {
		return string(raw)
	}
	sum := sha256.Sum256(raw)
	summary, _ := json.Marshal(map[string]any{"truncated": true, "bytes": len(raw), "sha256": hex.EncodeToString(sum[:])})
	return string(summary)
}
