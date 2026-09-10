// reach_send_pipeline_compliance.go 合规提醒与审计（R-8）：主动触达发送前的
// 合规提醒 WARN 日志，以及异步批量落库的合规审计记录（表 reach_compliance_log）。
package service

import (
	"context"
	"sync"
	"time"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

const complianceReminderTag = "[COMPLIANCE]"

func init() { _db.RegisterExtraModels(&model.ReachComplianceLog{}) }

// ReachComplianceLog 合规提醒审计日志（别名，模型已收敛到 model 层）
type ReachComplianceLog = model.ReachComplianceLog

const (
	complianceFlushBatchSize = 100
	complianceFlushInterval  = 5 * time.Second
)

// ComplianceAuditLogger R-8：异步批量落库的合规日志器
type ComplianceAuditLogger struct {
	mu      sync.Mutex
	buf     []*ReachComplianceLog
	repo    *repository.ReachComplianceLogRepository
	flushCh chan struct{}
	stop    chan struct{}
	stopped sync.Once
}

var (
	complianceLoggerOnce sync.Once
	complianceLogger     *ComplianceAuditLogger
)

// InitComplianceAuditLogger 初始化全局合规审计落库器（main/router 装配时调用一次）。
// db 为 nil 时退化为仅 WARN 日志（向后兼容）。表结构经 EnsureTable 惰性创建，
// migrate.go 正式注册另行报告。
func InitComplianceAuditLogger(db *gorm.DB) *ComplianceAuditLogger {
	complianceLoggerOnce.Do(func() {
		complianceLogger = &ComplianceAuditLogger{
			repo:    repository.NewReachComplianceLogRepository(db),
			flushCh: make(chan struct{}, 1),
			stop:    make(chan struct{}),
		}
		if db != nil {
			go complianceLogger.flushLoop()
		}
	})
	return complianceLogger
}

// GetComplianceAuditLogger 获取全局实例（未初始化返回 nil）
func GetComplianceAuditLogger() *ComplianceAuditLogger { return complianceLogger }

func (l *ComplianceAuditLogger) record(channel, recipientID string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.buf = append(l.buf, &ReachComplianceLog{Channel: channel, RecipientID: recipientID})
	if len(l.buf) > complianceFlushBatchSize*10 {
		l.buf = l.buf[len(l.buf)-complianceFlushBatchSize*10:]
	}
	full := len(l.buf) >= complianceFlushBatchSize
	l.mu.Unlock()
	if full {
		select {
		case l.flushCh <- struct{}{}:
		default:
		}
	}
}

// BufferedCount 当前缓冲条数（测试用）
func (l *ComplianceAuditLogger) BufferedCount() int {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buf)
}

// Flush 将缓冲批量写入 DB（供 flushLoop 与测试调用）；db 未配置时清空缓冲并返回
func (l *ComplianceAuditLogger) Flush() error {
	l.mu.Lock()
	if len(l.buf) == 0 {
		l.mu.Unlock()
		return nil
	}
	batch := l.buf
	l.buf = nil
	l.mu.Unlock()
	if l.repo == nil || len(batch) == 0 {
		return nil
	}
	if err := l.repo.CreateInBatches(context.Background(), copyComplianceBatch(batch), len(batch)); err != nil {

		l.mu.Lock()
		l.buf = append(batch, l.buf...)
		if len(l.buf) > complianceFlushBatchSize*10 {
			l.buf = l.buf[len(l.buf)-complianceFlushBatchSize*10:]
		}
		l.mu.Unlock()
		return err
	}
	return nil
}

// copyComplianceBatch 转换缓冲行为仓储入参
func copyComplianceBatch(batch []*ReachComplianceLog) []*model.ReachComplianceLog {
	out := make([]*model.ReachComplianceLog, 0, len(batch))
	for _, b := range batch {
		out = append(out, &model.ReachComplianceLog{
			Channel:     b.Channel,
			RecipientID: b.RecipientID,
			CreatedAt:   b.CreatedAt,
		})
	}
	return out
}

func (l *ComplianceAuditLogger) flushLoop() {
	ticker := time.NewTicker(complianceFlushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-l.stop:
			_ = l.Flush()
			return
		case <-l.flushCh:
			if err := l.Flush(); err != nil {
				logger.Errorf("[R-8] 合规日志刷盘失败: %v", err)
			}
		case <-ticker.C:
			if err := l.Flush(); err != nil {
				logger.Errorf("[R-8] 合规日志刷盘失败: %v", err)
			}
		}
	}
}

// Stop 停止刷盘协程（落盘剩余缓冲）
func (l *ComplianceAuditLogger) Stop() {
	if l == nil {
		return
	}
	l.stopped.Do(func() { close(l.stop) })
}

func LogComplianceReminder(channel, recipientID string) {
	logger.Warnf("%s 主动触达发送已触发：channel=%s, recipient=%s。"+
		"请严格遵守各渠道平台（微信/企业微信/抖音/快手/小红书/Telegram/WhatsApp(Meta)/短信/邮件 等）的"+
		"开发者规范、服务条款及当地法律法规；仅可向已授权、已明确同意接收的联系人发送，"+
		"严格控制发送频率，禁止发送垃圾营销、欺诈、骚扰或违法违规内容。"+
		"因违规发送导致的账号封禁、平台处罚、行政处罚或法律后果由使用者自行承担。",
		complianceReminderTag, channel, recipientID)

	complianceLogger.record(channel, recipientID)
}
