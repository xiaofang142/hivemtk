package repository

// observability_dashboard_route_repo_test.go TB-11 刀4：路由观测四读的
// 真库落地验证（列名/表名/聚合写法/伪行过滤），与 007 看板六读同口径。
import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

func TestObservabilityDashboardRepo_RouteReadsAgainstRealPG(t *testing.T) {
	db := testutil.NewTestDB(t,
		&model.IntentLog{},
		&model.IntentRouteDaily{},
		&model.CustomerSession{},
	)
	repo := NewObservabilityDashboardRepository(db)
	ctx := context.Background()
	since := time.Now().Add(-time.Hour)
	today := time.Now().Format("2006-01-02")

	// 夹具：intent_records 3 行（order_status×2 / complaint×1）
	logs := []model.IntentLog{
		{IntentMajor: "order_status", Confidence: 0.9, Timestamp: time.Now(), Message: "查物流"},
		{IntentMajor: "order_status", Confidence: 0.8, Timestamp: time.Now(), Message: "到哪了"},
		{IntentMajor: "complaint", Confidence: 0.7, Timestamp: time.Now(), Message: "太慢了"},
	}
	if err := db.Create(&logs).Error; err != nil {
		t.Fatalf("插 intent_records 夹具失败: %v", err)
	}

	// 夹具：intent_route_daily 链级行/工具级行/纠错事件行
	daily := []model.IntentRouteDaily{
		{Date: today, IntentType: "order_status", ChainName: "order_status", ToolName: "", ChainHits: 2, ChainMisses: 1},
		{Date: today, IntentType: "complaint", ChainName: "complaint", ToolName: "", ChainHits: 0, ChainMisses: 1},
		{Date: today, IntentType: "order_status", ChainName: "order_status", ToolName: "order.lookup", ToolCalls: 3, ToolSuccess: 2},
		{Date: today, IntentType: "refund", ChainName: "", ToolName: "__correction__", ToolCalls: 1},
	}
	if err := db.Create(&daily).Error; err != nil {
		t.Fatalf("插 intent_route_daily 夹具失败: %v", err)
	}

	// 夹具：customer_sessions 3 行（已了结 1 / 转人工 1 / 进行中 1）
	now := time.Now()
	sessions := []model.CustomerSession{
		{SessionID: "sess-route-1", Platform: "douyin", Status: model.SessionStatusClosed, ClosedAt: &now},
		{SessionID: "sess-route-2", Platform: "douyin", Status: model.SessionStatusHumanHandling, HandoffAt: &now},
		{SessionID: "sess-route-3", Platform: "wechat", Status: model.SessionStatusPending},
	}
	if err := db.Create(&sessions).Error; err != nil {
		t.Fatalf("插 customer_sessions 夹具失败: %v", err)
	}

	// 意图分布：真实识别量，order_status 排前。
	intentRows, err := repo.RouteIntentDistribution(ctx, since)
	if err != nil {
		t.Fatalf("RouteIntentDistribution 真库失败: %v", err)
	}
	if len(intentRows) != 2 || intentRows[0].IntentType != "order_status" || intentRows[0].Cnt != 2 ||
		intentRows[1].IntentType != "complaint" || intentRows[1].Cnt != 1 {
		t.Fatalf("意图分布错：%+v", intentRows)
	}

	// 链命中率：仅链级行（tool_name=''），纠错行 chain_name='' 不混入。
	chainRows, err := repo.RouteChainHitRate(ctx, since)
	if err != nil {
		t.Fatalf("RouteChainHitRate 真库失败: %v", err)
	}
	if len(chainRows) != 2 {
		t.Fatalf("链命中率行数应为 2（order_status/complaint），实得 %d：%+v", len(chainRows), chainRows)
	}
	var osChain, cpChain *ObsDashRouteChainRow
	for i := range chainRows {
		switch chainRows[i].ChainName {
		case "order_status":
			osChain = &chainRows[i]
		case "complaint":
			cpChain = &chainRows[i]
		}
	}
	if osChain == nil || osChain.Hits != 2 || osChain.Misses != 1 {
		t.Fatalf("order_status 链行错：%+v", osChain)
	}
	if cpChain == nil || cpChain.Hits != 0 || cpChain.Misses != 1 {
		t.Fatalf("complaint 链行错：%+v", cpChain)
	}

	// 工具成功率：排除链级空串行与 '__correction__' 纠错行。
	toolRows, err := repo.RouteToolSuccessRate(ctx, since)
	if err != nil {
		t.Fatalf("RouteToolSuccessRate 真库失败: %v", err)
	}
	if len(toolRows) != 1 || toolRows[0].ToolName != "order.lookup" ||
		toolRows[0].Calls != 3 || toolRows[0].Success != 2 {
		t.Fatalf("工具成功率错（应只剩 order.lookup 3/2）：%+v", toolRows)
	}

	// 北极星读数：sessions=3 resolved=1 handoff=1 corrections=1 recognized=3。
	ns, err := repo.RouteNorthStar(ctx, since)
	if err != nil {
		t.Fatalf("RouteNorthStar 真库失败: %v", err)
	}
	if ns.Sessions != 3 || ns.Resolved != 1 || ns.Handoff != 1 || ns.Corrections != 1 || ns.Recognized != 3 {
		t.Fatalf("北极星读数错：%+v", ns)
	}
}
