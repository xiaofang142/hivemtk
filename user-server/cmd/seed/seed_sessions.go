// seed_sessions.go 模块 C：客服会话种子数据
package main

import (
	"fmt"
	"log"
	"time"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

type sessionsSeeder struct{}

func (s *sessionsSeeder) Name() string { return "sessions" }
func (s *sessionsSeeder) Description() string {
	return "客服会话(30)+消息(400)+AI建议(20)+黑名单(3)+快捷回复(18)"
}

func (s *sessionsSeeder) Clean(database *gorm.DB) error {
	if _, err := cleanByCondition(database, &model.AISuggestion{}, "suggestion LIKE ?", "%"+seedTag+"%"); err != nil {
		return fmt.Errorf("清空 ai_suggestions 失败: %w", err)
	}
	if _, err := cleanByCondition(database, &model.UserBlacklist{}, "reason LIKE ?", "%"+seedTag+"%"); err != nil {
		return fmt.Errorf("清空 user_blacklist 失败: %w", err)
	}
	if _, err := cleanByCondition(database, &model.SessionMessage{}, "sender_id LIKE ?", "seed_%"); err != nil {
		return fmt.Errorf("清空 session_messages 失败: %w", err)
	}
	if _, err := cleanByCondition(database, &model.CustomerSession{}, "session_id LIKE ?", "seed-sess-%"); err != nil {
		return fmt.Errorf("清空 customer_sessions 失败: %w", err)
	}
	if _, err := cleanByCondition(database, &model.QuickReply{}, "title LIKE ?", "%"+seedTag+"%"); err != nil {
		return fmt.Errorf("清空 quick_replies 失败: %w", err)
	}
	return nil
}

func (s *sessionsSeeder) Seed(database *gorm.DB, ctx *SeedContext) error {
	sessions := s.buildSessions(ctx)
	if err := batchInsert(database, sessions, 50); err != nil {
		return fmt.Errorf("写入 customer_sessions 失败: %w", err)
	}
	for _, sess := range sessions {
		ctx.SessionIDs = append(ctx.SessionIDs, sess.SessionID)
		if sess.Status == model.SessionStatusAIHandling ||
			sess.Status == model.SessionStatusHumanHandling ||
			sess.Status == model.SessionStatusWaiting {
			ctx.ActiveSessionIDs = append(ctx.ActiveSessionIDs, sess.SessionID)
		} else {
			ctx.ResolvedSessionIDs = append(ctx.ResolvedSessionIDs, sess.SessionID)
		}
	}

	messages := s.buildMessages(sessions, ctx)
	if err := batchInsert(database, messages, 100); err != nil {
		return fmt.Errorf("写入 session_messages 失败: %w", err)
	}

	suggestions := s.buildAISuggestions(ctx)
	if err := batchInsert(database, suggestions, 50); err != nil {
		return fmt.Errorf("写入 ai_suggestions 失败: %w", err)
	}

	blacklist := s.buildBlacklist(ctx)
	if err := batchInsert(database, blacklist, 50); err != nil {
		return fmt.Errorf("写入 user_blacklist 失败: %w", err)
	}

	quickReplies := s.buildQuickReplies()
	if err := batchInsert(database, quickReplies, 50); err != nil {
		return fmt.Errorf("写入 quick_replies 失败: %w", err)
	}

	log.Printf("  ✓ 已写入 %d 会话 + %d 消息 + %d AI建议 + %d 黑名单 + %d 快捷回复",
		len(sessions), len(messages), len(suggestions), len(blacklist), len(quickReplies))
	return nil
}

// buildSessions 构建 30 个客服会话
func (s *sessionsSeeder) buildSessions(ctx *SeedContext) []model.CustomerSession {
	platforms := []model.Platform{
		model.PlatformDouyin, model.PlatformXiaohongshu, model.PlatformWeChat,
		model.PlatformXianyu, model.PlatformKuaishou, model.PlatformWeb,
		model.PlatformWebEmbed,
	}
	statuses := []model.SessionStatus{
		model.SessionStatusPending, model.SessionStatusPending, model.SessionStatusPending,
		model.SessionStatusPending, model.SessionStatusPending, model.SessionStatusPending,
		model.SessionStatusAIHandling, model.SessionStatusAIHandling, model.SessionStatusAIHandling,
		model.SessionStatusAIHandling, model.SessionStatusAIHandling,
		model.SessionStatusHumanHandling, model.SessionStatusHumanHandling,
		model.SessionStatusHumanHandling, model.SessionStatusHumanHandling,
		model.SessionStatusWaiting, model.SessionStatusWaiting, model.SessionStatusWaiting,
		model.SessionStatusWaiting, model.SessionStatusWaiting,
		model.SessionStatusResolved, model.SessionStatusResolved, model.SessionStatusResolved,
		model.SessionStatusResolved, model.SessionStatusResolved, model.SessionStatusResolved,
		model.SessionStatusClosed, model.SessionStatusClosed,
		model.SessionStatusClosed, model.SessionStatusClosed,
	}
	priorities := []int{0, 0, 1, 1, 2, 2}
	industries := []string{"电商", "教育", "金融", "医疗", "本地生活"}

	sessions := make([]model.CustomerSession, 0, 30)
	for i := 0; i < 30; i++ {
		custIdx := i % len(ctx.CustomerIDs)
		customerID := ctx.CustomerIDs[custIdx]
		plat := platforms[i%len(platforms)]
		status := statuses[i]
		handlerType := model.HandlerTypeAI
		if status == model.SessionStatusHumanHandling {
			handlerType = model.HandlerTypeHuman
		}
		var agentID uint
		var agentName string
		if i < len(ctx.CSUserIDs) {
			agentID = ctx.CSUserIDs[i]
			agentName = fmt.Sprintf("seed_cs_%02d", i+1)
		}
		lastMsgAt := hoursAgo(randInt(0, 48))
		var resolvedAt, closedAt *time.Time
		if status == model.SessionStatusResolved {
			t := hoursAgo(randInt(1, 24))
			resolvedAt = &t
		}
		if status == model.SessionStatusClosed {
			t := hoursAgo(randInt(2, 72))
			closedAt = &t
		}
		tags := []string{industries[i%5], "seed-demo"}
		sessions = append(sessions, model.CustomerSession{
			SessionID:       fmt.Sprintf("seed-sess-%04d", i+1),
			Platform:        plat,
			AccountID:       fmt.Sprintf("seed-acct-%02d", i%5+1),
			UserID:          customerID,
			UserName:        fmt.Sprintf("客户%d号", i+1),
			UserAvatar:      "https://cdn.hivemtk.demo/avatar.png",
			UserPhone:       fmt.Sprintf("1390000%04d", i+1),
			Status:          status,
			HandlerType:     handlerType,
			AgentID:         agentID,
			AgentName:       agentName,
			Priority:        priorities[i%len(priorities)],
			LastMessage:     randPick([]string{"怎么私有化部署？", "RAG 召回为空", "模型怎么换？", "支持抖音接入吗？", "已收到，谢谢"}),
			LastMessageAt:   &lastMsgAt,
			LastMessageBy:   randPick([]string{"user", "ai", "agent"}),
			MessageCount:    randInt(5, 30),
			AIReplyCount:    randInt(0, 15),
			HumanReplyCount: randInt(0, 10),
			AvgResponseTime: randInt(5, 120),
			Rating:          randInt(0, 5),
			RatingComment:   randPick([]string{"", "", "服务很好", "响应很快", "问题已解决"}),
			Tags:            toJSONString(tags),
			ResolvedAt:      resolvedAt,
			ClosedAt:        closedAt,
		})
	}
	return sessions
}

// buildMessages 为每个会话生成 10-30 条消息
func (s *sessionsSeeder) buildMessages(sessions []model.CustomerSession, ctx *SeedContext) []model.SessionMessage {
	userMsgs := []string{
		"你好，HiveMTK 怎么私有化部署？",
		"我想了解一下 ReAct 智能体是怎么工作的",
		"本地推理栈对机器配置有什么要求？",
		"支持接入抖音和小红书吗？",
		"怎么把客服挂到自己官网？",
		"数据会上传到云端吗？",
		"我已经部署好了，怎么访问控制台？",
		"帮我查一下 RAG 知识库有没有生效",
		"客服在吗？有部署问题想咨询",
		"HiveMTK 和 Dify 比有什么不一样？",
		"企业定制怎么收费？",
		"我想参与代码贡献",
	}
	aiMsgs := []string{
		"您好，欢迎使用 HiveMTK 智能客服。可以问我项目功能、技术架构、部署方式、资产市场等问题。",
		"我帮您查到：ReAct 智能体是感知→规划→调工具→反思的循环，内置 41 个业务工具，最多 5 轮自主决策。",
		"部署硬件：dev 档约 8GB 内存（Qwen2.5-1.5B），prod 档 16GB+（Qwen2.5-14B），可选 GPU 加速。",
		"是的，七端接入：抖音/快手/小红书/闲鱼/TikTok/微信/短信邮件，统一消息中心与 CDP 客户视图。",
		"用 embed-sdk 嵌入即可：引入原生 JS，访客自动调用 /api/chat/public/* 完成双向会话。",
		"不会。100% 私域零出域，对话与向量存于你内网，Embedding/Rerank 强制本地，云端不落向量。",
		"访问 http://<ip>:8204 控制台，默认账号 admin + 你设置的密码。",
		"请确认本地推理栈已起：curl http://localhost:8208/health 应返回 200，否则 RAG 召回为空。",
		"已为您转接人工支持，请稍候。",
		"建议参考文档对比章节，或直接转人工咨询。",
		"开源版 AGPL-3.0 免费；企业级集成与定制联系运营商务邮箱 jideilvluoqun@gmail.com。",
		"欢迎！Fork 仓库后提 PR，先读 CONTRIBUTING.md 与分层架构规范。",
	}
	agentMsgs := []string{
		"您好，我是 HiveMTK 支持工程师，很高兴为您服务。",
		"部署建议走三步：git clone → make install → make up，.env 密钥用 openssl rand -hex 24 生成。",
		"已为您查询：推理栈状态正常，Embedding 端点 8208 连通，RAG 可正常召回。",
		"可以的呢，请提供报错日志或 Gitee Issue 编号，我帮您排查。",
		"已收到您的反馈，我们会在 12 小时内回复（Gitee Issues 首响承诺）。",
		"感谢您的咨询，如需实时讨论可加微信交流群（wxid: xiao142000）。",
	}

	messages := make([]model.SessionMessage, 0, len(sessions)*15)
	for _, sess := range sessions {
		n := randInt(10, 30)
		startTime := hoursAgo(randInt(1, 48))
		for i := 0; i < n; i++ {
			var senderType, senderName, content string
			var aiConfidence float64
			var aiSource string
			switch i % 3 {
			case 0: 
				senderType = "user"
				senderName = sess.UserName
				content = randPick(userMsgs)
			case 1: 
				senderType = "ai"
				senderName = "AI助手"
				content = randPick(aiMsgs)
				aiConfidence = randFloat(0.6, 0.95)
				aiSource = randPick([]string{"rule", "rag", "llm"})
			case 2: 
				senderType = "agent"
				senderName = sess.AgentName
				content = randPick(agentMsgs)
			}
			msgTime := startTime.Add(time.Duration(i*30) * time.Minute)
			if msgTime.After(time.Now()) {
				msgTime = time.Now().Add(-time.Duration(n-i) * time.Minute)
			}
			messages = append(messages, model.SessionMessage{
				SessionID:    sess.SessionID,
				Content:      content,
				ContentType:  model.MessageTypeText,
				SenderType:   senderType,
				SenderID:     senderType + "_" + sess.SessionID,
				SenderName:   senderName,
				SenderAvatar: "https://cdn.hivemtk.demo/avatar.png",
				AIConfidence: aiConfidence,
				AISource:     aiSource,
				IsRead:       senderType != "user", 
				ReadAt:       nil,
				CreatedAt:    msgTime,
			})
		}
	}
	return messages
}

// buildAISuggestions 为进行中的会话生成 AI 建议
func (s *sessionsSeeder) buildAISuggestions(ctx *SeedContext) []model.AISuggestion {
	if len(ctx.ActiveSessionIDs) == 0 {
		return nil
	}
	suggestions := make([]model.AISuggestion, 0, 20)
	suggestionTexts := []string{
		"建议回复：HiveMTK 私有化部署三步即可：git clone → make install → make up。",
		"建议回复：数据 100% 私域零出域，Embedding/Rerank 强制本地，云端不落向量。",
		"建议回复：已查配置，dev 档模型本地就绪，prod 档需 16GB+ 内存或 GPU。",
		"建议回复：推理栈状态正常，Embedding 端点 8208 连通，RAG 可正常召回。",
		"建议转人工：客户问企业定制报价，需商务介入。",
		"建议回复：开源版 AGPL-3.0 免费；企业集成与定制联系商务邮箱 jideilvluoqun@gmail.com。",
		"建议回复：资产市场的付费资产由 ISV 定价，售后走平台工单流程。",
		"建议回复：新版本已发布，更新日志见 Gitee Releases，建议尽早升级。",
	}
	for i := 0; i < 20 && i < len(ctx.ActiveSessionIDs); i++ {
		suggestions = append(suggestions, model.AISuggestion{
			SessionID:  ctx.ActiveSessionIDs[i],
			MessageID:  uint(i + 1),
			Suggestion: randPick(suggestionTexts) + " " + seedTag,
			Confidence: randFloat(0.65, 0.95),
			Source:     randPick([]string{"rule", "rag", "llm"}),
			IsUsed:     randPick([]bool{true, false, false}),
		})
	}
	return suggestions
}

// buildBlacklist 创建 3 条用户黑名单
func (s *sessionsSeeder) buildBlacklist(ctx *SeedContext) []model.UserBlacklist {
	blacklist := make([]model.UserBlacklist, 0, 3)
	reasons := []string{
		"恶意刷屏广告，多次警告无效 " + seedTag,
		"发送违规内容，已自动拉黑 " + seedTag,
		"频繁投诉无理要求，影响客服工作 " + seedTag,
	}
	for i := 0; i < 3 && i < len(ctx.CustomerIDs); i++ {
		operID := uint(1)
		if len(ctx.CSUserIDs) > 0 {
			operID = ctx.CSUserIDs[i%len(ctx.CSUserIDs)]
		}
		expires := daysAgo(-30) 
		blacklist = append(blacklist, model.UserBlacklist{
			UserID:       ctx.CustomerIDs[i*5+3], 
			Platform:     randPick([]model.Platform{model.PlatformDouyin, model.PlatformXiaohongshu, model.PlatformWeChat}),
			Reason:       reasons[i],
			Source:       randPick([]string{"manual", "auto", "risk"}),
			OperatorID:   operID,
			OperatorName: fmt.Sprintf("seed_cs_%02d", i+1),
			SessionID:    fmt.Sprintf("seed-sess-%04d", i+1),
			Active:       true,
			ExpiresAt:    &expires,
		})
	}
	return blacklist
}

// buildQuickReplies 创建 18 条快捷回复（6 通用 + 12 渠道专属）
func (s *sessionsSeeder) buildQuickReplies() []model.QuickReply {
	specs := []struct {
		Category string
		Title    string
		Content  string
		Channel  string
	}{
		{"通用", "欢迎语 " + seedTag, "您好，欢迎使用 HiveMTK 智能客服，请问有什么可以帮您？", ""},
		{"通用", "开源动态 " + seedTag, "HiveMTK 近期发布新版本，新增功能与部署指南已更新，欢迎到 Gitee 查看。", ""},
		{"通用", "功能推荐 " + seedTag, "为您推荐了解 HiveMTK 七端接入与 ReAct 智能体，详见文档与仓库。", ""},
		{"通用", "部署说明 " + seedTag, "私有化部署支持 Docker Compose：git clone → make install → make up 三步完成。", ""},
		{"通用", "感谢咨询 " + seedTag, "感谢您的咨询，如有其他问题随时联系，祝您使用愉快！", ""},
		{"通用", "转人工 " + seedTag, "好的，正在为您转接人工支持，请稍候。", ""},
		{"whatsapp", "WA欢迎语 " + seedTag, "Hello! Welcome to HiveMTK open-source community. How can I help you today?", "whatsapp"},
		{"whatsapp", "WA动态 " + seedTag, "New release is out — check the changelog on Gitee. Star us to follow updates.", "whatsapp"},
		{"whatsapp", "WA致谢 " + seedTag, "Thank you! Docs and deploy guide: https://gitee.com/xhpmayun/hivemtk", "whatsapp"},
		{"wecom", "企微加粉 " + seedTag, "感谢添加企业微信，HiveMTK 专属支持 1 对 1，请问有什么可以帮您？", "wecom"},
		{"wecom", "企微动态 " + seedTag, "【HiveMTK】新版本上线，私有化部署指南已更新，点击查看详情。", "wecom"},
		{"wecom", "企微售后 " + seedTag, "您的支持工单已受理，工单号SH20240724001，预计 12 小时内处理。", "wecom"},
		{"feishu", "飞书会议 " + seedTag, "已为您预约15分钟后的视频会议，请点击链接加入：https://meet.hivemtk.demo/abc", "feishu"},
		{"feishu", "飞书文档 " + seedTag, "HiveMTK 部署与运维文档已分享给您，请查收：https://docs.hivemtk.demo/prod-intro", "feishu"},
		{"email", "邮件欢迎 " + seedTag, "尊敬的社区伙伴您好，欢迎加入 HiveMTK 开源社区，请访问 Gitee 激活与查看文档：", "email"},
		{"email", "邮件回访 " + seedTag, "感谢您上周的咨询，想了解您是否还有其他问题，期待您的回复。", "email"},
		{"sms", "短信验证 " + seedTag, "【HiveMTK】您的验证码为：886523，5分钟内有效，请勿告知他人。", "sms"},
		{"sms", "短信通知 " + seedTag, "【HiveMTK】您的推理栈已就绪，访问 http://<ip>:8204 开始使用。", "sms"},
	}
	replies := make([]model.QuickReply, 0, len(specs))
	for i, sp := range specs {
		replies = append(replies, model.QuickReply{
			Category:  sp.Category,
			Title:     sp.Title,
			Content:   sp.Content,
			Channel:   sp.Channel,
			SortOrder: i,
			IsPublic:  true,
			CreatedBy: 1,
		})
	}
	return replies
}


