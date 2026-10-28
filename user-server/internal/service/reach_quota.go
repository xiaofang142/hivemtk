// reach_quota.go 触达硬配额管理（D05/F03）——服务层编排。
//
// 真源在 PG（计数行锁），Redis 只管 QPS；Redis 不可用不退化成无限放行。
// 多 scope 预占按 key 排序后逐个 Reserve（锁顺序确定，避免死锁）；
// commit/release/unknown 按 intent 作用于其全部预占行。
package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/repository"
)

// QuotaScopeDef 一个预占维度（key 归一化后须稳定；limit<=0 表示该维度不限）
type QuotaScopeDef struct {
	Key   string
	Limit int
}

// QuotaReserver 配额预占接口（挂发送管线）
type QuotaReserver interface {
	ReserveIntent(ctx context.Context, intentID, payloadHash string, scopes []QuotaScopeDef) (token string, reused bool, err error)
	CommitIntent(ctx context.Context, intentID, token string) error
	ReleaseIntent(ctx context.Context, intentID, token string) error
	MarkUnknownIntent(ctx context.Context, intentID, token string) error
}

// ReachQuotaManager 默认实现（单仓储；多 scope 惰性合并）
type ReachQuotaManager struct {
	repo  *repository.ReachQuotaRepository
	clock func() time.Time
}

// NewReachQuotaManager 构造
func NewReachQuotaManager(repo *repository.ReachQuotaRepository) *ReachQuotaManager {
	return &ReachQuotaManager{repo: repo, clock: time.Now}
}

// SetClockForTest 测试注入时钟（自然日归属 CST）
func (m *ReachQuotaManager) SetClockForTest(fn func() time.Time) {
	m.clock = fn
}

func (m *ReachQuotaManager) day() string {
	return m.clock().In(cstZone).Format("2006-01-02")
}

// PayloadHash 请求载荷哈希（意图幂等判定用：同 intent 不同载荷 = 冲突）
func ReachPayloadHash(req *ReachSendRequest) string {
	raw, err := json.Marshal(req)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// ReserveIntent 多 scope 预占：按 key 排序逐个 Reserve（确定锁序）。
// 任一 scope 超限即整体失败且不产生半份预占——单 scope 实现下每个 scope
// 是独立意图行（intent#scope），失败的 scope 不会留下已用额度。
func (m *ReachQuotaManager) ReserveIntent(ctx context.Context, intentID, payloadHash string, scopes []QuotaScopeDef) (string, bool, error) {
	if m.repo == nil {
		return "", false, fmt.Errorf("reach quota manager not initialized")
	}
	if len(scopes) == 0 {
		return "", true, nil // 无维度 = 不设限，直接放行（reused 语义：无需再 commit）
	}
	sorted := make([]QuotaScopeDef, len(scopes))
	copy(sorted, scopes)
	for i := 1; i < len(sorted); i++ { // 插入排序（scope 数量小）
		for j := i; j > 0 && sorted[j].Key < sorted[j-1].Key; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	day := m.day()
	firstToken := ""
	reused := true
	for _, sc := range sorted {
		res, err := m.repo.Reserve(ctx, sc.Key, day, quotaIntentKey(intentID, sc.Key), payloadHash, sc.Limit)
		if err != nil {
			// 失败回滚已占 scope：确定未发送（预占≠发送），可安全释放
			if firstToken != "" {
				_ = m.rollbackScopes(ctx, intentID, sorted, sc.Key)
			}
			return "", false, err
		}
		if firstToken == "" {
			firstToken = res.Token
		}
		if !res.Reused {
			reused = false
		}
	}
	return firstToken, reused, nil
}

// rollbackScopes 释放排序在 limitKey 之前（含全部，当 limitKey 为空串）的预占
func (m *ReachQuotaManager) rollbackScopes(ctx context.Context, intentID string, scopes []QuotaScopeDef, beforeKey string) error {
	for _, sc := range scopes {
		if beforeKey != "" && sc.Key >= beforeKey {
			break
		}
		rec, err := m.repo.GetReservation(ctx, quotaIntentKey(intentID, sc.Key))
		if err != nil || rec == nil {
			continue
		}
		_ = m.repo.ReleaseIntent(ctx, rec.IntentID, rec.Token)
	}
	return nil
}

// verifyToken 校验调用方令牌 = 预占时返回的令牌（第一行）。伪令牌/串账在此拒绝，
// 各 scope 行内部的回写仍由仓储层逐行校验自己的 token。
func (m *ReachQuotaManager) verifyToken(rows []quotaIntentRef, token string) error {
	if len(rows) == 0 {
		return nil // 无 scope 行：无事可做（Reserve 时未设维度）
	}
	if rows[0].Token != token {
		return repository.ErrQuotaTokenMismatch
	}
	return nil
}

// CommitIntent 成功回执：全部 scope 提交（幂等）
func (m *ReachQuotaManager) CommitIntent(ctx context.Context, intentID, token string) error {
	recs, err := m.intentReservations(ctx, intentID)
	if err != nil {
		return err
	}
	if err := m.verifyToken(recs, token); err != nil {
		return err
	}
	for _, rec := range recs {
		if err := m.repo.CommitIntent(ctx, rec.IntentID, rec.Token); err != nil {
			return err
		}
	}
	return nil
}

// ReleaseIntent 确定未发送：全部 scope 释放并返额
func (m *ReachQuotaManager) ReleaseIntent(ctx context.Context, intentID, token string) error {
	recs, err := m.intentReservations(ctx, intentID)
	if err != nil {
		return err
	}
	if err := m.verifyToken(recs, token); err != nil {
		return err
	}
	for _, rec := range recs {
		if err := m.repo.ReleaseIntent(ctx, rec.IntentID, rec.Token); err != nil {
			return err
		}
	}
	return nil
}

// MarkUnknownIntent 结果未知：全部 scope 保留额度待核对
func (m *ReachQuotaManager) MarkUnknownIntent(ctx context.Context, intentID, token string) error {
	recs, err := m.intentReservations(ctx, intentID)
	if err != nil {
		return err
	}
	if err := m.verifyToken(recs, token); err != nil {
		return err
	}
	for _, rec := range recs {
		if err := m.repo.MarkUnknownIntent(ctx, rec.IntentID, rec.Token); err != nil {
			return err
		}
	}
	return nil
}

func (m *ReachQuotaManager) intentReservations(ctx context.Context, intentID string) ([]quotaIntentRef, error) {
	if m.repo == nil {
		return nil, fmt.Errorf("reach quota manager not initialized")
	}
	// intent 行按 (intent#scope) 键存储；这里通过前缀扫描还原全部 scope 行
	rows, err := m.repo.ListByIntentPrefix(ctx, intentID+"#")
	if err != nil {
		return nil, err
	}
	out := make([]quotaIntentRef, 0, len(rows))
	for _, row := range rows {
		out = append(out, quotaIntentRef{IntentID: row.IntentID, Token: row.Token})
	}
	return out, nil
}

type quotaIntentRef struct {
	IntentID string
	Token    string
}

func quotaIntentKey(intentID, scope string) string {
	return intentID + "#" + scope
}

// 建表登记：预占账与计数行随启动期 AutoMigrate 建（RegisterExtraModels 机制同上）。
func init() {
	_db.RegisterExtraModels(&model.ReachQuotaCounter{}, &model.ReachQuotaReservation{})
}
