package errors

import "errors"

// 业务错误码
const (
	CodeSuccess         = 0
	CodeParamInvalid    = 4000
	CodeUnauthorized    = 4001
	CodeForbidden       = 4003
	CodeNotFound        = 4004
	CodeConflict        = 4009
	CodeInternal        = 5000
	CodePlatformUnavail = 5001
	CodeAssetNotFound   = 6001
	CodeAssetDup        = 6002
	CodeAssetInvalid    = 6003
	CodeSyncFailed      = 6004
	CodeLoaderFallback  = 6005
)

// 功能模块化业务码（.specify/002-modular-subscription）
//
// 为什么单独开一段而不是塞进 6000 段：6000 段是「资产域」（资产包的
// 购买/同步/校验），功能模块是**授权域**。两者的处置方式完全不同 ——
// 资产错误多半是"重试或联系平台"，模块错误是"去市场开通"。
// 混在一段里，前端无法按段做差异化引导。
const (
	// CodeModuleNotEnabled 4301 该功能模块未开通（模块闸拦截）
	//
	// 用 4xxx 段（鉴权/权限）而不是 5xxx：本码的处置是"引导购买"，
	// 与权限不足同属「你的账号当前做不到」这一类。
	CodeModuleNotEnabled = 4301
	// CodeModuleNotFound 4302 模块不存在（目录里查无此 key）
	CodeModuleNotFound = 4302
	// CodeModulePlatformUnavail 4303 平台未连接/不可达，无法办理模块开通
	//
	// 与 CodePlatformUnavail(5001) 分开的原因：后者是"资产同步失败"，
	// 用户能重试；本码是"你离线部署，收费模块开不了"，重试一万次也一样，
	// 引导话术必须不同。
	CodeModulePlatformUnavail = 4303
	// CodeModuleRegistrationIncomplete 4304 开户登记信息不完整（缺公司名/负责人/电话/邮箱）
	CodeModuleRegistrationIncomplete = 4304
	// CodeSubscriptionExpired 4305 订阅已到期（订阅闸拦截，003 修订 #3）
	//
	// 为什么不是 4302：4302 已被 CodeModuleNotFound 占用。本码的处置是
	// 「去续费墙」，与 4301「去模块市场」是两个不同的去处 —— 前端
	// INIT_REDIRECT 互斥节流按码分流，混用会让续费墙和市场页互相打架。
	CodeSubscriptionExpired = 4305
)

// 租户/开户业务码（specs/001-saas-multi-tenant T029/T030 contracts/api.md）
const (
	// CodeTenantParamInvalid 4101 开户参数错误（企业名/用户名/邮箱/密码不合规）
	CodeTenantParamInvalid = 4101
	// CodeTenantUsernameTaken 4102 用户名在租户内重复（跨租户可并存）
	CodeTenantUsernameTaken = 4102
	// CodeTenantEmailTaken 4103 邮箱全局重复（登录通道唯一，跨租户亦不可并存）
	CodeTenantEmailTaken = 4103
	// CodeTenantRegisterClosed 4104 注册未开放（离线 TENANT_MODE=off 恒返回）
	CodeTenantRegisterClosed = 4104
	// CodeTenantRegisterRateLimited 4105 注册限流（同源 IP/指纹频率超限）
	CodeTenantRegisterRateLimited = 4105
	// CodeTenantNotFound 4106 租户不存在
	CodeTenantNotFound = 4106
	// CodeTenantSuspended 4107 租户已停用：登录受限、后台任务跳过（数据保留）
	CodeTenantSuspended = 4107
	// CodeTenantAmbiguousUsername 4108 用户名跨租户歧义，提示改用邮箱登录
	CodeTenantAmbiguousUsername = 4108
)

// BizError 业务错误
type BizError struct {
	Code    int
	Message string
	Cause   error
}

func (e *BizError) Error() string {
	if e.Cause != nil {
		return e.Message + ": " + e.Cause.Error()
	}
	return e.Message
}

func (e *BizError) Unwrap() error { return e.Cause }

func New(code int, msg string) *BizError {
	return &BizError{Code: code, Message: msg}
}

// IsUpstreamFailure 判断一个错误是不是「外部依赖（平台/渠道）调用失败」。
//
// 为什么需要这个判据：上层有一处按**错误文本**判定「记录不存在」
// （见 internal/pkg/errhttp.isNotFoundError：文本里含"不存在"或"not found" ⇒ 404）。
// 而渠道侧的原文常常自己带"not found"——阿里云短信回
// "Specified access key is not found."、腾讯回 "key not found" ——
// 于是「密钥填错了」被翻译成「记录不存在」，把操作者引向完全错误的方向：
// 去查记录，而不是去改密钥。
//
// 判据不能是"文本里有没有 not found"（那正是错的地方），
// 必须是**错误自己在声明**它是上游失败：带平台类业务码的 BizError。
//
// 用法：service 层把渠道错误包成
//
//	bizerr.Wrap(bizerr.CodePlatformUnavail, "发送短信失败（渠道不可达）", err)
//
// 上层据此判"这是上游挂了，不是我们的记录没了"。
//
// 为什么**不**按码段（5xxx 整段）判：5xxx 段里还有 CodeInternal(5000)，
// 那是**内部**故障（写库失败之类），不是上游问题。
// 把内部故障也算成"上游失败"⇒ 内部错误会被报成 502（Bad Gateway），
// 而 502 的语义是"网关/上游给的东西不能用" —— 运维会查错方向：
// 去查渠道，而不是查自己的库。两者必须分开。
// IsClientError 判断一个错误是不是「客户端侧的业务校验失败」（4xxx 段）。
//
// 为什么需要：4xxx 是"你这个账号当前做不到 / 参数不合法 / 状态不允许"这类
// **业务校验**，语义是 400 而不是 500。上层若一律按"没匹配到关键字就 500"处理，
// 这些本该引导用户改正的提示会被报成服务端故障，客户端还会当故障去重试。
//
// 与 IsUpstreamFailure 分工：
//
//	4xxx ⇒ 客户端侧问题（400）
//	5xxx 的 CodePlatformUnavail ⇒ 上游问题（502）
func IsClientError(err error) bool {
	if err == nil {
		return false
	}
	var be *BizError
	if !errors.As(err, &be) {
		return false
	}
	return be.Code >= 4000 && be.Code < 5000
}

func IsUpstreamFailure(err error) bool {
	if err == nil {
		return false
	}
	var be *BizError
	if !errors.As(err, &be) {
		return false
	}
	switch be.Code {
	case CodePlatformUnavail:
		return true
	default:
		return false
	}
}

func Wrap(code int, msg string, cause error) *BizError {
	return &BizError{Code: code, Message: msg, Cause: cause}
}

// LLM 计费业务码（模块化改造 · 任务五）
//
// 为什么单开一段而不塞进 4xxx：4xxx 是"你这个账号当前做不到"（鉴权/权限），
// 而余额不足是"你这个账号**可以**用，但账户没钱" —— 前者引导找管理员，
// 后者引导去充值，混在一起前端无法分流。
const (
	// CodeGeoProbeQuotaExceeded 4290 GEO 探针每日配额超限
	//
	// 判据与 geo/service checkProbeDailyQuota 一一对应：外部探针引擎
	//（perplexity/kimi 等）按次计费，登录用户无配额即可无限触发真实消耗。
	// 每租户每日计数（Redis 计数器，geo.probe_test_daily_limit 可调），
	// 超限返回本码（HTTP 429 语义），Redis 不可用时 fail-open 放行并告警。
	CodeGeoProbeQuotaExceeded = 4290
	// CodeLLMBalanceInsufficient 4401 平台模型余额不足
	//
	// 判据与 dispatcher 侧的 ErrInsufficientLLMBalance 一一对应：
	// 平台代理返回 402 → llm.ErrInsufficientLLMBalance → 本码。
	CodeLLMBalanceInsufficient = 4401
	// CodeLLMPlatformUnavail 4402 平台模型不可用（未连接平台 / provider 未配置）
	CodeLLMPlatformUnavail = 4402
)
