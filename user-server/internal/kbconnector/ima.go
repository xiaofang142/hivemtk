package kbconnector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// imaProvider 腾讯 IMA 知识库（OpenAPI）。
//
// 配置：{"client_id":"...","api_key":"...","knowledge_base_id":"...(可选，留空=全部)"}
// 认证：ima-openapi-clientid / ima-openapi-apikey 两个请求头。
// 范围：仅「文件类」条目可导出原文（pdf/word/ppt/xlsx 等）；笔记类内容
// OpenAPI 不提供导出，自动跳过——这是 IMA 平台侧限制，不是本连接器缺陷。
type imaProvider struct{}

func (imaProvider) List(ctx context.Context, cfg map[string]any) ([]ExternalDoc, error) {
	clientID := cfgString(cfg, "client_id")
	apiKey := cfgString(cfg, "api_key", "client_secret")
	if clientID == "" || apiKey == "" {
		return nil, fmt.Errorf("IMA 配置缺少 client_id/api_key")
	}
	kbID := cfgString(cfg, "knowledge_base_id")
	client := &http.Client{Timeout: 30 * time.Second}
	post := func(apiPath string, body any, out any) error {
		blob, _ := json.Marshal(body)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			"https://ima.qq.com/"+apiPath, bytes.NewReader(blob))
		if err != nil {
			return err
		}
		req.Header.Set("ima-openapi-clientid", clientID)
		req.Header.Set("ima-openapi-apikey", apiKey)
		req.Header.Set("Content-Type", "application/json")
		res, err := client.Do(req)
		if err != nil {
			return err
		}
		defer func() { _ = res.Body.Close() }()
		raw, _ := io.ReadAll(io.LimitReader(res.Body, 8<<20))
		if res.StatusCode != http.StatusOK {
			return fmt.Errorf("IMA %s HTTP %d: %s", apiPath, res.StatusCode, truncate(string(raw), 120))
		}
		if out == nil {
			return nil
		}
		return json.Unmarshal(raw, out)
	}

	// 知识库范围：指定 id 只同步该库；留空枚举全部（分页拉全）。
	ids := []string{}
	if kbID != "" {
		ids = append(ids, kbID)
	} else {
		var listResp struct {
			Code int `json:"code"`
			Data struct {
				InfoList []struct {
					KbID string `json:"kb_id"`
				} `json:"info_list"`
				NextCursor string `json:"next_cursor"`
				IsEnd      bool   `json:"is_end"`
			} `json:"data"`
		}
		for cursor := ""; ; {
			body := map[string]any{"query": "", "cursor": cursor, "limit": 20}
			if err := post("openapi/wiki/v1/search_knowledge_base", body, &listResp); err != nil {
				return nil, err
			}
			if listResp.Code != 0 {
				return nil, fmt.Errorf("IMA 列举知识库失败: code=%d", listResp.Code)
			}
			for _, it := range listResp.Data.InfoList {
				ids = append(ids, it.KbID)
			}
			if listResp.Data.IsEnd || listResp.Data.NextCursor == "" {
				break
			}
			cursor = listResp.Data.NextCursor
		}
	}

	docs := []ExternalDoc{}
	for _, id := range ids {
		for cursor := ""; ; {
			var resp struct {
				Code int `json:"code"`
				Data struct {
					KnowledgeList []struct {
						MediaID   string `json:"media_id"`
						Title     string `json:"title"`
						MediaType int    `json:"media_type"`
					} `json:"knowledge_list"`
					NextCursor string `json:"next_cursor"`
					IsEnd      bool   `json:"is_end"`
				} `json:"data"`
			}
			body := map[string]any{"knowledge_base_id": id, "cursor": cursor, "limit": 50}
			if cursor == "" {
				body["folder_id"] = map[string]any{}
			}
			if err := post("openapi/wiki/v1/get_knowledge_list", body, &resp); err != nil {
				return nil, err
			}
			if resp.Code != 0 {
				return nil, fmt.Errorf("IMA 列举内容失败: code=%d", resp.Code)
			}
			for _, it := range resp.Data.KnowledgeList {
				// 笔记(11)/文件夹(99) 跳过：笔记不可导出、文件夹由服务端递归展开
				if it.MediaType == 11 || it.MediaType == 99 {
					continue
				}
				mid, title := it.MediaID, it.Title
				docs = append(docs, ExternalDoc{
					DocKey: mid,
					Title:  title,
					Fetch: func(ctx context.Context) ([]byte, error) {
						return imaFetchFile(ctx, client, clientID, apiKey, mid)
					},
				})
			}
			if resp.Data.IsEnd || resp.Data.NextCursor == "" {
				break
			}
			cursor = resp.Data.NextCursor
		}
	}
	return docs, nil
}

func imaFetchFile(ctx context.Context, client *http.Client, clientID, apiKey, mediaID string) ([]byte, error) {
	blob, _ := json.Marshal(map[string]any{"media_id": mediaID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://ima.qq.com/openapi/wiki/v1/get_media_info", bytes.NewReader(blob))
	if err != nil {
		return nil, err
	}
	req.Header.Set("ima-openapi-clientid", clientID)
	req.Header.Set("ima-openapi-apikey", apiKey)
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	var resp struct {
		Code int `json:"code"`
		Data struct {
			URLInfo struct {
				URL string `json:"url"`
			} `json:"url_info"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, err
	}
	if resp.Code != 0 || strings.TrimSpace(resp.Data.URLInfo.URL) == "" {
		return nil, fmt.Errorf("IMA 取原文失败 code=%d（笔记类内容平台不提供导出）", resp.Code)
	}
	url := resp.Data.URLInfo.URL
	if !strings.HasPrefix(url, "http") {
		return nil, fmt.Errorf("IMA 原文链接不可下载（%s…）", truncate(url, 40))
	}
	dl, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	dlRes, err := client.Do(dl)
	if err != nil {
		return nil, err
	}
	defer func() { _ = dlRes.Body.Close() }()
	return io.ReadAll(io.LimitReader(dlRes.Body, 64<<20))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func init() { RegisterProvider("ima", imaProvider{}) }
