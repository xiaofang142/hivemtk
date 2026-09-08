// Package controller - GDPR DSAR 数据导出（G6）
package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
	"hivemtk-user/internal/pkg/utils/response"
)

// DataExportController GDPR DSAR 数据导出 API
type DataExportController struct {
	svc *service.DataExportService
}

// NewDataExportController 创建实例
func NewDataExportController() *DataExportController {
	return &DataExportController{
		svc: service.NewDataExportService(),
	}
}

// Export GET /api/gdpr/export/:customer_id
// 返回 JSON 流（Content-Type: application/json，带缩进便于人工查看）
func (c *DataExportController) Export(ctx *gin.Context) {
	customerID := ctx.Param("customer_id")
	if customerID == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "customer_id 不能为空",
		})
		return
	}

	data, err := c.svc.ExportJSON(ctx.Request.Context(), customerID)
	if err != nil {
		msg := err.Error()

		switch {
		case strings.Contains(msg, "DSAR_001"):
			ctx.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": msg})
		case strings.Contains(msg, "DSAR_002") || utils.IsRecordNotFound(err) || strings.Contains(msg, "不存在"):
			ctx.JSON(http.StatusNotFound, gin.H{"code": 404, "message": msg})
		default:
			ctx.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": fmt.Sprintf("导出失败: %v", err)})
		}
		return
	}

	ctx.Header("Content-Type", "application/json; charset=utf-8")
	ctx.Header("Content-Disposition",
		fmt.Sprintf(`attachment; filename="dsar_export_%s.json"`, customerID))
	ctx.Data(http.StatusOK, "application/json; charset=utf-8", data)

	_ = json.Marshal
}

// ManageDataExportController 管理端 GDPR 数据导出控制器
type ManageDataExportController struct {
	svc *service.DataExportService
}

// NewManageDataExportController 构造
func NewManageDataExportController() *ManageDataExportController {
	return &ManageDataExportController{svc: service.NewDataExportService()}
}

// Export GET /api/manage/data-export/:customer_id
// data 用 json.RawMessage 嵌入原始导出 JSON
func (c *ManageDataExportController) Export(ctx *gin.Context) {
	customerID := ctx.Param("customer_id")
	if customerID == "" {
		response.Error(ctx, 400, "customer_id 不能为空")
		return
	}
	data, err := c.svc.ExportJSON(ctx.Request.Context(), customerID)
	if HandleServiceError(ctx, err) {
		return
	}
	ctx.JSON(200, gin.H{
		"code":    0,
		"message": "ok",
		"data":    json.RawMessage(data),
		"_meta": gin.H{
			"customer_id":  customerID,
			"size_bytes":   len(data),
			"generated_at": fmt.Sprintf("data_export:%s", customerID),
		},
	})
}
