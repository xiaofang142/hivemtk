<template>
  
  <el-sub-menu
    v-if="menu.children && menu.children.length > 0"
    :index="menu.key || menu.path"
    popper-class="sidebar-menu-popper"
  >
    <template #title>
      <el-icon><component :is="resolveRouteIcon(menu.icon)" /></el-icon>
      <span>{{ t('menu.' + menu.key) }}</span>
    </template>

    
    <template v-for="item in menu.children" :key="item.path || item.key">
      <sub-menu-item :menu="item" :icon-components="props.iconComponents" />
    </template>
  </el-sub-menu>

  
  <el-menu-item v-else :index="menu.path" @click="handleMenuClick(menu)">
    <el-icon><component :is="resolveRouteIcon(menu.icon || 'Document')" /></el-icon>
    <span>{{ t('menu.' + menu.key) }}</span>
  </el-menu-item>
</template>

<script setup>
import { useRouter } from 'vue-router'
import { resolveRouteIcon } from '@/utils/iconMap'

import i18n from '@/i18n'
const t = i18n.global.t

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
