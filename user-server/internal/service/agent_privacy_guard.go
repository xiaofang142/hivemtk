package service

import (
	"regexp"
	"sort"
	"strings"
)

const (
	PrivacyReasonPhone       = "member_phone"
	PrivacyReasonEmail       = "member_email"
	PrivacyReasonOrderID     = "order_reference"
	PrivacyReasonPrivateFact = "private_member_fact"
)

var (
	phonePattern = regexp.MustCompile(`1[3-9][0-9]{9}`)
	emailPattern = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	orderPattern = regexp.MustCompile(`ORD-[0-9]{4,}-[0-9]{2,}|订单号[:：]?[0-9]{6,}`)
)

// GroupOutputPolicy 保存经授权可在群内出现的公开事实；任何未授权的成员私有事实都不得出现。
type GroupOutputPolicy struct {
	AllowedFacts    []string
	PrivateFacts    []string
	AllowedOrderIDs []string
}

type PrivacyDecision struct {
	Allowed bool     `json:"allowed"`
	Reasons []string `json:"reasons,omitempty"`
}

type GroupOutputPrivacyGuard struct{}

func NewGroupOutputPrivacyGuard() *GroupOutputPrivacyGuard {
	return &GroupOutputPrivacyGuard{}
}

// Check 在群回复真正发出前做确定性检查；只返回机器可读原因，绝不回显命中内容。
func (g *GroupOutputPrivacyGuard) Check(text string, policy GroupOutputPolicy) PrivacyDecision {
	reasons := make([]string, 0, 4)
	sanitized := text
	for _, id := range policy.AllowedOrderIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			sanitized = strings.ReplaceAll(sanitized, id, " ")
		}
	}
	if phonePattern.MatchString(sanitized) {
		reasons = append(reasons, PrivacyReasonPhone)
	}
	if emailPattern.MatchString(sanitized) {
		reasons = append(reasons, PrivacyReasonEmail)
	}
	if orderPattern.MatchString(sanitized) {
		reasons = append(reasons, PrivacyReasonOrderID)
	}
	for _, fact := range policy.PrivateFacts {
		fact = strings.TrimSpace(fact)
		if fact != "" && strings.Contains(text, fact) {
			reasons = append(reasons, PrivacyReasonPrivateFact)
			break
		}
	}
	sort.Strings(reasons)
	unique := reasons[:0]
	for _, reason := range reasons {
		if len(unique) == 0 || unique[len(unique)-1] != reason {
			unique = append(unique, reason)
		}
	}
	return PrivacyDecision{Allowed: len(unique) == 0, Reasons: unique}
}
