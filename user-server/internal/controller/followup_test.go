// followup_test.go 跟进提醒控制器（A11）。
//
// 只测决定可用性的出口（与 sales_workbench_test 同思路）：
//   - 服务未装配 ⇒ 503，不是 200+空壳；
//   - owner_id 缺失/date/limit/result 非法 ⇒ 400，垃圾参数不进读侧；
//   - 不存在 ⇒ 404、状态守卫（重复完成/终态取消）⇒ 409；
//   - 内存态 happy path：Schedule 后 today 可读、complete/cancel 可走通。
//
// 纯进程内内存态，不依赖 PG。
package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

func followupEngine(svc *service.FollowUpService) *gin.Engine {
	engine := gin.New()
	c := NewFollowUpController(svc)
	engine.GET("/today", c.GetToday)
	engine.GET("/pending", c.GetPending)
	engine.GET("/overdue", c.GetOverdue)
	engine.POST("/id/:id/complete", c.Complete)
	engine.POST("/id/:id/cancel", c.Cancel)
	return engine
}

// TestFollowUpController_Unassembled 未装配 ⇒ 五条全 503（不是空壳 200）
func TestFollowUpController_Unassembled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := followupEngine(nil)

	reqs := []struct {
		method string
		path   string
	}{
		{"GET", "/today?owner_id=u1"},
		{"GET", "/pending?owner_id=u1"},
		{"GET", "/overdue?owner_id=u1"},
		{"POST", "/id/rem_1/complete"},
		{"POST", "/id/rem_1/cancel"},
	}
	for _, r := range reqs {
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, httptest.NewRequest(r.method, r.path, nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s 期望 503，实际 %d", r.method, r.path, rec.Code)
		}
	}
}

// TestFollowUpController_Params 参数档：缺 owner / 坏 date / 坏 limit / 坏 result ⇒ 400
func TestFollowUpController_Params(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := service.NewFollowUpService(nil)
	engine := followupEngine(svc)

	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"today缺owner", "GET", "/today", ""},
		{"pending缺owner", "GET", "/pending", ""},
		{"overdue缺owner", "GET", "/overdue", ""},
		{"date格式错", "GET", "/today?owner_id=u1&date=2026/10/09", ""},
		{"limit非数", "GET", "/pending?owner_id=u1&limit=abc", ""},
		{"limit越界", "GET", "/pending?owner_id=u1&limit=999", ""},
		{"result非法", "POST", "/id/rem_1/complete", `{"result":"ok"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var body *strings.Reader
			if tc.body == "" {
				body = strings.NewReader("")
			} else {
				body = strings.NewReader(tc.body)
			}
			req := httptest.NewRequest(tc.method, tc.path, body)
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("%s 期望 400，实际 %d：%s", tc.path, rec.Code, rec.Body.String())
			}
		})
	}
}

// TestFollowUpController_Lifecycle happy path + 404/409 映射（状态守卫生效）
func TestFollowUpController_Lifecycle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := service.NewFollowUpService(nil)
	// 排程窗要按读侧的分桶口径算，不能写死 +1h：today 按"跑这条用例这台机器的自然日"分桶，
	// 而临近日午夜那一小时里 now+1h 已是次日，于是每天有一段必红窗（TZ=UTC+8 23:15 实测 list 为空）。
	// 贴到当天 23:00 之前排，due 与查询日恒在同一天；排到过去也不影响 pending 腿（ListPending 只认状态）。
	now := time.Now()
	delta := time.Hour
	if until := time.Date(now.Year(), now.Month(), now.Day(), 23, 0, 0, 0, now.Location()).Sub(now); until < delta {
		delta = until
	}
	r, err := svc.Schedule(context.Background(), "c1", "u1", service.ReminderCustom,
		delta, &service.ScheduleOptions{Title: "回访"})
	if err != nil {
		t.Fatalf("排程失败: %v", err)
	}
	engine := followupEngine(svc)

	// today 可读且带出该提醒
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest("GET", "/today?owner_id=u1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("today 期望 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), r.ID) {
		t.Errorf("today 应包含 %s，实际 %s", r.ID, rec.Body.String())
	}

	// pending 也带出（limit 缺省 50）
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest("GET", "/pending?owner_id=u1", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), r.ID) {
		t.Errorf("pending 期望 200 且含 %s，实际 %d：%s", r.ID, rec.Code, rec.Body.String())
	}

	// 完成成功
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest("POST", "/id/"+r.ID+"/complete",
		strings.NewReader(`{"result":"interested","note":"聊过"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("complete 期望 200，实际 %d：%s", rec.Code, rec.Body.String())
	}

	// 重复完成 ⇒ 409（状态守卫：防双记旅程/销售事件）
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest("POST", "/id/"+r.ID+"/complete", nil))
	if rec.Code != http.StatusConflict {
		t.Errorf("重复完成期望 409，实际 %d：%s", rec.Code, rec.Body.String())
	}

	// 终态再取消 ⇒ 409
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest("POST", "/id/"+r.ID+"/cancel", nil))
	if rec.Code != http.StatusConflict {
		t.Errorf("done 后取消期望 409，实际 %d：%s", rec.Code, rec.Body.String())
	}

	// 不存在 ⇒ 404
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest("POST", "/id/rem_404/complete", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("不存在完成期望 404，实际 %d：%s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest("POST", "/id/rem_404/cancel", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("不存在取消期望 404，实际 %d：%s", rec.Code, rec.Body.String())
	}
}

// TestFollowUpController_CancelThenComplete 取消后不可完成（409）
func TestFollowUpController_CancelThenComplete(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := service.NewFollowUpService(nil)
	r, err := svc.Schedule(context.Background(), "c2", "u2", service.ReminderCustom,
		time.Hour, nil)
	if err != nil {
		t.Fatalf("排程失败: %v", err)
	}
	engine := followupEngine(svc)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest("POST", "/id/"+r.ID+"/cancel", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("cancel 期望 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest("POST", "/id/"+r.ID+"/complete", nil))
	if rec.Code != http.StatusConflict {
		t.Errorf("canceled 后完成期望 409，实际 %d：%s", rec.Code, rec.Body.String())
	}
}
