package controller

import (
	"hivemtk-user/internal/pkg/errhttp"
	"net/http"
	"strconv"

	"hivemtk-user/internal/content/dto"
	"hivemtk-user/internal/content/service"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

type MaterialController struct {
	service service.MaterialService
}

func NewMaterialController(service service.MaterialService) *MaterialController {
	return &MaterialController{service: service}
}

// GetMaterialList 获取素材列表
func (c *MaterialController) GetMaterialList(ctx *gin.Context) {
	categoryID := ctx.Query("category_id")
	if categoryID == "" {
		categoryID = ctx.Query("categoryId")
	}
	materialType := ctx.Query("type")
	search := ctx.Query("search")
	if search == "" {
		search = ctx.Query("keyword")
	}
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "20"))

	licenseID := ctx.GetString("license_id")
	if licenseID == "" {
		response.Error(ctx, http.StatusUnauthorized, "未授权访问")
		return
	}

	resp, err := c.service.GetMaterialList(licenseID, categoryID, materialType, search, page, limit)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "获取素材列表失败")
		return
	}
	response.Success(ctx, resp, "success")
}

// GetMaterialByID 获取素材详情
func (c *MaterialController) GetMaterialByID(ctx *gin.Context) {
	materialID := ctx.Param("id")
	if materialID == "" {
		response.Error(ctx, http.StatusBadRequest, "素材ID不能为空")
		return
	}

	material, err := c.service.GetMaterial(materialID)
	if errhttp.HandleDBError(ctx, err, "获取素材详情") {
		return
	}
	response.Success(ctx, material, "success")
}

// GetMaterialCategoryByID 获取素材分类详情
func (c *MaterialController) GetMaterialCategoryByID(ctx *gin.Context) {
	categoryID := ctx.Param("id")
	if categoryID == "" {
		response.Error(ctx, http.StatusBadRequest, "分类ID不能为空")
		return
	}

	category, err := c.service.GetCategory(categoryID)
	if errhttp.HandleDBError(ctx, err, "获取素材分类详情") {
		return
	}
	response.Success(ctx, category, "success")
}

// UpdateMaterialUsage 更新素材使用次数
func (c *MaterialController) UpdateMaterialUsage(ctx *gin.Context) {
	materialID := ctx.Param("id")
	if materialID == "" {
		response.Error(ctx, http.StatusBadRequest, "素材ID不能为空")
		return
	}

	licenseID := ctx.GetString("license_id")
	if licenseID == "" {
		response.Error(ctx, http.StatusUnauthorized, "未授权访问")
		return
	}

	err := c.service.UpdateMaterialUsage(materialID)
	if errhttp.HandleDBError(ctx, err, "更新素材使用次数") {
		return
	}
	response.Success(ctx, nil, "更新成功")
}

// GetMaterialStats 获取素材统计信息
func (c *MaterialController) GetMaterialStats(ctx *gin.Context) {
	licenseID := ctx.GetString("license_id")
	if licenseID == "" {
		response.Error(ctx, http.StatusUnauthorized, "未授权访问")
		return
	}

	stats, err := c.service.GetMaterialStats(licenseID)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "获取素材统计失败")
		return
	}
	response.Success(ctx, stats, "success")
}

// UploadMaterial 上传素材
func (c *MaterialController) UploadMaterial(ctx *gin.Context) {
	licenseID := ctx.GetString("license_id")
	if licenseID == "" {
		response.Error(ctx, http.StatusUnauthorized, "未授权访问")
		return
	}

	categoryID := ctx.PostForm("category_id")
	if categoryID == "" {
		response.Error(ctx, http.StatusBadRequest, "请选择素材分类")
		return
	}

	file, header, err := ctx.Request.FormFile("file")
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "请上传文件")
		return
	}
	defer func() { _ = file.Close() }()

	if header.Size > 10*1024*1024 {
		response.Error(ctx, http.StatusBadRequest, "文件大小不能超过10MB")
		return
	}

	userID := ""
	if uid, exists := ctx.Get("user_id"); exists {
		if v, ok := uid.(uint); ok {
			userID = strconv.FormatUint(uint64(v), 10)
		} else if v, ok := uid.(float64); ok {
			userID = strconv.FormatUint(uint64(v), 10)
		}
	}
	uploadReq := &dto.UploadMaterialRequest{
		CategoryID: categoryID,
		Name:       header.Filename,
		LicenseID:  licenseID,
		UserID:     userID,
	}

	material, err := c.service.UploadMaterial(file, header, uploadReq)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "上传失败: "+err.Error())
		return
	}
	response.Success(ctx, material, "上传成功")
}

// DeleteMaterial 删除素材
func (c *MaterialController) DeleteMaterial(ctx *gin.Context) {
	materialID := ctx.Param("id")
	if materialID == "" {
		response.Error(ctx, http.StatusBadRequest, "素材ID不能为空")
		return
	}

	err := c.service.DeleteMaterial(materialID)
	if errhttp.HandleDBError(ctx, err, "删除素材") {
		return
	}
	response.Success(ctx, gin.H{"deleted": true}, "删除成功")
}

// GetMaterialCategories 获取素材分类列表
func (c *MaterialController) GetMaterialCategories(ctx *gin.Context) {
	licenseID := ctx.GetString("license_id")
	if licenseID == "" {
		response.Success(ctx, []any{}, "success")
		return
	}

	parentID := ctx.Query("parent_id")
	materialType := ctx.Query("type")
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "50"))

	categories, err := c.service.GetCategoryList(licenseID, parentID, materialType, page, limit)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "获取分类失败")
		return
	}
	response.Success(ctx, categories, "success")
}

// CreateMaterialCategory 创建素材分类
func (c *MaterialController) CreateMaterialCategory(ctx *gin.Context) {
	var req dto.CreateMaterialCategoryRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "参数错误")
		return
	}

	licenseID := ctx.GetString("license_id")
	if licenseID == "" {
		response.Error(ctx, http.StatusUnauthorized, "未授权访问")
		return
	}

	userID := ""
	if uid, exists := ctx.Get("user_id"); exists {
		if v, ok := uid.(uint); ok {
			userID = strconv.FormatUint(uint64(v), 10)
		} else if v, ok := uid.(float64); ok {
			userID = strconv.FormatUint(uint64(v), 10)
		}
	}
	req.LicenseID = licenseID
	req.UserID = userID

	category, err := c.service.CreateCategory(&req)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "创建失败")
		return
	}
	response.Success(ctx, category, "创建成功")
}

// UpdateMaterialCategory 更新素材分类
func (c *MaterialController) UpdateMaterialCategory(ctx *gin.Context) {
	categoryID := ctx.Param("id")
	if categoryID == "" {
		response.Error(ctx, http.StatusBadRequest, "分类ID不能为空")
		return
	}

	var req dto.UpdateMaterialCategoryRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "参数错误")
		return
	}

	category, err := c.service.UpdateCategory(categoryID, &req)
	if errhttp.HandleDBError(ctx, err, "更新素材分类") {
		return
	}
	response.Success(ctx, category, "更新成功")
}

// DeleteMaterialCategory 删除素材分类
func (c *MaterialController) DeleteMaterialCategory(ctx *gin.Context) {
	categoryID := ctx.Param("id")
	if categoryID == "" {
		response.Error(ctx, http.StatusBadRequest, "分类ID不能为空")
		return
	}

	err := c.service.DeleteCategory(categoryID)
	if errhttp.HandleDBError(ctx, err, "删除素材分类") {
		return
	}
	response.Success(ctx, gin.H{"deleted": true}, "删除成功")
}

// GetMaterialSelector 获取素材选择器数据（用于前端选择素材）
func (c *MaterialController) GetMaterialSelector(ctx *gin.Context) {
	licenseID := ctx.GetString("license_id")
	if licenseID == "" {
		response.Error(ctx, http.StatusUnauthorized, "未授权访问")
		return
	}

	materialType := ctx.Query("type")
	resp, err := c.service.GetMaterialSelector(licenseID, materialType)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "获取素材选择器失败")
		return
	}
	response.Success(ctx, resp, "success")
}
