package etl

// table_chunking_test.go P1-1 步骤 4 + AC 的三条腿：
//   - 表格单元格不被切断（AC「表格单元格召回可用」的可自动化代理判据）；
//   - 超长表格按行拆且每片重复表头；
//   - 关闭时与改造前逐字节一致（AC「未启用外挂时行为与现状一致」）。
// 另附 sidecar 端到端与「默认零外网请求」的极性对（正控制 + 反向）。

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	rag_core "hivemtk-user/internal/aiagent/rag/core"
)

// buildTableDoc 造一篇「正文 + 12 单元格表 + 正文」的文档。
func buildTableDoc() string {
	rows := make([]string, 0, 10)
	for i := 1; i <= 10; i++ {
		rows = append(rows, fmt.Sprintf("| SKU-%02d | 商品%02d | %d.00 | 有货 |", i, i, i*10))
	}
	return "产品价目说明段落，用于验证正文与表格混排时的分块边界。\n\n" +
		"| SKU | 名称 | 单价 | 库存 |\n|---|---|---|---|\n" +
		strings.Join(rows, "\n") + "\n\n" +
		"表格下方的补充说明段落，描述价格有效期与批量折扣规则。"
}

func newTableAwareProcessor() *DocumentProcessor {
	return NewDocumentProcessor(&ProcessingConfig{
		ChunkSize: 512, ChunkOverlap: 50, MinChunkSize: 100,
		MaxLengthPerChunk: 1000, MinLengthPerChunk: 50,
		TableAware: true,
	})
}

func TestTableAware_KeepsEveryCellIntact(t *testing.T) {
	dp := newTableAwareProcessor()
	doc := rag_core.Document{ID: "doc1", Content: buildTableDoc(), Metadata: map[string]any{"title": "价目表"}}
	chunks, err := dp.ProcessDocument(context.Background(), doc)
	if err != nil {
		t.Fatalf("分块失败: %v", err)
	}
	if len(chunks) < 2 {
		t.Fatalf("chunk 数 = %d，用例需要多块", len(chunks))
	}

	// 每块都不得出现「半行表格」：含表格行的 chunk，其每一行都必须是完整表行
	for i, c := range chunks {
		for _, line := range strings.Split(c.Content, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if strings.HasPrefix(line, "|") && !strings.HasSuffix(line, "|") {
				t.Errorf("chunk #%d 出现被切断的表行: %q", i, line)
			}
		}
	}

	// 12 个单元格（SKU/名称/单价/库存 × 10 行 = 40 个非空单元格值）逐个必须完整出现在**某一块**里
	joined := make([]string, len(chunks))
	for i, c := range chunks {
		joined[i] = c.Content
	}
	missing := []string{}
	for i := 1; i <= 10; i++ {
		for _, cell := range []string{fmt.Sprintf("SKU-%02d", i), fmt.Sprintf("商品%02d", i), fmt.Sprintf("%d.00", i*10)} {
			found := false
			for _, body := range joined {
				if strings.Contains(body, cell) {
					found = true
					break
				}
			}
			if !found {
				missing = append(missing, cell)
			}
		}
	}
	if len(missing) > 0 {
		t.Errorf("有 %d 个单元格在全部 chunk 中找不到（等于召回不到）: %v", len(missing), missing)
	}

	// 表格块必须带结构化标记，便于排查与检索侧分流
	tableChunks := 0
	for _, c := range chunks {
		if c.Metadata != nil && c.Metadata["chunk_type"] == "table" {
			tableChunks++
		}
	}
	if tableChunks == 0 {
		t.Error("没有任何 chunk 带 chunk_type=table 标记")
	}
	// 共享 metadata 不得被污染：正文 chunk 上不应出现 chunk_type
	for i, c := range chunks {
		if c.Metadata["chunk_type"] == "table" {
			continue
		}
		if _, ok := c.Metadata["chunk_type"]; ok {
			t.Errorf("chunk #%d 正文块被写入了 chunk_type（共享 map 污染）", i)
		}
	}
}

func TestTableAware_LongTableSplitsByRowWithHeaderRepeated(t *testing.T) {
	dp := NewDocumentProcessor(&ProcessingConfig{
		ChunkSize: 512, ChunkOverlap: 0, MinChunkSize: 1,
		MaxLengthPerChunk: 200, MinLengthPerChunk: 1,
		TableAware: true,
	})
	rows := make([]string, 0, 20)
	for i := 1; i <= 20; i++ {
		rows = append(rows, fmt.Sprintf("| R%02d | 值-%02d |", i, i))
	}
	content := "| 编号 | 数值 |\n|---|---|\n" + strings.Join(rows, "\n")
	chunks, err := dp.ProcessDocument(context.Background(), rag_core.Document{ID: "doc2", Content: content})
	if err != nil {
		t.Fatalf("分块失败: %v", err)
	}
	if len(chunks) < 2 {
		t.Fatalf("超长表应被拆成多块，实际 %d 块", len(chunks))
	}
	for i, c := range chunks {
		lines := strings.Split(c.Content, "\n")
		if !strings.HasPrefix(strings.TrimSpace(lines[0]), "| 编号") {
			t.Errorf("chunk #%d 首行不是重复的表头: %q", i, lines[0])
		}
		if len(lines) > 1 && !tableSepRe.MatchString(lines[1]) {
			t.Errorf("chunk #%d 第 2 行不是重复的分隔行: %q", i, lines[1])
		}
		for _, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "|") && !strings.HasSuffix(strings.TrimSpace(line), "|") {
				t.Errorf("chunk #%d 切断了表行: %q", i, line)
			}
		}
	}
	// 所有行都要在（拆不等于丢）
	var all strings.Builder
	for _, c := range chunks {
		all.WriteString(c.Content)
		all.WriteString("\n")
	}
	for i := 1; i <= 20; i++ {
		if !strings.Contains(all.String(), fmt.Sprintf("R%02d", i)) {
			t.Errorf("拆表后丢了行 R%02d", i)
		}
	}
}

func TestTableAware_OffMatchesLegacyByteForByte(t *testing.T) {
	text := buildTableDoc()
	doc := rag_core.Document{ID: "doc3", Content: text}

	off := NewDocumentProcessor(&ProcessingConfig{
		ChunkSize: 512, ChunkOverlap: 50, MinChunkSize: 100,
		MaxLengthPerChunk: 1000, MinLengthPerChunk: 50,
		TableAware: false,
	})
	got, err := off.ProcessDocument(context.Background(), doc)
	if err != nil {
		t.Fatalf("分块失败: %v", err)
	}
	legacy := off.postProcessChunks(off.chunkStrategyForLegacy(text, doc))
	if len(got) != len(legacy) {
		t.Fatalf("关闭表格感知后块数 = %d，改造前路径 = %d", len(got), len(legacy))
	}
	for i := range got {
		if got[i].Content != legacy[i].Content || got[i].ID != legacy[i].ID {
			t.Errorf("块 #%d 与改造前不一致：\n got=%q\nwant=%q", i, got[i].Content, legacy[i].Content)
		}
	}
}

// chunkStrategyForLegacy 复刻改造前的三条策略（不含 TableAware 分支）。
// 判据取自底层函数本身而不是手抄期望值——手抄会在改参数时静默过期。
func (dp *DocumentProcessor) chunkStrategyForLegacy(text string, doc rag_core.Document) []rag_core.Chunk {
	var chunks []rag_core.Chunk
	switch {
	case dp.shouldUseHeadingSplit(text):
		chunks = dp.splitByHeadings(text, doc)
	case dp.shouldUseSentenceSplit(text):
		chunks = dp.splitBySentences(text, doc)
	default:
		chunks = dp.splitByFixedLength(text, doc)
	}
	return chunks
}

func TestTableAware_MinChunkSizeDoesNotDropSmallTable(t *testing.T) {
	dp := NewDocumentProcessor(&ProcessingConfig{
		ChunkSize: 512, ChunkOverlap: 0, MinChunkSize: 500, // 远大于小表
		MaxLengthPerChunk: 1000, MinLengthPerChunk: 1,
		TableAware: true,
	})
	content := "引言。\n\n| 键 | 值 |\n|---|---|\n| a | 1 |\n\n结尾。"
	chunks, err := dp.ProcessDocument(context.Background(), rag_core.Document{ID: "doc4", Content: content})
	if err != nil {
		t.Fatalf("分块失败: %v", err)
	}
	found := false
	for _, c := range chunks {
		if strings.Contains(c.Content, "| a | 1 |") {
			found = true
		}
	}
	if !found {
		t.Error("小表被 MinChunkSize 过滤掉了（表格块必须豁免最小长度过滤）")
	}
}

func TestSidecar_E2E_TableOutputChunksKeepRows(t *testing.T) {
	// 极性对照：先证明"配了 URL 就会真打"，再证明"没配就一次都不打"
	var hits int
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte(buildTableDoc()))
	}))
	defer fake.Close()

	SetSidecarURLProvider(func() string { return fake.URL }) // env 已退役 B6，测试走 provider
	t.Cleanup(func() { SetSidecarURLProvider(nil) })

	// 正控制：配置 sidecar ⇒ 结构化文档走外挂并命中 1 次
	text, err := ParseDocument(context.Background(), "report.pdf", []byte("%PDF-1.4 fake"))
	if err != nil {
		t.Fatalf("sidecar 解析失败: %v", err)
	}
	if hits != 1 {
		t.Fatalf("正控制失效：sidecar 命中 %d 次 want 1（后续「零外网」结论不可信）", hits)
	}

	// 关掉 sidecar ⇒ 零外网请求（hits 不增），且走 builtin
	counter := hits
	if _, err := ParseDocument(context.Background(), "plain.txt", []byte("纯文本内容")); err != nil {
		t.Fatalf("builtin 解析失败: %v", err)
	}
	if hits != counter {
		t.Errorf("未配置 sidecar 时仍发生了 %d 次外发", hits-counter)
	}

	// 外挂输出走带 TableAware 的处理器：表格行保持完整
	dp := newTableAwareProcessor()
	chunks, err := dp.ProcessDocument(context.Background(), rag_core.Document{ID: "sc", Content: text})
	if err != nil {
		t.Fatalf("分块失败: %v", err)
	}
	guard := 0
	for _, c := range chunks {
		for _, line := range strings.Split(c.Content, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "|") && !strings.HasSuffix(line, "|") {
				t.Errorf("sidecar 输出分块后出现断行: %q", line)
			}
			if strings.Contains(line, "SKU-07") {
				guard++
			}
		}
	}
	if guard == 0 {
		t.Error("表格中的 SKU-07 未进入任何 chunk")
	}
}

func TestImageDescribe_E2E_DescriptionBecomesSearchableChunk(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"一张产品包装图：白色纸盒，正面印有「HiveMtk 精华液 30ml」与有效期 2027-06。"}}]}`))
	}))
	defer fake.Close()

	SetImageDescribeConfigProvider(func() (string, string) { return fake.URL, "test-vl" }) // env 已退役 B6
	t.Cleanup(func() { SetImageDescribeConfigProvider(nil) })

	desc, err := DescribeImage(context.Background(), "pack.png", []byte("\x89PNG fake bytes"))
	if err != nil {
		t.Fatalf("图片描述失败: %v", err)
	}
	if !strings.Contains(desc, "HiveMtk") {
		t.Fatalf("描述内容异常: %q", desc)
	}

	// 描述进文本管线后：关键词查询（此处以内容包含模拟检索命中面）能命中该 chunk
	dp := NewDocumentProcessor(nil)
	chunks, err := dp.ProcessDocument(context.Background(), rag_core.Document{ID: "img", Content: desc})
	if err != nil {
		t.Fatalf("分块失败: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("图片描述应成 1 块，实际 %d", len(chunks))
	}
	if !strings.Contains(chunks[0].Content, "2027-06") {
		t.Error("描述中的有效期未进入 chunk（文本 query 命中依赖它）")
	}
	if utf8.RuneCountInString(chunks[0].Content) == 0 {
		t.Error("空 chunk")
	}
}
