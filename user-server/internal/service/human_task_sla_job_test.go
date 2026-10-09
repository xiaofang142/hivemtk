// human_task_sla_job_test.go I2：待办 SLA 超时闭环的行为用例。
//
// 这里只测"扫描器在真表上到底做了什么"：三态旗子的档位语义、去重窗口、
// 升级线、以及"每类只认自己那一列"。仓储的 SQL 口径由 human_task 的仓储用例
// 与 Counts 用例钉住，服务出口的逾期读数由 normalizeHumanTaskQuery 复用同一
// 时钟 seam —— 本文件不复测那两处，只钉"读出来之后办没办"。
package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// setupHumanTaskSLADB 扫描器要碰两张表：待办本体 + 它写出去的通知。
func setupHumanTaskSLADB(t *testing.T) *gorm.DB {
	t.Helper()
	return testutil.NewTestDB(t, &model.HumanTask{}, &model.Notification{})
}

func newHumanTaskSLASvc(t *testing.T, database *gorm.DB, mode HumanTaskSLAMode) (*HumanTaskSLAWorker, *HumanTaskService) {
	t.Helper()
	svc := NewHumanTaskService(repository.NewHumanTaskRepositoryWithDB(database), &stubHumanTaskCfg{value: 5})
	notifier := NewNotificationService(database)
	return NewHumanTaskSLAWorker(svc, notifier, mode, 0, 0), svc
}

// insertOverdueHandoff 直写一条"已逾期"的会话待办：服务层没有改 SLA 的入口，
// 逾期这个事实只能靠仓储侧改时刻造出来（口径同 Counts 用例）。
func insertOverdueHandoff(t *testing.T, database *gorm.DB, id, assignee string, created, deadline time.Time) *model.HumanTask {
	t.Helper()
	d := deadline
	task := &model.HumanTask{
		ID: id, Kind: model.HumanTaskKindConversationHandoff,
		Status: model.HumanTaskStatusPending, SubjectType: "customer_session",
		SubjectID: "sess_" + id, Title: "逾期会话待办", CreatedAt: created,
		AssigneeUserID: assignee, SlaFirstResponseAt: &d,
	}
	if err := repository.NewHumanTaskRepositoryWithDB(database).Insert(context.Background(), task); err != nil {
		t.Fatalf("直写逾期待办 %s 失败：%v", id, err)
	}
	return task
}

func countNotifications(t *testing.T, database *gorm.DB) int64 {
	t.Helper()
	var n int64
	if err := database.Model(&model.Notification{}).Count(&n).Error; err != nil {
		t.Fatalf("通知计数失败：%v", err)
	}
	return n
}

// off 档：Start 不启协程、RunOnce 既不数也不办、一条通知不写。
func TestHumanTaskSLAWorker_OffIsNoOp(t *testing.T) {
	database := setupHumanTaskSLADB(t)
	useHumanTaskClock(t, humanTaskFixedNow)
	w, _ := newHumanTaskSLASvc(t, database, HumanTaskSLAModeOff)
	insertOverdueHandoff(t, database, "ht_off_1", "7", humanTaskFixedNow.Add(-time.Hour), humanTaskFixedNow.Add(-time.Minute))

	w.Start(context.Background())
	if w.Running() {
		t.Fatal("off 档不应启动扫描协程")
	}
	rep := w.RunOnce(context.Background())
	if rep.Overdue != 0 || rep.Reminded != 0 || rep.Escalated != 0 {
		t.Fatalf("off 档应完全不动作，实得 %+v", rep)
	}
	if got := countNotifications(t, database); got != 0 {
		t.Fatalf("off 档不应写任何通知，实得 %d 条", got)
	}
}

// shadow 档：数得出来，但一条不写（这一档就是用来验逾期判定 SQL 算得对不对）。
func TestHumanTaskSLAWorker_ShadowCountsButWritesNothing(t *testing.T) {
	database := setupHumanTaskSLADB(t)
	useHumanTaskClock(t, humanTaskFixedNow)
	w, _ := newHumanTaskSLASvc(t, database, HumanTaskSLAModeShadow)
	insertOverdueHandoff(t, database, "ht_shadow_1", "7", humanTaskFixedNow.Add(-time.Hour), humanTaskFixedNow.Add(-time.Minute))

	rep := w.RunOnce(context.Background())
	if rep.Overdue != 1 {
		t.Fatalf("shadow 应数到 1 条逾期，实得 %d", rep.Overdue)
	}
	if rep.Reminded != 0 || rep.Escalated != 0 {
		t.Fatalf("shadow 不应写提醒/升级，实得 %+v", rep)
	}
	if got := countNotifications(t, database); got != 0 {
		t.Fatalf("shadow 不应写通知，实得 %d 条", got)
	}
	if w.OverdueTotal() != 1 {
		t.Errorf("累计逾期 = %d，期望 1", w.OverdueTotal())
	}
}

// enforce 档：逾期即提醒责任人一次；同一去重窗口内第二轮不重复写。
func TestHumanTaskSLAWorker_EnforceRemindsOncePerWindow(t *testing.T) {
	database := setupHumanTaskSLADB(t)
	useHumanTaskClock(t, humanTaskFixedNow)
	w, _ := newHumanTaskSLASvc(t, database, HumanTaskSLAModeEnforce)
	// 截止刚过、但拖得还不到一个完整 SLA 窗口（span=59min），所以只提醒、不升级。
	insertOverdueHandoff(t, database, "ht_enforce_1", "7", humanTaskFixedNow.Add(-time.Hour), humanTaskFixedNow.Add(-time.Minute))

	first := w.RunOnce(context.Background())
	if first.Reminded != 1 || first.Escalated != 0 {
		t.Fatalf("首轮应只提醒 1 条、不升级，实得 %+v", first)
	}
	if got := countNotifications(t, database); got != 1 {
		t.Fatalf("首轮应写 1 条通知，实得 %d", got)
	}
	second := w.RunOnce(context.Background())
	if second.Reminded != 0 {
		t.Fatalf("同一去重窗口内第二轮不应重复提醒，实得 %+v", second)
	}
	if got := countNotifications(t, database); got != 1 {
		t.Fatalf("去重后仍应只有 1 条通知，实得 %d", got)
	}

	var n model.Notification
	if err := database.Where("type = ?", HumanTaskOverdueNotifyType).First(&n).Error; err != nil {
		t.Fatalf("读提醒失败：%v", err)
	}
	if n.UserID != 7 {
		t.Errorf("提醒应发给责任人 7，实得 %d", n.UserID)
	}
}

// enforce 档：拖过整整一个 SLA 窗口（= 原始 SLA 时长）后向管理员广播升级。
func TestHumanTaskSLAWorker_EnforceEscalatesAfterFullWindow(t *testing.T) {
	database := setupHumanTaskSLADB(t)
	svc := NewHumanTaskService(repository.NewHumanTaskRepositoryWithDB(database), &stubHumanTaskCfg{value: 5})
	notifier := NewNotificationService(database)
	w := NewHumanTaskSLAWorker(svc, notifier, HumanTaskSLAModeEnforce, 0, 0)

	useHumanTaskClock(t, humanTaskFixedNow)
	mustSubmit(t, svc, handoffSubmitInput("sess_esc"))
	// 首响截止 = fixedNow+5min，span=5min，升级线 = 截止+5min = fixedNow+10min。
	useHumanTaskClock(t, humanTaskFixedNow.Add(11*time.Minute))

	rep := w.RunOnce(context.Background())
	if rep.Overdue != 1 {
		t.Fatalf("应扫到 1 条逾期，实得 %+v", rep)
	}
	if rep.Escalated != 1 {
		t.Fatalf("拖过一整个 SLA 窗口应升级，实得 %+v", rep)
	}
	var esc model.Notification
	if err := database.Where("type = ?", HumanTaskOverdueEscalateNotifyType).First(&esc).Error; err != nil {
		t.Fatalf("未找到升级通知：%v", err)
	}
	if esc.UserID != 0 {
		t.Errorf("升级应广播给全体（UserID=0），实得 %d", esc.UserID)
	}
}

// 逾期判定"每类只认自己那一列"：会话类只认首响列、审批类只认裁决列、
// 催收类只认升级列；跨档脏行（会话类却只填了审批列）不算逾期。
func TestHumanTaskSLAWorker_EachKindUsesOwnColumn(t *testing.T) {
	database := setupHumanTaskSLADB(t)
	useHumanTaskClock(t, humanTaskFixedNow)
	w, _ := newHumanTaskSLASvc(t, database, HumanTaskSLAModeShadow)

	past := humanTaskFixedNow.Add(-time.Minute)
	future := humanTaskFixedNow.Add(time.Hour)
	repo := repository.NewHumanTaskRepositoryWithDB(database)
	rows := []*model.HumanTask{
		{ID: "ht_k_handoff_over", Kind: model.HumanTaskKindConversationHandoff, Status: model.HumanTaskStatusPending,
			SubjectType: "customer_session", SubjectID: "s1", Title: "a", CreatedAt: humanTaskFixedNow.Add(-time.Hour), SlaFirstResponseAt: &past},
		{ID: "ht_k_handoff_ok", Kind: model.HumanTaskKindConversationHandoff, Status: model.HumanTaskStatusPending,
			SubjectType: "customer_session", SubjectID: "s2", Title: "b", CreatedAt: humanTaskFixedNow, SlaFirstResponseAt: &future},
		{ID: "ht_k_approval_over", Kind: model.HumanTaskKindApproval, Status: model.HumanTaskStatusPending,
			SubjectType: "approval_request", SubjectID: "a1", Title: "c", CreatedAt: humanTaskFixedNow.Add(-time.Hour), SlaDecideAt: &past},
		{ID: "ht_k_collect_ok", Kind: model.HumanTaskKindCollectionEscalation, Status: model.HumanTaskStatusPending,
			SubjectType: "collection_case", SubjectID: "c1", Title: "d", CreatedAt: humanTaskFixedNow, SlaEscalateAt: &future},
		// 跨档脏行：会话类却只填了审批列 → 自己那一档是 NULL，不该被别的档救活。
		{ID: "ht_k_dirty", Kind: model.HumanTaskKindConversationHandoff, Status: model.HumanTaskStatusPending,
			SubjectType: "customer_session", SubjectID: "s3", Title: "e", CreatedAt: humanTaskFixedNow.Add(-time.Hour), SlaDecideAt: &past},
	}
	for _, r := range rows {
		if err := repo.Insert(context.Background(), r); err != nil {
			t.Fatalf("直写 %s 失败：%v", r.ID, err)
		}
	}

	rep := w.RunOnce(context.Background())
	if rep.Overdue != 2 {
		t.Fatalf("应只认各自那一列，逾期数=2（会话1+审批1），实得 %d", rep.Overdue)
	}
}

// 未注入待办服务时不许 panic，且要出声（回一份带 err 的报告）。
func TestHumanTaskSLAWorker_NilServiceSafe(t *testing.T) {
	w := NewHumanTaskSLAWorker(nil, nil, HumanTaskSLAModeEnforce, 0, 0)
	rep := w.RunOnce(context.Background())
	if rep.Err == "" {
		t.Fatal("无待办服务时应回错，实得空")
	}
	w.Start(context.Background())
	if w.Running() {
		t.Fatal("无待办服务时不应启动扫描协程")
	}
}
