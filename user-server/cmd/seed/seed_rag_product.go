// seed_rag_product.go 平台客服知识库的 RAG 产品注册行。
//
// 为什么需要它：cmd/seed 的客服智能体把 rag_product_ids 绑到 'hivemtk-platform-cs'，
// 而在本文件之前，这一行的**唯一**产码是 migrations/031_platform_cs_rag_seed.sql，
// 该文件按它自己的头注释「不会被任何自动路径执行」（compose 的 initdb.d 只挂
// init-user-db.sql，Go 侧迁移器只跑 GORM AutoMigrate + Go 任务、从不读 .sql）。
// 结果是：按常规路径装完的库里，智能体绑着一个查无此物的产品，后台「知识库」列表
// 看不到它、也就永远调不到它的检索参数（top_k / 阈值 / embedding 档位）。
//
// 与 031 的分工：031 仍是一次性人工灌入路径（它同时带 13 篇知识文档与分段）；
// 本文件只补"注册行"这一格，让任何一次 cmd/seed 都能把绑定落到真实存在的产品上。
// 两处必须同源的关键字段（id / category / vector_table / embedding_model /
// embedding_dim / llm_model）由 scripts/check-deploy-claims.py 规则 8 对着 031 校准。
package main

import (
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"hivemtk-user/internal/model"
)

const (
	// platformCSProductID 平台客服知识库产品 ID，与 scripts/seed/expand_knowledge_base*.py 的 PRODUCT_ID 同值。
	platformCSProductID = "hivemtk-platform-cs"
	// platformCSVectorTable 该产品的向量表名（rag_products.vector_table 上有唯一索引）。
	platformCSVectorTable = "rag_platform_cs"
)

// platformCSRagProduct 与 migrations/031_platform_cs_rag_seed.sql 的 INSERT 同值的注册行。
func platformCSRagProduct() model.RagProduct {
	return model.RagProduct{
		ID:                  platformCSProductID,
		Name:                "HiveMTK 平台客服知识库",
		Description:         "用于官网网页客服自动回答商户关于 HiveMTK 项目的开源信息、部署、运维、架构、资产市场、AI 智能体等咨询。涵盖 13 大主题文档与可检索知识分段。",
		Category:            "platform_cs",
		VectorTable:         platformCSVectorTable,
		EmbeddingModel:      "bge-m3",
		EmbeddingDim:        1024,
		LLMModel:            "Qwen2.5-3B-Instruct",
		Temperature:         0.3,
		MaxTokens:           1024,
		TopP:                0.9,
		FrequencyPenalty:    0.5,
		PresencePenalty:     0.5,
		ResponseFormat:      "text",
		SystemPrompt:        "你是 HiveMTK 官方客服助手，负责解答商户关于本项目的咨询。回答依据下方检索到的知识片段，要求：1) 准确，不编造；2) 简洁，单次回复不超过 200 字；3) 涉及部署/命令时给出具体步骤；4) 不涉及定价、版本下载、注册开户等已下线内容；5) 引导至 GitHub/Gitee 仓库或微信群获取更多帮助。",
		TopK:                5,
		ChunkSize:           800,
		ChunkOverlap:        100,
		SimilarityThreshold: 0.55,
		IsActive:            true,
		Status:              1,
	}
}

// ensurePlatformCSRagProduct 幂等补齐注册行：缺则插入，已存在则只同步配置字段。
//
// 冲突时**不**回写 doc_count / chunk_count / search_count / last_*_at：这些是导入与检索
// 链路攒出来的运行期统计，重跑一次演示种子就把它们抹零，等于用演示数据盖掉真实用量。
//
// Clean 侧刻意不删这一行：知识文档与分段可能是 031 或 expand_knowledge_base*.py 灌进来的，
// 种子不拥有它们，删掉注册行只会留下一堆没人认领的分段。
func ensurePlatformCSRagProduct(database *gorm.DB) error {
	row := platformCSRagProduct()
	err := database.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"name", "description", "category", "vector_table",
			"embedding_model", "embedding_dim", "llm_model",
			"temperature", "max_tokens", "top_p", "frequency_penalty", "presence_penalty",
			"response_format", "system_prompt",
			"top_k", "chunk_size", "chunk_overlap", "similarity_threshold",
			"is_active", "status", "updated_at",
		}),
	}).Create(&row).Error
	if err != nil {
		return fmt.Errorf("写入 rag_products(%s) 失败: %w", platformCSProductID, err)
	}
	return nil
}
