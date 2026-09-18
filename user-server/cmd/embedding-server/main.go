// Command embedding-server 是私域部署的本地 Embedding HTTP 服务（私域基线）
//
// 用途：
//   - 替代 TEI（ghcr.io/huggingface/text-embeddings-inference）容器
//   - 解决国内网络无法稳定拉取 ghcr.io 镜像的问题
//   - 提供 OpenAI 兼容的 /v1/embeddings 接口，user-server 无需修改调用代码
//   - 纯 Go 实现，无 Python / 无 ONNX / 无外部模型下载
//
// 算法：基于字符 n-gram TF-IDF + 随机投影（详见 internal/embedding/local）
//
// 启动（端口派生自 config.DefaultEmbeddingPort=8208，DEVELOPMENT.md §2.4 端口对照表）：
//
//	EMBEDDING_PORT=8208 ./embedding-server
//	或 docker compose up embedding
//
// 协议：
//
//	POST /v1/embeddings
//	{"model": "BAAI/bge-base-zh-v1.5", "input": ["文本1", "文本2"]}
//	→
//	{"object": "list", "data": [{"object": "embedding", "index": 0, "embedding": [0.1, 0.2, ...]}], "model": "...", "usage": {...}}
//
// 性能：
//   - 单条 ~2-5ms（中文 100 字以内）
//   - 批量 10 条 ~15-30ms
//   - 768 维 float32 = 3KB/向量
//   - 内存：~1GB（含 768*16384 投影矩阵）
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"hivemtk-user/internal/aiagent/embedding"
	"hivemtk-user/internal/config"
	"hivemtk-user/internal/pkg/utils/logger"
)

// EmbeddingRequest 通用 OpenAI 兼容请求
type EmbeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

// EmbeddingItem 单个向量结果
type EmbeddingItem struct {
	Object    string    `json:"object"`
	Index     int       `json:"index"`
	Embedding []float32 `json:"embedding"`
}

// Usage token 统计（粗略按字符数计）
type Usage struct {
	PromptTokens int `json:"prompt_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// EmbeddingResponse 通用 OpenAI 兼容响应
type EmbeddingResponse struct {
	Object string          `json:"object"`
	Data   []EmbeddingItem `json:"data"`
	Model  string          `json:"model"`
	Usage  Usage           `json:"usage"`
}

// ErrorResponse 错误响应（OpenAI 格式）
type ErrorResponse struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

// Server 持有引擎实例
type Server struct {
	engine     *embedding.LocalEmbedding
	defaultDim int
	defaultMod string
	startTime  time.Time
	mu         sync.RWMutex
}

func main() {
	port := flag.Int("port", envInt("EMBEDDING_PORT", config.DefaultEmbeddingPort), "HTTP 监听端口")
	dim := flag.Int("dim", envInt("EMBEDDING_DIM", 1024), "向量维度（与 BGE 模型一致）")
	model := flag.String("model", envStr("EMBEDDING_MODEL", "bge-m3"), "模型名（仅作声明，实际使用本地算法）")
	seed := flag.Int64("seed", 42, "投影矩阵随机种子（必须固定以保证决定性）")
	flag.Parse()

	logger.Infof("[embedding-server] 启动 dim=%d model=%s port=%d", *dim, *model, *port)

	eng := embedding.NewLocalEmbedding(*dim, *seed)
	srv := &Server{
		engine:     eng,
		defaultDim: *dim,
		defaultMod: *model,
		startTime:  time.Now(),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", srv.handleHealth)
	mux.HandleFunc("/v1/embeddings", srv.handleEmbed)
	mux.HandleFunc("/", srv.handleRoot)

	httpSrv := &http.Server{
		Addr:              fmt.Sprintf("0.0.0.0:%d", *port),
		Handler:           logMiddleware(mux),
		ReadHeaderTimeout: 30 * time.Second,
		ReadTimeout:       120 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	logger.Infof("[embedding-server] 监听 %s", httpSrv.Addr)
	if err := httpSrv.ListenAndServe(); err != nil {
		logger.Errorf("[embedding-server] 启动失败: %v", err)
		os.Exit(1)
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	resp := map[string]any{
		"status":      "ok",
		"dim":         s.defaultDim,
		"model":       s.defaultMod,
		"uptime_secs": int(time.Since(s.startTime).Seconds()),
		"engine":      "local-ngram-tfidf-randproj",
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = fmt.Fprintf(w, "local embedding-server (Go)\nmodel=%s\ndim=%d\nPOST /v1/embeddings\n", s.defaultMod, s.defaultDim)
}

func (s *Server) handleEmbed(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	body, err := readAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read body: "+err.Error(), "invalid_request_error")
		return
	}

	var req EmbeddingRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error(), "invalid_request_error")
		return
	}
	if len(req.Input) == 0 {
		writeError(w, http.StatusBadRequest, "input 不能为空", "invalid_request_error")
		return
	}
	if len(req.Input) > 256 {
		writeError(w, http.StatusBadRequest, "单次最多 256 条", "invalid_request_error")
		return
	}
	if req.Model == "" {
		req.Model = s.defaultMod
	}

	cleanInputs := make([]string, len(req.Input))
	for i, t := range req.Input {
		cleanInputs[i] = strings.TrimSpace(t)
	}

	s.mu.RLock()
	vectors := s.engine.EmbedBatch(cleanInputs)
	s.mu.RUnlock()

	data := make([]EmbeddingItem, len(vectors))
	var totalChars int
	for i, v := range vectors {
		data[i] = EmbeddingItem{
			Object:    "embedding",
			Index:     i,
			Embedding: v,
		}
		totalChars += len([]rune(cleanInputs[i]))
	}

	resp := EmbeddingResponse{
		Object: "list",
		Data:   data,
		Model:  req.Model,
		Usage: Usage{
			PromptTokens: totalChars,
			TotalTokens:  totalChars,
		},
	}
	writeJSON(w, http.StatusOK, resp)
}

func readAll(r interface {
	Read([]byte) (int, error)
}) ([]byte, error) {
	var buf bytes.Buffer
	tmp := make([]byte, 4096)
	for {
		n, err := r.Read(tmp)
		if n > 0 {
			buf.Write(tmp[:n])
		}
		if err != nil {
			if err.Error() == "EOF" {
				return buf.Bytes(), nil
			}
			return buf.Bytes(), err
		}
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg, errType string) {
	var er ErrorResponse
	er.Error.Message = msg
	er.Error.Type = errType
	er.Error.Code = strings.ToLower(strings.ReplaceAll(errType, "_", "-"))
	writeJSON(w, code, er)
}

func logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(ww, r)
		logger.Infof("[embedding-server] %s %s %d %dB %s",
			r.Method, r.URL.Path, ww.status, ww.bytes, time.Since(start))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
