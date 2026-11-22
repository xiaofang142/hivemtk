package service

import (
	"context"
	"strings"
	"time"

	"hivemtk-user/internal/model"
)

// ScopedConsentChecker 防止一个品牌的同意扩大为另一个品牌或出口的许可。
type ScopedConsentChecker interface {
	CheckScopedSendConsent(ctx context.Context, oneID, purpose, channel, brand string) ConsentDecision
}

func (s *ContactConsentService) CheckScopedSendConsent(ctx context.Context, oneID, purpose, channel, brand string) ConsentDecision {
	if s == nil || s.repo == nil {
		return ConsentDecision{Status: ConsentStatusUnavailable, Reason: ConsentReasonEventUnavailable}
	}
	brand = strings.TrimSpace(brand)
	if brand == "" {
		return ConsentDecision{Status: ConsentStatusMissing, Reason: ConsentReasonMissing}
	}
	ids, err := s.repo.LinkedOneIDs(ctx, strings.TrimSpace(oneID))
	if err != nil {
		return ConsentDecision{Status: ConsentStatusUnavailable, Reason: ConsentReasonIdentityUnavailable}
	}
	purpose = normalizeConsentPurpose(purpose)
	channel = strings.ToLower(strings.TrimSpace(channel))
	granted := false
	now := time.Now()
	for _, id := range ids {
		event, err := s.repo.LatestScopedEvent(ctx, id, purpose, channel, brand)
		if err != nil {
			return ConsentDecision{Status: ConsentStatusUnavailable, Reason: ConsentReasonEventUnavailable}
		}
		if event != nil {
			if event.Decision == model.ConsentDecisionRevoke {
				return ConsentDecision{Status: ConsentStatusRevoked, Reason: ConsentReasonRevoked}
			}
			valid := event.Decision == model.ConsentDecisionGrant && event.Brand == brand && event.Source != model.ConsentSourceLegacyAssert && event.StatementVersion != "" && event.EvidenceRef != "" && event.SourceRef != "" && (event.ExpiresAt == nil || event.ExpiresAt.After(now))
			if purpose == model.ConsentPurposeMarketing {
				valid = valid && event.LegalBasis == "consent"
			} else {
				valid = valid && (event.LegalBasis == "consent" || event.LegalBasis == "contract_necessary" || event.LegalBasis == "legal_obligation")
			}
			granted = granted || valid
		}
		pref, err := s.repo.GetPreference(ctx, id, purpose, channel)
		if err != nil {
			return ConsentDecision{Status: ConsentStatusUnavailable, Reason: ConsentReasonPreferenceUnavailable}
		}
		if pref != nil && pref.Paused && (pref.PausedUntil == nil || pref.PausedUntil.After(now)) {
			return ConsentDecision{Status: ConsentStatusPaused, PausedUntil: pref.PausedUntil, Reason: ConsentReasonPaused}
		}
	}
	if granted {
		return ConsentDecision{Status: ConsentStatusGranted, Trusted: true}
	}
	return ConsentDecision{Status: ConsentStatusMissing, Reason: ConsentReasonMissing}
}
