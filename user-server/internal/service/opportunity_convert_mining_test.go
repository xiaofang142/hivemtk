// opportunity_convert_mining_test.go T-P4-05 的**接缝**：线索挖掘写完之后转商机。
//
// 为什么这条接缝要落在真库上：本卡的 AC 全在讲"转换层做到什么"，而这个竖历史上死掉的
// 方式不是转换层算错，是**没人调用它**（M33 教训，判据见 check-unwired-assets.sh 的账本）。
// 用记录型替身只能证明"persistLead 里有一行调用"，证明不了那次调用真能把行写进去：
// 幂等键取的是刚由 BeforeCreate 生成的 clue.ID，传错成 account 或 hub.ID 时，
// 替身照样会老老实实记下"被调过一次"。所以这里跑真 clue 仓储 + 真转换器 + 真库，
// 只把没有 WithDB 构造器的客户仓储换成替身（客户行对接缝而言只是两个 ID 的来源）。
package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// miningCust 客户身份替身：固定返回同一个客户。
// 只嵌接口、只覆盖 resolveCustomer 真正会调的那一个方法，其余方法一旦被接缝用上就会
// nil panic —— 那是有意的：它把"接缝悄悄多依赖了一个客户查询"暴露成红，而不是绿。
type miningCust struct {
	repository.CustomerRepository
	c *model.Customer
}

func (m miningCust) GetByUnifiedID(ctx context.Context, id string) (*model.Customer, error) {
	return m.c, nil
}

// miningClueCreateFails 只把 Create 换成必然失败，其余走真仓储。
// ID 必须照真仓储的形状填上：model.Clue 的 BeforeCreate 钩子在 INSERT 之前就跑完了，
// 所以"写入失败"的真实形态是**线索号已经生成、行却没进去**。少了这一句，本替身会退化成
// "clue_id 是空串"，接缝那道口就会以"空 clueID 被转换器拒掉"为由假绿。
type miningClueCreateFails struct {
	repository.ClueRepository
}

func (f miningClueCreateFails) Create(ctx context.Context, c *model.Clue) error {
	c.ID = "clue-create-failed"
	return errors.New("boom: 线索写入失败")
}

func newMiningService(clueRepo repository.ClueRepository, customerID string) *Service {
	return &Service{
		custRepo:  miningCust{c: &model.Customer{ID: customerID, UnifiedID: "one:" + customerID, Name: "张三"}},
		clueRepo:  clueRepo,
		lastJudge: make(map[string]time.Time),
	}
}

// miningHub account 的形状就是 process() 里那个 platform:senderID。
func miningHub(account string) *model.MessageHub {
	platform, senderID, _ := strings.Cut(account, ":")
	return &model.MessageHub{
		Direction: "inbound", Platform: platform, SenderID: senderID, AccountID: account,
		SenderName: "张三", Content: "想买一套", ConversationID: "conv-" + account, MsgType: "text",
	}
}

func miningJudgement(score int, confidence float64) *LeadJudgement {
	return &LeadJudgement{IsLead: true, IntentScore: score, Confidence: confidence, Summary: "有明确采购意向"}
}

func opportunityCount(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var n int64
	if err := db.Raw(`SELECT COUNT(*) FROM opportunities`).Scan(&n).Error; err != nil {
		t.Fatalf("数商机失败: %v", err)
	}
	return n
}

func opportunityOfClue(t *testing.T, db *gorm.DB, clueID string) []model.Opportunity {
	t.Helper()
	var rows []model.Opportunity
	if err := db.Where("clue_id = ?", clueID).Find(&rows).Error; err != nil {
		t.Fatalf("查商机失败: %v", err)
	}
	return rows
}

func mustClueByAccount(t *testing.T, db *gorm.DB, account string) model.Clue {
	t.Helper()
	var clue model.Clue
	if err := db.Where("account = ?", account).First(&clue).Error; err != nil {
		t.Fatalf("读线索 %s 失败: %v", account, err)
	}
	return clue
}

// TestMiningConvertsNewlyCreatedClue 新建那一支：幂等键必须是刚生成出来的 clue.ID。
func TestMiningConvertsNewlyCreatedClue(t *testing.T) {
	svc, _, clueRepo, database := newConvertServiceWithDB(t, []string{"alpha"})
	SetGlobalOpportunityConverter(svc)
	t.Cleanup(func() { SetGlobalOpportunityConverter(nil) })

	s := newMiningService(clueRepo, "cust-new")
	s.persistLead(context.Background(), &model.LeadMiningConfig{Enabled: true},
		miningHub("whatsapp:u100"), "whatsapp:u100", miningJudgement(88, 0.91))

	clue := mustClueByAccount(t, database, "whatsapp:u100")
	rows := opportunityOfClue(t, database, clue.ID)
	if len(rows) != 1 {
		t.Fatalf("线索 %s 对应 %d 行商机，期望 1 行", clue.ID, len(rows))
	}
	if rows[0].OwnerUserID != "alpha" {
		t.Errorf("归属 %q，期望自动分到 alpha", rows[0].OwnerUserID)
	}
	if rows[0].OneID != "one:cust-new" {
		t.Errorf("商机没带上客户统一身份：%q", rows[0].OneID)
	}
}

// TestMiningConvertsExistingClue 更新那一支：这条最容易漏 —— 它在函数中间 return。
func TestMiningConvertsExistingClue(t *testing.T) {
	svc, _, clueRepo, database := newConvertServiceWithDB(t, []string{"beta"})
	SetGlobalOpportunityConverter(svc)
	t.Cleanup(func() { SetGlobalOpportunityConverter(nil) })

	ctx := context.Background()
	old := &model.Clue{Type: ClueTypeLeadMining, Account: "whatsapp:u200", Name: "老线索", IntentScore: 10}
	if err := clueRepo.Create(ctx, old); err != nil {
		t.Fatalf("造老线索失败: %v", err)
	}

	s := newMiningService(clueRepo, "cust-old")
	s.persistLead(ctx, &model.LeadMiningConfig{Enabled: true},
		miningHub("whatsapp:u200"), "whatsapp:u200", miningJudgement(90, 0.95))

	rows := opportunityOfClue(t, database, old.ID)
	if len(rows) != 1 {
		t.Fatalf("已有线索那一支没转换（%d 行商机）：更新路径在函数中间 return，漏接调用点就藏在里面", len(rows))
	}
	if rows[0].OwnerUserID != "beta" {
		t.Errorf("归属 %q，期望 beta", rows[0].OwnerUserID)
	}
	if n := opportunityCount(t, database); n != 1 {
		t.Errorf("全站商机 %d 行，期望 1 行：多出来的行会把漏斗第一格吹胀", n)
	}
}

// TestMiningRepeatedLeadConvertsOnce 同一账号连投两次 ⇒ 仍然只有一行商机。
// 幂等键是线索号而不是消息号，只有两支都接上调用点时这条才成立。
func TestMiningRepeatedLeadConvertsOnce(t *testing.T) {
	svc, _, clueRepo, database := newConvertServiceWithDB(t, []string{"alpha"})
	SetGlobalOpportunityConverter(svc)
	t.Cleanup(func() { SetGlobalOpportunityConverter(nil) })

	ctx := context.Background()
	s := newMiningService(clueRepo, "cust-repeat")
	cfg := &model.LeadMiningConfig{Enabled: true}
	for i := 0; i < 2; i++ {
		s.persistLead(ctx, cfg, miningHub("whatsapp:u300"), "whatsapp:u300", miningJudgement(80, 0.9))
	}
	if n := opportunityCount(t, database); n != 1 {
		t.Errorf("两次挖掘产出 %d 行商机，期望 1 行", n)
	}
}

// TestMiningUnaffectedWhenConverterAbsent 没装配商机竖 ⇒ 挖掘照旧写线索、不 panic。
// "非侵入异步"是本文件头那条既有契约：商机竖是**加**在挖掘之后的，它不在场时
// 不能把原来那条路径一起带走。
func TestMiningUnaffectedWhenConverterAbsent(t *testing.T) {
	_, _, clueRepo, database := newConvertServiceWithDB(t, []string{"alpha"})
	SetGlobalOpportunityConverter(nil)

	s := newMiningService(clueRepo, "cust-absent")
	s.persistLead(context.Background(), &model.LeadMiningConfig{Enabled: true},
		miningHub("whatsapp:u400"), "whatsapp:u400", miningJudgement(88, 0.91))

	mustClueByAccount(t, database, "whatsapp:u400")
}

// TestMiningSkipsConvertWhenClueWriteFails 线索没落库 ⇒ 不建商机。
// 反过来的话 opportunities.clue_id 会指向一个不存在的行，而本表刻意不建外键
// （T-P4-01 的判据），那条引用永远查不回来，幂等键也就失去意义。
func TestMiningSkipsConvertWhenClueWriteFails(t *testing.T) {
	svc, _, clueRepo, database := newConvertServiceWithDB(t, []string{"alpha"})
	SetGlobalOpportunityConverter(svc)
	t.Cleanup(func() { SetGlobalOpportunityConverter(nil) })

	s := newMiningService(miningClueCreateFails{clueRepo}, "cust-fail")
	s.persistLead(context.Background(), &model.LeadMiningConfig{Enabled: true},
		miningHub("whatsapp:u500"), "whatsapp:u500", miningJudgement(88, 0.91))

	if n := opportunityCount(t, database); n != 0 {
		t.Errorf("线索写入失败却建了 %d 行商机：那行的 clue_id 指向一个不存在的线索", n)
	}
}

// TestMiningDoesNotNormalizeOutOfRangeConfidence 模型给出 0–1 之外的把握 ⇒ 不建商机，
// 也**不**就地除以 100。静默归一会把"提示词把量程写错了"这件事伪装成一批看起来合规的商机。
// 本条断的是接缝这一侧：它不吞掉、不造假；越界判据本身在 opportunity_convert_test.go。
func TestMiningDoesNotNormalizeOutOfRangeConfidence(t *testing.T) {
	svc, _, clueRepo, database := newConvertServiceWithDB(t, []string{"alpha"})
	SetGlobalOpportunityConverter(svc)
	t.Cleanup(func() { SetGlobalOpportunityConverter(nil) })

	s := newMiningService(clueRepo, "cust-range")
	s.persistLead(context.Background(), &model.LeadMiningConfig{Enabled: true},
		miningHub("whatsapp:u600"), "whatsapp:u600", miningJudgement(88, 88))

	if n := opportunityCount(t, database); n != 0 {
		t.Errorf("越界 confidence 建出了 %d 行商机：接缝或转换层做了静默归一", n)
	}
	// 线索本身必须还在：判定侧的错不该把挖掘的既成事实一起回滚掉（两条独立写路径）。
	mustClueByAccount(t, database, "whatsapp:u600")
}
