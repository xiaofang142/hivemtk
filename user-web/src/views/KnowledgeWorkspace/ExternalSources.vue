<template>
  <!--
    外部知识源统一入口（2026-11-27 菜单审计）。

    ## 为什么要合并
    此前「知识中心」下并列两个语义几乎相同的菜单项，用户分不清该进哪个：
      - /knowledge/connectors  连接器配置        （Notion/飞书/钉钉/CRM 凭据 + 连通测试）
      - /kb/connectors         第三方知识库导入  （IMA/HTTP 连接器 + 定时同步落库）

    两者不是重复实现，而是**互补的两段能力**：
      - 前半段（凭据与连通）回答「我的第三方账号能不能连上」，实现在
        `/api/knowledge/connectors`（KV 配置，探针式鉴权测试，不落文档）；
      - 后半段（导入连接器）回答「连上之后把文档拉进来」，实现在
        `/api/kb-connectors`（类型注册表 + provider，拉取→分块→向量化→落库）。

    ## 为什么是两个 tab 而不是删掉一套
    强的那套（kb-connectors）**没有连通测试能力**，弱的那套（knowledge/connectors）
    **不能落库**。任何单边删除都会丢掉对方独有的能力，因此两套后端都保留，
    只在前端收敛为一个页面、两个 tab。

    ## 实现口径
    复用 /channels/* 已验证的「左 tab + 路由即 tab」模式：
    两个 tab 各自保留独立 path，深链与授权不变，菜单由 projectNavigation
    收敛为单个入口（见 utils/menuNavigation.js 的 connectorPaths）。
  -->
  <section
    class="kb-sources-module"
    data-testid="kb-sources-module"
  >
    <header class="kb-sources-heading">
      <h1>{{ $t('menu.kbSources') }}</h1>
      <p>{{ $t('kbSources.description') }}</p>
    </header>

    <el-tabs
      :model-value="route.path"
      tab-position="left"
      data-testid="kb-sources-tabs"
      @tab-change="openTab"
    >
      <el-tab-pane
        v-for="tab in visibleTabs"
        :key="tab.path"
        :name="tab.path"
        :label="tab.label"
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
import ConnectorCredentials from './Connectors.vue'
import ConnectorImport from '../kb/Connectors.vue'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const permission = usePermissionStore()

const tabs = [
  {
    path: '/knowledge/connectors',
    get label() { return t('kbSources.credentialsTab') },
    component: markRaw(ConnectorCredentials)
  },
  {
    path: '/kb/connectors',
    get label() { return t('kbSources.importTab') },
    component: markRaw(ConnectorImport)
  }
]

// 页面内容仍由原路由守卫授权；投影菜单不参与扩大权限。
const visibleTabs = computed(() => tabs.filter(tab => permission.hasGrantedMenuPath(tab.path)))

const activeTab = computed(() => visibleTabs.value.find(tab => tab.path === route.path))

function openTab(path) {
  if (path !== route.path && visibleTabs.value.some(tab => tab.path === path)) router.push(path)
}
</script>

<style scoped>
.kb-sources-module { min-width: 0; }
.kb-sources-heading h1 { margin: 0 0 8px; font-size: 24px; font-weight: 600; }
.kb-sources-heading p { margin: 0 0 16px; line-height: 1.6; color: var(--el-text-color-regular); }

.kb-sources-module :deep(.el-tabs--left) { display: flex; align-items: stretch; }
.kb-sources-module :deep(.el-tabs__header.is-left) { min-width: 180px; flex: 0 0 auto; }
.kb-sources-module :deep(.el-tabs__content) { flex: 1; min-width: 0; }
</style>
