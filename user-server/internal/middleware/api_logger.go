package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"

	"hivemtk-user/internal/pkg/utils/logger"
)

const (
	apiLogReqBodyCap  = 8 * 1024
	apiLogRespBodyCap = 16 * 1024
)

var apiLogSkipPrefixes = []string{"/health", "/healthz", "/readyz", "/metrics", "/swagger"}

var apiLogSensitiveKeys = map[string]struct{}{
	"password":                        {},
	"passwd":                          {},
	"token":                           {},
	"bridge_token":                    {},
	"secret":                          {},
	"authorization":                   {},
	"access_token":                    {},
	"refresh_token":                   {},
	"api_key":                         {},
	"apikey":                          {},
	"jwt":                             {},
	"secret_token":                    {},
	"x-telegram-bot-api-secret-token": {},
	// MA-02：键级清单补 PII 标识列（内容级脱敏由 SanitizeString 承担，
	// 这里管"这个字段本身就是手机号/邮箱"）。
	"phone":    {},
	"mobile":   {},
	"email":    {},
	"phone_no": {},
}

// APIInteractionLogger 记录每个 /api 请求的交互数据（请求 + 响应）。
//
// 行为：
//   - 仅对 /api 路径生效，健康检查等噪声端点跳过。
//   - 读取并记录请求体（随后还原，handler 仍可读到），敏感字段自动脱敏。
//   - 包装响应写入器捕获响应体；SSE/流式响应自动跳过，其余按上限截断。
//   - 状态码 >=500 记 Error、>=400 记 Warn、其余记 Info，事件名 api_interaction。
//
// 响应体落日志口径（MA-02）：**仅 status >= 400 记**，且键级 redact +
// 内容级 SanitizeString（手机号/邮箱/身份证/银行卡/长 token 段）双保险；
// 2xx 响应体（含 /api/customer 等全量客户数据的成功响应）不落日志——
// 成功面没有排障价值，却全量携带 PII。
//
// 注册顺序：须在 TraceMiddleware 之后，使日志自动携带 trace_id。
func APIInteractionLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		if !strings.HasPrefix(path, "/api") {
			c.Next()
			return
		}
		for _, p := range apiLogSkipPrefixes {
			if strings.HasPrefix(path, p) {
				c.Next()
				return
			}
		}

		var reqCap *cappedBufferWriter
		if c.Request.Body != nil {
			reqCap = &cappedBufferWriter{cap: apiLogReqBodyCap}
			c.Request.Body = io.NopCloser(io.TeeReader(c.Request.Body, reqCap))
		}

		w := &apiLogResponseWriter{ResponseWriter: c.Writer, cap: apiLogRespBodyCap}
		c.Writer = w

		start := time.Now()
		c.Next()
		latencyMs := time.Since(start).Milliseconds()

		reqBodyLog := ""
		if reqCap != nil {
			reqBodyLog = redactJSON(reqCap.Bytes(), apiLogReqBodyCap)
		}

		status := c.Writer.Status()
		// MA-02：仅 ≥400 记响应体（键级 redact + 内容级脱敏），2xx 不记——
		// 成功面无排障价值却全量携带客户 PII。
		respBodyLog := apiLogResponseSnapshot(status, w.snapshot())

		log := logger.Ctx(c.Request.Context())
		var ev *zerolog.Event
		switch {
		case status >= 500:
			ev = log.Error()
		case status >= 400:
			ev = log.Warn()
		default:
			ev = log.Info()
		}
		ev = ev.Str("event", "api_interaction").
			Str("method", c.Request.Method).
			Str("path", path).
			Int("status", status).
			Int64("latency_ms", latencyMs).
			Str("client_ip", c.ClientIP()).
			Str("request_body", reqBodyLog).
			Str("response_body", respBodyLog)
		if rawQuery := c.Request.URL.RawQuery; rawQuery != "" {
			ev = ev.Str("query", redactQuery(c.Request.URL.Query()))
		}
		if len(c.Errors) > 0 {
			ev = ev.Str("gin_errors", c.Errors.String())
		}
		ev.Msg("api_interaction")
	}
}

type apiLogResponseWriter struct {
	gin.ResponseWriter
	buf   bytes.Buffer
	cap   int
	full  bool
	skip  bool
	wrote bool
}

func (w *apiLogResponseWriter) Write(b []byte) (int, error) {
	if !w.wrote {
		w.wrote = true
		if ct := w.Header().Get("Content-Type"); ct != "" {
			if strings.Contains(ct, "text/event-stream") ||
				strings.Contains(ct, "application/x-ndjson") ||
				strings.Contains(ct, "application/octet-stream") {
				w.skip = true
			}
		}
	}
	if w.skip {
		return w.ResponseWriter.Write(b)
	}
	if !w.full {
		if w.buf.Len()+len(b) > w.cap {
			if remain := w.cap - w.buf.Len(); remain > 0 {
				w.buf.Write(b[:remain])
			}
			w.full = true
		} else {
			w.buf.Write(b)
		}
	}
	return w.ResponseWriter.Write(b)
}

func (w *apiLogResponseWriter) WriteString(s string) (int, error) {
	return w.Write([]byte(s))
}

func (w *apiLogResponseWriter) snapshot() string {
	if w.skip {
		return "(streaming response, not captured)"
	}
	s := w.buf.String()
	if w.full {
		s += "...(truncated)"
	}
	return s
}

type cappedBufferWriter struct {
	buf  bytes.Buffer
	cap  int
	full bool
}

func (w *cappedBufferWriter) Write(p []byte) (int, error) {
	if w.full {
		return len(p), nil
	}
	if w.buf.Len()+len(p) > w.cap {
		if remain := w.cap - w.buf.Len(); remain > 0 {
			w.buf.Write(p[:remain])
		}
		w.full = true
		return len(p), nil
	}
	w.buf.Write(p)
	return len(p), nil
}

func (w *cappedBufferWriter) Bytes() []byte { return w.buf.Bytes() }

// apiLogResponseSnapshot 响应体落日志口径（MA-02）：
//   - status < 400 ⇒ 空串（成功面不记——/api/customer 等成功响应全量携带客户数据，
//     排障价值低而 PII 落盘风险高）；
//   - status >= 400 ⇒ 键级 redact + 内容级 SanitizeString 双保险后的 ≤cap 片段。
func apiLogResponseSnapshot(status int, raw string) string {
	if status < 400 {
		return ""
	}
	return SanitizeString(redactJSON([]byte(raw), apiLogRespBodyCap))
}

func redactJSON(b []byte, capSize int) string {
	if len(b) == 0 {
		return ""
	}
	if len(b) > capSize {
		return string(b[:capSize]) + "...(truncated)"
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return string(b)
	}
	redactValue(v)
	out, err := json.Marshal(v)
	if err != nil {
		return string(b)
	}
	if len(out) > capSize {
		return string(out[:capSize]) + "...(truncated)"
	}
	return string(out)
}

// apiLogRedactedMask 键级脱敏统一掩码。query 串、请求体、响应体三处同用此常量，
// 防脱敏口径再次分化（N-1：audit 与 api_logger 共用同一掩码）。
const apiLogRedactedMask = "***REDACTED***"

func redactValue(v any) {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if _, ok := apiLogSensitiveKeys[strings.ToLower(k)]; ok {
				t[k] = apiLogRedactedMask
			} else {
				redactValue(val)
			}
		}
	case []any:
		for _, item := range t {
			redactValue(item)
		}
	}
}

// redactQueryValues 返回 q 的脱敏副本：键名（不区分大小写）命中 apiLogSensitiveKeys
// 时值替换为 apiLogRedactedMask，其余键值原样保留。它是 API 交互日志（api_logger.go
// 的 query 字段）与审计中间件（audit.go 的 detail.query，落 audit_logs 持久表）共享的
// 唯一实现——两处同用此函数 + 同一张键表 + 同一掩码，防脱敏口径再次分化（N-1）。
func redactQueryValues(q url.Values) url.Values {
	clone := url.Values{}
	for k, vs := range q {
		if _, ok := apiLogSensitiveKeys[strings.ToLower(k)]; ok {
			clone[k] = []string{apiLogRedactedMask}
		} else {
			clone[k] = vs
		}
	}
	return clone
}

// redactQuery 返回脱敏后的编码 query 串（API 交互日志 query 字段用）。
func redactQuery(q url.Values) string {
	return redactQueryValues(q).Encode()
}
