package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// CH-09 统一入站编排：从旁挂日志接入实际归档与业务触发。
//
// 刻意不修改 inbound_message.go（既有 InboundMessageService.Deliver 只归一化）。
// 本编排复用既有 MessageHub/Inbox 与双 Agent 调度，所有协议只做归一/确认；
// 由本服务统一去重、持久化、上下文更新与业务触发，防新旧链双写双触发。
var (
	// ErrInboundDuplicate 重放：同一 (tenant,event_id) 已归档，显式拒绝而不是静默成功。
	ErrInboundDuplicate = errors.New("入站事件重放，已归档")
	// ErrInboundOutOfOrder 乱序：早于最后归档事件的时间戳，显式标记可查。
	ErrInboundOutOfOrder = errors.New("入站事件乱序")
	// ErrInboundPersistFailed 归档持久化失败：不能只记日志后宣称业务成功。
	ErrInboundPersistFailed = errors.New("入站归档持久化失败")
)

// InboundOutcome 显式可查的投递结论（拒绝/失败不伪装成成功）。
type InboundOutcome struct {
	EventID      string `json:"event_id"`
	Status       string `json:"status"` // archived | duplicate | out_of_order | rejected | persist_failed
	Reason       string `json:"reason,omitempty"`
	ArchivedAt   string `json:"archived_at,omitempty"`
	Automation   string `json:"automation"` // triggered | skipped_unknown | skipped_gate | not_started
	IdentityRole string `json:"identity_role,omitempty"`
}

// InboundArchiveRepository 入站归档端口（复用既有 MessageHub 仓储，不新建收件箱模型）。
type InboundArchiveRepository interface {
	Available() bool
	// Exists 查重：(tenantID,eventID) 已存在返回 true。
	Exists(ctx context.Context, tenantID uint, eventID string) (bool, error)
	// LastTimestamp 取该会话最后归档时间戳（乱序检测）。
	LastTimestamp(ctx context.Context, tenantID uint, conversationID string) (int64, error)
	// Archive 原子归档一条消息；重复键返回 ErrInboundDuplicate。
	Archive(ctx context.Context, msg *model.MessageHub) error
}

// InboundBusinessTrigger 双 Agent + 上下文的业务触发端口（既有编排适配）。
type InboundBusinessTrigger interface {
	// Trigger 归档成功后调用；unknown 身份由上层 gate 拒绝，返回 skipped 原因。
	Trigger(ctx context.Context, msg *model.MessageHub, identity *ChannelIdentity) (string, error)
}

// ChannelInboundOrchestrator 统一入站编排。
type ChannelInboundOrchestrator struct {
	archive  InboundArchiveRepository
	resolver *ChannelIdentityResolver
	trigger  InboundBusinessTrigger
}

// NewChannelInboundOrchestrator 创建编排器。
func NewChannelInboundOrchestrator(archive InboundArchiveRepository, resolver *ChannelIdentityResolver, trigger InboundBusinessTrigger) *ChannelInboundOrchestrator {
	return &ChannelInboundOrchestrator{archive: archive, resolver: resolver, trigger: trigger}
}

// DeliverAndArchive 归一化 → 身份识别 → 去重/乱序 → 持久化 → 业务触发，全程返回显式结论。
// 解析失败、持久化失败返回 error；重放/乱序返回明确 Outcome（不是 nil 错误+假成功）。
func (o *ChannelInboundOrchestrator) DeliverAndArchive(ctx context.Context, msg *model.InboundMessage) (*InboundOutcome, error) {
	if o == nil || o.archive == nil || !o.archive.Available() {
		return nil, repository.ErrChannelStoreUnavailable
	}
	if msg == nil || msg.EventID == "" || msg.Source == "" || msg.TenantID == 0 {
		return nil, ErrInboundMessageInvalid
	}
	out := &InboundOutcome{EventID: msg.EventID, Status: "not_started", Automation: "not_started"}

	// 1) 重放：同 eventID 已归档，显式拒绝（防双写双触发）
	exists, err := o.archive.Exists(ctx, msg.TenantID, msg.EventID)
	if err != nil {
		out.Status, out.Reason = "persist_failed", "查重失败"
		return out, fmt.Errorf("%w: %v", ErrInboundPersistFailed, err)
	}
	if exists {
		out.Status, out.Reason = "duplicate", "event_id 已归档"
		return out, ErrInboundDuplicate
	}
	// 2) 乱序：早于会话最后归档时间，显式可查（不静默丢）
	if msg.ConversationID != "" {
		last, err := o.archive.LastTimestamp(ctx, msg.TenantID, msg.ConversationID)
		if err != nil {
			return out, fmt.Errorf("%w: %v", ErrInboundPersistFailed, err)
		}
		if last > 0 && msg.Timestamp > 0 && msg.Timestamp < last {
			out.Status, out.Reason = "out_of_order", fmt.Sprintf("ts=%d < last=%d", msg.Timestamp, last)
			return out, ErrInboundOutOfOrder
		}
	}
	// 3) 身份：解析失败显式拒绝，不猜客户、不触发业务
	var identity *ChannelIdentity
	if o.resolver != nil && msg.AccountRef != "" {
		ref := strings.TrimSpace(msg.AccountRef)
		acc, aerr := o.resolver.resolver.ResolveByAccountRef(ctx, msg.Channel, ref)
		if aerr != nil {
			out.Status, out.Reason = "rejected", "账号身份解析失败"
			return out, aerr
		}
		conversationType := "dm"
		if msg.IsGroup {
			conversationType = "group"
		}
		// CH-08：把平台自己的稳定 userID 与群成员角色证据传到统一识别口，
		// 不再以空 self / nil members 调用（那会把所有发言人压成 unknown）。
		identity, aerr = o.resolver.IdentifySpeaker(
			ctx, acc.TenantID, acc.AccountID, conversationType,
			msg.SenderID, msg.SelfPlatformID, toGroupMemberRoles(msg.MemberRoles),
		)
		if aerr != nil {
			out.Status, out.Reason = "rejected", "自他识别失败"
			return out, aerr
		}
	}
	if identity != nil {
		out.IdentityRole = identity.Role
	}
	if IsUnknownIdentity(identity) {
		out.Automation = "skipped_unknown"
	}
	// 4) 持久化：失败显式返回，绝不报业务成功
	hub := &model.MessageHub{
		TenantID:       msg.TenantID,
		Platform:       msg.Channel,
		MsgID:          msg.EventID,
		Direction:      "inbound",
		AccountID:      channelFirstNonEmpty(msg.AccountRef, msg.IdentifyCode),
		ConversationID: msg.ConversationID,
		SenderID:       msg.SenderID,
		Content:        msg.Content,
		MsgType:        msg.MsgType,
		IsGroup:        msg.IsGroup,
		GroupID:        msg.GroupID,
	}
	// 入站信封时间是 UnixMilli（channelgw adapter 口径），落库 SentAt 同源，
	// 供归档适配器的乱序检测按同一时间轴比较。
	if msg.Timestamp > 0 {
		hub.SentAt = time.UnixMilli(msg.Timestamp)
	}
	if err := o.archive.Archive(ctx, hub); err != nil {
		out.Status, out.Reason = "persist_failed", "归档失败"
		if errors.Is(err, ErrInboundDuplicate) {
			out.Status = "duplicate"
		}
		return out, fmt.Errorf("%w: %v", ErrInboundPersistFailed, err)
	}
	out.Status = "archived"
	out.ArchivedAt = time.Now().UTC().Format(time.RFC3339)

	// 5) 业务触发：unknown 不触发猜测自动回复
	if out.Automation == "skipped_unknown" || o.trigger == nil {
		return out, nil
	}
	decision, err := o.trigger.Trigger(ctx, hub, identity)
	if err != nil {
		out.Automation = "skipped_gate"
		return out, err // 闸决策失败显式上抛
	}
	out.Automation = decision
	return out, nil
}

// toGroupMemberRoles 把信封里的成员证据转成识别口类型（model 不依赖 service）。
func toGroupMemberRoles(rows []model.InboundMemberRole) []GroupMemberRole {
	if len(rows) == 0 {
		return nil
	}
	out := make([]GroupMemberRole, 0, len(rows))
	for _, r := range rows {
		out = append(out, GroupMemberRole{PlatformUserID: r.PlatformUserID, Role: r.Role})
	}
	return out
}

func channelFirstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
