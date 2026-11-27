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

// ErrConversationContextVersionConflict 共享上下文 CAS 冲突；调用方必须重读最新版本后重试。
var ErrConversationContextVersionConflict = errors.New("共享对话上下文版本冲突")

// ConversationContextRepository 两智能体共享对话上下文仓储（FR-020/021）。
type ConversationContextRepository interface {
	Get(ctx context.Context, channelAccountID uint, externalConversationID string) (*model.ConversationContext, error)
	Upsert(ctx context.Context, m *model.ConversationContext) error
	List(ctx context.Context, channelAccountID uint, limit int) ([]model.ConversationContext, error)
}

type conversationContextRepo struct {
	db *gorm.DB
}

func NewConversationContextRepository() ConversationContextRepository {
	return &conversationContextRepo{db: _db.GetDB()}
}

func NewConversationContextRepositoryWithDB(db *gorm.DB) ConversationContextRepository {
	return &conversationContextRepo{db: db}
}

func (r *conversationContextRepo) scoped(ctx context.Context) (*gorm.DB, uint, error) {
	if r == nil || r.db == nil {
		return nil, 0, ErrChannelStoreUnavailable
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

func (r *conversationContextRepo) Get(ctx context.Context, channelAccountID uint, externalConversationID string) (*model.ConversationContext, error) {
	q, tid, err := r.scoped(ctx)
	if err != nil {
		return nil, err
	}
	var m model.ConversationContext
	err = q.Where("channel_account_id = ? AND external_conversation_id = ?", channelAccountID, externalConversationID).First(&m).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &model.ConversationContext{TenantID: tid, ChannelAccountID: channelAccountID, ExternalConversationID: externalConversationID}, nil
		}
		return nil, err
	}
	return &m, nil
}

// Upsert 以 version 做 compare-and-swap；首建唯一冲突同样转成版本冲突，迫使调用方重读。
func (r *conversationContextRepo) Upsert(ctx context.Context, m *model.ConversationContext) error {
	if m == nil {
		return errors.New("对话上下文为空")
	}
	_, tid, err := r.scoped(ctx)
	if err != nil {
		return err
	}
	m.TenantID = tid
	if m.Version <= 0 {
		m.Version = 1
	}
	if m.SummaryVersion <= 0 {
		m.SummaryVersion = 1
	}
	if m.ID == 0 {
		if err := r.db.WithContext(ctx).Create(m).Error; err != nil {
			if isDuplicateKeyErr(err) {
				return ErrConversationContextVersionConflict
			}
			return err
		}
		return nil
	}

	expectedVersion := m.Version
	result := r.db.WithContext(ctx).
		Model(&model.ConversationContext{}).
		Where("tenant_id = ? AND id = ? AND version = ?", tid, m.ID, expectedVersion).
		Updates(map[string]any{
			"member_roles":       m.MemberRoles,
			"messages":           m.Messages,
			"tags":               m.Tags,
			"summary":            m.Summary,
			"source_cursor":      m.SourceCursor,
			"source_cursor_hash": m.SourceCursorHash,
			"summary_version":    m.SummaryVersion,
			"version":            expectedVersion + 1,
			"updated_at":         time.Now(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrConversationContextVersionConflict
	}
	m.Version = expectedVersion + 1
	return nil
}

func (r *conversationContextRepo) List(ctx context.Context, channelAccountID uint, limit int) ([]model.ConversationContext, error) {
	q, _, err := r.scoped(ctx)
	if err != nil {
		return nil, err
	}
	if channelAccountID > 0 {
		q = q.Where("channel_account_id = ?", channelAccountID)
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var items []model.ConversationContext
	err = q.Order("updated_at DESC").Limit(limit).Find(&items).Error
	return items, err
}
