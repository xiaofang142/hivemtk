package service

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"strings"
	"time"

	"hivemtk-user/internal/aiagent/knowledge/model"

	"github.com/google/uuid"
)

// BatchImportItem 批量导入的单条记录
type BatchImportItem struct {
	Title    string   `json:"title"`
	Content  string   `json:"content"`
	Category string   `json:"category"`
	Tags     []string `json:"tags"`
	Source   string   `json:"source"`
}

// BatchImportRequest 批量导入请求
type BatchImportRequest struct {
	ProductID string                `json:"product_id"`
	Operator  string                `json:"operator"`
	Format    string                `json:"format"`
	Items     []BatchImportItem     `json:"items,omitempty"`
	File      multipart.File        `json:"-"`
	FileHead  *multipart.FileHeader `json:"-"`
}

// BatchImportResult 批量导入结果
type BatchImportResult struct {
	BatchNo     string   `json:"batch_no"`
	Total       int      `json:"total"`
	Accepted    int      `json:"accepted"`
	Rejected    int      `json:"rejected"`
	DocumentIDs []uint64 `json:"document_ids"`
	Errors      []string `json:"errors"`
}

// BatchImport 批量导入（统一入口，items 或 file 二选一）
func (s *KnowledgeMerchantService) BatchImport(ctx context.Context, req *BatchImportRequest) (*BatchImportResult, error) {
	if req.ProductID == "" {
		return nil, errors.New("product_id 不能为空")
	}
	batchNo := "BATCH-" + time.Now().Format("20060102150405") + "-" + uuid.New().String()[:8]

	product, err := s.prodRepo.GetRagProductByID(ctx, req.ProductID)
	if err != nil || product == nil {
		return nil, errors.New("产品不存在")
	}
	productNumericID := req.ProductID

	items := req.Items
	if len(items) == 0 && req.File != nil {
		parsed, ferr := s.parseBatchFile(ctx, req.File, req.FileHead, req.Format)
		if ferr != nil {
			return nil, ferr
		}
		items = parsed
	}
	if len(items) == 0 {
		return &BatchImportResult{BatchNo: batchNo, Total: 0}, nil
	}

	result := &BatchImportResult{
		BatchNo:     batchNo,
		Total:       len(items),
		DocumentIDs: make([]uint64, 0),
		Errors:      make([]string, 0),
	}

	for idx, it := range items {
		if strings.TrimSpace(it.Content) == "" {
			result.Rejected++
			result.Errors = append(result.Errors, fmt.Sprintf("第 %d 行: 内容为空", idx+1))
			continue
		}
		title := it.Title
		if title == "" {
			title = fmt.Sprintf("批量导入_%d", idx+1)
		}
		imp, err := s.kbService.Import(ctx, &ImportRequest{
			ProductID:  req.ProductID,
			SourceType: model.SourceTypeBatch,
			Title:      title,
			Content:    it.Content,
			Category:   it.Category,
			Tags:       it.Tags,
			Operator:   req.Operator,
			BatchNo:    batchNo,
		})
		if err != nil {
			result.Rejected++
			result.Errors = append(result.Errors, fmt.Sprintf("第 %d 行: %s", idx+1, err.Error()))
			continue
		}
		result.Accepted++
		result.DocumentIDs = append(result.DocumentIDs, imp.DocumentID)
		_ = productNumericID
	}
	return result, nil
}

func (s *KnowledgeMerchantService) parseBatchFile(ctx context.Context, file multipart.File, header *multipart.FileHeader, format string) ([]BatchImportItem, error) {
	if file == nil {
		return nil, errors.New("文件不能为空")
	}
	defer func() { _ = file.Close() }()
	raw, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}
	if format == "" {
		format = "auto"
	}
	if format == "auto" && header != nil {
		name := strings.ToLower(header.Filename)
		switch {
		case strings.HasSuffix(name, ".json"):
			format = "json"
		case strings.HasSuffix(name, ".csv"):
			format = "csv"
		default:
			format = "json"
		}
	}
	switch format {
	case "csv":
		return parseCSV(raw)
	case "json":
		return parseJSON(raw)
	default:
		return nil, errors.New("不支持的格式: " + format)
	}
}

func parseCSV(data []byte) ([]BatchImportItem, error) {
	r := csv.NewReader(strings.NewReader(string(data)))
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("CSV 解析失败: %w", err)
	}
	if len(rows) < 2 {
		return nil, errors.New("CSV 必须至少包含表头和一行数据")
	}
	header := rows[0]
	idxTitle := -1
	idxContent := -1
	idxCategory := -1
	idxTags := -1
	for i, h := range header {
		switch strings.ToLower(strings.TrimSpace(h)) {
		case "title", "标题", "name":
			idxTitle = i
		case "content", "内容", "text", "body", "q", "question":
			idxContent = i
		case "category", "分类":
			idxCategory = i
		case "tags", "标签":
			idxTags = i
		}
	}
	if idxContent < 0 {
		return nil, errors.New("CSV 缺少 content 列")
	}
	items := make([]BatchImportItem, 0, len(rows)-1)
	for i, row := range rows[1:] {
		if idxContent >= len(row) {
			continue
		}
		it := BatchImportItem{
			Content: row[idxContent],
		}
		if idxTitle >= 0 && idxTitle < len(row) {
			it.Title = row[idxTitle]
		}
		if idxCategory >= 0 && idxCategory < len(row) {
			it.Category = row[idxCategory]
		}
		if idxTags >= 0 && idxTags < len(row) {
			tags := strings.Split(row[idxTags], ",")
			for j := range tags {
				tags[j] = strings.TrimSpace(tags[j])
			}
			it.Tags = tags
		}
		items = append(items, it)
		_ = i
	}
	return items, nil
}

// ParseCSV 公开版 parseCSV(供跨包测试使用)
func ParseCSV(data []byte) ([]BatchImportItem, error) {
	return parseCSV(data)
}

func parseJSON(data []byte) ([]BatchImportItem, error) {

	var arr []BatchImportItem
	if err := json.Unmarshal(data, &arr); err == nil {
		return arr, nil
	}
	var wrap struct {
		Items     []BatchImportItem `json:"items"`
		Data      []BatchImportItem `json:"data"`
		List      []BatchImportItem `json:"list"`
		Documents []BatchImportItem `json:"documents"`
	}
	if err := json.Unmarshal(data, &wrap); err != nil {
		return nil, fmt.Errorf("JSON 解析失败: %w", err)
	}
	if len(wrap.Items) > 0 {
		return wrap.Items, nil
	}
	if len(wrap.Data) > 0 {
		return wrap.Data, nil
	}
	if len(wrap.List) > 0 {
		return wrap.List, nil
	}
	if len(wrap.Documents) > 0 {
		return wrap.Documents, nil
	}
	return nil, errors.New("JSON 数据为空或结构不匹配")
}

// ParseJSON 公开版 parseJSON(供跨包测试使用)
func ParseJSON(data []byte) ([]BatchImportItem, error) {
	return parseJSON(data)
}
