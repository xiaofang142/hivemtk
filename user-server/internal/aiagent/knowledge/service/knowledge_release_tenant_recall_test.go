// knowledge_release_tenant_recall_test.go CS-03 身份到知识检索贯通——真库数据对照矩阵
// （真实 PG 8217 受控变量，testutil.NewTestDB；检索层直调，不经任何 UI/编排包装）。
//
// CS-01 分配缺口：CS-01 三样本的"知识命中"用的是 fakeRAGSearcher 测试工厂
// （cs01_douyin_samples_test.go newC01RAGHit），本文件把「真知识行发布后可被召回」
// 的关联在**真实检索路径**上钉住：
//
//	数据矩阵（同关键词"退款政策"五条对照行，product_id 全局唯一）：
//	  A1 租户 A 私有·已发布（kb_version == effective_version）   ⇒ A 检索命中，引用含真库 chunk ID/文档 ID/版本
//	  A2 租户 A 私有·未发布（kb_version >  effective_version）   ⇒ 不命中；发布（effective 指针前移）后命中
//	  A3 租户 A 私有·旧版已退役（retired_version ≤ effective）  ⇒ 不命中
//	  B1 租户 B 私有·同关键词不同答案                            ⇒ B 自己可召回；A 绝不命中（双向）
//	  B2 租户 B 公开知识（knowledge_documents.public_visible）   ⇒ A 不可召回（既有访问规则：
//	    检索路径没有任何跨租户公开豁免，public_visible 只作用于帮助中心读路——
//	    本用例按既有规则钉住，不新造"跨租户公开检索"能力，也不把跨租户一律禁掉：
//	    B 租户检索自己的私有知识照常可用，见 B1 正向格）
//
//	权限/发布过滤发生在实际检索路径的凭据：检索走 RagSearcher→KnowledgeChunkRepository
//	的 GORM 链（VectorSearchRows/BM25ScanRows），租户隔离由 tenancyidx callback 在该链上
//	注入 WHERE tenant_id（on 模式 fail-closed），发布闸由 kbrelease.WhereVisible 拼进同一条
//	SQL——B 行与 A 行同表共存而 A 的真检索看不见 B，即过滤在 SQL 路径而非读后筛选。
//
//	失败/边界（检索层）：embedding 服务错误 → 按既有配置降级 BM25-lite（降级路径同样
//	带租户隔离与发布闸，来源如实——返回的就是库里的行）；无候选 → 诚实零命中（err=nil、
//	0 条），不伪造成功。
package service

import (
	"context"
	"errors"
	"strconv"
	"testing"

	knowledgerepo "hivemtk-user/internal/aiagent/knowledge/repository"
	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/kbrelease"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/pkg/testutil/testmigrate"

	"gorm.io/gorm"
)

// CS-03 数据矩阵冻结字段（cs03- 前缀，与其他文件隔离；product_id 走 kb_releases
// 全局唯一索引，必须全库唯一）。
const (
	cs03TenantA = uint(601)
	cs03TenantB = uint(602)

	cs03ProdAPub    = "kbr03_a_pub"  // A 私有已发布库
	cs03ProdBPriv   = "kbr03_b_priv" // B 私有库（同关键词不同答案）
	cs03ProdBPublic = "kbr03_b_pub"  // B 公开知识库

	cs03EffVer = 1

	cs03QAPub     = "CS03A-MARK 退款政策：下单后7天内可无理由退款。"
	cs03QAPending = "CS03A-PENDING 退款政策（新版）：15天内可退款。"
	cs03QARetired = "CS03A-RETIRED 退款政策（旧版）：3天内可退款。"
	cs03QBPriv    = "CS03B-PRIV 退款政策：30天内仅支持换货不退款。"
	cs03QBPublic  = "CS03B-PUBLIC 退款政策：公开帮助中心退款条款。"

	cs03Query = "退款政策是怎么规定的"
	cs03Term  = "退款"
)

// cs03Emb 夹具查询向量桩：单位向量（与 kbSvcFakeEmb 同形，维度对齐 vector(1024)）。
// 只断可见性集合与引用定位，不依赖语义排序，外部模型不可达不影响红绿。
type cs03Emb struct{}

func (cs03Emb) Embed(_ context.Context, _ *llm.EmbeddingConfig, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = kbSvcUnitVec()
	}
	return out, nil
}

func (cs03Emb) EmbedOne(ctx context.Context, cfg *llm.EmbeddingConfig, text string) ([]float32, error) {
	vecs, err := cs03Emb{}.Embed(ctx, cfg, []string{text})
	if err != nil {
		return nil, err
	}
	return vecs[0], nil
}

func (cs03Emb) DefaultConfig() *llm.EmbeddingConfig {
	return &llm.EmbeddingConfig{Model: "fake-cs03", Dimension: 1024}
}

// cs03ErrEmb 恒错 embedding 桩：驱动向量检索失败 → BM25-lite 降级路径。
type cs03ErrEmb struct{ calls int }

func (e *cs03ErrEmb) Embed(_ context.Context, _ *llm.EmbeddingConfig, _ []string) ([][]float32, error) {
	e.calls++
	return nil, errors.New("cs03: embedding service unavailable")
}

func (e *cs03ErrEmb) EmbedOne(ctx context.Context, cfg *llm.EmbeddingConfig, text string) ([]float32, error) {
	e.calls++
	return nil, errors.New("cs03: embedding service unavailable")
}

func (e *cs03ErrEmb) DefaultConfig() *llm.EmbeddingConfig {
	return &llm.EmbeddingConfig{Model: "fake-cs03-err", Dimension: 1024}
}

// cs03Setup 建表并灌入数据矩阵（on 模式 + 各租户 ctx 写入，tenant_id 由隔离 callback 落章）。
func cs03Setup(t *testing.T) *gorm.DB {
	t.Helper()
	database := testutil.NewTestDB(t,
		&model.KnowledgeChunk{}, &model.KnowledgeDocument{}, &model.KBRelease{},
	)
	testmigrate.RunTestMigrations(t, database)

	prev := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(prev) })

	t.Setenv(kbrelease.FlagEnv, "on")

	ctxA := tenant.WithTenantID(context.Background(), cs03TenantA)
	ctxB := tenant.WithTenantID(context.Background(), cs03TenantB)

	// kb_releases：A/B 三库进发布制，effective=cs03EffVer。行随租户 ctx 落章。
	rel := func(ctx context.Context, pid string) {
		if err := database.WithContext(ctx).Create(&model.KBRelease{
			ProductID: pid, Governed: true, EffectiveVersion: cs03EffVer, ChangedBy: "cs03-tester",
		}).Error; err != nil {
			t.Fatalf("灌入 release 行 %s 失败：%v", pid, err)
		}
	}
	rel(ctxA, cs03ProdAPub)
	rel(ctxB, cs03ProdBPriv)
	rel(ctxB, cs03ProdBPublic)

	// chunk 矩阵：版本/退役三态 + 租户两侧。embedding 用 GORM Update 链补
	//（on 模式下裸 SQL 被隔离守卫拒绝，GORM API 是数据访问唯一形态）。
	type row struct {
		ctx     context.Context
		doc     int64
		product string
		content string
		version int
		retired int
	}
	rows := []row{
		{ctxA, 5311, cs03ProdAPub, cs03QAPub, cs03EffVer, 0},
		{ctxA, 5312, cs03ProdAPub, cs03QAPending, cs03EffVer + 1, 0},
		{ctxA, 5313, cs03ProdAPub, cs03QARetired, cs03EffVer, cs03EffVer},
		{ctxB, 5321, cs03ProdBPriv, cs03QBPriv, cs03EffVer, 0},
		{ctxB, 5322, cs03ProdBPublic, cs03QBPublic, cs03EffVer, 0},
	}
	for _, r := range rows {
		ch := model.KnowledgeChunk{
			DocumentID: uint64(r.doc), ProductID: r.product, ChunkIndex: 0,
			Content: r.content,
			// EmbeddingSource 显式 'tei'：向量路 SQL 自带 embedding_source='tei'，
			// 不把可见性结论挂在列默认值行为上（同 kbSvcSetup 口径）。
			KBVersion: r.version, RetiredVersion: r.retired, EmbeddingSource: "tei",
		}
		if err := database.WithContext(r.ctx).Create(&ch).Error; err != nil {
			t.Fatalf("灌入分段 doc=%d 失败：%v", r.doc, err)
		}
		if err := database.WithContext(r.ctx).Model(&model.KnowledgeChunk{}).
			Where("id = ?", ch.ID).
			Update("embedding", gorm.Expr("?::vector", toPgVector(kbSvcUnitVec()))).Error; err != nil {
			t.Fatalf("补向量失败（doc=%d）：%v", r.doc, err)
		}
	}

	// B 公开知识在 knowledge_documents 上有公开标记行（public_visible=true）——
	// 该标记只被帮助中心读路消费（repository/help_center_repo.go），
	// 检索路径没有任何跨租户公开豁免，这正是 B2 格要钉的既有规则。
	pubDoc := model.KnowledgeDocument{
		ProductID: cs03ProdBPublic, Title: "公开退款条款", SourceType: model.SourceTypeManual,
		PublicVisible: true,
	}
	if err := database.WithContext(ctxB).Create(&pubDoc).Error; err != nil {
		t.Fatalf("灌入 B 公开文档失败：%v", err)
	}
	return database
}

// cs03Searcher 真 RagSearcher（测试库仓储 + 桩向量，禁用 hybrid 走 legacy 直查）。
func cs03Searcher(t *testing.T, database *gorm.DB) *RagSearcher {
	t.Helper()
	s := NewRagSearcherWithRepo(knowledgerepo.NewKnowledgeChunkRepository(database))
	s.SetHybridSearcher(nil)
	s.embeddingService = cs03Emb{}
	return s
}

// cs03Contents 召回结果的正文集合。
func cs03Contents(chunks []RAGChunk) []string {
	out := make([]string, 0, len(chunks))
	for _, c := range chunks {
		out = append(out, c.Content)
	}
	return out
}

func cs03AssertNoB(t *testing.T, where string, chunks []RAGChunk) {
	t.Helper()
	for _, c := range chunks {
		if contains(c.Content, "CS03B-") {
			t.Errorf("%s：租户 A 的检索召回 B 的知识（跨租户泄漏）：%q", where, c.Content)
		}
	}
}

func cs03AssertNoA(t *testing.T, where string, chunks []RAGChunk) {
	t.Helper()
	for _, c := range chunks {
		if contains(c.Content, "CS03A-") {
			t.Errorf("%s：租户 B 的检索召回 A 的知识（跨租户泄漏）：%q", where, c.Content)
		}
	}
}

// TestCS03ReleaseMatrix_PublishedRecallable_UnpublishedHiddenUntilPublish 主矩阵：
// 已发布可召回（引用含真库 ID/版本）、未发布不命中且发布后可被召回、退役不可见。
func TestCS03ReleaseMatrix_PublishedRecallable_UnpublishedHiddenUntilPublish(t *testing.T) {
	database := cs03Setup(t)
	s := cs03Searcher(t, database)
	ctxA := tenant.WithTenantID(context.Background(), cs03TenantA)

	// 格①：A 私有已发布 → 命中，引用可追溯（chunk ID/文档 ID 为真库行 ID）。
	chunks, err := s.SearchProducts(ctxA, []string{cs03ProdAPub}, cs03Query, 10)
	if err != nil {
		t.Fatalf("检索失败: %v", err)
	}
	got := cs03Contents(chunks)
	if len(got) == 0 {
		t.Fatal("A 私有已发布知识未被召回（真库发布→召回关联断裂）")
	}
	var hasMark bool
	for _, c := range got {
		if contains(c, "CS03A-MARK") {
			hasMark = true
		}
		if contains(c, "CS03A-PENDING") {
			t.Errorf("未发布版本被召回（发布闸失效）：%q", c)
		}
		if contains(c, "CS03A-RETIRED") {
			t.Errorf("旧版（已退役）被召回（retire 失效）：%q", c)
		}
	}
	if !hasMark {
		t.Fatalf("已发布在服版本未在召回集内: %v", got)
	}
	// 引用定位三件：DocID=真库文档行、ChunkID=真库分段行、Score>0；版本从库读回对账。
	var aChunk model.KnowledgeChunk
	if err := database.WithContext(ctxA).Where("product_id = ? AND content LIKE ?", cs03ProdAPub, "CS03A-MARK%").First(&aChunk).Error; err != nil {
		t.Fatalf("读 A 在服分段失败: %v", err)
	}
	var located bool
	for _, c := range chunks {
		if c.ChunkID == strconv.FormatUint(aChunk.ID, 10) && c.DocID == strconv.FormatUint(aChunk.DocumentID, 10) && c.Score > 0 {
			located = true
		}
	}
	if !located {
		t.Fatalf("召回引用未定位到真库行（chunk_id=%d doc=%d）: %+v", aChunk.ID, aChunk.DocumentID, chunks)
	}
	var rel model.KBRelease
	if err := database.WithContext(ctxA).Where("product_id = ?", cs03ProdAPub).First(&rel).Error; err != nil {
		t.Fatalf("读 release 行失败: %v", err)
	}
	if aChunk.KBVersion != rel.EffectiveVersion {
		t.Errorf("引用版本对账失败：chunk.kb_version=%d，effective=%d（引用必须是生效版本）",
			aChunk.KBVersion, rel.EffectiveVersion)
	}

	// 格②：发布 = effective 指针前移 → 原未发布版本即刻可被召回（真发布语义，
	// 不重灌任何数据）。这是「真 FAQ 行发布后可被召回」关联的发布侧闸门。
	if err := database.WithContext(ctxA).Model(&model.KBRelease{}).
		Where("product_id = ?", cs03ProdAPub).
		Update("effective_version", cs03EffVer+1).Error; err != nil {
		t.Fatalf("发布（指针前移）失败: %v", err)
	}
	chunks2, err := s.SearchProducts(ctxA, []string{cs03ProdAPub}, cs03Query, 10)
	if err != nil {
		t.Fatalf("发布后检索失败: %v", err)
	}
	var hasPending bool
	for _, c := range cs03Contents(chunks2) {
		if contains(c, "CS03A-PENDING") {
			hasPending = true
		}
	}
	if !hasPending {
		t.Errorf("发布后新版本仍未被召回（发布→召回关联未打通）: %v", cs03Contents(chunks2))
	}
}

// TestCS03ReleaseMatrix_TenantIsolation_SameKeywordDifferentAnswer 跨租户矩阵：
// B 私有绝不命中 A（双向）、B 公开知识对 A 不可召回（既有规则：检索路无公开豁免）、
// B 检索自己的知识照常可用（不把跨租户一律禁掉）。
func TestCS03ReleaseMatrix_TenantIsolation_SameKeywordDifferentAnswer(t *testing.T) {
	database := cs03Setup(t)
	s := cs03Searcher(t, database)
	ctxA := tenant.WithTenantID(context.Background(), cs03TenantA)
	ctxB := tenant.WithTenantID(context.Background(), cs03TenantB)

	// 格③：A 拿 B 私有库的 product_id 检索 → 0 条（同关键词不同答案绝不串）。
	chunks, err := s.SearchProducts(ctxA, []string{cs03ProdBPriv}, cs03Query, 10)
	if err != nil {
		t.Fatalf("A 检索 B 私有库失败: %v", err)
	}
	if len(chunks) != 0 {
		t.Errorf("B 私有知识对 A 可见（越租户命中 %d 条）: %v", len(chunks), cs03Contents(chunks))
	}

	// 格④：A 拿 B 公开库检索 → 0 条。public_visible 只作用于帮助中心读路，
	// 检索路径不存在"公开即跨租户"的既有许可——本格按既有规则钉住。
	chunks, err = s.SearchProducts(ctxA, []string{cs03ProdBPublic}, cs03Query, 10)
	if err != nil {
		t.Fatalf("A 检索 B 公开库失败: %v", err)
	}
	if len(chunks) != 0 {
		t.Errorf("B 公开知识对 A 可见（检索路无跨租户公开豁免，越权 %d 条）: %v", len(chunks), cs03Contents(chunks))
	}

	// 格⑤：B 自己检索自己的私有库 → 正常命中（隔离不等于全禁）。
	chunks, err = s.SearchProducts(ctxB, []string{cs03ProdBPriv}, cs03Query, 10)
	if err != nil {
		t.Fatalf("B 检索自己私有库失败: %v", err)
	}
	var hasB bool
	for _, c := range cs03Contents(chunks) {
		if contains(c, "CS03B-PRIV") {
			hasB = true
		}
		if contains(c, "CS03A-") {
			t.Errorf("B 的检索召回 A 的知识: %q", c)
		}
	}
	if !hasB {
		t.Errorf("B 私有知识未被 B 自己召回（隔离误伤）: %v", cs03Contents(chunks))
	}

	// 格⑥：全库检索（不带 product 过滤）同样租户隔离——A 的全库视野里没有 B 的任何行
	//（私有与公开都不可见），B 同理。
	allA, err := s.Search(ctxA, cs03Query, 50)
	if err != nil {
		t.Fatalf("A 全库检索失败: %v", err)
	}
	if len(allA) == 0 {
		t.Fatal("A 全库检索未召回自己已发布知识")
	}
	cs03AssertNoB(t, "A 全库检索", allA)
	allB, err := s.Search(ctxB, cs03Query, 50)
	if err != nil {
		t.Fatalf("B 全库检索失败: %v", err)
	}
	cs03AssertNoA(t, "B 全库检索", allB)
}

// TestCS03ReleaseMatrix_EmbeddingErrorDegradesToBM25_StillTenantScoped embedding 服务
// 不可用：按既有配置降级 BM25-lite，降级路径同样带租户隔离与发布闸，来源如实
// （返回的就是库里的行，不是编造）；无候选时诚实零命中。
func TestCS03ReleaseMatrix_EmbeddingErrorDegradesToBM25_StillTenantScoped(t *testing.T) {
	database := cs03Setup(t)
	ctxA := tenant.WithTenantID(context.Background(), cs03TenantA)
	ctxB := tenant.WithTenantID(context.Background(), cs03TenantB)

	s := NewRagSearcherWithRepo(knowledgerepo.NewKnowledgeChunkRepository(database))
	s.SetHybridSearcher(nil)
	errEmb := &cs03ErrEmb{}
	s.embeddingService = errEmb

	// 降级路径：向量检索失败 → BM25-lite 兜底，A 只见 A 的在服行。
	chunks, err := s.Search(ctxA, cs03Query, 10)
	if err != nil {
		t.Fatalf("embedding 故障降级不应报错: %v", err)
	}
	if errEmb.calls == 0 {
		t.Fatal("embedding 桩未被实际调用（降级链前提不成立）")
	}
	var hasMark bool
	for _, c := range cs03Contents(chunks) {
		if contains(c, "CS03A-MARK") {
			hasMark = true
		}
		if contains(c, "CS03A-PENDING") || contains(c, "CS03A-RETIRED") {
			t.Errorf("降级路径绕过了发布闸: %q", c)
		}
		if contains(c, "CS03B-") {
			t.Errorf("降级路径跨租户泄漏: %q", c)
		}
	}
	if !hasMark {
		t.Errorf("BM25 降级未召回在服行: %v", cs03Contents(chunks))
	}

	// B 侧同一故障路径同样只见到自己的行。
	chunksB, err := s.Search(ctxB, cs03Query, 10)
	if err != nil {
		t.Fatalf("B 降级检索失败: %v", err)
	}
	cs03AssertNoA(t, "B 降级检索", chunksB)
	var hasBPriv bool
	for _, c := range cs03Contents(chunksB) {
		if contains(c, "CS03B-PRIV") {
			hasBPriv = true
		}
	}
	if !hasBPriv {
		t.Errorf("B 降级检索未召回自己私有行: %v", cs03Contents(chunksB))
	}

	// 无候选：词面上完全不相交的查询（分词后与五条夹具正文零公共词元，含单字）
	// → 诚实零命中（err=nil、0 条，不伪造成功）。
	empty, err := s.Search(ctxA, "火山灰扩散数值模拟报告", 10)
	if err != nil {
		t.Fatalf("无候选检索不应报错: %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("无候选应 0 条，实际 %d: %v", len(empty), cs03Contents(empty))
	}
}

// cs03Sanity 前置自证：矩阵行都真的在库里且分属两租户（防止夹具静默失真把断言量空）。
func TestCS03ReleaseMatrix_FixturePrecondition(t *testing.T) {
	database := cs03Setup(t)
	ctxA := tenant.WithTenantID(context.Background(), cs03TenantA)
	ctxB := tenant.WithTenantID(context.Background(), cs03TenantB)

	var aCount, bCount int64
	if err := database.WithContext(ctxA).Model(&model.KnowledgeChunk{}).Where("product_id = ?", cs03ProdAPub).Count(&aCount).Error; err != nil {
		t.Fatalf("读 A 分段失败: %v", err)
	}
	if err := database.WithContext(ctxB).Model(&model.KnowledgeChunk{}).Where("product_id IN ?", []string{cs03ProdBPriv, cs03ProdBPublic}).Count(&bCount).Error; err != nil {
		t.Fatalf("读 B 分段失败: %v", err)
	}
	if aCount != 3 || bCount != 2 {
		t.Fatalf("夹具前置不成立：A=%d（期望3）B=%d（期望2）", aCount, bCount)
	}
	// 各租户读不到对方的行（on 模式下隔离 callback 对读也生效）。
	var aSeesB, bSeesA int64
	database.WithContext(ctxA).Model(&model.KnowledgeChunk{}).Where("product_id = ?", cs03ProdBPriv).Count(&aSeesB)
	database.WithContext(ctxB).Model(&model.KnowledgeChunk{}).Where("product_id = ?", cs03ProdAPub).Count(&bSeesA)
	if aSeesB != 0 || bSeesA != 0 {
		t.Fatalf("隔离 callback 未生效：A见B=%d B见A=%d", aSeesB, bSeesA)
	}
}
