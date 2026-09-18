package controller

import (
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
	"net/http"

	"github.com/gin-gonic/gin"
)

type AccountController struct {
	svc *service.AccountService
}

func NewAccountController() *AccountController {
	return &AccountController{svc: service.NewAccountService()}
}

func (c *AccountController) CreateAccount(ctx *gin.Context) {
	var req dto.CreateAccountRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	account := model.Account{
		TgBotToken:       req.TgBotToken,
		Price:            req.Price,
		GroupID:          req.GroupID,
		ProxyEnableProxy: req.ProxyEnableProxy,
		ProxyProtoclo:    req.ProxyProtoclo,
		ProxyHost:        req.ProxyHost,
		ProxyPort:        req.ProxyPort,
	}

	createdAccount, err := c.svc.CreateAccount(ctx.Request.Context(), account)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}

	resp := dto.AccountResponse{
		ID:               createdAccount.ID,
		TgBotToken:       createdAccount.TgBotToken,
		Price:            createdAccount.Price,
		GroupID:          createdAccount.GroupID,
		ProxyEnableProxy: createdAccount.ProxyEnableProxy,
		ProxyProtoclo:    createdAccount.ProxyProtoclo,
		ProxyHost:        createdAccount.ProxyHost,
		ProxyPort:        createdAccount.ProxyPort,
		Status:           createdAccount.Status,
		CreateTime:       createdAccount.CreateTime,
	}
	response.Success(ctx, resp, "success")
}

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
			TgBotToken:       account.TgBotToken,
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
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}

	resp := dto.AccountResponse{
		ID:               account.ID,
		TgName:           account.TgName,
		TgBotToken:       account.TgBotToken,
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

func (c *AccountController) UpdateAccount(ctx *gin.Context) {
	var req dto.UpdateAccountRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	accountIDStr := ctx.Param("id")
	if accountIDStr == "" {
		response.Error(ctx, http.StatusBadRequest, "账户ID不能为空")
		return
	}

	if req.ID != "" && req.ID != accountIDStr {
		req.ID = accountIDStr
	}

	account := model.Account{
		ID:            accountIDStr,
		TgName:        req.TgName,
		TgBotToken:    req.TgBotToken,
		Price:         req.Price,
		GroupID:       req.GroupID,
		ProxyProtoclo: req.ProxyProtoclo,
		ProxyHost:     req.ProxyHost,
		ProxyPort:     req.ProxyPort,
	}
	// PATCH 语义：未传 proxy_enable_proxy 保留原值，防止漏字段把代理开关意外关闭
	if req.ProxyEnableProxy != nil {
		account.ProxyEnableProxy = *req.ProxyEnableProxy
	} else if existing, getErr := c.svc.GetAccount(ctx.Request.Context(), accountIDStr); getErr == nil && existing != nil {
		account.ProxyEnableProxy = existing.ProxyEnableProxy
	}
	err := c.svc.UpdateAccount(ctx.Request.Context(), account)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, nil, "success")
}

func (c *AccountController) DeleteAccount(ctx *gin.Context) {
	var req dto.DeleteAccountRequest
	if err := ctx.ShouldBindUri(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	err := c.svc.DeleteAccount(ctx.Request.Context(), req.ID)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, nil, "success")
}
