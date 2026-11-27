package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/secrets"

	"gorm.io/gorm"
)

var ErrAgentTranscriptConflict = errors.New("执行账本幂等或版本冲突")

type AgentTranscriptRepository interface {
	CreateTranscript(ctx context.Context, entry *model.ScenarioTranscript, payload string) error
	ListTranscripts(ctx context.Context, sessionID uint, generation int64, fromSequence int64, limit int) ([]model.ScenarioTranscript, error)
	CreateToolInvocation(ctx context.Context, item *model.ToolInvocation, args string) error
	SettleToolInvocation(ctx context.Context, item *model.ToolInvocation, expectedStatus, result string) error
	DeleteExpired(ctx context.Context, now time.Time, limit int) (int64, error)
}

type agentTranscriptRepo struct {
	db *gorm.DB
}

func NewAgentTranscriptRepository() AgentTranscriptRepository {
	return &agentTranscriptRepo{db: _db.GetDB()}
}

func NewAgentTranscriptRepositoryWithDB(db *gorm.DB) AgentTranscriptRepository {
	return &agentTranscriptRepo{db: db}
}

func (r *agentTranscriptRepo) database(ctx context.Context) (*gorm.DB, error) {
	if tx, ok := ctx.Value(txContextKey{}).(*gorm.DB); ok && tx != nil {
		return tx.WithContext(ctx), nil
	}
	if r == nil || r.db == nil {
		return nil, ErrChannelStoreUnavailable
	}
	return r.db.WithContext(ctx), nil
}

func (r *agentTranscriptRepo) scoped(ctx context.Context) (*gorm.DB, uint, error) {
	database, err := r.database(ctx)
	if err != nil {
		return nil, 0, err
	}
	tid, ok := tenant.FromContext(ctx)
	if !ok || tid == 0 {
		if tenant.IsMultiTenant() {
			return nil, 0, ErrLeadTenantRequired
		}
		tid = tenant.DefaultTenantID
	}
	return database.Where("tenant_id = ?", tid), tid, nil
}

func agentTranscriptHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func validTranscriptEntryType(value string) bool {
	switch value {
	case model.TranscriptModelStep, model.TranscriptToolCall, model.TranscriptToolResult,
		model.TranscriptHandoff, model.TranscriptCompaction:
		return true
	default:
		return false
	}
}

func validToolStatus(value string) bool {
	switch value {
	case model.ToolInvocationPlanned, model.ToolInvocationAuthorized, model.ToolInvocationDispatched,
		model.ToolInvocationUnknown, model.ToolInvocationConfirmed:
		return true
	default:
		return false
	}
}

func (r *agentTranscriptRepo) CreateTranscript(ctx context.Context, entry *model.ScenarioTranscript, payload string) error {
	if entry == nil {
		return errors.New("执行片段为空")
	}
	scope, tid, err := r.scoped(ctx)
	if err != nil {
		return err
	}
	entry.TenantID = tid
	entry.SnapshotID = strings.TrimSpace(entry.SnapshotID)
	entry.ExecutionScenarioID = strings.TrimSpace(entry.ExecutionScenarioID)
	entry.EntryType = strings.TrimSpace(entry.EntryType)
	entry.RequestIdempotencyKey = strings.TrimSpace(entry.RequestIdempotencyKey)
	if entry.LLMSessionStateID == 0 || entry.SnapshotID == "" || entry.ExecutionScenarioID == "" ||
		entry.SequenceNo <= 0 || !validTranscriptEntryType(entry.EntryType) || entry.RequestIdempotencyKey == "" {
		return errors.New("执行片段范围或标识不完整")
	}
	if len(payload) == 0 || entry.ExpiresAt.IsZero() || !entry.ExpiresAt.After(time.Now()) {
		return errors.New("执行片段载荷为空或 TTL 已失效")
	}
	if entry.Generation <= 0 {
		entry.Generation = 1
	}
	encrypted, err := secrets.EncryptString(payload)
	if err != nil {
		return err
	}
	entry.PayloadEnc = []byte(encrypted)
	entry.PayloadHash = agentTranscriptHash(payload)
	if err := scope.Create(entry).Error; err != nil {
		if isDuplicateKeyErr(err) {
			return ErrAgentTranscriptConflict
		}
		return err
	}
	return nil
}

func (r *agentTranscriptRepo) ListTranscripts(ctx context.Context, sessionID uint, generation int64, fromSequence int64, limit int) ([]model.ScenarioTranscript, error) {
	scope, _, err := r.scoped(ctx)
	if err != nil {
		return nil, err
	}
	if sessionID == 0 || generation <= 0 || fromSequence < 0 {
		return nil, errors.New("执行片段查询范围不完整")
	}
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	rows := make([]model.ScenarioTranscript, 0)
	err = scope.Where("llm_session_state_id = ? AND generation = ? AND sequence_no >= ?", sessionID, generation, fromSequence).
		Order("sequence_no ASC").Limit(limit).Find(&rows).Error
	return rows, err
}

func (r *agentTranscriptRepo) CreateToolInvocation(ctx context.Context, item *model.ToolInvocation, args string) error {
	if item == nil {
		return errors.New("工具调用为空")
	}
	scope, tid, err := r.scoped(ctx)
	if err != nil {
		return err
	}
	item.TenantID = tid
	item.ToolName = strings.TrimSpace(item.ToolName)
	item.ToolIdempotencyKey = strings.TrimSpace(item.ToolIdempotencyKey)
	item.RiskClass = strings.TrimSpace(item.RiskClass)
	item.Status = strings.TrimSpace(item.Status)
	item.AuthorizationHash = strings.TrimSpace(item.AuthorizationHash)
	item.RequestHash = strings.TrimSpace(item.RequestHash)
	if item.TranscriptID == 0 || item.ToolName == "" || item.AuthorizationHash == "" || item.RequestHash == "" ||
		len(args) == 0 || !validToolStatus(item.Status) {
		return errors.New("工具调用范围或证据不完整")
	}
	switch item.RiskClass {
	case model.ToolRiskRead, model.ToolRiskWrite:
	case model.ToolRiskHighWrite:
		if item.ToolIdempotencyKey == "" {
			return errors.New("高副作用工具必须携带幂等键")
		}
	default:
		return errors.New("未知工具风险等级")
	}
	if item.AttemptCount <= 0 {
		item.AttemptCount = 1
	}
	if err := scope.Create(item).Error; err != nil {
		if !isDuplicateKeyErr(err) {
			return err
		}
		if item.ToolIdempotencyKey == "" {
			return ErrAgentTranscriptConflict
		}
		var existing model.ToolInvocation
		if err := scope.Where("tenant_id = ? AND tool_idempotency_key = ?", tid, item.ToolIdempotencyKey).First(&existing).Error; err != nil {
			return ErrAgentTranscriptConflict
		}
		if existing.RequestHash != item.RequestHash {
			return ErrAgentTranscriptConflict
		}
		item.ID = existing.ID
		item.Status = existing.Status
		item.AttemptCount = existing.AttemptCount
		return nil
	}
	return nil
}

func (r *agentTranscriptRepo) SettleToolInvocation(ctx context.Context, item *model.ToolInvocation, expectedStatus, result string) error {
	if item == nil {
		return errors.New("工具调用为空")
	}
	scope, _, err := r.scoped(ctx)
	if err != nil {
		return err
	}
	if item.ID == 0 || item.TranscriptID == 0 || !validToolStatus(expectedStatus) || !validToolStatus(item.Status) {
		return errors.New("工具调用结算状态不完整")
	}
	if expectedStatus == model.ToolInvocationUnknown && item.RiskClass == model.ToolRiskHighWrite && item.Status == model.ToolInvocationConfirmed {
		return ErrAgentTranscriptConflict
	}
	encrypted, err := secrets.EncryptString(result)
	if err != nil {
		return err
	}
	resultHash := agentTranscriptHash(result)
	item.ResultEnc = []byte(encrypted)
	item.ResultHash = resultHash
	item.LastErrorCode = ""
	updates := map[string]any{
		"status":              item.Status,
		"result_enc":          []byte(encrypted),
		"result_hash":         resultHash,
		"external_receipt_id": item.ExternalReceiptID,
		"last_error_code":     "",
		"updated_at":          time.Now(),
	}
	res := scope.Model(&model.ToolInvocation{}).Where("tenant_id = ? AND id = ? AND status = ?", item.TenantID, item.ID, expectedStatus).
		Updates(updates)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrAgentTranscriptConflict
	}
	return nil
}

func (r *agentTranscriptRepo) DeleteExpired(ctx context.Context, now time.Time, limit int) (int64, error) {
	scope, tid, err := r.scoped(ctx)
	if err != nil {
		return 0, err
	}
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	expiredIDs := scope.Session(&gorm.Session{NewDB: true}).
		Model(&model.ScenarioTranscript{}).
		Select("id").
		Where("tenant_id = ? AND expires_at <= ?", tid, now).
		Limit(limit)
	res := scope.Where("id IN (?)", expiredIDs).Delete(&model.ScenarioTranscript{})
	if res.Error != nil {
		return 0, res.Error
	}
	if res.RowsAffected == 0 {
		return 0, nil
	}
	toolScope, _, err := r.scoped(ctx)
	if err != nil {
		return 0, err
	}
	expiredTranscriptIDs := toolScope.Session(&gorm.Session{NewDB: true}).
		Model(&model.ScenarioTranscript{}).
		Select("id").
		Where("tenant_id = ? AND expires_at <= ?", tid, now)
	if err := toolScope.Where("transcript_id IN (?)", expiredTranscriptIDs).Delete(&model.ToolInvocation{}).Error; err != nil {
		return 0, err
	}
	return res.RowsAffected, nil
}
