package service

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	contentmodel "hivemtk-user/internal/content/model"
	"hivemtk-user/internal/content/repository"
	mainmodel "hivemtk-user/internal/model"
)

// ErrScriptLibraryMissing 读前置与写口之间那一行没了（含「不存在」⇒ 出口 404）。
// 仓储定义、服务层转出，controller 仍可按 service 层哨兵判 404。
var ErrScriptLibraryMissing = repository.ErrScriptLibraryMissing

// ScriptTemplateSyncDeps 同步服务的仓储依赖。
type ScriptTemplateSyncDeps struct {
	TemplateRepo *repository.ScriptTemplateRepository
}

// NewScriptTemplateSyncDeps 由装配处用数据库句柄构造依赖包。
func NewScriptTemplateSyncDeps(db *gorm.DB) ScriptTemplateSyncDeps {
	return ScriptTemplateSyncDeps{TemplateRepo: repository.NewScriptTemplateRepository(db)}
}

// ScriptTemplateSyncService 话术模板 → 运行时话术库 同步服务
type ScriptTemplateSyncService struct {
	templateRepo *repository.ScriptTemplateRepository
}

// NewScriptTemplateSyncService 创建同步服务（依赖由装配处注入），
// 读写都跟着调用方 ctx 走。
func NewScriptTemplateSyncService(deps ScriptTemplateSyncDeps) *ScriptTemplateSyncService {
	return &ScriptTemplateSyncService{templateRepo: deps.TemplateRepo}
}

// SyncToLibraryStats 同步结果统计
type SyncToLibraryStats struct {
	Scanned int `json:"scanned"`
	Created int `json:"created"`
	Updated int `json:"updated"`
	Skipped int `json:"skipped"`
}

// SyncToLibrary 执行 template → library 单向同步（幂等，可重复执行）。
//
// 读取管理端话术模板，逐条按 (category, title) upsert 到 script_library：
//   - 未命中：创建（Tags 由逗号分隔字符串转 JSONArray，Scenario 取 JourneyStage）
//   - 已命中：更新 Content/Scenario/Tags，保留 library 侧 usage_count 等运行时统计
//
// 同步是**每租户各同步一次**的动作：读模板与查重键都由 ctx 决定租户，
// 新建的 library 行由隔离 callback 填上调用方租户。
// 不走 tenant.WithSystemScope：系统作用域会豁免闸，Create 时跳过 tenant_id 填充
// ⇒ 所有租户的话术都落进列默认值 1（默认租户），B 家内容变成 A 家可见。
func (s *ScriptTemplateSyncService) SyncToLibrary(ctx context.Context) (*SyncToLibraryStats, error) {
	if s == nil || s.templateRepo == nil {
		return nil, errors.New("script template sync: service not initialized")
	}
	templates, err := s.templateRepo.ListAll(ctx)
	if err != nil {
		return nil, err
	}

	stats := &SyncToLibraryStats{Scanned: len(templates)}
	for _, t := range templates {
		if strings.TrimSpace(t.Title) == "" || strings.TrimSpace(t.Content) == "" {
			stats.Skipped++
			continue
		}
		created, err := s.templateRepo.UpsertLibrary(ctx, templateToLibrary(t))
		if err != nil {
			return stats, err
		}
		if created {
			stats.Created++
		} else {
			stats.Updated++
		}
	}
	return stats, nil
}

func templateToLibrary(t *contentmodel.ScriptTemplate) *mainmodel.ScriptLibrary {
	return &mainmodel.ScriptLibrary{
		Category: t.Category,
		Title:    t.Title,
		Content:  t.Content,
		Scenario: t.JourneyStage,
		Tags:     parseTagsToJSONArray(t.Tags),
	}
}

func parseTagsToJSONArray(s string) mainmodel.JSONArray {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == '，' || r == ';' || r == '；'
	})
	arr := make(mainmodel.JSONArray, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			arr = append(arr, p)
		}
	}
	return arr
}
