package controller

import (
	"net/http"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// billing_admin.go 计费管理 + 角色-权限覆盖层端点（SaaS P2 首批 / P1-1，2026-10-11）。
//
// 计费（admin 全量 / team_admin 只读本团队）：
//   GET|POST /manage/billing/plans, PUT|DELETE /manage/billing/plans/:id
//   GET /manage/billing/subscriptions, PUT /manage/billing/subscriptions/:teamId, POST .../cancel
//   GET|POST /manage/billing/records, PUT /manage/billing/records/:id/pay|void
//   GET /team/billing（team_admin：本团队订阅 + 账单只读）
// 权限覆盖层（admin）：
//   GET|PUT /manage/role-permissions/:roleCode

var billingSvc = func() *service.BillingAdminService { return service.NewBillingAdminService() }
var rbacAdminSvc = func() *service.RbacAdminService { return service.NewRbacAdminService() }

// ---- 计费方案 CRUD ----

func ListBillingPlans(ctx *gin.Context) {
	svc := billingSvc()
	if !svc.Ready() {
		response.Error(ctx, http.StatusServiceUnavailable, "db 未就绪")
		return
	}
	rows, err := svc.ListPlans(ctx.Request.Context())
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	if rows == nil {
		rows = []model.BillingPlan{}
	}
	response.Success(ctx, gin.H{"list": rows}, "ok")
}

func CreateBillingPlan(ctx *gin.Context) {
	svc := billingSvc()
	if !svc.Ready() {
		response.Error(ctx, http.StatusServiceUnavailable, "db 未就绪")
		return
	}
	var req model.BillingPlan
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求体解析失败: "+err.Error())
		return
	}
	if err := svc.CreatePlan(ctx.Request.Context(), &req); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, gin.H{"plan": req}, "创建成功")
}

func UpdateBillingPlan(ctx *gin.Context) {
	svc := billingSvc()
	if !svc.Ready() {
		response.Error(ctx, http.StatusServiceUnavailable, "db 未就绪")
		return
	}
	id := parseUintOrZero(ctx.Param("id"))
	if id == 0 {
		response.Error(ctx, http.StatusBadRequest, "无效 id")
		return
	}
	var req map[string]any
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求体解析失败: "+err.Error())
		return
	}
	delete(req, "id") // 主键不走 Updates map
	if err := svc.UpdatePlan(ctx.Request.Context(), id, req); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, gin.H{"ok": true}, "已更新")
}

func DeleteBillingPlan(ctx *gin.Context) {
	svc := billingSvc()
	if !svc.Ready() {
		response.Error(ctx, http.StatusServiceUnavailable, "db 未就绪")
		return
	}
	id := parseUintOrZero(ctx.Param("id"))
	if err := svc.DeletePlan(ctx.Request.Context(), id); err != nil {
		response.Error(ctx, http.StatusConflict, err.Error())
		return
	}
	response.Success(ctx, gin.H{"ok": true}, "已删除")
}

// ---- 订阅 ----

func ListBillingSubscriptions(ctx *gin.Context) {
	svc := billingSvc()
	if !svc.Ready() {
		response.Error(ctx, http.StatusServiceUnavailable, "db 未就绪")
		return
	}
	rows, err := svc.ListSubscriptions(ctx.Request.Context())
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	if rows == nil {
		rows = []model.TeamSubscription{}
	}
	response.Success(ctx, gin.H{"list": rows}, "ok")
}

// AssignBillingPlan PUT /manage/billing/subscriptions/:teamId {plan_id, months, auto_renew}
func AssignBillingPlan(ctx *gin.Context) {
	svc := billingSvc()
	if !svc.Ready() {
		response.Error(ctx, http.StatusServiceUnavailable, "db 未就绪")
		return
	}
	teamID := parseUintOrZero(ctx.Param("teamId"))
	if teamID == 0 {
		response.Error(ctx, http.StatusBadRequest, "无效团队 id")
		return
	}
	var req struct {
		PlanID    uint `json:"plan_id"`
		Months    int  `json:"months"`
		AutoRenew bool `json:"auto_renew"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil || req.PlanID == 0 {
		response.Error(ctx, http.StatusBadRequest, "plan_id 必填")
		return
	}
	sub, err := svc.AssignPlan(ctx.Request.Context(), teamID, req.PlanID, req.Months, req.AutoRenew)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, gin.H{"subscription": sub}, "已指派")
}

func CancelBillingSubscription(ctx *gin.Context) {
	svc := billingSvc()
	if !svc.Ready() {
		response.Error(ctx, http.StatusServiceUnavailable, "db 未就绪")
		return
	}
	teamID := parseUintOrZero(ctx.Param("teamId"))
	if err := svc.CancelSubscription(ctx.Request.Context(), teamID); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, gin.H{"ok": true}, "已停用订阅")
}

// ---- 账单 ----

func ListBillingRecords(ctx *gin.Context) {
	svc := billingSvc()
	if !svc.Ready() {
		response.Error(ctx, http.StatusServiceUnavailable, "db 未就绪")
		return
	}
	page := parseIntOr(ctx.Query("page"), 1)
	size := parseIntOr(ctx.Query("size"), 20)
	rows, total, err := svc.ListRecords(ctx.Request.Context(), parseUintOrZero(ctx.Query("team_id")), ctx.Query("status"), page, size)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	if rows == nil {
		rows = []model.BillingRecord{}
	}
	response.Success(ctx, gin.H{"list": rows, "total": total}, "ok")
}

func CreateBillingRecord(ctx *gin.Context) {
	svc := billingSvc()
	if !svc.Ready() {
		response.Error(ctx, http.StatusServiceUnavailable, "db 未就绪")
		return
	}
	var req struct {
		TeamID      uint   `json:"team_id"`
		PlanID      uint   `json:"plan_id"`
		Months      int    `json:"months"`
		AmountCents int64  `json:"amount_cents"`
		Remark      string `json:"remark"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil || req.TeamID == 0 || req.PlanID == 0 {
		response.Error(ctx, http.StatusBadRequest, "team_id/plan_id 必填")
		return
	}
	row, err := svc.CreateRecord(ctx.Request.Context(), req.TeamID, req.PlanID, req.Months, req.AmountCents, req.Remark)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, gin.H{"record": row}, "已创建应收账单")
}

func PayBillingRecord(ctx *gin.Context) {
	svc := billingSvc()
	if !svc.Ready() {
		response.Error(ctx, http.StatusServiceUnavailable, "db 未就绪")
		return
	}
	id := parseUintOrZero(ctx.Param("id"))
	if err := svc.MarkRecordPaid(ctx.Request.Context(), id); err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, gin.H{"ok": true}, "已标记支付")
}

func VoidBillingRecord(ctx *gin.Context) {
	svc := billingSvc()
	if !svc.Ready() {
		response.Error(ctx, http.StatusServiceUnavailable, "db 未就绪")
		return
	}
	id := parseUintOrZero(ctx.Param("id"))
	if err := svc.VoidRecord(ctx.Request.Context(), id); err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, gin.H{"ok": true}, "已作废")
}

// ---- 团队管理员只读面 ----

// MyTeamBilling GET /api/team/billing（本团队订阅 + 账单；teamID 取自 JWT）
func MyTeamBilling(ctx *gin.Context) {
	svc := billingSvc()
	if !svc.Ready() {
		response.Error(ctx, http.StatusServiceUnavailable, "db 未就绪")
		return
	}
	teamID, _ := mcpManageScope(ctx)
	if teamID == 0 {
		response.Error(ctx, http.StatusBadRequest, "当前账号未归属任何团队")
		return
	}
	sub, _ := svc.GetTeamSubscription(ctx.Request.Context(), teamID)
	rows, total, err := svc.ListRecords(ctx.Request.Context(), teamID, "", 1, 50)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	if rows == nil {
		rows = []model.BillingRecord{}
	}
	if sub == nil {
		sub = &model.TeamSubscription{}
	}
	response.Success(ctx, gin.H{"subscription": sub, "records": rows, "total": total}, "ok")
}

// ---- 角色-权限覆盖层 ----

// GetRolePermissions GET /manage/role-permissions/:roleCode
func GetRolePermissions(ctx *gin.Context) {
	svc := rbacAdminSvc()
	if !svc.Ready() {
		response.Error(ctx, http.StatusServiceUnavailable, "db 未就绪")
		return
	}
	roleCode := ctx.Param("roleCode")
	perms, override, err := svc.GetRolePermissions(ctx.Request.Context(), roleCode)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	if perms == nil {
		perms = []string{}
	}
	response.Success(ctx, gin.H{"role": roleCode, "override": override, "permissions": perms}, "ok")
}

// SetRolePermissions PUT /manage/role-permissions/:roleCode {permissions: []}
func SetRolePermissions(ctx *gin.Context) {
	svc := rbacAdminSvc()
	if !svc.Ready() {
		response.Error(ctx, http.StatusServiceUnavailable, "db 未就绪")
		return
	}
	roleCode := ctx.Param("roleCode")
	var req struct {
		Permissions []string `json:"permissions"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求体解析失败: "+err.Error())
		return
	}
	if err := svc.SetRolePermissions(ctx.Request.Context(), roleCode, req.Permissions); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, gin.H{"ok": true}, "已保存（60s 内全局生效，立即生效可重启或清缓存）")
}
