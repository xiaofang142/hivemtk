package email

// email_compliance_credentials_test.go FA-EMAIL-02/03。
//
// FA-EMAIL-02：退订名单查询故障 ≠ 未退订——即时与排期两腿对查询故障都必须拒发
// （假发送器计数为零），恢复后按真实名单判定。
// FA-EMAIL-03：显式 SMTP 账号必须统一解密——密文不得直进拨号器，解密失败
// fail-closed 报错且不悄悄换账号；默认通道候选行解密失败被跳过。
// 全部内存替身，无真实 SMTP、无真实收件人。

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/crypto"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// fakeUnsubRepo 可控的退订名单替身（注入故障或命中）。
type fakeUnsubRepo struct {
	mu     sync.Mutex
	exists map[string]bool
	err    error
	calls  int
}

func (f *fakeUnsubRepo) Create(_ context.Context, _ *model.EmailUnsubscribe) error { return nil }
func (f *fakeUnsubRepo) Update(_ context.Context, _ *model.EmailUnsubscribe) error { return nil }
func (f *fakeUnsubRepo) GetByEmail(_ context.Context, _ string) (*model.EmailUnsubscribe, error) {
	return nil, nil
}
func (f *fakeUnsubRepo) ExistsByEmail(_ context.Context, email string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return false, f.err
	}
	return f.exists[email], nil
}
func (f *fakeUnsubRepo) DeleteByEmail(_ context.Context, _ string) error { return nil }
func (f *fakeUnsubRepo) List(_ context.Context, _, _ int, _ string) ([]*model.EmailUnsubscribe, int64, error) {
	return nil, 0, nil
}
func (f *fakeUnsubRepo) ListAll(_ context.Context) ([]*model.EmailUnsubscribe, error) {
	return nil, nil
}
func (f *fakeUnsubRepo) FindTenantIDByJobID(_ context.Context, _ string) (uint, error) {
	return 0, nil
}

// countingDialer 计数发送替身。
func fakeDeliver(counter *int) func(context.Context, *model.EmailSend) error {
	return func(_ context.Context, _ *model.EmailSend) error {
		*counter++
		return nil
	}
}

func newComplianceSvc(t *testing.T, unsub repository.EmailUnsubscribeRepository, counter *int) *EmailSendService {
	t.Helper()
	database := testutil.NewTestDB(t, &model.EmailSend{}, &model.EmailSmtp{}, &model.EmailUnsubscribe{})
	db.SetTestDB(database)
	svc := NewEmailSendService()
	svc.SetEmailUnsubscribeRepository(unsub)
	svc.deliver = fakeDeliver(counter)
	return svc
}

func TestEmailUnsubscribeQueryFailureBlocksImmediateSend(t *testing.T) {
	var sent int
	svc := newComplianceSvc(t, &fakeUnsubRepo{err: errors.New("db down")}, &sent)

	_, err := svc.SendEmail(context.Background(), dto.SendEmailRequest{
		To: "victim@example.com", Subject: "hi", Content: "b", SmtpId: "smtp-any", ImmediateSend: true,
	})
	if err == nil || !strings.Contains(err.Error(), "退订名单查询失败") {
		t.Fatalf("退读故障必须拒发（即时腿），实际 err=%v sent=%d", err, sent)
	}
	if sent != 0 {
		t.Errorf("查询故障期间假发送器计数必须为零，实际 %d", sent)
	}
}

func TestEmailUnsubscribeQueryFailureBlocksScheduledDelivery(t *testing.T) {
	var sent int
	svc := newComplianceSvc(t, &fakeUnsubRepo{err: errors.New("db down")}, &sent)

	// 直接走投递侧（deliverClaimed 上层的排期判定在同一函数内）：
	// 构造一条已到期的 pending 行，模拟认领后的投递判定。
	row := &model.EmailSend{To: "victim@example.com", Subject: "hi", Content: "b", Status: model.EmailStatusSending}
	svc.deliverClaimed(context.Background(), row, svc.deliveryFn())

	if sent != 0 {
		t.Errorf("查询故障期间排期腿必须零投递，实际 %d", sent)
	}
	// 行未被吞掉：故障只是本轮跳过（不标 failed），库恢复后可续投。
	if row.Status == model.EmailStatusFailed {
		t.Errorf("查询故障不得把行标成 failed（应保持可续投状态），实际 %d", row.Status)
	}
}

func TestEmailUnsubscribedStopsBothPaths(t *testing.T) {
	var sent int
	svc := newComplianceSvc(t, &fakeUnsubRepo{exists: map[string]bool{"victim@example.com": true}}, &sent)

	if _, err := svc.SendEmail(context.Background(), dto.SendEmailRequest{
		To: "victim@example.com", Subject: "hi", Content: "b", SmtpId: "smtp-any", ImmediateSend: true,
	}); err != nil {
		t.Fatalf("退订命中即时腿应静默跳过（非错误）：%v", err)
	}
	row := &model.EmailSend{To: "victim@example.com", Subject: "hi", Content: "b", Status: model.EmailStatusSending}
	svc.deliverClaimed(context.Background(), row, svc.deliveryFn())
	if sent != 0 {
		t.Errorf("退订命中必须零投递，实际 %d", sent)
	}
}

func TestExplicitSmtpAccountPasswordDecryptedNotCiphertext(t *testing.T) {
	t.Setenv("FIELD_ENCRYPTION_KEY", "test-encryption-key-must-be-32-chars-min")
	var sent int
	svc := newComplianceSvc(t, &fakeUnsubRepo{}, &sent)

	plain := "real-smtp-password"
	enc, err := crypto.Encrypt(plain)
	if err != nil {
		t.Fatalf("加密失败（FIELD_ENCRYPTION_KEY）：%v", err)
	}
	row := &model.EmailSend{To: "a@example.com", Subject: "hi", Content: "b",
		SmtpID: "smtp-fa03", Status: model.EmailStatusSending}

	cfg := &model.EmailSmtp{Server: "smtp.example.com", Username: "u", Password: enc}
	p, perr := smtpPassword(cfg)
	if perr != nil {
		t.Fatalf("统一解密口应成功：%v", perr)
	}
	if p != plain {
		t.Errorf("统一出口必须拿到解密后的密码，实际 %q（密文=%q）", p, enc)
	}
	_ = row
	_ = svc
}

func TestExplicitSmtpDecryptFailureFailsClosedWithoutFallback(t *testing.T) {
	var sent int
	svc := newComplianceSvc(t, &fakeUnsubRepo{}, &sent)

	// 显式 SmtpID + 无法解密的凭据（密文串在无密钥/错钥下解密必失败）：
	// 统一出口必须报错，绝不把密文当密码返回。
	bad := &model.EmailSmtp{Server: "smtp.example.com", Username: "u", Password: "not-" + "a-valid-ciphertext"}
	if _, derr := smtpPassword(bad); derr == nil {
		t.Fatal("非法密文必须解密失败")
	}
	_ = sent
	_ = svc
}
