<template>
  <nav class="breadcrumb-bar" role="navigation" aria-label="面包屑导航">
    <el-breadcrumb separator="/" :aria-label="'面包屑：' + (items[items.length - 1]?.title || '')">
      <el-breadcrumb-item v-for="(item, idx) in items" :key="idx" :to="item.to">
        <el-icon v-if="item.icon" class="bc-icon" aria-hidden="true"><component :is="resolveIcon(item.icon)" /></el-icon>
        {{ item.title }}
      </el-breadcrumb-item>
    </el-breadcrumb>
  </nav>
</template>

<script setup>
import i18n from '@/i18n'

import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { resolveRouteIcon } from '@/utils/iconMap'
import { buildMenuTree, filterMenuByRole, findTitleChain } from '@/layout/menu'
import { useUserStore } from '@/stores/user'

// 面包屑不再是第二份手写菜单表（这里原来有 topMenusMap + subMenusMap 两张硬编码表，
// 与 Layout.vue 的菜单树各记一份，新页面只改一边就会出现"侧边栏有、面包屑没有"）。
// 现在与侧边栏同源：路由表 → layout/menu.js → 标题链。
let route = null
let router = null
try {
  route = useRoute()
  router = useRouter()
} catch { /* 无路由环境（Storybook 单独渲染组件）：保持空面包屑 */ }

const resolveIcon = resolveRouteIcon

const items = computed(() => {
  if (!route || !router) return []
  // 读一次 locale：切换语言时面包屑要跟着重算
  void i18n.global.locale.value
  const t = i18n.global.t
  const te = (key) => i18n.global.te(key) || i18n.global.te(key, 'zh')

  let role = 'admin'
  try {
    role = useUserStore().role || 'admin'
  } catch { /* 无 Pinia 环境（Storybook）：按 admin 全量渲染 */ }

  const tree = filterMenuByRole(buildMenuTree(router.getRoutes(), { t, te }), role)
  const chain = findTitleChain(tree, route.path) || []

  if (route.path === '/') {
    return [{ title: t('首页'), to: '/', icon: 'HomeFilled' }]
  }

  if (chain.length === 0) {
    // 详情/编辑等未进菜单的页面：只有「首页 + 本页标题」
    const title = route.meta?.title || route.path.split('/').filter(Boolean).pop() || route.path
    return [
      { title: t('首页'), to: '/', icon: 'HomeFilled' },
      { title: t(title) || title }
    ]
  }
  // 首段是分区标题，最后一段是当前页；中间分组标题一并保留
  return [{ title: t('首页'), to: '/', icon: 'HomeFilled' }].concat(
    chain.map((title, idx) => (idx === 0 ? { title, to: '/' } : { title }))
  )
})
</script>

<style scoped>
.breadcrumb-bar {
  padding: 12px 20px;
  background: #fff;
  border-bottom: 1px solid #ebeef5;
  margin-bottom: 16px;
}
.bc-icon {
  margin-right: 4px;
  vertical-align: -2px;
}
</style>
