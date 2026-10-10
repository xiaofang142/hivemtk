package dto

import (
	_type "hivemtk-user/internal/pkg/utils/type"
)

type CreateAccountRequest struct {
	TgBotToken       string `json:"tg_bot_token" binding:"required"`
	Price            string `json:"price" binding:"required"`
	GroupID          int64  `json:"group_id" binding:"required"`
	ProxyEnableProxy bool   `json:"proxy_enable_proxy"`
	ProxyProtoclo    string `json:"proxy_protoclo"`
	ProxyHost        string `json:"proxy_host"`
	ProxyPort        int    `json:"proxy_port"`
}

type AccountResponse struct {
	ID               string                  `json:"id"`
	TgName           string                  `json:"tg_name"`
	TgBotToken       string                  `json:"tg_bot_token"`
	Price            string                  `json:"price"`
	GroupID          int64                   `json:"group_id"`
	ProxyEnableProxy bool                    `json:"proxy_enable_proxy"`
	ProxyProtoclo    string                  `json:"proxy_protoclo"`
	ProxyHost        string                  `json:"proxy_host"`
	ProxyPort        int                     `json:"proxy_port"`
	Status           _type.AccountStatusType `json:"status"`
	CreateTime       int64                   `json:"create_time"`
	Msg              string                  `json:"msg"`
	URL              string                  `json:"url"`
}

type GetAccountListResponse struct {
	Total int64              `json:"total"`
	List  []*AccountResponse `json:"list"`
}

type UpdateAccountRequest struct {
	ID         string `json:"id" binding:"required"`
	TgName     string `json:"tg_name"`
	TgBotToken string `json:"tg_bot_token"`
	Price      string `json:"price"`
	GroupID    int64  `json:"group_id"`
	// ProxyEnableProxy 用 *bool 表达 PATCH 语义：nil = 未传 = 保留原值。
	// 该接口当前无前端调用方，防御漏字段把代理开关意外关闭。
	ProxyEnableProxy *bool  `json:"proxy_enable_proxy"`
	ProxyProtoclo    string `json:"proxy_protoclo"`
	ProxyHost        string `json:"proxy_host"`
	ProxyPort        int    `json:"proxy_port"`
}

type DeleteAccountRequest struct {
	ID string `uri:"id" binding:"required"`
}
