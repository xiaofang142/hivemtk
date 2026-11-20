package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrCrawlLeaseLost = errors.New("爬取批次租约已失效")
var ErrCrawlSourceBusy = errors.New("该来源已有执行中的批次")

type CrawlLease struct {
	BatchID uint64
	Token   string
}
type crawlLeaseKey struct{}

func WithCrawlLease(ctx context.Context, lease CrawlLease) context.Context {
	return context.WithValue(ctx, crawlLeaseKey{}, lease)
}

func crawlerTenant(ctx context.Context) (uint, error) {
	id := tenant.WriteTenantID(ctx)
	if id == 0 || tenant.IsSystemScope(ctx) {
		return 0, errors.New("爬取任务缺少租户上下文")
	}
	return id, nil
}

func changedOne(result *gorm.DB) error {
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// ClaimBatch serializes on the source row, including claims made by other processes.
func (r *KBCrawlerRepository) ClaimBatch(ctx context.Context, id uint64, token string, now time.Time) error {
	tenantID, err := crawlerTenant(ctx)
	if err != nil {
		return err
	}
	if token == "" {
		return errors.New("批次租约标识不能为空")
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var batch model.KBCrawlBatch
		if err := tx.Where("tenant_id = ? AND id = ?", tenantID, id).First(&batch).Error; err != nil {
			return err
		}
		var src model.KBCrawlerSource
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id = ?", tenantID, batch.SourceID).First(&src).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id = ?", tenantID, id).First(&batch).Error; err != nil {
			return err
		}
		expired := batch.Status == model.KBCrawlBatchStatusRunning && ((batch.LeaseUntil != nil && !batch.LeaseUntil.After(now)) || (batch.LeaseUntil == nil && batch.StartedAt != nil && !batch.StartedAt.After(now.Add(-35*time.Minute))))
		if batch.Status != model.KBCrawlBatchStatusPending && !expired {
			return ErrCrawlSourceBusy
		}
		var active int64
		if err := tx.Model(&model.KBCrawlBatch{}).Where("tenant_id = ? AND source_id = ? AND id <> ? AND status = ? AND (lease_until > ? OR (lease_until IS NULL AND (started_at IS NULL OR started_at > ?)))", tenantID, src.ID, id, model.KBCrawlBatchStatusRunning, now, now.Add(-35*time.Minute)).Count(&active).Error; err != nil {
			return err
		}
		if active > 0 {
			return ErrCrawlSourceBusy
		}
		if !src.SiteVerified || src.Status != model.KBCrawlerSourceStatusActive {
			return errors.New("来源未验证或已停用")
		}
		return changedOne(tx.Model(&model.KBCrawlBatch{}).Where("tenant_id = ? AND id = ?", tenantID, id).Updates(map[string]any{"status": model.KBCrawlBatchStatusRunning, "started_at": now, "finished_at": nil, "lease_token": token, "lease_until": now.Add(35 * time.Minute), "error_msg": ""}))
	})
}

func checkCrawlLease(tx *gorm.DB, ctx context.Context, tenantID, sourceID uint) error {
	lease, ok := ctx.Value(crawlLeaseKey{}).(CrawlLease)
	if !ok {
		return nil
	} // Direct synchronous ingestion does not own a batch.
	var batch model.KBCrawlBatch
	if err := tx.Where("tenant_id = ? AND id = ?", tenantID, lease.BatchID).First(&batch).Error; err != nil {
		return errors.Join(ErrCrawlLeaseLost, err)
	}
	if sourceID != 0 && batch.SourceID != sourceID {
		return ErrCrawlLeaseLost
	}
	var source model.KBCrawlerSource
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id = ?", tenantID, batch.SourceID).First(&source).Error; err != nil {
		return err
	}
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id = ? AND status = ? AND lease_token = ? AND lease_until > ?", tenantID, lease.BatchID, model.KBCrawlBatchStatusRunning, lease.Token, time.Now()).First(&batch).Error; err != nil {
		return errors.Join(ErrCrawlLeaseLost, err)
	}
	return nil
}

// FinishBatch and its source summary either both commit or both remain recoverable.
func (r *KBCrawlerRepository) FinishBatch(ctx context.Context, id uint64, batchFields, sourceFields map[string]any) error {
	tenantID, err := crawlerTenant(ctx)
	if err != nil {
		return err
	}
	lease, leased := ctx.Value(crawlLeaseKey{}).(CrawlLease)
	if !leased || lease.BatchID != id {
		return ErrCrawlLeaseLost
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := checkCrawlLease(tx, ctx, tenantID, 0); err != nil {
			return err
		}
		var batch model.KBCrawlBatch
		if err := tx.Where("tenant_id = ? AND id = ?", tenantID, id).First(&batch).Error; err != nil {
			return err
		}
		if err := changedOne(tx.Model(&model.KBCrawlerSource{}).Where("tenant_id = ? AND id = ?", tenantID, batch.SourceID).Updates(sourceFields)); err != nil {
			return err
		}
		batchFields["lease_token"] = ""
		batchFields["lease_until"] = nil
		return changedOne(tx.Model(&model.KBCrawlBatch{}).Where("tenant_id = ? AND id = ?", tenantID, id).Updates(batchFields))
	})
}

func (r *KBCrawlerRepository) RecoverableBatches(ctx context.Context, now time.Time) ([]model.KBCrawlBatch, error) {
	tenantID, err := crawlerTenant(ctx)
	if err != nil {
		return nil, err
	}
	var batches []model.KBCrawlBatch
	err = r.db.WithContext(ctx).Where("tenant_id = ? AND (status = ? OR (status = ? AND (lease_until <= ? OR (lease_until IS NULL AND started_at <= ?))))", tenantID, model.KBCrawlBatchStatusPending, model.KBCrawlBatchStatusRunning, now, now.Add(-35*time.Minute)).Order("id").Limit(100).Find(&batches).Error
	return batches, err
}

// PrepareCrawlPage atomically binds the source, document and content version.
func (r *KBCrawlerRepository) PrepareCrawlPage(ctx context.Context, src *model.KBCrawlerSource, pageURL, content, hash string) (*model.KBCrawlPage, bool, error) {
	tenantID, err := crawlerTenant(ctx)
	if err != nil {
		return nil, false, err
	}
	if src.TenantID != tenantID {
		return nil, false, errors.New("来源租户不匹配")
	}
	digest := sha256.Sum256([]byte(pageURL))
	var page model.KBCrawlPage
	isNew := false
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := checkCrawlLease(tx, ctx, tenantID, src.ID); err != nil {
			return err
		}
		var source model.KBCrawlerSource
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id = ?", tenantID, src.ID).First(&source).Error; err != nil {
			return err
		}
		if source.KBID != src.KBID {
			return errors.New("来源知识库已改变")
		}
		lookup := tx.Where("tenant_id = ? AND source_id = ? AND url_hash = ?", tenantID, src.ID, hex.EncodeToString(digest[:])).First(&page).Error
		if lookup != nil && !errors.Is(lookup, gorm.ErrRecordNotFound) {
			return lookup
		}
		if errors.Is(lookup, gorm.ErrRecordNotFound) {
			doc := model.KBDocument{TenantID: tenantID, Title: pageURL, Content: content, FileType: ".html", FileSize: int64(len(content)), Status: model.KBDocumentStatusPending}
			if err := tx.Create(&doc).Error; err != nil {
				return err
			}
			page = model.KBCrawlPage{TenantID: tenantID, SourceID: src.ID, URLHash: hex.EncodeToString(digest[:]), URL: pageURL, KBID: src.KBID, DocumentID: doc.ID, ContentHash: hash}
			isNew = true
			return tx.Create(&page).Error
		}
		if page.KBID != src.KBID {
			return errors.New("页面知识库不匹配")
		}
		if page.ContentHash != hash {
			if err := tx.Table("kb_document_chunks").Where("tenant_id = ? AND document_id = ? AND status = ?", tenantID, page.DocumentID, "active").Update("status", "superseded").Error; err != nil {
				return err
			}
		}
		if err := changedOne(tx.Model(&model.KBDocument{}).Where("tenant_id = ? AND id = ?", tenantID, page.DocumentID).Updates(map[string]any{"content": content, "file_size": len(content), "status": model.KBDocumentStatusPending, "error_msg": ""})); err != nil {
			return err
		}
		page.ContentHash = hash
		return changedOne(tx.Model(&model.KBCrawlPage{}).Where("tenant_id = ? AND id = ?", tenantID, page.ID).Updates(map[string]any{"content_hash": hash, "completed_hash": "", "error_msg": ""}))
	})
	return &page, isNew, err
}

func (r *KBCrawlerRepository) CompleteCrawlPage(ctx context.Context, page *model.KBCrawlPage, qaDone bool, candidates []*model.QACandidate) error {
	tenantID, err := crawlerTenant(ctx)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := checkCrawlLease(tx, ctx, tenantID, page.SourceID); err != nil {
			return err
		}
		var current model.KBCrawlPage
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id = ? AND content_hash = ?", tenantID, page.ID, page.ContentHash).First(&current).Error; err != nil {
			return err
		}
		fields := map[string]any{"completed_hash": page.ContentHash, "error_msg": ""}
		if qaDone && current.QAHash != page.ContentHash {
			for _, candidate := range candidates {
				if candidate.TenantID != tenantID || candidate.KBID != current.KBID {
					return errors.New("候选租户或知识库不匹配")
				}
			}
			if len(candidates) > 0 {
				if err := tx.Create(&candidates).Error; err != nil {
					return err
				}
			}
			fields["qa_hash"] = page.ContentHash
		}
		return changedOne(tx.Model(&model.KBCrawlPage{}).Where("tenant_id = ? AND id = ?", tenantID, page.ID).Updates(fields))
	})
}

func (r *KBCrawlerRepository) FailCrawlPage(ctx context.Context, page *model.KBCrawlPage, cause error) error {
	tenantID, err := crawlerTenant(ctx)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := checkCrawlLease(tx, ctx, tenantID, page.SourceID); err != nil {
			return err
		}
		errMsg := "爬取页面处理失败"
		if cause != nil {
			errMsg = cause.Error()
		}
		if err := changedOne(tx.Model(&model.KBDocument{}).
			Where("tenant_id = ? AND id = ?", tenantID, page.DocumentID).
			Updates(map[string]any{
				"status":    model.KBDocumentStatusFailed,
				"error_msg": errMsg,
			})); err != nil {
			return err
		}
		return changedOne(tx.Model(&model.KBCrawlPage{}).
			Where("tenant_id = ? AND id = ? AND content_hash = ?", tenantID, page.ID, page.ContentHash).
			Updates(map[string]any{"completed_hash": "", "error_msg": errMsg}))
	})
}

// HasCompletedCrawlPage verifies durable state before reusing an in-process index.
func (r *KBCrawlerRepository) HasCompletedCrawlPage(ctx context.Context, src *model.KBCrawlerSource, pageURL, hash string, qaEnabled bool) (bool, error) {
	tenantID, err := crawlerTenant(ctx)
	if err != nil {
		return false, err
	}
	if src.TenantID != tenantID {
		return false, errors.New("来源租户不匹配")
	}
	digest := sha256.Sum256([]byte(pageURL))
	complete := false
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := checkCrawlLease(tx, ctx, tenantID, src.ID); err != nil {
			return err
		}
		var page model.KBCrawlPage
		err := tx.Where("tenant_id = ? AND source_id = ? AND url_hash = ?", tenantID, src.ID, hex.EncodeToString(digest[:])).First(&page).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if page.KBID != src.KBID || page.ContentHash != hash || page.CompletedHash != hash || (qaEnabled && page.QAHash != hash) {
			return nil
		}
		var doc model.KBDocument
		if err := tx.Where("tenant_id = ? AND id = ?", tenantID, page.DocumentID).First(&doc).Error; err != nil {
			return err
		}
		complete = doc.Status == model.KBDocumentStatusIndexed
		return nil
	})
	return complete, err
}
