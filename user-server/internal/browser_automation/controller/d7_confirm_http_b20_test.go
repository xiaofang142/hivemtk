package controller

// D7 放行的 HTTP 面契约锁：一条放行请求在控制器上必须能分辨四件事——
// 批了（200 confirmed:true）、批错内容（409 载荷不符）、批不动（409 闸门在别的进程）、
// 没得批（200 confirmed:false），且**没带载荷就不许批**（400）。
// 为什么四态要在 HTTP 面各占一格：前三种在服务端看都是「这次没放行」，
// 对用户的下一步却是三个完全不同的动作（等一等 / 重新看内容 / 换实例）。
// 折成一句的代价是真机实测过的：会话页明明挂着闸门，弹窗却说「当前没有待确认的提交点」。
//
// granted / mismatch 两条需要进程内真有挂起闸门，控制器层造不出来（登记闸门是 Executor 私有
// 路径，为测试开一个登记口等于把生产面撑个洞）。因此本文件分两层：
//   - 状态→HTTP 的映射：writeConfirmResult 四态逐个断言；
//   - 可达的真实链路：缺载荷 400、库里未到期 d7_wait 判「在别的进程」、过期帧判「没得批」，
//     全部走真测试库真行，不用替身。
// 闸门语义本身（哈希比对/一次性消费/落帧）见 service 层 d7_gate_b20_test.go；
// 「库读不动不许说成会话不存在」由同包 session_error_b19d_test.go 的七腿扫描覆盖。

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/browser_automation/model"
	barepo "hivemtk-user/internal/browser_automation/repository"
	basvc "hivemtk-user/internal/browser_automation/service"
	"hivemtk-user/internal/pkg/testutil"

	"github.com/gin-gonic/gin"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const (
	b20User  = uint(781420)
	b20Hash  = "b20-payload-hash"
	b20ReqOK = `{"payload_hash":"` + b20Hash + `"}`
)

type b20Body struct {
	Code    any            `json:"code"`
	Message string         `json:"message"`
	Data    map[string]any `json:"data"`
}

// b20Call 与 b19cCall 同构，只是要多看 data 一层（confirmed 布尔就住在那儿）。
func b20Call(t *testing.T, h gin.HandlerFunc, method, idParam string, userID uint, body string) (int, b20Body) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(method, "/api/browser-automation/sessions/"+idParam+"/confirm", nil)
	if body != "" {
		req = httptest.NewRequest(method, req.URL.Path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	ctx.Request = req
	if idParam != "" {
		ctx.Params = gin.Params{{Key: "id", Value: idParam}}
	}
	ctx.Set("user_id", userID)
	h(ctx)
	var b b20Body
	if e := json.Unmarshal(w.Body.Bytes(), &b); e != nil {
		t.Fatalf("响应体不是合法 JSON（%v）: %s", e, w.Body.String())
	}
	return w.Code, b
}

func newB20SvcDB(t *testing.T) (*basvc.SessionService, *gorm.DB) {
	t.Helper()
	db := testutil.NewTestDB(t, &model.BrowserSession{}, &model.BrowserStep{}, &model.BrowserCommandLog{})
	if db == nil {
		t.Fatal("测试库不可达：本文件的四态分流全部依赖真行，Skip 等于没锁")
	}
	sessionRepo := barepo.NewBrowserSessionRepositoryWithDB(db)
	stepRepo := barepo.NewBrowserStepRepositoryWithDB(db)
	svc := basvc.NewSessionService(sessionRepo, stepRepo,
		basvc.NewExecutor(nil, sessionRepo, stepRepo, nil, nil))
	svc.SetCommandLogRepository(barepo.NewBrowserCommandLogRepositoryWithDB(db))
	return svc, db
}

// seedActiveSession 落一条运行中的会话（Confirm 的状态门前置：非 created/active 直接判「没得批」）。
func seedActiveSession(t *testing.T, db *gorm.DB, taskID uint) *model.BrowserSession {
	t.Helper()
	sess := &model.BrowserSession{TaskID: taskID, UserID: b20User, Status: "active"}
	if err := db.Create(sess).Error; err != nil {
		t.Fatalf("会话种子失败：%v", err)
	}
	t.Cleanup(func() {
		db.Unscoped().Delete(&model.BrowserCommandLog{}, "session_id = ?", sess.ID)
		db.Unscoped().Delete(&model.BrowserSession{}, sess.ID)
	})
	return sess
}

// okPtr 三态 ok 的测试侧构造器：生产侧的同名函数 verdict 长在 service 包，跨包拿不到，
// 而这里种子帧确实带结论（event 帧），必须给指针而不是裸 bool。
func okPtr(v bool) *bool { return &v }

// seedWaitFrame 落一帧 d7_wait（A10 的唯一跨进程凭据）：expiresAt 决定「还在别处等」还是「早没人等了」。
func seedWaitFrame(t *testing.T, db *gorm.DB, sess *model.BrowserSession, expiresAt time.Time, hash string) {
	t.Helper()
	row := &model.BrowserCommandLog{
		SessionID: sess.ID, TaskID: sess.TaskID, Seq: 1, Direction: "event",
		Action: "d7_wait", Ok: okPtr(true),
		Payload: datatypes.JSON(fmt.Sprintf(`{"payload_hash":%q,"step_index":2,"expires_at":%q}`,
			hash, expiresAt.Format(time.RFC3339Nano))),
	}
	if err := db.Create(row).Error; err != nil {
		t.Fatalf("d7_wait 帧种子失败：%v", err)
	}
}

// 1) 载荷必填：不带 payload_hash 的放行等于空白支票，控制器必须先挡下来（不进服务层）。
func TestB20ConfirmRequiresPayloadHash(t *testing.T) {
	svc, db := newB20SvcDB(t)
	sess := seedActiveSession(t, db, 781421)
	c := NewSessionController(svc)
	id := fmt.Sprint(sess.ID)
	for _, body := range []string{"", "{}", `{"payload_hash":"   "}`, `{"reason":"随便批一下"}`} {
		code, b := b20Call(t, c.Confirm, http.MethodPost, id, b20User, body)
		if code != http.StatusBadRequest {
			t.Errorf("body=%q → HTTP %d want 400（msg=%q）", body, code, b.Message)
			continue
		}
		if !strings.Contains(b.Message, "payload_hash") {
			t.Errorf("body=%q 的 400 文案没点名缺哪个字段：%q", body, b.Message)
		}
		if b.Data["confirmed"] == true {
			t.Errorf("body=%q 缺载荷竟回了 confirmed:true", body)
		}
	}
}

// 2) 四态→HTTP 映射：状态与码、文案一一对应，三种「没放行」都不许被读成「放行了」。
func TestB20ConfirmStatusMappingIsDistinct(t *testing.T) {
	legs := []struct {
		status   basvc.ConfirmStatus
		wantCode int
		keyword  string
	}{
		{basvc.ConfirmStatusGranted, http.StatusOK, "已确认放行"},
		{basvc.ConfirmStatusMismatch, http.StatusConflict, "不一致"},
		{basvc.ConfirmStatusOtherInstance, http.StatusConflict, "另一个"},
		{basvc.ConfirmStatusNone, http.StatusOK, "没有待确认"},
	}
	if len(legs) != 4 {
		t.Fatalf("放行结论态数与断言腿数不一致（%d）：新增一态就得决定它的 HTTP 语义", len(legs))
	}
	for _, l := range legs {
		gin.SetMode(gin.TestMode)
		w := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(w)
		ctx.Request = httptest.NewRequest(http.MethodPost, "/sessions/1/confirm", nil)
		writeConfirmResult(ctx, basvc.ConfirmResult{Status: l.status})
		var b b20Body
		if e := json.Unmarshal(w.Body.Bytes(), &b); e != nil {
			t.Fatalf("%s 响应不是 JSON：%v（%s）", l.status, e, w.Body.String())
		}
		if w.Code != l.wantCode {
			t.Errorf("%s → HTTP %d want %d（msg=%q）", l.status, w.Code, l.wantCode, b.Message)
		}
		if !strings.Contains(b.Message, l.keyword) {
			t.Errorf("%s 文案 %q 不含 %q——四态折成同句就是本批要消灭的形状", l.status, b.Message, l.keyword)
		}
		wantConfirmed := l.status == basvc.ConfirmStatusGranted
		if got := b.Data["confirmed"] == true; got != wantConfirmed {
			t.Errorf("%s 的 confirmed=%v want %v", l.status, got, wantConfirmed)
		}
		// data.status 是前端唯一可依赖的判别位：409 经 response.Error 会统一折成
		// DUPLICATE_ENTRY_3003（实测），只靠码分不清「批错内容」与「闸门在别处」。
		if got, _ := b.Data["status"].(string); got != string(l.status) {
			t.Errorf("data.status=%q want %q（机器可读判别位缺失=前端只能读文案）", got, l.status)
		}
	}
}

// 3) A10 跨进程查证（真库真行）：本进程内存没有闸门，但库里躺着一帧未到期的 d7_wait
// ⇒ 必须回「闸门在别的进程」；同一条帧一旦过期 ⇒ 回到「没有待确认的提交点」。
// 后者是前者的对偶锁：少了它，一条旧帧能让这个 session 永远被回答「在别处」。
func TestB20ConfirmGateElsewhereFromAuditFrame(t *testing.T) {
	svc, db := newB20SvcDB(t)
	c := NewSessionController(svc)

	sess := seedActiveSession(t, db, 781422)
	seedWaitFrame(t, db, sess, time.Now().Add(10*time.Minute), b20Hash)
	code, b := b20Call(t, c.Confirm, http.MethodPost, fmt.Sprint(sess.ID), b20User, b20ReqOK)
	if code != http.StatusConflict || !strings.Contains(b.Message, "另一个") {
		t.Errorf("未到期 d7_wait + 无内存闸门 → HTTP %d msg=%q，want 409「闸门在别的进程」", code, b.Message)
	}
	if b.Data["confirmed"] == true {
		t.Error("跨进程未放行却回 confirmed:true")
	}

	expired := seedActiveSession(t, db, 781423)
	seedWaitFrame(t, db, expired, time.Now().Add(-time.Minute), b20Hash)
	code, b = b20Call(t, c.Confirm, http.MethodPost, fmt.Sprint(expired.ID), b20User, b20ReqOK)
	if code != http.StatusOK || !strings.Contains(b.Message, "没有待确认") {
		t.Errorf("已过期的 d7_wait 仍判「在别的进程」：HTTP %d msg=%q", code, b.Message)
	}

	quiet := seedActiveSession(t, db, 781424)
	code, b = b20Call(t, c.Confirm, http.MethodPost, fmt.Sprint(quiet.ID), b20User, b20ReqOK)
	if code != http.StatusOK || !strings.Contains(b.Message, "没有待确认") {
		t.Errorf("无帧会话 → HTTP %d msg=%q", code, b.Message)
	}

	// 会话已收口：闸门随执行结束消失，不得再接受放行（也不许说「在别的进程」——闸门确实没了）
	ended := seedActiveSession(t, db, 781425)
	seedWaitFrame(t, db, ended, time.Now().Add(10*time.Minute), b20Hash)
	if err := db.Model(&model.BrowserSession{}).Where("id = ?", ended.ID).
		Update("status", "completed").Error; err != nil {
		t.Fatalf("收口会话失败：%v", err)
	}
	code, b = b20Call(t, c.Confirm, http.MethodPost, fmt.Sprint(ended.ID), b20User, b20ReqOK)
	if code != http.StatusOK || !strings.Contains(b.Message, "没有待确认") {
		t.Errorf("已收口会话的放行 → HTTP %d msg=%q，want 200「没有待确认的提交点」", code, b.Message)
	}
}

// 4) 读侧详情：pending=false 时不带 gate（前端不该拿到一份空壳），越权读不到闸门。
func TestB20ConfirmGateReadEndpoint(t *testing.T) {
	svc, db := newB20SvcDB(t)
	sess := seedActiveSession(t, db, 781426)
	c := NewSessionController(svc)
	id := fmt.Sprint(sess.ID)

	code, b := b20Call(t, c.ConfirmGate, http.MethodGet, id, b20User, "")
	if code != http.StatusOK {
		t.Fatalf("本人读闸门详情 → HTTP %d（msg=%q）", code, b.Message)
	}
	if b.Data["pending"] != false {
		t.Errorf("无挂起闸门时 pending=%v want false（data=%+v）", b.Data["pending"], b.Data)
	}
	if _, has := b.Data["gate"]; has {
		t.Errorf("pending=false 却带回了 gate 空壳：%+v", b.Data)
	}
	// 越权：别人的会话读不到闸门详情（预览里含评论正文），404 且不透露存在性
	code, b = b20Call(t, c.ConfirmGate, http.MethodGet, id, b20User+1, "")
	if code != http.StatusNotFound {
		t.Errorf("越权读闸门详情 → HTTP %d want 404（msg=%q data=%+v）", code, b.Message, b.Data)
	}
	if _, has := b.Data["gate"]; has {
		t.Errorf("越权响应里带回了闸门详情：%+v", b.Data)
	}
}
