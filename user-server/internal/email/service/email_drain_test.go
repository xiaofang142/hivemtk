// email_drain_test.go 排期邮件排水的行为契约（R21）。
//
// 排水循环此前全仓零生产调用方：dto.SendEmailRequest 同时暴露 sendTime 与 immediateSend，
// 于是"排了时间"是一条合法入库形状，而它落 pending 之后既不会发送、也不会报错、
// 状态更不会流转 —— 用户看到的是一封永远停在"待发送"的邮件。本文件的每一条都对应
// 那个形状里的一格，且都必须能用"摘掉对应判据"的方式变红（见本轮变异记录）。
package email

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

func setupEmailDrainTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database := testutil.NewTestDB(t,
		&model.EmailSend{},
		&model.EmailSmtp{},
		&model.EmailUnsubscribe{},
	)
	db.SetTestDB(database)
	return database
}

// recorder 是投递接缝的测试替身：它记录"哪一封被真的尝试投递"。
//
// 断言必须打在尝试投递这一件事上，而不是打在最后状态上 —— 状态 1 既能来自"投出去了"，
// 也能来自"投递函数没被调用而直接被改状态"（那是本轮要防的另一种错法）。
type recorder struct {
	mu        sync.Mutex
	attempted []string
	err       error
}

func (r *recorder) deliver(_ context.Context, email *model.EmailSend) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.attempted = append(r.attempted, email.To)
	return r.err
}

func (r *recorder) called() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.attempted...)
}

// newDrainService 造一份"接缝已替身、退订名单读真表"的服务。
func newDrainService(t *testing.T, database *gorm.DB, rec *recorder) *EmailSendService {
	t.Helper()
	svc := NewEmailSendService()
	svc.deliver = rec.deliver
	svc.SetEmailUnsubscribeRepository(repository.NewEmailUnsubscribeRepository(database))
	return svc
}

func createPendingSend(t *testing.T, database *gorm.DB, to string, sendTime *time.Time) string {
	t.Helper()
	row := &model.EmailSend{To: to, Subject: "排水", Content: "内容", Status: model.EmailStatusPending, SendTime: sendTime}
	if err := database.Create(row).Error; err != nil {
		t.Fatalf("创建 pending 邮件失败 %s: %v", to, err)
	}
	return row.ID
}

func readSendStatus(t *testing.T, database *gorm.DB, id string) int {
	t.Helper()
	var row model.EmailSend
	if err := database.Where("id = ?", id).First(&row).Error; err != nil {
		t.Fatalf("读回邮件 %s 失败: %v", id, err)
	}
	return row.Status
}

// TestProcessPendingEmails_DeliversDueRowsIncludingNullSendTime 到期即投，且 send_time 为 NULL 的行不算"没排期"。
func TestProcessPendingEmails_DeliversDueRowsIncludingNullSendTime(t *testing.T) {
	database := setupEmailDrainTestDB(t)
	rec := &recorder{}
	now := time.Now()
	idPast := createPendingSend(t, database, "past@example.com", ptr(now.Add(-time.Hour)))
	idNull := createPendingSend(t, database, "null@example.com", nil)
	idFuture := createPendingSend(t, database, "future@example.com", ptr(now.Add(time.Hour)))

	if err := newDrainService(t, database, rec).ProcessPendingEmails(context.Background()); err != nil {
		t.Fatalf("ProcessPendingEmails 失败: %v", err)
	}

	if got := fmt.Sprint(rec.called()); got != "[past@example.com null@example.com]" {
		t.Errorf("尝试投递 = %s，期望恰好两封到期邮件（NULL send_time 也要投）", got)
	}
	for id, want := range map[string]int{idPast: model.EmailStatusSent, idNull: model.EmailStatusSent,
		idFuture: model.EmailStatusPending} {
		if s := readSendStatus(t, database, id); s != want {
			t.Errorf("status(%s) = %d，期望 %d", id, s, want)
		}
	}
}

// TestProcessPendingEmails_ExpiresPendingOlderThanTTL 排期时刻早于龄上限的 pending 判过期，不补发。
func TestProcessPendingEmails_ExpiresPendingOlderThanTTL(t *testing.T) {
	database := setupEmailDrainTestDB(t)
	rec := &recorder{}
	now := time.Now()
	stale := now.Add(-2 * EmailPendingTTL)
	idOld := createPendingSend(t, database, "ancient@example.com", &stale)
	idFresh := createPendingSend(t, database, "recent@example.com", ptr(now.Add(-time.Hour)))

	if err := newDrainService(t, database, rec).ProcessPendingEmails(context.Background()); err != nil {
		t.Fatalf("ProcessPendingEmails 失败: %v", err)
	}

	if got := fmt.Sprint(rec.called()); got != "[recent@example.com]" {
		t.Errorf("尝试投递 = %s，期望只投未超龄的那封（超龄邮件不该在重启后突然群发出去）", got)
	}
	if s := readSendStatus(t, database, idOld); s != model.EmailStatusExpired {
		t.Errorf("超龄行 status = %d，期望 expired(%d)", s, model.EmailStatusExpired)
	}
	if s := readSendStatus(t, database, idFresh); s != model.EmailStatusSent {
		t.Errorf("未超龄行 status = %d，期望 sent(%d)", s, model.EmailStatusSent)
	}
}

// TestProcessPendingEmails_SkipsUnsubscribedRecipient 已退订的收件人不得被排水路径投出去。
//
// 即时发送那条腿一直有这道检查，排期这条腿没有 —— 于是"排到明早 9 点"恰好绕开了合规判据。
// 与同日 SMS 侧的口径一致：命中退订名单不是一次投递失败，但也不能记成投递成功。
func TestProcessPendingEmails_SkipsUnsubscribedRecipient(t *testing.T) {
	database := setupEmailDrainTestDB(t)
	rec := &recorder{}
	if err := database.Create(&model.EmailUnsubscribe{
		Email: "gone@example.com", UnsubscribedAt: time.Now(), SourceLink: "/api/email/unsubscribe",
	}).Error; err != nil {
		t.Fatalf("写入退订名单失败: %v", err)
	}
	idUnsub := createPendingSend(t, database, "gone@example.com", ptr(time.Now().Add(-time.Hour)))
	idOther := createPendingSend(t, database, "stay@example.com", ptr(time.Now().Add(-time.Hour)))

	if err := newDrainService(t, database, rec).ProcessPendingEmails(context.Background()); err != nil {
		t.Fatalf("ProcessPendingEmails 失败: %v", err)
	}

	if got := fmt.Sprint(rec.called()); got != "[stay@example.com]" {
		t.Errorf("尝试投递 = %s，期望退订地址根本没被尝试", got)
	}
	if s := readSendStatus(t, database, idUnsub); s == model.EmailStatusSent {
		t.Errorf("退订行被记成 sent ⇒ 台账上多出一条没发出去的邮件（id=%s）", idUnsub)
	}
	if s := readSendStatus(t, database, idUnsub); s != model.EmailStatusFailed {
		t.Errorf("退订行 status = %d，期望与即时发送同口径的 failed(%d)", s, model.EmailStatusFailed)
	}
	if s := readSendStatus(t, database, idOther); s != model.EmailStatusSent {
		t.Errorf("同批未退订行 status = %d，期望 sent(%d)", s, model.EmailStatusSent)
	}
}

// TestProcessPendingEmails_ReclaimsCrashedSendingRow 上一进程崩溃遗留的 sending 行要在本轮回捞并重投。
func TestProcessPendingEmails_ReclaimsCrashedSendingRow(t *testing.T) {
	database := setupEmailDrainTestDB(t)
	rec := &recorder{}
	now := time.Now()
	idCrashed := createPendingSend(t, database, "crashed@example.com", ptr(now.Add(-time.Hour)))
	// 模拟"认领之后进程就死了"：状态已被置为 sending，且认领时刻（updated_at）早于回捞窗口。
	if err := database.Model(&model.EmailSend{}).Where("id = ?", idCrashed).
		Update("status", model.EmailStatusSending).Error; err != nil {
		t.Fatalf("置 sending 失败: %v", err)
	}
	staleClaim := now.Add(-2 * EmailSendingStaleAfter)
	if err := database.Model(&model.EmailSend{}).Where("id = ?", idCrashed).
		UpdateColumn("updated_at", staleClaim).Error; err != nil {
		t.Fatalf("回拨认领时刻失败: %v", err)
	}

	if err := newDrainService(t, database, rec).ProcessPendingEmails(context.Background()); err != nil {
		t.Fatalf("ProcessPendingEmails 失败: %v", err)
	}

	if got := fmt.Sprint(rec.called()); got != "[crashed@example.com]" {
		t.Errorf("尝试投递 = %s，期望崩溃遗留的到期邮件本轮被回捞并重投", got)
	}
	if s := readSendStatus(t, database, idCrashed); s != model.EmailStatusSent {
		t.Errorf("回捞行 status = %d，期望 sent(%d)（不能永远停在 sending）", s, model.EmailStatusSent)
	}
}

// TestProcessPendingEmails_FailedDeliveryMarksFailedAndKeepsGoing 单封投递失败不得中断整轮，且要落诚实状态。
func TestProcessPendingEmails_FailedDeliveryMarksFailedAndKeepsGoing(t *testing.T) {
	database := setupEmailDrainTestDB(t)
	rec := &recorder{err: fmt.Errorf("smtp 连接失败")}
	idA := createPendingSend(t, database, "a@example.com", ptr(time.Now().Add(-time.Hour)))
	idB := createPendingSend(t, database, "b@example.com", nil)

	if err := newDrainService(t, database, rec).ProcessPendingEmails(context.Background()); err != nil {
		t.Fatalf("ProcessPendingEmails 失败: %v", err)
	}
	if len(rec.called()) != 2 {
		t.Errorf("尝试投递 %d 封，期望两封都尝试（第一封失败不该中断整轮）", len(rec.called()))
	}
	for _, id := range []string{idA, idB} {
		if s := readSendStatus(t, database, id); s != model.EmailStatusFailed {
			t.Errorf("投递失败行 status(%s) = %d，期望 failed(%d)", id, s, model.EmailStatusFailed)
		}
	}
}

// TestProcessPendingEmails_ClaimIsExclusive 同一轮内两把"排水"（模拟两个副本）不重叠。
//
// 认领必须把行推出 pending，否则第二个执行体会把同一批再投一遍 —— 收件人视角就是重复邮件。
func TestProcessPendingEmails_ClaimIsExclusive(t *testing.T) {
	database := setupEmailDrainTestDB(t)
	rec := &recorder{}
	for i := 0; i < 3; i++ {
		createPendingSend(t, database, fmt.Sprintf("dup%d@example.com", i), ptr(time.Now().Add(-time.Hour)))
	}
	first := newDrainService(t, database, rec)
	second := newDrainService(t, database, rec)

	if err := first.ProcessPendingEmails(context.Background()); err != nil {
		t.Fatalf("第一把排水失败: %v", err)
	}
	afterFirst := len(rec.called())
	if err := second.ProcessPendingEmails(context.Background()); err != nil {
		t.Fatalf("第二把排水失败: %v", err)
	}
	if afterFirst != 3 {
		t.Fatalf("第一把尝试投递 %d 封，期望 3 封", afterFirst)
	}
	if len(rec.called()) != 3 {
		t.Errorf("第二把又尝试了 %d 封 ⇒ 认领没把行推出 pending，两副本会双投", len(rec.called())-afterFirst)
	}
}

func ptr(v time.Time) *time.Time { return &v }
