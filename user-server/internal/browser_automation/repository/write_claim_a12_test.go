package repository

// 批20f（A12）契约锁：双发闸的最后一层必须住在存储层。
//
// 立项依据（§8.3-20，批16b 自审 B4）：双发闸今天是**读后再写**——guardResubmit 先
// FindSubmitAttempt（一条普通 First，无 FOR UPDATE、无 advisory lock），通过后才由
// recordSubmitState 写台账。同一时刻并发的另一条腿看到的是同一条「查不到」，于是两条腿
// 都过闸、都提交一次，而评论不可撤回。同层的三条并发防护（t.Status=="running"、
// CountRunningByTask、CountRunningByUser）全是同一形状，task.go 那句「靠 DB 唯一性兜底竞态」
// 的注释下面并不存在那样一条约束。
//
// 正解不是再补一道 check-then-act（那只是把同一种形状复制一遍），而是把「这次提交是否已被
// 记过」交给**插入语句本身**判定：一张只有一把唯一键的小表，占坑即 INSERT，撞约束即拒发。
//
// 为什么不直接把唯一索引压在 browser_steps 上（原登记的写法）：拦阻集合是
// sent/unattributed/verified 三态，而「点击从未发生」的那一态（prepared 或空）必须**不**占坑，
// 否则一次被浮层遮住的写步会把唯一正确的处置（等页面停下再跑一次）永久拦死。
// 同一张表要同时表达「在途声明」与「已发生尝试」两种生命周期，压一个索引做不到——
// 所以声明独立成表，生命周期由「占坑 → 跨越则保留 / 从未发生则释放」管，
// browser_steps 那三列的语义一个都不动。
//
// 手法：真测试库（testutil.NewTestDB），A5/A6 两格刻意绕开本仓储的 Go API 直接下语句 /
// 开两个 goroutine——「应用层判得对」正是本批要否掉的东西，锁必须打在库上。
import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"hivemtk-user/internal/browser_automation/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// a12UniqueIndex 索引名是契约的一部分：A5 的判据要认准这一条约束，
// 换名必须同时改测试（否则「撞了别的约束」也会被读成闸门生效）。
const a12UniqueIndex = "uk_browser_write_claims_task_text"

const (
	a12TaskA  = uint(782101)
	a12TaskB  = uint(782102)
	a12Hash   = "1a2b3c4d"
	a12Hash2  = "4d3c2b1a"
	a12SessA  = uint(782201)
	a12SessB  = uint(782202)
	a12StepA  = uint(782301)
	a12StepB  = uint(782302)
	a12StepC  = uint(782303)
	a12DBHash = "deadbeef" // 只有 A5/A6 两格用：同任务同文本，专供直插与两腿抢
)

func a12Repo(t *testing.T) (BrowserWriteClaimRepository, *gorm.DB) {
	t.Helper()
	db := testutil.NewTestDB(t, &model.BrowserWriteClaim{})
	if db == nil {
		t.Fatal("测试库不可达：唯一约束在不在、抢坑谁赢，全都判不了（不 Skip，跳过等于没锁）")
	}
	return NewBrowserWriteClaimRepositoryWithDB(db), db
}

// A1 占坑成功、同一步重复占坑幂等。
// 幂等这一格不是洁癖：sent→verified 之间台账会重入同一个键，若第二次占坑把持有者判成
// 「别的步」，跨过提交点的那一步就会被自己的声明拦死，等于给正常路径造出一条永久红。
func TestWriteClaimTakeAndIdempotent(t *testing.T) {
	repo, db := a12Repo(t)
	ctx := context.Background()

	holder, err := repo.ClaimWriteSlot(ctx, a12TaskA, a12SessA, a12StepA, a12Hash)
	if err != nil || holder != nil {
		t.Fatalf("首次占坑应成功，got holder=%v err=%v", holder, err)
	}
	var n int64
	if err := db.Model(&model.BrowserWriteClaim{}).Where("task_id = ?", a12TaskA).Count(&n).Error; err != nil {
		t.Fatalf("计数失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("声明行数=%d want 1", n)
	}

	holder, err = repo.ClaimWriteSlot(ctx, a12TaskA, a12SessA, a12StepA, a12Hash)
	if err != nil || holder != nil {
		t.Fatalf("同一步重复占坑应幂等放行，got holder=%v err=%v", holder, err)
	}
	if err := db.Model(&model.BrowserWriteClaim{}).Where("task_id = ?", a12TaskA).Count(&n).Error; err != nil {
		t.Fatalf("计数失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("重复占坑后又落了一行（%d）：幂等是靠「再插一行」实现的，键位从此被两行共同占住", n)
	}
}

// A2 他腿占坑必须被拒，且把持有者事实交回去（上层文案要说「哪一步、哪次会话」，
// 只有一句「已存在」人就没法判该不该人工释放）。
func TestWriteClaimHeldByOtherStep(t *testing.T) {
	repo, _ := a12Repo(t)
	ctx := context.Background()
	if _, err := repo.ClaimWriteSlot(ctx, a12TaskA, a12SessA, a12StepA, a12Hash); err != nil {
		t.Fatalf("前置占坑失败: %v", err)
	}

	holder, err := repo.ClaimWriteSlot(ctx, a12TaskA, a12SessB, a12StepB, a12Hash)
	if err != nil {
		t.Fatalf("他腿占坑应返回持有者而不是报错，got err=%v", err)
	}
	if holder == nil {
		t.Fatal("他腿占坑被放行——双发闸在存储层形同虚设")
	}
	if holder.StepRowID != a12StepA || holder.SessionID != a12SessA {
		t.Fatalf("持有者事实不对：step=%d session=%d want step=%d session=%d",
			holder.StepRowID, holder.SessionID, a12StepA, a12SessA)
	}
}

// A3 键的粒度：换文本、换任务都得是另一把坑。
// 反过来说这一格守的是「别把闸门做到整表」——若唯一键只含 text_hash，同一句文案在两个任务
// 里就只能发一次；若只含 task_id，一个任务的第二条评论直接被拦。
func TestWriteClaimKeysAreIndependent(t *testing.T) {
	repo, _ := a12Repo(t)
	ctx := context.Background()
	if _, err := repo.ClaimWriteSlot(ctx, a12TaskA, a12SessA, a12StepA, a12Hash); err != nil {
		t.Fatalf("前置占坑失败: %v", err)
	}
	cases := []struct {
		name   string
		taskID uint
		hash   string
		stepID uint
	}{
		{"同任务换文本", a12TaskA, a12Hash2, a12StepB},
		{"同文本换任务", a12TaskB, a12Hash, a12StepC},
	}
	for _, c := range cases {
		holder, err := repo.ClaimWriteSlot(ctx, c.taskID, a12SessB, c.stepID, c.hash)
		if err != nil || holder != nil {
			t.Fatalf("%s：应各自占坑成功，got holder=%v err=%v", c.name, holder, err)
		}
	}
}

// A4 释放只放自己那把：跨持有者的释放必须是零影响。
// 这条是「从未发生 ⇒ 可安全重下发」那半边链子的地基——释放写成 WHERE text_hash=? 的话，
// 一条被判 *_not_found 的步会把别人正跨着提交点的声明一起删掉，双发闸当场消失。
func TestWriteClaimReleaseOnlyOwns(t *testing.T) {
	repo, _ := a12Repo(t)
	ctx := context.Background()
	if _, err := repo.ClaimWriteSlot(ctx, a12TaskA, a12SessA, a12StepA, a12Hash); err != nil {
		t.Fatalf("前置占坑失败: %v", err)
	}

	affected, err := repo.ReleaseWriteSlot(ctx, a12StepB, a12Hash)
	if err != nil {
		t.Fatalf("释放他人声明应无错返回 0 行，got err=%v", err)
	}
	if affected != 0 {
		t.Fatalf("非持有者释放了 %d 行", affected)
	}
	if _, err := repo.FindWriteClaim(ctx, a12TaskA, a12Hash); err != nil {
		t.Fatalf("被他人误删：FindWriteClaim=%v", err)
	}

	affected, err = repo.ReleaseWriteSlot(ctx, a12StepA, a12Hash)
	if err != nil || affected != 1 {
		t.Fatalf("持有者释放应删 1 行，got affected=%d err=%v", affected, err)
	}
	if _, err := repo.FindWriteClaim(ctx, a12TaskA, a12Hash); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("释放后应查不到，got err=%v", err)
	}
	// 释放必须真的腾出键位：下一轮同文本要能重新占坑（否则一次遮挡就把评论永久锁死）
	if _, err := repo.ClaimWriteSlot(ctx, a12TaskA, a12SessB, a12StepB, a12Hash); err != nil {
		t.Fatalf("释放后重新占坑失败: %v", err)
	}
	// 收尾会连着调两次释放（never-executed 分支与步结束兜底各一次），第二次必须是安静的
	// 0 行而不是「删不到东西」的错误——否则正常路径每次都以一次假故障收场。
	if affected, err = repo.ReleaseWriteSlot(ctx, a12StepB, a12Hash); err != nil || affected != 1 {
		t.Fatalf("持有者释放应删 1 行，got affected=%d err=%v", affected, err)
	}
	if affected, err = repo.ReleaseWriteSlot(ctx, a12StepB, a12Hash); err != nil || affected != 0 {
		t.Fatalf("二次释放应 0 行无错，got affected=%d err=%v", affected, err)
	}
}

// A5 约束住在库里，不是应用层的 if：绕开本仓储直写第二条同键，必须被 PG 判 23505。
// 这一格是整个 A12 的立身之本——如果约束是代码里的「先查再插」，这条语句会成功，
// 而上一轮的并发窗口就还在原地。
func TestWriteClaimConstraintLivesInStorage(t *testing.T) {
	_, db := a12Repo(t)
	raw := func(hash string, stepID uint) error {
		return db.Exec(`INSERT INTO browser_write_claims (task_id, text_hash, step_row_id, session_id, created_at)
			VALUES (?, ?, ?, ?, now())`, a12TaskA, hash, stepID, a12SessA).Error
	}
	if err := raw(a12DBHash, a12StepA); err != nil {
		t.Fatalf("首条直插应成功: %v", err)
	}
	err := raw(a12DBHash, a12StepB)
	if err == nil {
		t.Fatal("同键第二条直插成功了：唯一约束不存在，双发闸仍然只是一段 Go 代码")
	}
	// 判据要「SQLSTATE 23505 **且**约束名」两个条件同时命中（本仓口径，见 approval_request.go:142）：
	// 只认 23505 的话，本表上任何一条**别的**唯一约束被撞到时，这里也会判成「闸门有效」。
	msg := err.Error()
	if !strings.Contains(msg, "23505") || !strings.Contains(msg, a12UniqueIndex) {
		t.Fatalf("第二条同键应撞 %s（SQLSTATE 23505），got: %v", a12UniqueIndex, err)
	}
}

// A6 两腿同时抢一把坑：恰好一个赢。
// TOCTOU 的正面对抗——读闸（FindSubmitAttempt）在这一刻两边都查空，只有 INSERT 能分出先后。
// 用真 goroutine + WaitGroup，不 mock：mock 出来的「并发」只会证明代码按调用顺序跑。
func TestWriteClaimConcurrentTakeHasOneWinner(t *testing.T) {
	repo, _ := a12Repo(t)
	ctx := context.Background()
	const legs = 8
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		granted []uint
		held    int
		errored int
	)
	start := make(chan struct{})
	for i := 0; i < legs; i++ {
		wg.Add(1)
		stepID := uint(782400 + i)
		go func() {
			defer wg.Done()
			<-start
			holder, err := repo.ClaimWriteSlot(ctx, a12TaskB, a12SessA, stepID, a12DBHash)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err != nil:
				errored++
			case holder == nil:
				granted = append(granted, stepID)
			default:
				held++
			}
		}()
	}
	close(start)
	wg.Wait()

	if errored != 0 {
		t.Fatalf("%d 条腿报错（占坑的失败面必须是「他人持有」或「自己持有」两种结论，不是 error）", errored)
	}
	if len(granted) != 1 {
		t.Fatalf("%d 条腿同时占到同一把坑（want 1）：唯一约束没拦住并发", len(granted))
	}
	if held != legs-1 {
		t.Fatalf("其余腿应各拿到持有者事实，got held=%d want %d", held, legs-1)
	}
}

// A7 schema 契约：唯一索引在、且这张表**没有软删列**。
// 加 DeletedAt 的后果是具体的：gorm 的默认作用域会把软删行排除在唯一性之外（索引谓词
// 含 deleted_at IS NULL 的那套语义），删过的键位就腾出来了——而这张表存在的唯一理由
// 就是「键位不腾」。裁剪同理：本表按设计永不裁剪（一行 ≈ 40 字节，一年也到不了百万）。
func TestWriteClaimSchemaContract(t *testing.T) {
	_, db := a12Repo(t)
	m := db.Migrator()
	if !m.HasTable(&model.BrowserWriteClaim{}) {
		t.Fatal("表没建出来：模型未登记或标签写坏")
	}
	if m.HasColumn(&model.BrowserWriteClaim{}, "deleted_at") {
		t.Error("browser_write_claims 不得有 deleted_at：软删等于把键位腾出来，闸门随之消失")
	}
	var idxNames []string
	if err := db.Raw(`SELECT indexname FROM pg_indexes WHERE tablename = 'browser_write_claims' AND indexdef ILIKE '%UNIQUE%'`).
		Scan(&idxNames).Error; err != nil {
		t.Fatalf("索引查询失败: %v", err)
	}
	if len(idxNames) == 0 {
		t.Fatal("browser_write_claims 上没有 UNIQUE 索引：A5 那条直插能过，就说明闸门不在库里")
	}
	named := false
	for _, n := range idxNames {
		if n == a12UniqueIndex {
			named = true
		}
	}
	if !named {
		t.Fatalf("唯一索引不叫 %s（got %v）：A5 的约束名判据会失去指认对象", a12UniqueIndex, idxNames)
	}
}

// A8 键不完整一律报错，不占坑也不放行。
// 两个不完整各有一种坏法：
//   - 空 text_hash 若被允许入库，整表所有「没正文」的写会并到同一把键上，
//     第一条把后面所有任务的写全部拦死（键位本来是 (task, 文本)，没了文本就只剩 task）；
//   - step_row_id=0 的声明**永远删不掉**——释放按 (step_row_id, text_hash) 命中，
//     而 0 不是任何一行的 id，于是这一格键位永久占死。
//
// 判据取「报错」而不是「静默不占」：静默不占在调用方看起来和占到一模一样，
// 一次键位没落地的写就会被当成有闸门保护着发出去（批16 A8 的同一条口径）。
func TestWriteClaimIncompleteKeyRefuses(t *testing.T) {
	repo, db := a12Repo(t)
	ctx := context.Background()
	cases := []struct {
		name            string
		stepRowID       uint
		hash            string
		wantMsgFragment string
	}{
		{"空正文哈希", a12StepA, "", "text_hash"},
		{"零号步行", 0, a12Hash, "step_row_id"},
	}
	for _, c := range cases {
		_, err := repo.ClaimWriteSlot(ctx, a12TaskB, a12SessA, c.stepRowID, c.hash)
		if err == nil {
			t.Fatalf("%s：键不完整却占了坑/判成放行——双发闸在这一格是摆设", c.name)
		}
		if !strings.Contains(err.Error(), c.wantMsgFragment) {
			t.Errorf("%s：错误没点出缺的是哪一半键（got %v，需含 %q）", c.name, err, c.wantMsgFragment)
		}
	}
	var n int64
	if err := db.Raw(`SELECT count(*) FROM browser_write_claims WHERE task_id = ?`, a12TaskB).
		Scan(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("键不完整的占坑留下了 %d 行：拒了还落库，等于往黑名单里塞没人认领的键位", n)
	}
}
