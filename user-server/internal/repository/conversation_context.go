package repository

import (
	"context"
	"errors"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

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

// Upsert 按 (tenant_id, channel_account_id, external_conversation_id) 唯一键原子写。
func (r *conversationContextRepo) Upsert(ctx context.Context, m *model.ConversationContext) error {
	if m == nil {
		return errors.New("对话上下文为空")
	}
	_, tid, err := r.scoped(ctx)
	if err != nil {
		return err
	}
	m.TenantID = tid
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "tenant_id"}, {Name: "channel_account_id"}, {Name: "external_conversation_id"},
		},
		DoUpdates: clause.AssignmentColumns([]string{"member_roles", "messages", "tags", "summary", "updated_at"}),
	}).Create(m).Error
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
