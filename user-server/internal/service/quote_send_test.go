// quote_send_test.go T-P6-03：报价发送必经审批检查点。
//
// 本文件只测**这一层才成立**的事。三条 AC 各挂在不同的对象上，
// 任何一条被摘掉都不会让别条变红，所以逐条独立成例。
//
//	AC① 「未审批的发送请求 100% 停在 pending」用的是**反向测试**：外发出口是一台
//	     记账替身，判据是它在「没批 / 批了但还没批完 / 被拒 / 已过期」四种态下
//	     计数恒 0。只测「批了会发」那一侧，等于给一条"谁都放行"的实现打分。
//	AC② 「审批阻塞不占用请求线程」= 拿到 pending 时**立刻返回**并带上审批号，
//	     不睡、不轮询、不在进程内挂协程等裁决。
//	AC③ 发送成功回写 sales_events 的 opportunity_id / quote_id —— 断的是**库里那一行**
//	     （口径同 T-P6-02 的合计判据：断内存视图等于断服务自己算的数）。
//
// 另有三条不属于 AC 但同样会死人的判据：结论的**身份比对**（拿别的报价的批准
// 给自己开门）、**认领后外发**的失败方向（外发失败要能退回草稿；退不回去时错误里
// 必须同时装着两件事），以及**话术在发送这一刻重新解析**（T-P6-02 文件头写死的义务）。
package service

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// —— 夹具 ——————————————————————————————————————————————————————————————

// qssSetupDB 在 T-P6-02 那五张表之上再加两张：审批与事件。
// 分库建会把要证的因果链切成两半各测各的（同 setupApprovalTaskLinkDB 的理由）。
func qssSetupDB(t *testing.T) *gorm.DB {
	t.Helper()
	return testutil.NewTestDB(t,
		&model.Opportunity{}, &model.Quote{}, &model.QuoteLineItem{},
		&model.ScriptLibrary{}, &model.ScriptVersion{},
		&model.ApprovalRequest{}, &model.SalesEvent{},
	)
}

// qssReach 外发出口的记账替身。**唯一**能证明 AC① 的形态：
// 判据是它被叫了几次、带着什么，而不是"代码里有没有那个调用"。
type qssReach struct {
	calls int
	reqs  []*ProactiveReachRequest
	err   error
}

func (r *qssReach) ReachByCustomer(_ context.Context, req *ProactiveReachRequest) (*ProactiveReachResponse, error) {
	r.calls++
	r.reqs = append(r.reqs, req)
	if r.err != nil {
		return nil, r.err
	}
	return &ProactiveReachResponse{MessageID: "msg_qss_1", Channel: "sms", Status: "sent"}, nil
}

// qssEvents 销售事件的**记账代理**：底下垫真仓储。
//
// 为什么不能是纯替身：AC③ 的判据是"库里那一行带着 opportunity_id / quote_id"，
// 纯替身只能证明"服务打算写什么"（那等于让服务自己给自己出题）。
// 为什么不能全是真仓储："事件写失败不许把已发出的外发说成没发出"这一格在真库里
// 造不出只失败一次的写。所以只加一个失败开关，落库仍然走真路。
type qssEvents struct {
	repo repository.SalesEventRepository
	rows []*model.SalesEvent
	err  error
}

func (e *qssEvents) Create(ctx context.Context, ev *model.SalesEvent) error {
	if e.err != nil {
		return e.err
	}
	if err := e.repo.Create(ctx, ev); err != nil {
		return err
	}
	e.rows = append(e.rows, ev)
	return nil
}

// qssStore 包住真仓储，只换 UpdateStatus 的两格行为：
// failAll = 这次跃迁根本写不进去（并发对手先认领 / 库抖动）；
// failToDraft = 只有"退回草稿"那一次失败（外发失败后连回滚也失败）。
// updateCalls 数的是**任何一次**状态跃迁的尝试：判据②（完整性判据在认领之前）
// 只能靠"一次都没动过状态列"来证，看库里那一行还是 draft 证不到（没人改也算 draft）。
type qssStore struct {
	quoteSendStore
	failAll     bool
	failToDraft bool
	updateCalls int
}

func (s *qssStore) UpdateStatus(ctx context.Context, id, from, to string) error {
	s.updateCalls++
	if s.failAll || (s.failToDraft && to == model.QuoteStatusDraft) {
		return errors.New("boom: 状态写不进去")
	}
	return s.quoteSendStore.UpdateStatus(ctx, id, from, to)
}

// qssScripts 话术端口的探针：在 qsScripts 那一份替身之外只加一件事 ——
// 数它被问了几次。「发送时重新解析」这条判据没有计数就证不出来。
type qssScripts struct {
	inner qsScripts
	calls int
}

func (p *qssScripts) ActiveQuoteScript(ctx context.Context, id uint, oneID string) (QuoteScript, error) {
	p.calls++
	return p.inner.ActiveQuoteScript(ctx, id, oneID)
}

// qssPolicy auto-approve 快速路径的判据（C2 的同步退化态）。
type qssPolicy struct {
	allow  bool
	reason string
}

func (p qssPolicy) AutoApproves(context.Context, ApprovalSubmitInput) (bool, string) {
	return p.allow, p.reason
}

// qssReader 「这一版报价当前开着的那条待办」的读口。
// GetPendingBySubject 只在仓储上（审批服务刻意没有这一格，见 quote_send.go 文件头）。
type qssReader struct {
	repo repository.ApprovalRequestRepository
}

func (r qssReader) GetPendingBySubject(ctx context.Context, st, sid, pk string) (*model.ApprovalRequest, error) {
	return r.repo.GetPendingBySubject(ctx, st, sid, pk)
}

// qssParts 一次装配的全部零件，便于用例只换坏的那一格。
type qssParts struct {
	svc     *QuoteSendService
	appro   *ApprovalRequestService
	reach   *qssReach
	events  *qssEvents
	scripts *qssScripts
	store   *qssStore
}

// qssSvc 装一台"什么都能干"的发送服务：真库 + 真审批竖（policy=nil ⇒ 永不自动放行，
// 最保守的那一档）+ 记账外发与事件替身。
//
// 审批用**真服务**而不是替身：AC① 说的是"停在 pending"，那是 approval_requests 表里
// 一行的状态；用替身的话，那句判据就成了替身自己答应不答应。
func qssSvc(t *testing.T, db *gorm.DB) *qssParts {
	t.Helper()
	return qssSvcWithPolicy(t, db, nil)
}

func qssSvcWithPolicy(t *testing.T, db *gorm.DB, policy AutoApprovalPolicy) *qssParts {
	t.Helper()
	cfg := qsConfig{QuoteScriptIDKVKey: "77"}
	scripts := &qssScripts{inner: qsScripts{script: QuoteScript{
		ScriptID: 77, Version: 3, Bucket: "A", Content: "这是第三版生效话术"}}}
	appro := NewApprovalRequestService(repository.NewApprovalRequestRepositoryWithDB(db), policy)
	store := &qssStore{quoteSendStore: repository.NewQuoteRepositoryWithDB(db)}
	opps := repository.NewOpportunityRepositoryWithDB(db)
	reach := &qssReach{}
	events := &qssEvents{repo: repository.NewSalesEventRepositoryWithDB(db)}
	reader := qssReader{repo: repository.NewApprovalRequestRepositoryWithDB(db)}

	svc := NewQuoteSendService(store, opps, appro, reader, reach, events, cfg, scripts, qsGate{qsGateOn()})
	svc.SetClock(func() time.Time { return qsClockBase })
	return &qssParts{svc: svc, appro: appro, reach: reach, events: events, scripts: scripts, store: store}
}

// qssDraftSeq 保证每次造的草稿挂在**不同**的商机上：qssSeedOpportunity 用同一个号
// 插第二次会撞主键，而"一条链上两版"与"两条链各一版"是两回事，不能靠复用蒙过去。
var qssDraftSeq int

// qssSeedOpp 造发送腿要能解析出收件人的那条商机。
// oneID 留空 = 这条商机还不知道是谁（线索没落 OneID 的日常形状），发送腿必须因此拒发。
func qssSeedOpp(t *testing.T, db *gorm.DB, id, oneID string) {
	t.Helper()
	repo := repository.NewOpportunityRepositoryWithDB(db)
	row := &model.Opportunity{
		ID: id, Code: "OPP-QSS-" + id, CustomerID: "cus_qss", OneID: oneID,
		Stage: model.OpportunityStageProposal, Status: model.OpportunityStatusOpen,
		Amount: 1000, Currency: model.OpportunityCurrencyDefault, OwnerUserID: "sales_a",
		CreatedAt: qsClockBase, UpdatedAt: qsClockBase,
	}
	if err := repo.Insert(context.Background(), row); err != nil {
		t.Fatalf("造商机行失败：%v", err)
	}
}

// qssDraft 经 T-P6-02 那条真路造一版草稿，返回版本行主键。
// 用它而不是手插一行：发送腿的输入必须与生成腿的产物**逐字对得上**，
// 手插的行会替两边各自兜错（模板列名漂了照样绿）。
func qssDraft(t *testing.T, db *gorm.DB) string {
	t.Helper()
	qssDraftSeq++
	return qssDraftOn(t, db, fmt.Sprintf("opp_send_%d", qssDraftSeq), "one_1")
}

// qssDraftOn 在指定商机上造一版草稿（收件人身份跟着那条商机走）。
func qssDraftOn(t *testing.T, db *gorm.DB, oppID, oneID string) string {
	t.Helper()
	qssSeedOpp(t, db, oppID, oneID)
	gen, cfg, _ := qsSvc(t, db)
	cfg[qsTemplateKey] = qsTemplateJSON
	view, err := gen.Generate(context.Background(), QuoteGenerateInput{
		OpportunityID: oppID, OneID: oneID, TemplateCode: "std_annual",
	})
	if err != nil {
		t.Fatalf("造草稿失败：%v", err)
	}
	if view.Status != model.QuoteStatusDraft {
		t.Fatalf("生成腿交出来的不是草稿：%s", view.Status)
	}
	return view.ID
}

func qssSendInput(rowID string) QuoteSendInput {
	return QuoteSendInput{QuoteRowID: rowID, Operator: "sales_a"}
}

func qssSendWith(rowID, approvalID string) QuoteSendInput {
	return QuoteSendInput{QuoteRowID: rowID, ApprovalID: approvalID, Operator: "sales_a"}
}

// qssMustSend 走"未批先点发送"那一步，并把错误与 nil 结果都判掉。
//
// 这里原先写作 `first, _ := p.svc.Send(...)`：一旦那一步返回 nil（把 pending 那一档改成
// 立刻外发的那一刀就会 —— 空明细、无收件人之类的判据先把它拦下），下一行的
// `first.ApprovalID` 就是空指针解引用，而 **panic 会带走整个测试二进制**：后面几条用例
// 一条都不报，电池于是把"跑断"读成"这一刀杀掉了"。Fatal 只红这一条用例，其余照跑。
func qssMustSend(t *testing.T, p *qssParts, ctx context.Context, in QuoteSendInput) *QuoteSendResult {
	t.Helper()
	res, err := p.svc.Send(ctx, in)
	if err != nil {
		t.Fatalf("第一次发送（本应只开一条待办）失败：%v", err)
	}
	if res == nil {
		t.Fatalf("第一次发送返回 nil 结果：没有审批号可续，后面的判据无从谈起")
	}
	return res
}

// qssApprove 人工批掉这条审批（走真 Decide 路，不直改库：直改会绕开裁决者判据，
// 而"批了"这件事的权威就是那一行的 status）。
func qssApprove(t *testing.T, p *qssParts, approvalID string) {
	t.Helper()
	if _, err := p.appro.Decide(context.Background(), approvalID, ApprovalApprove, "boss_a", "可以发"); err != nil {
		t.Fatalf("裁决失败：%v", err)
	}
}

func qssApprovalRow(t *testing.T, db *gorm.DB, id string) *model.ApprovalRequest {
	t.Helper()
	var row model.ApprovalRequest
	if err := db.WithContext(context.Background()).First(&row, "id = ?", id).Error; err != nil {
		t.Fatalf("按 %s 读审批行失败：%v", id, err)
	}
	return &row
}

func qssStatus(t *testing.T, db *gorm.DB, rowID string) string {
	t.Helper()
	return qsRow(t, db, rowID).Status
}

func qssCountApprovals(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var n int64
	if err := db.WithContext(context.Background()).Model(&model.ApprovalRequest{}).Count(&n).Error; err != nil {
		t.Fatalf("数审批行失败：%v", err)
	}
	return n
}

// —— 反射白名单（本文件自带的三个小探针）——————————————————————————————————

func qssMethodNames(ifacePtr any) map[string]int {
	out := map[string]int{}
	typ := reflect.TypeOf(ifacePtr).Elem()
	for i := 0; i < typ.NumMethod(); i++ {
		out[typ.Method(i).Name]++
	}
	return out
}

func qssFieldNames(structPtr any) map[string]int {
	out := map[string]int{}
	typ := reflect.TypeOf(structPtr)
	for i := 0; i < typ.NumField(); i++ {
		out[typ.Field(i).Name]++
	}
	return out
}

func qssFieldTypes(structPtr any) []string {
	typ := reflect.TypeOf(structPtr)
	out := make([]string, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		out = append(out, typ.Field(i).Type.String())
	}
	return out
}

// —— AC①：未审批的发送请求 100% 停在 pending ——————————————————————————————

// TestQuoteSend_AwaitsApprovalAndSendsNothing 第一次点发送只换来一条待办。
//
// 三条断言各管一件事：库里那行 pending（闸门真的落了表）、行仍是 draft
// （没有"先发后补审批"）、外发计数 0（客户没收到东西）。
func TestQuoteSend_AwaitsApprovalAndSendsNothing(t *testing.T) {
	db := qssSetupDB(t)
	rowID := qssDraft(t, db)
	p := qssSvc(t, db)

	res, err := p.svc.Send(context.Background(), qssSendInput(rowID))
	if err != nil {
		t.Fatalf("Send 失败：%v", err)
	}
	if res.Disposition != QuoteSendAwaiting {
		t.Errorf("处置=%q，期望 %q", res.Disposition, QuoteSendAwaiting)
	}
	if res.ApprovalID == "" {
		t.Error("没把审批号交给调用方 ⇒ 批完之后没人能把它续上（AC② 要求不阻塞，那就必须留一个回来找路的凭据）")
	}
	if p.reach.calls != 0 {
		t.Errorf("外发被调了 %d 次，期望 0", p.reach.calls)
	}
	if got := qssStatus(t, db, rowID); got != model.QuoteStatusDraft {
		t.Errorf("库里状态=%s，期望 %s", got, model.QuoteStatusDraft)
	}
	appr := qssApprovalRow(t, db, res.ApprovalID)
	if appr.Status != model.ApprovalStatusPending {
		t.Errorf("审批状态=%s，期望 pending", appr.Status)
	}
	if appr.ExpiresAt == nil {
		t.Error("pending 审批没有截止时刻 ⇒ 它会永远挂着，清扫也扫不到它")
	}
}

// TestQuoteSend_NeverReachesCustomerWithoutAnApprovedVerdict AC① 的**反向测试**。
//
// 四种"没有有效批准"的态各走一遍，判据只有一个：外发出口计数恒 0、库里状态恒 draft。
// 这一条是本卡唯一能证伪"闸门形同虚设"的用例族 —— 把 pending 分支改成"那就先发"，
// 它红；把身份比对摘掉，它不红（那是另两格，见 Mismatch 两例），所以格子分开。
func TestQuoteSend_NeverReachesCustomerWithoutAnApprovedVerdict(t *testing.T) {
	ctx := context.Background()

	t.Run("递一个查无此号", func(t *testing.T) {
		db := qssSetupDB(t)
		rowID := qssDraft(t, db)
		p := qssSvc(t, db)
		res, err := p.svc.Send(ctx, qssSendWith(rowID, "apr_not_exist"))
		if !errors.Is(err, ErrApprovalNotFound) {
			t.Fatalf("查无此号应报明确的错，实际 %v", err)
		}
		if res != nil {
			t.Errorf("查无此号却拿到了结果：%+v", res)
		}
		if p.reach.calls != 0 {
			t.Errorf("外发计数=%d，期望 0", p.reach.calls)
		}
	})

	t.Run("审批还挂着等人", func(t *testing.T) {
		db := qssSetupDB(t)
		rowID := qssDraft(t, db)
		p := qssSvc(t, db)
		first, err := p.svc.Send(ctx, qssSendInput(rowID))
		if err != nil {
			t.Fatalf("首次 Send 失败：%v", err)
		}
		again, err := p.svc.Send(ctx, qssSendWith(rowID, first.ApprovalID))
		if err != nil {
			t.Fatalf("带 pending 号回来应正常返回，实际 %v", err)
		}
		if again.Disposition != QuoteSendAwaiting {
			t.Errorf("处置=%q，期望仍 %q", again.Disposition, QuoteSendAwaiting)
		}
		if p.reach.calls != 0 {
			t.Errorf("外发计数=%d，期望 0", p.reach.calls)
		}
		if got := qssStatus(t, db, rowID); got != model.QuoteStatusDraft {
			t.Errorf("等裁决期间报价状态被推进了：%s", got)
		}
	})

	t.Run("审批被人拒了", func(t *testing.T) {
		db := qssSetupDB(t)
		rowID := qssDraft(t, db)
		p := qssSvc(t, db)
		first := qssMustSend(t, p, ctx, qssSendInput(rowID))
		if _, err := p.appro.Decide(ctx, first.ApprovalID, ApprovalReject, "boss_a", "价不能这么报"); err != nil {
			t.Fatalf("拒绝失败：%v", err)
		}
		res, err := p.svc.Send(ctx, qssSendWith(rowID, first.ApprovalID))
		if err != nil {
			t.Fatalf("被拒不是一种故障，应带处置返回：%v", err)
		}
		if res.Disposition != QuoteSendRejected {
			t.Errorf("处置=%q，期望 %q", res.Disposition, QuoteSendRejected)
		}
		if p.reach.calls != 0 {
			t.Errorf("外发计数=%d，期望 0 —— 被拒之后还发出去，审批门就只是装饰", p.reach.calls)
		}
	})

	t.Run("审批到期了", func(t *testing.T) {
		db := qssSetupDB(t)
		rowID := qssDraft(t, db)
		p := qssSvc(t, db)
		freezeApprovalClock(t, qsClockBase)
		first := qssMustSend(t, p, ctx, qssSendInput(rowID))
		// 造的是**清扫那一路**的到期（把 expires_at 挪到过去再跑 ExpireOverdue），
		// 而不是直改 status：直改会连"到期是清扫落的终态"这条前提一起替掉。
		if err := db.Model(&model.ApprovalRequest{}).Where("id = ?", first.ApprovalID).
			Update("expires_at", qsClockBase.Add(-time.Hour)).Error; err != nil {
			t.Fatalf("挪到期时刻失败：%v", err)
		}
		flipped, err := p.appro.ExpireOverdue(ctx, 10)
		if err != nil {
			t.Fatalf("清扫失败：%v", err)
		}
		if len(flipped) != 1 {
			t.Fatalf("清扫翻转了 %d 行，期望 1", len(flipped))
		}
		res, err := p.svc.Send(ctx, qssSendWith(rowID, first.ApprovalID))
		if err != nil {
			t.Fatalf("过期不是故障：%v", err)
		}
		if res.Disposition != QuoteSendExpired {
			t.Errorf("处置=%q，期望 %q", res.Disposition, QuoteSendExpired)
		}
		if p.reach.calls != 0 {
			t.Errorf("外发计数=%d，期望 0", p.reach.calls)
		}
	})
}

// TestQuoteSend_WindowClosedButNotYetSweptCountsAsExpired 窗口过了但没人清扫 ⇒ 也算到期。
//
// 这一格守的是本卡装配口径留下的一条缝：报价竖的审批服务在 `FF_LTC_APPROVAL_RESUME=off`
// 时也能开待办（不然销售的"发送"按钮点了没地方去），而那一档下**没有清扫协程在跑**，
// 过期的 pending 会一直停在 pending。只看库里的 status，操作者每次点发送都拿到 awaiting，
// 那条待办永远挂着 —— "停在 pending"是事实，但"还要继续等"不是。
//
// 判据与仓储那次清扫同源（`expires_at <= now`，见 repository/approval_request.go:262），
// 两边各持一套比较口径就会出现"清扫认为过期、发送认为还开着"的第二天早上。
// 边界取"恰好等于"也算关掉：与 SQL 的 <= 一致，差一个等号就是两层判据分家。
//
// 两个方向刻意**不**判，理由是同一族：
//   - 批完隔了很久才来发 ⇒ 照发。窗口约束的是"等多久要不到裁决就算数"，
//     不是"这条批准几小时内有效"；把人的裁决按钟点作废，等于发送腿在改写别人的结论。
//   - 窗口关掉之后别人才批上的那条 ⇒ 也照发（同上一条）。真要把"过期即不可再批"做严，
//     正解是让清扫器把那一行落成终态（开审批运行时），或给 Decide 加窗口判据 ——
//     后者是 T-P3-01 的定义侧，本卡不动（不动的理由见文件头"为什么不加读口"那一段）。
//     这一条残余登记为移交项，不假装本层已经关掉它。
func TestQuoteSend_WindowClosedButNotYetSweptCountsAsExpired(t *testing.T) {
	ctx := context.Background()

	for _, c := range []struct {
		name string
		// 相对冻结时钟（qsClockBase）的位移：负 = 窗口早关掉，0 = 恰好压在边界上。
		shift time.Duration
	}{
		{"窗口关掉一小时", -time.Hour},
		{"恰好压在到期时刻上", 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			db := qssSetupDB(t)
			rowID := qssDraft(t, db)
			p := qssSvc(t, db)
			freezeApprovalClock(t, qsClockBase)

			first, err := p.svc.Send(ctx, qssSendInput(rowID))
			if err != nil {
				t.Fatalf("首次 Send 失败：%v", err)
			}
			if first.Disposition != QuoteSendAwaiting {
				t.Fatalf("第一次的处置=%q，期望 awaiting", first.Disposition)
			}
			// 只挪到期时刻，**不跑清扫**（不跑清扫 = 审批运行时 off 的那个部署）。
			if err := db.Model(&model.ApprovalRequest{}).Where("id = ?", first.ApprovalID).
				Update("expires_at", qsClockBase.Add(c.shift)).Error; err != nil {
				t.Fatalf("挪到期时刻失败：%v", err)
			}

			res, err := p.svc.Send(ctx, qssSendWith(rowID, first.ApprovalID))
			if err != nil {
				t.Fatalf("到期不是故障：%v", err)
			}
			if res.Disposition != QuoteSendExpired {
				t.Errorf("处置=%q，期望 %q（库里那一行还写着 pending，但那句话已经过期了）",
					res.Disposition, QuoteSendExpired)
			}
			if p.reach.calls != 0 {
				t.Errorf("外发计数=%d，期望 0", p.reach.calls)
			}
			if got := qssStatus(t, db, rowID); got != model.QuoteStatusDraft {
				t.Errorf("库里状态=%s，期望 %s", got, model.QuoteStatusDraft)
			}
			// status 由清扫那一方落终态，本层只读不写：这里必须还是 pending，
			// 否则发送腿就成了第二个改审批状态的地方（裁决入口不在本层的接缝里，正是为此）。
			if appr := qssApprovalRow(t, db, first.ApprovalID); appr.Status != model.ApprovalStatusPending {
				t.Errorf("发送腿把审批行改成了 %s，期望本层只读", appr.Status)
			}
		})
	}

	// 没有窗口的那一条（expires_at 为 NULL）不能被判成"早就过期"。
	// 它是 Submit 没带 TTL 时的形状；把它算成过期等于让"无限期等待"那条路永远走不通。
	t.Run("没有到期时刻的待办照旧算在等", func(t *testing.T) {
		db := qssSetupDB(t)
		rowID := qssDraft(t, db)
		p := qssSvc(t, db)
		first := qssMustSend(t, p, ctx, qssSendInput(rowID))
		if err := db.Model(&model.ApprovalRequest{}).Where("id = ?", first.ApprovalID).
			Update("expires_at", nil).Error; err != nil {
			t.Fatalf("清空到期时刻失败：%v", err)
		}
		res, err := p.svc.Send(ctx, qssSendWith(rowID, first.ApprovalID))
		if err != nil {
			t.Fatalf("Send 失败：%v", err)
		}
		if res.Disposition != QuoteSendAwaiting {
			t.Errorf("处置=%q，期望 %q", res.Disposition, QuoteSendAwaiting)
		}
	})
}

// TestQuoteSend_RepeatSendReusesThePendingApproval 连点两次发送只有一条待办。
//
// 反面是"每次点击建一行 pending"：待办中心出现两条同样的事，两个审批人各批一条，
// 而一次批准只放行一次动作 ⇒ 另一条被批了也发不出去，批的人以为自己批成功了。
func TestQuoteSend_RepeatSendReusesThePendingApproval(t *testing.T) {
	db := qssSetupDB(t)
	rowID := qssDraft(t, db)
	p := qssSvc(t, db)
	ctx := context.Background()

	first := qssMustSend(t, p, ctx, qssSendInput(rowID))
	second, err := p.svc.Send(ctx, qssSendInput(rowID))
	if err != nil {
		t.Fatalf("第二次 Send 失败：%v", err)
	}
	if second.ApprovalID != first.ApprovalID {
		t.Errorf("两次拿到不同审批号：%s / %s", second.ApprovalID, first.ApprovalID)
	}
	if n := qssCountApprovals(t, db); n != 1 {
		t.Errorf("库里有 %d 条审批，期望 1", n)
	}
}

// TestQuoteSend_SendAfterDecisionOpensANewApprovalInsteadOfReusingTheVerdict
// 把"丢了审批号"的后果钉成一句话：**只会更严，不会更松**。
//
// 批完却没带号回来 ⇒ 服务不去猜"上次是不是批过"（审批仓储没有"按 subject 读最新已裁决"
// 那格读口，猜就等于放行），而是新开一条 pending。代价是一次多余的待办；
// 反面是"任何一次没带号的调用都能沿用别人的结论"。
func TestQuoteSend_SendAfterDecisionOpensANewApprovalInsteadOfReusingTheVerdict(t *testing.T) {
	db := qssSetupDB(t)
	rowID := qssDraft(t, db)
	p := qssSvc(t, db)
	ctx := context.Background()

	first := qssMustSend(t, p, ctx, qssSendInput(rowID))
	qssApprove(t, p, first.ApprovalID)

	second, err := p.svc.Send(ctx, qssSendInput(rowID))
	if err != nil {
		t.Fatalf("第二次 Send 失败：%v", err)
	}
	if second.ApprovalID == first.ApprovalID || second.Disposition != QuoteSendAwaiting {
		t.Errorf("应新开一条待办，实际 %+v", second)
	}
	if p.reach.calls != 0 {
		t.Errorf("外发计数=%d，期望 0", p.reach.calls)
	}
	if n := qssCountApprovals(t, db); n != 2 {
		t.Errorf("库里有 %d 条审批，期望 2（第一条已 approved、第二条新 pending）", n)
	}
}

// —— AC③ + 派发腿 ————————————————————————————————————————————————

// TestQuoteSend_ApprovedApprovalDispatchesOnce 批了才发，且只发一次。
//
// 载荷断四件事：正文来自**重新解析**的生效话术、合计从库里那些行算出、
// 收件人走身份而不是显式手机号（显式收件人在外发服务里绕过冷却判据，
// 判据来源见 sop_reach_send.go 文件头第 29-31 行），以及凭证不外泄。
func TestQuoteSend_ApprovedApprovalDispatchesOnce(t *testing.T) {
	db := qssSetupDB(t)
	rowID := qssDraft(t, db)
	p := qssSvc(t, db)
	ctx := context.Background()

	first := qssMustSend(t, p, ctx, qssSendInput(rowID))
	qssApprove(t, p, first.ApprovalID)

	res, err := p.svc.Send(ctx, qssSendWith(rowID, first.ApprovalID))
	if err != nil {
		t.Fatalf("批准后 Send 失败：%v", err)
	}
	if res.Disposition != QuoteSendSent {
		t.Errorf("处置=%q，期望 %q", res.Disposition, QuoteSendSent)
	}
	if p.reach.calls != 1 {
		t.Fatalf("外发计数=%d，期望 1", p.reach.calls)
	}
	req := p.reach.reqs[0]
	if !strings.Contains(req.Content, "这是第三版生效话术") {
		t.Errorf("正文没带上生效话术：%q", req.Content)
	}
	// 模板两行的库侧合计：199×10 折 10% = 1791.00、8000×1 折 5% = 7600.00。
	if !strings.Contains(req.Content, "9391.00") {
		t.Errorf("正文没带上从库里算出的合计：%q", req.Content)
	}
	if !strings.Contains(req.Content, rowID[:2]) || !strings.Contains(req.Content, "第 1 版") {
		t.Errorf("正文没带上是哪一版：%q", req.Content)
	}
	if req.Phone != "" || req.Email != "" {
		t.Errorf("填了显式收件人（phone=%q email=%q）⇒ 会绕过外发服务的冷却判据", req.Phone, req.Email)
	}
	if req.OneID != "one_1" {
		t.Errorf("OneID=%q，期望 one_1", req.OneID)
	}
	if strings.Contains(req.Content, "rt_") {
		t.Errorf("正文里出现了恢复凭证：%q", req.Content)
	}
	if got := qssStatus(t, db, rowID); got != model.QuoteStatusSent {
		t.Errorf("库里状态=%s，期望 %s", got, model.QuoteStatusSent)
	}
	if res.SentAt == nil {
		t.Error("sent 结果没有时刻")
	}
	if res.Status != model.QuoteStatusSent {
		t.Errorf("结果里的状态=%q，期望 sent", res.Status)
	}
}

// TestQuoteSend_RecipientComesFromTheQuotedOpportunity 收件人是**查出来的事实**，不是入参。
//
// 反面的形状很具体：入参里能递 OneID ⇒ 操作者可以拿"给甲批的那一条批准"把报价发给乙。
// 批准的对象是"这一版可以出域"，它管不到"出给谁"，所以"给谁"只能由库里那条链回答：
// 版本行 → opportunity_id → opportunities.one_id。
// 话术分桶也跟着这个身份走（不是跟着点发送的人）：同一版报价给不同身份的客户，
// 生效话术本就是该按收件人分桶的，而按操作者分桶会让 AC① 那条"重新解析"解析错对象。
func TestQuoteSend_RecipientComesFromTheQuotedOpportunity(t *testing.T) {
	db := qssSetupDB(t)
	rowID := qssDraftOn(t, db, "opp_recv", "one_zhang")
	p := qssSvc(t, db)
	ctx := context.Background()

	first, err := p.svc.Send(ctx, QuoteSendInput{QuoteRowID: rowID, Operator: "sales_a"})
	if err != nil {
		t.Fatalf("首次 Send 失败：%v", err)
	}
	qssApprove(t, p, first.ApprovalID)
	if _, err := p.svc.Send(ctx, qssSendWith(rowID, first.ApprovalID)); err != nil {
		t.Fatalf("批准后 Send 失败：%v", err)
	}
	if p.reach.calls != 1 {
		t.Fatalf("外发计数=%d，期望 1", p.reach.calls)
	}
	if got := p.reach.reqs[0].OneID; got != "one_zhang" {
		t.Errorf("收件人=%q，期望从那条商机解析出的 one_zhang", got)
	}
	if got := p.scripts.inner.gotOneID; got != "one_zhang" {
		t.Errorf("话术按 %q 分桶，期望按收件人 one_zhang（按操作者分桶会把话术发给错的人）", got)
	}
}

// TestQuoteSend_QuoteWithoutACustomerIdentityIsNotSendable 解析不出发给谁 ⇒ 一列都不写。
//
// 判点位置是这条用例的全部意义：收件人解析排在**认领之前**（顺序判据②），
// 所以库里那一版必须还停在 draft、不需要任何回滚。把解析挪到认领之后，
// 这里就会红在"状态是 sent 而外发计数是 0"上 —— 那正是"库里说发过、客户没收到"的形状。
func TestQuoteSend_QuoteWithoutACustomerIdentityIsNotSendable(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name  string
		oneID string
		drop  bool
	}{
		{"商机没有 OneID", "", false},
		{"商机的 OneID 是空白", "   ", false},
		{"商机行已经不在了", "one_gone", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			db := qssSetupDB(t)
			qssDraftSeq++
			oppID := fmt.Sprintf("opp_noaddr_%d", qssDraftSeq)
			rowID := qssDraftOn(t, db, oppID, c.oneID)
			p := qssSvc(t, db)

			first, err := p.svc.Send(ctx, qssSendInput(rowID))
			if err != nil {
				t.Fatalf("首次 Send 失败：%v", err)
			}
			qssApprove(t, p, first.ApprovalID)
			if c.drop {
				if err := db.Unscoped().Where("id = ?", oppID).Delete(&model.Opportunity{}).Error; err != nil {
					t.Fatalf("删商机行失败：%v", err)
				}
			}
			res, err := p.svc.Send(ctx, qssSendWith(rowID, first.ApprovalID))
			if !errors.Is(err, ErrQuoteSendRecipientMissing) {
				t.Fatalf("应报 %v，实际 %v", ErrQuoteSendRecipientMissing, err)
			}
			if res != nil {
				t.Errorf("解析不到收件人却拿到了结果：%+v", res)
			}
			if p.reach.calls != 0 {
				t.Errorf("外发计数=%d，期望 0", p.reach.calls)
			}
			if got := qssStatus(t, db, rowID); got != model.QuoteStatusDraft {
				t.Errorf("库里状态=%s，期望 %s（收件人解析必须在认领之前）", got, model.QuoteStatusDraft)
			}
			if p.store.updateCalls != 0 {
				t.Errorf("状态跃迁被调了 %d 次，期望 0（没有收件人就不该先抢这一格）", p.store.updateCalls)
			}
		})
	}
}

// TestQuoteSend_SuccessWritesSalesEventColumns AC③：发送成功回写两列。
//
// 读回库里那一行而不是替身里那份：替身记录到的是"服务打算写什么"，
// 而这两列的存在理由正是"事后能从事件流里查出这次发的是谁的哪一版"。
func TestQuoteSend_SuccessWritesSalesEventColumns(t *testing.T) {
	db := qssSetupDB(t)
	rowID := qssDraft(t, db)
	p := qssSvc(t, db)
	ctx := context.Background()

	first := qssMustSend(t, p, ctx, qssSendInput(rowID))
	qssApprove(t, p, first.ApprovalID)
	if _, err := p.svc.Send(ctx, qssSendWith(rowID, first.ApprovalID)); err != nil {
		t.Fatalf("Send 失败：%v", err)
	}

	var ev model.SalesEvent
	if err := db.WithContext(ctx).First(&ev, "quote_id <> ''").Error; err != nil {
		t.Fatalf("读回销售事件失败（一条都没写）：%v", err)
	}
	head := qsRow(t, db, rowID)
	if ev.OpportunityID != head.OpportunityID {
		t.Errorf("opportunity_id=%q，期望 %q", ev.OpportunityID, head.OpportunityID)
	}
	if ev.QuoteID != head.QuoteID {
		t.Errorf("quote_id=%q，期望 %q", ev.QuoteID, head.QuoteID)
	}
	if ev.EventType != model.SalesEventTypeQuote {
		t.Errorf("event_type=%q，期望 %q", ev.EventType, model.SalesEventTypeQuote)
	}
	if ev.OwnerID != "sales_a" {
		t.Errorf("owner_id=%q，期望 sales_a（谁点的发送必须留得下来）", ev.OwnerID)
	}
	if ev.ID == 0 {
		t.Error("事件行没有主键")
	}
	if ev.OccurredAt.IsZero() {
		t.Error("事件行没有发生时刻")
	}
	// 两列的宽度是下游定的（sales_events.varchar(64)，见 model/sales_event.go 注释 a/b/c）。
	if len(ev.QuoteID) > 64 || len(ev.OpportunityID) > 64 {
		t.Errorf("两列超出 varchar(64)：%q / %q", ev.QuoteID, ev.OpportunityID)
	}
}

// TestQuoteSend_EventWriteFailureDoesNotUndoTheSend 事件写失败只能说"审计缺一条"。
//
// 报价已经到了客户手上，把它报成"发送失败"会让操作者再点一次 ⇒ 同一个人收到两遍。
// 反面（悄悄吞掉）更坏：AC③ 那句话在事故现场是假的。
func TestQuoteSend_EventWriteFailureDoesNotUndoTheSend(t *testing.T) {
	db := qssSetupDB(t)
	rowID := qssDraft(t, db)
	p := qssSvc(t, db)
	ctx := context.Background()

	first := qssMustSend(t, p, ctx, qssSendInput(rowID))
	qssApprove(t, p, first.ApprovalID)
	p.events.err = errors.New("connection reset by peer")

	res, err := p.svc.Send(ctx, qssSendWith(rowID, first.ApprovalID))
	if err != nil {
		t.Fatalf("事件写失败不该把已发出的外发说成失败：%v", err)
	}
	if res.Disposition != QuoteSendSent || res.EventRecorded {
		t.Errorf("结果=%+v，期望 sent 且 event_recorded=false", res)
	}
	if got := qssStatus(t, db, rowID); got != model.QuoteStatusSent {
		t.Error("事件写失败把报价状态退回了草稿")
	}
}

// —— 结论的身份 ————————————————————————————————————————————————

// TestQuoteSend_ApprovalForAnotherQuoteIsRefused 拿甲的批准给乙开门，拒。
//
// 审批侧把 subject 三列排除在裁决写白名单之外（approvalWriteColumns 的注释点名了这件事），
// 但那只挡住"改一条审批"。"递一条**别的**审批"要由消费侧比三样东西才拦得住：
// subject_type、subject_id、policy_key。少比任何一样，一张已批准的报价就能给
// 另一张报价（或另一道策略）放行。
func TestQuoteSend_ApprovalForAnotherQuoteIsRefused(t *testing.T) {
	db := qssSetupDB(t)
	rowA := qssDraft(t, db)
	rowB := qssDraft(t, db)
	p := qssSvc(t, db)
	ctx := context.Background()

	a := qssMustSend(t, p, ctx, qssSendInput(rowA))
	qssApprove(t, p, a.ApprovalID)

	_, err := p.svc.Send(ctx, qssSendWith(rowB, a.ApprovalID))
	if !errors.Is(err, ErrQuoteSendApprovalMismatch) {
		t.Fatalf("跨报价用批准应报 %v，实际 %v", ErrQuoteSendApprovalMismatch, err)
	}
	if p.reach.calls != 0 {
		t.Errorf("外发计数=%d，期望 0", p.reach.calls)
	}
	if got := qssStatus(t, db, rowB); got != model.QuoteStatusDraft {
		t.Errorf("被拒的乙状态动了：%s", got)
	}
}

// TestQuoteSend_ApprovalWithAnotherPolicyKeyIsRefused 同一张报价、另一道策略的批准也不行。
//
// 这一格与上一格必须分开：subject_id 相同时，只比 type+id 的实现**恰好**放过这一刀 ——
// 而 T-P6-04 的高折扣档就是同一 subject 上的第二个 policy_key。
// 合起来测的话，那一卡接上来的第一天就是静默放行。
func TestQuoteSend_ApprovalWithAnotherPolicyKeyIsRefused(t *testing.T) {
	db := qssSetupDB(t)
	rowID := qssDraft(t, db)
	p := qssSvc(t, db)
	ctx := context.Background()

	other, created, err := p.appro.Submit(ctx, ApprovalSubmitInput{
		SubjectType: QuoteApprovalSubjectType, SubjectID: rowID, PolicyKey: "quote.discount_high",
	})
	if err != nil || !created {
		t.Fatalf("造另一道策略的审批失败：created=%v err=%v", created, err)
	}
	if _, err := p.appro.Decide(ctx, other.ID, ApprovalApprove, "boss_a", "折扣可以"); err != nil {
		t.Fatalf("裁决失败：%v", err)
	}

	_, err = p.svc.Send(ctx, qssSendWith(rowID, other.ID))
	if !errors.Is(err, ErrQuoteSendApprovalMismatch) {
		t.Fatalf("跨策略用批准应报 %v，实际 %v", ErrQuoteSendApprovalMismatch, err)
	}
	if p.reach.calls != 0 {
		t.Errorf("外发计数=%d，期望 0", p.reach.calls)
	}
}

// TestQuoteSend_ApprovalWithAnotherSubjectTypeIsRefused 同一行、同一道策略，但那是**别的对象类型**的批准。
//
// 这一格是补 S06 那一刀的：把三列核对里的 subject_type 那一臂单独摘掉，另两列**恰好**都对得上
// —— 报价发送的 subject_id 与 subject_type 同值域（都是那行 quotes.id），一个只比 id+policy
// 的实现会放过"审批池里另一个域的结论"。而今天这个组合造得出来：T-P3-07 的外发闸门用的是
// reach_plan 那一档 subject_type，只要哪天有人给同一个 id 递一条 reach_plan 的审批，
// 少比一列就等于拿"外发计划被批了"去发一张报价单。
//
// 夹具刻意走真 Submit（不直改库）：subject_type 是 Submit 的入参而不是校验字段，
// 所以这一格能造出的东西与生产能写进表的形状一致。
func TestQuoteSend_ApprovalWithAnotherSubjectTypeIsRefused(t *testing.T) {
	db := qssSetupDB(t)
	rowID := qssDraft(t, db)
	p := qssSvc(t, db)
	ctx := context.Background()

	other, created, err := p.appro.Submit(ctx, ApprovalSubmitInput{
		SubjectType: "reach_plan", SubjectID: rowID, PolicyKey: QuoteSendPolicyKey,
	})
	if err != nil || !created {
		t.Fatalf("造别的对象类型的审批失败：created=%v err=%v", created, err)
	}
	if _, err := p.appro.Decide(ctx, other.ID, ApprovalApprove, "boss_a", "批的是另一件事"); err != nil {
		t.Fatalf("裁决失败：%v", err)
	}

	_, err = p.svc.Send(ctx, qssSendWith(rowID, other.ID))
	if !errors.Is(err, ErrQuoteSendApprovalMismatch) {
		t.Fatalf("跨对象类型用批准应报 %v，实际 %v", ErrQuoteSendApprovalMismatch, err)
	}
	if p.reach.calls != 0 {
		t.Errorf("外发计数=%d，期望 0（那一版从没被人按报价批过）", p.reach.calls)
	}
	if got := qssStatus(t, db, rowID); got != model.QuoteStatusDraft {
		t.Errorf("被拒的版本状态动了：%s", got)
	}
}

// TestQuoteSend_EachVersionNeedsItsOwnApproval 审批审的是**那一版**。
//
// subject_id 取 quotes.id（版本行主键）而不是 quotes.quote_id（逻辑号）：
// v1 的批准不许给 v2 用 —— 客户还过价的那一版从没被人看过。
// 这一格与 policy_key 那格同样是"合起来才会漏"的形状：链上追加一版之后，
// 若 subject 用的是逻辑号，那条已批准的审批会直接把新版放出去。
func TestQuoteSend_EachVersionNeedsItsOwnApproval(t *testing.T) {
	db := qssSetupDB(t)
	rowID := qssDraft(t, db)
	head := qsRow(t, db, rowID)

	gen, _, _ := qsSvc(t, db)
	v2, err := gen.Revise(context.Background(), head.QuoteID, QuoteGenerateInput{OneID: "one_1"})
	if err != nil {
		t.Fatalf("追加第二版失败：%v", err)
	}
	if v2.ID == rowID {
		t.Fatalf("还价拿回了同一个行键：%s", v2.ID)
	}

	p := qssSvc(t, db)
	ctx := context.Background()
	first := qssMustSend(t, p, ctx, qssSendInput(rowID))
	qssApprove(t, p, first.ApprovalID)
	if _, err := p.svc.Send(ctx, qssSendWith(rowID, first.ApprovalID)); err != nil {
		t.Fatalf("v1 派发失败：%v", err)
	}
	if p.reach.calls != 1 {
		t.Fatalf("外发计数=%d，期望 1", p.reach.calls)
	}

	// 拿 v1 那条已批准的审批去发 v2 ⇒ 拒。
	_, err = p.svc.Send(ctx, qssSendWith(v2.ID, first.ApprovalID))
	if !errors.Is(err, ErrQuoteSendApprovalMismatch) {
		t.Fatalf("用上一版的批准发新版应报 %v，实际 %v", ErrQuoteSendApprovalMismatch, err)
	}
	// 新版自己走一遍：先待办、后放行。
	newOne, err := p.svc.Send(ctx, qssSendInput(v2.ID))
	if err != nil {
		t.Fatalf("v2 首次 Send 失败：%v", err)
	}
	if newOne.Disposition != QuoteSendAwaiting || p.reach.calls != 1 {
		t.Errorf("v2 应停在待办（外发计数仍为 %d），实际 %+v", p.reach.calls, newOne)
	}
}

// —— 认领后外发的失败方向 ————————————————————————————————————————————

// TestQuoteSend_SentRowCannotBeSentTwice 已经是 sent 的版本不再外发。
//
// 两路都断：读到自己已是 sent（同一人重复点）与 CAS 抢不到（并发对手先认领）。
// 判据是"同一个人不会收到两遍同样的报价"。
func TestQuoteSend_SentRowCannotBeSentTwice(t *testing.T) {
	db := qssSetupDB(t)
	rowA := qssDraft(t, db)
	rowB := qssDraft(t, db)
	p := qssSvc(t, db)
	ctx := context.Background()

	a := qssMustSend(t, p, ctx, qssSendInput(rowA))
	qssApprove(t, p, a.ApprovalID)
	if _, err := p.svc.Send(ctx, qssSendWith(rowA, a.ApprovalID)); err != nil {
		t.Fatalf("A 首次派发失败：%v", err)
	}
	if p.reach.calls != 1 {
		t.Fatalf("外发计数=%d，期望 1", p.reach.calls)
	}
	if _, err := p.svc.Send(ctx, qssSendWith(rowA, a.ApprovalID)); !errors.Is(err, ErrQuoteSendNotDraft) {
		t.Errorf("已发送的版本应报 %v，实际 %v", ErrQuoteSendNotDraft, err)
	}
	if p.reach.calls != 1 {
		t.Errorf("重复调用把外发计数推到 %d，期望仍为 1", p.reach.calls)
	}

	// B 批好了，但认领那一刻跃迁写不进去（并发对手先走了一步）⇒ 零外发。
	b := qssMustSend(t, p, ctx, qssSendInput(rowB))
	qssApprove(t, p, b.ApprovalID)
	p.store.failAll = true
	if _, err := p.svc.Send(ctx, qssSendWith(rowB, b.ApprovalID)); err == nil {
		t.Error("CAS 失败应上抛，而不是假装发出去了")
	}
	if p.reach.calls != 1 {
		t.Errorf("CAS 失败却还是发了（计数=%d）", p.reach.calls)
	}
	if got := qssStatus(t, db, rowB); got != model.QuoteStatusDraft {
		t.Errorf("CAS 失败后 B 的状态=%s，期望仍为 draft", got)
	}
}

// TestQuoteSend_ReachFailureRollsBackToDraft 外发失败 ⇒ 草稿回到原点，还能再发一次。
//
// 反面的形状是"库里写着 sent、客户手上什么都没有"：这一版从此不可再发，
// 而报表读起来它已经出过域。
func TestQuoteSend_ReachFailureRollsBackToDraft(t *testing.T) {
	db := qssSetupDB(t)
	rowID := qssDraft(t, db)
	p := qssSvc(t, db)
	ctx := context.Background()

	first := qssMustSend(t, p, ctx, qssSendInput(rowID))
	qssApprove(t, p, first.ApprovalID)
	p.reach.err = errors.New("渠道超时")

	_, err := p.svc.Send(ctx, qssSendWith(rowID, first.ApprovalID))
	if !errors.Is(err, ErrQuoteSendOutboundFailed) {
		t.Fatalf("外发失败应报 %v，实际 %v", ErrQuoteSendOutboundFailed, err)
	}
	if !strings.Contains(err.Error(), "渠道超时") {
		t.Errorf("原始错因丢了：%q", err.Error())
	}
	if got := qssStatus(t, db, rowID); got != model.QuoteStatusDraft {
		t.Errorf("回滚后状态=%s，期望 %s", got, model.QuoteStatusDraft)
	}
	if len(p.events.rows) != 0 {
		t.Errorf("没发出去却写了 %d 条事件", len(p.events.rows))
	}

	// 退回来之后**还能再发**：审批结论还在（同一次批准对应的是同一次动作），
	// 渠道恢复了就该能补上，而不是逼审批人再批一遍。
	p.reach.err = nil
	res, err := p.svc.Send(ctx, qssSendWith(rowID, first.ApprovalID))
	if err != nil {
		t.Fatalf("重试失败：%v", err)
	}
	if res.Disposition != QuoteSendSent {
		t.Errorf("重试处置=%q，期望 %q", res.Disposition, QuoteSendSent)
	}
}

// TestQuoteSend_RollbackFailureReportsBothFacts 退不回去时，错误里必须同时装着两件事。
//
// 只报"外发失败"会让人以为还能安全重试（其实库里已是 sent，重试会被上一例那条判据拒掉）；
// 只报"状态写坏了"会让人以为东西还在自己手上（其实客户已经收到了）。
// 两条都必须出现，且能被 errors.Is 分到。
func TestQuoteSend_RollbackFailureReportsBothFacts(t *testing.T) {
	db := qssSetupDB(t)
	rowID := qssDraft(t, db)
	p := qssSvc(t, db)
	ctx := context.Background()

	first := qssMustSend(t, p, ctx, qssSendInput(rowID))
	qssApprove(t, p, first.ApprovalID)
	p.reach.err = errors.New("渠道超时")
	p.store.failToDraft = true

	_, err := p.svc.Send(ctx, qssSendWith(rowID, first.ApprovalID))
	if !errors.Is(err, ErrQuoteSendStatusStuck) {
		t.Fatalf("应报 %v，实际 %v", ErrQuoteSendStatusStuck, err)
	}
	if !errors.Is(err, ErrQuoteSendOutboundFailed) {
		t.Errorf("错误里还应能分到外发失败那一半：%v", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, "外发") || !strings.Contains(msg, "没退回") {
		t.Errorf("错误里缺一条事实：%q", msg)
	}
}

// —— 发送时才生效的判据 ————————————————————————————————————————————

// TestQuoteSend_ScriptIsReResolvedAtSendTime T-P6-02 文件头那条义务的可测形式。
//
// 生成时拿到的那份话术**不落库**，所以发送只能重新解析一次。
// 生效版本在这中间被下线 ⇒ 这一版不许出去，且状态停在草稿（不认领、不回滚，
// 认领之后才发现没正文的话就等于白抢一次跃迁）。
func TestQuoteSend_ScriptIsReResolvedAtSendTime(t *testing.T) {
	db := qssSetupDB(t)
	rowID := qssDraft(t, db)
	p := qssSvc(t, db)
	ctx := context.Background()

	// 生成腿没走发送服务，所以这里的 0 是"发送前一次都没问"。
	if p.scripts.calls != 0 {
		t.Fatalf("夹具阶段话术就被问了 %d 次", p.scripts.calls)
	}
	first := qssMustSend(t, p, ctx, qssSendInput(rowID))
	if p.scripts.calls != 0 {
		t.Errorf("只开待办的那一次就去问话术了（%d 次）⇒ 白解析", p.scripts.calls)
	}
	qssApprove(t, p, first.ApprovalID)

	p.scripts.inner.err = ErrQuoteScriptUnavailable
	_, err := p.svc.Send(ctx, qssSendWith(rowID, first.ApprovalID))
	if !errors.Is(err, ErrQuoteScriptUnavailable) {
		t.Fatalf("话术失效应原样上抛 %v，实际 %v", ErrQuoteScriptUnavailable, err)
	}
	if p.reach.calls != 0 {
		t.Errorf("话术都没了就发出去，外发计数=%d", p.reach.calls)
	}
	if got := qssStatus(t, db, rowID); got != model.QuoteStatusDraft {
		t.Errorf("状态=%s，期望停在 %s", got, model.QuoteStatusDraft)
	}
	if p.store.updateCalls != 0 {
		t.Errorf("状态跃迁被调了 %d 次，期望 0（话术判据必须落在认领之前，别靠回滚补）", p.store.updateCalls)
	}

	p.scripts.inner.err = nil
	p.scripts.inner.script.Content = "这是换过之后的第五版生效话术"
	if _, err := p.svc.Send(ctx, qssSendWith(rowID, first.ApprovalID)); err != nil {
		t.Fatalf("Send 失败：%v", err)
	}
	if p.scripts.calls == 0 {
		t.Error("发送腿一次都没重新解析话术 ⇒ 发出去的是生成时那份快照")
	}
	if !strings.Contains(p.reach.reqs[0].Content, "第五版") {
		t.Errorf("正文不是重新解析到的那一版：%q", p.reach.reqs[0].Content)
	}
}

// TestQuoteSend_VersionWithNoLinesIsNotSendable 合计 0 的空版本不许出去。
//
// 这一格的来路很具体：T-P6-02 的 persistLines 失败面就是"版本行已落库、行项目没跟上"，
// 那条错写的是"必须人工处置，不得当成可发对象"。发送腿若不读行、只读那一行 quotes，
// 就会把一个合计 0.00 的价格文件发给客户，而库里它是个完全正常的 sent。
//
// T-P6-04 之后判据往前挪了一格：明细是**门序**的输入（折扣取所有行里最大的那个），
// 所以它必须在开审批之前读 ⇒ 这种版本现在连一条待办都开不出来。方向朝严：
// 一件永远发不出去的东西不该进审批人的收件箱，而"批一条空报价"这条路就此不存在。
func TestQuoteSend_VersionWithNoLinesIsNotSendable(t *testing.T) {
	db := qssSetupDB(t)
	rowID := qssDraft(t, db)
	ctx := context.Background()
	if err := db.WithContext(ctx).Where("quote_row_id = ?", rowID).
		Delete(&model.QuoteLineItem{}).Error; err != nil {
		t.Fatalf("模拟行项目缺失失败：%v", err)
	}

	p := qssSvc(t, db)
	_, err := p.svc.Send(ctx, qssSendInput(rowID))
	if !errors.Is(err, ErrQuoteSendLinesMissing) {
		t.Fatalf("空版本应报 %v，实际 %v", ErrQuoteSendLinesMissing, err)
	}
	if p.reach.calls != 0 {
		t.Errorf("外发计数=%d，期望 0", p.reach.calls)
	}
	if got := qssStatus(t, db, rowID); got != model.QuoteStatusDraft {
		t.Errorf("状态=%s，期望停在 %s（没发出去就不该留下 sent 的痕迹）", got, model.QuoteStatusDraft)
	}
	if p.store.updateCalls != 0 {
		t.Errorf("状态跃迁被调了 %d 次，期望 0（明细判据必须落在认领之前）", p.store.updateCalls)
	}
	if n := qssCountApprovals(t, db); n != 0 {
		t.Errorf("审批行数=%d，期望 0（读不出门序就不该开待办：那是一条批了也发不出去的僵尸待办）", n)
	}
	if _, err := p.svc.OpenApproval(ctx, rowID); err != nil {
		t.Errorf("空版本的 OpenApproval 报错：%v（读口不该被明细缺失带崩，它只查审批表）", err)
	}
}

// TestQuoteSend_GateClosedSubmitsNoApproval 阶段闸门关着时连审批都不开。
//
// 反面是"先把待办发出去，等运营开了阶段再批"：审批人批的是一件此刻根本不允许发生的事，
// 而报价阶段关着的这几小时里那条待办一直挂在池子里。
func TestQuoteSend_GateClosedSubmitsNoApproval(t *testing.T) {
	db := qssSetupDB(t)
	rowID := qssDraft(t, db)
	p := qssSvc(t, db)
	p.svc.SetGate(qsGate{&LTCConfig{Enabled: false}})

	_, err := p.svc.Send(context.Background(), qssSendInput(rowID))
	if !errors.Is(err, ErrQuoteGateClosed) {
		t.Fatalf("应报 %v，实际 %v", ErrQuoteGateClosed, err)
	}
	if n := qssCountApprovals(t, db); n != 0 {
		t.Errorf("闸门关着时开了 %d 条审批，期望 0", n)
	}
}

// TestQuoteSend_AutoApprovalPolicyDispatchesInline C2 的同步退化态：原地拿结果。
//
// 这一格同时是 AC② 的另一半：不阻塞的实现**必须**也支持"根本不需要等"那一档，
// 否则每次低额报价都要人去点一下，运营会直接把整条链绕过去。
func TestQuoteSend_AutoApprovalPolicyDispatchesInline(t *testing.T) {
	db := qssSetupDB(t)
	rowID := qssDraft(t, db)
	p := qssSvcWithPolicy(t, db, qssPolicy{allow: true, reason: "低额且来自白名单商机"})

	res, err := p.svc.Send(context.Background(), qssSendInput(rowID))
	if err != nil {
		t.Fatalf("Send 失败：%v", err)
	}
	if res.Disposition != QuoteSendSent {
		t.Errorf("处置=%q，期望 %q", res.Disposition, QuoteSendSent)
	}
	if p.reach.calls != 1 {
		t.Errorf("外发计数=%d，期望 1", p.reach.calls)
	}
	appr := qssApprovalRow(t, db, res.ApprovalID)
	if appr.Status != model.ApprovalStatusApproved || appr.DecidedBy != model.ApprovalDecidedByPolicy {
		t.Errorf("自动放行落成了 %s/%s，期望 approved/%s",
			appr.Status, appr.DecidedBy, model.ApprovalDecidedByPolicy)
	}
	if !strings.Contains(appr.DecisionNote, "低额") {
		t.Errorf("理由没进 decision_note：%q（事后要能回答这条为什么没人看就过了）", appr.DecisionNote)
	}
}

// —— 入参与装配 ————————————————————————————————————————————————

// TestQuoteSend_InputGuards 坏输入全部在开审批之前挡掉。
//
// 收件人身份**不在**这一张表里（它不是入参，见 TestQuoteSend_RecipientComesFromTheQuote）。
// 这里只剩两格调用方的事：发哪一版、谁点的发送。
// Operator 超列宽（sales_events.owner_id varchar(64)）判在派发之前：
// 与 T-P6-02 那个 numeric 溢出同族 —— 越界要让服务报，别让 PG 在客户已经收到东西之后报。
func TestQuoteSend_InputGuards(t *testing.T) {
	db := qssSetupDB(t)
	rowID := qssDraft(t, db)
	p := qssSvc(t, db)
	ctx := context.Background()

	cases := []struct {
		name string
		in   QuoteSendInput
	}{
		{"没有版本行号", QuoteSendInput{Operator: "s"}},
		{"行号是空白", QuoteSendInput{QuoteRowID: "   ", Operator: "s"}},
		{"没有操作者", QuoteSendInput{QuoteRowID: rowID}},
		{"操作者是空白", QuoteSendInput{QuoteRowID: rowID, Operator: "   "}},
		{"操作者超出列宽", QuoteSendInput{QuoteRowID: rowID, Operator: strings.Repeat("s", 65)}},
	}
	for _, c := range cases {
		_, err := p.svc.Send(ctx, c.in)
		if !errors.Is(err, ErrQuoteSendInputInvalid) {
			t.Errorf("%s：应报 %v，实际 %v", c.name, ErrQuoteSendInputInvalid, err)
		}
	}
	if _, err := p.svc.Send(ctx, qssSendInput("q_不存在的行")); !errors.Is(err, ErrQuoteVersionMissing) {
		t.Errorf("行不存在应报 %v，实际 %v", ErrQuoteVersionMissing, err)
	}
	if n := qssCountApprovals(t, db); n != 0 {
		t.Errorf("坏输入开出了 %d 条审批，期望 0", n)
	}
	if p.reach.calls != 0 {
		t.Errorf("外发计数=%d，期望 0", p.reach.calls)
	}
}

// TestQuoteSend_UnassembledReturnsExplicitly 缺件必须报"没装配"，不能回空结果、不能 panic。
func TestQuoteSend_UnassembledReturnsExplicitly(t *testing.T) {
	db := qssSetupDB(t)
	rowID := qssDraft(t, db)
	ctx := context.Background()

	broken := NewQuoteSendService(nil, nil, nil, nil, nil, nil, nil, nil, nil)
	if broken.Available() {
		t.Error("全缺的服务自称可用")
	}
	if _, err := broken.Send(ctx, qssSendInput(rowID)); !errors.Is(err, ErrQuoteServiceUnavailable) {
		t.Errorf("应报 ErrQuoteServiceUnavailable，实际 %v", err)
	}
	if _, err := broken.OpenApproval(ctx, rowID); !errors.Is(err, ErrQuoteServiceUnavailable) {
		t.Errorf("OpenApproval 应报 ErrQuoteServiceUnavailable，实际 %v", err)
	}

	// 一格一格地缺（同包内直接改字段：不为此在生产类上开只有测试在用的 setter）。
	for _, slot := range []func(s *QuoteSendService){
		func(s *QuoteSendService) { s.store = nil },
		func(s *QuoteSendService) { s.opps = nil },
		func(s *QuoteSendService) { s.approvals = nil },
		func(s *QuoteSendService) { s.open = nil },
		func(s *QuoteSendService) { s.reach = nil },
		func(s *QuoteSendService) { s.events = nil },
		func(s *QuoteSendService) { s.cfg = nil },
		func(s *QuoteSendService) { s.scripts = nil },
		func(s *QuoteSendService) { s.gate = nil },
	} {
		svc := qssSvc(t, db).svc
		slot(svc)
		if svc.Available() {
			t.Error("缺件后仍自称可用")
		}
		if _, err := svc.Send(ctx, qssSendInput(rowID)); !errors.Is(err, ErrQuoteServiceUnavailable) {
			t.Errorf("缺件后应报 ErrQuoteServiceUnavailable，实际 %v", err)
		}
	}
}

// —— 类型层的锁 ————————————————————————————————————————————————

// TestQuoteSendStoreSurfaceIsTheStatusWriterAlone 状态改写权只在发送腿这一侧。
//
// T-P6-02 那条判据说的是「生成侧的接口里没有 UpdateStatus」；这里补另一半：
// 发送侧的接口里有、**而且方法集就是这三个**，同时生成服务拿不到发送腿的任何东西。
// 两边合起来才叫"发送必经闸门"被类型挡住，而不是一边写着、另一边偷偷也用。
func TestQuoteSendStoreSurfaceIsTheStatusWriterAlone(t *testing.T) {
	send := qssMethodNames((*quoteSendStore)(nil))
	if len(send) != 3 || send["GetByID"] != 1 || send["ListLines"] != 1 || send["UpdateStatus"] != 1 {
		t.Errorf("quoteSendStore 方法集=%v，期望恰好 GetByID/ListLines/UpdateStatus 三个", send)
	}
	gen := qssMethodNames((*quoteStore)(nil))
	if gen["UpdateStatus"] == 1 {
		t.Error("生成侧的 quoteStore 拿到了 UpdateStatus ⇒ AC② 的类型层判据失效")
	}
	for _, f := range qssFieldTypes(QuoteService{}) {
		if strings.Contains(f, "Send") || f == "quoteSendStore" {
			t.Errorf("QuoteService 持有发送腿依赖 %s ⇒ 生成侧又能改状态了", f)
		}
	}
	// 发送腿拿到的那份商机端口必须是"只能读"（判据④只要求查一次收件人）：
	// 一旦这里换成 *repository.OpportunityRepository，发送腿就能顺手改来源商机的阶段/金额，
	// 而那张表的跃迁表与乐观锁在 T-P4-03 那边守着，两边各写一次就是两套账。
	// 比对用**大小写敏感**的那一格：类型字符串里 service.opportunityReader 是小写端口名，
	// 带大写 Opportunity 的只有仓储那一份具体类型。
	nReader := 0
	for _, f := range qssFieldTypes(QuoteSendService{}) {
		if strings.Contains(f, "Opportunity") {
			t.Errorf("发送腿持有商机的具体类型 %s，期望只有窄读口 opportunityReader", f)
		}
		if strings.Contains(f, "opportunityReader") {
			nReader++
		}
	}
	if nReader != 1 {
		t.Errorf("发送腿里的商机读口有 %d 份，期望恰好 1 份", nReader)
	}
	if got := qssMethodNames((*opportunityReader)(nil)); len(got) != 1 || got["GetByID"] != 1 {
		t.Errorf("opportunityReader 方法集=%v，期望恰好 GetByID 一个（多一格就是给发送腿开了改商机的路）", got)
	}
}

// TestQuoteSendInputAndResultHaveNoCredentialOrScriptColumn 出参与入参的字段白名单。
//
// 入参不许带正文/合计/收件人/凭证：前三项都是**事实**（库里那些行与那条商机算得出来），
// 只有事实能由调用方递的话，"批准的这一版"和"发出去的那一份"就会是两件事。
// 字段白名单里今天连 OneID 都没有 —— 它是判据，不是遗漏（反面见 Mismatch 那两例与
// TestQuoteSend_RecipientComesFromTheQuote）。
// 出参不许带审批凭证 —— ResumeToken 靠 json:"-" 出不了门，但字段名一旦出现在结构体上，
// 就等于邀请下一个写端点把它填上（T-P3-04 收 T-P3-02 那条边界时立的同一口径）。
func TestQuoteSendInputAndResultHaveNoCredentialOrScriptColumn(t *testing.T) {
	in := qssFieldNames(QuoteSendInput{})
	wantIn := map[string]bool{"QuoteRowID": true, "ApprovalID": true, "Operator": true}
	if len(in) != len(wantIn) {
		t.Errorf("入参字段=%v，期望 %v", in, wantIn)
	}
	for name := range in {
		if !wantIn[name] {
			t.Errorf("入参多出未登记字段 %s", name)
		}
		lower := strings.ToLower(name)
		for _, bad := range []string{"content", "text", "total", "amount", "script", "phone", "email", "token", "oneid", "recipient", "customer"} {
			if strings.Contains(lower, bad) {
				t.Errorf("入参字段 %s 含 %q：正文/合计/收件人/凭证都不许由调用方递", name, bad)
			}
		}
	}
	res := qssFieldNames(QuoteSendResult{})
	for name := range res {
		lower := strings.ToLower(name)
		for _, bad := range []string{"token", "secret", "resume"} {
			if strings.Contains(lower, bad) {
				t.Errorf("出参字段 %s 含 %q：恢复凭证不是业务字段", name, bad)
			}
		}
	}
	for _, want := range []string{"Disposition", "ApprovalID", "EventRecorded", "Status"} {
		if res[want] != 1 {
			t.Errorf("出参缺 %s（控制器与调用方靠它分诊）", want)
		}
	}
}

// TestQuoteSendDispositionValuesAreTheContractOnly 处置值集合。
//
// 这四个串就是 HTTP 状态码的映射依据（awaiting→202、sent→200、rejected/expired→409），
// 多一个少一个都必须先在这里红。
func TestQuoteSendDispositionValuesAreTheContractOnly(t *testing.T) {
	set := map[string]bool{
		QuoteSendAwaiting: true, QuoteSendSent: true, QuoteSendRejected: true, QuoteSendExpired: true,
	}
	if len(set) != 4 {
		t.Fatalf("处置值集合塌成 %d 个了", len(set))
	}
	for name, s := range map[string]string{
		"awaiting": QuoteSendAwaiting, "sent": QuoteSendSent,
		"rejected": QuoteSendRejected, "expired": QuoteSendExpired,
	} {
		if strings.TrimSpace(s) == "" {
			t.Errorf("%s 那档处置是空串", name)
		}
	}
	if QuoteSendPolicyKey != "quote.send" {
		t.Errorf("policy_key=%q，卡面写的是 quote.send", QuoteSendPolicyKey)
	}
	if QuoteApprovalSubjectType != "quote" {
		t.Errorf("subject_type=%q，期望 quote", QuoteApprovalSubjectType)
	}
}

// —— 开放审批的读口（AC① 在 HTTP 上可读）————————————————————————————————

// TestQuoteSend_OpenApprovalExposesThePendingID 批完之前刷新页面，操作者还能找回那条待办。
//
// 这一格存在的理由很具体：Send 返回里那个审批号一旦丢了，调用方只能重开一条待办
// （见 SendAfterDecision 那例）；有了读口，"停在 pending"这件事才是**可查**的，
// 而不是只能记住。裁决落定之后回 nil（没有开放审批，而不是"查不到"）。
func TestQuoteSend_OpenApprovalExposesThePendingID(t *testing.T) {
	db := qssSetupDB(t)
	rowID := qssDraft(t, db)
	p := qssSvc(t, db)
	ctx := context.Background()

	if got, err := p.svc.OpenApproval(ctx, rowID); err != nil || got != nil {
		t.Fatalf("还没提过审批时应回 (nil,nil)，实际 (%v,%v)", got, err)
	}
	first := qssMustSend(t, p, ctx, qssSendInput(rowID))
	open, err := p.svc.OpenApproval(ctx, rowID)
	if err != nil {
		t.Fatalf("读开放审批失败：%v", err)
	}
	if open == nil || open.ID != first.ApprovalID {
		t.Fatalf("读到的开放审批不对：%+v（期望 id=%s）", open, first.ApprovalID)
	}
	qssApprove(t, p, first.ApprovalID)
	if got, err := p.svc.OpenApproval(ctx, rowID); err != nil || got != nil {
		t.Errorf("已裁决后应回 (nil,nil)，实际 (%v,%v)", got, err)
	}
	if _, err := p.svc.OpenApproval(ctx, "  "); !errors.Is(err, ErrQuoteSendInputInvalid) {
		t.Errorf("空白行号应报入参错，实际 %v", err)
	}
}
