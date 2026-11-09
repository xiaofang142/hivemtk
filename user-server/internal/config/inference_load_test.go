package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestDefaultInferenceConfig 验证 DefaultInferenceConfig() 返回值的契约
//
// 当 config.yaml 缺失时（如 Docker 首次启动），GetAppConfig 会回落到此默认值。
// 消费方（dispatcher/embedding/rerank）完全信任此返回值，不做任何兜底，
// 因此每个字段都必须是非空、合法、符合私域基线。
func TestDefaultInferenceConfig(t *testing.T) {
	c := DefaultInferenceConfig()

	t.Run("Profile", func(t *testing.T) {
		if c.Profile != "dev" {
			t.Errorf("profile 应为 dev，实际 %s", c.Profile)
		}
	})

	t.Run("Embedding_NonEmpty", func(t *testing.T) {
		if c.Embedding.Model == "" {
			t.Error("embedding.model 不能为空（dispatcher 不再兜底）")
		}
		if c.Embedding.BaseURL == "" {
			t.Error("embedding.base_url 不能为空")
		}
		if c.Embedding.Dimension <= 0 {
			t.Error("embedding.dimension 必须为正数")
		}
	})

	t.Run("Rerank_NonEmpty", func(t *testing.T) {
		if c.Rerank.Model == "" {
			t.Error("rerank.model 不能为空")
		}
		if c.Rerank.BaseURL == "" {
			t.Error("rerank.base_url 不能为空")
		}
	})

	t.Run("LLM_NonEmpty", func(t *testing.T) {
		if c.LLM.Model == "" {
			t.Error("llm.model 不能为空（dispatcher 不再兜底）")
		}
		if c.LLM.BaseURL == "" {
			t.Error("llm.base_url 不能为空（dispatcher 不再兜底）")
		}
		if c.LLM.Temperature < 0 || c.LLM.Temperature > 2 {
			t.Errorf("llm.temperature 应在 [0,2]，实际 %f", c.LLM.Temperature)
		}
		if c.LLM.MaxTokens <= 0 {
			t.Error("llm.max_tokens 必须为正数")
		}
		if c.LLM.TimeoutSeconds <= 0 {
			t.Error("llm.timeout_seconds 必须为正数")
		}
		if c.LLM.MaxRetries < 0 {
			t.Error("llm.max_retries 不能为负数")
		}
	})

	t.Run("PrivateDomainBaseline", func(t *testing.T) {
		if c.Embedding.Mode != InferenceModeLocal {
			t.Errorf("embedding.mode 必须为 local（私域基线），实际 %s", c.Embedding.Mode)
		}
		if c.Rerank.Mode != InferenceModeLocal {
			t.Errorf("rerank.mode 必须为 local（私域基线），实际 %s", c.Rerank.Mode)
		}
		if c.LLM.Mode != InferenceModeLocal {
			t.Errorf("llm.mode 必须为 local（私域基线），实际 %s", c.LLM.Mode)
		}
		for name, url := range map[string]string{
			"embedding": c.Embedding.BaseURL,
			"rerank":    c.Rerank.BaseURL,
			"llm":       c.LLM.BaseURL,
		} {
			if !strings.Contains(url, "127.0.0.1") && !strings.Contains(url, "localhost") {
				t.Errorf("%s.base_url 必须指向 127.0.0.1/localhost（私域基线），实际 %s", name, url)
			}
		}
		if c.Embedding.AllowFallback {
			t.Error("embedding.allow_fallback 必须为 false（禁止哈希伪向量降级）")
		}
		if len(c.LLM.CloudProviders) != 0 {
			t.Errorf("默认配置不应启用 cloud_providers，实际有 %d 个", len(c.LLM.CloudProviders))
		}
	})

	t.Run("DimensionConsistency", func(t *testing.T) {
		if c.Embedding.Dimension != 1024 {
			t.Errorf("embedding.dimension 必须为 1024（pgvector 对齐），实际 %d", c.Embedding.Dimension)
		}
	})

	t.Run("URLSuffixConsistency", func(t *testing.T) {
		for name, url := range map[string]string{
			"embedding": c.Embedding.BaseURL,
			"rerank":    c.Rerank.BaseURL,
			"llm":       c.LLM.BaseURL,
		} {
			if !strings.HasSuffix(url, "/v1") {
				t.Errorf("%s.base_url 必须以 /v1 结尾（OpenAI 兼容契约），实际 %s", name, url)
			}
		}
	})
}

func isPrivateDomainBaseURL(u string) bool {
	lower := strings.ToLower(u)
	return strings.Contains(lower, "127.0.0.1") ||
		strings.Contains(lower, "localhost") ||
		strings.Contains(lower, "host.docker.internal") ||
		strings.Contains(lower, "mtk-llm") ||
		strings.Contains(lower, "mtk-embedding") ||
		strings.Contains(lower, "mtk-rerank")
}

func TestInferenceConfigLoads(t *testing.T) {
	// 本用例断言的是仓库里 config.yaml 的**私域基线**，不是测试期降级开关。
	// CI/Makefile 为了让 embedding 用例不连未启动的 8208，全局设了
	// EMBEDDING_ALLOW_FALLBACK=true；不在这里钉回 false，下面的
	// SecurityBaseline_PrivateDomain 会被测试环境变量污染而误红。
	t.Setenv("EMBEDDING_ALLOW_FALLBACK", "false")

	p := filepath.Join("..", "..", "config.yaml")
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("必须读取 %s：配置文件是测试前置条件（项目规则不允许跳过）：%v", p, err)
	}
	var c AppConfig
	if err := yaml.Unmarshal([]byte(expandEnvWithDefault(string(data))), &c); err != nil {
		t.Fatal(err)
	}

	t.Run("Profile_Dev", func(t *testing.T) {
		if c.Inference.Profile != "dev" {
			t.Fatalf("profile 应为 dev，实际 %s", c.Inference.Profile)
		}
	})

	t.Run("Embedding_Fields", func(t *testing.T) {
		if c.Inference.Embedding.Model != "bge-m3" {
			t.Errorf("embedding model 应为 bge-m3（dev 档），实际 %s", c.Inference.Embedding.Model)
		}
		if c.Inference.Embedding.Dimension != 1024 {
			t.Errorf("embedding 维度必须 1024（pgvector 对齐），实际 %d", c.Inference.Embedding.Dimension)
		}
		if !isPrivateDomainBaseURL(c.Inference.Embedding.BaseURL) {
			t.Errorf("embedding base_url 必须落在私域内（127.0.0.1/localhost/host.docker.internal/mtk-*），实际 %s", c.Inference.Embedding.BaseURL)
		}
		if c.Inference.Embedding.Mode != InferenceModeLocal {
			t.Errorf("embedding mode 应为 local，实际 %s", c.Inference.Embedding.Mode)
		}
	})

	t.Run("Rerank_Fields", func(t *testing.T) {
		if c.Inference.Rerank.Model != "bge-reranker-v2-m3" {
			t.Errorf("rerank model 应为 bge-reranker-v2-m3（dev 档），实际 %s", c.Inference.Rerank.Model)
		}
		if !isPrivateDomainBaseURL(c.Inference.Rerank.BaseURL) {
			t.Errorf("rerank base_url 必须落在私域内（127.0.0.1/localhost/host.docker.internal/mtk-*），实际 %s", c.Inference.Rerank.BaseURL)
		}
		if c.Inference.Rerank.Mode != InferenceModeLocal {
			t.Errorf("rerank mode 应为 local，实际 %s", c.Inference.Rerank.Mode)
		}
		if !c.Inference.Rerank.Enabled {
			t.Error("rerank 应启用")
		}
	})

	t.Run("LLM_Fields", func(t *testing.T) {
		if c.Inference.LLM.Model == "" {
			t.Error("llm model 不应为空")
		}
		if !isPrivateDomainBaseURL(c.Inference.LLM.BaseURL) {
			t.Errorf("llm base_url 必须落在私域内（127.0.0.1/localhost/host.docker.internal/mtk-*），实际 %s", c.Inference.LLM.BaseURL)
		}

		if c.Inference.LLM.Mode != InferenceModeLocal && c.Inference.LLM.Mode != InferenceModeRemote {
			t.Errorf("llm mode 应为 local 或 remote，实际 %s", c.Inference.LLM.Mode)
		}
		if c.Inference.LLM.Temperature <= 0 || c.Inference.LLM.Temperature > 2 {
			t.Errorf("llm temperature 应在 (0,2]，实际 %f", c.Inference.LLM.Temperature)
		}
		if c.Inference.LLM.MaxTokens <= 0 {
			t.Errorf("llm max_tokens 必须为正数，实际 %d", c.Inference.LLM.MaxTokens)
		}
		if c.Inference.LLM.TimeoutSeconds <= 0 {
			t.Errorf("llm timeout_seconds 必须为正数，实际 %d", c.Inference.LLM.TimeoutSeconds)
		}
		if c.Inference.LLM.MaxRetries < 0 {
			t.Errorf("llm max_retries 不能为负数，实际 %d", c.Inference.LLM.MaxRetries)
		}
	})

	t.Run("SecurityBaseline_PrivateDomain", func(t *testing.T) {
		if c.Inference.Embedding.AllowFallback {
			t.Error("embedding.allow_fallback 必须为 false（禁止哈希伪向量降级，私域基线）")
		}
		if c.Inference.LLM.APIKey != "" {
			t.Error("llm.api_key 应为空（本地模型无需密钥，私域基线）")
		}
		if len(c.Inference.LLM.CloudProviders) != 0 {
			t.Errorf("dev 档 cloud_providers 应为空（禁止云端 fallback），实际有 %d 个",
				len(c.Inference.LLM.CloudProviders))
		}
		for name, url := range map[string]string{
			"embedding": c.Inference.Embedding.BaseURL,
			"rerank":    c.Inference.Rerank.BaseURL,
			"llm":       c.Inference.LLM.BaseURL,
		} {
			if !isPrivateDomainBaseURL(url) {
				t.Errorf("%s.base_url 必须落在私域内（127.0.0.1/localhost/host.docker.internal/mtk-*，不出域），实际 %s", name, url)
			}
		}
	})

	t.Run("InternalConsistency_DimensionAlignment", func(t *testing.T) {
		if c.Inference.Embedding.Dimension != c.VectorDatabase.PGVector.Dimension {
			t.Errorf("embedding.dimension(%d) 必须与 pgvector.dimension(%d) 一致",
				c.Inference.Embedding.Dimension, c.VectorDatabase.PGVector.Dimension)
		}
		if c.Inference.Embedding.Dimension != 1024 {
			t.Errorf("embedding.dimension 必须为 1024（bge-m3 契约），实际 %d", c.Inference.Embedding.Dimension)
		}
	})

	t.Run("InternalConsistency_URLSuffix", func(t *testing.T) {
		for name, url := range map[string]string{
			"embedding": c.Inference.Embedding.BaseURL,
			"rerank":    c.Inference.Rerank.BaseURL,
			"llm":       c.Inference.LLM.BaseURL,
		} {
			if !strings.HasSuffix(url, "/v1") {
				t.Errorf("%s.base_url 必须以 /v1 结尾（OpenAI 兼容契约），实际 %s", name, url)
			}
		}
	})

	t.Run("Consistency_WithDefault", func(t *testing.T) {
		d := DefaultInferenceConfig()
		if c.Inference.Embedding.Model != d.Embedding.Model {
			t.Errorf("config.yaml embedding.model(%s) 与默认值(%s) 不一致",
				c.Inference.Embedding.Model, d.Embedding.Model)
		}
		if c.Inference.Rerank.Model != d.Rerank.Model {
			t.Errorf("config.yaml rerank.model(%s) 与默认值(%s) 不一致",
				c.Inference.Rerank.Model, d.Rerank.Model)
		}
		if c.Inference.LLM.Model == "" {
			t.Error("config.yaml llm.model 不应为空")
		}
	})
}
