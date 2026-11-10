// connector.go TB-06b：连接器 HTTP 面（连通性自检；AdminAuth 闸）。
// 五层归属：Handler（参数绑定+调执行器+统一响应出口）。
package controller

import (
	"net/http"

	"hivemtk-user/internal/connector"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

// ConnectorController 连接器控制器
type ConnectorController struct{}

func NewConnectorController() *ConnectorController {
	return &ConnectorController{}
}

// PingInput 连通性自检入参（基址+方言+鉴权形态；凭证只进请求不透响应）。
type PingInput struct {
	BaseURL  string `json:"base_url" binding:"required"`
	Provider string `json:"provider"`
	AuthType string `json:"auth_type"`
	AuthKey  string `json:"auth_key"`
}

// Ping POST /api/connector/ping——网络层连通性（鉴权有效性=调用面业务，不在本端点承诺）。
func (c *ConnectorController) Ping(ctx *gin.Context) {
	var in PingInput
	if err := ctx.ShouldBindJSON(&in); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：base_url 为必填")
		return
	}
	if in.Provider == "" {
		in.Provider = "generic"
	}
	if connector.Get(in.Provider) == nil {
		response.Error(ctx, http.StatusBadRequest, "方言未注册（可用: generic/kuaidi100/grasp_hmac）")
		return
	}
	ex := connector.NewExecutor()
	reachable, statusCode, detail, err := ex.Ping(ctx.Request.Context(), connector.ConnectorConfig{
		BaseURL:  in.BaseURL,
		Provider: in.Provider,
		Auth:     connector.ConnectorAuth{Type: in.AuthType, Key: in.AuthKey},
	})
	if err != nil {
		// 基址不合法/方言未注册=配置错误（SSRF 防线 fail-closed）
		response.Error(ctx, http.StatusBadRequest, "连通性自检拒绝: "+detail)
		return
	}
	response.Success(ctx, gin.H{
		"reachable":   reachable,
		"status_code": statusCode,
		"detail":      detail,
		"provider":    in.Provider,
	}, "自检完成")
}
