// bad_case_test.go T-P8-03：Bad Case 仓储（表 bad_cases）。
//
// 本文件只测**只有真库才能证明**的六件事：
//  1. 那条 dedup_key 唯一索引真的挡得住重复（热路径上"这一轮已经标过"靠它，不靠先查再插）；
//  2. 冲突识别认约束名，不认"任何 23505"（把主键冲突读成"已标记"会让真坏例静默消失）；
//  3. ApplyAction 是"加锁读—判期望态—按列白名单写回"一步：现场列与身份列改不动，
//     改得动就等于把一条自动标记伪装成人工补录、或把已经进了评测集的证据事后改掉；
//  4. 过滤器里的未知值必须报错而不是回空列表（空列表会被读成"这类坏例一条都没有"）；
//  5. 聚合读数的键恒在（0 也要出现），值域外的脏状态/脏 label 要带回来而不是悄悄丢行；
//  6. ClaimForExport 用 SKIP LOCKED：并发两次导出各自拿到互不相交的一批，
//     同一份样本不会进两个评测集（AC② 的库侧前提）。
package repository

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

func setupBadCaseTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return testutil.NewTestDB(t, &model.BadCase{})
}

// badCaseBaseAt 是所有造行共用的标记时刻：一半断言在排序上，
// 依赖 GORM 的 autoCreateTime 会让"同一秒插入的两行谁在前"变成随机答案。
var badCaseBaseAt = time.Date(2026, 9, 22, 8, 0, 0, 0, time.UTC)

// newBadCaseRow 造一条待判坏例。
//
// dedup 键在这里按 source 现算而不是由调用方传：键的形态正是本卡要锁的行为，
// 夹具替调用方写死一个字符串就等于把"键算错"这个缺陷预先抹掉。
// message_id 必须逐行不同：自动来源的键里含它，共用会撞唯一索引、造出静默少行。
func newBadCaseRow(id, source, sessionID, messageID string) *model.BadCase {
	dedupKey := id
	if source != model.BadCaseSourceManual {
		k, ok := model.BadCaseDedupKey(source, sessionID, messageID)
		if !ok {
			panic("夹具造了空 message_id 的自动来源行：" + id)
		}
		dedupKey = k
	}
	return &model.BadCase{
		ID:             id,
		Source:         source,
		Status:         model.BadCaseStatusPending,
		SessionID:      sessionID,
		MessageID:      messageID,
		SignalID:       "sig_" + id,
		IntentType:     "price_inquiry",
		QueryText:      "问句 " + id,
		AnswerText:     "答句 " + id,
		Confidence:     0.42,
		Threshold:      0.7,
		RetrievedCount: 0,
		MarkReason:     "聚合置信 0.4200 低于本轮阈值 0.7000",
		DedupKey:       dedupKey,
		CreatedAt:      badCaseBaseAt,
		UpdatedAt:      badCaseBaseAt,
	}
}

// markBadCaseRow 把一行判成 labeled。**只给夹具用**：生产路径走 ApplyAction，
// 夹具直接写库是为了不拿被测方法给自己造前置条件。
func markBadCaseRow(t *testing.T, db *gorm.DB, id, label string, at time.Time) {
	t.Helper()
	res := db.Model(&model.BadCase{}).Where("id = ?", id).Updates(map[string]any{
		"status":     model.BadCaseStatusLabeled,
		"label":      label,
		"label_note": "判定依据",
		"labeler_id": "op-1",
		"labeled_at": at,
		"fix_layer":  model.FixLayerOfLabel(label),
		"updated_at": at,
	})
	if res.Error != nil {
		t.Fatalf("夹具打标 %s 失败：%v", id, res.Error)
	}
	if res.RowsAffected != 1 {
		t.Fatalf("夹具打标 %s 影响 %d 行，期望 1（行没造出来？）", id, res.RowsAffected)
	}
}

func TestBadCaseRepo_NilHandle(t *testing.T) {
	repo := NewBadCaseRepositoryWithDB(nil)
	if repo.Available() {
		t.Fatal("nil 句柄时 Available 应为 false")
	}
	ctx := context.Background()
	// 每个方法都要报错而不是回空结果：读侧的"一条坏例都没有"是业务结论，
	// 把一次故障读成这句话，运营会照着它判断"低质回答没人管也行"。
	if ok, err := repo.InsertIfAbsent(ctx, newBadCaseRow("bc_n_1", model.BadCaseSourceLowConfidence, "s1", "m1")); ok || err == nil {
		t.Errorf("InsertIfAbsent 应报错：%v/%v", ok, err)
	}
	if got, err := repo.GetByID(ctx, "bc_n_1"); got != nil || err == nil {
		t.Errorf("GetByID 应报错：%v/%v", got, err)
	}
	if ok, err := repo.ApplyAction(ctx, "bc_n_1", model.BadCaseStatusPending, nil); ok || err == nil {
		t.Errorf("ApplyAction 应报错：%v/%v", ok, err)
	}
	if rows, total, err := repo.List(ctx, BadCaseListQuery{}); rows != nil || total != 0 || err == nil {
		t.Errorf("List 应报错：%v/%d/%v", rows, total, err)
	}
	if m, err := repo.CountByStatus(ctx); m != nil || err == nil {
		t.Errorf("CountByStatus 应报错：%v/%v", m, err)
	}
	if m, err := repo.CountByFixLayer(ctx); m != nil || err == nil {
		t.Errorf("CountByFixLayer 应报错：%v/%v", m, err)
	}
	if rows, err := repo.ClaimForExport(ctx, nil, 10, "bce_n_1", badCaseBaseAt); rows != nil || err == nil {
		t.Errorf("ClaimForExport 应报错：%v/%v", rows, err)
	}
}

func TestBadCaseRepo_InsertAndReadBack(t *testing.T) {
	db := setupBadCaseTestDB(t)
	repo := NewBadCaseRepositoryWithDB(db)
	ctx := context.Background()

	row := newBadCaseRow("bc_r_1", model.BadCaseSourceLowConfidence, "sess_r", "msg_r")
	row.RetrievedCount = 3
	row.Confidence = 0.61
	ok, err := repo.InsertIfAbsent(ctx, row)
	if err != nil || !ok {
		t.Fatalf("首插应生效：(%v,%v)", ok, err)
	}
	got, err := repo.GetByID(ctx, row.ID)
	if err != nil || got == nil {
		t.Fatalf("回读失败：(%v,%v)", got, err)
	}
	// decimal(5,4) 与文本列要逐字段对得上：现场抄本一旦错位，
	// 打标的人看到的就是另一句回答，而这条已经会进评测集。
	if got.QueryText != "问句 bc_r_1" || got.AnswerText != "答句 bc_r_1" {
		t.Errorf("现场抄本错位：%q/%q", got.QueryText, got.AnswerText)
	}
	if got.Confidence != 0.61 || got.Threshold != 0.7 {
		t.Errorf("置信度/阈值错位：%.4f/%.4f", got.Confidence, got.Threshold)
	}
	if got.RetrievedCount != 3 {
		t.Errorf("retrieved_count 错位：%d", got.RetrievedCount)
	}
	if got.DedupKey != "low_confidence|sess_r|msg_r" {
		t.Errorf("dedup_key 形态不对：%q", got.DedupKey)
	}
	if !got.CreatedAt.Equal(badCaseBaseAt) {
		t.Errorf("created_at 被 autoCreateTime 改写：%v", got.CreatedAt)
	}
	if got.SignalID != "sig_bc_r_1" || got.IntentType != "price_inquiry" {
		t.Errorf("指回证据的列错位：%q/%q", got.SignalID, got.IntentType)
	}
	// 空 id 与 nil 记录要在进 SQL 之前挡下：否则上抛的是一串 PG 的 not-null 违反。
	if _, err := repo.GetByID(ctx, "   "); !errors.Is(err, ErrBadCaseInputInvalid) {
		t.Errorf("空白 id 应判入参非法，实际 %v", err)
	}
	if ok, err := repo.InsertIfAbsent(ctx, nil); ok || err == nil {
		t.Errorf("nil 记录应报错：%v/%v", ok, err)
	}
	if ok, err := repo.InsertIfAbsent(ctx, &model.BadCase{Source: model.BadCaseSourceManual}); ok || err == nil {
		t.Errorf("空 ID 应报错：%v/%v", ok, err)
	}
	// 不存在 → (nil, nil)：这是"这条没被记过"，不是"读不动"。
	if got, err := repo.GetByID(ctx, "bc_absent"); got != nil || err != nil {
		t.Errorf("不存在的 id 应回 (nil,nil)：%v/%v", got, err)
	}
}

// 幂等：同一 (source, session, message) 第二次插必须返回 (false, nil) 且不落第二行。
func TestBadCaseRepo_DedupIsReal(t *testing.T) {
	db := setupBadCaseTestDB(t)
	repo := NewBadCaseRepositoryWithDB(db)
	ctx := context.Background()

	first := newBadCaseRow("bc_d_1", model.BadCaseSourceLowConfidence, "sess_d", "msg_d")
	if ok, err := repo.InsertIfAbsent(ctx, first); !ok || err != nil {
		t.Fatalf("首插失败：(%v,%v)", ok, err)
	}
	// 换一个 id、同样的现场 —— 撞的必须是 dedup 键。
	second := newBadCaseRow("bc_d_2", model.BadCaseSourceLowConfidence, "sess_d", "msg_d")
	ok, err := repo.InsertIfAbsent(ctx, second)
	if err != nil || ok {
		t.Fatalf("重复现场应返回 (false,nil)：(%v,%v)", ok, err)
	}
	countAt := func(sess, msg string) int64 {
		var n int64
		if err := db.Model(&model.BadCase{}).Where("session_id = ? AND message_id = ?", sess, msg).Count(&n).Error; err != nil {
			t.Fatalf("计数失败：%v", err)
		}
		return n
	}
	if n := countAt("sess_d", "msg_d"); n != 1 {
		t.Errorf("同一现场落了 %d 行，期望 1 行", n)
	}
	// 被去重的那条不该能按自己的 id 读到（它压根没落库）。
	if got, err := repo.GetByID(ctx, "bc_d_2"); got != nil || err != nil {
		t.Errorf("被去重的行不该读得到：%v/%v", got, err)
	}

	// 同一现场、不同来源 → 两个键、两行：zero_hit 与 low_confidence 的修复责任人不同，
	// 互相去重会把其中一拨人的队列读空。
	other := newBadCaseRow("bc_d_3", model.BadCaseSourceZeroHit, "sess_d", "msg_d")
	if ok, err := repo.InsertIfAbsent(ctx, other); !ok || err != nil {
		t.Fatalf("不同来源应可并存：(%v,%v)", ok, err)
	}
	if n := countAt("sess_d", "msg_d"); n != 2 {
		t.Errorf("两来源应有 2 行，实际 %d", n)
	}

	// 手动来源的键是本行 id：两条同样现场的人工补录都是人的判断，不去重。
	m1 := newBadCaseRow("bc_d_4", model.BadCaseSourceManual, "sess_d", "msg_d")
	m2 := newBadCaseRow("bc_d_5", model.BadCaseSourceManual, "sess_d", "msg_d")
	for _, r := range []*model.BadCase{m1, m2} {
		if ok, err := repo.InsertIfAbsent(ctx, r); !ok || err != nil {
			t.Fatalf("手动来源第二条应可插入：(%v,%v)", ok, err)
		}
	}
	if m1.DedupKey == m2.DedupKey {
		t.Errorf("手动来源两条共用同一 dedup 键 %q：第二条会被静默吃掉", m1.DedupKey)
	}
	if n := countAt("sess_d", "msg_d"); n != 4 {
		t.Errorf("同现场合计应 4 行（2 自动来源 + 2 手动），实际 %d", n)
	}
}

// 冲突识别要认约束名：只判 23505 会把主键重号读成"这一轮已标记"，
// 于是真坏例在热路径上消失且全程零报错。
func TestBadCaseRepo_ConflictIdentityIsExact(t *testing.T) {
	db := setupBadCaseTestDB(t)
	repo := NewBadCaseRepositoryWithDB(db)
	ctx := context.Background()

	a := newBadCaseRow("bc_pk_1", model.BadCaseSourceLowConfidence, "sess_pk", "msg_a")
	if _, err := repo.InsertIfAbsent(ctx, a); err != nil {
		t.Fatalf("首插失败：%v", err)
	}
	// 同 id（主键冲突）、不同现场（dedup 键不冲突）
	dupID := newBadCaseRow("bc_pk_1", model.BadCaseSourceZeroHit, "sess_pk", "msg_b")
	ok, err := repo.InsertIfAbsent(ctx, dupID)
	if ok {
		t.Error("主键冲突不该报成功")
	}
	if err == nil {
		t.Fatal("主键冲突被读成\"已存在\"（err==nil）：去重键之外的约束必须上抛")
	}
	// 约束名判定函数本身：三档各给一句明确答案
	if !isBadCaseDedupConflict(errors.New(`ERROR: duplicate key value violates unique constraint "bad_cases_dedup_key_key" (SQLSTATE 23505)`)) {
		t.Error("真 dedup 冲突判不出")
	}
	if isBadCaseDedupConflict(errors.New(`ERROR: duplicate key value violates unique constraint "bad_cases_pkey" (SQLSTATE 23505)`)) {
		t.Error("主键冲突被判成 dedup 冲突")
	}
	if isBadCaseDedupConflict(nil) {
		t.Error("nil 不该是冲突")
	}
	// 只在报错文本里有 23505、没有约束名（别的错误），同样不许算冲突
	if isBadCaseDedupConflict(errors.New("some other failure 23505")) {
		t.Error("缺约束名的错误不该算 dedup 冲突")
	}
}

func TestBadCaseRepo_ApplyActionWriteWhitelist(t *testing.T) {
	db := setupBadCaseTestDB(t)
	repo := NewBadCaseRepositoryWithDB(db)
	ctx := context.Background()

	row := newBadCaseRow("bc_wl_1", model.BadCaseSourceLowConfidence, "sess_wl", "msg_wl")
	if _, err := repo.InsertIfAbsent(ctx, row); err != nil {
		t.Fatalf("写入失败：%v", err)
	}
	hijackAt := badCaseBaseAt.Add(99 * time.Hour)
	ok, err := repo.ApplyAction(ctx, row.ID, model.BadCaseStatusPending, func(m *model.BadCase) error {
		// 白名单之内：状态与判定结论
		m.Status = model.BadCaseStatusLabeled
		m.Label = model.BadCaseLabelKBMissing
		m.LabelNote = "库里缺这条政策"
		m.LabelerID = "op-1"
		m.LabeledAt = &hijackAt
		m.FixLayer = model.BadCaseFixLayerKnowledge
		// 白名单之外：身份、来源、现场、时间戳 —— 每一样改得动都能伪造出结论
		m.ID = "bc_wl_HIJACKED"
		m.Source = model.BadCaseSourceManual
		m.DedupKey = "tampered"
		m.SessionID = "sess_other"
		m.MessageID = "msg_other"
		m.SignalID = "sig_other"
		m.IntentType = "other_intent"
		m.QueryText = "改了问句"
		m.AnswerText = "改了答句"
		m.Confidence = 0.99
		m.Threshold = 0.10
		m.RetrievedCount = 42
		m.MarkReason = "改了理由"
		m.CreatedAt = badCaseBaseAt.Add(-100 * time.Hour)
		return nil
	})
	if err != nil || !ok {
		t.Fatalf("打标未生效：(%v,%v)", ok, err)
	}
	got, err := repo.GetByID(ctx, row.ID)
	if err != nil || got == nil {
		t.Fatalf("按原 ID 回读失败（ID 被改走了？）：(%v,%v)", got, err)
	}
	if got.Source != model.BadCaseSourceLowConfidence {
		t.Errorf("source 被改成 %q：自动标记能被伪装成人工补录", got.Source)
	}
	if got.DedupKey != row.DedupKey {
		t.Errorf("dedup_key 被改成 %q：幂等键被动过，后续同现场的真坏例会重复入库", got.DedupKey)
	}
	if got.SessionID != "sess_wl" || got.MessageID != "msg_wl" || got.SignalID != "sig_bc_wl_1" {
		t.Errorf("指回事实源的列被改走：%s/%s/%s", got.SessionID, got.MessageID, got.SignalID)
	}
	if got.IntentType != "price_inquiry" {
		t.Errorf("intent_type 被改走：%q", got.IntentType)
	}
	if got.QueryText != row.QueryText || got.AnswerText != row.AnswerText {
		t.Errorf("现场抄本被事后改写：%q/%q", got.QueryText, got.AnswerText)
	}
	if got.Confidence != 0.42 || got.Threshold != 0.7 || got.RetrievedCount != 0 {
		t.Errorf("判据数字被改写：%.4f/%.4f/%d", got.Confidence, got.Threshold, got.RetrievedCount)
	}
	if got.MarkReason != row.MarkReason {
		t.Errorf("mark_reason 被改写：%q", got.MarkReason)
	}
	if !got.CreatedAt.Equal(badCaseBaseAt) {
		t.Errorf("created_at 被改走：%v（队列排序与判定时长的分子）", got.CreatedAt)
	}
	// 白名单之内的列必须真的写进去 —— 只断"改不动"不断"改得动"，
	// 等于允许一个整块丢弃写入的实现。
	if got.Status != model.BadCaseStatusLabeled {
		t.Errorf("status 没落库：%q", got.Status)
	}
	if got.Label != model.BadCaseLabelKBMissing || got.LabelerID != "op-1" || got.LabelNote == "" {
		t.Errorf("判定结论没落库：%q/%q/%q", got.Label, got.LabelerID, got.LabelNote)
	}
	if got.FixLayer != model.BadCaseFixLayerKnowledge {
		t.Errorf("fix_layer 没落库：%q", got.FixLayer)
	}
	if got.LabeledAt == nil || !got.LabeledAt.Equal(hijackAt) {
		t.Errorf("labeled_at 没落库：%v", got.LabeledAt)
	}
}

func TestBadCaseRepo_ApplyActionExpectsStatus(t *testing.T) {
	db := setupBadCaseTestDB(t)
	repo := NewBadCaseRepositoryWithDB(db)
	ctx := context.Background()

	for i, c := range []struct {
		name         string
		storeStatus  string
		expectStatus string
		wantApplied  bool
	}{
		{"pending 上按 pending → 生效", model.BadCaseStatusPending, model.BadCaseStatusPending, true},
		{"已判完的行按 pending → 不生效", model.BadCaseStatusLabeled, model.BadCaseStatusPending, false},
		{"已导出的行按 labeled → 不生效", model.BadCaseStatusExported, model.BadCaseStatusLabeled, false},
		{"已放弃的行按 pending → 不生效", model.BadCaseStatusDismissed, model.BadCaseStatusPending, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			id := "bc_cas_" + strconv.Itoa(i)
			row := newBadCaseRow(id, model.BadCaseSourceManual, "sess_cas", "msg_cas_"+id)
			row.Status = c.storeStatus
			if _, err := repo.InsertIfAbsent(ctx, row); err != nil {
				t.Fatalf("造行失败：%v", err)
			}
			applied, err := repo.ApplyAction(ctx, id, c.expectStatus, func(m *model.BadCase) error {
				m.Status = c.expectStatus
				m.LabelNote = "动过"
				return nil
			})
			if err != nil {
				t.Fatalf("ApplyAction 报错：%v", err)
			}
			if applied != c.wantApplied {
				t.Fatalf("applied=%v want=%v", applied, c.wantApplied)
			}
			var note string
			if err := db.Model(&model.BadCase{}).Where("id = ?", id).Select("label_note").Scan(&note).Error; err != nil {
				t.Fatalf("回读失败：%v", err)
			}
			if c.wantApplied != (note == "动过") {
				t.Errorf("写入与 applied 不自洽：applied=%v label_note=%q", applied, note)
			}
			// 不生效时库里那行必须还停在原态（"没改成"不等于"改坏了一半"）。
			var stored model.BadCase
			if err := db.Where("id = ?", id).First(&stored).Error; err != nil {
				t.Fatalf("回读整行失败：%v", err)
			}
			if stored.Status != c.storeStatus {
				t.Errorf("库里状态被写坏：期望 %q 实际 %q", c.storeStatus, stored.Status)
			}
		})
	}

	// 行不存在 → (false, nil)：不是错误，是"这条没被记过"。
	if applied, err := repo.ApplyAction(ctx, "bc_absent", model.BadCaseStatusPending, nil); applied || err != nil {
		t.Errorf("不存在的 id 应回 (false,nil)：%v/%v", applied, err)
	}
	// 期望态值域外 → 入参错误（放进 SQL 它会变成"永不匹配"的静默 no-op）。
	if applied, err := repo.ApplyAction(ctx, "bc_cas_0", "bogus", nil); applied || !errors.Is(err, ErrBadCaseInputInvalid) {
		t.Errorf("未知期望态应判入参非法：%v/%v", applied, err)
	}
	if applied, err := repo.ApplyAction(ctx, " ", model.BadCaseStatusPending, nil); applied || !errors.Is(err, ErrBadCaseInputInvalid) {
		t.Errorf("空白 id 应判入参非法：%v/%v", applied, err)
	}
}

func TestBadCaseRepo_ApplyActionFnErrorRollsBack(t *testing.T) {
	db := setupBadCaseTestDB(t)
	repo := NewBadCaseRepositoryWithDB(db)
	ctx := context.Background()

	row := newBadCaseRow("bc_rb_1", model.BadCaseSourceManual, "sess_rb", "msg_rb")
	if _, err := repo.InsertIfAbsent(ctx, row); err != nil {
		t.Fatalf("造行失败：%v", err)
	}
	sentinel := errors.New("fn 里的校验没过")
	applied, err := repo.ApplyAction(ctx, row.ID, model.BadCaseStatusPending, func(m *model.BadCase) error {
		m.Status = model.BadCaseStatusLabeled
		m.Label = model.BadCaseLabelKBStale
		return sentinel
	})
	if applied {
		t.Error("fn 报错时 applied 不该为 true")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("fn 的错误应原样上抛，实际 %v", err)
	}
	got, err := repo.GetByID(ctx, row.ID)
	if err != nil || got == nil {
		t.Fatalf("回读失败：(%v,%v)", got, err)
	}
	if got.Status != model.BadCaseStatusPending || got.Label != "" {
		t.Errorf("事务没回滚：%s/%q", got.Status, got.Label)
	}
}

// fn 跑的时候行锁必须还握着：否则"判期望态"与"写回"之间的窗口里
// 另一次导出能把同一行取走，两边各写一半。
func TestBadCaseRepo_ApplyActionHoldsRowLockWhileFnRuns(t *testing.T) {
	database := setupBadCaseTestDB(t)
	repo := NewBadCaseRepositoryWithDB(database)
	ctx := context.Background()

	row := newBadCaseRow("bc_lk_1", model.BadCaseSourceManual, "sess_lk", "msg_lk")
	if _, err := repo.InsertIfAbsent(ctx, row); err != nil {
		t.Fatalf("写入失败：%v", err)
	}

	inside := make(chan struct{})
	releaseOnce := sync.Once{}
	release := func() { releaseOnce.Do(func() { close(inside) }) }
	defer release()

	entered := make(chan error, 1)
	go func() {
		_, err := repo.ApplyAction(ctx, row.ID, model.BadCaseStatusPending, func(m *model.BadCase) error {
			m.Label = model.BadCaseLabelKBMissing
			close(entered) // fn 确实进来了
			<-inside       // 且此刻仍持有行锁
			return nil
		})
		if err != nil {
			entered <- err
		}
	}()
	select {
	case err := <-entered:
		if err != nil {
			t.Fatalf("fn 未进入或 ApplyAction 报错：%v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("ApplyAction 迟迟没进 fn")
	}

	blocked := make(chan error, 1)
	go func() {
		blocked <- database.Model(&model.BadCase{}).Where("id = ?", row.ID).
			Update("query_text", "外部改写").Error
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
	var q string
	if err := database.Model(&model.BadCase{}).Where("id = ?", row.ID).Select("query_text").Scan(&q).Error; err != nil {
		t.Fatalf("回读失败：%v", err)
	}
	if q != "外部改写" {
		t.Errorf("锁释放后外部改写没生效：%q", q)
	}
}

func TestBadCaseRepo_ListFilters(t *testing.T) {
	db := setupBadCaseTestDB(t)
	repo := NewBadCaseRepositoryWithDB(db)
	ctx := context.Background()

	// 造六个面：三种来源各自一条 pending、一条已判。
	rows := []*model.BadCase{
		newBadCaseRow("bc_f_1", model.BadCaseSourceLowConfidence, "s1", "m1"),
		newBadCaseRow("bc_f_2", model.BadCaseSourceZeroHit, "s1", "m2"),
		newBadCaseRow("bc_f_3", model.BadCaseSourceManual, "s2", "m3"),
		newBadCaseRow("bc_f_4", model.BadCaseSourceLowConfidence, "s3", "m4"),
		newBadCaseRow("bc_f_5", model.BadCaseSourceZeroHit, "s4", "m5"),
		newBadCaseRow("bc_f_6", model.BadCaseSourceManual, "s5", "m6"),
	}
	for _, r := range rows {
		if _, err := repo.InsertIfAbsent(ctx, r); err != nil {
			t.Fatalf("造行 %s 失败：%v", r.ID, err)
		}
	}
	markBadCaseRow(t, db, "bc_f_2", model.BadCaseLabelKBMissing, badCaseBaseAt.Add(time.Minute))
	markBadCaseRow(t, db, "bc_f_3", model.BadCaseLabelGenerationWrong, badCaseBaseAt.Add(2*time.Minute))
	markBadCaseRow(t, db, "bc_f_4", model.BadCaseLabelRetrieveMiss, badCaseBaseAt.Add(3*time.Minute))
	// bc_f_4 判完又被导走 → exported
	ok, err := repo.ApplyAction(ctx, "bc_f_4", model.BadCaseStatusLabeled, func(m *model.BadCase) error {
		m.Status = model.BadCaseStatusExported
		m.EvalSetID = "bce_1"
		m.ExportedAt = &badCaseBaseAt
		return nil
	})
	if err != nil || !ok {
		t.Fatalf("置导出态失败：(%v,%v)", ok, err)
	}

	// 默认视图 = 只列 pending（打标队列不该把判完的混进来：那是另一拨人看的）
	list, total, err := repo.List(ctx, BadCaseListQuery{})
	if err != nil {
		t.Fatalf("默认 List 失败：%v", err)
	}
	if total != 3 || len(list) != 3 {
		t.Errorf("默认视图应 3 条 pending，实际 len=%d total=%d", len(list), total)
	}
	for _, r := range list {
		if r.Status != model.BadCaseStatusPending {
			t.Errorf("默认视图混进 %s（%s）", r.Status, r.ID)
		}
	}

	// 来源过滤
	list, total, err = repo.List(ctx, BadCaseListQuery{Sources: []string{model.BadCaseSourceManual}, Statuses: model.BadCaseStatuses})
	if err != nil {
		t.Fatalf("来源过滤失败：%v", err)
	}
	if total != 2 || len(list) != 2 {
		t.Errorf("manual 应 2 条，实际 len=%d total=%d", len(list), total)
	}
	for _, r := range list {
		if r.Source != model.BadCaseSourceManual {
			t.Errorf("来源过滤串档：%s/%s", r.ID, r.Source)
		}
	}

	// 状态过滤：导出的那行只能靠显式列 exported 读到
	if _, total, err := repo.List(ctx, BadCaseListQuery{Statuses: []string{model.BadCaseStatusExported}}); err != nil || total != 1 {
		t.Errorf("exported 应 1 条：(%d,%v)", total, err)
	}
	if _, total, err := repo.List(ctx, BadCaseListQuery{Statuses: []string{model.BadCaseStatusLabeled}}); err != nil || total != 2 {
		t.Errorf("labeled 应 2 条：(%d,%v)", total, err)
	}
	if _, total, err := repo.List(ctx, BadCaseListQuery{Statuses: []string{model.BadCaseStatusDismissed}}); err != nil || total != 0 {
		t.Errorf("dismissed 应 0 条：(%d,%v)", total, err)
	}

	// 类目与责任层过滤（层是 label 的派生列，两者必须自洽）
	if _, total, err := repo.List(ctx, BadCaseListQuery{Statuses: model.BadCaseStatuses, Labels: []string{model.BadCaseLabelKBMissing}}); err != nil || total != 1 {
		t.Errorf("kb_missing 应 1 条：(%d,%v)", total, err)
	}
	if _, total, err := repo.List(ctx, BadCaseListQuery{Statuses: model.BadCaseStatuses, FixLayer: model.BadCaseFixLayerKnowledge}); err != nil || total != 1 {
		t.Errorf("knowledge 层应 1 条：(%d,%v)", total, err)
	}
	if _, total, err := repo.List(ctx, BadCaseListQuery{Statuses: model.BadCaseStatuses, FixLayer: model.BadCaseFixLayerIntent}); err != nil || total != 0 {
		t.Errorf("intent 层应 0 条：(%d,%v)", total, err)
	}
	// 组合条件：来源 + 状态同时生效（只套一个的话另一个的读数会被当成"过滤过了"）
	if _, total, err := repo.List(ctx, BadCaseListQuery{Sources: []string{model.BadCaseSourceZeroHit}, Statuses: []string{model.BadCaseStatusPending}}); err != nil || total != 1 {
		t.Errorf("zero_hit 且 pending 应 1 条（bc_f_5）：(%d,%v)", total, err)
	}

	// 值域外的过滤器必须报错而不是回空列表（空列表 = "这类一条都没有"的业务结论）
	for _, c := range []struct {
		name string
		q    BadCaseListQuery
	}{
		{"未知来源", BadCaseListQuery{Sources: []string{"bogus_source"}}},
		{"未知状态", BadCaseListQuery{Statuses: []string{"bogus_status"}}},
		{"未知类目", BadCaseListQuery{Labels: []string{"bogus_label"}}},
		{"未知责任层", BadCaseListQuery{FixLayer: "bogus_layer"}},
		{"分页只给一半", BadCaseListQuery{Page: 2}},
		{"页码非正", BadCaseListQuery{Page: 0, PageSize: 10}},
		{"页长非正", BadCaseListQuery{Page: 1, PageSize: -5}},
	} {
		list, total, err := repo.List(ctx, c.q)
		if !errors.Is(err, ErrBadCaseInputInvalid) {
			t.Errorf("%s：应判入参非法，实际 err=%v total=%d rows=%d", c.name, err, total, len(list))
		}
		if list != nil || total != 0 {
			t.Errorf("%s：报错时不该带回数据：%d 行 / total=%d", c.name, len(list), total)
		}
	}

	// 单页上限要真夹住：一句 page_size=999999 不该能拉整表
	list, total, err = repo.List(ctx, BadCaseListQuery{Statuses: model.BadCaseStatuses, Page: 1, PageSize: badCaseMaxPageSize + 5})
	if err != nil {
		t.Fatalf("超大页失败：%v", err)
	}
	if len(list) > badCaseMaxPageSize {
		t.Errorf("单页返回 %d 行，超过上限 %d", len(list), badCaseMaxPageSize)
	}
	if total != 6 {
		t.Errorf("total 应是过滤后的总数 6，实际 %d（total 被分页夹住的话前端会以为没有下一页）", total)
	}

	// 分页：三页各 2 条，合起来正好等于 total（count 与取页各一条语句的自证）
	seen := map[string]bool{}
	for page := 1; page <= 3; page++ {
		pageRows, _, err := repo.List(ctx, BadCaseListQuery{Statuses: model.BadCaseStatuses, Page: page, PageSize: 2})
		if err != nil {
			t.Fatalf("第 %d 页失败：%v", page, err)
		}
		for _, r := range pageRows {
			if seen[r.ID] {
				t.Errorf("%s 在第 %d 页重复出现：偏移量算错了", r.ID, page)
			}
			seen[r.ID] = true
		}
	}
	if len(seen) != 6 {
		t.Errorf("三页合计拿到 %d 条，期望 6", len(seen))
	}
}

// 两处上限必须**恰好**夹在声明的那个值上。
//
// 为什么单开一个用例：ListFilters/ClaimForExport 里那两行断言写的是"不超过上限"，
// 而它们的夹具只有几行数据 ⇒ 把上限常量抬到天上、或者干脆不夹，都照样绿。
// 这两处上限都不是性能参数，是业务口径（一页不许拉整表、一份评测集不许灌进三年的
// 未复核样本），所以要拿"行数必须多于上限"的夹具把夹住这件事本身测出来。
// 造 2005 行只在这里花一次，别的用例各自 DropTable 重建、不受影响。
func TestBadCaseRepo_ClampsAreExact(t *testing.T) {
	db := setupBadCaseTestDB(t)
	repo := NewBadCaseRepositoryWithDB(db)
	ctx := context.Background()

	const seeded = badCaseMaxExportRows + 5 // 2005：两个上限都要被越过
	rows := make([]*model.BadCase, 0, seeded)
	for i := 0; i < seeded; i++ {
		id := "bc_cap_" + strconv.Itoa(i)
		row := newBadCaseRow(id, model.BadCaseSourceManual, "s_cap", "m_cap_"+id)
		row.Status = model.BadCaseStatusLabeled
		row.Label = model.BadCaseLabelKBMissing
		row.LabelNote = "判定依据"
		row.LabelerID = "op-cap"
		row.FixLayer = model.BadCaseFixLayerKnowledge
		row.LabeledAt = &badCaseBaseAt
		rows = append(rows, row)
	}
	if err := db.CreateInBatches(rows, 250).Error; err != nil {
		t.Fatalf("批量造 %d 行失败：%v", seeded, err)
	}
	// 前置自查：夹具没造够行数，后面两条断言就成了"拿不到数据"而不是"夹住了"。
	if _, total, err := repo.List(ctx, BadCaseListQuery{Statuses: []string{model.BadCaseStatusLabeled}}); err != nil || total != seeded {
		t.Fatalf("前置不成立：labeled 行 %d（期望 %d），err=%v", total, seeded, err)
	}

	// ① 单页上限：给一个比上限还大的 page_size，取回的行数必须正好是上限。
	list, total, err := repo.List(ctx, BadCaseListQuery{
		Statuses: []string{model.BadCaseStatusLabeled}, Page: 1, PageSize: badCaseMaxPageSize + 5})
	if err != nil {
		t.Fatalf("超大页失败：%v", err)
	}
	if len(list) != badCaseMaxPageSize {
		t.Errorf("单页取回 %d 行，期望正好夹在 %d（不夹就等于一句请求拉整表）", len(list), badCaseMaxPageSize)
	}
	if total != seeded {
		t.Errorf("total 应仍是过滤后的总数 %d，实际 %d（夹页不该夹总数）", seeded, total)
	}

	// ② 单次导出上限：同理，取走一批的行数必须正好是上限，剩下 5 条留在队列里。
	claimed, err := repo.ClaimForExport(ctx, nil, badCaseMaxExportRows+5, "bce_cap", badCaseBaseAt)
	if err != nil {
		t.Fatalf("超大 limit 导出失败：%v", err)
	}
	if len(claimed) != badCaseMaxExportRows {
		t.Errorf("一次导出取走 %d 行，期望正好夹在 %d（评测集一次灌进 %d 条未复核样本会淹掉基线）",
			len(claimed), badCaseMaxExportRows, seeded)
	}
	if _, total, err := repo.List(ctx, BadCaseListQuery{Statuses: []string{model.BadCaseStatusLabeled}}); err != nil || total != seeded-badCaseMaxExportRows {
		t.Errorf("导走之后仍待导出的应 %d 条，实际 %d（err=%v）",
			seeded-badCaseMaxExportRows, total, err)
	}
}

func TestBadCaseRepo_CountByStatusKeysAreAlwaysPresent(t *testing.T) {
	db := setupBadCaseTestDB(t)
	repo := NewBadCaseRepositoryWithDB(db)
	ctx := context.Background()

	// 空表：四个键都要在且为 0。少一个键的话前端的"待定 0 条"会变成 undefined。
	counts, err := repo.CountByStatus(ctx)
	if err != nil {
		t.Fatalf("空表聚合失败：%v", err)
	}
	for _, s := range model.BadCaseStatuses {
		if counts[s] != 0 {
			t.Errorf("空表时 %s 应为 0，实际 %d（键缺失会读成 undefined）", s, counts[s])
		}
	}
	if len(counts) != len(model.BadCaseStatuses) {
		t.Errorf("空表时键数应为 %d，实际 %d", len(model.BadCaseStatuses), len(counts))
	}

	row := newBadCaseRow("bc_cnt_1", model.BadCaseSourceManual, "s_cnt", "m_cnt_1")
	if _, err := repo.InsertIfAbsent(ctx, row); err != nil {
		t.Fatalf("造行失败：%v", err)
	}
	markBadCaseRow(t, db, row.ID, model.BadCaseLabelKBStale, badCaseBaseAt)
	counts, err = repo.CountByStatus(ctx)
	if err != nil {
		t.Fatalf("聚合失败：%v", err)
	}
	if counts[model.BadCaseStatusPending] != 0 || counts[model.BadCaseStatusLabeled] != 1 {
		t.Errorf("聚合读数不对：%+v", counts)
	}
	if counts[model.BadCaseStatusExported] != 0 || counts[model.BadCaseStatusDismissed] != 0 {
		t.Errorf("零计数没出现：%+v", counts)
	}

	// 值域外的脏状态不许被吞：今天不在枚举里，明天可能是有人手写 SQL 造出来的第五态。
	if err := db.Exec("INSERT INTO bad_cases (id, source, status, dedup_key, confidence, threshold, retrieved_count, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		"bc_cnt_dirty", model.BadCaseSourceManual, "weird_state", "k_dirty", 0.5, 0.7, 0, badCaseBaseAt, badCaseBaseAt).Error; err != nil {
		t.Fatalf("造脏行失败：%v", err)
	}
	counts, err = repo.CountByStatus(ctx)
	if err != nil {
		t.Fatalf("含脏行聚合失败：%v", err)
	}
	if counts["weird_state"] != 1 {
		t.Errorf("脏状态行被丢掉：%+v（读数不许悄悄少一行）", counts)
	}
}

func TestBadCaseRepo_CountByFixLayer(t *testing.T) {
	db := setupBadCaseTestDB(t)
	repo := NewBadCaseRepositoryWithDB(db)
	ctx := context.Background()

	// 空表：四层恒在且为 0 —— "这层 0 条"与"这层没统计到"必须是两种可读出的状态。
	layers, err := repo.CountByFixLayer(ctx)
	if err != nil {
		t.Fatalf("空表聚合失败：%v", err)
	}
	for _, l := range model.BadCaseFixLayers {
		if layers[l] != 0 {
			t.Errorf("空表时 %s 应为 0，实际 %d", l, layers[l])
		}
	}
	if _, ok := layers["unknown"]; ok {
		t.Errorf("空表不该有 unknown 键：%+v", layers)
	}

	mk := func(id, label string, exported bool) {
		row := newBadCaseRow(id, model.BadCaseSourceManual, "s_layer", "m_"+id)
		if _, err := repo.InsertIfAbsent(ctx, row); err != nil {
			t.Fatalf("造行 %s 失败：%v", id, err)
		}
		markBadCaseRow(t, db, id, label, badCaseBaseAt)
		if exported {
			// 导出态由夹具直接写：本用例测的是聚合读，不拿被测方法造前置。
			if err := db.Model(&model.BadCase{}).Where("id = ?", id).
				Updates(map[string]any{"status": model.BadCaseStatusExported, "eval_set_id": "bce_x"}).Error; err != nil {
				t.Fatalf("置导出态失败：%v", err)
			}
		}
	}
	mk("bc_l_1", model.BadCaseLabelKBMissing, false)
	mk("bc_l_2", model.BadCaseLabelKBStale, false)
	mk("bc_l_3", model.BadCaseLabelRetrieveMiss, false)
	mk("bc_l_4", model.BadCaseLabelGenerationWrong, true) // 已导出同样算一条归因
	mk("bc_l_5", model.BadCaseLabelIntentMisjudge, false)

	layers, err = repo.CountByFixLayer(ctx)
	if err != nil {
		t.Fatalf("聚合失败：%v", err)
	}
	want := map[string]int64{
		model.BadCaseFixLayerKnowledge:  2,
		model.BadCaseFixLayerRetrieval:  1,
		model.BadCaseFixLayerGeneration: 1,
		model.BadCaseFixLayerIntent:     1,
	}
	for l, n := range want {
		if layers[l] != n {
			t.Errorf("%s 应 %d 条，实际 %d（%+v）", l, n, layers[l], layers)
		}
	}

	// 未打标的 pending 行不进任何一层（它还没有归因，算进去等于把"没判"读成"判成某层"）
	pending := newBadCaseRow("bc_l_pending", model.BadCaseSourceLowConfidence, "s_layer", "m_pending")
	if _, err := repo.InsertIfAbsent(ctx, pending); err != nil {
		t.Fatalf("造 pending 失败：%v", err)
	}
	if layers, err = repo.CountByFixLayer(ctx); err != nil {
		t.Fatalf("聚合失败：%v", err)
	}
	var sum int64
	for _, l := range model.BadCaseFixLayers {
		sum += layers[l]
	}
	if sum != 5 {
		t.Errorf("pending 行被算进归因：四层合计 %d，期望 5", sum)
	}

	// 值域外的脏 label 必须带回来成 unknown，不许悄悄丢行
	if err := db.Model(&model.BadCase{}).Where("id = ?", "bc_l_5").Update("label", "bogus_label").Error; err != nil {
		t.Fatalf("造脏 label 失败：%v", err)
	}
	layers, err = repo.CountByFixLayer(ctx)
	if err != nil {
		t.Fatalf("含脏 label 聚合失败：%v", err)
	}
	if layers["unknown"] != 1 {
		t.Errorf("脏 label 没进 unknown：%+v", layers)
	}
	if layers[model.BadCaseFixLayerIntent] != 0 {
		t.Errorf("脏 label 被算进了 intent 层：%+v", layers)
	}
}

func TestBadCaseRepo_ClaimForExport(t *testing.T) {
	db := setupBadCaseTestDB(t)
	repo := NewBadCaseRepositoryWithDB(db)
	ctx := context.Background()

	// 只有 labeled 且 eval_set_id='' 的行可被取走：pending 没判过，dismissed 是"不算坏例"。
	pending := newBadCaseRow("bc_e_pending", model.BadCaseSourceLowConfidence, "s_e", "m_e_pending")
	if _, err := repo.InsertIfAbsent(ctx, pending); err != nil {
		t.Fatalf("造 pending 失败：%v", err)
	}
	labeledSpecs := []struct{ id, label string }{
		{"bc_e_1", model.BadCaseLabelKBMissing},
		{"bc_e_2", model.BadCaseLabelRetrieveMiss},
		{"bc_e_3", model.BadCaseLabelGenerationWrong},
	}
	for i, spec := range labeledSpecs {
		row := newBadCaseRow(spec.id, model.BadCaseSourceLowConfidence, "s_e", "m_e_"+spec.id)
		if _, err := repo.InsertIfAbsent(ctx, row); err != nil {
			t.Fatalf("造行 %s 失败：%v", spec.id, err)
		}
		markBadCaseRow(t, db, spec.id, spec.label, badCaseBaseAt.Add(time.Duration(i)*time.Minute))
	}
	dismissed := newBadCaseRow("bc_e_dis", model.BadCaseSourceManual, "s_e", "m_e_dis")
	dismissed.Status = model.BadCaseStatusDismissed
	dismissed.Label = model.BadCaseLabelKBMissing
	if _, err := repo.InsertIfAbsent(ctx, dismissed); err != nil {
		t.Fatalf("造 dismissed 失败：%v", err)
	}

	// 选不中任何行时报 ErrBadCaseAlreadyClaimed，而不是空集 + nil：
	// 空集会被读成"库里没货"，而事实是"没有符合条件的已判行"。
	if _, err := repo.ClaimForExport(ctx, []string{model.BadCaseLabelIntentMisjudge}, 10, "bce_none", badCaseBaseAt); !errors.Is(err, ErrBadCaseAlreadyClaimed) {
		t.Errorf("无可导行时应报 AlreadyClaimed，实际 %v", err)
	}
	// 失败的那笔不许留下半个 eval_set_id
	var halfWritten int64
	if err := db.Model(&model.BadCase{}).Where("eval_set_id = ?", "bce_none").Count(&halfWritten).Error; err != nil {
		t.Fatalf("计数失败：%v", err)
	}
	if halfWritten != 0 {
		t.Errorf("失败的导出写入了 %d 行", halfWritten)
	}

	rows, err := repo.ClaimForExport(ctx, nil, 2, "bce_first", badCaseBaseAt.Add(time.Hour))
	if err != nil {
		t.Fatalf("首次导出失败：%v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("应取走 2 行，实际 %d", len(rows))
	}
	// labeled_at 升序 ⇒ 先判的先导，运营重跑导出时拿到的名单可复现。
	if rows[0].ID >= rows[1].ID {
		t.Errorf("未按 labeled_at 升序：%s 在 %s 前", rows[0].ID, rows[1].ID)
	}
	for _, r := range rows {
		if r.Status != model.BadCaseStatusExported || r.EvalSetID != "bce_first" || r.ExportedAt == nil {
			t.Errorf("返回值没反映导出态：%s/%q/%v", r.Status, r.EvalSetID, r.ExportedAt)
		}
		var stored model.BadCase
		if err := db.Where("id = ?", r.ID).First(&stored).Error; err != nil {
			t.Fatalf("回读失败：%v", err)
		}
		if stored.Status != model.BadCaseStatusExported || stored.EvalSetID != "bce_first" || stored.ExportedAt == nil {
			t.Errorf("库里没盖上导出态：%s/%q/%v", stored.Status, stored.EvalSetID, stored.ExportedAt)
		}
		if stored.Label != r.Label || stored.QueryText != r.QueryText || stored.AnswerText != r.AnswerText {
			t.Errorf("导出行的现场被改动：库里 %q/%q 返回 %q/%q", stored.Label, stored.QueryText, r.Label, r.QueryText)
		}
		if stored.LabelerID != "op-1" || stored.FixLayer == "" {
			t.Errorf("导出把判定结论洗掉了：%q/%q", stored.LabelerID, stored.FixLayer)
		}
	}

	// 第二次导出只能拿到剩下的那一条 —— 已取走的两行 eval_set_id 非空。
	rest, err := repo.ClaimForExport(ctx, nil, 10, "bce_second", badCaseBaseAt.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("第二次导出失败：%v", err)
	}
	if len(rest) != 1 {
		t.Fatalf("第二次应取走 1 行，实际 %d", len(rest))
	}
	if rest[0].EvalSetID != "bce_second" {
		t.Errorf("第二次导出的集 id 不对：%q", rest[0].EvalSetID)
	}
	// 第三次：全被取走 → sentinel；被取走的行不能进第二个集（AC② 的单归属）
	if _, err := repo.ClaimForExport(ctx, nil, 10, "bce_third", badCaseBaseAt.Add(3*time.Hour)); !errors.Is(err, ErrBadCaseAlreadyClaimed) {
		t.Errorf("空批应报 AlreadyClaimed，实际 %v", err)
	}
	// pending 与 dismissed 都没被顺走
	for _, id := range []string{"bc_e_pending", "bc_e_dis"} {
		var stored model.BadCase
		if err := db.Where("id = ?", id).First(&stored).Error; err != nil {
			t.Fatalf("回读 %s 失败：%v", id, err)
		}
		if stored.EvalSetID != "" {
			t.Errorf("%s 被导走了（status=%s）：只有已打标未导出的行可进评测集", id, stored.Status)
		}
	}

	// 按类目筛：只导 knowledge 层那两个类目
	for i, label := range []string{model.BadCaseLabelKBMissing, model.BadCaseLabelKBStale} {
		id := "bc_e_k" + strconv.Itoa(i)
		row := newBadCaseRow(id, model.BadCaseSourceManual, "s_e", "m_k_"+id)
		if _, err := repo.InsertIfAbsent(ctx, row); err != nil {
			t.Fatalf("造行失败：%v", err)
		}
		markBadCaseRow(t, db, id, label, badCaseBaseAt.Add(time.Duration(i)*time.Minute))
	}
	gen := newBadCaseRow("bc_e_gen", model.BadCaseSourceManual, "s_e", "m_gen")
	if _, err := repo.InsertIfAbsent(ctx, gen); err != nil {
		t.Fatalf("造行失败：%v", err)
	}
	markBadCaseRow(t, db, gen.ID, model.BadCaseLabelGenerationWrong, badCaseBaseAt)

	byLabel, err := repo.ClaimForExport(ctx,
		[]string{model.BadCaseLabelKBMissing, model.BadCaseLabelKBStale}, 10, "bce_kb", badCaseBaseAt.Add(4*time.Hour))
	if err != nil {
		t.Fatalf("按类目导出失败：%v", err)
	}
	if len(byLabel) != 2 {
		t.Errorf("按类目应取 2 行，实际 %d", len(byLabel))
	}
	for _, r := range byLabel {
		if r.Label != model.BadCaseLabelKBMissing && r.Label != model.BadCaseLabelKBStale {
			t.Errorf("类目筛选串档：%s/%q", r.ID, r.Label)
		}
	}
	// 没选中的类目还在（导走它 = 交付一份混了别的责任层的评测集）
	if got, err := repo.GetByID(ctx, gen.ID); err != nil || got.Status != model.BadCaseStatusLabeled || got.EvalSetID != "" {
		t.Errorf("未选中的类目被导出：%+v/%v", got, err)
	}

	// 入参校验：每一项都要报错而不是回空集
	for _, c := range []struct {
		name      string
		labels    []string
		limit     int
		evalSetID string
	}{
		{"空 eval_set_id", nil, 10, ""},
		{"空白 eval_set_id", nil, 10, "  "},
		{"limit=0", nil, 0, "bce_bad"},
		{"limit 负数", nil, -1, "bce_bad"},
		{"未知类目", []string{"bogus_label"}, 10, "bce_bad"},
	} {
		if _, err := repo.ClaimForExport(ctx, c.labels, c.limit, c.evalSetID, badCaseBaseAt); !errors.Is(err, ErrBadCaseInputInvalid) {
			t.Errorf("%s：应判入参非法，实际 %v", c.name, err)
		}
	}

	// 上限夹住：limit=999999 不该变成一条无 LIMIT 的取全表语句
	rows, err = repo.ClaimForExport(ctx, nil, badCaseMaxExportRows+5, "bce_clamp", badCaseBaseAt.Add(5*time.Hour))
	if err != nil {
		t.Fatalf("超大 limit 导出失败：%v", err)
	}
	if len(rows) > badCaseMaxExportRows {
		t.Errorf("单次导出 %d 行，超过上限 %d", len(rows), badCaseMaxExportRows)
	}
}

// 并发导出：两次同时点"导出评测集"，各自拿到互不相交的一批，
// 合起来不重复也不遗漏 —— 同一份样本绝不进两个评测集。
func TestBadCaseRepo_ClaimForExportIsDisjointUnderConcurrency(t *testing.T) {
	db := setupBadCaseTestDB(t)
	repo := NewBadCaseRepositoryWithDB(db)
	ctx := context.Background()

	const total = 12
	for i := 0; i < total; i++ {
		id := "bc_cc_" + strconv.Itoa(i)
		row := newBadCaseRow(id, model.BadCaseSourceManual, "s_cc", "m_cc_"+id)
		if _, err := repo.InsertIfAbsent(ctx, row); err != nil {
			t.Fatalf("造行 %s 失败：%v", id, err)
		}
		// labeled_at 逐个错开：排序并列时 id 兜底，但错开能证明夹住的是时刻不是主键。
		markBadCaseRow(t, db, id, model.BadCaseLabelKBMissing, badCaseBaseAt.Add(time.Duration(i)*time.Second))
	}

	type out struct {
		ids   []string
		setID string
		err   error
	}
	results := make([]out, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			setID := "bce_cc_" + strconv.Itoa(i)
			<-start
			rows, err := repo.ClaimForExport(ctx, nil, 8, setID, badCaseBaseAt)
			if err != nil {
				results[i] = out{setID: setID, err: err}
				return
			}
			ids := make([]string, 0, len(rows))
			for _, r := range rows {
				ids = append(ids, r.ID)
			}
			results[i] = out{ids: ids, setID: setID}
		}(i)
	}
	close(start)
	wg.Wait()

	seen := map[string]string{}
	claimed := 0
	for i, r := range results {
		if r.err != nil {
			// 只允许"整批被对方抢走"这一类：别的错误说明并发路径没设计过。
			if !errors.Is(r.err, ErrBadCaseAlreadyClaimed) {
				t.Errorf("第 %d 次并发导出报非预期错误：%v", i, r.err)
			}
			continue
		}
		if len(r.ids) > 8 {
			t.Errorf("第 %d 次导出取走 %d 行，超过 limit=8", i, len(r.ids))
		}
		for _, id := range r.ids {
			if other, dup := seen[id]; dup {
				t.Errorf("%s 同时进了 %s 和 %s：SKIP LOCKED 没生效", id, other, r.setID)
			}
			seen[id] = r.setID
		}
		claimed += len(r.ids)
	}
	if claimed == 0 {
		t.Fatal("两次并发导出合计取走 0 行")
	}
	// 剩下的还能导出来：证明"没取到的"没被误盖导出态
	left, err := repo.ClaimForExport(ctx, nil, total, "bce_rest", badCaseBaseAt.Add(time.Hour))
	if err != nil {
		if !errors.Is(err, ErrBadCaseAlreadyClaimed) {
			t.Errorf("剩余导出报非预期错误：%v", err)
		}
	} else {
		claimed += len(left)
	}
	if claimed != total {
		t.Errorf("合计取走 %d 行，期望 %d（不重不漏）", claimed, total)
	}
	var wronglyClaimed int64
	if err := db.Model(&model.BadCase{}).Where("status = ? AND eval_set_id = ''", model.BadCaseStatusExported).Count(&wronglyClaimed).Error; err != nil {
		t.Fatalf("计数失败：%v", err)
	}
	if wronglyClaimed != 0 {
		t.Errorf("%d 行状态是 exported 却没盖 eval_set_id：写回不完整", wronglyClaimed)
	}
	var stillLabeled int64
	if err := db.Model(&model.BadCase{}).Where("status = ?", model.BadCaseStatusPending).Count(&stillLabeled).Error; err != nil {
		t.Fatalf("计数失败：%v", err)
	}
	if stillLabeled != 0 {
		t.Errorf("导出把没判过的行也改了态：%d", stillLabeled)
	}
	// 每一行的 eval_set_id 必须等于某一次 Claim 给的集 id：不能出现第三次导出把已导出的改走
	var hijacked int64
	if err := db.Model(&model.BadCase{}).Where("status = ? AND eval_set_id NOT IN ?",
		model.BadCaseStatusExported, []string{"bce_cc_0", "bce_cc_1", "bce_rest"}).Count(&hijacked).Error; err != nil {
		t.Fatalf("计数失败：%v", err)
	}
	if hijacked != 0 {
		t.Errorf("%d 行的评测集归属不在本次给出的三个 id 里", hijacked)
	}
}
