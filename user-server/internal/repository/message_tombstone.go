package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"
)

var ErrMessageTombstoneScope = errors.New("消息墓碑租户范围无效")

// MessageTombstoneRepository 编辑/撤回/脱敏墓碑的租户范围持久化口。
type MessageTombstoneRepository struct {
	db *gorm.DB
}

func NewMessageTombstoneRepository() *MessageTombstoneRepository {
	return &MessageTombstoneRepository{db: _db.GetDB()}
}

func NewMessageTombstoneRepositoryWithDB(db *gorm.DB) *MessageTombstoneRepository {
	return &MessageTombstoneRepository{db: db}
}

func (r *MessageTombstoneRepository) database(ctx context.Context) (*gorm.DB, error) {
	if tx, ok := ctx.Value(txContextKey{}).(*gorm.DB); ok && tx != nil {
		return tx.WithContext(ctx), nil
	}
	if r == nil || r.db == nil {
		return nil, ErrChannelStoreUnavailable
	}
	return r.db.WithContext(ctx), nil
}

// Upsert 以 (tenant, platform, account, message, type) 唯一键幂等写入。
func (r *MessageTombstoneRepository) Upsert(ctx context.Context, item *model.MessageTombstone) error {
	database, err := r.database(ctx)
	if err != nil {
		return err
	}
	tid, ok := tenant.FromContext(ctx)
	if !ok || tid == 0 || tid != item.TenantID {
		return ErrMessageTombstoneScope
	}
	if item.MessageID == "" || item.SourceCursor == "" {
		return ErrChannelStoreUnavailable
	}
	return database.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "tenant_id"}, {Name: "platform"}, {Name: "channel_account_id"},
			{Name: "message_id"}, {Name: "tombstone_type"},
		},
		DoUpdates: clause.AssignmentColumns([]string{"effective_at", "source_cursor"}),
	}).Create(item).Error
}
