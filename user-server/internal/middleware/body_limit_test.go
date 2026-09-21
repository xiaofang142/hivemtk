package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// bodyLimitEngine 挂一道上限为 maxBytes 的封顶，末段 handler 把整段 body 读完并回报读到的字节数，
// 用于把「预检就没放行」与「兜底把读取截断」这两条防线分开断言。
func bodyLimitEngine(maxBytes int64) (*gin.Engine, *int, *string) {
	r := gin.New()
	ran := new(int)
	readErr := new(string)
	r.Use(BodyLimit(maxBytes))
	r.POST("/echo", func(c *gin.Context) {
		*ran++
		// 读满而非读一次：chunked 兜底是在读到上限那一刻才报错的。
		n, err := io.ReadAll(c.Request.Body)
		if err != nil {
			*readErr = err.Error()
		}
		c.JSON(http.StatusOK, gin.H{"read": len(n)})
	})
	return r, ran, readErr
}

func envelopeOf(t *testing.T, body []byte) (code any, data map[string]any) {
	t.Helper()
	var parsed struct {
		Code any            `json:"code"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("响应不是合法信封 JSON: %v body=%s", err, body)
	}
	return parsed.Code, parsed.Data
}

func TestBodyLimitRejectsOversizedContentLength(t *testing.T) {
	const limit = int64(1024)
	r, ran, _ := bodyLimitEngine(limit)

	req := httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader(strings.Repeat("a", 2048)))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if req.ContentLength <= limit {
		t.Fatalf("夹具没铺到超限: ContentLength=%d limit=%d", req.ContentLength, limit)
	}
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("超限请求状态码=%d，期望 413", rec.Code)
	}
	code, data := envelopeOf(t, rec.Body.Bytes())
	if code == nil || code == float64(0) {
		t.Errorf("错误信封 code 不该是成功值: %#v", code)
	}
	if data["max_bytes"] == nil {
		t.Errorf("信封 data 要带 max_bytes 供调用方自证，实得 %#v", data)
	}
	if *ran != 0 {
		t.Errorf("超限请求仍进了 handler（跑了 %d 次）⇒ 预检没起作用", *ran)
	}
}

func TestBodyLimitAllowsBodyUnderCap(t *testing.T) {
	r, ran, readErr := bodyLimitEngine(1 << 20)

	req := httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader(`{"a":1}`))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("未超限请求状态码=%d，期望 200（封顶不得误伤正常载荷）", rec.Code)
	}
	if *ran != 1 {
		t.Errorf("handler 跑了 %d 次，期望 1 次", *ran)
	}
	if *readErr != "" {
		t.Errorf("正常 body 读取报错: %s", *readErr)
	}
}

// multipart 走上传通道，其内存占用由 gin 的 MaxMultipartMemory 控制（超出落临时文件），
// 一刀切会打断合法大文件上传 ⇒ 这一格断言的是「跳过」而不是「放行后仍被截断」。
func TestBodyLimitSkipsMultipart(t *testing.T) {
	r, ran, _ := bodyLimitEngine(1024)

	req := httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader(strings.Repeat("x", 4096)))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=--q")
	req.ContentLength = 4096
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code == http.StatusRequestEntityTooLarge {
		t.Errorf("multipart 被全局封顶拦掉了（413）⇒ 大文件上传会被打断")
	}
	if *ran != 1 {
		t.Errorf("multipart 未进 handler，跑了 %d 次，期望 1 次", *ran)
	}
}

// 第二道防线：chunked / 无 Content-Length 的请求预检看不见，只能靠 MaxBytesReader 截断读取。
// 断言的是「内存有界」这件事本身而不是状态码——响应已开写时中间件改不了码，这是既有取舍。
func TestBodyLimitCapsUnknownLengthBody(t *testing.T) {
	const limit = int64(1024)
	r, ran, readErr := bodyLimitEngine(limit)

	req := httptest.NewRequest(http.MethodPost, "/echo",
		iotest.OneByteReader(strings.NewReader(strings.Repeat("y", 8192))))
	req.ContentLength = -1
	req.TransferEncoding = []string{"chunked"}
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if *ran != 1 {
		t.Fatalf("无 Content-Length 的请求本该进 handler 再由读取兜底，实得 %d 次", *ran)
	}
	if *readErr == "" {
		t.Errorf("8KB 无界 body 在 limit=%d 下仍被整段读完 ⇒ 第二道防线没牙（内存未封顶）", limit)
	}
	if !strings.Contains(*readErr, "too large") {
		t.Errorf("读取错误应为 body 超限，实得 %q", *readErr)
	}
}

func TestBodyLimitNonPositiveDisables(t *testing.T) {
	for _, maxBytes := range []int64{0, -1} {
		r, ran, _ := bodyLimitEngine(maxBytes)
		req := httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader(strings.Repeat("z", 1<<20)))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code == http.StatusRequestEntityTooLarge || *ran != 1 {
			t.Errorf("maxBytes=%d 应为应急放行档，实得 状态码=%d handler=%d 次", maxBytes, rec.Code, *ran)
		}
	}
}

func TestBodyLimitFromEnv(t *testing.T) {
	const mib = 1024 * 1024
	cases := []struct {
		name  string
		unset bool
		raw   string
		want  int64
	}{
		{"未设置时回落默认", true, "", DefaultMaxJSONBodyMB * mib},
		{"空白回落默认", false, "   ", DefaultMaxJSONBodyMB * mib},
		{"非法值回落默认", false, "eight", DefaultMaxJSONBodyMB * mib},
		{"显式 0 是关闭档而非零上限", false, "0", 0},
		{"负数按关闭档读", false, "-3", 0},
		{"合法值按 MB 换算", false, "12", 12 * mib},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MAX_JSON_BODY_MB", "sentinel") // 注册 cleanup，用例结束后原值自动还原
			if tc.unset {
				if err := os.Unsetenv("MAX_JSON_BODY_MB"); err != nil {
					t.Fatalf("清理环境变量失败: %v", err)
				}
			} else {
				t.Setenv("MAX_JSON_BODY_MB", tc.raw)
			}
			if got := BodyLimitFromEnv(); got != tc.want {
				t.Errorf("BodyLimitFromEnv()=%d 期望 %d（raw=%q unset=%v）", got, tc.want, tc.raw, tc.unset)
			}
		})
	}
}

// gin 引擎自带的默认缓冲取自 `gin.New()` 的零值字段（不硬编码常量，升级 gin 改了默认值也能测出来）。
// 本仓最大的合法单文件是 50MB（知识库导入 MaxUploadFileSize），远超任何"值得留在内存里"的量，
// 且超出部分 gin 会自动落临时文件 —— 所以这道装配只允许**收紧**默认值，
// 抬高它等于给每个并发上传请求多发一份内存。
func TestMultipartMemoryOnlyTightensGinDefault(t *testing.T) {
	ginDefault := gin.New().MaxMultipartMemory
	got := MaxMultipartMemoryBytes()
	if got <= 0 {
		t.Fatalf("MaxMultipartMemoryBytes()=%d，非正值会让 gin 走 ParseMultipartForm 的异常路径", got)
	}
	if got > ginDefault {
		t.Errorf("multipart 内存缓冲 %d > gin 引擎默认 %d ⇒ 这道装配把内存占用抬高了而不是收紧了",
			got, ginDefault)
	}
}

// 前提校验（不是"新行为"的用例）：把 MaxMultipartMemory 从 gin 默认的 32MB 调到 8MB，
// 靠的是"超出缓冲的部分自动落临时文件、SaveUploadedFile 仍拿到完整原件"这条假设。
// 假设不成立时这里会红（文件大小/内容对不上），而不是等生产上传悄悄截断。
// 摘掉 multipart 跳过（= 电池里的 M3）同样会把它打红：9MB 上传会被 8MB 全局值直接 413。
// 反之，单独调大/调小缓冲的数值不会让它红——那是等价变异，不为它编断言。
func TestMultipartUploadLargerThanMemoryBufferSurvives(t *testing.T) {
	const fileSize = 9 << 20 // 故意大于 MaxMultipartMemoryBytes() 的 8MB
	if int64(fileSize) <= MaxMultipartMemoryBytes() {
		t.Fatalf("夹具前置不成立：上传体 %d 不大于内存缓冲 %d，测不到「超出即落盘」这条路径",
			fileSize, MaxMultipartMemoryBytes())
	}

	r := gin.New()
	r.MaxMultipartMemory = MaxMultipartMemoryBytes()
	r.Use(BodyLimit(BodyLimitFromEnv()))
	saved := new(int)
	r.POST("/upload", func(c *gin.Context) {
		fh, err := c.FormFile("file")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}
		dst := filepath.Join(c.PostForm("dest"), fh.Filename)
		if err := c.SaveUploadedFile(fh, dst); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
			return
		}
		got, err := os.ReadFile(dst)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
			return
		}
		if len(got) != fileSize {
			c.JSON(http.StatusInternalServerError, gin.H{"err": "落盘字节数与上传体不一致"})
			return
		}
		*saved = len(got)
		c.JSON(http.StatusOK, gin.H{"saved": len(got)})
	})

	dir := t.TempDir()
	payload := make([]byte, fileSize)
	for i := range payload {
		payload[i] = byte('a' + i%26) // 非全零：截断成稀疏文件也能被发现
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("dest", dir); err != nil {
		t.Fatalf("写表单字段失败: %v", err)
	}
	part, err := mw.CreateFormFile("file", "big.bin")
	if err != nil {
		t.Fatalf("建表单文件段失败: %v", err)
	}
	if _, err := part.Write(payload); err != nil {
		t.Fatalf("写表单文件失败: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("关闭 multipart writer 失败: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("9MB 上传在 8MB 缓冲 + 8MB 全局封顶下返回 %d：%s", rec.Code, rec.Body.String())
	}
	if *saved != fileSize {
		t.Errorf("落盘 %d 字节，期望 %d ⇒ 超出内存缓冲的部分没有被完整接住", *saved, fileSize)
	}
}

// 全局默认值不得比仓内既有的按端点上界更紧：`ORDER_WEBHOOK_MAX_BODY_BYTES` 允许运维把公网
// webhook 调到 maxWebhookMaxBody，全局值若低于该上界，这条 env 的高段就静默失效。
func TestGlobalDefaultDoesNotTightenExistingCaps(t *testing.T) {
	global := int64(DefaultMaxJSONBodyMB) * 1024 * 1024
	if global < maxWebhookMaxBody {
		t.Errorf("全局上限 %d < webhook 可调上界 %d ⇒ 既有 env 的高段会被全局封顶吃掉",
			global, maxWebhookMaxBody)
	}
}
