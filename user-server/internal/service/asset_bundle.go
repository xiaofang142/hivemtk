package service

import (
	"context"

	"encoding/json"

	"errors"

	"fmt"

	"regexp"

	"sort"

	"strings"

	"sync"

	"time"

	"hivemtk-user/internal/dto"

	"hivemtk-user/internal/model"

	"hivemtk-user/internal/pkg/utils/logger"

	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/safeprompt"
	"strconv"
)

type WeaveInput struct {
	Asset *model.AssetBundle

	UserQuery string

	RAGDocs []RAGDocument

	ChatHistory []model.AssetBundleMessage

	MerchantVars map[string]string

	Options WeaveOptions

	Sandbox bool
}

type RAGDocument struct {
	ID      string
	Title   string
	Content string
	Score   float64
	Source  string
}

type WeaveOptions struct {
	RAGPosition         RAGInsertPosition
	MaxHistoryMessages  int
	StripFewShotJSON    bool
	IncludeMerchantVars bool
}

type RAGInsertPosition string

const (
	RAGPositionAfterSystem RAGInsertPosition = "after_system"

	RAGPositionAfterFewShots RAGInsertPosition = "after_fewshots"
)

func DefaultWeaveOptions() WeaveOptions {
	return WeaveOptions{
		RAGPosition:         RAGPositionAfterFewShots,
		MaxHistoryMessages:  10,
		StripFewShotJSON:    true,
		IncludeMerchantVars: true,
	}
}

func Weave(in WeaveInput) ([]model.AssetBundleMessage, error) {
	if in.Asset == nil {
		return nil, errors.New("weave: asset bundle is nil")
	}
	if in.UserQuery == "" {
		return nil, errors.New("weave: user query is empty")
	}
	if len(in.Asset.Messages) == 0 {
		return nil, errors.New("weave: asset bundle has no messages")
	}
	hasSystem := false
	for _, m := range in.Asset.Messages {
		if m.Role == "system" {
			hasSystem = true
			break
		}
	}
	if !hasSystem {
		return nil, errors.New("weave: asset bundle must contain at least one role=system message")
	}
	opts := in.Options
	if opts.MaxHistoryMessages == 0 && !opts.StripFewShotJSON && !opts.IncludeMerchantVars && opts.RAGPosition == "" {
		opts = DefaultWeaveOptions()
	}
	if opts.MaxHistoryMessages < 0 {
		opts.MaxHistoryMessages = 0
	}

	result := make([]model.AssetBundleMessage, 0, len(in.Asset.Messages)+8)
	stripped := false
	for _, m := range in.Asset.Messages {
		msg := m
		if opts.StripFewShotJSON && msg.Role != "system" {
			if cleaned, ok := stripTrailingJSONBlock(msg.Content); ok {
				msg.Content = cleaned
				stripped = true
			}
		}
		result = append(result, msg)
	}

	if opts.IncludeMerchantVars && len(in.MerchantVars) > 0 {
		injectMerchantVars(result, in.MerchantVars)
	}

	ragMsgs := buildRAGMessages(in.RAGDocs)
	if len(ragMsgs) > 0 {
		switch opts.RAGPosition {
		case RAGPositionAfterSystem, "":
			result = insertAfterSystem(result, ragMsgs)
		case RAGPositionAfterFewShots:
			result = append(result, ragMsgs...)
		default:
			result = append(result, ragMsgs...)
		}
	}

	if len(in.ChatHistory) > 0 {
		hist := in.ChatHistory
		if opts.MaxHistoryMessages > 0 && len(hist) > opts.MaxHistoryMessages {
			hist = hist[len(hist)-opts.MaxHistoryMessages:]
		}
		result = append(result, hist...)
	}

	result = append(result, model.AssetBundleMessage{
		Role:    "user",
		Content: in.UserQuery,
	})

	logger.Debugf("[weave] asset=%s rag=%d hist=%d merchant=%d result_len=%d stripped=%v",
		in.Asset.AssetID, len(in.RAGDocs), len(in.ChatHistory),
		len(in.MerchantVars), len(result), stripped)
	return result, nil
}

func buildRAGMessages(docs []RAGDocument) []model.AssetBundleMessage {
	if len(docs) == 0 {
		return nil
	}
	var sb strings.Builder
	sb.WriteString("# 实时验证知识库上下文\n")
	sb.WriteString("请结合以下商户本地的实时参考数据，精准、诚实地回答用户后续的提问，不要凭空编造事实：\n\n")
	for i, d := range docs {
		fmt.Fprintf(&sb, "[商户本地官方参数 %d] (来源=%s, 相关度=%.2f)\n%s\n\n", i+1, d.Source, d.Score, d.Content)
	}
	return []model.AssetBundleMessage{
		{Role: "system", Content: sb.String()},
	}
}

func insertAfterSystem(msgs []model.AssetBundleMessage, inserts []model.AssetBundleMessage) []model.AssetBundleMessage {
	for i, m := range msgs {
		if m.Role == "system" {
			result := make([]model.AssetBundleMessage, 0, len(msgs)+len(inserts))
			result = append(result, msgs[:i+1]...)
			result = append(result, inserts...)
			result = append(result, msgs[i+1:]...)
			return result
		}
	}
	return append(msgs, inserts...)
}

func injectMerchantVars(msgs []model.AssetBundleMessage, vars map[string]string) {
	if len(msgs) == 0 || len(vars) == 0 {
		return
	}
	firstSystemIdx := -1
	for i, m := range msgs {
		if m.Role == "system" {
			firstSystemIdx = i
			break
		}
	}
	if firstSystemIdx < 0 {
		msgs = append(msgs, model.AssetBundleMessage{Role: "system", Content: ""})
		firstSystemIdx = len(msgs) - 1
	}
	var sb strings.Builder
	sb.WriteString(msgs[firstSystemIdx].Content)
	sb.WriteString("\n\n# 商户经营参数（自动注入）\n")

	whitelist := []string{"shop_name", "campaign_name", "discount_pct", "support_contact"}
	others := make([]string, 0, len(vars))
	for k, v := range vars {
		if containsKey(whitelist, k) || v == "" {
			continue
		}
		others = append(others, k)
	}
	sort.Strings(others)
	for _, k := range whitelist {
		if v, ok := vars[k]; ok && v != "" {
			fmt.Fprintf(&sb, "- %s: %s\n", k, v)
		}
	}
	for _, k := range others {
		fmt.Fprintf(&sb, "- %s: %s\n", k, vars[k])
	}
	msgs[firstSystemIdx].Content = sb.String()
}

func containsKey(keys []string, target string) bool {
	for _, k := range keys {
		if k == target {
			return true
		}
	}
	return false
}

var codeBlockRE = regexp.MustCompile("(?s)```(?:json|JSON)?[\\s\\S]*?```")

func stripTrailingJSONBlock(content string) (string, bool) {
	matches := codeBlockRE.FindAllStringIndex(content, -1)
	if len(matches) == 0 {
		return content, false
	}
	last := matches[len(matches)-1]
	if last[1] != len(content) {
		after := strings.TrimSpace(content[last[1]:])
		if after != "" {
			return content, false
		}
	}
	first := matches[0]
	stripped := content[:first[0]] + content[last[1]:]
	return strings.TrimRight(stripped, "\n"), true
}

type AssetBundleService struct {
	repo        repository.AssetBundleRepository
	version     repository.AssetBundleVersionLogRepository
	hotPlug     hotPlugCache
	localLoader LocalAssetLoader
}

func NewAssetBundleService(repo repository.AssetBundleRepository, version repository.AssetBundleVersionLogRepository) *AssetBundleService {
	s := &AssetBundleService{repo: repo, hotPlug: newHotPlugCache()}
	s.applyVersionLogRepo(version)
	return s
}

// WithVersionLogRepo 显式注入版本日志仓储。
//
// K-3：版本审计必须真实持久化——不再提供仅 Debug 日志的 stub；
// 构造函数传 nil 时回退全局 DB 的真实仓储（保持既有签名兼容调用方），
// 测试/特殊装配场景用本方法显式注入。
func (s *AssetBundleService) WithVersionLogRepo(v repository.AssetBundleVersionLogRepository) *AssetBundleService {
	s.applyVersionLogRepo(v)
	return s
}

func (s *AssetBundleService) applyVersionLogRepo(v repository.AssetBundleVersionLogRepository) {
	if v != nil {
		s.version = v
		return
	}
	s.version = nil
	logger.Warnf("[asset_bundle] version log repo not injected (nil): version audit DISABLED")
}

type LocalAssetLoader interface {
	LoadOne(ctx context.Context, assetID string) (*model.LocalAsset, []byte, error)
}

func (s *AssetBundleService) SetLocalLoader(l LocalAssetLoader) {
	s.localLoader = l
}

func (s *AssetBundleService) SetConfigKVRepo(kv repository.SystemConfigKVRepository) {
	s.hotPlug.SetConfigKVRepo(kv)
}

func (s *AssetBundleService) CreateBundle(ctx context.Context, m *model.AssetBundle) error {
	if m == nil {
		return errors.New("bundle is nil")
	}
	if m.AssetID == "" {
		return errors.New("asset_id required")
	}
	if m.Title == "" {
		return errors.New("title required")
	}
	if len(m.Messages) == 0 {
		return errors.New("messages required (at least system prompt)")
	}
	hasSystem := false
	for _, msg := range m.Messages {
		if msg.Role == "system" {
			hasSystem = true
			break
		}
	}
	if !hasSystem {
		return errors.New("messages must contain at least one role=system")
	}
	if m.Status == "" {
		m.Status = model.AssetBundleStatusDraft
	}
	if m.Version == "" {
		m.Version = "1.0.0"
	}
	if m.Scope == "" {
		m.Scope = model.AssetBundleScopePrivate
	}
	if m.Language == "" {
		m.Language = "zh"
	}
	exists, err := s.repo.ExistsByAssetID(ctx, m.AssetID)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("asset_id %s already exists", m.AssetID)
	}
	return s.repo.Create(ctx, m)
}

func (s *AssetBundleService) UpdateBundle(ctx context.Context, m *model.AssetBundle) error {
	if m == nil || m.ID == 0 {
		return errors.New("bundle id required")
	}
	old, err := s.repo.FindByID(ctx, m.ID)
	if err != nil {
		return err
	}

	if m.AssetID == "" {
		m.AssetID = old.AssetID
	}
	if old.AssetID != m.AssetID {
		return errors.New("asset_id cannot be changed")
	}

	if m.Title == "" {
		m.Title = old.Title
	}
	if m.Status == "" {
		m.Status = old.Status
	}
	if m.Version == "" {
		m.Version = old.Version
	}
	if m.Scope == "" {
		m.Scope = old.Scope
	}
	if m.Author == "" {
		m.Author = old.Author
	}
	if m.Description == "" {
		m.Description = old.Description
	}
	if m.Industry == "" {
		m.Industry = old.Industry
	}
	if m.Language == "" {
		m.Language = old.Language
	}
	if len(m.Tags) == 0 {
		m.Tags = old.Tags
	}
	if len(m.Messages) == 0 {
		m.Messages = old.Messages
	}
	if err := s.repo.Update(ctx, m); err != nil {
		return err
	}
	if old.Version != m.Version {
		if s.version == nil {
			logger.Errorf("[asset_bundle] version log repo missing: version change NOT audited asset=%s %s->%s operator=%s",
				m.AssetID, old.Version, m.Version, m.Author)
		} else if err := s.version.Create(ctx, &model.AssetBundleVersionLog{
			AssetID:    m.AssetID,
			FromVer:    old.Version,
			ToVer:      m.Version,
			ChangeNote: "manual update",
			Operator:   m.Author,
		}); err != nil {

			logger.Errorf("[asset_bundle] version log write FAILED asset=%s %s->%s operator=%s err=%v",
				m.AssetID, old.Version, m.Version, m.Author, err)
		}
	}
	return nil
}

func (s *AssetBundleService) PublishBundle(ctx context.Context, id int64) error {
	m, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return err
	}
	m.Status = model.AssetBundleStatusActive
	return s.repo.Update(ctx, m)
}

func (s *AssetBundleService) ArchiveBundle(ctx context.Context, id int64) error {
	m, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return err
	}
	m.Status = model.AssetBundleStatusArchived
	return s.repo.Update(ctx, m)
}

var ErrBundleNotHotEnabled = errors.New("asset bundle is not hot-enabled (call enable API first)")

func (s *AssetBundleService) EnableBundle(ctx context.Context, id int64) (*model.AssetBundle, error) {
	m, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	s.hotPlug.add(ctx, m.AssetID)
	logger.Debugf("[asset_bundle] hot-enable: asset_id=%s id=%d", m.AssetID, id)
	return m, nil
}

func (s *AssetBundleService) DisableBundle(ctx context.Context, id int64) (*model.AssetBundle, error) {
	m, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	s.hotPlug.remove(ctx, m.AssetID)
	logger.Debugf("[asset_bundle] hot-disable: asset_id=%s id=%d", m.AssetID, id)
	return m, nil
}

func (s *AssetBundleService) GetEnabledBundles(ctx context.Context) ([]*model.AssetBundle, error) {
	ids := s.hotPlug.list(ctx)
	out := make([]*model.AssetBundle, 0, len(ids))
	for _, aid := range ids {
		b, err := s.repo.FindByAssetID(ctx, aid)
		if err != nil {
			logger.Debugf("[asset_bundle] hot-enabled bundle load failed (skip): asset_id=%s err=%v", aid, err)
			continue
		}
		out = append(out, b)
	}
	return out, nil
}

func (s *AssetBundleService) IsBundleEnabled(ctx context.Context, assetID string) bool {
	return s.hotPlug.has(ctx, assetID)
}

func (s *AssetBundleService) DeleteBundle(ctx context.Context, id int64) error {
	return s.repo.SoftDelete(ctx, id)
}

func (s *AssetBundleService) GetBundle(ctx context.Context, id int64) (*model.AssetBundle, error) {
	return s.repo.FindByID(ctx, id)
}

func (s *AssetBundleService) GetBundleByAssetID(ctx context.Context, assetID string) (*model.AssetBundle, error) {
	return s.repo.FindByAssetID(ctx, assetID)
}

func (s *AssetBundleService) ResolveSystemPrompt(ctx context.Context, assetBundleID string) (string, error) {
	if assetBundleID == "" {
		return "", fmt.Errorf("asset_bundle_id empty")
	}
	bundle, err := s.repo.FindByAssetID(ctx, assetBundleID)
	if err == nil && bundle != nil {
		return systemPromptFromMessages(bundle.Messages), nil
	}
	if s.localLoader != nil {
		la, data, lerr := s.localLoader.LoadOne(ctx, assetBundleID)
		if lerr == nil && la != nil && len(data) > 0 {
			if p := resolveLocalAssetSystemPrompt(data); p != "" {
				return p, nil
			}
		}
	}
	return "", fmt.Errorf("asset bundle not found: %s", assetBundleID)
}

func systemPromptFromMessages(msgs []model.AssetBundleMessage) string {
	var sb strings.Builder
	for _, m := range msgs {
		if m.Role == "system" && strings.TrimSpace(m.Content) != "" {
			if sb.Len() > 0 {
				sb.WriteString("\n\n")
			}
			sb.WriteString(m.Content)
		}
	}
	return sb.String()
}

func parseChatML(data []byte) ([]model.AssetBundleMessage, error) {
	var msgs []model.AssetBundleMessage
	if err := json.Unmarshal(data, &msgs); err != nil {
		return nil, err
	}
	return msgs, nil
}

func resolveLocalAssetSystemPrompt(data []byte) string {
	if msgs, err := parseChatML(data); err == nil {
		if p := systemPromptFromMessages(msgs); p != "" {
			return p
		}
	}

	var obj struct {
		SystemPrompt string `json:"system_prompt"`
		Persona      struct {
			Tone      string   `json:"tone"`
			Expertise []string `json:"expertise"`
			Forbidden []string `json:"forbidden"`
		} `json:"persona"`
	}
	if err := json.Unmarshal(data, &obj); err == nil {
		if sp := strings.TrimSpace(obj.SystemPrompt); sp != "" {
			return sp
		}
		if obj.Persona.Tone != "" || len(obj.Persona.Expertise) > 0 || len(obj.Persona.Forbidden) > 0 {
			var b strings.Builder
			if obj.Persona.Tone != "" {
				b.WriteString("人设基调：" + obj.Persona.Tone + "。")
			}
			if len(obj.Persona.Expertise) > 0 {
				b.WriteString("擅长领域：" + strings.Join(obj.Persona.Expertise, "、") + "。")
			}
			if len(obj.Persona.Forbidden) > 0 {
				b.WriteString("禁忌：" + strings.Join(obj.Persona.Forbidden, "、") + "。")
			}
			if b.Len() > 0 {
				return b.String()
			}
		}
	}
	return ""
}

func (s *AssetBundleService) ListBundles(ctx context.Context, f repository.AssetBundleFilter) ([]*model.AssetBundle, int64, error) {
	return s.repo.List(ctx, f)
}

func (s *AssetBundleService) ListBundlesWithParams(

	ctx context.Context,

	keyword, author, industry, language, scope string,

	status int,

	tags string,

	page, size int,

) ([]*model.AssetBundle, int64, error) {
	return s.repo.List(ctx, repository.AssetBundleFilter{
		Keyword:  keyword,
		Author:   author,
		Industry: industry,
		Language: language,
		Scope:    model.AssetBundleScope(scope),
		Status:   statusToAssetBundleStatus(status),
		Tags:     splitTags(tags),
		Page:     page,
		Size:     size,
	})
}

func statusToAssetBundleStatus(code int) model.AssetBundleStatus {
	switch code {
	case 1:
		return model.AssetBundleStatusDraft
	case 2:
		return model.AssetBundleStatusActive
	case 3:
		return model.AssetBundleStatusInactive
	case 4:
		return model.AssetBundleStatusArchived
	default:
		return ""
	}
}

func splitTags(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (s *AssetBundleService) WeaveForRequest(ctx context.Context, assetID, userQuery string, in *WeaveInput) ([]model.AssetBundleMessage, error) {
	if assetID == "" {
		return nil, errors.New("asset_id required")
	}
	if !s.hotPlug.isEmpty(ctx) && !in.Sandbox && !s.hotPlug.has(ctx, assetID) {
		return nil, ErrBundleNotHotEnabled
	}
	if in.Asset == nil {
		b, err := s.repo.FindByAssetID(ctx, assetID)
		if err != nil {
			return nil, err
		}
		in.Asset = b
	}
	if in.UserQuery == "" {
		in.UserQuery = userQuery
	}
	if !in.Sandbox {
		if err := s.repo.IncrementUseCount(ctx, assetID); err != nil {
			logger.Warnf("[asset_bundle] increment use_count failed asset=%s err=%v", assetID, err)
		}
	}
	return Weave(*in)
}

const (
	bundleHotPlugKeyPrefix = "bundle.hotplug."

	bundleHotPlugIndexKey = "bundle.hotplug.index"

	bundleHotPlugLocalTTL = 30 * time.Second
)

type hotPlugEntry struct {
	enabled   bool
	fetchedAt time.Time
}

func (e hotPlugEntry) fresh() bool {
	return time.Since(e.fetchedAt) < bundleHotPlugLocalTTL
}

type hotPlugCache struct {
	mu      sync.RWMutex
	kv      repository.SystemConfigKVRepository
	entries map[string]hotPlugEntry
	index   []string
	indexAt time.Time
}

func newHotPlugCache() hotPlugCache {
	return hotPlugCache{entries: make(map[string]hotPlugEntry)}
}

func (c *hotPlugCache) SetConfigKVRepo(kv repository.SystemConfigKVRepository) {
	c.mu.Lock()
	c.kv = kv
	c.mu.Unlock()
}

func (c *hotPlugCache) ensureKV() repository.SystemConfigKVRepository {
	c.mu.RLock()
	kv := c.kv
	c.mu.RUnlock()
	if kv != nil {
		return kv
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.kv == nil {
		kv := repository.NewSystemConfigKVRepository()
		if !kv.Available() {
			return nil
		}
		c.kv = kv
	}
	return c.kv
}

func (c *hotPlugCache) fetchEnabled(ctx context.Context, assetID string, fallback bool) bool {
	kv := c.ensureKV()
	if kv == nil {
		return fallback
	}
	val, err := kv.Get(ctx, bundleHotPlugKeyPrefix+assetID)
	if err != nil {
		logger.Warnf("[asset_bundle] hotplug kv get failed asset=%s err=%v", assetID, err)
		return fallback
	}
	return strings.TrimSpace(val) == "1"
}

func (c *hotPlugCache) loadEnabled(ctx context.Context, assetID string) bool {
	if assetID == "" {
		return false
	}
	c.mu.RLock()
	e, ok := c.entries[assetID]
	fallback := ok && e.enabled
	fresh := ok && e.fresh()
	kv := c.kv
	c.mu.RUnlock()
	if fresh {
		return e.enabled
	}
	if kv == nil && c.ensureKV() == nil {

		return fallback
	}
	enabled := c.fetchEnabled(ctx, assetID, fallback)
	c.mu.Lock()
	c.entries[assetID] = hotPlugEntry{enabled: enabled, fetchedAt: time.Now()}
	c.mu.Unlock()
	return enabled
}

func (c *hotPlugCache) upsert(ctx context.Context, assetID string, enabled bool) {
	if assetID == "" {
		return
	}
	if kv := c.ensureKV(); kv != nil {
		val := "0"
		if enabled {
			val = "1"
		}
		if _, err := kv.Upsert(ctx, bundleHotPlugKeyPrefix+assetID, val); err != nil {
			logger.Errorf("[asset_bundle] hotplug persist FAILED asset=%s enabled=%v err=%v", assetID, enabled, err)
		}
		if enabled {
			c.appendIndex(ctx, kv, assetID)
		}
	} else {
		logger.Warnf("[asset_bundle] hotplug kv repo unavailable: enable/disable is process-local only asset=%s", assetID)
	}
	c.mu.Lock()
	c.entries[assetID] = hotPlugEntry{enabled: enabled, fetchedAt: time.Now()}
	c.mu.Unlock()
}

func (c *hotPlugCache) appendIndex(ctx context.Context, kv repository.SystemConfigKVRepository, assetID string) {
	ids := c.candidateIDs(ctx)
	for _, id := range ids {
		if id == assetID {
			return
		}
	}
	ids = append(ids, assetID)
	data, err := json.Marshal(ids)
	if err != nil {
		logger.Warnf("[asset_bundle] hotplug index marshal failed err=%v", err)
		return
	}
	if _, err := kv.Upsert(ctx, bundleHotPlugIndexKey, string(data)); err != nil {
		logger.Warnf("[asset_bundle] hotplug index persist failed err=%v", err)
	}
}

func (c *hotPlugCache) candidateIDs(ctx context.Context) []string {
	c.mu.RLock()
	localFresh := time.Since(c.indexAt) < bundleHotPlugLocalTTL
	merged := append([]string(nil), c.index...)
	kv := c.kv
	c.mu.RUnlock()
	if localFresh || kv == nil {
		return merged
	}
	kv = c.ensureKV()
	if kv == nil {
		return merged
	}
	raw, err := kv.Get(ctx, bundleHotPlugIndexKey)
	if err != nil {
		logger.Warnf("[asset_bundle] hotplug index load failed err=%v", err)
		return merged
	}
	var remote []string
	if raw != "" {
		if uerr := json.Unmarshal([]byte(raw), &remote); uerr != nil {
			logger.Warnf("[asset_bundle] hotplug index unmarshal failed err=%v", uerr)
		}
	}
	seen := make(map[string]struct{}, len(merged)+len(remote))
	for _, id := range append(append([]string{}, merged...), remote...) {
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		merged = append(merged, id)
	}
	c.mu.Lock()
	c.index = merged
	c.indexAt = time.Now()
	c.mu.Unlock()
	return merged
}

func (c *hotPlugCache) add(ctx context.Context, assetID string) {
	c.upsert(ctx, assetID, true)
}

func (c *hotPlugCache) remove(ctx context.Context, assetID string) {
	c.upsert(ctx, assetID, false)
}

func (c *hotPlugCache) has(ctx context.Context, assetID string) bool {
	return c.loadEnabled(ctx, assetID)
}

func (c *hotPlugCache) isEmpty(ctx context.Context) bool {
	return len(c.list(ctx)) == 0
}

func (c *hotPlugCache) list(ctx context.Context) []string {
	out := make([]string, 0)
	for _, id := range c.candidateIDs(ctx) {
		if c.loadEnabled(ctx, id) {
			out = append(out, id)
		}
	}
	return out
}

func BuildBundleFromMerchantForm(req dto.MerchantFormSaveRequest) (*model.AssetBundle, error) {
	if req.AssetID == "" {
		return nil, errors.New("asset_id required")
	}
	if req.Title == "" {
		return nil, errors.New("title required")
	}

	system := buildMerchantSystemPrompt(req)

	messages := []model.AssetBundleMessage{
		{Role: "system", Content: system},
	}

	cards := append([]dto.MerchantQACard(nil), req.QACards...)
	sortQACardsByOrder(cards)
	for _, card := range cards {
		if card.UserExample == "" || card.Reply == "" {
			continue
		}
		messages = append(messages,
			model.AssetBundleMessage{Role: "user", Content: card.UserExample},
			model.AssetBundleMessage{
				Role:    "assistant",
				Content: card.Reply + "\n```json\n" + buildIntentJSON(card) + "\n```",
			},
		)
	}

	if req.CardConfig.IntentType != "" {
		cardMsg := buildMerchantCardSystemMessage(req.CardConfig)
		messages = append(messages, model.AssetBundleMessage{
			Role: "system", Content: cardMsg,
		})
	}

	messages = append(messages, model.AssetBundleMessage{
		Role: "system", Content: buildMerchantJSONProtocol(req),
	})

	return &model.AssetBundle{
		AssetID:  req.AssetID,
		Title:    req.Title,
		Author:   req.Author,
		Scope:    model.AssetBundleScopePrivate,
		Status:   model.AssetBundleStatusDraft,
		Language: "zh",
		Version:  "1.0.0",
		Industry: "",
		Messages: messages,
	}, nil
}

func buildMerchantSystemPrompt(req dto.MerchantFormSaveRequest) string {
	var sb strings.Builder
	sb.WriteString("# 核心角色与销冠人设\n")
	sb.WriteString("你是一名经过严格训练、结果导向的【王牌私域销售代表】。你的唯一目标是解答客户对产品的疑问，并用高情商的话术积极引导客户留下联系方式（WhatsApp/微信/邮箱）或直接促成转账订单。\n\n")
	switch req.CensorshipLevel {
	case "strict":
		sb.WriteString("# 合规约束\n你必须严格遵守所有平台与地方法规，遇到敏感问题礼貌拒答。\n\n")
	case "unlock":
		sb.WriteString("# 灵活应答风格\n在严格遵守平台内容规范与法律法规的前提下，采用灵活自然的应答风格。对客户提出的各类商品咨询（口味、成分、包装、物流等），以专业、亲切、笃定的口吻直接解答，避免机械化的模板式回复，让沟通自然流畅。\n\n")
	default:
		sb.WriteString("# 合规与销售平衡\n你对绝大多数合规问题礼貌回答，对违规问题可适度解释，但不主动引导。\n\n")
	}
	switch req.ToneLevel {
	case "high":
		sb.WriteString("# 语气词口语化修饰\n在回复中适度增加\"哈\"、\"哦\"、\"🥰\"等情绪词，让客户感觉在和真人聊天。\n\n")
	case "low":
		sb.WriteString("# 语气词\n保持专业克制，避免过度情绪化用词。\n\n")
	default:
		sb.WriteString("# 语气\n中等程度口语化。\n\n")
	}
	sb.WriteString("# 商户经营参数\n")
	if req.ShopName != "" {
		sb.WriteString("- 店铺名称: " + req.ShopName + "\n")
	}
	if req.CampaignName != "" {
		sb.WriteString("- 促销活动: " + req.CampaignName + "\n")
	}
	if req.DiscountPct != "" {
		sb.WriteString("- 优惠比例: " + req.DiscountPct + "\n")
	}
	if req.SupportContact != "" {
		sb.WriteString("- 客服联系方式: " + req.SupportContact + "\n")
	}
	if req.CrisisThreshold != "" {
		sb.WriteString("- 危机感触发阈值: " + req.CrisisThreshold + "（达到此分数强制转人工）\n")
	}
	sb.WriteString("\n# 商户配置快照（勿修改）\n")
	sb.WriteString("- 危机感触发阈值: " + req.CrisisThreshold + "\n")
	sb.WriteString("- 语气词等级: " + req.ToneLevel + "\n")
	sb.WriteString("- 反审查尺度: " + req.CensorshipLevel + "\n")
	if len(req.EnabledIntents) > 0 {
		sb.WriteString("- 启用结算意图: " + strings.Join(req.EnabledIntents, ",") + "\n")
	}
	return sb.String()
}

func buildMerchantJSONProtocol(req dto.MerchantFormSaveRequest) string {
	_ = req
	return "# 强制业务结算协议\n为了配合后台数据登记，你必须在每一次回复用户的纯文本消息【最后】，强制附带一个结构完全合法的 JSON 块，并严格包裹在 ```json 和 ``` 之间。格式如下：\n```json\n{\n  \"intent\": \"枚举值: faq / lead_capture / human_transfer\",\n  \"captured_data\": {\"whatsapp\": \"提取的号码\", \"email\": \"提取的邮箱\", \"product\": \"意向产品\", \"quantity\": \"意向数量\"}\n}\n```\n## 铁律\n- 给用户的纯文本回复必须放在 JSON 块的【前面】。\n- 绝不允许漏掉最后的 JSON 块，即便 captured_data 为空对象 {} 也必须输出。"
}

func buildMerchantCardSystemMessage(cfg dto.MerchantCardConfig) string {
	var sb strings.Builder
	sb.WriteString("# 多媒体卡片消息配置\n")
	sb.WriteString("- 触发意图结算类型: " + cfg.IntentType + "\n")
	if cfg.ProductImage != "" {
		sb.WriteString("- 绑定商品主图: " + cfg.ProductImage + "\n")
	}
	if len(cfg.Buttons) > 0 {
		sb.WriteString("- 动作按钮链:\n")
		for i, btn := range cfg.Buttons {
			sb.WriteString("  " + strconv.Itoa(i+1) + ". [" + btn.Title + "]\n")
			switch btn.Action {
			case "open_url":
				sb.WriteString("     跳转 URL: " + btn.URL + "\n")
			case "call_api":
				sb.WriteString("     触发本地工具: " + btn.APIName + "\n")
			}
		}
	}
	return sb.String()
}

func buildIntentJSON(card dto.MerchantQACard) string {
	reply := strings.ToLower(card.Reply)
	trigger := strings.ToLower(card.Trigger)
	intent := "faq"
	if strings.Contains(reply, "whatsapp") || strings.Contains(reply, "wechat") || strings.Contains(reply, "邮箱") || strings.Contains(reply, "联系") {
		intent = "lead_capture"
	} else if strings.Contains(trigger, "退") || strings.Contains(trigger, "投诉") || strings.Contains(trigger, "骗子") {
		intent = "human_transfer"
	}
	captured := map[string]string{}
	if m := regexp.MustCompile(`\+?\d[\d\s-]{7,}`).FindString(card.Reply); m != "" {
		captured["whatsapp"] = strings.TrimSpace(m)
	}
	capturedJSON, _ := json.Marshal(captured)
	return fmt.Sprintf(`{"intent":"%s","captured_data":%s}`, intent, string(capturedJSON))
}

func sortQACardsByOrder(cards []dto.MerchantQACard) {
	for i := 1; i < len(cards); i++ {
		for j := i; j > 0 && cards[j-1].Order > cards[j].Order; j-- {
			cards[j-1], cards[j] = cards[j], cards[j-1]
		}
	}
}

// ParseBundleToMerchantForm messages 数组 → 商户表单
//
// 文档：方向9 §六
// 用正则从 messages[0].content 提取参数 + 从 Few-Shots 提取 QA 卡片
func ParseBundleToMerchantForm(bundle *model.AssetBundle) dto.MerchantFormParseResponse {
	resp := dto.MerchantFormParseResponse{
		QACards: []dto.MerchantQACard{},
	}
	if bundle == nil {
		return resp
	}
	for _, msg := range bundle.Messages {
		if msg.Role != "system" {
			continue
		}
		content := msg.Content
		if m := regexp.MustCompile(`店铺名称[:：]\s*(.+)`).FindStringSubmatch(content); len(m) > 1 {
			resp.ShopName = strings.TrimSpace(m[1])
		}
		if m := regexp.MustCompile(`促销活动[:：]\s*(.+)`).FindStringSubmatch(content); len(m) > 1 {
			resp.CampaignName = strings.TrimSpace(m[1])
		}
		if m := regexp.MustCompile(`优惠比例[:：]\s*(.+)`).FindStringSubmatch(content); len(m) > 1 {
			resp.DiscountPct = strings.TrimSpace(m[1])
		}
		if m := regexp.MustCompile(`客服联系方式[:：]\s*(.+)`).FindStringSubmatch(content); len(m) > 1 {
			resp.SupportContact = strings.TrimSpace(m[1])
		}
		if m := regexp.MustCompile(`危机感触发阈值[:：]\s*(\d+)`).FindStringSubmatch(content); len(m) > 1 {
			resp.CrisisThreshold = strings.TrimSpace(m[1])
		}
		if m := regexp.MustCompile(`语气词等级[:：]\s*(\S+)`).FindStringSubmatch(content); len(m) > 1 {
			resp.ToneLevel = strings.TrimSpace(m[1])
		}
		if m := regexp.MustCompile(`反审查尺度[:：]\s*(\S+)`).FindStringSubmatch(content); len(m) > 1 {
			resp.CensorshipLevel = strings.TrimSpace(m[1])
		}
		if m := regexp.MustCompile(`启用结算意图[:：]\s*([\w,]+)`).FindStringSubmatch(content); len(m) > 1 {
			resp.EnabledIntents = strings.Split(strings.TrimSpace(m[1]), ",")
		}
	}
	for i := 0; i < len(bundle.Messages)-1; i++ {
		if bundle.Messages[i].Role == "user" && bundle.Messages[i+1].Role == "assistant" {
			reply := bundle.Messages[i+1].Content
			if m := regexp.MustCompile(`\n*\x60{3}json[\s\S]*?\x60{3}\s*$`).FindStringIndex(reply); m != nil {
				reply = reply[:m[0]]
			}
			card := dto.MerchantQACard{
				ID:          fmt.Sprintf("card_%d", len(resp.QACards)+1),
				UserExample: bundle.Messages[i].Content,
				Reply:       reply,
				Order:       len(resp.QACards),
			}
			resp.QACards = append(resp.QACards, card)
		}
	}
	return resp
}

var platformSubmitBannedWords = []string{
	"越狱",
	"jailbreak",
	"无视审查",
	"反审查",
	"反安全审查",
	"拒答洗脑",
	`\bDAN\b`,
}

var (
	bannedWordRE = regexp.MustCompile(`(?i)(` + strings.Join(platformSubmitBannedWords, "|") + `)`)

	censorConfigKeyLineRE = regexp.MustCompile(`(?m)^- 反审查尺度[:：].*$`)
)

// ScanSystemPromptBannedWords 对 system prompt 做敏感词黑名单扫描，
// 命中即返回包含命中词的明确错误。商户配置快照的固定键行除外。
func ScanSystemPromptBannedWords(prompt string) error {
	if strings.TrimSpace(prompt) == "" {
		return nil
	}
	target := censorConfigKeyLineRE.ReplaceAllString(prompt, "")
	if hit := bannedWordRE.FindString(target); hit != "" {
		return fmt.Errorf("system prompt 包含违规敏感词 %q，拒绝提交平台审核", hit)
	}
	return nil
}

// ValidateBundleForPlatformSubmit 提交平台审核前的敏感词扫描入口（K-1）：
// 遍历资产包全部 role=system 消息，依次执行：
//  1. ScanSystemPromptBannedWords：黑名单越狱/对抗性话术扫描
//  2. safeprompt.ScanOutput：PII + 运行时敏感词扫描
//
// 任一命中严重违规即拒绝提交。
func ValidateBundleForPlatformSubmit(bundle *model.AssetBundle) error {
	if bundle == nil {
		return errors.New("bundle is nil")
	}
	for _, m := range bundle.Messages {
		if m.Role != "system" {
			continue
		}
		if err := ScanSystemPromptBannedWords(m.Content); err != nil {
			return err
		}
		if violations := safeprompt.ScanOutput(m.Content); safeprompt.HasCriticalViolation(violations) {
			return fmt.Errorf("system prompt 包含严重违规内容: %v", violations)
		}
	}
	return nil
}
