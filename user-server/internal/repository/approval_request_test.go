// approval_request_test.go T-P3-01：审批检查点仓储（表 approval_requests）。
//
// 本文件只测**只有真库才能证明**的四件事：
//  1. 两个部分唯一索引真的被 GORM 标签建成了部分索引（直查 pg_indexes）；
//  2. 幂等复用/重复待办的边界：pending 占坑、裁决后让坑（AC③ 的库侧前提）；
//  3. MutatePending 是"加锁读—判态—按列白名单写回"一步，并发下只有一个赢家，
//     而且**身份列改不动**（改得动就等于用一次合法裁决给别的对象开了门）；
//  4. 读失败与"查不到"是两种返回：把前者读成后者，闸门就会放行。
package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

func setupApprovalTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return testutil.NewTestDB(t, &model.ApprovalRequest{})
}

// newApprovalRow 造一行审批。时间戳显式给出：断言一半在排序与到期判据上，
// 依赖 GORM 的 autoCreateTime 会让"同一秒插入的两行谁在前"变成随机答案。
func newApprovalRow(id, subjectType, subjectID, policyKey, status string) *model.ApprovalRequest {
	now := time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)
	row := &model.ApprovalRequest{
		ID:          id,
		SubjectType: subjectType,
		SubjectID:   subjectID,
		PolicyKey:   policyKey,
		Status:      status,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if status == model.ApprovalStatusPending {
		exp := now.Add(24 * time.Hour)
		row.ExpiresAt = &exp
	}
	return row
}

func TestApprovalRequestRepo_NilHandle(t *testing.T) {
	repo := NewApprovalRequestRepositoryWithDB(nil)
	if repo.Available() {
		t.Fatal("nil 句柄时 Available 应为 false")
	}
	ctx := context.Background()
	// 每个方法都要报错。审批表刻意没有内存版底座（见文件头）：
	// "句柄没了 ⇒ 回空结果"在这里等于"查不到 pending ⇒ 放行"。
	if err := repo.Insert(ctx, newApprovalRow("a1", "quote", "q1", "quote.send", model.ApprovalStatusPending)); err == nil {
		t.Error("Insert 应报错")
	}
	if _, err := repo.GetByID(ctx, "a1"); err == nil {
		t.Error("GetByID 应报错")
	}
	if _, err := repo.GetByResumeToken(ctx, "rt_x"); err == nil {
		t.Error("GetByResumeToken 应报错")
	}
	if _, err := repo.GetPendingBySubject(ctx, "quote", "q1", "quote.send"); err == nil {
		t.Error("GetPendingBySubject 应报错")
	}
	if _, err := repo.MutatePending(ctx, "a1", nil); err == nil {
		t.Error("MutatePending 应报错")
	}
	if _, err := repo.ExpirePendingBatch(ctx, time.Now(), 10); err == nil {
		t.Error("ExpirePendingBatch 应报错")
	}
}

func TestApprovalRequestRepo_InsertNilKeyRejected(t *testing.T) {
	repo := NewApprovalRequestRepositoryWithDB(setupApprovalTestDB(t))
	ctx := context.Background()
	if err := repo.Insert(ctx, nil); err == nil {
		t.Error("nil 记录应报错")
	}
	// 空 ID 的行会以"看不见的幽灵审批"永久留在库里：列表查不到它，
	// 但幂等索引按 (subject, policy) 占坑，于是这条对象的真实审批永远建不出来。
	if err := repo.Insert(ctx, newApprovalRow("", "quote", "q-empty", "quote.send", model.ApprovalStatusPending)); err == nil {
		t.Error("空 ID 应报错")
	}
}

func TestApprovalRequestRepo_InsertAndGetRoundTrip(t *testing.T) {
	repo := NewApprovalRequestRepositoryWithDB(setupApprovalTestDB(t))
	ctx := context.Background()

	exp := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	dec := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	row := &model.ApprovalRequest{
		ID:           "apr_rt_1",
		SubjectType:  "reach_plan",
		SubjectID:    "rp-中文-9",
		PolicyKey:    "reach.batch_outreach",
		Status:       model.ApprovalStatusApproved,
		ResumeToken:  "",
		DecidedBy:    model.ApprovalDecidedByPolicy,
		DecidedAt:    &dec,
		DecisionNote: "低风险白名单内",
		CreatedAt:    time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC),
		UpdatedAt:    dec,
		ExpiresAt:    &exp,
	}
	if err := repo.Insert(ctx, row); err != nil {
		t.Fatalf("Insert 失败：%v", err)
	}
	got, err := repo.GetByID(ctx, row.ID)
	if err != nil || got == nil {
		t.Fatalf("GetByID 期望命中，实际 (%v,%v)", got, err)
	}
	if got.SubjectID != "rp-中文-9" {
		t.Errorf("中文 subject_id 应原样回读，实际 %q", got.SubjectID)
	}
	if got.Status != model.ApprovalStatusApproved || got.DecidedBy != model.ApprovalDecidedByPolicy {
		t.Errorf("状态/裁决者回读不一致：%+v", got)
	}
	if got.DecidedAt == nil || !got.DecidedAt.Equal(dec) {
		t.Errorf("decided_at 应原样回读，实际 %v", got.DecidedAt)
	}
	if got.ExpiresAt == nil || !got.ExpiresAt.Equal(exp) {
		t.Errorf("expires_at 应可回读（裁决后它是惰性的，但值不该丢），实际 %v", got.ExpiresAt)
	}
	if got.ResumeToken != "" {
		t.Errorf("auto-approve 行凭证应留空，实际 %q", got.ResumeToken)
	}

	// NULL 与零值时间的区分：pending 行没有 decided_at。
	p := newApprovalRow("apr_rt_2", "quote", "q2", "quote.send", model.ApprovalStatusPending)
	p.ResumeToken = "rt_apr_rt_2"
	if err := repo.Insert(ctx, p); err != nil {
		t.Fatalf("Insert pending 失败：%v", err)
	}
	gotP, err := repo.GetByID(ctx, p.ID)
	if err != nil || gotP == nil {
		t.Fatalf("读取 pending 失败：(%v,%v)", gotP, err)
	}
	if gotP.DecidedAt != nil {
		t.Errorf("pending 的 decided_at 应为 NULL（零值时间会冒充「1970 年就裁决了」），实际 %v", gotP.DecidedAt)
	}

	missing, err := repo.GetByID(ctx, "查无此单")
	if err != nil {
		t.Fatalf("不存在不是错误：%v", err)
	}
	if missing != nil {
		t.Errorf("不存在应回 (nil,nil)，实际 %+v", missing)
	}
}

// 两个部分唯一索引必须真实存在，且**带谓词**。
//
// GORM 标签写错（拼错 where、priority 少一列）时 AutoMigrate 不报错，只是静默不建索引，
// 于是"同一对象同策略两条 pending"只剩进程内判据 —— 多副本下失效。
// 这里直查 pg_indexes，而不是只断"第二次写入报错"（后者也可能因别的原因失败，证明力不够）。
func TestApprovalRequestRepo_PartialIndexesExist(t *testing.T) {
	database := setupApprovalTestDB(t)
	cases := []struct {
		index string
		want  []string
	}{
		{"uq_approval_request_open", []string{"unique", "subject_type", "subject_id", "policy_key", "where", "'pending'"}},
		{"uq_approval_request_token", []string{"unique", "resume_token", "where", "<>"}},
	}
	for _, tc := range cases {
		var def string
		err := database.Raw(
			"SELECT indexdef FROM pg_indexes WHERE tablename = 'approval_requests' AND indexname = ?",
			tc.index,
		).Scan(&def).Error
		if err != nil {
			t.Fatalf("查询 pg_indexes(%s) 失败：%v", tc.index, err)
		}
		if def == "" {
			t.Fatalf("%s 未被创建：GORM 的 uniqueIndex+where 标签没生效", tc.index)
		}
		// 只做包含比对：PG 会把谓词规范化成 `((status)::text = 'pending'::text)`，
		// 逐字比对等于把 PG 版本的输出格式钉进测试。
		lower := strings.ToLower(def)
		for _, w := range tc.want {
			if !strings.Contains(lower, strings.ToLower(w)) {
				t.Errorf("索引 %s 定义缺少 %q，实际：%s", tc.index, w, def)
			}
		}
	}
}

// AC③ 的库侧前提：pending 占坑、裁决后让坑。
//
// "让坑"全靠那个 `WHERE status = 'pending'` 谓词。把它摘掉（全表唯一）的后果不是
// 测试少一条，而是**被拒的对象修正后再也申请不了**：第一次的 rejected 行永久占着键位，
// 第二次 Submit 当场撞死。所以这里既测重复被拒，也测裁决后再申请成功。
func TestApprovalRequestRepo_OpenConflictAndRelease(t *testing.T) {
	repo := NewApprovalRequestRepositoryWithDB(setupApprovalTestDB(t))
	ctx := context.Background()

	first := newApprovalRow("apr_oc_1", "quote", "q-oc", "quote.send", model.ApprovalStatusPending)
	first.ResumeToken = "rt_oc_1"
	if err := repo.Insert(ctx, first); err != nil {
		t.Fatalf("首行写入失败：%v", err)
	}

	dup := newApprovalRow("apr_oc_2", "quote", "q-oc", "quote.send", model.ApprovalStatusPending)
	dup.ResumeToken = "rt_oc_2"
	err := repo.Insert(ctx, dup)
	if !errors.Is(err, ErrApprovalPendingConflict) {
		t.Fatalf("同对象同策略第二条 pending 应返回冲突哨兵，实际：%v", err)
	}
	if got, e := repo.GetByID(ctx, dup.ID); e != nil || got != nil {
		t.Errorf("冲突行不应落库，实际 (%v,%v)", got, e)
	}

	// 另一道策略不该被已有 pending 挡住：否则一道审批顺手批了另一道（键少一维的后果）。
	otherPolicy := newApprovalRow("apr_oc_3", "quote", "q-oc", "quote.high_discount", model.ApprovalStatusPending)
	otherPolicy.ResumeToken = "rt_oc_3"
	if err := repo.Insert(ctx, otherPolicy); err != nil {
		t.Errorf("同对象不同策略应可并存，实际：%v", err)
	}
	// 另一张报价单也不该撞。
	otherSubject := newApprovalRow("apr_oc_4", "quote", "q-other", "quote.send", model.ApprovalStatusPending)
	otherSubject.ResumeToken = "rt_oc_4"
	if err := repo.Insert(ctx, otherSubject); err != nil {
		t.Errorf("不同对象同策略应可并存，实际：%v", err)
	}

	// 裁决掉首行后再提交同 (subject, policy) 必须成功 —— 部分索引的另一半。
	applied, err := repo.MutatePending(ctx, first.ID, func(m *model.ApprovalRequest) {
		m.Status = model.ApprovalStatusRejected
	})
	if err != nil || !applied {
		t.Fatalf("裁决首行失败：(%v,%v)", applied, err)
	}
	retry := newApprovalRow("apr_oc_5", "quote", "q-oc", "quote.send", model.ApprovalStatusPending)
	retry.ResumeToken = "rt_oc_5"
	if err := repo.Insert(ctx, retry); err != nil {
		t.Errorf("首行已裁决，同对象再申请应成功（被拒对象修正后可重审），实际：%v", err)
	}
}

// 冲突判据必须"SQLSTATE 23505 **且**约束名"两个条件同时命中，缺一不可。
//
// 只认 23505 的后果不是"报错不好看"：本表上任何一条**别的**唯一约束被撞到时，service 都会
// 把它读成"已有 pending、幂等复用"，于是**一次真实的写入失败被汇报成一次成功的复用**，
// 而调用方拿着那条与本次请求毫无关系的记录去决定"要不要外发"。
// 判据要能被证伪就得真撞一次别的约束 —— 这里现场给表加一条与审批无关的唯一索引；
// 另一半（把判定整个摘掉）由同一条真·pending 冲突的断言兜住。
func TestApprovalRequestRepo_ConflictSenseIsConstraintSpecific(t *testing.T) {
	database := setupApprovalTestDB(t)
	repo := NewApprovalRequestRepositoryWithDB(database)
	ctx := context.Background()

	// 临时索引建在 decision_note 上：它与幂等键毫无关系，且 testutil 每例重建表，不会外溢。
	// 它是**全表**唯一 ⇒ 除了故意撞它的 a/b 那一对，其余各行的 decision_note 必须各不相同。
	if err := database.Exec("CREATE UNIQUE INDEX uq_approval_test_note ON approval_requests (decision_note)").Error; err != nil {
		t.Fatalf("临时唯一索引创建失败：%v", err)
	}

	a := newApprovalRow("apr_cs_1", "quote", "q-cs-1", "p1", model.ApprovalStatusApproved)
	a.DecisionNote = "note-first"
	if err := repo.Insert(ctx, a); err != nil {
		t.Fatalf("写入首行失败：%v", err)
	}
	// 不同 subject、同为 approved（不占 pending 的坑）⇒ 唯一可能的冲突是那条临时索引。
	b := newApprovalRow("apr_cs_2", "quote", "q-cs-2", "p1", model.ApprovalStatusApproved)
	b.DecisionNote = "note-first"
	err := repo.Insert(ctx, b)
	if err == nil {
		t.Fatal("撞临时索引的写入应失败")
	}
	if errors.Is(err, ErrApprovalPendingConflict) {
		t.Errorf("无关约束被判成「已有 pending」（只认 SQLSTATE 的漂移，service 会据此做幂等复用）：%v", err)
	}

	p := newApprovalRow("apr_cs_3", "quote", "q-cs-3", "p1", model.ApprovalStatusPending)
	p.DecisionNote = "note-pending"
	if err := repo.Insert(ctx, p); err != nil {
		t.Fatalf("写入 pending 失败：%v", err)
	}
	dup := newApprovalRow("apr_cs_4", "quote", "q-cs-3", "p1", model.ApprovalStatusPending)
	dup.DecisionNote = "note-dup"
	if err := repo.Insert(ctx, dup); !errors.Is(err, ErrApprovalPendingConflict) {
		t.Errorf("同三元组的第二条 pending 期望冲突哨兵，实际 %v", err)
	}

	// 真库造不出"名字对而 SQLSTATE 不对"的错误（那种组合本身不存在），
	// 所以这一半判据直接测谓词：两个条件各缺一半都不算命中。
	for _, msg := range []string{
		`duplicate key value violates unique constraint "uq_approval_request_open"`,
		`duplicate key value violates unique constraint "uq_other_index" (SQLSTATE 23505)`,
	} {
		if isApprovalPendingConflict(errors.New(msg)) {
			t.Errorf("只命中一半条件却判成 pending 冲突：%s", msg)
		}
	}
	if !isApprovalPendingConflict(errors.New(`duplicate key "uq_approval_request_open" (SQLSTATE 23505)`)) {
		t.Error("两个条件都命中时应判为 pending 冲突")
	}
}

// 已裁决行的身份三元组可以重复出现，且 auto-approve 行可以有很多条（凭证列留空串）。
//
// 这条测试专门盯 resume_token 那个部分索引：不带"排除空串"的谓词时，第二条
// auto-approve 记录会撞在空串上 —— 而 auto-approve 是 C2 里的**常态路径**（低风险直放），
// 也就是说这个失败会在上线第一天就发生。
func TestApprovalRequestRepo_DecidedRowsMayRepeatKeys(t *testing.T) {
	repo := NewApprovalRequestRepositoryWithDB(setupApprovalTestDB(t))
	ctx := context.Background()

	// 同一 subject+policy 两条已裁决（第一次拒、第二次批），历史都要留得下。
	for i, st := range []string{model.ApprovalStatusRejected, model.ApprovalStatusApproved} {
		row := newApprovalRow(fmt.Sprintf("apr_hist_%d", i), "quote", "q-hist", "quote.send", st)
		dec := time.Date(2026, 9, 19, 11, i, 0, 0, time.UTC)
		row.DecidedAt = &dec
		row.DecidedBy = "alice"
		row.ExpiresAt = nil
		if err := repo.Insert(ctx, row); err != nil {
			t.Fatalf("写入历史行 %d 失败：%v", i, err)
		}
	}
	// 多条 auto-approve：token 全为空串，彼此不冲突。
	for i := 0; i < 3; i++ {
		row := newApprovalRow(fmt.Sprintf("apr_auto_%d", i), fmt.Sprintf("t%d", i), fmt.Sprintf("s%d", i), "p.auto", model.ApprovalStatusApproved)
		row.ResumeToken = ""
		row.ExpiresAt = nil
		if err := repo.Insert(ctx, row); err != nil {
			t.Fatalf("写入第 %d 条 auto-approve 失败：%v", i, err)
		}
	}

	// 空凭证必须报错而**不是**去命中上面任意一条 token='' 的行。
	got, err := repo.GetByResumeToken(ctx, "")
	if !errors.Is(err, ErrApprovalTokenEmpty) {
		t.Errorf("空凭证期望 ErrApprovalTokenEmpty，实际 (%v,%v)", got, err)
	}
	if got != nil {
		t.Errorf("空凭证不得返回记录（否则一次漏传 token 的恢复调用会「恢复」到无关流程上）：%+v", got)
	}
	if _, err := repo.GetByResumeToken(ctx, "   "); !errors.Is(err, ErrApprovalTokenEmpty) {
		t.Errorf("纯空白凭证同样应被拒，实际 %v", err)
	}
	none, err := repo.GetByResumeToken(ctx, "rt_不存在")
	if err != nil || none != nil {
		t.Errorf("未知凭证期望 (nil,nil)，实际 (%v,%v)", none, err)
	}
}

func TestApprovalRequestRepo_GetPendingBySubject(t *testing.T) {
	repo := NewApprovalRequestRepositoryWithDB(setupApprovalTestDB(t))
	ctx := context.Background()

	p := newApprovalRow("apr_gps_1", "order_command", "oc-1", "order.amount_over_limit", model.ApprovalStatusPending)
	if err := repo.Insert(ctx, p); err != nil {
		t.Fatalf("写入失败：%v", err)
	}
	// 同键的已裁决历史行（谓词让它不占坑）不能被幂等查询捞回来，
	// 否则 service 会把"上次批过"当成"这次也批了"。
	// 创建时间**刻意早于**上面那条 pending：`First` 按 created_at ASC 取，若哪天
	// 查询漏掉了 status 条件，捞上来的就是这一行 —— 两行同时间的话漏判条件会靠
	// 插入顺序侥幸躲过（变异 Mu-R8 首轮正是这样存活的，改在这里加的时间差）。
	hist := newApprovalRow("apr_gps_2", "order_command", "oc-1", "order.amount_over_limit", model.ApprovalStatusRejected)
	hist.ExpiresAt = nil
	hist.CreatedAt = time.Date(2026, 9, 19, 8, 0, 0, 0, time.UTC)
	hist.UpdatedAt = hist.CreatedAt
	if err := repo.Insert(ctx, hist); err != nil {
		t.Fatalf("写入历史行失败：%v", err)
	}
	got, err := repo.GetPendingBySubject(ctx, "order_command", "oc-1", "order.amount_over_limit")
	if err != nil {
		t.Fatalf("GetPendingBySubject 失败：%v", err)
	}
	if got == nil || got.ID != p.ID {
		t.Fatalf("应只返回 pending 那条，实际 %+v", got)
	}

	miss, err := repo.GetPendingBySubject(ctx, "order_command", "oc-不存在", "order.amount_over_limit")
	if err != nil || miss != nil {
		t.Errorf("无 pending 期望 (nil,nil)，实际 (%v,%v)", miss, err)
	}
}

// 并发单赢家：8 个 goroutine 同时裁决同一行，只能有一个 applied=true。
//
// 这条是审批门最要命的场景 —— 两名审批人同时点"批准/拒绝"，两边都以为自己批成功了，
// 而一件事带着两个相反裁决继续往下跑，事后从表里只看得到最后写进去的那个。
// 用不同连接跑（GORM 默认连接池按需扩），同连接的事务锁不住自己。
//
// **本用例杀死的是带 `status='pending'` 的写回条件，不是 FOR UPDATE**：变异电池 Mu-R3
// 摘掉 `clause.Locking{Strength:"UPDATE"}` 后本用例仍绿（第二个事务的 UPDATE 会排在第一个
// 之后，重判条件时行已不是 pending ⇒ applied=false）。行锁的存在性由
// `_MutateHoldsRowLockWhileFnRuns` 单独观测，别把功劳记错对象。
func TestApprovalRequestRepo_ConcurrentMutateSingleWinner(t *testing.T) {
	repo := NewApprovalRequestRepositoryWithDB(setupApprovalTestDB(t))
	ctx := context.Background()

	row := newApprovalRow("apr_cas_1", "quote", "q-cas", "quote.send", model.ApprovalStatusPending)
	row.ResumeToken = "rt_cas_1"
	if err := repo.Insert(ctx, row); err != nil {
		t.Fatalf("写入失败：%v", err)
	}

	const n = 8
	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make([]struct {
		applied bool
		err     error
	}, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			applied, err := repo.MutatePending(ctx, row.ID, func(m *model.ApprovalRequest) {
				m.Status = model.ApprovalStatusApproved
				m.DecidedBy = fmt.Sprintf("approver-%d", i)
				now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
				m.DecidedAt = &now
			})
			results[i] = struct {
				applied bool
				err     error
			}{applied, err}
		}(i)
	}
	close(start)
	wg.Wait()

	winners := 0
	for i, r := range results {
		if r.err != nil {
			t.Fatalf("goroutine %d 报错（锁竞争应表现为 applied=false，不是失败）：%v", i, r.err)
		}
		if r.applied {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("期望恰好 1 个赢家，实际 %d（0=写回条件失效，>1=丢掉了行锁，两边都是覆盖裁决）", winners)
	}
	got, err := repo.GetByID(ctx, row.ID)
	if err != nil || got == nil {
		t.Fatalf("回读失败：(%v,%v)", got, err)
	}
	if got.Status != model.ApprovalStatusApproved {
		t.Errorf("最终状态应为 approved，实际 %s", got.Status)
	}
	if !strings.HasPrefix(got.DecidedBy, "approver-") {
		t.Errorf("decided_by 应是赢家留下的值，实际 %q", got.DecidedBy)
	}
}

// 行锁的**存在性**观测（Mu-R3 的靶子）。
//
// 为什么需要单独一条：把 `FOR UPDATE` 摘掉，上面那条并发用例仍绿（单赢家由带状态条件的
// 写回兜住），于是"仓储注释里写了 FOR UPDATE"这件事没有任何测试撑着 —— 而它买的东西很具体：
// **`fn` 执行期间这一行不许被别人改**。少了它，`fn` 读到的是"加锁读那一刻"的快照，
// 到写回之间别人可以改写非白名单列（或任何直连库的写入），白名单 + CAS 只保证状态列不被覆盖。
//
// 观测方式因此不看裁决结果、只看锁：让 `fn` 停在事务里等信号，另一条连接去改同一行，
// 它**必须阻塞**（不是失败、是等），放行后才完成。摘掉 FOR UPDATE ⇒ 它立刻返回 ⇒ 本用例红。
func TestApprovalRequestRepo_MutateHoldsRowLockWhileFnRuns(t *testing.T) {
	database := setupApprovalTestDB(t)
	repo := NewApprovalRequestRepositoryWithDB(database)
	ctx := context.Background()

	row := newApprovalRow("apr_lk_1", "quote", "q-lk", "quote.send", model.ApprovalStatusPending)
	row.ResumeToken = "rt_lk_1"
	if err := repo.Insert(ctx, row); err != nil {
		t.Fatalf("写入失败：%v", err)
	}

	entered := make(chan struct{})
	release := make(chan struct{})
	// 失败路径也必须放行那个 goroutine。少了这句，摘掉 FOR UPDATE 后本用例不是"红"而是
	// **挂死**：t.Fatalf 走 Goexit，goroutine 仍卡在事务里等 release，连接不归还 ⇒
	// testutil 的 cleanup（Close + DROP DATABASE）永久等待。实测首挂 40 分钟不退出 ——
	// "死锁"和"慢"在 `-timeout 0` 下长得一模一样，故电池这轮改用有限 timeout。
	var once sync.Once
	releaseNow := func() { once.Do(func() { close(release) }) }
	defer releaseNow()
	go func() {
		_, _ = repo.MutatePending(ctx, row.ID, func(m *model.ApprovalRequest) {
			m.Status = model.ApprovalStatusApproved
			close(entered)
			<-release // 停在事务内部：此刻行锁必须还握着
		})
	}()
	<-entered

	type outcome struct {
		took time.Duration
		err  error
	}
	done := make(chan outcome, 1)
	go func() {
		start := time.Now()
		// 另起一个 session（不同连接），否则会与那个事务互相排队、测不到锁。
		err := database.Session(&gorm.Session{NewDB: true}).WithContext(ctx).
			Model(&model.ApprovalRequest{}).Where("id = ?", row.ID).
			Update("decision_note", "written-from-outside").Error
		done <- outcome{time.Since(start), err}
	}()

	const holdWindow = 600 * time.Millisecond
	select {
	case o := <-done:
		t.Fatalf("外部 UPDATE 没有被行锁挡住（%v 就完成了）⇒ FOR UPDATE 已失效，"+
			"fn 读到与写回之间那一行是可以被别人插改的：%v", o.took, o.err)
	case <-time.After(holdWindow):
		releaseNow()
	}

	select {
	case o := <-done:
		if o.err != nil {
			t.Fatalf("放行后外部 UPDATE 失败：%v", o.err)
		}
		if o.took < holdWindow {
			t.Errorf("外部 UPDATE 耗时 %v 小于阻塞窗口 %v（阻塞不成立）", o.took, holdWindow)
		}
	case <-time.After(60 * time.Second):
		// 60s 而非更短：本机常有三套测试共用同一个 PG 实例（并行会话），放行后那条 UPDATE
		// 排在被抢空的 CPU 上，10s 内完不像是"没提交"，更像"没轮到"。
		t.Fatal("放行后外部 UPDATE 仍未完成（事务没提交？连接被占死？）")
	}

	back, err := repo.GetByID(ctx, row.ID)
	if err != nil || back == nil {
		t.Fatalf("回读失败：(%v,%v)", back, err)
	}
	if back.Status != model.ApprovalStatusApproved {
		t.Errorf("裁决最终应落库，实际 %s", back.Status)
	}
}

func TestApprovalRequestRepo_MutatePendingCASBasics(t *testing.T) {
	repo := NewApprovalRequestRepositoryWithDB(setupApprovalTestDB(t))
	ctx := context.Background()

	row := newApprovalRow("apr_mut_1", "quote", "q-mut", "quote.send", model.ApprovalStatusPending)
	// 时间戳放到一个必然早于"现在"的位置，好让"裁决有没有刷新 updated_at"成为可断言的事实。
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	row.CreatedAt = old
	row.UpdatedAt = old
	if err := repo.Insert(ctx, row); err != nil {
		t.Fatalf("写入失败：%v", err)
	}
	applied, err := repo.MutatePending(ctx, row.ID, func(m *model.ApprovalRequest) {
		m.Status = model.ApprovalStatusApproved
		m.DecidedBy = "alice"
	})
	if err != nil || !applied {
		t.Fatalf("首次裁决应生效，实际 (%v,%v)", applied, err)
	}
	// updated_at 在写集合里：它要么被 GORM 的 autoUpdateTime 刷成当前时刻，要么是
	// struct 里那份值 —— 断"它离开了 2020"同时排掉"写回 Go 零值时间"这种静默腐坏。
	stamped, err := repo.GetByID(ctx, row.ID)
	if err != nil || stamped == nil {
		t.Fatalf("回读失败：(%v,%v)", stamped, err)
	}
	if stamped.UpdatedAt.IsZero() || !stamped.UpdatedAt.After(old) {
		t.Errorf("裁决应刷新 updated_at（待办中心的排序与保留期都按它算），实际 %v", stamped.UpdatedAt)
	}
	if !stamped.CreatedAt.Equal(old) {
		t.Errorf("created_at 不该被裁决刷新：%v", stamped.CreatedAt)
	}
	// 落败方：非 pending 行不再被改写，且不是错误。
	before, _ := repo.GetByID(ctx, row.ID)
	applied2, err := repo.MutatePending(ctx, row.ID, func(m *model.ApprovalRequest) {
		m.Status = model.ApprovalStatusRejected
		m.DecisionNote = "被改坏了"
	})
	if err != nil {
		t.Fatalf("二次 mutate 报错：%v", err)
	}
	if applied2 {
		t.Error("已裁决行 mutate 不应生效")
	}
	after, _ := repo.GetByID(ctx, row.ID)
	if after.Status != before.Status || after.DecisionNote != before.DecisionNote {
		t.Errorf("落败的 mutate 不应改动任何列，实际 %+v", after)
	}
	missing, err := repo.MutatePending(ctx, "查无此单", nil)
	if err != nil || missing {
		t.Errorf("不存在期望 (false,nil)，实际 (%v,%v)", missing, err)
	}
}

// 身份列改不动：approvalWriteColumns 是白名单，名单外一切不可写。
//
// 这条测试是白名单存在的理由本身。若哪天有人把更新改成 Select("*")/整行 Save，
// fn 里顺手改 subject_id 就会把"已批准 (quote,q1)"变成"(quote,q2) 已批准" ——
// 一次合法裁决给另一张报价单开了门，而且表里看不出任何痕迹。
func TestApprovalRequestRepo_MutateCannotRepointIdentity(t *testing.T) {
	repo := NewApprovalRequestRepositoryWithDB(setupApprovalTestDB(t))
	ctx := context.Background()

	originCreated := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	row := newApprovalRow("apr_idn_1", "quote", "q-original", "quote.send", model.ApprovalStatusPending)
	row.ResumeToken = "rt_original"
	row.CreatedAt = originCreated
	row.ExpiresAt = func() *time.Time { x := originCreated.Add(time.Hour); return &x }()
	if err := repo.Insert(ctx, row); err != nil {
		t.Fatalf("写入失败：%v", err)
	}

	applied, err := repo.MutatePending(ctx, row.ID, func(m *model.ApprovalRequest) {
		m.Status = model.ApprovalStatusApproved
		m.DecidedBy = "bob"
		m.DecisionNote = "批了"
		// 以下四行全都不该落库。
		m.SubjectID = "q-stolen"
		m.SubjectType = "order_command"
		m.PolicyKey = "other.policy"
		m.ResumeToken = "rt_swapped"
		m.ID = "apr_someone_else"
		m.CreatedAt = time.Date(2026, 9, 19, 23, 0, 0, 0, time.UTC)
		m.ExpiresAt = nil
	})
	if err != nil || !applied {
		t.Fatalf("裁决应生效，实际 (%v,%v)", applied, err)
	}
	got, err := repo.GetByID(ctx, row.ID)
	if err != nil || got == nil {
		t.Fatalf("按原 ID 回读失败：(%v,%v)", got, err)
	}
	if got.Status != model.ApprovalStatusApproved || got.DecidedBy != "bob" || got.DecisionNote != "批了" {
		t.Errorf("白名单内的列应正常写入，实际 %+v", got)
	}
	if got.SubjectID != "q-original" || got.SubjectType != "quote" || got.PolicyKey != "quote.send" {
		t.Errorf("身份列被裁决改写了（= 一次批准给别的对象开了门）：%+v", got)
	}
	if got.ResumeToken != "rt_original" {
		t.Errorf("凭证被换掉会让流程手里那份当场失效，实际 %q", got.ResumeToken)
	}
	if !got.CreatedAt.Equal(originCreated) {
		t.Errorf("created_at 不应被裁决挪动：%v vs %v", got.CreatedAt, originCreated)
	}
	if got.ExpiresAt == nil {
		t.Error("expires_at 不在写集合内，应保留原值")
	}
	// 改身份没成功，改主键也没成功：原 ID 那行才是唯一存在的一行。
	if other, e := repo.GetByID(ctx, "apr_someone_else"); e != nil || other != nil {
		t.Errorf("裁决不得凭空产生新主键的行，实际 (%v,%v)", other, e)
	}
}

func TestApprovalRequestRepo_ExpirePendingBatch(t *testing.T) {
	repo := NewApprovalRequestRepositoryWithDB(setupApprovalTestDB(t))
	ctx := context.Background()
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)

	mk := func(id, status string, expires *time.Time) {
		row := newApprovalRow(id, "quote", id, "quote.send", status)
		row.ExpiresAt = expires
		row.CreatedAt = now.Add(-24 * time.Hour)
		row.UpdatedAt = now.Add(-24 * time.Hour)
		if status == model.ApprovalStatusPending {
			row.ResumeToken = "rt_" + id
		}
		if err := repo.Insert(ctx, row); err != nil {
			t.Fatalf("写入 %s 失败：%v", id, err)
		}
	}
	due := func(d time.Duration) *time.Time { x := now.Add(d); return &x }

	mk("apr_exp_due1", model.ApprovalStatusPending, due(-time.Hour))
	mk("apr_exp_due2", model.ApprovalStatusPending, due(-time.Minute))
	mk("apr_exp_fresh", model.ApprovalStatusPending, due(time.Hour))
	mk("apr_exp_null", model.ApprovalStatusPending, nil) // 手工造的脏行：pending 但没有到期时刻
	mk("apr_exp_approved", model.ApprovalStatusApproved, due(-24*time.Hour))
	mk("apr_exp_rejected", model.ApprovalStatusRejected, due(-24*time.Hour))

	flipped, err := repo.ExpirePendingBatch(ctx, now, 10)
	if err != nil {
		t.Fatalf("ExpirePendingBatch 失败：%v", err)
	}
	if len(flipped) != 2 {
		ids := make([]string, 0, len(flipped))
		for _, a := range flipped {
			ids = append(ids, a.ID)
		}
		t.Fatalf("期望翻 2 条，实际 %d：%v", len(flipped), ids)
	}
	for _, a := range flipped {
		if a.Status != model.ApprovalStatusExpired || a.DecidedBy != model.ApprovalDecidedByTTL {
			t.Errorf("返回行应已翻成 expired + system:ttl（供上层发事件），实际 %+v", a)
		}
	}
	// 返回行是内存里刷的，回读才算证据。
	got, err := repo.GetByID(ctx, "apr_exp_due1")
	if err != nil || got == nil {
		t.Fatalf("回读失败：(%v,%v)", got, err)
	}
	if got.Status != model.ApprovalStatusExpired || got.DecidedBy != model.ApprovalDecidedByTTL {
		t.Errorf("过期翻转应落库，实际 %+v", got)
	}
	if got.DecisionNote != "ttl_expired" {
		t.Errorf("超时应留下可区分的说明，实际 %q", got.DecisionNote)
	}
	// 到期行**不能**同时留下人工裁决的痕迹：超时和"人拒了"混成一个值，
	// 拒绝率就虚高，且看不到"审批没人处理"这个真问题。
	if !model.ApprovalDecidedAutomatically(got.DecidedBy) {
		t.Errorf("TTL 翻转的 decided_by 不该被读成人工裁决：%q", got.DecidedBy)
	}
	// 凭证不因过期而消失（历史可追溯），但已裁决的行不再可恢复 —— 判据在 service 侧。
	if got.ResumeToken != "rt_apr_exp_due1" {
		t.Errorf("resume_token 不该被清扫清掉：%q", got.ResumeToken)
	}

	for _, tc := range []struct{ id, want string }{
		{"apr_exp_fresh", model.ApprovalStatusPending},
		{"apr_exp_null", model.ApprovalStatusPending},
		{"apr_exp_approved", model.ApprovalStatusApproved},
		{"apr_exp_rejected", model.ApprovalStatusRejected},
	} {
		r, err := repo.GetByID(ctx, tc.id)
		if err != nil || r == nil {
			t.Fatalf("读取 %s 失败：(%v,%v)", tc.id, r, err)
		}
		if r.Status != tc.want {
			t.Errorf("%s 不该被过期扫描改动，期望 %s 实际 %s", tc.id, tc.want, r.Status)
		}
	}

	// 二次扫描 0 条：翻转必须真落库，否则"无界增长"只是从内存搬到 DB。
	again, err := repo.ExpirePendingBatch(ctx, now, 10)
	if err != nil || len(again) != 0 {
		t.Fatalf("二次扫描期望 0 条，实际 %d（err=%v）", len(again), err)
	}
}

// 单轮上限：一次长事务会把待办中心的读请求挡住，limit 必须真限制改写行数。
func TestApprovalRequestRepo_ExpirePendingBatchRespectsLimit(t *testing.T) {
	repo := NewApprovalRequestRepositoryWithDB(setupApprovalTestDB(t))
	ctx := context.Background()
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		exp := now.Add(-time.Duration(i+1) * time.Hour)
		row := newApprovalRow(fmt.Sprintf("apr_lim_%d", i), "quote", fmt.Sprintf("q-lim-%d", i), "quote.send", model.ApprovalStatusPending)
		row.ExpiresAt = &exp
		if err := repo.Insert(ctx, row); err != nil {
			t.Fatalf("写入失败：%v", err)
		}
	}
	for _, want := range []int{2, 2, 1, 0} {
		batch, err := repo.ExpirePendingBatch(ctx, now, 2)
		if err != nil {
			t.Fatalf("失败：%v", err)
		}
		if len(batch) != want {
			t.Fatalf("本轮期望翻 %d 条，实际 %d", want, len(batch))
		}
	}
}

// 读失败 ≠ 查不到。
//
// 关掉连接池之后，GetByID 若把驱动错误折叠成 (nil,nil)，service 侧的
// "行不存在 ⇒ 报审批不存在"与"库不通 ⇒ 别放行"就会塌成同一条路。
// 本表没有内存底座，所以这条区分是唯一的 fail-closed 依据。
func TestApprovalRequestRepo_ReadFailureIsNotNotFound(t *testing.T) {
	database := setupApprovalTestDB(t)
	repo := NewApprovalRequestRepositoryWithDB(database)
	ctx := context.Background()

	row := newApprovalRow("apr_fail_1", "quote", "q-fail", "quote.send", model.ApprovalStatusPending)
	if err := repo.Insert(ctx, row); err != nil {
		t.Fatalf("写入失败：%v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("取底层 sql.DB 失败：%v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("关闭连接池失败：%v", err)
	}
	if got, err := repo.GetByID(ctx, row.ID); err == nil {
		t.Errorf("连接已断，GetByID 期望报错，实际 (%v,nil)", got)
	}
	if got, err := repo.GetPendingBySubject(ctx, "quote", "q-fail", "quote.send"); err == nil {
		t.Errorf("读失败不能被折叠成「没有 pending」（那样下一次入队会再建一条待办），实际 (%v,nil)", got)
	}
	if got, err := repo.GetByResumeToken(ctx, "rt_any"); err == nil {
		t.Errorf("GetByResumeToken 期望报错，实际 (%v,nil)", got)
	}
	if _, err := repo.MutatePending(ctx, row.ID, nil); err == nil {
		t.Error("MutatePending 期望报错（故障时绝不能报 applied=true）")
	}
	if _, err := repo.ExpirePendingBatch(ctx, time.Now(), 10); err == nil {
		t.Error("ExpirePendingBatch 期望报错")
	}
	if err := repo.Insert(ctx, row); err == nil {
		t.Error("Insert 期望报错（写失败必须让审批入队失败，而不是静默丢弃）")
	}
}
