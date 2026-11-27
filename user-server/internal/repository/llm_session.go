package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
)

var (
	ErrLLMSessionStateScope    = errors.New("LLM 会话状态范围不完整")
	ErrLLMSessionStateConflict = errors.New("LLM 会话状态版本冲突")
)

type LLMSessionStateQuery struct {
	Platform            string
	ChannelAccountID    uint
	LLMConversationID   string
	ScopeHash           string
	BlindIndexes        []string
	ExecutionScenarioID string
	Provider            string
	Model               string
	Protocol            string
	Region              string
}

type LLMSessionStateRepository interface {
	GetLatest(ctx context.Context, query LLMSessionStateQuery) (*model.LLMSessionState, error)
	Create(ctx context.Context, item *model.LLMSessionState) error
	Update(ctx context.Context, item *model.LLMSessionState) error
	NextGeneration(ctx context.Context, query LLMSessionStateQuery) (int64, error)
	DeleteExpired(ctx context.Context, now time.Time, limit int) (int64, error)
}

type llmSessionStateRepo struct {
	db *gorm.DB
}

func NewLLMSessionStateRepository() LLMSessionStateRepository {
	return &llmSessionStateRepo{db: _db.GetDB()}
}

func NewLLMSessionStateRepositoryWithDB(db *gorm.DB) LLMSessionStateRepository {
	return &llmSessionStateRepo{db: db}
}

func (r *llmSessionStateRepo) database(ctx context.Context) (*gorm.DB, error) {
	if tx, ok := ctx.Value(txContextKey{}).(*gorm.DB); ok && tx != nil {
		return tx.WithContext(ctx), nil
	}
	if r == nil || r.db == nil {
		return nil, ErrChannelStoreUnavailable
	}
	return r.db.WithContext(ctx), nil
}

func (r *llmSessionStateRepo) scoped(ctx context.Context) (*gorm.DB, uint, error) {
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

func normalizeLLMSessionQuery(query LLMSessionStateQuery) (LLMSessionStateQuery, []string, error) {
	query.Platform = strings.TrimSpace(query.Platform)
	query.LLMConversationID = strings.TrimSpace(query.LLMConversationID)
	query.ScopeHash = strings.TrimSpace(query.ScopeHash)
	query.ExecutionScenarioID = strings.TrimSpace(query.ExecutionScenarioID)
	query.Provider = strings.TrimSpace(query.Provider)
	query.Model = strings.TrimSpace(query.Model)
	query.Protocol = strings.TrimSpace(query.Protocol)
	query.Region = strings.TrimSpace(query.Region)
	blinds := make([]string, 0, len(query.BlindIndexes))
	seen := make(map[string]bool, len(query.BlindIndexes))
	for _, blind := range query.BlindIndexes {
		blind = strings.TrimSpace(blind)
		if blind == "" || seen[blind] {
			continue
		}
		seen[blind] = true
		blinds = append(blinds, blind)
	}
	if query.Platform == "" || query.ChannelAccountID == 0 || query.LLMConversationID == "" || query.ScopeHash == "" ||
		len(blinds) == 0 || query.ExecutionScenarioID == "" || query.Provider == "" || query.Model == "" ||
		query.Protocol == "" || query.Region == "" {
		return LLMSessionStateQuery{}, nil, ErrLLMSessionStateScope
	}
	query.BlindIndexes = blinds
	return query, blinds, nil
}

func applyLLMSessionScope(query *gorm.DB, sessionQuery LLMSessionStateQuery, blinds []string) *gorm.DB {
	return query.
		Where("platform = ?", sessionQuery.Platform).
		Where("channel_account_id = ?", sessionQuery.ChannelAccountID).
		Where("llm_conversation_id = ?", sessionQuery.LLMConversationID).
		Where("scope_hash = ?", sessionQuery.ScopeHash).
		Where("bus_llm_session_id_blind IN ?", blinds).
		Where("execution_scenario_id = ?", sessionQuery.ExecutionScenarioID).
		Where("provider = ?", sessionQuery.Provider).
		Where("model = ?", sessionQuery.Model).
		Where("protocol = ?", sessionQuery.Protocol).
		Where("region = ?", sessionQuery.Region)
}

func (r *llmSessionStateRepo) GetLatest(ctx context.Context, sessionQuery LLMSessionStateQuery) (*model.LLMSessionState, error) {
	sessionQuery, blinds, err := normalizeLLMSessionQuery(sessionQuery)
	if err != nil {
		return nil, err
	}
	query, _, err := r.scoped(ctx)
	if err != nil {
		return nil, err
	}
	var item model.LLMSessionState
	err = applyLLMSessionScope(query, sessionQuery, blinds).Order("generation DESC").First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) || err == nil && item.ID == 0 {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *llmSessionStateRepo) Create(ctx context.Context, item *model.LLMSessionState) error {
	if item == nil {
		return errors.New("LLM 会话状态为空")
	}
	_, tid, err := r.scoped(ctx)
	if err != nil {
		return err
	}
	item.TenantID = tid
	if item.Generation <= 0 {
		item.Generation = 1
	}
	if item.Version <= 0 {
		item.Version = 1
	}
	if item.RetentionMode == "" {
		item.RetentionMode = model.LLMSessionRetentionStateless
	}
	database, err := r.database(ctx)
	if err != nil {
		return err
	}
	if err := database.Create(item).Error; err != nil {
		if isDuplicateKeyErr(err) {
			return ErrLLMSessionStateConflict
		}
		return err
	}
	return nil
}

func (r *llmSessionStateRepo) Update(ctx context.Context, item *model.LLMSessionState) error {
	if item == nil || item.ID == 0 {
		return ErrLLMSessionStateScope
	}
	query, tid, err := r.scoped(ctx)
	if err != nil {
		return err
	}
	item.TenantID = tid
	expectedVersion := item.Version
	if expectedVersion <= 0 {
		expectedVersion = 1
	}
	result := query.Model(&model.LLMSessionState{}).
		Where("id = ? AND version = ?", item.ID, expectedVersion).
		Updates(map[string]any{
			"dispatch_scenario":             item.DispatchScenario,
			"provider_continuation_ref_enc": item.ProviderContinuationRefEnc,
			"last_message_id":               item.LastMessageID,
			"last_message_cursor":           item.LastMessageCursor,
			"replied_through_cursor":        item.RepliedThroughCursor,
			"prompt_prefix_version":         item.PromptPrefixVersion,
			"context_schema_version":        item.ContextSchemaVersion,
			"capability_fingerprint":        item.CapabilityFingerprint,
			"retention_mode":                item.RetentionMode,
			"expires_at":                    item.ExpiresAt,
			"last_used_at":                  item.LastUsedAt,
			"version":                       expectedVersion + 1,
			"updated_at":                    time.Now(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrLLMSessionStateConflict
	}
	item.Version = expectedVersion + 1
	return nil
}

func (r *llmSessionStateRepo) NextGeneration(ctx context.Context, sessionQuery LLMSessionStateQuery) (int64, error) {
	sessionQuery, blinds, err := normalizeLLMSessionQuery(sessionQuery)
	if err != nil {
		return 0, err
	}
	query, _, err := r.scoped(ctx)
	if err != nil {
		return 0, err
	}
	var latest []model.LLMSessionState
	if err := applyLLMSessionScope(query, sessionQuery, blinds).
		Select("generation").Order("generation DESC").Limit(1).Find(&latest).Error; err != nil {
		return 0, err
	}
	if len(latest) == 0 || latest[0].Generation < 1 {
		return 1, nil
	}
	return latest[0].Generation + 1, nil
}

func (r *llmSessionStateRepo) DeleteExpired(ctx context.Context, now time.Time, limit int) (int64, error) {
	query, _, err := r.scoped(ctx)
	if err != nil {
		return 0, err
	}
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	subquery := query.Session(&gorm.Session{NewDB: true}).
		Model(&model.LLMSessionState{}).
		Select("id").
		Where("expires_at <= ?", now).
		Order("expires_at ASC").
		Limit(limit)
	result := query.Where("id IN (?)", subquery).Delete(&model.LLMSessionState{})
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}
