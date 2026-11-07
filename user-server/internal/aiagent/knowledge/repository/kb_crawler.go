// Package repository T4 知识库冷启动仓储层（AV-04c 批次7 刀3）。
//
// 爬取源/批次/QA 候选的读写与爬取文档入库（kb_documents / kb_document_chunks）
// 自 ops… 自 aiagent/knowledge/service/kb_crawler.go、kb_qa_split.go 下沉：
// service 只留校验、配额、状态机等业务规则，SQL 逐字保留在本包。
package repository

import (
	"context"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// KBCrawlerRepository 爬取源/批次/入库文档仓储
type KBCrawlerRepository struct {
	db *gorm.DB
}

// NewKBCrawlerRepository 创建爬取仓储（显式句柄，装配方/测试注入用）。
func NewKBCrawlerRepository(db *gorm.DB) *KBCrawlerRepository {
	return &KBCrawlerRepository{db: db}
}

// NewKBCrawlerRepositoryDefault 默认构造（全局句柄；与传 GetDB() 同语义）。
func NewKBCrawlerRepositoryDefault() *KBCrawlerRepository {
	return NewKBCrawlerRepository(_db.GetDB())
}

// Connected 句柄就绪探针（纯内存访问器，无 DB 操作）。
func (r *KBCrawlerRepository) Connected() bool { return r != nil && r.db != nil }

// —— 爬取源（SQL 逐字保留自 service/kb_crawler.go）——

// GetSourceByID 按主键取源（not-found 时返回 gorm 错误，由调用方映射语义）。
func (r *KBCrawlerRepository) GetSourceByID(ctx context.Context, id uint) (*model.KBCrawlerSource, error) {
	var src model.KBCrawlerSource
	if err := r.db.WithContext(ctx).First(&src, id).Error; err != nil {
		return nil, err
	}
	return &src, nil
}

// CountSources 统计源数（配额检查用；租户隔离 callback 在 on 模式下已注入 WHERE）。
func (r *KBCrawlerRepository) CountSources(ctx context.Context) (int64, error) {
	var cnt int64
	err := r.db.WithContext(ctx).Model(&model.KBCrawlerSource{}).Count(&cnt).Error
	return cnt, err
}

// KBExists 目标知识库存在性（建源归属校验用；租户隔离 callback 在 on 模式下
// 已注入 WHERE tenant_id）。
func (r *KBCrawlerRepository) KBExists(ctx context.Context, kbID uint) (bool, error) {
	var kbRow model.KnowledgeBase
	if err := r.db.WithContext(ctx).First(&kbRow, kbID).Error; err != nil {
		return false, err
	}
	return true, nil
}

// CreateSource 落库新源。
func (r *KBCrawlerRepository) CreateSource(ctx context.Context, src *model.KBCrawlerSource) error {
	return r.db.WithContext(ctx).Create(src).Error
}

// UpdateSourceFields 按白名单字段更新源（fields 已由 service 层白名单过滤）。
func (r *KBCrawlerRepository) UpdateSourceFields(ctx context.Context, id uint, fields map[string]any) error {
	return r.db.WithContext(ctx).Model(&model.KBCrawlerSource{}).Where("id = ?", id).Updates(fields).Error
}

// UpdateSourceStatus 源状态跃迁回写（active⇄paused / error→active 人工恢复；
// 跃迁合法性由 service 层 KBCrawlerSourceTransitionAllowed 判定）。
func (r *KBCrawlerRepository) UpdateSourceStatus(ctx context.Context, id uint, to string) error {
	return r.db.WithContext(ctx).Model(&model.KBCrawlerSource{}).Where("id = ?", id).
		Updates(map[string]any{"status": to, "error_msg": ""}).Error
}

// SetSourceVerified 站点验证通过回写。
func (r *KBCrawlerRepository) SetSourceVerified(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Model(&model.KBCrawlerSource{}).Where("id = ?", id).
		Updates(map[string]any{"site_verified": true}).Error
}

// DeleteSource 软删源（不删已入库文档）。
func (r *KBCrawlerRepository) DeleteSource(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&model.KBCrawlerSource{}, id).Error
}

// ListSources 列出源。kbID=0 不限知识库；status 为空不限状态（active/paused/error）。
//
// 状态筛选必须落在 SQL 上：前端“状态”下拉此前只是把 status 发上来，
// 服务层忽略后即使筛“已暂停”也照旧返回全部行，看起来像筛选坏了。
func (r *KBCrawlerRepository) ListSources(ctx context.Context, kbID uint, status string) ([]model.KBCrawlerSource, error) {
	var out []model.KBCrawlerSource
	q := r.db.WithContext(ctx).Order("id DESC")
	if kbID > 0 {
		q = q.Where("kb_id = ?", kbID)
	}
	if status = strings.TrimSpace(status); status != "" {
		q = q.Where("status = ?", status)
	}
	err := q.Find(&out).Error
	return out, err
}

// —— 批次（SQL 逐字保留自 service/kb_crawler.go）——

// GetBatchByID 按主键取批次。
func (r *KBCrawlerRepository) GetBatchByID(ctx context.Context, id uint64) (*model.KBCrawlBatch, error) {
	var batch model.KBCrawlBatch
	if err := r.db.WithContext(ctx).First(&batch, id).Error; err != nil {
		return nil, err
	}
	return &batch, nil
}

// SumPagesFoundSince 汇总某日 0 点后各批次发现页数（每租户每日配额 §8.2）。
// 原实现不检查 Scan 错误（错误 ⇒ 按 0 计，配额放行）——错误路径逐字保留，
// 由调用方显式忽略返回的 error。
func (r *KBCrawlerRepository) SumPagesFoundSince(ctx context.Context, dayStart string) (int64, error) {
	var total int64
	err := r.db.WithContext(ctx).Model(&model.KBCrawlBatch{}).
		Where("created_at >= ?", dayStart).
		Select("COALESCE(SUM(pages_found),0)").Scan(&total).Error
	return total, err
}

// CreateBatch 建 pending 批次行。
func (r *KBCrawlerRepository) CreateBatch(ctx context.Context, batch *model.KBCrawlBatch) error {
	return r.db.WithContext(ctx).Create(batch).Error
}

// UpdateBatchFields 回写批次运行字段（状态/计数/起止时间）。
func (r *KBCrawlerRepository) UpdateBatchFields(ctx context.Context, id uint64, fields map[string]any) error {
	return r.db.WithContext(ctx).Model(&model.KBCrawlBatch{}).Where("id = ?", id).Updates(fields).Error
}

// CountFailedBatchesSince 统计近 7 天 failed 批次数（连续失败自动暂停判据）。
// 原实现不检查 Count 错误（错误 ⇒ 按 0 计，不触发自动暂停）——错误路径逐字保留，
// 由调用方显式忽略返回的 error。
func (r *KBCrawlerRepository) CountFailedBatchesSince(ctx context.Context, sourceID uint, since time.Time) (int64, error) {
	var cnt int64
	err := r.db.WithContext(ctx).Model(&model.KBCrawlBatch{}).
		Where("source_id = ? AND status = ? AND created_at >= ?", sourceID, model.KBCrawlBatchStatusFailed, since).
		Count(&cnt).Error
	return cnt, err
}

// ListBatches 批次列表（Count 与 Find 沿原实现同链复用）。
func (r *KBCrawlerRepository) ListBatches(ctx context.Context, sourceID uint, page, pageSize int) ([]model.KBCrawlBatch, int64, error) {
	var out []model.KBCrawlBatch
	var total int64
	q := r.db.WithContext(ctx).Model(&model.KBCrawlBatch{})
	if sourceID > 0 {
		q = q.Where("source_id = ?", sourceID)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&out).Error
	return out, total, err
}

// —— 爬取入库（SQL 逐字保留自 service/kb_crawler.go IngestCrawledPage）——

// FindHTMLDocByTitle 以 (title=URL, file_type=".html") 约定键查已有文档；
// not-found 返回 gorm.ErrRecordNotFound（调用方据此走新建分支）。
func (r *KBCrawlerRepository) FindHTMLDocByTitle(ctx context.Context, pageURL string) (*model.KBDocument, error) {
	var existing model.KBDocument
	err := r.db.WithContext(ctx).Where("title = ? AND file_type = ?", pageURL, ".html").First(&existing).Error
	if err != nil {
		return nil, err
	}
	return &existing, nil
}

// SupersedeActiveChunks 变更重建：旧 chunks 标 superseded。
// 原实现不检查 Update 错误（best-effort 清理）——错误路径逐字保留，
// 由调用方显式忽略返回的 error。
func (r *KBCrawlerRepository) SupersedeActiveChunks(ctx context.Context, documentID uint) error {
	return r.db.WithContext(ctx).Table("kb_document_chunks").
		Where("document_id = ? AND status = ?", documentID, "active").
		Update("status", "superseded").Error
}

// UpdateDocumentFields 回写文档内容/状态（变更重建分支）。
// 原实现不检查 Updates 错误——错误路径逐字保留，由调用方显式忽略。
func (r *KBCrawlerRepository) UpdateDocumentFields(ctx context.Context, id uint, fields map[string]any) error {
	return r.db.WithContext(ctx).Model(&model.KBDocument{}).Where("id = ?", id).Updates(fields).Error
}

// CreateDocument 落新爬取文档。
func (r *KBCrawlerRepository) CreateDocument(ctx context.Context, doc *model.KBDocument) error {
	return r.db.WithContext(ctx).Create(doc).Error
}

// GetDocumentByID 按主键取 kb_documents 行（QA 拆分读正文用）。
func (r *KBCrawlerRepository) GetDocumentByID(ctx context.Context, id uint) (*model.KBDocument, error) {
	var doc model.KBDocument
	if err := r.db.WithContext(ctx).First(&doc, id).Error; err != nil {
		return nil, err
	}
	return &doc, nil
}
