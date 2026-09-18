package storage

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
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
