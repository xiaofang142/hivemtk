// kbrelease_test.go T-P9-02：读闸门叶子包（无库可测的那一半）。
//
// 这里锁的是"闸门这段 SQL 什么时候加、由谁加"，也就是 AC① 的读侧判据与 AC② 的
// 零搬运前提。真库行为（相关子查询到底隐不隐藏某一行）在
// internal/aiagent/... 与 internal/repository 的带库用例里，本文件刻意不碰连接：
//
//  1. 三态旗子的解析（含那条最容易忘的取舍：**布尔式真值只到 shadow**）；
//  2. off / shadow 两档下谓词必须是空串 —— 观察期"什么都不改"若改了，就没有对照基线；
//  3. 谓词形状（相关子查询、认 governed、认两个版本列），因为它一旦改成常量拼接，
//     跨库全量召回就会拿一个库的号去判另一个库的行；
//  4. 每次调用现读 env（热切）：缓存一次档位会让"改 env 出事"这条唯一的应急通路失效；
//  5. 所有快路径：无句柄 / 无归属 / 空集合必须**零查询**返回，而不是回一个错误。
package kbrelease

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"

	"gorm.io/gorm"
	"gorm.io/gorm/utils/tests"
)

func TestKbRelease_parseMode(t *testing.T) {
	cases := map[string]mode{
		"":          modeOff, // 未设置 = 与今天逐字节相同
		"off":       modeOff,
		"OFF":       modeOff,
		" false":    modeOff,
		"0":         modeOff,
		"no":        modeOff,
		"n":         modeOff,
		"none":      modeOff,
		"disabled":  modeOff,
		"shadow":    modeShadow,
		"Observe":   modeShadow,
		"watch":     modeShadow,
		"log":       modeShadow,
		"report":    modeShadow,
		"on":        modeOn,
		"enforce":   modeOn,
		"active":    modeOn,
		"true":      modeShadow, // ⚠️ 布尔真值只到 shadow：能改变生产召回集的档位必须显式 on
		"1":         modeShadow,
		"yes":       modeShadow,
		"y":         modeShadow,
		"banana":    modeOff, // 认不出的值保守回 off（并告警，见 TestKbRelease_UnknownValueWarns）
		"on shadow": modeOff,
	}
	for raw, want := range cases {
		if got := parseMode(raw); got != want {
			t.Errorf("parseMode(%q)=%q，期望 %q", raw, got, want)
		}
	}
}

// TestKbRelease_FalseyBooleansStayOff 与上一条成对：把 false/0/no 判成 shadow 同样是缺陷
// （运营以为关了，其实一直在统计）。这里单独钉一次，防的是上表被改坏时无人发现方向。
func TestKbRelease_FalseyBooleansStayOff(t *testing.T) {
	for _, raw := range []string{"false", "0", "no", "off", ""} {
		if parseMode(raw) != modeOff {
			t.Errorf("parseMode(%q) 必须是 off", raw)
		}
	}
}

func TestKbRelease_PredicateEmptyUnlessOn(t *testing.T) {
	for _, raw := range []string{"", "off", "shadow", "true", "1", "yes"} {
		t.Setenv(FlagEnv, raw)
		if p := VisiblePredicate(); p != "" {
			t.Errorf("%s=%q 时谓词必须为空，实得 %q", FlagEnv, raw, p)
		}
		if a := AndVisible(); a != "" {
			t.Errorf("%s=%q 时拼接片段必须为空串（非空才追加），实得 %q", FlagEnv, raw, a)
		}
		if GateOn() {
			t.Errorf("%s=%q 时 GateOn 必须为假", FlagEnv, raw)
		}
	}
}

// TestKbRelease_PredicateShape 闸门开着时谓词的**形状**。
//
// 这四个片段各守一个具体的坏法：
//   - NOT EXISTS/EXISTS 结构：整段是"隐藏条件取反"，写成正向匹配就把未治理的库全挡了；
//   - kr.product_id = knowledge_chunks.product_id：相关子查询（逐行按各自的库判）；
//     换成常量拼接，跨库召回会拿一个库的生效版本判另一个库的行；
//   - kr.governed：库级那道锁，摘掉它"未启用的库"也会被挡；
//   - retired_version > 0 AND ... <= effective：回滚后老内容要重新可见，判据在这一条上。
func TestKbRelease_PredicateShape(t *testing.T) {
	t.Setenv(FlagEnv, "on")
	p := VisiblePredicate()
	if !strings.HasPrefix(p, "NOT EXISTS (") {
		t.Errorf("谓词应以 NOT EXISTS ( 起头，实得 %q", p)
	}
	for _, frag := range []string{
		"kb_releases kr",
		"kr.product_id = knowledge_chunks.product_id",
		"kr.governed",
		"knowledge_chunks.kb_version > kr.effective_version",
		"knowledge_chunks.retired_version > 0",
		"retired_version <= kr.effective_version",
	} {
		if !strings.Contains(p, frag) {
			t.Errorf("谓词缺少片段 %q：\n%s", frag, p)
		}
	}
	if a := AndVisible(); !strings.HasPrefix(a, " AND NOT EXISTS (") {
		t.Errorf("AndVisible 应自带前导 AND，实得 %q", a)
	}
	if !strings.Contains(AndVisible(), p) {
		t.Error("AndVisible 与 VisiblePredicate 不是同一份判据（两处各写一遍迟早分叉）")
	}
}

// TestKbRelease_ShadowsOrOn 写侧档位的分界：只有 off 不碰版本账。
//
// shadow 必须在内：观察期的 would-hide 读数要按真实版本号算，否则"切到 on 会怎样"只能靠猜。
func TestKbRelease_ShadowsOrOn(t *testing.T) {
	for raw, want := range map[string]bool{"": false, "off": false, "true": true, "shadow": true, "on": true} {
		t.Setenv(FlagEnv, raw)
		if got := ShadowsOrOn(); got != want {
			t.Errorf("%s=%q 时 ShadowsOrOn=%t，期望 %t", FlagEnv, raw, got, want)
		}
	}
}

// TestKbRelease_ModeIsReadPerCall 每次调用现读 env（热切能力）。
//
// 这条看起来多余，防的是"给档位加一层 sync.Once 缓存"这种优化：启动期日志与运营端点
// 都靠现读回显当前档位，缓存之后 /api/kb-releases/gate 会一直报启动那一刻的值，
// 而出事时运维改的正是环境变量。
func TestKbRelease_ModeIsReadPerCall(t *testing.T) {
	t.Setenv(FlagEnv, "off")
	if GateOn() || ModeForLog() != "off" {
		t.Fatal("初始应为 off")
	}
	t.Setenv(FlagEnv, "on")
	if !GateOn() || ModeForLog() != "on" {
		t.Fatalf("改 env 后应立即生效，实得 %s", ModeForLog())
	}
	t.Setenv(FlagEnv, "shadow")
	if GateOn() {
		t.Error("shadow 档 GateOn 必须为假（观察期什么都不改）")
	}
	if !ShadowsOrOn() {
		t.Error("shadow 档 ShadowsOrOn 必须为真")
	}
}

func newDryRunDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(tests.DummyDialector{}, &gorm.Config{DryRun: true})
	if err != nil {
		t.Fatalf("dry-run 句柄构造失败: %v", err)
	}
	return db
}

// TestKbRelease_WhereVisible 三个分支：空句柄 / 闸门关（原样返回同一实例）/ 闸门开（挂上条件）。
//
// "闸门关必须返回**同一个**实例"是这条断言的全部价值：`Where("")` 在不同 GORM 版本上
// 要么生成空条件要么报错，而判空收在本函数里，11 个召回点只多一行、一处都不能漏判。
func TestKbRelease_WhereVisible(t *testing.T) {
	if got := WhereVisible(nil); got != nil {
		t.Errorf("nil 链应回 nil，实得 %v", got)
	}
	t.Setenv(FlagEnv, "shadow")
	base := newDryRunDB(t).Model(&model.KnowledgeChunk{}).Where("product_id = ?", "p1")
	if got := WhereVisible(base); got != base {
		t.Error("闸门未开时 WhereVisible 必须原样返回同一条链（不新增任何条件）")
	}

	t.Setenv(FlagEnv, "on")
	sql := newDryRunDB(t).ToSQL(func(tx *gorm.DB) *gorm.DB {
		return WhereVisible(tx.Model(&model.KnowledgeChunk{}).Where("product_id = ?", "p1")).
			Find(&[]model.KnowledgeChunk{})
	})
	if !strings.Contains(sql, "NOT EXISTS") {
		t.Errorf("闸门开着时 SQL 里必须出现可见性判据：%s", sql)
	}
	if !strings.Contains(sql, "product_id") {
		t.Errorf("挂条件不该把原有 WHERE 挤掉：%s", sql)
	}
}

// TestKbRelease_FastPathsDoNoWork 无句柄 / 无归属 / 空集合三类入参必须**零查询**返回。
//
// 三处快路径各自的坏法不同：
//   - EnsureDraftStamp(nil) 回错误 ⇒ 一个没接库的进程里所有导入失败（应该是"不受管"）；
//   - productID 为空回非 0 ⇒ 跨库全量重建会把内容打进某个具体库的待发布桶；
//   - DirectWriteBlocked 在 off 档还去查库 ⇒ 每一次分段编辑多一次点查，且出闸门没开时
//     也可能因读失败而拒掉一次正常编辑（判据顺序见函数体）。
func TestKbRelease_FastPathsDoNoWork(t *testing.T) {
	t.Setenv(FlagEnv, "on")
	ctx := context.Background()

	if v, err := EnsureDraftStamp(ctx, nil, "p1"); v != 0 || err != nil {
		t.Errorf("EnsureDraftStamp(nil)=%d/%v，期望 0/无错", v, err)
	}
	if v, err := EnsureDraftStamp(ctx, newDryRunDB(t), ""); v != 0 || err != nil {
		t.Errorf("无归属时应回 0（0 = 不受闸门管），实得 %d/%v", v, err)
	}

	if b, err := DirectWriteBlocked(ctx, nil, "p1"); b || err != nil {
		t.Errorf("DirectWriteBlocked(nil)=%t/%v，期望 false/无错", b, err)
	}
	if b, err := DirectWriteBlocked(ctx, newDryRunDB(t), ""); b || err != nil {
		t.Errorf("无归属时不该拒（也查不到归属），实得 %t/%v", b, err)
	}

	n, err := CountWouldHide(ctx, nil, []uint64{1, 2})
	if n != 0 || err != nil {
		t.Errorf("CountWouldHide(nil)=%d/%v", n, err)
	}
	n, err = CountWouldHide(ctx, newDryRunDB(t), nil)
	if n != 0 || err != nil {
		t.Errorf("空集合应直接回 0，实得 %d/%v", n, err)
	}
}

// TestKbRelease_DirectWriteBlockedOffEvenWithRow 旗子 off 时**一次查询都不发**：
// 用 dry-run 句柄都能看出判据顺序（GateOn 在 db 之前短路）。真库上"有 governed 行但旗子 off"
// 的放行面由 repository 层的带库用例覆盖。
func TestKbRelease_DirectWriteBlockedOffEvenWithRow(t *testing.T) {
	for _, raw := range []string{"", "off", "shadow"} {
		t.Setenv(FlagEnv, raw)
		b, err := DirectWriteBlocked(context.Background(), newDryRunDB(t), "p1")
		if b || err != nil {
			t.Errorf("%s=%q 时不该拒也不该报错，实得 %t/%v", FlagEnv, raw, b, err)
		}
	}
}

// TestKbRelease_StampForWriteOffLeavesRowsAlone off 档一行都不碰（含"已经把版本清零"这种坏法）。
//
// 断言里保留一条 kb_version=3 的存量行是关键：off 档若把 0 写回去，一个没开闸的库会被
// 就地取消版本管控，而它下一次开闸时这些行的可见性就变了。
func TestKbRelease_StampForWriteOffLeavesRowsAlone(t *testing.T) {
	t.Setenv(FlagEnv, "off")
	chunks := []model.KnowledgeChunk{
		{ID: 1, ProductID: "p1", KBVersion: 0},
		{ID: 2, ProductID: "p1", KBVersion: 3, RetiredVersion: 7},
		{ID: 3, ProductID: "", KBVersion: 5},
	}
	if err := StampForWrite(context.Background(), newDryRunDB(t), chunks); err != nil {
		t.Fatalf("off 档打戳不该报错: %v", err)
	}
	if chunks[1].KBVersion != 3 || chunks[1].RetiredVersion != 7 {
		t.Errorf("off 档不该改任何版本列，实得 v=%d retired=%d", chunks[1].KBVersion, chunks[1].RetiredVersion)
	}
	if chunks[2].KBVersion != 5 {
		t.Error("无归属的行也不该被改")
	}
	if err := StampOneForWrite(context.Background(), newDryRunDB(t), nil); err != nil {
		t.Errorf("nil 指针应直接返回，实得 %v", err)
	}
}

// TestKbRelease_LogWouldHideNeverBreaksSearch 观察通路的失败只出声，绝不让一次检索变红。
//
// 这里喂一个连不上库的 dry-run 句柄并把档位设为 shadow：CountWouldHide 必然失败，
// 而本函数没有 error 返回 —— 它必须咽下去。反向测（把它改成 panic 或上抛）由
// 本用例的 t.Fatal 语义承担：任何让检索失败的改动都会在这里红。
func TestKbRelease_LogWouldHideNeverBreaksSearch(t *testing.T) {
	t.Setenv(FlagEnv, "shadow")
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("LogWouldHide 不该 panic（观察期不能弄红一次检索）: %v", r)
		}
	}()
	LogWouldHide(context.Background(), newDryRunDB(t), []uint64{1, 2, 3}, "p1", "unit-test")
	LogWouldHide(context.Background(), newDryRunDB(t), nil, "p1", "unit-test")
}

// TestKbRelease_LogWouldHideNeedsRealRowsForZeroCount 是本文件"不碰连接"这条边界的登记，
// 不是一条行为断言：`LogWouldHide` 的读数 0 那一格（`if n > 0`）在这里**量不到** ——
// dry-run 句柄的 Scan 直接失败（"dry run mode unsupported"），函数走的是失败出口，
// 把断言写在这里只会得到一句假话："0 条不出声"其实从没被执行过。
//
// 因此那一格的反向测试落在带库的 RagSearcher 观察用例里
// （internal/aiagent/knowledge/service 的 TestKbGate_ShadowSilentWhenNothingWouldHide，
// 用真库真行喂 rankRAGChunks ⇒ CountWouldHide 真的算出 0）。这里只把前置钉成一条断言：
// dry-run 句柄必须**报错**，否则说明上面那条用例的位置选错了。
func TestKbRelease_LogWouldHideNeedsRealRowsForZeroCount(t *testing.T) {
	if n, err := CountWouldHide(context.Background(), newDryRunDB(t), []uint64{1, 2, 3}); err == nil {
		t.Errorf("dry-run 句柄本该让统计失败（能算出 n=0 且无错就说明这格该挪回本文件测），实得 n=%d err=%v", n, err)
	}
}

// TestKbRelease_ErrGovernedDirectWriteIsStandalone 哨兵错误必须能 errors.Is 认出，
// 且文案自带下一步动作（控制层把它翻成 409，运营照着这句话去提变更）。
func TestKbRelease_ErrGovernedDirectWriteIsStandalone(t *testing.T) {
	msg := ErrGovernedDirectWrite.Error()
	for _, frag := range []string{"发布制", "变更流程", "审批"} {
		if !strings.Contains(msg, frag) {
			t.Errorf("拒绝文案缺少 %q：%s", frag, msg)
		}
	}
}

// TestKbRelease_UnknownValueWarns 认不出的档位值必须**出声**，可识别的值必须不出声。
//
// parseMode 对 "on shadow"/"banana" 这类拼错的值都按 off 处理，档位本身分不出好坏；
// 唯一的区别是这一句告警。它不在的话，运营把旗子写成 `enable` 之后看到的是"闸门没生效"，
// 而排查方向会是"库里哪一列没配"——正是那句告警要省掉的弯路。
// 反向那一格（可识别值不许出声）守着的是另一半坏法：每次解析都告警，等于没有告警。
//
// 断言按**整行**匹配并数行数：口径同仓库层那条批量删除告警的用例，
// 整段 Contains 会把"这条说了两件事"和"两条各说了一件事"拼成一条绿。
func TestKbRelease_UnknownValueWarns(t *testing.T) {
	const warnFrag = "无法识别 ⇒ 按 off 处理"

	for _, tc := range []struct {
		name     string
		raw      string
		wantHits int
	}{
		{"拼错的值出声", "banana", 1},
		{"两个合法值粘在一起也出声", "on shadow", 1},
		{"off 不出声", "off", 0},
		{"shadow 不出声", "shadow", 0},
		{"on 不出声", "on", 0},
		{"布尔真值不出声（它有自己的档位，不是错值）", "true", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(FlagEnv, tc.raw)
			logged := kbReleaseCaptureLogs(t, func() {
				if got := modeValue(); got != modeOff && tc.wantHits == 1 {
					t.Errorf("拼错的值 %q 应保守回 off，实得 %q", tc.raw, got)
				}
			})
			var hits int
			for _, line := range strings.Split(logged, "\n") {
				if strings.Contains(line, warnFrag) {
					hits++
				}
			}
			if hits != tc.wantHits {
				t.Errorf("告警行数 %d，期望 %d；捕获内容：\n%s", hits, tc.wantHits, logged)
			}
			if tc.wantHits == 1 && !strings.Contains(logged, `FF_LTC_KB_CHANGE_GATE=`) {
				t.Errorf("告警没点名是哪把旗子：%s", logged)
			}
		})
	}
}

func kbReleaseCaptureLogs(t *testing.T, fn func()) string {
	t.Helper()
	oldOut := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("建管道失败：%v", err)
	}
	os.Stdout = w
	logger.InitLogger(logger.LoggingConfig{Level: "info", Format: "json", Output: "stdout"})
	defer func() {
		os.Stdout = oldOut
		logger.InitLogger(logger.DefaultConfig())
	}()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("关闭写端失败：%v", err)
	}
	os.Stdout = oldOut
	captured, readErr := io.ReadAll(r)
	if readErr != nil {
		t.Fatalf("读取日志失败：%v", readErr)
	}
	return string(captured)
}
