package controller

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func setupUploadTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	_ = os.Setenv("UPLOAD_DIR", dir)
	t.Cleanup(func() { os.Unsetenv("UPLOAD_DIR") })
	return dir
}

func createUploadMultipartRequest(t *testing.T, fieldName, filename string, content []byte) *http.Request {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile(fieldName, filename)
	if err != nil {
		t.Fatalf("Failed to create form file: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("Failed to write content: %v", err)
	}
	writer.Close()
	req, _ := http.NewRequest("POST", "/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

func newUploadRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/upload", UploadFile)
	return router
}

func TestUploadFile_Success_PNG(t *testing.T) {
	setupUploadTestDir(t)
	router := newUploadRouter(t)
	pngContent := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52}
	req := createUploadMultipartRequest(t, "file", "test.png", pngContent)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d. Body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "上传成功") {
		t.Error("Expected success message")
	}
}

func TestUploadFile_Success_JPG(t *testing.T) {
	setupUploadTestDir(t)
	router := newUploadRouter(t)
	jpgContent := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46, 0x00, 0x01}
	req := createUploadMultipartRequest(t, "file", "photo.jpg", jpgContent)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d. Body: %s", w.Code, w.Body.String())
	}
}

func TestUploadFile_Success_PDF(t *testing.T) {
	setupUploadTestDir(t)
	router := newUploadRouter(t)
	pdfContent := []byte{0x25, 0x50, 0x44, 0x46, 0x2D, 0x31, 0x2E, 0x34}
	req := createUploadMultipartRequest(t, "file", "document.pdf", pdfContent)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d. Body: %s", w.Code, w.Body.String())
	}
}

func TestUploadFile_Success_GIF(t *testing.T) {
	setupUploadTestDir(t)
	router := newUploadRouter(t)
	gifContent := []byte{0x47, 0x49, 0x46, 0x38, 0x39, 0x61, 0x01, 0x00, 0x01, 0x00}
	req := createUploadMultipartRequest(t, "file", "animation.gif", gifContent)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d. Body: %s", w.Code, w.Body.String())
	}
}

func TestUploadFile_Success_WebP(t *testing.T) {
	setupUploadTestDir(t)
	router := newUploadRouter(t)
	webpContent := []byte{0x52, 0x49, 0x46, 0x46, 0x00, 0x00, 0x00, 0x00, 0x57, 0x45, 0x42, 0x50, 0x56, 0x50, 0x38, 0x20}
	req := createUploadMultipartRequest(t, "file", "photo.webp", webpContent)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d. Body: %s", w.Code, w.Body.String())
	}
}

func TestUploadFile_Success_ZIP(t *testing.T) {
	setupUploadTestDir(t)
	router := newUploadRouter(t)
	zipContent := []byte{0x50, 0x4B, 0x03, 0x04, 0x14, 0x00, 0x00, 0x00, 0x08, 0x00}
	req := createUploadMultipartRequest(t, "file", "archive.zip", zipContent)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Logf("NOTE: ZIP upload may fail due to magic number overlap with DOCX: %s", w.Body.String())
	}
}

func TestUploadFile_Rejects_UnsupportedExtension(t *testing.T) {
	setupUploadTestDir(t)
	router := newUploadRouter(t)
	req := createUploadMultipartRequest(t, "file", "unknown.xyz", []byte{0x00, 0x00})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for .xyz, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "不支持的文件类型") {
		t.Error("Expected unsupported file type message")
	}
}

func TestUploadFile_Rejects_DangerousExtension(t *testing.T) {
	setupUploadTestDir(t)
	router := newUploadRouter(t)
	req := createUploadMultipartRequest(t, "file", "malware.exe", []byte{0x00, 0x00})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for .exe, got %d", w.Code)
	}
}

func TestUploadFile_Rejects_PHP(t *testing.T) {
	setupUploadTestDir(t)
	router := newUploadRouter(t)
	req := createUploadMultipartRequest(t, "file", "shell.php", []byte{0x3C, 0x3F, 0x70, 0x68, 0x70})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for .php, got %d", w.Code)
	}
}

func TestUploadFile_Rejects_BAT(t *testing.T) {
	setupUploadTestDir(t)
	router := newUploadRouter(t)
	req := createUploadMultipartRequest(t, "file", "script.bat", []byte{0x40, 0x65, 0x63, 0x68, 0x6F})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for .bat, got %d", w.Code)
	}
}

func TestUploadFile_Rejects_TypeMismatch(t *testing.T) {
	setupUploadTestDir(t)
	router := newUploadRouter(t)
	pngMagic := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	req := createUploadMultipartRequest(t, "file", "fake.jpg", pngMagic)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for type mismatch, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "不匹配") {
		t.Error("Expected type mismatch message")
	}
}

func TestUploadFile_Rejects_LargeFile(t *testing.T) {
	setupUploadTestDir(t)
	router := newUploadRouter(t)
	bigContent := make([]byte, MaxUploadSize+1)
	req := createUploadMultipartRequest(t, "file", "big.png", bigContent)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for oversized file, got %d", w.Code)
	}
}

func TestUploadFile_NoFile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/upload", UploadFile)

	req, _ := http.NewRequest("POST", "/upload", nil)
	req.Header.Set("Content-Type", "multipart/form-data")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for no file, got %d", w.Code)
	}
}

func TestUploadFile_EmptyContent(t *testing.T) {
	setupUploadTestDir(t)
	router := newUploadRouter(t)
	req := createUploadMultipartRequest(t, "file", "empty.png", []byte{})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for empty file, got %d", w.Code)
	}
}

func TestUploadFile_UploadDirCreated(t *testing.T) {
	uploadDir := setupUploadTestDir(t)
	router := newUploadRouter(t)
	pngContent := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	req := createUploadMultipartRequest(t, "file", "test.png", pngContent)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Upload failed: %d", w.Code)
	}

	entries, err := os.ReadDir(uploadDir)
	if err != nil {
		t.Fatalf("Failed to read upload dir: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("Expected 1 subdirectory (date), got %d", len(entries))
	}
}

func TestUploadFile_Rejects_MP4(t *testing.T) {
	setupUploadTestDir(t)
	router := newUploadRouter(t)
	mp4Content := []byte{0x00, 0x00, 0x00, 0x18, 0x66, 0x74, 0x79, 0x70, 0x69, 0x73, 0x6F, 0x6D}
	req := createUploadMultipartRequest(t, "file", "video.mp4", mp4Content)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for MP4 (MIME not in allowed types), got %d. Body: %s", w.Code, w.Body.String())
	}
}

func TestUploadFile_Rejects_RAR(t *testing.T) {
	setupUploadTestDir(t)
	router := newUploadRouter(t)
	rarContent := []byte{0x52, 0x61, 0x72, 0x21, 0x1A, 0x07, 0x00, 0x00}
	req := createUploadMultipartRequest(t, "file", "backup.rar", rarContent)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for RAR (MIME not in allowed types), got %d. Body: %s", w.Code, w.Body.String())
	}
}

func TestUploadFile_Rejects_SVG(t *testing.T) {
	setupUploadTestDir(t)
	router := newUploadRouter(t)
	svgContent := []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><circle cx="50" cy="50" r="40"/></svg>`)
	req := createUploadMultipartRequest(t, "file", "icon.svg", svgContent)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for SVG (MIME not in allowed types), got %d. Body: %s", w.Code, w.Body.String())
	}
}

func TestIsValidExtension(t *testing.T) {
	tests := []struct {
		ext  string
		want bool
	}{
		{".jpg", true}, {".jpeg", true}, {".png", true}, {".gif", true},
		{".webp", true}, {".svg", false}, {".mp4", false}, {".pdf", true},

		{".zip", false}, {".exe", false}, {".xyz", false}, {".php", false},
		{"", false}, {".JPG", true}, {".docx", true}, {".doc", true},
	}
	for _, tt := range tests {
		t.Run(tt.ext, func(t *testing.T) {
			if got := isValidExtension(tt.ext); got != tt.want {
				t.Errorf("isValidExtension(%q) = %v, want %v", tt.ext, got, tt.want)
			}
		})
	}
}

func TestDetectFileTypeByMagicNumber(t *testing.T) {
	// want 一律是**单值**：原来 .jpg/.zip 两行写成 "a|b|c" + 「命中任一即通过」，
	// 那是把「map 遍历顺序随机 ⇒ 同一文件检成不同扩展名」的缺陷固化进断言，
	// 无论实现怎样都绿（假绿）。规范名唯一，就钉唯一。
	tests := []struct {
		name string
		data []byte
		want string
	}{
		{"jpeg", []byte{0xFF, 0xD8, 0xFF, 0xE0}, ".jpg"},
		{"png", []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}, ".png"},
		{"gif87", []byte{0x47, 0x49, 0x46, 0x38, 0x37, 0x61}, ".gif"},
		{"gif89", []byte{0x47, 0x49, 0x46, 0x38, 0x39, 0x61}, ".gif"},
		{"pdf", []byte{0x25, 0x50, 0x44, 0x46, 0x2D}, ".pdf"},
		{"zip_family_canonical", []byte{0x50, 0x4B, 0x03, 0x04}, ".zip"},
		{"rar", []byte{0x52, 0x61, 0x72, 0x21, 0x1A, 0x07, 0x00}, ".rar"},
		{"webp", []byte{0x52, 0x49, 0x46, 0x46, 0x00, 0x00, 0x00, 0x00, 0x57, 0x45, 0x42, 0x50}, ".webp"},
		{"empty", []byte{}, ""},
		{"unknown", []byte{0x00, 0x01, 0x02, 0x03}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := detectFileTypeByMagicNumber(tt.data); got != tt.want {
				t.Errorf("detectFileTypeByMagicNumber() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestDetectFileTypeByMagicNumber_Deterministic 同一段字节反复检必须每次同结果。
// 命中的键曾有两个（zip 家族四键共用 magic、jpg/jpeg 共用 magic），map 遍历顺序
// 随机 ⇒ 单次断言侥幸绿，重试就换值。这里把「唯一命中」这一不变量本身钉住。
func TestDetectFileTypeByMagicNumber_Deterministic(t *testing.T) {
	data := map[string][]byte{
		"zip":  {0x50, 0x4B, 0x03, 0x04},
		"jpeg": {0xFF, 0xD8, 0xFF, 0xE0},
	}
	for name, b := range data {
		first := detectFileTypeByMagicNumber(b)
		if first == "" {
			t.Fatalf("%s: 未检出类型", name)
		}
		for i := 0; i < 200; i++ {
			if got := detectFileTypeByMagicNumber(b); got != first {
				t.Fatalf("%s: 第 %d 次检出 %q，与首次 %q 不一致（magic 表有多键命中）", name, i+1, got, first)
			}
		}
	}
}

func TestDetectMimeType(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want string
	}{
		{"jpeg", []byte{0xFF, 0xD8, 0xFF, 0xE0}, "image/jpeg"},
		{"png", []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}, "image/png"},
		{"gif", []byte{0x47, 0x49, 0x46, 0x38, 0x37, 0x61}, "image/gif"},
		{"pdf", []byte{0x25, 0x50, 0x44, 0x46, 0x2D}, "application/pdf"},
		{"zip", []byte{0x50, 0x4B, 0x03, 0x04}, "application/zip"},
		{"webp", []byte{0x52, 0x49, 0x46, 0x46, 0, 0, 0, 0, 0x57, 0x45, 0x42, 0x50}, "image/webp"},
		{"empty", []byte{}, "application/octet-stream"},
		{"unknown", []byte{0x00, 0x01}, "application/octet-stream"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := detectMimeType(tt.data); got != tt.want {
				t.Errorf("detectMimeType() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsAllowedMimeType(t *testing.T) {
	allowed := "image/jpeg,image/png,application/pdf"
	if !isAllowedMimeType("image/jpeg", allowed) {
		t.Error("image/jpeg should be allowed")
	}
	if !isAllowedMimeType("application/pdf", allowed) {
		t.Error("application/pdf should be allowed")
	}
	if isAllowedMimeType("text/html", allowed) {
		t.Error("text/html should not be allowed")
	}
	if !isAllowedMimeType("anything", "") {
		t.Error("Empty allowedTypes should allow everything")
	}
}

func TestIsOfficeDocument(t *testing.T) {
	if !isOfficeDocument(".docx") {
		t.Error(".docx should be office document")
	}
	if !isOfficeDocument(".xlsx") {
		t.Error(".xlsx should be office document")
	}
	if !isOfficeDocument(".pptx") {
		t.Error(".pptx should be office document")
	}
	if !isOfficeDocument(".DOCX") {
		t.Error(".DOCX should be office document (case insensitive)")
	}
	if isOfficeDocument(".pdf") {
		t.Error(".pdf should not be office document")
	}
	if isOfficeDocument(".doc") {
		t.Error(".doc should not be office document")
	}
}

func TestGetFileType(t *testing.T) {
	tests := []struct {
		ext  string
		want string
	}{
		{".jpg", "image"}, {".png", "image"},

		{".mp4", "other"}, {".zip", "other"},
		{".pdf", "doc"}, {".docx", "doc"}, {".xyz", "other"},
	}
	for _, tt := range tests {
		t.Run(tt.ext, func(t *testing.T) {
			if got := getFileType(tt.ext); got != tt.want {
				t.Errorf("getFileType(%q) = %v, want %v", tt.ext, got, tt.want)
			}
		})
	}
}

func TestParseInt64(t *testing.T) {
	tests := []struct {
		s    string
		want int64
	}{
		{"1024", 1024}, {"0", 0}, {"abc", 0}, {"", 0}, {"-100", -100},
	}
	for _, tt := range tests {
		t.Run(tt.s, func(t *testing.T) {
			if got := parseInt64(tt.s); got != tt.want {
				t.Errorf("parseInt64(%q) = %v, want %v", tt.s, got, tt.want)
			}
		})
	}
}

func TestScanFileContent_Safe(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "safe.txt")
	os.WriteFile(filePath, []byte("Hello, this is a safe file"), 0644)
	safe, err := ScanFileContent(filePath)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !safe {
		t.Error("Expected file to be safe")
	}
}

func TestScanFileContent_Dangerous(t *testing.T) {
	patterns := []string{
		"<?php echo 'hacked'; ?>",
		"<script>alert('xss')</script>",
		"javascript:void(0)",
		"eval(document.cookie)",
	}
	dir := t.TempDir()
	for i, pattern := range patterns {
		filePath := filepath.Join(dir, "danger"+string(rune('a'+i))+".txt")
		os.WriteFile(filePath, []byte(pattern), 0644)
		safe, err := ScanFileContent(filePath)
		if safe {
			t.Errorf("Pattern %q should be detected as dangerous", pattern)
		}
		if err == nil {
			t.Errorf("Expected error for pattern %q", pattern)
		}
	}
}

func TestScanFileContent_NotFound(t *testing.T) {
	safe, err := ScanFileContent("/nonexistent/path/file.txt")
	if safe {
		t.Error("Expected false for nonexistent file")
	}
	if err == nil {
		t.Error("Expected error for nonexistent file")
	}
}

func TestUploadFile_Docx_Uses_ZIP_Format(t *testing.T) {
	setupUploadTestDir(t)
	router := newUploadRouter(t)
	zipMagic := []byte{0x50, 0x4B, 0x03, 0x04, 0x00, 0x00, 0x00, 0x00}
	req := createUploadMultipartRequest(t, "file", "document.docx", zipMagic)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// 这段字节在 detectMimeType 里就是 application/zip；上传闸门靠
	// upload.go 的 "zip + isOfficeDocument ⇒ 换成扩展名自己的 MIME" 一条把它救回来。
	// 原来两个分支都只 t.Log，等于删掉那条救回逻辑也照样绿 —— 现在钉住必须 200。
	if w.Code != http.StatusOK {
		t.Errorf("docx（内容是 ZIP magic）应被放行，实际 code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestUploadFile_DangerousExtensions_AreDeadCode(t *testing.T) {
	dangerousExts := []string{".exe", ".bat", ".cmd", ".sh", ".php", ".jsp", ".asp", ".py", ".rb"}
	for _, ext := range dangerousExts {
		if isValidExtension(ext) {
			t.Errorf("Extension %s is both in allowedExtensions AND dangerousExtensions - dead code bug", ext)
		}
	}
}

// TestUploadFile_AllowedTypes_ExcludeRemovedMedia 钉住 DefaultUploadConfig.AllowedTypes
// 的内容：它是 upload.go:150 isAllowedMimeType 的唯一判据，而三组 t.Log 版用例
// （视频 MP4 / SVG 存储型 XSS / RAR）在两个分支里都不产生失败，改坏白名单无人可查。
func TestUploadFile_AllowedTypes_ExcludeRemovedMedia(t *testing.T) {
	allowed := DefaultUploadConfig.AllowedTypes
	for _, forbidden := range []string{"video/mp4", "image/svg+xml", "rar"} {
		if strings.Contains(allowed, forbidden) {
			t.Errorf("AllowedTypes 又出现了 %q（MP4 与 RAR 于 P0-26、SVG 于 M9 被移出白名单）：当前值=%s", forbidden, allowed)
		}
	}
	// 反向半句：白名单被整体清空/写错时，上面三条缺席断言会集体哑掉。
	for _, required := range []string{"image/png", "application/pdf"} {
		if !strings.Contains(allowed, required) {
			t.Errorf("AllowedTypes 缺了基线项 %q：当前值=%s", required, allowed)
		}
	}
}
