package rag_service

import (
	"encoding/json"
	"strings"
	"testing"

	rag_core "hivemtk-user/internal/aiagent/rag/core"
)

func TestBuildContextStringEmptyYieldsNoEvidence(t *testing.T) {
	if got := buildContextString(nil); got != "未找到相关文档。" {
		t.Errorf("空召回提示语=%q", got)
	}
	if got := buildContextString([]rag_core.Chunk{}); got != "未找到相关文档。" {
		t.Errorf("零长切片应同 nil, got %q", got)
	}
}

func TestBuildContextStringNumbersAndFormatsScore(t *testing.T) {
	chunks := []rag_core.Chunk{
		{ID: "c1", DocumentID: "doc-a", Content: "退款政策 7 天", Score: 0.8765},
		{ID: "c2", DocumentID: "doc-b", Content: "运费说明", Score: 0.5},
	}
	got := buildContextString(chunks)

	for _, want := range []string{
		"参考信息:\n",
		"[1] 来源: doc-a (相似度: 0.88)\n退款政策 7 天",
		"[2] 来源: doc-b (相似度: 0.50)\n运费说明",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("缺少片段 %q，实际输出:\n%s", want, got)
		}
	}
	if strings.Count(got, "[") != 2 {
		t.Errorf("编号数量错:\n%s", got)
	}
	if strings.Contains(got, "c1") {
		t.Error("Chunk.ID 不应出现在上下文里，只允许 DocumentID")
	}
}

func TestBuildRAGPromptStructure(t *testing.T) {
	base := buildRAGPrompt("多久能到？", "参考信息正文", nil)
	if !strings.HasPrefix(base, "基于以下参考信息回答问题") {
		t.Errorf("缺少指令头:\n%s", base)
	}
	for _, want := range []string{"参考信息正文", "问题: 多久能到？", "回答:"} {
		if !strings.Contains(base, want) {
			t.Errorf("缺少片段 %q", want)
		}
	}
	if strings.Contains(base, "额外上下文") {
		t.Error("contextData 为空时不应追加额外上下文段")
	}

	withCtx := buildRAGPrompt("q", "ctx", map[string]any{"channel": "wecom"})
	if !strings.Contains(withCtx, "额外上下文:") {
		t.Fatalf("应追加额外上下文:\n%s", withCtx)
	}
	tail := withCtx[strings.Index(withCtx, "额外上下文:"):]
	var decoded map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(tail, "额外上下文:"))), &decoded); err != nil {
		t.Fatalf("额外上下文不是合法 JSON: %v (%s)", err, tail)
	}
	if decoded["channel"] != "wecom" {
		t.Errorf("额外上下文内容错: %v", decoded)
	}

	// 既有行为：contextData 不可序列化时 json.Marshal 报错被吞掉，追加的是空串
	broken := buildRAGPrompt("q", "ctx", map[string]any{"bad": make(chan int)})
	if !strings.HasSuffix(broken, "额外上下文: ") {
		t.Errorf("序列化失败时应追加空的额外上下文段（既有行为）, got tail=%q",
			broken[strings.Index(broken, "额外上下文:"):])
	}
}

func TestBuildStructuredRAGPromptEmbedsSchema(t *testing.T) {
	schema := map[string]any{"type": "object", "required": []string{"answer"}}
	got := buildStructuredRAGPrompt("有什么颜色", "ctx 正文", map[string]any{"uid": "u1"}, schema)

	for _, want := range []string{"请严格按照以下JSON Schema返回结果:", "问题: 有什么颜色", "ctx 正文", `"required"`, `"answer"`, "额外上下文:"} {
		if !strings.Contains(got, want) {
			t.Errorf("缺少片段 %q:\n%s", want, got)
		}
	}
	start := strings.Index(got, "请严格按照以下JSON Schema返回结果:")
	schemaLine := strings.TrimSpace(strings.Split(got[start:], "\n")[1])
	var back map[string]any
	if err := json.Unmarshal([]byte(schemaLine), &back); err != nil {
		t.Fatalf("schema 段不是合法 JSON: %v (%q)", err, schemaLine)
	}
	if bt, ok := back["type"].(string); !ok || bt != "object" {
		t.Errorf("schema type 丢失: %v", back)
	}

	noSchema := buildStructuredRAGPrompt("q", "ctx", nil, nil)
	if !strings.Contains(noSchema, "null") {
		t.Errorf("nil schema 应序列化为 null:\n%s", noSchema)
	}
	if strings.Contains(noSchema, "额外上下文") {
		t.Error("nil contextData 不应追加额外上下文段")
	}
	// 既有行为：schema 不可序列化时 schema 段是空串（marshal 错误被吞）
	if bad := buildStructuredRAGPrompt("q", "ctx", nil, make(chan int)); strings.Contains(bad, "chan") ||
		!strings.Contains(bad, "回答:") {
		t.Errorf("不可序列化 schema 时 schema 段应为空串（既有行为）, got:\n%s", bad)
	}
}
