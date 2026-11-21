package controller

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

type MerchantCallbackController struct {
	svc service.MerchantCallbackService
}

func NewMerchantCallbackController(svc service.MerchantCallbackService) *MerchantCallbackController {
	return &MerchantCallbackController{svc: svc}
}

// Ingest 仅在持久提交后发送提供方成功响应；未注册验证器返回 503。
// @Router /api/v1/integrations/merchant-callbacks/*delivery [post]
func (c *MerchantCallbackController) Ingest(ctx *gin.Context) {
	merchantConnectionResponseHeaders(ctx)
	if ctx.Request.Method != http.MethodPost {
		ctx.Header("Allow", http.MethodPost)
		response.Protocol(ctx, http.StatusMethodNotAllowed, "text/plain; charset=utf-8", nil)
		return
	}
	provider := strings.TrimSuffix(strings.TrimPrefix(ctx.Param("delivery"), "/"), "/")
	if provider == "" || strings.Contains(provider, "/") || ctx.Request.URL.ForceQuery || ctx.Request.URL.RawQuery != "" || ctx.Request.Body == nil {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(ctx.Writer, ctx.Request.Body, service.MerchantCallbackBodyLimit))
	if err != nil {
		var maxBytes *http.MaxBytesError
		if errors.As(err, &maxBytes) {
			response.Error(ctx, http.StatusRequestEntityTooLarge, "请求体过大")
		} else {
			response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams)
		}
		return
	}
	out := c.svc.Ingest(ctx.Request.Context(), provider, body, ctx.Request.Header)
	ack := out.Ack
	if out.TrustedAck {
		contentType := ack.ContentType
		if contentType == "" && ack.Body != "" {
			contentType = "text/plain; charset=utf-8"
		}
		response.Protocol(ctx, ack.Status, contentType, []byte(ack.Body))
		return
	}
	if ack.Body != "" {
		if ack.ContentType == "" {
			ack.ContentType = "text/plain; charset=utf-8"
		}
		response.Protocol(ctx, ack.Status, ack.ContentType, []byte(ack.Body))
		return
	}
	if ack.Status >= 200 && ack.Status < 300 {
		response.Protocol(ctx, ack.Status, "", nil)
		return
	}
	response.Error(ctx, ack.Status, response.ErrBusinessError)
}
