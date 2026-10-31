package egolite

import (
	"context"
	"encoding/csv"
	"fmt"
	"strconv"
	"strings"
	"time"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// audit_export.go —— T22 审计导出（M3 治理）：会话 → 轮次 → 事件 → 产物 一条链。
//
// 用途：用户/管理员要"这次执行到底发生了什么"的完整离线副本——合规问询、
// 事故复盘、争议取证。导出内容与库**逐字节一致**（从账本表直读，不加工口径），
// 归属校验同 GET /sessions/:id（owner 或 admin，越权与不存在同响应）。
//
// 两种形状：
//   - JSON：完整嵌套链（机器审计/再处理）；
//   - CSV：轮次扁平表（人读/表格工具），事件与产物以计数列呈现——CSV 没有
//     无损嵌套表达，明细请走 JSON。

// SessionExport 会话审计链（JSON 形状）。
type SessionExport struct {
	ExportedAt string                    `json:"exported_at"`
	Session    *model.EgoliteSession     `json:"session"`
	Rounds     []*model.EgoliteRound     `json:"rounds"`
	Executions []*model.EgoliteExecution `json:"executions"`
	Events     []*model.EgoEventLog      `json:"events"`
	// ArtifactCount 产物事件数（产物本体在用户本机/storage，这里给的是账本面）
	ArtifactCount int64 `json:"artifact_count"`
}

// ExportSession 导出一条会话审计链（归属校验：userID=0 视为 admin 通道已裁决）。
func ExportSession(ctx context.Context, db *gorm.DB, userID uint, sessionID uint, isAdmin bool) (*SessionExport, error) {
	q := db.WithContext(ctx).Model(&model.EgoliteSession{}).Where("id = ?", sessionID)
	if !isAdmin {
		q = q.Where("user_id = ?", userID)
	}
	var sess model.EgoliteSession
	if err := q.First(&sess).Error; err != nil {
		return nil, ErrSessionNotFound // 同响应防枚举
	}

	out := &SessionExport{ExportedAt: time.Now().UTC().Format(time.RFC3339), Session: &sess}

	if err := db.WithContext(ctx).
		Where("session_id = ?", sessionID).Order("round_no ASC").
		Find(&out.Rounds).Error; err != nil {
		return nil, err
	}
	if err := db.WithContext(ctx).
		Where("id IN (?)", db.Model(&model.EgoliteRound{}).Select("DISTINCT execution_id").
			Where("session_id = ?", sessionID)).
		Order("id ASC").Find(&out.Executions).Error; err != nil {
		return nil, err
	}
	if err := db.WithContext(ctx).
		Where("execution_id IN (?)", db.Model(&model.EgoliteRound{}).Select("DISTINCT execution_id").
			Where("session_id = ?", sessionID)).
		Order("id ASC").Find(&out.Events).Error; err != nil {
		return nil, err
	}
	for _, ev := range out.Events {
		if ev.Type == "artifact" {
			out.ArtifactCount++
		}
	}
	return out, nil
}

// ExportSessionCSV 轮次扁平表：一行一轮，事件/产物以计数列收口。
func ExportSessionCSV(ctx context.Context, db *gorm.DB, userID uint, sessionID uint, isAdmin bool) ([]byte, error) {
	exp, err := ExportSession(ctx, db, userID, sessionID, isAdmin)
	if err != nil {
		return nil, err
	}
	eventsByExec := map[uint]int{}
	artifactsByExec := map[uint]int{}
	for _, ev := range exp.Events {
		eventsByExec[ev.ExecutionID]++
		if ev.Type == "artifact" {
			artifactsByExec[ev.ExecutionID]++
		}
	}

	buf := &strings.Builder{}
	w := csv.NewWriter(buf)
	_ = w.Write([]string{
		"round_no", "execution_id", "ok", "error_code", "duration_ms",
		"actions", "events", "artifacts", "created_at",
	})
	for _, r := range exp.Rounds {
		okCell := ""
		if r.Ok != nil {
			okCell = strconv.FormatBool(*r.Ok)
		}
		_ = w.Write([]string{
			strconv.Itoa(r.RoundNo), strconv.FormatUint(uint64(r.ExecutionID), 10), okCell,
			r.ErrorCode, strconv.FormatInt(r.DurationMs, 10), strconv.Itoa(r.Actions),
			strconv.Itoa(eventsByExec[r.ExecutionID]), strconv.Itoa(artifactsByExec[r.ExecutionID]),
			r.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, fmt.Errorf("CSV 编码失败: %w", err)
	}
	return []byte(buf.String()), nil
}
