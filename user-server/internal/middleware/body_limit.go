package middleware

import (
	"net/http"
	"os"
	"strconv"
	"strings"

	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

// DefaultMaxJSONBodyMB 是 JSON / 表单请求体的全局上限（MB）。
//
// ⚠️ 背景（2026-09-21 · 第十六轮登记的残项）：本仓此前只在**个别入口**设了 body 上限
// （公网 webhook、MCP 口、bridge 入站、微信回调、商机口各自为政），
// gin 引擎层面对 JSON 请求体**不设默认上限** ⇒ `c.ShouldBindJSON` 会把整个 body 读进内存。
// 未封顶的那批口（authed 内部口为主，如 asset-market 的 `io.ReadAll(c.Request.Body)`
// 与审计中间件整段读体）就是"一条 POST 要多少内存"的面。
//
// 取值依据（按本仓自己的既有上界推，不是照抄平台端）——逐个 grep 出来的现状：
//   - webhook 可调上界 `maxWebhookMaxBody` = 4MB（`ORDER_WEBHOOK_MAX_BODY_BYTES` 的合法顶格值）
//   - `service.ReadAll`（验签前读体）2MB、MCP 口 / bridge 入站 / 微信回调各 1MB、商机口 4KB
//
// 全局值必须**高于**这些既有的按端点上界，否则它会变成新的天花板、把运维已调好的高段
// 静默吃掉（该方向由 TestGlobalDefaultDoesNotTightenExistingCaps 钉住）。
// 8MB 是"高于全部既有上界且仍远小于内存吃满"的最低档；更大的合法载荷应走 multipart
// （已排除，见 BodyLimit 注释）或按 MAX_JSON_BODY_MB 显式调高。
const DefaultMaxJSONBodyMB = 8

// maxMultipartMemoryMB 是 multipart 走 gin 内存缓冲时的上限，供装配处设
// gin.Engine.MaxMultipartMemory（本中间件不处理 multipart）。
//
// 取值依据：gin 自带默认是 **32MB**（`gin.go` 的 `defaultMultipartMemory = 32 << 20`），
// 而本仓最大的合法单文件是 50MB（知识库导入 `MaxUploadFileSize`）、次之为聊天媒体 20MB、
// 素材/通用上传 10MB —— 没有任何一档需要把整份文件留在内存里：超出这个缓冲的部分
// gin 会自动落临时文件，`SaveUploadedFile`/`io.Copy` 两条路径都照常工作。
// 所以这道装配是**把默认值调小**（每并发上传请求的内存降到 1/4），不是照抄别处的更大值。
const maxMultipartMemoryMB = 8

// maxJSONBodyMBProvider 是全局 body 上限的取值口。默认返回常量，等价于参数中心未装配时的行为。
//
// 做成 provider 而不是直接在这里调 config_param service：middleware 不能反向依赖 service
// （service 层已经有一堆包，middleware 被它们间接持有，反向依赖会成环），
// 装配点在 internal/app，与本仓其余 8 批参数接线同一套范式。
var maxJSONBodyMBProvider = func() int { return DefaultMaxJSONBodyMB }

// SetMaxJSONBodyMBProvider 注入参数中心取值口。传 nil 是**空操作**而不是复位：
// 与本仓其余 seam 一致，防止装配顺序出错时把兜底值顶掉。
func SetMaxJSONBodyMBProvider(fn func() int) {
	if fn != nil {
		maxJSONBodyMBProvider = fn
	}
}

// maxJSONBodyMB 返回生效的 MB 数。非正数一律回落 DefaultMaxJSONBodyMB：
// 上限为 0 意味着「所有请求体都不许超过 0 字节」，会把健康检查、探活一并打死。
//
// ⚠️ env 侧的「<=0 表示不限制」是**部署层排障应急开关**，与这里不是一回事，
// 所以种子把 middleware.max_json_body_mb 的 Min 卡在 1，运营侧不给这个口子；
// 真要临时放开请改 env MAX_JSON_BODY_MB=0 并重启。
func maxJSONBodyMB() int {
	if mb := maxJSONBodyMBProvider(); mb > 0 {
		return mb
	}
	return DefaultMaxJSONBodyMB
}

// BodyLimitFromEnv 读取 MAX_JSON_BODY_MB 决定全局上限（字节）。
// 优先级：env > 参数中心 middleware.max_json_body_mb > DefaultMaxJSONBodyMB。
// env 设为 0 或负数仍是**不限制**（排障与迁移期的应急开关），这条既有语义不动。
//
// 用环境变量而非配置文件：与 platform 端同名同语义（`MAX_JSON_BODY_MB`），
// 部署层一次配好两端，不必按仓记两套旋钮。
func BodyLimitFromEnv() int64 {
	raw := strings.TrimSpace(os.Getenv("MAX_JSON_BODY_MB"))
	if raw == "" {
		return int64(maxJSONBodyMB()) * 1024 * 1024
	}
	mb, err := strconv.Atoi(raw)
	if err != nil {
		return int64(maxJSONBodyMB()) * 1024 * 1024
	}
	if mb <= 0 {
		return 0
	}
	return int64(mb) * 1024 * 1024
}

// MaxMultipartMemoryBytes 返回 multipart 内存缓冲上限（字节），供装配处设
// gin.Engine.MaxMultipartMemory —— 超出部分 gin 自动落临时文件，不会吃满内存。
func MaxMultipartMemoryBytes() int64 { return int64(maxMultipartMemoryMB) * 1024 * 1024 }

// BodyLimit 限制 JSON / 表单请求体大小，两条防线：
//
//  1. **ContentLength 预检**：绝大多数客户端带 Content-Length，超限直接 413，
//     handler 一次都不进（这条是真拦截，可给出明确错误码）；
//  2. **MaxBytesReader**：对 chunked / 无 Content-Length 的请求兜底，读到上限即中断。
//
// ⚠️ 已知取舍（如实记录，避免过度承诺）：第 2 条触发时 `ShouldBindJSON` 拿到
// "http: request body too large"，由各 handler 自行返回（通常是 4xx 而非 413）——
// 响应已开始写入，中间件无从改写状态码。但**内存已被限住**，这是本中间件的首要目标。
//
// **跳过 multipart**：本仓的上传通道（素材库 / 知识库文档 / 头像）走 multipart/form-data，
// 一刀切会打断合法大文件上传；其内存占用由 gin 的 MaxMultipartMemory 控制。
func BodyLimit(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if maxBytes <= 0 {
			c.Next()
			return
		}
		if strings.HasPrefix(c.ContentType(), "multipart/") {
			c.Next()
			return
		}
		if c.Request.ContentLength > maxBytes {
			response.Error(c, http.StatusRequestEntityTooLarge, "请求体过大",
				gin.H{"max_bytes": maxBytes})
			c.Abort()
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		c.Next()
	}
}
