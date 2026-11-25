package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

func seedHub(platform, sender, name, content string, idx int) model.MessageHub {
	return model.MessageHub{
		TenantID:       tenant.DefaultTenantID,
		MsgID:          fmt.Sprintf("seed-%s-%d", sender, idx),
		Platform:       platform,
		AccountID:      "integration-account",
		SenderID:       sender,
		SenderName:     name,
		Direction:      "inbound",
		MsgType:        "text",
		Content:        content,
		ConversationID: "conv-" + sender,
		SentAt:         time.Now().Add(-time.Duration(10-idx) * time.Minute),
	}
}

// 集成夹具必须具备真实渠道账号归属；禁云与能力表恢复仅限本用例。
func newLeadIntegrationDB(t *testing.T, platform string) *gorm.DB {
	t.Helper()
	setupLeadHistoryTest(t)
	database := testutil.NewTestDB(t, &model.LeadMiningConfig{}, &model.MessageHub{},
		&model.Customer{}, &model.Clue{}, &model.LeadMiningJudgement{}, &model.LeadMiningHit{},
		&model.LeadMiningRule{}, &model.Channel{}, &model.ChannelAccount{}, &model.ChannelIdentifyCode{}, &model.ConversationContext{})
	if err := database.Exec("CREATE UNIQUE INDEX IF NOT EXISTS uq_conv_ctx_tenant_account_conv ON conversation_contexts (tenant_id, channel_account_id, external_conversation_id)").Error; err != nil {
		t.Fatal(err)
	}
	ctx := tenant.OnlyTenant(context.Background(), tenant.DefaultTenantID)
	channel := model.Channel{TenantID: tenant.DefaultTenantID, Code: platform, Type: platform, Name: platform}
	if err := database.WithContext(ctx).Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	account := model.ChannelAccount{TenantID: tenant.DefaultTenantID, ChannelID: channel.ID, Name: "integration-account", Status: 1, DataScope: "{}"}
	if err := database.WithContext(ctx).Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	svc := &Service{channelResolver: NewChannelResolverWithRepos(repository.NewChannelAccountRepositoryWithDB(database), repository.NewChannelRepositoryWithDB(database))}
	hub := seedHub(platform, "fixture-peer", "夹具", "消息", 1)
	if scope, err := svc.historyScope(ctx, &hub); err != nil || scope.accountID != account.ID {
		t.Fatalf("集成账号必须自证到真实账号：%+v err=%v", scope, err)
	}
	return database
}

func leadIntegrationResolver(database *gorm.DB) *ChannelResolver {
	return NewChannelResolverWithRepos(repository.NewChannelAccountRepositoryWithDB(database), repository.NewChannelRepositoryWithDB(database))
}

// 真实管道构造：配置落库 + 多轮消息落库 + 真实 fetchHistoryDB + 真实写库
func TestLeadMining_Integration_FullPipeline(t *testing.T) {
	database := newLeadIntegrationDB(t, "telegram")
	db.SetTestDB(database)

	ctx := tenant.OnlyTenant(context.Background(), tenant.DefaultTenantID)
	cfgRepo := repository.NewLeadMiningConfigRepository()

	cfg := &model.LeadMiningConfig{
		Enabled:  true,
		Keywords: model.JSONStrings{"购买", "代理", "报价", "合作"},
		Tags:     model.JSONStrings{"高意向", "待跟进"},
		// P0-c：Channels 现在是显式关闭名单；留空 = 全纳入（telegram 也参与）。
		Channels:       model.JSONStrings{},
		Requirement:    "客户明确表达购买意向或咨询代理/报价，即视为线索",
		MinIntentScore: 50,
	}
	if err := cfgRepo.Save(ctx, cfg); err != nil {
		t.Fatalf("保存配置失败: %v", err)
	}
	got, err := cfgRepo.GetSingleton(ctx)
	if err != nil || got == nil || !got.Enabled {
		t.Fatalf("配置未正确落库: err=%v got=%+v", err, got)
	}
	if len(got.Keywords) != 4 {
		t.Fatalf("配置关键词未持久化: %v", got.Keywords)
	}

	sender := "integration_lead_001"
	cleanupLeadMiningData(t, database, sender)
	msgs := []model.MessageHub{
		seedHub("telegram", sender, "王经理", "你好，在吗", 1),
		seedHub("telegram", sender, "王经理", "我想了解一下你们的产品报价", 2),
		seedHub("telegram", sender, "王经理", "如果合适我想做区域代理", 3),
		seedHub("telegram", sender, "王经理", "大概预算 5 万左右", 4),
	}
	for i := range msgs {
		if err := database.Create(&msgs[i]).Error; err != nil {
			t.Fatalf("构造消息失败: %v", err)
		}
	}
	out := seedHub("telegram", sender, "王经理", "好的，我给您发资料", 5)
	out.Direction = "outbound"
	if err := database.Create(&out).Error; err != nil {
		t.Fatalf("构造出站消息失败: %v", err)
	}

	judge := &fakeJudge{resp: &LeadJudgement{
		IsLead:          true,
		IntentScore:     85,
		MatchedKeywords: []string{"报价", "代理"},
		MatchedTags:     []string{"AI兴趣"},
		Summary:         "王经理咨询产品报价并表达区域代理意向，预算约5万",
		Confidence:      0.95,
	}}
	hubRepo := repository.NewMessageHubRepository()
	repository.SetMessageHubRepoDB(hubRepo, database)
	s := &Service{
		judge:           judge,
		channelResolver: leadIntegrationResolver(database),
		custRepo:        repository.NewCustomerRepository(),
		clueRepo:        repository.NewClueRepositoryWithDB(database),
		cfgRepo:         cfgRepo,
		hubRepo:         hubRepo,
		lastJudge:       map[string]time.Time{},
	}

	last := msgs[len(msgs)-1]
	s.process(ctx, &last)

	account := "telegram:" + sender
	var clue model.Clue
	if err := database.Where("type = ? AND account = ?", ClueTypeLeadMining, account).First(&clue).Error; err != nil {
		t.Fatalf("未写入线索库存: %v", err)
	}
	if clue.IntentScore != 85 {
		t.Fatalf("线索意向分异常: %d", clue.IntentScore)
	}
	if clue.SourceID != "lead_mining" {
		t.Fatalf("线索来源异常: %s", clue.SourceID)
	}
	if clue.IsOpportunity != 1 {
		t.Fatalf("高意向(>=70)应标记商机，实际 %d", clue.IsOpportunity)
	}
	if clue.OneID == "" {
		t.Fatalf("线索 OneID 不应为空")
	}

	var cust model.Customer
	if err := database.Where("unified_id = ?", clue.OneID).First(&cust).Error; err != nil {
		t.Fatalf("未创建客户: %v", err)
	}
	if cust.UnifiedID != clue.OneID {
		t.Fatalf("线索与客户 OneID 不一致: clue=%s cust=%s", clue.OneID, cust.UnifiedID)
	}
	var tags []string
	_ = json.Unmarshal([]byte(cust.Tags), &tags)
	if !containsTag(tags, "高意向") || !containsTag(tags, "待跟进") || !containsTag(tags, "AI兴趣") {
		t.Fatalf("客户标签缺失，实际: %v", tags)
	}

	s.lastJudge = map[string]time.Time{}
	s.process(ctx, &last)
	var cnt int64
	database.Model(&model.Clue{}).Where("type = ? AND account = ?", ClueTypeLeadMining, account).Count(&cnt)
	if cnt != 1 {
		t.Fatalf("去重失败：同账号线索应只有 1 条，实际 %d", cnt)
	}
}

// 非线索场景：构造闲聊消息，判定为 false，不应写库
func TestLeadMining_Integration_NotLead(t *testing.T) {
	database := newLeadIntegrationDB(t, "telegram")
	db.SetTestDB(database)

	ctx := tenant.OnlyTenant(context.Background(), tenant.DefaultTenantID)
	cfgRepo := repository.NewLeadMiningConfigRepository()
	cfg := &model.LeadMiningConfig{Enabled: true, MinIntentScore: 50}
	if err := cfgRepo.Save(ctx, cfg); err != nil {
		t.Fatal(err)
	}

	sender := "integration_chat_002"
	cleanupLeadMiningData(t, database, sender)
	msg := seedHub("telegram", sender, "小李", "今天天气真不错", 1)
	if err := database.Create(&msg).Error; err != nil {
		t.Fatal(err)
	}

	judge := &fakeJudge{resp: &LeadJudgement{IsLead: false, IntentScore: 10}}
	s := &Service{
		judge:           judge,
		channelResolver: leadIntegrationResolver(database),
		custRepo:        repository.NewCustomerRepository(),
		clueRepo:        repository.NewClueRepositoryWithDB(database),
		cfgRepo:         cfgRepo,
		hubRepo:         repository.NewMessageHubRepositoryWithDB(database),
		lastJudge:       map[string]time.Time{},
	}
	s.process(ctx, &msg)
	if judge.calls != 1 {
		t.Fatalf("非线索分支必须真实执行判定，实际 %d 次", judge.calls)
	}

	var cnt int64
	database.Model(&model.Clue{}).Where("type = ?", ClueTypeLeadMining).Count(&cnt)
	if cnt != 0 {
		t.Fatalf("非线索不应写库，实际 %d 条", cnt)
	}
}

// 异步非阻塞路径：Enqueue 后由 worker 在真实 DB 中落地线索
func TestLeadMining_Integration_AsyncEnqueue(t *testing.T) {
	database := newLeadIntegrationDB(t, "telegram")
	db.SetTestDB(database)

	ctx := tenant.OnlyTenant(context.Background(), tenant.DefaultTenantID)
	cfgRepo := repository.NewLeadMiningConfigRepository()
	cfg := &model.LeadMiningConfig{Enabled: true, Tags: model.JSONStrings{"高意向"}, MinIntentScore: 50}
	if err := cfgRepo.Save(ctx, cfg); err != nil {
		t.Fatal(err)
	}

	sender := "integration_async_003"
	cleanupLeadMiningData(t, database, sender)
	msg := seedHub("telegram", sender, "赵总", "我想购买你们的企业版", 1)
	if err := database.Create(&msg).Error; err != nil {
		t.Fatal(err)
	}

	judge := &fakeJudge{resp: &LeadJudgement{IsLead: true, IntentScore: 75, MatchedTags: []string{"AI兴趣"}}}
	ctxCancel, cancel := context.WithCancel(ctx)
	hubRepo := repository.NewMessageHubRepository()
	repository.SetMessageHubRepoDB(hubRepo, database)
	s := &Service{
		queue:           make(chan *model.MessageHub, 16),
		workers:         1,
		judge:           judge,
		channelResolver: leadIntegrationResolver(database),
		custRepo:        repository.NewCustomerRepository(),
		clueRepo:        repository.NewClueRepositoryWithDB(database),
		cfgRepo:         cfgRepo,
		hubRepo:         hubRepo,
		cancel:          cancel,
		lastJudge:       map[string]time.Time{},
	}
	s.wg.Add(1)
	go s.worker(ctxCancel)
	defer s.Stop()

	s.Enqueue(&msg)

	account := "telegram:" + sender
	deadline := time.Now().Add(3 * time.Second)
	for {
		var cnt int64
		database.Model(&model.Clue{}).Where("type = ? AND account = ?", ClueTypeLeadMining, account).Count(&cnt)
		if cnt >= 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("异步 worker 未在 3s 内将线索落地到真实 DB")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if judge.calls != 1 {
		t.Fatalf("异步路径应判定 1 次，实际 %d", judge.calls)
	}
}

// 去抖：同客户窗口内多条消息只判定一次（真实管道）
func TestLeadMining_Integration_Debounce(t *testing.T) {
	database := newLeadIntegrationDB(t, "telegram")
	db.SetTestDB(database)

	ctx := tenant.OnlyTenant(context.Background(), tenant.DefaultTenantID)
	cfgRepo := repository.NewLeadMiningConfigRepository()
	cfg := &model.LeadMiningConfig{Enabled: true, MinIntentScore: 50}
	if err := cfgRepo.Save(ctx, cfg); err != nil {
		t.Fatal(err)
	}

	sender := "integration_deb_004"
	cleanupLeadMiningData(t, database, sender)
	for i := 1; i <= 3; i++ {
		m := seedHub("telegram", sender, "钱总", fmt.Sprintf("消息%d：我想咨询代理", i), i)
		if err := database.Create(&m).Error; err != nil {
			t.Fatal(err)
		}
	}

	judge := &fakeJudge{resp: &LeadJudgement{IsLead: true, IntentScore: 80}}
	hubRepo := repository.NewMessageHubRepository()
	repository.SetMessageHubRepoDB(hubRepo, database)
	s := &Service{
		judge:           judge,
		channelResolver: leadIntegrationResolver(database),
		custRepo:        repository.NewCustomerRepository(),
		clueRepo:        repository.NewClueRepositoryWithDB(database),
		cfgRepo:         cfgRepo,
		hubRepo:         hubRepo,
		lastJudge:       map[string]time.Time{},
	}
	for i := 1; i <= 3; i++ {
		var m model.MessageHub
		database.Where("msg_id = ?", fmt.Sprintf("seed-%s-%d", sender, i)).First(&m)
		s.process(ctx, &m)
	}
	if judge.calls != 1 {
		t.Fatalf("同客户窗口内应只判定 1 次，实际 %d", judge.calls)
	}
	var cnt int64
	database.Model(&model.Clue{}).Where("type = ?", ClueTypeLeadMining).Count(&cnt)
	if cnt != 1 {
		t.Fatalf("去抖后应只写 1 条线索，实际 %d", cnt)
	}
}

func containsTag(tags []string, v string) bool {
	for _, x := range tags {
		if strings.EqualFold(strings.TrimSpace(x), strings.TrimSpace(v)) {
			return true
		}
	}
	return false
}

func cleanupLeadMiningData(t *testing.T, database *gorm.DB, senders ...string) {
	t.Helper()
	t.Cleanup(func() {
		for _, s := range senders {
			account := "telegram:" + s
			database.Where("type = ? AND account = ?", ClueTypeLeadMining, account).Delete(&model.Clue{})
			database.Where("source_account_id = ?", s).Delete(&model.Customer{})
			database.Where("sender_id = ?", s).Delete(&model.MessageHub{})
			accountD := "douyin:" + s
			database.Where("type = ? AND account = ?", ClueTypeLeadMining, accountD).Delete(&model.Clue{})
			database.Where("source_account_id = ?", s).Delete(&model.Customer{})
		}
		database.Delete(&model.LeadMiningConfig{})
	})
}
