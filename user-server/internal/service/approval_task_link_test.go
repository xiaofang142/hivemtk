// approval_task_link_test.go T-P3-04：一条 pending 审批 = 池子里的一条 approval 类待办。
//
// 这张表上原来缺的不是字段而是**闭环**：审批行落库后没有任何人知道它存在（T-P3-01 建的表、
// T-P3-02 接的流程，两端都只写到"库里有一行 pending"），于是"没人裁决"这件事在池子、
// 在读数、在值班视野里同时不可见，而流程会安静地等到 TTL 自己过期。
// 本文件的判据因此全挂在**副作用**上：谁写了一行 human_task、写成了什么、
// 谁在什么时候把它收掉，而不是"代码里有没有那个调用"。
//
// 时钟必须两边一起冻：human_task 与 approval_request 各有自己的 nowFn（分属两张卡的
// 可测性设计），只冻一边时 sla_decide_at == expires_at 这条断言会随机红。
package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// setupApprovalTaskLinkDB 两张表都要在：待办投递与审批行是同一次调用的两个后果，
// 分库建就等于把要证的那条因果链切成两半各测各的。
func setupApprovalTaskLinkDB(t *testing.T) *gorm.DB {
	t.Helper()
	return testutil.NewTestDB(t, &model.HumanTask{}, &model.ApprovalRequest{})
}

// freezeBothClocks 把两张竖的时钟钉在同一时刻。
func freezeBothClocks(t *testing.T, at time.Time) func(time.Duration) {
	t.Helper()
	push := freezeApprovalClock(t, at)
	useHumanTaskClock(t, at)
	return push
}

// openApprovalTask 读回那条开放待办；没有返回 nil。
func openApprovalTask(t *testing.T, database *gorm.DB, approvalID string) *model.HumanTask {
	t.Helper()
	var rows []*model.HumanTask
	if err := database.Where("subject_type = ? AND subject_id = ?",
		HumanTaskSubjectApprovalRequest, approvalID).Find(&rows).Error; err != nil {
		t.Fatalf("读待办失败：%v", err)
	}
	if len(rows) > 1 {
		t.Fatalf("同一件事有 %d 行待办（幂等判据失效）", len(rows))
	}
	if len(rows) == 0 {
		return nil
	}
	return rows[0]
}

// fakeApprovalTaskSink 只回答一件事：投递/收口失败时上游怎么处理。
// 真库里造不出"写待办失败但写审批成功"这个窗口（同一事务？不是，两条竖各自提交），
// 所以用它跑那一条分支。
type fakeApprovalTaskSink struct {
	submitErr error
	closeErr  error

	submits []*model.ApprovalRequest
	closes  []*model.ApprovalRequest
}

func (f *fakeApprovalTaskSink) SubmitForApproval(_ context.Context, req *model.ApprovalRequest) error {
	f.submits = append(f.submits, req)
	return f.submitErr
}

func (f *fakeApprovalTaskSink) CloseForApproval(_ context.Context, req *model.ApprovalRequest) error {
	f.closes = append(f.closes, req)
	return f.closeErr
}

func newLinkApprovalSvc(t *testing.T, database *gorm.DB) *ApprovalRequestService {
	t.Helper()
	return NewApprovalRequestService(repository.NewApprovalRequestRepositoryWithDB(database), nil)
}

// --- 1. 投递 ——————————————————————————————————————————————

// TestApprovalPendingSubmitDeliversApprovalTask pending 审批落库的那一刻，池子里必须多一行。
//
// 变异靶子：把 Submit 里那次 sink 调用摘掉 ⇒ 本用例红在"待办为 nil"，
// 而审批侧全部既有用用例照旧绿（它们不知道待办的存在）⇒ 这条因果只能由本用例守。
func TestApprovalPendingSubmitDeliversApprovalTask(t *testing.T) {
	database := setupApprovalTaskLinkDB(t)
	taskSvc := newHumanTaskSvc(t, database, &stubHumanTaskCfg{value: 5})
	aprSvc := newLinkApprovalSvc(t, database)
	aprSvc.SetTaskSink(taskSvc)
	freezeBothClocks(t, humanTaskFixedNow)
	ctx := context.Background()

	row, created, err := aprSvc.Submit(ctx, ApprovalSubmitInput{
		SubjectType: ApprovalSubjectTypeSOPNode,
		SubjectID:   EncodeApprovalSubject(101, "wait_1"),
		PolicyKey:   "quote.send",
		TTL:         30 * time.Minute,
	})
	if err != nil || !created {
		t.Fatalf("入队审批失败：created=%v err=%v", created, err)
	}
	task := openApprovalTask(t, database, row.ID)
	if task == nil {
		t.Fatalf("pending 审批 %s 没投出待办：池子里看不见它，就等于没人要处理它", row.ID)
	}
	if task.Kind != model.HumanTaskKindApproval {
		t.Errorf("待办类别 = %q，期望 %q（类别决定它进哪个视图、算哪档 SLA）",
			task.Kind, model.HumanTaskKindApproval)
	}
	if task.Status != model.HumanTaskStatusPending {
		t.Errorf("待办状态 = %q，期望 %q", task.Status, model.HumanTaskStatusPending)
	}
	// 截止必须等于审批自己的到期时刻：两个事实源就会有两套"逾期"，
	// 而 TTL 与逾期读数的分界线正是审批行那一列（T-P3-02 ⑥ 同一条理由）。
	if task.SlaDecideAt == nil || !task.SlaDecideAt.Equal(*row.ExpiresAt) {
		t.Errorf("sla_decide_at = %v，期望 %v（审批行的 expires_at，不许另算一套）",
			task.SlaDecideAt, row.ExpiresAt)
	}
	if task.SlaFirstResponseAt != nil || task.SlaEscalateAt != nil {
		t.Errorf("审批类待办串了别的档：first=%v escalate=%v", task.SlaFirstResponseAt, task.SlaEscalateAt)
	}
	// 标题要能一眼看出"审的是什么策略要求的"：只有 ID 的列表页等于让值班逐个点开。
	if !strings.Contains(task.Title, "quote.send") {
		t.Errorf("标题里没有策略名：%q", task.Title)
	}
	if !strings.Contains(task.PayloadRef, EncodeApprovalSubject(101, "wait_1")) {
		// PayloadRef 指向被审对象：批之前总得看见要批的那件事。
		t.Errorf("payload_ref 没指向被审对象：%q", task.PayloadRef)
	}
	if task.AssigneeUserID != "" {
		t.Errorf("新投递不该已有人：assignee=%q", task.AssigneeUserID)
	}
	// 审批类不可抢占（C3）：这条待办如果被投成了会话类，下面这句就会绿得毫无意义。
	if _, err := taskSvc.Claim(ctx, task.ID, "seat-1"); err == nil {
		t.Error("审批类待办竟然可认领：说明投递时把 kind 写错了")
	}
}

// TestApprovalAutoApproveDeliversNoTask 策略当场放行 ⇒ 没有"等人裁决"这回事。
//
// 这条是反方向的判据：只在 pending 时投递。写反了（无条件投）会造出一批
// "永远等不到人、因为根本不需要人"的死待办，而它们会进逾期读数。
func TestApprovalAutoApproveDeliversNoTask(t *testing.T) {
	database := setupApprovalTaskLinkDB(t)
	taskSvc := newHumanTaskSvc(t, database, &stubHumanTaskCfg{value: 5})
	aprSvc := NewApprovalRequestService(repository.NewApprovalRequestRepositoryWithDB(database),
		&recordingPolicy{allow: true, reason: "低风险账号在白名单内"})
	aprSvc.SetTaskSink(taskSvc)
	freezeBothClocks(t, humanTaskFixedNow)

	row, _, err := aprSvc.Submit(context.Background(), ApprovalSubmitInput{
		SubjectType: ApprovalSubjectTypeSOPNode,
		SubjectID:   EncodeApprovalSubject(202, "wait_1"),
		PolicyKey:   "quote.send",
	})
	if err != nil {
		t.Fatalf("入队失败：%v", err)
	}
	if row.Status != model.ApprovalStatusApproved {
		t.Fatalf("期望 auto-approve 落 %q，实际 %q", model.ApprovalStatusApproved, row.Status)
	}
	if n := humanTaskRowCount(t, database, ""); n != 0 {
		t.Errorf("自动放行的审批投出了 %d 条待办：没有人要处理它，它只会灌水逾期读数", n)
	}
}

// TestApprovalResubmitKeepsSingleTaskAndRedeliversWhenMissing 重复入队不重复投递；
// 而第一次投递失败时，第二次入队必须把它补上。
//
// 两半各挡一种错：只做"新建时才投递"⇒ 投递失败过一次的 pending 永远没有待办
// （流程还挂着，没人看得见）；不做幂等⇒ 每次重试多一行待办。
func TestApprovalResubmitKeepsSingleTaskAndRedeliversWhenMissing(t *testing.T) {
	database := setupApprovalTaskLinkDB(t)
	taskSvc := newHumanTaskSvc(t, database, &stubHumanTaskCfg{value: 5})
	aprSvc := newLinkApprovalSvc(t, database)
	aprSvc.SetTaskSink(taskSvc)
	freezeBothClocks(t, humanTaskFixedNow)
	ctx := context.Background()
	subject := EncodeApprovalSubject(303, "wait_1")

	first, _, err := aprSvc.Submit(ctx, ApprovalSubmitInput{
		SubjectType: ApprovalSubjectTypeSOPNode, SubjectID: subject,
		PolicyKey: "quote.send", TTL: time.Hour,
	})
	if err != nil {
		t.Fatalf("首次入队失败：%v", err)
	}
	second, created, err := aprSvc.Submit(ctx, ApprovalSubmitInput{
		SubjectType: ApprovalSubjectTypeSOPNode, SubjectID: subject,
		PolicyKey: "quote.send", TTL: time.Hour,
	})
	if err != nil {
		t.Fatalf("重复入队失败：%v", err)
	}
	if created || second.ID != first.ID {
		t.Fatalf("重复入队没有复用同一行：created=%v %s→%s", created, first.ID, second.ID)
	}
	if n := humanTaskRowCount(t, database, first.ID); n != 1 {
		t.Fatalf("同一件事投出了 %d 条待办：一次裁决会留下一条没人管的死待办", n)
	}

	// 模拟"审批落库成功、待办投递失败"（那次 error 让 Submit 整体失败，但行已在库里）：
	// 删掉待办，再走一次入队（复用路径），必须把它补回来。
	if err := database.Where("subject_id = ?", first.ID).Delete(&model.HumanTask{}).Error; err != nil {
		t.Fatalf("清待办失败：%v", err)
	}
	if _, _, err := aprSvc.Submit(ctx, ApprovalSubmitInput{
		SubjectType: ApprovalSubjectTypeSOPNode, SubjectID: subject,
		PolicyKey: "quote.send", TTL: time.Hour,
	}); err != nil {
		t.Fatalf("复用路径入队失败：%v", err)
	}
	if n := humanTaskRowCount(t, database, first.ID); n != 1 {
		t.Errorf("复用已有 pending 时没有补投待办：现在 %d 条，期望 1 条"+
			"（流程还挂在它上面，池子里却看不见）", n)
	}
}

// --- 2. 收口 ——————————————————————————————————————————————

// TestApprovalDecideClosesTask 人工裁决（批与驳都算）把待办收在 done 上。
//
// 为什么拒也算"做完"：待办的内容是"请一个人来看这件事"，人来了并给了结论，
// 这件事就不再需要人 —— 至多批的是"不"。把它落成 cancelled 会让"人工处理量"
// 少算掉所有被拒的裁决，而那个数正是这张表要交付的指标。
func TestApprovalDecideClosesTask(t *testing.T) {
	for _, tc := range []struct {
		name    string
		verdict ApprovalVerdict
		by      string
	}{
		{"批准", ApprovalApprove, "ops-lead"},
		{"驳回", ApprovalReject, "ops-lead"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database := setupApprovalTaskLinkDB(t)
			taskSvc := newHumanTaskSvc(t, database, &stubHumanTaskCfg{value: 5})
			aprSvc := newLinkApprovalSvc(t, database)
			aprSvc.SetTaskSink(taskSvc)
			freezeBothClocks(t, humanTaskFixedNow)
			ctx := context.Background()

			row, _, err := aprSvc.Submit(ctx, ApprovalSubmitInput{
				SubjectType: ApprovalSubjectTypeSOPNode,
				SubjectID:   EncodeApprovalSubject(404, "wait_1"),
				PolicyKey:   "quote.send", TTL: time.Hour,
			})
			if err != nil {
				t.Fatalf("入队失败：%v", err)
			}
			if openApprovalTask(t, database, row.ID) == nil {
				t.Fatalf("前置断了：待办没投出来")
			}
			if _, err := aprSvc.Decide(ctx, row.ID, tc.verdict, tc.by, "已核对"); err != nil {
				t.Fatalf("裁决失败：%v", err)
			}
			task := openApprovalTask(t, database, row.ID)
			if task == nil {
				t.Fatalf("库里已无该 subject 的行：收口写成删除会把处理记录一起抹掉")
			}
			if task.Status != model.HumanTaskStatusDone {
				t.Errorf("裁决后待办状态 = %q，期望 %q", task.Status, model.HumanTaskStatusDone)
			}
			if task.AssigneeUserID != tc.by {
				t.Errorf("待办记的处理人 = %q，期望裁决者 %q", task.AssigneeUserID, tc.by)
			}
			if task.CompletedAt == nil || !task.CompletedAt.Equal(humanTaskFixedNow) {
				t.Errorf("完成时刻 = %v，期望 %v", task.CompletedAt, humanTaskFixedNow)
			}
			if task.CancelledAt != nil || task.CancelReason != "" {
				t.Errorf("人工裁决不该留下撤销痕迹：cancelled_at=%v reason=%q", task.CancelledAt, task.CancelReason)
			}
		})
	}
}

// TestApprovalExpireCancelsTask TTL 到期不是"人处理完了"，必须落 cancelled。
//
// 落成 done 会把"没人裁决"计进"人工处理量"——那正是 C3 分离视图要避免的那类污染。
func TestApprovalExpireCancelsTask(t *testing.T) {
	database := setupApprovalTaskLinkDB(t)
	taskSvc := newHumanTaskSvc(t, database, &stubHumanTaskCfg{value: 5})
	aprSvc := newLinkApprovalSvc(t, database)
	aprSvc.SetTaskSink(taskSvc)
	push := freezeBothClocks(t, humanTaskFixedNow)
	ctx := context.Background()

	row, _, err := aprSvc.Submit(ctx, ApprovalSubmitInput{
		SubjectType: ApprovalSubjectTypeSOPNode,
		SubjectID:   EncodeApprovalSubject(505, "wait_1"),
		PolicyKey:   "quote.send", TTL: time.Hour,
	})
	if err != nil {
		t.Fatalf("入队失败：%v", err)
	}
	push(2 * time.Hour) // 越过 expires_at
	flipped, err := aprSvc.ExpireOverdue(ctx, 10)
	if err != nil || len(flipped) != 1 {
		t.Fatalf("到期清扫=%d 条 err=%v", len(flipped), err)
	}
	task := openApprovalTask(t, database, row.ID)
	if task == nil {
		t.Fatalf("前置断了：待办没投出来")
	}
	if task.Status != model.HumanTaskStatusCancelled {
		t.Errorf("到期后待办状态 = %q，期望 %q", task.Status, model.HumanTaskStatusCancelled)
	}
	if !strings.Contains(task.CancelReason, "到期") {
		t.Errorf("撤销理由没说明缘由：%q（事后要能回答\"这条为什么没人做完就结束了\"）", task.CancelReason)
	}
	if task.AssigneeUserID != "" {
		t.Errorf("系统收口不该挂一个人：assignee=%q", task.AssigneeUserID)
	}
}

// TestApprovalSecondDecideDoesNotReopenOrRetouchTask 改判无路：第二次裁决失败时待办不许再动。
func TestApprovalSecondDecideDoesNotReopenOrRetouchTask(t *testing.T) {
	database := setupApprovalTaskLinkDB(t)
	taskSvc := newHumanTaskSvc(t, database, &stubHumanTaskCfg{value: 5})
	aprSvc := newLinkApprovalSvc(t, database)
	aprSvc.SetTaskSink(taskSvc)
	freezeBothClocks(t, humanTaskFixedNow)
	ctx := context.Background()

	row, _, err := aprSvc.Submit(ctx, ApprovalSubmitInput{
		SubjectType: ApprovalSubjectTypeSOPNode,
		SubjectID:   EncodeApprovalSubject(606, "wait_1"),
		PolicyKey:   "quote.send", TTL: time.Hour,
	})
	if err != nil {
		t.Fatalf("入队失败：%v", err)
	}
	if _, err := aprSvc.Decide(ctx, row.ID, ApprovalReject, "alice", "折扣过高"); err != nil {
		t.Fatalf("首次裁决失败：%v", err)
	}
	first := openApprovalTask(t, database, row.ID)
	if first.Status != model.HumanTaskStatusDone || first.CompletedAt == nil {
		t.Fatalf("前置断了：首次裁决没把待办收口（%+v）", first)
	}

	if _, err := aprSvc.Decide(ctx, row.ID, ApprovalApprove, "bob", "我要批"); !errors.Is(err, ErrApprovalAlreadyDecided) {
		t.Fatalf("第二次裁决期望 %v，实际 %v", ErrApprovalAlreadyDecided, err)
	}
	after := openApprovalTask(t, database, row.ID)
	if after.Status != first.Status || after.AssigneeUserID != first.AssigneeUserID ||
		!after.CompletedAt.Equal(*first.CompletedAt) {
		t.Errorf("失败的裁决改动了已收口的待办：%+v → %+v", first, after)
	}
}

// --- 3. 失败方向 ——————————————————————————————————————————

// TestApprovalSinkFailureFailsSuspendNotDecision 投递失败必须让入队整体失败；
// 收口失败不许把已经落库的裁决报成失败。
//
// 两条方向相反，理由是同一条：**能让流程继续往下走的只能是已经成立的事实**。
// 待办没投出去 ⇒ 这条 pending 大概永远没人看见 ⇒ 挂起必须失败（fail-closed，
// 不会放行危险动作）；裁决已经落库、唤醒已经发出 ⇒ 报"失败"会让操作者再点一次，
// 而第二次拿到的是 409，他会以为自己没权限。
func TestApprovalSinkFailureFailsSuspendNotDecision(t *testing.T) {
	database := setupApprovalTaskLinkDB(t)
	freezeBothClocks(t, humanTaskFixedNow)
	ctx := context.Background()
	sinkErr := errors.New("待办底座炸了")

	aprSvc := newLinkApprovalSvc(t, database)
	broken := &fakeApprovalTaskSink{submitErr: sinkErr}
	aprSvc.SetTaskSink(broken)
	row, _, err := aprSvc.Submit(ctx, ApprovalSubmitInput{
		SubjectType: ApprovalSubjectTypeSOPNode,
		SubjectID:   EncodeApprovalSubject(707, "wait_1"),
		PolicyKey:   "quote.send", TTL: time.Hour,
	})
	if !errors.Is(err, sinkErr) {
		t.Fatalf("投递失败期望上抛，实际 %v", err)
	}
	if row != nil {
		t.Error("投递失败时不该把记录当成结果交回去")
	}
	// 审批行仍留在库里（这是本用例要说明的代价，不是要顺手修掉的缺陷）：
	// 挂起失败了，但下一次入队会走复用路径把待办补上，见 _RedeliversWhenMissing。
	var pendingRows int64
	if err := database.Model(&model.ApprovalRequest{}).
		Where("subject_id = ?", EncodeApprovalSubject(707, "wait_1")).
		Where("status = ?", model.ApprovalStatusPending).Count(&pendingRows).Error; err != nil {
		t.Fatalf("数审批行失败：%v", err)
	}
	if pendingRows != 1 {
		t.Errorf("投递失败后库里应有 1 条 pending 审批（等补投），实际 %d", pendingRows)
	}

	// 收口失败：裁决照样成功返回，待办留在池子里（下一次动作会拿到 409，可接受）。
	aprSvc2 := newLinkApprovalSvc(t, database)
	aprSvc2.SetTaskSink(&fakeApprovalTaskSink{})
	pending, _, err := aprSvc2.Submit(ctx, ApprovalSubmitInput{
		SubjectType: ApprovalSubjectTypeSOPNode,
		SubjectID:   EncodeApprovalSubject(808, "wait_1"),
		PolicyKey:   "quote.send", TTL: time.Hour,
	})
	if err != nil {
		t.Fatalf("正常入队失败：%v", err)
	}
	aprSvc2.SetTaskSink(&fakeApprovalTaskSink{closeErr: sinkErr})
	decided, err := aprSvc2.Decide(ctx, pending.ID, ApprovalApprove, "ops-lead", "批")
	if err != nil {
		t.Fatalf("收口失败不该把已落库的裁决报成失败：%v", err)
	}
	if decided.Status != model.ApprovalStatusApproved {
		t.Errorf("裁决结果 = %q", decided.Status)
	}
}

// TestApprovalWithoutSinkDeliversNothingAndChangesNothing 未装配 sink ⇒ 与 T-P3-02 交付态逐字一致。
//
// 这条守的是"旗子 off / 待办底座未装配 ⇒ 本卡零影响"：装配层给 nil 时不得有任何副作用。
func TestApprovalWithoutSinkDeliversNothingAndChangesNothing(t *testing.T) {
	database := setupApprovalTaskLinkDB(t)
	aprSvc := newLinkApprovalSvc(t, database)
	freezeBothClocks(t, humanTaskFixedNow)
	ctx := context.Background()

	row, _, err := aprSvc.Submit(ctx, ApprovalSubmitInput{
		SubjectType: ApprovalSubjectTypeSOPNode,
		SubjectID:   EncodeApprovalSubject(909, "wait_1"),
		PolicyKey:   "quote.send", TTL: time.Hour,
	})
	if err != nil {
		t.Fatalf("无 sink 入队失败：%v", err)
	}
	if _, err := aprSvc.Decide(ctx, row.ID, ApprovalApprove, "ops-lead", "批"); err != nil {
		t.Fatalf("无 sink 裁决失败：%v", err)
	}
	if n := humanTaskRowCount(t, database, ""); n != 0 {
		t.Errorf("未装配 sink 却动了待办表：%d 行", n)
	}
	// nil 实例上的 setter 不许炸（装配层会对着 nil 服务调它）。
	var nilSvc *ApprovalRequestService
	nilSvc.SetTaskSink(&fakeApprovalTaskSink{})
}

// --- 4. 收口端的入参洁癖 ——————————————————————————————————

func TestApprovalCloseForApprovalRejectsStillPendingRow(t *testing.T) {
	database := setupApprovalTaskLinkDB(t)
	taskSvc := newHumanTaskSvc(t, database, &stubHumanTaskCfg{value: 5})
	aprSvc := newLinkApprovalSvc(t, database)
	aprSvc.SetTaskSink(taskSvc) // 先接上，才有"那条待办"可收
	freezeBothClocks(t, humanTaskFixedNow)
	ctx := context.Background()

	row, _, err := aprSvc.Submit(ctx, ApprovalSubmitInput{
		SubjectType: ApprovalSubjectTypeSOPNode,
		SubjectID:   EncodeApprovalSubject(111, "wait_1"),
		PolicyKey:   "quote.send", TTL: time.Hour,
	})
	if err != nil {
		t.Fatalf("入队失败：%v", err)
	}
	if err := taskSvc.CloseForApproval(ctx, row); err == nil {
		t.Error("对一条还 pending 的审批收口：应该报错而不是悄悄关掉待办")
	}
	task := openApprovalTask(t, database, row.ID)
	if task == nil {
		t.Fatal("前置断了：待办没投出来")
	}
	if task.Status != model.HumanTaskStatusPending {
		t.Errorf("失败的收口改动了待办状态：%q", task.Status)
	}
	// 待办不存在时收口是成功（幂等：没人投过就等于不需要收，别把 404 报给裁决路径）。
	if err := database.Where("subject_id = ?", row.ID).Delete(&model.HumanTask{}).Error; err != nil {
		t.Fatalf("清待办失败：%v", err)
	}
	decided := *row
	decided.Status = model.ApprovalStatusApproved
	decided.DecidedBy = "ops-lead"
	if err := taskSvc.CloseForApproval(ctx, &decided); err != nil {
		t.Errorf("无待办可收时期望成功，实际 %v", err)
	}
}
