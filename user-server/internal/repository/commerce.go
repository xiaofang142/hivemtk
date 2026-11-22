package repository

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	gormlogger "gorm.io/gorm/logger"
	"hivemtk-user/internal/model"
	appdb "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"
)

var ErrCommerceStore = errors.New("commerce storage unavailable")
var ErrCommerceNotFound = errors.New("commerce resource unavailable")
var ErrCommerceConflict = errors.New("commerce concurrent revision")

func init() {
	appdb.RegisterExtraModels(&model.CommerceOperation{}, &model.CommerceApproval{}, &model.CommerceAttempt{}, &model.CommerceResource{}, &model.CommerceOutbox{}, &model.CommercePolicy{}, &model.CommerceMember{}, &model.CommerceEvidence{})
}

type CommerceKey struct {
	ID                                         string
	ActorID                                    uint
	Profile, Product, ShopID, Action, IntentID string
	Revision                                   int64
	OrderID                                    uint
	ResourceID                                 string
	AfterSaleID, OrderChangeID                 uint
	ParentID                                   string
}

// CommerceAggregate is a locked persistence unit; decisions belong to Service.
type CommerceAggregate struct {
	Operation      model.CommerceOperation
	Exists         bool
	Order          model.ExternalOrder
	Binding        model.CapabilitySourceBinding
	Connection     model.MerchantConnection
	Capability     model.CapabilityEvidence
	Policy         model.CommercePolicy
	Member         model.CommerceMember
	Approver       model.CommerceMember
	Actor          model.SystemUser
	ApproverUser   model.SystemUser
	Approval       model.CommerceApproval
	Resource       model.CommerceResource
	AfterSale      model.AfterSale
	OrderChange    model.OrderChangeRequest
	Attempts       []model.CommerceAttempt
	Evidences      []model.CommerceEvidence
	NewApproval    *model.CommerceApproval
	UpdateAttempt  *model.CommerceAttempt
	NewAttempt     *model.CommerceAttempt
	NewEvidence    *model.CommerceEvidence
	UpdateEvidence *model.CommerceEvidence
	Parent         model.CommerceOperation
	Dirty          bool
}
type CommerceRepository interface {
	Transact(ctx context.Context, key CommerceKey, fn func(*CommerceAggregate) error) error
	List(ctx context.Context, actor uint, profile, shop, state string, limit int) ([]model.CommerceOperation, error)
	Due(ctx context.Context, now time.Time, limit int) ([]model.CommerceOperation, error)
	ConfigurePolicy(ctx context.Context, actor uint, expected int64, row model.CommercePolicy) (model.CommercePolicy, error)
	ConfigureMember(ctx context.Context, actor uint, expected int64, row model.CommerceMember) (model.CommerceMember, error)
}
type commerceRepository struct{ db *gorm.DB }

func NewCommerceRepository(db *gorm.DB) CommerceRepository { return &commerceRepository{db: db} }
func (r *commerceRepository) scope(ctx context.Context) (*gorm.DB, uint, error) {
	id, ok := tenant.FromContext(ctx)
	if !ok || tenant.IsSystemScope(ctx) || r.db == nil {
		return nil, 0, ErrCommerceStore
	}
	return r.db.Session(&gorm.Session{Logger: gormlogger.Discard}).WithContext(tenant.OnlyTenant(ctx, id)), id, nil
}
func commerceTake(q *gorm.DB, out any) error {
	e := q.Take(out).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return nil
	}
	return e
}
func commerceScoped(tx *gorm.DB, id uint) *gorm.DB { return tx.Where("tenant_id = ?", id) }
func commerceLocked(tx *gorm.DB, id uint) *gorm.DB {
	return commerceScoped(tx, id).Clauses(clause.Locking{Strength: "UPDATE"})
}
func (r *commerceRepository) Transact(ctx context.Context, key CommerceKey, fn func(*CommerceAggregate) error) error {
	db, tid, err := r.scope(ctx)
	if err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		a := CommerceAggregate{}
		// Resolve routing without locking, then acquire the shop policy first for every transition.
		if key.ID != "" {
			if e := commerceTake(commerceScoped(tx, tid).Where("id = ?", key.ID), &a.Operation); e != nil {
				return e
			}
			if a.Operation.ID == "" {
				return ErrCommerceNotFound
			}
			key.Profile, key.ShopID, key.OrderID, key.Product = a.Operation.Profile, a.Operation.ShopID, a.Operation.OrderID, a.Operation.Product
		}
		if e := commerceTake(commerceLocked(tx, tid).Where("profile = ? AND shop_id = ?", key.Profile, key.ShopID), &a.Policy); e != nil {
			return e
		}
		if e := commerceTake(commerceLocked(tx, tid).Select("id", "tenant_id", "role", "enabled", "status").Where("id = ?", key.ActorID), &a.Actor); e != nil {
			return e
		}
		if e := commerceTake(commerceLocked(tx, tid).Where("profile = ? AND shop_id = ? AND actor_id = ?", key.Profile, key.ShopID, key.ActorID), &a.Member); e != nil {
			return e
		}
		if e := commerceTake(commerceLocked(tx, tid).Select("id", "tenant_id", "provider", "shop_id", "version", "credential_version", "consent_version", "credential_contract", "scopes", "expires_at", "revoked", "refresh_until", "refresh_failed", "attempt_expires_at", "attempt_failed").Where("provider = ? AND shop_id = ?", key.Product, key.ShopID), &a.Connection); e != nil {
			return e
		}
		if key.ID != "" {
			a.Operation = model.CommerceOperation{}
			if e := commerceTake(commerceLocked(tx, tid).Where("id = ?", key.ID), &a.Operation); e != nil {
				return e
			}
		} else if key.IntentID != "" {
			if e := commerceTake(commerceLocked(tx, tid).Where("profile = ? AND shop_id = ? AND action = ? AND intent_id = ? AND revision = ?", key.Profile, key.ShopID, key.Action, key.IntentID, key.Revision), &a.Operation); e != nil {
				return e
			}
		}
		a.Exists = a.Operation.ID != ""
		if a.Exists {
			key.OrderID = a.Operation.OrderID
			key.Action = a.Operation.Action
		}
		if e := commerceTake(commerceScoped(tx, tid).Where("id = ?", key.OrderID), &a.Order); e != nil {
			return e
		}
		if a.Order.ID != 0 {
			if e := commerceTake(commerceScoped(tx, tid).Where("product = ? AND shop_id = ? AND resource_id = ? AND source = ?", key.Product, key.ShopID, a.Order.OrderID, "order"), &a.Binding); e != nil {
				return e
			}
		}
		if key.Action != "" {
			if e := commerceTake(commerceScoped(tx, tid).Where("product = ? AND shop_id = ? AND action = ?", key.Product, key.ShopID, key.Action), &a.Capability); e != nil {
				return e
			}
		}
		if a.Exists {
			if e := commerceTake(commerceScoped(tx, tid).Where("id = ?", a.Operation.ApprovalID), &a.Approval); e != nil {
				return e
			}
			if a.Approval.ActorID != 0 {
				if e := commerceTake(commerceLocked(tx, tid).Where("profile = ? AND shop_id = ? AND actor_id = ?", key.Profile, key.ShopID, a.Approval.ActorID), &a.Approver); e != nil {
					return e
				}
				if e := commerceTake(commerceLocked(tx, tid).Select("id", "tenant_id", "role", "enabled", "status").Where("id = ?", a.Approval.ActorID), &a.ApproverUser); e != nil {
					return e
				}
			}
			if e := commerceScoped(tx, tid).Where("operation_id = ?", a.Operation.ID).Order("created_at").Find(&a.Attempts).Error; e != nil {
				return e
			}
			if e := commerceScoped(tx, tid).Where("operation_id = ?", a.Operation.ID).Order("created_at").Find(&a.Evidences).Error; e != nil {
				return e
			}
			if a.Operation.AfterSaleID != 0 {
				if e := commerceTake(commerceScoped(tx, tid).Where("id = ?", a.Operation.AfterSaleID), &a.AfterSale); e != nil {
					return e
				}
			}
			if a.Operation.OrderChangeID != 0 {
				if e := commerceTake(commerceScoped(tx, tid).Where("id = ?", a.Operation.OrderChangeID), &a.OrderChange); e != nil {
					return e
				}
			}
		}
		if !a.Exists {
			if key.AfterSaleID != 0 {
				if e := commerceTake(commerceScoped(tx, tid).Where("id = ?", key.AfterSaleID), &a.AfterSale); e != nil {
					return e
				}
			}
			if key.OrderChangeID != 0 {
				if e := commerceTake(commerceScoped(tx, tid).Where("id = ?", key.OrderChangeID), &a.OrderChange); e != nil {
					return e
				}
			}
		}
		if a.Exists {
			key.ParentID = a.Operation.ParentID
		}
		if key.ParentID != "" {
			if e := commerceTake(commerceScoped(tx, tid).Where("id = ?", key.ParentID), &a.Parent); e != nil {
				return e
			}
		}
		key.ResourceID = fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%d/%s/%s/%d", tid, key.Product, key.ShopID, key.OrderID))))
		if key.ResourceID != "" {
			if e := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.CommerceResource{ID: key.ResourceID, TenantID: tid, Profile: key.Profile, ShopID: key.ShopID, OrderID: key.OrderID}).Error; e != nil {
				return e
			}
			if e := commerceTake(commerceLocked(tx, tid).Where("id = ?", key.ResourceID), &a.Resource); e != nil {
				return e
			}
		}
		if e := fn(&a); e != nil {
			return e
		}
		if !a.Dirty {
			return nil
		}
		a.Operation.TenantID = tid
		if a.Exists {
			result := commerceScoped(tx, tid).Model(&model.CommerceOperation{}).Where("id = ? AND version = ?", a.Operation.ID, a.Operation.Version-1).Select("*").Updates(&a.Operation)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrCommerceConflict
			}
		} else {
			if e := tx.Create(&a.Operation).Error; e != nil {
				return e
			}
		}
		if a.Policy.ID != "" {
			if e := commerceScoped(tx, tid).Model(&model.CommercePolicy{}).Where("id = ?", a.Policy.ID).Updates(map[string]any{"network_lease": a.Policy.NetworkLease, "network_until": a.Policy.NetworkUntil}).Error; e != nil {
				return e
			}
		}
		if a.Resource.ID != "" {
			a.Resource.TenantID = tid
			if e := commerceScoped(tx, tid).Save(&a.Resource).Error; e != nil {
				return e
			}
		}
		if a.NewApproval != nil {
			a.NewApproval.TenantID = tid
			if e := tx.Create(a.NewApproval).Error; e != nil {
				return e
			}
		}
		if a.NewAttempt != nil {
			a.NewAttempt.TenantID = tid
			if e := tx.Create(a.NewAttempt).Error; e != nil {
				return e
			}
		}
		if a.UpdateAttempt != nil {
			if e := commerceScoped(tx, tid).Model(&model.CommerceAttempt{}).Where("id = ? AND fence = ?", a.UpdateAttempt.ID, a.UpdateAttempt.Fence).Updates(map[string]any{"state": a.UpdateAttempt.State, "reason": a.UpdateAttempt.Reason, "external_ref": a.UpdateAttempt.ExternalRef, "finished_at": a.UpdateAttempt.FinishedAt}).Error; e != nil {
				return e
			}
		}
		if a.NewEvidence != nil {
			a.NewEvidence.TenantID = tid
			if e := tx.Create(a.NewEvidence).Error; e != nil {
				return e
			}
		}
		if a.UpdateEvidence != nil {
			if e := commerceScoped(tx, tid).Model(&model.CommerceEvidence{}).Where("id = ? AND reviewer_id = 0", a.UpdateEvidence.ID).Updates(map[string]any{"reviewer_id": a.UpdateEvidence.ReviewerID, "reviewed_at": a.UpdateEvidence.ReviewedAt}).Error; e != nil {
				return e
			}
		}
		event := model.CommerceOutbox{ID: uuid.NewString(), TenantID: tid, OperationID: a.Operation.ID, Version: a.Operation.Version, Kind: "commerce.operation.changed", SchemaVersion: 1, TraceID: a.Operation.IntentID, State: "pending", DueAt: a.Operation.DueAt}
		return tx.Create(&event).Error
	})
}
func (r *commerceRepository) List(ctx context.Context, actor uint, profile, shop, state string, limit int) ([]model.CommerceOperation, error) {
	db, tid, e := r.scope(ctx)
	if e != nil {
		return nil, e
	}
	var member model.CommerceMember
	if e = commerceTake(commerceScoped(db, tid).Where("profile = ? AND shop_id = ? AND actor_id = ?", profile, shop, actor), &member); e != nil {
		return nil, e
	}
	if !member.CanRead {
		return nil, ErrCommerceNotFound
	}
	q := commerceScoped(db, tid).Where("profile = ? AND shop_id = ?", profile, shop)
	if state != "" {
		q = q.Where("state = ?", state)
	}
	var rows []model.CommerceOperation
	e = q.Order("created_at DESC").Limit(limit).Find(&rows).Error
	return rows, e
}
func (r *commerceRepository) Due(ctx context.Context, now time.Time, limit int) ([]model.CommerceOperation, error) {
	db, tid, e := r.scope(ctx)
	if e != nil {
		return nil, e
	}
	var rows []model.CommerceOperation
	e = commerceScoped(db, tid).Where("state IN ? AND due_at <= ? AND (lease_until IS NULL OR lease_until < ?)", []string{"queued", "executing", "accepted", "processing", "awaiting_customer", "outcome_unknown", "reconciling", "manual_review"}, now, now).Order("due_at").Limit(limit).Find(&rows).Error
	return rows, e
}
func commerceAdmin(tx *gorm.DB, tid, actor uint) error {
	var u model.SystemUser
	if e := commerceTake(commerceScoped(tx, tid).Select("id", "role", "enabled", "status").Where("id = ?", actor), &u); e != nil {
		return e
	}
	if u.ID == 0 || u.Role != "admin" || !u.Enabled || u.Status != 1 {
		return ErrCommerceNotFound
	}
	return nil
}
func (r *commerceRepository) ConfigurePolicy(ctx context.Context, actor uint, expected int64, row model.CommercePolicy) (model.CommercePolicy, error) {
	db, tid, e := r.scope(ctx)
	if e != nil {
		return row, e
	}
	e = db.Transaction(func(tx *gorm.DB) error {
		if e := commerceAdmin(tx, tid, actor); e != nil {
			return e
		}
		var old model.CommercePolicy
		if e := commerceTake(commerceLocked(tx, tid).Where("id = ?", row.ID), &old); e != nil {
			return e
		}
		if old.Version != expected {
			return ErrCommerceConflict
		}
		row.TenantID = tid
		row.Version = expected + 1
		return tx.Save(&row).Error
	})
	return row, e
}
func (r *commerceRepository) ConfigureMember(ctx context.Context, actor uint, expected int64, row model.CommerceMember) (model.CommerceMember, error) {
	db, tid, e := r.scope(ctx)
	if e != nil {
		return row, e
	}
	e = db.Transaction(func(tx *gorm.DB) error {
		if e := commerceAdmin(tx, tid, actor); e != nil {
			return e
		}
		var p model.CommercePolicy
		if e := commerceTake(commerceLocked(tx, tid).Where("profile = ? AND shop_id = ?", row.Profile, row.ShopID), &p); e != nil {
			return e
		}
		if p.ID == "" {
			return ErrCommerceNotFound
		}
		var u model.SystemUser
		if e := commerceTake(commerceScoped(tx, tid).Select("id").Where("id = ?", row.ActorID), &u); e != nil {
			return e
		}
		if u.ID == 0 {
			return ErrCommerceNotFound
		}
		var old model.CommerceMember
		if e := commerceTake(commerceLocked(tx, tid).Where("id = ?", row.ID), &old); e != nil {
			return e
		}
		if old.Version != expected {
			return ErrCommerceConflict
		}
		row.TenantID = tid
		row.Version = expected + 1
		return tx.Save(&row).Error
	})
	return row, e
}
