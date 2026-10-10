package kbconnector

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// webhttpProvider 通用 HTTP 清单连接器：任何能提供一个 JSON 清单端点的
// 知识库系统都能接入（自建 wiki 导出、对象存储索引、n8n/dify 导出桥等）。
//
// 配置：{"manifest_url":"https://.../manifest.json","headers":{"Authorization":"Bearer x"}}
// 清单格式：{"docs":[{"title":"...","url":"https://.../a.pdf","key":"可选稳定键"}]}
// 也接受顶层数组。url 必须 http(s)，文件类型需在本地导入白名单内（pdf/docx/doc/txt/md）。
type webhttpProvider struct{}

type webhttpDoc struct {
	Title string `json:"title"`
	URL   string `json:"url"`
	Key   string `json:"key"`
}

func (webhttpProvider) List(ctx context.Context, cfg map[string]any) ([]ExternalDoc, error) {
	manifestURL := cfgString(cfg, "manifest_url", "url")
	if manifestURL == "" {
		return nil, fmt.Errorf("webhttp 配置缺少 manifest_url")
	}
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return nil, err
	}
	if headers, ok := cfg["headers"].(map[string]any); ok {
		for k, v := range headers {
			if sv, ok := v.(string); ok {
				req.Header.Set(k, sv)
			}
		}
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("清单 HTTP %d: %s", res.StatusCode, truncate(string(raw), 120))
	}

	var docs []webhttpDoc
	if err := json.Unmarshal(raw, &struct {
		Docs *[]webhttpDoc `json:"docs"`
	}{Docs: &docs}); err != nil {
		if err2 := json.Unmarshal(raw, &docs); err2 != nil {
			return nil, fmt.Errorf("清单不是 {docs:[...]} 或 [...] 格式: %v", err)
		}
	}

	out := []ExternalDoc{}
	for _, d := range docs {
		if strings.TrimSpace(d.URL) == "" {
			continue
		}
		docURL, title := d.URL, d.Title
		key := d.Key
		if key == "" {
			key = docURL
		}
		if title == "" {
			title = docURL
		}
		out = append(out, ExternalDoc{
			DocKey: key,
			Title:  title,
			Fetch: func(ctx context.Context) ([]byte, error) {
				dl, err := http.NewRequestWithContext(ctx, http.MethodGet, docURL, nil)
				if err != nil {
					return nil, err
				}
				dr, err := client.Do(dl)
				if err != nil {
					return nil, err
				}
				defer func() { _ = dr.Body.Close() }()
				if dr.StatusCode != http.StatusOK {
					return nil, fmt.Errorf("下载 HTTP %d", dr.StatusCode)
				}
				return io.ReadAll(io.LimitReader(dr.Body, 64<<20))
			},
		})
	}
	return out, nil
}

func init() { RegisterProvider("webhttp", webhttpProvider{}) }
