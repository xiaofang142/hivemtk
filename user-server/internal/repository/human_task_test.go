// human_task_test.go T-P3-03：统一待办仓储（表 human_tasks）。
//
// 本文件只测**只有真库才能证明**的五件事：
//  1. 那条部分唯一索引真的带谓词（不带谓词 = 处理完的待办永远占着坑，同一件事不能转第二次）；
//  2. "开放"占坑、"落定"让坑，且**两个终态都让坑**（done 与 cancelled；AC② 不重复投递的库侧前提）；
//  3. 冲突识别认约束名，不认"任何 23505"（把主键冲突读成"已有开放待办"会静默复用别人那行）；
//  4. ApplyAction 是"加锁读—判态—按列白名单写回"一步：并发认领只有一个赢家，
//     而且 kind / 身份 / SLA 三组列改不动（改得动就等于把一条待办挪到别的会话上）；
//  5. 三类待办的读数互不串档（AC③ 过滤 + AC④ 指标隔离）——含一行**跨档脏数据**的靶子。
package repository

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

func setupHumanTaskTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return testutil.NewTestDB(t, &model.HumanTask{})
}

// humanTaskBaseAt 是所有造行共用的入队时刻：断言一半在排序与逾期判据上，
// 依赖 GORM 的 autoCreateTime 会让"同一秒插入的两行谁在前"变成随机答案。
var humanTaskBaseAt = time.Date(2026, 9, 20, 8, 0, 0, 0, time.UTC)

// newHumanTaskRow 造一行待办。**不**在这里顺手填 SLA：哪一列该填由 kind 决定，
// 造行函数替调用方填好就等于把"填错列"这个缺陷在测试数据里预先抹掉。
func newHumanTaskRow(id, kind, subjectType, subjectID, status string) *model.HumanTask {
	return &model.HumanTask{
		ID:          id,
		Kind:        kind,
		Status:      status,
		SubjectType: subjectType,
		SubjectID:   subjectID,
		Title:       "待办 " + id,
		CreatedAt:   humanTaskBaseAt,
		UpdatedAt:   humanTaskBaseAt,
	}
}

func withHandoffSLA(t *model.HumanTask, due time.Time) *model.HumanTask {
	at := due
	t.SlaFirstResponseAt = &at
	return t
}

// humanTaskPGCast PG 在表达式里给列名与字面量补的类型转换（实测回显里有 ::text）。
var humanTaskPGCast = regexp.MustCompile(`::[a-zA-Z_]+`)

// humanTaskNormalizeSQL 把一段 SQL 表达式折成可比对的最小写法：**只**抹 PG 自己加的
// 语法糖（类型转换、括号、多余空白），比较符、列名、字符串字面量一个字符都不动。
// 于是"少一个终态""把 <> 写成 =""状态名拼错"三类真漂移照样会红 ——
// 归一化若顺手把语义也归了，这条比对就退化成永远为真。
func humanTaskNormalizeSQL(s string) string {
	s = humanTaskPGCast.ReplaceAllString(s, " ")
	s = strings.NewReplacer("(", " ", ")", " ").Replace(s)
	return strings.Join(strings.Fields(s), " ")
}

func TestHumanTaskRepo_NilHandle(t *testing.T) {
	repo := NewHumanTaskRepositoryWithDB(nil)
	if repo.Available() {
		t.Fatal("nil 句柄时 Available 应为 false")
	}
	ctx := context.Background()
	// 每个方法都要报错而不是回空结果：待办的读侧是"有哪些事在等人"，
	// 把一次故障读成"没有事在等人"，运维会照着这句话去判断"人工没被卡住"。
	if err := repo.Insert(ctx, newHumanTaskRow("ht_1", model.HumanTaskKindConversationHandoff, "customer_session", "s1", model.HumanTaskStatusPending)); err == nil {
		t.Error("Insert 应报错")
	}
	if got, err := repo.GetByID(ctx, "ht_1"); err == nil || got != nil {
		t.Errorf("GetByID 应报错且不给行，实际 (%v,%v)", got, err)
	}
	if got, err := repo.GetOpenBySubject(ctx, "customer_session", "s1"); err == nil || got != nil {
		t.Errorf("GetOpenBySubject 应报错且不给行，实际 (%v,%v)", got, err)
	}
	if ok, err := repo.ApplyAction(ctx, "ht_1", model.HumanTaskStatusPending, func(*model.HumanTask) error { return nil }); err == nil || ok {
		t.Errorf("ApplyAction 应报错且判未生效，实际 (%v,%v)", ok, err)
	}
	if rows, total, err := repo.List(ctx, HumanTaskQuery{}); err == nil || rows != nil || total != 0 {
		t.Errorf("List 应报错且不给结果，实际 (%v,%v,%v)", rows, total, err)
	}
	if m, err := repo.CountOpenByKind(ctx); err == nil || m != nil {
		t.Errorf("CountOpenByKind 应报错且不给映射，实际 (%v,%v)", m, err)
	}
	if m, err := repo.CountOverdueOpenByKind(ctx, humanTaskBaseAt); err == nil || m != nil {
		t.Errorf("CountOverdueOpenByKind 应报错且不给映射，实际 (%v,%v)", m, err)
	}
}

func TestHumanTaskRepo_InsertAndReadBack(t *testing.T) {
	repo := NewHumanTaskRepositoryWithDB(setupHumanTaskTestDB(t))
	ctx := context.Background()
	row := withHandoffSLA(newHumanTaskRow("ht_ins_1", model.HumanTaskKindConversationHandoff,
		"customer_session", "sess_ins_1", model.HumanTaskStatusPending), humanTaskBaseAt.Add(5*time.Minute))
	row.OneID = "one_ins"
	if err := repo.Insert(ctx, row); err != nil {
		t.Fatalf("Insert 失败：%v", err)
	}
	got, err := repo.GetByID(ctx, row.ID)
	if err != nil || got == nil {
		t.Fatalf("回读失败：(%v,%v)", got, err)
	}
	if got.Kind != row.Kind || got.Status != row.Status || got.SubjectID != row.SubjectID {
		t.Errorf("回读与写入不符：%s/%s/%s", got.Kind, got.Status, got.SubjectID)
	}
	if got.SlaFirstResponseAt == nil || !got.SlaFirstResponseAt.Equal(*row.SlaFirstResponseAt) {
		t.Errorf("SLA 列回读 = %v，期望 %v", got.SlaFirstResponseAt, *row.SlaFirstResponseAt)
	}
	// 不存在的 ID 必须读成 (nil, nil)，而不是 (nil, error)：
	// 上层要区分"这条待办不存在"（404）与"库读不动"（503）。
	missing, err := repo.GetByID(ctx, "ht_nope")
	if err != nil || missing != nil {
		t.Errorf("不存在的 ID 应给 (nil,nil)，实际 (%v,%v)", missing, err)
	}
}

// TestHumanTaskRepo_OpenIndexIsPartial AC② 的库侧地基：占坑的只有开放态。
//
// 若索引不带谓词（把 status 从 WHERE 里删掉是最常见的手滑），第二次转人工就会撞 UNIQUE，
// 而调用方为了不让转人工失败会把这次写吞掉 —— 于是"这个会话被转过两次人工"这件事
// 在库里永远只有一行，未读数也会长期偏小。
func TestHumanTaskRepo_OpenIndexIsPartial(t *testing.T) {
	database := setupHumanTaskTestDB(t)
	repo := NewHumanTaskRepositoryWithDB(database)
	ctx := context.Background()

	var def string
	if err := database.Raw(
		"SELECT indexdef FROM pg_indexes WHERE tablename = 'human_tasks' AND indexname = 'uq_human_task_open'",
	).Scan(&def).Error; err != nil {
		t.Fatalf("查索引定义失败：%v", err)
	}
	if def == "" {
		t.Fatal("没有 uq_human_task_open 这条索引：部分唯一没建成，重复投递无人拦")
	}
	if !strings.Contains(def, "WHERE") {
		t.Fatalf("索引不是部分的：%s", def)
	}
	// 逐字比对前先各抹一次 PG 的**语法糖**（见 humanTaskNormalizeSQL）：
	// 实测 PG 回显是 `WHERE (((status)::text <> 'done'::text) AND ((status)::text <> 'cancelled'::text))`，
	// 与 Go 侧那条谓词只差类型转换和括号 —— 拿 Contains 直接比会常年红。
	whereIdx := strings.Index(def, " WHERE ")
	if whereIdx < 0 {
		t.Fatalf("索引定义里找不到 WHERE 段：%s", def)
	}
	gotPred := humanTaskNormalizeSQL(def[whereIdx+len(" WHERE "):])
	wantPred := humanTaskNormalizeSQL(model.HumanTaskOpenPredicateSQL())
	if gotPred != wantPred {
		t.Errorf("索引谓词与 Go 侧开放态口径不一致：\n  库 %s\n  Go %s", gotPred, wantPred)
	}

	// 同一会话两行开放 → 第二行必撞。
	first := newHumanTaskRow("ht_p1", model.HumanTaskKindConversationHandoff, "customer_session", "sess_part", model.HumanTaskStatusPending)
	if err := repo.Insert(ctx, first); err != nil {
		t.Fatalf("首行写入失败：%v", err)
	}
	second := newHumanTaskRow("ht_p2", first.Kind, first.SubjectType, first.SubjectID, model.HumanTaskStatusClaimed)
	if err := repo.Insert(ctx, second); !errors.Is(err, ErrHumanTaskOpenConflict) {
		t.Errorf("两行开放应判 ErrHumanTaskOpenConflict，实际 %v", err)
	}

	// 第一条落定后同一对象可以再开一条（第二次转人工看得见）。
	if ok, err := repo.ApplyAction(ctx, first.ID, model.HumanTaskStatusPending, func(m *model.HumanTask) error {
		m.Status = model.HumanTaskStatusDone
		now := humanTaskBaseAt.Add(time.Minute)
		m.CompletedAt = &now
		return nil
	}); err != nil || !ok {
		t.Fatalf("第一条待办未能落定：(%v,%v)", ok, err)
	}
	if err := repo.Insert(ctx, second); err != nil {
		t.Errorf("前一条已落定，第二次转人工不该被拦：%v", err)
	}
	// 两行历史共存是本表的证据链取向：转了几次人工要用行数回答。
	var n int64
	if err := database.Model(&model.HumanTask{}).Where("subject_id = ?", "sess_part").Count(&n).Error; err != nil {
		t.Fatalf("计数失败：%v", err)
	}
	if n != 2 {
		t.Errorf("同一会话的两行历史应共存，实际 %d 行", n)
	}
}

// TestHumanTaskRepo_ConflictIdentityIsExact 认约束名而不是认 SQLSTATE。
//
// 靶子是主键冲突：两行同 ID 也是 23505。若只判一半，一次"ID 生成器重号"会被 service
// 读成"这个对象已有开放待办"，于是它去复用那一行、并把这次转人工当作已完成上报 ——
// 复用的是别人的待办，而且没有任何一句报错。
func TestHumanTaskRepo_ConflictIdentityIsExact(t *testing.T) {
	repo := NewHumanTaskRepositoryWithDB(setupHumanTaskTestDB(t))
	ctx := context.Background()
	row := newHumanTaskRow("ht_dup_id", model.HumanTaskKindApproval, "approval_request", "apr_x1", model.HumanTaskStatusPending)
	if err := repo.Insert(ctx, row); err != nil {
		t.Fatalf("写入失败：%v", err)
	}
	sameID := newHumanTaskRow("ht_dup_id", model.HumanTaskKindApproval, "approval_request", "apr_x2", model.HumanTaskStatusPending)
	err := repo.Insert(ctx, sameID)
	if err == nil {
		t.Fatal("同主键两行竟都写进去了")
	}
	if errors.Is(err, ErrHumanTaskOpenConflict) {
		t.Error("主键冲突被误判成开放态冲突（会诱导 service 去复用无关的一行）")
	}
}

// TestHumanTaskRepo_CancelledReleasesTheSlot 终态有两条，撤销这一条也得让坑。
//
// 上面那条用例只走 done。撤销是另一半，而且线上更常踩：会话被"客户已解决/会话关闭"
// 那把钩子自动撤销后，客户又开口、又要转人工 —— 若索引谓词只排掉 done，这条新投递
// 会撞在一条 cancelled 行上。撞了之后 service 回读开放待办又读不到它（Go 侧它不是
// 开放态，回读走的是同一个口径），于是把唯一索引错误原样抛回转人工：会话转了人工、
// 池子里没有这一行，而且一条待办也没人再去补。
//
// 变异靶子（两处同改，避开 tag↔builder 一致性那条比对用例）：把 `status <> 'cancelled'`
// 从索引 tag 与 HumanTaskOpenPredicateSQL 里同时删掉。那时 ①（读到已撤销的旧行）
// 与 ②（第二次投递撞在 cancelled 行上）两步都会红。
func TestHumanTaskRepo_CancelledReleasesTheSlot(t *testing.T) {
	database := setupHumanTaskTestDB(t)
	repo := NewHumanTaskRepositoryWithDB(database)
	ctx := context.Background()

	first := newHumanTaskRow("ht_cxl_1", model.HumanTaskKindConversationHandoff,
		"customer_session", "sess_cxl", model.HumanTaskStatusPending)
	if err := repo.Insert(ctx, first); err != nil {
		t.Fatalf("首行写入失败：%v", err)
	}
	cancelsAt := humanTaskBaseAt.Add(2 * time.Minute)
	ok, err := repo.ApplyAction(ctx, first.ID, model.HumanTaskStatusPending, func(m *model.HumanTask) error {
		m.Status = model.HumanTaskStatusCancelled
		m.CancelledAt = &cancelsAt
		m.CancelReason = "会话已由他人直接回复"
		return nil
	})
	if err != nil || !ok {
		t.Fatalf("撤销未生效：(%v,%v)", ok, err)
	}

	// ① 撤销不是开放态：回读必须给"确实没有"。
	if got, rerr := repo.GetOpenBySubject(ctx, "customer_session", "sess_cxl"); rerr != nil || got != nil {
		t.Fatalf("已撤销的待办被读成开放 (%v,%v)", got, rerr)
	}

	// ② 坑已让出：同一会话可以再投一条。
	second := newHumanTaskRow("ht_cxl_2", model.HumanTaskKindConversationHandoff,
		"customer_session", "sess_cxl", model.HumanTaskStatusPending)
	if err := repo.Insert(ctx, second); err != nil {
		t.Fatalf("前一条已撤销，第二次转人工不该被拦：%v", err)
	}

	// ③ 让坑不等于拆索引：两行同时开放仍必须拦。
	third := newHumanTaskRow("ht_cxl_3", model.HumanTaskKindConversationHandoff,
		"customer_session", "sess_cxl", model.HumanTaskStatusClaimed)
	if err := repo.Insert(ctx, third); !errors.Is(err, ErrHumanTaskOpenConflict) {
		t.Errorf("两行开放应判 ErrHumanTaskOpenConflict，实际 %v", err)
	}

	// ④ 开放态回读读的是新那行，不是被撤销的旧行。
	got, err := repo.GetOpenBySubject(ctx, "customer_session", "sess_cxl")
	if err != nil || got == nil {
		t.Fatalf("应读到那条新开放待办：(%v,%v)", got, err)
	}
	if got.ID != second.ID {
		t.Errorf("开放态读到 %s，期望 %s", got.ID, second.ID)
	}

	// ⑤ 两行历史共存（同上面的证据链取向）。
	var n int64
	if err := database.Model(&model.HumanTask{}).Where("subject_id = ?", "sess_cxl").Count(&n).Error; err != nil {
		t.Fatalf("计数失败：%v", err)
	}
	if n != 2 {
		t.Errorf("撤销后同一会话的两行历史应共存，实际 %d 行", n)
	}
}

func TestHumanTaskRepo_GetOpenBySubject(t *testing.T) {
	repo := NewHumanTaskRepositoryWithDB(setupHumanTaskTestDB(t))
	ctx := context.Background()
	done := newHumanTaskRow("ht_o1", model.HumanTaskKindConversationHandoff, "customer_session", "sess_open", model.HumanTaskStatusDone)
	if err := repo.Insert(ctx, done); err != nil {
		t.Fatalf("写入失败：%v", err)
	}
	// 同对象的旧行已落定 → 现在没有开放待办（不是"读不到"，是"确实没有"）。
	got, err := repo.GetOpenBySubject(ctx, "customer_session", "sess_open")
	if err != nil || got != nil {
		t.Errorf("已落定的行不该被当作开放，实际 (%v,%v)", got, err)
	}
	open := newHumanTaskRow("ht_o2", model.HumanTaskKindConversationHandoff, "customer_session", "sess_open", model.HumanTaskStatusClaimed)
	if err := repo.Insert(ctx, open); err != nil {
		t.Fatalf("写入失败：%v", err)
	}
	got, err = repo.GetOpenBySubject(ctx, "customer_session", "sess_open")
	if err != nil || got == nil {
		t.Fatalf("应读到那条开放待办：(%v,%v)", got, err)
	}
	if got.ID != open.ID {
		t.Errorf("读到 %s，期望 %s", got.ID, open.ID)
	}
	// 空身份必须报错：拿空串去查会命中库里任意一条（First 取哪条由排序决定）。
	if got, err := repo.GetOpenBySubject(ctx, "", ""); err == nil || got != nil {
		t.Errorf("空身份查询应报错，实际 (%v,%v)", got, err)
	}
}

func TestHumanTaskRepo_ApplyActionSingleWinner(t *testing.T) {
	repo := NewHumanTaskRepositoryWithDB(setupHumanTaskTestDB(t))
	ctx := context.Background()
	row := newHumanTaskRow("ht_cas_1", model.HumanTaskKindConversationHandoff, "customer_session", "sess_cas", model.HumanTaskStatusPending)
	if err := repo.Insert(ctx, row); err != nil {
		t.Fatalf("写入失败：%v", err)
	}
	const n = 8
	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make([]bool, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			ok, err := repo.ApplyAction(ctx, row.ID, model.HumanTaskStatusPending, func(m *model.HumanTask) error {
				m.Status = model.HumanTaskStatusClaimed
				m.AssigneeUserID = fmt.Sprintf("agent-%d", i)
				now := humanTaskBaseAt.Add(time.Duration(i) * time.Second)
				m.ClaimedAt = &now
				return nil
			})
			if err != nil {
				results[i] = false
				return
			}
			results[i] = ok
		}(i)
	}
	close(start)
	wg.Wait()

	winners := 0
	for _, ok := range results {
		if ok {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("并发认领期望恰好 1 个赢家，实际 %d（0=写回条件失效，>1=丢了行锁变成互相覆盖）", winners)
	}
	got, err := repo.GetByID(ctx, row.ID)
	if err != nil || got == nil {
		t.Fatalf("回读失败：(%v,%v)", got, err)
	}
	if got.Status != model.HumanTaskStatusClaimed || !strings.HasPrefix(got.AssigneeUserID, "agent-") {
		t.Errorf("最终态 = %s/%q，期望 claimed/agent-*", got.Status, got.AssigneeUserID)
	}
}

// ApplyAction 的行锁存在性观测：fn 停在事务里等信号时，外部对同一行的 UPDATE 必须阻塞。
//
// 摘掉 FOR UPDATE 时这条会红（CAS 用例仍绿 —— 别把功劳记错对象）：fn 里"读到的就是
// 将要写回的那一行"这个前提没了，service 在 fn 内做的判据（"仍开放吗"、"SLA 还没过吗"）
// 就建立在一个可以被别人改掉的快照上。
//
// 释放必须走 defer：t.Fatalf 走 Goexit 后若不 release，卡在事务里的协程不还连接，
// testutil 的 cleanup（Close + DROP DATABASE）会永久等待 —— T-P3-01 实测挂过 40 分钟。
func TestHumanTaskRepo_ApplyActionHoldsRowLockWhileFnRuns(t *testing.T) {
	database := setupHumanTaskTestDB(t)
	repo := NewHumanTaskRepositoryWithDB(database)
	ctx := context.Background()

	row := newHumanTaskRow("ht_lk_1", model.HumanTaskKindConversationHandoff, "customer_session", "sess_lk", model.HumanTaskStatusPending)
	if err := repo.Insert(ctx, row); err != nil {
		t.Fatalf("写入失败：%v", err)
	}

	inside := make(chan struct{})
	releaseOnce := sync.Once{}
	release := func() { releaseOnce.Do(func() { close(inside) }) }
	defer release()

	done := make(chan error, 1)
	go func() {
		_, err := repo.ApplyAction(ctx, row.ID, model.HumanTaskStatusPending, func(m *model.HumanTask) error {
			m.Status = model.HumanTaskStatusClaimed
			close(done) // fn 确实进来了
			<-inside    // 且此刻仍持有行锁
			return nil
		})
		if err != nil {
			done <- err
		}
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("fn 未进入或 ApplyAction 报错：%v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("ApplyAction 迟迟没进 fn")
	}

	blocked := make(chan error, 1)
	go func() {
		err := database.Model(&model.HumanTask{}).Where("id = ?", row.ID).
			Update("title", "外部改写").Error
		blocked <- err
	}()
	select {
	case err := <-blocked:
		release()
		t.Fatalf("外部 UPDATE 没有被行锁挡住（%v）⇒ fn 读到的不是将要写回的那一行", err)
	case <-time.After(300 * time.Millisecond):
	}
	release()
	if err := <-blocked; err != nil {
		t.Fatalf("释放后外部 UPDATE 失败：%v", err)
	}
}

// TestHumanTaskRepo_ApplyActionWriteWhitelist 白名单之外的列一律改不动。
//
// 这三组列各挡一件事：
//   - kind：把一条会话待办改成审批待办 = 让审批类待办被坐席的认领动作处理掉；
//   - subject_*：待办指向哪件事是它的身份，改得动就等于"处理了 A 却记在 B 头上"；
//   - sla_*：SLA 在开放那一刻定死，事后改它等于把逾期读数抹平（指标当场失真）。
func TestHumanTaskRepo_ApplyActionWriteWhitelist(t *testing.T) {
	repo := NewHumanTaskRepositoryWithDB(setupHumanTaskTestDB(t))
	ctx := context.Background()
	row := withHandoffSLA(newHumanTaskRow("ht_wl_1", model.HumanTaskKindConversationHandoff,
		"customer_session", "sess_wl", model.HumanTaskStatusPending), humanTaskBaseAt.Add(5*time.Minute))
	if err := repo.Insert(ctx, row); err != nil {
		t.Fatalf("写入失败：%v", err)
	}
	sneakyDue := humanTaskBaseAt.Add(99 * time.Hour)
	ok, err := repo.ApplyAction(ctx, row.ID, model.HumanTaskStatusPending, func(m *model.HumanTask) error {
		m.Status = model.HumanTaskStatusClaimed
		m.AssigneeUserID = "agent-sneaky"
		m.ID = "ht_wl_HIJACKED"
		m.Kind = model.HumanTaskKindApproval
		m.SubjectType = "customer_session"
		m.SubjectID = "sess_other"
		m.Title = "改了标题"
		m.SlaFirstResponseAt = &sneakyDue
		m.SlaDecideAt = &sneakyDue
		m.CreatedAt = humanTaskBaseAt.Add(-100 * time.Hour)
		return nil
	})
	if err != nil || !ok {
		t.Fatalf("认领未生效：(%v,%v)", ok, err)
	}
	got, err := repo.GetByID(ctx, row.ID)
	if err != nil || got == nil {
		t.Fatalf("按原 ID 回读失败（ID 被改走了？）：(%v,%v)", got, err)
	}
	if got.Kind != model.HumanTaskKindConversationHandoff {
		t.Errorf("kind 被改成 %q：审批类待办会被坐席认领动作处理掉", got.Kind)
	}
	if got.SubjectID != "sess_wl" || got.SubjectType != "customer_session" {
		t.Errorf("身份列被改走：%s/%s", got.SubjectType, got.SubjectID)
	}
	if got.SlaFirstResponseAt == nil || !got.SlaFirstResponseAt.Equal(*row.SlaFirstResponseAt) {
		t.Errorf("SLA 列被改写：%v（期望仍是 %v）", got.SlaFirstResponseAt, *row.SlaFirstResponseAt)
	}
	if got.SlaDecideAt != nil {
		t.Errorf("会话类待办被填上了审批档 SLA：%v", got.SlaDecideAt)
	}
	if !got.CreatedAt.Equal(humanTaskBaseAt) {
		t.Errorf("created_at 被改走：%v", got.CreatedAt)
	}
	// 白名单之内的列必须真的写进去了 —— 只断"改不动"不断"改得动"，等于允许一个整块丢弃写入的实现。
	if got.Status != model.HumanTaskStatusClaimed || got.AssigneeUserID != "agent-sneaky" {
		t.Errorf("可写列没落库：%s/%q", got.Status, got.AssigneeUserID)
	}
	if got.Title != row.Title {
		t.Errorf("title 被写了（%q）：它不在可写列里", got.Title)
	}
}

func TestHumanTaskRepo_ApplyActionExpectsStatus(t *testing.T) {
	repo := NewHumanTaskRepositoryWithDB(setupHumanTaskTestDB(t))
	ctx := context.Background()
	for _, c := range []struct {
		name         string
		storeStatus  string
		expectStatus string
		wantApplied  bool
	}{
		{"期望态与实际一致 → 生效", model.HumanTaskStatusPending, model.HumanTaskStatusPending, true},
		{"已被他人认领 → 本次不生效", model.HumanTaskStatusClaimed, model.HumanTaskStatusPending, false},
		{"已落定 → 不许原地再处理", model.HumanTaskStatusDone, model.HumanTaskStatusPending, false},
		{"已撤销 → 同上", model.HumanTaskStatusCancelled, model.HumanTaskStatusClaimed, false},
	} {
		id := "ht_st_" + strings.ReplaceAll(c.name, " ", "_")
		row := newHumanTaskRow(id, model.HumanTaskKindConversationHandoff, "customer_session", "sess_"+id, c.storeStatus)
		if err := repo.Insert(ctx, row); err != nil {
			t.Fatalf("%s 造行失败：%v", c.name, err)
		}
		called := false
		applied, err := repo.ApplyAction(ctx, id, c.expectStatus, func(*model.HumanTask) error {
			called = true
			return nil
		})
		if err != nil {
			t.Errorf("%s：报错 %v", c.name, err)
		}
		if applied != c.wantApplied {
			t.Errorf("%s：applied = %v，期望 %v", c.name, applied, c.wantApplied)
		}
		if c.wantApplied != called {
			t.Errorf("%s：fn 被调用 = %v，应与 applied 同值（不生效时不该跑修改逻辑）", c.name, called)
		}
	}
	// 不存在的 ID：applied=false 且**不报错**（404 与 503 的差别归 service 用 GetByID 判）。
	applied, err := repo.ApplyAction(ctx, "ht_missing", model.HumanTaskStatusPending, func(*model.HumanTask) error { return nil })
	if err != nil || applied {
		t.Errorf("不存在的行应给 (false,nil)，实际 (%v,%v)", applied, err)
	}
}

// TestHumanTaskRepo_ApplyActionFnErrorRollsBack fn 报错必须整笔回滚。
//
// 这一条是 service 敢把"跃迁合法性判据"放进 fn 的前提：判据跑在持锁的那份快照上，
// 判不过就一行都不写。少了回滚，判据就成了"先写坏再报错"。
func TestHumanTaskRepo_ApplyActionFnErrorRollsBack(t *testing.T) {
	repo := NewHumanTaskRepositoryWithDB(setupHumanTaskTestDB(t))
	ctx := context.Background()
	row := newHumanTaskRow("ht_rb_1", model.HumanTaskKindConversationHandoff, "customer_session", "sess_rb", model.HumanTaskStatusPending)
	if err := repo.Insert(ctx, row); err != nil {
		t.Fatalf("写入失败：%v", err)
	}
	sentinel := errors.New("人工判据不过")
	applied, err := repo.ApplyAction(ctx, row.ID, model.HumanTaskStatusPending, func(m *model.HumanTask) error {
		m.Status = model.HumanTaskStatusClaimed
		m.AssigneeUserID = "should-not-land"
		return sentinel
	})
	if applied {
		t.Error("fn 报错却判生效")
	}
	if err != nil && !errors.Is(err, sentinel) {
		t.Errorf("应把 fn 的错误原样上抛（供 service 判 404/409），实际 %v", err)
	}
	got, err := repo.GetByID(ctx, row.ID)
	if err != nil || got == nil {
		t.Fatalf("回读失败：(%v,%v)", got, err)
	}
	if got.Status != model.HumanTaskStatusPending || got.AssigneeUserID != "" {
		t.Errorf("报错后库里那一行被改写了：%s/%q（期望 pending/空）", got.Status, got.AssigneeUserID)
	}
}

func TestHumanTaskRepo_ListFilters(t *testing.T) {
	repo := NewHumanTaskRepositoryWithDB(setupHumanTaskTestDB(t))
	ctx := context.Background()
	seed := []*model.HumanTask{
		newHumanTaskRow("ht_l_1", model.HumanTaskKindConversationHandoff, "customer_session", "sess_l_1", model.HumanTaskStatusPending),
		newHumanTaskRow("ht_l_2", model.HumanTaskKindConversationHandoff, "customer_session", "sess_l_2", model.HumanTaskStatusClaimed),
		newHumanTaskRow("ht_l_3", model.HumanTaskKindApproval, "approval_request", "apr_l_1", model.HumanTaskStatusPending),
		newHumanTaskRow("ht_l_4", model.HumanTaskKindApproval, "approval_request", "apr_l_2", model.HumanTaskStatusClaimed),
		newHumanTaskRow("ht_l_5", model.HumanTaskKindApproval, "approval_request", "apr_l_3", model.HumanTaskStatusClaimed),
		newHumanTaskRow("ht_l_6", model.HumanTaskKindCollectionEscalation, "collection_case", "cc_l_1", model.HumanTaskStatusDone),
	}
	seed[1].AssigneeUserID = "agent-l"
	seed[3].AssigneeUserID = "agent-l"
	for _, r := range seed {
		if err := repo.Insert(ctx, r); err != nil {
			t.Fatalf("造行 %s 失败：%v", r.ID, err)
		}
	}

	ids := func(rows []*model.HumanTask) string {
		out := make([]string, 0, len(rows))
		for _, r := range rows {
			out = append(out, r.ID)
		}
		return strings.Join(out, ",")
	}
	assertList := func(name string, q HumanTaskQuery, want string, wantTotal int64) {
		t.Helper()
		rows, total, err := repo.List(ctx, q)
		if err != nil {
			t.Fatalf("%s：List 报错 %v", name, err)
		}
		if got := ids(rows); got != want {
			t.Errorf("%s：结果 = %q，期望 %q", name, got, want)
		}
		if total != wantTotal {
			t.Errorf("%s：total = %d，期望 %d（分页总数必须是过滤后的总数，不是本页行数）", name, total, wantTotal)
		}
	}

	// 默认视图：全部三类的开放态，按入队先后（FIFO）排。
	assertList("默认开放态", HumanTaskQuery{}, "ht_l_1,ht_l_2,ht_l_3,ht_l_4,ht_l_5", 5)
	assertList("只要审批类", HumanTaskQuery{Kinds: []string{model.HumanTaskKindApproval}}, "ht_l_3,ht_l_4,ht_l_5", 3)
	assertList("两类合并", HumanTaskQuery{Kinds: []string{
		model.HumanTaskKindConversationHandoff, model.HumanTaskKindCollectionEscalation}}, "ht_l_1,ht_l_2", 2)
	assertList("已处理完的", HumanTaskQuery{Statuses: []string{model.HumanTaskStatusDone}}, "ht_l_6", 1)
	assertList("按认领人过滤", HumanTaskQuery{AssigneeUserID: "agent-l"}, "ht_l_2,ht_l_4", 2)
	assertList("分页第一页", HumanTaskQuery{Page: 1, PageSize: 2}, "ht_l_1,ht_l_2", 5)
	assertList("分页第二页", HumanTaskQuery{Page: 2, PageSize: 2}, "ht_l_3,ht_l_4", 5)

	// 反向测试：过滤值写错必须报错，不能回空列表。
	// "空列表"会被读成"没有待办"这句业务结论，而事实是这次查询压根无效。
	if _, _, err := repo.List(ctx, HumanTaskQuery{Kinds: []string{"handoff"}}); err == nil {
		t.Error("近似拼写的 kind 未被拒（会静默返回空列表）")
	}
	if _, _, err := repo.List(ctx, HumanTaskQuery{Statuses: []string{"expired"}}); err == nil {
		t.Error("不存在的状态值未被拒")
	}
	// 半分页：两种歧义读法各差一整页，含糊入参必须拒而不是挑一种读法。
	for _, q := range []HumanTaskQuery{{Page: 0, PageSize: 2}, {Page: 3}, {Page: -1, PageSize: 5}, {Page: 1, PageSize: -1}} {
		if _, _, err := repo.List(ctx, q); err == nil {
			t.Errorf("page=%d page_size=%d 未被拒", q.Page, q.PageSize)
		}
	}
}

// TestHumanTaskRepo_MaxPageSizeClamped 单页上限是真的在生效：
// 待办中心每次打开都拉一次这条列表，不设上限时一句 page_size=999999 就把整表读进内存。
// 夹住而不是报错：这一路是"用户想看多一点"，不是"用户写错了"。
func TestHumanTaskRepo_MaxPageSizeClamped(t *testing.T) {
	repo := NewHumanTaskRepositoryWithDB(setupHumanTaskTestDB(t))
	ctx := context.Background()
	const rows = humanTaskMaxPageSize + 7
	batch := make([]*model.HumanTask, 0, rows)
	for i := 0; i < rows; i++ {
		batch = append(batch, newHumanTaskRow(fmt.Sprintf("ht_big_%04d", i),
			model.HumanTaskKindConversationHandoff, "customer_session", fmt.Sprintf("sess_big_%04d", i),
			model.HumanTaskStatusPending))
	}
	for _, r := range batch {
		if err := repo.Insert(ctx, r); err != nil {
			t.Fatalf("造行失败：%v", err)
		}
	}
	got, total, err := repo.List(ctx, HumanTaskQuery{Page: 1, PageSize: rows * 10})
	if err != nil {
		t.Fatalf("List 报错：%v", err)
	}
	if total != rows {
		t.Errorf("total = %d，期望 %d（总数必须是过滤后的全量，不受夹取影响）", total, rows)
	}
	if len(got) != humanTaskMaxPageSize {
		t.Errorf("本页行数 = %d，期望被夹到上限 %d", len(got), humanTaskMaxPageSize)
	}
}

func TestHumanTaskRepo_CountOpenByKind(t *testing.T) {
	repo := NewHumanTaskRepositoryWithDB(setupHumanTaskTestDB(t))
	ctx := context.Background()

	// 空库：三类都要在映射里给 0。少一类和被读成 0 是两件事——
	// 只有"键存在且为 0"才能支撑"这类待办一条都没有"这句话。
	before, err := repo.CountOpenByKind(ctx)
	if err != nil {
		t.Fatalf("空库计数报错：%v", err)
	}
	for _, k := range model.HumanTaskKinds {
		if _, ok := before[k]; !ok {
			t.Errorf("空库映射里缺 %s 这一类（缺键与 0 在 JSON 里读起来不同）", k)
		}
	}
	if before[model.HumanTaskKindConversationHandoff] != 0 {
		t.Errorf("空库会话类应为 0，实际 %d", before[model.HumanTaskKindConversationHandoff])
	}

	for _, r := range []*model.HumanTask{
		newHumanTaskRow("ht_c_1", model.HumanTaskKindConversationHandoff, "customer_session", "sess_c_1", model.HumanTaskStatusPending),
		newHumanTaskRow("ht_c_2", model.HumanTaskKindConversationHandoff, "customer_session", "sess_c_2", model.HumanTaskStatusClaimed),
		newHumanTaskRow("ht_c_3", model.HumanTaskKindApproval, "approval_request", "apr_c_1", model.HumanTaskStatusPending),
		newHumanTaskRow("ht_c_4", model.HumanTaskKindCollectionEscalation, "collection_case", "cc_c_1", model.HumanTaskStatusDone),
	} {
		if err := repo.Insert(ctx, r); err != nil {
			t.Fatalf("造行失败：%v", err)
		}
	}
	got, err := repo.CountOpenByKind(ctx)
	if err != nil {
		t.Fatalf("计数报错：%v", err)
	}
	want := map[string]int64{
		model.HumanTaskKindConversationHandoff:  2, // 待认领 + 已认领都算"还在等人做"
		model.HumanTaskKindApproval:             1,
		model.HumanTaskKindCollectionEscalation: 0, // 唯一那行已 done，不进开放计数
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s 开放数 = %d，期望 %d", k, got[k], v)
		}
	}
}

// TestHumanTaskRepo_CountOverdueUsesOwnColumnOnly AC④ 的核心：指标隔离。
//
// 靶子是最省事的写法 COALESCE(sla_first_response_at, sla_decide_at, sla_escalate_at) &lt; now（伪 SQL，故意不写成反引号包起的代码段）：
// 它看着像"哪档填了就用哪档"，实际是让三类互相污染 —— 一行 kind 是会话、
// 却被填上审批档时刻的脏数据，会按审批档的截止被判逾期，坐席的响应指标当场失真。
// 本用例故意造这么一行（绕过 service 直写库），断言它**不进**任何一类的逾期数。
func TestHumanTaskRepo_CountOverdueUsesOwnColumnOnly(t *testing.T) {
	database := setupHumanTaskTestDB(t)
	repo := NewHumanTaskRepositoryWithDB(database)
	ctx := context.Background()
	now := humanTaskBaseAt.Add(10 * time.Minute)
	past := humanTaskBaseAt.Add(time.Minute)
	future := humanTaskBaseAt.Add(24 * time.Hour)

	// 三行各自合规：自己那一档、时刻不同。
	rows := []*model.HumanTask{
		withHandoffSLA(newHumanTaskRow("ht_od_1", model.HumanTaskKindConversationHandoff, "customer_session", "sess_od_1", model.HumanTaskStatusPending), past),
		newHumanTaskRow("ht_od_2", model.HumanTaskKindApproval, "approval_request", "apr_od_1", model.HumanTaskStatusPending),
		newHumanTaskRow("ht_od_3", model.HumanTaskKindCollectionEscalation, "collection_case", "cc_od_1", model.HumanTaskStatusPending),
	}
	decide := future
	rows[1].SlaDecideAt = &decide
	escalate := past
	rows[2].SlaEscalateAt = &escalate
	for _, r := range rows {
		if err := repo.Insert(ctx, r); err != nil {
			t.Fatalf("造行失败：%v", err)
		}
	}
	// 跨档脏行：会话类，只填了审批档的时刻（且已过期）。
	dirty := newHumanTaskRow("ht_od_dirty", model.HumanTaskKindConversationHandoff, "customer_session", "sess_od_dirty", model.HumanTaskStatusPending)
	dirtyDecide := past
	dirty.SlaDecideAt = &dirtyDecide
	if err := repo.Insert(ctx, dirty); err != nil {
		t.Fatalf("造脏行失败：%v", err)
	}

	got, err := repo.CountOverdueOpenByKind(ctx, now)
	if err != nil {
		t.Fatalf("逾期计数报错：%v", err)
	}
	want := map[string]int64{
		// 会话类只有 ht_od_1 逾期：脏行自己那一列是 NULL ⇒ 它没有截止，也就不逾期。
		model.HumanTaskKindConversationHandoff: 1,
		// 审批类那条截止在未来 ⇒ 0。催收类已逾期 ⇒ 1。
		model.HumanTaskKindApproval:             0,
		model.HumanTaskKindCollectionEscalation: 1,
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s 逾期数 = %d，期望 %d", k, got[k], v)
		}
	}

	// 已认领的会话待办仍进逾期读数：认领不等于做完。
	claimed := withHandoffSLA(newHumanTaskRow("ht_od_4", model.HumanTaskKindConversationHandoff,
		"customer_session", "sess_od_4", model.HumanTaskStatusClaimed), past)
	if err := repo.Insert(ctx, claimed); err != nil {
		t.Fatalf("造行失败：%v", err)
	}
	after, err := repo.CountOverdueOpenByKind(ctx, now)
	if err != nil {
		t.Fatalf("逾期计数报错：%v", err)
	}
	if after[model.HumanTaskKindConversationHandoff] != 2 {
		t.Errorf("已认领的逾期会话待办没被算进来：%d，期望 2", after[model.HumanTaskKindConversationHandoff])
	}
	// 没填 SLA 的开放待办不报错也不计入（NULL 不参与比较）——但它必须在映射里有个键。
	if _, ok := after[model.HumanTaskKindApproval]; !ok {
		t.Error("审批类键缺失：0 与「没有这一类」是两件事")
	}
}
