// Package dto 提供 AssetBundle（资产包）的 API 传输对象，统一开发者模式（messages 数组直传）与商户低代码表单两套视图的出入参契约。
package dto

import (
	"time"
)

// AssetBundleMessage 资产包内的单条消息（OpenAI ChatML 协议，dto 自有类型）
// 与 model.AssetBundleMessage 字段一致，由 service 层 mapper 互转
type AssetBundleMessage struct {
	Role       string `json:"role"`
	Content    string `json:"content"`
	ToolCallID string `json:"tool_call_id,omitempty"`
	Name       string `json:"name,omitempty"`
}

// AssetBundleCreateRequest 创建资产包请求
type AssetBundleCreateRequest struct {
	AssetID     string               `json:"asset_id" binding:"required"`
	Title       string               `json:"title" binding:"required"`
	Description string               `json:"description"`
	Author      string               `json:"author"`
	Version     string               `json:"version"`
	Scope       string               `json:"scope"`
	Industry    string               `json:"industry"`
	Language    string               `json:"language"`
	Tags        []string             `json:"tags"`
	Messages    []AssetBundleMessage `json:"messages" binding:"required"`
}

// AssetBundleUpdateRequest 更新资产包请求
type AssetBundleUpdateRequest struct {
	// ID 由路由参数 :id 注入（见 controller.Update），因此 body 里不强制携带；
	// 曾用 binding:"required" 强制 body 回传 id，违反 REST 语义，外部客户端必踩。
	ID          int64                `json:"id"`
	Title       string               `json:"title"`
	Description string               `json:"description"`
	Author      string               `json:"author"`
	Version     string               `json:"version"`
	Scope       string               `json:"scope"`
	Industry    string               `json:"industry"`
	Language    string               `json:"language"`
	Tags        []string             `json:"tags"`
	Messages    []AssetBundleMessage `json:"messages"`
	Status      string               `json:"status"`
	CoverImage  string               `json:"cover_image"`
	ChangeNote  string               `json:"change_note"`
}

// AssetBundleListRequest 列表查询请求
type AssetBundleListRequest struct {
	Keyword  string   `json:"keyword" form:"keyword"`
	Author   string   `json:"author" form:"author"`
	Industry string   `json:"industry" form:"industry"`
	Language string   `json:"language" form:"language"`
	Scope    string   `json:"scope" form:"scope"`
	Status   string   `json:"status" form:"status"`
	Tags     []string `json:"tags" form:"tags"`
	Page     int      `json:"page" form:"page"`
	Size     int      `json:"size" form:"size"`
}

// AssetBundleView 资产包响应视图（镜像 model.AssetBundle 的 JSON 字段，不嵌入实体）
type AssetBundleView struct {
	ID                 int64                `json:"id"`
	AssetID            string               `json:"asset_id"`
	Title              string               `json:"title"`
	Description        string               `json:"description"`
	Author             string               `json:"author"`
	Version            string               `json:"version"`
	Scope              string               `json:"scope"`
	Status             string               `json:"status"`
	Industry           string               `json:"industry"`
	Language           string               `json:"language"`
	Tags               []string             `json:"tags"`
	Messages           []AssetBundleMessage `json:"messages"`
	Examples           []any                `json:"examples"`
	SupportedLanguages []string             `json:"supported_languages"`
	UseCount           int64                `json:"use_count"`
	Rating             float64              `json:"rating"`
	RatingCount        int                  `json:"rating_count"`
	CoverImage         string               `json:"cover_image"`
	CreatedAt          time.Time            `json:"created_at"`
	UpdatedAt          time.Time            `json:"updated_at"`
}

// AssetBundleListResponse 列表响应
type AssetBundleListResponse struct {
	List  []*AssetBundleView `json:"list"`
	Total int64              `json:"total"`
	Page  int                `json:"page"`
	Size  int                `json:"size"`
}

// MerchantFormSaveRequest 商户低代码表单保存请求。
// 商户在前端看到的是表单（活动名称/优惠比例/常见问答卡片），后端将其反向翻译成标准 messages 数组。
type MerchantFormSaveRequest struct {
	AssetID         string             `json:"asset_id" binding:"required"`
	Title           string             `json:"title"`
	Author          string             `json:"author"`
	ShopName        string             `json:"shop_name"`
	CampaignName    string             `json:"campaign_name"`
	DiscountPct     string             `json:"discount_pct"`
	SupportContact  string             `json:"support_contact"`
	CrisisThreshold string             `json:"crisis_threshold"`
	ToneLevel       string             `json:"tone_level"`
	CensorshipLevel string             `json:"censorship_level"`
	EnabledIntents  []string           `json:"enabled_intents"`
	QACards         []MerchantQACard   `json:"qa_cards"`
	CardConfig      MerchantCardConfig `json:"card_config"`
	TemplateAssetID string             `json:"template_asset_id"`
}

// MerchantQACard 常见问答卡片
type MerchantQACard struct {
	ID          string `json:"id"`
	Trigger     string `json:"trigger"`
	UserExample string `json:"user_example"`
	Reply       string `json:"reply"`
	Order       int    `json:"order"`
}

// MerchantCardConfig 乐高卡片配置
type MerchantCardConfig struct {
	IntentType   string               `json:"intent_type"`
	ProductImage string               `json:"product_image"`
	Buttons      []MerchantCardButton `json:"buttons"`
}

// MerchantCardButton 卡片按钮
type MerchantCardButton struct {
	Title   string `json:"title"`
	Action  string `json:"action"`
	URL     string `json:"url,omitempty"`
	APIName string `json:"api_name,omitempty"`
	APIArgs string `json:"api_args,omitempty"`
	Order   int    `json:"order"`
}

// MerchantFormParseResponse 解析 messages 数组后的商户表单响应，供前端回显。
// 前端用正则解析 messages[0].content 提取参数。
type MerchantFormParseResponse struct {
	ShopName        string             `json:"shop_name"`
	CampaignName    string             `json:"campaign_name"`
	DiscountPct     string             `json:"discount_pct"`
	SupportContact  string             `json:"support_contact"`
	QACards         []MerchantQACard   `json:"qa_cards"`
	CardConfig      MerchantCardConfig `json:"card_config"`
	EnabledIntents  []string           `json:"enabled_intents"`
	CrisisThreshold string             `json:"crisis_threshold"`
	ToneLevel       string             `json:"tone_level"`
	CensorshipLevel string             `json:"censorship_level"`
}

// WeaveRequest Weave 算法请求
type WeaveRequest struct {
	AssetID      string               `json:"asset_id" binding:"required"`
	UserQuery    string               `json:"user_query" binding:"required"`
	RAGDocs      []RAGDocumentDTO     `json:"rag_docs"`
	ChatHistory  []AssetBundleMessage `json:"chat_history"`
	MerchantVars map[string]string    `json:"merchant_vars"`
	Options      *WeaveOptionsDTO     `json:"options"`
	Sandbox      bool                 `json:"sandbox"`
}

// WeaveResponse Weave 响应
type WeaveResponse struct {
	Messages     []AssetBundleMessage `json:"messages"`
	ResultLength int                  `json:"result_length"`
	Stats        WeaveStats           `json:"stats"`
}

// WeaveStats 织布统计
type WeaveStats struct {
	AssetMessages    int `json:"asset_messages"`
	RAGMessages      int `json:"rag_messages"`
	HistoryMessages  int `json:"history_messages"`
	FinalTotal       int `json:"final_total"`
	StrippedFewShots int `json:"stripped_few_shots"`
}

// RAGDocumentDTO RAG 文档传输对象
type RAGDocumentDTO struct {
	ID      string  `json:"id"`
	Title   string  `json:"title"`
	Content string  `json:"content"`
	Score   float64 `json:"score"`
	Source  string  `json:"source"`
}

// WeaveOptionsDTO Weave 策略 DTO
type WeaveOptionsDTO struct {
	RAGPosition         string `json:"rag_position"`
	MaxHistoryMessages  int    `json:"max_history_messages"`
	StripFewShotJSON    bool   `json:"strip_few_shot_json"`
	IncludeMerchantVars bool   `json:"include_merchant_vars"`
}
