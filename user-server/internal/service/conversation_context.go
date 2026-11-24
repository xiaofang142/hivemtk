package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// 交叉引用命名空间（FR-020）：备注以带命名空间的 tag 形式落在共享上下文的 tags 列，
// 避免为「备注」新增列/迁移（conversation_contexts 已含 tags/summary/messages/member_roles）。
const (
	ContextNoteNamespaceAutoReply  = "auto_reply"
	ContextNoteNamespaceLeadMining = "lead_mining"
	contextNotePrefix              = "note:"
)

// ConversationContextService 两智能体共享的对话历史与用户数据读写口（FR-020/021，T050/T051/T052）。
//
// 边界（R8.3-C）：自动回复与线索发现只经本服务间接引用对方，禁止直调对方模块。
// 底层复用 Phase4 T033 的 model.ConversationContext 与 repository.ConversationContextRepository，
// 不新建同名类型、不改表语义。
type ConversationContextService struct {
	repo repository.ConversationContextRepository
}

func NewConversationContextService() *ConversationContextService {
	return &ConversationContextService{repo: repository.NewConversationContextRepository()}
}

func NewConversationContextServiceWithRepo(repo repository.ConversationContextRepository) *ConversationContextService {
	return &ConversationContextService{repo: repo}
}

var errConversationContextUnavailable = errors.New("共享对话上下文存储不可用")

func (s *ConversationContextService) get(ctx context.Context, accountID uint, convID string) (*model.ConversationContext, error) {
	if s == nil || s.repo == nil {
		return nil, errConversationContextUnavailable
	}
	convID = strings.TrimSpace(convID)
	if accountID == 0 || convID == "" {
		return nil, errors.New("渠道账号或会话ID为空")
	}
	return s.repo.Get(ctx, accountID, convID)
}

// AppendMessage 追加一条共享消息（两模块共同写入同一份历史，FR-020）。
func (s *ConversationContextService) AppendMessage(ctx context.Context, accountID uint, convID string, msg model.ConversationMessage) error {
	c, err := s.get(ctx, accountID, convID)
	if err != nil {
		return err
	}
	if msg.MessageID == "" {
		msg.MessageID = fmt.Sprintf("m_%d", len(c.Messages)+1)
	}
	c.Messages = append(c.Messages, msg)
	return s.repo.Upsert(ctx, c)
}

// Messages 读取共享消息历史（两模块共同读取，FR-020）。
func (s *ConversationContextService) Messages(ctx context.Context, accountID uint, convID string) (model.ConversationMessages, error) {
	c, err := s.get(ctx, accountID, convID)
	if err != nil {
		return nil, err
	}
	return c.Messages, nil
}

// SetMemberRole 记录群聊成员角色（T052 / FR-021，MemberRoles）。
func (s *ConversationContextService) SetMemberRole(ctx context.Context, accountID uint, convID, senderID, role string) error {
	if strings.TrimSpace(senderID) == "" {
		return nil
	}
	c, err := s.get(ctx, accountID, convID)
	if err != nil {
		return err
	}
	if c.MemberRoles == nil {
		c.MemberRoles = model.MemberRoles{}
	}
	c.MemberRoles[senderID] = role
	return s.repo.Upsert(ctx, c)
}

// AddTag 写入共享标签（两模块可交叉引用）。
func (s *ConversationContextService) AddTag(ctx context.Context, accountID uint, convID, tag string) error {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return nil
	}
	c, err := s.get(ctx, accountID, convID)
	if err != nil {
		return err
	}
	for _, exists := range c.Tags {
		if exists == tag {
			return nil
		}
	}
	c.Tags = append(c.Tags, tag)
	return s.repo.Upsert(ctx, c)
}

// SetSummary 写入共享摘要（两模块可交叉引用）。
func (s *ConversationContextService) SetSummary(ctx context.Context, accountID uint, convID, summary string) error {
	c, err := s.get(ctx, accountID, convID)
	if err != nil {
		return err
	}
	c.Summary = summary
	return s.repo.Upsert(ctx, c)
}

// AddNote 写入带命名空间的交叉引用备注（T051）。
// namespace 取 ContextNoteNamespaceAutoReply / ContextNoteNamespaceLeadMining。
func (s *ConversationContextService) AddNote(ctx context.Context, accountID uint, convID, namespace, note string) error {
	note = strings.TrimSpace(note)
	if note == "" {
		return nil
	}
	return s.AddTag(ctx, accountID, convID, contextNotePrefix+namespace+":"+note)
}

// CrossReferenceNotes 读取「其他模块」写入的备注（T051，双向可用）。
// 传入自己的 namespace，返回所有不属于该命名空间的 note 文本。
func (s *ConversationContextService) CrossReferenceNotes(ctx context.Context, accountID uint, convID, ownNamespace string) ([]string, error) {
	c, err := s.get(ctx, accountID, convID)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(c.Tags))
	ownKey := contextNotePrefix + ownNamespace + ":"
	for _, tag := range c.Tags {
		if !strings.HasPrefix(tag, contextNotePrefix) {
			continue
		}
		if strings.HasPrefix(tag, ownKey) {
			continue // 跳过自己写的，只读对方的
		}
		out = append(out, strings.TrimPrefix(tag, contextNotePrefix))
	}
	return out, nil
}

// ConversationContextSnapshot 共享上下文快照（管理端交叉引用展示，T054）。
type ConversationContextSnapshot struct {
	ChannelAccountID uint                       `json:"channel_account_id"`
	ConversationID   string                     `json:"conversation_id"`
	Messages         model.ConversationMessages `json:"messages"`
	Tags             []string                   `json:"tags"`
	Summary          string                     `json:"summary"`
	MemberRoles      map[string]string          `json:"member_roles"`
	AutoReplyNotes   []string                   `json:"auto_reply_notes"`
	LeadMiningNotes  []string                   `json:"lead_mining_notes"`
}

// Snapshot 读取共享上下文全量视图 + 双向交叉引用备注（T054 前端展示数据源）。
func (s *ConversationContextService) Snapshot(ctx context.Context, accountID uint, convID string) (*ConversationContextSnapshot, error) {
	c, err := s.get(ctx, accountID, convID)
	if err != nil {
		return nil, err
	}
	out := &ConversationContextSnapshot{
		ChannelAccountID: accountID,
		ConversationID:   convID,
		Messages:         c.Messages,
		Tags:             []string(c.Tags),
		Summary:          c.Summary,
		MemberRoles:      map[string]string(c.MemberRoles),
	}
	for _, tag := range c.Tags {
		if strings.HasPrefix(tag, contextNotePrefix) {
			note := strings.TrimPrefix(tag, contextNotePrefix)
			switch {
			case strings.HasPrefix(note, ContextNoteNamespaceAutoReply+":"):
				out.AutoReplyNotes = append(out.AutoReplyNotes, strings.TrimPrefix(note, ContextNoteNamespaceAutoReply+":"))
			case strings.HasPrefix(note, ContextNoteNamespaceLeadMining+":"):
				out.LeadMiningNotes = append(out.LeadMiningNotes, strings.TrimPrefix(note, ContextNoteNamespaceLeadMining+":"))
			}
		}
	}
	return out, nil
}

// AggregateGroupContext 群聊多成员上下文聚合（T052 / FR-021）。
// 以 MemberRoles 为权威给每条消息补 [角色:发送者] 前缀，并按角色归并统计；
// 角色未知时保留原始发送者，不猜测（fail-visible）。
func (s *ConversationContextService) AggregateGroupContext(ctx context.Context, accountID uint, convID string) ([]model.ConversationMessage, map[string]int, error) {
	c, err := s.get(ctx, accountID, convID)
	if err != nil {
		return nil, nil, err
	}
	byRole := make(map[string]int)
	out := make([]model.ConversationMessage, 0, len(c.Messages))
	for _, m := range c.Messages {
		role := "unknown"
		if r, ok := c.MemberRoles[m.SenderID]; ok && strings.TrimSpace(r) != "" {
			role = r
		}
		byRole[role]++
		enriched := m
		label := m.SenderID
		if label == "" {
			label = m.Role
		}
		enriched.Content = fmt.Sprintf("[%s:%s] %s", role, label, m.Content)
		out = append(out, enriched)
	}
	return out, byRole, nil
}
