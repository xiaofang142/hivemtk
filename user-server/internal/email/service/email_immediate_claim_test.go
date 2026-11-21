package email

// email_immediate_claim_test.go FA-EMAIL-01：即时与排期共享唯一认领，防双投。
//
// 钉住的契约：即时 goroutine 投递前必须按 ID 原子认领（与排水 worker 同一原语）；
// 行已被另一方认领时让位——阻塞假投递器交错即时 goroutine 与后台轮次，
// 接收计数恰好一次。走真实测试库（FOR UPDATE SKIP LOCKED 语义），
// 无真实 SMTP、无真实收件人。

import (
	"context"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// blockingRecorder 投递可阻塞的替身：enter/leave 两道闸控制交错。
type blockingRecorder struct {
	mu      sync.Mutex
	attempt []string
	enter   chan struct{} // 关闭即放行投递
	started chan struct{} // 首次投递开始时关闭
}

func newBlockingRecorder() *blockingRecorder {
	return &blockingRecorder{enter: make(chan struct{}), started: make(chan struct{})}
}

func (b *blockingRecorder) deliver(_ context.Context, email *model.EmailSend) error {
	b.mu.Lock()
	if len(b.attempt) == 0 {
		close(b.started)
	}
	b.attempt = append(b.attempt, email.To)
	b.mu.Unlock()
	<-b.enter // 阻塞：模拟 SMTP 慢/卡
	return nil
}

func (b *blockingRecorder) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.attempt)
}

func (b *blockingRecorder) waitStarted() {
	select {
	case <-b.started:
	case <-time.After(5 * time.Second):
	}
}

func setupImmediateClaimDB(t *testing.T) {
	t.Helper()
	database := testutil.NewTestDB(t, &model.EmailSend{}, &model.EmailSmtp{}, &model.EmailUnsubscribe{}, &model.SystemUser{})
	db.SetTestDB(database)
}

func newImmediateClaimService(t *testing.T, deliver func(context.Context, *model.EmailSend) error) *EmailSendService {
	t.Helper()
	svc := NewEmailSendService()
	svc.deliver = deliver
	svc.SetEmailUnsubscribeRepository(repository.NewEmailUnsubscribeRepository(db.GetDB()))
	return svc
}

func TestEmailImmediateAndWorkerShareSingleClaim(t *testing.T) {
	setupImmediateClaimDB(t)
	rec := newBlockingRecorder()
	svc := newImmediateClaimService(t, rec.deliver)

	// 即时发送：goroutine 认领（sending）并阻塞在投递里。
	emailSend, err := svc.SendEmail(context.Background(), dto.SendEmailRequest{
		To: "once@example.com", Subject: "hi", Content: "b", SmtpId: "smtp-any", ImmediateSend: true,
	})
	if err != nil {
		t.Fatalf("即时发送受理失败：%v", err)
	}
	rec.waitStarted()
	if rec.count() != 1 {
		t.Fatalf("即时 goroutine 应已开始投递，实际 %d", rec.count())
	}

	// 认领中的行：排水 worker 一轮不得再投（行是 sending，不是 pending）。
	if err := svc.ProcessPendingEmails(context.Background()); err != nil {
		t.Fatalf("排水一轮失败：%v", err)
	}
	if rec.count() != 1 {
		t.Fatalf("排水轮不得重复投递认领中的邮件，实际投递 %d 次", rec.count())
	}

	// 放行即时投递 → 恰好一次，状态 sent。
	close(rec.enter)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var row model.EmailSend
		if err := db.GetDB().Where("id = ?", emailSend.ID).First(&row).Error; err == nil && row.Status == model.EmailStatusSent {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	var row model.EmailSend
	if err := db.GetDB().Where("id = ?", emailSend.ID).First(&row).Error; err != nil {
		t.Fatalf("读回邮件行失败：%v", err)
	}
	if row.Status != model.EmailStatusSent {
		t.Fatalf("投递完成后应为 sent，实际 %d", row.Status)
	}
	if rec.count() != 1 {
		t.Fatalf("全程收件计数必须恰好一次，实际 %d", rec.count())
	}
}

func TestEmailImmediateYieldsWhenWorkerClaimed(t *testing.T) {
	setupImmediateClaimDB(t)
	rec := &recorder{}
	svc := newImmediateClaimService(t, rec.deliver)

	// 交错竞态（原语层）：worker 先按 ClaimDueForUpdate 认领（行→sending），
	// 即时侧随后按 ID 认领必须让位（not-ok），收件计数由 worker 侧承载恰好一次。
	now := time.Now()
	rowID := createPendingSend(t, db.GetDB(), "yield@example.com", &now)

	claimed, err := svc.repo.ClaimDueForUpdate(context.Background(), time.Now(), 10)
	if err != nil || len(claimed) != 1 || claimed[0].ID != rowID {
		t.Fatalf("worker 认领失败：%v %+v", err, claimed)
	}

	got, ok, cerr := svc.repo.ClaimByIDForDelivery(context.Background(), rowID)
	if cerr != nil {
		t.Fatalf("按 ID 认领不应报错：%v", cerr)
	}
	if ok || got != nil {
		t.Fatalf("worker 已认领的行，即时侧必须让位，实际 ok=%v row=%+v", ok, got)
	}

	// worker 投递一次并写回。
	if err := svc.ProcessPendingEmails(context.Background()); err != nil {
		t.Fatalf("排水一轮失败：%v", err)
	}
	// 行已是 sending 且不在此轮 pending 集内：手动走 deliverClaimed 验证写回口径。
	var row model.EmailSend
	if err := db.GetDB().Where("id = ?", rowID).First(&row).Error; err != nil {
		t.Fatalf("读回失败：%v", err)
	}
	svc.deliverClaimed(context.Background(), &row, svc.deliveryFn())
	if got := rec.called(); len(got) != 1 || got[0] != "yield@example.com" {
		t.Fatalf("恰好一次投递，实际 %v", got)
	}
}

func TestEmailClaimByIDFailsClosedOnMissingRow(t *testing.T) {
	setupImmediateClaimDB(t)
	svc := newImmediateClaimService(t, (&recorder{}).deliver)

	_, ok, err := svc.repo.ClaimByIDForDelivery(context.Background(), "no-such-row")
	if err != nil {
		t.Fatalf("不存在的行认领应静默让位而非报错：%v", err)
	}
	if ok {
		t.Fatal("不存在的行不得被认领")
	}
}
