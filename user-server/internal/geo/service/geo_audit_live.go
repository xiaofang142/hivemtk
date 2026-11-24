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
func (s *TechConfigService) RunGEOAuditLive(ctx context.Context, rawURL, title, content, metaDesc, schemaJSONLD, brand string) *GeoAuditReport {
	rep := s.RunGEOAudit(rawURL, title, content, metaDesc, schemaJSONLD, brand)

	base, err := safeAuditBaseURL(rawURL)
	if err != nil {
		return rep
	}
	httpClient := newSiteAuditHTTPClient(8 * time.Second)

	robotsOK, robotsDetail := checkRobotsAllowsAIBots(ctx, httpClient, base)
	llmsOK, llmsDetail := checkLLMsTxtPresent(ctx, httpClient, base)
	rep.Factors = overrideFactor(rep.Factors, "robots.txt 允许 GPTBot", robotsOK, robotsDetail)
	rep.Factors = overrideFactor(rep.Factors, "llms.txt 可用", llmsOK, llmsDetail)
	rep.Fixes = buildFixes(rep.Factors)
	rep.Score = auditScore(rep.Factors)
	rep.Grade = AuditGrade(rep.Score)
	rep.Summary = fmt.Sprintf("通过 %d/%d 项，加权得分 %d/100（含 robots.txt/llms.txt 真实检查）。", countPass(rep.Factors), countChecked(rep.Factors), rep.Score)
	return rep
}

func overrideFactor(fs []GeoAuditFactor, name string, pass bool, detail string) []GeoAuditFactor {
	for i := range fs {
		if strings.Contains(fs[i].Factor, name) {
			fs[i].Pass = pass
			fs[i].Detail = detail
			fs[i].Skipped = false
		}
	}
	return fs
}

func auditScore(fs []GeoAuditFactor) int {
	total, earned := 0, 0
	for _, f := range fs {
		if f.Skipped {
			continue
		}
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

// liveRobotsGroup 是 robots.txt 中一组连续 User-agent 共享的 Allow/Disallow 规则。
type liveRobotsGroup struct {
	agents    []string
	allows    []string
	disallows []string
}

// parseRobots 把 robots.txt 解析成规则组：连续的 User-agent 行归属同一组，
// 规则行（Allow/Disallow）把当前待定的 agent 列表固化为一组；注释与空行忽略。
func parseLiveRobots(body string) []liveRobotsGroup {
	var groups []liveRobotsGroup
	var agents []string
	ensureGroup := func() *liveRobotsGroup {
		if len(agents) > 0 {
			groups = append(groups, liveRobotsGroup{agents: agents})
			agents = nil
		}
		if len(groups) == 0 {
			groups = append(groups, liveRobotsGroup{})
		}
		return &groups[len(groups)-1]
	}
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		if line == "" {
			continue
		}
		colon := strings.IndexByte(line, ':')
		if colon < 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(line[:colon]))
		val := strings.TrimSpace(line[colon+1:])
		switch key {
		case "user-agent":
			agents = append(agents, strings.ToLower(val))
		case "allow", "disallow":
			g := ensureGroup()
			if key == "allow" {
				g.allows = append(g.allows, val)
			} else if val != "" {
				// 空 Disallow 等于放行，不作为阻断规则。
				g.disallows = append(g.disallows, val)
			}
		}
	}
	if len(agents) > 0 {
		groups = append(groups, liveRobotsGroup{agents: agents})
	}
	return groups
}

// liveRobotsPathAllowed 按 robots 路径优先级判定：最长匹配胜出，等长时 Allow 优先。
// 无任何规则命中时按 robots 规范默认放行。
func liveRobotsPathAllowed(g liveRobotsGroup, path string) (bool, string) {
	bestLen := -1
	allowed := true
	rule := "no matching rule (default allow)"
	for _, d := range g.disallows {
		if d != "" && strings.HasPrefix(path, d) && len(d) >= bestLen {
			bestLen, allowed, rule = len(d), false, "Disallow: "+d
		}
	}
	for _, a := range g.allows {
		if a != "" && strings.HasPrefix(path, a) && len(a) >= bestLen {
			bestLen, allowed, rule = len(a), true, "Allow: "+a
		}
	}
	return allowed, rule
}

// liveRobotsBotVerdict 逐 bot 判定：特定 User-agent 组优先于通配组；都没有则默认放行。
func liveRobotsBotVerdict(groups []liveRobotsGroup, token string) (bool, string) {
	var specific, wildcard *liveRobotsGroup
	for i := range groups {
		g := &groups[i]
		for _, a := range g.agents {
			if strings.Contains(a, token) && specific == nil {
				specific = g
			}
			if a == "*" && wildcard == nil {
				wildcard = g
			}
		}
	}
	g := specific
	if g == nil {
		g = wildcard
	}
	if g == nil {
		return true, "no matching group (default allow)"
	}
	return liveRobotsPathAllowed(*g, "/")
}

// checkRobotsAllowsAIBots 真实抓取 robots.txt，按规则组判定主流 AI 爬虫是否被放行。
// 不再把“出现 bot 名称”当成放行：必须在解析出的规则中逐 bot 得出可执行证据。
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
	groups := parseLiveRobots(body)
	if len(groups) == 0 {
		return true, "robots.txt 无规则组，主流 AI 爬虫默认可达"
	}
	allowed := 0
	evidence := make([]string, 0, len(aiBotTokens))
	for _, token := range aiBotTokens {
		ok, rule := liveRobotsBotVerdict(groups, token)
		if ok {
			allowed++
		}
		evidence = append(evidence, fmt.Sprintf("%s=%t(%s)", token, ok, rule))
	}
	detail := "逐 bot: " + strings.Join(evidence, "; ")
	if allowed == 0 {
		return false, "robots.txt 规则组判定全部主流 AI 爬虫被禁止；" + detail
	}
	return true, fmt.Sprintf("robots.txt 规则组判定 AI 爬虫可达 %d/%d；%s", allowed, len(aiBotTokens), detail)
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
