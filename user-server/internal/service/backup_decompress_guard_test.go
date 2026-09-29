package service

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"testing"

	"hivemtk-user/internal/storage"
)

// writeZip 按给定条目名造一个最小 zip 文件（内容固定），返回路径。
func writeZip(t *testing.T, names ...string) string {
	t.Helper()
	zp := filepath.Join(t.TempDir(), "probe.zip")
	fh, err := os.Create(zp)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(fh)
	for _, n := range names {
		w, err := zw.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		if len(n) > 0 && n[len(n)-1] != '/' {
			if _, err := w.Write([]byte("payload")); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := fh.Close(); err != nil {
		t.Fatal(err)
	}
	return zp
}

func TestRestoreService_DecompressBackup_PathTraversalGuard(t *testing.T) {
	svc := &RestoreService{}
	ctx := context.Background()

	// 反向/攻击用例：一切规范化后逃出当前目录的条目名必须整包拒绝，
	// 且不得留下任何解出文件。
	for _, evil := range []string{"../escape.txt", "/abs/escape.txt", "sub/../../escape.txt", "./../escape.txt"} {
		t.Chdir(t.TempDir())
		zp := writeZip(t, evil)
		if err := svc.decompressBackup(ctx, zp); err == nil {
			t.Errorf("条目 %q 应被拒绝但通过了守卫", evil)
		}
		if _, err := os.Stat(filepath.Join("..", "escape.txt")); err == nil {
			t.Errorf("条目 %q 逃逸写盘成功 ⇒ 守卫失守", evil)
		}
	}
}

func TestRestoreService_DecompressBackup_LegitNamesAndPerms(t *testing.T) {
	svc := &RestoreService{}
	ctx := context.Background()
	t.Chdir(t.TempDir())

	// 合法集：嵌套、带连续点的文件名、目录条目；旧守卫(Contains(".."))会误杀 b..c.txt，
	// IsLocal 规范化守卫应放行。
	zp := writeZip(t, "data.json", "b..c.txt", "d/", "d/nested.txt")
	if err := svc.decompressBackup(ctx, zp); err != nil {
		t.Fatalf("合法 zip 解压失败: %v", err)
	}

	_, restoreTmp := storage.BackupSource()

	for _, rel := range []string{"data.json", "b..c.txt", "d/nested.txt"} {
		p := filepath.Join(restoreTmp, rel)
		st, err := os.Stat(p)
		if err != nil {
			t.Fatalf("应解出 %s: %v", p, err)
		}
		if mode := st.Mode().Perm(); mode != 0o600 {
			t.Errorf("%s 权限 = %o, want 600", p, mode)
		}
	}
	// 目录条目须落在暂存根内（旧实现漏了前缀，会在 CWD 造出散目录）
	if _, err := os.Stat(filepath.Join(restoreTmp, "d")); err != nil {
		t.Errorf("目录条目未解到暂存根下: %v", err)
	}
	if _, err := os.Stat("d"); !os.IsNotExist(err) {
		t.Error("目录条目泄漏到暂存根之外")
	}
	if st, err := os.Stat(restoreTmp); err != nil || st.Mode().Perm()&0o077 != 0 {
		t.Errorf("暂存根目录权限过宽: %v %o", err, st.Mode().Perm())
	}
}
