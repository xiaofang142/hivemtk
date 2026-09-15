// seed_messages.go 模块 H：统一消息种子数据
//
// 覆盖表：
// platform_accounts (8) 平台账号配置，覆盖 douyin/kuaishou/xiaohongshu/xianyu/wecom/web 等平台
// unified_messages (30) 统一消息，覆盖各平台 + 各状态 + 各消息类型
// unified_replies (15) 统一回复，覆盖 rule/rag/llm/human 四种回复路径
// message_hub (40) 消息中台聚合消息，覆盖 inbound/outbound 双向
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

type messagesSeeder struct{}

func (s *messagesSeeder) Name() string { return "messages" }
func (s *messagesSeeder) Description() string {
	return "平台账号(8)+统一消息(30)+统一回复(15)+消息中台(40)"
}

func (s *messagesSeeder) Clean(database *gorm.DB) error {
	if _, err := cleanByCondition(database, &model.MessageHub{}, "msg_id LIKE ?", "seed-hub-%"); err != nil {
		return fmt.Errorf("清空 message_hub 失败: %w", err)
	}
	if _, err := cleanByCondition(database, &model.UnifiedReply{}, "reply_id LIKE ?", "seed-reply-%"); err != nil {
		return fmt.Errorf("清空 unified_replies 失败: %w", err)
	}
	if _, err := cleanByCondition(database, &model.UnifiedMessage{}, "message_id LIKE ?", "seed-msg-%"); err != nil {
		return fmt.Errorf("清空 unified_messages 失败: %w", err)
	}
	if _, err := cleanByCondition(database, &model.PlatformAccount{}, "account_name LIKE ?", "%"+seedTag+"%"); err != nil {
		return fmt.Errorf("清空 platform_accounts 失败: %w", err)
	}
	return nil
}

func (s *messagesSeeder) Seed(database *gorm.DB, ctx *SeedContext) error {
	accounts := s.buildPlatformAccounts()
	if err := batchInsert(database, accounts, 50); err != nil {
		return fmt.Errorf("写入 platform_accounts 失败: %w", err)
	}
	for _, a := range accounts {
		ctx.PlatformAccountIDs = append(ctx.PlatformAccountIDs, a.ID)
	}

	messages := s.buildUnifiedMessages(accounts, ctx)
	if err := batchInsert(database, messages, 50); err != nil {
		return fmt.Errorf("写入 unified_messages 失败: %w", err)
	}
	for _, m := range messages {
		ctx.UnifiedMessageIDs = append(ctx.UnifiedMessageIDs, uint64(m.ID))
	}

	replies := s.buildUnifiedReplies(messages)
	if err := batchInsert(database, replies, 50); err != nil {
		return fmt.Errorf("写入 unified_replies 失败: %w", err)
	}

	hubMessages := s.buildMessageHub(accounts, ctx)
	if err := batchInsert(database, hubMessages, 50); err != nil {
		return fmt.Errorf("写入 message_hub 失败: %w", err)
	}

	log.Printf("  ✓ 已写入 平台账号%d+统一消息%d+统一回复%d+消息中台%d",
		len(accounts), len(messages), len(replies), len(hubMessages))
	return nil
}


func (s *messagesSeeder) buildPlatformAccounts() []model.PlatformAccount {
	specs := []struct {
		Platform    model.Platform
		AccountID   string
		AccountName string
		Status      int
	}{
		{model.PlatformDouyin, "seed-douyin-acc-01", "抖音演示账号·美妆旗舰店 " + seedTag, 1},
		{model.PlatformKuaishou, "seed-kuaishou-acc-01", "快手演示账号·3C官方店 " + seedTag, 1},
		{model.PlatformXiaohongshu, "seed-xhs-acc-01", "小红书演示账号·穿搭达人 " + seedTag, 1},
		{model.PlatformXianyu, "seed-xianyu-acc-01", "闲鱼演示账号·二手数码 " + seedTag, 1},
		{model.PlatformTiktok, "seed-tiktok-acc-01", "TikTok 演示账号·出海电商 " + seedTag, 1},
		{model.PlatformWeChat, "seed-wechat-acc-01", "微信演示公众号·品牌官方 " + seedTag, 1},
		{model.PlatformWeb, "seed-web-acc-01", "官网在线客服 " + seedTag, 1},
		{model.PlatformWebEmbed, "seed-embed-acc-01", "第三方网站 Widget " + seedTag, 0},
	}
	accounts := make([]model.PlatformAccount, 0, len(specs))
	for i, sp := range specs {
		var lastSync, expires *time.Time
		if sp.Status == 1 {
			t := hoursAgo(randInt(1, 48))
			lastSync = &t
			exp := daysAgo(-30) 
			expires = &exp
		} else {
			exp := daysAgo(3) 
			expires = &exp
		}
		acc := model.PlatformAccount{
			Platform:      sp.Platform,
			AccountID:     sp.AccountID,
			AccountName:   sp.AccountName,
			AccountAvatar: fmt.Sprintf("https://example.com/seed/avatar-%d.png", i+1),
			Config:        fmt.Sprintf(`{"app_id":"seed-app-%d","scope":["messages","comments"]}`, i+1),
			Cookie:        fmt.Sprintf("enc-cookie-seed-%d", i+1),
			Token:         fmt.Sprintf("enc-token-seed-%d", i+1),
			Status:        sp.Status,
			LastSyncAt:    lastSync,
			ExpiresAt:     expires,
		}
		accounts = append(accounts, acc)
	}
	return accounts
}

func (s *messagesSeeder) buildUnifiedMessages(accounts []model.PlatformAccount, ctx *SeedContext) []model.UnifiedMessage {
	if len(accounts) == 0 {
		return nil
	}
	platforms := []model.Platform{
		model.PlatformDouyin, model.PlatformKuaishou, model.PlatformXiaohongshu,
		model.PlatformXianyu, model.PlatformTiktok, model.PlatformWeChat,
	}
	contentTypes := []model.MessageType{
		model.MessageTypeText, model.MessageTypeText, model.MessageTypeText,
		model.MessageTypeImage, model.MessageTypeVideo, model.MessageTypeCard,
	}
	statuses := []model.MessageStatus{
		model.MessageStatusReplied, model.MessageStatusReplied, model.MessageStatusReplied,
		model.MessageStatusPending, model.MessageStatusProcessing, model.MessageStatusFailed, model.MessageStatusIgnored,
	}
	texts := []string{
		"您好，想咨询一下这款产品的价格",
		"这个颜色有现货吗？",
		"可以发一下产品详情吗？",
		"我已经下单了，什么时候发货？",
		"质量怎么样？有售后保障吗？",
		"看到你们在直播，想了解一下",
		"这个型号有什么区别？",
		"有没有更优惠的活动？",
		"我朋友推荐的，想咨询一下",
	}
	senders := []struct {
		ID, Name, Avatar string
	}{
		{"seed-user-01", "张小姐", "https://example.com/seed/u1.png"},
		{"seed-user-02", "李先生", "https://example.com/seed/u2.png"},
		{"seed-user-03", "王女士", "https://example.com/seed/u3.png"},
		{"seed-user-04", "刘总", "https://example.com/seed/u4.png"},
		{"seed-user-05", "陈同学", "https://example.com/seed/u5.png"},
	}
	messages := make([]model.UnifiedMessage, 0, 30)
	for i := 0; i < 30; i++ {
		acc := accounts[i%len(accounts)]
		platform := platforms[i%len(platforms)]
		contentType := contentTypes[i%len(contentTypes)]
		status := statuses[i%len(statuses)]
		sender := senders[i%len(senders)]
		text := texts[i%len(texts)]
		chatType := model.ChatTypePrivate
		if i%7 == 0 {
			chatType = model.ChatTypeGroup
		}
		var mediaURL string
		if contentType != model.MessageTypeText {
			mediaURL = fmt.Sprintf("https://example.com/seed/media-%d.mp4", i+1)
		}
		content := text
		if contentType == model.MessageTypeCard {
			content = fmt.Sprintf(`{"title":"智能卡片 #%d","price":%d}`, i+1, (i+1)*100)
		}
		msg := model.UnifiedMessage{
			MessageID:    fmt.Sprintf("seed-msg-%d", i+1),
			Platform:     platform,
			AccountID:    acc.AccountID,
			AccountName:  acc.AccountName,
			ChatID:       fmt.Sprintf("seed-chat-%d", (i%8)+1),
			ChatType:     chatType,
			SenderID:     sender.ID,
			SenderName:   sender.Name,
			SenderAvatar: sender.Avatar,
			Content:      content,
			ContentType:  contentType,
			MediaURL:     mediaURL,
			Status:       status,
			RawData:      fmt.Sprintf(`{"seed":true,"idx":%d}`, i+1),
		}
		if len(ctx.CustomerIDs) > 0 {
			msg.SenderID = ctx.CustomerIDs[i%len(ctx.CustomerIDs)]
		}
		messages = append(messages, msg)
	}
	return messages
}

func (s *messagesSeeder) buildUnifiedReplies(messages []model.UnifiedMessage) []model.UnifiedReply {
	if len(messages) == 0 {
		return nil
	}
	replyTypes := []string{"rule", "rag", "llm", "human"}
	templates := []string{
		"HiveMTK 开源版 AGPL-3.0 免费自部署；企业级集成与定制联系商务邮箱 jideilvluoqun@gmail.com",
		"根据您的需求，推荐了解 HiveMTK 七端接入与 ReAct 智能体：%s",
		"数据 100% 私域零出域，对话与向量存于您内网，Embedding/Rerank 强制本地",
		"已为您查询推理栈状态，Embedding 端点 8208 连通，RAG 可正常召回",
		"我们的支持工程师会尽快为您处理，请稍候",
	}
	replies := make([]model.UnifiedReply, 0, 15)
	for i := 0; i < 15; i++ {
		msg := messages[i%len(messages)]
		replyType := replyTypes[i%len(replyTypes)]
		status := model.ReplyStatusSent
		if replyType == "human" && i%3 == 0 {
			status = model.ReplyStatusPending
		}
		var sentAt *time.Time
		if status == model.ReplyStatusSent {
			t := hoursAgo(randInt(1, 24))
			sentAt = &t
		}
		var errMsg string
		if status == model.ReplyStatusFailed {
			errMsg = "演示：平台风控拒绝"
		}
		r := model.UnifiedReply{
			ReplyID:       fmt.Sprintf("seed-reply-%d", i+1),
			MessageID:     msg.MessageID,
			Platform:      msg.Platform,
			AccountID:     msg.AccountID,
			ChatID:        msg.ChatID,
			Content:       fmt.Sprintf(templates[i%len(templates)], (i+1)*100) + " " + seedTag,
			ContentType:   model.MessageTypeText,
			ReplyType:     replyType,
			Confidence:    randFloat(0.55, 0.98),
			AgentID:       uint(i%3) + 1,
			Status:        status,
			ErrorMessage:  errMsg,
			PlatformMsgID: fmt.Sprintf("platform-msg-%d", i+1),
			SentAt:        sentAt,
		}
		replies = append(replies, r)
	}
	return replies
}

func (s *messagesSeeder) buildMessageHub(accounts []model.PlatformAccount, ctx *SeedContext) []model.MessageHub {
	if len(accounts) == 0 {
		return nil
	}
	platforms := []string{"douyin", "kuaishou", "xiaohongshu", "xianyu", "tiktok", "wechat", "wecom", "feishu"}
	msgTypes := []string{"text", "text", "text", "image", "file", "link", "card"}
	directions := []string{"inbound", "inbound", "outbound"}
	extras := []string{
		"咨询产品规格",
		"询问发货进度",
		"售后问题反馈",
		"价格优惠咨询",
		"活动信息查询",
		"复购意向表达",
		"投诉处理",
		"邀约到店",
	}
	hub := make([]model.MessageHub, 0, 40)
	for i := 0; i < 40; i++ {
		platform := platforms[i%len(platforms)]
		direction := directions[i%len(directions)]
		msgType := msgTypes[i%len(msgTypes)]
		acc := accounts[i%len(accounts)]
		sentAt := hoursAgo(randInt(1, 96))
		isAI := i%4 == 0
		var aiAgent string
		if isAI {
			aiAgent = fmt.Sprintf("seed-ai-agent-%d", (i%3)+1)
		}
		var content string
		if msgType == "text" {
			content = fmt.Sprintf("%s %s #%d", extras[i%len(extras)], seedTag, i+1)
		} else {
			content = fmt.Sprintf(`{"type":"%s","url":"https://example.com/seed/hub-%d","desc":"%s %s"}`, msgType, i+1, extras[i%len(extras)], seedTag)
		}
		customerID := fmt.Sprintf("seed-hub-customer-%d", (i%5)+1)
		if len(ctx.CustomerIDs) > 0 {
			customerID = ctx.CustomerIDs[i%len(ctx.CustomerIDs)]
		}
		isGroup := i%6 == 0
		h := model.MessageHub{
			MsgID:          fmt.Sprintf("seed-hub-%d", i+1),
			Platform:       platform,
			AccountID:      acc.AccountID,
			Direction:      direction,
			MsgType:        msgType,
			SenderID:       customerID,
			SenderName:     fmt.Sprintf("客户%d号", (i%5)+1),
			ReceiverID:     fmt.Sprintf("seed-recv-%d", (i%3)+1),
			ReceiverName:   fmt.Sprintf("客服%d号", (i%3)+1),
			Content:        content,
			MediaURL:       fmt.Sprintf("https://example.com/seed/hub-media-%d", i+1),
			ConversationID: fmt.Sprintf("seed-conv-%d", (i%8)+1),
			IsGroup:        isGroup,
			GroupID: func() string {
				if isGroup {
					return fmt.Sprintf("seed-group-%d", (i%4)+1)
				}
				return ""
			}(),
			IsAIReply: isAI,
			AIAgent:   aiAgent,
			IsRead:    direction == "outbound" || i%3 == 0,
			ReadAt: func() *time.Time {
				if direction == "outbound" || i%3 == 0 {
					t := hoursAgo(randInt(0, 24))
					return &t
				}
				return nil
			}(),
			SentAt: sentAt,
			Extra: model.JSONMap{
				"seed":     seedTag,
				"scenario": extras[i%len(extras)],
			},
		}
		hub = append(hub, h)
	}
	return hub
}

// 防止未使用 import 警告（json 在某些扩展场景使用）
var _ = json.Marshal


