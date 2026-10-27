// customer_service_plus_lock_test.go 编辑锁的持有人判定（A7）。
//
// 立起来的不变量：不带 body 的 DELETE 只能解**自己的**锁。
// 修前 holder 为空会跳过持有人比对（service 的 `holder != ""` 分支），
// 任何登录用户 DELETE 一下就能把别人正编辑着的会话锁解掉。

package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// serveRelease 走真路由，让 :id 参数按 gin 的规则落到 ctx.Param("id")。
func serveRelease(t *testing.T, ctrl *CustomerServicePlusController, sessionID string, userID uint) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.DELETE("/api/customer-sessions/:id/edit-lock", func(c *gin.Context) {
		c.Set("user_id", userID)
		ctrl.ReleaseEditLock(c)
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/customer-sessions/"+sessionID+"/edit-lock", nil)
	r.ServeHTTP(rec, req)
	return rec
}

func TestReleaseEditLock_EmptyHolderOnlyReleasesOwnLock(t *testing.T) {
	svc := service.NewCustomerServicePlusService()
	ctrl := NewCustomerServicePlusController()
	ctrl.svc = svc
	ctx := context.Background()

	const sid = "sess-lock-owner"
	if _, ok := svc.AcquireEditLock(ctx, sid, "user:26"); !ok {
		t.Fatal("前置不成立：26 号用户没抢到锁")
	}

	// 26 号自己不带 body 释放 → 成功
	if rec := serveRelease(t, ctrl, sid, 26); rec.Code != http.StatusOK {
		t.Fatalf("本人释放自己的锁应成功，实际 %d body=%s", rec.Code, rec.Body.String())
	}

	// 99 号抢到锁后，26 号不带 body 释放 → 必须 404（不能解别人的锁）
	const sid2 = "sess-lock-other"
	if _, ok := svc.AcquireEditLock(ctx, sid2, "user:99"); !ok {
		t.Fatal("前置不成立：99 号用户没抢到锁")
	}
	if rec := serveRelease(t, ctrl, sid2, 26); rec.Code != http.StatusNotFound {
		t.Fatalf("解别人的锁应回 404，实际 %d body=%s", rec.Code, rec.Body.String())
	}
	if _, stillLocked := svc.GetEditLock(ctx, sid2); !stillLocked {
		t.Fatal("别人的锁被解掉了：A7 回归")
	}
}
