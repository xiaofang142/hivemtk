// human_task_test.go T-P3-03：统一待办服务（三类共用的写入/跃迁/读数口径）。
//
// 服务层只测**只有它能负责**的四件事，仓储用例测过的机制不在这里重跑一遍：
//  1. 入参口径（未知 kind、空身份、超限、SLA 该不该由调用方给）—— 闸门在写入侧，
//     放过去一行脏 kind，读侧的三类过滤就永远对不上；
//  2. 幂等：同一件事的第二次投递必须回到**同一条**待办（AC②），含并发那一支；
//  3. 跃迁判据（哪个动作在哪类待办上走得通、失败时是 404/409/409-冲突 的哪一种）；
//  4. 读数装配（AC③ 的按类聚合 + AC④ 的指标隔离在**服务出口**上成立，
//     而不只是在某条 SQL 里成立 —— 中间还隔着一次映射，映射会漏键）。
//
// 时钟与 ID 生成走包级变量替换（同 approval 侧口径）：断言"逾期"和"排序"不能靠墙钟。
package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// --- 测试脚手架 ---------------------------------------------------------------

func setupHumanTaskSvcDB(t *testing.T) *gorm.DB {
	t.Helper()
	return testutil.NewTestDB(t, &model.HumanTask{})
}

// humanTaskFixedNow 服务层用例的共用"当前时刻"：逾期/SLA 断言全部围绕它算。
var humanTaskFixedNow = time.Date(2026, 9, 20, 8, 0, 0, 0, time.UTC)

// useHumanTaskClock 换掉服务层的时钟，用例结束自动还原。
func useHumanTaskClock(t *testing.T, at time.Time) {
	t.Helper()
	prev := humanTaskNowFn
	humanTaskNowFn = func() time.Time { return at }
	t.Cleanup(func() { humanTaskNowFn = prev })
}

// stubHumanTaskCfg 记录被问到的 group/key —— 这一条不是形式主义：
// 键名打错时 GetInt 会安静地回 fallback，功能"看起来正常"，而运维改参数永远不生效。
type stubHumanTaskCfg struct {
	gotGroup, gotKey string
	calls            int
	value            int
}

func (s *stubHumanTaskCfg) GetInt(_ context.Context, group, key string, fallback int) int {
	s.gotGroup, s.gotKey, s.calls = group, key, s.calls+1
	if s.value == 0 {
		return fallback
	}
	return s.value
}

func newHumanTaskSvc(t *testing.T, database *gorm.DB, cfg HumanTaskConfigReader) *HumanTaskService {
	t.Helper()
	return NewHumanTaskService(repository.NewHumanTaskRepositoryWithDB(database), cfg)
}

func handoffSubmitInput(subjectID string) HumanTaskSubmitInput {
	return HumanTaskSubmitInput{
		Kind:        model.HumanTaskKindConversationHandoff,
		SubjectType: "customer_session",
		SubjectID:   subjectID,
		Title:       "客户请求转人工",
		Reason:      "机器人连续两轮未答",
	}
}

func approvalSubmitInput(subjectID string, due time.Time) HumanTaskSubmitInput {
	at := due
	return HumanTaskSubmitInput{
		Kind:        model.HumanTaskKindApproval,
		SubjectType: "approval_request",
		SubjectID:   subjectID,
		Title:       "等报价审批",
		SlaDueAt:    &at,
	}
}

// mustSubmit 投递一条待办并断言它是**新建**的（复用要单独断言的用例不走这里）。
func mustSubmit(t *testing.T, svc *HumanTaskService, in HumanTaskSubmitInput) *model.HumanTask {
	t.Helper()
	task, created, err := svc.Submit(context.Background(), in)
	if err != nil {
		t.Fatalf("Submit(%s/%s) 报错：%v", in.Kind, in.SubjectID, err)
	}
	if !created {
		t.Fatalf("Submit(%s/%s) 判为复用，期望新建", in.Kind, in.SubjectID)
	}
	return task
}

// humanTaskRowCount 数一遍库里的行数（"第二次投递到底落了几行"只能用行数回答）。
func humanTaskRowCount(t *testing.T, database *gorm.DB, subjectID string) int64 {
	t.Helper()
	var n int64
	q := database.Model(&model.HumanTask{})
	if subjectID != "" {
		q = q.Where("subject_id = ?", subjectID)
	}
	if err := q.Count(&n).Error; err != nil {
		t.Fatalf("计数失败：%v", err)
	}
	return n
}

// --- 1. 入参口径 --------------------------------------------------------------

func TestHumanTaskSvc_SubmitRejectsBadInput(t *testing.T) {
	database := setupHumanTaskSvcDB(t)
	svc := newHumanTaskSvc(t, database, &stubHumanTaskCfg{value: 5})
	useHumanTaskClock(t, humanTaskFixedNow)
	ctx := context.Background()
	due := humanTaskFixedNow.Add(2 * time.Hour)

	withHandoffDue := func(at time.Time) HumanTaskSubmitInput {
		in := handoffSubmitInput("s1")
		p := at
		in.SlaDueAt = &p
		return in
	}

	cases := []struct {
		name string
		in   HumanTaskSubmitInput
		want string // 错误信息里要点名的那个字段
	}{
		{"未知 kind（近似拼写）", HumanTaskSubmitInput{Kind: "handoff", SubjectType: "customer_session", SubjectID: "s1"}, "kind"},
		{"空 kind", HumanTaskSubmitInput{SubjectType: "customer_session", SubjectID: "s1"}, "kind"},
		{"空 subject_type", HumanTaskSubmitInput{Kind: model.HumanTaskKindApproval, SubjectID: "s1", SlaDueAt: &due}, "subject_type"},
		{"空 subject_id", HumanTaskSubmitInput{Kind: model.HumanTaskKindApproval, SubjectType: "customer_session", SlaDueAt: &due}, "subject_id"},
		{"subject_type 含内部空格", HumanTaskSubmitInput{Kind: model.HumanTaskKindApproval, SubjectType: "customer session", SubjectID: "s1", SlaDueAt: &due}, "subject_type"},
		{"subject_type 含竖线", HumanTaskSubmitInput{Kind: model.HumanTaskKindApproval, SubjectType: "a|b", SubjectID: "s1", SlaDueAt: &due}, "subject_type"},
		{"subject_type 超长", HumanTaskSubmitInput{Kind: model.HumanTaskKindApproval, SubjectType: strings.Repeat("s", 33), SubjectID: "s1", SlaDueAt: &due}, "subject_type"},
		{"subject_id 超长", HumanTaskSubmitInput{Kind: model.HumanTaskKindApproval, SubjectType: "approval_request", SubjectID: strings.Repeat("x", 257), SlaDueAt: &due}, "subject_id"},
		{"one_id 超列宽", HumanTaskSubmitInput{Kind: model.HumanTaskKindApproval, SubjectType: "approval_request", SubjectID: "s1", OneID: strings.Repeat("o", 101), SlaDueAt: &due}, "one_id"},
		{"审批类缺 SLA", HumanTaskSubmitInput{Kind: model.HumanTaskKindApproval, SubjectType: "approval_request", SubjectID: "s1"}, "sla_due_at"},
		{"催收类缺 SLA", HumanTaskSubmitInput{Kind: model.HumanTaskKindCollectionEscalation, SubjectType: "collection_case", SubjectID: "s1"}, "sla_due_at"},
		{"会话类代填 SLA", withHandoffDue(due), "sla_due_at"},
	}
	for _, c := range cases {
		_, _, err := svc.Submit(ctx, c.in)
		if err == nil {
			t.Errorf("%s：竟然投递成功", c.name)
			continue
		}
		if !errors.Is(err, ErrHumanTaskInputInvalid) {
			t.Errorf("%s：错误种类 = %v，期望 ErrHumanTaskInputInvalid（controller 靠它映射 400）", c.name, err)
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s：错误信息没点名问题字段 %q，实际 %q", c.name, c.want, err.Error())
		}
	}
	// 上面十二次投递必须一行都没落。有行落进来意味着"报错"与"写入"不在同一侧，
	// 于是脏行会留在池子里等人（并且占住幂等键，把后面那次正确的投递判成重复投递）。
	if n := humanTaskRowCount(t, database, ""); n != 0 {
		t.Errorf("非法入参后库里留下 %d 行", n)
	}
}

// 展示文本（标题/原因）超长 ⇒ **裁断后收下**，而不是拒掉整条投递。
//
// 为什么这两个字段与身份字段不同命：subject_id/kind/one_id 错了要拒，因为"猜一个身份"
// 会把待办挂到别的事情上；而标题/原因只是给人看的那段话，裁断只是少几个字，
// 拒收却是"这条待办压根不存在"。转人工那一路的原因来自上游（LLM 归因、情绪策略文案），
// 长度不由我们决定 —— 一次超长就把"有个客户在等人工"这条事实丢掉，是拿最贵的代价
// 换最便宜的 Symptom（列表响应体变大）。上限仍然生效，只是生效在行上而不是请求上。
func TestHumanTaskSvc_SubmitClipsDisplayText(t *testing.T) {
	database := setupHumanTaskSvcDB(t)
	svc := newHumanTaskSvc(t, database, &stubHumanTaskCfg{value: 5})
	ctx := context.Background()
	due := humanTaskFixedNow.Add(2 * time.Hour)

	in := HumanTaskSubmitInput{
		Kind:        model.HumanTaskKindApproval,
		SubjectType: "approval_request",
		SubjectID:   "s_clip",
		Title:       strings.Repeat("标", humanTaskTitleMaxLen+40),
		Reason:      strings.Repeat("长", humanTaskReasonMaxLen+7),
		SlaDueAt:    &due,
	}
	task, created, err := svc.Submit(ctx, in)
	if err != nil || !created {
		t.Fatalf("超长展示文本不该拒收，实际 (%v,%v)", created, err)
	}
	if got := utf8.RuneCountInString(task.Title); got != humanTaskTitleMaxLen {
		t.Errorf("title 裁到 %d 字符，期望 %d", got, humanTaskTitleMaxLen)
	}
	if got := utf8.RuneCountInString(task.Reason); got != humanTaskReasonMaxLen {
		t.Errorf("reason 裁到 %d 字符，期望 %d", got, humanTaskReasonMaxLen)
	}
	// 裁断必须按字符：按字节切会把最后一个汉字的三字节砍成半个，
	// 落库是一串 U+FFFD，而列表页正是要把这一行显示出来的地方。
	if !strings.HasSuffix(task.Title, "标") {
		t.Errorf("裁断结果不该以半截汉字收尾：%q", task.Title[len(task.Title)-6:])
	}
	row, err := svc.Get(ctx, task.ID)
	if err != nil || row == nil {
		t.Fatalf("回读失败 (%v,%v)", row, err)
	}
	if utf8.RuneCountInString(row.Title) != humanTaskTitleMaxLen {
		t.Errorf("库内 title %d 字符，期望 %d", utf8.RuneCountInString(row.Title), humanTaskTitleMaxLen)
	}
	// 未超长的原文一个字都不该被动到（裁断只在越界时发生）。
	plain := HumanTaskSubmitInput{
		Kind: model.HumanTaskKindApproval, SubjectType: "approval_request",
		SubjectID: "s_clip_2", Title: "报价审批", Reason: "等客户确认折扣", SlaDueAt: &due,
	}
	got := mustSubmit(t, svc, plain)
	if got.Title != "报价审批" || got.Reason != "等客户确认折扣" {
		t.Errorf("正常长度被改动了：%q / %q", got.Title, got.Reason)
	}
}

// 标题与原因**不**必填：转人工那一路拿不到标题也得有个人被叫起来。
// 本用例钉住这个方向 —— 将来有人把它改成必填，就会在转人工的写入失败里丢掉待办。
func TestHumanTaskSvc_SubmitAllowsEmptyOptionalFields(t *testing.T) {
	database := setupHumanTaskSvcDB(t)
	svc := newHumanTaskSvc(t, database, &stubHumanTaskCfg{value: 5})
	in := HumanTaskSubmitInput{
		Kind:        model.HumanTaskKindConversationHandoff,
		SubjectType: "customer_session",
		SubjectID:   "sess_min",
	}
	task := mustSubmit(t, svc, in)
	if task.Title != "" || task.Reason != "" {
		t.Errorf("空可选字段被填了默认值：%q/%q", task.Title, task.Reason)
	}
}

// 落库形状由服务定：ID 有前缀（日志里认得出是哪张表）、起点是 pending、
// created_at 用注入时钟而不是"到时候再看"（逾期与排序断言都依赖它可复现）。
func TestHumanTaskSvc_SubmitRowShape(t *testing.T) {
	database := setupHumanTaskSvcDB(t)
	svc := newHumanTaskSvc(t, database, &stubHumanTaskCfg{value: 5})
	useHumanTaskClock(t, humanTaskFixedNow)
	got := mustSubmit(t, svc, handoffSubmitInput("sess_shape"))
	if !strings.HasPrefix(got.ID, "ht_") {
		t.Errorf("ID = %q，期望 ht_ 前缀", got.ID)
	}
	if got.Status != model.HumanTaskStatusPending {
		t.Errorf("初始态 = %s，期望 pending", got.Status)
	}
	if !got.CreatedAt.Equal(humanTaskFixedNow) {
		t.Errorf("created_at = %v，期望 %v", got.CreatedAt, humanTaskFixedNow)
	}
	if got.AssigneeUserID != "" || got.ClaimedAt != nil || got.CompletedAt != nil || got.CancelledAt != nil {
		t.Errorf("新投放的待办带了处理痕迹：%+v", got)
	}
}

// --- 2. SLA 落列（AC①）--------------------------------------------------------

// TestHumanTaskSvc_SubmitPlacesSLAInOwnColumn 三类各写自己那一列，其余两列必须为空。
//
// 落错列的后果不是难看，是指标串档：逾期读数按 kind 认列（仓储侧用例已钉），
// 一条会话待办被填上审批档就等于"没有截止"，坐席的响应指标当场漏算这一条。
func TestHumanTaskSvc_SubmitPlacesSLAInOwnColumn(t *testing.T) {
	database := setupHumanTaskSvcDB(t)
	cfg := &stubHumanTaskCfg{value: 7}
	svc := newHumanTaskSvc(t, database, cfg)
	useHumanTaskClock(t, humanTaskFixedNow)
	ctx := context.Background()

	due := humanTaskFixedNow.Add(3 * time.Hour)
	cases := []struct {
		kind     string
		in       HumanTaskSubmitInput
		wantCol  string
		wantDue  time.Time
		wantAskC int // 这一类该不该问配置（只有会话类问）
	}{
		{
			kind:     model.HumanTaskKindConversationHandoff,
			in:       handoffSubmitInput("sess_sla_1"),
			wantCol:  "sla_first_response_at",
			wantDue:  humanTaskFixedNow.Add(7 * time.Minute),
			wantAskC: 1,
		},
		{
			kind:    model.HumanTaskKindApproval,
			in:      approvalSubmitInput("apr_sla_1", due),
			wantCol: "sla_decide_at",
			wantDue: due,
		},
		{
			kind:    model.HumanTaskKindCollectionEscalation,
			in:      HumanTaskSubmitInput{Kind: model.HumanTaskKindCollectionEscalation, SubjectType: "collection_case", SubjectID: "cc_sla_1", SlaDueAt: &due},
			wantCol: "sla_escalate_at",
			wantDue: due,
		},
	}
	for _, c := range cases {
		got := mustSubmit(t, svc, c.in)
		if got.Kind != c.kind {
			t.Errorf("kind = %q，期望 %q", got.Kind, c.kind)
		}
		if bad := model.HumanTaskSLAUnsupportedFields(got); len(bad) != 0 {
			t.Errorf("%s：填了不属于自己档的 SLA 列 %v", c.kind, bad)
		}
		var col *time.Time
		switch c.wantCol {
		case "sla_first_response_at":
			col = got.SlaFirstResponseAt
		case "sla_decide_at":
			col = got.SlaDecideAt
		case "sla_escalate_at":
			col = got.SlaEscalateAt
		}
		if col == nil {
			t.Errorf("%s：期望 %s 有值，实际为 NULL", c.kind, c.wantCol)
			continue
		}
		if !col.Equal(c.wantDue) {
			t.Errorf("%s：%s = %v，期望 %v", c.kind, c.wantCol, col, c.wantDue)
		}
		// 回读一次：断言的是**落库后**的形状，不是内存里那份还没写进去的构造结果。
		back, err := svc.Get(ctx, got.ID)
		if err != nil || back == nil {
			t.Fatalf("%s 回读失败：(%v,%v)", got.ID, back, err)
		}
		if bad := model.HumanTaskSLAUnsupportedFields(back); len(bad) != 0 {
			t.Errorf("%s：库里的行填了越界 SLA 列 %v", c.kind, bad)
		}
	}
	// 配置只被会话类问一次，且问的是那个键（键名打错 = 参数改不动，且不会报错）。
	if cfg.calls != 1 {
		t.Errorf("配置被问了 %d 次，期望 1 次（另两类的截止来自各自业务表，问配置就是第二个事实源）", cfg.calls)
	}
	if cfg.gotGroup != HumanTaskConfigGroup || cfg.gotKey != HumanTaskHandoffSlaKey {
		t.Errorf("配置键 = %s/%s，期望 %s/%s", cfg.gotGroup, cfg.gotKey, HumanTaskConfigGroup, HumanTaskHandoffSlaKey)
	}
}

// 配置值是运维改的：0 / 负数 / 荒谬的大数都要退回默认，而不是
// "首响截止 = 投递那一刻"（那样每条会话待办一落库就是逾期，逾期读数当场失焦）。
func TestHumanTaskSvc_SubmitHandoffSLAFallsBackOnBadConfig(t *testing.T) {
	want := humanTaskFixedNow.Add(time.Duration(DefaultHumanTaskHandoffSlaMinutes) * time.Minute)
	for _, v := range []int{-3, 0, 100000} {
		database := setupHumanTaskSvcDB(t)
		svc := newHumanTaskSvc(t, database, &stubHumanTaskCfg{value: v})
		useHumanTaskClock(t, humanTaskFixedNow)
		got := mustSubmit(t, svc, handoffSubmitInput(fmt.Sprintf("sess_cfg_%d", v)))
		if got.SlaFirstResponseAt == nil || !got.SlaFirstResponseAt.Equal(want) {
			t.Errorf("配置 %d：首响截止 = %v，期望退回默认 %v", v, got.SlaFirstResponseAt, want)
		}
	}
}

// 没有配置读取器（装配早期）也必须给出默认口径，而不是"这条待办没有截止"。
func TestHumanTaskSvc_SubmitHandoffSLAWithoutConfigReader(t *testing.T) {
	database := setupHumanTaskSvcDB(t)
	svc := newHumanTaskSvc(t, database, nil)
	useHumanTaskClock(t, humanTaskFixedNow)
	want := humanTaskFixedNow.Add(time.Duration(DefaultHumanTaskHandoffSlaMinutes) * time.Minute)
	got := mustSubmit(t, svc, handoffSubmitInput("sess_cfg_none"))
	if got.SlaFirstResponseAt == nil || !got.SlaFirstResponseAt.Equal(want) {
		t.Errorf("无配置器时首响截止 = %v，期望默认 %v", got.SlaFirstResponseAt, want)
	}
}

// 配置在合理区间内要真的生效（上面测的是"不生效时不坏"，这条测"生效"）。
func TestHumanTaskSvc_SubmitHandoffSLAUsesConfiguredMinutes(t *testing.T) {
	database := setupHumanTaskSvcDB(t)
	svc := newHumanTaskSvc(t, database, &stubHumanTaskCfg{value: 90})
	useHumanTaskClock(t, humanTaskFixedNow)
	got := mustSubmit(t, svc, handoffSubmitInput("sess_cfg_ok"))
	want := humanTaskFixedNow.Add(90 * time.Minute)
	if got.SlaFirstResponseAt == nil || !got.SlaFirstResponseAt.Equal(want) {
		t.Errorf("首响截止 = %v，期望按配置 %v", got.SlaFirstResponseAt, want)
	}
}

// --- 3. 幂等（AC②）-----------------------------------------------------------

func TestHumanTaskSvc_SubmitIdempotentAcrossStates(t *testing.T) {
	database := setupHumanTaskSvcDB(t)
	svc := newHumanTaskSvc(t, database, &stubHumanTaskCfg{value: 5})
	useHumanTaskClock(t, humanTaskFixedNow)
	ctx := context.Background()

	first := mustSubmit(t, svc, handoffSubmitInput("sess_idem"))
	again, created, err := svc.Submit(ctx, handoffSubmitInput("sess_idem"))
	if err != nil {
		t.Fatalf("第二次投递报错：%v", err)
	}
	if created {
		t.Error("同一会话的第二次投递判为新建（AC② 要求的是同一件事只有一条开放待办）")
	}
	if again.ID != first.ID {
		t.Errorf("复用了另一条待办：%s ≠ %s", again.ID, first.ID)
	}
	if n := humanTaskRowCount(t, database, "sess_idem"); n != 1 {
		t.Errorf("库里 %d 行，期望 1 行", n)
	}

	// 入参归一后再比：带空格与大小写差异的同一身份不该造出第二条待办
	// （subject_type 参与幂等键，归一不做就会两侧各一行）。
	shifted := handoffSubmitInput("sess_idem")
	shifted.SubjectType = " Customer_Session "
	shifted.SubjectID = "  sess_idem  "
	if _, created, err = svc.Submit(ctx, shifted); err != nil {
		t.Fatalf("归一路径报错：%v", err)
	} else if created {
		t.Error("未归一的 subject 造出了第二条待办（幂等键两侧写法不同）")
	}

	// 处理完之后同一件事可以再开一条（第二次转人工必须看得见，且是**新行**）。
	if _, err := svc.Complete(ctx, first.ID, "agent-1"); err != nil {
		t.Fatalf("完成失败：%v", err)
	}
	third, created, err := svc.Submit(ctx, handoffSubmitInput("sess_idem"))
	if err != nil || !created {
		t.Fatalf("前一条已完成后应能再投一条：(%v,%v)", created, err)
	}
	if third.ID == first.ID {
		t.Error("复用了已落定的旧行：同一会话被转过两次人工这件事就看不见了")
	}
	if n := humanTaskRowCount(t, database, "sess_idem"); n != 2 {
		t.Errorf("库里 %d 行，期望 2 行（一次转人工一行，证据链按行数数）", n)
	}

	// 终态有两条，两条都得让坑：**撤销**这一条尤其容易漏 —— 会话被系统自动撤销
	// （resolved/closed 那条钩子）之后客户又开口、又要转人工，这是线上最常见的复投路径。
	// 索引谓词若只排掉 done，这一步会撞在一条 cancelled 行上，而 service 回读开放待办
	// 又读不到它（Go 侧它不是开放态），于是把唯一索引错误原样抛给转人工 ⇒ 会话转了人工、
	// 池子里没有这一行。判据：这一腿必须新建成功。
	if _, err := svc.Cancel(ctx, third.ID, "agent-1", "该会话改由他人处理"); err != nil {
		t.Fatalf("撤销失败：%v", err)
	}
	fourth, created, err := svc.Submit(ctx, handoffSubmitInput("sess_idem"))
	if err != nil || !created {
		t.Fatalf("前一条已撤销后应能再投一条：(%v,%v)", created, err)
	}
	if fourth.ID == third.ID || fourth.ID == first.ID {
		t.Error("复用了已撤销的旧行")
	}
	if n := humanTaskRowCount(t, database, "sess_idem"); n != 3 {
		t.Errorf("库里 %d 行，期望 3 行（三次转人工三行）", n)
	}
}

// 并发投递收敛到一行：幂等键在库里，而"查到就复用"这条读路径在竞态下会两个人都查空。
// 撞了唯一索引之后必须回去**复用那一行**，而不是把错误抛给转人工的调用方。
func TestHumanTaskSvc_SubmitConvergesToOneRow(t *testing.T) {
	database := setupHumanTaskSvcDB(t)
	svc := newHumanTaskSvc(t, database, &stubHumanTaskCfg{value: 5})
	ctx := context.Background()

	const n = 8
	var wg sync.WaitGroup
	start := make(chan struct{})
	ids := make([]string, n)
	createdFlags := make([]bool, n)
	var errs []error
	var mu sync.Mutex
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			task, created, err := svc.Submit(ctx, handoffSubmitInput("sess_race"))
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
				return
			}
			ids[i], createdFlags[i] = task.ID, created
		}(i)
	}
	close(start)
	wg.Wait()

	if len(errs) > 0 {
		t.Fatalf("并发投递有 %d 次报错：%v", len(errs), errs[0])
	}
	seen := map[string]bool{}
	winners := 0
	for i := range ids {
		if ids[i] == "" {
			continue
		}
		seen[ids[i]] = true
		if createdFlags[i] {
			winners++
		}
	}
	if len(seen) != 1 {
		t.Errorf("并发投递得到 %d 条不同待办：%v，期望 1 条", len(seen), seen)
	}
	if winners != 1 {
		t.Errorf("created=true 出现 %d 次，期望恰好 1 次（否则调用方会以为投递了 8 次人工）", winners)
	}
	if got := humanTaskRowCount(t, database, "sess_race"); got != 1 {
		t.Errorf("库里 %d 行，期望 1 行", got)
	}
}

// 不同会话之间不许互相抑制：subject_id 参与幂等键就是为了这个。
// 若哪天有人把它改成"每个 kind 只留一条开放待办"，本用例就会红。
func TestHumanTaskSvc_SubmitDoesNotDedupeAcrossSubjects(t *testing.T) {
	database := setupHumanTaskSvcDB(t)
	svc := newHumanTaskSvc(t, database, &stubHumanTaskCfg{value: 5})
	ctx := context.Background()
	a := mustSubmit(t, svc, handoffSubmitInput("sess_a"))
	b, created, err := svc.Submit(ctx, handoffSubmitInput("sess_b"))
	if err != nil || !created {
		t.Fatalf("另一个会话的投递被误抑制：(%v,%v)", created, err)
	}
	if a.ID == b.ID {
		t.Error("两个会话拿到同一条待办")
	}
}

// 同一身份换一类待办：必须报错，而不是"复用了那条会话待办"。
// 走到这里只有两种可能 —— 调用方把 kind 填错了，或者有人在给同一张业务表造第二类待办。
// 两种都不能静默：静默复用会让"审批没人裁"这件事记在一条会话待办头上，
// 而这两类看的根本不是同一批人（坐席收件箱里会出现一条谁也裁不了的审批）。
func TestHumanTaskSvc_SubmitRejectsKindMismatchOnSameSubject(t *testing.T) {
	database := setupHumanTaskSvcDB(t)
	svc := newHumanTaskSvc(t, database, &stubHumanTaskCfg{value: 5})
	ctx := context.Background()
	h := mustSubmit(t, svc, handoffSubmitInput("sess_mismatch"))

	lie := handoffSubmitInput("sess_mismatch")
	lie.Kind = model.HumanTaskKindApproval
	due := humanTaskFixedNow.Add(time.Hour)
	lie.SlaDueAt = &due
	_, created, err := svc.Submit(ctx, lie)
	if err == nil {
		t.Fatal("同一 subject 的第二类待办竟然投递成功")
	}
	if created {
		t.Error("kind 不符时不该报新建")
	}
	if !strings.Contains(err.Error(), model.HumanTaskKindConversationHandoff) ||
		!strings.Contains(err.Error(), model.HumanTaskKindApproval) {
		t.Errorf("错误信息应把冲突的两类都点出来：%q", err.Error())
	}
	if !errors.Is(err, ErrHumanTaskInputInvalid) {
		t.Errorf("错误种类 = %v，期望 ErrHumanTaskInputInvalid（400：是调用方写错了）", err)
	}
	// 冲突不能改写原来那条。
	back, gerr := svc.Get(ctx, h.ID)
	if gerr != nil || back == nil {
		t.Fatalf("回读失败：(%v,%v)", back, gerr)
	}
	if back.Kind != model.HumanTaskKindConversationHandoff || back.Status != model.HumanTaskStatusPending {
		t.Errorf("原待办被改写：%s/%s", back.Kind, back.Status)
	}
}

// --- 4. 跃迁（AC① 的后半 + 状态机对外形状）----------------------------------

func TestHumanTaskSvc_ClaimIsHandoffOnly(t *testing.T) {
	database := setupHumanTaskSvcDB(t)
	svc := newHumanTaskSvc(t, database, &stubHumanTaskCfg{value: 5})
	useHumanTaskClock(t, humanTaskFixedNow)
	ctx := context.Background()
	due := humanTaskFixedNow.Add(2 * time.Hour)

	apr := mustSubmit(t, svc, approvalSubmitInput("apr_claim", due))
	got, err := svc.Claim(ctx, apr.ID, "agent-1")
	if err == nil {
		t.Fatal("审批类待办竟然可以被认领")
	}
	if !errors.Is(err, ErrHumanTaskTransition) {
		t.Errorf("错误种类 = %v，期望 ErrHumanTaskTransition（映射 409）", err)
	}
	if got != nil {
		t.Error("失败出口不该回一份改过的行")
	}
	back, rerr := svc.Get(ctx, apr.ID)
	if rerr != nil || back == nil {
		t.Fatalf("回读失败：%v", rerr)
	}
	if back.Status != model.HumanTaskStatusPending || back.AssigneeUserID != "" {
		t.Errorf("审批待办被认领动作改写了：%s/%q（C3：审批不可抢占只能裁决）", back.Status, back.AssigneeUserID)
	}

	cc := mustSubmit(t, svc, HumanTaskSubmitInput{
		Kind: model.HumanTaskKindCollectionEscalation, SubjectType: "collection_case",
		SubjectID: "cc_claim", SlaDueAt: &due,
	})
	if _, err := svc.Claim(ctx, cc.ID, "agent-1"); !errors.Is(err, ErrHumanTaskTransition) {
		t.Errorf("催收类待办的认领 = %v，期望 ErrHumanTaskTransition", err)
	}

	// 会话类可以认领，且认领人落库。
	h := mustSubmit(t, svc, handoffSubmitInput("sess_claim"))
	claimed, err := svc.Claim(ctx, h.ID, "agent-9")
	if err != nil {
		t.Fatalf("会话类认领失败：%v", err)
	}
	if claimed.Status != model.HumanTaskStatusClaimed || claimed.AssigneeUserID != "agent-9" {
		t.Errorf("认领结果 = %s/%q", claimed.Status, claimed.AssigneeUserID)
	}
	if claimed.ClaimedAt == nil {
		t.Error("claimed_at 没落：响应时长指标没有起点")
	}
	if !claimed.ClaimedAt.Equal(humanTaskFixedNow) {
		t.Errorf("claimed_at = %v，期望 %v", claimed.ClaimedAt, humanTaskFixedNow)
	}
}

// 认领失败的几种出口要分得开：404（没这条）、400（入参不合法）、
// 409（状态/类别不允许）。三者给运维的话不同，混成一个就等于没说。
func TestHumanTaskSvc_ActionErrorKinds(t *testing.T) {
	database := setupHumanTaskSvcDB(t)
	svc := newHumanTaskSvc(t, database, &stubHumanTaskCfg{value: 5})
	ctx := context.Background()

	if _, err := svc.Claim(ctx, "ht_not_here", "agent-1"); !errors.Is(err, ErrHumanTaskNotFound) {
		t.Errorf("不存在的待办 = %v，期望 ErrHumanTaskNotFound（404）", err)
	}
	h := mustSubmit(t, svc, handoffSubmitInput("sess_err"))
	if _, err := svc.Release(ctx, h.ID, "agent-1"); !errors.Is(err, ErrHumanTaskTransition) {
		t.Errorf("未认领就释放 = %v，期望 ErrHumanTaskTransition", err)
	}
	if _, err := svc.Claim(ctx, h.ID, "   "); !errors.Is(err, ErrHumanTaskInputInvalid) {
		t.Errorf("空认领人 = %v，期望 ErrHumanTaskInputInvalid（400）", err)
	}
	if _, err := svc.Claim(ctx, "  ", "agent-1"); !errors.Is(err, ErrHumanTaskInputInvalid) {
		t.Errorf("空 ID = %v，期望 ErrHumanTaskInputInvalid", err)
	}
	if _, err := svc.Cancel(ctx, h.ID, "agent-1", "  "); !errors.Is(err, ErrHumanTaskInputInvalid) {
		t.Errorf("无因撤销 = %v，期望 ErrHumanTaskInputInvalid（撤销是有原因的动作）", err)
	}
	if _, err := svc.Complete(ctx, h.ID, ""); !errors.Is(err, ErrHumanTaskInputInvalid) {
		t.Errorf("无主完成 = %v，期望 ErrHumanTaskInputInvalid（谁做的这件事是这张表要回答的问题）", err)
	}
	if _, err := svc.Claim(ctx, h.ID, strings.Repeat("工", 200)); !errors.Is(err, ErrHumanTaskInputInvalid) {
		t.Errorf("超长认领人 = %v，期望 ErrHumanTaskInputInvalid", err)
	}

	// 同一人重复点"认领"：还是 409，但话要说清是"你已认领"而不是"被他人抢了"。
	if _, err := svc.Claim(ctx, h.ID, "agent-mine"); err != nil {
		t.Fatalf("首次认领失败：%v", err)
	}
	_, err := svc.Claim(ctx, h.ID, "agent-mine")
	if !errors.Is(err, ErrHumanTaskTransition) {
		t.Errorf("重复认领 = %v，期望 ErrHumanTaskTransition", err)
	}
	if !strings.Contains(err.Error(), "本人") {
		t.Errorf("重复认领的提示应说明是本人：%q", err.Error())
	}
	if _, err := svc.Claim(ctx, h.ID, "agent-other"); !strings.Contains(err.Error(), "他人") {
		t.Errorf("他人认领的提示应说明被他人抢了")
	}
}

// 释放必须把认领人退回池子：只改状态不清认领人，"谁现在在做这件事"就长期是错的人。
func TestHumanTaskSvc_ReleaseClearsAssignee(t *testing.T) {
	database := setupHumanTaskSvcDB(t)
	svc := newHumanTaskSvc(t, database, &stubHumanTaskCfg{value: 5})
	ctx := context.Background()

	h := mustSubmit(t, svc, handoffSubmitInput("sess_rel"))
	if _, err := svc.Claim(ctx, h.ID, "agent-a"); err != nil {
		t.Fatalf("认领失败：%v", err)
	}
	released, err := svc.Release(ctx, h.ID, "agent-a")
	if err != nil {
		t.Fatalf("释放失败：%v", err)
	}
	if released.Status != model.HumanTaskStatusPending {
		t.Errorf("释放后状态 = %s，期望 pending", released.Status)
	}
	if released.AssigneeUserID != "" {
		t.Errorf("释放后认领人仍是 %q", released.AssigneeUserID)
	}
	if released.ClaimedAt != nil {
		t.Errorf("释放后 claimed_at 仍为 %v（下一次认领的响应时长起点会被算成上一次的）", released.ClaimedAt)
	}
	// 释放后别人能认领（C3 那句"会话可抢占"走的是释放再认领，不是覆盖别人的认领）。
	if _, err := svc.Claim(ctx, h.ID, "agent-b"); err != nil {
		t.Errorf("释放后认领失败：%v", err)
	}
	// 非认领人不能替别人释放：那等于把别人的活儿抢走还不留痕。
	if _, err := svc.Release(ctx, h.ID, "agent-c"); !errors.Is(err, ErrHumanTaskNotHolder) {
		t.Errorf("旁人释放 = %v，期望 ErrHumanTaskNotHolder（映射 403）", err)
	}
	back, _ := svc.Get(ctx, h.ID)
	if back.AssigneeUserID != "agent-b" {
		t.Errorf("被拒的释放改写了库：%q", back.AssigneeUserID)
	}
}

// 完成时记下的是**做完的人**，即使他不是当初认领的那个人。
// （否则"我认领了但同事替我做了"会一直挂在认领人名下，坐席工作量当场虚高。）
func TestHumanTaskSvc_CompleteRecordsCompleter(t *testing.T) {
	database := setupHumanTaskSvcDB(t)
	svc := newHumanTaskSvc(t, database, &stubHumanTaskCfg{value: 5})
	useHumanTaskClock(t, humanTaskFixedNow)
	ctx := context.Background()

	h := mustSubmit(t, svc, handoffSubmitInput("sess_done"))
	if _, err := svc.Claim(ctx, h.ID, "agent-a"); err != nil {
		t.Fatalf("认领失败：%v", err)
	}
	done, err := svc.Complete(ctx, h.ID, "agent-b")
	if err != nil {
		t.Fatalf("完成失败：%v", err)
	}
	if done.Status != model.HumanTaskStatusDone {
		t.Errorf("状态 = %s，期望 done", done.Status)
	}
	if done.AssigneeUserID != "agent-b" {
		t.Errorf("完成人没记下：%q", done.AssigneeUserID)
	}
	if done.CompletedAt == nil || !done.CompletedAt.Equal(humanTaskFixedNow) {
		t.Errorf("completed_at = %v，期望 %v", done.CompletedAt, humanTaskFixedNow)
	}
	// 终态不许原地再处理。
	if _, err := svc.Complete(ctx, h.ID, "agent-c"); !errors.Is(err, ErrHumanTaskTransition) {
		t.Errorf("重复完成 = %v，期望 ErrHumanTaskTransition", err)
	}
	if _, err := svc.Cancel(ctx, h.ID, "agent-c", "手滑"); !errors.Is(err, ErrHumanTaskTransition) {
		t.Errorf("完成后撤销 = %v，期望 ErrHumanTaskTransition", err)
	}
}

// 未认领也能直接完成（坐席在待办中心一次性处理完，不走认领那一步）。
func TestHumanTaskSvc_CompleteFromPending(t *testing.T) {
	database := setupHumanTaskSvcDB(t)
	svc := newHumanTaskSvc(t, database, &stubHumanTaskCfg{value: 5})
	ctx := context.Background()
	apr := mustSubmit(t, svc, approvalSubmitInput("apr_direct", humanTaskFixedNow.Add(time.Hour)))
	done, err := svc.Complete(ctx, apr.ID, "boss-1")
	if err != nil {
		t.Fatalf("pending 直接完成失败：%v", err)
	}
	if done.Status != model.HumanTaskStatusDone || done.AssigneeUserID != "boss-1" {
		t.Errorf("结果 = %s/%q", done.Status, done.AssigneeUserID)
	}
}

// 撤销落终态并留下理由（事后要能回答"这条为什么没人做完就结束了"）。
func TestHumanTaskSvc_CancelRecordsReason(t *testing.T) {
	database := setupHumanTaskSvcDB(t)
	svc := newHumanTaskSvc(t, database, &stubHumanTaskCfg{value: 5})
	useHumanTaskClock(t, humanTaskFixedNow)
	ctx := context.Background()

	apr := mustSubmit(t, svc, approvalSubmitInput("apr_cancel", humanTaskFixedNow.Add(time.Hour)))
	got, err := svc.Cancel(ctx, apr.ID, "boss-1", "策略改为自动放行")
	if err != nil {
		t.Fatalf("撤销失败：%v", err)
	}
	if got.Status != model.HumanTaskStatusCancelled || got.CancelReason != "策略改为自动放行" {
		t.Errorf("撤销结果 = %s/%q", got.Status, got.CancelReason)
	}
	if got.CancelledAt == nil || !got.CancelledAt.Equal(humanTaskFixedNow) {
		t.Errorf("cancelled_at = %v", got.CancelledAt)
	}
	// 撤销长标题/长理由的边界：理由比标题长（撤销说明往往就是那段话）。
	longReason := strings.Repeat("由", 2000)
	if _, err := svc.Cancel(ctx, apr.ID, "boss-1", longReason); err == nil {
		t.Error("已落定的待办竞然再次撤销成功")
	}
}

// CAS 输了（判据通过但别人抢先把状态改了）必须报"冲突"而不是"成功"：
// 否则两个人都以为是自己处理了这条待办。
//
// 触发方式是给仓储套一层壳：读到快照之后先把那一行改掉，模拟"读完之后被抢"。
// 用真并发测不 pinned —— 十次里可能九次都恰好没有那个窗口。
func TestHumanTaskSvc_LostRaceReportsConflict(t *testing.T) {
	database := setupHumanTaskSvcDB(t)
	plain := repository.NewHumanTaskRepositoryWithDB(database)
	ctx := context.Background()

	base := NewHumanTaskService(plain, &stubHumanTaskCfg{value: 5})
	h := mustSubmit(t, base, handoffSubmitInput("sess_lost"))

	racy := &humanTaskStealRepo{HumanTaskRepository: plain, id: h.ID, t: t}
	svc := NewHumanTaskService(racy, &stubHumanTaskCfg{value: 5})
	_, err := svc.Claim(ctx, h.ID, "agent-late")
	if !errors.Is(err, ErrHumanTaskLost) {
		t.Fatalf("被抢先的认领 = %v，期望 ErrHumanTaskLost（409-冲突）", err)
	}
	if !strings.Contains(err.Error(), h.ID) {
		t.Errorf("冲突提示应带上是哪条待办：%q", err.Error())
	}
}

// humanTaskStealRepo 只在读到 pending 快照后抢跑一次认领，**并把那份旧快照原样返回**
// （复现"读完之后被别人改走"）；其余方法透传。
type humanTaskStealRepo struct {
	repository.HumanTaskRepository
	id string
	t  *testing.T
}

func (s *humanTaskStealRepo) GetByID(ctx context.Context, id string) (*model.HumanTask, error) {
	stale, err := s.HumanTaskRepository.GetByID(ctx, id)
	if err != nil || id != s.id || stale == nil || stale.Status != model.HumanTaskStatusPending {
		return stale, err
	}
	ok, aerr := s.HumanTaskRepository.ApplyAction(ctx, s.id, model.HumanTaskStatusPending, func(m *model.HumanTask) error {
		m.Status = model.HumanTaskStatusClaimed
		m.AssigneeUserID = "agent-first"
		return nil
	})
	if aerr != nil || !ok {
		s.t.Fatalf("前置条件破了：抢跑的那次认领没生效 (%v,%v)", ok, aerr)
	}
	return stale, nil
}

// --- 5. 读数装配（AC③ + AC④）------------------------------------------------

func TestHumanTaskSvc_CountsPerKind(t *testing.T) {
	database := setupHumanTaskSvcDB(t)
	svc := newHumanTaskSvc(t, database, &stubHumanTaskCfg{value: 5})
	useHumanTaskClock(t, humanTaskFixedNow)
	ctx := context.Background()

	// 会话类：一条已逾期（自己那一档），一条尚在未来。
	over := mustSubmit(t, svc, handoffSubmitInput("sess_cnt_over"))
	future := mustSubmit(t, svc, handoffSubmitInput("sess_cnt_future"))
	if _, err := svc.Claim(ctx, over.ID, "agent-x"); err != nil {
		t.Fatalf("认领失败：%v", err)
	}
	// "已逾期"这条靠仓储改时刻造出来：服务层没有改 SLA 的入口 —— 那正是 AC① 要的形状。
	if err := database.Model(&model.HumanTask{}).Where("id = ?", over.ID).
		UpdateColumn("sla_first_response_at", humanTaskFixedNow.Add(-time.Minute)).Error; err != nil {
		t.Fatalf("造逾期失败：%v", err)
	}
	// 审批类：截止在过去 ⇒ 逾期。
	mustSubmit(t, svc, approvalSubmitInput("apr_cnt", humanTaskFixedNow.Add(-time.Hour)))
	// 催收类：一条开放、未逾期。
	escalate := humanTaskFixedNow.Add(48 * time.Hour)
	mustSubmit(t, svc, HumanTaskSubmitInput{
		Kind: model.HumanTaskKindCollectionEscalation, SubjectType: "collection_case",
		SubjectID: "cc_cnt", SlaDueAt: &escalate,
	})
	// 已完成的会话待办不进开放数。
	if _, err := svc.Complete(ctx, future.ID, "agent-x"); err != nil {
		t.Fatalf("完成失败：%v", err)
	}

	counts, err := svc.Counts(ctx)
	if err != nil {
		t.Fatalf("Counts 报错：%v", err)
	}
	if !counts.At.Equal(humanTaskFixedNow) {
		t.Errorf("at = %v，期望 %v（逾期判据用的时刻必须回显，否则读数无法复算）", counts.At, humanTaskFixedNow)
	}
	if len(counts.ByKind) != len(model.HumanTaskKinds) {
		t.Fatalf("by_kind 有 %d 项，期望 %d 项（每类都要有键）", len(counts.ByKind), len(model.HumanTaskKinds))
	}
	want := map[string][2]int64{ // kind → {open, overdue}
		model.HumanTaskKindConversationHandoff:  {1, 1}, // over 那条（已认领仍算开放）
		model.HumanTaskKindApproval:             {1, 1},
		model.HumanTaskKindCollectionEscalation: {1, 0},
	}
	seen := map[string]bool{}
	for i, row := range counts.ByKind {
		if row.Kind != model.HumanTaskKinds[i] {
			t.Errorf("第 %d 项 kind = %s，期望按 %v 的顺序", i, row.Kind, model.HumanTaskKinds)
		}
		seen[row.Kind] = true
		w := want[row.Kind]
		if row.Open != w[0] {
			t.Errorf("%s 开放数 = %d，期望 %d", row.Kind, row.Open, w[0])
		}
		if row.Overdue != w[1] {
			t.Errorf("%s 逾期数 = %d，期望 %d", row.Kind, row.Overdue, w[1])
		}
	}
	for _, k := range model.HumanTaskKinds {
		if !seen[k] {
			t.Errorf("读数里缺 %s 这一类", k)
		}
	}
	if counts.TotalOpen != 3 {
		t.Errorf("total_open = %d，期望 3（三类开放数之和，不是全表行数）", counts.TotalOpen)
	}
}

// 库里出现值域外的 kind 时（历史脏数据 / 别的分支写进来一行），聚合必须把它**报出来**
// 而不是当成没有：少一行的读数与零一行是两件事。
func TestHumanTaskSvc_CountsReportsUnknownKind(t *testing.T) {
	database := setupHumanTaskSvcDB(t)
	svc := newHumanTaskSvc(t, database, &stubHumanTaskCfg{value: 5})
	ctx := context.Background()
	strange := &model.HumanTask{
		ID: "ht_unknown_kind", Kind: "refund_review", Status: model.HumanTaskStatusPending,
		SubjectType: "refund_case", SubjectID: "rf_1", CreatedAt: humanTaskFixedNow,
	}
	if err := repository.NewHumanTaskRepositoryWithDB(database).Insert(ctx, strange); err != nil {
		t.Fatalf("直写未知 kind 失败：%v", err)
	}
	counts, err := svc.Counts(ctx)
	if err != nil {
		t.Fatalf("Counts 报错：%v", err)
	}
	if counts.UnknownOpen != 1 {
		t.Errorf("unknown_open = %d，期望 1（这一行不属于任何已知类，必须单独报，不能被三类之和吃掉）", counts.UnknownOpen)
	}
}

// AC④ 的服务出口侧：跨档脏行（会话类却填了审批档）不许进任何一类的逾期数。
// 仓储用例已钉住 SQL，这里钉的是"服务没有替它兜底把三类合并算"。
func TestHumanTaskSvc_CountsNotPollutedByCrossColumnRow(t *testing.T) {
	database := setupHumanTaskSvcDB(t)
	svc := newHumanTaskSvc(t, database, &stubHumanTaskCfg{value: 5})
	useHumanTaskClock(t, humanTaskFixedNow)
	ctx := context.Background()

	dirty := humanTaskFixedNow.Add(-time.Hour)
	row := &model.HumanTask{
		ID: "ht_dirty_1", Kind: model.HumanTaskKindConversationHandoff,
		Status: model.HumanTaskStatusPending, SubjectType: "customer_session",
		SubjectID: "sess_dirty", Title: "跨档脏行", CreatedAt: humanTaskFixedNow,
		SlaDecideAt: &dirty,
	}
	if err := repository.NewHumanTaskRepositoryWithDB(database).Insert(ctx, row); err != nil {
		t.Fatalf("直写脏行失败：%v", err)
	}
	counts, err := svc.Counts(ctx)
	if err != nil {
		t.Fatalf("Counts 报错：%v", err)
	}
	for _, r := range counts.ByKind {
		if r.Overdue != 0 {
			t.Errorf("%s 逾期数 = %d，期望 0（脏行自己那一档是 NULL，不该被别的档救活）", r.Kind, r.Overdue)
		}
	}
	if counts.TotalOpen != 1 {
		t.Errorf("total_open = %d，期望 1（开放数按状态算，与 SLA 列无关）", counts.TotalOpen)
	}
}

// 底座读不动时必须报错，不能回一份零计数：
// "现在一条待办都没有"是一句业务结论，拿一次查询故障去支撑它就是假绿。
func TestHumanTaskSvc_ReportsUnavailableHandle(t *testing.T) {
	svc := NewHumanTaskService(repository.NewHumanTaskRepositoryWithDB(nil), &stubHumanTaskCfg{value: 5})
	ctx := context.Background()
	if svc.Available() {
		t.Error("nil 句柄时 Available 应为 false")
	}
	if _, err := svc.Counts(ctx); err == nil {
		t.Error("Counts 应报错而不是回零计数")
	}
	if _, _, err := svc.Submit(ctx, handoffSubmitInput("sess_nil")); err == nil {
		t.Error("Submit 应报错")
	}
	if _, _, err := svc.List(ctx, repository.HumanTaskQuery{}); err == nil {
		t.Error("List 应报错")
	}
	if _, err := svc.Get(ctx, "ht_x"); err == nil {
		t.Error("Get 应报错")
	}
	if _, err := svc.Complete(ctx, "ht_x", "agent-1"); err == nil {
		t.Error("Complete 应报错")
	}
	if _, _, err := svc.CancelOpenBySubject(ctx, "customer_session", "s1", "r"); err == nil {
		t.Error("CancelOpenBySubject 应报错")
	}
}

// --- 6. 撤销出口（会话侧钩子用）----------------------------------------------

// 会话被解决/关闭时那条待办必须一起落定，否则它会一直躺在池子里，
// 未读数和逾期数都算着一条其实已经没人等的事。
func TestHumanTaskSvc_CancelOpenBySubject(t *testing.T) {
	database := setupHumanTaskSvcDB(t)
	svc := newHumanTaskSvc(t, database, &stubHumanTaskCfg{value: 5})
	useHumanTaskClock(t, humanTaskFixedNow)
	ctx := context.Background()

	// 没投过时撤销：(nil, false, nil)，不是错误（会话自己关掉的，本来就没有待办）。
	if task, cancelled, err := svc.CancelOpenBySubject(ctx, "customer_session", "sess_none", "会话已关闭"); err != nil || cancelled || task != nil {
		t.Errorf("无开放待办时 = (%v,%v,%v)，期望 (nil,false,nil)", task, cancelled, err)
	}

	open := mustSubmit(t, svc, handoffSubmitInput("sess_auto"))
	if _, err := svc.Claim(ctx, open.ID, "agent-a"); err != nil {
		t.Fatalf("认领失败：%v", err)
	}
	task, cancelled, err := svc.CancelOpenBySubject(ctx, "customer_session", "sess_auto", "会话已自动解决")
	if err != nil || !cancelled {
		t.Fatalf("撤销失败：(%v,%v)", cancelled, err)
	}
	if task.Status != model.HumanTaskStatusCancelled || task.CancelReason != "会话已自动解决" {
		t.Errorf("撤销结果 = %s/%q", task.Status, task.CancelReason)
	}
	if task.CancelledAt == nil || !task.CancelledAt.Equal(humanTaskFixedNow) {
		t.Errorf("cancelled_at = %v", task.CancelledAt)
	}
	// 幂等：同一件事再说一次撤销，不报错也不重复落时间。
	if _, cancelled, err := svc.CancelOpenBySubject(ctx, "customer_session", "sess_auto", "再来一次"); err != nil || cancelled {
		t.Errorf("重复撤销 = (%v,%v)，期望 (nil,false,nil) 而不是报错", cancelled, err)
	}
	if n := humanTaskRowCount(t, database, "sess_auto"); n != 1 {
		t.Errorf("撤销动作多写了 %d 行", n)
	}
	// 空 subject 必须拒：拿空串去查会命中库里任意一条，撤销别人的待办。
	if _, _, err := svc.CancelOpenBySubject(ctx, "customer_session", "  ", "手滑"); !errors.Is(err, ErrHumanTaskInputInvalid) {
		t.Errorf("空身份撤销 = %v，期望 ErrHumanTaskInputInvalid", err)
	}
	// 无因撤销同样拒（撤销理由要能事后回答"为什么这条没人处理就结束了"）。
	if _, _, err := svc.CancelOpenBySubject(ctx, "customer_session", "sess_auto", "  "); !errors.Is(err, ErrHumanTaskInputInvalid) {
		t.Errorf("无因撤销 = %v，期望 ErrHumanTaskInputInvalid", err)
	}
}

// --- 7. 读侧透传 --------------------------------------------------------------

// 列表过滤口径（kind/status/认领人 + 分页）在仓储侧已逐条钉过；服务侧只钉三件事：
// 结果原样透传（不擅自重排/截断）、错误不被吞、以及入参归一（大小写与空白）。
func TestHumanTaskSvc_ListPassthrough(t *testing.T) {
	database := setupHumanTaskSvcDB(t)
	svc := newHumanTaskSvc(t, database, &stubHumanTaskCfg{value: 5})
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		mustSubmit(t, svc, handoffSubmitInput(fmt.Sprintf("sess_list_%d", i)))
	}
	rows, total, err := svc.List(ctx, repository.HumanTaskQuery{
		Kinds: []string{model.HumanTaskKindConversationHandoff}, Page: 1, PageSize: 2,
	})
	if err != nil {
		t.Fatalf("List 报错：%v", err)
	}
	if len(rows) != 2 || total != 3 {
		t.Errorf("List = %d 行/total %d，期望 2 行/total 3", len(rows), total)
	}
	if _, _, err := svc.List(ctx, repository.HumanTaskQuery{Kinds: []string{"nope"}}); !errors.Is(err, ErrHumanTaskInputInvalid) {
		t.Errorf("非法过滤 = %v，期望把仓储的入参错误透出来（controller 靠它映射 400）", err)
	}
	// 进程内调用（待办之外的批量读、以及未来的导出）允许未分页；HTTP 那一侧由 controller 强制分页。
	all, _, err := svc.List(ctx, repository.HumanTaskQuery{})
	if err != nil {
		t.Fatalf("默认 List 报错：%v", err)
	}
	if len(all) != 3 {
		t.Errorf("默认视图 %d 行，期望 3 行", len(all))
	}
	// kind 入参大小写归一（前端把 approval 写成 Approval 不该得到空列表）。
	upper, _, err := svc.List(ctx, repository.HumanTaskQuery{Kinds: []string{" Approval "}})
	if err != nil {
		t.Errorf("大写 kind 被拒：%v", err)
	}
	if len(upper) != 0 {
		t.Errorf("大写 kind 命中 %d 行，期望 0 行（库里只有会话类）", len(upper))
	}
}

// Get 的 404 与 503 分开：不存在给 (nil, nil)，由 controller 决定 404。
func TestHumanTaskSvc_GetMissingIsNotError(t *testing.T) {
	database := setupHumanTaskSvcDB(t)
	svc := newHumanTaskSvc(t, database, &stubHumanTaskCfg{value: 5})
	got, err := svc.Get(context.Background(), "ht_absent")
	if err != nil || got != nil {
		t.Errorf("不存在的待办 = (%v,%v)，期望 (nil,nil)", got, err)
	}
}

// --- 参数坐标一致性 -------------------------------------------------------------

// seed 表里的 group/key 是字面量，服务侧读的是两个常量 —— 两边打错任意一处都**不会报错**，
// 只会让运维在管理端改的那个数字永远不生效（GetInt 安静回退默认值），而"改参数没反应"
// 这件事在故障单里通常要过几周才会被想起是键名的问题。所以这一条逐字比，不比对数。
func TestHumanTaskSeedKeyMatchesServiceConstants(t *testing.T) {
	var hit *ParamDef
	n := 0
	defs := DefaultParamDefs()
	for i := range defs {
		if defs[i].Group == HumanTaskConfigGroup && defs[i].Key == HumanTaskHandoffSlaKey {
			n++
			hit = &defs[i]
		}
	}
	if n != 1 {
		t.Fatalf("seed 里 %s.%s 命中 %d 次，期望 1 次（0 = 管理端改不到这个键；>1 = 两条 seed 互相打架）",
			HumanTaskConfigGroup, HumanTaskHandoffSlaKey, n)
	}
	if hit.ValueType != "int" {
		t.Errorf("value_type = %q，服务侧按 int 读（Get），配成别的会一路走 fallback", hit.ValueType)
	}

	// 默认值两侧必须同一个数：否则"运维从没配过"与"运维照着默认填了一遍"两种状态行为不同。
	def, err := strconv.Atoi(hit.DefaultValue)
	if err != nil || def != DefaultHumanTaskHandoffSlaMinutes {
		t.Errorf("seed 默认 %q ≠ 服务默认 %d（err=%v）", hit.DefaultValue, DefaultHumanTaskHandoffSlaMinutes, err)
	}
	// 管理端的可填区间必须正好等于服务侧认的合法区间：
	// 放得进 0 就等于允许"一落库即逾期"，逾期读数当场被垃圾填满。
	minV, err1 := strconv.Atoi(*hit.Min)
	maxV, err2 := strconv.Atoi(*hit.Max)
	if err1 != nil || err2 != nil {
		t.Fatalf("seed 的 min/max 不是数字：%q / %q", *hit.Min, *hit.Max)
	}
	if minV != 1 || maxV != MaxHumanTaskHandoffSlaMinutes {
		t.Errorf("seed 区间 [%d,%d] ≠ 服务侧合法区间 [1,%d]", minV, maxV, MaxHumanTaskHandoffSlaMinutes)
	}
}
