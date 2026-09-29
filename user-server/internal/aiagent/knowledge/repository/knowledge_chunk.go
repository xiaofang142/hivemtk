package repository

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"hivemtk-user/internal/aiagent/knowledge/model"
	"hivemtk-user/internal/pkg/kbrelease"
	"hivemtk-user/internal/pkg/utils/logger"

	"gorm.io/gorm"
)

// KnowledgeChunkRepository 知识库分段仓储
type KnowledgeChunkRepository struct {
	db *gorm.DB
}

// NewKnowledgeChunkRepository 创建知识库分段仓储
func NewKnowledgeChunkRepository(db *gorm.DB) *KnowledgeChunkRepository {
	return &KnowledgeChunkRepository{db: db}
}

// Create 创建分段
//
// T-P9-02：写入前先过一次版本打戳（见 kbrelease.StampForWrite）。收在仓储这一格而不是
// 各导入入口，是因为漏掉一次打戳的表现是"这段内容带着 kb_version=0 落库"，
// 而 0 的语义是"不受闸门管" —— 那是 AC① 的无声绕过口，不是少一个装饰字段。
func (r *KnowledgeChunkRepository) Create(ctx context.Context, chunk *model.KnowledgeChunk) error {
	if chunk.Metadata == "" {
		chunk.Metadata = "{}"
	}
	if err := kbrelease.StampOneForWrite(ctx, r.db, chunk); err != nil {
		return err
	}
	return r.db.WithContext(ctx).Create(chunk).Error
}

// BatchCreate 批量创建分段（按 product_id 分组各分配一次待发布桶）
func (r *KnowledgeChunkRepository) BatchCreate(ctx context.Context, chunks []model.KnowledgeChunk) error {
	if len(chunks) == 0 {
		return nil
	}
	if err := kbrelease.StampForWrite(ctx, r.db, chunks); err != nil {
		return err
	}
	batchSize := 100
	for i := 0; i < len(chunks); i += batchSize {
		end := i + batchSize
		if end > len(chunks) {
			end = len(chunks)
		}
		if err := r.db.WithContext(ctx).Create(chunks[i:end]).Error; err != nil {
			return err
		}
	}
	return nil
}

// guardDirectWrite 拦下"在已进发布制的库上就地改写/物理删除一条在服分段"。
//
// 只拦这两种，其余写通路不动，理由是按"会不会让未审批的正文当场可检"划的线：
//   - Update 覆盖 content：kb_version 保持原值（存量行是 0），改完立刻在服 ⇒ 绕过审批 ⇒ 拒；
//   - Delete(id) 抹掉一条在服分段：已上线内容被一次没有留痕的动作撤下 ⇒ 拒（改提 retire）；
//   - Create / BatchCreate：新行进待发布桶，未发布前本来就不可见 ⇒ 放行并打戳；
//   - DeleteByDocumentID / DeleteByProductID：重切与整库清理的内部通路，
//     一并拦会把"导入"（本卡要求它走发布制）弄成静默失败 ⇒ 放行，只出声。
//
// productID 传空时按 id 回查一次：UpdateChunk 那条链带得上产品号，但 Delete 只有 id。
// 回查只在闸门 on 时发生（见 DirectWriteBlocked），off 档一个额外查询都不该有。
func (r *KnowledgeChunkRepository) guardDirectWrite(ctx context.Context, productID string, id uint64) error {
	if !kbrelease.GateOn() {
		return nil
	}
	if productID == "" {
		// 这里**不能**用 GetByID：它把"没有这一行"翻成 error（"chunk not found"），
		// 而闸门要的就是"没有这一行 ⇒ 没什么可保护的"。照 GetByID 的写法，
		// 删一条已经不存在的分段会在 on 档凭空多出一个 404/500 —— 而删除本来就是幂等的，
		// off 档它至今回 nil。判据只在"行确实在、且它所属的库受管"时才开火。
		var found model.KnowledgeChunk
		err := r.db.WithContext(ctx).Where("id = ?", id).First(&found).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		productID = found.ProductID
	}
	blocked, err := kbrelease.DirectWriteBlocked(ctx, r.db, productID)
	if err != nil {
		return err
	}
	if blocked {
		return fmt.Errorf("%w（chunk=%d product=%s）", kbrelease.ErrGovernedDirectWrite, id, productID)
	}
	return nil
}

// GetByDocumentID 根据文档 ID 获取分段
func (r *KnowledgeChunkRepository) GetByDocumentID(ctx context.Context, documentID uint64) ([]model.KnowledgeChunk, error) {
	var chunks []model.KnowledgeChunk
	if err := r.db.WithContext(ctx).Where("document_id = ?", documentID).
		Order("chunk_index ASC").Find(&chunks).Error; err != nil {
		return nil, err
	}
	return chunks, nil
}

// PageByDocumentID 分页获取文档分段
func (r *KnowledgeChunkRepository) PageByDocumentID(ctx context.Context, documentID uint64, page, pageSize int) ([]model.KnowledgeChunk, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	var total int64
	q := r.db.WithContext(ctx).Model(&model.KnowledgeChunk{}).Where("document_id = ?", documentID)
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var chunks []model.KnowledgeChunk
	if err := q.Order("chunk_index ASC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&chunks).Error; err != nil {
		return nil, 0, err
	}
	return chunks, total, nil
}

// Update 更新分段
//
// embedding_id 总是参与更新；service 层决定是否清空，repository 层如实落盘。
// 这样 service.UpdateChunk 显式设置 chunk.EmbeddingID = "" 触发重新向量化时能正常清空旧向量。
//   - 唯一调用方为 KnowledgeMerchantService.UpdateChunk，行为一致，无副作用。
func (r *KnowledgeChunkRepository) Update(ctx context.Context, chunk *model.KnowledgeChunk) error {
	if chunk.ID == 0 {
		return errors.New("chunk id is required")
	}
	if err := r.guardDirectWrite(ctx, chunk.ProductID, chunk.ID); err != nil {
		return err
	}
	updates := map[string]any{
		"content":      chunk.Content,
		"char_count":   chunk.CharCount,
		"token_count":  chunk.TokenCount,
		"content_hash": chunk.ContentHash,
		"embedding_id": chunk.EmbeddingID,
	}
	return r.db.WithContext(ctx).Model(&model.KnowledgeChunk{}).
		Where("id = ?", chunk.ID).Updates(updates).Error
}

// Delete 根据 ID 删除分段
//
// 已进发布制的库上这条被拒（guardDirectWrite）：物理删除一条在服分段既不留痕，
// 又让该库的回滚失去可回之物 —— 要下线内容请提一条 retire 变更，走审批与发布。
func (r *KnowledgeChunkRepository) Delete(ctx context.Context, id uint64) error {
	if err := r.guardDirectWrite(ctx, "", id); err != nil {
		return err
	}
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.KnowledgeChunk{}).Error
}

// DeleteByDocumentID 删除文档的所有分段
//
// 重切/导入的内部通路，闸门下**不**拒（拒它等于把导入链路一起关掉），但受管库上会出声：
// 老行被物理删掉、新行进待发布桶 ⇒ 这篇文档在发布之前检不到，且回滚救不了已删的行。
func (r *KnowledgeChunkRepository) DeleteByDocumentID(ctx context.Context, documentID uint64) error {
	if err := r.warnIfGovernedLosingRows(ctx, "document_id", "document_id = ?", documentID); err != nil {
		return err
	}
	return r.db.WithContext(ctx).Where("document_id = ?", documentID).
		Delete(&model.KnowledgeChunk{}).Error
}

// DeleteByProductID 删除产品的所有分段
func (r *KnowledgeChunkRepository) DeleteByProductID(ctx context.Context, productID string) error {
	if err := r.warnIfGovernedLosingRows(ctx, "product_id", "product_id = ?", productID); err != nil {
		return err
	}
	return r.db.WithContext(ctx).Where("product_id = ?", productID).
		Delete(&model.KnowledgeChunk{}).Error
}

// warnIfGovernedLosingRows 批量物理删除前，若该批行里有**在服**的分段就出声。
//
// 只出声不改判据的原因见上面两条方法注释：这条通路既服务于导入重切、也服务于整库清理，
// 拦下来的代价是导入不可用，放过的代价只是回滚窗口变短 —— 两者都不该由这里代答。
// 谓词用的是叶子包那一份可见性判据（同一处定义），不在这里另写版本号比较。
//
// label 是**这句告警自己**要的条件名：where 那段是给 SQL 用的（带占位符），
// 把它原样拼进人读的那句会变成 "document_id = ?=10"，值班照着这句去库里查会查错列。
//
// 一次 COUNT 换一句告警，且只在闸门 on 时发生。
func (r *KnowledgeChunkRepository) warnIfGovernedLosingRows(ctx context.Context, label, where string, arg any) error {
	if !kbrelease.GateOn() {
		return nil
	}
	pred := kbrelease.VisiblePredicate()
	if pred == "" {
		return nil
	}
	var inForce int64
	if err := r.db.WithContext(ctx).Table("knowledge_chunks").
		Where(where, arg).Where(pred).Count(&inForce).Error; err != nil {
		return err
	}
	if inForce > 0 {
		logger.Warnf("[kb-release] 即将物理删除 %s=%v 的 %d 条在服分段：该库若已进发布制，"+
			"这些行不会随回滚回来，重建出的新段要等一次发布才可见", label, arg, inForce)
	}
	return nil
}

// CountByProductID 统计产品分段数
func (r *KnowledgeChunkRepository) CountByProductID(ctx context.Context, productID string) (int64, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&model.KnowledgeChunk{}).
		Where("product_id = ?", productID).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (r *KnowledgeChunkRepository) CountByMerchant(ctx context.Context) (int64, error) {
	var count int64
	q := r.db.WithContext(ctx).Model(&model.KnowledgeChunk{})
	if err := q.Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// FindByContentHash 根据内容哈希查重
func (r *KnowledgeChunkRepository) FindByContentHash(ctx context.Context, productID string, hash string) (*model.KnowledgeChunk, error) {
	var chunk model.KnowledgeChunk
	q := r.db.WithContext(ctx).Where("content_hash = ?", hash)
	if productID != "" {
		q = q.Where("product_id = ?", productID)
	}
	if err := q.First(&chunk).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &chunk, nil
}

// UpdateScore 更新最近检索分数
func (r *KnowledgeChunkRepository) UpdateScore(ctx context.Context, id uint64, score float64) error {
	return r.db.WithContext(ctx).Model(&model.KnowledgeChunk{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"similarity_score": score,
			"hit_count":        gorm.Expr("hit_count + 1"),
		}).Error
}

// IncrementHitCount 批量累加命中分段 hit_count（tooluse 调用，替代直接 DB 访问）
//
// 五层架构合规：L4 能力层（tooluse）通过 L3 仓储层间接操作 DB
func (r *KnowledgeChunkRepository) IncrementHitCount(ctx context.Context, ids []uint64) error {
	if len(ids) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.KnowledgeChunk{}).
		Where("id IN ?", ids).
		UpdateColumn("hit_count", gorm.Expr("hit_count + 1")).Error
}

// GetByID 根据 ID 获取分段
func (r *KnowledgeChunkRepository) GetByID(ctx context.Context, id uint64) (*model.KnowledgeChunk, error) {
	var chunk model.KnowledgeChunk
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&chunk).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("chunk not found")
		}
		return nil, err
	}
	return &chunk, nil
}

// BatchUpdateLastIndexed 批量更新最后索引时间
func (r *KnowledgeChunkRepository) BatchUpdateLastIndexed(ctx context.Context, documentID uint64) error {
	return r.db.WithContext(ctx).Model(&model.KnowledgeChunk{}).
		Where("document_id = ?", documentID).
		Update("created_at", time.Now()).Error
}

// UpdateEmbeddingsBatch 事务式批量更新分段 embedding
//
// 要求 chunks 与 embeddings 长度一致；使用参数化 SQL 防止注入；事务保证原子性。
func (r *KnowledgeChunkRepository) UpdateEmbeddingsBatch(ctx context.Context, chunks []model.KnowledgeChunk, embeddings [][]float32) error {
	return r.UpdateEmbeddingsBatchWithSource(ctx, chunks, embeddings, "tei")
}

// UpdateEmbeddingsBatchWithSource D16：更新 embedding 并显式标记来源（"tei"/"hash"）。
// 兜底向量（hash）必须与真实向量（tei）可区分——读路径按 source='tei' 过滤防混写。
func (r *KnowledgeChunkRepository) UpdateEmbeddingsBatchWithSource(ctx context.Context, chunks []model.KnowledgeChunk, embeddings [][]float32, source string) error {
	if len(chunks) != len(embeddings) {
		return errors.New("chunks 与 embeddings 长度不一致")
	}
	if len(chunks) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for i, c := range chunks {
			vec := vecToPGString(embeddings[i])
			if err := tx.Exec(
				"UPDATE knowledge_chunks SET embedding = $1::vector, embed_status = 'indexed', embedding_source = $3 WHERE id = $2",
				vec, c.ID, source,
			).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func vecToPGString(v []float32) string {
	if len(v) == 0 {
		return "[]"
	}
	var b []byte
	b = append(b, '[')
	for i, f := range v {
		if i > 0 {
			b = append(b, ',')
		}
		b = strconv.AppendFloat(b, float64(f), 'g', -1, 32)
	}
	b = append(b, ']')
	return string(b)
}
