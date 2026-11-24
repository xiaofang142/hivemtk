package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/repository"
)

// OneIDMergeRuleService OneID 合并规则服务。
//
// FA-43：规则按租户持久化到 system_config_kv（key=oneid_merge_rules_v1），
// 每次保存版本号 +1；不再使用进程内单例——重启、双副本、不同租户各读自己
// 租户名下那一份版本，A/B 相反规则互不覆盖。预览（PreviewMergeRules）与
// 正式合并（CustomerIdentityService.MergeByIdentity → ApplyRules）共用
// matchRule 判定与同一份规则版本。
type OneIDMergeRuleService struct {
	kv repository.SystemConfigKVRepository
}

// oneIDMergeRulesKVKey 规则集在租户级 system_config_kv 中的键。
const oneIDMergeRulesKVKey = "oneid_merge_rules_v1"

// NewOneIDMergeRuleService 构造。KV 仓储在构造时捕获 DB 句柄；无句柄时
// GetRules 回落默认规则集（与旧内存单例的 fail-open 语义一致），SaveRules
// 则返回 ErrKVStoreUnavailable，避免"保存成功但重启丢失"的假象。
func NewOneIDMergeRuleService() *OneIDMergeRuleService {
	return &OneIDMergeRuleService{kv: repository.NewSystemConfigKVRepository()}
}

// MergeRule 合并规则。
//
// FA-44：冻结可执行字段契约。Type/Fields/Threshold/Action 是前端规则编辑器
// 实际可配的字段，必须原样落库并参与判定，不能保存后静默丢弃；Field/Op/Value
// 保留兼容旧数据与内置规则。ID 由服务端统一分配（新规则发 0），保证稳定唯一。
type MergeRule struct {
	ID          int64    `json:"id"`
	Name        string   `json:"name"`
	Type        string   `json:"type,omitempty"` // deterministic|probabilistic
	Fields      []string `json:"fields,omitempty"`
	Threshold   int      `json:"threshold,omitempty"`
	Action      string   `json:"action,omitempty"` // auto|review
	Field       string   `json:"field"`
	Op          string   `json:"op"`
	Value       string   `json:"value"`
	Priority    int      `json:"priority"`
	Enabled     bool     `json:"enabled"`
	BuiltIn     bool     `json:"built_in"`
	UpdatedAt   string   `json:"updated_at"`
	Description string   `json:"description"`
	Example     string   `json:"example"`
}

// MergeStrategy 合并策略。AutoMergeEnabled 是自动合并总闸：关闭后
// MergeByIdentity 只按标识找到主档、不执行任何合并，但保留识别/补全语义。
type MergeStrategy struct {
	PrimaryRule      string   `json:"primary_rule"`
	ConflictBehavior string   `json:"conflict_behavior"`
	AutoMergeEnabled bool     `json:"auto_merge_enabled"`
	WindowStart      string   `json:"window_start"`
	WindowEnd        string   `json:"window_end"`
	PostMergeActions []string `json:"post_merge_actions"`
}

// MergeRuleSet 完整规则集
type MergeRuleSet struct {
	Version   int           `json:"version"`
	BuiltIn   []MergeRule   `json:"built_in"`
	Custom    []MergeRule   `json:"custom"`
	Strategy  MergeStrategy `json:"strategy"`
	UpdatedAt string        `json:"updated_at"`
}

// versionedMergeRuleSet KV 中存储的结构：版本号与规则集同处一条记录，
// 读口把 Version 回填到 MergeRuleSet，保证预览/正式合并看到同一版本。
type versionedMergeRuleSet struct {
	Version int           `json:"version"`
	Rules   *MergeRuleSet `json:"rules"`
}

// GetRules 读取当前租户的规则集（按 KV 持久化版本）。未配置/存储不可用时
// 返回默认规则集并置 Version=0；规则集 JSON 损坏才报错，避免静默用默认
// 规则跑错租户版本。
func (s *OneIDMergeRuleService) GetRules(ctx context.Context) (*MergeRuleSet, error) {
	if s == nil || s.kv == nil || !s.kv.Available() {
		return defaultRuleSet(), nil
	}
	raw, err := s.kv.Get(ctx, oneIDMergeRulesKVKey)
	if err != nil {
		if errors.Is(err, repository.ErrKVStoreUnavailable) || errors.Is(err, repository.ErrKVTenantContextMissing) {
			return defaultRuleSet(), nil
		}
		// 规则表尚未迁移（滚动发布/测试库未建 system_config_kv）时按"无持久化规则"
		// 回落默认集，而不是让合并整条链失败。这与"未配置规则→默认"语义一致；
		// 其它 DB 故障仍透出，不掩盖真实读写问题。
		if strings.Contains(err.Error(), "42P01") || strings.Contains(err.Error(), "does not exist") {
			return defaultRuleSet(), nil
		}
		return nil, err
	}
	if raw == "" {
		return defaultRuleSet(), nil
	}
	var stored versionedMergeRuleSet
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return nil, fmt.Errorf("oneid 合并规则反序列化失败: %w", err)
	}
	if stored.Rules == nil {
		return defaultRuleSet(), nil
	}
	out := *stored.Rules
	out.Version = stored.Version
	return &out, nil
}

// SaveRules 全量替换当前租户规则集，版本号 +1。
// 内置规则只允许切换 enabled / 调整 priority（字段与算子不可篡改）。
func (s *OneIDMergeRuleService) SaveRules(ctx context.Context, set *MergeRuleSet) (*MergeRuleSet, error) {
	if set == nil {
		return nil, errors.New("规则集不能为空")
	}
	if err := validateRuleSet(set); err != nil {
		return nil, err
	}
	current, err := s.GetRules(ctx)
	if err != nil {
		return nil, err
	}
	version := 1
	if current != nil && current.Version > 0 {
		version = current.Version + 1
	}
	out := *set
	// FA-44：服务端统一分配稳定 ID。前端新建规则发 0/省略，绝不让多条新规则
	// 撞同一个 ID；已带正数 ID 的规则保持原 ID（编辑路径），重载/删除只影响目标。
	out.Custom = make([]MergeRule, len(set.Custom))
	copy(out.Custom, set.Custom)
	used := make(map[int64]bool)
	for _, r := range current.BuiltIn {
		used[r.ID] = true
	}
	for _, r := range out.BuiltIn {
		used[r.ID] = true
	}
	for _, r := range out.Custom {
		if r.ID > 0 {
			used[r.ID] = true
		}
	}
	nextID := int64(1000)
	for used[nextID] {
		nextID++
	}
	for i := range out.Custom {
		if out.Custom[i].ID <= 0 {
			out.Custom[i].ID = nextID
			used[nextID] = true
			nextID++
			for used[nextID] {
				nextID++
			}
		}
	}
	out.Version = version
	out.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	stored := versionedMergeRuleSet{Version: version, Rules: &out}
	raw, err := json.Marshal(stored)
	if err != nil {
		return nil, err
	}
	if s == nil || s.kv == nil || !s.kv.Available() {
		return nil, repository.ErrKVStoreUnavailable
	}
	if _, err := s.kv.Upsert(ctx, oneIDMergeRulesKVKey, string(raw)); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteRule 按 ID 删除当前租户的自定义规则；内置规则不允许删除。
func (s *OneIDMergeRuleService) DeleteRule(ctx context.Context, id int64) (*MergeRuleSet, error) {
	cur, err := s.GetRules(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range cur.BuiltIn {
		if r.ID == id {
			return nil, errors.New("内置规则不允许删除，仅可停用")
		}
	}
	found := false
	custom := make([]MergeRule, 0, len(cur.Custom))
	for _, r := range cur.Custom {
		if r.ID == id {
			found = true
			continue
		}
		custom = append(custom, r)
	}
	if !found {
		return nil, fmt.Errorf("规则 %d 不存在", id)
	}
	cur.Custom = custom
	version := cur.Version + 1
	if version < 1 {
		version = 1
	}
	cur.Version = version
	cur.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	stored := versionedMergeRuleSet{Version: version, Rules: cur}
	raw, err := json.Marshal(stored)
	if err != nil {
		return nil, err
	}
	if s == nil || s.kv == nil || !s.kv.Available() {
		return nil, repository.ErrKVStoreUnavailable
	}
	if _, err := s.kv.Upsert(ctx, oneIDMergeRulesKVKey, string(raw)); err != nil {
		return nil, err
	}
	return cur, nil
}

// ApplyRulesWithAction 用当前租户版本的规则判断一对身份是否应合并。
// 返回 (是否命中, 命中规则名, 动作 auto|review, 匹配分, 错误)。
// 与预览共用 evaluateRule：多字段、阈值口径完全一致；action 供正式合并
// 决定直接合并还是进入人工审核。
func (s *OneIDMergeRuleService) ApplyRulesWithAction(ctx context.Context, a, b map[string]string) (bool, string, string, int, error) {
	rules, err := s.GetRules(ctx)
	if err != nil {
		return false, "", "", 0, err
	}
	for _, group := range [][]MergeRule{rules.BuiltIn, rules.Custom} {
		for _, r := range group {
			if !r.Enabled {
				continue
			}
			hit, score := evaluateRule(a, b, r)
			if hit {
				return true, r.Name, ruleAction(r), score, nil
			}
		}
	}
	return false, "", "", 0, nil
}

// ApplyRules 兼容旧调用：只取命中与规则名（FA-43 既有用例）。
func (s *OneIDMergeRuleService) ApplyRules(ctx context.Context, a, b map[string]string) (bool, string, error) {
	hit, name, _, _, err := s.ApplyRulesWithAction(ctx, a, b)
	return hit, name, err
}

// CanAutoMerge 报告当前租户版本是否允许自动合并：总闸关闭或窗口外均为 false。
// 窗口为空视为全天允许；窗口跨午夜（start > end）按"或"处理。
func (s *OneIDMergeRuleService) CanAutoMerge(ctx context.Context, now time.Time) (bool, error) {
	rules, err := s.GetRules(ctx)
	if err != nil {
		return false, err
	}
	if !rules.Strategy.AutoMergeEnabled {
		return false, nil
	}
	return inMergeWindow(now, rules.Strategy.WindowStart, rules.Strategy.WindowEnd), nil
}

// ChoosePrimary 按当前租户版本的主客户策略选主档。
// latest_active 取最近更新者；most_orders 取关联订单最多者；
// manual / 未配置保持历史"取第一个"语义。
func (s *OneIDMergeRuleService) ChoosePrimary(ctx context.Context, customers []*model.Customer) (*model.Customer, error) {
	if len(customers) == 0 {
		return nil, errors.New("候选客户为空")
	}
	rules, err := s.GetRules(ctx)
	if err != nil {
		return nil, err
	}
	primary := customers[0]
	switch rules.Strategy.PrimaryRule {
	case "latest_active":
		for _, c := range customers[1:] {
			if c.UpdatedAt.After(primary.UpdatedAt) ||
				(c.UpdatedAt.Equal(primary.UpdatedAt) && c.CreatedAt.After(primary.CreatedAt)) {
				primary = c
			}
		}
	case "most_orders":
		bestN := countCustomerOrders(ctx, primary.UnifiedID)
		for _, c := range customers[1:] {
			n := countCustomerOrders(ctx, c.UnifiedID)
			if n > bestN {
				primary, bestN = c, n
			}
		}
	}
	return primary, nil
}

// customerIdentityMap 把客户的强标识铺成规则字段键，供 ApplyRules 使用。
func customerIdentityMap(c *model.Customer) map[string]string {
	return map[string]string{
		"phone":          c.Phone,
		"email":          c.Email,
		"unionid":        c.WechatOpenID,
		"openid":         c.WechatOpenID,
		"wechat_open_id": c.WechatOpenID,
		"douyin_id":      c.DouyinOpenID,
		"douyin_open_id": c.DouyinOpenID,
		"xiaohongshu_id": c.XiaohongshuID,
		"external_id":    c.WeComExternalID,
	}
}

func countCustomerOrders(ctx context.Context, unifiedID string) int64 {
	if unifiedID == "" {
		return 0
	}
	n := repository.NewOrderRepository().CountByUnifiedID(ctx, unifiedID)
	return n
}

// inMergeWindow 判断 now（分钟粒度）是否落在 HH:MM–HH:MM 窗口内。
func inMergeWindow(now time.Time, start, end string) bool {
	start, end = strings.TrimSpace(start), strings.TrimSpace(end)
	if start == "" || end == "" {
		return true
	}
	sh, sm, err1 := parseHHMM(start)
	eh, em, err2 := parseHHMM(end)
	if err1 != nil || err2 != nil {
		return true
	}
	minutes := now.Hour()*60 + now.Minute()
	from, to := sh*60+sm, eh*60+em
	if from <= to {
		return minutes >= from && minutes <= to
	}
	return minutes >= from || minutes <= to // 跨午夜
}

func parseHHMM(s string) (int, int, error) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("非法时间 %q", s)
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, err
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, err
	}
	return h, m, nil
}

func matchRule(a, b, op string) bool {
	switch op {
	case "eq":
		return a == b
	case "prefix":
		plen := 7
		if len(a) < plen || len(b) < plen {
			return false
		}
		return a[:plen] == b[:plen]
	case "like":
		return containsSubstr(a, b) || containsSubstr(b, a)
	}
	return false
}

func containsSubstr(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// ruleScore 按算子给单字段匹配打分（越高越可信）：
// eq/prefix 命中=100；like 互为子串=100，否则按最长公共前缀占比。
// 未命中返回 0。阈值判定与预览共用此口径。
func ruleScore(a, b, op string) int {
	if a == "" || b == "" {
		return 0
	}
	switch op {
	case "eq":
		if a == b {
			return 100
		}
	case "prefix":
		plen := 7
		if len(a) >= plen && len(b) >= plen && a[:plen] == b[:plen] {
			return 100
		}
	case "like":
		if containsSubstr(a, b) || containsSubstr(b, a) {
			return 100
		}
		n := len(a)
		if len(b) < n {
			n = len(b)
		}
		i := 0
		for i < n && a[i] == b[i] {
			i++
		}
		max := len(a)
		if len(b) > max {
			max = len(b)
		}
		if max > 0 {
			return i * 100 / max
		}
	}
	return 0
}

// evaluateRule 按完整规则契约判定一对身份：多字段 OR 取最高分，
// Threshold<=0 视为 100；Fields 为空时兼容回落到 Field。
// 预览（PreviewMergeRules）与正式合并（ApplyRules）共用本函数，
// 保证字段/阈值口径一致，不会出现"预览命中、正式不命中"。
func evaluateRule(a, b map[string]string, r MergeRule) (matched bool, score int) {
	fields := r.Fields
	if len(fields) == 0 && r.Field != "" {
		fields = []string{r.Field}
	}
	op := r.Op
	if op == "" {
		op = "eq"
	}
	threshold := r.Threshold
	if threshold <= 0 {
		threshold = 100
	}
	best := 0
	for _, f := range fields {
		va, oka := a[f]
		vb, okb := b[f]
		if !oka || !okb {
			continue
		}
		if s := ruleScore(va, vb, op); s > best {
			best = s
		}
	}
	return best >= threshold, best
}

// ruleAction 归一化规则动作：review 之外的取值一律视为 auto。
func ruleAction(r MergeRule) string {
	if r.Action == "review" {
		return "review"
	}
	return "auto"
}

// allowedMergeFields 是规则可执行的字段全集。页面或 API 提交集合外字段
// 一律报错，避免"保存成功但合并时静默不执行"的假保存。
var allowedMergeFields = map[string]bool{
	"phone": true, "email": true, "unionid": true, "openid": true,
	"wechat_open_id": true, "douyin_id": true, "douyin_open_id": true,
	"xiaohongshu_id": true, "external_id": true, "nickname": true,
	"region": true, "device_fingerprint": true,
}

func validateRuleSet(set *MergeRuleSet) error {
	if len(set.BuiltIn) == 0 && len(set.Custom) == 0 {
		return errors.New("至少需要 1 条规则")
	}
	// 内置规则只允许切换 enabled / 调整 priority，字段与算子不可篡改。
	defaults := defaultRuleSet()
	byID := make(map[int64]MergeRule, len(defaults.BuiltIn))
	for _, r := range defaults.BuiltIn {
		byID[r.ID] = r
	}
	seenIDs := make(map[int64]bool)
	for _, r := range set.BuiltIn {
		d, ok := byID[r.ID]
		if !ok || r.Field != d.Field || r.Op != d.Op || r.Name != d.Name {
			return errors.New("内置规则的字段/算子/名称不可修改，仅可切换 enabled 或调整 priority")
		}
		if seenIDs[r.ID] {
			return fmt.Errorf("规则 ID %d 重复", r.ID)
		}
		seenIDs[r.ID] = true
	}
	for i, r := range set.Custom {
		if r.Name == "" {
			return errors.New("custom rule #" + strconv.Itoa(i) + " 缺 name")
		}
		if len(r.Fields) == 0 && r.Field == "" {
			return errors.New("custom rule " + r.Name + " 缺 field/fields")
		}
		// FA-44：字段全集 + 无重复字段 + 类型/算子/动作/阈值枚举校验。
		seenFields := make(map[string]bool)
		for _, f := range r.Fields {
			if !allowedMergeFields[f] {
				return fmt.Errorf("规则 %s 含不支持字段 %q", r.Name, f)
			}
			if seenFields[f] {
				return fmt.Errorf("规则 %s 字段 %q 重复", r.Name, f)
			}
			seenFields[f] = true
		}
		if r.Field != "" && !allowedMergeFields[r.Field] {
			return fmt.Errorf("规则 %s 含不支持字段 %q", r.Name, r.Field)
		}
		switch r.Type {
		case "", "deterministic", "probabilistic":
		default:
			return fmt.Errorf("规则 %s 的 type 取值非法", r.Name)
		}
		switch r.Op {
		case "", "eq", "prefix", "like":
		default:
			return fmt.Errorf("规则 %s 的 op 取值非法", r.Name)
		}
		switch r.Action {
		case "", "auto", "review":
		default:
			return fmt.Errorf("规则 %s 的 action 取值非法", r.Name)
		}
		if r.Threshold < 0 || r.Threshold > 100 {
			return fmt.Errorf("规则 %s 的 threshold 必须在 0-100", r.Name)
		}
		if r.ID > 0 {
			if seenIDs[r.ID] {
				return fmt.Errorf("规则 ID %d 重复", r.ID)
			}
			seenIDs[r.ID] = true
		}
	}
	switch set.Strategy.PrimaryRule {
	case "latest_active", "most_orders", "manual":
	default:
		return errors.New("primary_rule 取值非法")
	}
	switch set.Strategy.ConflictBehavior {
	case "auto_merge", "queue_review", "skip":
	default:
		return errors.New("conflict_behavior 取值非法")
	}
	return nil
}

func defaultRuleSet() *MergeRuleSet {
	now := time.Now().UTC().Format(time.RFC3339)
	return &MergeRuleSet{
		Version: 0,
		BuiltIn: []MergeRule{
			{ID: 1, Name: "同手机号合并", Field: "phone", Op: "eq", Priority: 100, Enabled: true, BuiltIn: true, UpdatedAt: now, Description: "相同手机号必合并", Example: "13800001111 == 13800001111"},
			{ID: 2, Name: "同 UnionID 合并", Field: "unionid", Op: "eq", Priority: 95, Enabled: true, BuiltIn: true, UpdatedAt: now, Description: "微信开放平台 UnionID", Example: "o6_bm123 == o6_bm123"},
			{ID: 3, Name: "同邮箱合并", Field: "email", Op: "eq", Priority: 90, Enabled: true, BuiltIn: true, UpdatedAt: now, Description: "相同邮箱必合并", Example: "a@x.com == a@x.com"},
			{ID: 4, Name: "同 OpenID 合并", Field: "openid", Op: "eq", Priority: 80, Enabled: false, BuiltIn: true, UpdatedAt: now, Description: "单应用内 OpenID", Example: "同一公众号 OpenID"},
			{ID: 5, Name: "同外部 ID 合并", Field: "external_id", Op: "eq", Priority: 70, Enabled: true, BuiltIn: true, UpdatedAt: now, Description: "CRM 外部系统 ID", Example: "salesforce_001 == sfdc_001"},
			{ID: 6, Name: "手机号前 7 位合并", Field: "phone", Op: "prefix", Priority: 50, Enabled: false, BuiltIn: true, UpdatedAt: now, Description: "易换号场景兜底", Example: "1380000**** == 1380000****"},
			{ID: 7, Name: "同抖音 OpenID 合并", Field: "douyin_open_id", Op: "eq", Priority: 85, Enabled: true, BuiltIn: true, UpdatedAt: now, Description: "抖音开放平台标识", Example: "同一抖音 OpenID"},
			{ID: 8, Name: "同小红书 ID 合并", Field: "xiaohongshu_id", Op: "eq", Priority: 60, Enabled: true, BuiltIn: true, UpdatedAt: now, Description: "小红书用户 ID", Example: "同一小红书 ID"},
		},
		Custom: []MergeRule{},
		Strategy: MergeStrategy{
			PrimaryRule:      "latest_active",
			ConflictBehavior: "auto_merge",
			AutoMergeEnabled: true,
			// 默认无窗口（全时段可合并）：未显式配置规则时保持旧版 fail-open 语义，
			// 避免历史身份合并（同手机/邮箱自愈）在非 02:00–05:00 被默认规则悄悄跳过。
			// 需要限定时段由租户显式配置 WindowStart/WindowEnd（见 TestFA43_MergeWindow）。
			WindowStart:      "",
			WindowEnd:        "",
			PostMergeActions: []string{"unify_tag", "write_audit"},
		},
		UpdatedAt: now,
	}
}

// TenantOfMergeRules 暴露当前 KV 仓储归属（测试装配用）；无租户时返回默认租户。
func TenantOfMergeRules(ctx context.Context) uint {
	if id, ok := tenant.FromContext(ctx); ok {
		return id
	}
	return tenant.DefaultTenantID
}
