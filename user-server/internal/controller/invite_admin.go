package controller

import (
	"net/http"
	"strconv"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/repository"

	"github.com/gin-gonic/gin"
)

// invite_admin.go 邀请码管理端点（SaaS P2 准入，2026-10-11）。
//
//   平台管理员：GET|POST /manage/teams/:id/invite-codes, POST /manage/teams/:id/invite-codes/:codeId/disable
//   团队管理员：GET|POST /team/invite-codes（本团队），POST /team/invite-codes/:codeId/disable
// 消耗方：注册流（service.Register 的 Consume 原子扣次）。

var inviteRepo = func() *repository.InviteCodeRepo { return repository.NewInviteCodeRepo() }

type inviteReq struct {
	MaxUses     int `json:"max_uses"`
	ExpiresDays int `json:"expires_days"`
}

func inviteOut(row *model.InviteCode) gin.H {
	return gin.H{"id": row.ID, "code": row.Code, "team_id": row.TeamID,
		"max_uses": row.MaxUses, "used_count": row.UsedCount,
		"expires_at": row.ExpiresAt, "status": row.Status, "created_at": row.CreatedAt}
}

func generateInvite(ctx *gin.Context, teamID uint, createdBy *uint) {
	if teamID == 0 {
		response.Error(ctx, http.StatusBadRequest, "无效团队 id")
		return
	}
	var req inviteReq
	_ = ctx.ShouldBindJSON(&req)
	if req.MaxUses < 0 {
		req.MaxUses = 0
	}
	var expiresAt *time.Time
	if req.ExpiresDays > 0 {
		t := time.Now().AddDate(0, 0, req.ExpiresDays)
		expiresAt = &t
	}
	row, err := inviteRepo().Generate(ctx.Request.Context(), teamID, req.MaxUses, expiresAt, createdBy)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "生成邀请码失败: "+err.Error())
		return
	}
	response.Success(ctx, gin.H{"invite": inviteOut(row)}, "已生成")
}

func listInvites(ctx *gin.Context, teamID uint) {
	rows, err := inviteRepo().ListByTeam(ctx.Request.Context(), teamID)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	if rows == nil {
		rows = []model.InviteCode{}
	}
	out := make([]gin.H, 0, len(rows))
	for i := range rows {
		out = append(out, inviteOut(&rows[i]))
	}
	response.Success(ctx, gin.H{"list": out}, "ok")
}

func disableInvite(ctx *gin.Context) {
	codeID := parseUintOrZero(ctx.Param("codeId"))
	if codeID == 0 {
		response.Error(ctx, http.StatusBadRequest, "无效 id")
		return
	}
	if err := inviteRepo().Disable(ctx.Request.Context(), codeID); err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, gin.H{"ok": true}, "已停用")
}

// ---- 平台管理员面 ----

// CreateTeamInvite POST /manage/teams/:id/invite-codes
func ginCtxUserID(ctx *gin.Context) uint {
	if v, ok := ctx.Get("user_id"); ok {
		if id, ok2 := v.(uint); ok2 {
			return id
		}
	}
	return 0
}

func CreateTeamInvite(ctx *gin.Context) {
	teamID := parseUintOrZero(ctx.Param("id"))
	_, platform := mcpManageScope(ctx)
	uid := ginCtxUserID(ctx)
	if !platform {
		response.Error(ctx, http.StatusForbidden, "平台管理员专属")
		return
	}
	var createdBy *uint
	if uid != 0 {
		createdBy = &uid
	}
	generateInvite(ctx, teamID, createdBy)
}

// ListTeamInvites GET /manage/teams/:id/invite-codes
func ListTeamInvites(ctx *gin.Context) {
	teamID := parseUintOrZero(ctx.Param("id"))
	_, platform := mcpManageScope(ctx)
	if !platform {
		response.Error(ctx, http.StatusForbidden, "平台管理员专属")
		return
	}
	listInvites(ctx, teamID)
}

// DisableTeamInvite POST /manage/teams/:id/invite-codes/:codeId/disable
func DisableTeamInvite(ctx *gin.Context) {
	_, platform := mcpManageScope(ctx)
	if !platform {
		response.Error(ctx, http.StatusForbidden, "平台管理员专属")
		return
	}
	disableInvite(ctx)
}

// ---- 团队管理员面（本团队，teamID 取自 JWT） ----

// MyTeamInvites GET /team/invite-codes
func MyTeamInvites(ctx *gin.Context) {
	teamID, _ := mcpManageScope(ctx)
	if teamID == 0 {
		response.Error(ctx, http.StatusBadRequest, "当前账号未归属任何团队")
		return
	}
	listInvites(ctx, teamID)
}

// CreateMyTeamInvite POST /team/invite-codes
func CreateMyTeamInvite(ctx *gin.Context) {
	teamID, _ := mcpManageScope(ctx)
	uid := ginCtxUserID(ctx)
	if teamID == 0 {
		response.Error(ctx, http.StatusBadRequest, "当前账号未归属任何团队")
		return
	}
	var createdBy *uint
	if uid != 0 {
		createdBy = &uid
	}
	generateInvite(ctx, teamID, createdBy)
}

// DisableMyTeamInvite POST /team/invite-codes/:codeId/disable
func DisableMyTeamInvite(ctx *gin.Context) {
	teamID, _ := mcpManageScope(ctx)
	if teamID == 0 {
		response.Error(ctx, http.StatusBadRequest, "当前账号未归属任何团队")
		return
	}
	// 归属校验：只能停本团队的码
	repo := inviteRepo()
	codeID := parseUintOrZero(ctx.Param("codeId"))
	rows, err := repo.ListByTeam(ctx.Request.Context(), teamID)
	if err == nil {
		mine := false
		for _, r := range rows {
			if r.ID == codeID {
				mine = true
				break
			}
		}
		if !mine {
			response.Error(ctx, http.StatusForbidden, "只能操作本团队的邀请码")
			return
		}
	}
	disableInvite(ctx)
}

var _ = strconv.Itoa
