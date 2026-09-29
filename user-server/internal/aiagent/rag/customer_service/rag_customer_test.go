package ragcustomerservice

import (
	"context"
	"testing"
	"time"
)

func TestNewInMemoryDialogManager_NilConfig(t *testing.T) {
	manager := NewInMemoryDialogManager(nil)
	if manager == nil {
		t.Fatal("Expected non-nil InMemoryDialogManager")
	}
	if manager.config.DefaultMaxHistoryLength != 10 {
		t.Errorf("Expected default max history length 10, got %d", manager.config.DefaultMaxHistoryLength)
	}
}

func TestNewInMemoryDialogManager_CustomConfig(t *testing.T) {
	config := &DialogManagerConfig{
		DefaultMaxHistoryLength: 20,
		DefaultSessionTimeout:   1 * time.Hour,
	}
	manager := NewInMemoryDialogManager(config)
	if manager.config.DefaultMaxHistoryLength != 20 {
		t.Errorf("Expected max history length 20, got %d", manager.config.DefaultMaxHistoryLength)
	}
}

func TestInMemoryDialogManager_CreateSession(t *testing.T) {
	manager := NewInMemoryDialogManager(nil)
	session, err := manager.CreateSession(context.Background(), "user-1", "web", "kb-1", SessionConfig{})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if session == nil {
		t.Fatal("Expected non-nil session")
	}
	if session.UserID != "user-1" {
		t.Errorf("Expected UserID 'user-1', got '%s'", session.UserID)
	}
	if session.Platform != "web" {
		t.Errorf("Expected Platform 'web', got '%s'", session.Platform)
	}
	if session.Status != SessionActive {
		t.Errorf("Expected Status 'active', got '%s'", session.Status)
	}
}

func TestInMemoryDialogManager_GetSession(t *testing.T) {
	config := &DialogManagerConfig{
		DefaultMaxHistoryLength: 10,
		DefaultSessionTimeout:   30 * time.Minute,
		SessionCleanupInterval:  5 * time.Minute,
	}
	manager := NewInMemoryDialogManager(config)
	session, _ := manager.CreateSession(context.Background(), "user-1", "web", "kb-1", SessionConfig{})

	got, err := manager.GetSession(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if got.ID != session.ID {
		t.Errorf("Expected session ID '%s', got '%s'", session.ID, got.ID)
	}
}

func TestInMemoryDialogManager_GetSession_NotFound(t *testing.T) {
	config := &DialogManagerConfig{
		DefaultMaxHistoryLength: 10,
		DefaultSessionTimeout:   30 * time.Minute,
		SessionCleanupInterval:  5 * time.Minute,
	}
	manager := NewInMemoryDialogManager(config)
	_, err := manager.GetSession(context.Background(), "nonexistent")
	if err == nil {
		t.Error("Expected error for nonexistent session")
	}
}

func TestInMemoryDialogManager_CloseSession(t *testing.T) {
	config := &DialogManagerConfig{
		DefaultMaxHistoryLength: 10,
		DefaultSessionTimeout:   30 * time.Minute,
		SessionCleanupInterval:  5 * time.Minute,
	}
	manager := NewInMemoryDialogManager(config)
	session, _ := manager.CreateSession(context.Background(), "user-1", "web", "kb-1", SessionConfig{})

	err := manager.CloseSession(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	got, _ := manager.GetSession(context.Background(), session.ID)
	if got.Status != SessionClosed {
		t.Errorf("Expected Status 'closed', got '%s'", got.Status)
	}
}

func TestSessionStatus_Values(t *testing.T) {
	if SessionActive != "active" {
		t.Error("SessionActive should be 'active'")
	}
	if SessionPaused != "paused" {
		t.Error("SessionPaused should be 'paused'")
	}
	if SessionClosed != "closed" {
		t.Error("SessionClosed should be 'closed'")
	}
	if SessionExpired != "expired" {
		t.Error("SessionExpired should be 'expired'")
	}
}
