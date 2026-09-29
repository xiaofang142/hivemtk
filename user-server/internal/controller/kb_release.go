// kb_release.go G-5 知识库变更流程的 HTTP 出口（新规划任务清单 T-P9-02）。
//
// 本层只负责四件事（与 bad_case.go 同一分工，刻意不做第五件）：
//  1. **身份**：提交、撤回、发布、回滚、启停治理都要落到具体的人 —— requested_by 与
//     changed_by 是 AC③ 留痕的唯一出处。取不到身份回 401，而不是"以空串处理"。
//  2. **形状**：查询参数 → service.KBChangeListQuery，以及 {code,data,message} 契约。
//  3. **值域**：status / op 的枚举闸（见 kbChangeQueryValues 的注释——为什么在这儿拦）。
//  4. **状态码**：服务侧 sentinel 各对应一个语义（400/404/409/503）。
//
// 门槛判断、跃迁表、审批结论的读取全在服务层，这里一行都不重写。
package controller

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/kbrelease"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
)

// KBReleaseController 变更请求与发布指针的共用控制器。
//
// svc 允许是 nil（本竖未装配）：除闸门档位那一条之外，所有出口走 Available() 回 503。
type KBReleaseController struct {
	svc *service.KBReleaseService
}

// NewKBReleaseController 构造。
func NewKBReleaseController(svc *service.KBReleaseService) *KBReleaseController {
	return &KBReleaseController{svc: svc}
}

// RegisterRoutes 挂到已鉴权的 /api 组下（路径按 CLAUDE.md 的 /api/{domain}/{resource}）。
//
// 两组路径分开（变更 / 发布指针）而不是一组带斜杠嵌套，是因为两者的生命周期与归属人不同：
// 变更行归发起人（能提交、能撤回、能看自己的留痕），发布指针归库（发布/回滚/启停治理）。
// 混成一组会让人以为"撤回一条变更"和"回滚一版"是同一件事 —— 它们不是，
// 前者动一行状态、后者动一个指针，且后者不需要任何新的批准记录。
func (c *KBReleaseController) RegisterRoutes(router *gin.RouterGroup) {
	ch := router.Group("/kb-changes")
	{
		ch.GET("", c.ListChanges)
		ch.GET("/taxonomy", c.Taxonomy)
		ch.POST("", c.SubmitChange)
		ch.GET("/:id", c.GetChange)
		ch.POST("/:id/withdraw", c.WithdrawChange)
		ch.GET("/:id/audit", c.ChangeAudit)
	}
	rel := router.Group("/kb-releases")
	{
		// gate 必须是**静态段**且在 :product 之前注册：gin 在同一层优先匹配静态节点，
		// 而这条端点读的是进程 env、不依赖底座 —— 底座没装配时它仍要答得出档位。
		rel.GET("/gate", c.Gate)
		rel.GET("", c.ListReleases)
		rel.GET("/:product", c.GetRelease)
		rel.GET("/:product/stats", c.Stats)
		rel.GET("/:product/audit", c.ReleaseAudit)
		rel.POST("/:product/publish", c.Publish)
		rel.POST("/:product/rollback", c.Rollback)
		rel.POST("/:product/restore", c.Restore)
		rel.PATCH("/:product/governed", c.SetGoverned)
	}
}

// kbChangeIDParamMaxLen 变更号长度闸（生成格式 kbc_<unixnano>_<seq> 用不到 30 字符，
// 列宽是 40）。收窄的理由与坏例同一条：号会被拼进 subject_key 回查，无上限的入参
// 等于给调用方一个免费的慢查询。
const kbChangeIDParamMaxLen = 64

// SubmitChange POST /api/kb-changes —— 提交一条变更（同时入队一条审批）。
func (c *KBReleaseController) SubmitChange(ctx *gin.Context) {
	if !c.svc.Available() {
		c.unavailable(ctx)
		return
	}
	operator, ok := contextOperatorID(ctx)
	if !ok {
		response.Error(ctx, http.StatusUnauthorized, "提交变更必须带操作者身份（未登录或会话里没有 user id）")
		return
	}
	var body struct {
		ProductID     string `json:"product_id"`
		DocumentID    uint64 `json:"document_id"`
		Op            string `json:"op"`
		TargetChunkID uint64 `json:"target_chunk_id"`
		Content       string `json:"content"`
		Reason        string `json:"reason"`
	}
	if err := ctx.ShouldBindJSON(&body); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求体不是合法 JSON："+err.Error())
		return
	}
	row, err := c.svc.SubmitChange(ctx.Request.Context(), service.KBChangeSubmitInput{
		ProductID:     body.ProductID,
		DocumentID:    body.DocumentID,
		Op:            body.Op,
		TargetChunkID: body.TargetChunkID,
		Content:       body.Content,
		Reason:        body.Reason,
		RequestedBy:   operator,
	})
	if err != nil {
		c.replyError(ctx, err)
		return
	}
	response.Success(ctx, row, "ok")
}

// WithdrawChange POST /api/kb-changes/:id/withdraw —— 发起人撤回一条还没发布的变更。
//
// 不收请求体：撤回的"为什么"在 AC③ 里由跃迁本身表达（pending→withdrawn + 谁按的），
// 而收一个可选理由字段会立刻产生两种留痕（有理由的和没有的），后者在审计里没有价值。
// 已发布的撤不掉：那需要一条新的 retire 变更和一次新的批准，不是一条特权路径。
func (c *KBReleaseController) WithdrawChange(ctx *gin.Context) {
	if !c.svc.Available() {
		c.unavailable(ctx)
		return
	}
	id, ok := kbChangeIDParam(ctx)
	if !ok {
		return
	}
	operator, ok := contextOperatorID(ctx)
	if !ok {
		response.Error(ctx, http.StatusUnauthorized, "撤回变更必须带操作者身份（未登录或会话里没有 user id）")
		return
	}
	row, err := c.svc.WithdrawChange(ctx.Request.Context(), id, operator)
	if err != nil {
		c.replyError(ctx, err)
		return
	}
	response.Success(ctx, row, "ok")
}

// ListChanges GET /api/kb-changes
func (c *KBReleaseController) ListChanges(ctx *gin.Context) {
	if !c.svc.Available() {
		c.unavailable(ctx)
		return
	}
	f, ok := kbChangeQueryFromRequest(ctx)
	if !ok {
		return // helper 已经回过 400
	}
	list, total, err := c.svc.ListChanges(ctx.Request.Context(), f)
	if err != nil {
		c.replyError(ctx, err)
		return
	}
	// 空结果回 []，不回 null（同 intent.go:198 那一条兜底）。
	// 本层的判据是"空列表是一句业务结论"，而 null 让每个调用点都得先判类型再 .map ——
	// 那句 `|| []` 散落十处之后，"这个库没有待办"与"这次没带回数组"在页面上又长一样了。
	if list == nil {
		list = []model.KBChangeRequest{}
	}
	response.SuccessWithList(ctx, list, total)
}

// GetChange GET /api/kb-changes/:id
func (c *KBReleaseController) GetChange(ctx *gin.Context) {
	if !c.svc.Available() {
		c.unavailable(ctx)
		return
	}
	id, ok := kbChangeIDParam(ctx)
	if !ok {
		return
	}
	row, err := c.svc.GetChange(ctx.Request.Context(), id)
	if err != nil {
		c.replyError(ctx, err)
		return
	}
	if row == nil {
		// 服务侧"读不到"是 (nil, nil)，404 这个判断归本层。
		response.Error(ctx, http.StatusNotFound, "变更不存在")
		return
	}
	response.Success(ctx, row, "ok")
}

// ChangeAudit GET /api/kb-changes/:id/audit —— 一条变更的全部动作（AC③ 的读口）
func (c *KBReleaseController) ChangeAudit(ctx *gin.Context) {
	if !c.svc.Available() {
		c.unavailable(ctx)
		return
	}
	id, ok := kbChangeIDParam(ctx)
	if !ok {
		return
	}
	rows, ok := c.audit(ctx, "change:"+id)
	if !ok {
		return
	}
	response.SuccessWithList(ctx, rows, int64(len(rows)))
}

// ReleaseAudit GET /api/kb-releases/:product/audit —— 一个库发布指针的全部移动
func (c *KBReleaseController) ReleaseAudit(ctx *gin.Context) {
	if !c.svc.Available() {
		c.unavailable(ctx)
		return
	}
	product, ok := kbReleaseProductParam(ctx)
	if !ok {
		return
	}
	rows, ok := c.audit(ctx, "release:"+product)
	if !ok {
		return
	}
	response.SuccessWithList(ctx, rows, int64(len(rows)))
}

// audit 两条留痕读口的共用体：subject_key 由调用方拼好，这里只管上限与错误。
//
// 上限不传就是 50（仓储口径）。留痕是只增的事件流，没有上限的一次读取等于把
// 一张会一直长大的表整份交给浏览器 —— 而审计的用途恰恰是"查最近谁动的"。
func (c *KBReleaseController) audit(ctx *gin.Context, subjectKey string) ([]model.KBChangeAuditLog, bool) {
	limit := 0
	if raw := strings.TrimSpace(ctx.Query("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			response.Error(ctx, http.StatusBadRequest, "limit 必须是正整数")
			return nil, false
		}
		limit = n
	}
	rows, err := c.svc.ListAudit(ctx.Request.Context(), subjectKey, limit)
	if err != nil {
		c.replyError(ctx, err)
		return nil, false
	}
	// 与列表出口同一兜底：留痕为空是"这条没被动过"这句结论，不是一个需要前端特判的 null。
	if rows == nil {
		rows = []model.KBChangeAuditLog{}
	}
	return rows, true
}

// ListReleases GET /api/kb-releases —— 已启用发布制的库
func (c *KBReleaseController) ListReleases(ctx *gin.Context) {
	if !c.svc.Available() {
		c.unavailable(ctx)
		return
	}
	rows, err := c.svc.ListReleases(ctx.Request.Context())
	if err != nil {
		c.replyError(ctx, err)
		return
	}
	if rows == nil {
		// 一个库都没启用时回 []，不回 null："还没有库进发布制"是一句业务结论。
		rows = []model.KBRelease{}
	}
	response.Success(ctx, rows, "ok")
}

// GetRelease GET /api/kb-releases/:product —— 一个库的发布指针
func (c *KBReleaseController) GetRelease(ctx *gin.Context) {
	if !c.svc.Available() {
		c.unavailable(ctx)
		return
	}
	product, ok := kbReleaseProductParam(ctx)
	if !ok {
		return
	}
	rel, err := c.svc.GetRelease(ctx.Request.Context(), product)
	if err != nil {
		c.replyError(ctx, err)
		return
	}
	if rel == nil {
		// "没有行"是一个可读的事实（这个库从没被启停过治理），不是 404：
		// 管理端要在这个位置展示"未启用发布制"，而 404 会被前端读成"库不存在"。
		response.Success(ctx, gin.H{"product_id": product, "exists": false, "governed": false}, "ok")
		return
	}
	response.Success(ctx, rel, "ok")
}

// Stats GET /api/kb-releases/:product/stats —— 分条数 / 在服数 / 待办数 / 闸门档位
func (c *KBReleaseController) Stats(ctx *gin.Context) {
	if !c.svc.Available() {
		c.unavailable(ctx)
		return
	}
	product, ok := kbReleaseProductParam(ctx)
	if !ok {
		return
	}
	stats, err := c.svc.Stats(ctx.Request.Context(), product)
	if err != nil {
		c.replyError(ctx, err)
		return
	}
	response.Success(ctx, stats, "ok")
}

// Publish POST /api/kb-releases/:product/publish —— 把已批准的变更落库并前移指针
//
// 返回体是"两块账"而不是一个布尔：verdicts 说这次每条待办变更的去向（带上 / 等审批 /
// 被拒 / 过期 / 结论不明），result 说指针从哪一格移到哪一格、落了哪几条。
// 既没有已批准的变更、待发布桶又是空的（= 这库这期间什么都没攒下）时 result 为 null ——
// 那是"这次什么都没发生"，与"发生了一次空发布"必须是两种可区分的读数
// （后者会在留痕里留下一条不存在的发布）。桶上有货时即使一条变更也没批准，
// 也真的会发布（那批货是导入链路攒下的）。
func (c *KBReleaseController) Publish(ctx *gin.Context) {
	if !c.svc.Available() {
		c.unavailable(ctx)
		return
	}
	product, ok := kbReleaseProductParam(ctx)
	if !ok {
		return
	}
	operator, ok := contextOperatorID(ctx)
	if !ok {
		response.Error(ctx, http.StatusUnauthorized, "发布必须带操作者身份（留痕里的 changed_by 是追责依据）")
		return
	}
	verdicts, result, err := c.svc.PublishPending(ctx.Request.Context(), product, operator)
	if err != nil {
		c.replyError(ctx, err)
		return
	}
	response.Success(ctx, gin.H{"verdicts": verdicts, "result": result}, "ok")
}

// Rollback POST /api/kb-releases/:product/rollback —— 生效指针回拨一格（不碰语料）
func (c *KBReleaseController) Rollback(ctx *gin.Context) {
	c.movePointer(ctx, false)
}

// Restore POST /api/kb-releases/:product/restore —— 把刚被回滚掉的那一版放回去
func (c *KBReleaseController) Restore(ctx *gin.Context) {
	c.movePointer(ctx, true)
}

// movePointer 回滚与放回共用一条出口：两者都只是同一行上四个号的换法，
// 差别只是方向。分成两条路由（而不是一个 body 里带 direction）是为了让权限与
// 审计动作各归各的名 —— "回滚"和"放回被回滚的那版"在留痕里是两个动作（rollback / restored）。
func (c *KBReleaseController) movePointer(ctx *gin.Context, forward bool) {
	if !c.svc.Available() {
		c.unavailable(ctx)
		return
	}
	product, ok := kbReleaseProductParam(ctx)
	if !ok {
		return
	}
	operator, ok := contextOperatorID(ctx)
	if !ok {
		response.Error(ctx, http.StatusUnauthorized, "版本移动必须带操作者身份（留痕里的 changed_by 是追责依据）")
		return
	}
	var (
		res any
		err error
	)
	if forward {
		res, err = c.svc.Restore(ctx.Request.Context(), product, operator)
	} else {
		res, err = c.svc.Rollback(ctx.Request.Context(), product, operator)
	}
	if err != nil {
		c.replyError(ctx, err)
		return
	}
	response.Success(ctx, res, "ok")
}

// SetGoverned PATCH /api/kb-releases/:product/governed  body: {"governed":true}
//
// 用 PATCH 而不是 POST：这一格改的是"这个库受不受管"这一个布尔属性，不创建也不删除发布行
// （启用后指针全为 0，语料照常按存量可见）。
// 幂等：重复置同一值返回同一份记录，只在真正翻转的那一刻记一条 governed 留痕。
func (c *KBReleaseController) SetGoverned(ctx *gin.Context) {
	if !c.svc.Available() {
		c.unavailable(ctx)
		return
	}
	product, ok := kbReleaseProductParam(ctx)
	if !ok {
		return
	}
	operator, ok := contextOperatorID(ctx)
	if !ok {
		response.Error(ctx, http.StatusUnauthorized, "启停发布制必须带操作者身份（governed 翻转是治理动作，要落到人）")
		return
	}
	var body struct {
		Governed *bool `json:"governed"`
	}
	if err := ctx.ShouldBindJSON(&body); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求体不是合法 JSON："+err.Error())
		return
	}
	if body.Governed == nil {
		// 必须显式给布尔值：漏字段就当成"启用"是一个会扩大治理范围的静默动作，
		// 而这一格的正确方向恰好相反 —— 宁可不改，不可多改。
		response.Error(ctx, http.StatusBadRequest, `请求体缺少 governed 布尔字段（形如 {"governed":true}）`)
		return
	}
	rel, err := c.svc.SetGoverned(ctx.Request.Context(), product, *body.Governed, operator)
	if err != nil {
		c.replyError(ctx, err)
		return
	}
	response.Success(ctx, rel, "ok")
}

// Gate GET /api/kb-releases/gate —— 闸门档位回显
//
// 这条端点**不查底座是否可用**：档位是进程事实（读 env），与有没有 DB 句柄无关。
// 底座没装配时运维最需要知道的恰恰是"闸门现在到底开到了哪一档"，
// 让它连带着回 503 就等于在最需要答案的时候不给答案。
// governed_* 两格在底座不可用时为 null（"没读到"而不是"一个都没有"）。
func (c *KBReleaseController) Gate(ctx *gin.Context) {
	snap := gin.H{
		"flag_env":     kbrelease.FlagEnv,
		"mode":         service.GateMode(),
		"blocks_read":  kbrelease.GateOn(),
		"stamps_write": kbrelease.ShadowsOrOn(),
	}
	if !c.svc.Available() {
		snap["governed_products"] = nil
		snap["note"] = "变更底座未装配（无 DB 句柄）⇒ 受治理库名单读不出来，上面三项仍为进程真实档位"
		response.Success(ctx, snap, "ok")
		return
	}
	rows, err := c.svc.ListReleases(ctx.Request.Context())
	if err != nil {
		snap["governed_products"] = nil
		snap["governed_note"] = "受治理库名单读取失败：" + err.Error()
		response.Success(ctx, snap, "ok")
		return
	}
	products := make([]string, 0, len(rows))
	for _, r := range rows {
		products = append(products, r.ProductID)
	}
	snap["governed_products"] = products
	snap["governed_count"] = len(products)
	response.Success(ctx, snap, "ok")
}

// Taxonomy GET /api/kb-changes/taxonomy —— 动作与状态的值域
//
// 与坏例那条同理：值域一旦在前端抄一份，库里加一个状态就会出现"判得出来、选不到"。
func (c *KBReleaseController) Taxonomy(ctx *gin.Context) {
	response.Success(ctx, gin.H{
		"ops":                 model.KBChangeOps,
		"statuses":            model.KBChangeStatuses,
		"audit_actions":       model.KBChangeAuditActions,
		"approval_subject":    service.KBChangeApprovalSubjectType,
		"approval_policy_key": service.KBChangeApprovalPolicyKey,
	}, "ok")
}

// unavailable 503 出口：不带任何数据字段。
//
// 空列表与 {"pending_changes":0} 都是一句业务结论（"这个库没有待办"），
// 而此刻的事实是"一次都没读到"。两种读法在值班手上的动作完全相反。
func (c *KBReleaseController) unavailable(ctx *gin.Context) {
	response.Error(ctx, http.StatusServiceUnavailable,
		"知识库变更底座不可用（未装配或缺少 DB 句柄），本次未读到任何发布状态")
}

// replyError 把服务/仓储的 sentinel 翻成状态码。
//
// 三条分档的判据是"调用方下一步该做什么"：
//   - 400 = 改请求（超长、空值、op 越界）；
//   - 404 = 这条不存在（前端该刷新列表）；
//   - 409 = 当前状态不允许这个动作（该刷新指针视图 / 先处置被回滚的版本），
//     重试同一次请求不会有不同结果；
//   - 503 = 依赖没就绪（底座未装配或审批没给结论），该报装配而不是改请求；
//   - 其余 = 500，且必须留一行 Warn —— 未预期的错误静默回 500 会让"哪个环节漂了"
//     只剩响应体一句话，而这句话里不许带内部错误细节。
//
// 服务层的 sentinel 与仓储的是**同一个变量**（service/kb_release.go 的同名转授），
// 所以这里一条 errors.Is 就够，再写一条 repository.Xxx 是死分支。
func (c *KBReleaseController) replyError(ctx *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrKBReleaseUnavailable):
		response.Error(ctx, http.StatusServiceUnavailable, err.Error())
	case errors.Is(err, service.ErrKBReleaseApprovalMissing):
		response.Error(ctx, http.StatusServiceUnavailable, err.Error())
	case errors.Is(err, service.ErrKBChangeInputInvalid):
		response.Error(ctx, http.StatusBadRequest, err.Error())
	case errors.Is(err, service.ErrKBChangeNotFound):
		response.Error(ctx, http.StatusNotFound, err.Error())
	case errors.Is(err, service.ErrKBChangeNotWithdrawable),
		errors.Is(err, service.ErrKBReleaseNotGoverned),
		errors.Is(err, service.ErrKBReleaseRollbackUnavailable),
		errors.Is(err, service.ErrKBReleaseRestoreUnavailable),
		errors.Is(err, service.ErrKBReleaseRecallBan),
		errors.Is(err, service.ErrKBReleaseNothingToPublish),
		errors.Is(err, service.ErrKBReleaseTargetConflict),
		errors.Is(err, service.ErrKBReleaseChangeNotPending),
		errors.Is(err, kbrelease.ErrGovernedDirectWrite):
		response.Error(ctx, http.StatusConflict, err.Error())
	default:
		logger.Warnf("[kb-release] 未预期错误: %v", err)
		response.Error(ctx, http.StatusInternalServerError, "知识库变更操作失败")
	}
}

// kbChangeQueryFromRequest 解析变更列表的查询参数。返回 ok=false 表示已回过错误响应。
func kbChangeQueryFromRequest(ctx *gin.Context) (service.KBChangeListQuery, bool) {
	var q service.KBChangeListQuery
	productID := strings.TrimSpace(ctx.Query("product_id"))
	if len(productID) > service.KBReleaseProductMaxLen {
		response.Error(ctx, http.StatusBadRequest, "product_id 过长")
		return q, false
	}
	q.ProductID = productID

	// 值域在这里就拦，而不是透传给仓储拿"空列表"：筛选器打错一个字母时的两种读数
	// —— "这个库没有这种状态的变更" 与 "你选了个不存在的状态" —— 在页面上长得一模一样，
	// 而后者才是事实。空列表是一句业务结论，不能由输入错误代答。
	status := strings.TrimSpace(ctx.Query("status"))
	if status != "" && !model.IsValidKBChangeStatus(status) {
		response.Error(ctx, http.StatusBadRequest, "status 必须是 "+strings.Join(model.KBChangeStatuses, "/")+" 之一")
		return q, false
	}
	q.Status = status

	op := strings.ToLower(strings.TrimSpace(ctx.Query("op")))
	if op != "" && !model.IsValidKBChangeOp(op) {
		response.Error(ctx, http.StatusBadRequest, "op 必须是 "+strings.Join(model.KBChangeOps, "/")+" 之一")
		return q, false
	}
	q.Op = op

	page, size, ok := kbChangePaging(ctx)
	if !ok {
		return q, false
	}
	q.Limit = size
	q.Offset = (page - 1) * size
	return q, true
}

// kbChangePaging 解析分页（page 从 1 起，翻成仓储的 Limit/Offset）。
//
// 给了非法值直接 400，不做"那就当第一页"的静默纠正：前端把 page 传成 0 是 bug，
// 替它兜住只会让翻页永远停在第一页而没人发现。
func kbChangePaging(ctx *gin.Context) (int, int, bool) {
	page, size := 1, 50
	rawPage, rawSize := strings.TrimSpace(ctx.Query("page")), strings.TrimSpace(ctx.Query("page_size"))
	if rawPage != "" {
		n, err := strconv.Atoi(rawPage)
		if err != nil || n < 1 {
			response.Error(ctx, http.StatusBadRequest, "page 必须是从 1 起的整数")
			return 0, 0, false
		}
		page = n
	}
	if rawSize != "" {
		n, err := strconv.Atoi(rawSize)
		if err != nil || n < 1 {
			response.Error(ctx, http.StatusBadRequest, "page_size 必须是正整数")
			return 0, 0, false
		}
		size = n
	}
	return page, size, true
}

func kbChangeIDParam(ctx *gin.Context) (string, bool) {
	id := strings.TrimSpace(ctx.Param("id"))
	if id == "" {
		response.Error(ctx, http.StatusBadRequest, "变更 id 不能为空")
		return "", false
	}
	if utf8.RuneCountInString(id) > kbChangeIDParamMaxLen {
		response.Error(ctx, http.StatusBadRequest, "变更 id 过长")
		return "", false
	}
	return id, true
}

// kbReleaseProductParam 取路径上的 product_id。
//
// 长度闸来自列宽（service.KBReleaseProductMaxLen），不是审美：超长的一次读取会让
// PG 在 varchar(64) 的隐式转换上报 "value too long" ⇒ 同一个原因在别的端点报 400、
// 在这一格报 500。回显时不带入参数原文（响应体放大器）。
func kbReleaseProductParam(ctx *gin.Context) (string, bool) {
	product := strings.TrimSpace(ctx.Param("product"))
	if product == "" {
		response.Error(ctx, http.StatusBadRequest, "product_id 不能为空")
		return "", false
	}
	if utf8.RuneCountInString(product) > service.KBReleaseProductMaxLen {
		response.Error(ctx, http.StatusBadRequest, "product_id 过长")
		return "", false
	}
	return product, true
}
