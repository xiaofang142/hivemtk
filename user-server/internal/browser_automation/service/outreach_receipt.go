package service

import (
	"context"
	"encoding/json"

	"hivemtk-user/internal/browser_automation/model"
	"hivemtk-user/internal/browser_automation/repository"
	"hivemtk-user/internal/pkg/utils/logger"
)

// outreach_receipt.go — 触达 P0 Chunk 5：触达回执留存（验收交付物）。
//
// 修的事实：一次触达的「验收」需要三样东西留在库里——截图、帖子链接、当时发出的文案原文。
// 今天这三样都撑不起验收：final_screenshot_url 只有用户显式编排了 screenshot 步才有；
// 帖子链接只散在 command_log 与 extracted_data 的 JSON 里，没有可检索的列；
// 文案只留下 hash，事后无从核对「当时到底发了什么」。
//
// 两条纪律：
//  ① 回执是**增强层**，不是闸门。它落不落得住都不改「内容已发布」这个事实，
//     所以全部 warn-only：截图失败、落库失败都只记日志，绝不判红——
//     判红会让一次已经真发出去的触达在面板上显示为失败，人重跑就是双发。
//  ② verified=false 的回执**照样落**。「平台侧归因不到」与「没发出去」是两件事，
//     双发闸正是因为分不清才把人叫来裁决；回执行若只留绿的，等于替平台做了裁决。
//
// 文案快照取 step.Value 原文（不取 hash）：去重靠 hash 判「是不是同一条」，
// 回执靠原文回答「发的是什么」——两者用途不同，取同一个值必然有一边不够用。

// receiptPending 收口期挂起的待落回执。
//
// 为什么不在 verified 分支直接落：截图必须赶在 cleanupSessionTab 回收 tab 之前拍
// （G17：captureVisibleTab 只能截激活 tab，而 tab 一回收证据就没了），而一条任务里
// 可能发多条触达。逐条当场截图 = 每条都抢一次焦点；收口时拍一张挂在本次会话的回执行上
// 是同一次抢焦点，且交付形态不变（一次触达活动一张验收截图）。
type receiptPending struct {
	platform  string
	action    string
	targetURL string
	copyHash  string
	copyText  string
	evidence  map[string]any
	verified  bool
	stepIndex int
}

// setOutreachReceiptRepository 回执仓储注入（装配期一次性）。
// 漏装不报错（nil 短路），代价是触达发出去但验收面是空的。
func (e *Executor) SetOutreachReceiptRepository(r repository.BrowserOutreachReceiptRepository) {
	e.outreachReceiptRepo = r
}

// enqueueOutreachReceipt 记下本步待落的回执（不落库）。
// oc 为 nil 表示去重键压根没冻结（目标 URL 取不到/归一化失败），此时仍落回执但不带
// 帖子链接——「发出去了但不知道发在哪」也是事实，比整条回执消失要好。
func (e *Executor) enqueueOutreachReceipt(ctx context.Context, task *model.BrowserTask, session *model.BrowserSession, step parsedStep, stepRow *model.BrowserStep, oc *outreachDedupeCtx, evidence map[string]any, verified bool) {
	if e.outreachReceiptRepo == nil {
		return
	}
	p := &receiptPending{
		platform: taskPlatformID(task),
		action:   step.Action,
		copyHash: writeStepKey(step),
		copyText: step.Value,
		verified: verified,
	}
	if stepRow != nil {
		p.stepIndex = stepRow.StepIndex
	}
	if oc != nil {
		p.platform = oc.platform
		p.targetURL = oc.targetURL
		p.action = oc.action
		p.copyHash = oc.copyHash
	}
	if p.targetURL == "" {
		// 去重键没冻结（去重仓储未接线 / 归一化失败）时回执仍要带帖子链接：
		// 回执是验收交付物，它的完整性不该挂在去重层是否装配上。写步稀少，
		// 这里多拍一次只读快照的成本可以接受；取不到就留空——「发出去了但
		// 不知道发在哪」也是事实，比整条回执消失要好。
		if e.hand != nil && session.ChromeTabID != 0 {
			if _, raw, err := e.hand.snapshot(ctx, task.UserID, session.ChromeTabID); err != nil {
				logger.Warnf("[BrowserExec] 回执取页面链接失败 session=%d: %v", session.ID, err)
			} else if norm, ok := normalizeOutreachURL(raw); ok {
				p.targetURL = norm
			}
		}
	}
	p.evidence = evidence
	// 与 stopRegistry / confirmRegistry 同纪律：Executor 是进程级单例，
	// 不同用户的会话在同一个 map 上并存，裸 map 写入是数据竞态。
	// 不走 defer：超限分支要先解锁再打日志（defer + 手动 Unlock = 二次解锁 panic）
	e.receiptMu.Lock()
	if e.pendingReceipts == nil {
		e.pendingReceipts = make(map[uint][]*receiptPending)
	}
	if len(e.pendingReceipts[session.ID]) >= receiptPendingCap {
		e.receiptMu.Unlock()
		logger.Errorf("[BrowserExec] 回执挂起数超上限（%d）session=%d，本条不再挂起（触达本身已执行，不受影响）", receiptPendingCap, session.ID)
		return
	}
	e.pendingReceipts[session.ID] = append(e.pendingReceipts[session.ID], p)
	e.receiptMu.Unlock()
}

// receiptPendingCap 单会话待落回执行上限。触达步数由 steps JSON 决定、有界，
// 但 sessions 表里 steps 是可写的——没有上限时，一条畸形编排就能把进程内存吃光。
// 超限后只记日志不阻断触达：回执是增强层，宁可少留证据也不能让触达线停摆。
const receiptPendingCap = 200

// discardPendingReceipts 兜底清理（ExecuteSession 收口没跑到时兜底，见调用点 defer）。
func (e *Executor) discardPendingReceipts(sessionID uint) {
	e.receiptMu.Lock()
	defer e.receiptMu.Unlock()
	delete(e.pendingReceipts, sessionID)
}

// captureSessionReceipts 会话收口：拍一张验收截图 + 把本会话待落的回执行写进库。
//
// 落点铁律：必须在 cleanupSessionTab 之前调用。tab 一回收，captureVisibleTab
// 就只能截到用户自己的页面（静默假内容，比没有更坏），所以收口顺序是
// 拍回执截图 → 落库 → 回收 tab。
//
// 仅在「本会话确实有触达」与「已经拍到截图」时才有动作：
// 只读任务每轮都截一张 30s 的图会把验收面变成噪声，而噪声会让人不再看这张面。
func (e *Executor) captureSessionReceipts(ctx context.Context, task *model.BrowserTask, session *model.BrowserSession, seq *int) {
	if e.outreachReceiptRepo == nil || session == nil {
		return
	}
	e.receiptMu.Lock()
	pending := e.pendingReceipts[session.ID]
	delete(e.pendingReceipts, session.ID)
	e.receiptMu.Unlock()
	if len(pending) == 0 {
		return
	}
	screenshotURL := e.captureReceiptScreenshot(ctx, task, session, seq)
	for _, p := range pending {
		if p == nil {
			continue
		}
		rec := &model.BrowserOutreachReceipt{
			TaskID:        task.ID,
			SessionID:     session.ID,
			Platform:      p.platform,
			Action:        p.action,
			TargetURL:     p.targetURL,
			CopyHash:      p.copyHash,
			CopySnapshot:  p.copyText,
			ScreenshotURL: screenshotURL,
			Verified:      p.verified,
		}
		if len(p.evidence) > 0 {
			if blob, err := json.Marshal(p.evidence); err == nil {
				rec.Evidence = blob
			} else {
				logger.Warnf("[BrowserExec] 回执证据序列化失败 session=%d step=%d: %v", session.ID, p.stepIndex, err)
			}
		}
		if err := e.outreachReceiptRepo.RecordOutreachReceipt(ctx, rec); err != nil {
			logger.Warnf("[BrowserExec] 触达回执落库失败 session=%d（内容已发布，仅记日志）: %v", session.ID, err)
		}
	}
	logger.Infof("[BrowserExec] 触达回执已留存 session=%d 回执数=%d 截图=%q", session.ID, len(pending), screenshotURL)
}

// captureReceiptScreenshot 拍一张验收截图并存公开 URL。
// 任何一环失败都只返回空串（回执行照落，只是没有图）：截图是回执的一部分，
// 不是回执成立的前提。
func (e *Executor) captureReceiptScreenshot(ctx context.Context, task *model.BrowserTask, session *model.BrowserSession, seq *int) string {
	if e.hand == nil || session.ChromeTabID == 0 {
		return ""
	}
	b64, err := e.hand.screenshot(ctx, task.UserID, session.ChromeTabID, true)
	if err != nil {
		logger.Warnf("[BrowserExec] 回执截图失败 session=%d: %v", session.ID, err)
		return ""
	}
	if b64 == "" || e.feedback == nil {
		return ""
	}
	url, err := e.feedback.SaveFinalScreenshot(ctx, session.ID, b64)
	if err != nil {
		logger.Warnf("[BrowserExec] 回执截图落库失败 session=%d: %v", session.ID, err)
		return ""
	}
	if url != "" {
		session.FinalScreenshotURL = url
		_ = e.sessionRepo.UpdateArtifacts(ctx, session.ID, nil, url, "")
	}
	if seq != nil {
		*seq++
		e.appendCommandLog(ctx, session.ID, task.ID, 0, *seq, "event", "outreach_receipt", map[string]any{
			"reason":         "receipt_screenshot",
			"screenshot_url": url,
		}, 0, verdict(true))
	}
	return url
}

// SetOutreachReceiptRepository 回执读侧注入（未装配时列表返回空不报错，同 D1 纪律）。
func (s *TaskService) SetOutreachReceiptRepository(r repository.BrowserOutreachReceiptRepository) {
	s.outreachReceiptRepo = r
}

// ListOutreachReceipts 任务级回执读侧。
//
// 归属校验先过任务再查回执（taskRepo.GetByID 带 user_id）：回执行自己没有 user_id 列，
// 不先落这一步就等于「知道 task_id 就能读别人的触达原文与截图链接」。
func (s *TaskService) ListOutreachReceipts(ctx context.Context, taskID uint, userID uint) ([]*model.BrowserOutreachReceipt, error) {
	if _, err := s.taskRepo.GetByID(ctx, taskID, userID); err != nil {
		return nil, err
	}
	if s.outreachReceiptRepo == nil {
		return nil, nil
	}
	return s.outreachReceiptRepo.ListByTaskID(ctx, taskID, userID, 50)
}
