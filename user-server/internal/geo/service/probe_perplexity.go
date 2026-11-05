package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// perplexityProbe 真实 AI 搜索探针：Perplexity 的 Sonar API 会返回真实检索到的
// citations，而不是让模型“扮演”搜索引擎。无凭证/无配置时不会装配。
//
// 与 llmEndpointProbe 的关键差别：
//   - Mode=real（UI 必须显示“真实”）；
//   - citations 来自上游响应字段（缺失时才从正文兜底提取 URL）；
//   - 不注入“请列出你所知的链接”这类诱导，避免把模型幻觉包装成信源。
type perplexityProbe struct {
	name      string
	endpoint  string
	model     string
	apiKey    string
	brandHint string
}

func (p *perplexityProbe) Name() string {
	if p.name != "" {
		return p.name
	}
	return "perplexity"
}

func (p *perplexityProbe) Mode() string { return ProbeModeReal }

func (p *perplexityProbe) Probe(ctx context.Context, query string) (*ProbeResult, error) {
	start := time.Now()
	systemPrompt := "你是 AI 搜索助手。请基于你检索到的公开资料直接回答问题，200-400 字。" +
		"如果涉及具体产品/品牌，客观陈述其定位与特点。回答最后单独输出一行情感标签：" +
		"[SENTIMENT: positive] / [SENTIMENT: neutral] / [SENTIMENT: negative]，三选一。"
	if p.brandHint != "" {
		systemPrompt += "\n【品牌上下文】" + p.brandHint + "。回答中涉及此品牌时以此描述为准。"
	}
	payload := map[string]any{
		"model": p.model,
		"messages": []map[string]any{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": query},
		},
		"temperature": 0.3,
		"max_tokens":  1024,
	}
	auth := ""
	if p.apiKey != "" && !strings.EqualFold(p.apiKey, "local") {
		auth = "Bearer " + p.apiKey
	}

	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Citations json.RawMessage `json:"citations"`
		Error     *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := doJSON(ctx, chatCompletionsURL(p.endpoint), auth, payload, &out); err != nil {
		return nil, fmt.Errorf("%s: %w", p.Name(), err)
	}
	if out.Error != nil && out.Error.Message != "" {
		return nil, fmt.Errorf("%s: %s", p.Name(), out.Error.Message)
	}
	if len(out.Choices) == 0 {
		return nil, fmt.Errorf("%s: empty response", p.Name())
	}
	content := out.Choices[0].Message.Content
	sentiment := "neutral"
	if m := sentimentRegexp.FindStringSubmatch(content); len(m) == 2 {
		sentiment = m[1]
		content = strings.TrimSpace(sentimentRegexp.ReplaceAllString(content, ""))
	}
	cites := parseProbeCitations(out.Citations)
	if len(cites) == 0 {
		cites = extractCitationsFromText(content)
	}
	return &ProbeResult{
		Engine: p.Name(), Mode: ProbeModeReal, Query: query, Response: content,
		Citations: cites, LatencyMs: time.Since(start).Milliseconds(), Sentiment: sentiment,
	}, nil
}

func chatCompletionsURL(endpoint string) string {
	e := strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if strings.HasSuffix(e, "/chat/completions") {
		return e
	}
	return e + "/chat/completions"
}

// parseProbeCitations 兼容 []string 与 [{"url":...}] 两种上游形状。
func parseProbeCitations(raw json.RawMessage) []Citation {
	if len(raw) == 0 {
		return nil
	}
	var urls []string
	if err := json.Unmarshal(raw, &urls); err == nil {
		out := make([]Citation, 0, len(urls))
		for _, u := range urls {
			if strings.TrimSpace(u) != "" {
				out = append(out, Citation{URL: u})
			}
		}
		return out
	}
	var objs []struct {
		URL   string `json:"url"`
		Title string `json:"title"`
	}
	if err := json.Unmarshal(raw, &objs); err == nil {
		out := make([]Citation, 0, len(objs))
		for _, o := range objs {
			if strings.TrimSpace(o.URL) != "" {
				out = append(out, Citation{URL: o.URL, Title: o.Title})
			}
		}
		return out
	}
	return nil
}

// ProbeModeOf 取探针的数据来源；未声明 Mode 的实现按 simulated 处理。
func ProbeModeOf(p SearchProbe) string {
	if m, ok := p.(interface{ Mode() string }); ok {
		return probeModeOrDefault(m.Mode())
	}
	return ProbeModeSimulated
}
