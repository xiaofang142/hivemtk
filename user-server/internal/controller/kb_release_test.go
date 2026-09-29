// kb_release_test.go T-P9-02：变更/发布出口（L5 控制层）的用例。
//
// 本层只主张它自己那四件事 —— 身份、形状、值域、状态码 —— 所以主力用替身而不是真库：
//
//   - **状态码翻译表要 12 条 sentinel 全过一遍**。其中 ErrKBReleaseChangeNotPending
//     要的是"service 读完待办之后、进事务之前有人撤回"这种并发形态，HTTP 这一侧
//     没有任何一条输入能确定性地把它造出来（仓储用例是直接改写库里的状态造的），
//     而控制层要证的只是"拿到这个错回哪个码"。拿真库绕一圈去造它，测到的是仓储，不是这张表。
//   - **查询参数 → KBChangeFilter 的翻译**、subject_key 的拼法（change:/release:）、
//     page→Offset 的换算，判据都是"传下去的那一格是什么"。替身原样回收实参，
//     一句断言直指目标；真库只能靠"结果条数对不对"反推，而条数对不上也可能是过滤条件之外的原因。
//
// 真库那一条（末尾 TestKBReleaseController_RealStack）不重复上面的任何一格，只证一件事：
// 这组路由与**真的** repository + 真的审批服务装在一起时，从提交到发布到留痕的出口仍然通
// —— 替身测不出"装配形状对不上"，也测不出静态段 /gate 有没有被 :product 吞掉。
package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/kbrelease"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"
)

// ---------------------------------------------------------------------------
// 替身
// ---------------------------------------------------------------------------

// kbFakeStore 实现 service 那一扇窄仓储接口（类型不导出，但 Go 允许在包外满足它：
// 只要方法集对上，NewKBReleaseService 的形参类型不需要可见）。
//
// 每条腿的默认值是**报错**而不是零值：忘接线的那一扇必须让用例红在读到 nil 之前，
// 否则"控制层回 200 + 空数据"会替实现者把漏掉的调用点圆过去。
type kbFakeStore struct {
	available bool
	calls     []string

	lastFilter   repository.KBChangeFilter
	lastSubject  string
	lastLimit    int
	lastIntent   repository.PublishIntent
	lastProduct  string
	lastActor    string
	lastGoverned bool
	lastInsert   *model.KBChangeRequest
	lastGetID    string

	getReleaseFn   func(product string) (*model.KBRelease, error)
	listReleasesFn func() ([]model.KBRelease, error)
	setGovernedFn  func(product string, governed bool, actor string) (*model.KBRelease, error)
	insertChangeFn func(ch *model.KBChangeRequest) error
	getChangeFn    func(id string) (*model.KBChangeRequest, error)
	listChangesFn  func(f repository.KBChangeFilter) ([]model.KBChangeRequest, int64, error)
	pendingFn      func(product string) ([]model.KBChangeRequest, error)
	withdrawFn     func(id string) (bool, error)
	recordAuditFn  func(log model.KBChangeAuditLog) error
	listAuditFn    func(subject string, limit int) ([]model.KBChangeAuditLog, error)
	publishFn      func(in repository.PublishIntent) (*repository.PublishResult, error)
	rollbackFn     func(product, actor string) (*repository.RollbackResult, error)
	restoreFn      func(product, actor string) (*repository.RollbackResult, error)
	countInForceFn func(product string) (int64, int64, error)
}

// newKBFakeStore 可用（Available()=true）且全部未接线的替身。
func newKBFakeStore() *kbFakeStore { return &kbFakeStore{available: true} }

func (f *kbFakeStore) hit(name string) { f.calls = append(f.calls, name) }

// called 某一扇有没有被调过。静态段路由的归属判据靠它：/gate 若被 :product 吞掉，
// GetRelease 就会出现在调用名单里。
func (f *kbFakeStore) called(name string) bool {
	for _, c := range f.calls {
		if c == name {
			return true
		}
	}
	return false
}

func (f *kbFakeStore) Available() bool { return f.available }

func errNotWired(name string) error { return fmt.Errorf("kbFakeStore.%s 未接线", name) }

func (f *kbFakeStore) GetRelease(_ context.Context, product string) (*model.KBRelease, error) {
	f.hit("GetRelease")
	f.lastProduct = product
	if f.getReleaseFn != nil {
		return f.getReleaseFn(product)
	}
	return nil, errNotWired("GetRelease")
}

func (f *kbFakeStore) ListReleases(_ context.Context) ([]model.KBRelease, error) {
	f.hit("ListReleases")
	if f.listReleasesFn != nil {
		return f.listReleasesFn()
	}
	return nil, errNotWired("ListReleases")
}

func (f *kbFakeStore) SetGoverned(_ context.Context, product string, governed bool, actor string) (*model.KBRelease, error) {
	f.hit("SetGoverned")
	f.lastProduct, f.lastGoverned, f.lastActor = product, governed, actor
	if f.setGovernedFn != nil {
		return f.setGovernedFn(product, governed, actor)
	}
	return nil, errNotWired("SetGoverned")
}

func (f *kbFakeStore) InsertChange(_ context.Context, ch *model.KBChangeRequest) error {
	f.hit("InsertChange")
	f.lastInsert = ch
	if f.insertChangeFn != nil {
		return f.insertChangeFn(ch)
	}
	return nil
}

func (f *kbFakeStore) GetChange(_ context.Context, id string) (*model.KBChangeRequest, error) {
	f.hit("GetChange")
	f.lastGetID = id
	if f.getChangeFn != nil {
		return f.getChangeFn(id)
	}
	return nil, errNotWired("GetChange")
}

func (f *kbFakeStore) ListChanges(_ context.Context, fl repository.KBChangeFilter) ([]model.KBChangeRequest, int64, error) {
	f.hit("ListChanges")
	f.lastFilter = fl
	if f.listChangesFn != nil {
		return f.listChangesFn(fl)
	}
	return nil, 0, errNotWired("ListChanges")
}

func (f *kbFakeStore) PendingChanges(_ context.Context, product string) ([]model.KBChangeRequest, error) {
	f.hit("PendingChanges")
	f.lastProduct = product
	if f.pendingFn != nil {
		return f.pendingFn(product)
	}
	return nil, errNotWired("PendingChanges")
}

func (f *kbFakeStore) WithdrawChange(_ context.Context, id string) (bool, error) {
	f.hit("WithdrawChange")
	f.lastGetID = id
	if f.withdrawFn != nil {
		return f.withdrawFn(id)
	}
	return false, errNotWired("WithdrawChange")
}

func (f *kbFakeStore) RecordAudit(_ context.Context, log model.KBChangeAuditLog) error {
	f.hit("RecordAudit")
	if f.recordAuditFn != nil {
		return f.recordAuditFn(log)
	}
	return nil
}

func (f *kbFakeStore) ListAuditBySubject(_ context.Context, subject string, limit int) ([]model.KBChangeAuditLog, error) {
	f.hit("ListAuditBySubject")
	f.lastSubject, f.lastLimit = subject, limit
	if f.listAuditFn != nil {
		return f.listAuditFn(subject, limit)
	}
	return nil, errNotWired("ListAuditBySubject")
}

func (f *kbFakeStore) Publish(_ context.Context, in repository.PublishIntent) (*repository.PublishResult, error) {
	f.hit("Publish")
	f.lastIntent = in
	if f.publishFn != nil {
		return f.publishFn(in)
	}
	return nil, errNotWired("Publish")
}

func (f *kbFakeStore) Rollback(_ context.Context, product, actor string) (*repository.RollbackResult, error) {
	f.hit("Rollback")
	f.lastProduct, f.lastActor = product, actor
	if f.rollbackFn != nil {
		return f.rollbackFn(product, actor)
	}
	return nil, errNotWired("Rollback")
}

func (f *kbFakeStore) Restore(_ context.Context, product, actor string) (*repository.RollbackResult, error) {
	f.hit("Restore")
	f.lastProduct, f.lastActor = product, actor
	if f.restoreFn != nil {
		return f.restoreFn(product, actor)
	}
	return nil, errNotWired("Restore")
}

func (f *kbFakeStore) CountChunksInForce(_ context.Context, product string) (int64, int64, error) {
	f.hit("CountChunksInForce")
	f.lastProduct = product
	if f.countInForceFn != nil {
		return f.countInForceFn(product)
	}
	return 0, 0, errNotWired("CountChunksInForce")
}

// kbFakeApproval 审批网关替身。Submit 回一行"已入队、还在等"的审批（与真实默认一致），
// Get 按 id 回调用点塞进去的那一张。
type kbFakeApproval struct {
	submitFn func(in service.ApprovalSubmitInput) (*model.ApprovalRequest, bool, error)
	getFn    func(id string) (*model.ApprovalRequest, error)

	lastSubmit service.ApprovalSubmitInput
	submitN    int
}

func (a *kbFakeApproval) Submit(_ context.Context, in service.ApprovalSubmitInput) (*model.ApprovalRequest, bool, error) {
	a.lastSubmit, a.submitN = in, a.submitN+1
	if a.submitFn != nil {
		return a.submitFn(in)
	}
	return &model.ApprovalRequest{
		ID: "apr_fake_" + in.SubjectID, SubjectType: in.SubjectType,
		SubjectID: in.SubjectID, PolicyKey: in.PolicyKey,
		Status: model.ApprovalStatusPending,
	}, true, nil
}

func (a *kbFakeApproval) Get(_ context.Context, id string) (*model.ApprovalRequest, error) {
	if a.getFn != nil {
		return a.getFn(id)
	}
	return nil, nil
}

// kbApproved 回一张"三列都是这条变更的、状态为已批准"的审批行。
func kbApproved(changeID string) *model.ApprovalRequest {
	return &model.ApprovalRequest{
		ID: "apr_" + changeID, SubjectType: service.KBChangeApprovalSubjectType,
		SubjectID: changeID, PolicyKey: service.KBChangeApprovalPolicyKey,
		Status: model.ApprovalStatusApproved,
	}
}

// ---------------------------------------------------------------------------
// 夹具：引擎
// ---------------------------------------------------------------------------

// kbCtrlGoverned 回一个"已进发布制"的发布指针读取函数，桶号由调用方给。
//
// 发布这条链先问治理、再问内容（见 service.PublishPending 里那段注释），所以每个走
// 发布的用例都要显式装这一行；把它做成替身的默认值，"发布不查治理开关"这个变异
// 就没有任何用例能发现。bucket>0 = 导入链路已经攒好待发布桶。
func kbCtrlGoverned(bucket int) func(string) (*model.KBRelease, error) {
	return func(string) (*model.KBRelease, error) {
		rel := &model.KBRelease{ID: 1, ProductID: "p1", Governed: true,
			DraftVersion: bucket, AllocatedVersion: bucket}
		if bucket > 0 {
			// 桶号是在高水位之后新取的一格，于是它必然正好高于在服版本一格；桶为 0 时
			// 在服版本也是 0（还没发过版），别让夹具凭空造出一个负号。
			rel.EffectiveVersion = bucket - 1
		}
		return rel, nil
	}
}

// kbCtrlRouter 按 router/kb_release_routes.go 的形状挂载（/api 组内、写入口带身份）。
//
// operator 传 nil 表示"这一趟没有登录态"：auth 中间件压根不写 user_id，
// 让 401 那几条腿测的是出口本身，而不是"空身份被下游救回来"。
func kbCtrlRouter(t *testing.T, store *kbFakeStore, approvals *kbFakeApproval, operator any) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	var svc *service.KBReleaseService
	if store != nil {
		if approvals == nil {
			approvals = &kbFakeApproval{}
		}
		svc = service.NewKBReleaseService(store, approvals, nil)
	}
	engine := gin.New()
	auth := engine.Group("/api")
	if operator != nil {
		auth.Use(func(ctx *gin.Context) {
			ctx.Set("user_id", operator)
			ctx.Next()
		})
	}
	NewKBReleaseController(svc).RegisterRoutes(auth)
	return engine
}

// kbCtrlReady 一条可用的变更/发布出口 + 默认全部走通的替身（用例只覆盖自己关心的那一扇）。
func kbCtrlReady(t *testing.T, store *kbFakeStore) (*gin.Engine, *kbFakeApproval) {
	t.Helper()
	gw := &kbFakeApproval{}
	return kbCtrlRouter(t, store, gw, uint(7)), gw
}

func kbCtrlDo(t *testing.T, engine *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *bytes.Reader
	if body == "" {
		rdr = bytes.NewReader(nil)
	} else {
		rdr = bytes.NewReader([]byte(body))
	}
	req, err := http.NewRequest(method, path, rdr)
	if err != nil {
		t.Fatalf("构造请求失败: %v", err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	return w
}

// kbCtrlBody 解出统一响应契约。解不动就 Fatal —— 半解开的 body 会让后面的断言
// 在 nil map 上取字段，把一个真缺陷读成"键不存在"。
func kbCtrlBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("响应不是合法 JSON（%v）：%s", err, w.Body.String())
	}
	return out
}

func kbCtrlMsg(body map[string]any) string {
	s, _ := body["message"].(string)
	return s
}

// kbCtrlDataOf 取 data 格并要求它是对象。空集合与缺失要能分开，所以调用方拿到的是
// map 而不是 any —— nil 在这里直接 Fatal，而不是让断言去猜。
func kbCtrlDataOf(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	data, ok := body["data"].(map[string]any)
	if !ok {
		t.Fatalf("data 不是对象：%v", body["data"])
	}
	return data
}

// kbCtrlAuditActions 把留痕出口读成"动作集合"，并顺手对账 total 与行数。
//
// 集合而不是切片：断言要的是"这条链上发生过哪几件事"，而重复的同名动作（比如发布
// 被记了两次）该由条数那一侧发现，两者一起看才是完整的账。
func kbCtrlAuditActions(t *testing.T, data map[string]any) map[string]bool {
	t.Helper()
	list, ok := data["list"].([]any)
	if !ok {
		t.Fatalf("留痕出口没带回数组：%#v", data["list"])
	}
	if want, isNum := data["total"].(float64); isNum && int(want) != len(list) {
		t.Errorf("total=%v 与行数 %d 不符", data["total"], len(list))
	}
	actions := map[string]bool{}
	for _, item := range list {
		row, _ := item.(map[string]any)
		action, _ := row["action"].(string)
		if action == "" {
			t.Errorf("留痕行没有动作：%v", row)
			continue
		}
		actions[action] = true
	}
	return actions
}

// ---------------------------------------------------------------------------
// 503：底座不在
// ---------------------------------------------------------------------------

// TestKBReleaseController_UnavailableEveryExit 未装配时每个出口的答复。
//
// 两条都要测：store.Available()=false（半装配：句柄在但不可用）与 svc 本身就是 nil
// （路由照挂、全局没登记）。两者的期望完全一致，且都必须**不 panic** ——
// 后者是最容易漏的一格，因为 NewKBReleaseController(nil) 在装配失败时是真会发生的形状。
func TestKBReleaseController_UnavailableEveryExit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// 每个出口一个 method/path/body；gate 单独在下面反向测（它**不该**回 503）。
	exits := []struct{ method, path, body string }{
		{"GET", "/api/kb-changes", ""},
		{"GET", "/api/kb-changes/kbc_1", ""},
		{"GET", "/api/kb-changes/kbc_1/audit", ""},
		{"POST", "/api/kb-changes", `{"product_id":"p1","op":"add","content":"c","reason":"r","document_id":1}`},
		{"POST", "/api/kb-changes/kbc_1/withdraw", ""},
		{"GET", "/api/kb-releases", ""},
		{"GET", "/api/kb-releases/p1", ""},
		{"GET", "/api/kb-releases/p1/stats", ""},
		{"GET", "/api/kb-releases/p1/audit", ""},
		{"POST", "/api/kb-releases/p1/publish", ""},
		{"POST", "/api/kb-releases/p1/rollback", ""},
		{"POST", "/api/kb-releases/p1/restore", ""},
		{"PATCH", "/api/kb-releases/p1/governed", `{"governed":true}`},
	}
	for _, shape := range []struct {
		name  string
		store *kbFakeStore
	}{{"底座不可用", &kbFakeStore{}}, {"服务未装配", nil}} {
		engine := kbCtrlRouter(t, shape.store, nil, uint(7))
		for _, e := range exits {
			w := kbCtrlDo(t, engine, e.method, e.path, e.body)
			if w.Code != http.StatusServiceUnavailable {
				t.Errorf("%s：%s %s 期望 503，实得 %d（body=%s）",
					shape.name, e.method, e.path, w.Code, w.Body.String())
			}
			// 503 不许带数据字段：空列表/零计数都是一句业务结论，而此刻的事实是"一次都没读到"。
			if v, ok := kbCtrlBody(t, w)["data"]; ok && v != nil {
				t.Errorf("%s：%s %s 的 503 带了 data=%v", shape.name, e.method, e.path, v)
			}
		}
	}
}

// TestKBReleaseController_GateAnswersWithoutStore /gate 是唯一的例外：档位是进程事实（env），
// 与有没有 DB 句柄无关。底座没装配时运维最需要知道的恰恰是"闸门开到了哪一档"。
func TestKBReleaseController_GateAnswersWithoutStore(t *testing.T) {
	t.Setenv(kbrelease.FlagEnv, "on")
	for _, store := range []*kbFakeStore{nil, {}} {
		engine := kbCtrlRouter(t, store, nil, nil)
		w := kbCtrlDo(t, engine, "GET", "/api/kb-releases/gate", "")
		if w.Code != http.StatusOK {
			t.Fatalf("svc=%v 时 /gate 期望 200，实得 %d：%s", store, w.Code, w.Body.String())
		}
		data := kbCtrlDataOf(t, kbCtrlBody(t, w))
		if data["mode"] != "on" || data["blocks_read"] != true {
			t.Errorf("底座不在时档位仍该照实回显，实得 %v", data)
		}
		if data["governed_products"] != nil {
			t.Errorf("底座不在时受治理名单该是 null（没读到），实得 %v", data["governed_products"])
		}
		if note, _ := data["note"].(string); !strings.Contains(note, "未装配") {
			t.Errorf("少了\"读不出名单\"的出声：%v", data)
		}
	}
}

// TestKBReleaseController_GateModes 档位回显的四格：mode / blocks_read / stamps_write / 名单。
//
// true 那一档单独配一条：布尔式真值只到 shadow（一把能改生产召回集的旗子不该因为
// 有人按习惯写了 =true 就直接拿到"少返几条"的能力），于是 blocks_read=false 而
// stamps_write=true —— 这两个布尔分开断言才测得到"布尔真值没有直通 on"。
func TestKBReleaseController_GateModes(t *testing.T) {
	cases := []struct {
		env             string
		wantMode        string
		wantBlocks      bool
		wantStampsWrite bool
	}{
		{"", "off", false, false},
		{"off", "off", false, false},
		{"shadow", "shadow", false, true},
		{"on", "on", true, true},
		{"true", "shadow", false, true},
		{"enforce", "on", true, true},
		{"nonsense", "off", false, false},
	}
	for _, c := range cases {
		t.Setenv(kbrelease.FlagEnv, c.env)
		store := newKBFakeStore()
		store.listReleasesFn = func() ([]model.KBRelease, error) {
			return []model.KBRelease{{ProductID: "p_gov", Governed: true}}, nil
		}
		engine, _ := kbCtrlReady(t, store)
		w := kbCtrlDo(t, engine, "GET", "/api/kb-releases/gate", "")
		data := kbCtrlDataOf(t, kbCtrlBody(t, w))
		if data["flag_env"] != kbrelease.FlagEnv {
			t.Errorf("env=%q：flag_env 回显该是 %q，实得 %v", c.env, kbrelease.FlagEnv, data["flag_env"])
		}
		if data["mode"] != c.wantMode || data["blocks_read"] != c.wantBlocks ||
			data["stamps_write"] != c.wantStampsWrite {
			t.Errorf("env=%q：期望 (%s, blocks=%t, stamps=%t)，实得 %v",
				c.env, c.wantMode, c.wantBlocks, c.wantStampsWrite, data)
		}
		if data["governed_count"] != float64(1) {
			t.Errorf("env=%q：名单条数该是 1，实得 %v", c.env, data["governed_count"])
		}
		// /gate 是静态段：它一旦落到 /:product 上，GetRelease 会被以 "gate" 为库号调用。
		if store.called("GetRelease") {
			t.Errorf("env=%q：/gate 被 /:product 抢走了（GetRelease 收到 product=%q）", c.env, store.lastProduct)
		}
	}
}

// TestKBReleaseController_GateReadFailureStillAnswers 名单读不出来时 /gate 仍回 200 + 出声。
//
// 不把它压成 500：档位那三项是本次请求唯一真正要的答案，它没有失败。
func TestKBReleaseController_GateReadFailureStillAnswers(t *testing.T) {
	store := newKBFakeStore()
	store.listReleasesFn = func() ([]model.KBRelease, error) { return nil, errors.New("连接断了") }
	engine, _ := kbCtrlReady(t, store)
	w := kbCtrlDo(t, engine, "GET", "/api/kb-releases/gate", "")
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实得 %d", w.Code)
	}
	data := kbCtrlDataOf(t, kbCtrlBody(t, w))
	if data["governed_products"] != nil {
		t.Errorf("读取失败时名单该是 null，实得 %v", data["governed_products"])
	}
	if note, _ := data["governed_note"].(string); !strings.Contains(note, "读取失败") {
		t.Errorf("少了读取失败的出声：%v", data)
	}
}

// ---------------------------------------------------------------------------
// 401：身份
// ---------------------------------------------------------------------------

// TestKBReleaseController_WriteRequiresOperator 六个写入口都要落到具体的人。
//
// 表里连 "0 / 负数 / 全空白字符串" 一起测：这几格在 contextOperatorID 里判"没有身份"，
// 而 service 侧的 actor 非空校验会把它们救回来 —— 于是同一件事有两种报法。
// 出口只认 401，因为空串写进 changed_by 之后，审计里那条"谁按的"永远查不回来了。
func TestKBReleaseController_WriteRequiresOperator(t *testing.T) {
	writes := []struct{ method, path, body string }{
		{"POST", "/api/kb-changes", `{"product_id":"p1","op":"add","content":"c","reason":"r","document_id":1}`},
		{"POST", "/api/kb-changes/kbc_1/withdraw", ""},
		{"POST", "/api/kb-releases/p1/publish", ""},
		{"POST", "/api/kb-releases/p1/rollback", ""},
		{"POST", "/api/kb-releases/p1/restore", ""},
		{"PATCH", "/api/kb-releases/p1/governed", `{"governed":true}`},
	}
	// 五种"看上去有、其实没有"的身份形状。nil 那一格连中间件一起撤掉（会话里压根没有
	// user_id 这个键），其余四格走同一条中间件 —— 两类的期望完全一致。
	for _, nope := range []struct {
		name  string
		value any
	}{
		{"没有 user_id", nil},
		{"uint(0)", uint(0)},
		{"int(-3)", int(-3)},
		{"float64(0.5)", float64(0.5)},
		{"空白字符串", "   "},
	} {
		for _, e := range writes {
			store := newKBFakeStore()
			engine := kbCtrlRouter(t, store, nil, nope.value)
			w := kbCtrlDo(t, engine, e.method, e.path, e.body)
			if w.Code != http.StatusUnauthorized {
				t.Errorf("%s：%s %s 期望 401，实得 %d（body=%s）",
					nope.name, e.method, e.path, w.Code, w.Body.String())
			}
			if len(store.calls) != 0 {
				t.Errorf("%s：%s %s 在回 401 之前已经动了底座（calls=%v）", nope.name, e.method, e.path, store.calls)
			}
		}
	}
}

// TestKBReleaseController_OperatorTypesReachActor 三种合法身份形状都要翻成同一个操作者串。
//
// 断言打在**传到仓储那一格**上（lastActor），不是打在状态码上：401 只说明"没放行"，
// 放行之后把 "12" 写成 "12 " 或者把 float64 写成 "1.2e+01"，状态码一样、留痕里的
// changed_by 却查不到同一个人。
func TestKBReleaseController_OperatorTypesReachActor(t *testing.T) {
	cases := []struct {
		value any
		want  string
	}{
		{uint(12), "12"},
		{int(12), "12"},
		{float64(12), "12"},
		{"u_ops", "u_ops"},
	}
	for _, c := range cases {
		store := newKBFakeStore()
		store.setGovernedFn = func(string, bool, string) (*model.KBRelease, error) {
			return &model.KBRelease{ProductID: "p1", Governed: true}, nil
		}
		engine := kbCtrlRouter(t, store, nil, c.value)
		w := kbCtrlDo(t, engine, "PATCH", "/api/kb-releases/p1/governed", `{"governed":true}`)
		if w.Code != http.StatusOK {
			t.Fatalf("身份 %T(%v) 期望放行，实得 %d：%s", c.value, c.value, w.Code, w.Body.String())
		}
		if store.lastActor != c.want {
			t.Errorf("身份 %T(%v)：actor 该是 %q，实得 %q", c.value, c.value, c.want, store.lastActor)
		}
	}
}

// ---------------------------------------------------------------------------
// 400：形状与值域
// ---------------------------------------------------------------------------

// TestKBReleaseController_ListQueryTranslation 查询参数 → KBChangeFilter 的逐格翻译。
//
// 两条容易写错的形状各占一格：
//   - op 控制器会转小写（前端传 ADD 也算），status **不会**（PENDING 直接 400）——
//     这个不对称是刻意的，测出来才能防止下一个人"顺手统一成都转小写"；
//   - page 从 1 起，仓储吃的是 Limit/Offset：page=3/size=20 必须是 Offset=40，
//     写成 Offset=3 的话第二页起每一页都在重放第一页。
func TestKBReleaseController_ListQueryTranslation(t *testing.T) {
	cases := []struct {
		name      string
		query     string
		want      repository.KBChangeFilter
		wantOK    bool
		wantInMsg string
	}{
		{
			name: "全默认",
			want: repository.KBChangeFilter{Limit: 50, Offset: 0}, wantOK: true,
		},
		{
			name:   "按库+状态+动作",
			query:  "?product_id=p9&status=applied&op=revise",
			want:   repository.KBChangeFilter{ProductID: "p9", Status: "applied", Op: "revise", Limit: 50},
			wantOK: true,
		},
		{
			name:   "op 大小写折叠、product_id 去空白",
			query:  "?product_id=%20p9%20&op=ADD",
			want:   repository.KBChangeFilter{ProductID: "p9", Op: "add", Limit: 50},
			wantOK: true,
		},
		{
			name:   "翻页换算",
			query:  "?page=3&page_size=20",
			want:   repository.KBChangeFilter{Limit: 20, Offset: 40},
			wantOK: true,
		},
		{
			name:   "只有 page_size",
			query:  "?page_size=7",
			want:   repository.KBChangeFilter{Limit: 7, Offset: 0},
			wantOK: true,
		},
		{name: "page=0", query: "?page=0", wantInMsg: "page 必须是从 1 起"},
		{name: "page 非数字", query: "?page=abc", wantInMsg: "page 必须是从 1 起"},
		{name: "page_size 负数", query: "?page_size=-1", wantInMsg: "page_size 必须是正整数"},
		{name: "page_size=0", query: "?page_size=0", wantInMsg: "page_size 必须是正整数"},
		{name: "status 越界", query: "?status=rejected", wantInMsg: "status 必须是 pending/applied/withdrawn 之一"},
		{name: "status 大写", query: "?status=PENDING", wantInMsg: "status 必须是"},
		{name: "op 越界", query: "?op=delete", wantInMsg: "op 必须是 add/revise/retire 之一"},
		{name: "product_id 超长", query: "?product_id=" + strings.Repeat("p", 65), wantInMsg: "product_id 过长"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			store := newKBFakeStore()
			var got repository.KBChangeFilter
			store.listChangesFn = func(f repository.KBChangeFilter) ([]model.KBChangeRequest, int64, error) {
				got = f
				return nil, 0, nil
			}
			engine, _ := kbCtrlReady(t, store)
			w := kbCtrlDo(t, engine, "GET", "/api/kb-changes"+c.query, "")
			if !c.wantOK {
				if w.Code != http.StatusBadRequest {
					t.Fatalf("期望 400，实得 %d：%s", w.Code, w.Body.String())
				}
				if msg := kbCtrlMsg(kbCtrlBody(t, w)); !strings.Contains(msg, c.wantInMsg) {
					t.Errorf("报错文案该含 %q，实得 %q", c.wantInMsg, msg)
				}
				if store.called("ListChanges") {
					t.Errorf("值域闸在查询前就该拦下，不该把坏值透给仓储（calls=%v）", store.calls)
				}
				return
			}
			if w.Code != http.StatusOK {
				t.Fatalf("期望 200，实得 %d：%s", w.Code, w.Body.String())
			}
			if got != c.want {
				t.Errorf("传下去的过滤条件是 %+v，期望 %+v", got, c.want)
			}
			// 空列表是一句业务结论：200 + list 为空、total 为 0。
			data := kbCtrlDataOf(t, kbCtrlBody(t, w))
			if list, ok := data["list"].([]any); !ok || len(list) != 0 {
				t.Errorf("data.list 该是空数组，实得 %v", data["list"])
			}
			if data["total"] != float64(0) {
				t.Errorf("data.total 该是 0，实得 %v", data["total"])
			}
		})
	}
}

// TestKBReleaseController_ListTotalPassthrough total 用仓储给的数，不用本页条数。
//
// 前端算分页要的是全量条数；把 len(list) 当 total 的话，第二页之后总数会缩成一页的长度。
func TestKBReleaseController_ListTotalPassthrough(t *testing.T) {
	store := newKBFakeStore()
	store.listChangesFn = func(repository.KBChangeFilter) ([]model.KBChangeRequest, int64, error) {
		return []model.KBChangeRequest{{ID: "kbc_a"}, {ID: "kbc_b"}}, 137, nil
	}
	engine, _ := kbCtrlReady(t, store)
	w := kbCtrlDo(t, engine, "GET", "/api/kb-changes?page=2&page_size=2", "")
	data := kbCtrlDataOf(t, kbCtrlBody(t, w))
	if data["total"] != float64(137) {
		t.Errorf("total 该透传 137，实得 %v", data["total"])
	}
	if list, _ := data["list"].([]any); len(list) != 2 {
		t.Errorf("list 该是 2 条，实得 %v", data["list"])
	}
}

// TestKBReleaseController_ParamGates 路径参数上的三闸：空、超长（按 rune 计）、以及
// 查询串里的 limit。超长这条不是审美：列宽 64，超长的一次读取会让 PG 在 varchar(64)
// 隐式转换上报 "value too long" ⇒ 同一个原因在这一格报 500、在别处报 400。
func TestKBReleaseController_ParamGates(t *testing.T) {
	long := strings.Repeat("x", 65)
	cases := []struct {
		name     string
		method   string
		path     string
		body     string
		wantCode int
		wantMsg  string
	}{
		{"变更号超长 GET", "GET", "/api/kb-changes/" + long, "", http.StatusBadRequest, "变更 id 过长"},
		{"变更号超长 withdraw", "POST", "/api/kb-changes/" + long + "/withdraw", "", http.StatusBadRequest, "变更 id 过长"},
		{"变更号超长 audit", "GET", "/api/kb-changes/" + long + "/audit", "", http.StatusBadRequest, "变更 id 过长"},
		{"库号超长 get", "GET", "/api/kb-releases/" + long, "", http.StatusBadRequest, "product_id 过长"},
		{"库号超长 stats", "GET", "/api/kb-releases/" + long + "/stats", "", http.StatusBadRequest, "product_id 过长"},
		{"库号超长 audit", "GET", "/api/kb-releases/" + long + "/audit", "", http.StatusBadRequest, "product_id 过长"},
		{"库号超长 publish", "POST", "/api/kb-releases/" + long + "/publish", "", http.StatusBadRequest, "product_id 过长"},
		{"库号超长 rollback", "POST", "/api/kb-releases/" + long + "/rollback", "", http.StatusBadRequest, "product_id 过长"},
		{"库号超长 restore", "POST", "/api/kb-releases/" + long + "/restore", "", http.StatusBadRequest, "product_id 过长"},
		{"库号超长 governed", "PATCH", "/api/kb-releases/" + long + "/governed", `{"governed":true}`, http.StatusBadRequest, "product_id 过长"},
		{"留痕 limit=0", "GET", "/api/kb-changes/kbc_1/audit?limit=0", "", http.StatusBadRequest, "limit 必须是正整数"},
		{"留痕 limit 非数字", "GET", "/api/kb-releases/p1/audit?limit=xx", "", http.StatusBadRequest, "limit 必须是正整数"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			store := newKBFakeStore()
			engine, _ := kbCtrlReady(t, store)
			w := kbCtrlDo(t, engine, c.method, c.path, c.body)
			if w.Code != c.wantCode {
				t.Fatalf("期望 %d，实得 %d：%s", c.wantCode, w.Code, w.Body.String())
			}
			if msg := kbCtrlMsg(kbCtrlBody(t, w)); !strings.Contains(msg, c.wantMsg) {
				t.Errorf("报错文案该含 %q，实得 %q", c.wantMsg, msg)
			}
			if len(store.calls) != 0 {
				t.Errorf("坏参数不该打到底座（calls=%v）", store.calls)
			}
		})
	}
}

// TestKBReleaseController_ProductParamCountsRunes 路径上的长度闸数的是 rune 而不是字节。
//
// 64 个汉字（192 字节）必须放行：列宽限制的是字符数，按字节挡会把中文库号拦成 400；
// 65 个汉字必须拦下。两格放一起测才分得开"数错了单位"与"数得对"。
func TestKBReleaseController_ProductParamCountsRunes(t *testing.T) {
	store := newKBFakeStore()
	store.getReleaseFn = func(product string) (*model.KBRelease, error) {
		return &model.KBRelease{ProductID: product, Governed: true}, nil
	}
	engine, _ := kbCtrlReady(t, store)
	for _, c := range []struct {
		runes    int
		wantCode int
	}{{64, http.StatusOK}, {65, http.StatusBadRequest}} {
		path := "/api/kb-releases/" + strings.Repeat("知", c.runes)
		w := kbCtrlDo(t, engine, "GET", path, "")
		if w.Code != c.wantCode {
			t.Errorf("%d 个汉字（%d 字节）期望 %d，实得 %d：%s",
				c.runes, len([]byte(strings.Repeat("知", c.runes))), c.wantCode, w.Code, w.Body.String())
		}
	}
}

// TestKBReleaseController_BadBodies 请求体形状：坏 JSON、缺 governed、governed 类型不对。
//
// governed 那一格最要命：body 是指针，漏字段与显式 false 在零值上长得一样，
// 而这一处的缺省方向如果是"当成启用"，一个打漏字段的调用就会把某个库推进发布制。
func TestKBReleaseController_BadBodies(t *testing.T) {
	cases := []struct {
		name   string
		method string
		path   string
		body   string
		want   string
	}{
		{"提交：坏 JSON", "POST", "/api/kb-changes", `{"product_id":`, "请求体不是合法 JSON"},
		{"提交：类型不对", "POST", "/api/kb-changes", `{"product_id":"p1","document_id":"不是数","op":"add"}`, "请求体不是合法 JSON"},
		{"启停：坏 JSON", "PATCH", "/api/kb-releases/p1/governed", `{`, "请求体不是合法 JSON"},
		{"启停：缺 governed", "PATCH", "/api/kb-releases/p1/governed", `{}`, "请求体缺少 governed 布尔字段"},
		{"启停：governed 是字符串", "PATCH", "/api/kb-releases/p1/governed", `{"governed":"yes"}`, "请求体不是合法 JSON"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			store := newKBFakeStore()
			store.setGovernedFn = func(string, bool, string) (*model.KBRelease, error) {
				t.Error("坏请求体不该走到仓储")
				return nil, nil
			}
			engine, _ := kbCtrlReady(t, store)
			w := kbCtrlDo(t, engine, c.method, c.path, c.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("期望 400，实得 %d：%s", w.Code, w.Body.String())
			}
			if msg := kbCtrlMsg(kbCtrlBody(t, w)); !strings.Contains(msg, c.want) {
				t.Errorf("报错文案该含 %q，实得 %q", c.want, msg)
			}
		})
	}
}

// TestKBReleaseController_SubmitInputJudgments 提交侧的入参判据（服务层报错、本层翻成 400）。
//
// 只测映射，不重复服务层的判据表：这五格共同证明 ErrKBChangeInputInvalid 这一条
// sentinel 在出口上是 400，而不是散落成 500。
func TestKBReleaseController_SubmitInputJudgments(t *testing.T) {
	cases := []struct{ name, body, wantInMsg string }{
		{"缺 product_id", `{"op":"add","content":"c","reason":"r","document_id":1}`, "product_id 为空"},
		{"缺 reason", `{"product_id":"p1","op":"add","content":"c","document_id":1}`, "reason 为空"},
		{"op 越界", `{"product_id":"p1","op":"zap","content":"c","reason":"r","document_id":1}`, "op=\"zap\" 不在 add/revise/retire 之内"},
		{"add 缺 document_id", `{"product_id":"p1","op":"add","content":"c","reason":"r"}`, "add 必须指定 document_id"},
		{"add 不该带 target", `{"product_id":"p1","op":"add","content":"c","reason":"r","document_id":1,"target_chunk_id":9}`, "add 不该带 target_chunk_id"},
		{"retire 不该带 content", `{"product_id":"p1","op":"retire","content":"c","reason":"r","target_chunk_id":9}`, "retire 不该带 content"},
		{"content 超长", `{"product_id":"p1","op":"add","reason":"r","document_id":1,"content":"` +
			strings.Repeat("汉", 16385) + `"}`, "超单条分段上限"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			store := newKBFakeStore()
			engine, _ := kbCtrlReady(t, store)
			w := kbCtrlDo(t, engine, "POST", "/api/kb-changes", c.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("期望 400，实得 %d：%s", w.Code, w.Body.String())
			}
			if msg := kbCtrlMsg(kbCtrlBody(t, w)); !strings.Contains(msg, c.wantInMsg) {
				t.Errorf("报错文案该含 %q，实得 %q", c.wantInMsg, msg)
			}
			if store.called("InsertChange") {
				t.Error("入参不合法时不该落变更行")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 404 / 409：状态码翻译表
// ---------------------------------------------------------------------------

// TestKBReleaseController_ReplyErrorTable replyError 的每一条腿各配一个错误、各要一个码。
//
// 直接调 replyError（而不是想办法从 HTTP 造出每个错）的理由写在文件头：其中三条
// （ChangeNotPending / RecallBan / GovernedDirectWrite）在出口侧没有确定性输入。
// 每格都配**包过一层**的错误（%w），因为 errors.Is 与 == 的差别只有包装之后才测得出来 ——
// 服务层给仓储的错加了上下文（"变更 kbc_x 的审批…"），若本层改成 ==，
// 十二条腿会全部塌进 default 的 500，而端到端用例只覆盖得了其中三条。
func TestKBReleaseController_ReplyErrorTable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	table := []struct {
		name string
		err  error
		want int
	}{
		{"底座未装配", service.ErrKBReleaseUnavailable, http.StatusServiceUnavailable},
		{"审批没给结论", service.ErrKBReleaseApprovalMissing, http.StatusServiceUnavailable},
		{"入参不合法", service.ErrKBChangeInputInvalid, http.StatusBadRequest},
		{"查无此条", service.ErrKBChangeNotFound, http.StatusNotFound},
		{"撤不掉", service.ErrKBChangeNotWithdrawable, http.StatusConflict},
		{"库未进发布制", service.ErrKBReleaseNotGoverned, http.StatusConflict},
		{"没有上一版可回", service.ErrKBReleaseRollbackUnavailable, http.StatusConflict},
		{"没有可放回的版本", service.ErrKBReleaseRestoreUnavailable, http.StatusConflict},
		{"发布禁令", service.ErrKBReleaseRecallBan, http.StatusConflict},
		{"没有可发布的东西", service.ErrKBReleaseNothingToPublish, http.StatusConflict},
		{"目标分段对不上", service.ErrKBReleaseTargetConflict, http.StatusConflict},
		{"变更已不在待处理态", service.ErrKBReleaseChangeNotPending, http.StatusConflict},
		{"已进发布制不许直写", kbrelease.ErrGovernedDirectWrite, http.StatusConflict},
		{"未登记的内部错误", errors.New("连接断了"), http.StatusInternalServerError},
	}
	for _, c := range table {
		for _, shape := range []string{"裸错误", "包一层"} {
			err := c.err
			if shape == "包一层" {
				err = fmt.Errorf("变更 kbc_1 的处理失败: %w", c.err)
			}
			name := c.name + "/" + shape
			t.Run(name, func(t *testing.T) {
				recorder := httptest.NewRecorder()
				ctx, _ := gin.CreateTestContext(recorder)
				ctx.Request, _ = http.NewRequest("GET", "/api/kb-changes", nil)
				(&KBReleaseController{}).replyError(ctx, err)
				if recorder.Code != c.want {
					t.Fatalf("%s：期望 %d，实得 %d（body=%s）", name, c.want, recorder.Code, recorder.Body.String())
				}
				var body map[string]any
				if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
					t.Fatalf("响应不是 JSON: %v", err)
				}
				msg := kbCtrlMsg(body)
				if c.want == http.StatusInternalServerError {
					// 未预期错误走固定文案：内部原文（"连接断了"）进响应体就是
					// 一个免费的响应体放大器，而这一句在值班手上是"该报装配"还是"该改请求"的分界。
					if msg != "知识库变更操作失败" {
						t.Errorf("500 该回固定文案，实得 %q", msg)
					}
					if strings.Contains(msg, "连接断了") || strings.Contains(msg, "kbc_1") {
						t.Errorf("500 的响应体里出现了内部错误原文：%q", msg)
					}
					return
				}
				// 业务结论要把服务的原话给出去（前端照着它决定下一步），
				// 包装过的上下文也不许丢 —— 少了"是哪条变更"，四个字的报错在日志外无从对账。
				if !strings.Contains(msg, c.err.Error()) {
					t.Errorf("响应里没带上 sentinel 原文 %q，实得 %q", c.err.Error(), msg)
				}
				if shape == "包一层" && !strings.Contains(msg, "的处理失败") {
					t.Errorf("包装过的上下文丢了：real=%q", msg)
				}
			})
		}
	}
}

// ---------------------------------------------------------------------------
// 200：成功形状与传下去的实参
// ---------------------------------------------------------------------------

// TestKBReleaseController_SubmitShape 提交成功：变更行、审批入队的三列、响应体里的号。
//
// 三列断言打的是**网关收到的那个入参**：subject_id 必须是刚生成的变更号，
// 否则两次提交会共用一条审批（幂等键是 subject_type+subject_id+policy_key），
// 第二次改动就永远发不出去。
func TestKBReleaseController_SubmitShape(t *testing.T) {
	store := newKBFakeStore()
	engine, gw := kbCtrlReady(t, store)
	w := kbCtrlDo(t, engine, "POST", "/api/kb-changes",
		`{"product_id":" p9 ","op":"REVISE","content":" 新正文 ","reason":" 口径要改 ","document_id":11,"target_chunk_id":5}`)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实得 %d：%s", w.Code, w.Body.String())
	}
	row := store.lastInsert
	if row == nil {
		t.Fatal("没有落变更行")
	}
	if row.ProductID != "p9" || row.Reason != "口径要改" || row.Content != "新正文" {
		t.Errorf("入参整理没做够：%+v", row)
	}
	if row.Op != model.KBChangeOpRevise || row.Status != model.KBChangeStatusPending {
		t.Errorf("op/status 落错：%+v", row)
	}
	if row.RequestedBy != "7" {
		t.Errorf("requested_by 该取会话身份 7，实得 %q", row.RequestedBy)
	}
	if row.ApprovalID == "" {
		t.Error("变更行没挂上审批号（发布时会被判成\"结论不明\"）")
	}
	if gw.lastSubmit.SubjectType != service.KBChangeApprovalSubjectType ||
		gw.lastSubmit.PolicyKey != service.KBChangeApprovalPolicyKey {
		t.Errorf("审批入队三列错了两列：%+v", gw.lastSubmit)
	}
	if gw.lastSubmit.SubjectID != row.ID {
		t.Errorf("审批的 subject_id=%q 与变更号 %q 不同源", gw.lastSubmit.SubjectID, row.ID)
	}
	data := kbCtrlDataOf(t, kbCtrlBody(t, w))
	if data["id"] != row.ID {
		t.Errorf("响应该回显变更号 %q，实得 %v", row.ID, data["id"])
	}
	// 响应里不许出现凭证类字段：本表没有，但整份 model 被序列化出去这件事要盯住恢复凭证那一列。
	if strings.Contains(w.Body.String(), "resume_token") {
		t.Errorf("响应体里出现了 resume_token：%s", w.Body.String())
	}
}

// TestKBReleaseController_SubmitApprovalEnqueueFailed 审批入队失败 ⇒ 一行变更都不留。
//
// 顺序判据（先审批后入库）在出口上的读数：500（内部错误，不是业务结论），且 InsertChange 没被调。
func TestKBReleaseController_SubmitApprovalEnqueueFailed(t *testing.T) {
	store := newKBFakeStore()
	gw := &kbFakeApproval{submitFn: func(service.ApprovalSubmitInput) (*model.ApprovalRequest, bool, error) {
		return nil, false, errors.New("审批底座不可用")
	}}
	engine := kbCtrlRouter(t, store, gw, uint(7))
	w := kbCtrlDo(t, engine, "POST", "/api/kb-changes",
		`{"product_id":"p9","op":"add","content":"c","reason":"r","document_id":1}`)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("期望 500，实得 %d：%s", w.Code, w.Body.String())
	}
	if store.called("InsertChange") {
		t.Error("审批没入队却落了变更行 ⇒ 孤儿待办（AC① 的反例）")
	}
}

// TestKBReleaseController_GetChange404 (nil, nil) 的 404 判断归本层。
//
// 两条分开测：服务侧"读不到"回的是 (nil, nil)（不是 error），所以 404 只能在这里发生。
func TestKBReleaseController_GetChange404(t *testing.T) {
	store := newKBFakeStore()
	store.getChangeFn = func(string) (*model.KBChangeRequest, error) { return nil, nil }
	engine, _ := kbCtrlReady(t, store)
	w := kbCtrlDo(t, engine, "GET", "/api/kb-changes/kbc_missing", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("期望 404，实得 %d：%s", w.Code, w.Body.String())
	}
	if store.lastGetID != "kbc_missing" {
		t.Errorf("按号取数取错了：%q", store.lastGetID)
	}
}

// TestKBReleaseController_GetChangeFound 命中时把整行回出去（含审批号与状态）。
func TestKBReleaseController_GetChangeFound(t *testing.T) {
	store := newKBFakeStore()
	store.getChangeFn = func(string) (*model.KBChangeRequest, error) {
		return &model.KBChangeRequest{
			ID: "kbc_a", ProductID: "p1", Op: model.KBChangeOpAdd, Content: "正文",
			Reason: "理由", Status: model.KBChangeStatusPending, ApprovalID: "apr_a",
			RequestedBy: "7",
		}, nil
	}
	engine, _ := kbCtrlReady(t, store)
	w := kbCtrlDo(t, engine, "GET", "/api/kb-changes/kbc_a", "")
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实得 %d：%s", w.Code, w.Body.String())
	}
	data := kbCtrlDataOf(t, kbCtrlBody(t, w))
	for key, want := range map[string]any{
		"id": "kbc_a", "status": "pending", "approval_id": "apr_a", "op": "add", "content": "正文",
	} {
		if data[key] != want {
			t.Errorf("data.%s 该是 %v，实得 %v", key, want, data[key])
		}
	}
}

// TestKBReleaseController_WithdrawPaths 撤回的三条去向：放行 / 已被发布（CAS 没中）/ 不存在。
func TestKBReleaseController_WithdrawPaths(t *testing.T) {
	t.Run("放行", func(t *testing.T) {
		store := newKBFakeStore()
		store.getChangeFn = func(string) (*model.KBChangeRequest, error) {
			return &model.KBChangeRequest{ID: "kbc_a", Status: model.KBChangeStatusPending}, nil
		}
		store.withdrawFn = func(string) (bool, error) { return true, nil }
		var audit model.KBChangeAuditLog
		store.recordAuditFn = func(log model.KBChangeAuditLog) error {
			audit = log
			return nil
		}
		// WithdrawChange 末尾还要再读一次回给调用方：第二次读要拿到 withdrawn 那一份。
		var reads int
		first := store.getChangeFn
		store.getChangeFn = func(id string) (*model.KBChangeRequest, error) {
			reads++
			if reads > 1 {
				return &model.KBChangeRequest{ID: id, Status: model.KBChangeStatusWithdrawn}, nil
			}
			return first(id)
		}
		engine, _ := kbCtrlReady(t, store)
		w := kbCtrlDo(t, engine, "POST", "/api/kb-changes/kbc_a/withdraw", "")
		if w.Code != http.StatusOK {
			t.Fatalf("期望 200，实得 %d：%s", w.Code, w.Body.String())
		}
		if kbCtrlDataOf(t, kbCtrlBody(t, w))["status"] != "withdrawn" {
			t.Errorf("回给前端的该是改后的行：%v", kbCtrlBody(t, w)["data"])
		}
		// 留痕的三格各打一处：键的拼法、跃迁本身、以及"谁按的"。
		// AC③ 要的是"谁在什么时候把它推到了哪一格"，条数对得上而 actor 是空串照样查不出人。
		if audit.SubjectKey != "change:kbc_a" || audit.Action != model.KBAuditWithdrawn ||
			audit.OldValue != model.KBChangeStatusPending || audit.NewValue != model.KBChangeStatusWithdrawn ||
			audit.Actor != "7" {
			t.Errorf("撤回留痕不对：%+v", audit)
		}
		if reads != 2 {
			t.Errorf("撤回后该回读一次改后的行，实得读 %d 次", reads)
		}
	})
	t.Run("已被发布", func(t *testing.T) {
		store := newKBFakeStore()
		store.getChangeFn = func(string) (*model.KBChangeRequest, error) {
			return &model.KBChangeRequest{ID: "kbc_a", Status: model.KBChangeStatusApplied}, nil
		}
		engine, _ := kbCtrlReady(t, store)
		w := kbCtrlDo(t, engine, "POST", "/api/kb-changes/kbc_a/withdraw", "")
		if w.Code != http.StatusConflict {
			t.Fatalf("已发布的变更撤不掉，期望 409，实得 %d：%s", w.Code, w.Body.String())
		}
		if store.called("WithdrawChange") {
			t.Error("跃迁表已经判了不许，不该再去 CAS 改行")
		}
	})
	t.Run("读不到行", func(t *testing.T) {
		store := newKBFakeStore()
		store.getChangeFn = func(string) (*model.KBChangeRequest, error) { return nil, nil }
		engine, _ := kbCtrlReady(t, store)
		w := kbCtrlDo(t, engine, "POST", "/api/kb-changes/kbc_none/withdraw", "")
		if w.Code != http.StatusNotFound {
			t.Fatalf("期望 404，实得 %d：%s", w.Code, w.Body.String())
		}
	})
}

// TestKBReleaseController_AuditSubjectKeys 两条留痕读口各自的前缀与 limit 传法。
//
// 前缀是本层的产物（服务只认 subject_key 整串），拼错一位就是"查得到、永远空"，
// 而空列表在审计页面上读起来跟"这条变更没被动过"一模一样。
func TestKBReleaseController_AuditSubjectKeys(t *testing.T) {
	cases := []struct {
		path        string
		wantSubject string
		wantLimit   int
	}{
		{"/api/kb-changes/kbc_9/audit", "change:kbc_9", 0},
		{"/api/kb-changes/kbc_9/audit?limit=7", "change:kbc_9", 7},
		{"/api/kb-releases/p9/audit", "release:p9", 0},
		{"/api/kb-releases/p9/audit?limit=200", "release:p9", 200},
	}
	for _, c := range cases {
		store := newKBFakeStore()
		store.listAuditFn = func(string, int) ([]model.KBChangeAuditLog, error) {
			return []model.KBChangeAuditLog{{Action: model.KBAuditSubmitted}, {Action: model.KBAuditWithdrawn}}, nil
		}
		engine, _ := kbCtrlReady(t, store)
		w := kbCtrlDo(t, engine, "GET", c.path, "")
		if w.Code != http.StatusOK {
			t.Fatalf("%s 期望 200，实得 %d：%s", c.path, w.Code, w.Body.String())
		}
		if store.lastSubject != c.wantSubject {
			t.Errorf("%s：subject_key 该是 %q，实得 %q", c.path, c.wantSubject, store.lastSubject)
		}
		// 0 是"让仓储按它的默认值收口"的哨兵，不是"一条都不要"。
		if store.lastLimit != c.wantLimit {
			t.Errorf("%s：limit 该传 %d，实得 %d", c.path, c.wantLimit, store.lastLimit)
		}
		data := kbCtrlDataOf(t, kbCtrlBody(t, w))
		if data["total"] != float64(2) {
			t.Errorf("%s：留痕条数该回 2，实得 %v", c.path, data["total"])
		}
	}

	// 零条留痕回 [] 而不是 null。这一格单独现测：兜底只在 nil 分支上开火，
	// 上面四条腿都带着数据、走不到它。
	store := newKBFakeStore()
	store.listAuditFn = func(string, int) ([]model.KBChangeAuditLog, error) { return nil, nil }
	engine, _ := kbCtrlReady(t, store)
	w := kbCtrlDo(t, engine, "GET", "/api/kb-changes/kbc_none/audit", "")
	empty := kbCtrlDataOf(t, kbCtrlBody(t, w))
	if list, ok := empty["list"].([]any); !ok || len(list) != 0 {
		t.Errorf("无留痕时 data.list 该是空数组，实得 %#v", empty["list"])
	}
}

// TestKBReleaseController_GetReleaseAbsent exists:false 而不是 404。
//
// "没有行"是一个可读的事实（这库从没启停过治理），管理端要在这里显示"未启用发布制"；
// 404 会被前端读成"库不存在"，于是运营换一个真的不存在的产品也看到同一句话。
func TestKBReleaseController_GetReleaseAbsent(t *testing.T) {
	store := newKBFakeStore()
	store.getReleaseFn = func(string) (*model.KBRelease, error) { return nil, nil }
	engine, _ := kbCtrlReady(t, store)
	w := kbCtrlDo(t, engine, "GET", "/api/kb-releases/p_none", "")
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实得 %d：%s", w.Code, w.Body.String())
	}
	data := kbCtrlDataOf(t, kbCtrlBody(t, w))
	if data["exists"] != false || data["governed"] != false {
		t.Errorf("缺行时该回 exists:false/governed:false，实得 %v", data)
	}
	if data["product_id"] != "p_none" {
		t.Errorf("该回显被询问的库号，实得 %v", data["product_id"])
	}
}

// TestKBReleaseController_GetReleasePresent 有行时把四个号原样交出去。
//
// 四个号是发布视图的全部事实（生效/上一版/待发布桶/高水位）加一条禁令，
// 少回一格前端就只能靠猜 —— 而猜错的那一格恰好是"能不能回滚"。
func TestKBReleaseController_GetReleasePresent(t *testing.T) {
	store := newKBFakeStore()
	store.getReleaseFn = func(string) (*model.KBRelease, error) {
		return &model.KBRelease{
			ProductID: "p1", Governed: true, EffectiveVersion: 3, PreviousVersion: 2,
			DraftVersion: 4, AllocatedVersion: 4, RecalledVersion: 0, ChangedBy: "7",
		}, nil
	}
	engine, _ := kbCtrlReady(t, store)
	w := kbCtrlDo(t, engine, "GET", "/api/kb-releases/p1", "")
	data := kbCtrlDataOf(t, kbCtrlBody(t, w))
	for key, want := range map[string]any{
		"product_id": "p1", "governed": true,
		"effective_version": float64(3), "previous_version": float64(2),
		"draft_version": float64(4), "allocated_version": float64(4),
		"recalled_version": float64(0),
	} {
		if data[key] != want {
			t.Errorf("data.%s 该是 %v，实得 %v", key, want, data[key])
		}
	}
	// exists 只在缺行那一格出现；有行时把整份 model 序列化出去，不该多出这个键。
	if _, ok := data["exists"]; ok {
		t.Errorf("有行时不该带 exists 键：%v", data)
	}
}

// TestKBReleaseController_SetGovernedExplicitBool governed=false 必须真的被传下去。
//
// 只测 true 那一格会漏掉整个停用通路；而漏字段的 400 在 BadBodies 里已经测过，
// 这一格要的是"显式 false ≠ 没传 ≠ 默认 true"。
func TestKBReleaseController_SetGovernedExplicitBool(t *testing.T) {
	for _, want := range []bool{true, false} {
		store := newKBFakeStore()
		store.setGovernedFn = func(product string, governed bool, actor string) (*model.KBRelease, error) {
			if actor != "7" {
				t.Errorf("changed_by 该落到会话身份，实得 %q", actor)
			}
			return &model.KBRelease{ProductID: product, Governed: governed}, nil
		}
		engine, _ := kbCtrlReady(t, store)
		w := kbCtrlDo(t, engine, "PATCH", "/api/kb-releases/p1/governed",
			fmt.Sprintf(`{"governed":%t}`, want))
		if w.Code != http.StatusOK {
			t.Fatalf("governed=%t 期望 200，实得 %d：%s", want, w.Code, w.Body.String())
		}
		if store.lastGoverned != want {
			t.Errorf("请求要 governed=%t，仓储收到 %t", want, store.lastGoverned)
		}
		if kbCtrlDataOf(t, kbCtrlBody(t, w))["governed"] != want {
			t.Errorf("响应该回显改后的值 %t：%v", want, kbCtrlBody(t, w)["data"])
		}
	}
}

// TestKBReleaseController_ListReleases 已启用的库（仓储只回 governed=true 的行）。
func TestKBReleaseController_ListReleases(t *testing.T) {
	store := newKBFakeStore()
	store.listReleasesFn = func() ([]model.KBRelease, error) {
		return []model.KBRelease{{ProductID: "p1", Governed: true}, {ProductID: "p2", Governed: true}}, nil
	}
	engine, _ := kbCtrlReady(t, store)
	w := kbCtrlDo(t, engine, "GET", "/api/kb-releases", "")
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实得 %d：%s", w.Code, w.Body.String())
	}
	// 这一格 data 是**数组**而不是 {list,total}：已启用的库是一张清单，不是一页待办。
	arr, ok := kbCtrlBody(t, w)["data"].([]any)
	if !ok || len(arr) != 2 {
		t.Fatalf("data 该是 2 条的数组，实得 %v", kbCtrlBody(t, w)["data"])
	}
	first, _ := arr[0].(map[string]any)
	if first["product_id"] != "p1" || first["governed"] != true {
		t.Errorf("清单一格的内容不对：%v", first)
	}

	// 空清单回 []，不回 null（同上一条列表出口的兜底）。这一格必须现测：
	// 兜底写在 nil 分支上，只有零行的那次读取才走得到。
	storeEmpty := newKBFakeStore()
	storeEmpty.listReleasesFn = func() ([]model.KBRelease, error) { return nil, nil }
	engineEmpty, _ := kbCtrlReady(t, storeEmpty)
	w = kbCtrlDo(t, engineEmpty, "GET", "/api/kb-releases", "")
	raw := kbCtrlBody(t, w)["data"]
	arrEmpty, ok := raw.([]any)
	if !ok || len(arrEmpty) != 0 {
		t.Fatalf("零行时 data 该是空数组，实得 %#v", raw)
	}
}

// TestKBReleaseController_StatsShape 发布视图的五格：在服/全量/待办/档位/观察标志。
//
// shadow_stats 在 off 与 shadow 两档都为真（它是"这个数不是闸门过滤后的读数"），
// 只有 on 才假 —— 界面靠它决定要不要给数字加"观察期"角标。
func TestKBReleaseController_StatsShape(t *testing.T) {
	cases := []struct {
		env         string
		wantMode    string
		wantShadow  bool
		wantInForce float64
	}{
		{"off", "off", true, 9}, // 闸门没参与过滤 ⇒ 与 total 同数
		{"shadow", "shadow", true, 9},
		{"on", "on", false, 4}, // 真拦 ⇒ 在服数小于全量
	}
	for _, c := range cases {
		t.Setenv(kbrelease.FlagEnv, c.env)
		store := newKBFakeStore()
		store.getReleaseFn = func(string) (*model.KBRelease, error) {
			return &model.KBRelease{ProductID: "p1", Governed: true, EffectiveVersion: 1}, nil
		}
		store.countInForceFn = func(string) (int64, int64, error) {
			// 仓储自己按档位算：闸门没开时 inForce==total（全量），开了才少。
			if c.env == "on" {
				return 4, 9, nil
			}
			return 9, 9, nil
		}
		store.pendingFn = func(string) ([]model.KBChangeRequest, error) {
			return []model.KBChangeRequest{{ID: "kbc_a"}, {ID: "kbc_b"}}, nil
		}
		engine, _ := kbCtrlReady(t, store)
		w := kbCtrlDo(t, engine, "GET", "/api/kb-releases/p1/stats", "")
		if w.Code != http.StatusOK {
			t.Fatalf("%s 档期望 200，实得 %d：%s", c.env, w.Code, w.Body.String())
		}
		data := kbCtrlDataOf(t, kbCtrlBody(t, w))
		if data["in_force"] != c.wantInForce || data["total"] != float64(9) {
			t.Errorf("%s 档：in_force/total 实得 %v/%v，期望 %v/9",
				c.env, data["in_force"], data["total"], c.wantInForce)
		}
		if data["pending_changes"] != float64(2) {
			t.Errorf("%s 档：待办数该是 2，实得 %v", c.env, data["pending_changes"])
		}
		if data["gate_mode"] != c.wantMode || data["shadow_stats"] != c.wantShadow {
			t.Errorf("%s 档：档位回显错了 %v / %v", c.env, data["gate_mode"], data["shadow_stats"])
		}
	}
}

// TestKBReleaseController_StatsEmptyProduct 空白库号：路径参数去空白后为空 ⇒ 400。
//
// gin 的 :product 不会给空串（空段匹配不到路由），这一格测的是"只传空白"的形状
// —— /api/kb-releases/%20/stats 里 product 是 " "，控制器 TrimSpace 之后为空。
func TestKBReleaseController_StatsEmptyProduct(t *testing.T) {
	store := newKBFakeStore()
	engine, _ := kbCtrlReady(t, store)
	w := kbCtrlDo(t, engine, "GET", "/api/kb-releases/%20/stats", "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("空白库号期望 400，实得 %d：%s", w.Code, w.Body.String())
	}
	if msg := kbCtrlMsg(kbCtrlBody(t, w)); !strings.Contains(msg, "product_id 不能为空") {
		t.Errorf("报错文案：%q", msg)
	}
	if len(store.calls) != 0 {
		t.Errorf("空白库号不该动底座（calls=%v）", store.calls)
	}
}

// TestKBReleaseController_PublicNullVsEmpty "一条都没批准"与"发生了一次空发布"必须可区分。
//
// result 为 null（没发生）而不是 {}（发生了一次零条的发布），verdicts 五格都回空数组
// 而不是 null —— 前端按 verdicts 渲染五个筛选器，null 会渲染成"没查"。
func TestKBReleaseController_PublicNullVsEmpty(t *testing.T) {
	store := newKBFakeStore()
	// 治理开关先开着：未进发布制的库走不到"这条腿"（另有 TestKBReleaseController_PublishGovernance）。
	// 桶为 0 是"这库这期间什么都没攒下"，只有这一种形状才允许 200 + result:null。
	store.getReleaseFn = kbCtrlGoverned(0)
	store.pendingFn = func(string) ([]model.KBChangeRequest, error) {
		return []model.KBChangeRequest{
			{ID: "kbc_wait", Op: model.KBChangeOpAdd, ProductID: "p1", ApprovalID: "apr_kbc_wait"},
		}, nil
	}
	gw := &kbFakeApproval{}
	gw.getFn = func(id string) (*model.ApprovalRequest, error) {
		// 这一格要的是"审批行在、结论是 pending"：网关默认的 Get 回 (nil,nil)，
		// 那会被服务判成"结论不明"，于是这条腿测的就不是"等审批"那一档了。
		return &model.ApprovalRequest{ID: id, SubjectType: service.KBChangeApprovalSubjectType,
			SubjectID: "kbc_wait", PolicyKey: service.KBChangeApprovalPolicyKey,
			Status: model.ApprovalStatusPending}, nil
	}
	engine := kbCtrlRouter(t, store, gw, uint(7))
	w := kbCtrlDo(t, engine, "POST", "/api/kb-releases/p1/publish", "")
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实得 %d：%s", w.Code, w.Body.String())
	}
	body := kbCtrlBody(t, w)
	data := kbCtrlDataOf(t, body)
	if raw, ok := data["result"]; !ok || raw != nil {
		t.Errorf("没有可发布的变更时 result 该是 null（这次什么都没发生），实得 %#v", data["result"])
	}
	verdicts, ok := data["verdicts"].(map[string]any)
	if !ok {
		t.Fatalf("verdicts 该是对象，实得 %v", data["verdicts"])
	}
	for _, key := range []string{"included", "skipped_pending", "skipped_refused", "skipped_expired", "skipped_unknown"} {
		arr, isArray := verdicts[key].([]any)
		if !isArray {
			t.Errorf("verdicts.%s 该是数组（哪怕是空的），实得 %#v", key, verdicts[key])
			continue
		}
		want := 0
		if key == "skipped_pending" {
			want = 1
		}
		if len(arr) != want {
			t.Errorf("verdicts.%s 该有 %d 条，实得 %v", key, want, arr)
		}
	}
	if store.called("Publish") {
		t.Error("一条都没批准时不该进发布事务（那会留下一条不存在的发布留痕）")
	}
}

// TestKBReleaseController_PublishGovernanceAndBucket 发布的两道前置在出口上的形状。
//
// 三格都判"哪一句该被说出来"：
//   - 没有发布行 / 有行但没开治理 ⇒ 409 且**不读待办**：把"你没开门"说成"屋里是空的"
//     是最坏的一种误读，运营照着后者只会再点一次发布按钮；
//   - 开着治理、桶上有货、一条变更都没批 ⇒ 200 且 result 有值。这批货是导入链路攒下的，
//     漏掉它等于"导入也走发布制"整句承诺落空 —— 服务侧原先正是在这里按"零变更"短路掉的。
func TestKBReleaseController_PublishGovernanceAndBucket(t *testing.T) {
	table := []struct {
		name        string
		release     func(string) (*model.KBRelease, error)
		wantCode    int
		wantPublish bool
	}{
		{"没有发布行", func(string) (*model.KBRelease, error) { return nil, nil },
			http.StatusConflict, false},
		{"有行但没开治理", func(string) (*model.KBRelease, error) {
			return &model.KBRelease{ID: 1, ProductID: "p1", Governed: false}, nil
		}, http.StatusConflict, false},
		{"已治理 + 桶上有货 + 零变更", kbCtrlGoverned(3), http.StatusOK, true},
	}
	for _, c := range table {
		t.Run(c.name, func(t *testing.T) {
			store := newKBFakeStore()
			store.getReleaseFn = c.release
			store.pendingFn = func(string) ([]model.KBChangeRequest, error) { return nil, nil }
			store.publishFn = func(repository.PublishIntent) (*repository.PublishResult, error) {
				return &repository.PublishResult{FromVersion: 2, ToVersion: 3}, nil
			}
			engine := kbCtrlRouter(t, store, &kbFakeApproval{}, uint(7))
			w := kbCtrlDo(t, engine, "POST", "/api/kb-releases/p1/publish", "")
			if w.Code != c.wantCode {
				t.Fatalf("%s：期望 %d，实得 %d：%s", c.name, c.wantCode, w.Code, w.Body.String())
			}
			if got := store.called("Publish"); got != c.wantPublish {
				t.Errorf("%s：发布事务调没调=%v，期望 %v（calls=%v）", c.name, got, c.wantPublish, store.calls)
			}
			if c.wantCode == http.StatusConflict {
				if store.called("PendingChanges") {
					t.Errorf("%s：没过治理判据就不该去读待办", c.name)
				}
				if msg := kbCtrlMsg(kbCtrlBody(t, w)); !strings.Contains(msg, "未启用发布制") {
					t.Errorf("%s：要把服务的原话给出去，实得 %q", c.name, msg)
				}
				return
			}
			data := kbCtrlDataOf(t, kbCtrlBody(t, w))
			result, _ := data["result"].(map[string]any)
			if result == nil || result["to_version"] != float64(3) {
				t.Errorf("%s：桶上的货必须真的上线，实得 %v", c.name, data["result"])
			}
			if intent := store.lastIntent; intent.ProductID != "p1" || intent.Actor != "7" ||
				len(intent.Changes) != 0 {
				t.Errorf("%s：空变更批也要归人、带库号，实得 %+v", c.name, intent)
			}
		})
	}
}

// TestKBReleaseController_PublishIntentCarriesApprovedOnly 事务只带已批准的那几条。
//
// 五格分账 + intent 里只剩 approved 一条，这两处一起断言才测到"分账不是摆设"：
// 只断言 verdicts 的话，实现可以报完账再把五条全带进事务。
func TestKBReleaseController_PublishIntentCarriesApprovedOnly(t *testing.T) {
	store := newKBFakeStore()
	store.getReleaseFn = kbCtrlGoverned(0)
	store.pendingFn = func(string) ([]model.KBChangeRequest, error) {
		return []model.KBChangeRequest{
			{ID: "kbc_ok", Op: model.KBChangeOpAdd, ProductID: "p1", DocumentID: 7, Content: "新格", ApprovalID: "apr_kbc_ok"},
			{ID: "kbc_wait", Op: model.KBChangeOpAdd, ProductID: "p1", ApprovalID: "apr_kbc_wait"},
			{ID: "kbc_noapr", Op: model.KBChangeOpAdd, ProductID: "p1"}, // 没有审批号
		}, nil
	}
	gw := &kbFakeApproval{}
	gw.getFn = func(id string) (*model.ApprovalRequest, error) {
		switch id {
		case "apr_kbc_ok":
			return kbApproved("kbc_ok"), nil
		case "apr_kbc_wait":
			return &model.ApprovalRequest{ID: id, SubjectType: service.KBChangeApprovalSubjectType,
				SubjectID: "kbc_wait", PolicyKey: service.KBChangeApprovalPolicyKey,
				Status: model.ApprovalStatusPending}, nil
		}
		return nil, nil
	}
	store.publishFn = func(in repository.PublishIntent) (*repository.PublishResult, error) {
		return &repository.PublishResult{FromVersion: 0, ToVersion: 1,
			Applied: []repository.AppliedChange{{ChangeID: "kbc_ok", AppliedChunkID: 99, Content: "新格"}}}, nil
	}
	engine := kbCtrlRouter(t, store, gw, uint(7))
	w := kbCtrlDo(t, engine, "POST", "/api/kb-releases/p1/publish", "")
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实得 %d：%s", w.Code, w.Body.String())
	}
	intent := store.lastIntent
	if len(intent.Changes) != 1 || intent.Changes[0].ID != "kbc_ok" {
		t.Fatalf("进事务的该只有已批准那条，实得 %+v", intent.Changes)
	}
	if intent.Actor != "7" || intent.ProductID != "p1" {
		t.Errorf("intent 的库号/操作者传错：%+v", intent)
	}
	data := kbCtrlDataOf(t, kbCtrlBody(t, w))
	verdicts, _ := data["verdicts"].(map[string]any)
	if included, _ := verdicts["included"].([]any); len(included) != 1 {
		t.Errorf("included 该有 1 条，实得 %v", verdicts["included"])
	}
	if unknown, _ := verdicts["skipped_unknown"].([]any); len(unknown) != 1 {
		t.Errorf("没审批号那条该记成结论不明，实得 %v", verdicts["skipped_unknown"])
	}
	result, _ := data["result"].(map[string]any)
	if result["from_version"] != float64(0) || result["to_version"] != float64(1) {
		t.Errorf("指针移动该原样回给调用方，实得 %v", data["result"])
	}
}

// TestKBReleaseController_PointerMovesNotInterchangeable 回滚与放回各走各的出口。
//
// 两条路由差在方向，共用一个 body 形状（都不收）。断言打在**仓储的哪一扇被调**上：
// 实现如果把 restore 也接进 Rollback，两个留痕动作（rollback/restored）会并成一条，
// 而"放回被回滚的那版"在审计里就跟"再回滚一格"长得一样 —— 那是最坏的一次误读。
func TestKBReleaseController_PointerMovesNotInterchangeable(t *testing.T) {
	for _, c := range []struct {
		path     string
		wantCall string
		from, to int
	}{
		{"/api/kb-releases/p1/rollback", "Rollback", 2, 1},
		{"/api/kb-releases/p1/restore", "Restore", 1, 2},
	} {
		store := newKBFakeStore()
		res := &repository.RollbackResult{FromVersion: c.from, ToVersion: c.to, Recalled: c.to}
		if c.wantCall == "Rollback" {
			store.rollbackFn = func(string, string) (*repository.RollbackResult, error) { return res, nil }
		} else {
			store.restoreFn = func(string, string) (*repository.RollbackResult, error) { return res, nil }
		}
		engine, _ := kbCtrlReady(t, store)
		w := kbCtrlDo(t, engine, "POST", c.path, "")
		if w.Code != http.StatusOK {
			t.Fatalf("%s 期望 200，实得 %d：%s", c.path, w.Code, w.Body.String())
		}
		if !store.called(c.wantCall) {
			t.Errorf("%s 该调仓储的 %s，实得调用名单 %v", c.path, c.wantCall, store.calls)
		}
		other := "Restore"
		if c.wantCall == "Restore" {
			other = "Rollback"
		}
		if store.called(other) {
			t.Errorf("%s 串到了 %s 上（calls=%v）", c.path, other, store.calls)
		}
		data := kbCtrlDataOf(t, kbCtrlBody(t, w))
		if data["effective_version"] != nil {
			// RollbackResult 的三个号是 from/to/recalled，别把指针字段名混进来。
			t.Errorf("回滚响应体形状不对：%v", data)
		}
		if data["from_version"] != float64(c.from) || data["to_version"] != float64(c.to) {
			t.Errorf("%s：三个号回错了 %v", c.path, data)
		}
	}
}

// TestKBReleaseController_Taxonomy 值域只有一个出处：model 那三张名单。
//
// 断言逐条对上 model 常量而不是硬编码字符串 —— 硬编码的话，加一个状态时这条用例
// 会红在"名单不对"，而不是红在"前端选不到"，前者才是我们要的出声。
func TestKBReleaseController_Taxonomy(t *testing.T) {
	engine, _ := kbCtrlReady(t, newKBFakeStore())
	w := kbCtrlDo(t, engine, "GET", "/api/kb-changes/taxonomy", "")
	if w.Code != http.StatusOK {
		t.Fatalf("值域出口不该依赖底座，实得 %d：%s", w.Code, w.Body.String())
	}
	data := kbCtrlDataOf(t, kbCtrlBody(t, w))
	for key, want := range map[string][]string{
		"ops": model.KBChangeOps, "statuses": model.KBChangeStatuses,
		"audit_actions": model.KBChangeAuditActions,
	} {
		got, ok := data[key].([]any)
		if !ok {
			t.Fatalf("data.%s 该是数组，实得 %#v", key, data[key])
		}
		if len(got) != len(want) {
			t.Fatalf("data.%s 有 %d 项，model 名单有 %d 项 ⇒ 两处不同源了", key, len(got), len(want))
		}
		for i, v := range want {
			if got[i] != v {
				t.Errorf("data.%s[%d]=%v，model 给的是 %q（顺序也是契约的一部分）", key, i, got[i], v)
			}
		}
	}
	if data["approval_subject"] != service.KBChangeApprovalSubjectType ||
		data["approval_policy_key"] != service.KBChangeApprovalPolicyKey {
		t.Errorf("审批两键的回显与服务侧常量不一致：%v", data)
	}
	// 值域里没有 rejected / expired（批没批的唯一事实源是审批表）。
	statuses, _ := data["statuses"].([]any)
	for _, banned := range []string{"rejected", "expired"} {
		for _, s := range statuses {
			if s == banned {
				t.Errorf("变更状态里出现了 %q ⇒ 审批结论被复制了一份", banned)
			}
		}
	}
}

// TestKBReleaseController_TaxonomyWithoutStore 值域出口不查底座。
func TestKBReleaseController_TaxonomyWithoutStore(t *testing.T) {
	for _, store := range []*kbFakeStore{nil, {}} {
		engine := kbCtrlRouter(t, store, nil, nil)
		w := kbCtrlDo(t, engine, "GET", "/api/kb-changes/taxonomy", "")
		if w.Code != http.StatusOK {
			t.Errorf("svc=%v 时 taxonomy 仍该回 200，实得 %d", store, w.Code)
		}
	}
}

// ---------------------------------------------------------------------------
// 真库一条：出口与真实底座的合装
// ---------------------------------------------------------------------------

// TestKBReleaseController_RealStack 把**真的**仓储 + 真的审批服务接到这组路由上走一遍。
//
// 替身测不出的三件事在这一条里：
//  1. 静态段 /gate 与 /:product 的匹配优先级（gin 的注册顺序判据，见 RegisterRoutes 注释）；
//  2. service.NewKBReleaseService 的三根句柄真的能凑出一个 Available()=true 的组合
//     —— 替身永远为真，装配写错（比如 approvals 传了 nil）在这里才会露出来；
//  3. 一次真实发布之后，出口回的数字与库里的一致（在服/待办/留痕条数）。
//
// 闸门档位全程 off：本条测的是出口的合装，不是闸门的拦截（那是召回侧的用例）。
func TestKBReleaseController_RealStack(t *testing.T) {
	t.Setenv(kbrelease.FlagEnv, "off")
	database := testutil.NewTestDB(t,
		&model.KBRelease{}, &model.KBChangeRequest{}, &model.KBChangeAuditLog{},
		&model.KnowledgeChunk{}, &model.ApprovalRequest{},
	)
	gin.SetMode(gin.TestMode)
	svc := service.NewKBReleaseService(
		repository.NewKBReleaseRepository(database),
		service.NewApprovalRequestService(repository.NewApprovalRequestRepositoryWithDB(database), nil),
		nil,
	)
	if !svc.Available() {
		t.Fatal("真底座装进来了但 Available() 仍为假 ⇒ 装配形状不对")
	}
	engine := gin.New()
	auth := engine.Group("/api")
	auth.Use(func(ctx *gin.Context) { ctx.Set("user_id", uint(3)); ctx.Next() })
	NewKBReleaseController(svc).RegisterRoutes(auth)

	const product = "pctrl_real"
	getData := func(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
		t.Helper()
		if w.Code != http.StatusOK {
			t.Fatalf("期望 200，实得 %d：%s", w.Code, w.Body.String())
		}
		return kbCtrlDataOf(t, kbCtrlBody(t, w))
	}

	// /gate 在真路由上仍是静态段：命中它不会去查 kb_releases。
	w := kbCtrlDo(t, engine, "GET", "/api/kb-releases/gate", "")
	data := getData(t, w)
	if data["mode"] != "off" || data["governed_count"] != float64(0) {
		t.Errorf("/gate 读数不对：%v", data)
	}

	// 未启用的库：exists:false（不是 404）。
	w = kbCtrlDo(t, engine, "GET", "/api/kb-releases/"+product, "")
	if getData(t, w)["exists"] != false {
		t.Errorf("启用前该回 exists=false：%v", kbCtrlBody(t, w)["data"])
	}

	// 未启用就发布 ⇒ 409（真仓储的那条 sentinel 穿过真服务到出口）。
	w = kbCtrlDo(t, engine, "POST", "/api/kb-releases/"+product+"/publish", "")
	if w.Code != http.StatusConflict {
		t.Fatalf("未进发布制的库发布期望 409，实得 %d：%s", w.Code, w.Body.String())
	}

	// 启用 → 提交一条变更 → 待办可见。
	w = kbCtrlDo(t, engine, "PATCH", "/api/kb-releases/"+product+"/governed", `{"governed":true}`)
	getData(t, w)
	w = kbCtrlDo(t, engine, "POST", "/api/kb-changes",
		`{"product_id":"`+product+`","op":"add","content":"真库正文","reason":"出口合装","document_id":555}`)
	row := getData(t, w)
	changeID, _ := row["id"].(string)
	if changeID == "" {
		t.Fatalf("没拿到变更号：%v", row)
	}
	w = kbCtrlDo(t, engine, "GET", "/api/kb-changes?product_id="+product, "")
	if getData(t, w)["total"] != float64(1) {
		t.Errorf("待办该有 1 条：%v", kbCtrlBody(t, w)["data"])
	}

	// 审批还在 pending 时发布：result 为 null、库里一条都没进。
	w = kbCtrlDo(t, engine, "POST", "/api/kb-releases/"+product+"/publish", "")
	afterWait := getData(t, w)
	if afterWait["result"] != nil {
		t.Errorf("审批没批就不该有发布结果：%v", afterWait["result"])
	}
	var chunkN int64
	if err := database.Table("knowledge_chunks").Count(&chunkN).Error; err != nil {
		t.Fatalf("数语料失败: %v", err)
	}
	if chunkN != 0 {
		t.Fatalf("AC①：未批准的变更已经进了语料（%d 条）", chunkN)
	}

	// 直接在库里批了它（裁决入口属于审批域，本条只借它把结论落到 approved）。
	approvalID, _ := row["approval_id"].(string)
	if approvalID == "" {
		t.Fatalf("变更行没挂审批号：%v", row)
	}
	if err := database.Model(&model.ApprovalRequest{}).Where("id = ?", approvalID).
		Update("status", model.ApprovalStatusApproved).Error; err != nil {
		t.Fatalf("置审批结论失败: %v", err)
	}
	w = kbCtrlDo(t, engine, "POST", "/api/kb-releases/"+product+"/publish", "")
	result, _ := getData(t, w)["result"].(map[string]any)
	if result == nil || result["to_version"] != float64(1) {
		t.Fatalf("发布结果不对：%v", getData(t, w)["result"])
	}
	if err := database.Table("knowledge_chunks").Count(&chunkN).Error; err != nil {
		t.Fatalf("数语料失败: %v", err)
	}
	if chunkN != 1 {
		t.Errorf("发布后语料该有 1 条，实得 %d", chunkN)
	}

	// 已发布的撤不掉 ⇒ 409（跃迁表在真链路上生效）。
	w = kbCtrlDo(t, engine, "POST", "/api/kb-changes/"+changeID+"/withdraw", "")
	if w.Code != http.StatusConflict {
		t.Errorf("已发布的变更撤回期望 409，实得 %d：%s", w.Code, w.Body.String())
	}

	// 只发过一版 ⇒ 没有上一版可回 ⇒ 409，而不是 500 或"回到 0"。
	w = kbCtrlDo(t, engine, "POST", "/api/kb-releases/"+product+"/rollback", "")
	if w.Code != http.StatusConflict {
		t.Errorf("无上一版时回滚期望 409，实得 %d：%s", w.Code, w.Body.String())
	}

	// 留痕：这条变更该有两条 —— 提交时的 submitted 与落库时的 published（后者记的是
	// pending → applied@v1，也就是"这条改动确实进过线上"这条历史事实）；
	// 库那一侧该有 governed + published 两条。只数条数不判动作的话，两条同动作的
	// 留痕也能凑够数，而"谁在什么时候把它推到哪一格"恰恰就是动作名给出的。
	w = kbCtrlDo(t, engine, "GET", "/api/kb-changes/"+changeID+"/audit", "")
	audit := getData(t, w)
	changeActions := kbCtrlAuditActions(t, audit)
	if len(changeActions) != 2 || !changeActions["submitted"] || !changeActions["published"] {
		t.Errorf("变更留痕的动作该是 submitted+published，实得 %v（total=%v）", changeActions, audit["total"])
	}
	w = kbCtrlDo(t, engine, "GET", "/api/kb-releases/"+product+"/audit", "")
	releaseAudit := getData(t, w)
	releaseActions := kbCtrlAuditActions(t, releaseAudit)
	if len(releaseActions) != 2 || !releaseActions["governed"] || !releaseActions["published"] {
		t.Errorf("库的留痕该是 governed+published 两条，实得 %v（total=%v）", releaseActions, releaseAudit["total"])
	}

	// 统计口径：闸门 off ⇒ in_force == total（观察期读数），pending 归零。
	w = kbCtrlDo(t, engine, "GET", "/api/kb-releases/"+product+"/stats", "")
	stats := getData(t, w)
	if stats["total"] != float64(1) || stats["in_force"] != float64(1) || stats["pending_changes"] != float64(0) {
		t.Errorf("统计不对：%v", stats)
	}
	if stats["gate_mode"] != "off" || stats["shadow_stats"] != true {
		t.Errorf("档位回显不对：%v", stats)
	}
}
