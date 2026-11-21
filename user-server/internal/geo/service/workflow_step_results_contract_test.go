package service

// workflow_step_results_contract_test.go FA-31：上游结果传递契约闭环。
//
// 钉住的契约：引擎（WorkflowService.Run）把 `_step_results` 以
// map[string]*dto.StepResult（步骤名/类型/状态/结果）传给执行器；真实注册的
// source_attribution / content_gap_fill 能按 StepType 找到上游结果——
// 探测→归因→补缺→线索捕获四步在真实 Run 路径上闭环（思维链行、补位任务、
// 线索捕获计数都有实际落库/调用）。禁止退化回「测试自拼 map 直调执行器」。

import (
	"context"
	"strings"
	"testing"

	"hivemtk-user/internal/geo/dto"
	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

type countingLeadPort struct {
	calls []string
}

// geoTestDBWithTasks geoTestDB 基础上补齐决策链两张表（思维链/补位任务）。
func geoTestDBWithTasks(t *testing.T) *gorm.DB {
	return testutil.NewTestDB(t,
		&model.GeoKeyword{},
		&model.GeoArticle{},
		&model.GeoPlatformAccount{},
		&model.GeoPublishRecord{},
		&model.GeoKnowledgeDocument{},
		&model.GeoWorkflow{},
		&model.GeoWorkflowExecution{},
		&model.GeoWorkflowTemplate{},
		&model.GeoQueryChain{},
		&model.GeoContentTask{},
	)
}

func (p *countingLeadPort) CaptureLead(_ context.Context, contact, contactType, chainID, intent string) (string, error) {
	p.calls = append(p.calls, contactType+":"+contact+"|"+chainID+"|"+intent)
	return "clue-fa31-1", nil
}

func TestWorkflowRun_DecisionChainUpstreamContractClosedLoop(t *testing.T) {
	db := geoTestDBWithTasks(t)
	leads := &countingLeadPort{}
	svc := NewWorkflowService(
		newGeoWFRepo(db), newGeoExecRepo(db), newGeoTplRepo(db),
		repository.NewGeoQueryChainRepositoryWithDB(db),
		repository.NewGeoContentTaskRepositoryWithDB(db),
		nil,
	)
	// 决策链执行器：真实探针替身 + 真实思维链/任务仓储（测试库）+ 计数线索端口。
	svc.RegisterDecisionChainExecutors(DecisionChainDeps{
		Probe:    &fakeProbe{},
		ChainRepo: repository.NewGeoQueryChainRepositoryWithDB(db),
		TaskRepo:  repository.NewGeoContentTaskRepositoryWithDB(db),
		Brand:     "ourbrand",
		LeadPort:  leads,
	})

	wf, err := svc.Create(context.Background(), &dto.SaveWorkflowRequest{
		Name: "FA31-决策链闭环",
		Steps: []dto.WorkflowStep{
			{Name: "开", Type: "trigger"},
			{Name: "探测", Type: "query_probe", Params: map[string]interface{}{"keyword": "CRM 选型", "intent": "对比"}},
			{Name: "归因", Type: "source_attribution", Params: map[string]interface{}{"our_domains": "oursite.example.com"}},
			{Name: "补缺", Type: "content_gap_fill", Params: map[string]interface{}{"keyword": "CRM 选型", "intent": "对比"}},
			{Name: "收线", Type: "capture_lead", Params: map[string]interface{}{"contact": "13900003100", "contact_type": "phone", "intent": "对比"}},
			{Name: "终", Type: "end"},
		},
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("创建工作流失败：%v", err)
	}

	resp, err := svc.Run(context.Background(), wf.ID)
	if err != nil {
		t.Fatalf("实际 Run 应成功（契约断裂时归因/补缺必失败）：%v", err)
	}
	if resp.Status != "success" {
		t.Fatalf("执行状态应为 success：%+v", resp)
	}

	// 归因→补缺：假探针命中 rival-source.example.com ⇒ 至少 1 条补位任务落库。
	taskRepo := repository.NewGeoContentTaskRepositoryWithDB(db)
	pendingCount, err := taskRepo.CountByStatus(context.Background(), "pending")
	if err != nil {
		t.Fatalf("读补位任务失败：%v", err)
	}
	if pendingCount == 0 {
		t.Fatal("补缺步骤未从上游归因结果创建任务——上游契约在真实 Run 路径上断裂")
	}
	tasks, err := taskRepo.ListPending(context.Background(), 50)
	if err != nil {
		t.Fatalf("读补位任务列表失败：%v", err)
	}
	found := false
	for _, tk := range tasks {
		if tk.GapType == "missing_domain" && strings.Contains(tk.Detail, "rival-source.example.com") {
			found = true
		}
	}
	if !found {
		t.Errorf("补位任务应含 rival-source.example.com 缺口，实际 %+v", tasks)
	}

	// capture_lead 真实调用线索端口（拿到上游引擎传递的步骤参数）。
	if len(leads.calls) != 1 || !strings.Contains(leads.calls[0], "13900003100") {
		t.Errorf("线索捕获应恰好一次且带收件人，实际 %v", leads.calls)
	}
}

// 上游缺失/失败：归因执行器必须显式报错，不得静默返回空结果继续下游。
func TestWorkflowRun_MissingUpstreamFailsExplicitly(t *testing.T) {
	db := geoTestDBWithTasks(t)
	svc := NewWorkflowService(
		newGeoWFRepo(db), newGeoExecRepo(db), newGeoTplRepo(db),
		repository.NewGeoQueryChainRepositoryWithDB(db),
		repository.NewGeoContentTaskRepositoryWithDB(db),
		nil,
	)
	svc.RegisterDecisionChainExecutors(DecisionChainDeps{
		Probe: &fakeProbe{},
		ChainRepo: repository.NewGeoQueryChainRepositoryWithDB(db),
		TaskRepo:  repository.NewGeoContentTaskRepositoryWithDB(db),
		Brand:     "ourbrand",
	})
	exec := svc.executors["source_attribution"]
	if exec == nil {
		t.Fatal("source_attribution 未注册")
	}
	// 无 _step_results：显式报错。
	if _, err := exec(context.Background(), map[string]interface{}{"our_domains": "oursite.example.com"}); err == nil {
		t.Fatal("缺上游 query_probe 必须显式报错")
	}
	// 旧契约形状（map[string]string）：不得被误读为有效上游——这是防「类型契约回退」的守卫。
	if _, err := exec(context.Background(), map[string]interface{}{
		"our_domains":   "oursite.example.com",
		"_step_results": map[string]string{"qp": `{"cited_domains":{"rival-source.example.com":2}}`,
		},
	}); err == nil {
		t.Fatal("旧类型形状不得通过上游校验（契约已冻结为 StepResult）")
	}
}
