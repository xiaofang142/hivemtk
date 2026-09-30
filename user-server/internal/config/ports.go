package config

import (
	"os"
	"strings"
)

const (
	// DefaultListenHost HTTP 监听主机兜底值。保持 0.0.0.0（历史行为）：
	// 收回本机只能靠显式设 SERVER_HOST=127.0.0.1，见 docs/DEPLOYMENT_GUIDE.md §6.2 加固路线①。
	DefaultListenHost = "0.0.0.0"

	DefaultListenPort = "8204"

	DefaultDBPortDev = 8232

	DefaultDBPortDocker = 8202

	DefaultRedisPort = "8203"

	DefaultPlatformPort = "8205"

	DefaultChromiumCDPPort = "8206"

	DefaultLLMPort = 8207

	DefaultLLMPortStr = "8207"

	DefaultEmbeddingPort = 8208

	DefaultEmbeddingPortStr = "8208"

	DefaultRerankPort = 8209

	DefaultRerankPortStr = "8209"

	// DefaultLayaPort Laya 决策服务（ModernBERT-large 421M，非自回归 choice/score/noul）。
	// Host-only torch 服务（scripts/inference-host/laya/server.py），端点 POST /v1/decide。
	DefaultLayaPort = 8210

	DefaultLayaPortStr = "8210"
)

const (
	DefaultUserServerBaseURL = "http://localhost:" + DefaultListenPort

	DefaultPlatformBaseURL = "http://localhost:" + DefaultPlatformPort

	// DefaultWebsiteBaseURL 官网基址（GitHub Pages 项目页形态，带 /hivemtk 路径前缀）。
	// 单一源：本常量；运行期覆盖：GEO_SITE_BASE_URL。
	DefaultWebsiteBaseURL = "https://xiaofang142.github.io/hivemtk"

	DefaultRemoteDebugURL = "http://localhost:" + DefaultChromiumCDPPort

	DefaultLLMBaseURLDev = "http://127.0.0.1:" + DefaultLLMPortStr + "/v1"

	DefaultEmbeddingBaseURLDev = "http://127.0.0.1:" + DefaultEmbeddingPortStr + "/v1"

	DefaultRerankBaseURLDev = "http://127.0.0.1:" + DefaultRerankPortStr + "/v1"

	DefaultLLMBaseURLDocker = "http://mtk-llm:" + DefaultLLMPortStr + "/v1"

	DefaultEmbeddingBaseURLDocker = "http://mtk-embedding:" + DefaultEmbeddingPortStr + "/v1"

	DefaultRerankBaseURLDocker = "http://mtk-rerank:" + DefaultRerankPortStr + "/v1"

	// DefaultLayaBaseURLDev Laya 决策服务基址（host-only，无容器形态）。
	// 注意无 /v1 后缀：决策端点是 POST {base}/v1/decide（非 OpenAI chat 形态）。
	DefaultLayaBaseURLDev = "http://127.0.0.1:" + DefaultLayaPortStr

	DefaultBGEBaseURLDev = "http://127.0.0.1:" + DefaultEmbeddingPortStr + "/v1"

	DefaultBGEBaseURLDocker = "http://mtk-embedding:" + DefaultEmbeddingPortStr + "/v1"

	DefaultOllamaBaseURL = "http://localhost:11434"
)

// UserServerSelfBaseURL 本进程回调自己 HTTP 入口时应使用的基址。
//
// 与 DefaultUserServerBaseURL 的区别只在这一点上：监听端口被 PORT 覆盖时（同机多实例
// 并行、或 8204 被别的进程占走）默认常量会把请求投给**另一个**进程。
// Telegram polling 的自投递（getUpdates → POST 自家 /api/webhook/telegram/:id）
// 就属于这一类：投错进程等于消息被没有同一份会话状态的实例处理，或直接 404 丢掉。
func UserServerSelfBaseURL() string {
	if v := strings.TrimSpace(os.Getenv("PORT")); v != "" {
		return "http://localhost:" + v
	}
	return DefaultUserServerBaseURL
}
