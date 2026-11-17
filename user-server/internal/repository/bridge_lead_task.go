package repository

import (
	"context"
	"errors"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type BridgeLeadTaskRepository struct{ database *gorm.DB }

func NewBridgeLeadTaskRepository(database *gorm.DB) *BridgeLeadTaskRepository {
	return &BridgeLeadTaskRepository{database: database}
}

func (r *BridgeLeadTaskRepository) Reconcile(ctx context.Context, limit int) (int64, error) {
	id, err := r.tenantID(ctx)
	if err != nil {
		return 0, err
	}
	var inserted int64
	err = r.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		hubs := []model.MessageHub{}
		err := tx.Select("id, tenant_id, platform, account_id, conversation_id").
			Where("tenant_id = ? AND bridge_lead_eligible AND direction = 'inbound'", id).
			Where("account_id <> '' AND account_id <> 'default' AND conversation_id <> ''").
			Where("platform IN ?", []string{"douyin", "tiktok", "kuaishou", "xiaohongshu", "xianyu"}).
			Where("NOT EXISTS (SELECT 1 FROM bridge_lead_tasks AS task WHERE task.tenant_id = message_hub.tenant_id AND task.hub_id = message_hub.id)").
			Order("id ASC").Limit(bridgeLeadBatchLimit(limit)).Find(&hubs).Error
		if err != nil || len(hubs) == 0 {
			return err
		}
		rows := make([]model.BridgeLeadTask, 0, len(hubs))
		for _, hub := range hubs {
			rows = append(rows, model.BridgeLeadTask{TenantID: id, HubID: hub.ID, Channel: hub.Platform,
				AccountID: hub.AccountID, ConversationID: hub.ConversationID, Status: model.BridgeLeadPending})
		}
		result := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "tenant_id"}, {Name: "hub_id"}}, DoNothing: true}).Create(&rows)
		inserted = result.RowsAffected
		return result.Error
	})
	return inserted, err
}
func (r *BridgeLeadTaskRepository) Claim(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]model.BridgeLeadTask, error) {
	id, err := r.tenantID(ctx)
	if err != nil {
		return nil, err
	}
	if lease <= 0 || lease > 2*time.Minute {
		return nil, errors.New("bridge lead lease must be positive and at most two minutes")
	}
	rows := []model.BridgeLeadTask{}
	err = r.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("tenant_id = ? AND status = ?", id, model.BridgeLeadPending).
			Order("id ASC").Limit(bridgeLeadBatchLimit(limit)).Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Find(&rows).Error; err != nil {
			return err
		}
		until := now.Add(lease)
		for i := range rows {
			rows[i].Status = model.BridgeLeadClaimed
			rows[i].LeaseToken = uuid.NewString()
			rows[i].LeaseUntil = &until
			if err := tx.Model(&model.BridgeLeadTask{}).Where("tenant_id = ? AND id = ? AND status = ?", id, rows[i].ID, model.BridgeLeadPending).
				Updates(map[string]any{"status": rows[i].Status, "lease_token": rows[i].LeaseToken, "lease_until": until}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	return rows, err
}
func (r *BridgeLeadTaskRepository) Expire(ctx context.Context, now time.Time) (int64, error) {
	id, err := r.tenantID(ctx)
	if err != nil {
		return 0, err
	}
	var affected int64
	err = r.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		unstarted := tx.Model(&model.BridgeLeadTask{}).
			Where("tenant_id = ? AND status = ? AND lease_until <= ?", id, model.BridgeLeadClaimed, now).
			Updates(map[string]any{"status": model.BridgeLeadPending, "lease_token": "", "lease_until": nil})
		if unstarted.Error != nil {
			return unstarted.Error
		}
		running := tx.Model(&model.BridgeLeadTask{}).
			Where("tenant_id = ? AND status = ? AND lease_until <= ?", id, model.BridgeLeadRunning, now).
			Updates(map[string]any{"status": model.BridgeLeadUnknown, "completed_at": now, "last_error": "execution_lease_expired"})
		affected = unstarted.RowsAffected + running.RowsAffected
		return running.Error
	})
	return affected, err
}
func (r *BridgeLeadTaskRepository) Begin(ctx context.Context, task *model.BridgeLeadTask, now time.Time) (bool, error) {
	q, err := r.fenced(ctx, task, model.BridgeLeadClaimed, now)
	if err != nil {
		return false, err
	}
	result := q.Updates(map[string]any{"status": model.BridgeLeadRunning, "started_at": now})
	return result.RowsAffected == 1, result.Error
}
func (r *BridgeLeadTaskRepository) Finish(ctx context.Context, task *model.BridgeLeadTask, status string, now time.Time, reason string) (bool, error) {
	if status != model.BridgeLeadCompleted && status != model.BridgeLeadUnknown && status != model.BridgeLeadSkipped {
		return false, errors.New("bridge lead completion requires a terminal outcome")
	}
	q, err := r.fenced(ctx, task, model.BridgeLeadRunning, now)
	if err != nil {
		return false, err
	}
	// 只持久化已约定分类，原始异常可能含客户正文或凭证。
	if reason != "" && reason != "scope_unconfirmed" && reason != "execution_failed" && reason != "execution_panicked" && reason != "stale_input" {
		reason = "execution_failed"
	}
	result := q.Updates(map[string]any{"status": status, "completed_at": now, "last_error": reason})
	return result.RowsAffected == 1, result.Error
}
func (r *BridgeLeadTaskRepository) LoadHub(ctx context.Context, task *model.BridgeLeadTask) (*model.MessageHub, error) {
	id, err := r.tenantID(ctx)
	if err != nil {
		return nil, err
	}
	if !validBridgeLeadTask(task, id) {
		return nil, errors.New("bridge lead task requires complete tenant and message scope")
	}
	var hub model.MessageHub
	err = r.database.WithContext(ctx).Where("tenant_id = ? AND id = ? AND platform = ? AND account_id = ? AND conversation_id = ?", id, task.HubID, task.Channel, task.AccountID, task.ConversationID).
		Where("bridge_lead_eligible AND direction = 'inbound'").First(&hub).Error
	if err != nil {
		return nil, err
	}
	return &hub, nil
}

func (r *BridgeLeadTaskRepository) tenantID(ctx context.Context) (uint, error) {
	if r == nil || r.database == nil {
		return 0, gorm.ErrInvalidDB
	}
	id, ok := tenant.FromContext(ctx)
	if !ok || id == 0 || tenant.IsSystemScope(ctx) {
		return 0, errors.New("bridge lead tasks require an explicit tenant, not system scope")
	}
	return id, nil
}

func (r *BridgeLeadTaskRepository) fenced(ctx context.Context, task *model.BridgeLeadTask, status string, now time.Time) (*gorm.DB, error) {
	id, err := r.tenantID(ctx)
	if err != nil {
		return nil, err
	}
	if !validBridgeLeadTask(task, id) || task.ID == 0 || task.LeaseToken == "" {
		return nil, errors.New("bridge lead transition requires a valid lease and scope")
	}
	return r.database.WithContext(ctx).Model(&model.BridgeLeadTask{}).
		Where("tenant_id = ? AND id = ? AND hub_id = ? AND channel = ? AND account_id = ? AND conversation_id = ?", id, task.ID, task.HubID, task.Channel, task.AccountID, task.ConversationID).
		Where("status = ? AND lease_token = ? AND lease_until > ?", status, task.LeaseToken, now), nil
}

func validBridgeLeadTask(task *model.BridgeLeadTask, tenantID uint) bool {
	return task != nil && task.TenantID == tenantID && task.HubID != 0 && task.Channel != "" && task.AccountID != "" && task.ConversationID != ""
}

func bridgeLeadBatchLimit(limit int) int {
	if limit <= 0 || limit > 100 {
		return 100
	}
	return limit
}
