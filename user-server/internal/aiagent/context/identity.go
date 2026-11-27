package context

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var (
	ErrInvalidScope         = errors.New("invalid conversation scope")
	ErrMissingStablePeer    = errors.New("missing stable peer id")
	ErrMissingGroupID       = errors.New("missing group id")
	ErrSessionScopeConflict = errors.New("llm session scope conflict")
)

type ScopeIdentity struct {
	ScopeHash         string
	LLMConversationID string
	FallbackSessionID string
	BusLLMSessionID   string
	ConversationID    string
	GroupID           string
	IsGroup           bool
}

type SessionBinding struct {
	ScopeHash       string
	BusLLMSessionID string
}

func DeriveScopeIdentity(scope ConversationScope) (ScopeIdentity, error) {
	if scope.TenantID == 0 || strings.TrimSpace(scope.Platform) == "" || scope.ChannelAccountID == 0 {
		return ScopeIdentity{}, ErrInvalidScope
	}

	identity := ScopeIdentity{
		ConversationID: strings.TrimSpace(scope.ConversationID),
		GroupID:        strings.TrimSpace(scope.GroupID),
		IsGroup:        scope.IsGroup,
	}
	if scope.IsGroup {
		if identity.GroupID == "" {
			return ScopeIdentity{}, ErrInvalidScope
		}
	} else if identity.ConversationID == "" {
		identity.ConversationID = strings.TrimSpace(scope.StablePeerID)
		if identity.ConversationID == "" {
			return ScopeIdentity{}, ErrMissingStablePeer
		}
	}

	root := identity.ConversationID
	if identity.IsGroup {
		root = identity.GroupID
	}
	identity.ScopeHash = scopeHash(scope, root)
	identity.LLMConversationID = "llmconv_" + identity.ScopeHash
	identity.FallbackSessionID = "llmsess_" + identity.ScopeHash
	identity.BusLLMSessionID = strings.TrimSpace(scope.BusLLMSessionID)
	if identity.BusLLMSessionID == "" {
		identity.BusLLMSessionID = identity.FallbackSessionID
	}
	return identity, nil
}

func ValidateSessionBinding(binding SessionBinding, scope ConversationScope, busLLMSessionID string) error {
	if strings.TrimSpace(binding.ScopeHash) == "" || strings.TrimSpace(binding.BusLLMSessionID) == "" ||
		strings.TrimSpace(busLLMSessionID) == "" {
		return ErrSessionScopeConflict
	}
	if busLLMSessionID != binding.BusLLMSessionID {
		return ErrSessionScopeConflict
	}
	identity, err := DeriveScopeIdentity(scope)
	if err != nil {
		return err
	}
	if binding.ScopeHash != identity.ScopeHash {
		return ErrSessionScopeConflict
	}
	return nil
}

func scopeHash(scope ConversationScope, root string) string {
	kind := "dm"
	if scope.IsGroup {
		kind = "group"
	}
	parts := []string{
		strconv.FormatUint(uint64(scope.TenantID), 10),
		strings.ToLower(strings.TrimSpace(scope.Platform)),
		strconv.FormatUint(uint64(scope.ChannelAccountID), 10),
		kind,
		root,
	}
	return hashParts(parts...)
}

func hashParts(parts ...string) string {
	var builder strings.Builder
	for _, part := range parts {
		builder.WriteString(strconv.Itoa(len(part)))
		builder.WriteByte(':')
		builder.WriteString(part)
		builder.WriteByte('|')
	}
	sum := sha256.Sum256([]byte(builder.String()))
	return hex.EncodeToString(sum[:])
}

func (s ScopeIdentity) String() string {
	return fmt.Sprintf("group=%t conversation=%q scope=%s", s.IsGroup, s.ConversationID, s.ScopeHash)
}
