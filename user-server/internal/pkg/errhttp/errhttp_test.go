package errhttp

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

func newTestCtx(t *testing.T) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	return ctx, rec
}

func TestHandleDBError_NilError(t *testing.T) {
	ctx, rec := newTestCtx(t)
	if HandleDBError(ctx, nil, "获取") {
		t.Fatal("nil error should return false (not handled)")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("no response should be written, got %d", rec.Code)
	}
}

func TestHandleDBError_NotFound(t *testing.T) {
	ctx, rec := newTestCtx(t)
	handled := HandleDBError(ctx, errors.New("记录不存在"), "获取短链")
	if !handled {
		t.Fatal("error should be handled")
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("not-found error should map to 404, got %d", rec.Code)
	}
}

func TestHandleDBError_OtherError(t *testing.T) {
	ctx, rec := newTestCtx(t)
	handled := HandleDBError(ctx, errors.New("db boom"), "创建活码")
	if !handled {
		t.Fatal("error should be handled")
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("db error should map to 500, got %d", rec.Code)
	}
}

func TestHandleServiceError_BusinessErrorMaps400(t *testing.T) {
	ctx, rec := newTestCtx(t)
	handled := HandleServiceError(ctx, errors.New("系统模板不能修改"))
	if !handled {
		t.Fatal("error should be handled")
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("business error should map to 400, got %d", rec.Code)
	}
}

func TestIsNotFoundError_Matchers(t *testing.T) {
	if !IsNotFoundError(errors.New("user not found")) {
		t.Fatal("english 'not found' should match")
	}
	if IsNotFoundError(errors.New("internal error")) {
		t.Fatal("unrelated error should not match")
	}
	_ = response.Success // 保持 response 包引用（协议一致性）
}
