<template>
  
  <el-sub-menu
    v-if="menu.children && menu.children.length > 0"
    :index="menu.key || menu.path"
    popper-class="sidebar-menu-popper"
  >
    <template #title>
      <el-icon><component :is="resolveRouteIcon(menu.icon)" /></el-icon>
      <span>{{ menu.title }}</span>
    </template>

    
    <template v-for="item in menu.children" :key="item.path || item.key">
      <sub-menu-item :menu="item" :icon-components="props.iconComponents" />
    </template>
  </el-sub-menu>

  
  <el-menu-item v-else :index="menu.path" @click="handleMenuClick(menu)">
    <el-icon><component :is="resolveRouteIcon(menu.icon || 'Document')" /></el-icon>
    <span>{{ menu.title }}</span>
  </el-menu-item>
</template>

<script setup>
import { useRouter } from 'vue-router'
import { resolveRouteIcon } from '@/utils/iconMap'

// 菜单标题由 src/layout/menu.js 在构建时解析好（t('menu.<key>') → meta.title 回退），
// 这里只负责渲染 menu.title：组件内再拼一次 t('menu.' + key) 会让「键缺失」重新变成
// 界面上的一串裸 key，把菜单构建器的兜底白做一遍。
const router = useRouter()

const handleMenuClick = (menu) => {
  if (menu.path) {
    router.push(menu.path)
  }
}

const props = defineProps({
  menu: {
    type: Object,
    required: true
  },
  iconComponents: {
    type: Object,
    default: () => ({})
  }
})
</script>

<style lang="scss" scoped>
/* 确保三级菜单正确显示 */
:deep(.el-menu .el-sub-menu .el-menu-item) {
  padding-left: 50px !important;
  min-width: 200px;
}

:deep(.el-sub-menu .el-menu-item) {
  background-color: transparent;
}

:deep(.el-sub-menu .el-menu-item:hover) {
  background-color: rgba(255, 255, 255, 0.06);
}

/* 确保三级菜单缩进正确 */
:deep(.el-menu .el-sub-menu .el-sub-menu .el-menu-item) {
  padding-left: 70px !important;
}

/* 确保子菜单正确展开 */
:deep(.el-sub-menu .el-sub-menu__title) {
  padding-left: $spacing-lg !important;
}

:deep(.el-menu .el-sub-menu .el-sub-menu .el-sub-menu__title) {
  padding-left: 50px !important;
}

/* 确保三级菜单子菜单正确展开 */
:deep(.el-sub-menu .el-sub-menu .el-menu) {
  background-color: transparent;
}
</style>
