// kb_release_wiring.go G-5 知识库变更流程的装配层（新规划任务清单 T-P9-02）。
//
// 职责与 bad_case_wiring.go 同一条：把"有没有 DB 句柄"翻译成一个可注入的服务并登记到全局，
// 让路由（现取全局，决定回 200 还是 503）在同一时刻看到同一份。
//
// 本竖**不再加第三把旗子**。闸门已经存在且是两道（同 model.KBRelease 的文件头）：
//   - `FF_LTC_KB_CHANGE_GATE`（off|shadow|on）：召回 SQL 加不加版本闸门、写侧要不要打版本戳；
//   - `kb_releases.governed`（逐库）：这个库受不受管。
//
// 装配本身则一直做（有 DB 就装）。理由是这两道锁已经把行为封住了：没进发布制的库
// 连一次 Publish 都过不去（仓储那条 ErrKBReleaseNotGoverned），而变更行在 apply 之前
// 一行都不进 knowledge_chunks ⇒ 装了这个服务并不等于线上有任何变化。
// 反过来（旗子 off 就不给装）会弄出一个更坏的形态：影子期想读"这库有几条待发布"
// 却只能拿到 503，而那正是决定要不要转 on 所需的证据。
//
// 与审批运行时的耦合（本文件最需要出声的一处，与 quote_wiring.go 同一条）：裁决入口
// `/api/approvals/:id/decide` 读的是**全局**审批服务，而那份只在
// `FF_LTC_APPROVAL_RESUME` 不为 off 时登记。这里复用全局，没有就地构造一份只用于
// 入队/读结论的 —— 差别必须写进日志，否则运维会把它读成"这条变更没人批"，
// 而事实是"没人能在 HTTP 上批"。
package app

import (
	knowledgesvc "hivemtk-user/internal/aiagent/knowledge/service"
	"hivemtk-user/internal/pkg/kbrelease"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"gorm.io/gorm"
)

// InitKBReleaseRuntime 装配知识库变更底座并登记为全局实例，可重复调用（后写覆盖）。
//
// db == nil 时必须把全局**清空**而不是"什么都不做"：Init 可被重复调用（测试里尤其如此），
// 上一次装配成功的实例若留在全局里，路由就会对着一句"未装配"的告警继续回 200。
func InitKBReleaseRuntime(db *gorm.DB) *service.KBReleaseService {
	if db == nil {
		service.SetGlobalKBReleaseService(nil)
		logger.Warnf("[kb-release] ⚠️ 无 DB 句柄 ⇒ 知识库变更底座不装配：/api/kb-changes/* 与 /api/kb-releases/* 全部回 503；" +
			"读路径的版本闸门不受本句影响（它只看 " + kbrelease.FlagEnv + "），未装配时召回与今天逐字节相同")
		return nil
	}

	approvals, approvalsFromGlobal := approvalSubmitReader(db)
	svc := service.NewKBReleaseService(
		repository.NewKBReleaseRepository(db),
		approvals,
		// 向量补算走 aiagent 那一份 KnowledgeService：per-product 的 embedding 配置
		// （provider_config_json）只有它读得到，另造一份就会把"这个库用的是哪个向量端点"
		// 这件事在两层各记一遍。用 WithDB 形式而不是 NewKnowledgeService()：后者内部取
		// 全局句柄，装配点就说不清"这条链用的是哪把句柄"。
		knowledgesvc.NewKnowledgeServiceWithDB(db),
	)
	service.SetGlobalKBReleaseService(svc)

	mode := kbrelease.ModeForLog()
	switch mode {
	case "off":
		logger.Infof("[kb-release] ✅ 变更底座已装配，但 %s=off ⇒ 召回侧不加版本闸门：已批准的变更仍要先发布才进语料，"+
			"而**导入链路的新内容不受版本管控**（当场可见）。影子期请先置 shadow，真拦须显式写 on", kbrelease.FlagEnv)
	case "shadow":
		logger.Infof("[kb-release] ✅ 变更底座已装配，%s=shadow ⇒ 写侧照常打版本戳、召回结果一条不少，"+
			"只在日志里报\"切到 on 会被隐藏几条\"（转 on 的准入证据）", kbrelease.FlagEnv)
	default:
		logger.Infof("[kb-release] ✅ 变更底座已装配，%s=%s ⇒ 未发布的内容对线上检索不可见（逐库看 governed）", kbrelease.FlagEnv, mode)
	}
	if !approvalsFromGlobal {
		logger.Warnf("[kb-release] ⚠️ 审批运行时未装配（%s=off）⇒ 变更底座自带一个只用于入队/读结论的审批服务："+
			"pending 会正常落库、待办会正常投递，但 /api/approvals/* 回 503，**没人能在 HTTP 上批准这一条**，"+
			"发布永远是\"已批准的变更 0 条\"。要出变更须把该旗子开到 on|shadow 并重启", ApprovalResumeFlagEnv)
	}
	if !svc.Available() {
		// 半装配比不装配更难查：不装配是清一色 503，一眼看得出来；半装配是"路由挂了、
		// 服务在、每条业务请求都失败"，而原因散在三个句柄里。
		logger.Warnf("[kb-release] ❌ 底座已登记但 Available() 为假 ⇒ 端点会回 503，检查上面哪一句装配没出声")
	}
	return svc
}

// StopKBReleaseRuntime 撤掉全局实例（幂等）。
//
// 与 StopApprovalRuntime 同一存在理由：旗子从 on 切回 off 时若只改 env 不清全局，
// /api/kb-changes/* 会继续对着一个已经不该提供服务的实例回 200。本竖没有协程要停，
// 所以这里只做全局清空；调用方为装配的对称入口（测试与灰度重启）。
func StopKBReleaseRuntime() {
	service.SetGlobalKBReleaseService(nil)
}
