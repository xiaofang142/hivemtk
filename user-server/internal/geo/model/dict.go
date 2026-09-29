package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// GEO 通用字典类目：所有原硬编码数据统一走数据库驱动。
const (
	DictCategoryModelPrices       = "model_prices"       // LLM 模型定价表（原 llm.go modelPrices）
	DictCategoryLongtailTemplates = "longtail_templates" // 长尾词模板（原 keyword_mining.go DefaultLongtailTemplates）
	DictCategoryNegativeSeeds     = "negative_seeds"     // 负面监控种子词（原 scheduler.go negativeSeeds）
	DictCategoryIntentMatrix      = "intent_matrix"      // 意图策略矩阵（原 intent_matrix.go DefaultIntentMatrix）
	DictCategoryCrawlerUAs        = "crawler_uas"        // AI 爬虫 UA 列表（原 monitor_crawler.go aiBotUserAgents）
	DictCategoryKeywordLandings   = "keyword_landings"   // 关键词落地页映射（原 monitor_crawler.go keywordToLandings）
	DictCategoryKeywordWordbanks  = "keyword_wordbanks"  // 关键词组合词库（原 keyword.go wordbanks+patterns）
	DictCategoryPlatformMetas     = "platform_metas"     // 发布平台元数据（原 publish_pipeline.go PlatformMetas）
	DictCategoryPromptTpl         = "prompt_tpl"         // Prompt 模板（原 prompts.go / prompt_manager.go）
	DictCategorySourceLevels      = "source_levels"      // 信源站点等级映射（原 repository/crawler_visit.go domainSourceLevel）
)

// GeoDict GEO 通用配置字典（数据库驱动，替代各 Service 内硬编码数据）。
//
// 设计：同一类目下按 Key 取值，Value 存文本或 JSON；首轮运行时若缺行，
// 由 DictService 用代码内置缺省值自动播种；之后 DB 即唯一真实来源，
// 可经 /geo/dicts 管理端接口查看与维护。DB 故障时 fail-open 走缺省值。
type GeoDict struct {
	ID       string `gorm:"type:varchar(36);primaryKey" json:"id"`
	Category string `gorm:"type:varchar(64);index:idx_dict_cat_key,unique;index" json:"category"`
	Key      string `gorm:"type:varchar(128);index:idx_dict_cat_key,unique" json:"key"`
	Value    string `gorm:"type:text" json:"value"`
	Remark   string `gorm:"type:varchar(500);default:''" json:"remark"`
	Active   bool   `gorm:"default:true;index" json:"active"`
	Sort     int    `gorm:"default:0;index" json:"sort"`

	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (m *GeoDict) TableName() string {
	return "geo_dicts"
}

func (m *GeoDict) BeforeCreate(tx *gorm.DB) error {
	if m.ID == "" {
		m.ID = uuid.NewString()
	}
	return nil
}
