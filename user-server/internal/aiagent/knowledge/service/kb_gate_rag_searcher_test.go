// kb_gate_rag_searcher_test.go T-P9-02 读闸门在 RagSearcher 那 4 个召回拼接点上的行为。
//
// 与 rag/retrieval 那一份同一条主张，但那份量的是"检索器自己拼的裸 SQL"，这一份量的是
// **GORM 链式**那两个点（`kbrelease.WhereVisible(...)`）与向量那一路的两个分支：
//   - WhereVisible 的失效方向不是"少一层过滤"而是"查询报错"（把 `Where("")` 直接挂上去
//     在不同 GORM 版本表现不一致），所以 off 档必须真的能跑通并返回全量；
//   - 向量那两处是字符串拼接，`+ kbrelease.AndVisible() +` 落在反引号块里，
//     少拼一处不会报错、只会多放一条 —— 只能靠"on 档看不见了"这一条断言拦住。
//
// 向量来源用桩而不是真 TEI：这一格断言的是可见性，向量只要维度合法即可；
// 把用例挂在外部模型服务上，CI 里一次兜底向量切换就会把"闸门挡住没有"这件事
// 变成"TEI 今天可达吗"（同 rag_searcher_test.go 里 semanticVectors 那条区分）。
package service

import (
	"context"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/kbrelease"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/pkg/testutil/testmigrate"
	"hivemtk-user/internal/pkg/utils/logger"
)

const (
	kbSvcGov    = "kbsvc_gov"   // 进发布制，effective_version=1
	kbSvcUngov  = "kbsvc_ungov" // 有 release 行但不受管
	kbSvcNoRel  = "kbsvc_none"  // 没有 release 行
	kbSvcTerm   = "kbgatesvc"   // 六行公共词，bm25 与 ILIKE 两路都命中
	kbSvcEffVer = 1
)

// kbSvcFakeEmb 固定向量桩：维度与检索 SQL 里的 vector(1024) 对齐。
type kbSvcFakeEmb struct{}

func (kbSvcFakeEmb) Embed(_ context.Context, _ *llm.EmbeddingConfig, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = kbSvcUnitVec()
	}
	return out, nil
}

func (k kbSvcFakeEmb) EmbedOne(ctx context.Context, cfg *llm.EmbeddingConfig, _ string) ([]float32, error) {
	vecs, err := k.Embed(ctx, cfg, []string{""})
	if err != nil {
		return nil, err
	}
	return vecs[0], nil
}

func (kbSvcFakeEmb) DefaultConfig() *llm.EmbeddingConfig {
	return &llm.EmbeddingConfig{Model: "fake-kb-gate", Dimension: 1024}
}

func kbSvcUnitVec() []float32 {
	v := make([]float32, 1024)
	for i := range v {
		v[i] = 1.0
	}
	return v
}

// kbSvcSetup 建表（含 embedding 列与 kb_releases）并灌入六行分段。
//
// 与 rag/retrieval 那份夹具的差别：这里可以走 AutoMigrate（模型带 kb_version 三列），
// 所以不存在"手写 DDL 落后于模型"的那条债 —— 那份债由那边的覆盖闸拦。
func kbSvcSetup(t *testing.T) *gorm.DB {
	t.Helper()
	database := testutil.NewTestDB(t, &model.KnowledgeChunk{}, &model.KnowledgeDocument{}, &model.KBRelease{})
	testmigrate.RunTestMigrations(t, database)

	if err := database.Exec(`DELETE FROM kb_releases WHERE product_id LIKE 'kbsvc_%'`).Error; err != nil {
		t.Fatalf("清理 kb_releases 失败：%v", err)
	}
	rel := func(pid string, governed bool, eff int) {
		if err := database.Exec(`INSERT INTO kb_releases (product_id, governed, effective_version, changed_by) VALUES (?, ?, ?, 'tester')`,
			pid, governed, eff).Error; err != nil {
			t.Fatalf("灌入 release 行 %s 失败：%v", pid, err)
		}
	}
	rel(kbSvcGov, true, kbSvcEffVer)
	// 未受管那行的 effective_version 故意给 99（比分段版本号还大）：可见性判据里
	// "先问 governed"这一眼被去掉时，这库的行会当场消失，本用例立刻红。
	rel(kbSvcUngov, false, 99)

	type row struct {
		doc     int
		product string
		version int
		retired int
	}
	rows := []row{
		{doc: 911, product: kbSvcGov, version: 0},
		{doc: 912, product: kbSvcGov, version: kbSvcEffVer},
		{doc: 913, product: kbSvcGov, version: kbSvcEffVer + 1},
		{doc: 914, product: kbSvcGov, version: kbSvcEffVer, retired: kbSvcEffVer},
		{doc: 915, product: kbSvcUngov, version: 99},
		{doc: 916, product: kbSvcNoRel, version: 99},
	}
	for _, r := range rows {
		ch := model.KnowledgeChunk{
			DocumentID: uint64(r.doc), ProductID: r.product, ChunkIndex: 0,
			Content: kbSvcTerm + " 分段 " + strconv.Itoa(r.doc),
			// EmbeddingSource 显式给 'tei'：向量那两路的 SQL 自带 `embedding_source = 'tei'`，
			// 靠 gorm 的列默认值填这一格会把"闸门挡住没有"这件事挂在默认值行为上。
			KBVersion: r.version, RetiredVersion: r.retired, EmbeddingSource: "tei",
		}
		if err := database.Create(&ch).Error; err != nil {
			t.Fatalf("灌入分段 %d 失败：%v", r.doc, err)
		}
		if err := database.Exec(`UPDATE knowledge_chunks SET embedding = ?::vector WHERE id = ?`,
			toPgVector(kbSvcUnitVec()), ch.ID).Error; err != nil {
			t.Fatalf("补向量失败（分段 %d）：%v", r.doc, err)
		}
	}
	return database
}

func kbSvcFromRAG(t *testing.T, rows []RAGChunk) []int {
	t.Helper()
	out := make([]int, 0, len(rows))
	for _, r := range rows {
		n, err := strconv.Atoi(r.DocID)
		if err != nil {
			t.Fatalf("召回结果的 DocID 不是数字：%q", r.DocID)
		}
		out = append(out, n)
	}
	return out
}

// kbSvcDocs 折成升序集合：召回结果本身按分数排，可见性断言只关心"哪些行进来了"。
func kbSvcDocs(ids []int) []int {
	out := append([]int(nil), ids...)
	sort.Ints(out)
	return out
}

func kbSvcFromMerchant(rows []MerchantRAGChunk) []int {
	out := make([]int, 0, len(rows))
	for _, r := range rows {
		out = append(out, int(r.DocumentID))
	}
	return out
}

func kbSvcFromScored(pairs []scored) []int {
	out := make([]int, 0, len(pairs))
	for _, p := range pairs {
		out = append(out, int(p.row.DocumentID))
	}
	return out
}

// 四个站点在两档下的期望集合（off 与 shadow 共用前者：观察期不许改变结果）。
var (
	kbSvcOffGov    = []int{911, 912, 913, 914}
	kbSvcOnGov     = []int{911, 912}
	kbSvcOffCorpus = []int{911, 912, 913, 914, 915, 916}
	kbSvcOnCorpus  = []int{911, 912, 915, 916}
)

func TestKbGate_RagSearcherSitesHideUnpublished(t *testing.T) {
	// 正向控制（判据必须先有对象可量）：受管库里确有 2 行该被挡、未受管两库确有 2 行超前。
	db0 := kbSvcSetup(t)
	var shouldHide, shouldNot int
	if err := db0.Raw(`SELECT COUNT(*) FROM knowledge_chunks
		WHERE product_id = ? AND (kb_version > ? OR retired_version > 0)`, kbSvcGov, kbSvcEffVer).
		Scan(&shouldHide).Error; err != nil {
		t.Fatalf("统计该被挡的行失败：%v", err)
	}
	if shouldHide != 2 {
		t.Fatalf("受管库里该有 2 行该被闸门挡，实际 %d", shouldHide)
	}
	if err := db0.Raw(`SELECT COUNT(*) FROM knowledge_chunks
		WHERE product_id <> ? AND kb_version > ?`, kbSvcGov, kbSvcEffVer).
		Scan(&shouldNot).Error; err != nil {
		t.Fatalf("统计不该被挡的超前行失败：%v", err)
	}
	if shouldNot != 2 {
		t.Fatalf("未受管那两库里该有 2 行版本超前（证明「不该挡」不是空集），实际 %d", shouldNot)
	}

	sites := []struct {
		name    string
		branch  string
		call    func(t *testing.T, s *RagSearcher) []int
		off, on []int
	}{
		{"bm25SearchAll", "全库", func(t *testing.T, s *RagSearcher) []int {
			rows, err := s.bm25SearchAll(context.Background(), kbSvcTerm, 50)
			require.NoError(t, err)
			return kbSvcDocs(kbSvcFromRAG(t, rows))
		}, kbSvcOffCorpus, kbSvcOnCorpus},
		{"bm25SearchIndex", "按库过滤", func(t *testing.T, s *RagSearcher) []int {
			rows, err := s.bm25SearchIndex(context.Background(), kbSvcGov, kbSvcTerm, 50)
			require.NoError(t, err)
			return kbSvcDocs(kbSvcFromMerchant(rows))
		}, kbSvcOffGov, kbSvcOnGov},
		{"vectorSearch", "按库过滤", func(t *testing.T, s *RagSearcher) []int {
			pairs, err := s.vectorSearch(context.Background(), kbSvcGov, kbSvcTerm, 50)
			require.NoError(t, err)
			return kbSvcDocs(kbSvcFromScored(pairs))
		}, kbSvcOffGov, kbSvcOnGov},
		{"vectorSearch", "全库", func(t *testing.T, s *RagSearcher) []int {
			pairs, err := s.vectorSearch(context.Background(), "", kbSvcTerm, 50)
			require.NoError(t, err)
			return kbSvcDocs(kbSvcFromScored(pairs))
		}, kbSvcOffCorpus, kbSvcOnCorpus},
	}

	for _, tc := range []struct{ mode string }{{"off"}, {"shadow"}, {"on"}} {
		for _, site := range sites {
			t.Run(tc.mode+"/"+site.name+"/"+site.branch, func(t *testing.T) {
				t.Setenv(kbrelease.FlagEnv, tc.mode)
				want := site.off
				if tc.mode == "on" {
					want = site.on
				}
				database := kbSvcSetup(t)
				s := &RagSearcher{db: database, embeddingService: kbSvcFakeEmb{}}
				got := site.call(t, s)
				require.Equal(t, want, got, "%s 在 %s=%s 档的可见集合不对", site.name, kbrelease.FlagEnv, tc.mode)
			})
		}
	}
}

// TestKbGate_ShadowLogsWouldHide 观察档的"转 on 会被隐藏几条"这句必须真的出声。
//
// 这句是运营决定要不要把旗子推到 on 的**唯一**证据来源（影子期读不到线上召回集少了多少），
// 所以它不能只是"代码里有一行 logger.Infof"：这里抓真日志、按整行匹配并数行数，
// 同时钉住另外两档**不出声**（off 档零开销是那份文件里写明的口径）。
//
// 两个排序出口各点一次名（rankRAGChunks 走通用召回、rankMerchantChunks 走商家召回）：
// 那两句 `kbrelease.LogWouldHide(ctx, s.db, ids, "", "<label>")` 是**两处**并排的调用，
// 只测其中一处时删掉另一处没有任何用例会红，而失效形态正是"商家那一路的召回从来没有
// 影子读数"——运营据此判断开闸影响，读到的却是通用那一路的数字。
// 按 label 分桩数行，两路的读数不许互相顶替。
func TestKbGate_ShadowLogsWouldHide(t *testing.T) {
	sites := []struct {
		label string
		call  func(s *RagSearcher, pendingID uint64)
	}{
		{"rankRAGChunks", func(s *RagSearcher, id uint64) {
			s.rankRAGChunks(context.Background(), []RAGChunk{
				{DocID: "913", ChunkID: strconv.FormatUint(id, 10), Score: 1},
				{DocID: "911", ChunkID: "0", Score: 1},
			})
		}},
		{"rankMerchantChunks", func(s *RagSearcher, id uint64) {
			s.rankMerchantChunks(context.Background(), []MerchantRAGChunk{
				{ID: id, DocumentID: 913, Score: 1},
			})
		}},
	}

	type leg struct {
		mode     string
		wantLine bool
	}
	for _, l := range []leg{{"shadow", true}, {"off", false}, {"on", false}} {
		for _, site := range sites {
			t.Run(l.mode+"/"+site.label, func(t *testing.T) {
				t.Setenv(kbrelease.FlagEnv, l.mode)
				database := kbSvcSetup(t)
				s := &RagSearcher{db: database, embeddingService: kbSvcFakeEmb{}}

				// 取那条"已写入未发布"的 chunk id：闸门在 on 档该挡的就是它。
				var pendingID uint64
				if err := database.Raw(`SELECT id FROM knowledge_chunks WHERE document_id = 913`).
					Scan(&pendingID).Error; err != nil {
					t.Fatalf("取未发布分段 id 失败：%v", err)
				}
				if pendingID == 0 {
					t.Fatal("夹具里没有 document_id=913 的分段（前置不成立）")
				}

				logged := kbSvcCaptureLogs(t, func() { site.call(s, pendingID) })

				// 整行匹配：影子句的唯一稳定标识是那句消息开头的 label。
				var hits int
				for _, line := range strings.Split(logged, "\n") {
					if strings.Contains(line, `"message":"[kb-release] shadow: `+site.label) {
						hits++
					}
				}
				if l.wantLine {
					if hits != 1 {
						t.Errorf("%s 档该有 1 行 shadow 读数，实际 %d 行；捕获内容：\n%s", l.mode, hits, logged)
					}
					if !strings.Contains(logged, "有 1 条") {
						t.Errorf("shadow 读数该说\"有 1 条\"会被隐藏：%s", logged)
					}
					return
				}
				if hits != 0 {
					t.Errorf("%s 档不该出 shadow 声，实际 %d 行：\n%s", l.mode, hits, logged)
				}
			})
		}
	}
}

// TestKbGate_ShadowSilentWhenNothingWouldHide 观察档的读数为 0 时**不出声**。
//
// 守的是 kbrelease.LogWouldHide 里 `if n > 0` 那一格。它必须在带库的包度量：
// dry-run 句柄的 Scan 直接失败（"dry run mode unsupported"），函数从失败出口回去，
// 在叶子包里写这条断言等于断言一件从没被执行过的事（那边的登记见
// internal/pkg/kbrelease/kbrelease_test.go 的 LogWouldHideNeedsRealRowsForZeroCount）。
//
// 坏法很具体：读数 0 也打一行 ⇒ 影子期每次召回刷一行"有 0 条会被隐藏"，
// 运营据此判断开闸影响时读到的是噪声，而"没拦住东西"本来就是默认状态。
//
// 前置单独钉成 Fatal：真库里这次统计必须**真的算出 0 且无错**（否则本用例量的是
// 失败出口，`n > 0` 那格变异会假杀）。
func TestKbGate_ShadowSilentWhenNothingWouldHide(t *testing.T) {
	t.Setenv(kbrelease.FlagEnv, "shadow")
	database := kbSvcSetup(t)
	s := &RagSearcher{db: database, embeddingService: kbSvcFakeEmb{}}

	// 取在服那两行的 id（doc 911 版本 0、doc 912 版本=effective，都不该被闸门挡）。
	var ids []uint64
	if err := database.Raw(`SELECT id FROM knowledge_chunks WHERE document_id IN (911, 912) ORDER BY id`).
		Scan(&ids).Error; err != nil {
		t.Fatalf("取在服分段 id 失败：%v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("夹具该有 2 条在服分段，实得 %d（前置不成立）", len(ids))
	}
	n, err := kbrelease.CountWouldHide(context.Background(), database, ids)
	if err != nil || n != 0 {
		t.Fatalf("夹具前置不成立：这两条在服行该算出 (0, 无错)，实得 n=%d err=%v", n, err)
	}

	chunks := make([]RAGChunk, 0, len(ids))
	for _, id := range ids {
		chunks = append(chunks, RAGChunk{DocID: "912", ChunkID: strconv.FormatUint(id, 10), Score: 1})
	}
	logged := kbSvcCaptureLogs(t, func() { s.rankRAGChunks(context.Background(), chunks) })

	for _, frag := range []string{`"message":"[kb-release] shadow: rankRAGChunks`, "shadow 统计失败"} {
		if strings.Contains(logged, frag) {
			t.Errorf("读数为 0 却出现 %q：%s", frag, logged)
		}
	}

	// 同一条通路换上那条"已写入未发布"的行必须出声：证明上面那次"没出声"是判据在起作用，
	// 而不是 rankRAGChunks 根本没走到观察通路（夹具或调用形状漂了的情形）。
	var pendingID uint64
	if err := database.Raw(`SELECT id FROM knowledge_chunks WHERE document_id = 913`).Scan(&pendingID).Error; err != nil {
		t.Fatalf("取未发布分段 id 失败：%v", err)
	}
	if pendingID == 0 {
		t.Fatal("夹具里没有 document_id=913 的分段（前置不成立）")
	}
	logged = kbSvcCaptureLogs(t, func() {
		s.rankRAGChunks(context.Background(), []RAGChunk{{DocID: "913", ChunkID: strconv.FormatUint(pendingID, 10), Score: 1}})
	})
	if !strings.Contains(logged, `"message":"[kb-release] shadow: rankRAGChunks`) {
		t.Errorf("对照格：有 1 条会被隐藏时该出声，实得：%s", logged)
	}
}

// kbSvcCaptureLogs 抓一段闭包写出的日志。
//
// 走 os.Pipe + logger.InitLogger(json) 这一条既有通路（internal/app 的两处真抓日志用例同形）：
// json 档下消息是一个字段，"一次 Write = 一行"由 lineSafeWriter 保证，
// 于是整行匹配不会被正文里的换行劈开、也不会把两条不相干的行拼成一条。
func kbSvcCaptureLogs(t *testing.T, fn func()) string {
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
