package controller

import (
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
	"net/http"

	"github.com/gin-gonic/gin"
)

type AccountController struct {
	svc *service.AccountService
}

func NewAccountController(svc *service.AccountService) *AccountController {
	return &AccountController{svc: svc}
}

// @Summary      查询 TG 账户列表（bot token 掩码）
// @Tags      account
// @Router    /api/account/list [get]
func (c *AccountController) GetAccounts(ctx *gin.Context) {
	accounts, err := c.svc.GetAccountList(ctx.Request.Context())
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	resp := dto.GetAccountListResponse{
		Total: int64(len(accounts)),
		List:  []*dto.AccountResponse{},
	}
	for _, account := range accounts {
		resp.List = append(resp.List, &dto.AccountResponse{
			ID:               account.ID,
			TgName:           account.TgName,
			TgBotToken:       maskBotToken(account.TgBotToken),
			Price:            account.Price,
			GroupID:          account.GroupID,
			ProxyEnableProxy: account.ProxyEnableProxy,
			ProxyProtoclo:    account.ProxyProtoclo,
			ProxyHost:        account.ProxyHost,
			ProxyPort:        account.ProxyPort,
			Status:           account.Status,
			CreateTime:       account.CreateTime,
		})
	}
	response.Success(ctx, resp, "success")
}

// @Summary GET /api/account/{id}
// @Tags      account
// @Router    /api/account/{id} [get]
func (c *AccountController) GetAccount(ctx *gin.Context) {
	accountIDStr := ctx.Param("id")
	if accountIDStr == "" {
		response.Error(ctx, http.StatusBadRequest, "账户ID不能为空")
		return
	}

	account, err := c.svc.GetAccount(ctx.Request.Context(), accountIDStr)
	if err != nil {
		if isNotFoundError(err) {
			response.Error(ctx, http.StatusNotFound, "账户不存在")
			return
		}
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}

	resp := dto.AccountResponse{
		ID:               account.ID,
		TgName:           account.TgName,
		TgBotToken:       maskBotToken(account.TgBotToken),
		Price:            account.Price,
		GroupID:          account.GroupID,
		ProxyEnableProxy: account.ProxyEnableProxy,
		ProxyProtoclo:    account.ProxyProtoclo,
		ProxyHost:        account.ProxyHost,
		ProxyPort:        account.ProxyPort,
		Status:           account.Status,
		CreateTime:       account.CreateTime,
		Msg:              account.Msg,
		URL:              account.URL,
	}
	response.Success(ctx, resp, "success")
}
