// Package etl — parser_seam.go P1-1：可插拔文档解析器。
//
// 现状（G13 后）：ExtractText 直调、零抽象。本文件引入 Parser seam：
//   - builtin：现 ExtractText（默认，行为逐字节不变）
//   - sidecar：外挂解析服务（RAGFlow/MinerU 形态），HTTP POST multipart 上传
//     返回纯文本。默认不启用——只有显式配置 PARSER_SIDECAR_URL 才走，保持
//     「默认零外网请求」的隐私口径。
//
// seam 唯一消费方：knowledge_service_pipeline.go（ExtractText 调用点替换为 ParseDocument）。
package etl

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

// sidecarURL 外挂解析服务地址；空 = 不启用（builtin 路径）。
// SaaS 配置面改造（B6，2026-11-17）：PARSER_SIDECAR_URL 环境变量退役，改由参数中心
// etl.parser_sidecar_url 经 provider 注入（装配点 internal/app）。运行期不热切——
// 解析引擎切换影响索引一致性，不允许热切（provider 在装配期读一次快照进闭包）。
var sidecarURLProvider = func() string { return "" }

// SetSidecarURLProvider 注入外挂解析地址取值口。传 nil 是空操作而不是复位。
func SetSidecarURLProvider(fn func() string) {
	if fn != nil {
		sidecarURLProvider = fn
	}
}

func sidecarURL() string { return strings.TrimSpace(sidecarURLProvider()) }

// sidecarTimeout 外挂解析超时（复杂版式解析比纯文本慢一个量级）。
var sidecarTimeout = 120 * time.Second

// ParseDocument 统一解析入口（seam）。sidecar 未启用或非结构化后缀时
// 走 builtin ExtractText——语义与现状完全一致。
func ParseDocument(ctx context.Context, filename string, data []byte) (string, error) {
	if sidecarURL() == "" {
		return ExtractText(filename, data)
	}
	// 只有 builtin 明确「会失败或解析得差」的格式才值得走外挂；
	// 纯文本类直接 builtin（外挂对 .txt 没有增量价值，反而多一跳网络）。
	if IsPlainTextExt(filename) {
		return ExtractText(filename, data)
	}
	text, err := parseViaSidecar(ctx, filename, data)
	if err != nil {
		// 外挂失败不静默退化（G13 教训）：报错让上游 markFailed。
		// 也不回退 builtin——能走到外挂的格式（扫描件 PDF）builtin 必然也失败，
		// 回退只会掩盖 sidecar 故障。
		return "", fmt.Errorf("sidecar 解析失败（文件 %s）: %w", filename, err)
	}
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("sidecar 解析结果为空（可能为扫描件或空文件）")
	}
	return text, nil
}

// parseViaSidecar HTTP POST multipart 上传 → 纯文本响应。
// 契约：POST {sidecarURL}，字段 file=@文档；200 响应体即提取文本。
// （RAGFlow/MinerU 均可由一层薄适配 CGI/函数满足该契约；具体部署见部署文档。）
func parseViaSidecar(ctx context.Context, filename string, data []byte) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, sidecarTimeout)
	defer cancel()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("file", filename)
	if err != nil {
		return "", err
	}
	if _, err := fw.Write(data); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(cctx, http.MethodPost, sidecarURL(), &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("sidecar HTTP %d: %s", resp.StatusCode, string(body))
	}
	out, err := io.ReadAll(io.LimitReader(resp.Body, maxDocText+1))
	if err != nil {
		return "", err
	}
	if len(out) > maxDocText {
		return "", fmt.Errorf("sidecar 文本超过 %dMB 上限（疑似构造文档）", maxDocText>>20)
	}
	return string(out), nil
}

// SidecarEnabled 测试/装配可观测：外挂解析是否启用。
func SidecarEnabled() bool { return sidecarURL() != "" }
