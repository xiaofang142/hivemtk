package service

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/storage"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"
)

// backupExtraTables 是导出/恢复共认的扩表清单（data.json 键 → 数据库表名）。
// 两端必须同源：历史上恢复端认 23 张而导出端只写 3 张，导致恢复对缺表静默
// no-op 并最终报 completed（备份实际残缺）。任何一端变更都必须同步另一端。
var backupExtraTables = []struct {
	key       string
	tableName string
}{
	{"mfa", "user_mfa"},
	{"obs_config", "obs_config"},
	{"email_accounts", "email_accounts"},
	{"email_jobs", "email_jobs"},
	{"dnc", "customer_do_not_contact"},
	{"system_config", "system_config"},
	{"webhook_subscriptions", "webhook_subscriptions"},
	{"password_history", "password_history"},

	{"customer_sessions", "customer_sessions"},
	{"messages", "message"},
	{"csat_scores", "csat_surveys"},
	{"customers", "customers"},
	{"agent_statuses", "agent_statuses"},
	{"alert_rules", "alert_rules"},
	{"automation_rules", "automation_rules"},
	{"bridge_accounts", "bridge_accounts"},
	{"operation_logs", "operation_logs"},
	{"login_events", "login_events"},
	{"security_alerts", "security_alerts"},
	{"password_reset_tokens", "password_reset_tokens"},
	{"prompts", "prompts"},
	{"ai_agents", "ai_agents"},
	{"feature_flags", "feature_flags"},
}

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

// NewBackupServiceWithRepos 创建备份服务实例（仓储由装配层注入）
func NewBackupServiceWithRepos(backupRepo *repository.BackupRepository, backupDataRepo repository.BackupDataRepository) *BackupService {
	return &BackupService{
		backupRepo:     backupRepo,
		backupDataRepo: backupDataRepo,
	}
}

// NewBackupService 兼容旧装配点：绑定全局 DB 的仓储。
func NewBackupService() *BackupService {
	return NewBackupServiceWithRepos(repository.NewBackupRepository(), repository.NewBackupDataRepository())
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

	// 后台执行协程拿独立快照：executeBackup 会持续改写状态/路径字段，与调用方共享
	// 同一 *model.Backup 即为数据竞争（go test -race 实证），调用方还可能读到未落库
	// 的中间态。返回原始指针，语义恒为"刚入库的那一行（pending）"。
	// 备份是全实例动作：必须走 SystemScope 才能跨租户读取，否则 on 模式下
	// 隔离 callback 只会导出调用方所属租户的数据，备份"成功"但内容残缺。
	job := *backup
	// SafeGo：备份长跑任务由 API 触发，panic 不能击穿进程
	utils.SafeGo(ctx, "backup.execute", func(_ context.Context) {
		s.executeBackup(tenant.WithSystemScope(context.WithoutCancel(ctx)), &job)
	})

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
			logger.Errorf("[Backup] executeBackup panic: %v\n%s", r, debug.Stack())
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

	backupRoot, _ := storage.BackupSource()
	backupDir := filepath.Join(backupRoot, backup.BackupName)
	if err := os.MkdirAll(backupDir, 0700); err != nil {
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

	// 备份含 PII 与口令哈希：zip 本体收紧到 0600，与 data.json 同级，
	// 不给同组/其他用户留可读面（AUD-09）。
	if e := os.Chmod(backupFile, 0600); e != nil {
		logger.Warnf("[Backup] 备份文件权限收紧失败 path=%s: %v", backupFile, e)
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

	// 保留策略：备份成功后清点同型旧备份，防含 PII 的备份无限累积（AUD-05）。
	s.pruneBackups(ctx, backup)
}

// backupRetentionCount 每种备份类型在本机保留的最大份数；超出后删除最旧的
// 目录与记录。备份是运维自救手段，份数无上限既撑爆磁盘也扩大泄露面。
const backupRetentionCount = 30

// pruneBackups 保留策略清扫：同类型只留最新 backupRetentionCount 份。
// 清扫失败只告警不阻断——本次备份已成功落盘，不因清扫把状态改坏。
func (s *BackupService) pruneBackups(ctx context.Context, keep *model.Backup) {
	list, err := s.backupRepo.ListByType(ctx, keep.BackupType)
	if err != nil {
		logger.Warnf("[Backup] 保留策略列举失败 type=%s: %v", keep.BackupType, err)
		return
	}
	excess := len(list) - backupRetentionCount
	if excess <= 0 {
		return
	}
	for _, old := range list[:excess] {
		if old.ID == keep.ID {
			continue
		}
		if err := s.DeleteBackup(ctx, old.ID); err != nil {
			logger.Warnf("[Backup] 保留策略删除旧备份失败 id=%d: %v", old.ID, err)
		}
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

	clues, err := s.backupDataRepo.DumpClues(ctx)
	if err != nil {
		return nil, err
	}

	users, err := s.dumpAllUsers(ctx)
	if err != nil {
		return nil, err
	}

	links, err := s.dumpAllShortLinks(ctx)
	if err != nil {
		return nil, err
	}

	clueCount := jsonArrayLen(clues)
	userCount := jsonArrayLen(users)
	linkCount := jsonArrayLen(links)

	// 扩表与恢复端 backupExtraTables 同源：导出缺键会被恢复端 fail-closed
	// 探针拦下，但与其靠恢复报错，不如导出端就不产生残缺备份（AUD-04）。
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
	stats := data["stats"].(map[string]int)
	for _, t := range backupExtraTables {
		rows, err := s.backupDataRepo.DumpTable(ctx, t.tableName)
		if err != nil {
			return nil, fmt.Errorf("导出扩表 %s 失败: %w", t.tableName, err)
		}
		data[t.key] = rawMessage(rows)
		stats[t.key] = jsonArrayLen(rows)
	}
	return json.Marshal(data)
}

func rawMessage(b []byte) json.RawMessage {
	if len(b) == 0 {
		return json.RawMessage("[]")
	}
	return json.RawMessage(b)
}

// dumpAllUsers 分页循环已下沉到 repo（keyset 分批，见 backup_data.go DumpUsers）：
// caller 驱动的 offset 分页在导出窗口内有并发写入时整窗漏行（G-10 二次审核）。
func (s *BackupService) dumpAllUsers(ctx context.Context) ([]byte, error) {
	return s.backupDataRepo.DumpUsers(ctx)
}

// dumpAllShortLinks 分页循环已下沉到 repo（keyset 分批，见 DumpShortLinks），
// 理由同 dumpAllUsers。
func (s *BackupService) dumpAllShortLinks(ctx context.Context) ([]byte, error) {
	return s.backupDataRepo.DumpShortLinks(ctx)
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

func (s *BackupService) compressBackup(ctx context.Context, dir, output string) (err error) {
	zipFile, err := os.Create(output)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := zipFile.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	zipWriter := zip.NewWriter(zipFile)
	defer func() {
		if cerr := zipWriter.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

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
		defer func() { _ = file.Close() }()

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
		// 备份目录内除 zip 外还有 data.json，整目录清除而非只删 zip，
		// 避免含 PII 的明文 json 留在磁盘上（AUD-05/09 一并收口）。
		if e := os.RemoveAll(filepath.Dir(backup.FilePath)); e != nil && !os.IsNotExist(e) {
			logger.Warnf("[Backup] 删除备份目录失败 path=%s: %v", filepath.Dir(backup.FilePath), e)
		}
	}

	return s.backupRepo.Delete(ctx, id)
}

// RestoreService 恢复服务
type RestoreService struct {
	restoreRepo    *repository.RestoreRecordRepository
	backupRepo     *repository.BackupRepository
	backupDataRepo repository.BackupDataRepository
}

// NewRestoreServiceWithRepos 创建恢复服务实例（仓储由装配层注入）
func NewRestoreServiceWithRepos(
	restoreRepo *repository.RestoreRecordRepository,
	backupRepo *repository.BackupRepository,
	backupDataRepo repository.BackupDataRepository,
) *RestoreService {
	return &RestoreService{
		restoreRepo:    restoreRepo,
		backupRepo:     backupRepo,
		backupDataRepo: backupDataRepo,
	}
}

// NewRestoreService 兼容旧装配点：绑定全局 DB 的仓储。
func NewRestoreService() *RestoreService {
	return NewRestoreServiceWithRepos(
		repository.NewRestoreRecordRepository(),
		repository.NewBackupRepository(),
		repository.NewBackupDataRepository(),
	)
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

	// SafeGo：恢复长跑任务由 API 触发，panic 不能击穿进程
	// （与备份对称：恢复要写回备份覆盖的全部租户，不能被调用方租户截断；
	//  快照拷贝 *record 与原 go func(src) 传参语义逐字节等价）
	src := *record
	utils.SafeGo(ctx, "backup.execute_restore", func(_ context.Context) {
		s.executeRestore(tenant.WithSystemScope(context.WithoutCancel(ctx)), &src, backup)
	})

	return record, nil
}

func (s *RestoreService) executeRestore(ctx context.Context, record *model.RestoreRecord, backup *model.Backup) {
	record.Status = "running"
	if e := s.restoreRepo.Update(ctx, record); e != nil {
		logger.Errorf("[Restore] 状态落库失败 recordID=%d: %v", record.ID, e)
	}

	if err := s.decompressBackup(ctx, backup.FilePath); err != nil {
		record.Status = "failed"
		record.ErrorMessage = "解压备份失败：" + err.Error()
		if e := s.restoreRepo.Update(ctx, record); e != nil {
			logger.Errorf("[Restore] 失败状态落库失败 recordID=%d: %v", record.ID, e)
		}
		return
	}

	if err := s.restoreDatabase(ctx, backup); err != nil {
		record.Status = "failed"
		record.ErrorMessage = "数据库恢复失败：" + err.Error()
		if e := s.restoreRepo.Update(ctx, record); e != nil {
			logger.Errorf("[Restore] 失败状态落库失败 recordID=%d: %v", record.ID, e)
		}
		return
	}

	record.Status = "completed"
	if e := s.restoreRepo.Update(ctx, record); e != nil {
		logger.Errorf("[Restore] 完成状态落库失败 recordID=%d: %v", record.ID, e)
	}
}

func (s *RestoreService) decompressBackup(ctx context.Context, backupFile string) error {
	_, restoreTmp := storage.BackupSource()
	r, err := zip.OpenReader(backupFile)
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()

	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			return err
		}

		// filepath.Clean + IsLocal：规范化后仍是"当前目录内的相对路径"才放行。
		// 覆盖绝对路径、盘符、".." 上跳（含 sub/../.. 组合）；不再误杀 "b..c" 这类合法文件名。
		name := filepath.Clean(f.Name)
		if !filepath.IsLocal(name) {
			_ = rc.Close()
			return fmt.Errorf("非法的备份条目路径: %s", f.Name)
		}
		path := filepath.Join(restoreTmp, name)

		if f.FileInfo().IsDir() {
			_ = rc.Close()
			if e := os.MkdirAll(path, 0700); e != nil {
				return e
			}
			continue
		}

		if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
			_ = rc.Close()
			return e
		}
		// 0600：备份解出的明文数据在 restore_tmp 清理前不对同组/其他用户可读
		outFile, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
		if err != nil {
			_ = rc.Close()
			return err
		}

		// 循环体内禁止 defer（大备份数千条目会堆积数千 FD 触发 EMFILE），逐文件显式关闭
		_, copyErr := io.Copy(outFile, rc)
		closeErr := outFile.Close()
		_ = rc.Close()
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
	_, restoreTmp := storage.BackupSource()
	jsonFile := filepath.Join(restoreTmp, "data.json")
	data, err := os.ReadFile(jsonFile)
	if err != nil {
		return err
	}

	var backupData struct {
		Version   string           `json:"version"`
		Clues     []map[string]any `json:"clues"`
		Users     []map[string]any `json:"users"`
		ShortLink []map[string]any `json:"short_link"`
	}
	if err := json.Unmarshal(data, &backupData); err != nil {
		return err
	}

	// fail-closed 完整性探针：导出端与恢复端共用 backupExtraTables 同源清单；
	// 缺任一键说明是清单不一致时代的残缺备份，拒绝按"空扩表"静默恢复并报
	// completed（AUD-04：旧格式只有 clues/users/short_link 三键）。
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	var missingKeys []string
	for _, key := range []string{"clues", "users", "short_link"} {
		if _, ok := raw[key]; !ok {
			missingKeys = append(missingKeys, key)
		}
	}
	for _, t := range backupExtraTables {
		if _, ok := raw[t.key]; !ok {
			missingKeys = append(missingKeys, t.key)
		}
	}
	if len(missingKeys) > 0 {
		return fmt.Errorf("备份格式不完整，缺少键: %s（该备份产生于导出/恢复清单不一致的旧版本，请用当前版本程序重新执行备份）",
			strings.Join(missingKeys, ", "))
	}

	restoredClues := 0
	for _, c := range backupData.Clues {
		id, _ := c["id"].(string)
		if id == "" {
			continue
		}
		tenantID := backupRowTenantID(c)
		exists, err := s.backupDataRepo.ClueExists(ctx, tenantID, id)
		if err != nil {
			logger.Error(err, "检查线索失败: "+id)
			continue
		}
		if exists {
			continue
		}
		row := map[string]any{
			"tenant_id": tenantID,
			"id":        id,
			"source_id": c["source_id"],
			"account":   c["account"],
			"type":      c["type"],
			"is_verify": c["is_verify"],
			"name":      c["name"],
			"city":      c["city"],
			"address":   c["address"],
			"desc":      c["desc"],
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
		tenantID := backupRowTenantID(u)
		exists, err := s.backupDataRepo.UserExistsByUsername(ctx, tenantID, username)
		if err != nil {
			continue
		}
		if exists {
			continue
		}

		if err := s.backupDataRepo.RestoreUser(ctx, map[string]any{
			"tenant_id": tenantID,
			"id":        u["id"],
			"username":  username,
			"email":     u["email"],
			"phone":     u["phone"],
			"password":  u["password"],
		}); err != nil {
			logger.Error(err, "恢复用户失败: "+username)
			continue
		}
		restoredUsers++
	}

	restoredLinks := 0
	for _, l := range backupData.ShortLink {
		code, _ := l["short_code"].(string)
		if code == "" {
			continue
		}
		tenantID := backupRowTenantID(l)
		exists, err := s.backupDataRepo.ShortLinkExistsByCode(ctx, tenantID, code)
		if err != nil {
			logger.Error(err, "检查短链失败: "+code)
			continue
		}
		if exists {
			continue
		}
		if err := s.backupDataRepo.RestoreShortLink(ctx, map[string]any{
			"tenant_id":    tenantID,
			"short_code":   code,
			"original_url": l["original_url"],
		}); err != nil {
			logger.Error(err, "恢复短链失败: "+code)
			continue
		}
		restoredLinks++
	}

	restoredExtraTables := 0
	var failedExtraTables []string
	for _, t := range backupExtraTables {
		var rows []map[string]any
		if err := json.Unmarshal(raw[t.key], &rows); err != nil {
			return fmt.Errorf("解析扩表 %s 失败: %w", t.tableName, err)
		}
		if len(rows) == 0 {
			continue
		}
		if err := s.backupDataRepo.RestoreTable(ctx, t.tableName, rows); err != nil {
			logger.Error(err, fmt.Sprintf("恢复扩表 %s 失败", t.tableName))
			failedExtraTables = append(failedExtraTables, t.tableName)
			continue
		}
		restoredExtraTables++
	}
	if len(failedExtraTables) > 0 {
		return fmt.Errorf("恢复扩表失败: %s（数据库处于该备份恢复前的状态或部分表失败，请检查日志）",
			strings.Join(failedExtraTables, ", "))
	}

	logger.Info(fmt.Sprintf("备份 %s 恢复完成: 线索 %d 条, 用户 %d 个, 短链 %d 条, 扩表 %d 组",
		backup.BackupName, restoredClues, restoredUsers, restoredLinks, restoredExtraTables))

	if e := os.RemoveAll(restoreTmp); e != nil {
		logger.Warnf("[Restore] 清理临时目录失败: %v", e)
	}
	return nil
}

// backupRowTenantID 读取备份行归属租户；旧格式没有该字段，按默认租户归属
// （与恢复时 Create 落库的列默认值一致）。
func backupRowTenantID(row map[string]any) uint {
	for _, key := range []string{"tenant_id", "TenantID"} {
		v, ok := row[key]
		if !ok {
			continue
		}
		switch n := v.(type) {
		case uint:
			if n != 0 {
				return n
			}
		case int:
			if n > 0 {
				return uint(n)
			}
		case int64:
			if n > 0 {
				return uint(n)
			}
		case float64:
			if n > 0 && n == float64(uint(n)) {
				return uint(n)
			}
		}
	}
	return tenant.DefaultTenantID
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
	if err := service.CreateDailyBackup(context.Background()); err != nil {
		logger.Errorf("[Backup] 定时备份任务失败: %v", err)
	}
}
