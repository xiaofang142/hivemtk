package service

import (
	"context"
	"testing"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
)

func TestProactiveReachService_PickChannel_OutboundChannels(t *testing.T) {
	svc := NewProactiveReachService(nil, &mockAccountLookup{})
	customer := &model.Customer{
		UnifiedID: "phone:13800138000",
		Phone:     "13800138000",
		Email:     "user@example.com",
	}

	tests := []struct {
		name      string
		channel   string
		recipient string
	}{
		{"sms_no_account_lookup", "sms", "13800138000"},
		{"email_no_account_lookup", "email", "user@example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, recipient, acc, err := svc.pickChannel(context.Background(), []string{tt.channel}, customer)
			if err != nil {
				t.Fatalf("expected no error for %s, got: %v", tt.channel, err)
			}
			if recipient != tt.recipient {
				t.Errorf("recipient mismatch: want=%s got=%s", tt.recipient, recipient)
			}
			if acc != "" {
				t.Errorf("accountID should be empty for outbound channel, got=%s", acc)
			}
		})
	}
}

// TestProactiveReachService_PickChannel_NilDB_OutboundChannels 验证 nil DB 下出站渠道仍可用
func TestProactiveReachService_PickChannel_NilDB_OutboundChannels(t *testing.T) {
	svc := NewProactiveReachService(nil, &defaultAccountLookup{repo: nil})
	customer := &model.Customer{
		UnifiedID: "phone:13800138000",
		Phone:     "13800138000",
	}
	channel, recipient, _, err := svc.pickChannel(context.Background(), []string{"sms"}, customer)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if channel != "sms" || recipient != "13800138000" {
		t.Errorf("mismatch: channel=%s recipient=%s", channel, recipient)
	}
}

// TestProactiveReachService_PickChannel_SkipsEmptyIdentity 验证无身份的渠道被跳过
func TestProactiveReachService_PickChannel_SkipsEmptyIdentity(t *testing.T) {
	svc := NewProactiveReachService(nil, &mockAccountLookup{supported: map[string]bool{"wechat": true}})
	customer := &model.Customer{
		UnifiedID: "phone:13800138000",
		Phone:     "13800138000",
	}

	_, _, _, err := svc.pickChannel(context.Background(), []string{"wechat"}, customer)
	if err == nil {
		t.Fatalf("expected error when no wechat identity")
	}
}

// TestProactiveReachService_PickChannel_UsesAccountLookup 验证需要账号的渠道走 lookup
func TestProactiveReachService_PickChannel_UsesAccountLookup(t *testing.T) {
	svc := NewProactiveReachService(nil, &mockAccountLookup{
		supported: map[string]bool{"telegram": true},
		results:   map[string]string{"telegram": "42"},
	})
	customer := &model.Customer{
		UnifiedID:      "telegram:12345",
		TelegramChatID: 12345,
	}
	channel, recipient, acc, err := svc.pickChannel(context.Background(), []string{"telegram"}, customer)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if channel != "telegram" || recipient != "12345" || acc != "42" {
		t.Errorf("mismatch: channel=%s recipient=%s acc=%s", channel, recipient, acc)
	}
}

// TestProactiveReachService_PickChannel_NoActiveAccount 验证 lookup 返回空时报错
func TestProactiveReachService_PickChannel_NoActiveAccount(t *testing.T) {
	svc := NewProactiveReachService(nil, &mockAccountLookup{
		supported: map[string]bool{"telegram": true},
		results:   map[string]string{"telegram": ""},
	})
	customer := &model.Customer{
		UnifiedID:      "telegram:12345",
		TelegramChatID: 12345,
	}
	_, _, _, err := svc.pickChannel(context.Background(), []string{"telegram"}, customer)
	if err == nil {
		t.Fatalf("expected error when no active account")
	}
}

type mockAccountLookup struct {
	supported map[string]bool
	results   map[string]string
	errs      map[string]error
}

func (m *mockAccountLookup) FindActiveAccount(_ context.Context, channel string) (string, error) {
	if !m.supported[channel] {
		return "", gorm.ErrRecordNotFound
	}
	if m.errs != nil {
		if err, ok := m.errs[channel]; ok {
			return "", err
		}
	}
	if m.results != nil {
		return m.results[channel], nil
	}
	return "", nil
}
