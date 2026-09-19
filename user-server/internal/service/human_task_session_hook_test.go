// human_task_session_hook_test.go T-P3-03 的收口半边：会话结束了，待办不能留在池子里。
//
// 为什么这一条必须测到 UpdateSessionStatus 而不是只测服务方法：
// CancelOpenBySubject 本身对不对，服务层的用例已经锁死了；这里要证的是
// "会话关闭那一刻**真的有人去叫它**"。只测服务方法的话，钩子没挂上也能全绿，
// 而池子里就会长期留着"会话早就结束、待办还挂着"的行 —— 那正是坐席收件箱
// 最招人恨的形状，也是本卡相对"只做一张表"的全部增量。
package service

import (
	"context"
	"testing"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
)

// useGlobalHumanTaskSvc 把服务登记成全局实例，用例结束自动撤掉。
// 不撤的话，后面那些"未装配"的用例会看到上一份残留（全局指针是整个包共享的）。
func useGlobalHumanTaskSvc(t *testing.T, svc *HumanTaskService) {
	t.Helper()
	prev := GlobalHumanTaskService()
	SetGlobalHumanTaskService(svc)
	t.Cleanup(func() { SetGlobalHumanTaskService(prev) })
}

func handoffTaskOf(t *testing.T, database *gorm.DB, subjectID string) *model.HumanTask {
	t.Helper()
	var rows []*model.HumanTask
	if err := database.Where("subject_id = ?", subjectID).Find(&rows).Error; err != nil {
		t.Fatalf("读待办失败: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("%s 的待办 %d 条，期望 1 条", subjectID, len(rows))
	}
	return rows[0]
}

// 主用例：转人工投出待办 → 会话被置为 resolved → 那一条必须落 cancelled。
func TestUpdateSessionStatus_CancelsOpenHandoffTask(t *testing.T) {
	database := newHandoffProduceDB(t)
	ctx := context.Background()
	useGlobalHumanTaskSvc(t, newHumanTaskSvc(t, database, &stubHumanTaskCfg{value: 5}))

	session := seedHandoffSession(t, database, "sess_hook_close", "oneid_hook_1")
	if err := newHandoffOrchestrator(t, database).transferToHuman(ctx, session, "客户要求人工"); err != nil {
		t.Fatalf("转人工失败: %v", err)
	}

	svc := NewCustomerSessionServiceWithDB(database)
	if err := svc.UpdateSessionStatus(ctx, session.ID, model.SessionStatusResolved); err != nil {
		t.Fatalf("置会话 resolved 失败: %v", err)
	}

	task := handoffTaskOf(t, database, "sess_hook_close")
	if task.Status != model.HumanTaskStatusCancelled {
		t.Errorf("会话结束后待办仍是 %q，期望 cancelled（坐席收件箱里会留着一条没有会话可看的行）", task.Status)
	}
	if task.CancelReason == "" {
		t.Error("cancel_reason 为空 ⇒ 事后无法回答\"这条为什么没人做完就结束了\"")
	}
	if task.CancelledAt == nil {
		t.Error("cancelled_at 为空")
	}
	// 撤销不许顺手改掉认领人：那一列的语义是"处理过这件事的人"，工作量指标读它。
	if task.AssigneeUserID != "" {
		t.Errorf("assignee_user_id = %q，期望仍为空", task.AssigneeUserID)
	}
}

// 同一条会话上的第二次转人工（上一轮已处理完）会留下第二条待办：关闭会话时
// 只有**开放**那条该被撤销，已 done 的那条必须原样留着复盘。
func TestUpdateSessionStatus_KeepsTerminalTaskAlone(t *testing.T) {
	database := newHandoffProduceDB(t)
	ctx := context.Background()
	ht := newHumanTaskSvc(t, database, &stubHumanTaskCfg{value: 5})
	useGlobalHumanTaskSvc(t, ht)

	session := seedHandoffSession(t, database, "sess_hook_done", "oneid_hook_2")
	if err := newHandoffOrchestrator(t, database).transferToHuman(ctx, session, "第一次"); err != nil {
		t.Fatalf("转人工失败: %v", err)
	}
	first := handoffTaskOf(t, database, "sess_hook_done")
	if _, err := ht.Complete(ctx, first.ID, "agent-1"); err != nil {
		t.Fatalf("完成第一条失败: %v", err)
	}
	// 完成后再转一次 ⇒ 第二条开放待办（终态那条不占唯一索引的空位）。
	session.Status = model.SessionStatusAIHandling
	if err := newHandoffOrchestrator(t, database).transferToHuman(ctx, session, "第二次"); err != nil {
		t.Fatalf("第二次转人工失败: %v", err)
	}

	if err := NewCustomerSessionServiceWithDB(database).
		UpdateSessionStatus(ctx, session.ID, model.SessionStatusClosed); err != nil {
		t.Fatalf("置会话 closed 失败: %v", err)
	}

	var rows []*model.HumanTask
	if err := database.Where("subject_id = ?", "sess_hook_done").Order("id asc").Find(&rows).Error; err != nil {
		t.Fatalf("读待办失败: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("共 %d 条，期望 2 条", len(rows))
	}
	if rows[0].Status != model.HumanTaskStatusDone {
		t.Errorf("已完成的待办被改成了 %q ⇒ 坐席工作量与复盘记录被系统撤销覆盖", rows[0].Status)
	}
	if rows[1].Status != model.HumanTaskStatusCancelled {
		t.Errorf("开放那条是 %q，期望 cancelled", rows[1].Status)
	}
}

// 未装配（全局 nil）时会话关闭必须照旧成功：本卡不许给既有路径添新失败面。
func TestUpdateSessionStatus_WithoutHumanTaskServiceStillSucceeds(t *testing.T) {
	database := newHandoffProduceDB(t)
	ctx := context.Background()
	useGlobalHumanTaskSvc(t, nil)

	session := seedHandoffSession(t, database, "sess_hook_nil", "oneid_hook_3")
	if err := newHandoffOrchestrator(t, database).transferToHuman(ctx, session, "低置信度"); err != nil {
		t.Fatalf("转人工失败: %v", err)
	}
	// 上面那次投递用的是编排器自带的服务实例（不经全局），所以池子里确实有一行开放待办；
	// 关闭会话时全局是 nil ⇒ 那行留在原处，而会话本身照常结束。
	if n := humanTaskProduceRowCount(t, database, "sess_hook_nil"); n != 1 {
		t.Fatalf("前置造数不对：%d 行", n)
	}
	if err := NewCustomerSessionServiceWithDB(database).
		UpdateSessionStatus(ctx, session.ID, model.SessionStatusResolved); err != nil {
		t.Fatalf("未装配待办底座时会话关闭失败: %v", err)
	}
	if got := handoffTaskOf(t, database, "sess_hook_nil"); got.Status != model.HumanTaskStatusPending {
		t.Errorf("未装配时不该改动作废，实际 %q", got.Status)
	}
	var stored model.CustomerSession
	if err := database.Where("session_id = ?", "sess_hook_nil").First(&stored).Error; err != nil {
		t.Fatalf("回读会话失败: %v", err)
	}
	if stored.Status != model.SessionStatusResolved {
		t.Errorf("会话状态 = %q，期望 resolved", stored.Status)
	}
}

// 没有开放待办的会话（压根没转过人工）关闭时不许报错，也不许凭空投一条。
func TestUpdateSessionStatus_NoOpenTaskIsNotAnError(t *testing.T) {
	database := newHandoffProduceDB(t)
	ctx := context.Background()
	useGlobalHumanTaskSvc(t, newHumanTaskSvc(t, database, &stubHumanTaskCfg{value: 5}))

	session := seedHandoffSession(t, database, "sess_hook_none", "oneid_hook_4")
	if err := NewCustomerSessionServiceWithDB(database).
		UpdateSessionStatus(ctx, session.ID, model.SessionStatusClosed); err != nil {
		t.Fatalf("关闭一条从未转人工的会话失败: %v", err)
	}
	if n := humanTaskProduceRowCount(t, database, ""); n != 0 {
		t.Errorf("关闭会话反而投出 %d 条待办", n)
	}
}
