package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"hivemtk-user/internal/aiagent/knowledge/model"
	"hivemtk-user/internal/aiagent/knowledge/repository"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"

	"github.com/google/uuid"
)

// ExternalImportRequest 外部导入请求
type ExternalImportRequest struct {
	Source       string            `json:"source"`
	ProductID    string            `json:"product_id"`
	Token        string            `json:"-"`
	Items        []BatchImportItem `json:"items"`
	FeishuDocID  string            `json:"feishu_doc_id,omitempty"`
	NotionPageID string            `json:"notion_page_id,omitempty"`
	Operator     string            `json:"operator"`
	Sync         bool              `json:"sync"`
}

// ExternalImportResponse 外部导入响应
type ExternalImportResponse struct {
	JobNo       string   `json:"job_no"`
	Status      string   `json:"status"`
	Total       int      `json:"total"`
	Accepted    int      `json:"accepted"`
	Rejected    int      `json:"rejected"`
	FailedItems int      `json:"failed_items"`
	DocumentIDs []uint64 `json:"document_ids,omitempty"`
	Errors      []string `json:"errors,omitempty"`
	Async       bool     `json:"async"`
}

// ExternalImport 外部系统导入（统一入口）
func (s *KnowledgeMerchantService) ExternalImport(ctx context.Context, req *ExternalImportRequest) (*ExternalImportResponse, error) {
	if req.Source == "" {
		return nil, errors.New("source 不能为空")
	}
	if req.ProductID == "" {
		return nil, errors.New("product_id 不能为空")
	}

	tok, err := s.ValidateToken(ctx, req.Token)
	if err != nil {
		return nil, err
	}
	if !tokenHasScope(tok.Scopes, "write") {
		return nil, fmt.Errorf("%w: token 缺少 write 权限", utils.ErrForbidden)
	}

	if tok.ProductID != "" && tok.ProductID != "*" && tok.ProductID != req.ProductID {
		return nil, fmt.Errorf("%w: token 无权操作产品 %s（授权范围: %s）", utils.ErrForbidden, req.ProductID, tok.ProductID)
	}

	if _, err := s.prodRepo.GetRagProductByID(ctx, req.ProductID); err != nil {
		return nil, err
	}

	items := req.Items
	if len(items) == 0 {
		switch req.Source {
		case "feishu":
			if req.FeishuDocID == "" {
				return nil, errors.New("飞书模式需要 feishu_doc_id")
			}
			fetched, ferr := s.fetchFeishu(ctx, req.FeishuDocID)
			if ferr != nil {
				return nil, ferr
			}
			items = fetched
		case "notion":
			if req.NotionPageID == "" {
				return nil, errors.New("Notion 模式需要 notion_page_id")
			}
			fetched, ferr := s.fetchNotion(ctx, req.NotionPageID, tok)
			if ferr != nil {
				return nil, ferr
			}
			items = fetched
		default:
			return nil, errors.New("未提供 items，且 source 不支持自动抓取")
		}
	}

	jobNo := "EXT-" + time.Now().Format("20060102150405") + "-" + uuid.New().String()[:8]
	if !req.Sync {
		s.ensureReposFromDB()
		job := &model.ExternalImportJob{
			JobNo:      jobNo,
			ProductID:  req.ProductID,
			Source:     req.Source,
			TotalItems: len(items),
			Status:     "pending",
			Operator:   req.Operator,
		}
		payload, _ := json.Marshal(req)
		job.Payload = string(payload)
		_ = s.externalRepo.Create(ctx, job)
		go func(productID string, items []BatchImportItem, op string) {
			// SafeGo：裸 go func 中 panic 会击穿进程；失败不得标 completed
			defer func() {
				if r := recover(); r != nil {
					logger.Errorf("[ExternalImport] panic recovered job_no=%s panic=%v", jobNo, r)
					failedAt := time.Now()
					_ = s.externalRepo.UpdateStatusByJobNo(context.Background(), jobNo, map[string]any{
						"status":       "failed",
						"finished_at":  &failedAt,
						"error_detail": fmt.Sprintf("panic: %v", r),
					})
				}
			}()
			bg, bgCancel := context.WithTimeout(context.Background(), ExternalImportTimeout())
			defer bgCancel()
			started := time.Now()
			now := time.Now()
			_ = s.externalRepo.UpdateStatusByJobNo(bg, jobNo, map[string]any{
				"status":     "running",
				"started_at": &now,
			})
			resp, runErr := s.runExternalImport(bg, productID, items, op, jobNo)
			finished := time.Now()
			if runErr != nil || resp == nil {
				errDetail := ""
				if runErr != nil {
					errDetail = runErr.Error()
				}
				_ = s.externalRepo.UpdateStatusByJobNo(bg, jobNo, map[string]any{
					"status":       "failed",
					"finished_at":  &finished,
					"error_detail": errDetail,
				})
				return
			}
			updates := map[string]any{
				"status":       "completed",
				"finished_at":  &finished,
				"done_items":   resp.Accepted,
				"failed_items": resp.FailedItems,
			}
			if len(resp.Errors) > 0 {
				ed, _ := json.Marshal(resp.Errors)
				updates["error_detail"] = string(ed)
			}
			_ = s.externalRepo.UpdateStatusByJobNo(bg, jobNo, updates)
			_ = started
		}(req.ProductID, items, req.Operator)
		return &ExternalImportResponse{
			JobNo:  jobNo,
			Status: "pending",
			Total:  len(items),
			Async:  true,
		}, nil
	}

	return s.runExternalImport(ctx, req.ProductID, items, req.Operator, jobNo)
}

func (s *KnowledgeMerchantService) runExternalImport(ctx context.Context, productID string, items []BatchImportItem, operator, jobNo string) (*ExternalImportResponse, error) {
	resp := &ExternalImportResponse{
		JobNo:       jobNo,
		Status:      "running",
		Total:       len(items),
		DocumentIDs: make([]uint64, 0),
		Errors:      make([]string, 0),
	}
	for idx, it := range items {
		if strings.TrimSpace(it.Content) == "" {
			resp.Rejected++
			resp.FailedItems++
			resp.Errors = append(resp.Errors, fmt.Sprintf("第 %d 项: 内容为空", idx+1))
			continue
		}
		title := it.Title
		if title == "" {
			title = fmt.Sprintf("外部导入_%s_%d", jobNo, idx+1)
		}
		imp, err := s.kbService.Import(ctx, &ImportRequest{
			ProductID:  productID,
			SourceType: model.SourceTypeBatch,
			Title:      title,
			Content:    it.Content,
			Category:   it.Category,
			Tags:       it.Tags,
			Operator:   operator,
			BatchNo:    jobNo,
		})
		if err != nil {
			resp.Rejected++
			resp.FailedItems++
			resp.Errors = append(resp.Errors, fmt.Sprintf("第 %d 项: %s", idx+1, err.Error()))
			continue
		}
		resp.Accepted++
		resp.DocumentIDs = append(resp.DocumentIDs, imp.DocumentID)
	}
	resp.Status = "completed"
	return resp, nil
}

// ListExternalJobs 列出外部导入任务
func (s *KnowledgeMerchantService) ListExternalJobs(ctx context.Context, productID string, page, pageSize int) ([]model.ExternalImportJob, int64, error) {
	s.ensureReposFromDB()
	return s.externalRepo.List(ctx, repository.ExternalJobListFilter{
		ProductID: productID,
		Page:      page,
		PageSize:  pageSize,
	})
}

func (s *KnowledgeMerchantService) fetchFeishu(ctx context.Context, docID string) ([]BatchImportItem, error) {
	if docID == "" {
		return nil, errors.New("飞书 docID 不能为空")
	}

	appID := os.Getenv("FEISHU_APP_ID")
	appSecret := os.Getenv("FEISHU_APP_SECRET")
	if appID == "" || appSecret == "" {
		return nil, errors.New("飞书抓取未配置凭证 (FEISHU_APP_ID/FEISHU_APP_SECRET)，请通过 items 字段直接传入结构化数据")
	}

	client := &http.Client{Timeout: 15 * time.Second}

	form := url.Values{}
	form.Set("app_id", appID)
	form.Set("app_secret", appSecret)
	tokenReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://open.feishu.cn/open-apis/auth/v3/tenant_access_token/internal",
		strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("构建飞书 token 请求失败: %w", err)
	}
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	tokenResp, err := client.Do(tokenReq)
	if err != nil {
		return nil, fmt.Errorf("飞书 token 请求失败: %w", err)
	}
	defer tokenResp.Body.Close()
	var tokenBody struct {
		Code              int    `json:"code"`
		Msg               string `json:"msg"`
		TenantAccessToken string `json:"tenant_access_token"`
		Expire            int    `json:"expire"`
	}
	if err := json.NewDecoder(tokenResp.Body).Decode(&tokenBody); err != nil {
		return nil, fmt.Errorf("解析飞书 token 响应失败: %w", err)
	}
	if tokenBody.Code != 0 || tokenBody.TenantAccessToken == "" {
		return nil, fmt.Errorf("飞书鉴权失败: code=%d msg=%s", tokenBody.Code, tokenBody.Msg)
	}

	docURL := fmt.Sprintf("https://open.feishu.cn/open-apis/docx/v1/documents/%s/raw_content", docID)
	docReq, err := http.NewRequestWithContext(ctx, http.MethodGet, docURL, nil)
	if err != nil {
		return nil, fmt.Errorf("构建飞书文档请求失败: %w", err)
	}
	docReq.Header.Set("Authorization", "Bearer "+tokenBody.TenantAccessToken)
	docResp, err := client.Do(docReq)
	if err != nil {
		return nil, fmt.Errorf("飞书文档请求失败: %w", err)
	}
	defer docResp.Body.Close()
	if docResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(docResp.Body)
		return nil, fmt.Errorf("飞书文档拉取失败: status=%d body=%s", docResp.StatusCode, string(body))
	}
	var docBody struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Content string `json:"content"`
		} `json:"data"`
	}
	if err := json.NewDecoder(docResp.Body).Decode(&docBody); err != nil {
		return nil, fmt.Errorf("解析飞书文档响应失败: %w", err)
	}
	if docBody.Code != 0 {
		return nil, fmt.Errorf("飞书文档错误: code=%d msg=%s", docBody.Code, docBody.Msg)
	}
	markdown := docBody.Data.Content
	if strings.TrimSpace(markdown) == "" {
		return nil, errors.New("飞书文档内容为空")
	}

	items := splitMarkdownToItems(markdown, docID, "feishu")
	return items, nil
}

func (s *KnowledgeMerchantService) fetchNotion(ctx context.Context, pageID string, tok *model.KnowledgeAPIToken) ([]BatchImportItem, error) {
	if pageID == "" {
		return nil, errors.New("Notion pageID 不能为空")
	}
	_ = tok

	apiKey := os.Getenv("NOTION_API_KEY")
	if apiKey == "" {
		return nil, errors.New("Notion 抓取未配置凭证 (NOTION_API_KEY)，请通过 items 字段直接传入结构化数据")
	}

	client := &http.Client{Timeout: 15 * time.Second}
	items, err := s.fetchNotionBlocksRecursive(ctx, client, apiKey, pageID, 0, 8)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, errors.New("Notion 页面内容为空或全为非文本块")
	}
	return items, nil
}

func (s *KnowledgeMerchantService) fetchNotionBlocksRecursive(ctx context.Context, client *http.Client, apiKey, blockID string, depth, maxDepth int) ([]BatchImportItem, error) {
	if depth > maxDepth {
		return nil, nil
	}
	u := fmt.Sprintf("https://api.notion.com/v1/blocks/%s/children?page_size=100", blockID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("构建 Notion 请求失败: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Notion-Version", "2022-06-28")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Notion 拉取失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Notion 拉取失败: status=%d body=%s", resp.StatusCode, string(body))
	}
	var nb struct {
		Results []struct {
			ID          string `json:"id"`
			Type        string `json:"type"`
			HasChildren bool   `json:"has_children"`
			Heading1    *struct {
				RichText []struct {
					PlainText string `json:"plain_text"`
				} `json:"rich_text"`
			} `json:"heading_1"`
			Heading2 *struct {
				RichText []struct {
					PlainText string `json:"plain_text"`
				} `json:"rich_text"`
			} `json:"heading_2"`
			Heading3 *struct {
				RichText []struct {
					PlainText string `json:"plain_text"`
				} `json:"rich_text"`
			} `json:"heading_3"`
			Paragraph *struct {
				RichText []struct {
					PlainText string `json:"plain_text"`
				} `json:"rich_text"`
			} `json:"paragraph"`
			BulletedListItem *struct {
				RichText []struct {
					PlainText string `json:"plain_text"`
				} `json:"rich_text"`
			} `json:"bulleted_list_item"`
		} `json:"results"`
		HasMore    bool   `json:"has_more"`
		NextCursor string `json:"next_cursor"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&nb); err != nil {
		return nil, fmt.Errorf("解析 Notion 响应失败: %w", err)
	}

	var items []BatchImportItem
	var currentTitle string
	var currentBuf strings.Builder

	flush := func() {
		body := strings.TrimSpace(currentBuf.String())
		if body != "" {
			items = append(items, BatchImportItem{
				Title:   currentTitle,
				Content: body,
				Source:  "notion:" + blockID,
			})
		}
		currentBuf.Reset()
	}

	extractText := func(rt []struct {
		PlainText string `json:"plain_text"`
	}) string {
		parts := make([]string, 0, len(rt))
		for _, r := range rt {
			parts = append(parts, r.PlainText)
		}
		return strings.Join(parts, "")
	}

	for _, b := range nb.Results {
		var text string
		var isSectionBoundary bool
		switch b.Type {
		case "heading_1":
			if b.Heading1 != nil {
				text = extractText(b.Heading1.RichText)
				isSectionBoundary = true
			}
		case "heading_2":
			if b.Heading2 != nil {
				text = extractText(b.Heading2.RichText)
				isSectionBoundary = true
			}
		case "heading_3":
			if b.Heading3 != nil {
				text = extractText(b.Heading3.RichText)
				currentBuf.WriteString("### ")
				currentBuf.WriteString(text)
				currentBuf.WriteString("\n")
			}
		case "paragraph":
			if b.Paragraph != nil {
				text = extractText(b.Paragraph.RichText)
				currentBuf.WriteString(text)
				currentBuf.WriteString("\n\n")
			}
		case "bulleted_list_item":
			if b.BulletedListItem != nil {
				text = extractText(b.BulletedListItem.RichText)
				currentBuf.WriteString("- ")
				currentBuf.WriteString(text)
				currentBuf.WriteString("\n")
			}
		}
		_ = text

		if isSectionBoundary {
			flush()
			currentTitle = strings.TrimSpace(text)
			if currentTitle == "" {
				currentTitle = "Untitled"
			}
		}

		if b.HasChildren {
			childItems, err := s.fetchNotionBlocksRecursive(ctx, client, apiKey, b.ID, depth+1, maxDepth)
			if err != nil {
				return nil, err
			}
			for _, ci := range childItems {
				currentBuf.WriteString(ci.Content)
				currentBuf.WriteString("\n")
			}
		}
	}
	flush()
	return items, nil
}

func splitMarkdownToItems(markdown, sourceID, source string) []BatchImportItem {
	lines := strings.Split(markdown, "\n")
	var items []BatchImportItem
	var currentTitle = "Main"
	var currentBuf strings.Builder

	flush := func() {
		body := strings.TrimSpace(currentBuf.String())
		if body == "" {
			return
		}

		const maxLen = 2000
		if len([]rune(body)) > maxLen {
			chunks := softSplitParagraphs(body, maxLen)
			for i, c := range chunks {
				items = append(items, BatchImportItem{
					Title:   fmt.Sprintf("%s (part %d)", currentTitle, i+1),
					Content: c,
					Source:  fmt.Sprintf("%s:%s", source, sourceID),
				})
			}
		} else {
			items = append(items, BatchImportItem{
				Title:   currentTitle,
				Content: body,
				Source:  fmt.Sprintf("%s:%s", source, sourceID),
			})
		}
		currentBuf.Reset()
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			flush()
			currentTitle = strings.TrimSpace(strings.TrimPrefix(trimmed, "## "))
			continue
		}
		currentBuf.WriteString(line)
		currentBuf.WriteString("\n")
	}
	flush()
	return items
}

func softSplitParagraphs(body string, maxLen int) []string {
	paragraphs := strings.Split(body, "\n\n")
	var chunks []string
	var buf strings.Builder
	for _, p := range paragraphs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if len([]rune(p)) > maxLen {
			if buf.Len() > 0 {
				chunks = append(chunks, strings.TrimSpace(buf.String()))
				buf.Reset()
			}
			runes := []rune(p)
			for i := 0; i < len(runes); i += maxLen {
				end := i + maxLen
				if end > len(runes) {
					end = len(runes)
				}
				chunks = append(chunks, string(runes[i:end]))
			}
			continue
		}
		if buf.Len()+len(p)+2 > maxLen {
			chunks = append(chunks, strings.TrimSpace(buf.String()))
			buf.Reset()
		}
		buf.WriteString(p)
		buf.WriteString("\n\n")
	}
	if buf.Len() > 0 {
		chunks = append(chunks, strings.TrimSpace(buf.String()))
	}
	return chunks
}
