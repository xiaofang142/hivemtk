package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func resetKnowledgeParamProviders() {
	maxUploadFileSizeProvider = func() int64 { return DefaultMaxUploadFileSize }
	merchantKnowledgeMaxLenProvider = func() int { return DefaultMerchantKnowledgeMaxLen }
}

func TestMaxUploadFileSizeSeam(t *testing.T) {
	resetKnowledgeParamProviders()
	t.Cleanup(resetKnowledgeParamProviders)

	t.Run("未注入时返回兜底值 50MB", func(t *testing.T) {
		require.Equal(t, int64(50<<20), MaxUploadFileSize())
	})

	t.Run("注入后生效", func(t *testing.T) {
		SetMaxUploadFileSizeProvider(func() int64 { return 20 << 20 })
		require.Equal(t, int64(20<<20), MaxUploadFileSize())
	})

	t.Run("注入非法值回落兜底（0 会让 io.LimitReader 完全不读）", func(t *testing.T) {
		SetMaxUploadFileSizeProvider(func() int64 { return 0 })
		require.Equal(t, DefaultMaxUploadFileSize, MaxUploadFileSize())
	})

	t.Run("setter 传 nil 是空操作而非复位", func(t *testing.T) {
		SetMaxUploadFileSizeProvider(func() int64 { return 20 << 20 })
		SetMaxUploadFileSizeProvider(nil)
		require.Equal(t, int64(20<<20), MaxUploadFileSize())
	})
}

func TestMerchantKnowledgeMaxLenSeam(t *testing.T) {
	resetKnowledgeParamProviders()
	t.Cleanup(resetKnowledgeParamProviders)

	t.Run("未注入时返回兜底值 2000", func(t *testing.T) {
		require.Equal(t, 2000, MerchantKnowledgeMaxLen())
	})

	t.Run("注入后生效", func(t *testing.T) {
		SetMerchantKnowledgeMaxLenProvider(func() int { return 50 })
		require.Equal(t, 50, MerchantKnowledgeMaxLen())
	})

	t.Run("注入非法值回落兜底", func(t *testing.T) {
		SetMerchantKnowledgeMaxLenProvider(func() int { return -1 })
		require.Equal(t, DefaultMerchantKnowledgeMaxLen, MerchantKnowledgeMaxLen())
	})
}

// TestSplitMarkdownRespectsMaxLen 行为级断言：
// 外部 markdown 导入按 MerchantKnowledgeMaxLen 切块，把上限调到 50 后
// 一段 500 字符的正文应被切成 10 段而不是 1 段。
// 钉的是「读取点真的走了 seam」，比只测 MerchantKnowledgeMaxLen() 本身强。
func TestSplitMarkdownRespectsMaxLen(t *testing.T) {
	resetKnowledgeParamProviders()
	t.Cleanup(resetKnowledgeParamProviders)

	// 一段没有空行的长正文：按段落软切时整段只能被硬切，所以段数 = ceil(500/limit)
	body := strings.Repeat("字", 500)
	md := "## 标题\n" + body

	items := splitMarkdownToItems(md, "doc-1", "notion")
	require.Len(t, items, 1, "默认上限 2000 时 500 字不该被切")

	SetMerchantKnowledgeMaxLenProvider(func() int { return 50 })
	items = splitMarkdownToItems(md, "doc-1", "notion")
	require.Len(t, items, 10, "上限 50 时 500 字应被切成 10 段")
	for _, it := range items {
		require.LessOrEqual(t, len([]rune(it.Content)), 50, "切出的每段都不得超过上限")
	}
}
