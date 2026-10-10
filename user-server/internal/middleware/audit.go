package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
)

// AuditManager 审计管理器（封装全局状态）
type AuditManager struct {
	droppedCount atomic.Int64
	logChan      chan *AuditEntry
	initOnce     sync.Once
	ctx          context.Context
	cancel       context.CancelFunc
}

// DefaultAuditManager 默认审计管理器
var DefaultAuditManager = NewAuditManager()

// DefaultAuditFlushInterval 是参数中心 middleware.audit_flush_interval 的兜底值。
const DefaultAuditFlushInterval = 5 * time.Second

// auditFlushIntervalProvider 由 internal/app 的参数装配层注入。
// 传 nil 表示不注入（装配顺序错时不该把兜底值顶掉）。
var auditFlushIntervalProvider = func() time.Duration { return DefaultAuditFlushInterval }

// SetAuditFlushIntervalProvider 注入审计刷盘周期读取器，仅装配层调用。
func SetAuditFlushIntervalProvider(fn func() time.Duration) {
	if fn != nil {
		auditFlushIntervalProvider = fn
	}
}

// AuditFlushInterval 返回生效中的审计刷盘周期。
// 这是「审计日志从内存队列落库到 DB」的最大滞留时间，敏感操作审计要可查就得靠它兜底。
func AuditFlushInterval() time.Duration {
	d := auditFlushIntervalProvider()
	if d <= 0 {
		return DefaultAuditFlushInterval
	}
	return d
}

// NewAuditManager 创建审计管理器
func NewAuditManager() *AuditManager {
	return &AuditManager{}
}

// Init 初始化审计日志异步处理器
func (m *AuditManager) Init() {
	m.initOnce.Do(func() {
		m.logChan = make(chan *AuditEntry, 1000)
		m.ctx, m.cancel = context.WithCancel(context.Background())
		go m.processAuditLogs()
	})
}

// Stop 停止审计处理器
func (m *AuditManager) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
}

// GetDroppedCount 获取降级计数
func (m *AuditManager) GetDroppedCount() int64 {
	return m.droppedCount.Load()
}

func (m *AuditManager) processAuditLogs() {
	const batchSize = 50
	flushInterval := AuditFlushInterval()

	batch := make([]*AuditEntry, 0, batchSize)
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	for {
		select {
		case log, ok := <-m.logChan:
			if !ok {
				if len(batch) > 0 {
					m.saveAuditBatch(batch)
				}
				return
			}
			batch = append(batch, log)
			if len(batch) >= batchSize {
				m.saveAuditBatch(batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			if len(batch) > 0 {
				m.saveAuditBatch(batch)
				batch = batch[:0]
			}
		case <-m.ctx.Done():
			return
		}
	}
}

func (m *AuditManager) saveAuditBatch(logs []*AuditEntry) {
	if len(logs) == 0 {
		return
	}

	sink := getAuditSink()
	if sink == nil {
		warnPortMissing("AuditSink")
		log.Printf("[audit] 丢弃 %d 条审计日志", len(logs))
		return
	}

	maxRetries := 3
	baseDelay := 100 * time.Millisecond

	for attempt := 0; attempt < maxRetries; attempt++ {
		failedLogs := make([]*AuditEntry, 0, len(logs))

		for _, entry := range logs {
			if err := sink.Save(context.Background(), entry); err != nil {
				failedLogs = append(failedLogs, entry)
			}
		}

		if len(failedLogs) == 0 {
			return
		}

		if attempt == maxRetries-1 {
			log.Printf("[audit] %d 条审计日志在 %d 次重试后仍写入失败", len(failedLogs), maxRetries)
		}

		if attempt < maxRetries-1 {
			time.Sleep(baseDelay * time.Duration(1<<uint(attempt)))
			logs = failedLogs
		}
	}
}

func (m *AuditManager) saveAuditLog(entry *AuditEntry) {
	m.Init()
	select {
	case m.logChan <- entry:

	default:

		m.droppedCount.Add(1)
		if sink := getAuditSink(); sink != nil {
			if err := sink.Save(context.Background(), entry); err != nil {
				log.Printf("[audit] 同步落库失败（已记入降级计数）: %v", err)
			}
		}
	}
}

// AuditConfig 审计配置
type AuditConfig struct {
	Enabled         bool
	ExcludePaths    []string
	SensitiveFields []string
}

// DefaultAuditConfig 默认审计配置
var DefaultAuditConfig = AuditConfig{
	Enabled: true,
	ExcludePaths: []string{
		"/api/health",
		"/api/system/info",
		"/api/team/logs",
	},
	SensitiveFields: []string{
		"password",
		"old_password",
		"new_password",
		"token",
		"secret",
	},
}

// AuditMiddleware 审计日志中间件
func AuditMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !DefaultAuditConfig.Enabled {
			c.Next()
			return
		}

		path := c.Request.URL.Path
		for _, excludePath := range DefaultAuditConfig.ExcludePaths {
			if path == excludePath {
				c.Next()
				return
			}
		}

		startTime := time.Now()

		var requestBody []byte
		if c.Request.Body != nil {
			requestBody, _ = io.ReadAll(c.Request.Body)
			c.Request.Body = io.NopCloser(bytes.NewBuffer(requestBody))
		}

		responseWriter := &auditResponseWriter{
			ResponseWriter: c.Writer,
			body:           &bytes.Buffer{},
		}
		c.Writer = responseWriter

		c.Next()

		method := c.Request.Method
		if method != "POST" && method != "PUT" && method != "DELETE" && method != "PATCH" {
			return
		}

		userID, _ := c.Get("user_id")
		username, _ := c.Get("username")

		if userID == nil {
			return
		}

		var sanitizedRequest any
		if len(requestBody) > 0 {
			var reqMap map[string]any
			if err := json.Unmarshal(requestBody, &reqMap); err == nil {
				sanitizedRequest = sanitizeMap(reqMap)
			}
		}

		action := getActionFromMethod(method)
		module := getModuleFromPath(path)
		resource := getResourceFromPath(path)

		detail := map[string]any{
			"method":      method,
			"path":        path,
			"query":       c.Request.URL.Query(),
			"status_code": c.Writer.Status(),
			"latency":     time.Since(startTime).Milliseconds(),
			"request":     sanitizedRequest,
		}

		if c.Writer.Status() >= 400 {
			var responseBody map[string]any
			if err := json.Unmarshal(responseWriter.body.Bytes(), &responseBody); err == nil {
				detail["error"] = responseBody
			}
		}

		detailJSON, _ := json.Marshal(detail)

		entry := &AuditEntry{
			UserID:     convertToUint(userID),
			Username:   convertToString(username),
			Action:     action,
			Module:     module,
			Resource:   resource,
			ResourceID: getResourceIDFromPath(path),
			Detail:     string(detailJSON),
			IP:         c.ClientIP(),
			UserAgent:  c.Request.UserAgent(),
		}

		DefaultAuditManager.saveAuditLog(entry)
	}
}

type auditResponseWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

func (w *auditResponseWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

func (w *auditResponseWriter) WriteString(s string) (int, error) {
	w.body.WriteString(s)
	return w.ResponseWriter.WriteString(s)
}

func sanitizeMap(m map[string]any) map[string]any {
	result := make(map[string]any)
	for k, v := range m {
		if isSensitiveField(k) {
			result[k] = "******"
		} else {
			switch val := v.(type) {
			case map[string]any:
				result[k] = sanitizeMap(val)
			default:
				result[k] = v
			}
		}
	}
	return result
}

func isSensitiveField(field string) bool {
	for _, sensitiveField := range DefaultAuditConfig.SensitiveFields {
		if field == sensitiveField {
			return true
		}
	}
	return false
}

func getActionFromMethod(method string) string {
	switch method {
	case "POST":
		return "create"
	case "PUT", "PATCH":
		return "update"
	case "DELETE":
		return "delete"
	default:
		return method
	}
}

func getModuleFromPath(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) >= 4 && parts[3] != "" {
		return parts[3]
	}
	return "unknown"
}

func getResourceFromPath(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) >= 4 && parts[3] != "" {
		return parts[3]
	}
	return "unknown"
}

func getResourceIDFromPath(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) >= 5 {
		for _, part := range parts[4:] {
			if _, err := strconv.Atoi(part); err == nil {
				return part
			}
		}
	}
	return ""
}

func convertToUint(v any) uint {
	switch val := v.(type) {
	case uint:
		return val
	case int:
		return uint(val)
	case int64:
		return uint(val)
	case float64:
		return uint(val)
	case string:

		if result, err := strconv.ParseUint(val, 10, 64); err == nil {
			return uint(result)
		}
		var digits []byte
		for i := 0; i < len(val); i++ {
			if val[i] >= '0' && val[i] <= '9' {
				digits = append(digits, val[i])
			}
		}
		if len(digits) > 0 {
			if result, err := strconv.ParseUint(string(digits), 10, 64); err == nil {
				return uint(result)
			}
		}
		return 0
	default:
		return 0
	}
}

func convertToString(v any) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	default:
		return ""
	}
}

// LogLogin 登录日志
func LogLogin(userID uint, username, ip, userAgent string) {
	entry := &AuditEntry{
		UserID:    userID,
		Username:  username,
		Action:    "login",
		Module:    "auth",
		Resource:  "session",
		IP:        ip,
		UserAgent: userAgent,
	}
	DefaultAuditManager.saveAuditLog(entry)
}

// LogLogout 登出日志
func LogLogout(userID uint, username, ip string) {
	entry := &AuditEntry{
		UserID:   userID,
		Username: username,
		Action:   "logout",
		Module:   "auth",
		Resource: "session",
		IP:       ip,
	}
	DefaultAuditManager.saveAuditLog(entry)
}

// LogCustom 自定义日志
func LogCustom(userID uint, username, action, module, resource, resourceID string, detail any) {
	detailJSON, _ := json.Marshal(detail)
	entry := &AuditEntry{
		UserID:     userID,
		Username:   username,
		Action:     action,
		Module:     module,
		Resource:   resource,
		ResourceID: resourceID,
		Detail:     string(detailJSON),
	}
	DefaultAuditManager.saveAuditLog(entry)
}

// DataChangeMiddleware 数据变更审计中间件
func DataChangeMiddleware(module, resource string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
	}
}

// GetAuditDroppedCount 暴露给监控/可观测层（兼容旧接口）
func GetAuditDroppedCount() int64 {
	return DefaultAuditManager.GetDroppedCount()
}
