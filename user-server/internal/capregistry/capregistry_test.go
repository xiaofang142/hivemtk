package capregistry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 矩阵文档 = 注册表渲染，漂移即红（借鉴 catbus capabilities 文档新鲜度测试）。
// 更新方式：UPDATE_GOLDEN=1 跑测试后 diff 入库 docs/capabilities.md。
func TestMatrixDocFreshness(t *testing.T) {
	got := MatrixMarkdown()
	// 测试 cwd = 本包目录；仓库 docs 在 ../../docs
	path := filepath.Join("..", "..", "docs", "capabilities.md")

	want, err := os.ReadFile(path)
	if err != nil || os.Getenv("UPDATE_GOLDEN") == "1" {
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		if werr := os.WriteFile(path, []byte(got), 0o644); werr != nil {
			t.Fatalf("写能力矩阵失败: %v", werr)
		}
		if err != nil {
			t.Skipf("capabilities.md 不存在，已生成基线: %s（请 diff 后入库）", path)
		}
		t.Skipf("UPDATE_GOLDEN=1：capabilities.md 已刷新，请 diff 后入库")
	}
	if strings.TrimSpace(string(want)) != strings.TrimSpace(got) {
		t.Fatalf("docs/capabilities.md 与注册表不一致：改能力请同步在 internal/capregistry/channels.go 登记后 UPDATE_GOLDEN=1 重新生成")
	}
}

func TestRegisterDuplicatePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("重复登记应 panic")
		}
	}()
	Register(Entry{Platform: "douyin", Cap: CapDMReceive, Via: "x"})
}

func TestAllSortedStable(t *testing.T) {
	a := All()
	b := All()
	if len(a) == 0 {
		t.Fatal("注册表为空")
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatal("All() 顺序不稳定")
		}
	}
}
