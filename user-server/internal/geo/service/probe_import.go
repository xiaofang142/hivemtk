package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"hivemtk-user/internal/geo/model"
)

// ProbeImportRow 人工导入的一行真实探针结果（第三方 AI 可见度工具/人工复核）。
type ProbeImportRow struct {
	Engine         string   `json:"engine"`
	Query          string   `json:"query"`
	Response       string   `json:"response"`
	Citations      []string `json:"citations"`
	BrandMentioned *bool    `json:"brand_mentioned"`
	Sentiment      string   `json:"sentiment"`
}

// ImportRuns 把人工导入结果写成 mode=manual 的探针行。
//
// 去重：同一批内按 (engine, query, response) 哈希去重；跨批次的历史重复
// 不做静默合并——report 需要看到导入了几次。必填缺失/写库失败逐行跳过并回报。
func (s *ProbeService) ImportRuns(ctx context.Context, rows []ProbeImportRow) (created, skipped int, errs []string) {
	seen := map[string]bool{}
	brandNames := s.getBrandNames(ctx)
	for i, row := range rows {
		engine := strings.TrimSpace(row.Engine)
		query := strings.TrimSpace(row.Query)
		response := strings.TrimSpace(row.Response)
		if engine == "" || query == "" || response == "" {
			skipped++
			errs = append(errs, fmt.Sprintf("第 %d 行缺少 engine/query/response", i+1))
			continue
		}
		key := probeImportKey(engine, query, response)
		if seen[key] {
			skipped++
			errs = append(errs, fmt.Sprintf("第 %d 行与本批重复", i+1))
			continue
		}
		seen[key] = true

		brandHit := matchBrandNames(response, brandNames)
		if row.BrandMentioned != nil {
			brandHit = *row.BrandMentioned
		}
		sentiment := strings.ToLower(strings.TrimSpace(row.Sentiment))
		switch sentiment {
		case "positive", "neutral", "negative":
		default:
			sentiment = "neutral"
		}
		var citesJSON []byte
		if len(row.Citations) > 0 {
			cites := make([]Citation, 0, len(row.Citations))
			for _, u := range row.Citations {
				if strings.TrimSpace(u) != "" {
					cites = append(cites, Citation{URL: strings.TrimSpace(u)})
				}
			}
			if len(cites) > 0 {
				citesJSON, _ = json.Marshal(cites)
			}
		}
		run := &model.GeoProbeRun{
			Engine: engine, Mode: ProbeModeManual, Query: query, Response: response,
			Citations: citesJSON, Sentiment: sentiment, BrandMentioned: brandHit,
		}
		if err := s.repo.Create(ctx, run); err != nil {
			skipped++
			errs = append(errs, fmt.Sprintf("第 %d 行写库失败: %v", i+1, err))
			continue
		}
		created++
	}
	return created, skipped, errs
}

func probeImportKey(engine, query, response string) string {
	sum := sha256.Sum256([]byte(engine + "\x00" + query + "\x00" + response))
	return hex.EncodeToString(sum[:])
}
