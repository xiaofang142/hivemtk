// opportunity_convert_wiring_test.go T-P4-05：装配点把转换竖装起来没有、装的是哪副底座。
//
// 判据抄 M33 那一课（本仓反复出现的形状是"service 侧单测全绿、生产装配点没人调用"）：
// 挖掘侧那条接缝（opportunity_convert_mining_test.go）断的是"全局里有转换器时它会干活"，
// 而"生产环境到底有没有那一步"只能由本文件回答 —— 它调真实的 InitOpportunityRuntime，
// 再拿装配出来的全局实例转一条线索，看它是不是真把行写进了这把库、
// 是不是真从这把库的 sales_events 里认出了在册销售。
//
// 最后那一条断言（归属=carol）是"哪副底座"的证据：如果装配时把名单句柄落成了别的库
// 或者根本没接，转换器照样 Available()，只是每一单都分到 no_roster、owner 恒为空 ——
// 只断 Available() 就看不见这件事。
package app

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"gorm.io/gorm"
)

// converterTestDB 建一套带商机竖全部下家的库，并把全局句柄指过去。
// 指全局不是偷懒：装配点注入的 LTC 存储走 NewSystemConfigKVRepository()，
// 那个构造器在**那一刻**捕获 db.GetDB()，所以顺序必须是先指全局、再装配。
func converterTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database := testutil.NewTestDB(t, &model.Opportunity{}, &model.Clue{},
		&model.OperationLog{}, &model.SalesEvent{}, &model.SystemConfigKV{})
	prev := db.GetDB()
	db.SetTestDB(database)
	t.Cleanup(func() { db.SetTestDB(prev) })
	return database
}

// turnOnOpportunityStage 用**运营用的同一个入口**（LTCConfigService.Save）把商机阶段打开。
// 直接往库里塞 JSON 等于绕过校验器，那装配测试就会在一个生产写不出来的配置上变绿。
func turnOnOpportunityStage(t *testing.T) {
	t.Helper()
	ltc := service.NewLTCConfigServiceWithStore(repository.NewSystemConfigKVRepository())
	service.SetGlobalLTCConfig(ltc)
	cfg := &service.LTCConfig{
		Enabled:       true,
		StagesEnabled: service.LTCStages{}.With(service.LTCStageOpportunity),
		Thresholds: service.LTCThresholds{
			LeadScore: 70, Confidence: 0.80, DiscountPercent: 15, WinProbability: 0.50,
		},
	}
	if _, err := ltc.Save(context.Background(), cfg, 0); err != nil {
		t.Fatalf("打开商机阶段失败: %v", err)
	}
}

func seedProfile(t *testing.T, database *gorm.DB, salesID string) {
	t.Helper()
	ev := repository.NewSalesEventRepositoryWithDB(database)
	// 同一个人两条档案事件：名单必须把他算作一个人（重复计人会把负载均衡算歪）。
	for _, at := range []time.Time{time.Now().Add(-48 * time.Hour), time.Now().Add(-time.Hour)} {
		if err := ev.Create(context.Background(), &model.SalesEvent{
			EventType: model.SalesEventTypeSalesProfile, OwnerID: salesID,
			SalesName: salesID, OccurredAt: at,
		}); err != nil {
			t.Fatalf("写入销售档案事件失败: %v", err)
		}
	}
}

func TestInitOpportunityRuntimeWiresConverter(t *testing.T) {
	prevConv := service.GlobalOpportunityConverter()
	prevLTC := service.GlobalLTCConfig()
	t.Cleanup(func() {
		service.SetGlobalOpportunityConverter(prevConv)
		service.SetGlobalLTCConfig(prevLTC)
	})

	database := converterTestDB(t)
	turnOnOpportunityStage(t)
	seedProfile(t, database, "carol")
	seedProfile(t, database, "erin")

	if svc := InitOpportunityRuntime(database); svc == nil {
		t.Fatal("有 DB 句柄却没装商机底座")
	}
	conv := service.GlobalOpportunityConverter()
	if conv == nil {
		t.Fatal("InitOpportunityRuntime 没登记全局转换器：挖掘那条接缝在生产环境永远是空转")
	}
	if !conv.Available() {
		t.Fatal("登记了转换器但 Available() 为假：装配少了一句柄")
	}

	res, err := conv.ConvertFromClue(context.Background(), service.OpportunityConversion{
		ClueID: "clue-wiring-1", CustomerID: "cust-wiring", OneID: "phone:13800000000",
		LeadScore: 88, Confidence: 0.91,
	})
	if err != nil {
		t.Fatalf("装配出来的转换器转化失败: %v", err)
	}
	if !res.Created {
		t.Fatalf("装配出来的转换器没建单：%s %s", res.GateReason, res.GateDetail)
	}
	// 归属必须是库里那两位之一（字典序取 carol）：空归属=no_roster，
	// 而 no_roster 恰恰是"名单句柄接错库"最容易被漏掉的表征。
	if res.Opportunity.OwnerUserID != "carol" {
		t.Errorf("自动分配到 %q，期望 carol（在册名单没接对底座时会是空串）", res.Opportunity.OwnerUserID)
	}
	var stored model.Opportunity
	if err := database.Where("id = ?", res.Opportunity.ID).First(&stored).Error; err != nil {
		t.Fatalf("转化的那一行没写进这把库: %v", err)
	}
	if stored.ClueID != "clue-wiring-1" {
		t.Errorf("库里的幂等键是 %q，期望 clue-wiring-1", stored.ClueID)
	}
	// 审计必须落在同一把库：否则"这单凭什么建"在生产环境查无实据。
	var audits int64
	if err := database.Raw(`SELECT COUNT(*) FROM operation_logs WHERE module = ? AND resource_id = ?`,
		service.OpportunityConvertAuditModule, "clue-wiring-1").Scan(&audits).Error; err != nil {
		t.Fatalf("查审计失败: %v", err)
	}
	if audits != 1 {
		t.Errorf("转化审计 %d 行，期望 1 行（装配时漏接审计句柄就在这里红）", audits)
	}
}

func TestInitOpportunityRuntimeClearsConverterWithoutDB(t *testing.T) {
	prevConv := service.GlobalOpportunityConverter()
	prevOpp := service.GlobalOpportunityService()
	t.Cleanup(func() {
		service.SetGlobalOpportunityConverter(prevConv)
		service.SetGlobalOpportunityService(prevOpp)
	})

	database := converterTestDB(t)
	InitOpportunityRuntime(database)
	if service.GlobalOpportunityConverter() == nil {
		t.Fatal("前置条件不成立：带库装配之后全局转换器是空的")
	}

	if got := InitOpportunityRuntime(nil); got != nil {
		t.Errorf("无库时返回了实例 %v", got)
	}
	if conv := service.GlobalOpportunityConverter(); conv != nil {
		t.Error("无库装配只清了底座、没清转换器：挖掘会往一张不该再写的表里继续写行")
	}
	if svc := service.GlobalOpportunityService(); svc != nil {
		t.Error("无库装配没清商机底座全局实例")
	}
}
