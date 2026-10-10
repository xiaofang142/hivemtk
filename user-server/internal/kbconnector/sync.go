package kbconnector

import (
	"context"
	"crypto/sha256"
	"os"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"hivemtk-user/internal/aiagent/knowledge/service"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/utils/logger"

	"gorm.io/gorm"
)

// Sync 同步一个连接器：列清单 → 逐个下载（跳过已导入同内容）→ 走本地知识库
// 导入管线（分块+向量化异步处理）→ 回写连接器状态与幂等明细。
//
// 复用判定：external_kb_sync_items 以 (connector_id, doc_key) 唯一，content_hash
// 相同且上次成功即跳过（第三方文档未变化），不同则重新导入并翻新明细。
func Sync(ctx context.Context, connectorID uint) (*SyncResult, error) {
	g := db.GetDB()
	if g == nil {
		return nil, fmt.Errorf("db 未就绪")
	}
	var conn model.ExternalKBConnector
	if err := g.WithContext(ctx).First(&conn, connectorID).Error; err != nil {
		return nil, fmt.Errorf("连接器不存在: %w", err)
	}
	setStatus(g, conn.ID, "running", "")

	provider, err := LookupProvider(conn.Type)
	if err != nil {
		markFailed(g, conn.ID, err.Error())
		return nil, err
	}
	cfg := map[string]any{}
	if strings.TrimSpace(conn.Config) != "" {
		if err := json.Unmarshal([]byte(conn.Config), &cfg); err != nil {
			markFailed(g, conn.ID, "config 不是合法 JSON: "+err.Error())
			return nil, err
		}
	}

	docs, err := provider.List(ctx, cfg)
	res := &SyncResult{}
	if err != nil {
		markFailed(g, conn.ID, err.Error())
		return res, err
	}

	kbSvc := service.NewKnowledgeBaseService()
	now := time.Now()

	for _, doc := range docs {
		var prev model.ExternalKBSyncItem
		exists := g.WithContext(ctx).
			Where("connector_id = ? AND doc_key = ?", conn.ID, doc.DocKey).
			First(&prev).Error == nil

		blob, ferr := doc.Fetch(ctx)
		if ferr != nil {
			res.Failed++
			upsertItem(g, conn.ID, doc, "", "failed", truncate(ferr.Error(), 500), 0, now)
			continue
		}
		sum := sha256.Sum256(blob)
		hash := hex.EncodeToString(sum[:])
		if exists && prev.ContentHash == hash && prev.Status == "success" {
			res.Skipped++
			continue
		}

		ext := strings.ToLower(filepath.Ext(doc.Title))
		if ext == "" {
			ext = ".txt" // 无扩展名按纯文本兜底（在导入白名单内）
		}
		tmpDir, _ := os.MkdirTemp("", "kbconn")
		tmpPath := filepath.Join(tmpDir, fmt.Sprintf("%s%s", sanitizeFilename(doc.DocKey), ext))
		if werr := os.WriteFile(tmpPath, blob, 0o644); werr != nil {
			res.Failed++
			upsertItem(g, conn.ID, doc, hash, "failed", truncate(werr.Error(), 500), 0, now)
			continue
		}
		docID, ierr := kbSvc.IngestLocalFile(ctx, doc.Title, tmpPath, ext)
		if ierr != nil {
			res.Failed++
			upsertItem(g, conn.ID, doc, hash, "failed", truncate(ierr.Error(), 500), 0, now)
			continue
		}
		res.Imported++
		upsertItem(g, conn.ID, doc, hash, "success", "", docID, now)
	}

	summary := fmt.Sprintf("共 %d：导入 %d，跳过 %d，失败 %d", len(docs), res.Imported, res.Skipped, res.Failed)
	status := "success"
	if res.Failed > 0 {
		status = "failed"
	}
	setStatus(g, conn.ID, status, summary)
	res.Summary = summary
	logger.Infof("[kbconnector] connector=%d(%s) %s", conn.ID, conn.Type, summary)
	return res, nil
}

// SyncResult 单次同步结果摘要。
type SyncResult struct {
	Imported int    `json:"imported"`
	Skipped  int    `json:"skipped"`
	Failed   int    `json:"failed"`
	Summary  string `json:"summary"`
}

func setStatus(g *gorm.DB, connectorID uint, status, summary string) {
	updates := map[string]any{"last_status": status}
	if summary != "" {
		updates["last_summary"] = summary
	}
	if status == "success" || status == "failed" {
		now := time.Now()
		updates["last_sync_at"] = &now
	}
	_ = g.Model(&model.ExternalKBConnector{}).Where("id = ?", connectorID).Updates(updates).Error
}

func markFailed(g *gorm.DB, connectorID uint, msg string) {
	setStatus(g, connectorID, "failed", truncate(msg, 512))
}

func upsertItem(g *gorm.DB, connID uint, doc ExternalDoc, hash, status, detail string, docID uint, at time.Time) {
	item := model.ExternalKBSyncItem{
		ConnectorID: connID,
		DocKey:      doc.DocKey,
		Title:       doc.Title,
		ContentHash: hash,
		Status:      status,
		Detail:      detail,
		DocumentID:  docID,
		SyncedAt:    at,
	}
	if err := g.Create(&item).Error; err != nil {
		// 唯一索引冲突 = 该 doc_key 已有明细，翻新内容字段
		_ = g.Model(&model.ExternalKBSyncItem{}).
			Where("connector_id = ? AND doc_key = ?", connID, doc.DocKey).
			Updates(map[string]any{
				"title": item.Title, "content_hash": hash, "status": status,
				"detail": detail, "document_id": docID, "synced_at": at,
			}).Error
	}
}

// sanitizeFilename 把 doc_key 清洗成安全文件名主干（保留扩展名由 ext 决定）。
func sanitizeFilename(key string) string {
	b := strings.Builder{}
	for _, r := range key {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
		if b.Len() >= 80 {
			break
		}
	}
	if b.Len() == 0 {
		return "doc"
	}
	return b.String()
}
