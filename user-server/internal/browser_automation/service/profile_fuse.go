package service

import (
	"context"

	"hivemtk-user/internal/browser_automation/model"
	"hivemtk-user/internal/pkg/utils/logger"
)

// profile_fuse.go — Chunk2 单主 Profile 健康监护：封号熔断写入。
//
// 本项目风控特色是复用主 Profile（不做多账号矩阵），熔断对象是
// 「平台 × 主 Profile 登录态」而非某个子账号。熔断状态必须落库——
// Executor 是进程级单例（R-A4），内存计数会跨 session 串包，重启即丢。

// recordProfileBlocked 封号熔断写入：MarkBlocked 落库 + block_fused 审计帧。
// profileHealthRepo==nil（未接线）时跳过写入——熔断是增强不是门禁，执行不受影响。
// 返回库错误（调用方决定是否中止）；审计帧失败仅告警（appendCommandLog 自带 nil-safe）。
func (e *Executor) recordProfileBlocked(ctx context.Context, task *model.BrowserTask, session *model.BrowserSession, reason string) error {
	platform := taskPlatformID(task)
	seq := 0
	if e.profileHealthRepo == nil {
		return nil
	}
	if err := e.profileHealthRepo.MarkBlocked(ctx, platform, reason); err != nil {
		logger.Errorf("[BrowserFuse] 熔断落库失败 platform=%s: %v", platform, err)
		return err
	}
	e.appendCommandLog(ctx, session.ID, task.ID, 0, seq, "event", "block_fused",
		map[string]any{"platform": platform, "reason": truncateRunes(reason, 200, "…")}, 0, verdict(false))
	return nil
}
