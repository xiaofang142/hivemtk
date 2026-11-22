package service

import (
	"context"
	"github.com/google/uuid"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
	"slices"
	"time"
)

// execute commits a bounded dispatch lease before any network call. Recovery always observes.
func (s *commerceService) execute(ctx context.Context, actor uint, id string, observeOnly bool) (dto.CommerceOperationResponse, error) {
	var out dto.CommerceOperationResponse
	var req CommerceAdapterRequest
	var p dto.CommerceProfileResponse
	var dispatch model.CommerceOperation
	var lease string
	var submit bool
	e := s.repo.Transact(ctx, repository.CommerceKey{ID: id, ActorID: actor}, func(a *repository.CommerceAggregate) error {
		p, _ = commerceProfile(a.Operation.Profile)
		if e := commerceReadGuard(a, p); e != nil {
			return e
		}
		o := &a.Operation
		if slices.Contains([]string{"succeeded", "failed", "cancelled", "expired"}, o.State) {
			out = commerceOperationDTO(*o, a.Attempts, a.Approval)
			return nil
		}
		if !slices.Contains([]string{"queued", "executing", "accepted", "processing", "awaiting_customer", "outcome_unknown", "reconciling", "manual_review"}, o.State) {
			return ErrCommerceConflict
		}
		if o.Mode == commerceWorkbench && (!observeOnly || s.adapters[p.ID] == nil) {
			out = commerceOperationDTO(*o, a.Attempts, a.Approval)
			return nil
		}
		if o.LeaseUntil != nil && o.LeaseUntil.After(s.now()) {
			return ErrCommerceBusy
		}
		provider := s.adapters[p.ID]
		if provider == nil {
			return ErrCommerceProvider
		}
		submit = !observeOnly && o.State == "queued" && o.DispatchedAt == nil && o.Mode != commerceManaged
		// Once dispatch was committed, missing acknowledgement never permits another submit.
		if submit {
			if e := commerceWriteGuard(a, s.now()); e != nil {
				return e
			}
			if e := commerceApprovalGuard(a, s.now()); e != nil {
				return e
			}
			if a.Resource.ActiveOperation != o.ID {
				return ErrCommerceBusy
			}
		}
		if submit && a.Policy.NetworkUntil != nil && a.Policy.NetworkUntil.After(s.now()) {
			return ErrCommerceBusy
		}
		lease = uuid.NewString()
		until := s.now().Add(60 * time.Second)
		if submit {
			a.Policy.NetworkLease = lease
			a.Policy.NetworkUntil = &until
		}
		o.LeaseOwner = lease
		o.LeaseUntil = &until
		o.Fence++
		o.State = "reconciling"
		kind := "observe"
		if submit {
			o.State = "executing"
			kind = "submit"
		}
		req = commerceRequest(a, p, commerceParams(*o))
		dispatch = *o
		// This attempt denotes a committed network dispatch permission; a crash here is unknown.
		now := s.now()
		if submit {
			o.DispatchedAt = &now
		}
		if submit {
			a.Operation.UnknownSince = &now
			a.Operation.FailureReason = "dispatch_committed_result_pending"
		}
		a.NewAttempt = &model.CommerceAttempt{ID: uuid.NewString(), OperationID: o.ID, Kind: kind, Fence: o.Fence, DispatchCommitted: true, State: "dispatch_committed"}
		commerceMutate(a)
		dispatch = *o
		return nil
	})
	if e != nil || lease == "" {
		return out, e
	}
	provider := s.adapters[p.ID]
	callctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	var result CommerceAdapterResult
	var callErr error
	if submit {
		// The last remote read is pure and checks the authoritative version and refundable balance.
		var current CommerceSnapshot
		current, callErr = provider.Read(callctx, req)
		frozen := commerceSnapshot(dispatch)
		freshFor := 30 * time.Second
		if dispatch.AmountMinor > 0 {
			freshFor = 15 * time.Second
		}
		if callErr == nil && (current.Version != frozen.Version || current.Source == "" || s.now().Sub(current.FetchedAt) > freshFor || current.FetchedAt.After(s.now().Add(time.Second)) || !slices.Contains(current.AllowedActions, dispatch.Action) || dispatch.AmountMinor > current.RefundableMinor || (dispatch.AmountMinor > 0 && dispatch.Currency != current.Currency) || dispatch.Quantity > current.AvailableQuantity) {
			callErr = ErrCommerceConflict
		}
		if callErr != nil {
			result = CommerceAdapterResult{DefinitelyNotDispatched: true, Reason: "pre_dispatch_read_conflict"}
		} else {
			// Revalidate every local guard after the external read and immediately before Submit.
			callErr = s.repo.Transact(ctx, repository.CommerceKey{ID: id, ActorID: actor}, func(a *repository.CommerceAggregate) error {
				if a.Operation.LeaseOwner != lease || a.Operation.Fence != dispatch.Fence {
					return ErrCommerceConflict
				}
				if e := commerceWriteGuard(a, s.now()); e != nil {
					return e
				}
				return commerceApprovalGuard(a, s.now())
			})
			if callErr != nil {
				result = CommerceAdapterResult{DefinitelyNotDispatched: true, Reason: "dispatch_permission_changed"}
			} else {
				result, callErr = provider.Submit(callctx, req)
			}
		}
	} else {
		result, callErr = provider.Observe(callctx, req)
	}
	// Use a detached but tenant-preserving cleanup context so shutdown cancellation cannot silently lose results.
	persistctx, persistcancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer persistcancel()
	e = s.repo.Transact(persistctx, repository.CommerceKey{ID: id, ActorID: actor}, func(a *repository.CommerceAggregate) error {
		o := &a.Operation
		if o.LeaseOwner != lease || o.Fence != dispatch.Fence {
			return ErrCommerceConflict
		}
		if a.Policy.NetworkLease == lease {
			a.Policy.NetworkLease = ""
			a.Policy.NetworkUntil = nil
		}
		o.LeaseOwner = ""
		o.LeaseUntil = nil
		o.NativeState = result.NativeState
		if result.ExternalRef != "" {
			o.ExternalRef = result.ExternalRef
		}
		if callErr != nil {
			if result.DefinitelyNotDispatched && submit {
				o.State = "awaiting_approval"
				o.DispatchedAt = nil
				o.ApprovalID = ""
				o.PreflightHash = ""
				o.FailureReason = result.Reason
				commerceRelease(a)
			} else {
				commerceUnknown(o, s.now(), "transport_or_observation_unknown")
			}
		} else {
			s.applyResult(a, result, submit)
		}
		if o.State == "awaiting_customer" || o.State == "processing" || o.State == "accepted" {
			o.DueAt = s.now().Add(15 * time.Second)
		}
		if o.UnknownSince != nil && s.now().Sub(*o.UnknownSince) >= 15*time.Minute {
			o.State = "manual_review"
			o.FailureReason = "unknown_escalated_to_owner"
		}
		for i := range a.Attempts {
			if a.Attempts[i].Fence == dispatch.Fence {
				done := s.now()
				a.Attempts[i].State = o.State
				a.Attempts[i].Reason = o.FailureReason
				a.Attempts[i].ExternalRef = o.ExternalRef
				a.Attempts[i].FinishedAt = &done
				a.UpdateAttempt = &a.Attempts[i]
			}
		}
		commerceMutate(a)
		out = commerceOperationDTO(*o, a.Attempts, a.Approval)
		return nil
	})
	return out, e
}
func commerceUnknown(o *model.CommerceOperation, now time.Time, reason string) {
	o.State = "outcome_unknown"
	if o.UnknownSince == nil {
		o.UnknownSince = &now
	}
	o.DueAt = now.Add(15 * time.Second)
	o.FailureReason = reason
}
func commerceRelease(a *repository.CommerceAggregate) {
	if a.Resource.ActiveOperation == a.Operation.ID {
		a.Resource.ActiveOperation = ""
		a.Resource.AmountMinor = 0
		a.Resource.Quantity = 0
		a.Resource.Revision++
	}
}
func (s *commerceService) applyResult(a *repository.CommerceAggregate, r CommerceAdapterResult, wasSubmit bool) {
	o := &a.Operation
	if r.CustomerReview {
		o.State = "awaiting_customer"
		o.FundsState = r.FundsState
		return
	}
	if slices.Contains([]string{"accepted", "processing", "awaiting_customer"}, r.State) {
		o.State = r.State
		o.FundsState = r.FundsState
		return
	}
	if r.State == "failed" && r.Authoritative && r.EvidenceRef != "" && r.EvidenceSource == "platform" {
		o.State = "failed"
		o.EvidenceLevel = "platform_verified"
		o.EvidenceRef = r.EvidenceRef
		o.FailureReason = "provider_rejected"
		o.UnknownSince = nil
		commerceRelease(a)
		return
	}
	if r.State != "succeeded" || !r.Authoritative || !r.Postcondition || r.EvidenceSource != "platform" || r.EvidenceRef == "" || r.Action != o.Action {
		commerceUnknown(o, s.now(), "postcondition_unverified")
		return
	}
	params := commerceParams(*o)
	switch o.Action {
	case "address.change":
		if r.AddressDigest != commerceDigest([]string{params.Recipient, params.Phone, params.Address, params.Province, params.City, params.District}) || r.CustomerReview {
			commerceUnknown(o, s.now(), "address_readback_mismatch")
			return
		}
	case "refund", "refund.partial", "refund.difference":
		if r.FundsState != "succeeded" || r.AmountMinor != o.AmountMinor || r.Currency != o.Currency || r.ExternalRef == "" || params.PaymentPath == "manual" {
			commerceUnknown(o, s.now(), "funds_unverified")
			return
		}
	case "return.approve":
		if r.NativeState != "return_authorized" {
			commerceUnknown(o, s.now(), "return_state_unverified")
			return
		}
	case "exchange.ship", "reship":
		if r.NativeState != "shipment_created" || r.ExternalRef == "" {
			commerceUnknown(o, s.now(), "shipment_unverified")
			return
		}
	case "managed.observe":
		if wasSubmit {
			commerceUnknown(o, s.now(), "managed_write_forbidden")
			return
		}
	}
	o.State = "succeeded"
	o.EvidenceLevel = "platform_verified"
	o.EvidenceRef = r.EvidenceRef
	o.FundsState = r.FundsState
	o.UnknownSince = nil
	o.FailureReason = ""
	commerceRelease(a)
}

// RunDue is a bounded lifecycle entry. Caller supplies one tenant context; cancellation stops claims.
// Each shop's short network lease is distinct from the order guard retained while waiting.
func (s *commerceService) RunDue(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 10 {
		limit = 10
	}
	rows, e := s.repo.Due(ctx, s.now(), limit)
	if e != nil {
		return 0, e
	}
	done := 0
	for _, o := range rows {
		if ctx.Err() != nil {
			return done, ctx.Err()
		}
		if o.Mode == commerceWorkbench {
			continue
		}
		_, e := s.execute(ctx, o.ActorID, o.ID, false)
		if e != nil {
			return done, e
		}
		done++
	}
	return done, nil
}
