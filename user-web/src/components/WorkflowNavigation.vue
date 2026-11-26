<template>
  <nav
    v-if="visibleLinks.length"
    class="workflow-navigation"
    :aria-label="t('workflowNavigation.label')"
  >
    <router-link
      v-for="link in visibleLinks"
      :key="link.path"
      :to="link.path"
      class="workflow-link"
    >
      {{ t('workflowNavigation.' + link.key) }}
    </router-link>
  </nav>
</template>

<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { usePermissionStore } from '@/stores/permission'

const props = defineProps({
  owner: { type: String, required: true },
  links: { type: Array, required: true }
})
const { t } = useI18n()
const permission = usePermissionStore()
// 这些管理工作流的服务端端点限定管理员；菜单未加载或所属模块关闭时不提供入口。
const visibleLinks = computed(() => permission.isAdmin && permission.hasGrantedMenuPath(props.owner)
  ? props.links.filter(link => permission.canAccessPath(link.path))
  : [])
</script>

<style scoped>
.workflow-navigation { display: flex; flex-wrap: wrap; gap: 8px 20px; margin-bottom: 16px; }
.workflow-link { color: var(--el-color-primary); text-decoration: none; padding: 6px 0; }
.workflow-link:hover { text-decoration: underline; }
.workflow-link:focus-visible { outline: 2px solid var(--el-color-primary); outline-offset: 3px; }
</style>
