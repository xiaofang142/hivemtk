// email_drain_test.go 邮件出站排水的仓库契约（R21）。
//
// 这三条判据各自堵一个真实的坑，缺一个都会退回原病灶：
//   - COALESCE(send_time, created_at)：send_time 为 NULL 的行（DTO 允许 immediateSend
//     与 sendTime 都不给）在三值逻辑下永不匹配 `send_time <= now`，会永远停在 pending；
//   - FOR UPDATE SKIP LOCKED 认领：多副本部署下两个进程抢同一批到期行 = 收件人收到重复邮件；
//   - TTL 判 expired / 崩溃遗留回捞：前者防"把几周前排的邮件突然群发出去"，
//     后者防"进程在投递中途崩溃 ⇒ 行永久卡在 sending"。
package repository

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// createEmailSend 用独立零值 struct 落一行（复用已填充 struct 再写会把旧字段带进 WHERE）。
func createEmailSend(t *testing.T, database *gorm.DB, to string, status int, sendTime *time.Time, createdAt time.Time) string {
	t.Helper()
	row := &model.EmailSend{
		To:        to,
		Subject:   "排水用例 " + to,
		Content:   "内容",
		Status:    status,
		SendTime:  sendTime,
		CreatedAt: createdAt,
	}
	if err := database.Create(row).Error; err != nil {
		t.Fatalf("创建 email_sends 行失败 %s: %v", to, err)
	}
	return row.ID
}

func emailSendStatus(t *testing.T, database *gorm.DB, id string) int {
	t.Helper()
	var row model.EmailSend
	if err := database.Where("id = ?", id).First(&row).Error; err != nil {
		t.Fatalf("读回 email_sends %s 失败: %v", id, err)
	}
	return row.Status
}

func ptrTime(v time.Time) *time.Time { return &v }

// TestEmailDrain_ClaimDueIncludesNullSendTime 认领必须覆盖 send_time 为 NULL 的到期行。
func TestEmailDrain_ClaimDueIncludesNullSendTime(t *testing.T) {
	database := setupEmailSendTestDB(t)
	repo := NewEmailSendRepository()
	ctx := context.Background()
	now := time.Now()

	idPast := createEmailSend(t, database, "drain-past@example.com", model.EmailStatusPending, ptrTime(now.Add(-time.Hour)), now)
	idNull := createEmailSend(t, database, "drain-null@example.com", model.EmailStatusPending, nil, now)
	idFuture := createEmailSend(t, database, "drain-future@example.com", model.EmailStatusPending, ptrTime(now.Add(time.Hour)), now)
	idSent := createEmailSend(t, database, "drain-sent@example.com", model.EmailStatusSent, ptrTime(now.Add(-time.Hour)), now)

	picked, err := repo.ClaimDueForUpdate(ctx, now, 10)
	if err != nil {
		t.Fatalf("ClaimDueForUpdate 失败: %v", err)
	}
	got := map[string]bool{}
	for _, e := range picked {
		got[e.ID] = true
	}
	if len(picked) != 2 || !got[idPast] || !got[idNull] {
		t.Errorf("认领结果 = %v，期望恰好 {past, null-send-time} 两条", picked)
	}
	if got[idFuture] {
		t.Error("未到期的行被认领了")
	}
	if got[idSent] {
		t.Error("非 pending 的行被认领了")
	}
	for id, want := range map[string]int{idPast: model.EmailStatusSending, idNull: model.EmailStatusSending,
		idFuture: model.EmailStatusPending, idSent: model.EmailStatusSent} {
		if s := emailSendStatus(t, database, id); s != want {
			t.Errorf("认领后 status(%s) = %d，期望 %d", id, s, want)
		}
	}
}

// TestEmailDrain_ExpireStalePendingByAge 超过 TTL 的 pending 判 expired，NULL send_time 按 created_at 计龄。
func TestEmailDrain_ExpireStalePendingByAge(t *testing.T) {
	database := setupEmailSendTestDB(t)
	repo := NewEmailSendRepository()
	ctx := context.Background()
	now := time.Now()
	ttl := 24 * time.Hour

	idOldSendTime := createEmailSend(t, database, "stale-sendtime@example.com", model.EmailStatusPending,
		ptrTime(now.Add(-48*time.Hour)), now)
	idOldNull := createEmailSend(t, database, "stale-null@example.com", model.EmailStatusPending,
		nil, now.Add(-48*time.Hour))
	idFresh := createEmailSend(t, database, "fresh@example.com", model.EmailStatusPending,
		ptrTime(now.Add(-time.Hour)), now)
	idFreshNull := createEmailSend(t, database, "fresh-null@example.com", model.EmailStatusPending, nil, now)
	idOldButSent := createEmailSend(t, database, "old-sent@example.com", model.EmailStatusSent,
		ptrTime(now.Add(-48*time.Hour)), now)

	n, err := repo.ExpireStalePending(ctx, now.Add(-ttl))
	if err != nil {
		t.Fatalf("ExpireStalePending 失败: %v", err)
	}
	if n != 2 {
		t.Errorf("过期行数 = %d，期望 2", n)
	}
	for id, want := range map[string]int{idOldSendTime: model.EmailStatusExpired, idOldNull: model.EmailStatusExpired,
		idFresh: model.EmailStatusPending, idFreshNull: model.EmailStatusPending, idOldButSent: model.EmailStatusSent} {
		if s := emailSendStatus(t, database, id); s != want {
			t.Errorf("过期判定后 status(%s) = %d，期望 %d", id, s, want)
		}
	}
}

// TestEmailDrain_ReclaimStaleSending 崩溃遗留的 sending 行回捞为 pending，新鲜认领不动。
func TestEmailDrain_ReclaimStaleSending(t *testing.T) {
	database := setupEmailSendTestDB(t)
	repo := NewEmailSendRepository()
	ctx := context.Background()
	now := time.Now()

	idStale := createEmailSend(t, database, "stale-sending@example.com", model.EmailStatusSending, ptrTime(now.Add(-time.Hour)), now)
	idFresh := createEmailSend(t, database, "fresh-sending@example.com", model.EmailStatusSending, ptrTime(now.Add(-time.Hour)), now)
	// UpdateColumn 跳过 GORM 的自动时间戳，否则写不进去这个"很久以前认领的"时刻。
	for _, id := range []string{idStale, idFresh} {
		err := database.Model(&model.EmailSend{}).Where("id = ?", id).
			UpdateColumn("updated_at", now.Add(-40*time.Minute)).Error
		if err != nil {
			t.Fatalf("置旧 updated_at 失败 %s: %v", id, err)
		}
	}
	if err := database.Model(&model.EmailSend{}).Where("id = ?", idFresh).
		UpdateColumn("updated_at", now).Error; err != nil {
		t.Fatalf("刷新 updated_at 失败: %v", err)
	}

	n, err := repo.ReclaimStaleSending(ctx, now.Add(-30*time.Minute))
	if err != nil {
		t.Fatalf("ReclaimStaleSending 失败: %v", err)
	}
	if n != 1 {
		t.Errorf("回捞行数 = %d，期望 1", n)
	}
	if s := emailSendStatus(t, database, idStale); s != model.EmailStatusPending {
		t.Errorf("崩溃遗留行 status = %d，期望回到 pending(%d)", s, model.EmailStatusPending)
	}
	if s := emailSendStatus(t, database, idFresh); s != model.EmailStatusSending {
		t.Errorf("新鲜认领行 status = %d，期望仍是 sending(%d)", s, model.EmailStatusSending)
	}
}

// TestEmailDrain_ClaimSkipsRowsLockedByAnotherInstance 另一会话持锁时本轮跳过且不阻塞。
//
// 这条用例判的是"多副本不会互相等锁"，而 SKIP LOCKED 的失败模式不是报错而是**卡住**：
// 摘掉 SKIP LOCKED 后本用例会一直挂到超时，故这里显式把挂住当成红因报出来。
func TestEmailDrain_ClaimSkipsRowsLockedByAnotherInstance(t *testing.T) {
	database := setupEmailSendTestDB(t)
	repo := NewEmailSendRepository()
	ctx := context.Background()
	now := time.Now()
	id := createEmailSend(t, database, "locked@example.com", model.EmailStatusPending, ptrTime(now.Add(-time.Hour)), now)

	other := database.Begin()
	if other.Error != nil {
		t.Fatalf("开启第二会话失败: %v", other.Error)
	}
	defer other.Rollback()
	var held []model.EmailSend
	if err := other.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).Find(&held).Error; err != nil {
		t.Fatalf("第二会话加锁失败: %v", err)
	}
	if len(held) != 1 {
		t.Fatalf("第二会话应锁住 1 行，实际 %d", len(held))
	}

	type result struct {
		picked []*model.EmailSend
		err    error
	}
	done := make(chan result, 1)
	go func() {
		picked, err := repo.ClaimDueForUpdate(ctx, now, 10)
		done <- result{picked, err}
	}()

	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("ClaimDueForUpdate 失败: %v", r.err)
		}
		if len(r.picked) != 0 {
			t.Errorf("被他人锁住的行仍被认领（%d 条）⇒ 同一封邮件会投两次", len(r.picked))
		}
	case <-time.After(10 * time.Second):
		t.Fatal("认领被他人持锁的行时阻塞住了 ⇒ 缺 SKIP LOCKED：多副本部署会互相等锁")
	}

	if s := emailSendStatus(t, database, id); s != model.EmailStatusPending {
		t.Errorf("跳过认领后该行 status = %d，期望仍是 pending(%d)", s, model.EmailStatusPending)
	}
}
