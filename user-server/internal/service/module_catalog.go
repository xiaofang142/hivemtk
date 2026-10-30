package service

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/platform"
	"hivemtk-user/internal/repository"

	"hivemtk-user/internal/config"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
)

// 功能模块闸（模块化改造的核心）
//
// 执行顺序（三条，顺序不可换）：登录态 → 模块闸 → 角色权限。
// 闸必须在角色权限之前：超管是「本系统内权限最高」，不是「买了所有模块」；
// 闸放在权限之后，超管就成了绕过付费模块的后门。
//
// 租户语义：镜像行按 tenant_id 归属，闸是进程级共享的，缓存必须是
// map[租户]状态；一次判定只读、只刷新当前 ctx 解析出的那一个租户。
//
// 三态语义：
//   ① 该租户模块数据为空 → 只放行基础模块（model.BaseModuleKeys），收费模块拦。
//   ② 有数据且命中模块 → 过闸（按行 usable 判定）。
//   ③ 标注缺失（新增接口忘了归模块）→ 放行 + 记 WARN。
//      该态发生在路由层：路径没进模式表时 ModuleOwnerOf 返回 ""，
//      不会带着非空 key 进到 IsEnabled（见 middleware/module_gate.go）。
//      选「放行」而非「拦」：新功能上线时全站 403 是灾难，漏拦最多让某个接口多暴露。

// gateTenantState 单个租户的闸状态快照。
// 约定：加载后整体替换、只读共享 —— 任何方法都不原地改 m。
type gateTenantState struct {
	m            map[string]bool // module_key → usable
	bootstrapped bool            // 该租户镜像是否有过行（len(rows)>0）
	loadedAt     time.Time
}

// ModuleGateService 模块闸（进程级单例，见 NewModuleGateService）
type ModuleGateService struct {
	// repo 显式仓储（测试注入）。nil 表示按次从全局 DB 现取：
	// 单例的寿命比单个测试用例长，构造期定格会把用例结束时被还原/关闭的
	// 连接钉死在单例上。
	repo repository.TenantModuleRepository

	mu      sync.RWMutex
	tenants map[uint]*gateTenantState // 租户 → 状态（进程级共享）
}

// gateCacheTTL 模块状态本地缓存有效期（按租户条目各自计时）。
// 取 60s：模块开通是低频但要尽快生效的操作，一分钟内看到菜单变化可接受，
// 而每次请求都查库会让模块闸变成热点。
const gateCacheTTL = 60 * time.Second

// baseModuleKeySet 基础模块集（免费常开）的判定快照。
// BaseModuleKeys 每次都遍历目录构造，闸的判定是每请求热路径 —— 装配一次。
var baseModuleKeySet = func() map[string]struct{} {
	out := make(map[string]struct{}, 8)
	for _, k := range model.BaseModuleKeys() {
		out[k] = struct{}{}
	}
	return out
}()

// isBaseModuleKey 是否属于基础模块集
func isBaseModuleKey(k string) bool {
	_, ok := baseModuleKeySet[k]
	return ok
}

var (
	processGateOnce sync.Once
	processGate     *ModuleGateService
)

// NewModuleGateService 返回进程单例的模块闸。
//
// 为什么是单例（勿改回「每次 new 一份」）：闸被 middleware（接口闸）、菜单过滤、
// 同步收尾三处共用，缓存与失效必须是同一份，否则 Invalidate 打不中目标。
// 测试请用 NewModuleGateServiceWithRepo（每例全新、缓存隔离）。
func NewModuleGateService() *ModuleGateService {
	processGateOnce.Do(func() {
		processGate = &ModuleGateService{}
	})
	return processGate
}

// NewModuleGateServiceWithRepo 带显式仓储（测试注入：独立实例、独立缓存）
func NewModuleGateServiceWithRepo(repo repository.TenantModuleRepository) *ModuleGateService {
	return &ModuleGateService{repo: repo}
}

// modules 解析实际使用的仓储（显式注入优先，否则按次取全局 DB）
func (s *ModuleGateService) modules() repository.TenantModuleRepository {
	if s.repo != nil {
		return s.repo
	}
	return repository.NewTenantModuleRepository()
}

// gateTenantOf 解析本次判定所属的租户。
//
// 口径：
//   - SystemScope → DefaultTenantID：系统作用域没有「调用方租户」，镜像与
//     同步统一落默认租户，菜单快照（ModuleEnabledFunc）也走这条；
//   - 已绑定（TenantMiddleware 写进请求 ctx 的租户）→ 就是它；
//   - 未绑定（裸 Background、装配早期）→ DefaultTenantID。on 模式下这是
//     「没有调用方身份」时最保守的可读视图 —— 绝不能兜成全量 union，
//     那会把别的租户的开通状态判给当前请求。
func gateTenantOf(ctx context.Context) uint {
	if tenant.IsSystemScope(ctx) {
		return tenant.DefaultTenantID
	}
	if id, ok := tenant.FromContext(ctx); ok {
		return id
	}
	return tenant.DefaultTenantID
}

// gateReadCtx 构造「只读该租户镜像」的读 ctx。
//
// 调用方 ctx 已绑定同一租户时原样透传；其余情形（SystemScope / 无绑定 /
// 绑了别的租户）一律换成钉死 tid 的干净 ctx —— SystemScope 会让
// scopeQueryForTenant 直接放行，读到的是跨租户 union。
func gateReadCtx(ctx context.Context, tid uint) context.Context {
	if !tenant.IsSystemScope(ctx) {
		if id, ok := tenant.FromContext(ctx); ok && id == tid {
			return ctx
		}
	}
	return tenant.OnlyTenant(ctx, tid)
}

// Refresh 从仓储重载当前 ctx 所属租户的状态到缓存。
// 其它租户的条目不受影响；全清用 Invalidate。
func (s *ModuleGateService) Refresh(ctx context.Context) error {
	return s.refreshTenant(ctx, gateTenantOf(ctx))
}

// refreshTenant 拉取 tid 的镜像行并整体替换其缓存条目
func (s *ModuleGateService) refreshTenant(ctx context.Context, tid uint) error {
	rows, err := s.modules().ListAll(gateReadCtx(ctx, tid))
	if err != nil {
		return err
	}
	now := time.Now()
	m := make(map[string]bool, len(rows))
	for _, r := range rows {
		m[r.ModuleKey] = model.TenantModuleUsable(r, now)
	}
	st := &gateTenantState{m: m, bootstrapped: len(rows) > 0, loadedAt: now}
	s.mu.Lock()
	if s.tenants == nil {
		s.tenants = make(map[uint]*gateTenantState, 1)
	}
	s.tenants[tid] = st
	s.mu.Unlock()
	return nil
}

// Invalidate 清掉全部租户的缓存条目（同步/激活收尾、下单后调用）。
// 只清单租户是不够的：激活的落点是下单租户，可能不是默认租户。
func (s *ModuleGateService) Invalidate() {
	s.mu.Lock()
	s.tenants = nil
	s.mu.Unlock()
}

// loadState 取当前 ctx 所属租户的状态快照；缺失或超过 gateCacheTTL 则刷新。
// 刷新失败沿用上一次快照（判定不因一次 DB 抖动翻转）；从未成功加载过则按
// 空租户态返回（①：基础模块放行、收费拦 —— fail-closed）。
func (s *ModuleGateService) loadState(ctx context.Context) gateTenantState {
	tid := gateTenantOf(ctx)
	s.mu.RLock()
	st := s.tenants[tid]
	s.mu.RUnlock()
	if st != nil && time.Since(st.loadedAt) < gateCacheTTL {
		return *st
	}
	if err := s.refreshTenant(ctx, tid); err != nil {
		logger.Warnf("[module-gate] 刷新租户 %d 模块状态失败，按上一次缓存判定: %v", tid, err)
	}
	s.mu.RLock()
	st = s.tenants[tid]
	s.mu.RUnlock()
	if st == nil {
		return gateTenantState{}
	}
	return *st
}

// IsEnabled 模块是否已开通。
//
// 唯一的判定入口：菜单过滤、接口闸、cron/工具装配全部走它，
// 避免「菜单过滤用了 A 口径、接口闸用了 B 口径」的不一致。
func (s *ModuleGateService) IsEnabled(ctx context.Context, moduleKey string) bool {
	if moduleKey == "" {
		return true // 未标注的路由：放行（见文件头 ③）
	}
	st := s.loadState(ctx)
	if !st.bootstrapped {
		// ① 该租户镜像为空（首启 / 从未同步 / on 模式 2+ 租户的空镜像）：
		//    只放行基础模块，其余一律拦。与 EnabledKeys 空态同口径。
		//
		// on 模式空镜像租户拿到可用的基础系统（base-pack），收费模块 4301
		//    → 市场 → 下单，收款确认后激活的是自己租户的行（见 purchaseTenants）。
		// 全新安装：首轮同步在 router.Setup 之前执行，同步前的空窗里基础
		//    能力可用、收费能力锁住，与 bootstrapOffline 灌「基础模块=免费常开」
		//    是同一条产品线。
		// off 模式：离线没有付费概念，bootstrapOffline 落地的是 base（全免费）。
		//
		// 附核对：platform-link 属基础模块（ModuleCategoryBase），/api/modules/my
		//    与市场购买链路在空态下依然可用；市场列表/下单本就不挂闸。
		//    故不需要 gateAlwaysAllowed 之类的旁路集合 —— 空态允许集
		//    就是 BaseModuleKeys。
		return isBaseModuleKey(moduleKey)
	}
	v, ok := st.m[moduleKey]
	if !ok {
		// ② 该模块在本地镜像里没有记录 —— 平台还没下发它（或已被撤销）
		return false
	}
	return v
}

// EnabledKeys 已开通模块 key 列表（菜单过滤用）
func (s *ModuleGateService) EnabledKeys(ctx context.Context) map[string]struct{} {
	st := s.loadState(ctx)
	out := make(map[string]struct{}, len(st.m)+len(baseModuleKeySet))
	if !st.bootstrapped {
		// 空镜像：与 IsEnabled ① 完全同口径 —— 只有基础模块
		for k := range baseModuleKeySet {
			out[k] = struct{}{}
		}
		return out
	}
	for k, v := range st.m {
		if v {
			out[k] = struct{}{}
		}
	}
	return out
}

// ModuleEnabledFunc 返回可注入 model.FilterMenuTreeByModule 的判定函数
func (s *ModuleGateService) ModuleEnabledFunc() func(string) bool {
	// 菜单过滤是进程级单快照（全局单口径，不属于任何请求），装配/同步时
	// 取一次就固化进闭包。SystemScope 显式表达「系统快照」意图：读按
	// gateTenantOf 解析为 DefaultTenantID 视图。off 模式直通，行为不变。
	//
	// on 模式下菜单按默认租户的开通集渲染是有意为之（进程级共享）：按
	// 租户分裂菜单要同时动会话/缓存/权限三处的假设，不在本闸范围；接口闸
	// （IsEnabled）才是逐请求按租户判的 —— 菜单与接口短暂不一致时以接口
	// 4301 为准，购买入口（市场/我的模块）不受菜单裁剪影响。
	keys := s.EnabledKeys(tenant.WithSystemScope(context.Background()))
	return func(moduleKey string) bool {
		if moduleKey == "" {
			return true
		}
		_, ok := keys[moduleKey]
		return ok
	}
}

// InstallWiring 把模块闸接到菜单过滤上（装配期调用一次）
func (s *ModuleGateService) InstallWiring(ctx context.Context) {
	model.SetModuleEnabledFilter(s.ModuleEnabledFunc())
}

// refreshProcessGate 同步/离线引导落地后让闸与菜单立刻生效。
//
// Invalidate 是全租户清：本轮可能激活了下单租户的行（落点=订单归属租户），
// 只刷默认租户会把激活租户的陈旧条目留到 TTL 过期 —— 用户下单成功却要等
// 60s 才开通。之后把默认租户重灌一遍，菜单快照直接拿这份新状态。
func refreshProcessGate(ctx context.Context) {
	g := NewModuleGateService()
	g.Invalidate()
	if err := g.Refresh(ctx); err != nil {
		logger.Warnf("[module-gate] 同步后刷新默认租户状态失败（其余租户靠 TTL 内自愈）: %v", err)
	}
	model.SetModuleEnabledFilter(g.ModuleEnabledFunc())
}

// ---------------------------------------------------------- 同步

// currentTenantCode 解析当前上下文租户的短码（平台交互用：sync query /
// 订阅订单 body）。口径：
//   - ctx 无租户绑定（后台 runner、裸 Background）→ 空串；
//   - off 模式（TenantMiddleware 绑默认租户但 tenants 表无行 / on 模式查不到
//     该 id 的行）→ 空串；
//   - 查库失败 → 空串（短码是给平台的归属提示，取不到就走平台旧行为兜底，
//     绝不允许把同步/下单整条链路打挂）。
//
// tenants 表是全局表（无 tenant_id 列、不进隔离登记表），所以挂 SystemScope
// 的 sync ctx 也能直查，不受隔离 callback fail-closed 影响。
func currentTenantCode(ctx context.Context) string {
	id, ok := tenant.FromContext(ctx)
	if !ok {
		return ""
	}
	t, err := repository.NewTenantRepository().GetByID(tenant.WithSystemScope(ctx), id)
	if err != nil || t == nil {
		return ""
	}
	return t.Code
}

// ModuleCatalogService 模块下发与市场服务
type ModuleCatalogService struct {
	repo  repository.TenantModuleRepository
	order repository.ModuleOrderRepository
}

// NewModuleCatalogService 构造
func NewModuleCatalogService() *ModuleCatalogService {
	return &ModuleCatalogService{
		repo:  repository.NewTenantModuleRepository(),
		order: repository.NewModuleOrderRepository(),
	}
}

// NewModuleCatalogServiceWithRepos 带显式仓储（测试注入）
func NewModuleCatalogServiceWithRepos(repo repository.TenantModuleRepository, orderRepo repository.ModuleOrderRepository) *ModuleCatalogService {
	return &ModuleCatalogService{repo: repo, order: orderRepo}
}

// ModuleSyncResult 同步结果
type ModuleSyncResult struct {
	ActiveCount int    `json:"active_count"`
	Source      string `json:"source"` // platform | offline_bootstrap | cache
	Degraded    bool   `json:"degraded"`
	Message     string `json:"message"`
}

// Sync 从平台拉取模块快照并落本地镜像。
//
// 离线降级（PLATFORM_ENABLED=false）：
//   - 本地没有任何模块行 → 灌入赠送集（基础模块 + 全部 0 元付费模块，
//     browser-automation 除外，见 offlineGiftModuleRows）
//   - 本地已有数据 → 幂等补齐缺失的赠送行（存量部署升级路径），已有行原样
//     保留（不降级到"全开"，否则用户会被静默收回已购模块）
func (s *ModuleCatalogService) Sync(ctx context.Context) (*ModuleSyncResult, error) {
	start := time.Now()

	// 请求租户必须在挂 SystemScope **之前**取：那一层会把 ctx 的写权限放大到
	// 全租户，但仍保留租户绑定（WithSystemScope 不剥 value），这里取的是
	// 「谁发起了本轮同步」。手动同步按钮与开户后的异步补铺都带着它；后台周期
	// runner 没有绑定（ok=false），按单商户旧行为处理。
	reqTenantID, reqTenantBound := tenant.FromContext(ctx)

	// 同步/离线引导跑在请求之外（后台 runner、启动期、手动同步按钮），ctx 可能
	// 无租户绑定 —— on 模式直连会被隔离 callback fail-closed，Sync 整体报错、
	// 镜像永不落地（已购模块永远激活不了，闸停在空态）。挂 SystemScope：平台侧
	// 是单商户单部署，目录镜像与同步日志统一落默认租户（tenant_id 由列默认值/
	// GORM default 填 1）。**唯一例外是收费模块的落点** —— 有本地下单记录的按
	// 下单租户分流（见落点归并/purchaseTenants），否则租户 2 买的东西会激活到
	// 租户 1。off 模式 SystemScope 是直通，SQL 形状逐字节不变。
	ctx = tenant.WithSystemScope(ctx)

	if !config.PlatformEnabled() {
		return s.bootstrapOffline(ctx, start)
	}
	// 请求上带当前租户 code：平台端订阅按 (merchant_key, tenant_code) 每租户
	// 一段下发。取不到（后台 runner 无租户绑定 / off 模式无租户行 / 查库失败）
	// 传空串 —— 平台对空值走旧行为兜底，这里绝不让同步失败。
	snap, err := platform.NewModuleClient().SyncModules(ctx, currentTenantCode(ctx))
	if err != nil {
		// 平台不可达：保留上次镜像（不清空），只记 degraded 日志。
		// 清空会让用户正在用的模块突然消失 —— 那比「数据不准」严重得多。
		existing, listErr := s.repo.ListAll(ctx)
		if listErr == nil && len(existing) > 0 {
			_ = s.repo.CreateLog(ctx, &model.ModuleSyncLog{
				Action: "sync", Status: "degraded", Source: "platform",
				ActiveCount: len(existing), ErrorMsg: err.Error(),
				DurationMs: time.Since(start).Milliseconds(),
			})
			return &ModuleSyncResult{
				ActiveCount: len(existing), Source: "cache", Degraded: true,
				Message: "平台暂不可达，沿用上次同步的模块状态",
			}, nil
		}
		return s.bootstrapOffline(ctx, start)
	}

	// 订单状态同步（与模块同一个周期）。**必须先于镜像落库**：下面的落点
	// 归并要用「已收款」的最新状态决定付费模块落到哪个租户 —— 顺序反了，
	// 平台本轮刚确认的那单会按旧状态（pending）错过激活。
	//
	// module_orders 只在**下单时**写一次，之后不刷新 —— 平台确认收款后，
	// 用户端订单页会**永远显示「待确认收款」**，而「我的模块」页却已经显示
	// 该模块已开通。两页自相矛盾，用户会以为「付款了但没生效」而重复下单。
	//
	// 只刷状态、不覆盖 items/payment_guide：那两样是下单时的快照，不可变；
	// 而状态是唯一会变的东西。这也避免了为刷新去扩展平台端的列表响应。
	if n, oerr := s.syncOrderStatuses(ctx); oerr != nil {
		logger.Warnf("[module-sync] 订单状态同步失败（不影响模块下发）: %v", oerr)
	} else if n > 0 {
		logger.Infof("[module-sync] 已刷新 %d 条订单状态", n)
	}

	// ---- 订阅镜像 ----
	// 部署级单行，sync 在 SystemScope 写（镜像表无 tenant_id，不受租户串写影响）。
	// 平台段缺失（老平台/读失败）不写：保留最后已知态（fail-open 的数据来源）。
	if snap.Subscription != nil {
		if err := NewSubscriptionMirrorService().SaveFromSnapshot(ctx, snap.Subscription); err != nil {
			logger.Warnf("[module-sync] 订阅镜像写入失败（不影响模块下发）: %v", err)
		}
	}

	rows := make([]*model.TenantModule, 0, len(snap.ActiveKeys))
	now := time.Now()
	for _, r := range snap.Catalog {
		if !listHas(snap.ActiveKeys, r.ModuleKey) {
			continue
		}
		menuKeys, _ := json.Marshal(r.MenuKeys)
		rows = append(rows, &model.TenantModule{
			ModuleKey:   r.ModuleKey,
			Status:      model.TenantModuleActive,
			Source:      sourceOf(r.OwnedSource, r.Category, r.ModuleKey),
			Category:    r.Category,
			PriceCents:  r.PriceCents,
			MenuKeys:    string(menuKeys),
			ActivatedAt: now,
		})
	}
	// ---- 落点归并：激活必须绑**下单租户** ----
	//
	// 收费模块的激活信号以平台下发目录为准（平台 MarkPaid 后 owned 随
	// /modules/sync 下发），但镜像行必须落在**下单的那个租户**上：平台侧
	// 只有 merchant 视角，订单在用户端带着 tenant_id。若把目录行统一塞进
	// SystemScope 写（列默认值 1），租户 2 买的东西会激活到租户 1 —— 租户 2
	// 永远打不开、租户 1 反而白得。对不上本地订单的行（平台种子、迁移前的
	// 历史单）落默认租户；off 模式订单全部归默认租户。
	existing, listErr := s.repo.ListAll(ctx)

	targets := s.purchaseTenants(ctx)
	groups := make(map[uint][]*model.TenantModule, 8)
	for _, row := range rows {
		tid := tenant.DefaultTenantID
		// 基础模块行与 0 元模块行**永远**落默认租户：前者是免费常开的底线，
		// 后者自 2026-10-30 起是「需开通但不收费」——平台对 0 元模块自动
		// owned=true，所以它对每个租户都成立，不能再按下单租户分流。分流一旦
		// 生效，默认租户会保留一条旧的 disabled 行（线上事故：reach-social
		// 被分到别的下单租户，tenant 1 仍是 disabled，五个平台菜单全空）。
		//
		// 收费模块（PriceCents>0）仍按订单落点：有本轮租户绑定就落它自己，
		// 商户级那轮才从订单反查（历史原因见下方注释）。
		if !isBaseModuleKey(row.ModuleKey) && row.PriceCents > 0 {
			switch {
			case reqTenantBound:
				// 本轮同步有租户绑定 ⇒ 落点就是**本轮这个租户**。
				//
				// 为什么不再从订单反查：平台端 active_keys
				// 早已按租户分开下发（每个租户拿到的名单只含它自己开通的模块），
				// 落点本该直接是它自己。而 purchaseTenants 的判据是「首见即最新」
				// —— 跨租户订单里**最新那一笔**的租户赢了：m1 与 m2 都买过
				// reach-auto，m2 的订单更新，于是 m1 那轮 sync 把 reach-auto
				// 写到了 **m2 名下**，m1 自己的行停在 disabled（市场页显示未拥有），
				// m2 白得一个模块。
				//
				// 订单反查只在**商户级那轮**（无租户绑定、平台下发的是商户级名单）
				// 才有意义：那时名单是整个商户的，落到哪个租户需要看订单。
				tid = reqTenantID
			default:
				if t, ok := targets[row.ModuleKey]; ok {
					tid = t
				}
			}
		}
		groups[tid] = append(groups[tid], row)
	}
	// 每个已存在的租户、本轮发起租户、默认租户，都必须拿到基础模块行与
	// 全部 0 元模块行：boot=true 之后判定按行走，缺 base 行会把 core/customer
	// 等基础能力一并判关；0 元模块缺行则会被 module gate 判成未开通，菜单
	// 只剩骨架（新注册商户、存量商户都踩过）。
	//
	// 收费模块（PriceCents>0）不在此列：它们只对下单租户可见，落点由上面的
	// 订单归并决定；对没下单的租户复制就是白送。
	tenantIDs := map[uint]bool{tenant.DefaultTenantID: true}
	for _, e := range existing {
		if e != nil && e.TenantID != 0 {
			tenantIDs[e.TenantID] = true
		}
	}
	for tid := range groups {
		tenantIDs[tid] = true
	}
	if reqTenantBound && reqTenantID != 0 {
		tenantIDs[reqTenantID] = true
	}
	freeRows := make([]*model.TenantModule, 0, len(rows))
	for _, r := range rows {
		if r.PriceCents == 0 && !isBaseModuleKey(r.ModuleKey) {
			freeRows = append(freeRows, r)
		}
	}
	for tid := range tenantIDs {
		g := groups[tid]
		have := make(map[string]bool, len(g)+len(model.BaseModuleKeys())+len(freeRows))
		for _, r := range g {
			have[r.ModuleKey] = true
		}
		for _, r := range baseTenantModuleRows(now) {
			if have[r.ModuleKey] {
				continue
			}
			g = append(g, r)
			have[r.ModuleKey] = true
		}
		for _, r := range freeRows {
			if have[r.ModuleKey] {
				continue
			}
			// 每个租户各拿一份独立行：隔离 callback 会把 TenantID 写在行上，
			// 复用同一指针会被跨租户校验拒绝（或把后一个租户写成前一个）。
			clone := *r
			clone.TenantID = 0
			clone.Source = model.TenantModuleSourceGift
			clone.ActivatedAt = now
			g = append(g, &clone)
			have[r.ModuleKey] = true
		}
		groups[tid] = g
	}
	tids := make([]uint, 0, len(groups))
	for tid := range groups {
		tids = append(tids, tid)
	}
	sort.Slice(tids, func(i, j int) bool { return tids[i] < tids[j] })
	for _, tid := range tids {
		uctx := ctx
		if tid != tenant.DefaultTenantID {
			// ctx 挂着 SystemScope：直接用会绕过隔离填充（行落列默认值 1），
			// 慢路径先查后写也会变跨租户 —— 换成只带该租户的干净 ctx。
			uctx = tenant.OnlyTenant(ctx, tid)
		}
		if err := s.repo.UpsertMany(uctx, groups[tid]); err != nil {
			_ = s.repo.CreateLog(ctx, &model.ModuleSyncLog{
				Action: "sync", Status: "failed", Source: "platform", ErrorMsg: err.Error(),
				DurationMs: time.Since(start).Milliseconds(),
			})
			return nil, err
		}
	}

	// 本地存在但平台已不再下发的模块要显式失效，否则撤销永远不生效
	//
	// 作用域纪律：本轮的 active_keys 是**某一个租户**
	// 的名单 —— 有租户绑定时是它自己的，没绑定时是**商户级**名单（平台按
	// merchant_key 的兜底行下发）。商户级名单不代表任何租户，拿它去遍历
	// 全租户的行会把**别的租户已开通的付费模块**误标成 disabled
	// （验证：m1 买过 reach-auto 且平台端 active，用户端却显示未拥有）。
	//
	// 所以 stale 判定只在**本轮有该租户专属名单**时做，且只看该租户的行：
	//   - 有租户绑定 ⇒ 判该租户的行（撤销语义正确）；
	//   - 无租户绑定（商户级那轮）⇒ 不判任何租户行（无权判定别人存废）。
	if listErr == nil && reqTenantBound {
		active := map[string]bool{}
		for _, k := range snap.ActiveKeys {
			active[k] = true
		}
		stale := make([]*model.TenantModule, 0)
		for _, e := range existing {
			if e.TenantID != reqTenantID {
				continue // 别的租户的行：商户级/他租户名单无权判定
			}
			if !active[e.ModuleKey] && e.Status == model.TenantModuleActive {
				// gift 行豁免：平台对 0 元模块的 owned 下发口径尚未两端定死 ——
				// 平台没把某 0 元模块算进 active_keys 时（例如存量商户升级前、
				// 离线期落的 gift 行），这里的 stale 判定会把免费能力整批误关。
				// gift 来源不参与 stale：撤销免费模块请走平台 owned/owned_status
				// 口径下发，不要依赖「不在下发名单里」。
				if e.Source == model.TenantModuleSourceGift {
					continue
				}
				c := *e
				c.Status = model.TenantModuleDisabled
				stale = append(stale, &c)
			}
		}
		if len(stale) > 0 {
			// 只带该租户的干净 ctx：SystemScope 下的写会把行落默认租户。
			uctx := tenant.OnlyTenant(ctx, reqTenantID)
			if err := s.repo.UpsertMany(uctx, stale); err != nil {
				logger.Warnf("[module] 失效陈旧模块失败: %v", err)
			}
		}
	}
	_ = s.repo.CreateLog(ctx, &model.ModuleSyncLog{
		Action: "sync", Status: "success", Source: "platform",
		ActiveCount: len(snap.ActiveKeys), DurationMs: time.Since(start).Milliseconds(),
	})
	// 同步后必须让闸与菜单立刻生效，否则激活租户要等 60s 缓存过期
	refreshProcessGate(ctx)
	return &ModuleSyncResult{ActiveCount: len(snap.ActiveKeys), Source: "platform"}, nil
}

// purchaseTenants 从本地订单镜像推导「模块 key → 下单租户」。
//
// 激活的落点判据：平台只有 merchant 视角（哪个模块 owned），**谁买的是
// 用户端订单行里的 tenant_id**（下单时由请求租户经 fillCreateForTenant
// 写入并校验）。取「最近一笔已收款/已履约的模块订单」——订单镜像按
// created_at DESC 分页，首见即最新（后翻页只补尚未覆盖的 key）。
//
// 读挂 SystemScope（Sync 的 ctx）→ 跨租户全量，这正是这里需要的：要给
// **任意租户**的订单找落点。读失败按「无订单」处理 → 全部落默认租户，
// 即旧行为（宁可退化到已知行为，也不能让同步整体失败）。
func (s *ModuleCatalogService) purchaseTenants(ctx context.Context) map[string]uint {
	out := make(map[string]uint)
	page := 1
	for {
		orders, total, err := s.order.List(ctx, page, 100)
		if err != nil {
			logger.Warnf("[module-sync] 读取订单镜像失败，本轮按默认租户落镜像: %v", err)
			return out
		}
		for _, o := range orders {
			if o == nil || o.OrderType != "module" {
				continue
			}
			if o.Status != model.ModuleOrderPaid && o.Status != model.ModuleOrderFulfilled {
				continue
			}
			tid := o.TenantID
			if tid == 0 {
				// 迁移前落库的存量行没有 tenant_id 值：归默认租户（存量数据
				// 升级口径同此），绝不放行 tid=0（那会构造出无绑定 ctx →
				// on 模式 fail-closed 让整轮同步失败）。
				tid = tenant.DefaultTenantID
			}
			for _, key := range orderModuleKeys(o) {
				if _, seen := out[key]; !seen {
					out[key] = tid
				}
			}
		}
		if int64(page*100) >= total {
			break
		}
		page++
	}
	return out
}

// orderModuleKeys 订单行快照里的模块 key。
//
// 只收「类型是模块且在 24 模块目录内」的行：选项行/LLM 行的 item_key 不是
// 模块 key，误收会凭空造出不存在的开通记录（闸会因此放行没买过的模块）。
func orderModuleKeys(o *model.ModuleOrder) []string {
	if o == nil || o.Items == "" {
		return nil
	}
	var items []platform.ModuleOrderItem
	if err := json.Unmarshal([]byte(o.Items), &items); err != nil {
		return nil
	}
	var out []string
	for _, it := range items {
		if it.ItemType != "" && it.ItemType != "module" {
			continue
		}
		if model.OwnerModuleCategory(it.ItemKey) == "" {
			continue // 不在目录里 = 不是模块 key
		}
		out = append(out, it.ItemKey)
	}
	return out
}

// baseTenantModuleRows 基础模块行（免费常开）。
// 「给非默认租户首次落行」用 —— 保证「有行的租户必然有基础行」，
// 与空态放行 BaseModuleKeys 是同一批 key。
//
// **不要**把离线赠送逻辑合并进来：本函数同时被 on 模式的落点归并分支使用，
// 塞进 0 元付费模块会让云端租户凭空拿到全部免费模块、随同步开关震荡。
// 离线口径见 offlineGiftModuleRows。
func baseTenantModuleRows(now time.Time) []*model.TenantModule {
	rows := make([]*model.TenantModule, 0, 8)
	for _, k := range model.BaseModuleKeys() {
		menuKeys, _ := json.Marshal(model.ModuleMenuBindings[k])
		rows = append(rows, &model.TenantModule{
			ModuleKey:   k,
			Status:      model.TenantModuleActive,
			Source:      model.TenantModuleSourceDefaultBase,
			Category:    model.ModuleCategoryBase,
			MenuKeys:    string(menuKeys),
			ActivatedAt: now,
		})
	}
	return rows
}

// offlineGiftModuleRows 离线赠送行：基础模块 + 全部 0 元付费模块
// （browser-automation 除外——¥1 买断，本地无支付通道 → 保持不可购不可用）。
//
// 与 baseTenantModuleRows 的分工见后者注释：本函数**只**服务 bootstrapOffline。
// 0 元付费模块必须显式落行（source=gift），否则闸的空态/有行判定只认
// BaseModuleKeys，菜单 16 组不会出现。
func offlineGiftModuleRows(now time.Time) []*model.TenantModule {
	rows := baseTenantModuleRows(now)
	for _, m := range model.ModuleCatalogMeta {
		if m.Category != model.ModuleCategoryPaid || m.Key == model.ModuleBrowserAutomation {
			continue
		}
		if m.PriceCents != 0 {
			continue // 目录将来恢复收费的模块自动退出赠送集（改价即可）
		}
		menuKeys, _ := json.Marshal(model.ModuleMenuBindings[m.Key])
		rows = append(rows, &model.TenantModule{
			ModuleKey:   m.Key,
			Status:      model.TenantModuleActive,
			Source:      model.TenantModuleSourceGift,
			Category:    m.Category,
			PriceCents:  m.PriceCents,
			MenuKeys:    string(menuKeys),
			ActivatedAt: now,
		})
	}
	return rows
}

// bootstrapOffline 离线兜底：基础模块 + 0 元赠送模块全开（browser-automation 锁定）
func (s *ModuleCatalogService) bootstrapOffline(ctx context.Context, start time.Time) (*ModuleSyncResult, error) {
	existing, err := s.repo.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	if len(existing) > 0 {
		// 存量部署升级路径：镜像非空 ≠ 赠送集已齐 —— 升级后必须**幂等补齐**
		// 缺失的 0 元赠送行，否则新增的 16 组菜单永远不会出现；同时刷新闸与
		// 菜单快照，否则要等 60s 缓存 TTL 或下一次同步才生效。
		// 已有行一律不动（含已购/已禁用行：不降级、不复活）。
		now := time.Now()
		have := make(map[string]struct{}, len(existing))
		for _, e := range existing {
			have[e.ModuleKey] = struct{}{}
		}
		missing := make([]*model.TenantModule, 0)
		for _, r := range offlineGiftModuleRows(now) {
			if _, ok := have[r.ModuleKey]; !ok {
				missing = append(missing, r)
			}
		}
		if len(missing) > 0 {
			if err := s.repo.UpsertMany(ctx, missing); err != nil {
				return nil, err
			}
		}
		total := len(existing) + len(missing)
		_ = s.repo.CreateLog(ctx, &model.ModuleSyncLog{
			Action: "sync", Status: "success", Source: "cache",
			ActiveCount: total, DurationMs: time.Since(start).Milliseconds(),
		})
		refreshProcessGate(ctx)
		return &ModuleSyncResult{ActiveCount: total, Source: "cache"}, nil
	}
	rows := offlineGiftModuleRows(time.Now())
	if err := s.repo.UpsertMany(ctx, rows); err != nil {
		return nil, err
	}
	_ = s.repo.CreateLog(ctx, &model.ModuleSyncLog{
		Action: "offline_bootstrap", Status: "success", Source: "offline_bootstrap",
		ActiveCount: len(rows), DurationMs: time.Since(start).Milliseconds(),
	})
	refreshProcessGate(ctx)
	return &ModuleSyncResult{
		ActiveCount: len(rows), Source: "offline_bootstrap",
		Message: "未连接平台，已按目录开通全部免费能力（浏览器自动化除外）",
	}, nil
}

// ProvisionTenantModules 开户时给新租户铺模块镜像（幂等，失败不阻断开户）。
//
// 为什么开户就要铺、不能等用户点「同步」：模块闸对空镜像只放行基础包，
// 新商户登录后除基础外的每一页都回 4301、「初始化只有骨架」。基础行在本地
// 直接写（不依赖平台可达，线上事故里平台一直是好的，缺的是本地这步）；
// 0 元赠送集与订阅镜像需要平台快照，放到后台异步补 —— 开户请求不能等一次
// 外部 HMAC 往返，失败也只是晚 10 分钟（周期同步会走同一条 Sync）。
func ProvisionTenantModules(ctx context.Context, tenantID uint) {
	if tenantID == 0 || !tenant.IsMultiTenant() {
		return
	}
	tctx := tenant.OnlyTenant(ctx, tenantID)
	if err := repository.NewTenantModuleRepository().
		UpsertMany(tctx, baseTenantModuleRows(time.Now())); err != nil {
		logger.Warnf("[tenant-provision] tenant=%d 基础模块行写入失败（不阻断开户）: %v", tenantID, err)
	}
	// 后台任务只认租户归属：不继承请求 ctx 的取消（开户响应返回后仍要跑完），
	// 也不带 SystemScope（否则同步的落点归并会把行写回默认租户）。
	bg := tenant.OnlyTenant(context.Background(), tenantID)
	utils.SafeGoDetached(bg, "module.tenant_provision", 30*time.Second, func(c context.Context) {
		if _, err := NewModuleCatalogService().Sync(c); err != nil {
			logger.Warnf("[tenant-provision] tenant=%d 模块同步失败（周期同步会补）: %v", tenantID, err)
		}
	})
}

// sourceOf 决定本地镜像记录的开通来源。
//
// 判据按平台下发的 OwnedSource 直取；缺省时按模块分类回落：
// 基础模块 → default_base，收费模块 → purchase（保守：不要凭空说用户买了）。
func sourceOf(ownedSource, category, moduleKey string) string {
	if ownedSource != "" {
		return ownedSource
	}
	if category == model.ModuleCategoryBase {
		return model.TenantModuleSourceDefaultBase
	}
	if model.OwnerModuleCategory(moduleKey) == model.ModuleCategoryBase {
		return model.TenantModuleSourceDefaultBase
	}
	return model.TenantModuleSourcePurchase
}

func listHas(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// MyModuleView 「我的模块」视图项
type MyModuleView struct {
	ModuleKey  string `json:"module_key"`
	Name       string `json:"name"`
	Category   string `json:"category"`
	PriceCents int64  `json:"price_cents"`
	// BillingCycle 计费周期（once=买断/monthly/yearly/metered）。
	// 缺它时 priceLabel 走不到「买断」分支，¥1 买断的浏览器自动化会被标成「¥1 / 年」。
	BillingCycle string     `json:"billing_cycle"`
	Icon         string     `json:"icon"`
	Summary      string     `json:"summary"`
	DependsOn    []string   `json:"depends_on"`
	MenuKeys     []string   `json:"menu_keys"`
	Owned        bool       `json:"owned"`
	Source       string     `json:"source"`
	Status       string     `json:"status"`
	ExpiresAt    *time.Time `json:"expires_at"`
}

// MyModules 返回「我的模块」：已开通 + 未开通（全部目录）
//
// 一次返回两侧，前端「我的模块」页既能显示已购也能引导未购，不必两次请求。
func (s *ModuleCatalogService) MyModules(ctx context.Context) ([]MyModuleView, error) {
	rows, err := s.repo.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	owned := map[string]*model.TenantModule{}
	for _, r := range rows {
		owned[r.ModuleKey] = r
	}

	// 目录来自编译期注册的 ModuleCatalogMeta（离线也拿得到）
	out := make([]MyModuleView, 0, len(model.ModuleCatalogMeta))
	for _, meta := range model.ModuleCatalogMeta {
		mk := meta.Key
		v := MyModuleView{
			ModuleKey:    mk,
			Name:         meta.Name,
			Category:     meta.Category,
			PriceCents:   meta.PriceCents,
			BillingCycle: meta.BillingCycle,
			Icon:         meta.Icon,
			Summary:      meta.Summary,
			DependsOn:    meta.DependsOn,
			MenuKeys:     model.ModuleMenuBindings[mk],
			Owned:        false,
		}
		if tm, ok := owned[mk]; ok {
			v.Owned = model.TenantModuleUsable(tm, now)
			v.Source = tm.Source
			v.Status = tm.Status
			v.ExpiresAt = tm.ExpiresAt
			if v.PriceCents == 0 {
				v.PriceCents = tm.PriceCents
			}
			// 菜单键**优先用编译期绑定**，本地镜像列只作兜底。
			//
			// 镜像里的 menu_keys 是平台下发回来的，而平台那份又是由本仓
			// cmd/gen-module-bindings 从 ModuleMenuBindings 导出的 ——
			// 也就是「把编译期常量绕一圈发回来」。用它当权威有两个坏处：
			//   ① 离线且从未同步成功时它是空的，而编译期绑定始终可用；
			//   ② 两端版本不一致时（用户端升级了、平台还没重新 seed），
			//      它会显示过期清单，而本地那份是对的。
			// 只有 llm-cloud 这类**没有菜单页**的模块，编译期绑定本就为空，
			// 此时才需要镜像兜底。
			if len(v.MenuKeys) == 0 {
				v.MenuKeys = repository.MenuKeysOf(tm)
			}
		}
		out = append(out, v)
	}
	return out, nil
}

// SyncLogs 同步日志
func (s *ModuleCatalogService) SyncLogs(ctx context.Context, limit int) ([]*model.ModuleSyncLog, error) {
	return s.repo.ListLogs(ctx, limit)
}

// syncOrderStatuses 从平台拉取本商户订单列表，刷新本地镜像的**状态**。
//
// 只更新 status / paid_at：items 与 payment_guide 是下单时的快照，不可变。
// 返回更新的条数。
func (s *ModuleCatalogService) syncOrderStatuses(ctx context.Context) (int, error) {
	rows, _, err := platform.NewModuleClient().ListOrders(ctx, "", 1, 200)
	if err != nil {
		return 0, err
	}
	updated := 0
	now := time.Now()
	for _, r := range rows {
		orderNo := mapStr(r, "order_no")
		status := mapStr(r, "status")
		if orderNo == "" || status == "" {
			continue
		}
		local, err := s.order.GetByOrderNo(ctx, orderNo)
		if err != nil || local == nil {
			continue // 本地没有的订单不新建：列表只以本地已下的单为准
		}
		if local.Status == status && local.PaidAt != nil {
			continue
		}
		local.Status = status
		local.UpdatedAt = now
		if status == model.ModuleOrderPaid || status == model.ModuleOrderFulfilled {
			if local.PaidAt == nil {
				local.PaidAt = &now
			}
		}
		if err := s.order.Upsert(ctx, local); err != nil {
			logger.Warnf("[module-sync] 刷新订单 %s 状态失败: %v", orderNo, err)
			continue
		}
		updated++
	}
	return updated, nil
}

// mapStr 从平台返回的 map 里安全取字符串字段
func mapStr(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}
