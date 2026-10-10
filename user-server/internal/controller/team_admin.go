package controller

import (
	"errors"
	"net/http"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// team_admin.go 团队（租户）管理端点（多租户 P0）。
//
// 两组口径：
//   - /api/manage/teams/*  ：平台管理员专属（authPlainAdmin），全量 CRUD 与成员调拨；
//   - /api/team/*          ：团队管理员可用（TeamAdminAuthMiddleware），仅本团队，
//     归属校验在 service.authorizeTeamAccess（服务端强制，前端只做显隐）。
//
// 越权语义：跨团队一律 403（明确告知无权，区别于 404 的"资源不存在"）。

var teamAdminSvc = func() *service.TeamAdminService { return service.NewTeamAdminService() }

// teamCaller 当前调用方身份（gin ctx 由 JWT 中间件写入）。
func teamCaller(ctx *gin.Context) (userID uint, role string, teamID uint, isPlatform bool) {
	if v, ok := ctx.Get("user_id"); ok {
		if id, ok2 := v.(uint); ok2 {
			userID = id
		}
	}
	if v, ok := ctx.Get("role"); ok {
		role, _ = v.(string)
	}
	if v, ok := ctx.Get("team_id"); ok {
		if id, ok2 := v.(uint); ok2 {
			teamID = id
		}
	}
	isPlatform = role == model.SystemUserRoleAdmin
	return
}

// ListTeams GET /api/manage/teams?page=&size=
func ListTeams(ctx *gin.Context) {
	svc := teamAdminSvc()
	if !svc.Ready() {
		response.Error(ctx, http.StatusServiceUnavailable, "db 未就绪")
		return
	}
	page := parseIntOr(ctx.Query("page"), 1)
	size := parseIntOr(ctx.Query("size"), 20)
	rows, total, err := svc.List(ctx.Request.Context(), page, size)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "查询团队失败: "+err.Error())
		return
	}
	if rows == nil {
		rows = []model.Team{}
	}
	response.Success(ctx, gin.H{"list": rows, "total": total}, "ok")
}

// CreateTeam POST /api/manage/teams
// body: {"name":"...","plan":"free","owner_user_id":2,"remark":"..."}
func CreateTeam(ctx *gin.Context) {
	svc := teamAdminSvc()
	if !svc.Ready() {
		response.Error(ctx, http.StatusServiceUnavailable, "db 未就绪")
		return
	}
	var req struct {
		Name        string `json:"name"`
		Plan        string `json:"plan"`
		Remark      string `json:"remark"`
		OwnerUserID *uint  `json:"owner_user_id"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求体解析失败: "+err.Error())
		return
	}
	t, err := svc.Create(ctx.Request.Context(), req.Name, req.Plan, req.Remark, req.OwnerUserID)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, gin.H{"team": t}, "创建成功")
}

// UpdateTeam PUT /api/manage/teams/:id（部分更新；nil 字段不动）
func UpdateTeam(ctx *gin.Context) {
	svc := teamAdminSvc()
	if !svc.Ready() {
		response.Error(ctx, http.StatusServiceUnavailable, "db 未就绪")
		return
	}
	id := parseUintOrZero(ctx.Param("id"))
	if id == 0 {
		response.Error(ctx, http.StatusBadRequest, "无效 id")
		return
	}
	var req struct {
		Name        *string `json:"name"`
		Plan        *string `json:"plan"`
		Status      *string `json:"status"`
		Remark      *string `json:"remark"`
		OwnerUserID *uint   `json:"owner_user_id"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求体解析失败: "+err.Error())
		return
	}
	t, err := svc.Update(ctx.Request.Context(), id, req.Name, req.Plan, req.Status, req.Remark, req.OwnerUserID)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, gin.H{"team": t}, "已更新")
}

// DeleteTeam DELETE /api/manage/teams/:id（空团队才可删；默认团队拒删）
func DeleteTeam(ctx *gin.Context) {
	svc := teamAdminSvc()
	if !svc.Ready() {
		response.Error(ctx, http.StatusServiceUnavailable, "db 未就绪")
		return
	}
	id := parseUintOrZero(ctx.Param("id"))
	if id == 0 {
		response.Error(ctx, http.StatusBadRequest, "无效 id")
		return
	}
	if err := svc.Delete(ctx.Request.Context(), id); err != nil {
		response.Error(ctx, http.StatusConflict, err.Error())
		return
	}
	response.Success(ctx, gin.H{"ok": true}, "已删除")
}

// ListTeamUsers GET /api/manage/teams/:id/users（平台管理员查任意团队）
func ListTeamUsers(ctx *gin.Context) {
	svc := teamAdminSvc()
	if !svc.Ready() {
		response.Error(ctx, http.StatusServiceUnavailable, "db 未就绪")
		return
	}
	teamID := parseUintOrZero(ctx.Param("id"))
	if teamID == 0 {
		response.Error(ctx, http.StatusBadRequest, "无效 id")
		return
	}
	_, role, callerTeam, isPlatform := teamCaller(ctx)
	page := parseIntOr(ctx.Query("page"), 1)
	size := parseIntOr(ctx.Query("size"), 20)
	rows, total, err := svc.ListMembers(ctx.Request.Context(), teamID, isPlatform, callerTeam, page, size)
	if err != nil {
		teamMemberListError(ctx, role, err)
		return
	}
	if rows == nil {
		rows = []model.SystemUser{}
	}
	response.Success(ctx, gin.H{"list": rows, "total": total}, "ok")
}

// SetTeamUserRole PUT /api/manage/teams/:id/users/:userId/role
func SetTeamUserRole(ctx *gin.Context) {
	svc := teamAdminSvc()
	if !svc.Ready() {
		response.Error(ctx, http.StatusServiceUnavailable, "db 未就绪")
		return
	}
	teamID := parseUintOrZero(ctx.Param("id"))
	userID := parseUintOrZero(ctx.Param("userId"))
	if teamID == 0 || userID == 0 {
		response.Error(ctx, http.StatusBadRequest, "无效 id")
		return
	}
	var req struct {
		Role string `json:"role"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil || req.Role == "" {
		response.Error(ctx, http.StatusBadRequest, "role 必填（user/team_admin）")
		return
	}
	_, role, callerTeam, isPlatform := teamCaller(ctx)
	u, err := svc.SetMemberRole(ctx.Request.Context(), teamID, userID, req.Role, isPlatform, callerTeam)
	if err != nil {
		teamMemberListError(ctx, role, err)
		return
	}
	response.Success(ctx, gin.H{"user": u}, "已更新")
}

// MoveTeamUser POST /api/manage/teams/:id/users/:userId/move
// 把用户调入该团队（跨团队调拨为平台管理员专属）。目标团队由路径给定。
func MoveTeamUser(ctx *gin.Context) {
	svc := teamAdminSvc()
	if !svc.Ready() {
		response.Error(ctx, http.StatusServiceUnavailable, "db 未就绪")
		return
	}
	teamID := parseUintOrZero(ctx.Param("id"))
	userID := parseUintOrZero(ctx.Param("userId"))
	if teamID == 0 || userID == 0 {
		response.Error(ctx, http.StatusBadRequest, "无效 id")
		return
	}
	_, role, _, isPlatform := teamCaller(ctx)
	if !isPlatform {
		response.Error(ctx, http.StatusForbidden, "跨团队调拨成员是平台管理员专属操作")
		return
	}
	u, err := svc.MoveMember(ctx.Request.Context(), userID, teamID)
	if err != nil {
		teamMemberListError(ctx, role, err)
		return
	}
	response.Success(ctx, gin.H{"user": u}, "已调拨（该用户旧令牌已吊销，需重新登录）")
}

// ---- 团队管理员自面（/api/team/*）：只能看/管本团队 ----

// MyTeam GET /api/team/detail
func MyTeam(ctx *gin.Context) {
	svc := teamAdminSvc()
	if !svc.Ready() {
		response.Error(ctx, http.StatusServiceUnavailable, "db 未就绪")
		return
	}
	_, _, callerTeam, _ := teamCaller(ctx)
	if callerTeam == 0 {
		response.Error(ctx, http.StatusBadRequest, "当前账号未归属任何团队")
		return
	}
	t, err := svc.Get(ctx.Request.Context(), callerTeam)
	if err != nil {
		response.Error(ctx, http.StatusNotFound, "团队不存在")
		return
	}
	response.Success(ctx, gin.H{"team": t}, "ok")
}

// MyTeamUsers GET /api/team/users（本团队成员；teamID 取自 JWT，调用方无法指定别人团队）
func MyTeamUsers(ctx *gin.Context) {
	svc := teamAdminSvc()
	if !svc.Ready() {
		response.Error(ctx, http.StatusServiceUnavailable, "db 未就绪")
		return
	}
	_, role, callerTeam, isPlatform := teamCaller(ctx)
	if callerTeam == 0 && !isPlatform {
		response.Error(ctx, http.StatusBadRequest, "当前账号未归属任何团队")
		return
	}
	page := parseIntOr(ctx.Query("page"), 1)
	size := parseIntOr(ctx.Query("size"), 20)
	rows, total, err := svc.ListMembers(ctx.Request.Context(), callerTeam, isPlatform, callerTeam, page, size)
	if err != nil {
		teamMemberListError(ctx, role, err)
		return
	}
	if rows == nil {
		rows = []model.SystemUser{}
	}
	response.Success(ctx, gin.H{"list": rows, "total": total}, "ok")
}

// SetMyTeamUserRole PUT /api/team/users/:userId/role（本团队内 user↔team_admin）
func SetMyTeamUserRole(ctx *gin.Context) {
	svc := teamAdminSvc()
	if !svc.Ready() {
		response.Error(ctx, http.StatusServiceUnavailable, "db 未就绪")
		return
	}
	userID := parseUintOrZero(ctx.Param("userId"))
	if userID == 0 {
		response.Error(ctx, http.StatusBadRequest, "无效 id")
		return
	}
	var req struct {
		Role string `json:"role"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil || req.Role == "" {
		response.Error(ctx, http.StatusBadRequest, "role 必填（user/team_admin）")
		return
	}
	_, role, callerTeam, isPlatform := teamCaller(ctx)
	u, err := svc.SetMemberRole(ctx.Request.Context(), callerTeam, userID, req.Role, isPlatform, callerTeam)
	if err != nil {
		teamMemberListError(ctx, role, err)
		return
	}
	response.Success(ctx, gin.H{"user": u}, "已更新")
}

// teamMemberListError 团队成员操作的统一错误映射：越权 403，其余按消息透传。
func teamMemberListError(ctx *gin.Context, role string, err error) {
	if service.IsTeamForbidden(err) || errors.Is(err, errTeamAdminOnly) {
		response.Error(ctx, http.StatusForbidden, err.Error())
		return
	}
	response.Error(ctx, http.StatusInternalServerError, err.Error())
}

// errTeamAdminOnly 预留：团队管理员专属操作被非本团队成员触达时的语义锚点。
var errTeamAdminOnly = errors.New("仅团队管理员可执行该操作")
