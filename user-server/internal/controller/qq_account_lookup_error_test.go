// qq_account_lookup_error_test.go 「查 QQ 账号」失败的语义分流。
//
// 立起来的不变量：只有"这条真没有"才 404；库/租户上下文等内部故障必须 500，
// 不能统一说成「账号不存在」——那会把故障指到数据上（A19 排查时就吃过这个亏）。

package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func newQQLookupTestCtx() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/qq/accounts/1", nil)
	return ctx, rec
}

func TestAbortQQAccountLookup_RecordNotFoundIs404(t *testing.T) {
	ctx, rec := newQQLookupTestCtx()
	abortQQAccountLookup(ctx, gorm.ErrRecordNotFound)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("记录不存在应回 404，实际 %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAbortQQAccountLookup_InternalErrorIs500(t *testing.T) {
	ctx, rec := newQQLookupTestCtx()
	abortQQAccountLookup(ctx, errors.New("dial tcp 127.0.0.1:8202: connect: connection refused"))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("库故障应回 500 而不是 404，实际 %d body=%s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "connection refused") {
		t.Errorf("500 响应应带原始错误便于排查，实际 body=%s", body)
	}
}
