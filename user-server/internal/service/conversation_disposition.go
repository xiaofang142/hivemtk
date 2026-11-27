package service

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/repository"
)

var (
	ErrConversationDispositionStoreUnavailable  = errors.New("逐消息处置存储不可用")
	ErrConversationDispositionScope             = errors.New("逐消息处置会话范围无效")
	ErrConversationDispositionNotFound          = errors.New("逐消息处置记录不存在")
	ErrConversationDispositionVersionConflict   = repository.ErrConversationDispositionVersionConflict
	ErrConversationDispositionInvalidTransition = errors.New("逐消息处置状态转换无效")
)

// ConversationDispositionScope 限定单聊/群聊根会话；群聊必须使用 group_id 派生后的根 conversation_id。
type ConversationDispositionScope struct {
	Platform         string
	ChannelAccountID uint
	ConversationID   string
}

// ConversationDispositionRef 在会话范围内唯一定位一条入站消息。
type ConversationDispositionRef struct {
	Platform         string
	ChannelAccountID uint
	ConversationID   string
	MessageID        string
}

// DispositionTransition 描述一次用户可见状态迁移；replied 必须同时带真实出站 ID 与送达确认。
type DispositionTransition struct {
	Status         string
	ReplyMessageID string
	DeliveredAt    *time.Time
	Reason         string
	Late           bool
}

// ConversationDispositionStore 是服务层反向依赖的窄存储口；实现负责租户隔离与 version CAS。
type ConversationDispositionStore interface {
	Get(ctx context.Context, tenantID uint, platform string, accountID uint, conversationID, messageID string) (*model.ConversationMessageDisposition, error)
	Save(ctx context.Context, item *model.ConversationMessageDisposition) error
	List(ctx context.Context, tenantID uint, platform string, accountID uint, conversationID string) ([]model.ConversationMessageDisposition, error)
}

type ConversationDispositionService struct {
	store ConversationDispositionStore
}

func NewConversationDispositionService(store ConversationDispositionStore) *ConversationDispositionService {
	return &ConversationDispositionService{store: store}
}

func (s *ConversationDispositionService) Ensure(ctx context.Context, item model.ConversationMessageDisposition) (*model.ConversationMessageDisposition, error) {
	tid, err := conversationDispositionTenantID(ctx)
	if err != nil {
		return nil, err
	}
	ref := ConversationDispositionRef{
		Platform: item.Platform, ChannelAccountID: item.ChannelAccountID,
		ConversationID: item.ConversationID, MessageID: item.MessageID,
	}
	if err := validateConversationDispositionRef(ref); err != nil {
		return nil, err
	}
	if strings.TrimSpace(item.MessageCursor) == "" || strings.TrimSpace(item.BusLLMSessionIDBlind) == "" {
		return nil, ErrConversationDispositionScope
	}
	if s == nil || s.store == nil {
		return nil, ErrConversationDispositionStoreUnavailable
	}
	existing, err := s.store.Get(ctx, tid, ref.Platform, ref.ChannelAccountID, ref.ConversationID, ref.MessageID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}
	item.TenantID = tid
	item.MessageID = strings.TrimSpace(item.MessageID)
	item.MessageCursor = strings.TrimSpace(item.MessageCursor)
	if item.Status == "" {
		item.Status = model.ConversationDispositionPending
	}
	if !validConversationDispositionStatus(item.Status) {
		return nil, ErrConversationDispositionInvalidTransition
	}
	item.Version = 1
	if err := s.store.Save(ctx, &item); err != nil {
		if !errors.Is(err, ErrConversationDispositionVersionConflict) {
			return nil, err
		}
		existing, getErr := s.store.Get(ctx, tid, ref.Platform, ref.ChannelAccountID, ref.ConversationID, ref.MessageID)
		if getErr != nil {
			return nil, getErr
		}
		if existing == nil {
			return nil, err
		}
		return existing, nil
	}
	return &item, nil
}

func (s *ConversationDispositionService) Transition(ctx context.Context, ref ConversationDispositionRef, expectedVersion int64, transition DispositionTransition) (*model.ConversationMessageDisposition, error) {
	tid, err := conversationDispositionTenantID(ctx)
	if err != nil {
		return nil, err
	}
	if err := validateConversationDispositionRef(ref); err != nil {
		return nil, err
	}
	if s == nil || s.store == nil {
		return nil, ErrConversationDispositionStoreUnavailable
	}
	current, err := s.store.Get(ctx, tid, ref.Platform, ref.ChannelAccountID, ref.ConversationID, ref.MessageID)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, ErrConversationDispositionNotFound
	}
	if current.Version != expectedVersion {
		return nil, ErrConversationDispositionVersionConflict
	}
	changed, err := applyDispositionTransition(current, transition)
	if err != nil {
		return nil, err
	}
	if !changed {
		return current, nil
	}
	if err := s.store.Save(ctx, current); err != nil {
		return nil, err
	}
	return current, nil
}

// TransitionCurrent 读取当前版本后执行 CAS 迁移，供投递出口在不跨层持有版本时使用。
func (s *ConversationDispositionService) TransitionCurrent(
	ctx context.Context,
	ref ConversationDispositionRef,
	transition DispositionTransition,
) (*model.ConversationMessageDisposition, error) {
	tid, err := conversationDispositionTenantID(ctx)
	if err != nil {
		return nil, err
	}
	if s == nil || s.store == nil {
		return nil, ErrConversationDispositionStoreUnavailable
	}
	current, err := s.store.Get(ctx, tid, ref.Platform, ref.ChannelAccountID, ref.ConversationID, ref.MessageID)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, ErrConversationDispositionNotFound
	}
	return s.Transition(ctx, ref, current.Version, transition)
}

func (s *ConversationDispositionService) ReplyWatermark(ctx context.Context, scope ConversationDispositionScope) (string, error) {
	tid, err := conversationDispositionTenantID(ctx)
	if err != nil {
		return "", err
	}
	if err := validateConversationDispositionScope(scope); err != nil {
		return "", err
	}
	if s == nil || s.store == nil {
		return "", ErrConversationDispositionStoreUnavailable
	}
	items, err := s.store.List(ctx, tid, scope.Platform, scope.ChannelAccountID, scope.ConversationID)
	if err != nil {
		return "", err
	}
	return ComputeConversationReplyWatermark(items), nil
}

func applyDispositionTransition(item *model.ConversationMessageDisposition, transition DispositionTransition) (bool, error) {
	if item == nil {
		return false, ErrConversationDispositionNotFound
	}
	target := strings.TrimSpace(transition.Status)
	if !validConversationDispositionStatus(target) {
		return false, ErrConversationDispositionInvalidTransition
	}
	if item.Status == target {
		switch target {
		case model.ConversationDispositionReplied:
			if strings.TrimSpace(transition.ReplyMessageID) == strings.TrimSpace(item.ReplyMessageID) && sameDispositionDeliveryTime(transition.DeliveredAt, item.DeliveredAt) {
				return false, nil
			}
		case model.ConversationDispositionSkipped, model.ConversationDispositionMerged:
			if strings.TrimSpace(transition.Reason) == strings.TrimSpace(item.Reason) {
				return false, nil
			}
		default:
			return false, nil
		}
		return false, ErrConversationDispositionInvalidTransition
	}

	switch target {
	case model.ConversationDispositionProcessing:
		if item.Status != model.ConversationDispositionPending {
			return false, ErrConversationDispositionInvalidTransition
		}
		item.Status = target
		item.Late = transition.Late
		return true, nil
	case model.ConversationDispositionReplied:
		if item.Status != model.ConversationDispositionProcessing {
			return false, ErrConversationDispositionInvalidTransition
		}
		replyID := strings.TrimSpace(transition.ReplyMessageID)
		if replyID == "" || transition.DeliveredAt == nil || transition.DeliveredAt.IsZero() {
			return false, ErrConversationDispositionInvalidTransition
		}
		item.Status = target
		item.Late = transition.Late
		item.ReplyMessageID = replyID
		deliveredAt := *transition.DeliveredAt
		item.DeliveredAt = &deliveredAt
		item.Reason = strings.TrimSpace(transition.Reason)
		return true, nil
	case model.ConversationDispositionSkipped, model.ConversationDispositionMerged:
		if item.Status != model.ConversationDispositionPending && item.Status != model.ConversationDispositionProcessing {
			return false, ErrConversationDispositionInvalidTransition
		}
		reason := strings.TrimSpace(transition.Reason)
		if reason == "" {
			return false, ErrConversationDispositionInvalidTransition
		}
		item.Status = target
		item.Late = transition.Late
		item.ReplyMessageID = ""
		item.DeliveredAt = nil
		item.Reason = reason
		return true, nil
	default:
		return false, ErrConversationDispositionInvalidTransition
	}
}

// ComputeConversationReplyWatermark 仅推进连续且已确认送达的 replied 前缀；merged/skipped 不伪造回复。
func ComputeConversationReplyWatermark(items []model.ConversationMessageDisposition) string {
	ordered := append([]model.ConversationMessageDisposition{}, items...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return compareConversationDispositionCursors(ordered[i].MessageCursor, ordered[j].MessageCursor) < 0
	})
	watermark := ""
	for _, item := range ordered {
		if item.Status != model.ConversationDispositionReplied || strings.TrimSpace(item.ReplyMessageID) == "" || item.DeliveredAt == nil || item.DeliveredAt.IsZero() {
			break
		}
		watermark = item.MessageCursor
	}
	return watermark
}

// DecideLateConversationDisposition 实现“未回复则补、已回复则合并”；超窗未回复消息跳过并留原因。
func DecideLateConversationDisposition(item model.ConversationMessageDisposition, watermark string, now time.Time, window time.Duration) (DispositionTransition, error) {
	if window <= 0 {
		return DispositionTransition{}, errors.New("迟到处置窗口必须大于 0")
	}
	if item.MessageCursor == "" || item.CreatedAt.IsZero() || now.IsZero() || now.Before(item.CreatedAt) {
		return DispositionTransition{}, errors.New("迟到处置缺少可信游标或接收时间")
	}
	if watermark != "" && compareConversationDispositionCursors(item.MessageCursor, watermark) <= 0 {
		return DispositionTransition{Status: model.ConversationDispositionMerged, Reason: "covered_by_newer_reply", Late: true}, nil
	}
	if now.Sub(item.CreatedAt) > window {
		return DispositionTransition{Status: model.ConversationDispositionSkipped, Reason: "late_window_expired", Late: true}, nil
	}
	return DispositionTransition{Status: model.ConversationDispositionPending, Reason: "late_unreplied", Late: true}, nil
}

func compareConversationDispositionCursors(left, right string) int {
	leftNumber, leftNumeric := conversationDispositionCursorNumber(left)
	rightNumber, rightNumeric := conversationDispositionCursorNumber(right)
	if leftNumeric && rightNumeric {
		if leftNumber < rightNumber {
			return -1
		}
		if leftNumber > rightNumber {
			return 1
		}
	}
	return strings.Compare(strings.TrimSpace(left), strings.TrimSpace(right))
}

func conversationDispositionCursorNumber(cursor string) (uint64, bool) {
	cursor = strings.TrimSpace(cursor)
	cursor = strings.TrimPrefix(cursor, "hub:")
	value, err := strconv.ParseUint(cursor, 10, 64)
	return value, err == nil
}

func sameDispositionDeliveryTime(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func validConversationDispositionStatus(status string) bool {
	switch status {
	case model.ConversationDispositionPending, model.ConversationDispositionProcessing,
		model.ConversationDispositionReplied, model.ConversationDispositionSkipped,
		model.ConversationDispositionMerged:
		return true
	default:
		return false
	}
}

func validateConversationDispositionRef(ref ConversationDispositionRef) error {
	if ref.MessageID == "" {
		return ErrConversationDispositionScope
	}
	return validateConversationDispositionScope(ConversationDispositionScope{
		Platform: ref.Platform, ChannelAccountID: ref.ChannelAccountID, ConversationID: ref.ConversationID,
	})
}

func validateConversationDispositionScope(scope ConversationDispositionScope) error {
	if strings.TrimSpace(scope.Platform) == "" || scope.ChannelAccountID == 0 || strings.TrimSpace(scope.ConversationID) == "" {
		return ErrConversationDispositionScope
	}
	return nil
}

func conversationDispositionTenantID(ctx context.Context) (uint, error) {
	if ctx == nil || tenant.IsSystemScope(ctx) {
		return 0, ErrConversationDispositionScope
	}
	tid, ok := tenant.FromContext(ctx)
	if (!ok || tid == 0) && !tenant.IsMultiTenant() {
		tid = tenant.DefaultTenantID
	}
	if tid == 0 {
		return 0, ErrConversationDispositionScope
	}
	return tid, nil
}
