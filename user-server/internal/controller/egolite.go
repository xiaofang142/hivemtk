package controller

// EgoLiteController ego-lite 腿的 HTTP 层（2026-11-05 二次审计）。
//
// 之前这些处理器以闭包形式直接写在 internal/router/egolite_routes.go 里，
// 同时直连 tokenRepo/flowRepo/execRepo/gormDB 等，违反「Router 只做映射、
// Controller 只调 Service」。现全部收进本控制器：路由文件只保留 DI 装配与方法注册。

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/egolite"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/storage"
)

// EgoLiteController ego-lite 腿控制器。
type EgoLiteController struct {
	runner    *egolite.EgoRunnerAdminService
	exec      *egolite.EgoExecutionService
	flow      *egolite.EgoFlowService
	asset     *egolite.EgoAssetCatalog
	cron      *egolite.EgoFlowCronService
	session   *egolite.EgoSessionService
	telemetry *egolite.EgoTelemetryService
	gov       *egolite.EgoGovernanceService
}

// NewEgoLiteController 装配（依赖全部由 internal/router 的 SetupEgoLiteRoutes 构造后注入）。
func NewEgoLiteController(
	runner *egolite.EgoRunnerAdminService,
	exec *egolite.EgoExecutionService,
	flow *egolite.EgoFlowService,
	asset *egolite.EgoAssetCatalog,
	cron *egolite.EgoFlowCronService,
	session *egolite.EgoSessionService,
	telemetry *egolite.EgoTelemetryService,
	gov *egolite.EgoGovernanceService,
) *EgoLiteController {
	return &EgoLiteController{
		runner: runner, exec: exec, flow: flow, asset: asset,
		cron: cron, session: session, telemetry: telemetry, gov: gov,
	}
}

// ---------------------------------------------------------------- runner 通道

// PullAssets GET /api/egolite/assets/pull（eg_ token 即身份）
func (h *EgoLiteController) PullAssets(c *gin.Context) {
	userID, ok := h.runner.Authenticate(c.Request.Context(), runnerTokenOf(c))
	if !ok {
		response.Envelope(c, 401, gin.H{"code": 1, "message": "runner token 无效"})
		return
	}
	assets, err := h.asset.ListInstallableForUser(c.Request.Context(), userID)
	if err != nil {
		response.Envelope(c, 500, gin.H{"code": 1, "message": egoErrorMessage(err, "服务暂时不可用，请稍后重试")})
		return
	}
	// format=zip（S-7 包通道）：同一份清单打成包下发，runner 原子解包后换入。
	// 默认仍是 JSON 清单——旧 runner 不认 zip，改了默认等于断兼容。
	// S-6：带签名的资产先验签（fail-close）；未配置信任公钥时，已签名资产一律拒。
	pub, kerr := egolite.LoadAssetTrustPubKeyFromEnv()
	if kerr != nil {
		response.Envelope(c, 400, gin.H{"code": 1, "message": kerr.Error()})
		return
	}
	if verr := egolite.VerifyPackageSignatures(assets, pub); verr != nil {
		response.Envelope(c, 400, gin.H{"code": 1, "message": verr.Error()})
		return
	}
	if c.Query("format") == "zip" {
		pkg, perr := egolite.BuildAssetPackage(assets)
		if perr != nil {
			response.Envelope(c, 400, gin.H{"code": 1, "message": perr.Error()})
			return
		}
		c.Data(200, "application/zip", pkg)
		return
	}
	response.Envelope(c, 200, gin.H{"code": 0, "data": gin.H{"assets": assets}})
}

// UploadArtifact POST /api/egolite/runner/artifacts（截图；8MB 上限 + 产物日配额）
func (h *EgoLiteController) UploadArtifact(c *gin.Context) {
	userID, ok := h.runner.Authenticate(c.Request.Context(), runnerTokenOf(c))
	if !ok {
		response.Envelope(c, 401, gin.H{"code": 1, "message": "runner token 无效"})
		return
	}
	const maxArtifact = 8 << 20
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxArtifact+1024)
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		response.Envelope(c, 400, gin.H{"code": 1, "message": "缺少 file 字段或超出大小上限"})
		return
	}
	defer func() { _ = file.Close() }()
	if header.Size > maxArtifact {
		response.Envelope(c, 413, gin.H{"code": 1, "message": "产物超过 8MB 上限"})
		return
	}
	// T21 产物日配额：截图是最容易失控的采集面（脚本循环截图），总量闸在此。
	if qerr := h.gov.CheckArtifactQuota(c.Request.Context(), userID); qerr != nil {
		response.Envelope(c, 429, gin.H{"code": 1, "message": qerr.Error()})
		return
	}
	buf := &bytes.Buffer{}
	if _, err := io.Copy(buf, io.LimitReader(file, maxArtifact+1)); err != nil {
		response.Envelope(c, 500, gin.H{"code": 1, "message": "读取产物失败"})
		return
	}
	b := buf.Bytes()
	if len(b) > maxArtifact {
		response.Envelope(c, 413, gin.H{"code": 1, "message": "产物超过 8MB 上限"})
		return
	}
	ext := ""
	switch {
	case len(b) >= 8 && bytes.Equal(b[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}):
		ext = ".png"
	case len(b) >= 3 && b[0] == 0xff && b[1] == 0xd8 && b[2] == 0xff:
		ext = ".jpg"
	default:
		response.Envelope(c, 400, gin.H{"code": 1, "message": "只接受 PNG/JPEG 产物"})
		return
	}
	baseDir, publicPrefix, folder := storage.LocalSource()
	driver := storage.NewLocalDriver(baseDir, publicPrefix)
	name := fmt.Sprintf("ego-%d-%d%s", userID, time.Now().UnixNano(), ext)
	url, _, err := driver.UploadReader(c.Request.Context(), bytes.NewReader(b), int64(len(b)), folder+"/egolite", name)
	if err != nil {
		response.Envelope(c, 500, gin.H{"code": 1, "message": "产物落盘失败"})
		return
	}
	response.Envelope(c, 200, gin.H{"code": 0, "data": gin.H{"url": url, "bytes": len(b)}})
}

// TrustPEM GET /api/egolite/assets/trust.pem（公钥本就是公开物，免鉴权）
func (h *EgoLiteController) TrustPEM(c *gin.Context) {
	pub, err := egolite.LoadAssetTrustPubKeyFromEnv()
	if err != nil {
		response.Envelope(c, 500, gin.H{"code": 1, "message": egoErrorMessage(err, "服务暂时不可用，请稍后重试")})
		return
	}
	if pub == nil {
		response.Envelope(c, 404, gin.H{"code": 1, "message": "本实例未配置资产信任公钥（EGO_ASSET_TRUST_PUBLIC_KEY）"})
		return
	}
	c.String(200, "# hivemtk ego asset trust store (ed25519)\n# fingerprint=%s\n%s\n",
		egolite.PubKeyFingerprint(pub), base64.StdEncoding.EncodeToString(pub))
}

// IngestTelemetry POST /api/egolite/runner/telemetry（S-9）
func (h *EgoLiteController) IngestTelemetry(c *gin.Context) {
	userID, ok := h.runner.Authenticate(c.Request.Context(), runnerTokenOf(c))
	if !ok {
		response.Envelope(c, 401, gin.H{"code": 1, "message": "runner token 无效"})
		return
	}
	var body struct {
		Events []map[string]any `json:"events"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || len(body.Events) == 0 {
		response.Envelope(c, 400, gin.H{"code": 1, "message": "events 必填（非空数组）"})
		return
	}
	accepted, droppedKeys, truncated, err := h.telemetry.Ingest(c.Request.Context(), userID, body.Events)
	if err != nil {
		response.Envelope(c, 500, gin.H{"code": 1, "message": "遥测入库失败"})
		return
	}
	response.Envelope(c, 200, gin.H{"code": 0, "data": gin.H{
		"accepted": accepted, "dropped_keys": droppedKeys, "truncated": truncated,
	}})
}

// ---------------------------------------------------------------- runner 管理（JWT）

// RunnerStatus GET /egolite/runner/status
func (h *EgoLiteController) RunnerStatus(c *gin.Context) {
	response2(c, h.runner.StatusFor(currentUID(c), c.GetString("role") == "admin"), "ok")
}

// ListRunnerTokens GET /egolite/runner/tokens
func (h *EgoLiteController) ListRunnerTokens(c *gin.Context) {
	list, err := h.runner.ListTokens(c.Request.Context(), currentUID(c))
	if err != nil {
		response.Envelope(c, 500, gin.H{"code": 1, "message": egoErrorMessage(err, "服务暂时不可用，请稍后重试")})
		return
	}
	response.Envelope(c, 200, gin.H{"code": 0, "data": gin.H{"list": list}})
}

// IssueRunnerToken POST /egolite/runner/tokens
func (h *EgoLiteController) IssueRunnerToken(c *gin.Context) {
	var body struct {
		Label string `json:"label"`
	}
	_ = c.ShouldBindJSON(&body)
	plain, rec, err := h.runner.IssueToken(c.Request.Context(), currentUID(c), body.Label)
	if err != nil {
		response.Envelope(c, 500, gin.H{"code": 1, "message": "令牌签发失败"})
		return
	}
	response.Envelope(c, 200, gin.H{"code": 0, "data": gin.H{"token": plain, "token_hint": rec.TokenHint, "id": rec.ID},
		"message": "令牌只显示这一次，请立即复制到 runner 配置"})
}

// RevokeRunnerToken POST /egolite/runner/tokens/:id/revoke
func (h *EgoLiteController) RevokeRunnerToken(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	if err := h.runner.RevokeToken(c.Request.Context(), uint(id), currentUID(c)); err != nil {
		response.Envelope(c, 400, gin.H{"code": 1, "message": "令牌不存在或已撤销"})
		return
	}
	response.Envelope(c, 200, gin.H{"code": 0, "message": "已撤销；该令牌立即失效"})
}

// ResetRunnerToken POST /egolite/runner/token/reset（admin 限定）
func (h *EgoLiteController) ResetRunnerToken(c *gin.Context) {
	if c.GetString("role") != "admin" {
		response.Envelope(c, 403, gin.H{"code": 1, "message": "仅管理员可轮换兜底 runner token"})
		return
	}
	tok, err := h.runner.RotateFallback(c.Request.Context())
	if err != nil {
		response.Envelope(c, 500, gin.H{"code": 1, "message": "令牌轮换失败"})
		return
	}
	response.Envelope(c, 200, gin.H{"code": 0, "data": gin.H{"token": tok},
		"message": "已重置兜底 token；旧值仍可用，直到下一次重置"})
}

// ---------------------------------------------------------------- 流程域

// SaveFlow POST /egolite/flows
func (h *EgoLiteController) SaveFlow(c *gin.Context) {
	var req egolite.SaveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Envelope(c, 400, gin.H{"code": 1, "message": err.Error()})
		return
	}
	flowID := uint(0)
	if v := c.Query("id"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 32); err == nil {
			flowID = uint(n)
		}
	}
	f, report, err := h.flow.Save(c.Request.Context(), currentUID(c), flowID, req)
	if err != nil {
		response.Envelope(c, 400, gin.H{"code": 1, "message": err.Error()})
		return
	}
	response.Envelope(c, 200, gin.H{"code": 0, "data": gin.H{"flow": f, "scan_report": report}})
}

// ListFlows GET /egolite/flows
func (h *EgoLiteController) ListFlows(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	list, total, err := h.flow.ListByUser(c.Request.Context(), currentUID(c), page, size)
	if err != nil {
		response.Envelope(c, 500, gin.H{"code": 1, "message": egoErrorMessage(err, "服务暂时不可用，请稍后重试")})
		return
	}
	response.Envelope(c, 200, gin.H{"code": 0, "data": gin.H{"list": list, "total": total}})
}

// GetFlow GET /egolite/flows/:id
func (h *EgoLiteController) GetFlow(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	f, err := h.flow.GetForUser(c.Request.Context(), uint(id), currentUID(c))
	if err != nil {
		response.Envelope(c, 404, gin.H{"code": 1, "message": "流程不存在"})
		return
	}
	response.Envelope(c, 200, gin.H{"code": 0, "data": f})
}

// RescanFlow POST /egolite/flows/:id/rescan
func (h *EgoLiteController) RescanFlow(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	report, err := h.flow.Rescan(c.Request.Context(), currentUID(c), uint(id))
	if err != nil {
		response.Envelope(c, 400, gin.H{"code": 1, "message": err.Error()})
		return
	}
	response.Envelope(c, 200, gin.H{"code": 0, "data": report})
}

// PublishFlow POST /egolite/flows/:id/publish
func (h *EgoLiteController) PublishFlow(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	f, err := h.flow.Publish(c.Request.Context(), currentUID(c), uint(id))
	if err != nil {
		response.Envelope(c, 400, gin.H{"code": 1, "message": err.Error()})
		return
	}
	response.Envelope(c, 200, gin.H{"code": 0, "data": f})
}

// DryRunFlow POST /egolite/flows/:id/dry-run（draft 通道）
func (h *EgoLiteController) DryRunFlow(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	var body struct {
		Params     json.RawMessage `json:"params"`
		SpaceName  string          `json:"space_name"`
		TimeoutSec int             `json:"timeout_sec"`
	}
	_ = c.ShouldBindJSON(&body)
	e, err := h.flow.DryRun(c.Request.Context(), currentUID(c), uint(id), body.Params, body.SpaceName, body.TimeoutSec)
	if err != nil {
		status := 400
		if err == egolite.ErrConcurrencyGate {
			status = 429
		}
		response.Envelope(c, status, gin.H{"code": 1, "message": err.Error()})
		return
	}
	response.Envelope(c, 200, gin.H{"code": 0, "data": e})
}

// DeleteFlow DELETE /egolite/flows/:id
func (h *EgoLiteController) DeleteFlow(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	if err := h.flow.DeleteForUser(c.Request.Context(), uint(id), currentUID(c)); err != nil {
		response.Envelope(c, 404, gin.H{"code": 1, "message": "流程不存在"})
		return
	}
	// 流程删掉后定时触发器必须一并摘除：残留行会继续被调度器扫到（fire 闭包虽会拒绝），
	// 且 GET /flows/:id/cron 仍会返回「已挂载」，与「流程已删除」自相矛盾。
	// 未挂载时 Delete 返回 ErrFlowCronNotFound，属正常分支，不算失败。
	if err := h.cron.Delete(c.Request.Context(), currentUID(c), uint(id)); err != nil && !errors.Is(err, egolite.ErrFlowCronNotFound) {
		egoLogWarnf("egolite: 删除流程 %d 后摘除定时触发器失败: %v", id, err)
	}
	response.Envelope(c, 200, gin.H{"code": 0, "message": "已删除"})
}

// SubmitFlow POST /egolite/flows/:id/submit
func (h *EgoLiteController) SubmitFlow(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	f, err := h.flow.Submit(c.Request.Context(), currentUID(c), uint(id))
	if err != nil {
		response.Envelope(c, 400, gin.H{"code": 1, "message": err.Error()})
		return
	}
	response.Envelope(c, 200, gin.H{"code": 0, "data": f})
}

// GetFlowCron GET /egolite/flows/:id/cron
func (h *EgoLiteController) GetFlowCron(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	tr, err := h.cron.Get(c.Request.Context(), currentUID(c), uint(id))
	if err != nil {
		response.Envelope(c, 404, gin.H{"code": 1, "message": "流程未挂载定时触发器"})
		return
	}
	response.Envelope(c, 200, gin.H{"code": 0, "data": tr})
}

// UpsertFlowCron POST /egolite/flows/:id/cron
func (h *EgoLiteController) UpsertFlowCron(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	var body struct {
		CronExpr string `json:"cron_expr"`
		TimeZone string `json:"time_zone"`
		Enabled  *bool  `json:"enabled"` // nil ⇒ 默认 true；显式 false 原样落库
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.CronExpr == "" {
		response.Envelope(c, 400, gin.H{"code": 1, "message": "cron_expr 必填（5 段「分 时 日 月 周」）"})
		return
	}
	tr, err := h.cron.Upsert(c.Request.Context(), currentUID(c), uint(id), egolite.UpsertCronRequest{
		CronExpr: body.CronExpr, TimeZone: body.TimeZone, Enabled: body.Enabled,
	})
	if err != nil {
		response.Envelope(c, 400, gin.H{"code": 1, "message": egoErrorMessage(err, "挂载定时触发器失败")})
		return
	}
	response.Envelope(c, 200, gin.H{"code": 0, "data": tr, "message": "已挂载；纯读流程按表达式定时发起执行"})
}

// DeleteFlowCron DELETE /egolite/flows/:id/cron
func (h *EgoLiteController) DeleteFlowCron(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	if err := h.cron.Delete(c.Request.Context(), currentUID(c), uint(id)); err != nil {
		response.Envelope(c, 404, gin.H{"code": 1, "message": "流程未挂载定时触发器"})
		return
	}
	response.Envelope(c, 200, gin.H{"code": 0, "message": "已摘除定时触发器"})
}

// ---------------------------------------------------------------- 共享辅助

// runnerTokenOf 取 runner 明文令牌：query 优先，Authorization: Bearer 兜底。
func runnerTokenOf(c *gin.Context) string {
	if tok := c.Query("token"); tok != "" {
		return tok
	}
	return strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
}

// currentUID 从 JWT 注入的 claims 取 user_id（host.go GetStatus 同取法 c.GetUint）
func currentUID(c *gin.Context) uint {
	return c.GetUint("user_id")
}

// response2 统一响应形状（response.Success 别名，避免 import 环）
func response2(c *gin.Context, data any, msg string) {
	response.Envelope(c, 200, gin.H{"code": 0, "message": msg, "data": data})
}

// egoErrorMessage 收敛对外错误文案：领域错误本身就是中文（如「流程不存在」），原样返回；
// 驱动/数据库/运行时错误含内部细节（SQLSTATE、连接串等），一律用 fallback 兜住，避免泄漏。
func egoErrorMessage(err error, fallback string) string {
	if err == nil {
		return fallback
	}
	msg := strings.ToLower(err.Error())
	for _, marker := range []string{
		"sqlstate", "record not found", "duplicate key", "failed to",
		"connection refused", "connection reset", "timeout", "panic",
		"nil pointer", "invalid connection", "no such host",
	} {
		if strings.Contains(msg, marker) {
			return fallback
		}
	}
	return err.Error()
}

// egoLogWarnf egolite 控制器层的轻量告警出口。
func egoLogWarnf(format string, args ...any) {
	log.Printf("[WARN] "+format, args...)
}
