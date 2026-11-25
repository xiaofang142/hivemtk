package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

type leadScopeJudge struct{ histories [][]llm.ChatMessage }

func (j *leadScopeJudge) Judge(_ context.Context, _ *model.LeadMiningConfig, history []llm.ChatMessage) (*LeadJudgement, error) {
	j.histories = append(j.histories, history)
	return &LeadJudgement{IsLead: false}, nil
}

func disableLeadScopeCloud(t *testing.T) {
	t.Helper()
	d := llm.GetGlobalDispatcher()
	for _, name := range d.GetEnabledProviderNames() {
		original := *d.GetProvider(name)
		disabled := original
		disabled.Enabled = false
		d.AddProvider(disabled)
		t.Cleanup(func() { d.AddProvider(original) })
	}
	if enabled := d.GetEnabledProviderNames(); len(enabled) != 0 {
		t.Fatalf("隔离测试不得保留启用中的云端厂商：%v", enabled)
	}
}

// 恢复全局能力表，避免已有投影测试遗留的非空集合误挡 telegram。
func setupLeadHistoryTest(t *testing.T) {
	t.Helper()
	disableLeadScopeCloud(t)
	leadMiningMu.Lock()
	previous := leadMiningCapableChannels
	leadMiningCapableChannels = map[string]struct{}{"telegram": {}, "douyin": {}, "qq": {}, "personal_wx": {}}
	leadMiningMu.Unlock()
	t.Cleanup(func() {
		leadMiningMu.Lock()
		leadMiningCapableChannels = previous
		leadMiningMu.Unlock()
	})
}

// 名称/数值ID/识别码必须归一到同一真实账号；外租户及错平台识别码不能扩权。
func TestLeadHistory_CanonicalAccountAliases(t *testing.T) {
	setupLeadHistoryTest(t)
	database := testutil.NewTestDB(t, &model.Channel{}, &model.ChannelAccount{}, &model.ChannelIdentifyCode{},
		&model.MessageHub{}, &model.ConversationContext{}, &model.LeadMiningRule{}, &model.LeadMiningHit{}, &model.LeadMiningJudgement{})
	// 复用生产迁移 094 的冲突目标，AutoMigrate 不会创建该手写唯一索引。
	if err := database.Exec("CREATE UNIQUE INDEX IF NOT EXISTS uq_conv_ctx_tenant_account_conv ON conversation_contexts (tenant_id, channel_account_id, external_conversation_id)").Error; err != nil {
		t.Fatal(err)
	}
	ctx := tenant.OnlyTenant(context.Background(), 51)
	channel := model.Channel{TenantID: 51, Code: "telegram", Name: "TG", Type: "telegram"}
	if err := database.WithContext(ctx).Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	a := model.ChannelAccount{TenantID: 51, ChannelID: channel.ID, Name: "account-a", IdentifyCode: "cid_history_a"}
	b := model.ChannelAccount{TenantID: 51, ChannelID: channel.ID, Name: "account-b", IdentifyCode: "cid_history_b"}
	foreign := model.ChannelAccount{TenantID: 52, ChannelID: channel.ID, Name: "foreign", IdentifyCode: "cid_history_foreign"}
	for _, acc := range []*model.ChannelAccount{&a, &b, &foreign} {
		acc.DataScope = "{}"
		if err := database.WithContext(tenant.OnlyTenant(context.Background(), acc.TenantID)).Create(acc).Error; err != nil {
			t.Fatal(err)
		}
	}
	refs := []string{strconv.FormatUint(uint64(a.ID), 10), a.Name, a.IdentifyCode, b.Name}
	for i, ref := range refs {
		row := model.MessageHub{TenantID: 51, AccountID: ref, Platform: "telegram", MsgID: fmt.Sprintf("alias-%d", i),
			ConversationID: "same", SenderID: "peer", Direction: "inbound", Content: fmt.Sprintf("消息%d", i), SentAt: time.Now().Add(time.Duration(i) * time.Second)}
		if err := database.WithContext(ctx).Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	resolver := NewChannelResolverWithRepos(repository.NewChannelAccountRepositoryWithDB(database), repository.NewChannelRepositoryWithDB(database))
	s := &Service{hubRepo: repository.NewMessageHubRepositoryWithDB(database), channelResolver: resolver}
	// 生产构造默认解析器；此处显式仓储注入，避免其他测试的全局 DB 污染。
	hub := &model.MessageHub{TenantID: 51, Platform: "telegram", AccountID: a.Name, ConversationID: "same",
		SenderID: "peer", Direction: "inbound", Content: "A本轮消息", MsgID: "current-a"}
	history := s.fetchHistoryDB(ctx, hub)
	if len(history) != 3 {
		t.Fatalf("A的三种合法引用应合并，实际 %+v", history)
	}
	for i, message := range history {
		if message.Content != fmt.Sprintf("消息%d", i) {
			t.Fatalf("历史应按时间升序，实际 %+v", history)
		}
	}
	scope, err := s.historyScope(ctx, hub)
	if err != nil || scope.accountID != a.ID {
		t.Fatalf("真实账号解析失败：%+v err=%v", scope, err)
	}
	key := leadMiningDebounceKey(51, "telegram", scope.accountKey, "same", "", "peer", false)
	for _, ref := range refs[:3] {
		copy := *hub
		copy.AccountID = ref
		aliasScope, err := s.historyScope(ctx, &copy)
		if err != nil || leadMiningDebounceKey(51, "telegram", aliasScope.accountKey, "same", "", "peer", false) != key {
			t.Fatalf("同一账号别名应共用节流窗口：%q %+v err=%v", ref, aliasScope, err)
		}
	}
	ctxRepo := repository.NewConversationContextRepositoryWithDB(database)
	for _, acc := range []model.ChannelAccount{a, b} {
		h := *hub
		h.AccountID = acc.Name
		saveCompactSummary(ctx, &h, acc.ID, &LeadJudgement{Summary: acc.Name})
	}
	for _, ref := range refs[:3] {
		h := *hub
		h.AccountID = ref
		scope, err := s.historyScope(ctx, &h)
		if err != nil {
			t.Fatal(err)
		}
		conv, err := leadScopedConversation(ctx, ctxRepo, scope.accountID, leadHistoryConversationID(&h))
		if err != nil || conv.Summary != "诉求:"+a.Name {
			t.Fatalf("账号别名应只读同账号摘要：%+v err=%v", conv, err)
		}
	}
	j := &leadScopeJudge{}
	s.judge, s.cfgRepo, s.lastJudge = j, &fakeCfgRepo{cfg: &model.LeadMiningConfig{Enabled: true}}, map[string]time.Time{}
	s.process(ctx, hub)
	other := *hub
	other.AccountID = b.Name
	other.Content, other.MsgID = "B本轮消息", "current-b"
	s.process(ctx, &other)
	alias := *hub
	alias.AccountID = refs[0]
	s.process(ctx, &alias)
	if len(j.histories) != 2 || len(j.histories[0]) != 3 || len(j.histories[1]) != 1 {
		t.Fatalf("真实账号 A/B 的 process 历史与节流应独立、别名共窗：%+v", j.histories)
	}
	for _, acc := range []model.ChannelAccount{a, b} {
		conv, err := ctxRepo.Get(ctx, acc.ID, leadHistoryConversationID(hub))
		want := "A本轮消息"
		if acc.ID == b.ID {
			want = other.Content
		}
		if err != nil || len(conv.Messages) != 1 || conv.Messages[0].Content != want {
			t.Fatalf("process 共享上下文应使用本次已校验的真实账号：%+v err=%v", conv, err)
		}
	}
	for _, ref := range []string{"", foreign.IdentifyCode} {
		h := *hub
		h.AccountID = ref
		s.process(ctx, &h)
	}
	if len(j.histories) != 2 {
		t.Fatalf("空账号与跨租户真实账号不得进入Judge：%+v", j.histories)
	}
	for _, ref := range []string{"", foreign.IdentifyCode} {
		copy := *hub
		copy.AccountID = ref
		if rows := s.fetchHistoryDB(ctx, &copy); len(rows) != 0 {
			t.Fatalf("无效归属 %q 不得取证，实际 %+v", ref, rows)
		}
	}
	wrongPlatform := *hub
	wrongPlatform.AccountID, wrongPlatform.Platform = a.IdentifyCode, "qq"
	if scope, err := s.historyScope(ctx, &wrongPlatform); err == nil || scope != nil {
		t.Fatalf("识别码错平台必须拒绝：%+v err=%v", scope, err)
	}
	if scope, err := s.historyScope(tenant.OnlyTenant(context.Background(), 52), hub); err == nil || scope != nil {
		t.Fatalf("消息与上下文跨租户必须拒绝：%+v err=%v", scope, err)
	}
	sharedCode := model.ChannelIdentifyCode{TenantID: 51, ChannelID: channel.ID, Code: "cid_history_shared"}
	if err := database.WithContext(ctx).Create(&sharedCode).Error; err != nil {
		t.Fatal(err)
	}
	ambiguous := *hub
	ambiguous.AccountID = sharedCode.Code
	if scope, err := s.historyScope(ctx, &ambiguous); err == nil || scope != nil {
		t.Fatalf("渠道识别码对应多个账号必须拒绝：%+v err=%v", scope, err)
	}
	// B 的外部名称若恰好等于 A 的数值 ID，历史原始引用无法证明属于谁。
	if err := database.WithContext(ctx).Model(&b).Update("name", refs[0]).Error; err != nil {
		t.Fatal(err)
	}
	ambiguous.AccountID = refs[0]
	if scope, err := s.historyScope(ctx, &ambiguous); err == nil || scope != nil {
		t.Fatalf("数值ID与他账号名称冲突必须拒绝：%+v err=%v", scope, err)
	}
	if history := s.fetchHistoryDB(ctx, hub); len(history) != 2 {
		t.Fatalf("合法名称读取不能合并歧义数值别名：%+v", history)
	}
	// 群ID才是群历史/窗口边界，重复或缺失的 conversation_id 不能覆盖其他群或私聊摘要。
	for _, groupID := range []string{"group-a", "group-b", hub.ConversationID} {
		group := *hub
		group.IsGroup, group.GroupID = true, groupID
		saveCompactSummary(ctx, &group, a.ID, &LeadJudgement{Summary: groupID})
		conv, err := ctxRepo.Get(ctx, a.ID, leadHistoryConversationID(&group))
		if err != nil || conv.Summary != "诉求:"+groupID {
			t.Fatalf("群摘要必须按群ID独立：%+v err=%v", conv, err)
		}
	}
	private, err := ctxRepo.Get(ctx, a.ID, leadHistoryConversationID(hub))
	if err != nil || private.Summary != "诉求:"+a.Name {
		t.Fatalf("群摘要不能覆盖私聊摘要：%+v err=%v", private, err)
	}
	// 历史旧键缺平台/类型证明，不能作为兼容回退混入本次判断。
	if err := ctxRepo.Upsert(ctx, &model.ConversationContext{ChannelAccountID: a.ID,
		ExternalConversationID: "legacy", Summary: "旧键混合摘要"}); err != nil {
		t.Fatal(err)
	}
	for _, conversationID := range []string{"legacy", strings.Repeat("长", 128)} {
		h := *hub
		h.ConversationID = conversationID
		conv, err := ctxRepo.Get(ctx, a.ID, leadHistoryConversationID(&h))
		if err != nil || conv.Summary != "" {
			t.Fatalf("新作用域不得回读旧键摘要：%+v err=%v", conv, err)
		}
		saveCompactSummary(ctx, &h, a.ID, &LeadJudgement{Summary: "新摘要"})
		conv, err = ctxRepo.Get(ctx, a.ID, leadHistoryConversationID(&h))
		if err != nil || conv.Summary != "诉求:新摘要" {
			t.Fatalf("长外部会话ID也应可持久化摘要：%+v err=%v", conv, err)
		}
	}
}

// 同租户同会话同发送者的两个账号必须各自判定，同群成员则共用同一节流窗口。
func TestLeadHistory_ProcessAccountDebounce(t *testing.T) {
	setupLeadHistoryTest(t)
	t.Setenv("AI_DEBOUNCE_SECONDS", "30")
	previous := leadDebounceSingleton
	leadDebounceSingleton = NewLeadDebounceService()
	t.Cleanup(func() { leadDebounceSingleton = previous })
	for _, hash := range []string{"", "same-hash"} {
		t.Run("dedup="+hash, func(t *testing.T) {
			j := &leadScopeJudge{}
			s := &Service{judge: j, cfgRepo: &fakeCfgRepo{cfg: &model.LeadMiningConfig{Enabled: true}},
				historyFetcher: cannedHistory(), lastJudge: map[string]time.Time{}}
			a := sampleHub("telegram", "peer", "A")
			a.TenantID, a.AccountID = 51, "unknown-a"
			a.DedupHash = hash
			b := *a
			b.AccountID = "unknown-b"
			s.process(context.Background(), a)
			s.process(context.Background(), &b)
			s.process(context.Background(), a)
			if len(j.histories) != 2 {
				t.Fatalf("两个账号应各判定一次，实际 %d", len(j.histories))
			}
			otherConversation := *a
			otherConversation.ConversationID = "another"
			s.process(context.Background(), &otherConversation)
			if len(j.histories) != 3 {
				t.Fatalf("同账号同发送者的不同会话不能互抑制，实际 %d", len(j.histories))
			}
			groupA := *a
			groupA.IsGroup, groupA.GroupID = true, "same-group"
			groupB := groupA
			groupB.SenderID = "member-b"
			s.process(context.Background(), &groupA)
			s.process(context.Background(), &groupB)
			groupOtherAccount := groupA
			groupOtherAccount.AccountID = "unknown-b"
			s.process(context.Background(), &groupOtherAccount)
			if len(j.histories) != 5 {
				t.Fatalf("同群成员共窗，不同账号同群独立，实际 %d", len(j.histories))
			}
			missingAccount := *a
			missingAccount.AccountID = ""
			s.process(context.Background(), &missingAccount)
			s.process(tenant.OnlyTenant(context.Background(), 52), a)
			if len(j.histories) != 5 {
				t.Fatalf("空账号及跨租户上下文不得进入Judge，实际 %d", len(j.histories))
			}
		})
	}
}

// 真实仓储路径不能因相同 conversation_id 将别的来源账号读进模型上下文。
func TestLeadHistory_DBAccountScope(t *testing.T) {
	setupLeadHistoryTest(t)
	database := testutil.NewTestDB(t, &model.MessageHub{}, &model.ConversationContext{})
	ctx := tenant.OnlyTenant(context.Background(), 51)
	a := sampleHub("douyin", "peer", "A消息")
	a.ID, a.TenantID, a.AccountID, a.MsgID = 0, 51, "unknown-a", "a"
	a.SentAt = time.Now()
	b := *a
	b.AccountID, b.MsgID, b.Content = "unknown-b", "b", "B消息"
	if err := database.WithContext(ctx).Create(a).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.WithContext(ctx).Create(&b).Error; err != nil {
		t.Fatal(err)
	}
	s := &Service{hubRepo: repository.NewMessageHubRepositoryWithDB(database)}
	history := s.fetchHistoryDB(ctx, a)
	if len(history) != 1 || history[0].Content != "A消息" {
		t.Fatalf("A历史不得包含B，实际 %+v", history)
	}
	empty := *a
	empty.AccountID = ""
	if rows := s.fetchHistoryDB(ctx, &empty); len(rows) != 0 {
		t.Fatalf("空账号必须拒绝取证，实际 %+v", rows)
	}
	scope, err := s.historyScope(ctx, a)
	if err != nil || scope.accountID != 0 || len(scope.AccountIDs) != 1 || scope.AccountIDs[0] != a.AccountID {
		t.Fatalf("解析失败只能显式降级到精确原始账号，实际 %+v err=%v", scope, err)
	}
	if conv, err := leadScopedConversation(ctx, nil, scope.accountID, a.ConversationID); err == nil || conv != nil {
		t.Fatalf("解析失败不得读取账号0的共享上下文：%+v err=%v", conv, err)
	}
	if rows := s.fetchHistoryDB(context.Background(), a); len(rows) != 0 {
		t.Fatalf("缺租户上下文不能读取历史：%+v", rows)
	}
	outbound := *a
	outbound.ID, outbound.MsgID = 0, "a-out"
	outbound.Direction, outbound.SenderID, outbound.ReceiverID, outbound.Content = "outbound", "self", "peer", "人工发出"
	// 同时间按主键稳定排序，服务反转为时间正序，不能遗漏人工发出。
	if err := database.WithContext(ctx).Create(&outbound).Error; err != nil {
		t.Fatal(err)
	}
	if history := s.fetchHistoryDB(ctx, a); len(history) != 2 || history[0].Content != a.Content || history[1].Content != outbound.Content || history[1].Role != "assistant" {
		t.Fatalf("私聊应按稳定正序包含客户与人工双方：%+v", history)
	}
	for _, account := range []string{a.AccountID, b.AccountID} {
		for i, sender := range []string{"member-a", "member-b", "self"} {
			row := *a
			row.ID, row.AccountID, row.SenderID, row.SenderName = 0, account, sender, sender
			row.IsGroup, row.GroupID = true, "shared-group"
			row.MsgID, row.Content = fmt.Sprintf("%s-group-%d", account, i), fmt.Sprintf("%s-%d", account, i)
			if i == 2 {
				row.Direction = "outbound"
			}
			if err := database.WithContext(ctx).Create(&row).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	group := *a
	group.IsGroup, group.GroupID = true, "shared-group"
	history = s.fetchHistoryDB(ctx, &group)
	if len(history) != 3 {
		t.Fatalf("群应只含来源账号的全部已采集成员：%+v", history)
	}
	for i, sender := range []string{"member-a", "member-b", "self"} {
		if history[i].Content != fmt.Sprintf("[%s] %s-%d", sender, a.AccountID, i) {
			t.Fatalf("群成员及人工消息应保持稳定正序：%+v", history)
		}
	}
	// 独立 raw-source 取证不能拆散既有稳定身份的跨账号客户/线索关联。
	j := &fakeJudge{resp: &LeadJudgement{IsLead: true, IntentScore: 80}}
	svc := newTestService(t, &model.LeadMiningConfig{Enabled: true, MinIntentScore: 50}, j)
	svc.hubRepo, svc.historyFetcher = s.hubRepo, nil
	svc.process(ctx, a)
	svc.process(ctx, &b)
	if j.calls != 2 || len(svc.custRepo.(*fakeCustRepo).byID) != 1 || len(svc.clueRepo.(*fakeClueRepo).byTypeAcct) != 1 {
		t.Fatalf("独立取证仍应复用同一客户/线索关联：calls=%d customers=%+v clues=%+v", j.calls, svc.custRepo, svc.clueRepo)
	}
	var shared int64
	if err := database.Model(&model.ConversationContext{}).Count(&shared).Error; err != nil || shared != 0 {
		t.Fatalf("raw-source-only 账号不得读写共享摘要：rows=%d err=%v", shared, err)
	}
}
