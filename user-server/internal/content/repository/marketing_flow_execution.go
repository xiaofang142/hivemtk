package repository

import (
	"context"
	"errors"
	"time"

	"hivemtk-user/internal/content/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrFlowExecutionFence = errors.New("marketing flow execution fence rejected")

func init() { db.RegisterExtraModels(&model.FlowNodeAttempt{}) }

func (r *FlowExecutionRepository) executionDB(ctx context.Context) (*gorm.DB, uint, error) {
	id, ok := tenant.FromContext(ctx)
	if !ok || id == 0 || tenant.IsSystemScope(ctx) || r == nil || r.db == nil {
		return nil, 0, ErrFlowExecutionFence
	}
	return r.db.WithContext(tenant.OnlyTenant(ctx, id)), id, nil
}

func (r *FlowExecutionRepository) CreateDurable(ctx context.Context, row *model.FlowExecution) (bool, error) {
	database, id, err := r.executionDB(ctx)
	if err != nil {
		return false, err
	}
	if row == nil || row.TriggerKey == nil || row.FlowID == 0 || (row.TenantID != 0 && row.TenantID != id) {
		return false, ErrFlowExecutionFence
	}
	row.TenantID = id
	created := false
	err = database.Transaction(func(tx *gorm.DB) error {
		var flow model.MarketingFlow
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id = ?", id, row.FlowID).Take(&flow).Error; err != nil {
			return err
		}
		if flow.Status != model.FlowStatusActive || flow.Version != row.FlowVersion || flow.FlowData != row.GraphSnapshot {
			return ErrFlowExecutionFence
		}
		result := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "tenant_id"}, {Name: "trigger_key"}}, DoNothing: true}).Create(row)
		if result.Error != nil {
			return result.Error
		}
		created = result.RowsAffected == 1
		if !created {
			return tx.Where("tenant_id = ? AND trigger_key = ?", id, *row.TriggerKey).Take(row).Error
		}
		return nil
	})
	return created, err
}

func (r *FlowExecutionRepository) ClaimDurable(ctx context.Context, executionID uint, token string, now time.Time) (*model.FlowExecution, error) {
	database, id, err := r.executionDB(ctx)
	if err != nil {
		return nil, err
	}
	var row model.FlowExecution
	err = database.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id = ?", id, executionID).Take(&row).Error; err != nil {
			return err
		}
		var flow model.MarketingFlow
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id = ?", id, row.FlowID).Take(&flow).Error; err != nil {
			return err
		}
		if flow.Status != model.FlowStatusActive || row.GraphSnapshot == "" || (row.Status != "running" && row.Status != "waiting" && row.Status != "paused") || (row.DueAt != nil && row.DueAt.After(now)) || (row.LeaseUntil != nil && row.LeaseUntil.After(now)) {
			return ErrFlowExecutionFence
		}
		until := now.Add(30 * time.Second)
		row.LeaseToken, row.LeaseUntil, row.Status = token, &until, "running"
		return tx.Model(&model.FlowExecution{}).Where("tenant_id = ? AND id = ?", id, row.ID).Updates(map[string]any{"lease_token": token, "lease_until": until, "status": row.Status}).Error
	})
	return &row, err
}

func (r *FlowExecutionRepository) BeginDurableNode(ctx context.Context, executionID uint, token, nodeID string, dueAt *time.Time, now time.Time) (model.FlowNodeAttempt, bool, error) {
	database, id, err := r.executionDB(ctx)
	if err != nil {
		return model.FlowNodeAttempt{}, false, err
	}
	var attempt model.FlowNodeAttempt
	fresh := false
	err = database.Transaction(func(tx *gorm.DB) error {
		var row model.FlowExecution
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id = ?", id, executionID).Take(&row).Error; err != nil {
			return err
		}
		if row.Status != "running" || row.LeaseToken != token || row.LeaseUntil == nil || !row.LeaseUntil.After(now) {
			return ErrFlowExecutionFence
		}
		var flow model.MarketingFlow
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id = ?", id, row.FlowID).Take(&flow).Error; err != nil {
			return err
		}
		if flow.Status != model.FlowStatusActive {
			return ErrFlowExecutionFence
		}
		err := tx.Where("tenant_id = ? AND execution_id = ? AND node_id = ?", id, executionID, nodeID).Take(&attempt).Error
		if err == nil {
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		attempt = model.FlowNodeAttempt{TenantID: id, ExecutionID: executionID, NodeID: nodeID, State: "dispatch_committed", DispatchCommittedAt: &now, DueAt: dueAt}
		if dueAt != nil {
			attempt.State = "waiting"
			attempt.DispatchCommittedAt = nil
		}
		if err := tx.Create(&attempt).Error; err != nil {
			return err
		}
		fresh = true
		until := now.Add(30 * time.Second)
		return tx.Model(&model.FlowExecution{}).Where("tenant_id = ? AND id = ?", id, executionID).Updates(map[string]any{"current_node": nodeID, "lease_until": until}).Error
	})
	return attempt, fresh, err
}

func (r *FlowExecutionRepository) CompleteDurableNode(ctx context.Context, executionID uint, token, nodeID, output string, now time.Time) error {
	database, id, err := r.executionDB(ctx)
	if err != nil {
		return err
	}
	return database.Transaction(func(tx *gorm.DB) error {
		var row model.FlowExecution
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id = ? AND lease_token = ?", id, executionID, token).Take(&row).Error; err != nil {
			return err
		}
		if row.Status != "running" || row.LeaseUntil == nil || !row.LeaseUntil.After(now) {
			return ErrFlowExecutionFence
		}
		res := tx.Model(&model.FlowNodeAttempt{}).Where("tenant_id = ? AND execution_id = ? AND node_id = ? AND state IN ?", id, executionID, nodeID, []string{"dispatch_committed", "waiting"}).Updates(map[string]any{"state": "completed", "output": output, "completed_at": now})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return ErrFlowExecutionFence
		}
		return nil
	})
}

func (r *FlowExecutionRepository) FinishDurable(ctx context.Context, row *model.FlowExecution, token string) error {
	database, id, err := r.executionDB(ctx)
	if err != nil {
		return err
	}
	result := database.Model(&model.FlowExecution{}).Where("tenant_id = ? AND id = ? AND lease_token = ?", id, row.ID, token).Updates(map[string]any{"status": row.Status, "execution_data": row.ExecutionData, "error_message": row.ErrorMessage, "due_at": row.DueAt, "completed_at": row.CompletedAt, "lease_token": "", "lease_until": nil})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrFlowExecutionFence
	}
	return nil
}

func (r *FlowExecutionRepository) DueDurable(ctx context.Context, flowID uint, now time.Time) ([]model.FlowExecution, error) {
	database, id, err := r.executionDB(ctx)
	if err != nil {
		return nil, err
	}
	var rows []model.FlowExecution
	err = database.Where("tenant_id = ? AND flow_id = ? AND graph_snapshot <> '' AND status IN ? AND (due_at IS NULL OR due_at <= ?) AND (lease_until IS NULL OR lease_until <= ?)", id, flowID, []string{"running", "waiting", "paused"}, now, now).Order("id").Limit(10).Find(&rows).Error
	return rows, err
}

// ActiveDueFlows 只返回本租户有持久待办的激活流程，每轮上限10个。
func (r *FlowExecutionRepository) ActiveDueFlows(ctx context.Context, now time.Time) ([]uint, error) {
	database, id, err := r.executionDB(ctx)
	if err != nil {
		return nil, err
	}
	var ids []uint
	due := database.Model(&model.FlowExecution{}).Select("flow_id").Where("tenant_id = ? AND graph_snapshot <> '' AND status IN ? AND (due_at IS NULL OR due_at <= ?) AND (lease_until IS NULL OR lease_until <= ?)", id, []string{"running", "waiting", "paused"}, now, now)
	err = database.Model(&model.MarketingFlow{}).Where("tenant_id = ? AND status = ? AND id IN (?)", id, model.FlowStatusActive, due).Order("id").Limit(10).Pluck("id", &ids).Error
	return ids, err
}
