// seed_reach.go 模块 E：触达运营种子数据
//
// 覆盖表：
// reach_pipelines (5) 5个渠道 Pipeline
// reach_jobs (20) 触达任务，覆盖5种状态
// inbox_conversations (15) 收件箱会话
// inbox_assignments (10) 分配历史
// email_jobs (5) 邮件任务
// email_send (10) 邮件发送记录
// sms_jobs (5) 短信任务
// sms_job_details (15) 短信任务详情
// sms_records (10) 短信发送记录
// douyin_cards (5) 抖音卡片
// xiaohongshu_cards (5) 小红书卡片
// kuaishou_cards (5) 快手卡片
package main

import (
	"fmt"
	"log"
	"time"

	"hivemtk-user/internal/model"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type reachSeeder struct{}

func (s *reachSeeder) Name() string { return "reach" }
func (s *reachSeeder) Description() string {
	return "Pipeline(5)+任务(20)+收件箱(15)+分配(10)+邮件(15)+短信(30)+卡片(15)"
}

func (s *reachSeeder) Clean(database *gorm.DB) error {
	if _, err := cleanByCondition(database, &model.KuaishouCard{}, "title LIKE ?", "%"+seedTag+"%"); err != nil {
		return fmt.Errorf("清空 kuaishou_cards 失败: %w", err)
	}
	if _, err := cleanByCondition(database, &model.XiaohongshuCard{}, "title LIKE ?", "%"+seedTag+"%"); err != nil {
		return fmt.Errorf("清空 xiaohongshu_cards 失败: %w", err)
	}
	if _, err := cleanByCondition(database, &model.DouyinCard{}, "title LIKE ?", "%"+seedTag+"%"); err != nil {
		return fmt.Errorf("清空 douyin_cards 失败: %w", err)
	}
	if _, err := cleanByCondition(database, &model.SmsRecord{}, "content LIKE ?", "%"+seedTag+"%"); err != nil {
		return fmt.Errorf("清空 sms_records 失败: %w", err)
	}
	if _, err := cleanByCondition(database, &model.SmsJobDetail{}, "content LIKE ?", "%"+seedTag+"%"); err != nil {
		return fmt.Errorf("清空 sms_job_details 失败: %w", err)
	}
	if _, err := cleanByCondition(database, &model.SmsJob{}, "name LIKE ?", "%"+seedTag+"%"); err != nil {
		return fmt.Errorf("清空 sms_jobs 失败: %w", err)
	}
	if _, err := cleanByCondition(database, &model.EmailSend{}, "subject LIKE ?", "%"+seedTag+"%"); err != nil {
		return fmt.Errorf("清空 email_send 失败: %w", err)
	}
	if _, err := cleanByCondition(database, &model.EmailJobs{}, "subject LIKE ?", "%"+seedTag+"%"); err != nil {
		return fmt.Errorf("清空 email_jobs 失败: %w", err)
	}
	if _, err := cleanByCondition(database, &model.InboxAssignment{}, "remark LIKE ?", "%"+seedTag+"%"); err != nil {
		return fmt.Errorf("清空 inbox_assignments 失败: %w", err)
	}
	if _, err := cleanByCondition(database, &model.InboxConversation{}, "last_message_preview LIKE ?", "%"+seedTag+"%"); err != nil {
		return fmt.Errorf("清空 inbox_conversations 失败: %w", err)
	}
	if _, err := cleanByCondition(database, &model.ReachJob{}, "payload::text LIKE ?", "%"+seedTag+"%"); err != nil {
		return fmt.Errorf("清空 reach_jobs 失败: %w", err)
	}
	if _, err := cleanByCondition(database, &model.ReachPipeline{}, "name LIKE ?", "%"+seedTag+"%"); err != nil {
		return fmt.Errorf("清空 reach_pipelines 失败: %w", err)
	}
	return nil
}

func (s *reachSeeder) Seed(database *gorm.DB, ctx *SeedContext) error {
	pipelines := s.buildPipelines()
	if err := batchInsert(database, pipelines, 50); err != nil {
		return fmt.Errorf("写入 reach_pipelines 失败: %w", err)
	}
	for _, p := range pipelines {
		ctx.ReachPipelineIDs = append(ctx.ReachPipelineIDs, p.ID)
	}

	jobs := s.buildReachJobs(pipelines, ctx)
	if err := batchInsert(database, jobs, 50); err != nil {
		return fmt.Errorf("写入 reach_jobs 失败: %w", err)
	}
	for _, j := range jobs {
		ctx.ReachJobIDs = append(ctx.ReachJobIDs, j.ID)
	}

	inboxConvs := s.buildInboxConversations(ctx)
	if err := batchInsert(database, inboxConvs, 50); err != nil {
		return fmt.Errorf("写入 inbox_conversations 失败: %w", err)
	}
	for _, c := range inboxConvs {
		ctx.InboxConvIDs = append(ctx.InboxConvIDs, c.ID)
	}

	assignments := s.buildInboxAssignments(inboxConvs, ctx)
	if err := batchInsert(database, assignments, 50); err != nil {
		return fmt.Errorf("写入 inbox_assignments 失败: %w", err)
	}

	emailJobs := s.buildEmailJobs()
	if err := batchInsert(database, emailJobs, 50); err != nil {
		return fmt.Errorf("写入 email_jobs 失败: %w", err)
	}

	emailSends := s.buildEmailSends(ctx)
	if err := batchInsert(database, emailSends, 50); err != nil {
		return fmt.Errorf("写入 email_send 失败: %w", err)
	}

	smsJobs := s.buildSMSJobs()
	if err := batchInsert(database, smsJobs, 50); err != nil {
		return fmt.Errorf("写入 sms_jobs 失败: %w", err)
	}

	smsDetails := s.buildSMSJobDetails(smsJobs, ctx)
	if err := batchInsert(database, smsDetails, 50); err != nil {
		return fmt.Errorf("写入 sms_job_details 失败: %w", err)
	}

	smsRecords := s.buildSMSRecords(ctx)
	if err := batchInsert(database, smsRecords, 50); err != nil {
		return fmt.Errorf("写入 sms_records 失败: %w", err)
	}

	douyinCards := s.buildDouyinCards()
	if err := batchInsert(database, douyinCards, 50); err != nil {
		return fmt.Errorf("写入 douyin_cards 失败: %w", err)
	}

	xhsCards := s.buildXiaohongshuCards()
	if err := batchInsert(database, xhsCards, 50); err != nil {
		return fmt.Errorf("写入 xiaohongshu_cards 失败: %w", err)
	}

	ksCards := s.buildKuaishouCards()
	if err := batchInsert(database, ksCards, 50); err != nil {
		return fmt.Errorf("写入 kuaishou_cards 失败: %w", err)
	}

	log.Printf("  ✓ 已写入 Pipeline%d+任务%d+收件箱%d+分配%d+邮件任务%d+邮件发送%d+短信任务%d+短信详情%d+短信记录%d+抖音%d+小红书%d+快手%d",
		len(pipelines), len(jobs), len(inboxConvs), len(assignments),
		len(emailJobs), len(emailSends), len(smsJobs), len(smsDetails), len(smsRecords),
		len(douyinCards), len(xhsCards), len(ksCards))
	return nil
}

func (s *reachSeeder) buildPipelines() []model.ReachPipeline {
	pipelines := []model.ReachPipeline{
		{
			Name:         "企微全渠道触达Pipeline " + seedTag,
			Description:  "覆盖企微全渠道的9步触达流程，包括筛选→执行→反馈→优化",
			Channel:      "wecom",
			Steps:        model.JSONArray{s.pipelineStep(1, "筛选客户", "filter"), s.pipelineStep(2, "发送消息", "send"), s.pipelineStep(3, "等待回复", "wait"), s.pipelineStep(4, "处理回复", "process"), s.pipelineStep(5, "升级人工", "escalate")},
			RetryPolicy:  model.JSONMap{"max_retry": 3, "interval": "5m"},
			RateLimit:    model.JSONMap{"per_minute": 100, "per_day": 10000},
			Status:       "active",
			Version:      1,
			TotalRuns:    randInt64(100, 5000),
			TotalSuccess: randInt64(80, 4000),
			TotalFailure: randInt64(5, 200),
		},
		{
			Name:         "邮件营销Pipeline " + seedTag,
			Description:  "邮件营销触达，包含模板渲染+退订处理+跟踪",
			Channel:      "email",
			Steps:        model.JSONArray{s.pipelineStep(1, "选择模板", "template"), s.pipelineStep(2, "渲染内容", "render"), s.pipelineStep(3, "SMTP发送", "send"), s.pipelineStep(4, "跟踪打开", "track")},
			RetryPolicy:  model.JSONMap{"max_retry": 2, "interval": "30m"},
			RateLimit:    model.JSONMap{"per_minute": 50, "per_day": 5000},
			Status:       "active",
			Version:      2,
			TotalRuns:    randInt64(50, 2000),
			TotalSuccess: randInt64(40, 1800),
			TotalFailure: randInt64(2, 100),
		},
		{
			Name:         "短信通知Pipeline " + seedTag,
			Description:  "短信通知触达，含阿里云/腾讯云/华为云多通道支持",
			Channel:      "sms",
			Steps:        model.JSONArray{s.pipelineStep(1, "选择通道", "select_provider"), s.pipelineStep(2, "签名校验", "verify"), s.pipelineStep(3, "发送短信", "send"), s.pipelineStep(4, "状态回执", "receipt")},
			RetryPolicy:  model.JSONMap{"max_retry": 3, "interval": "1m"},
			RateLimit:    model.JSONMap{"per_minute": 200, "per_day": 20000},
			Status:       "active",
			Version:      1,
			TotalRuns:    randInt64(200, 8000),
			TotalSuccess: randInt64(180, 7500),
			TotalFailure: randInt64(10, 300),
		},
		{
			Name:         "抖音私信触达Pipeline " + seedTag,
			Description:  "抖音私信+卡片触达，覆盖短视频粉丝",
			Channel:      "douyin",
			Steps:        model.JSONArray{s.pipelineStep(1, "粉丝筛选", "filter"), s.pipelineStep(2, "卡片发送", "send_card"), s.pipelineStep(3, "互动跟踪", "track")},
			RetryPolicy:  model.JSONMap{"max_retry": 2, "interval": "10m"},
			RateLimit:    model.JSONMap{"per_minute": 30, "per_day": 1000},
			Status:       "paused",
			Version:      1,
			TotalRuns:    randInt64(20, 500),
			TotalSuccess: randInt64(15, 400),
			TotalFailure: randInt64(2, 50),
		},
		{
			Name:         "小红书种草Pipeline " + seedTag,
			Description:  "小红书种草+引流，含笔记触达和私信跟进",
			Channel:      "xiaohongshu",
			Steps:        model.JSONArray{s.pipelineStep(1, "笔记发布", "publish"), s.pipelineStep(2, "互动引导", "engage"), s.pipelineStep(3, "私信转化", "message")},
			RetryPolicy:  model.JSONMap{"max_retry": 1, "interval": "1h"},
			RateLimit:    model.JSONMap{"per_minute": 20, "per_day": 500},
			Status:       "archived",
			Version:      1,
			TotalRuns:    randInt64(10, 200),
			TotalSuccess: randInt64(5, 150),
			TotalFailure: randInt64(1, 30),
		},
	}
	return pipelines
}

func (s *reachSeeder) pipelineStep(order int, name, action string) map[string]any {
	return map[string]any{
		"order":  order,
		"name":   name,
		"action": action,
		"seed":   seedTag,
	}
}

func (s *reachSeeder) buildReachJobs(pipelines []model.ReachPipeline, ctx *SeedContext) []model.ReachJob {
	if len(pipelines) == 0 {
		return nil
	}
	states := []string{"pending", "running", "success", "failed", "canceled"}
	jobs := make([]model.ReachJob, 0, 20)
	for i := 0; i < 20; i++ {
		p := pipelines[i%len(pipelines)]
		state := states[i%len(states)]
		customerID := ""
		if len(ctx.CustomerIDs) > 0 {
			customerID = ctx.CustomerIDs[i%len(ctx.CustomerIDs)]
		}
		now := time.Now()
		startedAt := daysAgo(randInt(0, 25))
		completedAt := startedAt.Add(time.Duration(randInt(60, 3600)) * time.Second)
		var nextRunAt *time.Time
		if state == "pending" || state == "running" {
			t := now.Add(time.Duration(randInt(1, 24)) * time.Hour)
			nextRunAt = &t
		}
		errMsg := ""
		if state == "failed" {
			errMsg = "触达失败：渠道限流"
		}
		job := model.ReachJob{
			PipelineID:   p.ID,
			Channel:      p.Channel,
			CustomerID:   customerID,
			AccountID:    fmt.Sprintf("seed-account-%d", i+1),
			Payload:      model.JSONMap{"seed": seedTag, "title": fmt.Sprintf("触达任务#%d", i+1), "content": "开源动态与教程分享"},
			State:        state,
			CurrentStep:  randInt(1, 5),
			StepResults:  model.JSONArray{s.pipelineStep(1, "已完成", "done")},
			RetryCount:   randInt(0, 3),
			MaxRetry:     3,
			NextRunAt:    nextRunAt,
			StartedAt:    &startedAt,
			CompletedAt:  &completedAt,
			ErrorMessage: errMsg,
			DurationMs:   randInt(100, 5000),
		}
		jobs = append(jobs, job)
	}
	return jobs
}

func (s *reachSeeder) buildInboxConversations(ctx *SeedContext) []model.InboxConversation {
	if len(ctx.CustomerIDs) == 0 {
		return nil
	}
	platforms := []string{"wecom", "douyin", "xiaohongshu", "feishu", "dingtalk", "whatsapp", "telegram"}
	statuses := []string{"unread", "open", "assigned", "closed"}
	fromTypes := []string{"customer", "staff", "ai"}
	convs := make([]model.InboxConversation, 0, 15)
	for i := 0; i < 15; i++ {
		platform := platforms[i%len(platforms)]
		status := statuses[i%len(statuses)]
		fromType := fromTypes[i%len(fromTypes)]
		customerID := ctx.CustomerIDs[i%len(ctx.CustomerIDs)]
		lastMsgAt := hoursAgo(randInt(1, 72))
		var assignedTo string
		var assignedAt *time.Time
		if status == "assigned" || status == "closed" {
			if len(ctx.CSUserIDs) > 0 {
				assignedTo = fmt.Sprintf("%d", ctx.CSUserIDs[i%len(ctx.CSUserIDs)])
				t := hoursAgo(randInt(1, 24))
				assignedAt = &t
			}
		}
		var closedAt *time.Time
		if status == "closed" {
			t := hoursAgo(randInt(1, 12))
			closedAt = &t
		}
		conv := model.InboxConversation{
			Platform:           platform,
			AccountID:          fmt.Sprintf("seed-account-%d", i+1),
			CustomerID:         customerID,
			CustomerName:       fmt.Sprintf("客户%d号", i+1),
			ConversationID:     fmt.Sprintf("seed-conv-%d", i+1),
			Status:             status,
			AssignedTo:         assignedTo,
			AssignedAt:         assignedAt,
			UnreadCount:        randInt(0, 5),
			TotalCount:         randInt(5, 50),
			LastMessageID:      uint(i + 1),
			LastMessagePreview: fmt.Sprintf("最近消息预览 #%d %s", i+1, seedTag),
			LastMessageAt:      &lastMsgAt,
			LastMessageFrom:    fromType,
			Pinned:             i%5 == 0,
			Starred:            i%7 == 0,
			Muted:              i%11 == 0,
			Tags:               model.JSONArray{platform, "seed"},
			Extra:              model.JSONMap{"seed": seedTag},
			ClosedAt:           closedAt,
		}
		convs = append(convs, conv)
	}
	return convs
}

func (s *reachSeeder) buildInboxAssignments(convs []model.InboxConversation, ctx *SeedContext) []model.InboxAssignment {
	if len(convs) == 0 {
		return nil
	}
	actions := []string{"assign", "reassign", "release", "close", "reopen"}
	toTypes := []string{"human", "ai", "system"}
	assignments := make([]model.InboxAssignment, 0, 10)
	for i := 0; i < 10; i++ {
		conv := convs[i%len(convs)]
		toType := toTypes[i%len(toTypes)]
		var toUserID string
		var toSOPID uint
		if toType == "human" && len(ctx.CSUserIDs) > 0 {
			toUserID = fmt.Sprintf("%d", ctx.CSUserIDs[i%len(ctx.CSUserIDs)])
		}
		if toType == "ai" && len(ctx.SOPAgentIDs) > 0 {
			toSOPID = ctx.SOPAgentIDs[i%len(ctx.SOPAgentIDs)]
		}
		a := model.InboxAssignment{
			ConversationID: conv.ID,
			Platform:       conv.Platform,
			AccountID:      conv.AccountID,
			CustomerID:     conv.CustomerID,
			Action:         actions[i%len(actions)],
			FromType:       []string{"human", "ai", "system"}[i%3],
			ToType:         toType,
			ToUserID:       toUserID,
			ToSOPID:        toSOPID,
			OperatorID:     fmt.Sprintf("seed-op-%d", i+1),
			Remark:         fmt.Sprintf("分配操作 #%d %s", i+1, seedTag),
		}
		assignments = append(assignments, a)
	}
	return assignments
}

func (s *reachSeeder) buildEmailJobs() []model.EmailJobs {
	subjects := []string{
		"HiveMTK v1.0 版本发布预告 " + seedTag,
		"HiveMTK 开源贡献者招募 " + seedTag,
		"HiveMTK 功能更新通知 v2.0 " + seedTag,
		"HiveMTK 社区月报 " + seedTag,
		"用户调研邀请 " + seedTag,
	}
	jobs := make([]model.EmailJobs, 0, 5)
	for _, subj := range subjects {
		total := int64(randInt(100, 5000))
		success := int64(randInt(80, 4500))
		fail := total - success
		read := int64(randInt(40, 3000))
		j := model.EmailJobs{
			Subject:      subj,
			SendTotal:    total,
			EmailTotal:   total,
			SuccessTotal: success,
			FailTotal:    fail,
			ReadTotal:    read,
		}
		jobs = append(jobs, j)
	}
	return jobs
}

func (s *reachSeeder) buildEmailSends(ctx *SeedContext) []model.EmailSend {
	if len(ctx.CustomerIDs) == 0 {
		return nil
	}
	statuses := []int{0, 1, 1, 1, 2}
	sends := make([]model.EmailSend, 0, 10)
	for i := 0; i < 10; i++ {
		status := statuses[i%len(statuses)]
		sendTime := daysAgo(randInt(0, 25))
		var sendTimePtr *time.Time
		if status != 0 {
			sendTimePtr = &sendTime
		}
		subject := fmt.Sprintf("HiveMTK 动态邮件 #%d %s", i+1, seedTag)
		email := fmt.Sprintf("seed-user%d@demo.com", i+1)
		es := model.EmailSend{
			ID:          uuid.New().String(),
			To:          email,
			Subject:     subject,
			Content:     fmt.Sprintf("尊敬的社区伙伴，HiveMTK 近期更新与教程已发布，欢迎到 GitHub/Gitee 查看部署指南与更新日志。%s", seedTag),
			Attachments: "",
			Status:      status,
			SendTime:    sendTimePtr,
			SmtpID:      fmt.Sprintf("seed-smtp-%d", i%3+1),
		}
		sends = append(sends, es)
	}
	return sends
}

func (s *reachSeeder) buildSMSJobs() []model.SmsJob {
	names := []string{
		"版本发布通知短信 " + seedTag,
		"社区动态推送短信 " + seedTag,
		"构建完成通知短信 " + seedTag,
		"贡献者招募短信 " + seedTag,
		"验证码下发任务 " + seedTag,
	}
	statuses := []string{"pending", "running", "completed", "completed", "failed"}
	jobs := make([]model.SmsJob, 0, 5)
	for i, name := range names {
		total := randInt(100, 5000)
		sent := randInt(80, 4500)
		failed := total - sent
		if statuses[i] == "failed" {
			failed = total
			sent = 0
		}
		var scheduleTime *time.Time
		if statuses[i] == "pending" {
			t := time.Now().Add(time.Duration(randInt(1, 48)) * time.Hour)
			scheduleTime = &t
		}
		j := model.SmsJob{
			Name:         name,
			Total:        total,
			Sent:         sent,
			Failed:       failed,
			Status:       statuses[i],
			ScheduleTime: scheduleTime,
		}
		jobs = append(jobs, j)
	}
	return jobs
}

func (s *reachSeeder) buildSMSJobDetails(jobs []model.SmsJob, ctx *SeedContext) []model.SmsJobDetail {
	if len(jobs) == 0 {
		return nil
	}
	statuses := []string{"pending", "sent", "sent", "failed"}
	details := make([]model.SmsJobDetail, 0, 15)
	for i := 0; i < 15; i++ {
		job := jobs[i%len(jobs)]
		status := statuses[i%len(statuses)]
		var sendTime *time.Time
		if status == "sent" {
			t := daysAgo(randInt(0, 20))
			sendTime = &t
		}
		errMsg := ""
		if status == "failed" {
			errMsg = "号码空号"
		}
		phone := fmt.Sprintf("138%08d", i+1)
		if len(ctx.CustomerIDs) > 0 {
			_ = ctx.CustomerIDs[i%len(ctx.CustomerIDs)]
		}
		detail := model.SmsJobDetail{
			JobID:   job.ID,
			Phone:   phone,
			Content: fmt.Sprintf("【HiveMTK】新版本已发布，查看更新日志与部署指南：https://gitee.com/xhpmayun/hivemtk 。%s", seedTag),
			Status:  status,
			ErrorCode: func() string {
				if status == "failed" {
					return "EMPTY_NUMBER"
				}
				return ""
			}(),
			ErrorMsg: errMsg,
			SendTime: sendTime,
		}
		details = append(details, detail)
	}
	return details
}

func (s *reachSeeder) buildSMSRecords(ctx *SeedContext) []model.SmsRecord {
	providers := []string{"aliyun", "tencent", "huawei"}
	statuses := []string{"pending", "sent", "sent", "failed"}
	records := make([]model.SmsRecord, 0, 10)
	for i := 0; i < 10; i++ {
		status := statuses[i%len(statuses)]
		var sendTime *time.Time
		if status == "sent" {
			t := daysAgo(randInt(0, 15))
			sendTime = &t
		}
		errMsg := ""
		if status == "failed" {
			errMsg = "运营商拦截"
		}
		rec := model.SmsRecord{
			Phone:    fmt.Sprintf("139%08d", i+1),
			Content:  fmt.Sprintf("【HiveMTK】您的验证码：%d，5 分钟内有效。%s", randInt(100000, 999999), seedTag),
			Provider: providers[i%len(providers)],
			Status:   status,
			ErrorCode: func() string {
				if status == "failed" {
					return "BLOCKED"
				}
				return ""
			}(),
			ErrorMsg: errMsg,
			SendTime: sendTime,
		}
		records = append(records, rec)
	}
	return records
}

func (s *reachSeeder) buildDouyinCards() []model.DouyinCard {
	cards := []model.DouyinCard{
		{
			Title:       "开源部署教程·3 步私有化 " + seedTag,
			Description: "Docker Compose 私域部署，从 git clone 到 make up",
			ImageURL:    "https://example.com/seed/douyin1.jpg",
			RedirectURL: "https://gitee.com/xhpmayun/hivemtk",
			Tags:        "部署,教程",
			ViewCount:   randInt(1000, 50000),
			ShareCount:  randInt(50, 2000),
			IsActive:    true,
		},
		{
			Title:       "核心功能亮点 " + seedTag,
			Description: "七端接入 + ReAct 智能体 + 三级 RAG + 零出域",
			ImageURL:    "https://example.com/seed/douyin2.jpg",
			RedirectURL: "https://gitee.com/xhpmayun/hivemtk",
			Tags:        "功能,亮点",
			ViewCount:   randInt(5000, 100000),
			ShareCount:  randInt(200, 5000),
			IsActive:    true,
		},
		{
			Title:       "v1.0 版本发布预告 " + seedTag,
			Description: "新版本即将发布，敬请期待更新日志",
			ImageURL:    "https://example.com/seed/douyin3.jpg",
			RedirectURL: "https://gitee.com/xhpmayun/hivemtk",
			Tags:        "版本,预告",
			ViewCount:   randInt(500, 30000),
			ShareCount:  randInt(20, 1000),
			IsActive:    true,
		},
		{
			Title:       "微信交流群招募 " + seedTag,
			Description: "加入交流群，7x24 答疑与共建",
			ImageURL:    "https://example.com/seed/douyin4.jpg",
			RedirectURL: "https://gitee.com/xhpmayun/hivemtk",
			Tags:        "社群,招募",
			ViewCount:   randInt(800, 20000),
			ShareCount:  randInt(30, 800),
			IsActive:    true,
		},
		{
			Title:       "我们为什么做开源 AI 营销 " + seedTag,
			Description: "聊聊 HiveMTK 的起源与开源理念",
			ImageURL:    "https://example.com/seed/douyin5.jpg",
			RedirectURL: "https://gitee.com/xhpmayun/hivemtk",
			Tags:        "品牌,故事",
			ViewCount:   randInt(2000, 80000),
			ShareCount:  randInt(100, 3000),
			IsActive:    false,
		},
	}
	return cards
}

func (s *reachSeeder) buildXiaohongshuCards() []model.XiaohongshuCard {
	cards := []model.XiaohongshuCard{
		{
			Title:       "部署笔记·Docker Compose 私域部署 " + seedTag,
			Description: "从 0 到 1 跑通 HiveMTK 私有化",
			ImageURL:    "https://example.com/seed/xhs1.jpg",
			RedirectURL: "https://gitee.com/xhpmayun/hivemtk",
			ShareURL:    "https://example.com/seed/xhs/share1",
			Tags:        "部署,笔记",
			ViewCount:   randInt(1000, 30000),
			IsActive:    true,
		},
		{
			Title:       "开源 AI 营销工具推荐 " + seedTag,
			Description: "零出域、可私有化的智能体套件",
			ImageURL:    "https://example.com/seed/xhs2.jpg",
			RedirectURL: "https://gitee.com/xhpmayun/hivemtk",
			ShareURL:    "https://example.com/seed/xhs/share2",
			Tags:        "工具,推荐",
			ViewCount:   randInt(2000, 50000),
			IsActive:    true,
		},
		{
			Title:       "新手教程 " + seedTag,
			Description: "新手必看，5 分钟跑通最小闭环",
			ImageURL:    "https://example.com/seed/xhs3.jpg",
			RedirectURL: "https://gitee.com/xhpmayun/hivemtk",
			ShareURL:    "https://example.com/seed/xhs/share3",
			Tags:        "教程,新手",
			ViewCount:   randInt(500, 15000),
			IsActive:    true,
		},
		{
			Title:       "用户案例 " + seedTag,
			Description: "私域运营实战案例分享",
			ImageURL:    "https://example.com/seed/xhs4.jpg",
			RedirectURL: "https://gitee.com/xhpmayun/hivemtk",
			ShareURL:    "https://example.com/seed/xhs/share4",
			Tags:        "案例,口碑",
			ViewCount:   randInt(800, 20000),
			IsActive:    true,
		},
		{
			Title:       "贡献者招募·一起共建 " + seedTag,
			Description: "Fork + PR，参与 HiveMTK 开源",
			ImageURL:    "https://example.com/seed/xhs5.jpg",
			RedirectURL: "https://gitee.com/xhpmayun/hivemtk",
			ShareURL:    "https://example.com/seed/xhs/share5",
			Tags:        "贡献,招募",
			ViewCount:   randInt(300, 10000),
			IsActive:    false,
		},
	}
	return cards
}

func (s *reachSeeder) buildKuaishouCards() []model.KuaishouCard {
	cards := []model.KuaishouCard{
		{
			Title:       "直播预告·今晚 8 点讲部署 " + seedTag,
			Description: "手把手演示私有化部署与排障",
			ImageURL:    "https://example.com/seed/ks1.jpg",
			RedirectURL: "https://gitee.com/xhpmayun/hivemtk",
			Tags:        "直播,部署",
			ViewCount:   randInt(1000, 30000),
			ShareCount:  randInt(50, 1000),
			IsActive:    true,
		},
		{
			Title:       "开源共建福利·加群领资料 " + seedTag,
			Description: "加入交流群，领取部署与运维资料",
			ImageURL:    "https://example.com/seed/ks2.jpg",
			RedirectURL: "https://gitee.com/xhpmayun/hivemtk",
			Tags:        "福利,共建",
			ViewCount:   randInt(2000, 50000),
			ShareCount:  randInt(100, 2000),
			IsActive:    true,
		},
		{
			Title:       "版本首发·v1.0 " + seedTag,
			Description: "v1.0 发布，限前 100 位 star 伙伴",
			ImageURL:    "https://example.com/seed/ks3.jpg",
			RedirectURL: "https://gitee.com/xhpmayun/hivemtk",
			Tags:        "版本,首发",
			ViewCount:   randInt(800, 20000),
			ShareCount:  randInt(40, 800),
			IsActive:    true,
		},
		{
			Title:       "star 抽奖·感谢贡献者 " + seedTag,
			Description: "为贡献者送出周边与定制礼物",
			ImageURL:    "https://example.com/seed/ks4.jpg",
			RedirectURL: "https://gitee.com/xhpmayun/hivemtk",
			Tags:        "抽奖,贡献者",
			ViewCount:   randInt(500, 15000),
			ShareCount:  randInt(30, 600),
			IsActive:    true,
		},
		{
			Title:       "开源周年专场 " + seedTag,
			Description: "聊聊一年来的迭代与路线图",
			ImageURL:    "https://example.com/seed/ks5.jpg",
			RedirectURL: "https://gitee.com/xhpmayun/hivemtk",
			Tags:        "周年,专场",
			ViewCount:   randInt(1000, 25000),
			ShareCount:  randInt(50, 1200),
			IsActive:    false,
		},
	}
	return cards
}
