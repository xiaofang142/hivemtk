// dialogue_eval.go 对话级 gold 评测 HTTP 面（P0-1）。形状对齐 controller/rag_eval.go：
// run/list/get 三端点 + manage 面。
package controller

import (
	"net/http"
	"strconv"

	"hivemtk-user/internal/aiagent/eval"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// DialogueEvalController 对话级评测控制器
type DialogueEvalController struct {
	svc *service.DialogueEvalService
}

// NewDialogueEvalController 构造
func NewDialogueEvalController() *DialogueEvalController {
	return &DialogueEvalController{svc: service.NewDialogueEvalService(service.GlobalBadCaseService())}
}

// RunRequest 运行参数
type DialogueEvalRunRequest struct {
	Name         string `json:"name"`
	BadCaseLimit int    `json:"bad_case_limit"`
	// EvalSetID 非 0 时对导入的评测集跑（与 bad_case 送料并行入口，P0-1 CSV）。
	EvalSetID uint `json:"eval_set_id"`
}

// @Summary     运行对话级 gold 评测（基线+退化双相）
// @Tags      dialogue-eval
// @Router    /api/dialogue-eval/run [post]
// Run POST /api/dialogue-eval/run
// 请求体: {"name": "my_eval", "bad_case_limit": 20}
func (c *DialogueEvalController) Run(ctx *gin.Context) {
	var req DialogueEvalRunRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		req = DialogueEvalRunRequest{}
	}
	runs, err := c.svc.RunGoldEvaluationWithSet(ctx.Request.Context(), req.Name, req.BadCaseLimit, req.EvalSetID)
	if err != nil {
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}
	response.Success(ctx, runs, "评测完成")
}

// @Summary      查询对话评测运行列表
// @Tags      dialogue-eval
// @Router    /api/dialogue-eval/runs [get]
// List GET /api/dialogue-eval/runs?limit=20
func (c *DialogueEvalController) List(ctx *gin.Context) {
	limit := 20
	if v := ctx.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	runs, err := c.svc.ListDialogueEvalRuns(ctx.Request.Context(), limit)
	if err != nil {
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}
	response.Success(ctx, runs, "获取成功")
}

// @Summary      按 ID 查询对话评测运行详情
// @Tags      dialogue-eval
// @Router    /api/dialogue-eval/runs/{id} [get]
// Get GET /api/dialogue-eval/runs/:id
func (c *DialogueEvalController) Get(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 id")
		return
	}
	run, err := c.svc.GetDialogueEvalRun(ctx.Request.Context(), uint(id))
	if err != nil {
		response.Error(ctx, http.StatusNotFound, "运行不存在")
		return
	}
	response.Success(ctx, run, "获取成功")
}

// EvalSetImportRequest CSV 导入请求体
type EvalSetImportRequest struct {
	Name string `json:"name"`
	CSV  string `json:"csv"`
}

// @Summary     导入对话评测集 CSV
// @Tags      dialogue-eval
// @Router    /api/dialogue-eval/sets/import [post]
// ImportSet POST /api/dialogue-eval/sets/import
// 请求体: {"name": "我的题集", "csv": "id,scene,...\n..."}
// 结构错误整体拒绝（不做部分导入）。
func (c *DialogueEvalController) ImportSet(ctx *gin.Context) {
	var req EvalSetImportRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求体格式错误")
		return
	}
	set, err := c.svc.ImportEvalSetCSV(ctx.Request.Context(), req.Name, req.CSV)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, set, "导入成功")
}

// @Summary      评测集列表
// @Tags      dialogue-eval
// @Router    /api/dialogue-eval/sets [get]
// ListSets GET /api/dialogue-eval/sets?limit=20
func (c *DialogueEvalController) ListSets(ctx *gin.Context) {
	limit := 20
	if v := ctx.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	sets, err := c.svc.ListEvalSets(ctx.Request.Context(), limit)
	if err != nil {
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}
	response.Success(ctx, sets, "获取成功")
}

// @Summary      导出评测集 CSV
// @Tags      dialogue-eval
// @Router    /api/dialogue-eval/sets/{id}/export [get]
// ExportSet GET /api/dialogue-eval/sets/:id/export → text/csv 附件
func (c *DialogueEvalController) ExportSet(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 id")
		return
	}
	csvText, _, err := c.svc.ExportEvalSetCSV(ctx.Request.Context(), uint(id))
	if err != nil {
		response.Error(ctx, http.StatusNotFound, err.Error())
		return
	}
	ctx.Header("Content-Disposition", "attachment; filename=\"dialogue_eval_set_"+strconv.FormatUint(id, 10)+".csv\"")
	ctx.Data(http.StatusOK, "text/csv; charset=utf-8", []byte(csvText))
}

// @Summary      CSV 模板下载
// @Tags      dialogue-eval
// @Router    /api/dialogue-eval/sets/template [get]
// Template GET /api/dialogue-eval/sets/template → 一行样例的模板 CSV
func (c *DialogueEvalController) Template(ctx *gin.Context) {
	ctx.Header("Content-Disposition", "attachment; filename=\"dialogue_eval_template.csv\"")
	ctx.Data(http.StatusOK, "text/csv; charset=utf-8", eval.DialogueCSVTemplate())
}
