package repository

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

type assignmentWriterKey struct{}

func assignmentHistoryDB(t *testing.T) (*gorm.DB, context.Context, *model.InboxConversation) {
	t.Helper()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() {
		tenant.ResetRegistryForTest()
		tenant.SetMode(tenant.ModeOff)
	})
	db := testutil.NewTestDB(t, &model.InboxConversation{}, &model.InboxAssignment{})
	if db == nil {
		t.Fatal("分配历史测试必须连接专用测试库")
	}
	ctx := tenant.WithTenantID(context.Background(), 731)
	conv := &model.InboxConversation{
		Platform: "web", AccountID: "assignment-history", CustomerID: "customer",
		Status: "assigned", AssignedTo: "owner-0",
	}
	if err := db.WithContext(ctx).Create(conv).Error; err != nil {
		t.Fatalf("创建会话失败: %v", err)
	}
	return db, ctx, conv
}

func TestInboxAssignTx_ConcurrentHistoryChain(t *testing.T) {
	db, baseCtx, conv := assignmentHistoryDB(t)
	ctx, cancel := context.WithTimeout(baseCtx, 10*time.Second)
	defer cancel()
	firstRead, secondRead := make(chan struct{}), make(chan struct{})
	firstRelease, secondRelease := make(chan struct{}), make(chan struct{})
	var releaseFirst, releaseSecond sync.Once
	defer releaseFirst.Do(func() { close(firstRelease) })
	defer releaseSecond.Do(func() { close(secondRelease) })
	secondPID := make(chan int, 1)

	// 在同一事务连接取得 PID，只用于观察第二个读取是否已实际等待行锁。
	if err := db.Callback().Query().Before("gorm:query").Register("test:assignment_pid", func(tx *gorm.DB) {
		if tx.Statement.Context.Value(assignmentWriterKey{}) != 2 {
			return
		}
		var pid int
		if err := tx.Session(&gorm.Session{NewDB: true}).Raw("SELECT pg_backend_pid()").Scan(&pid).Error; err != nil {
			tx.AddError(err)
			return
		}
		secondPID <- pid
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Query().After("gorm:query").Register("test:assignment_read", func(tx *gorm.DB) {
		if tx.Error != nil {
			return
		}
		var release <-chan struct{}
		switch tx.Statement.Context.Value(assignmentWriterKey{}) {
		case 1:
			close(firstRead)
			release = firstRelease
		case 2:
			close(secondRead)
			release = secondRelease
		default:
			return
		}
		select {
		case <-release:
		case <-ctx.Done():
			tx.AddError(ctx.Err())
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Callback().Query().Remove("test:assignment_pid")
		db.Callback().Query().Remove("test:assignment_read")
	})

	repo := NewInboxConversationRepositoryWithDB(db)
	firstDone, secondDone := make(chan error, 1), make(chan error, 1)
	assign := func(writer int, owner string, done chan<- error) {
		_, err := repo.AssignTx(context.WithValue(ctx, assignmentWriterKey{}, writer), AssignTxInput{
			ConversationID: conv.ID, Action: "reassign", ToType: "human", ToUserID: owner,
		})
		done <- err
	}
	go assign(1, "owner-1", firstDone)
	select {
	case <-firstRead:
	case <-ctx.Done():
		t.Fatal("第一事务没有完成读取")
	}
	go assign(2, "owner-2", secondDone)
	var pid int
	select {
	case pid = <-secondPID:
	case <-ctx.Done():
		t.Fatal("第二事务没有开始读取")
	}

	// 旧实现会完成第二次读取；修复后第二次读取必须等待第一事务持有的锁。
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	observed := false
	for !observed {
		select {
		case <-secondRead:
			observed = true
		case <-ticker.C:
			var waits int64
			if err := db.WithContext(tenant.WithSystemScope(ctx)).Raw(
				"SELECT COUNT(*) FROM pg_stat_activity WHERE pid = ? AND wait_event_type = 'Lock'", pid,
			).Scan(&waits).Error; err != nil {
				t.Fatalf("观察数据库锁等待失败: %v", err)
			}
			observed = waits == 1
		case <-ctx.Done():
			t.Fatal("第二事务既未读取也未进入数据库锁等待")
		}
	}
	releaseFirst.Do(func() { close(firstRelease) })
	if err := <-firstDone; err != nil {
		t.Fatalf("第一事务失败: %v", err)
	}
	releaseSecond.Do(func() { close(secondRelease) })
	if err := <-secondDone; err != nil {
		t.Fatalf("第二事务失败: %v", err)
	}

	var history []model.InboxAssignment
	if err := db.WithContext(baseCtx).Where("conversation_id = ?", conv.ID).Order("id").Find(&history).Error; err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 {
		t.Fatalf("期望两条已提交历史，实际 %d", len(history))
	}
	if history[0].FromUserID != "owner-0" || history[1].FromUserID != history[0].ToUserID {
		t.Fatalf("并发分配历史断裂: %s→%s, %s→%s", history[0].FromUserID, history[0].ToUserID, history[1].FromUserID, history[1].ToUserID)
	}
	var after model.InboxConversation
	if err := db.WithContext(baseCtx).First(&after, conv.ID).Error; err != nil {
		t.Fatal(err)
	}
	if after.AssignedTo != history[1].ToUserID {
		t.Fatalf("最终归属 %s 与最后历史 %s 不一致", after.AssignedTo, history[1].ToUserID)
	}
}

func TestInboxAssignTx_HistoryFailureRollsBack(t *testing.T) {
	db, ctx, conv := assignmentHistoryDB(t)
	failure := errors.New("测试历史插入失败")
	if err := db.Callback().Create().Before("gorm:create").Register("test:assignment_insert_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "inbox_assignments" {
			tx.AddError(failure)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Callback().Create().Remove("test:assignment_insert_failure") })
	out, err := NewInboxConversationRepositoryWithDB(db).AssignTx(ctx, AssignTxInput{
		ConversationID: conv.ID, Action: "reassign", ToType: "human", ToUserID: "owner-1",
	})
	if !errors.Is(err, failure) || out != nil {
		t.Fatalf("历史失败应返回原错误且没有成功输出: out=%+v err=%v", out, err)
	}
	var after model.InboxConversation
	if err := db.WithContext(ctx).First(&after, conv.ID).Error; err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.WithContext(ctx).Model(&model.InboxAssignment{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if after.AssignedTo != "owner-0" || count != 0 {
		t.Fatalf("回滚未保留原归属: owner=%s history=%d", after.AssignedTo, count)
	}
}

func TestInboxAssignTx_ReadCancellationPreserved(t *testing.T) {
	db, baseCtx, conv := assignmentHistoryDB(t)
	ctx, cancel := context.WithCancel(baseCtx)
	defer cancel()
	if err := db.Callback().Query().Before("gorm:query").Register("test:assignment_cancel", func(tx *gorm.DB) {
		cancel()
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Callback().Query().Remove("test:assignment_cancel") })
	_, err := NewInboxConversationRepositoryWithDB(db).AssignTx(ctx, AssignTxInput{
		ConversationID: conv.ID, Action: "reassign", ToType: "human", ToUserID: "owner-1",
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("读取取消不应被改写为会话不存在，实际: %v", err)
	}
}

func TestInboxAssignTx_TenantIsolation(t *testing.T) {
	db, ctx, conv := assignmentHistoryDB(t)
	repo := NewInboxConversationRepositoryWithDB(db)
	for _, tc := range []struct {
		name string
		ctx  context.Context
		id   uint
	}{
		{"跨租户", tenant.WithTenantID(context.Background(), 732), conv.ID},
		{"缺租户", context.Background(), conv.ID},
		{"不存在", ctx, conv.ID + 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := repo.AssignTx(tc.ctx, AssignTxInput{ConversationID: tc.id, Action: "assign", ToType: "human", ToUserID: "intruder"})
			if err == nil || out != nil {
				t.Fatalf("不可见会话不能修改: out=%+v err=%v", out, err)
			}
			if tc.name == "不存在" && err.Error() != "conversation not found" {
				t.Fatalf("真正不存在应保留兼容错误: %v", err)
			}
		})
	}
	var after model.InboxConversation
	if err := db.WithContext(ctx).First(&after, conv.ID).Error; err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.WithContext(ctx).Model(&model.InboxAssignment{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if after.AssignedTo != "owner-0" || count != 0 {
		t.Fatalf("拒绝的请求产生了副作用: owner=%s history=%d", after.AssignedTo, count)
	}
}
