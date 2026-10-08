package service

import (
	"context"
	"net/url"
	"strings"

	"hivemtk-user/internal/browser_automation/model"
	"hivemtk-user/internal/browser_automation/repository"
	"hivemtk-user/internal/pkg/utils/logger"
)

// outreachDedupeKey 触达去重键（platform + 归一化目标 URL + 文案 hash；action 由调用方拼入查询）。
// 注：测试文件另有同名 4 参辅助函数 outreachKey(platform,url,action,hash)，此处类型名加 Dedupe 后缀避让。
type outreachDedupeKey struct {
	Platform  string
	TargetURL string
	CopyHash  string
}

// normalizeOutreachURL 归一化目标 URL：小写 scheme+host、去 fragment、trim 尾斜杠、保留 query。
// 非法 URL 返回 ("", false)，调用方应跳过检查（fail-open，去重是优化层）。
func normalizeOutreachURL(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", false
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Fragment = ""
	u.Path = strings.TrimSuffix(u.Path, "/")
	return u.String(), true
}

// checkOutreachDedupe 查跨任务触达去重：先归一化再查；非法 URL 跳过 (false, key, nil)。
// 返回 (hit, key, err)：hit=true 表示已触达过，应跳过本次发送。
func checkOutreachDedupe(ctx context.Context, repo repository.BrowserOutreachDedupeRepository, platform, rawURL, action, copyHash string) (bool, outreachDedupeKey, error) {
	key := outreachDedupeKey{Platform: platform, CopyHash: copyHash}
	norm, ok := normalizeOutreachURL(rawURL)
	if !ok {
		return false, key, nil
	}
	key.TargetURL = norm
	row, err := repo.FindOutreachHit(ctx, platform, norm, action, copyHash)
	if err != nil {
		return false, key, err
	}
	return row != nil, key, nil
}

// outreachDedupeCtx 写步携带的去重上下文：键已在检查点备齐，插入点直接用，
// 不再二次归一化/二次查库（检查与插入之间页面可能跳转，键必须冻结）。
type outreachDedupeCtx struct {
	platform  string
	targetURL string
	action    string
	copyHash  string
}

// resolveOutreachDedupe 写步去重检查（只在写步调用）：返回 (ctx*, skip, err)。
// skip=true 表示跨任务已触达过，调用方应 finishStep skipped + 审计帧后绿返回。
// ctx 在**未命中时也非 nil**（键已冻结，供 verified 后落去重行）；
// 只有「压根没查成」——repo 未接线 / pageURL 缺失且快照失败 / 归一化失败 / DB 错误——才是 nil。
// fail-open：上述任一情形一律 warn 后放行——去重是防扰民层，真安全网是写声明与台账，
// 不得因它拦停发送。
func resolveOutreachDedupe(ctx context.Context, e *Executor, task *model.BrowserTask, step parsedStep, writeKey, pageURL string, tabID int) (*outreachDedupeCtx, bool, error) {
	if e.outreachDedupeRepo == nil {
		return nil, false, nil
	}
	if pageURL == "" {
		// 显式路径无现货 pageURL：按需快照补齐（写步稀少，读只读可接受）。
		if e.hand == nil {
			return nil, false, nil
		}
		if _, url, err := e.hand.snapshot(ctx, task.UserID, tabID); err != nil {
			logger.Warnf("[BrowserExec] 去重快照失败（跳过去重检查）: %v", err)
			return nil, false, nil
		} else {
			pageURL = url
		}
	}
	hit, key, err := checkOutreachDedupe(ctx, e.outreachDedupeRepo, taskPlatformID(task), pageURL, step.Action, writeKey)
	if err != nil {
		logger.Warnf("[BrowserExec] 去重查询失败（跳过去重检查）: %v", err)
		return nil, false, nil
	}
	if key.TargetURL == "" {
		// 归一化不出来 ⇒ 没有可冻结的键。fail-open 放行，且不去重行也不落
		// （落一个空 URL 的行等于把「未知目标」永久标成已触达）。
		return nil, false, nil
	}
	// 键在检查点冻结：命中与未命中都要带走。未命中时它是空委托——
	// 本步 verified 之后由 recordOutreachDedupeSend 落行，下一个任务才撞得上。
	// 检查与插入之间页面可能跳转，此刻重新取 URL / 重算 hash 拿到的会是另一个键。
	oc := &outreachDedupeCtx{
		platform:  key.Platform,
		targetURL: key.TargetURL,
		action:    step.Action,
		copyHash:  key.CopyHash,
	}
	return oc, hit, nil
}

// recordOutreachDedupeSend verified 后记去重行：published 即事实，插入失败仅 warn 不判红。
func recordOutreachDedupeSend(ctx context.Context, e *Executor, task *model.BrowserTask, session *model.BrowserSession, oc *outreachDedupeCtx) {
	if e.outreachDedupeRepo == nil || oc == nil {
		return
	}
	if err := e.outreachDedupeRepo.RecordOutreachSend(ctx, &model.BrowserOutreachDedupe{
		Platform:  oc.platform,
		TargetURL: oc.targetURL,
		Action:    oc.action,
		CopyHash:  oc.copyHash,
		TaskID:    task.ID,
		SessionID: session.ID,
	}); err != nil {
		logger.Warnf("[BrowserExec] 去重行落库失败（内容已发布，仅记日志）: %v", err)
	}
}
