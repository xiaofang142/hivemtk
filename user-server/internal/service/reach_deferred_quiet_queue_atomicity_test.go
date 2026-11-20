package service

import (
	"errors"
	"testing"

	"hivemtk-user/internal/model"
)

func TestReleaseAfterDeferredFailureRequiresTerminalPersistence(t *testing.T) {
	released := false
	if err := releaseAfterDeferredFailure(func() error { return errors.New("mark failed") }, func() {
		released = true
	}); err == nil {
		t.Fatal("failed terminal persistence must return its error")
	}
	if released {
		t.Fatal("quota must not release before failed status is persisted")
	}

	if err := releaseAfterDeferredFailure(func() error { return nil }, func() {
		released = true
	}); err != nil {
		t.Fatalf("successful terminal persistence should return nil: %v", err)
	}
	if !released {
		t.Fatal("quota should release after successful terminal persistence")
	}
}

func TestDeferredIntentID_ExtractsMetadataFromInvalidRequestShape(t *testing.T) {
	rec := &model.ReachDeferredOutbound{
		RequestJSON: model.JSONMap{
			"channel":  123,
			"metadata": map[string]any{"action_intent_id": "intent-invalid-shape"},
		},
	}

	if got := deferredIntentID(rec); got != "intent-invalid-shape" {
		t.Fatalf("deferredIntentID = %q, want intent-invalid-shape", got)
	}
}
