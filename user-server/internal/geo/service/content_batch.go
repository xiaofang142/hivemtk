package service

import (
	"context"

	"hivemtk-user/internal/geo/dto"
	"hivemtk-user/internal/geo/model"
)

// content_batch.go —— B2 批量生成（2026-10-24）。
//
// 规划 §4.8 B2：后端批量任务 + 前端选择/进度/结果；与现有单条生成共用服务。
//
// 形态选择：**同步有界批量**而不是异步任务表。理由——单条 GenerateContent 是
// 秒级 LLM 调用（heuristic 降级则毫秒级），批上限 20 时最坏 ~2-3 分钟，在 HTTP
// 超时内可收口；异步任务表要引入 geo_job_runs 之外的第二轮调度/查询/取消面，
// 与 keywordPipelineJob 的有界批重复。批次进度用逐条结果数组回传（前端逐条渲染），
// 失败单条容错继续（与 keywordPipelineJob 同口径）——一条失败不拖死整批。
// 真正的"后台大批量"由 keyword_pipeline cron 承担，本接口面向运营页面的"选 3-20 条
// 立即生成"场景。

// BatchGenerateMaxKeywords 单批关键词上限。
// 上限即护栏：同步请求里塞 500 个词等于把 HTTP 连接当任务队列用（占 worker、
// 无取消、无进度持久化）——那种量该走 keyword_pipeline cron。
const BatchGenerateMaxKeywords = 20

// BatchGenerateItem 单条生成结果（成功带 article，失败带 error——逐条容错）。
type BatchGenerateItem struct {
	Keyword string            `json:"keyword"`
	OK      bool              `json:"ok"`
	Article *model.GeoArticle `json:"article,omitempty"`
	Error   string            `json:"error,omitempty"`
}

// BatchGenerateResult 批量生成汇总。
type BatchGenerateResult struct {
	Total       int                 `json:"total"`        // 请求的关键词数
	Succeeded   int                 `json:"succeeded"`    // 成功数
	Failed      int                 `json:"failed"`       // 失败数
	Items       []BatchGenerateItem `json:"items"`        // 逐条结果（前端进度/下钻渲染）
	TruncatedTo int                 `json:"truncated_to"` // 超上限被截断到的数量；0=未截断
}

// GenerateContentBatch 批量生成：对每个关键词跑与单条**同一**的 GenerateContent。
// 逐条容错：一条失败（LLM 挂/写库炸）只记 error 继续下一条。
// 超上限截断并回传 truncated_to——静默砍请求是前端"为什么只出了 20 条"事故的来源。
func (s *ContentService) GenerateContentBatch(ctx context.Context, req dto.BatchGenerateRequest) *BatchGenerateResult {
	res := &BatchGenerateResult{Total: len(req.Keywords)}
	if len(req.Keywords) > BatchGenerateMaxKeywords {
		res.TruncatedTo = BatchGenerateMaxKeywords
		req.Keywords = req.Keywords[:BatchGenerateMaxKeywords]
	}
	res.Items = make([]BatchGenerateItem, 0, len(req.Keywords))
	for _, kw := range req.Keywords {
		item := BatchGenerateItem{Keyword: kw}
		article, err := s.GenerateContent(ctx, req.Model, req.Lang, kw, req.BrandName, req.Advantages, req.WordCount, req.Style)
		if err != nil {
			item.Error = err.Error()
		} else {
			item.OK = true
			item.Article = article
		}
		res.Items = append(res.Items, item)
		if item.OK {
			res.Succeeded++
		} else {
			res.Failed++
		}
	}
	return res
}
