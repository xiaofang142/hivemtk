package bridge

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

type boundedReadProbe struct {
	data     []byte
	pos      int
	limit    int
	exceeded bool
}

func (r *boundedReadProbe) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := len(p)
	if n > len(r.data)-r.pos {
		n = len(r.data) - r.pos
	}
	if r.pos+n > r.limit {
		r.exceeded = true
		return 0, errors.New("read exceeded permitted body bound")
	}
	copy(p, r.data[r.pos:r.pos+n])
	r.pos += n
	return n, nil
}

// 超大 body 是纯协议错误，必须在 identify_code/DB 解析前返回 413；有无 Content-Length
// 都应按真实读取字节数执行上限，不能先无上限 ReadAll 再让缺码 404 掩盖。
func TestHTTPIngestHandler_BodyLimitBeforeIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	large := strings.Repeat("x", HTTPIngestMaxBodySize+1)
	newHandler := func() *BridgeIngestHandler {
		return NewBridgeIngestHandlerWithMock(
			func(context.Context, *model.MessageEvent) (*service.InboxIngressResult, error) {
				t.Fatal("超限 body 不得进入业务处理")
				return nil, nil
			},
			func(context.Context, *model.MessageEvent, string) error {
				t.Fatal("超限 body 不得写历史")
				return nil
			},
		)
	}

	t.Run("logging-preview-read-is-bounded", func(t *testing.T) {
		probe := &boundedReadProbe{data: append([]byte(large), 'x'), limit: HTTPIngestMaxBodySize + 1}
		req := httptest.NewRequest(http.MethodPost, "/api/bridge/ingest?channel=douyin&account_id=1", nil)
		req.Body = io.NopCloser(probe)
		req.ContentLength = -1
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = req
		info := collectHTTPRequestInfo(c)
		if probe.exceeded {
			t.Fatal("collectHTTPRequestInfo/readBodyForLog 发生了无上限读取")
		}
		if info.BodySize > HTTPIngestMaxBodySize+1 {
			t.Fatalf("预览读取字节数应被限制，实际 %d", info.BodySize)
		}
	})

	t.Run("content-length-over-limit-no-code", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/bridge/ingest?channel=douyin&account_id=1", strings.NewReader(large))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		newHandler().HandleHTTPIngest(c)
		if w.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("已知超限 Content-Length 应 413，实际 %d", w.Code)
		}
	})

	t.Run("unknown-length-over-limit-no-code", func(t *testing.T) {
		probe := &boundedReadProbe{data: append([]byte(large), 'x'), limit: HTTPIngestMaxBodySize + 1}
		req := httptest.NewRequest(http.MethodPost, "/api/bridge/ingest?channel=douyin&account_id=1", nil)
		req.Body = io.NopCloser(probe)
		req.ContentLength = -1
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		newHandler().HandleHTTPIngest(c)
		if w.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("未知长度超限 body 应 413，实际 %d", w.Code)
		}
		if probe.exceeded {
			t.Fatal("未知长度 body 读取超过 max+1，预览/真实读取未绑定")
		}
	})

	t.Run("configured-limit-before-identity", func(t *testing.T) {
		db := setupBridgeTestDB(t, &model.ConfigParam{})
		param := &model.ConfigParam{Group: "bridge", Key: "ingest_max_body_bytes", Name: "test body limit", ValueType: "int", Value: "1024", DefaultValue: "1024"}
		if err := db.Create(param).Error; err != nil {
			t.Fatalf("seed body limit param: %v", err)
		}
		service.SetGlobalForTest(service.NewConfigParamService())
		t.Cleanup(func() { service.SetGlobalForTest(nil) })
		req := httptest.NewRequest(http.MethodPost, "/api/bridge/ingest?channel=douyin&account_id=1", strings.NewReader(strings.Repeat("x", 1025)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		newHandler().HandleHTTPIngest(c)
		if w.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("配置上限应在身份解析前生效，实际 %d", w.Code)
		}
	})

	t.Run("valid-size-ingest", func(t *testing.T) {
		db := setupBridgeTestDB(t, &model.Channel{}, &model.ChannelAccount{}, &model.ChannelIdentifyCode{}, &model.MessageHub{})
		fixture := seedCanonicalBridgeFixture(t, db, 1, "douyin", "body-limit-valid")
		body := fmt.Sprintf(`{"channel":"douyin","account_id":"%s","messages":[{"event_id":"body-limit-valid-1","sender_id":"customer","content":"hi"}]}`, fixture.AccountID)
		probe := &boundedReadProbe{data: []byte(body), limit: HTTPIngestMaxBodySize + 1}
		calls := 0
		h := NewBridgeIngestHandlerWithMock(func(_ context.Context, event *model.MessageEvent) (*service.InboxIngressResult, error) {
			calls++
			if event.EventID != "body-limit-valid-1" || event.Content != "hi" {
				t.Fatalf("有界预览缓存必须原样供真实 ingest 使用，实际 event=%+v", event)
			}
			return &service.InboxIngressResult{Accepted: true}, nil
		}, func(context.Context, *model.MessageEvent, string) error { return nil })
		req := httptest.NewRequest(http.MethodPost, "/api/bridge/ingest?channel=douyin&account_id="+fixture.AccountID, nil)
		req.Body = io.NopCloser(probe)
		req.ContentLength = -1
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Channel-Identify-Code", fixture.Code)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		h.HandleHTTPIngest(c)
		if w.Code != http.StatusOK || calls != 1 {
			t.Fatalf("合法大小 ingest 不得回归，实际 status=%d calls=%d body=%s", w.Code, calls, w.Body.String())
		}
		if probe.exceeded || probe.pos != len(body) {
			t.Fatalf("预览与真实 ingest 必须共用同一有界读取，实际 exceeded=%v read=%d want=%d", probe.exceeded, probe.pos, len(body))
		}
		if c.Request.ContentLength != int64(len(body)) {
			t.Fatalf("复用 body 的 Content-Length 应等于真实字节数，实际 %d want=%d", c.Request.ContentLength, len(body))
		}
	})

	t.Run("valid-code-over-limit", func(t *testing.T) {
		db := setupBridgeTestDB(t, &model.Channel{}, &model.ChannelAccount{}, &model.ChannelIdentifyCode{})
		fixture := seedCanonicalBridgeFixture(t, db, 1, "douyin", "body-limit")
		req := httptest.NewRequest(http.MethodPost, "/api/bridge/ingest?channel=douyin&account_id="+fixture.AccountID, strings.NewReader(large))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Channel-Identify-Code", fixture.Code)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		newHandler().HandleHTTPIngest(c)
		if w.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("合法识别码也不得放行超限 body，实际 %d", w.Code)
		}
	})
}

// 防止预览修复后又在 HandleHTTPIngest 里恢复第二次无上限 ReadAll。
func TestHTTPIngestHandler_NoUnboundedBodyReads(t *testing.T) {
	raw, err := os.ReadFile("handler_http.go")
	if err != nil {
		t.Fatalf("读取 handler_http.go 失败: %v", err)
	}
	text := string(raw)
	if strings.Contains(text, "io.ReadAll(c.Request.Body)") || strings.Contains(text, "io.ReadAll(rc)") {
		t.Fatal("ingest 真实读取/预览读取不得使用无上限 io.ReadAll")
	}
	if !strings.Contains(text, "io.LimitReader(rc, int64(maxBodyBytes)+1)") {
		t.Fatal("body 预览读取必须绑定 maxBodyBytes+1")
	}
	if !strings.Contains(text, "collectHTTPRequestInfoWithLimit(c, maxBodySize)") {
		t.Fatal("HandleHTTPIngest 必须复用有界预览读取返回的 body")
	}
}
