// seed_clues.go 模块 F：线索营销种子数据
//
// 覆盖表：
// clues (30) 覆盖6种来源 + 5种城市分布
// clue_scores (30) 每条线索对应评分，覆盖S/A/B/C/D 5个等级
// clue_engagement_events (60) 互动事件（每线索2-3条）
package main

import (
	"fmt"
	"log"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

type cluesSeeder struct{}

func (s *cluesSeeder) Name() string        { return "clues" }
func (s *cluesSeeder) Description() string { return "线索(30)+评分(30)+互动事件(60)" }

func (s *cluesSeeder) Clean(database *gorm.DB) error {
	if _, err := cleanByCondition(database, &model.ClueEngagementEvent{}, "payload LIKE ?", "%"+seedTag+"%"); err != nil {
		return fmt.Errorf("清空 clue_engagement_events 失败: %w", err)
	}
	if _, err := cleanByCondition(database, &model.ClueScore{}, "account LIKE ?", "seed-%"); err != nil {
		return fmt.Errorf("清空 clue_scores 失败: %w", err)
	}
	if _, err := cleanByCondition(database, &model.Clue{}, "account LIKE ?", "seed-%"); err != nil {
		return fmt.Errorf("清空 clues 失败: %w", err)
	}
	return nil
}

func (s *cluesSeeder) Seed(database *gorm.DB, ctx *SeedContext) error {
	clues := s.buildClues()
	if err := batchInsert(database, clues, 50); err != nil {
		return fmt.Errorf("写入 clues 失败: %w", err)
	}
	for _, c := range clues {
		ctx.ClueIDs = append(ctx.ClueIDs, c.ID)
	}

	scores := s.buildScores(clues)
	if err := batchInsert(database, scores, 50); err != nil {
		return fmt.Errorf("写入 clue_scores 失败: %w", err)
	}

	events := s.buildEngagementEvents(clues)
	if err := batchInsert(database, events, 50); err != nil {
		return fmt.Errorf("写入 clue_engagement_events 失败: %w", err)
	}

	log.Printf("  ✓ 已写入 线索%d + 评分%d + 互动事件%d", len(clues), len(scores), len(events))
	return nil
}

func (s *cluesSeeder) buildClues() []model.Clue {
	sources := []struct {
		SourceID string
		Type     int64
		Account  string
	}{
		{"web_form", 1, "seed-form"},
		{"baidu_ad", 2, "seed-baidu"},
		{"douyin_ad", 2, "seed-dy-ad"},
		{"wechat_pub", 3, "seed-wx-pub"},
		{"xiaohongshu", 4, "seed-xhs"},
		{"referral", 5, "seed-referral"},
		{"live_event", 6, "seed-event"},
		{"webinar", 6, "seed-webinar"},
	}
	cities := []string{"北京", "上海", "广州", "深圳", "杭州", "成都", "武汉", "南京"}
	names := []string{
		"王先生", "李女士", "张总", "刘经理", "陈先生", "赵女士",
		"黄总", "周经理", "吴先生", "徐女士", "孙总", "马经理",
		"朱先生", "胡女士", "郭总", "何经理", "高先生", "林女士",
		"罗总", "梁经理",
	}
	descs := []string{
		"对我们的企业版感兴趣，希望了解报价 " + seedTag,
		"咨询产品功能对比，已使用竞品 " + seedTag,
		"想了解私有化部署方案 " + seedTag,
		"需要 50 人团队版本，预算 10 万 " + seedTag,
		"关注数据安全和合规 " + seedTag,
		"想体验产品功能，希望预约演示 " + seedTag,
	}
	clues := make([]model.Clue, 0, 30)
	for i := 0; i < 30; i++ {
		src := sources[i%len(sources)]
		city := cities[i%len(cities)]
		name := names[i%len(names)]
		desc := descs[i%len(descs)]
		isVerify := int64(0)
		if i%3 == 0 {
			isVerify = 1
		}
		intentScore := int64(randInt(20, 95))
		isOpp := int64(0)
		if intentScore >= 70 {
			isOpp = 1
		}
		clue := model.Clue{
			SourceID:      src.SourceID,
			Account:       fmt.Sprintf("%s-%d", src.Account, i+1),
			Type:          src.Type,
			IsVerify:      isVerify,
			Name:          fmt.Sprintf("%s（线索#%d）", name, i+1),
			City:          city,
			Address:       fmt.Sprintf("%s市XX区XX路%d号", city, i+1),
			Desc:          desc,
			IntentScore:   intentScore,
			IsOpportunity: isOpp,
			OneID:         fmt.Sprintf("seed-oneid-clue-%d", i+1),
		}
		clues = append(clues, clue)
	}
	return clues
}

func (s *cluesSeeder) buildScores(clues []model.Clue) []model.ClueScore {
	scores := make([]model.ClueScore, 0, len(clues))
	for i, c := range clues {
		// 5 个等级按位置分布：前 6 个 S，6-12 A，12-18 B，18-24 C，24-30 D
		var totalScore int
		var grade string
		switch {
		case i < 6:
			totalScore = randInt(90, 100)
			grade = model.ClueGradeS
		case i < 12:
			totalScore = randInt(75, 89)
			grade = model.ClueGradeA
		case i < 18:
			totalScore = randInt(60, 74)
			grade = model.ClueGradeB
		case i < 24:
			totalScore = randInt(40, 59)
			grade = model.ClueGradeC
		default:
			totalScore = randInt(10, 39)
			grade = model.ClueGradeD
		}
		channelScore := randInt(40, 95)
		verifyScore := int(c.IsVerify) * 100
		profileScore := randInt(50, 90)
		engagementScore := randInt(20, 90)
		recencyScore := randInt(50, 100)

		factors := fmt.Sprintf(`{"channel":"%s","verify":%d,"profile":"%s","engagement":%d,"recency":%d,"seed":"%s"}`,
			c.SourceID, c.IsVerify, c.City, engagementScore, recencyScore, seedTag)

		score := model.ClueScore{
			ClueID:          c.ID,
			Account:         c.Account,
			TotalScore:      totalScore,
			Grade:           grade,
			Confidence:      randInt(70, 99),
			ChannelScore:    channelScore,
			VerifyScore:     verifyScore,
			ProfileScore:    profileScore,
			EngagementScore: engagementScore,
			RecencyScore:    recencyScore,
			FactorsJSON:     factors,
			ModelVersion:    "h-score-1",
		}
		scores = append(scores, score)
	}
	return scores
}

func (s *cluesSeeder) buildEngagementEvents(clues []model.Clue) []model.ClueEngagementEvent {
	eventTypes := []struct {
		EventType string
		Channel   string
	}{
		{"reply", "wecom"},
		{"click", "web"},
		{"call", "phone"},
		{"visit", "web"},
	}
	events := make([]model.ClueEngagementEvent, 0, 60)
	for i, c := range clues {
		for j := 0; j < 2; j++ {
			et := eventTypes[(i*2+j)%len(eventTypes)]
			payload := fmt.Sprintf(`{"source":"%s","note":"seed-demo#%d","tag":"%s"}`, c.SourceID, i*2+j+1, seedTag)
			ev := model.ClueEngagementEvent{
				ClueID:    c.ID,
				EventType: et.EventType,
				Channel:   et.Channel,
				Payload:   payload,
			}
			events = append(events, ev)
		}
	}
	return events
}
