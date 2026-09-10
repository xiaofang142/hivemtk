package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	knowledgemodel "hivemtk-user/internal/aiagent/knowledge/model"
	knowledgesvc "hivemtk-user/internal/aiagent/knowledge/service"
)

// ConnectorPullLimits 拉取上限
const (
	ConnectorPullMaxPagesDefault = 10
	ConnectorPullMaxPagesCap     = 20
	notionAPIRateGap             = 350 * time.Millisecond
)

// ConnectorPuller 连接器拉取函数签名（闭包形式，s 由调用方注入）
type ConnectorPuller func(ctx context.Context, productID string, saved *SaveConnectorRequest, req *ConnectorPullRequest) (*ConnectorPullResult, error)

var kbPullers = map[string]ConnectorPuller{}

func init() {
	RegisterKBPuller("notion", func(ctx context.Context, productID string, saved *SaveConnectorRequest, req *ConnectorPullRequest) (*ConnectorPullResult, error) {
		return globalKBPullService.pullNotion(ctx, productID, saved, req)
	})
}

var globalKBPullService = &KBConnectorService{}

// RegisterKBPuller 注册一个新的连接器拉取实现（供外部扩展）
func RegisterKBPuller(source string, p ConnectorPuller) { kbPullers[source] = p }

type ConnectorPullResult struct {
	Source   string                 `json:"source"`
	Imported int                    `json:"imported"`
	Failed   int                    `json:"failed"`
	Skipped  int                    `json:"skipped"`
	Details  []ConnectorPullPageRow `json:"details"`
}

// ConnectorPullPageRow 单页结果
type ConnectorPullPageRow struct {
	PageID  string `json:"page_id"`
	Title   string `json:"title"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// ConnectorPullRequest 拉取参数
type ConnectorPullRequest struct {
	Query    string `json:"query"`
	MaxPages int    `json:"max_pages"`
}

type kbImportSink interface {
	Import(ctx context.Context, req *knowledgesvc.ImportRequest) (any, error)
}

// Pull 从连接器源拉取内容并导入知识库
func (s *KBConnectorService) Pull(ctx context.Context, source, productID string, req *ConnectorPullRequest) (*ConnectorPullResult, error) {
	if !kbConnectorSources[source] {
		return nil, fmt.Errorf("不支持的连接器: %s", source)
	}
	if strings.TrimSpace(productID) == "" {
		return nil, fmt.Errorf("product_id(KB ID) 不能为空")
	}
	raw, err := s.kv.Get(ctx, connectorKVKey(source))
	if err != nil || raw == "" {
		return nil, fmt.Errorf("连接器 %s 未配置凭据，请先保存凭据并测试连接", source)
	}
	var saved SaveConnectorRequest
	if err := json.Unmarshal([]byte(raw), &saved); err != nil {
		return nil, fmt.Errorf("凭据损坏，请重新保存")
	}
	puller, ok := kbPullers[source]
	if !ok {
		return nil, fmt.Errorf("连接器 %s 的自动拉取尚未实现（已支持的 source: notion；其他可用外部导入推送或 OpenAPI）", source)
	}
	return puller(ctx, productID, &saved, req)
}

func (s *KBConnectorService) pullNotion(ctx context.Context, productID string, saved *SaveConnectorRequest, req *ConnectorPullRequest) (*ConnectorPullResult, error) {
	token, _ := saved.Config["token"].(string)
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("Notion token 缺失，请重新保存凭据")
	}
	maxPages := req.MaxPages
	if maxPages <= 0 {
		maxPages = ConnectorPullMaxPagesDefault
	}
	if maxPages > ConnectorPullMaxPagesCap {
		maxPages = ConnectorPullMaxPagesCap
	}

	pages, err := s.notionSearch(ctx, token, req.Query)
	if err != nil {
		return nil, fmt.Errorf("Notion 搜索失败: %w", err)
	}
	res := &ConnectorPullResult{Source: "notion"}
	importer := knowledgesvc.NewKnowledgeService()

	for i, pg := range pages {
		if i >= maxPages {
			break
		}
		row := ConnectorPullPageRow{PageID: pg.ID, Title: pg.Title}
		text, err := s.notionPageText(ctx, token, pg.ID)
		if err != nil {
			row.Status = "failed"
			row.Message = err.Error()
			res.Failed++
			res.Details = append(res.Details, row)
			continue
		}
		if strings.TrimSpace(text) == "" {
			row.Status = "skipped"
			row.Message = "页面无可提取文本（纯附件/数据库视图）"
			res.Skipped++
			res.Details = append(res.Details, row)
			continue
		}
		_, err = importer.Import(ctx, &knowledgesvc.ImportRequest{
			ProductID:  productID,
			SourceType: knowledgemodel.SourceTypeText,
			Title:      pg.Title,
			Content:    text,
			Operator:   "connector:notion",
			SourceRef:  pg.URL,
			Metadata: map[string]any{
				"connector": "notion",
				"page_id":   pg.ID,
			},
		})
		if err != nil {
			row.Status = "failed"
			row.Message = err.Error()
			res.Failed++
		} else {
			row.Status = "imported"
			res.Imported++
		}
		res.Details = append(res.Details, row)
		time.Sleep(notionAPIRateGap)
	}
	return res, nil
}

type notionPage struct {
	ID    string
	Title string
	URL   string
}

func (s *KBConnectorService) notionSearch(ctx context.Context, token, query string) ([]notionPage, error) {
	body := map[string]any{
		"page_size": ConnectorPullMaxPagesCap,
		"filter":    map[string]any{"property": "object", "value": "page"},
	}
	if query != "" {
		body["query"] = query
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.notion.com/v1/search", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Notion-Version", "2022-06-28")
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.httpCli.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out struct {
		Results []struct {
			ID         string         `json:"id"`
			URL        string         `json:"url"`
			Properties map[string]any `json:"properties"`
		} `json:"results"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, out.Message)
	}
	pages := make([]notionPage, 0, len(out.Results))
	for _, r := range out.Results {
		pages = append(pages, notionPage{
			ID:    r.ID,
			Title: extractNotionTitle(r.Properties),
			URL:   r.URL,
		})
	}
	return pages, nil
}

func (s *KBConnectorService) notionPageText(ctx context.Context, token, pageID string) (string, error) {
	// 分页拉取：Notion blocks 接口单页上限 100，超 100 block 的页面
	// 不翻页会静默丢内容（导入"成功"但知识不完整）
	var sb strings.Builder
	cursor := ""
	const notionPageLimit = 100
	for page := 0; page < 100; page++ { // 100 页硬上限（10000 block）防异常数据拖死循环
		u := "https://api.notion.com/v1/blocks/" + pageID + "/children?page_size=100"
		if cursor != "" {
			u += "&start_cursor=" + cursor
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Notion-Version", "2022-06-28")
		resp, err := s.httpCli.Do(req)
		if err != nil {
			return "", err
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			var e struct {
				Message string `json:"message"`
			}
			_ = json.Unmarshal(raw, &e)
			return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, e.Message)
		}

		var generic struct {
			Results             []map[string]any `json:"results"`
			HasMore             bool             `json:"has_more"`
			NextCursor          string           `json:"next_cursor"`
		}
		if err := json.Unmarshal(raw, &generic); err != nil {
			return "", err
		}
		for _, b := range generic.Results {
			bt, _ := b["type"].(string)
			if bt == "" {
				continue
			}
			obj, ok := b[bt].(map[string]any)
			if !ok {
				continue
			}
			if line := notionRichText(obj["rich_text"]); line != "" {
				sb.WriteString(line)
				sb.WriteString("\n")
			}
		}
		if !generic.HasMore || generic.NextCursor == "" {
			break
		}
		cursor = generic.NextCursor
		_ = notionPageLimit // 单页 page_size=100 固定在 URL 中
	}
	return sb.String(), nil
}

func notionRichText(v any) string {
	arr, ok := v.([]any)
	if !ok {
		return ""
	}
	var sb strings.Builder
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if pt, ok := m["plain_text"].(string); ok {
			sb.WriteString(pt)
		}
	}
	return sb.String()
}

func extractNotionTitle(props map[string]any) string {
	if props == nil {
		return ""
	}

	for _, pv := range props {
		pm, ok := pv.(map[string]any)
		if !ok {
			continue
		}
		if t, _ := pm["type"].(string); t != "title" {
			continue
		}
		if title := notionRichText(pm["title"]); title != "" {
			return title
		}
	}
	return "未命名页面"
}
