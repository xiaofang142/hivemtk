// Package etl — image_describe.go P1-1：图片知识库（本地 VL 模型描述→文本索引）。
//
// 与「Embedding/Rerank 强制本地」同构：图片不走云端视觉 API，而是把图片 POST 给
// 本地 VL 推理服务（OpenAI chat 兼容形状，llama.cpp server 亦可挂 mmproj 模型），
// 拿回文本描述后走既有文本管线入库——图片从此可被文本 query 命中。
//
// 默认不启用（IMAGE_DESCRIBE_URL 为空时返回 ErrImageDescribeDisabled）：
// 上游管线对禁用态按「不支持格式」markFailed，与 G13 口径一致。
package etl

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ErrImageDescribeDisabled 未配置视觉服务（上游应按不支持格式处理）。
var ErrImageDescribeDisabled = errors.New("图片描述服务未配置（etl.image_describe_url）")

// SaaS 配置面改造（B6，2026-11-17）：IMAGE_DESCRIBE_URL/MODEL 环境变量退役，
// 改由参数中心 etl 组（image_describe_url / image_describe_model）经 provider 注入
// （装配点 internal/app）。
var imageDescribeConfigProvider = func() (string, string) { return "", "" }

// SetImageDescribeConfigProvider 注入图片描述服务取值口。传 nil 是空操作而不是复位。
func SetImageDescribeConfigProvider(fn func() (string, string)) {
	if fn != nil {
		imageDescribeConfigProvider = fn
	}
}

func imageDescribeConfig() (url, model string) { return imageDescribeConfigProvider() }

var imageExt = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".bmp": true}

// IsImageExt 是否图片后缀（管线据此分流到描述路径）。
func IsImageExt(filename string) bool { return imageExt[strings.ToLower(extOf(filename))] }

func extOf(filename string) string {
	i := strings.LastIndex(filename, ".")
	if i < 0 {
		return ""
	}
	return filename[i:]
}

// DescribeImage 图片→文本描述。dataURL 形状（base64 内联），零外发文件。
func DescribeImage(ctx context.Context, filename string, data []byte) (string, error) {
	url, model := imageDescribeConfig()
	if url == "" {
		return "", ErrImageDescribeDisabled
	}
	cctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	mime := "image/png"
	switch strings.ToLower(extOf(filename)) {
	case ".jpg", ".jpeg":
		mime = "image/jpeg"
	case ".gif":
		mime = "image/gif"
	case ".webp":
		mime = "image/webp"
	case ".bmp":
		mime = "image/bmp"
	}
	dataURL := "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)

	if model == "" {
		model = "vl"
	}
	payload := map[string]any{
		"model": model,
		"messages": []map[string]any{{
			"role": "user",
			"content": []map[string]any{
				{"type": "text", "text": "请详细描述这张图片的全部文字内容与关键信息，用于知识库检索索引。只输出描述本身。"},
				{"type": "image_url", "image_url": map[string]string{"url": dataURL}},
			},
		}},
		"max_tokens": 1024,
	}
	raw, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(cctx, http.MethodPost,
		strings.TrimRight(url, "/")+"/v1/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("视觉服务请求失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("视觉服务 HTTP %d: %s", resp.StatusCode, truncateStr(string(body), 200))
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("视觉服务响应解析失败: %w", err)
	}
	if len(out.Choices) == 0 || strings.TrimSpace(out.Choices[0].Message.Content) == "" {
		return "", fmt.Errorf("视觉服务返回空描述")
	}
	return out.Choices[0].Message.Content, nil
}

// ImageDescribeEnabled 可观测。
func ImageDescribeEnabled() bool { u, _ := imageDescribeConfig(); return u != "" }

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
