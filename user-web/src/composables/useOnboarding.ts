import { driver as Driver } from 'driver.js'
import 'driver.js/dist/driver.css'
import { useUserStore } from '@/stores/user'
import { useI18n } from 'vue-i18n'
import { createPageTours, getPageTour, type PageTour } from '@/config/onboarding/pageTours'

const ONBOARDING_COMPLETED_KEY = 'hivemtk_onboarding_completed'
const ONBOARDING_STEP_KEY = 'hivemtk_onboarding_step'
const ONBOARDING_TOUR_TYPE_KEY = 'hivemtk_onboarding_tour_type'
/**
 * 「这一页的导览看过了」标记。
 *
 * 为什么单独一个键而不是复用 _completed：completed 表示「走完了」，
 * 关闭/跳过也要算「看过了」——否则用户点 × 关掉导览，下次进同一页又弹一遍，
 * 看起来就像关闭按钮没生效（旧实现正是这个毛病：markCompleted 只挂在最后一步的
 * onNextClick 上，× 关闭 / Esc / 点遮罩三条退出路径都不写标记）。
 */
const ONBOARDING_PAGE_SEEN_KEY = 'hivemtk_onboarding_page_seen'

type TourType = 'global' | 'module' | 'page'

interface OnboardingStep {
  element: string
  popover: {
    title: string
    description: string
    side?: 'top' | 'bottom' | 'left' | 'right'
    align?: 'start' | 'center' | 'end'
  }
  onHighlightStarted?: () => void
  onHighlighted?: () => void
  onDeselected?: () => void
}

let driverInstance: ReturnType<typeof Driver> | null = null
let currentTourType: TourType = 'global'
/** 当前正在跑的页面导览对应的路由模式（用于写「看过」标记） */
let currentPagePattern = ''

/**
 * 由 route.matched 还原「路由模式」（带 :param 的那一份）。
 *
 * 为什么不能直接用 route.path：route.path 是**实例**路径，
 * /aiAgent/edit/17 与 /aiAgent/edit/23 是两个不同的 path，但它们是同一张页面、
 * 应该共用同一条导览。注册表按模式登记，运行时必须用模式去查。
 *
 * 拼法：Layout 根记录的 path 是 '/'，跳过；子记录里绝对路径直接用，
 * 相对路径（browserAutomation 的 'tasks' 挂在 '/browser-automation' 下）
 * 拼到父路径后面。
 */
export function resolveRoutePattern(matched: Array<{ path: string }> | undefined): string {
  if (!matched || matched.length === 0) return ''
  let pattern = ''
  for (const record of matched) {
    const p = record.path
    if (!p || p === '/') continue
    pattern = p.startsWith('/') ? p : `${pattern}/${p}`
  }
  return pattern
}

/**
 * 步骤里是否至少有一个目标元素真实存在于 DOM 中。
 *
 * driver.js 找不到元素时会用「dummy element」（一个 0×0 的隐藏 div）占位，
 * 并把 undefined 作为 element 传给 onHighlighted / onDeselected hook。
 * 本仓的 hook 需要按「有没有外壳」来决定要不要开导览，所以先在开导览前自查。
 */
function hasLayoutChrome(steps: OnboardingStep[]): boolean {
  // 判定依据必须是 Layout 外壳本身，不能只看“某个步骤目标存在”：
  // 登录页、帮助中心等公开页也有自己的 .brand-mark 等装饰元素，
  // 只按目标元素判定会把 /login 误认为外壳已挂载，于是全局导览
  // 在未登录的登录页上弹出，并把气泡锚到登录页的 logo 上。
  if (!document.querySelector('.app-shell')) return false
  return steps.some(step => {
    try {
      return typeof step.element === 'string' && !!document.querySelector(step.element)
    } catch {
      // 选择器非法也不能让导览流程炸掉
      return false
    }
  })
}

/** 步骤目标是否**全部**都在 DOM 里。少一个就会有一个气泡锚在空气上，必须整条拒掉。 */
function allTargetsExist(steps: OnboardingStep[]): boolean {
  return steps.every(step => {
    try {
      return typeof step.element === 'string' && !!document.querySelector(step.element)
    } catch {
      return false
    }
  })
}

function getGlobalNavSteps(t: (key: string) => string): OnboardingStep[] {
  return [
    {
      element: '.brand-mark',
      popover: {
        title: t('setup.onboarding.global.step1.title'),
        description: t('setup.onboarding.global.step1.description'),
        side: 'bottom',
        align: 'center',
      },
    },
    {
      element: '.top-menu',
      popover: {
        title: t('setup.onboarding.global.step2.title'),
        description: t('setup.onboarding.global.step2.description'),
        side: 'bottom',
        align: 'center',
      },
    },
    {
      element: '.app-aside',
      popover: {
        title: t('setup.onboarding.global.step3.title'),
        description: t('setup.onboarding.global.step3.description'),
        side: 'right',
        align: 'center',
      },
    },
    {
      element: '.notif-bell',
      popover: {
        title: t('setup.onboarding.global.step4.title'),
        description: t('setup.onboarding.global.step4.description'),
        side: 'bottom',
        align: 'center',
      },
    },
    {
      element: '.user-area',
      popover: {
        title: t('setup.onboarding.global.step5.title'),
        description: t('setup.onboarding.global.step5.description'),
        side: 'bottom',
        align: 'center',
      },
    },
    {
      element: '.app-main',
      popover: {
        title: t('setup.onboarding.global.step6.title'),
        description: t('setup.onboarding.global.step6.description'),
        side: 'top',
        align: 'center',
      },
    },
  ]
}

/**
 * 由页面导览配置拼出 driver.js 的步骤。
 *
 * 文案在注册表里已按当前语言解析好（见 createPageTours），这里只做形状转换。
 */
function buildPageTourSteps(tour: PageTour): OnboardingStep[] {
  return tour.steps.map(step => ({
    element: step.element,
    popover: {
      title: step.title,
      description: step.description,
      side: step.side || 'bottom',
      align: step.align || 'center',
    },
  }))
}

export function useOnboarding() {
  const userStore = useUserStore()
  const { t, locale } = useI18n()

  /**
   * 页面导览表按语言缓存。
   *
   * 注册表里每条文案都是静态的点分键调用，一次构建要解析几百条词条；
   * 路由每变一次就重建太浪费。而文案又是按语言解析的，切换语言必须重建——
   * 所以缓存键取当前 locale。
   */
  let toursCache: { locale: string; tours: Record<string, PageTour> } | null = null
  function getTours(): Record<string, PageTour> {
    const currentLocale = locale.value
    if (toursCache && toursCache.locale === currentLocale) return toursCache.tours
    toursCache = { locale: currentLocale, tours: createPageTours(t) }
    return toursCache.tours
  }

  function isCompleted(tourType: TourType = 'global'): boolean {
    return localStorage.getItem(`${ONBOARDING_COMPLETED_KEY}_${tourType}`) === 'true'
  }

  function getCurrentStep(): number {
    return Number(localStorage.getItem(ONBOARDING_STEP_KEY) || '0')
  }

  function setCurrentStep(step: number): void {
    localStorage.setItem(ONBOARDING_STEP_KEY, String(step))
  }

  function markCompleted(tourType: TourType = 'global'): void {
    localStorage.setItem(`${ONBOARDING_COMPLETED_KEY}_${tourType}`, 'true')
    localStorage.removeItem(ONBOARDING_STEP_KEY)
  }

  /**
   * 记「这一页的导览已经看过」。
   *
   * 关闭（×）、跳过、Esc、点遮罩、走到最后点完成——五条退出路径**都**要写这个标记。
   * 少写一条，用户就会在下次进同一页时又被弹一遍，体感就是「关闭按钮无效」。
   */
  function markPageSeen(pattern: string): void {
    if (!pattern) return
    try {
      const seen = new Set<string>(readSeenPages())
      seen.add(pattern)
      localStorage.setItem(ONBOARDING_PAGE_SEEN_KEY, JSON.stringify([...seen]))
    } catch {
      // localStorage 不可用（隐私模式等）时不能阻断导览
    }
  }

  function readSeenPages(): string[] {
    try {
      const raw = localStorage.getItem(ONBOARDING_PAGE_SEEN_KEY)
      if (!raw) return []
      const parsed = JSON.parse(raw)
      return Array.isArray(parsed) ? parsed.filter(x => typeof x === 'string') : []
    } catch {
      return []
    }
  }

  function isPageSeen(pattern: string): boolean {
    if (!pattern) return false
    return readSeenPages().includes(pattern)
  }

  function reset(tourType: TourType = 'global'): void {
    localStorage.removeItem(`${ONBOARDING_COMPLETED_KEY}_${tourType}`)
    localStorage.removeItem(ONBOARDING_STEP_KEY)
    localStorage.removeItem(ONBOARDING_TOUR_TYPE_KEY)
  }

  /** 清掉「所有页面都看过」的记录（用于「重看引导」） */
  function resetPageSeen(): void {
    localStorage.removeItem(ONBOARDING_PAGE_SEEN_KEY)
  }

  function createDriver(
    steps: OnboardingStep[],
    onComplete?: () => void,
    onDestroy?: (reason?: string) => void,
  ): ReturnType<typeof Driver> {
    if (driverInstance) {
      driverInstance.destroy()
    }

    driverInstance = Driver({
      animate: true,
      allowClose: true,
      overlayOpacity: 0.75,
      overlayColor: '#0f172a',
      stageRadius: 8,
      stagePadding: 8,
      nextBtnText: t('setup.onboarding.next'),
      prevBtnText: t('setup.onboarding.prev'),
      doneBtnText: t('setup.onboarding.done'),
      // driver.js 1.x 的配置名是 closeBtnLabel（1.x 之前的 closeBtnText 已被移除，
      // 写 closeBtnText 不会报错但也不会生效，× 按钮的 aria-label 会一直是英文 Close）。
      closeBtnLabel: t('setup.onboarding.close'),
      keyboardControl: true,
      steps,
      onHighlightStarted: (_element, _step, opts) => {
        // driver.js 的 hook 签名是 (element, step, opts)——第一个参数是**元素**，
        // 不是步骤；旧代码把 element 当 step 用，steps.indexOf(element) 恒为 -1。
        //
        // 也不能用 steps.indexOf(step)：driver.js 传给 hook 的 step 是 B() 复制
        // 出来的副本（{...原步骤, popover:{...}}），不是 steps 数组里那个对象，
        // indexOf 依然返回 -1。权威的当前步号在 opts.index（= 0 基的活动索引）。
        const idx = opts?.index
        if (typeof idx === 'number' && idx >= 0) setCurrentStep(idx + 1)
      },
      onHighlighted: (_element, step) => {
        // 同上：step 才是步骤。element 可能是 undefined——driver.js 在步骤的
        // element 选择器没命中时用「dummy element」占位，并把 undefined 传给
        // hook。旧代码直接 `step.onHighlighted?.()`，在这种情况下抛
        // "Cannot read properties of undefined (reading 'onHighlighted')"。
        step?.onHighlighted?.()
      },
      onDeselected: (_element, step) => {
        step?.onDeselected?.()
      },
      // 「这一页看过了」的标记写在 onDestroyed，而不是 onCloseClick。
      //
      // 为什么不能挂在 onCloseClick 上：driver.js 1.x 里，弹窗的关闭按钮走的是
      // `popover.onCloseClick || config.onCloseClick`，**一旦提供了自定义
      // onCloseClick，driver 自己的 closeClick 事件就不会再 emit**，于是导览
      // 根本不会被销毁——表现就是「点了 × 没反应，弹窗还在」（旧实现正是这个毛病）。
      // 而且 Esc / 点遮罩这两条退出路径压根不经过 onCloseClick。
      //
      // onDestroyed 是**所有**退出路径的汇合点（× / Esc / 遮罩 / 完成 / API 销毁），
      // 在这里落盘最稳。opts.reason 给出退出原因：done=走完、close=×、
      // escape=Esc、overlay=点遮罩、api=代码调用 destroy。
      onDestroyed: (_element, _step, opts) => {
        driverInstance = null
        onDestroy?.(opts?.reason)
      },
    })

    // 监听完成事件（最后一步点击"完成"按钮）
    //
    // ⚠️ 这里必须自己调 destroy()，不能只打标记了事。
    //
    // 原因是 driver.js 的接线方式：构造每一步的 popover 时，B() 取的是
    //   onNextClick: 步骤级.popover.onNextClick || 默认(推进/销毁)
    // —— **步骤级优先**。我们一旦在最后一步的 popover 上挂了 onNextClick，
    // driver 自己「点到最后一页就 y('done') 销毁」的默认逻辑就再也不执行了。
    // 表现就是：点「完成」→ 标记打上了（下次不再弹），但**遮罩和气泡原地不动**，
    // 用户以为按钮失灵，只能再去点 ×（2026-10-22 逐页拟人点验实测复现）。
    //
    // onDestroyed 里已经有「无论哪条路退出都落标记」的兜底，所以这里主动
    // destroy 与它重复调用是无害的（markCompleted / markPageSeen 都是幂等写）。
    if (onComplete && steps.length > 0) {
      const instance = driverInstance
      const last = steps[steps.length - 1]
      const originalDoneClick = last?.popover?.onNextClick
      last.popover.onNextClick = () => {
        onComplete()
        originalDoneClick?.()
        instance?.destroy()
      }
    }

    return driverInstance
  }

  function startGlobalTour(): void {
    if (!userStore.isLoggedIn) return
    currentTourType = 'global'
    const steps = getGlobalNavSteps(t)
    // 全局导览引导的是「布局外壳」（品牌位/顶栏/侧边栏/通知铃/用户区/主区），
    // 这 6 个选择器全都只存在于 layout/Layout.vue。而 App.vue 的 tryAutoStart()
    // 在**任何**路由上都会触发——包括 /login、/chat/embed/*、/help-center、
    // /setup 这些不挂 Layout 的页面。外壳不在时 driver.js 会退化成
    // 「dummy element」：弹窗锚在一个不存在的点上，用户只看到一个居中气泡，
    // 且 onHighlighted 会拿到 undefined 元素抛 TypeError。
    // 所以外壳不在就不启动全局导览——没东西可引导。
    if (!hasLayoutChrome(steps)) {
      console.info('[Onboarding] 跳过全局导览：当前页面无布局外壳')
      return
    }
    // 全局导览的「完成」标记也写在 onDestroyed：旧实现只挂在最后一步的
    // onNextClick 上，用户点 × 关掉后标记不落盘 → 每次刷新都重新弹一遍，
    // 观感就是「关闭按钮无效」。这里无论从哪条路退出都记成已完成。
    const driver = createDriver(
      steps,
      () => markCompleted('global'),
      () => markCompleted('global'),
    )
    driver.drive()
  }

  /**
   * 打开某个页面的导览。
   *
   * @param pattern 路由模式（/aiAgent/edit/:id 这样的），由 route.matched 还原
   * @param force 忽略「看过」标记强制重放（用户菜单里「重看本页引导」用）
   * @returns 是否真的开起来了。调用方要区分「没开」的原因：
   *   - 没有配置      → 不用重试
   *   - 已经看过      → 不用重试
   *   - 元素还没出现  → **要**重试（页面数据还没加载完）
   */
  function startPageTour(pattern: string, force = false): boolean {
    if (!userStore.isLoggedIn) return false
    const tour = getPageTour(getTours(), pattern)
    if (!tour) {
      console.info(`[Onboarding] 该页面没有导览配置：${pattern}`)
      return false
    }
    if (!force && isPageSeen(pattern)) return false

    const steps = buildPageTourSteps(tour)
    // 目标元素必须**全部**在 DOM 里。少一个就会有一个气泡锚在 0×0 的
    // dummy element 上（屏幕正中的空气泡），而且用户点「下一步」才知道。
    // 与其弹一个半残的导览，不如这一页干脆不弹。
    if (!allTargetsExist(steps)) {
      const missing = steps
        .filter(s => !document.querySelector(s.element))
        .map(s => s.element)
      console.warn('[Onboarding] 跳过页面导览（目标元素缺失）：', missing)
      return false
    }

    currentTourType = 'page'
    currentPagePattern = pattern
    const driver = createDriver(
      steps,
      () => {
        markCompleted('page')
      },
      (reason) => {
        // 无论从哪条路退出（× / Esc / 遮罩 / 走完），这一页都要记成「看过」——
        // 否则用户点 × 关掉后下次进同一页又弹一遍，体感就是「关闭按钮无效」。
        // 走完（reason=done）额外记「完成」；中途关掉只记「看过」，
        // 这样「完成」这个状态仍能反映「确实读完了」。
        markPageSeen(pattern)
        if (reason === 'done') markCompleted('page')
      },
    )
    driver.drive()
    return true
  }

  /**
   * 某一页是否配了导览、其目标元素是否已就位。
   *
   * 调用方（Layout 的路由 watcher）用它区分「这一页压根没配导览」（不用重试）
   * 与「元素还没渲染出来」（要继续等）——数据驱动的页面（列表、卡片）都是
   * 接口返回后才渲染，路由切换那一刻 DOM 里还没有它们。
   */
  function getPageTourState(pattern: string): 'ready' | 'missing-config' | 'already-seen' | 'no-target' {
    const tour = getPageTour(getTours(), pattern)
    if (!tour) return 'missing-config'
    if (isPageSeen(pattern)) return 'already-seen'
    const steps = buildPageTourSteps(tour)
    return allTargetsExist(steps) ? 'ready' : 'no-target'
  }

  function startModuleTour(moduleKey: string): void {
    if (!userStore.isLoggedIn) return
    currentTourType = 'module'
    console.info(`[Onboarding] 模块导览未配置：${moduleKey}`)
  }

  function restart(tourType: TourType = 'global'): void {
    reset(tourType)
    if (tourType === 'global') startGlobalTour()
  }

  function destroy(): void {
    if (driverInstance) {
      driverInstance.destroy()
      driverInstance = null
    }
  }

  /** 当前是否有导览正在跑 */
  function isTourActive(): boolean {
    return driverInstance !== null
  }

  /** 当前正在跑的导览类型（global / module / page） */
  function getActiveTourType(): TourType {
    return currentTourType
  }

  /**
   * 只关掉「页面导览」，不动全局导览。
   *
   * 切页时用：页面导览的高亮还留在上个页面的 DOM 上、遮罩也还盖着，
   * 不收掉的话用户会以为新页面卡住了。但全局导览（首登时那 6 步）锚的是
   * 布局外壳，切页不影响它，不能一起关掉。
   */
  function destroyPageTour(): void {
    if (driverInstance && currentTourType === 'page') {
      driverInstance.destroy()
      driverInstance = null
    }
  }

  function shouldAutoStart(tourType: TourType = 'global'): boolean {
    return userStore.isLoggedIn && !isCompleted(tourType)
  }

  function getAvailableModules(): string[] {
    return Object.keys(getTours())
  }

  return {
    startGlobalTour,
    startModuleTour,
    startPageTour,
    getPageTourState,
    restart,
    destroy,
    destroyPageTour,
    isTourActive,
    getActiveTourType,
    isCompleted,
    getCurrentStep,
    shouldAutoStart,
    markCompleted,
    markPageSeen,
    isPageSeen,
    resetPageSeen,
    getAvailableModules,
  }
}

export function useOnboardingAutoStart() {
  const { shouldAutoStart, startGlobalTour, markCompleted } = useOnboarding()
  const { t } = useI18n()

  // 全局导览要等「布局外壳」真的挂上才能开：
  //   - 登录路径：token 落地后 SPA 才切到 Layout 路由，Layout 是**之后**才渲染的，
  //     固定延时会有竞态（机器慢时 1s 内 Layout 还没挂，外壳判定失败 → 导览被跳过）。
  //   - 无外壳页面（/login、/chat/embed/*、/help-center、/setup）：外壳永远不会出现，
  //     轮询到上限就放弃，不能让定时器一直挂着。
  // 轮询上限 3s：够慢机器渲染完，也短到不会在无外壳页面上留下可见延迟。
  function startWhenLayoutReady(maxWaitMs = 3000): void {
    const deadline = Date.now() + maxWaitMs
    const tick = () => {
      if (hasLayoutChrome(getGlobalNavSteps(t))) {
        startGlobalTour()
        return
      }
      if (Date.now() < deadline) {
        setTimeout(tick, 200)
        return
      }
      console.info('[Onboarding] 跳过全局导览：当前页面无布局外壳')
    }
    tick()
  }

  function tryAutoStart(): void {
    if (shouldAutoStart('global')) {
      startWhenLayoutReady()
    }
  }

  function onTourComplete(tourType: TourType = 'global'): void {
    markCompleted(tourType)
  }

  return {
    tryAutoStart,
    onTourComplete,
  }
}

export { createPageTours }
