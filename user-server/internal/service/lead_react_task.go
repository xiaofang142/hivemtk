package service

import (
	"context"
	"strconv"
	"strings"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
)

const (
	leadReactTaskMaxMessages        = 20
	leadReactTaskMaxMessageRunes    = 1000
	leadReactTaskMaxCurrentRunes    = 2000
	leadReactTaskMaxRuleRunes       = 2000
	leadReactTaskMaxKeywords        = 20
	leadReactTaskMaxHits            = 20
	leadReactTaskMaxEvidenceRunes   = 2000
	leadReactTaskMaxIdentifierRunes = 256
	leadReactTaskMaxAccountIDs      = 20
)

// LeadReactTask 是单次 ReAct 取证的不可变可信视图。租户不进入该结构，始终只从
// context.Context 解析；ACL 只来自冻结的 leadHistoryScope，模型参数不能改写范围。
type LeadReactTask struct {
	Platform           string
	CanonicalAccountID uint
	ChannelID          uint
	AccountIDs         []string
	SourceRef          string
	RawFallback        bool
	ScopeValid         bool
	ConversationID     string
	GroupID            string
	SenderID           string
	IsGroup            bool
	CurrentMessage     string
	Messages           []LeadReactTaskMessage
	RuleDescription    string
	Keywords           []string
	SemanticKeywords   []string
	Hits               []LeadReactTaskHit
}

type LeadReactTaskMessage struct {
	MessageID string
	Role      string
	SenderID  string
	Content   string
}

type LeadReactTaskHit struct {
	Type               string
	Complete           bool
	Degraded           bool
	Reason             string
	EvidenceMessageIDs []string
}

type leadReactTaskContextKey struct{}

func withLeadReactTask(ctx context.Context, task LeadReactTask) context.Context {
	return context.WithValue(ctx, leadReactTaskContextKey{}, task)
}

func leadReactTaskFromContext(ctx context.Context) (LeadReactTask, bool) {
	task, ok := ctx.Value(leadReactTaskContextKey{}).(LeadReactTask)
	return task, ok
}

// buildLeadReactTask 只从 frozen leadHistoryScope 取 ACL；hub/jctx 仅提供当前消息、
// 历史、规则与检测证据。scope 缺失、租户不符、平台/账号/群类型不匹配时 ScopeValid=false。
func buildLeadReactTask(ctx context.Context, hub *model.MessageHub, jctx *LeadJudgeContext, scope *leadHistoryScope) LeadReactTask {
	task := LeadReactTask{}
	if jctx != nil {
		task.RuleDescription = boundedLeadReactRuleDescription(jctx.Rule)
		task.Keywords = boundedLeadReactStrings(jctxRuleKeywords(jctx.Rule), leadReactTaskMaxKeywords)
		task.SemanticKeywords = boundedLeadReactStrings(jctxRuleSemanticKeywords(jctx.Rule), leadReactTaskMaxKeywords)
		if len(jctx.Hits) > 0 {
			task.Hits = make([]LeadReactTaskHit, 0, min(len(jctx.Hits), leadReactTaskMaxHits))
			for _, hit := range jctx.Hits[:min(len(jctx.Hits), leadReactTaskMaxHits)] {
				task.Hits = append(task.Hits, LeadReactTaskHit{
					Type:               hit.Type,
					Complete:           hit.Complete,
					Degraded:           hit.Degraded,
					Reason:             truncateLeadReactRunes(hit.Reason, 300),
					EvidenceMessageIDs: boundedLeadReactStrings(hit.EvidenceMessageIDs, leadReactTaskMaxKeywords),
				})
			}
		}
	}

	if hub != nil {
		task.ConversationID = strings.TrimSpace(hub.ConversationID)
		task.GroupID = strings.TrimSpace(hub.GroupID)
		task.SenderID = strings.TrimSpace(hub.SenderID)
		task.IsGroup = hub.IsGroup
		task.CurrentMessage = truncateLeadReactRunes(hub.Content, leadReactTaskMaxCurrentRunes)
	}

	if jctx != nil && len(jctx.Messages) > 0 {
		start := max(0, len(jctx.Messages)-leadReactTaskMaxMessages)
		task.Messages = make([]LeadReactTaskMessage, 0, len(jctx.Messages)-start)
		for _, message := range jctx.Messages[start:] {
			task.Messages = append(task.Messages, LeadReactTaskMessage{
				MessageID: message.MessageID,
				Role:      message.Role,
				SenderID:  message.SenderID,
				Content:   truncateLeadReactRunes(message.Content, leadReactTaskMaxMessageRunes),
			})
		}
	}
	if task.CurrentMessage == "" && len(task.Messages) > 0 {
		last := task.Messages[len(task.Messages)-1]
		task.CurrentMessage = last.Content
		task.SenderID = firstNonEmptyLeadReact(task.SenderID, last.SenderID)
	}

	tid, ok := tenant.FromContext(ctx)
	if !ok || tid == 0 || tenant.IsSystemScope(ctx) || hub == nil || scope == nil {
		return task
	}
	if scope.tenantID != tid || (hub.TenantID != 0 && hub.TenantID != tid) {
		return task
	}
	platform := strings.TrimSpace(scope.Platform)
	sourceRef := strings.TrimSpace(scope.sourceRef)
	if platform == "" || sourceRef == "" || !strings.EqualFold(strings.TrimSpace(hub.Platform), platform) || strings.TrimSpace(hub.AccountID) != sourceRef {
		return task
	}
	if scope.IsGroup != hub.IsGroup {
		return task
	}
	if jctx != nil {
		if jctx.Platform != "" && !strings.EqualFold(strings.TrimSpace(jctx.Platform), platform) {
			return task
		}
		if jctx.AccountID != "" && strings.TrimSpace(jctx.AccountID) != sourceRef {
			return task
		}
		if jctx.IsGroup != hub.IsGroup {
			return task
		}
	}

	aliases := uniqueNonEmptyLeadReact(scope.AccountIDs)
	if len(aliases) == 0 {
		return task
	}
	if scope.accountID > 0 {
		if scope.channelID == 0 || (jctx != nil && jctx.ChannelAccountID != scope.accountID) {
			return task
		}
		canonical := strconv.FormatUint(uint64(scope.accountID), 10)
		aliases = append([]string{canonical}, aliases...)
		task.CanonicalAccountID = scope.accountID
		task.ChannelID = scope.channelID
	} else {
		if scope.channelID != 0 || len(aliases) != 1 || aliases[0] != sourceRef {
			return task
		}
		task.RawFallback = true
	}
	task.Platform = platform
	task.AccountIDs = uniqueNonEmptyLeadReact(aliases)
	task.SourceRef = sourceRef
	task.ScopeValid = true
	return task
}

func validLeadReactTaskContext(ctx context.Context, task LeadReactTask) bool {
	tid, ok := tenant.FromContext(ctx)
	if !ok || tid == 0 || tenant.IsSystemScope(ctx) || !task.ScopeValid || task.Platform == "" || task.SourceRef == "" || len(task.AccountIDs) == 0 {
		return false
	}
	if len(task.AccountIDs) > leadReactTaskMaxAccountIDs ||
		!boundedLeadReactIdentifier(task.Platform) ||
		!boundedLeadReactIdentifier(task.SourceRef) ||
		!boundedLeadReactIdentifier(task.ConversationID) ||
		!boundedLeadReactIdentifier(task.GroupID) ||
		!boundedLeadReactIdentifier(task.SenderID) {
		return false
	}
	for _, accountID := range task.AccountIDs {
		if strings.TrimSpace(accountID) == "" || !boundedLeadReactIdentifier(accountID) {
			return false
		}
	}
	if task.RawFallback {
		return task.CanonicalAccountID == 0 && task.ChannelID == 0 && len(task.AccountIDs) == 1 && task.AccountIDs[0] == task.SourceRef
	}
	canonical := strconv.FormatUint(uint64(task.CanonicalAccountID), 10)
	return task.CanonicalAccountID > 0 && task.ChannelID > 0 && containsLeadReactString(task.AccountIDs, canonical)
}

func boundedLeadReactIdentifier(value string) bool {
	return len([]rune(value)) <= leadReactTaskMaxIdentifierRunes
}

func boundedLeadReactRuleDescription(rule *model.LeadMiningRule) string {
	if rule == nil {
		return ""
	}
	return truncateLeadReactRunes(rule.SystemDescription, leadReactTaskMaxRuleRunes)
}

func jctxRuleKeywords(rule *model.LeadMiningRule) []string {
	if rule == nil {
		return nil
	}
	return rule.Keywords
}

func jctxRuleSemanticKeywords(rule *model.LeadMiningRule) []string {
	if rule == nil {
		return nil
	}
	return rule.SemanticKeywords
}

func boundedLeadReactStrings[T ~string](values []T, limit int) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, min(len(values), limit))
	for _, value := range values[:min(len(values), limit)] {
		text := strings.TrimSpace(string(value))
		if text != "" {
			out = append(out, truncateLeadReactRunes(text, 200))
		}
	}
	return out
}

func uniqueNonEmptyLeadReact(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func containsLeadReactString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func truncateLeadReactRunes(value string, limit int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if limit <= 0 || len(runes) <= limit {
		return value
	}
	return string(runes[:limit-1]) + "…"
}

func firstNonEmptyLeadReact(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
