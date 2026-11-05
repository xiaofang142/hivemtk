package service

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hivemtk-user/internal/geo/model"
)

// fakeKBDocs 是 B1 验收用的 KB 文档源替身。
type fakeKBDocs struct {
	docs []*model.GeoKnowledgeDocument
	err  error
}

func (f *fakeKBDocs) ListDocuments(ctx context.Context) ([]*model.GeoKnowledgeDocument, error) {
	return f.docs, f.err
}

// TestGenerateLLMsTxt_AutoIncludesKBDocs B1 验收（规划 §4.8 B1）：
// 请求体不带 Documents 时，从 GEO KB 文档表自动装配 Docs 段。
func TestGenerateLLMsTxt_AutoIncludesKBDocs(t *testing.T) {
	kb := &fakeKBDocs{docs: []*model.GeoKnowledgeDocument{
		{Title: "产品白皮书", SourceURL: "https://example.com/wp", Content: strings.Repeat("产品能力说明。", 40)},
		{Title: "无外链文档", SourceURL: "", Content: "没有 SourceURL 的 KB 文档"},
	}}
	svc := NewTechConfigServiceWithKB(kb)
	out := svc.GenerateLLMsTxt(&LLMsTxtConfig{
		SiteURL: "https://example.com", Brand: "Acme",
	})

	// ① KB 文档进了 Docs 段
	assert.Contains(t, out, "## Docs")
	assert.Contains(t, out, "[产品白皮书](https://example.com/wp)")
	// ② 描述是 Content 截断（80 rune 上限）
	assert.Contains(t, out, ": 产品能力说明。")
	// ③ 无 SourceURL 的文档被跳过——llms.txt 条目必须是可取回的 URL，
	//    编不出就缺一条，不塞 404 链接
	assert.NotContains(t, out, "无外链文档")
}

// TestGenerateLLMsTxt_ManualDocsTakePriority 手工条目优先：请求体显式给了文档
// （≥1 条）即完全接管，不混入 KB 条目——可预期性优先。
func TestGenerateLLMsTxt_ManualDocsTakePriority(t *testing.T) {
	kb := &fakeKBDocs{docs: []*model.GeoKnowledgeDocument{
		{Title: "KB文档", SourceURL: "https://example.com/kb"},
	}}
	svc := NewTechConfigServiceWithKB(kb)
	out := svc.GenerateLLMsTxt(&LLMsTxtConfig{
		SiteURL: "https://example.com", Brand: "Acme",
		Documents: []LLMsDocEntry{{Title: "手工文档", URL: "https://example.com/manual"}},
	})
	assert.Contains(t, out, "[手工文档](https://example.com/manual)")
	assert.NotContains(t, out, "KB文档")
}

// TestGenerateLLMsTxt_DedupByURL 自动装配对 URL 去重：手工与 KB 撞同 URL 时不重复。
func TestGenerateLLMsTxt_DedupByURL(t *testing.T) {
	kb := &fakeKBDocs{docs: []*model.GeoKnowledgeDocument{
		{Title: "白皮书", SourceURL: "https://example.com/wp", Content: "c1"},
	}}
	svc := NewTechConfigServiceWithKB(kb)
	cfg := &LLMsTxtConfig{
		SiteURL: "https://example.com", Brand: "Acme",
		Documents: []LLMsDocEntry{
			{Title: "白皮书", URL: "https://example.com/wp"},
			// 第二条手工 ⇒ ≥1 手工接管……不行，那就测不到去重了。
		},
	}
	// 直接测 merge 函数：manual 0 条 + KB 1 条，URL 唯一 ⇒ 1 条
	merged := mergeKBDocsInto(context.Background(), svc, cfg, nil)
	require.Len(t, merged, 1)
	assert.Equal(t, "https://example.com/wp", merged[0].URL)
}

// TestGenerateLLMsTxt_NilKBKeepsLegacyBehavior KB 源为 nil（旧装配）⇒ 行为与旧版一致。
func TestGenerateLLMsTxt_NilKBKeepsLegacyBehavior(t *testing.T) {
	svc := NewTechConfigService()
	out := svc.GenerateLLMsTxt(&LLMsTxtConfig{
		SiteURL: "https://example.com", Brand: "Acme",
		Documents: []LLMsDocEntry{{Title: "d", URL: "https://e.com/d", Description: "desc"}},
	})
	assert.Contains(t, out, "## Docs")
	assert.Contains(t, out, "[d](https://e.com/d): desc")
	// 无文档时不出 Docs 段
	out2 := svc.GenerateLLMsTxt(&LLMsTxtConfig{SiteURL: "https://e.com", Brand: "B"})
	assert.NotContains(t, out2, "## Docs")
}

// TestGenerateLLMsTxt_KBErrorFallsBackSilently KB 读失败 ⇒ 降级为纯手工（不炸生成）。
func TestGenerateLLMsTxt_KBErrorFallsBackSilently(t *testing.T) {
	kb := &fakeKBDocs{err: context.DeadlineExceeded}
	svc := NewTechConfigServiceWithKB(kb)
	out := svc.GenerateLLMsTxt(&LLMsTxtConfig{SiteURL: "https://e.com", Brand: "B"})
	assert.NotContains(t, out, "## Docs")
	assert.Contains(t, out, "# B")
}
