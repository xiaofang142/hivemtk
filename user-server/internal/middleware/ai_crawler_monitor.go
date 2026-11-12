package middleware

import (
	"context"
	"strings"

	"hivemtk-user/internal/pkg/utils"

	"github.com/gin-gonic/gin"
)

var aiCrawlerBots = []struct {
	prefix string
	engine string
}{
	{"GPTBot", "OpenAI"},
	{"ChatGPT-User", "OpenAI"},
	{"OAI-SearchBot", "OpenAI"},
	{"PerplexityBot", "Perplexity"},
	{"Perplexity-User", "Perplexity"},
	{"ClaudeBot", "Anthropic"},
	{"Claude-Web", "Anthropic"},
	{"anthropic-ai", "Anthropic"},
	{"CCBot", "CommonCrawl"},
	{"Google-Extended", "Google"},
	{"GoogleOther", "Google"},
	{"Bytespider", "ByteDance"},
	{"Amazonbot", "Amazon"},
	{"Applebot-Extended", "Apple"},
	{"cohere-ai", "Cohere"},
	{"Diffbot", "Diffbot"},
}

// DetectAICrawler 检测 UA 是否为已知 AI 爬虫，返回归一化引擎名（非爬虫返回空）
func DetectAICrawler(ua string) string {
	if ua == "" {
		return ""
	}
	for _, b := range aiCrawlerBots {
		if strings.Contains(ua, b.prefix) {
			return b.engine
		}
	}
	return ""
}

// AICrawlerMonitor 中间件：自动记录 AI 爬虫访问到 geo_crawler_visits 表。
// 非阻塞：记录失败不影响请求处理。
// 注入 recorder 由装配层提供（避免 middleware→repository 直连违反五层架构）。
func AICrawlerMonitor(recorder func(engine, path, ua, ip string)) gin.HandlerFunc {
	return func(c *gin.Context) {
		engine := DetectAICrawler(c.GetHeader("User-Agent"))
		if engine != "" && recorder != nil {
			// 参数在请求 goroutine 内先取值：避免异步 goroutine 并发读 gin.Context
			// （gin.Context 非并发安全，且请求结束后会被池化复用）。
			path, ua, ip := c.Request.URL.Path, c.GetHeader("User-Agent"), c.ClientIP()
			utils.SafeGo(context.Background(), "middleware.ai_crawler_monitor", func(_ context.Context) {
				recorder(engine, path, ua, ip)
			})
		}
		c.Next()
	}
}
