package config

const (
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

	DefaultBGEBaseURLDev = "http://127.0.0.1:" + DefaultEmbeddingPortStr + "/v1"

	DefaultBGEBaseURLDocker = "http://mtk-embedding:" + DefaultEmbeddingPortStr + "/v1"

	DefaultOllamaBaseURL = "http://localhost:11434"
)
