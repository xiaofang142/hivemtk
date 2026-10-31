package controller

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/dto"
	i18npkg "hivemtk-user/internal/pkg/i18n"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service/translation"
)

// GlossaryController 术语表管理控制器
type GlossaryController struct {
	svc       *translation.GlossaryService
	validator *translation.PostValidator
}

// NewGlossaryController 构造术语表控制器
func NewGlossaryController(svc *translation.GlossaryService) *GlossaryController {
	return &GlossaryController{
		svc:       svc,
		validator: translation.NewPostValidator(),
	}
}

// RegisterRoutes 注册路由
func (ctrl *GlossaryController) RegisterRoutes(router *gin.RouterGroup) {
	g := router.Group("/glossaries")
	{
		g.POST("", ctrl.Create)
		g.GET("", ctrl.List)
		g.GET("/:term_id", ctrl.Get)
		g.PUT("/:term_id", ctrl.Update)
		g.DELETE("/:term_id", ctrl.Delete)
		g.POST("/validate", ctrl.Validate)
	}
}

// @Summary      创建术语表词条
// @Tags      glossary
// @Router    /api/glossaries [post]
// Create 创建术语
// POST /api/glossaries
func (ctrl *GlossaryController) Create(c *gin.Context) {
	var req dto.GlossaryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}
	g := translation.ToGlossaryModel(&req)
	if err := ctrl.svc.Create(c.Request.Context(), g); err != nil {
		response.Error(c, http.StatusBadRequest, "创建失败", err.Error())
		return
	}
	response.Success(c, translation.FromGlossaryModel(g), "创建成功")
}

// @Summary      列表查询（支持 category/status/keyword 过滤 + 分页）
// @Tags      glossary
// @Router    /api/glossaries [get]
// List 列表查询（支持 category/status/keyword 过滤 + 分页）
// GET /api/glossaries?category=&status=&keyword=&page=1&page_size=20
func (ctrl *GlossaryController) List(c *gin.Context) {
	var req dto.GlossaryListRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}
	if req.Page < 1 {
		req.Page = 1
	}
	if req.PageSize < 1 || req.PageSize > 100 {
		req.PageSize = 20
	}

	if req.Category != "" && req.Status == "" && req.Keyword == "" {
		list, err := ctrl.svc.ListByCategory(c.Request.Context(), req.Category)
		if err != nil {
			response.ErrorFromDB(c, err, "查询失败", err.Error())
			return
		}
		response.SuccessWithList(c, translation.FromGlossaryModelList(list), int64(len(list)))
		return
	}

	list, total, err := ctrl.svc.List(c.Request.Context(), req.Status, req.Keyword, req.Page, req.PageSize)
	if err != nil {
		response.ErrorFromDB(c, err, "查询失败", err.Error())
		return
	}
	response.SuccessWithPage(c, translation.FromGlossaryModelList(list), int64(req.Page), int64(req.PageSize), total)
}

// @Summary      按词条 ID 查询术语
// @Tags      glossary
// @Router    /api/glossaries/{term_id} [get]
// Get 查询单条术语
// GET /api/glossaries/:term_id
func (ctrl *GlossaryController) Get(c *gin.Context) {
	termID := c.Param("term_id")
	if termID == "" {
		response.Error(c, http.StatusBadRequest, "term_id 不能为空")
		return
	}
	g, err := ctrl.svc.GetByTermID(c.Request.Context(), termID)
	if err != nil {
		if errors.Is(err, translation.ErrGlossaryNotFound) {
			response.NotFoundError(c, "术语")
			return
		}
		response.ErrorFromDB(c, err, "查询失败", err.Error())
		return
	}
	response.Success(c, translation.FromGlossaryModel(g), "获取成功")
}

// @Summary      更新术语表词条
// @Tags      glossary
// @Router    /api/glossaries/{term_id} [put]
// Update 更新术语
// PUT /api/glossaries/:term_id
func (ctrl *GlossaryController) Update(c *gin.Context) {
	termID := c.Param("term_id")
	if termID == "" {
		response.Error(c, http.StatusBadRequest, "term_id 不能为空")
		return
	}
	existing, err := ctrl.svc.GetByTermID(c.Request.Context(), termID)
	if err != nil {
		if errors.Is(err, translation.ErrGlossaryNotFound) {
			response.NotFoundError(c, "术语")
			return
		}
		response.ErrorFromDB(c, err, "查询失败", err.Error())
		return
	}

	var req dto.GlossaryUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}
	updated := translation.ToGlossaryModelUpdate(&req)
	updated.ID = existing.ID
	updated.TermID = termID
	if updated.Status == "" {
		updated.Status = existing.Status
	}
	if err := ctrl.svc.Update(c.Request.Context(), updated); err != nil {
		response.Error(c, http.StatusBadRequest, "更新失败", err.Error())
		return
	}
	response.Success(c, translation.FromGlossaryModel(updated), "更新成功")
}

// @Summary      删除术语表词条
// @Tags      glossary
// @Router    /api/glossaries/{term_id} [delete]
// Delete 删除术语
// DELETE /api/glossaries/:term_id
func (ctrl *GlossaryController) Delete(c *gin.Context) {
	termID := c.Param("term_id")
	if termID == "" {
		response.Error(c, http.StatusBadRequest, "term_id 不能为空")
		return
	}
	if err := ctrl.svc.Delete(c.Request.Context(), termID); err != nil {
		response.Error(c, http.StatusBadRequest, "删除失败", err.Error())
		return
	}
	response.Success(c, nil, "删除成功")
}

// @Summary 预览某段文本的术语校验结果
// @Tags      glossary
// @Router    /api/glossaries/validate [post]
// Validate 预览某段文本的术语校验结果
// POST /api/glossaries/validate  body: {"text": "...", "lang": "zh"}
func (ctrl *GlossaryController) Validate(c *gin.Context) {
	var req dto.GlossaryValidateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}
	lang := i18npkg.NormalizeLang(req.Lang)
	view, err := ctrl.svc.LoadByLang(c.Request.Context(), lang)
	if err != nil {
		response.ErrorFromDB(c, err, "加载术语表失败", err.Error())
		return
	}
	corrected, issues := ctrl.validator.Validate(req.Text, lang, view)
	resp := &dto.GlossaryValidateResponse{
		OriginalText:  req.Text,
		CorrectedText: corrected,
		Issues:        make([]dto.GlossaryValidateIssue, 0, len(issues)),
	}
	for _, iss := range issues {
		resp.Issues = append(resp.Issues, dto.GlossaryValidateIssue{
			Type:     iss.Type,
			Term:     iss.Term,
			Expected: iss.Expected,
			Actual:   iss.Actual,
		})
	}
	response.Success(c, resp, "校验完成")
}
