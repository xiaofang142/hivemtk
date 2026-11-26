<template>
  <section
    class="channels-module"
    data-testid="channels-module"
  >
    <header class="channels-heading">
      <h1>{{ t('channels.title') }}</h1>
      <p>{{ t('channels.description') }}</p>
      <p class="channels-service-note">
        {{ t('channels.serviceNote') }}
      </p>
    </header>
    <!-- T08 左右分栏：左侧为渠道模块导航（渠道/账号/识别码），右侧为对应内容表格。
         内容组件放进 el-tabs 默认插槽，才会落入 .el-tabs__content 出现在右栏。 -->
    <el-tabs
      :model-value="route.path"
      tab-position="left"
      data-testid="channels-tabs"
      @tab-change="openTab"
    >
      <el-tab-pane
        v-for="tab in visibleTabs"
        :key="tab.path"
        :name="tab.path"
        :label="t(tab.label)"
      />
      <component
        :is="activeTab.component"
        v-if="activeTab"
        :key="activeTab.path"
      />
    </el-tabs>
  </section>
</template>

<script setup>
import { computed, markRaw } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { usePermissionStore } from '@/stores/permission'
import ChannelManage from './channel-manage.vue'
import AccountManage from './account-manage.vue'
import IdentifyCodeManage from './identify-code-manage.vue'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const permission = usePermissionStore()
const tabs = [
  { path: '/channels/manage', label: 'channels.catalog', component: markRaw(ChannelManage) },
  { path: '/channels/accounts', label: 'channels.accounts', component: markRaw(AccountManage) },
  { path: '/channels/identify-codes', label: 'channels.identifyCodes', component: markRaw(IdentifyCodeManage) }
]
const visibleTabs = computed(() => tabs.filter(tab => permission.hasGrantedMenuPath(tab.path)))
// 页面内容仍由原路由守卫授权；投影菜单不参与扩大权限。
const activeTab = computed(() => visibleTabs.value.find(tab => tab.path === route.path))
function openTab(path) {
  if (path !== route.path && visibleTabs.value.some(tab => tab.path === path)) router.push(path)
}
</script>

<style scoped>
.channels-heading h1 { margin: 0 0 8px; font-size: 24px; font-weight: 600; }
.channels-heading p { margin: 0 0 8px; line-height: 1.6; color: var(--el-text-color-regular); }
.channels-heading .channels-service-note { color: var(--el-text-color-secondary); font-size: 13px; }
.channels-module { min-width: 0; }

/* T08 左右分栏：左栏固定 200px（与 customer360/List.vue 的 350px 固定左栏同口径），
   右栏 flex:1 + min-width:0，避免宽表格把布局撑破。窄屏回退为上下布局。 */
.channels-module :deep(.el-tabs--left) { display: flex; align-items: stretch; }
.channels-module :deep(.el-tabs__header.is-left) {
  flex: 0 0 200px;
  width: 200px;
  margin-right: 0;
  border-right: 1px solid var(--el-border-color-light);
}
.channels-module :deep(.el-tabs__nav-wrap.is-left) { height: 100%; }
.channels-module :deep(.el-tabs__item.is-left) { height: 44px; line-height: 44px; text-align: left; }
.channels-module :deep(.el-tabs__content) { flex: 1; min-width: 0; padding-left: 20px; }

@media (max-width: 900px) {
  .channels-module :deep(.el-tabs--left) { display: block; }
  .channels-module :deep(.el-tabs__header.is-left) {
    width: auto;
    flex-basis: auto;
    margin-bottom: 12px;
    border-right: 0;
  }
  .channels-module :deep(.el-tabs__content) { padding-left: 0; }
}
</style>
