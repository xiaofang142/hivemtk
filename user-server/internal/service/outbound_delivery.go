package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"hivemtk-user/internal/capregistry"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/repository"
)

// OutboundDeliveryRepository 是 SSE 装配复用的原生队列仓储口；授权策略只在 service。
type OutboundDeliveryRepository interface {
	WithOutboundDeliveryPage(context.Context, repository.OutboundDeliveryPage, func([]model.MessageHub) []uint, func(*repository.MessageHubRepository) error) (uint64, error)
}

type outboundCapabilityPageRepository interface {
	WithOutboundCapabilityPage(context.Context, repository.OutboundDeliveryPage, func([]model.MessageHub, repository.CapabilityEvidenceRepository) ([]uint, error), func(*repository.MessageHubRepository) error) (uint64, error)
}

// OutboundDeliveryService 每次消费重新核对当前精确格，不信任入队时许可或事件载荷中的类型。
// 没有可信加载源时上下文缺证据即阻塞；不写 paused 以免抹去历史状态/租约/预算。
type OutboundDeliveryService struct {
	repo       OutboundDeliveryRepository
	verifier   capregistry.CapabilityProofVerifier
	authorizer BridgeCapabilityAuthorizer
}

func NewOutboundDeliveryService(repo OutboundDeliveryRepository, authorizers ...BridgeCapabilityAuthorizer) *OutboundDeliveryService {
	svc := &OutboundDeliveryService{repo: repo}
	if len(authorizers) == 1 {
		svc.authorizer = authorizers[0]
	}
	return svc
}

func outboundDeliveryScope(ctx context.Context) error {
	if _, ok := tenant.FromContext(ctx); !ok || tenant.IsSystemScope(ctx) {
		err := &capregistry.SubmissionDeniedError{ReasonCode: "tenant_scope_invalid"}
		logCapabilityDenial(ctx, "bridge_delivery", "", err)
		return err
	}
	return nil
}

// A new row may leave TenantID unset for repository persistence to bind.
func (s *InboxIngressService) authorizeNewOutboundRow(ctx context.Context, row *model.MessageHub) error {
	if err := outboundDeliveryScope(ctx); err != nil {
		return err
	}
	tenantID, _ := tenant.FromContext(ctx)
	if row.TenantID != 0 && row.TenantID != tenantID {
		err := &capregistry.SubmissionDeniedError{ReasonCode: "tenant_binding_mismatch"}
		logCapabilityDenial(ctx, "bridge_delivery", row.Platform, err)
		return err
	}
	return authorizeBridgeForIngress(ctx, s, row.Platform, row.AccountID, row.MsgType)
}

// The transaction reader reuses the page connection and locks the current authorization rows.
func (s *OutboundDeliveryService) authorizeRow(ctx context.Context, resolver capregistry.PersistentCapabilityResolver, row *model.MessageHub) error {
	tenantID, _ := tenant.FromContext(ctx)
	if row.TenantID != tenantID {
		return &capregistry.SubmissionDeniedError{ReasonCode: "tenant_binding_mismatch"}
	}
	if s.authorizer != nil {
		return s.authorizer(ctx, row.Platform, row.AccountID, row.MsgType, time.Now().UTC())
	}
	return authorizePersistentBridge(ctx, resolver, row.Platform, row.AccountID, row.MsgType)
}

func isCapabilityStorageUnavailable(err error) bool {
	var denied *capregistry.SubmissionDeniedError
	return errors.As(err, &denied) && denied.ReasonCode == "capability_storage_unavailable"
}

func (s *OutboundDeliveryService) withCapabilityPage(ctx context.Context, page repository.OutboundDeliveryPage, decide func([]model.MessageHub, repository.CapabilityEvidenceRepository) ([]uint, error), apply func(*repository.MessageHubRepository) error) (uint64, error) {
	if store, ok := s.repo.(outboundCapabilityPageRepository); ok {
		return store.WithOutboundCapabilityPage(ctx, page, decide, apply)
	}
	var decisionErr error
	next, err := s.repo.WithOutboundDeliveryPage(ctx, page, func(rows []model.MessageHub) []uint {
		var ids []uint
		ids, decisionErr = decide(rows, nil)
		if decisionErr != nil {
			return nil
		}
		return ids
	}, apply)
	if err == nil {
		err = decisionErr
	}
	return next, err
}

// walkPages records the first denial and advances by the last candidate ID, even
// when a full page has no approved IDs. publish runs only after a page commits.
func (s *OutboundDeliveryService) walkPages(ctx context.Context, page repository.OutboundDeliveryPage, cursorOnly bool,
	apply func(*repository.MessageHubRepository, []model.MessageHub) (publish func(), err error),
	done func() bool,
) error {
	var firstDenial error
	var deniedPlatform string
	defer func() { logCapabilityDenial(ctx, "bridge_delivery", deniedPlatform, firstDenial) }()
	var approvedAny bool
	for {
		var approved []model.MessageHub
		var publish func()
		next, err := s.withCapabilityPage(ctx, page, func(rows []model.MessageHub, reader repository.CapabilityEvidenceRepository) ([]uint, error) {
			resolver := capregistry.PersistentCapabilityResolver{Reader: reader, Verifier: s.verifier}
			ids := make([]uint, 0, len(rows))
			for i := range rows {
				if err := s.authorizeRow(ctx, resolver, &rows[i]); err != nil {
					// A failed SQL read can abort the page transaction. Return before applying any claims.
					if isCapabilityStorageUnavailable(err) {
						firstDenial, deniedPlatform = err, rows[i].Platform
						return nil, err
					}
					if firstDenial == nil {
						firstDenial, deniedPlatform = err, rows[i].Platform
					}
					if cursorOnly {
						break
					}
					continue
				}
				ids = append(ids, rows[i].ID)
				approved = append(approved, rows[i])
			}
			return ids, nil
		}, func(repo *repository.MessageHubRepository) error {
			var err error
			publish, err = apply(repo, approved)
			return err
		})
		if err != nil {
			return fmt.Errorf("process outbound delivery page: %w", err)
		}
		if publish != nil {
			publish()
		}
		approvedAny = approvedAny || len(approved) > 0
		if next == 0 || done() || (cursorOnly && firstDenial != nil) {
			break
		}
		page.AfterID = next
	}
	if !approvedAny {
		return firstDenial
	}
	return nil
}

func (s *OutboundDeliveryService) ClaimOutboundForPush(ctx context.Context, id uint64, channel, accountID, msgType string, timeout time.Duration) (bool, error) {
	if err := outboundDeliveryScope(ctx); err != nil {
		return false, err
	}
	if id == 0 {
		return false, nil
	}
	var claimed bool
	var denial error
	_, err := s.withCapabilityPage(ctx, repository.OutboundDeliveryPage{ID: id}, func(rows []model.MessageHub, reader repository.CapabilityEvidenceRepository) ([]uint, error) {
		row := &rows[0]
		if row.Platform != channel || row.AccountID != accountID || row.MsgType != msgType {
			denial = &capregistry.SubmissionDeniedError{ReasonCode: "delivery_binding_mismatch"}
			return nil, nil
		}
		denial = s.authorizeRow(ctx, capregistry.PersistentCapabilityResolver{Reader: reader, Verifier: s.verifier}, row)
		if isCapabilityStorageUnavailable(denial) {
			return nil, denial
		}
		if denial != nil {
			return nil, nil
		}
		return []uint{row.ID}, nil
	}, func(repo *repository.MessageHubRepository) error {
		var err error
		claimed, err = repo.ClaimOutboundForPush(ctx, id, timeout)
		return err
	})
	logCapabilityDenial(ctx, "bridge_delivery", channel, denial)
	if err != nil {
		return false, fmt.Errorf("claim outbound for push: %w", err)
	}
	return claimed, denial
}

// ClaimPendingOutbound may return committed claims together with a later page error.
func (s *OutboundDeliveryService) ClaimPendingOutbound(ctx context.Context, channel, accountID string, timeout time.Duration, limit int) ([]model.MessageHub, error) {
	if err := outboundDeliveryScope(ctx); err != nil {
		return nil, err
	}
	if limit <= 0 || channel == "" || accountID == "" {
		return nil, nil
	}
	page := repository.OutboundDeliveryPage{Platform: channel, AccountID: accountID, DueBefore: time.Now().Add(-timeout)}
	var rows []model.MessageHub
	err := s.walkPages(ctx, page, false, func(repo *repository.MessageHubRepository, _ []model.MessageHub) (func(), error) {
		claims, err := repo.ClaimPendingOutbound(ctx, channel, accountID, limit-len(rows), timeout)
		return func() { rows = append(rows, claims...) }, err
	}, func() bool { return len(rows) >= limit })
	return rows, err
}

func (s *OutboundDeliveryService) FetchOutboundUndelivered(ctx context.Context, channel, accountID string, timeout time.Duration, limit int) ([]model.MessageHub, error) {
	if err := outboundDeliveryScope(ctx); err != nil {
		return nil, err
	}
	if channel == "" || accountID == "" {
		return nil, nil
	}
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	page := repository.OutboundDeliveryPage{Platform: channel, AccountID: accountID, DueBefore: time.Now().Add(-timeout)}
	var rows []model.MessageHub
	err := s.walkPages(ctx, page, false, func(repo *repository.MessageHubRepository, _ []model.MessageHub) (func(), error) {
		owed, err := repo.FetchOutboundUndelivered(ctx, channel, accountID, timeout, limit-len(rows))
		return func() { rows = append(rows, owed...) }, err
	}, func() bool { return len(rows) >= limit })
	return rows, err
}

func (s *OutboundDeliveryService) FetchOutboundSince(ctx context.Context, channel, accountID string, sinceID uint64, limit int) ([]model.MessageHub, error) {
	if err := outboundDeliveryScope(ctx); err != nil {
		return nil, err
	}
	if channel == "" || accountID == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 200
	}
	page := repository.OutboundDeliveryPage{Platform: channel, AccountID: accountID, AfterID: sinceID}
	var rows []model.MessageHub
	err := s.walkPages(ctx, page, true, func(_ *repository.MessageHubRepository, approved []model.MessageHub) (func(), error) {
		return func() { rows = append(rows, approved[:min(len(approved), limit-len(rows))]...) }, nil
	}, func() bool { return len(rows) >= limit })
	return rows, err
}

// ReclaimOutbound 不允许巡检通过系统范围绕过当前授权；拒绝时保留历史租约、预算和状态。
func (s *OutboundDeliveryService) ReclaimOutbound(ctx context.Context, claimTimeout, staleAfter time.Duration) (reclaimed, exhausted, aged int64, err error) {
	if err := outboundDeliveryScope(ctx); err != nil {
		return 0, 0, 0, err
	}
	if claimTimeout <= 0 {
		return 0, 0, 0, nil
	}
	now := time.Now()
	page := repository.OutboundDeliveryPage{DueBefore: now.Add(-claimTimeout), Reclaim: true}
	if staleAfter > 0 {
		page.AgeBefore = now.Add(-staleAfter)
	}
	err = s.walkPages(ctx, page, false, func(repo *repository.MessageHubRepository, _ []model.MessageHub) (func(), error) {
		r, e, a, err := repo.ReclaimOutboundSweep(ctx, claimTimeout, staleAfter)
		return func() { reclaimed += r; exhausted += e; aged += a }, err
	}, func() bool { return false })
	return reclaimed, exhausted, aged, err
}
