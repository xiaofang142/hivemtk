package controller

// 批19d 契约锁：会话域不许把「读不出来」说成「不存在」。
// session 的六个读/写入口把所有 service 错误一律写成 `404 会话不存在`——两类结论被折成一句：
// 真的没有这条会话（用户该换个 id），和会话在、但库里读不动（连接/超时/数据故障，服务端该进监控）。
// 真机侧的观感是列表页明明有这条会话、点详情却弹「会话不存在」；同时一次服务端故障被记成
// 用户误操作（批10b 的 500 洗白问题，方向相反：那是客户端错误被记成故障，这是故障被记成客户端错误）。
// 断的是码与主语，不是措辞。
//
// 造内部错误的取径说明（先量后写，别照着想当然）：
//   - 试过「可空列写 NULL、模型侧是非指针 string」——实测 GORM 把 NULL 落成零值，读不报错
//     （value=NULL / url=NULL 两条腿都是绿的，见本文件首版 RED 记录），所以那条路证不到东西；
//   - 负数 id 的行 handler 根本 targeting 不到（parseID 先回 400）；
//   - 于是用真会发生的读失败：**库不可达**。它不碰共享测试库、不改 schema，
//     错误类型就是「非 not-found 的读失败」这一整族的代表。

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"hivemtk-user/internal/browser_automation/model"
	barepo "hivemtk-user/internal/browser_automation/repository"
	basvc "hivemtk-user/internal/browser_automation/service"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/pkg/utils"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

const (
	b19dOwner     = uint(781405)
	b19dMissingID = "987654321" // 测试库自增 id 到不了这个量级
)

// newB19dDeadSvc 会话服务，所有仓储指向一个必然连不上的库（127.0.0.1:1 无监听）。
// DisableAutomaticPing + SkipInitializeWithVersion 是必需的：否则 gorm.Open 自己就报错，
// 拿不到「查询时失败」这一族错误。
func newB19dDeadSvc(t *testing.T) *basvc.SessionService {
	t.Helper()
	dead, err := gorm.Open(postgres.New(postgres.Config{
		DSN: "host=127.0.0.1 port=1 user=b19d password=b19d dbname=b19d sslmode=disable connect_timeout=2",
	}), &gorm.Config{
		// 不关自动 ping 的话 gorm.Open 自己就连一次、当场返回失败，
		// 拿不到「句柄可用但查询必挂」这个夹具（前置锁会 Fatal，测不到 handler）。
		DisableAutomaticPing: true,
	})
	if err != nil {
		t.Fatalf("死库句柄构造失败：%v", err)
	}
	sessionRepo := barepo.NewBrowserSessionRepositoryWithDB(dead)
	stepRepo := barepo.NewBrowserStepRepositoryWithDB(dead)
	svc := basvc.NewSessionService(sessionRepo, stepRepo, basvc.NewExecutor(nil, sessionRepo, stepRepo, nil, nil))
	svc.SetCommandLogRepository(barepo.NewBrowserCommandLogRepositoryWithDB(dead))
	svc.SetLLMPlanRepository(barepo.NewBrowserLLMPlanRepositoryWithDB(dead))
	// 前置锁：夹具必须真的回一个「非 not-found」的读错误。
	// 少了这层，哪天 gorm/pgx 把连接失败折叠成 ErrRecordNotFound，六条断言会以「404 是对的」形态绿。
	_, e := svc.Get(context.Background(), 1, b19dOwner)
	switch {
	case e == nil:
		t.Fatal("夹具失效：不可达的库竟然读成功")
	case errors.Is(e, gorm.ErrRecordNotFound):
		t.Fatalf("夹具造出的是 not-found 而非读失败：%v", e)
	}
	return svc
}

func TestB19DSessionReadFailureIsNotReportedAsMissingSession(t *testing.T) {
	c := NewSessionController(newB19dDeadSvc(t))
	type leg struct {
		name   string
		h      gin.HandlerFunc
		method string
		body   string
	}
	legs := []leg{
		{"Get", c.Get, http.MethodGet, ""},
		{"ListSteps", c.ListSteps, http.MethodGet, ""},
		{"ListLogs", c.ListLogs, http.MethodGet, ""},
		{"Export", c.Export, http.MethodGet, ""},
		{"Stop", c.Stop, http.MethodPost, ""},
		// 批20 起放行必须带载荷哈希：不带的话 400 挡在服务层之前，
		// 这条腿要测的是「服务层读不动」，就得先把入参喂对。
		{"Confirm", c.Confirm, http.MethodPost, `{"payload_hash":"b19d-any-hash"}`},
		{"ConfirmGate", c.ConfirmGate, http.MethodGet, ""},
	}
	if len(legs) != 7 {
		t.Fatalf("会话域出口数变了（%d）：分流要逐个入口锁，少一个入口就是漏一个网", len(legs))
	}
	for _, l := range legs {
		httpCode, body := b19cCall(t, l.h, l.method, "/api/browser-automation/sessions/1/"+l.name,
			l.body, "1", b19dOwner)
		if httpCode == http.StatusNotFound || strings.Contains(body.Message, "会话不存在") {
			t.Errorf("%s：库读不动却回「会话不存在」（HTTP %d code=%v msg=%q）", l.name, httpCode, body.Code, body.Message)
		}
		if httpCode != http.StatusInternalServerError {
			t.Errorf("%s：HTTP %d want 500（code=%v msg=%q）", l.name, httpCode, body.Code, body.Message)
			continue
		}
		if body.Code != string(utils.ErrorCodeInternalError) {
			t.Errorf("%s：code=%v want %s", l.name, body.Code, utils.ErrorCodeInternalError)
		}
	}
}

// 反向锁：真的没有这条会话时，404 与「会话不存在」必须原样保留——分流不能把 not-found
// 一并洗成 500，否则前端丢掉「这条 id 不存在」的结论（同批10b 的对偶腿）。
func TestB19DMissingSessionStill404WithRightSubject(t *testing.T) {
	db := testutil.NewTestDB(t, &model.BrowserSession{}, &model.BrowserStep{})
	if db == nil {
		t.Fatal("测试库不可达：本文件的 404 主语无法判定（不 Skip，跳过等于没锁）")
	}
	sessionRepo := barepo.NewBrowserSessionRepositoryWithDB(db)
	stepRepo := barepo.NewBrowserStepRepositoryWithDB(db)
	svc := basvc.NewSessionService(sessionRepo, stepRepo, basvc.NewExecutor(nil, sessionRepo, stepRepo, nil, nil))
	svc.SetCommandLogRepository(barepo.NewBrowserCommandLogRepositoryWithDB(db))
	svc.SetLLMPlanRepository(barepo.NewBrowserLLMPlanRepositoryWithDB(db))
	c := NewSessionController(svc)
	handlers := map[string]string{
		"Get":         "",
		"ListSteps":   "",
		"ListLogs":    "",
		"Export":      "",
		"Stop":        "",
		"Confirm":     `{"payload_hash":"b19d-any-hash"}`, // 缺载荷会先吃 400，测不到「会话不存在」这条腿
		"ConfirmGate": "",
	}
	call := map[string]gin.HandlerFunc{
		"Get": c.Get, "ListSteps": c.ListSteps, "ListLogs": c.ListLogs,
		"Export": c.Export, "Stop": c.Stop, "Confirm": c.Confirm, "ConfirmGate": c.ConfirmGate,
	}
	if len(handlers) != len(call) || len(call) != 7 {
		t.Fatalf("会话域出口数与正锁不一致：%d/%d", len(handlers), len(call))
	}
	for name, body := range handlers {
		httpCode, b := b19cCall(t, call[name], http.MethodGet, "/api/browser-automation/sessions/"+b19dMissingID,
			body, b19dMissingID, b19dOwner)
		if httpCode != http.StatusNotFound {
			t.Errorf("%s 不存在的会话 → HTTP %d want 404（code=%v msg=%q）", name, httpCode, b.Code, b.Message)
			continue
		}
		if !strings.Contains(b.Message, "会话") {
			t.Errorf("%s 404 文案主语得是会话：%q", name, b.Message)
		}
		if strings.Contains(strings.ToLower(b.Message), "record not found") {
			t.Errorf("%s 把 gorm 原文回给了前端：%q", name, b.Message)
		}
	}
}

// 列表类出口保持原样：ListByUser / ListByTask 读失败时是 500 + 固定文案，不外泄驱动原文。
// 这条是「顺手改道」的护栏：分流改造若把列表也接进 mapper，用户弹条里就会出现 SQL 连接文本
// （列表页一次库抖动的正确提示只有「查询会话失败」这一句有用）。
func TestB19DListEndpointsKeepGenericMessage(t *testing.T) {
	c := NewSessionController(newB19dDeadSvc(t))
	legs := map[string]gin.HandlerFunc{"List": c.List, "ListByTask": c.ListByTask}
	if len(legs) != 2 {
		t.Fatalf("列表类出口数变了（%d）", len(legs))
	}
	for name, h := range legs {
		httpCode, body := b19cCall(t, h, http.MethodGet, "/api/browser-automation/sessions", "", "1", b19dOwner)
		if httpCode != http.StatusInternalServerError {
			t.Errorf("%s 读失败 → HTTP %d want 500（msg=%q）", name, httpCode, body.Message)
			continue
		}
		if body.Message != "查询会话失败" {
			t.Errorf("%s 文案 %q want「查询会话失败」（不能把驱动原文弹给用户）", name, body.Message)
		}
	}
}
