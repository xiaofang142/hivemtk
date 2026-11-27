<template>
  <!--
    「只有一个子页」的分组不渲染成可展开的二级菜单（展开一项毫无意义），
    直接内联成叶子。但**标题必须沿用父节点**：父节点是注册表里声明的业务
    分类名（QQ 机器人 / 飞书 / 诊断 / 承接），子节点往往只是该分类下的
    具体页面名（机器人账号 / 飞书账号 / 审计中心 / 承接链接）。

    2026-11-27 菜单审计修正：此前内联用的是「子节点的 menu 对象」，
    于是侧栏只剩子页面名，父分类名整个消失——用户在菜单里找不到
    「QQ 机器人」「飞书」「诊断」「承接」这四个入口，只能看到它们
    下面那个更窄的页面名。改为：index/跳转用子节点 path（可点），
    标题与图标用父节点（分类语义）。
  -->
  <el-menu-item
    v-if="menu.children?.length === 1"
    :index="menu.children[0].path"
  >
    <el-icon><component :is="resolveRouteIcon(menu.icon || menu.children[0].icon || 'Document')" /></el-icon>
    <span>{{ t('menu.' + menu.key) }}</span>
  </el-menu-item>
  <el-sub-menu
    v-else-if="menu.children && menu.children.length > 0"
    :index="menu.key || menu.path"
    popper-class="sidebar-menu-popper"
  >
    <template #title>
      <el-icon><component :is="resolveRouteIcon(menu.icon)" /></el-icon>
      <span>{{ t('menu.' + menu.key) }}</span>
    </template>

    
    <template
      v-for="item in menu.children"
      :key="item.path || item.key"
    >
      <sub-menu-item :menu="item" />
    </template>
  </el-sub-menu>

  
  <el-menu-item
    v-else
    :index="menu.path"
  >
    <el-icon><component :is="resolveRouteIcon(menu.icon || 'Document')" /></el-icon>
    <span>{{ t('menu.' + menu.key) }}</span>
  </el-menu-item>
</template>

<script setup>
import { resolveRouteIcon } from '@/utils/iconMap'

import i18n from '@/i18n'
const t = i18n.global.t

defineProps({
  menu: {
    type: Object,
    required: true
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
