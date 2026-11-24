package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"hivemtk-user/internal/model"
)

type fakeSageXInstRepo struct {
	byToken map[string]*model.SageXInstallation
}

func (f *fakeSageXInstRepo) Create(_ context.Context, inst *model.SageXInstallation) error {
	if f.byToken == nil {
		f.byToken = map[string]*model.SageXInstallation{}
	}
	f.byToken[inst.Token] = inst
	return nil
}
func (f *fakeSageXInstRepo) Update(_ context.Context, inst *model.SageXInstallation) error {
	if f.byToken == nil {
		f.byToken = map[string]*model.SageXInstallation{}
	}
	f.byToken[inst.Token] = inst
	return nil
}
func (f *fakeSageXInstRepo) GetByID(_ context.Context, _ uint) (*model.SageXInstallation, error) {
	return nil, nil
}
func (f *fakeSageXInstRepo) GetByToken(_ context.Context, token string) (*model.SageXInstallation, error) {
	if f.byToken == nil {
		return nil, nil
	}
	return f.byToken[token], nil
}
func (f *fakeSageXInstRepo) GetByInstallationID(_ context.Context, _ string) (*model.SageXInstallation, error) {
	return nil, nil
}
func (f *fakeSageXInstRepo) TouchLastSeen(_ context.Context, _ uint, _ time.Time) error { return nil }

type fakeSageXReceiptRepo struct {
	receipts map[string]*model.SageXEventReceipt
}

func receiptKey(tenantID uint, installationID, eventID string) string {
	return string(rune(tenantID)) + "|" + installationID + "|" + eventID
}

func (f *fakeSageXReceiptRepo) GetByInstallationAndEventID(_ context.Context, tenantID uint, installationID, eventID string) (*model.SageXEventReceipt, error) {
	if f.receipts == nil {
		return nil, nil
	}
	r := f.receipts[receiptKey(tenantID, installationID, eventID)]
	if r == nil {
		return nil, nil
	}
	cp := *r
	return &cp, nil
}
func (f *fakeSageXReceiptRepo) Create(_ context.Context, receipt *model.SageXEventReceipt) error {
	if f.receipts == nil {
		f.receipts = map[string]*model.SageXEventReceipt{}
	}
	k := receiptKey(receipt.TenantID, receipt.InstallationID, receipt.EventID)
	if _, ok := f.receipts[k]; ok {
		return errors.New("duplicate key value violates unique constraint")
	}
	cp := *receipt
	f.receipts[k] = &cp
	return nil
}
func (f *fakeSageXReceiptRepo) Update(_ context.Context, receipt *model.SageXEventReceipt) error {
	if f.receipts == nil {
		f.receipts = map[string]*model.SageXEventReceipt{}
	}
	k := receiptKey(receipt.TenantID, receipt.InstallationID, receipt.EventID)
	cp := *receipt
	f.receipts[k] = &cp
	return nil
}

type fakeSageXIngress struct {
	calls   []*model.MessageEvent
	result  *InboxIngressResult
	err     error
	results map[string]*InboxIngressResult
}

func (f *fakeSageXIngress) HandleIngressMessage(_ context.Context, event *model.MessageEvent) (*InboxIngressResult, error) {
	f.calls = append(f.calls, event)
	if f.results != nil {
		if r, ok := f.results[event.EventID]; ok {
			return r, f.err
		}
	}
	if f.result != nil {
		cp := *f.result
		return &cp, f.err
	}
	return &InboxIngressResult{Accepted: true}, f.err
}

func TestSageXUpload_Unauthorized(t *testing.T) {
	svc := NewSageXUploadService(&fakeSageXInstRepo{}, &fakeSageXReceiptRepo{}, &fakeSageXIngress{})
	_, err := svc.HandleUpload(context.Background(), "bad", &SageXUploadRequest{
		SchemaVersion: SageXSchemaVersion, BatchID: "b1", InstallationID: "i1",
		Events: []SageXEventItem{{EventID: "e1", EventType: "notification.posted"}},
	}, 10)
	if !errors.Is(err, ErrSageXUnauthorized) {
		t.Fatalf("expected unauthorized, got %v", err)
	}
}

func TestSageXUpload_SchemaAndBatchValidation(t *testing.T) {
	instRepo := &fakeSageXInstRepo{byToken: map[string]*model.SageXInstallation{"tok": {TenantID: 1, ID: 1, InstallationID: "i1", Token: "tok", Status: 1}}}
	svc := NewSageXUploadService(instRepo, &fakeSageXReceiptRepo{}, &fakeSageXIngress{})
	_, err := svc.HandleUpload(context.Background(), "tok", &SageXUploadRequest{SchemaVersion: "2.0", BatchID: "b1", InstallationID: "i1", Events: []SageXEventItem{{EventID: "e1"}}}, 10)
	if !errors.Is(err, ErrSageXUnsupportedSchema) {
		t.Fatalf("expected schema error, got %v", err)
	}
	_, err = svc.HandleUpload(context.Background(), "tok", &SageXUploadRequest{SchemaVersion: SageXSchemaVersion, BatchID: "", InstallationID: "i1", Events: []SageXEventItem{{EventID: "e1"}}}, 10)
	if !errors.Is(err, ErrSageXInvalidEnvelope) {
		t.Fatalf("expected invalid envelope, got %v", err)
	}
}

func TestSageXUpload_DuplicateInBatchNotAcked(t *testing.T) {
	instRepo := &fakeSageXInstRepo{byToken: map[string]*model.SageXInstallation{"tok": {TenantID: 1, ID: 1, InstallationID: "i1", Token: "tok", Status: 1}}}
	ingress := &fakeSageXIngress{}
	svc := NewSageXUploadService(instRepo, &fakeSageXReceiptRepo{}, ingress)
	resp, err := svc.HandleUpload(context.Background(), "tok", &SageXUploadRequest{
		SchemaVersion: SageXSchemaVersion, BatchID: "b1", InstallationID: "i1",
		Events: []SageXEventItem{
			{EventID: "e1", EventType: "notification.posted", Source: SageXSource{Platform: "android", Package: "com.tencent.mobileqq"}, Conversation: SageXConversation{Type: "private", Title: "Alice"}, Message: SageXMessage{Text: "hi"}},
			{EventID: "e1", EventType: "notification.posted", Source: SageXSource{Platform: "android", Package: "com.tencent.mobileqq"}, Conversation: SageXConversation{Type: "private", Title: "Alice"}, Message: SageXMessage{Text: "hi"}},
			{EventID: "e2", EventType: "notification.posted", Source: SageXSource{Platform: "android", Package: "com.tencent.mm"}, Conversation: SageXConversation{Type: "private", Title: "Bob"}, Message: SageXMessage{Text: "yo"}},
		},
	}, 10)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(resp.Acks) != 1 || resp.Acks[0].EventID != "e2" || resp.Acks[0].Status != SageXAckAccepted {
		t.Fatalf("unexpected acks: %+v", resp.Acks)
	}
	if len(ingress.calls) != 1 {
		t.Fatalf("expected 1 ingress call, got %d", len(ingress.calls))
	}
}

func TestSageXUpload_IdempotentReceiptReturnsDuplicate(t *testing.T) {
	instRepo := &fakeSageXInstRepo{byToken: map[string]*model.SageXInstallation{"tok": {TenantID: 1, ID: 1, InstallationID: "i1", Token: "tok", Status: 1}}}
	receiptRepo := &fakeSageXReceiptRepo{receipts: map[string]*model.SageXEventReceipt{
		receiptKey(1, "i1", "e1"): {TenantID: 1, InstallationID: "i1", EventID: "e1", Status: SageXAckAccepted},
	}}
	ingress := &fakeSageXIngress{}
	svc := NewSageXUploadService(instRepo, receiptRepo, ingress)
	resp, err := svc.HandleUpload(context.Background(), "tok", &SageXUploadRequest{
		SchemaVersion: SageXSchemaVersion, BatchID: "b2", InstallationID: "i1",
		Events: []SageXEventItem{{EventID: "e1", EventType: "notification.posted", Source: SageXSource{Package: "com.tencent.mobileqq"}, Conversation: SageXConversation{Type: "private", Title: "A"}, Message: SageXMessage{Text: "x"}}},
	}, 10)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(resp.Acks) != 1 || resp.Acks[0].Status != SageXAckDuplicate {
		t.Fatalf("unexpected acks: %+v", resp.Acks)
	}
	if len(ingress.calls) != 0 {
		t.Fatalf("ingress should not be called for idempotent receipt")
	}
}

func TestSageXUpload_MapsQQAndWeChat(t *testing.T) {
	instRepo := &fakeSageXInstRepo{byToken: map[string]*model.SageXInstallation{"tok": {TenantID: 1, ID: 1, InstallationID: "i1", Token: "tok", Status: 1, AccountID: "acct1"}}}
	ingress := &fakeSageXIngress{}
	svc := NewSageXUploadService(instRepo, &fakeSageXReceiptRepo{}, ingress)
	resp, err := svc.HandleUpload(context.Background(), "tok", &SageXUploadRequest{
		SchemaVersion: SageXSchemaVersion, BatchID: "b1", InstallationID: "i1",
		Events: []SageXEventItem{
			{EventID: "e1", EventType: "notification.posted", Source: SageXSource{Platform: "android", Package: "com.tencent.mobileqq"}, Conversation: SageXConversation{Type: "direct", Title: "Alice"}, Message: SageXMessage{Sender: "Alice", Text: "hi", NotificationPostedAt: 1795435200000}, CapturedAt: "2026-11-23T10:00:01Z"},
			{EventID: "e2", EventType: "notification.posted", Source: SageXSource{Platform: "android", Package: "com.tencent.mm"}, Conversation: SageXConversation{Type: "group", Title: "G"}, Message: SageXMessage{Sender: "Bob", Text: "yo"}},
		},
	}, 10)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(resp.Acks) != 2 {
		t.Fatalf("expected 2 acks, got %+v", resp.Acks)
	}
	if ingress.calls[0].Channel != model.ChannelQQ {
		t.Fatalf("expected qq, got %s", ingress.calls[0].Channel)
	}
	if ingress.calls[1].Channel != model.ChannelPersonalWX {
		t.Fatalf("expected personal_wx, got %s", ingress.calls[1].Channel)
	}
	if ingress.calls[0].ReceiverID != "acct1" || ingress.calls[0].SenderType != "customer" {
		t.Fatalf("unexpected mapped event: %+v", ingress.calls[0])
	}
	if !ingress.calls[1].IsGroup || ingress.calls[1].GroupID == "" {
		t.Fatalf("group mapping failed: %+v", ingress.calls[1])
	}
}

func TestSageXUpload_UnsupportedPackageRejected(t *testing.T) {
	instRepo := &fakeSageXInstRepo{byToken: map[string]*model.SageXInstallation{"tok": {TenantID: 1, ID: 1, InstallationID: "i1", Token: "tok", Status: 1}}}
	svc := NewSageXUploadService(instRepo, &fakeSageXReceiptRepo{}, &fakeSageXIngress{})
	resp, err := svc.HandleUpload(context.Background(), "tok", &SageXUploadRequest{
		SchemaVersion: SageXSchemaVersion, BatchID: "b1", InstallationID: "i1",
		Events: []SageXEventItem{{EventID: "e1", EventType: "notification.posted", Source: SageXSource{Package: "com.unknown.app"}, Conversation: SageXConversation{Type: "private", Title: "A"}, Message: SageXMessage{Text: "x"}}},
	}, 10)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if resp.Acks[0].Status != SageXAckRejected {
		t.Fatalf("expected rejected, got %+v", resp.Acks[0])
	}
}
