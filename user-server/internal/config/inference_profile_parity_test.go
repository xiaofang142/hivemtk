package config

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestDefaultModelsMatchEnvExample 锁定「Go 回落默认模型名」与「安装脚本实际下载的模型」同源。
//
// 为什么要这条断言：DefaultInferenceConfig() 是 config.yaml 缺失时的回落值，
// 消费方（dispatcher 等）不做任何兜底。而宿主机推理栈由 scripts/inference-host/_common.sh
// 用 .env 的 LLM_SERVED_NAME / EMBEDDING_SERVED_NAME / RERANK_SERVED_NAME 作为
// llama-server --alias 启动；.env 由 Makefile 从 .env-example 复制而来。
// 一旦回落常量落后于 .env-example 的档位（历史上 LLM 从 1.5B 调到 3B 时此处未动），
// 回落路径就会把一个本机根本没下载的模型名发给 llama-server。
func TestDefaultModelsMatchEnvExample(t *testing.T) {
	env := readEnvExample(t)

	cases := []struct {
		role string
		key  string
		got  string
	}{
		{"LLM", "LLM_SERVED_NAME", DefaultLLMModel()},
		{"Embedding", "EMBEDDING_SERVED_NAME", DefaultEmbeddingModel()},
		{"Rerank", "RERANK_SERVED_NAME", DefaultRerankModel()},
	}

	for _, tc := range cases {
		t.Run(tc.role, func(t *testing.T) {
			want, ok := env[tc.key]
			if !ok {
				t.Fatalf(".env-example 缺少 %s，无法核验回落默认值是否与安装档位同源", tc.key)
			}
			if tc.got != want {
				t.Errorf("回落默认 %s 模型名(%s) 与 .env-example %s(%s) 不同源",
					tc.role, tc.got, tc.key, want)
			}
		})
	}

	t.Run("LLM_MODEL_与_SERVED_NAME_一致", func(t *testing.T) {
		// .env-example 自己也得内部自洽：user-server 读 LLM_MODEL，推理栈读 LLM_SERVED_NAME，
		// 两者不等则同一次安装里后端与推理服务就会各说各话。
		if env["LLM_MODEL"] != env["LLM_SERVED_NAME"] {
			t.Errorf(".env-example 内部不一致：LLM_MODEL(%s) != LLM_SERVED_NAME(%s)",
				env["LLM_MODEL"], env["LLM_SERVED_NAME"])
		}
	})
}

// TestNoStaleModelNameLiteral 锁住「模型名字面量只准出现在 getter 里」这条仓内约定。
//
// 为什么要有这条断言：ai_agents.llm_model 有三个写死的回落值，全是**上一代推理栈**留下的——
// gorm 列默认 `smollm3-3b-4bit-mlx`（MLX 时代）、controller/service 建号时同款字面量、
// 迁移里给"默认销售智能体"塞 `gpt-4o-mini`、客服回复生成器 nil-config 回落 `gpt-3.5-turbo`。
// 消费方（sales_engine → LLM 请求的 model 字段）不做兜底换算，宿主机 llama-server 只认
// .env 的 LLM_SERVED_NAME ⇒ 这些名字发出去就是"指向一个本机没跑的模型"。
// 更糟的是列默认与代码回落不一致，同一次安装里两条路径写出来的行就不一样。
func TestNoStaleModelNameLiteral(t *testing.T) {
	// "default" 不是模型名而是 **provider 名**：dispatcher_register.go 以此注册默认 provider，
	// sales_engine.go 的 case "default" 认它。llm_model 字段被复用来存 provider 选择，
	// 这是既有契约（本断言不扩到改它），所以放行。
	allowed := map[string]bool{DefaultLLMModel(): true, "default": true, "": true}

	assignRe := regexp.MustCompile(`\bLLMModel\s*[:=]\s*"([^"]*)"`)
	// gorm 的 default 有两种写法：`default:'x'`（带引号，ai_agent.go）与 `default:x`（不带，
	// rag_product.go）；标签内 json/gorm 的先后顺序也不固定。两种写法、两种顺序都要吃进来，
	// 只认其中一档就是给下一列硬编码留门。
	tagRe := regexp.MustCompile("(?m)^\\s*LLMModel\\s+\\w+\\s+`[^`]*?gorm:\"[^\"]*default:'?([^'\"\\s`;]+)'?")

	seen := 0
	err := filepath.Walk("..", func(path string, info fs.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		check := func(kind string, re *regexp.Regexp, body, path string) {
			for _, m := range re.FindAllStringSubmatch(body, -1) {
				seen++
				if allowed[m[1]] {
					continue
				}
				t.Errorf("%s 以%s硬编码 LLM 模型名 %q，与 .env-example 的 LLM_SERVED_NAME(%s) 不同源；改用 DefaultLLMModel()",
					path, kind, m[1], DefaultLLMModel())
			}
		}
		text := string(body)
		check("字段回落", assignRe, text, path)
		check("gorm 列默认（AutoMigrate 会写进 schema）", tagRe, text, path)
		return nil
	})
	if err != nil {
		t.Fatalf("遍历 internal/ 失败: %v", err)
	}
	// 命中数为 0 不是"通过"，是断言瞎了：至少要有 gorm 列默认与若干回落值被查到。
	if seen == 0 {
		t.Fatal("正则一处都没命中 ⇒ 本用例判据失效，而不是代码干净")
	}
	t.Logf("检查 %d 处模型名字面量", seen)
}

// readEnvExample 以 KEY=VALUE 末值语义解析仓库根目录的 .env-example。
//
// 只取 KEY=VALUE 形态、去掉行首 export 与行尾空白；不展开变量、不执行任何一行。
func readEnvExample(t *testing.T) map[string]string {
	t.Helper()

	const path = "../../../.env-example"
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("打开 %s 失败: %v", path, err)
	}
	defer f.Close()

	out := map[string]string{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 256*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		if !strings.HasPrefix(k, "LLM_") && !strings.HasPrefix(k, "EMBEDDING_") && !strings.HasPrefix(k, "RERANK_") {
			continue
		}
		out[k] = strings.Trim(strings.TrimSpace(v), `"'`)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("读取 %s 失败: %v", path, err)
	}
	return out
}
