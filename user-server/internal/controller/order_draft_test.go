// order_draft_test.go 草稿 HTTP 出口的关闸、身份、参数校验与状态码。
//
// 底座一律用内存服务（NewOrderDraftService）而不是 DB：这一层要判的是
// "路由在位但服务缺席回什么""匿名请求回什么""服务侧哨兵翻成哪个状态码"，
// 换成真库只会把这些判据埋在连接问题后面（同 human_task_routes_test.go 的取舍）。
package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

const orderDraftFlagForTest = "FF_LTC_ORDER_DRAFT_DB"

// orderDraftTestEnv 造一个已鉴权的引擎。userID 为空表示"匿名"（中间件不写 user_id）。
func orderDraftTestEnv(t *testing.T, svc *service.OrderDraftService, userID string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		if userID != "" {
			c.Set("user_id", userID)
		}
		c.Next()
	})
	NewOrderDraftController(svc, orderDraftFlagForTest).RegisterRoutes(engine.Group("/api"))
	return engine
}

func orderDraftDo(t *testing.T, engine *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

// orderDraftDecode 解开 {code,data,message} 信封。
//
// 空响应体必须是 t.Fatal：只 Errorf 的话下面每个字段读的都是零值，
// 一次"根本没解码成功"会报成一串毫不相干的断言失败。
func orderDraftDecode(t *testing.T, rec *httptest.ResponseRecorder) (map[string]any, string) {
	t.Helper()
	if rec.Body.Len() == 0 {
		t.Fatalf("响应体为空，status=%d", rec.Code)
	}
	var envelope struct {
		Code    any            `json:"code"`
		Message string         `json:"message"`
		Data    map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("响应不是合法 JSON（%v）：%s", err, rec.Body.String())
	}
	return envelope.Data, envelope.Message
}

// orderDraftSeed 造一张待确认草稿，返回它的 id。
func orderDraftSeed(t *testing.T, svc *service.OrderDraftService, customerID, ownerID, product string, price float64, qty int) string {
	t.Helper()
	d, err := svc.CreateManual(context.Background(), &service.CreateDraftRequest{
		CustomerID:  customerID,
		OwnerID:     ownerID,
		ProductName: product,
		Quantity:    qty,
		UnitPrice:   price,
	})
	if err != nil {
		t.Fatalf("造草稿失败：%v", err)
	}
	return d.ID
}

func mustOrderDraft(t *testing.T, svc *service.OrderDraftService, id string) *service.OrderDraft {
	t.Helper()
	d, err := svc.GetByID(context.Background(), id)
	if err != nil || d == nil {
		t.Fatalf("读回草稿 %s 失败：%v / %v", id, err, d)
	}
	return d
}

// TestOrderDraftController_NotAssembledIs503 未装配 ⇒ 五条端点全部 503，且指回旗子名。
//
// 这一条是本卡最要关的闸：服务缺席时最容易写成的"贴心"版本是回一份空列表，
// 而空列表在销售工作台上的意思是"今天没有待确认草稿"。
func TestOrderDraftController_NotAssembledIs503(t *testing.T) {
	engine := orderDraftTestEnv(t, nil, "7")
	cases := []struct{ method, path, body string }{
		{http.MethodGet, "/api/manage/order-drafts", ""},
		{http.MethodGet, "/api/manage/order-drafts/draft_1", ""},
		{http.MethodPost, "/api/manage/order-drafts/draft_1/confirm", ""},
		{http.MethodPost, "/api/manage/order-drafts/draft_1/cancel", `{"reason":"客户改主意"}`},
		{http.MethodPatch, "/api/manage/order-drafts/draft_1", `{"quantity":2}`},
	}
	for _, tc := range cases {
		rec := orderDraftDo(t, engine, tc.method, tc.path, tc.body)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s 期望 503，实际 %d：%s", tc.method, tc.path, rec.Code, rec.Body.String())
			continue
		}
		_, msg := orderDraftDecode(t, rec)
		if !strings.Contains(msg, orderDraftFlagForTest) {
			t.Errorf("%s %s 的 503 必须点出该改的旗子名，实际 %q", tc.method, tc.path, msg)
		}
	}
}

// TestOrderDraftController_AllEntrypointsRequireIdentity 五条出口匿名一律 401。
//
// 确认人/取消人是要写进草稿元数据的归因键；放匿名请求过去等于攒出一批
// "不知道谁处理的"草稿，而 401 与 400 搞混还会让前端去申诉参数而不是去补登录态。
func TestOrderDraftController_AllEntrypointsRequireIdentity(t *testing.T) {
	svc := service.NewOrderDraftService(nil)
	id := orderDraftSeed(t, svc, "cust-a", "", "云企版", 1999, 1)
	engine := orderDraftTestEnv(t, svc, "")
	cases := []struct{ method, path, body string }{
		{http.MethodGet, "/api/manage/order-drafts", ""},
		{http.MethodGet, "/api/manage/order-drafts/" + id, ""},
		{http.MethodPost, "/api/manage/order-drafts/" + id + "/confirm", ""},
		{http.MethodPost, "/api/manage/order-drafts/" + id + "/cancel", `{"reason":"客户预算取消"}`},
		{http.MethodPatch, "/api/manage/order-drafts/" + id, `{"quantity":2}`},
	}
	for _, tc := range cases {
		rec := orderDraftDo(t, engine, tc.method, tc.path, tc.body)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s 期望 401，实际 %d：%s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
	// 匿名列表失败后不该有任何写动作。
	if cur := mustOrderDraft(t, svc, id); cur.Status != service.DraftStatusPending {
		t.Errorf("匿名请求不该改动草稿，实际状态 %s", cur.Status)
	}
}

// TestOrderDraftController_ListIsSharedPool 待确认列表是共享池：默认不按人过滤。
//
// 反过来说，如果这里默认按 operator 过滤，AI 提取出来的那批草稿（owner_id="system"）
// 会从每个人的列表里消失，只剩 stats 上一个计数 —— 本卡要打通的正是这一步。
func TestOrderDraftController_ListIsSharedPool(t *testing.T) {
	svc := service.NewOrderDraftService(nil)
	orderDraftSeed(t, svc, "cust-a", "7", "云企版", 1999, 1)
	orderDraftSeed(t, svc, "cust-b", "", "数据中台", 8888, 1) // 空 owner ⇒ 服务层填 "system"

	engine := orderDraftTestEnv(t, svc, "9")
	rec := orderDraftDo(t, engine, http.MethodGet, "/api/manage/order-drafts", "")
	data, _ := orderDraftDecode(t, rec)
	list, ok := data["list"].([]any)
	if !ok {
		t.Fatalf("list 字段缺失或类型不对：%#v", data["list"])
	}
	if len(list) != 2 {
		t.Fatalf("池子里应有 2 条（含无归属那条），实际 %d：%#v", len(list), list)
	}
	if v, _ := data["view"].(string); v != "pool_pending" {
		t.Errorf("view 应说明这是共享待确认池，实际 %q", v)
	}
	if p, _ := data["pending"].(bool); !p {
		t.Error("待确认视图的 pending 必须为 true")
	}
	if total, _ := data["total"].(float64); total != 2 {
		t.Errorf("total 应与 list 条数一致，实际 %#v", data["total"])
	}

	rec = orderDraftDo(t, engine, http.MethodGet, "/api/manage/order-drafts?owner_id=7", "")
	data, _ = orderDraftDecode(t, rec)
	if got := len(data["list"].([]any)); got != 1 {
		t.Errorf("owner_id 过滤后应只剩 1 条，实际 %d", got)
	}

	rec = orderDraftDo(t, engine, http.MethodGet, "/api/manage/order-drafts?owner_id=all", "")
	data, _ = orderDraftDecode(t, rec)
	if got := len(data["list"].([]any)); got != 2 {
		t.Errorf("owner_id=all 应等价于不过滤（2 条），实际 %d", got)
	}
}

// TestOrderDraftController_ListTruncated limit 命中上界时 truncated 必须为真。
// total 只到"本次几条"，没有这个字段前端就会把截断读成"池子里就这两条"。
func TestOrderDraftController_ListTruncated(t *testing.T) {
	svc := service.NewOrderDraftService(nil)
	for _, p := range []string{"产品A", "产品B", "产品C"} {
		orderDraftSeed(t, svc, "cust-trunc", "7", p, 100, 1)
	}
	engine := orderDraftTestEnv(t, svc, "7")

	rec := orderDraftDo(t, engine, http.MethodGet, "/api/manage/order-drafts?limit=2", "")
	data, _ := orderDraftDecode(t, rec)
	if len(data["list"].([]any)) != 2 {
		t.Fatalf("limit=2 应只回 2 条，实际 %#v", data["list"])
	}
	if tr, _ := data["truncated"].(bool); !tr {
		t.Error("截断时 truncated 必须为 true")
	}

	rec = orderDraftDo(t, engine, http.MethodGet, "/api/manage/order-drafts?limit=10", "")
	data, _ = orderDraftDecode(t, rec)
	if tr, _ := data["truncated"].(bool); tr {
		t.Error("没截断时 truncated 必须为 false，否则前端会一直显示\"还有更多\"")
	}
}

// TestOrderDraftController_ListLimitParam limit 越界与非法值判 400，不做静默纠正。
func TestOrderDraftController_ListLimitParam(t *testing.T) {
	svc := service.NewOrderDraftService(nil)
	orderDraftSeed(t, svc, "cust-a", "7", "云企版", 1999, 1)
	engine := orderDraftTestEnv(t, svc, "7")
	for _, raw := range []string{"0", "-3", "abc", "201"} {
		rec := orderDraftDo(t, engine, http.MethodGet, "/api/manage/order-drafts?limit="+raw, "")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("limit=%s 期望 400，实际 %d：%s", raw, rec.Code, rec.Body.String())
		}
	}
}

// TestOrderDraftController_CustomerViewIncludesTerminal 客户视图含终态草稿，
// pending 必须为 false（否则已取消的一行会被前端算成待办）。
func TestOrderDraftController_CustomerViewIncludesTerminal(t *testing.T) {
	svc := service.NewOrderDraftService(nil)
	keep := orderDraftSeed(t, svc, "cust-x", "7", "云企版", 1999, 1)
	other := orderDraftSeed(t, svc, "cust-x", "8", "数据中台", 8888, 2)
	if err := svc.Cancel(context.Background(), orderDraftSeed(t, svc, "cust-x", "7", "驻场实施", 500, 1),
		"客户预算取消", "7"); err != nil {
		t.Fatalf("预置已取消草稿失败：%v", err)
	}

	engine := orderDraftTestEnv(t, svc, "7")
	rec := orderDraftDo(t, engine, http.MethodGet, "/api/manage/order-drafts?customer_id=cust-x", "")
	data, _ := orderDraftDecode(t, rec)
	list := data["list"].([]any)
	if len(list) != 3 {
		t.Fatalf("该客户应有 3 条（含已取消、含他人名下），实际 %d：%#v", len(list), list)
	}
	if p, _ := data["pending"].(bool); p {
		t.Error("客户视图含终态草稿，pending 必须为 false")
	}
	// 待确认池不该把已取消的那条也算进去。
	rec = orderDraftDo(t, engine, http.MethodGet, "/api/manage/order-drafts", "")
	data, _ = orderDraftDecode(t, rec)
	if len(data["list"].([]any)) != 2 {
		t.Errorf("池视图应排除已取消，实际 %#v", data["list"])
	}
	for _, item := range data["list"].([]any) {
		if id, _ := item.(map[string]any)["id"].(string); id == keep || id == other {
			continue
		}
		t.Errorf("池视图里出现了不该在的草稿：%#v", item)
	}
}

func TestOrderDraftController_GetNotFoundAndOversizedID(t *testing.T) {
	svc := service.NewOrderDraftService(nil)
	engine := orderDraftTestEnv(t, svc, "7")

	rec := orderDraftDo(t, engine, http.MethodGet, "/api/manage/order-drafts/draft_missing", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("不存在的草稿期望 404，实际 %d：%s", rec.Code, rec.Body.String())
	}

	rec = orderDraftDo(t, engine, http.MethodGet, "/api/manage/order-drafts/"+strings.Repeat("d", 65), "")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("超长 id 期望 400（回显放大器），实际 %d", rec.Code)
	}
}

// TestOrderDraftController_ConfirmProvisionalOrderID 没注入订单服务时确认仍然会翻状态，
// 但响应必须把"这个订单号是本进程临时生成的"说出口 —— 判据正是本卡开工前那处会被读反的半边。
func TestOrderDraftController_ConfirmProvisionalOrderID(t *testing.T) {
	svc := service.NewOrderDraftService(nil)
	id := orderDraftSeed(t, svc, "cust-a", "7", "云企版", 1999, 2)
	engine := orderDraftTestEnv(t, svc, "7")

	rec := orderDraftDo(t, engine, http.MethodPost, "/api/manage/order-drafts/"+id+"/confirm", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("确认期望 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	data, msg := orderDraftDecode(t, rec)
	if provisional, _ := data["order_provisional"].(bool); !provisional {
		t.Errorf("order_provisional 必须为 true，实际 %#v", data["order_provisional"])
	}
	if orderID, _ := data["order_id"].(string); orderID == "" {
		t.Error("确认结果里连临时订单号都没有，说明走的是失败分支")
	}
	// 值在场还不够：展示 order_id 的前端多半不读那个布尔，所以这句话要跟着值一起被读走。
	if !strings.Contains(msg, "临时") {
		t.Errorf("message 应说明订单号是临时生成的，实际 %q", msg)
	}
	if cur := mustOrderDraft(t, svc, id); cur.Status != service.DraftStatusConfirmed {
		t.Errorf("确认成功后草稿应是 confirmed，实际 %s", cur.Status)
	}

	rec = orderDraftDo(t, engine, http.MethodPost, "/api/manage/order-drafts/"+id+"/confirm", "")
	if rec.Code != http.StatusConflict {
		t.Errorf("重复确认期望 409（前端该刷新列表），实际 %d：%s", rec.Code, rec.Body.String())
	}
	// 第二次确认（409）不该再产生一个新订单号，也不该改动已经落在草稿上的那个。
	if cur := mustOrderDraft(t, svc, id); cur.OrderID != data["order_id"] {
		t.Errorf("重复确认后草稿上的订单号变了：草稿里 %q，首次响应 %v", cur.OrderID, data["order_id"])
	}
}

func TestOrderDraftController_CancelNeedsReason(t *testing.T) {
	svc := service.NewOrderDraftService(nil)
	id := orderDraftSeed(t, svc, "cust-a", "7", "云企版", 1999, 1)
	engine := orderDraftTestEnv(t, svc, "7")

	for _, body := range []string{`{}`, `{"reason":""}`, `{"reason":"   "}`, `not-json`} {
		rec := orderDraftDo(t, engine, http.MethodPost, "/api/manage/order-drafts/"+id+"/cancel", body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body=%s 期望 400，实际 %d：%s", body, rec.Code, rec.Body.String())
		}
	}
	if cur := mustOrderDraft(t, svc, id); cur.Status != service.DraftStatusPending {
		t.Errorf("被 400 拒掉的取消不该翻状态，实际 %s", cur.Status)
	}

	rec := orderDraftDo(t, engine, http.MethodPost, "/api/manage/order-drafts/"+id+"/cancel",
		`{"reason":"客户预算取消"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("正常取消期望 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	data, _ := orderDraftDecode(t, rec)
	if s, _ := data["status"].(string); s != string(service.DraftStatusCancelled) {
		t.Errorf("取消后的响应应带 cancelled，实际 %#v", data["status"])
	}
	if r, _ := data["cancel_reason"].(string); r != "客户预算取消" {
		t.Errorf("取消理由没落进响应，实际 %q", r)
	}
	// 谁取消的要留在响应里：这是"这单是谁收尾的"唯一凭据。
	if meta, ok := data["metadata"].(map[string]any); !ok || meta["cancelled_by"] != "7" {
		t.Errorf("响应里应带 cancelled_by=7，实际 %#v", data["metadata"])
	}

	rec = orderDraftDo(t, engine, http.MethodPost, "/api/manage/order-drafts/"+id+"/cancel",
		`{"reason":"再来一次"}`)
	if rec.Code != http.StatusConflict {
		t.Errorf("重复取消期望 409，实际 %d：%s", rec.Code, rec.Body.String())
	}
}

func TestOrderDraftController_Edit(t *testing.T) {
	svc := service.NewOrderDraftService(nil)
	id := orderDraftSeed(t, svc, "cust-a", "7", "云企版", 19.99, 3)
	engine := orderDraftTestEnv(t, svc, "7")

	// 空请求体：四个字段一个都没给，"改了"是假话。
	rec := orderDraftDo(t, engine, http.MethodPatch, "/api/manage/order-drafts/"+id, `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("空改期望 400，实际 %d：%s", rec.Code, rec.Body.String())
	}
	for _, body := range []string{`{"quantity":0}`, `{"unit_price":-1}`, `{"product_name":"  "}`} {
		rec = orderDraftDo(t, engine, http.MethodPatch, "/api/manage/order-drafts/"+id, body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body=%s 期望 400，实际 %d：%s", body, rec.Code, rec.Body.String())
		}
	}

	rec = orderDraftDo(t, engine, http.MethodPatch, "/api/manage/order-drafts/"+id,
		`{"quantity":3,"unit_price":19.99,"note":"客户砍价后确认"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("改价期望 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	data, _ := orderDraftDecode(t, rec)
	// 19.99*3 的二进制值是 59.969999999999995，不夹到分就会带着这个尾巴回给前端。
	if got, _ := data["total_amount"].(float64); got != 59.97 {
		t.Errorf("总金额应四舍五入到分（59.97），实际 %#v", data["total_amount"])
	}
	if note, _ := data["note"].(string); note != "客户砍价后确认" {
		t.Errorf("备注未落库，实际 %q", note)
	}

	// 已确认的草稿不可编辑 ⇒ 409。
	if _, err := svc.Confirm(context.Background(), id, "7"); err != nil {
		t.Fatalf("预置已确认草稿失败：%v", err)
	}
	rec = orderDraftDo(t, engine, http.MethodPatch, "/api/manage/order-drafts/"+id, `{"quantity":9}`)
	if rec.Code != http.StatusConflict {
		t.Errorf("编辑已确认草稿期望 409，实际 %d：%s", rec.Code, rec.Body.String())
	}
}

// TestOrderDraftController_EditMissingDraftIs404 编辑不存在的草稿 ⇒ 404，
// 而不是让服务层的 mutateIfPending 落败后回一句 500。
func TestOrderDraftController_EditMissingDraftIs404(t *testing.T) {
	svc := service.NewOrderDraftService(nil)
	engine := orderDraftTestEnv(t, svc, "7")
	rec := orderDraftDo(t, engine, http.MethodPatch, "/api/manage/order-drafts/draft_absent", `{"quantity":2}`)
	if rec.Code != http.StatusNotFound {
		t.Errorf("期望 404，实际 %d：%s", rec.Code, rec.Body.String())
	}
}

// TestOrderDraftController_ReplyErrorCodes 哨兵 → 状态码的映射，含"同名索引的
// duplicate key 才是 409、别的唯一键冲突仍是 500"这一条区分。
func TestOrderDraftController_ReplyErrorCodes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		err  error
		want int
	}{
		{fmt.Errorf("%w：草稿 draft_x 不存在", service.ErrOrderDraftNotFound), http.StatusNotFound},
		{fmt.Errorf("%w：草稿 draft_x 状态为 confirmed，不可确认", service.ErrOrderDraftNotPending), http.StatusConflict},
		{service.ErrOrderDraftExpired, http.StatusConflict},
		{errors.New(`ERROR: duplicate key value violates unique constraint "uq_order_draft_pending" (SQLSTATE 23505)`), http.StatusConflict},
		{errors.New(`ERROR: duplicate key value violates unique constraint "order_drafts_pkey" (SQLSTATE 23505)`), http.StatusInternalServerError},
		{errors.New("读取草稿失败: connection refused"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		engine := gin.New()
		want, err := tc.want, tc.err
		engine.GET("/probe", func(ctx *gin.Context) {
			(&OrderDraftController{}).replyError(ctx, err)
		})
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/probe", nil))
		if rec.Code != want {
			t.Errorf("err=%v 期望 %d，实际 %d", err, want, rec.Code)
		}
	}
}
