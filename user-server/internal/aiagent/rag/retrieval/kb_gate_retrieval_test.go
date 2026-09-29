// kb_gate_retrieval_test.go T-P9-02 读闸门在**真实召回 SQL** 上的行为（本包 11 个拼接点）。
//
// 为什么必须有真库这一格：kbrelease 那一层的用例量的是"片段长什么样、什么时候为空"，
// 而本包这 11 处 `sql += kbrelease.AndVisible()` 的失效面全在**拼接点本身** ——
// 少拼一次、拼在 LIMIT 之后、拼错分支（只在 product 过滤那一路挂上），
// 三种写法在片段单测里都是绿的，在线上却是"未发布的内容当场可检"（AC① 的反例）。
// 所以这里逐个调用被改过的那个方法，断言 on 档看不见的行**真的**看不见。
//
// 三档的断言强度不一样，这是刻意分开的：
//   - off ⇒ 一条都不许隐藏（今天的线上行为，也是回滚口径"出事关旗子即刻回到今天"）；
//   - on  ⇒ 只隐藏"所属库已进发布制、且版本号在生效版之后 / 已被 retire"的行；
//   - shadow ⇒ 结果集与 off **逐条相同**（观察期若有行为差别，它就不再是观察期）。
//
// 两条恒等式（未 governance 的库、kb_version=0 的存量行）也在表里，
// 因为它们是"这套闸门不伤存量"的全部依据，一旦片段里的比较方向被改反，
// 最先红的应该是这两条而不是那条 pending 行。
package ragretrieval

import (
	"context"
	"sort"
	"strconv"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/kbrelease"

	"gorm.io/gorm"
)

const (
	kbGateGov    = "kbgt_gov"   // 进发布制：effective_version=1
	kbGateUngov  = "kbgt_ungov" // 有 release 行但 governed=false
	kbGateNoRel  = "kbgt_none"  // 完全没有 release 行
	kbGateTerm   = "kbgateterm" // 六个分段的公共词，保证词法四路都命中
	kbGateEffVer = 1
)

// kbGateRow 一行分段：doc 同时充当断言用的身份（chunk 主键是自增的，跨用例不稳）。
type kbGateRow struct {
	doc     int
	product string
	version int
	retired int
}

// kbGateRows 六行覆盖可见性判据的全部分支。
//
// 901/902 在 on 档必须仍然可见（前者是"升级前的存量行"，后者是"正在服的那一版"）；
// 903 是"已写入未发布"，904 是"已被下一版 retire"，这两条才是闸门该挡的；
// 905/906 版本号同样超前，但所属库不受管 ⇒ 一条都不该被挡。
func kbGateRows() []kbGateRow {
	return []kbGateRow{
		{doc: 901, product: kbGateGov, version: 0},
		{doc: 902, product: kbGateGov, version: kbGateEffVer},
		{doc: 903, product: kbGateGov, version: kbGateEffVer + 1},
		{doc: 904, product: kbGateGov, version: kbGateEffVer, retired: kbGateEffVer},
		{doc: 905, product: kbGateUngov, version: 99},
		{doc: 906, product: kbGateNoRel, version: 99},
	}
}

var (
	// kbGateAllGov 受管库里全部四行（off / shadow 档该看到的）
	kbGateAllGov = []int{901, 902, 903, 904}
	// kbGateOnGov on 档受管库里该剩下的
	kbGateOnGov = []int{901, 902}
	// kbGateAllCorpus 全库（product 过滤为空那一支）off 档该看到的
	kbGateAllCorpus = []int{901, 902, 903, 904, 905, 906}
	// kbGateOnCorpus on 档全库该剩下的：未受管两库的超前版本号一条都不许被挡
	kbGateOnCorpus = []int{901, 902, 905, 906}
)

// kbGateSetup 建库（含 knowledge_chunks 的原始 DDL 与 kb_releases）并灌入六行。
//
// kb_releases 用 AutoMigrate 而不是手写 DDL：这段夹具要的是"有一个 product_id 唯一索引
// 的真表"，而它的列集由 model.KBRelease 自己给出，手写一份就等于在本卡已有的
// "测试 DDL 与生产漂移"这条债上再叠一条同形状的。
func kbGateSetup(t *testing.T) *gorm.DB {
	t.Helper()
	db := setupHybridTestDB(t)

	if err := db.Exec(`DROP TABLE IF EXISTS kb_releases`).Error; err != nil {
		t.Fatalf("清理 kb_releases 失败：%v", err)
	}
	if err := db.AutoMigrate(&model.KBRelease{}); err != nil {
		t.Fatalf("建 kb_releases 失败：%v", err)
	}
	if err := db.Exec(`INSERT INTO kb_releases (product_id, governed, effective_version, changed_by) VALUES (?, true, ?, 'tester')`,
		kbGateGov, kbGateEffVer).Error; err != nil {
		t.Fatalf("灌入受管 release 行失败：%v", err)
	}
	if err := db.Exec(`INSERT INTO kb_releases (product_id, governed, effective_version, changed_by) VALUES (?, false, 7, 'tester')`,
		kbGateUngov).Error; err != nil {
		t.Fatalf("灌入未受管 release 行失败：%v", err)
	}
	// 未受管那一行的 effective_version 故意给 7（大于分段上的版本号）：
	// 若可见性判据里"先看 governed"这一眼被去掉，这库就会被按 v7 判，905 立刻消失。

	vec := vecToPGString(makeFixedVector(1024, 1.0))
	for _, r := range kbGateRows() {
		if err := db.Exec(`
			INSERT INTO knowledge_chunks (document_id, product_id, content, embedding, embed_status, content_tsv, kb_version, retired_version, change_id)
			VALUES ($1, $2, $3, $4::vector, 'indexed', to_tsvector('simple', $3), $5, $6, '')
		`, r.doc, r.product, kbGateTerm+" 分段 "+strconv.Itoa(r.doc), vec, r.version, r.retired).Error; err != nil {
			t.Fatalf("灌入分段 %d 失败：%v", r.doc, err)
		}
	}
	return db
}

// kbGateDocs 把召回结果折成排序后的 document_id 集合（比对用，顺序无关）。
func kbGateDocs(t *testing.T, chunks []Chunk) []int {
	t.Helper()
	out := make([]int, 0, len(chunks))
	for _, c := range chunks {
		n, err := strconv.Atoi(c.DocumentID)
		if err != nil {
			t.Fatalf("召回结果的 document_id 不是数字：%q", c.DocumentID)
		}
		out = append(out, n)
	}
	sort.Ints(out)
	return out
}

func kbGateEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// kbGateSite 一个被闸门改过的读路径入口：给定 db 与 product 过滤，返回召回结果。
type kbGateSite struct {
	name     string
	governed func(t *testing.T, db *gorm.DB, productID string) []Chunk
	// wantEmptyBranch 该入口是否有"product 过滤为空"那一支（全库召回）。
	// 有三支站点同时量两个分支，因为闸门片段在源码里是**两处**拼接
	// （`if productID != ""` 的两个分支各拼一次），少改一支的失效是"带库过滤时挡住、
	// 跨库全量检索时挡不住"，只在带过滤那一路断言量不到。
	wantEmptyBranch bool
}

func kbGateHybrid(t *testing.T, db *gorm.DB) *HybridSearcher {
	t.Helper()
	return NewHybridSearcher(db, &mockEmbeddingService{
		vectors: [][]float32{makeFixedVector(1024, 1.0)},
	}, nil, nil, nil, &HybridSearcherConfig{
		DefaultTopK: 50, CandidatePool: 50, FusedTopN: 50, RRFK: 60, EfSearch: 80,
	})
}

// TestKbGate_RetrievalSitesHideUnpublished 本包 11 个拼接点逐点验证。
func TestKbGate_RetrievalSitesHideUnpublished(t *testing.T) {
	sites := []kbGateSite{
		{
			name: "hybrid.vectorSearchPG",
			governed: func(t *testing.T, db *gorm.DB, pid string) []Chunk {
				h := kbGateHybrid(t, db)
				rows, err := h.vectorSearchPG(context.Background(), pid, makeFixedVector(1024, 1.0), 50)
				if err != nil {
					t.Fatalf("vectorSearchPG 失败：%v", err)
				}
				return rows
			},
			wantEmptyBranch: true,
		},
		{
			name: "hybrid.tsvectorSearch",
			governed: func(t *testing.T, db *gorm.DB, pid string) []Chunk {
				h := kbGateHybrid(t, db)
				rows, err := h.tsvectorSearch(context.Background(), "simple", "content_tsv", pid, kbGateTerm, 50)
				if err != nil {
					t.Fatalf("tsvectorSearch 失败：%v", err)
				}
				return rows
			},
			wantEmptyBranch: true,
		},
		{
			name: "hybrid.keywordSearchPGFallback",
			governed: func(t *testing.T, db *gorm.DB, pid string) []Chunk {
				h := kbGateHybrid(t, db)
				rows, err := h.keywordSearchPGFallback(context.Background(), pid, kbGateTerm, 50)
				if err != nil {
					t.Fatalf("keywordSearchPGFallback 失败：%v", err)
				}
				return rows
			},
			wantEmptyBranch: true,
		},
		{
			name: "lexical.tryTSQuery",
			governed: func(t *testing.T, db *gorm.DB, pid string) []Chunk {
				r := NewLexicalRetriever(db)
				rows, err := r.tryTSQuery(context.Background(), pid, kbGateTerm, 50, "content_tsv", "simple")
				if err != nil {
					t.Fatalf("tryTSQuery 失败：%v", err)
				}
				return rows
			},
			wantEmptyBranch: true,
		},
		{
			name: "lexical.ilikeFallback",
			governed: func(t *testing.T, db *gorm.DB, pid string) []Chunk {
				r := NewLexicalRetriever(db)
				rows, err := r.ilikeFallback(context.Background(), pid, kbGateTerm, 50)
				if err != nil {
					t.Fatalf("ilikeFallback 失败：%v", err)
				}
				return rows
			},
			wantEmptyBranch: true,
		},
		{
			name: "vector.SearchVector",
			governed: func(t *testing.T, db *gorm.DB, pid string) []Chunk {
				v := NewVectorRetriever(db, nil, 80)
				rows, err := v.SearchVector(context.Background(), pid, makeFixedVector(1024, 1.0), 50)
				if err != nil {
					t.Fatalf("SearchVector 失败：%v", err)
				}
				return rows
			},
			wantEmptyBranch: true,
		},
		{
			name: "vector.Retrieve",
			governed: func(t *testing.T, db *gorm.DB, pid string) []Chunk {
				v := NewVectorRetriever(db, &mockEmbeddingService{
					vectors: [][]float32{makeFixedVector(1024, 1.0)},
				}, 80)
				rows, err := v.Retrieve(context.Background(), pid, kbGateTerm, 50)
				if err != nil {
					t.Fatalf("Retrieve 失败：%v", err)
				}
				return rows
			},
			wantEmptyBranch: true,
		},
	}

	// 正向控制：如果夹具本身没造出"闸门该挡的行"和"闸门不该挡的超前行"，上面每一格
	// 都会以误判的姿态绿着 —— 前者退化成"on 与 off 相同所以拼接点没问题"，
	// 后者退化成"未受管那两库其实没有可被误挡的行"。两个计数各判一件事，都不许靠推断。
	db0 := kbGateSetup(t)
	var shouldHide, shouldNotHide int
	if err := db0.Raw(`SELECT COUNT(*) FROM knowledge_chunks
		WHERE product_id = $1 AND (kb_version > $2 OR retired_version > 0)`,
		kbGateGov, kbGateEffVer).Scan(&shouldHide).Error; err != nil {
		t.Fatalf("统计受管库里该被挡的行失败：%v", err)
	}
	if shouldHide != 2 {
		t.Fatalf("受管库里该有 2 行落在「版本超前 / 已被 retire」，实际 %d —— 隐藏判据没有对象可量", shouldHide)
	}
	if err := db0.Raw(`SELECT COUNT(*) FROM knowledge_chunks
		WHERE product_id <> $1 AND kb_version > $2`,
		kbGateGov, kbGateEffVer).Scan(&shouldNotHide).Error; err != nil {
		t.Fatalf("统计未受管库里版本号超前的行失败：%v", err)
	}
	if shouldNotHide != 2 {
		t.Fatalf("未受管的两库里该有 2 行版本号超前（用于证明「不该挡」不是空集），实际 %d", shouldNotHide)
	}

	for _, site := range sites {
		for _, branch := range []struct{ label, productID string }{
			{"按库过滤", kbGateGov},
			{"跨库全量", ""},
		} {
			if branch.productID == "" && !site.wantEmptyBranch {
				continue
			}
			name := site.name + "/" + branch.label
			wantOff, wantOn := kbGateAllGov, kbGateOnGov
			if branch.productID == "" {
				wantOff, wantOn = kbGateAllCorpus, kbGateOnCorpus
			}

			// 三档各自建一次库：旗子是每次调用读 env 的（同 kbrelease 那一份用例的口径），
			// 但**结果集**是这里的主张，跨档复用同一份数据只是为了让"少一条"这种读数
			// 一定来自闸门、而不是来自上一档的清理没做干净。
			for _, tc := range []struct {
				mode string
				want []int
			}{
				{"off", wantOff},
				{"shadow", wantOff}, // 观察档必须与 off 逐条相同
				{"on", wantOn},
			} {
				t.Run(name+"/"+tc.mode, func(t *testing.T) {
					t.Setenv(kbrelease.FlagEnv, tc.mode)
					db := kbGateSetup(t)
					got := kbGateDocs(t, site.governed(t, db, branch.productID))
					if !kbGateEqual(got, tc.want) {
						t.Errorf("%s 在 %s=%s 档召回 %v，期望 %v", name, kbrelease.FlagEnv, tc.mode, got, tc.want)
					}
				})
			}
		}
	}
}

// TestKbGate_EmbeddingSourceFilterStillApplies 闸门片段是**追加**在既有 WHERE 之后的，
// 它不该把原本的过滤条件吃掉。反向控制：把 embedding_source 改成非 'tei' 后
// 向量那两路必须一条都不返回 —— 若某天闸门拼接覆盖掉了前一个条件，这条会先红。
func TestKbGate_EmbeddingSourceFilterStillApplies(t *testing.T) {
	t.Setenv(kbrelease.FlagEnv, "off")
	db := kbGateSetup(t)
	if err := db.Exec(`UPDATE knowledge_chunks SET embedding_source = 'hash'`).Error; err != nil {
		t.Fatalf("改 embedding_source 失败：%v", err)
	}
	v := NewVectorRetriever(db, nil, 80)
	rows, err := v.SearchVector(context.Background(), kbGateGov, makeFixedVector(1024, 1.0), 50)
	if err != nil {
		t.Fatalf("SearchVector 失败：%v", err)
	}
	if len(rows) != 0 {
		t.Errorf("embedding_source='hash' 时向量召回应为空，实际 %v", kbGateDocs(t, rows))
	}
}

// TestKbGate_TestSchemaCoversModelColumns 本包那份手写 DDL 的常驻覆盖闸。
//
// 存在理由（同一文件里已经写了三次"请同步本 DDL"，还是漂了两回）：这里不能用
// AutoMigrate 建 knowledge_chunks（pgvector 与三个 tsvector 列不在 gorm 模型里），
// 于是列集由人手抄写。缺列的失效分两类，两类都会伪装成"测试通过"：
//   - 检索 SQL 少列 ⇒ 报 42703，被上层降级成"空结果 + WARN"，用例红成 `expected at least 1 result`；
//   - 而**只有开了旗子才引用到的列**（kb_version）缺失时，本包既有全部用例都在 off 档跑，
//     一条都不红 ⇒ 闸门上线那一刻才在真库上炸。
//
// 判据取"模型的列集 ⊆ 实表的列集"而不是逐列写死：加列的人只需改模型，
// 忘改 DDL 由这里拦；表里多出模型没有的列（tsvector/embedding）不算漂移。
func TestKbGate_TestSchemaCoversModelColumns(t *testing.T) {
	db := kbGateSetup(t)

	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(&model.KnowledgeChunk{}); err != nil {
		t.Fatalf("解析 KnowledgeChunk 失败：%v", err)
	}
	if stmt.Schema.Table != "knowledge_chunks" {
		t.Fatalf("模型表名不是 knowledge_chunks：%s（本用例的判据按这张表写死）", stmt.Schema.Table)
	}
	var have []string
	if err := db.Raw(`SELECT column_name FROM information_schema.columns WHERE table_name = 'knowledge_chunks'`).
		Scan(&have).Error; err != nil {
		t.Fatalf("读取实表列集失败：%v", err)
	}
	set := make(map[string]bool, len(have))
	for _, c := range have {
		set[c] = true
	}
	// 控制组：实表必然有 content 这一列。读不到列集（空集）时不能把"零缺失"当成通过。
	if !set["content"] {
		t.Fatalf("实表列集里没有 content（读到 %d 列）—— 本用例的判据没有对象可量", len(have))
	}

	var missing []string
	for _, col := range stmt.Schema.DBNames {
		if !set[col] {
			missing = append(missing, col)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("手写 DDL 缺模型列 %v —— 补进 setupHybridTestDB 的 CREATE TABLE（默认值与 model 的 gorm tag 一致）", missing)
	}
}
