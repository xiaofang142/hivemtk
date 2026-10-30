package repository

import (
	"context"
)

// 模板/技能市场（F2 最小可用版）的目录数据源。
//
// 为什么是「进程内注册的只读目录」而不是数据库表：
// F2 在开发计划里是 P2 季度级项，本轮只立「目录契约」——浏览 + 元数据 + 分类，
// 不含支付/发布/审核闭环。目录项指向的既有事实源（内容模板 HTML、sop_templates、
// integration_templates、MCP 守卫、智能路由等）分散在多个包/多张表里，现在为它们
// 建一张 market_catalog 表，等于把「一份事实抄成两份」且没有任何写路径能同步它。
// 等发布/审核闭环立项时，再迁到真实仓储（表名与归属理由见 specs/013 的 delivery.md）。

// 目录条目类别（kind）：模板 / 技能。
const (
	EntryKindTemplate = "template"
	EntryKindSkill    = "skill"
)

// 目录条目可用性（availability）：
//   - builtin      进程内已具备，开箱即用
//   - needs_module 需要先开通对应功能模块（收费闸）
//   - coming_soon  骨架已在，能力未闭环（占位展示）
const (
	AvailabilityBuiltin     = "builtin"
	AvailabilityNeedsModule = "needs_module"
	AvailabilityComingSoon  = "coming_soon"
)

// TemplateMarketEntry 市场目录条目（元数据契约）。
//
// 这是 F2 最小数据面的契约形态：ID / 名称 / 分类 / 简介 / 来源 / 可用性，
// 外加 kind（模板或技能）与 tags（辅助过滤）。字段语义：
//   - Source 指向既有实现的「事实源」（文件路径或表/服务名），不重复存储内容本身。
//   - Availability 表达「用户能不能立刻用」，是目录层与内容层的唯一耦合点。
type TemplateMarketEntry struct {
	ID           string   `json:"id"`
	Kind         string   `json:"kind"` // EntryKindTemplate | EntryKindSkill
	Name         string   `json:"name"`
	Category     string   `json:"category"`
	Summary      string   `json:"summary"`
	Source       string   `json:"source"`
	Availability string   `json:"availability"` // AvailabilityBuiltin | NeedsModule | ComingSoon
	Tags         []string `json:"tags,omitempty"`
	// PriceCents 售价（分）。内置种子恒 0（免费）；写侧条目（S-22 发布-审核闭环
	// 落库的 market_catalog_entries）可定价，付费条目走订单链购买。
	PriceCents int64 `json:"price_cents"`
}

// TemplateMarketRepository 市场目录仓储接口。
//
// 只有两个读方法，刻意不提供写方法：本轮是只读市场，发布/审核闭环未立项，
// 写方法现在定义出来只会被内存实现「假装实现」然后在接真仓储时返工。
type TemplateMarketRepository interface {
	// All 返回目录全量条目（只读）。
	All(ctx context.Context) ([]TemplateMarketEntry, error)
	// Get 按 ID 取单条；目录中不存在时返回 (nil, nil)（与仓库层「查无返回 nil,nil」惯例一致）。
	Get(ctx context.Context, id string) (*TemplateMarketEntry, error)
}

// MemoryTemplateMarketRepository 进程内目录仓储（假仓储，不连 DB）。
type MemoryTemplateMarketRepository struct {
	entries []TemplateMarketEntry
	index   map[string]int // ID → entries 下标
}

// NewMemoryTemplateMarketRepository 构造进程内目录仓储。
func NewMemoryTemplateMarketRepository() *MemoryTemplateMarketRepository {
	return &MemoryTemplateMarketRepository{
		entries: marketCatalogSeed(),
		index:   buildMarketIndex(marketCatalogSeed()),
	}
}

// NewMemoryTemplateMarketRepositoryWith 用显式目录构造（测试注入）。
func NewMemoryTemplateMarketRepositoryWith(entries []TemplateMarketEntry) *MemoryTemplateMarketRepository {
	return &MemoryTemplateMarketRepository{
		entries: entries,
		index:   buildMarketIndex(entries),
	}
}

func buildMarketIndex(entries []TemplateMarketEntry) map[string]int {
	m := make(map[string]int, len(entries))
	for i, e := range entries {
		m[e.ID] = i
	}
	return m
}

// All 返回目录副本：只读契约，防调用方原地改写目录数据。
func (r *MemoryTemplateMarketRepository) All(ctx context.Context) ([]TemplateMarketEntry, error) {
	out := make([]TemplateMarketEntry, len(r.entries))
	copy(out, r.entries)
	return out, nil
}

// Get 按 ID 取单条；不存在返回 (nil, nil)。
func (r *MemoryTemplateMarketRepository) Get(ctx context.Context, id string) (*TemplateMarketEntry, error) {
	i, ok := r.index[id]
	if !ok {
		return nil, nil
	}
	e := r.entries[i]
	return &e, nil
}

// marketCatalogSeed 目录种子数据。
//
// 每一条都必须能指向仓库里真实存在的实现（Source 列），不虚构能力；
// coming_soon 那一条对应开发计划 D1「Agentic Lead-to-Cash」——骨架（ltc_gate）
// 已在，闭环未实挂，如实标成占位。
func marketCatalogSeed() []TemplateMarketEntry {
	seed := []TemplateMarketEntry{
		// ── 模板：渠道卡片页（内容模板）──────────────────────────────
		{ID: "card-douyin", Kind: EntryKindTemplate, Name: "抖音卡片页", Category: "channel_card",
			Summary: "抖音渠道的落地卡片页模板，标题/描述/配图/跳转由卡片数据驱动",
			Source:  "internal/reach/card/template/douyin_card.html", Availability: AvailabilityBuiltin,
			Tags: []string{"douyin", "卡片"}},
		{ID: "card-xiaohongshu", Kind: EntryKindTemplate, Name: "小红书卡片页", Category: "channel_card",
			Summary: "小红书渠道的落地卡片页模板",
			Source:  "internal/reach/card/template/xiaohongshu_card.html", Availability: AvailabilityBuiltin,
			Tags: []string{"xiaohongshu", "卡片"}},
		{ID: "card-xianyu", Kind: EntryKindTemplate, Name: "闲鱼卡片页", Category: "channel_card",
			Summary: "闲鱼渠道的落地卡片页模板",
			Source:  "internal/reach/card/template/xianyu_card.html", Availability: AvailabilityBuiltin,
			Tags: []string{"xianyu", "卡片"}},
		{ID: "card-kuaishou", Kind: EntryKindTemplate, Name: "快手卡片页", Category: "channel_card",
			Summary: "快手渠道的落地卡片页模板",
			Source:  "internal/reach/card/template/kuaishou_card.html", Availability: AvailabilityBuiltin,
			Tags: []string{"kuaishou", "卡片"}},
		{ID: "card-chat", Kind: EntryKindTemplate, Name: "卡片聊天页（四平台统一）", Category: "channel_card",
			Summary: "抖音/快手/小红书/闲鱼四平台共用的卡片聊天落地页模板",
			Source:  "internal/template/card_chat.html（reach/card/template.GenerateCardChatPage）", Availability: AvailabilityBuiltin,
			Tags: []string{"chat", "卡片"}},
		{ID: "live-code", Kind: EntryKindTemplate, Name: "活码落地页", Category: "live_code",
			Summary: "活码展示页模板，含点击统计与二维码区块",
			Source:  "internal/reach/card/template/live_code.html（GenerateLiveCodePage）", Availability: AvailabilityBuiltin,
			Tags: []string{"活码", "二维码"}},
		{ID: "sop-reply", Kind: EntryKindTemplate, Name: "SOP 话术回复模板", Category: "reply",
			Summary: "按意图/阶段拼接的客服回复话术模板（Go text/template）",
			Source:  "model/sop_templates（service.SOPTemplateService）", Availability: AvailabilityNeedsModule,
			Tags: []string{"sop", "话术"}},
		{ID: "integration-erp", Kind: EntryKindTemplate, Name: "ERP/CRM 对接模板", Category: "integration",
			Summary: "第三方系统对接的预置字段映射与端点模板",
			Source:  "model/integration_templates", Availability: AvailabilityBuiltin,
			Tags: []string{"erp", "crm", "对接"}},
		{ID: "whatsapp-message", Kind: EntryKindTemplate, Name: "WhatsApp 消息模板", Category: "whatsapp",
			Summary: "WhatsApp 渠道消息模板（需开通渠道能力后使用）",
			Source:  "model/whatsapp_message_template", Availability: AvailabilityNeedsModule,
			Tags: []string{"whatsapp", "消息"}},

		// ── 技能 ─────────────────────────────────────────────────────
		{ID: "skill-mcp-tool", Kind: EntryKindSkill, Name: "MCP 工具接入", Category: "mcp",
			Summary: "通过 MCP 协议接入外部工具，供智能体调用",
			Source:  "internal/middleware/mcp_guard.go", Availability: AvailabilityBuiltin,
			Tags: []string{"mcp", "工具"}},
		{ID: "skill-smart-router", Kind: EntryKindSkill, Name: "智能路由", Category: "routing",
			Summary: "按技能/负载/可用性为会话挑选坐席或智能体",
			Source:  "internal/service/smart_router.go", Availability: AvailabilityNeedsModule,
			Tags: []string{"routing", "智能体"}},
		{ID: "skill-assignment", Kind: EntryKindSkill, Name: "会话智能分配", Category: "assignment",
			Summary: "按策略把会话自动分配给合适的坐席",
			Source:  "internal/service/assignment.go", Availability: AvailabilityNeedsModule,
			Tags: []string{"assignment", "坐席"}},
		{ID: "skill-agentic-ltc", Kind: EntryKindSkill, Name: "Agentic Lead-to-Cash 闭环", Category: "agentic",
			Summary: "AI 报价→改价确认→自动转订单/回款的全流程闭环（骨架已存，闭环未实挂）",
			Source:  "internal/middleware/ltc_gate*.go（开发计划 D1）", Availability: AvailabilityComingSoon,
			Tags: []string{"ltc", "agentic"}},
	}
	// 种子顺序即默认排序：模板在前、技能在后，同类相邻；ID 必须唯一，由测试钉住。
	return seed
}
