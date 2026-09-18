package controller

import (
	"bufio"
	"bytes"
	"fmt"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/storage"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// 文件上传配置
const (
	MaxUploadSize = 10 * 1024 * 1024

	AllowedImageTypes   = "image/jpeg,image/jpg,image/png,image/gif,image/webp"
	AllowedVideoTypes   = "video/mp4,video/quicktime,video/x-msvideo"
	AllowedDocTypes     = "application/pdf,application/msword,application/vnd.openxmlformats-officedocument.wordprocessingml.document,application/vnd.ms-excel,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,application/vnd.ms-powerpoint,application/vnd.openxmlformats-officedocument.presentationml.presentation"
	AllowedArchiveTypes = "application/zip,application/x-zip-compressed,application/x-rar-compressed,application/x-7z-compressed"
)

var allowedExtensions = map[string][]string{
	"image": {".jpg", ".jpeg", ".png", ".gif", ".webp"},
	"doc":   {".pdf", ".doc", ".docx"},
}

var dangerousExtensions = map[string]bool{
	".exe": true, ".bat": true, ".cmd": true, ".sh": true,
	".php": true, ".jsp": true, ".asp": true, ".aspx": true,
	".cgi": true, ".pl": true, ".py": true, ".rb": true,
	".jar": true, ".war": true, ".com": true, ".pif": true,
	".msi": true, ".scr": true, ".vbs": true, ".wsf": true,
	".hta": true, ".cpl": true, ".reg": true, ".dll": true,
	".drv": true, ".sys": true, ".lnk": true, ".inf": true,
}

var fileMagicNumbers = map[string][][]byte{
	".jpg":  {{0xFF, 0xD8, 0xFF}},
	".jpeg": {{0xFF, 0xD8, 0xFF}},
	".png":  {{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}},
	".gif":  {{0x47, 0x49, 0x46, 0x38, 0x37, 0x61}, {0x47, 0x49, 0x46, 0x38, 0x39, 0x61}},
	".webp": {{0x52, 0x49, 0x46, 0x46}},
	".pdf":  {{0x25, 0x50, 0x44, 0x46}},
	".zip":  {{0x50, 0x4B, 0x03, 0x04}, {0x50, 0x4B, 0x05, 0x06}, {0x50, 0x4B, 0x07, 0x08}},
	".rar":  {{0x52, 0x61, 0x72, 0x21, 0x1A, 0x07, 0x00}, {0x52, 0x61, 0x72, 0x21, 0x1A, 0x07, 0x01, 0x00}},
	".docx": {{0x50, 0x4B, 0x03, 0x04}},
	".xlsx": {{0x50, 0x4B, 0x03, 0x04}},
	".pptx": {{0x50, 0x4B, 0x03, 0x04}},
	".mp4":  {{0x00, 0x00, 0x00, 0x18, 0x66, 0x74, 0x79, 0x70}, {0x00, 0x00, 0x00, 0x1C, 0x66, 0x74, 0x79, 0x70}},
	".mov":  {{0x00, 0x00, 0x00, 0x14, 0x66, 0x74, 0x79, 0x70}},
}

// UploadConfig 上传配置
type UploadConfig struct {
	MaxSize          int64
	AllowedTypes     string
	EnableVirusScan  bool
	VirusScanURL     string
	UploadDir        string
	CheckMagicNumber bool
}

var DefaultUploadConfig = UploadConfig{
	MaxSize:          MaxUploadSize,
	AllowedTypes:     "image/jpeg,image/jpg,image/png,image/gif,image/webp,application/pdf,application/msword,application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	EnableVirusScan:  false,
	VirusScanURL:     "",
	UploadDir:        "./uploads",
	CheckMagicNumber: true,
}

// UploadFile 文件上传 - 安全加固版本
func UploadFile(ctx *gin.Context) {
	config := DefaultUploadConfig

	if envMaxSize := os.Getenv("UPLOAD_MAX_SIZE"); envMaxSize != "" {
		if size := parseInt64(envMaxSize); size > 0 {
			config.MaxSize = size
		}
	}
	if envVirusScan := os.Getenv("UPLOAD_VIRUS_SCAN"); envVirusScan == "true" {
		config.EnableVirusScan = true
	}
	if envVirusURL := os.Getenv("VIRUS_SCAN_URL"); envVirusURL != "" {
		config.VirusScanURL = envVirusURL
		config.EnableVirusScan = true
	}

	if err := ctx.Request.ParseMultipartForm(config.MaxSize); err != nil {
		response.Error(ctx, http.StatusBadRequest, "文件大小超出限制")
		return
	}

	file, header, err := ctx.Request.FormFile("file")
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "获取上传文件失败")
		return
	}
	defer func() { _ = file.Close() }()

	if header.Size > config.MaxSize {
		response.Error(ctx, http.StatusBadRequest, fmt.Sprintf("文件大小超出限制，最大允许 %dMB", config.MaxSize/1024/1024))
		return
	}

	fileBuffer := bytes.NewBuffer(nil)
	if _, err := io.Copy(fileBuffer, file); err != nil {
		response.ErrorFromDB(ctx, err, "读取文件失败")
		return
	}
	fileBytes := fileBuffer.Bytes()

	originalExt := strings.ToLower(filepath.Ext(header.Filename))
	if !isValidExtension(originalExt) {
		response.Error(ctx, http.StatusBadRequest, "不支持的文件类型："+originalExt)
		return
	}

	if dangerousExtensions[originalExt] {
		response.Error(ctx, http.StatusBadRequest, "禁止上传可执行文件或脚本文件")
		return
	}

	actualExt := detectFileTypeByMagicNumber(fileBytes)
	if config.CheckMagicNumber && actualExt != "" && !extensionsMatch(actualExt, originalExt) {
		if !isZipBasedFormat(actualExt, originalExt) {
			response.Error(ctx, http.StatusBadRequest, "文件类型与内容不匹配，可能为伪造文件")
			return
		}
	}

	if originalExt == ".svg" {
		response.Error(ctx, http.StatusBadRequest, "不支持上传 SVG 文件（存储型 XSS 风险），请转换为 PNG/JPG 后重试")
		return
	}

	detectedMime := detectMimeType(fileBytes)

	if detectedMime == "application/zip" && isOfficeDocument(originalExt) {
		detectedMime = extToMIME(originalExt)
	}
	if !isAllowedMimeType(detectedMime, config.AllowedTypes) {
		response.Error(ctx, http.StatusBadRequest, "不允许上传此类型的文件："+detectedMime)
		return
	}

	virusScanned := false
	virusClean := true
	if config.EnableVirusScan && config.VirusScanURL != "" {
		virusScanned, virusClean = scanFileForVirus(fileBytes, config.VirusScanURL)
		if !virusClean {
			response.Error(ctx, http.StatusBadRequest, "文件可能包含病毒，已拒绝上传")
			return
		}
	}

	uploadFolder := os.Getenv("UPLOAD_FOLDER")
	if uploadFolder == "" {
		uploadFolder = "attachments"
	}
	baseDir := os.Getenv("STORAGE_LOCAL_BASE_DIR")
	if baseDir == "" {

		baseDir = os.Getenv("UPLOAD_DIR")
	}
	publicURLPrefix := os.Getenv("STORAGE_LOCAL_PUBLIC_URL")
	driver := storage.NewLocalDriver(baseDir, publicURLPrefix)

	publicURL, _, err := driver.UploadReader(ctx, bytes.NewReader(fileBytes), header.Size, uploadFolder, header.Filename)
	if err != nil {
		response.ErrorFromDB(ctx, err, "保存文件失败")
		return
	}
	fileURL := publicURL
	fileType := getFileType(originalExt)

	response.Success(ctx, gin.H{
		"url":           fileURL,
		"filename":      header.Filename,
		"size":          header.Size,
		"mime_type":     detectedMime,
		"extension":     originalExt,
		"file_type":     fileType,
		"virus_scanned": virusScanned,
		"virus_clean":   virusClean,
	}, "上传成功")
}

func isValidExtension(ext string) bool {
	ext = strings.ToLower(ext)
	for _, extensions := range allowedExtensions {
		for _, allowed := range extensions {
			if ext == allowed {
				return true
			}
		}
	}
	return false
}

func detectFileTypeByMagicNumber(data []byte) string {
	if len(data) == 0 {
		return ""
	}

	for ext, magicNumbers := range fileMagicNumbers {
		for _, magic := range magicNumbers {
			if len(data) >= len(magic) && bytes.HasPrefix(data, magic) {
				if ext == ".webp" {
					if len(data) >= 12 && string(data[8:12]) == "WEBP" {
						return ".webp"
					}
					continue
				}
				if ext == ".docx" || ext == ".xlsx" || ext == ".pptx" {
					return ext
				}
				return ext
			}
		}
	}
	return ""
}

func detectMimeType(data []byte) string {
	if len(data) == 0 {
		return "application/octet-stream"
	}

	if bytes.HasPrefix(data, []byte{0xFF, 0xD8, 0xFF}) {
		return "image/jpeg"
	}
	if bytes.HasPrefix(data, []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}) {
		return "image/png"
	}
	if bytes.HasPrefix(data, []byte{0x47, 0x49, 0x46}) {
		return "image/gif"
	}
	if bytes.HasPrefix(data, []byte{0x25, 0x50, 0x44, 0x46}) {
		return "application/pdf"
	}
	if bytes.HasPrefix(data, []byte{0x50, 0x4B, 0x03, 0x04}) {
		return "application/zip"
	}
	if len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		return "image/webp"
	}

	return "application/octet-stream"
}

func isAllowedMimeType(mimeType, allowedTypes string) bool {
	if allowedTypes == "" {
		return true
	}

	allowed := strings.Split(allowedTypes, ",")
	for _, t := range allowed {
		if strings.TrimSpace(t) == mimeType {
			return true
		}
	}
	return false
}

func isOfficeDocument(ext string) bool {
	officeExts := map[string]bool{
		".docx": true, ".xlsx": true, ".pptx": true,
	}
	return officeExts[strings.ToLower(ext)]
}

func extToMIME(ext string) string {
	ext = strings.ToLower(ext)
	switch ext {
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case ".pptx":
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	case ".doc":
		return "application/msword"
	case ".pdf":
		return "application/pdf"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	default:
		return "application/octet-stream"
	}
}

func extensionsMatch(a, b string) bool {
	a = strings.ToLower(a)
	b = strings.ToLower(b)
	if a == b {
		return true
	}
	if (a == ".jpg" && b == ".jpeg") || (a == ".jpeg" && b == ".jpg") {
		return true
	}
	return false
}

func isZipBasedFormat(detectedExt, originalExt string) bool {
	if detectedExt == ".zip" && isOfficeDocument(originalExt) {
		return true
	}
	return false
}

func getFileType(ext string) string {
	ext = strings.ToLower(ext)
	for fileType, extensions := range allowedExtensions {
		for _, e := range extensions {
			if ext == e {
				return fileType
			}
		}
	}
	return "other"
}

func scanFileForVirus(fileData []byte, scanURL string) (scanned bool, clean bool) {
	req, err := http.NewRequest("POST", scanURL, bytes.NewReader(fileData))
	if err != nil {
		return false, true
	}
	req.Header.Set("Content-Type", "application/octet-stream")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false, true
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusOK {
		return true, true
	}

	responseStr := string(body)
	if strings.Contains(strings.ToLower(responseStr), "clean") ||
		strings.Contains(strings.ToLower(responseStr), "ok") {
		return true, true
	}

	return true, false
}

func parseInt64(s string) int64 {
	var result int64
	// 解析失败时返回 0 是本辅助函数的既定契约，故忽略 Sscanf 错误。
	_, _ = fmt.Sscanf(s, "%d", &result)
	return result
}

// ScanFileContent 对外提供的文件扫描接口（用于上传后扫描）
func ScanFileContent(filePath string) (bool, error) {
	dangerousPatterns := []string{
		`<\?php`, `<%`, `<script`, `javascript:`,
		`eval\(`, `exec\(`, `system\(`, `passthru\(`,
		`shell_exec`, `popen`, `proc_open`,
	}

	file, err := os.Open(filePath)
	if err != nil {
		return false, err
	}
	defer func() { _ = file.Close() }()

	reader := bufio.NewReader(file)
	buf := make([]byte, 32*1024)

	for {
		n, err := reader.Read(buf)
		if err != nil && err != io.EOF {
			return false, err
		}
		if n == 0 {
			break
		}

		content := string(buf[:n])
		for _, pattern := range dangerousPatterns {
			if matched, _ := regexp.MatchString(pattern, content); matched {
				return false, fmt.Errorf("检测到危险内容：%s", pattern)
			}
		}
	}

	return true, nil
}
