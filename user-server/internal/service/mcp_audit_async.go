package service

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// mcp_audit_async.go MCP 审计异步写入器（提交组6 A-2，2026-10-11）。
//
// 为什么异步：审计此前在请求路径上同步 best-effort 落库（失败只记日志）——
// DB 抖动时每条 MCP 调用都白扛一次写放大，而审计写失败依旧静默丢。
// 现在统一进内存队列：批量落库 + 失败重试 3 次（100/200/400ms 退避）+
// 最终失败的进丢弃计数（可观测）。审计写失败仍然不阻断工具调用，
// 但"丢了多少"从无人知晓变成一个可读的数。
//
// 生命周期：首次 Record 时惰性启动，进程级单例（main.go 当前禁碰，
// 不走显式 Start/Stop——进程退出丢队列尾巴属可接受损耗，丢弃计数可见）。
// 篡改防护（hash-chain/WORM）与告警 webhook 在 P2/P3（backlog A-6）。

const (
	mcpAuditQueueSize   = 4096
	mcpAuditBatchSize   = 50
	mcpAuditFlushPeriod = 2 * time.Second
)

type mcpAuditWriter struct {
	callCh  chan *model.MCPCallAudit
	adminCh chan *model.MCPCredentialAuditLog

	dropped atomic.Int64 // 重试耗尽仍失败的行数（可观测口径）
	retried atomic.Int64 // 重试次数（>0 即发生过 DB 抖动）
	flushed atomic.Int64 // 成功落库行数

	startOnce sync.Once
}

var (
	mcpAuditWriterRef   *mcpAuditWriter
	mcpAuditWriterMutex sync.Mutex
)

// GetMCPAuditWriter 进程级单例（首次调用惰性启动后台 flush 循环）。
func GetMCPAuditWriter() *mcpAuditWriter {
	mcpAuditWriterMutex.Lock()
	defer mcpAuditWriterMutex.Unlock()
	if mcpAuditWriterRef == nil {
		w := &mcpAuditWriter{
			callCh:  make(chan *model.MCPCallAudit, mcpAuditQueueSize),
			adminCh: make(chan *model.MCPCredentialAuditLog, mcpAuditQueueSize),
		}
		go w.loopCall()
		go w.loopAdmin()
		mcpAuditWriterRef = w
	}
	return mcpAuditWriterRef
}

// MCPAuditDroppedCount 丢弃计数（监控/告警接 P2；当前先可查询）。
func MCPAuditDroppedCount() int64 { return GetMCPAuditWriter().dropped.Load() }

// EnqueueCall 入队一条调用审计；队满时同步兜底写一次（尽力而为）。
func (w *mcpAuditWriter) EnqueueCall(repo *repository.MCPCallAuditRepo, row *model.MCPCallAudit) {
	select {
	case w.callCh <- row:
	default:
		// 队满：同步写兜底，失败计入丢弃——绝不因审计积压阻塞工具调用。
		if err := repo.Create(context.Background(), row); err != nil {
			w.dropped.Add(1)
		}
	}
}

// EnqueueAdmin 同 EnqueueCall（管理审计）。
func (w *mcpAuditWriter) EnqueueAdmin(repo *repository.MCPCredentialAuditLogRepo, row *model.MCPCredentialAuditLog) {
	select {
	case w.adminCh <- row:
	default:
		if err := repo.Create(context.Background(), row); err != nil {
			w.dropped.Add(1)
		}
	}
}

func (w *mcpAuditWriter) loopCall() {
	batch := make([]*model.MCPCallAudit, 0, mcpAuditBatchSize)
	ticker := time.NewTicker(mcpAuditFlushPeriod)
	defer ticker.Stop()
	for {
		select {
		case row := <-w.callCh:
			batch = append(batch, row)
			if len(batch) >= mcpAuditBatchSize {
				w.flushCall(batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			if len(batch) > 0 {
				w.flushCall(batch)
				batch = batch[:0]
			}
		}
	}
}

func (w *mcpAuditWriter) flushCall(batch []*model.MCPCallAudit) {
	repo := repository.NewMCPCallAuditRepo()
	if !repo.Available() {
		w.dropped.Add(int64(len(batch)))
		return
	}
	backoffs := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond}
	for _, row := range batch {
		ok := false
		for attempt := 0; attempt <= len(backoffs); attempt++ {
			if attempt > 0 {
				w.retried.Add(1)
				time.Sleep(backoffs[attempt-1])
			}
			if err := repo.Create(context.Background(), row); err == nil {
				ok = true
				break
			}
		}
		if ok {
			w.flushed.Add(1)
		} else {
			w.dropped.Add(1)
		}
	}
}

func (w *mcpAuditWriter) loopAdmin() {
	batch := make([]*model.MCPCredentialAuditLog, 0, mcpAuditBatchSize)
	ticker := time.NewTicker(mcpAuditFlushPeriod)
	defer ticker.Stop()
	for {
		select {
		case row := <-w.adminCh:
			batch = append(batch, row)
			if len(batch) >= mcpAuditBatchSize {
				w.flushAdmin(batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			if len(batch) > 0 {
				w.flushAdmin(batch)
				batch = batch[:0]
			}
		}
	}
}

func (w *mcpAuditWriter) flushAdmin(batch []*model.MCPCredentialAuditLog) {
	repo := repository.NewMCPCredentialAuditLogRepo()
	if !repo.Available() {
		w.dropped.Add(int64(len(batch)))
		return
	}
	backoffs := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond}
	for _, row := range batch {
		ok := false
		for attempt := 0; attempt <= len(backoffs); attempt++ {
			if attempt > 0 {
				w.retried.Add(1)
				time.Sleep(backoffs[attempt-1])
			}
			if err := repo.Create(context.Background(), row); err == nil {
				ok = true
				break
			}
		}
		if ok {
			w.flushed.Add(1)
		} else {
			w.dropped.Add(1)
			logger.Warnf("[mcp-audit] 管理审计重试耗尽仍失败（丢弃累计 %d）", w.dropped.Load())
		}
	}
}
