package service

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// BackupService 备份服务
type BackupService struct {
	backupRepo     *repository.BackupRepository
	backupDataRepo repository.BackupDataRepository
}

func validateBackupName(name string) error {
	if name == "" {
		return errors.New("backup_name 不能为空")
	}
	if len(name) > 128 {
		return errors.New("backup_name 长度不能超过 128")
	}
	for _, c := range name {
		isSafe := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '_' || c == '-'
		if !isSafe {
			return fmt.Errorf("backup_name 包含非法字符 %q（仅允许 [A-Za-z0-9_-]）", c)
		}
	}
	return nil
}

// NewBackupService 创建备份服务实例
func NewBackupService() *BackupService {
	return &BackupService{
		backupRepo:     repository.NewBackupRepository(),
		backupDataRepo: repository.NewBackupDataRepository(),
	}
}

// CreateBackupRequest 创建备份请求
type CreateBackupRequest struct {
	BackupName string           `json:"backup_name" binding:"required"`
	BackupType model.BackupType `json:"backup_type"`
}

// CreateBackup 创建备份
func (s *BackupService) CreateBackup(ctx context.Context, createdBy uint, req *CreateBackupRequest) (*model.Backup, error) {
	backup := &model.Backup{
		BackupName: req.BackupName,
		BackupType: req.BackupType,
		Status:     model.BackupStatusPending,
		CreatedBy:  createdBy,
		StartedAt:  time.Now(),
	}

	if backup.BackupType == "" {
		backup.BackupType = model.BackupTypeFull
	}
	if backup.BackupName == "" {
		backup.BackupName = fmt.Sprintf("backup_%s", time.Now().Format("20060102_150405"))
	}

	if err := validateBackupName(backup.BackupName); err != nil {
		return nil, err
	}

	if err := s.backupRepo.Create(ctx, backup); err != nil {
		return nil, err
	}

	go func(b *model.Backup) {
		s.executeBackup(context.WithoutCancel(ctx), b)
	}(backup)

	return backup, nil
}

// CreateBackupSimple 创建备份（字符串类型入参，供 controller 调用，
// 避免 controller 直接引用 model.BackupType）。
func (s *BackupService) CreateBackupSimple(ctx context.Context, createdBy uint, backupName, backupType string) (*model.Backup, error) {
	return s.CreateBackup(ctx, createdBy, &CreateBackupRequest{
		BackupName: backupName,
		BackupType: ParseBackupType(backupType),
	})
}

// ParseBackupType 把 string 转成 model.BackupType（未知值默认全量备份）
func ParseBackupType(s string) model.BackupType {
	switch s {
	case string(model.BackupTypeFull):
		return model.BackupTypeFull
	case string(model.BackupTypeIncremental):
		return model.BackupTypeIncremental
	case "data", "config":
		return model.BackupTypeFull
	}
	return model.BackupTypeFull
}

func (s *BackupService) executeBackup(ctx context.Context, backup *model.Backup) {
	defer func() {
		if r := recover(); r != nil {
			if backup == nil {
				return
			}
			backup.Status = model.BackupStatusFailed
			backup.ErrorMessage = fmt.Sprintf("backup panic: %v", r)
			if s.backupRepo != nil {
				if err := s.backupRepo.Update(ctx, backup); err != nil {
					logger.Errorf("[backup] update status after panic failed: %v", err)
				}
			}
		}
	}()

	backup.Status = model.BackupStatusRunning
	if err := s.backupRepo.Update(ctx, backup); err != nil {
		logger.Errorf("[backup] update backup status failed: %v", err)
	}

	backupDir := filepath.Join("backups", backup.BackupName)
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		backup.Status = model.BackupStatusFailed
		backup.ErrorMessage = err.Error()
		if err := s.backupRepo.Update(ctx, backup); err != nil {
			logger.Errorf("[backup] update backup status failed: %v", err)
		}
		return
	}

	backupFile := filepath.Join(backupDir, fmt.Sprintf("%s.zip", backup.BackupName))
	backup.FilePath = backupFile

	if err := s.backupDatabase(ctx, backupDir); err != nil {
		backup.Status = model.BackupStatusFailed
		backup.ErrorMessage = "数据库备份失败：" + err.Error()
		if err := s.backupRepo.Update(ctx, backup); err != nil {
			logger.Errorf("[backup] update backup status failed: %v", err)
		}
		return
	}

	if err := s.compressBackup(ctx, backupDir, backupFile); err != nil {
		backup.Status = model.BackupStatusFailed
		backup.ErrorMessage = "压缩备份失败：" + err.Error()
		if err := s.backupRepo.Update(ctx, backup); err != nil {
			logger.Errorf("[backup] update backup status failed: %v", err)
		}
		return
	}

	if info, err := os.Stat(backupFile); err == nil {
		backup.FileSize = info.Size()
	}

	backup.Status = model.BackupStatusCompleted
	now := time.Now()
	backup.CompletedAt = &now
	if err := s.backupRepo.Update(ctx, backup); err != nil {
		logger.Errorf("[backup] update backup status failed: %v", err)
	}
}

func (s *BackupService) backupDatabase(ctx context.Context, dir string) error {
	data, err := s.exportData(ctx)
	if err != nil {
		return err
	}

	sqlFile := filepath.Join(dir, "data.json")
	return os.WriteFile(sqlFile, data, 0600)
}

func (s *BackupService) exportData(ctx context.Context) ([]byte, error) {
	if s.backupDataRepo == nil {
		return nil, fmt.Errorf("backupDataRepo is nil")
	}

	now := time.Now()
	cutoff := now.Add(-24 * time.Hour).Unix()

	clues, err := s.backupDataRepo.DumpClues(ctx, cutoff)
	if err != nil {
		return nil, err
	}

	users, err := s.dumpAllUsers(ctx)
	if err != nil {
		return nil, err
	}

	links, err := s.dumpAllShortLinks(ctx)
	if err != nil {
		links = []byte("[]")
	}

	clueCount := jsonArrayLen(clues)
	userCount := jsonArrayLen(users)
	linkCount := jsonArrayLen(links)

	data := map[string]any{
		"backup_time": now.Format("2006-01-02 15:04:05"),
		"version":     "1.0.0",
		"stats": map[string]int{
			"clues":       clueCount,
			"users":       userCount,
			"short_links": linkCount,
		},
		"clues":      rawMessage(clues),
		"users":      rawMessage(users),
		"short_link": rawMessage(links),
	}
	return json.Marshal(data)
}

func rawMessage(b []byte) json.RawMessage {
	if len(b) == 0 {
		return json.RawMessage("[]")
	}
	return json.RawMessage(b)
}

const backupPageSize = 1000

func (s *BackupService) dumpAllUsers(ctx context.Context) ([]byte, error) {
	var all []json.RawMessage
	for offset := 0; ; offset += backupPageSize {
		batch, err := s.backupDataRepo.DumpUsers(ctx, backupPageSize, offset)
		if err != nil {
			return nil, err
		}
		var arr []json.RawMessage
		if err := json.Unmarshal(batch, &arr); err != nil {
			return nil, fmt.Errorf("unmarshal users batch: %w", err)
		}
		if len(arr) == 0 {
			break
		}
		all = append(all, arr...)
		if len(arr) < backupPageSize {
			break
		}
	}
	if all == nil {
		return []byte("[]"), nil
	}
	return json.Marshal(all)
}

func (s *BackupService) dumpAllShortLinks(ctx context.Context) ([]byte, error) {
	var all []json.RawMessage
	for offset := 0; ; offset += backupPageSize {
		batch, err := s.backupDataRepo.DumpShortLinks(ctx, backupPageSize, offset)
		if err != nil {
			return nil, err
		}
		var arr []json.RawMessage
		if err := json.Unmarshal(batch, &arr); err != nil {
			return nil, fmt.Errorf("unmarshal short_links batch: %w", err)
		}
		if len(arr) == 0 {
			break
		}
		all = append(all, arr...)
		if len(arr) < backupPageSize {
			break
		}
	}
	if all == nil {
		return []byte("[]"), nil
	}
	return json.Marshal(all)
}

func jsonArrayLen(b []byte) int {
	if len(b) < 2 {
		return 0
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(b, &arr); err != nil {
		return 0
	}
	return len(arr)
}

func (s *BackupService) compressBackup(ctx context.Context, dir, output string) error {
	zipFile, err := os.Create(output)
	if err != nil {
		return err
	}
	defer zipFile.Close()

	zipWriter := zip.NewWriter(zipFile)
	defer zipWriter.Close()

	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() || filepath.Ext(path) == ".zip" {
			return nil
		}

		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name, _ = filepath.Rel(dir, path)
		header.Method = zip.Deflate

		writer, err := zipWriter.CreateHeader(header)
		if err != nil {
			return err
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		_, err = io.Copy(writer, file)
		return err
	})
}

// GetBackupList 获取备份列表
func (s *BackupService) GetBackupList(ctx context.Context, page, pageSize int) ([]*model.Backup, int64, error) {
	return s.backupRepo.GetAll(ctx, page, pageSize)
}

// GetBackupByID 获取备份详情
func (s *BackupService) GetBackupByID(ctx context.Context, id uint) (*model.Backup, error) {
	return s.backupRepo.GetByID(ctx, id)
}

// DeleteBackup 删除备份
func (s *BackupService) DeleteBackup(ctx context.Context, id uint) error {
	backup, err := s.backupRepo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	if backup.FilePath != "" {
		os.Remove(backup.FilePath)
	}

	return s.backupRepo.Delete(ctx, id)
}

// RestoreService 恢复服务
type RestoreService struct {
	restoreRepo    *repository.RestoreRecordRepository
	backupRepo     *repository.BackupRepository
	backupDataRepo repository.BackupDataRepository
}

// NewRestoreService 创建恢复服务实例
func NewRestoreService() *RestoreService {
	return &RestoreService{
		restoreRepo:    repository.NewRestoreRecordRepository(),
		backupRepo:     repository.NewBackupRepository(),
		backupDataRepo: repository.NewBackupDataRepository(),
	}
}

// RestoreBackupRequest 恢复备份请求
type RestoreBackupRequest struct {
	BackupID uint `json:"backup_id" binding:"required"`
}

// RestoreBackup 恢复备份
func (s *RestoreService) RestoreBackup(ctx context.Context, createdBy uint, req *RestoreBackupRequest) (*model.RestoreRecord, error) {
	backup, err := s.backupRepo.GetByID(ctx, req.BackupID)
	if err != nil {
		return nil, fmt.Errorf("备份记录不存在")
	}
	if backup.Status != model.BackupStatusCompleted {
		return nil, fmt.Errorf("备份未完成，无法恢复")
	}

	record := &model.RestoreRecord{
		BackupID:   req.BackupID,
		BackupName: backup.BackupName,
		Status:     "pending",
		CreatedBy:  createdBy,
		RestoredAt: time.Now(),
	}

	if err := s.restoreRepo.Create(ctx, record); err != nil {
		return nil, err
	}

	go func(src model.RestoreRecord) {

		s.executeRestore(context.WithoutCancel(ctx), &src, backup)
	}(*record)

	return record, nil
}

func (s *RestoreService) executeRestore(ctx context.Context, record *model.RestoreRecord, backup *model.Backup) {
	record.Status = "running"
	s.restoreRepo.Update(ctx, record)

	if err := s.decompressBackup(ctx, backup.FilePath); err != nil {
		record.Status = "failed"
		record.ErrorMessage = "解压备份失败：" + err.Error()
		s.restoreRepo.Update(ctx, record)
		return
	}

	if err := s.restoreDatabase(ctx, backup); err != nil {
		record.Status = "failed"
		record.ErrorMessage = "数据库恢复失败：" + err.Error()
		s.restoreRepo.Update(ctx, record)
		return
	}

	record.Status = "completed"
	s.restoreRepo.Update(ctx, record)
}

func (s *RestoreService) decompressBackup(ctx context.Context, backupFile string) error {
	r, err := zip.OpenReader(backupFile)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			return err
		}

		if f.FileInfo().IsDir() {
			rc.Close()
			if filepath.IsAbs(f.Name) || strings.Contains(f.Name, "..") {
				return fmt.Errorf("非法的备份条目路径: %s", f.Name)
			}
			os.MkdirAll(f.Name, 0700)
			continue
		}

		if filepath.IsAbs(f.Name) || strings.Contains(f.Name, "..") {
			rc.Close()
			return fmt.Errorf("非法的备份条目路径: %s", f.Name)
		}
		path := filepath.Join("restore_tmp", f.Name)
		os.MkdirAll(filepath.Dir(path), 0700)
		outFile, err := os.Create(path)
		if err != nil {
			rc.Close()
			return err
		}

		// 循环体内禁止 defer（大备份数千条目会堆积数千 FD 触发 EMFILE），逐文件显式关闭
		_, copyErr := io.Copy(outFile, rc)
		closeErr := outFile.Close()
		rc.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func (s *RestoreService) restoreDatabase(ctx context.Context, backup *model.Backup) error {
	jsonFile := filepath.Join("restore_tmp", "data.json")
	data, err := os.ReadFile(jsonFile)
	if err != nil {
		return err
	}

	var backupData struct {
		Version string           `json:"version"`
		Clues   []map[string]any `json:"clues"`
		Users   []map[string]any `json:"users"`

		MFA             []map[string]any `json:"mfa"`
		OBSConfig       []map[string]any `json:"obs_config"`
		EmailAccounts   []map[string]any `json:"email_accounts"`
		EmailJobs       []map[string]any `json:"email_jobs"`
		DNC             []map[string]any `json:"dnc"`
		SystemConfig    []map[string]any `json:"system_config"`
		WebhookSubs     []map[string]any `json:"webhook_subscriptions"`
		PasswordHistory []map[string]any `json:"password_history"`

		CustomerSessions    []map[string]any `json:"customer_sessions"`
		Messages            []map[string]any `json:"messages"`
		CsatScores          []map[string]any `json:"csat_scores"`
		Customers           []map[string]any `json:"customers"`
		AgentStatuses       []map[string]any `json:"agent_statuses"`
		AlertRules          []map[string]any `json:"alert_rules"`
		AutomationRules     []map[string]any `json:"automation_rules"`
		BridgeAccounts      []map[string]any `json:"bridge_accounts"`
		OperationLogs       []map[string]any `json:"operation_logs"`
		LoginEvents         []map[string]any `json:"login_events"`
		SecurityAlerts      []map[string]any `json:"security_alerts"`
		PasswordResetTokens []map[string]any `json:"password_reset_tokens"`
		Prompts             []map[string]any `json:"prompts"`
		AIAgents            []map[string]any `json:"ai_agents"`
		FeatureFlags        []map[string]any `json:"feature_flags"`
	}
	if err := json.Unmarshal(data, &backupData); err != nil {
		return err
	}

	restoredClues := 0
	for _, c := range backupData.Clues {
		id, _ := c["id"].(string)
		if id == "" {
			continue
		}
		exists, err := s.backupDataRepo.ClueExists(ctx, id)
		if err != nil {
			logger.Error(err, "检查线索失败: "+id)
			continue
		}
		if exists {
			continue
		}
		row := map[string]any{
			"id":          id,
			"source_id":   c["source_id"],
			"account":     c["account"],
			"clue_type":   c["type"],
			"is_verify":   c["is_verify"],
			"name":        c["name"],
			"city":        c["city"],
			"address":     c["address"],
			"description": c["desc"],
		}
		if err := s.backupDataRepo.RestoreClue(ctx, row); err != nil {
			logger.Error(err, "恢复线索失败: "+id)
			continue
		}
		restoredClues++
	}

	restoredUsers := 0
	for _, u := range backupData.Users {
		username, _ := u["username"].(string)
		if username == "" {
			continue
		}
		exists, err := s.backupDataRepo.UserExistsByUsername(ctx, username)
		if err != nil {
			continue
		}
		if exists {
			continue
		}

		if err := s.backupDataRepo.RestoreUser(ctx, map[string]any{
			"id":       u["id"],
			"username": username,
			"email":    u["email"],
			"phone":    u["phone"],
			"password": u["password"],
		}); err != nil {
			logger.Error(err, "恢复用户失败: "+username)
			continue
		}
		restoredUsers++
	}

	type restoreExtra struct {
		key       string
		tableName string
		rows      []map[string]any
	}
	extras := []restoreExtra{

		{"mfa", "user_mfa", backupData.MFA},
		{"obs_config", "obs_config", backupData.OBSConfig},
		{"email_accounts", "email_accounts", backupData.EmailAccounts},
		{"email_jobs", "email_jobs", backupData.EmailJobs},
		{"dnc", "customer_do_not_contact", backupData.DNC},
		{"system_config", "system_config", backupData.SystemConfig},
		{"webhook_subscriptions", "webhook_subscriptions", backupData.WebhookSubs},
		{"password_history", "password_history", backupData.PasswordHistory},

		{"customer_sessions", "customer_sessions", backupData.CustomerSessions},
		{"messages", "message", backupData.Messages},
		{"csat_scores", "csat_surveys", backupData.CsatScores},
		{"customers", "customers", backupData.Customers},
		{"agent_statuses", "agent_statuses", backupData.AgentStatuses},
		{"alert_rules", "alert_rules", backupData.AlertRules},
		{"automation_rules", "automation_rules", backupData.AutomationRules},
		{"bridge_accounts", "bridge_accounts", backupData.BridgeAccounts},
		{"operation_logs", "operation_logs", backupData.OperationLogs},
		{"login_events", "login_events", backupData.LoginEvents},
		{"security_alerts", "security_alerts", backupData.SecurityAlerts},
		{"password_reset_tokens", "password_reset_tokens", backupData.PasswordResetTokens},
		{"prompts", "prompts", backupData.Prompts},
		{"ai_agents", "ai_agents", backupData.AIAgents},
		{"feature_flags", "feature_flags", backupData.FeatureFlags},
	}
	restoredExtraTables := 0
	for _, e := range extras {
		if len(e.rows) == 0 {
			continue
		}
		if err := s.backupDataRepo.RestoreTable(ctx, e.tableName, e.rows); err != nil {
			logger.Error(err, fmt.Sprintf("恢复扩表 %s 失败", e.tableName))
			continue
		}
		restoredExtraTables++
	}

	logger.Info(fmt.Sprintf("备份 %s 恢复完成: 线索 %d 条, 用户 %d 个, 扩表 %d 组",
		backup.BackupName, restoredClues, restoredUsers, restoredExtraTables))

	os.RemoveAll("restore_tmp")
	return nil
}

// GetRestoreList 获取恢复记录列表
func (s *RestoreService) GetRestoreList(ctx context.Context, page, pageSize int) ([]*model.RestoreRecord, int64, error) {
	return s.restoreRepo.GetAll(ctx, page, pageSize)
}

// GetLastRestore 获取最近一次恢复记录
func (s *RestoreService) GetLastRestore(ctx context.Context) (*model.RestoreRecord, error) {
	return s.restoreRepo.GetLastRestore(ctx)
}

// ScheduleBackupService 定时备份服务
type ScheduleBackupService struct {
	backupService *BackupService
}

// NewScheduleBackupService 创建定时备份服务实例
func NewScheduleBackupService() *ScheduleBackupService {
	return &ScheduleBackupService{
		backupService: NewBackupService(),
	}
}

func (s *ScheduleBackupService) CreateDailyBackup(ctx context.Context) error {
	logger.Info("执行定时备份任务...")

	successCount := 0
	failCount := 0
	req := &CreateBackupRequest{
		BackupName: fmt.Sprintf("daily_backup_%s_%d", time.Now().Format("20060102"), time.Now().UnixNano()%1000000),
		BackupType: model.BackupTypeFull,
	}
	if _, err := s.backupService.CreateBackup(ctx, 0, req); err != nil {
		logger.Error(err, "定时备份失败")
		failCount++
	} else {
		successCount++
	}

	logger.Info(fmt.Sprintf("定时备份任务完成: 成功 %d, 失败 %d", successCount, failCount))
	return nil
}

// RunDailyBackup 运行每日备份（定时任务入口）
func RunDailyBackup() {
	service := NewScheduleBackupService()
	service.CreateDailyBackup(context.Background())
}
