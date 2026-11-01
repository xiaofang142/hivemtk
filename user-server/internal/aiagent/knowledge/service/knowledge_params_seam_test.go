package service

import (
	"bytes"
	"context"
	"mime/multipart"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func resetKnowledgeParamProviders() {
	maxUploadFileSizeProvider = func() int64 { return DefaultMaxUploadFileSize }
	merchantBatchMaxFileSizeProvider = func() int64 { return DefaultMerchantBatchMaxFileSize }
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

func TestMerchantBatchMaxFileSizeSeam(t *testing.T) {
	resetKnowledgeParamProviders()
	t.Cleanup(resetKnowledgeParamProviders)

	t.Run("未注入时返回兜底值 20MB", func(t *testing.T) {
		require.Equal(t, int64(20<<20), MerchantBatchMaxFileSize())
	})

	t.Run("注入后生效", func(t *testing.T) {
		SetMerchantBatchMaxFileSizeProvider(func() int64 { return 5 << 20 })
		require.Equal(t, int64(5<<20), MerchantBatchMaxFileSize())
	})

	t.Run("注入非法值回落兜底（0 等于把这个口全堵死，而它接线前是全开）", func(t *testing.T) {
		SetMerchantBatchMaxFileSizeProvider(func() int64 { return 0 })
		require.Equal(t, DefaultMerchantBatchMaxFileSize, MerchantBatchMaxFileSize())
	})

	t.Run("setter 传 nil 是空操作而非复位", func(t *testing.T) {
		SetMerchantBatchMaxFileSizeProvider(func() int64 { return 5 << 20 })
		SetMerchantBatchMaxFileSizeProvider(nil)
		require.Equal(t, int64(5<<20), MerchantBatchMaxFileSize())
	})

	// 与文档上传那条各走各的口：并成一条时这一格会先红（两条默认值不同，20 vs 50）。
	t.Run("与 max_upload_file_size 是两条独立的上限", func(t *testing.T) {
		SetMerchantBatchMaxFileSizeProvider(func() int64 { return 5 << 20 })
		require.Equal(t, int64(5<<20), MerchantBatchMaxFileSize())
		require.Equal(t, int64(50<<20), MaxUploadFileSize())
	})
}

// fakeBatchFile 只用来喂 parseBatchFile：multipart.File 的四个方法里
// Read/ReadAt/Seek 由内嵌的 bytes.Reader 提供，Close 补一个空的。
// 用例真正要控制的是 FileHeader.Size（判据读的就是它），不是字节数。
type fakeBatchFile struct{ *bytes.Reader }

func (fakeBatchFile) Close() error { return nil }

// TestParseBatchFileEnforcesMaxFileSize 行为级断言：上限是从 seam 取的，
// 不是把 20MB 抄在判断行上。把上限调到 5MB 后，一个 header.Size=6MB 的请求必须被拒，
// 而且必须**在 io.ReadAll 之前**被拒（接线前这里零校验，整个文件已经被读进内存了）。
func TestParseBatchFileEnforcesMaxFileSize(t *testing.T) {
	resetKnowledgeParamProviders()
	t.Cleanup(resetKnowledgeParamProviders)

	s := &KnowledgeMerchantService{}
	csvBody := "标题,内容\n第一行,正文\n"

	t.Run("上限以下照常解析（正控制）", func(t *testing.T) {
		items, err := s.parseBatchFile(context.Background(),
			fakeBatchFile{bytes.NewReader([]byte(csvBody))},
			&multipart.FileHeader{Filename: "batch.csv", Size: int64(len(csvBody))}, "csv")
		require.NoError(t, err)
		require.Len(t, items, 1)
	})

	t.Run("超过上限被拒", func(t *testing.T) {
		SetMerchantBatchMaxFileSizeProvider(func() int64 { return 5 << 20 })
		items, err := s.parseBatchFile(context.Background(),
			fakeBatchFile{bytes.NewReader([]byte(csvBody))},
			&multipart.FileHeader{Filename: "batch.csv", Size: 6 << 20}, "csv")
		require.ErrorContains(t, err, "文件过大")
		require.Nil(t, items, "被拒时不许返回半成品")
		require.NotContains(t, err.Error(), "标题", "报错不许回显文件内容")
	})

	t.Run("兜底值也拦得住（没注入时按 20MB 判）", func(t *testing.T) {
		resetKnowledgeParamProviders()
		_, err := s.parseBatchFile(context.Background(),
			fakeBatchFile{bytes.NewReader([]byte(csvBody))},
			&multipart.FileHeader{Filename: "batch.csv", Size: 21 << 20}, "csv")
		require.ErrorContains(t, err, "上限 20 MB")
	})
}

// TestParseBatchFileTxtAutoDetect BatchImport.vue 的文件选择器 accept 为 .csv,.json,.txt，
// 但此前 auto 分派把 .txt 落进 default→json，上传 .txt 必报「JSON 解析失败」
// （2026-10-31 API 全量回归实测）。锁定：.txt 按「整文件单文档」解析，
// 标题取文件名去扩展名，正文即全文；空文件报可操作错误。
func TestParseBatchFileTxtAutoDetect(t *testing.T) {
	s := &KnowledgeMerchantService{}

	t.Run("auto + .txt 解析为单文档", func(t *testing.T) {
		body := "这是纯文本知识内容，第一段。\n\n这是第二段。"
		items, err := s.parseBatchFile(context.Background(),
			fakeBatchFile{bytes.NewReader([]byte(body))},
			&multipart.FileHeader{Filename: "产品说明.txt", Size: int64(len(body))}, "")
		require.NoError(t, err)
		require.Len(t, items, 1)
		require.Equal(t, "产品说明", items[0].Title)
		require.Equal(t, body, items[0].Content)
	})

	t.Run("显式 format=text 同样生效", func(t *testing.T) {
		body := "显式指定 text 格式的内容。"
		items, err := s.parseBatchFile(context.Background(),
			fakeBatchFile{bytes.NewReader([]byte(body))},
			&multipart.FileHeader{Filename: "any.bin", Size: int64(len(body))}, "text")
		require.NoError(t, err)
		require.Len(t, items, 1)
		require.Equal(t, "any", items[0].Title, "标题取文件名去扩展名（与 .txt 同一规则）")
	})

	t.Run("空 .txt 报可操作错误且不回显内容", func(t *testing.T) {
		items, err := s.parseBatchFile(context.Background(),
			fakeBatchFile{bytes.NewReader([]byte("   \n  "))},
			&multipart.FileHeader{Filename: "empty.txt", Size: 5}, "")
		require.ErrorContains(t, err, "TXT 文件内容为空")
		require.Nil(t, items)
	})
}
