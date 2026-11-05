package service

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"hivemtk-user/internal/geo/dto"
	"hivemtk-user/internal/geo/model"
)

// publishWordPress 通过 WordPress XML-RPC（wp.newPost）真实发布文章。
// 凭证：endpoint（xmlrpc.php）、username、password（应用密码）。
func (s *PlatformService) publishWordPress(ctx context.Context, article *model.GeoArticle, req *dto.PublishRequest) (*dto.PublishResponse, error) {
	creds := s.latestAccountCredentials(ctx, "wordpress")
	if creds == nil {
		return nil, fmt.Errorf("未配置 WordPress 账号：请先保存 endpoint/用户名/应用密码")
	}
	endpoint := firstNonEmpty(creds["endpoint"], creds["url"])
	username := firstNonEmpty(creds["username"], creds["user"])
	password := firstNonEmpty(creds["password"], creds["api_key"])
	if endpoint == "" || username == "" || password == "" {
		return nil, fmt.Errorf("WordPress 凭证不完整：需要 endpoint、username、password")
	}
	if err := validateOutboundURL(endpoint, true); err != nil {
		return nil, err
	}

	title := firstNonEmpty(article.Title, article.Keyword)
	if title == "" {
		title = "GEO 文章"
	}
	body := wpNewPostXML(username, password, title, article.Content)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("构造 WordPress 请求失败: %w", err)
	}
	httpReq.Header.Set("Content-Type", "text/xml; charset=utf-8")
	httpReq.Header.Set("User-Agent", "HiveMtk-GEO/1.0")

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		s.saveFailedRecord(ctx, article.ID, req.Platform, err.Error())
		return nil, fmt.Errorf("WordPress 请求失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		msg := strings.TrimSpace(string(respBody))
		if len(msg) > 500 {
			msg = msg[:500]
		}
		s.saveFailedRecord(ctx, article.ID, req.Platform, msg)
		return nil, fmt.Errorf("WordPress XML-RPC %d: %s", resp.StatusCode, msg)
	}

	postID, fault := parseWPNewPostResponse(respBody)
	if fault != "" {
		s.saveFailedRecord(ctx, article.ID, req.Platform, fault)
		return nil, fmt.Errorf("WordPress 发布失败: %s", fault)
	}
	if postID == "" {
		s.saveFailedRecord(ctx, article.ID, req.Platform, "响应缺少文章 ID")
		return nil, fmt.Errorf("WordPress 响应缺少文章 ID")
	}

	site := firstNonEmpty(creds["site_url"], wpSiteFromEndpoint(endpoint))
	publishedURL := strings.TrimRight(site, "/") + "/?p=" + url.QueryEscape(postID)
	record := &model.GeoPublishRecord{
		ArticleID:    article.ID,
		Platform:     req.Platform,
		Status:       PublishStatusCopied,
		PublishedURL: publishedURL,
		PublishedAt:  time.Now(),
	}
	if err := s.recordRepo.Create(ctx, record); err != nil {
		return nil, err
	}
	return &dto.PublishResponse{ID: record.ID, Status: PublishStatusCopied, PublishedURL: publishedURL}, nil
}

func xmlEscape(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func wpNewPostXML(username, password, title, content string) []byte {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><methodCall><methodName>wp.newPost</methodName><params>`)
	b.WriteString(`<param><value><int>1</int></value></param>`)
	b.WriteString(`<param><value><string>` + xmlEscape(username) + `</string></value></param>`)
	b.WriteString(`<param><value><string>` + xmlEscape(password) + `</string></value></param>`)
	b.WriteString(`<param><value><struct>`)
	b.WriteString(`<member><name>post_title</name><value><string>` + xmlEscape(title) + `</string></value></member>`)
	b.WriteString(`<member><name>post_content</name><value><string>` + xmlEscape(content) + `</string></value></member>`)
	b.WriteString(`<member><name>post_status</name><value><string>publish</string></value></member>`)
	b.WriteString(`</struct></value></param></params></methodCall>`)
	return []byte(b.String())
}

type wpMethodResponse struct {
	Params struct {
		Param struct {
			Value struct {
				String string `xml:"string"`
				Int    string `xml:"int"`
			} `xml:"value"`
		} `xml:"param"`
	} `xml:"params"`
	Fault *struct {
		Value struct {
			Struct struct {
				Members []struct {
					Name  string `xml:"name"`
					Value struct {
						String string `xml:"string"`
						Int    string `xml:"int"`
					} `xml:"value"`
				} `xml:"member"`
			} `xml:"struct"`
		} `xml:"value"`
	} `xml:"fault"`
}

func parseWPNewPostResponse(body []byte) (postID, fault string) {
	var resp wpMethodResponse
	if err := xml.Unmarshal(body, &resp); err != nil {
		return "", "解析 XML-RPC 响应失败: " + err.Error()
	}
	if resp.Fault != nil {
		for _, m := range resp.Fault.Value.Struct.Members {
			if m.Name == "faultString" {
				return "", firstNonEmpty(m.Value.String, m.Value.Int, "WordPress 返回未知错误")
			}
		}
		return "", "WordPress 返回未知错误"
	}
	return firstNonEmpty(resp.Params.Param.Value.String, resp.Params.Param.Value.Int), ""
}

func wpSiteFromEndpoint(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return endpoint
	}
	return u.Scheme + "://" + u.Host
}
