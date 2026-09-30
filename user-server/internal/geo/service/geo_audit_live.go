package service

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// RunGEOAuditLive 带真实技术检查的审计：先跑纯内容审计，再对站点
// robots.txt / llms.txt 做真实 HTTP 检查并覆盖两个硬编码因子。
// SSRF 防护：仅 http/https，拒绝私网/回环/link-local IP 字面量与云元数据地址。
func (s *TechConfigService) RunGEOAuditLive(ctx context.Context, rawURL, title, content, metaDesc, schemaJSONLD string) *GeoAuditReport {
	rep := s.RunGEOAudit(rawURL, title, content, metaDesc, schemaJSONLD)

	base, err := safeAuditBaseURL(rawURL)
	if err != nil {
		return rep
	}
	httpClient := &http.Client{Timeout: 8 * time.Second}

	robotsOK, robotsDetail := checkRobotsAllowsAIBots(ctx, httpClient, base)
	llmsOK, llmsDetail := checkLLMsTxtPresent(ctx, httpClient, base)
	rep.Factors = overrideFactor(rep.Factors, "robots.txt 允许 GPTBot", robotsOK, robotsDetail)
	rep.Factors = overrideFactor(rep.Factors, "llms.txt 可用", llmsOK, llmsDetail)
	rep.Fixes = buildFixes(rep.Factors)
	rep.Score = auditScore(rep.Factors)
	rep.Grade = AuditGrade(rep.Score)
	rep.Summary = fmt.Sprintf("通过 %d/%d 项，加权得分 %d/100（含 robots.txt/llms.txt 真实检查）。", countPass(rep.Factors), len(rep.Factors), rep.Score)
	return rep
}

func overrideFactor(fs []GeoAuditFactor, name string, pass bool, detail string) []GeoAuditFactor {
	for i := range fs {
		if strings.Contains(fs[i].Factor, name) {
			fs[i].Pass = pass
			fs[i].Detail = detail
		}
	}
	return fs
}

func auditScore(fs []GeoAuditFactor) int {
	total, earned := 0, 0
	for _, f := range fs {
		total += f.Weight
		if f.Pass {
			earned += f.Weight
		}
	}
	if total == 0 {
		return 0
	}
	return earned * 100 / total
}

// safeAuditBaseURL 解析站点基址并做 SSRF 白名单式拒绝
func safeAuditBaseURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return "", fmt.Errorf("仅支持 http/https 站点 URL")
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return "", fmt.Errorf("拒绝内网主机")
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
			return "", fmt.Errorf("拒绝内网 IP")
		}
	}
	// 云元数据地址即使不在私网段也显式拒绝
	if host == "169.254.169.254" || host == "metadata.google.internal" {
		return "", fmt.Errorf("拒绝元数据地址")
	}
	return strings.TrimRight(u.Scheme+"://"+u.Host, "/"), nil
}

var aiBotTokens = []string{"gptbot", "oai-searchbot", "claudebot", "claude-searchbot", "perplexitybot", "google-extended", "bytespider"}

// checkRobotsAllowsAIBots 真实抓取 robots.txt，确认主流 AI 爬虫未被 Disallow 全站
func checkRobotsAllowsAIBots(ctx context.Context, client *http.Client, base string) (bool, string) {
	body, code, err := fetchAuditFile(ctx, client, base+"/robots.txt")
	if err != nil {
		return false, "robots.txt 抓取失败: " + err.Error()
	}
	if code == 404 {
		return false, "站点无 robots.txt（404），AI 爬虫默认可达但建议显式放行"
	}
	if code >= 400 {
		return false, fmt.Sprintf("robots.txt 返回 %d", code)
	}
	lb := strings.ToLower(body)
	blockedAll := strings.Contains(lb, "user-agent: *") && strings.Contains(lb, "disallow: /")
	allowed := 0
	for _, t := range aiBotTokens {
		if strings.Contains(lb, t) {
			allowed++
		}
	}
	if blockedAll && allowed == 0 {
		return false, "robots.txt 全站 Disallow 且未显式放行 AI 爬虫"
	}
	return true, fmt.Sprintf("robots.txt 可达，命中 %d/7 主流 AI 爬虫 token", allowed)
}

// checkLLMsTxtPresent 真实检查 /llms.txt 存在且非空
func checkLLMsTxtPresent(ctx context.Context, client *http.Client, base string) (bool, string) {
	body, code, err := fetchAuditFile(ctx, client, base+"/llms.txt")
	if err != nil {
		return false, "llms.txt 抓取失败: " + err.Error()
	}
	if code == 404 {
		return false, "站点无 /llms.txt（可用 POST /geo/techconfig/llms-txt 生成）"
	}
	if code >= 400 {
		return false, fmt.Sprintf("/llms.txt 返回 %d", code)
	}
	words := len([]rune(body))
	if words < 100 {
		return false, fmt.Sprintf("/llms.txt 过短（%d 字符），建议补充站点索引", words)
	}
	return true, fmt.Sprintf("/llms.txt 可达（约 %d 字符）", words)
}

func fetchAuditFile(ctx context.Context, client *http.Client, target string) (string, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("User-Agent", "HiveMTK-GEO-Audit/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	return string(raw), resp.StatusCode, nil
}
