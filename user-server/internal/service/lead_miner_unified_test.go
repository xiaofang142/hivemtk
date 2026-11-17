package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
)

type deferredMinerClueRepo struct {
	*fakeClueRepo
	updateErr  error
	findErr    error
	createErr  error
	updated    bool
	afterWrite func()
}

func (r *deferredMinerClueRepo) FindByTypeAndAccount(ctx context.Context, kind int64, account string) (*model.Clue, error) {
	if r.findErr != nil {
		return nil, r.findErr
	}
	return r.fakeClueRepo.FindByTypeAndAccount(ctx, kind, account)
}

func (r *deferredMinerClueRepo) Create(ctx context.Context, clue *model.Clue) error {
	if r.createErr != nil {
		return r.createErr
	}
	if err := r.fakeClueRepo.Create(ctx, clue); err != nil {
		return err
	}
	if r.afterWrite != nil {
		r.afterWrite()
	}
	return nil
}

func (r *deferredMinerClueRepo) UpdateByID(ctx context.Context, id string, updates map[string]any) error {
	if r.updateErr != nil {
		return r.updateErr
	}
	if err := r.fakeClueRepo.UpdateByID(ctx, id, updates); err != nil {
		return err
	}
	r.updated = true
	if r.afterWrite != nil {
		r.afterWrite()
	}
	return nil
}

type deferredMinerAdapter struct {
	ChannelLeadAdapter
	repo         *deferredMinerClueRepo
	outreach     int
	beforeUpdate bool
}

func (a *deferredMinerAdapter) TriggerOutreach(context.Context, *WebhookService, string, string, string, string, int, string) {
	a.outreach++
	a.beforeUpdate = !a.repo.updated
}

func TestBridgeLeadMiner_UpgradePersistsBeforeOneOutreach(t *testing.T) {
	old := leadLLMRefineEnabledProvider
	leadLLMRefineEnabledProvider = func() bool { return false }
	t.Cleanup(func() { leadLLMRefineEnabledProvider = old })
	t.Setenv("K_TRACE_LLM_REFINE", "")
	for _, tc := range []struct {
		name string
		err  error
		want int
	}{
		{name: "更新成功仅触达一次", want: 1},
		{name: "更新失败不得触达", err: errors.New("测试持久化故障")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &deferredMinerClueRepo{fakeClueRepo: newFakeClueRepo(), updateErr: tc.err}
			a := &deferredMinerAdapter{ChannelLeadAdapter: bridgeLeadAdapterForChannel("douyin"), repo: r}
			clue := &model.Clue{Account: a.AccountKey("customer-a", "", ""), Type: a.ClueType(), IntentScore: 8}
			if err := r.fakeClueRepo.Create(context.Background(), clue); err != nil {
				t.Fatal(err)
			}
			s := &WebhookService{clueRepo: r}
			newOpp := MineUnifiedLead(context.Background(), s, &model.MessageHub{}, a,
				"account-a", "conversation-a", "", "customer-a", "", "", "价格报价采购预算合作")
			if a.outreach != tc.want || a.beforeUpdate || newOpp != (tc.want == 1) {
				t.Fatalf("触达必须发生在更新成功之后且仅一次：次数=%d 提前=%v 新商机=%v", a.outreach, a.beforeUpdate, newOpp)
			}
		})
	}
}

func TestBridgeLeadMiner_DetailedErrorsAreNotSuccess(t *testing.T) {
	old := leadLLMRefineEnabledProvider
	leadLLMRefineEnabledProvider = func() bool { return false }
	t.Cleanup(func() { leadLLMRefineEnabledProvider = old })
	t.Setenv("K_TRACE_LLM_REFINE", "")
	for _, operation := range []string{"查询", "创建", "更新"} {
		t.Run(operation, func(t *testing.T) {
			failure := errors.New("测试仓储故障")
			r := &deferredMinerClueRepo{fakeClueRepo: newFakeClueRepo()}
			a := &deferredMinerAdapter{ChannelLeadAdapter: bridgeLeadAdapterForChannel("douyin"), repo: r}
			switch operation {
			case "查询":
				r.findErr = failure
			case "创建":
				r.createErr = failure
			case "更新":
				clue := &model.Clue{Account: a.AccountKey("customer-a", "", ""), Type: a.ClueType(), IntentScore: 8}
				if err := r.fakeClueRepo.Create(context.Background(), clue); err != nil {
					t.Fatal(err)
				}
				r.updateErr = failure
			}
			newOpp, err := MineUnifiedLeadDetailed(context.Background(), &WebhookService{clueRepo: r}, &model.MessageHub{}, a,
				"account-a", "conversation-a", "", "customer-a", "", "", "价格报价采购预算合作")
			if err == nil || newOpp || a.outreach != 0 {
				t.Fatalf("持久化错误不得被标作成功或触达：%v 新商机=%v 触达=%d", err, newOpp, a.outreach)
			}
		})
	}
}

func TestBridgeLeadMiner_PersistedProcessorRequiresTrustedScope(t *testing.T) {
	old := leadLLMRefineEnabledProvider
	leadLLMRefineEnabledProvider = func() bool { return false }
	t.Cleanup(func() { leadLLMRefineEnabledProvider = old })
	t.Setenv("K_TRACE_LLM_REFINE", "")
	for _, tc := range []struct {
		name     string
		mutate   func(*model.MessageHub)
		accepted bool
	}{
		{name: "可信持久入站", accepted: true},
		{name: "不同租户", mutate: func(h *model.MessageHub) { h.TenantID = 19 }},
		{name: "客户端伪资格", mutate: func(h *model.MessageHub) {
			h.BridgeLeadEligible = false
			h.Extra = model.JSONMap{"bridge_lead_eligible": true}
		}},
		{name: "出站消息", mutate: func(h *model.MessageHub) { h.Direction = "outbound" }},
		{name: "缺少持久身份", mutate: func(h *model.MessageHub) { h.ID = 0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &deferredMinerClueRepo{fakeClueRepo: newFakeClueRepo()}
			s := &WebhookService{clueRepo: r}
			hub := &model.MessageHub{ID: 1, TenantID: 18, Platform: "douyin", AccountID: "lead-account", ConversationID: "lead-conversation", MsgID: "lead-message", Direction: "inbound", SenderID: "customer-a", Content: "大家好", SentAt: time.Now(), BridgeLeadEligible: true}
			if tc.mutate != nil {
				tc.mutate(hub)
			}
			err := s.ProcessBridgeLead(tenant.OnlyTenant(context.Background(), 18), hub)
			if tc.accepted && (err != nil || r.createCnt != 1) {
				t.Fatalf("可信正常输入必须执行持久挖掘：%v 次数=%d", err, r.createCnt)
			}
			if !tc.accepted && (err == nil || r.createCnt != 0) {
				t.Fatal("作用域或资格缺失必须拒绝处理")
			}
		})
	}
}

func TestBridgeLeadMiner_CancellationAfterPersistenceCannotOutreach(t *testing.T) {
	old := leadLLMRefineEnabledProvider
	leadLLMRefineEnabledProvider = func() bool { return false }
	t.Cleanup(func() { leadLLMRefineEnabledProvider = old })
	t.Setenv("K_TRACE_LLM_REFINE", "")
	for _, operation := range []string{"创建", "更新"} {
		t.Run(operation, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			r := &deferredMinerClueRepo{fakeClueRepo: newFakeClueRepo(), afterWrite: cancel}
			a := &deferredMinerAdapter{ChannelLeadAdapter: bridgeLeadAdapterForChannel("douyin"), repo: r}
			if operation == "更新" {
				clue := &model.Clue{Account: a.AccountKey("customer-a", "", ""), Type: a.ClueType(), IntentScore: 8}
				if err := r.fakeClueRepo.Create(ctx, clue); err != nil {
					t.Fatal(err)
				}
			}
			_, err := MineUnifiedLeadDetailed(ctx, &WebhookService{clueRepo: r}, &model.MessageHub{}, a,
				"account-a", "conversation-a", "", "customer-a", "", "", "价格报价采购预算合作")
			if !errors.Is(err, context.Canceled) || a.outreach != 0 {
				t.Fatalf("持久化后取消必须终止主动触达：错误=%v 次数=%d", err, a.outreach)
			}
		})
	}
}
