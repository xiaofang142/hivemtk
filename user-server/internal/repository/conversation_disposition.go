package repository

import (
	"context"
	"errors"
	"time"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
)

// ErrConversationDispositionVersionConflict 逐消息处置 CAS 冲突；调用方必须重读后重试。
var ErrConversationDispositionVersionConflict = errors.New("逐消息处置版本冲突")

// ConversationDispositionRepository 持久化用户可见的逐入站消息处置状态。
type ConversationDispositionRepository interface {
	Get(ctx context.Context, tenantID uint, platform string, accountID uint, conversationID, messageID string) (*model.ConversationMessageDisposition, error)
	Save(ctx context.Context, item *model.ConversationMessageDisposition) error
	List(ctx context.Context, tenantID uint, platform string, accountID uint, conversationID string) ([]model.ConversationMessageDisposition, error)
}

type conversationDispositionRepo struct {
	db *gorm.DB
}

func NewConversationDispositionRepository() ConversationDispositionRepository {
	return &conversationDispositionRepo{db: _db.GetDB()}
}

func NewConversationDispositionRepositoryWithDB(db *gorm.DB) ConversationDispositionRepository {
	return &conversationDispositionRepo{db: db}
}

func (r *conversationDispositionRepo) scoped(ctx context.Context) (*gorm.DB, uint, error) {
	if r == nil || r.db == nil {
		return nil, 0, ErrChannelStoreUnavailable
	}
	if ctx == nil || tenant.IsSystemScope(ctx) {
		return nil, 0, ErrLeadTenantRequired
	}
	tid, ok := tenant.FromContext(ctx)
	if !ok || tid == 0 {
		if tenant.IsMultiTenant() {
			return nil, 0, ErrLeadTenantRequired
		}
		tid = tenant.DefaultTenantID
	}
	return r.db.WithContext(ctx).Where("tenant_id = ?", tid), tid, nil
}

func (r *conversationDispositionRepo) Get(ctx context.Context, tenantID uint, platform string, accountID uint, conversationID, messageID string) (*model.ConversationMessageDisposition, error) {
	q, tid, err := r.scoped(ctx)
	if err != nil {
		return nil, err
	}
	if tenantID == 0 || tenantID != tid {
		return nil, ErrLeadTenantRequired
	}
	var item model.ConversationMessageDisposition
	err = q.Where(
		"platform = ? AND channel_account_id = ? AND conversation_id = ? AND message_id = ?",
		platform, accountID, conversationID, messageID,
	).First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *conversationDispositionRepo) Save(ctx context.Context, item *model.ConversationMessageDisposition) error {
	if item == nil {
		return errors.New("逐消息处置为空")
	}
	_, tid, err := r.scoped(ctx)
	if err != nil {
		return err
	}
	item.TenantID = tid
	if item.Version <= 0 {
		item.Version = 1
	}
	if item.ID == 0 {
		if err := r.db.WithContext(ctx).Create(item).Error; err != nil {
			if isDuplicateKeyErr(err) {
				return ErrConversationDispositionVersionConflict
			}
			return err
		}
		return nil
	}

	expectedVersion := item.Version
	result := r.db.WithContext(ctx).
		Model(&model.ConversationMessageDisposition{}).
		Where("tenant_id = ? AND id = ? AND version = ?", tid, item.ID, expectedVersion).
		Updates(map[string]any{
			"group_id":         item.GroupID,
			"status":           item.Status,
			"reply_message_id": item.ReplyMessageID,
			"delivered_at":     item.DeliveredAt,
			"late":             item.Late,
			"reason":           item.Reason,
			"version":          expectedVersion + 1,
			"updated_at":       time.Now(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrConversationDispositionVersionConflict
	}
	item.Version = expectedVersion + 1
	return nil
}

func (r *conversationDispositionRepo) List(ctx context.Context, tenantID uint, platform string, accountID uint, conversationID string) ([]model.ConversationMessageDisposition, error) {
	q, tid, err := r.scoped(ctx)
	if err != nil {
		return nil, err
	}
	if tenantID == 0 || tenantID != tid {
		return nil, ErrLeadTenantRequired
	}
	items := make([]model.ConversationMessageDisposition, 0)
	err = q.Where(
		"platform = ? AND channel_account_id = ? AND conversation_id = ?",
		platform, accountID, conversationID,
	).Order("created_at ASC, id ASC").Limit(10000).Find(&items).Error
	return items, err
}
