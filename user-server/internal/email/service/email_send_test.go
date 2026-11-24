package email

import (
	"context"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/repository"
	"testing"
	"time"

	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func setupEmailSendServiceTestDB(t *testing.T) *gorm.DB {
	database := testutil.NewTestDB(t,
		&model.EmailSend{},
		&model.EmailSmtp{},
		&model.EmailDraft{},
		&model.EmailList{},
		&model.EmailJobs{},
	)
	db.SetTestDB(database)
	return database
}

// TestNewEmailSendService 测试创建邮件发送服务
func TestNewEmailSendService(t *testing.T) {
	setupEmailSendServiceTestDB(t)

	service := NewEmailSendService()
	if service == nil {
		t.Error("Expected non-nil service")
	}
}

// TestEmailSendService_SendEmail_ImmediateSend 测试立即发送邮件
func TestEmailSendService_SendEmail_ImmediateSend(t *testing.T) {
	setupEmailSendServiceTestDB(t)
	smtpRepo := repository.NewEmailSmtpRepository()

	smtp := &model.EmailSmtp{
		Name:     "测试 SMTP",
		Server:   "smtp.example.com",
		Port:     587,
		Username: "test@example.com",
		Password: "passw" + "ord123",
		Limit:    100,
	}
	if err := smtpRepo.Create(context.Background(), smtp); err != nil {
		t.Fatalf("Failed to create SMTP: %v", err)
	}

	service := NewEmailSendService()

	req := dto.SendEmailRequest{
		To:            "recipient@example.com",
		Subject:       "测试邮件主题",
		Content:       "这是一封测试邮件",
		Attachments:   []string{"attachment1.pdf", "attachment2.docx"},
		SmtpId:        smtp.ID,
		ImmediateSend: true,
	}

	email, err := service.SendEmail(context.Background(), req)
	if err != nil {
		t.Fatalf("SendEmail failed: %v", err)
	}

	if email == nil {
		t.Fatal("Expected non-nil email")
	}

	if email.To != req.To {
		t.Errorf("Expected to '%s', got %s", req.To, email.To)
	}

	if email.Subject != req.Subject {
		t.Errorf("Expected subject '%s', got %s", req.Subject, email.Subject)
	}

	if email.Content != req.Content {
		t.Errorf("Expected content '%s', got %s", req.Content, email.Content)
	}

	expectedAttachments := "attachment1.pdf,attachment2.docx"
	if email.Attachments != expectedAttachments {
		t.Errorf("Expected attachments '%s', got %s", expectedAttachments, email.Attachments)
	}

	if email.SmtpID != req.SmtpId {
		t.Errorf("Expected smtp_id '%s', got %s", req.SmtpId, email.SmtpID)
	}

	if email.ID == "" {
		t.Error("Expected non-empty ID")
	}
}

// TestEmailSendService_SendEmail_ScheduledSend 测试计划发送邮件
func TestEmailSendService_SendEmail_ScheduledSend(t *testing.T) {
	setupEmailSendServiceTestDB(t)
	smtpRepo := repository.NewEmailSmtpRepository()

	smtp := &model.EmailSmtp{
		Name:     "测试 SMTP",
		Server:   "smtp.example.com",
		Port:     587,
		Username: "test@example.com",
		Password: "passw" + "ord123",
		Limit:    100,
	}
	if err := smtpRepo.Create(context.Background(), smtp); err != nil {
		t.Fatalf("Failed to create SMTP: %v", err)
	}

	service := NewEmailSendService()

	sendTime := time.Now().Add(24 * time.Hour)

	req := dto.SendEmailRequest{
		To:            "recipient@example.com",
		Subject:       "计划邮件主题",
		Content:       "这是一封计划发送的邮件",
		SmtpId:        smtp.ID,
		SendTime:      &sendTime,
		ImmediateSend: false,
	}

	email, err := service.SendEmail(context.Background(), req)
	if err != nil {
		t.Fatalf("SendEmail failed: %v", err)
	}

	if email == nil {
		t.Fatal("Expected non-nil email")
	}

	if email.To != req.To {
		t.Errorf("Expected to '%s', got %s", req.To, email.To)
	}

	if email.SendTime == nil {
		t.Error("Expected SendTime to be set")
	}
}

// TestEmailSendService_SendEmail_EmptyAttachments 测试空附件列表
func TestEmailSendService_SendEmail_EmptyAttachments(t *testing.T) {
	setupEmailSendServiceTestDB(t)
	smtpRepo := repository.NewEmailSmtpRepository()

	smtp := &model.EmailSmtp{
		Name:     "测试 SMTP",
		Server:   "smtp.example.com",
		Port:     587,
		Username: "test@example.com",
		Password: "passw" + "ord123",
		Limit:    100,
	}
	if err := smtpRepo.Create(context.Background(), smtp); err != nil {
		t.Fatalf("Failed to create SMTP: %v", err)
	}

	service := NewEmailSendService()

	req := dto.SendEmailRequest{
		To:            "recipient@example.com",
		Subject:       "无附件邮件",
		Content:       "这是一封没有附件的邮件",
		Attachments:   []string{},
		SmtpId:        smtp.ID,
		ImmediateSend: true,
	}

	email, err := service.SendEmail(context.Background(), req)
	if err != nil {
		t.Fatalf("SendEmail failed: %v", err)
	}

	if email == nil {
		t.Fatal("Expected non-nil email")
	}

	if email.Attachments != "" {
		t.Errorf("Expected empty attachments, got %s", email.Attachments)
	}

	if email.To != req.To {
		t.Errorf("Expected to '%s', got %s", req.To, email.To)
	}
}

// TestEmailSendService_SendEmail_WithDraftId 测试带草稿 ID 的邮件发送
func TestEmailSendService_SendEmail_WithDraftId(t *testing.T) {
	setupEmailSendServiceTestDB(t)
	smtpRepo := repository.NewEmailSmtpRepository()

	smtp := &model.EmailSmtp{
		Name:     "测试 SMTP",
		Server:   "smtp.example.com",
		Port:     587,
		Username: "test@example.com",
		Password: "passw" + "ord123",
		Limit:    100,
	}
	if err := smtpRepo.Create(context.Background(), smtp); err != nil {
		t.Fatalf("Failed to create SMTP: %v", err)
	}

	draft := &model.EmailDraft{
		Subject: "草稿主题",
		Content: "草稿内容",
	}
	if err := db.GetDB().Create(draft).Error; err != nil {
		t.Fatalf("Failed to create draft: %v", err)
	}

	service := NewEmailSendService()

	req := dto.SendEmailRequest{
		DraftId:       draft.ID.String(),
		To:            "recipient@example.com",
		Subject:       "来自草稿的邮件",
		Content:       "这是从草稿发送的邮件",
		SmtpId:        smtp.ID,
		ImmediateSend: true,
	}

	email, err := service.SendEmail(context.Background(), req)
	if err != nil {
		t.Fatalf("SendEmail failed: %v", err)
	}

	if email == nil {
		t.Fatal("Expected non-nil email")
	}

	if email.ID == "" {
		t.Error("Expected non-empty ID")
	}
}

// TestEmailSendService_SendEmail_MultipleRecipients 测试多个收件人
func TestEmailSendService_SendEmail_MultipleRecipients(t *testing.T) {
	setupEmailSendServiceTestDB(t)
	smtpRepo := repository.NewEmailSmtpRepository()

	smtp := &model.EmailSmtp{
		Name:     "测试 SMTP",
		Server:   "smtp.example.com",
		Port:     587,
		Username: "test@example.com",
		Password: "passw" + "ord123",
		Limit:    100,
	}
	if err := smtpRepo.Create(context.Background(), smtp); err != nil {
		t.Fatalf("Failed to create SMTP: %v", err)
	}

	service := NewEmailSendService()

	req := dto.SendEmailRequest{
		To:            "user1@example.com,user2@example.com,user3@example.com",
		Subject:       "群发邮件",
		Content:       "这是群发给多个收件人的邮件",
		SmtpId:        smtp.ID,
		ImmediateSend: true,
	}

	email, err := service.SendEmail(context.Background(), req)
	if err != nil {
		t.Fatalf("SendEmail failed: %v", err)
	}

	if email == nil {
		t.Fatal("Expected non-nil email")
	}

	if email.To != req.To {
		t.Errorf("Expected to '%s', got %s", req.To, email.To)
	}

	if email.ID == "" {
		t.Error("Expected non-empty ID")
	}
}

// TestEmailSendService_SendEmail_SpecialCharacters 测试特殊字符
func TestEmailSendService_SendEmail_SpecialCharacters(t *testing.T) {
	setupEmailSendServiceTestDB(t)
	smtpRepo := repository.NewEmailSmtpRepository()

	smtp := &model.EmailSmtp{
		Name:     "测试 SMTP",
		Server:   "smtp.example.com",
		Port:     587,
		Username: "test@example.com",
		Password: "passw" + "ord123",
		Limit:    100,
	}
	if err := smtpRepo.Create(context.Background(), smtp); err != nil {
		t.Fatalf("Failed to create SMTP: %v", err)
	}

	service := NewEmailSendService()

	req := dto.SendEmailRequest{
		To:            "recipient@example.com",
		Subject:       "测试邮件主题 - 特殊字符测试！@#$%%^&*()",
		Content:       "这是包含特殊字符的邮件内容：\n换行符\n制表符\t中文内容\n<br>HTML 标签",
		SmtpId:        smtp.ID,
		ImmediateSend: true,
	}

	email, err := service.SendEmail(context.Background(), req)
	if err != nil {
		t.Fatalf("SendEmail failed: %v", err)
	}

	if email == nil {
		t.Fatal("Expected non-nil email")
	}

	if email.Content != req.Content {
		t.Errorf("Expected content '%s', got %s", req.Content, email.Content)
	}

	if email.ID == "" {
		t.Error("Expected non-empty ID")
	}
}

// TestEmailSendService_ProcessPendingEmails_NoPendingEmails 测试没有待处理邮件的情况
func TestEmailSendService_ProcessPendingEmails_NoPendingEmails(t *testing.T) {
	database := setupEmailSendServiceTestDB(t)
	_ = repository.NewEmailSendRepository()

	service := NewEmailSendService()

	err := service.ProcessPendingEmails(context.Background())
	if err != nil {
		t.Fatalf("ProcessPendingEmails failed: %v", err)
	}

	var count int64
	database.Model(&model.EmailSend{}).Where("status = ?", 0).Count(&count)
	if count != 0 {
		t.Errorf("Expected 0 pending emails, got %d", count)
	}
}

// TestEmailSendService_ProcessPendingEmails_WithPendingEmails 测试有待处理邮件的情况
func TestEmailSendService_ProcessPendingEmails_WithPendingEmails(t *testing.T) {
	database := setupEmailSendServiceTestDB(t)
	smtpRepo := repository.NewEmailSmtpRepository()

	smtp := &model.EmailSmtp{
		Name:     "测试 SMTP",
		Server:   "smtp.example.com",
		Port:     587,
		Username: "test@example.com",
		Password: "passw" + "ord123",
		Limit:    100,
	}
	if err := smtpRepo.Create(context.Background(), smtp); err != nil {
		t.Fatalf("Failed to create SMTP: %v", err)
	}

	pastTime := time.Now().Add(-1 * time.Hour)
	pendingEmail1 := &model.EmailSend{
		To:       "pending1@example.com",
		Subject:  "待处理邮件 1",
		Content:  "这是待处理邮件 1 的内容",
		SmtpID:   smtp.ID,
		Status:   0,
		SendTime: &pastTime,
	}
	pendingEmail2 := &model.EmailSend{
		To:       "pending2@example.com",
		Subject:  "待处理邮件 2",
		Content:  "这是待处理邮件 2 的内容",
		SmtpID:   smtp.ID,
		Status:   0,
		SendTime: &pastTime,
	}

	if err := database.Create(pendingEmail1).Error; err != nil {
		t.Fatalf("Failed to create pending email 1: %v", err)
	}
	if err := database.Create(pendingEmail2).Error; err != nil {
		t.Fatalf("Failed to create pending email 2: %v", err)
	}

	futureTime := time.Now().Add(1 * time.Hour)
	futureEmail := &model.EmailSend{
		To:       "future@example.com",
		Subject:  "未来邮件",
		Content:  "这是未来发送的邮件",
		SmtpID:   smtp.ID,
		Status:   0,
		SendTime: &futureTime,
	}
	if err := database.Create(futureEmail).Error; err != nil {
		t.Fatalf("Failed to create future email: %v", err)
	}

	service := NewEmailSendService()

	err := service.ProcessPendingEmails(context.Background())
	if err != nil {
		t.Fatalf("ProcessPendingEmails failed: %v", err)
	}

	var count int64
	database.Model(&model.EmailSend{}).Where("status = ?", 0).Count(&count)
	if count != 1 {
		t.Errorf("Expected 1 pending email (future email), got %d", count)
	}

	var failedCount int64
	database.Model(&model.EmailSend{}).Where("status = ?", 2).Count(&failedCount)
	if failedCount != 2 {
		t.Errorf("Expected 2 failed emails (SMTP unreachable), got %d", failedCount)
	}
}

// TestEmailSendService_ProcessPendingEmails_WithMixedStatusEmails 测试混合状态的邮件
func TestEmailSendService_ProcessPendingEmails_WithMixedStatusEmails(t *testing.T) {
	database := setupEmailSendServiceTestDB(t)
	smtpRepo := repository.NewEmailSmtpRepository()

	smtp := &model.EmailSmtp{
		Name:     "测试 SMTP",
		Server:   "smtp.example.com",
		Port:     587,
		Username: "test@example.com",
		Password: "passw" + "ord123",
		Limit:    100,
	}
	if err := smtpRepo.Create(context.Background(), smtp); err != nil {
		t.Fatalf("Failed to create SMTP: %v", err)
	}

	now := time.Now()

	emails := []*model.EmailSend{
		{To: "sent@example.com", Subject: "已发送", Content: "已发送内容", SmtpID: smtp.ID, Status: 1, SendTime: &now},
		{To: "failed@example.com", Subject: "发送失败", Content: "失败内容", SmtpID: smtp.ID, Status: 2, SendTime: &now},
		{To: "pending@example.com", Subject: "待发送", Content: "待发送内容", SmtpID: smtp.ID, Status: 0, SendTime: &now},
	}

	for _, email := range emails {
		if err := database.Create(email).Error; err != nil {
			t.Fatalf("Failed to create email: %v", err)
		}
	}

	service := NewEmailSendService()

	err := service.ProcessPendingEmails(context.Background())
	if err != nil {
		t.Fatalf("ProcessPendingEmails failed: %v", err)
	}

	var sentCount, failedCount, pendingCount int64
	database.Model(&model.EmailSend{}).Where("status = ?", 1).Count(&sentCount)
	database.Model(&model.EmailSend{}).Where("status = ?", 2).Count(&failedCount)
	database.Model(&model.EmailSend{}).Where("status = ?", 0).Count(&pendingCount)

	if sentCount != 1 {
		t.Errorf("Expected 1 sent email, got %d", sentCount)
	}
	if failedCount != 2 {
		t.Errorf("Expected 2 failed emails (1 original + 1 processed), got %d", failedCount)
	}
	if pendingCount != 0 {
		t.Errorf("Expected 0 pending emails (processed), got %d", pendingCount)
	}
}

// TestEmailSendService_SendEmail_LongContent 测试长内容邮件
func TestEmailSendService_SendEmail_LongContent(t *testing.T) {
	setupEmailSendServiceTestDB(t)
	smtpRepo := repository.NewEmailSmtpRepository()

	smtp := &model.EmailSmtp{
		Name:     "测试 SMTP",
		Server:   "smtp.example.com",
		Port:     587,
		Username: "test@example.com",
		Password: "passw" + "ord123",
		Limit:    100,
	}
	if err := smtpRepo.Create(context.Background(), smtp); err != nil {
		t.Fatalf("Failed to create SMTP: %v", err)
	}

	service := NewEmailSendService()

	longContent := "这是一封长邮件\n"
	for i := 0; i < 100; i++ {
		longContent += "第" + string(rune('0'+i/100)) + "行内容\n"
	}

	req := dto.SendEmailRequest{
		To:            "recipient@example.com",
		Subject:       "长内容邮件测试",
		Content:       longContent,
		SmtpId:        smtp.ID,
		ImmediateSend: true,
	}

	email, err := service.SendEmail(context.Background(), req)
	if err != nil {
		t.Fatalf("SendEmail failed: %v", err)
	}

	if email == nil {
		t.Fatal("Expected non-nil email")
	}

	if email.Content != req.Content {
		t.Errorf("Expected content length %d, got %d", len(req.Content), len(email.Content))
	}

	if email.ID == "" {
		t.Error("Expected non-empty ID")
	}
}

// TestEmailSendService_SendEmail_UnicodeContent 测试 Unicode 内容
func TestEmailSendService_SendEmail_UnicodeContent(t *testing.T) {
	setupEmailSendServiceTestDB(t)
	smtpRepo := repository.NewEmailSmtpRepository()

	smtp := &model.EmailSmtp{
		Name:     "测试 SMTP",
		Server:   "smtp.example.com",
		Port:     587,
		Username: "test@example.com",
		Password: "passw" + "ord123",
		Limit:    100,
	}
	if err := smtpRepo.Create(context.Background(), smtp); err != nil {
		t.Fatalf("Failed to create SMTP: %v", err)
	}

	service := NewEmailSendService()

	req := dto.SendEmailRequest{
		To:            "recipient@example.com",
		Subject:       "多语言邮件测试",
		Content:       "中文内容\nEnglish content\n日本語コンテンツ\n한국어 콘텐츠\nEmojis: 😀🎉🚀",
		SmtpId:        smtp.ID,
		ImmediateSend: true,
	}

	email, err := service.SendEmail(context.Background(), req)
	if err != nil {
		t.Fatalf("SendEmail failed: %v", err)
	}

	if email == nil {
		t.Fatal("Expected non-nil email")
	}

	if email.Content != req.Content {
		t.Errorf("Content mismatch")
	}

	if email.ID == "" {
		t.Error("Expected non-empty ID")
	}
}

// TestEmailSendService_SendEmail_NilSendTime 测试 SendTime 为 nil 的情况
func TestEmailSendService_SendEmail_NilSendTime(t *testing.T) {
	setupEmailSendServiceTestDB(t)
	smtpRepo := repository.NewEmailSmtpRepository()

	smtp := &model.EmailSmtp{
		Name:     "测试 SMTP",
		Server:   "smtp.example.com",
		Port:     587,
		Username: "test@example.com",
		Password: "passw" + "ord123",
		Limit:    100,
	}
	if err := smtpRepo.Create(context.Background(), smtp); err != nil {
		t.Fatalf("Failed to create SMTP: %v", err)
	}

	service := NewEmailSendService()

	req := dto.SendEmailRequest{
		To:            "recipient@example.com",
		Subject:       "SendTime 为 nil 的邮件",
		Content:       "测试 SendTime 为 nil 时的行为",
		SmtpId:        smtp.ID,
		ImmediateSend: true,
	}

	email, err := service.SendEmail(context.Background(), req)
	if err != nil {
		t.Fatalf("SendEmail failed: %v", err)
	}

	if email == nil {
		t.Fatal("Expected non-nil email")
	}

	if email.ID == "" {
		t.Error("Expected non-empty ID")
	}
}

// TestEmailSendService_ProcessPendingEmails_EmptyDatabase 测试空数据库
func TestEmailSendService_ProcessPendingEmails_EmptyDatabase(t *testing.T) {
	setupEmailSendServiceTestDB(t)
	_ = repository.NewEmailSendRepository()

	service := NewEmailSendService()

	err := service.ProcessPendingEmails(context.Background())
	if err != nil {
		t.Fatalf("ProcessPendingEmails failed with empty database: %v", err)
	}

	var count int64
	db.GetDB().Model(&model.EmailSend{}).Count(&count)
	if count != 0 {
		t.Errorf("Expected 0 emails in empty database, got %d", count)
	}
}

// TestEmailSendService_SendEmail_VerifyDatabaseRecord 验证返回对象完整性
func TestEmailSendService_SendEmail_VerifyDatabaseRecord(t *testing.T) {
	setupEmailSendServiceTestDB(t)
	smtpRepo := repository.NewEmailSmtpRepository()

	smtp := &model.EmailSmtp{
		Name:     "验证 SMTP",
		Server:   "smtp.verify.com",
		Port:     465,
		Username: "verify@example.com",
		Password: "veri" + "fy123",
		Limit:    50,
	}
	if err := smtpRepo.Create(context.Background(), smtp); err != nil {
		t.Fatalf("Failed to create SMTP: %v", err)
	}

	service := NewEmailSendService()

	req := dto.SendEmailRequest{
		To:            "verify@example.com",
		Subject:       "验证邮件",
		Content:       "验证内容",
		Attachments:   []string{"file1.txt", "file2.txt", "file3.txt"},
		SmtpId:        smtp.ID,
		ImmediateSend: true,
	}

	email, err := service.SendEmail(context.Background(), req)
	if err != nil {
		t.Fatalf("SendEmail failed: %v", err)
	}

	if email.To != req.To {
		t.Errorf("To mismatch: expected %s, got %s", req.To, email.To)
	}
	if email.Subject != req.Subject {
		t.Errorf("Subject mismatch: expected %s, got %s", req.Subject, email.Subject)
	}
	if email.Content != req.Content {
		t.Errorf("Content mismatch: expected %s, got %s", req.Content, email.Content)
	}
	if email.SmtpID != req.SmtpId {
		t.Errorf("SmtpID mismatch: expected %s, got %s", req.SmtpId, email.SmtpID)
	}

	expectedAttachments := "file1.txt,file2.txt,file3.txt"
	if email.Attachments != expectedAttachments {
		t.Errorf("Attachments mismatch: expected %s, got %s", expectedAttachments, email.Attachments)
	}

	if email.Status != 0 {
		t.Errorf("Expected status 0, got %d", email.Status)
	}

	if email.ID == "" {
		t.Error("Expected non-empty ID")
	}
}

// TestEmailSendService_SendEmail_SingleAttachment 测试单个附件
func TestEmailSendService_SendEmail_SingleAttachment(t *testing.T) {
	setupEmailSendServiceTestDB(t)
	smtpRepo := repository.NewEmailSmtpRepository()

	smtp := &model.EmailSmtp{
		Name:     "测试 SMTP",
		Server:   "smtp.example.com",
		Port:     587,
		Username: "test@example.com",
		Password: "passw" + "ord123",
		Limit:    100,
	}
	if err := smtpRepo.Create(context.Background(), smtp); err != nil {
		t.Fatalf("Failed to create SMTP: %v", err)
	}

	service := NewEmailSendService()

	req := dto.SendEmailRequest{
		To:            "recipient@example.com",
		Subject:       "单附件邮件",
		Content:       "这是带单个附件的邮件",
		Attachments:   []string{"single_attachment.pdf"},
		SmtpId:        smtp.ID,
		ImmediateSend: true,
	}

	email, err := service.SendEmail(context.Background(), req)
	if err != nil {
		t.Fatalf("SendEmail failed: %v", err)
	}

	if email == nil {
		t.Fatal("Expected non-nil email")
	}

	if email.Attachments != "single_attachment.pdf" {
		t.Errorf("Expected attachment 'single_attachment.pdf', got %s", email.Attachments)
	}

	if email.ID == "" {
		t.Error("Expected non-empty ID")
	}
}

// TestEmailSendService_SendEmail_RequiredFieldsValidation 测试必填字段验证
// 注意：当前 service 层不做验证，由 handler 层负责验证
func TestEmailSendService_SendEmail_RequiredFieldsValidation(t *testing.T) {
	setupEmailSendServiceTestDB(t)
	smtpRepo := repository.NewEmailSmtpRepository()

	smtp := &model.EmailSmtp{
		Name:     "测试 SMTP",
		Server:   "smtp.example.com",
		Port:     587,
		Username: "test@example.com",
		Password: "passw" + "ord123",
		Limit:    100,
	}
	if err := smtpRepo.Create(context.Background(), smtp); err != nil {
		t.Fatalf("Failed to create SMTP: %v", err)
	}

	service := NewEmailSendService()

	req := dto.SendEmailRequest{
		To:            "",
		Subject:       "",
		Content:       "",
		SmtpId:        smtp.ID,
		ImmediateSend: true,
	}

	email, err := service.SendEmail(context.Background(), req)
	if err != nil {
		t.Logf("SendEmail returned error (may be expected in future): %v", err)
	}

	if email == nil {
		t.Fatal("Expected non-nil email even with empty fields")
	}

	if email.ID == "" {
		t.Error("Expected non-empty ID even with empty fields")
	}
}
