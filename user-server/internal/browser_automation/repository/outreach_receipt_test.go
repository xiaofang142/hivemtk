package repository

// 契约锁：回执表是交付物不是闸门。
// 去重表用四元组唯一键 + OnConflict{DoNothing}（重复行=重复打扰，必须吞）；
// 回执表刻意没有唯一键（重复行=重复留证，必须都留）——
// 一旦在这里也吞掉，「内容已发出但列表里没有回执」与「这步根本没执行过」就再也分不开，
// 而回执的唯一用途就是回答这个区分。
import (
	"context"
	"testing"

	"hivemtk-user/internal/browser_automation/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func receiptRepo(t *testing.T) (BrowserOutreachReceiptRepository, *gorm.DB) {
	t.Helper()
	db := testutil.NewTestDB(t, &model.BrowserOutreachReceipt{}, &model.BrowserTask{}, &model.BrowserSession{})
	if db == nil {
		t.Fatal("测试库不可达：回执行不落库这件事判不了（不 Skip）")
	}
	return NewBrowserOutreachReceiptRepositoryWithDB(db), db
}

func seedReceiptOwner(t *testing.T, db *gorm.DB, userID uint) (taskID, sessionID uint) {
	t.Helper()
	ctx := context.Background()
	task := &model.BrowserTask{Name: "回执归属", TaskType: "one_shot", Status: "ready",
		Url: "https://www.xiaohongshu.com/explore", Platform: "xiaohongshu", UserID: userID, LoopCount: 1, TimeoutSec: 60}
	if err := db.WithContext(ctx).Create(task).Error; err != nil {
		t.Fatalf("任务落库失败: %v", err)
	}
	sess := &model.BrowserSession{TaskID: task.ID, UserID: userID, Status: "completed"}
	if err := db.WithContext(ctx).Create(sess).Error; err != nil {
		t.Fatalf("会话落库失败: %v", err)
	}
	return task.ID, sess.ID
}

// 同四元组重复落库必须都留下（这是与去重表的核心差异）。
func TestRecordOutreachReceiptKeepsDuplicates(t *testing.T) {
	repo, db := receiptRepo(t)
	ctx := context.Background()
	taskID, sessionID := seedReceiptOwner(t, db, 7701)

	for i := 0; i < 3; i++ {
		if err := repo.RecordOutreachReceipt(ctx, &model.BrowserOutreachReceipt{
			TaskID: taskID, SessionID: sessionID, Platform: "xiaohongshu", Action: "post_comment",
			TargetURL: "https://www.xiaohongshu.com/explore/abc", CopyHash: "same-hash",
			CopySnapshot: "测试评论正文", Verified: i%2 == 0,
		}); err != nil {
			t.Fatalf("第 %d 次落回执失败（回执表不该有唯一键约束）: %v", i+1, err)
		}
	}
	list, err := repo.ListByTaskID(ctx, taskID, 0, 50)
	if err != nil {
		t.Fatalf("列出回执失败: %v", err)
	}
	if len(list) != 3 {
		t.Errorf("同四元组回执行数=%d want 3（吞掉重复会让「已发出但无回执」不可区分）", len(list))
	}
}

// 归属过滤：别人的任务读不到回执（回执行没有 user_id 列，归属经 task/session 落到归属表上判）。
func TestListOutreachReceiptsEnforcesOwnership(t *testing.T) {
	repo, db := receiptRepo(t)
	ctx := context.Background()
	ownerTask, ownerSess := seedReceiptOwner(t, db, 7702)
	if err := repo.RecordOutreachReceipt(ctx, &model.BrowserOutreachReceipt{
		TaskID: ownerTask, SessionID: ownerSess, Platform: "xiaohongshu", Action: "post_comment",
		TargetURL: "https://www.xiaohongshu.com/explore/xyz", CopyHash: "h-1", CopySnapshot: "私密文案",
		Verified: true,
	}); err != nil {
		t.Fatalf("落回执失败: %v", err)
	}

	own, err := repo.ListByTaskID(ctx, ownerTask, 7702, 50)
	if err != nil {
		t.Fatalf("owner 读取失败: %v", err)
	}
	if len(own) != 1 {
		t.Errorf("owner 应读到 1 行，实得 %d", len(own))
	}
	other, err := repo.ListByTaskID(ctx, ownerTask, 9999, 50)
	if err != nil {
		t.Fatalf("非 owner 读取应返回空而非报错: %v", err)
	}
	if len(other) != 0 {
		t.Errorf("非 owner 读到了 %d 行回执（文案与截图链接会泄露）", len(other))
	}
	otherSess, err := repo.ListBySessionID(ctx, ownerSess, 9999)
	if err != nil {
		t.Fatalf("非 owner 会话级读取应返回空而非报错: %v", err)
	}
	if len(otherSess) != 0 {
		t.Errorf("非 owner 读到了 %d 行会话级回执", len(otherSess))
	}
}

// 列表按时间倒序（面板读最新几条）+ limit 钳位（防一条任务刷出上万行）。
func TestListOutreachReceiptsOrderAndLimit(t *testing.T) {
	repo, db := receiptRepo(t)
	ctx := context.Background()
	taskID, sessionID := seedReceiptOwner(t, db, 7703)

	for i := 0; i < 3; i++ {
		if err := repo.RecordOutreachReceipt(ctx, &model.BrowserOutreachReceipt{
			TaskID: taskID, SessionID: sessionID, Platform: "xiaohongshu", Action: "post_comment",
			TargetURL: "https://www.xiaohongshu.com/explore/p" + string(rune('a'+i)), CopyHash: "h" + string(rune('a'+i)),
		}); err != nil {
			t.Fatalf("落回执失败: %v", err)
		}
	}
	list, err := repo.ListByTaskID(ctx, taskID, 0, 2)
	if err != nil {
		t.Fatalf("列出回执失败: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("limit=2 应返回 2 行，实得 %d", len(list))
	}
	if list[0].ID <= list[1].ID {
		t.Errorf("回执应按 id 倒序（新到旧），实得 %d <= %d", list[0].ID, list[1].ID)
	}
	// 超界 limit 回落缺省 50 而不是报错或返回全量
	big, err := repo.ListByTaskID(ctx, taskID, 0, 9999)
	if err != nil {
		t.Fatalf("超界 limit 应回落缺省而非报错: %v", err)
	}
	if len(big) != 3 {
		t.Errorf("超界 limit 应回落 50（实得 %d 行）", len(big))
	}
}

// 未装配仓储：写报明确错误，读返回错误（不静默假装「没有回执」）。
func TestOutreachReceiptRepoNilDB(t *testing.T) {
	r := NewBrowserOutreachReceiptRepositoryWithDB(nil)
	if err := r.RecordOutreachReceipt(context.Background(), &model.BrowserOutreachReceipt{}); err == nil {
		t.Error("未装配仓储写入应报错（否则接线缺失会静默吞掉整条验收链）")
	}
	if _, err := r.ListByTaskID(context.Background(), 1, 0, 10); err == nil {
		t.Error("未装配仓储读取应报错")
	}
}
