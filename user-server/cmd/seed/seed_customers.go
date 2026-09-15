// seed_customers.go 模块 B：客户中心 CDP 种子数据
//
// 覆盖表：
// customers (50) 覆盖5渠道来源 + 5个RFM分层
// customer_tags (12) 4分类各3个标签
// customer_rfm (50) 与客户一一对应，分布到5个分层
// customer_events (300+) 每客户 5-10 个行为事件
// customer_long_term_memory (15) 高价值客户偏好/习惯/事件
// recovery_queue (5) 流失客户挽回队列
package main

import (
	"encoding/json"
	"fmt"
	"log"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

type customersSeeder struct{}

func (s *customersSeeder) Name() string { return "customers" }
func (s *customersSeeder) Description() string {
	return "客户(50)+标签(12)+RFM(50)+事件(300)+记忆(15)+挽回(5)"
}

func (s *customersSeeder) Clean(database *gorm.DB) error {
	if _, err := cleanByCondition(database, &model.RecoveryQueue{}, "meta_json LIKE ?", "%seed-demo%"); err != nil {
		return fmt.Errorf("清空 recovery_queue 失败: %w", err)
	}
	if _, err := cleanByCondition(database, &model.CustomerLongTermMemory{}, "metadata::text LIKE ?", "%seed-demo%"); err != nil {
		return fmt.Errorf("清空 customer_long_term_memory 失败: %w", err)
	}
	if _, err := cleanByCondition(database, &model.CustomerEvent{}, "event_data LIKE ?", "%seed-demo%"); err != nil {
		return fmt.Errorf("清空 customer_events 失败: %w", err)
	}
	if _, err := cleanByCondition(database, &model.CustomerRFM{}, "unified_id LIKE ?", "seed-%"); err != nil {
		return fmt.Errorf("清空 customer_rfm 失败: %w", err)
	}
	if _, err := cleanByCondition(database, &model.CustomerTag{}, "name LIKE ?", "%[seed]%"); err != nil {
		return fmt.Errorf("清空 customer_tags 失败: %w", err)
	}
	if _, err := cleanByCondition(database, &model.Customer{}, "phone LIKE ? OR email LIKE ?", "1390000%", "seed-%"); err != nil {
		return fmt.Errorf("清空 customers 失败: %w", err)
	}
	return nil
}

func (s *customersSeeder) Seed(database *gorm.DB, ctx *SeedContext) error {
	tags := s.buildTags()
	if err := batchInsert(database, tags, 50); err != nil {
		return fmt.Errorf("写入 customer_tags 失败: %w", err)
	}
	for _, t := range tags {
		ctx.TagIDs = append(ctx.TagIDs, t.ID)
	}

	customers := s.buildCustomers()
	if err := batchInsert(database, customers, 50); err != nil {
		return fmt.Errorf("写入 customers 失败: %w", err)
	}
	for _, c := range customers {
		ctx.CustomerIDs = append(ctx.CustomerIDs, c.ID)
		if len(ctx.ChampionCustomerIDs) < 8 {
			ctx.ChampionCustomerIDs = append(ctx.ChampionCustomerIDs, c.ID)
		}
	}

	rfms := s.buildRFM(customers)
	if err := batchInsert(database, rfms, 50); err != nil {
		return fmt.Errorf("写入 customer_rfm 失败: %w", err)
	}

	events := s.buildEvents(customers)
	if err := batchInsert(database, events, 100); err != nil {
		return fmt.Errorf("写入 customer_events 失败: %w", err)
	}

	memories := s.buildLongTermMemories(customers[:15])
	if err := batchInsert(database, memories, 50); err != nil {
		return fmt.Errorf("写入 customer_long_term_memory 失败: %w", err)
	}

	recoveryQ := s.buildRecoveryQueue(customers[45:])
	if err := batchInsert(database, recoveryQ, 50); err != nil {
		return fmt.Errorf("写入 recovery_queue 失败: %w", err)
	}
	for _, c := range customers[45:] {
		ctx.ChurnCustomerIDs = append(ctx.ChurnCustomerIDs, c.ID)
	}

	log.Printf("  ✓ 已写入 %d 客户 + %d 标签 + %d RFM + %d 事件 + %d 记忆 + %d 挽回",
		len(customers), len(tags), len(rfms), len(events), len(memories), len(recoveryQ))
	return nil
}

// buildTags 构建 12 个客户标签
func (s *customersSeeder) buildTags() []model.CustomerTag {
	specs := []struct {
		Name     string
		Category model.TagCategory
	}{
		{"高净值客户 " + seedTag, model.TagCategoryDemographic},
		{"新用户 " + seedTag, model.TagCategoryDemographic},
		{"老用户 " + seedTag, model.TagCategoryDemographic},
		{"高频互动 " + seedTag, model.TagCategoryBehavioral},
		{"低频互动 " + seedTag, model.TagCategoryBehavioral},
		{"休眠用户 " + seedTag, model.TagCategoryBehavioral},
		{"已成交 " + seedTag, model.TagCategoryTransactional},
		{"复购客户 " + seedTag, model.TagCategoryTransactional},
		{"高客单价 " + seedTag, model.TagCategoryTransactional},
		{"价格敏感 " + seedTag, model.TagCategoryPsychographic},
		{"品质优先 " + seedTag, model.TagCategoryPsychographic},
		{"品牌忠诚 " + seedTag, model.TagCategoryPsychographic},
	}
	tags := make([]model.CustomerTag, 0, len(specs))
	for _, sp := range specs {
		tags = append(tags, model.CustomerTag{
			Name:     sp.Name,
			Category: sp.Category,
			Source:   model.TagSourceManual,
			Rule:     toJSONString(map[string]any{"source": "seed", "category": sp.Category}),
		})
	}
	return tags
}

// buildCustomers 构建 50 个客户，覆盖 5 渠道 + 5 RFM 分层
func (s *customersSeeder) buildCustomers() []model.Customer {
	// 渠道分布（共 50 个客户）
	// phone(15) + email(10) + wechat(12) + douyin(8) + xiaohongshu(5)

	industries := []string{"电商", "教育", "金融", "医疗", "本地生活"}
	cities := []string{"北京", "上海", "广州", "深圳", "杭州", "成都", "武汉", "南京", "苏州", "西安"}
	customers := make([]model.Customer, 0, 50)

	for i := 0; i < 50; i++ {
		idx := i + 1
		phone := fmt.Sprintf("1390000%04d", idx)
		email := fmt.Sprintf("seed-customer-%02d@hivemtk.demo", idx)
		wechat := fmt.Sprintf("seed_wx_%04d", idx)
		douyin := fmt.Sprintf("seed_dy_%04d", idx)
		xhs := fmt.Sprintf("seed_xhs_%04d", idx)

		c := model.Customer{
			Phone:     phone,
			Email:     email,
			Tags:      toJSONArrayString([]string{industries[i%5], cities[i%10]}),
			RFMScore:  randInt(1, 100),
			ChurnRisk: []string{"low", "medium", "high"}[i%3],
		}

		switch {
		case i < 15:
		case i < 25:
			c.Phone = ""
		case i < 37:
			c.Phone = ""
			c.WechatOpenID = wechat
		case i < 45:
			c.Phone = ""
			c.DouyinOpenID = douyin
		default:
			c.Phone = ""
			c.XiaohongshuID = xhs
		}
		customers = append(customers, c)
	}
	return customers
}

// buildRFM 为 50 客户生成 RFM 记录，分布到 5 个分层
func (s *customersSeeder) buildRFM(customers []model.Customer) []model.CustomerRFM {
	segmentSpecs := []struct {
		Count    int
		Segment  string
		RRange   [2]int
		FRange   [2]int
		MRange   [2]int64
		RScore   int
		FScore   int
		MScore   int
		ChurnLvl string
	}{
		{8, model.RFMSegmentChampion, [2]int{1, 7}, [2]int{10, 30}, [2]int64{5000000, 20000000}, 5, 5, 5, "low"},
		{12, model.RFMSegmentLoyal, [2]int{3, 14}, [2]int{5, 15}, [2]int64{1000000, 5000000}, 4, 4, 4, "low"},
		{15, model.RFMSegmentPotential, [2]int{7, 21}, [2]int{2, 8}, [2]int64{300000, 1500000}, 3, 3, 3, "medium"},
		{10, model.RFMSegmentAtRisk, [2]int{14, 45}, [2]int{1, 4}, [2]int64{100000, 500000}, 2, 2, 2, "high"},
		{5, model.RFMSegmentChurn, [2]int{60, 120}, [2]int{0, 2}, [2]int64{0, 100000}, 1, 1, 1, "high"},
	}

	rfms := make([]model.CustomerRFM, 0, len(customers))
	idx := 0
	for _, seg := range segmentSpecs {
		for i := 0; i < seg.Count && idx < len(customers); i++ {
			c := customers[idx]
			idx++
			recency := randInt(seg.RRange[0], seg.RRange[1])
			freq := randInt(seg.FRange[0], seg.FRange[1])
			monetary := randInt64(seg.MRange[0], seg.MRange[1])
			avgOrder := int64(0)
			if freq > 0 {
				avgOrder = monetary / int64(freq)
			}
			lastActive := daysAgo(recency)
			composite := (seg.RScore*30 + seg.FScore*30 + seg.MScore*40) / 5
			rfm := model.CustomerRFM{
				CustomerID:     c.ID,
				UnifiedID:      "seed-" + c.UnifiedID,
				RecencyDays:    recency,
				Frequency:      freq,
				MonetaryTotal:  monetary,
				AvgOrderValue:  avgOrder,
				RScore:         seg.RScore,
				FScore:         seg.FScore,
				MScore:         seg.MScore,
				CompositeScore: composite,
				Segment:        seg.Segment,
				ChurnRiskLevel: seg.ChurnLvl,
				ChurnScore:     100 - composite,
				LastActiveAt:   &lastActive,
			}
			rfms = append(rfms, rfm)
		}
	}
	return rfms
}

// buildEvents 为每个客户生成 5-10 个行为事件
func (s *customersSeeder) buildEvents(customers []model.Customer) []model.CustomerEvent {
	eventTypes := []model.EventType{
		model.EventTypePageView, model.EventTypeClick, model.EventTypePurchase,
		model.EventTypeAddToCart, model.EventTypeSignup, model.EventTypeLogin,
	}
	eventSources := []model.EventSource{
		model.EventSourceWechat, model.EventSourceDouyin, model.EventSourceXiaohongshu,
		model.EventSourceWebsite, model.EventSourceApp,
	}
	pages := []string{"/products", "/cart", "/checkout", "/profile", "/orders", "/coupons", "/support"}
	products := []string{"智能手表", "无线耳机", "美容仪", "健身器材", "教育课程", "理财产品", "母婴用品", "数码配件"}

	events := make([]model.CustomerEvent, 0, len(customers)*7)
	for _, c := range customers {
		n := randInt(5, 10)
		for i := 0; i < n; i++ {
			et := eventTypes[seededRand.Intn(len(eventTypes))]
			es := eventSources[seededRand.Intn(len(eventSources))]
			occurred := daysAgo(randInt(1, 30))

			var data map[string]any
			switch et {
			case model.EventTypePageView:
				data = map[string]any{"url": randPick(pages), "duration_sec": randInt(5, 300)}
			case model.EventTypeClick:
				data = map[string]any{"target": "button_buy_now", "page": randPick(pages)}
			case model.EventTypePurchase:
				data = map[string]any{"product": randPick(products), "amount_cents": randInt64(9900, 999900), "order_id": fmt.Sprintf("ORD-%d", randInt(10000, 99999))}
			case model.EventTypeAddToCart:
				data = map[string]any{"product": randPick(products), "quantity": randInt(1, 3)}
			case model.EventTypeSignup:
				data = map[string]any{"source": "qr_code", "campaign": "seed-demo"}
			case model.EventTypeLogin:
				data = map[string]any{"device": randPick([]string{"ios", "android", "web"}), "ip": "192.168.1.1"}
			}
			dataJSON, _ := json.Marshal(data)
			events = append(events, model.CustomerEvent{
				CustomerID:  c.ID,
				EventType:   et,
				EventSource: es,
				EventData:   string(dataJSON),
				OccurredAt:  occurred,
			})
		}
	}
	return events
}

// buildLongTermMemories 为前 15 个高价值客户生成 15 条长期记忆
func (s *customersSeeder) buildLongTermMemories(customers []model.Customer) []model.CustomerLongTermMemory {
	type memSpec struct {
		MType      model.LongTermMemoryType
		Content    string
		Importance int
	}
	specs := []memSpec{
		{model.LongTermMemoryPreference, "客户偏好高端品牌，预算 5000-10000 元，关注智能手表和数码产品", 9},
		{model.LongTermMemoryHabit, "习惯在晚上 20-22 点咨询，喜欢通过微信沟通", 7},
		{model.LongTermMemoryFeedback, "对客服响应速度满意，希望增加产品视频展示", 6},
		{model.LongTermMemoryEvent, "客户生日 3 月 15 日，去年购买过 3 次客单价 3000+", 8},
		{model.LongTermMemoryFact, "VIP 等级金卡，年消费超过 5 万元", 9},
		{model.LongTermMemoryPreference, "客户偏好教育类产品，预算 2000-5000 元，关注儿童英语和编程", 8},
		{model.LongTermMemoryHabit, "习惯在周末上午咨询，偏好电话沟通", 6},
		{model.LongTermMemoryFeedback, "对产品包装不满意，希望改进物流时效", 5},
		{model.LongTermMemoryEvent, "客户投诉过物流慢，已补偿优惠券解决", 7},
		{model.LongTermMemoryFact, "家庭客户，有 2 个孩子，月均消费 8000 元", 8},
		{model.LongTermMemoryPreference, "偏好金融理财类产品，关注收益稳定型", 8},
		{model.LongTermMemoryHabit, "工作日午休时间活跃，喜欢简洁回复", 5},
		{model.LongTermMemoryFeedback, "对 AI 客服推荐的话术很满意", 7},
		{model.LongTermMemoryEvent, "去年购买过美容仪，今年想复购新品", 8},
		{model.LongTermMemoryFact, "城市白领，年龄 28-35 岁，月收入 2-3 万", 6},
	}
	memories := make([]model.CustomerLongTermMemory, 0, len(specs))
	for i, sp := range specs {
		if i >= len(customers) {
			break
		}
		c := customers[i]
		meta, _ := json.Marshal(map[string]any{"source": "seed", "extracted_at": daysAgo(randInt(1, 14))})
		memories = append(memories, model.CustomerLongTermMemory{
			CustomerID: c.ID,
			MemoryType: sp.MType,
			Content:    sp.Content + " " + seedTag,
			Importance: sp.Importance,
			Source:     model.LongTermMemorySourceConversation,
			Metadata:   model.JSONMap{},
			ExpiresAt:  nil,
		})
		memories[i].Metadata = model.JSONMap{}
		_ = meta
	}
	return memories
}

// buildRecoveryQueue 为 5 个流失客户创建挽回队列
func (s *customersSeeder) buildRecoveryQueue(customers []model.Customer) []model.RecoveryQueue {
	strategies := []string{"sms_coupon", "email_winback", "wecom_reach", "card_recomment", "ai_call"}
	reasons := []string{"churn", "downgrade", "complaint"}
	channels := []string{"sms", "email", "wecom", "card", "douyin"}
	results := []string{"no_response", "clicked_no_buy", "replied_negative", "unsubscribed", "bounce"}

	queue := make([]model.RecoveryQueue, 0, len(customers))
	for i, c := range customers {
		nextAttempt := daysAgo(-randInt(1, 7))
		lastAttempt := daysAgo(randInt(1, 7))
		queue = append(queue, model.RecoveryQueue{
			CustomerID:    c.ID,
			UnifiedID:     "seed-" + c.UnifiedID,
			Account:       c.Phone,
			Reason:        reasons[i%len(reasons)],
			Strategy:      strategies[i%len(strategies)],
			Priority:      randInt(1, 8),
			Stage:         randPick([]string{"queued", "running"}),
			Attempts:      randInt(0, 2),
			MaxAttempts:   3,
			LastAttemptAt: &lastAttempt,
			NextAttemptAt: &nextAttempt,
			LastChannel:   channels[i%len(channels)],
			LastResult:    results[i%len(results)],
			MetaJSON:      toJSONString(map[string]any{"source": "seed-demo", "campaign": "churn_winback_q3"}),
		})
	}
	return queue
}

// randInt64 生成 64 位随机整数
func randInt64(min, max int64) int64 {
	if max <= min {
		return min
	}
	return min + seededRand.Int63n(max-min+1)
}
