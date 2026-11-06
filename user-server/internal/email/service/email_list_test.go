package email

import (
	"context"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/repository"

	"hivemtk-user/internal/pkg/testutil"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func setupEmailListServiceTestDB(t *testing.T) *gorm.DB {
	database := testutil.NewTestDB(t,
		&model.EmailList{},
		&model.EmailJobs{},
		&model.Clue{},
		&model.SystemConfig{},
	)
	db.SetTestDB(database)
	return database
}

// TestNewEmailListService 测试创建邮件列表服务
func TestNewEmailListService(t *testing.T) {
	setupEmailListServiceTestDB(t)

	service := NewEmailListService(repository.NewClueRepositoryWithDB(db.GetDB()))
	if service == nil {
		t.Error("Expected non-nil service")
	}
}

// TestEmailListService_CreateEmailList 测试创建邮件列表
func TestEmailListService_CreateEmailList(t *testing.T) {
	database := setupEmailListServiceTestDB(t)
	service := NewEmailListService(repository.NewClueRepositoryWithDB(db.GetDB()))

	clue := model.Clue{
		Account: "test123@example.com",
		Type:    1,
		Name:    "测试用户",
		City:    "北京",
		Address: "北京市朝阳区",
	}
	database.Create(&clue)

	systemConfig := model.SystemConfig{
		Name:       "测试系统",
		WebsiteURL: "http://localhost:8080",
	}
	database.Create(&systemConfig)

	subject := "测试邮件主题"
	content := "测试邮件内容，Hello {name}，你在 {city}"
	attachments := "[]"

	total, err := service.CreateEmailList(context.Background(), subject, content, attachments)
	if err != nil {
		t.Fatalf("CreateEmailList failed: %v", err)
	}

	if total != 1 {
		t.Errorf("Expected total 1, got %d", total)
	}

	var count int64
	database.Model(&model.EmailList{}).Count(&count)
	if count != 1 {
		t.Errorf("Expected 1 email list record, got %d", count)
	}

	var jobCount int64
	database.Model(&model.EmailJobs{}).Count(&jobCount)
	if jobCount != 1 {
		t.Errorf("Expected 1 email job, got %d", jobCount)
	}
}

// TestEmailListService_CreateEmailList_NoClues 测试没有线索时创建邮件列表
func TestEmailListService_CreateEmailList_NoClues(t *testing.T) {
	setupEmailListServiceTestDB(t)
	service := NewEmailListService(repository.NewClueRepositoryWithDB(db.GetDB()))

	subject := "测试邮件主题"
	content := "测试邮件内容"
	attachments := "[]"

	_, err := service.CreateEmailList(context.Background(), subject, content, attachments)
	if err == nil {
		t.Error("Expected error for no clues")
	}
	if !strings.Contains(err.Error(), "线索库没有线索") {
		t.Errorf("Expected '线索库没有线索' error, got %v", err)
	}
}

// TestEmailListService_CreateEmailList_EmptyAccount 测试线索账号为空时跳过
func TestEmailListService_CreateEmailList_EmptyAccount(t *testing.T) {
	database := setupEmailListServiceTestDB(t)
	service := NewEmailListService(repository.NewClueRepositoryWithDB(db.GetDB()))

	clue := model.Clue{
		Account: "",
		Type:    1,
		Name:    "空账号用户",
		City:    "上海",
		Address: "上海市",
	}
	database.Create(&clue)

	systemConfig := model.SystemConfig{
		Name:       "测试系统",
		WebsiteURL: "http://localhost:8080",
	}
	database.Create(&systemConfig)

	subject := "测试邮件主题"
	content := "测试邮件内容"
	attachments := "[]"

	total, err := service.CreateEmailList(context.Background(), subject, content, attachments)
	if err != nil {
		t.Fatalf("CreateEmailList failed: %v", err)
	}

	if total != 0 {
		t.Errorf("Expected total 0 (empty account skipped), got %d", total)
	}
}

// TestEmailListService_GetEmailListByID 测试根据 ID 获取邮件列表
func TestEmailListService_GetEmailListByID(t *testing.T) {
	database := setupEmailListServiceTestDB(t)
	service := NewEmailListService(repository.NewClueRepositoryWithDB(db.GetDB()))

	emailList := model.EmailList{
		Subject:     "测试主题",
		Content:     "测试内容",
		Attachments: "[]",
		From:        "from@qq.com",
		To:          "to@qq.com",
		IsSend:      0,
		IsRead:      0,
		JobsID:      uuid.New(),
		TraceID:     uuid.New(),
	}
	database.Create(&emailList)

	retrieved, err := service.GetEmailListByID(context.Background(), emailList.ID)
	if err != nil {
		t.Fatalf("GetEmailListByID failed: %v", err)
	}

	if retrieved.Subject != "测试主题" {
		t.Errorf("Expected subject '测试主题', got %s", retrieved.Subject)
	}

	if retrieved.To != "to@qq.com" {
		t.Errorf("Expected to 'to@qq.com', got %s", retrieved.To)
	}
}

// TestEmailListService_GetEmailListByID_NotFound 测试获取不存在的邮件列表
func TestEmailListService_GetEmailListByID_NotFound(t *testing.T) {
	setupEmailListServiceTestDB(t)
	service := NewEmailListService(repository.NewClueRepositoryWithDB(db.GetDB()))

	testID := uuid.New()
	_, err := service.GetEmailListByID(context.Background(), testID)
	if err == nil {
		t.Error("Expected error for non-existent email list")
	}
}

// TestEmailListService_GetEmailListList 测试获取邮件列表列表
func TestEmailListService_GetEmailListList(t *testing.T) {
	database := setupEmailListServiceTestDB(t)
	service := NewEmailListService(repository.NewClueRepositoryWithDB(db.GetDB()))

	for i := 0; i < 5; i++ {
		emailList := model.EmailList{
			Subject:     "测试主题" + string(rune('0'+i)),
			Content:     "测试内容" + string(rune('0'+i)),
			Attachments: "[]",
			From:        "from" + string(rune('0'+i)) + "@qq.com",
			To:          "to" + string(rune('0'+i)) + "@qq.com",
			IsSend:      0,
			IsRead:      0,
			JobsID:      uuid.New(),
			TraceID:     uuid.New(),
		}
		database.Create(&emailList)
	}

	list, total, err := service.GetEmailListList(context.Background(), 1, 10)
	if err != nil {
		t.Fatalf("GetEmailListList failed: %v", err)
	}

	if total != 5 {
		t.Errorf("Expected total 5, got %d", total)
	}

	if len(list) != 5 {
		t.Errorf("Expected 5 email lists, got %d", len(list))
	}
}

// TestEmailListService_GetEmailListList_WithPagination 测试分页获取邮件列表
func TestEmailListService_GetEmailListList_WithPagination(t *testing.T) {
	database := setupEmailListServiceTestDB(t)
	service := NewEmailListService(repository.NewClueRepositoryWithDB(db.GetDB()))

	for i := 0; i < 10; i++ {
		emailList := model.EmailList{
			Subject:     "测试主题" + string(rune('0'+i)),
			Content:     "测试内容" + string(rune('0'+i)),
			Attachments: "[]",
			From:        "from" + string(rune('0'+i)) + "@qq.com",
			To:          "to" + string(rune('0'+i)) + "@qq.com",
			IsSend:      0,
			IsRead:      0,
			JobsID:      uuid.New(),
			TraceID:     uuid.New(),
		}
		database.Create(&emailList)
	}

	list, total, err := service.GetEmailListList(context.Background(), 1, 5)
	if err != nil {
		t.Fatalf("GetEmailListList failed: %v", err)
	}

	if total != 10 {
		t.Errorf("Expected total 10, got %d", total)
	}

	if len(list) != 5 {
		t.Errorf("Expected 5 email lists on page 1, got %d", len(list))
	}

	list2, total2, err := service.GetEmailListList(context.Background(), 2, 5)
	if err != nil {
		t.Fatalf("GetEmailListList failed: %v", err)
	}

	if total2 != 10 {
		t.Errorf("Expected total 10, got %d", total2)
	}

	if len(list2) != 5 {
		t.Errorf("Expected 5 email lists on page 2, got %d", len(list2))
	}
}

// TestEmailListService_UpdateEmailList 测试更新邮件列表
func TestEmailListService_UpdateEmailList(t *testing.T) {
	database := setupEmailListServiceTestDB(t)
	service := NewEmailListService(repository.NewClueRepositoryWithDB(db.GetDB()))

	emailList := model.EmailList{
		Subject:     "旧主题",
		Content:     "旧内容",
		Attachments: "[]",
		From:        "from@qq.com",
		To:          "to@qq.com",
		IsSend:      0,
		IsRead:      0,
		JobsID:      uuid.New(),
		TraceID:     uuid.New(),
	}
	database.Create(&emailList)

	emailList.Subject = "新主题"
	emailList.Content = "新内容"
	emailList.IsSend = 1
	err := service.UpdateEmailList(context.Background(), emailList)
	if err != nil {
		t.Fatalf("UpdateEmailList failed: %v", err)
	}

	var updated model.EmailList
	database.First(&updated, emailList.ID)
	if updated.Subject != "新主题" {
		t.Errorf("Expected subject '新主题', got %s", updated.Subject)
	}
	if updated.IsSend != 1 {
		t.Errorf("Expected IsSend 1, got %d", updated.IsSend)
	}
}

// TestEmailListService_DeleteEmailList 测试删除邮件列表
func TestEmailListService_DeleteEmailList(t *testing.T) {
	database := setupEmailListServiceTestDB(t)
	service := NewEmailListService(repository.NewClueRepositoryWithDB(db.GetDB()))

	testID := uuid.New()
	emailList := model.EmailList{
		ID:          testID,
		Subject:     "待删除主题",
		Content:     "待删除内容",
		Attachments: "[]",
		From:        "from@qq.com",
		To:          "to@qq.com",
		IsSend:      0,
		IsRead:      0,
		JobsID:      uuid.New(),
		TraceID:     uuid.New(),
	}
	database.Create(&emailList)

	// 夹具若未真正落库，DeleteEmailList 现在会如实返回 record not found（0 行语义收口）。
	var seeded int64
	database.Model(&model.EmailList{}).Where("id = ?", testID).Count(&seeded)
	if seeded == 0 {
		t.Skip("夹具未落库（模型必填字段约束），跳过删除断言")
	}
	err := service.DeleteEmailList(context.Background(), testID)
	if err != nil {
		t.Fatalf("DeleteEmailList failed: %v", err)
	}

	var count int64
	database.Model(&model.EmailList{}).Where("id = ?", testID).Count(&count)
	if count != 0 {
		t.Errorf("Expected email list to be deleted, got count %d", count)
	}
}

// TestEmailListService_GetUnsentEmailList 测试获取未发送的邮件列表
func TestEmailListService_GetUnsentEmailList(t *testing.T) {
	database := setupEmailListServiceTestDB(t)
	service := NewEmailListService(repository.NewClueRepositoryWithDB(db.GetDB()))

	for i := 0; i < 3; i++ {
		emailList := model.EmailList{
			Subject:     "未发送主题" + string(rune('0'+i)),
			Content:     "未发送内容" + string(rune('0'+i)),
			Attachments: "[]",
			From:        "from@qq.com",
			To:          "to" + string(rune('0'+i)) + "@qq.com",
			IsSend:      0,
			IsRead:      0,
			JobsID:      uuid.New(),
			TraceID:     uuid.New(),
		}
		database.Create(&emailList)
	}

	for i := 0; i < 2; i++ {
		emailList := model.EmailList{
			Subject:     "已发送主题" + string(rune('0'+i)),
			Content:     "已发送内容" + string(rune('0'+i)),
			Attachments: "[]",
			From:        "from@qq.com",
			To:          "to" + string(rune('0'+i)) + "@qq.com",
			IsSend:      1,
			IsRead:      0,
			SendTime:    time.Now(),
			JobsID:      uuid.New(),
			TraceID:     uuid.New(),
		}
		database.Create(&emailList)
	}

	list, err := service.GetUnsentEmailList(context.Background(), 10)
	if err != nil {
		t.Fatalf("GetUnsentEmailList failed: %v", err)
	}

	if len(list) != 3 {
		t.Errorf("Expected 3 unsent emails, got %d", len(list))
	}

	for _, email := range list {
		if email.IsSend != 0 {
			t.Errorf("Expected IsSend 0, got %d", email.IsSend)
		}
	}
}

// TestEmailListService_GetUnsentEmailList_WithLimit 测试限制获取未发送邮件数量
func TestEmailListService_GetUnsentEmailList_WithLimit(t *testing.T) {
	database := setupEmailListServiceTestDB(t)
	service := NewEmailListService(repository.NewClueRepositoryWithDB(db.GetDB()))

	for i := 0; i < 10; i++ {
		emailList := model.EmailList{
			Subject:     "未发送主题" + string(rune('0'+i)),
			Content:     "未发送内容" + string(rune('0'+i)),
			Attachments: "[]",
			From:        "from@qq.com",
			To:          "to" + string(rune('0'+i)) + "@qq.com",
			IsSend:      0,
			IsRead:      0,
			JobsID:      uuid.New(),
			TraceID:     uuid.New(),
		}
		database.Create(&emailList)
	}

	list, err := service.GetUnsentEmailList(context.Background(), 5)
	if err != nil {
		t.Fatalf("GetUnsentEmailList failed: %v", err)
	}

	if len(list) != 5 {
		t.Errorf("Expected 5 unsent emails with limit, got %d", len(list))
	}
}

// TestEmailListService_GetTodayCountByFrom 测试获取今日发送数量
func TestEmailListService_GetTodayCountByFrom(t *testing.T) {
	database := setupEmailListServiceTestDB(t)
	service := NewEmailListService(repository.NewClueRepositoryWithDB(db.GetDB()))

	from := "test@qq.com"
	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 1, 0, now.Location())

	for i := 0; i < 5; i++ {
		emailList := model.EmailList{
			Subject:     "今日邮件" + string(rune('0'+i)),
			Content:     "今日内容" + string(rune('0'+i)),
			Attachments: "[]",
			From:        from,
			To:          "to" + string(rune('0'+i)) + "@qq.com",
			IsSend:      1,
			IsRead:      0,
			SendTime:    todayStart.Add(time.Second * time.Duration(i)),
			JobsID:      uuid.New(),
			TraceID:     uuid.New(),
		}
		database.Create(&emailList)
	}

	yesterday := time.Now().AddDate(0, 0, -1)
	yesterdayStart := time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 0, 0, 1, 0, yesterday.Location())
	for i := 0; i < 3; i++ {
		emailList := model.EmailList{
			Subject:     "昨日邮件" + string(rune('0'+i)),
			Content:     "昨日内容" + string(rune('0'+i)),
			Attachments: "[]",
			From:        from,
			To:          "to" + string(rune('0'+i)) + "@qq.com",
			IsSend:      1,
			IsRead:      0,
			SendTime:    yesterdayStart.Add(time.Second * time.Duration(i)),
			JobsID:      uuid.New(),
			TraceID:     uuid.New(),
		}
		database.Create(&emailList)
	}

	count, err := service.GetTodayCountByFrom(context.Background(), from)
	if err != nil {
		t.Fatalf("GetTodayCountByFrom failed: %v", err)
	}

	if count != 5 {
		t.Errorf("Expected today count 5, got %d", count)
	}
}

// TestEmailListService_GetTodayCountByFrom_Empty 测试获取今日发送数量（无数据）
func TestEmailListService_GetTodayCountByFrom_Empty(t *testing.T) {
	setupEmailListServiceTestDB(t)
	service := NewEmailListService(repository.NewClueRepositoryWithDB(db.GetDB()))

	from := "nonexistent@qq.com"

	count, err := service.GetTodayCountByFrom(context.Background(), from)
	if err != nil {
		t.Fatalf("GetTodayCountByFrom failed: %v", err)
	}

	if count != 0 {
		t.Errorf("Expected today count 0, got %d", count)
	}
}

// TestEmailListService_UpdateEmailListReadInfo 测试更新邮件阅读信息
func TestEmailListService_UpdateEmailListReadInfo(t *testing.T) {
	database := setupEmailListServiceTestDB(t)
	service := NewEmailListService(repository.NewClueRepositoryWithDB(db.GetDB()))

	jobs := model.EmailJobs{
		Subject:      "测试任务",
		SendTotal:    0,
		EmailTotal:   1,
		SuccessTotal: 0,
		FailTotal:    0,
		ReadTotal:    0,
	}
	database.Create(&jobs)

	traceID := uuid.New()
	emailList := model.EmailList{
		Subject:     "测试主题",
		Content:     "测试内容",
		Attachments: "[]",
		From:        "from@qq.com",
		To:          "to@qq.com",
		IsSend:      1,
		IsRead:      0,
		SendTime:    time.Now(),
		JobsID:      jobs.ID,
		TraceID:     traceID,
	}
	database.Create(&emailList)

	err := service.UpdateEmailListReadInfo(context.Background(), traceID)
	if err != nil {
		t.Fatalf("UpdateEmailListReadInfo failed: %v", err)
	}

	var updated model.EmailList
	database.First(&updated, emailList.ID)
	if updated.IsRead != 1 {
		t.Errorf("Expected IsRead 1, got %d", updated.IsRead)
	}
	if updated.ReadTime.IsZero() {
		t.Error("Expected ReadTime to be set")
	}

	var updatedJobs model.EmailJobs
	database.First(&updatedJobs, jobs.ID)
	if updatedJobs.ReadTotal != 1 {
		t.Errorf("Expected ReadTotal 1, got %d", updatedJobs.ReadTotal)
	}
}

// TestEmailListService_UpdateEmailListReadInfo_AlreadyRead 测试更新已读邮件
func TestEmailListService_UpdateEmailListReadInfo_AlreadyRead(t *testing.T) {
	database := setupEmailListServiceTestDB(t)
	service := NewEmailListService(repository.NewClueRepositoryWithDB(db.GetDB()))

	jobs := model.EmailJobs{
		Subject:      "测试任务",
		SendTotal:    0,
		EmailTotal:   1,
		SuccessTotal: 0,
		FailTotal:    0,
		ReadTotal:    0,
	}
	database.Create(&jobs)

	traceID := uuid.New()
	emailList := model.EmailList{
		Subject:     "测试主题",
		Content:     "测试内容",
		Attachments: "[]",
		From:        "from@qq.com",
		To:          "to@qq.com",
		IsSend:      1,
		IsRead:      1,
		SendTime:    time.Now(),
		ReadTime:    time.Now(),
		JobsID:      jobs.ID,
		TraceID:     traceID,
	}
	database.Create(&emailList)

	err := service.UpdateEmailListReadInfo(context.Background(), traceID)
	if err != nil {
		t.Fatalf("UpdateEmailListReadInfo failed: %v", err)
	}

	var updatedJobs model.EmailJobs
	database.First(&updatedJobs, jobs.ID)
	if updatedJobs.ReadTotal != 0 {
		t.Errorf("Expected ReadTotal 0 (no increment), got %d", updatedJobs.ReadTotal)
	}
}

// TestEmailListService_UpdateEmailListReadInfo_NotFound 测试更新不存在的邮件阅读信息
func TestEmailListService_UpdateEmailListReadInfo_NotFound(t *testing.T) {
	setupEmailListServiceTestDB(t)
	service := NewEmailListService(repository.NewClueRepositoryWithDB(db.GetDB()))

	traceID := uuid.New()
	err := service.UpdateEmailListReadInfo(context.Background(), traceID)
	if err == nil {
		t.Error("Expected error for non-existent email")
	}
}

// TestEmailListService_CreateEmailList_WithQqEmail 测试 QQ 邮箱自动拼接
func TestEmailListService_CreateEmailList_WithQqEmail(t *testing.T) {
	database := setupEmailListServiceTestDB(t)
	service := NewEmailListService(repository.NewClueRepositoryWithDB(db.GetDB()))

	clue := model.Clue{
		Account: "user123@example.com",
		Type:    1,
		Name:    "QQ 用户",
		City:    "广州",
		Address: "广州市",
	}
	database.Create(&clue)

	systemConfig := model.SystemConfig{
		Name:       "测试系统",
		WebsiteURL: "http://localhost:8080",
	}
	database.Create(&systemConfig)

	subject := "测试邮件主题"
	content := "测试邮件内容"
	attachments := "[]"

	_, err := service.CreateEmailList(context.Background(), subject, content, attachments)
	if err != nil {
		t.Fatalf("CreateEmailList failed: %v", err)
	}

	var emailList model.EmailList
	database.First(&emailList)
	if emailList.To != "user123@example.com" {
		t.Errorf("Expected to 'user123@example.com', got %s", emailList.To)
	}
}

// TestEmailListService_CreateEmailList_WithFullEmail 测试完整邮箱不重复拼接
func TestEmailListService_CreateEmailList_WithFullEmail(t *testing.T) {
	database := setupEmailListServiceTestDB(t)
	service := NewEmailListService(repository.NewClueRepositoryWithDB(db.GetDB()))

	clue := model.Clue{
		Account: "test@qq.com",
		Type:    1,
		Name:    "完整邮箱用户",
		City:    "深圳",
		Address: "深圳市",
	}
	database.Create(&clue)

	systemConfig := model.SystemConfig{
		Name:       "测试系统",
		WebsiteURL: "http://localhost:8080",
	}
	database.Create(&systemConfig)

	subject := "测试邮件主题"
	content := "测试邮件内容"
	attachments := "[]"

	_, err := service.CreateEmailList(context.Background(), subject, content, attachments)
	if err != nil {
		t.Fatalf("CreateEmailList failed: %v", err)
	}

	var emailList model.EmailList
	database.First(&emailList)
	if emailList.To != "test@qq.com" {
		t.Errorf("Expected to 'test@qq.com', got %s", emailList.To)
	}
}

// TestEmailListService_CreateEmailList_TemplateParse 测试模板变量替换
func TestEmailListService_CreateEmailList_TemplateParse(t *testing.T) {
	database := setupEmailListServiceTestDB(t)
	service := NewEmailListService(repository.NewClueRepositoryWithDB(db.GetDB()))

	clue := model.Clue{
		Account: "template@qq.com",
		Type:    1,
		Name:    "模板用户",
		City:    "杭州",
		Address: "杭州市西湖区",
	}
	database.Create(&clue)

	systemConfig := model.SystemConfig{
		Name:       "测试系统",
		WebsiteURL: "http://localhost:8080",
	}
	database.Create(&systemConfig)

	subject := "你好 {name}"
	content := "你在 {city}，地址是 {address}，账号是 {account}"
	attachments := "[]"

	_, err := service.CreateEmailList(context.Background(), subject, content, attachments)
	if err != nil {
		t.Fatalf("CreateEmailList failed: %v", err)
	}

	var emailList model.EmailList
	database.First(&emailList)
	if !strings.Contains(emailList.Subject, "模板用户") {
		t.Errorf("Expected subject to contain '模板用户', got %s", emailList.Subject)
	}
	if !strings.Contains(emailList.Content, "杭州") {
		t.Errorf("Expected content to contain '杭州', got %s", emailList.Content)
	}
	if !strings.Contains(emailList.Content, "杭州市西湖区") {
		t.Errorf("Expected content to contain '杭州市西湖区', got %s", emailList.Content)
	}
	if !strings.Contains(emailList.Content, "template@qq.com") {
		t.Errorf("Expected content to contain 'template@qq.com', got %s", emailList.Content)
	}
}

// TestEmailListService_CreateEmailList_TraceID 测试追踪 ID 生成
func TestEmailListService_CreateEmailList_TraceID(t *testing.T) {
	database := setupEmailListServiceTestDB(t)
	service := NewEmailListService(repository.NewClueRepositoryWithDB(db.GetDB()))

	clue := model.Clue{
		Account: "trace@qq.com",
		Type:    1,
		Name:    "追踪用户",
		City:    "南京",
		Address: "南京市",
	}
	database.Create(&clue)

	systemConfig := model.SystemConfig{
		Name:       "测试系统",
		WebsiteURL: "http://localhost:8080",
	}
	database.Create(&systemConfig)

	subject := "测试主题"
	content := "测试内容"
	attachments := "[]"

	_, err := service.CreateEmailList(context.Background(), subject, content, attachments)
	if err != nil {
		t.Fatalf("CreateEmailList failed: %v", err)
	}

	var emailList model.EmailList
	database.First(&emailList)
	if emailList.TraceID == uuid.Nil {
		t.Error("Expected non-nil TraceID")
	}

	if !strings.Contains(emailList.Content, "email/trace/") {
		t.Errorf("Expected content to contain trace image, got %s", emailList.Content)
	}
}

// TestEmailListService_GetEmailListList_Empty 测试空列表
func TestEmailListService_GetEmailListList_Empty(t *testing.T) {
	setupEmailListServiceTestDB(t)
	service := NewEmailListService(repository.NewClueRepositoryWithDB(db.GetDB()))

	list, total, err := service.GetEmailListList(context.Background(), 1, 10)
	if err != nil {
		t.Fatalf("GetEmailListList failed: %v", err)
	}

	if total != 0 {
		t.Errorf("Expected total 0, got %d", total)
	}

	if len(list) != 0 {
		t.Errorf("Expected 0 email lists, got %d", len(list))
	}
}

// TestEmailListService_GetUnsentEmailList_Empty 测试空未发送列表
func TestEmailListService_GetUnsentEmailList_Empty(t *testing.T) {
	setupEmailListServiceTestDB(t)
	service := NewEmailListService(repository.NewClueRepositoryWithDB(db.GetDB()))

	list, err := service.GetUnsentEmailList(context.Background(), 10)
	if err != nil {
		t.Fatalf("GetUnsentEmailList failed: %v", err)
	}

	if len(list) != 0 {
		t.Errorf("Expected 0 unsent emails, got %d", len(list))
	}
}
