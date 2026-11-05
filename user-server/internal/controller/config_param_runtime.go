package controller

import (
	"net/http"
	"sort"
	"strings"

	"hivemtk-user/internal/pkg/utils/logger"

	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

// runtimePublicGroups 是 `GET /api/config/runtime` 允许读取的**组**白名单；
// runtimePublicKeys 是在组内进一步收窄的**键**级排除清单。
//
// 为什么两级而不是一级：
//  1. 组级白名单挡的是「别把管理端全量列表端给终端用户」——`/api/manage/config-params`
//     每条带 Name/Description/Min/Max/Restart/ReadOnly，暴露它等于把整套运维决策面摊开。
//  2. 键级排除挡的是另一种泄漏：某个组整体上是给前端的，但组里有个别键的后端消费点
//     是 0（如 `pagination.page_max_size`——`ParsePagination` 全树只有测试调用）。
//     这类键留在白名单里，前端会在页面上拿到一个「改了没人读」的旋钮，
//     比不给更糟（运维以为它生效）。**标注和后端事实由
//     `scripts/check-config-param-readpoints.py` 用同一份清单核对**：把键登记进
//     runtimePublicKeys 就是向硬闸声明"这条有前端消费者"。
//
// 新增组/键要在这里显式登记，并写清楚为什么前端需要它。
var runtimePublicGroups = map[string]struct{}{
	// 前端自己实现的 WS 客户端重连/心跳/ACK 批次等旋钮
	// （utils/agentSocket.js、utils/chatSocket.js、utils/journeyTracker.js）。
	"frontend_ws": {},
	// 前端视图里的阈值类常量（轮询间隔、扫描页大小、失败上限等）。
	"frontend_ui": {},
	// 列表分页的默认页大小与单页上限：前端 api/*.js 里各写一份的
	// limit||20 / pageSize||20 应统一读这里。
	"pagination": {},
}

// runtimePublicKeys 是组内排除清单：这些键即使所属组在白名单里也不下发。
//
// 当前两条的后端消费点都是 0（详见 config_param_seeds.go 里它们各自的 Description：
// `ParsePagination`/`ParsePaginationOffset` 全树只有测试调用；`ParseCursorParams`
// 的兜底分支被三个调用方的正数 defaultLimit 遮蔽）。给前端等于给一个改了没人读的
// 旋钮。
var runtimePublicKeys = map[string]struct{}{
	"pagination.page_max_size":     {},
	"pagination.page_default_size": {},
}

// runtimeCrossGroupKeys 是「所属组不在白名单，但单独把这一个键下发给前端」的显式清单。
//
// 为什么要有第三级：把整个 `knowledge` 组放进 runtimePublicGroups 是最省事的做法，
// 但它会把 30 多条只给服务端消费的阈值（熔断阈值、重试次数、embedding 维度……）
// 一起摊给任何一个登录用户，而前端真正需要的只有一个「上传大小上限」。
// 组级白名单挡的就是这种「顺手多给一整组」，所以这里按键逐个放行。
//
// 放行的判据是「前端有一个本地拦截在冒充服务端权威」：
//   - knowledge.max_upload_file_size —— KnowledgeManagement.vue 写死 50MB、
//     后端读的是这条参数。运维把它调到 100MB 后，前端照样把 60MB 的文件拦在浏览器里，
//     页面上没有任何地方说这个上限其实可以改。
//   - knowledge.merchant_batch_max_file_size —— BatchImport.vue 写死 20MB，
//     而接线前后端那条链路根本不判大小（页面那 20MB 是唯一防线，且能绕过）。
//     这条的默认值就是 20MB，所以下发之后前端的读数与接线前一致。
//
// 这里放的键**不受 groups 过滤**（跨组键本来就按全坐标 `group.key` 寻址，
// 让调用方先猜到自己该传哪个组名只会多一种拿不到值的形态）。
var runtimeCrossGroupKeys = map[string]struct{}{
	"knowledge.max_upload_file_size":         {},
	"knowledge.merchant_batch_max_file_size": {},
}

// @Summary      查询运行时配置（按分组）
// @Tags      config-param
// @Router    /api/config/runtime [get]
// Runtime GET /api/config/runtime?groups=frontend_ws,pagination
//
// 只读运行时配置，供前端一次性拉取后缓存。响应只有 value：
//
//	{"code":0,"data":{"frontend_ws.max_reconnect_attempts_default":"10", ...}}
//
// 判据（三条都写死在代码里，不给「可配置」的口子）：
//  1. 只返回白名单内的组；传入的 groups 里不在白名单的**静默忽略**——不报错也不回显，
//     免得调用方拿它当「探测有哪些组」的手段。
//  2. 只返回 value（字符串原样）。Name/Description/Min/Max/Restart 一个都不给。
//  3. 不读数据库时（参数中心不可用）返回空 map + code 0，前端据此用代码兜底值——
//     不能因为运维配置面挂了就让整个 SPA 白屏。
func (c *ConfigParamController) Runtime(ctx *gin.Context) {
	requested := splitGroups(ctx.Query("groups"))
	if len(requested) == 0 {
		// 不传 groups 时返回白名单全部，省得前端每次 hardcode 一份清单
		// （那份清单会和这里漂移，然后前端悄悄少读一组）。
		for g := range runtimePublicGroups {
			requested = append(requested, g)
		}
		sort.Strings(requested)
	}
	allowed := make([]string, 0, len(requested))
	for _, g := range requested {
		if _, ok := runtimePublicGroups[g]; ok {
			allowed = append(allowed, g)
		}
	}

	// c.svc 为 nil（装配顺序出错、或测试直接 NewConfigParamController(nil)）时
	// 返回空配置：Runtime 是前端启动链上的一环，它 panic 会让整个 SPA 打不开。
	out := make(map[string]string, 32)
	if c.svc == nil {
		response.Envelope(ctx, http.StatusOK, gin.H{"code": 0, "data": out, "message": "ok"})
		return
	}
	if len(allowed) > 0 {
		for _, g := range allowed {
			list, err := c.svc.ListByGroup(ctx.Request.Context(), g, false)
			if err != nil {
				// 单组读失败不应让整次请求失败：其余组照常返回，失败的组由前端用
				// 兜底值顶着（这正是「配置面挂了不白屏」的判据 3）。
				logger.Warnf("[ConfigParam] Runtime group=%s err=%v", g, err)
				continue
			}
			for i := range list {
				coord := g + "." + list[i].Key
				if _, excluded := runtimePublicKeys[coord]; excluded {
					continue
				}
				out[coord] = list[i].Value
			}
		}
	}
	for coord, value := range c.crossGroupValues(ctx) {
		out[coord] = value
	}
	response.Envelope(ctx, http.StatusOK, gin.H{"code": 0, "data": out, "message": "ok"})
}

// crossGroupValues 取 runtimeCrossGroupKeys 里那些「组不在白名单、但键单独放行」的值。
//
// 按组聚合后再读：一个组里放行 3 个键也只查一次，免得每加一个跨组键就多一条 SQL。
// 读失败照旧出声不回退——这一层的缺失前端会落回代码兜底值，与判据 3 同一形态。
func (c *ConfigParamController) crossGroupValues(ctx *gin.Context) map[string]string {
	out := make(map[string]string, len(runtimeCrossGroupKeys))
	if c.svc == nil || len(runtimeCrossGroupKeys) == 0 {
		return out
	}
	want := make(map[string]map[string]struct{}, 2)
	for coord := range runtimeCrossGroupKeys {
		g, key, ok := strings.Cut(coord, ".")
		if !ok {
			continue
		}
		if want[g] == nil {
			want[g] = make(map[string]struct{}, 4)
		}
		want[g][key] = struct{}{}
	}
	for g, keys := range want {
		list, err := c.svc.ListByGroup(ctx.Request.Context(), g, false)
		if err != nil {
			logger.Warnf("[ConfigParam] Runtime cross group=%s err=%v", g, err)
			continue
		}
		for i := range list {
			if _, hit := keys[list[i].Key]; !hit {
				continue
			}
			out[g+"."+list[i].Key] = list[i].Value
		}
	}
	return out
}

// splitGroups 解析 groups 查询参数：逗号分隔，去空白，去重，保序去空。
func splitGroups(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	seen := make(map[string]struct{}, 4)
	out := make([]string, 0, 4)
	for _, part := range strings.Split(raw, ",") {
		g := strings.TrimSpace(part)
		if g == "" {
			continue
		}
		if _, dup := seen[g]; dup {
			continue
		}
		seen[g] = struct{}{}
		out = append(out, g)
	}
	return out
}

// AnyRuntimeGroupPublic 供测试断言白名单集合本身（不暴露内部 map）。
func AnyRuntimeGroupPublic(groups ...string) bool {
	for _, g := range groups {
		if _, ok := runtimePublicGroups[g]; !ok {
			return false
		}
	}
	return true
}
