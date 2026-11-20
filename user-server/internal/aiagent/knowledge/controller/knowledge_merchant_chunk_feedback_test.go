package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/aiagent/knowledge/service"
)

func TestChunkWriteIndexingFailureReturnsSafeServiceUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/knowledge-merchant/chunks/7", nil)

	replyChunkWriteError(c, errors.Join(service.ErrChunkIndexingFailed, errors.New("provider secret detail")))

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d (body %s)", w.Code, http.StatusServiceUnavailable, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "索引更新失败") {
		t.Fatalf("safe partial-persistence explanation missing: %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "provider secret detail") {
		t.Fatalf("provider detail leaked in response: %s", w.Body.String())
	}
}
