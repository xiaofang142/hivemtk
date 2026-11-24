package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/logger"

	"gorm.io/gorm"
)

const (
	SageXSchemaVersion   = "1.0"
	SageXMaxEvents       = 50
	SageXMaxRequestBytes = 1_048_576
	SageXMaxAckBytes     = 262_144
	SageXAckAccepted     = "accepted"
	SageXAckDuplicate    = "duplicate"
	SageXAckRetry        = "retry"
	SageXAckRejected     = "rejected"
)

var (
	ErrSageXUnauthorized      = errors.New("sagex: unauthorized")
	ErrSageXInvalidEnvelope   = errors.New("sagex: invalid envelope")
	ErrSageXUnsupportedSchema = errors.New("sagex: unsupported schema")
	ErrSageXTooManyEvents     = errors.New("sagex: too many events")
	ErrSageXEmptyEvents       = errors.New("sagex: empty events")
)

type SageXUploadRequest struct {
	SchemaVersion  string `json:"schema_version"`
	BatchID        string `json:"batch_id"`
	SentAt         string `json:"sent_at"`
	InstallationID string `json:"installation_id"`
	// IdentifyCode 渠道唯一识别码（T059 / FR-028）：SageX 上报以此解析租户/账号，
	// 与安装凭证（Authorization Bearer）双重校验；两者归属不一致时 fail-closed 拒绝。
	IdentifyCode string           `json:"identify_code"`
	Events       []SageXEventItem `json:"events"`
}

type SageXEventItem struct {
	EventID         string               `json:"event_id"`
	EventType       string               `json:"event_type"`
	CapturedAt      string               `json:"captured_at"`
	Source          SageXSource          `json:"source"`
	ParseStatus     string               `json:"parse_status"`
	Conversation    SageXConversation    `json:"conversation"`
	Message         SageXMessage         `json:"message"`
	RawNotification SageXRawNotification `json:"raw_notification"`
	TruncatedFields []string             `json:"truncated_fields"`
}

type SageXSource struct {
	Platform string `json:"platform"`
	Package  string `json:"package"`
}

type SageXConversation struct {
	Type  string `json:"type"`
	Title string `json:"title"`
}

// SageXFlexInt64 accepts JSON number or numeric string for protocol robustness.
type SageXFlexInt64 int64

func (v *SageXFlexInt64) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if s == "" || s == "null" {
		*v = 0
		return nil
	}
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		var str string
		if err := json.Unmarshal(b, &str); err != nil {
			return err
		}
		s = strings.TrimSpace(str)
		if s == "" {
			*v = 0
			return nil
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		f, ferr := strconv.ParseFloat(s, 64)
		if ferr != nil {
			return err
		}
		*v = SageXFlexInt64(int64(f))
		return nil
	}
	*v = SageXFlexInt64(n)
	return nil
}

func (v SageXFlexInt64) Int64() int64 { return int64(v) }

// SageXMessage matches SageX notification-upload-v1: sender is string|null,
// notification_posted_at is integer milliseconds.
type SageXMessage struct {
	Sender               string         `json:"sender"`
	Text                 string         `json:"text"`
	NotificationPostedAt SageXFlexInt64 `json:"notification_posted_at"`
}

type SageXRawNotification struct {
	Key                 string              `json:"key"`
	ID                  int64               `json:"id"`
	Tag                 string              `json:"tag"`
	Title               string              `json:"title"`
	Text                string              `json:"text"`
	BigText             string              `json:"big_text"`
	SubText             string              `json:"sub_text"`
	SummaryText         string              `json:"summary_text"`
	ConversationTitle   string              `json:"conversation_title"`
	TextLines           []string            `json:"text_lines"`
	TickerText          string              `json:"ticker_text"`
	Category            string              `json:"category"`
	ChannelID           string              `json:"channel_id"`
	GroupKey            string              `json:"group_key"`
	IsGroupSummary      bool                `json:"is_group_summary"`
	IsGroupConversation *bool               `json:"is_group_conversation"`
	Messages            []SageXStyleMessage `json:"messages"`
	HistoricMessages    []SageXStyleMessage `json:"historic_messages"`
}

type SageXStyleMessage struct {
	Text       string `json:"text"`
	Timestamp  int64  `json:"timestamp"`
	SenderName string `json:"sender_name"`
	SenderKey  string `json:"sender_key"`
	MimeType   string `json:"mime_type"`
	HasDataURI bool   `json:"has_data_uri"`
}

type SageXUploadAckResponse struct {
	SchemaVersion string         `json:"schema_version"`
	BatchID       string         `json:"batch_id"`
	Acks          []SageXAckItem `json:"acks"`
}

type SageXAckItem struct {
	EventID string `json:"event_id"`
	Status  string `json:"status"`
	Code    string `json:"code,omitempty"`
}

type SageXInstallationStore interface {
	Create(ctx context.Context, inst *model.SageXInstallation) error
	Update(ctx context.Context, inst *model.SageXInstallation) error
	GetByID(ctx context.Context, id uint) (*model.SageXInstallation, error)
	GetByToken(ctx context.Context, token string) (*model.SageXInstallation, error)
	GetByInstallationID(ctx context.Context, installationID string) (*model.SageXInstallation, error)
	TouchLastSeen(ctx context.Context, id uint, at time.Time) error
}

type SageXEventReceiptStore interface {
	GetByInstallationAndEventID(ctx context.Context, tenantID uint, installationID, eventID string) (*model.SageXEventReceipt, error)
	Create(ctx context.Context, receipt *model.SageXEventReceipt) error
	Update(ctx context.Context, receipt *model.SageXEventReceipt) error
}

type SageXIngress interface {
	HandleIngressMessage(ctx context.Context, event *model.MessageEvent) (*InboxIngressResult, error)
}

type SageXUploadService struct {
	instRepo    SageXInstallationStore
	receiptRepo SageXEventReceiptStore
	ingress     SageXIngress
	// resolver 可选：装配后 SageX 上报可用 identify_code 解析租户/账号（T059）。
	// 未装配且请求带 identify_code 时 fail-closed 拒绝，不静默放行。
	resolver *ChannelResolver
}

// SetChannelResolver 注入渠道识别码解析器（装配点用）。
func (s *SageXUploadService) SetChannelResolver(r *ChannelResolver) {
	if s != nil && r != nil {
		s.resolver = r
	}
}

func NewSageXUploadService(
	instRepo SageXInstallationStore,
	receiptRepo SageXEventReceiptStore,
	ingress SageXIngress,
) *SageXUploadService {
	return &SageXUploadService{instRepo: instRepo, receiptRepo: receiptRepo, ingress: ingress}
}

func (s *SageXUploadService) HandleUpload(ctx context.Context, token string, req *SageXUploadRequest, rawBodySize int) (*SageXUploadAckResponse, error) {
	if req == nil {
		return nil, ErrSageXInvalidEnvelope
	}
	if rawBodySize > SageXMaxRequestBytes {
		return nil, ErrSageXInvalidEnvelope
	}
	if strings.TrimSpace(req.SchemaVersion) != SageXSchemaVersion {
		return nil, ErrSageXUnsupportedSchema
	}
	if strings.TrimSpace(req.BatchID) == "" {
		return nil, ErrSageXInvalidEnvelope
	}
	if len(req.Events) == 0 {
		return nil, ErrSageXEmptyEvents
	}
	if len(req.Events) > SageXMaxEvents {
		return nil, ErrSageXTooManyEvents
	}

	inst, err := s.instRepo.GetByToken(ctx, strings.TrimSpace(token))
	if err != nil {
		return nil, err
	}
	if inst == nil {
		return nil, ErrSageXUnauthorized
	}
	// T059：带识别码时由 ChannelResolver 全局唯一反查租户/账号，并与安装凭证归属
	// 交叉校验；解析不出或分属不同租户一律拒绝（fail-closed，不回落默认租户）。
	code := strings.TrimSpace(req.IdentifyCode)
	if code != "" {
		if s.resolver == nil {
			return nil, ErrSageXUnauthorized
		}
		resolved, rerr := s.resolver.ResolveByIdentifyCode(ctx, code)
		if rerr != nil || resolved == nil || resolved.TenantID == 0 || resolved.TenantID != inst.TenantID {
			logger.Ctx(ctx).Warn().Msgf("[SageX] identify_code 解析失败或与安装凭证归属不一致 err=%v", rerr)
			return nil, ErrSageXUnauthorized
		}
	}
	reqInstallationID := strings.TrimSpace(req.InstallationID)
	// installation_id 不是授权依据；同一 token 可能被多个私用设备复用（如 QQ/微信分端），
	// 以最后上报的 installation_id 作为展示/绑定值，不因不一致拒绝。
	// installation_id 只在首次绑定时写入，之后保持稳定；不随每次请求覆盖。
	// 否则多设备共用 token 会来回改写绑定值，导致 account_id/会话/receipt 命名空间漂移，
	// 同一客户被拆成多个会话，甚至重试时查不到原 receipt 而重复入库。
	if inst.InstallationID == "" && reqInstallationID != "" {
		inst.InstallationID = reqInstallationID
		if err := s.instRepo.Update(tenant.WithTenantID(ctx, inst.TenantID), inst); err != nil {
			logger.Ctx(ctx).Warn().Msgf("[SageX] bind installation failed installation=%s err=%v", reqInstallationID, err)
		}
	}
	effectiveInstallationID := inst.InstallationID
	if effectiveInstallationID == "" {
		effectiveInstallationID = reqInstallationID
	}
	tenantCtx := tenant.WithTenantID(ctx, inst.TenantID)
	_ = s.instRepo.TouchLastSeen(tenantCtx, inst.ID, time.Now())

	counts := make(map[string]int, len(req.Events))
	for i := range req.Events {
		id := strings.TrimSpace(req.Events[i].EventID)
		if id != "" {
			counts[id]++
		}
	}

	acks := make([]SageXAckItem, 0, len(req.Events))
	for i := range req.Events {
		ev := req.Events[i]
		eventID := strings.TrimSpace(ev.EventID)
		if eventID == "" {
			acks = append(acks, SageXAckItem{EventID: ev.EventID, Status: SageXAckRejected, Code: "INVALID_EVENT_ID"})
			continue
		}
		if counts[eventID] > 1 {
			// 同一 event_id 在批次内重复：按协议该事件 ACK 全部无效，缺失 ACK 使其保持 pending。
			continue
		}

		ack := s.processEvent(tenantCtx, inst, effectiveInstallationID, req.BatchID, &ev)
		acks = append(acks, ack)
	}

	return &SageXUploadAckResponse{
		SchemaVersion: SageXSchemaVersion,
		BatchID:       req.BatchID,
		Acks:          acks,
	}, nil
}

func (s *SageXUploadService) processEvent(ctx context.Context, inst *model.SageXInstallation, installationID, batchID string, ev *SageXEventItem) SageXAckItem {
	eventID := strings.TrimSpace(ev.EventID)
	receipt, err := s.receiptRepo.GetByInstallationAndEventID(ctx, inst.TenantID, installationID, eventID)
	if err != nil {
		logger.Ctx(ctx).Warn().Msgf("[SageX] receipt lookup failed installation=%s event=%s err=%v", installationID, eventID, err)
		return SageXAckItem{EventID: eventID, Status: SageXAckRetry, Code: "RECEIPT_LOOKUP"}
	}
	if receipt != nil {
		switch receipt.Status {
		case SageXAckAccepted, SageXAckDuplicate:
			return SageXAckItem{EventID: eventID, Status: SageXAckDuplicate, Code: "ALREADY_PROCESSED"}
		case SageXAckRejected:
			// rejected 是终态：重试仍返回 rejected，客户端应保持 blocked，不能被误标为已确认。
			return SageXAckItem{EventID: eventID, Status: SageXAckRejected, Code: "ALREADY_REJECTED"}
		}
	}

	if strings.TrimSpace(ev.EventType) != "notification.posted" {
		return s.saveTerminalReceipt(ctx, inst, installationID, batchID, eventID, SageXAckRejected, 0, "UNSUPPORTED_EVENT")
	}

	platform, ok := sagexPlatform(ev.Source.Package)
	if !ok {
		return s.saveTerminalReceipt(ctx, inst, installationID, batchID, eventID, SageXAckRejected, 0, "UNSUPPORTED_PACKAGE")
	}

	msgEvent, buildErr := s.buildMessageEvent(inst, installationID, batchID, platform, ev)
	if buildErr != nil {
		logger.Ctx(ctx).Warn().Msgf("[SageX] build event failed installation=%s event=%s err=%v", installationID, eventID, buildErr)
		return SageXAckItem{EventID: eventID, Status: SageXAckRetry, Code: "BUILD_EVENT"}
	}

	if s.ingress == nil {
		return SageXAckItem{EventID: eventID, Status: SageXAckRetry, Code: "INGRESS_UNAVAILABLE"}
	}

	result, err := s.ingress.HandleIngressMessage(ctx, msgEvent)
	if err != nil {
		logger.Ctx(ctx).Warn().Msgf("[SageX] ingress failed installation=%s event=%s err=%v", installationID, eventID, err)
		return SageXAckItem{EventID: eventID, Status: SageXAckRetry, Code: "INGEST_FAILED"}
	}

	status := SageXAckAccepted
	if result != nil {
		if isSageXIngressDuplicateReason(result.Reason) {
			status = SageXAckDuplicate
		} else if !result.Accepted {
			return s.saveTerminalReceipt(ctx, inst, installationID, batchID, eventID, SageXAckRejected, 0, "INGRESS_REJECTED")
		}
	}
	receipt = &model.SageXEventReceipt{
		TenantID:       inst.TenantID,
		InstallationID: installationID,
		EventID:        eventID,
		BatchID:        batchID,
		Status:         status,
	}
	if err := s.receiptRepo.Create(ctx, receipt); err != nil {
		// unique conflict means another concurrent request processed it
		if isUniqueConflict(err) {
			return SageXAckItem{EventID: eventID, Status: SageXAckDuplicate, Code: "ALREADY_PROCESSED"}
		}
		logger.Ctx(ctx).Warn().Msgf("[SageX] receipt create failed installation=%s event=%s err=%v", installationID, eventID, err)
		// message may already be ingested; return retry to be safe? No - better accepted if ingress ok.
		// But we can't guarantee dedupe next time without receipt. Return retry with code.
		return SageXAckItem{EventID: eventID, Status: SageXAckRetry, Code: "RECEIPT_SAVE"}
	}

	return SageXAckItem{EventID: eventID, Status: status}
}

func (s *SageXUploadService) saveTerminalReceipt(ctx context.Context, inst *model.SageXInstallation, installationID, batchID, eventID, status string, msgHubID uint, code string) SageXAckItem {
	receipt := &model.SageXEventReceipt{
		TenantID:       inst.TenantID,
		InstallationID: installationID,
		EventID:        eventID,
		BatchID:        batchID,
		Status:         status,
		MessageHubID:   msgHubID,
	}
	if err := s.receiptRepo.Create(ctx, receipt); err != nil {
		if isUniqueConflict(err) {
			return SageXAckItem{EventID: eventID, Status: SageXAckDuplicate, Code: "ALREADY_PROCESSED"}
		}
		logger.Ctx(ctx).Warn().Msgf("[SageX] terminal receipt create failed installation=%s event=%s err=%v", installationID, eventID, err)
		return SageXAckItem{EventID: eventID, Status: SageXAckRetry, Code: "RECEIPT_SAVE"}
	}
	ack := SageXAckItem{EventID: eventID, Status: status}
	if code != "" {
		ack.Code = code
	}
	return ack
}

func (s *SageXUploadService) buildMessageEvent(inst *model.SageXInstallation, installationID, batchID, platform string, ev *SageXEventItem) (*model.MessageEvent, error) {
	eventID := strings.TrimSpace(ev.EventID)
	accountID := strings.TrimSpace(inst.AccountID)
	if accountID == "" {
		// 用安装绑定行 ID 派生，跨 installation_id 变化保持稳定，避免重装/换机后账号分裂。
		accountID = "sagex_inst_" + strconv.FormatUint(uint64(inst.ID), 10)
	}

	conversationID := sagexConversationID(installationID, platform, ev.Conversation)
	senderName := strings.TrimSpace(ev.Message.Sender)
	if senderName == "" {
		senderName = strings.TrimSpace(ev.Conversation.Title)
	}
	customerID := conversationID
	senderID := customerID
	isGroup := ev.Conversation.Type == "group"
	groupID := ""
	if isGroup {
		groupID = conversationID
		// keep sender as group member identity if possible
		if senderName != "" {
			senderID = conversationID + " " + sagexStableID(senderName)
		}
	}

	ts := parseSageXTime(ev.CapturedAt)
	if ts.IsZero() && ev.Message.NotificationPostedAt.Int64() > 0 {
		ts = time.UnixMilli(ev.Message.NotificationPostedAt.Int64())
	}
	if ts.IsZero() {
		ts = time.Now()
	}

	content := ev.Message.Text
	if strings.TrimSpace(content) == "" {
		content = "[通知消息]"
	}
	content = sanitizeSageXText(content)

	msgEvent := &model.MessageEvent{
		EventID:        "sagex:" + installationID + ":" + eventID,
		SessionID:      "sagex:" + installationID + ":" + conversationID,
		EventType:      model.EventTypeMessageReceived,
		Channel:        platform,
		SenderID:       senderID,
		SenderName:     senderName,
		SenderType:     "customer",
		ReceiverID:     accountID,
		MsgType:        "text",
		Content:        content,
		ConversationID: conversationID,
		IsGroup:        isGroup,
		GroupID:        groupID,
		Timestamp:      ts,
		Extra: map[string]any{
			"sagex": map[string]any{
				"event_id":        eventID,
				"batch_id":        batchID,
				"installation_id": installationID,
				"source_package":  ev.Source.Package,
				"parse_status":    ev.ParseStatus,
				"conversation":    ev.Conversation,
				"raw_notification": map[string]any{
					"key":                   ev.RawNotification.Key,
					"id":                    ev.RawNotification.ID,
					"tag":                   ev.RawNotification.Tag,
					"title":                 ev.RawNotification.Title,
					"text":                  ev.RawNotification.Text,
					"big_text":              ev.RawNotification.BigText,
					"sub_text":              ev.RawNotification.SubText,
					"summary_text":          ev.RawNotification.SummaryText,
					"conversation_title":    ev.RawNotification.ConversationTitle,
					"text_lines":            ev.RawNotification.TextLines,
					"ticker_text":           ev.RawNotification.TickerText,
					"category":              ev.RawNotification.Category,
					"channel_id":            ev.RawNotification.ChannelID,
					"group_key":             ev.RawNotification.GroupKey,
					"is_group_summary":      ev.RawNotification.IsGroupSummary,
					"is_group_conversation": ev.RawNotification.IsGroupConversation,
				},
				"truncated_fields": ev.TruncatedFields,
			},
			"account_id": accountID,
		},
	}
	return msgEvent, nil
}

func sagexPlatform(pkg string) (string, bool) {
	switch strings.TrimSpace(pkg) {
	case "com.tencent.mobileqq":
		return model.ChannelQQ, true
	case "com.tencent.mm":
		return model.ChannelPersonalWX, true
	default:
		return "", false
	}
}

func sagexConversationID(installationID, platform string, conv SageXConversation) string {
	ctype := strings.TrimSpace(conv.Type)
	if ctype == "" {
		ctype = "unknown"
	}
	title := strings.TrimSpace(conv.Title)
	if title == "" {
		title = "unknown"
	}
	return "sagex:" + sagexStableID(installationID+"|"+platform+"|"+ctype+"|"+title)
}

func sagexStableID(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:16])
}

func parseSageXTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	return time.Time{}
}

func sanitizeSageXText(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	return strings.ToValidUTF8(s, "")
}

func isUniqueConflict(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate") || strings.Contains(msg, "unique constraint") || strings.Contains(msg, "sqlstate 23505")
}

func (s *SageXUploadService) CreateInstallation(ctx context.Context, inst *model.SageXInstallation) error {
	if inst == nil {
		return ErrSageXInvalidEnvelope
	}
	if strings.TrimSpace(inst.Token) == "" {
		inst.Token = generateSageXToken()
	}
	if inst.Status == 0 {
		inst.Status = 1
	}
	return s.instRepo.Create(ctx, inst)
}

func (s *SageXUploadService) UpdateInstallation(ctx context.Context, inst *model.SageXInstallation) error {
	if inst == nil || inst.ID == 0 {
		return ErrSageXInvalidEnvelope
	}
	return s.instRepo.Update(ctx, inst)
}

func (s *SageXUploadService) GetInstallationByID(ctx context.Context, id uint) (*model.SageXInstallation, error) {
	return s.instRepo.GetByID(ctx, id)
}

func (s *SageXUploadService) GetInstallationByInstallationID(ctx context.Context, installationID string) (*model.SageXInstallation, error) {
	return s.instRepo.GetByInstallationID(ctx, installationID)
}

func generateSageXToken() string {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "sx_" + sagexStableID(time.Now().UTC().Format(time.RFC3339Nano))
	}
	return "sx_" + hex.EncodeToString(buf)
}

func isSageXIngressDuplicateReason(reason string) bool {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return false
	}
	for _, prefix := range []string{
		"msg_id already exists",
		"msg_id exists with different direction",
		"content_hash already exists",
		"intercepted by middleware",
		"self-echo",
		"self echo",
		"duplicate",
	} {
		if strings.HasPrefix(reason, prefix) {
			return true
		}
	}
	return false
}
