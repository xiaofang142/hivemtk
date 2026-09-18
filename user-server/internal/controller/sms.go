package controller

import (
	"net/http"
	"strconv"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/pkg/utils/pagination"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// SmsController 短信控制器
type SmsController struct {
	service service.SmsService
}

// NewSmsController 创建短信控制器
func NewSmsController(service service.SmsService) *SmsController {
	return &SmsController{service: service}
}

// RegisterRoutes 注册路由
func (c *SmsController) RegisterRoutes(router *gin.RouterGroup) {
	sms := router.Group("/sms")
	{
		sms.GET("/config", c.GetConfig)
		sms.POST("/config", c.SaveConfig)

		sms.GET("/list", c.GetSmsList)
		sms.GET("/detail/:id", c.GetSmsDetail)
		sms.POST("/send", c.SendSms)
		sms.POST("/resend/:id", c.ResendSms)

		sms.GET("/draft/list", c.GetDraftList)
		sms.GET("/draft/:id", c.GetDraft)
		sms.POST("/draft", c.CreateDraft)
		sms.PUT("/draft/:id", c.UpdateDraft)
		sms.DELETE("/draft/:id", c.DeleteDraft)
		sms.POST("/draft/send/:id", c.SendDraft)
		sms.POST("/draft/:id/send", c.SendDraft)

		sms.GET("/job/list", c.GetJobList)
		sms.GET("/job/:id", c.GetJob)
		sms.POST("/job", c.CreateJob)
		sms.POST("/job/pause/:id", c.PauseJob)
		sms.POST("/job/resume/:id", c.ResumeJob)
		sms.POST("/job/stop/:id", c.StopJob)
		sms.POST("/job/:id/pause", c.PauseJob)
		sms.POST("/job/:id/resume", c.ResumeJob)
		sms.POST("/job/:id/stop", c.StopJob)
		sms.DELETE("/job/:id", c.DeleteJob)
		sms.GET("/job/:id/records", c.GetJobRecords)
	}
}

// GetConfig godoc
// @Summary      获取短信配置
// @Description  读取当前商户的短信通道配置（签名、通道、限流）
// @Tags         SMS
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  response.Response  "成功"
// @Router       /api/sms/config [get]
func (c *SmsController) GetConfig(ctx *gin.Context) {
	config, err := c.service.GetConfig(ctx.Request.Context())
	if err == nil && config != nil {
		maskSMSConfig(config)
	}
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取配置失败: "+err.Error())
		return
	}

	response.Success(ctx, config, "success")
}

// SaveConfig godoc
// @Summary      保存短信配置
// @Description  更新商户的短信通道配置
// @Tags         SMS
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body  dto.SmsConfigRequest  true  "短信配置"
// @Success      200   {object}  response.Response  "保存成功"
// @Router       /api/sms/config [post]
func (c *SmsController) SaveConfig(ctx *gin.Context) {
	var req dto.SmsConfigRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}

	if err := c.service.SaveConfig(ctx.Request.Context(), &req); err != nil {
		response.ErrorFromDB(ctx, err, "保存配置失败: "+err.Error())
		return
	}

	response.Success(ctx, nil, "success")
}

// DeleteJob godoc
// @Summary      删除短信任务
// @Description  根据 ID 软删除短信任务
// @Tags         SMS
// @Produce      json
// @Security     BearerAuth
// @Param        id  path  int  true  "任务 ID"
// @Success      200  {object}  response.Response  "删除成功"
// @Router       /api/sms/job/{id} [delete]
func (c *SmsController) DeleteJob(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的ID")
		return
	}

	if HandleDBError(ctx, c.service.DeleteJob(ctx.Request.Context(), uint(id)), "删除任务") {
		return
	}

	response.Success(ctx, nil, "success")
}

// GetJobRecords godoc
// @Summary      短信任务执行记录
// @Description  分页查询任务下每条短信的发送结果
// @Tags         SMS
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int  true   "任务 ID"
// @Param        page  query int  false  "页码"  default(1)
// @Param        limit query int  false  "每页"  default(20)
// @Success      200   {object}  response.Response  "成功"
// @Router       /api/sms/job/{id}/records [get]
func (c *SmsController) GetJobRecords(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的ID")
		return
	}

	page, limit, err := pagination.Parse(ctx)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	records, total, err := c.service.GetJobRecords(ctx.Request.Context(), uint(id), page, limit)
	if HandleDBError(ctx, err, "获取任务记录") {
		return
	}

	response.Success(ctx, gin.H{
		"list":  records,
		"total": total,
	}, "success")
}

// GetSmsList 获取短信列表
func (c *SmsController) GetSmsList(ctx *gin.Context) {
	var req dto.SmsListRequest

	page, limit, err := pagination.Parse(ctx)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	phone := ctx.Query("phone")
	status := ctx.Query("status")
	startDate := ctx.Query("startDate")
	endDate := ctx.Query("endDate")

	req = dto.SmsListRequest{
		Page:      page,
		Limit:     limit,
		Phone:     phone,
		Status:    status,
		StartDate: startDate,
		EndDate:   endDate,
	}

	list, total, err := c.service.GetSmsList(ctx.Request.Context(), &req)
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取短信列表失败: "+err.Error())
		return
	}

	response.Success(ctx, gin.H{
		"list":  list,
		"total": total,
	}, "success")
}

// GetSmsDetail 获取短信详情
func (c *SmsController) GetSmsDetail(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的ID")
		return
	}

	sms, err := c.service.GetSmsByID(ctx.Request.Context(), uint(id))
	if HandleDBError(ctx, err, "获取短信详情") {
		return
	}

	response.Success(ctx, sms, "success")
}

// SendSms 发送短信
func (c *SmsController) SendSms(ctx *gin.Context) {
	var req dto.SmsSendRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}

	config, err := c.service.GetConfig(ctx.Request.Context())
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "SMS service not configured")
		return
	}
	configured, err := c.service.IsProviderConfigured(ctx.Request.Context(), config.DefaultProvider)
	if err != nil || !configured {
		response.Error(ctx, http.StatusBadRequest, "SMS service not configured")
		return
	}

	if err := c.service.SendSms(ctx.Request.Context(), &req); err != nil {
		response.ErrorFromDB(ctx, err, "发送短信失败: "+err.Error())
		return
	}

	response.Success(ctx, nil, "success")
}

// ResendSms 重发短信
func (c *SmsController) ResendSms(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的ID")
		return
	}

	if HandleServiceError(ctx, c.service.ResendSms(ctx.Request.Context(), uint(id))) {
		return
	}

	response.Success(ctx, nil, "success")
}

// GetDraftList 获取草稿列表
func (c *SmsController) GetDraftList(ctx *gin.Context) {
	var req dto.SmsDraftListRequest

	page, limit, err := pagination.Parse(ctx)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	title := ctx.Query("title")

	req = dto.SmsDraftListRequest{
		Page:  page,
		Limit: limit,
		Title: title,
	}

	list, total, err := c.service.GetDraftList(ctx.Request.Context(), &req)
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取草稿列表失败: "+err.Error())
		return
	}

	response.Success(ctx, gin.H{
		"list":  list,
		"total": total,
	}, "success")
}

// GetDraft 获取草稿详情
func (c *SmsController) GetDraft(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的ID")
		return
	}

	draft, err := c.service.GetDraftByID(ctx.Request.Context(), uint(id))
	if HandleDBError(ctx, err, "获取草稿详情") {
		return
	}

	response.Success(ctx, draft, "success")
}

// CreateDraft 创建草稿
func (c *SmsController) CreateDraft(ctx *gin.Context) {
	var req dto.SmsDraftCreateRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}

	if err := c.service.CreateDraft(ctx.Request.Context(), &req); err != nil {
		response.ErrorFromDB(ctx, err, "创建草稿失败: "+err.Error())
		return
	}

	response.Success(ctx, nil, "success")
}

// UpdateDraft 更新草稿
func (c *SmsController) UpdateDraft(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的ID")
		return
	}

	var req dto.SmsDraftUpdateRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}

	if HandleDBError(ctx, c.service.UpdateDraft(ctx.Request.Context(), uint(id), &req), "更新草稿") {
		return
	}

	response.Success(ctx, nil, "success")
}

// DeleteDraft 删除草稿
func (c *SmsController) DeleteDraft(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的ID")
		return
	}

	if HandleDBError(ctx, c.service.DeleteDraft(ctx.Request.Context(), uint(id)), "删除草稿") {
		return
	}

	response.Success(ctx, nil, "success")
}

// SendDraft 发送草稿
func (c *SmsController) SendDraft(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的ID")
		return
	}

	phone := ctx.PostForm("phone")
	if phone == "" {

		var req struct {
			Phone string `json:"phone" binding:"required"`
		}
		if err := ctx.ShouldBindJSON(&req); err != nil {
			response.Error(ctx, http.StatusBadRequest, "手机号不能为空")
			return
		}
		phone = req.Phone
	}

	if HandleDBError(ctx, c.service.SendDraft(ctx.Request.Context(), uint(id), phone), "发送草稿") {
		return
	}

	response.Success(ctx, nil, "success")
}

// GetJobList 获取任务列表
func (c *SmsController) GetJobList(ctx *gin.Context) {
	var req dto.SmsJobListRequest

	page, limit, err := pagination.Parse(ctx)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	status := ctx.Query("status")
	name := ctx.Query("name")

	req = dto.SmsJobListRequest{
		Page:   page,
		Limit:  limit,
		Status: status,
		Name:   name,
	}

	list, total, err := c.service.GetJobList(ctx.Request.Context(), &req)
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取任务列表失败: "+err.Error())
		return
	}

	response.Success(ctx, gin.H{
		"list":  list,
		"total": total,
	}, "success")
}

// GetJob 获取任务详情
func (c *SmsController) GetJob(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的ID")
		return
	}

	job, err := c.service.GetJobByID(ctx.Request.Context(), uint(id))
	if HandleDBError(ctx, err, "获取任务详情") {
		return
	}

	response.Success(ctx, job, "success")
}

// CreateJob 创建任务
func (c *SmsController) CreateJob(ctx *gin.Context) {
	var req dto.SmsJobCreateRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}

	if err := c.service.CreateJob(ctx.Request.Context(), &req); err != nil {
		response.ErrorFromDB(ctx, err, "创建任务失败: "+err.Error())
		return
	}

	response.Success(ctx, nil, "success")
}

// PauseJob 暂停任务
func (c *SmsController) PauseJob(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的ID")
		return
	}

	if HandleDBError(ctx, c.service.PauseJob(ctx.Request.Context(), uint(id)), "暂停任务") {
		return
	}

	response.Success(ctx, nil, "success")
}

// ResumeJob 继续任务
func (c *SmsController) ResumeJob(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的ID")
		return
	}

	if HandleDBError(ctx, c.service.ResumeJob(ctx.Request.Context(), uint(id)), "继续任务") {
		return
	}

	response.Success(ctx, nil, "success")
}

// StopJob 停止任务
func (c *SmsController) StopJob(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的ID")
		return
	}

	if HandleDBError(ctx, c.service.StopJob(ctx.Request.Context(), uint(id)), "停止任务") {
		return
	}

	response.Success(ctx, nil, "success")
}

func maskSMSConfig(config *dto.SmsConfigResponse) {
	mask := func(v string) string {
		if len(v) <= 4 {
			return "****"
		}
		return "****" + v[len(v)-4:]
	}
	config.Aliyun.AccessKeyId = mask(config.Aliyun.AccessKeyId)
	config.Aliyun.AccessKeySecret = mask(config.Aliyun.AccessKeySecret)
	if config.Tencent.SecretId != "" {
		config.Tencent.SecretId = mask(config.Tencent.SecretId)
	}
	if config.Tencent.SecretKey != "" {
		config.Tencent.SecretKey = mask(config.Tencent.SecretKey)
	}
	if config.Huawei.AppKey != "" {
		config.Huawei.AppKey = mask(config.Huawei.AppKey)
	}
	if config.Huawei.AppSecret != "" {
		config.Huawei.AppSecret = mask(config.Huawei.AppSecret)
	}
}
