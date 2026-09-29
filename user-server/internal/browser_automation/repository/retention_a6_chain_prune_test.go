package repository

// 攻击补腿：独立攻击复审在 command_log.go 里指出五处「改坏了却没有一条腿读那个字段」
// 的空白。本文件不改动 retention_a6_test.go（并行车道共享工作树，就地编辑易撞车），
// 只**新增**腿，并复用同包已有的 a6DB / a6Row / a6Seed / a6SeedFrozen / a6Digests / a6RunRows /
// a6CountLog / a6Prune / a6Cutoff / a6FrozenStamp 夹具。每条腿头顶一句中文注释写明它要杀的那个变异。
//
// 对应五处发现：
//   1. lastAuditDigest 的 `Order("ordinal DESC")`→ASC：旧腿每 session 至多一条前驱摘要，DESC/ASC 同形。
//   2. ChainHash = auditChainHash(prevChain, batch)→= batch：旧腿要么只对照批摘要，要么用同名算式复算，读不到这一格。
//   3. prevChain = prev.ChainHash→prev.BatchDigest：没有任何腿往同一 session 追加第三条摘要。
//   4. tx.Create(&d)→r.db.Create(&d)（摘要写在删除事务之外）：旧「摘要写不进」腿用的是挡 INSERT 本身的 CHECK 约束，形状不同。
//   5. RowsBefore: rowsBefore→total：经论证在可写 run 行的任何确定性路径上两者恒等——本腿只钉闭合算术，并说明它杀不掉该变异。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"hivemtk-user/internal/browser_automation/model"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const (
	// a6SessionC 专供三轮链腿：不与既有 782201/782202 的任何摘要共用一条链，避免并行残留下串链。
	a6SessionC = uint(782205)
	// a6SessionFresh 是五号腿里「未到期、不该被碰」那一段的 session。
	a6SessionFresh = uint(782206)
	// a6SourceTwo 供需要两次裁剪的腿复用：界刻意取整秒，两次 PruneBefore 传同一个 cutoff，
	// 于是 run 行的 cutoff 也逐字节确定（cutoff_source 必须与既有 a6Source 不同，否则撞
	// browser_audit_prune_runs 上那条「同界同来源」唯一键，第二次裁剪会被库挡下）。
	a6SourceTwo = "test:retention_a6_two"
)

// a6RowAt 与 a6Row 同形，唯一区别是 created_at 写死成传入的瞬时（gorm 的 autoCreateTime 只在零值时填，
// 给了非零值便如实落库）——需要把多行钉在同一瞬时的窗口切分腿用它。
func a6RowAt(sessionID uint, seq int, action, payload string, at time.Time) *model.BrowserCommandLog {
	return &model.BrowserCommandLog{
		SessionID: sessionID, TaskID: a6Task, Seq: seq,
		Direction: "command", Action: action,
		Payload:   datatypes.JSON([]byte(payload)),
		CreatedAt: at,
	}
}

// a6RawDigestCount 跨 session 数摘要行：链断言要在「第三轮没落库」时也能跑，
// 故直接计数而不走 a6Digests（后者按单 session 读列表）。
func a6RawDigestCount(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var n int64
	if err := db.Model(&model.BrowserAuditDigest{}).Count(&n).Error; err != nil {
		t.Fatalf("数摘要行失败：%v", err)
	}
	return n
}

// 杀变异 1+3：lastAuditDigest 的 `ordinal DESC`→ASC（新摘要接不回最新那条）、
// `prevChain = prev.ChainHash`→`prev.BatchDigest`（prev_chain_hash 落错字段）。
// 三轮裁剪后断言链首尾相接、且第三条的 prev_chain_hash 钉在第二条**存储的 chain_hash** 上（不是它的 batch_digest），
// 同时与第一条 chain_hash 不同——DESC 若退化成 ASC，第三轮 ordinal 会撞回 2、摘要写不进、整批回滚。
func TestA6ChainLinksToNewestDigestAcrossThreeRounds(t *testing.T) {
	db := a6DB(t, &model.BrowserCommandLog{}, &model.BrowserAuditDigest{}, &model.BrowserAuditPruneRun{})
	repo := NewBrowserCommandLogRepositoryWithDB(db)
	ctx := context.Background()

	// 三轮各攒 2 行、内容互不相同（payload 里带轮号，保证批摘要与链哈希逐轮有别）。
	rounds := []struct {
		seqs    [2]int
		payload [2]string
	}{
		{[2]int{1, 2}, [2]string{`{"r":"1","x":1}`, `{"r":"1","x":2}`}},
		{[2]int{3, 4}, [2]string{`{"r":"2","x":3}`, `{"r":"2","x":4}`}},
		{[2]int{5, 6}, [2]string{`{"r":"3","x":5}`, `{"r":"3","x":6}`}},
	}
	for i, r := range rounds {
		a6Seed(t, db,
			a6Row(a6SessionC, r.seqs[0], "click", r.payload[0]),
			a6Row(a6SessionC, r.seqs[1], "type", r.payload[1]),
		)
		if _, err := repo.PruneBefore(ctx, a6Cutoff(), a6SourceTwo); err != nil {
			t.Fatalf("第 %d 轮裁剪报错：%v —— ASC 世界会把链头接回最旧那条、ordinal 撞回旧值 → 摘要写不进 → 整批回滚", i+1, err)
		}
	}

	list := a6Digests(t, db, a6SessionC)
	if len(list) != 3 {
		t.Fatalf("摘要条数 %d want 3：%+v（DESC 失效时第三轮根本落不进）", len(list), list)
	}
	first, second, third := list[0], list[1], list[2]
	if first.Ordinal != 1 || second.Ordinal != 2 || third.Ordinal != 3 {
		t.Errorf("ordinal 序列 = %d/%d/%d want 1/2/3：每轮没接在最新摘要之后（ASC 会让 ordinal 反复撞回 2）",
			first.Ordinal, second.Ordinal, third.Ordinal)
	}

	// 变异 3：prev_chain_hash 必须等于**上一条存储的 chain_hash**，而不是上一条的 batch_digest。
	if second.PrevChainHash != first.ChainHash {
		t.Errorf("第二条 prev_chain_hash=%q != 第一条 chain_hash=%q：prevChain 读成了 batch_digest",
			second.PrevChainHash, first.ChainHash)
	}
	if third.PrevChainHash != second.ChainHash {
		t.Errorf("第三条 prev_chain_hash=%q != 第二条 chain_hash=%q：链没接在最新摘要的 chain_hash 上",
			third.PrevChainHash, second.ChainHash)
	}
	// 变异 1（DESC→ASC）：第三条链头若等于第一条 chain_hash，即证明接回的是最旧那条。
	// 前置：第二条 chain_hash 必与第一条不同，否则上面这句对照是空的。
	if second.ChainHash == first.ChainHash {
		t.Fatalf("第二条 chain_hash 与第一条同为 %q：三轮内容没折出可区分的链，本腿对照失效", first.ChainHash)
	}
	if third.PrevChainHash == first.ChainHash {
		t.Errorf("第三条 prev_chain_hash 恰好等于第一条 chain_hash=%q：DESC 退化成了 ASC，新摘要接回了最旧一条",
			first.ChainHash)
	}

	// 三条链逐格复算 chain = sha256(prev_chain_hash || batch_digest)：钉死拼接算式本身。
	for _, d := range list {
		sum := sha256.Sum256([]byte(d.PrevChainHash + d.BatchDigest))
		if want := hex.EncodeToString(sum[:]); d.ChainHash != want {
			t.Errorf("ordinal=%d chain_hash=%q != sha256(prev||batch)=%q：链算式被改动", d.Ordinal, d.ChainHash, want)
		}
	}
}

// 杀变异 2：`ChainHash = auditChainHash(prevChain, w.BatchDigest)`→`= w.BatchDigest`。
// 首轮 prev 为空 → chain=sha256(""||batch)≠batch；次轮 prev 非空（等于首轮 chain）→
// 断言 chain_hash != batch_digest 且 chain_hash == sha256(prev_chain_hash || batch_digest)。
// 变异把 chain_hash 直接写成 batch_digest，首轮即令 `chain != batch` 红，次轮再令 `== sha256` 红；
// 且被挡的 INSERT 约束腿证明 chain_hash 列确实被逐字节落库（64→3 会被约束挡下），排除了「列宽截断」这类假象。
func TestA6ChainHashComposesPrevWithBatch(t *testing.T) {
	db := a6DB(t, &model.BrowserCommandLog{}, &model.BrowserAuditDigest{}, &model.BrowserAuditPruneRun{})
	repo := NewBrowserCommandLogRepositoryWithDB(db)
	ctx := context.Background()

	// 两轮用同一冻结瞬时、同一 seq/action/payload：唯一区别是前一轮的摘要已在库里，
	// 于是第二条的 prev_chain_hash 非空——这正是本腿要吃的「非空前驱」。
	a6SeedFrozen(t, db, a6Row(a6SessionC, 1, "click", `{"x":1}`))
	if _, err := repo.PruneBefore(ctx, a6Cutoff(), a6SourceTwo); err != nil {
		t.Fatalf("首轮裁剪报错：%v", err)
	}
	a6SeedFrozen(t, db, a6Row(a6SessionC, 1, "click", `{"x":1}`))
	if _, err := repo.PruneBefore(ctx, a6Cutoff(), a6SourceTwo); err != nil {
		t.Fatalf("次轮裁剪报错：%v", err)
	}

	list := a6Digests(t, db, a6SessionC)
	if len(list) != 2 {
		t.Fatalf("摘要条数 %d want 2：%+v", len(list), list)
	}
	first, second := list[0], list[1]

	// 排假象：chain_hash 与 batch_digest 必须逐字节都被落库（各 64 位十六进制），
	// 于是「chain != batch」的红不可能是「chain 列被截短」造成的。
	if len(first.ChainHash) != 64 || len(first.BatchDigest) != 64 {
		t.Fatalf("首轮 chain/batch 长度 = %d/%d want 64/64：列宽不足会让下面的对照失真",
			len(first.ChainHash), len(first.BatchDigest))
	}
	// chain_hash 绝不等于它自己声明要混合的 batch_digest。
	if first.ChainHash == first.BatchDigest {
		t.Errorf("首轮 chain_hash == batch_digest=%q：链哈希退化成了批摘要，前驱没参与", first.ChainHash)
	}
	if second.ChainHash == second.BatchDigest {
		t.Errorf("次轮 chain_hash == batch_digest=%q：同上（非空前驱也没参与）", second.ChainHash)
	}
	// 非空前驱：chain = sha256(prev || batch)。
	if second.PrevChainHash == "" {
		t.Fatalf("次轮 prev_chain_hash 为空：本腿要钉的「非空前驱」不存在，对照失效")
	}
	sum := sha256.Sum256([]byte(second.PrevChainHash + second.BatchDigest))
	if want := hex.EncodeToString(sum[:]); second.ChainHash != want {
		t.Errorf("次轮 chain_hash=%q want sha256(prev||batch)=%q：链没把前驱折进来", second.ChainHash, want)
	}
}

// 杀变异 4：`tx.Create(&d)`→`r.db.WithContext(ctx).Create(&d)`（摘要写在删除事务之外）。
// 造一个「挡删除、不挡插入」的行级触发器，且只对 session B 生效：
// 一个事务里先处理 session A（摘要甲写入、删除成功）、再处理 session B（摘要乙写入、删除抛错→整事务回滚）。
// 用 tx 时摘要甲/乙随事务一起消失（digest==0，摘要绝不为还在表里的行谎称已删）；
// 用 r.db 时它们落在池连接上自动提交、逃过回滚（digest>0）——正是这一格要杀的。
// 末尾反向锁：摘掉触发器后同一批必须裁得动、恰好补出两条摘要，证明失败只出在被挡的 DELETE 上。
func TestA6PruneFailsCloseWhenDeleteFailsMidRun(t *testing.T) {
	db := a6DB(t, &model.BrowserCommandLog{}, &model.BrowserAuditDigest{}, &model.BrowserAuditPruneRun{})
	install := `CREATE FUNCTION a6_block_del_fn() RETURNS trigger AS $fn$
BEGIN
	IF OLD.session_id = 782202 THEN
		RAISE EXCEPTION 'a6: 拦截 session % 的删除', OLD.session_id;
	END IF;
	RETURN OLD;
END $fn$ LANGUAGE plpgsql`
	// 注掉 CREATE TRIGGER 后 ENABLE ALWAYS TRIGGER 报错，正好用来钉「夹具是否真的装上了」这一格。
	trigger := `CREATE TRIGGER a6_block_del BEFORE DELETE ON browser_command_log ` +
		`FOR EACH ROW EXECUTE FUNCTION a6_block_del_fn()`
	// 池连接跑在 session_replication_role='replica'（testdb.go:180）下，默认（ORIGIN）触发器静默不触发，
	// 必须 ENABLE ALWAYS 才有牙（见 TestA6PruneFailsCloseWhenDigestWriteBlocked 的同类实测）。
	always := `ALTER TABLE browser_command_log ENABLE ALWAYS TRIGGER a6_block_del`
	remove := func() error {
		return db.Exec(`DROP TRIGGER IF EXISTS a6_block_del ON browser_command_log`).Error
	}
	if err := db.Exec(install).Error; err != nil {
		t.Fatalf("造删除触发器函数失败（本腿前提）：%v", err)
	}
	if err := db.Exec(trigger).Error; err != nil {
		t.Fatalf("建删除触发器失败（本腿前提）：%v", err)
	}
	if err := db.Exec(always).Error; err != nil {
		t.Fatalf("启用 ALWAYS 触发器失败：%v —— 夹具没装上：若 CREATE TRIGGER 一步被注掉，这里必然报错", err)
	}
	t.Cleanup(func() {
		if err := remove(); err != nil {
			t.Errorf("删除拦截触发器失败，同库后续用例会撞上：%v", err)
		}
		if err := db.Exec(`DROP FUNCTION IF EXISTS a6_block_del_fn()`).Error; err != nil {
			t.Errorf("删除拦截函数失败：%v", err)
		}
	})

	// 先播种（A、B 各 2 行，推到保留期之前）。B 排在 A 之后：pruneWindowSQL 按 session_id 升序，
	// 于是同一事务里先处理 A（摘要甲写入、删除成功）再处理 B（摘要乙写入、删除抛错→整事务回滚）。
	a6Seed(t, db,
		a6Row(a6SessionA, 1, "click", `{"a":1}`),
		a6Row(a6SessionA, 2, "click", `{"a":2}`),
		a6Row(a6SessionB, 1, "click", `{"b":1}`),
		a6Row(a6SessionB, 2, "click", `{"b":2}`),
	)

	// 前提自证（各自回滚，不动已播的种）：A 删得动、B 被挡，夹具本身可证伪。
	// 空表上 FOR EACH ROW 触发器根本不触发，所以这一步必须落在有行的种子上做。
	atx := db.Begin()
	aErr := atx.Where("session_id = ?", a6SessionA).Delete(&model.BrowserCommandLog{}).Error
	atx.Rollback()
	if aErr != nil {
		t.Fatalf("夹具前提失败：拦截器不该挡 session A 的删除：%v", aErr)
	}
	btx := db.Begin()
	bErr := btx.Where("session_id = ?", a6SessionB).Delete(&model.BrowserCommandLog{}).Error
	btx.Rollback()
	if bErr == nil {
		t.Fatal("夹具前提失败：拦截器没挡住 session B 的删除，本腿无判据")
	}

	pruned, err := NewBrowserCommandLogRepositoryWithDB(db).PruneBefore(context.Background(), a6Cutoff(), a6Source)
	if err == nil {
		t.Fatalf("删除被挡却返回了无错（裁 %d 行）：DELETE 失败被吞掉了", pruned)
	}
	// 事务回滚 → A、B 的行都还在（A 那句删成功的 DELETE 也被一并撤销）。
	if n := a6CountLog(t, db, a6SessionA); n != 2 {
		t.Errorf("session A 剩 %d 行 want 2：整事务回滚没把 A 的删除撤销", n)
	}
	if n := a6CountLog(t, db, a6SessionB); n != 2 {
		t.Errorf("session B 剩 %d 行 want 2：删失败的行不该消失", n)
	}
	// 核心判据：摘要一条都不该留下。tx 下随事务回滚=0；换成 r.db 落在池连接上=2。
	if n := a6RawDigestCount(t, db); n != 0 {
		t.Errorf("删除失败后库里留下 %d 条摘要 want 0：摘要写在删除事务之外，会对仍在其外的行谎称已删（摘要一旦说谎就不是凭据）", n)
	}

	// 反向锁：摘掉拦截器后，同一批必须裁得动，且恰好补出两条摘要（A、B 各一），行清空。
	if err := remove(); err != nil {
		t.Fatalf("摘掉拦截触发器失败，无法做反向锁：%v", err)
	}
	if got := a6Prune(t, db); got != 4 {
		t.Errorf("拦截器摘掉后裁了 %d 行 want 4：上面的红是夹具把路一起堵死，不是删除事务的牙", got)
	}
	if n := a6RawDigestCount(t, db); n != 2 {
		t.Errorf("反向锁后摘要 %d 条 want 2：一次干净裁剪该为两个 session 各补一条", n)
	}
}

// 钉闭合算术：run 行的 rows_before == rows_pruned + rows_left_after（本界内、高水位之下未删的行）。
// 两 session 交错播 2×pruneBatchRows+1 行，逼出多批、多摘要的裁剪。它**不针对**变异 5——见报告：
// 在能写下 run 行的任何确定性路径上 total 恒等于 rowsBefore（循环把入口数到的那批删空才返回，
// 中途任何行数不符都会回滚且不写 run 行），故 `RowsBefore: total` 与 `RowsBefore: rowsBefore` 逐字节同值、
// 无法被任何断言区分。本腿只把「零遗留」这一闭合不变量钉住（若哪天窗口/删除谓词不再一致，它会红）。
func TestA6RunRowClosesArithmeticUnderTwoBatches(t *testing.T) {
	db := a6DB(t, &model.BrowserCommandLog{}, &model.BrowserAuditDigest{}, &model.BrowserAuditPruneRun{})

	// 两 session 交错落库（id 混排），窗口按 id 升序切批，逼出跨 session 的多批裁剪。
	n := 2*pruneBatchRows + 1
	sids := []uint{a6SessionA, a6SessionB}
	seqA, seqB := 1, 1
	for i := 0; i < n; i++ {
		sid := sids[i%2]
		seq := seqA
		if sid == a6SessionB {
			seq = seqB
		}
		row := a6RowAt(sid, seq, "click", fmt.Sprintf(`{"i":%d}`, i), a6FrozenStamp)
		if sid == a6SessionA {
			seqA++
		} else {
			seqB++
		}
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("种子落库失败（第 %d 行）：%v", i, err)
		}
	}
	// 另落 3 行未到期（created_at 保持 now，远在冻结界之后），验证 rows_left_after 只数「本界内」的。
	for i := 0; i < 3; i++ {
		if err := db.Create(a6Row(a6SessionFresh, i+1, "click", `{"fresh":1}`)).Error; err != nil {
			t.Fatalf("未到期种子落库失败：%v", err)
		}
	}

	pruned := a6Prune(t, db)
	if pruned != int64(n) {
		t.Fatalf("裁剪行数 %d want %d", pruned, n)
	}

	// 本界（frozen cutoff）内、高水位之下未删的行：应为 0（循环必须跑到窗口空才写 run 行）。
	var left int64
	if err := db.Model(&model.BrowserCommandLog{}).
		Where("created_at < ?", a6Cutoff()).Count(&left).Error; err != nil {
		t.Fatalf("数界内剩余行失败：%v", err)
	}

	runs := a6RunRows(t, db)
	if len(runs) != 1 {
		t.Fatalf("run 行数 %d want 1：%+v", len(runs), runs)
	}
	run := runs[0]
	// 闭合恒等式：入口该走的 == 实删的 + 界内没走掉的。
	if run.RowsBefore != run.RowsPruned+left {
		t.Errorf("闭合算术塌了：rows_before=%d != rows_pruned=%d + rows_left=%d", run.RowsBefore, run.RowsPruned, left)
	}
	if left != 0 {
		t.Errorf("界内还剩 %d 行未裁：循环没跑到窗口空就写了 run 行，摘要会漏记这一段", left)
	}
	// 未到期那 3 行必须毫发无损（谓词丢了 cutoff 的反证）。
	if kept := a6CountLog(t, db, a6SessionFresh); kept != 3 {
		t.Errorf("未到期行被裁（剩 %d want 3）：分批/闭合谓词丢了 cutoff", kept)
	}
	// 摘要覆盖的行数总和 == 实删行数（每行被删前落进且只落进一条摘要）。
	var digestRows int64
	if err := db.Model(&model.BrowserAuditDigest{}).
		Where("session_id IN ?", []uint{a6SessionA, a6SessionB}).
		Select("COALESCE(sum(row_count),0)").Scan(&digestRows).Error; err != nil {
		t.Fatalf("汇总摘要行数失败：%v", err)
	}
	if digestRows != run.RowsPruned {
		t.Errorf("摘要覆盖 %d 行、实删 %d 行：有行绕开摘要消失了", digestRows, run.RowsPruned)
	}
	// 记录并说明：rows_before == rows_pruned（left==0）——这正是变异 5 无从落脚的原因。
	if run.RowsBefore != run.RowsPruned {
		t.Errorf("rows_before=%d != rows_pruned=%d：与恒等式矛盾（left 已判 0）", run.RowsBefore, run.RowsPruned)
	}
	t.Logf("钉死：rows_before=%d == rows_pruned=%d == 入口到期数，故 `RowsBefore: total` 与实现逐字节同值、杀不掉",
		run.RowsBefore, run.RowsPruned)
}
