package repository

import (
	"context"
	"errors"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// OutboundDeliveryPageSize bounds the number of candidates locked in one transaction.
const OutboundDeliveryPageSize = 64

type OutboundDeliveryPage struct {
	Platform, AccountID  string
	ID, AfterID          uint64
	DueBefore, AgeBefore time.Time
	Reclaim              bool
}

// WithOutboundDeliveryPage locks one ordered tenant page. The legacy decision
// callback receives only the locked message rows.
func (r *MessageHubRepository) WithOutboundDeliveryPage(ctx context.Context, page OutboundDeliveryPage, decide func([]model.MessageHub) []uint, apply func(*MessageHubRepository) error) (uint64, error) {
	return r.WithOutboundCapabilityPage(ctx, page, func(rows []model.MessageHub, _ CapabilityEvidenceRepository) ([]uint, error) {
		return decide(rows), nil
	}, apply)
}

// WithOutboundCapabilityPage also exposes local authorization reads on the same
// transaction. A decision error rolls back the page before any queue mutation.
// Apply remains confined to the approved IDs in this locked page.
func (r *MessageHubRepository) WithOutboundCapabilityPage(ctx context.Context, page OutboundDeliveryPage, decide func([]model.MessageHub, CapabilityEvidenceRepository) ([]uint, error), apply func(*MessageHubRepository) error) (uint64, error) {
	if r == nil || r.db == nil {
		return 0, ErrDBNotReady
	}
	tenantID, ok := tenant.FromContext(ctx)
	if !ok || tenant.IsSystemScope(ctx) {
		return 0, errors.New("outbound delivery requires tenant scope")
	}
	var next uint64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		q := tx.Where("tenant_id = ? AND direction = 'outbound' AND status IN ('pending', 'inflight')", tenantID).
			Where(outboundDeliveryPredicate(""))
		if page.ID != 0 {
			q = q.Where("id = ?", page.ID)
		}
		if page.Platform != "" || page.AccountID != "" {
			q = q.Where("platform = ? AND account_id = ?", page.Platform, page.AccountID)
		}
		if page.AfterID != 0 {
			q = q.Where("id > ?", page.AfterID)
		}
		if page.Reclaim {
			due := "(status = 'pending' AND push_attempts >= ?) OR (status = 'inflight' AND claimed_at < ?)"
			args := []any{MaxOutboundPushAttempts, page.DueBefore}
			if !page.AgeBefore.IsZero() {
				due += " OR (created_at < ? AND push_attempts < ?)"
				args = append(args, page.AgeBefore, MaxOutboundPushAttempts)
			}
			q = q.Where("("+due+")", args...)
		} else if !page.DueBefore.IsZero() {
			q = q.Where("(status = 'pending' OR (status = 'inflight' AND claimed_at < ?))", page.DueBefore)
		}
		var rows []model.MessageHub
		if err := q.Order("id ASC").Limit(OutboundDeliveryPageSize).Clauses(clause.Locking{Strength: "UPDATE"}).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		if len(rows) == OutboundDeliveryPageSize {
			next = uint64(rows[len(rows)-1].ID)
		}
		locked := make(map[uint]struct{}, len(rows))
		for i := range rows {
			locked[rows[i].ID] = struct{}{}
		}
		approved, err := decide(rows, &capabilityEvidenceRepository{db: tx, lock: true})
		if err != nil {
			return err
		}
		var ids []uint
		for _, id := range approved {
			if _, ok := locked[id]; !ok {
				return errors.New("outbound delivery ID outside locked page")
			}
			ids = append(ids, id)
		}
		if len(ids) == 0 || apply == nil {
			return nil
		}
		return apply(&MessageHubRepository{db: tx.Where("id IN ? AND tenant_id = ?", ids, tenantID), deliveryPage: true})
	})
	return next, err
}
