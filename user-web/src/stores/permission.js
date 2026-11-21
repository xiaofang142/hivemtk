import { defineStore, storeToRefs } from 'pinia';
import { computed, ref } from 'vue'
import { useUserStore } from './user'
import { getUserMenus } from '@/api/menu'
import { findNavigationTrail, getMenuLanding, getNavigationPaths, projectNavigation } from '@/utils/menuNavigation'

const ROLE_RANK = {
  viewer: 10,
  guest: 10,
  agent: 20,
  customer_service: 20,
  member: 25,
  supervisor: 30,
  manager: 40,
  admin: 80,
  owner: 100
};

export const usePermissionStore = defineStore('permission', () => {
  const userStore = useUserStore()
  const { role } = storeToRefs(userStore);

  const isAdmin = computed(() => role.value === 'admin');

  const isLoggedIn = computed(() => userStore.isLoggedIn);

  const roleRank = computed(() => ROLE_RANK[role.value] || 0);

  // —— 后端菜单权限（RBAC）状态 ——
  const menus = ref([])            // 当前用户可见菜单树（GET /api/menus）
  const allAccess = ref(false)     // 超管全量
  const menusLoaded = ref(false)
  const menusLoading = ref(false)
  // fail-closed（2026-11-20 交互审计 UWA-07）：菜单加载失败时 canAccessPath
  // 必须拒绝放行——权限未知时不等于"有权"，路由守卫据此拦截非管理员受保护路径。
  const menusLoadError = ref(false)
  let menusLoadPromise = null
  let menusLoadGeneration = 0

  // 递归收集树中的全部路由 path
  const collectPaths = (nodes, out = []) => {
    for (const n of nodes || []) {
      if (n.path) out.push(n.path)
      if (n.children?.length) collectPaths(n.children, out)
    }
    return out
  }

  const menuPaths = computed(() => collectPaths(menus.value))
  const grantedMenuPaths = computed(() => new Set(getNavigationPaths(menus.value)))
  // 动作显隐读取服务端实际返回的授权叶子，不继承路由守卫的放行与前缀规则。
  const hasGrantedMenuPath = (path) => menusLoaded.value && grantedMenuPaths.value.has(path)
  const navigationMenus = computed(() => projectNavigation(menus.value))
  const navigationPaths = computed(() => getNavigationPaths(navigationMenus.value))
  const getNavigationTrail = (route) => findNavigationTrail(navigationMenus.value, route)
  const getMenuReturnPath = (preferredPath, currentPath) => {
    if (preferredPath !== currentPath && hasGrantedMenuPath(preferredPath)) return preferredPath
    for (const menu of navigationMenus.value) {
      const landing = getMenuLanding(menu, currentPath)
      if (landing) return landing
    }
    return '/profile'
  }

  // ensureMenus 拉取当前用户菜单树（登录后调用；已加载则跳过）。
  // 并发调用共享同一个 Promise（单飞）；resetMenus 推进代次，
  // 迟到响应不得把旧账号的菜单/全量权限写回新身份。
  const ensureMenus = async (force = false) => {
    if (!userStore.isLoggedIn) {
      menus.value = []
      allAccess.value = false
      menusLoaded.value = false
      menusLoadError.value = false
      return
    }
    if (menusLoaded.value && !force) return
    if (menusLoadPromise) return menusLoadPromise
    menusLoading.value = true
    menusLoadError.value = false
    const loadGeneration = menusLoadGeneration
    let loadPromise
    loadPromise = (async () => {
      try {
        const res = await getUserMenus()
        if (loadGeneration !== menusLoadGeneration || !userStore.isLoggedIn) return
        menus.value = res?.tree || []
        allAccess.value = !!res?.all
        menusLoaded.value = true
        menusLoadError.value = false
      } catch (e) {
        if (loadGeneration !== menusLoadGeneration) return
        console.error('[permission] 拉取菜单权限失败', e)
        menus.value = []
        allAccess.value = false
        menusLoaded.value = false
        menusLoadError.value = true
        // 抛给路由守卫：由它区分 401 竞态并给出"菜单权限加载失败"的落地页，
        // 不能静默吞掉让权限未知态放行。
        throw e
      } finally {
        if (menusLoadPromise === loadPromise) {
          menusLoading.value = false
          menusLoadPromise = null
        }
      }
    })()
    menusLoadPromise = loadPromise
    return loadPromise
  }

  // 重置（登出/切换账号）
  const resetMenus = () => {
    menusLoadGeneration += 1
    menusLoadPromise = null
    menus.value = []
    allAccess.value = false
    menusLoaded.value = false
    menusLoading.value = false
    menusLoadError.value = false
  }

  // canAccessPath 路由守卫：路径是否在授权菜单内（超管放行）
  // 精确匹配或前缀匹配（带页目录下的深链，如 /browser-automation/tasks/create）
  const canAccessPath = (path) => {
    if (allAccess.value || role.value === 'admin') return true
    if (menusLoadError.value) return false
    if (!menusLoaded.value) return false
    if (!path) return true
    for (const p of menuPaths.value) {
      if (path === p || path.startsWith(p + '/')) return true
    }
    return false
  }

  // hasMenuKey 菜单 key 是否可见
  const hasMenuKey = (key) => {
    if (allAccess.value || role.value === 'admin') return true
    const found = (nodes) => {
      for (const n of nodes || []) {
        if (n.key === key) return true
        if (n.children?.length && found(n.children)) return true
      }
      return false
    }
    return found(menus.value)
  }

  const canAccess = (requiredRole) => {
    if (!isLoggedIn.value) return false
    if (!requiredRole) return true
    if (role.value === 'admin') return true
    const requiredRank = ROLE_RANK[requiredRole] || 0
    return roleRank.value >= requiredRank
  };

  const hasRole = (roles) => {
    if (!roles) return true
    const list = Array.isArray(roles) ? roles : [roles]
    if (list.length === 0) return true
    return list.includes(role.value)
  };

  const hasMenuPermission = (menu) => {
    if (!isLoggedIn.value) return false
    if (!menu || !menu.roles || menu.roles.length === 0) return true
    if (role.value === 'admin') return true
    return menu.roles.includes(role.value)
  };

  return {
    role,
    isAdmin,
    isLoggedIn,
    roleRank,
    canAccess,
    hasRole,
    hasMenuPermission,
    // RBAC 菜单
    menus,
    allAccess,
    menusLoaded,
    menusLoading,
    menusLoadError,
    menuPaths,
    navigationMenus,
    navigationPaths,
    getNavigationTrail,
    hasGrantedMenuPath,
    getMenuReturnPath,
    ensureMenus,
    resetMenus,
    canAccessPath,
    hasMenuKey
  }
})

export default usePermissionStore
