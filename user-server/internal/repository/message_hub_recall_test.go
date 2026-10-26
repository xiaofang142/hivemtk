package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// B2 撤回状态格的仓储判据：
//
//	① MarkRecalled 把 recalled_at 写进去、把 recall_error 清空；
//	② 第二次 MarkRecalled 返回 wrote=false（幂等：并发/重复请求不得都当成功写入）；
//	③ 入站行不可被置为已撤回（where direction='outbound' 兜住）；
//	④ MarkRecallFailed 只写原因、不动 recalled_at；
//	⑤ 已撤回的行不会被失败原因覆盖（终态不回翻，与 UpdateDeliveryStatus 的乱序保护同一条取舍）；
//	⑥ GetByIDForRecall 取到的行足以驱动撤回（platform/msg_id/sent_at/recalled_at 都在）。
func TestMessageHubRecallRepository(t *testing.T) {
	database := setupMessageHubTestDB(t)
	repo := NewMessageHubRepositoryWithDB(database)
	ctx := context.Background()

	seed := func(msgID, direction string) *model.MessageHub {
		row := newHub("telegram", "acc1", msgID, direction, "text", "hi", "sent")
		row.SentAt = time.Now()
		if err := database.Create(row).Error; err != nil {
			t.Fatalf("seed %s: %v", msgID, err)
		}
		return row
	}

	t.Run("① 置终态并清空失败原因", func(t *testing.T) {
		row := seed("mh-recall-ok", "outbound")
		// 先留一句旧失败原因，成功时必须被清掉（"上一条失败"不该挂在已撤回的行上）
		if err := repo.MarkRecallFailed(ctx, row.ID, "旧失败"); err != nil {
			t.Fatalf("预置失败原因: %v", err)
		}
		at := time.Now().Truncate(time.Millisecond)
		wrote, err := repo.MarkRecalled(ctx, row.ID, at)
		if err != nil || !wrote {
			t.Fatalf("MarkRecalled wrote=%v err=%v，want true/nil", wrote, err)
		}
		var got model.MessageHub
		if err := database.First(&got, row.ID).Error; err != nil {
			t.Fatalf("回读: %v", err)
		}
		if got.RecalledAt == nil {
			t.Fatalf("recalled_at 未写入")
		}
		if got.RecallError != "" {
			t.Fatalf("recall_error 未清空，got %q", got.RecallError)
		}

		// ② 幂等：第二次不许再写成"这一笔是你写的"
		wrote2, err := repo.MarkRecalled(ctx, row.ID, time.Now())
		if err != nil {
			t.Fatalf("第二次 MarkRecalled: %v", err)
		}
		if wrote2 {
			t.Fatalf("② 第二次 MarkRecalled 仍报 wrote=true：并发两笔都会被当成首次撤回")
		}
	})

	t.Run("③ 入站行不可被置为已撤回", func(t *testing.T) {
		row := seed("mh-recall-in", "inbound")
		wrote, err := repo.MarkRecalled(ctx, row.ID, time.Now())
		if err != nil {
			t.Fatalf("MarkRecalled(inbound): %v", err)
		}
		if wrote {
			t.Fatalf("③ 入站行被置为已撤回")
		}
	})

	t.Run("④ 失败原因单独落格、不动终态", func(t *testing.T) {
		row := seed("mh-recall-fail", "outbound")
		if err := repo.MarkRecallFailed(ctx, row.ID, "窗口过期"); err != nil {
			t.Fatalf("MarkRecallFailed: %v", err)
		}
		var got model.MessageHub
		if err := database.First(&got, row.ID).Error; err != nil {
			t.Fatalf("回读: %v", err)
		}
		if got.RecallError != "窗口过期" {
			t.Fatalf("recall_error = %q, want 窗口过期", got.RecallError)
		}
		if got.RecalledAt != nil {
			t.Fatalf("④ 失败被写成了终态（recalled_at 非空）")
		}
	})

	t.Run("⑤ 终态不被失败原因覆盖", func(t *testing.T) {
		row := seed("mh-recall-terminal", "outbound")
		if _, err := repo.MarkRecalled(ctx, row.ID, time.Now()); err != nil {
			t.Fatalf("MarkRecalled: %v", err)
		}
		if err := repo.MarkRecallFailed(ctx, row.ID, "迟到的失败"); err != nil {
			t.Fatalf("MarkRecallFailed: %v", err)
		}
		var got model.MessageHub
		if err := database.First(&got, row.ID).Error; err != nil {
			t.Fatalf("回读: %v", err)
		}
		if got.RecallError != "" {
			t.Fatalf("⑤ 已撤回的行被写上了失败原因 %q", got.RecallError)
		}
	})

	t.Run("⑥ 撤回读口带齐判定所需列", func(t *testing.T) {
		row := seed("mh-recall-select", "outbound")
		got, err := repo.GetByIDForRecall(ctx, row.ID)
		if err != nil {
			t.Fatalf("GetByIDForRecall: %v", err)
		}
		if got.Platform != "telegram" || got.MsgID != "mh-recall-select" || got.Direction != "outbound" {
			t.Fatalf("⑥ 判定列缺失: platform=%q msg_id=%q direction=%q", got.Platform, got.MsgID, got.Direction)
		}
		if got.SentAt.IsZero() {
			t.Fatalf("⑥ sent_at 未取到：撤回窗口判不了")
		}
		if _, err := repo.GetByIDForRecall(ctx, 99999999); !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("⑥ 不存在的行应回 ErrRecordNotFound, got %v", err)
		}
	})
}
