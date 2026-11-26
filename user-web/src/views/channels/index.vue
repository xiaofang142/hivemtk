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
    <el-tabs
      :model-value="route.path"
      data-testid="channels-tabs"
      @tab-change="openTab"
    >
      <el-tab-pane
        v-for="tab in visibleTabs"
        :key="tab.path"
        :name="tab.path"
        :label="t(tab.label)"
      />
    </el-tabs>
    <component
      :is="activeTab.component"
      v-if="activeTab"
      :key="activeTab.path"
    />
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
</style>
