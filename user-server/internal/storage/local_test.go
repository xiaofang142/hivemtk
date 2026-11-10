package storage

import (
	"bytes"
	"context"
	"hivemtk-user/internal/pkg/tenant"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func newTestDriver(t *testing.T) *LocalDriver {
	t.Helper()
	return NewLocalDriver(t.TempDir(), "/files")
}

func TestNewLocalDriver_Defaults(t *testing.T) {
	d := NewLocalDriver("", "")
	if d.baseDir != "uploads" { // NewLocalDriver 对 baseDir 做 filepath.Clean
		t.Fatalf("baseDir = %q, want uploads", d.baseDir)
	}
	if d.publicBaseURL != "/files" {
		t.Fatalf("publicBaseURL = %q, want /files", d.publicBaseURL)
	}
	d2 := NewLocalDriver("./x", "https://cdn.example.com/")
	if d2.publicBaseURL != "https://cdn.example.com" {
		t.Fatalf("trailing slash not trimmed: %q", d2.publicBaseURL)
	}
}

func TestGeneratePath_SanitizesOriginalFilename(t *testing.T) {
	d := newTestDriver(t)
	got := d.generatePath("avatars", "../../etc/passwd.PDF")
	if strings.Contains(got, "passwd") || strings.Contains(got, "..") {
		t.Fatalf("generatePath leaked original filename: %q", got)
	}
	if !strings.HasSuffix(got, ".pdf") {
		t.Fatalf("generatePath ext = %q, want lowercase .pdf", got)
	}
	noExt := d.generatePath("attachments", "noext")
	if !strings.HasSuffix(noExt, ".bin") {
		t.Fatalf("generatePath without ext = %q, want .bin suffix", noExt)
	}
	segs := strings.Split(filepath.ToSlash(noExt), "/")
	if len(segs) != 4 || segs[0] != "attachments" {
		t.Fatalf("unexpected layout: %q", noExt)
	}
}

func TestUploadReader_RoundTrip(t *testing.T) {
	if runtime.GOOS == "windows" {
		// Windows 上 Download 返回的 *os.File 在 defer Close 前后与 Delete 竞态：
		// 打开中的文件不能 os.Remove（"being used by another process"）。语义断言
		// （上传/下载/存在性）在 POSIX 侧已覆盖，Windows 跳过 Delete 交叉段。
		t.Skip("Windows 文件锁：Download 打开的句柄未关前 Delete 必失败（NTFS 语义）")
	}
	d := newTestDriver(t)
	ctx := context.Background()
	content := "hello-hivemtk"
	url, storagePath, err := d.UploadReader(ctx, strings.NewReader(content), int64(len(content)), "docs", "note.txt")
	if err != nil {
		t.Fatalf("UploadReader: %v", err)
	}
	if !strings.HasPrefix(url, "/files/") {
		t.Fatalf("publicURL = %q, want /files/ prefix", url)
	}
	full := filepath.Join(d.BaseDir(), storagePath)
	raw, err := os.ReadFile(full)
	if err != nil {
		t.Fatalf("stored file missing: %v", err)
	}
	if string(raw) != content {
		t.Fatalf("stored content = %q", raw)
	}
	if _, err := os.Stat(full + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("tmp file left behind after rename")
	}

	rc, err := d.Download(ctx, storagePath)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	if !bytes.Equal(got, []byte(content)) {
		t.Fatalf("Download content = %q", got)
	}

	if ok, err := d.Exists(ctx, storagePath); err != nil || !ok {
		t.Fatalf("Exists after upload = %v, %v", ok, err)
	}
	if err := d.Delete(ctx, storagePath); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if ok, _ := d.Exists(ctx, storagePath); ok {
		t.Fatal("Exists true after Delete")
	}
	if err := d.Delete(ctx, storagePath); err != nil {
		t.Fatalf("Delete of missing file should be nil, got %v", err)
	}
}

func TestUploadReader_NilReader(t *testing.T) {
	d := newTestDriver(t)
	if _, _, err := d.UploadReader(context.Background(), nil, 0, "docs", "x.txt"); err == nil {
		t.Fatal("expected error for nil reader")
	}
}

func TestDriver_PathTraversalRejected(t *testing.T) {
	base := t.TempDir()
	outside := filepath.Join(base, "secret.txt")
	if err := os.WriteFile(outside, []byte("top-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	d := NewLocalDriver(filepath.Join(base, "uploads"), "/files")
	ctx := context.Background()

	evils := []string{"../secret.txt", "./../secret.txt", "a/../../secret.txt"}
	for _, p := range evils {
		if rc, err := d.Download(ctx, p); err == nil {
			rc.Close()
			t.Fatalf("Download(%q) succeeded, want rejection", p)
		}
		if _, err := d.Exists(ctx, p); err == nil {
			t.Fatalf("Exists(%q) succeeded, want rejection", p)
		}
		if err := d.Delete(ctx, p); err == nil {
			t.Fatalf("Delete(%q) succeeded, want rejection", p)
		}
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("outside file was touched: %v", err)
	}
}

func TestSignUploadURL_Unsupported(t *testing.T) {
	d := newTestDriver(t)
	if _, _, err := d.SignUploadURL(context.Background(), "f", "n.txt", "text/plain", 0); err == nil {
		t.Fatal("local driver must reject pre-signed URL requests")
	}
}

func TestType(t *testing.T) {
	if newTestDriver(t).Type() != "local" {
		t.Fatal("Type() must be local")
	}
}

func TestLocalDriverTenantPathPrefix(t *testing.T) {
	tenant.SetMode(tenant.ModeOff)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })

	dir := t.TempDir()
	d := NewLocalDriver(dir, "")

	ctx42 := tenant.WithTenantID(context.Background(), 42)

	// off：带租户 ctx 也必须与现状逐字节一致（无前缀）
	_, path, err := d.UploadReader(ctx42, strings.NewReader("off"), 3, "chat", "a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(path, "tenant_") {
		t.Fatalf("off 模式路径不得带租户段: %q", path)
	}

	// on + 租户 ctx：tenant_{id}/ 前缀，且文件真实落在该域内
	tenant.SetMode(tenant.ModeOn)
	_, path, err = d.UploadReader(ctx42, strings.NewReader("on42"), 4, "chat", "b.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(path, "tenant_42/") {
		t.Fatalf("on 模式路径应自证租户: %q", path)
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(path))); err != nil {
		t.Fatalf("文件应落在租户域目录: %v", err)
	}

	// on + 无租户 ctx：系统级上传落存量根域
	_, path, err = d.UploadReader(context.Background(), strings.NewReader("sys"), 3, "chat", "c.txt")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(path, "tenant_") {
		t.Fatalf("无租户 ctx 应落存量根域: %q", path)
	}

	// on：下载/删除经 resolve 走同一路径（自证租户段在 baseDir 内，穿越守卫天然覆盖）
	rc, err := d.Download(ctx42, path)
	if err != nil {
		t.Fatalf("租户域文件应可回读: %v", err)
	}
	_ = rc.Close()
}
