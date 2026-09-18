package controller

import (
	"encoding/csv"
	"html/template"
	"net/http"
	"net/url"
	"strconv"

	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// EmailUnsubscribeController 邮件退订控制器
//
// 路由：
//   - GET  /api/email/unsubscribe          退订确认页（HTML）
//   - POST /api/email/unsubscribe/confirm  退订确认提交
//   - GET  /api/email/unsubscribe/list     分页查询退订名单
//   - GET  /api/email/unsubscribe/export   导出退订名单（CSV）
//   - POST /api/email/unsubscribe/resubscribe 重新订阅
type EmailUnsubscribeController struct {
	svc *service.EmailUnsubscribeService
}

// NewEmailUnsubscribeController 创建邮件退订控制器
func NewEmailUnsubscribeController(svc *service.EmailUnsubscribeService) *EmailUnsubscribeController {
	return &EmailUnsubscribeController{svc: svc}
}

// UnsubscribePage GET /api/email/unsubscribe?token=xxx
// 返回 HTML 退订确认页（合规要求：退订链接点击后落地页可确认）
func (c *EmailUnsubscribeController) UnsubscribePage(ctx *gin.Context) {
	token := ctx.Query("token")
	if token == "" {
		ctx.String(http.StatusBadRequest, "缺少 token 参数")
		return
	}

	claim, err := c.svc.VerifyUnsubscribeToken(ctx.Request.Context(), token)
	if err != nil {
		ctx.String(http.StatusBadRequest, "退订链接无效或已过期：%s", err.Error())
		return
	}

	if c.svc.IsUnsubscribed(ctx.Request.Context(), claim.Email) {
		ctx.Data(http.StatusOK, "text/html; charset=utf-8", []byte(unsubscribedAlreadyHTML(claim.Email)))
		return
	}

	ctx.Data(http.StatusOK, "text/html; charset=utf-8", []byte(unsubscribeConfirmHTML(claim.Email, token)))
}

// UnsubscribeConfirmRequest 退订确认请求体
type UnsubscribeConfirmRequest struct {
	Token  string `json:"token" binding:"required"`
	Reason string `json:"reason"`
}

// UnsubscribeConfirm POST /api/email/unsubscribe/confirm
func (c *EmailUnsubscribeController) UnsubscribeConfirm(ctx *gin.Context) {
	var req UnsubscribeConfirmRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "参数错误："+err.Error())
		return
	}

	claim, err := c.svc.VerifyUnsubscribeToken(ctx.Request.Context(), req.Token)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "退订链接无效或已过期："+err.Error())
		return
	}

	ip := ctx.ClientIP()
	ua := ctx.Request.UserAgent()
	if err := c.svc.UnsubscribeEmail(ctx.Request.Context(), claim.Email, req.Reason, "/api/email/unsubscribe/confirm", claim.JobID, ip, ua); err != nil {
		response.ErrorFromDB(ctx, err, "退订失败："+err.Error())
		return
	}

	response.Success(ctx, gin.H{"email": claim.Email, "status": "unsubscribed"}, "退订成功")
}

// ListUnsubscribes GET /api/email/unsubscribe/list
func (c *EmailUnsubscribeController) ListUnsubscribes(ctx *gin.Context) {
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "20"))
	keyword := ctx.Query("keyword")

	records, total, err := c.svc.ListUnsubscribes(ctx.Request.Context(), page, limit, keyword)
	if err != nil {
		response.ErrorFromDB(ctx, err, "查询退订名单失败："+err.Error())
		return
	}

	response.SuccessWithPage(ctx, records, int64(page), int64(limit), total)
}

// ExportUnsubscribes GET /api/email/unsubscribe/export
// 导出退订名单 CSV
func (c *EmailUnsubscribeController) ExportUnsubscribes(ctx *gin.Context) {
	records, err := c.svc.ListAllUnsubscribes(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, "导出退订名单失败："+err.Error())
		return
	}

	ctx.Header("Content-Type", "text/csv; charset=utf-8")
	ctx.Header("Content-Disposition", "attachment; filename=email_unsubscribes.csv")

	_, _ = ctx.Writer.Write([]byte{0xEF, 0xBB, 0xBF})

	w := csv.NewWriter(ctx.Writer)
	_ = w.Write([]string{"email", "reason", "unsubscribed_at", "source_link", "ip", "ua", "job_id"})
	for _, r := range records {
		_ = w.Write([]string{r.Email, r.Reason, r.UnsubscribedAt.Format("2006-01-02 15:04:05"), r.SourceLink, r.IP, r.UA, r.JobID})
	}
	w.Flush()
}

// ResubscribeRequest 重新订阅请求体
type ResubscribeRequest struct {
	Email string `json:"email" binding:"required,email"`
}

// Resubscribe POST /api/email/unsubscribe/resubscribe
// 允许用户重新订阅（合规要求）
func (c *EmailUnsubscribeController) Resubscribe(ctx *gin.Context) {
	var req ResubscribeRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "参数错误："+err.Error())
		return
	}

	if err := c.svc.ResubscribeEmail(ctx.Request.Context(), req.Email); err != nil {
		response.ErrorFromDB(ctx, err, "重新订阅失败："+err.Error())
		return
	}

	response.Success(ctx, gin.H{"email": req.Email, "status": "resubscribed"}, "重新订阅成功")
}

// RegisterRoutes 注册路由（公开 + 鉴权混合）
//
// 公开路由：退订确认页 / 退订确认提交（用户从邮件点击进入，无 JWT）
// 鉴权路由：名单查询 / 导出 / 重新订阅（后台管理）
func (c *EmailUnsubscribeController) RegisterRoutes(public *gin.RouterGroup, authed *gin.RouterGroup) {
	if public != nil {
		public.GET("/email/unsubscribe", c.UnsubscribePage)
		public.POST("/email/unsubscribe/confirm", c.UnsubscribeConfirm)
	}
	if authed != nil {
		authed.GET("/email/unsubscribe/list", c.ListUnsubscribes)
		authed.GET("/email/unsubscribe/export", c.ExportUnsubscribes)
		authed.POST("/email/unsubscribe/resubscribe", c.Resubscribe)
	}
}

func unsubscribeConfirmHTML(email, token string) string {
	escEmail := template.HTMLEscapeString(email)
	escTokenJS := template.JSEscapeString(token)
	escTokenURL := url.QueryEscape(token)
	return `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<title>邮件退订确认</title>
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<style>
  body{font-family:-apple-system,BlinkMacSystemFont,'Segoe UI','PingFang SC','Hiragino Sans GB','Microsoft YaHei',sans-serif;background:#f5f7fa;margin:0;padding:40px 16px;color:#303133;}
  .card{max-width:520px;margin:0 auto;background:#fff;border-radius:8px;box-shadow:0 2px 12px rgba(0,0,0,0.08);padding:32px;}
  h1{font-size:20px;margin:0 0 12px;color:#303133;}
  p{line-height:1.6;margin:8px 0;color:#606266;}
  .email{color:#409EFF;font-weight:500;word-break:break-all;}
  textarea{width:100%;min-height:80px;padding:8px 12px;border:1px solid #DCDFE6;border-radius:4px;font-size:14px;box-sizing:border-box;}
  button{background:#F56C6C;color:#fff;border:0;border-radius:4px;padding:10px 24px;font-size:14px;cursor:pointer;margin-top:16px;}
  button:hover{background:#E64242;}
  .footer{margin-top:24px;font-size:12px;color:#909399;text-align:center;}
</style>
</head>
<body>
<div class="card">
  <h1>邮件退订确认</h1>
  <p>您正在退订邮箱：<span class="email">` + escEmail + `</span></p>
  <p>退订后，我们将不再向此邮箱发送营销邮件（重要账户通知仍会发送）。</p>
  <form id="form">
    <label for="reason" style="font-size:14px;color:#606266;">退订原因（可选）：</label><br>
    <textarea id="reason" name="reason" placeholder="请告诉我们退订原因，帮助我们改进"></textarea><br>
    <button type="submit">确认退订</button>
  </form>
  <div class="footer">依据《互联网电子邮件服务管理办法》，您可在退订后随时联系客服重新订阅。</div>
</div>
<script>
document.getElementById('form').addEventListener('submit', function(e){
  e.preventDefault();
  var reason = document.getElementById('reason').value;
  fetch('/api/email/unsubscribe/confirm', {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({token: '` + escTokenJS + `', reason: reason})
  }).then(function(r){return r.json();}).then(function(d){
    alert((d && d.message) ? d.message : '退订成功');
    if(d && d.code === 0){ window.location.href = '/api/email/unsubscribe?token=` + escTokenURL + `'; }
  }).catch(function(e){ alert('退订请求失败：' + e); });
});
</script>
</body>
</html>`
}

func unsubscribedAlreadyHTML(email string) string {
	escEmail := template.HTMLEscapeString(email)
	return `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<title>已退订</title>
<style>
  body{font-family:-apple-system,BlinkMacSystemFont,sans-serif;background:#f5f7fa;margin:0;padding:40px 16px;color:#303133;}
  .card{max-width:520px;margin:0 auto;background:#fff;border-radius:8px;padding:32px;text-align:center;}
  h1{font-size:18px;color:#67C23A;margin:0 0 12px;}
  p{color:#606266;line-height:1.6;}
</style>
</head>
<body>
<div class="card">
  <h1>已成功退订</h1>
  <p>邮箱 <strong>` + escEmail + `</strong> 已在退订名单中。</p>
  <p>我们将不再向您发送营销邮件。如需重新订阅，请联系管理员。</p>
</div>
</body>
</html>`
}
