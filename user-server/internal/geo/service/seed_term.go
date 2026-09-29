package service

import (
	"context"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"
	"hivemtk-user/internal/pkg/utils/logger"
)

// SeedTermService 关键词种子词条服务：DB 优先，硬编码只做缺省播种包。
//
// 独立部署首轮运行时表为空 → 用 TechStackAttrs/BusinessLines/品牌组合
// 自动播种后返回；之后 DB 即唯一真实来源，可经 /geo/seed-terms 维护。
// DB 不可读时 fail-open 回硬编码，保证管线不断链。
type SeedTermService struct {
	repo repository.GeoSeedTermRepository
}

func NewSeedTermService(repo repository.GeoSeedTermRepository) *SeedTermService {
	return &SeedTermService{repo: repo}
}

// ListTerms 管理端查看全部启用词条
func (s *SeedTermService) ListTerms(ctx context.Context) ([]*model.GeoSeedTerm, error) {
	return s.repo.ListActive()
}

// ReseedDefaults 幂等重播缺省包（新增缺省词会上架，已有词条不删除）
func (s *SeedTermService) ReseedDefaults(ctx context.Context) (int, error) {
	return s.repo.UpsertBatch(buildDefaultSeedRows())
}

// LoadSeedMatrix 加载种子矩阵：DB 优先，空表自动播种，DB 故障回硬编码。
// 返回与 BuildBusinessSeedMatrix 同构（seeds 去重保序 + seed→业务线映射）。
func (s *SeedTermService) LoadSeedMatrix(ctx context.Context) ([]string, map[string]string) {
	rows, err := s.repo.ListActive()
	if err != nil {
		logger.Warnf("[GEO seed_term] 种子词表读取失败，fail-open 用硬编码缺省: %v", err)
		return BuildBusinessSeedMatrix()
	}
	if len(rows) == 0 {
		if n, sErr := s.repo.UpsertBatch(buildDefaultSeedRows()); sErr != nil {
			logger.Warnf("[GEO seed_term] 缺省播种失败，用内存缺省继续: %v", sErr)
		} else {
			logger.Infof("[GEO seed_term] 空表自动播种 %d 词条", n)
		}
		return BuildBusinessSeedMatrix()
	}

	var techAttrs []string
	coreByLine := map[string][]string{}
	var brandCombos map[string]string
	for _, r := range rows {
		switch r.Category {
		case model.SeedTermCategoryTechAttr:
			techAttrs = append(techAttrs, r.Term)
		case model.SeedTermCategoryCoreTerm:
			coreByLine[r.BusinessLine] = append(coreByLine[r.BusinessLine], r.Term)
		case model.SeedTermCategoryBrandCombo:
			if brandCombos == nil {
				brandCombos = map[string]string{}
			}
			brandCombos[r.Term] = r.BusinessLine
		}
	}
	if len(techAttrs) == 0 {
		techAttrs = TechStackAttrs
	}
	lines := make([]BusinessLine, len(BusinessLines))
	copy(lines, BusinessLines)
	for i := range lines {
		if cores, ok := coreByLine[lines[i].Name]; ok && len(cores) > 0 {
			lines[i].CoreTerms = cores
		}
	}
	return buildSeedMatrix(techAttrs, lines, brandCombos)
}

// buildDefaultSeedRows 缺省播种包（与 buildSeedMatrix 的派生口径一致，sort 保序）
func buildDefaultSeedRows() []*model.GeoSeedTerm {
	rows := make([]*model.GeoSeedTerm, 0, 256)
	sort := 0
	for _, t := range TechStackAttrs {
		rows = append(rows, &model.GeoSeedTerm{
			Term: t, Category: model.SeedTermCategoryTechAttr,
			Source: "builtin:TechStackAttrs", Active: true, Sort: sort,
		})
		sort++
	}
	for _, bl := range BusinessLines {
		for _, core := range bl.CoreTerms {
			rows = append(rows, &model.GeoSeedTerm{
				Term: core, Category: model.SeedTermCategoryCoreTerm,
				BusinessLine: bl.Name, Source: "builtin:BusinessLines",
				Active: true, Sort: sort,
			})
			sort++
		}
	}
	for _, bl := range BusinessLines {
		brandTerm := DefaultBrandName + bl.ShortName
		if bl.ShortName == "GEO" {
			brandTerm = DefaultBrandName + " GEO"
		}
		rows = append(rows, &model.GeoSeedTerm{
			Term: brandTerm, Category: model.SeedTermCategoryBrandCombo,
			BusinessLine: bl.Name, Source: "builtin:brand",
			Active: true, Sort: sort,
		})
		sort++
	}
	if len(BusinessLines) > 1 {
		for _, core := range BusinessLines[1].CoreTerms {
			rows = append(rows, &model.GeoSeedTerm{
				Term: DefaultBrandName + " " + core, Category: model.SeedTermCategoryBrandCombo,
				BusinessLine: BusinessLines[1].Name, Source: "builtin:brand",
				Active: true, Sort: sort,
			})
			sort++
		}
	}
	return rows
}
