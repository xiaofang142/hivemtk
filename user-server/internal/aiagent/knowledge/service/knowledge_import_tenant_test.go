package service

// A7（`未完成任务.md` §9）回归：知识库导入落盘按租户分域。
//
// 修前形态（2026-10-23 前）：知识库的 4 条导入通路**绕过 LocalDriver 自己 os.Create/**
// os.WriteFile，目录写死 `{STORAGE_LOCAL_BASE_DIR}/knowledge-base|knowledge|knowledge-text|
// knowledge-url/…`。而租户分域（T011）只在 `LocalDriver.UploadReader` 里实现过 ⇒
// "上传口分域了、知识库导入口没分域"，同一个 uploads 根下两种布局并存。
//
// 判据：
//  1. off 模式：相对路径与升级前**逐字节一致**（无 tenant_ 段，存量文件零迁移）；
//  2. on 模式 + 租户 ctx：相对路径以 `tenant_{id}/` 打头（与 LocalDriver 同一份前缀函数）；
//  3. on 模式无租户 ctx：落存量根域（系统级导入的既有语义）；
//  4. 历史文件仍可读：根域路径在 on 模式下不被 `/files` 守卫拦（见 router 包的
//     `TestTenantPathAllowed_*`，「不改变合法用法」的那一侧）。

import (
	"context"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hivemtk-user/internal/aiagent/knowledge/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
)

func withTenantModeKB(t *testing.T, m tenant.Mode) {
	t.Helper()
	prev := tenant.CurrentMode()
	tenant.SetMode(m)
	t.Cleanup(func() { tenant.SetMode(prev) })
}

// importTempUpload 造一个真实 multipart 上传（os.File 满足 multipart.File）。
func importTempUpload(t *testing.T, content string) (multipart.File, *multipart.FileHeader) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("造上传文件失败: %v", err)
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatalf("打开上传文件失败: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f, &multipart.FileHeader{Filename: "a.txt", Size: int64(len(content))}
}

// relToBase 把落盘绝对路径转成相对 STORAGE_LOCAL_BASE_DIR 的形式（即 /files 的相对路径）。
func relToBase(t *testing.T, base, p string) string {
	t.Helper()
	rel, err := filepath.Rel(base, p)
	if err != nil {
		t.Fatalf("算相对路径失败: %v", err)
	}
	return filepath.ToSlash(rel)
}

// TestImportDocument_TenantScopedPath 三条形态各钉一次（off / on+租户 / on 无租户）。
func TestImportDocument_TenantScopedPath(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.KBDocument{})
	if db == nil {
		return
	}
	// 这些用例跑完就删表：模型表里其它用例可能同名，按 testutil 的隔离口径来
	t.Cleanup(func() { _ = db.Migrator().DropTable(&model.KBDocument{}) })

	base := t.TempDir()
	t.Setenv("STORAGE_LOCAL_BASE_DIR", base)

	svc := NewKnowledgeBaseService()

	cases := []struct {
		name       string
		mode       tenant.Mode
		tenantID   uint
		wantPrefix string // 相对路径必须以此打头
	}{
		{"off 模式路径不变", tenant.ModeOff, tenant.DefaultTenantID, "knowledge-base/"},
		{"on 模式+租户7", tenant.ModeOn, 7, "tenant_7/knowledge-base/"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withTenantModeKB(t, tc.mode)
			ctx := context.Background()
			if tc.tenantID != 0 {
				ctx = tenant.WithTenantID(ctx, tc.tenantID)
			}
			file, header := importTempUpload(t, "hello")
			res, err := svc.ImportDocument(ctx, "t", file, header)
			if err != nil {
				t.Fatalf("导入失败: %v", err)
			}
			// ImportDocument 的报告路径与实际落盘路径同源，先确认文件真的在
			if _, statErr := os.Stat(res.FilePath); statErr != nil {
				t.Fatalf("报告路径上的文件不存在: %v", statErr)
			}
			rel := relToBase(t, base, res.FilePath)
			if !strings.HasPrefix(rel, tc.wantPrefix) {
				t.Fatalf("落盘相对路径 %q 应以 %q 打头（A7：租户分域）", rel, tc.wantPrefix)
			}
			// off 模式的硬约束：路径里绝不能出现 tenant_ 段
			if tc.mode == tenant.ModeOff && strings.Contains(rel, "tenant_") {
				t.Fatalf("off 模式路径不得含 tenant_ 段（逐字节等价），实际 %q", rel)
			}
		})
	}
}

// TestImportDocument_OnModeWithoutTenantFailsClosed on 模式 + 无租户 ctx：
//
// 这一档**到不了"落根域"那一步**——写 `kb_documents` 时租户隔离 callback 直接 fail-closed。
// 顺带钉住修前就有的清理语义：DB 写失败时刚落的文件必须被删掉，否则每次失败的导入
// 都在 uploads 里留下一个无行引用的孤儿文件（无行引用 = 谁都不知道它属于谁）。
func TestImportDocument_OnModeWithoutTenantFailsClosed(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.KBDocument{})
	if db == nil {
		return
	}
	t.Cleanup(func() { _ = db.Migrator().DropTable(&model.KBDocument{}) })

	base := t.TempDir()
	t.Setenv("STORAGE_LOCAL_BASE_DIR", base)

	withTenantModeKB(t, tenant.ModeOn)
	svc := NewKnowledgeBaseService()

	file, header := importTempUpload(t, "hello")
	if _, err := svc.ImportDocument(context.Background(), "t", file, header); err == nil {
		t.Fatalf("on 模式无租户 ctx 的导入必须被拒绝（fail-closed），实际成功")
	}

	var leftovers []string
	_ = filepath.Walk(base, func(p string, info os.FileInfo, err error) error {
		if err == nil && info != nil && !info.IsDir() {
			leftovers = append(leftovers, p)
		}
		return nil
	})
	if len(leftovers) > 0 {
		t.Fatalf("导入失败后不应留下孤儿文件，实际留下 %v", leftovers)
	}
}

// TestImportText_TenantScopedPath 第二条导入通路（文本导入）同样分域。
//
// 单独钉一次的理由：A7 的缺陷形态正是"分域只做了一半"，只测一条通路会让
// "另一条通路又忘了"重新变成绿灯。
func TestImportText_TenantScopedPath(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.KnowledgeDocument{})
	if db == nil {
		return
	}
	t.Cleanup(func() { _ = db.Migrator().DropTable(&model.KnowledgeDocument{}) })

	base := t.TempDir()
	t.Setenv("STORAGE_LOCAL_BASE_DIR", base)

	svc := NewKnowledgeService()
	withTenantModeKB(t, tenant.ModeOn)
	ctx := tenant.WithTenantID(context.Background(), 9)

	doc, err := svc.importText(ctx, &ImportRequest{Content: "文本内容", Title: "t"}, nil, "0")
	if err != nil {
		t.Fatalf("文本导入失败: %v", err)
	}
	rel := relToBase(t, base, doc.FilePath)
	if !strings.HasPrefix(rel, "tenant_9/knowledge-text/") {
		t.Fatalf("文本导入落盘路径 %q 应以 tenant_9/knowledge-text/ 打头（A7）", rel)
	}
}
