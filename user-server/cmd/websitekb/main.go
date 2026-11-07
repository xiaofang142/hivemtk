// 官网客服知识库导入工具（一次性）。
//
// 用途：把本地 hivemtk-platform-cs 知识库按官网口径改写后的内容，
//
//	导入线上 product「HiveMTK 产品知识库」并完成向量化。
//
// 为什么用程序而不是 SQL：
//  1. 插完 chunk 必须**调 embedding 服务向量化**（embedding 走线上服务，SQL 做不到）；
//  2. 走 GORM 与生产代码同一条路径，避开手写 SQL 的列名/类型坑。
//
// 幂等：按 (product_id, title) 定位文档、按 (document_id, chunk_index) 定位 chunk，
//
//	重复执行不会产生重复数据。
//
// 用法：
//
//	DB_HOST=127.0.0.1 DB_PORT=8202 POSTGRES_PASSWORD=xxx go run ./cmd/websitekb
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	knowledgerepo "hivemtk-user/internal/aiagent/knowledge/repository"
	knowledgesvc "hivemtk-user/internal/aiagent/knowledge/service"
	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/secrets"
)

const productID = "ad7b78b5-5ff4-4ea0-b689-328edfaf91a9"

func main() {
	host := envOr("DB_HOST", "127.0.0.1")
	port := envOr("DB_PORT", "8202")
	user := envOr("POSTGRES_USER", "admin")
	pass := os.Getenv("POSTGRES_PASSWORD")
	if pass == "" {
		fmt.Println("POSTGRES_PASSWORD 未设置")
		os.Exit(1)
	}
	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=user_db sslmode=disable", host, port, user, pass)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger:                 logger.Default.LogMode(logger.Warn),
		SkipDefaultTransaction: true,
	})
	if err != nil {
		fmt.Printf("连库失败: %v\n", err)
		os.Exit(1)
	}
	ctx := context.Background()

	// 初始化全局 AEAD：参数中心里 embedding.global_override 的 api_key 是加密存储的，
	// 解密靠 secrets 包的 MASTER_KEY。secrets.InitFromEnv 由**装配层**调用，
	// 本工具是独立进程 ⇒ 必须自己调一次，否则解密失败、embedding 拿空 key 去请求
	// dashscope ⇒ 401。
	if err := secrets.InitFromEnv(); err != nil {
		fmt.Printf("secrets 初始化失败（MASTER_KEY 未设置或过短）: %v\n", err)
		os.Exit(1)
	}

	// 注入 embedding 全局覆盖的读取口。
	//
	// llm.GetGlobalEmbeddingOverride 依赖装配层注入的 provider（参数中心
	// embedding.global_override 存的是云端 embedding 地址 + 加密的 key）。
	// 本工具是独立进程、没走完整装配 ⇒ provider 为 nil ⇒ 取不到覆盖，
	// 回落默认的本地地址 127.0.0.1:8208（生产没这个服务）⇒ 向量化必然失败。
	// 这里按装配层同样的口径把 provider 装上：读参数中心，交给 llm 侧解密。
	llm.SetEmbeddingOverrideProvider(func(c context.Context) string {
		var raw string
		if err := db.WithContext(c).Raw(
			`SELECT param_value FROM config_params
			  WHERE param_group = 'embedding' AND key = 'global_override' LIMIT 1`,
		).Scan(&raw).Error; err != nil {
			return ""
		}
		return raw
	})

	// 先修 FAQ 里"配置"这类泛化词造成的歧义，再补知识库。
	if err := fixFAQKeywords(ctx, db); err != nil {
		fmt.Printf("FAQ 关键词修正失败: %v\n", err)
		os.Exit(1)
	}

	// 先修线上已存在的「价格与计费」文档（口径过时），再补新文档。
	if err := fixPricingDoc(ctx, db); err != nil {
		fmt.Printf("价格文档修正失败: %v\n", err)
		os.Exit(1)
	}

	for _, d := range kbDocs {
		doc, err := upsertDoc(ctx, db, d)
		if err != nil {
			fmt.Printf("[%s] 文档失败: %v\n", d.Title, err)
			os.Exit(1)
		}
		n, err := upsertChunks(ctx, db, doc.ID, d.Chunks)
		if err != nil {
			fmt.Printf("[%s] chunk 失败: %v\n", d.Title, err)
			os.Exit(1)
		}
		fmt.Printf("[%s] 文档 id=%d，chunk %d 条\n", d.Title, doc.ID, n)

		// 向量化：Reindex 会读 chunk 内容 → 调 embedding 服务 → 回写 embedding
		// 并把文档状态置为 indexed。不做这步，chunk 的 embedding 为空，
		// 检索时 `WHERE embedding IS NOT NULL` 会把它们全过滤掉（等于白插）。
		ks := knowledgesvc.NewKnowledgeServiceWithDeps(knowledgesvc.KnowledgeServiceDeps{
			RagRepo:       knowledgerepo.NewRagConfigRepository(db),
			DocRepo:       knowledgerepo.NewKnowledgeDocumentRepository(db),
			ChunkRepo:     knowledgerepo.NewKnowledgeChunkRepository(db),
			ParentRepo:    knowledgerepo.NewKnowledgeChunkParentRepository(db),
			ImportLogRepo: knowledgerepo.NewKnowledgeImportLogRepository(db),
			SearchLogRepo: knowledgerepo.NewKnowledgeSearchLogRepository(db),
		})
		if err := ks.Reindex(ctx, productID, doc.ID); err != nil {
			fmt.Printf("[%s] 向量化失败: %v\n", d.Title, err)
			os.Exit(1)
		}
		fmt.Printf("[%s] 向量化完成\n", d.Title)
	}

	// 自检
	type row struct {
		Title   string
		Chunks  int64
		WithEmb int64
	}
	var rows []row
	db.Raw(`SELECT d.title AS title,
	              (SELECT count(*) FROM knowledge_chunks c WHERE c.document_id = d.id) AS chunks,
	              (SELECT count(*) FROM knowledge_chunks c WHERE c.document_id = d.id AND c.embedding IS NOT NULL) AS with_emb
	         FROM knowledge_documents d
	        WHERE d.product_id = ? ORDER BY d.id`, productID).Scan(&rows)
	fmt.Println("\n知识库现状：")
	for _, r := range rows {
		fmt.Printf("  %-42s chunk=%-3d 有向量=%-3d\n", r.Title, r.Chunks, r.WithEmb)
	}
}

// fixPricingDoc 修正线上「HiveMTK 价格与计费」文档的旧口径并重新向量化。
func fixPricingDoc(ctx context.Context, db *gorm.DB) error {
	var doc model.KnowledgeDocument
	err := db.WithContext(ctx).
		Where("product_id = ? AND title = ?", productID, "HiveMTK 价格与计费").
		First(&doc).Error
	if err != nil {
		return fmt.Errorf("找不到价格文档: %w", err)
	}
	var chunkID uint64
	if err := db.WithContext(ctx).Raw(
		`SELECT id FROM knowledge_chunks WHERE document_id = ? ORDER BY chunk_index LIMIT 1`, doc.ID,
	).Scan(&chunkID).Error; err != nil {
		return err
	}
	if chunkID == 0 {
		return fmt.Errorf("价格文档没有 chunk")
	}
	if err := db.WithContext(ctx).Exec(
		`UPDATE knowledge_chunks SET content = $1, char_count = $2, embed_status = 'pending', embedding = NULL WHERE id = $3`,
		pricingCorrection, len([]rune(pricingCorrection)), chunkID).Error; err != nil {
		return err
	}
	doc.EmbedStatus = model.EmbedStatusPending
	if err := db.WithContext(ctx).Save(&doc).Error; err != nil {
		return err
	}
	ks := knowledgesvc.NewKnowledgeServiceWithDeps(knowledgesvc.KnowledgeServiceDeps{
		RagRepo:       knowledgerepo.NewRagConfigRepository(db),
		DocRepo:       knowledgerepo.NewKnowledgeDocumentRepository(db),
		ChunkRepo:     knowledgerepo.NewKnowledgeChunkRepository(db),
		ParentRepo:    knowledgerepo.NewKnowledgeChunkParentRepository(db),
		ImportLogRepo: knowledgerepo.NewKnowledgeImportLogRepository(db),
		SearchLogRepo: knowledgerepo.NewKnowledgeSearchLogRepository(db),
	})
	if err := ks.Reindex(ctx, productID, doc.ID); err != nil {
		return fmt.Errorf("价格文档向量化失败: %w", err)
	}
	fmt.Println("[HiveMTK 价格与计费] 口径已修正并重新向量化")
	return nil
}

// fixFAQKeywords 修正线上 FAQ 里"配置"这类在多条间重复的泛化词。
//
// 现象：问「部署需要什么配置？」，命中的是「AI 智能体在哪里配置？」——
// 4 条 FAQ 的关键词都含"配置"，而问法里只有"部署""需要"能区分。
// 泛化词让多条在同一个词上打平，最后由别的词元决胜、答非所问。
//
// 改法：把裸的泛化词（"配置"）换成带上下文的区分度词组
// （"配置要求""需要什么配置" / "智能体配置""后台操作" / "创建智能体" …），
// 让"问的是什么配置"这件事本身成为命中条件。
func fixFAQKeywords(ctx context.Context, db *gorm.DB) error {
	type fix struct {
		question string
		keywords string
	}
	fixes := []fix{
		{"AI 智能体在哪里配置？后台怎么操作？",
			"ARRAY['后台','智能体配置','人设','欢迎语','绑定到渠道','在哪配置','后台操作','绑定智能体']"},
		{"私有化部署需要什么配置？",
			"ARRAY['CPU','内存','显卡','GPU','多少核','几个G','服务器','硬件','多大','机器','配置要求','部署配置','需要什么配置','资源要求']"},
		{"怎么创建和配置智能体？",
			"ARRAY['创建','新建','智能体创建','怎么创建','创建智能体','新建智能体']"},
		{"HiveMTK 怎么收费？",
			"ARRAY['收费','价格','多少钱','计费','订阅费','套餐','积分','免费','购买','费用','怎么收费','收费吗','价格表']"},
		{"如何配置 AI 智能体？（后台操作步骤）",
			"ARRAY['设置','人设','欢迎语','绑定到渠道','配置智能体','智能体设置','绑定渠道','怎么设置智能体']"},
		{"HiveMTK 的 AI 智能体能做什么？",
			"ARRAY['AI','RAG','自动回复','自动应答','销冠','有什么功能','产品功能','功能有哪些','智能体能力','智能体功能']"},
		{"HiveMTK 的 AI 智能体和普通客服机器人有什么区别？",
			"ARRAY['ReAct','机器人','区别','AI','工具','和普通','智能体区别','普通客服机器人']"},
		{"知识库怎么建？RAG 问答效果如何？",
			"ARRAY['知识库','RAG','检索','召回','切片','向量化','rag','文档','建库','知识库怎么建','怎么建知识库','建知识库']"},
	}
	for _, f := range fixes {
		if err := db.WithContext(ctx).Exec(
			`UPDATE faq_entries SET keywords = `+f.keywords+` WHERE tenant_id = 1 AND agent_id = 1 AND question = $1`,
			f.question).Error; err != nil {
			return fmt.Errorf("修正 %q 关键词失败: %w", f.question, err)
		}
	}
	fmt.Println("[FAQ 关键词] 4 条含「配置」的问答已去泛化")
	return nil
}

func upsertDoc(ctx context.Context, db *gorm.DB, d kbDoc) (*model.KnowledgeDocument, error) {
	var existing model.KnowledgeDocument
	err := db.WithContext(ctx).Where("product_id = ? AND title = ?", productID, d.Title).First(&existing).Error
	if err == nil {
		existing.ChunkCount = len(d.Chunks)
		existing.EmbedStatus = model.EmbedStatusPending
		existing.UpdatedAt = nowUTC()
		if err := db.WithContext(ctx).Save(&existing).Error; err != nil {
			return nil, err
		}
		return &existing, nil
	}
	doc := &model.KnowledgeDocument{
		TenantID:      1,
		ProductID:     productID,
		SourceType:    model.SourceType("manual"),
		Title:         d.Title,
		FileName:      "hivemtk-kb.md",
		FileType:      "md",
		MimeType:      "text/markdown",
		ChunkCount:    len(d.Chunks),
		EmbedStatus:   model.EmbedStatusPending,
		Category:      d.Category,
		Tags:          "[]",
		Metadata:      "{}",
		PublicVisible: false,
		Status:        1,
	}
	if err := db.WithContext(ctx).Create(doc).Error; err != nil {
		return nil, err
	}
	return doc, nil
}

// upsertChunks 插/更新 chunk。
//
// 用原生 SQL 而不是 GORM 模型：knowledge_chunks 表上的 embed_status / embedding
// 两列在 model.KnowledgeChunk 里**没有对应字段**（模型只声明了业务列），
// 而这两列恰恰是这里的关键：embed_status 决定 Reindex 走不走重算、embedding
// 为空会让检索层 `WHERE embedding IS NOT NULL` 把 chunk 全过滤掉。
func upsertChunks(ctx context.Context, db *gorm.DB, docID uint64, contents []string) (int, error) {
	for i, c := range contents {
		var existingID uint64
		err := db.WithContext(ctx).Raw(
			`SELECT id FROM knowledge_chunks WHERE document_id = ? AND chunk_index = ?`,
			docID, i).Scan(&existingID).Error
		if err != nil {
			return i, err
		}
		if existingID != 0 {
			if err := db.WithContext(ctx).Exec(
				`UPDATE knowledge_chunks
				    SET content = $1, char_count = $2, embed_status = 'pending', embedding = NULL
				  WHERE id = $3`,
				c, len([]rune(c)), existingID).Error; err != nil {
				return i, err
			}
			continue
		}
		// 只写两边都有的最小列集：线上 knowledge_chunks 没有 updated_at 列
		// （本地有），写了会在线上报 column does not exist。
		if err := db.WithContext(ctx).Exec(
			`INSERT INTO knowledge_chunks
			    (tenant_id, document_id, product_id, chunk_index, content, char_count, weight,
			     embed_status, embedding_source, source_language, metadata, created_at)
			 VALUES (1, $1, $2, $3, $4, $5, 1, 'pending', 'tei', 'zh', '{}'::jsonb, NOW())`,
			docID, productID, i, c, len([]rune(c))).Error; err != nil {
			return i, err
		}
	}
	return len(contents), nil
}

func nowUTC() time.Time { return time.Now().UTC() }

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
