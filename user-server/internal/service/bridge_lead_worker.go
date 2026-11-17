package service

import (
	"context"
	"errors"
	"sync"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

type BridgeLeadProcessor func(context.Context, *model.MessageHub) error

type BridgeLeadPermission func(context.Context) (bool, error)

type BridgeLeadWorker struct {
	repo       *repository.BridgeLeadTaskRepository
	process    BridgeLeadProcessor
	permission BridgeLeadPermission
	execution  sync.Mutex
	lifecycle  sync.Mutex
	started    bool
	stopped    bool
	cancel     context.CancelFunc
	done       chan struct{}
}

func NewBridgeLeadWorker(repo *repository.BridgeLeadTaskRepository, process BridgeLeadProcessor, permissions ...BridgeLeadPermission) *BridgeLeadWorker {
	permission := BridgeLeadPermission(func(ctx context.Context) (bool, error) {
		return NewModuleGateService().ConfirmEnabledForExecution(ctx, model.ModuleCustomer)
	})
	if len(permissions) > 0 {
		permission = permissions[0]
	}
	return &BridgeLeadWorker{repo: repo, process: process, permission: permission}
}

func (w *BridgeLeadWorker) RunTenant(ctx context.Context) (int64, error) {
	if w == nil || w.repo == nil || w.process == nil || w.permission == nil || ctx == nil {
		return 0, errors.New("bridge lead worker dependencies unavailable")
	}
	w.lifecycle.Lock()
	stopped := w.stopped
	w.lifecycle.Unlock()
	if stopped {
		return 0, errors.New("bridge lead worker instance stopped")
	}
	if !w.execution.TryLock() {
		return 0, nil
	}
	defer w.execution.Unlock()
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	allowed, err := w.permission(ctx)
	if err != nil || !allowed {
		return 0, err
	}
	if _, err := w.repo.Expire(ctx, time.Now()); err != nil {
		return 0, err
	}
	if _, err := w.repo.Reconcile(ctx, 100); err != nil {
		return 0, err
	}
	// 一个实例只有一个执行槽位；不预先认领一批等待中的外发任务。
	tasks, err := w.repo.Claim(ctx, time.Now(), time.Minute, 1)
	if err != nil || len(tasks) == 0 {
		return 0, err
	}
	task := &tasks[0]
	// 认领后再次确认；尚未开始的任务保留租约，过期后可安全恢复 pending。
	allowed, err = w.permission(ctx)
	if err != nil || !allowed {
		return 0, err
	}
	began, err := w.repo.Begin(ctx, task, time.Now())
	if err != nil || !began {
		return 0, err
	}
	status, reason := model.BridgeLeadCompleted, ""
	hub, err := w.repo.LoadHub(ctx, task)
	if err != nil {
		status, reason = model.BridgeLeadUnknown, "scope_unconfirmed"
	} else if hub.SentAt.IsZero() || hub.SentAt.Before(time.Now().Add(-InboxReplyWindow)) || hub.SentAt.After(time.Now().Add(InboxBackfillFutureTolerance)) {
		status, reason = model.BridgeLeadSkipped, "stale_input"
	} else {
		processCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		panicked := false
		err, panicked = w.execute(processCtx, hub)
		cancel()
		if panicked {
			status, reason = model.BridgeLeadUnknown, "execution_panicked"
		} else if err != nil {
			status, reason = model.BridgeLeadUnknown, "execution_failed"
		}
	}
	// 请求/关停取消不能吞掉终态记录；短预算仅用于记录，不继续外发。
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	finished, finishErr := w.repo.Finish(finishCtx, task, status, time.Now(), reason)
	if finishErr != nil {
		return 0, finishErr
	}
	if !finished {
		return 0, errors.New("bridge lead completion lease no longer valid")
	}
	if status == model.BridgeLeadUnknown {
		logger.Ctx(ctx).Warn().Uint("task_id", task.ID).Str("reason", reason).Msg("bridge lead outcome unknown; automatic replay disabled")
		return 1, errors.New("bridge lead execution outcome unknown")
	}
	return 1, nil
}

func (w *BridgeLeadWorker) execute(ctx context.Context, hub *model.MessageHub) (err error, panicked bool) {
	defer func() {
		if recover() != nil {
			err, panicked = errors.New("bridge lead processor panicked"), true
		}
	}()
	err = w.process(ctx, hub)
	if err == nil {
		err = ctx.Err()
	}
	return err, false
}

func (w *BridgeLeadWorker) Start(parent context.Context) error {
	if w == nil || w.repo == nil || w.process == nil || w.permission == nil || parent == nil {
		return errors.New("bridge lead worker lifecycle requires dependencies and context")
	}
	w.lifecycle.Lock()
	defer w.lifecycle.Unlock()
	if w.started || w.stopped {
		return errors.New("bridge lead worker instance already started or stopped")
	}
	ctx, cancel := context.WithCancel(parent)
	w.cancel, w.done, w.started = cancel, make(chan struct{}), true
	utils.SafeGo(ctx, "bridge-lead.worker", func(ctx context.Context) {
		defer close(w.done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			if ctx.Err() != nil {
				return
			}
			_, err := tenant.Fanout(ctx, nil, func(tenantCtx context.Context, _ uint) (int64, error) {
				return w.RunTenant(tenantCtx)
			})
			if err != nil && ctx.Err() == nil {
				logger.Ctx(ctx).Warn().Msg("bridge lead worker pass failed; persisted tasks retained")
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	})
	return nil
}

func (w *BridgeLeadWorker) Stop(ctx context.Context) error {
	if w == nil {
		return nil
	}
	if ctx == nil {
		return errors.New("bridge lead worker stop context unavailable")
	}
	w.lifecycle.Lock()
	w.stopped = true
	cancel, done := w.cancel, w.done
	w.lifecycle.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
