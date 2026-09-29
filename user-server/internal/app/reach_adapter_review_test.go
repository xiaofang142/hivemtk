package app

import (
	"context"
	"errors"
	"testing"
)

// TestParseAccountID 覆盖 parseAccountID 边界用例
func TestParseAccountID(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    uint
		wantErr error
	}{
		{"normal", "123", 123, nil},
		{"large", "999999", 999999, nil},
		{"zero", "0", 0, ErrInvalidAccountID},
		{"empty", "", 0, ErrInvalidAccountID},
		{"non-numeric", "abc", 0, ErrInvalidAccountID},
		{"negative", "-1", 0, ErrInvalidAccountID},
		{"mixed", "12a3", 0, ErrInvalidAccountID},
		{"space", " 123", 0, ErrInvalidAccountID},
		{"plus", "+123", 0, ErrInvalidAccountID},
		{"hex", "0x1A", 0, ErrInvalidAccountID},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseAccountID(tt.input)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("parseAccountID(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Errorf("parseAccountID(%q) unexpected error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("parseAccountID(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

// TestParseInt64 覆盖 parseInt64 边界用例（包括群组 chat_id 负数）
func TestParseInt64(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int64
		wantErr error
	}{
		{"normal-positive", "123", 123, nil},
		{"normal-negative", "-1001234567890", -1001234567890, nil},
		{"zero", "0", 0, ErrInvalidInt64},
		{"empty", "", 0, ErrInvalidInt64},
		{"non-numeric", "abc", 0, ErrInvalidInt64},
		{"plus-prefix", "+123", 123, nil},
		{"minus-only", "-", 0, ErrInvalidInt64},
		{"trailing-chars", "123x", 0, ErrInvalidInt64},
		{"max-int64", "9223372036854775807", 9223372036854775807, nil},
		{"min-int64", "-9223372036854775808", -9223372036854775808, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseInt64(tt.input)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("parseInt64(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Errorf("parseInt64(%q) unexpected error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("parseInt64(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

// TestIntegrationReachAdapter_SentinelErrors 验证 IntegrationReachAdapter 返回 sentinel error
func TestIntegrationReachAdapter_SentinelErrors(t *testing.T) {
	a := &IntegrationReachAdapter{}
	_, err := a.SendTelegram(context.Background(), "1", "123", "x")
	if !errors.Is(err, ErrIntegrationServiceNotConfigured) {
		t.Errorf("SendTelegram 应返回 ErrIntegrationServiceNotConfigured, got %v", err)
	}
	_, err = a.SendWhatsApp(context.Background(), "1", "+861", "x")
	if !errors.Is(err, ErrIntegrationServiceNotConfigured) {
		t.Errorf("SendWhatsApp 应返回 ErrIntegrationServiceNotConfigured, got %v", err)
	}
	_, err = a.SendFeishu(context.Background(), "1", "ou_x", "x")
	if !errors.Is(err, ErrIntegrationServiceNotConfigured) {
		t.Errorf("SendFeishu 应返回 ErrIntegrationServiceNotConfigured, got %v", err)
	}

	_, err = a.SendSMS(context.Background(), "1", "x", "", nil)
	if !errors.Is(err, ErrIntegrationServiceNotConfigured) {
		t.Errorf("SendSMS 应返回 ErrIntegrationServiceNotConfigured, got %v", err)
	}
	_, err = a.SendEmail(context.Background(), "1", "s", "x", nil)
	if !errors.Is(err, ErrIntegrationServiceNotConfigured) {
		t.Errorf("SendEmail 应返回 ErrIntegrationServiceNotConfigured, got %v", err)
	}
	err = a.Recall(context.Background(), "sms", "x")
	if !errors.Is(err, ErrChannelNotImplemented) {
		t.Errorf("Recall 应返回 ErrChannelNotImplemented, got %v", err)
	}
	_, err = a.AccountHealth(context.Background(), "sms", "1")
	if !errors.Is(err, ErrChannelNotImplemented) {
		t.Errorf("AccountHealth 应返回 ErrChannelNotImplemented, got %v", err)
	}
	_, err = a.ListAccounts(context.Background(), "sms")
	if !errors.Is(err, ErrChannelNotImplemented) {
		t.Errorf("ListAccounts 应返回 ErrChannelNotImplemented, got %v", err)
	}
}

// TestNewIntegrationReachAdapterFromDB_NilDB 验证 nil db 时不 panic 且返回空壳
// 真实 db 路径实例化 IntegrationService，见 TestNewIntegrationReachAdapterFromDB_RealDB。
func TestNewIntegrationReachAdapterFromDB_NilDB(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("NewIntegrationReachAdapterFromDB(nil) panic: %v", r)
		}
	}()
	a := NewIntegrationReachAdapterFromDB(nil)
	if a == nil {
		t.Fatal("应返回非 nil adapter")
	}
	if a.tg != nil || a.wa != nil || a.feishu != nil {
		t.Error("nil db 时所有 IntegrationService 应为 nil")
	}
}
