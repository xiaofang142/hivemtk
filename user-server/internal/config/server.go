package config

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"hivemtk-user/internal/pkg/utils/logger"

	"gopkg.in/yaml.v3"
)

var envVarWithDefaultRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?::([^}]*))?\}`)

func expandEnvWithDefault(s string) string {
	return envVarWithDefaultRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := envVarWithDefaultRe.FindStringSubmatch(m)
		if len(sub) < 2 {
			return m
		}
		name := sub[1]
		def := ""
		if len(sub) >= 3 {
			def = sub[2]
		}
		if v, ok := os.LookupEnv(name); ok {
			return v
		}
		return def
	})
}

// DBType 数据库类型（本系统统一使用 PostgreSQL）
type DBType string

const (
	DBTypePostgres DBType = "postgres"
)

// VectorDBType 向量数据库类型
type VectorDBType string

const (
	VectorDBTypePGVector VectorDBType = "pgvector"
)

type platformAPIYAML struct {
	PlatformAPI struct {
		BaseURL string `yaml:"api_url"`
	} `yaml:"platform_api"`
}

// DatabaseConfig 数据库配置
type DatabaseConfig struct {
	Type     DBType         `yaml:"type"`
	Postgres PostgresConfig `yaml:"postgres"`
	Pool     PoolConfig     `yaml:"pool"`
}

// PoolConfig 数据库连接池配置
type PoolConfig struct {
	MaxIdleConns    int `yaml:"max_idle_conns"`
	MaxOpenConns    int `yaml:"max_open_conns"`
	ConnMaxIdleTime int `yaml:"conn_max_idle_time"`
	ConnMaxLifetime int `yaml:"conn_max_lifetime"`
}

// DefaultPoolConfig 默认连接池配置
//
// 面向 2000 万/日（主动 1000 万 + 被动 1000 万）单节点工作站。
// AI 生成已与接入 worker 解耦，DB 连接仅在短暂的读写期间占用、不跨越数秒级 LLM 推理，
// 因此可将连接池放大以吸收峰值并发；同时设 ConnMaxLifetime 回收长生命周期连接避免陈旧连接。
var DefaultPoolConfig = PoolConfig{
	MaxIdleConns:    50,
	MaxOpenConns:    200,
	ConnMaxIdleTime: 300,
	ConnMaxLifetime: 3600,
}

const (
	defaultLLMBaseURL       = DefaultLLMBaseURLDev
	defaultEmbeddingBaseURL = DefaultEmbeddingBaseURLDev
	defaultRerankBaseURL    = DefaultRerankBaseURLDev
	defaultEmbeddingDim     = 1024

	// 三个模型名必须与 .env-example 的 *_SERVED_NAME 同源（推理栈用它作 llama-server --alias），
	// 由 TestDefaultModelsMatchEnvExample 把关；换档只改 .env-example 会让本回落指向未下载的模型。
	defaultLLMModelLocal       = "Qwen2.5-3B-Instruct"
	defaultEmbeddingModelLocal = "bge-m3"
	defaultRerankModelLocal    = "bge-reranker-v2-m3"
)

const (
	defaultLLMModel       = defaultLLMModelLocal
	defaultEmbeddingModel = defaultEmbeddingModelLocal
	defaultRerankModel    = defaultRerankModelLocal
)

// DefaultInferenceConfig 返回本地推理栈默认配置（私域部署基线）
//
// 用于 config.yaml 缺失时的回落，确保消费方拿到的 InferenceConfig 永远非零值。
// 消费方（dispatcher 等）可从该默认配置取得模型名/URL，无需各自兜底硬编码。
func DefaultInferenceConfig() InferenceConfig {
	return InferenceConfig{
		Profile: "dev",
		Embedding: InferenceEmbeddingConfig{
			Mode:      InferenceModeLocal,
			BaseURL:   defaultEmbeddingBaseURL,
			Model:     defaultEmbeddingModel,
			Dimension: defaultEmbeddingDim,
		},
		Rerank: InferenceRerankConfig{
			Mode:    InferenceModeLocal,
			BaseURL: defaultRerankBaseURL,
			Model:   defaultRerankModel,
			Enabled: true,
		},
		LLM: InferenceLLMConfig{
			Mode:           InferenceModeLocal,
			BaseURL:        defaultLLMBaseURL,
			Model:          defaultLLMModel,
			Temperature:    0.7,
			MaxTokens:      1024,
			TimeoutSeconds: 720,
			MaxRetries:     1,
		},
		// Tier 缺省为 false（禁止云端降级，合规基线）；显式写出便于 grep 审计。
		Tier: InferenceTierConfig{FallbackToCloudLLM: false},
	}
}

// DefaultLLMModel dev 档默认 LLM 模型名（外部包引用入口）
func DefaultLLMModel() string { return defaultLLMModelLocal }

// DefaultEmbeddingModel dev 档默认 Embedding 模型名（外部包引用入口）
func DefaultEmbeddingModel() string { return defaultEmbeddingModelLocal }

// DefaultRerankModel dev 档默认 Rerank 模型名（外部包引用入口）
func DefaultRerankModel() string { return defaultRerankModelLocal }

// PostgresConfig PostgreSQL 配置
type PostgresConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	DBName   string `yaml:"dbname"`
	SSLMode  string `yaml:"sslmode"`
	Timezone string `yaml:"timezone"`
}

// VectorDatabaseConfig 向量数据库配置
type VectorDatabaseConfig struct {
	Type     VectorDBType   `yaml:"type"`
	PGVector PGVectorConfig `yaml:"pgvector"`
}

// InferenceMode 推理能力来源模式
type InferenceMode string

const (
	InferenceModeLocal  InferenceMode = "local"
	InferenceModeRemote InferenceMode = "remote"
)

// InferenceConfig 本地推理栈（embedding / rerank / llm）统一配置
//
// 设计要点（详见 docs/architecture/HOST_INFERENCE_PLAN.md）：
//   - 三个能力统一暴露为 OpenAI 兼容 HTTP（/v1/embeddings、/v1/rerank、/v1/chat/completions）。
//   - 默认走本地（mode: local）；用户只需在 base_url+api_key 填线上地址即切到线上（mode 自动 remote）。
//   - profile=dev 选最轻量模型；profile=prod 选效果/硬件平衡；但 embedding 维度必须 1024。
//   - 三级回落：配置文件 inference.* > 环境变量 > 内置本地默认。
type InferenceConfig struct {
	Profile   string                   `yaml:"profile" json:"profile"`
	Embedding InferenceEmbeddingConfig `yaml:"embedding" json:"embedding"`
	Rerank    InferenceRerankConfig    `yaml:"rerank" json:"rerank"`
	LLM       InferenceLLMConfig       `yaml:"llm" json:"llm"`
	Tier      InferenceTierConfig      `yaml:"tier" json:"tier"`
}

// InferenceTierConfig 推理降级档配置（C2）。
//
// 默认 false：本地 LLM 不可达时不自动回退云端（合规基线——云端 LLM 出域必须显式开启）。
// 开启后，启动期档位选择器（internal/app/inference_tier.go）才会在本地 LLM 不可达时
// 选择 cloud_llm_local_embedding 档；Embedding 无论档位都保持本地，数据不出域。
type InferenceTierConfig struct {
	// FallbackToCloudLLM 本地 LLM 不可达时是否允许回退云端 LLM。
	FallbackToCloudLLM bool `yaml:"fallback_to_cloud_llm" json:"fallback_to_cloud_llm"`
}

// InferenceEmbeddingConfig 文本向量配置
//
// 私域部署基线：Embedding 默认走本地服务（宿主机 127.0.0.1:8208 / docker mtk-embedding:8208），
// 严禁静默走公网 LLM 厂商 API，也严禁静默降级到哈希伪向量。
//   - base_url: OpenAI 兼容 /v1/embeddings 地址
//   - model:    向量模型名；维度必须与 pgvector vector(N) 一致（硬性 1024）
//   - dimension:向量维度（默认 1024，与 bge-m3 一致）
//   - allow_fallback: 是否允许哈希伪向量降级（默认 false，仅单测显式开启）
type InferenceEmbeddingConfig struct {
	Mode      InferenceMode `yaml:"mode" json:"mode"`
	BaseURL   string        `yaml:"base_url" json:"base_url"`
	Model     string        `yaml:"model" json:"model"`
	Dimension int           `yaml:"dimension" json:"dimension"`
	APIKey    string        `yaml:"api_key" json:"api_key"`
	// Protocol embedding 协议：空或 "openai" = {base}/v1/embeddings；
	// "tei" = TEI 原生 {base}/embed（请求 {inputs}，响应 [[float]]）。
	Protocol      string `yaml:"protocol" json:"protocol"`
	AllowFallback bool   `yaml:"allow_fallback" json:"allow_fallback"`
}

// InferenceRerankConfig 重排配置
//
// 私域部署基线：Rerank 与 Embedding 同走本地推理服务（宿主机 127.0.0.1:8209 / docker mtk-rerank:8209），
// 使用跨编码器，数据不出域。
type InferenceRerankConfig struct {
	Mode    InferenceMode `yaml:"mode" json:"mode"`
	BaseURL string        `yaml:"base_url" json:"base_url"`
	Model   string        `yaml:"model" json:"model"`
	Enabled bool          `yaml:"enabled" json:"enabled"`
	APIKey  string        `yaml:"api_key" json:"api_key"`
	// Protocol 重排协议：空或 "openai" = OpenAI 兼容 /v1/rerank（现状）；
	// "dashscope" = DashScope 原生 text-rerank 端点
	// （POST {base_url}/api/v1/services/rerank/text-rerank/text-rerank，base_url 即完整端点路径）；
	// "tei" = TEI 原生协议（POST {base_url}/rerank，请求 query/texts，响应 [{index,score}]）。
	Protocol string `yaml:"protocol" json:"protocol"`
}

// InferenceLLMConfig 大语言模型配置
//
// 默认走本地 mtk-llm（llama.cpp，OpenAI 兼容 /v1/chat/completions）。
// 若 mode=remote 或 base_url 指向线上 OpenAI 兼容服务且 api_key 非空，则自动走线上。
// CloudProviders 为可选的云端 fallback（deepseek/qwen/gpt-4o 等），仅在配置 api_key 后启用。
// NoFC: 标记该 LLM 是否支持 OpenAI Function Calling；本地 Qwen2.5-3B-Instruct 不支持，
// 启用 ReAct 适配器（thought/action 文本协议）走工具调用；用户在 config.yaml
// 设置 inference.llm.no_fc=false 可显式关闭（如果模型已升级支持 FC）。
type InferenceLLMConfig struct {
	Mode            InferenceMode                  `yaml:"mode" json:"mode"`
	BaseURL         string                         `yaml:"base_url" json:"base_url"`
	Model           string                         `yaml:"model" json:"model"`
	APIKey          string                         `yaml:"api_key" json:"api_key"`
	Temperature     float64                        `yaml:"temperature" json:"temperature"`
	MaxTokens       int                            `yaml:"max_tokens" json:"max_tokens"`
	TimeoutSeconds  int                            `yaml:"timeout_seconds" json:"timeout_seconds"`
	MaxRetries      int                            `yaml:"max_retries" json:"max_retries"`
	NoFC            *bool                          `yaml:"no_fc" json:"no_fc"`
	PrimaryProvider string                         `yaml:"primary_provider" json:"primary_provider"`
	CloudProviders  []InferenceCloudProviderConfig `yaml:"cloud_providers" json:"cloud_providers"`
}

// InferenceCloudProviderConfig 可选的云端 fallback 厂商（OpenAI 兼容）
//
// 仅当用户显式配置 api_key 并 enabled=true 时，dispatcher 才将其注册为可选 fallback。
type InferenceCloudProviderConfig struct {
	Name    string `yaml:"name"`
	APIKey  string `yaml:"api_key"`
	BaseURL string `yaml:"base_url"`
	APIType string `yaml:"api_type"`
	Model   string `yaml:"model"`
	Enabled bool   `yaml:"enabled"`
}

// PGVectorConfig pgvector 配置
type PGVectorConfig struct {
	Table     string `yaml:"table"`
	Dimension int    `yaml:"dimension"`
}

// StorageConfig 存储配置
type StorageConfig struct {
	Type  string      `yaml:"type"`
	Qiniu QiniuConfig `yaml:"qiniu"`
}

// QiniuConfig 七牛云配置
type QiniuConfig struct {
	AccessKey    string `yaml:"access_key"`
	SecretKey    string `yaml:"secret_key"`
	Bucket       string `yaml:"bucket"`
	Domain       string `yaml:"domain"`
	UploadDomain string `yaml:"upload_domain"`
	Region       string `yaml:"region"`
}

// AppConfig 应用配置
type AppConfig struct {
	Database       DatabaseConfig       `yaml:"database"`
	VectorDatabase VectorDatabaseConfig `yaml:"vector_database"`
	Inference      InferenceConfig      `yaml:"inference"`
	Storage        StorageConfig        `yaml:"storage"`
	Logging        logger.LoggingConfig `yaml:"logging"`
	I18n           I18nConfig           `yaml:"i18n"`
	External       ExternalConfig       `yaml:"external"`
	Proxy          ProxyConfig          `yaml:"proxy"`
	SSO            SSOConfig            `yaml:"sso"`
	Security       SecurityConfig       `yaml:"security"`
}

// SecurityConfig 安全配置
//
// 用于存储安全相关密钥和策略配置，如 visitor token HMAC 密钥。
type SecurityConfig struct {
	// VisitorTokenSecret 访客 token HMAC 密钥
	// 用于生成和验证 visitor_token，防止 IDOR 越权访问
	// 建议使用 32+ 字符随机字符串，通过环境变量覆盖
	VisitorTokenSecret string `yaml:"visitor_token_secret" json:"visitor_token_secret"`
}

// ProxyConfig HTTP 代理配置
//
// 用途：在中国等需要代理才能访问外部 API（如 Telegram API）的环境中使用。
// 未配置时（enabled=false 或地址为空），HTTP 请求直连不走代理。
//
// 配置方式：
//  1. config.yaml proxy 段
//  2. 环境变量 HTTP_PROXY / HTTPS_PROXY / NO_PROXY（Go 标准库自动识别）
//
// 示例 (config.yaml)：
//
//	proxy:
//	  enabled: true
//	  http_proxy: "http://127.0.0.1:7890"
//	  https_proxy: "http://127.0.0.1:7890"
//	  no_proxy: "localhost,127.0.0.1,10.0.0.0/8"
type ProxyConfig struct {
	Enabled    bool   `yaml:"enabled" json:"enabled"`
	HTTPProxy  string `yaml:"http_proxy" json:"http_proxy"`
	HTTPSProxy string `yaml:"https_proxy" json:"https_proxy"`
	NoProxy    string `yaml:"no_proxy" json:"no_proxy"`
}

const (
	proxyIdleConnTimeout     = 90 * time.Second
	proxyMaxIdleConns        = 100
	proxyMaxIdleConnsPerHost = 10
)

var (
	proxyTransportMu  sync.Mutex
	proxyTransportKey string
	proxyTransport    *http.Transport
)

// GetProxyTransport 返回配置了代理的 http.Transport
//
// 代理优先级（自高到低）：
//  1. config.yaml proxy 段（enabled=true + 地址非空）
//  2. 环境变量 HTTP_PROXY / HTTPS_PROXY / NO_PROXY（Go 标准库 ProxyFromEnvironment）
//  3. 直连（无代理）
//
// 返回值可直接赋给 http.Client.Transport。
//
// 返回的是按当前配置复用的单例，不是每次新建：零值 Transport 的 IdleConnTimeout=0
// 意味着空闲连接永不过期，而本函数被 tgbot 的每次出站动作（发消息、注册/删除回调）
// 反复调用，每新建一个就带走一组连接和它钉住的一对 readLoop/writeLoop goroutine。
// 配置变了键就变，于是下一次调用拿到带新代理决策的实例，不会读到过期选择。
func GetProxyTransport() *http.Transport {
	cfg := GetAppConfig().Proxy
	configured := cfg.Enabled && (cfg.HTTPProxy != "" || cfg.HTTPSProxy != "")
	envProxy := os.Getenv("HTTP_PROXY") != "" || os.Getenv("HTTPS_PROXY") != "" ||
		os.Getenv("http_proxy") != "" || os.Getenv("https_proxy") != ""

	branch := "direct"
	switch {
	case configured:
		branch = "config:" + cfg.HTTPProxy + "|" + cfg.HTTPSProxy + "|" + cfg.NoProxy
	case envProxy:
		branch = "env"
	}

	proxyTransportMu.Lock()
	defer proxyTransportMu.Unlock()
	if proxyTransport != nil && proxyTransportKey == branch {
		return proxyTransport
	}

	t := &http.Transport{
		IdleConnTimeout:     proxyIdleConnTimeout,
		MaxIdleConns:        proxyMaxIdleConns,
		MaxIdleConnsPerHost: proxyMaxIdleConnsPerHost,
	}
	switch {
	case configured:
		httpProxy, httpsProxy := cfg.HTTPProxy, cfg.HTTPSProxy
		t.Proxy = func(req *http.Request) (*url.URL, error) {
			if req.URL.Scheme == "https" && httpsProxy != "" {
				return url.Parse(httpsProxy)
			}
			if httpProxy != "" {
				return url.Parse(httpProxy)
			}
			return nil, nil
		}
	case envProxy:
		t.Proxy = http.ProxyFromEnvironment
	}

	proxyTransportKey = branch
	proxyTransport = t
	return t
}

// ExternalConfig 外部可达地址配置
//
// 用途：用户端 user-server 通常部署在内网/反代后面（frp / 反向代理层 / 云函数），
// 业务侧无法通过「请求 Host 头」直接推断 Telegram / 飞书 / 钉钉等回调所需的公网 URL。
// 显式声明 public_base_url 后，系统在注册被动渠道 webhook 时优先使用该地址，
// 避免因「localhost:8080」导致 Telegram 无法向本系统投递更新。
//
// 覆盖优先级（自高到低）：
//  1. 环境变量 PUBLIC_BASE_URL（部署期直接覆盖）
//  2. config.yaml external.public_base_url 字段
//  3. X-Forwarded-Proto / X-Forwarded-Host 头（反代透传）
//  4. 请求自身 Host（仅适合公网直连调试）
type ExternalConfig struct {
	PublicBaseURL string `yaml:"public_base_url" json:"public_base_url"`
}

// publicBaseURLProvider 是 urls.public_base_url 参数中心取值口。
// 默认返回空串 = 未装配，GetPublicBaseURLFor() 继续回落 env / config.yaml / 空串。
//
// 做成 provider 而不是在这里调 config_param service：config 不能反向依赖 service
// （service 层 import 了 config，反向会成环），装配点在 internal/app/url_params_wiring.go。
var publicBaseURLProvider = func(ctx context.Context) string { return "" }

// SetPublicBaseURLProvider 注入 urls.public_base_url 参数中心取值口。传 nil 是空操作而不是复位。
func SetPublicBaseURLProvider(fn func(ctx context.Context) string) {
	if fn != nil {
		publicBaseURLProvider = fn
	}
}

// GetPublicBaseURLFor 解析公网基座 URL（用于自动推导渠道 webhook 回调地址）
//
// 解析顺序：
//  1. 环境变量 PUBLIC_BASE_URL（部署期单点覆盖）
//  2. config.yaml external.public_base_url
//  3. 参数中心 urls.public_base_url（按 ctx 解析租户覆盖，未绑定租户读平台默认）
//  4. 返回空字符串 → 调用方回退到 X-Forwarded-* 头 / 请求 Host
//
// 返回值已去除尾部斜杠，scheme 强制 https（若用户误填 http+端口 自动升级，避免 Telegram 拒收）。
func GetPublicBaseURLFor(ctx context.Context) string {
	if v := strings.TrimSpace(os.Getenv("PUBLIC_BASE_URL")); v != "" {
		return NormalizePublicBaseURL(v)
	}
	if cfg := GetAppConfig().External.PublicBaseURL; cfg != "" {
		return NormalizePublicBaseURL(cfg)
	}
	if v := strings.TrimSpace(publicBaseURLProvider(ctx)); v != "" {
		return NormalizePublicBaseURL(v)
	}
	return ""
}

// GetPublicBaseURL 无 ctx 形态：平台级调用（无租户归属）等价于未绑定租户读取。
func GetPublicBaseURL() string { return GetPublicBaseURLFor(context.Background()) }

// NormalizePublicBaseURL 规范化公网基座 URL：
//   - 去除尾部斜杠
//   - 若 scheme 缺失，无端口默认补 https（公网基线）
//   - 若显式 http 但提供端口，自动升级为 https（Telegram / 飞书等强制 https）
//
// 暴露为公开函数，便于 controller / bootstrap 单测直接覆盖。
func NormalizePublicBaseURL(raw string) string {
	v := strings.TrimSpace(raw)
	v = strings.TrimRight(v, "/")
	if v == "" {
		return ""
	}
	idx := strings.Index(v, "://")
	if idx == -1 {
		return "https://" + v
	}
	scheme := strings.ToLower(v[:idx])
	rest := v[idx+3:]
	if scheme == "http" && strings.Contains(rest, ":") {
		return "https://" + rest
	}
	return v[:idx+3] + rest
}

// I18nConfig 多语言方案配置（v1.2 出海多语言）
//
// 包含三个子段：
//   - embedding：bge-m3 多语言 embedding provider 配置（独立于 inference.embedding，
//     可为多语言路径指定不同的 base_url/model/api_key）
//   - cache：跨语言回复翻译缓存配置（Redis 后端）
//
// fallback：低资源语言降级桥配置（：DeepL 翻译降级）
type I18nConfig struct {
	Embedding I18nEmbeddingConfig `yaml:"embedding" json:"embedding"`
	Cache     I18nCacheConfig     `yaml:"cache" json:"cache"`
	Fallback  I18nFallbackConfig  `yaml:"fallback" json:"fallback"`
}

// I18nFallbackConfig 低资源语言降级桥配置。
//
// 启用条件：enabled=true 且 deepl.api_key 非空。
// 未配置 api_key 时 FallbackBridge 自动禁用，不影响主流程。
type I18nFallbackConfig struct {
	Enabled          bool        `yaml:"enabled" json:"enabled"`
	LowResourceLangs []string    `yaml:"low_resource_langs" json:"low_resource_langs"`
	Translator       string      `yaml:"translator" json:"translator"`
	DeepL            DeepLConfig `yaml:"deepl" json:"deepl"`
}

// DeepLConfig DeepL 翻译服务配置。
type DeepLConfig struct {
	APIKey  string `yaml:"api_key" json:"api_key"`
	BaseURL string `yaml:"base_url" json:"base_url"`
}

// I18nEmbeddingConfig 多语言 embedding provider 配置
//
// provider 类型：
//   - "openai"（默认）：复用 inference.embedding 配置（基于 llm.EmbeddingService）
//   - "bge-m3"：显式 bge-m3 provider，走 OpenAI 兼容 /v1/embeddings，
//     支持 normalize / batch_size 等 bge-m3 专属参数
//
// 向后兼容：provider 为空时回退 "openai"。
type I18nEmbeddingConfig struct {
	Provider  string `yaml:"provider" json:"provider"`
	Model     string `yaml:"model" json:"model"`
	BaseURL   string `yaml:"base_url" json:"base_url"`
	APIKey    string `yaml:"api_key" json:"api_key"`
	Dimension int    `yaml:"dimension" json:"dimension"`
	Normalize bool   `yaml:"normalize" json:"normalize"`
	BatchSize int    `yaml:"batch_size" json:"batch_size"`
}

// I18nCacheConfig 跨语言翻译缓存配置
type I18nCacheConfig struct {
	Enabled    bool   `yaml:"enabled" json:"enabled"`
	TTL        int    `yaml:"ttl" json:"ttl"`
	KeyPrefix  string `yaml:"key_prefix" json:"key_prefix"`
	MaxEntries int    `yaml:"max_entries" json:"max_entries"`
}

// GetLoggingConfig 返回统一日志配置；缺省段时回落到日志包默认配置。
func GetLoggingConfig() logger.LoggingConfig {
	return GetAppConfig().Logging
}

func GetServerBaseURL() string {
	// 路径解析与 loadAppConfigOnce 共用同一条搜索链（CF-05）：此前这里裸读
	// "config.yaml" 单路径，与主装载的多路搜索口径分裂。逐次重读+展开的语义
	// 不动（env_expand_test.go 钉住：同一进程内改 env 立即生效），只统一找文件的方式。
	for _, p := range configSearchPaths() {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var cfg platformAPIYAML
		// config.yaml 里写的是 `${PLATFORM_API_URL:http://127.0.0.1:8205}`；
		// 不先展开就直接 Unmarshal，拿到的是字面占位符，会被拼进跟踪/退订链接。
		if yaml.Unmarshal([]byte(expandEnvWithDefault(string(data))), &cfg) == nil && cfg.PlatformAPI.BaseURL != "" {
			return cfg.PlatformAPI.BaseURL
		}
	}
	if v := os.Getenv("SERVER_API_BASE"); v != "" {
		return v
	}
	return DefaultPlatformBaseURL
}

// GetAppConfig 获取应用配置
// 本系统仅使用 PostgreSQL，缺少 config.yaml 时使用 Docker 网络默认配置，
// 由 merchant_init 流程在部署阶段确保 config.yaml 已生成。
//
// 实现说明：
//   - 未注入时按需懒加载：读取 config.yaml 并展开 ${VAR:default}，不修改全局状态
//   - 测试场景可通过 SetAppConfig 注入自定义配置（用 nil 重置）
func GetAppConfig() AppConfig {
	if appConfig != nil {
		return *appConfig
	}
	return loadAppConfigOnce()
}

// SetAppConfig 注入应用配置（测试场景专用）
//
// 生产代码请勿调用——它会把注入值一直保留到下一次 SetAppConfig(nil)。
// 仅用于单测替换配置，验证 GetPublicBaseURL 等函数对配置的读取行为。
func SetAppConfig(cfg *AppConfig) {
	if cfg == nil {
		appConfig = nil
		return
	}
	appConfig = cfg
}

var appConfig *AppConfig

// configSearchPaths 返回 config.yaml 的搜索链（CF-05，2026-11-12 §3.17 配置审计批）。
//
// CONFIG_PATH 环境变量显式设置时插到链头（未设置时链与历史逐字节一致，行为零变更）；
// 其余为：CWD → 父目录 → user-server 子目录（兼容从仓库根/从子目录启动两种姿势）→
// 可执行文件目录及其父目录（兼容编译产物单文件部署）。
// GetServerBaseURL 与 loadAppConfigOnce 共用本链，两处口径不再分裂。
func configSearchPaths() []string {
	paths := make([]string, 0, 6)
	if p := os.Getenv("CONFIG_PATH"); p != "" {
		paths = append(paths, p)
	}
	paths = append(paths, "config.yaml", "../config.yaml", "user-server/config.yaml")
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		paths = append(paths,
			filepath.Join(exeDir, "config.yaml"),
			filepath.Join(exeDir, "..", "config.yaml"),
		)
	}
	return paths
}

func loadAppConfigOnce() AppConfig {
	var config AppConfig

	configPaths := configSearchPaths()

	var data []byte
	var err error
	for _, p := range configPaths {
		data, err = os.ReadFile(p)
		if err == nil {
			logger.Infof("[config] 从 %s 加载 config.yaml", p)
			break
		}
	}
	if err != nil {
		// 沉默失败家族（CF-05）：config.yaml 缺失曾静默回落全默认值，部署方从
		// 非预期工作目录启动时拿到的是 Docker 网络默认档，症状是"我的配置没生效"
		// 却没有任何日志线索。这里响亮一行把尝试过的路径和 CWD 都说清——
		// 默认值回落本身保留（config.yaml 缺失不挡启动是既有口径），只补可观测性。
		logger.Warnf("[config] 搜索链内未找到 config.yaml（尝试过 %v，CWD=%s），已回落内置默认配置——"+
			"若这不是预期，请用 CONFIG_PATH 指向配置文件或从正确目录启动", configPaths, func() string {
			wd, e := os.Getwd()
			if e != nil {
				return "<unknown>"
			}
			return wd
		}())
		config.Database.Type = DBTypePostgres
		config.Database.Postgres.Host = "postgres-user"
		config.Database.Postgres.Port = DefaultDBPortDocker
		config.Database.Postgres.User = "admin"
		config.Database.Postgres.Password = os.Getenv("POSTGRES_PASSWORD")
		config.Database.Postgres.DBName = "user_db"
		config.Database.Postgres.SSLMode = "disable"
		config.VectorDatabase.Type = VectorDBTypePGVector
		config.VectorDatabase.PGVector.Table = "knowledge_embeddings"
		config.VectorDatabase.PGVector.Dimension = 1024
		config.Inference = DefaultInferenceConfig()
		return config
	}

	err = yaml.Unmarshal([]byte(expandEnvWithDefault(string(data))), &config)
	if err != nil {
		panic(err)
	}

	config.Database.Type = DBTypePostgres

	return config
}
