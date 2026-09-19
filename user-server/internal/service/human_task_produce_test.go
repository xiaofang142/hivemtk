// human_task_produce_test.go T-P3-03 AC②：转人工那一路必须产出**一条**会话待办。
//
// 卡面写的是「conversation_handoff 由 transferToHuman 产生，验证不重复投递」。
// "产生"和"不重复"这两件事只有在真跑到 transferToHuman 那一行时才同时被验证：
//   - 只测 produceHandoffTask 这个方法，测到的是"我给它的闭包会被叫到"，
//     而 transferToHuman 里那一行调用有没有写、写在 Update 之前还是之后，全都没测；
//   - 只测 HumanTaskService.Submit 的幂等，测到的是"同一个 subject 投两次回同一行"，
//     而转人工那一路填进去的 subject 到底是不是会话本身，也没测。
//
// 所以下面这组是**真库 + 真编排器 + 真服务**：只把自动分派摘掉（它要一张有在线坐席的
// 表，与本卡的判据无关），其余一步都不桩。
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

// newHandoffProduceDB 建这套用例要的三张表。
//
// ⚠️ 一个用例只能调一次：NewTestDB 会先 DROP 再建，第二次调用会把第一个连接
// 刚写下的会话行连表一起清掉（本卡实测踩过）。需要直接落库旁路时用返回的句柄。
func newHandoffProduceDB(t *testing.T) *gorm.DB {
	t.Helper()
	return testutil.NewTestDB(t, &model.CustomerSession{}, &model.SessionMessage{}, &model.HumanTask{})
}

// newHandoffOrchestrator 装一台"会真写库"的编排器：会话/消息两个仓储指向测试库，
// 人工待办走真服务（配置桩只用来钉首响分钟数）。
func newHandoffOrchestrator(t *testing.T, database *gorm.DB) *SmartCSOrchestrator {
	t.Helper()
	o := NewSmartCSOrchestrator(nil, nil, nil)
	o.sessionRepo = repository.NewCustomerSessionRepositoryWithDB(database)
	o.messageRepo = repository.NewSessionMessageRepositoryWithDB(database)
	// 自动分派与本卡判据无关，显式摘掉：留着的话失败原因会混进"没有在线坐席"那条日志。
	o.assignmentSvc = nil
	cfg := &stubHumanTaskCfg{value: 5}
	o.SetHumanTaskProducer(HumanTaskHandoffProducer(newHumanTaskSvc(t, database, cfg)))
	return o
}

// seedHandoffSession 真造一条会话行：transferToHuman 走的是 Save，
// 库里没有这一行时"更新成功"这句话就是假的（0 行受影响不算错）。
func seedHandoffSession(t *testing.T, database *gorm.DB, sessionID, oneID string) *model.CustomerSession {
	t.Helper()
	s := &model.CustomerSession{
		SessionID: sessionID,
		UserID:    "cust_" + sessionID,
		OneID:     oneID,
		Platform:  model.PlatformWeb,
		Status:    model.SessionStatusAIHandling,
	}
	if err := database.Create(s).Error; err != nil {
		t.Fatalf("造会话行失败: %v", err)
	}
	return s
}

// 主用例：同一次转人工被调用两次（低置信度 + 情绪策略两条出口都会打到这里），
// 池子里只能有一条待办，且那一行的身份/类别/SLA 档都得对。
func TestTransferToHuman_ProducesExactlyOneHandoffTask(t *testing.T) {
	database := newHandoffProduceDB(t)
	o := newHandoffOrchestrator(t, database)
	ctx := context.Background()

	session := seedHandoffSession(t, database, "sess_ht_once", "oneid_ht_1")
	reason := "机器人连续两轮未答，客户要求人工"

	if err := o.transferToHuman(ctx, session, reason); err != nil {
		t.Fatalf("第一次转人工失败: %v", err)
	}
	session2 := *session // 换一份快照再走一次（真实场景是下一次入参重新读出来的行）
	if err := o.transferToHuman(ctx, &session2, reason); err != nil {
		t.Fatalf("第二次转人工失败: %v", err)
	}

	var rows []*model.HumanTask
	if err := database.Where("subject_id = ?", "sess_ht_once").Find(&rows).Error; err != nil {
		t.Fatalf("读待办失败: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("两次转人工投出 %d 条待办，期望 1 条（AC②）", len(rows))
	}
	got := rows[0]
	if got.Kind != model.HumanTaskKindConversationHandoff {
		t.Errorf("kind = %q", got.Kind)
	}
	if got.Status != model.HumanTaskStatusPending {
		t.Errorf("status = %q，期望 pending（新投的待办不该自带认领人）", got.Status)
	}
	if got.SubjectType != HumanTaskSubjectCustomerSession {
		t.Errorf("subject_type = %q，期望 %q", got.SubjectType, HumanTaskSubjectCustomerSession)
	}
	if got.AssigneeUserID != "" {
		t.Errorf("assignee_user_id = %q，期望空", got.AssigneeUserID)
	}
	if got.OneID != "oneid_ht_1" {
		t.Errorf("one_id = %q，期望带上会话的客户身份（待办中心要按人聚合）", got.OneID)
	}
	if got.Reason != reason {
		t.Errorf("reason = %q，期望原样带上转人工原因", got.Reason)
	}
	if !strings.HasPrefix(got.Title, "转人工") {
		t.Errorf("title = %q，待办中心那一行字要一眼看出是转人工", got.Title)
	}
	// AC①/AC④ 的隔离：会话待办只许占首响那一档。
	if got.SlaFirstResponseAt == nil {
		t.Error("sla_first_response_at 为空 ⇒ 这条待办永远不会进逾期读数")
	} else {
		left := time.Until(*got.SlaFirstResponseAt)
		if left <= 0 || left > 6*time.Minute {
			t.Errorf("首响截止 %s（剩 %s）应落在配置给的 5 分钟附近", got.SlaFirstResponseAt, left)
		}
	}
	if got.SlaDecideAt != nil || got.SlaEscalateAt != nil {
		t.Errorf("会话类填了别的 SLA 档（decide=%v escalate=%v）⇒ CS-32 的响应时长会被污染",
			got.SlaDecideAt, got.SlaEscalateAt)
	}

	// 坐席处理完之后再次转人工：那是**另一件事**，必须新投一条（复用旧行等于把
	// 上一轮的认领时间与结论抄进这一轮）。
	if _, err := newHumanTaskSvc(t, database, &stubHumanTaskCfg{value: 5}).
		Complete(ctx, got.ID, "agent-1"); err != nil {
		t.Fatalf("完成第一条待办失败: %v", err)
	}
	session3 := *session
	if err := o.transferToHuman(ctx, &session3, reason); err != nil {
		t.Fatalf("第三次转人工失败: %v", err)
	}
	var after []*model.HumanTask
	if err := database.Where("subject_id = ?", "sess_ht_once").Find(&after).Error; err != nil {
		t.Fatalf("读待办失败: %v", err)
	}
	if len(after) != 2 {
		t.Errorf("处理完再转人工后共 %d 条，期望 2 条（终态那条要留着复盘，不是复用它的坑）", len(after))
	}
}

// 待办写不进去不许把转人工带崩：会话状态此刻已经落库、回滚不了，
// 而上抛会让调用方把整条消息处理判失败 —— 客户那边其实已经转过去了。
func TestTransferToHuman_ProducerFailureIsNotFatal(t *testing.T) {
	database := newHandoffProduceDB(t)
	o := newHandoffOrchestrator(t, database)
	o.SetHumanTaskProducer(func(context.Context, *model.CustomerSession, string) error {
		return errors.New("待办底座炸了")
	})

	session := seedHandoffSession(t, database, "sess_ht_fail", "oneid_ht_2")
	if err := o.transferToHuman(context.Background(), session, "情绪升级"); err != nil {
		t.Fatalf("生产者失败不该让转人工失败，实际 %v", err)
	}
	var stored model.CustomerSession
	if err := database.Where("session_id = ?", "sess_ht_fail").First(&stored).Error; err != nil {
		t.Fatalf("回读会话失败: %v", err)
	}
	if stored.Status != model.SessionStatusWaiting {
		t.Errorf("会话状态 = %q，期望 waiting（转人工本身要落地）", stored.Status)
	}
	if n := humanTaskProduceRowCount(t, database, "sess_ht_fail"); n != 0 {
		t.Errorf("待办写入失败时不该有半行落库，实际 %d 行", n)
	}
}

// humanTaskProduceRowCount 数待办行数；subjectID 传空串 = 数整表。
func humanTaskProduceRowCount(t *testing.T, database *gorm.DB, subjectID string) int64 {
	t.Helper()
	var n int64
	sel := database.Model(&model.HumanTask{})
	if subjectID != "" {
		sel = sel.Where("subject_id = ?", subjectID)
	}
	if err := sel.Count(&n).Error; err != nil {
		t.Fatalf("数待办失败: %v", err)
	}
	return n
}

// 没挂生产者（本卡天然的关闸：不注入服务）时，transferToHuman 的行为与挂载前逐字一致。
func TestTransferToHuman_WithoutProducerIsUnchanged(t *testing.T) {
	database := newHandoffProduceDB(t)
	o := newHandoffOrchestrator(t, database)
	o.SetHumanTaskProducer(nil)

	session := seedHandoffSession(t, database, "sess_ht_nil", "oneid_ht_3")
	if err := o.transferToHuman(context.Background(), session, "低置信度"); err != nil {
		t.Fatalf("未挂生产者时转人工失败: %v", err)
	}
	if stored := humanTaskProduceRowCount(t, database, "sess_ht_nil"); stored != 0 {
		t.Errorf("未装配时投出 %d 条待办", stored)
	}
}

// --- produceHandoffTask 这一段脚手架自身的判据 --------------------------------

func TestProduceHandoffTask_NilProducerIsNoop(t *testing.T) {
	o := NewSmartCSOrchestrator(nil, nil, nil)
	if err := o.produceHandoffTask(context.Background(), &model.CustomerSession{SessionID: "s"}, "r"); err != nil {
		t.Errorf("未注入时应该零动作，实际 %v", err)
	}
	called := 0
	o.SetHumanTaskProducer(func(context.Context, *model.CustomerSession, string) error { called++; return nil })
	if err := o.produceHandoffTask(context.Background(), &model.CustomerSession{SessionID: "s"}, "r"); err != nil {
		t.Errorf("注入后不该报错: %v", err)
	}
	if called != 1 {
		t.Errorf("注入后应调用一次，实际 %d", called)
	}
}

// 客户端断开不许丢掉"有人在等"这条记录：ctx 要**续时间不续取消**
// （WithoutCancel + 上界），会话与原因原样透传。
func TestProduceHandoffTask_ArgsAndDetachedCtx(t *testing.T) {
	var (
		gotSession  *model.CustomerSession
		gotReason   string
		gotErr      error
		deadline    time.Time
		hasDeadline bool
	)
	o := NewSmartCSOrchestrator(nil, nil, nil)
	o.SetHumanTaskProducer(func(ctx context.Context, s *model.CustomerSession, reason string) error {
		gotSession, gotReason = s, reason
		deadline, hasDeadline = ctx.Deadline()
		return ctx.Err()
	})

	parent, cancel := context.WithCancel(context.Background())
	cancel() // 复现"请求已经返回给客户端了，这段还要写库"
	session := &model.CustomerSession{SessionID: "sess_ctx", OneID: "one_ctx"}
	if err := o.produceHandoffTask(parent, session, "客户要求人工"); err != nil {
		t.Fatalf("ctx 被取消不该让投递失败: %v", err)
	}
	if gotSession != session {
		t.Error("应把同一个会话对象交给生产者（换一份副本就会丢掉刚写进去的 handoff 字段）")
	}
	if gotReason != "客户要求人工" {
		t.Errorf("reason = %q", gotReason)
	}
	if !hasDeadline {
		t.Fatal("必须带上界：库卡住时这一段不能无限挂着一个请求")
	}
	if left := time.Until(deadline); left <= 0 || left > humanTaskProduceTimeout {
		t.Errorf("剩余时限 %s 应落在 (0, %s] 内", left, humanTaskProduceTimeout)
	}
	if gotErr != nil {
		t.Errorf("父 ctx 已取消时子 ctx 仍应可用，实际 ctx.Err()=%v", gotErr)
	}
}

// 空会话不许一路走到 Submit：那里会因 subject_id 为空报 400，
// 但那句错误在日志里完全指不回"哪一次转人工"，所以在这里先拒并说清是谁的锅。
func TestProduceHandoffTask_RejectsNilSession(t *testing.T) {
	o := NewSmartCSOrchestrator(nil, nil, nil)
	called := 0
	o.SetHumanTaskProducer(func(context.Context, *model.CustomerSession, string) error { called++; return nil })
	if err := o.produceHandoffTask(context.Background(), nil, "r"); !errors.Is(err, ErrHumanTaskInputInvalid) {
		t.Errorf("nil 会话 = %v，期望 ErrHumanTaskInputInvalid", err)
	}
	if called != 0 {
		t.Errorf("nil 会话时生产者不该被叫到，实际 %d 次", called)
	}
}

// --- SubmitHandoffForSession 的映射判据（服务出口，不经编排器） ------------------

func TestSubmitHandoffForSession_Mapping(t *testing.T) {
	database := newHandoffProduceDB(t)
	svc := newHumanTaskSvc(t, database, &stubHumanTaskCfg{value: 5})
	ctx := context.Background()

	session := seedHandoffSession(t, database, "sess_map_1", "oneid_map")
	task, created, err := svc.SubmitHandoffForSession(ctx, session, "置信度低于阈值")
	if err != nil || !created {
		t.Fatalf("首次投递 = (%v,%v)", created, err)
	}
	if task.SubjectType != HumanTaskSubjectCustomerSession || task.SubjectID != "sess_map_1" {
		t.Errorf("身份二元组 = %s/%s", task.SubjectType, task.SubjectID)
	}
	// 会话 id 为空的行：那是"待办指不回任何一件事"，必须拒而不是建出一条孤儿待办。
	orphan := &model.CustomerSession{OneID: "oneid_map"}
	if _, _, err := svc.SubmitHandoffForSession(ctx, orphan, "x"); !errors.Is(err, ErrHumanTaskInputInvalid) {
		t.Errorf("空会话 id = %v，期望 ErrHumanTaskInputInvalid", err)
	}
	if n := humanTaskProduceRowCount(t, database, ""); n != 1 {
		t.Errorf("被拒的投递不该落库，实际整表 %d 行", n)
	}
}
