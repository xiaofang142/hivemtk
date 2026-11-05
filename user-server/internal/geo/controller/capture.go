package controller

import (
	"html/template"
	"net/http"
	"strconv"
	"strings"

	"hivemtk-user/internal/geo/dto"
	"hivemtk-user/internal/geo/repository"
	"hivemtk-user/internal/geo/service"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

// CaptureController GEO 承接链接管理端。
type CaptureController struct {
	svc *service.CaptureService
}

func NewCaptureController(svc *service.CaptureService) *CaptureController {
	return &CaptureController{svc: svc}
}

// Create POST /geo/capture/links
func (c *CaptureController) Create(ctx *gin.Context) {
	var req service.CaptureLinkInput
	if !response.BindJSON(ctx, &req) {
		return
	}
	view, err := c.svc.Create(ctx.Request.Context(), req, ctx.GetString("username"))
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, view, "承接链接已创建")
}

// List GET /geo/capture/links?keyword=&page=&limit=
func (c *CaptureController) List(ctx *gin.Context) {
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "20"))
	list, total, err := c.svc.List(ctx.Request.Context(), repository.CaptureLinkQuery{
		Keyword: ctx.Query("keyword"), Page: page, Limit: limit,
	})
	if err != nil {
		response.ErrorFromDB(ctx, err, "读取承接链接失败")
		return
	}
	response.Success(ctx, gin.H{"list": list, "total": total}, "ok")
}

// Get GET /geo/capture/links/:id（含首响统计）
func (c *CaptureController) Get(ctx *gin.Context) {
	id, _ := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if id == 0 {
		response.Error(ctx, http.StatusBadRequest, "无效的链接 ID")
		return
	}
	view, err := c.svc.Get(ctx.Request.Context(), uint(id))
	if err != nil {
		response.ErrorFromDB(ctx, err, "读取承接链接失败")
		return
	}
	response.Success(ctx, view, "ok")
}

// Delete DELETE /geo/capture/links/:id
func (c *CaptureController) Delete(ctx *gin.Context) {
	id, _ := strconv.ParseUint(ctx.Param("id"), 10, 32)
	if id == 0 {
		response.Error(ctx, http.StatusBadRequest, "无效的链接 ID")
		return
	}
	if err := c.svc.Delete(ctx.Request.Context(), uint(id)); err != nil {
		response.ErrorFromDB(ctx, err, "删除承接链接失败")
		return
	}
	response.Success(ctx, nil, "已删除")
}

// PublicController 公开落地页：页面渲染 + FAQ + 留资。
type PublicController struct {
	svc *service.CaptureService
}

func NewPublicController(svc *service.CaptureService) *PublicController {
	return &PublicController{svc: svc}
}

type capturePageData struct {
	Title     string
	Keyword   string
	Code      string
	TargetURL string
	AskPath   string
	LeadPath  string
}

var captureLandingTpl = template.Must(template.New("capture-landing").Parse(`<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
<style>
  :root { color-scheme: light; }
  * { box-sizing: border-box; }
  body { margin: 0; font-family: -apple-system, "PingFang SC", "Microsoft YaHei", sans-serif; background: #f6f7fb; color: #111827; }
  .wrap { max-width: 760px; margin: 0 auto; padding: 32px 20px 56px; }
  .card { background: #fff; border: 1px solid #e5e7eb; border-radius: 14px; padding: 24px; margin-bottom: 16px; box-shadow: 0 6px 24px rgba(17,24,39,.05); }
  h1 { font-size: 24px; margin: 0 0 8px; }
  .muted { color: #6b7280; font-size: 13px; }
  .btn { display: inline-block; padding: 10px 18px; border-radius: 10px; background: #2563eb; color: #fff; text-decoration: none; border: 0; cursor: pointer; font-size: 15px; }
  .btn:disabled { opacity: .55; cursor: wait; }
  .ghost { background: #eef2ff; color: #1e3a8a; }
  input, textarea { width: 100%; padding: 10px 12px; border: 1px solid #d1d5db; border-radius: 10px; font: inherit; margin: 6px 0 12px; }
  textarea { min-height: 88px; resize: vertical; }
  .row { display: flex; gap: 8px; }
  .row input { flex: 1; }
  .answer { white-space: pre-wrap; line-height: 1.65; margin-top: 12px; }
  .sources { color: #6b7280; font-size: 12px; margin-top: 6px; }
  .ok { color: #047857; }
  .err { color: #b91c1c; }
  .actions { display: flex; gap: 10px; flex-wrap: wrap; margin-top: 12px; }
</style>
</head>
<body>
<div class="wrap">
  <div class="card">
    <h1>{{.Title}}</h1>
    {{if .Keyword}}<div class="muted">关于「{{.Keyword}}」的常见问题，可以直接问。</div>{{end}}
    <div class="actions">
      {{if .TargetURL}}<a class="btn ghost" href="{{.TargetURL}}" rel="noopener">查看详情</a>{{end}}
    </div>
  </div>

  <div class="card">
    <h2 style="font-size:18px;margin:0 0 10px">先问一句</h2>
    <form id="askForm">
      <div class="row">
        <input id="question" maxlength="500" placeholder="例如：这个方案适合我们吗？" required>
        <button class="btn" id="askBtn" type="submit">提问</button>
      </div>
    </form>
    <div id="answerBox" class="answer"></div>
    <div id="sources" class="sources"></div>
  </div>

  <div class="card">
    <h2 style="font-size:18px;margin:0 0 10px">留下联系方式</h2>
    <div class="muted" style="margin-bottom:10px">我们会尽快联系你，不会公开你的联系方式。</div>
    <form id="leadForm">
      <input id="contact" maxlength="120" placeholder="手机号 / 邮箱 / 微信" required>
      <textarea id="message" maxlength="1000" placeholder="想了解什么？（选填）"></textarea>
      <button class="btn" id="leadBtn" type="submit">提交</button>
      <div id="leadMsg" class="muted" style="margin-top:8px"></div>
    </form>
  </div>

  <div class="muted">Powered by HiveMtk GEO 承接页</div>
</div>
<script>
(function () {
  var ASK = {{.AskPath}};
  var LEAD = {{.LeadPath}};
  function post(url, body) {
    return fetch(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })
      .then(function (r) {
        return r.json().catch(function () { return {}; }).then(function (j) {
          if (!r.ok) throw new Error(j.msg || j.message || '请求失败');
          return j.data || j;
        });
      });
  }
  document.getElementById('askForm').addEventListener('submit', function (e) {
    e.preventDefault();
    var q = document.getElementById('question').value.trim();
    if (!q) return;
    var btn = document.getElementById('askBtn');
    var box = document.getElementById('answerBox');
    var src = document.getElementById('sources');
    btn.disabled = true; box.className = 'answer muted'; box.textContent = '正在查知识库…'; src.textContent = '';
    post(ASK, { question: q }).then(function (d) {
      box.className = 'answer';
      box.textContent = d.answer || '暂时没有答案。';
      src.textContent = (d.sources || []).length ? '参考：' + d.sources.join(' / ') : '';
    }).catch(function (err) {
      box.className = 'answer err'; box.textContent = err.message;
    }).then(function () { btn.disabled = false; });
  });
  document.getElementById('leadForm').addEventListener('submit', function (e) {
    e.preventDefault();
    var contact = document.getElementById('contact').value.trim();
    var msg = document.getElementById('message').value.trim();
    if (!contact) return;
    var btn = document.getElementById('leadBtn');
    var out = document.getElementById('leadMsg');
    btn.disabled = true; out.className = 'muted'; out.textContent = '提交中…';
    post(LEAD, { contact: contact, contact_type: 'other', message: msg }).then(function () {
      out.className = 'ok'; out.textContent = '已收到，我们会尽快联系你。';
      document.getElementById('contact').value = '';
      document.getElementById('message').value = '';
    }).catch(function (err) {
      out.className = 'err'; out.textContent = err.message;
    }).then(function () { btn.disabled = false; });
  });
})();
</script>
</body>
</html>`))

// Landing GET /api/public/geo/l/:code
//
// 默认渲染 HTML 落地页（访客直接打开），?format=json 返回结构化数据。
func (c *PublicController) Landing(ctx *gin.Context) {
	code := strings.TrimSpace(ctx.Param("code"))
	format := strings.TrimSpace(ctx.Query("format"))
	accept := ctx.GetHeader("Accept")
	asJSON := format == "json" || (strings.Contains(accept, "application/json") && !strings.Contains(accept, "text/html"))
	link, err := c.svc.Landing(ctx.Request.Context(), code, !asJSON)
	if err != nil {
		if asJSON {
			response.Error(ctx, http.StatusNotFound, "落地页不存在或已下线")
			return
		}
		ctx.Data(http.StatusNotFound, "text/html; charset=utf-8", []byte("<h1>落地页不存在或已下线</h1>"))
		return
	}
	if asJSON {
		response.Success(ctx, service.CaptureLandingView{
			Code: link.Code, Title: link.Title, Keyword: link.Keyword, TargetURL: link.TargetURL,
		}, "ok")
		return
	}
	data := capturePageData{
		Title: link.Title, Keyword: link.Keyword, Code: link.Code, TargetURL: link.TargetURL,
		AskPath: "/api/public/geo/l/" + link.Code + "/ask", LeadPath: "/api/public/geo/l/" + link.Code + "/lead",
	}
	ctx.Header("Content-Type", "text/html; charset=utf-8")
	ctx.Header("X-Content-Type-Options", "nosniff")
	ctx.Header("Referrer-Policy", "no-referrer")
	if err := captureLandingTpl.Execute(ctx.Writer, data); err != nil {
		response.Error(ctx, http.StatusInternalServerError, "渲染落地页失败")
	}
}

// Ask POST /api/public/geo/l/:code/ask
func (c *PublicController) Ask(ctx *gin.Context) {
	var req dto.CaptureAskRequest
	if !response.BindJSON(ctx, &req) {
		return
	}
	resp, err := c.svc.Ask(ctx.Request.Context(), ctx.Param("code"), req.Question)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, resp, "ok")
}

// Lead POST /api/public/geo/l/:code/lead
func (c *PublicController) Lead(ctx *gin.Context) {
	var req dto.CaptureLeadRequest
	if !response.BindJSON(ctx, &req) {
		return
	}
	clueID, err := c.svc.SubmitLead(ctx.Request.Context(), ctx.Param("code"), req.Contact, req.ContactType, req.Message)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, gin.H{"clue_id": clueID}, "已收到")
}
