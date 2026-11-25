package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"hivemtk-user/internal/model"
)

// CRM-15/16/17 原型：这些判据仅有单元测试调用，尚未接入生产通知链。
// 内存 outbox 不提供进程重启持久性；保留待集成代码不代表生产能力已交付。

// leadNotifyDeliveryRecord 一条按事件/收件人/渠道追加的业务送达记录。
type leadNotifyDeliveryRecord struct { //nolint:unused // 保留 CRM-15 送达记录原型，生产链尚未接入。
	TenantID    uint   `json:"tenant_id"`
	ClueID      string `json:"clue_id"`
	EventID     string `json:"event_id"`
	Recipient   string `json:"recipient"`
	Channel     string `json:"channel"`
	RouteID     string `json:"route_id,omitempty"`
	RouteScope  string `json:"route_scope,omitempty"`
	RouteRuleID string `json:"route_rule_id,omitempty"`
	Success     bool   `json:"success"`
	Status      string `json:"status,omitempty"`
	Error       string `json:"error,omitempty"`
	MaskPolicy  string `json:"mask_policy,omitempty"`
	At          int64  `json:"at"`
}

func modelLeadNotifyRouteWithHistory(raw string) *model.LeadMiningNotifyRoute { //nolint:unused // 仅供通知原型历史记录测试使用。
	return &model.LeadMiningNotifyRoute{DeliveryRecords: raw}
}

// appendLeadNotifyDeliveryRecords 把新记录**追加**到路由行既有 JSON 数组后；
// 历史不可被覆盖，空/坏 JSON 从空数组起（保留"曾经没有记录"这一事实）。
func appendLeadNotifyDeliveryRecords(route *model.LeadMiningNotifyRoute, added []map[string]any) string { //nolint:unused // 保留 CRM-15 追加历史判据，尚未接入生产投递。
	records := make([]map[string]any, 0, 8)
	if route != nil {
		var old []map[string]any
		if json.Unmarshal([]byte(route.DeliveryRecords), &old) == nil {
			records = append(records, old...)
		}
	}
	return string(marshalLeadNotifyRecords(append(records, added...)))
}

// marshalLeadNotifyRecords 序列化原型内部记录。
func marshalLeadNotifyRecords(v any) []byte { //nolint:unused // 仅被未接入生产的通知原型调用。
	raw, err := json.Marshal(v)
	if err != nil {
		return []byte("[]")
	}
	return raw
}

// leadNotifyDeliveryStatus 把一组投递结果归一为明确状态（不把空记录当成功）。
func leadNotifyDeliveryStatus(records []map[string]any) string { //nolint:unused // 保留 CRM-15 状态矩阵原型，生产通知链尚未使用。
	if len(records) == 0 {
		return "no_recipient"
	}
	succeeded := 0
	for _, r := range records {
		if ok, _ := r["success"].(bool); ok {
			succeeded++
		}
	}
	switch {
	case succeeded == len(records):
		return "delivered"
	case succeeded > 0:
		return "partial"
	default:
		for _, r := range records {
			if strings.Contains(fmt.Sprint(r["error"]), "no_target") {
				return "no_target"
			}
			if strings.Contains(fmt.Sprint(r["error"]), "no_recipient") {
				return "no_recipient"
			}
		}
		return "failed"
	}
}

// leadNotifyIdempotencyKey 事件/收件人/渠道（含租户与线索）构成 durable 幂等键。
func leadNotifyIdempotencyKey(tenantID uint, clueID, eventID, recipient, channel string) string { //nolint:unused // 保留 CRM-16 幂等键原型，尚无生产 outbox 适配。
	raw := fmt.Sprintf("%d|%s|%s|%s|%s", tenantID, clueID, eventID, recipient, strings.ToLower(strings.TrimSpace(channel)))
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])[:24]
}

type leadNotifyOutboxItem struct { //nolint:unused // CRM-16 内存原型记录，不宣称耐久投递。
	Key           string    `json:"key"`
	TenantID      uint      `json:"tenant_id"`
	ClueID        string    `json:"clue_id"`
	EventID       string    `json:"event_id"`
	Recipient     string    `json:"recipient"`
	Channel       string    `json:"channel"`
	Status        string    `json:"status"`
	Attempts      int       `json:"attempts"`
	LastError     string    `json:"last_error,omitempty"`
	LastAttemptAt time.Time `json:"last_attempt_at,omitempty"`
	NextAttemptAt time.Time `json:"next_attempt_at,omitempty"`
	ReceiptLost   bool      `json:"receipt_lost,omitempty"`
}

// memoryLeadNotifyOutbox 是仅用于原型测试的内存仓储。
// 共享 map 只能证明实例重建可读，不能证明进程重启恢复；生产接入需要持久化适配。
type memoryLeadNotifyOutbox struct { //nolint:unused // 保留其他会话的 CRM-16 原型，尚未接入生产 worker。
	items map[string]leadNotifyOutboxItem
	mu    sync.Mutex
}

func newMemoryLeadNotifyOutbox() *memoryLeadNotifyOutbox { //nolint:unused // 仅供 CRM-16 原型测试构造内存仓储。
	return &memoryLeadNotifyOutbox{items: map[string]leadNotifyOutboxItem{}}
}

func (s *memoryLeadNotifyOutbox) Enqueue(_ context.Context, key string, in leadNotifyOutboxItem) (leadNotifyOutboxItem, bool) { //nolint:unused // 仅供未接入生产的 CRM-16 内存原型使用。
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.items[key]; ok {
		return existing, false
	}
	in.Key = key
	in.Status = "pending"
	s.items[key] = in
	return in, true
}

func (s *memoryLeadNotifyOutbox) Get(_ context.Context, key string) (leadNotifyOutboxItem, bool) { //nolint:unused // 仅供未接入生产的 CRM-16 内存原型使用。
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[key]
	return item, ok
}

func (s *memoryLeadNotifyOutbox) All() []leadNotifyOutboxItem { //nolint:unused // 仅供未接入生产的 CRM-16 内存原型使用。
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]leadNotifyOutboxItem, 0, len(s.items))
	for _, item := range s.items {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func (s *memoryLeadNotifyOutbox) Fail(_ context.Context, key string, err error, attempt int) (leadNotifyOutboxItem, error) { //nolint:unused // 仅供未接入生产的 CRM-16 重试原型使用。
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[key]
	if !ok {
		return item, errors.New("投递意图不存在")
	}
	item.Attempts = attempt
	item.LastError = err.Error()
	now := time.Now()
	item.LastAttemptAt = now
	item.NextAttemptAt = now.Add(leadNotifyBackoff(attempt))
	item.Status = "retry"
	if attempt >= 6 {
		item.Status = "exhausted"
	}
	s.items[key] = item
	return item, nil
}

func (s *memoryLeadNotifyOutbox) RetryManually(_ context.Context, key string, _ int) (leadNotifyOutboxItem, error) { //nolint:unused // 仅供未接入生产的 CRM-16 重试原型使用。
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[key]
	if !ok {
		return item, errors.New("投递意图不存在")
	}
	item.Status = "pending"
	item.Attempts = 0
	item.NextAttemptAt = time.Time{}
	s.items[key] = item
	return item, nil
}

func (s *memoryLeadNotifyOutbox) MarkReceiptUnknown(_ context.Context, key string) (leadNotifyOutboxItem, error) { //nolint:unused // 仅供未接入生产的 CRM-16 回执原型使用。
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[key]
	if !ok {
		return item, errors.New("投递意图不存在")
	}
	item.Status = "receipt_unknown"
	item.ReceiptLost = true
	s.items[key] = item
	return item, nil
}

func leadNotifyBackoff(attempt int) time.Duration { //nolint:unused // 仅供未接入生产的 CRM-16 重试原型使用。
	if attempt <= 0 {
		attempt = 1
	}
	if attempt > 6 {
		attempt = 6
	}
	return time.Minute << uint(attempt-1)
}

var (
	leadNotifyPhoneRe  = regexp.MustCompile(`1[3-9]\d{9}`)
	leadNotifyEmailRe  = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	leadNotifyWechatRe = regexp.MustCompile(`(wxid_[A-Za-z0-9_\-]+|微信号[:：]\s*[A-Za-z0-9_\-]{5,})`) //nolint:unused // CRM-17 脱敏原型尚未接入生产发送。
)

// maskLeadNotifySummary 外部渠道专用最小摘要：先脱敏再拼需登录再鉴权的站内详情引用。
func maskLeadNotifySummary(raw, channel string) (string, string) { //nolint:unused // 保留 CRM-17 隐私判据，尚未接入生产发送。
	policy := make([]string, 0, 3)
	masked := leadNotifyPhoneRe.ReplaceAllString(raw, "1**********")
	if masked != raw {
		policy = append(policy, "phone")
	}
	next := leadNotifyEmailRe.ReplaceAllString(masked, "***@***")
	if next != masked {
		policy = append(policy, "email")
		masked = next
	}
	next = leadNotifyWechatRe.ReplaceAllString(masked, "[chat_id_redacted]")
	if next != masked {
		policy = append(policy, "chat_id")
		masked = next
	}
	masked = strings.TrimSpace(masked) + "｜详情请登录系统查看 /lead-mining（需再鉴权）"
	if len(policy) == 0 {
		policy = append(policy, "no_pii_match")
	}
	if channel != "" {
		policy = append(policy, "channel="+strings.ToLower(channel))
	}
	return masked, strings.Join(policy, ",")
}

// leadNotifyRecipient 外部收件人授权输入（不接触真实渠道）。
type leadNotifyRecipient struct { //nolint:unused // 保留 CRM-17 收件人授权原型，尚未接入生产发送。
	TenantID   uint
	UserID     uint
	Channel    string
	Authorized bool
}

// checkLeadNotifyRecipient 发送前复核：租户一致、收件人有效且有当前可见客户授权。
func checkLeadNotifyRecipient(tenantID uint, target leadNotifyRecipient) (bool, string) { //nolint:unused // 保留 CRM-17 授权判据，尚未接入生产发送。
	if target.TenantID == 0 || target.TenantID != tenantID {
		return false, "cross_tenant_target"
	}
	if target.UserID == 0 {
		return false, "invalid_owner"
	}
	if !target.Authorized {
		return false, "recipient_authorization_revoked"
	}
	return true, ""
}
