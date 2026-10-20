import { defineStore, storeToRefs } from 'pinia';
import { computed, ref } from 'vue'
import { useUserStore } from './user'
import { getUserMenus } from '@/api/menu'

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

  // 递归收集树中的全部路由 path
  const collectPaths = (nodes, out = []) => {
    for (const n of nodes || []) {
      if (n.path) out.push(n.path)
      if (n.children?.length) collectPaths(n.children, out)
    }
    return out
  }

  const menuPaths = computed(() => collectPaths(menus.value))

  // ensureMenus 拉取当前用户菜单树（登录后调用；已加载则跳过）
  const ensureMenus = async (force = false) => {
    if (!userStore.isLoggedIn) {
      menus.value = []
      allAccess.value = false
      menusLoaded.value = false
      return
    }
    if (menusLoaded.value && !force) return
    if (menusLoading.value) return
    menusLoading.value = true
    try {
      const res = await getUserMenus()
      menus.value = res?.tree || []
      allAccess.value = !!res?.all
      menusLoaded.value = true
    } catch (e) {
      console.error('[permission] 拉取菜单权限失败', e)
      menus.value = []
      allAccess.value = false
      menusLoaded.value = false
    } finally {
      menusLoading.value = false
    }
  }

  // 重置（登出/切换账号）
  const resetMenus = () => {
    menus.value = []
    allAccess.value = false
    menusLoaded.value = false
    menusLoading.value = false
  }

  const EXTRA_ALLOWED_PATHS = ['/kb/connectors', '/cap/matrix', '/mcp/credentials']

  // canAccessPath 路由守卫：路径是否在授权菜单内（超管放行）
  // 精确匹配或前缀匹配（带页目录下的深链，如 /browser-automation/tasks/create）
  const canAccessPath = (path) => {
    if (allAccess.value || role.value === 'admin') return true
    if (!menusLoaded.value) return true // 未加载（如公开页）不拦截，由调用方先 ensureMenus
    if (!path) return true
    for (const p of EXTRA_ALLOWED_PATHS) {
      if (path === p || path.startsWith(p + '/')) return true
    }
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
    menuPaths,
    ensureMenus,
    resetMenus,
    canAccessPath,
    hasMenuKey
  }
})

export default usePermissionStore
