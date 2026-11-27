package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	agentcontext "hivemtk-user/internal/aiagent/context"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/repository"
)

var (
	ErrUnifiedMessageScope              = errors.New("统一消息读取缺少明确租户、平台、账号或会话范围")
	ErrUnifiedMessageAmbiguousDuplicate = errors.New("统一消息存在冲突的 correlation 重复")
	ErrUnifiedMessageUnavailable        = errors.New("统一消息读取仓储未装配")
)

// UnifiedMessage 保留渠道事实源和会话补充来源的稳定身份、时间与可见性证据。
type UnifiedMessage struct {
	agentcontext.ContextMessage
	TenantID          uint
	Platform          string
	AccountID         string
	ConversationID    string
	IsGroup           bool
	GroupID           string
	SourceMessageID   string
	PlatformMessageID string
	Status            string
	DeliveredAt       *time.Time
}

// UnifiedMessageQuery 是 UnifiedMessageReader 的 fail-closed 查询边界。
type UnifiedMessageQuery struct {
	TenantID         uint
	Platform         string
	AccountIDs       []string
	IsGroup          bool
	ConversationID   string
	GroupID          string
	StablePeerID     string
	SessionID        string
	SessionAccountID string
	Limit            int
}

type unifiedMessageHubRepository interface {
	ListRecentByConversation(context.Context, repository.MessageHubHistoryScope, string, int) ([]model.MessageHub, error)
	ListRecentByGroup(context.Context, repository.MessageHubHistoryScope, string, int) ([]model.MessageHub, error)
	ListRecentBySender(context.Context, repository.MessageHubHistoryScope, string, int) ([]model.MessageHub, error)
}

type unifiedSessionMessageRepository interface {
	ListRecentDescBySessionID(context.Context, string, int) ([]model.SessionMessage, error)
}

// UnifiedMessageReader 以 MessageHub 为权威来源，SessionMessage 只补未落 hub 的出站。
type UnifiedMessageReader struct {
	hubRepo     unifiedMessageHubRepository
	sessionRepo unifiedSessionMessageRepository
}

func NewUnifiedMessageReader(
	hubRepo unifiedMessageHubRepository,
	sessionRepo unifiedSessionMessageRepository,
) *UnifiedMessageReader {
	return &UnifiedMessageReader{hubRepo: hubRepo, sessionRepo: sessionRepo}
}

func (r *UnifiedMessageReader) List(ctx context.Context, query UnifiedMessageQuery) ([]UnifiedMessage, error) {
	if query.Limit == 0 {
		query.Limit = 50
	}
	if err := validateUnifiedMessageQuery(ctx, query); err != nil {
		return nil, err
	}
	if r == nil || r.hubRepo == nil || r.sessionRepo == nil {
		return nil, ErrUnifiedMessageUnavailable
	}

	scope := repository.MessageHubHistoryScope{
		Platform:   strings.TrimSpace(query.Platform),
		AccountIDs: normalizedAccountIDs(query.AccountIDs),
		IsGroup:    query.IsGroup,
	}
	var (
		hubs []model.MessageHub
		err  error
	)
	switch {
	case query.IsGroup:
		hubs, err = r.hubRepo.ListRecentByGroup(ctx, scope, strings.TrimSpace(query.GroupID), query.Limit)
	case strings.TrimSpace(query.ConversationID) != "":
		hubs, err = r.hubRepo.ListRecentByConversation(ctx, scope, strings.TrimSpace(query.ConversationID), query.Limit)
	default:
		hubs, err = r.hubRepo.ListRecentBySender(ctx, scope, strings.TrimSpace(query.StablePeerID), query.Limit)
	}
	if err != nil {
		return nil, fmt.Errorf("读取 MessageHub 历史: %w", err)
	}

	messages := make([]UnifiedMessage, 0, len(hubs))
	for _, hub := range hubs {
		if !hubMatchesUnifiedQuery(hub, query) {
			continue
		}
		messages = append(messages, unifiedMessageFromHub(hub))
	}

	if strings.TrimSpace(query.SessionID) != "" {
		rows, readErr := r.sessionRepo.ListRecentDescBySessionID(ctx, strings.TrimSpace(query.SessionID), query.Limit)
		if readErr != nil {
			return nil, fmt.Errorf("读取 SessionMessage 历史: %w", readErr)
		}
		for _, row := range rows {
			if row.TenantID != query.TenantID || row.SessionID != strings.TrimSpace(query.SessionID) {
				continue
			}
			messages = append(messages, unifiedMessageFromSession(
				row,
				scope.Platform,
				strings.TrimSpace(query.SessionAccountID),
				strings.TrimSpace(query.ConversationID),
				query.IsGroup,
				strings.TrimSpace(query.GroupID),
			))
		}
	}

	merged, err := mergeUnifiedMessages(messages)
	if err != nil {
		return nil, err
	}
	if len(merged) > query.Limit {
		merged = merged[len(merged)-query.Limit:]
	}
	return merged, nil
}

func validateUnifiedMessageQuery(ctx context.Context, query UnifiedMessageQuery) error {
	tid, ok := tenant.FromContext(ctx)
	if !ok || tid == 0 || tenant.IsSystemScope(ctx) || query.TenantID != tid {
		return ErrUnifiedMessageScope
	}
	if strings.TrimSpace(query.Platform) == "" || len(normalizedAccountIDs(query.AccountIDs)) == 0 {
		return ErrUnifiedMessageScope
	}
	if query.IsGroup {
		if strings.TrimSpace(query.GroupID) == "" {
			return ErrUnifiedMessageScope
		}
	} else if strings.TrimSpace(query.ConversationID) == "" && strings.TrimSpace(query.StablePeerID) == "" {
		return ErrUnifiedMessageScope
	}
	if strings.TrimSpace(query.SessionID) != "" && strings.TrimSpace(query.SessionAccountID) == "" {
		return ErrUnifiedMessageScope
	}
	if query.Limit < 1 || query.Limit > 100 {
		return fmt.Errorf("%w: limit 必须在 1..100", ErrUnifiedMessageScope)
	}
	return nil
}

func normalizedAccountIDs(accountIDs []string) []string {
	result := []string{}
	seen := map[string]struct{}{}
	for _, accountID := range accountIDs {
		accountID = strings.TrimSpace(accountID)
		if accountID == "" {
			continue
		}
		if _, ok := seen[accountID]; ok {
			continue
		}
		seen[accountID] = struct{}{}
		result = append(result, accountID)
	}
	return result
}

func hubMatchesUnifiedQuery(hub model.MessageHub, query UnifiedMessageQuery) bool {
	if hub.TenantID != query.TenantID ||
		hub.Platform != strings.TrimSpace(query.Platform) ||
		hub.IsGroup != query.IsGroup ||
		!accountIDInScope(hub.AccountID, normalizedAccountIDs(query.AccountIDs)) {
		return false
	}
	if query.IsGroup {
		return hub.GroupID == strings.TrimSpace(query.GroupID)
	}
	if conversationID := strings.TrimSpace(query.ConversationID); conversationID != "" {
		return hub.ConversationID == conversationID
	}
	peerID := strings.TrimSpace(query.StablePeerID)
	return peerID != "" && hub.ConversationID == "" && (hub.SenderID == peerID || hub.ReceiverID == peerID)
}

func accountIDInScope(accountID string, accountIDs []string) bool {
	for _, candidate := range accountIDs {
		if accountID == candidate {
			return true
		}
	}
	return false
}

func unifiedMessageFromHub(hub model.MessageHub) UnifiedMessage {
	messageID := "hub:" + strconv.FormatUint(uint64(hub.ID), 10)
	metadata := map[string]string{
		"msg_id":      hub.MsgID,
		"platform_id": hub.Platform,
		"account_id":  hub.AccountID,
		"msg_type":    hub.MsgType,
		"status":      hub.Status,
	}
	copyHubStringExtra(hub.Extra, metadata, "channel_msg_id")
	copyHubStringExtra(hub.Extra, metadata, "trace_id")
	copyHubStringExtra(hub.Extra, metadata, "media_url")

	return UnifiedMessage{
		ContextMessage: agentcontext.ContextMessage{
			MessageID:      messageID,
			Origin:         agentcontext.OriginMessageHub,
			CorrelationID:  hubCorrelationID(hub.Extra),
			HistorySource:  agentcontext.HistorySourceMessageHub,
			SourceMetadata: metadata,
			Role:           hubRole(hub.Direction),
			Direction:      hub.Direction,
			SenderID:       hub.SenderID,
			SenderName:     hub.SenderName,
			MemberRole:     hubStringExtra(hub.Extra, "member_role"),
			Content:        hub.Content,
			SentAt:         hub.SentAt,
			ReplyTo:        hubStringExtra(hub.Extra, "reply_to"),
			Mentions:       hubStringSliceExtra(hub.Extra, "mentions"),
			Visibility:     unifiedVisibility(hub.Visibility),
			Audience:       append([]string{}, hub.AudienceJSON...),
		},
		TenantID:          hub.TenantID,
		Platform:          hub.Platform,
		AccountID:         hub.AccountID,
		ConversationID:    hub.ConversationID,
		IsGroup:           hub.IsGroup,
		GroupID:           hub.GroupID,
		SourceMessageID:   hub.MsgID,
		PlatformMessageID: hub.PlatformMsgID,
		Status:            hub.Status,
	}
}

func unifiedMessageFromSession(
	row model.SessionMessage,
	platform string,
	accountID string,
	conversationID string,
	isGroup bool,
	groupID string,
) UnifiedMessage {
	direction := sessionMessageDirection(row.SenderType)
	return UnifiedMessage{
		ContextMessage: agentcontext.ContextMessage{
			MessageID:      "session:" + strconv.FormatUint(uint64(row.ID), 10),
			Origin:         agentcontext.OriginSessionMessage,
			CorrelationID:  strings.TrimSpace(row.CorrelationID),
			HistorySource:  agentcontext.HistorySourceSessionMessage,
			SourceMetadata: map[string]string{"session_id": row.SessionID, "sender_type": row.SenderType},
			Role:           sessionMessageRole(row.SenderType),
			Direction:      direction,
			SenderID:       row.SenderID,
			SenderName:     row.SenderName,
			Content:        row.Content,
			SentAt:         row.CreatedAt,
			Visibility:     agentcontext.VisibilityUnknown,
			Audience:       []string{},
		},
		TenantID:        row.TenantID,
		Platform:        platform,
		AccountID:       accountID,
		ConversationID:  conversationID,
		IsGroup:         isGroup,
		GroupID:         groupID,
		SourceMessageID: strconv.FormatUint(uint64(row.ID), 10),
		DeliveredAt:     row.DeliveredAt,
	}
}

func hubCorrelationID(extra model.JSONMap) string {
	for _, key := range []string{"correlation_id", "llm_correlation_id"} {
		if value := hubStringExtra(extra, key); value != "" {
			return value
		}
	}
	return ""
}

func hubStringExtra(extra model.JSONMap, key string) string {
	if extra == nil {
		return ""
	}
	value, _ := extra[key].(string)
	return strings.TrimSpace(value)
}

func copyHubStringExtra(extra model.JSONMap, target map[string]string, key string) {
	if value := hubStringExtra(extra, key); value != "" {
		target[key] = value
	}
}

func hubStringSliceExtra(extra model.JSONMap, key string) []string {
	result := []string{}
	if extra == nil {
		return result
	}
	switch values := extra[key].(type) {
	case []string:
		for _, value := range values {
			if value = strings.TrimSpace(value); value != "" {
				result = append(result, value)
			}
		}
	case []any:
		for _, value := range values {
			if text, ok := value.(string); ok {
				if text = strings.TrimSpace(text); text != "" {
					result = append(result, text)
				}
			}
		}
	}
	return result
}

func unifiedVisibility(visibility string) agentcontext.Visibility {
	switch agentcontext.Visibility(visibility) {
	case agentcontext.VisibilityPublic, agentcontext.VisibilityMember, agentcontext.VisibilityPrivate:
		return agentcontext.Visibility(visibility)
	default:
		return agentcontext.VisibilityUnknown
	}
}

func hubRole(direction string) string {
	if direction == "outbound" {
		return "assistant"
	}
	return "user"
}

func sessionMessageDirection(senderType string) string {
	switch strings.ToLower(strings.TrimSpace(senderType)) {
	case "customer", "visitor", "user":
		return "inbound"
	case "system":
		return "system"
	default:
		return "outbound"
	}
}

func sessionMessageRole(senderType string) string {
	switch strings.ToLower(strings.TrimSpace(senderType)) {
	case "customer", "visitor", "user":
		return "user"
	case "system":
		return "system"
	default:
		return "assistant"
	}
}

func mergeUnifiedMessages(messages []UnifiedMessage) ([]UnifiedMessage, error) {
	ordered := append([]UnifiedMessage{}, messages...)
	sortUnifiedMessages(ordered)
	result := make([]UnifiedMessage, 0, len(ordered))
	byCorrelation := map[string]int{}

	for _, message := range ordered {
		correlationID := strings.TrimSpace(message.CorrelationID)
		if correlationID == "" {
			result = append(result, message)
			continue
		}
		index, exists := byCorrelation[correlationID]
		if !exists {
			byCorrelation[correlationID] = len(result)
			result = append(result, message)
			continue
		}
		existing := result[index]
		if !compatibleUnifiedDuplicate(existing, message) {
			return nil, fmt.Errorf(
				"%w: correlation_id=%q message_ids=%q,%q",
				ErrUnifiedMessageAmbiguousDuplicate,
				correlationID,
				existing.MessageID,
				message.MessageID,
			)
		}
		if unifiedMessagePriority(message) > unifiedMessagePriority(existing) {
			result[index] = message
		}
	}
	return result, nil
}

func compatibleUnifiedDuplicate(left UnifiedMessage, right UnifiedMessage) bool {
	if left.MessageID == right.MessageID {
		return left.Content == right.Content &&
			left.Direction == right.Direction &&
			left.SenderID == right.SenderID &&
			left.SentAt.Equal(right.SentAt)
	}
	if left.Content != right.Content || left.Direction != right.Direction {
		return false
	}
	if left.TenantID != 0 && right.TenantID != 0 && left.TenantID != right.TenantID {
		return false
	}
	if left.Platform != "" && right.Platform != "" && left.Platform != right.Platform {
		return false
	}
	if left.AccountID != "" && right.AccountID != "" && left.AccountID != right.AccountID {
		return false
	}
	if left.IsGroup != right.IsGroup {
		return false
	}
	if left.IsGroup {
		return left.GroupID == right.GroupID
	}
	return left.ConversationID == right.ConversationID
}

func unifiedMessagePriority(message UnifiedMessage) int {
	if message.Origin == agentcontext.OriginMessageHub {
		return 2
	}
	return 1
}

func sortUnifiedMessages(messages []UnifiedMessage) {
	sort.SliceStable(messages, func(i, j int) bool {
		if !messages[i].SentAt.Equal(messages[j].SentAt) {
			return messages[i].SentAt.Before(messages[j].SentAt)
		}
		return messages[i].MessageID < messages[j].MessageID
	})
}
